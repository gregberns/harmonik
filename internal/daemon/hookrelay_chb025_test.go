package daemon_test

// hookrelay_chb025_test.go — tests for CHB-025 daemon last-received-wins
// outcome_emitted dedup (hk-w5vra.11).
//
// Covers:
//   1. Multi-Stop-emission: drive 3 outcome_emitted arrivals; assert the 3rd
//      payload is the latestOutcome on the session.
//   2. Stale post-close arrival: after CloseHookSession, a late outcome_emitted
//      returns unknown_session and leaves no state.
//   3. End-to-end socket integration: verify the hookRelayEnvelope → ACK round
//      trip over a real Unix domain socket (including unknown_session on closed
//      session).
//
// Helper prefix: hookRelayFixture (implementer-protocol.md §Helper-prefix discipline;
// bead hk-w5vra.11).

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"testing"

	"github.com/gregberns/harmonik/internal/daemon"
)

// ─────────────────────────────────────────────────────────────────────────────
// Fixtures
// ─────────────────────────────────────────────────────────────────────────────

// hookRelayFixtureMakePayload returns a JSON payload for an outcome_emitted
// message with the given kind and summary, suitable for use in tests.
func hookRelayFixtureMakePayload(t *testing.T, kind, summary string) json.RawMessage {
	t.Helper()
	pl, err := json.Marshal(map[string]string{"kind": kind, "summary": summary})
	if err != nil {
		t.Fatalf("hookRelayFixtureMakePayload: marshal: %v", err)
	}
	return pl
}

// hookRelayFixtureMakeEnvelope builds a hookRelayEnvelopeExported for use in
// ExportedHookDispatch calls.
func hookRelayFixtureMakeEnvelope(runID, claudeSessionID, msgType string, payload json.RawMessage) daemon.HookRelayEnvelopeExported {
	return daemon.HookRelayEnvelopeExported{
		Type:             msgType,
		RunID:            runID,
		ClaudeSessionID:  claudeSessionID,
		HandlerSessionID: "handler-sess-1",
		Payload:          payload,
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Test 1: Multi-Stop-emission — last-received-wins
// ─────────────────────────────────────────────────────────────────────────────

// TestHookSessionStore_MultiStopDedup verifies that three consecutive
// outcome_emitted arrivals for the same (run_id, claude_session_id) result in
// latestOutcome holding only the third payload (last-received-wins per CHB-025).
func TestHookSessionStore_MultiStopDedup(t *testing.T) {
	const runID = "run-multi-stop-01"
	const sessionID = "claude-sess-multi-stop-01"

	store := daemon.ExportedNewHookSessionStore()
	daemon.ExportedHookRegister(store, runID, sessionID)

	payloads := []json.RawMessage{
		hookRelayFixtureMakePayload(t, "WORK_COMPLETE", "first stop"),
		hookRelayFixtureMakePayload(t, "WORK_COMPLETE", "second stop"),
		hookRelayFixtureMakePayload(t, "WORK_COMPLETE", "third stop — authoritative"),
	}

	// Dispatch all three outcome_emitted messages.
	for i, pl := range payloads {
		env := hookRelayFixtureMakeEnvelope(runID, sessionID, "outcome_emitted", pl)
		status, reason := daemon.ExportedHookDispatch(store, env)
		if status != "ok" {
			t.Errorf("dispatch %d: status=%q reason=%q, want status=ok", i+1, status, reason)
		}
	}

	// latestOutcome MUST be the third payload.
	got := daemon.ExportedHookLatestOutcome(store, runID, sessionID)
	if got == nil {
		t.Fatal("LatestOutcome: got nil, want non-nil")
	}

	var gotMap map[string]string
	if err := json.Unmarshal(*got, &gotMap); err != nil {
		t.Fatalf("LatestOutcome: unmarshal: %v", err)
	}
	if gotMap["summary"] != "third stop — authoritative" {
		t.Errorf("LatestOutcome summary=%q, want %q", gotMap["summary"], "third stop — authoritative")
	}
	if gotMap["kind"] != "WORK_COMPLETE" {
		t.Errorf("LatestOutcome kind=%q, want WORK_COMPLETE", gotMap["kind"])
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Test 2: Stale post-close arrival — unknown_session
// ─────────────────────────────────────────────────────────────────────────────

// TestHookSessionStore_StalePostCloseArrival verifies that after CloseHookSession
// any subsequent outcome_emitted returns unknown_session and does not affect the
// (already-removed) session state.
func TestHookSessionStore_StalePostCloseArrival(t *testing.T) {
	const runID = "run-stale-01"
	const sessionID = "claude-sess-stale-01"

	store := daemon.ExportedNewHookSessionStore()
	daemon.ExportedHookRegister(store, runID, sessionID)

	// Deliver one legitimate outcome_emitted while the session is open.
	livePayload := hookRelayFixtureMakePayload(t, "WORK_COMPLETE", "live outcome")
	env := hookRelayFixtureMakeEnvelope(runID, sessionID, "outcome_emitted", livePayload)
	status, reason := daemon.ExportedHookDispatch(store, env)
	if status != "ok" {
		t.Fatalf("live dispatch: status=%q reason=%q, want ok", status, reason)
	}

	// Close the session (simulates cmd.Wait() returning).
	daemon.ExportedHookClose(store, runID, sessionID)

	// Confirm latestOutcome is gone (session was deleted).
	latestAfterClose := daemon.ExportedHookLatestOutcome(store, runID, sessionID)
	if latestAfterClose != nil {
		t.Errorf("LatestOutcome after close: got non-nil, want nil (session deleted)")
	}

	// Stale late arrival — MUST be rejected with unknown_session.
	stalePayload := hookRelayFixtureMakePayload(t, "WORK_COMPLETE", "stale late outcome")
	staleEnv := hookRelayFixtureMakeEnvelope(runID, sessionID, "outcome_emitted", stalePayload)
	staleStatus, staleReason := daemon.ExportedHookDispatch(store, staleEnv)
	if staleStatus != "unknown_session" {
		t.Errorf("stale dispatch: status=%q reason=%q, want unknown_session", staleStatus, staleReason)
	}

	// Confirm the closed session was NOT resurrected.
	latestAfterStale := daemon.ExportedHookLatestOutcome(store, runID, sessionID)
	if latestAfterStale != nil {
		t.Errorf("LatestOutcome after stale: got non-nil, want nil (session must not be resurrected)")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Test 3: Socket round-trip — outcome_emitted ACK and unknown_session
// ─────────────────────────────────────────────────────────────────────────────

// ─────────────────────────────────────────────────────────────────────────────
// Test: agent_ready relay dispatch triggers callback (hk-1rocd / CHB-013)
// ─────────────────────────────────────────────────────────────────────────────

// TestHookSessionStore_AgentReadyDispatch_TriggersCallback verifies that when
// the daemon socket receives an agent_ready relay message, the registered
// agentReadyCallback is called.  This is the relay-synthesized agent_ready
// path (CHB-013 / HC-039) that allows waitAgentReady to observe a
// claude-originated ready signal rather than a daemon self-emission.
func TestHookSessionStore_AgentReadyDispatch_TriggersCallback(t *testing.T) {
	const runID = "run-agent-ready-01"
	const sessionID = "claude-sess-agent-ready-01"

	store := daemon.ExportedNewHookSessionStore()
	daemon.ExportedHookRegister(store, runID, sessionID)

	// Register a callback.
	called := make(chan struct{}, 1)
	daemon.ExportedHookSetAgentReadyCallback(store, runID, sessionID, func() {
		select {
		case called <- struct{}{}:
		default:
		}
	})

	// Dispatch agent_ready.
	env := hookRelayFixtureMakeEnvelope(runID, sessionID, "agent_ready", nil)
	status, _ := daemon.ExportedHookDispatch(store, env)
	if status != "ok" {
		t.Errorf("agent_ready dispatch: status=%q, want ok", status)
	}

	// Callback should have been called.
	select {
	case <-called:
		// expected
	default:
		t.Error("agent_ready dispatch: callback was NOT called; relay-synthesized agent_ready must trigger the callback")
	}
}

// TestHookSessionStore_AgentReadyDispatch_NoCallbackIsNoOp verifies that
// dispatching agent_ready without a registered callback is a safe no-op.
func TestHookSessionStore_AgentReadyDispatch_NoCallbackIsNoOp(t *testing.T) {
	const runID = "run-agent-ready-noop-01"
	const sessionID = "claude-sess-agent-ready-noop-01"

	store := daemon.ExportedNewHookSessionStore()
	daemon.ExportedHookRegister(store, runID, sessionID)

	// No callback registered — dispatch must return ok without panicking.
	env := hookRelayFixtureMakeEnvelope(runID, sessionID, "agent_ready", nil)
	status, _ := daemon.ExportedHookDispatch(store, env)
	if status != "ok" {
		t.Errorf("agent_ready no-callback dispatch: status=%q, want ok", status)
	}
}

// TestHookSessionStore_LaunchInitiated_IsNoOp verifies that launch_initiated
// messages (handler pre-exec precursor per CHB-018) are accepted without any
// state change.  They MUST NOT trigger the agent_ready callback (HC-041).
func TestHookSessionStore_LaunchInitiated_IsNoOp(t *testing.T) {
	const runID = "run-launch-initiated-01"
	const sessionID = "claude-sess-launch-initiated-01"

	store := daemon.ExportedNewHookSessionStore()
	daemon.ExportedHookRegister(store, runID, sessionID)

	// Register a callback — it must NOT fire on launch_initiated.
	callbackFired := false
	daemon.ExportedHookSetAgentReadyCallback(store, runID, sessionID, func() {
		callbackFired = true
	})

	env := hookRelayFixtureMakeEnvelope(runID, sessionID, "launch_initiated", nil)
	status, _ := daemon.ExportedHookDispatch(store, env)
	if status != "ok" {
		t.Errorf("launch_initiated dispatch: status=%q, want ok", status)
	}
	if callbackFired {
		t.Error("launch_initiated dispatch: agent_ready callback was fired; it MUST NOT fire for launch_initiated (HC-041)")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Test 3: Socket round-trip — outcome_emitted ACK and unknown_session
// ─────────────────────────────────────────────────────────────────────────────

// TestHookSessionStore_SocketRoundTrip verifies the full socket path:
// 1. A hook-relay envelope is written to a real Unix domain socket.
// 2. The daemon reads it and returns a hookRelayAckMsg with status "ok".
// 3. After CloseHookSession, the same envelope returns "unknown_session".
func TestHookSessionStore_SocketRoundTrip(t *testing.T) {
	const runID = "run-sock-rt-01"
	const sessionID = "claude-sess-sock-rt-01"

	store := daemon.ExportedNewHookSessionStore()
	daemon.ExportedHookRegister(store, runID, sessionID)

	sockPath := socketFixtureTempSockPath(t)

	// Use a minimal no-op RequestHandler for the SocketRequest path.
	noopHandler := &stubHandler{}

	cancel, _ := socketFixtureStartListener(t, sockPath, noopHandler, store)
	defer cancel()
	socketFixtureWaitReady(t, sockPath)

	// Helper: send a hookRelayEnvelope over the socket and read back the ACK.
	sendEnvAndReadAck := func(t *testing.T, env map[string]interface{}) map[string]string {
		t.Helper()
		conn, err := (&net.Dialer{}).DialContext(t.Context(), "unix", sockPath)
		if err != nil {
			t.Fatalf("dial: %v", err)
		}
		defer func() { _ = conn.Close() }() //nolint:errcheck // cleanup; error unactionable

		data, marshalErr := json.Marshal(env)
		if marshalErr != nil {
			t.Fatalf("marshal env: %v", marshalErr)
		}
		if _, writeErr := fmt.Fprintf(conn, "%s\n", data); writeErr != nil {
			t.Fatalf("write: %v", writeErr)
		}

		scanner := bufio.NewScanner(conn)
		if !scanner.Scan() {
			scanErr := scanner.Err()
			if scanErr == nil {
				t.Fatal("read ack: EOF without response")
			}
			t.Fatalf("read ack: %v", scanErr)
		}
		var ack map[string]string
		if err := json.Unmarshal(scanner.Bytes(), &ack); err != nil {
			t.Fatalf("unmarshal ack: %v", err)
		}
		return ack
	}

	payload := hookRelayFixtureMakePayload(t, "WORK_COMPLETE", "socket round-trip outcome")
	env := map[string]interface{}{
		"type":               "outcome_emitted",
		"run_id":             runID,
		"claude_session_id":  sessionID,
		"handler_session_id": "handler-sess-sock",
		"emitted_at_ns":      int64(1000),
		"payload":            json.RawMessage(payload),
	}

	// Live dispatch: expect "ok".
	ack1 := sendEnvAndReadAck(t, env)
	if ack1["status"] != "ok" {
		t.Errorf("live ACK status=%q, want ok", ack1["status"])
	}

	// Verify latestOutcome was updated.
	got := daemon.ExportedHookLatestOutcome(store, runID, sessionID)
	if got == nil {
		t.Fatal("LatestOutcome after socket dispatch: nil, want non-nil")
	}

	// Close the session and send a stale arrival.
	daemon.ExportedHookClose(store, runID, sessionID)
	ack2 := sendEnvAndReadAck(t, env)
	if ack2["status"] != "unknown_session" {
		t.Errorf("stale ACK status=%q, want unknown_session", ack2["status"])
	}
}
