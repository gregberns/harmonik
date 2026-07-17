package daemon

// hookrelay_chb025.go — daemon shell over the pure internal/hook state machine.
//
// The pure last-received-wins dedup + agent_ready callback registry moved to
// internal/hook (M5 slice 1). What remains here is the daemon-only composition:
// the bus-emitting rate-limit routing path (agent_rate_limited /
// agent_rate_limit_cleared), which needs handlercontract.EventEmitter,
// uuid.Parse, and time.Now — all impure effects the hook package cannot carry.
//
// hookSessionStore embeds *hook.SessionStore and adds those effects. It routes
// the rate-limit types locally (option (a)) and delegates every other type to
// the pure store's Dispatch, so the socket wire protocol is byte-identical.
//
// # Watcher-goroutine discipline (CHB-025)
//
// UpdateOutcome / notifyAgentReady are called from the socket-acceptor goroutine
// for each incoming relay connection. The pure store's sync.Mutex is the only
// locking surface needed.
//
// Spec refs:
//   - specs/claude-hook-bridge.md §4.10 CHB-025
//   - specs/claude-hook-bridge.md §6.2 HookRelayAck

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/handlercontract"
	"github.com/gregberns/harmonik/internal/hook"
)

// hookRelayEnvelope / hookRelayAckMsg are the daemon-local names for the pure
// wire types. Kept as aliases so socket.go, the HookRelayHandler contract, and
// the existing daemon test suite continue to compile unchanged.
type (
	hookRelayEnvelope = hook.RelayEnvelope
	hookRelayAckMsg   = hook.RelayAck
)

// hookStoreIface is the interface over hook-session state used by the work loop
// and waitWithSocketGrace. The concrete *hookSessionStore implements it (its
// embedded *hook.SessionStore promotes every method); tests may supply a
// lightweight stub via workLoopDeps to avoid the 3-second stopHookGrace window.
//
// Bead ref: hk-kqdpf.1.
type hookStoreIface interface {
	RegisterHookSession(runID, claudeSessionID string)
	CloseHookSession(runID, claudeSessionID string)
	LatestOutcome(runID, claudeSessionID string) *json.RawMessage
	WaitForOutcome(ctx context.Context, runID, claudeSessionID string) (json.RawMessage, error)

	// SetAgentReadyCallback registers a callback that is called (once) when the
	// daemon socket receives an agent_ready relay message for (runID,
	// claudeSessionID). The callback is invoked from the socket-acceptor goroutine
	// and MUST be non-blocking. Used by the work loop to forward relay-synthesized
	// agent_ready into the per-run event tap so waitAgentReady can observe it
	// (CHB-013 / HC-039).
	SetAgentReadyCallback(runID, claudeSessionID string, cb func())
}

// hookSessionStore is the daemon-side composition of the pure hook.SessionStore
// plus the bus emitter used by the rate-limit routing path (hk-lqtzq).
//
// The embedded *hook.SessionStore promotes RegisterHookSession, CloseHookSession,
// LatestOutcome, WaitForOutcome, and SetAgentReadyCallback, so *hookSessionStore
// satisfies hookStoreIface directly.
type hookSessionStore struct {
	*hook.SessionStore

	// emitter is optional; when non-nil, dispatchHookRelayEnvelope emits
	// agent_rate_limit_status bus events for agent_rate_limited /
	// agent_rate_limit_cleared relay messages (hk-lqtzq).
	emitter handlercontract.EventEmitter
}

// newHookSessionStore constructs a daemon hook store wrapping a fresh pure
// SessionStore (no emitter wired yet; call SetEmitter before beads dispatch).
func newHookSessionStore() *hookSessionStore {
	return &hookSessionStore{SessionStore: hook.NewSessionStore()}
}

// SetEmitter wires the daemon bus emitter so the store can forward
// agent_rate_limit_status events. Must be called before beads are dispatched.
func (s *hookSessionStore) SetEmitter(e handlercontract.EventEmitter) {
	s.emitter = e
}

// HandleHookRelay implements HookRelayHandler. It is called from the socket
// acceptor goroutine for each hook-relay connection.
func (s *hookSessionStore) HandleHookRelay(env hookRelayEnvelope) hookRelayAckMsg {
	return s.dispatchHookRelayEnvelope(env)
}

// dispatchHookRelayEnvelope routes an incoming envelope. The daemon owns the
// rate-limit types (they emit onto the bus); every other type — including
// bad-envelope validation, outcome_emitted dedup, and agent_ready — is delegated
// to the pure hook.SessionStore so the ack is byte-identical to pre-extraction.
//
// The top-level envelope validation mirrors the pure store so a rate-limit
// message missing type/run_id/claude_session_id still returns bad_envelope
// exactly as before (rather than silently no-op'ing in emitRateLimitStatus).
func (s *hookSessionStore) dispatchHookRelayEnvelope(env hookRelayEnvelope) hookRelayAckMsg {
	if env.Type == "" {
		return hookRelayAckMsg{Status: "bad_envelope", Reason: "missing type field"}
	}
	if env.RunID == "" || env.ClaudeSessionID == "" {
		return hookRelayAckMsg{Status: "bad_envelope", Reason: "missing run_id or claude_session_id"}
	}

	switch env.Type {
	case "agent_rate_limited":
		// hk-lqtzq: StopFailure{error_type: "rate_limit"} arrives here as
		// agent_rate_limited. Forward to the bus as agent_rate_limit_status{active}
		// so HandlerPausePolicyGoroutine and bandwidthTunerBackstop can react.
		s.emitRateLimitStatus(env, core.AgentRateLimitStatusActive)
		return hookRelayAckMsg{Status: "ok"}

	case "agent_rate_limit_cleared":
		// hk-lqtzq: paired clearance event. Forward as agent_rate_limit_status{cleared}.
		s.emitRateLimitStatus(env, core.AgentRateLimitStatusCleared)
		return hookRelayAckMsg{Status: "ok"}

	default:
		// outcome_emitted, agent_ready, and every other type are pure — the hook
		// state machine owns them.
		return s.Dispatch(env)
	}
}

// emitRateLimitStatus emits an agent_rate_limit_status bus event.
// No-op when emitter is nil (unit-test callers that don't wire the bus) or when
// env.RunID is not a valid UUID (payload would be invalid per spec).
func (s *hookSessionStore) emitRateLimitStatus(env hookRelayEnvelope, status core.AgentRateLimitStatus) {
	if s.emitter == nil {
		return
	}
	runUUID, parseErr := uuid.Parse(env.RunID)
	if parseErr != nil {
		return // RunID is required and must be a valid UUID per AgentRateLimitStatusPayload.Valid()
	}

	// Parse retry_after_seconds from the relay payload (present only on active).
	var relayPl struct {
		RetryAfterSeconds *int `json:"retry_after_seconds,omitempty"`
	}
	_ = json.Unmarshal(env.Payload, &relayPl)

	pl := core.AgentRateLimitStatusPayload{
		RunID:             core.RunID(runUUID),
		SessionID:         core.SessionID(env.HandlerSessionID),
		Status:            status,
		RetryAfterSeconds: relayPl.RetryAfterSeconds,
		ChangedAt:         time.Now().UTC().Format("2006-01-02T15:04:05.000Z07:00"),
	}

	plBytes, marshalErr := json.Marshal(pl)
	if marshalErr != nil {
		return // non-fatal
	}
	_ = s.emitter.EmitWithRunID(context.Background(), core.RunID(runUUID), core.EventTypeAgentRateLimitStatus, plBytes)
}
