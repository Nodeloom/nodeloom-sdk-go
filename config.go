package nodeloom

import (
	"fmt"
	"log"
	"strings"
	"time"
)

const (
	// SDKVersion is the version of this SDK.
	SDKVersion = "0.9.0"

	// SDKLanguage identifies the language of this SDK in telemetry payloads.
	SDKLanguage = "go"

	defaultEndpoint      = "https://api.nodeloom.io"
	defaultBatchSize     = 100
	defaultFlushInterval = 5 * time.Second
	defaultMaxQueueSize  = 10000
	defaultEnvironment   = "production"
	defaultMaxRetries    = 3
	defaultShutdownWait  = 5 * time.Second
)

// Config holds the configuration for a NodeLoom client.
type Config struct {
	APIKey        string
	Endpoint      string
	BatchSize     int
	FlushInterval time.Duration
	MaxQueueSize  int
	Environment   string
	AgentVersion  string
	MaxRetries    int
	ShutdownWait  time.Duration
}

// String returns a human-readable representation of Config with the API key masked.
func (c *Config) String() string {
	maskedKey := "***"
	if len(c.APIKey) > 6 {
		maskedKey = c.APIKey[:6] + "***"
	}
	return fmt.Sprintf("Config{Endpoint:%s, APIKey:%s, Environment:%s}", c.Endpoint, maskedKey, c.Environment)
}

func defaultConfig(apiKey string) Config {
	return Config{
		APIKey:        apiKey,
		Endpoint:      defaultEndpoint,
		BatchSize:     defaultBatchSize,
		FlushInterval: defaultFlushInterval,
		MaxQueueSize:  defaultMaxQueueSize,
		Environment:   defaultEnvironment,
		MaxRetries:    defaultMaxRetries,
		ShutdownWait:  defaultShutdownWait,
	}
}

// Option is a functional option for configuring a NodeLoom client.
type Option func(*Config)

// WithEndpoint sets the NodeLoom API endpoint.
func WithEndpoint(endpoint string) Option {
	return func(c *Config) {
		c.Endpoint = endpoint
		if !strings.HasPrefix(endpoint, "https://") && !strings.Contains(endpoint, "localhost") && !strings.Contains(endpoint, "127.0.0.1") {
			log.Printf("[nodeloom] WARNING: endpoint '%s' does not use HTTPS. API keys will be sent in plaintext.", endpoint)
		}
	}
}

// WithBatchSize sets the maximum number of events per batch. When the queue
// accumulates this many events, the batch processor flushes immediately.
func WithBatchSize(size int) Option {
	return func(c *Config) {
		if size > 0 {
			c.BatchSize = size
		}
	}
}

// WithFlushInterval sets how often the batch processor flushes queued events,
// even if the batch size threshold has not been reached.
func WithFlushInterval(d time.Duration) Option {
	return func(c *Config) {
		if d > 0 {
			c.FlushInterval = d
		}
	}
}

// WithMaxQueueSize sets the bounded queue capacity. Events enqueued when the
// queue is full are silently dropped and logged.
func WithMaxQueueSize(size int) Option {
	return func(c *Config) {
		if size > 0 {
			c.MaxQueueSize = size
		}
	}
}

// WithEnvironment sets the environment label (e.g., "production", "staging").
func WithEnvironment(env string) Option {
	return func(c *Config) {
		c.Environment = env
	}
}

// WithAgentVersion sets the version of the instrumented agent.
func WithAgentVersion(version string) Option {
	return func(c *Config) {
		c.AgentVersion = version
	}
}

// TraceOption is a functional option applied when creating a trace.
type TraceOption func(*traceConfig)

type traceConfig struct {
	input     map[string]any
	metadata  map[string]any
	sessionID string
}

// WithInput attaches input data to a trace or span.
func WithInput(input map[string]any) TraceOption {
	return func(tc *traceConfig) {
		tc.input = input
	}
}

// WithMetadata attaches arbitrary metadata to a trace.
func WithMetadata(metadata map[string]any) TraceOption {
	return func(tc *traceConfig) {
		tc.metadata = metadata
	}
}

// WithSessionID links this trace to a conversation session.
func WithSessionID(sessionID string) TraceOption {
	return func(tc *traceConfig) {
		tc.sessionID = sessionID
	}
}

// EndOption is a functional option applied when ending a trace.
type EndOption func(*endConfig)

type endConfig struct {
	output   map[string]any
	errorMsg string
}

// WithOutput attaches output data when ending a trace.
func WithOutput(output map[string]any) EndOption {
	return func(ec *endConfig) {
		ec.output = output
	}
}

// WithError attaches an error message when ending a trace with StatusError.
func WithError(errMsg string) EndOption {
	return func(ec *endConfig) {
		ec.errorMsg = errMsg
	}
}

// SpanOption is a functional option applied when creating a span.
type SpanOption func(*spanConfig)

type spanConfig struct {
	input    map[string]any
	metadata map[string]any
}

// WithSpanInput attaches input data to a span.
func WithSpanInput(input map[string]any) SpanOption {
	return func(sc *spanConfig) {
		sc.input = input
	}
}

// WithSpanMetadata attaches arbitrary metadata to a span.
func WithSpanMetadata(metadata map[string]any) SpanOption {
	return func(sc *spanConfig) {
		sc.metadata = metadata
	}
}
