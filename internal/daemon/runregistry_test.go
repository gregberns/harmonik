package daemon_test

import (
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
)

func runregistryFixtureRunID(t *testing.T) core.RunID {
	t.Helper()
	u, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("runregistryFixtureRunID: uuid.NewV7: %v", err)
	}
	return core.RunID(u)
}

func runregistryFixtureHandle(beadID string, wtPath string) *daemon.RunHandle {
	return &daemon.RunHandle{
		BeadID:       core.BeadID(beadID),
		WorktreePath: wtPath,
		StartedAt:    time.Now().UTC(),
	}
}

func TestRunRegistry_RegisterGetUnregister(t *testing.T) {
	reg := daemon.NewRunRegistry()

	runID := runregistryFixtureRunID(t)
	handle := runregistryFixtureHandle("bead-abc", "/worktrees/abc")

	if got, ok := reg.Get(runID); ok || got != nil {
		t.Fatalf("expected Get to return nil,false before Register; got %v,%v", got, ok)
	}

	reg.Register(runID, handle)
	got, ok := reg.Get(runID)
	if !ok {
		t.Fatal("expected Get to return true after Register")
	}
	if got != handle {
		t.Fatalf("Get returned wrong pointer: want %p got %p", handle, got)
	}

	if n := reg.Len(); n != 1 {
		t.Fatalf("Len() = %d, want 1", n)
	}

	reg.Unregister(runID)
	if got2, ok2 := reg.Get(runID); ok2 || got2 != nil {
		t.Fatalf("expected Get to return nil,false after Unregister; got %v,%v", got2, ok2)
	}

	if n := reg.Len(); n != 0 {
		t.Fatalf("Len() = %d, want 0 after Unregister", n)
	}
}

func TestRunRegistry_UnregisterNoop(t *testing.T) {
	reg := daemon.NewRunRegistry()
	runID := runregistryFixtureRunID(t)
	reg.Unregister(runID)
	if n := reg.Len(); n != 0 {
		t.Fatalf("Len() = %d after no-op Unregister, want 0", n)
	}
}

func TestRunRegistry_RegisterOverwrite(t *testing.T) {
	reg := daemon.NewRunRegistry()
	runID := runregistryFixtureRunID(t)

	h1 := runregistryFixtureHandle("bead-1", "/wt/1")
	h2 := runregistryFixtureHandle("bead-2", "/wt/2")

	reg.Register(runID, h1)
	reg.Register(runID, h2) // overwrite

	got, ok := reg.Get(runID)
	if !ok {
		t.Fatal("Get returned false after overwrite")
	}
	if got != h2 {
		t.Fatalf("Get returned old handle after overwrite; want h2 (%p), got %p", h2, got)
	}
	if n := reg.Len(); n != 1 {
		t.Fatalf("Len() = %d after overwrite, want 1", n)
	}
}

func TestRunRegistry_Snapshot(t *testing.T) {
	const count = 5
	reg := daemon.NewRunRegistry()

	ids := make([]core.RunID, count)
	for i := range count {
		ids[i] = runregistryFixtureRunID(t)
		reg.Register(ids[i], runregistryFixtureHandle("bead", "/wt"))
	}

	snap := reg.Snapshot()
	if len(snap) != count {
		t.Fatalf("Snapshot len = %d, want %d", len(snap), count)
	}

	for _, id := range ids {
		reg.Unregister(id)
	}
	if len(snap) != count {
		t.Fatalf("Snapshot length changed after Unregister (not a stable copy): got %d, want %d", len(snap), count)
	}
	if reg.Len() != 0 {
		t.Fatalf("Len() = %d after removing all; want 0", reg.Len())
	}
}

func TestRunRegistry_ConcurrentRegisterUnregister(t *testing.T) {
	const n = 100
	reg := daemon.NewRunRegistry()

	ids := make([]core.RunID, n)
	for i := range n {
		ids[i] = runregistryFixtureRunID(t)
	}

	var wg sync.WaitGroup
	wg.Add(n)
	for i := range n {
		go func(i int) {
			defer wg.Done()
			runID := ids[i]
			h := runregistryFixtureHandle("bead", "/wt")
			reg.Register(runID, h)
			_ = reg.Len()
			_, _ = reg.Get(runID)
			reg.Unregister(runID)
		}(i)
	}
	wg.Wait()

	if n := reg.Len(); n != 0 {
		t.Fatalf("Len() = %d after all goroutines finished; want 0", n)
	}
}

func TestRunRegistry_SnapshotStableDuringMutation(t *testing.T) {
	const (
		preload  = 50 // entries present before snapshot
		mutators = 50 // concurrent mutator goroutines
	)

	reg := daemon.NewRunRegistry()

	preloadIDs := make([]core.RunID, preload)
	for i := range preload {
		preloadIDs[i] = runregistryFixtureRunID(t)
		reg.Register(preloadIDs[i], runregistryFixtureHandle("bead-pre", "/wt/pre"))
	}

	var (
		wg          sync.WaitGroup
		snapshotLen int
	)

	wg.Add(1)
	go func() {
		defer wg.Done()
		snap := reg.Snapshot()
		snapshotLen = len(snap)
	}()

	mutatorIDs := make([]core.RunID, mutators)
	for i := range mutators {
		mutatorIDs[i] = runregistryFixtureRunID(t)
	}
	wg.Add(mutators)
	for i := range mutators {
		go func(i int) {
			defer wg.Done()
			reg.Register(mutatorIDs[i], runregistryFixtureHandle("bead-mut", "/wt/mut"))
			reg.Unregister(mutatorIDs[i])
		}(i)
	}

	wg.Wait()

	if snapshotLen < 0 || snapshotLen > preload+mutators {
		t.Fatalf("Snapshot len = %d, want 0..%d", snapshotLen, preload+mutators)
	}
}

// TestRunHandle_ResolvedProvider_UnsetByDefault asserts GetResolvedProvider
// returns ("", false) before SetResolvedProvider is ever called — the
// "not yet resolved" state distinct from "resolved to the empty string".
func TestRunHandle_ResolvedProvider_UnsetByDefault(t *testing.T) {
	h := runregistryFixtureHandle("bead-unresolved", "/wt/unresolved")
	if got, ok := h.GetResolvedProvider(); ok || got != "" {
		t.Fatalf("GetResolvedProvider() = (%q, %v), want (\"\", false)", got, ok)
	}
}

// TestRunHandle_ResolvedProvider_SetGet asserts a value set via
// SetResolvedProvider round-trips through GetResolvedProvider, including the
// harness-global-default case (empty string, but resolved: ok == true).
func TestRunHandle_ResolvedProvider_SetGet(t *testing.T) {
	h := runregistryFixtureHandle("bead-resolved", "/wt/resolved")

	h.SetResolvedProvider("openrouter")
	if got, ok := h.GetResolvedProvider(); !ok || got != "openrouter" {
		t.Fatalf("GetResolvedProvider() = (%q, %v), want (\"openrouter\", true)", got, ok)
	}

	h.SetResolvedProvider("")
	if got, ok := h.GetResolvedProvider(); !ok || got != "" {
		t.Fatalf("GetResolvedProvider() after resolving to empty = (%q, %v), want (\"\", true)", got, ok)
	}
}

// TestRunRegistry_LenForProvider asserts the per-provider tally counts only
// runs resolved to the named provider and excludes unresolved runs, mirroring
// LenForQueue's per-queue tally semantics.
func TestRunRegistry_LenForProvider(t *testing.T) {
	reg := daemon.NewRunRegistry()

	openrouterA := runregistryFixtureHandle("bead-or-a", "/wt/or-a")
	openrouterA.SetResolvedProvider("openrouter")
	openrouterB := runregistryFixtureHandle("bead-or-b", "/wt/or-b")
	openrouterB.SetResolvedProvider("openrouter")
	ornith := runregistryFixtureHandle("bead-ornith", "/wt/ornith")
	ornith.SetResolvedProvider("ornith")
	unresolved := runregistryFixtureHandle("bead-unresolved", "/wt/unresolved")

	reg.Register(runregistryFixtureRunID(t), openrouterA)
	reg.Register(runregistryFixtureRunID(t), openrouterB)
	reg.Register(runregistryFixtureRunID(t), ornith)
	reg.Register(runregistryFixtureRunID(t), unresolved)

	if n := reg.LenForProvider("openrouter"); n != 2 {
		t.Fatalf("LenForProvider(openrouter) = %d, want 2", n)
	}
	if n := reg.LenForProvider("ornith"); n != 1 {
		t.Fatalf("LenForProvider(ornith) = %d, want 1", n)
	}
	if n := reg.LenForProvider("nonexistent"); n != 0 {
		t.Fatalf("LenForProvider(nonexistent) = %d, want 0", n)
	}
}
