//go:build scenario

package daemon_test

// scenario_queue_submit_dispatch_hksk00a_test.go — submit-RPC → dispatch → close
// bridge scenario test (hk-sk00a).
//
// Gaps covered:
//   - hk-24xn1: idle daemon does not see a newly submitted queue until the wake
//     channel fires; this test submits via HandleQueueSubmit while the daemon is
//     idle and asserts the bead reaches "closed".
//   - hk-nbjht: a deferred-for-ledger-dep item in a stream group is never
//     re-evaluated after its in-group blocker completes; this test submits [A, B]
//     where B is deferred behind A, lets A close, and asserts B un-defers and
//     also reaches "closed".
//   - hk-4ie1z: exercised vacuously — any run that reaches run_completed without
//     a commit would formerly false-close the bead; the twin scenario emits a
//     valid outcome so the run completes cleanly.
//
// Bridge: queue_setqueue_wiring_test.go (HandleQueueSubmit path, no dispatch)
//         scenario_happypath_n1_test.go (dispatch path, no queue submit)
// Both paths are connected here end-to-end.
//
// Helper prefix: queueSubmitDispatch (per implementer-protocol.md §Helper-prefix).
//
// Spec refs:
//   - specs/queue-model.md §2.8 QM-025 (deferred-for-ledger-dep)
//   - specs/queue-model.md §3.2 QM-002 (queue-active → idle wake-on-submit)
//   - specs/execution-model.md §7.4 TS-1 (queue-pull dispatch)
//   - specs/scenario-harness.md §4 (assertion vocabulary)
//
// Bead: hk-sk00a.

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/daemon/scenariotest"
	"github.com/gregberns/harmonik/internal/eventbus"
	"github.com/gregberns/harmonik/internal/queue"
	queuecli "github.com/gregberns/harmonik/internal/queue/cli"
	"github.com/gregberns/harmonik/internal/workspace"
)

// ─────────────────────────────────────────────────────────────────────────────
// Shared fixture helpers (queueSubmitDispatch prefix)
// ─────────────────────────────────────────────────────────────────────────────

// queueSubmitDispatchProjectDir creates the minimal project directory layout.
// Returns (projectDir, jsonlPath).
func queueSubmitDispatchProjectDir(t *testing.T) (string, string) {
	t.Helper()
	raw, err := os.MkdirTemp("/tmp", "qsd-")
	require.NoError(t, err, "queueSubmitDispatchProjectDir: MkdirTemp")
	t.Cleanup(func() { require.NoError(t, os.RemoveAll(raw), "queueSubmitDispatchProjectDir: cleanup") })
	dir, err := filepath.EvalSymlinks(raw)
	require.NoError(t, err, "queueSubmitDispatchProjectDir: EvalSymlinks")
	for _, sub := range []string{
		filepath.Join(".harmonik", "events"),
		filepath.Join(".harmonik", "beads-intents"),
		filepath.Join(".harmonik", "queues"),
	} {
		//nolint:gosec // G301: 0755 matches .harmonik dir conventions
		require.NoError(t, os.MkdirAll(filepath.Join(dir, sub), 0o755),
			"queueSubmitDispatchProjectDir: MkdirAll %s", sub)
	}
	return dir, filepath.Join(dir, ".harmonik", "events", "events.jsonl")
}

func queueSubmitDispatchWaitSocket(t *testing.T, projectDir string) {
	t.Helper()
	socketPath := filepath.Join(projectDir, ".harmonik", "daemon.sock")
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(socketPath); err == nil {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("daemon socket did not appear at %s", socketPath)
}

func queueSubmitDispatchSubmitCLI(t *testing.T, projectDir string, ids []core.BeadID) string {
	t.Helper()
	beadIDs := make([]string, len(ids))
	for i, id := range ids {
		beadIDs[i] = string(id)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		var out strings.Builder
		var errOut strings.Builder
		exitCode := queuecli.RunQueueSubmit(t.Context(), []string{
			"--project", projectDir,
			"--beads", strings.Join(beadIDs, ","),
			"--json",
		}, &out, &errOut)
		if exitCode == 0 {
			return strings.TrimSpace(out.String())
		}
		if exitCode != 17 || time.Now().After(deadline) {
			t.Fatalf("queue submit CLI exit=%d: %s", exitCode, errOut.String())
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func queueSubmitDispatchDryRunCLI(t *testing.T, projectDir string, ids []core.BeadID) queue.QueueDryRunResponse {
	t.Helper()
	beadIDs := make([]string, len(ids))
	for i, id := range ids {
		beadIDs[i] = string(id)
	}
	var out strings.Builder
	var errOut strings.Builder
	exitCode := queuecli.RunQueueDryRun(t.Context(), []string{
		"--project", projectDir,
		"--beads", strings.Join(beadIDs, ","),
		"--json",
	}, &out, &errOut)
	require.Equal(t, 0, exitCode, "queue dry-run CLI: %s", errOut.String())

	var response queue.QueueDryRunResponse
	require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(out.String())), &response),
		"decode queue dry-run response: %s", out.String())
	return response
}

func queueSubmitDispatchEpicCompleted(t *testing.T, jsonlPath string, epicID core.BeadID) []core.Event {
	t.Helper()
	f, err := os.Open(jsonlPath)
	require.NoError(t, err, "open event log")
	defer f.Close()

	var matches []core.Event
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		var event core.Event
		if json.Unmarshal(scanner.Bytes(), &event) != nil || event.Type != core.EventTypeEpicCompleted {
			continue
		}
		var payload struct {
			EpicID string `json:"epic_id"`
		}
		if json.Unmarshal(event.Payload, &payload) == nil && payload.EpicID == string(epicID) {
			matches = append(matches, event)
		}
	}
	require.NoError(t, scanner.Err(), "scan event log")
	return matches
}

// queueSubmitDispatchGitRepo initialises a git repository with one commit in
// dir, and wires a bare-repo "origin" remote so that mergeRunBranchToMain's
// git-push step succeeds (avoiding push_failed run_failed events in scenario
// tests that make actual commits in the worktree).
func queueSubmitDispatchGitRepo(t *testing.T, dir string) {
	t.Helper()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.CommandContext(t.Context(), "git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "queueSubmitDispatchGitRepo: git %v\n%s", args, out)
	}
	run("init", "--initial-branch=main")
	run("config", "user.email", "test@harmonik.local")
	run("config", "user.name", "Harmonik Test")
	readmePath := filepath.Join(dir, "README")
	require.NoError(t, os.WriteFile(readmePath, []byte("queue-submit-dispatch scenario\n"), 0o644),
		"queueSubmitDispatchGitRepo: WriteFile README")
	// Both target names: see the note in the em012a fixture — the per-bead gate
	// was renamed from `make full` to `make core` at D3=v3, and a fixture
	// pinned to one name fails misleadingly when that moves.
	makefile := ".PHONY: full core\nfull core:\n\t@latest=$$(git diff-tree --no-commit-id --name-only -r HEAD); " +
		"printf '%s\\n' \"$$latest\" | grep -q '^.harmonik-twin-commit-'; " +
		"mkdir -p $$(git rev-parse --git-common-dir)/../.harmonik; " +
		"printf '%s\\n' \"$$latest\" >> $$(git rev-parse --git-common-dir)/../.harmonik/validation-runs\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "Makefile"), []byte(makefile), 0o644),
		"queueSubmitDispatchGitRepo: WriteFile Makefile")
	run("add", "README", "Makefile")
	run("commit", "-m", "Initial commit")
	run("branch", "integration")

	// Add a bare-repo origin so mergeRunBranchToMain's push step succeeds.
	// Without a remote the push fails with "fatal: 'origin' does not appear to
	// be a git repository" and the run is reopened as push_failed (run_failed).
	raw := t.TempDir()
	originDir, err := filepath.EvalSymlinks(raw)
	require.NoError(t, err, "queueSubmitDispatchGitRepo: EvalSymlinks originDir")
	initBareCmd := exec.CommandContext(t.Context(), "git", "init", "--bare", "--initial-branch=main", originDir)
	out, err := initBareCmd.CombinedOutput()
	require.NoError(t, err, "queueSubmitDispatchGitRepo: git init --bare\n%s", out)
	run("remote", "add", "origin", originDir)
	run("push", "origin", "main")
	run("push", "origin", "integration")
}

func queueSubmitDispatchInstallFailingGate(t *testing.T, projectDir string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(projectDir, "Makefile"), []byte(".PHONY: full core\nfull core:\n\t@false\n"), 0o644),
		"write failing validation gate")
	run := func(args ...string) {
		t.Helper()
		cmd := exec.CommandContext(t.Context(), "git", args...)
		cmd.Dir = projectDir
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "git %v\n%s", args, out)
	}
	run("add", "Makefile")
	run("commit", "-m", "Install failing scenario gate")
	run("branch", "-f", "integration", "HEAD")
	run("push", "--force", "origin", "main", "integration")
}

func queueSubmitDispatchAssertLanded(t *testing.T, projectDir, targetBranch string, ids []core.BeadID) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", "rev-list", "--count", "main.."+targetBranch)
	cmd.Dir = projectDir
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "git rev-list main..%s\n%s", targetBranch, out)
	require.Equal(t, fmt.Sprintf("%d", len(ids)), strings.TrimSpace(string(out)),
		"target branch must advance once for each closed bead while main stays unchanged")
}

// queueSubmitDispatchBrPath returns the path to the real br binary. Skips if absent.
func queueSubmitDispatchBrPath(t *testing.T) string {
	t.Helper()
	path, err := exec.LookPath("br")
	if err != nil {
		t.Skip("br required for queue-submit-dispatch scenario test (not on PATH)")
	}
	return path
}

// queueSubmitDispatchBrWrapper writes a /bin/sh wrapper that invokes brPath
// with --db dbPath prepended to all args. Returns the wrapper path.
func queueSubmitDispatchBrWrapper(t *testing.T, brPath, dbPath string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "br")
	content := "#!/bin/sh\nexec " + brPath + " --db " + dbPath + " \"$@\"\n"
	//nolint:gosec // G306: script is test-only; 0755 required for execution
	require.NoError(t, os.WriteFile(path, []byte(content), 0o755),
		"queueSubmitDispatchBrWrapper: WriteFile")
	return path
}

// queueSubmitDispatchTwinWrapper writes a /bin/sh wrapper that is phase-aware so
// these review-loop tests complete: the implementer commits and the reviewer
// emits an APPROVE verdict (hk-4f5ua).
//
// Phase detection is by the presence of .harmonik/review-target.md, which the
// daemon writes ONLY into the reviewer's isolated worktree
// (workspace.WriteReviewTarget):
//
//   - Reviewer phase (review-target.md present): write an APPROVE verdict to
//     $PWD/.harmonik/review.json (read by the daemon from revWtPath) so the
//     review loop terminates with success → run_completed + bead closed.
//   - Implementer phase (review-target.md absent): run the twin with
//     --scenario commit-on-cue-startup-delay --worktree-path $PWD. The
//     commit_on_cue step writes a timestamped sentinel file and git-commits it,
//     satisfying the no-commit guard (hk-mmh8f), then emits outcome_emitted and
//     agent_completed. Each commit uses a unique timestamp so concurrent beads
//     do not conflict.
//
// Before hk-81n9r these tests ran in single mode (no reviewer); hk-81n9r made
// them review-loop, so the reviewer phase previously ran commit-on-cue too,
// wrote no verdict, and tripped "verdict absent at iteration 1".
//
// $PWD equals the worktree path because the daemon sets cmd.Dir = workspacePath.
func queueSubmitDispatchTwinWrapper(t *testing.T, twinPath string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "twin-wrapper.sh")
	content := `#!/bin/sh
set -e
if [ -f "$PWD/.harmonik/review-target.md" ]; then
  mkdir -p "$PWD/.harmonik"
  printf '{"schema_version":1,"verdict":"APPROVE","flags":[],"notes":"queue-submit-dispatch review-loop happy path"}' > "$PWD/.harmonik/review.json"
  exit 0
fi
exec "` + twinPath + `" --scenario commit-on-cue-startup-delay --worktree-path "$PWD"
`
	//nolint:gosec // G306: script is test-only; 0755 required for execution
	require.NoError(t, os.WriteFile(path, []byte(content), 0o755),
		"queueSubmitDispatchTwinWrapper: WriteFile")
	return path
}

func queueSubmitDispatchFailTwinWrapper(t *testing.T, twinPath string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "twin-fail-wrapper.sh")
	content := "#!/bin/sh\nexec \"" + twinPath + "\" --scenario dial-failed --worktree-path \"$PWD\"\n"
	require.NoError(t, os.WriteFile(path, []byte(content), 0o755),
		"queueSubmitDispatchFailTwinWrapper: WriteFile")
	return path
}

// queueSubmitDispatchInitBr initialises a br workspace in projectDir.
// Creates one open bead and returns its ID.
func queueSubmitDispatchInitBr(t *testing.T, brPath, projectDir, brWrapper string) core.BeadID {
	t.Helper()
	initCmd := exec.CommandContext(t.Context(), brPath, "init", "--prefix", "qsd")
	initCmd.Dir = projectDir
	out, err := initCmd.CombinedOutput()
	require.NoError(t, err, "queueSubmitDispatchInitBr: br init\n%s", out)

	createCmd := exec.CommandContext(t.Context(), brWrapper, "create",
		"queue-submit idle-wake test bead", "--status", "open", "--silent")
	createOut, createErr := createCmd.CombinedOutput()
	require.NoError(t, createErr, "queueSubmitDispatchInitBr: br create\n%s", createOut)
	id := strings.TrimSpace(string(createOut))
	require.NotEmpty(t, id, "queueSubmitDispatchInitBr: br create returned empty ID")
	return core.BeadID(id)
}

// queueSubmitDispatchInitBrWithDep initialises a br workspace with two open
// beads (A and B) and a dependency edge B→A (B depends on A; A blocks B).
// Returns (aID, bID).
func queueSubmitDispatchInitBrWithDep(t *testing.T, brPath, projectDir, brWrapper string) (core.BeadID, core.BeadID) {
	t.Helper()
	initCmd := exec.CommandContext(t.Context(), brPath, "init", "--prefix", "qsd2")
	initCmd.Dir = projectDir
	out, err := initCmd.CombinedOutput()
	require.NoError(t, err, "queueSubmitDispatchInitBrWithDep: br init\n%s", out)

	// Create bead A (blocker).
	createA := exec.CommandContext(t.Context(), brWrapper, "create",
		"deferred-undefer: bead A (blocker)", "--status", "open", "--silent")
	outA, errA := createA.CombinedOutput()
	require.NoError(t, errA, "queueSubmitDispatchInitBrWithDep: br create A\n%s", outA)
	aID := core.BeadID(strings.TrimSpace(string(outA)))
	require.NotEmpty(t, aID, "queueSubmitDispatchInitBrWithDep: br create A returned empty ID")

	// Create bead B (blocked by A).
	createB := exec.CommandContext(t.Context(), brWrapper, "create",
		"deferred-undefer: bead B (blocked)", "--status", "open", "--silent")
	outB, errB := createB.CombinedOutput()
	require.NoError(t, errB, "queueSubmitDispatchInitBrWithDep: br create B\n%s", outB)
	bID := core.BeadID(strings.TrimSpace(string(outB)))
	require.NotEmpty(t, bID, "queueSubmitDispatchInitBrWithDep: br create B returned empty ID")

	// Add dependency: B depends on A.
	// "br dep add <issue> <depends-on>" — issue=B, depends-on=A.
	depCmd := exec.CommandContext(t.Context(), brWrapper, "dep", "add",
		string(bID), string(aID))
	depOut, depErr := depCmd.CombinedOutput()
	require.NoError(t, depErr, "queueSubmitDispatchInitBrWithDep: br dep add B A\n%s", depOut)

	return aID, bID
}

// queueSubmitDispatchInitBrFanGraph creates one epic over A -> [B,C,D] -> E.
func queueSubmitDispatchInitBrFanGraph(t *testing.T, brPath, projectDir, brWrapper string) (core.BeadID, []core.BeadID) {
	t.Helper()
	initCmd := exec.CommandContext(t.Context(), brPath, "init", "--prefix", "qsdg")
	initCmd.Dir = projectDir
	out, err := initCmd.CombinedOutput()
	require.NoError(t, err, "queueSubmitDispatchInitBrFanGraph: br init\n%s", out)

	epicCmd := exec.CommandContext(t.Context(), brWrapper, "create",
		"fan graph epic", "--type", "epic", "--status", "open", "--silent")
	epicOut, epicErr := epicCmd.CombinedOutput()
	require.NoError(t, epicErr, "queueSubmitDispatchInitBrFanGraph: br create epic\n%s", epicOut)
	epicID := core.BeadID(strings.TrimSpace(string(epicOut)))
	require.NotEmpty(t, epicID, "queueSubmitDispatchInitBrFanGraph: empty epic ID")

	ids := make([]core.BeadID, 5)
	for i, name := range []string{"A root", "B branch", "C branch", "D branch", "E join"} {
		cmd := exec.CommandContext(t.Context(), brWrapper, "create",
			"fan graph: "+name, "--status", "open", "--silent")
		created, createErr := cmd.CombinedOutput()
		require.NoError(t, createErr, "queueSubmitDispatchInitBrFanGraph: br create %s\n%s", name, created)
		ids[i] = core.BeadID(strings.TrimSpace(string(created)))
		require.NotEmpty(t, ids[i], "queueSubmitDispatchInitBrFanGraph: empty ID for %s", name)
	}

	addDep := func(blocked, blocker core.BeadID) {
		t.Helper()
		cmd := exec.CommandContext(t.Context(), brWrapper, "dep", "add", string(blocked), string(blocker))
		depOut, depErr := cmd.CombinedOutput()
		require.NoError(t, depErr, "queueSubmitDispatchInitBrFanGraph: br dep add %s %s\n%s", blocked, blocker, depOut)
	}
	for _, branch := range ids[1:4] {
		addDep(branch, ids[0])
		addDep(ids[4], branch)
	}
	for _, child := range ids {
		cmd := exec.CommandContext(t.Context(), brWrapper, "dep", "add",
			string(child), string(epicID), "--type", "parent-child")
		depOut, depErr := cmd.CombinedOutput()
		require.NoError(t, depErr, "queueSubmitDispatchInitBrFanGraph: parent %s %s\n%s", child, epicID, depOut)
	}
	return epicID, ids
}

// queueSubmitDispatchPollBeadClosed polls br show <id> until status=="closed"
// or budget expires. Returns true when closed.
func queueSubmitDispatchPollBeadClosed(t *testing.T, brWrapper string, id core.BeadID, budget time.Duration) bool {
	t.Helper()
	deadline := time.Now().Add(budget)
	for time.Now().Before(deadline) {
		cmd := exec.CommandContext(t.Context(), brWrapper, "show", string(id), "--format", "json")
		out, err := cmd.Output()
		if err == nil {
			var records []struct {
				Status string `json:"status"`
			}
			if json.Unmarshal(out, &records) == nil && len(records) > 0 && records[0].Status == "closed" {
				return true
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	return false
}

// queueSubmitDispatchWaitRunTerminal polls the JSONL log for a terminal
// run event (run_completed or run_failed) for up to budget. Returns true when found.
func queueSubmitDispatchWaitRunTerminal(t *testing.T, jsonlPath string, budget time.Duration) bool {
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
				t.Logf("queueSubmitDispatchWaitRunTerminal: close: %v", closeErr)
			}
			if found {
				return true
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	return false
}

// queueSubmitDispatchCountRunTerminal returns the number of run_completed or
// run_failed events in the JSONL log.
func queueSubmitDispatchCountRunTerminal(t *testing.T, jsonlPath string) int {
	t.Helper()
	//nolint:gosec // G304: path is t.TempDir()-based; not user input
	f, err := os.Open(jsonlPath)
	if err != nil {
		return 0
	}
	defer func() {
		if closeErr := f.Close(); closeErr != nil {
			t.Logf("queueSubmitDispatchCountRunTerminal: close: %v", closeErr)
		}
	}()
	n := 0
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.Contains(line, string(core.EventTypeRunCompleted)) ||
			strings.Contains(line, string(core.EventTypeRunFailed)) {
			n++
		}
	}
	return n
}

// queueSubmitDispatchCountRunStarted returns the number of run_started events
// in the JSONL log.
func queueSubmitDispatchCountRunStarted(t *testing.T, jsonlPath string) int {
	t.Helper()
	//nolint:gosec // G304: path is t.TempDir()-based; not user input
	f, err := os.Open(jsonlPath)
	if err != nil {
		return 0
	}
	defer func() {
		if closeErr := f.Close(); closeErr != nil {
			t.Logf("queueSubmitDispatchCountRunStarted: close: %v", closeErr)
		}
	}()
	n := 0
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		if strings.Contains(scanner.Text(), string(core.EventTypeRunStarted)) {
			n++
		}
	}
	return n
}

// ─────────────────────────────────────────────────────────────────────────────
// Fake BeadLedger implementations
// ─────────────────────────────────────────────────────────────────────────────

// qsdOpenLedger is a minimal queue.BeadLedger that marks every bead as open
// with no blocking edges. Used when the test beads have no dependencies.
type qsdOpenLedger struct{}

func (l *qsdOpenLedger) LookupStatus(_ context.Context, _ core.BeadID) (queue.BeadStatus, error) {
	return queue.BeadStatusOpen, nil
}

func (l *qsdOpenLedger) BlocksEdge(_ context.Context, _, _ core.BeadID) (bool, error) {
	return false, nil
}

// ─────────────────────────────────────────────────────────────────────────────
// TestScenario_QueueSubmit_IdleWake_hk24xn1
// ─────────────────────────────────────────────────────────────────────────────

// TestScenario_QueueSubmit_IdleWake_hk24xn1 verifies that a queue submitted
// via HandleQueueSubmit to an IDLE daemon (no active queue, NoAutoPull=true)
// causes the daemon to wake on the submit-wake channel, dispatch the bead, and
// close it — end-to-end from submit RPC through dispatch to bead closure.
//
// This pins the gap in hk-24xn1: the idle work loop must listen on
// QueueStore.WakeCh() so a newly submitted queue does not wait for the next
// poll tick.
//
// Setup:
//  1. TempDir project with git + br DB.
//  2. One open bead seeded via br create.
//  3. daemon.Start wired with harmonik-twin-claude and NoAutoPull=true.
//  4. After daemon starts (idle), submit via HandlerAdapter.HandleQueueSubmit.
//
// Assertions:
//  1. run_started event appears in JSONL (dispatch occurred).
//  2. run_completed event appears in JSONL (bead ran to completion).
//  3. Bead status == "closed" in the br DB.
//
// Bead: hk-sk00a; regression guard for hk-24xn1.
func TestScenario_QueueSubmit_IdleWake_hk24xn1(t *testing.T) {
	skipRealDaemonE2EInShort(t)
	// Not parallel: uses os.Setenv(HARMONIK_CLAUDE_CONFIG_PATH).
	twinPath, ok := scenariotest.TwinBinaryPath()
	if !ok {
		t.Skip("harmonik-twin-claude binary not found; set HARMONIK_TWIN_CLAUDE or build the binary")
	}

	realBrPath := queueSubmitDispatchBrPath(t)
	projectDir, jsonlPath := queueSubmitDispatchProjectDir(t)
	queueSubmitDispatchGitRepo(t, projectDir)

	dbPath := filepath.Join(projectDir, ".beads", "beads.db")
	brWrapper := queueSubmitDispatchBrWrapper(t, realBrPath, dbPath)
	beadID := queueSubmitDispatchInitBr(t, realBrPath, projectDir, brWrapper)
	t.Logf("queueSubmitDispatch idle-wake: seeded bead %s", beadID)

	twinWrapper := queueSubmitDispatchTwinWrapper(t, twinPath)
	// Dispatch in dot mode over the implementer→reviewer graph the twin
	// wrapper models. The embedded standard-bead.dot default would run its
	// commit_gate (go build/vet/tests) inside this three-file temp worktree
	// and fail every run for reasons unrelated to queue dispatch.
	scenariotest.WriteReviewLoopWorkflowDot(t, projectDir)

	claudeConfigPath := filepath.Join(t.TempDir(), ".claude.json")
	prevClaudeCfg, hadClaudeCfg := os.LookupEnv("HARMONIK_CLAUDE_CONFIG_PATH")
	require.NoError(t, os.Setenv("HARMONIK_CLAUDE_CONFIG_PATH", claudeConfigPath))
	// hk-1o0cc: restore prior value (TestMain package default) — see scenario_happypath_n1.
	t.Cleanup(func() {
		if hadClaudeCfg {
			_ = os.Setenv("HARMONIK_CLAUDE_CONFIG_PATH", prevClaudeCfg)
		} else {
			_ = os.Unsetenv("HARMONIK_CLAUDE_CONFIG_PATH")
		}
	})

	// Pre-create the QueueStore so the test holds the pointer for submit.
	qs := daemon.ExportedNewQueueStore()

	loopCtx, loopCancel := context.WithCancel(context.Background())
	defer loopCancel()

	cfg := daemon.Config{
		ProjectDir:            projectDir,
		JSONLLogPath:          jsonlPath,
		BrPath:                brWrapper,
		HandlerBinary:         twinWrapper,
		HandlerEnv:            os.Environ(), // commit-on-cue-startup-delay needs PATH to run git
		SkipWALCheckpoint:     true,
		SkipBrHistoryRotation: true,
		AgentReadyTimeout:     5 * time.Second,
		NoAutoPull:            true, // queue-only dispatch; prevents br-ready pre-emption (hk-24xn1)
		QueueStore:            qs,
		LogWriter:             testLogWriter{t: t},
		WorkflowModeDefault:   core.WorkflowModeDot,
	}

	startDone := make(chan error, 1)
	go func() {
		startDone <- daemon.Start(loopCtx, cfg)
	}()

	// Brief pause: give the daemon's workloop time to reach its idle select.
	// The exact timing is not critical; the wake channel is buffered (depth 1)
	// so a submit before the loop reaches select still wakes it.
	time.Sleep(200 * time.Millisecond)

	// ── Submit a queue with the single open bead ────────────────────────────

	// The HandlerAdapter uses the same QueueStore the daemon holds; SetQueue
	// fires the wake channel (hk-24xn1) so the idle loop wakes immediately.
	adapter := queue.NewHandlerAdapter(
		&qsdOpenLedger{}, // all beads open, no deps
		projectDir,
		qs,  // same QueueStore the daemon is watching
		nil, // no event bus seam needed in the test
	)

	submitReq := queue.QueueSubmitRequest{
		SchemaVersion: 1,
		Groups: []queue.Group{
			{
				Kind:  queue.GroupKindStream,
				Items: []queue.Item{{BeadID: beadID, Status: queue.ItemStatusPending}},
			},
		},
	}
	params, err := json.Marshal(submitReq)
	require.NoError(t, err, "marshal QueueSubmitRequest")

	raw, rpcErr := adapter.HandleQueueSubmit(t.Context(), params)
	require.Nil(t, rpcErr, "HandleQueueSubmit: unexpected RPCError: %v", rpcErr)
	require.NotNil(t, raw, "HandleQueueSubmit: nil response")

	var submitResp queue.QueueSubmitResponse
	require.NoError(t, json.Unmarshal(raw, &submitResp), "decode QueueSubmitResponse")
	t.Logf("queueSubmitDispatch idle-wake: submitted queue_id=%s", submitResp.QueueID)

	// ── Wait for the single terminal event ─────────────────────────────────

	const terminalBudget = 25 * time.Second
	scenariotest.MustCompleteWithin(t, jsonlPath, "", nil, terminalBudget, func() {
		for {
			if queueSubmitDispatchWaitRunTerminal(t, jsonlPath, 50*time.Millisecond) {
				return
			}
		}
	})

	loopCancel()
	scenariotest.MustCompleteWithin(t, jsonlPath, "", nil, 5*time.Second, func() {
		if err := <-startDone; err != nil {
			t.Errorf("daemon.Start returned error after cancel: %v", err)
		}
	})

	// ── Assertions ──────────────────────────────────────────────────────────

	// 1. run_started must appear: the daemon dispatched the bead.
	require.True(t,
		scenariotest.WaitForEvent(t, jsonlPath, string(core.EventTypeRunStarted), "", 100*time.Millisecond),
		"run_started must appear in JSONL after idle-wake dispatch (hk-24xn1)")

	// 2. run_completed must appear: the bead ran to completion.
	require.True(t,
		scenariotest.WaitForEvent(t, jsonlPath, string(core.EventTypeRunCompleted), "", 100*time.Millisecond),
		"run_completed must appear in JSONL after dispatch (hk-24xn1)")

	// 3. Bead must be closed in br.
	scenariotest.AssertBeadStatus(t, brWrapper, string(beadID), "closed")

	// 4. Causality: run_started must precede run_completed.
	scenariotest.AssertEventSequence(t, jsonlPath, []scenariotest.ExpectedEvent{
		{Type: string(core.EventTypeRunStarted)},
		{Type: string(core.EventTypeRunCompleted)},
	})

	t.Logf("TestScenario_QueueSubmit_IdleWake_hk24xn1: PASS bead=%s", beadID)
}

// ─────────────────────────────────────────────────────────────────────────────
// TestScenario_QueueSubmit_DeferredUndefer_hknbjht
// ─────────────────────────────────────────────────────────────────────────────

// TestScenario_QueueSubmit_DeferredUndefer_hknbjht verifies the full
// deferred-for-ledger-dep lifecycle end-to-end through daemon.Start + real br
// + harmonik-twin-claude:
//
//  1. Submit a stream queue containing [A(pending), B(deferred)] where B is
//     deferred at submit time because A blocks B per the ledger.
//  2. A dispatches first (B is still deferred; head-of-line is A).
//  3. After A completes, evaluateGroupAdvanceWithOutcome wakes the loop.
//  4. On the next workloop tick, ReevaluateDeferred detects A is terminal in
//     the queue and un-defers B to pending.
//  5. B dispatches and completes.
//  6. Both A and B are "closed" in br.
//
// The br dependency edge (B depends on A) is set up via `br dep add` so the
// queuewiring.BRQueueLedger reports BlocksEdge(A, B) = true during the
// ReevaluateDeferred pass (§2.8 un-defer condition check).
//
// Bead: hk-sk00a; regression guard for hk-nbjht.
func TestScenario_QueueSubmit_DeferredUndefer_hknbjht(t *testing.T) {
	skipRealDaemonE2EInShort(t)
	// Not parallel: uses os.Setenv(HARMONIK_CLAUDE_CONFIG_PATH).
	twinPath, ok := scenariotest.TwinBinaryPath()
	if !ok {
		t.Skip("harmonik-twin-claude binary not found; set HARMONIK_TWIN_CLAUDE or build the binary")
	}

	realBrPath := queueSubmitDispatchBrPath(t)
	projectDir, jsonlPath := queueSubmitDispatchProjectDir(t)
	queueSubmitDispatchGitRepo(t, projectDir)

	dbPath := filepath.Join(projectDir, ".beads", "beads.db")
	brWrapper := queueSubmitDispatchBrWrapper(t, realBrPath, dbPath)
	aID, bID := queueSubmitDispatchInitBrWithDep(t, realBrPath, projectDir, brWrapper)
	t.Logf("queueSubmitDispatch deferred-undefer: A=%s B=%s (B depends on A)", aID, bID)

	twinWrapper := queueSubmitDispatchTwinWrapper(t, twinPath)
	// Dispatch in dot mode over the implementer→reviewer graph the twin
	// wrapper models. The embedded standard-bead.dot default would run its
	// commit_gate (go build/vet/tests) inside this three-file temp worktree
	// and fail every run for reasons unrelated to queue dispatch.
	scenariotest.WriteReviewLoopWorkflowDot(t, projectDir)

	claudeConfigPath := filepath.Join(t.TempDir(), ".claude.json")
	prevClaudeCfg, hadClaudeCfg := os.LookupEnv("HARMONIK_CLAUDE_CONFIG_PATH")
	require.NoError(t, os.Setenv("HARMONIK_CLAUDE_CONFIG_PATH", claudeConfigPath))
	// hk-1o0cc: restore prior value (TestMain package default) — see scenario_happypath_n1.
	t.Cleanup(func() {
		if hadClaudeCfg {
			_ = os.Setenv("HARMONIK_CLAUDE_CONFIG_PATH", prevClaudeCfg)
		} else {
			_ = os.Unsetenv("HARMONIK_CLAUDE_CONFIG_PATH")
		}
	})

	qs := daemon.ExportedNewQueueStore()

	loopCtx, loopCancel := context.WithCancel(context.Background())
	defer loopCancel()

	cfg := daemon.Config{
		ProjectDir:            projectDir,
		JSONLLogPath:          jsonlPath,
		BrPath:                brWrapper,
		HandlerBinary:         twinWrapper,
		HandlerEnv:            os.Environ(), // commit-on-cue-startup-delay needs PATH to run git
		SkipWALCheckpoint:     true,
		SkipBrHistoryRotation: true,
		AgentReadyTimeout:     5 * time.Second,
		NoAutoPull:            true, // queue-only; prevents br-ready from racing the submit
		QueueStore:            qs,
		LogWriter:             testLogWriter{t: t},
		WorkflowModeDefault:   core.WorkflowModeDot,
		TargetBranch:          "integration",
		ProtectBranches:       []string{"main"},
	}

	startDone := make(chan error, 1)
	go func() {
		startDone <- daemon.Start(loopCtx, cfg)
	}()

	// Brief pause to let the workloop reach its idle select.
	time.Sleep(200 * time.Millisecond)

	// ── Submit [A(pending), B(deferred)] via HandlerAdapter ─────────────────

	queueSubmitDispatchWaitSocket(t, projectDir)
	submitOut := queueSubmitDispatchSubmitCLI(t, projectDir, []core.BeadID{aID, bID})
	t.Logf("queueSubmitDispatch deferred-undefer: submitted through socket: %s", submitOut)

	// Verify B was deferred at submit time: queue.json must show B as
	// deferred-for-ledger-dep immediately after HandleQueueSubmit returns.
	scenariotest.AssertQueueJSON(t, projectDir, scenariotest.QueueExpectation{
		ItemStatuses: []string{
			string(queue.ItemStatusPending),              // A: pending (eligible head)
			string(queue.ItemStatusDeferredForLedgerDep), // B: deferred behind A
		},
	})
	t.Log("queueSubmitDispatch deferred-undefer: B confirmed deferred-for-ledger-dep at submit time")

	// ── Wait for BOTH A and B to complete ───────────────────────────────────

	// Two separate run_completed / run_failed events must land: one for A,
	// one for B. Budget = 2 × single-bead budget + headroom.
	const terminalBudget = 45 * time.Second
	scenariotest.MustCompleteWithin(t, jsonlPath, "", nil, terminalBudget, func() {
		for {
			if queueSubmitDispatchCountRunTerminal(t, jsonlPath) >= 2 {
				return
			}
			time.Sleep(50 * time.Millisecond)
		}
	})

	loopCancel()
	scenariotest.MustCompleteWithin(t, jsonlPath, "", nil, 5*time.Second, func() {
		if err := <-startDone; err != nil {
			t.Errorf("daemon.Start returned error after cancel: %v", err)
		}
	})

	// ── Assertions ──────────────────────────────────────────────────────────

	// 1. Both beads must be closed in br.
	scenariotest.AssertBeadStatus(t, brWrapper, string(aID), "closed")
	scenariotest.AssertBeadStatus(t, brWrapper, string(bID), "closed")
	queueSubmitDispatchAssertLanded(t, projectDir, "integration", []core.BeadID{aID, bID})

	// 2. Two run_started events must appear: one dispatch per bead.
	runStartedCount := queueSubmitDispatchCountRunStarted(t, jsonlPath)
	require.GreaterOrEqual(t, runStartedCount, 2,
		"expected ≥2 run_started events (one per bead); got %d (hk-nbjht un-defer regression guard)",
		runStartedCount)

	// 3. Event sequence: run_started(A) → run_completed(A) → run_started(B) → run_completed(B).
	// Subsequence check — intervening events are allowed.
	scenariotest.AssertEventSequence(t, jsonlPath, []scenariotest.ExpectedEvent{
		{Type: string(core.EventTypeRunStarted)},
		{Type: string(core.EventTypeRunCompleted)},
		{Type: string(core.EventTypeRunStarted)},
		{Type: string(core.EventTypeRunCompleted)},
	})

	// 4. No orphan tmux windows (nil adapter → skipped in non-tmux environments).
	scenariotest.AssertNoOrphanTmuxWindows(t, nil)

	t.Logf("TestScenario_QueueSubmit_DeferredUndefer_hknbjht: PASS A=%s B=%s", aID, bID)
}

// TestScenario_QueueSubmit_FanOutFanIn proves that the stream group minted by
// `harmonik queue submit --beads ...` can run A, then B/C/D concurrently, then
// E without supervisor mutation.
func TestScenario_QueueSubmit_FanOutFanIn(t *testing.T) {
	skipRealDaemonE2EInShort(t)
	twinPath, ok := scenariotest.TwinBinaryPath()
	if !ok {
		t.Skip("harmonik-twin-claude binary not found; set HARMONIK_TWIN_CLAUDE or build the binary")
	}

	realBrPath := queueSubmitDispatchBrPath(t)
	projectDir, jsonlPath := queueSubmitDispatchProjectDir(t)
	queueSubmitDispatchGitRepo(t, projectDir)
	dbPath := filepath.Join(projectDir, ".beads", "beads.db")
	brWrapper := queueSubmitDispatchBrWrapper(t, realBrPath, dbPath)
	epicID, ids := queueSubmitDispatchInitBrFanGraph(t, realBrPath, projectDir, brWrapper)
	twinWrapper := queueSubmitDispatchTwinWrapper(t, twinPath)
	scenariotest.WriteStandardWorkflowDot(t, projectDir)

	claudeConfigPath := filepath.Join(t.TempDir(), ".claude.json")
	prevClaudeCfg, hadClaudeCfg := os.LookupEnv("HARMONIK_CLAUDE_CONFIG_PATH")
	require.NoError(t, os.Setenv("HARMONIK_CLAUDE_CONFIG_PATH", claudeConfigPath))
	t.Cleanup(func() {
		if hadClaudeCfg {
			_ = os.Setenv("HARMONIK_CLAUDE_CONFIG_PATH", prevClaudeCfg)
		} else {
			_ = os.Unsetenv("HARMONIK_CLAUDE_CONFIG_PATH")
		}
	})

	qs := daemon.ExportedNewQueueStore()
	loopCtx, loopCancel := context.WithCancel(context.Background())
	defer loopCancel()
	cfg := daemon.Config{
		ProjectDir:            projectDir,
		JSONLLogPath:          jsonlPath,
		BrPath:                brWrapper,
		HandlerBinary:         twinWrapper,
		HandlerEnv:            os.Environ(),
		SkipWALCheckpoint:     true,
		SkipBrHistoryRotation: true,
		AgentReadyTimeout:     15 * time.Second,
		MaxConcurrent:         3,
		NoAutoPull:            true,
		QueueStore:            qs,
		LogWriter:             testLogWriter{t: t},
		WorkflowModeDefault:   core.WorkflowModeDot,
		TargetBranch:          "integration",
		ProtectBranches:       []string{"main"},
	}
	startDone := make(chan error, 1)
	go func() { startDone <- daemon.Start(loopCtx, cfg) }()
	time.Sleep(200 * time.Millisecond)

	queueSubmitDispatchWaitSocket(t, projectDir)
	omittedRootDryRun := queueSubmitDispatchDryRunCLI(t, projectDir, ids[1:])
	require.Len(t, omittedRootDryRun.LedgerDepNotices, 3,
		"dry-run reports only dependency edges whose endpoints are both submitted")
	for _, item := range omittedRootDryRun.ResolvedQueue.Groups[0].Items[:3] {
		require.Equal(t, queue.ItemStatusPending, item.Status,
			"dry-run cannot defer %s when its omitted root blocker is outside the request", item.BeadID)
	}
	dryRun := queueSubmitDispatchDryRunCLI(t, projectDir, ids)
	require.True(t, dryRun.ParallelismNarrowed, "dependency graph must narrow initial parallelism")
	require.Len(t, dryRun.LedgerDepNotices, 6, "A→B/C/D and B/C/D→E must produce six edge notices")
	require.Equal(t, queue.ItemStatusPending, dryRun.ResolvedQueue.Groups[0].Items[0].Status,
		"root must be ready in the dry-run plan")
	for _, item := range dryRun.ResolvedQueue.Groups[0].Items[1:] {
		require.Equal(t, queue.ItemStatusDeferredForLedgerDep, item.Status,
			"blocked item %s must be deferred in the dry-run plan", item.BeadID)
	}
	_, statErr := os.Stat(filepath.Join(projectDir, ".harmonik", "queues", "main.json"))
	require.ErrorIs(t, statErr, os.ErrNotExist, "dry-run must not persist the queue")
	_ = queueSubmitDispatchSubmitCLI(t, projectDir, ids)

	const terminalBudget = 90 * time.Second
	scenariotest.MustCompleteWithin(t, jsonlPath, "", nil, terminalBudget, func() {
		for queueSubmitDispatchCountRunTerminal(t, jsonlPath) < len(ids) {
			time.Sleep(50 * time.Millisecond)
		}
	})
	loopCancel()
	scenariotest.MustCompleteWithin(t, jsonlPath, "", nil, 5*time.Second, func() {
		if startErr := <-startDone; startErr != nil {
			t.Errorf("daemon.Start returned error after cancel: %v", startErr)
		}
	})

	for _, id := range ids {
		scenariotest.AssertBeadStatus(t, brWrapper, string(id), "closed")
	}
	validationData, validationErr := os.ReadFile(filepath.Join(projectDir, ".harmonik", "validation-runs"))
	require.NoError(t, validationErr, "read durable validation evidence")
	require.Len(t, strings.Fields(string(validationData)), len(ids), "every child must pass the commit gate once")
	scenariotest.AssertBeadStatus(t, brWrapper, string(epicID), "open")
	epicEvents := queueSubmitDispatchEpicCompleted(t, jsonlPath, epicID)
	require.Len(t, epicEvents, 1, "the last child must emit one epic completion fact")
	require.NotNil(t, epicEvents[0].RunID, "epic completion must retain the last child run identity")
	derivedBranch, deriveErr := workspace.IntegrationBranchName(t.Context(), string(epicID))
	require.NoError(t, deriveErr, "derive epic integration branch")
	queueSubmitDispatchAssertLanded(t, projectDir, derivedBranch, ids)
	cmd := exec.CommandContext(t.Context(), "git", "rev-list", "--count", "main..integration")
	cmd.Dir = projectDir
	baseOut, baseErr := cmd.CombinedOutput()
	require.NoError(t, baseErr, "git rev-list main..integration\n%s", baseOut)
	require.Equal(t, "0", strings.TrimSpace(string(baseOut)),
		"configured integration base must stay unchanged when the epic has a derived branch")

	graphEvents := queueSubmitDispatchGraphEvents(t, jsonlPath, ids)
	require.Equal(t, ids[0], graphEvents.started[0], "A must start first")
	require.Less(t, graphEvents.completedAt[ids[0]], graphEvents.startedAt[ids[1]], "B must start after A completes")
	require.Less(t, graphEvents.completedAt[ids[0]], graphEvents.startedAt[ids[2]], "C must start after A completes")
	require.Less(t, graphEvents.completedAt[ids[0]], graphEvents.startedAt[ids[3]], "D must start after A completes")
	firstBranchCompletion := min(
		graphEvents.completedAt[ids[1]],
		min(graphEvents.completedAt[ids[2]], graphEvents.completedAt[ids[3]]),
	)
	branchStartsBeforeCompletion := 0
	for _, id := range ids[1:4] {
		if graphEvents.startedAt[id] < firstBranchCompletion {
			branchStartsBeforeCompletion++
		}
	}
	require.GreaterOrEqual(t, branchStartsBeforeCompletion, 2, "fan-out must overlap at least two branch runs")
	for _, id := range ids[1:4] {
		require.Less(t, graphEvents.completedAt[id], graphEvents.startedAt[ids[4]], "E must start after %s completes", id)
	}
	t.Logf("TestScenario_QueueSubmit_FanOutFanIn: PASS graph=%v", ids)
}

// TestScenario_QueueSubmit_FailedBlockerPauses proves that a failed root does
// not launch its dependent. The queue records the failure and pauses for an
// explicit recovery decision.
func TestScenario_QueueSubmit_FailedBlockerPauses(t *testing.T) {
	skipRealDaemonE2EInShort(t)
	twinPath, ok := scenariotest.TwinBinaryPath()
	if !ok {
		t.Skip("harmonik-twin-claude binary not found; set HARMONIK_TWIN_CLAUDE or build the binary")
	}

	realBrPath := queueSubmitDispatchBrPath(t)
	projectDir, jsonlPath := queueSubmitDispatchProjectDir(t)
	queueSubmitDispatchGitRepo(t, projectDir)
	dbPath := filepath.Join(projectDir, ".beads", "beads.db")
	brWrapper := queueSubmitDispatchBrWrapper(t, realBrPath, dbPath)
	aID, bID := queueSubmitDispatchInitBrWithDep(t, realBrPath, projectDir, brWrapper)
	queueSubmitDispatchInstallFailingGate(t, projectDir)
	scenariotest.WriteStandardWorkflowDot(t, projectDir)

	qs := daemon.ExportedNewQueueStore()
	loopCtx, loopCancel := context.WithCancel(context.Background())
	defer loopCancel()
	cfg := daemon.Config{
		ProjectDir:            projectDir,
		JSONLLogPath:          jsonlPath,
		BrPath:                brWrapper,
		HandlerBinary:         queueSubmitDispatchTwinWrapper(t, twinPath),
		HandlerEnv:            os.Environ(),
		SkipWALCheckpoint:     true,
		SkipBrHistoryRotation: true,
		AgentReadyTimeout:     15 * time.Second,
		MaxConcurrent:         1,
		NoAutoPull:            true,
		QueueStore:            qs,
		LogWriter:             testLogWriter{t: t},
		WorkflowModeDefault:   core.WorkflowModeDot,
		TargetBranch:          "integration",
		ProtectBranches:       []string{"main"},
	}
	startDone := make(chan error, 1)
	go func() { startDone <- daemon.Start(loopCtx, cfg) }()
	queueSubmitDispatchWaitSocket(t, projectDir)
	_ = queueSubmitDispatchSubmitCLI(t, projectDir, []core.BeadID{aID, bID})

	scenariotest.MustCompleteWithin(t, jsonlPath, "", nil, 75*time.Second, func() {
		for {
			q, err := queue.Load(t.Context(), projectDir, queue.QueueNameMain)
			if err == nil && q != nil && q.Status == queue.QueueStatusPausedByFailure {
				return
			}
			time.Sleep(50 * time.Millisecond)
		}
	})
	loopCancel()
	scenariotest.MustCompleteWithin(t, jsonlPath, "", nil, 5*time.Second, func() {
		if startErr := <-startDone; startErr != nil {
			t.Errorf("daemon.Start returned error after cancel: %v", startErr)
		}
	})

	require.Equal(t, 1, queueSubmitDispatchCountRunStarted(t, jsonlPath),
		"only failed root A may start; dependent B must not launch")
	scenariotest.AssertBeadStatus(t, brWrapper, string(aID), "open")
	scenariotest.AssertBeadStatus(t, brWrapper, string(bID), "open")
	_, validationErr := os.Stat(filepath.Join(projectDir, ".harmonik", "validation-runs"))
	require.ErrorIs(t, validationErr, os.ErrNotExist, "a failed gate must not record a validation pass")
	cmd := exec.CommandContext(t.Context(), "git", "rev-list", "--count", "main..integration")
	cmd.Dir = projectDir
	landed, landedErr := cmd.CombinedOutput()
	require.NoError(t, landedErr, "count failed-gate landings: %s", landed)
	require.Equal(t, "0", strings.TrimSpace(string(landed)), "failed validation must not merge the root")
	pausedQueue, pausedErr := queue.Load(t.Context(), projectDir, queue.QueueNameMain)
	require.NoError(t, pausedErr, "load failed queue")
	require.Equal(t, queue.ItemStatusFailed, pausedQueue.Groups[0].Items[1].Status,
		"the core must fail the dependent without offering it for dispatch")
	require.Equal(t, "dependency_failed:"+string(aID), pausedQueue.Groups[0].Items[1].LastFailureReason)
	scenariotest.AssertEventSequence(t, jsonlPath, []scenariotest.ExpectedEvent{
		{Type: string(core.EventTypeRunStarted)},
		{Type: string(core.EventTypeRunFailed)},
		{Type: string(core.EventTypeQueueGroupCompleted)},
		{Type: string(core.EventTypeQueuePaused)},
	})
}

// TestScenario_QueueSubmit_CleanStopResumesPendingGraph proves a clean daemon
// restart preserves and continues a pending queue without resubmission.
func TestScenario_QueueSubmit_CleanStopResumesPendingGraph(t *testing.T) {
	skipRealDaemonE2EInShort(t)
	twinPath, ok := scenariotest.TwinBinaryPath()
	if !ok {
		t.Skip("harmonik-twin-claude binary not found; set HARMONIK_TWIN_CLAUDE or build the binary")
	}
	realBrPath := queueSubmitDispatchBrPath(t)
	projectDir, jsonlPath := queueSubmitDispatchProjectDir(t)
	queueSubmitDispatchGitRepo(t, projectDir)
	dbPath := filepath.Join(projectDir, ".beads", "beads.db")
	brWrapper := queueSubmitDispatchBrWrapper(t, realBrPath, dbPath)
	beadID := queueSubmitDispatchInitBr(t, realBrPath, projectDir, brWrapper)
	twinWrapper := queueSubmitDispatchTwinWrapper(t, twinPath)
	scenariotest.WriteReviewLoopWorkflowDot(t, projectDir)

	now := time.Now().UTC()
	queueUUID, err := uuid.NewV7()
	require.NoError(t, err, "mint queue UUIDv7")
	group := queue.NewActiveGroup(queue.Group{
		GroupIndex: 0,
		Kind:       queue.GroupKindStream,
		Items: []queue.Item{{
			BeadID: beadID,
			Status: queue.ItemStatusPending,
		}},
		CreatedAt: now,
	})
	q := queue.NewActiveQueue(queue.Queue{
		SchemaVersion: 1,
		QueueID:       queueUUID.String(),
		Name:          queue.QueueNameMain,
		Workers:       1,
		SubmittedAt:   now,
		Groups:        []queue.Group{group},
	})
	require.NoError(t, queue.Persist(t.Context(), projectDir, &q), "persist active graph queue")

	pauseBus := eventbus.NewBusImpl()
	require.NoError(t, pauseBus.Seal(), "seal handler-pause bus")
	pauseCtrl := daemon.NewHandlerPauseController(pauseBus, nil)
	require.NoError(t, pauseCtrl.Pause(t.Context(), core.AgentTypeClaudeCode, core.HandlerPauseCause{
		FailureClass: core.FailureClassTransient,
		SubReason:    "rate_limit",
		SourceRunID:  "clean-stop-fixture",
		SourceBeadID: string(beadID),
		TrippedAt:    now.Format(time.RFC3339Nano),
	}, nil), "pause handler before daemon start")

	startAndStop := func(waitForRun bool) {
		t.Helper()
		loopCtx, loopCancel := context.WithCancel(context.Background())
		defer loopCancel()
		qs := daemon.ExportedNewQueueStore()
		startDone := make(chan error, 1)
		go func() {
			startDone <- daemon.Start(loopCtx, daemon.Config{
				ProjectDir:             projectDir,
				JSONLLogPath:           jsonlPath,
				BrPath:                 brWrapper,
				HandlerBinary:          twinWrapper,
				HandlerEnv:             os.Environ(),
				AgentReadyTimeout:      15 * time.Second,
				NoAutoPull:             true,
				QueueStore:             qs,
				HandlerPauseController: pauseCtrl,
				SkipWALCheckpoint:      true,
				SkipBrHistoryRotation:  true,
				SkipRestartBackoff:     true,
				LogWriter:              testLogWriter{t: t},
				WorkflowModeDefault:    core.WorkflowModeDot,
				TargetBranch:           "integration",
				ProtectBranches:        []string{"main"},
			})
		}()
		if waitForRun {
			queueSubmitDispatchWaitSocket(t, projectDir)
			scenariotest.MustCompleteWithin(t, jsonlPath, "", nil, 45*time.Second, func() {
				for queueSubmitDispatchCountRunTerminal(t, jsonlPath) < 1 {
					time.Sleep(50 * time.Millisecond)
				}
			})
		} else {
			// Socket readiness proves startup loaded the queue. The handler pause
			// holds launch while cancellation drives the work-loop drain.
			queueSubmitDispatchWaitSocket(t, projectDir)
			loopCancel()
		}
		if waitForRun {
			loopCancel()
		}
		scenariotest.MustCompleteWithin(t, jsonlPath, "", nil, 10*time.Second, func() {
			require.NoError(t, <-startDone, "daemon clean stop")
		})
	}

	startAndStop(false)
	require.Equal(t, 0, queueSubmitDispatchCountRunStarted(t, jsonlPath),
		"paused handler must keep the fixture at a between-run stop point")
	scenariotest.AssertBeadStatus(t, brWrapper, string(beadID), "open")
	loaded, loadErr := queue.Load(t.Context(), projectDir, queue.QueueNameMain)
	require.NoError(t, loadErr, "load queue after clean stop")
	require.NotNil(t, loaded, "clean stop must preserve the canonical queue")
	require.Equal(t, queue.QueueStatusPausedByDrain, loaded.Status)
	require.True(t, loaded.ResumeOnStart, "clean stop must mark the one-shot restart intent")

	require.NoError(t, pauseCtrl.Resume(t.Context(), core.AgentTypeClaudeCode, core.HandlerResumedByOperator),
		"release fixture handler pause before restart")
	startAndStop(true)
	require.Equal(t, 1, queueSubmitDispatchCountRunStarted(t, jsonlPath),
		"restart must continue the preserved queue exactly once")
	scenariotest.AssertBeadStatus(t, brWrapper, string(beadID), "closed")
	queueSubmitDispatchAssertLanded(t, projectDir, "integration", []core.BeadID{beadID})
}

type queueSubmitDispatchGraphObservation struct {
	started     []core.BeadID
	startedAt   map[core.BeadID]int
	completedAt map[core.BeadID]int
}

func queueSubmitDispatchGraphEvents(t *testing.T, jsonlPath string, ids []core.BeadID) queueSubmitDispatchGraphObservation {
	t.Helper()
	data, err := os.ReadFile(jsonlPath) //nolint:gosec // test temp path
	require.NoError(t, err, "read graph event log")
	obs := queueSubmitDispatchGraphObservation{
		startedAt:   make(map[core.BeadID]int),
		completedAt: make(map[core.BeadID]int),
	}
	for pos, line := range strings.Split(string(data), "\n") {
		for _, id := range ids {
			if !strings.Contains(line, string(id)) {
				continue
			}
			if strings.Contains(line, `"type":"run_started"`) {
				obs.started = append(obs.started, id)
				obs.startedAt[id] = pos
			}
			if strings.Contains(line, `"type":"run_completed"`) {
				obs.completedAt[id] = pos
			}
		}
	}
	require.Len(t, obs.startedAt, len(ids), "every graph bead must have run_started")
	require.Len(t, obs.completedAt, len(ids), "every graph bead must have run_completed")
	return obs
}
