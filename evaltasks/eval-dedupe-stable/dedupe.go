// Package evaldedupestable provides stable-order deduplication for eval grading.
// This is the pre-committed reference; the model-under-test overwrites it.
package evaldedupestable

// Dedupe returns a new slice with duplicates removed, preserving first-seen order.
// A nil or empty input returns an empty (len 0) slice.
func Dedupe(in []int) []int {
	seen := make(map[int]bool, len(in))
	out := make([]int, 0, len(in))
	for _, v := range in {
		if !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	return out
}
