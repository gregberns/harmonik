package daemon_test

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"testing"

	"github.com/gregberns/harmonik/internal/daemon"
)

func chbInv001FixtureSendRelayEnvAndReadAck(t *testing.T, sockPath string, env map[string]interface{}) map[string]string {
	t.Helper()

	conn, err := (&net.Dialer{}).DialContext(t.Context(), "unix", sockPath)
	if err != nil {
		t.Fatalf("chbInv001FixtureSendRelayEnvAndReadAck: dial %q: %v", sockPath, err)
	}
	defer func() { _ = conn.Close() }()

	data, marshalErr := json.Marshal(env)
	if marshalErr != nil {
		t.Fatalf("chbInv001FixtureSendRelayEnvAndReadAck: marshal: %v", marshalErr)
	}
	if _, writeErr := fmt.Fprintf(conn, "%s\n", data); writeErr != nil {
		t.Fatalf("chbInv001FixtureSendRelayEnvAndReadAck: write: %v", writeErr)
	}

	scanner := bufio.NewScanner(conn)
	if !scanner.Scan() {
		if scanErr := scanner.Err(); scanErr != nil {
			t.Fatalf("chbInv001FixtureSendRelayEnvAndReadAck: scan: %v", scanErr)
		}
		t.Fatal("chbInv001FixtureSendRelayEnvAndReadAck: EOF before ack")
	}
	var ack map[string]string
	if unmarshalErr := json.Unmarshal(scanner.Bytes(), &ack); unmarshalErr != nil {
		t.Fatalf("chbInv001FixtureSendRelayEnvAndReadAck: unmarshal ack: %v", unmarshalErr)
	}
	return ack
}

func chbInv001FixtureMakeOutcomeEnv(t *testing.T, runID, sessionID, summary string) map[string]interface{} {
	t.Helper()
	payload, err := json.Marshal(map[string]string{"kind": "WORK_COMPLETE", "summary": summary})
	if err != nil {
		t.Fatalf("chbInv001FixtureMakeOutcomeEnv: marshal payload: %v", err)
	}
	return map[string]interface{}{
		"type":               "outcome_emitted",
		"run_id":             runID,
		"claude_session_id":  sessionID,
		"handler_session_id": "handler-sess-chbinv001",
		"emitted_at_ns":      int64(1000),
		"payload":            json.RawMessage(payload),
	}
}

func chbInv001FixtureMakeLaunchInitiatedEnv(runID, sessionID string) map[string]interface{} {
	return map[string]interface{}{
		"type":               "launch_initiated",
		"run_id":             runID,
		"claude_session_id":  sessionID,
		"handler_session_id": "handler-sess-chbinv001",
		"emitted_at_ns":      int64(500),
		"payload":            json.RawMessage(`{}`),
	}
}

// TestCHBINV001_TwoContributorSession is the primary CHB-INV-001 acceptance sensor.
//
// It drives two distinct connection types — a handler-side launch_initiated and
// a relay-side outcome_emitted — to the same daemon socket under the same
// (run_id, claude_session_id) tuple, and asserts that both are accepted with
// status "ok" and that the session remains coherent.
func TestCHBINV001_TwoContributorSession(t *testing.T) {
	t.Parallel()

	const runID = "run-chbinv001-two-contrib-01"
	const sessionID = "claude-sess-chbinv001-two-contrib-01"

	store := daemon.ExportedNewHookSessionStore()
	daemon.ExportedHookRegister(store, runID, sessionID)

	sockPath := socketFixtureTempSockPath(t)
	noopH := &stubHandler{}

	cancel, _ := socketFixtureStartListener(t, sockPath, noopH, store)
	defer cancel()
	socketFixtureWaitReady(t, sockPath)

	handlerEnv := chbInv001FixtureMakeLaunchInitiatedEnv(runID, sessionID)
	handlerAck := chbInv001FixtureSendRelayEnvAndReadAck(t, sockPath, handlerEnv)

	if handlerAck["status"] != "ok" {
		t.Errorf("handler-side launch_initiated: ACK status = %q (reason=%q), want %q",
			handlerAck["status"], handlerAck["reason"], "ok")
	}

	const wantSummary = "chbinv001 relay outcome"
	relayEnv := chbInv001FixtureMakeOutcomeEnv(t, runID, sessionID, wantSummary)
	relayAck := chbInv001FixtureSendRelayEnvAndReadAck(t, sockPath, relayEnv)

	if relayAck["status"] != "ok" {
		t.Errorf("relay-side outcome_emitted: ACK status = %q (reason=%q), want %q",
			relayAck["status"], relayAck["reason"], "ok")
	}

	latestOutcome := daemon.ExportedHookLatestOutcome(store, runID, sessionID)
	if latestOutcome == nil {
		t.Fatal("CHB-INV-001: latestOutcome is nil after two-contributor session; want relay outcome recorded")
	}

	var gotMap map[string]string
	if err := json.Unmarshal(*latestOutcome, &gotMap); err != nil {
		t.Fatalf("CHB-INV-001: unmarshal latestOutcome: %v", err)
	}
	if gotMap["summary"] != wantSummary {
		t.Errorf("CHB-INV-001: latestOutcome summary = %q, want %q", gotMap["summary"], wantSummary)
	}
	if gotMap["kind"] != "WORK_COMPLETE" {
		t.Errorf("CHB-INV-001: latestOutcome kind = %q, want WORK_COMPLETE", gotMap["kind"])
	}
}

// TestCHBINV001_MultipleRelayContributors verifies that multiple relay-side
// one-shot connections (simulating multiple Stop hooks from a single session)
// are all accepted by the daemon socket under the same (run_id, claude_session_id)
// tuple, with last-received-wins semantics per CHB-025.
func TestCHBINV001_MultipleRelayContributors(t *testing.T) {
	t.Parallel()

	const runID = "run-chbinv001-multi-relay-01"
	const sessionID = "claude-sess-chbinv001-multi-relay-01"

	store := daemon.ExportedNewHookSessionStore()
	daemon.ExportedHookRegister(store, runID, sessionID)

	sockPath := socketFixtureTempSockPath(t)
	noopH := &stubHandler{}

	cancel, _ := socketFixtureStartListener(t, sockPath, noopH, store)
	defer cancel()
	socketFixtureWaitReady(t, sockPath)

	summaries := []string{"relay-outcome-01", "relay-outcome-02", "relay-outcome-final"}
	for i, summary := range summaries {
		env := chbInv001FixtureMakeOutcomeEnv(t, runID, sessionID, summary)
		ack := chbInv001FixtureSendRelayEnvAndReadAck(t, sockPath, env)
		if ack["status"] != "ok" {
			t.Errorf("relay connection %d: ACK status = %q (reason=%q), want ok",
				i+1, ack["status"], ack["reason"])
		}
	}

	latestOutcome := daemon.ExportedHookLatestOutcome(store, runID, sessionID)
	if latestOutcome == nil {
		t.Fatal("CHB-INV-001 multi-relay: latestOutcome is nil, want final relay outcome")
	}

	var gotMap map[string]string
	if err := json.Unmarshal(*latestOutcome, &gotMap); err != nil {
		t.Fatalf("CHB-INV-001 multi-relay: unmarshal latestOutcome: %v", err)
	}
	if gotMap["summary"] != summaries[len(summaries)-1] {
		t.Errorf("CHB-INV-001 multi-relay: latestOutcome summary = %q, want %q (last-received-wins)",
			gotMap["summary"], summaries[len(summaries)-1])
	}
}

// TestCHBINV001_SessionKeyIsolation verifies that two concurrent sessions with
// different (run_id, claude_session_id) tuples do not interfere: relay
// contributions to session A do not appear in session B's store and vice versa.
func TestCHBINV001_SessionKeyIsolation(t *testing.T) {
	t.Parallel()

	const runIDA = "run-chbinv001-iso-A"
	const sessionIDA = "claude-sess-chbinv001-iso-A"
	const runIDB = "run-chbinv001-iso-B"
	const sessionIDB = "claude-sess-chbinv001-iso-B"

	store := daemon.ExportedNewHookSessionStore()
	daemon.ExportedHookRegister(store, runIDA, sessionIDA)
	daemon.ExportedHookRegister(store, runIDB, sessionIDB)

	sockPath := socketFixtureTempSockPath(t)
	noopH := &stubHandler{}

	cancel, _ := socketFixtureStartListener(t, sockPath, noopH, store)
	defer cancel()
	socketFixtureWaitReady(t, sockPath)

	handlerEnvA := chbInv001FixtureMakeLaunchInitiatedEnv(runIDA, sessionIDA)
	ackHA := chbInv001FixtureSendRelayEnvAndReadAck(t, sockPath, handlerEnvA)
	if ackHA["status"] != "ok" {
		t.Errorf("session A handler ack: status = %q, want ok", ackHA["status"])
	}

	relayEnvA := chbInv001FixtureMakeOutcomeEnv(t, runIDA, sessionIDA, "outcome-for-A")
	ackRA := chbInv001FixtureSendRelayEnvAndReadAck(t, sockPath, relayEnvA)
	if ackRA["status"] != "ok" {
		t.Errorf("session A relay ack: status = %q, want ok", ackRA["status"])
	}

	relayEnvB := chbInv001FixtureMakeOutcomeEnv(t, runIDB, sessionIDB, "outcome-for-B")
	ackRB := chbInv001FixtureSendRelayEnvAndReadAck(t, sockPath, relayEnvB)
	if ackRB["status"] != "ok" {
		t.Errorf("session B relay ack: status = %q, want ok", ackRB["status"])
	}

	latestA := daemon.ExportedHookLatestOutcome(store, runIDA, sessionIDA)
	if latestA == nil {
		t.Fatal("CHB-INV-001 isolation: session A latestOutcome is nil")
	}
	var gotA map[string]string
	if err := json.Unmarshal(*latestA, &gotA); err != nil {
		t.Fatalf("CHB-INV-001 isolation: unmarshal A: %v", err)
	}
	if gotA["summary"] != "outcome-for-A" {
		t.Errorf("CHB-INV-001 isolation: session A summary = %q, want outcome-for-A", gotA["summary"])
	}

	latestB := daemon.ExportedHookLatestOutcome(store, runIDB, sessionIDB)
	if latestB == nil {
		t.Fatal("CHB-INV-001 isolation: session B latestOutcome is nil")
	}
	var gotB map[string]string
	if err := json.Unmarshal(*latestB, &gotB); err != nil {
		t.Fatalf("CHB-INV-001 isolation: unmarshal B: %v", err)
	}
	if gotB["summary"] != "outcome-for-B" {
		t.Errorf("CHB-INV-001 isolation: session B summary = %q, want outcome-for-B", gotB["summary"])
	}
}
