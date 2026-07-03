package evalstringreverse

import "testing"

func TestReverse(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"", ""},
		{"abc", "cba"},
		{"a🙂b", "b🙂a"},
	}
	for _, c := range cases {
		got := Reverse(c.input)
		if got != c.want {
			t.Errorf("Reverse(%q) = %q, want %q", c.input, got, c.want)
		}
	}
}
