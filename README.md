# NodeLoom Go SDK

Go SDK for instrumenting AI agents and sending telemetry to [NodeLoom](https://nodeloom.io).

## Features

- Fire-and-forget telemetry that never blocks or crashes your application
- Zero external dependencies (only Go standard library)
- Automatic batching and retry with exponential backoff
- Thread-safe client with functional options pattern
- Channel-based bounded queue prevents unbounded memory growth
- Graceful shutdown with configurable timeout

## Requirements

- Go 1.21+

## Installation

```bash
go get github.com/nodeloom/nodeloom-sdk-go
```

## Quick Start

```go
package main

import (
    "github.com/nodeloom/nodeloom-sdk-go"
)

func main() {
    client := nodeloom.New("sdk_your_api_key")
    defer client.Close()

    trace := client.Trace("my-agent", nodeloom.WithInput(map[string]any{
        "query": "What is NodeLoom?",
    }))

    span := trace.Span("openai-call", nodeloom.SpanTypeLLM,
        nodeloom.WithSpanInput(map[string]any{
            "messages": []map[string]any{
                {"role": "user", "content": "What is NodeLoom?"},
            },
        }),
    )
    span.SetOutput(map[string]any{"text": "NodeLoom is an AI agent operations platform."})
    span.SetTokenUsage(15, 20, "gpt-4o")
    span.End()

    trace.End(nodeloom.StatusSuccess, nodeloom.WithOutput(map[string]any{
        "response": "NodeLoom is an AI agent operations platform.",
    }))
}
```

## Traces and Spans

A **trace** represents a single end-to-end agent execution. A **span** represents a unit of work within a trace.

### Span Types

| Type | Description |
|------|-------------|
| `SpanTypeLLM` | Language model call |
| `SpanTypeTool` | Tool or function invocation |
| `SpanTypeRetrieval` | Vector search or data retrieval |
| `SpanTypeChain` | Pipeline or chain of steps |
| `SpanTypeAgent` | Sub-agent invocation |
| `SpanTypeCustom` | User-defined operation |

### Nested Spans

```go
parentSpan := trace.Span("agent-step", nodeloom.SpanTypeAgent)
childSpan := trace.ChildSpan("llm-call", nodeloom.SpanTypeLLM, parentSpan,
    nodeloom.WithSpanInput(map[string]any{"prompt": "Hello"}),
)
childSpan.SetOutput(map[string]any{"response": "Hi there!"})
childSpan.SetTokenUsage(10, 20, "gpt-4o")
childSpan.End()
parentSpan.End()
```

### Standalone Events

```go
client.Event("guardrail_triggered", nodeloom.LevelWarn, map[string]any{
    "rule": "pii_detected",
})
```

### Error Handling

```go
span.EndWithError(errors.New("connection timeout"))

trace.End(nodeloom.StatusError, nodeloom.WithError("agent execution failed"))
```

### Span State Methods

```go
span.SetInput(map[string]any{"prompt": "Hello"})
span.SetOutput(map[string]any{"response": "Hi"})
span.SetStatus("error")
span.SetTokenUsage(150, 200, "gpt-4o")
span.SetMetadata(map[string]any{"model_version": "latest"})
```

All span methods are thread-safe.

## Configuration

```go
client := nodeloom.New("sdk_your_api_key",
    nodeloom.WithEndpoint("https://api.nodeloom.io"),
    nodeloom.WithEnvironment("production"),
    nodeloom.WithAgentVersion("1.0.0"),
    nodeloom.WithBatchSize(100),
    nodeloom.WithFlushInterval(5 * time.Second),
    nodeloom.WithMaxQueueSize(10000),
)
defer client.Close()
```

| Option | Default | Description |
|--------|---------|-------------|
| `WithEndpoint` | `https://api.nodeloom.io` | NodeLoom API base URL |
| `WithEnvironment` | `production` | Deployment environment label |
| `WithAgentVersion` | `""` | Version string for the agent |
| `WithBatchSize` | `100` | Max events per batch |
| `WithFlushInterval` | `5s` | Duration between automatic flushes |
| `WithMaxQueueSize` | `10,000` | Max queued events before dropping |

## Running Tests

```bash
go test ./...
```

## License

MIT
