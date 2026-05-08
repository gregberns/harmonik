package scenario

import "fmt"

// CrashPoint identifies where in the checkpoint sequence the daemon is killed
// for a crash-recovery scenario. The three values correspond exactly to the
// three crash-injection points named in §10.2 of specs/scenario-harness.md and
// in specs/execution-model.md §4.4.EM-016 / §4.5.EM-025a.
//
// Spec refs:
//   - specs/execution-model.md §4.4.EM-016 (checkpoint atomicity boundary)
//   - specs/execution-model.md §4.5.EM-025a (emission ordering)
//   - specs/execution-model.md §4.5.EM-025 (ENOSPC classification)
type CrashPoint string

// Declared CrashPoint constants.
//
// The three constants map directly onto the three crash-injection scenarios
// specified in the bead hk-b3f.87:
//
//   - AfterWriteTree  — between git write-tree / commit-tree and git update-ref;
//     the commit object exists as a loose object but the ref has not advanced.
//     Expected invariants: no observable partial state; orphan loose objects
//     are eligible for git gc; the prior branch-tip is intact (EM-024).
//
//   - AfterUpdateRef  — between git update-ref and event emission; the ref has
//     advanced (the transition IS durable) but the checkpoint_written event and
//     state-entered event have not been emitted per EM-025a.
//     Expected invariants: the checkpoint commit is reachable from the task
//     branch; reconciliation is dispatched (EM-017a / Cat 6a when the sibling
//     file is missing); the new tip is a fast-forward of the prior tip (EM-024a).
//
//   - DuringCommitTreeENOSPC — ENOSPC injected mid-checkpoint (during object
//     writes); write-tree / commit-tree may be partial; update-ref has NOT run.
//     Expected invariants: classified as transient per EM-025; a new
//     transition_id is generated on retry (EM-018a); evidence files from the
//     failed attempt are removed before retry (EM-025); the branch-tip is
//     unchanged from before the failed attempt.
const (
	// CrashPointAfterWriteTree injects the crash after git write-tree /
	// commit-tree completes but before git update-ref runs.
	// Spec ref: specs/execution-model.md §4.4.EM-016.
	CrashPointAfterWriteTree CrashPoint = "after_write_tree"

	// CrashPointAfterUpdateRef injects the crash after git update-ref returns
	// success but before the daemon emits the checkpoint_written event.
	// Spec ref: specs/execution-model.md §4.5.EM-025a.
	CrashPointAfterUpdateRef CrashPoint = "after_update_ref"

	// CrashPointDuringCommitTreeENOSPC injects an ENOSPC condition during the
	// object-write phase (commit-tree / loose-object streaming). git update-ref
	// has NOT run at crash time.
	// Spec ref: specs/execution-model.md §4.5.EM-025 (ENOSPC classification).
	CrashPointDuringCommitTreeENOSPC CrashPoint = "during_commit_tree_enospc"
)

// Valid reports whether p is one of the three declared CrashPoint constants.
func (p CrashPoint) Valid() bool {
	switch p {
	case CrashPointAfterWriteTree,
		CrashPointAfterUpdateRef,
		CrashPointDuringCommitTreeENOSPC:
		return true
	default:
		return false
	}
}

// MarshalText implements encoding.TextMarshaler so CrashPoint serialises
// correctly in JSON and YAML scenario files.
func (p CrashPoint) MarshalText() ([]byte, error) {
	if !p.Valid() {
		return nil, fmt.Errorf("crashpoint: unknown value %q", string(p))
	}
	return []byte(p), nil
}

// UnmarshalText implements encoding.TextUnmarshaler. It rejects any value that
// is not one of the three declared constants.
func (p *CrashPoint) UnmarshalText(text []byte) error {
	v := CrashPoint(text)
	if !v.Valid() {
		return fmt.Errorf("crashpoint: unknown value %q; must be one of after_write_tree, after_update_ref, during_commit_tree_enospc", string(text))
	}
	*p = v
	return nil
}

// CrashRecoveryInvariant names a post-restart invariant that the scenario
// harness MUST verify after restarting the daemon following a crash injection.
// Multiple invariants are composed into a CrashRecoveryFixture.Invariants slice;
// each invariant is evaluated independently (no short-circuit per SH-023).
//
// Spec refs:
//   - specs/execution-model.md §4.4.EM-016 (orphan loose objects / no partial state)
//   - specs/execution-model.md §4.5.EM-024a (branch-tip monotonicity)
//   - specs/execution-model.md §4.4.EM-017a (corrupted-checkpoint → reconciliation)
//   - specs/execution-model.md §4.5.EM-025 (new transition_id on retry)
//   - specs/execution-model.md §4.7.EM-031 / EM-INV-001 (state reconstructable from git+Beads)
type CrashRecoveryInvariant string

// Declared CrashRecoveryInvariant constants.
const (
	// InvariantNoObservablePartialState asserts that after restart no subsystem
	// observes a partial (uncommitted) transition: the task-branch tip either
	// reflects the last durable checkpoint or the pre-crash tip, never an
	// intermediate object not reachable from a ref.
	// Spec ref: specs/execution-model.md §4.4.EM-016.
	InvariantNoObservablePartialState CrashRecoveryInvariant = "no_observable_partial_state"

	// InvariantOrphanLooseObjectsEligibleForGC asserts that any loose objects
	// written between write-tree and update-ref are NOT reachable from any ref
	// and are therefore eligible for reclamation by git gc. The harness verifies
	// this by checking that no new ref advancement occurred for the in-flight
	// transition's commit SHA.
	// Spec ref: specs/execution-model.md §4.4.EM-016.
	InvariantOrphanLooseObjectsEligibleForGC CrashRecoveryInvariant = "orphan_loose_objects_eligible_for_gc"

	// InvariantReconciliationDispatched asserts that after restart the daemon
	// detects the corrupted or incomplete checkpoint and dispatches a
	// reconciliation workflow (Cat 6a per reconciliation/spec.md §8.11).
	// Applicable when CrashPoint is AfterUpdateRef and the sibling transition
	// file is absent or truncated (EM-017a).
	// Spec ref: specs/execution-model.md §4.4.EM-017a.
	InvariantReconciliationDispatched CrashRecoveryInvariant = "reconciliation_dispatched"

	// InvariantNewTransitionIDOnRetry asserts that when the daemon retries a
	// checkpoint after a transient failure (ENOSPC), it generates a fresh
	// transition_id per EM-018a rather than reusing the failed attempt's ID.
	// Spec ref: specs/execution-model.md §4.4.EM-018a / §4.5.EM-025.
	InvariantNewTransitionIDOnRetry CrashRecoveryInvariant = "new_transition_id_on_retry"

	// InvariantBranchTipMonotonicity asserts that on restart the daemon verifies
	// the task-branch tip is a fast-forward of the last-persisted tip SHA and
	// does NOT route to reconciliation for a crash-induced non-advancement (the
	// prior tip is unchanged, not rewound).
	// Spec ref: specs/execution-model.md §4.5.EM-024a.
	InvariantBranchTipMonotonicity CrashRecoveryInvariant = "branch_tip_monotonicity"

	// InvariantStateReconstructableFromGitAndBeads asserts that after restart
	// the daemon can fully reconstruct every in-flight run's durable state by
	// walking the git checkpoint trail and querying Beads — without consulting
	// the JSONL event log.
	// Spec ref: specs/execution-model.md §4.7.EM-031 / EM-INV-001.
	InvariantStateReconstructableFromGitAndBeads CrashRecoveryInvariant = "state_reconstructable_from_git_and_beads"
)

// Valid reports whether v is one of the five declared CrashRecoveryInvariant
// constants.
func (v CrashRecoveryInvariant) Valid() bool {
	switch v {
	case InvariantNoObservablePartialState,
		InvariantOrphanLooseObjectsEligibleForGC,
		InvariantReconciliationDispatched,
		InvariantNewTransitionIDOnRetry,
		InvariantBranchTipMonotonicity,
		InvariantStateReconstructableFromGitAndBeads:
		return true
	default:
		return false
	}
}

// MarshalText implements encoding.TextMarshaler so CrashRecoveryInvariant
// serialises correctly in JSON and YAML scenario files.
func (v CrashRecoveryInvariant) MarshalText() ([]byte, error) {
	if !v.Valid() {
		return nil, fmt.Errorf("crashrecoveryinvariant: unknown value %q", string(v))
	}
	return []byte(v), nil
}

// UnmarshalText implements encoding.TextUnmarshaler. It rejects any value that
// is not one of the five declared constants.
func (v *CrashRecoveryInvariant) UnmarshalText(text []byte) error {
	candidate := CrashRecoveryInvariant(text)
	if !candidate.Valid() {
		return fmt.Errorf("crashrecoveryinvariant: unknown value %q; must be one of no_observable_partial_state, orphan_loose_objects_eligible_for_gc, reconciliation_dispatched, new_transition_id_on_retry, branch_tip_monotonicity, state_reconstructable_from_git_and_beads", string(text))
	}
	*v = candidate
	return nil
}

// CrashRecoveryFixture is the scenario-harness configuration record for a
// crash-recovery scenario. It names the crash injection point, the expected
// post-restart invariants to assert, and an optional description.
//
// The harness uses CrashRecoveryFixture to:
//  1. Arm the crash-injection hook at the specified CrashPoint in the daemon's
//     checkpoint sequence.
//  2. Kill the daemon at that point and observe the crash.
//  3. Restart the daemon and evaluate each declared Invariant independently
//     (no short-circuit per SH-023).
//
// CrashRecoveryFixture is "shared infrastructure": scenario YAML files embed
// it under the key `crash_recovery` at the scenario root, and the harness
// orchestrator reads it to set up the crash-injection run.
//
// Spec refs:
//   - specs/execution-model.md §4.4.EM-016, §4.4.EM-017a, §4.4.EM-018a
//   - specs/execution-model.md §4.5.EM-024a, §4.5.EM-025, §4.5.EM-025a
//   - specs/execution-model.md §4.7.EM-031, EM-INV-001
//   - specs/scenario-harness.md §10.2 (test-surface obligations for EM atomicity)
type CrashRecoveryFixture struct {
	// CrashAt is the point in the checkpoint sequence where the daemon is
	// killed. Required (must be one of the declared CrashPoint constants).
	CrashAt CrashPoint `json:"crash_at" yaml:"crash_at"`

	// Invariants lists the post-restart invariants the harness MUST evaluate
	// after restarting the daemon. Each invariant is evaluated independently
	// (no short-circuit per SH-023). At least one invariant MUST be declared;
	// an empty slice is invalid.
	Invariants []CrashRecoveryInvariant `json:"invariants" yaml:"invariants"`

	// Description is an operator-facing label for this crash-recovery fixture.
	// Required (non-empty) so that scenario result records are self-describing
	// when multiple crash-recovery scenarios appear in the same suite.
	Description string `json:"description" yaml:"description"`
}

// Valid reports whether the CrashRecoveryFixture is structurally well-formed:
//   - CrashAt is one of the three declared CrashPoint constants.
//   - Invariants is non-empty and every element is a declared CrashRecoveryInvariant.
//   - Description is non-empty.
//   - No invariant appears more than once (duplicate invariants are a scenario
//     authoring error; the harness would evaluate the same assertion twice).
func (f CrashRecoveryFixture) Valid() bool {
	if !f.CrashAt.Valid() {
		return false
	}
	if f.Description == "" {
		return false
	}
	if len(f.Invariants) == 0 {
		return false
	}
	seen := make(map[CrashRecoveryInvariant]struct{}, len(f.Invariants))
	for _, inv := range f.Invariants {
		if !inv.Valid() {
			return false
		}
		if _, dup := seen[inv]; dup {
			return false
		}
		seen[inv] = struct{}{}
	}
	return true
}

// canonicalInvariantsFor returns the canonical set of CrashRecoveryInvariant
// values appropriate for a given CrashPoint. This is the recommended default
// for scenario authors who want comprehensive coverage without hand-picking
// each invariant. Callers MAY use this to populate CrashRecoveryFixture.Invariants
// or MAY declare their own subset.
//
// The mapping is:
//
//   - AfterWriteTree  → NoObservablePartialState, OrphanLooseObjectsEligibleForGC,
//     BranchTipMonotonicity, StateReconstructableFromGitAndBeads
//
//   - AfterUpdateRef  → NoObservablePartialState, ReconciliationDispatched,
//     BranchTipMonotonicity, StateReconstructableFromGitAndBeads
//
//   - DuringCommitTreeENOSPC → NoObservablePartialState, OrphanLooseObjectsEligibleForGC,
//     NewTransitionIDOnRetry, BranchTipMonotonicity, StateReconstructableFromGitAndBeads
//
// An unknown CrashPoint returns nil.
func canonicalInvariantsFor(p CrashPoint) []CrashRecoveryInvariant {
	switch p {
	case CrashPointAfterWriteTree:
		return []CrashRecoveryInvariant{
			InvariantNoObservablePartialState,
			InvariantOrphanLooseObjectsEligibleForGC,
			InvariantBranchTipMonotonicity,
			InvariantStateReconstructableFromGitAndBeads,
		}
	case CrashPointAfterUpdateRef:
		return []CrashRecoveryInvariant{
			InvariantNoObservablePartialState,
			InvariantReconciliationDispatched,
			InvariantBranchTipMonotonicity,
			InvariantStateReconstructableFromGitAndBeads,
		}
	case CrashPointDuringCommitTreeENOSPC:
		return []CrashRecoveryInvariant{
			InvariantNoObservablePartialState,
			InvariantOrphanLooseObjectsEligibleForGC,
			InvariantNewTransitionIDOnRetry,
			InvariantBranchTipMonotonicity,
			InvariantStateReconstructableFromGitAndBeads,
		}
	default:
		return nil
	}
}
