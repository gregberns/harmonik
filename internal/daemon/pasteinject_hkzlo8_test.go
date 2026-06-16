package daemon_test

// pasteinject_hkzlo8_test.go — regression for hk-zlo8: pasteInjectOnLaunch and
// pasteInjectQuitOnCommit must be skipped for ProcessExit harnesses (codex) and
// retained for interactive (claude) harnesses.
//
// # Problem
//
// Before hk-zlo8, pasteInjectOnLaunch was called UNCONDITIONALLY in both
// workloop.go (single mode) and reviewloop.go (review-loop mode).  The hk-f6g7
// fix gated waitAgentReady on CompletionProcessExit, but the ADJACENT paste-inject
// block was left ungated.  CodexHarness (CompletionProcessExit) has no tmux pane
// and receives its task via argv (the launch spec), not pane paste.  When the
// daemon tried to paste-inject it hit "WriteLastPane: tmux: paste-buffer exited 1:
// cant find pane" → pasteinject_failed → implementer_phase_complete → no_commit in
// ~4s — the codex run never did any work.
//
// # Fix
//
// workloop.go and reviewloop.go now gate the entire paste-inject block (both
// pasteInjectOnLaunch and pasteInjectQuitOnCommit) on
// completionMode != CompletionProcessExit, mirroring the existing hk-f6g7 gate
// for waitAgentReady.
//
// # Test matrix
//
//   - TestPasteInject_ProcessExitHarness_SkipsPaste: a codex (ProcessExit)
//     run with a spy substrate that records WriteLastPane calls.  The gate
//     prevents the call; the bead completes successfully with 0 paste attempts.
//
//   - TestPasteInject_InteractiveHarness_RetainsPaste: an interactive (claude-
//     completion-mode) run with the same spy substrate.  No HarnessRegistry →
//     completionMode defaults to CompletionEventStreamThenQuit → gate not
//     applied → WriteLastPane IS called (≥1 call recorded).
//
// Helper prefix: hkzlo8 (per implementer-protocol.md §Helper-prefix discipline).
//
// Spec ref: specs/process-lifecycle.md §4.7 PL-021d; specs/harness-contract.md §2 N5.
// Bead ref: hk-zlo8.

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/handler"
)

// ─────────────────────────────────────────────────────────────────────────────
// hkzlo8SpySubstrate — spy that implements handler.Substrate AND pasteInjecter
// ─────────────────────────────────────────────────────────────────────────────

// hkzlo8SpySubstrate is a handler.Substrate spy that:
//   - Counts every SpawnWindow call and runs the real Argv as a subprocess.
//   - Also implements WriteLastPane (pasteInjecter) to track paste-inject calls.
//
// Because it is NOT a *tmuxSubstrate, newPerRunSubstrate returns nil and
// runPasteTarget falls back to deps.substrate (this spy), so pasteInjectOnLaunch
// will detect WriteLastPane via the pasteInjecter type assertion when the gate
// is not applied.
type hkzlo8SpySubstrate struct {
	spawnCount atomic.Int64
	pasteCount atomic.Int64
}

func (s *hkzlo8SpySubstrate) SpawnWindow(ctx context.Context, in handler.SubstrateSpawn) (handler.SubstrateSession, error) {
	s.spawnCount.Add(1)
	if len(in.Argv) == 0 {
		return nil, fmt.Errorf("hkzlo8SpySubstrate: SubstrateSpawn.Argv is empty")
	}
	//nolint:gosec // G204: Argv comes from test-internal WorkLoopDepsParams; not user input
	cmd := exec.CommandContext(ctx, in.Argv[0], in.Argv[1:]...)
	cmd.Dir = in.Cwd
	cmd.Env = in.Env
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("hkzlo8SpySubstrate: StdoutPipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("hkzlo8SpySubstrate: Start: %w", err)
	}
	return &hkzlo8SpySession{cmd: cmd, stdout: stdout}, nil
}

// WriteLastPane records the paste-inject call and returns nil (success).
// The gate in workloop.go/reviewloop.go must prevent this from being called
// for ProcessExit harnesses.
func (s *hkzlo8SpySubstrate) WriteLastPane(_ context.Context, _ string, _ []byte) error {
	s.pasteCount.Add(1)
	return nil
}

var _ handler.Substrate = (*hkzlo8SpySubstrate)(nil)
var _ daemon.PasteInjecterExported = (*hkzlo8SpySubstrate)(nil)

// ─────────────────────────────────────────────────────────────────────────────
// hkzlo8SpySession — SubstrateSession backed by a real *exec.Cmd
// ─────────────────────────────────────────────────────────────────────────────

type hkzlo8SpySession struct {
	cmd    *exec.Cmd
	stdout io.Reader
}

func (s *hkzlo8SpySession) Kill(_ context.Context) error {
	if s.cmd.Process != nil {
		return s.cmd.Process.Kill()
	}
	return nil
}

func (s *hkzlo8SpySession) Wait(_ context.Context) error {
	_ = s.cmd.Wait()
	return nil
}

func (s *hkzlo8SpySession) Outcome() handler.Outcome { return handler.Outcome{} }

func (s *hkzlo8SpySession) PID() int {
	if s.cmd.Process != nil {
		return s.cmd.Process.Pid
	}
	return 0
}

func (s *hkzlo8SpySession) Stdout() io.Reader { return s.stdout }

var _ handler.SubstrateSession = (*hkzlo8SpySession)(nil)

// ─────────────────────────────────────────────────────────────────────────────
// TestPasteInject_ProcessExitHarness_SkipsPaste (hk-zlo8 regression)
// ─────────────────────────────────────────────────────────────────────────────

// TestPasteInject_ProcessExitHarness_SkipsPaste verifies that a bead dispatched
// with a ProcessExit harness (codex) does NOT call WriteLastPane on the substrate.
//
// Before hk-zlo8, pasteInjectOnLaunch was called unconditionally and hit
// "cant find pane" (no tmux pane for codex) → pasteinject_failed → no_commit.
// After hk-zlo8, the paste-inject block is gated on
// completionMode != CompletionProcessExit, so WriteLastPane is never called.
//
// Not parallel: uses t.Setenv(HARMONIK_CLAUDE_CONFIG_PATH).
func TestPasteInject_ProcessExitHarness_SkipsPaste(t *testing.T) {
	projectDir, _ := workloopFixtureProjectDir(t)
	workloopFixtureGitRepo(t, projectDir)

	claudeConfigPath := filepath.Join(t.TempDir(), ".claude.json")
	t.Setenv("HARMONIK_CLAUDE_CONFIG_PATH", claudeConfigPath)

	const beadID = core.BeadID("hkzlo8-process-exit-no-paste")
	ledger := &stubBeadLedger{ready: []core.BeadID{beadID}}
	collector := &stubEventCollector{}

	scriptPath := hkf6g7FixtureCommitScript(t, beadID)

	harnessReg, err := daemon.ExportedNewHarnessRegistry()
	if err != nil {
		t.Fatalf("ExportedNewHarnessRegistry: %v", err)
	}

	spy := &hkzlo8SpySubstrate{}

	deps := daemon.ExportedWorkLoopDeps(daemon.WorkLoopDepsParams{
		BrAdapter:    ledger,
		Bus:          collector,
		ProjectDir:   projectDir,
		IntentLogDir: filepath.Join(projectDir, ".harmonik", "beads-intents"),
		// CodexAdapter: ForAgent(codex) succeeds → code enters the else branch and
		// the hk-zlo8 completion-mode gate is checked.
		AdapterRegistry2: NewCodexSealedAdapterRegistryForTest(t),
		// HarnessRegistry with CodexHarness: Completion() == CompletionProcessExit
		// → paste-inject block is skipped (the fix under test).
		HarnessRegistry: harnessReg,
		// LaunchSpecBuilder that stamps resolvedAgentType=codex.
		LaunchSpecBuilder: daemon.ExportedCodexProcessExitLaunchSpecBuilder(scriptPath),
		AgentReadyTimeout: 2 * time.Second,
		// Spy substrate that records WriteLastPane calls.
		Substrate: spy,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	loopDone := make(chan struct{})
	go func() {
		defer close(loopDone)
		daemon.ExportedRunWorkLoop(ctx, deps)
	}()

	terminalDeadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(terminalDeadline) {
		if len(ledger.closedIDs()) > 0 || len(ledger.reopenedIDs()) > 0 {
			break
		}
		time.Sleep(25 * time.Millisecond)
	}

	cancel()
	select {
	case <-loopDone:
	case <-time.After(5 * time.Second):
		t.Error("hkzlo8: work loop did not exit within 5 s after context cancel")
	}

	closedIDs := ledger.closedIDs()
	reopenedIDs := ledger.reopenedIDs()
	pasteAttempts := spy.pasteCount.Load()
	emittedTypes := collector.eventTypes()
	t.Logf("hkzlo8: closedIDs=%v reopenedIDs=%v pasteAttempts=%d eventTypes=%v",
		closedIDs, reopenedIDs, pasteAttempts, emittedTypes)

	// ── Assertion 1: WriteLastPane NOT called (the core regression) ──────────
	if pasteAttempts > 0 {
		t.Errorf("hkzlo8 FAIL: WriteLastPane called %d time(s) for a ProcessExit harness; want 0\n"+
			"The paste-inject gate is missing — pasteInjectOnLaunch was NOT gated on "+
			"completionMode != CompletionProcessExit (hk-zlo8).",
			pasteAttempts)
	}

	// ── Assertion 2: CloseBead called (run completed successfully) ────────────
	if len(closedIDs) == 0 {
		t.Errorf("hkzlo8 FAIL: CloseBead not called; bead %q never completed (reopenedIDs=%v)",
			beadID, reopenedIDs)
	} else if closedIDs[0] != beadID {
		t.Errorf("hkzlo8 FAIL: closed bead = %q; want %q", closedIDs[0], beadID)
	}

	// ── Assertion 3: pasteinject_failed NOT emitted ───────────────────────────
	for _, et := range emittedTypes {
		if et == string(core.EventTypePasteInjectFailed) {
			t.Errorf("hkzlo8 FAIL: pasteinject_failed emitted for a ProcessExit harness; "+
				"paste-inject should have been skipped entirely (hk-zlo8)")
			break
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// TestPasteInject_InteractiveHarness_RetainsPaste (hk-zlo8 regression guard)
// ─────────────────────────────────────────────────────────────────────────────

// TestPasteInject_InteractiveHarness_RetainsPaste is the counter-test: when no
// HarnessRegistry is supplied (completionMode defaults to
// CompletionEventStreamThenQuit), the paste-inject gate is NOT applied and
// WriteLastPane IS called on the substrate.
//
// This guards against an over-broad fix that skips paste-inject for all harnesses.
//
// Not parallel: uses t.Setenv(HARMONIK_CLAUDE_CONFIG_PATH).
func TestPasteInject_InteractiveHarness_RetainsPaste(t *testing.T) {
	projectDir := pasteinjectFixtureProjectSetup(t)

	claudeConfigPath := filepath.Join(t.TempDir(), ".claude.json")
	t.Setenv("HARMONIK_CLAUDE_CONFIG_PATH", claudeConfigPath)

	const beadID = core.BeadID("hkzlo8-interactive-paste-retained")
	wtPath, parentSHA := pasteinjectFixtureWorktree(t, projectDir)
	scriptPath := pasteinjectFixtureHandlerScript(t, wtPath)

	spy := &hkzlo8SpySubstrate{}
	collector := &stubEventCollector{}

	deps := daemon.ExportedWorkLoopDeps(daemon.WorkLoopDepsParams{
		Bus:          collector,
		ProjectDir:   projectDir,
		HandlerBinary: "/bin/sh",
		HandlerArgs:  []string{scriptPath},
		IntentLogDir: filepath.Join(projectDir, ".harmonik", "beads-intents"),
		// Empty registry: ForAgent returns error → waitAgentReady skipped via
		// adapterErr path. completionMode stays CompletionEventStreamThenQuit
		// (no HarnessRegistry → default) → paste-inject gate NOT applied.
		AdapterRegistry2: NewEmptySealedAdapterRegistryForTest(t),
		// HarnessRegistry intentionally nil: completionMode defaults to
		// CompletionEventStreamThenQuit → the gate condition is false → paste
		// IS attempted (the retained behaviour under test).
		HarnessRegistry: nil,
		// Spy substrate: WriteLastPane records the paste call.
		Substrate: spy,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	result := daemon.ExportedRunReviewLoop(
		ctx, deps,
		pasteinjectFixtureRunID(t),
		beadID,
		wtPath, parentSHA,
	)

	pasteAttempts := spy.pasteCount.Load()
	t.Logf("hkzlo8 interactive: success=%v pasteAttempts=%d", result.Success, pasteAttempts)

	// ── Assertion: WriteLastPane WAS called (paste retained for non-ProcessExit) ──
	if pasteAttempts == 0 {
		t.Errorf("hkzlo8 FAIL: WriteLastPane was NOT called for an interactive harness; "+
			"paste-inject must be retained when completionMode != CompletionProcessExit.\n"+
			"The gate may be over-broad (applying to all harnesses, not just ProcessExit).")
	}
}
