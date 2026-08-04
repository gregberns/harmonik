package daemon

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	ltmux "github.com/gregberns/harmonik/internal/lifecycle/tmux"
	"github.com/gregberns/harmonik/internal/workspace"
)

// recordingReapAdapter is an ltmux.Adapter that records whether the coordinator
// reap reached tmux.
//
// The embedded interface is nil on purpose. Only ListSessions is implemented,
// because that is the first and only adapter method reapDeadCoordinatorSession
// calls before it decides there is nothing to kill. Any OTHER method the code
// under test starts calling panics on the nil interface, which is the outcome we
// want: a silent new tmux call in a maintenance pass should fail this test, not
// be absorbed by a permissive stub.
type recordingReapAdapter struct {
	ltmux.Adapter
	listSessionsCalls int
}

// ListSessions records the call and reports no live sessions, so the reap finds
// nothing to kill and never reaches KillSession.
func (a *recordingReapAdapter) ListSessions(context.Context) ([]string, error) {
	a.listSessionsCalls++
	return nil, nil
}

// haltFixture builds the deps, coordinator-reap port, and maintenance state the
// two subtests share.
//
// projectDir is a real temp dir with no coordinator sentinel file in it, so
// probeCoordinatorSentinel reports "not live" and the reap proceeds to the
// adapter. That is what makes the negative assertion below meaningful: the reap
// WOULD call tmux on this fixture, so observing no call proves the short-circuit
// rather than proving the fixture is inert.
//
// diskFreeBytesFunc is injected well above the watermark so the disk job takes
// its healthy path here. The disk-LOW branch is covered separately by
// TestDiskLowBranch below.
func haltFixture(t *testing.T) (coordinatorReapPort, diskReclaimPort, *recordingReapAdapter) {
	t.Helper()
	adapter := &recordingReapAdapter{}
	port := coordinatorReapPort{
		projectDir: t.TempDir(),
		adapter:    adapter,
		interval:   time.Nanosecond, // cadence is never the reason a call is skipped
	}
	diskReclaim := diskReclaimPort{
		projectDir:        t.TempDir(),
		diskFreeBytesFunc: func(string) (uint64, error) { return 1 << 62, nil },
	}
	return port, diskReclaim, adapter
}

// TestTickBeforeDispatchHaltShortCircuits pins the invariant loopmaintenance.go
// calls load-bearing: when the governor has an ARMED halt, tickBeforeDispatch
// reports it and runs NOTHING else on that tick.
//
// The "nothing else" half is the point of the test, not a bonus. The ordering
// comment in tickBeforeDispatch claims the halt check short-circuits the pass, and
// before this test nothing executable held that claim. The halt path had no
// coverage at all: scenario_flywheel_bt5_hk5pcr_test.go reaches sentinel.Evaluate
// returning ActivationHalt and stops there, never reaching onHalt, halted(), or
// the loop's exit.
func TestTickBeforeDispatchHaltShortCircuits(t *testing.T) {
	t.Run("halted governor reports halt and skips every other job", func(t *testing.T) {
		port, diskReclaim, adapter := haltFixture(t)
		m := &loopMaintenance{coordinatorReap: port, diskReclaim: diskReclaim, governor: &movementGovernor{haltRequested: true}}

		obs := m.tickBeforeDispatch(context.Background())

		if !obs.halt {
			t.Fatalf("armed governor halt: want observation.halt = true, got %+v", obs)
		}
		// The load-bearing assertion: the reap must not have run.
		if adapter.listSessionsCalls != 0 {
			t.Errorf("halt tick ran the coordinator reap: ListSessions called %d times, want 0",
				adapter.listSessionsCalls)
		}
		// The reap's clock must be untouched too. A stamped clock would mean the
		// job ran even if the adapter somehow was not reached.
		if !m.state.lastCoordinatorReap.IsZero() {
			t.Errorf("halt tick stamped lastCoordinatorReap = %v, want zero",
				m.state.lastCoordinatorReap)
		}
		// Same for the disk job: no probe, so no latch and no clock.
		if !m.state.lastDiskCheck.IsZero() {
			t.Errorf("halt tick ran the disk check: lastDiskCheck = %v, want zero",
				m.state.lastDiskCheck)
		}
		if obs.diskLow {
			t.Error("halt tick reported diskLow; the halt observation carries halt only")
		}
	})

	// Positive control. Without this, the assertions above would still pass if the
	// fixture simply could not reach tmux, and the test would prove nothing.
	t.Run("same fixture without the halt does run the reap", func(t *testing.T) {
		port, diskReclaim, adapter := haltFixture(t)
		m := &loopMaintenance{coordinatorReap: port, diskReclaim: diskReclaim, governor: &movementGovernor{haltRequested: false}}

		obs := m.tickBeforeDispatch(context.Background())

		if obs.halt {
			t.Error("no armed halt: want observation.halt = false")
		}
		if adapter.listSessionsCalls != 1 {
			t.Fatalf("unhalted tick: ListSessions called %d times, want 1 — "+
				"the negative subtest above is only meaningful if this fixture reaches tmux",
				adapter.listSessionsCalls)
		}
		if m.state.lastCoordinatorReap.IsZero() {
			t.Error("unhalted tick did not stamp lastCoordinatorReap")
		}
		if obs.diskLow {
			t.Error("free space was injected far above the watermark, so diskLow must be false")
		}
	})

	// A nil governor is the switched-off subsystem. It must read as "no halt"
	// rather than panicking, which is what lets runWorkLoop hold one code path for
	// both configurations.
	t.Run("absent governor subsystem never halts", func(t *testing.T) {
		port, diskReclaim, adapter := haltFixture(t)
		m := &loopMaintenance{coordinatorReap: port, diskReclaim: diskReclaim, governor: nil}

		obs := m.tickBeforeDispatch(context.Background())

		if obs.halt {
			t.Error("nil governor must not request a halt")
		}
		if adapter.listSessionsCalls != 1 {
			t.Errorf("nil governor should not short-circuit the pass: ListSessions called %d times, want 1",
				adapter.listSessionsCalls)
		}
	})
}

// diskLowFixture builds deps that drive the disk probe BELOW the watermark with
// the one remaining subprocess seam stubbed.
//
// worktreeReclaimFunc stands in for the `git worktree remove` /
// `worktree prune` sequence (runWorktreeReclaim prefers it when non-nil). It is
// wired even though it is unreachable on this fixture, so that if the branch
// ever grows a new route to that subprocess the test records a call instead of
// spawning one.
//
// There is no go-cache seam. The daemon no longer runs `go clean -cache`, and
// TestDiskLowNeverDeletesTheGoBuildCache holds that property.
//
// runRegistry is nil on purpose: mergeOrRunInFlight reports "idle", so the
// branch takes the reclaim path rather than the run-in-flight warning path, and
// reclaimStaleWorktrees returns 0 immediately, so the reclaim-was-sufficient
// early return is skipped and the probe reaches the report step.
//
// bus is nil, so the disk_low event emit is skipped. The event payload is not
// what this test is about.
func diskLowFixture(t *testing.T, freeBytes uint64) (diskReclaimPort, *diskSeamCalls) {
	t.Helper()
	calls := &diskSeamCalls{}
	port := diskReclaimPort{
		projectDir:        t.TempDir(),
		diskFreeBytesFunc: func(string) (uint64, error) { return freeBytes, nil },
		worktreeReclaimFunc: func(context.Context, string, []string) error {
			calls.worktreeReclaim++
			return nil
		},
	}
	// Fire on the first tick instead of waiting out diskCheckInterval.
	ExportedDiskCheckSetCheckInterval(&port, time.Nanosecond)
	return port, calls
}

// diskSeamCalls counts the stubbed subprocess seams. A named field rather than a
// bare *int, so a call site cannot silently transpose it with a future sibling.
type diskSeamCalls struct {
	worktreeReclaim int
}

// staleWorktreeFixture is diskLowFixture with the stale-worktree reclaim made
// REACHABLE: a real (empty) run registry plus one UUID-named directory under
// .harmonik/worktrees/.
//
// The reclaim stub counts the call and returns nil WITHOUT removing the
// directory. Two things follow, and both are wanted. reclaimStaleWorktrees
// counts zero directories actually gone, so the "reclaim was enough" early
// return is not taken and the probe reaches the report step. And the directory
// is still reclaimable on the NEXT probe, which is what makes
// "the healthy path reclaimed nothing" a real assertion rather than a
// restatement of an empty fixture.
func staleWorktreeFixture(t *testing.T, freeBytes uint64) (diskReclaimPort, *diskSeamCalls) {
	t.Helper()
	projectDir := t.TempDir()
	staleDir := filepath.Join(projectDir, workspace.DefaultWorktreeRoot, uuid.NewString())
	if err := os.MkdirAll(staleDir, 0o750); err != nil {
		t.Fatalf("create stale worktree dir: %v", err)
	}
	calls := &diskSeamCalls{}
	port := diskReclaimPort{
		projectDir:        projectDir,
		runRegistry:       NewRunRegistry(),
		diskFreeBytesFunc: func(string) (uint64, error) { return freeBytes, nil },
		worktreeReclaimFunc: func(context.Context, string, []string) error {
			calls.worktreeReclaim++
			return nil
		},
	}
	ExportedDiskCheckSetCheckInterval(&port, time.Nanosecond)
	return port, calls
}

// TestDiskLowBranch covers the disk-below-watermark branch of the periodic disk
// check, which had NO coverage anywhere in the tree: diskcheck_hksxlb_test.go was
// deleted, leaving the export shims in export_maintenance_test.go with zero
// callers.
//
// An earlier version of this file claimed the branch was too expensive to test
// because it would run subprocesses for real. That was wrong. diskReclaimPort
// carries worktreeReclaimFunc as a seam for exactly this purpose, so the branch
// is cheap. The false claim is recorded here because a comment that talks a
// reader out of a test they could have written is worse than no comment.
func TestDiskLowBranch(t *testing.T) {
	// Below the watermark: latch set, no real subprocess.
	t.Run("below watermark sets diskLow", func(t *testing.T) {
		port, calls := diskLowFixture(t, diskLowWatermarkDefault-1)
		ms := ExportedNewMaintState()

		ExportedRunPeriodicDiskCheck(context.Background(), port, ms)

		if !ExportedDiskCheckDiskLow(ms) {
			t.Error("free space one byte below the watermark: want diskLow = true")
		}
		// Nil runRegistry means no stale worktrees are enumerated, so the reclaim
		// seam is not reached on this path. Asserted so the fixture's shape stays
		// visible rather than implied.
		if calls.worktreeReclaim != 0 {
			t.Errorf("worktree-reclaim seam called %d times, want 0 on a nil run registry", calls.worktreeReclaim)
		}
	})

	// The low path reclaims the daemon's OWN stale worktrees. This is the
	// positive control for the recovery subtest below: without it, "the healthy
	// path reclaimed nothing" would pass on a fixture that could never reclaim
	// anything at all.
	t.Run("below watermark reclaims the daemon's own stale worktrees", func(t *testing.T) {
		port, calls := staleWorktreeFixture(t, diskLowWatermarkDefault-1)
		ms := ExportedNewMaintState()

		ExportedRunPeriodicDiskCheck(context.Background(), port, ms)

		if !ExportedDiskCheckDiskLow(ms) {
			t.Error("the stub removes nothing, so free space is still below the watermark: want diskLow = true")
		}
		if calls.worktreeReclaim != 1 {
			t.Errorf("worktree-reclaim seam called %d times, want 1 — a stale run worktree is the one thing the daemon may reclaim",
				calls.worktreeReclaim)
		}
	})

	// The latch must CLEAR when the disk recovers, using the same state handle.
	// This is the transition, not two independent probes.
	t.Run("recovery clears the diskLow latch and reclaims nothing", func(t *testing.T) {
		port, calls := staleWorktreeFixture(t, diskLowWatermarkDefault-1)
		ms := ExportedNewMaintState()

		ExportedRunPeriodicDiskCheck(context.Background(), port, ms)
		if !ExportedDiskCheckDiskLow(ms) {
			t.Fatal("setup: want diskLow = true before testing recovery")
		}
		if calls.worktreeReclaim != 1 {
			t.Fatalf("setup: worktree-reclaim seam called %d times on the low probe, want 1", calls.worktreeReclaim)
		}

		// Same deps, same state handle, disk now healthy. The stale worktree is
		// still on disk and still reclaimable, so a healthy path that reclaimed
		// would push this counter to 2.
		port.diskFreeBytesFunc = func(string) (uint64, error) { return 1 << 62, nil }
		ExportedRunPeriodicDiskCheck(context.Background(), port, ms)

		if ExportedDiskCheckDiskLow(ms) {
			t.Error("disk recovered above the watermark: want diskLow = false")
		}
		if calls.worktreeReclaim != 1 {
			t.Errorf("worktree-reclaim seam called %d times, want 1 — a healthy disk must reclaim nothing, and this fixture still has something to reclaim",
				calls.worktreeReclaim)
		}
	})

	// The branch must be reachable through the new maintenance seam, not only by
	// calling runPeriodicDiskCheck directly, and the observation must carry the
	// latch out to the loop.
	t.Run("tickBeforeDispatch reports diskLow to the loop", func(t *testing.T) {
		port, _ := diskLowFixture(t, diskLowWatermarkDefault-1)
		m := &loopMaintenance{diskReclaim: port}

		obs := m.tickBeforeDispatch(context.Background())

		if !obs.diskLow {
			t.Error("want observation.diskLow = true so the loop skips bead claiming this tick")
		}
		if obs.halt {
			t.Error("a low disk must not request a halt")
		}
		if !m.state.diskLow {
			t.Error("the latch must persist on loopMaintenance.state across ticks")
		}
	})
}

// TestDiskLowNeverDeletesTheGoBuildCache is the guard on the property this
// package was changed to hold: a low disk makes the daemon REPORT, never delete
// a resource it shares.
//
// `go clean -cache` empties the default GOCACHE. That cache is read and written
// at the same time by both lanes, every agent worktree, the daemon's own merge
// builds, and any terminal the operator is using. Deleting it mid-build produced
// two failures, and the second is the dangerous one: builds that failed with
// "could not import os/context/testing/...", and builds that reported success
// without rebuilding anything. The daemon cannot tell when the delete is safe,
// because its run registry only sees its own runs.
//
// The assertion is source-level, in the same shape as agentlaunch_scope_test.go.
// A behavioural version would have to point GOCACHE at a scratch directory and
// look for its contents afterwards, and that means changing a process-wide
// environment variable while sibling tests in this package shell out to the go
// toolchain — the test would create the very class of mid-build cache surprise
// it exists to prevent.
//
// The scan covers the whole package, not just diskcheck_hksxlb.go, because the
// reap does not have to come back in the file it left.
func TestDiskLowNeverDeletesTheGoBuildCache(t *testing.T) {
	t.Parallel()

	pkgDir := filepath.Join(repoRootForConformance(), "internal", "daemon")
	entries, err := os.ReadDir(pkgDir)
	if err != nil {
		t.Fatalf("read internal/daemon: %v", err)
	}

	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, parseErr := parser.ParseFile(token.NewFileSet(), filepath.Join(pkgDir, name), nil, parser.SkipObjectResolution)
		if parseErr != nil {
			t.Fatalf("parse %s: %v", name, parseErr)
		}
		if args := goToolchainCleanCalls(file); len(args) != 0 {
			t.Errorf("internal/daemon/%s runs the go toolchain with %v.\n"+
				"The daemon must not delete the Go build cache. That cache is shared with builds the daemon cannot see, and deleting it mid-build has produced builds that reported success without rebuilding anything. Detecting a low disk and emitting disk_low is the whole job; reclaiming a shared resource is the operator's call — see docs/disk-reclaim.md. If the daemon must reclaim something itself, reclaim what it owns, the way reclaimStaleWorktrees does.",
				name, args)
		}
	}
}

// goToolchainCleanCalls returns the literal argument list of every exec call in
// file that runs `go` with a `clean` subcommand.
//
// It matches on the string literals passed to os/exec rather than on a helper
// name, so restoring the reap under a fresh function name is still caught. A
// caller that assembles the argument list at run time is NOT caught; that is a
// deliberate limit, because the file-level comment on diskcheck_hksxlb.go — not
// this scan — is what tells the next reader why the reap is gone.
func goToolchainCleanCalls(file *ast.File) [][]string {
	var found [][]string
	ast.Inspect(file, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, selOK := call.Fun.(*ast.SelectorExpr)
		if !selOK || !isIdent(sel.X, "exec") {
			return true
		}
		if sel.Sel.Name != "Command" && sel.Sel.Name != "CommandContext" {
			return true
		}
		var literals []string
		for _, arg := range call.Args {
			lit, litOK := unparenExpr(arg).(*ast.BasicLit)
			if !litOK || lit.Kind != token.STRING {
				continue
			}
			unquoted, unquoteErr := strconv.Unquote(lit.Value)
			if unquoteErr != nil {
				continue
			}
			literals = append(literals, unquoted)
		}
		runsGo, runsClean := false, false
		for _, l := range literals {
			switch l {
			case "go":
				runsGo = true
			case "clean":
				runsClean = true
			}
		}
		if runsGo && runsClean {
			found = append(found, literals)
		}
		return true
	})
	return found
}
