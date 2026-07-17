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

// ─────────────────────────────────────────────────────────────────────────────
// TestScenario_Codex_EmptyModel_FullLifecycle (GAP-6)
// ─────────────────────────────────────────────────────────────────────────────

// codexEmptyModelFixtureInitBr initialises a beads workspace, creates ONE ready
// bead labelled harness:codex (so the daemon routes it through the REAL codex
// harness → buildCodexLaunchSpec, exercising the empty-model --model omission),
// and returns its ID. Unlike codexLifecycleFixtureInitBr, the bead carries the
// harness pin so routing does not fall back to the claude default.
func codexEmptyModelFixtureInitBr(t *testing.T, realBrPath, projectDir, brWrapperPath string) string {
	t.Helper()
	//nolint:gosec // G204: br args are test-internal literals
	initCmd := exec.CommandContext(t.Context(), realBrPath, "init", "--prefix", "cl")
	initCmd.Dir = projectDir
	if initOut, initErr := initCmd.CombinedOutput(); initErr != nil {
		t.Fatalf("codexEmptyModelFixtureInitBr: br init: %v\n%s", initErr, initOut)
	}
	//nolint:gosec // G204: br args are test-internal literals
	createCmd := exec.CommandContext(t.Context(), brWrapperPath,
		"create", "codex empty-model lifecycle test bead",
		"--status", "open", "--labels", "harness:codex", "--silent")
	createOut, createErr := createCmd.CombinedOutput()
	if createErr != nil {
		t.Fatalf("codexEmptyModelFixtureInitBr: br create: %v\n%s", createErr, createOut)
	}
	id := strings.TrimSpace(string(createOut))
	if id == "" {
		t.Fatal("codexEmptyModelFixtureInitBr: br create returned empty ID")
	}
	return id
}

// codexEmptyModelFixtureCodexShim writes an executable named exactly "codex" into
// a fresh temp dir and returns that dir. The shim is the load-bearing sensor for
// GAP-6:
//
//   - It is spawned by the daemon AS the codex binary (the daemon's exec fallback
//     resolves the relative name "codex" against the process PATH, which the test
//     prepends with this dir).
//   - It receives the REAL daemon-built codex argv as "$@". BEFORE invoking the
//     twin it asserts "$@" carries NO --model token — making the empty-model
//     --model-omission branch load-bearing end-to-end (a regression that re-added
//     --model would make the shim exit 3 → the run fails → this test fails).
//   - It then invokes the model-blind harmonik-twin-codex with the trailer-commit
//     scenario so the run lands a Refs: commit exactly as the codex adapter's
//     happy path would.
func codexEmptyModelFixtureCodexShim(t *testing.T, codexTwinPath string) string {
	t.Helper()
	shimDir := t.TempDir()
	codexTwinEsc := strings.ReplaceAll(codexTwinPath, "'", "'\\''")
	script := fmt.Sprintf(`#!/bin/sh
set -e
# GAP-6 sensor: the daemon-built empty-model codex argv MUST NOT carry --model.
case " $* " in
  *" --model "*) echo "argv leaked --model: $*" >&2; exit 3;;
esac
# Resolve the worktree from -C in the daemon argv (fall back to CWD).
WS="$(pwd)"
prev=""
for a in "$@"; do
  if [ "$prev" = "-C" ]; then WS="$a"; fi
  prev="$a"
done
bead_id=""
if [ -f "$WS/.harmonik/agent-task.md" ]; then
  bead_id=$(grep '^bead_id:' "$WS/.harmonik/agent-task.md" | awk '{print $2}')
fi
# Fail LOUD (exit 4) rather than invoking the twin with an empty --bead-id: if
# agent-task.md moves or its bead_id: line changes shape, an empty id would make
# the twin commit an untrailered change and surface as an opaque "bead not
# closed" instead of a broken sensor. Mirrors the exit-3 --model discipline.
if [ -z "$bead_id" ]; then
  echo "shim: could not derive bead_id from $WS/.harmonik/agent-task.md" >&2
  exit 4
fi
'%s' exec --json -C "$WS" --scenario trailer-commit --bead-id "$bead_id" >/dev/null 2>&1
exit 0
`, codexTwinEsc)
	shimPath := filepath.Join(shimDir, "codex")
	//nolint:gosec // G306: test-only script; chmod 0755 required for execution
	if err := os.WriteFile(shimPath, []byte(script), 0o755); err != nil {
		t.Fatalf("codexEmptyModelFixtureCodexShim: WriteFile: %v", err)
	}
	return shimDir
}

// codexEmptyModelFixtureModelSelected scans the JSONL log for the FIRST
// model_selected event and returns its decoded payload. found=false when no such
// event is present.
func codexEmptyModelFixtureModelSelected(t *testing.T, jsonlPath string) (core.ModelSelectedPayload, bool) {
	t.Helper()
	for _, line := range scenarioFixtureReadJSONLLines(t, jsonlPath) {
		var env struct {
			Type    string          `json:"type"`
			Payload json.RawMessage `json:"payload"`
		}
		if err := json.Unmarshal([]byte(line), &env); err != nil {
			continue
		}
		if env.Type != string(core.EventTypeModelSelected) {
			continue
		}
		var pl core.ModelSelectedPayload
		if err := json.Unmarshal(env.Payload, &pl); err != nil {
			t.Fatalf("codexEmptyModelFixtureModelSelected: decode payload: %v\nraw: %s", err, env.Payload)
		}
		return pl, true
	}
	return core.ModelSelectedPayload{}, false
}

// TestScenario_Codex_EmptyModel_FullLifecycle is the GAP-6 full-stack twin proof
// for the codex empty-model account-default path.
//
// It differs from TestScenario_CodexAdapter_FullLifecycle in two load-bearing
// ways:
//
//  1. The bead is labelled harness:codex and the daemon spawns a "codex" shim
//     (on PATH) — so the run routes through the REAL codex harness and
//     buildCodexLaunchSpec builds the empty-model argv (no --model, because a
//     codex bead with no model: label resolves to ""). The shim asserts "$@" has
//     no --model, making the omission branch load-bearing end-to-end.
//  2. It asserts the model_selected event the routing layer publishes reports
//     harness=="codex" with an empty model — the observability tie-through the
//     lifecycle test never checked.
//
// Hermeticity: HOME is pointed at a throwaway dir so the in-process daemon's
// codex billing guard materializes forced_login_method=chatgpt into
// <tmp>/.codex and passes (no auth.json → no API-pool leak) WITHOUT touching the
// operator's real ~/.codex. PATH is prepended with the shim dir so the daemon's
// exec fallback resolves "codex" to the shim, not the real codex-cli. Not
// parallel: t.Setenv forbids it.
func TestScenario_Codex_EmptyModel_FullLifecycle(t *testing.T) {
	if codexTwinBinaryPath == "" {
		t.Skip("harmonik-twin-codex binary not built; skipping codex empty-model scenario")
	}

	// Hermetic HOME → hermetic CODEX_HOME (billing guard materializes into
	// <tmp>/.codex, never the real ~/.codex) and redirect ~/.claude.json.
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("HARMONIK_CLAUDE_CONFIG_PATH", filepath.Join(tmpHome, ".claude.json"))

	// Prepend the codex shim dir so the daemon's exec fallback resolves the
	// relative "codex" binary to our sensor shim (process-PATH lookup, handler.go).
	shimDir := codexEmptyModelFixtureCodexShim(t, codexTwinBinaryPath)
	t.Setenv("PATH", shimDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	realBrPath := codexLifecycleFixtureBrPath(t)
	projectDir, jsonlPath := codexLifecycleFixtureProjectDir(t)
	codexLifecycleFixtureGitRepo(t, projectDir)

	dbPath := filepath.Join(projectDir, ".beads", "beads.db")
	brWrapper := codexLifecycleFixtureBrWrapperScript(t, realBrPath, dbPath)
	beadID := codexEmptyModelFixtureInitBr(t, realBrPath, projectDir, brWrapper)

	cfg := daemon.Config{
		ProjectDir:   projectDir,
		JSONLLogPath: jsonlPath,
		BrPath:       brWrapper,
		// Single mode: codex implementer only (no reviewer), matching the
		// codex:local matrix cell. The twin's trailer-commit lands a Refs: commit;
		// the daemon merges it to main and closes the bead.
		WorkflowModeDefault: core.WorkflowModeSingle,
		HandlerEnv:          os.Environ(), // carries the shimmed PATH + hermetic HOME
	}

	cancel, daemonDone := scenarioFixtureStartDaemon(t, cfg)

	const terminalBudget = 90 * time.Second
	gotTerminal := scenarioCheckRunCompleted(t, jsonlPath, terminalBudget)

	if gotTerminal {
		codexLifecycleFixturePollBeadClosed(t, brWrapper, beadID, 10*time.Second)
	}

	cancel()
	scenarioFixtureWaitDaemon(t, daemonDone, 10*time.Second)

	// ── Assertions ────────────────────────────────────────────────────────────

	if !gotTerminal {
		t.Error("timed out waiting for run_completed/run_failed event")
	}

	lines := scenarioFixtureReadJSONLLines(t, jsonlPath)

	// The run must have COMPLETED (not failed). A run_failed here most likely
	// means the shim's --model sensor tripped (exit 3) — i.e. the daemon leaked
	// --model into the empty-model codex argv.
	// Decode each envelope and compare its Type rather than substring-matching the
	// raw line: "run_failed" can appear inside an unrelated payload (e.g. a stderr
	// tail quoting it), which would false-positive this sensor.
	sawFailed := false
	for _, line := range lines {
		var env struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal([]byte(line), &env); err != nil {
			continue
		}
		if env.Type == string(core.EventTypeRunFailed) {
			sawFailed = true
		}
	}
	if sawFailed {
		t.Errorf("run_failed present — the codex shim likely rejected a leaked --model; JSONL:\n%s",
			strings.Join(lines, "\n"))
	}

	// GAP-6 core: model_selected must report harness=codex with an EMPTY model.
	pl, found := codexEmptyModelFixtureModelSelected(t, jsonlPath)
	if !found {
		t.Errorf("no model_selected event found; JSONL:\n%s", strings.Join(lines, "\n"))
	} else {
		if pl.Harness != "codex" {
			t.Errorf("model_selected.harness = %q; want %q", pl.Harness, "codex")
		}
		if pl.Model != "" {
			t.Errorf("model_selected.model = %q; want empty (codex account-default, not harmonik-controlled)", pl.Model)
		}
	}

	// A commit with Refs:<beadID> must be present on main (twin trailer-commit
	// landed through the real empty-model codex launch path).
	//nolint:gosec // G204: git args are test-internal literals
	logCmd := exec.CommandContext(t.Context(), "git", "log", "--format=%B", "main")
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

	t.Logf("PASS beadID=%s gotTerminal=%v modelSelectedFound=%v", beadID, gotTerminal, found)
}
