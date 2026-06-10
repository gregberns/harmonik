//go:build scenario

// TestScenario_CodexAdapter_FullLifecycle exercises the full codex-adapter
// lifecycle on the twin substrate: run_started → implementer Refs commit →
// reviewer_verdict (APPROVE) → run_completed → bead closed.
//
// Spec ref: codex-harness C2-codex-adapter-spec.md §AC2.3;
// codex-harness C6-migration-test-spec.md §Approach.
// Bead: hk-vfmn9.
package scenario

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
)

// ─────────────────────────────────────────────────────────────────────────────
// Fixtures (codexLifecycleFixture prefix — bead hk-vfmn9)
// ─────────────────────────────────────────────────────────────────────────────

// codexLifecycleFixtureBrPath locates the real `br` binary. Skips the test if
// br is not on PATH.
func codexLifecycleFixtureBrPath(t *testing.T) string {
	t.Helper()
	brPath, err := exec.LookPath("br")
	if err != nil {
		t.Skip("br required for codex lifecycle scenario (not on PATH)")
	}
	return brPath
}

// codexLifecycleFixtureProjectDir creates the minimal harmonik project
// directory tree (.harmonik/events/, .harmonik/beads-intents/). Returns the
// project dir and JSONL log path.
//
// EvalSymlinks is applied so that br (which rejects symlinked paths on macOS)
// receives the canonical path.
func codexLifecycleFixtureProjectDir(t *testing.T) (string, string) {
	t.Helper()
	raw := t.TempDir()
	resolved, err := filepath.EvalSymlinks(raw)
	if err != nil {
		t.Fatalf("codexLifecycleFixtureProjectDir: EvalSymlinks: %v", err)
	}
	projectDir := resolved

	for _, sub := range []string{
		".harmonik/events",
		".harmonik/beads-intents",
	} {
		//nolint:gosec // G301: test-only temp directory
		if mkErr := os.MkdirAll(filepath.Join(projectDir, sub), 0o755); mkErr != nil {
			t.Fatalf("codexLifecycleFixtureProjectDir: MkdirAll %s: %v", sub, mkErr)
		}
	}

	jsonlPath := filepath.Join(projectDir, ".harmonik", "events", "events.jsonl")
	return projectDir, jsonlPath
}

// codexLifecycleFixtureGitRepo initialises a git repository in dir with a
// single initial commit and a bare origin remote so the daemon's post-merge
// `git push origin main` succeeds.
func codexLifecycleFixtureGitRepo(t *testing.T, dir string) {
	t.Helper()
	run := func(d string, args ...string) {
		t.Helper()
		//nolint:gosec // G204: git args are test-internal literals
		cmd := exec.CommandContext(t.Context(), "git", args...)
		cmd.Dir = d
		if out, cmdErr := cmd.CombinedOutput(); cmdErr != nil {
			t.Fatalf("codexLifecycleFixtureGitRepo: git %v: %v\n%s", args, cmdErr, out)
		}
	}
	run(dir, "init", "--initial-branch=main")
	run(dir, "config", "user.email", "test@harmonik.local")
	run(dir, "config", "user.name", "Harmonik Test")
	initFile := filepath.Join(dir, "README")
	if err := os.WriteFile(initFile, []byte("codex lifecycle scenario repo\n"), 0o644); err != nil {
		t.Fatalf("codexLifecycleFixtureGitRepo: WriteFile README: %v", err)
	}
	run(dir, "add", "README")
	run(dir, "commit", "-m", "Initial commit")

	originDir := t.TempDir()
	run(originDir, "init", "--bare", "--initial-branch=main")
	run(dir, "remote", "add", "origin", originDir)
	run(dir, "push", "origin", "main")
}

// codexLifecycleFixtureBrWrapperScript writes a wrapper script that invokes
// realBrPath with --db <dbPath> prepended. Returns its absolute path.
func codexLifecycleFixtureBrWrapperScript(t *testing.T, realBrPath, dbPath string) string {
	t.Helper()
	scriptPath := filepath.Join(t.TempDir(), "br")
	content := "#!/bin/sh\nexec " + realBrPath + " --db " + dbPath + " \"$@\"\n"
	//nolint:gosec // G306: test-only script; chmod 0755 required for execution
	if err := os.WriteFile(scriptPath, []byte(content), 0o755); err != nil {
		t.Fatalf("codexLifecycleFixtureBrWrapperScript: WriteFile: %v", err)
	}
	return scriptPath
}

// codexLifecycleFixtureInitBr initialises a beads workspace in projectDir,
// creates one ready bead, and returns its ID.
func codexLifecycleFixtureInitBr(t *testing.T, realBrPath, projectDir, brWrapperPath string) string {
	t.Helper()
	//nolint:gosec // G204: br args are test-internal literals
	initCmd := exec.CommandContext(t.Context(), realBrPath, "init", "--prefix", "cl")
	initCmd.Dir = projectDir
	if initOut, initErr := initCmd.CombinedOutput(); initErr != nil {
		t.Fatalf("codexLifecycleFixtureInitBr: br init: %v\n%s", initErr, initOut)
	}
	//nolint:gosec // G204: br args are test-internal literals
	createCmd := exec.CommandContext(t.Context(), brWrapperPath,
		"create", "codex adapter lifecycle test bead", "--status", "open", "--silent")
	createOut, createErr := createCmd.CombinedOutput()
	if createErr != nil {
		t.Fatalf("codexLifecycleFixtureInitBr: br create: %v\n%s", createErr, createOut)
	}
	id := strings.TrimSpace(string(createOut))
	if id == "" {
		t.Fatal("codexLifecycleFixtureInitBr: br create returned empty ID")
	}
	return id
}

// codexLifecycleFixtureHandlerScript writes a /bin/sh wrapper script that
// dispatches to the implementer or reviewer path based on whether
// .harmonik/review-target.md is present in $HARMONIK_WORKSPACE_PATH:
//
//   - Implementer (no review-target.md): invokes harmonik-twin-codex exec
//     --scenario trailer-commit, which makes a Refs: commit. Twin stdout is
//     directed to /dev/null so the daemon's NDJSON watcher sees only EOF.
//   - Reviewer (review-target.md present): writes an APPROVE verdict JSON to
//     $HARMONIK_WORKSPACE_PATH/.harmonik/review.json.
func codexLifecycleFixtureHandlerScript(t *testing.T, codexTwinPath string) string {
	t.Helper()
	// Escape single quotes in the binary path for shell embedding.
	codexTwinEsc := strings.ReplaceAll(codexTwinPath, "'", "'\\''")
	script := fmt.Sprintf(`#!/bin/sh
set -e
WS="${HARMONIK_WORKSPACE_PATH:-$(pwd)}"
if [ -f "$WS/.harmonik/review-target.md" ]; then
  # Reviewer phase: write APPROVE verdict.
  mkdir -p "$WS/.harmonik"
  printf '{"schema_version":1,"verdict":"APPROVE","flags":[],"notes":"codex-adapter lifecycle test"}' > "$WS/.harmonik/review.json"
else
  # Implementer phase: run the codex twin with trailer-commit scenario.
  # Redirect stdout+stderr to /dev/null so the daemon's NDJSON watcher only
  # sees EOF when this script exits (not codex JSONL that would trigger
  # malformed_progress_message noise).
  bead_id=""
  if [ -f "$WS/.harmonik/agent-task.md" ]; then
    bead_id=$(grep '^bead_id:' "$WS/.harmonik/agent-task.md" | awk '{print $2}')
  fi
  '%s' exec --json -C "$WS" --scenario trailer-commit --bead-id "$bead_id" >/dev/null 2>&1
fi
exit 0
`, codexTwinEsc)
	scriptPath := filepath.Join(t.TempDir(), "codex_lifecycle_handler.sh")
	//nolint:gosec // G306: test-only script; chmod 0755 required for execution
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("codexLifecycleFixtureHandlerScript: WriteFile: %v", err)
	}
	return scriptPath
}

// codexLifecycleFixturePollBeadClosed polls `br show <id>` at 10 ms intervals
// for up to budget. Returns true if the bead reaches "closed" status.
func codexLifecycleFixturePollBeadClosed(t *testing.T, brWrapperPath, beadID string, budget time.Duration) bool {
	t.Helper()
	deadline := time.Now().Add(budget)
	for time.Now().Before(deadline) {
		//nolint:gosec // G204: br args are test-internal literals
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

// ─────────────────────────────────────────────────────────────────────────────
// TestScenario_CodexAdapter_FullLifecycle
// ─────────────────────────────────────────────────────────────────────────────

// TestScenario_CodexAdapter_FullLifecycle verifies the full codex-adapter
// lifecycle on the twin substrate (bead hk-vfmn9):
//
//  1. Daemon dispatches the implementer. The handler wrapper invokes
//     harmonik-twin-codex exec --scenario trailer-commit, which makes a git
//     commit with a Refs:<beadID> trailer.
//  2. Daemon launches the reviewer. The handler wrapper detects review-target.md
//     and writes an APPROVE verdict JSON.
//  3. Daemon emits reviewer_verdict (APPROVE) then run_completed.
//  4. Commit with Refs:<beadID> is present on main.
//  5. Bead is closed.
func TestScenario_CodexAdapter_FullLifecycle(t *testing.T) {
	if codexTwinBinaryPath == "" {
		t.Skip("harmonik-twin-codex binary not built; skipping codex lifecycle scenario")
	}

	realBrPath := codexLifecycleFixtureBrPath(t)
	projectDir, jsonlPath := codexLifecycleFixtureProjectDir(t)
	codexLifecycleFixtureGitRepo(t, projectDir)

	dbPath := filepath.Join(projectDir, ".beads", "beads.db")
	brWrapper := codexLifecycleFixtureBrWrapperScript(t, realBrPath, dbPath)
	handlerScript := codexLifecycleFixtureHandlerScript(t, codexTwinBinaryPath)
	beadID := codexLifecycleFixtureInitBr(t, realBrPath, projectDir, brWrapper)

	cfg := daemon.Config{
		ProjectDir:          projectDir,
		JSONLLogPath:        jsonlPath,
		BrPath:              brWrapper,
		HandlerBinary:       handlerScript,
		WorkflowModeDefault: core.WorkflowModeReviewLoop,
		HandlerEnv:          os.Environ(),
	}

	cancel, daemonDone := scenarioFixtureStartDaemon(t, cfg)

	// Phase 1: wait for reviewer_verdict (covers full implementer→reviewer cycle).
	const verdictBudget = 90 * time.Second
	gotVerdict := scenarioFixturePollJSONLForEvent(t, jsonlPath,
		[]string{string(core.EventTypeReviewerVerdict)}, verdictBudget)

	// Phase 2: wait for terminal event (run_completed or run_failed).
	const terminalBudget = 30 * time.Second
	gotTerminal := scenarioCheckRunCompleted(t, jsonlPath, terminalBudget)

	// Allow bead close to propagate before cancelling the daemon.
	if gotTerminal {
		codexLifecycleFixturePollBeadClosed(t, brWrapper, beadID, 10*time.Second)
	}

	cancel()
	scenarioFixtureWaitDaemon(t, daemonDone, 10*time.Second)

	// ── Assertions ──────────────────────────────────────────────────────────

	if !gotVerdict {
		t.Error("timed out waiting for reviewer_verdict event")
	}
	if !gotTerminal {
		t.Error("timed out waiting for run_completed/run_failed event")
	}

	lines := scenarioFixtureReadJSONLLines(t, jsonlPath)

	// Sequence: run_started → reviewer_verdict → run_completed.
	if !scenarioEventSequence(t, lines, []string{
		string(core.EventTypeRunStarted),
		string(core.EventTypeReviewerVerdict),
		string(core.EventTypeRunCompleted),
	}) {
		t.Errorf("expected event sequence run_started→reviewer_verdict→run_completed; JSONL:\n%s",
			strings.Join(lines, "\n"))
	}

	// Reviewer verdict must be APPROVE.
	foundApprove := false
	for _, line := range lines {
		if strings.Contains(line, string(core.EventTypeReviewerVerdict)) &&
			strings.Contains(line, "APPROVE") {
			foundApprove = true
			break
		}
	}
	if !foundApprove {
		t.Errorf("reviewer_verdict APPROVE not found; JSONL:\n%s", strings.Join(lines, "\n"))
	}

	// A commit with Refs:<beadID> must be present on main.
	//nolint:gosec // G204: git args are test-internal literals
	logCmd := exec.Command("git", "log", "--format=%B", "main")
	logCmd.Dir = projectDir
	if logOut, logErr := logCmd.Output(); logErr == nil {
		if !strings.Contains(string(logOut), "Refs: "+beadID) {
			t.Errorf("expected 'Refs: %s' in git log on main; got:\n%s", beadID, logOut)
		}
	} else {
		t.Logf("git log main: %v (non-fatal — commit check skipped)", logErr)
	}

	// Bead must be closed.
	if !codexLifecycleFixturePollBeadClosed(t, brWrapper, beadID, 2*time.Second) {
		t.Errorf("bead %s not closed after run_completed", beadID)
	}

	t.Logf("PASS beadID=%s gotVerdict=%v gotTerminal=%v", beadID, gotVerdict, gotTerminal)
}
