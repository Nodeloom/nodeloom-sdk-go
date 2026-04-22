package nodeloom

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"time"
)

// transport handles HTTP communication with the NodeLoom ingest API.
type transport struct {
	client     *http.Client
	endpoint   string
	apiKey     string
	maxRetries int
	registry   *ControlRegistry
}

// newTransport creates a transport configured for the given endpoint and API key.
func newTransport(endpoint, apiKey string, maxRetries int) *transport {
	return newTransportWithRegistry(endpoint, apiKey, maxRetries, nil)
}

func newTransportWithRegistry(endpoint, apiKey string, maxRetries int, registry *ControlRegistry) *transport {
	return &transport{
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
		endpoint:   endpoint,
		apiKey:     apiKey,
		maxRetries: maxRetries,
		registry:   registry,
	}
}

// sendBatch sends a batch of telemetry events to the NodeLoom ingest API.
// It serializes each event to its wire format, wraps them in a BatchRequest,
// and performs the HTTP POST with retry logic.
func (t *transport) sendBatch(ctx context.Context, events []*TelemetryEvent) error {
	if len(events) == 0 {
		return nil
	}

	rawEvents := make([]json.RawMessage, 0, len(events))
	for _, e := range events {
		data, err := e.marshalJSON()
		if err != nil {
			log.Printf("[nodeloom] failed to marshal event type=%s: %v", e.Type, err)
			continue
		}
		rawEvents = append(rawEvents, data)
	}

	if len(rawEvents) == 0 {
		return nil
	}

	batch := BatchRequest{
		Events:      rawEvents,
		SDKVersion:  SDKVersion,
		SDKLanguage: SDKLanguage,
	}

	body, err := json.Marshal(batch)
	if err != nil {
		return fmt.Errorf("failed to marshal batch request: %w", err)
	}

	return t.postWithRetry(ctx, body)
}

// postWithRetry performs an HTTP POST with exponential backoff retry.
// It retries on 5xx status codes and network errors, up to maxRetries attempts.
func (t *transport) postWithRetry(ctx context.Context, body []byte) error {
	url := t.endpoint + "/api/sdk/v1/telemetry"

	var lastErr error
	for attempt := 0; attempt <= t.maxRetries; attempt++ {
		if attempt > 0 {
			backoff := time.Duration(math.Pow(2, float64(attempt-1))) * time.Second
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(backoff):
			}
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
		if err != nil {
			return fmt.Errorf("failed to create request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+t.apiKey)
		req.Header.Set("User-Agent", "nodeloom-sdk-go/"+SDKVersion)

		resp, err := t.client.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("request failed: %w", err)
			log.Printf("[nodeloom] request attempt %d/%d failed: %v", attempt+1, t.maxRetries+1, err)
			continue
		}

		// Always drain the body so the connection can be reused.
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20)) // 1 MB limit
		resp.Body.Close()

		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			t.maybeUpdateControl(respBody)
			return nil
		}

		lastErr = fmt.Errorf("server returned status %d: %s", resp.StatusCode, string(respBody))

		// Retry on server errors (5xx) and 429 (rate limit). Other 4xx are not retryable.
		if resp.StatusCode >= 400 && resp.StatusCode < 500 && resp.StatusCode != 429 {
			log.Printf("[nodeloom] non-retryable error (status %d): %s", resp.StatusCode, string(respBody))
			return lastErr
		}

		log.Printf("[nodeloom] retryable error attempt %d/%d (status %d)", attempt+1, t.maxRetries+1, resp.StatusCode)
	}

	return fmt.Errorf("all %d attempts exhausted: %w", t.maxRetries+1, lastErr)
}

// maybeUpdateControl forwards the piggy-backed control payload to the registry.
// Best-effort: parse failures are swallowed so they never break ingestion.
func (t *transport) maybeUpdateControl(body []byte) {
	if t.registry == nil || len(body) == 0 {
		return
	}
	var envelope struct {
		Control *AgentControlPayload `json:"control"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return
	}
	if envelope.Control != nil {
		t.registry.Update(envelope.Control)
	}
}
