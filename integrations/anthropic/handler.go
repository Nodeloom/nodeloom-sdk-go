// Package anthropic provides a handler for auto-instrumenting
// Anthropic Managed Agent sessions with NodeLoom traces and spans.
package anthropic

import (
	"encoding/json"
	"sync"

	nodeloom "github.com/nodeloom/nodeloom-sdk-go"
)

// ManagedAgentsHandler instruments Anthropic Managed Agent sessions.
type ManagedAgentsHandler struct {
	client     *nodeloom.Client
	agentName  string
	guardrails bool
}

// Option configures the handler.
type Option func(*ManagedAgentsHandler)

// WithGuardrails enables or disables guardrail checks (default: true).
func WithGuardrails(enabled bool) Option {
	return func(h *ManagedAgentsHandler) {
		h.guardrails = enabled
	}
}

// New creates a new ManagedAgentsHandler.
func New(client *nodeloom.Client, agentName string, opts ...Option) *ManagedAgentsHandler {
	h := &ManagedAgentsHandler{
		client:     client,
		agentName:  agentName,
		guardrails: true,
	}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

// TraceSession creates a session trace that maps Anthropic events to NodeLoom spans.
func (h *ManagedAgentsHandler) TraceSession(sessionID string) *SessionTrace {
	trace := h.client.Trace(h.agentName, nodeloom.WithSessionID(sessionID))
	return &SessionTrace{
		trace:       trace,
		client:      h.client,
		guardrails:  h.guardrails,
		activeSpans: make(map[string]*nodeloom.Span),
	}
}

// SessionTrace represents an active session being traced.
type SessionTrace struct {
	trace       *nodeloom.Trace
	client      *nodeloom.Client
	guardrails  bool
	activeSpans map[string]*nodeloom.Span
	lastOutput  map[string]interface{}
	mu          sync.Mutex
}

// OnEvent processes an Anthropic SSE event and creates appropriate spans.
func (s *SessionTrace) OnEvent(eventType string, eventData map[string]interface{}) {
	s.mu.Lock()
	defer s.mu.Unlock()

	switch eventType {
	case "agent.message":
		s.handleMessage(eventData)
	case "agent.tool_use":
		s.handleToolUse(eventData)
	case "agent.tool_result":
		s.handleToolResult(eventData)
	case "agent.thinking":
		s.handleThinking(eventData)
	}
}

// OnEventJSON processes a raw JSON event string.
func (s *SessionTrace) OnEventJSON(eventJSON string) error {
	var data map[string]interface{}
	if err := json.Unmarshal([]byte(eventJSON), &data); err != nil {
		return err
	}
	eventType, _ := data["type"].(string)
	if eventType != "" {
		s.OnEvent(eventType, data)
	}
	return nil
}

// CheckInput runs guardrail checks on input text.
// When guardrails are disabled, returns a map with passed=true.
func (s *SessionTrace) CheckInput(text string) (map[string]interface{}, error) {
	if !s.guardrails {
		return map[string]interface{}{"passed": true, "violations": []interface{}{}}, nil
	}
	body := map[string]any{"text": text, "direction": "input"}
	resp, err := s.client.Api().CheckGuardrails("", body)
	if err != nil {
		return map[string]interface{}{"passed": true, "violations": []interface{}{}}, err
	}
	var result map[string]interface{}
	if err := json.Unmarshal(resp, &result); err != nil {
		return map[string]interface{}{"passed": true, "violations": []interface{}{}}, err
	}
	return result, nil
}

// CheckOutput runs guardrail checks on output text.
// When guardrails are disabled, returns a map with passed=true.
func (s *SessionTrace) CheckOutput(text string) (map[string]interface{}, error) {
	if !s.guardrails {
		return map[string]interface{}{"passed": true, "violations": []interface{}{}}, nil
	}
	body := map[string]any{"text": text, "direction": "output"}
	resp, err := s.client.Api().CheckGuardrails("", body)
	if err != nil {
		return map[string]interface{}{"passed": true, "violations": []interface{}{}}, err
	}
	var result map[string]interface{}
	if err := json.Unmarshal(resp, &result); err != nil {
		return map[string]interface{}{"passed": true, "violations": []interface{}{}}, err
	}
	return result, nil
}

// End finalizes the session trace with success status.
func (s *SessionTrace) End() {
	s.EndWithStatus(nodeloom.StatusSuccess)
}

// EndWithStatus finalizes the session trace with a specific status.
func (s *SessionTrace) EndWithStatus(status nodeloom.TraceStatus) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, span := range s.activeSpans {
		span.End()
	}
	s.activeSpans = make(map[string]*nodeloom.Span)

	if s.lastOutput != nil {
		s.trace.End(status, nodeloom.WithOutput(s.lastOutput))
	} else {
		s.trace.End(status)
	}
}

func (s *SessionTrace) handleMessage(data map[string]interface{}) {
	text := extractText(data)
	span := s.trace.Span("llm-response", nodeloom.SpanTypeLLM)
	if text != "" {
		span.SetOutput(map[string]interface{}{"text": text})
		s.lastOutput = map[string]interface{}{"text": text}
	}
	span.End()
}

func (s *SessionTrace) handleToolUse(data map[string]interface{}) {
	name, _ := data["name"].(string)
	if name == "" {
		name = "tool"
	}
	span := s.trace.Span(name, nodeloom.SpanTypeTool)
	if input, ok := data["input"].(map[string]interface{}); ok {
		span.SetInput(input)
	}
	if id, ok := data["id"].(string); ok && id != "" {
		s.activeSpans[id] = span
	} else {
		span.End()
	}
}

func (s *SessionTrace) handleToolResult(data map[string]interface{}) {
	toolID, _ := data["tool_use_id"].(string)
	if toolID != "" {
		if span, ok := s.activeSpans[toolID]; ok {
			text := extractText(data)
			if text != "" {
				span.SetOutput(map[string]interface{}{"result": text})
			}
			span.End()
			delete(s.activeSpans, toolID)
		}
	}
}

func (s *SessionTrace) handleThinking(data map[string]interface{}) {
	text := extractText(data)
	span := s.trace.Span("thinking", nodeloom.SpanTypeCustom)
	if text != "" {
		span.SetInput(map[string]interface{}{"thinking": text})
	}
	span.End()
}

func extractText(data map[string]interface{}) string {
	content, ok := data["content"]
	if !ok {
		return ""
	}

	if blocks, ok := content.([]interface{}); ok {
		var result string
		for _, block := range blocks {
			if m, ok := block.(map[string]interface{}); ok {
				if text, ok := m["text"].(string); ok {
					if result != "" {
						result += " "
					}
					result += text
				}
			}
		}
		return result
	}

	if s, ok := content.(string); ok {
		return s
	}

	return ""
}
