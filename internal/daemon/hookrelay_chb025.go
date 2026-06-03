package daemon

// hookrelay_chb025.go — daemon-side last-received-wins dedup for outcome_emitted.
//
// Implements CHB-025: the daemon maintains a per-session store keyed by
// (run_id, claude_session_id). Each receipt of an outcome_emitted hookRelayMessage
// REPLACES the previous "current outcome" (last-received-wins). On Wait-return,
// the work loop consults latestOutcome to choose the terminal event.
//
// Stale arrivals (session already closed) are rejected with an unknown_session
// typed-error response per §6.2 CHB-025.
//
// # Goroutine safety
//
// hookSessionStore uses a sync.Mutex; all methods are safe for concurrent use
// from multiple goroutine (one per accepted socket connection). The store is
// cheap: one mutex per daemon instance, contended only by hook-relay one-shot
// connections which are rare relative to the bead dispatch rate.
//
// # Watcher-goroutine discipline (CHB-025)
//
// The spec notes "the watcher goroutine owns all writes". In the MVH
// implementation the "watcher" is the socket acceptor goroutine for each
// incoming relay connection; UpdateOutcome is called from that goroutine.
// No additional locking surface beyond hookSessionStore.mu is needed.
//
// Spec refs:
//   - specs/claude-hook-bridge.md §4.10 CHB-025
//   - specs/claude-hook-bridge.md §6.2 HookRelayAck

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/handlercontract"
)

// hookRelayEnvelope is the NDJSON envelope sent by the harmonik hook-relay
// subprocess to the daemon socket per §6.1 HookRelayMessage.
//
// The relay writes one envelope per invocation; the daemon's socket acceptor
// reads it, routes it by (run_id, claude_session_id), and returns a
// hookRelayAckMsg per §6.2.
type hookRelayEnvelope struct {
	// Type is the progress-stream message type (e.g., "outcome_emitted").
	Type string `json:"type"`

	// RunID is the run's stable identifier (= HARMONIK_RUN_ID env var).
	RunID string `json:"run_id"`

	// ClaudeSessionID is the Claude Code session identifier minted by the
	// handler subprocess (= HARMONIK_CLAUDE_SESSION_ID).
	ClaudeSessionID string `json:"claude_session_id"`

	// HandlerSessionID correlates the relay message to the handler's own
	// progress-stream emissions (= HARMONIK_HANDLER_SESSION_ID).
	HandlerSessionID string `json:"handler_session_id"`

	// EmittedAtNs is a monotonic-relative nanosecond timestamp recorded
	// by the relay at invocation start, for observability only.
	EmittedAtNs int64 `json:"emitted_at_ns"`

	// Payload is the message-type-specific payload (e.g., the WORK_COMPLETE /
	// REVIEWER_VERDICT / FAILURE_SIGNAL body from CHB-013).
	Payload json.RawMessage `json:"payload"`
}

// hookRelayAckMsg is the ACK / typed-error the daemon returns to the relay per §6.2.
type hookRelayAckMsg struct {
	// Status is one of: "ok", "daemon_not_ready", "bad_envelope", "unknown_session".
	Status string `json:"status"`

	// Reason is a human-readable explanation; omitted when Status == "ok".
	Reason string `json:"reason,omitempty"`
}

// hookSessionKey is the compound identifier for a daemon-managed hook session.
// It matches the (run_id, claude_session_id) pair in each hookRelayEnvelope.
type hookSessionKey struct {
	runID           string
	claudeSessionID string
}

// hookSession tracks the dedup state for a single open handler session window.
type hookSession struct {
	// latestOutcome is the payload from the most recently received outcome_emitted
	// message. Replaced on every arrival (last-received-wins per CHB-025).
	// nil until the first outcome_emitted is received.
	latestOutcome *json.RawMessage

	// closed is set to true by CloseHookSession when cmd.Wait() returns.
	// Any incoming relay message targeting a closed session returns unknown_session.
	closed bool

	// agentReadyCallback is called (non-nil only) when an agent_ready relay
	// message is received for this session.  Set by SetAgentReadyCallback after
	// RegisterHookSession; used by the work loop to forward relay-synthesized
	// agent_ready into the per-run event tap so waitAgentReady can observe it.
	agentReadyCallback func()
}

// hookStoreIface is the interface over hook-session state used by the work loop
// and waitWithSocketGrace.  The concrete type *hookSessionStore implements it;
// tests may supply a lightweight stub via workLoopDeps to avoid the 3-second
// stopHookGrace window (see synthHookStore in export_test.go).
//
// Bead ref: hk-kqdpf.1.
type hookStoreIface interface {
	RegisterHookSession(runID, claudeSessionID string)
	CloseHookSession(runID, claudeSessionID string)
	LatestOutcome(runID, claudeSessionID string) *json.RawMessage
	WaitForOutcome(ctx context.Context, runID, claudeSessionID string) (json.RawMessage, error)

	// SetAgentReadyCallback registers a callback that is called (once) when the
	// daemon socket receives an agent_ready relay message for (runID,
	// claudeSessionID).  The callback is invoked from the socket-acceptor goroutine
	// and MUST be non-blocking.  Used by the work loop to forward relay-synthesized
	// agent_ready into the per-run event tap so waitAgentReady can observe it
	// (CHB-013 / HC-039).
	SetAgentReadyCallback(runID, claudeSessionID string, cb func())
}

// hookSessionStore is the daemon-wide registry of active hook-relay sessions.
//
// One entry exists per active handler subprocess (open session window). Entries
// are created by RegisterHookSession (at handler launch) and removed by
// CloseHookSession (when cmd.Wait() returns).
//
// notifyChans holds per-session broadcast channels for WaitForOutcome callers.
// Each call to WaitForOutcome registers a buffered chan struct{} here; when
// updateOutcome records the first outcome for a session it closes every channel
// for that key (fan-out broadcast). Late arrivals (after close) drain and
// return immediately because the store check is done under the mutex before
// entering the select.
type hookSessionStore struct {
	mu          sync.Mutex
	sessions    map[hookSessionKey]*hookSession
	notifyChans map[hookSessionKey][]chan struct{}

	// emitter is optional; when non-nil, dispatchHookRelayEnvelope emits
	// agent_rate_limit_status bus events for agent_rate_limited /
	// agent_rate_limit_cleared relay messages (hk-lqtzq).
	emitter handlercontract.EventEmitter
}

// newHookSessionStore constructs an empty hookSessionStore.
func newHookSessionStore() *hookSessionStore {
	return &hookSessionStore{
		sessions:    make(map[hookSessionKey]*hookSession),
		notifyChans: make(map[hookSessionKey][]chan struct{}),
	}
}

// SetEmitter wires the daemon bus emitter so the store can forward
// agent_rate_limit_status events.  Must be called before beads are dispatched.
func (s *hookSessionStore) SetEmitter(e handlercontract.EventEmitter) {
	s.emitter = e
}

// RegisterHookSession opens the session window for (runID, claudeSessionID).
//
// Called from the work loop goroutine BEFORE dispatching the handler subprocess.
// Registering an already-open key is a no-op (idempotent; covers daemon-restart
// edge cases where the key may be re-registered under the same identifiers).
func (s *hookSessionStore) RegisterHookSession(runID, claudeSessionID string) {
	key := hookSessionKey{runID: runID, claudeSessionID: claudeSessionID}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.sessions[key]; !exists {
		s.sessions[key] = &hookSession{}
	}
}

// SetAgentReadyCallback sets a callback on the session identified by (runID,
// claudeSessionID). The callback is called from the socket-acceptor goroutine
// when an agent_ready relay message is received for that session.
//
// Called from the work loop goroutine after RegisterHookSession and after the
// per-run event tap has been created (so the callback can safely forward to the
// tap channel).
//
// If the session is not registered the call is a no-op.
func (s *hookSessionStore) SetAgentReadyCallback(runID, claudeSessionID string, cb func()) {
	key := hookSessionKey{runID: runID, claudeSessionID: claudeSessionID}
	s.mu.Lock()
	defer s.mu.Unlock()
	if sess, ok := s.sessions[key]; ok && sess != nil {
		sess.agentReadyCallback = cb
	}
}

// CloseHookSession marks the session window as closed and removes it from the
// registry. Called from the work loop goroutine when cmd.Wait() returns.
//
// Any outcome_emitted relay message that arrives AFTER CloseHookSession returns
// will observe a missing key and return unknown_session per CHB-025.
func (s *hookSessionStore) CloseHookSession(runID, claudeSessionID string) {
	key := hookSessionKey{runID: runID, claudeSessionID: claudeSessionID}
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, key)
}

// LatestOutcome returns the most recently received outcome_emitted payload for
// (runID, claudeSessionID), or nil if no outcome_emitted has been received yet.
//
// Called from the work loop goroutine AFTER cmd.Wait() returns (before
// CloseHookSession) to read the last-received-wins outcome.
func (s *hookSessionStore) LatestOutcome(runID, claudeSessionID string) *json.RawMessage {
	key := hookSessionKey{runID: runID, claudeSessionID: claudeSessionID}
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.sessions[key]
	if !ok || sess == nil {
		return nil
	}
	return sess.latestOutcome
}

// WaitForOutcome blocks until an outcome_emitted payload is available for the
// given (runID, claudeSessionID), then returns it. If an outcome is already
// present at call time it is returned immediately (no blocking).
//
// On ctx cancellation the method returns (nil, ctx.Err()). If the session is
// not registered it returns (nil, nil) immediately so callers can distinguish
// "session unknown" from "context cancelled".
//
// Multiple concurrent callers for the same key are each woken independently
// (fan-out close on the notify channel).
//
// Wakeup ordering: the waiter re-reads latestOutcome under the mutex after
// waking, so it always observes the current last-received-wins value.
func (s *hookSessionStore) WaitForOutcome(ctx context.Context, runID, claudeSessionID string) (json.RawMessage, error) {
	key := hookSessionKey{runID: runID, claudeSessionID: claudeSessionID}

	// Fast path: check under the mutex before allocating a channel.
	s.mu.Lock()
	sess, ok := s.sessions[key]
	if !ok || sess == nil {
		s.mu.Unlock()
		return nil, nil
	}
	if sess.latestOutcome != nil {
		result := *sess.latestOutcome
		s.mu.Unlock()
		return result, nil
	}

	// Slow path: register a per-waiter notify channel and wait outside the mutex.
	ch := make(chan struct{})
	s.notifyChans[key] = append(s.notifyChans[key], ch)
	s.mu.Unlock()

	select {
	case <-ctx.Done():
		// Remove our channel from the notify list to avoid a memory leak.
		s.mu.Lock()
		chans := s.notifyChans[key]
		filtered := chans[:0]
		for _, c := range chans {
			if c != ch {
				filtered = append(filtered, c)
			}
		}
		if len(filtered) == 0 {
			delete(s.notifyChans, key)
		} else {
			s.notifyChans[key] = filtered
		}
		s.mu.Unlock()
		return nil, ctx.Err()

	case <-ch:
		// Outcome arrived — read the current value under the mutex.
		s.mu.Lock()
		sess2, ok2 := s.sessions[key]
		var result json.RawMessage
		if ok2 && sess2 != nil && sess2.latestOutcome != nil {
			result = *sess2.latestOutcome
		}
		s.mu.Unlock()
		return result, nil
	}
}

// updateOutcome replaces the session's latestOutcome with payload (last-received-wins).
//
// Returns (true, "") when the update succeeds.
// Returns (false, reason) when the session is unknown (closed or never registered).
//
// When this is the FIRST outcome recorded for the session, all channels in
// notifyChans[key] are closed (broadcast), waking any concurrent WaitForOutcome
// callers. Subsequent calls update latestOutcome but do not re-signal (waiters
// have already been released; they read the latest value under the mutex after
// wake-up).
func (s *hookSessionStore) updateOutcome(runID, claudeSessionID string, payload json.RawMessage) (ok bool, ackStatus string) {
	key := hookSessionKey{runID: runID, claudeSessionID: claudeSessionID}
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, exists := s.sessions[key]
	if !exists || sess == nil || sess.closed {
		return false, "unknown_session"
	}
	// Last-received-wins: replace (not append) the current outcome.
	pl := make(json.RawMessage, len(payload))
	copy(pl, payload)
	firstOutcome := sess.latestOutcome == nil
	sess.latestOutcome = &pl

	// Broadcast to any WaitForOutcome callers on first outcome arrival.
	if firstOutcome {
		for _, ch := range s.notifyChans[key] {
			close(ch)
		}
		delete(s.notifyChans, key)
	}
	return true, "ok"
}

// notifyAgentReady invokes the agentReadyCallback for (runID, claudeSessionID)
// if one has been registered. The callback is invoked outside the mutex to
// avoid lock inversion; it is read under the mutex then called after unlock.
func (s *hookSessionStore) notifyAgentReady(runID, claudeSessionID string) {
	key := hookSessionKey{runID: runID, claudeSessionID: claudeSessionID}
	s.mu.Lock()
	var cb func()
	if sess, ok := s.sessions[key]; ok && sess != nil {
		cb = sess.agentReadyCallback
	}
	s.mu.Unlock()
	if cb != nil {
		cb()
	}
}

// HandleHookRelay implements HookRelayHandler. It is called from the socket
// acceptor goroutine for each hook-relay connection.
func (s *hookSessionStore) HandleHookRelay(env hookRelayEnvelope) hookRelayAckMsg {
	return s.dispatchHookRelayEnvelope(env)
}

// dispatchHookRelayEnvelope handles an incoming hookRelayEnvelope received on
// the daemon socket.
//
// It returns a hookRelayAckMsg to be serialised and written back to the relay's
// connection per §6.2 HookRelayAck.
//
// Routing rules:
//   - type == "outcome_emitted" → update latestOutcome for (run_id, claude_session_id).
//     If session is unknown (closed or never opened) → {status: "unknown_session"}.
//   - type == "" or unrecognised → {status: "bad_envelope"}.
//   - Any other known type (e.g., "agent_heartbeat") → {status: "ok"} (accepted,
//     no state update needed for non-dedup message types at MVH).
//
// CHB-027 (orphan-connection / partial write): if the relay wrote zero complete
// lines the JSON decode step in handleSocketConn returns an error BEFORE this
// function is reached; that case is handled by the socket layer, not here.
func (s *hookSessionStore) dispatchHookRelayEnvelope(env hookRelayEnvelope) hookRelayAckMsg {
	if env.Type == "" {
		return hookRelayAckMsg{Status: "bad_envelope", Reason: "missing type field"}
	}
	if env.RunID == "" || env.ClaudeSessionID == "" {
		return hookRelayAckMsg{Status: "bad_envelope", Reason: "missing run_id or claude_session_id"}
	}

	switch env.Type {
	case "outcome_emitted":
		ok, status := s.updateOutcome(env.RunID, env.ClaudeSessionID, env.Payload)
		if !ok {
			return hookRelayAckMsg{
				Status: status,
				Reason: "session window closed or not registered for run_id=" + env.RunID + " claude_session_id=" + env.ClaudeSessionID,
			}
		}
		return hookRelayAckMsg{Status: "ok"}

	case "agent_ready":
		// CHB-013 (hk-p63bz): relay-synthesized agent_ready on first SessionStart
		// receipt.  Forward to the per-run event tap via the registered callback
		// so waitAgentReady can observe it (HC-039 / HC-041).
		s.notifyAgentReady(env.RunID, env.ClaudeSessionID)
		return hookRelayAckMsg{Status: "ok"}

	case "agent_rate_limited":
		// hk-lqtzq: StopFailure{error_type: "rate_limit"} arrives here as
		// agent_rate_limited.  Forward to the bus as agent_rate_limit_status{active}
		// so HandlerPausePolicyGoroutine and bandwidthTunerBackstop can react.
		s.emitRateLimitStatus(env, core.AgentRateLimitStatusActive)
		return hookRelayAckMsg{Status: "ok"}

	case "agent_rate_limit_cleared":
		// hk-lqtzq: paired clearance event.  Forward as agent_rate_limit_status{cleared}.
		s.emitRateLimitStatus(env, core.AgentRateLimitStatusCleared)
		return hookRelayAckMsg{Status: "ok"}

	default:
		// Any other known or future message type is accepted without state update.
		return hookRelayAckMsg{Status: "ok"}
	}
}

// emitRateLimitStatus emits an agent_rate_limit_status bus event.
// No-op when emitter is nil (unit-test callers that don't wire the bus)
// or when env.RunID is not a valid UUID (payload would be invalid per spec).
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
