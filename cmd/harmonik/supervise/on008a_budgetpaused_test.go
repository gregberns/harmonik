package supervisecmd

// on008a_budgetpaused_test.go — conformance tests for ON-008a.
//
// ON-008a has two obligations:
//  1. `harmonik supervise start` MUST inject the credential into Pi from the
//     non-committed scoped source (CI-006). Tested separately in
//     ci006_ci001_explore_hk96s75_test.go.
//  2. The `budget-paused` pause-reason MUST be surfaced to the operator via
//     `harmonik supervise status` alongside `circuit-tripped`.
//
// This file covers obligation 2: verifying that:
//   - CognitionLoopStatus declares budget-paused and circuit-tripped as valid values.
//   - WriteLoopStatusAtomic / ReadLoopStatus round-trip correctly.
//   - buildStatus includes loop_status and pause_reason from loop-status.json.
//   - The human-readable status output includes loop_status when the file is present.
//   - The JSON status output includes loop_status and pause_reason fields.
//
// Spec ref: specs/operator-nfr.md §4.3 ON-008a;
//           specs/cognition-loop.md §6 LoopStatus.
// Bead: hk-cy8rp.

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// TestON008a_CognitionLoopStatusDeclaresRequiredValues
// ---------------------------------------------------------------------------

// TestON008a_CognitionLoopStatusDeclaresRequiredValues confirms that the
// CognitionLoopStatus type declares all seven LoopStatus values from
// cognition-loop.md §6, including the two pause-reason states that MUST be
// surfaced to the operator per ON-008a.
func TestON008a_CognitionLoopStatusDeclaresRequiredValues(t *testing.T) {
	t.Parallel()

	required := []struct {
		value CognitionLoopStatus
		label string
	}{
		{CognitionLoopStatusStarting, "starting"},
		{CognitionLoopStatusReady, "ready"},
		{CognitionLoopStatusPaused, "paused"},
		{CognitionLoopStatusBudgetPaused, "budget-paused"},
		{CognitionLoopStatusCircuitTripped, "circuit-tripped"},
		{CognitionLoopStatusDraining, "draining"},
		{CognitionLoopStatusStopped, "stopped"},
	}

	for _, tc := range required {
		if string(tc.value) != tc.label {
			t.Errorf("CognitionLoopStatus %q has wire value %q; want %q (cognition-loop.md §6 LoopStatus)",
				tc.label, string(tc.value), tc.label)
		}
		if !tc.value.IsKnown() {
			t.Errorf("CognitionLoopStatus %q: IsKnown() = false; all declared values must be known",
				tc.label)
		}
	}
}

// TestON008a_BudgetPausedAndCircuitTrippedAreKnown verifies the two
// pause-reason states that ON-008a specifically requires to be surfaced.
func TestON008a_BudgetPausedAndCircuitTrippedAreKnown(t *testing.T) {
	t.Parallel()

	for _, status := range []CognitionLoopStatus{
		CognitionLoopStatusBudgetPaused,
		CognitionLoopStatusCircuitTripped,
	} {
		if !status.IsKnown() {
			t.Errorf("ON-008a: CognitionLoopStatus %q IsKnown() = false; must be a declared value so the operator surface can report it", status)
		}
	}
}

// ---------------------------------------------------------------------------
// TestON008a_LoopStatusRoundTrip
// ---------------------------------------------------------------------------

// TestON008a_LoopStatusRoundTrip verifies that WriteLoopStatusAtomic followed
// by ReadLoopStatus produces the original record without loss.
func TestON008a_LoopStatusRoundTrip(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	cases := []LoopStatusRecord{
		{
			SchemaVersion: 1,
			Status:        CognitionLoopStatusBudgetPaused,
			UpdatedAt:     "2026-01-01T00:00:00Z",
			PauseReason:   "budget-paused",
		},
		{
			SchemaVersion: 1,
			Status:        CognitionLoopStatusCircuitTripped,
			UpdatedAt:     "2026-01-02T00:00:00Z",
			PauseReason:   "circuit-tripped",
		},
		{
			SchemaVersion: 1,
			Status:        CognitionLoopStatusReady,
		},
	}

	for _, want := range cases {
		if err := WriteLoopStatusAtomic(dir, want); err != nil {
			t.Fatalf("ON-008a: WriteLoopStatusAtomic(%q): %v", want.Status, err)
		}

		got, err := ReadLoopStatus(dir)
		if err != nil {
			t.Fatalf("ON-008a: ReadLoopStatus: %v", err)
		}
		if got == nil {
			t.Fatal("ON-008a: ReadLoopStatus returned nil; loop-status.json must exist after write")
		}

		if got.SchemaVersion != want.SchemaVersion {
			t.Errorf("ON-008a: schema_version: got %d, want %d", got.SchemaVersion, want.SchemaVersion)
		}
		if got.Status != want.Status {
			t.Errorf("ON-008a: status: got %q, want %q", got.Status, want.Status)
		}
		if got.PauseReason != want.PauseReason {
			t.Errorf("ON-008a: pause_reason: got %q, want %q", got.PauseReason, want.PauseReason)
		}
	}
}

// TestON008a_ReadLoopStatusAbsent verifies ReadLoopStatus returns nil (not an
// error) when loop-status.json does not exist — the cognition loop may not
// have written status yet.
func TestON008a_ReadLoopStatusAbsent(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	rec, err := ReadLoopStatus(dir)
	if err != nil {
		t.Fatalf("ON-008a: ReadLoopStatus on missing file: got error %v; want nil", err)
	}
	if rec != nil {
		t.Errorf("ON-008a: ReadLoopStatus on missing file: got %+v; want nil", rec)
	}
}

// ---------------------------------------------------------------------------
// TestON008a_BuildStatusIncludesLoopStatus
// ---------------------------------------------------------------------------

// TestON008a_BuildStatusIncludesLoopStatus verifies that buildStatus populates
// LoopStatus and PauseReason from loop-status.json, surfacing the
// budget-paused and circuit-tripped states to the operator per ON-008a.
func TestON008a_BuildStatusIncludesLoopStatus(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		rec         LoopStatusRecord
		wantStatus  string
		wantReason  string
	}{
		{
			name:       "budget-paused",
			rec:        LoopStatusRecord{SchemaVersion: 1, Status: CognitionLoopStatusBudgetPaused, PauseReason: "budget-paused"},
			wantStatus: "budget-paused",
			wantReason: "budget-paused",
		},
		{
			name:       "circuit-tripped",
			rec:        LoopStatusRecord{SchemaVersion: 1, Status: CognitionLoopStatusCircuitTripped, PauseReason: "circuit-tripped"},
			wantStatus: "circuit-tripped",
			wantReason: "circuit-tripped",
		},
		{
			name:       "ready",
			rec:        LoopStatusRecord{SchemaVersion: 1, Status: CognitionLoopStatusReady},
			wantStatus: "ready",
			wantReason: "",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			if err := WriteLoopStatusAtomic(dir, tc.rec); err != nil {
				t.Fatalf("WriteLoopStatusAtomic: %v", err)
			}

			result := buildStatus(dir)

			if result.LoopStatus != tc.wantStatus {
				t.Errorf("ON-008a: StatusResult.LoopStatus = %q; want %q", result.LoopStatus, tc.wantStatus)
			}
			if result.PauseReason != tc.wantReason {
				t.Errorf("ON-008a: StatusResult.PauseReason = %q; want %q", result.PauseReason, tc.wantReason)
			}
		})
	}
}

// TestON008a_BuildStatusNoLoopStatusWhenFileAbsent confirms that buildStatus
// leaves LoopStatus empty when no loop-status.json is present (cognition loop
// not yet started).
func TestON008a_BuildStatusNoLoopStatusWhenFileAbsent(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	result := buildStatus(dir)

	if result.LoopStatus != "" {
		t.Errorf("ON-008a: StatusResult.LoopStatus = %q; want empty when loop-status.json absent", result.LoopStatus)
	}
	if result.PauseReason != "" {
		t.Errorf("ON-008a: StatusResult.PauseReason = %q; want empty when loop-status.json absent", result.PauseReason)
	}
}

// ---------------------------------------------------------------------------
// TestON008a_HumanReadableOutputIncludesLoopStatus
// ---------------------------------------------------------------------------

// TestON008a_HumanReadableOutputIncludesLoopStatus verifies that RunStatus in
// text mode emits loop_status and pause_reason lines when the file is present.
// This encodes the operator-visible surface for budget-paused (ON-008a).
func TestON008a_HumanReadableOutputIncludesLoopStatus(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	rec := LoopStatusRecord{
		SchemaVersion: 1,
		Status:        CognitionLoopStatusBudgetPaused,
		PauseReason:   "budget-paused",
	}
	if err := WriteLoopStatusAtomic(dir, rec); err != nil {
		t.Fatalf("WriteLoopStatusAtomic: %v", err)
	}

	var stdout, stderr bytes.Buffer
	code := RunStatus([]string{"--project", dir}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("RunStatus exit code = %d; want 0; stderr=%q", code, stderr.String())
	}

	out := stdout.String()
	if !strings.Contains(out, "budget-paused") {
		t.Errorf("ON-008a: status output missing 'budget-paused'; want loop_status line; got:\n%s", out)
	}
	if !strings.Contains(out, "loop_status") {
		t.Errorf("ON-008a: status output missing 'loop_status:' label; got:\n%s", out)
	}
}

// TestON008a_JSONOutputIncludesLoopStatusFields verifies that RunStatus in
// JSON mode includes loop_status and pause_reason in the emitted object.
func TestON008a_JSONOutputIncludesLoopStatusFields(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	rec := LoopStatusRecord{
		SchemaVersion: 1,
		Status:        CognitionLoopStatusCircuitTripped,
		PauseReason:   "circuit-tripped",
	}
	if err := WriteLoopStatusAtomic(dir, rec); err != nil {
		t.Fatalf("WriteLoopStatusAtomic: %v", err)
	}

	var stdout, stderr bytes.Buffer
	code := RunStatus([]string{"--project", dir, "--json"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("RunStatus --json exit code = %d; want 0; stderr=%q", code, stderr.String())
	}

	var result StatusResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("ON-008a: unmarshal JSON output: %v; raw=%q", err, stdout.String())
	}

	if result.LoopStatus != "circuit-tripped" {
		t.Errorf("ON-008a: JSON loop_status = %q; want circuit-tripped", result.LoopStatus)
	}
	if result.PauseReason != "circuit-tripped" {
		t.Errorf("ON-008a: JSON pause_reason = %q; want circuit-tripped", result.PauseReason)
	}
}

// ---------------------------------------------------------------------------
// TestON008a_LoopStatusPathUnderCognitionDir
// ---------------------------------------------------------------------------

// TestON008a_LoopStatusPathUnderCognitionDir verifies that LoopStatusPath
// returns a path inside .harmonik/cognition/ — the gitignored cognition
// directory used by the supervise file surface.
func TestON008a_LoopStatusPathUnderCognitionDir(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := LoopStatusPath(dir)
	cognitionDir := CognitionDir(dir)

	if !strings.HasPrefix(path, cognitionDir) {
		t.Errorf("ON-008a: LoopStatusPath %q is not under CognitionDir %q; loop-status.json must be gitignored under cognition/",
			path, cognitionDir)
	}

	base := path[len(cognitionDir):]
	if !strings.Contains(base, "loop-status.json") {
		t.Errorf("ON-008a: LoopStatusPath %q does not end with loop-status.json; got suffix %q", path, base)
	}
}

// ---------------------------------------------------------------------------
// TestON008a_UnknownStatusStringRejected
// ---------------------------------------------------------------------------

// TestON008a_UnknownStatusStringRejected verifies that an unrecognised string
// is not reported as IsKnown — preventing spurious values from being accepted
// silently.
func TestON008a_UnknownStatusStringRejected(t *testing.T) {
	t.Parallel()

	bogus := CognitionLoopStatus("not-a-real-status")
	if bogus.IsKnown() {
		t.Errorf("ON-008a: CognitionLoopStatus(%q).IsKnown() = true; unexpected string must not be known", bogus)
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// readFile is a thin wrapper used in tests that need to verify the raw JSON.
func readFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path) //nolint:gosec
	if err != nil {
		t.Fatalf("readFile %q: %v", path, err)
	}
	return data
}
