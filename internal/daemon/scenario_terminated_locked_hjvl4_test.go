//go:build scenario

package daemon_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/daemon/scenariotest"
	"github.com/gregberns/harmonik/internal/eventbus"
	queuecli "github.com/gregberns/harmonik/internal/queue/cli"
)

func tlLockEvalSymlinks(t *testing.T, path string) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatalf("tlLockEvalSymlinks: EvalSymlinks %q: %v", path, err)
	}
	return resolved
}

func tlLockShortTempDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "tll")
	if err != nil {
		t.Fatalf("tlLockShortTempDir: MkdirTemp: %v", err)
	}
	t.Cleanup(func() {
		_ = os.RemoveAll(dir)
	})
	return dir
}

func tlLockProjectDir(t *testing.T) (projectDir, jsonlPath string) {
	t.Helper()
	projectDir = tlLockEvalSymlinks(t, tlLockShortTempDir(t))
	for _, sub := range []string{
		filepath.Join(".harmonik", "events"),
		filepath.Join(".harmonik", "beads-intents"),
	} {
		//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
		if err := os.MkdirAll(filepath.Join(projectDir, sub), 0o755); err != nil {
			t.Fatalf("tlLockProjectDir: mkdir %s: %v", sub, err)
		}
	}
	jsonlPath = filepath.Join(projectDir, ".harmonik", "events", "events.jsonl")
	if err := os.WriteFile(jsonlPath, nil, 0o600); err != nil {
		t.Fatalf("tlLockProjectDir: create empty events.jsonl: %v", err)
	}

	run := func(args ...string) {
		t.Helper()
		cmd := exec.CommandContext(t.Context(), "git", args...)
		cmd.Dir = projectDir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("tlLockProjectDir: git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "--initial-branch=main")
	run("config", "user.email", "test@harmonik.local")
	run("config", "user.name", "Harmonik Test")
	readmePath := filepath.Join(projectDir, "README")
	if err := os.WriteFile(readmePath, []byte("hk-hjvl4 GATE-0 scenario test\n"), 0o644); err != nil {
		t.Fatalf("tlLockProjectDir: WriteFile README: %v", err)
	}
	run("add", "README")
	run("commit", "-m", "Initial commit")

	return projectDir, jsonlPath
}

func tlLockBrPath(t *testing.T) string {
	t.Helper()
	brPath, err := exec.LookPath("br")
	if err != nil {
		t.Skip("br required for scenario test (not on PATH)")
	}
	return brPath
}

func tlLockBrWrapperScript(t *testing.T, realBrPath, dbPath string) string {
	t.Helper()
	dir := tlLockEvalSymlinks(t, t.TempDir())
	path := filepath.Join(dir, "br")
	content := "#!/bin/sh\nexec " + realBrPath + " --db " + dbPath + " \"$@\"\n"
	//nolint:gosec // G306: script is test-only; chmod 0755 required for execution
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatalf("tlLockBrWrapperScript: WriteFile: %v", err)
	}
	return path
}

func tlLockInitBrWithInProgress(t *testing.T, realBrPath, projectDir, brWrapper string) string {
	t.Helper()

	//nolint:gosec // G204: br args are test-internal literals; not user input
	initCmd := exec.CommandContext(t.Context(), realBrPath, "init", "--prefix", "tll")
	initCmd.Dir = projectDir
	initOut, initErr := initCmd.CombinedOutput()
	if initErr != nil {
		t.Fatalf("tlLockInitBrWithInProgress: br init: %v\n%s", initErr, initOut)
	}

	//nolint:gosec // G204: br args are test-internal literals; not user input
	createCmd := exec.CommandContext(t.Context(), brWrapper, "create",
		"terminated-but-locked GATE-0 test bead", "--status", "open", "--silent")
	createOut, createErr := createCmd.CombinedOutput()
	if createErr != nil {
		t.Fatalf("tlLockInitBrWithInProgress: br create: %v\n%s", createErr, createOut)
	}
	beadID := strings.TrimSpace(string(createOut))
	if beadID == "" {
		t.Fatal("tlLockInitBrWithInProgress: br create returned empty ID")
	}

	//nolint:gosec // G204: br args are test-internal literals; not user input
	updateCmd := exec.CommandContext(t.Context(), brWrapper, "update", beadID,
		"--status", "in_progress")
	updateOut, updateErr := updateCmd.CombinedOutput()
	if updateErr != nil {
		t.Fatalf("tlLockInitBrWithInProgress: br update in_progress: %v\n%s", updateErr, updateOut)
	}

	return beadID
}

func tlLockTwinWrapperScript(t *testing.T, twinPath string) string {
	t.Helper()
	dir := tlLockEvalSymlinks(t, t.TempDir())
	path := filepath.Join(dir, "twin-handler-fatal.sh")
	content := "#!/bin/sh\nexec " + twinPath + " --scenario handler-fatal\n"
	//nolint:gosec // G306: script is test-only; chmod 0755 required for execution
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatalf("tlLockTwinWrapperScript: WriteFile: %v", err)
	}
	return path
}

func tlLockWaitForSocket(t *testing.T, projectDir string, budget time.Duration) {
	t.Helper()
	sockPath := filepath.Join(projectDir, ".harmonik", "daemon.sock")
	deadline := time.Now().Add(budget)
	for time.Now().Before(deadline) {
		if fi, err := os.Stat(sockPath); err == nil && fi.Mode()&os.ModeSocket != 0 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("tlLock: daemon socket %s not ready within %s", sockPath, budget)
}

type tlLockRunStartedBead struct {
	BeadID string `json:"bead_id"`
}

type tlLockRunCompletedPayload struct {
	RunID   string `json:"run_id"`
	BeadID  string `json:"bead_id"`
	Success bool   `json:"success"`
	Summary string `json:"summary"`
	EndedAt string `json:"ended_at"`
}

func tlLockAppendTerminatedRun(t *testing.T, jsonlPath, beadID string) string {
	t.Helper()

	runUUID, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("tlLockAppendTerminatedRun: uuid.NewV7: %v", err)
	}
	runID := core.RunID(runUUID)

	writer, err := eventbus.OpenJSONLWriter(jsonlPath)
	if err != nil {
		t.Fatalf("tlLockAppendTerminatedRun: OpenJSONLWriter: %v", err)
	}
	bus := eventbus.NewBusImplWithWriter(core.NewRedactionRegistry(), writer)

	histBeadID := core.BeadID(beadID)
	histWorker := "tlLock-historical-worker"
	histWorkerOS := "linux"
	startedPl, err := json.Marshal(core.RunStartedPayload{
		RunID:                   runID,
		WorkflowID:              core.WorkflowID("standard-bead"),
		WorkflowVersion:         core.WorkflowVersion("1.0"),
		WorkflowMode:            core.WorkflowModeDot,
		ReviewPolicy:            core.ReviewPolicyReviewed,
		WorkflowSelectionSource: core.WorkflowSelectionEmbeddedDefault,
		BeadID:                  &histBeadID,
		WorkspacePath:           "/tmp/tlLock-historical-run",
		InputRef:                "bead:" + beadID,
		StartedAt:               time.Date(2026, 7, 5, 0, 0, 0, 0, time.UTC),
		WorkerName:              &histWorker,
		WorkerOS:                &histWorkerOS,
	})
	if err != nil {
		t.Fatalf("tlLockAppendTerminatedRun: marshal run_started: %v", err)
	}
	var startedRoundTrip core.RunStartedPayload
	if rtErr := json.Unmarshal(startedPl, &startedRoundTrip); rtErr != nil {
		t.Fatalf("tlLockAppendTerminatedRun: fixture run_started payload is not a valid version-2 record: %v", rtErr)
	}
	if emitErr := bus.EmitWithRunID(context.Background(), runID, core.EventTypeRunStarted, startedPl); emitErr != nil {
		t.Fatalf("tlLockAppendTerminatedRun: emit run_started: %v", emitErr)
	}

	failedPl, err := json.Marshal(tlLockRunCompletedPayload{
		RunID:   runID.String(),
		BeadID:  beadID,
		Success: false,
		Summary: "non_ff_merge (hk-hjvl4 GATE-0 historical-run fixture)",
		EndedAt: "2026-07-05T01:00:00Z",
	})
	if err != nil {
		t.Fatalf("tlLockAppendTerminatedRun: marshal run_failed: %v", err)
	}
	if emitErr := bus.EmitWithRunID(context.Background(), runID, core.EventTypeRunFailed, failedPl); emitErr != nil {
		t.Fatalf("tlLockAppendTerminatedRun: emit run_failed: %v", emitErr)
	}

	if err := writer.Close(); err != nil {
		t.Fatalf("tlLockAppendTerminatedRun: writer.Close: %v", err)
	}

	return runID.String()
}

type tlLockRPCResult struct {
	ExitCode  int
	OK        bool
	ErrorCode int
	Message   string
	Raw       string
}

type tlLockErrorBody struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func tlLockParseResult(t *testing.T, exitCode int, out string) tlLockRPCResult {
	t.Helper()
	res := tlLockRPCResult{ExitCode: exitCode, Raw: out}
	switch exitCode {
	case 0:
		res.OK = true
	case 1:
		var body tlLockErrorBody
		if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &body); err != nil {
			t.Fatalf("tlLockParseResult: cannot parse errorBody JSON from %q: %v", out, err)
		}
		res.ErrorCode = body.Code
		res.Message = body.Message
	default:
		t.Fatalf("tlLockParseResult: unexpected CLI exit code %d (raw=%q)", exitCode, out)
	}
	return res
}

func tlLockSubmit(t *testing.T, projectDir, beadID string) tlLockRPCResult {
	t.Helper()
	var out, errOut bytes.Buffer
	code := queuecli.RunQueueSubmit(context.Background(),
		[]string{"--project", projectDir, "--beads", beadID, "--json"}, &out, &errOut)
	if errOut.Len() > 0 {
		t.Logf("tlLockSubmit: stderr: %s", errOut.String())
	}
	return tlLockParseResult(t, code, out.String())
}

func tlLockDryRun(t *testing.T, projectDir, beadID string) tlLockRPCResult {
	t.Helper()
	var out, errOut bytes.Buffer
	code := queuecli.RunQueueDryRun(context.Background(),
		[]string{"--project", projectDir, "--beads", beadID, "--json"}, &out, &errOut)
	if errOut.Len() > 0 {
		t.Logf("tlLockDryRun: stderr: %s", errOut.String())
	}
	return tlLockParseResult(t, code, out.String())
}

func tlLockWaitForFreshRunStarted(t *testing.T, jsonlPath, beadID, excludeRunID string, budget time.Duration) string {
	t.Helper()
	deadline := time.Now().Add(budget)
	for time.Now().Before(deadline) {
		//nolint:gosec // G304: path is t.TempDir()-based; not user input
		data, err := os.ReadFile(jsonlPath)
		if err == nil {
			for _, line := range strings.Split(string(data), "\n") {
				line = strings.TrimSpace(line)
				if line == "" {
					continue
				}
				var env struct {
					Type    string          `json:"type"`
					RunID   string          `json:"run_id"`
					Payload json.RawMessage `json:"payload"`
				}
				if json.Unmarshal([]byte(line), &env) != nil {
					continue
				}
				if env.Type != string(core.EventTypeRunStarted) || env.RunID == excludeRunID {
					continue
				}
				var pl tlLockRunStartedBead
				if json.Unmarshal(env.Payload, &pl) == nil && pl.BeadID == beadID {
					return env.RunID
				}
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("tlLock: no fresh run_started for bead %s (excluding %s) within %s", beadID, excludeRunID, budget)
	return "" // unreachable
}

// TestScenario_TerminatedButLocked_BootReconcileReleasesDispatchLock is the
// GATE-0 isolation e2e for hk-hjvl4. See the file doc comment for the full
// scenario. FAILS on 0cb9529a (lock not released across restart); PASSES on
// 1b348917 (released).
//
// Run standalone (per repo GATE-0 convention — full-suite scenario runs are
// resource-contended and flaky; this class of test is designed to be run in
// isolation):
//
//	go test -race -tags=scenario ./internal/daemon/... \
//	  -run TestScenario_TerminatedButLocked_BootReconcileReleasesDispatchLock
//
// Bead: hk-nxcvi (GATE-0 for hk-hjvl4).
func TestScenario_TerminatedButLocked_BootReconcileReleasesDispatchLock(t *testing.T) {
	skipRealDaemonE2EInShort(t)

	twinPath, ok := scenariotest.TwinBinaryPath()
	if !ok {
		t.Skip("harmonik-twin-claude binary not found; set HARMONIK_TWIN_CLAUDE or build the binary")
	}
	realBrPath := tlLockBrPath(t)

	projectDir, jsonlPath := tlLockProjectDir(t)

	dbPath := filepath.Join(projectDir, ".beads", "beads.db")
	brWrapper := tlLockBrWrapperScript(t, realBrPath, dbPath)
	beadID := tlLockInitBrWithInProgress(t, realBrPath, projectDir, brWrapper)
	t.Logf("tlLock: seeded bead ID = %s (in_progress, no queue/event history yet)", beadID)
	scenariotest.AssertBeadStatus(t, brWrapper, beadID, "in_progress")

	twinWrapper := tlLockTwinWrapperScript(t, twinPath)

	claudeConfigPath := filepath.Join(tlLockEvalSymlinks(t, t.TempDir()), ".claude.json")
	prevClaudeCfg, hadClaudeCfg := os.LookupEnv("HARMONIK_CLAUDE_CONFIG_PATH")
	if err := os.Setenv("HARMONIK_CLAUDE_CONFIG_PATH", claudeConfigPath); err != nil {
		t.Fatalf("tlLock: Setenv HARMONIK_CLAUDE_CONFIG_PATH: %v", err)
	}
	t.Cleanup(func() {
		if hadClaudeCfg {
			_ = os.Setenv("HARMONIK_CLAUDE_CONFIG_PATH", prevClaudeCfg)
		} else {
			_ = os.Unsetenv("HARMONIK_CLAUDE_CONFIG_PATH")
		}
	})

	boot1Ctx, boot1Cancel := context.WithCancel(context.Background())
	defer boot1Cancel() // safety net; explicitly cancelled below at RESTART (idempotent)

	cfg1 := daemon.Config{
		ProjectDir:            projectDir,
		JSONLLogPath:          jsonlPath,
		BrPath:                brWrapper,
		NoAutoPull:            true, // queue-only; no br-ready fallback dispatch of our seeded bead
		SkipWALCheckpoint:     true,
		SkipBrHistoryRotation: true,
		LogWriter:             testLogWriter{t: t},
		WorkflowModeDefault:   core.WorkflowModeDot,
	}

	boot1Done := make(chan error, 1)
	go func() {
		boot1Done <- daemon.Start(boot1Ctx, cfg1)
	}()

	const socketBudget = 20 * time.Second
	tlLockWaitForSocket(t, projectDir, socketBudget)
	t.Logf("tlLock: boot 1 socket live")

	submit1 := tlLockSubmit(t, projectDir, beadID)
	if submit1.OK {
		t.Fatalf("tlLock: boot-1 queue submit unexpectedly succeeded for in_progress bead %s (raw=%s)", beadID, submit1.Raw)
	}
	if submit1.ErrorCode != -32015 {
		t.Fatalf("tlLock: boot-1 queue submit error_code = %d, want -32015 (bead_already_dispatched); raw=%s", submit1.ErrorCode, submit1.Raw)
	}
	t.Logf("tlLock: boot-1 queue submit correctly rejected: %s", submit1.Raw)

	dryrun1 := tlLockDryRun(t, projectDir, beadID)
	if dryrun1.OK {
		t.Fatalf("tlLock: boot-1 queue dry-run unexpectedly succeeded for in_progress bead %s (raw=%s)", beadID, dryrun1.Raw)
	}
	if dryrun1.ErrorCode != -32015 {
		t.Fatalf("tlLock: boot-1 queue dry-run error_code = %d, want -32015 (bead_already_dispatched); raw=%s", dryrun1.ErrorCode, dryrun1.Raw)
	}
	t.Logf("tlLock: boot-1 queue dry-run correctly rejected: %s", dryrun1.Raw)

	historicalRunID := tlLockAppendTerminatedRun(t, jsonlPath, beadID)
	t.Logf("tlLock: appended historical terminated run %s for bead %s", historicalRunID, beadID)

	scenariotest.AssertBeadStatus(t, brWrapper, beadID, "in_progress")

	boot1Cancel()
	scenariotest.MustCompleteWithin(t, jsonlPath, "", nil, 10*time.Second, func() {
		if err := <-boot1Done; err != nil {
			t.Errorf("tlLock: boot 1 daemon.Start returned error after context cancel: %v", err)
		}
	})
	t.Logf("tlLock: boot 1 stopped cleanly")

	boot2Ctx, boot2Cancel := context.WithCancel(context.Background())
	defer boot2Cancel()

	cfg2 := daemon.Config{
		ProjectDir:            projectDir,
		JSONLLogPath:          jsonlPath,
		BrPath:                brWrapper,
		HandlerBinary:         twinWrapper,
		SkipWALCheckpoint:     true,
		SkipBrHistoryRotation: true,
		SkipRestartBackoff:    true,
		AgentReadyTimeout:     5 * time.Second,
		LogWriter:             testLogWriter{t: t},
		WorkflowModeDefault:   core.WorkflowModeDot,
	}

	boot2Done := make(chan error, 1)
	go func() {
		boot2Done <- daemon.Start(boot2Ctx, cfg2)
	}()

	tlLockWaitForSocket(t, projectDir, socketBudget)
	t.Logf("tlLock: boot 2 socket live")

	scenariotest.AssertBeadStatus(t, brWrapper, beadID, "open")
	if !t.Failed() {
		t.Logf("tlLock: boot-2 reconcile released the bead — status is now open")
	}

	submit2 := tlLockSubmit(t, projectDir, beadID)
	if !submit2.OK {
		t.Fatalf("tlLock: boot-2 queue submit still rejected (error_code=%d, message=%q, raw=%s) — dispatch-lock NOT released",
			submit2.ErrorCode, submit2.Message, submit2.Raw)
	}
	t.Logf("tlLock: boot-2 queue submit succeeded: %s", submit2.Raw)

	const runStartedBudget = 20 * time.Second
	freshRunID := tlLockWaitForFreshRunStarted(t, jsonlPath, beadID, historicalRunID, runStartedBudget)
	t.Logf("tlLock: fresh run_started observed: run_id=%s (bead=%s)", freshRunID, beadID)

	boot2Cancel()
	scenariotest.MustCompleteWithin(t, jsonlPath, "", nil, 10*time.Second, func() {
		if err := <-boot2Done; err != nil {
			t.Errorf("tlLock: boot 2 daemon.Start returned error after context cancel: %v", err)
		}
	})

	if !t.Failed() {
		t.Logf("tlLock: PASS bead=%s historical_run=%s fresh_run=%s — boot-reconcile released the terminated-but-locked -32015 dispatch-lock",
			beadID, historicalRunID, freshRunID)
	}
}
