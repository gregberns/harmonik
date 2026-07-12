package evalvol

import "testing"

func TestTitleCase(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"simple", "hello world", "Hello World"},
		{"already upper", "HELLO WORLD", "Hello World"},
		{"mixed case", "hELLo WoRLD", "Hello World"},
		{"collapses spaces", "hello    world", "Hello World"},
		{"leading and trailing spaces", "  hello world  ", "Hello World"},
		{"single word", "hello", "Hello"},
		{"empty string", "", ""},
		{"whitespace only", "   ", ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := TitleCase(tc.in)
			if got != tc.want {
				t.Errorf("TitleCase(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
