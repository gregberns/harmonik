package evalvol

import "testing"

func TestCaesar(t *testing.T) {
	cases := []struct {
		in    string
		shift int
		want  string
	}{
		{"abc", 1, "bcd"},
		{"xyz", 3, "abc"},
		{"ABC", 1, "BCD"},
		{"XYZ", 3, "ABC"},
		{"Hello, World!", 13, "Uryyb, Jbeyq!"},
		{"abc", 0, "abc"},
		{"abc", 26, "abc"},
		{"abc", -1, "zab"},
		{"abc", -27, "zab"},
		{"abc", 100, "wxy"},
		{"Hello123", 5, "Mjqqt123"},
		{"", 5, ""},
	}
	for _, tc := range cases {
		got := Caesar(tc.in, tc.shift)
		if got != tc.want {
			t.Errorf("Caesar(%q, %d) = %q; want %q", tc.in, tc.shift, got, tc.want)
		}
	}
}
