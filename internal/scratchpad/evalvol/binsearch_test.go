package evalvol

import "testing"

func TestSearch(t *testing.T) {
	cases := []struct {
		name   string
		xs     []int
		target int
		want   int
	}{
		{"empty", []int{}, 5, -1},
		{"single found", []int{5}, 5, 0},
		{"single not found", []int{5}, 1, -1},
		{"middle found", []int{1, 3, 5, 7, 9}, 5, 2},
		{"first found", []int{1, 3, 5, 7, 9}, 1, 0},
		{"last found", []int{1, 3, 5, 7, 9}, 9, 4},
		{"not found below", []int{1, 3, 5, 7, 9}, 0, -1},
		{"not found above", []int{1, 3, 5, 7, 9}, 10, -1},
		{"not found between", []int{1, 3, 5, 7, 9}, 4, -1},
		{"duplicates found", []int{1, 2, 2, 2, 3}, 2, 2},
		{"duplicates not found", []int{1, 2, 2, 2, 3}, 4, -1},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := Search(c.xs, c.target)
			if c.name == "duplicates found" {
				if got < 1 || got > 3 || c.xs[got] != 2 {
					t.Fatalf("Search(%v, %d) = %d, want an index in [1,3] with value 2", c.xs, c.target, got)
				}
				return
			}
			if got != c.want {
				t.Fatalf("Search(%v, %d) = %d, want %d", c.xs, c.target, got, c.want)
			}
		})
	}
}
