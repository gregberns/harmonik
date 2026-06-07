package keeper_test

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/keeper"
)

// cycleSpyInjector records inject calls (target + text) without spawning tmux.
type cycleSpyInjector struct {
	mu    sync.Mutex
	calls []cycleInjectCall
}

type cycleInjectCall struct {
	target string
	text   string
}

func (s *cycleSpyInjector) inject(_ context.Context, target, text string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, cycleInjectCall{target: target, text: text})
	return nil
}

func (s *cycleSpyInjector) texts() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, len(s.calls))
	for i, c := range s.calls {
		out[i] = c.text
	}
	return out
}

// journalCapture records journal phases in order (replaces disk writes).
type journalCapture struct {
	mu     sync.Mutex
	phases []string
}

func (jc *journalCapture) write(_ string, j *keeper.CycleJournal) error {
	jc.mu.Lock()
	defer jc.mu.Unlock()
	jc.phases = append(jc.phases, j.Phase)
	return nil
}

func (jc *journalCapture) snapshot() []string {
	jc.mu.Lock()
	defer jc.mu.Unlock()
	out := make([]string, len(jc.phases))
	copy(out, jc.phases)
	return out
}

// handoffReturnsNonceAfter returns a ReadHandoff fake that returns an error on
// the first n calls, then returns a handoff body containing the nonce.
func handoffReturnsNonceAfter(n int, nonce string) func(path string) (string, error) {
	var count int
	var mu sync.Mutex
	return func(_ string) (string, error) {
		mu.Lock()
		defer mu.Unlock()
		count++
		if count > n {
			return "# Handoff\n\n" + nonce + "\n\nSome handoff content.", nil
		}
		return "", context.DeadlineExceeded // not found yet
	}
}

// handoffNeverReturnsNonce always fails — simulates a timeout.
func handoffNeverReturnsNonce(_ string) (string, error) {
	return "", context.DeadlineExceeded
}

// gaugeReturnsNewSIDAfter returns a ReadGaugeFn fake that returns prevSID for
// the first n calls, then switches to newSID.
func gaugeReturnsNewSIDAfter(n int, projectDir, agentName, prevSID, newSID string) func(string, string) (*keeper.CtxFile, time.Time, error) {
	var count int
	var mu sync.Mutex
	return func(_, _ string) (*keeper.CtxFile, time.Time, error) {
		mu.Lock()
		defer mu.Unlock()
		count++
		sid := prevSID
		if count > n {
			sid = newSID
		}
		cf := &keeper.CtxFile{Pct: 95.0, SessionID: sid}
		return cf, time.Now(), nil
	}
}

// newTestCycler builds a Cycler wired with test fakes.
func newTestCycler(
	agentName string,
	projectDir string,
	em keeper.Emitter,
	spy *cycleSpyInjector,
	jc *journalCapture,
	cycleID string,
	readHandoff func(string) (string, error),
	readGaugeFn func(string, string) (*keeper.CtxFile, time.Time, error),
	crispIdle bool,
	holdingDispatch bool,
	handoffTimeout time.Duration,
	clearSettle time.Duration,
) *keeper.Cycler {
	cfg := keeper.CyclerConfig{
		AgentName:      agentName,
		ProjectDir:     projectDir,
		TmuxTarget:     "fake-pane",
		ActPct:         90.0,
		HandoffTimeout: handoffTimeout,
		ClearSettle:    clearSettle,
		PollInterval:   10 * time.Millisecond,
		CycleIDGen:     func() string { return cycleID },
		HandoffFilePath: func(_, agent string) string {
			return "/tmp/HANDOFF-" + agent + ".md"
		},
		ReadHandoff:       readHandoff,
		InjectFn:          spy.inject,
		ReadGaugeFn:       readGaugeFn,
		CrispIdleFn:       func(_, _ string) bool { return crispIdle },
		HoldingDispatchFn: func(_, _ string) bool { return holdingDispatch },
		WriteJournalFn:    jc.write,
	}
	return keeper.NewCycler(cfg, em)
}

// TestCycler_HappyPath verifies the full 7-step ordering:
// journal(opened) → handoff inject → nonce confirmed → /clear inject →
// session-resume inject → journal(complete) → cycle_complete event.
func TestCycler_HappyPath(t *testing.T) {
	t.Parallel()

	const (
		agent   = "happy-agent"
		cycleID = "cyc-happy-001"
		prevSID = "sess-before"
		newSID  = "sess-after"
	)

	em := &keeper.RecordingEmitter{}
	spy := &cycleSpyInjector{}
	jc := &journalCapture{}

	nonce := "<!-- KEEPER:" + cycleID + " -->"

	// ReadHandoff: first 2 calls return error; call 3+ returns content with nonce.
	readHandoff := handoffReturnsNonceAfter(2, nonce)

	// ReadGaugeFn: first 2 calls return prevSID; call 3+ returns newSID.
	readGaugeFn := gaugeReturnsNewSIDAfter(2, "", agent, prevSID, newSID)

	cycler := newTestCycler(
		agent, t.TempDir(), em, spy, jc, cycleID,
		readHandoff, readGaugeFn,
		true,                 // crispIdle
		false,                // holdingDispatch
		500*time.Millisecond, // handoffTimeout
		200*time.Millisecond, // clearSettle
	)

	cf := &keeper.CtxFile{Pct: 95.0, SessionID: prevSID}
	if err := cycler.MaybeRun(context.Background(), cf); err != nil {
		t.Fatalf("MaybeRun: %v", err)
	}

	// (a) Journal phase sequence.
	phases := jc.snapshot()
	want := []string{"opened", "handoff_injected", "confirmed", "cleared", "resumed", "complete"}
	if len(phases) != len(want) {
		t.Errorf("journal phases = %v; want %v", phases, want)
	} else {
		for i, p := range want {
			if phases[i] != p {
				t.Errorf("journal phase[%d] = %q; want %q", i, phases[i], p)
			}
		}
	}

	// (b) Injection ordering: handoff text, /clear, /session-resume.
	texts := spy.texts()
	if len(texts) < 3 {
		t.Fatalf("want ≥3 inject calls; got %d: %v", len(texts), texts)
	}
	if !containsSubstr(texts[0], "/session-handoff") {
		t.Errorf("inject[0] should contain '/session-handoff'; got %q", texts[0])
	}
	if !containsSubstr(texts[0], nonce) {
		t.Errorf("inject[0] should contain nonce %q; got %q", nonce, texts[0])
	}
	if texts[1] != "/clear" {
		t.Errorf("inject[1] = %q; want \"/clear\"", texts[1])
	}
	if !containsSubstr(texts[2], "/session-resume") {
		t.Errorf("inject[2] should contain '/session-resume'; got %q", texts[2])
	}

	// (c) Events: handoff_started then cycle_complete; no cycle_aborted.
	handoffEvts := em.EventsOfType(core.EventTypeSessionKeeperHandoffStarted)
	if len(handoffEvts) != 1 {
		t.Errorf("want 1 handoff_started; got %d", len(handoffEvts))
	} else {
		var p core.SessionKeeperHandoffStartedPayload
		if err := json.Unmarshal(handoffEvts[0].Payload, &p); err != nil {
			t.Fatalf("unmarshal handoff_started: %v", err)
		}
		if p.CycleID != cycleID {
			t.Errorf("handoff_started.cycle_id = %q; want %q", p.CycleID, cycleID)
		}
	}

	completeEvts := em.EventsOfType(core.EventTypeSessionKeeperCycleComplete)
	if len(completeEvts) != 1 {
		t.Errorf("want 1 cycle_complete; got %d", len(completeEvts))
	} else {
		var p core.SessionKeeperCycleCompletePayload
		if err := json.Unmarshal(completeEvts[0].Payload, &p); err != nil {
			t.Fatalf("unmarshal cycle_complete: %v", err)
		}
		if p.PrevSessionID != prevSID {
			t.Errorf("cycle_complete.prev_session_id = %q; want %q", p.PrevSessionID, prevSID)
		}
	}

	abortedEvts := em.EventsOfType(core.EventTypeSessionKeeperCycleAborted)
	if len(abortedEvts) != 0 {
		t.Errorf("want 0 cycle_aborted in happy path; got %d", len(abortedEvts))
	}
}

// TestCycler_AbortOnNonceTimeout verifies the abort path: when the handoff nonce
// never appears before HandoffTimeout, the cycle ABORTS and NEVER issues /clear.
func TestCycler_AbortOnNonceTimeout(t *testing.T) {
	t.Parallel()

	const (
		agent   = "abort-agent"
		cycleID = "cyc-abort-001"
		sid     = "sess-abort"
	)

	em := &keeper.RecordingEmitter{}
	spy := &cycleSpyInjector{}
	jc := &journalCapture{}

	// ReadGaugeFn is unused in the abort path (never reaches settle step).
	noopGauge := func(_, _ string) (*keeper.CtxFile, time.Time, error) {
		return &keeper.CtxFile{Pct: 95.0, SessionID: sid}, time.Now(), nil
	}

	cycler := newTestCycler(
		agent, t.TempDir(), em, spy, jc, cycleID,
		handoffNeverReturnsNonce, noopGauge,
		true,                // crispIdle
		false,               // holdingDispatch
		60*time.Millisecond, // handoffTimeout — short for test
		30*time.Millisecond, // clearSettle
	)

	cf := &keeper.CtxFile{Pct: 95.0, SessionID: sid}
	if err := cycler.MaybeRun(context.Background(), cf); err != nil {
		t.Fatalf("MaybeRun: %v", err)
	}

	// (a) Journal must end in "aborted".
	phases := jc.snapshot()
	if len(phases) == 0 {
		t.Fatal("no journal phases recorded")
	}
	last := phases[len(phases)-1]
	if last != "aborted" {
		t.Errorf("last journal phase = %q; want \"aborted\"", last)
	}

	// (b) /clear must NEVER have been injected.
	for i, text := range spy.texts() {
		if text == "/clear" {
			t.Errorf("inject[%d] = %q: /clear must NEVER be issued on abort", i, text)
		}
	}

	// (c) cycle_aborted emitted; cycle_complete NOT emitted.
	abortedEvts := em.EventsOfType(core.EventTypeSessionKeeperCycleAborted)
	if len(abortedEvts) != 1 {
		t.Errorf("want 1 cycle_aborted; got %d", len(abortedEvts))
	} else {
		var p core.SessionKeeperCycleAbortedPayload
		if err := json.Unmarshal(abortedEvts[0].Payload, &p); err != nil {
			t.Fatalf("unmarshal cycle_aborted: %v", err)
		}
		if p.Reason != "handoff_timeout" {
			t.Errorf("cycle_aborted.reason = %q; want \"handoff_timeout\"", p.Reason)
		}
	}

	completeEvts := em.EventsOfType(core.EventTypeSessionKeeperCycleComplete)
	if len(completeEvts) != 0 {
		t.Errorf("want 0 cycle_complete on abort; got %d", len(completeEvts))
	}
}

// TestCycler_Gating verifies that the cycle does not fire when any gate fails:
// pct < act_pct, CrispIdle = false, or HoldingDispatch = true.
func TestCycler_Gating(t *testing.T) {
	t.Parallel()

	noopGauge := func(_, _ string) (*keeper.CtxFile, time.Time, error) {
		return &keeper.CtxFile{Pct: 95.0, SessionID: "sess-x"}, time.Now(), nil
	}
	alwaysNonce := func(_ string) (string, error) {
		return "<!-- KEEPER:any -->", nil
	}

	cases := []struct {
		name            string
		pct             float64
		crispIdle       bool
		holdingDispatch bool
	}{
		{"below_act_pct", 85.0, true, false},
		{"not_crisp_idle", 95.0, false, false},
		{"holding_dispatch", 95.0, true, true},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			em := &keeper.RecordingEmitter{}
			spy := &cycleSpyInjector{}
			jc := &journalCapture{}

			cycler := newTestCycler(
				"gate-agent", t.TempDir(), em, spy, jc, "cyc-gate-001",
				alwaysNonce, noopGauge,
				tc.crispIdle, tc.holdingDispatch,
				100*time.Millisecond, 30*time.Millisecond,
			)

			cf := &keeper.CtxFile{Pct: tc.pct, SessionID: "sess-x"}
			if err := cycler.MaybeRun(context.Background(), cf); err != nil {
				t.Fatalf("MaybeRun: %v", err)
			}

			// No injection and no events when gated.
			if n := len(spy.texts()); n != 0 {
				t.Errorf("gate %q: want 0 inject calls; got %d", tc.name, n)
			}
			if evts := em.EventsOfType(core.EventTypeSessionKeeperHandoffStarted); len(evts) != 0 {
				t.Errorf("gate %q: want 0 handoff_started; got %d", tc.name, len(evts))
			}
		})
	}
}

// TestCycler_NoRefireWithinSameSessionID verifies the anti-loop suppression:
// a second MaybeRun call with the same session_id must not re-fire the cycle.
func TestCycler_NoRefireWithinSameSessionID(t *testing.T) {
	t.Parallel()

	const (
		agent   = "anti-loop-agent"
		cycleID = "cyc-loop-001"
		sid     = "sess-stable"
	)

	em := &keeper.RecordingEmitter{}
	spy := &cycleSpyInjector{}
	jc := &journalCapture{}

	nonce := "<!-- KEEPER:" + cycleID + " -->"
	readHandoff := handoffReturnsNonceAfter(0, nonce) // nonce available immediately

	// Gauge always returns the same session_id (no /clear effect in fakes).
	stableGauge := func(_, _ string) (*keeper.CtxFile, time.Time, error) {
		return &keeper.CtxFile{Pct: 95.0, SessionID: sid}, time.Now(), nil
	}

	cycler := newTestCycler(
		agent, t.TempDir(), em, spy, jc, cycleID,
		readHandoff, stableGauge,
		true, false,
		200*time.Millisecond, 30*time.Millisecond,
	)

	cf := &keeper.CtxFile{Pct: 95.0, SessionID: sid}

	// First call — should fire.
	if err := cycler.MaybeRun(context.Background(), cf); err != nil {
		t.Fatalf("first MaybeRun: %v", err)
	}
	firstCount := len(em.EventsOfType(core.EventTypeSessionKeeperHandoffStarted))
	if firstCount != 1 {
		t.Fatalf("want 1 handoff_started after first run; got %d", firstCount)
	}

	// Second call with SAME session_id — must NOT re-fire.
	if err := cycler.MaybeRun(context.Background(), cf); err != nil {
		t.Fatalf("second MaybeRun: %v", err)
	}
	secondCount := len(em.EventsOfType(core.EventTypeSessionKeeperHandoffStarted))
	if secondCount != firstCount {
		t.Errorf("want exactly %d handoff_started after same-session second call; got %d (re-fired)", firstCount, secondCount)
	}
}

// containsSubstr is a helper to check substring presence.
func containsSubstr(s, sub string) bool {
	return len(s) >= len(sub) && func() bool {
		for i := 0; i <= len(s)-len(sub); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	}()
}
