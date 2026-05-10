// Package core — BI-020 forward-doc sensor.
//
// This file provides a forward-doc sensor for beads-integration.md §4 BI-020.
//
// BI-020: "Session logs produced for an agent subprocess running as part of a
// bead-bound run MUST carry bead_id metadata per [workspace-model.md §4.7].
// CASS indexing uses this metadata for join-to-Beads queries."
//
// SessionMetadataSidecar (workspace-model.md §6.1) is now implemented in
// internal/workspace/sessionmetadatasidecar_wm063.go (bead hk-8mwo.63).
// Concrete assertions on the bead_id field live in
// internal/workspace/sessionmetadatasidecar_wm063_test.go.
//
// Requirement-traceable bead: hk-872.21.
package core

import "testing"

// TestSessionBI020_ForwardDocSensor is a documentation-marker test for
// beads-integration.md §4 BI-020 (hk-872.21).
//
// BI-020 requires that session-log sidecar metadata MUST include bead_id for
// bead-bound runs; CASS depends on this for join-to-Beads queries. The field
// is defined at workspace-model.md §6.1 SessionMetadataSidecar.bead_id.
//
// SessionMetadataSidecar is now implemented in the workspace package
// (hk-8mwo.63). Concrete bead_id assertions are in
// internal/workspace/sessionmetadatasidecar_wm063_test.go.
func TestSessionBI020_ForwardDocSensor(t *testing.T) {
	t.Log("BI-020 (hk-872.21): session-log sidecar metadata MUST include bead_id for bead-bound runs.")
	t.Log("Spec reference: beads-integration.md §4 BI-020; workspace-model.md §6.1 SessionMetadataSidecar.bead_id.")
	t.Log("SessionMetadataSidecar implemented in internal/workspace (hk-8mwo.63).")
	// No skip — the record now exists; this test documents the BI-020 traceability.
}

// TestSessionBI020_SessionIDTypedAliasExists verifies that the SessionID typed
// alias (the only currently-implemented session primitive) round-trips its
// underlying string value. This grounds the BI-020 sensor in a concrete,
// compilable assertion while the broader SessionMetadataSidecar record is still
// pending.
//
// BI-020 citation: beads-integration.md §4 BI-020 (hk-872.21).
func TestSessionBI020_SessionIDTypedAliasExists(t *testing.T) {
	const raw = "0196b1c2-d3e4-7000-9f2a-000000000020"
	s := SessionID(raw)
	if got := string(s); got != raw {
		t.Errorf("SessionID string round-trip failed: got %q, want %q", got, raw)
	}
}
