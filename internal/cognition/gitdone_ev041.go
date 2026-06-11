package cognition

// gitdone_ev041.go — EV-041 git-done-but-no-terminal-event heuristic.
//
// Subscribe consumers track bead_ids in consecutive heartbeat active_runs
// arrays. When a bead_id that was previously in active_runs disappears for K
// consecutive heartbeats without a terminal event, the consumer SHOULD
// git-check whether a commit carrying that run's Harmonik-Run-ID trailer
// exists anywhere in the git history.
//
// A merged commit without a terminal event indicates the daemon crashed
// mid-terminal-emission; git completion is treated as authoritative per
// EV-INV-001, and the consumer synthesizes a Tier-1 reaction (advance kerf
// baseline, close stale bead) without waiting for an event that will never arrive.
//
// Note: heartbeat active_runs carries bead_id + age_seconds ONLY (EV-039);
// run_id is absent. Consumers requiring run_id for the git check MUST read
// queue.json to correlate bead_id → run_id before calling CheckGitForRunID.
//
// Spec ref: specs/event-model.md §4.11 EV-041.
// Bead: hk-p1uz5.

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// HeartbeatActiveRun is one entry from a subscribe heartbeat's active_runs array.
//
// It mirrors the daemon's on-wire JSON shape (bead_id + age_seconds). run_id is
// absent — EV-039 notes that run-level correlation requires reading queue.json.
type HeartbeatActiveRun struct {
	BeadID     string
	AgeSeconds int
}

// GitDoneHeuristicTracker implements the EV-041 observe-and-check pattern.
//
// The tracker is NOT safe for concurrent use; callers must synchronize externally
// when sharing across goroutines.
//
// Typical usage:
//
//	tr := cognition.NewGitDoneHeuristicTracker(2) // K=2 per spec suggestion
//	// On each terminal event:
//	tr.MarkTerminal(beadID)
//	// On each heartbeat:
//	triggers := tr.ProcessHeartbeat(runs)
//	// For each bead_id in triggers, look up run_id from queue.json, then:
//	//   found, err := cognition.CheckGitForRunID(ctx, gitDir, runID)
//
// Spec ref: specs/event-model.md §4.11 EV-041.
type GitDoneHeuristicTracker struct {
	// k is the consecutive-absent heartbeat threshold that triggers the git check.
	// Always ≥ 1; normalized from the K parameter in NewGitDoneHeuristicTracker.
	k int

	// seenBeads is the set of bead_ids ever observed in any active_runs array.
	// Once a bead is seen, it stays here so its consecutive absence can be tracked
	// across subsequent heartbeats.
	seenBeads map[string]struct{}

	// absenceCount maps bead_id → count of consecutive heartbeats where the bead
	// was absent from active_runs (while seenBeads contains the bead_id).
	// Reset to 0 when the bead re-appears in active_runs.
	absenceCount map[string]int

	// terminalIDs holds bead_ids for which MarkTerminal has been called.
	// Bead_ids in this set are never returned as git-check triggers.
	terminalIDs map[string]struct{}
}

// NewGitDoneHeuristicTracker creates a tracker with the given K threshold.
// K ≤ 0 is normalized to 2 (the spec-suggested default).
//
// Spec ref: specs/event-model.md §4.11 EV-041 — "suggested K=2."
func NewGitDoneHeuristicTracker(k int) *GitDoneHeuristicTracker {
	if k <= 0 {
		k = 2
	}
	return &GitDoneHeuristicTracker{
		k:            k,
		seenBeads:    make(map[string]struct{}),
		absenceCount: make(map[string]int),
		terminalIDs:  make(map[string]struct{}),
	}
}

// ProcessHeartbeat updates tracking state with a new heartbeat's active_runs.
//
// It returns the bead_ids that should trigger the EV-041 git-check: bead_ids
// that were previously in active_runs but have been absent for K consecutive
// heartbeats without a terminal event.
//
// The returned slice may be nil when no triggers are ready. Order is not
// guaranteed. Safe to call with nil or empty runs (treats as empty active_runs).
//
// Spec ref: specs/event-model.md §4.11 EV-041.
func (tr *GitDoneHeuristicTracker) ProcessHeartbeat(runs []HeartbeatActiveRun) []string {
	currentSet := make(map[string]struct{}, len(runs))
	for _, r := range runs {
		if r.BeadID != "" {
			currentSet[r.BeadID] = struct{}{}
			tr.seenBeads[r.BeadID] = struct{}{}
		}
	}

	// Reset absence count for bead_ids that appear in this heartbeat.
	for beadID := range currentSet {
		delete(tr.absenceCount, beadID)
	}

	// For each bead_id ever seen in active_runs that is now absent, increment its
	// consecutive absence count. When the count reaches K and no terminal event
	// was recorded, yield a trigger.
	var triggers []string
	for beadID := range tr.seenBeads {
		if _, inCurrent := currentSet[beadID]; inCurrent {
			continue // still active — no absence to count
		}
		if _, hasTerminal := tr.terminalIDs[beadID]; hasTerminal {
			continue // terminal event already observed — heuristic not needed
		}
		tr.absenceCount[beadID]++
		if tr.absenceCount[beadID] >= tr.k {
			triggers = append(triggers, beadID)
		}
	}

	return triggers
}

// MarkTerminal records that a terminal event (run_completed or run_failed) has
// been observed for beadID. After this call ProcessHeartbeat will never return
// beadID as a git-check trigger, even if the bead later disappears from
// active_runs.
func (tr *GitDoneHeuristicTracker) MarkTerminal(beadID string) {
	if beadID == "" {
		return
	}
	tr.terminalIDs[beadID] = struct{}{}
	delete(tr.absenceCount, beadID)
}

// CheckGitForRunID checks whether any commit reachable via --all in the git
// repository at gitDir carries the "Harmonik-Run-ID: <runID>" trailer.
//
// It runs:
//
//	git -C gitDir log --all --grep "Harmonik-Run-ID: <runID>" --max-count=1 --format=%H
//
// The --all flag reaches commits on merged branches, which is the authoritative
// source of truth per EV-INV-001.
//
// Returns (true, nil) when a matching commit is found.
// Returns (false, nil) when no commit is found or the git repo does not exist
// (git exits 128).
// Returns (false, err) on other git execution errors.
//
// Spec ref: specs/event-model.md §4.11 EV-041.
func CheckGitForRunID(ctx context.Context, gitDir, runID string) (bool, error) {
	grep := "Harmonik-Run-ID: " + runID
	//nolint:gosec // G204: runID is a daemon-minted UUIDv7; gitDir is a controlled path.
	cmd := exec.CommandContext(ctx, "git", "-C", gitDir, "log", "--all",
		"--grep", grep, "--max-count=1", "--format=%H")
	out, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 128 {
			// git repo absent or ref resolution error — treat as not found.
			return false, nil
		}
		return false, fmt.Errorf("cognition: git log --all --grep %q: %w", grep, err)
	}
	return strings.TrimSpace(string(out)) != "", nil
}
