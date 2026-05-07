// Package core holds shared types that cross subsystem boundaries.
// internal/core imports nothing from internal/* subsystems.
package core

import (
	"time"

	"github.com/google/uuid"
)

// State records the position and accumulated context of a Run at a node
// (execution-model.md §6.1).
type State struct {
	// StateID uniquely identifies this run-state.
	StateID StateID

	// RunID is the run this state belongs to.
	RunID RunID

	// NodeID is the node the run is positioned at.
	// Under sub-workflow expansion the value is namespaced as
	// <parent_node_id>/<sub_node_id> per EM-034a; this is a runtime concern —
	// the struct stores the resolved (possibly namespaced) value as-is.
	NodeID NodeID

	// EnteredAt is the RFC 3339 wall clock at the moment the run entered this state.
	EnteredAt time.Time

	// TransitionHistory is the commit range on the task branch filtered by the
	// run's Harmonik-Run-ID trailer.
	TransitionHistory CommitRange
}

// Valid reports whether all five fields carry non-zero values.
// A State is considered valid iff:
//   - StateID is not the zero UUID
//   - RunID is not the zero UUID
//   - NodeID is not empty
//   - EnteredAt is not the zero Time
//   - TransitionHistory.FirstCommitSHA and LastCommitSHA are both non-empty
func (s State) Valid() bool {
	return uuid.UUID(s.StateID) != uuid.Nil &&
		uuid.UUID(s.RunID) != uuid.Nil &&
		s.NodeID != "" &&
		!s.EnteredAt.IsZero() &&
		s.TransitionHistory.FirstCommitSHA != "" &&
		s.TransitionHistory.LastCommitSHA != ""
}
