package evalvol

import "testing"

func TestAreAnagrams(t *testing.T) {
	cases := []struct {
		name string
		a    string
		b    string
		want bool
	}{
		{"simple anagram", "listen", "silent", true},
		{"case insensitive", "Listen", "Silent", true},
		{"ignores spaces", "conversation", "voices rant on", true},
		{"not anagram", "hello", "world", false},
		{"different length", "abc", "ab", false},
		{"identical strings", "abc", "abc", true},
		{"empty strings", "", "", true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := AreAnagrams(tc.a, tc.b)
			if got != tc.want {
				t.Errorf("AreAnagrams(%q, %q) = %v, want %v", tc.a, tc.b, got, tc.want)
			}
		})
	}
}
