package dispatch

import (
	"path/filepath"
	"time"

	"github.com/google/uuid"
)

// WorktreeObservation is the value-only result of one workspace discovery.
type WorktreeObservation struct {
	RunID          string
	Path           string
	Registered     bool
	LeasePresent   bool
	LeaseReadable  bool
	LeaseRunID     string
	LeasePID       int
	LeaseCreatedAt string
	LeaseTTLSec    int
}

// ClassifyWorktreeObservations validates worktree authority for one intent.
func ClassifyWorktreeObservations(intent Intent, observations []WorktreeObservation) WorktreeFact {
	if intent.Validate() != nil {
		return WorktreeConflict
	}
	wantRunID := intent.Binding.RunID.String()
	var match *WorktreeObservation
	for index := range observations {
		candidate := &observations[index]
		if candidate.RunID != wantRunID {
			if candidate.LeasePresent && candidate.LeaseReadable && candidate.LeaseRunID == wantRunID {
				return WorktreeConflict
			}
			continue
		}
		if match != nil {
			return WorktreeConflict
		}
		match = candidate
	}
	if match == nil {
		return WorktreeAbsent
	}
	if !validWorktreeObservation(*match, wantRunID) {
		return WorktreeConflict
	}
	return WorktreeLeased
}

func validWorktreeObservation(observation WorktreeObservation, wantRunID string) bool {
	if observation.Path == "" || filepath.Base(observation.Path) != wantRunID || !observation.Registered ||
		!observation.LeasePresent || !observation.LeaseReadable {
		return false
	}
	runID, err := uuid.Parse(observation.LeaseRunID)
	if err != nil || runID.Version() != 7 || runID.String() != observation.LeaseRunID || observation.LeaseRunID != wantRunID {
		return false
	}
	createdAt, err := time.Parse(time.RFC3339, observation.LeaseCreatedAt)
	return err == nil && !createdAt.IsZero() && observation.LeasePID > 0 && observation.LeaseTTLSec > 0
}
