// Package schedule provides a greedy interval scheduler (activity selection).
//
// Two intervals [a,b] and [c,d] overlap iff c < b (strict). Touching
// endpoints are NOT overlapping: [1,2] and [2,3] may both be selected.
//
// Tie-break: equal end times → prefer later start (shorter duration first).
package schedule

import "sort"

// Interval is a closed interval [Start, End].
type Interval struct {
	Start, End int
}

// Schedule returns the maximum-cardinality set of non-overlapping intervals,
// ordered by end time. The input slice is not modified.
func Schedule(intervals []Interval) []Interval {
	if len(intervals) == 0 {
		return []Interval{}
	}
	sorted := make([]Interval, len(intervals))
	copy(sorted, intervals)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].End != sorted[j].End {
			return sorted[i].End < sorted[j].End
		}
		return sorted[i].Start > sorted[j].Start
	})
	result := []Interval{sorted[0]}
	lastEnd := sorted[0].End
	for _, iv := range sorted[1:] {
		if iv.Start >= lastEnd {
			result = append(result, iv)
			lastEnd = iv.End
		}
	}
	return result
}
