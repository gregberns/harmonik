//go:build scenario

package daemon_test

import (
	"context"
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
	"github.com/gregberns/harmonik/internal/mergeq"
)

type multiBeadLedger struct {
	mu sync.Mutex

	// pending is the FIFO of bead-ids still to be handed out by Ready.
	pending []core.BeadID

	// total is the count of beads this ledger was seeded with.
	total int

	// closed / reopened map bead-id → call count.
	closed   map[core.BeadID]int
	reopened map[core.BeadID]int

	// reopenReason maps bead-id → last reopen reason (for the conflict-reason assertion).
	reopenReason map[core.BeadID]string

	// runToBead maps a run-id string → the bead-id it was claimed for, populated
	// in ClaimBead. The worktree factory consults this to decide whether the run
	// is the designated conflict bead.
	runToBead map[string]core.BeadID

	// doneCh is closed once close+reopen calls cover all `total` beads.
	doneCh   chan struct{}
	doneOnce sync.Once
}

func newMultiBeadLedger(beads []core.BeadID) *multiBeadLedger {
	pending := make([]core.BeadID, len(beads))
	copy(pending, beads)
	return &multiBeadLedger{
		pending:      pending,
		total:        len(beads),
		closed:       make(map[core.BeadID]int),
		reopened:     make(map[core.BeadID]int),
		reopenReason: make(map[core.BeadID]string),
		runToBead:    make(map[string]core.BeadID),
		doneCh:       make(chan struct{}),
	}
}

func (l *multiBeadLedger) Ready(_ context.Context) ([]core.BeadRecord, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.pending) == 0 {
		return nil, nil
	}
	id := l.pending[0]
	l.pending = l.pending[1:]
	return []core.BeadRecord{{BeadID: id, Status: core.CoarseStatusOpen, Labels: workloopFixtureSingleLabels}}, nil
}

func (l *multiBeadLedger) ShowBead(_ context.Context, id core.BeadID) (core.BeadRecord, error) {
	return core.BeadRecord{BeadID: id, Status: core.CoarseStatusOpen, Labels: workloopFixtureSingleLabels}, nil
}

func (l *multiBeadLedger) ClaimBead(_ context.Context, _ string, _ brcli.TimeoutConfig, runID core.RunID, _ core.TransitionID, beadID core.BeadID) error {
	l.mu.Lock()
	l.runToBead[runID.String()] = beadID
	l.mu.Unlock()
	return nil
}

func (l *multiBeadLedger) beadForRun(runID string) (core.BeadID, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	b, ok := l.runToBead[runID]
	return b, ok
}

func (l *multiBeadLedger) CloseBead(_ context.Context, _ string, _ brcli.TimeoutConfig, _ core.RunID, _ core.TransitionID, beadID core.BeadID, _ bool) error {
	l.mu.Lock()
	l.closed[beadID]++
	l.maybeSignalDoneNoLock()
	l.mu.Unlock()
	return nil
}

func (l *multiBeadLedger) ReopenBead(_ context.Context, _ string, _ brcli.TimeoutConfig, _ core.RunID, _ core.TransitionID, beadID core.BeadID, reason string) error {
	l.mu.Lock()
	l.reopened[beadID]++
	l.reopenReason[beadID] = reason
	l.maybeSignalDoneNoLock()
	l.mu.Unlock()
	return nil
}

func (l *multiBeadLedger) maybeSignalDoneNoLock() {
	terminal := make(map[core.BeadID]struct{}, l.total)
	for b := range l.closed {
		terminal[b] = struct{}{}
	}
	for b := range l.reopened {
		terminal[b] = struct{}{}
	}
	if len(terminal) >= l.total {
		l.doneOnce.Do(func() { close(l.doneCh) })
	}
}

func (l *multiBeadLedger) closedCount(id core.BeadID) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.closed[id]
}

func (l *multiBeadLedger) reopenedCount(id core.BeadID) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.reopened[id]
}

func (l *multiBeadLedger) reopenReasonOf(id core.BeadID) string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.reopenReason[id]
}

func (l *multiBeadLedger) totalClosed() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	n := 0
	for _, c := range l.closed {
		n += c
	}
	return n
}

func hktijajGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("hktijajGit: git %v (dir=%s): %v\n%s", args, dir, err, out)
	}
	return strings.TrimRight(string(out), "\n")
}

func hktijajInitRepoWithOrigin(t *testing.T) (string, string) {
	t.Helper()
	projectDir := t.TempDir()
	//nolint:gosec // G301: 0755 matches .harmonik dir conventions
	if err := os.MkdirAll(filepath.Join(projectDir, ".harmonik", "beads-intents"), 0o755); err != nil {
		t.Fatalf("hktijajInitRepoWithOrigin: mkdir beads-intents: %v", err)
	}
	//nolint:gosec // G301
	if err := os.MkdirAll(filepath.Join(projectDir, ".harmonik", "events"), 0o755); err != nil {
		t.Fatalf("hktijajInitRepoWithOrigin: mkdir events: %v", err)
	}

	hktijajGit(t, projectDir, "init", "--initial-branch=main")
	hktijajGit(t, projectDir, "config", "user.email", "daemon@harmonik.local")
	hktijajGit(t, projectDir, "config", "user.name", "Harmonik Test")

	//nolint:gosec // G306: test fixture file
	if err := os.WriteFile(filepath.Join(projectDir, "README"), []byte("initial\n"), 0o644); err != nil {
		t.Fatalf("hktijajInitRepoWithOrigin: WriteFile README: %v", err)
	}
	hktijajGit(t, projectDir, "add", "README")
	hktijajGit(t, projectDir, "commit", "-m", "init")

	originDir := t.TempDir()
	hktijajGit(t, originDir, "init", "--bare", "--initial-branch=main")
	hktijajGit(t, projectDir, "remote", "add", "origin", originDir)
	hktijajGit(t, projectDir, "push", "origin", "main")

	return projectDir, originDir
}

func hktijajStageInWorktree(t *testing.T, ctx context.Context, wtPath, relPath, content string) {
	t.Helper()
	full := filepath.Join(wtPath, relPath)
	//nolint:gosec // G306: test fixture file
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatalf("hktijajStageInWorktree: WriteFile %s: %v", relPath, err)
	}
	cmd := exec.CommandContext(ctx, "git", "add", relPath)
	cmd.Dir = wtPath
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("hktijajStageInWorktree: git add %s: %v\n%s", relPath, err, out)
	}
}

func hktijajAgentHandlerArgs(t *testing.T) []string {
	t.Helper()
	gitPath, err := exec.LookPath("git")
	if err != nil {
		t.Fatalf("hktijajAgentHandlerArgs: git not found on PATH: %v", err)
	}
	script := fmt.Sprintf(
		"exec 1>&2\nset -e\n%s commit -q -m 'feat: agent work'\n",
		gitPath,
	)
	return []string{"-c", script}
}

func hktijajAdvanceMainOn(t *testing.T, ctx context.Context, projectDir, relPath, content string) {
	t.Helper()
	full := filepath.Join(projectDir, relPath)
	//nolint:gosec // G306: test fixture file
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatalf("hktijajAdvanceMainOn: WriteFile %s: %v", relPath, err)
	}
	for _, args := range [][]string{
		{"add", relPath},
		{"commit", "-m", "out-of-band advance on " + relPath},
		{"push", "origin", "main"},
	} {
		cmd := exec.CommandContext(ctx, "git", args...)
		cmd.Dir = projectDir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("hktijajAdvanceMainOn: git %v: %v\n%s", args, err, out)
		}
	}
}

// TestScenario_MultiBead_ConflictSkipsButOthersProceed verifies the EM-053
// "others-proceed" property the single-bead conflict test cannot reach:
//
//	Given a batch of N beads where exactly one (the conflict bead) writes a file
//	that main is then advanced on with different content,
//	When the work loop merges each run to main,
//	Then:
//	  (1) the conflict bead REOPENS (reopened ≥1, closed 0) with a reason that
//	      names the rebase conflict — it is NOT silently lost,
//	  (2) every OTHER bead in the same batch still merges + closes (closed == 1,
//	      reopened 0),
//	  (3) main is NOT corrupted by the conflict bead: the conflict bead's
//	      worktree content never lands on main, while all clean beads' files do.
//
// Harness: in-process ExportedRunWorkLoop, MaxConcurrent=N, real git repos.
// The conflict is produced deterministically by the worktree factory: it looks
// up the bead-id for the run (recorded in ClaimBead), and for the designated
// conflict bead it (a) commits conflict.txt in the worktree, then (b) advances
// main on conflict.txt with different content — guaranteeing a rebase conflict
// at merge time. Clean beads commit a unique work-<bead>.txt that never
// collides.
//
// Bead: hk-tijaj. Spec: specs/execution-model.md §4.12 EM-053.
func TestScenario_MultiBead_ConflictSkipsButOthersProceed(t *testing.T) {
	skipRealDaemonE2EInShort(t)
	t.Parallel()

	const (
		beadConflict = core.BeadID("hktijaj-conflict-bead")
		beadCleanA   = core.BeadID("hktijaj-clean-A")
		beadCleanB   = core.BeadID("hktijaj-clean-B")
		beadCleanC   = core.BeadID("hktijaj-clean-C")
	)
	cleanBeads := []core.BeadID{beadCleanA, beadCleanB, beadCleanC}
	allBeads := append([]core.BeadID{beadConflict}, cleanBeads...)

	projectDir, _ := hktijajInitRepoWithOrigin(t)
	ledger := newMultiBeadLedger(allBeads)
	collector := &stubEventCollector{}

	mergeQ := mergeq.New(nil)
	mergeQCtx, mergeQCancel := context.WithCancel(context.Background())
	mergeQ.Start(mergeQCtx)
	t.Cleanup(mergeQCancel)

	fileForBead := func(id core.BeadID) string {
		return "work-" + strings.ReplaceAll(string(id), "/", "_") + ".txt"
	}
	const conflictFile = "conflict.txt"

	worktreeFactory := func(ctx context.Context, projectDir, runID, headSHA string) (string, func(), error) {
		wtPath, cleanup, err := daemon.ExportedProductionWorktreeFactory(ctx, projectDir, runID, headSHA)
		if err != nil {
			return "", nil, err
		}
		beadID, ok := ledger.beadForRun(runID)
		if !ok {
			cleanup()
			return "", nil, fmt.Errorf("hktijaj: no bead recorded for run %s", runID)
		}
		if beadID == beadConflict {
			hktijajStageInWorktree(t, ctx, wtPath, conflictFile, "agent version of conflict file\n")
			hktijajAdvanceMainOn(t, ctx, projectDir, conflictFile, "main's out-of-band conflicting content\n")
			return wtPath, cleanup, nil
		}
		hktijajStageInWorktree(t, ctx, wtPath, fileForBead(beadID), "clean work for "+string(beadID)+"\n")
		return wtPath, cleanup, nil
	}

	deps := daemon.ExportedTestRuntime(daemon.TestRuntimeParams{
		BrAdapter:        ledger,
		Bus:              collector,
		ProjectDir:       projectDir,
		HandlerBinary:    "/bin/sh",
		HandlerArgs:      hktijajAgentHandlerArgs(t),
		IntentLogDir:     filepath.Join(projectDir, ".harmonik", "beads-intents"),
		MaxConcurrent:    len(allBeads),
		AdapterRegistry2: NewSealedAdapterRegistryForTest(t),
		WorktreeFactory:  worktreeFactory,
		MergeQueue:       mergeQ,
	})

	ctx, cancel := context.WithTimeout(t.Context(), 240*time.Second)
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
		t.Fatalf("timed out waiting for all %d beads to reach a terminal state", len(allBeads))
	}

	awaitLoopTeardown(t, loopDone, "work loop")

	if got := ledger.reopenedCount(beadConflict); got < 1 {
		t.Errorf("conflict bead %s reopened %d times; want ≥1 (EM-053 auto-skip)", beadConflict, got)
	}
	if got := ledger.closedCount(beadConflict); got != 0 {
		t.Errorf("conflict bead %s closed %d times; want 0 (must NOT be closed on conflict)", beadConflict, got)
	}
	if reason := ledger.reopenReasonOf(beadConflict); !strings.Contains(reason, "conflict") {
		t.Errorf("conflict bead reopen reason = %q; want it to name the conflict", reason)
	}

	for _, b := range cleanBeads {
		if got := ledger.closedCount(b); got != 1 {
			t.Errorf("clean bead %s closed %d times; want 1 (others-proceed)", b, got)
		}
		if got := ledger.reopenedCount(b); got != 0 {
			t.Errorf("clean bead %s reopened %d times; want 0", b, got)
		}
	}

	hktijajGit(t, projectDir, "checkout", "main")
	for _, b := range cleanBeads {
		path := filepath.Join(projectDir, fileForBead(b))
		if _, err := os.Stat(path); err != nil {
			t.Errorf("clean bead %s file %s missing on main: %v", b, fileForBead(b), err)
		}
	}
	cb, err := os.ReadFile(filepath.Join(projectDir, conflictFile)) //nolint:gosec // test path
	if err != nil {
		t.Fatalf("read conflict.txt on main: %v", err)
	}
	if strings.Contains(string(cb), "agent version") {
		t.Errorf("main's %s contains the conflict bead's agent content — main corrupted by a non-merged run: %q",
			conflictFile, string(cb))
	}

	t.Logf("conflict-skip+others-proceed OK: conflict bead reopened, %d clean beads closed; main intact", len(cleanBeads))
}

// TestScenario_MultiBead_SerializedNCompletion verifies the mergeMu property
// (hk-yyso7) that the per-bead tests cannot reach: when N beads complete
// near-simultaneously, every one of the N distinct commits lands on the target
// branch (no lost commit, no clobbered update-ref) and the resulting history is
// a clean LINEAR chain of exactly N commits past the seed.
//
// A merge race (the failure this guards against) would manifest as either a
// lost commit (one run's update-ref overwritten by a sibling between the
// FF-check and the push) or a non-linear / corrupted history. Under correct
// mergeMu serialisation each run rebases onto the latest tip and fast-forwards,
// so the N work files form a single ancestry line and all N close.
//
// Harness: in-process ExportedRunWorkLoop, MaxConcurrent=N (all dispatched
// concurrently), real git repos. Every bead writes a UNIQUE file so there is
// NO content conflict — the ONLY thing that can drop a commit is a merge race.
//
// Bead: hk-tijaj. Spec: specs/execution-model.md §4.12 EM-052; mergeMu (hk-yyso7).
func TestScenario_MultiBead_SerializedNCompletion(t *testing.T) {
	skipRealDaemonE2EInShort(t)
	t.Parallel()

	const n = 5
	beads := make([]core.BeadID, n)
	for i := 0; i < n; i++ {
		beads[i] = core.BeadID(fmt.Sprintf("hktijaj-serial-%d", i))
	}

	projectDir, _ := hktijajInitRepoWithOrigin(t)
	ledger := newMultiBeadLedger(beads)
	collector := &stubEventCollector{}

	fileForBead := func(id core.BeadID) string {
		return "serial-" + string(id) + ".txt"
	}

	worktreeFactory := func(ctx context.Context, projectDir, runID, headSHA string) (string, func(), error) {
		wtPath, cleanup, err := daemon.ExportedProductionWorktreeFactory(ctx, projectDir, runID, headSHA)
		if err != nil {
			return "", nil, err
		}
		beadID, ok := ledger.beadForRun(runID)
		if !ok {
			cleanup()
			return "", nil, fmt.Errorf("hktijaj: no bead recorded for run %s", runID)
		}
		hktijajStageInWorktree(t, ctx, wtPath, fileForBead(beadID), "serial work for "+string(beadID)+"\n")
		return wtPath, cleanup, nil
	}

	deps := daemon.ExportedTestRuntime(daemon.TestRuntimeParams{
		BrAdapter:        ledger,
		Bus:              collector,
		ProjectDir:       projectDir,
		HandlerBinary:    "/bin/sh",
		HandlerArgs:      hktijajAgentHandlerArgs(t),
		IntentLogDir:     filepath.Join(projectDir, ".harmonik", "beads-intents"),
		MaxConcurrent:    n,
		AdapterRegistry2: NewSealedAdapterRegistryForTest(t),
		WorktreeFactory:  worktreeFactory,
	})

	ctx, cancel := context.WithTimeout(t.Context(), 120*time.Second)
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
		t.Fatalf("timed out waiting for all %d beads to complete; closed so far=%d", n, ledger.totalClosed())
	}

	awaitLoopTeardown(t, loopDone, "work loop")

	for _, b := range beads {
		if got := ledger.closedCount(b); got != 1 {
			t.Errorf("bead %s closed %d times; want 1 (serialized completion, no lost merge)", b, got)
		}
		if got := ledger.reopenedCount(b); got != 0 {
			t.Errorf("bead %s reopened %d times; want 0 (no merge race / no spurious non-FF)", b, got)
		}
	}

	hktijajGit(t, projectDir, "checkout", "main")
	for _, b := range beads {
		path := filepath.Join(projectDir, fileForBead(b))
		if _, err := os.Stat(path); err != nil {
			t.Errorf("work file for bead %s (%s) missing on main — commit lost to a merge race: %v",
				b, fileForBead(b), err)
		}
	}

	logOut := hktijajGit(t, projectDir, "log", "--format=%H %P", "main")
	mergeCommits := 0
	for _, ln := range strings.Split(strings.TrimSpace(logOut), "\n") {
		fields := strings.Fields(ln) // fields[0]=commit; fields[1:]=parents
		if len(fields) >= 3 {        // commit + ≥2 parents == merge commit
			mergeCommits++
		}
	}
	if mergeCommits != 0 {
		t.Errorf("main history has %d merge commit(s); want 0 (one-at-a-time FF merges, no merge race):\n%s",
			mergeCommits, logOut)
	}

	totalCommits := hktijajGit(t, projectDir, "rev-list", "--count", "HEAD")
	t.Logf("serialized N-completion OK: %d beads closed, %d files on main, no merge commits (total main commits incl. init/seed=%s)",
		n, n, totalCommits)
}
