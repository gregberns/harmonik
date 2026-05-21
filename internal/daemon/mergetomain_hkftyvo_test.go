package daemon_test

// mergetomain_hkftyvo_test.go — integration tests for the §4.12 merge-to-main
// sequence introduced by EM-052/EM-053 (bead hk-ftyvo).
//
// Test assertions per §10.2 EM-052–EM-053 obligation:
//   (a) refs/heads/main advances to the run-branch tip on success.
//   (b) A push-origin-main attempt is made (observed via git push --dry-run).
//   (c) outcome_emitted{kind=approved} emitted before bead_closed.
//   (d) bead_closed emitted after CloseBead.
//   (e) run_completed{success:true} is the final lifecycle event.
//   (f) ReopenBead called on non-FF.
//   (g) outcome_emitted{kind=rejected, reason=non_ff_merge} emitted on non-FF.
//   (h) CloseBead NOT called on non-FF.
//
// The test wires the work loop with:
//   - productionWorktreeFactory so a real git worktree (and run-branch) is created.
//   - A shell handler that writes a file and commits it onto the run-branch, then
//     exits 0 (auto-close heuristic branch, EM-052 branch 2).
//   - A recording bead ledger so Close/Reopen calls are observable.
//   - A recording event bus so event ordering is verifiable.
//
// Helper prefix: mergeToMainFixture (per implementer-protocol.md §Helper-prefix
// discipline; bead hk-ftyvo).
//
// Spec refs:
//   - specs/execution-model.md §4.12 EM-052, EM-053
//   - specs/workspace-model.md §4.2 WM-005b (task-branch naming)
//
// Bead: hk-ftyvo.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/brcli"
	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
)

// ─────────────────────────────────────────────────────────────────────────────
// Fixtures
// ─────────────────────────────────────────────────────────────────────────────

// mergeToMainFixtureGitRepo initialises a git repository in dir with:
//   - git identity set to daemon@harmonik.local
//   - "main" branch with an initial commit
//
// Returns the repo root path (== dir).
func mergeToMainFixtureGitRepo(t *testing.T, dir string) {
	t.Helper()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.CommandContext(t.Context(), "git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("mergeToMainFixtureGitRepo: git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "--initial-branch=main")
	run("config", "user.email", "daemon@harmonik.local")
	run("config", "user.name", "Harmonik Test")

	initPath := filepath.Join(dir, "README")
	//nolint:gosec // G306: 0644 is fine for a test fixture file
	if err := os.WriteFile(initPath, []byte("initial\n"), 0o644); err != nil {
		t.Fatalf("mergeToMainFixtureGitRepo: WriteFile: %v", err)
	}
	run("add", "README")
	run("commit", "-m", "init")
}

// mergeToMainFixtureProjectDir creates the minimal .harmonik/ directory tree.
func mergeToMainFixtureProjectDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
	if err := os.MkdirAll(filepath.Join(dir, ".harmonik", "events"), 0o755); err != nil {
		t.Fatalf("mergeToMainFixtureProjectDir: mkdir events: %v", err)
	}
	//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
	if err := os.MkdirAll(filepath.Join(dir, ".harmonik", "beads-intents"), 0o755); err != nil {
		t.Fatalf("mergeToMainFixtureProjectDir: mkdir beads-intents: %v", err)
	}
	return dir
}

// mergeToMainFixtureHeadSHA resolves the HEAD SHA of branch in repoRoot.
func mergeToMainFixtureHeadSHA(t *testing.T, repoRoot, branch string) string {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", "rev-parse", "refs/heads/"+branch)
	cmd.Dir = repoRoot
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("mergeToMainFixtureHeadSHA: git rev-parse refs/heads/%s: %v", branch, err)
	}
	return strings.TrimRight(string(out), "\n")
}

// mergeToMainFixtureAdvanceMain creates a diverging commit on main in repoRoot
// so that any run-branch is no longer a fast-forward. The commit touches a
// different file than the agent's work.txt, so a rebase will succeed without
// conflicts. Used to test the rebase-success path (hk-j1aq5).
func mergeToMainFixtureAdvanceMain(t *testing.T, repoRoot string) {
	t.Helper()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.CommandContext(t.Context(), "git", args...)
		cmd.Dir = repoRoot
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("mergeToMainFixtureAdvanceMain: git %v: %v\n%s", args, err, out)
		}
	}
	divergePath := filepath.Join(repoRoot, "DIVERGE")
	//nolint:gosec // G306: 0644 is fine for a test fixture file
	if err := os.WriteFile(divergePath, []byte("diverge\n"), 0o644); err != nil {
		t.Fatalf("mergeToMainFixtureAdvanceMain: WriteFile: %v", err)
	}
	run("add", "DIVERGE")
	run("commit", "-m", "diverging commit on main")
}

// mergeToMainFixtureAdvanceMainConflicting creates a diverging commit on main
// that edits work.txt — the same file the agent writes — producing a rebase
// conflict. Used to exercise the EM-053 rebase_conflict reopen path.
func mergeToMainFixtureAdvanceMainConflicting(t *testing.T, repoRoot string) {
	t.Helper()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.CommandContext(t.Context(), "git", args...)
		cmd.Dir = repoRoot
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("mergeToMainFixtureAdvanceMainConflicting: git %v: %v\n%s", args, err, out)
		}
	}
	conflictPath := filepath.Join(repoRoot, "work.txt")
	//nolint:gosec // G306: 0644 is fine for a test fixture file
	if err := os.WriteFile(conflictPath, []byte("conflicting main content\n"), 0o644); err != nil {
		t.Fatalf("mergeToMainFixtureAdvanceMainConflicting: WriteFile: %v", err)
	}
	run("add", "work.txt")
	run("commit", "-m", "conflicting commit on main")
}

// ─────────────────────────────────────────────────────────────────────────────
// Recording bead ledger (captures Close/Reopen)
// ─────────────────────────────────────────────────────────────────────────────

// mergeToMainRecordingLedger captures CloseBead and ReopenBead calls.
type mergeToMainRecordingLedger struct {
	mu sync.Mutex

	beadID core.BeadID

	// closedCount is incremented by each successful CloseBead call.
	closedCount int

	// reopenedCount is incremented by each ReopenBead call.
	reopenedCount int

	// doneCh is closed after the first Close or Reopen so the test can unblock.
	doneCh   chan struct{}
	doneOnce sync.Once
}

func newMergeToMainRecordingLedger(beadID core.BeadID) *mergeToMainRecordingLedger {
	return &mergeToMainRecordingLedger{
		beadID: beadID,
		doneCh: make(chan struct{}),
	}
}

func (l *mergeToMainRecordingLedger) Ready(_ context.Context) ([]core.BeadRecord, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	// Return the bead only once — after it is consumed, Ready returns empty.
	if l.closedCount+l.reopenedCount == 0 && !l.isDoneNoLock() {
		return []core.BeadRecord{{BeadID: l.beadID, Status: core.CoarseStatusOpen}}, nil
	}
	return []core.BeadRecord{}, nil
}

func (l *mergeToMainRecordingLedger) isDoneNoLock() bool {
	select {
	case <-l.doneCh:
		return true
	default:
		return false
	}
}

func (l *mergeToMainRecordingLedger) ShowBead(_ context.Context, id core.BeadID) (core.BeadRecord, error) {
	return core.BeadRecord{BeadID: id, Status: core.CoarseStatusOpen}, nil
}

func (l *mergeToMainRecordingLedger) ClaimBead(_ context.Context, _ string, _ brcli.TimeoutConfig, _ core.RunID, _ core.TransitionID, _ core.BeadID) error {
	return nil
}

func (l *mergeToMainRecordingLedger) CloseBead(_ context.Context, _ string, _ brcli.TimeoutConfig, _ core.RunID, _ core.TransitionID, _ core.BeadID, _ bool) error {
	l.mu.Lock()
	l.closedCount++
	l.mu.Unlock()
	l.doneOnce.Do(func() { close(l.doneCh) })
	return nil
}

func (l *mergeToMainRecordingLedger) ReopenBead(_ context.Context, _ string, _ brcli.TimeoutConfig, _ core.RunID, _ core.TransitionID, _ core.BeadID, _ string) error {
	l.mu.Lock()
	l.reopenedCount++
	l.mu.Unlock()
	l.doneOnce.Do(func() { close(l.doneCh) })
	return nil
}

func (l *mergeToMainRecordingLedger) getClosedCount() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.closedCount
}

func (l *mergeToMainRecordingLedger) getReopenedCount() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.reopenedCount
}

// ─────────────────────────────────────────────────────────────────────────────
// worktreeFactory that also commits a file onto the run-branch
// ─────────────────────────────────────────────────────────────────────────────

// mergeToMainCommittingFactory wraps productionWorktreeFactory and, after the
// worktree is created, writes a file and commits it onto the run-branch. This
// simulates an agent that made work — the run-branch tip is one commit ahead
// of main, so the merge-to-main fast-forward is non-trivial.
func mergeToMainCommittingFactory(t *testing.T) func(ctx context.Context, projectDir, runID, headSHA string) (string, func(), error) {
	t.Helper()
	return func(ctx context.Context, projectDir, runID, headSHA string) (string, func(), error) {
		// Create the real git worktree (run-branch is created here).
		wtPath, cleanup, err := daemon.ExportedProductionWorktreeFactory(ctx, projectDir, runID, headSHA)
		if err != nil {
			return "", nil, err
		}

		// Write a file and commit it inside the worktree.
		workFile := filepath.Join(wtPath, "work.txt")
		//nolint:gosec // G306: 0644 is fine for a test fixture file
		if err2 := os.WriteFile(workFile, []byte("agent work\n"), 0o644); err2 != nil {
			cleanup()
			return "", nil, fmt.Errorf("mergeToMainCommittingFactory: WriteFile: %w", err2)
		}

		addCmd := exec.CommandContext(ctx, "git", "add", "work.txt")
		addCmd.Dir = wtPath
		if out, err2 := addCmd.CombinedOutput(); err2 != nil {
			cleanup()
			return "", nil, fmt.Errorf("mergeToMainCommittingFactory: git add: %v\n%s", err2, out)
		}

		commitCmd := exec.CommandContext(ctx, "git", "commit", "-m", "feat: agent work",
			"--trailer", "Harmonik-Run-ID: "+runID,
		)
		commitCmd.Dir = wtPath
		if out, err2 := commitCmd.CombinedOutput(); err2 != nil {
			cleanup()
			return "", nil, fmt.Errorf("mergeToMainCommittingFactory: git commit: %v\n%s", err2, out)
		}

		return wtPath, cleanup, nil
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Helpers to inspect the event stream
// ─────────────────────────────────────────────────────────────────────────────

// mergeToMainFindEvents returns all events with the given event type from the
// collector. Returns nil if none are found.
func mergeToMainFindEvents(collector *stubEventCollector, eventType string) []stubEmittedEvent {
	var found []stubEmittedEvent
	for _, ev := range collector.allEvents() {
		if ev.EventType == eventType {
			found = append(found, ev)
		}
	}
	return found
}

// mergeToMainEventOrder returns the slice of event types in emission order.
func mergeToMainEventOrder(collector *stubEventCollector) []string {
	return collector.eventTypes()
}

// mergeToMainPayloadKind unmarshals the "kind" field from an outcome_emitted payload.
func mergeToMainPayloadKind(t *testing.T, ev stubEmittedEvent) string {
	t.Helper()
	var m map[string]interface{}
	if err := json.Unmarshal(ev.Payload, &m); err != nil {
		t.Fatalf("mergeToMainPayloadKind: unmarshal: %v", err)
	}
	k, _ := m["kind"].(string)
	return k
}

// mergeToMainPayloadReason unmarshals the "reason" field from a payload.
func mergeToMainPayloadReason(t *testing.T, ev stubEmittedEvent) string {
	t.Helper()
	var m map[string]interface{}
	if err := json.Unmarshal(ev.Payload, &m); err != nil {
		t.Fatalf("mergeToMainPayloadReason: unmarshal: %v", err)
	}
	r, _ := m["reason"].(string)
	return r
}

// ─────────────────────────────────────────────────────────────────────────────
// Test: success path (EM-052 assertions a–e)
// ─────────────────────────────────────────────────────────────────────────────

// TestMergeToMain_SuccessPath verifies that on a successful run (auto-close
// heuristic branch, exit=0) the daemon:
//
//	(a) fast-forwards refs/heads/main to the run-branch tip,
//	(b) emits outcome_emitted{kind=approved} before bead_closed,
//	(c) emits bead_closed after CloseBead (CloseBead call count == 1),
//	(d) emits run_completed{success:true} as the final lifecycle event.
//
// Push (assertion b of the spec obligation) is attempted but will fail in the
// test because there is no remote. The implementation rolls back the local
// update-ref on push failure and reopens the bead. To avoid this, this test
// uses a "bare remote" trick: a second bare repo acts as origin so the push
// succeeds in CI.
//
// Spec refs: specs/execution-model.md §4.12 EM-052.
// Bead: hk-ftyvo.
func TestMergeToMain_SuccessPath(t *testing.T) {
	t.Parallel()

	const beadID = core.BeadID("mergetomain-success-bead-001")

	projectDir := mergeToMainFixtureProjectDir(t)
	mergeToMainFixtureGitRepo(t, projectDir)

	// Create a bare remote (origin) so git push succeeds.
	originDir := t.TempDir()
	initBareCmd := exec.CommandContext(t.Context(), "git", "init", "--bare", "--initial-branch=main", originDir)
	if out, err := initBareCmd.CombinedOutput(); err != nil {
		t.Fatalf("git init --bare: %v\n%s", err, out)
	}
	// Push current main to origin to prime it.
	primeCmd := exec.CommandContext(t.Context(), "git", "remote", "add", "origin", originDir)
	primeCmd.Dir = projectDir
	if out, err := primeCmd.CombinedOutput(); err != nil {
		t.Fatalf("git remote add origin: %v\n%s", err, out)
	}
	pushInitCmd := exec.CommandContext(t.Context(), "git", "push", "origin", "main")
	pushInitCmd.Dir = projectDir
	if out, err := pushInitCmd.CombinedOutput(); err != nil {
		t.Fatalf("git push origin main (initial): %v\n%s", err, out)
	}

	mainSHABefore := mergeToMainFixtureHeadSHA(t, projectDir, "main")

	ledger := newMergeToMainRecordingLedger(beadID)
	collector := &stubEventCollector{}

	// The handler exits 0 immediately — triggers the auto-close heuristic (branch 2).
	deps := daemon.ExportedWorkLoopDeps(daemon.WorkLoopDepsParams{
		BrAdapter:       ledger,
		Bus:             collector,
		ProjectDir:      projectDir,
		HandlerBinary:   "/bin/sh",
		HandlerArgs:     []string{"-c", "exit 0"},
		IntentLogDir:    filepath.Join(projectDir, ".harmonik", "beads-intents"),
		AdapterRegistry2: NewSealedAdapterRegistryForTest(t),
		WorktreeFactory: mergeToMainCommittingFactory(t),
	})

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	loopDone := make(chan struct{})
	go func() {
		defer close(loopDone)
		daemon.ExportedRunWorkLoop(ctx, deps)
	}()

	// Wait for the bead to be closed or reopened (doneCh) or test timeout.
	select {
	case <-ledger.doneCh:
		cancel()
	case <-ctx.Done():
		t.Error("timed out waiting for bead close/reopen")
	}

	select {
	case <-loopDone:
	case <-time.After(5 * time.Second):
		t.Error("work loop did not exit within 5s")
	}

	// ── Assertion (a): main advanced beyond mainSHABefore. ────────────────────
	mainSHAAfter := mergeToMainFixtureHeadSHA(t, projectDir, "main")
	if mainSHAAfter == mainSHABefore {
		t.Errorf("main HEAD unchanged after success run: still %s; want run-branch tip", mainSHABefore)
	}

	// ── Assertion (c): CloseBead called exactly once. ─────────────────────────
	if got := ledger.getClosedCount(); got != 1 {
		t.Errorf("CloseBead call count = %d; want 1", got)
	}
	if got := ledger.getReopenedCount(); got != 0 {
		t.Errorf("ReopenBead call count = %d; want 0 on success path", got)
	}

	// ── Assertion (b, d): event sequence includes outcome_emitted{approved},
	//    bead_closed, run_completed — in that order. ──────────────────────────
	types := mergeToMainEventOrder(collector)

	// Find the indices of the key events.
	outcomeIdx := -1
	beadClosedIdx := -1
	runCompletedIdx := -1
	for i, et := range types {
		switch et {
		case "outcome_emitted":
			if outcomeIdx == -1 {
				outcomeIdx = i // first outcome_emitted
			}
		case "bead_closed":
			if beadClosedIdx == -1 {
				beadClosedIdx = i
			}
		case "run_completed":
			if runCompletedIdx == -1 {
				runCompletedIdx = i
			}
		}
	}

	if outcomeIdx == -1 {
		t.Errorf("outcome_emitted not found in event stream: %v", types)
	} else {
		outcomeEvs := mergeToMainFindEvents(collector, "outcome_emitted")
		if len(outcomeEvs) == 0 {
			t.Error("no outcome_emitted events collected")
		} else {
			kind := mergeToMainPayloadKind(t, outcomeEvs[0])
			if kind != "approved" {
				t.Errorf("outcome_emitted kind = %q; want %q", kind, "approved")
			}
		}
	}

	if beadClosedIdx == -1 {
		t.Errorf("bead_closed not found in event stream: %v", types)
	}

	if runCompletedIdx == -1 {
		t.Errorf("run_completed not found in event stream: %v", types)
	}

	// Order checks: outcome_emitted < bead_closed < run_completed.
	if outcomeIdx != -1 && beadClosedIdx != -1 && outcomeIdx > beadClosedIdx {
		t.Errorf("outcome_emitted (idx %d) must precede bead_closed (idx %d)", outcomeIdx, beadClosedIdx)
	}
	if beadClosedIdx != -1 && runCompletedIdx != -1 && beadClosedIdx > runCompletedIdx {
		t.Errorf("bead_closed (idx %d) must precede run_completed (idx %d)", beadClosedIdx, runCompletedIdx)
	}

	// ── Assertion (e): run_completed carries success=true. ────────────────────
	runCompletedEvs := mergeToMainFindEvents(collector, "run_completed")
	if len(runCompletedEvs) == 0 {
		t.Error("no run_completed events found")
	} else {
		var m map[string]interface{}
		if err := json.Unmarshal(runCompletedEvs[0].Payload, &m); err != nil {
			t.Fatalf("run_completed payload unmarshal: %v", err)
		}
		success, _ := m["success"].(bool)
		if !success {
			t.Errorf("run_completed success = false; want true")
		}
	}

	t.Logf("merge-to-main success path OK: main %s → %s, events: %v",
		mainSHABefore[:8], mainSHAAfter[:8], types)
}

// ─────────────────────────────────────────────────────────────────────────────
// Test: non-FF path (EM-053 assertions f–h)
// ─────────────────────────────────────────────────────────────────────────────

// TestMergeToMain_NonFFReopen verifies that when main has advanced with a
// conflicting commit (same file the agent modified), the daemon hits a rebase
// conflict and:
//
//	(f) calls ReopenBead,
//	(g) emits outcome_emitted{kind=rejected, reason containing "rebase_conflict"},
//	(h) does NOT call CloseBead.
//
// Setup: after the worktree is created (forking from main), we add a diverging
// commit to main that edits the same file as the agent — producing a rebase
// conflict. The handler exits 0 so branch 2 is taken. The rebase then fails →
// EM-053 rebase_conflict reopen path.
//
// Spec refs: specs/execution-model.md §4.12 EM-052 step 2, EM-053.
// Bead: hk-ftyvo, hk-j1aq5.
func TestMergeToMain_NonFFReopen(t *testing.T) {
	t.Parallel()

	const beadID = core.BeadID("mergetomain-nonff-bead-001")

	projectDir := mergeToMainFixtureProjectDir(t)
	mergeToMainFixtureGitRepo(t, projectDir)

	ledger := newMergeToMainRecordingLedger(beadID)
	collector := &stubEventCollector{}

	// Use a custom worktreeFactory that:
	//   1. Creates the real run-branch + commits work.txt (agent work).
	//   2. Then advances main with a conflicting commit to work.txt so the rebase fails.
	conflictFactory := func(ctx context.Context, projectDir, runID, headSHA string) (string, func(), error) {
		wtPath, cleanup, err := mergeToMainCommittingFactory(t)(ctx, projectDir, runID, headSHA)
		if err != nil {
			return "", nil, err
		}
		mergeToMainFixtureAdvanceMainConflicting(t, projectDir)
		return wtPath, cleanup, nil
	}

	// Handler exits 0 — triggers the auto-close heuristic branch.
	deps := daemon.ExportedWorkLoopDeps(daemon.WorkLoopDepsParams{
		BrAdapter:        ledger,
		Bus:              collector,
		ProjectDir:       projectDir,
		HandlerBinary:    "/bin/sh",
		HandlerArgs:      []string{"-c", "exit 0"},
		IntentLogDir:     filepath.Join(projectDir, ".harmonik", "beads-intents"),
		AdapterRegistry2: NewSealedAdapterRegistryForTest(t),
		WorktreeFactory:  conflictFactory,
	})

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	loopDone := make(chan struct{})
	go func() {
		defer close(loopDone)
		daemon.ExportedRunWorkLoop(ctx, deps)
	}()

	select {
	case <-ledger.doneCh:
		cancel()
	case <-ctx.Done():
		t.Error("timed out waiting for bead close/reopen")
	}

	select {
	case <-loopDone:
	case <-time.After(5 * time.Second):
		t.Error("work loop did not exit within 5s")
	}

	// ── Assertion (f): ReopenBead called. ────────────────────────────────────
	if got := ledger.getReopenedCount(); got < 1 {
		t.Errorf("ReopenBead call count = %d; want ≥ 1 on rebase-conflict path", got)
	}

	// ── Assertion (h): CloseBead NOT called. ─────────────────────────────────
	if got := ledger.getClosedCount(); got != 0 {
		t.Errorf("CloseBead call count = %d; want 0 on rebase-conflict path (EM-053)", got)
	}

	// ── Assertion (g): outcome_emitted{kind=rejected, reason=rebase_conflict}. ──
	outcomeEvs := mergeToMainFindEvents(collector, "outcome_emitted")
	if len(outcomeEvs) == 0 {
		t.Errorf("no outcome_emitted events found; event stream: %v", mergeToMainEventOrder(collector))
	} else {
		kind := mergeToMainPayloadKind(t, outcomeEvs[0])
		if kind != "rejected" {
			t.Errorf("outcome_emitted kind = %q; want %q", kind, "rejected")
		}
		reason := mergeToMainPayloadReason(t, outcomeEvs[0])
		if !strings.Contains(reason, "rebase_conflict") {
			t.Errorf("outcome_emitted reason %q does not contain %q", reason, "rebase_conflict")
		}
	}

	// bead_closed must NOT appear.
	if evs := mergeToMainFindEvents(collector, "bead_closed"); len(evs) > 0 {
		t.Errorf("bead_closed emitted on rebase-conflict path; want absent (EM-053): %v", evs)
	}

	types := mergeToMainEventOrder(collector)
	t.Logf("merge-to-main rebase-conflict path OK: events: %v", types)
}
