package evalvol

import (
	"math"
	"testing"
)

func TestMovingAverage(t *testing.T) {
	cases := []struct {
		name    string
		xs      []float64
		w       int
		want    []float64
		wantErr bool
	}{
		{
			name: "basic window 3",
			xs:   []float64{1, 2, 3, 4, 5},
			w:    3,
			want: []float64{2, 3, 4},
		},
		{
			name: "window 1 returns input",
			xs:   []float64{1, 2, 3},
			w:    1,
			want: []float64{1, 2, 3},
		},
		{
			name: "window equals length",
			xs:   []float64{1, 2, 3},
			w:    3,
			want: []float64{2},
		},
		{
			name: "window larger than length",
			xs:   []float64{1, 2},
			w:    5,
			want: []float64{},
		},
		{
			name: "empty input",
			xs:   []float64{},
			w:    2,
			want: []float64{},
		},
		{
			name:    "zero window errors",
			xs:      []float64{1, 2, 3},
			w:       0,
			wantErr: true,
		},
		{
			name:    "negative window errors",
			xs:      []float64{1, 2, 3},
			w:       -1,
			wantErr: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := MovingAverage(tc.xs, tc.w)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("length mismatch: got %v, want %v", got, tc.want)
			}
			for i := range got {
				if math.Abs(got[i]-tc.want[i]) > 1e-9 {
					t.Fatalf("index %d: got %v, want %v", i, got[i], tc.want[i])
				}
			}
		})
	}
}
