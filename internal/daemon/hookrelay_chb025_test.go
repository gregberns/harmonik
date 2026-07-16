package daemon_test

// hookrelay_chb025_test.go — daemon-side socket round-trip test for CHB-025.
//
// The pure last-received-wins dedup, agent_ready callback, and WaitForOutcome
// unit tests were migrated to internal/hook (package hook_test) in M5 slice 1 —
// they no longer need the daemon. What remains here is the ONE test that must
// stand up the real daemon socket: the hookRelayEnvelope → ACK round trip over a
// Unix domain socket, including unknown_session on a closed session. This
// exercises the daemon shell (socket acceptor + hookSessionStore.HandleHookRelay
// delegation), which the pure hook package cannot cover.
//
// Helper prefix: hookRelayFixture (implementer-protocol.md §Helper-prefix
// discipline; bead hk-w5vra.11).

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"testing"

	"github.com/gregberns/harmonik/internal/daemon"
)

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
