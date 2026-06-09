package main

// heartbeatThenHoldScenario_hkhe18w_test.go — unit tests for the
// heartbeat-then-hold canned scenario and the `hold` script step (validation-net
// VN2, bead hk-he18w).
//
// These tests assert the structural contract the daemon-level regression guard
// (hk-ukhzu) relies on:
//
//   - The scenario emits at least one agent_heartbeat (the heartbeat the
//     watchdog must observe to clear firstHeartbeatSeen).
//   - The scenario ends with a `hold` step and emits NO terminal event
//     (no agent_completed / agent_failed / outcome_emitted) and NO commit step —
//     so the pane child stays alive contending on the per-run tap.
//   - The hold step blocks until ctx is cancelled (hold_ms=0) and unblocks
//     promptly on cancellation, and self-terminates when hold_ms>0.
//
// Helper prefix: hthFixture (bead hk-he18w; per implementer-protocol.md
// §Helper-prefix discipline).

import (
	"context"
	"testing"
	"time"
)

// hthFixtureScenario loads the heartbeat-then-hold canned scenario, failing the
// test if it is not registered.
func hthFixtureScenario(t *testing.T) *ScriptFile {
	t.Helper()
	sf, err := cannedScenario("heartbeat-then-hold")
	if err != nil {
		t.Fatalf("cannedScenario(heartbeat-then-hold): %v", err)
	}
	if sf == nil {
		t.Fatal("cannedScenario(heartbeat-then-hold): nil ScriptFile")
	}
	return sf
}

// TestHeartbeatThenHold_Registered verifies the scenario is wired into
// cannedScenario (so --scenario heartbeat-then-hold resolves).
func TestHeartbeatThenHold_Registered(t *testing.T) {
	_ = hthFixtureScenario(t)
}

// TestHeartbeatThenHold_EmitsHeartbeat verifies the scenario emits at least one
// agent_heartbeat — the signal the pasteInjectQuitOnCommit watchdog must observe
// (firstHeartbeatSeen) to advance past the launch-suppression branch.
func TestHeartbeatThenHold_EmitsHeartbeat(t *testing.T) {
	sf := hthFixtureScenario(t)
	heartbeats := 0
	for _, m := range sf.Messages {
		if m.Type == "agent_heartbeat" {
			heartbeats++
		}
	}
	if heartbeats < 1 {
		t.Errorf("heartbeat-then-hold: emitted %d agent_heartbeat messages, want >=1 "+
			"(the watchdog needs a heartbeat to clear firstHeartbeatSeen)", heartbeats)
	}
}

// TestHeartbeatThenHold_NoTerminalNoCommit verifies the scenario does NOT emit a
// terminal event and does NOT commit — the pane child must stay alive so the
// launch-suppression branch (pasteinject.go:679) remains active. A terminal
// event or commit would end the run early and defeat the regression guard.
func TestHeartbeatThenHold_NoTerminalNoCommit(t *testing.T) {
	sf := hthFixtureScenario(t)
	for i, m := range sf.Messages {
		switch m.Type {
		case "agent_completed", "agent_failed", "outcome_emitted":
			t.Errorf("heartbeat-then-hold: message %d is terminal type %q; "+
				"the scenario must not terminate (pane must stay alive)", i, m.Type)
		case commitOnCueStep:
			t.Errorf("heartbeat-then-hold: message %d is %q; the scenario must not "+
				"commit (a commit ends the run before the wedge can manifest)", i, m.Type)
		}
	}
}

// TestHeartbeatThenHold_EndsWithHold verifies the final step is a hold step —
// the blocking primitive that keeps the process alive.
func TestHeartbeatThenHold_EndsWithHold(t *testing.T) {
	sf := hthFixtureScenario(t)
	if len(sf.Messages) == 0 {
		t.Fatal("heartbeat-then-hold: no messages")
	}
	last := sf.Messages[len(sf.Messages)-1]
	if last.Type != holdStep {
		t.Errorf("heartbeat-then-hold: last message type = %q, want %q "+
			"(the scenario must end by holding the process alive)", last.Type, holdStep)
	}
}

// TestRunHold_BlocksUntilCtxCancel verifies runHold (hold_ms=0) blocks until the
// context is cancelled, then returns promptly. This is the canonical mode the
// daemon drives: the daemon cancels the run context to unblock the twin.
func TestRunHold_BlocksUntilCtxCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	msg := ScriptMessage{Type: holdStep, Payload: map[string]any{"hold_ms": 0}}

	done := make(chan struct{})
	go func() {
		runHold(ctx, msg)
		close(done)
	}()

	// runHold must still be blocking before cancel.
	select {
	case <-done:
		t.Fatal("runHold returned before ctx was cancelled; it must block on hold_ms=0")
	case <-time.After(50 * time.Millisecond):
	}

	cancel()

	select {
	case <-done:
		// Returned promptly after cancel — correct.
	case <-time.After(2 * time.Second):
		t.Fatal("runHold did not return within 2s after ctx cancel")
	}
}

// TestRunHold_SelfTerminatesOnHoldMs verifies runHold with hold_ms>0 returns
// after the bounded window even when ctx is never cancelled (defensive backstop).
func TestRunHold_SelfTerminatesOnHoldMs(t *testing.T) {
	msg := ScriptMessage{Type: holdStep, Payload: map[string]any{"hold_ms": 30}}

	start := time.Now()
	done := make(chan struct{})
	go func() {
		runHold(context.Background(), msg)
		close(done)
	}()

	select {
	case <-done:
		if elapsed := time.Since(start); elapsed < 20*time.Millisecond {
			t.Errorf("runHold returned after %v, want >= ~30ms (hold_ms bound)", elapsed)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("runHold(hold_ms=30) did not self-terminate within 2s")
	}
}

// TestRunHold_FloatHoldMs verifies runHold accepts a float64 hold_ms (the type
// YAML/map[string]any numbers decode to), mirroring runSignalInterrupt's
// delay_ms handling.
func TestRunHold_FloatHoldMs(t *testing.T) {
	msg := ScriptMessage{Type: holdStep, Payload: map[string]any{"hold_ms": float64(30)}}
	done := make(chan struct{})
	go func() {
		runHold(context.Background(), msg)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("runHold(hold_ms=float64(30)) did not self-terminate within 2s")
	}
}
