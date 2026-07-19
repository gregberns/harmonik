package keepertest_test

// T13 metrics export — persists the NEW reactor's full replayed emitted-event
// stream (all 507 corpus cycles, re-enveloped) as an events.jsonl for the
// out-of-band jq oracle (scripts/keeper-metrics.sh; measurement-design §6.3,
// §7 metrics 2–6).
//
// Skipped unless KEEPER_METRICS_EXPORT names the output path. This is NOT a
// regression test (TestL1_ReplayedStreamInvariants owns the in-process
// assertion) — it is the deterministic PRODUCER for the jq/grep recompute:
// the checker reads the persisted file with jq/grep, never the reactor's own
// report, satisfying "the thing under repair cannot be its own oracle" (D13
// out-of-band; census Acceptance Oracle condition 3). Zero-daemon, zero-token.

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
