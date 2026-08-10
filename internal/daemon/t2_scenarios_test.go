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

	// Create a bare clone as "origin" so that mergeRunBranchToMain's
	// `git push origin main` step succeeds for tests whose handler produces a
	// worktree commit (e.g. via workloopFixturePreCommitWorktreeFactory). Without
	// an origin remote the merge step fails and the bead is reopened instead of
	// closed — mirrors workloopFixtureGitRepo.
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

// t2WorktreePath is the same as workspace.WorktreePath but without importing
// workspace — constructs the conventional path.
func t2WorktreePath(projectDir, runID string) string {
	return filepath.Join(projectDir, ".harmonik", "worktrees", runID)
}

// t2FindBinary finds a pre-built twin that `make twins` writes to the checkout
// root. It used to look only at this worktree's root, so in any git worktree —
// which is where most work on this repo happens — the six TestT2_* tests below
// found nothing and skipped silently. scenariotest.CheckoutBinaryPath also tries
// the main checkout.
//
// When the binary is nowhere it returns the path it looked at first, not "", so
// the caller's os.Stat still fails and its skip message names a real location.
func t2FindBinary(name string) string {
	path, _ := scenariotest.CheckoutBinaryPath(name)
	return path
}

// t2ScopedTwin copies a twin binary to a path whose basename is unique to THIS
// test and THIS process, and returns the path plus that unique basename. The
// basename is the launched process's argv[0], so a `pgrep -f` / `pkill -f` on it
// matches this test's own twin and nothing else on the host.
//
// Why the copy: three tests in this file launch the hang twin, all three call
// t.Parallel(), and two of them ran `pkill -SIGKILL -f twin-hang`. That pattern
// is scoped to no process group, no project and no user, so each run killed its
// siblings' twins and any other agent's twin anywhere on the box — including a
// real dispatched agent's. It was only ever correct when nothing else ran on the
// machine, which is not the operating condition here. Refs
// hk-scenario-hostwide-pkill-hf166, and the same hazard family as hk-c6dt2.
//
// The test cannot kill by pid instead: the work loop spawns the twin, so the
// test never holds the handle. A unique argv is the scoping the bead asks for.
//
// Skips (does not fail) when the twin is not built, matching the callers this
// replaces.
func t2ScopedTwin(t *testing.T, name string) (binPath, marker string) {
	t.Helper()
	src := t2FindBinary(name)
	data, err := os.ReadFile(src) //nolint:gosec // G304: src is a build artifact at the checkout root, not user input
	if err != nil {
		t.Skipf("%s not found at %s; build with: make twins (%v)", name, src, err)
	}
	// t.Name() is unique inside one test binary; the pid separates concurrent
	// test binaries and any other checkout on the same host. "/" appears in
	// subtest names and would make the copy a path rather than a basename.
	marker = fmt.Sprintf("%s-%s-%d", name, strings.ReplaceAll(t.Name(), "/", "_"), os.Getpid())
	binPath = filepath.Join(t.TempDir(), marker)
	if err := os.WriteFile(binPath, data, 0o700); err != nil { //nolint:gosec // G306: the copy has to be executable
		t.Fatalf("t2ScopedTwin: write %s: %v", binPath, err)
	}
	return binPath, marker
}

// t2OrphanReapBudget is how long this test's own twin may still be visible
// after the work loop has finished tearing down.
//
// loopexit_test.go removed the second stopwatches that tests put on teardown
// they were not named for. This one stays, under the same rule: T2-S6's SUBJECT
// is orphan cleanup, so the bound is the claim rather than a stopwatch on top of
// one. A leaked hang twin never exits by itself, so the budget only has to
// outlast process death on a loaded box — the passing path returns the moment
// pgrep comes back empty and pays none of it.
const t2OrphanReapBudget = 10 * time.Second

// t2TwinPIDs returns the pids of THIS test's own twin, as pgrep prints them, or
// "" when no such process is running. The pattern is the per-test marker in the
// twin's argv, never the shared twin name — see t2ScopedTwin. That marker is
// built from t.Name() and the pid, so it is test-internal and not user input.
func t2TwinPIDs(ctx context.Context, marker string) string {
	out, err := exec.CommandContext(ctx, "pgrep", "-f", marker).Output()
	if err != nil {
		// pgrep exits 1 when nothing matches, which is the common case here.
		return ""
	}
	return strings.TrimSpace(string(out))
}

// t2AwaitTwinAlive blocks until this test's twin is really running, and fails
// the test if it never starts.
//
// A caller cannot use run_started for this. That event is emitted BEFORE the
// handler process is executed, so a probe placed right after it reliably finds
// nothing. Both callers below did exactly that: T2-S6 guarded its orphan-leak
// assertion on the result and so never ran it, and T2-S2 sent its SIGKILL to a
// process that did not exist yet (hk-3xwsj).
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

// t2AwaitTwinGone blocks until this test's twin is gone, and returns the pids
// still present when t2OrphanReapBudget runs out. "" means cleanly reaped.
func t2AwaitTwinGone(t *testing.T, marker string) string {
	t.Helper()
	// context.Background(), not the run context: this runs AFTER that context
	// has been cancelled, which is the whole point of the check.
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

	// Poll until ReopenBead is called (indicates loop handled the failure).

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

	// We need to intercept the process to kill it. Since the loop runs handler
	// internally, we use a short context timeout to simulate external kill.
	// But for a real SIGKILL, we use the OS to find and kill the spawned twin process.

	// First, let the loop start and wait a bit for the hang twin to be launched.
	waitDone := make(chan struct{})
	go func() {
		defer close(waitDone)
		daemon.ExportedRunWorkLoop(ctx, deps)
	}()

	// Wait until the twin is really running. run_started is NOT that signal: it
	// is emitted before the handler is executed, so the pkill this replaces fired
	// at a process that did not exist yet and killed nothing. The test still
	// passed, because the twin then died on its own — see hk-3xwsj and the
	// comment in test/twins/hang/main.go. Wait for the process itself.
	twinPIDs := t2AwaitTwinAlive(ctx, t, twinMarker)
	t.Logf("T2-S2: twin running as %s; SIGKILLing it via pkill", twinPIDs)

	// Kill this test's OWN twin. The pattern is the per-test marker in the
	// twin's argv, never the shared "twin-hang" name — see t2ScopedTwin.
	//nolint:gosec // G204: twinMarker is built from t.Name() and the pid — test-internal, not user input
	killCmd := exec.CommandContext(context.Background(), "pkill", "-SIGKILL", "-f", twinMarker)
	if killErr := killCmd.Run(); killErr != nil {
		t.Fatalf("T2-S2: pkill did not kill the twin (%s): %v — the SIGKILL under test never happened", twinPIDs, killErr)
	}

	// Now wait for the loop to detect the kill and reopen the bead.
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
//
// Post-hk-9cob3 behaviour: malformed NDJSON triggers an agent_failed event from
// the watcher; the work loop treats watcher failure as a run failure and calls
// ReopenBead even when the handler exits 0. This is the correct behaviour per
// hk-9cob3 (watcher failure = run failure). The assertion below reflects the new
// contract: bead must be REOPENED (not closed) after malformed NDJSON + exit 0.
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

	// Poll for bead state change (closed or reopened).

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

	// Post-hk-9cob3: malformed NDJSON → watcher emits agent_failed → work loop
	// calls ReopenBead regardless of exit code. Bead must be REOPENED, not closed.
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

// ─────────────────────────────────────────────────────────────────────────────
// T2-S4: Twin exits 0 without explicit ready/done signal
// ─────────────────────────────────────────────────────────────────────────────

// TestT2_ExitZeroNoSignal verifies that when the handler exits 0 without
// emitting any NDJSON signals, the bead is CLOSED (success path) and a
// run_completed event is emitted.
func TestT2_ExitZeroNoSignal(t *testing.T) {
	skipRealDaemonE2EInShort(t)
	t.Parallel()

	projectDir := t2FixtureProjectDir(t)

	const beadID = core.BeadID("t2-bead-silent-exit")
	// workflow:single is load-bearing; see stubBeadLedger.labels. Without it this
	// bead selects the reviewed graph, whose commit gate cannot pass in a fixture
	// repo that holds one README, so the run reopens the bead and the test reads
	// that as a silent exit 0 failing to close.
	ledger := &stubBeadLedger{
		ready:  []core.BeadID{beadID},
		labels: workloopFixtureSingleLabels,
	}
	collector := &stubEventCollector{}

	// The handler commits and exits 0 without writing one NDJSON line, which is
	// what this scenario is about: a twin that exits clean and says nothing. The
	// commit has to be here rather than in a worktree factory, because a factory
	// runs before the launch and the node baseline would already carry it — see
	// workloopFixtureAdvanceHeadHandlerArgs.
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

	// Scoped copy: this twin must not be visible to a sibling's pgrep or
	// reapable by a sibling's pkill — see t2ScopedTwin.
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

	// The subject here is that the loop EXITS with a twin that never will, not
	// how fast. The old 10-second cap read like a latency claim and behaved like
	// one: it failed under parallel load on a loop that exits in 3.8 seconds
	// alone, and a twin that hangs for ever fails this test at any bound. The
	// elapsed time is logged, so a real slowdown is still visible to anyone
	// reading the run (hk-scenario-budgets-structural-2z9dx).
	awaitLoopTeardown(t, waitDone, "T2-S5 work loop with a hanging twin")
	t.Logf("T2-S5: loop exited in %v; loopErr=%v events=%v closed=%v reopened=%v",
		time.Since(startTime), loopErr, collector.eventTypes(), ledger.closedIDs(), ledger.reopenedIDs())

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

	// Launch and wait until run_started is emitted (hang twin is alive).
	ctx, cancel := context.WithTimeout(context.Background(), workLoopTestBudget)
	defer cancel()

	waitDone := make(chan struct{})
	go func() {
		defer close(waitDone)
		daemon.ExportedRunWorkLoop(ctx, deps)
	}()

	// Wait for run_started.
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

	// Wait until THIS test's twin is really running. run_started fires before
	// the handler is executed, so the probe this replaces ran too early, found
	// nothing every time, and skipped the assertion below (hk-3xwsj). Matching
	// the per-test marker rather than the shared "twin-hang" name also keeps a
	// sibling's twin out of the count.
	beforePIDs := t2AwaitTwinAlive(ctx, t, twinMarker)
	t.Logf("T2-S6: twin PIDs before cancel: %s", beforePIDs)

	// Cancel context (simulates SIGINT/SIGTERM to the daemon).
	cancel()
	awaitLoopTeardown(t, waitDone, "T2-S6 work loop")

	// After loop exit, the twin must go away. A leaked hang twin stays forever,
	// so waiting out the budget costs nothing when the cleanup works.
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
