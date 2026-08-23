package daemon

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/handlercontract"
	"github.com/gregberns/harmonik/internal/hook"
	"github.com/gregberns/harmonik/internal/runloop"
)

type (
	hookRelayEnvelope = hook.RelayEnvelope
	hookRelayAckMsg   = hook.RelayAck
)

type hookStoreIface = runloop.HookStore

type hookSessionStore struct {
	*hook.SessionStore

	// emitter is optional; when non-nil, dispatchHookRelayEnvelope emits
	// agent_rate_limit_status bus events for agent_rate_limited /
	// agent_rate_limit_cleared relay messages (hk-lqtzq).
	emitter handlercontract.EventEmitter
}

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

func (s *hookSessionStore) dispatchHookRelayEnvelope(env hookRelayEnvelope) hookRelayAckMsg {
	if env.Type == "" {
		return hookRelayAckMsg{Status: "bad_envelope", Reason: "missing type field"}
	}
	if env.RunID == "" || env.ClaudeSessionID == "" {
		return hookRelayAckMsg{Status: "bad_envelope", Reason: "missing run_id or claude_session_id"}
	}

	switch env.Type {
	case "agent_rate_limited":
		s.emitRateLimitStatus(env, core.AgentRateLimitStatusActive)
		return hookRelayAckMsg{Status: "ok"}

	case "agent_rate_limit_cleared":
		s.emitRateLimitStatus(env, core.AgentRateLimitStatusCleared)
		return hookRelayAckMsg{Status: "ok"}

	default:
		return s.Dispatch(env)
	}
}

func (s *hookSessionStore) emitRateLimitStatus(env hookRelayEnvelope, status core.AgentRateLimitStatus) {
	if s.emitter == nil {
		return
	}
	runUUID, parseErr := uuid.Parse(env.RunID)
	if parseErr != nil {
		return // RunID is required and must be a valid UUID per AgentRateLimitStatusPayload.Valid()
	}

	var relayPl struct {
		RetryAfterSeconds *int `json:"retry_after_seconds,omitempty"`
	}
	if unmarshalErr := json.Unmarshal(env.Payload, &relayPl); unmarshalErr != nil {
		relayPl.RetryAfterSeconds = nil
	}

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
	if emitErr := s.emitter.EmitWithRunID(context.Background(), core.RunID(runUUID), core.EventTypeAgentRateLimitStatus, plBytes); emitErr != nil {
		slog.WarnContext(context.Background(), "daemon: emit agent_rate_limit_status failed", "err", emitErr, "run_id", runUUID.String())
	}
}
