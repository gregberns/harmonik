package hook

// sessionstore.go — pure last-received-wins dedup for outcome_emitted plus the
// agent_ready callback registry (CHB-025 / CHB-013).
//
// The daemon maintains a per-session store keyed by (run_id, claude_session_id).
// Each receipt of an outcome_emitted RelayEnvelope REPLACES the previous
// "current outcome" (last-received-wins). On Wait-return the work loop consults
// LatestOutcome to choose the terminal event.
//
// Stale arrivals (session already closed) are rejected with an unknown_session
// typed-error ack per §6.2 CHB-025.
//
// # Goroutine safety
//
// SessionStore uses a sync.Mutex; all methods are safe for concurrent use from
// multiple goroutines (one per accepted socket connection). The store is cheap:
// one mutex per instance, contended only by hook-relay one-shot connections
// which are rare relative to the bead dispatch rate.
//
// Spec refs:
//   - specs/claude-hook-bridge.md §4.10 CHB-025
//   - specs/claude-hook-bridge.md §6.2 HookRelayAck

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
)

// RelayEnvelope is the NDJSON envelope sent by the harmonik hook-relay
// subprocess to the daemon socket per §6.1 HookRelayMessage.
//
// The relay writes one envelope per invocation; the daemon's socket acceptor
// reads it, routes it by (run_id, claude_session_id), and returns a RelayAck
// per §6.2.
type RelayEnvelope struct {
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

	// EmittedAtNs is a monotonic-relative nanosecond timestamp recorded by the
	// relay at invocation start, for observability only.
	EmittedAtNs int64 `json:"emitted_at_ns"`

	// Payload is the message-type-specific payload (e.g., the WORK_COMPLETE /
	// REVIEWER_VERDICT / FAILURE_SIGNAL body from CHB-013).
	Payload json.RawMessage `json:"payload"`
}

// RelayAck is the ACK / typed-error the daemon returns to the relay per §6.2.
type RelayAck struct {
	// Status is one of: "ok", "daemon_not_ready", "bad_envelope", "unknown_session".
	Status string `json:"status"`

	// Reason is a human-readable explanation; omitted when Status == "ok".
	Reason string `json:"reason,omitempty"`
}

// sessionKey is the compound identifier for a hook session. It matches the
// (run_id, claude_session_id) pair in each RelayEnvelope.
type sessionKey struct {
	runID           string
	claudeSessionID string
}

// session tracks the dedup state for a single open handler session window.
type session struct {
	// handlerSessionID binds this registry generation to the concrete handler
	// launch that owns it. An empty value is reserved for the legacy two-key
	// compatibility API; new SessionRegistration callers are always bound.
	handlerSessionID string

	// latestOutcome is the payload from the most recently received
	// outcome_emitted message. Replaced on every arrival (last-received-wins
	// per CHB-025). nil until the first outcome_emitted is received.
	latestOutcome *json.RawMessage

	// agentReadyCallback is called (non-nil only) when an agent_ready relay
	// message is received for this session. Set by SetAgentReadyCallback after
	// RegisterHookSession; used by the work loop to forward relay-synthesized
	// agent_ready into the per-run event tap so waitAgentReady can observe it.
	agentReadyCallback func()

	// readyFired latches that an agent_ready relay message arrived for this
	// session, regardless of whether a callback was installed yet. The daemon
	// registers the session + launches the handler subprocess BEFORE installing
	// the callback (workloop: RegisterHookSession + Launch, then
	// SetAgentReadyCallback). If the subprocess's SessionStart hook fires
	// agent_ready in that window, notifyAgentReady would find agentReadyCallback
	// nil and silently drop the signal — waitAgentReady then blocks until timeout
	// (lost-wakeup, dispatch stall). This edge-latch closes the window:
	// notifyAgentReady always sets readyFired, and SetAgentReadyCallback replays
	// the callback immediately when readyFired was already set (H13).
	readyFired bool

	// readyDelivered records that the one genuine SessionStart-ready signal has
	// been admitted to the callback. Relay retries may deliver agent_ready more
	// than once, but a session registration exposes it exactly once.
	readyDelivered bool

	// nonterminalSealed prevents new ready-callback admission. Terminal hook
	// outcome delivery deliberately remains open until Close: phase teardown
	// seals nonterminal callbacks before it consumes the final Stop-hook state.
	nonterminalSealed bool

	// callbacksInFlight counts ready callbacks admitted under SessionStore.mu
	// but not yet returned. callbacksDrained is created on the zero-to-one
	// transition and closed on the one-to-zero transition. SealNonterminal
	// first seals admission under the same mutex, then waits on this channel,
	// making unregister a quiescence barrier without calling user code while
	// holding the store mutex.
	callbacksInFlight int
	callbacksDrained  chan struct{}

	// closed makes SessionRegistration.Close idempotent even when multiple
	// handles for the same idempotently-registered session close concurrently.
	closed bool
}

// ErrSessionRegistrationSealed is returned when a caller attempts to install a
// nonterminal callback after the registration's admission barrier has sealed.
var ErrSessionRegistrationSealed = errors.New("hook: session registration is sealed")

// ErrSessionIdentityInvalid reports an incomplete identity passed to the
// generation-bound registration API.
var ErrSessionIdentityInvalid = errors.New("hook: session identity is incomplete")

// ErrSessionRegistrationConflict reports an attempt to bind an already-open
// (run_id, claude_session_id) window to a different handler launch.
var ErrSessionRegistrationConflict = errors.New("hook: session registration conflicts with open generation")

// SessionRegistration is the ownership handle for one hook-relay session
// generation. It provides quiescent nonterminal callback sealing and
// concurrency-safe, idempotent close while the legacy SessionStore methods
// remain available as compatibility wrappers.
//
// A registration is generation-specific: if the same identifiers are
// registered again after Close, an old handle cannot mutate or close the new
// session.
type SessionRegistration struct {
	store *SessionStore
	key   sessionKey
	sess  *session
}

// SessionStore is the registry of active hook-relay sessions.
//
// One entry exists per active handler subprocess (open session window). Entries
// are created by RegisterHookSession (at handler launch) and removed by
// CloseHookSession (when cmd.Wait() returns).
//
// notifyChans holds per-session broadcast channels for WaitForOutcome callers.
// Each call to WaitForOutcome registers a buffered chan struct{} here; when
// updateOutcome records the first outcome for a session it closes every channel
// for that key (fan-out broadcast). Late arrivals (after close) drain and return
// immediately because the store check is done under the mutex before entering
// the select.
type SessionStore struct {
	mu          sync.Mutex
	sessions    map[sessionKey]*session
	notifyChans map[sessionKey][]chan struct{}
}

// NewSessionStore constructs an empty SessionStore.
func NewSessionStore() *SessionStore {
	return &SessionStore{
		sessions:    make(map[sessionKey]*session),
		notifyChans: make(map[sessionKey][]chan struct{}),
	}
}

// RegisterSession opens a generation-bound session window and returns its
// ownership handle. handlerSessionID is the concrete per-launch identity carried
// by RelayEnvelope.HandlerSessionID; binding it prevents a delayed envelope from
// an earlier launch that reused (runID, claudeSessionID) from entering the new
// generation.
//
// Registering the same complete identity again returns another handle to the
// same generation. Registering a different handlerSessionID while the two-key
// window is open returns ErrSessionRegistrationConflict.
func (s *SessionStore) RegisterSession(runID, claudeSessionID, handlerSessionID string) (*SessionRegistration, error) {
	if runID == "" || claudeSessionID == "" || handlerSessionID == "" {
		return nil, ErrSessionIdentityInvalid
	}
	key := sessionKey{runID: runID, claudeSessionID: claudeSessionID}
	s.mu.Lock()
	sess := s.sessions[key]
	if sess == nil {
		sess = &session{handlerSessionID: handlerSessionID}
		s.sessions[key] = sess
	} else if sess.handlerSessionID == "" {
		// Migration bridge: a bound owner may adopt only a pristine legacy
		// window. Once that unbound window has admitted a callback or relay
		// state, its generation cannot be proven and adoption must fail closed.
		if sess.latestOutcome != nil || sess.agentReadyCallback != nil ||
			sess.readyFired || sess.readyDelivered || sess.nonterminalSealed ||
			sess.callbacksInFlight != 0 {
			s.mu.Unlock()
			return nil, ErrSessionRegistrationConflict
		}
		sess.handlerSessionID = handlerSessionID
	} else if sess.handlerSessionID != handlerSessionID {
		s.mu.Unlock()
		return nil, ErrSessionRegistrationConflict
	}
	s.mu.Unlock()
	return &SessionRegistration{store: s, key: key, sess: sess}, nil
}

// RegisterHookSession opens the session window for (runID, claudeSessionID).
//
// Called from the work loop goroutine BEFORE dispatching the handler subprocess.
// Registering an already-open key is a no-op (idempotent; covers daemon-restart
// edge cases where the key may be re-registered under the same identifiers).
func (s *SessionStore) RegisterHookSession(runID, claudeSessionID string) {
	key := sessionKey{runID: runID, claudeSessionID: claudeSessionID}
	s.mu.Lock()
	if s.sessions[key] == nil {
		// handlerSessionID intentionally remains empty: this wrapper preserves
		// the legacy two-key routing contract until its callers adopt
		// RegisterSession with the concrete launch identity.
		s.sessions[key] = &session{}
	}
	s.mu.Unlock()
}

// currentRegistration returns a generation-specific handle for the currently
// registered key, or nil when the session is unknown.
func (s *SessionStore) currentRegistration(runID, claudeSessionID string) *SessionRegistration {
	key := sessionKey{runID: runID, claudeSessionID: claudeSessionID}
	s.mu.Lock()
	sess := s.sessions[key]
	s.mu.Unlock()
	if sess == nil {
		return nil
	}
	return &SessionRegistration{store: s, key: key, sess: sess}
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
func (s *SessionStore) SetAgentReadyCallback(runID, claudeSessionID string, cb func()) {
	if registration := s.currentRegistration(runID, claudeSessionID); registration != nil {
		// Compatibility surface: legacy callers have no error return. New
		// phase-owned code uses SessionRegistration directly and observes a
		// post-seal admission rejection.
		_ = registration.SetAgentReadyCallback(cb)
	}
}

// CloseHookSession removes the session window from the registry. Called from
// the work loop goroutine when cmd.Wait() returns.
//
// Any outcome_emitted relay message that arrives AFTER CloseHookSession returns
// will observe a missing key and return unknown_session per CHB-025.
//
// Any WaitForOutcome caller still blocked on this key is woken: the session is
// gone, so on wake it observes a missing key and returns (nil, nil) rather than
// blocking until ctx cancellation.
func (s *SessionStore) CloseHookSession(runID, claudeSessionID string) {
	if registration := s.currentRegistration(runID, claudeSessionID); registration != nil {
		// The legacy surface has neither a context nor an error return. Preserve
		// it as a synchronous compatibility wrapper; phase-owned callers use
		// SessionRegistration.Close with their teardown bound.
		_ = registration.Close(context.Background())
	}
}

// SetAgentReadyCallback installs the callback for the registration's one
// genuine SessionStart-ready signal. If ready arrived before installation, the
// latched signal is delivered synchronously. Relay retries and replacement
// callbacks cannot cause a second delivery.
//
// The method returns ErrSessionRegistrationSealed once SealNonterminal or Close
// has begun. A callback admitted before sealing may still be running; the seal
// barrier waits for it.
func (r *SessionRegistration) SetAgentReadyCallback(cb func()) error {
	s := r.store
	s.mu.Lock()
	if s.sessions[r.key] != r.sess || r.sess.closed || r.sess.nonterminalSealed {
		s.mu.Unlock()
		return ErrSessionRegistrationSealed
	}
	r.sess.agentReadyCallback = cb
	admitted := s.admitReadyCallbackLocked(r.sess)
	s.mu.Unlock()
	s.runReadyCallback(r.sess, admitted)
	return nil
}

// SealNonterminal prevents new ready-callback admission and waits for every
// callback admitted before the seal to return. Terminal outcome delivery and
// reads remain available until Close.
//
// A context error leaves the registration sealed but still open, so a later
// call can resume waiting without losing terminal hook state.
func (r *SessionRegistration) SealNonterminal(ctx context.Context) error {
	s := r.store
	s.mu.Lock()
	if r.sess.closed || s.sessions[r.key] != r.sess {
		s.mu.Unlock()
		return nil
	}
	r.sess.nonterminalSealed = true
	r.sess.agentReadyCallback = nil
	drained := r.sess.callbacksDrained
	s.mu.Unlock()

	if drained == nil {
		return nil
	}
	select {
	case <-drained:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Close seals nonterminal callback admission, waits for admitted callbacks,
// then removes this session generation and wakes outcome waiters. It is safe
// for repeated or concurrent calls.
//
// If ctx expires while an admitted callback is still running, Close returns
// ctx.Err and leaves the sealed registration open so terminal outcome state
// remains consumable and a later Close can finish.
func (r *SessionRegistration) Close(ctx context.Context) error {
	if err := r.SealNonterminal(ctx); err != nil {
		return err
	}

	s := r.store
	s.mu.Lock()
	defer s.mu.Unlock()
	if r.sess.closed || s.sessions[r.key] != r.sess {
		return nil
	}
	r.sess.closed = true
	delete(s.sessions, r.key)
	for _, ch := range s.notifyChans[r.key] {
		close(ch)
	}
	delete(s.notifyChans, r.key)
	return nil
}

// admitReadyCallbackLocked admits the exactly-once ready callback when the
// genuine signal and callback are both present. SessionStore.mu must be held.
func (s *SessionStore) admitReadyCallbackLocked(sess *session) func() {
	if sess.nonterminalSealed || sess.closed || sess.readyDelivered ||
		!sess.readyFired || sess.agentReadyCallback == nil {
		return nil
	}
	sess.readyDelivered = true
	if sess.callbacksInFlight == 0 {
		sess.callbacksDrained = make(chan struct{})
	}
	sess.callbacksInFlight++
	return sess.agentReadyCallback
}

// runReadyCallback invokes admitted user code outside SessionStore.mu and
// always accounts for its completion, including when the callback panics.
func (s *SessionStore) runReadyCallback(sess *session, cb func()) {
	if cb == nil {
		return
	}
	defer s.finishReadyCallback(sess)
	cb()
}

// finishReadyCallback records one callback completion. SessionStore.mu is
// acquired here because callbacks run outside it.
func (s *SessionStore) finishReadyCallback(sess *session) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess.callbacksInFlight--
	if sess.callbacksInFlight == 0 {
		close(sess.callbacksDrained)
		sess.callbacksDrained = nil
	}
}

// LatestOutcome returns the most recently received outcome_emitted payload for
// (runID, claudeSessionID), or nil if no outcome_emitted has been received yet.
//
// Called from the work loop goroutine AFTER cmd.Wait() returns (before
// CloseHookSession) to read the last-received-wins outcome.
func (s *SessionStore) LatestOutcome(runID, claudeSessionID string) *json.RawMessage {
	key := sessionKey{runID: runID, claudeSessionID: claudeSessionID}
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
func (s *SessionStore) WaitForOutcome(ctx context.Context, runID, claudeSessionID string) (json.RawMessage, error) {
	key := sessionKey{runID: runID, claudeSessionID: claudeSessionID}

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

// updateOutcome replaces the session's latestOutcome with payload
// (last-received-wins).
//
// Returns (true, "") when the update succeeds.
// Returns (false, reason) when the session is unknown (already closed and
// removed, or never registered).
//
// When this is the FIRST outcome recorded for the session, all channels in
// notifyChans[key] are closed (broadcast), waking any concurrent WaitForOutcome
// callers. Subsequent calls update latestOutcome but do not re-signal (waiters
// have already been released; they read the latest value under the mutex after
// wake-up).
func (s *SessionStore) updateOutcome(runID, claudeSessionID, handlerSessionID string, payload json.RawMessage) (ok bool, ackStatus string) {
	key := sessionKey{runID: runID, claudeSessionID: claudeSessionID}
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, exists := s.sessions[key]
	if !exists || sess == nil || !handlerGenerationMatches(sess, handlerSessionID) {
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

// handlerGenerationMatches reports whether an envelope belongs to sess. Empty
// session binding is the intentional legacy compatibility mode; bound owner
// registrations require an exact per-launch handler identity.
func handlerGenerationMatches(sess *session, handlerSessionID string) bool {
	return sess.handlerSessionID == "" || sess.handlerSessionID == handlerSessionID
}

// notifyAgentReady invokes the agentReadyCallback for (runID, claudeSessionID)
// if one has been registered. The callback is invoked outside the mutex to
// avoid lock inversion; it is read under the mutex then called after unlock.
//
// known reports that the two-key session window exists; matched reports that
// HandlerSessionID belongs to its bound generation. A missing legacy window
// remains the historical no-op, while a mismatch on an existing bound window
// is rejected by Dispatch as unknown_session.
func (s *SessionStore) notifyAgentReady(runID, claudeSessionID, handlerSessionID string) (known, matched bool) {
	key := sessionKey{runID: runID, claudeSessionID: claudeSessionID}
	s.mu.Lock()
	var (
		sess *session
		cb   func()
	)
	if current, ok := s.sessions[key]; ok && current != nil {
		known = true
		sess = current
		if handlerGenerationMatches(sess, handlerSessionID) {
			matched = true
			// H13: always latch that ready fired, so a callback installed LATER
			// (SetAgentReadyCallback) can replay the signal instead of losing it.
			sess.readyFired = true
			cb = s.admitReadyCallbackLocked(sess)
		}
	}
	s.mu.Unlock()
	s.runReadyCallback(sess, cb)
	return known, matched
}

// Dispatch handles an incoming RelayEnvelope and returns the RelayAck to be
// serialised back to the relay connection per §6.2 HookRelayAck.
//
// This is the PURE routing surface: it owns outcome_emitted (last-received-wins
// dedup) and agent_ready (callback fan-out), plus bad-envelope validation. Any
// other message type is accepted with {status: "ok"} and no state change.
//
// The daemon shell (internal/daemon/hookrelay_chb025.go) intercepts the
// rate-limit message types (agent_rate_limited / agent_rate_limit_cleared)
// BEFORE delegating here, because emitting agent_rate_limit_status requires a
// bus emitter, a UUID parse, and a clock read — all impure effects this package
// does not carry.
//
// Routing rules:
//   - type == "" or missing run_id/claude_session_id → {status: "bad_envelope"}.
//   - type == "outcome_emitted" → update latestOutcome; unknown_session if the
//     session is closed or never opened.
//   - type == "agent_ready" → fire the registered agent_ready callback.
//   - any other type → {status: "ok"} (accepted, no state update).
func (s *SessionStore) Dispatch(env RelayEnvelope) RelayAck {
	if env.Type == "" {
		return RelayAck{Status: "bad_envelope", Reason: "missing type field"}
	}
	if env.RunID == "" || env.ClaudeSessionID == "" {
		return RelayAck{Status: "bad_envelope", Reason: "missing run_id or claude_session_id"}
	}

	switch env.Type {
	case "outcome_emitted":
		ok, status := s.updateOutcome(env.RunID, env.ClaudeSessionID, env.HandlerSessionID, env.Payload)
		if !ok {
			return RelayAck{
				Status: status,
				Reason: "session window closed or not registered for run_id=" + env.RunID + " claude_session_id=" + env.ClaudeSessionID,
			}
		}
		return RelayAck{Status: "ok"}

	case "agent_ready":
		// CHB-013 (hk-p63bz): relay-synthesized agent_ready on first SessionStart
		// receipt. Forward to the per-run event tap via the registered callback
		// so waitAgentReady can observe it (HC-039 / HC-041).
		if known, matched := s.notifyAgentReady(env.RunID, env.ClaudeSessionID, env.HandlerSessionID); known && !matched {
			return RelayAck{
				Status: "unknown_session",
				Reason: "handler session generation does not match open window for run_id=" + env.RunID +
					" claude_session_id=" + env.ClaudeSessionID,
			}
		}
		return RelayAck{Status: "ok"}

	default:
		// Any other known or future message type is accepted without state update.
		return RelayAck{Status: "ok"}
	}
}
