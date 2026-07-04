// Package evalfizzbuzz provides a FizzBuzz implementation for eval grading.
// This is the pre-committed reference; the model-under-test overwrites it.
package evalfizzbuzz

import "strconv"

// FizzBuzz returns a slice of length n where element i (0-indexed, for i+1):
// multiples of 15 → "FizzBuzz", of 3 → "Fizz", of 5 → "Buzz", else decimal string.
func FizzBuzz(n int) []string {
	out := make([]string, n)
	for i := range out {
		v := i + 1
		switch {
		case v%15 == 0:
			out[i] = "FizzBuzz"
		case v%3 == 0:
			out[i] = "Fizz"
		case v%5 == 0:
			out[i] = "Buzz"
		default:
			out[i] = strconv.Itoa(v)
		}
	}
	return out
}
