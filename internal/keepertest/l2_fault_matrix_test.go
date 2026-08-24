package keepertest_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/keeper"
	"github.com/gregberns/harmonik/internal/keepertwin"
)

func sr9VirtualBound(cfg *keeper.CyclerConfig) time.Duration {
	return cfg.HandoffTimeout + cfg.ModelDoneTimeout + cfg.ClearConfirmBackstop + time.Minute
}

var matrixModes = []struct {
	name string
	mode keepertwin.FaultMode
}{
	{"drop_after", keepertwin.FaultDropAfter},
	{"stall", keepertwin.FaultStall},
	{"truncate", keepertwin.FaultTruncate},
	{"dup", keepertwin.FaultDup},
}

func strippedStimulusLen(t *testing.T, sum keepertwin.CycleSummary) int {
	t.Helper()
	events, err := keepertwin.SynthesizeStimulus(sum)
	if err != nil {
		t.Fatalf("synthesize %s: %v", sum.CKey, err)
	}
	return len(stripPreScheduledTimers(events))
}

var closedJournalPhases = map[string]bool{"complete": true, "pending": true, "parked": true, "aborted": true}

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

					if n == 1 && (fm.mode == keepertwin.FaultStall || fm.mode == keepertwin.FaultTruncate) {
						if started != 0 || len(endings) != 0 || sink.Clears != 0 || len(sink.Journals) != 0 {
							t.Fatalf("entry-foreclosed cell leaked state: started=%d endings=%v clears=%d journals=%d",
								started, endings, sink.Clears, len(sink.Journals))
						}
						return
					}

					if len(endings) != 1 {
						t.Fatalf("want exactly 1 cycle ending, got %v (%v)", endings, types)
					}
					ending := endings[0]
					if bound := sr9VirtualBound(testConfig(sum.AgentName)); elapsed > bound {
						t.Fatalf("ending at %v virtual, beyond the SK-015 bound %v", elapsed, bound)
					}
					authorized := countType(types, core.EventTypeSessionKeeperHandoffWritten) > 0
					switch {
					case authorized && ending != outcomeComplete && ending != outcomeClearUnconfirmed:
						t.Fatalf("ending = %s after restart authority (handoff_written); "+
							"want completion or a visible unconfirmed-clear failure (%v)", ending, types)
					case !authorized && ending != outcomeParkedPending:
						t.Fatalf("ending = %s with no restart authority; the only legitimate "+
							"ending before handoff_written is %s (SK-025) (%v)",
							ending, outcomeParkedPending, types)
					}
					if !authorized && sink.Clears != 0 {
						t.Fatalf("clears = %d with no confirmed handoff (SK-INV-001)", sink.Clears)
					}
					if started != 1 {
						t.Fatalf("handoff_started = %d, want 1 (SR7)", started)
					}
					if len(sink.Journals) == 0 {
						t.Fatal("no journal writes for a started cycle")
					}
					last := sink.Journals[len(sink.Journals)-1].Phase
					if !closedJournalPhases[last] {
						t.Fatalf("journal left half-open in phase %q", last)
					}
					if !authorized && last != "pending" {
						t.Fatalf("journal phase %q for a suspension; want \"pending\", "+
							"the only phase stepIdleCrashJournal restores (SK-025)", last)
					}
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
