package brcli_test

// workflowlabelconflict_test.go — unit tests for BI-009a multi-workflow-label
// conflict detection (workflowlabelconflict.go).
//
// Spec refs:
//   - specs/beads-integration.md §4.3 BI-009a
//   - specs/event-model.md §8.8.6
//
// Coverage:
//   - Two workflow:<mode> labels → Conflicted=true, one event emitted.
//   - Three workflow:<mode> labels → Conflicted=true, one event emitted.
//   - Single unknown-mode workflow:<mode> label → Conflicted=true.
//   - Single valid workflow:single → Conflicted=false, no event.
//   - Single valid workflow:review-loop → Conflicted=false.
//   - Single valid workflow:dot → Conflicted=false.
//   - No workflow: labels → Conflicted=false, no event.
//   - Structured-log fallback: nil bus does not panic; result is still Conflicted.
//   - Bus error fallback: emit error triggers structured-log path; still Conflicted.
//   - ConflictingLabels carries all offending labels (not a subset).
//   - Deterministic fallback: calling twice with the same inputs yields the same result.
//   - Spec-content sensor: BI-009a anchor present in beads-integration.md.
//
// Bead: hk-7om2q.12.

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/gregberns/harmonik/internal/brcli"
	"github.com/gregberns/harmonik/internal/core"
)

// ─── Stub emitter ─────────────────────────────────────────────────────────────

// wlcStubEmitter is a minimal LabelConflictEmitter stub that records every
// emission call for inspection.
type wlcStubEmitter struct {
	mu     sync.Mutex
	calls  []wlcEmitCall
	retErr error // if non-nil, Emit returns this error
}

type wlcEmitCall struct {
	eventType core.EventType
	payload   []byte
}

func (s *wlcStubEmitter) Emit(_ context.Context, eventType core.EventType, payload []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, wlcEmitCall{eventType: eventType, payload: payload})
	return s.retErr
}

// wlcFixtureEmitter returns a fresh stub emitter with no configured error.
func wlcFixtureEmitter() *wlcStubEmitter {
	return &wlcStubEmitter{}
}

// wlcFixtureEmitterWithError returns a stub that returns retErr on every Emit.
func wlcFixtureEmitterWithError(retErr error) *wlcStubEmitter {
	return &wlcStubEmitter{retErr: retErr}
}

// ─── Spec-content sensor ──────────────────────────────────────────────────────

// wlcFixtureSpecContent reads specs/beads-integration.md and returns the
// paragraph containing the BI-009a anchor. The test fails if the spec is
// unreadable or the anchor is absent.
func wlcFixtureSpecContent(t *testing.T) string {
	t.Helper()

	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("wlcFixtureSpecContent: runtime.Caller failed — cannot locate repo root")
	}
	// internal/brcli/<file> → internal → repo root
	repoRoot := filepath.Join(filepath.Dir(thisFile), "..", "..")
	specPath := filepath.Join(repoRoot, "specs", "beads-integration.md")

	//nolint:gosec // G304: path is constructed from runtime.Caller + known relative segments, not user input
	raw, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatalf("wlcFixtureSpecContent: cannot read %s: %v", specPath, err)
	}
	content := string(raw)

	const anchor = "BI-009a"
	idx := strings.Index(content, anchor)
	if idx < 0 {
		t.Fatalf(
			"wlcFixtureSpecContent: spec %s does not contain %q; BI-009a may have been removed or renamed",
			specPath, anchor,
		)
	}

	// Return the paragraph from the anchor to the next section boundary.
	para := content[idx:]
	if end := strings.Index(para, "\n####"); end > 0 {
		para = para[:end]
	}
	return para
}

// ─── Conflict detection: two labels ──────────────────────────────────────────

// TestWLC_TwoWorkflowLabels_Conflicted verifies that two workflow:<mode> labels
// produce Conflicted=true and exactly one bead_label_conflict event emission.
//
// This is the primary acceptance criterion from the bead body:
// "two labels emit one conflict event".
func TestWLC_TwoWorkflowLabels_Conflicted(t *testing.T) {
	t.Parallel()

	bus := wlcFixtureEmitter()
	labels := []string{"workflow:single", "workflow:review-loop", "area:foo"}

	result := brcli.DetectWorkflowLabelConflict(context.Background(), "hk-test.1", labels, bus)

	if !result.Conflicted {
		t.Fatal("BI-009a: expected Conflicted=true for two workflow: labels; got false")
	}
	if len(result.ConflictingLabels) != 2 {
		t.Errorf("BI-009a: ConflictingLabels length = %d; want 2; got %v",
			len(result.ConflictingLabels), result.ConflictingLabels)
	}

	bus.mu.Lock()
	callCount := len(bus.calls)
	bus.mu.Unlock()

	if callCount != 1 {
		t.Errorf("BI-009a: bus.Emit call count = %d; want 1 (one bead_label_conflict per detection)", callCount)
	}
	if callCount > 0 {
		bus.mu.Lock()
		et := bus.calls[0].eventType
		bus.mu.Unlock()
		if et != core.EventType("bead_label_conflict") {
			t.Errorf("BI-009a: emitted event type = %q; want %q", et, "bead_label_conflict")
		}
	}
}

// TestWLC_ThreeWorkflowLabels_Conflicted verifies that three workflow: labels
// also produce exactly one conflict event (not one per pair).
func TestWLC_ThreeWorkflowLabels_Conflicted(t *testing.T) {
	t.Parallel()

	bus := wlcFixtureEmitter()
	labels := []string{"workflow:single", "workflow:review-loop", "workflow:dot"}

	result := brcli.DetectWorkflowLabelConflict(context.Background(), "hk-test.2", labels, bus)

	if !result.Conflicted {
		t.Fatal("BI-009a: expected Conflicted=true for three workflow: labels")
	}
	if len(result.ConflictingLabels) != 3 {
		t.Errorf("BI-009a: ConflictingLabels length = %d; want 3", len(result.ConflictingLabels))
	}

	bus.mu.Lock()
	callCount := len(bus.calls)
	bus.mu.Unlock()

	if callCount != 1 {
		t.Errorf("BI-009a: bus.Emit call count = %d; want exactly 1", callCount)
	}
}

// ─── Conflict detection: unknown mode ────────────────────────────────────────

// TestWLC_SingleUnknownMode_Conflicted verifies that a single workflow:<mode>
// label with an unrecognised mode is treated as a conflict (BI-009a condition b).
func TestWLC_SingleUnknownMode_Conflicted(t *testing.T) {
	t.Parallel()

	bus := wlcFixtureEmitter()
	labels := []string{"workflow:badmode"}

	result := brcli.DetectWorkflowLabelConflict(context.Background(), "hk-test.3", labels, bus)

	if !result.Conflicted {
		t.Fatal("BI-009a: expected Conflicted=true for unknown mode label; got false")
	}
	if len(result.ConflictingLabels) != 1 {
		t.Errorf("BI-009a: ConflictingLabels = %v; want [workflow:badmode]", result.ConflictingLabels)
	}

	bus.mu.Lock()
	callCount := len(bus.calls)
	bus.mu.Unlock()

	if callCount != 1 {
		t.Errorf("BI-009a: bus.Emit call count = %d; want 1", callCount)
	}
}

// ─── No-conflict: valid single labels ────────────────────────────────────────

// TestWLC_SingleValidSingle_NoConflict verifies workflow:single does not conflict.
func TestWLC_SingleValidSingle_NoConflict(t *testing.T) {
	t.Parallel()

	bus := wlcFixtureEmitter()
	result := brcli.DetectWorkflowLabelConflict(
		context.Background(), "hk-test.4", []string{"workflow:single"}, bus,
	)

	if result.Conflicted {
		t.Errorf("BI-009a: expected Conflicted=false for workflow:single; got true")
	}

	bus.mu.Lock()
	callCount := len(bus.calls)
	bus.mu.Unlock()

	if callCount != 0 {
		t.Errorf("BI-009a: bus.Emit called %d time(s) for valid single label; want 0", callCount)
	}
}

// TestWLC_SingleValidReviewLoop_NoConflict verifies workflow:review-loop does not conflict.
func TestWLC_SingleValidReviewLoop_NoConflict(t *testing.T) {
	t.Parallel()

	bus := wlcFixtureEmitter()
	result := brcli.DetectWorkflowLabelConflict(
		context.Background(), "hk-test.5", []string{"workflow:review-loop"}, bus,
	)

	if result.Conflicted {
		t.Errorf("BI-009a: expected Conflicted=false for workflow:review-loop; got true")
	}

	bus.mu.Lock()
	callCount := len(bus.calls)
	bus.mu.Unlock()

	if callCount != 0 {
		t.Errorf("BI-009a: bus.Emit called %d time(s) for valid review-loop label; want 0", callCount)
	}
}

// TestWLC_SingleValidDot_NoConflict verifies workflow:dot does not conflict.
func TestWLC_SingleValidDot_NoConflict(t *testing.T) {
	t.Parallel()

	bus := wlcFixtureEmitter()
	result := brcli.DetectWorkflowLabelConflict(
		context.Background(), "hk-test.6", []string{"workflow:dot"}, bus,
	)

	if result.Conflicted {
		t.Errorf("BI-009a: expected Conflicted=false for workflow:dot; got true")
	}
}

// ─── No-conflict: no workflow: labels ────────────────────────────────────────

// TestWLC_NoWorkflowLabels_NoConflict verifies that beads without any
// workflow: label produce no conflict.
func TestWLC_NoWorkflowLabels_NoConflict(t *testing.T) {
	t.Parallel()

	bus := wlcFixtureEmitter()
	labels := []string{"area:core", "size:S", "priority:2"}

	result := brcli.DetectWorkflowLabelConflict(context.Background(), "hk-test.7", labels, bus)

	if result.Conflicted {
		t.Errorf("BI-009a: expected Conflicted=false for labels without workflow:; got true")
	}

	bus.mu.Lock()
	callCount := len(bus.calls)
	bus.mu.Unlock()

	if callCount != 0 {
		t.Errorf("BI-009a: bus.Emit called %d time(s) for no-workflow-label bead; want 0", callCount)
	}
}

// TestWLC_EmptyLabels_NoConflict verifies empty label slice produces no conflict.
func TestWLC_EmptyLabels_NoConflict(t *testing.T) {
	t.Parallel()

	bus := wlcFixtureEmitter()
	result := brcli.DetectWorkflowLabelConflict(context.Background(), "hk-test.8", []string{}, bus)

	if result.Conflicted {
		t.Errorf("BI-009a: expected Conflicted=false for empty labels; got true")
	}
}

// ─── Structured-log fallback: nil bus ─────────────────────────────────────────

// TestWLC_NilBus_ConflictedAndNosPanic verifies that a nil bus does not
// panic, the result is still Conflicted=true, and the function returns
// normally (relying on structured-log fallback).
//
// Acceptance criterion from bead body: "structured-log fallback when bus unavailable".
func TestWLC_NilBus_ConflictedAndNoPanic(t *testing.T) {
	t.Parallel()

	labels := []string{"workflow:single", "workflow:review-loop"}

	// Must not panic.
	result := brcli.DetectWorkflowLabelConflict(context.Background(), "hk-test.9", labels, nil)

	if !result.Conflicted {
		t.Fatal("BI-009a: nil bus: expected Conflicted=true; got false")
	}
	if len(result.ConflictingLabels) == 0 {
		t.Error("BI-009a: nil bus: ConflictingLabels must be non-empty when Conflicted=true")
	}
}

// TestWLC_NilBus_UnknownMode_NoPanic verifies nil bus + unknown mode does not panic.
func TestWLC_NilBus_UnknownMode_NoPanic(t *testing.T) {
	t.Parallel()

	result := brcli.DetectWorkflowLabelConflict(
		context.Background(), "hk-test.10", []string{"workflow:unknown"}, nil,
	)

	if !result.Conflicted {
		t.Fatal("BI-009a: nil bus + unknown mode: expected Conflicted=true")
	}
}

// ─── Structured-log fallback: bus emit error ──────────────────────────────────

// TestWLC_BusEmitError_FallbackAndConflicted verifies that a bus emission
// error triggers the structured-log fallback path but still returns
// Conflicted=true (the error is non-fatal to the conflict result).
func TestWLC_BusEmitError_FallbackAndConflicted(t *testing.T) {
	t.Parallel()

	emitErr := errors.New("bus: simulated emit failure")
	bus := wlcFixtureEmitterWithError(emitErr)
	labels := []string{"workflow:single", "workflow:dot"}

	result := brcli.DetectWorkflowLabelConflict(context.Background(), "hk-test.11", labels, bus)

	if !result.Conflicted {
		t.Fatal("BI-009a: bus emit error: expected Conflicted=true")
	}

	// The stub Emit was called even though it returned an error.
	bus.mu.Lock()
	callCount := len(bus.calls)
	bus.mu.Unlock()

	if callCount != 1 {
		t.Errorf("BI-009a: bus emit error: expected 1 Emit call; got %d", callCount)
	}
}

// ─── Deterministic fallback ───────────────────────────────────────────────────

// TestWLC_Deterministic_SameResultOnTwoCalls verifies that calling
// DetectWorkflowLabelConflict twice with the same inputs yields identical
// Conflicted and ConflictingLabels values.
//
// Acceptance criterion from bead body: "falls back deterministically".
func TestWLC_Deterministic_SameResultOnTwoCalls(t *testing.T) {
	t.Parallel()

	labels := []string{"workflow:single", "workflow:review-loop"}
	ctx := context.Background()

	bus1 := wlcFixtureEmitter()
	r1 := brcli.DetectWorkflowLabelConflict(ctx, "hk-det.1", labels, bus1)

	bus2 := wlcFixtureEmitter()
	r2 := brcli.DetectWorkflowLabelConflict(ctx, "hk-det.1", labels, bus2)

	if r1.Conflicted != r2.Conflicted {
		t.Errorf("BI-009a: determinism: Conflicted differs across calls: %v vs %v", r1.Conflicted, r2.Conflicted)
	}
	if len(r1.ConflictingLabels) != len(r2.ConflictingLabels) {
		t.Errorf("BI-009a: determinism: ConflictingLabels length differs: %d vs %d",
			len(r1.ConflictingLabels), len(r2.ConflictingLabels))
	}
}

// ─── ConflictingLabels completeness ──────────────────────────────────────────

// TestWLC_ConflictingLabels_CarriesAllOffendingLabels verifies that
// ConflictingLabels contains all workflow: labels (not just the first two).
func TestWLC_ConflictingLabels_CarriesAllOffendingLabels(t *testing.T) {
	t.Parallel()

	bus := wlcFixtureEmitter()
	wfLabels := []string{"workflow:single", "workflow:review-loop", "workflow:dot"}
	labels := append([]string{"area:core"}, wfLabels...)

	result := brcli.DetectWorkflowLabelConflict(context.Background(), "hk-test.12", labels, bus)

	if !result.Conflicted {
		t.Fatal("BI-009a: expected Conflicted=true for three workflow: labels")
	}
	if len(result.ConflictingLabels) != 3 {
		t.Errorf("BI-009a: ConflictingLabels has %d entry/entries; want 3 (all offending labels); got %v",
			len(result.ConflictingLabels), result.ConflictingLabels)
	}
	// Verify all three are present.
	got := make(map[string]bool, len(result.ConflictingLabels))
	for _, l := range result.ConflictingLabels {
		got[l] = true
	}
	for _, want := range wfLabels {
		if !got[want] {
			t.Errorf("BI-009a: ConflictingLabels missing %q; got %v", want, result.ConflictingLabels)
		}
	}
}

// ─── No-conflict: ConflictingLabels nil ──────────────────────────────────────

// TestWLC_NoConflict_ConflictingLabelsNil verifies that ConflictingLabels is
// nil (not an empty slice) when there is no conflict, so callers can use
// len(result.ConflictingLabels)==0 or result.ConflictingLabels==nil interchangeably.
func TestWLC_NoConflict_ConflictingLabelsNil(t *testing.T) {
	t.Parallel()

	result := brcli.DetectWorkflowLabelConflict(
		context.Background(), "hk-test.13", []string{"workflow:single"}, nil,
	)

	if result.ConflictingLabels != nil {
		t.Errorf("BI-009a: no conflict: ConflictingLabels = %v; want nil", result.ConflictingLabels)
	}
}

// ─── Spec-content sensor ──────────────────────────────────────────────────────

// TestWLC_SpecContainsBI009a verifies that BI-009a is present in
// specs/beads-integration.md and encodes the required normative phrases.
// Protects against spec drift that would silently un-anchor the enforcement.
func TestWLC_SpecContainsBI009a(t *testing.T) {
	t.Parallel()

	para := wlcFixtureSpecContent(t)

	cases := []struct {
		phrase string
		hint   string
	}{
		{
			phrase: "workflow:<mode>",
			hint:   "BI-009a must reference the workflow:<mode> label form",
		},
		{
			phrase: "bead_label_conflict",
			hint:   "BI-009a must name the bead_label_conflict event",
		},
		{
			phrase: "MUST",
			hint:   "BI-009a must contain normative MUST language",
		},
	}

	for _, tc := range cases {
		if !strings.Contains(para, tc.phrase) {
			t.Errorf(
				"BI-009a spec paragraph does not contain %q — %s\nParagraph:\n%s",
				tc.phrase, tc.hint, para,
			)
		}
	}
}
