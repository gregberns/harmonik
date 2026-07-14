package keepertest_test

// The exhaustive fault matrix — T12 (SR9 / SK-INV-005 / SK-015; SK-R8;
// measurement-design §5; RS-INV-003).
//
// Dimensions: 4 substrate fault modes (drop_after / stall / truncate / dup) ×
// 4 corpus strata × every 1-based EventN position of that stratum's STRIPPED
// discrete stimulus (pre-scheduled TimerFired lines removed — see
// l2_integration_test.go's header; the harness generates timer firings from
// the reactor's own ArmTimer actions). Positions per stratum:
//
//	clean_complete        4  (GaugeTick, NonceObserved, ModelDone, SessionChanged)
//	degraded_complete     3  (GaugeTick, NonceObserved, ModelDone)
//	abort_handoff_timeout 1  (GaugeTick)
//	unterminated          3  (GaugeTick, NonceObserved, ModelDone)
//
// = 11 positions × 4 modes = 44 cells. Required pass rate: 100% — these are
// invariants, not statistics; one silence = fail.
//
// WHAT EACH CELL ASSERTS (the §5 uniform shape, as amended for T12):
//
//	exactly ONE explicit terminal (cycle_complete XOR cycle_aborted) …
//	… within the SK-015 bounded VIRTUAL window (≈520s: HandoffTimeout +
//	  model_done_timeout + ClearConfirmBackstop + injection overhead),
//	exactly ONE handoff_started (SR7 — no overlapping cycle),
//	a TERMINAL journal phase ("complete"/"aborted") — no half-open journal,
//	cycle_aborted ⇒ non-empty explicit reason,
//	and NEVER silence: runDiscrete converts "still in-cycle with nothing
//	  pending" into an explicit test failure, drainTwin's wall-clock idle
//	  timer converts a genuinely hung stream into a failure, and the
//	  100k-step guard converts a livelock into a failure (plus go test's own
//	  -timeout as the outermost backstop). None of these is a golden value.
//
// THE §5 SEAM-GAP DECISION (path A — ratified here; carried from the T9/T10
// reviews): keeper's Event vocabulary has no native transport-error or
// disconnect kind (the pre-rebuild keeper read files/panes directly), so the
// keepertwin codec returns SENTINEL kinds (twin_transport_error /
// twin_disconnected, 00b R3) that the pure reactor's TOTAL transition
// ignores. Under FaultDropAfter/FaultTruncate the reactor therefore never
// "sees" the disconnect — it proceeds to its OWN timeout-driven terminal
// (pre-nonce: cycle_aborted{handoff_timeout}; post-nonce: model_done
// fail-open → clear backstop → cycle_complete + clear_unconfirmed). That IS
// the SR9 invariant satisfied: SK-015 mandates "exactly one terminal outcome
// within a bounded window … silence is FORBIDDEN" — a bounded-LIVENESS
// invariant, not a terminal-TYPE mandate (specs/session-keeper.md SK-015 /
// SK-INV-005 already carry exactly this wording; no spec change needed).
// measurement-design §5's table rows, which predated R3 and said DropAfter →
// "cycle_aborted or restart_failed-class", are AMENDED (2026-07-13, T12) to
// describe the ignore-and-timeout reality; the amendment fixes the table's
// expectation wording and does NOT weaken SR9.
//
// ENTRY-FORECLOSED CELLS (2 per stratum, 8 total): FaultStall@1 withholds the
// cycle-opening GaugeTick entirely and FaultTruncate@1 replaces it with the
// ignored transport-error sentinel — NO cycle ever opens, so SR9 ("every
// handoff_started(c) reaches …") is vacuously satisfied. Those cells assert
// the no-cycle shape instead: zero handoff_started, zero terminals, zero
// /clear, zero journal writes, and a clean harness exit (the liveness half is
// still proven — a hang would fail in drainTwin or the step guard).

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/keeper"
	"github.com/gregberns/harmonik/internal/keepertwin"
)

// sr9VirtualBound is the SK-015 bounded window: every cycle must reach its
// terminal within HandoffTimeout + model_done_timeout + ClearConfirmBackstop
// + injection overhead (≈520s virtual with the defaults 300+60+150).
func sr9VirtualBound(cfg *keeper.CyclerConfig) time.Duration {
	return cfg.HandoffTimeout + cfg.ModelDoneTimeout + cfg.ClearConfirmBackstop + time.Minute
}

// matrixModes enumerates the four substrate fault modes under test.
var matrixModes = []struct {
	name string
	mode keepertwin.FaultMode
}{
	{"drop_after", keepertwin.FaultDropAfter},
	{"stall", keepertwin.FaultStall},
	{"truncate", keepertwin.FaultTruncate},
	{"dup", keepertwin.FaultDup},
}

// strippedStimulusLen returns the number of discrete stimulus positions for a
// cycle (the EventN domain: 1..len over the post-strip stream, matching
// substrate.FaultConfig's 1-based post-skip indexing).
func strippedStimulusLen(t *testing.T, sum keepertwin.CycleSummary) int {
	t.Helper()
	events, err := keepertwin.SynthesizeStimulus(sum)
	if err != nil {
		t.Fatalf("synthesize %s: %v", sum.CKey, err)
	}
	return len(stripPreScheduledTimers(events))
}

// abortReasons decodes every cycle_aborted payload reason in emit order.
func abortReasons(t *testing.T, sink *KeeperBridgeSink) []string {
	t.Helper()
	var out []string
	for _, a := range sink.Emits {
		if a.Type != core.EventTypeSessionKeeperCycleAborted {
			continue
		}
		var p core.SessionKeeperCycleAbortedPayload
		if err := json.Unmarshal(a.Payload, &p); err != nil {
			t.Fatalf("decode aborted payload: %v", err)
		}
		out = append(out, p.Reason)
	}
	return out
}

// TestKeeperReplay_FaultMatrix is the T12 matrix. The name matches the §7
// metric-8 recompute command (`go test -run 'TestKeeperReplay_Fault' …`).
func TestKeeperReplay_FaultMatrix(t *testing.T) {
	t.Parallel()
	picks := pickPerStratum(t)
	strata := []keepertwin.Stratum{
		keepertwin.StratumCleanComplete,
		keepertwin.StratumDegradedComplete,
		keepertwin.StratumAbortHandoffTimeout,
		keepertwin.StratumUnterminated,
	}

	// Per-stratum no-fault baseline /clear counts: the FaultDup cells assert
	// re-delivery changes NOTHING — in particular no second /clear (§5 dup
	// row; the reactor-side dedup analog of codex I2).
	baselineClears := map[keepertwin.Stratum]int{}
	for _, st := range strata {
		sink, _ := runDiscrete(t, picks[st], keepertwin.FaultConfig{}, false)
		baselineClears[st] = sink.Clears
	}

	for _, st := range strata {
		sum := picks[st]
		positions := strippedStimulusLen(t, sum)
		for _, fm := range matrixModes {
			for n := 1; n <= positions; n++ {
				t.Run(fmt.Sprintf("%s/%s/event%d", st, fm.name, n), func(t *testing.T) {
					t.Parallel()
					stallExpected := fm.mode == keepertwin.FaultStall
					sink, elapsed := runDiscrete(t, sum,
						keepertwin.FaultConfig{Mode: fm.mode, EventN: n}, stallExpected)

					types := sink.emitTypes()
					started := countType(types, core.EventTypeSessionKeeperHandoffStarted)
					complete := countType(types, core.EventTypeSessionKeeperCycleComplete)
					aborted := countType(types, core.EventTypeSessionKeeperCycleAborted)
					unconfirmed := countType(types, core.EventTypeSessionKeeperClearUnconfirmed)

					// Entry-foreclosed cells: the fault erases the cycle-opening
					// GaugeTick — no cycle exists for SR9 to bound (vacuous); the
					// harness has already proven the loop EXITS. Assert the
					// no-cycle shape: nothing started, nothing half-open.
					if n == 1 && (fm.mode == keepertwin.FaultStall || fm.mode == keepertwin.FaultTruncate) {
						if started != 0 || complete+aborted != 0 || sink.Clears != 0 || len(sink.Journals) != 0 {
							t.Fatalf("entry-foreclosed cell leaked state: started=%d terminals=%d clears=%d journals=%d",
								started, complete+aborted, sink.Clears, len(sink.Journals))
						}
						return
					}

					// SR9 / SK-015: exactly ONE explicit terminal…
					if complete+aborted != 1 {
						t.Fatalf("want exactly 1 terminal, got complete=%d aborted=%d (%v)",
							complete, aborted, types)
					}
					// …within the bounded virtual window.
					if bound := sr9VirtualBound(testConfig(sum.AgentName)); elapsed > bound {
						t.Fatalf("terminal at %v virtual, beyond the SK-015 bound %v", elapsed, bound)
					}
					// SR7: exactly one cycle — no overlap, no double start.
					if started != 1 {
						t.Fatalf("handoff_started = %d, want 1 (SR7)", started)
					}
					// No half-open journal claiming an in-progress cycle.
					if len(sink.Journals) == 0 {
						t.Fatal("no journal writes for a started cycle")
					}
					if last := sink.Journals[len(sink.Journals)-1].Phase; last != "complete" && last != "aborted" {
						t.Fatalf("journal left half-open in phase %q", last)
					}
					// Aborts carry an explicit, non-empty reason.
					for _, r := range abortReasons(t, sink) {
						if r == "" {
							t.Fatal("cycle_aborted with empty reason (must be explicit)")
						}
					}
					// FaultDup: the duplicate delivery is absorbed — no second
					// /clear beyond the stratum's no-fault baseline.
					if fm.mode == keepertwin.FaultDup && sink.Clears != baselineClears[st] {
						t.Errorf("clears = %d, want %d (dup re-delivery must not add a /clear)",
							sink.Clears, baselineClears[st])
					}

					t.Logf("terminal: complete=%d aborted=%d unconfirmed=%d clears=%d elapsed=%v",
						complete, aborted, unconfirmed, sink.Clears, elapsed)
				})
			}
		}
	}
}
