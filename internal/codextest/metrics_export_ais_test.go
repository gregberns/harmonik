package codextest_test

// T9 metrics export — persists the INPUT reactor's replayed emitted-event stream
// (every stratum replayed fault-free, re-enveloped) as an events.jsonl for the
// out-of-band jq/grep oracle (scripts/codex-metrics.sh).
//
// Skipped unless CODEX_METRICS_EXPORT names the output path. This is NOT a
// regression test (the L2 tiers own the in-process assertions) — it is the
// deterministic PRODUCER for the jq/grep recompute: the checker reads the
// persisted file with jq/grep, NEVER the driver's own report, satisfying "the
// thing under repair cannot be its own oracle" (D13). Zero-daemon, zero-token.

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/gregberns/harmonik/internal/codexdigitaltwin"
	"github.com/gregberns/harmonik/internal/codexinput"
)

// aisReplayEnvelope is the minimal re-enveloped emitted event the out-of-band
// metrics script consumes (jq reads .type / .input_seq / .reason / .turn_id).
type aisReplayEnvelope struct {
	Type     codexinput.EmitType `json:"type"`
	InputSeq uint64              `json:"input_seq,omitempty"`
	TurnID   string              `json:"turn_id,omitempty"`
	Reason   string              `json:"reason,omitempty"`
	Stratum  string              `json:"stratum"`
}

func TestMetricsExport_ReplayedStream(t *testing.T) {
	out := os.Getenv("CODEX_METRICS_EXPORT")
	if out == "" {
		t.Skip("set CODEX_METRICS_EXPORT=<path> to export the replayed input-ack stream (T9 oracle producer)")
	}

	f, err := os.Create(out) //nolint:gosec // G304: test-owned output path
	if err != nil {
		t.Fatalf("create replayed stream: %v", err)
	}
	enc := json.NewEncoder(f)

	n := 0
	for _, stratum := range codexdigitaltwin.AllInputStrata {
		sink, _ := runInputDiscrete(t, stratum, codexdigitaltwin.FaultConfig{}, false)
		for _, a := range sink.Emits {
			if err := enc.Encode(aisReplayEnvelope{
				Type:     a.Emit,
				InputSeq: a.InputSeq,
				TurnID:   a.TurnID,
				Reason:   a.Reason,
				Stratum:  string(stratum),
			}); err != nil {
				t.Fatalf("encode envelope: %v", err)
			}
			n++
		}
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close replayed stream: %v", err)
	}
	if n == 0 {
		t.Fatal("exported zero envelopes")
	}
	t.Logf("exported %d envelopes to %s", n, out)
}
