package nodeloom

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestControlRegistry_DefaultsForUnknownAgent(t *testing.T) {
	r := NewControlRegistry()
	snap := r.Snapshot("agent-1")
	if snap.Halted {
		t.Errorf("expected not halted, got halted")
	}
	if snap.HaltSource != HaltSourceNone {
		t.Errorf("expected source %q, got %q", HaltSourceNone, snap.HaltSource)
	}
	if snap.Revision != 0 {
		t.Errorf("expected revision 0, got %d", snap.Revision)
	}
	if snap.RequireGuardrails != RequireGuardrailsOff {
		t.Errorf("expected OFF, got %q", snap.RequireGuardrails)
	}
}

func TestControlRegistry_AgentLevelHalt(t *testing.T) {
	r := NewControlRegistry()
	r.Update(&AgentControlPayload{
		AgentName:  "agent-1",
		Halted:     true,
		HaltSource: HaltSourceAgent,
		HaltReason: "policy",
		Revision:   5,
	})
	snap := r.Snapshot("agent-1")
	if !snap.Halted {
		t.Errorf("expected halted")
	}
	if snap.HaltReason != "policy" {
		t.Errorf("expected reason 'policy', got %q", snap.HaltReason)
	}
	if snap.HaltSource != HaltSourceAgent {
		t.Errorf("expected source 'agent', got %q", snap.HaltSource)
	}
	if snap.Revision != 5 {
		t.Errorf("expected revision 5, got %d", snap.Revision)
	}
}

func TestControlRegistry_TeamHaltOverridesAllAgents(t *testing.T) {
	r := NewControlRegistry()
	r.Update(&AgentControlPayload{AgentName: "known", Halted: false, HaltSource: HaltSourceNone, Revision: 1})
	r.Update(&AgentControlPayload{
		AgentName:  "known",
		Halted:     true,
		HaltSource: HaltSourceTeam,
		HaltReason: "incident",
		Revision:   1_000_000,
	})

	for _, name := range []string{"known", "never-seen-agent"} {
		snap := r.Snapshot(name)
		if !snap.Halted {
			t.Errorf("%s: expected halted under team-wide halt", name)
		}
		if snap.HaltSource != HaltSourceTeam {
			t.Errorf("%s: expected source 'team', got %q", name, snap.HaltSource)
		}
		if snap.HaltReason != "incident" {
			t.Errorf("%s: expected reason 'incident', got %q", name, snap.HaltReason)
		}
	}
}

func TestControlRegistry_StaleRevisionIgnored(t *testing.T) {
	r := NewControlRegistry()
	r.Update(&AgentControlPayload{
		AgentName: "agent-1", Halted: true, HaltSource: HaltSourceAgent,
		HaltReason: "current", Revision: 10,
	})
	r.Update(&AgentControlPayload{
		AgentName: "agent-1", Halted: false, HaltSource: HaltSourceNone, Revision: 3,
	})
	if !r.Snapshot("agent-1").Halted {
		t.Errorf("stale revision should not have cleared halt")
	}
}

func TestControlRegistry_GuardrailSessionRoundTrip(t *testing.T) {
	r := NewControlRegistry()
	now := int64(1_000)
	r.recordGuardrailSession("agent-1", "sess-abc", 300, now)
	if got := r.takeGuardrailSession("agent-1", now+1_000); got != "sess-abc" {
		t.Errorf("expected 'sess-abc', got %q", got)
	}
}

func TestControlRegistry_GuardrailSessionExpires(t *testing.T) {
	r := NewControlRegistry()
	now := int64(1_000)
	r.recordGuardrailSession("agent-1", "sess-abc", 5, now)
	if got := r.takeGuardrailSession("agent-1", now+6_000); got != "" {
		t.Errorf("expected empty after expiry, got %q", got)
	}
}

func TestControlRegistry_BlankInputsAreNoop(t *testing.T) {
	r := NewControlRegistry()
	r.RecordGuardrailSession("", "sess", 60)
	r.RecordGuardrailSession("agent-1", "", 60)
	if got := r.TakeGuardrailSession("agent-1"); got != "" {
		t.Errorf("expected empty session id, got %q", got)
	}
}

func TestControlRegistry_ClampsNonsensicalTTL(t *testing.T) {
	r := NewControlRegistry()
	r.Update(&AgentControlPayload{AgentName: "agent-1", HaltSource: HaltSourceNone, Revision: 1,
		RequireGuardrails: "OFF", GuardrailSessionTTLSeconds: 120})
	if got := r.Snapshot("agent-1").GuardrailSessionTTLSeconds; got != 120 {
		t.Fatalf("baseline TTL wrong: got %d", got)
	}
	// Negative TTL rejected; previous value preserved.
	r.Update(&AgentControlPayload{AgentName: "agent-1", HaltSource: HaltSourceNone, Revision: 2,
		RequireGuardrails: "OFF", GuardrailSessionTTLSeconds: -5})
	if got := r.Snapshot("agent-1").GuardrailSessionTTLSeconds; got != 120 {
		t.Errorf("negative TTL should be rejected; got %d", got)
	}
	// Huge TTL rejected too.
	r.Update(&AgentControlPayload{AgentName: "agent-1", HaltSource: HaltSourceNone, Revision: 3,
		RequireGuardrails: "OFF", GuardrailSessionTTLSeconds: 1_000_000})
	if got := r.Snapshot("agent-1").GuardrailSessionTTLSeconds; got != 120 {
		t.Errorf("over-cap TTL should be rejected; got %d", got)
	}
}

func TestControlRegistry_AgentSourceDoesNotClearTeamHalt(t *testing.T) {
	r := NewControlRegistry()
	r.Update(&AgentControlPayload{
		AgentName: "agent-1", Halted: true, HaltSource: HaltSourceTeam,
		HaltReason: "incident", Revision: 1_000_000,
	})
	// Even with a higher revision, an agent-source payload must not clear team halt.
	r.Update(&AgentControlPayload{
		AgentName: "agent-1", Halted: false, HaltSource: HaltSourceAgent, Revision: 2_000_000,
	})
	snap := r.Snapshot("agent-1")
	if !snap.Halted {
		t.Errorf("team halt cleared by agent-source payload; should not be")
	}
	if snap.HaltSource != HaltSourceTeam {
		t.Errorf("source should still be 'team'; got %q", snap.HaltSource)
	}
}

func TestAgentHaltedError_IsSentinel(t *testing.T) {
	err := &AgentHaltedError{AgentName: "agent-1", Reason: "x", Source: HaltSourceAgent, Revision: 1}
	if !errors.Is(err, ErrAgentHalted) {
		t.Errorf("expected errors.Is to identify the sentinel")
	}
}

func TestClient_TraceWithControl_ReturnsHaltError(t *testing.T) {
	c := New("sdk_test", WithEndpoint("http://127.0.0.1:1"), WithControlPollInterval(0))
	defer c.Close()

	c.registry.Update(&AgentControlPayload{
		AgentName: "halted", Halted: true, HaltSource: HaltSourceAgent,
		HaltReason: "manual", Revision: 1,
	})

	trace, err := c.TraceWithControl("halted")
	if err == nil {
		t.Fatalf("expected halt error, got nil")
	}
	if !errors.Is(err, ErrAgentHalted) {
		t.Errorf("expected errors.Is(err, ErrAgentHalted) to be true; got %v", err)
	}
	if !trace.IsHalted() {
		t.Errorf("expected trace.IsHalted() to be true")
	}
}

func TestClient_Trace_AttachesGuardrailSessionID(t *testing.T) {
	// Collect every request body the transport sends. trace_start may land in
	// a different batch than trace_end depending on auto-flush timing, so we
	// scan ALL captured payloads for the one carrying the session id.
	received := make(chan []byte, 16)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		select {
		case received <- body:
		default:
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"accepted":1,"rejected":0,"errors":[]}`))
	}))
	defer srv.Close()

	c := New("sdk_test",
		WithEndpoint(srv.URL),
		WithFlushInterval(50*time.Millisecond),
		WithControlPollInterval(0),
	)

	c.registry.recordGuardrailSession("ok", "sess-xyz", 300, time.Now().UnixMilli())

	trace, err := c.TraceWithControl("ok")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	trace.End(StatusSuccess)

	// Close() forces a synchronous drain of the processor's buffer through
	// the transport — the guaranteed delivery path used by real callers
	// before process exit. Avoids racing with the auto-flush ticker.
	c.Close()

	deadline := time.After(5 * time.Second)
	for {
		select {
		case captured := <-received:
			if strings.Contains(string(captured), `"guardrail_session_id":"sess-xyz"`) {
				return // found it
			}
		case <-deadline:
			t.Fatalf("no captured payload carried the guardrail session id")
		}
	}
}

func TestTransport_PiggyBackUpdatesRegistry(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"accepted":1, "rejected":0, "errors":[],
			"control":{
				"agent_name":"agent-1",
				"halted":true,
				"halt_source":"agent",
				"halt_reason":"policy",
				"revision":7,
				"require_guardrails":"HARD",
				"guardrail_session_ttl_seconds":300
			}
		}`))
	}))
	defer srv.Close()

	c := New("sdk_test",
		WithEndpoint(srv.URL),
		WithFlushInterval(50*time.Millisecond),
		WithControlPollInterval(0),
	)
	defer c.Close()

	trace, _ := c.TraceWithControl("agent-1")
	trace.End(StatusSuccess)
	c.Flush()
	time.Sleep(150 * time.Millisecond)

	snap := c.Control().Snapshot("agent-1")
	if !snap.Halted {
		t.Errorf("expected agent to be halted via piggy-backed control")
	}
	if snap.RequireGuardrails != RequireGuardrailsHard {
		t.Errorf("expected HARD guardrails, got %q", snap.RequireGuardrails)
	}
}

func TestApiClient_GetAgentControl_UpdatesRegistry(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"agent_name":"agent-1",
			"halted":true,
			"halt_source":"team",
			"halt_reason":"incident",
			"revision":1000000,
			"require_guardrails":"OFF"
		}`))
	}))
	defer srv.Close()

	registry := NewControlRegistry()
	api := newApiClientWithRegistry("sdk_test", srv.URL, registry)
	payload, err := api.GetAgentControl("agent-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !payload.Halted {
		t.Errorf("expected payload.Halted true")
	}
	if !registry.IsHalted("any-other-agent") {
		t.Errorf("team-wide halt should propagate to unknown agents")
	}
}

func TestApiClient_CheckGuardrails_CachesSessionID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"passed":true,"violations":[],"checks":[],"redactedContent":"ok","guardrailSessionId":"sess-321"}`))
	}))
	defer srv.Close()

	registry := NewControlRegistry()
	api := newApiClientWithRegistry("sdk_test", srv.URL, registry)
	body := map[string]any{"text": "hello", "agentName": "agent-1"}
	respBody, err := api.CheckGuardrails("team-1", body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if got := parsed["guardrailSessionId"]; got != "sess-321" {
		t.Errorf("expected sessionId in response; got %v", got)
	}
	if got := registry.TakeGuardrailSession("agent-1"); got != "sess-321" {
		t.Errorf("expected registry to cache 'sess-321'; got %q", got)
	}
}

// stop() must honor its timeout so Client.Close doesn't hang when a
// tick is mid-HTTP to a slow or unreachable backend.
func TestControlPoller_StopIsBoundedByTimeout(t *testing.T) {
	blockUntil := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-blockUntil
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"agent_name":"a","halted":false,"revision":1,"require_guardrails":"OFF"}`))
	}))
	defer srv.Close()
	defer close(blockUntil)

	registry := NewControlRegistry()
	// Seed a known agent so the tick has something to poll.
	registry.Update(&AgentControlPayload{AgentName: "a", Revision: 1})
	apiFactory := func() *ApiClient {
		return newApiClientWithRegistry("sdk_test", srv.URL, registry)
	}
	poller := newControlPoller(registry, apiFactory, 10*time.Millisecond)
	poller.start()

	// Let one tick start so we're mid-blocking-request when stop fires.
	time.Sleep(50 * time.Millisecond)

	start := time.Now()
	poller.stop(100 * time.Millisecond)
	elapsed := time.Since(start)

	if elapsed > 300*time.Millisecond {
		t.Errorf("stop timeout not honored: elapsed %v > 300ms", elapsed)
	}
}

// CheckGuardrails called with an empty teamID must omit the teamId query
// param entirely so the backend can fall back to inferring team from the
// SDK token. This is the code path Anthropic integrations rely on since
// they don't know the team UUID at call time.
func TestApiClient_CheckGuardrails_EmptyTeamIDOmitsQueryParam(t *testing.T) {
	var receivedPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedPath = r.URL.RequestURI()
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"passed":true,"violations":[]}`))
	}))
	defer srv.Close()

	api := newApiClient("sdk_test", srv.URL)
	body := map[string]any{"text": "hello", "agentName": "agent-1"}
	if _, err := api.CheckGuardrails("", body); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if receivedPath != "/api/guardrails/check" {
		t.Errorf("expected path without teamId query; got %q", receivedPath)
	}
}
