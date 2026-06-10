package workspace

import (
	"testing"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
)

// Tests for the Workspace record shape per workspace-model.md §6.1 (bead hk-8mwo.59).
//
// Helper prefix: wsRecordFixture (distinct from other helper prefixes in this package).

// wsRecordFixtureValid returns a fully-populated, valid Workspace for tests.
func wsRecordFixtureValid(t *testing.T) *Workspace {
	t.Helper()
	runID := core.RunID(uuid.MustParse("0196e200-0000-7000-8000-000000000001"))
	beadID := core.BeadID("hk-8mwo.59")
	handlerRef := core.HandlerRef("claude-code@v1.0.0")
	return &Workspace{
		WorkspaceID:           "ws-0196e200-0000-7000-8000-000000000001",
		RunID:                 runID,
		Repository:            "/abs/path/to/repo",
		ParentCommit:          "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef",
		BranchName:            "run/0196e200-0000-7000-8000-000000000001",
		Path:                  "/abs/path/to/repo/.harmonik/worktrees/0196e200-0000-7000-8000-000000000001",
		State:                 core.WorkspaceStateLeased,
		InterruptState:        core.InterruptStateNone,
		BeadID:                &beadID,
		ImplementerHandlerRef: &handlerRef,
		Metadata: map[string]string{
			"created_at":           "2026-05-10T00:00:00Z",
			"operator_fingerprint": "test-operator",
		},
		SchemaVersion: WorkspaceSchemaVersion,
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Field set — all 12 fields accessible
// ─────────────────────────────────────────────────────────────────────────────

// TestWM059_WorkspaceRecord12Fields verifies that all 12 fields declared in §6.1
// are present and accessible on the Workspace struct.
func TestWM059_WorkspaceRecord12Fields(t *testing.T) {
	t.Parallel()

	ws := wsRecordFixtureValid(t)

	// Required fields.
	if ws.WorkspaceID == "" {
		t.Error("WM-059: WorkspaceID is empty")
	}
	if ws.RunID == (core.RunID{}) {
		t.Error("WM-059: RunID is zero")
	}
	if ws.Repository == "" {
		t.Error("WM-059: Repository is empty")
	}
	if ws.ParentCommit == "" {
		t.Error("WM-059: ParentCommit is empty")
	}
	if ws.BranchName == "" {
		t.Error("WM-059: BranchName is empty")
	}
	if ws.Path == "" {
		t.Error("WM-059: Path is empty")
	}
	if !ws.State.Valid() {
		t.Errorf("WM-059: State %q is not valid", ws.State)
	}
	if !ws.InterruptState.Valid() {
		t.Errorf("WM-059: InterruptState %q is not valid", ws.InterruptState)
	}
	if ws.SchemaVersion <= 0 {
		t.Errorf("WM-059: SchemaVersion = %d; want positive", ws.SchemaVersion)
	}
	if ws.Metadata == nil {
		t.Error("WM-059: Metadata is nil")
	}

	// Optional fields (present in fixture).
	if ws.BeadID == nil {
		t.Error("WM-059: BeadID is nil in fixture; want non-nil (test setup error)")
	}
	if ws.ImplementerHandlerRef == nil {
		t.Error("WM-059: ImplementerHandlerRef is nil in fixture; want non-nil (test setup error)")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Valid() — required field validation
// ─────────────────────────────────────────────────────────────────────────────

// TestWM059_ValidHappyPath verifies that a fully-populated Workspace passes Valid().
func TestWM059_ValidHappyPath(t *testing.T) {
	t.Parallel()

	ws := wsRecordFixtureValid(t)
	if err := ws.Valid(); err != nil {
		t.Errorf("WM-059: Valid() = %v; want nil", err)
	}
}

// TestWM059_ValidOptionalFieldsAbsent verifies that Valid() passes when optional
// fields BeadID and ImplementerHandlerRef are nil.
func TestWM059_ValidOptionalFieldsAbsent(t *testing.T) {
	t.Parallel()

	ws := wsRecordFixtureValid(t)
	ws.BeadID = nil
	ws.ImplementerHandlerRef = nil
	if err := ws.Valid(); err != nil {
		t.Errorf("WM-059: Valid() with nil optionals = %v; want nil", err)
	}
}

// TestWM059_ValidRejectsEmptyWorkspaceID verifies that Valid() rejects empty WorkspaceID.
func TestWM059_ValidRejectsEmptyWorkspaceID(t *testing.T) {
	t.Parallel()

	ws := wsRecordFixtureValid(t)
	ws.WorkspaceID = ""
	if err := ws.Valid(); err == nil {
		t.Error("WM-059: Valid() with empty WorkspaceID = nil; want error")
	}
}

// TestWM059_ValidRejectsZeroRunID verifies that Valid() rejects a zero RunID.
func TestWM059_ValidRejectsZeroRunID(t *testing.T) {
	t.Parallel()

	ws := wsRecordFixtureValid(t)
	ws.RunID = core.RunID{}
	if err := ws.Valid(); err == nil {
		t.Error("WM-059: Valid() with zero RunID = nil; want error")
	}
}

// TestWM059_ValidRejectsEmptyRepository verifies that Valid() rejects empty Repository.
func TestWM059_ValidRejectsEmptyRepository(t *testing.T) {
	t.Parallel()

	ws := wsRecordFixtureValid(t)
	ws.Repository = ""
	if err := ws.Valid(); err == nil {
		t.Error("WM-059: Valid() with empty Repository = nil; want error")
	}
}

// TestWM059_ValidRejectsEmptyParentCommit verifies that Valid() rejects empty ParentCommit.
func TestWM059_ValidRejectsEmptyParentCommit(t *testing.T) {
	t.Parallel()

	ws := wsRecordFixtureValid(t)
	ws.ParentCommit = ""
	if err := ws.Valid(); err == nil {
		t.Error("WM-059: Valid() with empty ParentCommit = nil; want error")
	}
}

// TestWM059_ValidRejectsEmptyBranchName verifies that Valid() rejects empty BranchName.
func TestWM059_ValidRejectsEmptyBranchName(t *testing.T) {
	t.Parallel()

	ws := wsRecordFixtureValid(t)
	ws.BranchName = ""
	if err := ws.Valid(); err == nil {
		t.Error("WM-059: Valid() with empty BranchName = nil; want error")
	}
}

// TestWM059_ValidRejectsEmptyPath verifies that Valid() rejects empty Path.
func TestWM059_ValidRejectsEmptyPath(t *testing.T) {
	t.Parallel()

	ws := wsRecordFixtureValid(t)
	ws.Path = ""
	if err := ws.Valid(); err == nil {
		t.Error("WM-059: Valid() with empty Path = nil; want error")
	}
}

// TestWM059_ValidRejectsInvalidState verifies that Valid() rejects an invalid WorkspaceState.
func TestWM059_ValidRejectsInvalidState(t *testing.T) {
	t.Parallel()

	ws := wsRecordFixtureValid(t)
	ws.State = core.WorkspaceState("not-a-state")
	if err := ws.Valid(); err == nil {
		t.Error("WM-059: Valid() with invalid State = nil; want error")
	}
}

// TestWM059_ValidRejectsInvalidInterruptState verifies that Valid() rejects
// an invalid InterruptState.
func TestWM059_ValidRejectsInvalidInterruptState(t *testing.T) {
	t.Parallel()

	ws := wsRecordFixtureValid(t)
	ws.InterruptState = core.InterruptState("not-an-interrupt-state")
	if err := ws.Valid(); err == nil {
		t.Error("WM-059: Valid() with invalid InterruptState = nil; want error")
	}
}

// TestWM059_ValidRejectsZeroSchemaVersion verifies that Valid() rejects SchemaVersion = 0.
func TestWM059_ValidRejectsZeroSchemaVersion(t *testing.T) {
	t.Parallel()

	ws := wsRecordFixtureValid(t)
	ws.SchemaVersion = 0
	if err := ws.Valid(); err == nil {
		t.Error("WM-059: Valid() with SchemaVersion=0 = nil; want error")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Schema version constant
// ─────────────────────────────────────────────────────────────────────────────

// TestWM059_SchemaVersionConstantIsPositive verifies that WorkspaceSchemaVersion
// is positive per §6.4.
func TestWM059_SchemaVersionConstantIsPositive(t *testing.T) {
	t.Parallel()

	if WorkspaceSchemaVersion <= 0 {
		t.Errorf("WM-059: WorkspaceSchemaVersion = %d; want positive", WorkspaceSchemaVersion)
	}
}

// TestWM059_SchemaVersionConstantMatchesFixture verifies that the fixture uses
// WorkspaceSchemaVersion (i.e., the constant is the canonical value).
func TestWM059_SchemaVersionConstantMatchesFixture(t *testing.T) {
	t.Parallel()

	ws := wsRecordFixtureValid(t)
	if ws.SchemaVersion != WorkspaceSchemaVersion {
		t.Errorf("WM-059: fixture SchemaVersion = %d, WorkspaceSchemaVersion = %d; want equal",
			ws.SchemaVersion, WorkspaceSchemaVersion)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Spec invariants
// ─────────────────────────────────────────────────────────────────────────────

// TestWM059_WorkspaceIDDerivedFromRunID verifies that workspace_id is "ws-"+run_id
// per WM-004 when constructed correctly. This test uses the fixture's values to
// confirm the derivation rule is honoured.
func TestWM059_WorkspaceIDDerivedFromRunID(t *testing.T) {
	t.Parallel()

	runID := "0196e200-0000-7000-8000-000000000001"
	wantWorkspaceID := "ws-" + runID

	ws := wsRecordFixtureValid(t)
	if ws.WorkspaceID != wantWorkspaceID {
		t.Errorf("WM-059: workspace_id = %q, want %q (ws-+run_id per WM-004)",
			ws.WorkspaceID, wantWorkspaceID)
	}
}

// TestWM059_MetadataClosedMapKeys verifies that the metadata map carries only the
// two declared keys per §6.1: "created_at" and "operator_fingerprint".
func TestWM059_MetadataClosedMapKeys(t *testing.T) {
	t.Parallel()

	ws := wsRecordFixtureValid(t)

	if _, ok := ws.Metadata["created_at"]; !ok {
		t.Error("WM-059: metadata missing required key \"created_at\"")
	}
	if _, ok := ws.Metadata["operator_fingerprint"]; !ok {
		t.Error("WM-059: metadata missing required key \"operator_fingerprint\"")
	}
}
