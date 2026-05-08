package core

import "testing"

// verifiermetricsFixture returns a populated VerifierMetrics map for use in tests.
func verifiermetricsFixture() VerifierMetrics {
	return VerifierMetrics{
		"score":    0.95,
		"latency":  120,
		"verified": true,
	}
}

func TestVerifierMetricsValid_Nil(t *testing.T) {
	t.Parallel()

	var v VerifierMetrics
	if !v.Valid() {
		t.Error("Valid() = false for nil VerifierMetrics, want true (spec: nil map is permitted)")
	}
}

func TestVerifierMetricsValid_Empty(t *testing.T) {
	t.Parallel()

	v := VerifierMetrics{}
	if !v.Valid() {
		t.Error("Valid() = false for empty VerifierMetrics, want true")
	}
}

func TestVerifierMetricsValid_ArbitraryKeys(t *testing.T) {
	t.Parallel()

	v := verifiermetricsFixture()
	if !v.Valid() {
		t.Error("Valid() = false for VerifierMetrics with arbitrary keys, want true")
	}
}
