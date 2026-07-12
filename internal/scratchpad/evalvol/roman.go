package evalvol

import "fmt"

// ToRoman converts n to its Roman numeral representation.
// Returns an error if n is outside the range 1..3999.
func ToRoman(n int) (string, error) {
	if n < 1 || n > 3999 {
		return "", fmt.Errorf("ToRoman: %d out of range [1, 3999]", n)
	}
	vals := []int{1000, 900, 500, 400, 100, 90, 50, 40, 10, 9, 5, 4, 1}
	syms := []string{"M", "CM", "D", "CD", "C", "XC", "L", "XL", "X", "IX", "V", "IV", "I"}
	var result string
	for i, v := range vals {
		for n >= v {
			result += syms[i]
			n -= v
		}
	}
	return result, nil
}
