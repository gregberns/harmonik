package keepertest_test

// L2 integration tier — Twin → reactor → KeeperBridgeSink over the §2.2
// DISCRETE-EVENT harness (T10; RS-017 L2; measurement-design §3 "L2
// integration" row + §2.2).
//
// Replay mode: DISCRETE-EVENT, not flat. The synthesizer's pre-scheduled
// TimerFired lines are STRIPPED from the stimulus; the harness reacts to the
// reactor's ArmTimer/CancelTimer actions by arming real virtual-time timers
// and fires the earliest armed deadline whenever no external stimulus is
// pending (standard discrete-event simulation). Backstop discipline is
// shell-faithful: TimerClearBackstop is consulted only at settle-window ends
// (shell.go pollOnce), never mid-window.
//
// WHY discrete-event here (the T9-review critical note): L2 asserts interior
// effect COUNTS a live shell would produce — most importantly the degraded
// path's defensive /clear re-injects (hk-vdqe2): 15 clear_sent attempts with
// the default 10s settle window against the 150s backstop. A flat replay
// leaves ClearAttempt at 1 and would force a weakened golden; the discrete
// loop reproduces the real interleaving in virtual time (milliseconds, zero
// wall-clock waits, deterministic across runs and -race).
//
// L2 also hosts the per-mode fault smoke (RS-017: "at least one fault case
// per mode asserting a terminal signal"); the exhaustive 4-fault × 4-strata ×
// EventN matrix lives in l2_fault_matrix_test.go (T12).

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/keeper"
	"github.com/gregberns/harmonik/internal/keepertwin"
	"github.com/gregberns/harmonik/internal/substrate"
)

// ─── KeeperBridgeSink ────────────────────────────────────────────────────────

// KeeperBridgeSink is the keeper vertical's test-local bridge sink (RS-018:
// bridge sinks stay per-vertical and test-local; substrate R9 — no generic
// sink). It records what the real shell effector would drive onto the five
// ports (PanePort / GaugePort / HandoffPort / EmitterPort — ClockPort is the
// harness's virtual-time cursor), with no tmux and no filesystem.
type KeeperBridgeSink struct {
	// PanePort effects.
	Escapes     int
	HandoffCmds []string // cycle ids of injected /session-handoff commands
	Clears      int      // injected /clear count
	Briefs      int      // injected agent-brief count
	EnvSets     map[string]string

	// GaugePort effects.
	ManagedWrites    []string // SetManagedSession values ("" = clear binding)
	PrecompactClears int
	Holds            int

	// HandoffPort effects.
	Journals  []keeper.CycleJournal
	Truncates int

	// EmitterPort effects.
	Emits []keeper.Action // ActEmit actions in order

	// RespawnPort effects.
	ForceRestarts int
}

// Execute implements substrate.Effector[keeper.Action] for the non-timer
// actions (the harness intercepts ArmTimer/CancelTimer before the sink).
func (s *KeeperBridgeSink) Execute(_ context.Context, a keeper.Action) error {
	switch a.Kind {
	case keeper.ActSendEscape:
		s.Escapes++
	case keeper.ActInjectHandoffCmd:
		s.HandoffCmds = append(s.HandoffCmds, a.CycleID)
	case keeper.ActInjectClear:
		s.Clears++
	case keeper.ActInjectBrief:
		s.Briefs++
	case keeper.ActSetTmuxEnv:
		if s.EnvSets == nil {
			s.EnvSets = map[string]string{}
		}
		s.EnvSets[a.Key] = a.Value
	case keeper.ActSetManagedSession:
		s.ManagedWrites = append(s.ManagedWrites, a.SID)
	case keeper.ActClearPrecompact:
		s.PrecompactClears++
	case keeper.ActSetHold:
		s.Holds++
	case keeper.ActWriteJournal:
		s.Journals = append(s.Journals, a.Journal)
	case keeper.ActTruncateHandoff:
		s.Truncates++
	case keeper.ActEmit:
		s.Emits = append(s.Emits, a)
	case keeper.ActForceRestart:
		s.ForceRestarts++
	}
	return nil
}

var _ substrate.Effector[keeper.Action] = (*KeeperBridgeSink)(nil)

// emitTypes returns the sink's emitted event types in order.
func (s *KeeperBridgeSink) emitTypes() []core.EventType {
	return emittedTypes(s.Emits)
}

// clearAttempts decodes the clear_sent attempt counters, in order.
func (s *KeeperBridgeSink) clearAttempts(t *testing.T) []int {
	t.Helper()
	var out []int
	for _, a := range s.Emits {
		if a.Type != core.EventTypeSessionKeeperClearSent {
			continue
		}
		var p core.SessionKeeperClearSentPayload
		if err := json.Unmarshal(a.Payload, &p); err != nil {
			t.Fatalf("decode clear_sent payload: %v", err)
		}
		out = append(out, p.Attempt)
	}
	return out
}

// ─── The §2.2 discrete-event harness ─────────────────────────────────────────

// stripPreScheduledTimers removes the synthesizer's flat pre-scheduled
// TimerFired lines: in the discrete-event model timer firings are produced by
// the harness from the reactor's own ArmTimer actions (measurement-design
// §2.2), never delivered as external stimulus.
func stripPreScheduledTimers(events []keeper.Event) []keeper.Event {
	out := make([]keeper.Event, 0, len(events))
	for _, ev := range events {
		if ev.Kind == keeper.EvTimerFired {
			continue
		}
		out = append(out, ev)
	}
	return out
}

// drainTwin collects the Twin's decoded stimulus stream. For FaultStall the
// Twin blocks forever by design, so a per-receive idle timeout (generous —
// the Twin produces from memory in microseconds) converts the stall into
// end-of-stimulus; the timeout is harness liveness plumbing, never a golden
// value. For every other mode the channel closes and the timeout arm is
// unreachable in practice; hitting it is reported as a failure.
func drainTwin(t *testing.T, twin *keepertwin.Twin, stallExpected bool) []keeper.Event {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch := twin.Events(ctx)

	var out []keeper.Event
	for {
		idle := time.NewTimer(2 * time.Second)
		select {
		case ev, ok := <-ch:
			idle.Stop()
			if !ok {
				return out
			}
			out = append(out, ev)
		case <-idle.C:
			if !stallExpected {
				t.Fatalf("twin produced no event within the idle budget (silence bug?); got %d events", len(out))
			}
			cancel()
			for range ch { //nolint:revive // drain to let the twin goroutine exit
			}
			return out
		}
	}
}

// runDiscrete replays one cycle through the discrete-event harness and
// returns the sink plus the total VIRTUAL time elapsed (first stimulus → last
// step; the T12 matrix asserts it against the SK-015 bounded window). It
// FAILS the test if the reactor is still in-cycle when both the stimulus and
// every armed timer are exhausted — that is the silence bug (SR9), converted
// into an explicit failure, never a hang.
func runDiscrete(t *testing.T, sum keepertwin.CycleSummary, fault keepertwin.FaultConfig, stallExpected bool) (*KeeperBridgeSink, time.Duration) {
	t.Helper()

	events, err := keepertwin.SynthesizeStimulus(sum)
	if err != nil {
		t.Fatalf("synthesize %s: %v", sum.CKey, err)
	}
	stimulus := stripPreScheduledTimers(events)
	raw, err := keepertwin.EncodeStimulus(stimulus)
	if err != nil {
		t.Fatalf("encode %s: %v", sum.CKey, err)
	}

	twin := keepertwin.New(bytes.NewReader(raw), fault)
	stimuli := drainTwin(t, twin, stallExpected)

	cfg := testConfig(sum.AgentName)
	cyc := keeper.NewCycle(cfg)
	sink := &KeeperBridgeSink{}

	// Virtual-time cursor + the harness-owned timer registry. Deadlines are
	// anchored at ArmTimer EXECUTION time, exactly like shell.go execute.
	var now time.Time
	if len(stimuli) > 0 {
		now = stimuli[0].At
	}
	start := now
	timers := map[keeper.TimerKind]time.Time{}

	feed := func(ev keeper.Event) {
		for _, a := range cyc.Step(ev) {
			switch a.Kind {
			case keeper.ActArmTimer:
				timers[a.Timer] = now.Add(a.D)
			case keeper.ActCancelTimer:
				delete(timers, a.Timer)
			default:
				_ = sink.Execute(context.Background(), a) //nolint:errcheck // sink never errors
			}
		}
	}

	// nextTimer picks the next timer to fire under the shell's Clearing
	// discipline: the backstop is consulted only at settle-window ends
	// (shell.go pollOnce), so while a settle window is armed the backstop's
	// EFFECTIVE fire instant is the settle deadline, and it wins the tie there
	// (timeout-before-read at the boundary, backstop checked first).
	nextTimer := func() (keeper.TimerKind, time.Time, bool) {
		settleDL, hasSettle := timers[keeper.TimerClearSettle]
		var bestK keeper.TimerKind
		var bestT time.Time
		found := false
		for k, dl := range timers {
			eff := dl
			if k == keeper.TimerClearBackstop && hasSettle {
				if dl.After(settleDL) {
					continue // not yet elapsed at this window's end; settle handles it
				}
				eff = settleDL
			}
			switch {
			case !found || eff.Before(bestT):
				bestK, bestT, found = k, eff, true
			case eff.Equal(bestT) && k == keeper.TimerClearBackstop:
				bestK = k // backstop-first at the boundary
			}
		}
		return bestK, bestT, found
	}

	i := 0
	for steps := 0; ; steps++ {
		if steps > 100_000 {
			t.Fatalf("%s: discrete-event livelock (>100k steps)", sum.CKey)
		}
		tk, tdl, haveTimer := nextTimer()
		haveStim := i < len(stimuli)
		var sdl time.Time
		if haveStim {
			sdl = stimuli[i].At
			if sdl.Before(now) {
				sdl = now
			}
		}

		switch {
		case haveTimer && (!haveStim || !tdl.After(sdl)):
			now = tdl
			delete(timers, tk)
			feed(keeper.Event{Kind: keeper.EvTimerFired, Timer: tk, CycleID: cyc.State().CycleID, At: now})
		case haveStim:
			if sdl.After(now) {
				now = sdl
			}
			feed(stimuli[i])
			i++
		default:
			// Stimulus and timers both exhausted.
			if cyc.InCycle() {
				t.Fatalf("%s: SILENCE — reactor still in-cycle (phase %s) with no stimulus and no armed timer",
					sum.CKey, cyc.State().Phase)
			}
			return sink, now.Sub(start)
		}
	}
}

// wantDegradedClears computes the live-faithful defensive /clear count on the
// backstop-exhausted path: the entry inject plus one re-inject per settle
// window that ends strictly before the backstop's own expiry window, capped
// by ClearConfirmRetries (hk-vdqe2). ceil(backstop/settle) with defaults
// 150s/10s = 15.
func wantDegradedClears(cfg *keeper.CyclerConfig) int {
	k := int((cfg.ClearConfirmBackstop + cfg.ClearSettle - 1) / cfg.ClearSettle)
	if cfg.ClearConfirmRetries < k {
		return cfg.ClearConfirmRetries
	}
	return k
}

// assertOneTerminal asserts exactly one terminal with the wanted shape.
func assertOneTerminal(t *testing.T, sink *KeeperBridgeSink, ckey string, wantComplete, wantUnconfirmed bool) {
	t.Helper()
	types := sink.emitTypes()
	complete := countType(types, core.EventTypeSessionKeeperCycleComplete)
	aborted := countType(types, core.EventTypeSessionKeeperCycleAborted)
	unconfirmed := countType(types, core.EventTypeSessionKeeperClearUnconfirmed)
	if complete+aborted != 1 {
		t.Fatalf("%s: want exactly 1 terminal, got complete=%d aborted=%d (%v)", ckey, complete, aborted, types)
	}
	if wantComplete != (complete == 1) {
		t.Fatalf("%s: terminal complete=%v, want %v (%v)", ckey, complete == 1, wantComplete, types)
	}
	if wantUnconfirmed != (unconfirmed == 1) {
		t.Fatalf("%s: clear_unconfirmed=%d, want present:%v (%v)", ckey, unconfirmed, wantUnconfirmed, types)
	}
}

// journalPhases extracts the journal phase sequence.
func journalPhases(js []keeper.CycleJournal) []string {
	out := make([]string, 0, len(js))
	for _, j := range js {
		out = append(out, j.Phase)
	}
	return out
}

// eqStrings compares two string slices.
func eqStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// ─── Happy-path effects per stratum ──────────────────────────────────────────

// TestL2_CleanCompleteEffects drives the clean-complete stratum and asserts
// the exact port-effect shape a live shell would produce.
func TestL2_CleanCompleteEffects(t *testing.T) {
	t.Parallel()
	sum := pickPerStratum(t)[keepertwin.StratumCleanComplete]
	sink, _ := runDiscrete(t, sum, keepertwin.FaultConfig{}, false)

	assertOneTerminal(t, sink, sum.CKey, true, false)
	if sink.Escapes != 1 {
		t.Errorf("escapes = %d, want 1", sink.Escapes)
	}
	if len(sink.HandoffCmds) != 1 || sink.HandoffCmds[0] != sum.CycleID {
		t.Errorf("handoff cmds = %v, want [%s]", sink.HandoffCmds, sum.CycleID)
	}
	if sink.Clears != 1 {
		t.Errorf("clears = %d, want 1 (clean path: single /clear)", sink.Clears)
	}
	if sink.Briefs != 1 {
		t.Errorf("briefs = %d, want 1", sink.Briefs)
	}
	if got := sink.EnvSets["HARMONIK_AGENT"]; got != sum.AgentName {
		t.Errorf("HARMONIK_AGENT = %q, want %q", got, sum.AgentName)
	}
	// The managed binding rebinds to the NEW session id (the synthesizer's
	// deterministic post-clear SID).
	wantSID := "twin-post-" + sum.CycleID
	if len(sink.ManagedWrites) != 1 || sink.ManagedWrites[0] != wantSID {
		t.Errorf("managed writes = %v, want [%s]", sink.ManagedWrites, wantSID)
	}
	wantJournal := []string{"opened", "handoff_injected", "confirmed", "cleared", "resumed", "complete"}
	if got := journalPhases(sink.Journals); !eqStrings(got, wantJournal) {
		t.Errorf("journal phases = %v, want %v", got, wantJournal)
	}
}

// TestL2_DegradedCompleteEffects drives the degraded stratum and asserts the
// LIVE-FAITHFUL interior counts the flat replay cannot see: the hk-vdqe2
// defensive re-inject ladder (attempts 1..15 with defaults), then
// clear_unconfirmed + brief + complete, with the managed binding cleared.
func TestL2_DegradedCompleteEffects(t *testing.T) {
	t.Parallel()
	sum := pickPerStratum(t)[keepertwin.StratumDegradedComplete]
	sink, _ := runDiscrete(t, sum, keepertwin.FaultConfig{}, false)

	assertOneTerminal(t, sink, sum.CKey, true, true)

	want := wantDegradedClears(testConfig(sum.AgentName))
	if sink.Clears != want {
		t.Errorf("clears = %d, want %d (entry + defensive settle re-injects)", sink.Clears, want)
	}
	attempts := sink.clearAttempts(t)
	if len(attempts) != want {
		t.Fatalf("clear_sent emits = %d, want %d", len(attempts), want)
	}
	for i, a := range attempts {
		if a != i+1 {
			t.Fatalf("clear_sent attempts = %v, want 1..%d monotonically", attempts, want)
		}
	}
	if sink.Briefs != 1 {
		t.Errorf("briefs = %d, want 1 (degraded completion still briefs)", sink.Briefs)
	}
	if len(sink.ManagedWrites) != 1 || sink.ManagedWrites[0] != "" {
		t.Errorf("managed writes = %v, want [\"\"] (binding cleared on unconfirmed)", sink.ManagedWrites)
	}
}

// TestL2_AbortEffects drives the handoff-timeout stratum: no /clear, no
// brief, journal aborted, explicit reason.
func TestL2_AbortEffects(t *testing.T) {
	t.Parallel()
	sum := pickPerStratum(t)[keepertwin.StratumAbortHandoffTimeout]
	sink, _ := runDiscrete(t, sum, keepertwin.FaultConfig{}, false)

	assertOneTerminal(t, sink, sum.CKey, false, false)
	if sink.Clears != 0 {
		t.Errorf("clears = %d, want 0 (NEVER /clear an unconfirmed handoff)", sink.Clears)
	}
	if sink.Briefs != 0 {
		t.Errorf("briefs = %d, want 0 on abort", sink.Briefs)
	}
	if len(sink.HandoffCmds) != 1 {
		t.Errorf("handoff cmds = %v, want exactly 1", sink.HandoffCmds)
	}
	phases := journalPhases(sink.Journals)
	if len(phases) == 0 || phases[len(phases)-1] != "aborted" {
		t.Errorf("journal phases = %v, want terminal \"aborted\"", phases)
	}
	for _, a := range sink.Emits {
		if a.Type != core.EventTypeSessionKeeperCycleAborted {
			continue
		}
		var p core.SessionKeeperCycleAbortedPayload
		if err := json.Unmarshal(a.Payload, &p); err != nil {
			t.Fatalf("decode aborted payload: %v", err)
		}
		if p.Reason != "handoff_timeout" {
			t.Errorf("abort reason = %q, want handoff_timeout", p.Reason)
		}
	}
}

// TestL2_UnterminatedCycleFixedEffects drives the ONE recorded unterminated
// cycle through the discrete harness: the NEW reactor's armed clear_backstop
// MUST convert the old wedge into a bounded degraded completion (the SR9 fix
// — the required divergence, asserted at the ports level).
func TestL2_UnterminatedCycleFixedEffects(t *testing.T) {
	t.Parallel()
	sum := pickPerStratum(t)[keepertwin.StratumUnterminated]
	if sum.CKey != knownUnterminatedCKey {
		t.Fatalf("unterminated pick = %s, want %s", sum.CKey, knownUnterminatedCKey)
	}
	sink, _ := runDiscrete(t, sum, keepertwin.FaultConfig{}, false)

	// FIXED behavior: complete + clear_unconfirmed within the virtual bound.
	assertOneTerminal(t, sink, sum.CKey, true, true)
	if sink.Briefs != 1 {
		t.Errorf("briefs = %d, want 1 (the fixed cycle still resumes the agent)", sink.Briefs)
	}
}

// ─── Fault smoke: one case per substrate mode (full matrix = T12) ────────────

// TestL2_FaultSmoke asserts RS-INV-003 for one representative cell per fault
// mode: every fault yields exactly one explicit terminal within the virtual
// deadline — never silence. EventN indexes the stripped discrete stimulus
// (clean stratum: 1=GaugeTick, 2=NonceObserved, 3=ModelDone, 4=SessionChanged).
func TestL2_FaultSmoke(t *testing.T) {
	t.Parallel()
	clean := pickPerStratum(t)[keepertwin.StratumCleanComplete]

	cases := []struct {
		name            string
		fault           keepertwin.FaultConfig
		stallExpected   bool
		wantComplete    bool
		wantUnconfirmed bool
		wantClears      int
	}{
		{
			// Pane/session lost right after the nonce landed: the reactor is
			// awaiting model-done; the fail-open model_done_timeout then the
			// clear backstop carry it to a bounded degraded completion.
			name:            "drop_after_nonce",
			fault:           keepertwin.FaultConfig{Mode: keepertwin.FaultDropAfter, EventN: 2},
			wantComplete:    true,
			wantUnconfirmed: true,
			wantClears:      wantDegradedClears(testConfig(clean.AgentName)),
		},
		{
			// Stimulus stalls before the nonce (the writeNonce=false analog):
			// handoff_timeout aborts with the explicit reason.
			name:          "stall_before_nonce",
			fault:         keepertwin.FaultConfig{Mode: keepertwin.FaultStall, EventN: 2},
			stallExpected: true,
			wantComplete:  false,
			wantClears:    0,
		},
		{
			// Corrupt stimulus at the nonce position: the codec's transport-
			// error event replaces it and the stream ends; the armed
			// handoff_timeout aborts explicitly — a parse failure is never
			// swallowed into silence.
			name:         "truncate_at_nonce",
			fault:        keepertwin.FaultConfig{Mode: keepertwin.FaultTruncate, EventN: 2},
			wantComplete: false,
			wantClears:   0,
		},
		{
			// Nonce delivered twice (re-delivery probe): exactly one terminal,
			// no second /clear, no overlapping cycle (SR7).
			name:         "dup_nonce",
			fault:        keepertwin.FaultConfig{Mode: keepertwin.FaultDup, EventN: 2},
			wantComplete: true,
			wantClears:   1,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			sink, _ := runDiscrete(t, clean, tc.fault, tc.stallExpected)
			assertOneTerminal(t, sink, clean.CKey, tc.wantComplete, tc.wantUnconfirmed)
			if sink.Clears != tc.wantClears {
				t.Errorf("clears = %d, want %d", sink.Clears, tc.wantClears)
			}
			types := sink.emitTypes()
			if n := countType(types, core.EventTypeSessionKeeperHandoffStarted); n != 1 {
				t.Errorf("handoff_started = %d, want 1 (SR7: no overlapping cycle)", n)
			}
			// Explicit-reason companion (metric 4) on the abort outcomes.
			if !tc.wantComplete {
				for _, a := range sink.Emits {
					if a.Type != core.EventTypeSessionKeeperCycleAborted {
						continue
					}
					var p core.SessionKeeperCycleAbortedPayload
					if err := json.Unmarshal(a.Payload, &p); err != nil {
						t.Fatalf("decode aborted payload: %v", err)
					}
					if p.Reason == "" {
						t.Error("fault abort has empty reason (must be explicit)")
					}
				}
			}
		})
	}
}
