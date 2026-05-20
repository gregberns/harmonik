package daemon_test

// stophook_smoke_hkcj0gm_test.go — smoke test: outcome_emitted delivered via
// the real daemon socket is observed by waitWithSocketGrace within stopHookGrace.
//
// Reproduces the failure mode from hk-ajhqw / hk-cj0gm:
//   - Claude exits and fires the Stop hook.
//   - The hook relay connects to the daemon socket and sends outcome_emitted.
//   - The daemon's hookSessionStore records the outcome.
//   - Later, waitWithSocketGrace is called and must find the stored outcome via
//     LatestOutcome (fast path) — even if the outcome arrived long before the
//     grace window started.
//
// Two test cases:
//
//  1. OutcomeArrivesBeforeGrace — outcome stored before waitWithSocketGrace is
//     called; LatestOutcome fast-path must return it (regression guard for the
//     hk-ajhqw failure where the outcome arrived ~5 minutes before the daemon
//     finally noticed claude had exited).
//
//  2. OutcomeArrivesWithinGrace — outcome stored after waitWithSocketGrace
//     starts (the slow path); WaitForOutcome must wake within 3 s.
//
// Both cases use a real Unix-domain socket via RunSocketListener so the full
// relay→socket→store path is exercised, not just the in-memory store.
//
// Helper prefix: stopHookSmoke (implementer-protocol.md §Helper-prefix discipline;
// bead hk-cj0gm).
//
// Spec refs:
//   - specs/claude-hook-bridge.md §4.7 CHB-020 (terminal-event mapping)
//   - specs/claude-hook-bridge.md §4.10 CHB-025 (last-received-wins)
//   - internal/daemon/waitsocketgrace.go (stopHookGrace = 3 s)
//
// Bead: hk-cj0gm.

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/handlercontract"
)

// ─────────────────────────────────────────────────────────────────────────────
// Fixtures
// ─────────────────────────────────────────────────────────────────────────────

// stopHookSmokePayload builds an outcome_emitted JSON payload with the given kind.
func stopHookSmokePayload(t *testing.T, kind string) json.RawMessage {
	t.Helper()
	pl, err := json.Marshal(map[string]string{"kind": kind})
	if err != nil {
		t.Fatalf("stopHookSmokePayload: marshal: %v", err)
	}
	return pl
}

// stopHookSmokeWriteEnvelope connects to sockPath, sends a hook-relay
// outcome_emitted envelope for (runID, sessionID), and reads the ACK.
// Returns the ACK status string (e.g., "ok", "unknown_session").
//
// Mirrors the wire protocol in internal/hookrelay/hookrelay.go §sendToSocket:
// write JSON + newline, read one NDJSON ACK line.
func stopHookSmokeWriteEnvelope(t *testing.T, sockPath, runID, sessionID, kind string) string {
	t.Helper()

	payload := stopHookSmokePayload(t, kind)
	env := map[string]interface{}{
		"type":               "outcome_emitted",
		"run_id":             runID,
		"claude_session_id":  sessionID,
		"handler_session_id": "handler-smoke-01",
		"emitted_at_ns":      0,
		"payload":            json.RawMessage(payload),
	}
	envBytes, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("stopHookSmokeWriteEnvelope: marshal envelope: %v", err)
	}

	dialCtx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	conn, err := (&net.Dialer{}).DialContext(dialCtx, "unix", sockPath)
	if err != nil {
		t.Fatalf("stopHookSmokeWriteEnvelope: dial %q: %v", sockPath, err)
	}
	defer func() { _ = conn.Close() }()

	// Write envelope + newline.
	if _, werr := fmt.Fprintf(conn, "%s\n", envBytes); werr != nil {
		t.Fatalf("stopHookSmokeWriteEnvelope: write: %v", werr)
	}

	// Read ACK.
	if derr := conn.SetReadDeadline(time.Now().Add(5 * time.Second)); derr != nil {
		t.Fatalf("stopHookSmokeWriteEnvelope: set read deadline: %v", derr)
	}
	scanner := bufio.NewScanner(conn)
	if !scanner.Scan() {
		t.Fatalf("stopHookSmokeWriteEnvelope: read ACK: %v", scanner.Err())
	}

	var ack struct {
		Status string `json:"status"`
		Reason string `json:"reason,omitempty"`
	}
	if jerr := json.Unmarshal(scanner.Bytes(), &ack); jerr != nil {
		t.Fatalf("stopHookSmokeWriteEnvelope: parse ACK %q: %v", scanner.Bytes(), jerr)
	}
	return ack.Status
}

// stopHookSmokeStartSocket starts RunSocketListener in a goroutine with a real
// hookSessionStore wired as the HookRelayHandler.
// Returns the store so callers can register sessions and call waitWithSocketGrace.
func stopHookSmokeStartSocket(t *testing.T) (sockPath string, store *daemon.HookSessionStoreExported) {
	t.Helper()

	sockPath = socketFixtureTempSockPath(t)
	store = daemon.ExportedNewHookSessionStore()

	// nil RequestHandler — smoke tests never exercise the SocketRequest protocol.
	socketFixtureStartListener(t, sockPath, nil, store)
	socketFixtureWaitReady(t, sockPath)
	return sockPath, store
}

// ─────────────────────────────────────────────────────────────────────────────
// Test 1: outcome arrives BEFORE waitWithSocketGrace is called (fast path)
// ─────────────────────────────────────────────────────────────────────────────

// TestStopHookSmoke_OutcomeBeforeGrace verifies that an outcome_emitted message
// delivered to the daemon socket BEFORE waitWithSocketGrace is called is
// returned by the fast-path LatestOutcome check.
//
// This is the primary regression guard for hk-ajhqw / hk-cj0gm:
// in that incident the Stop hook relay fired ~5 minutes before the daemon's
// waitWithSocketGrace call; the fast-path check must find the stored outcome
// regardless of how much time elapsed between relay delivery and the call.
func TestStopHookSmoke_OutcomeBeforeGrace(t *testing.T) {
	t.Parallel()

	const runID = "run-stophook-smoke-before-01"
	const sessionID = "claude-sess-smoke-before-01"

	sockPath, store := stopHookSmokeStartSocket(t)
	daemon.ExportedHookRegister(store, runID, sessionID)

	// Deliver outcome_emitted via the real socket BEFORE calling waitWithSocketGrace.
	status := stopHookSmokeWriteEnvelope(t, sockPath, runID, sessionID, "WORK_COMPLETE")
	if status != "ok" {
		t.Fatalf("socket ACK status=%q; want ok", status)
	}

	// Verify the store has the outcome before calling waitWithSocketGrace.
	if raw := daemon.ExportedHookLatestOutcome(store, runID, sessionID); raw == nil {
		t.Fatal("LatestOutcome returned nil after socket delivery; outcome not stored")
	}

	// Now call waitWithSocketGrace with a session that has already exited (exitCode=0).
	// watcher=nil triggers the substrate path in waitWithSocketGrace.
	sess := waitGraceFixtureNewSession(0, nil)
	sess.unblockWait()

	outcome, ei := daemon.ExportedWaitWithSocketGrace(
		t.Context(), store, nil, sess, runID, sessionID,
	)

	if outcome == nil {
		t.Fatal("waitWithSocketGrace returned nil outcome; expected WORK_COMPLETE from fast path")
	}
	if outcome.Kind != "WORK_COMPLETE" {
		t.Errorf("outcome.Kind=%q; want WORK_COMPLETE", outcome.Kind)
	}
	if ei.ExitCode != 0 {
		t.Errorf("exitCode=%d; want 0", ei.ExitCode)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Test 2: outcome arrives WITHIN stopHookGrace (slow path)
// ─────────────────────────────────────────────────────────────────────────────

// TestStopHookSmoke_OutcomeWithinGrace verifies that an outcome_emitted message
// delivered via the real socket AFTER sess.Wait() returns (but within the 3 s
// stopHookGrace window) is returned by waitWithSocketGrace.
//
// This exercises the WaitForOutcome slow path (step 4 in waitsocketgrace.go).
func TestStopHookSmoke_OutcomeWithinGrace(t *testing.T) {
	t.Parallel()

	const runID = "run-stophook-smoke-within-01"
	const sessionID = "claude-sess-smoke-within-01"

	sockPath, store := stopHookSmokeStartSocket(t)
	daemon.ExportedHookRegister(store, runID, sessionID)

	// Session exits immediately (exitCode=0, no watcher).
	sess := waitGraceFixtureNewSession(0, nil)
	sess.unblockWait()

	// Deliver outcome_emitted after a short delay simulating relay process startup
	// + socket round-trip (~50 ms; well within the 3 s grace window).
	go func() {
		time.Sleep(50 * time.Millisecond)
		if status := stopHookSmokeWriteEnvelope(t, sockPath, runID, sessionID, "WORK_COMPLETE"); status != "ok" {
			// t.Errorf is safe from goroutines.
			t.Errorf("goroutine: socket ACK status=%q; want ok", status)
		}
	}()

	outcome, ei := daemon.ExportedWaitWithSocketGrace(
		t.Context(), store, nil, sess, runID, sessionID,
	)

	if outcome == nil {
		t.Fatal("waitWithSocketGrace returned nil outcome; expected WORK_COMPLETE within grace window")
	}
	if outcome.Kind != "WORK_COMPLETE" {
		t.Errorf("outcome.Kind=%q; want WORK_COMPLETE", outcome.Kind)
	}
	if ei.ExitCode != 0 {
		t.Errorf("exitCode=%d; want 0", ei.ExitCode)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Test 3: tmux substrate ctx-cancel pid-dead check (hk-cj0gm fix #2)
// ─────────────────────────────────────────────────────────────────────────────

// TestStopHookSmoke_SubstratePathWithWatcher verifies the watcher-based path:
// when watcher.Done() fires and sess.Wait() returns, outcome_emitted delivered
// before the call is still returned by waitWithSocketGrace.
//
// This guards the watcher-path (non-substrate) scenario.
func TestStopHookSmoke_SubstratePathWithWatcher(t *testing.T) {
	t.Parallel()

	const runID = "run-stophook-smoke-watcher-01"
	const sessionID = "claude-sess-smoke-watcher-01"

	sockPath, store := stopHookSmokeStartSocket(t)
	daemon.ExportedHookRegister(store, runID, sessionID)

	// Deliver outcome via socket first.
	status := stopHookSmokeWriteEnvelope(t, sockPath, runID, sessionID, "WORK_COMPLETE")
	if status != "ok" {
		t.Fatalf("socket ACK status=%q; want ok", status)
	}

	sess := waitGraceFixtureNewSession(0, nil)
	watcher, closeDone := handlercontract.NewWatcherForTest()

	closeDone()
	sess.unblockWait()

	outcome, ei := daemon.ExportedWaitWithSocketGrace(
		t.Context(), store, watcher, sess, runID, sessionID,
	)

	if outcome == nil {
		t.Fatal("waitWithSocketGrace returned nil outcome (watcher path); expected WORK_COMPLETE")
	}
	if outcome.Kind != "WORK_COMPLETE" {
		t.Errorf("outcome.Kind=%q; want WORK_COMPLETE", outcome.Kind)
	}
	if ei.ExitCode != 0 {
		t.Errorf("exitCode=%d; want 0", ei.ExitCode)
	}
}
