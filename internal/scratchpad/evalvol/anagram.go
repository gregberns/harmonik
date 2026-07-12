package evalvol

import (
	"sort"
	"strings"
)

// AreAnagrams reports whether a and b are anagrams of each other,
// ignoring case and spaces.
func AreAnagrams(a, b string) bool {
	return normalizeForAnagram(a) == normalizeForAnagram(b)
}

func normalizeForAnagram(s string) string {
	s = strings.ToLower(strings.ReplaceAll(s, " ", ""))
	chars := strings.Split(s, "")
	sort.Strings(chars)
	return strings.Join(chars, "")
}
