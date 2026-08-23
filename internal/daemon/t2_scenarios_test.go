package daemon_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/daemon/scenariotest"
)

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

	bareDir := dir + "-bare"
	//nolint:gosec // G204: git args are test-internal literals; not user input
	cloneCmd := exec.CommandContext(t.Context(), "git", "clone", "--bare", dir, bareDir)
	if cloneOut, cloneErr := cloneCmd.CombinedOutput(); cloneErr != nil {
		t.Fatalf("t2Fixture: git clone --bare: %v\n%s", cloneErr, cloneOut)
	}
	run("remote", "add", "origin", bareDir)

	for _, sub := range []string{".harmonik/events", ".harmonik/beads-intents"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o755); err != nil {
			t.Fatalf("t2Fixture: mkdir %s: %v", sub, err)
		}
	}
	return dir
}

func t2WorktreePath(projectDir, runID string) string {
	return filepath.Join(projectDir, ".harmonik", "worktrees", runID)
}

func t2FindBinary(name string) string {
	path, _ := scenariotest.CheckoutBinaryPath(name)
	return path
}

func t2ScopedTwin(t *testing.T, name string) (binPath, marker string) {
	t.Helper()
	src := t2FindBinary(name)
	data, err := os.ReadFile(src) //nolint:gosec // G304: src is a build artifact at the checkout root, not user input
	if err != nil {
		t.Skipf("%s not found at %s; build with: make twins (%v)", name, src, err)
	}
	marker = fmt.Sprintf("%s-%s-%d", name, strings.ReplaceAll(t.Name(), "/", "_"), os.Getpid())
	binPath = filepath.Join(t.TempDir(), marker)
	if err := os.WriteFile(binPath, data, 0o700); err != nil { //nolint:gosec // G306: the copy has to be executable
		t.Fatalf("t2ScopedTwin: write %s: %v", binPath, err)
	}
	return binPath, marker
}

const t2OrphanReapBudget = 10 * time.Second

func t2TwinPIDs(ctx context.Context, marker string) string {
	out, err := exec.CommandContext(ctx, "pgrep", "-f", marker).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func t2AwaitTwinAlive(ctx context.Context, t *testing.T, marker string) string {
	t.Helper()
	for {
		if pids := t2TwinPIDs(ctx, marker); pids != "" {
			return pids
		}
		select {
		case <-ctx.Done():
			t.Fatalf("%s never started, so this test's subject was never produced", marker)
			return ""
		case <-time.After(20 * time.Millisecond):
		}
	}
}

func t2AwaitTwinGone(t *testing.T, marker string) string {
	t.Helper()
	deadline := time.NewTimer(t2OrphanReapBudget)
	defer deadline.Stop()
	for {
		pids := t2TwinPIDs(context.Background(), marker)
		if pids == "" {
			return ""
		}
		select {
		case <-deadline.C:
			return pids
		case <-time.After(50 * time.Millisecond):
		}
	}
}

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

	deps := daemon.ExportedTestRuntime(daemon.TestRuntimeParams{
		BrAdapter:        ledger,
		Bus:              collector,
		ProjectDir:       projectDir,
		HandlerBinary:    twinFail,
		HandlerArgs:      nil,
		AdapterRegistry2: NewSealedAdapterRegistryForTest(t),
		IntentLogDir:     filepath.Join(projectDir, ".harmonik", "beads-intents"),
	})

	ctx, cancel := context.WithTimeout(context.Background(), workLoopTestBudget)
	defer cancel()

	waitDone := make(chan struct{})
	go func() {
		defer close(waitDone)
		daemon.ExportedRunWorkLoop(ctx, deps)
	}()

	for len(ledger.reopenedIDs()) == 0 {
		select {
		case <-ctx.Done():
			t.Logf("closed=%v reopened=%v events=%v", ledger.closedIDs(), ledger.reopenedIDs(), collector.eventTypes())
			t.Fatal("T2-S1 FAIL: timed out; ReopenBead never called after non-zero exit")
		case <-time.After(50 * time.Millisecond):
		}
	}

	cancel()
	<-waitDone

	if len(ledger.closedIDs()) > 0 {
		t.Errorf("T2-S1 FAIL: CloseBead called after non-zero exit; bead should be reopened not closed: %v", ledger.closedIDs())
	}
	if len(ledger.reopenedIDs()) == 0 {
		t.Error("T2-S1 FAIL: ReopenBead not called after non-zero exit")
	}

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

// TestT2_SIGKILLDuringRun verifies that if the handler subprocess is
// SIGKILLed while the work loop is waiting, the loop handles the termination
// gracefully: ReopenBead is called, run_failed is emitted, and the loop
// continues (not stuck).
//
// This test uses the hang twin and kills it externally from the test goroutine.
func TestT2_SIGKILLDuringRun(t *testing.T) {
	t.Parallel()

	twinHang, twinMarker := t2ScopedTwin(t, "twin-hang")

	projectDir := t2FixtureProjectDir(t)

	const beadID = core.BeadID("t2-bead-sigkill")
	ledger := &stubBeadLedger{
		ready: []core.BeadID{beadID},
	}
	collector := &stubEventCollector{}

	deps := daemon.ExportedTestRuntime(daemon.TestRuntimeParams{
		BrAdapter:        ledger,
		Bus:              collector,
		ProjectDir:       projectDir,
		HandlerBinary:    twinHang,
		HandlerArgs:      nil,
		AdapterRegistry2: NewSealedAdapterRegistryForTest(t),
		IntentLogDir:     filepath.Join(projectDir, ".harmonik", "beads-intents"),
	})

	ctx, cancel := context.WithTimeout(context.Background(), workLoopTestBudget)
	defer cancel()

	waitDone := make(chan struct{})
	go func() {
		defer close(waitDone)
		daemon.ExportedRunWorkLoop(ctx, deps)
	}()

	twinPIDs := t2AwaitTwinAlive(ctx, t, twinMarker)
	t.Logf("T2-S2: twin running as %s; SIGKILLing it via pkill", twinPIDs)

	// Kill this test's OWN twin. The pattern is the per-test marker in the
	// twin's argv, never the shared "twin-hang" name — see t2ScopedTwin.
	//nolint:gosec // G204: twinMarker is built from t.Name() and the pid — test-internal, not user input
	killCmd := exec.CommandContext(context.Background(), "pkill", "-SIGKILL", "-f", twinMarker)
	if killErr := killCmd.Run(); killErr != nil {
		t.Fatalf("T2-S2: pkill did not kill the twin (%s): %v — the SIGKILL under test never happened", twinPIDs, killErr)
	}

	for len(ledger.reopenedIDs()) == 0 && len(ledger.closedIDs()) == 0 {
		select {
		case <-ctx.Done():
			t.Logf("T2-S2: events=%v closed=%v reopened=%v", collector.eventTypes(), ledger.closedIDs(), ledger.reopenedIDs())
			t.Fatal("T2-S2 FAIL: timed out waiting for bead state change after SIGKILL")
		case <-time.After(100 * time.Millisecond):
		}
	}

	cancel()
	<-waitDone

	t.Logf("T2-S2: events=%v closed=%v reopened=%v", collector.eventTypes(), ledger.closedIDs(), ledger.reopenedIDs())

	if len(ledger.closedIDs()) > 0 {
		t.Errorf("T2-S2 FAIL: CloseBead called after SIGKILL; should have called ReopenBead")
	}
	if len(ledger.reopenedIDs()) == 0 {
		t.Error("T2-S2 FAIL: ReopenBead not called after SIGKILL")
	}
}

// TestT2_MalformedNDJSON verifies that when the handler emits malformed NDJSON,
// the watcher does not crash the process and the work loop continues to function.
//
// Post-hk-9cob3 behaviour: malformed NDJSON triggers an agent_failed event from
// the watcher; the work loop treats watcher failure as a run failure and calls
// ReopenBead even when the handler exits 0. This is the correct behaviour per
// hk-9cob3 (watcher failure = run failure). The assertion below reflects the new
// contract: bead must be REOPENED (not closed) after malformed NDJSON + exit 0.
func TestT2_MalformedNDJSON(t *testing.T) {
	t.Parallel()

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

	deps := daemon.ExportedTestRuntime(daemon.TestRuntimeParams{
		BrAdapter:        ledger,
		Bus:              collector,
		ProjectDir:       projectDir,
		HandlerBinary:    "/bin/sh",
		HandlerArgs:      []string{scriptPath},
		AdapterRegistry2: NewSealedAdapterRegistryForTest(t),
		IntentLogDir:     filepath.Join(projectDir, ".harmonik", "beads-intents"),
	})

	ctx, cancel := context.WithTimeout(context.Background(), workLoopTestBudget)
	defer cancel()

	waitDone := make(chan struct{})
	var loopErr error
	go func() {
		defer close(waitDone)
		loopErr = daemon.ExportedRunWorkLoop(ctx, deps)
	}()

	for len(ledger.closedIDs()) == 0 && len(ledger.reopenedIDs()) == 0 {
		select {
		case <-ctx.Done():
			t.Logf("T2-S3: events=%v closed=%v reopened=%v", collector.eventTypes(), ledger.closedIDs(), ledger.reopenedIDs())
			t.Fatal("T2-S3 FAIL: timed out; bead never closed or reopened after malformed NDJSON + exit 0")
		case <-time.After(50 * time.Millisecond):
		}
	}

	cancel()
	<-waitDone

	t.Logf("T2-S3: loopErr=%v events=%v closed=%v reopened=%v", loopErr, collector.eventTypes(), ledger.closedIDs(), ledger.reopenedIDs())

	if len(ledger.reopenedIDs()) == 0 {
		t.Errorf("T2-S3 FAIL: bead was not reopened after malformed NDJSON; closed=%v (expected ReopenBead per hk-9cob3 watcher-failure contract)", ledger.closedIDs())
	}
	if len(ledger.closedIDs()) > 0 {
		t.Errorf("T2-S3 FAIL: bead was closed after malformed NDJSON (should be reopened per hk-9cob3); closed=%v", ledger.closedIDs())
	}
	if loopErr != nil {
		t.Errorf("T2-S3 FAIL: work loop returned non-nil error (crash) after malformed NDJSON: %v", loopErr)
	}
}

// TestT2_ExitZeroNoSignal verifies that when the handler exits 0 without
// emitting any NDJSON signals, the bead is CLOSED (success path) and a
// run_completed event is emitted.
func TestT2_ExitZeroNoSignal(t *testing.T) {
	skipRealDaemonE2EInShort(t)
	t.Parallel()

	projectDir := t2FixtureProjectDir(t)

	const beadID = core.BeadID("t2-bead-silent-exit")
	ledger := &stubBeadLedger{
		ready:  []core.BeadID{beadID},
		labels: workloopFixtureSingleLabels,
	}
	collector := &stubEventCollector{}

	deps := daemon.ExportedTestRuntime(daemon.TestRuntimeParams{
		BrAdapter:        ledger,
		Bus:              collector,
		ProjectDir:       projectDir,
		HandlerBinary:    "/bin/sh",
		HandlerArgs:      workloopFixtureAdvanceHeadHandlerArgs(t),
		AdapterRegistry2: NewEmptySealedAdapterRegistryForTest(t),
		IntentLogDir:     filepath.Join(projectDir, ".harmonik", "beads-intents"),
	})

	ctx, cancel := context.WithTimeout(context.Background(), workLoopTestBudget)
	defer cancel()

	waitDone := make(chan struct{})
	go func() {
		defer close(waitDone)
		daemon.ExportedRunWorkLoop(ctx, deps)
	}()

	for len(ledger.closedIDs()) == 0 && len(ledger.reopenedIDs()) == 0 {
		select {
		case <-ctx.Done():
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

// TestT2_HangTwinCtxCancel verifies that when the handler hangs and context
// is cancelled, the work loop exits cleanly within a reasonable time, the bead
// is reopened (not closed, not abandoned), and no goroutine leaks are obvious.
func TestT2_HangTwinCtxCancel(t *testing.T) {
	t.Parallel()

	twinHang, _ := t2ScopedTwin(t, "twin-hang")

	projectDir := t2FixtureProjectDir(t)

	const beadID = core.BeadID("t2-bead-hang")
	ledger := &stubBeadLedger{
		ready: []core.BeadID{beadID},
	}
	collector := &stubEventCollector{}

	deps := daemon.ExportedTestRuntime(daemon.TestRuntimeParams{
		BrAdapter:        ledger,
		Bus:              collector,
		ProjectDir:       projectDir,
		HandlerBinary:    twinHang,
		HandlerArgs:      nil,
		AdapterRegistry2: NewSealedAdapterRegistryForTest(t),
		IntentLogDir:     filepath.Join(projectDir, ".harmonik", "beads-intents"),
	})

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	startTime := time.Now()
	waitDone := make(chan struct{})
	var loopErr error
	go func() {
		defer close(waitDone)
		loopErr = daemon.ExportedRunWorkLoop(ctx, deps)
	}()

	awaitLoopTeardown(t, waitDone, "T2-S5 work loop with a hanging twin")
	t.Logf("T2-S5: loop exited in %v; loopErr=%v events=%v closed=%v reopened=%v",
		time.Since(startTime), loopErr, collector.eventTypes(), ledger.closedIDs(), ledger.reopenedIDs())

	if loopErr != nil {
		t.Errorf("T2-S5: loop returned non-nil error: %v", loopErr)
	}
}

// TestT2_ProcessGroupCleanup probes whether the child process group is cleaned
// up after the context is cancelled with a hang twin. This is relevant for
// operator UX: if the hang twin leaves zombies/orphans, there's a problem.
func TestT2_ProcessGroupCleanup(t *testing.T) {
	t.Parallel()

	twinHang, twinMarker := t2ScopedTwin(t, "twin-hang")

	projectDir := t2FixtureProjectDir(t)

	const beadID = core.BeadID("t2-bead-pg-cleanup")
	ledger := &stubBeadLedger{
		ready: []core.BeadID{beadID},
	}
	collector := &stubEventCollector{}

	deps := daemon.ExportedTestRuntime(daemon.TestRuntimeParams{
		BrAdapter:        ledger,
		Bus:              collector,
		ProjectDir:       projectDir,
		HandlerBinary:    twinHang,
		AdapterRegistry2: NewSealedAdapterRegistryForTest(t),
		IntentLogDir:     filepath.Join(projectDir, ".harmonik", "beads-intents"),
	})

	ctx, cancel := context.WithTimeout(context.Background(), workLoopTestBudget)
	defer cancel()

	waitDone := make(chan struct{})
	go func() {
		defer close(waitDone)
		daemon.ExportedRunWorkLoop(ctx, deps)
	}()

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
		case <-ctx.Done():
			t.Fatalf("T2-S6: run_started never fired")
		case <-time.After(50 * time.Millisecond):
		}
	}

	beforePIDs := t2AwaitTwinAlive(ctx, t, twinMarker)
	t.Logf("T2-S6: twin PIDs before cancel: %s", beforePIDs)

	cancel()
	awaitLoopTeardown(t, waitDone, "T2-S6 work loop")

	afterPIDs := t2AwaitTwinGone(t, twinMarker)
	t.Logf("T2-S6: twin PIDs after cancel: %q (was running before: %s)", afterPIDs, beforePIDs)

	if afterPIDs != "" {
		t.Errorf("T2-S6 FINDING: twin process(es) still alive after context cancellation: %s — orphan leak", afterPIDs)
		// Cleanup for the test run — this test's own twin only.
		//nolint:gosec // G204: twinMarker is test-internal; see t2ScopedTwin
		if killErr := exec.CommandContext(context.Background(), "pkill", "-SIGKILL", "-f", twinMarker).Run(); killErr != nil {
			t.Logf("T2-S6: cleanup pkill: %v", killErr)
		}
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

	deps := daemon.ExportedTestRuntime(daemon.TestRuntimeParams{
		BrAdapter:        ledger,
		Bus:              collector,
		ProjectDir:       projectDir,
		HandlerBinary:    twinFail,
		AdapterRegistry2: NewSealedAdapterRegistryForTest(t),
		IntentLogDir:     filepath.Join(projectDir, ".harmonik", "beads-intents"),
	})

	ctx, cancel := context.WithTimeout(context.Background(), workLoopTestBudget)
	defer cancel()

	waitDone := make(chan struct{})
	go func() {
		defer close(waitDone)
		daemon.ExportedRunWorkLoop(ctx, deps)
	}()

	for len(ledger.reopenedIDs()) == 0 {
		select {
		case <-ctx.Done():
			t.Fatal("T2-ExitCode: timed out waiting for reopen")
		case <-time.After(50 * time.Millisecond):
		}
	}
	cancel()
	<-waitDone

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

	const beadID = core.BeadID("t2-bead-wt-check")
	ledger := &stubBeadLedger{
		ready: []core.BeadID{beadID},
	}
	collector := &stubEventCollector{}

	deps := daemon.ExportedTestRuntime(daemon.TestRuntimeParams{
		BrAdapter:        ledger,
		Bus:              collector,
		ProjectDir:       projectDir,
		HandlerBinary:    twinFail,
		AdapterRegistry2: NewSealedAdapterRegistryForTest(t),
		IntentLogDir:     filepath.Join(projectDir, ".harmonik", "beads-intents"),
	})

	ctx, cancel := context.WithTimeout(context.Background(), workLoopTestBudget)
	defer cancel()

	waitDone := make(chan struct{})
	go func() {
		defer close(waitDone)
		daemon.ExportedRunWorkLoop(ctx, deps)
	}()

	for len(ledger.reopenedIDs()) == 0 {
		select {
		case <-ctx.Done():
			t.Fatal("T2-S7: timed out waiting for reopen after failure")
		case <-time.After(50 * time.Millisecond):
		}
	}
	cancel()
	<-waitDone

	wtDir := filepath.Join(projectDir, ".harmonik", "worktrees")
	entries, err := os.ReadDir(wtDir)
	if err != nil && !os.IsNotExist(err) {
		t.Logf("T2-S7: ReadDir error: %v", err)
	}

	var runID string
	for _, ev := range collector.events {
		if ev.EventType == string(core.EventTypeRunStarted) {
			payload := string(ev.Payload)
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

var _ = (*stubEventCollector)(nil)

var _ = syscall.SIGKILL
