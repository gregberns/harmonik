package evalvol

import "testing"

func TestGCD(t *testing.T) {
	cases := []struct {
		a, b, want int
	}{
		{0, 0, 0},
		{0, 5, 5},
		{5, 0, 5},
		{12, 8, 4},
		{8, 12, 4},
		{7, 13, 1}, // coprime
		{-12, 8, 4},
		{12, -8, 4},
		{-12, -8, 4},
		{100, 75, 25},
	}
	for _, c := range cases {
		if got := GCD(c.a, c.b); got != c.want {
			t.Errorf("GCD(%d, %d) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

func TestLCM(t *testing.T) {
	cases := []struct {
		a, b, want int
	}{
		{0, 5, 0},
		{5, 0, 0},
		{0, 0, 0},
		{4, 6, 12},
		{6, 4, 12},
		{7, 13, 91}, // coprime
		{-4, 6, 12},
		{4, -6, 12},
		{-4, -6, 12},
		{3, 5, 15},
	}
	for _, c := range cases {
		if got := LCM(c.a, c.b); got != c.want {
			t.Errorf("LCM(%d, %d) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}
