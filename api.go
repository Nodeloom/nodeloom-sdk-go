package nodeloom

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// ApiClient provides authenticated access to the NodeLoom REST API.
// SDK tokens can now authenticate against all /api/** endpoints.
type ApiClient struct {
	httpClient *http.Client
	endpoint   string
	apiKey     string
}

// newApiClient creates a new API client with the given configuration.
func newApiClient(apiKey, endpoint string) *ApiClient {
	return &ApiClient{
		httpClient: &http.Client{Timeout: 30 * time.Second},
		endpoint:   strings.TrimRight(endpoint, "/"),
		apiKey:     apiKey,
	}
}

// ApiError represents a non-2xx API response.
type ApiError struct {
	StatusCode int
	Body       string
}

func (e *ApiError) Error() string {
	return fmt.Sprintf("API error %d: %s", e.StatusCode, e.Body)
}

// Request makes an authenticated HTTP request to the NodeLoom API.
// The path should include query parameters if needed (e.g., "/api/workflows?teamId=...").
// Pass nil for body on GET/DELETE requests.
func (a *ApiClient) Request(method, path string, body any) ([]byte, error) {
	reqURL := a.endpoint + path

	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshal request body: %w", err)
		}
		bodyReader = bytes.NewReader(data)
	}

	req, err := http.NewRequest(method, reqURL, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+a.apiKey)

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &ApiError{StatusCode: resp.StatusCode, Body: string(respBody)}
	}

	return respBody, nil
}

// RequestJSON makes an authenticated request and unmarshals the response into dest.
func (a *ApiClient) RequestJSON(method, path string, body any, dest any) error {
	respBody, err := a.Request(method, path, body)
	if err != nil {
		return err
	}
	if dest != nil && len(respBody) > 0 {
		return json.Unmarshal(respBody, dest)
	}
	return nil
}

// ── Convenience Methods ─────────────────────────────────────

// ListWorkflows lists all workflows for a team.
func (a *ApiClient) ListWorkflows(teamID string) ([]byte, error) {
	return a.Request("GET", "/api/workflows?teamId="+url.QueryEscape(teamID), nil)
}

// GetWorkflow gets a workflow by ID.
func (a *ApiClient) GetWorkflow(workflowID string) ([]byte, error) {
	return a.Request("GET", "/api/workflows/"+url.QueryEscape(workflowID), nil)
}

// ExecuteWorkflow triggers a workflow execution.
func (a *ApiClient) ExecuteWorkflow(workflowID string, input map[string]any) ([]byte, error) {
	if input == nil {
		input = map[string]any{}
	}
	return a.Request("POST", "/api/workflows/"+url.QueryEscape(workflowID)+"/execute", input)
}

// ListExecutions lists executions for a team.
func (a *ApiClient) ListExecutions(teamID string, page, size int) ([]byte, error) {
	return a.Request("GET", fmt.Sprintf("/api/executions?teamId=%s&page=%d&size=%d",
		url.QueryEscape(teamID), page, size), nil)
}

// GetExecution gets an execution by ID.
func (a *ApiClient) GetExecution(executionID string) ([]byte, error) {
	return a.Request("GET", "/api/executions/"+url.QueryEscape(executionID), nil)
}

// ListCredentials lists credentials for a team.
func (a *ApiClient) ListCredentials(teamID string) ([]byte, error) {
	return a.Request("GET", "/api/credentials?teamId="+url.QueryEscape(teamID), nil)
}

// CheckGuardrails runs guardrail checks on text content.
func (a *ApiClient) CheckGuardrails(teamID string, body map[string]any) ([]byte, error) {
	return a.Request("POST", "/api/guardrails/check?teamId="+url.QueryEscape(teamID), body)
}

// ── Feedback ────────────────────────────────────────────────

// SubmitFeedback submits feedback for an execution.
func (a *ApiClient) SubmitFeedback(body map[string]any) ([]byte, error) {
	return a.Request("POST", "/api/sdk/v1/feedback", body)
}

// ListFeedback lists feedback. Pass params as query string.
func (a *ApiClient) ListFeedback(params string) ([]byte, error) {
	path := "/api/sdk/v1/feedback"
	if params != "" {
		path += "?" + params
	}
	return a.Request("GET", path, nil)
}

// ── Sentiment ───────────────────────────────────────────────

// AnalyzeSentiment analyzes text sentiment.
func (a *ApiClient) AnalyzeSentiment(body map[string]any) ([]byte, error) {
	return a.Request("POST", "/api/sdk/v1/sentiment", body)
}

// ── Costs ───────────────────────────────────────────────────

// GetCosts returns cost summary.
func (a *ApiClient) GetCosts(params string) ([]byte, error) {
	path := "/api/sdk/v1/costs"
	if params != "" {
		path += "?" + params
	}
	return a.Request("GET", path, nil)
}

// ── Webhooks ────────────────────────────────────────────────

// RegisterWebhook registers an alert webhook.
func (a *ApiClient) RegisterWebhook(body map[string]any) ([]byte, error) {
	return a.Request("POST", "/api/sdk/v1/alerts/webhooks", body)
}

// ListWebhooks lists registered webhooks.
func (a *ApiClient) ListWebhooks() ([]byte, error) {
	return a.Request("GET", "/api/sdk/v1/alerts/webhooks", nil)
}

// DeleteWebhook deletes a webhook.
func (a *ApiClient) DeleteWebhook(webhookID string) ([]byte, error) {
	return a.Request("DELETE", "/api/sdk/v1/alerts/webhooks/"+url.QueryEscape(webhookID), nil)
}

// ── Prompts ─────────────────────────────────────────────────

// CreatePrompt creates a prompt template version.
func (a *ApiClient) CreatePrompt(body map[string]any) ([]byte, error) {
	return a.Request("POST", "/api/sdk/v1/prompts", body)
}

// GetPrompt gets a prompt template by name. Pass version as query param.
func (a *ApiClient) GetPrompt(name string, params string) ([]byte, error) {
	path := "/api/sdk/v1/prompts/" + url.QueryEscape(name)
	if params != "" {
		path += "?" + params
	}
	return a.Request("GET", path, nil)
}

// ListPrompts lists all prompt templates.
func (a *ApiClient) ListPrompts() ([]byte, error) {
	return a.Request("GET", "/api/sdk/v1/prompts", nil)
}

// ── Red Team ────────────────────────────────────────────────

// StartRedTeamScan starts a red team scan.
func (a *ApiClient) StartRedTeamScan(body map[string]any) ([]byte, error) {
	return a.Request("POST", "/api/sdk/v1/redteam/scan", body)
}

// GetRedTeamScan gets scan status and results.
func (a *ApiClient) GetRedTeamScan(scanID string) ([]byte, error) {
	return a.Request("GET", "/api/sdk/v1/redteam/scan/"+url.QueryEscape(scanID), nil)
}

// ── Evaluation ──────────────────────────────────────────────

// TriggerEvaluation triggers LLM evaluation for an execution.
func (a *ApiClient) TriggerEvaluation(body map[string]any) ([]byte, error) {
	return a.Request("POST", "/api/sdk/v1/evaluate", body)
}

// ── Metrics ─────────────────────────────────────────────────

// SetCallbackUrl registers a callback URL for red team testing of an SDK agent.
func (a *ApiClient) SetCallbackUrl(agentName string, callbackUrl string) ([]byte, error) {
	return a.Request("POST", "/api/sdk/v1/agents/"+url.QueryEscape(agentName)+"/callback",
		map[string]any{"callback_url": callbackUrl})
}

// RemoveCallbackUrl removes the callback URL for an SDK agent.
func (a *ApiClient) RemoveCallbackUrl(agentName string) ([]byte, error) {
	return a.Request("DELETE", "/api/sdk/v1/agents/"+url.QueryEscape(agentName)+"/callback", nil)
}

// GetGuardrailConfig gets the current guardrail config for an SDK agent (read-only, configure via NodeLoom UI).
func (a *ApiClient) GetGuardrailConfig(agentName string) ([]byte, error) {
	return a.Request("GET", "/api/sdk/v1/agents/"+url.QueryEscape(agentName)+"/guardrails", nil)
}

// GetMetrics gets custom metrics aggregation.
func (a *ApiClient) GetMetrics(params string) ([]byte, error) {
	path := "/api/sdk/v1/metrics"
	if params != "" {
		path += "?" + params
	}
	return a.Request("GET", path, nil)
}
