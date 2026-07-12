package evalvol

import "testing"

func TestToRoman(t *testing.T) {
	cases := []struct {
		input   int
		want    string
		wantErr bool
	}{
		{4, "IV", false},
		{9, "IX", false},
		{40, "XL", false},
		{3999, "MMMCMXCIX", false},
		{0, "", true},
		{4000, "", true},
	}
	for _, tc := range cases {
		got, err := ToRoman(tc.input)
		if tc.wantErr {
			if err == nil {
				t.Errorf("ToRoman(%d): expected error, got %q", tc.input, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("ToRoman(%d): unexpected error: %v", tc.input, err)
			continue
		}
		if got != tc.want {
			t.Errorf("ToRoman(%d) = %q, want %q", tc.input, got, tc.want)
		}
	}
}
