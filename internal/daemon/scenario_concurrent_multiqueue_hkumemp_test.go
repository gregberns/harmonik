//go:build scenario

package daemon_test

// scenario_concurrent_multiqueue_hkumemp_test.go — concurrent multi-queue
// dispatch scenario (hk-umemp).
//
// # What is tested
//
// TestScenario_ConcurrentMultiQueue_N2_HappyPath boots the full daemon.Start
// composition root with two active named queues ("alpha" and "beta"), each
// holding distinct beads plus a shared "dup" bead that appears in both queues.
// MaxConcurrent=2 with Workers=1 per queue exercises all four scenario
// correctness properties concurrently:
//
//  (a) Both queue-unique beads (alphaA, betaB) dispatch, complete, and close
//      in br — confirming normal concurrent dispatch across two named queues.
//
//  (b) QM-062 two-level cap is honored: at no point do more than
//      MaxConcurrent=2 runs appear in-flight simultaneously (tracked by
//      counting run_started minus run_completed/run_failed events in order).
//
//  (c) The dup bead dispatches exactly once: the "winner" queue dispatches it
//      and closes it in br; the "loser" queue sees its item failed with
//      LastFailureReason containing "cross_queue_duplicate" (hk-a11re guard).
//      No run starts for the loser copy — ClaimBead is never called for it.
//
//  (d) A sibling merge to main (when the first run completes and its worktree
//      is merged) does NOT emit implementer_escaped_worktree for the other
//      in-flight run (hk-77q8e sibling-exclusion fix).
//
// TestScenario_ConcurrentMultiQueue_N2_MidRunKill exercises the G1 cause-side:
// it starts the same two-queue setup but cancels the daemon context while runs
// are in-flight via a blocking twin wrapper (sleep 3600). After daemon exit:
//
//  - At least one run_started event appears (a bead was actually dispatched).
//  - run_completed is absent (no run finished before the kill).
//  - The dispatched bead is NOT closed in br (still open or in_progress).
//
// This confirms the root cause of the stuck-queue state that
// TestScenario_RestartRecovery_QM002bDeadlock (hk-ivzsl) exercises from the
// recovery side: a live mid-run kill leaves a bead in ItemStatusDispatched in
// the queue file, which the next daemon startup must reconcile via QM-002b
// Class A'.
//
// # Helper prefix
//
// Helpers in this file use the prefix "cmq" (concurrent-multi-queue).
// Per implementer-protocol.md §Helper-prefix discipline.
//
// # Spec refs
//
//   - specs/queue-model.md §9.3 QM-062 (two-level capacity cap)
//   - specs/queue-model.md §9.7 QM-066 (per-queue worker count)
//   - specs/queue-model.md §9.8 QM-067 (cross-queue round-robin)
//   - specs/queue-model.md §6.3 QM-022 (no double dispatch from any source)
//   - specs/queue-model.md §3.2b QM-002b Class A' (dispatched+closed reconciliation)
//
// Bead: hk-umemp.
// Refs: hk-77q8e, hk-a11re, hk-tigaf.5, QM-062, QM-067.

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/daemon/scenariotest"
	"github.com/gregberns/harmonik/internal/queue"
)

// ─────────────────────────────────────────────────────────────────────────────
// cmq fixture helpers
// ─────────────────────────────────────────────────────────────────────────────

// cmqEvalSymlinks resolves all symlinks in path so that br — which rejects
// paths containing symlinks outside the beads directory — receives a canonical
// path. On macOS, t.TempDir() returns /var/folders/... which is a symlink to
// /private/var/folders/..., triggering br's symlink guard.
func cmqEvalSymlinks(t *testing.T, path string) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(path)
	require.NoError(t, err, "cmqEvalSymlinks: EvalSymlinks %q", path)
	return resolved
}

// cmqProjectDir creates the minimal project directory for the scenario.
// Returns the project dir and the JSONL events log path.
func cmqProjectDir(t *testing.T) (projectDir, jsonlPath string) {
	t.Helper()
	projectDir = cmqEvalSymlinks(t, t.TempDir())
	for _, sub := range []string{
		filepath.Join(".harmonik", "events"),
		filepath.Join(".harmonik", "beads-intents"),
		filepath.Join(".harmonik", "queues"),
	} {
		//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
		require.NoError(t,
			os.MkdirAll(filepath.Join(projectDir, sub), 0o755),
			"cmqProjectDir: mkdir %s", sub,
		)
	}
	jsonlPath = filepath.Join(projectDir, ".harmonik", "events", "events.jsonl")
	return projectDir, jsonlPath
}

// cmqGitRepo initialises a git repository with one commit in dir, and wires a
// bare-repo "origin" remote so that mergeRunBranchToMain's git-push step
// succeeds (avoiding push_failed run_failed events when the twin makes commits).
func cmqGitRepo(t *testing.T, dir string) {
	t.Helper()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.CommandContext(t.Context(), "git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "cmqGitRepo: git %v\n%s", args, out)
	}
	run("init", "--initial-branch=main")
	run("config", "user.email", "test@harmonik.local")
	run("config", "user.name", "Harmonik CMQ Test")
	readmePath := filepath.Join(dir, "README")
	require.NoError(t, os.WriteFile(readmePath, []byte("cmq scenario test\n"), 0o644), "cmqGitRepo: write README")
	run("add", "README")
	run("commit", "-m", "Initial commit")

	// Add a bare-repo origin so mergeRunBranchToMain's push step succeeds.
	// Without a remote the push fails with "fatal: 'origin' does not appear to
	// be a git repository" and the run is reopened as push_failed (run_failed).
	raw := t.TempDir()
	originDir, err := filepath.EvalSymlinks(raw)
	require.NoError(t, err, "cmqGitRepo: EvalSymlinks originDir")
	initBareCmd := exec.CommandContext(t.Context(), "git", "init", "--bare", "--initial-branch=main", originDir)
	out, err := initBareCmd.CombinedOutput()
	require.NoError(t, err, "cmqGitRepo: git init --bare\n%s", out)
	run("remote", "add", "origin", originDir)
	run("push", "origin", "main")
}

// cmqBrPath returns the path to the real `br` binary, skipping the test when
// br is not on PATH.
func cmqBrPath(t *testing.T) string {
	t.Helper()
	brPath, err := exec.LookPath("br")
	if err != nil {
		t.Skip("cmq: br required for scenario test (not on PATH)")
	}
	return brPath
}

// cmqBrWrapperScript writes a /bin/sh wrapper that invokes realBrPath with
// --db <dbPath> prepended to all args. Returns the wrapper path.
func cmqBrWrapperScript(t *testing.T, realBrPath, dbPath string) string {
	t.Helper()
	dir := cmqEvalSymlinks(t, t.TempDir())
	path := filepath.Join(dir, "br")
	content := "#!/bin/sh\nexec " + realBrPath + " --db " + dbPath + " \"$@\"\n"
	//nolint:gosec // G306: script is test-only; chmod 0755 required for execution
	require.NoError(t, os.WriteFile(path, []byte(content), 0o755), "cmqBrWrapperScript: WriteFile")
	return path
}

// cmqInitBrWithBeads initialises a beads workspace and creates three beads:
//   - dupBeadID: appears in both queues (cross-queue dedup target, hk-a11re)
//   - alphaAID: unique to the "alpha" queue
//   - betaBID: unique to the "beta" queue
//
// Returns (dupBeadID, alphaAID, betaBID).
func cmqInitBrWithBeads(t *testing.T, realBrPath, projectDir, brWrapper string) (dupBeadID, alphaAID, betaBID string) {
	t.Helper()
	//nolint:gosec // G204: br args are test-internal literals; not user input
	initCmd := exec.CommandContext(t.Context(), realBrPath, "init", "--prefix", "cmq")
	initCmd.Dir = projectDir
	initOut, initErr := initCmd.CombinedOutput()
	require.NoError(t, initErr, "cmqInitBrWithBeads: br init: %s", initOut)

	createBead := func(title string) string {
		t.Helper()
		//nolint:gosec // G204: br args are test-internal literals; not user input
		cmd := exec.CommandContext(t.Context(), brWrapper, "create", title, "--status", "open", "--silent")
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "cmqInitBrWithBeads: br create %q: %s", title, out)
		id := strings.TrimSpace(string(out))
		require.NotEmpty(t, id, "cmqInitBrWithBeads: br create %q returned empty ID", title)
		return id
	}

	dupBeadID = createBead("cmq dup bead — cross-queue dedup target (hk-a11re)")
	alphaAID = createBead("cmq alpha-A bead — unique to alpha queue")
	betaBID = createBead("cmq beta-B bead — unique to beta queue")
	return dupBeadID, alphaAID, betaBID
}

// cmqBuildActiveWaveQueue builds an active wave queue with Workers=1 holding
// the given beads as pending items (group 0, active).
//
// Workers=1 means at most 1 in-flight run for this queue at any time (QM-066).
// Combined with MaxConcurrent=2, two queues each at Workers=1 fill the global
// ceiling exactly.
func cmqBuildActiveWaveQueue(name, queueID string, beadIDs ...core.BeadID) *queue.Queue {
	items := make([]queue.Item, len(beadIDs))
	for i, id := range beadIDs {
		items[i] = queue.Item{BeadID: id, Status: queue.ItemStatusPending}
	}
	now := time.Now().UTC()
	started := now
	return &queue.Queue{
		SchemaVersion: 1,
		QueueID:       queueID,
		Name:          name,
		Workers:       1,
		SubmittedAt:   now,
		Status:        queue.QueueStatusActive,
		Groups: []queue.Group{
			{
				GroupIndex: 0,
				Kind:       queue.GroupKindWave,
				Status:     queue.GroupStatusActive,
				Items:      items,
				CreatedAt:  now,
				StartedAt:  &started,
			},
		},
	}
}

// cmqTwinWrapperScript writes a /bin/sh wrapper that is phase-aware so these
// review-loop runs complete (hk-4f5ua).
//
// Phase detection is by the presence of .harmonik/review-target.md, which the
// daemon writes ONLY into the reviewer's isolated worktree:
//
//   - Implementer phase (review-target.md absent): invoke the twin with
//     --scenario single-happy-path. The twin emits the agent protocol events
//     without making any git commits; the no-commit guard (hk-mmh8f) is
//     satisfied by the emptyCommitWorktreeFactory injected via
//     WithWorktreeFactory, which pre-commits before the handler binary starts,
//     serialising commits and eliminating the concurrent-merge race.
//   - Reviewer phase (review-target.md present): write an APPROVE verdict to
//     $PWD/.harmonik/review.json so the review loop terminates with success →
//     run_completed + bead closed. The reviewer must NOT commit (its worktree
//     gets no pre-commit from the factory).
//
// Before hk-81n9r these runs were single-mode (no reviewer); hk-81n9r made them
// review-loop, so the reviewer phase ran single-happy-path too, wrote no
// verdict, and tripped "verdict absent at iteration 1".
func cmqTwinWrapperScript(t *testing.T, twinPath string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "twin-cmq-wrapper.sh")
	content := `#!/bin/sh
set -e
if [ -f "$PWD/.harmonik/review-target.md" ]; then
  mkdir -p "$PWD/.harmonik"
  printf '{"schema_version":1,"verdict":"APPROVE","flags":[],"notes":"cmq review-loop happy path"}' > "$PWD/.harmonik/review.json"
  exit 0
fi
exec "` + twinPath + `" --scenario single-happy-path
`
	//nolint:gosec // G306: script is test-only; chmod 0755 required for execution
	require.NoError(t, os.WriteFile(path, []byte(content), 0o755), "cmqTwinWrapperScript: WriteFile")
	return path
}

// cmqBlockingTwinWrapperScript writes a /bin/sh wrapper that blocks
// indefinitely (sleep 3600). Used by the mid-run-kill sub-test to guarantee
// that runs are in-flight when the daemon context is cancelled.
func cmqBlockingTwinWrapperScript(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "twin-cmq-blocking.sh")
	content := "#!/bin/sh\nsleep 3600\n"
	//nolint:gosec // G306: script is test-only; chmod 0755 required for execution
	require.NoError(t, os.WriteFile(path, []byte(content), 0o755), "cmqBlockingTwinWrapperScript: WriteFile")
	return path
}

// cmqMaxConcurrentRuns scans the JSONL at jsonlPath and returns the maximum
// number of runs that were concurrently in-flight at any point. It increments
// the counter on run_started and decrements on run_completed or run_failed.
func cmqMaxConcurrentRuns(t *testing.T, jsonlPath string) int {
	t.Helper()
	//nolint:gosec // G304: path is t.TempDir()-based; not user input
	f, err := os.Open(jsonlPath)
	if os.IsNotExist(err) {
		return 0
	}
	require.NoError(t, err, "cmqMaxConcurrentRuns: open %s", jsonlPath)
	defer func() {
		if closeErr := f.Close(); closeErr != nil {
			t.Logf("cmqMaxConcurrentRuns: close: %v", closeErr)
		}
	}()

	var current, maxSeen int
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var env struct {
			Type string `json:"type"`
		}
		if json.Unmarshal([]byte(line), &env) != nil {
			continue
		}
		switch env.Type {
		case string(core.EventTypeRunStarted):
			current++
			if current > maxSeen {
				maxSeen = current
			}
		case string(core.EventTypeRunCompleted), string(core.EventTypeRunFailed):
			if current > 0 {
				current--
			}
		}
	}
	return maxSeen
}

// cmqEventCount returns the number of JSONL events matching eventType.
func cmqEventCount(t *testing.T, jsonlPath, eventType string) int {
	t.Helper()
	//nolint:gosec // G304: path is t.TempDir()-based; not user input
	f, err := os.Open(jsonlPath)
	if os.IsNotExist(err) {
		return 0
	}
	require.NoError(t, err, "cmqEventCount: open %s", jsonlPath)
	defer func() {
		if closeErr := f.Close(); closeErr != nil {
			t.Logf("cmqEventCount: close: %v", closeErr)
		}
	}()

	var count int
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var env struct {
			Type string `json:"type"`
		}
		if json.Unmarshal([]byte(line), &env) != nil {
			continue
		}
		if env.Type == eventType {
			count++
		}
	}
	return count
}

// cmqItemState holds the observable fields of a queue item for assertions.
type cmqItemState struct {
	BeadID            string
	Status            string
	LastFailureReason string
}

// cmqLoadQueueItems reads .harmonik/queues/<name>.json and returns the items
// in group 0. Returns nil when the file is absent (queue completed and unlinked).
func cmqLoadQueueItems(t *testing.T, projectDir, queueName string) []cmqItemState {
	t.Helper()
	queuePath := filepath.Join(projectDir, ".harmonik", "queues", queueName+".json")
	//nolint:gosec // G304: path is t.TempDir()-based; not user input
	data, err := os.ReadFile(queuePath)
	if os.IsNotExist(err) {
		return nil // queue completed + unlinked: acceptable
	}
	require.NoError(t, err, "cmqLoadQueueItems: read %s", queuePath)

	var q struct {
		Groups []struct {
			Items []struct {
				BeadID            string `json:"bead_id"`
				Status            string `json:"status"`
				LastFailureReason string `json:"last_failure_reason"`
			} `json:"items"`
		} `json:"groups"`
	}
	require.NoError(t, json.Unmarshal(data, &q), "cmqLoadQueueItems: unmarshal %s", queuePath)
	if len(q.Groups) == 0 {
		return nil
	}
	items := make([]cmqItemState, len(q.Groups[0].Items))
	for i, item := range q.Groups[0].Items {
		items[i] = cmqItemState{
			BeadID:            item.BeadID,
			Status:            item.Status,
			LastFailureReason: item.LastFailureReason,
		}
	}
	return items
}

// cmqPollBeadClosed polls `br show <id>` every 10 ms for up to budget.
// Returns true if the bead reaches "closed" status within budget.
func cmqPollBeadClosed(t *testing.T, brWrapper, beadID string, budget time.Duration) bool {
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

// cmqPollRunStartedCount polls the JSONL log until at least wantCount
// run_started events appear, up to budget. Returns the count found.
func cmqPollRunStartedCount(t *testing.T, jsonlPath string, wantCount int, budget time.Duration) int {
	t.Helper()
	deadline := time.Now().Add(budget)
	for time.Now().Before(deadline) {
		if n := cmqEventCount(t, jsonlPath, string(core.EventTypeRunStarted)); n >= wantCount {
			return n
		}
		time.Sleep(20 * time.Millisecond)
	}
	return cmqEventCount(t, jsonlPath, string(core.EventTypeRunStarted))
}

// ─────────────────────────────────────────────────────────────────────────────
// TestScenario_ConcurrentMultiQueue_N2_HappyPath
// ─────────────────────────────────────────────────────────────────────────────

// TestScenario_ConcurrentMultiQueue_N2_HappyPath is the full concurrent
// multi-queue happy-path scenario.
//
// Setup:
//  1. TempDir project with git + br DB.
//  2. Three ready beads: dupBead (in both queues), alphaA (alpha only), betaB (beta only).
//  3. Queue "alpha" (Workers=1): [dupBead, alphaA].
//  4. Queue "beta"  (Workers=1): [dupBead, betaB].
//  5. daemon.Start wired with MaxConcurrent=2 and a harmonik-twin-claude wrapper.
//
// Expected dispatch order (with round-robin + Workers=1 per queue):
//   tick 1 — alpha dispatches dupBead (1 alpha in-flight, 1 global)
//   tick 2 — beta tries dupBead → cross_queue_duplicate guard fires;
//             beta then dispatches betaB (1 beta in-flight, 2 global)
//   dupBead + betaB run concurrently (global at cap = 2)
//   dupBead completes → alpha slot freed; alpha dispatches alphaA
//   betaB completes → all done
//
// Assertions:
//  (a) alphaA and betaB are closed in br; dupBead is closed by alpha.
//  (b) Max concurrent runs observed ≤ MaxConcurrent=2 (QM-062).
//  (c) Beta's dupBead item: status=failed, reason contains
//      "cross_queue_duplicate" (hk-a11re).
//  (d) implementer_escaped_worktree event is absent (hk-77q8e).
//
// Not parallel: uses os.Setenv(HARMONIK_CLAUDE_CONFIG_PATH) to isolate
// EnsureWorktreeTrust — same rationale as TestScenario_HappyPath_N1.
//
// Bead: hk-umemp.
func TestScenario_ConcurrentMultiQueue_N2_HappyPath(t *testing.T) {
	// Locate the twin binary; skip when absent.
	twinPath, ok := scenariotest.TwinBinaryPath()
	if !ok {
		t.Skip("cmq: harmonik-twin-claude binary not found; set HARMONIK_TWIN_CLAUDE or build the binary")
	}

	// Locate br binary.
	realBrPath := cmqBrPath(t)

	// Create project directory with git repo and br DB.
	projectDir, jsonlPath := cmqProjectDir(t)
	cmqGitRepo(t, projectDir)

	// Initialise br DB and create three beads.
	dbPath := filepath.Join(projectDir, ".beads", "beads.db")
	brWrapper := cmqBrWrapperScript(t, realBrPath, dbPath)
	dupBeadID, alphaAID, betaBID := cmqInitBrWithBeads(t, realBrPath, projectDir, brWrapper)
	t.Logf("cmq: dupBead=%s alphaA=%s betaB=%s", dupBeadID, alphaAID, betaBID)

	// Pre-seed both named queues on disk so LoadQueueAtStartup enumerates them.
	//
	// Queue "alpha": [dupBead (item 0), alphaA (item 1)]
	// Queue "beta":  [dupBead (item 0), betaB  (item 1)]
	//
	// dupBead at index 0 in alpha ensures alpha dispatches it before beta can
	// claim a slot, triggering the cross_queue_duplicate guard in beta.
	ctx := t.Context()
	qAlpha := cmqBuildActiveWaveQueue("alpha", "00000000-0000-7a00-8000-aa1000000001",
		core.BeadID(dupBeadID), core.BeadID(alphaAID))
	qBeta := cmqBuildActiveWaveQueue("beta", "00000000-0000-7a00-8000-bb2000000002",
		core.BeadID(dupBeadID), core.BeadID(betaBID))
	require.NoError(t, queue.Persist(ctx, projectDir, qAlpha), "cmq: persist alpha queue")
	require.NoError(t, queue.Persist(ctx, projectDir, qBeta), "cmq: persist beta queue")

	// Build the twin wrapper script (ignores Claude-specific flags).
	twinWrapper := cmqTwinWrapperScript(t, twinPath)

	// Redirect EnsureWorktreeTrust to a test-local claude config so this test
	// does not contend with the running harmonik daemon on ~/.claude.json.lock.
	claudeConfigPath := filepath.Join(t.TempDir(), ".claude.json")
	if err := os.Setenv("HARMONIK_CLAUDE_CONFIG_PATH", claudeConfigPath); err != nil {
		t.Fatalf("cmq: Setenv HARMONIK_CLAUDE_CONFIG_PATH: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Unsetenv("HARMONIK_CLAUDE_CONFIG_PATH"); err != nil {
			t.Logf("cmq: Unsetenv HARMONIK_CLAUDE_CONFIG_PATH: %v", err)
		}
	})

	// Wire daemon.Config with production composition root.
	loopCtx, loopCancel := context.WithCancel(context.Background())
	defer loopCancel()

	cfg := daemon.Config{
		ProjectDir:            projectDir,
		JSONLLogPath:          jsonlPath,
		BrPath:                brWrapper,
		HandlerBinary:         twinWrapper,
		NoAutoPull:            true, // queue-only mode: no br-ready fallback
		MaxConcurrent:         2,    // global ceiling = 2 (QM-062)
		SkipWALCheckpoint:     true,
		SkipBrHistoryRotation: true,
		SkipRestartBackoff:    true,
		AgentReadyTimeout:     5 * time.Second,
		LogWriter:             testLogWriter{t: t},
		WorkflowModeDefault:   core.WorkflowModeReviewLoop,
	}

	// Launch daemon.StartForTesting with:
	//  - emptyCommitWorktreeFactory: satisfies the no-commit guard (hk-mmh8f)
	//    by pre-committing an --allow-empty commit in the worktree BEFORE the
	//    handler binary starts, without requiring the handler to run git.
	//  - WithMergeMutex: serialises the full rebase → update-ref → push sequence
	//    across all concurrent bead goroutines so that concurrent merges from
	//    dupBead and betaB do not race on refs/heads/main (non-fast-forward push
	//    failures observed without the mutex when both goroutines push at the same
	//    time to the shared bare-repo origin).
	var mergeMu sync.Mutex
	startDone := make(chan error, 1)
	go func() {
		startDone <- daemon.StartForTesting(loopCtx, cfg,
			daemon.WithWorktreeFactory(emptyCommitWorktreeFactory),
			daemon.WithMergeMutex(&mergeMu),
		)
	}()

	// ── Phase 1: wait for all expected terminal events ────────────────────────
	//
	// Expected runs:
	//   1. dupBead dispatched by alpha → run_completed
	//   2. betaB dispatched by beta   → run_completed
	//   3. alphaA dispatched by alpha after dupBead completes → run_completed
	//
	// dupBead in beta gets cross_queue_duplicate BEFORE dispatch (no run starts).
	// So 3 run_started + 3 terminal events total.
	//
	// Budget: AgentReadyTimeout(5s) × 3 runs + merge overhead + headroom = 60s.
	const terminalBudget = 60 * time.Second
	const wantTerminalCount = 3

	scenariotest.MustCompleteWithin(t, jsonlPath, "", nil, terminalBudget, func() {
		for {
			nCompleted := cmqEventCount(t, jsonlPath, string(core.EventTypeRunCompleted))
			nFailed := cmqEventCount(t, jsonlPath, string(core.EventTypeRunFailed))
			if nCompleted+nFailed >= wantTerminalCount {
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
	})

	// Cancel the daemon; all runs are terminal.
	loopCancel()
	scenariotest.MustCompleteWithin(t, jsonlPath, "", nil, 10*time.Second, func() {
		if err := <-startDone; err != nil {
			t.Errorf("cmq: daemon.Start returned error after cancel: %v", err)
		}
	})

	// ── Assertion (a): alphaA and betaB closed in br ──────────────────────────
	//
	// Also assert dupBead closed (won by alpha queue).
	if !cmqPollBeadClosed(t, brWrapper, dupBeadID, 2*time.Second) {
		t.Errorf("cmq (a): dupBead %s not closed within 2s after terminal events", dupBeadID)
	}
	if !cmqPollBeadClosed(t, brWrapper, alphaAID, 2*time.Second) {
		t.Errorf("cmq (a): alphaA %s not closed within 2s after terminal events", alphaAID)
	}
	if !cmqPollBeadClosed(t, brWrapper, betaBID, 2*time.Second) {
		t.Errorf("cmq (a): betaB %s not closed within 2s after terminal events", betaBID)
	}

	// ── Assertion (b): QM-062 two-level cap honored ───────────────────────────
	//
	// Max concurrent in-flight runs (tracked from run_started/run_completed
	// events in order) must not exceed MaxConcurrent=2.
	maxConcurrent := cmqMaxConcurrentRuns(t, jsonlPath)
	if maxConcurrent > cfg.MaxConcurrent {
		t.Errorf("cmq (b): QM-062 violated: max concurrent runs = %d, want ≤ %d",
			maxConcurrent, cfg.MaxConcurrent)
	}
	t.Logf("cmq (b): max concurrent runs = %d (cap = %d)", maxConcurrent, cfg.MaxConcurrent)

	// ── Assertion (c): cross-queue dedup (hk-a11re) ───────────────────────────
	//
	// Beta's queue file must contain a failed item for dupBead with
	// LastFailureReason containing "cross_queue_duplicate". The alpha queue's
	// dupBead item must be "completed" (it won the dispatch race).
	//
	// The beta queue file may have been renamed to *.paused-by-failure-<ts>
	// if evaluateGroupAdvanceWithOutcome ran; read it via queue.Load which
	// looks for the canonical *.json path only. If the file is absent (queue
	// fully completed), fall back to reading the item status from the alpha
	// side only and assert no run_started for beta's dupBead indirectly.
	//
	// Simpler: load both queue files directly from disk.
	alphaItems := cmqLoadQueueItems(t, projectDir, "alpha")
	betaItems := cmqLoadQueueItems(t, projectDir, "beta")
	t.Logf("cmq (c): alpha items = %+v", alphaItems)
	t.Logf("cmq (c): beta  items = %+v", betaItems)

	if betaItems != nil {
		// Find dupBead in beta's items.
		foundDupInBeta := false
		for _, item := range betaItems {
			if item.BeadID == dupBeadID {
				foundDupInBeta = true
				if item.Status != string(queue.ItemStatusFailed) {
					t.Errorf("cmq (c): beta dupBead item status = %q, want \"failed\" (cross_queue_duplicate guard)",
						item.Status)
				}
				if !strings.Contains(item.LastFailureReason, "cross_queue_duplicate") {
					t.Errorf("cmq (c): beta dupBead LastFailureReason = %q, want to contain \"cross_queue_duplicate\"",
						item.LastFailureReason)
				}
			}
		}
		if !foundDupInBeta {
			t.Errorf("cmq (c): dupBead %s not found in beta queue items %+v", dupBeadID, betaItems)
		}
	} else {
		// Beta queue completed and was unlinked — verify cross_queue_duplicate
		// fired before any run by checking run_started count (should be exactly 3,
		// not 4: dupBead in beta never starts).
		nStarted := cmqEventCount(t, jsonlPath, string(core.EventTypeRunStarted))
		if nStarted > wantTerminalCount {
			t.Errorf("cmq (c): %d run_started events; want ≤ %d (dupBead must not start in beta)",
				nStarted, wantTerminalCount)
		}
	}

	// ── Assertion (d): no implementer_escaped_worktree events ────────────────
	//
	// A sibling-merge exclusion (hk-77q8e) ensures that files changed by one
	// run's merge to main are excluded from the escape-detector check for
	// other in-flight runs. The twin makes no real commits, so main HEAD does
	// not advance; this assertion is a regression guard ensuring the detector
	// never fires for a clean concurrent-twin run.
	nEscape := cmqEventCount(t, jsonlPath, string(core.EventTypeImplementerEscapedWorktree))
	if nEscape > 0 {
		t.Errorf("cmq (d): implementer_escaped_worktree emitted %d time(s); want 0 (hk-77q8e sibling-exclusion fix)", nEscape)
	}

	// ── Causality invariants (hk-xegej) ──────────────────────────────────────
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

	t.Logf("cmq HappyPath PASS: dupBead=%s (alpha) alphaA=%s betaB=%s maxConcurrent=%d noEscape=%v",
		dupBeadID, alphaAID, betaBID, maxConcurrent, nEscape == 0)
}

// ─────────────────────────────────────────────────────────────────────────────
// TestScenario_ConcurrentMultiQueue_N2_MidRunKill
// ─────────────────────────────────────────────────────────────────────────────

// TestScenario_ConcurrentMultiQueue_N2_MidRunKill exercises the G1 cause-side:
// it cancels the daemon while beads are in-flight via a blocking twin wrapper
// (sleep 3600) and verifies the stuck-queue state that the restart-recovery
// test (hk-ivzsl) handles from the recovery side.
//
// Setup:
//  1. TempDir project with git + br DB.
//  2. Two beads: alphaA and betaB.
//  3. Queue "alpha" (Workers=1): [alphaA].
//  4. Queue "beta"  (Workers=1): [betaB].
//  5. daemon.Start wired with MaxConcurrent=2 and a blocking twin wrapper.
//
// Phase 1: wait for at least one run_started (confirming dispatch occurred).
// Phase 2: cancel daemon context immediately — runs are still in-flight.
// Phase 3: wait for daemon to exit cleanly.
//
// Assertions:
//  - At least one run_started event is present (bead was dispatched).
//  - run_completed is absent (no run finished before the kill).
//  - Dispatched bead(s) are NOT closed in br (still open or in_progress).
//
// Not parallel: uses os.Setenv(HARMONIK_CLAUDE_CONFIG_PATH).
//
// Bead: hk-umemp.
func TestScenario_ConcurrentMultiQueue_N2_MidRunKill(t *testing.T) {
	// Locate br binary.
	realBrPath := cmqBrPath(t)

	// Create project directory with git repo and br DB.
	projectDir, jsonlPath := cmqProjectDir(t)
	cmqGitRepo(t, projectDir)

	// Initialise br DB and create two beads (no dup bead needed for this sub-test).
	dbPath := filepath.Join(projectDir, ".beads", "beads.db")
	brWrapper := cmqBrWrapperScript(t, realBrPath, dbPath)

	//nolint:gosec // G204: br args are test-internal literals; not user input
	initCmd := exec.CommandContext(t.Context(), realBrPath, "init", "--prefix", "mkl")
	initCmd.Dir = projectDir
	initOut, initErr := initCmd.CombinedOutput()
	require.NoError(t, initErr, "cmq MidRunKill: br init: %s", initOut)

	createBead := func(title string) string {
		t.Helper()
		//nolint:gosec // G204: br args are test-internal literals; not user input
		cmd := exec.CommandContext(t.Context(), brWrapper, "create", title, "--status", "open", "--silent")
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "cmq MidRunKill: br create %q: %s", title, out)
		id := strings.TrimSpace(string(out))
		require.NotEmpty(t, id, "cmq MidRunKill: br create returned empty ID for %q", title)
		return id
	}
	alphaAID := createBead("mkl alpha-A bead")
	betaBID := createBead("mkl beta-B bead")
	t.Logf("cmq MidRunKill: alphaA=%s betaB=%s", alphaAID, betaBID)

	// Pre-seed one bead per queue.
	ctx := t.Context()
	qAlpha := cmqBuildActiveWaveQueue("alpha", "00000000-0000-7b00-8000-cc3000000003", core.BeadID(alphaAID))
	qBeta := cmqBuildActiveWaveQueue("beta", "00000000-0000-7b00-8000-dd4000000004", core.BeadID(betaBID))
	require.NoError(t, queue.Persist(ctx, projectDir, qAlpha), "cmq MidRunKill: persist alpha queue")
	require.NoError(t, queue.Persist(ctx, projectDir, qBeta), "cmq MidRunKill: persist beta queue")

	// Use a blocking twin wrapper so runs are guaranteed in-flight when we cancel.
	blockingWrapper := cmqBlockingTwinWrapperScript(t)

	// Redirect EnsureWorktreeTrust to a test-local config.
	claudeConfigPath := filepath.Join(t.TempDir(), ".claude.json")
	if err := os.Setenv("HARMONIK_CLAUDE_CONFIG_PATH", claudeConfigPath); err != nil {
		t.Fatalf("cmq MidRunKill: Setenv HARMONIK_CLAUDE_CONFIG_PATH: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Unsetenv("HARMONIK_CLAUDE_CONFIG_PATH"); err != nil {
			t.Logf("cmq MidRunKill: Unsetenv HARMONIK_CLAUDE_CONFIG_PATH: %v", err)
		}
	})

	loopCtx, loopCancel := context.WithCancel(context.Background())
	defer loopCancel()

	cfg := daemon.Config{
		ProjectDir:            projectDir,
		JSONLLogPath:          jsonlPath,
		BrPath:                brWrapper,
		HandlerBinary:         blockingWrapper,
		NoAutoPull:            true,
		MaxConcurrent:         2,
		SkipWALCheckpoint:     true,
		SkipBrHistoryRotation: true,
		SkipRestartBackoff:    true,
		AgentReadyTimeout:     10 * time.Second,
		LogWriter:             testLogWriter{t: t},
		WorkflowModeDefault:   core.WorkflowModeReviewLoop,
	}

	// Launch daemon.Start in a goroutine.
	startDone := make(chan error, 1)
	go func() {
		startDone <- daemon.Start(loopCtx, cfg)
	}()

	// ── Phase 1: wait for at least one run_started ────────────────────────────
	//
	// The blocking twin never emits agent_ready, so the daemon sits in
	// waitAgentReady (up to AgentReadyTimeout). We only need run_started —
	// that fires as soon as the subprocess is launched. Budget: 30 s to
	// allow for worktree creation + process spawn on a busy CI machine.
	const dispatchBudget = 30 * time.Second
	nStarted := cmqPollRunStartedCount(t, jsonlPath, 1, dispatchBudget)
	if nStarted == 0 {
		t.Fatalf("cmq MidRunKill: no run_started event within %s — daemon did not dispatch any bead", dispatchBudget)
	}
	t.Logf("cmq MidRunKill: observed %d run_started event(s) — cancelling daemon mid-run", nStarted)

	// ── Phase 2: cancel daemon while runs are in-flight ───────────────────────
	loopCancel()

	// ── Phase 3: wait for daemon to exit ─────────────────────────────────────
	//
	// Budget: 15 s — the daemon must kill the blocking subprocess and drain
	// in-flight goroutines. On exit it calls drainCancelledQueue.
	scenariotest.MustCompleteWithin(t, jsonlPath, "", nil, 15*time.Second, func() {
		if err := <-startDone; err != nil {
			t.Errorf("cmq MidRunKill: daemon.Start returned error after cancel: %v", err)
		}
	})

	// ── Assertion: run_started present, run_completed absent ─────────────────
	nStartedFinal := cmqEventCount(t, jsonlPath, string(core.EventTypeRunStarted))
	nCompleted := cmqEventCount(t, jsonlPath, string(core.EventTypeRunCompleted))
	if nStartedFinal == 0 {
		t.Error("cmq MidRunKill: run_started absent — bead was never dispatched")
	}
	if nCompleted > 0 {
		t.Errorf("cmq MidRunKill: run_completed present (%d); want 0 (blocking twin never exits 0)", nCompleted)
	}
	t.Logf("cmq MidRunKill: run_started=%d run_completed=%d (expected: >=1 / 0)", nStartedFinal, nCompleted)

	// ── Assertion: dispatched beads NOT closed in br ──────────────────────────
	//
	// The blocking twin never exits 0, so CloseBead is never called.
	// Both beads should remain open (or in_progress if ClaimBead was called).
	checkNotClosed := func(beadID, label string) {
		t.Helper()
		cmd := exec.CommandContext(t.Context(), brWrapper, "show", beadID, "--format", "json")
		out, err := cmd.Output()
		if err != nil {
			t.Logf("cmq MidRunKill: br show %s failed: %v (bead may not be found)", beadID, err)
			return
		}
		var items []struct {
			Status string `json:"status"`
		}
		if json.Unmarshal(out, &items) != nil || len(items) == 0 {
			t.Logf("cmq MidRunKill: br show %s returned no items", beadID)
			return
		}
		if items[0].Status == "closed" {
			t.Errorf("cmq MidRunKill: %s bead %s is closed after mid-run kill; want open or in_progress", label, beadID)
		} else {
			t.Logf("cmq MidRunKill: %s bead %s status = %q (not closed — correct)", label, beadID, items[0].Status)
		}
	}
	checkNotClosed(alphaAID, "alphaA")
	checkNotClosed(betaBID, "betaB")

	t.Logf("cmq MidRunKill PASS: dispatched=%d completed=0 beads-not-closed=true", nStartedFinal)
}
