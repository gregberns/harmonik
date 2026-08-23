package daemon_test

import (
	"context"
	"math/rand"
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/handlercontract"
	"github.com/gregberns/harmonik/internal/keepertwin"
	"github.com/gregberns/harmonik/internal/twinparity"
)

const (
	agentReadyBand = 200 * time.Millisecond

	boundaryGuard = 120 * time.Millisecond
	// overBandSpread is how far past (band+guard) the out-of-band draws range.
	overBandSpread = 40 * time.Millisecond
)

type timingDraw struct {
	// AgentReadyDelay is how long after launch the agent_ready signal arrives.
	// > agentReadyBand ⇒ agent_ready_timeout.
	AgentReadyDelay time.Duration
}

func (d timingDraw) asVector() []time.Duration {
	return []time.Duration{d.AgentReadyDelay}
}

func drawFromVector(v []time.Duration) timingDraw {
	return timingDraw{AgentReadyDelay: v[0]}
}

func observeAnomalies(t *testing.T, draw timingDraw) []string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	emitter := &handlercontract.CollectingEmitter{}
	runID := core.RunID(uuid.Must(uuid.NewV7()))

	if draw.AgentReadyDelay > agentReadyBand {
		daemon.ExportedEmitAgentReadyTimeout(ctx, emitter, runID, "twin-sid", agentReadyBand)
	}
	return anomalyKindsIn(emitter.EventTypes())
}

func anomalyKindsIn(emitted []string) []string {
	anomalySet := map[string]struct{}{}
	for _, a := range twinparity.AnomalyKinds {
		anomalySet[a] = struct{}{}
	}
	seen := map[string]struct{}{}
	var out []string
	for _, e := range emitted {
		if _, ok := anomalySet[e]; ok {
			if _, dup := seen[e]; !dup {
				seen[e] = struct{}{}
				out = append(out, e)
			}
		}
	}
	sort.Strings(out)
	return out
}

func terminalSetFor(anoms []string) []string {
	if len(anoms) == 0 {
		out := append([]string(nil), twinparity.TerminalKinds...)
		sort.Strings(out)
		return out
	}
	return []string{}
}

func keeperCoObserve(t *testing.T, draw timingDraw) bool {
	t.Helper()
	sum := keepertwin.CycleSummary{
		CKey:      "twin|cyc",
		AgentName: "twin",
		CycleID:   "cyc",
	}
	if draw.AgentReadyDelay > agentReadyBand {
		sum.Outcome = "aborted"
		sum.AbortReason = "handoff_timeout"
	} else {
		sum.Outcome = "complete"
		sum.ClearUnconfirmed = false
	}
	stratum, err := keepertwin.Classify(sum)
	if err != nil {
		t.Fatalf("keeperCoObserve: Classify: %v", err)
	}
	return stratum == keepertwin.StratumAbortHandoffTimeout
}

func shrinkTimingVector(vec []time.Duration, fails func([]time.Duration) bool) []time.Duration {
	cur := append([]time.Duration(nil), vec...)
	if !fails(cur) {
		return cur // not actually failing; nothing to shrink
	}
	for {
		progressed := false
		for i := range cur {
			if cur[i] != 0 {
				cand := append([]time.Duration(nil), cur...)
				cand[i] = 0
				if fails(cand) {
					cur = cand
					progressed = true
					continue
				}
			}
			if cur[i] > 0 {
				cand := append([]time.Duration(nil), cur...)
				cand[i] = cur[i] / 2
				if cand[i] != cur[i] && fails(cand) {
					cur = cand
					progressed = true
				}
			}
		}
		if !progressed {
			return cur
		}
	}
}

const numDraws = 64

const timingPropSeed = 0x5C1A11EC

func TestTimingProperty_InBandInvariants(t *testing.T) {
	t.Parallel()
	rng := rand.New(rand.NewSource(timingPropSeed)) //nolint:gosec // G404: deterministic fuzz seed, not security-sensitive

	var firstTerminal []string
	for i := 0; i < numDraws; i++ {
		draw := timingDraw{
			AgentReadyDelay: time.Duration(rng.Int63n(int64(agentReadyBand - boundaryGuard))),
		}

		anoms := observeAnomalies(t, draw)

		if len(anoms) != 0 {
			minVec := shrinkTimingVector(draw.asVector(), func(v []time.Duration) bool {
				return len(observeAnomalies(t, drawFromVector(v))) != 0
			})
			t.Errorf("inv-2 VIOLATED: in-band draw produced anomalies %v; minimal failing vector = %v", anoms, minVec)
		}

		if keeperCoObserve(t, draw) {
			t.Errorf("keeper co-observation DISAGREES: in-band draw %v classified as handoff-timeout abort", draw)
		}

		term := terminalSetFor(anoms)
		if firstTerminal == nil {
			firstTerminal = term
		} else if !reflect.DeepEqual(term, firstTerminal) {
			t.Errorf("inv-1 VIOLATED: draw %d terminal set %v != first %v", i, term, firstTerminal)
		}
	}

	if !reflect.DeepEqual(firstTerminal, sortedTerminalKinds()) {
		t.Errorf("inv-1: in-band terminal set = %v, want %v", firstTerminal, sortedTerminalKinds())
	}
}

func sortedTerminalKinds() []string {
	out := append([]string(nil), twinparity.TerminalKinds...)
	sort.Strings(out)
	return out
}

func TestTimingProperty_OutOfBandMatchingAnomaly(t *testing.T) {
	t.Parallel()
	rng := rand.New(rand.NewSource(timingPropSeed + 1)) //nolint:gosec // G404: deterministic fuzz seed

	cases := []struct {
		name    string
		build   func() timingDraw
		wantOne string
	}{
		{
			name: "agent_ready over band",
			build: func() timingDraw {
				return timingDraw{
					AgentReadyDelay: agentReadyBand + boundaryGuard + time.Duration(rng.Int63n(int64(overBandSpread))),
				}
			},
			wantOne: string(core.EventTypeAgentReadyTimeout),
		},
	}

	const drawsPerCase = 60 // ≥50 total; the post_ready case that used to share the budget is gone
	for _, tc := range cases {
		for i := 0; i < drawsPerCase; i++ {
			draw := tc.build()
			anoms := observeAnomalies(t, draw)

			want := []string{tc.wantOne}
			if !reflect.DeepEqual(anoms, want) {
				minVec := shrinkTimingVector(draw.asVector(), func(v []time.Duration) bool {
					got := observeAnomalies(t, drawFromVector(v))
					return !reflect.DeepEqual(got, want)
				})
				t.Errorf("inv-3 VIOLATED [%s]: got anomalies %v, want exactly %v; minimal failing vector = %v",
					tc.name, anoms, want, minVec)
			}

			keeperAbort := keeperCoObserve(t, draw)
			daemonAgentReadyTimeout := reflect.DeepEqual(anoms, []string{string(core.EventTypeAgentReadyTimeout)})
			if keeperAbort != daemonAgentReadyTimeout {
				t.Errorf("keeper co-observation DISAGREES [%s]: keeperAbort=%v daemonAgentReadyTimeout=%v (draw %v)",
					tc.name, keeperAbort, daemonAgentReadyTimeout, draw)
			}
		}
	}
}

// TestTimingProperty_RealThresholdsPinned pins the production threshold constants
// the scaled bands model. A drift here means the harness's bands no longer track
// the real thresholds and must be re-derived.
func TestTimingProperty_RealThresholdsPinned(t *testing.T) {
	t.Parallel()
	if got := daemon.ExportedDefaultAgentReadyTimeout; got != 150*time.Second {
		t.Errorf("defaultAgentReadyTimeout drifted: got %v, want 150s (agentready.go:64)", got)
	}
}

// TestTimingProperty_ShrinkerMinimizes proves the shrinker reduces a failing
// vector to a minimal one, independent of the deliberate-break demonstration.
// Predicate: "fails when component 0 exceeds a threshold." The minimal failing
// vector must zero component 1 and reduce component 0 to just over the boundary.
func TestTimingProperty_ShrinkerMinimizes(t *testing.T) {
	t.Parallel()
	boundary := 40 * time.Millisecond
	fails := func(v []time.Duration) bool { return v[0] > boundary }

	start := []time.Duration{500 * time.Millisecond, 700 * time.Millisecond}
	minVec := shrinkTimingVector(start, fails)

	if !fails(minVec) {
		t.Fatalf("shrinker returned a non-failing vector: %v", minVec)
	}
	if minVec[1] != 0 {
		t.Errorf("shrinker did not zero the irrelevant component: got minVec[1]=%v, want 0", minVec[1])
	}
	if minVec[0] <= boundary || minVec[0] > 2*boundary {
		t.Errorf("shrinker did not minimize component 0: got %v, want in (%v, %v]", minVec[0], boundary, 2*boundary)
	}
}
