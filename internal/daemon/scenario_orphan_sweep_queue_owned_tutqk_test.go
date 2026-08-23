package daemon_test

import (
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

func sweepQOEvalSymlinks(t *testing.T, path string) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatalf("sweepQOEvalSymlinks: EvalSymlinks %q: %v", path, err)
	}
	return resolved
}

func sweepQOProjectDir(t *testing.T) (projectDir, jsonlPath string) {
	t.Helper()
	projectDir = sweepQOEvalSymlinks(t, t.TempDir())
	for _, sub := range []string{
		filepath.Join(".harmonik", "events"),
		filepath.Join(".harmonik", "beads-intents"),
	} {
		//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
		if err := os.MkdirAll(filepath.Join(projectDir, sub), 0o755); err != nil {
			t.Fatalf("sweepQOProjectDir: mkdir %s: %v", sub, err)
		}
	}
	jsonlPath = filepath.Join(projectDir, ".harmonik", "events", "events.jsonl")
	return projectDir, jsonlPath
}

func sweepQOBrPath(t *testing.T) string {
	t.Helper()
	brPath, err := exec.LookPath("br")
	if err != nil {
		t.Skip("br required for scenario test (not on PATH)")
	}
	return brPath
}

func sweepQOBrWrapperScript(t *testing.T, realBrPath, dbPath string) string {
	t.Helper()
	dir := sweepQOEvalSymlinks(t, t.TempDir())
	path := filepath.Join(dir, "br")
	content := "#!/bin/sh\nexec " + realBrPath + " --db " + dbPath + " \"$@\"\n"
	//nolint:gosec // G306: script is test-only; chmod 0755 required for execution
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatalf("sweepQOBrWrapperScript: WriteFile: %v", err)
	}
	return path
}

func sweepQOInitBrWithInProgress(t *testing.T, realBrPath, projectDir, brWrapper string) string {
	t.Helper()

	initCmd := exec.CommandContext(t.Context(), realBrPath, "init", "--prefix", "sqo")
	initCmd.Dir = projectDir
	initOut, initErr := initCmd.CombinedOutput()
	if initErr != nil {
		t.Fatalf("sweepQOInitBrWithInProgress: br init: %v\n%s", initErr, initOut)
	}

	createCmd := exec.CommandContext(t.Context(), brWrapper, "create",
		"orphan sweep queue-owned test bead", "--status", "open", "--silent")
	createOut, createErr := createCmd.CombinedOutput()
	if createErr != nil {
		t.Fatalf("sweepQOInitBrWithInProgress: br create: %v\n%s", createErr, createOut)
	}
	beadID := strings.TrimSpace(string(createOut))
	if beadID == "" {
		t.Fatal("sweepQOInitBrWithInProgress: br create returned empty ID")
	}

	// br update --status in_progress — simulate a crash-left-behind bead.
	//nolint:gosec // G204: br args are test-internal literals; not user input
	updateCmd := exec.CommandContext(t.Context(), brWrapper, "update", beadID,
		"--status", "in_progress")
	updateOut, updateErr := updateCmd.CombinedOutput()
	if updateErr != nil {
		t.Fatalf("sweepQOInitBrWithInProgress: br update in_progress: %v\n%s", updateErr, updateOut)
	}

	return beadID
}

func sweepQOWriteQueueJSON(t *testing.T, projectDir, beadID string) {
	t.Helper()

	harmonikDir := filepath.Join(projectDir, ".harmonik", "queues")
	//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
	if err := os.MkdirAll(harmonikDir, 0o755); err != nil {
		t.Fatalf("sweepQOWriteQueueJSON: MkdirAll .harmonik/queues: %v", err)
	}

	queue := map[string]interface{}{
		"schema_version": 1,
		"queue_id":       "00000000-0000-7000-8000-000000000001",
		"submitted_at":   time.Now().UTC().Format(time.RFC3339),
		"status":         "active",
		"groups": []map[string]interface{}{
			{
				"group_index": 0,
				"kind":        "wave",
				"status":      "active",
				"created_at":  time.Now().UTC().Format(time.RFC3339),
				"items": []map[string]interface{}{
					{
						"bead_id": beadID,
						"status":  "pending",
					},
				},
			},
		},
	}

	data, err := json.Marshal(queue)
	if err != nil {
		t.Fatalf("sweepQOWriteQueueJSON: marshal: %v", err)
	}

	queuePath := filepath.Join(harmonikDir, "main.json")
	if err := os.WriteFile(queuePath, data, 0o600); err != nil {
		t.Fatalf("sweepQOWriteQueueJSON: WriteFile: %v", err)
	}
}

func sweepQOPollOrphanSweepCompleted(t *testing.T, jsonlPath string, budget time.Duration) bool {
	t.Helper()
	return scenariotest.WaitForEvent(t, jsonlPath,
		string(core.EventTypeDaemonOrphanSweepCompleted), "", budget)
}

// TestScenario_OrphanSweep_QueueOwnedBeadReset is the integration test for the
// daemon.Start + queue.json + orphan-sweep queue-owned bead-reset path.
//
// Setup:
//  1. TempDir project with br DB seeded with one bead in `in_progress` status.
//  2. queue.json written with that bead_id at status=pending (queue-owned, not
//     dispatched).
//  3. daemon.Start wired with BrPath but no HandlerBinary needed for the sweep.
//
// Assertions:
//  1. daemon_orphan_sweep_completed event appears in the JSONL log.
//  2. Bead status == "open" in the br DB (reset by queue-owned provenance path).
//
// Bead: hk-tutqk.
func TestScenario_OrphanSweep_QueueOwnedBeadReset(t *testing.T) {
	skipRealDaemonE2EInShort(t)

	realBrPath := sweepQOBrPath(t)

	projectDir, jsonlPath := sweepQOProjectDir(t)

	dbPath := filepath.Join(projectDir, ".beads", "beads.db")
	brWrapper := sweepQOBrWrapperScript(t, realBrPath, dbPath)
	beadID := sweepQOInitBrWithInProgress(t, realBrPath, projectDir, brWrapper)
	t.Logf("sweepQO: seeded bead ID = %s (in_progress)", beadID)

	scenariotest.AssertBeadStatus(t, brWrapper, beadID, "in_progress")

	sweepQOWriteQueueJSON(t, projectDir, beadID)

	claudeConfigPath := filepath.Join(sweepQOEvalSymlinks(t, t.TempDir()), ".claude.json")
	prevClaudeCfg, hadClaudeCfg := os.LookupEnv("HARMONIK_CLAUDE_CONFIG_PATH")
	if err := os.Setenv("HARMONIK_CLAUDE_CONFIG_PATH", claudeConfigPath); err != nil {
		t.Fatalf("sweepQO: Setenv HARMONIK_CLAUDE_CONFIG_PATH: %v", err)
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
		SkipWALCheckpoint:     true,
		SkipBrHistoryRotation: true,
		// No HandlerBinary: work loop will not be able to dispatch. We cancel
		// before any dispatch attempt anyway.
		// AgentReadyTimeout left at zero (= default 30 s); irrelevant since we
		// cancel before any dispatch.
		LogWriter: testLogWriter{t: t},
		// WorkflowModeDefault is required by daemon.Start since hk-81n9r
		// (9835491b). This sweep-only test cancels before any dispatch, so the
		// value is never exercised by a workloop run; the standard default is
		// dot (hk-30vlb) but any valid mode is immaterial here. Without it,
		// daemon.Start returns the "WorkflowModeDefault must be set (PL-004a)"
		// error (hk-4f5ua).
		WorkflowModeDefault: core.WorkflowModeDot,
	}

	startDone := make(chan error, 1)
	go func() {
		startDone <- daemon.Start(loopCtx, cfg)
	}()

	const sweepPollBudget = 10 * time.Second
	scenariotest.MustCompleteWithin(t, jsonlPath, "", nil, sweepPollBudget, func() {
		for {
			if scenariotest.WaitForEvent(t, jsonlPath, "daemon_orphan_sweep_completed", "", 50*time.Millisecond) {
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

	scenariotest.AssertBeadStatus(t, brWrapper, beadID, "open")

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

	t.Logf("sweepQO: PASS bead=%s reset to open by queue-owned provenance path", beadID)
}
