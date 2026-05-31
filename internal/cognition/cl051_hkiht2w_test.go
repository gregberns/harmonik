package cognition_test

// cl051_hkiht2w_test.go — scenario test for specs/cognition-loop.md §4.7 CL-051
// (two-phase done).
//
// CL-051 specifies that a bead is DONE only when BOTH conditions are met:
//  1. run_completed{success} event observed for the bead.
//  2. git log origin/main --grep "Refs: <bead-id>" --max-count=1 non-empty.
//
// This file covers §7 acceptance scenario 8:
//
//   A run_completed{success} for hk-XYZ whose Refs: trailer is absent on
//   origin/main does NOT mark the bead done: the harness treats it as in-flight
//   and re-polls; conversely a Refs: trailer present on origin/main with no
//   terminal event emits loop_observed_phantom_done and routes to Tier-2
//   reconciliation without direct action (CL-051).
//
// Scenarios exercised:
//
//	CL051-S1: event only (Condition 1 met, Condition 2 absent)
//	  → DoneStatusInFlight  (must NOT be marked done; re-poll Condition 2)
//
//	CL051-S2: trailer only (Condition 2 met, Condition 1 absent)
//	  → DoneStatusPhantomDone  (loop_observed_phantom_done; route to Tier-2)
//
//	CL051-S3: both conditions met
//	  → DoneStatusDone  (positive control; confirms the checker works)
//
//	CL051-S4: neither condition met
//	  → DoneStatusInFlight  (baseline: not-yet-done bead is still in-flight)
//
// The test builds a minimal git fixture with a local "origin" bare repo so
// that "origin/main" resolves via the standard git remote-tracking ref.
//
// Spec ref: specs/cognition-loop.md §4.7 CL-051, §7 acceptance scenario 8.
// Bead: hk-iht2w.

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/gregberns/harmonik/internal/cognition"
	"github.com/gregberns/harmonik/internal/core"
)

// cl051FixtureSetup creates a git repo fixture for CL-051 tests.
//
// It creates:
//   - A bare "origin" repo at <tmp>/origin.git
//   - A working clone at <tmp>/work
//   - An initial commit on main in the working clone and pushes to origin
//
// Returns workDir (the working clone path).
func cl051FixtureSetup(t *testing.T) string {
	t.Helper()

	base := t.TempDir()
	originDir := filepath.Join(base, "origin.git")
	workDir := filepath.Join(base, "work")

	runGit := func(dir string, args ...string) {
		t.Helper()
		//nolint:gosec // G204: args are constant strings in tests; not user-supplied.
		cmd := exec.CommandContext(t.Context(), "git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("cl051Fixture: git %v in %s: %v\n%s", args, dir, err, out)
		}
	}

	// Create bare origin.
	if err := os.MkdirAll(originDir, 0o755); err != nil {
		t.Fatalf("cl051Fixture: mkdir origin: %v", err)
	}
	runGit(originDir, "init", "--bare", "--initial-branch=main")

	// Clone into working directory.
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatalf("cl051Fixture: mkdir work: %v", err)
	}
	runGit(base, "clone", originDir, "work")

	// Configure identity in the clone.
	runGit(workDir, "config", "user.email", "test@harmonik.local")
	runGit(workDir, "config", "user.name", "Harmonik CL051 Test")

	// Initial commit.
	readme := filepath.Join(workDir, "README")
	if err := os.WriteFile(readme, []byte("cl051 test fixture\n"), 0o644); err != nil {
		t.Fatalf("cl051Fixture: write README: %v", err)
	}
	runGit(workDir, "add", "README")
	runGit(workDir, "commit", "-m", "Initial commit")
	runGit(workDir, "push", "origin", "main")

	return workDir
}

// cl051FixtureCommitWithRefsTrailer adds a commit on origin/main that carries
// the "Refs: <beadID>" trailer and pushes it, making the trailer visible via
// git log origin/main.
func cl051FixtureCommitWithRefsTrailer(t *testing.T, workDir, beadID string) {
	t.Helper()

	runGit := func(args ...string) {
		t.Helper()
		//nolint:gosec // G204: args are constant strings; workDir is t.TempDir()-based.
		cmd := exec.CommandContext(t.Context(), "git", args...)
		cmd.Dir = workDir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("cl051FixtureCommit: git %v: %v\n%s", args, err, out)
		}
	}

	// Write a file change so the commit is non-empty.
	f := filepath.Join(workDir, "cl051-marker.txt")
	if err := os.WriteFile(f, []byte("marker for "+beadID+"\n"), 0o644); err != nil {
		t.Fatalf("cl051FixtureCommit: write marker: %v", err)
	}
	runGit("add", "cl051-marker.txt")

	// Commit message with Refs: trailer.
	msg := "feat: cl051 test\n\nRefs: " + beadID
	runGit("commit", "-m", msg)
	runGit("push", "origin", "main")
}

// ─────────────────────────────────────────────────────────────────────────────
// CL051-S1: run_completed{success} present, Refs: trailer absent
// ─────────────────────────────────────────────────────────────────────────────

// TestCL051_S1_EventOnlyIsInFlight verifies that when Condition 1 is met
// (run_completed{success} observed) but Condition 2 is absent (no Refs:
// trailer on origin/main), CheckTwoPhaseDone returns DoneStatusInFlight.
//
// This is the canonical guard against premature bead closure after the daemon
// emits run_completed{success} but before the commit reaches origin/main.
//
// Spec ref: CL-051 — "Condition 1 only … Loop MUST treat as in-flight and
// re-poll Condition 2; MUST NOT advance the bead."
// Bead: hk-iht2w.
func TestCL051_S1_EventOnlyIsInFlight(t *testing.T) {
	t.Parallel()

	workDir := cl051FixtureSetup(t)
	const beadID = "hk-cl051-s1"

	// Condition 1 is met; Condition 2 is absent (no trailer commit pushed).
	result, err := cognition.CheckTwoPhaseDone(t.Context(), workDir, beadID, true)
	if err != nil {
		t.Fatalf("CL051-S1: CheckTwoPhaseDone: %v", err)
	}

	if result.Status != cognition.DoneStatusInFlight {
		t.Errorf("CL051-S1 FAIL: status = %v, want DoneStatusInFlight; "+
			"a run_completed{success} without a Refs: trailer on origin/main "+
			"MUST NOT mark the bead done (CL-051)", result.Status)
	}
	if result.BeadID != beadID {
		t.Errorf("CL051-S1: BeadID = %q, want %q", result.BeadID, beadID)
	}

	t.Logf("CL051-S1 PASS: event-only bead %q → InFlight (re-poll Condition 2)", beadID)
}

// ─────────────────────────────────────────────────────────────────────────────
// CL051-S2: Refs: trailer present, no run_completed event
// ─────────────────────────────────────────────────────────────────────────────

// TestCL051_S2_TrailerOnlyIsPhantomDone verifies that when Condition 2 is met
// (Refs: trailer present on origin/main) but Condition 1 is absent (no
// run_completed{success} event), CheckTwoPhaseDone returns DoneStatusPhantomDone.
//
// On DoneStatusPhantomDone the caller MUST emit loop_observed_phantom_done and
// route to Tier-2 reconciliation; no direct bead action is permitted.
//
// Spec ref: CL-051 — "Condition 2 only … Loop MUST emit
// loop_observed_phantom_done{bead_id} warning and route to Tier-2 … MUST NOT
// act directly."
// Bead: hk-iht2w.
func TestCL051_S2_TrailerOnlyIsPhantomDone(t *testing.T) {
	t.Parallel()

	workDir := cl051FixtureSetup(t)
	const beadID = "hk-cl051-s2"

	// Add commit with Refs: trailer to origin/main.
	cl051FixtureCommitWithRefsTrailer(t, workDir, beadID)

	// Condition 1 is absent; Condition 2 is met.
	result, err := cognition.CheckTwoPhaseDone(t.Context(), workDir, beadID, false)
	if err != nil {
		t.Fatalf("CL051-S2: CheckTwoPhaseDone: %v", err)
	}

	if result.Status != cognition.DoneStatusPhantomDone {
		t.Errorf("CL051-S2 FAIL: status = %v, want DoneStatusPhantomDone; "+
			"a Refs: trailer on origin/main without a terminal run_completed event "+
			"MUST be classified as phantom-done and routed to Tier-2 (CL-051)", result.Status)
	}
	if result.BeadID != beadID {
		t.Errorf("CL051-S2: BeadID = %q, want %q", result.BeadID, beadID)
	}

	t.Logf("CL051-S2 PASS: trailer-only bead %q → PhantomDone (emit loop_observed_phantom_done)", beadID)
}

// ─────────────────────────────────────────────────────────────────────────────
// CL051-S3: both conditions met (positive control)
// ─────────────────────────────────────────────────────────────────────────────

// TestCL051_S3_BothConditionsIsDone verifies that when both Condition 1
// (run_completed{success} event) and Condition 2 (Refs: trailer on
// origin/main) are met, CheckTwoPhaseDone returns DoneStatusDone.
//
// This is the positive control confirming the checker correctly identifies
// the fully-done state.
//
// Spec ref: CL-051 — "DONE only when BOTH … (1) run_completed{success} … AND
// (2) git log origin/main … non-empty."
// Bead: hk-iht2w.
func TestCL051_S3_BothConditionsIsDone(t *testing.T) {
	t.Parallel()

	workDir := cl051FixtureSetup(t)
	const beadID = "hk-cl051-s3"

	// Add commit with Refs: trailer to origin/main.
	cl051FixtureCommitWithRefsTrailer(t, workDir, beadID)

	// Both conditions met.
	result, err := cognition.CheckTwoPhaseDone(t.Context(), workDir, beadID, true)
	if err != nil {
		t.Fatalf("CL051-S3: CheckTwoPhaseDone: %v", err)
	}

	if result.Status != cognition.DoneStatusDone {
		t.Errorf("CL051-S3 FAIL: status = %v, want DoneStatusDone; "+
			"both run_completed{success} AND Refs: trailer on origin/main are present "+
			"— bead MUST be classified as done (CL-051)", result.Status)
	}

	t.Logf("CL051-S3 PASS: both-conditions bead %q → Done", beadID)
}

// ─────────────────────────────────────────────────────────────────────────────
// CL051-S4: neither condition met (baseline)
// ─────────────────────────────────────────────────────────────────────────────

// TestCL051_S4_NeitherConditionIsInFlight verifies that when neither condition
// is met, CheckTwoPhaseDone returns DoneStatusInFlight.
//
// A bead with no run_completed event and no git trailer is simply not yet done.
//
// Spec ref: CL-051 — baseline; bead is still in-flight until at least one
// condition is met.
// Bead: hk-iht2w.
func TestCL051_S4_NeitherConditionIsInFlight(t *testing.T) {
	t.Parallel()

	workDir := cl051FixtureSetup(t)
	const beadID = "hk-cl051-s4"

	// Neither condition met.
	result, err := cognition.CheckTwoPhaseDone(t.Context(), workDir, beadID, false)
	if err != nil {
		t.Fatalf("CL051-S4: CheckTwoPhaseDone: %v", err)
	}

	if result.Status != cognition.DoneStatusInFlight {
		t.Errorf("CL051-S4 FAIL: status = %v, want DoneStatusInFlight; "+
			"a bead with no event and no trailer must be in-flight (CL-051)", result.Status)
	}

	t.Logf("CL051-S4 PASS: neither-condition bead %q → InFlight", beadID)
}

// ─────────────────────────────────────────────────────────────────────────────
// CL051 event-type registration check
// ─────────────────────────────────────────────────────────────────────────────

// TestCL051_PhantomDoneEventTypeIsDefined verifies that the
// loop_observed_phantom_done event type constant is declared in core.EventType.
//
// The harness that emits this warning must reference the same constant.
//
// Spec ref: CL-051 — "Loop MUST emit loop_observed_phantom_done{bead_id}."
// Bead: hk-iht2w.
func TestCL051_PhantomDoneEventTypeIsDefined(t *testing.T) {
	t.Parallel()

	const want = core.EventType("loop_observed_phantom_done")
	if core.EventTypeLoopObservedPhantomDone != want {
		t.Errorf("CL051: EventTypeLoopObservedPhantomDone = %q, want %q; "+
			"the event type string must match the spec literal", core.EventTypeLoopObservedPhantomDone, want)
	}
	if !core.EventTypeLoopObservedPhantomDone.Valid() {
		t.Error("CL051: EventTypeLoopObservedPhantomDone.Valid() = false; constant must be non-empty")
	}

	t.Logf("CL051: EventTypeLoopObservedPhantomDone = %q (valid)", core.EventTypeLoopObservedPhantomDone)
}

// ─────────────────────────────────────────────────────────────────────────────
// CL051 trailer-isolation test
// ─────────────────────────────────────────────────────────────────────────────

// TestCL051_TrailerCheckIsolatedToBeadID verifies that a Refs: trailer for
// one bead does not falsely satisfy Condition 2 for a different bead.
//
// This guards against over-broad git grep patterns.
//
// Spec ref: CL-051 Condition 2 — git grep is keyed to the specific bead ID.
// Bead: hk-iht2w.
func TestCL051_TrailerCheckIsolatedToBeadID(t *testing.T) {
	t.Parallel()

	workDir := cl051FixtureSetup(t)

	const beadWithTrailer = "hk-cl051-isolation-a"
	const beadWithoutTrailer = "hk-cl051-isolation-b"

	// Push a commit with trailer only for beadWithTrailer.
	cl051FixtureCommitWithRefsTrailer(t, workDir, beadWithTrailer)

	// beadWithTrailer should be PhantomDone (has trailer, no event).
	resultA, err := cognition.CheckTwoPhaseDone(t.Context(), workDir, beadWithTrailer, false)
	if err != nil {
		t.Fatalf("CL051-isolation: CheckTwoPhaseDone(beadWithTrailer): %v", err)
	}
	if resultA.Status != cognition.DoneStatusPhantomDone {
		t.Errorf("CL051-isolation: beadWithTrailer status = %v, want PhantomDone", resultA.Status)
	}

	// beadWithoutTrailer should be InFlight — its trailer is NOT on origin/main.
	resultB, err := cognition.CheckTwoPhaseDone(t.Context(), workDir, beadWithoutTrailer, false)
	if err != nil {
		t.Fatalf("CL051-isolation: CheckTwoPhaseDone(beadWithoutTrailer): %v", err)
	}
	if resultB.Status != cognition.DoneStatusInFlight {
		t.Errorf("CL051-isolation FAIL: beadWithoutTrailer status = %v, want InFlight; "+
			"a Refs: commit for a different bead must NOT satisfy Condition 2 for this bead (CL-051 isolation)", resultB.Status)
	}

	t.Logf("CL051-isolation PASS: trailer for %q does not bleed into %q", beadWithTrailer, beadWithoutTrailer)
}
