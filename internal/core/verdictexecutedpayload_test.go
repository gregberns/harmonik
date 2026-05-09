package core

import (
	"testing"

	"github.com/google/uuid"
)

// verdictExecutedPayloadFixture returns a fully-populated VerdictExecutedPayload
// with all required fields set to valid non-empty values. Tests mutate
// individual fields to probe Valid().
func verdictExecutedPayloadFixture(t *testing.T) VerdictExecutedPayload {
	t.Helper()
	return VerdictExecutedPayload{
		InvestigatorRunID:   uuid.Must(uuid.NewV7()),
		TargetRunID:         uuid.Must(uuid.NewV7()),
		Verdict:             VerdictResumeHere,
		ExecutedAtTimestamp: "2026-05-08T00:00:00Z",
		ActionSummary:       "re-dispatched outer run current node",
	}
}

func TestVerdictExecutedPayloadValid_AllFieldsSet(t *testing.T) {
	t.Parallel()

	p := verdictExecutedPayloadFixture(t)
	if !p.Valid() {
		t.Error("Valid() = false for fully-populated VerdictExecutedPayload, want true")
	}
}

func TestVerdictExecutedPayloadValid_AllVerdicts(t *testing.T) {
	t.Parallel()

	verdicts := []Verdict{
		VerdictResumeHere,
		VerdictResumeWithContext,
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
			p := verdictExecutedPayloadFixture(t)
			p.Verdict = v
			if !p.Valid() {
				t.Errorf("Valid() = false for verdict=%q, want true", v)
			}
		})
	}
}

func TestVerdictExecutedPayloadValid_ZeroInvestigatorRunID(t *testing.T) {
	t.Parallel()

	p := verdictExecutedPayloadFixture(t)
	p.InvestigatorRunID = uuid.Nil
	if p.Valid() {
		t.Error("Valid() = true with zero InvestigatorRunID, want false")
	}
}

func TestVerdictExecutedPayloadValid_ZeroTargetRunID(t *testing.T) {
	t.Parallel()

	p := verdictExecutedPayloadFixture(t)
	p.TargetRunID = uuid.Nil
	if p.Valid() {
		t.Error("Valid() = true with zero TargetRunID, want false")
	}
}

func TestVerdictExecutedPayloadValid_EmptyVerdict(t *testing.T) {
	t.Parallel()

	p := verdictExecutedPayloadFixture(t)
	p.Verdict = Verdict("")
	if p.Valid() {
		t.Error("Valid() = true with empty Verdict, want false")
	}
}

func TestVerdictExecutedPayloadValid_UnknownVerdict(t *testing.T) {
	t.Parallel()

	p := verdictExecutedPayloadFixture(t)
	p.Verdict = Verdict("not-a-verdict")
	if p.Valid() {
		t.Error("Valid() = true with unknown Verdict, want false")
	}
}

func TestVerdictExecutedPayloadValid_EmptyExecutedAtTimestamp(t *testing.T) {
	t.Parallel()

	p := verdictExecutedPayloadFixture(t)
	p.ExecutedAtTimestamp = ""
	if p.Valid() {
		t.Error("Valid() = true with empty ExecutedAtTimestamp, want false")
	}
}

func TestVerdictExecutedPayloadValid_EmptyActionSummary(t *testing.T) {
	t.Parallel()

	p := verdictExecutedPayloadFixture(t)
	p.ActionSummary = ""
	if p.Valid() {
		t.Error("Valid() = true with empty ActionSummary, want false")
	}
}
