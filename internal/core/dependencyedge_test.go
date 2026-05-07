package core

import "testing"

func TestDependencyEdgeValid(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		edge      DependencyEdge
		wantValid bool
	}{
		{
			name: "valid edge",
			edge: DependencyEdge{
				FromBeadID: BeadID("bead-1"),
				ToBeadID:   BeadID("bead-2"),
				EdgeKind:   EdgeKindParentChild,
			},
			wantValid: true,
		},
		{
			name: "valid edge blocks kind",
			edge: DependencyEdge{
				FromBeadID: BeadID("bead-a"),
				ToBeadID:   BeadID("bead-b"),
				EdgeKind:   EdgeKindBlocks,
			},
			wantValid: true,
		},
		{
			name: "valid edge conditional-blocks kind",
			edge: DependencyEdge{
				FromBeadID: BeadID("bead-a"),
				ToBeadID:   BeadID("bead-b"),
				EdgeKind:   EdgeKindConditionalBlocks,
			},
			wantValid: true,
		},
		{
			name: "valid edge waits-for kind",
			edge: DependencyEdge{
				FromBeadID: BeadID("bead-a"),
				ToBeadID:   BeadID("bead-b"),
				EdgeKind:   EdgeKindWaitsFor,
			},
			wantValid: true,
		},
		{
			name: "empty FromBeadID rejected",
			edge: DependencyEdge{
				FromBeadID: BeadID(""),
				ToBeadID:   BeadID("bead-2"),
				EdgeKind:   EdgeKindParentChild,
			},
			wantValid: false,
		},
		{
			name: "empty ToBeadID rejected",
			edge: DependencyEdge{
				FromBeadID: BeadID("bead-1"),
				ToBeadID:   BeadID(""),
				EdgeKind:   EdgeKindParentChild,
			},
			wantValid: false,
		},
		{
			name: "invalid EdgeKind rejected",
			edge: DependencyEdge{
				FromBeadID: BeadID("bead-1"),
				ToBeadID:   BeadID("bead-2"),
				EdgeKind:   EdgeKind("unknown"),
			},
			wantValid: false,
		},
		{
			name:      "zero value rejected",
			edge:      DependencyEdge{},
			wantValid: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := tc.edge.Valid()
			if got != tc.wantValid {
				t.Errorf("DependencyEdge%+v.Valid() = %v, want %v", tc.edge, got, tc.wantValid)
			}
		})
	}
}
