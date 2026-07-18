package hook

// sessionstore_test.go — standalone unit tests for the pure CHB-025 hook-relay
// state machine (internal/hook). These prove the dedup / registry / agent_ready
// / WaitForOutcome domain is fully testable WITHOUT standing up the daemon: no
// socket, no bus, no clock, no UUID. They exercise only the exported surface of
// SessionStore.
//
// Migrated from internal/daemon/hookrelay_chb025_test.go and
// internal/daemon/hookrelay_waitforoutcome_hkgql2020_test.go (M5 slice 1).
//
// Spec refs:
//   - specs/claude-hook-bridge.md §4.10 CHB-025 (last-received-wins dedup)
//   - specs/claude-hook-bridge.md §6.1/§6.2 (HookRelayMessage / HookRelayAck)
//   - CHB-013 / HC-039 / HC-041 (relay-synthesized agent_ready callback)

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"
)

// ─────────────────────────────────────────────────────────────────────────────
// Fixtures
// ─────────────────────────────────────────────────────────────────────────────

// hookFixtureMakePayload returns a JSON payload for a WORK_COMPLETE
// outcome_emitted message with the given summary.
func hookFixtureMakePayload(t *testing.T, summary string) json.RawMessage {
	t.Helper()
	pl, err := json.Marshal(map[string]string{"kind": "WORK_COMPLETE", "summary": summary})
	if err != nil {
		t.Fatalf("hookFixtureMakePayload: marshal: %v", err)
	}
	return pl
}

// hookFixtureMakeEnvelope builds a RelayEnvelope for Dispatch calls.
func hookFixtureMakeEnvelope(runID, claudeSessionID, msgType string, payload json.RawMessage) RelayEnvelope {
	return RelayEnvelope{
		Type:             msgType,
		RunID:            runID,
		ClaudeSessionID:  claudeSessionID,
		HandlerSessionID: "handler-sess-1",
		Payload:          payload,
	}
}

// hookFixtureUnmarshal unmarshals a RawMessage into a map, failing on error.
func hookFixtureUnmarshal(t *testing.T, raw json.RawMessage) map[string]string {
	t.Helper()
	var m map[string]string
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("hookFixtureUnmarshal: %v", err)
	}
	return m
}

// ─────────────────────────────────────────────────────────────────────────────
// Dispatch: last-received-wins dedup (CHB-025)
// ─────────────────────────────────────────────────────────────────────────────

// TestSessionStore_MultiStopDedup verifies that three consecutive
// outcome_emitted arrivals for the same (run_id, claude_session_id) result in
// LatestOutcome holding only the third payload (last-received-wins per CHB-025).
func TestSessionStore_MultiStopDedup(t *testing.T) {
	t.Parallel()
	const runID = "run-multi-stop-01"
	const sessionID = "claude-sess-multi-stop-01"

	store := NewSessionStore()
	store.RegisterHookSession(runID, sessionID)

	payloads := []json.RawMessage{
		hookFixtureMakePayload(t, "first stop"),
		hookFixtureMakePayload(t, "second stop"),
		hookFixtureMakePayload(t, "third stop — authoritative"),
	}

	for i, pl := range payloads {
		env := hookFixtureMakeEnvelope(runID, sessionID, "outcome_emitted", pl)
		ack := store.Dispatch(env)
		if ack.Status != "ok" {
			t.Errorf("dispatch %d: status=%q reason=%q, want status=ok", i+1, ack.Status, ack.Reason)
		}
	}

	got := store.LatestOutcome(runID, sessionID)
	if got == nil {
		t.Fatal("LatestOutcome: got nil, want non-nil")
	}
	m := hookFixtureUnmarshal(t, *got)
	if m["summary"] != "third stop — authoritative" {
		t.Errorf("LatestOutcome summary=%q, want %q", m["summary"], "third stop — authoritative")
	}
	if m["kind"] != "WORK_COMPLETE" {
		t.Errorf("LatestOutcome kind=%q, want WORK_COMPLETE", m["kind"])
	}
}

// TestSessionStore_StalePostCloseArrival verifies that after CloseHookSession any
// subsequent outcome_emitted returns unknown_session and does not resurrect the
// (already-removed) session state.
func TestSessionStore_StalePostCloseArrival(t *testing.T) {
	t.Parallel()
	const runID = "run-stale-01"
	const sessionID = "claude-sess-stale-01"

	store := NewSessionStore()
	store.RegisterHookSession(runID, sessionID)

	live := hookFixtureMakeEnvelope(runID, sessionID, "outcome_emitted", hookFixtureMakePayload(t, "live outcome"))
	if ack := store.Dispatch(live); ack.Status != "ok" {
		t.Fatalf("live dispatch: status=%q reason=%q, want ok", ack.Status, ack.Reason)
	}

	store.CloseHookSession(runID, sessionID)

	if store.LatestOutcome(runID, sessionID) != nil {
		t.Errorf("LatestOutcome after close: got non-nil, want nil (session deleted)")
	}

	stale := hookFixtureMakeEnvelope(runID, sessionID, "outcome_emitted", hookFixtureMakePayload(t, "stale late outcome"))
	if ack := store.Dispatch(stale); ack.Status != "unknown_session" {
		t.Errorf("stale dispatch: status=%q reason=%q, want unknown_session", ack.Status, ack.Reason)
	}

	if store.LatestOutcome(runID, sessionID) != nil {
		t.Errorf("LatestOutcome after stale: got non-nil, want nil (must not resurrect)")
	}
}

// TestSessionStore_BadEnvelope verifies the pure envelope validation: missing
// type or missing run_id/claude_session_id → bad_envelope.
func TestSessionStore_BadEnvelope(t *testing.T) {
	t.Parallel()
	store := NewSessionStore()

	if ack := store.Dispatch(RelayEnvelope{Type: "", RunID: "r", ClaudeSessionID: "s"}); ack.Status != "bad_envelope" {
		t.Errorf("empty type: status=%q, want bad_envelope", ack.Status)
	}
	if ack := store.Dispatch(RelayEnvelope{Type: "outcome_emitted", RunID: "", ClaudeSessionID: "s"}); ack.Status != "bad_envelope" {
		t.Errorf("empty run_id: status=%q, want bad_envelope", ack.Status)
	}
	if ack := store.Dispatch(RelayEnvelope{Type: "outcome_emitted", RunID: "r", ClaudeSessionID: ""}); ack.Status != "bad_envelope" {
		t.Errorf("empty claude_session_id: status=%q, want bad_envelope", ack.Status)
	}
}

// TestSessionStore_UnknownType verifies that an unrecognised (but well-formed)
// message type is accepted with ok and no state change — including the
// rate-limit types, which the daemon shell intercepts before delegation and the
// pure store therefore treats as inert.
func TestSessionStore_UnknownType(t *testing.T) {
	t.Parallel()
	const runID = "run-unknown-01"
	const sessionID = "claude-sess-unknown-01"

	store := NewSessionStore()
	store.RegisterHookSession(runID, sessionID)

	for _, typ := range []string{"agent_heartbeat", "agent_rate_limited", "launch_initiated"} {
		env := hookFixtureMakeEnvelope(runID, sessionID, typ, json.RawMessage(`{"retry_after_seconds":60}`))
		if ack := store.Dispatch(env); ack.Status != "ok" {
			t.Errorf("%s dispatch: status=%q, want ok", typ, ack.Status)
		}
	}
	if store.LatestOutcome(runID, sessionID) != nil {
		t.Errorf("LatestOutcome after inert dispatches: got non-nil, want nil (no state change)")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// agent_ready callback (CHB-013 / HC-039 / HC-041)
// ─────────────────────────────────────────────────────────────────────────────

// TestSessionStore_AgentReadyDispatch_TriggersCallback verifies that an
// agent_ready envelope fires the registered agentReadyCallback.
func TestSessionStore_AgentReadyDispatch_TriggersCallback(t *testing.T) {
	t.Parallel()
	const runID = "run-agent-ready-01"
	const sessionID = "claude-sess-agent-ready-01"

	store := NewSessionStore()
	store.RegisterHookSession(runID, sessionID)

	called := make(chan struct{}, 1)
	store.SetAgentReadyCallback(runID, sessionID, func() {
		select {
		case called <- struct{}{}:
		default:
		}
	})

	env := hookFixtureMakeEnvelope(runID, sessionID, "agent_ready", nil)
	if ack := store.Dispatch(env); ack.Status != "ok" {
		t.Errorf("agent_ready dispatch: status=%q, want ok", ack.Status)
	}

	select {
	case <-called:
		// expected
	default:
		t.Error("agent_ready dispatch: callback was NOT called")
	}
}

// TestSessionStore_AgentReadyDispatch_NoCallbackIsNoOp verifies that dispatching
// agent_ready without a registered callback is a safe no-op.
func TestSessionStore_AgentReadyDispatch_NoCallbackIsNoOp(t *testing.T) {
	t.Parallel()
	const runID = "run-agent-ready-noop-01"
	const sessionID = "claude-sess-agent-ready-noop-01"

	store := NewSessionStore()
	store.RegisterHookSession(runID, sessionID)

	env := hookFixtureMakeEnvelope(runID, sessionID, "agent_ready", nil)
	if ack := store.Dispatch(env); ack.Status != "ok" {
		t.Errorf("agent_ready no-callback dispatch: status=%q, want ok", ack.Status)
	}
}

// TestSessionStore_AgentReadyLatch_ReplaysOnLateCallback verifies the H13
// edge-latch: when agent_ready fires BEFORE SetAgentReadyCallback is installed
// (the lost-wakeup window — daemon registers + launches the subprocess before
// installing the callback), the signal is latched and replayed when the callback
// is finally installed, rather than being silently dropped.
func TestSessionStore_AgentReadyLatch_ReplaysOnLateCallback(t *testing.T) {
	t.Parallel()
	const runID = "run-agent-ready-latch-01"
	const sessionID = "claude-sess-agent-ready-latch-01"

	store := NewSessionStore()
	store.RegisterHookSession(runID, sessionID)

	// agent_ready arrives in the window BEFORE any callback is installed.
	env := hookFixtureMakeEnvelope(runID, sessionID, "agent_ready", nil)
	if ack := store.Dispatch(env); ack.Status != "ok" {
		t.Fatalf("pre-callback agent_ready dispatch: status=%q, want ok", ack.Status)
	}

	// Installing the callback now must replay the latched signal immediately.
	called := make(chan struct{}, 1)
	store.SetAgentReadyCallback(runID, sessionID, func() {
		select {
		case called <- struct{}{}:
		default:
		}
	})

	select {
	case <-called:
		// expected — the latch replayed the missed agent_ready.
	default:
		t.Error("agent_ready latch: callback was NOT replayed on late install (lost-wakeup)")
	}
}

// TestSessionStore_AgentReadyLatch_ConcurrentFireAndInstall stresses the latch
// under a race between notifyAgentReady (via Dispatch) and SetAgentReadyCallback:
// exactly one delivery must occur regardless of ordering, never zero.
func TestSessionStore_AgentReadyLatch_ConcurrentFireAndInstall(t *testing.T) {
	t.Parallel()
	for i := 0; i < 200; i++ {
		store := NewSessionStore()
		const runID = "run-latch-race"
		const sessionID = "claude-sess-latch-race"
		store.RegisterHookSession(runID, sessionID)

		got := make(chan struct{}, 4)
		cb := func() { got <- struct{}{} }

		done := make(chan struct{}, 2)
		go func() {
			env := hookFixtureMakeEnvelope(runID, sessionID, "agent_ready", nil)
			store.Dispatch(env)
			done <- struct{}{}
		}()
		go func() {
			store.SetAgentReadyCallback(runID, sessionID, cb)
			done <- struct{}{}
		}()
		<-done
		<-done

		if len(got) == 0 {
			t.Fatalf("iter %d: agent_ready lost (no delivery under concurrent fire/install)", i)
		}
	}
}

// TestSessionStore_LaunchInitiated_DoesNotFireCallback verifies that
// launch_initiated (CHB-018 pre-exec precursor) does NOT fire the agent_ready
// callback (HC-041).
func TestSessionStore_LaunchInitiated_DoesNotFireCallback(t *testing.T) {
	t.Parallel()
	const runID = "run-launch-initiated-01"
	const sessionID = "claude-sess-launch-initiated-01"

	store := NewSessionStore()
	store.RegisterHookSession(runID, sessionID)

	fired := false
	store.SetAgentReadyCallback(runID, sessionID, func() { fired = true })

	env := hookFixtureMakeEnvelope(runID, sessionID, "launch_initiated", nil)
	if ack := store.Dispatch(env); ack.Status != "ok" {
		t.Errorf("launch_initiated dispatch: status=%q, want ok", ack.Status)
	}
	if fired {
		t.Error("launch_initiated dispatch: agent_ready callback fired; it MUST NOT (HC-041)")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// WaitForOutcome (hk-gql20.20)
// ─────────────────────────────────────────────────────────────────────────────

// TestWaitForOutcome_AlreadyPresent verifies that when an outcome has already
// arrived, WaitForOutcome returns the payload immediately without blocking.
func TestWaitForOutcome_AlreadyPresent(t *testing.T) {
	t.Parallel()
	const runID = "run-wait-present-01"
	const sessionID = "claude-sess-wait-present-01"

	store := NewSessionStore()
	store.RegisterHookSession(runID, sessionID)

	env := hookFixtureMakeEnvelope(runID, sessionID, "outcome_emitted", hookFixtureMakePayload(t, "pre-existing outcome"))
	if ack := store.Dispatch(env); ack.Status != "ok" {
		t.Fatalf("dispatch: status=%q reason=%q, want ok", ack.Status, ack.Reason)
	}

	done := make(chan struct{})
	var gotRaw json.RawMessage
	var gotErr error
	go func() {
		gotRaw, gotErr = store.WaitForOutcome(t.Context(), runID, sessionID)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("WaitForOutcome did not return promptly when outcome already present")
	}

	if gotErr != nil {
		t.Fatalf("WaitForOutcome: unexpected error: %v", gotErr)
	}
	if gotRaw == nil {
		t.Fatal("WaitForOutcome: got nil payload, want non-nil")
	}
	if m := hookFixtureUnmarshal(t, gotRaw); m["summary"] != "pre-existing outcome" {
		t.Errorf("summary=%q, want %q", m["summary"], "pre-existing outcome")
	}
}

// TestWaitForOutcome_BlocksThenUnblocks verifies WaitForOutcome blocks with no
// outcome present, then returns the payload once outcome_emitted arrives.
func TestWaitForOutcome_BlocksThenUnblocks(t *testing.T) {
	t.Parallel()
	const runID = "run-wait-block-01"
	const sessionID = "claude-sess-wait-block-01"

	store := NewSessionStore()
	store.RegisterHookSession(runID, sessionID)

	type result struct {
		raw json.RawMessage
		err error
	}
	resultCh := make(chan result, 1)
	go func() {
		raw, err := store.WaitForOutcome(t.Context(), runID, sessionID)
		resultCh <- result{raw: raw, err: err}
	}()

	time.Sleep(20 * time.Millisecond)

	env := hookFixtureMakeEnvelope(runID, sessionID, "outcome_emitted", hookFixtureMakePayload(t, "delayed outcome"))
	if ack := store.Dispatch(env); ack.Status != "ok" {
		t.Fatalf("dispatch: status=%q reason=%q, want ok", ack.Status, ack.Reason)
	}

	select {
	case res := <-resultCh:
		if res.err != nil {
			t.Fatalf("WaitForOutcome: unexpected error: %v", res.err)
		}
		if res.raw == nil {
			t.Fatal("WaitForOutcome: got nil payload, want non-nil")
		}
		if m := hookFixtureUnmarshal(t, res.raw); m["summary"] != "delayed outcome" {
			t.Errorf("summary=%q, want %q", m["summary"], "delayed outcome")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("WaitForOutcome did not unblock after outcome arrived")
	}
}

// TestWaitForOutcome_CtxCancel verifies WaitForOutcome returns (nil, ctx.Err())
// when ctx is cancelled before any outcome arrives.
func TestWaitForOutcome_CtxCancel(t *testing.T) {
	t.Parallel()
	const runID = "run-wait-cancel-01"
	const sessionID = "claude-sess-wait-cancel-01"

	store := NewSessionStore()
	store.RegisterHookSession(runID, sessionID)

	ctx, cancel := context.WithCancel(context.Background())

	type result struct {
		raw json.RawMessage
		err error
	}
	resultCh := make(chan result, 1)
	go func() {
		raw, err := store.WaitForOutcome(ctx, runID, sessionID)
		resultCh <- result{raw: raw, err: err}
	}()

	time.Sleep(20 * time.Millisecond)
	cancel()

	select {
	case res := <-resultCh:
		if res.raw != nil {
			t.Errorf("WaitForOutcome: got non-nil payload on cancel, want nil")
		}
		if res.err == nil {
			t.Fatal("WaitForOutcome: expected ctx.Err(), got nil")
		}
		if !errors.Is(res.err, context.Canceled) {
			t.Errorf("WaitForOutcome: err=%v, want context.Canceled", res.err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("WaitForOutcome did not return after ctx cancel")
	}
}

// TestWaitForOutcome_MultipleWaiters verifies that multiple concurrent
// WaitForOutcome callers for the same session all receive the payload when it
// arrives (fan-out broadcast).
func TestWaitForOutcome_MultipleWaiters(t *testing.T) {
	t.Parallel()
	const runID = "run-wait-multi-01"
	const sessionID = "claude-sess-wait-multi-01"
	const numWaiters = 5

	store := NewSessionStore()
	store.RegisterHookSession(runID, sessionID)

	type result struct {
		raw json.RawMessage
		err error
	}
	results := make([]chan result, numWaiters)
	for i := range results {
		results[i] = make(chan result, 1)
	}

	var wg sync.WaitGroup
	for i := 0; i < numWaiters; i++ {
		wg.Add(1)
		ch := results[i]
		go func() {
			defer wg.Done()
			raw, err := store.WaitForOutcome(t.Context(), runID, sessionID)
			ch <- result{raw: raw, err: err}
		}()
	}

	time.Sleep(30 * time.Millisecond)

	env := hookFixtureMakeEnvelope(runID, sessionID, "outcome_emitted", hookFixtureMakePayload(t, "broadcast outcome"))
	if ack := store.Dispatch(env); ack.Status != "ok" {
		t.Fatalf("dispatch: status=%q reason=%q, want ok", ack.Status, ack.Reason)
	}

	timeout := time.After(2 * time.Second)
	for i, ch := range results {
		select {
		case res := <-ch:
			if res.err != nil {
				t.Errorf("waiter %d: unexpected error: %v", i, res.err)
				continue
			}
			if res.raw == nil {
				t.Errorf("waiter %d: got nil payload, want non-nil", i)
				continue
			}
			if m := hookFixtureUnmarshal(t, res.raw); m["summary"] != "broadcast outcome" {
				t.Errorf("waiter %d: summary=%q, want %q", i, m["summary"], "broadcast outcome")
			}
		case <-timeout:
			t.Errorf("waiter %d: did not unblock within timeout", i)
		}
	}
	wg.Wait()
}
