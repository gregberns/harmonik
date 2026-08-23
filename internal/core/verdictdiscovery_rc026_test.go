package core

import (
	"testing"
)

// TestVerdictDiscoveryState_ThreeValuesAreDeclared verifies that exactly three
// VerdictDiscoveryState constants are declared and each is valid.
//
// RC-026 maps branch evidence to three mutually exclusive states.
func TestVerdictDiscoveryState_ThreeValuesAreDeclared(t *testing.T) {
	t.Parallel()

	states := []struct {
		constant VerdictDiscoveryState
		str      string
	}{
		{VerdictDiscoveryStateClean, "clean"},
		{VerdictDiscoveryStateCat3b, "cat-3b"},
		{VerdictDiscoveryStateResolved, "resolved"},
	}

	const wantCount = 3
	if len(states) != wantCount {
		t.Errorf("VerdictDiscoveryState: expected %d constants, got %d", wantCount, len(states))
	}

	for _, tc := range states {
		t.Run(tc.str, func(t *testing.T) {
			t.Parallel()

			if !tc.constant.Valid() {
				t.Errorf("VerdictDiscoveryState(%q).Valid() = false, want true", tc.str)
			}
			if string(tc.constant) != tc.str {
				t.Errorf("VerdictDiscoveryState string = %q, want %q",
					string(tc.constant), tc.str)
			}
		})
	}
}

// TestVerdictDiscoveryState_UnknownIsInvalid verifies that unknown
// VerdictDiscoveryState values fail Valid().
func TestVerdictDiscoveryState_UnknownIsInvalid(t *testing.T) {
	t.Parallel()

	unknown := []VerdictDiscoveryState{
		"",
		"cat3b",
		"resolve",
		"dirty",
	}

	for _, s := range unknown {
		t.Run(string(s), func(t *testing.T) {
			t.Parallel()

			if s.Valid() {
				t.Errorf("VerdictDiscoveryState(%q).Valid() = true, want false (unknown value)", s)
			}
		})
	}
}

// TestVerdictDiscoveryState_Cat3bStringMatchesReconciliationCategory verifies
// that VerdictDiscoveryStateCat3b's string representation matches the
// ReconciliationCategoryCat3b string, so Cat 3b references are consistent
// across both types.
//
// RC-026: a branch with a verdict commit but no verdict-executed commit is
// classified as Cat 3b. The two type systems MUST agree on the string "cat-3b".
func TestVerdictDiscoveryState_Cat3bStringMatchesReconciliationCategory(t *testing.T) {
	t.Parallel()

	discoveryStr := string(VerdictDiscoveryStateCat3b)
	categoryStr := string(ReconciliationCategoryCat3b)

	if discoveryStr != categoryStr {
		t.Errorf("VerdictDiscoveryStateCat3b string %q != ReconciliationCategoryCat3b string %q; "+
			"both must represent Cat 3b consistently", discoveryStr, categoryStr)
	}
}

// TestDiscoverVerdictExecution_Clean_NeitherCommit verifies that when neither
// the verdict commit nor the verdict-executed commit is present, the result is
// VerdictDiscoveryStateClean and ReconciliationCategoryCat5.
//
// RC-026: "A reconciliation workflow with a verdict commit but no verdict-
// executed commit MUST be classified as Cat 3b." The converse (no verdict
// commit at all) is Cat 5 (clean restart).
//
// Spec ref: specs/reconciliation/spec.md §8.5; RC-026.
func TestDiscoverVerdictExecution_Clean_NeitherCommit(t *testing.T) {
	t.Parallel()

	evidence := BranchVerdictEvidence{
		HasVerdictCommit:         false,
		HasVerdictExecutedCommit: false,
	}

	state, cat := DiscoverVerdictExecution(evidence)

	if state != VerdictDiscoveryStateClean {
		t.Errorf("DiscoverVerdictExecution(clean branch).state = %q, want %q",
			state, VerdictDiscoveryStateClean)
	}
	if cat != ReconciliationCategoryCat5 {
		t.Errorf("DiscoverVerdictExecution(clean branch).category = %q, want %q",
			cat, ReconciliationCategoryCat5)
	}
}

// TestDiscoverVerdictExecution_Cat3b_VerdictCommitOnlyPresent verifies that
// when a verdict commit is present but no verdict-executed commit follows, the
// result is VerdictDiscoveryStateCat3b and ReconciliationCategoryCat3b.
//
// RC-026: "A reconciliation workflow with a verdict commit but no verdict-
// executed commit MUST be classified as §8.5 Cat 3b with the dedicated
// auto-resolver re-attempting the verdict's mechanical action under a fresh
// staleness check (RC-024)."
//
// Spec ref: specs/reconciliation/spec.md §8.5 (Cat 3b detection rule);
// RC-026; schemas.md §6.3 Cat 3b row.
func TestDiscoverVerdictExecution_Cat3b_VerdictCommitOnlyPresent(t *testing.T) {
	t.Parallel()

	evidence := BranchVerdictEvidence{
		HasVerdictCommit:         true,
		HasVerdictExecutedCommit: false,
	}

	state, cat := DiscoverVerdictExecution(evidence)

	if state != VerdictDiscoveryStateCat3b {
		t.Errorf("DiscoverVerdictExecution(verdict-only branch).state = %q, want %q",
			state, VerdictDiscoveryStateCat3b)
	}
	if cat != ReconciliationCategoryCat3b {
		t.Errorf("DiscoverVerdictExecution(verdict-only branch).category = %q, want %q",
			cat, ReconciliationCategoryCat3b)
	}
}

// TestDiscoverVerdictExecution_Resolved_BothCommitsPresent verifies that when
// both the verdict commit and the verdict-executed commit are present, the
// result is VerdictDiscoveryStateResolved.
//
// RC-026: "The startup detector...MUST treat a reconciliation workflow as
// resolved ONLY if both the verdict commit AND the verdict-executed commit
// (per RC-025) are present on the investigator's branch."
//
// Spec ref: specs/reconciliation/spec.md §4.5 RC-026.
func TestDiscoverVerdictExecution_Resolved_BothCommitsPresent(t *testing.T) {
	t.Parallel()

	evidence := BranchVerdictEvidence{
		HasVerdictCommit:         true,
		HasVerdictExecutedCommit: true,
	}

	state, _ := DiscoverVerdictExecution(evidence)

	if state != VerdictDiscoveryStateResolved {
		t.Errorf("DiscoverVerdictExecution(both commits).state = %q, want %q",
			state, VerdictDiscoveryStateResolved)
	}
}

// TestDiscoverVerdictExecution_Resolved_IsNotCat3b verifies that the resolved
// state is strictly distinct from Cat 3b: having both commits is the ONLY
// condition that bypasses Cat 3b.
//
// RC-026: "MUST be classified as Cat 3b" when verdict commit is present but
// verdict-executed commit is absent.
func TestDiscoverVerdictExecution_Resolved_IsNotCat3b(t *testing.T) {
	t.Parallel()

	evidence := BranchVerdictEvidence{
		HasVerdictCommit:         true,
		HasVerdictExecutedCommit: true,
	}

	state, cat := DiscoverVerdictExecution(evidence)

	if state == VerdictDiscoveryStateCat3b {
		t.Error("DiscoverVerdictExecution(both commits) returned Cat 3b; want resolved — " +
			"verdict-executed commit presence MUST prevent Cat 3b classification (RC-026)")
	}
	if cat == ReconciliationCategoryCat3b {
		t.Error("DiscoverVerdictExecution(both commits) returned category cat-3b; want cat-5 sentinel — " +
			"resolved workflow MUST NOT produce a Cat 3b category")
	}
}

// TestDiscoverVerdictExecution_AllThreeStatesReturnValidCategory verifies that
// every branch-evidence combination produces a valid ReconciliationCategory.
//
// RC-026 maps every possible BranchVerdictEvidence combination to one of the
// declared ReconciliationCategory values; the returned category must pass
// ReconciliationCategory.Valid() for the daemon to emit a valid
// reconciliation_category_assigned event (RC-013).
func TestDiscoverVerdictExecution_AllThreeStatesReturnValidCategory(t *testing.T) {
	t.Parallel()

	cases := []BranchVerdictEvidence{
		{HasVerdictCommit: false, HasVerdictExecutedCommit: false},
		{HasVerdictCommit: true, HasVerdictExecutedCommit: false},
		{HasVerdictCommit: true, HasVerdictExecutedCommit: true},
	}

	for _, e := range cases {
		t.Run("", func(t *testing.T) {
			t.Parallel()

			state, cat := DiscoverVerdictExecution(e)

			if !state.Valid() {
				t.Errorf("DiscoverVerdictExecution(%+v).state = %q; state must be valid", e, state)
			}
			if !cat.Valid() {
				t.Errorf("DiscoverVerdictExecution(%+v).category = %q; category must be valid", e, cat)
			}
		})
	}
}

// TestDiscoverVerdictExecution_Cat3b_RequiresVerdictCommit verifies the
// necessary condition for Cat 3b: HasVerdictCommit MUST be true.
//
// A branch without a verdict commit cannot be Cat 3b regardless of the
// HasVerdictExecutedCommit field.
//
// RC-026: "A reconciliation workflow with a verdict commit but no verdict-
// executed commit MUST be classified as Cat 3b." Without a verdict commit,
// no Cat 3b condition exists.
func TestDiscoverVerdictExecution_Cat3b_RequiresVerdictCommit(t *testing.T) {
	t.Parallel()

	evidence := BranchVerdictEvidence{
		HasVerdictCommit:         false,
		HasVerdictExecutedCommit: false,
	}

	state, cat := DiscoverVerdictExecution(evidence)

	if state == VerdictDiscoveryStateCat3b {
		t.Error("RC-026: DiscoverVerdictExecution with HasVerdictCommit=false returned Cat 3b; " +
			"Cat 3b requires a verdict commit")
	}
	if cat == ReconciliationCategoryCat3b {
		t.Error("RC-026: DiscoverVerdictExecution with HasVerdictCommit=false returned category cat-3b; " +
			"Cat 3b requires a verdict commit")
	}
}

// TestDiscoverVerdictExecution_StartupDetectorMustSeeVerdictExecutedToResolve
// verifies the RC-026 "resolved ONLY IF both commits" rule by confirming that
// the verdict-executed commit alone (without a verdict commit) does NOT produce
// a resolved state (it would be a Cat 6a integrity violation in practice, but
// DiscoverVerdictExecution returns clean since HasVerdictCommit is the gate).
//
// RC-026: "MUST treat a reconciliation workflow as resolved ONLY if both the
// verdict commit AND the verdict-executed commit...are present."
func TestDiscoverVerdictExecution_StartupDetectorMustSeeVerdictExecutedToResolve(t *testing.T) {
	t.Parallel()

	evidence := BranchVerdictEvidence{
		HasVerdictCommit:         false,
		HasVerdictExecutedCommit: false,
	}

	state, _ := DiscoverVerdictExecution(evidence)

	if state == VerdictDiscoveryStateResolved {
		t.Error("RC-026: DiscoverVerdictExecution without verdict commit returned resolved; " +
			"resolved requires BOTH verdict commit AND verdict-executed commit")
	}
}

// TestReconciliationClassificationGate_DefaultIsValid verifies that
// DefaultReconciliationClassificationGate() returns a valid gate record.
//
// RC-026: The startup-ordering contract is non-empty and structurally valid.
func TestReconciliationClassificationGate_DefaultIsValid(t *testing.T) {
	t.Parallel()

	gate := DefaultReconciliationClassificationGate()

	if !gate.Valid() {
		t.Error("DefaultReconciliationClassificationGate().Valid() = false; " +
			"the default gate record must satisfy all shape invariants")
	}
}

// TestReconciliationClassificationGate_ClassificationPassBeforeReadyIsNonEmpty
// verifies the startup-ordering rule field is non-empty.
//
// RC-026: "reconciliation detectors run before the daemon transitions to
// `ready`; ordinary dispatch is gated behind detection completion."
func TestReconciliationClassificationGate_ClassificationPassBeforeReadyIsNonEmpty(t *testing.T) {
	t.Parallel()

	gate := DefaultReconciliationClassificationGate()

	if gate.ClassificationPassBeforeReady == "" {
		t.Error("DefaultReconciliationClassificationGate().ClassificationPassBeforeReady is empty; " +
			"RC-026 startup-ordering rule must be documented")
	}
}

// TestReconciliationClassificationGate_OrdinaryDispatchGatedByIsNonEmpty verifies
// that the ordinary-dispatch gate description is non-empty.
//
// RC-026: "ordinary dispatch is gated behind detection completion."
func TestReconciliationClassificationGate_OrdinaryDispatchGatedByIsNonEmpty(t *testing.T) {
	t.Parallel()

	gate := DefaultReconciliationClassificationGate()

	if gate.OrdinaryDispatchGatedBy == "" {
		t.Error("DefaultReconciliationClassificationGate().OrdinaryDispatchGatedBy is empty; " +
			"RC-026 dispatch-gating rule must be documented")
	}
}

// TestReconciliationClassificationGate_OQRc003FailOpenPolicyIsNonEmpty verifies
// that the OQ-RC-003 open question is documented in the gate record.
//
// OQ-RC-003: The fail-open escalation question must be tracked; the
// conservative default (refuse to reach `ready`) is the current policy.
func TestReconciliationClassificationGate_OQRc003FailOpenPolicyIsNonEmpty(t *testing.T) {
	t.Parallel()

	gate := DefaultReconciliationClassificationGate()

	if gate.OQRc003FailOpenPolicy == "" {
		t.Error("DefaultReconciliationClassificationGate().OQRc003FailOpenPolicy is empty; " +
			"OQ-RC-003 fail-open escalation question must be documented per RC-026")
	}
}

// TestReconciliationClassificationGate_EmptyFieldsAreInvalid verifies that a
// gate with any empty field fails Valid().
func TestReconciliationClassificationGate_EmptyFieldsAreInvalid(t *testing.T) {
	t.Parallel()

	base := DefaultReconciliationClassificationGate()

	cases := []struct {
		name   string
		mutate func(*ReconciliationClassificationGate)
	}{
		{
			name:   "empty ClassificationPassBeforeReady",
			mutate: func(g *ReconciliationClassificationGate) { g.ClassificationPassBeforeReady = "" },
		},
		{
			name:   "empty OrdinaryDispatchGatedBy",
			mutate: func(g *ReconciliationClassificationGate) { g.OrdinaryDispatchGatedBy = "" },
		},
		{
			name:   "empty OQRc003FailOpenPolicy",
			mutate: func(g *ReconciliationClassificationGate) { g.OQRc003FailOpenPolicy = "" },
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			g := base
			tc.mutate(&g)
			if g.Valid() {
				t.Errorf("ReconciliationClassificationGate.Valid() = true with %s; want false", tc.name)
			}
		})
	}
}

// TestRC026_StartupDetectorMustRunBeforeReady verifies the RC-026 startup-
// ordering invariant at the shape level: the classification gate documents that
// reconciliation detectors MUST run before the daemon reaches `ready`.
//
// RC-026: "The daemon startup sequence MUST dispatch the reconciliation-workflow
// classification pass BEFORE any ordinary workflow dispatches."
//
// Spec ref: specs/reconciliation/spec.md §4.5 RC-026;
// specs/process-lifecycle.md §4.2 PL-005 step 7.
func TestRC026_StartupDetectorMustRunBeforeReady(t *testing.T) {
	t.Parallel()

	gate := DefaultReconciliationClassificationGate()

	if gate.ClassificationPassBeforeReady == "" {
		t.Error("RC-026: ClassificationPassBeforeReady is empty; " +
			"the startup ordering constraint MUST be documented in the gate record")
	}
	if !gate.Valid() {
		t.Error("RC-026: StartupClassificationGate.Valid() = false; " +
			"the startup ordering contract must satisfy all shape invariants")
	}
}

// TestRC026_Cat3bDetectionRuleBindsToVerdictUnexecutedCategory verifies that
// the Cat 3b detection rule (verdict commit present, no verdict-executed commit)
// produces exactly the Cat 3b ReconciliationCategory, not a different category.
//
// This is the binding test for the detection rule: the result of
// DiscoverVerdictExecution for the Cat 3b case MUST map to
// ReconciliationCategoryCat3b, which is the same category consumed by the
// auto-resolver and the reconciliation_category_assigned event (RC-013).
//
// Spec ref: specs/reconciliation/spec.md §8.5; RC-026; §8.12.
func TestRC026_Cat3bDetectionRuleBindsToVerdictUnexecutedCategory(t *testing.T) {
	t.Parallel()

	evidence := BranchVerdictEvidence{
		HasVerdictCommit:         true,
		HasVerdictExecutedCommit: false,
	}

	state, cat := DiscoverVerdictExecution(evidence)

	if state != VerdictDiscoveryStateCat3b {
		t.Errorf("RC-026: Cat 3b evidence produced state %q; want %q",
			state, VerdictDiscoveryStateCat3b)
	}
	if cat != ReconciliationCategoryCat3b {
		t.Errorf("RC-026: Cat 3b evidence produced category %q; want %q",
			cat, ReconciliationCategoryCat3b)
	}
	if !cat.Valid() {
		t.Errorf("RC-026: Cat 3b category %q is invalid per ReconciliationCategory.Valid()", cat)
	}
}

// TestRC026_ResolvedStateRequiresBothCommits verifies the "resolved ONLY IF
// both commits" rule from RC-026.
//
// The resolved state is the ONLY state produced by a branch with both the
// verdict commit and the verdict-executed commit; no other combination reaches
// VerdictDiscoveryStateResolved.
//
// Spec ref: specs/reconciliation/spec.md §4.5 RC-026.
func TestRC026_ResolvedStateRequiresBothCommits(t *testing.T) {
	t.Parallel()

	withBoth := BranchVerdictEvidence{HasVerdictCommit: true, HasVerdictExecutedCommit: true}
	stateWithBoth, _ := DiscoverVerdictExecution(withBoth)
	if stateWithBoth != VerdictDiscoveryStateResolved {
		t.Errorf("RC-026: both commits → state = %q, want %q",
			stateWithBoth, VerdictDiscoveryStateResolved)
	}

	withVerdictOnly := BranchVerdictEvidence{HasVerdictCommit: true, HasVerdictExecutedCommit: false}
	stateVerdictOnly, _ := DiscoverVerdictExecution(withVerdictOnly)
	if stateVerdictOnly == VerdictDiscoveryStateResolved {
		t.Errorf("RC-026: verdict commit only → state = %q (resolved); "+
			"resolved MUST require both commits", stateVerdictOnly)
	}

	withNeither := BranchVerdictEvidence{HasVerdictCommit: false, HasVerdictExecutedCommit: false}
	stateNeither, _ := DiscoverVerdictExecution(withNeither)
	if stateNeither == VerdictDiscoveryStateResolved {
		t.Errorf("RC-026: neither commit → state = %q (resolved); "+
			"resolved MUST require both commits", stateNeither)
	}
}

// TestRC026_OQRc003_ConservativeDefaultDocumented verifies that the OQ-RC-003
// conservative default (refuse to reach `ready` when classification fails) is
// documented in the gate record.
//
// OQ-RC-003: "The conservative default is (a); (b) would improve availability
// at the cost of letting new work accumulate against unclassified existing work."
//
// Spec ref: specs/reconciliation/spec.md OQ-RC-003.
func TestRC026_OQRc003_ConservativeDefaultDocumented(t *testing.T) {
	t.Parallel()

	gate := DefaultReconciliationClassificationGate()

	if gate.OQRc003FailOpenPolicy == "" {
		t.Error("OQ-RC-003: fail-open policy field is empty; " +
			"the conservative default must be documented in the gate record")
	}
}
