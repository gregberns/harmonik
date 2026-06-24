package scenario

// sh_inv_004_rerun_diff_test.go — contract tests for the SH-INV-004 nightly
// rerun-diff sensor (DiffRerunSnapshots, SnapshotFromResult, ReadEventTypeMultiset).
//
// Per specs/scenario-harness.md §5 SH-INV-004 and §4.8 SH-027: the sensor
// MUST detect any divergence in the determinism contract field set across N≥10
// reruns of the regression-cadence scenario subset:
//
//   - verdict
//   - failure_class
//   - ordered list of (AssertionResult.passed, AssertionResult.assertion_kind) tuples
//   - multiset of distinct event types observed in the captured JSONL
//
// Scope carve-out (§4.8): nightly-cadence scenarios exercising HC-026a wall-clock
// heartbeat mode are exempt. These tests cover the non-exempt (deterministic) case.
//
// Test naming: shINV004RerunDiff* (helper prefix per implementer-protocol).
//
// Spec ref: specs/scenario-harness.md §5 SH-INV-004, §4.8 SH-027.
// Tags: mechanism
// Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
)

// ─────────────────────────────────────────────────────────────────────────────
// Test helpers
// ─────────────────────────────────────────────────────────────────────────────

// shINV004BaseSnapshot returns a canonical RerunDiffSnapshot used as the
// reference run in multi-run tests.
func shINV004BaseSnapshot() RerunDiffSnapshot {
	return RerunDiffSnapshot{
		ScenarioName: "regression/twin-failure-classification",
		Verdict:      ScenarioVerdictPass,
		FailureClass: "",
		AssertionTuples: []AssertionTuple{
			{Passed: true, AssertionKind: AssertionResultKindEventPresent},
			{Passed: true, AssertionKind: AssertionResultKindEventAbsent},
			{Passed: true, AssertionKind: AssertionResultKindExitCode},
		},
		EventTypeMultiset: map[core.EventType]int{
			"agent_ready":     1,
			"agent_completed": 1,
			"outcome_emitted": 1,
		},
	}
}

// shINV004MakeNSnapshots returns a slice of n deep copies of base.
func shINV004MakeNSnapshots(base RerunDiffSnapshot, n int) []RerunDiffSnapshot {
	snapshots := make([]RerunDiffSnapshot, n)
	for i := range snapshots {
		tuples := make([]AssertionTuple, len(base.AssertionTuples))
		copy(tuples, base.AssertionTuples)
		multiset := make(map[core.EventType]int, len(base.EventTypeMultiset))
		for k, v := range base.EventTypeMultiset {
			multiset[k] = v
		}
		snapshots[i] = RerunDiffSnapshot{
			ScenarioName:      base.ScenarioName,
			Verdict:           base.Verdict,
			FailureClass:      base.FailureClass,
			AssertionTuples:   tuples,
			EventTypeMultiset: multiset,
		}
	}
	return snapshots
}

// shINV004WriteJSONL writes JSONL lines to a temp file and returns its path.
func shINV004WriteJSONL(t *testing.T, lines []string) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "harmonik-shinv004-")
	if err != nil {
		t.Fatalf("shINV004WriteJSONL: MkdirTemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	p := filepath.Join(dir, "events.jsonl")
	f, err := os.Create(p) //nolint:gosec // G304: test-only temp path
	if err != nil {
		t.Fatalf("shINV004WriteJSONL: Create: %v", err)
	}
	defer f.Close()
	for _, line := range lines {
		if _, err := fmt.Fprintln(f, line); err != nil {
			t.Fatalf("shINV004WriteJSONL: Fprintln: %v", err)
		}
	}
	return p
}

// shINV004MakeResult builds a minimal valid ScenarioResult with the given verdict.
func shINV004MakeResult(t *testing.T, name string, verdict ScenarioVerdict, fc FailureClass, assertions []AssertionResult) ScenarioResult {
	t.Helper()
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	return ScenarioResult{
		ScenarioName:          name,
		SourcePath:            "scenarios/regression/" + name + ".yaml",
		StartedAt:             start,
		CompletedAt:           start.Add(30 * time.Second),
		Verdict:               verdict,
		FailureClass:          fc,
		AssertionResults:      assertions,
		EventLogPath:          name + "/events.jsonl",
		WorkspaceSnapshotPath: name + "/workspace",
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// AssertionTuple zero-value and field access
// ─────────────────────────────────────────────────────────────────────────────

// TestSHINV004_AssertionTuple_ZeroValue verifies the zero value is well-formed.
func TestSHINV004_AssertionTuple_ZeroValue(t *testing.T) {
	t.Parallel()

	var at AssertionTuple
	if at.Passed {
		t.Error("AssertionTuple zero value: Passed = true; want false")
	}
	if at.AssertionKind != "" {
		t.Errorf("AssertionTuple zero value: AssertionKind = %q; want empty string", at.AssertionKind)
	}
}

// TestSHINV004_AssertionTuple_FieldsStored verifies both fields are stored correctly.
func TestSHINV004_AssertionTuple_FieldsStored(t *testing.T) {
	t.Parallel()

	at := AssertionTuple{Passed: true, AssertionKind: AssertionResultKindWorkspaceState}
	if !at.Passed {
		t.Error("Passed = false; want true")
	}
	if at.AssertionKind != AssertionResultKindWorkspaceState {
		t.Errorf("AssertionKind = %q; want %q", at.AssertionKind, AssertionResultKindWorkspaceState)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// DiffRerunSnapshots — vacuous cases
// ─────────────────────────────────────────────────────────────────────────────

// TestSHINV004_DiffRerunSnapshots_UniformOnNil verifies that nil returns uniform.
func TestSHINV004_DiffRerunSnapshots_UniformOnNil(t *testing.T) {
	t.Parallel()

	report := DiffRerunSnapshots(nil)
	if report == nil {
		t.Fatal("DiffRerunSnapshots(nil): nil report; want non-nil")
	}
	if !report.Uniform {
		t.Error("Uniform = false; want true (vacuous empty input)")
	}
	if len(report.Divergences) != 0 {
		t.Errorf("Divergences = %v; want empty", report.Divergences)
	}
}

// TestSHINV004_DiffRerunSnapshots_UniformOnSingle verifies a single snapshot
// is vacuously uniform.
func TestSHINV004_DiffRerunSnapshots_UniformOnSingle(t *testing.T) {
	t.Parallel()

	report := DiffRerunSnapshots([]RerunDiffSnapshot{shINV004BaseSnapshot()})
	if report == nil {
		t.Fatal("DiffRerunSnapshots: nil report")
	}
	if !report.Uniform {
		t.Error("Uniform = false; want true for single snapshot")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// DiffRerunSnapshots — N≥10 uniform (SH-INV-004 primary conformance test)
// ─────────────────────────────────────────────────────────────────────────────

// TestSHINV004_DiffRerunSnapshots_UniformOnTenIdentical is the primary SH-INV-004
// conformance test: N=10 identical regression-cadence snapshots MUST produce a
// Uniform=true report with no divergences.
//
// This satisfies the spec's N≥10 rerun requirement for the nightly cadence sensor.
//
// Spec ref: specs/scenario-harness.md §5 SH-INV-004.
func TestSHINV004_DiffRerunSnapshots_UniformOnTenIdentical(t *testing.T) {
	t.Parallel()

	const n = 10
	snapshots := shINV004MakeNSnapshots(shINV004BaseSnapshot(), n)
	if len(snapshots) < 10 {
		t.Fatalf("test setup: got %d snapshots; need ≥10 per SH-INV-004", len(snapshots))
	}

	report := DiffRerunSnapshots(snapshots)
	if report == nil {
		t.Fatal("DiffRerunSnapshots: nil report")
	}
	if !report.Uniform {
		t.Errorf("DiffRerunSnapshots(%d identical).Uniform = false; want true", n)
	}
	if len(report.Divergences) != 0 {
		t.Errorf("Divergences = %v; want none for %d identical runs", report.Divergences, n)
	}
}

// TestSHINV004_DiffRerunSnapshots_UniformOnFifteenIdentical verifies the sensor
// handles N=15 without false divergences.
func TestSHINV004_DiffRerunSnapshots_UniformOnFifteenIdentical(t *testing.T) {
	t.Parallel()

	const n = 15
	report := DiffRerunSnapshots(shINV004MakeNSnapshots(shINV004BaseSnapshot(), n))
	if report == nil {
		t.Fatal("DiffRerunSnapshots: nil report")
	}
	if !report.Uniform {
		t.Errorf("DiffRerunSnapshots(%d identical).Uniform = false; want true", n)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// DiffRerunSnapshots — verdict divergence
// ─────────────────────────────────────────────────────────────────────────────

// TestSHINV004_DiffRerunSnapshots_DetectsVerdictDivergence verifies that a
// verdict change on run 5 of 10 is reported.
func TestSHINV004_DiffRerunSnapshots_DetectsVerdictDivergence(t *testing.T) {
	t.Parallel()

	const n = 10
	snapshots := shINV004MakeNSnapshots(shINV004BaseSnapshot(), n)
	snapshots[5].Verdict = ScenarioVerdictFail
	snapshots[5].FailureClass = FailureClassAssertionFailed

	report := DiffRerunSnapshots(snapshots)
	if report.Uniform {
		t.Error("Uniform = true; want false (verdict diverged on run 5)")
	}

	found := false
	for _, d := range report.Divergences {
		if d.FieldName == "verdict" && d.RunIndex == 5 {
			found = true
			if d.BaseValue != string(ScenarioVerdictPass) {
				t.Errorf("BaseValue = %q; want %q", d.BaseValue, ScenarioVerdictPass)
			}
			if d.DivergedValue != string(ScenarioVerdictFail) {
				t.Errorf("DivergedValue = %q; want %q", d.DivergedValue, ScenarioVerdictFail)
			}
		}
	}
	if !found {
		t.Errorf("no verdict divergence at RunIndex=5 in: %+v", report.Divergences)
	}
}

// TestSHINV004_DiffRerunSnapshots_DetectsVerdictDivergenceAllRuns verifies that
// all N-1 divergences are reported when every run after 0 changes verdict.
func TestSHINV004_DiffRerunSnapshots_DetectsVerdictDivergenceAllRuns(t *testing.T) {
	t.Parallel()

	const n = 10
	snapshots := shINV004MakeNSnapshots(shINV004BaseSnapshot(), n)
	for i := 1; i < n; i++ {
		snapshots[i].Verdict = ScenarioVerdictError
		snapshots[i].FailureClass = FailureClassHarnessInternalError
	}

	report := DiffRerunSnapshots(snapshots)
	if report.Uniform {
		t.Error("Uniform = true; want false (all runs after 0 diverged)")
	}
	count := 0
	for _, d := range report.Divergences {
		if d.FieldName == "verdict" {
			count++
		}
	}
	if count != n-1 {
		t.Errorf("verdict divergence count = %d; want %d", count, n-1)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// DiffRerunSnapshots — failure_class divergence
// ─────────────────────────────────────────────────────────────────────────────

// TestSHINV004_DiffRerunSnapshots_DetectsFailureClassDivergence verifies a
// failure_class change on run 3 of 10 is detected independently of verdict.
func TestSHINV004_DiffRerunSnapshots_DetectsFailureClassDivergence(t *testing.T) {
	t.Parallel()

	const n = 10
	base := shINV004BaseSnapshot()
	base.Verdict = ScenarioVerdictFail
	base.FailureClass = FailureClassAssertionFailed
	base.AssertionTuples = []AssertionTuple{
		{Passed: false, AssertionKind: AssertionResultKindEventPresent},
	}

	snapshots := shINV004MakeNSnapshots(base, n)
	snapshots[3].FailureClass = FailureClassHarnessInternalError

	report := DiffRerunSnapshots(snapshots)
	if report.Uniform {
		t.Error("Uniform = true; want false (failure_class diverged on run 3)")
	}
	found := false
	for _, d := range report.Divergences {
		if d.FieldName == "failure_class" && d.RunIndex == 3 {
			found = true
			if d.BaseValue != string(FailureClassAssertionFailed) {
				t.Errorf("BaseValue = %q; want %q", d.BaseValue, FailureClassAssertionFailed)
			}
			if d.DivergedValue != string(FailureClassHarnessInternalError) {
				t.Errorf("DivergedValue = %q; want %q", d.DivergedValue, FailureClassHarnessInternalError)
			}
		}
	}
	if !found {
		t.Errorf("no failure_class divergence at RunIndex=3: %+v", report.Divergences)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// DiffRerunSnapshots — assertion_tuples divergence
// ─────────────────────────────────────────────────────────────────────────────

// TestSHINV004_DiffRerunSnapshots_DetectsAssertionTuplesDivergence verifies that
// a changed assertion outcome on run 7 of 10 is reported.
func TestSHINV004_DiffRerunSnapshots_DetectsAssertionTuplesDivergence(t *testing.T) {
	t.Parallel()

	const n = 10
	snapshots := shINV004MakeNSnapshots(shINV004BaseSnapshot(), n)
	snapshots[7].AssertionTuples = []AssertionTuple{
		{Passed: false, AssertionKind: AssertionResultKindEventPresent}, // was true
		{Passed: true, AssertionKind: AssertionResultKindEventAbsent},
		{Passed: true, AssertionKind: AssertionResultKindExitCode},
	}

	report := DiffRerunSnapshots(snapshots)
	if report.Uniform {
		t.Error("Uniform = true; want false (assertion_tuples diverged on run 7)")
	}
	found := false
	for _, d := range report.Divergences {
		if d.FieldName == "assertion_tuples" && d.RunIndex == 7 {
			found = true
			if !containsSubstring(d.BaseValue, "true:event_present") {
				t.Errorf("BaseValue %q missing expected tuple", d.BaseValue)
			}
			if !containsSubstring(d.DivergedValue, "false:event_present") {
				t.Errorf("DivergedValue %q missing expected tuple", d.DivergedValue)
			}
		}
	}
	if !found {
		t.Errorf("no assertion_tuples divergence at RunIndex=7: %+v", report.Divergences)
	}
}

// TestSHINV004_DiffRerunSnapshots_DetectsAssertionCountChange verifies that a
// different count of assertion tuples (not just values) is detected.
func TestSHINV004_DiffRerunSnapshots_DetectsAssertionCountChange(t *testing.T) {
	t.Parallel()

	const n = 10
	snapshots := shINV004MakeNSnapshots(shINV004BaseSnapshot(), n)
	snapshots[2].AssertionTuples = []AssertionTuple{
		{Passed: true, AssertionKind: AssertionResultKindEventPresent},
		// dropped the second and third assertion
	}

	report := DiffRerunSnapshots(snapshots)
	if report.Uniform {
		t.Error("Uniform = true; want false (assertion count changed on run 2)")
	}
	found := false
	for _, d := range report.Divergences {
		if d.FieldName == "assertion_tuples" && d.RunIndex == 2 {
			found = true
		}
	}
	if !found {
		t.Errorf("no assertion_tuples divergence at RunIndex=2: %+v", report.Divergences)
	}
}

// TestSHINV004_DiffRerunSnapshots_EmptyAssertionTuplesUniform verifies N
// identical snapshots with no tuples remain uniform.
func TestSHINV004_DiffRerunSnapshots_EmptyAssertionTuplesUniform(t *testing.T) {
	t.Parallel()

	const n = 10
	base := shINV004BaseSnapshot()
	base.AssertionTuples = nil

	report := DiffRerunSnapshots(shINV004MakeNSnapshots(base, n))
	if !report.Uniform {
		t.Errorf("Uniform = false for %d empty-tuple snapshots; want true", n)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// DiffRerunSnapshots — event_type_multiset divergence
// ─────────────────────────────────────────────────────────────────────────────

// TestSHINV004_DiffRerunSnapshots_DetectsEventTypeCountChange verifies that a
// changed event-type count on run 1 of 10 is reported.
func TestSHINV004_DiffRerunSnapshots_DetectsEventTypeCountChange(t *testing.T) {
	t.Parallel()

	const n = 10
	snapshots := shINV004MakeNSnapshots(shINV004BaseSnapshot(), n)
	snapshots[1].EventTypeMultiset = map[core.EventType]int{
		"agent_ready":     2, // was 1
		"agent_completed": 1,
		"outcome_emitted": 1,
	}

	report := DiffRerunSnapshots(snapshots)
	if report.Uniform {
		t.Error("Uniform = true; want false (event_type_multiset diverged on run 1)")
	}
	found := false
	for _, d := range report.Divergences {
		if d.FieldName == "event_type_multiset" && d.RunIndex == 1 {
			found = true
			if !containsSubstring(d.BaseValue, "agent_ready=1") {
				t.Errorf("BaseValue %q missing agent_ready=1", d.BaseValue)
			}
			if !containsSubstring(d.DivergedValue, "agent_ready=2") {
				t.Errorf("DivergedValue %q missing agent_ready=2", d.DivergedValue)
			}
		}
	}
	if !found {
		t.Errorf("no event_type_multiset divergence at RunIndex=1: %+v", report.Divergences)
	}
}

// TestSHINV004_DiffRerunSnapshots_DetectsAbsentEventType verifies that a missing
// event type on run 9 of 10 is detected.
func TestSHINV004_DiffRerunSnapshots_DetectsAbsentEventType(t *testing.T) {
	t.Parallel()

	const n = 10
	snapshots := shINV004MakeNSnapshots(shINV004BaseSnapshot(), n)
	snapshots[9].EventTypeMultiset = map[core.EventType]int{
		"agent_ready":     1,
		"agent_completed": 1,
		// outcome_emitted absent
	}

	report := DiffRerunSnapshots(snapshots)
	if report.Uniform {
		t.Error("Uniform = true; want false (event type absent on run 9)")
	}
	found := false
	for _, d := range report.Divergences {
		if d.FieldName == "event_type_multiset" && d.RunIndex == 9 {
			found = true
		}
	}
	if !found {
		t.Errorf("no event_type_multiset divergence at RunIndex=9: %+v", report.Divergences)
	}
}

// TestSHINV004_DiffRerunSnapshots_NilAndEmptyMultisetIdentical verifies that nil
// and empty-map event-type multisets are treated as equivalent across 10 runs.
func TestSHINV004_DiffRerunSnapshots_NilAndEmptyMultisetIdentical(t *testing.T) {
	t.Parallel()

	base := shINV004BaseSnapshot()
	base.EventTypeMultiset = nil

	other := shINV004BaseSnapshot()
	other.EventTypeMultiset = map[core.EventType]int{}

	// Interleave nil and empty to cover 10 runs.
	snapshots := make([]RerunDiffSnapshot, 10)
	for i := range snapshots {
		if i%2 == 0 {
			snapshots[i] = base
		} else {
			snapshots[i] = other
		}
	}

	report := DiffRerunSnapshots(snapshots)
	if !report.Uniform {
		t.Errorf("nil vs empty multiset treated as divergent; want uniform: %+v", report.Divergences)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// DiffRerunSnapshots — multi-field divergence
// ─────────────────────────────────────────────────────────────────────────────

// TestSHINV004_DiffRerunSnapshots_ReportsAllDivergentFields verifies that when
// all four contract fields diverge on a single run, all four are reported.
func TestSHINV004_DiffRerunSnapshots_ReportsAllDivergentFields(t *testing.T) {
	t.Parallel()

	const n = 10
	snapshots := shINV004MakeNSnapshots(shINV004BaseSnapshot(), n)
	snapshots[4].Verdict = ScenarioVerdictFail
	snapshots[4].FailureClass = FailureClassAssertionFailed
	snapshots[4].AssertionTuples = []AssertionTuple{
		{Passed: false, AssertionKind: AssertionResultKindEventPresent},
	}
	snapshots[4].EventTypeMultiset = map[core.EventType]int{
		"agent_ready": 1,
	}

	report := DiffRerunSnapshots(snapshots)
	if report.Uniform {
		t.Error("Uniform = true; want false")
	}

	fields := make(map[string]bool)
	for _, d := range report.Divergences {
		if d.RunIndex == 4 {
			fields[d.FieldName] = true
		}
	}
	for _, want := range []string{"verdict", "failure_class", "assertion_tuples", "event_type_multiset"} {
		if !fields[want] {
			t.Errorf("missing divergence for field %q on run 4", want)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// SnapshotFromResult
// ─────────────────────────────────────────────────────────────────────────────

// TestSHINV004_SnapshotFromResult_ExtractsVerdictAndFailureClass verifies that
// verdict and failure_class are correctly projected from ScenarioResult.
func TestSHINV004_SnapshotFromResult_ExtractsVerdictAndFailureClass(t *testing.T) {
	t.Parallel()

	result := shINV004MakeResult(t, "my-scenario", ScenarioVerdictFail, FailureClassAssertionFailed, nil)
	snap := SnapshotFromResult(result)

	if snap.ScenarioName != "my-scenario" {
		t.Errorf("ScenarioName = %q; want %q", snap.ScenarioName, "my-scenario")
	}
	if snap.Verdict != ScenarioVerdictFail {
		t.Errorf("Verdict = %q; want %q", snap.Verdict, ScenarioVerdictFail)
	}
	if snap.FailureClass != FailureClassAssertionFailed {
		t.Errorf("FailureClass = %q; want %q", snap.FailureClass, FailureClassAssertionFailed)
	}
	if snap.EventTypeMultiset != nil {
		t.Error("EventTypeMultiset should be nil (not populated by SnapshotFromResult)")
	}
}

// TestSHINV004_SnapshotFromResult_ExtractsAssertionTuplesInOrder verifies that
// AssertionResults are mapped to AssertionTuples in source order.
func TestSHINV004_SnapshotFromResult_ExtractsAssertionTuplesInOrder(t *testing.T) {
	t.Parallel()

	assertions := []AssertionResult{
		{AssertionKind: AssertionResultKindEventPresent, Description: "a", Passed: true},
		{AssertionKind: AssertionResultKindExitCode, Description: "b", Passed: false},
		{AssertionKind: AssertionResultKindWorkspaceState, Description: "c", Passed: true},
	}
	result := shINV004MakeResult(t, "tuple-test", ScenarioVerdictPass, "", assertions)
	snap := SnapshotFromResult(result)

	if len(snap.AssertionTuples) != 3 {
		t.Fatalf("AssertionTuples len = %d; want 3", len(snap.AssertionTuples))
	}
	want := []AssertionTuple{
		{Passed: true, AssertionKind: AssertionResultKindEventPresent},
		{Passed: false, AssertionKind: AssertionResultKindExitCode},
		{Passed: true, AssertionKind: AssertionResultKindWorkspaceState},
	}
	for i, got := range snap.AssertionTuples {
		if got != want[i] {
			t.Errorf("AssertionTuples[%d] = %+v; want %+v", i, got, want[i])
		}
	}
}

// TestSHINV004_SnapshotFromResult_NilAssertionsProducesEmptySlice verifies that
// nil AssertionResults produces a non-nil empty slice (not nil).
func TestSHINV004_SnapshotFromResult_NilAssertionsProducesEmptySlice(t *testing.T) {
	t.Parallel()

	result := shINV004MakeResult(t, "empty", ScenarioVerdictPass, "", nil)
	snap := SnapshotFromResult(result)
	if snap.AssertionTuples == nil {
		t.Error("AssertionTuples is nil for nil source; want non-nil empty slice")
	}
	if len(snap.AssertionTuples) != 0 {
		t.Errorf("AssertionTuples len = %d; want 0", len(snap.AssertionTuples))
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// ReadEventTypeMultiset
// ─────────────────────────────────────────────────────────────────────────────

// TestSHINV004_ReadEventTypeMultiset_CountsTypes verifies event types are counted
// correctly from a valid JSONL file.
func TestSHINV004_ReadEventTypeMultiset_CountsTypes(t *testing.T) {
	t.Parallel()

	lines := []string{
		`{"type":"agent_ready","event_id":"00000000-0000-0000-0000-000000000001"}`,
		`{"type":"agent_completed","event_id":"00000000-0000-0000-0000-000000000002"}`,
		`{"type":"agent_ready","event_id":"00000000-0000-0000-0000-000000000003"}`,
		`{"type":"outcome_emitted","event_id":"00000000-0000-0000-0000-000000000004"}`,
	}
	p := shINV004WriteJSONL(t, lines)

	multiset, err := ReadEventTypeMultiset(p)
	if err != nil {
		t.Fatalf("ReadEventTypeMultiset: %v", err)
	}
	want := map[core.EventType]int{
		"agent_ready":     2,
		"agent_completed": 1,
		"outcome_emitted": 1,
	}
	for k, wantCount := range want {
		if multiset[k] != wantCount {
			t.Errorf("multiset[%q] = %d; want %d", k, multiset[k], wantCount)
		}
	}
	if len(multiset) != len(want) {
		t.Errorf("multiset key count = %d; want %d", len(multiset), len(want))
	}
}

// TestSHINV004_ReadEventTypeMultiset_SkipsEmptyAndWhitespaceLines verifies
// blank lines are silently skipped (torn-tail tolerance per SH-020).
func TestSHINV004_ReadEventTypeMultiset_SkipsEmptyAndWhitespaceLines(t *testing.T) {
	t.Parallel()

	lines := []string{
		``,
		`{"type":"agent_ready","event_id":"00000000-0000-0000-0000-000000000001"}`,
		`   `,
		`{"type":"outcome_emitted","event_id":"00000000-0000-0000-0000-000000000002"}`,
		``,
	}
	p := shINV004WriteJSONL(t, lines)

	multiset, err := ReadEventTypeMultiset(p)
	if err != nil {
		t.Fatalf("ReadEventTypeMultiset: %v", err)
	}
	if multiset["agent_ready"] != 1 {
		t.Errorf("agent_ready = %d; want 1", multiset["agent_ready"])
	}
	if multiset["outcome_emitted"] != 1 {
		t.Errorf("outcome_emitted = %d; want 1", multiset["outcome_emitted"])
	}
}

// TestSHINV004_ReadEventTypeMultiset_SkipsMalformedLines verifies that invalid
// JSON lines are silently skipped (torn-tail tolerance per SH-020).
func TestSHINV004_ReadEventTypeMultiset_SkipsMalformedLines(t *testing.T) {
	t.Parallel()

	lines := []string{
		`{"type":"agent_ready","event_id":"00000000-0000-0000-0000-000000000001"}`,
		`not-json`,
		`{"type":"agent_completed","event_id":"00000000-0000-0000-0000-000000000002"}`,
		`{broken`,
	}
	p := shINV004WriteJSONL(t, lines)

	multiset, err := ReadEventTypeMultiset(p)
	if err != nil {
		t.Fatalf("ReadEventTypeMultiset: %v", err)
	}
	if len(multiset) != 2 {
		t.Errorf("multiset keys = %d; want 2 (malformed lines skipped)", len(multiset))
	}
}

// TestSHINV004_ReadEventTypeMultiset_SkipsEmptyTypeField verifies that records
// with an empty "type" field are silently skipped.
func TestSHINV004_ReadEventTypeMultiset_SkipsEmptyTypeField(t *testing.T) {
	t.Parallel()

	lines := []string{
		`{"type":"","event_id":"00000000-0000-0000-0000-000000000001"}`,
		`{"type":"agent_ready","event_id":"00000000-0000-0000-0000-000000000002"}`,
	}
	p := shINV004WriteJSONL(t, lines)

	multiset, err := ReadEventTypeMultiset(p)
	if err != nil {
		t.Fatalf("ReadEventTypeMultiset: %v", err)
	}
	if multiset[""] != 0 {
		t.Errorf("empty-type counted %d times; want 0", multiset[""])
	}
	if multiset["agent_ready"] != 1 {
		t.Errorf("agent_ready = %d; want 1", multiset["agent_ready"])
	}
}

// TestSHINV004_ReadEventTypeMultiset_EmptyFileNonNilMap verifies that a file
// with no decodable events returns a non-nil empty map (not nil).
func TestSHINV004_ReadEventTypeMultiset_EmptyFileNonNilMap(t *testing.T) {
	t.Parallel()

	p := shINV004WriteJSONL(t, nil)

	multiset, err := ReadEventTypeMultiset(p)
	if err != nil {
		t.Fatalf("ReadEventTypeMultiset: %v", err)
	}
	if multiset == nil {
		t.Error("ReadEventTypeMultiset: nil map for empty file; want non-nil empty map")
	}
	if len(multiset) != 0 {
		t.Errorf("multiset len = %d; want 0", len(multiset))
	}
}

// TestSHINV004_ReadEventTypeMultiset_ErrorOnMissingFile verifies that a
// non-existent path returns an error.
func TestSHINV004_ReadEventTypeMultiset_ErrorOnMissingFile(t *testing.T) {
	t.Parallel()

	_, err := ReadEventTypeMultiset("/does/not/exist/events.jsonl")
	if err == nil {
		t.Error("ReadEventTypeMultiset: no error for non-existent file; want error")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Round-trip: SnapshotFromResult → DiffRerunSnapshots
// ─────────────────────────────────────────────────────────────────────────────

// TestSHINV004_RoundTrip_IdenticalResultsUniform verifies that N identical
// ScenarioResult values produce N identical snapshots that are uniform.
func TestSHINV004_RoundTrip_IdenticalResultsUniform(t *testing.T) {
	t.Parallel()

	const n = 10
	assertions := []AssertionResult{
		{AssertionKind: AssertionResultKindEventPresent, Description: "a", Passed: true},
		{AssertionKind: AssertionResultKindExitCode, Description: "b", Passed: true},
	}
	result := shINV004MakeResult(t, "round-trip", ScenarioVerdictPass, "", assertions)

	snapshots := make([]RerunDiffSnapshot, n)
	for i := range snapshots {
		snapshots[i] = SnapshotFromResult(result)
	}

	report := DiffRerunSnapshots(snapshots)
	if !report.Uniform {
		t.Errorf("identical SnapshotFromResult outputs are not uniform: %+v", report.Divergences)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Spec corpus: SH-INV-004 declared in spec
// ─────────────────────────────────────────────────────────────────────────────

// TestSHINV004_SpecCorpus_SensorDeclaredInSpec verifies that the
// scenario-harness spec declares SH-INV-004 and the required tokens.
// Guards against spec-drift silently removing the sensor obligation.
func TestSHINV004_SpecCorpus_SensorDeclaredInSpec(t *testing.T) {
	t.Parallel()

	root := conformanceCorpusFixtureRepoRoot(t)
	specPath := filepath.Join(root, "specs", "scenario-harness.md")
	data, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatalf("reading spec %q: %v", specPath, err)
	}
	spec := string(data)

	required := []string{
		"SH-INV-004",
		"verdict",
		"failure_class",
		"assertion_kind",
		"event type",
		"nightly",
	}
	for _, token := range required {
		if !containsSubstring(spec, token) {
			t.Errorf("spec %q missing required token %q; spec may have drifted", specPath, token)
		}
	}
}
