package nodeloom

import (
	"errors"
	"sync"
	"testing"
	"time"
)

// eventCollector is a test helper that captures enqueued events.
type eventCollector struct {
	mu     sync.Mutex
	events []*TelemetryEvent
}

func (ec *eventCollector) collect(e *TelemetryEvent) {
	ec.mu.Lock()
	defer ec.mu.Unlock()
	ec.events = append(ec.events, e)
}

func (ec *eventCollector) all() []*TelemetryEvent {
	ec.mu.Lock()
	defer ec.mu.Unlock()
	out := make([]*TelemetryEvent, len(ec.events))
	copy(out, ec.events)
	return out
}

func (ec *eventCollector) byType(t EventType) []*TelemetryEvent {
	ec.mu.Lock()
	defer ec.mu.Unlock()
	var out []*TelemetryEvent
	for _, e := range ec.events {
		if e.Type == t {
			out = append(out, e)
		}
	}
	return out
}

func newTestTrace(collector *eventCollector) *Trace {
	return &Trace{
		traceID:      "test-trace-id",
		agentName:    "test-agent",
		agentVersion: "1.0.0",
		environment:  "test",
		startTime:    time.Now().UTC(),
		enqueue:      collector.collect,
		genID:        func() string { return generateUUID() },
	}
}

func TestTrace_Span_CreatesSpan(t *testing.T) {
	collector := &eventCollector{}
	trace := newTestTrace(collector)

	span := trace.Span("llm-call", SpanTypeLLM)
	if span == nil {
		t.Fatal("expected non-nil span")
	}
	if span.traceID != "test-trace-id" {
		t.Errorf("expected trace ID 'test-trace-id', got %q", span.traceID)
	}
	if span.name != "llm-call" {
		t.Errorf("expected span name 'llm-call', got %q", span.name)
	}
	if span.spanType != SpanTypeLLM {
		t.Errorf("expected span type LLM, got %q", span.spanType)
	}
	if span.parentSpanID != "" {
		t.Errorf("expected empty parent span ID, got %q", span.parentSpanID)
	}
	if span.spanID == "" {
		t.Error("expected non-empty span ID")
	}
}

func TestTrace_Span_WithOptions(t *testing.T) {
	collector := &eventCollector{}
	trace := newTestTrace(collector)

	input := map[string]any{"prompt": "hello"}
	meta := map[string]any{"model": "gpt-4o"}

	span := trace.Span("llm-call", SpanTypeLLM,
		WithSpanInput(input),
		WithSpanMetadata(meta),
	)

	if span.input["prompt"] != "hello" {
		t.Errorf("expected input prompt 'hello', got %v", span.input["prompt"])
	}
	if span.metadata["model"] != "gpt-4o" {
		t.Errorf("expected metadata model 'gpt-4o', got %v", span.metadata["model"])
	}
}

func TestTrace_ChildSpan(t *testing.T) {
	collector := &eventCollector{}
	trace := newTestTrace(collector)

	parent := trace.Span("agent-step", SpanTypeAgent)
	child := trace.ChildSpan("llm-call", SpanTypeLLM, parent)

	if child.parentSpanID != parent.SpanID() {
		t.Errorf("expected parent span ID %q, got %q", parent.SpanID(), child.parentSpanID)
	}
	if child.traceID != trace.traceID {
		t.Errorf("expected trace ID %q, got %q", trace.traceID, child.traceID)
	}
}

func TestTrace_ChildSpan_NilParent(t *testing.T) {
	collector := &eventCollector{}
	trace := newTestTrace(collector)

	child := trace.ChildSpan("llm-call", SpanTypeLLM, nil)

	if child.parentSpanID != "" {
		t.Errorf("expected empty parent span ID for nil parent, got %q", child.parentSpanID)
	}
}

func TestTrace_End_EnqueuesEvent(t *testing.T) {
	collector := &eventCollector{}
	trace := newTestTrace(collector)

	output := map[string]any{"result": "success"}
	trace.End(StatusSuccess, WithOutput(output))

	events := collector.byType(EventTypeTraceEnd)
	if len(events) != 1 {
		t.Fatalf("expected 1 trace_end event, got %d", len(events))
	}

	e := events[0]
	if e.TraceID != "test-trace-id" {
		t.Errorf("expected trace ID 'test-trace-id', got %q", e.TraceID)
	}
	if e.Status != StatusSuccess {
		t.Errorf("expected status 'success', got %q", e.Status)
	}
	if e.Output["result"] != "success" {
		t.Errorf("expected output result 'success', got %v", e.Output["result"])
	}
}

func TestTrace_End_Idempotent(t *testing.T) {
	collector := &eventCollector{}
	trace := newTestTrace(collector)

	trace.End(StatusSuccess)
	trace.End(StatusError)
	trace.End(StatusSuccess)

	events := collector.byType(EventTypeTraceEnd)
	if len(events) != 1 {
		t.Errorf("expected exactly 1 trace_end event (idempotent), got %d", len(events))
	}
}

func TestTrace_Event(t *testing.T) {
	collector := &eventCollector{}
	trace := newTestTrace(collector)

	data := map[string]any{"reason": "content_filter"}
	trace.Event("guardrail_triggered", EventLevelWarn, data)

	events := collector.byType(EventTypeEvent)
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}

	e := events[0]
	if e.EventName != "guardrail_triggered" {
		t.Errorf("expected event name 'guardrail_triggered', got %q", e.EventName)
	}
	if e.Level != EventLevelWarn {
		t.Errorf("expected level 'warn', got %q", e.Level)
	}
	if e.TraceID != "test-trace-id" {
		t.Errorf("expected trace ID 'test-trace-id', got %q", e.TraceID)
	}
}

func TestSpan_End_EnqueuesEvent(t *testing.T) {
	collector := &eventCollector{}
	trace := newTestTrace(collector)

	span := trace.Span("tool-call", SpanTypeTool)
	span.SetInput(map[string]any{"arg": "value"})
	span.SetOutput(map[string]any{"result": 42})
	span.End()

	events := collector.byType(EventTypeSpan)
	if len(events) != 1 {
		t.Fatalf("expected 1 span event, got %d", len(events))
	}

	e := events[0]
	if e.Name != "tool-call" {
		t.Errorf("expected span name 'tool-call', got %q", e.Name)
	}
	if e.SpanType != SpanTypeTool {
		t.Errorf("expected span type 'tool', got %q", e.SpanType)
	}
	if e.SpanStatus != StatusSuccess {
		t.Errorf("expected default status 'success', got %q", e.SpanStatus)
	}
	if e.SpanInput["arg"] != "value" {
		t.Errorf("expected input arg 'value', got %v", e.SpanInput["arg"])
	}
	if e.SpanOutput["result"] != 42 {
		t.Errorf("expected output result 42, got %v", e.SpanOutput["result"])
	}
}

func TestSpan_End_Idempotent(t *testing.T) {
	collector := &eventCollector{}
	trace := newTestTrace(collector)

	span := trace.Span("test", SpanTypeCustom)
	span.End()
	span.End()
	span.End()

	events := collector.byType(EventTypeSpan)
	if len(events) != 1 {
		t.Errorf("expected exactly 1 span event (idempotent), got %d", len(events))
	}
}

func TestSpan_EndWithError(t *testing.T) {
	collector := &eventCollector{}
	trace := newTestTrace(collector)

	span := trace.Span("failing-call", SpanTypeLLM)
	span.EndWithError(errors.New("connection timeout"))

	events := collector.byType(EventTypeSpan)
	if len(events) != 1 {
		t.Fatalf("expected 1 span event, got %d", len(events))
	}

	e := events[0]
	if e.SpanStatus != StatusError {
		t.Errorf("expected status 'error', got %q", e.SpanStatus)
	}
	if e.SpanError != "connection timeout" {
		t.Errorf("expected error 'connection timeout', got %v", e.SpanError)
	}
}

func TestSpan_SetTokenUsage(t *testing.T) {
	collector := &eventCollector{}
	trace := newTestTrace(collector)

	span := trace.Span("llm-call", SpanTypeLLM)
	span.SetTokenUsage(150, 200, "gpt-4o")
	span.End()

	events := collector.byType(EventTypeSpan)
	if len(events) != 1 {
		t.Fatalf("expected 1 span event, got %d", len(events))
	}

	tu := events[0].TokenUsage
	if tu == nil {
		t.Fatal("expected non-nil token usage")
	}
	if tu.PromptTokens != 150 {
		t.Errorf("expected 150 prompt tokens, got %d", tu.PromptTokens)
	}
	if tu.CompletionTokens != 200 {
		t.Errorf("expected 200 completion tokens, got %d", tu.CompletionTokens)
	}
	if tu.TotalTokens != 350 {
		t.Errorf("expected 350 total tokens, got %d", tu.TotalTokens)
	}
	if tu.Model != "gpt-4o" {
		t.Errorf("expected model 'gpt-4o', got %q", tu.Model)
	}
}

func TestSpan_SetStatus(t *testing.T) {
	collector := &eventCollector{}
	trace := newTestTrace(collector)

	span := trace.Span("test", SpanTypeCustom)
	span.SetStatus(StatusError)
	span.End()

	events := collector.byType(EventTypeSpan)
	if len(events) != 1 {
		t.Fatalf("expected 1 span event, got %d", len(events))
	}
	if events[0].SpanStatus != StatusError {
		t.Errorf("expected status 'error', got %q", events[0].SpanStatus)
	}
}

func TestSpan_Timestamps(t *testing.T) {
	collector := &eventCollector{}
	trace := newTestTrace(collector)

	span := trace.Span("test", SpanTypeCustom)
	time.Sleep(5 * time.Millisecond)
	span.End()

	events := collector.byType(EventTypeSpan)
	if len(events) != 1 {
		t.Fatalf("expected 1 span event, got %d", len(events))
	}

	e := events[0]
	if e.Timestamp == "" {
		t.Error("expected non-empty timestamp")
	}
	if e.EndTimestamp == "" {
		t.Error("expected non-empty end_timestamp")
	}
	if e.Timestamp == e.EndTimestamp {
		t.Error("expected start and end timestamps to differ")
	}
}

func TestSpan_ConcurrentAccess(t *testing.T) {
	collector := &eventCollector{}
	trace := newTestTrace(collector)

	span := trace.Span("concurrent-test", SpanTypeCustom)

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			span.SetInput(map[string]any{"i": i})
			span.SetOutput(map[string]any{"i": i})
			span.SetMetadata(map[string]any{"i": i})
			span.SetTokenUsage(i, i, "model")
		}(i)
	}
	wg.Wait()

	span.End()

	events := collector.byType(EventTypeSpan)
	if len(events) != 1 {
		t.Errorf("expected exactly 1 span event after concurrent access, got %d", len(events))
	}
}

func TestSpanType_Values(t *testing.T) {
	expected := map[SpanType]string{
		SpanTypeLLM:       "llm",
		SpanTypeTool:      "tool",
		SpanTypeRetrieval: "retrieval",
		SpanTypeAgent:     "agent",
		SpanTypeChain:     "chain",
		SpanTypeCustom:    "custom",
	}
	for st, s := range expected {
		if string(st) != s {
			t.Errorf("expected SpanType %q, got %q", s, string(st))
		}
	}
}

func TestTrace_End_WithError(t *testing.T) {
	collector := &eventCollector{}
	trace := newTestTrace(collector)

	trace.End(StatusError, WithError("agent crashed"), WithOutput(map[string]any{"partial": true}))

	events := collector.byType(EventTypeTraceEnd)
	if len(events) != 1 {
		t.Fatalf("expected 1 trace_end event, got %d", len(events))
	}

	e := events[0]
	if e.Status != StatusError {
		t.Errorf("expected status 'error', got %q", e.Status)
	}
	if e.ErrorMsg != "agent crashed" {
		t.Errorf("expected error 'agent crashed', got %q", e.ErrorMsg)
	}
	if e.Output["partial"] != true {
		t.Errorf("expected output partial=true, got %v", e.Output["partial"])
	}
}

func TestSpan_EndWithError_Idempotent(t *testing.T) {
	collector := &eventCollector{}
	trace := newTestTrace(collector)

	span := trace.Span("test", SpanTypeCustom)
	span.EndWithError(errors.New("first error"))
	span.EndWithError(errors.New("second error"))
	span.End()

	events := collector.byType(EventTypeSpan)
	if len(events) != 1 {
		t.Errorf("expected exactly 1 span event (idempotent), got %d", len(events))
	}
	if events[0].SpanError != "first error" {
		t.Errorf("expected first error message, got %q", events[0].SpanError)
	}
}

func TestTraceStatus_Values(t *testing.T) {
	if string(StatusSuccess) != "success" {
		t.Errorf("expected 'success', got %q", string(StatusSuccess))
	}
	if string(StatusError) != "error" {
		t.Errorf("expected 'error', got %q", string(StatusError))
	}
}
