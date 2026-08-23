//go:build scenario

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

	"github.com/stretchr/testify/require"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/daemon/scenariotest"
	"github.com/gregberns/harmonik/internal/mergeq"
	"github.com/gregberns/harmonik/internal/queue"
)

func cmqEvalSymlinks(t *testing.T, path string) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(path)
	require.NoError(t, err, "cmqEvalSymlinks: EvalSymlinks %q", path)
	return resolved
}

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

	raw := t.TempDir()
	originDir, err := filepath.EvalSymlinks(raw)
	require.NoError(t, err, "cmqGitRepo: EvalSymlinks originDir")
	initBareCmd := exec.CommandContext(t.Context(), "git", "init", "--bare", "--initial-branch=main", originDir)
	out, err := initBareCmd.CombinedOutput()
	require.NoError(t, err, "cmqGitRepo: git init --bare\n%s", out)
	run("remote", "add", "origin", originDir)
	run("push", "origin", "main")
}

func cmqBrPath(t *testing.T) string {
	t.Helper()
	brPath, err := exec.LookPath("br")
	if err != nil {
		t.Skip("cmq: br required for scenario test (not on PATH)")
	}
	return brPath
}

func cmqBrWrapperScript(t *testing.T, realBrPath, dbPath string) string {
	t.Helper()
	dir := cmqEvalSymlinks(t, t.TempDir())
	path := filepath.Join(dir, "br")
	content := "#!/bin/sh\nexec " + realBrPath + " --db " + dbPath + " \"$@\"\n"
	//nolint:gosec // G306: script is test-only; chmod 0755 required for execution
	require.NoError(t, os.WriteFile(path, []byte(content), 0o755), "cmqBrWrapperScript: WriteFile")
	return path
}

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
exec "` + twinPath + `" --scenario commit-on-cue-startup-delay --worktree-path "$PWD"
`
	//nolint:gosec // G306: script is test-only; chmod 0755 required for execution
	require.NoError(t, os.WriteFile(path, []byte(content), 0o755), "cmqTwinWrapperScript: WriteFile")
	return path
}

func cmqBlockingTwinWrapperScript(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "twin-cmq-blocking.sh")
	content := "#!/bin/sh\nsleep 3600\n"
	//nolint:gosec // G306: script is test-only; chmod 0755 required for execution
	require.NoError(t, os.WriteFile(path, []byte(content), 0o755), "cmqBlockingTwinWrapperScript: WriteFile")
	return path
}

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

type cmqItemState struct {
	BeadID            string
	Status            string
	LastFailureReason string
}

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

func cmqLoadQueueStatus(t *testing.T, projectDir, queueName string) string {
	t.Helper()
	queuePath := filepath.Join(projectDir, ".harmonik", "queues", queueName+".json")
	//nolint:gosec // G304: path is t.TempDir()-based; not user input
	data, err := os.ReadFile(queuePath)
	if os.IsNotExist(err) {
		return ""
	}
	require.NoError(t, err, "cmqLoadQueueStatus: read %s", queuePath)
	var q struct {
		Status string `json:"status"`
	}
	require.NoError(t, json.Unmarshal(data, &q), "cmqLoadQueueStatus: unmarshal %s", queuePath)
	return q.Status
}

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
//
//	tick 1 — alpha dispatches dupBead (1 alpha in-flight, 1 global)
//	tick 2 — beta tries dupBead → the collision is detected and REFUSED;
//	          beta then dispatches betaB (1 beta in-flight, 2 global)
//	dupBead + betaB run concurrently (global at cap = 2)
//	dupBead completes → alpha slot freed; alpha dispatches alphaA
//	betaB completes → all done
//
// Assertions:
//
//	(a) alphaA and betaB are closed in br; dupBead is closed by alpha.
//	(b) Max concurrent runs observed ≤ MaxConcurrent=2 (QM-062).
//	(c) Beta's dupBead item is NOT failed and beta's queue is NOT parked
//	    (hk-a11re detection, hk-nsion disposition).
//
// Not parallel: uses os.Setenv(HARMONIK_CLAUDE_CONFIG_PATH) to isolate
// EnsureWorktreeTrust — same rationale as TestScenario_HappyPath_N1.
//
// Bead: hk-umemp.
func TestScenario_ConcurrentMultiQueue_N2_HappyPath(t *testing.T) {
	skipRealDaemonE2EInShort(t)
	twinPath, ok := scenariotest.TwinBinaryPath()
	if !ok {
		t.Skip("cmq: harmonik-twin-claude binary not found; set HARMONIK_TWIN_CLAUDE or build the binary")
	}

	realBrPath := cmqBrPath(t)

	projectDir, jsonlPath := cmqProjectDir(t)
	cmqGitRepo(t, projectDir)

	dbPath := filepath.Join(projectDir, ".beads", "beads.db")
	brWrapper := cmqBrWrapperScript(t, realBrPath, dbPath)
	dupBeadID, alphaAID, betaBID := cmqInitBrWithBeads(t, realBrPath, projectDir, brWrapper)
	t.Logf("cmq: dupBead=%s alphaA=%s betaB=%s", dupBeadID, alphaAID, betaBID)

	ctx := t.Context()
	qAlpha := cmqBuildActiveWaveQueue("alpha", "00000000-0000-7a00-8000-aa1000000001",
		core.BeadID(dupBeadID), core.BeadID(alphaAID))
	qBeta := cmqBuildActiveWaveQueue("beta", "00000000-0000-7a00-8000-bb2000000002",
		core.BeadID(dupBeadID), core.BeadID(betaBID))
	require.NoError(t, queue.Persist(ctx, projectDir, qAlpha), "cmq: persist alpha queue")
	require.NoError(t, queue.Persist(ctx, projectDir, qBeta), "cmq: persist beta queue")

	twinWrapper := cmqTwinWrapperScript(t, twinPath)

	claudeConfigPath := filepath.Join(t.TempDir(), ".claude.json")
	prevClaudeCfg, hadClaudeCfg := os.LookupEnv("HARMONIK_CLAUDE_CONFIG_PATH")
	if err := os.Setenv("HARMONIK_CLAUDE_CONFIG_PATH", claudeConfigPath); err != nil {
		t.Fatalf("cmq: Setenv HARMONIK_CLAUDE_CONFIG_PATH: %v", err)
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

	scenariotest.WriteReviewLoopWorkflowDot(t, projectDir)

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
		WorkflowModeDefault:   core.WorkflowModeDot,
	}

	mergeQ := mergeq.New(nil)
	mergeQCtx, mergeQCancel := context.WithCancel(context.Background())
	mergeQ.Start(mergeQCtx)
	t.Cleanup(mergeQCancel)
	startDone := make(chan error, 1)
	go func() {
		startDone <- daemon.StartForTesting(loopCtx, cfg,
			daemon.WithWorktreeFactory(emptyCommitWorktreeFactory),
			daemon.WithMergeQueue(mergeQ),
		)
	}()

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

	loopCancel()
	scenariotest.MustCompleteWithin(t, jsonlPath, "", nil, 10*time.Second, func() {
		if err := <-startDone; err != nil {
			t.Errorf("cmq: daemon.Start returned error after cancel: %v", err)
		}
	})

	if !cmqPollBeadClosed(t, brWrapper, dupBeadID, 2*time.Second) {
		t.Errorf("cmq (a): dupBead %s not closed within 2s after terminal events", dupBeadID)
	}
	if !cmqPollBeadClosed(t, brWrapper, alphaAID, 2*time.Second) {
		t.Errorf("cmq (a): alphaA %s not closed within 2s after terminal events", alphaAID)
	}
	if !cmqPollBeadClosed(t, brWrapper, betaBID, 2*time.Second) {
		t.Errorf("cmq (a): betaB %s not closed within 2s after terminal events", betaBID)
	}

	maxConcurrent := cmqMaxConcurrentRuns(t, jsonlPath)
	if maxConcurrent > cfg.MaxConcurrent {
		t.Errorf("cmq (b): QM-062 violated: max concurrent runs = %d, want ≤ %d",
			maxConcurrent, cfg.MaxConcurrent)
	}
	t.Logf("cmq (b): max concurrent runs = %d (cap = %d)", maxConcurrent, cfg.MaxConcurrent)

	alphaItems := cmqLoadQueueItems(t, projectDir, "alpha")
	betaItems := cmqLoadQueueItems(t, projectDir, "beta")
	t.Logf("cmq (c): alpha items = %+v", alphaItems)
	t.Logf("cmq (c): beta  items = %+v", betaItems)

	nStarted := cmqEventCount(t, jsonlPath, string(core.EventTypeRunStarted))
	if nStarted > wantTerminalCount {
		t.Errorf("cmq (c): %d run_started events; want ≤ %d (dupBead must not start in beta)",
			nStarted, wantTerminalCount)
	}

	if betaItems == nil {
		t.Log("cmq (c): beta queue file absent (queue completed and was unlinked); disposition assertions skipped")
	} else {
		if status := cmqLoadQueueStatus(t, projectDir, "beta"); status == string(queue.QueueStatusPausedByFailure) {
			t.Errorf("cmq (c): beta queue status = %q; losing another queue's race must not park the loser's "+
				"whole queue (§9.8 QM-067, hk-nsion)", status)
		}
		foundDupInBeta := false
		for _, item := range betaItems {
			if item.BeadID != dupBeadID {
				continue
			}
			foundDupInBeta = true
			if item.Status == string(queue.ItemStatusFailed) {
				t.Errorf("cmq (c): beta dupBead item status = %q reason = %q; a refusal is a property of the "+
					"tick and must not be made durable (§9.8 QM-067, hk-nsion)",
					item.Status, item.LastFailureReason)
			}
			if strings.Contains(item.LastFailureReason, "cross_queue_duplicate") {
				t.Errorf("cmq (c): beta dupBead LastFailureReason = %q; the terminal duplicate failure is now "+
					"the tail case past a bound, not the first response to a collision", item.LastFailureReason)
			}
		}
		if !foundDupInBeta {
			t.Errorf("cmq (c): dupBead %s not found in beta queue items %+v", dupBeadID, betaItems)
		}
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

	t.Logf("cmq HappyPath PASS: dupBead=%s (alpha) alphaA=%s betaB=%s maxConcurrent=%d",
		dupBeadID, alphaAID, betaBID, maxConcurrent)
}

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
//   - At least one run_started event is present (bead was dispatched).
//   - run_completed is absent (no run finished before the kill).
//   - Dispatched bead(s) are NOT closed in br (still open or in_progress).
//
// Not parallel: uses os.Setenv(HARMONIK_CLAUDE_CONFIG_PATH).
//
// Bead: hk-umemp.
func TestScenario_ConcurrentMultiQueue_N2_MidRunKill(t *testing.T) {
	skipRealDaemonE2EInShort(t)
	realBrPath := cmqBrPath(t)

	projectDir, jsonlPath := cmqProjectDir(t)
	cmqGitRepo(t, projectDir)

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

	ctx := t.Context()
	qAlpha := cmqBuildActiveWaveQueue("alpha", "00000000-0000-7b00-8000-cc3000000003", core.BeadID(alphaAID))
	qBeta := cmqBuildActiveWaveQueue("beta", "00000000-0000-7b00-8000-dd4000000004", core.BeadID(betaBID))
	require.NoError(t, queue.Persist(ctx, projectDir, qAlpha), "cmq MidRunKill: persist alpha queue")
	require.NoError(t, queue.Persist(ctx, projectDir, qBeta), "cmq MidRunKill: persist beta queue")

	blockingWrapper := cmqBlockingTwinWrapperScript(t)

	claudeConfigPath := filepath.Join(t.TempDir(), ".claude.json")
	prevClaudeCfg, hadClaudeCfg := os.LookupEnv("HARMONIK_CLAUDE_CONFIG_PATH")
	if err := os.Setenv("HARMONIK_CLAUDE_CONFIG_PATH", claudeConfigPath); err != nil {
		t.Fatalf("cmq MidRunKill: Setenv HARMONIK_CLAUDE_CONFIG_PATH: %v", err)
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

	scenariotest.WriteReviewLoopWorkflowDot(t, projectDir)

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
		WorkflowModeDefault:   core.WorkflowModeDot,
	}

	startDone := make(chan error, 1)
	go func() {
		startDone <- daemon.Start(loopCtx, cfg)
	}()

	const dispatchBudget = 30 * time.Second
	nStarted := cmqPollRunStartedCount(t, jsonlPath, 1, dispatchBudget)
	if nStarted == 0 {
		t.Fatalf("cmq MidRunKill: no run_started event within %s — daemon did not dispatch any bead", dispatchBudget)
	}
	t.Logf("cmq MidRunKill: observed %d run_started event(s) — cancelling daemon mid-run", nStarted)

	loopCancel()

	scenariotest.MustCompleteWithin(t, jsonlPath, "", nil, 15*time.Second, func() {
		if err := <-startDone; err != nil {
			t.Errorf("cmq MidRunKill: daemon.Start returned error after cancel: %v", err)
		}
	})

	nStartedFinal := cmqEventCount(t, jsonlPath, string(core.EventTypeRunStarted))
	nCompleted := cmqEventCount(t, jsonlPath, string(core.EventTypeRunCompleted))
	if nStartedFinal == 0 {
		t.Error("cmq MidRunKill: run_started absent — bead was never dispatched")
	}
	if nCompleted > 0 {
		t.Errorf("cmq MidRunKill: run_completed present (%d); want 0 (blocking twin never exits 0)", nCompleted)
	}
	t.Logf("cmq MidRunKill: run_started=%d run_completed=%d (expected: >=1 / 0)", nStartedFinal, nCompleted)

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
