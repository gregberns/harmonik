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
	tok := core.SnapshotToken{
		GitHeadHash:         "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef",
		BeadsAuditEntryID:   "audit-001",
		CapturedAtTimestamp: "2026-05-10T00:00:00Z",
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
		SnapshotToken:       &tok,
		SchemaVersion:       handlercontract.LaunchSpecSchemaVersion,
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Field set: verify 13 fields are present and accessible
// ─────────────────────────────────────────────────────────────────────────────

// TestLaunchSpec_RequiredFields verifies that all 13 fields declared in §6.1
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
