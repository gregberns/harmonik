//go:build scenario

package daemon

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

func hktfxjpNewRunID(t *testing.T) core.RunID {
	t.Helper()
	u, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("hktfxjpNewRunID: NewV7: %v", err)
	}
	return core.RunID(u)
}

type hktfxjpStubLedger struct {
	mu      sync.Mutex
	records map[core.BeadID]core.BeadRecord
}

func hktfxjpNewStubLedger() *hktfxjpStubLedger {
	return &hktfxjpStubLedger{records: make(map[core.BeadID]core.BeadRecord)}
}

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

func hktfxjpChildEdge(child, parent core.BeadID, status core.CoarseStatus) core.DependencyEdge {
	return core.DependencyEdge{
		FromBeadID:     child,
		ToBeadID:       parent,
		EdgeKind:       core.EdgeKindParentChild,
		EndpointStatus: status,
	}
}

func hktfxjpParentEdge(child, parent core.BeadID) core.DependencyEdge {
	return core.DependencyEdge{
		FromBeadID: child,
		ToBeadID:   parent,
		EdgeKind:   core.EdgeKindParentChild,
	}
}

func hktfxjpDeps(t *testing.T, ledger beadLedger, bus handlercontract.EventEmitter, seed map[core.BeadID]struct{}) testRuntime {
	t.Helper()
	deps := ExportedTestRuntime(TestRuntimeParams{
		BrAdapter:     ledger,
		Bus:           bus,
		ProjectDir:    t.TempDir(),
		HandlerBinary: "true",
		IntentLogDir:  t.TempDir(),
	})
	if seed != nil {
		deps.handles.EmittedEpics = seed
		deps.handles.EmittedEpicsMu = &sync.Mutex{}
	}
	return deps
}

func TestScenario_EpicCompleted_EmitsOnLastChildClose_hktfxjp(t *testing.T) {
	skipRealDaemonE2EInShort(t)
	ctx := context.Background()

	const (
		epic  = core.BeadID("hk-tfxjp-epic-1")
		child = core.BeadID("hk-tfxjp-child-1")
	)

	ledger := hktfxjpNewStubLedger()
	ledger.set(child, core.BeadRecord{
		BeadID: child,
		Status: core.CoarseStatusClosed,
		Edges:  []core.DependencyEdge{hktfxjpParentEdge(child, epic)},
	})
	ledger.set(epic, core.BeadRecord{
		BeadID: epic,
		Status: core.CoarseStatusOpen,
		Edges:  []core.DependencyEdge{hktfxjpChildEdge(child, epic, core.CoarseStatusClosed)},
	})

	bus := hktfxjpNewCapturingBus()
	deps := hktfxjpDeps(t, ledger, bus, nil)
	runID := hktfxjpNewRunID(t)

	emitBeadClosedAndMaybeEpic(ctx, deps.runPorts(), deps.sharedHandles(), runID, child)

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
		emitBeadClosedAndMaybeEpic(ctx, deps.runPorts(), deps.sharedHandles(), hktfxjpNewRunID(t), closed)
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
		emitBeadClosedAndMaybeEpic(ctx, deps.runPorts(), deps.sharedHandles(), hktfxjpNewRunID(t), standalone)
		if got := bus.countEpicCompleted(); got != 0 {
			t.Fatalf("AC-4: expected 0 epic_completed for a parentless bead, got %d", got)
		}
	})
}

func TestScenario_EpicCompleted_AtMostOnceUnderSiblingRace_hktfxjp(t *testing.T) {
	skipRealDaemonE2EInShort(t)
	ctx := context.Background()

	const (
		epic   = core.BeadID("hk-tfxjp-epic-2")
		childA = core.BeadID("hk-tfxjp-child-2a")
		childB = core.BeadID("hk-tfxjp-child-2b")
	)

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
		emitBeadClosedAndMaybeEpic(ctx, deps.runPorts(), deps.sharedHandles(), hktfxjpNewRunID(t), childA)
		maybeEmitEpicCompleted(ctx, deps.runPorts(), deps.sharedHandles(), hktfxjpNewRunID(t), childB)
		if got := bus.countEpicCompleted(); got != 1 {
			t.Fatalf("AC-2 sequential: expected exactly 1 emit across two sibling closes, got %d", got)
		}
	})

	t.Run("idempotent_reclose_zero", func(t *testing.T) {
		ledger := newLedger()
		bus := hktfxjpNewCapturingBus()
		deps := hktfxjpDeps(t, ledger, bus, nil)
		emitBeadClosedAndMaybeEpic(ctx, deps.runPorts(), deps.sharedHandles(), hktfxjpNewRunID(t), childA)
		if got := bus.countEpicCompleted(); got != 1 {
			t.Fatalf("AC-2 idempotent: setup expected 1 emit, got %d", got)
		}
		emitBeadClosedAndMaybeEpic(ctx, deps.runPorts(), deps.sharedHandles(), hktfxjpNewRunID(t), childA)
		if got := bus.countEpicCompleted(); got != 1 {
			t.Fatalf("AC-2 idempotent: re-close emitted extra epic_completed; total = %d, want 1", got)
		}
	})

	t.Run("parallel_race_one_emit", func(t *testing.T) {
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
				emitBeadClosedAndMaybeEpic(ctx, deps.runPorts(), deps.sharedHandles(), hktfxjpNewRunID(t), childA)
			}()
			go func() {
				defer wg.Done()
				<-start
				maybeEmitEpicCompleted(ctx, deps.runPorts(), deps.sharedHandles(), hktfxjpNewRunID(t), childB)
			}()
			close(start)
			wg.Wait()

			if got := bus.countEpicCompleted(); got != 1 {
				t.Fatalf("AC-2 parallel (iter %d): expected exactly 1 emit under concurrent close, got %d", i, got)
			}
		}
	})
}

func TestScenario_EpicCompleted_BootSeedSurvivesRestart_hktfxjp(t *testing.T) {
	skipRealDaemonE2EInShort(t)
	ctx := context.Background()

	const (
		epic  = core.BeadID("hk-tfxjp-epic-5")
		child = core.BeadID("hk-tfxjp-child-5")
	)

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
	if err := writer.Close(); err != nil {
		t.Fatalf("AC-5: close writer: %v", err)
	}

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

	emitBeadClosedAndMaybeEpic(ctx, deps.runPorts(), deps.sharedHandles(), hktfxjpNewRunID(t), child)
	if got := bus.countEpicCompleted(); got != 0 {
		t.Fatalf("AC-5: re-close after boot-seed emitted %d epic_completed, want 0 (boot scan must suppress)", got)
	}
}
