package core

import "github.com/google/uuid"

// cpevents_hqwn59.go — event-bus payload types for §8.2.9-§8.2.12 control-point
// registration and evaluation lifecycle events:
//   - control_points_registered            (§8.2.9)
//   - control_points_registration_started  (§8.2.10)
//   - verdict_envelope_mismatch            (§8.2.11)
//   - policy_expression_exceeded_cost      (§8.2.12)
//
// Spec ref: specs/event-model.md §8.2.9-§8.2.12.
// Bead refs: hk-hqwn.59.20, hk-hqwn.59.79, hk-hqwn.59.80, hk-hqwn.59.81.

// ControlPointsRegisteredPayload is the typed event payload for the
// control_points_registered event (event-model.md §8.2.9).
//
// Tags: mechanism
// Axes: llm-freedom=none; io-determinism=best-effort; replay-safety=safe; idempotency=non-idempotent
// Durability class: O (ordinary — observability and audit of the startup
// registration batch; the authoritative registration state is held by the
// control-point registry in memory).
//
// Emitted by the control-points subsystem (S02) when a registration batch
// completes. Paired with control_points_registration_started (§8.2.10); the
// two events bracket the registration batch per control-points.md §7.1.
//
// # Payload fields (event-model.md §8.2.9)
//
//   - count       — number of control points registered in this batch
//   - started_at  — RFC 3339 wall-clock timestamp at batch start (matches
//     control_points_registration_started.started_at)
type ControlPointsRegisteredPayload struct {
	// Count is the number of control points registered in this batch.
	// Required (must be >= 0; zero is valid for an empty batch).
	Count int `json:"count"`

	// StartedAt is the RFC 3339 wall-clock timestamp at which the registration
	// batch started. Required (non-empty). Matches the started_at field on the
	// paired control_points_registration_started event (§8.2.10).
	StartedAt string `json:"started_at"`
}

// Valid reports whether p is a well-formed ControlPointsRegisteredPayload.
//
// Rules per event-model.md §8.2.9:
//   - Count must be >= 0.
//   - StartedAt must be non-empty.
func (p ControlPointsRegisteredPayload) Valid() bool {
	if p.Count < 0 {
		return false
	}
	if p.StartedAt == "" {
		return false
	}
	return true
}

// ControlPointsRegistrationStartedPayload is the typed event payload for the
// control_points_registration_started event (event-model.md §8.2.10).
//
// Tags: mechanism
// Axes: llm-freedom=none; io-determinism=best-effort; replay-safety=safe; idempotency=non-idempotent
// Durability class: O (ordinary — observability and audit; presence of this
// event without a paired control_points_registered for the same batch_id signals
// a crashed-mid-registration batch per control-points.md §7.1).
//
// Emitted by the control-points subsystem (S02) when a registration batch begins.
// Companion to control_points_registered (§8.2.9); the pair brackets the
// registration batch per CP §7.1.
//
// # Payload fields (event-model.md §8.2.10)
//
//   - batch_id    — opaque per-batch identifier correlating started and completed
//     events; plain string per typed-alias-deferral
//   - started_at  — RFC 3339 wall-clock timestamp at batch start
type ControlPointsRegistrationStartedPayload struct {
	// BatchID is the opaque per-batch identifier. Required (non-empty).
	// Consumers correlate this value with the batch_id on the paired
	// control_points_registered event (§8.2.9) to detect crashed batches.
	//
	// TODO(hk-hqwn.59.79): hoist to typed BatchID alias when that type lands.
	BatchID string `json:"batch_id"`

	// StartedAt is the RFC 3339 wall-clock timestamp at batch start.
	// Required (non-empty). Mirrors ControlPointsRegisteredPayload.StartedAt.
	StartedAt string `json:"started_at"`
}

// Valid reports whether p is a well-formed ControlPointsRegistrationStartedPayload.
//
// Rules per event-model.md §8.2.10:
//   - BatchID must be non-empty.
//   - StartedAt must be non-empty.
func (p ControlPointsRegistrationStartedPayload) Valid() bool {
	if p.BatchID == "" {
		return false
	}
	if p.StartedAt == "" {
		return false
	}
	return true
}

// VerdictEnvelopeMismatchPayload is the typed event payload for the
// verdict_envelope_mismatch event (event-model.md §8.2.11).
//
// Tags: mechanism
// Axes: llm-freedom=none; io-determinism=best-effort; replay-safety=safe; idempotency=non-idempotent
// Durability class: O (ordinary — reconciliation and audit input; the
// mismatch itself is a Cat 6 reconciliation signal per control-points.md §4.8.CP-041).
//
// Emitted by the control-points subsystem (S02) when a replay of a persisted
// verdict produces an envelope hash that does not match the stored hash per
// control-points.md §4.8.CP-041.
//
// # Payload fields (event-model.md §8.2.11)
//
//   - run_id                  — the run in whose context the mismatch was detected
//   - control_point_name      — the name of the control point whose verdict hash mismatched
//   - transition_id           — optional transition context at detection time
//   - event_id_ref            — optional EventID reference for the verdict event
//   - stored_envelope_hash    — the envelope hash that was stored at persist time
//   - current_envelope_hash   — the envelope hash produced on replay
//   - detected_at             — RFC 3339 wall-clock timestamp at mismatch detection
type VerdictEnvelopeMismatchPayload struct {
	// RunID is the run in whose context the mismatch was detected.
	// Required (must not be uuid.Nil).
	RunID RunID `json:"run_id"`

	// ControlPointName is the name of the control point whose verdict hash
	// mismatched. Required (non-empty).
	//
	// TODO(hk-hqwn.59.80): hoist to typed ControlPointName alias when that
	// type lands.
	ControlPointName string `json:"control_point_name"`

	// TransitionID is the transition in whose context the mismatch was detected.
	// Corresponds to transition_id? in event-model.md §8.2.11. Nil when no
	// transition context is available. Non-nil must not be uuid.Nil.
	TransitionID *TransitionID `json:"transition_id,omitempty"`

	// EventIDRef is an optional EventID reference for the verdict event.
	// Corresponds to event_id_ref? in event-model.md §8.2.11. Nil when no
	// event reference is available. Non-nil must not be uuid.Nil.
	EventIDRef *EventID `json:"event_id_ref,omitempty"`

	// StoredEnvelopeHash is the envelope hash stored at verdict-persist time.
	// Required (non-empty).
	StoredEnvelopeHash string `json:"stored_envelope_hash"`

	// CurrentEnvelopeHash is the envelope hash produced when the verdict was
	// replayed. Required (non-empty).
	CurrentEnvelopeHash string `json:"current_envelope_hash"`

	// DetectedAt is the RFC 3339 wall-clock timestamp at mismatch detection.
	// Required (non-empty).
	DetectedAt string `json:"detected_at"`
}

// Valid reports whether p is a well-formed VerdictEnvelopeMismatchPayload.
//
// Rules per event-model.md §8.2.11:
//   - RunID must not be uuid.Nil.
//   - ControlPointName must be non-empty.
//   - TransitionID, when non-nil, must not be uuid.Nil.
//   - EventIDRef, when non-nil, must not be uuid.Nil.
//   - StoredEnvelopeHash must be non-empty.
//   - CurrentEnvelopeHash must be non-empty.
//   - DetectedAt must be non-empty.
func (p VerdictEnvelopeMismatchPayload) Valid() bool {
	if uuid.UUID(p.RunID) == uuid.Nil {
		return false
	}
	if p.ControlPointName == "" {
		return false
	}
	if p.TransitionID != nil && uuid.UUID(*p.TransitionID) == uuid.Nil {
		return false
	}
	if p.EventIDRef != nil && uuid.UUID(*p.EventIDRef) == uuid.Nil {
		return false
	}
	if p.StoredEnvelopeHash == "" {
		return false
	}
	if p.CurrentEnvelopeHash == "" {
		return false
	}
	if p.DetectedAt == "" {
		return false
	}
	return true
}

// PolicyCostBound is the typed discriminator for the bound_fired field of a
// policy_expression_exceeded_cost event (event-model.md §8.2.12 §6.3;
// control-points.md §4.7.CP-034b).
//
// The two values discriminate which cost bound triggered the policy expression
// abort. This field is load-bearing per CP-034b.
type PolicyCostBound string

const (
	// PolicyCostBoundASTSteps is fired when the AST-step count bound was exceeded.
	// Corresponds to io-determinism=deterministic (AST step counting is deterministic).
	PolicyCostBoundASTSteps PolicyCostBound = "ast_steps"

	// PolicyCostBoundWallClock is fired when the wall-clock time bound was exceeded.
	// Corresponds to io-determinism=best-effort (wall-clock depends on scheduling).
	PolicyCostBoundWallClock PolicyCostBound = "wall_clock"
)

// Valid reports whether b is one of the two declared PolicyCostBound constants.
func (b PolicyCostBound) Valid() bool {
	switch b {
	case PolicyCostBoundASTSteps, PolicyCostBoundWallClock:
		return true
	default:
		return false
	}
}

// PolicyEvalIODeterminism is the typed discriminator for the io_determinism field
// of a policy_expression_exceeded_cost event (event-model.md §8.2.12 §6.3;
// control-points.md §4.7.CP-034b).
//
// This field is load-bearing per CP-034b: `ast_steps` bound => deterministic;
// `wall_clock` bound => best-effort. Re-adding or renaming post-MVH would be a
// breaking event-payload change per §8.2.12 spec note.
type PolicyEvalIODeterminism string

const (
	// PolicyEvalIODeterminismDeterministic is used when bound_fired=ast_steps.
	PolicyEvalIODeterminismDeterministic PolicyEvalIODeterminism = "deterministic"

	// PolicyEvalIODeterminismBestEffort is used when bound_fired=wall_clock.
	PolicyEvalIODeterminismBestEffort PolicyEvalIODeterminism = "best-effort"
)

// Valid reports whether d is one of the two declared PolicyEvalIODeterminism constants.
func (d PolicyEvalIODeterminism) Valid() bool {
	switch d {
	case PolicyEvalIODeterminismDeterministic, PolicyEvalIODeterminismBestEffort:
		return true
	default:
		return false
	}
}

// PolicyExpressionExceededCostPayload is the typed event payload for the
// policy_expression_exceeded_cost event (event-model.md §8.2.12).
//
// Tags: mechanism
// Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent
// Durability class: F (fsync-boundary — CP-034b durability pair: the event MUST
// reach JSONL durability before the evaluator wrapper returns control to its caller).
//
// Emitted by the control-points subsystem (S02) when a policy expression
// evaluation aborts because it exceeded the cost ceiling per CP-034b. The
// bound_fired and io_determinism fields are load-bearing per CP-034b and MUST
// NOT be removed or renamed post-MVH.
//
// # Payload fields (event-model.md §8.2.12 §6.3)
//
//   - run_id              — the run in whose context the abort occurred; absent for
//     non-run-scoped policy evaluations
//   - control_point_name  — the name of the control point whose policy aborted
//   - bound_fired         — which cost bound triggered the abort (ast_steps or wall_clock)
//   - io_determinism      — per-abort io-determinism tag (deterministic or best-effort)
//   - aborted_at          — RFC 3339 wall-clock timestamp at abort
type PolicyExpressionExceededCostPayload struct {
	// RunID is the run in whose context the abort occurred.
	// Corresponds to run_id? in event-model.md §8.2.12. Nil for non-run-scoped
	// policy evaluations. Non-nil must not be uuid.Nil.
	RunID *RunID `json:"run_id,omitempty"`

	// ControlPointName is the name of the control point whose policy evaluation
	// exceeded the cost ceiling. Required (non-empty).
	//
	// TODO(hk-hqwn.59.81): hoist to typed ControlPointName alias when that
	// type lands.
	ControlPointName string `json:"control_point_name"`

	// BoundFired identifies which CP-034b cost bound triggered the abort.
	// Required; must be a valid PolicyCostBound constant.
	// Load-bearing per CP-034b — MUST NOT be removed or renamed post-MVH.
	BoundFired PolicyCostBound `json:"bound_fired"`

	// IODeterminism is the per-abort io-determinism tag per CP-034b.
	// Must be PolicyEvalIODeterminismDeterministic when BoundFired=ast_steps,
	// and PolicyEvalIODeterminismBestEffort when BoundFired=wall_clock.
	// Required; must be a valid PolicyEvalIODeterminism constant.
	// Load-bearing per CP-034b — MUST NOT be removed or renamed post-MVH.
	IODeterminism PolicyEvalIODeterminism `json:"io_determinism"`

	// AbortedAt is the RFC 3339 wall-clock timestamp at which the evaluation
	// was aborted. Required (non-empty).
	AbortedAt string `json:"aborted_at"`
}

// Valid reports whether p is a well-formed PolicyExpressionExceededCostPayload.
//
// Rules per event-model.md §8.2.12 and control-points.md §4.7.CP-034b:
//   - RunID, when non-nil, must not be uuid.Nil.
//   - ControlPointName must be non-empty.
//   - BoundFired must be a valid PolicyCostBound constant.
//   - IODeterminism must be a valid PolicyEvalIODeterminism constant.
//   - IODeterminism must be consistent with BoundFired:
//     ast_steps => deterministic; wall_clock => best-effort.
//   - AbortedAt must be non-empty.
func (p PolicyExpressionExceededCostPayload) Valid() bool {
	if p.RunID != nil && uuid.UUID(*p.RunID) == uuid.Nil {
		return false
	}
	if p.ControlPointName == "" {
		return false
	}
	if !p.BoundFired.Valid() {
		return false
	}
	if !p.IODeterminism.Valid() {
		return false
	}
	// CP-034b consistency invariant: ast_steps => deterministic; wall_clock => best-effort.
	switch p.BoundFired {
	case PolicyCostBoundASTSteps:
		if p.IODeterminism != PolicyEvalIODeterminismDeterministic {
			return false
		}
	case PolicyCostBoundWallClock:
		if p.IODeterminism != PolicyEvalIODeterminismBestEffort {
			return false
		}
	}
	if p.AbortedAt == "" {
		return false
	}
	return true
}
