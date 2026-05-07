// Package core — EM-014 record-shape sensors.
//
// This file exists to provide named, requirement-traceable sensors for the
// many-runs-per-bead invariant described in execution-model.md §4.3 EM-014:
//
//	A Run MAY be tied to a bead via bead_id. A bead MAY have zero runs (not yet
//	claimed), one run (active or completed), or many runs across its lifetime (a
//	prior run failed fundamentally — crash, unrecoverable error, or reopen-bead
//	verdict — and a subsequent claim spawned a new run).
//
// Each test function documents the specific EM-014 case it covers. The sensors
// operate entirely at the record-shape level: they verify that the Run struct
// and its Valid() method permit the many-runs-per-bead shape without imposing
// uniqueness constraints on BeadID across Run instances.
//
// Out of scope: worktree/branch lifecycle (a separate subsystem; workspace-model.md
// §4.9 owns fresh-worktree creation on re-run). No new exports are added.
package core

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

// TestRunEM014_ManyRunsShareBeadID exercises the core many-runs-per-bead
// invariant from execution-model.md §4.3 EM-014: multiple Run records MAY
// share the same BeadID, and each such Run is individually valid.
//
// Scenario: a bead is claimed three times (e.g., two prior fundamental failures
// followed by a live run). Each claim produces a distinct RunID and StartTime,
// but all three point to the same BeadID. The Run.Valid() method must accept
// each of them independently — it has no cross-Run uniqueness check and should
// not have one.
func TestRunEM014_ManyRunsShareBeadID(t *testing.T) {
	t.Parallel()

	shared := BeadID("bead-shared-001")

	// Build N=3 Run records with distinct RunID/StartTime but the same BeadID.
	runs := make([]Run, 3)
	for i := range runs {
		r := validRun(t)
		// Override BeadID with the shared value to assert the many-runs shape.
		r.BeadID = &shared
		// Ensure StartTime is distinct across the three runs (monotonically later).
		r.StartTime = time.Now().Add(time.Duration(i) * time.Second)
		end := r.StartTime.Add(time.Minute)
		r.EndTime = &end
		// Assign a fresh RunID so the three Runs are genuinely distinct.
		r.RunID = RunID(uuid.Must(uuid.NewV7()))
		runs[i] = r
	}

	// Each Run must be individually valid (EM-014: Valid() is per-record).
	for i, r := range runs {
		if !r.Valid() {
			t.Errorf("run[%d].Valid() = false, want true (EM-014 shared BeadID must be individually valid)", i)
		}
	}

	// Sanity: all three RunIDs must be distinct — confirms we actually built
	// three different Run records rather than aliasing the same one.
	seen := make(map[RunID]bool)
	for _, r := range runs {
		if seen[r.RunID] {
			t.Errorf("duplicate RunID %v — test is broken, did not build distinct Run records", r.RunID)
		}
		seen[r.RunID] = true
	}
}

// TestRunEM014_OneRunForBead is a minimal EM-014 sensor for the single-run case:
// a bead has exactly one Run tied to it. Although this overlaps with
// TestRunValid_AllFieldsSet in run_test.go, the explicit EM-014 name provides
// requirement traceability — a grep for EM-014 finds this test directly.
//
// EM-014 reference: execution-model.md §4.3 EM-014.
func TestRunEM014_OneRunForBead(t *testing.T) {
	t.Parallel()

	r := validRun(t) // validRun already sets a non-nil BeadID.
	if !r.Valid() {
		t.Error("Valid() = false for single-bead-tied Run, want true (EM-014 one-run case)")
	}
}

// TestRunEM014_ZeroRunsForBead documents the zero-runs case from EM-014:
// a bead that has been created in Beads but not yet claimed has no Run record.
// No Run construction is needed because the Run type simply does not exist for
// that bead yet — the absence of a Run is the correct state. This test exists
// solely to provide a named EM-014 sensor that `go test -run EM014` will report.
//
// EM-014 reference: execution-model.md §4.3 EM-014.
func TestRunEM014_ZeroRunsForBead(t *testing.T) {
	t.Parallel()

	// Zero-runs is the natural state when a bead exists in Beads but has never
	// been claimed. The Go Run type has no "bead exists" record; the absence of
	// any Run with that BeadID is the correct representation. Nothing to assert
	// at the record-shape level — the invariant is satisfied by the type not
	// existing, which is always true before any Run is created.
	//
	// This test is intentionally a no-op assertion; its presence as a named
	// EM-014 sensor satisfies the requirement-traceability goal of this file.
}
