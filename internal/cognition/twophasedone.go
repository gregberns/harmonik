// Package cognition provides the deterministic harness layer of the cognition
// loop (specs/cognition-loop.md).  This package is mechanism-tagged per
// CL-013/CL-INV-001: every export here is pure-code, no model calls.
//
// Current coverage:
//   - Two-phase done checker (CL-051).
package cognition

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// DoneStatus classifies the result of CheckTwoPhaseDone.
//
// Spec ref: specs/cognition-loop.md §4.7 CL-051.
type DoneStatus int

const (
	// DoneStatusInFlight means only Condition 1 is met (run_completed{success}
	// observed) but the Refs: trailer is absent on origin/main.  The daemon's
	// terminal multi-step window (push may have failed after the merge) is still
	// open.  Harness MUST re-poll Condition 2; MUST NOT advance the bead.
	//
	// Spec ref: CL-051 — "Condition 1 only … Loop MUST treat as in-flight."
	DoneStatusInFlight DoneStatus = iota

	// DoneStatusDone means both Condition 1 and Condition 2 are satisfied: the
	// bead has a run_completed{success} event AND the Refs: trailer is present
	// on origin/main.  Harness MAY close the bead.
	//
	// Spec ref: CL-051 — "DONE only when BOTH …"
	DoneStatusDone

	// DoneStatusPhantomDone means only Condition 2 is met (Refs: trailer present
	// on origin/main) but no terminal run_completed{success} event was observed.
	// The harness MUST emit loop_observed_phantom_done and route to Tier-2
	// reconciliation; MUST NOT act directly.
	//
	// Spec ref: CL-051 — "Condition 2 only … Loop MUST emit
	// loop_observed_phantom_done{bead_id} warning and route to Tier-2."
	DoneStatusPhantomDone
)

// DoneCheckResult is the output of CheckTwoPhaseDone.
//
// Spec ref: specs/cognition-loop.md §4.7 CL-051.
type DoneCheckResult struct {
	BeadID string
	Status DoneStatus
}

// CheckTwoPhaseDone implements the CL-051 two-phase done check for bead beadID.
//
// Parameters:
//   - gitDir: path to the git repo to check (used for the git-log Condition 2 probe).
//   - beadID: the bead identifier string (e.g. "hk-XYZ").
//   - hasRunCompletedEvent: true when a run_completed{success} event for this bead
//     has been observed in events.jsonl (Condition 1).
//
// The function is mechanism-tagged: it runs git deterministically; it does not
// consult the model.
//
// Spec ref: specs/cognition-loop.md §4.7 CL-051.
func CheckTwoPhaseDone(ctx context.Context, gitDir, beadID string, hasRunCompletedEvent bool) (DoneCheckResult, error) {
	hasTrailer, err := checkGitTrailerOnOriginMain(ctx, gitDir, beadID)
	if err != nil {
		return DoneCheckResult{}, fmt.Errorf("cognition: two-phase done git check for %s: %w", beadID, err)
	}
	return classifyDoneStatus(beadID, hasRunCompletedEvent, hasTrailer), nil
}

// classifyDoneStatus applies the CL-051 routing table to the two boolean
// inputs and returns the appropriate DoneStatus.
//
// Spec ref: CL-051 routing table:
//
//	Condition 1 (event) + Condition 2 (trailer) → DoneStatusDone
//	Condition 1 only                             → DoneStatusInFlight
//	Condition 2 only                             → DoneStatusPhantomDone
//	Neither                                      → DoneStatusInFlight
func classifyDoneStatus(beadID string, hasEvent, hasTrailer bool) DoneCheckResult {
	switch {
	case hasEvent && hasTrailer:
		return DoneCheckResult{BeadID: beadID, Status: DoneStatusDone}
	case hasEvent && !hasTrailer:
		return DoneCheckResult{BeadID: beadID, Status: DoneStatusInFlight}
	case !hasEvent && hasTrailer:
		return DoneCheckResult{BeadID: beadID, Status: DoneStatusPhantomDone}
	default:
		return DoneCheckResult{BeadID: beadID, Status: DoneStatusInFlight}
	}
}

// checkGitTrailerOnOriginMain checks whether a commit on origin/main carries
// the "Refs: <beadID>" trailer.
//
// Returns (false, nil) when origin/main is absent (git exits 128) — the branch
// may not yet exist on the remote, which is not an error condition.
//
// Spec ref: CL-051 Condition 2 — "git log origin/main --grep 'Refs: <bead-id>'
// --max-count=1 non-empty."
func checkGitTrailerOnOriginMain(ctx context.Context, gitDir, beadID string) (bool, error) {
	grep := "Refs: " + beadID
	//nolint:gosec // G204: beadID is an internal identifier validated by br; gitDir is a controlled path.
	cmd := exec.CommandContext(ctx, "git", "-C", gitDir, "log", "origin/main",
		"--grep", grep, "--max-count=1", "--format=%H")
	out, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 128 {
			// origin/main does not exist — treat as no trailer.
			return false, nil
		}
		return false, fmt.Errorf("git log origin/main --grep %q: %w", grep, err)
	}
	return strings.TrimSpace(string(out)) != "", nil
}
