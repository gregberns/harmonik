// Package evalparseintsafe provides a panic-safe integer parse for eval grading.
// This is the pre-committed reference; the model-under-test overwrites it.
package evalparseintsafe

import "strconv"

// ParseIntOr parses s as a base-10 integer and returns it.
// On empty input, non-numeric input, or overflow it returns def without panicking.
func ParseIntOr(s string, def int) int {
	v, err := strconv.Atoi(s)
	if err != nil {
		return def
	}
	return v
}
