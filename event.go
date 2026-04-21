package nodeloom

import "encoding/json"

// EventType classifies the kind of telemetry event.
type EventType string

const (
	EventTypeTraceStart EventType = "trace_start"
	EventTypeTraceEnd   EventType = "trace_end"
	EventTypeSpan       EventType = "span"
	EventTypeEvent      EventType = "event"
	EventTypeMetric     EventType = "metric"
	EventTypeFeedback   EventType = "feedback"
)

// EventLevel indicates the severity of a standalone event.
type EventLevel string

const (
	EventLevelInfo  EventLevel = "info"
	EventLevelWarn  EventLevel = "warn"
	EventLevelError EventLevel = "error"
)

// TelemetryEvent is the unified envelope for all telemetry data sent to NodeLoom.
// Fields are selectively populated depending on Type.
type TelemetryEvent struct {
	// Common fields
	Type      EventType `json:"type"`
	TraceID   string    `json:"trace_id"`
	Timestamp string    `json:"timestamp"`

	// trace_start fields
	AgentName    string         `json:"agent_name,omitempty"`
	AgentVersion string         `json:"agent_version,omitempty"`
	Environment  string         `json:"environment,omitempty"`
	Input        map[string]any `json:"input,omitempty"`
	Metadata     map[string]any `json:"metadata,omitempty"`
	// GuardrailSessionID is attached to trace_start when a recent
	// CheckGuardrails call has cached a session id (HARD-mode enforcement).
	GuardrailSessionID string `json:"guardrail_session_id,omitempty"`

	// trace_end fields
	Status    TraceStatus    `json:"status,omitempty"`
	Output    map[string]any `json:"output,omitempty"`
	ErrorMsg  string         `json:"error,omitempty"`

	// span fields
	SpanID       string       `json:"span_id,omitempty"`
	ParentSpanID string       `json:"parent_span_id,omitempty"`
	Name         string       `json:"name,omitempty"`
	SpanType     SpanType     `json:"span_type,omitempty"`
	SpanStatus   TraceStatus  `json:"span_status,omitempty"`
	SpanInput    map[string]any `json:"span_input,omitempty"`
	SpanOutput   map[string]any `json:"span_output,omitempty"`
	SpanError    string       `json:"span_error,omitempty"`
	TokenUsage   *TokenUsage  `json:"token_usage,omitempty"`
	EndTimestamp string       `json:"end_timestamp,omitempty"`

	// event fields
	EventName  string         `json:"event_name,omitempty"`
	Level      EventLevel     `json:"level,omitempty"`
	Data       map[string]any `json:"data,omitempty"`

	// session tracking
	SessionID string `json:"session_id,omitempty"`

	// metric fields
	MetricName  string            `json:"metric_name,omitempty"`
	MetricValue float64           `json:"metric_value,omitempty"`
	MetricUnit  string            `json:"metric_unit,omitempty"`
	MetricTags  map[string]string `json:"metric_tags,omitempty"`

	// feedback fields
	FeedbackRating  int    `json:"rating,omitempty"`
	FeedbackComment string `json:"comment,omitempty"`

	// prompt tracking
	PromptTemplate string `json:"prompt_template,omitempty"`
	PromptVersion  int    `json:"prompt_version,omitempty"`

	// agent discovery fields
	Framework        string `json:"framework,omitempty"`
	FrameworkVersion string `json:"framework_version,omitempty"`
	SDKLanguage      string `json:"sdk_language,omitempty"`
}

// marshalJSON produces the wire-format JSON for a TelemetryEvent, using only
// the fields relevant to each event type. This keeps the payload clean and
// avoids sending empty fields.
func (e *TelemetryEvent) marshalJSON() ([]byte, error) {
	switch e.Type {
	case EventTypeTraceStart:
		return json.Marshal(traceStartWire{
			Type:               string(e.Type),
			TraceID:            e.TraceID,
			AgentName:          e.AgentName,
			AgentVersion:       e.AgentVersion,
			Environment:        e.Environment,
			SessionID:          e.SessionID,
			Input:              e.Input,
			Metadata:           e.Metadata,
			Framework:          e.Framework,
			FrameworkVersion:   e.FrameworkVersion,
			SDKLanguage:        e.SDKLanguage,
			GuardrailSessionID: e.GuardrailSessionID,
			Timestamp:          e.Timestamp,
		})
	case EventTypeTraceEnd:
		return json.Marshal(traceEndWire{
			Type:      string(e.Type),
			TraceID:   e.TraceID,
			Status:    string(e.Status),
			Output:    e.Output,
			Error:     e.ErrorMsg,
			Timestamp: e.Timestamp,
		})
	case EventTypeSpan:
		return json.Marshal(spanWire{
			Type:         string(e.Type),
			TraceID:      e.TraceID,
			SpanID:       e.SpanID,
			ParentSpanID: e.ParentSpanID,
			Name:         e.Name,
			SpanType:     string(e.SpanType),
			Status:       string(e.SpanStatus),
			Input:        e.SpanInput,
			Output:       e.SpanOutput,
			Error:        e.SpanError,
			TokenUsage:   e.TokenUsage,
			Timestamp:    e.Timestamp,
			EndTimestamp:  e.EndTimestamp,
		})
	case EventTypeEvent:
		return json.Marshal(eventWire{
			Type:      string(e.Type),
			TraceID:   e.TraceID,
			Name:      e.EventName,
			Level:     string(e.Level),
			Data:      e.Data,
			Timestamp: e.Timestamp,
		})
	case EventTypeMetric:
		return json.Marshal(metricWire{
			Type:       string(e.Type),
			TraceID:    e.TraceID,
			MetricName: e.MetricName,
			MetricValue: e.MetricValue,
			MetricUnit: e.MetricUnit,
			MetricTags: e.MetricTags,
			Timestamp:  e.Timestamp,
		})
	case EventTypeFeedback:
		return json.Marshal(feedbackWire{
			Type:    string(e.Type),
			TraceID: e.TraceID,
			Rating:  e.FeedbackRating,
			Comment: e.FeedbackComment,
			Timestamp: e.Timestamp,
		})
	default:
		return json.Marshal(e)
	}
}

// Wire format structs for clean JSON serialization.

type traceStartWire struct {
	Type               string         `json:"type"`
	TraceID            string         `json:"trace_id"`
	AgentName          string         `json:"agent_name"`
	AgentVersion       string         `json:"agent_version,omitempty"`
	Environment        string         `json:"environment,omitempty"`
	SessionID          string         `json:"session_id,omitempty"`
	Input              map[string]any `json:"input,omitempty"`
	Metadata           map[string]any `json:"metadata,omitempty"`
	Framework          string         `json:"framework,omitempty"`
	FrameworkVersion   string         `json:"framework_version,omitempty"`
	SDKLanguage        string         `json:"sdk_language,omitempty"`
	GuardrailSessionID string         `json:"guardrail_session_id,omitempty"`
	Timestamp          string         `json:"timestamp"`
}

type traceEndWire struct {
	Type      string         `json:"type"`
	TraceID   string         `json:"trace_id"`
	Status    string         `json:"status"`
	Output    map[string]any `json:"output,omitempty"`
	Error     string         `json:"error,omitempty"`
	Timestamp string         `json:"timestamp"`
}

type spanWire struct {
	Type         string         `json:"type"`
	TraceID      string         `json:"trace_id"`
	SpanID       string         `json:"span_id"`
	ParentSpanID string         `json:"parent_span_id"`
	Name         string         `json:"name"`
	SpanType     string         `json:"span_type"`
	Status       string         `json:"status"`
	Input        map[string]any `json:"input,omitempty"`
	Output       map[string]any `json:"output,omitempty"`
	Error        string         `json:"error,omitempty"`
	TokenUsage   *TokenUsage    `json:"token_usage,omitempty"`
	Timestamp    string         `json:"timestamp"`
	EndTimestamp  string        `json:"end_timestamp"`
}

type eventWire struct {
	Type      string         `json:"type"`
	TraceID   string         `json:"trace_id,omitempty"`
	Name      string         `json:"name"`
	Level     string         `json:"level"`
	Data      map[string]any `json:"data,omitempty"`
	Timestamp string         `json:"timestamp"`
}

type metricWire struct {
	Type        string            `json:"type"`
	TraceID     string            `json:"trace_id,omitempty"`
	MetricName  string            `json:"metric_name"`
	MetricValue float64           `json:"metric_value"`
	MetricUnit  string            `json:"metric_unit,omitempty"`
	MetricTags  map[string]string `json:"metric_tags,omitempty"`
	Timestamp   string            `json:"timestamp"`
}

type feedbackWire struct {
	Type      string `json:"type"`
	TraceID   string `json:"trace_id"`
	Rating    int    `json:"rating"`
	Comment   string `json:"comment,omitempty"`
	Timestamp string `json:"timestamp"`
}

// BatchRequest is the top-level payload sent to the NodeLoom ingest API.
type BatchRequest struct {
	Events     []json.RawMessage `json:"events"`
	SDKVersion string            `json:"sdk_version"`
	SDKLanguage string           `json:"sdk_language"`
}

// BatchResponse is the response from the NodeLoom ingest API.
type BatchResponse struct {
	Accepted int    `json:"accepted"`
	Error    string `json:"error,omitempty"`
}
