package anthropic

import (
	"testing"

	nodeloom "github.com/nodeloom/nodeloom-sdk-go"
)

func TestNewHandler(t *testing.T) {
	h := New(nil, "test-agent")
	if h.agentName != "test-agent" {
		t.Errorf("expected agent name 'test-agent', got '%s'", h.agentName)
	}
	if !h.guardrails {
		t.Error("expected guardrails enabled by default")
	}
}

func TestWithGuardrails(t *testing.T) {
	h := New(nil, "test", WithGuardrails(false))
	if h.guardrails {
		t.Error("expected guardrails disabled")
	}
}

func TestExtractText(t *testing.T) {
	tests := []struct {
		name     string
		data     map[string]interface{}
		expected string
	}{
		{
			name: "content blocks",
			data: map[string]interface{}{
				"content": []interface{}{
					map[string]interface{}{"text": "Hello"},
					map[string]interface{}{"text": "World"},
				},
			},
			expected: "Hello World",
		},
		{
			name:     "string content",
			data:     map[string]interface{}{"content": "direct text"},
			expected: "direct text",
		},
		{
			name:     "no content",
			data:     map[string]interface{}{},
			expected: "",
		},
		{
			name:     "empty blocks",
			data:     map[string]interface{}{"content": []interface{}{}},
			expected: "",
		},
		{
			name: "blocks with non-text entries",
			data: map[string]interface{}{
				"content": []interface{}{
					map[string]interface{}{"text": "Hello"},
					map[string]interface{}{"type": "image"},
				},
			},
			expected: "Hello",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractText(tt.data)
			if result != tt.expected {
				t.Errorf("expected '%s', got '%s'", tt.expected, result)
			}
		})
	}
}

func TestSessionTraceCheckInputNoGuardrails(t *testing.T) {
	st := &SessionTrace{
		guardrails:  false,
		activeSpans: make(map[string]*nodeloom.Span),
	}
	result, err := st.CheckInput("hello")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	passed, ok := result["passed"].(bool)
	if !ok || !passed {
		t.Error("expected passed=true when guardrails disabled")
	}
}

func TestSessionTraceCheckOutputNoGuardrails(t *testing.T) {
	st := &SessionTrace{
		guardrails:  false,
		activeSpans: make(map[string]*nodeloom.Span),
	}
	result, err := st.CheckOutput("goodbye")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	passed, ok := result["passed"].(bool)
	if !ok || !passed {
		t.Error("expected passed=true when guardrails disabled")
	}
}

func TestSessionTraceOnEventUnknownType(t *testing.T) {
	st := &SessionTrace{
		guardrails:  false,
		activeSpans: make(map[string]*nodeloom.Span),
	}
	// Should not panic on unknown event types
	st.OnEvent("unknown.type", map[string]interface{}{})
}

func TestOnEventJSONInvalid(t *testing.T) {
	st := &SessionTrace{
		guardrails:  false,
		activeSpans: make(map[string]*nodeloom.Span),
	}
	err := st.OnEventJSON("not valid json")
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestOnEventJSONNoType(t *testing.T) {
	st := &SessionTrace{
		guardrails:  false,
		activeSpans: make(map[string]*nodeloom.Span),
	}
	err := st.OnEventJSON(`{"data": "no type field"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
