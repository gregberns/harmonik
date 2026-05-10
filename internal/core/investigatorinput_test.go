package core

import (
	"testing"

	"github.com/google/uuid"
)

// investigatorInputFixture returns a fully-populated InvestigatorInput with
// all required fields set to valid non-zero values. Tests mutate individual
// fields to probe Valid().
func investigatorInputFixture(t *testing.T) InvestigatorInput {
	t.Helper()
	runID := RunID(uuid.Must(uuid.NewV7()))
	transitionID := TransitionID(uuid.Must(uuid.NewV7()))
	beadIDStr := "bead-42"
	beadID := BeadID(beadIDStr)
	return InvestigatorInput{
		SnapshotToken: SnapshotToken{
			GitHeadHash:         "abc123def456",
			BeadsAuditEntryID:   "audit-001",
			CapturedAtTimestamp: "2026-05-09T12:00:00Z",
		},
		TargetRunID:           RunID(uuid.Must(uuid.NewV7())),
		TargetWorkflowID:      WorkflowID(uuid.Must(uuid.NewV7())),
		TargetWorkflowVersion: "v1.0.0",
		TargetBeadID:          &beadIDStr,
		BeadRecord: &BeadRecord{
			BeadID:        beadID,
			Title:         "Some task",
			BeadType:      "task",
			Status:        CoarseStatusInProgress,
			Edges:         []DependencyEdge{},
			AuditTrailRef: "audit-trail-001",
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
		PlaybookRef:            "playbook://non-idempotent-inflight",
		BudgetWallClockSeconds: 300,
	}
}

func TestInvestigatorInputValid_AllFieldsSet(t *testing.T) {
	t.Parallel()

	inp := investigatorInputFixture(t)
	if !inp.Valid() {
		t.Error("Valid() = false for fully-populated InvestigatorInput, want true")
	}
}

func TestInvestigatorInputValid_ZeroValue(t *testing.T) {
	t.Parallel()

	inp := InvestigatorInput{}
	if inp.Valid() {
		t.Error("Valid() = true for zero-value InvestigatorInput, want false")
	}
}

func TestInvestigatorInputValid_InvalidSnapshotToken(t *testing.T) {
	t.Parallel()

	inp := investigatorInputFixture(t)
	inp.SnapshotToken = SnapshotToken{} // zero-value: all fields empty
	if inp.Valid() {
		t.Error("Valid() = true with zero SnapshotToken, want false")
	}
}

func TestInvestigatorInputValid_ZeroTargetRunID(t *testing.T) {
	t.Parallel()

	inp := investigatorInputFixture(t)
	inp.TargetRunID = RunID(uuid.Nil)
	if inp.Valid() {
		t.Error("Valid() = true with zero TargetRunID, want false")
	}
}

func TestInvestigatorInputValid_ZeroTargetWorkflowID(t *testing.T) {
	t.Parallel()

	inp := investigatorInputFixture(t)
	inp.TargetWorkflowID = WorkflowID(uuid.Nil)
	if inp.Valid() {
		t.Error("Valid() = true with zero TargetWorkflowID, want false")
	}
}

func TestInvestigatorInputValid_EmptyTargetWorkflowVersion(t *testing.T) {
	t.Parallel()

	inp := investigatorInputFixture(t)
	inp.TargetWorkflowVersion = ""
	if inp.Valid() {
		t.Error("Valid() = true with empty TargetWorkflowVersion, want false")
	}
}

func TestInvestigatorInputValid_NilTargetBeadIDIsValid(t *testing.T) {
	t.Parallel()

	inp := investigatorInputFixture(t)
	inp.TargetBeadID = nil
	inp.BeadRecord = nil
	if !inp.Valid() {
		t.Error("Valid() = false with nil TargetBeadID and nil BeadRecord, want true (both nullable)")
	}
}

func TestInvestigatorInputValid_NilBeadRecordWithBeadIDIsValid(t *testing.T) {
	t.Parallel()

	inp := investigatorInputFixture(t)
	inp.BeadRecord = nil // spec allows BeadRecord | None independently
	if !inp.Valid() {
		t.Error("Valid() = false with nil BeadRecord and non-nil TargetBeadID, want true (BeadRecord nullable)")
	}
}

func TestInvestigatorInputValid_InvalidLastCheckpoint(t *testing.T) {
	t.Parallel()

	inp := investigatorInputFixture(t)
	inp.LastCheckpoint = Checkpoint{} // zero-value: no required fields set
	if inp.Valid() {
		t.Error("Valid() = true with zero-value LastCheckpoint, want false")
	}
}

func TestInvestigatorInputValid_InvalidLastTransition(t *testing.T) {
	t.Parallel()

	inp := investigatorInputFixture(t)
	inp.LastTransition = Transition{} // zero-value
	if inp.Valid() {
		t.Error("Valid() = true with zero-value LastTransition, want false")
	}
}

func TestInvestigatorInputValid_InvalidWorkspaceObservation(t *testing.T) {
	t.Parallel()

	inp := investigatorInputFixture(t)
	inp.WorkspaceObservation = WorkspaceObservation{} // zero-value: Path empty
	if inp.Valid() {
		t.Error("Valid() = true with zero-value WorkspaceObservation, want false")
	}
}

func TestInvestigatorInputValid_NilSessionLogRefIsValid(t *testing.T) {
	t.Parallel()

	inp := investigatorInputFixture(t)
	inp.SessionLogRef = nil
	if !inp.Valid() {
		t.Error("Valid() = false with nil SessionLogRef, want true (nullable)")
	}
}

func TestInvestigatorInputValid_NonNilSessionLogRefIsValid(t *testing.T) {
	t.Parallel()

	inp := investigatorInputFixture(t)
	ref := "cass://session/abc123"
	inp.SessionLogRef = &ref
	if !inp.Valid() {
		t.Error("Valid() = false with non-nil SessionLogRef, want true")
	}
}

func TestInvestigatorInputValid_EmptyCategory(t *testing.T) {
	t.Parallel()

	inp := investigatorInputFixture(t)
	inp.Category = ReconciliationCategory("")
	if inp.Valid() {
		t.Error("Valid() = true with empty Category, want false")
	}
}

func TestInvestigatorInputValid_UnknownCategory(t *testing.T) {
	t.Parallel()

	inp := investigatorInputFixture(t)
	inp.Category = ReconciliationCategory("cat-99")
	if inp.Valid() {
		t.Error("Valid() = true with unknown Category, want false")
	}
}

func TestInvestigatorInputValid_EmptyPlaybookRef(t *testing.T) {
	t.Parallel()

	inp := investigatorInputFixture(t)
	inp.PlaybookRef = ""
	if inp.Valid() {
		t.Error("Valid() = true with empty PlaybookRef, want false")
	}
}

func TestInvestigatorInputValid_ZeroBudget(t *testing.T) {
	t.Parallel()

	inp := investigatorInputFixture(t)
	inp.BudgetWallClockSeconds = 0
	if inp.Valid() {
		t.Error("Valid() = true with zero BudgetWallClockSeconds, want false")
	}
}

func TestInvestigatorInputValid_NegativeBudget(t *testing.T) {
	t.Parallel()

	inp := investigatorInputFixture(t)
	inp.BudgetWallClockSeconds = -1
	if inp.Valid() {
		t.Error("Valid() = true with negative BudgetWallClockSeconds, want false")
	}
}

func TestInvestigatorInputValid_NilJSONLTailIsValid(t *testing.T) {
	t.Parallel()

	inp := investigatorInputFixture(t)
	inp.JSONLTail = nil
	if !inp.Valid() {
		t.Error("Valid() = false with nil JSONLTail, want true (nil slice is valid)")
	}
}
