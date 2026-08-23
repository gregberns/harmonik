package codextest_test

import (
	"fmt"
	"testing"

	"github.com/gregberns/harmonik/internal/codexdigitaltwin"
	"github.com/gregberns/harmonik/internal/codexinput"
)

var aisMatrixModes = []struct {
	name string
	mode codexdigitaltwin.FaultMode
}{
	{"drop_after", codexdigitaltwin.FaultDropAfter},
	{"stall", codexdigitaltwin.FaultStall},
	{"truncate", codexdigitaltwin.FaultTruncate},
	{"dup", codexdigitaltwin.FaultDup},
}

func aisStrippedLen(t *testing.T, stratum codexdigitaltwin.InputStratum) int {
	t.Helper()
	events, err := codexdigitaltwin.SynthesizeInputStimulus(stratum)
	if err != nil {
		t.Fatalf("synthesize %s: %v", stratum, err)
	}
	return len(events)
}

// TestCodexInputReplay_FaultMatrix is the T9 matrix. The name matches the
// scripts/codex-metrics.sh recompute command (`-run 'TestCodexInputReplay_Fault'`).
func TestCodexInputReplay_FaultMatrix(t *testing.T) {
	t.Parallel()
	cfg := aisConfig()
	bound := aisVirtualBound(cfg)

	for _, stratum := range codexdigitaltwin.AllInputStrata {
		positions := aisStrippedLen(t, stratum)
		for _, fm := range aisMatrixModes {
			for n := 1; n <= positions; n++ {
				t.Run(fmt.Sprintf("%s/%s/event%d", stratum, fm.name, n), func(t *testing.T) {
					t.Parallel()
					stallExpected := fm.mode == codexdigitaltwin.FaultStall
					sink, elapsed := runInputDiscrete(t, stratum,
						codexdigitaltwin.FaultConfig{Mode: fm.mode, EventN: n}, stallExpected)

					acked := sink.emitCount(codexinput.EmitInputAcked)
					stale := sink.emitCount(codexinput.EmitInputStale)
					launchFail := sink.emitCount(codexinput.EmitLaunchFailure)
					submitted := sink.emitCount(codexinput.EmitInputSubmitted)

					entryForeclosed := n == 1 &&
						(fm.mode == codexdigitaltwin.FaultStall || fm.mode == codexdigitaltwin.FaultTruncate)
					if entryForeclosed {
						if sink.Handshakes != 0 || submitted != 0 || acked+stale+launchFail != 0 {
							t.Fatalf("entry-foreclosed cell leaked state: handshakes=%d submitted=%d terminals(acked+stale+launch)=%d",
								sink.Handshakes, submitted, acked+stale+launchFail)
						}
						return
					}

					if acked > 1 || stale > 1 {
						t.Fatalf("duplicate terminal: acked=%d stale=%d (must be <=1 each)", acked, stale)
					}
					if acked == 1 && stale == 1 {
						t.Fatalf("both acked AND stale fired for one submission (not single-terminal)")
					}

					if submitted == 1 {
						rejected := stratum == codexdigitaltwin.StratumRejected &&
							acked == 0 && stale == 0
						terminals := acked + stale
						if !rejected && terminals != 1 {
							t.Fatalf("submission opened but terminals=%d (acked=%d stale=%d), want exactly 1 (or a rejection resolution)",
								terminals, acked, stale)
						}
					}

					if elapsed > bound {
						t.Fatalf("terminal at %v virtual, beyond the AIS-INV-001 bound %v", elapsed, bound)
					}

					t.Logf("acked=%d stale=%d launch_failure=%d submitted=%d elapsed=%v",
						acked, stale, launchFail, submitted, elapsed)
				})
			}
		}
	}
}
