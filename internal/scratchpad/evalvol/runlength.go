package evalvol

import (
	"fmt"
	"strconv"
	"strings"
)

// Encode run-length encodes s as a sequence of <count><char> pairs, e.g. "aaabcc" -> "3a1b2c".
func Encode(s string) string {
	var b strings.Builder
	runes := []rune(s)
	for i := 0; i < len(runes); {
		j := i
		for j < len(runes) && runes[j] == runes[i] {
			j++
		}
		b.WriteString(strconv.Itoa(j - i))
		b.WriteRune(runes[i])
		i = j
	}
	return b.String()
}

// Decode reverses Encode, expanding <count><char> pairs back into the original string.
func Decode(s string) (string, error) {
	var b strings.Builder
	runes := []rune(s)
	i := 0
	for i < len(runes) {
		start := i
		for i < len(runes) && runes[i] >= '0' && runes[i] <= '9' {
			i++
		}
		if i == start {
			return "", fmt.Errorf("runlength: expected digit at position %d", start)
		}
		count, err := strconv.Atoi(string(runes[start:i]))
		if err != nil {
			return "", fmt.Errorf("runlength: invalid count %q: %w", string(runes[start:i]), err)
		}
		if i >= len(runes) {
			return "", fmt.Errorf("runlength: missing character after count at position %d", i)
		}
		c := runes[i]
		i++
		b.WriteString(strings.Repeat(string(c), count))
	}
	return b.String(), nil
}
