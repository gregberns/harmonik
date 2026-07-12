package evalvol

import "errors"

// MovingAverage returns the simple moving average of xs over a window of size w.
func MovingAverage(xs []float64, w int) ([]float64, error) {
	if w <= 0 {
		return nil, errors.New("window size must be greater than 0")
	}
	if w > len(xs) {
		return []float64{}, nil
	}

	out := make([]float64, len(xs)-w+1)
	var sum float64
	for i, x := range xs {
		sum += x
		if i >= w {
			sum -= xs[i-w]
		}
		if i >= w-1 {
			out[i-w+1] = sum / float64(w)
		}
	}
	return out, nil
}
