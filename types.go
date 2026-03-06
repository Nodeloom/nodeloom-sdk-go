package nodeloom

// SpanType classifies the kind of work a span represents.
type SpanType string

const (
	// SpanTypeLLM represents a call to a large language model.
	SpanTypeLLM SpanType = "llm"

	// SpanTypeTool represents a tool or function call.
	SpanTypeTool SpanType = "tool"

	// SpanTypeRetrieval represents a retrieval operation (e.g., vector search).
	SpanTypeRetrieval SpanType = "retrieval"

	// SpanTypeAgent represents a sub-agent invocation.
	SpanTypeAgent SpanType = "agent"

	// SpanTypeChain represents a chain of operations.
	SpanTypeChain SpanType = "chain"

	// SpanTypeCustom represents a user-defined span type.
	SpanTypeCustom SpanType = "custom"
)

// TraceStatus indicates the final outcome of a trace or span.
type TraceStatus string

const (
	// StatusSuccess indicates the operation completed successfully.
	StatusSuccess TraceStatus = "success"

	// StatusError indicates the operation failed.
	StatusError TraceStatus = "error"
)

// TokenUsage holds token consumption metrics for an LLM call.
type TokenUsage struct {
	PromptTokens     int    `json:"prompt_tokens"`
	CompletionTokens int    `json:"completion_tokens"`
	TotalTokens      int    `json:"total_tokens"`
	Model            string `json:"model,omitempty"`
}
