// Package goalstate manages the durable operator intent file at
// .harmonik/intent/goal-state.json (flywheel V6, hk-owz1).
//
// The goal-state captures objectives, antigoals, verbatim operator directives
// (guidance, not law), and a last_event_id cursor so the goal-keeper can
// resume from where it left off without re-reading all comms history.
package goalstate

import (
	"path/filepath"
)

// SchemaVersion is the current schema version for GoalState.
const SchemaVersion = 1

// GoalState is the durable operator intent document persisted at
// .harmonik/intent/goal-state.json.
//
// Schema v1 contract:
//   - objectives: ordered list of free-text goals the captain should pursue.
//   - antigoals: things the captain must NOT do; checked before dispatching.
//   - operator_directives: verbatim operator messages extracted by the goal-
//     keeper from comms log. These are guidance, NOT standing law. The captain
//     may override them when get-shit-done protocol applies. Bounded to the
//     most recent MaxDirectives entries to keep the file compact.
//   - last_event_id: UUIDv7 cursor into events.jsonl. The goal-keeper reads
//     only messages after this event so runs are incremental and idempotent.
type GoalState struct {
	SchemaVersion      int      `json:"schema_version"`
	Objectives         []string `json:"objectives"`
	Antigoals          []string `json:"antigoals"`
	OperatorDirectives []string `json:"operator_directives"`
	// LastEventID is the event_id of the last operator comms message consumed
	// by the goal-keeper. Empty means the goal-keeper has never run (first run
	// reads all operator comms history).
	LastEventID string `json:"last_event_id,omitempty"`
}

// MaxDirectives is the maximum number of operator directives retained in the
// goal-state file. Older entries are pruned by the goal-keeper when this
// limit is exceeded to keep the file bounded and the captain context compact.
const MaxDirectives = 20

// Path returns the canonical path to goal-state.json for the given project
// directory.
func Path(projectDir string) string {
	return filepath.Join(projectDir, ".harmonik", "intent", "goal-state.json")
}
