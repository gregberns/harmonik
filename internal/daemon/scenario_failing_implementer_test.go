package daemon_test

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
func (failImplNoOpTmuxAdapter) KillSession(_ context.Context, _ string) error          { return nil }
func (failImplNoOpTmuxAdapter) LoadBuffer(_ context.Context, _ string, _ []byte) error { return nil }
func (failImplNoOpTmuxAdapter) PasteBuffer(_ context.Context, _, _ string) error       { return nil }
func (failImplNoOpTmuxAdapter) SendKeysLiteral(_ context.Context, _, _ string) error   { return nil }
func (failImplNoOpTmuxAdapter) SendKeysEnter(_ context.Context, _ string) error        { return nil }
func (failImplNoOpTmuxAdapter) SendKeysQuit(_ context.Context, _ string) error         { return nil }
func (failImplNoOpTmuxAdapter) WriteToPane(_ context.Context, _, _ string, _ []byte) error {
	return nil
}

var _ tmuxPkg.Adapter = failImplNoOpTmuxAdapter{}

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

func failImplBrPath(t *testing.T) string {
	t.Helper()
	brPath, err := exec.LookPath("br")
	if err != nil {
		t.Skip("br required for scenario test (not on PATH)")
	}
	return brPath
}

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
	skipRealDaemonE2EInShort(t)

	twinPath, ok := scenariotest.TwinBinaryPath()
	if !ok {
		t.Skip("harmonik-twin-claude binary not found; set HARMONIK_TWIN_CLAUDE or build the binary")
	}

	realBrPath := failImplBrPath(t)

	projectDir, jsonlPath := failImplProjectDir(t)
	failImplGitRepo(t, projectDir)

	dbPath := filepath.Join(projectDir, ".beads", "beads.db")
	brWrapper := failImplBrWrapperScript(t, realBrPath, dbPath)
	beadID := failImplInitBr(t, realBrPath, projectDir, brWrapper)
	t.Logf("failImpl: seeded bead ID = %s", beadID)

	twinWrapper := failImplTwinWrapperScript(t, twinPath)

	claudeConfigPath := filepath.Join(t.TempDir(), ".claude.json")
	prevClaudeCfg, hadClaudeCfg := os.LookupEnv("HARMONIK_CLAUDE_CONFIG_PATH")
	if err := os.Setenv("HARMONIK_CLAUDE_CONFIG_PATH", claudeConfigPath); err != nil {
		t.Fatalf("failImpl: Setenv HARMONIK_CLAUDE_CONFIG_PATH: %v", err)
	}
	t.Cleanup(func() {
		if hadClaudeCfg {
			_ = os.Setenv("HARMONIK_CLAUDE_CONFIG_PATH", prevClaudeCfg)
		} else {
			_ = os.Unsetenv("HARMONIK_CLAUDE_CONFIG_PATH")
		}
	})

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
		AgentReadyTimeout:   5 * time.Second,
		LogWriter:           testLogWriter{t: t},
		WorkflowModeDefault: core.WorkflowModeDot,
	}

	startDone := make(chan error, 1)
	go func() {
		startDone <- daemon.Start(loopCtx, cfg)
	}()

	const runFailedPollBudget = 20 * time.Second
	if !failImplPollRunFailed(t, jsonlPath, runFailedPollBudget) {
		t.Errorf("failImpl: run_failed not found in JSONL within %s", runFailedPollBudget)
	}

	runID := failImplRunIDFromJSONL(t, jsonlPath)
	if runID == "" {
		t.Error("failImpl: run_started event not found in JSONL; cannot assert worktree removal")
	}

	loopCancel()

	select {
	case err := <-startDone:
		if err != nil {
			t.Errorf("daemon.Start returned error after context cancel: %v", err)
		}
	case <-time.After(daemon.ExportedDaemonExitHangBudget):
		t.Errorf("daemon.Start did not return within %s after context cancel", daemon.ExportedDaemonExitHangBudget)
	}

	scenariotest.AssertEventSequence(t, jsonlPath, []scenariotest.ExpectedEvent{
		{Type: string(core.EventTypeRunStarted)},
		{Type: string(core.EventTypeRunFailed)},
	})

	scenariotest.AssertNoOrphanTmuxWindowsRequired(t, failImplNoOpTmuxAdapter{})

	if runID != "" {
		scenariotest.AssertWorktreeGone(t, projectDir, runID)
	}

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
