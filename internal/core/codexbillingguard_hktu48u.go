package core

// codexbillingguard_hktu48u.go — event-bus payload for the codex_billing_guard
// event type (codex-harness C3/T11, hk-tu48u).
//
// This is the POSITIVE half of the codex billing guard. The negative half
// (C3/T10, hk-jxgnp) strips OPENAI_API_KEY / CODEX_API_KEY from the codex child
// env so an inherited key cannot silently bill the API credit pool. T11 forces
// and asserts the wanted path: ChatGPT-subscription login.
//
// The launch path:
//  1. materializes forced_login_method = "chatgpt" into $CODEX_HOME/config.toml,
//  2. runs a fail-closed pre-flight assert (assertChatGPTPlan) that refuses to
//     launch codex unless the ChatGPT plan can be confirmed, and
//  3. emits this event at each observable step (materialized / allowed / denied).
//
// Spec ref: specs/event-model.md §8.3 (agent/handler lifecycle).
// Bead ref: hk-tu48u [C3/T11].

// CodexBillingGuardOutcome is the typed outcome of a codex billing-guard step.
type CodexBillingGuardOutcome string

const (
	// CodexBillingGuardMaterialized records that forced_login_method = "chatgpt"
	// was ensured in $CODEX_HOME/config.toml before the pre-flight assert ran.
	CodexBillingGuardMaterialized CodexBillingGuardOutcome = "materialized"

	// CodexBillingGuardAllowed records that the pre-flight assertChatGPTPlan
	// confirmed the ChatGPT plan; the codex launch may proceed.
	CodexBillingGuardAllowed CodexBillingGuardOutcome = "allowed"

	// CodexBillingGuardDenied records that the pre-flight assertChatGPTPlan could
	// NOT confirm the ChatGPT plan. The guard fails closed: codex is NOT launched.
	CodexBillingGuardDenied CodexBillingGuardOutcome = "denied"
)

// Valid reports whether o is one of the three declared outcomes.
func (o CodexBillingGuardOutcome) Valid() bool {
	switch o {
	case CodexBillingGuardMaterialized, CodexBillingGuardAllowed, CodexBillingGuardDenied:
		return true
	default:
		return false
	}
}

// CodexBillingGuardPayload is the typed event payload for the codex_billing_guard
// event type (hk-tu48u).
//
// Tags: mechanism
// Durability class: O (ordinary — observability; a denied launch is also surfaced
// via the buildCodexLaunchSpec error).
//
// # Payload fields
//
//   - run_id     — the run whose codex launch was guarded (optional; the spec
//     builder may run before a run_id is minted, so empty is permitted and the
//     event is then emitted run-unscoped)
//   - bead_id    — the bead correlation id (required, non-empty)
//   - codex_home — the resolved $CODEX_HOME the guard inspected (required)
//   - outcome    — materialized | allowed | denied (required, valid)
//   - reason     — human-readable detail (required, non-empty); for a denied
//     outcome this carries the fail-closed cause
type CodexBillingGuardPayload struct {
	// RunID is the run whose codex launch was guarded. Optional: the spec builder
	// may run before a run_id is minted. When empty the event is run-unscoped.
	RunID string `json:"run_id,omitempty"`

	// BeadID is the bead correlation id. Required (non-empty).
	BeadID string `json:"bead_id"`

	// CodexHome is the resolved $CODEX_HOME directory the guard materialized into
	// and asserted against. Required (non-empty).
	CodexHome string `json:"codex_home"`

	// Outcome is the guard step outcome. Required (must be a valid declared value).
	Outcome CodexBillingGuardOutcome `json:"outcome"`

	// Reason is a human-readable description of the outcome. Required (non-empty).
	// For a denied outcome it carries the fail-closed cause (e.g. "config.toml
	// missing forced_login_method=chatgpt", "auth.json carries a populated
	// OPENAI_API_KEY (API-pool billing)").
	Reason string `json:"reason"`
}

// Valid reports whether p is a well-formed CodexBillingGuardPayload.
//
// Rules:
//   - BeadID must be non-empty.
//   - CodexHome must be non-empty.
//   - Outcome must be one of the declared CodexBillingGuardOutcome values.
//   - Reason must be non-empty.
//
// RunID is intentionally NOT required: the codex launch-spec builder can run
// before a run_id is minted, in which case the event is emitted run-unscoped.
func (p CodexBillingGuardPayload) Valid() bool {
	if p.BeadID == "" {
		return false
	}
	if p.CodexHome == "" {
		return false
	}
	if !p.Outcome.Valid() {
		return false
	}
	if p.Reason == "" {
		return false
	}
	return true
}
