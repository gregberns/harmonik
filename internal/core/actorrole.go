package core

import "fmt"

// ActorRole is the role-name string carried in a Trace/Transition record
// (architecture.md §4.3.AR-012; architecture.md §4.8.AR-032).
//
// The seven declared role names per AR-032 are: Planner, Researcher, Builder,
// Reviewer, Verifier, Scheduler, Governor. Daemon-synthesized transitions use
// "daemon" or "reconciliation" per execution-model.md §4.10.EM-046.
//
// ActorRole is a closed enum at MVH. Unknown values MUST NOT be silently
// accepted; callers observing an unknown ActorRole MUST route to reconciliation
// Cat 6a per [reconciliation/spec.md §8.11].
type ActorRole string

// Declared ActorRole constants per architecture.md §4.8.AR-032 and
// execution-model.md §4.10.EM-046.
const (
	// ActorRolePlanner is the planning role (MVH-required per AR-033).
	ActorRolePlanner ActorRole = "Planner"

	// ActorRoleResearcher is the research role (declared-but-deferred at MVH per AR-033).
	ActorRoleResearcher ActorRole = "Researcher"

	// ActorRoleBuilder is the builder role (MVH-required per AR-033).
	ActorRoleBuilder ActorRole = "Builder"

	// ActorRoleReviewer is the reviewer role (MVH-required per AR-033).
	ActorRoleReviewer ActorRole = "Reviewer"

	// ActorRoleVerifier is the verifier role (declared-but-deferred at MVH per AR-033).
	ActorRoleVerifier ActorRole = "Verifier"

	// ActorRoleScheduler is the scheduler role (declared-but-deferred at MVH per AR-033).
	ActorRoleScheduler ActorRole = "Scheduler"

	// ActorRoleGovernor is the governor role (declared-but-deferred at MVH per AR-033).
	ActorRoleGovernor ActorRole = "Governor"

	// ActorRoleDaemon is used for daemon-synthesized transitions (EM-046).
	ActorRoleDaemon ActorRole = "daemon"

	// ActorRoleReconciliation is used for reconciliation-directed transitions (EM-046).
	ActorRoleReconciliation ActorRole = "reconciliation"
)

// Valid reports whether r is one of the declared ActorRole constants.
// Unknown values return false; callers MUST NOT silently degrade — route to
// reconciliation Cat 6a per [reconciliation/spec.md §8.11].
func (r ActorRole) Valid() bool {
	switch r {
	case ActorRolePlanner,
		ActorRoleResearcher,
		ActorRoleBuilder,
		ActorRoleReviewer,
		ActorRoleVerifier,
		ActorRoleScheduler,
		ActorRoleGovernor,
		ActorRoleDaemon,
		ActorRoleReconciliation:
		return true
	default:
		return false
	}
}

// AllActorRoles returns the complete slice of declared ActorRole constants,
// including daemon-synthesised roles (ActorRoleDaemon, ActorRoleReconciliation).
//
// The slice is fixed at compile time; callers MUST NOT mutate it.
//
// Note (A9 reconciliation, 2026-07-17): the handlercontract HC-016
// WorkQueueSet helper that formerly consumed this slice was dead code (no
// non-test caller) and has been deleted. The shipping dispatch surface is
// per-queue-name, not per-actor-role — see internal/daemon
// queuestore_hkj808w.go / perqueuespendmeter_tigaf11.go.
//
// Spec: [architecture.md §4.8 AR-032].
func AllActorRoles() []ActorRole {
	return []ActorRole{
		ActorRolePlanner,
		ActorRoleResearcher,
		ActorRoleBuilder,
		ActorRoleReviewer,
		ActorRoleVerifier,
		ActorRoleScheduler,
		ActorRoleGovernor,
		ActorRoleDaemon,
		ActorRoleReconciliation,
	}
}

// MarshalText implements encoding.TextMarshaler so ActorRole serialises
// correctly in JSON and YAML.
// It rejects any value that is not one of the declared constants.
func (r ActorRole) MarshalText() ([]byte, error) {
	if !r.Valid() {
		return nil, fmt.Errorf("actorrole: unknown value %q", string(r))
	}
	return []byte(r), nil
}

// UnmarshalText implements encoding.TextUnmarshaler.
// It rejects any value that is not one of the declared constants.
// Per reconciliation/spec.md §8.11, callers observing an unknown ActorRole
// MUST route to Cat 6a; callers MUST NOT silently degrade to a default.
func (r *ActorRole) UnmarshalText(text []byte) error {
	v := ActorRole(text)
	if !v.Valid() {
		return fmt.Errorf(
			"actorrole: unknown value %q; must be one of Planner, Researcher, Builder, Reviewer, Verifier, Scheduler, Governor, daemon, reconciliation",
			string(text),
		)
	}
	*r = v
	return nil
}
