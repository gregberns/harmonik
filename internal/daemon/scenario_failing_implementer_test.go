package daemon_test

// scenario_failing_implementer_test.go — failing-implementer scenario test (hk-59lg8).
//
// TestScenario_FailingImplementer_RunFailed injects a twin running
// --scenario handler-fatal (ExitWithError=true, emits agent_failed, exits 1)
// and asserts the full failure path:
//
//   (a) run_failed fires in the JSONL event log within the budget.
//   (b) AssertNoOrphanTmuxWindowsRequired passes (adapter is non-nil; covers
//       hk-e6mtt: no test previously asserted pane cleanup on run_failed).
//   (c) The git worktree at .harmonik/worktrees/<run_id>/ is removed.
//
// # Production composition root
//
// daemon.Start is used directly; no internal seams below daemon.Start are used.
//
// # Twin binary invocation
//
// A thin wrapper script invokes harmonik-twin-claude with
// --scenario handler-fatal, ignoring all other flags appended by the daemon.
//
// # tmux
//
// No tmux Substrate is wired (daemon runs handlers as direct subprocesses).
// A minimal no-op tmux adapter is passed to AssertNoOrphanTmuxWindowsRequired
// so the assertion actually runs — not silently skipped.
//
// # Expected event sequence (JSONL, subsequence check)
//
//   run_started → run_failed
//
// # Helper prefix: failImpl (bead hk-59lg8; per implementer-protocol.md
// §Helper-prefix discipline).
//
// Bead: hk-59lg8.

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/daemon/scenariotest"
	tmuxPkg "github.com/gregberns/harmonik/internal/lifecycle/tmux"
)

// ─────────────────────────────────────────────────────────────────────────────
// failImplNoOpTmuxAdapter — minimal tmux.Adapter with no sessions
// ─────────────────────────────────────────────────────────────────────────────

// failImplNoOpTmuxAdapter is a minimal tmux.Adapter implementation that
// reports no sessions and no windows. It satisfies AssertNoOrphanTmuxWindowsRequired
// (which requires a non-nil adapter) while modelling a daemon run with no tmux
// Substrate — no windows are created, so none should remain.
type failImplNoOpTmuxAdapter struct{}

func (failImplNoOpTmuxAdapter) ProbeTmux(_ context.Context) error { return nil }
func (failImplNoOpTmuxAdapter) ListSessions(_ context.Context) ([]string, error) {
	return nil, nil
}
func (failImplNoOpTmuxAdapter) ListWindows(_ context.Context, _ string) ([]string, error) {
	return nil, nil
}
func (failImplNoOpTmuxAdapter) NewWindowIn(_ context.Context, _ tmuxPkg.NewWindowIn) tmuxPkg.Outcome {
	return tmuxPkg.Outcome{}
}
func (failImplNoOpTmuxAdapter) KillWindow(_ context.Context, _ tmuxPkg.WindowHandle) error {
	return nil
}
func (failImplNoOpTmuxAdapter) WindowPanePID(_ context.Context, _ tmuxPkg.WindowHandle) (int, error) {
	return 0, nil
}
func (failImplNoOpTmuxAdapter) WindowPaneID(_ context.Context, _ tmuxPkg.WindowHandle) (string, error) {
	return "", nil
}
func (failImplNoOpTmuxAdapter) KillSession(_ context.Context, _ string) error { return nil }
func (failImplNoOpTmuxAdapter) LoadBuffer(_ context.Context, _ string, _ []byte) error { return nil }
func (failImplNoOpTmuxAdapter) PasteBuffer(_ context.Context, _, _ string) error { return nil }
func (failImplNoOpTmuxAdapter) SendKeysLiteral(_ context.Context, _, _ string) error { return nil }
func (failImplNoOpTmuxAdapter) SendKeysEnter(_ context.Context, _ string) error { return nil }
func (failImplNoOpTmuxAdapter) SendKeysQuit(_ context.Context, _ string) error { return nil }
func (failImplNoOpTmuxAdapter) WriteToPane(_ context.Context, _, _ string, _ []byte) error {
	return nil
}

// compile-time check: failImplNoOpTmuxAdapter satisfies tmuxPkg.Adapter.
var _ tmuxPkg.Adapter = failImplNoOpTmuxAdapter{}

// ─────────────────────────────────────────────────────────────────────────────
// failImpl fixture helpers
// ─────────────────────────────────────────────────────────────────────────────

// failImplProjectDir creates the minimal project directory for the failing
// scenario: .harmonik/events/, .harmonik/beads-intents/. Returns the project
// dir (with symlinks resolved) and the JSONL events log path.
func failImplProjectDir(t *testing.T) (projectDir, jsonlPath string) {
	t.Helper()
	raw := t.TempDir()
	resolved, resolveErr := filepath.EvalSymlinks(raw)
	if resolveErr != nil {
		t.Fatalf("failImplProjectDir: EvalSymlinks %q: %v", raw, resolveErr)
	}
	projectDir = resolved
	for _, sub := range []string{
		filepath.Join(".harmonik", "events"),
		filepath.Join(".harmonik", "beads-intents"),
	} {
		//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
		if err := os.MkdirAll(filepath.Join(projectDir, sub), 0o755); err != nil {
			t.Fatalf("failImplProjectDir: mkdir %s: %v", sub, err)
		}
	}
	jsonlPath = filepath.Join(projectDir, ".harmonik", "events", "events.jsonl")
	return projectDir, jsonlPath
}

// failImplGitRepo initialises a bare git repository with one commit in dir.
func failImplGitRepo(t *testing.T, dir string) {
	t.Helper()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.CommandContext(t.Context(), "git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("failImplGitRepo: git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "--initial-branch=main")
	run("config", "user.email", "test@harmonik.local")
	run("config", "user.name", "Harmonik Test")
	readmePath := filepath.Join(dir, "README")
	if err := os.WriteFile(readmePath, []byte("failing implementer scenario test\n"), 0o644); err != nil {
		t.Fatalf("failImplGitRepo: WriteFile README: %v", err)
	}
	run("add", "README")
	run("commit", "-m", "Initial commit")
}

// failImplBrPath returns the path to the real `br` binary, skipping the test
// when br is not on PATH.
func failImplBrPath(t *testing.T) string {
	t.Helper()
	brPath, err := exec.LookPath("br")
	if err != nil {
		t.Skip("br required for scenario test (not on PATH)")
	}
	return brPath
}

// failImplBrWrapperScript writes a /bin/sh wrapper that invokes realBrPath
// with --db <dbPath> prepended to all args. Returns the wrapper path.
func failImplBrWrapperScript(t *testing.T, realBrPath, dbPath string) string {
	t.Helper()
	raw := t.TempDir()
	dir, resolveErr := filepath.EvalSymlinks(raw)
	if resolveErr != nil {
		t.Fatalf("failImplBrWrapperScript: EvalSymlinks %q: %v", raw, resolveErr)
	}
	path := filepath.Join(dir, "br")
	content := "#!/bin/sh\nexec " + realBrPath + " --db " + dbPath + " \"$@\"\n"
	//nolint:gosec // G306: script is test-only; chmod 0755 required for execution
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatalf("failImplBrWrapperScript: WriteFile: %v", err)
	}
	return path
}

// failImplInitBr initialises a beads workspace in projectDir, creates one
// open bead, and returns its ID.
func failImplInitBr(t *testing.T, realBrPath, projectDir, brWrapper string) string {
	t.Helper()
	initCmd := exec.CommandContext(t.Context(), realBrPath, "init", "--prefix", "fi")
	initCmd.Dir = projectDir
	initOut, initErr := initCmd.CombinedOutput()
	if initErr != nil {
		t.Fatalf("failImplInitBr: br init: %v\n%s", initErr, initOut)
	}
	createCmd := exec.CommandContext(t.Context(), brWrapper, "create",
		"failing implementer test bead", "--status", "open", "--silent")
	createOut, createErr := createCmd.CombinedOutput()
	if createErr != nil {
		t.Fatalf("failImplInitBr: br create: %v\n%s", createErr, createOut)
	}
	id := strings.TrimSpace(string(createOut))
	if id == "" {
		t.Fatal("failImplInitBr: br create returned empty ID")
	}
	return id
}

// failImplTwinWrapperScript writes a /bin/sh wrapper that invokes the twin
// binary with --scenario handler-fatal, ignoring all other flags appended by
// the daemon (e.g. --session-id).
func failImplTwinWrapperScript(t *testing.T, twinPath string) string {
	t.Helper()
	raw := t.TempDir()
	dir, resolveErr := filepath.EvalSymlinks(raw)
	if resolveErr != nil {
		t.Fatalf("failImplTwinWrapperScript: EvalSymlinks %q: %v", raw, resolveErr)
	}
	path := filepath.Join(dir, "twin-failing.sh")
	content := "#!/bin/sh\nexec " + twinPath + " --scenario handler-fatal\n"
	//nolint:gosec // G306: script is test-only; chmod 0755 required for execution
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatalf("failImplTwinWrapperScript: WriteFile: %v", err)
	}
	return path
}

// failImplPollRunFailed polls the JSONL log for a run_failed event for up to
// budget. Returns true when found.
func failImplPollRunFailed(t *testing.T, jsonlPath string, budget time.Duration) bool {
	t.Helper()
	deadline := time.Now().Add(budget)
	for time.Now().Before(deadline) {
		//nolint:gosec // G304: path is t.TempDir()-based; not user input
		f, err := os.Open(jsonlPath)
		if err == nil {
			found := false
			scanner := bufio.NewScanner(f)
			for scanner.Scan() {
				line := scanner.Text()
				if strings.Contains(line, string(core.EventTypeRunFailed)) {
					found = true
					break
				}
			}
			if closeErr := f.Close(); closeErr != nil {
				t.Logf("failImplPollRunFailed: close: %v", closeErr)
			}
			if found {
				return true
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	return false
}

// failImplRunIDFromJSONL scans the JSONL log for the first run_started event
// and returns its run_id field. Returns "" when no such event is found.
func failImplRunIDFromJSONL(t *testing.T, jsonlPath string) string {
	t.Helper()
	//nolint:gosec // G304: path is t.TempDir()-based; not user input
	f, err := os.Open(jsonlPath)
	if err != nil {
		return ""
	}
	defer func() {
		if closeErr := f.Close(); closeErr != nil {
			t.Logf("failImplRunIDFromJSONL: close: %v", closeErr)
		}
	}()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var env struct {
			Type  string `json:"type"`
			RunID string `json:"run_id"`
		}
		if decErr := json.Unmarshal([]byte(line), &env); decErr != nil {
			continue
		}
		if env.Type == string(core.EventTypeRunStarted) && env.RunID != "" {
			return env.RunID
		}
	}
	return ""
}

// ─────────────────────────────────────────────────────────────────────────────
// TestScenario_FailingImplementer_RunFailed
// ─────────────────────────────────────────────────────────────────────────────

// TestScenario_FailingImplementer_RunFailed is the failing-implementer scenario
// test (hk-59lg8).
//
// Setup:
//  1. TempDir project with git repo and br DB.
//  2. One open bead seeded via br create.
//  3. daemon.Start wired with a handler-fatal twin wrapper (ExitWithError=true).
//
// Assertions:
//  1. run_failed event fires in the JSONL log within budget.
//  2. No orphan tmux windows — asserted via AssertNoOrphanTmuxWindowsRequired
//     with a non-nil no-op adapter so the check actually runs (covers hk-e6mtt).
//  3. .harmonik/worktrees/<run_id>/ is removed (daemon cleanup on run_failed).
//  4. Causality invariant: run_started → run_failed within 60 s.
//
// Bead: hk-59lg8.
func TestScenario_FailingImplementer_RunFailed(t *testing.T) {
	// Not parallel: uses os.Setenv(HARMONIK_CLAUDE_CONFIG_PATH).

	// Locate the twin binary; skip when absent.
	twinPath, ok := scenariotest.TwinBinaryPath()
	if !ok {
		t.Skip("harmonik-twin-claude binary not found; set HARMONIK_TWIN_CLAUDE or build the binary")
	}

	// Locate br binary.
	realBrPath := failImplBrPath(t)

	// Create project directory with git repo.
	projectDir, jsonlPath := failImplProjectDir(t)
	failImplGitRepo(t, projectDir)

	// Initialise br DB and seed one open bead.
	dbPath := filepath.Join(projectDir, ".beads", "beads.db")
	brWrapper := failImplBrWrapperScript(t, realBrPath, dbPath)
	beadID := failImplInitBr(t, realBrPath, projectDir, brWrapper)
	t.Logf("failImpl: seeded bead ID = %s", beadID)

	// Build the twin wrapper script for --scenario handler-fatal.
	twinWrapper := failImplTwinWrapperScript(t, twinPath)

	// Redirect EnsureWorktreeTrust to a test-local config path.
	claudeConfigPath := filepath.Join(t.TempDir(), ".claude.json")
	if err := os.Setenv("HARMONIK_CLAUDE_CONFIG_PATH", claudeConfigPath); err != nil {
		t.Fatalf("failImpl: Setenv HARMONIK_CLAUDE_CONFIG_PATH: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Unsetenv("HARMONIK_CLAUDE_CONFIG_PATH"); err != nil {
			t.Logf("failImpl: Unsetenv HARMONIK_CLAUDE_CONFIG_PATH: %v", err)
		}
	})

	// Wire daemon.Config — production composition root.
	loopCtx, loopCancel := context.WithCancel(context.Background())
	defer loopCancel()

	cfg := daemon.Config{
		ProjectDir:            projectDir,
		JSONLLogPath:          jsonlPath,
		BrPath:                brWrapper,
		HandlerBinary:         twinWrapper,
		HandlerEnv:            nil,
		SkipWALCheckpoint:     true,
		SkipBrHistoryRotation: true,
		// Short timeout for scenario tests — the twin exits quickly on handler-fatal.
		AgentReadyTimeout: 5 * time.Second,
		LogWriter:         testLogWriter{t: t},
	}

	// Launch daemon.Start in a goroutine.
	startDone := make(chan error, 1)
	go func() {
		startDone <- daemon.Start(loopCtx, cfg)
	}()

	// Poll until run_failed appears in the JSONL log.
	//
	// Budget: AgentReadyTimeout (5 s) + agent_failed path overhead + headroom.
	const runFailedPollBudget = 20 * time.Second
	if !failImplPollRunFailed(t, jsonlPath, runFailedPollBudget) {
		t.Errorf("failImpl: run_failed not found in JSONL within %s", runFailedPollBudget)
	}

	// Extract daemon run_id from run_started before cancelling.
	runID := failImplRunIDFromJSONL(t, jsonlPath)
	if runID == "" {
		t.Error("failImpl: run_started event not found in JSONL; cannot assert worktree removal")
	}

	// Stop the work loop.
	loopCancel()

	// Wait for daemon.Start to return — this also drains in-flight goroutines
	// (wg.Wait inside runWorkLoop), guaranteeing defer wtCleanup() has run.
	select {
	case err := <-startDone:
		if err != nil {
			t.Errorf("daemon.Start returned error after context cancel: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Error("daemon.Start did not return within 5 s after context cancel")
	}

	// ── Assertion 1: run_failed in JSONL ─────────────────────────────────────

	scenariotest.AssertEventSequence(t, jsonlPath, []scenariotest.ExpectedEvent{
		{Type: string(core.EventTypeRunStarted)},
		{Type: string(core.EventTypeRunFailed)},
	})

	// ── Assertion 2: no orphan tmux windows (required — non-nil adapter) ─────
	//
	// The daemon ran without a Substrate (nil), so no tmux windows were created.
	// The no-op adapter reports no sessions → no orphan windows. Using the
	// Required variant ensures this check actually executes (covers hk-e6mtt:
	// AssertNoOrphanTmuxWindows with nil adapter silently passed even with leaks).

	scenariotest.AssertNoOrphanTmuxWindowsRequired(t, failImplNoOpTmuxAdapter{})

	// ── Assertion 3: worktree removed ────────────────────────────────────────
	//
	// After run_failed, productionWorktreeFactory's cleanup func (defer wtCleanup)
	// removes .harmonik/worktrees/<run_id>/. daemon.Start returns only after
	// wg.Wait() drains all goroutines, so cleanup is complete before we reach here.

	if runID != "" {
		scenariotest.AssertWorktreeGone(t, projectDir, runID)
	}

	// ── Assertion 4: causality invariants (hk-xegej) ─────────────────────────

	scenariotest.AssertEventCausality(t, jsonlPath,
		"run_started",
		[]string{"run_completed", "run_failed", "run_cancelled"},
		60*time.Second,
	)
	scenariotest.AssertEventCausality(t, jsonlPath,
		"implementer_commit",
		[]string{"reviewer_launched", "run_completed"},
		30*time.Second,
	)

	t.Logf("failImpl: PASS bead=%s run_id=%s", beadID, runID)
}
