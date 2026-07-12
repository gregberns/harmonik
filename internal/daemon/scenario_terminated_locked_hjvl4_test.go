//go:build scenario

package daemon_test

// scenario_terminated_locked_hjvl4_test.go — GATE-0 isolation e2e for the
// hk-hjvl4 fix: reconcileOrphanedRunsOnResume's "terminated-but-locked" pass
// (runinflightreconcile_hkr73qr.go).
//
// # What is tested
//
// TestScenario_TerminatedButLocked_BootReconcileReleasesDispatchLock reproduces
// the hk-thbbv-class repro end-to-end through TWO successive daemon.Start
// composition-root boots against the SAME project directory (a genuine
// restart, not a single-boot fixture load):
//
//  1. Boot 1 comes up with a bead already at CoarseStatus=in_progress (a stale
//     claim, as if left behind by a prior crashed run) and an EMPTY event log.
//     Because reconcileOrphanedRunsOnResume only scans the durable event log at
//     its OWN startup, and no terminal event exists yet at boot-1 startup, this
//     bead is invisible to every reconcile pass at boot 1 — it stays wedged.
//  2. While boot 1 is alive, `queue submit` / `queue dry-run` against its LIVE
//     socket are asserted to reject the bead with QM-022's bead_already_dispatched
//     (-32015) — the bead's ledger status alone (in_progress) is enough to prove
//     the live daemon considers it dispatched.
//  3. Still while boot 1 is alive, a run_started + run_failed pair for this bead
//     is appended directly to the durable event log — modelling "a historical
//     queue-run whose terminal event WAS recorded but whose bead-status /
//     queue-item update never fully landed" (the hk-hjvl4 gap). This models the
//     durable fact independently of the live daemon's own bookkeeping — sibling
//     scenario tests (hk-iwu8a's dtOrphan, hk-ivzsl's rrRecov) pre-seed the exact
//     same class of durable-but-unreconciled state via direct fixture writes.
//  4. Boot 1 is cancelled (RESTART). Boot 2 starts fresh against the SAME
//     project directory. Its OWN reconcileOrphanedRunsOnResume now scans the
//     event log at ITS startup and finds: the run terminated (run_failed
//     present — the primary orphan loop does NOT touch it), the bead is NOT a
//     dispatchedBeads member (no "main" queue item ever existed for it — the
//     hk-iwu8a pass does NOT touch it either), and it is NOT live. Only the
//     hk-hjvl4 "terminated-but-locked" pass resets it — FAILS before hk-hjvl4
//     (bead stays in_progress forever, -32015 wedged across every restart),
//     PASSES after (bead reset to open before QM-002a runs).
//  5. Once boot 2 is up, `queue submit` for the SAME bead is asserted to
//     SUCCEED, and a fresh run_started fires — the bead re-dispatches cleanly.
//
// # Production composition root
//
// daemon.Start is used directly for BOTH boots (a genuinely isolated,
// standalone daemon instance per boot — no internal seams below daemon.Start
// are used). The queue submit / dry-run calls go through the REAL client-side
// CLI package (internal/queue/cli), over the REAL unix socket, exactly as the
// `harmonik queue submit` / `harmonik queue dry-run` commands do.
//
// # Helper prefix
//
// Helpers in this file use the prefix "tlLock" (terminated-locked).
// Per implementer-protocol.md §Helper-prefix discipline.
//
// # Spec refs
//
//   - specs/process-lifecycle.md §4.5 PL-006 (hk-r73qr).
//   - specs/queue-model.md §3.2a QM-002a (hk-mdus1); §6.10 QM-022 / QM-029b (-32015).
//
// Bead: hk-nxcvi (GATE-0 for hk-hjvl4).

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

// ─────────────────────────────────────────────────────────────────────────────
// tlLock fixture helpers
// ─────────────────────────────────────────────────────────────────────────────

// tlLockEvalSymlinks resolves all symlinks in path so that br — which rejects
// paths containing symlinks outside the beads directory — receives a
// canonical path.
func tlLockEvalSymlinks(t *testing.T, path string) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatalf("tlLockEvalSymlinks: EvalSymlinks %q: %v", path, err)
	}
	return resolved
}

// tlLockShortTempDir creates a SHORT-prefixed temp directory (unlike
// t.TempDir(), which embeds the full (long) test name into the path) and
// registers its removal via t.Cleanup. The project directory MUST stay short
// enough that <projectDir>/.harmonik/daemon.sock fits the AF_UNIX sun_path
// limit (104 bytes, 103 usable) — a long t.TempDir()-derived path here is a
// known landmine (scripts/scratch-daemon.sh hits the same limit).
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

// tlLockProjectDir creates the minimal project directory for the scenario and
// a one-commit git repo (required for real worktree-based dispatch in boot 2).
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
	// Create an empty events.jsonl — boot 1 must start with NO record of any
	// run for the target bead; that absence is the crux of the crash window.
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

// tlLockBrPath returns the path to the real `br` binary, skipping the test
// when br is not on PATH.
func tlLockBrPath(t *testing.T) string {
	t.Helper()
	brPath, err := exec.LookPath("br")
	if err != nil {
		t.Skip("br required for scenario test (not on PATH)")
	}
	return brPath
}

// tlLockBrWrapperScript writes a /bin/sh wrapper that invokes realBrPath with
// --db <dbPath> prepended to all args.
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

// tlLockInitBrWithInProgress initialises a beads workspace in projectDir,
// creates one bead, and sets it to in_progress status — a stale claim as if
// left behind by a prior crashed run, BEFORE boot 1 ever starts. Returns the
// bead ID.
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

// tlLockTwinWrapperScript writes a /bin/sh wrapper that invokes the twin
// binary with --scenario handler-fatal (fast, deterministic run_started +
// run_failed), ignoring all other flags appended by the daemon.
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

// tlLockWaitForSocket polls for the daemon's unix socket to appear, mirroring
// scripts/scratch-daemon.sh's own `[ -S "$sock" ]` readiness check. Fails the
// test if the socket does not appear within budget.
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

// tlLockRunStartedPayload mirrors the wire shape of the (unexported)
// workloopRunStartedPayload — only the JSON tags matter here, not the type
// identity, since this file lives in package daemon_test.
type tlLockRunStartedPayload struct {
	RunID         string `json:"run_id"`
	BeadID        string `json:"bead_id"`
	WorkspacePath string `json:"workspace_path"`
	StartedAt     string `json:"started_at"`
}

// tlLockRunCompletedPayload mirrors the wire shape of the (unexported)
// workloopRunCompletedPayload for a run_failed event.
type tlLockRunCompletedPayload struct {
	RunID   string `json:"run_id"`
	BeadID  string `json:"bead_id"`
	Success bool   `json:"success"`
	Summary string `json:"summary"`
	EndedAt string `json:"ended_at"`
}

// tlLockAppendTerminatedRun appends a run_started + run_failed pair for
// beadID directly to the durable event log at jsonlPath, using a REAL
// eventbus writer (O_APPEND — safe to open independently of the live
// daemon's own writer on the same path; each line stays well under
// PIPE_BUF). Models the "durable event log recorded the terminal event, but
// the queue-item / bead-status update never landed" crash window (hk-hjvl4)
// as an independently-verifiable durable fact, not something the live daemon
// process itself needs to have produced. Returns the synthetic run's ID.
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

	startedPl, err := json.Marshal(tlLockRunStartedPayload{
		RunID:         runID.String(),
		BeadID:        beadID,
		WorkspacePath: "/tmp/tlLock-historical-run",
		StartedAt:     "2026-07-05T00:00:00Z",
	})
	if err != nil {
		t.Fatalf("tlLockAppendTerminatedRun: marshal run_started: %v", err)
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

// tlLockRPCResult is the decoded outcome of a `queue submit` / `queue
// dry-run` CLI call against a live daemon socket.
type tlLockRPCResult struct {
	ExitCode  int
	OK        bool
	ErrorCode int
	Message   string
	Raw       string
}

// tlLockErrorBody mirrors internal/queue/cli's (unexported) errorBody JSON
// shape — only the JSON tags matter here.
type tlLockErrorBody struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// tlLockParseResult decodes a CLI exit code + --json stdout body into a
// tlLockRPCResult. exitCode 0 = success (OK=true); exitCode 1 = validation
// error (ErrorCode / Message decoded from the errorBody JSON).
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

// tlLockSubmit calls `harmonik queue submit --beads <beadID> --json` against
// the live daemon socket under projectDir, via the REAL client-side CLI
// package (the same code path `harmonik queue submit` runs).
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

// tlLockDryRun calls `harmonik queue dry-run --beads <beadID> --json` against
// the live daemon socket under projectDir.
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

// tlLockWaitForFreshRunStarted polls jsonlPath for a run_started event for
// beadID whose run_id differs from excludeRunID (the synthetic historical
// run injected by tlLockAppendTerminatedRun). Returns the fresh run_id.
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
				var pl tlLockRunStartedPayload
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

// ─────────────────────────────────────────────────────────────────────────────
// TestScenario_TerminatedButLocked_BootReconcileReleasesDispatchLock
// ─────────────────────────────────────────────────────────────────────────────

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
	// Not parallel: uses os.Setenv(HARMONIK_CLAUDE_CONFIG_PATH) — same
	// rationale as every other scenario test in this package.

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

	// ═══════════════════════════════════════════════════════════════════════
	// BOOT 1 — the "crashed" daemon. Comes up with an empty event log (no
	// terminal event recorded YET for this bead), so no reconcile pass touches
	// it at boot-1 startup. It stays wedged in_progress.
	// ═══════════════════════════════════════════════════════════════════════

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

	// ── Step 2: confirm the LIVE daemon rejects submit/dry-run for the
	// stale in_progress bead with -32015 (QM-022 bead_already_dispatched). ──

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

	// ── Step 3: append the historical run's terminal event to the durable
	// log WHILE boot 1 is still alive. The bead's ledger status remains
	// in_progress (boot 1's own startup already ran; nothing re-scans this
	// mid-lifetime) — this is exactly the "terminal event recorded, but the
	// bead-status / queue-item update never landed" gap hk-hjvl4 targets. ──

	historicalRunID := tlLockAppendTerminatedRun(t, jsonlPath, beadID)
	t.Logf("tlLock: appended historical terminated run %s for bead %s", historicalRunID, beadID)

	scenariotest.AssertBeadStatus(t, brWrapper, beadID, "in_progress")

	// ═══════════════════════════════════════════════════════════════════════
	// RESTART — stop boot 1, start boot 2 fresh against the SAME project dir.
	// ═══════════════════════════════════════════════════════════════════════

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

	// ── Step 4: boot-reconcile must have released the dispatch-lock. ──
	//
	// reconcileOrphanedRunsOnResume's terminated-but-locked pass (hk-hjvl4)
	// runs synchronously before the socket is bound, so by the time the
	// socket exists, the bead has already been reset to open (if the fix is
	// present) — or is still stuck in_progress (if it is not).

	scenariotest.AssertBeadStatus(t, brWrapper, beadID, "open")
	t.Logf("tlLock: boot-2 reconcile released the bead — status is now open")

	// ── Step 5: the bead re-dispatches cleanly — submit succeeds, run_started
	// fires, no -32015. ──

	submit2 := tlLockSubmit(t, projectDir, beadID)
	if !submit2.OK {
		t.Fatalf("tlLock: boot-2 queue submit still rejected (error_code=%d, message=%q, raw=%s) — dispatch-lock NOT released",
			submit2.ErrorCode, submit2.Message, submit2.Raw)
	}
	t.Logf("tlLock: boot-2 queue submit succeeded: %s", submit2.Raw)

	const runStartedBudget = 20 * time.Second
	freshRunID := tlLockWaitForFreshRunStarted(t, jsonlPath, beadID, historicalRunID, runStartedBudget)
	t.Logf("tlLock: fresh run_started observed: run_id=%s (bead=%s)", freshRunID, beadID)

	// ── Cleanup: stop boot 2 ──

	boot2Cancel()
	scenariotest.MustCompleteWithin(t, jsonlPath, "", nil, 10*time.Second, func() {
		if err := <-boot2Done; err != nil {
			t.Errorf("tlLock: boot 2 daemon.Start returned error after context cancel: %v", err)
		}
	})

	t.Logf("tlLock: PASS bead=%s historical_run=%s fresh_run=%s — boot-reconcile released the terminated-but-locked -32015 dispatch-lock",
		beadID, historicalRunID, freshRunID)
}
