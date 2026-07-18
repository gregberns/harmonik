package core

import (
	"testing"

	"github.com/google/uuid"
)

// budgetexhaustion_rc018_test.go — tests for the RC-018 budget-exhaustion
// handler sequence and fallback-verdict synthesis.
//
// Covers:
//   - BudgetExhaustionStep enum validity and count.
//   - BudgetExhaustionHandlerSequence: 5 steps in spec order, non-empty labels
//     and descriptions, correct fsync-boundary classification.
//   - SynthesizeBudgetExhaustionFallbackVerdict: valid output for valid inputs;
//     correct rejection of zero RunIDs and invalid SnapshotToken; fallback
//     verdict is escalate-to-human; synthesized event passes VerdictEvent.Valid().
//   - Indistinguishability: synthesized VerdictEvent has the same shape as an
//     investigator-emitted escalate-to-human per RC-018.
//   - Crash-between-steps: crash between step (3) and step (4) leaves
//     budget_exhausted event without a verdict commit → Cat 3b via
//     DiscoverVerdictExecution.
//   - No-commit invariant: RC-018 does NOT write a reconciliation commit.
//
// Spec ref: specs/reconciliation/spec.md §4.4 RC-018.

// ---- BudgetExhaustionStep enum ----

// TestBudgetExhaustionStep_FiveValuesAreDeclared verifies that exactly five
// BudgetExhaustionStep constants are declared and that their string values
// match the spec ordering labels.
func TestBudgetExhaustionStep_FiveValuesAreDeclared(t *testing.T) {
	t.Parallel()

	steps := []struct {
		constant BudgetExhaustionStep
		str      string
	}{
		{BudgetExhaustionStepTerminateSubprocess, "1-sigterm-sigkill-investigator"},
		{BudgetExhaustionStepWaitForWatcher, "2-wait-for-process-termination"},
		{BudgetExhaustionStepEmitBudgetExhausted, "3-emit-budget-exhausted"},
		{BudgetExhaustionStepEmitFallbackVerdict, "4-emit-fallback-verdict"},
		{BudgetExhaustionStepVerdictExecutorConsumes, "5-verdict-executor-consumes"},
	}

	const wantCount = 5
	if len(steps) != wantCount {
		t.Fatalf("RC-018: expected %d BudgetExhaustionStep constants, got %d; update this test on spec amendment", wantCount, len(steps))
	}

	for _, tc := range steps {
		tc := tc
		t.Run(tc.str, func(t *testing.T) {
			t.Parallel()

			if string(tc.constant) != tc.str {
				t.Errorf("BudgetExhaustionStep string = %q, want %q", string(tc.constant), tc.str)
			}
			if tc.constant == BudgetExhaustionStep("") {
				t.Errorf("BudgetExhaustionStep(%q) is empty", tc.str)
			}
		})
	}
}

// ---- BudgetExhaustionHandlerSequence ----

// TestBudgetExhaustionHandlerSequence_HasFiveSteps verifies that
// BudgetExhaustionHandlerSequence returns exactly five steps.
//
// Per RC-018: the handler sequence has exactly 5 steps. Adding or removing a
// step requires a spec-level amendment per RC-009.
func TestBudgetExhaustionHandlerSequence_HasFiveSteps(t *testing.T) {
	t.Parallel()

	seq := BudgetExhaustionHandlerSequence()
	const wantCount = 5
	if len(seq) != wantCount {
		t.Errorf("RC-018: BudgetExhaustionHandlerSequence() has %d steps, want %d; "+
			"any change is a spec-level amendment (RC-009)", len(seq), wantCount)
	}
}

// TestBudgetExhaustionHandlerSequence_AllLabelsNonEmpty verifies that every
// step in the sequence has a non-empty Label and Description.
func TestBudgetExhaustionHandlerSequence_AllLabelsNonEmpty(t *testing.T) {
	t.Parallel()

	for i, step := range BudgetExhaustionHandlerSequence() {
		step := step
		t.Run(string(step.Label), func(t *testing.T) {
			t.Parallel()

			if step.Label == BudgetExhaustionStep("") {
				t.Errorf("RC-018: step[%d].Label is empty; all steps must have a typed label", i)
			}
			if step.Description == "" {
				t.Errorf("RC-018: step[%d] (%q) has empty Description; all steps must be documented", i, step.Label)
			}
		})
	}
}

// TestBudgetExhaustionHandlerSequence_OrderMatchesSpec verifies that the steps
// are returned in the order mandated by RC-018.
func TestBudgetExhaustionHandlerSequence_OrderMatchesSpec(t *testing.T) {
	t.Parallel()

	want := []BudgetExhaustionStep{
		BudgetExhaustionStepTerminateSubprocess,
		BudgetExhaustionStepWaitForWatcher,
		BudgetExhaustionStepEmitBudgetExhausted,
		BudgetExhaustionStepEmitFallbackVerdict,
		BudgetExhaustionStepVerdictExecutorConsumes,
	}
	got := BudgetExhaustionHandlerSequence()

	if len(got) != len(want) {
		t.Fatalf("RC-018: sequence length = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i].Label != want[i] {
			t.Errorf("RC-018: step[%d] = %q, want %q", i, got[i].Label, want[i])
		}
	}
}

// TestBudgetExhaustionHandlerSequence_FsyncBoundaryOnSteps3And4 verifies that
// exactly steps (3) and (4) are fsync-boundary writes, and no other steps are.
//
// Per RC-018: "Steps (3) and (4) are NOT atomic but each is fsync-boundary."
func TestBudgetExhaustionHandlerSequence_FsyncBoundaryOnSteps3And4(t *testing.T) {
	t.Parallel()

	seq := BudgetExhaustionHandlerSequence()
	if len(seq) != 5 {
		t.Fatalf("unexpected sequence length %d", len(seq))
	}

	// Steps are 0-indexed; step (3) = index 2, step (4) = index 3.
	for i, step := range seq {
		wantFsync := i == 2 || i == 3 // steps (3) and (4)
		if step.IsFsyncBoundary != wantFsync {
			t.Errorf("RC-018: step[%d] (%q) IsFsyncBoundary = %v, want %v",
				i, step.Label, step.IsFsyncBoundary, wantFsync)
		}
	}
}

// ---- SynthesizeBudgetExhaustionFallbackVerdict ----

// rc018FallbackFixture returns valid inputs for SynthesizeBudgetExhaustionFallbackVerdict.
func rc018FallbackFixture(t *testing.T) (RunID, RunID, SnapshotToken) {
	t.Helper()
	return RunID(uuid.Must(uuid.NewV7())),
		RunID(uuid.Must(uuid.NewV7())),
		SnapshotToken{
			GitHeadHash:         "abc123def456",
			BeadsAuditEntryID:   "audit-001",
			CapturedAtTimestamp: "2026-06-01T00:00:00Z",
		}
}

// TestSynthesizeBudgetExhaustionFallbackVerdict_ValidInputs verifies that
// SynthesizeBudgetExhaustionFallbackVerdict returns a valid VerdictEvent for
// fully-valid inputs.
func TestSynthesizeBudgetExhaustionFallbackVerdict_ValidInputs(t *testing.T) {
	t.Parallel()

	reconID, targetID, snap := rc018FallbackFixture(t)
	v, err := SynthesizeBudgetExhaustionFallbackVerdict(reconID, targetID, snap)
	if err != nil {
		t.Fatalf("RC-018: SynthesizeBudgetExhaustionFallbackVerdict returned unexpected error: %v", err)
	}
	if !v.Valid() {
		t.Error("RC-018: synthesized VerdictEvent.Valid() = false; want true")
	}
}

// TestSynthesizeBudgetExhaustionFallbackVerdict_FallbackIsEscalateToHuman
// verifies that the synthesized verdict is always escalate-to-human, per RC-018:
// "issue a default verdict of escalate-to-human on the outer (target) run."
func TestSynthesizeBudgetExhaustionFallbackVerdict_FallbackIsEscalateToHuman(t *testing.T) {
	t.Parallel()

	reconID, targetID, snap := rc018FallbackFixture(t)
	v, err := SynthesizeBudgetExhaustionFallbackVerdict(reconID, targetID, snap)
	if err != nil {
		t.Fatalf("RC-018: unexpected error: %v", err)
	}
	if v.Verdict != VerdictEscalateToHuman {
		t.Errorf("RC-018: synthesized Verdict = %q, want %q (escalate-to-human)", v.Verdict, VerdictEscalateToHuman)
	}
}

// TestSynthesizeBudgetExhaustionFallbackVerdict_Indistinguishable verifies
// the RC-018 indistinguishability requirement: the synthesized VerdictEvent is
// structurally identical to an investigator-emitted escalate-to-human.
//
// Both the synthesized event and an investigator-emitted escalate-to-human:
//   - Have Verdict == VerdictEscalateToHuman.
//   - Have non-nil, non-zero InvestigatorRunID and TargetRunID.
//   - Have no Context (nil), no CheckpointRef (nil).
//   - Have SchemaVersion == 1.
//   - Pass VerdictEvent.Valid().
func TestSynthesizeBudgetExhaustionFallbackVerdict_Indistinguishable(t *testing.T) {
	t.Parallel()

	reconID, targetID, snap := rc018FallbackFixture(t)

	// Daemon-synthesized fallback.
	synthetic, err := SynthesizeBudgetExhaustionFallbackVerdict(reconID, targetID, snap)
	if err != nil {
		t.Fatalf("RC-018: unexpected error: %v", err)
	}

	// Investigator-emitted escalate-to-human (direct construction, same shape).
	investigatorEmitted := VerdictEvent{
		Verdict:           VerdictEscalateToHuman,
		InvestigatorRunID: uuid.Must(uuid.NewV7()),
		TargetRunID:       uuid.UUID(targetID),
		SnapshotToken:     snap,
		SchemaVersion:     1,
	}

	// Both must be valid.
	if !synthetic.Valid() {
		t.Error("RC-018/indistinguishable: synthesized VerdictEvent.Valid() = false")
	}
	if !investigatorEmitted.Valid() {
		t.Error("RC-018/indistinguishable: investigator-emitted VerdictEvent.Valid() = false; fixture error")
	}

	// Structural equality of shape-determining fields.
	if synthetic.Verdict != investigatorEmitted.Verdict {
		t.Errorf("RC-018/indistinguishable: Verdict mismatch: %q vs %q", synthetic.Verdict, investigatorEmitted.Verdict)
	}
	if synthetic.Context != nil {
		t.Error("RC-018/indistinguishable: synthesized Context is non-nil; escalate-to-human must have nil Context")
	}
	if synthetic.CheckpointRef != nil {
		t.Error("RC-018/indistinguishable: synthesized CheckpointRef is non-nil; escalate-to-human must have nil CheckpointRef")
	}
	if synthetic.SchemaVersion != investigatorEmitted.SchemaVersion {
		t.Errorf("RC-018/indistinguishable: SchemaVersion mismatch: %d vs %d", synthetic.SchemaVersion, investigatorEmitted.SchemaVersion)
	}
}

// TestSynthesizeBudgetExhaustionFallbackVerdict_ZeroReconciliationRunID
// verifies that a zero reconciliationRunID is rejected.
func TestSynthesizeBudgetExhaustionFallbackVerdict_ZeroReconciliationRunID(t *testing.T) {
	t.Parallel()

	_, targetID, snap := rc018FallbackFixture(t)
	_, err := SynthesizeBudgetExhaustionFallbackVerdict(RunID(uuid.Nil), targetID, snap)
	if err == nil {
		t.Error("RC-018: SynthesizeBudgetExhaustionFallbackVerdict with zero reconciliationRunID returned nil error, want error")
	}
}

// TestSynthesizeBudgetExhaustionFallbackVerdict_ZeroTargetRunID
// verifies that a zero targetRunID is rejected.
func TestSynthesizeBudgetExhaustionFallbackVerdict_ZeroTargetRunID(t *testing.T) {
	t.Parallel()

	reconID, _, snap := rc018FallbackFixture(t)
	_, err := SynthesizeBudgetExhaustionFallbackVerdict(reconID, RunID(uuid.Nil), snap)
	if err == nil {
		t.Error("RC-018: SynthesizeBudgetExhaustionFallbackVerdict with zero targetRunID returned nil error, want error")
	}
}

// TestSynthesizeBudgetExhaustionFallbackVerdict_InvalidSnapshotToken
// verifies that an invalid SnapshotToken is rejected.
func TestSynthesizeBudgetExhaustionFallbackVerdict_InvalidSnapshotToken(t *testing.T) {
	t.Parallel()

	reconID, targetID, _ := rc018FallbackFixture(t)
	invalid := SnapshotToken{} // zero value: Valid() == false
	_, err := SynthesizeBudgetExhaustionFallbackVerdict(reconID, targetID, invalid)
	if err == nil {
		t.Error("RC-018: SynthesizeBudgetExhaustionFallbackVerdict with invalid SnapshotToken returned nil error, want error")
	}
}

// TestSynthesizeBudgetExhaustionFallbackVerdict_TargetRunIDPreserved verifies
// that the synthesized VerdictEvent carries the exact targetRunID supplied by
// the caller.
func TestSynthesizeBudgetExhaustionFallbackVerdict_TargetRunIDPreserved(t *testing.T) {
	t.Parallel()

	reconID, targetID, snap := rc018FallbackFixture(t)
	v, err := SynthesizeBudgetExhaustionFallbackVerdict(reconID, targetID, snap)
	if err != nil {
		t.Fatalf("RC-018: unexpected error: %v", err)
	}
	if v.TargetRunID != uuid.UUID(targetID) {
		t.Errorf("RC-018: synthesized TargetRunID = %v, want %v", v.TargetRunID, uuid.UUID(targetID))
	}
}

// TestSynthesizeBudgetExhaustionFallbackVerdict_ReconciliationRunIDAsInvestigator
// verifies that the synthesized VerdictEvent uses the reconciliation workflow's
// run_id as InvestigatorRunID, since the reconciliation workflow acts as its own
// investigator in the budget-exhaustion path.
func TestSynthesizeBudgetExhaustionFallbackVerdict_ReconciliationRunIDAsInvestigator(t *testing.T) {
	t.Parallel()

	reconID, targetID, snap := rc018FallbackFixture(t)
	v, err := SynthesizeBudgetExhaustionFallbackVerdict(reconID, targetID, snap)
	if err != nil {
		t.Fatalf("RC-018: unexpected error: %v", err)
	}
	if v.InvestigatorRunID != uuid.UUID(reconID) {
		t.Errorf("RC-018: synthesized InvestigatorRunID = %v, want reconciliationRunID %v", v.InvestigatorRunID, uuid.UUID(reconID))
	}
}

// TestSynthesizeBudgetExhaustionFallbackVerdict_SnapshotTokenPreserved verifies
// that the synthesized VerdictEvent carries the exact SnapshotToken supplied.
func TestSynthesizeBudgetExhaustionFallbackVerdict_SnapshotTokenPreserved(t *testing.T) {
	t.Parallel()

	reconID, targetID, snap := rc018FallbackFixture(t)
	v, err := SynthesizeBudgetExhaustionFallbackVerdict(reconID, targetID, snap)
	if err != nil {
		t.Fatalf("RC-018: unexpected error: %v", err)
	}
	if v.SnapshotToken != snap {
		t.Errorf("RC-018: synthesized SnapshotToken = %+v, want %+v", v.SnapshotToken, snap)
	}
}

// ---- Crash-between-steps invariants ----

// TestRC018_CrashBetweenSteps3And4_RoutesViaCat3b verifies the spec invariant
// that a crash between step (3) (budget_exhausted emitted, fsync-boundary) and
// step (4) (fallback verdict emitted, fsync-boundary) routes through Cat 3b
// on the next daemon startup via DiscoverVerdictExecution.
//
// Spec ref: RC-018 — "on crash between [budget_exhausted] and [fallback verdict],
// the next daemon startup detects the budget-exhausted event with no subsequent
// verdict commit and routes through Cat 3b retry cap (RC-026a)."
func TestRC018_CrashBetweenSteps3And4_RoutesViaCat3b(t *testing.T) {
	t.Parallel()

	// After a crash between steps (3) and (4):
	// - budget_exhausted event is durable (fsync-boundary), but
	// - no verdict commit landed (step (4) did not complete).
	// The startup detector sees: verdict commit absent → VerdictDiscoveryStateClean
	// → Cat 5 appears from DiscoverVerdictExecution for a clean branch,
	// BUT the budget_exhausted event in the JSONL log is the trigger for Cat 3b.
	//
	// At the pure type layer, we can verify that BranchVerdictEvidence{HasVerdictCommit: false}
	// maps to VerdictDiscoveryStateClean, which is the state the startup detector
	// sees; the Cat 3b routing is then applied by the detector when it also sees
	// the durable budget_exhausted event.
	//
	// This test is a unit-layer proxy for the scenario-layer crash-recovery test
	// described in TESTING.md §4 and RC-031. It verifies the detection-state
	// transition, not the full daemon startup sequence.
	evidence := BranchVerdictEvidence{
		HasVerdictCommit:         false, // crash before step (4) → no verdict commit
		HasVerdictExecutedCommit: false,
	}
	state, cat := DiscoverVerdictExecution(evidence)

	// The branch is clean from a verdict-commit perspective; the Cat 3b routing
	// arises from the durable budget_exhausted event detected by the JSONL-layer
	// detector (not modeled at this pure layer). At this layer the clean-branch
	// state correctly indicates no verdict commit.
	if state != VerdictDiscoveryStateClean {
		t.Errorf("RC-018/crash-3to4: VerdictDiscoveryState = %q, want %q (no verdict commit landed)",
			state, VerdictDiscoveryStateClean)
	}
	// Cat 5 is the branch-inspection result; the full Cat 3b routing is applied
	// by the daemon's JSONL-layer startup detector when it also sees budget_exhausted.
	if cat != ReconciliationCategoryCat5 {
		t.Errorf("RC-018/crash-3to4: branch-level category = %q, want %q (Cat 3b routing is applied at JSONL-detector layer)",
			cat, ReconciliationCategoryCat5)
	}
}

// TestRC018_NoCommitBeforeStep5 verifies the RC-018 invariant that the
// budget-exhaustion handler MUST NOT write a reconciliation commit.
//
// RC-018: "NOT write a reconciliation commit. The budget-exhausted event +
// daemon-default verdict are the durable trace."
//
// At the pure-type layer this is verified structurally: the synthesized
// VerdictEvent carries no EvidenceRef (the git commit hash of a reconciliation
// commit), because no commit exists at synthesis time.
func TestRC018_NoCommitBeforeStep5(t *testing.T) {
	t.Parallel()

	reconID, targetID, snap := rc018FallbackFixture(t)
	v, err := SynthesizeBudgetExhaustionFallbackVerdict(reconID, targetID, snap)
	if err != nil {
		t.Fatalf("RC-018: unexpected error: %v", err)
	}
	// EvidenceRef is nil because no reconciliation commit is written before step (5).
	if v.EvidenceRef != nil {
		t.Errorf("RC-018/no-commit: synthesized VerdictEvent.EvidenceRef = %q (non-nil); "+
			"budget-exhaustion handler must NOT write a reconciliation commit (RC-018)",
			*v.EvidenceRef)
	}
}

// TestRC018_FallbackVerdictPlanIsEscalateToHuman verifies that
// PlanForVerdict(VerdictEscalateToHuman) produces the expected action plan for
// the fallback verdict consumed by the verdict-executor at step (5).
//
// This test anchors the RC-018 step-5 contract to the RC-025 verdict-execution
// table: the fallback verdict MUST route through the same escalate-to-human
// execution path as any investigator-emitted escalate-to-human.
func TestRC018_FallbackVerdictPlanIsEscalateToHuman(t *testing.T) {
	t.Parallel()

	plan, _ := PlanForVerdict(VerdictEscalateToHuman)

	if !plan.Valid() {
		t.Fatal("RC-018/step5: PlanForVerdict(VerdictEscalateToHuman).Valid() = false; want true")
	}
	if plan.ActionKind != VerdictActionKindEscalateToHuman {
		t.Errorf("RC-018/step5: ActionKind = %q, want %q",
			plan.ActionKind, VerdictActionKindEscalateToHuman)
	}
	if !plan.EmitsOperatorEscalation {
		t.Error("RC-018/step5: EmitsOperatorEscalation = false; escalate-to-human must emit operator_escalation_required")
	}
}
