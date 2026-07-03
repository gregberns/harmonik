package schedule

import (
	"reflect"
	"testing"
)

// TestSchedule is the tamper-proof acceptance suite for the greedy interval scheduler.
// Implementers must NOT edit this file — only schedule.go.
//
// Overlap rule: [a,b] and [c,d] overlap iff c < b (strict). Touching endpoints
// (e.g. [1,2] and [2,3]) are NOT overlapping and may both be selected.
//
// Tie-break rule: when two intervals share an end time, prefer the one with the
// later start (shorter duration). This yields a unique, deterministic result.
func TestSchedule(t *testing.T) {
	cases := []struct {
		name string
		in   []Interval
		want []Interval
	}{
		{
			name: "empty input",
			in:   []Interval{},
			want: []Interval{},
		},
		{
			name: "single interval",
			in:   []Interval{{1, 2}},
			want: []Interval{{1, 2}},
		},
		{
			name: "all non-overlapping sorted",
			in:   []Interval{{1, 2}, {3, 4}, {5, 6}},
			want: []Interval{{1, 2}, {3, 4}, {5, 6}},
		},
		{
			name: "touching endpoints are not overlapping",
			in:   []Interval{{1, 2}, {2, 3}, {3, 4}},
			want: []Interval{{1, 2}, {2, 3}, {3, 4}},
		},
		{
			name: "unsorted input",
			in:   []Interval{{3, 5}, {1, 2}, {2, 3}},
			want: []Interval{{1, 2}, {2, 3}, {3, 5}},
		},
		{
			name: "basic overlap evicts middle",
			in:   []Interval{{1, 3}, {2, 4}, {3, 5}},
			want: []Interval{{1, 3}, {3, 5}},
		},
		{
			name: "large span loses to two short spans",
			in:   []Interval{{0, 10}, {1, 2}, {3, 4}},
			want: []Interval{{1, 2}, {3, 4}},
		},
		{
			name: "all overlap same end tie-break picks latest start",
			in:   []Interval{{1, 5}, {2, 5}, {3, 5}, {4, 5}},
			want: []Interval{{4, 5}},
		},
		{
			name: "tie-break shorter duration allows successor",
			in:   []Interval{{1, 5}, {3, 5}, {5, 7}},
			want: []Interval{{3, 5}, {5, 7}},
		},
		{
			name: "duplicates yield one",
			in:   []Interval{{1, 2}, {1, 2}, {1, 2}},
			want: []Interval{{1, 2}},
		},
		{
			name: "negative coordinates",
			in:   []Interval{{-3, -1}, {-2, 0}, {0, 1}},
			want: []Interval{{-3, -1}, {0, 1}},
		},
		{
			name: "classic activity selection (11 intervals)",
			in: []Interval{
				{1, 4}, {3, 5}, {0, 6}, {5, 7},
				{3, 8}, {5, 9}, {6, 10}, {8, 11},
				{8, 12}, {2, 13}, {12, 14},
			},
			want: []Interval{{1, 4}, {5, 7}, {8, 11}, {12, 14}},
		},
		{
			name: "zero-length interval (point)",
			in:   []Interval{{3, 3}, {1, 3}, {3, 5}},
			want: []Interval{{3, 3}, {3, 5}},
		},
		{
			name: "mixed negatives and positives",
			in:   []Interval{{-5, -1}, {-1, 2}, {1, 3}, {3, 6}},
			want: []Interval{{-5, -1}, {-1, 2}, {3, 6}},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Schedule(tc.in)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("Schedule(%v)\n  got  %v\n  want %v", tc.in, got, tc.want)
			}
		})
	}
}
