package core

import "testing"

// snapshotTokenFixture returns a fully-populated SnapshotToken with all
// required fields set to valid non-empty values. Tests mutate individual
// fields to probe Valid().
func snapshotTokenFixture(t *testing.T) SnapshotToken {
	t.Helper()
	return SnapshotToken{
		GitHeadHash:         "abc123def456",
		BeadsAuditEntryID:   "audit-001",
		CapturedAtTimestamp: "2026-05-08T00:00:00Z",
	}
}

func TestSnapshotTokenValid_AllFieldsSet(t *testing.T) {
	t.Parallel()

	tok := snapshotTokenFixture(t)
	if !tok.Valid() {
		t.Error("Valid() = false for fully-populated SnapshotToken, want true")
	}
}

func TestSnapshotTokenValid_MissingField(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		mutFn func(*SnapshotToken)
	}{
		{
			name:  "empty GitHeadHash",
			mutFn: func(s *SnapshotToken) { s.GitHeadHash = "" },
		},
		{
			name:  "empty BeadsAuditEntryID",
			mutFn: func(s *SnapshotToken) { s.BeadsAuditEntryID = "" },
		},
		{
			name:  "empty CapturedAtTimestamp",
			mutFn: func(s *SnapshotToken) { s.CapturedAtTimestamp = "" },
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			tok := snapshotTokenFixture(t)
			tc.mutFn(&tok)
			if tok.Valid() {
				t.Errorf("Valid() = true with %s, want false", tc.name)
			}
		})
	}
}
