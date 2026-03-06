package nodeloom

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestBatchProcessor_FlushOnBatchSize(t *testing.T) {
	var received atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var batch BatchRequest
		if err := json.NewDecoder(r.Body).Decode(&batch); err != nil {
			t.Errorf("failed to decode batch: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		received.Add(int64(len(batch.Events)))
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(BatchResponse{Accepted: len(batch.Events)})
	}))
	defer server.Close()

	q := newQueue(1000)
	tr := newTransport(server.URL, "test-key", 1)
	batchSize := 5
	// Use a long flush interval so only batch-size triggers a flush.
	proc := newBatchProcessor(q, tr, batchSize, 1*time.Minute)
	proc.start()

	// Enqueue exactly batchSize events.
	for i := 0; i < batchSize; i++ {
		q.enqueue(&TelemetryEvent{
			Type:      EventTypeEvent,
			EventName: "test",
			Level:     EventLevelInfo,
			Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
		})
	}

	// Wait for the batch to be sent.
	deadline := time.After(3 * time.Second)
	for {
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for batch flush, received %d events", received.Load())
		default:
			if received.Load() >= int64(batchSize) {
				proc.stop(2 * time.Second)
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
}

func TestBatchProcessor_FlushOnInterval(t *testing.T) {
	var received atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var batch BatchRequest
		if err := json.NewDecoder(r.Body).Decode(&batch); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		received.Add(int64(len(batch.Events)))
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(BatchResponse{Accepted: len(batch.Events)})
	}))
	defer server.Close()

	q := newQueue(1000)
	tr := newTransport(server.URL, "test-key", 1)
	// Large batch size so the interval triggers the flush, not the batch size.
	proc := newBatchProcessor(q, tr, 1000, 100*time.Millisecond)
	proc.start()

	// Enqueue fewer events than batch size.
	for i := 0; i < 3; i++ {
		q.enqueue(&TelemetryEvent{
			Type:      EventTypeEvent,
			EventName: "test",
			Level:     EventLevelInfo,
			Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
		})
	}

	// Wait for the interval-based flush.
	deadline := time.After(3 * time.Second)
	for {
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for interval flush, received %d events", received.Load())
		default:
			if received.Load() >= 3 {
				proc.stop(2 * time.Second)
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
}

func TestBatchProcessor_FlushOnShutdown(t *testing.T) {
	var received atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var batch BatchRequest
		if err := json.NewDecoder(r.Body).Decode(&batch); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		received.Add(int64(len(batch.Events)))
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(BatchResponse{Accepted: len(batch.Events)})
	}))
	defer server.Close()

	q := newQueue(1000)
	tr := newTransport(server.URL, "test-key", 1)
	// Long intervals so only shutdown triggers the flush.
	proc := newBatchProcessor(q, tr, 1000, 1*time.Minute)
	proc.start()

	for i := 0; i < 7; i++ {
		q.enqueue(&TelemetryEvent{
			Type:      EventTypeEvent,
			EventName: "test",
			Level:     EventLevelInfo,
			Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
		})
	}

	// Give the processor a moment to pick up events, then stop.
	time.Sleep(50 * time.Millisecond)
	proc.stop(5 * time.Second)

	if received.Load() < 7 {
		t.Errorf("expected at least 7 events flushed on shutdown, got %d", received.Load())
	}
}

func TestBatchProcessor_BatchRequestFormat(t *testing.T) {
	var capturedBatch BatchRequest
	var mu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		json.NewDecoder(r.Body).Decode(&capturedBatch)

		// Verify headers.
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("expected Content-Type 'application/json', got %q", ct)
		}
		if auth := r.Header.Get("Authorization"); auth != "Bearer test-key" {
			t.Errorf("expected Authorization 'Bearer test-key', got %q", auth)
		}
		if ua := r.Header.Get("User-Agent"); ua != "nodeloom-sdk-go/"+SDKVersion {
			t.Errorf("expected User-Agent 'nodeloom-sdk-go/%s', got %q", SDKVersion, ua)
		}

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(BatchResponse{Accepted: len(capturedBatch.Events)})
	}))
	defer server.Close()

	q := newQueue(1000)
	tr := newTransport(server.URL, "test-key", 1)
	proc := newBatchProcessor(q, tr, 1, 100*time.Millisecond)
	proc.start()

	q.enqueue(&TelemetryEvent{
		Type:      EventTypeTraceStart,
		TraceID:   "test-id",
		AgentName: "test-agent",
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
	})

	// Wait for the flush.
	time.Sleep(300 * time.Millisecond)
	proc.stop(2 * time.Second)

	mu.Lock()
	defer mu.Unlock()

	if capturedBatch.SDKVersion != SDKVersion {
		t.Errorf("expected sdk_version %q, got %q", SDKVersion, capturedBatch.SDKVersion)
	}
	if capturedBatch.SDKLanguage != SDKLanguage {
		t.Errorf("expected sdk_language %q, got %q", SDKLanguage, capturedBatch.SDKLanguage)
	}
	if len(capturedBatch.Events) == 0 {
		t.Fatal("expected at least 1 event in batch")
	}

	// Verify the event deserializes correctly.
	var event map[string]any
	if err := json.Unmarshal(capturedBatch.Events[0], &event); err != nil {
		t.Fatalf("failed to unmarshal event: %v", err)
	}
	if event["type"] != "trace_start" {
		t.Errorf("expected event type 'trace_start', got %v", event["type"])
	}
	if event["agent_name"] != "test-agent" {
		t.Errorf("expected agent_name 'test-agent', got %v", event["agent_name"])
	}
}

func TestBatchProcessor_RetryOnServerError(t *testing.T) {
	var attempts atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := attempts.Add(1)
		if n <= 2 {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(`{"error":"internal error"}`))
			return
		}
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(BatchResponse{Accepted: 1})
	}))
	defer server.Close()

	q := newQueue(1000)
	tr := newTransport(server.URL, "test-key", 3)
	proc := newBatchProcessor(q, tr, 1, 100*time.Millisecond)
	proc.start()

	q.enqueue(&TelemetryEvent{
		Type:      EventTypeEvent,
		EventName: "test",
		Level:     EventLevelInfo,
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
	})

	// Wait for retries to complete.
	time.Sleep(2 * time.Second)
	proc.stop(2 * time.Second)

	if attempts.Load() < 3 {
		t.Errorf("expected at least 3 attempts (2 retries + 1 success), got %d", attempts.Load())
	}
}

func TestBatchProcessor_NoRetryOnClientError(t *testing.T) {
	var attempts atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":"bad request"}`))
	}))
	defer server.Close()

	q := newQueue(1000)
	tr := newTransport(server.URL, "test-key", 3)
	proc := newBatchProcessor(q, tr, 1, 100*time.Millisecond)
	proc.start()

	q.enqueue(&TelemetryEvent{
		Type:      EventTypeEvent,
		EventName: "test",
		Level:     EventLevelInfo,
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
	})

	// Wait for processing.
	time.Sleep(500 * time.Millisecond)
	proc.stop(2 * time.Second)

	// Client errors (4xx) should not be retried.
	if attempts.Load() != 1 {
		t.Errorf("expected exactly 1 attempt for 4xx error (no retry), got %d", attempts.Load())
	}
}

func TestTransport_SendBatch_EmptyEvents(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("should not have made a request for empty events")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	tr := newTransport(server.URL, "test-key", 1)
	err := tr.sendBatch(context.Background(), nil)
	if err != nil {
		t.Errorf("expected no error for empty events, got %v", err)
	}
}

func TestTransport_SendBatch_ContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Simulate slow server.
		time.Sleep(5 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	tr := newTransport(server.URL, "test-key", 0)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	events := []*TelemetryEvent{{
		Type:      EventTypeEvent,
		EventName: "test",
		Level:     EventLevelInfo,
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
	}}

	err := tr.sendBatch(ctx, events)
	if err == nil {
		t.Error("expected error from cancelled context")
	}
}

func TestQueue_BoundedCapacity(t *testing.T) {
	q := newQueue(5)

	// Fill the queue.
	for i := 0; i < 5; i++ {
		q.enqueue(&TelemetryEvent{Type: EventTypeEvent, EventName: "test"})
	}

	if q.len() != 5 {
		t.Errorf("expected queue length 5, got %d", q.len())
	}

	// This should be silently dropped (queue is full).
	q.enqueue(&TelemetryEvent{Type: EventTypeEvent, EventName: "dropped"})

	if q.len() != 5 {
		t.Errorf("expected queue length to remain 5 after overflow, got %d", q.len())
	}
}

func TestQueue_Drain(t *testing.T) {
	q := newQueue(100)

	for i := 0; i < 10; i++ {
		q.enqueue(&TelemetryEvent{Type: EventTypeEvent, EventName: "test"})
	}

	// Drain up to 5.
	events := q.drain(5)
	if len(events) != 5 {
		t.Errorf("expected 5 drained events, got %d", len(events))
	}

	// 5 should remain.
	if q.len() != 5 {
		t.Errorf("expected 5 remaining events, got %d", q.len())
	}

	// Drain remaining.
	events = q.drain(100)
	if len(events) != 5 {
		t.Errorf("expected 5 drained events, got %d", len(events))
	}

	// Queue should be empty.
	if q.len() != 0 {
		t.Errorf("expected empty queue, got %d", q.len())
	}
}

func TestQueue_DrainEmpty(t *testing.T) {
	q := newQueue(100)

	events := q.drain(10)
	if len(events) != 0 {
		t.Errorf("expected 0 events from empty queue, got %d", len(events))
	}
}

func TestEventWireFormat_TraceStart(t *testing.T) {
	event := &TelemetryEvent{
		Type:         EventTypeTraceStart,
		TraceID:      "trace-123",
		AgentName:    "my-agent",
		AgentVersion: "1.0.0",
		Environment:  "production",
		Input:        map[string]any{"query": "hello"},
		Timestamp:    "2024-01-01T00:00:00Z",
	}

	data, err := event.marshalJSON()
	if err != nil {
		t.Fatalf("failed to marshal trace_start: %v", err)
	}

	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if m["type"] != "trace_start" {
		t.Errorf("expected type 'trace_start', got %v", m["type"])
	}
	if m["trace_id"] != "trace-123" {
		t.Errorf("expected trace_id 'trace-123', got %v", m["trace_id"])
	}
	if m["agent_name"] != "my-agent" {
		t.Errorf("expected agent_name 'my-agent', got %v", m["agent_name"])
	}

	// Verify no span-specific fields are present.
	if _, ok := m["span_id"]; ok {
		t.Error("trace_start should not contain span_id")
	}
	if _, ok := m["status"]; ok {
		t.Error("trace_start should not contain status")
	}
}

func TestEventWireFormat_Span(t *testing.T) {
	event := &TelemetryEvent{
		Type:         EventTypeSpan,
		TraceID:      "trace-123",
		SpanID:       "span-456",
		ParentSpanID: "",
		Name:         "llm-call",
		SpanType:     SpanTypeLLM,
		SpanStatus:   StatusSuccess,
		SpanInput:    map[string]any{"prompt": "hi"},
		SpanOutput:   map[string]any{"response": "hello"},
		TokenUsage:   &TokenUsage{PromptTokens: 10, CompletionTokens: 20, TotalTokens: 30, Model: "gpt-4o"},
		Timestamp:    "2024-01-01T00:00:00Z",
		EndTimestamp:  "2024-01-01T00:00:01Z",
	}

	data, err := event.marshalJSON()
	if err != nil {
		t.Fatalf("failed to marshal span: %v", err)
	}

	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if m["type"] != "span" {
		t.Errorf("expected type 'span', got %v", m["type"])
	}
	if m["span_id"] != "span-456" {
		t.Errorf("expected span_id 'span-456', got %v", m["span_id"])
	}
	if m["span_type"] != "llm" {
		t.Errorf("expected span_type 'llm', got %v", m["span_type"])
	}
	if m["status"] != "success" {
		t.Errorf("expected status 'success', got %v", m["status"])
	}

	tu, ok := m["token_usage"].(map[string]any)
	if !ok {
		t.Fatal("expected token_usage to be a map")
	}
	if tu["model"] != "gpt-4o" {
		t.Errorf("expected model 'gpt-4o', got %v", tu["model"])
	}
}

func TestEventWireFormat_TraceEnd(t *testing.T) {
	event := &TelemetryEvent{
		Type:      EventTypeTraceEnd,
		TraceID:   "trace-123",
		Status:    StatusSuccess,
		Output:    map[string]any{"result": "done"},
		Timestamp: "2024-01-01T00:00:00Z",
	}

	data, err := event.marshalJSON()
	if err != nil {
		t.Fatalf("failed to marshal trace_end: %v", err)
	}

	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if m["type"] != "trace_end" {
		t.Errorf("expected type 'trace_end', got %v", m["type"])
	}
	if m["status"] != "success" {
		t.Errorf("expected status 'success', got %v", m["status"])
	}
}

func TestEventWireFormat_Event(t *testing.T) {
	event := &TelemetryEvent{
		Type:      EventTypeEvent,
		TraceID:   "",
		EventName: "guardrail_triggered",
		Level:     EventLevelWarn,
		Data:      map[string]any{"reason": "content_filter"},
		Timestamp: "2024-01-01T00:00:00Z",
	}

	data, err := event.marshalJSON()
	if err != nil {
		t.Fatalf("failed to marshal event: %v", err)
	}

	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if m["type"] != "event" {
		t.Errorf("expected type 'event', got %v", m["type"])
	}
	if m["name"] != "guardrail_triggered" {
		t.Errorf("expected name 'guardrail_triggered', got %v", m["name"])
	}
	if m["level"] != "warn" {
		t.Errorf("expected level 'warn', got %v", m["level"])
	}
}

func TestEndToEnd_Integration(t *testing.T) {
	var received []BatchRequest
	var mu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var batch BatchRequest
		json.NewDecoder(r.Body).Decode(&batch)
		mu.Lock()
		received = append(received, batch)
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(BatchResponse{Accepted: len(batch.Events)})
	}))
	defer server.Close()

	client := New("sdk_test_key",
		WithEndpoint(server.URL),
		WithBatchSize(10),
		WithFlushInterval(50*time.Millisecond),
		WithEnvironment("test"),
		WithAgentVersion("1.0.0"),
	)

	trace := client.Trace("test-agent", WithInput(map[string]any{"query": "hello"}))

	span := trace.Span("llm-call", SpanTypeLLM,
		WithSpanInput(map[string]any{"prompt": "hello"}),
	)
	span.SetOutput(map[string]any{"response": "hi there"})
	span.SetTokenUsage(10, 20, "gpt-4o")
	span.End()

	trace.Event("guardrail_check", EventLevelInfo, map[string]any{"passed": true})
	trace.End(StatusSuccess, WithOutput(map[string]any{"result": "done"}))

	client.Close()

	mu.Lock()
	defer mu.Unlock()

	totalEvents := 0
	for _, batch := range received {
		totalEvents += len(batch.Events)
		if batch.SDKVersion != SDKVersion {
			t.Errorf("expected sdk_version %q, got %q", SDKVersion, batch.SDKVersion)
		}
		if batch.SDKLanguage != SDKLanguage {
			t.Errorf("expected sdk_language %q, got %q", SDKLanguage, batch.SDKLanguage)
		}
	}

	// We expect 4 events: trace_start, span, event, trace_end.
	if totalEvents < 4 {
		t.Errorf("expected at least 4 events (trace_start, span, event, trace_end), got %d", totalEvents)
	}
}
