package canary

import "testing"

func TestDouble(t *testing.T) {
	cases := []struct {
		in, want int
	}{
		{0, 0},
		{3, 6},
		{-4, -8},
	}
	for _, c := range cases {
		got := Double(c.in)
		if got != c.want {
			t.Errorf("Double(%d) = %d, want %d", c.in, got, c.want)
		}
	}
}
