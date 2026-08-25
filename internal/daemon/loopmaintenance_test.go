package daemon

import (
	"bytes"
	"context"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/runregistry"

	"github.com/google/uuid"

	ltmux "github.com/gregberns/harmonik/internal/lifecycle/tmux"
	"github.com/gregberns/harmonik/internal/queue"
	"github.com/gregberns/harmonik/internal/workspace"
)

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

type recordingCompletionGC struct {
	calls       int
	projectDir  string
	observation queue.CompletionGCObservation
	results     []queue.CompletionGCResult
}

func (r *recordingCompletionGC) GarbageCollectCompletionReceipts(
	projectDir string,
	observation queue.CompletionGCObservation,
) ([]queue.CompletionGCResult, error) {
	r.calls++
	r.projectDir = projectDir
	r.observation = observation
	return r.results, nil
}

func TestTickBeforeDispatchCallsCompletionGCMaintenanceWithUntrustedClock(t *testing.T) {
	coordinator, disk, _ := haltFixture(t)
	gc := &recordingCompletionGC{}
	gc.results = []queue.CompletionGCResult{{
		QueueID: "queue-one", ReceiptID: "receipt-one",
		Phase: queue.CompletionGCPhaseReceiptSyncIndeterminate,
		Err:   errors.New("cut receipt sync"),
	}}
	projectDir := t.TempDir()
	var logOutput bytes.Buffer
	m := &loopMaintenance{
		coordinatorReap: coordinator,
		diskReclaim:     disk,
		queueSurface: queueSurfacePort{
			completionGC: gc,
			projectDir:   projectDir,
		},
		logW: &logOutput,
	}
	m.tickBeforeDispatch(t.Context())
	if gc.calls != 1 || gc.projectDir != projectDir || gc.observation != (queue.CompletionGCObservation{}) {
		t.Fatalf("GC call = count=%d project=%q observation=%+v", gc.calls, gc.projectDir, gc.observation)
	}
	for _, want := range []string{"queue-one", "receipt-one", "receipt_sync_indeterminate", "cut receipt sync"} {
		if !strings.Contains(logOutput.String(), want) {
			t.Fatalf("GC log %q does not contain %q", logOutput.String(), want)
		}
	}
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
		if adapter.listSessionsCalls != 0 {
			t.Errorf("halt tick ran the coordinator reap: ListSessions called %d times, want 0",
				adapter.listSessionsCalls)
		}
		if !m.state.lastCoordinatorReap.IsZero() {
			t.Errorf("halt tick stamped lastCoordinatorReap = %v, want zero",
				m.state.lastCoordinatorReap)
		}
		if !m.state.lastDiskCheck.IsZero() {
			t.Errorf("halt tick ran the disk check: lastDiskCheck = %v, want zero",
				m.state.lastDiskCheck)
		}
		if obs.diskLow {
			t.Error("halt tick reported diskLow; the halt observation carries halt only")
		}
	})

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
	ExportedDiskCheckSetCheckInterval(&port, time.Nanosecond)
	return port, calls
}

type diskSeamCalls struct {
	worktreeReclaim int
}

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
		runRegistry:       runregistry.NewRunRegistry(),
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
	t.Run("below watermark sets diskLow", func(t *testing.T) {
		port, calls := diskLowFixture(t, diskLowWatermarkDefault-1)
		ms := ExportedNewMaintState()

		ExportedRunPeriodicDiskCheck(context.Background(), port, ms)

		if !ExportedDiskCheckDiskLow(ms) {
			t.Error("free space one byte below the watermark: want diskLow = true")
		}
		if calls.worktreeReclaim != 0 {
			t.Errorf("worktree-reclaim seam called %d times, want 0 on a nil run registry", calls.worktreeReclaim)
		}
	})

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
