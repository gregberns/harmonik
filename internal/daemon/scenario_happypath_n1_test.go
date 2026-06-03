package daemon_test

// scenario_happypath_n1_test.go — N=1 happy-path scenario test (hk-jf2tb).
//
// TestScenario_HappyPath_N1 exercises the full daemon stack from daemon.Start
// through a real harmonik-twin-claude subprocess running the "single-happy-path"
// canned scenario.  It asserts the expected event sequence in the JSONL log and
// verifies that the bead reaches "closed" status.
//
// # Production composition root
//
// daemon.Start is used directly (production composition root per bead hk-jf2tb).
// No test-only seams below daemon.Start are used.
//
// # Twin binary invocation
//
// harmonik-twin-claude requires specific flags (--scenario, --socket-path) but
// daemon.Start's buildClaudeLaunchSpec appends Claude-specific flags
// (--session-id etc.) that the twin does not recognise. A thin wrapper script
// is written to t.TempDir() that invokes the twin with only the flags it
// understands (--scenario single-happy-path), ignoring all other args supplied
// by the daemon. This is the idiomatic pattern for twin-via-daemon tests:
// the wrapper acts as the adaptation layer between the production composition
// root and the scenario-controlled twin.
//
// # tmux
//
// No tmux is used in this test (Substrate is nil / production exec path).
// AssertNoOrphanTmuxWindows is called with a nil adapter so the assertion is
// a no-op in non-tmux environments per the scenariotest contract.
//
// # Expected event sequence (JSONL, subsequence check)
//
//   run_started → handler_capabilities → session_log_location →
//   skills_provisioned → launch_initiated → agent_ready →
//   agent_heartbeat → run_completed
//
// The daemon emits handler_capabilities / session_log_location / skills_provisioned
// / launch_initiated from buildClaudeLaunchSpec PreExecMessages (step 3) BEFORE
// the subprocess starts. The twin then additionally emits these events on stdout
// when driven by the single-happy-path scenario; both sets appear in the JSONL.
// AssertEventSequence performs a subsequence check so duplicates do not cause
// failures (earlier occurrence is matched first).
//
// Helper prefix: scenarioN1 (bead hk-jf2tb; per implementer-protocol.md
// §Helper-prefix discipline).
//
// Spec refs:
//   - specs/scenario-harness.md §4 (assertion vocabulary)
//   - specs/handler-contract.md §4.6 CHB-018 (event ordering)
//   - specs/event-model.md §8.1, §8.3
//
// Bead: hk-jf2tb.

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
)

// testLogWriter adapts *testing.T to io.Writer so daemon log output is
// captured in the test log and visible with -v.
type testLogWriter struct {
	t *testing.T
}

func (w testLogWriter) Write(p []byte) (int, error) {
	w.t.Log("daemon:", strings.TrimRight(string(p), "\n"))
	return len(p), nil
}

// ─────────────────────────────────────────────────────────────────────────────
// Compile-time: ensure testLogWriter satisfies io.Writer.
var _ interface{ Write([]byte) (int, error) } = testLogWriter{}

// ─────────────────────────────────────────────────────────────────────────────
// scenarioN1 fixture helpers
// ─────────────────────────────────────────────────────────────────────────────

// scenarioN1ProjectDir creates the minimal project directory for the scenario
// test: .harmonik/events/, .harmonik/beads-intents/. Returns the project dir
// and the JSONL events log path.
func scenarioN1ProjectDir(t *testing.T) (projectDir, jsonlPath string) {
	t.Helper()
	// Resolve symlinks so that br receives the canonical path (macOS /var → /private/var).
	raw := t.TempDir()
	resolved, resolveErr := filepath.EvalSymlinks(raw)
	if resolveErr != nil {
		t.Fatalf("scenarioN1ProjectDir: EvalSymlinks %q: %v", raw, resolveErr)
	}
	projectDir = resolved
	for _, sub := range []string{
		filepath.Join(".harmonik", "events"),
		filepath.Join(".harmonik", "beads-intents"),
	} {
		//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
		if err := os.MkdirAll(filepath.Join(projectDir, sub), 0o755); err != nil {
			t.Fatalf("scenarioN1ProjectDir: mkdir %s: %v", sub, err)
		}
	}
	jsonlPath = filepath.Join(projectDir, ".harmonik", "events", "events.jsonl")
	return projectDir, jsonlPath
}

// scenarioN1GitRepo initialises a bare git repository with one commit in dir.
func scenarioN1GitRepo(t *testing.T, dir string) {
	t.Helper()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.CommandContext(t.Context(), "git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("scenarioN1GitRepo: git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "--initial-branch=main")
	run("config", "user.email", "test@harmonik.local")
	run("config", "user.name", "Harmonik Test")
	readmePath := filepath.Join(dir, "README")
	if err := os.WriteFile(readmePath, []byte("scenario N=1 test\n"), 0o644); err != nil {
		t.Fatalf("scenarioN1GitRepo: WriteFile README: %v", err)
	}
	run("add", "README")
	run("commit", "-m", "Initial commit")
}

// scenarioN1BrPath returns the path to the real `br` binary, skipping the test
// when br is not on PATH.
func scenarioN1BrPath(t *testing.T) string {
	t.Helper()
	brPath, err := exec.LookPath("br")
	if err != nil {
		t.Skip("br required for scenario test (not on PATH)")
	}
	return brPath
}

// scenarioN1BrWrapperScript writes a /bin/sh wrapper that invokes realBrPath
// with --db <dbPath> prepended to all args. Returns the wrapper path.
func scenarioN1BrWrapperScript(t *testing.T, realBrPath, dbPath string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "br")
	content := "#!/bin/sh\nexec " + realBrPath + " --db " + dbPath + " \"$@\"\n"
	//nolint:gosec // G306: script is test-only; chmod 0755 required for execution
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatalf("scenarioN1BrWrapperScript: WriteFile: %v", err)
	}
	return path
}

// scenarioN1InitBr initialises a beads workspace in projectDir, creates one
// ready bead, and returns its ID.
func scenarioN1InitBr(t *testing.T, realBrPath, projectDir, brWrapper string) string {
	t.Helper()
	initCmd := exec.CommandContext(t.Context(), realBrPath, "init", "--prefix", "sn1")
	initCmd.Dir = projectDir
	initOut, initErr := initCmd.CombinedOutput()
	if initErr != nil {
		t.Fatalf("scenarioN1InitBr: br init: %v\n%s", initErr, initOut)
	}
	createCmd := exec.CommandContext(t.Context(), brWrapper, "create",
		"scenario N=1 happy path", "--status", "open", "--silent")
	createOut, createErr := createCmd.CombinedOutput()
	if createErr != nil {
		t.Fatalf("scenarioN1InitBr: br create: %v\n%s", createErr, createOut)
	}
	id := strings.TrimSpace(string(createOut))
	if id == "" {
		t.Fatal("scenarioN1InitBr: br create returned empty ID")
	}
	return id
}

// scenarioN1TwinWrapperScript writes a /bin/sh wrapper script that invokes the
// twin binary with --scenario single-happy-path, ignoring all other args
// passed by daemon.Start's buildClaudeLaunchSpec (e.g. --session-id).
//
// The wrapper is the adaptation layer between the production composition root
// (which appends Claude-specific flags) and the twin binary (which only
// understands its own flags).
func scenarioN1TwinWrapperScript(t *testing.T, twinPath string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "twin-wrapper.sh")
	// Ignore all args; invoke the twin with only --scenario.
	content := "#!/bin/sh\nexec " + twinPath + " --scenario single-happy-path\n"
	//nolint:gosec // G306: script is test-only; chmod 0755 required for execution
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatalf("scenarioN1TwinWrapperScript: WriteFile: %v", err)
	}
	return path
}

// scenarioN1PollBeadClosed polls `br show <id>` every 10 ms for up to budget.
// Returns true if the bead reaches "closed" status.
func scenarioN1PollBeadClosed(t *testing.T, brWrapper, beadID string, budget time.Duration) bool {
	t.Helper()
	deadline := time.Now().Add(budget)
	for time.Now().Before(deadline) {
		cmd := exec.CommandContext(t.Context(), brWrapper, "show", beadID, "--format", "json")
		out, err := cmd.Output()
		if err == nil {
			var items []struct {
				Status string `json:"status"`
			}
			if json.Unmarshal(out, &items) == nil && len(items) > 0 {
				if items[0].Status == "closed" {
					return true
				}
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	return false
}

// scenarioN1PollRunTerminal polls the JSONL log for a run_completed or
// run_failed event for up to budget. Returns true when found.
func scenarioN1PollRunTerminal(t *testing.T, jsonlPath string, budget time.Duration) bool {
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
				if strings.Contains(line, string(core.EventTypeRunCompleted)) ||
					strings.Contains(line, string(core.EventTypeRunFailed)) {
					found = true
					break
				}
			}
			if closeErr := f.Close(); closeErr != nil {
				t.Logf("scenarioN1PollRunTerminal: close: %v", closeErr)
			}
			if found {
				return true
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	return false
}

// ─────────────────────────────────────────────────────────────────────────────
// TestScenario_HappyPath_N1
// ─────────────────────────────────────────────────────────────────────────────

// TestScenario_HappyPath_N1 is the N=1 happy-path scenario test.
//
// Setup:
//  1. TempDir project with git + br DB.
//  2. One ready bead seeded via br create.
//  3. daemon.Start wired with harmonik-twin-claude wrapper as HandlerBinary.
//
// Assertions:
//  1. Full event subsequence: run_started → handler_capabilities →
//     session_log_location → skills_provisioned → launch_initiated →
//     agent_ready → agent_heartbeat → run_completed.
//  2. Bead status == "closed" in the br DB.
//  3. No orphan tmux windows (nil adapter → skip in non-tmux env).
//
// Bead: hk-jf2tb.
func TestScenario_HappyPath_N1(t *testing.T) {
	// Not parallel: uses os.Setenv(HARMONIK_CLAUDE_CONFIG_PATH) to isolate
	// EnsureWorktreeTrust from the running harmonik daemon's ~/.claude.json.lock.
	// Parallelism would race on the process-wide env var across concurrent scenario tests.

	// Locate the twin binary; skip when absent.
	twinPath, ok := scenariotest.TwinBinaryPath()
	if !ok {
		t.Skip("harmonik-twin-claude binary not found; set HARMONIK_TWIN_CLAUDE or build the binary")
	}

	// Locate br binary.
	realBrPath := scenarioN1BrPath(t)

	// Create project directory with git repo.
	projectDir, jsonlPath := scenarioN1ProjectDir(t)
	scenarioN1GitRepo(t, projectDir)

	// Initialise br DB and seed one ready bead.
	dbPath := filepath.Join(projectDir, ".beads", "beads.db")
	brWrapper := scenarioN1BrWrapperScript(t, realBrPath, dbPath)
	beadID := scenarioN1InitBr(t, realBrPath, projectDir, brWrapper)
	t.Logf("scenarioN1: seeded bead ID = %s", beadID)

	// Build the twin wrapper script (ignores Claude-specific flags).
	twinWrapper := scenarioN1TwinWrapperScript(t, twinPath)

	// Redirect EnsureWorktreeTrust to a test-local ~/.claude.json so the test
	// does not contend with a running harmonik daemon on ~/.claude.json.lock.
	// The HARMONIK_CLAUDE_CONFIG_PATH env var is honoured by workspace.EnsureWorktreeTrust
	// (workspace/claudetrust_wm040b.go defaultClaudeGlobalConfigPath).
	//
	// Note: t.Setenv cannot be used with t.Parallel(), so we use os.Setenv with
	// a manual cleanup registration.  Each test gets a unique t.TempDir() path so
	// parallel invocations do not race on the env var value.
	claudeConfigPath := filepath.Join(t.TempDir(), ".claude.json")
	if err := os.Setenv("HARMONIK_CLAUDE_CONFIG_PATH", claudeConfigPath); err != nil {
		t.Fatalf("scenarioN1: Setenv HARMONIK_CLAUDE_CONFIG_PATH: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Unsetenv("HARMONIK_CLAUDE_CONFIG_PATH"); err != nil {
			t.Logf("scenarioN1: Unsetenv HARMONIK_CLAUDE_CONFIG_PATH: %v", err)
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
		// Short agent_ready timeout for scenario tests: the twin emits agent_ready
		// via the watcher so the timeout only fires when the watcher exits before
		// agent_ready is processed.  5 s leaves a comfortable margin above the
		// watcher-done → readyCancel race (watcher finishes in <1 s; the race window
		// is the OS scheduler quantum) while keeping the test under 10 s in CI.
		AgentReadyTimeout: 5 * time.Second,
		// LogWriter: direct daemon logs to test output for debugging.
		LogWriter:           testLogWriter{t: t},
		WorkflowModeDefault: core.WorkflowModeReviewLoop,
	}

	// Launch daemon.Start in a goroutine.
	startDone := make(chan error, 1)
	go func() {
		startDone <- daemon.Start(loopCtx, cfg)
	}()

	// Poll until a terminal JSONL event (run_completed or run_failed) appears.
	//
	// Budget = AgentReadyTimeout (5 s) + socketGrace (3 s) + CloseBead budget (5 s) + headroom.
	// We poll for the JSONL terminal event BEFORE cancelling the daemon context so
	// that CloseBead is not interrupted mid-retry (which would produce run_failed
	// instead of run_completed and leave the bead in_progress).
	const terminalPollBudget = 20 * time.Second
	scenariotest.MustCompleteWithin(t, jsonlPath, "", nil, terminalPollBudget, func() {
		for {
			if scenariotest.WaitForEvent(t, jsonlPath, "run_completed", "", 50*time.Millisecond) ||
				scenariotest.WaitForEvent(t, jsonlPath, "run_failed", "", 50*time.Millisecond) {
				return
			}
		}
	})

	// Stop the work loop now that the run has reached a terminal JSONL event.
	loopCancel()

	// Wait for daemon.Start to return (up to 5 s).
	scenariotest.MustCompleteWithin(t, jsonlPath, "", nil, 5*time.Second, func() {
		if err := <-startDone; err != nil {
			t.Errorf("daemon.Start returned error after context cancel: %v", err)
		}
	})

	// ── Assertion 1: bead closed ─────────────────────────────────────────────

	closed := scenarioN1PollBeadClosed(t, brWrapper, beadID, 2*time.Second)
	if !closed {
		t.Errorf("ScenarioN1: bead %s not closed within %s after terminal event", beadID, terminalPollBudget)
	}

	// ── Assertion 2: expected event sequence ─────────────────────────────────
	//
	// AssertEventSequence performs a subsequence check over the JSONL:
	// each required event must appear after all preceding required events.
	// Duplicate events (e.g. handler_capabilities emitted by the daemon
	// pre-exec AND by the twin) do not cause failures — the first occurrence
	// is matched.

	scenariotest.AssertEventSequence(t, jsonlPath, []scenariotest.ExpectedEvent{
		{Type: string(core.EventTypeRunStarted)},
		{Type: string(core.EventTypeHandlerCapabilities)},
		{Type: string(core.EventTypeSessionLogLocation)},
		{Type: string(core.EventTypeSkillsProvisioned)},
		{Type: string(core.EventTypeLaunchInitiated)},
		{Type: string(core.EventTypeAgentReady)},
		{Type: string(core.EventTypeAgentHeartbeat)},
		{Type: string(core.EventTypeRunCompleted)},
	})

	// ── Assertion 3: br status == closed ─────────────────────────────────────

	scenariotest.AssertBeadStatus(t, brWrapper, beadID, "closed")

	// ── Assertion 4: no orphan tmux windows ──────────────────────────────────
	// nil adapter → skipped in non-tmux test environments per scenariotest contract.

	scenariotest.AssertNoOrphanTmuxWindows(t, nil)

	// ── Assertion 5: causality invariants (hk-xegej) ─────────────────────────
	// run_started must be followed by a terminal run event within 60 s.
	// implementer_commit (when present) must be followed by reviewer_launched or
	// run_completed within 30 s; passes vacuously when implementer_commit is absent.
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

	t.Logf("ScenarioN1: PASS bead=%s", beadID)
}
