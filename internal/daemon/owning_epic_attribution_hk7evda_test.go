package daemon

// owning_epic_attribution_hk7evda_test.go — unit tests for owning-epic
// attribution denormalization added to run_failed / run_stale / run_completed
// payloads (hk-7evda, logmine F13).
//
// # What this tests
//
//  1. resolveOwningEpicFromRecord — three branches:
//     a. No parent-child edge in BeadRecord.Edges → returns ("", "").
//     b. Parent-child edge found, ShowBead fails → returns (epicID, "").
//     c. Parent-child edge found, ShowBead succeeds, epic has assignee →
//        returns (epicID, assignee).
//
//  2. emitRunCompleted JSON shape — owning_epic_id / owning_epic_assignee:
//     a. Both absent when owningEpicID is empty.
//     b. owning_epic_id present, owning_epic_assignee absent when assignee is "".
//     c. Both present and correct when both are non-empty.
//
// These are internal-package tests using the unexported helpers directly —
// no real git or tmux infrastructure needed.
//
// Bead ref: hk-7evda.

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/brcli"
	"github.com/gregberns/harmonik/internal/core"
)

// stubBeadLedger implements beadLedger for testing.
// ShowBead returns the record from showResults keyed by bead ID; if the key is
// absent it returns a sentinel error.
type stubBeadLedger struct {
	showResults map[core.BeadID]core.BeadRecord
	showErr     map[core.BeadID]error
}

func (s *stubBeadLedger) ShowBead(_ context.Context, id core.BeadID) (core.BeadRecord, error) {
	if err, ok := s.showErr[id]; ok {
		return core.BeadRecord{}, err
	}
	if rec, ok := s.showResults[id]; ok {
		return rec, nil
	}
	return core.BeadRecord{}, errors.New("stub: bead not found")
}

func (s *stubBeadLedger) Ready(_ context.Context) ([]core.BeadRecord, error) { return nil, nil }
func (s *stubBeadLedger) ClaimBead(_ context.Context, _ string, _ brcli.TimeoutConfig, _ core.RunID, _ core.TransitionID, _ core.BeadID) error {
	return nil
}

func (s *stubBeadLedger) CloseBead(_ context.Context, _ string, _ brcli.TimeoutConfig, _ core.RunID, _ core.TransitionID, _ core.BeadID, _ bool) error {
	return nil
}

func (s *stubBeadLedger) ReopenBead(_ context.Context, _ string, _ brcli.TimeoutConfig, _ core.RunID, _ core.TransitionID, _ core.BeadID, _ string) error {
	return nil
}

// compile-time check.
var _ beadLedger = (*stubBeadLedger)(nil)

// makeChildRecord builds a BeadRecord for childID with a parent-child edge
// pointing to parentID.
func makeChildRecord(childID, parentID core.BeadID) core.BeadRecord {
	return core.BeadRecord{
		BeadID:        childID,
		Title:         "child bead",
		BeadType:      "task",
		Status:        core.CoarseStatusOpen,
		AuditTrailRef: string(childID),
		Edges: []core.DependencyEdge{
			{
				FromBeadID: childID,
				ToBeadID:   parentID,
				EdgeKind:   core.EdgeKindParentChild,
			},
		},
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// resolveOwningEpicFromRecord — three branches
// ─────────────────────────────────────────────────────────────────────────────

// TestResolveOwningEpic_NoParentEdge verifies that ("", "") is returned when
// the bead has no parent-child edge (orphan bead or top-level epic).
func TestResolveOwningEpic_NoParentEdge(t *testing.T) {
	t.Parallel()

	childID := core.BeadID("hk-child-001")
	record := core.BeadRecord{
		BeadID:        childID,
		Title:         "orphan bead",
		BeadType:      "task",
		Status:        core.CoarseStatusOpen,
		AuditTrailRef: string(childID),
		// No Edges — no parent.
	}
	ledger := &stubBeadLedger{}

	epicID, assignee := resolveOwningEpicFromRecord(context.Background(), ledger, record)

	if epicID != "" {
		t.Errorf("epicID = %q; want empty (no parent-child edge)", epicID)
	}
	if assignee != "" {
		t.Errorf("assignee = %q; want empty (no parent-child edge)", assignee)
	}
}

// TestResolveOwningEpic_ShowBeadError verifies that (epicID, "") is returned
// when a parent-child edge is found but ShowBead on the epic fails.
func TestResolveOwningEpic_ShowBeadError(t *testing.T) {
	t.Parallel()

	childID := core.BeadID("hk-child-002")
	epicID := core.BeadID("hk-epic-002")
	record := makeChildRecord(childID, epicID)

	ledger := &stubBeadLedger{
		showErr: map[core.BeadID]error{
			epicID: errors.New("br: unavailable"),
		},
	}

	gotEpicID, gotAssignee := resolveOwningEpicFromRecord(context.Background(), ledger, record)

	if gotEpicID != string(epicID) {
		t.Errorf("epicID = %q; want %q", gotEpicID, epicID)
	}
	if gotAssignee != "" {
		t.Errorf("assignee = %q; want empty (ShowBead failed)", gotAssignee)
	}
}

// TestResolveOwningEpic_Success verifies that (epicID, assignee) is returned
// when a parent-child edge is found and ShowBead succeeds with an assignee.
func TestResolveOwningEpic_Success(t *testing.T) {
	t.Parallel()

	childID := core.BeadID("hk-child-003")
	epicID := core.BeadID("hk-epic-003")
	record := makeChildRecord(childID, epicID)

	epicRecord := core.BeadRecord{
		BeadID:        epicID,
		Title:         "owner epic",
		BeadType:      "epic",
		Status:        core.CoarseStatusOpen,
		AuditTrailRef: string(epicID),
		Assignee:      "chani",
	}
	ledger := &stubBeadLedger{
		showResults: map[core.BeadID]core.BeadRecord{epicID: epicRecord},
	}

	gotEpicID, gotAssignee := resolveOwningEpicFromRecord(context.Background(), ledger, record)

	if gotEpicID != string(epicID) {
		t.Errorf("epicID = %q; want %q", gotEpicID, epicID)
	}
	if gotAssignee != "chani" {
		t.Errorf("assignee = %q; want %q", gotAssignee, "chani")
	}
}

// TestResolveOwningEpic_NoAssigneeOnEpic verifies that (epicID, "") is returned
// when ShowBead succeeds but the epic has no assignee (unowned epic).
func TestResolveOwningEpic_NoAssigneeOnEpic(t *testing.T) {
	t.Parallel()

	childID := core.BeadID("hk-child-004")
	epicID := core.BeadID("hk-epic-004")
	record := makeChildRecord(childID, epicID)

	epicRecord := core.BeadRecord{
		BeadID:        epicID,
		Title:         "unassigned epic",
		BeadType:      "epic",
		Status:        core.CoarseStatusOpen,
		AuditTrailRef: string(epicID),
		Assignee:      "", // no assignee
	}
	ledger := &stubBeadLedger{
		showResults: map[core.BeadID]core.BeadRecord{epicID: epicRecord},
	}

	gotEpicID, gotAssignee := resolveOwningEpicFromRecord(context.Background(), ledger, record)

	if gotEpicID != string(epicID) {
		t.Errorf("epicID = %q; want %q", gotEpicID, epicID)
	}
	if gotAssignee != "" {
		t.Errorf("assignee = %q; want empty (epic has no assignee)", gotAssignee)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// emitRunCompleted JSON shape — owning_epic_id / owning_epic_assignee
// ─────────────────────────────────────────────────────────────────────────────

// TestOwningEpicFields_AbsentWhenEmpty verifies that owning_epic_id and
// owning_epic_assignee are absent from the emitted JSON when both are "".
func TestOwningEpicFields_AbsentWhenEmpty(t *testing.T) {
	t.Parallel()

	bus := &payloadCollector{}
	runID := core.RunID(uuid.New())

	emitRunCompleted(context.Background(), bus, runID, "hk-bead-001", "", "", false, "no-progress", nil, nil, nil)

	if len(bus.events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(bus.events))
	}
	raw := string(bus.events[0].payload)
	if strings.Contains(raw, "owning_epic_id") {
		t.Errorf("owning_epic_id must be absent when empty; payload=%s", raw)
	}
	if strings.Contains(raw, "owning_epic_assignee") {
		t.Errorf("owning_epic_assignee must be absent when empty; payload=%s", raw)
	}
}

// TestOwningEpicFields_EpicIDPresentAssigneeAbsent verifies that owning_epic_id
// appears but owning_epic_assignee is absent when epicID is set but assignee is "".
func TestOwningEpicFields_EpicIDPresentAssigneeAbsent(t *testing.T) {
	t.Parallel()

	bus := &payloadCollector{}
	runID := core.RunID(uuid.New())

	emitRunCompleted(context.Background(), bus, runID, "hk-bead-002", "hk-epic-002", "", false, "no-progress", nil, nil, nil)

	if len(bus.events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(bus.events))
	}
	raw := string(bus.events[0].payload)
	if !strings.Contains(raw, `"owning_epic_id":"hk-epic-002"`) {
		t.Errorf("owning_epic_id must be present; payload=%s", raw)
	}
	if strings.Contains(raw, "owning_epic_assignee") {
		t.Errorf("owning_epic_assignee must be absent when assignee is empty; payload=%s", raw)
	}
}

// TestOwningEpicFields_BothPresentAndCorrect verifies that both owning_epic_id
// and owning_epic_assignee appear with correct values when both are non-empty.
func TestOwningEpicFields_BothPresentAndCorrect(t *testing.T) {
	t.Parallel()

	bus := &payloadCollector{}
	runID := core.RunID(uuid.New())

	emitRunCompleted(context.Background(), bus, runID, "hk-bead-003", "hk-epic-003", "chani", true, "done", nil, nil, nil)

	if len(bus.events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(bus.events))
	}
	ev := bus.events[0]
	if ev.eventType != core.EventTypeRunCompleted {
		t.Errorf("event type = %q; want run_completed", ev.eventType)
	}

	var pl workloopRunCompletedPayload
	if err := json.Unmarshal(ev.payload, &pl); err != nil {
		t.Fatalf("unmarshal payload: %v\n  raw: %s", err, ev.payload)
	}

	if pl.BeadID != "hk-bead-003" {
		t.Errorf("bead_id = %q; want %q", pl.BeadID, "hk-bead-003")
	}
	if pl.OwningEpicID == nil || *pl.OwningEpicID != "hk-epic-003" {
		t.Errorf("owning_epic_id = %v; want %q", pl.OwningEpicID, "hk-epic-003")
	}
	if pl.OwningEpicAssignee == nil || *pl.OwningEpicAssignee != "chani" {
		t.Errorf("owning_epic_assignee = %v; want %q", pl.OwningEpicAssignee, "chani")
	}

	raw := string(ev.payload)
	if !strings.Contains(raw, `"owning_epic_id":"hk-epic-003"`) {
		t.Errorf("raw JSON missing owning_epic_id; payload=%s", raw)
	}
	if !strings.Contains(raw, `"owning_epic_assignee":"chani"`) {
		t.Errorf("raw JSON missing owning_epic_assignee; payload=%s", raw)
	}
}
