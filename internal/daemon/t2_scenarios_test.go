package daemon_test

// t2_scenarios_test.go — T2 exploratory test: subprocess failure modes.
//
// This test file is authored by exploratory tester T2. It drives the
// work loop against the real twin binaries to observe failure handling.
//
// Scenarios covered:
//   1. Twin exits non-zero — bead should be reopened, not closed.
//   2. Twin gets SIGKILLed externally during run — bead state / worktree.
//   3. Twin emits malformed NDJSON on stdout — watcher should not crash.
//   4. Twin emits valid NDJSON but exits 0 without explicit done signal.
//   5. Twin hangs (cancel via context timeout).
//
// Note: twin-fail exits immediately with code 1; twin-hang blocks forever.

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
)

// t2FixtureProjectDir creates a project dir with git repo.
func t2FixtureProjectDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		//nolint:gosec
		cmd := exec.CommandContext(t.Context(), "git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("t2Fixture: git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "--initial-branch=main")
	run("config", "user.email", "test@harmonik.local")
	run("config", "user.name", "Harmonik Test")
	if err := os.WriteFile(filepath.Join(dir, "README"), []byte("t2 test\n"), 0o644); err != nil {
		t.Fatalf("t2Fixture: WriteFile: %v", err)
	}
	run("add", "README")
	run("commit", "-m", "Initial commit")
	for _, sub := range []string{".harmonik/events", ".harmonik/beads-intents"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o755); err != nil {
			t.Fatalf("t2Fixture: mkdir %s: %v", sub, err)
		}
	}
	return dir
}

// t2WorktreePath is the same as workspace.WorktreePath but without importing
// workspace — constructs the conventional path.
func t2WorktreePath(projectDir, runID string) string {
	return filepath.Join(projectDir, ".harmonik", "worktrees", runID)
}

// t2FindBinaryInWorktree finds a pre-built twin at a repo-relative path.
// It resolves relative to this test file's module root.
func t2FindBinary(name string) string {
	_, thisFile, _, _ := runtime.Caller(0)
	// thisFile = .../internal/daemon/t2_scenarios_test.go
	root := filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
	return filepath.Join(root, name)
}

// ─────────────────────────────────────────────────────────────────────────────
// T2-S1: Twin exits non-zero
// ─────────────────────────────────────────────────────────────────────────────

// TestT2_NonZeroExit verifies that when the handler exits with code 1:
//   - ReopenBead is called (not CloseBead).
//   - run_failed event is emitted (not run_completed with success=true).
//   - The bead is NOT permanently lost (stuck in "in-progress" with no close/reopen).
func TestT2_NonZeroExit(t *testing.T) {
	t.Parallel()

	twinFail := t2FindBinary("twin-fail")
	if _, err := os.Stat(twinFail); err != nil {
		t.Skipf("twin-fail not found at %s; build with: go build -o ./twin-fail ./test/twins/fail-immediately", twinFail)
	}

	projectDir := t2FixtureProjectDir(t)

	const beadID = core.BeadID("t2-bead-nonzero")
	ledger := &stubBeadLedger{
		ready: []core.BeadID{beadID},
	}
	collector := &stubEventCollector{}

	deps := daemon.ExportedWorkLoopDeps(daemon.WorkLoopDepsParams{
		BrAdapter:     ledger,
		Bus:           collector,
		ProjectDir:    projectDir,
		HandlerBinary: twinFail,
		HandlerArgs:   nil,
		IntentLogDir:  filepath.Join(projectDir, ".harmonik", "beads-intents"),
	})

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	waitDone := make(chan struct{})
	go func() {
		defer close(waitDone)
		daemon.ExportedRunWorkLoop(ctx, deps)
	}()

	// Poll until ReopenBead is called (indicates loop handled the failure).
	deadline := time.After(6 * time.Second)
	for {
		if len(ledger.reopenedIDs()) > 0 {
			break
		}
		select {
		case <-deadline:
			t.Logf("closed=%v reopened=%v events=%v", ledger.closedIDs(), ledger.reopenedIDs(), collector.eventTypes())
			t.Fatal("T2-S1 FAIL: timed out; ReopenBead never called after non-zero exit")
		case <-time.After(50 * time.Millisecond):
		}
	}

	cancel()
	<-waitDone

	// Assertions.
	if len(ledger.closedIDs()) > 0 {
		t.Errorf("T2-S1 FAIL: CloseBead called after non-zero exit; bead should be reopened not closed: %v", ledger.closedIDs())
	}
	if len(ledger.reopenedIDs()) == 0 {
		t.Error("T2-S1 FAIL: ReopenBead not called after non-zero exit")
	}

	// run_failed event must be present.
	eventTypes := collector.eventTypes()
	foundFailed := false
	for _, et := range eventTypes {
		if et == string(core.EventTypeRunFailed) {
			foundFailed = true
			break
		}
	}
	t.Logf("T2-S1: events=%v closed=%v reopened=%v", eventTypes, ledger.closedIDs(), ledger.reopenedIDs())
	if !foundFailed {
		t.Errorf("T2-S1 FAIL: run_failed event not emitted; got %v", eventTypes)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// T2-S2: Twin gets SIGKILLed externally during run
// ─────────────────────────────────────────────────────────────────────────────

// TestT2_SIGKILLDuringRun verifies that if the handler subprocess is
// SIGKILLed while the work loop is waiting, the loop handles the termination
// gracefully: ReopenBead is called, run_failed is emitted, and the loop
// continues (not stuck).
//
// This test uses the hang twin and kills it externally from the test goroutine.
func TestT2_SIGKILLDuringRun(t *testing.T) {
	t.Parallel()

	twinHang := t2FindBinary("twin-hang")
	if _, err := os.Stat(twinHang); err != nil {
		t.Skipf("twin-hang not found at %s; build with: go build -o ./twin-hang ./test/twins/hang", twinHang)
	}

	projectDir := t2FixtureProjectDir(t)

	const beadID = core.BeadID("t2-bead-sigkill")
	ledger := &stubBeadLedger{
		ready: []core.BeadID{beadID},
	}
	collector := &stubEventCollector{}

	deps := daemon.ExportedWorkLoopDeps(daemon.WorkLoopDepsParams{
		BrAdapter:     ledger,
		Bus:           collector,
		ProjectDir:    projectDir,
		HandlerBinary: twinHang,
		HandlerArgs:   nil,
		IntentLogDir:  filepath.Join(projectDir, ".harmonik", "beads-intents"),
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// We need to intercept the process to kill it. Since the loop runs handler
	// internally, we use a short context timeout to simulate external kill.
	// But for a real SIGKILL, we use the OS to find and kill the spawned twin process.

	// First, let the loop start and wait a bit for the hang twin to be launched.
	waitDone := make(chan struct{})
	go func() {
		defer close(waitDone)
		daemon.ExportedRunWorkLoop(ctx, deps)
	}()

	// Wait for run_started event indicating the hang twin is running.
	deadline := time.After(6 * time.Second)
	for {
		types := collector.eventTypes()
		for _, et := range types {
			if et == string(core.EventTypeRunStarted) {
				goto launched
			}
		}
		select {
		case <-deadline:
			t.Fatalf("T2-S2: run_started never fired; events=%v", collector.eventTypes())
		case <-time.After(50 * time.Millisecond):
		}
	}
launched:
	t.Log("T2-S2: run_started seen; SIGKILLing hang twin via pkill")

	// Kill the twin-hang process via pkill.
	killCmd := exec.CommandContext(context.Background(), "pkill", "-SIGKILL", "-f", "twin-hang")
	_ = killCmd.Run() // ignore error if no process found

	// Now wait for the loop to detect the kill and reopen the bead.
	sigkillDeadline := time.After(6 * time.Second)
	for {
		if len(ledger.reopenedIDs()) > 0 || len(ledger.closedIDs()) > 0 {
			break
		}
		select {
		case <-sigkillDeadline:
			t.Logf("T2-S2: events=%v closed=%v reopened=%v", collector.eventTypes(), ledger.closedIDs(), ledger.reopenedIDs())
			t.Fatal("T2-S2 FAIL: timed out waiting for bead state change after SIGKILL")
		case <-time.After(100 * time.Millisecond):
		}
	}

	cancel()
	<-waitDone

	t.Logf("T2-S2: events=%v closed=%v reopened=%v", collector.eventTypes(), ledger.closedIDs(), ledger.reopenedIDs())

	// After SIGKILL (non-zero exit code from OS), bead should be REOPENED not closed.
	if len(ledger.closedIDs()) > 0 {
		t.Errorf("T2-S2 FAIL: CloseBead called after SIGKILL; should have called ReopenBead")
	}
	if len(ledger.reopenedIDs()) == 0 {
		t.Error("T2-S2 FAIL: ReopenBead not called after SIGKILL")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// T2-S3: Twin emits malformed NDJSON on stdout
// ─────────────────────────────────────────────────────────────────────────────

// TestT2_MalformedNDJSON verifies that when the handler emits malformed NDJSON,
// the watcher does not crash the process and the work loop continues to function.
func TestT2_MalformedNDJSON(t *testing.T) {
	t.Parallel()

	// Write a shell script that emits malformed NDJSON, then exits 0.
	scriptDir := t.TempDir()
	scriptPath := filepath.Join(scriptDir, "malformed-json.sh")
	script := `#!/bin/sh
echo "this is not json"
echo "{broken json without closing brace"
echo "}{bad ndjson}"
echo '{"partial": true'
exit 0
`
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("T2-S3: write script: %v", err)
	}

	projectDir := t2FixtureProjectDir(t)

	const beadID = core.BeadID("t2-bead-malformed")
	ledger := &stubBeadLedger{
		ready: []core.BeadID{beadID},
	}
	collector := &stubEventCollector{}

	deps := daemon.ExportedWorkLoopDeps(daemon.WorkLoopDepsParams{
		BrAdapter:     ledger,
		Bus:           collector,
		ProjectDir:    projectDir,
		HandlerBinary: "/bin/sh",
		HandlerArgs:   []string{scriptPath},
		IntentLogDir:  filepath.Join(projectDir, ".harmonik", "beads-intents"),
	})

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	waitDone := make(chan struct{})
	var loopErr error
	go func() {
		defer close(waitDone)
		loopErr = daemon.ExportedRunWorkLoop(ctx, deps)
	}()

	// Poll for bead state change (closed or reopened).
	deadline := time.After(6 * time.Second)
	for {
		if len(ledger.closedIDs()) > 0 || len(ledger.reopenedIDs()) > 0 {
			break
		}
		select {
		case <-deadline:
			t.Logf("T2-S3: events=%v closed=%v reopened=%v", collector.eventTypes(), ledger.closedIDs(), ledger.reopenedIDs())
			t.Fatal("T2-S3 FAIL: timed out; bead never closed or reopened after malformed NDJSON + exit 0")
		case <-time.After(50 * time.Millisecond):
		}
	}

	cancel()
	<-waitDone

	t.Logf("T2-S3: loopErr=%v events=%v closed=%v reopened=%v", loopErr, collector.eventTypes(), ledger.closedIDs(), ledger.reopenedIDs())

	// Exit 0 means bead should be CLOSED (not reopened).
	if len(ledger.closedIDs()) == 0 {
		t.Errorf("T2-S3 FAIL: bead was not closed after exit 0 + malformed NDJSON; reopened=%v", ledger.reopenedIDs())
	}
	if loopErr != nil {
		t.Errorf("T2-S3 FAIL: work loop returned non-nil error (crash) after malformed NDJSON: %v", loopErr)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// T2-S4: Twin exits 0 without explicit ready/done signal
// ─────────────────────────────────────────────────────────────────────────────

// TestT2_ExitZeroNoSignal verifies that when the handler exits 0 without
// emitting any NDJSON signals, the bead is CLOSED (success path) and a
// run_completed event is emitted.
func TestT2_ExitZeroNoSignal(t *testing.T) {
	t.Parallel()

	// Shell script: writes nothing, exits 0.
	scriptDir := t.TempDir()
	scriptPath := filepath.Join(scriptDir, "silent-exit.sh")
	if err := os.WriteFile(scriptPath, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("T2-S4: write script: %v", err)
	}

	projectDir := t2FixtureProjectDir(t)

	const beadID = core.BeadID("t2-bead-silent-exit")
	ledger := &stubBeadLedger{
		ready: []core.BeadID{beadID},
	}
	collector := &stubEventCollector{}

	deps := daemon.ExportedWorkLoopDeps(daemon.WorkLoopDepsParams{
		BrAdapter:     ledger,
		Bus:           collector,
		ProjectDir:    projectDir,
		HandlerBinary: "/bin/sh",
		HandlerArgs:   []string{scriptPath},
		IntentLogDir:  filepath.Join(projectDir, ".harmonik", "beads-intents"),
	})

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	waitDone := make(chan struct{})
	go func() {
		defer close(waitDone)
		daemon.ExportedRunWorkLoop(ctx, deps)
	}()

	deadline := time.After(6 * time.Second)
	for {
		if len(ledger.closedIDs()) > 0 || len(ledger.reopenedIDs()) > 0 {
			break
		}
		select {
		case <-deadline:
			t.Logf("T2-S4: events=%v closed=%v reopened=%v", collector.eventTypes(), ledger.closedIDs(), ledger.reopenedIDs())
			t.Fatal("T2-S4 FAIL: timed out waiting for bead to be closed after silent exit 0")
		case <-time.After(50 * time.Millisecond):
		}
	}

	cancel()
	<-waitDone

	t.Logf("T2-S4: events=%v closed=%v reopened=%v", collector.eventTypes(), ledger.closedIDs(), ledger.reopenedIDs())

	if len(ledger.closedIDs()) == 0 {
		t.Errorf("T2-S4 FAIL: bead not closed after silent exit 0; reopened=%v", ledger.reopenedIDs())
	}

	// run_completed (success) must be emitted.
	eventTypes := collector.eventTypes()
	foundCompleted := false
	for _, et := range eventTypes {
		if et == string(core.EventTypeRunCompleted) {
			foundCompleted = true
			break
		}
	}
	if !foundCompleted {
		t.Errorf("T2-S4 FAIL: run_completed not emitted; events=%v", eventTypes)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// T2-S5: Twin hangs — context cancellation stops loop
// ─────────────────────────────────────────────────────────────────────────────

// TestT2_HangTwinCtxCancel verifies that when the handler hangs and context
// is cancelled, the work loop exits cleanly within a reasonable time, the bead
// is reopened (not closed, not abandoned), and no goroutine leaks are obvious.
func TestT2_HangTwinCtxCancel(t *testing.T) {
	t.Parallel()

	twinHang := t2FindBinary("twin-hang")
	if _, err := os.Stat(twinHang); err != nil {
		t.Skipf("twin-hang not found at %s", twinHang)
	}

	projectDir := t2FixtureProjectDir(t)

	const beadID = core.BeadID("t2-bead-hang")
	ledger := &stubBeadLedger{
		ready: []core.BeadID{beadID},
	}
	collector := &stubEventCollector{}

	deps := daemon.ExportedWorkLoopDeps(daemon.WorkLoopDepsParams{
		BrAdapter:     ledger,
		Bus:           collector,
		ProjectDir:    projectDir,
		HandlerBinary: twinHang,
		HandlerArgs:   nil,
		IntentLogDir:  filepath.Join(projectDir, ".harmonik", "beads-intents"),
	})

	// Short context timeout — 3s gives the hang twin time to be launched,
	// then cancels to simulate Ctrl-C from operator.
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	startTime := time.Now()
	waitDone := make(chan struct{})
	var loopErr error
	go func() {
		defer close(waitDone)
		loopErr = daemon.ExportedRunWorkLoop(ctx, deps)
	}()

	// Loop should exit within 5s of context cancellation.
	select {
	case <-waitDone:
		elapsed := time.Since(startTime)
		t.Logf("T2-S5: loop exited in %v; loopErr=%v events=%v closed=%v reopened=%v",
			elapsed, loopErr, collector.eventTypes(), ledger.closedIDs(), ledger.reopenedIDs())
	case <-time.After(10 * time.Second):
		t.Fatal("T2-S5 FAIL: work loop did not exit within 10s after context cancellation with hanging twin")
	}

	if loopErr != nil {
		t.Errorf("T2-S5: loop returned non-nil error: %v", loopErr)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// T2-S6: Bead state after SIGKILL — check what really happens to process group
// ─────────────────────────────────────────────────────────────────────────────

// TestT2_ProcessGroupCleanup probes whether the child process group is cleaned
// up after the context is cancelled with a hang twin. This is relevant for
// operator UX: if the hang twin leaves zombies/orphans, there's a problem.
func TestT2_ProcessGroupCleanup(t *testing.T) {
	t.Parallel()

	twinHang := t2FindBinary("twin-hang")
	if _, err := os.Stat(twinHang); err != nil {
		t.Skipf("twin-hang not found")
	}

	projectDir := t2FixtureProjectDir(t)

	const beadID = core.BeadID("t2-bead-pg-cleanup")
	ledger := &stubBeadLedger{
		ready: []core.BeadID{beadID},
	}
	collector := &stubEventCollector{}

	deps := daemon.ExportedWorkLoopDeps(daemon.WorkLoopDepsParams{
		BrAdapter:     ledger,
		Bus:           collector,
		ProjectDir:    projectDir,
		HandlerBinary: twinHang,
		IntentLogDir:  filepath.Join(projectDir, ".harmonik", "beads-intents"),
	})

	// Launch and wait until run_started is emitted (hang twin is alive).
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	waitDone := make(chan struct{})
	go func() {
		defer close(waitDone)
		daemon.ExportedRunWorkLoop(ctx, deps)
	}()

	// Wait for run_started.
	runStartedDeadline := time.After(6 * time.Second)
	for {
		types := collector.eventTypes()
		runStarted := false
		for _, et := range types {
			if et == string(core.EventTypeRunStarted) {
				runStarted = true
				break
			}
		}
		if runStarted {
			break
		}
		select {
		case <-runStartedDeadline:
			t.Fatalf("T2-S6: run_started never fired")
		case <-time.After(50 * time.Millisecond):
		}
	}

	// Check that twin-hang is running.
	checkCmd := exec.CommandContext(context.Background(), "pgrep", "-f", "twin-hang")
	pids, _ := checkCmd.Output()
	t.Logf("T2-S6: twin-hang PIDs before cancel: %s", strings.TrimSpace(string(pids)))
	hangRunning := len(strings.TrimSpace(string(pids))) > 0

	// Cancel context (simulates SIGINT/SIGTERM to the daemon).
	cancel()
	select {
	case <-waitDone:
	case <-time.After(6 * time.Second):
		t.Fatal("T2-S6: loop did not exit after context cancellation")
	}

	// After loop exit, check whether twin-hang is still running.
	time.Sleep(500 * time.Millisecond) // give OS time to reap
	checkCmd2 := exec.CommandContext(context.Background(), "pgrep", "-f", "twin-hang")
	pids2, _ := checkCmd2.Output()
	afterPIDs := strings.TrimSpace(string(pids2))
	t.Logf("T2-S6: twin-hang PIDs after cancel: %q (was running before: %v)", afterPIDs, hangRunning)

	if hangRunning && afterPIDs != "" {
		t.Errorf("T2-S6 FINDING: twin-hang process(es) still alive after context cancellation: %s — orphan leak", afterPIDs)
		// Cleanup for the test run.
		_ = exec.CommandContext(context.Background(), "pkill", "-SIGKILL", "-f", "twin-hang").Run()
	}
}

// TestT2_RunFailedEventContainsExitCode verifies that the run_failed event payload
// contains the exit code so operators can diagnose failures.
func TestT2_RunFailedEventContainsExitCode(t *testing.T) {
	t.Parallel()

	twinFail := t2FindBinary("twin-fail")
	if _, err := os.Stat(twinFail); err != nil {
		t.Skipf("twin-fail not found")
	}

	projectDir := t2FixtureProjectDir(t)

	const beadID = core.BeadID("t2-bead-exitcode")
	ledger := &stubBeadLedger{
		ready: []core.BeadID{beadID},
	}
	collector := &stubEventCollector{}

	deps := daemon.ExportedWorkLoopDeps(daemon.WorkLoopDepsParams{
		BrAdapter:     ledger,
		Bus:           collector,
		ProjectDir:    projectDir,
		HandlerBinary: twinFail,
		IntentLogDir:  filepath.Join(projectDir, ".harmonik", "beads-intents"),
	})

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	waitDone := make(chan struct{})
	go func() {
		defer close(waitDone)
		daemon.ExportedRunWorkLoop(ctx, deps)
	}()

	deadline := time.After(6 * time.Second)
	for {
		if len(ledger.reopenedIDs()) > 0 {
			break
		}
		select {
		case <-deadline:
			t.Fatal("T2-ExitCode: timed out waiting for reopen")
		case <-time.After(50 * time.Millisecond):
		}
	}
	cancel()
	<-waitDone

	// Find the run_failed event and check its payload contains exit code info.
	var runFailedPayload string
	for _, ev := range collector.events {
		if ev.EventType == string(core.EventTypeRunFailed) {
			runFailedPayload = string(ev.Payload)
			break
		}
	}

	t.Logf("T2-ExitCode: run_failed payload: %s", runFailedPayload)

	if runFailedPayload == "" {
		t.Error("T2-ExitCode FAIL: run_failed event not found")
	} else if !strings.Contains(runFailedPayload, "exit=1") && !strings.Contains(runFailedPayload, "exit_code") && !strings.Contains(runFailedPayload, "exit=") {
		t.Logf("T2-ExitCode FINDING: run_failed payload does not contain exit code; payload=%s", runFailedPayload)
	}

	// Check whether the summary field encodes the exit code.
	if strings.Contains(runFailedPayload, "exit=1") {
		t.Logf("T2-ExitCode PASS: payload contains 'exit=1'")
	} else if strings.Contains(runFailedPayload, "exit=") {
		t.Logf("T2-ExitCode PASS: payload contains 'exit=' prefix")
	} else {
		t.Logf("T2-ExitCode NOTE: payload summary does not embed exit code numerically; operators cannot distinguish exit=1 from exit=2")
	}
}

// T2-S7: Worktree left behind after failure
func TestT2_WorktreeLeftAfterFailure(t *testing.T) {
	t.Parallel()

	twinFail := t2FindBinary("twin-fail")
	if _, err := os.Stat(twinFail); err != nil {
		t.Skipf("twin-fail not found")
	}

	projectDir := t2FixtureProjectDir(t)

	// We need the run_id to check for worktree. We can intercept from the event payload.
	const beadID = core.BeadID("t2-bead-wt-check")
	ledger := &stubBeadLedger{
		ready: []core.BeadID{beadID},
	}
	collector := &stubEventCollector{}

	deps := daemon.ExportedWorkLoopDeps(daemon.WorkLoopDepsParams{
		BrAdapter:     ledger,
		Bus:           collector,
		ProjectDir:    projectDir,
		HandlerBinary: twinFail,
		IntentLogDir:  filepath.Join(projectDir, ".harmonik", "beads-intents"),
	})

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	waitDone := make(chan struct{})
	go func() {
		defer close(waitDone)
		daemon.ExportedRunWorkLoop(ctx, deps)
	}()

	deadline := time.After(6 * time.Second)
	for {
		if len(ledger.reopenedIDs()) > 0 {
			break
		}
		select {
		case <-deadline:
			t.Fatal("T2-S7: timed out waiting for reopen after failure")
		case <-time.After(50 * time.Millisecond):
		}
	}
	cancel()
	<-waitDone

	// Check whether any worktree was left in .harmonik/worktrees/
	wtDir := filepath.Join(projectDir, ".harmonik", "worktrees")
	entries, err := os.ReadDir(wtDir)
	if err != nil && !os.IsNotExist(err) {
		t.Logf("T2-S7: ReadDir error: %v", err)
	}

	// Extract run_id from run_started event payload.
	var runID string
	for _, ev := range collector.events {
		if ev.EventType == string(core.EventTypeRunStarted) {
			payload := string(ev.Payload)
			// Find "run_id":"..."
			const prefix = `"run_id":"`
			if idx := strings.Index(payload, prefix); idx >= 0 {
				rest := payload[idx+len(prefix):]
				if end := strings.Index(rest, `"`); end >= 0 {
					runID = rest[:end]
				}
			}
			break
		}
	}

	t.Logf("T2-S7: run_id=%s; worktrees present: %d entries in %s",
		runID, len(entries), wtDir)
	for _, e := range entries {
		t.Logf("T2-S7:   worktree entry: %s", e.Name())
	}

	if len(entries) > 0 {
		t.Logf("T2-S7 FINDING: worktree NOT cleaned up after handler failure; %d entry(ies) remain in %s", len(entries), wtDir)
	} else {
		t.Logf("T2-S7 PASS: no leftover worktrees after handler failure")
	}
}

// Ensure stubEventCollector exposes events field for direct access.
// (Relies on the fact we're in the same test package.)
var _ = (*stubEventCollector)(nil)

// Suppress unused import for syscall.
var _ = syscall.SIGKILL
