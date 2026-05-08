package core

import (
	"testing"

	"github.com/google/uuid"
)

// b3f93VerdictEventValid returns a fully-populated VerdictEvent using
// VerdictResumeHere (no context, no checkpoint_ref required) with all
// required fields set to valid values. Tests mutate individual fields to
// probe Valid().
func b3f93VerdictEventValid(t *testing.T) VerdictEvent {
	t.Helper()
	return VerdictEvent{
		Verdict:           VerdictResumeHere,
		InvestigatorRunID: uuid.Must(uuid.NewV7()),
		TargetRunID:       uuid.Must(uuid.NewV7()),
		EvidenceRef:       nil,
		Context:           nil,
		CheckpointRef:     nil,
		SnapshotToken: SnapshotToken{
			GitHeadHash:         "abc123def456",
			BeadsAuditEntryID:   "audit-001",
			CapturedAtTimestamp: "2026-05-08T00:00:00Z",
		},
		SchemaVersion: 1,
	}
}

// b3f93VerdictEventResumeWithContext returns a valid VerdictEvent with
// Verdict=VerdictResumeWithContext and a non-empty Context.
func b3f93VerdictEventResumeWithContext(t *testing.T) VerdictEvent {
	t.Helper()
	e := b3f93VerdictEventValid(t)
	ctx := "investigator context text"
	e.Verdict = VerdictResumeWithContext
	e.Context = &ctx
	return e
}

// b3f93VerdictEventResetToCheckpoint returns a valid VerdictEvent with
// Verdict=VerdictResetToCheckpoint and a non-nil CheckpointRef.
func b3f93VerdictEventResetToCheckpoint(t *testing.T) VerdictEvent {
	t.Helper()
	e := b3f93VerdictEventValid(t)
	tid := TransitionID(uuid.Must(uuid.NewV7()))
	e.Verdict = VerdictResetToCheckpoint
	e.CheckpointRef = &tid
	return e
}

// --- AllValid tests ---

func TestVerdictEventValid_ResumeHere(t *testing.T) {
	t.Parallel()

	e := b3f93VerdictEventValid(t)
	if !e.Valid() {
		t.Error("Valid() = false for VerdictResumeHere event, want true")
	}
}

func TestVerdictEventValid_ResumeWithContext(t *testing.T) {
	t.Parallel()

	e := b3f93VerdictEventResumeWithContext(t)
	if !e.Valid() {
		t.Error("Valid() = false for VerdictResumeWithContext event, want true")
	}
}

func TestVerdictEventValid_ResetToCheckpoint(t *testing.T) {
	t.Parallel()

	e := b3f93VerdictEventResetToCheckpoint(t)
	if !e.Valid() {
		t.Error("Valid() = false for VerdictResetToCheckpoint event, want true")
	}
}

func TestVerdictEventValid_AllOtherVerdicts(t *testing.T) {
	t.Parallel()

	verdicts := []Verdict{
		VerdictReopenBead,
		VerdictAcceptCloseWithNote,
		VerdictNoOpAccept,
		VerdictEscalateToHuman,
	}
	for _, v := range verdicts {
		v := v
		t.Run(string(v), func(t *testing.T) {
			t.Parallel()
			e := b3f93VerdictEventValid(t)
			e.Verdict = v
			if !e.Valid() {
				t.Errorf("Valid() = false for verdict=%q, want true", v)
			}
		})
	}
}

// --- Verdict field ---

func TestVerdictEventValid_EmptyVerdict(t *testing.T) {
	t.Parallel()

	e := b3f93VerdictEventValid(t)
	e.Verdict = Verdict("")
	if e.Valid() {
		t.Error("Valid() = true with empty Verdict, want false")
	}
}

func TestVerdictEventValid_UnknownVerdict(t *testing.T) {
	t.Parallel()

	e := b3f93VerdictEventValid(t)
	e.Verdict = Verdict("not-a-verdict")
	if e.Valid() {
		t.Error("Valid() = true with unknown Verdict, want false")
	}
}

// --- UUID fields ---

func TestVerdictEventValid_ZeroInvestigatorRunID(t *testing.T) {
	t.Parallel()

	e := b3f93VerdictEventValid(t)
	e.InvestigatorRunID = uuid.Nil
	if e.Valid() {
		t.Error("Valid() = true with zero InvestigatorRunID, want false")
	}
}

func TestVerdictEventValid_ZeroTargetRunID(t *testing.T) {
	t.Parallel()

	e := b3f93VerdictEventValid(t)
	e.TargetRunID = uuid.Nil
	if e.Valid() {
		t.Error("Valid() = true with zero TargetRunID, want false")
	}
}

// --- RC-022a: context non-empty iff verdict=resume-with-context ---

func TestVerdictEventValid_ResumeWithContextMissingContext(t *testing.T) {
	t.Parallel()

	// verdict=resume-with-context but Context is nil — must be rejected.
	e := b3f93VerdictEventResumeWithContext(t)
	e.Context = nil
	if e.Valid() {
		t.Error("Valid() = true with resume-with-context and nil Context, want false (RC-022a)")
	}
}

func TestVerdictEventValid_ResumeWithContextEmptyContext(t *testing.T) {
	t.Parallel()

	// verdict=resume-with-context but Context is empty string — must be rejected.
	e := b3f93VerdictEventResumeWithContext(t)
	empty := ""
	e.Context = &empty
	if e.Valid() {
		t.Error("Valid() = true with resume-with-context and empty Context, want false (RC-022a)")
	}
}

func TestVerdictEventValid_NonResumeWithContext_ContextPresent(t *testing.T) {
	t.Parallel()

	// verdict=resume-here but Context is set — must be rejected (RC-022a: MUST be empty otherwise).
	e := b3f93VerdictEventValid(t)
	ctx := "unexpected context"
	e.Context = &ctx
	if e.Valid() {
		t.Error("Valid() = true with resume-here and non-nil Context, want false (RC-022a)")
	}
}

func TestVerdictEventValid_OtherVerdicts_ContextMustBeAbsent(t *testing.T) {
	t.Parallel()

	ctx := "context that should not be here"
	verdicts := []Verdict{
		VerdictResetToCheckpoint,
		VerdictReopenBead,
		VerdictAcceptCloseWithNote,
		VerdictNoOpAccept,
		VerdictEscalateToHuman,
	}
	for _, v := range verdicts {
		v := v
		t.Run(string(v), func(t *testing.T) {
			t.Parallel()
			e := b3f93VerdictEventValid(t)
			e.Verdict = v
			if v == VerdictResetToCheckpoint {
				// also set required checkpoint_ref to avoid that failure
				tid := TransitionID(uuid.Must(uuid.NewV7()))
				e.CheckpointRef = &tid
			}
			e.Context = &ctx
			if e.Valid() {
				t.Errorf("Valid() = true for verdict=%q with non-nil Context, want false (RC-022a)", v)
			}
		})
	}
}

// --- checkpoint_ref non-nil iff verdict=reset-to-checkpoint ---

func TestVerdictEventValid_ResetToCheckpointMissingRef(t *testing.T) {
	t.Parallel()

	e := b3f93VerdictEventResetToCheckpoint(t)
	e.CheckpointRef = nil
	if e.Valid() {
		t.Error("Valid() = true with reset-to-checkpoint and nil CheckpointRef, want false")
	}
}

func TestVerdictEventValid_NonResetToCheckpoint_CheckpointRefPresent(t *testing.T) {
	t.Parallel()

	// verdict=resume-here but CheckpointRef is non-nil — must be rejected.
	e := b3f93VerdictEventValid(t)
	tid := TransitionID(uuid.Must(uuid.NewV7()))
	e.CheckpointRef = &tid
	if e.Valid() {
		t.Error("Valid() = true with resume-here and non-nil CheckpointRef, want false")
	}
}

func TestVerdictEventValid_OtherVerdicts_CheckpointRefMustBeAbsent(t *testing.T) {
	t.Parallel()

	verdicts := []Verdict{
		VerdictResumeHere,
		VerdictReopenBead,
		VerdictAcceptCloseWithNote,
		VerdictNoOpAccept,
		VerdictEscalateToHuman,
	}
	for _, v := range verdicts {
		v := v
		t.Run(string(v), func(t *testing.T) {
			t.Parallel()
			e := b3f93VerdictEventValid(t)
			e.Verdict = v
			tid := TransitionID(uuid.Must(uuid.NewV7()))
			e.CheckpointRef = &tid
			if e.Valid() {
				t.Errorf("Valid() = true for verdict=%q with non-nil CheckpointRef, want false", v)
			}
		})
	}
}

// --- SchemaVersion ---

func TestVerdictEventValid_ZeroSchemaVersion(t *testing.T) {
	t.Parallel()

	e := b3f93VerdictEventValid(t)
	e.SchemaVersion = 0
	if e.Valid() {
		t.Error("Valid() = true with SchemaVersion=0, want false")
	}
}

func TestVerdictEventValid_NegativeSchemaVersion(t *testing.T) {
	t.Parallel()

	e := b3f93VerdictEventValid(t)
	e.SchemaVersion = -1
	if e.Valid() {
		t.Error("Valid() = true with negative SchemaVersion, want false")
	}
}

// --- EvidenceRef is optional ---

func TestVerdictEventValid_EvidenceRefNil(t *testing.T) {
	t.Parallel()

	e := b3f93VerdictEventValid(t)
	e.EvidenceRef = nil
	if !e.Valid() {
		t.Error("Valid() = false with nil EvidenceRef, want true (optional field)")
	}
}

func TestVerdictEventValid_EvidenceRefPresent(t *testing.T) {
	t.Parallel()

	e := b3f93VerdictEventValid(t)
	ref := "deadbeefcafe"
	e.EvidenceRef = &ref
	if !e.Valid() {
		t.Error("Valid() = false with non-nil EvidenceRef, want true")
	}
}
