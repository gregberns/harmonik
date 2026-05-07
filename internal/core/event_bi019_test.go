// Package core — BI-019 tests for bead-scoped event payload discipline.
//
// Per beads-integration.md §4 BI-019: every event emitted for a bead-bound run
// MUST carry "bead_id" on its payload; events not scoped to a specific run
// (e.g., daemon lifecycle) MUST omit the field. The tests below verify the
// PayloadHasBeadID structural primitive against all five sentinel cases.
package core

import "testing"

// TestEventBI019_BeadBoundPayloadCarriesBeadID verifies that a payload containing
// a non-empty "bead_id" string satisfies BI-019 (PayloadHasBeadID == true).
func TestEventBI019_BeadBoundPayloadCarriesBeadID(t *testing.T) {
	t.Parallel()

	payload := map[string]any{
		"bead_id": "bead-001",
	}
	if !PayloadHasBeadID(payload) {
		t.Error("PayloadHasBeadID = false for payload with non-empty bead_id string, want true (BI-019)")
	}
}

// TestEventBI019_DaemonLifecyclePayloadOmitsBeadID verifies that a payload that
// does not contain the "bead_id" key returns false, matching the daemon-lifecycle
// event rule in BI-019.
func TestEventBI019_DaemonLifecyclePayloadOmitsBeadID(t *testing.T) {
	t.Parallel()

	payload := map[string]any{
		"reason": "startup",
	}
	if PayloadHasBeadID(payload) {
		t.Error("PayloadHasBeadID = true for payload without bead_id key, want false (BI-019)")
	}
}

// TestEventBI019_NilPayload verifies that a nil map returns false per BI-019.
func TestEventBI019_NilPayload(t *testing.T) {
	t.Parallel()

	if PayloadHasBeadID(nil) {
		t.Error("PayloadHasBeadID = true for nil payload, want false (BI-019)")
	}
}

// TestEventBI019_EmptyBeadIDValueIsRejected verifies that a payload with
// "bead_id" mapped to an empty string returns false. BI-019 requires a
// non-empty string value.
func TestEventBI019_EmptyBeadIDValueIsRejected(t *testing.T) {
	t.Parallel()

	payload := map[string]any{
		"bead_id": "",
	}
	if PayloadHasBeadID(payload) {
		t.Error("PayloadHasBeadID = true for payload with empty bead_id string, want false (BI-019)")
	}
}

// TestEventBI019_NonStringBeadIDValueIsRejected verifies that a payload with
// "bead_id" mapped to a non-string value (e.g., int) returns false. BI-019
// requires the value to be a non-empty string.
func TestEventBI019_NonStringBeadIDValueIsRejected(t *testing.T) {
	t.Parallel()

	payload := map[string]any{
		"bead_id": 42,
	}
	if PayloadHasBeadID(payload) {
		t.Error("PayloadHasBeadID = true for payload with non-string bead_id value, want false (BI-019)")
	}
}
