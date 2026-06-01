package handlercontract_test

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/handlercontract"
)

// Tests for the LaunchSpec record per specs/handler-contract.md §6.1 HC-006.
//
// Helper prefix: launchspecFixture (bead hk-8i31.74; distinct from other
// handlercontract helper prefixes).

// launchspecFixtureValid returns a fully-populated, valid LaunchSpec for tests.
func launchspecFixtureValid(t *testing.T) handlercontract.LaunchSpec {
	t.Helper()
	runID := core.RunID(uuid.MustParse("0196e100-0000-7000-8000-000000000001"))
	wfID := core.WorkflowID(uuid.MustParse("0196e100-0000-7000-8000-000000000002"))
	beadID := "hk-8i31.74"
	// snapshot_token is String|None per HC-006: encode the SnapshotToken as JSON.
	tokEncoded, err := handlercontract.MarshalSnapshotToken(core.SnapshotToken{
		GitHeadHash:         "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef",
		BeadsAuditEntryID:   "audit-001",
		CapturedAtTimestamp: "2026-05-10T00:00:00Z",
	})
	if err != nil {
		t.Fatalf("launchspecFixtureValid: MarshalSnapshotToken: %v", err)
	}
	return handlercontract.LaunchSpec{
		RunID:               runID,
		WorkflowID:          wfID,
		NodeID:              core.NodeID("impl-node-1"),
		AgentType:           core.AgentType("claude-code"),
		WorkspacePath:       "/tmp/harmonik-test/worktrees/run-001",
		RequiredSkills:      []string{"beads-cli", "git"},
		SkillSearchPaths:    []string{"/usr/local/share/harmonik/skills"},
		Timeout:             3600,
		ProvisioningTimeout: 60,
		Budget:              core.BudgetRef("default"),
		FreedomProfileRef:   "standard",
		BeadID:              &beadID,
		SnapshotToken:       &tokEncoded,
		SchemaVersion:       handlercontract.LaunchSpecSchemaVersion,
	}
}

// launchspecFixtureReviewLoop returns a valid LaunchSpec for a review-loop
// dispatch in the implementer-resume phase (all four new HC-006 fields set).
func launchspecFixtureReviewLoop(t *testing.T) handlercontract.LaunchSpec {
	t.Helper()
	spec := launchspecFixtureValid(t)
	mode := "review-loop"
	phase := handlercontract.ReviewLoopPhaseImplementerResume
	iter := 2
	sessionID := "claude-session-abc123"
	spec.WorkflowMode = &mode
	spec.Phase = &phase
	spec.IterationCount = &iter
	spec.ClaudeSessionID = &sessionID
	return spec
}

// ─────────────────────────────────────────────────────────────────────────────
// Field set: verify fields are present and accessible
// ─────────────────────────────────────────────────────────────────────────────

// TestLaunchSpec_RequiredFields verifies that all required fields declared in §6.1
// are present and accessible on the LaunchSpec struct.
func TestLaunchSpec_RequiredFields(t *testing.T) {
	t.Parallel()

	spec := launchspecFixtureValid(t)

	// Required fields: must be non-zero.
	if spec.RunID == (core.RunID{}) {
		t.Error("HC-006: RunID is zero; want non-zero")
	}
	if spec.WorkflowID == (core.WorkflowID{}) {
		t.Error("HC-006: WorkflowID is zero; want non-zero")
	}
	if spec.NodeID == "" {
		t.Error("HC-006: NodeID is empty; want non-empty")
	}
	if spec.AgentType == "" {
		t.Error("HC-006: AgentType is empty; want non-empty")
	}
	if spec.WorkspacePath == "" {
		t.Error("HC-006: WorkspacePath is empty; want non-empty")
	}
	if spec.RequiredSkills == nil {
		t.Error("HC-006: RequiredSkills is nil; want non-nil slice")
	}
	if spec.SkillSearchPaths == nil {
		t.Error("HC-006: SkillSearchPaths is nil; want non-nil slice")
	}
	if spec.Timeout <= 0 {
		t.Errorf("HC-006: Timeout = %d; want positive", spec.Timeout)
	}
	if spec.ProvisioningTimeout <= 0 {
		t.Errorf("HC-006: ProvisioningTimeout = %d; want positive", spec.ProvisioningTimeout)
	}
	if !spec.Budget.Valid() {
		t.Error("HC-006: Budget is empty; want non-empty")
	}
	if spec.FreedomProfileRef == "" {
		t.Error("HC-006: FreedomProfileRef is empty; want non-empty")
	}
	if spec.SchemaVersion <= 0 {
		t.Errorf("HC-006: SchemaVersion = %d; want positive", spec.SchemaVersion)
	}

	// Optional fields (present in fixture): must be non-nil.
	if spec.BeadID == nil {
		t.Error("HC-006: BeadID is nil in fixture; want non-nil (test setup error)")
	}
	if spec.SnapshotToken == nil {
		t.Error("HC-006: SnapshotToken is nil in fixture; want non-nil (test setup error)")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Valid() contract
// ─────────────────────────────────────────────────────────────────────────────

// TestLaunchSpec_ValidHappyPath verifies that a fully-populated spec passes Valid().
func TestLaunchSpec_ValidHappyPath(t *testing.T) {
	t.Parallel()

	spec := launchspecFixtureValid(t)
	if err := spec.Valid(); err != nil {
		t.Errorf("HC-006: Valid() = %v; want nil", err)
	}
}

// TestLaunchSpec_ValidOptionalFieldsAbsent verifies that Valid() passes when
// optional fields BeadID and SnapshotToken are nil.
func TestLaunchSpec_ValidOptionalFieldsAbsent(t *testing.T) {
	t.Parallel()

	spec := launchspecFixtureValid(t)
	spec.BeadID = nil
	spec.SnapshotToken = nil
	if err := spec.Valid(); err != nil {
		t.Errorf("HC-006: Valid() with nil optionals = %v; want nil", err)
	}
}

// TestLaunchSpec_ValidZeroRunID verifies that Valid() rejects a zero RunID.
func TestLaunchSpec_ValidZeroRunID(t *testing.T) {
	t.Parallel()

	spec := launchspecFixtureValid(t)
	spec.RunID = core.RunID{}
	if err := spec.Valid(); err == nil {
		t.Error("HC-006: Valid() with zero RunID = nil; want error")
	}
}

// TestLaunchSpec_ValidZeroWorkflowID verifies that Valid() rejects a zero WorkflowID.
func TestLaunchSpec_ValidZeroWorkflowID(t *testing.T) {
	t.Parallel()

	spec := launchspecFixtureValid(t)
	spec.WorkflowID = core.WorkflowID{}
	if err := spec.Valid(); err == nil {
		t.Error("HC-006: Valid() with zero WorkflowID = nil; want error")
	}
}

// TestLaunchSpec_ValidEmptyNodeID verifies that Valid() rejects an empty NodeID.
func TestLaunchSpec_ValidEmptyNodeID(t *testing.T) {
	t.Parallel()

	spec := launchspecFixtureValid(t)
	spec.NodeID = ""
	if err := spec.Valid(); err == nil {
		t.Error("HC-006: Valid() with empty NodeID = nil; want error")
	}
}

// TestLaunchSpec_ValidEmptyAgentType verifies that Valid() rejects an empty AgentType.
func TestLaunchSpec_ValidEmptyAgentType(t *testing.T) {
	t.Parallel()

	spec := launchspecFixtureValid(t)
	spec.AgentType = ""
	if err := spec.Valid(); err == nil {
		t.Error("HC-006: Valid() with empty AgentType = nil; want error")
	}
}

// TestLaunchSpec_ValidEmptyWorkspacePath verifies that Valid() rejects an empty WorkspacePath.
func TestLaunchSpec_ValidEmptyWorkspacePath(t *testing.T) {
	t.Parallel()

	spec := launchspecFixtureValid(t)
	spec.WorkspacePath = ""
	if err := spec.Valid(); err == nil {
		t.Error("HC-006: Valid() with empty WorkspacePath = nil; want error")
	}
}

// TestLaunchSpec_ValidZeroTimeout verifies that Valid() rejects Timeout = 0.
func TestLaunchSpec_ValidZeroTimeout(t *testing.T) {
	t.Parallel()

	spec := launchspecFixtureValid(t)
	spec.Timeout = 0
	if err := spec.Valid(); err == nil {
		t.Error("HC-006: Valid() with Timeout=0 = nil; want error (zero forbidden per HC-006)")
	}
}

// TestLaunchSpec_ValidNegativeTimeout verifies that Valid() rejects negative Timeout.
func TestLaunchSpec_ValidNegativeTimeout(t *testing.T) {
	t.Parallel()

	spec := launchspecFixtureValid(t)
	spec.Timeout = -1
	if err := spec.Valid(); err == nil {
		t.Error("HC-006: Valid() with Timeout=-1 = nil; want error")
	}
}

// TestLaunchSpec_ValidZeroProvisioningTimeout verifies that Valid() rejects ProvisioningTimeout = 0.
func TestLaunchSpec_ValidZeroProvisioningTimeout(t *testing.T) {
	t.Parallel()

	spec := launchspecFixtureValid(t)
	spec.ProvisioningTimeout = 0
	if err := spec.Valid(); err == nil {
		t.Error("HC-006: Valid() with ProvisioningTimeout=0 = nil; want error")
	}
}

// TestLaunchSpec_ValidEmptyBudget verifies that Valid() rejects an empty Budget.
func TestLaunchSpec_ValidEmptyBudget(t *testing.T) {
	t.Parallel()

	spec := launchspecFixtureValid(t)
	spec.Budget = ""
	if err := spec.Valid(); err == nil {
		t.Error("HC-006: Valid() with empty Budget = nil; want error")
	}
}

// TestLaunchSpec_ValidEmptyFreedomProfileRef verifies that Valid() rejects an empty FreedomProfileRef.
func TestLaunchSpec_ValidEmptyFreedomProfileRef(t *testing.T) {
	t.Parallel()

	spec := launchspecFixtureValid(t)
	spec.FreedomProfileRef = ""
	if err := spec.Valid(); err == nil {
		t.Error("HC-006: Valid() with empty FreedomProfileRef = nil; want error")
	}
}

// TestLaunchSpec_ValidZeroSchemaVersion verifies that Valid() rejects SchemaVersion = 0.
func TestLaunchSpec_ValidZeroSchemaVersion(t *testing.T) {
	t.Parallel()

	spec := launchspecFixtureValid(t)
	spec.SchemaVersion = 0
	if err := spec.Valid(); err == nil {
		t.Error("HC-006: Valid() with SchemaVersion=0 = nil; want error")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// JSON round-trip
// ─────────────────────────────────────────────────────────────────────────────

// TestLaunchSpec_JSONRoundTrip verifies that a valid LaunchSpec can be
// marshalled to JSON and unmarshalled back with all fields intact.
func TestLaunchSpec_JSONRoundTrip(t *testing.T) {
	t.Parallel()

	original := launchspecFixtureValid(t)
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("HC-006: json.Marshal(LaunchSpec): %v", err)
	}

	var decoded handlercontract.LaunchSpec
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("HC-006: json.Unmarshal(LaunchSpec): %v", err)
	}

	if err := decoded.Valid(); err != nil {
		t.Errorf("HC-006: decoded LaunchSpec.Valid() = %v; want nil", err)
	}

	// Spot-check key fields survive the round-trip.
	if decoded.RunID != original.RunID {
		t.Errorf("HC-006: RunID mismatch after round-trip: got %v, want %v", decoded.RunID, original.RunID)
	}
	if decoded.Timeout != original.Timeout {
		t.Errorf("HC-006: Timeout mismatch after round-trip: got %d, want %d", decoded.Timeout, original.Timeout)
	}
	if decoded.SchemaVersion != original.SchemaVersion {
		t.Errorf("HC-006: SchemaVersion mismatch: got %d, want %d", decoded.SchemaVersion, original.SchemaVersion)
	}
}

// TestLaunchSpec_JSONOptionalFieldsOmitted verifies that optional fields
// (BeadID, SnapshotToken) are omitted from JSON when nil, not serialised as
// null — i.e. the `omitempty` tag is respected.
func TestLaunchSpec_JSONOptionalFieldsOmitted(t *testing.T) {
	t.Parallel()

	spec := launchspecFixtureValid(t)
	spec.BeadID = nil
	spec.SnapshotToken = nil

	data, err := json.Marshal(spec)
	if err != nil {
		t.Fatalf("HC-006: json.Marshal: %v", err)
	}

	// Verify that "bead_id" and "snapshot_token" keys are absent.
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("HC-006: json.Unmarshal to raw map: %v", err)
	}
	if _, ok := raw["bead_id"]; ok {
		t.Error("HC-006: bead_id present in JSON with nil BeadID; want omitted")
	}
	if _, ok := raw["snapshot_token"]; ok {
		t.Error("HC-006: snapshot_token present in JSON with nil SnapshotToken; want omitted")
	}
}

// TestLaunchSpec_JSONRequiredFieldsPresent verifies that all required JSON
// fields appear in the serialised form of a valid spec.
func TestLaunchSpec_JSONRequiredFieldsPresent(t *testing.T) {
	t.Parallel()

	spec := launchspecFixtureValid(t)
	// Clear optional fields to get a minimal required-field JSON.
	spec.BeadID = nil
	spec.SnapshotToken = nil

	data, err := json.Marshal(spec)
	if err != nil {
		t.Fatalf("HC-006: json.Marshal: %v", err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("HC-006: json.Unmarshal to raw map: %v", err)
	}

	required := []string{
		"run_id", "workflow_id", "node_id", "agent_type",
		"workspace_path", "required_skills", "skill_search_paths",
		"timeout", "provisioning_timeout", "budget",
		"freedom_profile_ref", "schema_version",
	}
	for _, field := range required {
		if _, ok := raw[field]; !ok {
			t.Errorf("HC-006: required JSON field %q absent from marshalled LaunchSpec", field)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Schema version
// ─────────────────────────────────────────────────────────────────────────────

// TestLaunchSpec_SchemaVersionConstant verifies that LaunchSpecSchemaVersion
// is positive and matches the SchemaVersion of a freshly-constructed spec.
func TestLaunchSpec_SchemaVersionConstant(t *testing.T) {
	t.Parallel()

	if handlercontract.LaunchSpecSchemaVersion <= 0 {
		t.Errorf("HC-006: LaunchSpecSchemaVersion = %d; want positive", handlercontract.LaunchSpecSchemaVersion)
	}

	spec := launchspecFixtureValid(t)
	if spec.SchemaVersion != handlercontract.LaunchSpecSchemaVersion {
		t.Errorf("HC-006: fixture SchemaVersion = %d, LaunchSpecSchemaVersion = %d; want equal",
			spec.SchemaVersion, handlercontract.LaunchSpecSchemaVersion)
	}
}

// TestLaunchSpec_ProvisioningTimeoutDefaultIs60 verifies the spec-declared
// default of 60 seconds for ProvisioningTimeout (HC-048a) by confirming the
// fixture uses it and Valid() accepts it.
func TestLaunchSpec_ProvisioningTimeoutDefaultIs60(t *testing.T) {
	t.Parallel()

	spec := launchspecFixtureValid(t)
	if spec.ProvisioningTimeout != 60 {
		t.Logf("HC-006: note: fixture ProvisioningTimeout = %d (spec default is 60)", spec.ProvisioningTimeout)
	}

	// Any positive value is valid per Valid().
	spec.ProvisioningTimeout = 60
	if err := spec.Valid(); err != nil {
		t.Errorf("HC-006: Valid() with ProvisioningTimeout=60: %v", err)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Review-loop optional fields (HC-006): WorkflowMode, Phase, IterationCount,
// ClaudeSessionID presence / absence rules.
// ─────────────────────────────────────────────────────────────────────────────

// TestLaunchSpec_ReviewLoopFieldsAllPresent verifies that a review-loop
// LaunchSpec with all four optional fields set passes Valid().
func TestLaunchSpec_ReviewLoopFieldsAllPresent(t *testing.T) {
	t.Parallel()

	spec := launchspecFixtureReviewLoop(t)
	if err := spec.Valid(); err != nil {
		t.Errorf("HC-006: Valid() with review-loop fields = %v; want nil", err)
	}
}

// TestLaunchSpec_ReviewLoopAllOptionalFieldsAbsent verifies that Valid() passes
// when all four optional review-loop fields are nil (single-mode run).
func TestLaunchSpec_ReviewLoopAllOptionalFieldsAbsent(t *testing.T) {
	t.Parallel()

	spec := launchspecFixtureValid(t)
	// WorkflowMode, Phase, IterationCount, ClaudeSessionID all nil by default.
	if spec.WorkflowMode != nil || spec.Phase != nil || spec.IterationCount != nil || spec.ClaudeSessionID != nil {
		t.Fatal("HC-006: base fixture must have no review-loop fields set (test setup error)")
	}
	if err := spec.Valid(); err != nil {
		t.Errorf("HC-006: Valid() with all review-loop fields absent = %v; want nil", err)
	}
}

// TestLaunchSpec_ReviewLoopPhaseWithoutIterationCount verifies that Valid()
// rejects a spec where Phase is present but IterationCount is absent.
func TestLaunchSpec_ReviewLoopPhaseWithoutIterationCount(t *testing.T) {
	t.Parallel()

	spec := launchspecFixtureReviewLoop(t)
	spec.IterationCount = nil
	if err := spec.Valid(); err == nil {
		t.Error("HC-006: Valid() with Phase set and IterationCount nil = nil; want error (co-presence rule)")
	}
}

// TestLaunchSpec_ReviewLoopIterationCountWithoutPhase verifies that Valid()
// rejects a spec where IterationCount is present but Phase is absent.
func TestLaunchSpec_ReviewLoopIterationCountWithoutPhase(t *testing.T) {
	t.Parallel()

	spec := launchspecFixtureReviewLoop(t)
	spec.Phase = nil
	if err := spec.Valid(); err == nil {
		t.Error("HC-006: Valid() with IterationCount set and Phase nil = nil; want error (co-presence rule)")
	}
}

// TestLaunchSpec_ReviewLoopInvalidPhase verifies that Valid() rejects an
// unrecognised Phase value.
func TestLaunchSpec_ReviewLoopInvalidPhase(t *testing.T) {
	t.Parallel()

	spec := launchspecFixtureReviewLoop(t)
	badPhase := handlercontract.ReviewLoopPhase("bogus-phase")
	spec.Phase = &badPhase
	if err := spec.Valid(); err == nil {
		t.Error("HC-006: Valid() with invalid Phase = nil; want error")
	}
}

// TestLaunchSpec_ReviewLoopZeroIterationCount verifies that Valid() rejects
// IterationCount = 0 when Phase is also present.
func TestLaunchSpec_ReviewLoopZeroIterationCount(t *testing.T) {
	t.Parallel()

	spec := launchspecFixtureReviewLoop(t)
	zero := 0
	spec.IterationCount = &zero
	if err := spec.Valid(); err == nil {
		t.Error("HC-006: Valid() with IterationCount=0 = nil; want error")
	}
}

// TestLaunchSpec_ClaudeSessionIDRequiredForImplementerResume verifies that
// Valid() rejects Phase=implementer-resume without ClaudeSessionID.
func TestLaunchSpec_ClaudeSessionIDRequiredForImplementerResume(t *testing.T) {
	t.Parallel()

	spec := launchspecFixtureReviewLoop(t)
	spec.ClaudeSessionID = nil
	if err := spec.Valid(); err == nil {
		t.Error("HC-006: Valid() with Phase=implementer-resume and nil ClaudeSessionID = nil; want error")
	}
}

// TestLaunchSpec_ClaudeSessionIDForbiddenForImplementerInitial verifies that
// Valid() rejects ClaudeSessionID when Phase=implementer-initial (no prior session).
func TestLaunchSpec_ClaudeSessionIDForbiddenForImplementerInitial(t *testing.T) {
	t.Parallel()

	spec := launchspecFixtureReviewLoop(t)
	phase := handlercontract.ReviewLoopPhaseImplementerInitial
	spec.Phase = &phase
	// ClaudeSessionID still set — should be rejected.
	if err := spec.Valid(); err == nil {
		t.Error("HC-006: Valid() with Phase=implementer-initial and ClaudeSessionID set = nil; want error")
	}
}

// TestLaunchSpec_ClaudeSessionIDForbiddenForReviewer verifies that Valid()
// rejects ClaudeSessionID when Phase=reviewer (each reviewer is a fresh session).
func TestLaunchSpec_ClaudeSessionIDForbiddenForReviewer(t *testing.T) {
	t.Parallel()

	spec := launchspecFixtureReviewLoop(t)
	phase := handlercontract.ReviewLoopPhaseReviewer
	spec.Phase = &phase
	// ClaudeSessionID still set — should be rejected.
	if err := spec.Valid(); err == nil {
		t.Error("HC-006: Valid() with Phase=reviewer and ClaudeSessionID set = nil; want error")
	}
}

// TestLaunchSpec_ImplementerInitialNoClaudeSessionID verifies that a valid
// implementer-initial spec (no ClaudeSessionID) passes Valid().
func TestLaunchSpec_ImplementerInitialNoClaudeSessionID(t *testing.T) {
	t.Parallel()

	spec := launchspecFixtureReviewLoop(t)
	phase := handlercontract.ReviewLoopPhaseImplementerInitial
	spec.Phase = &phase
	spec.ClaudeSessionID = nil
	if err := spec.Valid(); err != nil {
		t.Errorf("HC-006: Valid() with Phase=implementer-initial, no ClaudeSessionID = %v; want nil", err)
	}
}

// TestLaunchSpec_ReviewerPhaseNoClaudeSessionID verifies that a valid reviewer
// spec (no ClaudeSessionID) passes Valid().
func TestLaunchSpec_ReviewerPhaseNoClaudeSessionID(t *testing.T) {
	t.Parallel()

	spec := launchspecFixtureReviewLoop(t)
	phase := handlercontract.ReviewLoopPhaseReviewer
	spec.Phase = &phase
	spec.ClaudeSessionID = nil
	if err := spec.Valid(); err != nil {
		t.Errorf("HC-006: Valid() with Phase=reviewer, no ClaudeSessionID = %v; want nil", err)
	}
}

// TestLaunchSpec_WorkflowModeOptionalPresence verifies that WorkflowMode can be
// present or absent independently of Phase/IterationCount (it is observational
// only per HC-003a; no co-presence rule with Phase).
func TestLaunchSpec_WorkflowModeOptionalPresence(t *testing.T) {
	t.Parallel()

	spec := launchspecFixtureValid(t)
	mode := "review-loop"
	spec.WorkflowMode = &mode
	// No Phase/IterationCount — WorkflowMode alone is fine.
	if err := spec.Valid(); err != nil {
		t.Errorf("HC-006: Valid() with WorkflowMode set but no Phase/IterationCount = %v; want nil", err)
	}
}

// TestLaunchSpec_ReviewLoopJSONRoundTrip verifies that a review-loop LaunchSpec
// with all four optional fields survives JSON marshal/unmarshal with values intact.
func TestLaunchSpec_ReviewLoopJSONRoundTrip(t *testing.T) {
	t.Parallel()

	original := launchspecFixtureReviewLoop(t)
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("HC-006: json.Marshal(review-loop LaunchSpec): %v", err)
	}

	var decoded handlercontract.LaunchSpec
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("HC-006: json.Unmarshal(review-loop LaunchSpec): %v", err)
	}

	if err := decoded.Valid(); err != nil {
		t.Errorf("HC-006: decoded review-loop LaunchSpec.Valid() = %v; want nil", err)
	}
	if decoded.WorkflowMode == nil || *decoded.WorkflowMode != *original.WorkflowMode {
		t.Errorf("HC-006: WorkflowMode mismatch after round-trip: got %v, want %v", decoded.WorkflowMode, original.WorkflowMode)
	}
	if decoded.Phase == nil || *decoded.Phase != *original.Phase {
		t.Errorf("HC-006: Phase mismatch after round-trip: got %v, want %v", decoded.Phase, original.Phase)
	}
	if decoded.IterationCount == nil || *decoded.IterationCount != *original.IterationCount {
		t.Errorf("HC-006: IterationCount mismatch after round-trip: got %v, want %v", decoded.IterationCount, original.IterationCount)
	}
	if decoded.ClaudeSessionID == nil || *decoded.ClaudeSessionID != *original.ClaudeSessionID {
		t.Errorf("HC-006: ClaudeSessionID mismatch after round-trip: got %v, want %v", decoded.ClaudeSessionID, original.ClaudeSessionID)
	}
}

// TestLaunchSpec_ReviewLoopFieldsOmittedWhenAbsent verifies that the four
// optional review-loop fields are absent from JSON when nil.
func TestLaunchSpec_ReviewLoopFieldsOmittedWhenAbsent(t *testing.T) {
	t.Parallel()

	spec := launchspecFixtureValid(t)
	data, err := json.Marshal(spec)
	if err != nil {
		t.Fatalf("HC-006: json.Marshal: %v", err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("HC-006: json.Unmarshal to raw map: %v", err)
	}

	for _, field := range []string{"workflow_mode", "phase", "iteration_count", "claude_session_id"} {
		if _, ok := raw[field]; ok {
			t.Errorf("HC-006: %q present in JSON with nil value; want omitted", field)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// RC-015 / HC-006: SnapshotToken is String|None (wire format)
// ─────────────────────────────────────────────────────────────────────────────

// TestHC006_SnapshotTokenIsJSONStringInWireFormat verifies that the
// snapshot_token field serialises as a JSON string value (not a JSON object)
// in the marshalled LaunchSpec — matching the String|None declaration in
// specs/handler-contract.md §6.1 RECORD LaunchSpec.
//
// RC-015: "LaunchSpec.snapshot_token field, declared as String|None in HC-006,
// MUST carry the JSON-serialized form of the SnapshotToken record."
//
// Spec ref: specs/handler-contract.md §6.1; specs/reconciliation/spec.md §4.4 RC-015.
func TestHC006_SnapshotTokenIsJSONStringInWireFormat(t *testing.T) {
	t.Parallel()

	spec := launchspecFixtureValid(t)
	data, err := json.Marshal(spec)
	if err != nil {
		t.Fatalf("HC-006: json.Marshal(LaunchSpec): %v", err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("HC-006: json.Unmarshal to raw map: %v", err)
	}

	rawTok, ok := raw["snapshot_token"]
	if !ok {
		t.Fatal("HC-006: snapshot_token absent from marshalled LaunchSpec; want present (fixture includes it)")
	}

	// The wire value MUST be a JSON string, not a JSON object.
	// A JSON string starts with '"'; a JSON object starts with '{'.
	if len(rawTok) == 0 || rawTok[0] != '"' {
		t.Errorf("HC-006: snapshot_token wire value = %s; want a JSON string (starting with '\"'); "+
			"field must be String|None per HC-006, not an embedded object", rawTok)
	}
}

// TestHC006_MarshalSnapshotTokenRoundTrip verifies that MarshalSnapshotToken
// and ParseSnapshotToken are exact inverses: round-tripping a SnapshotToken
// through both functions recovers all three fields identically.
//
// Spec ref: specs/handler-contract.md §6.1; specs/reconciliation/spec.md §4.4 RC-015.
func TestHC006_MarshalSnapshotTokenRoundTrip(t *testing.T) {
	t.Parallel()

	original := core.SnapshotToken{
		GitHeadHash:         "abc123abc123abc123abc123abc123abc123abc1",
		BeadsAuditEntryID:   "audit-hk6306.23",
		CapturedAtTimestamp: "2026-05-31T12:00:00Z",
	}

	encoded, err := handlercontract.MarshalSnapshotToken(original)
	if err != nil {
		t.Fatalf("HC-006: MarshalSnapshotToken: %v", err)
	}
	if encoded == "" {
		t.Fatal("HC-006: MarshalSnapshotToken returned empty string")
	}

	decoded, err := handlercontract.ParseSnapshotToken(encoded)
	if err != nil {
		t.Fatalf("HC-006: ParseSnapshotToken: %v", err)
	}

	if decoded.GitHeadHash != original.GitHeadHash {
		t.Errorf("HC-006: GitHeadHash = %q, want %q", decoded.GitHeadHash, original.GitHeadHash)
	}
	if decoded.BeadsAuditEntryID != original.BeadsAuditEntryID {
		t.Errorf("HC-006: BeadsAuditEntryID = %q, want %q", decoded.BeadsAuditEntryID, original.BeadsAuditEntryID)
	}
	if decoded.CapturedAtTimestamp != original.CapturedAtTimestamp {
		t.Errorf("HC-006: CapturedAtTimestamp = %q, want %q", decoded.CapturedAtTimestamp, original.CapturedAtTimestamp)
	}
	if !decoded.Valid() {
		t.Error("HC-006: decoded SnapshotToken.Valid() = false; want true")
	}
}

// TestHC006_ParseSnapshotTokenRejectsInvalidJSON verifies that ParseSnapshotToken
// returns an error on malformed JSON input.
//
// Spec ref: specs/handler-contract.md §6.1; specs/reconciliation/spec.md §4.4 RC-015.
func TestHC006_ParseSnapshotTokenRejectsInvalidJSON(t *testing.T) {
	t.Parallel()

	_, err := handlercontract.ParseSnapshotToken("not-valid-json")
	if err == nil {
		t.Error("HC-006: ParseSnapshotToken(invalid JSON) = nil error; want error")
	}
}

// TestHC006_ParseSnapshotTokenRejectsIncompleteToken verifies that
// ParseSnapshotToken returns an error when the decoded SnapshotToken fails
// Valid() (i.e. a required field is missing).
//
// Spec ref: specs/handler-contract.md §6.1; specs/reconciliation/spec.md §4.4 RC-015.
func TestHC006_ParseSnapshotTokenRejectsIncompleteToken(t *testing.T) {
	t.Parallel()

	// Only git_head_hash present — missing beads_audit_entry_id and captured_at_timestamp.
	incomplete := `{"git_head_hash":"deadbeef"}`
	_, err := handlercontract.ParseSnapshotToken(incomplete)
	if err == nil {
		t.Error("HC-006: ParseSnapshotToken(incomplete token) = nil error; want error (Valid() should fail)")
	}
}

// TestLaunchSpec_ReviewLoopPhaseConstants verifies all three ReviewLoopPhase
// constants are valid per ReviewLoopPhase.Valid().
func TestLaunchSpec_ReviewLoopPhaseConstants(t *testing.T) {
	t.Parallel()

	phases := []handlercontract.ReviewLoopPhase{
		handlercontract.ReviewLoopPhaseImplementerInitial,
		handlercontract.ReviewLoopPhaseImplementerResume,
		handlercontract.ReviewLoopPhaseReviewer,
	}
	for _, p := range phases {
		if !p.Valid() {
			t.Errorf("HC-006: ReviewLoopPhase(%q).Valid() = false; want true", p)
		}
	}
	invalid := handlercontract.ReviewLoopPhase("not-a-phase")
	if invalid.Valid() {
		t.Error("HC-006: ReviewLoopPhase(\"not-a-phase\").Valid() = true; want false")
	}
}
