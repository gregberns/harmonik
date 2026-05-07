// Package core — BI-020 forward-doc sensor.
//
// This file provides a forward-doc sensor for beads-integration.md §4 BI-020.
//
// BI-020: "Session logs produced for an agent subprocess running as part of a
// bead-bound run MUST carry bead_id metadata per [workspace-model.md §4.7].
// CASS indexing uses this metadata for join-to-Beads queries."
//
// The SessionMetadataSidecar Go record (workspace-model.md §6.1) is NOT yet
// shipped; no sessionmetadatasidecar.go exists in this package. This file
// exists so that future implementers of the sidecar will discover BI-020 by
// name when they search the test suite.
//
// When SessionMetadataSidecar lands, the future bead implementer SHOULD either:
//   - Delete the skip-only marker TestSessionBI020_ForwardDocSensor and replace
//     it with concrete assertions on the new record's bead_id field, OR
//   - Extend the marker with those concrete assertions, retaining the BI-020
//     citation and hk-872.21 traceability.
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
// This test skips unconditionally because SessionMetadataSidecar is not yet a
// Go record. It exists solely as a discoverable anchor in the test suite. When
// SessionMetadataSidecar is implemented, replace or extend this marker with
// concrete assertions on the bead_id field.
//
// See also: TestSessionBI020_SessionIDTypedAliasExists for the only currently
// implemented session primitive.
func TestSessionBI020_ForwardDocSensor(t *testing.T) {
	t.Log("BI-020 (hk-872.21): session-log sidecar metadata MUST include `bead_id` for bead-bound runs.")
	t.Log("CASS depends on this field for join-to-Beads queries.")
	t.Log("Spec reference: beads-integration.md §4 BI-020; field definition: workspace-model.md §6.1 SessionMetadataSidecar.bead_id.")
	t.Log("")
	t.Log("SessionMetadataSidecar (workspace-model.md §6.1) is not yet a Go record.")
	t.Log("When it lands, the implementer of that bead SHOULD:")
	t.Log("  1. Delete this skip-only marker, OR")
	t.Log("  2. Extend it with concrete assertions on the bead_id field.")
	t.Log("Requirement-traceable bead: hk-872.21.")
	t.SkipNow()
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
