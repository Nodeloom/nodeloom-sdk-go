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
	mu     sync.RWMutex
	config Config
	queue  *queue
	proc   *batchProcessor
	closed bool
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
	t := newTransport(cfg.Endpoint, cfg.APIKey, cfg.MaxRetries)
	proc := newBatchProcessor(q, t, cfg.BatchSize, cfg.FlushInterval)

	c := &Client{
		config: cfg,
		queue:  q,
		proc:   proc,
	}

	proc.start()
	return c
}

// Trace starts a new trace for the named agent. A trace_start event is
// immediately enqueued. The returned Trace should be ended with Trace.End()
// when the agent execution completes.
func (c *Client) Trace(agentName string, opts ...TraceOption) *Trace {
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

	// Enqueue trace_start event immediately.
	event := &TelemetryEvent{
		Type:         EventTypeTraceStart,
		TraceID:      traceID,
		AgentName:    agentName,
		AgentVersion: c.config.AgentVersion,
		Environment:  c.config.Environment,
		Input:        cfg.input,
		Metadata:     cfg.metadata,
		Timestamp:    t.startTime.Format(time.RFC3339Nano),
	}
	c.enqueue(event)

	return t
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

	c.proc.stop(c.config.ShutdownWait)
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
