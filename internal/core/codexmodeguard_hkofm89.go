package core

// codexmodeguard_hkofm89.go — typed payload for the codex_mode_guard event.
//
// The guard makes the codex-labelling safety boundary SELF-ENFORCING instead of
// procedural: a per-bead tier-1 harness:<captured-harness> label is safe only
// when the bead dispatches through the DOT cascade, where reviewer nodes carry
// an explicit harness pin. The daemon refuses to dispatch such a bead in any
// other resolved workflow mode.
//
// Bead: hk-ofm89.

// CodexModeGuardOutcome is the typed outcome of a codex mode-guard evaluation.
type CodexModeGuardOutcome string

const (
	// CodexModeGuardRefused records that the daemon REFUSED to dispatch the bead:
	// a captured-session harness in review-loop mode, where the reviewer has no
	// node pin available and the boundary that makes the label safe does not
	// exist.
	CodexModeGuardRefused CodexModeGuardOutcome = "refused"

	// CodexModeGuardAudited records that the bead was ALLOWED to dispatch, with
	// the situation recorded: a captured-session harness in single mode, where an
	// explicit per-bead workflow:single label opted out of review entirely.
	//
	// Not a refusal, because single mode has no reviewer to hijack and the opt-out
	// is the operator's explicit, already-audited choice (review_bypassed fires on
	// the same run). The event exists so "codex work landed unreviewed" is
	// searchable rather than silent.
	CodexModeGuardAudited CodexModeGuardOutcome = "audited"
)

// Valid reports whether o is one of the two declared outcomes.
func (o CodexModeGuardOutcome) Valid() bool {
	switch o {
	case CodexModeGuardRefused, CodexModeGuardAudited:
		return true
	default:
		return false
	}
}

// CodexModeGuardPayload is the typed event payload for the codex_mode_guard
// event type (hk-ofm89).
//
// Tags: mechanism
// Durability class: O (ordinary — observability; the refusal is also surfaced
// as the run's terminal failure reason).
//
// # Payload fields
//
//   - run_id        — the run that was refused (required, non-empty): the guard
//     fires after the run_id is minted and before any node dispatches
//   - bead_id       — the bead correlation id (required, non-empty)
//   - harness_label — the tier-1 harness:<agent-type> label that triggered the
//     guard, verbatim as it appears on the bead (required, non-empty)
//   - agent_type    — the agent type that label resolved to (required, non-empty)
//   - resolved_mode — the RESOLVED workflow mode the guard keyed on (required,
//     non-empty). This is the post-tier-1 value, so a per-bead workflow:<mode>
//     label is visible here even when the global config says dot
//   - outcome       — refused | audited (required, valid)
//   - reason        — human-readable cause (required, non-empty)
type CodexModeGuardPayload struct {
	// RunID is the run the guard refused. Required (non-empty).
	RunID string `json:"run_id"`

	// BeadID is the bead correlation id. Required (non-empty).
	BeadID string `json:"bead_id"`

	// HarnessLabel is the tier-1 harness:<agent-type> label verbatim. Required
	// (non-empty). Recorded verbatim so an operator reading the event can see
	// exactly what to remove.
	HarnessLabel string `json:"harness_label"`

	// AgentType is the agent type HarnessLabel resolved to. Required (non-empty).
	AgentType string `json:"agent_type"`

	// ResolvedMode is the resolved workflow mode. Required (non-empty). This is
	// the value the EM-012a walk returned, NOT the global config setting: the
	// per-bead workflow:<mode> label defeats the boundary for one bead alone and
	// is invisible in config review, so the resolved value is the only one worth
	// recording.
	ResolvedMode string `json:"resolved_mode"`

	// Outcome is refused (dispatch stopped) or audited (dispatch allowed, event
	// recorded). Required (must be a valid declared value).
	Outcome CodexModeGuardOutcome `json:"outcome"`

	// Reason is a human-readable description of the refusal. Required (non-empty).
	Reason string `json:"reason"`
}

// Valid reports whether p is a well-formed CodexModeGuardPayload.
//
// Every field is required: the guard emits exactly one shape, at one call site,
// with all values in hand. There is no partial-knowledge case to permit.
func (p CodexModeGuardPayload) Valid() bool {
	return p.RunID != "" &&
		p.BeadID != "" &&
		p.HarnessLabel != "" &&
		p.AgentType != "" &&
		p.ResolvedMode != "" &&
		p.Outcome.Valid() &&
		p.Reason != ""
}
