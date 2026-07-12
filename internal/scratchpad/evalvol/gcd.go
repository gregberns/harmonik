package evalvol

// GCD returns the greatest common divisor of a and b using Euclidean algorithm.
// GCD(0, n) == n; GCD of negatives uses absolute values.
func GCD(a, b int) int {
	if a < 0 {
		a = -a
	}
	if b < 0 {
		b = -b
	}
	for b != 0 {
		a, b = b, a%b
	}
	return a
}

// LCM returns the least common multiple of a and b.
func LCM(a, b int) int {
	if a == 0 || b == 0 {
		return 0
	}
	result := a / GCD(a, b) * b
	if result < 0 {
		return -result
	}
	return result
}
