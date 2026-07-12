package evalvol

import "sort"

// UniqueSorted returns the distinct values of xs in ascending order.
func UniqueSorted(xs []int) []int {
	seen := make(map[int]bool, len(xs))
	out := make([]int, 0, len(xs))
	for _, x := range xs {
		if !seen[x] {
			seen[x] = true
			out = append(out, x)
		}
	}
	sort.Ints(out)
	return out
}
