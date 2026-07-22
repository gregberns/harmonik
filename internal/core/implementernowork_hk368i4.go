package core

// implementernowork_hk368i4.go — ImplementerNoWorkSuspectedPayload (hk-368i4).
//
// Emitted by the daemon when a process-exit implementer (codex) finishes with
// NO commit and a CLEAN worktree (the codexRefsNoChange outcome) AND its phase
// ran for less than the no-work duration floor.
//
// The event is a DETECTOR, not a gate: the run is already failing through the
// standard no-commit guard by the time this fires. It exists because the
// hk-jcrzn silent-implementer failure was invisible at every layer that was
// checked — a fallback committer read the daemon's own scaffolding as agent
// work and manufactured a passing commit. hk-jcrzn fixes that cause; this names
// the SHAPE so a future silent failure of a different cause is still caught.
//
// Why the conjunction rather than duration alone: a legitimately trivial bead
// could in principle finish fast, and a slow run can still fail to commit.
// Neither signal is safe on its own. Pairing them is what makes the detector
// safe to emit loudly, because a real fast run still produces a commit and so
// never reaches codexRefsNoChange.
//
// Durability class: O (observability — loss costs a diagnostic, not correctness;
// the run's failure is recorded independently by the no-commit path).
//
// Bead ref: hk-368i4 (split from hk-jcrzn).

// ImplementerNoWorkSuspectedPayload is the typed event payload for the
// implementer_no_work_suspected event (hk-368i4).
//
// # Payload fields
//
//   - run_id           — the run whose implementer did no work (required, non-empty)
//   - bead_id          — the bead being implemented (required, non-empty)
//   - duration_seconds — wall-clock seconds from implementer launch to session end
//   - floor_seconds    — the no-work duration floor in force for this run, so a
//     reader can tell a threshold change from a behaviour change
type ImplementerNoWorkSuspectedPayload struct {
	// RunID is the run whose implementer phase produced no work. Required.
	RunID string `json:"run_id"`

	// BeadID is the bead the implementer was working. Required.
	BeadID string `json:"bead_id"`

	// DurationSeconds is the wall-clock duration from implementer launch to
	// session end. Below FloorSeconds by construction whenever this event fires.
	DurationSeconds float64 `json:"duration_seconds"`

	// FloorSeconds is the no-work duration floor that was in force. Recorded so
	// a reader can distinguish a re-tuned threshold from a behaviour change.
	FloorSeconds float64 `json:"floor_seconds"`
}

// Valid reports whether p is a well-formed ImplementerNoWorkSuspectedPayload.
func (p ImplementerNoWorkSuspectedPayload) Valid() bool {
	return p.RunID != "" && p.BeadID != "" && p.FloorSeconds > 0
}
