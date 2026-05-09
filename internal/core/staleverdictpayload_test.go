package core

import "testing"

// staleVerdictPayloadFixture returns a fully-populated StaleVerdictPayload
// with all required fields set to valid non-empty values. Tests mutate
// individual fields to probe Valid().
func staleVerdictPayloadFixture(t *testing.T) StaleVerdictPayload {
	t.Helper()
	return StaleVerdictPayload{
		SnapshotToken: SnapshotToken{
			GitHeadHash:         "abc123def456",
			BeadsAuditEntryID:   "audit-001",
			CapturedAtTimestamp: "2026-05-08T00:00:00Z",
		},
		CurrentGitHeadHash:  "def456ghi789",
		CurrentBeadsAuditID: "audit-002",
		DivergenceReason:    StaleDivergenceReasonGitBranchAdvanced,
	}
}

func TestStaleVerdictPayloadValid_AllFieldsSet(t *testing.T) {
	t.Parallel()

	p := staleVerdictPayloadFixture(t)
	if !p.Valid() {
		t.Error("Valid() = false for fully-populated StaleVerdictPayload, want true")
	}
}

func TestStaleVerdictPayloadValid_AllDivergenceReasons(t *testing.T) {
	t.Parallel()

	reasons := []StaleDivergenceReason{
		StaleDivergenceReasonGitBranchAdvanced,
		StaleDivergenceReasonBeadsAuditAdvanced,
	}
	for _, r := range reasons {
		r := r
		t.Run(string(r), func(t *testing.T) {
			t.Parallel()
			p := staleVerdictPayloadFixture(t)
			p.DivergenceReason = r
			if !p.Valid() {
				t.Errorf("Valid() = false for divergence_reason=%q, want true", r)
			}
		})
	}
}

func TestStaleVerdictPayloadValid_InvalidSnapshotToken(t *testing.T) {
	t.Parallel()

	p := staleVerdictPayloadFixture(t)
	p.SnapshotToken = SnapshotToken{} // zero value: all fields empty
	if p.Valid() {
		t.Error("Valid() = true with zero SnapshotToken, want false")
	}
}

func TestStaleVerdictPayloadValid_EmptySnapshotGitHeadHash(t *testing.T) {
	t.Parallel()

	p := staleVerdictPayloadFixture(t)
	p.SnapshotToken.GitHeadHash = ""
	if p.Valid() {
		t.Error("Valid() = true with empty SnapshotToken.GitHeadHash, want false")
	}
}

func TestStaleVerdictPayloadValid_EmptySnapshotBeadsAuditEntryID(t *testing.T) {
	t.Parallel()

	p := staleVerdictPayloadFixture(t)
	p.SnapshotToken.BeadsAuditEntryID = ""
	if p.Valid() {
		t.Error("Valid() = true with empty SnapshotToken.BeadsAuditEntryID, want false")
	}
}

func TestStaleVerdictPayloadValid_EmptySnapshotCapturedAtTimestamp(t *testing.T) {
	t.Parallel()

	p := staleVerdictPayloadFixture(t)
	p.SnapshotToken.CapturedAtTimestamp = ""
	if p.Valid() {
		t.Error("Valid() = true with empty SnapshotToken.CapturedAtTimestamp, want false")
	}
}

func TestStaleVerdictPayloadValid_EmptyCurrentGitHeadHash(t *testing.T) {
	t.Parallel()

	p := staleVerdictPayloadFixture(t)
	p.CurrentGitHeadHash = ""
	if p.Valid() {
		t.Error("Valid() = true with empty CurrentGitHeadHash, want false")
	}
}

func TestStaleVerdictPayloadValid_EmptyCurrentBeadsAuditID(t *testing.T) {
	t.Parallel()

	p := staleVerdictPayloadFixture(t)
	p.CurrentBeadsAuditID = ""
	if p.Valid() {
		t.Error("Valid() = true with empty CurrentBeadsAuditID, want false")
	}
}

func TestStaleVerdictPayloadValid_EmptyDivergenceReason(t *testing.T) {
	t.Parallel()

	p := staleVerdictPayloadFixture(t)
	p.DivergenceReason = StaleDivergenceReason("")
	if p.Valid() {
		t.Error("Valid() = true with empty DivergenceReason, want false")
	}
}

func TestStaleVerdictPayloadValid_UnknownDivergenceReason(t *testing.T) {
	t.Parallel()

	p := staleVerdictPayloadFixture(t)
	p.DivergenceReason = StaleDivergenceReason("not-a-reason")
	if p.Valid() {
		t.Error("Valid() = true with unknown DivergenceReason, want false")
	}
}
