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

	"github.com/gregberns/harmonik/internal/brcli"
	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/daemon/scenariotest"
	"github.com/gregberns/harmonik/internal/queue"
	"github.com/gregberns/harmonik/internal/queuewiring"
)

func rrRecovEvalSymlinks(t *testing.T, path string) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatalf("rrRecovEvalSymlinks: EvalSymlinks %q: %v", path, err)
	}
	return resolved
}

func rrRecovProjectDir(t *testing.T) (projectDir, jsonlPath string) {
	t.Helper()
	projectDir = rrRecovEvalSymlinks(t, t.TempDir())
	for _, sub := range []string{
		filepath.Join(".harmonik", "events"),
		filepath.Join(".harmonik", "beads-intents"),
		filepath.Join(".harmonik", "queues"),
	} {
		//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
		if err := os.MkdirAll(filepath.Join(projectDir, sub), 0o755); err != nil {
			t.Fatalf("rrRecovProjectDir: mkdir %s: %v", sub, err)
		}
	}
	jsonlPath = filepath.Join(projectDir, ".harmonik", "events", "events.jsonl")
	return projectDir, jsonlPath
}

func rrRecovBrPath(t *testing.T) string {
	t.Helper()
	brPath, err := exec.LookPath("br")
	if err != nil {
		t.Skip("br required for scenario test (not on PATH)")
	}
	return brPath
}

func rrRecovBrWrapperScript(t *testing.T, realBrPath, dbPath string) string {
	t.Helper()
	dir := rrRecovEvalSymlinks(t, t.TempDir())
	path := filepath.Join(dir, "br")
	content := "#!/bin/sh\nexec " + realBrPath + " --db " + dbPath + " \"$@\"\n"
	//nolint:gosec // G306: script is test-only; chmod 0755 required for execution
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatalf("rrRecovBrWrapperScript: WriteFile: %v", err)
	}
	return path
}

func rrRecovInitBrWithBeads(t *testing.T, realBrPath, projectDir, brWrapper string) (beadAID, beadBID string) {
	t.Helper()

	//nolint:gosec // G204: br args are test-internal literals; not user input
	initCmd := exec.CommandContext(t.Context(), realBrPath, "init", "--prefix", "rrr")
	initCmd.Dir = projectDir
	initOut, initErr := initCmd.CombinedOutput()
	if initErr != nil {
		t.Fatalf("rrRecovInitBrWithBeads: br init: %v\n%s", initErr, initOut)
	}

	// Create bead A — simulates the bead that was dispatched and landed elsewhere.
	//nolint:gosec // G204: br args are test-internal literals; not user input
	createA := exec.CommandContext(t.Context(), brWrapper, "create",
		"restart-recovery test bead A (dispatched+closed)", "--status", "open", "--silent")
	outA, errA := createA.CombinedOutput()
	if errA != nil {
		t.Fatalf("rrRecovInitBrWithBeads: br create A: %v\n%s", errA, outA)
	}
	beadAID = strings.TrimSpace(string(outA))
	if beadAID == "" {
		t.Fatal("rrRecovInitBrWithBeads: br create A returned empty ID")
	}

	// Close bead A — models "landed via another queue or direct br close".
	//nolint:gosec // G204: br args are test-internal literals; not user input
	closeA := exec.CommandContext(t.Context(), brWrapper, "close", beadAID, "--reason", "landed-via-other-path")
	closeAOut, closeAErr := closeA.CombinedOutput()
	if closeAErr != nil {
		t.Fatalf("rrRecovInitBrWithBeads: br close A: %v\n%s", closeAErr, closeAOut)
	}

	// Create bead B — will be represented as failed in the queue (cross_queue_duplicate).
	//nolint:gosec // G204: br args are test-internal literals; not user input
	createB := exec.CommandContext(t.Context(), brWrapper, "create",
		"restart-recovery test bead B (failed sibling)", "--status", "open", "--silent")
	outB, errB := createB.CombinedOutput()
	if errB != nil {
		t.Fatalf("rrRecovInitBrWithBeads: br create B: %v\n%s", errB, outB)
	}
	beadBID = strings.TrimSpace(string(outB))
	if beadBID == "" {
		t.Fatal("rrRecovInitBrWithBeads: br create B returned empty ID")
	}

	return beadAID, beadBID
}

func rrRecovWriteStuckQueueJSON(t *testing.T, projectDir, beadAID, beadBID string) {
	t.Helper()

	queuesDir := filepath.Join(projectDir, ".harmonik", "queues")
	//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
	if err := os.MkdirAll(queuesDir, 0o755); err != nil {
		t.Fatalf("rrRecovWriteStuckQueueJSON: MkdirAll queues: %v", err)
	}

	runID := "rrr-run-0001-aaaa-bbbb-cccc-000000000001"
	now := time.Now().UTC().Format(time.RFC3339)
	q := map[string]interface{}{
		"schema_version": 1,
		"queue_id":       "00000000-0000-7000-8000-aaaa000000001",
		"submitted_at":   now,
		"status":         "active",
		"groups": []map[string]interface{}{
			{
				"group_index": 0,
				"kind":        "wave",
				"status":      "active",
				"created_at":  now,
				"started_at":  now,
				"items": []map[string]interface{}{
					{
						"bead_id": beadAID,
						"status":  "dispatched",
						"run_id":  runID,
					},
					{
						"bead_id":             beadBID,
						"status":              "failed",
						"last_failure_reason": "cross_queue_duplicate",
					},
				},
			},
		},
	}

	data, err := json.Marshal(q)
	if err != nil {
		t.Fatalf("rrRecovWriteStuckQueueJSON: marshal: %v", err)
	}

	queuePath := filepath.Join(queuesDir, "main.json")
	if err := os.WriteFile(queuePath, data, 0o600); err != nil {
		t.Fatalf("rrRecovWriteStuckQueueJSON: WriteFile: %v", err)
	}
}

func rrRecovCreateOpenBead(t *testing.T, brWrapper, title string) string {
	t.Helper()
	//nolint:gosec // G204: br args are test-internal literals; not user input
	cmd := exec.CommandContext(t.Context(), brWrapper, "create", title, "--status", "open", "--silent")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("rrRecovCreateOpenBead: br create: %v\n%s", err, out)
	}
	beadID := strings.TrimSpace(string(out))
	if beadID == "" {
		t.Fatal("rrRecovCreateOpenBead: br create returned empty ID")
	}
	return beadID
}

func rrRecovForceQueueStatus(t *testing.T, projectDir, status string) {
	t.Helper()

	queuePath := filepath.Join(projectDir, ".harmonik", "queues", "main.json")
	data, err := os.ReadFile(queuePath) //nolint:gosec // G304: path is t.TempDir()-based; not user input
	if err != nil {
		t.Fatalf("rrRecovForceQueueStatus: ReadFile: %v", err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("rrRecovForceQueueStatus: unmarshal: %v", err)
	}
	encoded, err := json.Marshal(status)
	if err != nil {
		t.Fatalf("rrRecovForceQueueStatus: marshal status: %v", err)
	}
	raw["status"] = encoded
	out, err := json.Marshal(raw)
	if err != nil {
		t.Fatalf("rrRecovForceQueueStatus: marshal queue: %v", err)
	}
	if err := os.WriteFile(queuePath, out, 0o600); err != nil {
		t.Fatalf("rrRecovForceQueueStatus: WriteFile: %v", err)
	}
}

type rrRecovMismatchPayload struct {
	QueueID       string `json:"queue_id"`
	GroupIndex    int    `json:"group_index"`
	BeadID        string `json:"bead_id"`
	MismatchClass string `json:"mismatch_class"`
	LedgerStatus  string `json:"ledger_status"`
	QueueStatus   string `json:"queue_status"`
	ObservedAt    string `json:"observed_at"`
}

func rrRecovExtractMismatchPayload(t *testing.T, jsonlPath, wantClass string) rrRecovMismatchPayload {
	t.Helper()

	//nolint:gosec // G304: path is t.TempDir()-based; not user input
	f, err := os.Open(jsonlPath)
	if err != nil {
		t.Fatalf("rrRecovExtractMismatchPayload: open %s: %v", jsonlPath, err)
	}
	defer func() {
		if closeErr := f.Close(); closeErr != nil {
			t.Logf("rrRecovExtractMismatchPayload: close: %v", closeErr)
		}
	}()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var env struct {
			Type    string          `json:"type"`
			Payload json.RawMessage `json:"payload"`
		}
		if json.Unmarshal([]byte(line), &env) != nil {
			continue
		}
		if env.Type != "reconciliation_mismatch_observed" {
			continue
		}
		var p rrRecovMismatchPayload
		if json.Unmarshal(env.Payload, &p) != nil {
			continue
		}
		if p.MismatchClass == wantClass {
			return p
		}
	}

	t.Fatalf("rrRecovExtractMismatchPayload: event type=reconciliation_mismatch_observed mismatch_class=%q not found in %s",
		wantClass, jsonlPath)
	return rrRecovMismatchPayload{} // unreachable
}

func rrRecovReadGroupItemStatuses(t *testing.T, projectDir string) []string {
	t.Helper()

	queuePath := filepath.Join(projectDir, ".harmonik", "queues", "main.json")
	data, err := os.ReadFile(queuePath) //nolint:gosec // G304: path is t.TempDir()-based; not user input
	if err != nil {
		t.Fatalf("rrRecovReadGroupItemStatuses: ReadFile: %v", err)
	}

	var q struct {
		Groups []struct {
			Items []struct {
				Status string `json:"status"`
			} `json:"items"`
		} `json:"groups"`
	}
	if err := json.Unmarshal(data, &q); err != nil {
		t.Fatalf("rrRecovReadGroupItemStatuses: unmarshal: %v", err)
	}
	if len(q.Groups) == 0 {
		t.Fatal("rrRecovReadGroupItemStatuses: no groups in queue")
	}

	statuses := make([]string, len(q.Groups[0].Items))
	for i, item := range q.Groups[0].Items {
		statuses[i] = item.Status
	}
	return statuses
}

// TestScenario_RestartRecovery_QM002bDeadlock is the scenario-level complement
// to the unit tests in internal/lifecycle/startup_pl005_qm002_test.go.  It
// exercises the LIVE restart-recovery path through the full daemon.Start
// composition root.
//
// The test pre-seeds the exact "deadlock combination" from hk-z0pmi:
//   - One bead stuck at ItemStatus=dispatched in queue.json (crashed daemon
//     left its goroutine abandoned) while its br status is already closed
//     (landed via another path).  Without QM-002b Class A', this item is
//     stuck forever because no goroutine owns it to call
//     evaluateGroupAdvanceWithOutcome.
//   - One failed sibling (cross_queue_duplicate, QM-034): QM-034 requires all
//     items to be terminal before the group advances.  The dispatched sibling
//     blocks this invariant.
//
// The combination meant the group could NEVER reach complete-with-failures,
// so queue.json remained active across daemon restarts and QM-027 refused all
// new submits.
//
// After the Class A' fix (f82c051e, landed in reconcileThreeWay):
//
//  1. daemon.Start → LoadQueueAtStartup → reconcileThreeWay detects bead A
//     is dispatched + closed → advances item to completed, persists queue.
//     Emits reconciliation_mismatch_observed{mismatch_class=bead_closed_queue_dispatched}.
//
//  2. Both items are now terminal (completed + failed), so the same startup
//     reconcile advances the group to complete-with-failures and demotes the
//     queue to paused-by-failure (reconcileQueueTerminalState, the F5 pass).
//
//  3. The queue is no longer active at context-cancel time, so the shutdown
//     drain (drainQueuesForRestart) is a no-op and main.json survives with its
//     failure record.  Do not expect an unlink here — an unlink is the
//     all-complete-success path, and this queue has a failed item.
//
//  4. QM-027 exempts paused-by-failure, so the next submit to the same name is
//     accepted and overwrites the queue with a fresh queue_id.
//
// Run: go test -race -tags=scenario ./internal/daemon/... -run TestScenario_RestartRecovery_QM002bDeadlock
//
// Refs: hk-z0pmi, hk-5pg37, QM-002b Class A', QM-034, QM-027.
// Bead: hk-ivzsl.
func TestScenario_RestartRecovery_QM002bDeadlock(t *testing.T) {
	skipRealDaemonE2EInShort(t)

	realBrPath := rrRecovBrPath(t)

	projectDir, jsonlPath := rrRecovProjectDir(t)

	dbPath := filepath.Join(projectDir, ".beads", "beads.db")
	brWrapper := rrRecovBrWrapperScript(t, realBrPath, dbPath)
	beadAID, beadBID := rrRecovInitBrWithBeads(t, realBrPath, projectDir, brWrapper)
	t.Logf("rrRecov: beadA=%s (dispatched+closed), beadB=%s (failed sibling)", beadAID, beadBID)

	scenariotest.AssertBeadStatus(t, brWrapper, beadAID, "closed")
	scenariotest.AssertBeadStatus(t, brWrapper, beadBID, "open")

	rrRecovWriteStuckQueueJSON(t, projectDir, beadAID, beadBID)

	claudeConfigPath := filepath.Join(rrRecovEvalSymlinks(t, t.TempDir()), ".claude.json")
	prevClaudeCfg, hadClaudeCfg := os.LookupEnv("HARMONIK_CLAUDE_CONFIG_PATH")
	if err := os.Setenv("HARMONIK_CLAUDE_CONFIG_PATH", claudeConfigPath); err != nil {
		t.Fatalf("rrRecov: Setenv HARMONIK_CLAUDE_CONFIG_PATH: %v", err)
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
		NoAutoPull:            true, // queue-only mode; no br-ready fallback
		SkipWALCheckpoint:     true,
		SkipBrHistoryRotation: true,
		SkipRestartBackoff:    true,
		LogWriter:             testLogWriter{t: t},
		WorkflowModeDefault:   core.WorkflowModeDot,
	}

	startDone := make(chan error, 1)
	go func() {
		startDone <- daemon.Start(loopCtx, cfg)
	}()

	const reconcileBudget = 15 * time.Second
	scenariotest.MustCompleteWithin(t, jsonlPath, "", nil, reconcileBudget, func() {
		for {
			if scenariotest.WaitForEvent(t, jsonlPath, "reconciliation_mismatch_observed", "", 50*time.Millisecond) {
				return
			}
		}
	})

	mismatch := rrRecovExtractMismatchPayload(t, jsonlPath, "bead_closed_queue_dispatched")
	if mismatch.BeadID != beadAID {
		t.Errorf("rrRecov: Class A' mismatch.BeadID = %q, want %q", mismatch.BeadID, beadAID)
	}
	if mismatch.QueueStatus != "dispatched" {
		t.Errorf("rrRecov: Class A' mismatch.QueueStatus = %q, want \"dispatched\"", mismatch.QueueStatus)
	}
	if mismatch.LedgerStatus == "" {
		t.Error("rrRecov: Class A' mismatch.LedgerStatus is empty")
	}
	if mismatch.ObservedAt == "" {
		t.Error("rrRecov: Class A' mismatch.ObservedAt is empty")
	}

	itemStatuses := rrRecovReadGroupItemStatuses(t, projectDir)
	if len(itemStatuses) != 2 {
		t.Fatalf("rrRecov: expected 2 items in group 0, got %d", len(itemStatuses))
	}
	if itemStatuses[0] != "completed" {
		t.Errorf("rrRecov: item A status = %q, want \"completed\" (Class A' advance)", itemStatuses[0])
	}
	if itemStatuses[1] != "failed" {
		t.Errorf("rrRecov: item B status = %q, want \"failed\" (sibling unchanged)", itemStatuses[1])
	}

	loopCancel()

	scenariotest.MustCompleteWithin(t, jsonlPath, "", nil, 10*time.Second, func() {
		if err := <-startDone; err != nil {
			t.Errorf("rrRecov: daemon.Start returned error after context cancel: %v", err)
		}
	})

	queueMainPath := filepath.Join(projectDir, ".harmonik", "queues", "main.json")
	if _, statErr := os.Stat(queueMainPath); statErr != nil {
		t.Errorf("rrRecov: .harmonik/queues/main.json must remain after daemon exit (the failure record is kept); statErr=%v", statErr)
	}

	loadedQ, loadErr := queue.Load(context.Background(), projectDir, queue.QueueNameMain)
	if loadErr != nil {
		t.Fatalf("rrRecov: queue.Load after daemon exit: %v", loadErr)
	}
	if loadedQ == nil {
		t.Fatal("rrRecov: queue.Load after daemon exit = nil; want the paused-by-failure queue")
	}
	if loadedQ.Status != queue.QueueStatusPausedByFailure {
		t.Errorf("rrRecov: queue status after daemon exit = %q, want %q (F5 demotes an all-terminal-with-failures queue)",
			loadedQ.Status, queue.QueueStatusPausedByFailure)
	}
	if len(loadedQ.Groups) != 1 {
		t.Fatalf("rrRecov: expected 1 group after daemon exit, got %d", len(loadedQ.Groups))
	}
	if loadedQ.Groups[0].Status != queue.GroupStatusCompleteWithFailures {
		t.Errorf("rrRecov: group status after daemon exit = %q, want %q",
			loadedQ.Groups[0].Status, queue.GroupStatusCompleteWithFailures)
	}

	brAdapter, adapterErr := brcli.NewForProject(brWrapper, projectDir)
	if adapterErr != nil {
		t.Fatalf("rrRecov: brcli.NewForProject: %v", adapterErr)
	}
	ledger := queuewiring.NewBRQueueLedger(brAdapter)

	beadCID := rrRecovCreateOpenBead(t, brWrapper, "restart-recovery test bead C (post-recovery submit)")
	dryRunReq := queue.QueueDryRunRequest{
		SchemaVersion: 1,
		Name:          queue.QueueNameMain,
		Groups: []queue.Group{
			{
				GroupIndex: 0,
				Kind:       queue.GroupKindWave,
				Items:      []queue.Item{{BeadID: core.BeadID(beadCID)}},
			},
		},
	}
	if _, rpcErr := queue.HandleQueueDryRun(context.Background(), dryRunReq, ledger, projectDir); rpcErr != nil {
		t.Errorf("rrRecov: queue dry-run against the paused-by-failure queue was rejected (code=%d message=%q); want accepted — QM-027 is still wedged",
			rpcErr.Code, rpcErr.Message)
	}

	rrRecovForceQueueStatus(t, projectDir, string(queue.QueueStatusActive))
	_, blockedErr := queue.HandleQueueDryRun(context.Background(), dryRunReq, ledger, projectDir)
	if blockedErr == nil {
		t.Error("rrRecov: control: queue dry-run against an ACTIVE queue was accepted; QM-027 never ran")
	} else if blockedErr.Code != queue.ErrorCodeQueueAlreadyActive {
		t.Errorf("rrRecov: control: queue dry-run against an ACTIVE queue = code %d (%q), want %d (queue_already_active)",
			blockedErr.Code, blockedErr.Message, queue.ErrorCodeQueueAlreadyActive)
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

	if !t.Failed() {
		t.Logf("rrRecov: PASS beadA=%s Class-A'-advanced=completed beadB=%s sibling-unchanged=failed queue-paused-by-failure=true subsequent-submit-accepted=true beadC=%s",
			beadAID, beadBID, beadCID)
	}
}
