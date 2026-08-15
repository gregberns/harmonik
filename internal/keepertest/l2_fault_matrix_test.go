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
//	exactly ONE explicit cycle ending …
//	… of the kind the cycle EARNED. The dividing line is restart authority,
//	  granted at handoff_written (internal/keeper stepConfirmHandoff). Past
//	  it the machine owns the destructive tail and owes a completion, clean
//	  or degraded (SK-INV-005). Before it the handoff is still pending, so
//	  the only legitimate ending is cycle_parked{handoff_pending} (SK-025).
//	  "Exactly one ending" alone is NOT enough here: a keeper that parked
//	  every cycle and never restarted anything would satisfy it in all 44
//	  cells, which is precisely the failure this matrix exists to catch,
//	… within the SK-015 bounded VIRTUAL window (≈520s: HandoffTimeout +
//	  model_done_timeout + ClearConfirmBackstop + injection overhead),
//	exactly ONE handoff_started (SR7 — no overlapping cycle),
//	no /clear without authority (SK-INV-001),
//	a CLOSED journal phase ("complete"/"pending"/"parked") — no half-open
//	  journal. Phase "aborted" is absent because nothing emits it on this
//	  path any more; crash recovery is its only writer (§8.2),
//	a park ⇒ one of the two §8.4 reasons, never empty (parkReason),
//	and NEVER silence: runDiscrete converts "still in-cycle with nothing
//	  pending" into an explicit test failure, drainTwin's wall-clock idle
//	  timer converts a genuinely hung stream into a failure, and the
//	  100k-step guard converts a livelock into a failure (plus go test's own
//	  -timeout as the outermost backstop). None of these is a golden value.
//
// A NOTE ON "abort_handoff_timeout": that stratum name is the CORPUS's, and
// the corpus is frozen. The recorded cycles did abort. Replayed through the
// current reactor the same stimulus suspends instead, so the cells under that
// stratum expect a park.
//
// THE §5 SEAM-GAP DECISION (path A — ratified here; carried from the T9/T10
// reviews): keeper's Event vocabulary has no native transport-error or
// disconnect kind (the pre-rebuild keeper read files/panes directly), so the
// keepertwin codec returns SENTINEL kinds (twin_transport_error /
// twin_disconnected, 00b R3) that the pure reactor's TOTAL transition
// ignores. Under FaultDropAfter/FaultTruncate the reactor therefore never
// "sees" the disconnect — it proceeds to its OWN timeout-driven ending
// (pre-nonce: cycle_parked{handoff_pending}; post-nonce: model_done
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
// the no-cycle shape instead: zero handoff_started, zero endings, zero
// /clear, zero journal writes, and a clean harness exit (the liveness half is
// still proven — a hang would fail in drainTwin or the step guard).

import (
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

// closedJournalPhases are the journal phases that mean the machine returned to
// Idle and left nothing half-open. "pending" is a suspension rather than a
// terminal, but it is a COMPLETE record: crash recovery reads it and restores
// the same request (internal/keeper stepIdleCrashJournal). "aborted" is not
// here because nothing on this path writes it any more (§8.2).
var closedJournalPhases = map[string]bool{"complete": true, "pending": true, "parked": true}

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
					endings := cycleEndings(t, sink.Emits)

					// Entry-foreclosed cells: the fault erases the cycle-opening
					// GaugeTick — no cycle exists for SR9 to bound (vacuous); the
					// harness has already proven the loop EXITS. Assert the
					// no-cycle shape: nothing started, nothing half-open.
					if n == 1 && (fm.mode == keepertwin.FaultStall || fm.mode == keepertwin.FaultTruncate) {
						if started != 0 || len(endings) != 0 || sink.Clears != 0 || len(sink.Journals) != 0 {
							t.Fatalf("entry-foreclosed cell leaked state: started=%d endings=%v clears=%d journals=%d",
								started, endings, sink.Clears, len(sink.Journals))
						}
						return
					}

					// SR9 / SK-015: exactly ONE explicit ending…
					if len(endings) != 1 {
						t.Fatalf("want exactly 1 cycle ending, got %v (%v)", endings, types)
					}
					ending := endings[0]
					// …within the bounded virtual window.
					if bound := sr9VirtualBound(testConfig(sum.AgentName)); elapsed > bound {
						t.Fatalf("ending at %v virtual, beyond the SK-015 bound %v", elapsed, bound)
					}
					// …and of the kind the cycle earned. handoff_written is where
					// restart authority begins, and it is what separates an owed
					// terminal from a legitimate suspension.
					authorized := countType(types, core.EventTypeSessionKeeperHandoffWritten) > 0
					switch {
					case authorized && !isCompletion(ending):
						t.Fatalf("ending = %s after restart authority (handoff_written); "+
							"an authorized restart owes a completion (SK-INV-005) (%v)", ending, types)
					case !authorized && ending != outcomeParkedPending:
						t.Fatalf("ending = %s with no restart authority; the only legitimate "+
							"ending before handoff_written is %s (SK-025) (%v)",
							ending, outcomeParkedPending, types)
					}
					// SK-INV-001: /clear NEVER precedes a confirmed handoff.
					if !authorized && sink.Clears != 0 {
						t.Fatalf("clears = %d with no confirmed handoff (SK-INV-001)", sink.Clears)
					}
					// SR7: exactly one cycle — no overlap, no double start.
					if started != 1 {
						t.Fatalf("handoff_started = %d, want 1 (SR7)", started)
					}
					// No half-open journal claiming an in-progress cycle.
					if len(sink.Journals) == 0 {
						t.Fatal("no journal writes for a started cycle")
					}
					last := sink.Journals[len(sink.Journals)-1].Phase
					if !closedJournalPhases[last] {
						t.Fatalf("journal left half-open in phase %q", last)
					}
					// A suspension owes the one phase crash recovery can read
					// back. stepIdleCrashJournal restores a request from
					// "pending" only — "parked" falls through its default and
					// recovers nothing — so a cycle that emits
					// cycle_parked{handoff_pending} while journaling "parked"
					// strands the request across a keeper restart. Membership in
					// closedJournalPhases alone cannot see that.
					if !authorized && last != "pending" {
						t.Fatalf("journal phase %q for a suspension; want \"pending\", "+
							"the only phase stepIdleCrashJournal restores (SK-025)", last)
					}
					// FaultDup: the duplicate delivery is absorbed — no second
					// /clear beyond the stratum's no-fault baseline.
					if fm.mode == keepertwin.FaultDup && sink.Clears != baselineClears[st] {
						t.Errorf("clears = %d, want %d (dup re-delivery must not add a /clear)",
							sink.Clears, baselineClears[st])
					}

					t.Logf("ending: %s authorized=%v clears=%d elapsed=%v",
						ending, authorized, sink.Clears, elapsed)
				})
			}
		}
	}
}
