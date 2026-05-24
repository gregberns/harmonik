package core

// implementerphasecomplete_hkcd8yu.go — ImplementerPhaseCompletePayload (hk-cd8yu).
//
// Emitted by the daemon's workloop and reviewloop immediately after the
// implementer session ends, regardless of how it ended (normal exit,
// noChange-timeout kill, or context cancellation). This closes the
// diagnostic gap between run_started and reviewer_launched where silent
// implementer failures previously produced no structured event.
//
// The event fires in both single-mode (workloop.go) and review-loop mode
// (reviewloop.go) at the point where waitWithSocketGrace returns for the
// implementer phase.
//
// Durability class: F (terminal-state landmark — emitted once per implementer
// session; loss would hide the exit cause from JSONL-only diagnosis).
//
// Bead ref: hk-cd8yu.

import (
	"github.com/google/uuid"
)

// ImplementerPhaseCompletePayload is the typed event payload for the
// implementer_phase_complete event (hk-cd8yu).
//
// Fields:
//   - RunID:           the run whose implementer session ended (required).
//   - ExitCode:        process exit code (0 = clean; non-zero = error; -1 = kill).
//   - StderrTailHead:  first 200 bytes of the stderr tail captured by
//     waitWithSocketGrace; empty when stderr was not captured (substrate sessions).
//   - CommitLanded:    true when the worktree HEAD advanced past the parent SHA
//     before this event was emitted.
//   - DurationSeconds: wall-clock seconds from implementer launch to session end.
type ImplementerPhaseCompletePayload struct {
	// RunID identifies the run whose implementer phase ended. Required.
	RunID RunID `json:"run_id"`

	// ExitCode is the process exit code from the implementer subprocess.
	// 0 = clean exit, non-zero = error exit, -1 = killed by daemon.
	ExitCode int `json:"exit_code"`

	// StderrTailHead is the first 200 bytes of the stderr tail captured from
	// the implementer subprocess by waitWithSocketGrace. Empty for substrate
	// (tmux) sessions where stderr is not captured separately.
	StderrTailHead string `json:"stderr_tail_head"`

	// CommitLanded reports whether the worktree HEAD advanced past the parent
	// SHA before this event was emitted (i.e., the implementer produced a commit).
	CommitLanded bool `json:"commit_landed"`

	// DurationSeconds is the wall-clock duration in seconds from implementer
	// launch to session end.
	DurationSeconds float64 `json:"duration_seconds"`
}

// Valid reports whether p is a well-formed ImplementerPhaseCompletePayload.
func (p ImplementerPhaseCompletePayload) Valid() bool {
	if uuid.UUID(p.RunID) == uuid.Nil {
		return false
	}
	return true
}
