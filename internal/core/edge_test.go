package core

import "testing"

func ptr[T any](v T) *T { return &v }

func TestEdgeValid(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		edge Edge
		want bool
	}{
		{
			name: "valid minimal edge",
			edge: Edge{
				FromNode:    "node-a",
				ToNode:      "node-b",
				OrderingKey: "0",
			},
			want: true,
		},
		{
			name: "valid full edge all optionals set",
			edge: Edge{
				FromNode:       "node-a",
				ToNode:         "node-b",
				Condition:      ptr("outcome == 'success'"),
				Label:          ptr("success"),
				PreferredLabel: ptr("preferred-success"),
				Weight:         5,
				OrderingKey:    "a",
				TraversalCap:   ptr(3),
			},
			want: true,
		},
		{
			name: "empty FromNode rejected",
			edge: Edge{
				FromNode:    "",
				ToNode:      "node-b",
				OrderingKey: "0",
			},
			want: false,
		},
		{
			name: "empty ToNode rejected",
			edge: Edge{
				FromNode:    "node-a",
				ToNode:      "",
				OrderingKey: "0",
			},
			want: false,
		},
		{
			name: "empty OrderingKey rejected",
			edge: Edge{
				FromNode:    "node-a",
				ToNode:      "node-b",
				OrderingKey: "",
			},
			want: false,
		},
		{
			name: "TraversalCap zero rejected",
			edge: Edge{
				FromNode:     "node-a",
				ToNode:       "node-b",
				OrderingKey:  "0",
				TraversalCap: ptr(0),
			},
			want: false,
		},
		{
			name: "TraversalCap negative rejected",
			edge: Edge{
				FromNode:     "node-a",
				ToNode:       "node-b",
				OrderingKey:  "0",
				TraversalCap: ptr(-1),
			},
			want: false,
		},
		{
			name: "TraversalCap one accepted",
			edge: Edge{
				FromNode:     "node-a",
				ToNode:       "node-b",
				OrderingKey:  "0",
				TraversalCap: ptr(1),
			},
			want: true,
		},
		{
			name: "Weight zero accepted",
			edge: Edge{
				FromNode:    "node-a",
				ToNode:      "node-b",
				OrderingKey: "0",
				Weight:      0,
			},
			want: true,
		},
		{
			name: "Weight negative accepted",
			edge: Edge{
				FromNode:    "node-a",
				ToNode:      "node-b",
				OrderingKey: "0",
				Weight:      -1,
			},
			want: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := tc.edge.Valid()
			if got != tc.want {
				t.Errorf("Edge.Valid() = %v, want %v (edge: %+v)", got, tc.want, tc.edge)
			}
		})
	}
}
