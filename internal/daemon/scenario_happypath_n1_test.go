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
)

type testLogWriter struct {
	t *testing.T
}

func (w testLogWriter) Write(p []byte) (int, error) {
	w.t.Log("daemon:", strings.TrimRight(string(p), "\n"))
	return len(p), nil
}

var _ interface{ Write([]byte) (int, error) } = testLogWriter{}

func scenarioN1ProjectDir(t *testing.T) (projectDir, jsonlPath string) {
	t.Helper()
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

	bareDir := dir + "-bare"
	//nolint:gosec // G204: git args are test-internal literals; not user input
	cloneCmd := exec.CommandContext(t.Context(), "git", "clone", "--bare", dir, bareDir)
	if cloneOut, cloneErr := cloneCmd.CombinedOutput(); cloneErr != nil {
		t.Fatalf("scenarioN1GitRepo: git clone --bare: %v\n%s", cloneErr, cloneOut)
	}
	run("remote", "add", "origin", bareDir)
}

func scenarioN1BrPath(t *testing.T) string {
	t.Helper()
	brPath, err := exec.LookPath("br")
	if err != nil {
		t.Skip("br required for scenario test (not on PATH)")
	}
	return brPath
}

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

func scenarioN1InitBr(t *testing.T, realBrPath, projectDir, brWrapper string) string {
	t.Helper()
	initCmd := exec.CommandContext(t.Context(), realBrPath, "init", "--prefix", "sn1")
	initCmd.Dir = projectDir
	initOut, initErr := initCmd.CombinedOutput()
	if initErr != nil {
		t.Fatalf("scenarioN1InitBr: br init: %v\n%s", initErr, initOut)
	}
	createCmd := exec.CommandContext(t.Context(), brWrapper, "create",
		"scenario N=1 happy path", "--status", "open", "--labels", "workflow:single", "--silent")
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

func scenarioN1TwinWrapperScript(t *testing.T, twinPath string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "twin-wrapper.sh")
	content := "#!/bin/sh\nexport PATH=" + os.Getenv("PATH") + "\nexec " + twinPath +
		" --scenario commit-on-cue-startup-delay --worktree-path \"$PWD\"\n"
	//nolint:gosec // G306: script is test-only; chmod 0755 required for execution
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatalf("scenarioN1TwinWrapperScript: WriteFile: %v", err)
	}
	return path
}

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
	skipRealDaemonE2EInShort(t)

	twinPath, ok := scenariotest.TwinBinaryPath()
	if !ok {
		t.Skip("harmonik-twin-claude binary not found; set HARMONIK_TWIN_CLAUDE or build the binary")
	}

	realBrPath := scenarioN1BrPath(t)

	projectDir, jsonlPath := scenarioN1ProjectDir(t)
	// This test never calls RunConcurrentMerge, so it needs its own hook: it has
	// reached run_failed in a merge gate and printed no reason at all.
	scenariotest.ReportRunFailures(t, jsonlPath)
	scenarioN1GitRepo(t, projectDir)

	dbPath := filepath.Join(projectDir, ".beads", "beads.db")
	brWrapper := scenarioN1BrWrapperScript(t, realBrPath, dbPath)
	beadID := scenarioN1InitBr(t, realBrPath, projectDir, brWrapper)
	t.Logf("scenarioN1: seeded bead ID = %s", beadID)

	twinWrapper := scenarioN1TwinWrapperScript(t, twinPath)

	claudeConfigPath := filepath.Join(t.TempDir(), ".claude.json")
	// t.Setenv does the save-and-restore this test used to hand-roll, and it
	// checks the errors the hand-rolled version discarded. It refuses a parallel
	// test; this one is not parallel, and RunConcurrentMerge already sets the
	// same variable this way.
	t.Setenv("HARMONIK_CLAUDE_CONFIG_PATH", claudeConfigPath)

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
		WorkflowModeDefault: core.WorkflowModeDot,
	}

	startDone := make(chan error, 1)
	go func() {
		startDone <- daemon.Start(loopCtx, cfg)
	}()

	const terminalPollBudget = 20 * time.Second
	scenariotest.MustCompleteWithin(t, jsonlPath, "", nil, terminalPollBudget, func() {
		for {
			if scenariotest.WaitForEvent(t, jsonlPath, "run_completed", "", 50*time.Millisecond) ||
				scenariotest.WaitForEvent(t, jsonlPath, "run_failed", "", 50*time.Millisecond) {
				return
			}
		}
	})

	loopCancel()

	scenariotest.MustCompleteWithin(t, jsonlPath, "", nil, 5*time.Second, func() {
		if err := <-startDone; err != nil {
			t.Errorf("daemon.Start returned error after context cancel: %v", err)
		}
	})

	closed := scenarioN1PollBeadClosed(t, brWrapper, beadID, 2*time.Second)
	if !closed {
		t.Errorf("ScenarioN1: bead %s not closed within %s after terminal event", beadID, terminalPollBudget)
	}

	scenariotest.AssertEventSequence(t, jsonlPath, []scenariotest.ExpectedEvent{
		{Type: string(core.EventTypeRunStarted)},
		{Type: string(core.EventTypeHandlerCapabilities)},
		{Type: string(core.EventTypeSessionLogLocation)},
		{Type: string(core.EventTypeSkillsProvisioned)},
		{Type: string(core.EventTypeLaunchInitiated)},
		{Type: string(core.EventTypeAgentHeartbeat)},
		{Type: string(core.EventTypeAgentReady)},
		{Type: string(core.EventTypeRunCompleted)},
	})

	scenariotest.AssertBeadStatus(t, brWrapper, beadID, "closed")

	scenariotest.AssertNoOrphanTmuxWindows(t, nil)

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
