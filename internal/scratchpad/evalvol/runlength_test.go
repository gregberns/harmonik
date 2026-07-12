package evalvol

import "testing"

func TestRunLengthRoundTrip(t *testing.T) {
	cases := []struct {
		name string
		in   string
		enc  string
	}{
		{"empty", "", ""},
		{"single", "a", "1a"},
		{"repeats", "aaabcc", "3a1b2c"},
		{"no repeats", "abc", "1a1b1c"},
		{"long run", "aaaaaaaaaaaa", "12a"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Encode(tc.in)
			if got != tc.enc {
				t.Fatalf("Encode(%q) = %q, want %q", tc.in, got, tc.enc)
			}

			decoded, err := Decode(got)
			if err != nil {
				t.Fatalf("Decode(%q) returned error: %v", got, err)
			}
			if decoded != tc.in {
				t.Fatalf("Decode(Encode(%q)) = %q, want %q", tc.in, decoded, tc.in)
			}
		})
	}
}

func TestDecodeInvalid(t *testing.T) {
	invalid := []string{"a", "3", "3a2"}
	for _, in := range invalid {
		if _, err := Decode(in); err == nil {
			t.Fatalf("Decode(%q) expected error, got nil", in)
		}
	}
}
