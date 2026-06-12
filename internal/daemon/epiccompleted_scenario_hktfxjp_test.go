//go:build scenario

package daemon

// epiccompleted_scenario_hktfxjp_test.go — daemon-level scenario coverage for the
// C1 "epic_completed" feature (bead hk-tfxjp / "T4").
//
// Exercises three load-bearing guarantees of the C1 implementation
// (maybeEmitEpicCompleted / emitBeadClosedAndMaybeEpic in workloop.go, the
// emittedEpics at-most-once guard, and the daemon.Start boot-seed scan in
// daemon.go):
//
//   (1) AC-1 — emit on last-child close: closing the last open child of an epic
//       emits exactly one epic_completed carrying {epic_id, last_child_bead_id,
//       closed_at}.
//   (2) AC-2 — at-most-once under sibling race: two closes (one via the daemon
//       close path emitBeadClosedAndMaybeEpic, one out-of-band via a direct
//       maybeEmitEpicCompleted call), in any order and concurrently, produce
//       exactly one emit; an idempotent re-close emits zero.
//   (3) AC-5 — boot survives restart: an epic_completed already in the durable
//       event log seeds the in-memory guard on (re)boot via the same
//       eventbus.ScanAfter scan daemon.Start runs, so a subsequent last-child
//       re-close emits zero.
//
// This is an internal (package daemon) test because it drives the unexported
// emit helpers (emitBeadClosedAndMaybeEpic / maybeEmitEpicCompleted), the
// unexported epicCompletedPayload, and ExportedWorkLoopDeps — none of which are
// reachable from package daemon_test.
//
// # Helper-prefix discipline
//
// Every helper/type/fixture added here is namespaced with the "hktfxjp" suffix
// so it can never redeclare a symbol that exists in another scenario test file
// in package daemon (parallel-helper-collision lesson).
//
// Spec refs:
//   - docs/plans/captain/05-specs/c1-spec.md §5 (AC-1, AC-2, AC-5) + §6 (scenario).
//   - docs/plans/captain/06-integration.md §6 (C1 per-component gate).
//
// Bead: hk-tfxjp (refs the C1 impl beads hk-w6y70, hk-o50hy).

import (
	"context"
	"encoding/json"
	"path/filepath"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/brcli"
	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/eventbus"
	"github.com/gregberns/harmonik/internal/handlercontract"
)

// hktfxjpNewRunID returns a fresh UUIDv7-based RunID (mirrors the
// staleFixtureNewRunID idiom; namespaced to avoid redeclaring it).
func hktfxjpNewRunID(t *testing.T) core.RunID {
	t.Helper()
	u, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("hktfxjpNewRunID: NewV7: %v", err)
	}
	return core.RunID(u)
}

// ─────────────────────────────────────────────────────────────────────────────
// hktfxjp fixtures
// ─────────────────────────────────────────────────────────────────────────────

// hktfxjpStubLedger is a beadLedger whose ShowBead returns canned records keyed
// by bead id. The C1 emit helper calls ShowBead twice per close — once for the
// closed child (to find its parent via the outgoing parent-child edge) and once
// for the parent (to enumerate sibling statuses). All other beadLedger methods
// are no-ops; maybeEmitEpicCompleted never calls them.
type hktfxjpStubLedger struct {
	mu      sync.Mutex
	records map[core.BeadID]core.BeadRecord
}

func hktfxjpNewStubLedger() *hktfxjpStubLedger {
	return &hktfxjpStubLedger{records: make(map[core.BeadID]core.BeadRecord)}
}

// set installs/overwrites the record returned for id.
func (l *hktfxjpStubLedger) set(id core.BeadID, rec core.BeadRecord) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.records[id] = rec
}

func (l *hktfxjpStubLedger) Ready(_ context.Context) ([]core.BeadRecord, error) {
	return nil, nil
}

func (l *hktfxjpStubLedger) ShowBead(_ context.Context, id core.BeadID) (core.BeadRecord, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if rec, ok := l.records[id]; ok {
		return rec, nil
	}
	// Unknown bead: return an empty-but-id'd record (no edges) so the helper's
	// "no parent" path (AC-4) is taken rather than erroring.
	return core.BeadRecord{BeadID: id, Status: core.CoarseStatusClosed}, nil
}

func (l *hktfxjpStubLedger) ClaimBead(_ context.Context, _ string, _ brcli.TimeoutConfig, _ core.RunID, _ core.TransitionID, _ core.BeadID) error {
	return nil
}

func (l *hktfxjpStubLedger) CloseBead(_ context.Context, _ string, _ brcli.TimeoutConfig, _ core.RunID, _ core.TransitionID, _ core.BeadID, _ bool) error {
	return nil
}

func (l *hktfxjpStubLedger) ReopenBead(_ context.Context, _ string, _ brcli.TimeoutConfig, _ core.RunID, _ core.TransitionID, _ core.BeadID, _ string) error {
	return nil
}

// hktfxjpCapturingBus is a handlercontract.EventEmitter that records every emit
// (type + run_id presence + raw payload) so a test can count epic_completed
// emissions and decode the payload. It does NOT write to disk — the durable-log
// half of AC-5 is exercised via a real eventbus.busImpl + JSONLWriter instead.
type hktfxjpCapturingBus struct {
	mu     sync.Mutex
	events []hktfxjpCapturedEvent
}

type hktfxjpCapturedEvent struct {
	Type    core.EventType
	HasRun  bool
	RunID   core.RunID
	Payload []byte
}

func hktfxjpNewCapturingBus() *hktfxjpCapturingBus {
	return &hktfxjpCapturingBus{}
}

func (b *hktfxjpCapturingBus) Emit(_ context.Context, eventType core.EventType, payload []byte) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	cp := append([]byte(nil), payload...)
	b.events = append(b.events, hktfxjpCapturedEvent{Type: eventType, Payload: cp})
	return nil
}

func (b *hktfxjpCapturingBus) EmitWithRunID(_ context.Context, runID core.RunID, eventType core.EventType, payload []byte) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	cp := append([]byte(nil), payload...)
	b.events = append(b.events, hktfxjpCapturedEvent{Type: eventType, HasRun: true, RunID: runID, Payload: cp})
	return nil
}

// countEpicCompleted returns the number of epic_completed events captured.
func (b *hktfxjpCapturingBus) countEpicCompleted() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := 0
	for _, e := range b.events {
		if e.Type == core.EventTypeEpicCompleted {
			n++
		}
	}
	return n
}

// firstEpicCompleted returns the captured epic_completed event (HasRun + Payload)
// and whether one was found.
func (b *hktfxjpCapturingBus) firstEpicCompleted() (hktfxjpCapturedEvent, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, e := range b.events {
		if e.Type == core.EventTypeEpicCompleted {
			return e, true
		}
	}
	return hktfxjpCapturedEvent{}, false
}

// hktfxjpChildEdge builds an incoming parent-child edge (FromBeadID == child,
// ToBeadID == parent) carrying the child's status — the shape a parent's
// dependents[] entry parses into per brcli/show.go.
func hktfxjpChildEdge(child, parent core.BeadID, status core.CoarseStatus) core.DependencyEdge {
	return core.DependencyEdge{
		FromBeadID:     child,
		ToBeadID:       parent,
		EdgeKind:       core.EdgeKindParentChild,
		EndpointStatus: status,
	}
}

// hktfxjpParentEdge builds an outgoing parent-child edge (FromBeadID == child,
// ToBeadID == parent) — the shape a child's dependencies[] entry parses into.
// The helper finds the parent via exactly this edge on the closed child's record.
func hktfxjpParentEdge(child, parent core.BeadID) core.DependencyEdge {
	return core.DependencyEdge{
		FromBeadID: child,
		ToBeadID:   parent,
		EdgeKind:   core.EdgeKindParentChild,
	}
}

// hktfxjpDeps builds a workLoopDeps wired to the stub ledger + capturing bus,
// with a fresh (empty) emittedEpics guard, via ExportedWorkLoopDeps. An optional
// seed map pre-populates the guard (used by the AC-5 boot-seed sub-test).
func hktfxjpDeps(t *testing.T, ledger beadLedger, bus handlercontract.EventEmitter, seed map[core.BeadID]struct{}) workLoopDeps {
	t.Helper()
	// AdapterRegistry2 is intentionally left nil: maybeEmitEpicCompleted /
	// emitBeadClosedAndMaybeEpic only touch brAdapter, bus, and the emittedEpics
	// guard — they never reach beadRunOne/waitAgentReady, which is the only
	// consumer of adapterRegistry. (NewSealedAdapterRegistryForTest lives in
	// package daemon_test and is unreachable from this internal-package file.)
	deps := ExportedWorkLoopDeps(WorkLoopDepsParams{
		BrAdapter:     ledger,
		Bus:           bus,
		ProjectDir:    t.TempDir(),
		HandlerBinary: "true",
		IntentLogDir:  t.TempDir(),
	})
	if seed != nil {
		deps.emittedEpics = seed
		deps.emittedEpicsMu = &sync.Mutex{}
	}
	return deps
}

// ─────────────────────────────────────────────────────────────────────────────
// AC-1 — emit on last-child close
// ─────────────────────────────────────────────────────────────────────────────

func TestScenario_EpicCompleted_EmitsOnLastChildClose_hktfxjp(t *testing.T) {
	skipRealDaemonE2EInShort(t)
	ctx := context.Background()

	const (
		epic  = core.BeadID("hk-tfxjp-epic-1")
		child = core.BeadID("hk-tfxjp-child-1")
	)

	ledger := hktfxjpNewStubLedger()
	// Closed child: its dependencies[] carry one outgoing parent-child edge → parent.
	ledger.set(child, core.BeadRecord{
		BeadID: child,
		Status: core.CoarseStatusClosed,
		Edges:  []core.DependencyEdge{hktfxjpParentEdge(child, epic)},
	})
	// Parent epic: its dependents[] carry one incoming parent-child edge for the
	// (now closed) child.
	ledger.set(epic, core.BeadRecord{
		BeadID: epic,
		Status: core.CoarseStatusOpen,
		Edges:  []core.DependencyEdge{hktfxjpChildEdge(child, epic, core.CoarseStatusClosed)},
	})

	bus := hktfxjpNewCapturingBus()
	deps := hktfxjpDeps(t, ledger, bus, nil)
	runID := hktfxjpNewRunID(t)

	// Drive the close through the real daemon close branch.
	emitBeadClosedAndMaybeEpic(ctx, deps, runID, child)

	if got := bus.countEpicCompleted(); got != 1 {
		t.Fatalf("AC-1: expected exactly 1 epic_completed, got %d", got)
	}

	ev, ok := bus.firstEpicCompleted()
	if !ok {
		t.Fatal("AC-1: no epic_completed captured")
	}
	if !ev.HasRun {
		t.Error("AC-1: epic_completed must be emitted via EmitWithRunID (run-scoped)")
	}
	var pl epicCompletedPayload
	if err := json.Unmarshal(ev.Payload, &pl); err != nil {
		t.Fatalf("AC-1: payload decode: %v", err)
	}
	if pl.EpicID != string(epic) {
		t.Errorf("AC-1: epic_id = %q, want %q", pl.EpicID, epic)
	}
	if pl.LastChildBeadID != string(child) {
		t.Errorf("AC-1: last_child_bead_id = %q, want %q", pl.LastChildBeadID, child)
	}
	if pl.ClosedAt == "" {
		t.Error("AC-1: closed_at must be non-empty")
	}
}

// AC-3 / AC-4 negative cases — bundled here to confirm the guard is not
// trigger-happy (one still-open sibling → zero; no parent → zero).
func TestScenario_EpicCompleted_NoEmitWhenNotComplete_hktfxjp(t *testing.T) {
	skipRealDaemonE2EInShort(t)
	ctx := context.Background()

	t.Run("AC-3_open_sibling", func(t *testing.T) {
		const (
			epic   = core.BeadID("hk-tfxjp-epic-3")
			closed = core.BeadID("hk-tfxjp-child-3a")
			open   = core.BeadID("hk-tfxjp-child-3b")
		)
		ledger := hktfxjpNewStubLedger()
		ledger.set(closed, core.BeadRecord{
			BeadID: closed,
			Status: core.CoarseStatusClosed,
			Edges:  []core.DependencyEdge{hktfxjpParentEdge(closed, epic)},
		})
		ledger.set(epic, core.BeadRecord{
			BeadID: epic,
			Status: core.CoarseStatusOpen,
			Edges: []core.DependencyEdge{
				hktfxjpChildEdge(closed, epic, core.CoarseStatusClosed),
				hktfxjpChildEdge(open, epic, core.CoarseStatusOpen),
			},
		})
		bus := hktfxjpNewCapturingBus()
		deps := hktfxjpDeps(t, ledger, bus, nil)
		emitBeadClosedAndMaybeEpic(ctx, deps, hktfxjpNewRunID(t), closed)
		if got := bus.countEpicCompleted(); got != 0 {
			t.Fatalf("AC-3: expected 0 epic_completed with an open sibling, got %d", got)
		}
	})

	t.Run("AC-4_no_parent", func(t *testing.T) {
		const standalone = core.BeadID("hk-tfxjp-standalone-4")
		ledger := hktfxjpNewStubLedger()
		ledger.set(standalone, core.BeadRecord{
			BeadID: standalone,
			Status: core.CoarseStatusClosed,
			Edges:  nil, // no parent-child edge
		})
		bus := hktfxjpNewCapturingBus()
		deps := hktfxjpDeps(t, ledger, bus, nil)
		emitBeadClosedAndMaybeEpic(ctx, deps, hktfxjpNewRunID(t), standalone)
		if got := bus.countEpicCompleted(); got != 0 {
			t.Fatalf("AC-4: expected 0 epic_completed for a parentless bead, got %d", got)
		}
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// AC-2 — at-most-once under sibling race + idempotent re-close
// ─────────────────────────────────────────────────────────────────────────────

func TestScenario_EpicCompleted_AtMostOnceUnderSiblingRace_hktfxjp(t *testing.T) {
	skipRealDaemonE2EInShort(t)
	ctx := context.Background()

	const (
		epic   = core.BeadID("hk-tfxjp-epic-2")
		childA = core.BeadID("hk-tfxjp-child-2a")
		childB = core.BeadID("hk-tfxjp-child-2b")
	)

	// Build a ledger where the epic has TWO children, both closed (the race
	// scenario: both siblings observe "all children closed" simultaneously). One
	// close comes via the daemon path, the other simulates an out-of-band close
	// (crew/human on their own queue) by invoking the emit helper directly.
	newLedger := func() *hktfxjpStubLedger {
		l := hktfxjpNewStubLedger()
		l.set(childA, core.BeadRecord{
			BeadID: childA, Status: core.CoarseStatusClosed,
			Edges: []core.DependencyEdge{hktfxjpParentEdge(childA, epic)},
		})
		l.set(childB, core.BeadRecord{
			BeadID: childB, Status: core.CoarseStatusClosed,
			Edges: []core.DependencyEdge{hktfxjpParentEdge(childB, epic)},
		})
		l.set(epic, core.BeadRecord{
			BeadID: epic, Status: core.CoarseStatusOpen,
			Edges: []core.DependencyEdge{
				hktfxjpChildEdge(childA, epic, core.CoarseStatusClosed),
				hktfxjpChildEdge(childB, epic, core.CoarseStatusClosed),
			},
		})
		return l
	}

	t.Run("sequential_two_closes_one_emit", func(t *testing.T) {
		ledger := newLedger()
		bus := hktfxjpNewCapturingBus()
		deps := hktfxjpDeps(t, ledger, bus, nil)
		// Daemon-path close of childA, then out-of-band close of childB.
		emitBeadClosedAndMaybeEpic(ctx, deps, hktfxjpNewRunID(t), childA)
		maybeEmitEpicCompleted(ctx, deps, hktfxjpNewRunID(t), childB)
		if got := bus.countEpicCompleted(); got != 1 {
			t.Fatalf("AC-2 sequential: expected exactly 1 emit across two sibling closes, got %d", got)
		}
	})

	t.Run("idempotent_reclose_zero", func(t *testing.T) {
		ledger := newLedger()
		bus := hktfxjpNewCapturingBus()
		deps := hktfxjpDeps(t, ledger, bus, nil)
		emitBeadClosedAndMaybeEpic(ctx, deps, hktfxjpNewRunID(t), childA)
		if got := bus.countEpicCompleted(); got != 1 {
			t.Fatalf("AC-2 idempotent: setup expected 1 emit, got %d", got)
		}
		// Re-close the same (already-closed) last child → guard must suppress.
		emitBeadClosedAndMaybeEpic(ctx, deps, hktfxjpNewRunID(t), childA)
		if got := bus.countEpicCompleted(); got != 1 {
			t.Fatalf("AC-2 idempotent: re-close emitted extra epic_completed; total = %d, want 1", got)
		}
	})

	t.Run("parallel_race_one_emit", func(t *testing.T) {
		// Fire both checks concurrently against the SAME deps (shared emittedEpics
		// guard) and assert the atomic check-and-set lets exactly one win.
		const iterations = 50
		for i := 0; i < iterations; i++ {
			ledger := newLedger()
			bus := hktfxjpNewCapturingBus()
			deps := hktfxjpDeps(t, ledger, bus, nil)

			var wg sync.WaitGroup
			start := make(chan struct{})
			wg.Add(2)
			go func() {
				defer wg.Done()
				<-start
				emitBeadClosedAndMaybeEpic(ctx, deps, hktfxjpNewRunID(t), childA)
			}()
			go func() {
				defer wg.Done()
				<-start
				maybeEmitEpicCompleted(ctx, deps, hktfxjpNewRunID(t), childB)
			}()
			close(start)
			wg.Wait()

			if got := bus.countEpicCompleted(); got != 1 {
				t.Fatalf("AC-2 parallel (iter %d): expected exactly 1 emit under concurrent close, got %d", i, got)
			}
		}
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// AC-5 — boot survives restart (boot scan seeds the guard from the durable log)
// ─────────────────────────────────────────────────────────────────────────────

func TestScenario_EpicCompleted_BootSeedSurvivesRestart_hktfxjp(t *testing.T) {
	skipRealDaemonE2EInShort(t)
	ctx := context.Background()

	const (
		epic  = core.BeadID("hk-tfxjp-epic-5")
		child = core.BeadID("hk-tfxjp-child-5")
	)

	// ── Pre-boot: write a real epic_completed to the durable JSONL log via a
	// real eventbus busImpl + JSONLWriter, exactly as the daemon does. This is
	// the durable-write half of the restart round-trip. ──
	jsonlPath := filepath.Join(t.TempDir(), "events.jsonl")
	writer, err := eventbus.OpenJSONLWriter(jsonlPath)
	if err != nil {
		t.Fatalf("AC-5: open JSONL writer: %v", err)
	}
	preBus := eventbus.NewBusImplWithWriter(core.NewRedactionRegistry(), writer)

	prePayload := core.EpicCompletedPayload{
		EpicID:          epic,
		LastChildBeadID: child,
		ClosedAt:        "2026-06-09T00:00:00Z",
	}
	plBytes, err := json.Marshal(prePayload)
	if err != nil {
		t.Fatalf("AC-5: marshal pre-boot payload: %v", err)
	}
	if err := preBus.EmitWithRunID(ctx, hktfxjpNewRunID(t), core.EventTypeEpicCompleted, plBytes); err != nil {
		t.Fatalf("AC-5: pre-boot emit: %v", err)
	}
	// Close the writer to flush the queued line to disk before scanning.
	if err := writer.Close(); err != nil {
		t.Fatalf("AC-5: close writer: %v", err)
	}

	// ── Boot: reconstruct the emittedEpics seed via the SAME eventbus.ScanAfter
	// scan + EpicCompletedPayload decode that daemon.Start runs (daemon.go C1
	// boot-seed block). ──
	seed := make(map[core.BeadID]struct{})
	for ev := range eventbus.ScanAfter(jsonlPath, core.EventID{}) {
		if core.EventType(ev.Type) != core.EventTypeEpicCompleted {
			continue
		}
		var pl core.EpicCompletedPayload
		if err := json.Unmarshal(ev.Payload, &pl); err != nil || !pl.Valid() {
			continue
		}
		seed[pl.EpicID] = struct{}{}
	}
	if _, ok := seed[epic]; !ok {
		t.Fatalf("AC-5: boot scan did not seed epic %q from the durable log (seed=%v)", epic, seed)
	}

	// ── Post-boot: a fresh daemon (deps seeded from the scan) re-closes the last
	// child of the already-completed epic → guard must suppress (zero emit). ──
	ledger := hktfxjpNewStubLedger()
	ledger.set(child, core.BeadRecord{
		BeadID: child, Status: core.CoarseStatusClosed,
		Edges: []core.DependencyEdge{hktfxjpParentEdge(child, epic)},
	})
	ledger.set(epic, core.BeadRecord{
		BeadID: epic, Status: core.CoarseStatusOpen,
		Edges: []core.DependencyEdge{hktfxjpChildEdge(child, epic, core.CoarseStatusClosed)},
	})

	bus := hktfxjpNewCapturingBus()
	deps := hktfxjpDeps(t, ledger, bus, seed)

	emitBeadClosedAndMaybeEpic(ctx, deps, hktfxjpNewRunID(t), child)
	if got := bus.countEpicCompleted(); got != 0 {
		t.Fatalf("AC-5: re-close after boot-seed emitted %d epic_completed, want 0 (boot scan must suppress)", got)
	}
}
