package core

import "testing"

// validBeadRecord returns a fully-populated valid BeadRecord for use in tests.
func validBeadRecord(t *testing.T) BeadRecord {
	t.Helper()
	return BeadRecord{
		BeadID:      BeadID("bead-abc-123"),
		Title:       "Implement reconciliation loop",
		Description: "Extended description (optional)",
		BeadType:    "task",
		Status:      CoarseStatusOpen,
		Edges: []DependencyEdge{
			{
				FromBeadID: BeadID("bead-abc-123"),
				ToBeadID:   BeadID("bead-def-456"),
				EdgeKind:   EdgeKindBlocks,
			},
		},
		AuditTrailRef: "audit-ref-opaque-handle",
	}
}

func TestBeadRecordValid_HappyPath(t *testing.T) {
	t.Parallel()

	r := validBeadRecord(t)
	if !r.Valid() {
		t.Error("Valid() = false for fully-populated BeadRecord, want true")
	}
}

func TestBeadRecordValid_EmptyEdgesSliceIsValid(t *testing.T) {
	t.Parallel()

	r := validBeadRecord(t)
	r.Edges = []DependencyEdge{}
	if !r.Valid() {
		t.Error("Valid() = false with empty Edges slice, want true")
	}
}

func TestBeadRecordValid_NilEdgesSliceIsValid(t *testing.T) {
	t.Parallel()

	r := validBeadRecord(t)
	r.Edges = nil
	if !r.Valid() {
		t.Error("Valid() = false with nil Edges slice, want true")
	}
}

func TestBeadRecordValid_DescriptionEmptyIsValid(t *testing.T) {
	t.Parallel()

	r := validBeadRecord(t)
	r.Description = ""
	if !r.Valid() {
		t.Error("Valid() = false with empty Description, want true (Description is optional)")
	}
}

func TestBeadRecordValid_RejectionCases(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		mutate func(*BeadRecord)
	}{
		{
			name:   "empty BeadID rejected",
			mutate: func(r *BeadRecord) { r.BeadID = "" },
		},
		{
			name:   "empty Title rejected",
			mutate: func(r *BeadRecord) { r.Title = "" },
		},
		{
			name:   "empty BeadType rejected",
			mutate: func(r *BeadRecord) { r.BeadType = "" },
		},
		{
			name:   "invalid Status rejected",
			mutate: func(r *BeadRecord) { r.Status = CoarseStatus("unknown-status") },
		},
		{
			name:   "empty AuditTrailRef rejected",
			mutate: func(r *BeadRecord) { r.AuditTrailRef = "" },
		},
		{
			name: "invalid edge in Edges slice rejected",
			mutate: func(r *BeadRecord) {
				r.Edges = []DependencyEdge{
					// valid edge
					{
						FromBeadID: BeadID("bead-abc-123"),
						ToBeadID:   BeadID("bead-def-456"),
						EdgeKind:   EdgeKindBlocks,
					},
					// invalid edge: empty FromBeadID
					{
						FromBeadID: BeadID(""),
						ToBeadID:   BeadID("bead-def-456"),
						EdgeKind:   EdgeKindBlocks,
					},
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			r := validBeadRecord(t)
			tc.mutate(&r)
			if r.Valid() {
				t.Errorf("Valid() = true after %q mutation, want false", tc.name)
			}
		})
	}
}
