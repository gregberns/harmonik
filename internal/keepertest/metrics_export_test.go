package keepertest_test

import (
	"os"
	"testing"
)

func TestMetricsExport_ReplayedStream(t *testing.T) {
	out := os.Getenv("KEEPER_METRICS_EXPORT")
	if out == "" {
		t.Skip("set KEEPER_METRICS_EXPORT=<path> to export the replayed stream (T13 oracle producer)")
	}
	n := writeReplayedStream(t, out)
	if n == 0 {
		t.Fatal("exported zero envelopes")
	}
	t.Logf("exported %d envelopes to %s", n, out)
}
