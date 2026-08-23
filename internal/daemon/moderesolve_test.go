package daemon_test

import (
	"context"
	"encoding/json"
	"sync"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/handlercontract"
)

func modeResolveFixtureBead(t *testing.T, labels []string) core.BeadRecord {
	t.Helper()
	return core.BeadRecord{
		BeadID:        core.BeadID("test-bead-001"),
		Title:         "fixture bead",
		BeadType:      "task",
		Status:        core.CoarseStatusOpen,
		Labels:        labels,
		AuditTrailRef: "test-bead-001",
	}
}

type modeResolveFixtureBus struct {
	mu     sync.Mutex
	events []modeResolveFixtureEvent
}

type modeResolveFixtureEvent struct {
	EventType core.EventType
	Payload   []byte
}

func (b *modeResolveFixtureBus) Emit(_ context.Context, et core.EventType, payload []byte) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.events = append(b.events, modeResolveFixtureEvent{EventType: et, Payload: payload})
	return nil
}

// EmitWithRunID satisfies handlercontract.EventEmitter; run_id is not stored
// because mode-resolution events are not run-scoped.
func (b *modeResolveFixtureBus) EmitWithRunID(ctx context.Context, _ core.RunID, et core.EventType, payload []byte) error {
	return b.Emit(ctx, et, payload)
}

var _ handlercontract.EventEmitter = (*modeResolveFixtureBus)(nil)

func modeResolveFixtureBusEvents(t *testing.T, bus *modeResolveFixtureBus) []modeResolveFixtureEvent {
	t.Helper()
	bus.mu.Lock()
	defer bus.mu.Unlock()
	out := make([]modeResolveFixtureEvent, len(bus.events))
	copy(out, bus.events)
	return out
}

// TestResolveWorkflowModePrecedence is the primary table-driven test covering
// all four tiers of EM-012a resolution and the conflict-event paths.
func TestResolveWorkflowModePrecedence(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name          string
		beadLabels    []string
		daemonDefault core.WorkflowMode
		wantMode      core.WorkflowMode
		// wantConflictEvent: when true, exactly one bead_label_conflict event
		// must be emitted.
		wantConflictEvent bool
	}{
		{
			name:          "tier1 single label overrides daemon default",
			beadLabels:    []string{"area:daemon", "workflow:single"},
			daemonDefault: core.WorkflowModeDot,
			wantMode:      core.WorkflowModeSingle,
		},
		{
			name:              "tier1 retired review-loop label is unknown: conflict, falls to tier3",
			beadLabels:        []string{"workflow:review-loop"},
			daemonDefault:     core.WorkflowModeSingle,
			wantMode:          core.WorkflowModeSingle,
			wantConflictEvent: true,
		},
		{
			name:          "tier1 dot label overrides daemon default",
			beadLabels:    []string{"workflow:dot"},
			daemonDefault: core.WorkflowModeSingle,
			wantMode:      core.WorkflowModeDot,
		},

		{
			name:              "tier1 conflict multiple labels emits bead_label_conflict and falls to tier3",
			beadLabels:        []string{"workflow:single", "workflow:review-loop"},
			daemonDefault:     core.WorkflowModeDot,
			wantMode:          core.WorkflowModeDot,
			wantConflictEvent: true,
		},
		{
			name:              "tier1 conflict multiple labels falls to tier4 when daemon default absent",
			beadLabels:        []string{"workflow:review-loop", "workflow:dot"},
			daemonDefault:     core.WorkflowMode(""), // invalid / absent
			wantMode:          core.WorkflowModeDot,  // hk-30vlb: tier4 fallback is now dot
			wantConflictEvent: true,
		},

		{
			name:              "tier1 unknown mode value emits bead_label_conflict and falls to tier3",
			beadLabels:        []string{"workflow:bogus"},
			daemonDefault:     core.WorkflowModeDot,
			wantMode:          core.WorkflowModeDot,
			wantConflictEvent: true,
		},
		{
			name:              "tier1 unknown mode value falls to tier4 when daemon default absent",
			beadLabels:        []string{"workflow:"},
			daemonDefault:     core.WorkflowMode(""),
			wantMode:          core.WorkflowModeDot, // hk-30vlb: tier4 fallback is now dot
			wantConflictEvent: true,
		},

		{
			name:          "tier3 daemon default single when no bead label",
			beadLabels:    nil,
			daemonDefault: core.WorkflowModeSingle,
			wantMode:      core.WorkflowModeSingle,
		},
		{
			name:          "tier3 retired review-loop daemon default is invalid: falls to tier4 dot",
			beadLabels:    []string{"area:brcli"}, // non-workflow label; tier 1 absent
			daemonDefault: core.WorkflowMode(core.WorkflowModeRetiredReviewLoop),
			wantMode:      core.WorkflowModeDot,
		},
		{
			name:          "tier3 daemon default dot when no bead label (v1.0 production default)",
			beadLabels:    nil,
			daemonDefault: core.WorkflowModeDot,
			wantMode:      core.WorkflowModeDot,
		},

		{
			name:          "tier4 fallback to dot when no bead label and no daemon default",
			beadLabels:    nil,
			daemonDefault: core.WorkflowMode(""), // empty → invalid
			wantMode:      core.WorkflowModeDot,
		},
		{
			name:          "tier4 fallback to dot when bead has unrelated labels only",
			beadLabels:    []string{"size:S", "area:core"},
			daemonDefault: core.WorkflowMode(""),
			wantMode:      core.WorkflowModeDot,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			bus := &modeResolveFixtureBus{}
			bead := modeResolveFixtureBead(t, tc.beadLabels)

			got := daemon.ExportedResolveWorkflowMode(t.Context(), bead, tc.daemonDefault, bus)

			if got != tc.wantMode {
				t.Errorf("resolveWorkflowMode: got %q; want %q", got, tc.wantMode)
			}

			events := modeResolveFixtureBusEvents(t, bus)
			conflictEvents := 0
			for _, e := range events {
				if e.EventType == core.EventTypeBeadLabelConflict {
					conflictEvents++
				}
			}
			if tc.wantConflictEvent && conflictEvents == 0 {
				t.Error("resolveWorkflowMode: expected bead_label_conflict event; none emitted")
			}
			if !tc.wantConflictEvent && conflictEvents > 0 {
				t.Errorf("resolveWorkflowMode: unexpected bead_label_conflict event(s): %d emitted", conflictEvents)
			}
		})
	}
}

// TestResolveWorkflowModeConflictPayloadShape verifies that when a
// bead_label_conflict event is emitted, its payload satisfies
// BeadLabelConflictPayload.Valid() and carries the expected bead_id.
func TestResolveWorkflowModeConflictPayloadShape(t *testing.T) {
	t.Parallel()

	bus := &modeResolveFixtureBus{}
	bead := modeResolveFixtureBead(t, []string{"workflow:single", "workflow:review-loop"})

	daemon.ExportedResolveWorkflowMode(t.Context(), bead, core.WorkflowModeSingle, bus)

	events := modeResolveFixtureBusEvents(t, bus)
	var conflictEvent *modeResolveFixtureEvent
	for i := range events {
		if events[i].EventType == core.EventTypeBeadLabelConflict {
			conflictEvent = &events[i]
			break
		}
	}
	if conflictEvent == nil {
		t.Fatal("resolveWorkflowMode: no bead_label_conflict event emitted for multi-label conflict")
	}

	var pl core.BeadLabelConflictPayload
	if err := json.Unmarshal(conflictEvent.Payload, &pl); err != nil {
		t.Fatalf("resolveWorkflowMode: bead_label_conflict payload unmarshal: %v", err)
	}
	if !pl.Valid() {
		t.Errorf("resolveWorkflowMode: BeadLabelConflictPayload.Valid() = false; payload = %+v", pl)
	}
	if pl.BeadID != string(bead.BeadID) {
		t.Errorf("resolveWorkflowMode: bead_label_conflict bead_id = %q; want %q", pl.BeadID, bead.BeadID)
	}
	if len(pl.ConflictingLabels) == 0 {
		t.Error("resolveWorkflowMode: bead_label_conflict conflicting_labels is empty")
	}
}

// TestResolveWorkflowModeResultIsValidMode verifies the resolved value is always
// a declared WorkflowMode constant regardless of inputs.
func TestResolveWorkflowModeResultIsValidMode(t *testing.T) {
	t.Parallel()

	inputs := []struct {
		labels        []string
		daemonDefault core.WorkflowMode
	}{
		{nil, core.WorkflowModeSingle},
		{nil, core.WorkflowMode("")},
		{[]string{"workflow:single"}, core.WorkflowMode("")},
		{[]string{"workflow:review-loop"}, core.WorkflowModeSingle},
		{[]string{"workflow:dot"}, core.WorkflowModeSingle},
		{[]string{"workflow:bogus"}, core.WorkflowModeDot},
		{[]string{"workflow:single", "workflow:review-loop"}, core.WorkflowModeDot},
		// A stale review-loop daemon default must still resolve to a VALID mode.
		{[]string{"area:core"}, core.WorkflowMode(core.WorkflowModeRetiredReviewLoop)},
		{[]string{"area:core"}, core.WorkflowMode("")},
	}

	for _, in := range inputs {
		bus := &modeResolveFixtureBus{}
		bead := modeResolveFixtureBead(t, in.labels)
		got := daemon.ExportedResolveWorkflowMode(t.Context(), bead, in.daemonDefault, bus)
		if !got.Valid() {
			t.Errorf("resolveWorkflowMode: result %q is not a valid WorkflowMode; labels=%v daemonDefault=%q",
				got, in.labels, in.daemonDefault)
		}
	}
}
