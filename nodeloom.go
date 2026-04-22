// Package nodeloom provides a Go SDK for instrumenting AI agents and sending
// telemetry to NodeLoom's monitoring pipeline. It is designed for production use
// with zero external dependencies, fire-and-forget semantics, and automatic
// client-side batching.
//
// Basic usage:
//
//	client := nodeloom.New("sdk_...", nodeloom.WithEndpoint("https://api.nodeloom.io"))
//	defer client.Close()
//
//	trace := client.Trace("my-agent", nodeloom.WithInput(map[string]any{"query": "..."}))
//	span := trace.Span("openai-call", nodeloom.SpanTypeLLM)
//	span.SetOutput(result)
//	span.SetTokenUsage(150, 200, "gpt-4o")
//	span.End()
//	trace.End(nodeloom.StatusSuccess, nodeloom.WithOutput(finalResult))
package nodeloom

import (
	"crypto/rand"
	"fmt"
	"log"
	"sync"
	"time"
)

// Client is the main entry point for sending telemetry to NodeLoom. It manages
// event queueing, batching, and transport. A Client is safe for concurrent use.
type Client struct {
	mu        sync.RWMutex
	config    Config
	queue     *queue
	proc      *batchProcessor
	apiClient *ApiClient
	registry  *ControlRegistry
	poller    *controlPoller
	closed    bool
}

// New creates a new NodeLoom client with the given API key and options.
// The client starts a background goroutine for batch processing. You must
// call Close() when you are done to flush pending events and release resources.
func New(apiKey string, opts ...Option) *Client {
	cfg := defaultConfig(apiKey)
	for _, opt := range opts {
		opt(&cfg)
	}

	if cfg.APIKey == "" {
		log.Printf("[nodeloom] warning: API key is empty, events will still be queued but requests will fail")
	}

	q := newQueue(cfg.MaxQueueSize)
	registry := NewControlRegistry()
	t := newTransportWithRegistry(cfg.Endpoint, cfg.APIKey, cfg.MaxRetries, registry)
	proc := newBatchProcessor(q, t, cfg.BatchSize, cfg.FlushInterval)

	c := &Client{
		config:   cfg,
		queue:    q,
		proc:     proc,
		registry: registry,
	}

	if cfg.ControlPollInterval > 0 {
		c.poller = newControlPoller(registry, c.Api, cfg.ControlPollInterval)
		c.poller.start()
	}

	proc.start()
	return c
}

// Trace starts a new trace for the named agent. A trace_start event is
// immediately enqueued. The returned Trace should be ended with Trace.End()
// when the agent execution completes.
//
// When the agent has been halted by the NodeLoom backend (per-agent or
// team-wide), Trace returns a non-nil halted Trace alongside an
// AgentHaltedError. Callers should check for ErrAgentHalted via
// errors.Is and refuse to proceed.
func (c *Client) Trace(agentName string, opts ...TraceOption) *Trace {
	t, _ := c.TraceWithControl(agentName, opts...)
	return t
}

// TraceWithControl is the explicit variant of Trace that surfaces halt errors.
// Use this when you want to react to halt programmatically; otherwise use
// Trace and check the returned trace's IsHalted() helper.
func (c *Client) TraceWithControl(agentName string, opts ...TraceOption) (*Trace, error) {
	cfg := &traceConfig{}
	for _, opt := range opts {
		opt(cfg)
	}

	traceID := generateUUID()

	t := &Trace{
		traceID:      traceID,
		agentName:    agentName,
		agentVersion: c.config.AgentVersion,
		environment:  c.config.Environment,
		input:        cfg.input,
		metadata:     cfg.metadata,
		startTime:    time.Now().UTC(),
		enqueue:      c.enqueue,
		genID:        generateUUID,
	}

	if cfg.sessionID != "" {
		t.sessionID = cfg.sessionID
	}

	// Halt check: if the registry says the agent (or its team) is halted, mark
	// the trace as halted, skip the trace_start event, and return the error.
	if c.registry != nil {
		if haltErr := c.registry.haltError(agentName); haltErr != nil {
			t.halted = true
			return t, haltErr
		}
	}

	// Phase 2: attach the cached guardrail session id so HARD-mode
	// required-guardrail enforcement can correlate this trace with a recent check.
	guardrailSessionID := ""
	if c.registry != nil {
		guardrailSessionID = c.registry.TakeGuardrailSession(agentName)
	}

	// Enqueue trace_start event immediately.
	event := &TelemetryEvent{
		Type:               EventTypeTraceStart,
		TraceID:            traceID,
		AgentName:          agentName,
		AgentVersion:       c.config.AgentVersion,
		Environment:        c.config.Environment,
		SessionID:          cfg.sessionID,
		Input:              cfg.input,
		Metadata:           cfg.metadata,
		SDKLanguage:        "go",
		GuardrailSessionID: guardrailSessionID,
		Timestamp:          t.startTime.Format(time.RFC3339Nano),
	}
	c.enqueue(event)

	return t, nil
}

// Control returns the in-memory control registry. Useful for tests and custom
// halt-detection workflows; production callers typically rely on
// TraceWithControl returning the halt error directly.
func (c *Client) Control() *ControlRegistry {
	return c.registry
}

// Metric records a custom metric event.
func (c *Client) Metric(name string, value float64, unit string, tags map[string]string) {
	event := &TelemetryEvent{
		Type:        EventTypeMetric,
		MetricName:  name,
		MetricValue: value,
		MetricUnit:  unit,
		MetricTags:  tags,
		Timestamp:   time.Now().UTC().Format(time.RFC3339Nano),
	}
	c.enqueue(event)
}

// Feedback records a feedback event for a trace.
func (c *Client) Feedback(traceID string, rating int, comment string) {
	event := &TelemetryEvent{
		Type:            EventTypeFeedback,
		TraceID:         traceID,
		FeedbackRating:  rating,
		FeedbackComment: comment,
		Timestamp:       time.Now().UTC().Format(time.RFC3339Nano),
	}
	c.enqueue(event)
}

// Event records a standalone event that is not associated with any trace.
func (c *Client) Event(name string, level EventLevel, data map[string]any) {
	event := &TelemetryEvent{
		Type:      EventTypeEvent,
		TraceID:   "",
		EventName: name,
		Level:     level,
		Data:      data,
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
	}
	c.enqueue(event)
}

// Flush triggers an immediate flush of all queued events. This is a
// convenience method; normal operation relies on automatic batching.
func (c *Client) Flush() {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.closed {
		return
	}

	events := c.queue.drain(c.config.BatchSize * 10)
	if len(events) > 0 {
		c.proc.flush(events)
	}
}

// Close shuts down the client, flushing any remaining events. It blocks until
// all events are sent or the shutdown timeout (5 seconds by default) expires.
// After Close returns, further calls to Trace or enqueue are no-ops.
func (c *Client) Close() {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	c.closed = true
	c.mu.Unlock()

	if c.poller != nil {
		c.poller.stop(c.config.ShutdownWait)
	}
	c.proc.stop(c.config.ShutdownWait)
}

// Api returns the REST API client for making authenticated requests
// to all NodeLoom API endpoints.
func (c *Client) Api() *ApiClient {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.apiClient == nil {
		c.apiClient = newApiClientWithRegistry(c.config.APIKey, c.config.Endpoint, c.registry)
	}
	return c.apiClient
}

// enqueue adds a telemetry event to the internal queue. If the client is
// closed, the event is silently dropped.
func (c *Client) enqueue(event *TelemetryEvent) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.closed {
		return
	}

	c.queue.enqueue(event)
}

// generateUUID produces a Version 4 UUID string using crypto/rand.
func generateUUID() string {
	var uuid [16]byte
	_, err := rand.Read(uuid[:])
	if err != nil {
		// Fallback: this should never happen, but if it does, produce a
		// timestamp-based identifier so traces are not lost.
		return fmt.Sprintf("fallback-%d", time.Now().UnixNano())
	}

	// Set version 4 bits.
	uuid[6] = (uuid[6] & 0x0f) | 0x40
	// Set variant bits.
	uuid[8] = (uuid[8] & 0x3f) | 0x80

	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		uuid[0:4], uuid[4:6], uuid[6:8], uuid[8:10], uuid[10:16])
}
