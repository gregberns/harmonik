package evalvol

import (
	"reflect"
	"testing"
)

func TestFlattenInts(t *testing.T) {
	tests := []struct {
		name   string
		nested [][]int
		want   []int
	}{
		{
			name:   "basic",
			nested: [][]int{{1, 2}, {3}, {4, 5, 6}},
			want:   []int{1, 2, 3, 4, 5, 6},
		},
		{
			name:   "empty inners",
			nested: [][]int{{}, {1}, {}, {2, 3}, {}},
			want:   []int{1, 2, 3},
		},
		{
			name:   "all empty",
			nested: [][]int{{}, {}},
			want:   []int{},
		},
		{
			name:   "no inners",
			nested: [][]int{},
			want:   []int{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FlattenInts(tt.nested)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("FlattenInts(%v) = %v, want %v", tt.nested, got, tt.want)
			}
		})
	}
}
