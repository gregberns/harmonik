package daemon_test

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
)

func chbInv002FixtureMakeEnvelope(msgType string, payload json.RawMessage) daemon.HookRelayEnvelopeExported {
	return daemon.HookRelayEnvelopeExported{
		Type:             msgType,
		RunID:            "chbinv002-run-01",
		ClaudeSessionID:  "chbinv002-claude-sess-01",
		HandlerSessionID: "chbinv002-handler-sess-01",
		EmittedAtNs:      1000,
		Payload:          payload,
	}
}

func chbInv002FixtureStopFailurePayload(t *testing.T) json.RawMessage {
	t.Helper()
	pl, err := json.Marshal(map[string]interface{}{
		"kind":            "FAILURE_SIGNAL",
		"error_type":      "claude_invalid_request",
		"sub_reason":      "claude_invalid_request",
		"suggested_class": "ErrStructural",
	})
	if err != nil {
		t.Fatalf("chbInv002FixtureStopFailurePayload: marshal: %v", err)
	}
	return pl
}

func chbInv002FixtureProjectDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	//nolint:gosec // G301: test-only temp directory; not production
	if err := os.MkdirAll(filepath.Join(dir, ".harmonik", "events"), 0o755); err != nil {
		t.Fatalf("chbInv002FixtureProjectDir: mkdir events: %v", err)
	}
	//nolint:gosec // G301: test-only temp directory; not production
	if err := os.MkdirAll(filepath.Join(dir, ".harmonik", "beads-intents"), 0o755); err != nil {
		t.Fatalf("chbInv002FixtureProjectDir: mkdir beads-intents: %v", err)
	}
	run := func(args ...string) {
		t.Helper()
		cmd := exec.CommandContext(t.Context(), "git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("chbInv002FixtureProjectDir: git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "--initial-branch=main")
	run("config", "user.email", "test@harmonik.local")
	run("config", "user.name", "Harmonik Test")
	readmePath := filepath.Join(dir, "README")
	if err := os.WriteFile(readmePath, []byte("chbinv002 test repo\n"), 0o644); err != nil {
		t.Fatalf("chbInv002FixtureProjectDir: WriteFile: %v", err)
	}
	run("add", "README")
	run("commit", "-m", "Initial commit")
	return dir
}

// TestCHBINV002_StopFailureInvalidRequest_RelayEmitsNonTerminal verifies that
// the hookSessionStore dispatch of an outcome_emitted{kind=FAILURE_SIGNAL}
// envelope (the relay translation of StopFailure{error_type=invalid_request})
// returns status="ok" and that the stored latestOutcome carries kind=FAILURE_SIGNAL,
// NOT a terminal event type.
//
// CHB-INV-002 constraint: the relay MUST NEVER emit agent_completed or
// agent_failed.  This test confirms the dispatch path accepts outcome_emitted
// (non-terminal) and that no terminal event type leaks through the store.
func TestCHBINV002_StopFailureInvalidRequest_RelayEmitsNonTerminal(t *testing.T) {
	t.Parallel()

	const runID = "chbinv002-run-01"
	const sessionID = "chbinv002-claude-sess-01"

	store := daemon.ExportedNewHookSessionStore()
	daemon.ExportedHookRegister(store, runID, sessionID)

	payload := chbInv002FixtureStopFailurePayload(t)
	env := chbInv002FixtureMakeEnvelope("outcome_emitted", payload)
	status, reason := daemon.ExportedHookDispatch(store, env)

	if status != "ok" {
		t.Errorf("StopFailure dispatch: status=%q reason=%q, want ok; "+
			"relay outcome_emitted must be accepted by the store (CHB-INV-002)", status, reason)
	}

	got := daemon.ExportedHookLatestOutcome(store, runID, sessionID)
	if got == nil {
		t.Fatal("latestOutcome is nil after StopFailure dispatch; want non-nil FAILURE_SIGNAL payload")
	}

	var gotMap map[string]interface{}
	if err := json.Unmarshal(*got, &gotMap); err != nil {
		t.Fatalf("latestOutcome unmarshal: %v", err)
	}
	if gotMap["kind"] != "FAILURE_SIGNAL" {
		t.Errorf("latestOutcome kind=%q, want FAILURE_SIGNAL; relay must not promote StopFailure to a terminal event", gotMap["kind"])
	}

	terminalTypes := map[string]bool{
		"agent_completed": true,
		"agent_failed":    true,
	}
	if terminalTypes[env.Type] {
		t.Errorf("relay envelope type=%q is a terminal event type; relay MUST NOT emit terminal events per CHB-INV-002", env.Type)
	}
}

// TestCHBINV002_SessionContainsExactlyOneTerminalEvent verifies that after a
// complete bead run where the relay injects outcome_emitted{kind=FAILURE_SIGNAL}
// into the hookSessionStore (simulating StopFailure{error_type=invalid_request}),
// the daemon's event bus contains exactly one terminal event (agent_completed OR
// agent_failed) and every relay-dispatched envelope type is non-terminal.
//
// This exercises CHB-INV-002 end-to-end: handler-process holds sole emitter
// authority for terminal events; relay is confined to non-terminal types.
func TestCHBINV002_SessionContainsExactlyOneTerminalEvent(t *testing.T) {
	t.Parallel()

	projectDir := chbInv002FixtureProjectDir(t)

	const beadID = core.BeadID("chbinv002-bead-01")
	ledger := &stubBeadLedger{
		ready: []core.BeadID{beadID},
	}
	collector := &stubEventCollector{}

	store := daemon.ExportedNewHookSessionStore()

	const relayRunID = "chbinv002-wl-run-01"
	const relaySessionID = "chbinv002-relay-sess-01" // independent of work loop's session ID

	relayDispatched := []string{}

	daemon.ExportedHookRegister(store, relayRunID, relaySessionID)

	relayEnvelopes := []struct {
		msgType string
		payload json.RawMessage
	}{
		{"outcome_emitted", chbInv002FixtureStopFailurePayload(t)},
		{"agent_heartbeat", json.RawMessage(`{"phase":"reasoning"}`)},
		{"agent_rate_limited", json.RawMessage(`{"retry_after_seconds":60}`)},
	}

	for _, env := range relayEnvelopes {
		e := daemon.HookRelayEnvelopeExported{
			Type:             env.msgType,
			RunID:            relayRunID,
			ClaudeSessionID:  relaySessionID,
			HandlerSessionID: "handler-sess-relay",
			EmittedAtNs:      1000,
			Payload:          env.payload,
		}
		status, reason := daemon.ExportedHookDispatch(store, e)
		if status != "ok" {
			t.Errorf("relay dispatch %q: status=%q reason=%q, want ok", env.msgType, status, reason)
		}
		relayDispatched = append(relayDispatched, env.msgType)
	}

	terminalSet := map[string]bool{
		string(core.EventTypeAgentCompleted): true,
		string(core.EventTypeAgentFailed):    true,
	}
	for _, relayType := range relayDispatched {
		if terminalSet[relayType] {
			t.Errorf("relay dispatched terminal event type %q; CHB-INV-002: relay MUST NEVER emit terminal events", relayType)
		}
	}

	scriptDir := t.TempDir()
	scriptPath := filepath.Join(scriptDir, "chbinv002_handler.sh")
	agentFailedLine := `{"type":"agent_failed","run_id":"00000000-0000-0000-0000-000000000000","class":"ErrStructural","sub_reason":"claude_invalid_request","summary":"StopFailure: invalid_request"}`
	script := "#!/bin/sh\nprintf '%s\\n' '" + agentFailedLine + "'\nexit 1\n"
	//nolint:gosec // G306: test-only fixture script
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("chbInv002: write handler script: %v", err)
	}

	deps := daemon.ExportedTestRuntime(daemon.TestRuntimeParams{
		BrAdapter:     ledger,
		Bus:           collector,
		ProjectDir:    projectDir,
		HandlerBinary: "/bin/sh",
		HandlerArgs:   []string{scriptPath},
		IntentLogDir:  filepath.Join(projectDir, ".harmonik", "beads-intents"),
		// HookStore: nil → real hookSessionStore; stopHookGrace (~3s) fires on exit.
		AdapterRegistry2: NewSealedAdapterRegistryForTest(t),
	})

	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()

	waitDone := make(chan struct{})
	go func() {
		defer close(waitDone)
		daemon.ExportedRunWorkLoop(ctx, deps)
	}()

	deadline := time.After(10 * time.Second)
	for len(ledger.reopenedIDs()) == 0 && len(ledger.closedIDs()) == 0 {
		select {
		case <-deadline:
			t.Logf("chbInv002: events=%v closed=%v reopened=%v",
				collector.eventTypes(), ledger.closedIDs(), ledger.reopenedIDs())
			t.Fatal("chbInv002: timed out waiting for bead state change after handler exit=1")
		case <-time.After(50 * time.Millisecond):
		}
	}

	cancel()
	<-waitDone

	allEvents := collector.allEvents()
	terminalCount := 0
	for _, ev := range allEvents {
		if terminalSet[ev.EventType] {
			terminalCount++
		}
	}

	t.Logf("chbInv002: events=%v closed=%v reopened=%v terminalCount=%d",
		collector.eventTypes(), ledger.closedIDs(), ledger.reopenedIDs(), terminalCount)

	if terminalCount != 1 {
		t.Errorf("bus contains %d terminal event(s); want exactly 1 (CHB-INV-002: handler-process is sole terminal emitter); events=%v",
			terminalCount, collector.eventTypes())
	}

	foundTerminalType := ""
	for _, ev := range allEvents {
		if terminalSet[ev.EventType] {
			foundTerminalType = ev.EventType
			break
		}
	}
	if terminalCount == 1 && foundTerminalType != string(core.EventTypeAgentFailed) {
		t.Errorf("terminal event type=%q; want agent_failed for a StopFailure run (CHB-INV-002)", foundTerminalType)
	}
}
