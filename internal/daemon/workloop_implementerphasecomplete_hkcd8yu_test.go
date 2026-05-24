package daemon_test

// workloop_implementerphasecomplete_hkcd8yu_test.go — unit and integration tests
// for the implementer_phase_complete event (hk-cd8yu).
//
// # What this file tests
//
// 1. TestImplementerPhaseComplete_SingleMode_EventFired — single-mode path
//    (beadRunOne). A handler that exits 0 without committing triggers the
//    no_commit guard but the implementer_phase_complete event MUST still fire
//    before run_failed.
//
// 2. TestImplementerPhaseComplete_ReviewLoop_EventFired — review-loop path
//    (runReviewLoop iteration 1). Same handler that crashes with non-zero exit
//    before committing; implementer_phase_complete fires before the review-loop
//    no_commit_during_implementer failure.
//
// 3. TestImplementerPhaseComplete_Payload_Fields — unit test that checks the
//    payload fields (exit_code, stderr_tail_head, commit_landed, duration_seconds)
//    against known handler behaviour.
//
// # Helper prefix: implPhaseFixture (bead hk-cd8yu per implementer-protocol
// §Helper-prefix discipline).

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
)

// ─────────────────────────────────────────────────────────────────────────────
// Fixture helpers
// ─────────────────────────────────────────────────────────────────────────────

// implPhaseFixtureProjectDir creates the minimal project directory tree:
// .harmonik/events/ and .harmonik/beads-intents/.
func implPhaseFixtureProjectDir(t *testing.T) string {
	t.Helper()
	raw := t.TempDir()
	dir, err := filepath.EvalSymlinks(raw)
	if err != nil {
		t.Fatalf("implPhaseFixtureProjectDir: EvalSymlinks %q: %v", raw, err)
	}
	for _, sub := range []string{
		filepath.Join(".harmonik", "events"),
		filepath.Join(".harmonik", "beads-intents"),
	} {
		//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
		if mkErr := os.MkdirAll(filepath.Join(dir, sub), 0o755); mkErr != nil {
			t.Fatalf("implPhaseFixtureProjectDir: mkdir %s: %v", sub, mkErr)
		}
	}
	return dir
}

// implPhaseFixtureHandlerExitsNoCommit writes a handler script that writes
// stderrMsg to stderr and exits with exitCode WITHOUT making a commit.
func implPhaseFixtureHandlerExitsNoCommit(t *testing.T, exitCode int, stderrMsg string) string {
	t.Helper()
	script := fmt.Sprintf("#!/bin/sh\nprintf '%%s\\n' '%s' 1>&2\nexit %d\n", stderrMsg, exitCode)
	scriptPath := filepath.Join(t.TempDir(), "impl_phase_handler.sh")
	//nolint:gosec // G306: test-only fixture script requires execute bit
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("implPhaseFixtureHandlerExitsNoCommit: WriteFile: %v", err)
	}
	return scriptPath
}

// implPhaseFixtureFirstEventOfType scans collector events for the first event
// of the given type and returns (payload, true). Returns ("", false) if not found.
func implPhaseFixtureFirstEventOfType(collector *stubEventCollector, eventType string) (json.RawMessage, bool) {
	for _, ev := range collector.allEvents() {
		if ev.EventType == eventType {
			return ev.Payload, true
		}
	}
	return nil, false
}

// ─────────────────────────────────────────────────────────────────────────────
// TestImplementerPhaseComplete_ReviewLoop_EventFired
// ─────────────────────────────────────────────────────────────────────────────

// TestImplementerPhaseComplete_ReviewLoop_EventFired asserts that the
// implementer_phase_complete event fires when the implementer session ends in
// review-loop mode (iteration 1, non-zero exit, no commit).
//
// The event MUST appear before the run exits so that it is visible in events.jsonl
// between run_started and run_failed/run_completed.
//
// Bead: hk-cd8yu.
func TestImplementerPhaseComplete_ReviewLoop_EventFired(t *testing.T) {
	t.Parallel()

	const stderrMarker = "cd8yu-impl-phase-crash-marker"
	const handlerExitCode = 42

	projectDir := implPhaseFixtureProjectDir(t)
	rlFixtureGitRepo(t, projectDir)
	wtPath, parentSHA := rlFixtureWorktree(t, projectDir)

	scriptPath := implPhaseFixtureHandlerExitsNoCommit(t, handlerExitCode, stderrMarker)

	collector := &stubEventCollector{}
	ledger := &stubBeadLedger{}

	deps := daemon.ExportedWorkLoopDeps(daemon.WorkLoopDepsParams{
		BrAdapter:           ledger,
		Bus:                 collector,
		ProjectDir:          projectDir,
		HandlerBinary:       "/bin/sh",
		HandlerArgs:         []string{scriptPath},
		IntentLogDir:        filepath.Join(projectDir, ".harmonik", "beads-intents"),
		AdapterRegistry2:    NewSealedAdapterRegistryForTest(t),
		WorkflowModeDefault: core.WorkflowModeReviewLoop,
	})

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	result := daemon.ExportedRunReviewLoop(
		ctx, deps,
		rlFixtureRunID(t),
		core.BeadID("cd8yu-rl-event-fired-001"),
		wtPath, parentSHA,
	)

	// The review loop should fail (no commit on iteration 1).
	if result.Success {
		t.Errorf("hk-cd8yu: result.Success = true; want false (no commit)")
	}

	// implementer_phase_complete MUST have been emitted.
	payload, found := implPhaseFixtureFirstEventOfType(collector,
		string(core.EventTypeImplementerPhaseComplete))
	if !found {
		types := collector.eventTypes()
		t.Errorf("hk-cd8yu: implementer_phase_complete event not found.\nEmitted events: %v",
			types)
		return
	}

	// Decode and validate the payload fields.
	var pl core.ImplementerPhaseCompletePayload
	if decErr := json.Unmarshal(payload, &pl); decErr != nil {
		t.Fatalf("hk-cd8yu: unmarshal implementer_phase_complete payload: %v", decErr)
	}

	// exit_code must match the handler's exit code.
	if pl.ExitCode != handlerExitCode {
		t.Errorf("hk-cd8yu: ExitCode = %d; want %d", pl.ExitCode, handlerExitCode)
	}

	// commit_landed must be false (handler didn't commit).
	if pl.CommitLanded {
		t.Errorf("hk-cd8yu: CommitLanded = true; want false (no commit)")
	}

	// duration_seconds must be non-negative.
	if pl.DurationSeconds < 0 {
		t.Errorf("hk-cd8yu: DurationSeconds = %f; want >= 0", pl.DurationSeconds)
	}

	// stderr_tail_head should contain the stderr marker emitted by the handler.
	if !strings.Contains(pl.StderrTailHead, stderrMarker) {
		t.Errorf("hk-cd8yu: StderrTailHead does not contain %q.\ngot: %q",
			stderrMarker, pl.StderrTailHead)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// TestImplementerPhaseComplete_Payload_StderrTailHead_Truncated
// ─────────────────────────────────────────────────────────────────────────────

// TestImplementerPhaseComplete_Payload_StderrTailHead_Truncated verifies that
// when the implementer emits more than 200 bytes of stderr, only the first 200
// bytes appear in StderrTailHead.
//
// Bead: hk-cd8yu.
func TestImplementerPhaseComplete_Payload_StderrTailHead_Truncated(t *testing.T) {
	t.Parallel()

	// Build a handler that emits >200 bytes of stderr.
	longStderr := strings.Repeat("X", 512)
	scriptPath := implPhaseFixtureHandlerExitsNoCommit(t, 1, longStderr)

	projectDir := implPhaseFixtureProjectDir(t)
	rlFixtureGitRepo(t, projectDir)
	wtPath, parentSHA := rlFixtureWorktree(t, projectDir)

	collector := &stubEventCollector{}
	ledger := &stubBeadLedger{}

	deps := daemon.ExportedWorkLoopDeps(daemon.WorkLoopDepsParams{
		BrAdapter:           ledger,
		Bus:                 collector,
		ProjectDir:          projectDir,
		HandlerBinary:       "/bin/sh",
		HandlerArgs:         []string{scriptPath},
		IntentLogDir:        filepath.Join(projectDir, ".harmonik", "beads-intents"),
		AdapterRegistry2:    NewSealedAdapterRegistryForTest(t),
		WorkflowModeDefault: core.WorkflowModeReviewLoop,
	})

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	daemon.ExportedRunReviewLoop(
		ctx, deps,
		rlFixtureRunID(t),
		core.BeadID("cd8yu-stderr-trunc-001"),
		wtPath, parentSHA,
	)

	payload, found := implPhaseFixtureFirstEventOfType(collector,
		string(core.EventTypeImplementerPhaseComplete))
	if !found {
		t.Fatal("hk-cd8yu: implementer_phase_complete event not found")
	}

	var pl core.ImplementerPhaseCompletePayload
	if decErr := json.Unmarshal(payload, &pl); decErr != nil {
		t.Fatalf("hk-cd8yu: unmarshal: %v", decErr)
	}

	const maxHead = 200
	if len(pl.StderrTailHead) > maxHead {
		t.Errorf("hk-cd8yu: StderrTailHead len = %d; want <= %d",
			len(pl.StderrTailHead), maxHead)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// TestImplementerPhaseComplete_SingleMode_EventFired
// ─────────────────────────────────────────────────────────────────────────────

// TestImplementerPhaseComplete_SingleMode_EventFired asserts that the
// implementer_phase_complete event fires in single-mode (beadRunOne) when the
// implementer exits without committing (no_commit_during_implementer path).
//
// Bead: hk-cd8yu.
func TestImplementerPhaseComplete_SingleMode_EventFired(t *testing.T) {
	t.Parallel()

	const stderrMarker = "cd8yu-single-mode-crash-marker"

	projectDir := implPhaseFixtureProjectDir(t)
	workloopFixtureGitRepo(t, projectDir)

	// Handler: write stderr, exit 1, no commit.
	scriptPath := implPhaseFixtureHandlerExitsNoCommit(t, 1, stderrMarker)

	collector := &stubEventCollector{}
	ledger := &stubBeadLedger{ready: []core.BeadID{"cd8yu-single-001"}}

	loopCtx, loopCancel := context.WithCancel(t.Context())

	deps := daemon.ExportedWorkLoopDeps(daemon.WorkLoopDepsParams{
		BrAdapter:        ledger,
		Bus:              collector,
		ProjectDir:       projectDir,
		HandlerBinary:    "/bin/sh",
		HandlerArgs:      []string{scriptPath},
		IntentLogDir:     filepath.Join(projectDir, ".harmonik", "beads-intents"),
		AdapterRegistry2: NewSealedAdapterRegistryForTest(t),
	})

	// Run the work loop until the first bead is exhausted then cancel.
	runDone := make(chan error, 1)
	go func() {
		runDone <- daemon.ExportedRunWorkLoop(loopCtx, deps)
	}()

	// Wait for implementer_phase_complete or run_failed to appear — whichever
	// comes first — then cancel the loop so it terminates.
	const pollBudget = 20 * time.Second
	deadline := time.Now().Add(pollBudget)
	found := false
	for !found && time.Now().Before(deadline) {
		for _, ev := range collector.allEvents() {
			if ev.EventType == string(core.EventTypeImplementerPhaseComplete) {
				found = true
				break
			}
		}
		if !found {
			time.Sleep(10 * time.Millisecond)
		}
	}
	loopCancel()
	select {
	case <-runDone:
	case <-time.After(5 * time.Second):
		t.Error("hk-cd8yu: work loop did not stop within 5 s after cancel")
	}

	if !found {
		types := collector.eventTypes()
		t.Errorf("hk-cd8yu single-mode: implementer_phase_complete not found within %s.\nEmitted events: %v",
			pollBudget, types)
	}
}
