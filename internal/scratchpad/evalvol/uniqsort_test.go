package evalvol

import "testing"

func TestUniqueSorted(t *testing.T) {
	cases := []struct {
		name string
		in   []int
		want []int
	}{
		{"empty", []int{}, []int{}},
		{"all duplicates", []int{7, 7, 7, 7}, []int{7}},
		{"already sorted unique", []int{1, 2, 3}, []int{1, 2, 3}},
		{"unsorted with dups", []int{5, 3, 5, 1, 3, 2}, []int{1, 2, 3, 5}},
		{"negatives and zero", []int{0, -1, -1, 2, 0}, []int{-1, 0, 2}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := UniqueSorted(tc.in)
			if len(got) != len(tc.want) {
				t.Fatalf("UniqueSorted(%v) = %v, want %v", tc.in, got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("UniqueSorted(%v) = %v, want %v", tc.in, got, tc.want)
				}
			}
		})
	}
}
