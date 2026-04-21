package nodeloom

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

// Control payload constants. Mirrors the backend enum values.
const (
	HaltSourceNone  = "none"
	HaltSourceAgent = "agent"
	HaltSourceTeam  = "team"

	RequireGuardrailsOff  = "OFF"
	RequireGuardrailsSoft = "SOFT"
	RequireGuardrailsHard = "HARD"
)

// ErrAgentHalted is returned by SDK operations when the targeted agent has been
// halted by the NodeLoom backend (per-agent or team-wide).
//
// Use errors.Is to detect halt without needing a type assertion. Use
// errors.As to recover the AgentHaltedError detail (reason, source, revision).
var ErrAgentHalted = errors.New("agent halted by NodeLoom control plane")

// AgentHaltedError carries the resolved control payload alongside ErrAgentHalted
// so callers can log the reason and source.
type AgentHaltedError struct {
	AgentName string
	Reason    string
	Source    string
	Revision  int64
}

func (e *AgentHaltedError) Error() string {
	base := fmt.Sprintf("agent %q halted (source=%s, revision=%d)", e.AgentName, e.Source, e.Revision)
	if e.Reason != "" {
		base = base + ": " + e.Reason
	}
	return base
}

// Is reports whether target is the sentinel ErrAgentHalted, allowing callers to
// write `errors.Is(err, nodeloom.ErrAgentHalted)`.
func (e *AgentHaltedError) Is(target error) bool {
	return target == ErrAgentHalted
}

// AgentControlPayload mirrors the backend's GET /api/sdk/v1/agents/{name}/control
// response. Returned as-is from ApiClient.GetAgentControl.
type AgentControlPayload struct {
	AgentName                  string `json:"agent_name"`
	Halted                     bool   `json:"halted"`
	HaltReason                 string `json:"halt_reason,omitempty"`
	HaltSource                 string `json:"halt_source,omitempty"`
	HaltedAt                   string `json:"halted_at,omitempty"`
	Revision                   int64  `json:"revision"`
	RequireGuardrails          string `json:"require_guardrails,omitempty"`
	GuardrailSessionTTLSeconds int64  `json:"guardrail_session_ttl_seconds,omitempty"`
	PolledAt                   string `json:"polled_at,omitempty"`
}

// agentControlEntry is the per-agent state held in the registry. Internal.
type agentControlEntry struct {
	halted                       bool
	haltReason                   string
	haltSource                   string
	revision                     int64
	requireGuardrails            string
	guardrailSessionTTLSeconds   int64
	guardrailSessionID           string
	guardrailSessionExpiresAtMs  int64
}

// ControlRegistry tracks remote-control state per agent in a thread-safe way.
//
// The registry is shared between the transport (which updates it from every
// telemetry batch response), the optional poller, the API client (for
// guardrail-session caching), and the trace path (which reads it to fail-fast
// halted agents).
type ControlRegistry struct {
	mu              sync.RWMutex
	agents          map[string]*agentControlEntry
	teamHalted      bool
	teamHaltReason  string
	teamRevision    int64
}

// NewControlRegistry returns an empty registry. Most callers should not need to
// build one directly: nodeloom.New constructs and wires one for you.
func NewControlRegistry() *ControlRegistry {
	return &ControlRegistry{agents: make(map[string]*agentControlEntry)}
}

// Snapshot returns the current control payload for an agent. Unknown agents
// inherit any active team-wide halt and otherwise return a zero-value payload.
func (r *ControlRegistry) Snapshot(agentName string) AgentControlPayload {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.snapshotLocked(agentName)
}

func (r *ControlRegistry) snapshotLocked(agentName string) AgentControlPayload {
	entry := r.agents[agentName]
	out := AgentControlPayload{
		AgentName:                  agentName,
		HaltSource:                 HaltSourceNone,
		RequireGuardrails:          RequireGuardrailsOff,
		GuardrailSessionTTLSeconds: 300,
	}
	if entry != nil {
		out.Halted = entry.halted
		out.HaltReason = entry.haltReason
		out.HaltSource = entry.haltSource
		out.Revision = entry.revision
		out.RequireGuardrails = entry.requireGuardrails
		out.GuardrailSessionTTLSeconds = entry.guardrailSessionTTLSeconds
	}
	if r.teamHalted {
		out.Halted = true
		out.HaltSource = HaltSourceTeam
		out.HaltReason = r.teamHaltReason
	}
	return out
}

// IsHalted is a convenience for the trace path.
func (r *ControlRegistry) IsHalted(agentName string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.teamHalted {
		return true
	}
	entry := r.agents[agentName]
	return entry != nil && entry.halted
}

// KnownAgents returns the list of agents observed since startup. Used by the
// poller to fan out polls across all active agents.
func (r *ControlRegistry) KnownAgents() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, 0, len(r.agents))
	for name := range r.agents {
		out = append(out, name)
	}
	return out
}

// Update merges a backend control payload into the registry. Stale revisions
// are ignored to keep updates idempotent and order-tolerant.
func (r *ControlRegistry) Update(payload *AgentControlPayload) {
	if payload == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	source := payload.HaltSource
	if source == "" {
		source = HaltSourceNone
	}
	revision := payload.Revision
	halted := payload.Halted

	// Team-wide flag is only mutated by team-source payloads with fresh revisions.
	// Agent-source payloads never touch team state — this prevents a piggy-backed
	// agent response arriving late from clobbering a team halt issued after it.
	if source == HaltSourceTeam && revision >= r.teamRevision {
		r.teamHalted = halted
		r.teamHaltReason = payload.HaltReason
		r.teamRevision = revision
	}

	if payload.AgentName == "" {
		return
	}
	entry := r.agents[payload.AgentName]
	if entry == nil {
		entry = &agentControlEntry{
			haltSource:                 HaltSourceNone,
			requireGuardrails:          RequireGuardrailsOff,
			guardrailSessionTTLSeconds: 300,
		}
		r.agents[payload.AgentName] = entry
	}
	if revision < entry.revision {
		return
	}
	entry.halted = halted && source != HaltSourceTeam
	entry.haltReason = ""
	if source != HaltSourceTeam {
		entry.haltReason = payload.HaltReason
	}
	entry.haltSource = source
	entry.revision = revision
	if payload.RequireGuardrails != "" {
		entry.requireGuardrails = strings.ToUpper(payload.RequireGuardrails)
	}
	// Clamp TTL to [1, 86_400] seconds; guards against a buggy server payload.
	if ttl := payload.GuardrailSessionTTLSeconds; ttl >= 1 && ttl <= 86_400 {
		entry.guardrailSessionTTLSeconds = ttl
	}
}

// RecordGuardrailSession caches a guardrail session id returned by
// CheckGuardrails. Subsequent traces for the same agent attach the id to the
// trace_start event so HARD-mode required-guardrail enforcement can verify it.
func (r *ControlRegistry) RecordGuardrailSession(agentName, sessionID string, ttlSeconds int64) {
	r.recordGuardrailSession(agentName, sessionID, ttlSeconds, time.Now().UnixMilli())
}

func (r *ControlRegistry) recordGuardrailSession(agentName, sessionID string, ttlSeconds, nowMs int64) {
	if agentName == "" || sessionID == "" {
		return
	}
	if ttlSeconds <= 0 {
		ttlSeconds = 1
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	entry := r.agents[agentName]
	if entry == nil {
		entry = &agentControlEntry{
			haltSource:                 HaltSourceNone,
			requireGuardrails:          RequireGuardrailsOff,
			guardrailSessionTTLSeconds: ttlSeconds,
		}
		r.agents[agentName] = entry
	}
	entry.guardrailSessionID = sessionID
	entry.guardrailSessionExpiresAtMs = nowMs + ttlSeconds*1000
}

// TakeGuardrailSession returns the cached guardrail session id while it is
// still within TTL. Returns the empty string when no valid session exists.
func (r *ControlRegistry) TakeGuardrailSession(agentName string) string {
	return r.takeGuardrailSession(agentName, time.Now().UnixMilli())
}

func (r *ControlRegistry) takeGuardrailSession(agentName string, nowMs int64) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	entry := r.agents[agentName]
	if entry == nil || entry.guardrailSessionID == "" {
		return ""
	}
	if nowMs >= entry.guardrailSessionExpiresAtMs {
		entry.guardrailSessionID = ""
		entry.guardrailSessionExpiresAtMs = 0
		return ""
	}
	return entry.guardrailSessionID
}

// haltError returns a populated AgentHaltedError matching the snapshot, or
// nil when the agent is not halted.
func (r *ControlRegistry) haltError(agentName string) error {
	snap := r.Snapshot(agentName)
	if !snap.Halted {
		return nil
	}
	return &AgentHaltedError{
		AgentName: agentName,
		Reason:    snap.HaltReason,
		Source:    snap.HaltSource,
		Revision:  snap.Revision,
	}
}
