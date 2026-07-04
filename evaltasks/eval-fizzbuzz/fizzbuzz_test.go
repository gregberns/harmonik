package evalfizzbuzz_test

import (
	"testing"

	evalfizzbuzz "github.com/gregberns/harmonik/evaltasks/eval-fizzbuzz"
)

func TestFizzBuzz(t *testing.T) {
	t.Parallel()

	got := evalfizzbuzz.FizzBuzz(30)
	if len(got) != 30 {
		t.Fatalf("FizzBuzz(30): len = %d, want 30", len(got))
	}

	cases := []struct {
		i    int // 1-indexed
		want string
	}{
		{1, "1"},
		{2, "2"},
		{3, "Fizz"},
		{5, "Buzz"},
		{6, "Fizz"},
		{10, "Buzz"},
		{15, "FizzBuzz"},
		{20, "Buzz"},
		{21, "Fizz"},
		{25, "Buzz"},
		{30, "FizzBuzz"},
	}
	for _, tc := range cases {
		if got[tc.i-1] != tc.want {
			t.Errorf("FizzBuzz(30)[%d] = %q, want %q", tc.i-1, got[tc.i-1], tc.want)
		}
	}
}
