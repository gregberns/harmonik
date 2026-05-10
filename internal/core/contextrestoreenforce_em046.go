package core

import (
	"errors"
	"fmt"
)

// ErrContextRestoreHandlerForbidden is returned by
// ValidateContextRestoreInitiationSource when a handler-produced Outcome
// selects TransitionKindContextRestore.
//
// Per execution-model.md §4.10.EM-046:
//
//	"A context-restore transition is initiated by the daemon or by a
//	reconciliation verdict per [reconciliation/spec.md §4.5 RC-020], not by a
//	handler."
//
// Callers SHOULD use errors.Is to test for this sentinel and MUST reject the
// handler outcome without checkpointing.
var ErrContextRestoreHandlerForbidden = errors.New(
	"context-restore transition forbidden: initiation source must be daemon or reconciliation, not a handler (EM-046)",
)

// ValidateContextRestoreInitiationSource enforces the EM-046 initiation-source
// rule for context-restore transitions.
//
// Tags: mechanism
//
// # Spec source (execution-model.md §4.10.EM-046)
//
// A context-restore transition MUST be initiated by the daemon or by a
// reconciliation verdict, NOT by a handler:
//
//	"A context-restore transition is initiated by the daemon or by a
//	reconciliation verdict per [reconciliation/spec.md §4.5 RC-020], not by a
//	handler; the Outcome associated with a context-restore transition is
//	synthesized by the daemon with status = SUCCESS and an actor_role of daemon
//	or the role of the verdict-executing subsystem."
//
// # Usage
//
// Call this function before checkpointing any transition whose kind is derived
// from a handler-produced Outcome. If the proposed TransitionKind is
// context-restore and the Outcome was produced by a handler (i.e., actorRole
// is one of the declared handler roles per architecture.md §4.8.AR-032), this
// function returns ErrContextRestoreHandlerForbidden and the daemon MUST reject
// the transition.
//
// A return value of nil means either:
//   - the transition kind is not context-restore (no restriction), OR
//   - the actor role is daemon or reconciliation (initiation-source permitted).
//
// # Actor role check
//
// Handler roles at MVH per architecture.md §4.8.AR-032:
// Planner, Researcher, Builder, Reviewer, Verifier, Scheduler, Governor.
// Daemon-permitted roles per EM-046: ActorRoleDaemon, ActorRoleReconciliation.
func ValidateContextRestoreInitiationSource(kind TransitionKind, actorRole ActorRole) error {
	if kind != TransitionKindContextRestore {
		return nil
	}
	switch actorRole {
	case ActorRoleDaemon, ActorRoleReconciliation:
		return nil
	default:
		return fmt.Errorf(
			"actorRole %q cannot initiate context-restore transition: %w",
			string(actorRole), ErrContextRestoreHandlerForbidden,
		)
	}
}

// NewContextRestoreTransition constructs a daemon-synthesized Transition for a
// context-restore operation per execution-model.md §4.10.EM-046 and
// §4.5.EM-023a.
//
// Tags: mechanism
//
// # Spec source (execution-model.md §4.10.EM-046 + §4.5.EM-023a)
//
// A context-restore transition is daemon-produced, not handler-produced. The
// daemon synthesizes an Outcome with status = SUCCESS and actor_role ∈
// {daemon, reconciliation}. The synthesized Outcome is recorded in the
// Transition record's evidence map under the reserved key
// EvidenceKeySynthesizedOutcome = true per EM-023a.
//
// # Parameter contracts
//
//   - actorRole MUST be ActorRoleDaemon or ActorRoleReconciliation. Any other
//     value returns an error wrapping ErrContextRestoreHandlerForbidden.
//   - base is the caller-supplied Transition skeleton. NewContextRestoreTransition
//     enforces: TransitionKind = context-restore, OutcomeStatus = SUCCESS,
//     ActorRole = actorRole, and EvidenceKeySynthesizedOutcome = true in Evidence.
//     RollbackToStateID MUST be nil (context-restore does not relocate graph
//     position per EM-044/EM-046); a non-nil value is rejected with an error.
//   - The caller MUST supply a valid TransitionID (non-zero), RunID (non-zero),
//     FromState, ToState, ChosenAction, PolicyVersion, and SchemaVersion before
//     calling Valid() on the returned Transition.
//
// # Usage note
//
// The returned Transition has TransitionKind and OutcomeStatus enforced; the
// caller is responsible for the remaining Transition fields (TransitionID,
// RunID, FromState, ToState, ChosenAction, PolicyVersion, SchemaVersion).
// Call tr.Valid() after populating all fields before checkpointing.
func NewContextRestoreTransition(base Transition, actorRole ActorRole) (Transition, error) {
	if err := ValidateContextRestoreInitiationSource(TransitionKindContextRestore, actorRole); err != nil {
		return Transition{}, err
	}
	if base.RollbackToStateID != nil {
		return Transition{}, fmt.Errorf(
			"context-restore transition MUST NOT carry RollbackToStateID (EM-044/EM-046): got non-nil value %v",
			*base.RollbackToStateID,
		)
	}

	// Enforce daemon-synthesized fields per EM-046 + EM-023a.
	base.TransitionKind = TransitionKindContextRestore
	base.OutcomeStatus = OutcomeStatusSuccess
	base.ActorRole = actorRole

	// Record the synthesized-outcome marker per EM-023a.
	if base.Evidence == nil {
		base.Evidence = make(Evidence)
	}
	base.Evidence[EvidenceKeySynthesizedOutcome] = true

	return base, nil
}
