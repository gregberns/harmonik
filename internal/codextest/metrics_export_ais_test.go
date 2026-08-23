package codextest_test

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/gregberns/harmonik/internal/codexdigitaltwin"
	"github.com/gregberns/harmonik/internal/codexinput"
)

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
