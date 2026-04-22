package nodeloom

import (
	"sync"
	"time"
)

// Trace represents an end-to-end execution of an agent. It groups related
// spans and provides context for the entire operation.
type Trace struct {
	mu sync.Mutex

	traceID      string
	agentName    string
	agentVersion string
	environment  string
	sessionID    string
	input        map[string]any
	metadata     map[string]any
	startTime    time.Time
	ended        bool
	halted       bool

	enqueue func(*TelemetryEvent)
	genID   func() string
}

// IsHalted reports whether this trace was created against a halted agent.
// When true, no trace_start was enqueued; spans/end events are still safe to
// call but will be no-ops on the backend (the agent is halted, telemetry will
// be rejected). Callers should normally check this and skip work.
func (t *Trace) IsHalted() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.halted
}

// Span creates a new child span within this trace.
func (t *Trace) Span(name string, spanType SpanType, opts ...SpanOption) *Span {
	cfg := &spanConfig{}
	for _, opt := range opts {
		opt(cfg)
	}

	return &Span{
		traceID:      t.traceID,
		spanID:       t.genID(),
		parentSpanID: "",
		name:         name,
		spanType:     spanType,
		input:        cfg.input,
		metadata:     cfg.metadata,
		startTime:    time.Now().UTC(),
		enqueue:      t.enqueue,
	}
}

// ChildSpan creates a new span that is a child of the given parent span.
// This is useful for representing nested operations (e.g., an agent span
// containing multiple LLM calls).
func (t *Trace) ChildSpan(name string, spanType SpanType, parent *Span, opts ...SpanOption) *Span {
	cfg := &spanConfig{}
	for _, opt := range opts {
		opt(cfg)
	}

	parentID := ""
	if parent != nil {
		parentID = parent.SpanID()
	}

	return &Span{
		traceID:      t.traceID,
		spanID:       t.genID(),
		parentSpanID: parentID,
		name:         name,
		spanType:     spanType,
		input:        cfg.input,
		metadata:     cfg.metadata,
		startTime:    time.Now().UTC(),
		enqueue:      t.enqueue,
	}
}

// End marks this trace as complete with the given status. Optional EndOption
// values can attach output data. Calling End more than once has no effect.
func (t *Trace) End(status TraceStatus, opts ...EndOption) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.ended {
		return
	}
	t.ended = true

	cfg := &endConfig{}
	for _, opt := range opts {
		opt(cfg)
	}

	event := &TelemetryEvent{
		Type:      EventTypeTraceEnd,
		TraceID:   t.traceID,
		Status:    status,
		Output:    cfg.output,
		ErrorMsg:  cfg.errorMsg,
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
	}

	t.enqueue(event)
}

// Event records a standalone event associated with this trace.
func (t *Trace) Event(name string, level EventLevel, data map[string]any) {
	event := &TelemetryEvent{
		Type:      EventTypeEvent,
		TraceID:   t.traceID,
		EventName: name,
		Level:     level,
		Data:      data,
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
	}

	t.enqueue(event)
}

// Feedback submits feedback for this trace.
func (t *Trace) Feedback(rating int, comment string) {
	event := &TelemetryEvent{
		Type:            EventTypeEvent,
		TraceID:         t.traceID,
		EventName:       "feedback",
		Timestamp:       time.Now().UTC().Format(time.RFC3339Nano),
		FeedbackRating:  rating,
		FeedbackComment: comment,
	}
	// Override type to "feedback"
	event.Type = "feedback"
	t.enqueue(event)
}

// TraceID returns the unique identifier for this trace.
func (t *Trace) TraceID() string {
	return t.traceID
}
