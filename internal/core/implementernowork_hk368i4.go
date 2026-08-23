package core

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
