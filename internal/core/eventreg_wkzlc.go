package core

// eventreg_wkzlc.go — startup-time registration of the §8.12 run_stale event
// payload type into the global event registry per EV-032 / EV-034.
//
// Spec ref: specs/event-model.md §6.3 EV-032, §4.9 EV-034.
// Bead ref: hk-wkzlc.

func init() {
	registerStalenessEvents()
}

// registerStalenessEvents registers all §8.12 staleness-detection event payload
// constructors (hk-wkzlc).
//
// Durability classes per §8.12 table:
//   - run_stale (§8.12.1): O (ordinary — observational; orchestrator decides action)
func registerStalenessEvents() {
	mustRegister("run_stale", func() EventPayload { return &RunStalePayload{} })
}

// RunStalePayload is the event-bus payload for the run_stale event (§8.12.1).
//
// Emitted by the daemon stale-watch goroutine when an active run has produced
// no event of any kind for M minutes (default 10, configurable per-queue via
// stale_after_seconds in queue JSON, overridable per-bead via stale_after
// label). Re-emitted at 2M, 4M, … (exponential backoff) until the run
// terminates. The orchestrator's Monitor grep matches this like any other event
// and decides whether to kill the run.
//
// # Payload fields (§8.12.1)
//
//   - run_id:               the stale run (required)
//   - bead_id:              the bead being executed (required)
//   - age_seconds:          seconds since the run produced any event (required, >0)
//   - last_event_type:      EventType of the most recent event seen, or "" if none
//   - last_event_at:        RFC 3339 timestamp of the most recent event, or "" if none
//   - emit_count:           1-based count of run_stale emissions for this run (≥1)
//   - snapshot:             lightweight daemon health snapshot at emission time
//   - owning_epic_id:       bead ID of the parent epic (optional; logmine F13 / hk-7evda)
//   - owning_epic_assignee: crew name assigned to the parent epic (optional; logmine F13 / hk-7evda)
type RunStalePayload struct {
	// RunID identifies the stale run. Required (non-empty string).
	RunID string `json:"run_id"`

	// BeadID is the bead being executed in this run. Required (non-empty string).
	BeadID string `json:"bead_id"`

	// AgeSeconds is the wall-clock seconds elapsed since the run last produced
	// any bus event. Always positive; rounded to the nearest second.
	AgeSeconds int64 `json:"age_seconds"`

	// LastEventType is the EventType string of the most recent event received
	// from this run. Empty when no event has been seen yet (e.g. the run was
	// claimed but never emitted run_started — unusual but possible during race
	// between claim and first emit).
	LastEventType string `json:"last_event_type,omitempty"`

	// LastEventAt is the RFC 3339 wall-clock timestamp of the most recent event.
	// Empty under the same condition as LastEventType.
	LastEventAt string `json:"last_event_at,omitempty"`

	// EmitCount is the 1-based count of run_stale emissions for this particular
	// run. 1 = first stale alert (at M minutes), 2 = second (at 2M), 3 = third
	// (at 4M), etc. Allows consumers to distinguish first alert from repeated
	// warnings.
	EmitCount int `json:"emit_count"`

	// Snapshot holds a lightweight daemon health snapshot captured at emission
	// time. Nil when the snapshot could not be populated (e.g. registry not
	// available in test contexts).
	Snapshot *RunStaleSnapshot `json:"snapshot,omitempty"`

	// OwningEpicID is the bead ID of the parent epic for the stale bead.
	// Nil when the bead has no parent epic. Denormalized from RunHandle to
	// eliminate captain br round-trips for attribution (logmine F13 / hk-7evda).
	OwningEpicID *string `json:"owning_epic_id,omitempty"`

	// OwningEpicAssignee is the crew name assigned to the parent epic.
	// Nil when OwningEpicID is nil or the epic has no assignee. Mirrors the
	// captain's `br update <epic> --assignee <crew>` durable marker.
	OwningEpicAssignee *string `json:"owning_epic_assignee,omitempty"`
}

// RunStaleSnapshot is the embedded health snapshot in RunStalePayload.
type RunStaleSnapshot struct {
	// ActiveRunCount is the total number of runs currently registered in the
	// daemon's RunRegistry at emission time.
	ActiveRunCount int `json:"active_run_count"`

	// GoroutineCount is the runtime.NumGoroutine() value at emission time.
	// Useful for detecting goroutine leaks when correlated across events.
	GoroutineCount int `json:"goroutine_count"`

	// LifecycleState is the LifecycleState.String() label of the session's FSM
	// at emission time (e.g. "Ready", "Executing", "Failed"). Empty when the
	// lifecycle Machine is not yet available (handler has not launched).
	// Populated by the stale watcher from handle.GetMachine() per hk-xrygh.
	LifecycleState string `json:"lifecycle_state,omitempty"`

	// LifecycleEnteredAt is the RFC 3339 wall-clock timestamp at which the
	// session entered its current LifecycleState. Empty when LifecycleState is
	// empty. Provides a wall-clock reference for "how long has the session been
	// stuck in this state?" at run_stale emission time.
	LifecycleEnteredAt string `json:"lifecycle_entered_at,omitempty"`

	// WorktreeCommitSHA is the HEAD commit SHA in the run's worktree at
	// run_stale emission time.  Non-empty when the worktree has at least one
	// commit (i.e. the implementer made progress even if no lifecycle events
	// were recorded).  Empty when the worktree path is unknown, the worktree
	// has no commits, or the git probe fails.
	// Added by hk-fra5l for orphan-commit visibility (acceptance criterion 3).
	WorktreeCommitSHA string `json:"worktree_commit_sha,omitempty"`
}

// Valid reports whether p is a well-formed RunStalePayload.
//
// Rules per §8.12.1:
//   - RunID must be non-empty.
//   - BeadID must be non-empty.
//   - AgeSeconds must be > 0.
//   - EmitCount must be ≥ 1.
//   - LastEventType and LastEventAt must either both be empty or both be non-empty.
func (p RunStalePayload) Valid() bool {
	if p.RunID == "" {
		return false
	}
	if p.BeadID == "" {
		return false
	}
	if p.AgeSeconds <= 0 {
		return false
	}
	if p.EmitCount < 1 {
		return false
	}
	// LastEventType and LastEventAt must be either both set or both empty.
	if (p.LastEventType == "") != (p.LastEventAt == "") {
		return false
	}
	return true
}
