package core

// pibillingguard.go — event-bus payload for the pi_billing_guard event type
// (codename:pilot, PI-040/041/042/043, hk-l1bkp).
//
// Pi's billing guard is the INVERSE of the codex guard: codex forces a specific
// billing method and refuses if an API key is present (don't bill the API pool);
// Pi refuses if the configured provider's key is ABSENT (fail closed without a key).
//
// Two steps per launch:
//  1. Pre-flight assert (PI-040): env var named by api_key_env must be present +
//     non-empty; absent/empty -> typed error, refuse launch BEFORE agent_ready.
//  2. On-disk credential check (PI-042): until Pi's no-persist behavior is
//     confirmed (findings.md §4), check for a persisted auth file mirroring
//     codex's authIndicatesAPIKeyLogin.
//
// Events name the env-var NAME, never its value (PI-040 / ps-argv leak prevention).
//
// Spec ref: specs/pi-harness.md §4 (PI-040/PI-041/PI-042/PI-043).
// Design: ~/.kerf/projects/gregberns-harmonik/pilot/04-design/pi-harness-design.md §3.6.
// Bead ref: hk-l1bkp.

// PiBillingGuardOutcome is the typed outcome of a Pi billing-guard step.
//
// Pi's guard has two outcomes (no "materialized" intermediate: there is nothing
// to materialize — the guard only asserts presence). Codex's guard has three
// because it materializes forced_login_method before asserting; Pi has no
// equivalent materialization step.
type PiBillingGuardOutcome string

const (
	// PiBillingGuardAllowed records that the pre-flight assert confirmed the
	// selected provider's env var is present and non-empty, and no persistent
	// on-disk credential was detected; the Pi launch may proceed.
	PiBillingGuardAllowed PiBillingGuardOutcome = "allowed"

	// PiBillingGuardDenied records that the pre-flight assert failed: either
	// the selected provider's env var is absent/empty, or the on-disk credential
	// check detected a potentially-persisted credential. The guard fails closed:
	// Pi is NOT launched.
	PiBillingGuardDenied PiBillingGuardOutcome = "denied"
)

// Valid reports whether o is one of the two declared outcomes.
func (o PiBillingGuardOutcome) Valid() bool {
	switch o {
	case PiBillingGuardAllowed, PiBillingGuardDenied:
		return true
	default:
		return false
	}
}

// PiBillingGuardPayload is the typed event payload for the pi_billing_guard
// event type (codename:pilot, PI-040/041/042/043, hk-l1bkp).
//
// Tags: mechanism
// Durability class: O (ordinary — observability; a denied launch is also surfaced
// via the buildPiLaunchSpec error to the caller).
//
// # Payload fields
//
//   - run_id      — the run whose Pi launch was guarded (optional; the spec
//     builder may run before a run_id is minted, so empty is permitted and the
//     event is then emitted run-unscoped)
//   - bead_id     — the bead correlation id (required, non-empty)
//   - api_key_env — the NAME of the env var carrying the provider key (required;
//     NEVER the value — PI-040 ps/argv leak prevention)
//   - outcome     — allowed | denied (required, valid)
//   - reason      — human-readable detail (required, non-empty); for a denied
//     outcome this carries the fail-closed cause
type PiBillingGuardPayload struct {
	// RunID is the run whose Pi launch was guarded. Optional: the spec builder
	// may run before a run_id is minted. When empty the event is run-unscoped.
	RunID string `json:"run_id,omitempty"`

	// BeadID is the bead correlation id. Required (non-empty).
	BeadID string `json:"bead_id"`

	// EnvVarName is the NAME of the env var carrying the selected provider key.
	// Required (non-empty). NEVER the key VALUE — PI-040 (ps/argv leak).
	// Named EnvVarName (not APIKeyEnv) to satisfy EV-036 secret-prefix rule
	// (hk-6x7dw): "api[_-]?key" matches the scanner; the bare env-var-name
	// label does not.
	EnvVarName string `json:"env_var_name"`

	// Outcome is the guard step outcome. Required (must be a valid declared value).
	Outcome PiBillingGuardOutcome `json:"outcome"`

	// Reason is a human-readable description of the outcome. Required (non-empty).
	// For a denied outcome it carries the fail-closed cause (e.g. "env var
	// OPENROUTER_API_KEY is absent or empty").
	Reason string `json:"reason"`
}

// Valid reports whether p is a well-formed PiBillingGuardPayload.
//
// Rules:
//   - BeadID must be non-empty.
//   - EnvVarName must be non-empty.
//   - Outcome must be one of the declared PiBillingGuardOutcome values.
//   - Reason must be non-empty.
//
// RunID is intentionally NOT required: the Pi launch-spec builder can run before
// a run_id is minted, in which case the event is emitted run-unscoped.
func (p PiBillingGuardPayload) Valid() bool {
	if p.BeadID == "" {
		return false
	}
	if p.EnvVarName == "" {
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
