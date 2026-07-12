package evalvol

// FlattenInts flattens nested into a single slice, preserving order.
func FlattenInts(nested [][]int) []int {
	result := []int{}
	for _, inner := range nested {
		result = append(result, inner...)
	}
	return result
}
