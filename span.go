package nodeloom

import (
	"sync"
	"time"
)

// Span represents a unit of work within a trace, such as an LLM call,
// tool invocation, or retrieval operation.
type Span struct {
	mu sync.Mutex

	traceID      string
	spanID       string
	parentSpanID string
	name         string
	spanType     SpanType
	status       TraceStatus
	input        map[string]any
	output       map[string]any
	metadata     map[string]any
	errorMsg     string
	tokenUsage      *TokenUsage
	promptTemplate  string
	promptVersion   int
	startTime       time.Time
	endTime         time.Time
	ended           bool

	enqueue func(*TelemetryEvent)
}

// SetInput sets the input data for this span.
func (s *Span) SetInput(input map[string]any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.input = input
}

// SetOutput sets the output data for this span.
func (s *Span) SetOutput(output map[string]any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.output = output
}

// SetStatus sets the status of this span.
func (s *Span) SetStatus(status TraceStatus) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status = status
}

// SetTokenUsage records token consumption metrics for this span.
// This is typically used for LLM spans.
func (s *Span) SetTokenUsage(promptTokens, completionTokens int, model string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tokenUsage = &TokenUsage{
		PromptTokens:     promptTokens,
		CompletionTokens: completionTokens,
		TotalTokens:      promptTokens + completionTokens,
		Model:            model,
	}
}

// SetMetadata attaches arbitrary metadata to this span.
func (s *Span) SetMetadata(metadata map[string]any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.metadata = metadata
}

// SetPrompt records which prompt template and version was used.
func (s *Span) SetPrompt(template string, version int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.promptTemplate = template
	s.promptVersion = version
}

// Metric emits a custom metric tied to this span's trace.
func (s *Span) Metric(name string, value float64, unit string, tags map[string]string) {
	event := &TelemetryEvent{
		Type:        EventTypeMetric,
		TraceID:     s.traceID,
		MetricName:  name,
		MetricValue: value,
		MetricUnit:  unit,
		MetricTags:  tags,
		Timestamp:   time.Now().UTC().Format(time.RFC3339Nano),
	}
	s.enqueue(event)
}

// End marks this span as complete and enqueues the span event for sending.
// If no status has been set, it defaults to StatusSuccess. Calling End more
// than once has no effect.
func (s *Span) End() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.ended {
		return
	}
	s.ended = true
	s.endTime = time.Now().UTC()

	if s.status == "" {
		s.status = StatusSuccess
	}

	event := &TelemetryEvent{
		Type:           EventTypeSpan,
		TraceID:        s.traceID,
		SpanID:         s.spanID,
		ParentSpanID:   s.parentSpanID,
		Name:           s.name,
		SpanType:       s.spanType,
		SpanStatus:     s.status,
		SpanInput:      s.input,
		SpanOutput:     s.output,
		SpanError:      s.errorMsg,
		TokenUsage:     s.tokenUsage,
		PromptTemplate: s.promptTemplate,
		PromptVersion:  s.promptVersion,
		Timestamp:      s.startTime.Format(time.RFC3339Nano),
		EndTimestamp:    s.endTime.Format(time.RFC3339Nano),
	}

	s.enqueue(event)
}

// EndWithError marks this span as failed and enqueues the span event.
// It sets the status to StatusError and records the error message.
func (s *Span) EndWithError(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.ended {
		return
	}
	s.ended = true
	s.status = StatusError
	s.errorMsg = err.Error()
	s.endTime = time.Now().UTC()

	event := &TelemetryEvent{
		Type:           EventTypeSpan,
		TraceID:        s.traceID,
		SpanID:         s.spanID,
		ParentSpanID:   s.parentSpanID,
		Name:           s.name,
		SpanType:       s.spanType,
		SpanStatus:     s.status,
		SpanInput:      s.input,
		SpanOutput:     s.output,
		SpanError:      s.errorMsg,
		TokenUsage:     s.tokenUsage,
		PromptTemplate: s.promptTemplate,
		PromptVersion:  s.promptVersion,
		Timestamp:      s.startTime.Format(time.RFC3339Nano),
		EndTimestamp:    s.endTime.Format(time.RFC3339Nano),
	}

	s.enqueue(event)
}

// TraceID returns the trace ID this span belongs to.
func (s *Span) TraceID() string {
	return s.traceID
}

// SpanID returns the unique identifier for this span.
func (s *Span) SpanID() string {
	return s.spanID
}
