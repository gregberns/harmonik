package workspace

import (
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
)

// TestWM040_SetInterruptStateToNone_OperatorResuming verifies that
// SetInterruptStateToNone clears interrupt_state with operator_resuming cause.
//
// Spec ref: workspace-model.md §4.10 WM-040 clause (a) — "an operator_resuming
// event … for operator-initiated interrupts."
func TestWM040_SetInterruptStateToNone_OperatorResuming(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	ws := &Workspace{
		WorkspaceID:    "ws-0196b300-0000-7000-8000-000000040010",
		State:          core.WorkspaceStateLeased,
		InterruptState: core.InterruptStateOperatorPaused,
		Metadata:       map[string]string{"created_at": "2026-05-07T00:00:00Z", "operator_fingerprint": "test"},
		SchemaVersion:  1,
	}

	runID := "0196b300-0000-7000-8000-000000040010"
	err := SetInterruptStateToNone(ws, dir, runID, InterruptStateClearCauseOperatorResuming)
	if err != nil {
		t.Fatalf("WM-040 operator-resume: SetInterruptStateToNone: %v", err)
	}
	if ws.InterruptState != core.InterruptStateNone {
		t.Errorf("WM-040 operator-resume: interrupt_state = %q, want none", ws.InterruptState)
	}
}

// TestWM040_SetInterruptStateToNone_ReconciliationVerdict verifies that
// SetInterruptStateToNone clears interrupt_state with reconciliation_verdict cause.
//
// Spec ref: workspace-model.md §4.10 WM-040 clause (b) — "a reconciliation
// verdict … for daemon-crash or lost-lease interrupts."
func TestWM040_SetInterruptStateToNone_ReconciliationVerdict(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	ws := &Workspace{
		WorkspaceID:    "ws-0196b300-0000-7000-8000-000000040011",
		State:          core.WorkspaceStateLeased,
		InterruptState: core.InterruptStateDaemonCrashSuspected,
		Metadata:       map[string]string{"created_at": "2026-05-07T00:00:00Z", "operator_fingerprint": "test"},
		SchemaVersion:  1,
	}

	runID := "0196b300-0000-7000-8000-000000040011"
	err := SetInterruptStateToNone(ws, dir, runID, InterruptStateClearCauseReconciliationVerdict)
	if err != nil {
		t.Fatalf("WM-040 recon-verdict: SetInterruptStateToNone: %v", err)
	}
	if ws.InterruptState != core.InterruptStateNone {
		t.Errorf("WM-040 recon-verdict: interrupt_state = %q, want none", ws.InterruptState)
	}
}

// TestWM040_SetInterruptStateToNone_EmptyCauseIsRejected verifies that
// SetInterruptStateToNone returns ErrInterruptStateClearRequiresCause when the
// cause is empty, and does NOT mutate the field.
//
// Spec ref: workspace-model.md §4.10 WM-040 — "The workspace manager MUST NOT
// silently clear the field."
func TestWM040_SetInterruptStateToNone_EmptyCauseIsRejected(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	ws := &Workspace{
		WorkspaceID:    "ws-0196b300-0000-7000-8000-000000040012",
		State:          core.WorkspaceStateLeased,
		InterruptState: core.InterruptStateOperatorPaused,
		Metadata:       map[string]string{"created_at": "2026-05-07T00:00:00Z", "operator_fingerprint": "test"},
		SchemaVersion:  1,
	}

	runID := "0196b300-0000-7000-8000-000000040012"
	err := SetInterruptStateToNone(ws, dir, runID, "")
	if err == nil {
		t.Fatal("WM-040 empty-cause: expected error, got nil")
	}
	if !errors.Is(err, ErrInterruptStateClearRequiresCause) {
		t.Errorf("WM-040 empty-cause: error = %v, want ErrInterruptStateClearRequiresCause", err)
	}
	// Field MUST NOT be mutated on error.
	if ws.InterruptState != core.InterruptStateOperatorPaused {
		t.Errorf("WM-040 empty-cause: interrupt_state mutated to %q; want operator-paused (unchanged)", ws.InterruptState)
	}
}

// TestWM040_SetInterruptStateToNone_UnrecognisedCauseIsRejected verifies that
// an unrecognised cause string returns ErrInterruptStateClearRequiresCause.
//
// Spec ref: workspace-model.md §4.10 WM-040 — silent clears are forbidden.
func TestWM040_SetInterruptStateToNone_UnrecognisedCauseIsRejected(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	ws := &Workspace{
		WorkspaceID:    "ws-0196b300-0000-7000-8000-000000040013",
		State:          core.WorkspaceStateLeased,
		InterruptState: core.InterruptStateOperatorStoppedGraceful,
		Metadata:       map[string]string{"created_at": "2026-05-07T00:00:00Z", "operator_fingerprint": "test"},
		SchemaVersion:  1,
	}

	runID := "0196b300-0000-7000-8000-000000040013"
	err := SetInterruptStateToNone(ws, dir, runID, "silent-clear-attempt")
	if err == nil {
		t.Fatal("WM-040 unrecognised-cause: expected error, got nil")
	}
	if !errors.Is(err, ErrInterruptStateClearRequiresCause) {
		t.Errorf("WM-040 unrecognised-cause: error = %v, want ErrInterruptStateClearRequiresCause", err)
	}
	// Field MUST NOT be mutated.
	if ws.InterruptState != core.InterruptStateOperatorStoppedGraceful {
		t.Errorf("WM-040 unrecognised-cause: interrupt_state changed; want operator-stopped-graceful (unchanged)")
	}
}

// TestWM040_SetInterruptStateToNone_WritesMarker verifies that
// SetInterruptStateToNone appends an interrupt_state_changed JSONL marker to
// the workspace-local events file per WM-038a.
//
// Spec ref: workspace-model.md §4.10 WM-038a — "the workspace manager MUST
// on every interrupt_state mutation … append a single workspace-scoped JSONL
// line … and fsync."
func TestWM040_SetInterruptStateToNone_WritesMarker(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	workspaceID := "ws-0196b300-0000-7000-8000-000000040014"
	runID := "0196b300-0000-7000-8000-000000040014"

	ws := &Workspace{
		WorkspaceID:    workspaceID,
		State:          core.WorkspaceStateLeased,
		InterruptState: core.InterruptStateDaemonCrashSuspected,
		Metadata:       map[string]string{"created_at": "2026-05-07T00:00:00Z", "operator_fingerprint": "test"},
		SchemaVersion:  1,
	}

	// Capture bracket times in UTC (marker writes in UTC) to avoid timezone comparison issues.
	before := time.Now().UTC().Truncate(time.Second) // RFC3339 has 1s precision
	err := SetInterruptStateToNone(ws, dir, runID, InterruptStateClearCauseReconciliationVerdict)
	if err != nil {
		t.Fatalf("WM-040 marker: SetInterruptStateToNone: %v", err)
	}
	after := time.Now().UTC().Add(time.Second) // +1s for RFC3339 truncation tolerance

	// Read and parse the marker.
	eventsFile := WorkspaceLocalEventsPath(dir, workspaceID)
	//nolint:gosec // G304: path constructed from t.TempDir() + known relative segments, not user input
	data, readErr := os.ReadFile(eventsFile)
	if readErr != nil {
		t.Fatalf("WM-040 marker: ReadFile %q: %v", eventsFile, readErr)
	}

	// Trim trailing newline before unmarshalling.
	if len(data) > 0 && data[len(data)-1] == '\n' {
		data = data[:len(data)-1]
	}
	var marker map[string]string
	if jsonErr := json.Unmarshal(data, &marker); jsonErr != nil {
		t.Fatalf("WM-040 marker: json.Unmarshal: %v\nraw: %s", jsonErr, data)
	}

	checks := []struct {
		key  string
		want string
	}{
		{"event", "interrupt_state_changed"},
		{"workspace_id", workspaceID},
		{"run_id", runID},
		{"prior_interrupt_state", string(core.InterruptStateDaemonCrashSuspected)},
		{"new_interrupt_state", string(core.InterruptStateNone)},
		{"cause", string(InterruptStateClearCauseReconciliationVerdict)},
	}
	for _, c := range checks {
		if marker[c.key] != c.want {
			t.Errorf("WM-040 marker: field %q = %q, want %q", c.key, marker[c.key], c.want)
		}
	}

	// changed_at must be a valid RFC 3339 timestamp within [before, after].
	changedAt, parseErr := time.Parse(time.RFC3339, marker["changed_at"])
	if parseErr != nil {
		t.Fatalf("WM-040 marker: changed_at parse: %v (raw: %q)", parseErr, marker["changed_at"])
	}
	if changedAt.Before(before) || changedAt.After(after) {
		t.Errorf("WM-040 marker: changed_at %v not in [%v, %v]", changedAt, before, after)
	}
}

// TestWM040_SetInterruptStateToNone_AllInterruptValues verifies that
// SetInterruptStateToNone works for all non-none interrupt_state values.
//
// Spec ref: workspace-model.md §4.10 WM-040 — applies to any non-none value.
func TestWM040_SetInterruptStateToNone_AllInterruptValues(t *testing.T) {
	t.Parallel()

	nonNoneValues := []core.InterruptState{
		core.InterruptStateOperatorPaused,
		core.InterruptStateOperatorStoppedGraceful,
		core.InterruptStateOperatorStoppedImmediate,
		core.InterruptStateDaemonCrashSuspected,
	}

	for _, iv := range nonNoneValues {
		iv := iv
		t.Run(string(iv), func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			ws := &Workspace{
				WorkspaceID:    "ws-0196b300-0000-7000-8000-000000040020",
				State:          core.WorkspaceStateLeased,
				InterruptState: iv,
				Metadata:       map[string]string{"created_at": "2026-05-07T00:00:00Z", "operator_fingerprint": "test"},
				SchemaVersion:  1,
			}

			err := SetInterruptStateToNone(ws, dir, "0196b300-0000-7000-8000-000000040020",
				InterruptStateClearCauseOperatorResuming)
			if err != nil {
				t.Fatalf("WM-040 all-values[%s]: SetInterruptStateToNone: %v", iv, err)
			}
			if ws.InterruptState != core.InterruptStateNone {
				t.Errorf("WM-040 all-values[%s]: interrupt_state = %q, want none", iv, ws.InterruptState)
			}
		})
	}
}

// TestWM040_WriteInterruptStateChangedMarker_CreatesFileIfAbsent verifies that
// WriteInterruptStateChangedMarker creates the events file (and its parent
// directories) if they do not exist.
//
// Spec ref: workspace-model.md §4.10 WM-038a — marker file created on first
// mutation.
func TestWM040_WriteInterruptStateChangedMarker_CreatesFileIfAbsent(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	workspaceID := "ws-0196b300-0000-7000-8000-000000040030"
	runID := "0196b300-0000-7000-8000-000000040030"

	err := WriteInterruptStateChangedMarker(
		dir, workspaceID, runID,
		"operator-paused", "none", "operator_resuming",
	)
	if err != nil {
		t.Fatalf("WM-040 create-file: WriteInterruptStateChangedMarker: %v", err)
	}

	eventsFile := WorkspaceLocalEventsPath(dir, workspaceID)
	if _, statErr := os.Stat(eventsFile); os.IsNotExist(statErr) {
		t.Errorf("WM-040 create-file: events file %q not created", eventsFile)
	}
}

// TestWM040_WriteInterruptStateChangedMarker_IsAppendOnly verifies that
// multiple markers append to the same file rather than overwriting.
//
// Spec ref: workspace-model.md §4.10 WM-038a — file is append-only.
func TestWM040_WriteInterruptStateChangedMarker_IsAppendOnly(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	workspaceID := "ws-0196b300-0000-7000-8000-000000040031"
	runID := "0196b300-0000-7000-8000-000000040031"

	// Write two markers.
	for i := 0; i < 2; i++ {
		if err := WriteInterruptStateChangedMarker(
			dir, workspaceID, runID,
			"daemon-crash-suspected", "none", "reconciliation_verdict",
		); err != nil {
			t.Fatalf("WM-040 append-only: WriteInterruptStateChangedMarker[%d]: %v", i, err)
		}
	}

	eventsFile := WorkspaceLocalEventsPath(dir, workspaceID)
	//nolint:gosec // G304: path constructed from t.TempDir() + known relative segments, not user input
	data, err := os.ReadFile(eventsFile)
	if err != nil {
		t.Fatalf("WM-040 append-only: ReadFile: %v", err)
	}

	// Count newlines: two appends → two JSONL lines.
	lineCount := 0
	for _, b := range data {
		if b == '\n' {
			lineCount++
		}
	}
	if lineCount != 2 {
		t.Errorf("WM-040 append-only: file has %d lines, want 2", lineCount)
	}
}
