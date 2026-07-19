package codextest_test

// The exhaustive INPUT-driver fault matrix — T9 (AIS-INV-001; RS-INV-003;
// harness-acceptance-design §"Fault matrix").
//
// Dimensions: 4 substrate fault modes (drop_after / stall / truncate / dup) ×
// 4 strata × every 1-based EventN position of that stratum's STRIPPED discrete
// stimulus. Positions per stratum:
//
//	acked          5  (Spawned, HandshakeOK, InputSubmitted, InputAcked, TurnCompleted)
//	rejected       4  (Spawned, HandshakeOK, InputSubmitted, InputRejected)
//	stale_timeout  3  (Spawned, HandshakeOK, InputSubmitted)
//	handshake_fail 1  (Spawned)
//
// = 13 positions × 4 modes = 52 cells. Required pass rate: 100% — these are
// invariants, not statistics; one silence = fail.
//
// PER-CELL UNIFORM SHAPE (harness-acceptance-design §"Fault matrix"):
//   - never-silence: after the stimulus AND every armed timer are exhausted the
//     reactor is NOT left in AwaitingAck / Handshaking (runInputDiscrete's own
//     assertion), and a submission that opened resolved to EXACTLY one terminal;
//   - single-terminal: at most one positive terminal emit — agent_input_acked
//     XOR agent_input_stale — and never both for the same submission;
//   - bounded window: the terminal lands within the AIS-INV-001 virtual window;
//   - fault→assert mapping is honored implicitly by the sentinel-ignore codec:
//     FaultDropAfter (twin_disconnected) and FaultTruncate (twin_transport_error)
//     are dropped by Step, so an in-flight submission proceeds to its OWN armed
//     input_ack_timeout → agent_input_stale; FaultStall withholds the frame so the
//     same timer fires; FaultDup re-delivers, and the reactor's phase/seq guards
//     keep it single-terminal.
//
// ENTRY-FORECLOSED CELLS (FaultStall@1 / FaultTruncate@1): the fault erases the
// lifecycle-opening Spawned frame, so no handshake and no submission ever open —
// AIS-INV-001 ("every SubmitInput reaches a terminal") is vacuously satisfied.
// Those cells assert the no-submit shape: zero handshake writes, zero emits,
// reactor still in Spawning.

import (
	"fmt"
	"testing"

	"github.com/gregberns/harmonik/internal/codexdigitaltwin"
	"github.com/gregberns/harmonik/internal/codexinput"
)

// aisMatrixModes enumerates the four substrate fault modes under test.
var aisMatrixModes = []struct {
	name string
	mode codexdigitaltwin.FaultMode
}{
	{"drop_after", codexdigitaltwin.FaultDropAfter},
	{"stall", codexdigitaltwin.FaultStall},
	{"truncate", codexdigitaltwin.FaultTruncate},
	{"dup", codexdigitaltwin.FaultDup},
}

// aisStrippedLen returns the number of discrete stimulus positions for a stratum
// (the EventN domain, matching substrate.FaultConfig's 1-based indexing).
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

					// Entry-foreclosed cells: the fault erases Spawned — nothing
					// opens. Assert the no-submit shape.
					entryForeclosed := n == 1 &&
						(fm.mode == codexdigitaltwin.FaultStall || fm.mode == codexdigitaltwin.FaultTruncate)
					if entryForeclosed {
						if sink.Handshakes != 0 || submitted != 0 || acked+stale+launchFail != 0 {
							t.Fatalf("entry-foreclosed cell leaked state: handshakes=%d submitted=%d terminals(acked+stale+launch)=%d",
								sink.Handshakes, submitted, acked+stale+launchFail)
						}
						return
					}

					// Single-terminal: never both a positive ack and a stale for
					// the same submission.
					if acked > 1 || stale > 1 {
						t.Fatalf("duplicate terminal: acked=%d stale=%d (must be <=1 each)", acked, stale)
					}
					if acked == 1 && stale == 1 {
						t.Fatalf("both acked AND stale fired for one submission (not single-terminal)")
					}

					// If a submission opened it MUST reach exactly one terminal:
					// an ack, a stale, or a protocol rejection (which resolves the
					// sync Ack{Rejected} — no emit, reactor returns to a settled
					// phase; runInputDiscrete already proved it is not left
					// AwaitingAck).
					if submitted == 1 {
						rejected := stratum == codexdigitaltwin.StratumRejected &&
							acked == 0 && stale == 0
						terminals := acked + stale
						if !rejected && terminals != 1 {
							t.Fatalf("submission opened but terminals=%d (acked=%d stale=%d), want exactly 1 (or a rejection resolution)",
								terminals, acked, stale)
						}
					}

					// Bounded virtual window.
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
