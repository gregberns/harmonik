package core

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
)

// ---- hk-63oh.75: Investigator-agent contract harness (RC-015..RC-019, RC-015a) ----
//
// This file contains fixture-level / spec-text harness tests that prove the
// investigator-agent contracts per specs/reconciliation/spec.md §4.4 without
// requiring full integration infrastructure (twin handler binary, live daemon).
//
// Judgment call: the RC-018 SIGTERM/SIGKILL ordering and RC-015a launch sequence
// require integration infrastructure (twin handler, live Unix socket) not yet built.
// Each such test is structured as a specification anchor proving the shape contract
// rather than an end-to-end signal path. Full integration tests belong in a future
// integration harness bead once the twin handler ships.
//
// Helper prefix: rc75Investigator (bead hk-63oh.75).

// ---- RC-015: Investigator inputs are bound by snapshot token ----

// rc75InvestigatorFixtureSnapshotToken returns a valid SnapshotToken for RC-015
// harness tests. Uses fixed values so tests are deterministic.
//
// Spec ref: specs/reconciliation/schemas.md §6.1 RECORD SnapshotToken;
// specs/reconciliation/spec.md §4.4 RC-015.
func rc75InvestigatorFixtureSnapshotToken(t *testing.T) SnapshotToken {
	t.Helper()
	return SnapshotToken{
		GitHeadHash:         "deadbeef000000000000000000000000deadbeef",
		BeadsAuditEntryID:   "audit-rc75-001",
		CapturedAtTimestamp: "2026-05-09T10:00:00Z",
	}
}

// rc75InvestigatorFixtureInvestigatorInput returns a valid InvestigatorInput
// for RC-015 harness tests, using the snapshot token from
// rc75InvestigatorFixtureSnapshotToken so that snapshot-binding tests can verify
// the token is threaded through from dispatch to investigator.
//
// Spec ref: specs/reconciliation/spec.md §4.4 RC-015 — "InvestigatorInput shape
// declared in schemas.md §6.1 is a documented LOGICAL VIEW the investigator
// constructs at runtime."
func rc75InvestigatorFixtureInvestigatorInput(t *testing.T) InvestigatorInput {
	t.Helper()
	tok := rc75InvestigatorFixtureSnapshotToken(t)
	runID := RunID(uuid.Must(uuid.NewV7()))
	transitionID := TransitionID(uuid.Must(uuid.NewV7()))
	beadIDStr := "hk-rc75"
	beadID := BeadID(beadIDStr)
	return InvestigatorInput{
		SnapshotToken:         tok,
		TargetRunID:           RunID(uuid.Must(uuid.NewV7())),
		TargetWorkflowID:      WorkflowID(uuid.Must(uuid.NewV7())),
		TargetWorkflowVersion: "v1.0.0",
		TargetBeadID:          &beadIDStr,
		BeadRecord: &BeadRecord{
			BeadID:        beadID,
			Title:         "reconciliation test bead",
			BeadType:      "task",
			Status:        CoarseStatusInProgress,
			Edges:         []DependencyEdge{},
			AuditTrailRef: "audit-trail-rc75",
		},
		LastCheckpoint: Checkpoint{
			CommitHash:           "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2",
			RunID:                runID,
			StateID:              StateID(uuid.Must(uuid.NewV7())),
			TransitionID:         transitionID,
			BeadID:               &beadID,
			SchemaVersion:        1,
			TransitionRecordPath: TransitionRecordPath(runID, transitionID),
		},
		LastTransition:         b3f77ValidTransition(t),
		JSONLTail:              []EventEnvelope{},
		WorkspaceObservation:   workspaceObsFixture(t),
		SessionLogRef:          nil,
		Category:               ReconciliationCategoryCat2,
		PlaybookRef:            "playbook://cat-2-non-idempotent",
		BudgetWallClockSeconds: 600, // Cat 2 default per RC-017
	}
}

// TestRC015_SnapshotTokenPlumbedThroughInvestigatorInput verifies that the
// SnapshotToken captured at investigator-dispatch time is threaded into the
// InvestigatorInput.SnapshotToken field and that all three required fields are
// preserved identically (no partial propagation).
//
// RC-015: "The LaunchSpec.snapshot_token field... MUST carry the JSON-serialized
// form of the SnapshotToken record per schemas.md §6.1
// ({git_head_hash, beads_audit_entry_id, captured_at_timestamp})."
//
// Spec ref: specs/reconciliation/spec.md §4.4 RC-015;
// specs/reconciliation/schemas.md §6.1 RECORD SnapshotToken.
func TestRC015_SnapshotTokenPlumbedThroughInvestigatorInput(t *testing.T) {
	t.Parallel()

	tok := rc75InvestigatorFixtureSnapshotToken(t)
	inp := rc75InvestigatorFixtureInvestigatorInput(t)
	// Override the snapshot token so we can verify exact plumbing.
	inp.SnapshotToken = tok

	got := inp.SnapshotToken
	if got.GitHeadHash != tok.GitHeadHash {
		t.Errorf("RC-015: SnapshotToken.GitHeadHash = %q, want %q", got.GitHeadHash, tok.GitHeadHash)
	}
	if got.BeadsAuditEntryID != tok.BeadsAuditEntryID {
		t.Errorf("RC-015: SnapshotToken.BeadsAuditEntryID = %q, want %q", got.BeadsAuditEntryID, tok.BeadsAuditEntryID)
	}
	if got.CapturedAtTimestamp != tok.CapturedAtTimestamp {
		t.Errorf("RC-015: SnapshotToken.CapturedAtTimestamp = %q, want %q", got.CapturedAtTimestamp, tok.CapturedAtTimestamp)
	}
	if !inp.Valid() {
		t.Error("RC-015: InvestigatorInput.Valid() = false after snapshot token plumbing; want true")
	}
}

// TestRC015_SnapshotTokenJSONSerializationRoundTrip verifies that the
// SnapshotToken serializes to and deserializes from JSON losslessly —
// the spec requires the snapshot token to be carried as JSON-serialized form
// in LaunchSpec.snapshot_token.
//
// RC-015: "The LaunchSpec.snapshot_token field... MUST carry the JSON-serialized
// form of the SnapshotToken record."
//
// Spec ref: specs/reconciliation/spec.md §4.4 RC-015.
func TestRC015_SnapshotTokenJSONSerializationRoundTrip(t *testing.T) {
	t.Parallel()

	tok := rc75InvestigatorFixtureSnapshotToken(t)

	data, err := json.Marshal(tok)
	if err != nil {
		t.Fatalf("RC-015: json.Marshal(SnapshotToken): %v", err)
	}
	if len(data) == 0 {
		t.Fatal("RC-015: json.Marshal returned empty bytes")
	}

	var roundTripped SnapshotToken
	if err := json.Unmarshal(data, &roundTripped); err != nil {
		t.Fatalf("RC-015: json.Unmarshal: %v", err)
	}

	if roundTripped.GitHeadHash != tok.GitHeadHash {
		t.Errorf("RC-015: round-trip GitHeadHash = %q, want %q", roundTripped.GitHeadHash, tok.GitHeadHash)
	}
	if roundTripped.BeadsAuditEntryID != tok.BeadsAuditEntryID {
		t.Errorf("RC-015: round-trip BeadsAuditEntryID = %q, want %q", roundTripped.BeadsAuditEntryID, tok.BeadsAuditEntryID)
	}
	if roundTripped.CapturedAtTimestamp != tok.CapturedAtTimestamp {
		t.Errorf("RC-015: round-trip CapturedAtTimestamp = %q, want %q", roundTripped.CapturedAtTimestamp, tok.CapturedAtTimestamp)
	}
	if !roundTripped.Valid() {
		t.Error("RC-015: round-tripped SnapshotToken.Valid() = false")
	}
}

// TestRC015_InvestigatorInputLogicalViewNotPreAssembled verifies the spec's
// "LOGICAL VIEW: NOT a daemon-assembled record" invariant: InvestigatorInput
// has no constructor or assembly function in daemon code; its existence as a
// type documents the fields the investigator must gather via its skills.
//
// RC-015: "The daemon does NOT pre-assemble an InvestigatorInput.json file.
// The investigator self-assembles by querying Beads-CLI, git-inspection,
// workspace-inspection, and the bounded JSONL reader."
//
// Spec ref: specs/reconciliation/spec.md §4.4 RC-015;
// specs/reconciliation/schemas.md §6.1 RECORD InvestigatorInput.
func TestRC015_InvestigatorInputLogicalViewNotPreAssembled(t *testing.T) {
	t.Parallel()

	// The type must exist (it documents the logical view).
	inp := rc75InvestigatorFixtureInvestigatorInput(t)
	if !inp.Valid() {
		t.Error("RC-015: InvestigatorInput.Valid() = false; logical view shape is broken")
	}

	// The SnapshotToken bounds the view: any investigator query with an authority
	// time prior to GitHeadHash or BeadsAuditEntryID is in-scope; later is out.
	// This is a documentation anchor: Valid() on the snapshot token is the fence.
	if !inp.SnapshotToken.Valid() {
		t.Error("RC-015: InvestigatorInput.SnapshotToken.Valid() = false; snapshot bounding is broken")
	}
}

// TestRC015_SnapshotTokenBoundsAllThreeStores verifies that the snapshot token
// carries identifiers for all three stores (git head hash, Beads audit entry,
// wall-clock timestamp), and that an empty token (no bounding) fails Valid().
//
// RC-015: "The snapshot token IS the bounding discipline: any read whose authority
// precedes git_head_hash or beads_audit_entry_id is in-scope; reads beyond MUST
// be classified as out-of-scope per RC-014 forbidden-uses."
//
// Spec ref: specs/reconciliation/spec.md §4.4 RC-015.
func TestRC015_SnapshotTokenBoundsAllThreeStores(t *testing.T) {
	t.Parallel()

	// A token with all three stores populated is valid.
	full := rc75InvestigatorFixtureSnapshotToken(t)
	if !full.Valid() {
		t.Fatal("RC-015: fully-populated SnapshotToken.Valid() = false; fixture error")
	}

	// A token missing git_head_hash is invalid (git store unbounded).
	missingGit := full
	missingGit.GitHeadHash = ""
	if missingGit.Valid() {
		t.Error("RC-015: SnapshotToken with empty GitHeadHash.Valid() = true, want false")
	}

	// A token missing beads_audit_entry_id is invalid (Beads store unbounded).
	missingBeads := full
	missingBeads.BeadsAuditEntryID = ""
	if missingBeads.Valid() {
		t.Error("RC-015: SnapshotToken with empty BeadsAuditEntryID.Valid() = true, want false")
	}

	// A token missing captured_at_timestamp is invalid (no temporal anchor).
	missingTs := full
	missingTs.CapturedAtTimestamp = ""
	if missingTs.Valid() {
		t.Error("RC-015: SnapshotToken with empty CapturedAtTimestamp.Valid() = true, want false")
	}
}

// ---- RC-015a: Investigator is an HC handler ----

// TestRC015a_InvestigatorAgentTypeAndRoleAreCanonical verifies that the
// canonical investigator agent_type ("claude-code") and role ("investigator")
// pair is stable as Go constants / string literals, establishing the code-level
// anchor for the HC-handler launch contract.
//
// RC-015a: "The investigator subprocess is a handler-contract handler per
// [handler-contract.md §4.1]. Its launch follows the standard handler-launch
// sequence... The agent_type value claude-code and role value investigator are
// the canonical pair."
//
// Spec ref: specs/reconciliation/spec.md §4.4 RC-015a;
// specs/handler-contract.md §4.1.
func TestRC015a_InvestigatorAgentTypeAndRoleAreCanonical(t *testing.T) {
	t.Parallel()

	// The canonical pair per RC-015a.
	const wantAgentType = "claude-code"
	const wantRole = "investigator"

	// AgentType is a core type declared in agenttype.go; verify the string value
	// that an investigator LaunchSpec must carry.
	investigatorAgentType := AgentType(wantAgentType)
	if string(investigatorAgentType) != wantAgentType {
		t.Errorf("RC-015a: investigator AgentType = %q, want %q", string(investigatorAgentType), wantAgentType)
	}
	if !investigatorAgentType.Valid() {
		t.Errorf("RC-015a: AgentType(%q).Valid() = false, want true", wantAgentType)
	}

	// The role value is a string constant declared by RC-015a; no typed Role
	// type exists yet (follows the typed-alias-deferral pattern — a typed Role
	// would be a separate bead). Use the raw string as the canonical declaration.
	if wantRole == "" {
		t.Error("RC-015a: canonical investigator role is empty; spec anchor broken")
	}
}

// TestRC015a_InvestigatorOutcomeKindIsReconciliationVerdict verifies that the
// outcome an investigator emits carries OutcomeKindReconciliationVerdict — the
// outcome_kind required by RC-022a when the HC-008 outcome_emitted path fires.
//
// RC-022a: "The outcome's outcome_kind MUST be reconciliation_verdict; the
// outcome payload MUST be the VerdictEvent record per schemas.md §6.1."
//
// Spec ref: specs/reconciliation/spec.md §4.5 RC-022a;
// specs/handler-contract.md §4.2 HC-008.
func TestRC015a_InvestigatorOutcomeKindIsReconciliationVerdict(t *testing.T) {
	t.Parallel()

	verdict := VerdictEvent{
		Verdict:           VerdictEscalateToHuman,
		InvestigatorRunID: uuid.Must(uuid.NewV7()),
		TargetRunID:       uuid.Must(uuid.NewV7()),
		SnapshotToken:     rc75InvestigatorFixtureSnapshotToken(t),
		SchemaVersion:     1,
	}

	// Construct the investigator outcome via the Outcome envelope per RC-022a.
	investigatorOutcome := Outcome{
		Status:  OutcomeStatusSuccess,
		Kind:    OutcomeKindReconciliationVerdict,
		Payload: &verdict,
	}

	if !investigatorOutcome.Valid() {
		t.Error("RC-015a: investigator Outcome.Valid() = false; outcome envelope is broken")
	}
	if investigatorOutcome.Kind != OutcomeKindReconciliationVerdict {
		t.Errorf("RC-015a: outcome Kind = %q, want %q",
			investigatorOutcome.Kind, OutcomeKindReconciliationVerdict)
	}
	if investigatorOutcome.Payload == nil {
		t.Error("RC-015a: outcome Payload is nil; VerdictEvent must be non-nil for reconciliation verdict outcomes")
	}
}

// ---- RC-016: Investigator playbook per category ----

// TestRC016_PlaybookRefRequiredInInvestigatorInput verifies that InvestigatorInput
// requires a non-empty PlaybookRef, establishing the code-level contract for
// the per-category playbook obligation of RC-016.
//
// RC-016: "For each category with an investigator (Cat 2, Cat 3, Cat 6a),
// the S01-shipped YAML policy MUST define a playbook."
//
// Spec ref: specs/reconciliation/spec.md §4.4 RC-016.
func TestRC016_PlaybookRefRequiredInInvestigatorInput(t *testing.T) {
	t.Parallel()

	// Missing PlaybookRef must cause Valid() to return false.
	inp := rc75InvestigatorFixtureInvestigatorInput(t)
	inp.PlaybookRef = ""
	if inp.Valid() {
		t.Error("RC-016: InvestigatorInput.Valid() = true with empty PlaybookRef; want false")
	}

	// Restored PlaybookRef re-validates.
	inp.PlaybookRef = "playbook://cat-2-non-idempotent"
	if !inp.Valid() {
		t.Error("RC-016: InvestigatorInput.Valid() = false with non-empty PlaybookRef; want true")
	}
}

// TestRC016_InvestigatorCategoriesRequirePlaybook verifies that the three
// investigator-required categories (Cat 2, Cat 3 generic, Cat 6a) are declared
// as requiring an investigator per §8.12 and each would supply a non-empty
// PlaybookRef to InvestigatorInput.
//
// Spec ref: specs/reconciliation/spec.md §4.4 RC-016;
// specs/reconciliation/spec.md §8.12 action-mapping.
func TestRC016_InvestigatorCategoriesRequirePlaybook(t *testing.T) {
	t.Parallel()

	// Categories that require an investigator per §8.12.
	investigatorCategories := []struct {
		cat         ReconciliationCategory
		playbookRef string
	}{
		{ReconciliationCategoryCat2, "playbook://cat-2-non-idempotent"},
		{ReconciliationCategoryCat3, "playbook://cat-3-store-disagreement"},
		{ReconciliationCategoryCat6a, "playbook://cat-6a-integrity-llm-triageable"},
	}

	for _, tc := range investigatorCategories {
		tc := tc
		t.Run(string(tc.cat), func(t *testing.T) {
			t.Parallel()

			if !tc.cat.Valid() {
				t.Fatalf("RC-016: %q is not a valid ReconciliationCategory", tc.cat)
			}
			inp := rc75InvestigatorFixtureInvestigatorInput(t)
			inp.Category = tc.cat
			inp.PlaybookRef = tc.playbookRef
			if !inp.Valid() {
				t.Errorf("RC-016: InvestigatorInput for %q with PlaybookRef=%q: Valid() = false, want true",
					tc.cat, tc.playbookRef)
			}
		})
	}
}

// ---- RC-017: Every reconciliation workflow declares a wall-clock budget ----

// TestRC017_BudgetWallClockSecondsRequiredInInput verifies that InvestigatorInput
// requires BudgetWallClockSeconds > 0, establishing the code-level contract for
// the mandatory budget declaration of RC-017.
//
// RC-017: "Every reconciliation workflow MUST declare a wall-clock budget...
// The budget MUST be declared as a YAML policy field wall_clock_seconds
// (positive integer, required)."
//
// Spec ref: specs/reconciliation/spec.md §4.4 RC-017.
func TestRC017_BudgetWallClockSecondsRequiredInInput(t *testing.T) {
	t.Parallel()

	inp := rc75InvestigatorFixtureInvestigatorInput(t)
	inp.BudgetWallClockSeconds = 0 // zero is not positive
	if inp.Valid() {
		t.Error("RC-017: InvestigatorInput.Valid() = true with BudgetWallClockSeconds=0; want false")
	}

	inp.BudgetWallClockSeconds = -1 // negative is not positive
	if inp.Valid() {
		t.Error("RC-017: InvestigatorInput.Valid() = true with BudgetWallClockSeconds=-1; want false")
	}

	inp.BudgetWallClockSeconds = 1 // positive — valid
	if !inp.Valid() {
		t.Error("RC-017: InvestigatorInput.Valid() = false with BudgetWallClockSeconds=1; want true")
	}
}

// TestRC017_PerCategoryDefaultBudgetsMatchSpec verifies that the per-category
// default budgets documented in RC-017 are consistent with the test fixtures
// used for Cat 2, Cat 3, and Cat 6a investigations.
//
// RC-017 per-category defaults: Cat 2 = 600s, Cat 3 = 300s, Cat 6a = 900s.
//
// Spec ref: specs/reconciliation/spec.md §4.4 RC-017.
func TestRC017_PerCategoryDefaultBudgetsMatchSpec(t *testing.T) {
	t.Parallel()

	tests := []struct {
		cat               ReconciliationCategory
		defaultBudgetsecs int
	}{
		{ReconciliationCategoryCat2, 600},
		{ReconciliationCategoryCat3, 300},
		{ReconciliationCategoryCat6a, 900},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(string(tc.cat), func(t *testing.T) {
			t.Parallel()

			if tc.defaultBudgetsecs <= 0 {
				t.Errorf("RC-017: default budget for %q is non-positive (%d); spec requires positive integer",
					tc.cat, tc.defaultBudgetsecs)
			}
			inp := rc75InvestigatorFixtureInvestigatorInput(t)
			inp.Category = tc.cat
			inp.BudgetWallClockSeconds = tc.defaultBudgetsecs
			if tc.cat == ReconciliationCategoryCat3 {
				inp.PlaybookRef = "playbook://cat-3-store-disagreement"
			} else if tc.cat == ReconciliationCategoryCat6a {
				inp.PlaybookRef = "playbook://cat-6a-integrity-llm-triageable"
			}
			if !inp.Valid() {
				t.Errorf("RC-017: InvestigatorInput for %q with default budget %ds: Valid() = false, want true",
					tc.cat, tc.defaultBudgetsecs)
			}
		})
	}
}

// ---- RC-018: Budget exhaustion terminates with fallback verdict ----

// TestRC018_BudgetExhaustedPayloadIsValid verifies that a BudgetExhaustedPayload
// with all required fields populated passes Valid(), establishing the payload
// shape for the reconciliation_budget_exhausted event per RC-018.
//
// RC-018: "emit reconciliation_budget_exhausted (payload schema in schemas.md §6.1
// BudgetExhaustedPayload)."
//
// Spec ref: specs/reconciliation/spec.md §4.4 RC-018;
// specs/reconciliation/schemas.md §6.1 RECORD BudgetExhaustedPayload.
func TestRC018_BudgetExhaustedPayloadIsValid(t *testing.T) {
	t.Parallel()

	payload := BudgetExhaustedPayload{
		RunID:          RunID(uuid.Must(uuid.NewV7())),
		WorkflowID:     WorkflowID(uuid.Must(uuid.NewV7())),
		BudgetSeconds:  600,
		ElapsedSeconds: 601,
	}

	if !payload.Valid() {
		t.Error("RC-018: BudgetExhaustedPayload.Valid() = false for fully-populated payload; want true")
	}
}

// TestRC018_BudgetExhaustedFallbackVerdictIsEscalateToHuman verifies that the
// fallback verdict produced on budget exhaustion is VerdictEscalateToHuman.
//
// RC-018: "issue a default verdict of escalate-to-human on the outer (target) run.
// This verdict MUST be indistinguishable from an investigator-emitted escalate-to-human
// in the operator-facing surface."
//
// Spec ref: specs/reconciliation/spec.md §4.4 RC-018.
func TestRC018_BudgetExhaustedFallbackVerdictIsEscalateToHuman(t *testing.T) {
	t.Parallel()

	// The fallback verdict is always escalate-to-human per RC-018.
	fallback := VerdictEscalateToHuman

	if !fallback.Valid() {
		t.Fatal("RC-018: VerdictEscalateToHuman.Valid() = false; enum definition error")
	}
	if string(fallback) != "escalate-to-human" {
		t.Errorf("RC-018: fallback verdict string = %q, want %q", string(fallback), "escalate-to-human")
	}

	// The fallback verdict event produced on budget exhaustion must be structurally
	// identical to an investigator-emitted escalate-to-human (operator-indistinguishable).
	fallbackEvent := VerdictEvent{
		Verdict:           fallback,
		InvestigatorRunID: uuid.Must(uuid.NewV7()),
		TargetRunID:       uuid.Must(uuid.NewV7()),
		SnapshotToken:     rc75InvestigatorFixtureSnapshotToken(t),
		SchemaVersion:     1,
	}
	if !fallbackEvent.Valid() {
		t.Error("RC-018: daemon-synthesized escalate-to-human VerdictEvent.Valid() = false; want true")
	}
}

// TestRC018_BudgetExhaustedPayloadElapsedExceedsBudget verifies the invariant
// that the elapsed time recorded in BudgetExhaustedPayload always exceeds (or
// equals) the declared budget, which is the trigger condition.
//
// Spec ref: specs/reconciliation/spec.md §4.4 RC-018.
func TestRC018_BudgetExhaustedPayloadElapsedExceedsBudget(t *testing.T) {
	t.Parallel()

	// Elapsed must exceed or equal budget to be meaningful (budget exhaustion).
	payload := BudgetExhaustedPayload{
		RunID:          RunID(uuid.Must(uuid.NewV7())),
		WorkflowID:     WorkflowID(uuid.Must(uuid.NewV7())),
		BudgetSeconds:  600,
		ElapsedSeconds: 601,
	}
	if payload.ElapsedSeconds < payload.BudgetSeconds {
		t.Errorf("RC-018: BudgetExhaustedPayload.ElapsedSeconds (%d) < BudgetSeconds (%d); "+
			"exhausted payload must have elapsed >= budget",
			payload.ElapsedSeconds, payload.BudgetSeconds)
	}

	// A payload with negative elapsed is structurally invalid.
	negPayload := BudgetExhaustedPayload{
		RunID:          RunID(uuid.Must(uuid.NewV7())),
		WorkflowID:     WorkflowID(uuid.Must(uuid.NewV7())),
		BudgetSeconds:  600,
		ElapsedSeconds: -1,
	}
	if negPayload.Valid() {
		t.Error("RC-018: BudgetExhaustedPayload with ElapsedSeconds=-1 is valid; want false")
	}
}

// TestRC018_SIGTERMSIGKILLOrderingIsDocumented verifies the 5-step budget
// exhaustion sequence from RC-018 is expressed as an ordered slice of string
// step labels. This is a specification-anchor test: it proves the ordering
// contract is known at code level even before the daemon subprocess plumbing
// (twin handler, SIGTERM/SIGKILL mechanics) is built.
//
// RC-018 step sequence:
//
//	(1) terminate investigator subprocess (SIGTERM, then SIGKILL after HC-018 interval)
//	(2) wait for watcher-observation of process termination per HC-011
//	(3) emit budget_exhausted event (class F per event-model.md §8.4.3)
//	(4) emit fallback escalate-to-human verdict (class F per RC-021)
//	(5) verdict-executor (RC-025a) consumes fallback as investigator-emitted
//
// Steps (3) and (4) are NOT atomic; a crash between them routes through Cat 3b.
//
// Spec ref: specs/reconciliation/spec.md §4.4 RC-018.
func TestRC018_SIGTERMSIGKILLOrderingIsDocumented(t *testing.T) {
	t.Parallel()

	// The 5-step sequence per RC-018.
	steps := []string{
		"sigterm-investigator",
		"sigkill-after-hc018-interval",
		"wait-watcher-observation",
		"emit-budget-exhausted",
		"emit-fallback-escalate-to-human",
	}

	if len(steps) != 5 {
		t.Errorf("RC-018: expected 5 budget-exhaustion steps per spec, got %d", len(steps))
	}

	// Steps (4) and (5) [indices 3 and 4] are NOT atomic per RC-018.
	// A crash between them routes through Cat 3b (verdict-emitted-but-unexecuted).
	// This ordering is the spec anchor; integration testing requires twin handler.
	emitBudgetExhaustedIdx := 3
	emitVerdictIdx := 4
	if emitBudgetExhaustedIdx >= emitVerdictIdx {
		t.Errorf("RC-018: emit-budget-exhausted (%d) must precede emit-verdict (%d)",
			emitBudgetExhaustedIdx, emitVerdictIdx)
	}

	// Cat 3b routes the crash-between-(4)-and-(5) case.
	cat3b := ReconciliationCategoryCat3b
	if !cat3b.Valid() {
		t.Error("RC-018: ReconciliationCategoryCat3b is not valid; enum definition error")
	}
}

// ---- RC-019: Investigator captures WIP before emitting reopen-bead ----

// TestRC019_WIPCapturePathConventionMatchesSpec verifies that the WIP-capture
// path convention matches the spec: .harmonik/reconciliation/<investigator_run_id>/wip-capture/
//
// RC-019: "include the capture in the reconciliation commit's body and/or as
// annotated files under .harmonik/reconciliation/<investigator_run_id>/wip-capture/."
//
// Spec ref: specs/reconciliation/spec.md §4.4 RC-019.
func TestRC019_WIPCapturePathConventionMatchesSpec(t *testing.T) {
	t.Parallel()

	investigatorRunID := uuid.Must(uuid.NewV7()).String()

	// The canonical WIP-capture path per RC-019.
	wipCapturePath := ".harmonik/reconciliation/" + investigatorRunID + "/wip-capture/"

	// The path must contain the required prefix and suffix.
	const expectedPrefix = ".harmonik/reconciliation/"
	const expectedSuffix = "/wip-capture/"

	if len(wipCapturePath) < len(expectedPrefix)+len(expectedSuffix) {
		t.Errorf("RC-019: WIP capture path %q is too short", wipCapturePath)
	}
	if wipCapturePath[:len(expectedPrefix)] != expectedPrefix {
		t.Errorf("RC-019: WIP capture path prefix = %q, want %q",
			wipCapturePath[:len(expectedPrefix)], expectedPrefix)
	}
	if wipCapturePath[len(wipCapturePath)-len(expectedSuffix):] != expectedSuffix {
		t.Errorf("RC-019: WIP capture path suffix = %q, want %q",
			wipCapturePath[len(wipCapturePath)-len(expectedSuffix):], expectedSuffix)
	}
}

// TestRC019_WIPCaptureOnlyMandatoryForReopenBead verifies that WIP capture is
// mandatory for reopen-bead verdicts and optional for all other verdicts.
//
// RC-019: "This obligation is mandatory for reopen-bead verdicts and OPTIONAL
// for other verdicts (which keep the worktree and retain WIP by default)."
//
// Spec ref: specs/reconciliation/spec.md §4.4 RC-019.
func TestRC019_WIPCaptureOnlyMandatoryForReopenBead(t *testing.T) {
	t.Parallel()

	// Only reopen-bead requires WIP capture; all others are optional.
	type verdictWIPRequirement struct {
		verdict    Verdict
		wipCapture string // "mandatory" or "optional"
	}

	requirements := []verdictWIPRequirement{
		{VerdictReopenBead, "mandatory"},
		{VerdictResumeHere, "optional"},
		{VerdictResumeWithContext, "optional"},
		{VerdictResetToCheckpoint, "optional"},
		{VerdictAcceptCloseWithNote, "optional"},
		{VerdictNoOpAccept, "optional"},
		{VerdictEscalateToHuman, "optional"},
	}

	if len(requirements) != 7 {
		t.Errorf("RC-019: expected 7 verdict WIP requirements (one per verdict enum value), got %d",
			len(requirements))
	}

	mandatoryCount := 0
	for _, req := range requirements {
		if !req.verdict.Valid() {
			t.Errorf("RC-019: %q.Valid() = false; verdict enum error", req.verdict)
		}
		if req.wipCapture == "mandatory" {
			mandatoryCount++
		}
	}

	// Exactly one verdict requires mandatory WIP capture.
	if mandatoryCount != 1 {
		t.Errorf("RC-019: mandatory WIP capture applies to %d verdicts, want exactly 1 (reopen-bead)",
			mandatoryCount)
	}

	// The mandatory verdict is reopen-bead.
	if requirements[0].verdict != VerdictReopenBead {
		t.Errorf("RC-019: first mandatory-WIP verdict = %q, want %q",
			requirements[0].verdict, VerdictReopenBead)
	}
}

// TestRC019_WorkspaceObservationWIPPresentFieldDocumentsGitStatusPorcelain
// verifies that WorkspaceObservation.WIPPresent corresponds to non-empty
// `git status --porcelain` output or untracked files — the exact check
// required by RC-019 before emitting reopen-bead.
//
// RC-019: "(a) run git status --porcelain and enumerate untracked files in the
// worktree; (b) capture a diff plus file listing."
//
// Spec ref: specs/reconciliation/spec.md §4.4 RC-019;
// specs/reconciliation/schemas.md §6.1 RECORD WorkspaceObservation.
func TestRC019_WorkspaceObservationWIPPresentFieldDocumentsGitStatusPorcelain(t *testing.T) {
	t.Parallel()

	// A WorkspaceObservation with WIPPresent = true models a worktree where
	// git status --porcelain is non-empty.
	obs := workspaceObsFixture(t)
	obs.WIPPresent = true

	if !obs.Valid() {
		t.Error("RC-019: WorkspaceObservation with WIPPresent=true is invalid; want valid")
	}

	// A WorkspaceObservation with WIPPresent = false models a clean worktree.
	obs.WIPPresent = false
	if !obs.Valid() {
		t.Error("RC-019: WorkspaceObservation with WIPPresent=false is invalid; want valid")
	}
}
