package workspace

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
)

// Tests for the SessionMetadataSidecar record per workspace-model.md §6.1
// and §4.7.WM-026 (bead hk-8mwo.63).
//
// Helper prefix: sidecarRecordFixture (distinct from other helper prefixes).

// sidecarRecordFixtureValid returns a fully-populated, valid SessionMetadataSidecar.
func sidecarRecordFixtureValid(t *testing.T) SessionMetadataSidecar {
	t.Helper()
	runID := core.RunID(uuid.MustParse("0196e300-0000-7000-8000-000000000001"))
	wfID := core.WorkflowID(uuid.MustParse("0196e300-0000-7000-8000-000000000002"))
	beadID := core.BeadID("hk-8mwo.63")
	return SessionMetadataSidecar{
		RunID:         runID,
		NodeID:        core.NodeID("impl-node-1"),
		AgentType:     core.AgentType("claude-code"),
		WorkflowID:    wfID,
		BeadID:        &beadID,
		LaunchedAt:    "2026-05-10T00:00:00Z",
		SchemaVersion: SessionMetadataSidecarSchemaVersion,
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Field set — all 7 fields accessible
// ─────────────────────────────────────────────────────────────────────────────

// TestWM063_SidecarRecord7Fields verifies that all 7 fields declared in §6.1
// are present and accessible on SessionMetadataSidecar.
func TestWM063_SidecarRecord7Fields(t *testing.T) {
	t.Parallel()

	s := sidecarRecordFixtureValid(t)

	// Required fields.
	if s.RunID == (core.RunID{}) {
		t.Error("WM-063: RunID is zero")
	}
	if s.NodeID == "" {
		t.Error("WM-063: NodeID is empty")
	}
	if s.AgentType == "" {
		t.Error("WM-063: AgentType is empty")
	}
	if s.WorkflowID == (core.WorkflowID{}) {
		t.Error("WM-063: WorkflowID is zero")
	}
	if s.LaunchedAt == "" {
		t.Error("WM-063: LaunchedAt is empty")
	}
	if s.SchemaVersion <= 0 {
		t.Errorf("WM-063: SchemaVersion = %d; want positive", s.SchemaVersion)
	}

	// Optional field (present in fixture).
	if s.BeadID == nil {
		t.Error("WM-063: BeadID is nil in fixture; want non-nil (test setup error)")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Valid() — required field validation
// ─────────────────────────────────────────────────────────────────────────────

// TestWM063_ValidHappyPath verifies that a fully-populated sidecar passes Valid().
func TestWM063_ValidHappyPath(t *testing.T) {
	t.Parallel()

	s := sidecarRecordFixtureValid(t)
	if err := s.Valid(); err != nil {
		t.Errorf("WM-063: Valid() = %v; want nil", err)
	}
}

// TestWM063_ValidOptionalFieldAbsent verifies that Valid() passes when BeadID is nil.
func TestWM063_ValidOptionalFieldAbsent(t *testing.T) {
	t.Parallel()

	s := sidecarRecordFixtureValid(t)
	s.BeadID = nil
	if err := s.Valid(); err != nil {
		t.Errorf("WM-063: Valid() with nil BeadID = %v; want nil", err)
	}
}

// TestWM063_ValidRejectsZeroRunID verifies that Valid() rejects a zero RunID.
func TestWM063_ValidRejectsZeroRunID(t *testing.T) {
	t.Parallel()

	s := sidecarRecordFixtureValid(t)
	s.RunID = core.RunID{}
	if err := s.Valid(); err == nil {
		t.Error("WM-063: Valid() with zero RunID = nil; want error")
	}
}

// TestWM063_ValidRejectsEmptyNodeID verifies that Valid() rejects empty NodeID.
func TestWM063_ValidRejectsEmptyNodeID(t *testing.T) {
	t.Parallel()

	s := sidecarRecordFixtureValid(t)
	s.NodeID = ""
	if err := s.Valid(); err == nil {
		t.Error("WM-063: Valid() with empty NodeID = nil; want error")
	}
}

// TestWM063_ValidRejectsEmptyAgentType verifies that Valid() rejects empty AgentType.
func TestWM063_ValidRejectsEmptyAgentType(t *testing.T) {
	t.Parallel()

	s := sidecarRecordFixtureValid(t)
	s.AgentType = ""
	if err := s.Valid(); err == nil {
		t.Error("WM-063: Valid() with empty AgentType = nil; want error")
	}
}

// TestWM063_ValidRejectsZeroWorkflowID verifies that Valid() rejects a zero WorkflowID.
func TestWM063_ValidRejectsZeroWorkflowID(t *testing.T) {
	t.Parallel()

	s := sidecarRecordFixtureValid(t)
	s.WorkflowID = core.WorkflowID{}
	if err := s.Valid(); err == nil {
		t.Error("WM-063: Valid() with zero WorkflowID = nil; want error")
	}
}

// TestWM063_ValidRejectsEmptyLaunchedAt verifies that Valid() rejects empty LaunchedAt.
func TestWM063_ValidRejectsEmptyLaunchedAt(t *testing.T) {
	t.Parallel()

	s := sidecarRecordFixtureValid(t)
	s.LaunchedAt = ""
	if err := s.Valid(); err == nil {
		t.Error("WM-063: Valid() with empty LaunchedAt = nil; want error")
	}
}

// TestWM063_ValidRejectsZeroSchemaVersion verifies that Valid() rejects SchemaVersion = 0.
func TestWM063_ValidRejectsZeroSchemaVersion(t *testing.T) {
	t.Parallel()

	s := sidecarRecordFixtureValid(t)
	s.SchemaVersion = 0
	if err := s.Valid(); err == nil {
		t.Error("WM-063: Valid() with SchemaVersion=0 = nil; want error")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// JSON round-trip and tag verification
// ─────────────────────────────────────────────────────────────────────────────

// TestWM063_JSONRoundTrip verifies that a valid sidecar can be marshalled and
// unmarshalled with all fields intact.
func TestWM063_JSONRoundTrip(t *testing.T) {
	t.Parallel()

	original := sidecarRecordFixtureValid(t)
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("WM-063: json.Marshal: %v", err)
	}

	var decoded SessionMetadataSidecar
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("WM-063: json.Unmarshal: %v", err)
	}
	if err := decoded.Valid(); err != nil {
		t.Errorf("WM-063: decoded.Valid() = %v; want nil", err)
	}

	if decoded.RunID != original.RunID {
		t.Errorf("WM-063: RunID mismatch: got %v, want %v", decoded.RunID, original.RunID)
	}
	if decoded.LaunchedAt != original.LaunchedAt {
		t.Errorf("WM-063: LaunchedAt mismatch: got %q, want %q", decoded.LaunchedAt, original.LaunchedAt)
	}
}

// TestWM063_JSONOptionalFieldOmitted verifies that BeadID is omitted when nil.
func TestWM063_JSONOptionalFieldOmitted(t *testing.T) {
	t.Parallel()

	s := sidecarRecordFixtureValid(t)
	s.BeadID = nil

	data, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("WM-063: json.Marshal: %v", err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("WM-063: json.Unmarshal to raw map: %v", err)
	}
	if _, ok := raw["bead_id"]; ok {
		t.Error("WM-063: bead_id present in JSON with nil BeadID; want omitted")
	}
}

// TestWM063_JSONRequiredFieldsPresent verifies that all 6 required JSON keys
// appear in the serialised form.
func TestWM063_JSONRequiredFieldsPresent(t *testing.T) {
	t.Parallel()

	s := sidecarRecordFixtureValid(t)
	s.BeadID = nil

	data, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("WM-063: json.Marshal: %v", err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("WM-063: json.Unmarshal to raw map: %v", err)
	}

	for _, field := range []string{"run_id", "node_id", "agent_type", "workflow_id", "launched_at", "schema_version"} {
		if _, ok := raw[field]; !ok {
			t.Errorf("WM-063: required JSON field %q absent from marshalled sidecar", field)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Path helper
// ─────────────────────────────────────────────────────────────────────────────

// TestWM063_SidecarPathShape verifies that SessionMetadataSidecarPath returns
// the canonical path per §6.2:
// ${workspace_path}/.harmonik/sessions/<session_id>/harmonik.meta.json
func TestWM063_SidecarPathShape(t *testing.T) {
	t.Parallel()

	workspacePath := "/abs/path/to/worktree"
	sessionID := "sess-0196e300-0000-7000-8000-000000000001"

	got := SessionMetadataSidecarPath(workspacePath, sessionID)
	want := filepath.Join(workspacePath, ".harmonik", "sessions", sessionID, "harmonik.meta.json")

	if got != want {
		t.Errorf("WM-063: SidecarPath = %q, want %q", got, want)
	}
}

// TestWM063_SidecarPathContainsHarmonikMeta verifies that the filename component
// is exactly "harmonik.meta.json" per the spec.
func TestWM063_SidecarPathContainsHarmonikMeta(t *testing.T) {
	t.Parallel()

	got := SessionMetadataSidecarPath("/any/path", "session-001")
	if filepath.Base(got) != "harmonik.meta.json" {
		t.Errorf("WM-063: sidecar filename = %q, want harmonik.meta.json", filepath.Base(got))
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Schema version constant
// ─────────────────────────────────────────────────────────────────────────────

// TestWM063_SchemaVersionConstantIsPositive verifies SessionMetadataSidecarSchemaVersion > 0.
func TestWM063_SchemaVersionConstantIsPositive(t *testing.T) {
	t.Parallel()

	if SessionMetadataSidecarSchemaVersion <= 0 {
		t.Errorf("WM-063: SessionMetadataSidecarSchemaVersion = %d; want positive",
			SessionMetadataSidecarSchemaVersion)
	}
}

// TestWM063_AgentTypeIsAuthoritative verifies that the AgentType field is present
// and can distinguish between agentic and non-agentic sessions for WM-022.
// This test uses the "claude-code" agent type (agentic) as the canonical example.
func TestWM063_AgentTypeIsAuthoritative(t *testing.T) {
	t.Parallel()

	s := sidecarRecordFixtureValid(t)
	// AgentType must be non-empty; the fixture uses "claude-code" (agentic).
	if s.AgentType == "" {
		t.Error("WM-063: AgentType is empty; WM-022 requires it for implementer identification")
	}
	// The agent_type field must survive Valid() — it's a required field.
	if err := s.Valid(); err != nil {
		t.Errorf("WM-063: sidecar with AgentType %q: Valid() = %v; want nil", s.AgentType, err)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// WriteSessionMetadataSidecarAtomic / ReadSessionMetadataSidecar
// ─────────────────────────────────────────────────────────────────────────────

// TestWM063_WriteAtomicCreatesFile verifies that WriteSessionMetadataSidecarAtomic
// creates the sidecar file at the canonical path.
func TestWM063_WriteAtomicCreatesFile(t *testing.T) {
	t.Parallel()

	workspacePath := t.TempDir()
	sessionID := "sess-0196e300-0000-7000-8000-000000000002"
	target := SessionMetadataSidecarPath(workspacePath, sessionID)

	s := sidecarRecordFixtureValid(t)
	if err := WriteSessionMetadataSidecarAtomic(target, &s); err != nil {
		t.Fatalf("WM-026: WriteSessionMetadataSidecarAtomic: %v", err)
	}

	if _, err := os.Stat(target); err != nil {
		t.Errorf("WM-026: sidecar file absent after write: %v", err)
	}
}

// TestWM063_WriteAtomicNoTempFileAfterSuccess verifies that no .tmp-<pid> file
// remains after a successful atomic write (rename cleans it up).
func TestWM063_WriteAtomicNoTempFileAfterSuccess(t *testing.T) {
	t.Parallel()

	workspacePath := t.TempDir()
	sessionID := "sess-0196e300-0000-7000-8000-000000000003"
	target := SessionMetadataSidecarPath(workspacePath, sessionID)

	s := sidecarRecordFixtureValid(t)
	if err := WriteSessionMetadataSidecarAtomic(target, &s); err != nil {
		t.Fatalf("WM-026: WriteSessionMetadataSidecarAtomic: %v", err)
	}

	// No .tmp-* file should remain in the session directory.
	dir := filepath.Dir(target)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, e := range entries {
		if e.Name() != "harmonik.meta.json" {
			t.Errorf("WM-026: unexpected file in session dir after write: %q (want only harmonik.meta.json)", e.Name())
		}
	}
}

// TestWM063_WriteAtomicRoundTrip verifies that a sidecar written by
// WriteSessionMetadataSidecarAtomic can be read back by ReadSessionMetadataSidecar
// with all fields intact.
func TestWM063_WriteAtomicRoundTrip(t *testing.T) {
	t.Parallel()

	workspacePath := t.TempDir()
	sessionID := "sess-0196e300-0000-7000-8000-000000000004"
	target := SessionMetadataSidecarPath(workspacePath, sessionID)

	original := sidecarRecordFixtureValid(t)
	if err := WriteSessionMetadataSidecarAtomic(target, &original); err != nil {
		t.Fatalf("WM-026: WriteSessionMetadataSidecarAtomic: %v", err)
	}

	decoded, err := ReadSessionMetadataSidecar(target)
	if err != nil {
		t.Fatalf("WM-026: ReadSessionMetadataSidecar: %v", err)
	}
	if decoded == nil {
		t.Fatal("WM-026: ReadSessionMetadataSidecar returned nil; want non-nil")
	}

	if decoded.RunID != original.RunID {
		t.Errorf("WM-026: RunID mismatch: got %v, want %v", decoded.RunID, original.RunID)
	}
	if decoded.AgentType != original.AgentType {
		t.Errorf("WM-026: AgentType mismatch: got %q, want %q", decoded.AgentType, original.AgentType)
	}
	if decoded.SchemaVersion != original.SchemaVersion {
		t.Errorf("WM-026: SchemaVersion mismatch: got %d, want %d", decoded.SchemaVersion, original.SchemaVersion)
	}
}

// TestWM063_ReadAbsentSidecarReturnsNil verifies that ReadSessionMetadataSidecar
// returns (nil, nil) when the sidecar file does not exist.
func TestWM063_ReadAbsentSidecarReturnsNil(t *testing.T) {
	t.Parallel()

	target := filepath.Join(t.TempDir(), "nonexistent", "harmonik.meta.json")
	s, err := ReadSessionMetadataSidecar(target)
	if err != nil {
		t.Errorf("WM-026: ReadSessionMetadataSidecar(absent) error = %v; want nil", err)
	}
	if s != nil {
		t.Errorf("WM-026: ReadSessionMetadataSidecar(absent) returned non-nil sidecar; want nil")
	}
}

// TestWM063_WriteAtomicRejectsInvalidSidecar verifies that
// WriteSessionMetadataSidecarAtomic returns an error for an invalid sidecar.
func TestWM063_WriteAtomicRejectsInvalidSidecar(t *testing.T) {
	t.Parallel()

	target := filepath.Join(t.TempDir(), "harmonik.meta.json")
	s := &SessionMetadataSidecar{} // zero value: all required fields empty

	if err := WriteSessionMetadataSidecarAtomic(target, s); err == nil {
		t.Error("WM-026: WriteSessionMetadataSidecarAtomic with invalid sidecar = nil; want error")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// T-WM-030 acceptance tests: WorkflowMode field on sidecar (hk-7om2q.30)
//
// BI-020 amendment: the sidecar MAY carry the resolved workflow_mode.
// When non-nil, the value MUST match the mode the daemon dispatched the run
// under. Helper prefix: sidecarWMFixture (distinct from sidecarRecordFixture).
// ─────────────────────────────────────────────────────────────────────────────

// sidecarWMFixtureWithMode returns a valid SessionMetadataSidecar with the
// WorkflowMode field set to mode.
func sidecarWMFixtureWithMode(t *testing.T, mode core.WorkflowMode) SessionMetadataSidecar {
	t.Helper()
	s := sidecarRecordFixtureValid(t)
	s.WorkflowMode = &mode
	return s
}

// TestWM030_ReviewLoopSidecarCarriesWorkflowMode verifies that a sidecar
// written for a review-loop run round-trips with workflow_mode="review-loop"
// (T-WM-030 acceptance criterion 1).
func TestWM030_ReviewLoopSidecarCarriesWorkflowMode(t *testing.T) {
	t.Parallel()

	workspacePath := t.TempDir()
	sessionID := "sess-0196e300-0000-7000-8000-000000000010"
	target := SessionMetadataSidecarPath(workspacePath, sessionID)

	original := sidecarWMFixtureWithMode(t, core.WorkflowModeReviewLoop)
	if err := WriteSessionMetadataSidecarAtomic(target, &original); err != nil {
		t.Fatalf("T-WM-030: WriteSessionMetadataSidecarAtomic: %v", err)
	}

	decoded, err := ReadSessionMetadataSidecar(target)
	if err != nil {
		t.Fatalf("T-WM-030: ReadSessionMetadataSidecar: %v", err)
	}
	if decoded == nil {
		t.Fatal("T-WM-030: ReadSessionMetadataSidecar returned nil; want non-nil")
	}

	if decoded.WorkflowMode == nil {
		t.Fatal("T-WM-030: decoded.WorkflowMode is nil; want review-loop")
	}
	if *decoded.WorkflowMode != core.WorkflowModeReviewLoop {
		t.Errorf("T-WM-030: decoded.WorkflowMode = %q; want %q", *decoded.WorkflowMode, core.WorkflowModeReviewLoop)
	}
}

// TestWM030_ReviewLoopSidecarJSONKeyPresent verifies that a review-loop sidecar
// marshals with the "workflow_mode" key set to "review-loop".
func TestWM030_ReviewLoopSidecarJSONKeyPresent(t *testing.T) {
	t.Parallel()

	s := sidecarWMFixtureWithMode(t, core.WorkflowModeReviewLoop)

	data, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("T-WM-030: json.Marshal: %v", err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("T-WM-030: json.Unmarshal to raw map: %v", err)
	}

	val, ok := raw["workflow_mode"]
	if !ok {
		t.Fatal("T-WM-030: workflow_mode key absent in JSON; want present for review-loop run")
	}
	if string(val) != `"review-loop"` {
		t.Errorf("T-WM-030: workflow_mode JSON value = %s; want \"review-loop\"", val)
	}
}

// TestWM030_SingleModeSidecarOmitsWorkflowMode verifies that a sidecar where
// WorkflowMode is nil (single-mode sidecar not carrying the optional field)
// omits the "workflow_mode" key from the JSON (T-WM-030 acceptance criterion 2,
// omit branch).
func TestWM030_SingleModeSidecarOmitsWorkflowMode(t *testing.T) {
	t.Parallel()

	s := sidecarRecordFixtureValid(t)
	// WorkflowMode is nil — represents a single-mode run that omits the field.

	data, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("T-WM-030: json.Marshal: %v", err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("T-WM-030: json.Unmarshal to raw map: %v", err)
	}

	if _, ok := raw["workflow_mode"]; ok {
		t.Error("T-WM-030: workflow_mode key present with nil WorkflowMode; want omitted")
	}
}

// TestWM030_SingleModeSidecarCarriesWorkflowModeSingle verifies that a sidecar
// where WorkflowMode is explicitly set to "single" carries that value in JSON
// (T-WM-030 acceptance criterion 2, carry-single branch).
func TestWM030_SingleModeSidecarCarriesWorkflowModeSingle(t *testing.T) {
	t.Parallel()

	s := sidecarWMFixtureWithMode(t, core.WorkflowModeSingle)

	data, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("T-WM-030: json.Marshal: %v", err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("T-WM-030: json.Unmarshal to raw map: %v", err)
	}

	val, ok := raw["workflow_mode"]
	if !ok {
		t.Fatal("T-WM-030: workflow_mode key absent; want \"single\" when explicitly set")
	}
	if string(val) != `"single"` {
		t.Errorf("T-WM-030: workflow_mode JSON value = %s; want \"single\"", val)
	}
}

// TestWM030_WorkflowModeRoundTrip verifies that a sidecar with
// WorkflowMode=review-loop survives a full write→read round-trip with the
// field intact.
func TestWM030_WorkflowModeRoundTrip(t *testing.T) {
	t.Parallel()

	workspacePath := t.TempDir()
	sessionID := "sess-0196e300-0000-7000-8000-000000000011"
	target := SessionMetadataSidecarPath(workspacePath, sessionID)

	original := sidecarWMFixtureWithMode(t, core.WorkflowModeReviewLoop)
	if err := WriteSessionMetadataSidecarAtomic(target, &original); err != nil {
		t.Fatalf("T-WM-030: WriteSessionMetadataSidecarAtomic: %v", err)
	}

	decoded, err := ReadSessionMetadataSidecar(target)
	if err != nil {
		t.Fatalf("T-WM-030: ReadSessionMetadataSidecar: %v", err)
	}
	if decoded == nil {
		t.Fatal("T-WM-030: decoded is nil")
	}

	// All pre-existing required fields must survive.
	if decoded.RunID != original.RunID {
		t.Errorf("T-WM-030: RunID mismatch: got %v, want %v", decoded.RunID, original.RunID)
	}
	if decoded.AgentType != original.AgentType {
		t.Errorf("T-WM-030: AgentType mismatch: got %q, want %q", decoded.AgentType, original.AgentType)
	}

	// WorkflowMode must survive.
	if decoded.WorkflowMode == nil {
		t.Fatal("T-WM-030: decoded.WorkflowMode is nil after round-trip; want review-loop")
	}
	if *decoded.WorkflowMode != core.WorkflowModeReviewLoop {
		t.Errorf("T-WM-030: WorkflowMode mismatch: got %q, want %q", *decoded.WorkflowMode, core.WorkflowModeReviewLoop)
	}
}

// TestWM030_WorkflowModeFieldIsOptional verifies that Valid() passes for a sidecar
// with WorkflowMode set to nil (field is optional per BI-020).
func TestWM030_WorkflowModeFieldIsOptional(t *testing.T) {
	t.Parallel()

	s := sidecarRecordFixtureValid(t)
	// WorkflowMode is nil — the field is optional.
	if err := s.Valid(); err != nil {
		t.Errorf("T-WM-030: Valid() with nil WorkflowMode = %v; want nil (field is optional)", err)
	}
}
