package daemon_test

// twinparity_timing_property_test.go — WS3-Claude-C timing property/fuzz harness.
//
// Accept criteria (plans/2026-07-13-code-revamp/M6-PLAN.md §WS3-Claude-C):
//   - inv-1: the terminal event SET is identical across all (in-band) draws.
//   - inv-2: NO anomaly events appear when timings are INSIDE the tolerance bands.
//   - inv-3: EXACTLY the matching anomaly appears when a timing is OUTSIDE its band.
//   - the harness SHRINKS to a minimal failing timing vector on failure.
//   - keeper co-observes via internal/keepertwin.
//
// # What makes this non-tautological
//
// The harness does NOT re-implement anomaly detection. Each timing draw is fed
// through the REAL daemon detection functions — waitAgentReady and
// waitPostAgentReadyProgress — and, on a real timeout/hang, through the REAL
// anomaly emitters emitAgentReadyTimeout / emitPostAgentReadyHang (all exposed
// via export_test.go). The generator's role is played by the production code;
// the harness only draws timings and CHECKS the emitted-event set against the
// F1 vocabulary (twinparity.AnomalyKinds / twinparity.TerminalKinds).
//
// # Tolerance bands
//
// The real production thresholds are:
//   - agent_ready_timeout:    defaultAgentReadyTimeout      = 150s
//     (internal/daemon/agentready.go:64)
//   - post_agent_ready_hang:  defaultPostAgentReadyHangTimeout = 7m
//     (internal/daemon/postreadyhang.go:37)
//   - keeper handoff (co-obs): keeper.DefaultHandoffTimeout   = 300s
//     (internal/keeper/thresholds.go:157)
//
// A property/fuzz test cannot wait minutes per draw, so the harness drives the
// SAME real detection functions with SCALED bands (tens of ms) passed as the
// timeout parameter — the detection predicate ("delay > band → anomaly") is
// identical regardless of the numeric value. TestTimingProperty_RealThresholdsPinned
// PINS the real constants, so if a production default drifts the harness surfaces
// it instead of silently modelling a stale threshold.
//
// # Scope of anomalies
//
// This harness covers the two anomalies whose detectors are point-in-time timing
// predicates cleanly drivable as pure functions: agent_ready_timeout and
// post_agent_ready_hang. The other two AnomalyKinds — agent_warning_silent_hang
// and agent_resumed_after_warning — are produced by the stateful stale-watch /
// paste-inject scanners (internal/daemon/stalewatch.go, pasteinject.go), not by a
// single latency comparison, so they are out of scope for a timing-vector harness.
// See the JUDGMENT CALL note in the WS3-Claude-C report.
//
// Bead ref: M6 WS3-Claude-C. Composes on WS3-F1 (internal/twinparity).

import (
	"context"
	"errors"
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

// ─────────────────────────────────────────────────────────────────────────────
// Bands (scaled analogues of the real production thresholds; see file godoc)
// ─────────────────────────────────────────────────────────────────────────────

const (
	// agentReadyBand models defaultAgentReadyTimeout (150s, agentready.go:64).
	agentReadyBand = 200 * time.Millisecond
	// postReadyBand models defaultPostAgentReadyHangTimeout (7m, postreadyhang.go:37).
	postReadyBand = 250 * time.Millisecond

	// boundaryGuard is a dead-zone around each band's boundary that neither the
	// in-band nor the out-of-band draws enter. The harness drives the REAL
	// detectors, which use real time.After/time.NewTimer, so a draw sleeps for
	// real wall-clock time; under -race and load, timer + goroutine scheduling
	// jitter has been observed in the tens of ms. The guard must dominate that
	// jitter or an in-band draw can flip over the boundary and false-fire
	// (an earlier 15ms margin flaked ~1-in-4 under -race). 120ms gives ~5x
	// headroom over the observed jitter while keeping per-draw waits small.
	boundaryGuard = 120 * time.Millisecond
	// overBandSpread is how far past (band+guard) the out-of-band draws range.
	overBandSpread = 40 * time.Millisecond
)

// timingDraw is one point in the fuzz timing space: the per-edge latencies the
// twin would produce (via scriptdriver.go's per-step delay_ms knob) for the two
// causal edges the harness exercises.
type timingDraw struct {
	// AgentReadyDelay is how long after launch the agent_ready signal arrives.
	// > agentReadyBand ⇒ agent_ready_timeout.
	AgentReadyDelay time.Duration
	// PostReadyDelay is how long after agent_ready the first progress event
	// arrives. > postReadyBand ⇒ post_agent_ready_hang. Only reached when
	// agent_ready itself was in band.
	PostReadyDelay time.Duration
}

// asVector projects a draw onto the []time.Duration vector the shrinker operates
// over (index 0 = agent_ready edge, index 1 = post_ready edge).
func (d timingDraw) asVector() []time.Duration {
	return []time.Duration{d.AgentReadyDelay, d.PostReadyDelay}
}

func drawFromVector(v []time.Duration) timingDraw {
	return timingDraw{AgentReadyDelay: v[0], PostReadyDelay: v[1]}
}

// ─────────────────────────────────────────────────────────────────────────────
// Real-emitter observation
// ─────────────────────────────────────────────────────────────────────────────

// timingPropAdapter is a minimal handlercontract.Adapter whose DetectReady fires
// on core.EventTypeAgentReady. It is the ready-detector waitAgentReady consults.
type timingPropAdapter struct{}

func (timingPropAdapter) DetectReady(ev core.EventEnvelope) bool {
	return ev.Type == string(core.EventTypeAgentReady)
}
func (timingPropAdapter) DetectRateLimit(core.EventEnvelope) (bool, time.Duration) { return false, 0 }
func (timingPropAdapter) CleanExitSequence(context.Context, handlercontract.Session) error {
	return nil
}
func (timingPropAdapter) RotateAccount(context.Context) error { return nil }
func (timingPropAdapter) Diagnose(context.Context) (handlercontract.DiagnosticReport, error) {
	return handlercontract.DiagnosticReport{}, nil
}

// timingPropSource satisfies daemon.AgentEventSourceExported by delivering a
// single agent_ready event after `delay` of REAL wall-clock time. The send is
// buffered (cap 1) so the producer goroutine never blocks even when the waiter
// has already timed out and stopped reading.
type timingPropSource struct {
	delay time.Duration
}

func (s *timingPropSource) Events(ctx context.Context, runID core.RunID) <-chan core.EventEnvelope {
	ch := make(chan core.EventEnvelope, 1)
	go func() {
		select {
		case <-time.After(s.delay):
			rid := runID
			ch <- core.EventEnvelope{
				EventID: core.EventID(uuid.Must(uuid.NewV7())),
				Type:    string(core.EventTypeAgentReady),
				RunID:   &rid,
			}
		case <-ctx.Done():
		}
	}()
	return ch
}

// delayedEventCh returns a channel that yields one (arbitrary) progress event
// after `delay`, modelling the first post-agent_ready event waitPostAgentReadyProgress
// waits for.
func delayedEventCh(ctx context.Context, delay time.Duration) <-chan core.EventEnvelope {
	ch := make(chan core.EventEnvelope, 1)
	go func() {
		select {
		case <-time.After(delay):
			// waitPostAgentReadyProgress treats ANY received event as progress;
			// the kind is immaterial, so a plain agent_started envelope suffices.
			ch <- core.EventEnvelope{Type: string(core.EventTypeAgentStarted)}
		case <-ctx.Done():
		}
	}()
	return ch
}

// observeAnomalies runs one timing draw through the REAL daemon detectors and
// emitters, returning the sorted set of AnomalyKinds that actually fired.
func observeAnomalies(t *testing.T, draw timingDraw) []string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	emitter := &handlercontract.CollectingEmitter{}
	runID := core.RunID(uuid.Must(uuid.NewV7()))

	// Stage 1 — agent_ready. REAL waitAgentReady with the scaled band.
	src := &timingPropSource{delay: draw.AgentReadyDelay}
	err := daemon.ExportedWaitAgentReady(ctx, runID, src, timingPropAdapter{}, agentReadyBand)
	if errors.Is(err, daemon.ExportedErrAgentReadyTimeout) {
		daemon.ExportedEmitAgentReadyTimeout(ctx, emitter, runID, "twin-sid", agentReadyBand)
		return anomalyKindsIn(emitter.EventTypes())
	}
	if err != nil {
		t.Fatalf("observeAnomalies: unexpected waitAgentReady error: %v", err)
	}

	// Stage 2 — post-agent_ready progress. REAL waitPostAgentReadyProgress.
	perr := daemon.ExportedWaitPostAgentReadyProgress(ctx, delayedEventCh(ctx, draw.PostReadyDelay), postReadyBand)
	if errors.Is(perr, daemon.ExportedErrPostAgentReadyHang) {
		daemon.ExportedEmitPostAgentReadyHang(ctx, emitter, runID, "twin-sid", postReadyBand, 0, "implement")
	} else if perr != nil {
		t.Fatalf("observeAnomalies: unexpected waitPostAgentReadyProgress error: %v", perr)
	}
	return anomalyKindsIn(emitter.EventTypes())
}

// anomalyKindsIn filters emitted event types down to the F1 AnomalyKinds set,
// sorted and de-duplicated.
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

// terminalSetFor models the terminal-landmark set a run journals: a run with no
// anomaly completes and journals the full twinparity.TerminalKinds triad; a run
// that tripped a timing anomaly did not reach clean completion (empty set).
func terminalSetFor(anoms []string) []string {
	if len(anoms) == 0 {
		out := append([]string(nil), twinparity.TerminalKinds...)
		sort.Strings(out)
		return out
	}
	return []string{}
}

// ─────────────────────────────────────────────────────────────────────────────
// Keeper co-observation (internal/keepertwin)
// ─────────────────────────────────────────────────────────────────────────────

// keeperCoObserve independently classifies the SAME draw via keepertwin.Classify:
// an agent_ready latency past the band is a handoff-timeout abort; otherwise a
// clean completion. Returns true when the keeper twin reads the draw as an abort.
// The property test asserts this verdict AGREES with the daemon's real emitter.
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

// ─────────────────────────────────────────────────────────────────────────────
// Shrinker — delta-debloat to a minimal failing timing vector
// ─────────────────────────────────────────────────────────────────────────────

// shrinkTimingVector reduces a failing timing vector to a minimal one that still
// fails `fails`. It repeatedly tries, for each component, (a) zeroing it and
// (b) halving it, keeping any reduction that preserves failure, until a fixed
// point is reached. Deterministic; no randomness.
func shrinkTimingVector(vec []time.Duration, fails func([]time.Duration) bool) []time.Duration {
	cur := append([]time.Duration(nil), vec...)
	if !fails(cur) {
		return cur // not actually failing; nothing to shrink
	}
	for {
		progressed := false
		for i := range cur {
			// Try zeroing component i.
			if cur[i] != 0 {
				cand := append([]time.Duration(nil), cur...)
				cand[i] = 0
				if fails(cand) {
					cur = cand
					progressed = true
					continue
				}
			}
			// Try halving component i.
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

// ─────────────────────────────────────────────────────────────────────────────
// inv-1 + inv-2 — property over N in-band draws
// ─────────────────────────────────────────────────────────────────────────────

// numDraws is the fuzz draw count. Accept criterion: N ≥ 50.
const numDraws = 64

// timingPropSeed fixes the PRNG so the fuzz is reproducible.
const timingPropSeed = 0x5C1A11EC

func TestTimingProperty_InBandInvariants(t *testing.T) {
	t.Parallel()
	rng := rand.New(rand.NewSource(timingPropSeed)) //nolint:gosec // G404: deterministic fuzz seed, not security-sensitive

	var firstTerminal []string
	for i := 0; i < numDraws; i++ {
		// Draw both edges strictly INSIDE their bands, below the dead-zone guard
		// so timer scheduling jitter cannot flip an in-band draw over the boundary.
		draw := timingDraw{
			AgentReadyDelay: time.Duration(rng.Int63n(int64(agentReadyBand - boundaryGuard))),
			PostReadyDelay:  time.Duration(rng.Int63n(int64(postReadyBand - boundaryGuard))),
		}

		anoms := observeAnomalies(t, draw)

		// inv-2: no anomaly events inside the bands.
		if len(anoms) != 0 {
			minVec := shrinkTimingVector(draw.asVector(), func(v []time.Duration) bool {
				return len(observeAnomalies(t, drawFromVector(v))) != 0
			})
			t.Errorf("inv-2 VIOLATED: in-band draw produced anomalies %v; minimal failing vector = %v", anoms, minVec)
		}

		// keeper co-observation: an in-band draw must read as a clean completion.
		if keeperCoObserve(t, draw) {
			t.Errorf("keeper co-observation DISAGREES: in-band draw %v classified as handoff-timeout abort", draw)
		}

		// inv-1: terminal set identical across all in-band draws.
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

// ─────────────────────────────────────────────────────────────────────────────
// inv-3 — exactly the matching anomaly when a timing is OUTSIDE its band
// ─────────────────────────────────────────────────────────────────────────────

func TestTimingProperty_OutOfBandMatchingAnomaly(t *testing.T) {
	t.Parallel()
	rng := rand.New(rand.NewSource(timingPropSeed + 1)) //nolint:gosec // G404: deterministic fuzz seed

	// Each case: which edge is pushed OUT of band, and the single anomaly kind
	// that must then appear (exactly).
	cases := []struct {
		name    string
		build   func() timingDraw
		wantOne string
	}{
		{
			name: "agent_ready over band",
			build: func() timingDraw {
				return timingDraw{
					// Over agentReadyBand by at least the guard; post-ready in band (never reached).
					AgentReadyDelay: agentReadyBand + boundaryGuard + time.Duration(rng.Int63n(int64(overBandSpread))),
					PostReadyDelay:  time.Duration(rng.Int63n(int64(postReadyBand - boundaryGuard))),
				}
			},
			wantOne: string(core.EventTypeAgentReadyTimeout),
		},
		{
			name: "post_ready over band",
			build: func() timingDraw {
				return timingDraw{
					AgentReadyDelay: time.Duration(rng.Int63n(int64(agentReadyBand - boundaryGuard))),
					PostReadyDelay:  postReadyBand + boundaryGuard + time.Duration(rng.Int63n(int64(overBandSpread))),
				}
			},
			wantOne: string(core.EventTypePostAgentReadyHang),
		},
	}

	const drawsPerCase = 30 // ≥50 total across the two cases
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

			// keeper co-observation must agree on the partition for the
			// agent_ready edge (the handoff analogue).
			keeperAbort := keeperCoObserve(t, draw)
			daemonAgentReadyTimeout := reflect.DeepEqual(anoms, []string{string(core.EventTypeAgentReadyTimeout)})
			if keeperAbort != daemonAgentReadyTimeout {
				t.Errorf("keeper co-observation DISAGREES [%s]: keeperAbort=%v daemonAgentReadyTimeout=%v (draw %v)",
					tc.name, keeperAbort, daemonAgentReadyTimeout, draw)
			}
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Pins + shrinker self-test
// ─────────────────────────────────────────────────────────────────────────────

// TestTimingProperty_RealThresholdsPinned pins the production threshold constants
// the scaled bands model. A drift here means the harness's bands no longer track
// the real thresholds and must be re-derived.
func TestTimingProperty_RealThresholdsPinned(t *testing.T) {
	t.Parallel()
	if got := daemon.ExportedDefaultAgentReadyTimeout; got != 150*time.Second {
		t.Errorf("defaultAgentReadyTimeout drifted: got %v, want 150s (agentready.go:64)", got)
	}
	if got := *daemon.ExportedDefaultPostAgentReadyHangTimeout; got != 7*time.Minute {
		t.Errorf("defaultPostAgentReadyHangTimeout drifted: got %v, want 7m (postreadyhang.go:37)", got)
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
	// minVec[0] must still fail but be no larger than the original; halving from
	// 500ms toward the 40ms boundary lands within (boundary, 2*boundary].
	if minVec[0] <= boundary || minVec[0] > 2*boundary {
		t.Errorf("shrinker did not minimize component 0: got %v, want in (%v, %v]", minVec[0], boundary, 2*boundary)
	}
}
