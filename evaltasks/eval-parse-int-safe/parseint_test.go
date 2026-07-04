package evalparseintsafe_test

import (
	"testing"

	evalparseintsafe "github.com/gregberns/harmonik/evaltasks/eval-parse-int-safe"
)

func TestParseIntOr(t *testing.T) {
	t.Parallel()

	cases := []struct {
		s    string
		def  int
		want int
	}{
		{"42", 0, 42},
		{"-7", 0, -7},
		{"0", 99, 0},
		{"", 5, 5},
		{"abc", 5, 5},
		{"12x", 5, 5},
		{"99999999999999999999999", 5, 5}, // overflow
		{" 3", 5, 5},                      // leading space is not a digit
	}
	for _, tc := range cases {
		got := evalparseintsafe.ParseIntOr(tc.s, tc.def)
		if got != tc.want {
			t.Errorf("ParseIntOr(%q, %d) = %d, want %d", tc.s, tc.def, got, tc.want)
		}
	}
}
