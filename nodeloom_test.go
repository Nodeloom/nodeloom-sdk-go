package nodeloom

import (
	"testing"
	"time"
)

func TestNew_DefaultConfig(t *testing.T) {
	client := New("sdk_test_key")
	defer client.Close()

	if client.config.APIKey != "sdk_test_key" {
		t.Errorf("expected API key 'sdk_test_key', got %q", client.config.APIKey)
	}
	if client.config.Endpoint != defaultEndpoint {
		t.Errorf("expected endpoint %q, got %q", defaultEndpoint, client.config.Endpoint)
	}
	if client.config.BatchSize != defaultBatchSize {
		t.Errorf("expected batch size %d, got %d", defaultBatchSize, client.config.BatchSize)
	}
	if client.config.FlushInterval != defaultFlushInterval {
		t.Errorf("expected flush interval %v, got %v", defaultFlushInterval, client.config.FlushInterval)
	}
	if client.config.MaxQueueSize != defaultMaxQueueSize {
		t.Errorf("expected max queue size %d, got %d", defaultMaxQueueSize, client.config.MaxQueueSize)
	}
	if client.config.Environment != defaultEnvironment {
		t.Errorf("expected environment %q, got %q", defaultEnvironment, client.config.Environment)
	}
}

func TestNew_WithOptions(t *testing.T) {
	client := New("sdk_test_key",
		WithEndpoint("https://custom.api.io"),
		WithBatchSize(50),
		WithFlushInterval(10*time.Second),
		WithMaxQueueSize(5000),
		WithEnvironment("staging"),
		WithAgentVersion("1.2.3"),
	)
	defer client.Close()

	if client.config.Endpoint != "https://custom.api.io" {
		t.Errorf("expected endpoint 'https://custom.api.io', got %q", client.config.Endpoint)
	}
	if client.config.BatchSize != 50 {
		t.Errorf("expected batch size 50, got %d", client.config.BatchSize)
	}
	if client.config.FlushInterval != 10*time.Second {
		t.Errorf("expected flush interval 10s, got %v", client.config.FlushInterval)
	}
	if client.config.MaxQueueSize != 5000 {
		t.Errorf("expected max queue size 5000, got %d", client.config.MaxQueueSize)
	}
	if client.config.Environment != "staging" {
		t.Errorf("expected environment 'staging', got %q", client.config.Environment)
	}
	if client.config.AgentVersion != "1.2.3" {
		t.Errorf("expected agent version '1.2.3', got %q", client.config.AgentVersion)
	}
}

func TestNew_InvalidOptionValues(t *testing.T) {
	client := New("sdk_test_key",
		WithBatchSize(-1),
		WithFlushInterval(-1*time.Second),
		WithMaxQueueSize(0),
	)
	defer client.Close()

	// Invalid values should be ignored, keeping defaults.
	if client.config.BatchSize != defaultBatchSize {
		t.Errorf("expected default batch size %d for invalid input, got %d", defaultBatchSize, client.config.BatchSize)
	}
	if client.config.FlushInterval != defaultFlushInterval {
		t.Errorf("expected default flush interval for invalid input, got %v", client.config.FlushInterval)
	}
	if client.config.MaxQueueSize != defaultMaxQueueSize {
		t.Errorf("expected default max queue size for invalid input, got %d", client.config.MaxQueueSize)
	}
}

func TestClient_Close_Idempotent(t *testing.T) {
	client := New("sdk_test_key")

	// Calling Close multiple times should not panic.
	client.Close()
	client.Close()
	client.Close()
}

func TestClient_EnqueueAfterClose(t *testing.T) {
	client := New("sdk_test_key")
	client.Close()

	// Creating a trace after close should not panic; events are silently dropped.
	trace := client.Trace("test-agent")
	if trace == nil {
		t.Error("expected non-nil trace even after close")
	}
}

func TestGenerateUUID_Format(t *testing.T) {
	uuid := generateUUID()

	// UUID v4 format: 8-4-4-4-12 hex characters.
	if len(uuid) != 36 {
		t.Errorf("expected UUID length 36, got %d: %q", len(uuid), uuid)
	}

	if uuid[8] != '-' || uuid[13] != '-' || uuid[18] != '-' || uuid[23] != '-' {
		t.Errorf("UUID has incorrect dash positions: %q", uuid)
	}
}

func TestGenerateUUID_Unique(t *testing.T) {
	seen := make(map[string]bool)
	for i := 0; i < 1000; i++ {
		uuid := generateUUID()
		if seen[uuid] {
			t.Fatalf("duplicate UUID generated: %q", uuid)
		}
		seen[uuid] = true
	}
}

func TestClient_Event(t *testing.T) {
	client := New("sdk_test_key")
	defer client.Close()

	// Should not panic.
	client.Event("test_event", EventLevelInfo, map[string]any{"key": "value"})
}

func TestClient_Flush(t *testing.T) {
	client := New("sdk_test_key")
	defer client.Close()

	// Should not panic, even with no events.
	client.Flush()

	// Enqueue some events, then flush.
	client.Trace("test-agent")
	client.Flush()
}

func TestClient_FlushAfterClose(t *testing.T) {
	client := New("sdk_test_key")
	client.Close()

	// Flush after close should not panic.
	client.Flush()
}

func TestSDKConstants(t *testing.T) {
	if SDKVersion != "0.2.0" {
		t.Errorf("expected SDK version '0.2.0', got %q", SDKVersion)
	}
	if SDKLanguage != "go" {
		t.Errorf("expected SDK language 'go', got %q", SDKLanguage)
	}
}
