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
)

func smokeFixtureProjectDir(t *testing.T) (projectDir, jsonlPath string) {
	t.Helper()
	raw := t.TempDir()
	resolved, resolveErr := filepath.EvalSymlinks(raw)
	if resolveErr != nil {
		t.Fatalf("smokeFixtureProjectDir: EvalSymlinks %q: %v", raw, resolveErr)
	}
	projectDir = resolved
	eventsDir := filepath.Join(projectDir, ".harmonik", "events")
	//nolint:gosec // G301: test-only temp directory; not production
	if err := os.MkdirAll(eventsDir, 0o755); err != nil {
		t.Fatalf("smokeFixtureProjectDir: mkdir events: %v", err)
	}
	intentsDir := filepath.Join(projectDir, ".harmonik", "beads-intents")
	//nolint:gosec // G301: test-only temp directory; not production
	if err := os.MkdirAll(intentsDir, 0o755); err != nil {
		t.Fatalf("smokeFixtureProjectDir: mkdir beads-intents: %v", err)
	}
	jsonlPath = filepath.Join(eventsDir, "events.jsonl")
	return projectDir, jsonlPath
}

func smokeFixtureGitRepo(t *testing.T, dir string) {
	t.Helper()
	run := func(d string, args ...string) {
		t.Helper()
		cmd := exec.CommandContext(t.Context(), "git", args...)
		cmd.Dir = d
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("smokeFixtureGitRepo: git %v: %v\n%s", args, err, out)
		}
	}
	run(dir, "init", "--initial-branch=main")
	run(dir, "config", "user.email", "test@harmonik.local")
	run(dir, "config", "user.name", "Harmonik Test")
	initFile := filepath.Join(dir, "README")
	if err := os.WriteFile(initFile, []byte("harmonik smoke test repo\n"), 0o644); err != nil {
		t.Fatalf("smokeFixtureGitRepo: WriteFile: %v", err)
	}
	run(dir, "add", "README")
	run(dir, "commit", "-m", "Initial commit")

	originDir := t.TempDir()
	run(originDir, "init", "--bare", "--initial-branch=main")
	run(dir, "remote", "add", "origin", originDir)
	run(dir, "push", "origin", "main")
}

func smokeFixtureBrPath(t *testing.T) string {
	t.Helper()
	brPath, err := exec.LookPath("br")
	if err != nil {
		t.Skip("br required for smoke test (not on PATH); CI sets br on PATH")
	}
	return brPath
}

func smokeFixtureBrWrapperScript(t *testing.T, realBrPath, dbPath string) string {
	t.Helper()
	scriptDir := t.TempDir()
	scriptPath := filepath.Join(scriptDir, "br")
	content := "#!/bin/sh\nexec " + realBrPath + " --db " + dbPath + " \"$@\"\n"
	//nolint:gosec // G306: script is test-only, chmod 0755 required for execution
	if err := os.WriteFile(scriptPath, []byte(content), 0o755); err != nil {
		t.Fatalf("smokeFixtureBrWrapperScript: WriteFile: %v", err)
	}
	return scriptPath
}

func smokeFixtureHandlerScript(t *testing.T) string {
	t.Helper()
	scriptDir := t.TempDir()
	scriptPath := filepath.Join(scriptDir, "handler.sh")
	content := `#!/bin/sh
set -e
bead_id=$(grep '^bead_id:' .harmonik/agent-task.md | awk '{print $2}') 2>/dev/null
echo "smoke: ${bead_id}" > "smoke-${bead_id}.txt"
git add "smoke-${bead_id}.txt" >&2
git commit -m "smoke: handler commit for ${bead_id}

Refs: ${bead_id}" >&2
exit 0
`
	//nolint:gosec // G306: script is test-only, chmod 0755 required for execution
	if err := os.WriteFile(scriptPath, []byte(content), 0o755); err != nil {
		t.Fatalf("smokeFixtureHandlerScript: WriteFile: %v", err)
	}
	return scriptPath
}

func smokeFixtureInitBr(t *testing.T, realBrPath, projectDir, brWrapperPath string) string {
	t.Helper()

	initCmd := exec.CommandContext(t.Context(), realBrPath, "init", "--prefix", "sm")
	initCmd.Dir = projectDir
	initOut, initErr := initCmd.CombinedOutput()
	if initErr != nil {
		t.Fatalf("smokeFixtureInitBr: br init in %s: %v\n%s", projectDir, initErr, initOut)
	}

	createCmd := exec.CommandContext(t.Context(), brWrapperPath,
		"create", "smoke test bead", "--status", "open", "--labels", "workflow:single", "--silent")
	createOut, createErr := createCmd.CombinedOutput()
	if createErr != nil {
		t.Fatalf("smokeFixtureInitBr: br create: %v\n%s", createErr, createOut)
	}
	id := strings.TrimSpace(string(createOut))
	if id == "" {
		t.Fatal("smokeFixtureInitBr: br create returned empty ID")
	}
	return id
}

func smokeFixtureReadJSONLLines(t *testing.T, path string) []string {
	t.Helper()
	//nolint:gosec // G304: path is t.TempDir()-based; not user input
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("smokeFixtureReadJSONLLines: open %s: %v", path, err)
	}
	defer func() { _ = f.Close() }()
	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		if line := strings.TrimSpace(scanner.Text()); line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

func smokeFixturePollBeadClosed(t *testing.T, brWrapperPath, beadID string, budget time.Duration) bool {
	t.Helper()
	deadline := time.Now().Add(budget)
	for time.Now().Before(deadline) {
		cmd := exec.CommandContext(t.Context(), brWrapperPath, "show", beadID, "--format", "json")
		out, err := cmd.Output()
		if err == nil {
			var items []struct {
				Status string `json:"status"`
			}
			if jsonErr := json.Unmarshal(out, &items); jsonErr == nil && len(items) == 1 {
				if items[0].Status == "closed" {
					return true
				}
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	return false
}

func smokeFixturePollRunTerminal(t *testing.T, jsonlPath string, budget time.Duration) bool {
	t.Helper()
	deadline := time.Now().Add(budget)
	for time.Now().Before(deadline) {
		lines := smokeFixtureReadJSONLLines(t, jsonlPath)
		for _, line := range lines {
			if strings.Contains(line, string(core.EventTypeRunCompleted)) ||
				strings.Contains(line, string(core.EventTypeRunFailed)) {
				return true
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	return false
}

// TestSmokeLoop is the EARLY_ROADMAP row #11 proof-of-life integration test.
// It exercises the full daemon work loop against a real beads SQLite DB:
//
//  1. Seed one ready bead.
//  2. daemon.Start (real br adapter, real git worktree, real handler subprocess).
//  3. Poll until the bead is closed.
//  4. Assert JSONL contains run_started and run_completed events.
//
// The test passes a cancellable context to daemon.Start (hk-7oz2f) and calls
// cancel once the bead is confirmed closed, avoiding SIGINT to the test process.
//
// Known gaps (follow-up beads filed inline):
//   - Config.HandlerArgs not present → workaround: baked handler.sh script.
//   - Worktree cleanup not wired in R10 → workaround: accept worktree present.
func TestSmokeLoop(t *testing.T) {
	skipRealDaemonE2EInShort(t)
	t.Parallel()

	realBrPath := smokeFixtureBrPath(t)

	projectDir, jsonlPath := smokeFixtureProjectDir(t)
	smokeFixtureGitRepo(t, projectDir)

	dbPath := filepath.Join(projectDir, ".beads", "beads.db")
	brWrapper := smokeFixtureBrWrapperScript(t, realBrPath, dbPath)
	handlerScript := smokeFixtureHandlerScript(t)

	beadID := smokeFixtureInitBr(t, realBrPath, projectDir, brWrapper)
	t.Logf("smoke: seeded bead ID = %s", beadID)

	cfg := daemon.Config{
		ProjectDir:          projectDir,
		JSONLLogPath:        jsonlPath,
		BrPath:              brWrapper,
		HandlerBinary:       handlerScript,
		HandlerEnv:          nil,
		WorkflowModeDefault: core.WorkflowModeDot,
	}

	loopCtx, loopCancel := context.WithCancel(context.Background())
	defer loopCancel()

	startDone := make(chan error, 1)
	go func() {
		startDone <- daemon.Start(loopCtx, cfg)
	}()

	const pollBudget = 20 * time.Second
	closed := smokeFixturePollBeadClosed(t, brWrapper, beadID, pollBudget)
	if closed {
		_ = smokeFixturePollRunTerminal(t, jsonlPath, 5*time.Second)
	}

	loopCancel()

	if err := awaitLoopTeardownErr(t, startDone, "daemon.Start"); err != nil {
		t.Errorf("daemon.Start returned error after context cancel: %v", err)
	}

	if !closed {
		closed = smokeFixturePollBeadClosed(t, brWrapper, beadID, 2*time.Second)
	}
	if !closed {
		t.Errorf("bead %s was not closed within %s; work loop did not complete the dispatch cycle", beadID, pollBudget)
	}

	lines := smokeFixtureReadJSONLLines(t, jsonlPath)
	if len(lines) == 0 {
		t.Fatal("JSONL log is empty; expected daemon_started, run_started, run_completed")
	}

	foundRunStarted := false
	for _, line := range lines {
		if strings.Contains(line, string(core.EventTypeRunStarted)) ||
			strings.Contains(line, `"workspace_path"`) {
			foundRunStarted = true
			break
		}
	}
	if !foundRunStarted {
		t.Errorf("run_started event not found in JSONL log; lines: %v", lines)
	}

	foundRunCompleted := false
	for _, line := range lines {
		if strings.Contains(line, string(core.EventTypeRunCompleted)) ||
			strings.Contains(line, `"auto-close: exit=0"`) {
			foundRunCompleted = true
			break
		}
	}
	if !foundRunCompleted {
		t.Errorf("run_completed event not found in JSONL log; lines: %v", lines)
	}

	t.Logf("smoke: JSONL line count = %d; bead closed = %v; run_started = %v; run_completed = %v",
		len(lines), closed, foundRunStarted, foundRunCompleted)
}
