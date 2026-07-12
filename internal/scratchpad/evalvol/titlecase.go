package evalvol

import (
	"strings"
	"unicode"
)

// TitleCase capitalizes the first letter of each word in s and lowercases the
// rest, collapsing runs of whitespace between words into a single space.
func TitleCase(s string) string {
	words := strings.Fields(s)
	for i, w := range words {
		runes := []rune(strings.ToLower(w))
		runes[0] = unicode.ToUpper(runes[0])
		words[i] = string(runes)
	}
	return strings.Join(words, " ")
}
