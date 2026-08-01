package daemon

// epiccompleted_tombstone_test.go — a tombstoned child must not suppress
// epic_completed for its parent.
//
// maybeEmitEpicCompleted used to test each child edge against
// core.CoarseStatusClosed. A tombstoned child is finished, but it never becomes
// closed, so one tombstone held its parent's epic_completed back forever. The
// lane waiting on that event stopped with nothing red and nothing logged. The
// check is now core.CoarseStatus.IsTerminal(), which admits closed and tombstone
// and nothing else.
//
// Two cases, because the fix must not go the other way either:
//   - a two-child epic with one tombstoned child and one closed child emits;
//   - the same epic with one tombstoned child and one OPEN child stays silent.
//
// This is an internal (package daemon) test: it drives the unexported
// maybeEmitEpicCompleted and builds workLoopDeps through ExportedWorkLoopDeps.
// It carries no build tag on purpose — the scenario-tier coverage of this helper
// in epiccompleted_scenario_hktfxjp_test.go does not run in check-fast or
// check-short, and this behaviour is cheap enough to gate on every commit.
//
// Every helper here is namespaced with an "epictomb" prefix so it cannot
// redeclare a symbol in another test file of package daemon — the scenario file
// above declares the same fixtures under its own prefix and both compile
// together under -tags scenario.
//
// Refs: DECOMPOSITION-MAP.md step 19; plans/2026-07-27-delete-and-rewrite/LANES.md §7.

import (
	"context"
	"encoding/json"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/brcli"
	"github.com/gregberns/harmonik/internal/core"
)

// epictombStubLedger is a beadLedger whose ShowBead returns canned records keyed
// by bead id. maybeEmitEpicCompleted calls ShowBead twice — once for the closed
// child, to find its parent, and once for the parent, to read every sibling's
// status. No other beadLedger method is reached.
type epictombStubLedger struct {
	mu      sync.Mutex
	records map[core.BeadID]core.BeadRecord
}

func epictombNewStubLedger() *epictombStubLedger {
	return &epictombStubLedger{records: make(map[core.BeadID]core.BeadRecord)}
}

func (l *epictombStubLedger) set(id core.BeadID, rec core.BeadRecord) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.records[id] = rec
}

func (l *epictombStubLedger) Ready(_ context.Context) ([]core.BeadRecord, error) { return nil, nil }

func (l *epictombStubLedger) ShowBead(_ context.Context, id core.BeadID) (core.BeadRecord, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if rec, ok := l.records[id]; ok {
		return rec, nil
	}
	return core.BeadRecord{BeadID: id, Status: core.CoarseStatusClosed}, nil
}

func (l *epictombStubLedger) ClaimBead(_ context.Context, _ string, _ brcli.TimeoutConfig, _ core.RunID, _ core.TransitionID, _ core.BeadID) error {
	return nil
}

func (l *epictombStubLedger) CloseBead(_ context.Context, _ string, _ brcli.TimeoutConfig, _ core.RunID, _ core.TransitionID, _ core.BeadID, _ bool) error {
	return nil
}

func (l *epictombStubLedger) ReopenBead(_ context.Context, _ string, _ brcli.TimeoutConfig, _ core.RunID, _ core.TransitionID, _ core.BeadID, _ string) error {
	return nil
}

// epictombCapturingBus records every emit so a test can count epic_completed and
// decode its payload.
type epictombCapturingBus struct {
	mu     sync.Mutex
	events []epictombCapturedEvent
}

type epictombCapturedEvent struct {
	Type    core.EventType
	Payload []byte
}

func epictombNewCapturingBus() *epictombCapturingBus { return &epictombCapturingBus{} }

func (b *epictombCapturingBus) Emit(_ context.Context, eventType core.EventType, payload []byte) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.events = append(b.events, epictombCapturedEvent{Type: eventType, Payload: append([]byte(nil), payload...)})
	return nil
}

func (b *epictombCapturingBus) EmitWithRunID(_ context.Context, _ core.RunID, eventType core.EventType, payload []byte) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.events = append(b.events, epictombCapturedEvent{Type: eventType, Payload: append([]byte(nil), payload...)})
	return nil
}

// epicCompleted returns the captured epic_completed events.
func (b *epictombCapturingBus) epicCompleted() []epictombCapturedEvent {
	b.mu.Lock()
	defer b.mu.Unlock()
	var out []epictombCapturedEvent
	for _, e := range b.events {
		if e.Type == core.EventTypeEpicCompleted {
			out = append(out, e)
		}
	}
	return out
}

// epictombChildEdge builds the incoming parent-child edge a parent's dependents[]
// entry parses into: FromBeadID is the child, ToBeadID is the parent, and
// EndpointStatus carries the child's status.
func epictombChildEdge(child, parent core.BeadID, status core.CoarseStatus) core.DependencyEdge {
	return core.DependencyEdge{
		FromBeadID:     child,
		ToBeadID:       parent,
		EdgeKind:       core.EdgeKindParentChild,
		EndpointStatus: status,
	}
}

// epictombParentEdge builds the outgoing parent-child edge a child's
// dependencies[] entry parses into. maybeEmitEpicCompleted finds the parent
// through exactly this edge.
func epictombParentEdge(child, parent core.BeadID) core.DependencyEdge {
	return core.DependencyEdge{
		FromBeadID: child,
		ToBeadID:   parent,
		EdgeKind:   core.EdgeKindParentChild,
	}
}

func epictombDeps(t *testing.T, ledger beadLedger, bus *epictombCapturingBus) workLoopDeps {
	t.Helper()
	// AdapterRegistry2 stays nil: this path touches only the ledger, the bus and
	// the emittedEpics guard.
	return ExportedWorkLoopDeps(WorkLoopDepsParams{
		BrAdapter:     ledger,
		Bus:           bus,
		ProjectDir:    t.TempDir(),
		HandlerBinary: "true",
		IntentLogDir:  t.TempDir(),
	})
}

func epictombNewRunID(t *testing.T) core.RunID {
	t.Helper()
	u, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("epictombNewRunID: NewV7: %v", err)
	}
	return core.RunID(u)
}

// epictombLedger wires a two-child epic whose second child carries siblingStatus.
// It returns the ledger, the epic's id, and the id of the child that is closed.
func epictombLedger(t *testing.T, siblingStatus core.CoarseStatus) (ledger *epictombStubLedger, epicID, closedChildID core.BeadID) {
	t.Helper()
	const (
		epic    = core.BeadID("hk-epictomb-epic")
		closed  = core.BeadID("hk-epictomb-child-closed")
		sibling = core.BeadID("hk-epictomb-child-sibling")
	)

	l := epictombNewStubLedger()
	l.set(closed, core.BeadRecord{
		BeadID: closed,
		Status: core.CoarseStatusClosed,
		Edges:  []core.DependencyEdge{epictombParentEdge(closed, epic)},
	})
	l.set(epic, core.BeadRecord{
		BeadID: epic,
		Status: core.CoarseStatusOpen,
		Edges: []core.DependencyEdge{
			epictombChildEdge(closed, epic, core.CoarseStatusClosed),
			epictombChildEdge(sibling, epic, siblingStatus),
		},
	})
	return l, epic, closed
}

// A tombstoned sibling is finished. The epic completes when the last other child
// closes, and the event must fire.
func TestEpicCompleted_TombstonedChildDoesNotSuppressEmit(t *testing.T) {
	ctx := context.Background()
	ledger, epic, closed := epictombLedger(t, core.CoarseStatusTombstone)
	bus := epictombNewCapturingBus()
	deps := epictombDeps(t, ledger, bus)

	maybeEmitEpicCompleted(ctx, deps.runPorts(), deps.sharedHandles(), epictombNewRunID(t), closed)

	got := bus.epicCompleted()
	if len(got) != 1 {
		t.Fatalf("tombstoned sibling: want exactly 1 epic_completed, got %d", len(got))
	}

	var pl struct {
		EpicID          string `json:"epic_id"`
		LastChildBeadID string `json:"last_child_bead_id"`
	}
	if err := json.Unmarshal(got[0].Payload, &pl); err != nil {
		t.Fatalf("decode epic_completed payload: %v", err)
	}
	if pl.EpicID != string(epic) {
		t.Errorf("epic_id: got %q, want %q", pl.EpicID, epic)
	}
	if pl.LastChildBeadID != string(closed) {
		t.Errorf("last_child_bead_id: got %q, want %q", pl.LastChildBeadID, closed)
	}
}

// The other direction. A tombstone is terminal, an open child is not, and one
// open child still holds the epic back.
func TestEpicCompleted_OpenChildStillSuppressesEmit(t *testing.T) {
	ctx := context.Background()
	ledger, _, closed := epictombLedger(t, core.CoarseStatusOpen)
	bus := epictombNewCapturingBus()
	deps := epictombDeps(t, ledger, bus)

	maybeEmitEpicCompleted(ctx, deps.runPorts(), deps.sharedHandles(), epictombNewRunID(t), closed)

	if got := bus.epicCompleted(); len(got) != 0 {
		t.Fatalf("open sibling: want 0 epic_completed, got %d", len(got))
	}
}
