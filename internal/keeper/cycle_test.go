package keeper_test

import (
	"context"
	"encoding/json"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/keeper"
	"github.com/gregberns/harmonik/internal/substrate"
)

// steppingAdvanceClock is a test ClockPort for deflaking the Cycler
// interval/timeout tests (hk-h0twl, follow-up to hk-3dn16). Now() auto-steps
// virtual time by `step` on each call, so a drive-loop timeout trips after a
// DETERMINISTIC number of polls (independent of real -race scheduling), exactly
// like the awaitack fakeClock. NewTicker returns a REAL ticker so the drive loop
// still iterates (the real ticker only paces WHEN polls happen; the virtual
// stepping governs HOW MANY polls reach a timeout). Advance jumps virtual time
// explicitly, replacing a real time.Sleep for "interval elapsed" transitions —
// so an interval window can be made arbitrarily large in VIRTUAL time (huge
// deterministic margin) without slowing the test. Sleep is a no-op that respects
// ctx.
type steppingAdvanceClock struct {
	mu   sync.Mutex
	now  time.Time
	step time.Duration
}

func newSteppingAdvanceClock(t0 time.Time, step time.Duration) *steppingAdvanceClock {
	return &steppingAdvanceClock{now: t0, step: step}
}

func (c *steppingAdvanceClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(c.step)
	return c.now
}

func (c *steppingAdvanceClock) Since(t time.Time) time.Duration { return c.Now().Sub(t) }

func (c *steppingAdvanceClock) NewTicker(d time.Duration) substrate.Ticker {
	return substrate.SystemClock{}.NewTicker(d)
}

func (c *steppingAdvanceClock) Sleep(ctx context.Context, _ time.Duration) bool {
	select {
	case <-ctx.Done():
		return false
	default:
		return true
	}
}

// Advance jumps virtual time forward by d without a real sleep — used to cross an
// interval/cooldown boundary deterministically.
func (c *steppingAdvanceClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	c.mu.Unlock()
}

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
	last   *keeper.CycleJournal
}

func (jc *journalCapture) write(_ string, j *keeper.CycleJournal) error {
	jc.mu.Lock()
	defer jc.mu.Unlock()
	jc.phases = append(jc.phases, j.Phase)
	cp := *j
	jc.last = &cp
	return nil
}

func (jc *journalCapture) snapshot() []string {
	jc.mu.Lock()
	defer jc.mu.Unlock()
	out := make([]string, len(jc.phases))
	copy(out, jc.phases)
	return out
}

func (jc *journalCapture) lastJournal() *keeper.CycleJournal {
	jc.mu.Lock()
	defer jc.mu.Unlock()
	return jc.last
}

// journalStore is a read-write fake journal store used for crash recovery tests.
type journalStore struct {
	mu sync.Mutex
	j  *keeper.CycleJournal
}

func (js *journalStore) write(_ string, j *keeper.CycleJournal) error {
	js.mu.Lock()
	defer js.mu.Unlock()
	cp := *j
	js.j = &cp
	return nil
}

func (js *journalStore) read(_ string) (*keeper.CycleJournal, error) {
	js.mu.Lock()
	defer js.mu.Unlock()
	if js.j == nil {
		return nil, os.ErrNotExist
	}
	cp := *js.j
	return &cp, nil
}

func (js *journalStore) lastJournal() *keeper.CycleJournal {
	js.mu.Lock()
	defer js.mu.Unlock()
	if js.j == nil {
		return nil
	}
	cp := *js.j
	return &cp
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
// isManaged controls whether the IsManagedFn returns true (managed) or false.
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
	return newTestCyclerManaged(agentName, projectDir, em, spy, jc, cycleID,
		readHandoff, readGaugeFn, crispIdle, holdingDispatch, handoffTimeout, clearSettle, true)
}

func newTestCyclerManaged(
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
	isManaged bool,
) *keeper.Cycler {
	cfg := keeper.CyclerConfig{
		AgentName:      agentName,
		ProjectDir:     projectDir,
		TmuxTarget:     "fake-pane",
		ActPct:         90.0,
		WarnPct:        80.0,
		HandoffTimeout: handoffTimeout,
		ClearSettle:    clearSettle,
		PollInterval:   10 * time.Millisecond,
		CycleIDGen:     func() string { return cycleID },
		IsManagedFn:    func(_, _ string) bool { return isManaged },
		HandoffFilePath: func(_, agent string) string {
			return "/tmp/HANDOFF-" + agent + ".md"
		},
		ReadHandoff:       readHandoff,
		TruncateHandoffFn: func(_ string) error { return nil }, // no-op in most tests
		InjectFn:          spy.inject,
		ReadGaugeFn:       readGaugeFn,
		CrispIdleFn:       func(_, _ string) bool { return crispIdle },
		HoldingDispatchFn: func(_, _ string) bool { return holdingDispatch },
		WriteJournalFn:    jc.write,
		SetTmuxEnvFn:      func(_ context.Context, _, _, _ string) error { return nil }, // no-op in most tests
		// Stop hook wired and freshly fired (T8, SK-014): the .idle marker
		// reads as "await-input boundary now", so ModelDone{idle_marker} lands
		// on the first AwaitModelDone detection tick — the real primary path,
		// with no added wait (the pre-T8 clear-right-after-confirm cadence).
		IdleMarkerModTimeFn: idleMarkerFreshNow,
	}
	return keeper.NewCycler(cfg, em)
}

// idleMarkerFreshNow is the shared test IdleMarkerModTimeFn: a Stop-hook
// .idle marker whose mtime is always "now" (≥ t_nonce on the first
// AwaitModelDone poll). Tests exercising the timeout/backstop paths override
// it explicitly.
func idleMarkerFreshNow(_, _ string) (time.Time, bool) { return time.Now(), true }

// TestCycler_HappyPath verifies the full 7-step ordering:
// journal(opened) → handoff inject → nonce confirmed → /clear inject →
// agent-brief inject → journal(complete) → cycle_complete event.
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

	// (b) Injection ordering: handoff text, /clear, agent brief.
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
	// hk-pgtt6: the directive must be ONE line with a VISIBLE separator. Claude
	// Code collapses a pasted "\n\n" away entirely, fusing the handoff path onto
	// the instruction; a trailing space is not enough. See
	// inject_directive_shape_hkpgtt6_test.go for the full collapse assertion.
	if containsSubstr(texts[0], "\n") {
		t.Errorf("inject[0] must be a single line (hk-pgtt6); got %q", texts[0])
	}
	// hk-4tjyj: the reboot command must be self-describing, not dependent on
	// $HARMONIK_AGENT and the pane's CWD.
	if !containsSubstr(texts[2], "--agent ") || !containsSubstr(texts[2], "--project ") {
		t.Errorf("inject[2] should pin --agent and --project (hk-4tjyj); got %q", texts[2])
	}
	if texts[1] != "/clear" {
		t.Errorf("inject[1] = %q; want \"/clear\"", texts[1])
	}
	if !containsSubstr(texts[2], "agent brief") {
		t.Errorf("inject[2] should contain 'agent brief'; got %q", texts[2])
	}
	if !containsSubstr(texts[2], "keeper-restart") {
		t.Errorf("inject[2] should contain 'keeper-restart'; got %q", texts[2])
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
		// pct=92: above ActPct (90) but below ForceActPct (95) — CrispIdle still gates.
		{"not_crisp_idle", 92.0, false, false},
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

// TestCycler_EmptySessionIDNeverFires verifies DEFECT-1: when session_id is
// empty the cycle must never fire regardless of pct or other conditions.
func TestCycler_EmptySessionIDNeverFires(t *testing.T) {
	t.Parallel()

	em := &keeper.RecordingEmitter{}
	spy := &cycleSpyInjector{}
	jc := &journalCapture{}

	alwaysNonce := func(_ string) (string, error) {
		return "<!-- KEEPER:any -->", nil
	}
	noopGauge := func(_, _ string) (*keeper.CtxFile, time.Time, error) {
		return &keeper.CtxFile{Pct: 95.0}, time.Now(), nil
	}

	cycler := newTestCycler(
		"empty-sid-agent", t.TempDir(), em, spy, jc, "cyc-empty-001",
		alwaysNonce, noopGauge,
		true, false,
		100*time.Millisecond, 30*time.Millisecond,
	)

	// session_id is "" — must not fire even at pct >= act_pct.
	cf := &keeper.CtxFile{Pct: 95.0, SessionID: ""}
	for i := 0; i < 5; i++ {
		if err := cycler.MaybeRun(context.Background(), cf); err != nil {
			t.Fatalf("MaybeRun[%d]: %v", i, err)
		}
	}

	if n := len(spy.texts()); n != 0 {
		t.Errorf("want 0 inject calls with empty session_id; got %d", n)
	}
	if evts := em.EventsOfType(core.EventTypeSessionKeeperHandoffStarted); len(evts) != 0 {
		t.Errorf("want 0 handoff_started with empty session_id; got %d", len(evts))
	}
}

// TestCycler_NilCtxFileNoPanic verifies the MaybeRun nil-guard: a CF-less tick
// (cf == nil) must be skipped gracefully rather than crashing the keeper when
// the reactor's gate ladder dereferences ev.CF.
func TestCycler_NilCtxFileNoPanic(t *testing.T) {
	t.Parallel()

	em := &keeper.RecordingEmitter{}
	spy := &cycleSpyInjector{}
	jc := &journalCapture{}

	alwaysNonce := func(_ string) (string, error) {
		return "<!-- KEEPER:any -->", nil
	}
	noopGauge := func(_, _ string) (*keeper.CtxFile, time.Time, error) {
		return &keeper.CtxFile{Pct: 95.0}, time.Now(), nil
	}

	cycler := newTestCycler(
		"nil-cf-agent", t.TempDir(), em, spy, jc, "cyc-nilcf-001",
		alwaysNonce, noopGauge,
		true, false,
		100*time.Millisecond, 30*time.Millisecond,
	)

	// nil cf must not panic and must not fire.
	if err := cycler.MaybeRun(context.Background(), nil); err != nil {
		t.Fatalf("MaybeRun(nil): %v", err)
	}
	if n := len(spy.texts()); n != 0 {
		t.Errorf("want 0 inject calls with nil cf; got %d", n)
	}
}

// TestCycler_AbortDoesNotRefire verifies DEFECT-4: after an abort the cycle
// must not re-fire on the same session_id on the very next tick.
func TestCycler_AbortDoesNotRefire(t *testing.T) {
	t.Parallel()

	const (
		agent   = "abort-nofire-agent"
		cycleID = "cyc-abort-nofire"
		sid     = "sess-abort-nofire"
	)

	em := &keeper.RecordingEmitter{}
	spy := &cycleSpyInjector{}
	jc := &journalCapture{}

	noopGauge := func(_, _ string) (*keeper.CtxFile, time.Time, error) {
		return &keeper.CtxFile{Pct: 95.0, SessionID: sid}, time.Now(), nil
	}

	cycler := newTestCycler(
		agent, t.TempDir(), em, spy, jc, cycleID,
		handoffNeverReturnsNonce, noopGauge,
		true, false,
		40*time.Millisecond, // short timeout to trigger abort quickly
		20*time.Millisecond,
	)

	cf := &keeper.CtxFile{Pct: 95.0, SessionID: sid}

	// First call: fires but aborts (nonce never appears).
	if err := cycler.MaybeRun(context.Background(), cf); err != nil {
		t.Fatalf("first MaybeRun: %v", err)
	}
	abortedAfterFirst := len(em.EventsOfType(core.EventTypeSessionKeeperCycleAborted))
	if abortedAfterFirst != 1 {
		t.Fatalf("want 1 cycle_aborted after first run; got %d", abortedAfterFirst)
	}

	// Second call: same session_id at high pct — must NOT re-fire (DEFECT-4 fix).
	if err := cycler.MaybeRun(context.Background(), cf); err != nil {
		t.Fatalf("second MaybeRun: %v", err)
	}
	// No new handoff_started events — suppressed by abort.
	handoffEvts := len(em.EventsOfType(core.EventTypeSessionKeeperHandoffStarted))
	// handoff_started is emitted right after journal open, before the abort.
	// There should be exactly 1 (from the first run, not a second).
	abortedAfterSecond := len(em.EventsOfType(core.EventTypeSessionKeeperCycleAborted))
	if abortedAfterSecond != abortedAfterFirst {
		t.Errorf("want no new cycle_aborted after second call; got %d total (was %d)", abortedAfterSecond, abortedAfterFirst)
	}
	if handoffEvts != 1 {
		t.Errorf("want 1 handoff_started (emitted before abort); got %d", handoffEvts)
	}
}

// TestCycler_ManagedGuardInsideMaybeRun verifies DEFECT-3: the .managed check
// is enforced inside MaybeRun, not only in the caller.
func TestCycler_ManagedGuardInsideMaybeRun(t *testing.T) {
	t.Parallel()

	em := &keeper.RecordingEmitter{}
	spy := &cycleSpyInjector{}
	jc := &journalCapture{}

	alwaysNonce := func(_ string) (string, error) { return "<!-- KEEPER:any -->", nil }
	noopGauge := func(_, _ string) (*keeper.CtxFile, time.Time, error) {
		return &keeper.CtxFile{Pct: 95.0, SessionID: "sess-managed"}, time.Now(), nil
	}

	// isManaged = false: managed guard should prevent firing.
	cycler := newTestCyclerManaged(
		"unmanaged-agent", t.TempDir(), em, spy, jc, "cyc-managed-001",
		alwaysNonce, noopGauge,
		true, false,
		100*time.Millisecond, 30*time.Millisecond,
		false, // not managed
	)

	cf := &keeper.CtxFile{Pct: 95.0, SessionID: "sess-managed"}
	if err := cycler.MaybeRun(context.Background(), cf); err != nil {
		t.Fatalf("MaybeRun: %v", err)
	}

	if n := len(spy.texts()); n != 0 {
		t.Errorf("unmanaged: want 0 inject calls; got %d", n)
	}
	if evts := em.EventsOfType(core.EventTypeSessionKeeperHandoffStarted); len(evts) != 0 {
		t.Errorf("unmanaged: want 0 handoff_started; got %d", len(evts))
	}
}

// TestCycler_SuppressionRequiresBothConditions verifies the full anti-loop
// spec: after a cycle completes, the Cycler stays suppressed until BOTH a new
// session_id is observed AND pct has been seen below WarnPct on that session.
func TestCycler_SuppressionRequiresBothConditions(t *testing.T) {
	t.Parallel()

	const (
		agent   = "rearm-agent"
		cycleID = "cyc-rearm-001"
		prevSID = "sess-old"
		newSID  = "sess-new"
		warnPct = 80.0
		actPct  = 90.0
	)

	em := &keeper.RecordingEmitter{}
	spy := &cycleSpyInjector{}
	jc := &journalCapture{}

	nonce := "<!-- KEEPER:" + cycleID + " -->"
	readHandoff := handoffReturnsNonceAfter(0, nonce)
	stableGauge := func(_, _ string) (*keeper.CtxFile, time.Time, error) {
		return &keeper.CtxFile{Pct: 95.0, SessionID: newSID}, time.Now(), nil
	}

	cfg := keeper.CyclerConfig{
		IdleMarkerModTimeFn: idleMarkerFreshNow, // Stop hook wired: model-done on first AwaitModelDone poll (T8)
		AgentName:           agent,
		ProjectDir:          t.TempDir(),
		TmuxTarget:          "fake-pane",
		ActPct:              actPct,
		WarnPct:             warnPct,
		HandoffTimeout:      500 * time.Millisecond,
		ClearSettle:         50 * time.Millisecond,
		PollInterval:        10 * time.Millisecond,
		CycleIDGen:          func() string { return cycleID },
		IsManagedFn:         func(_, _ string) bool { return true },
		HandoffFilePath:     func(_, a string) string { return "/tmp/HANDOFF-" + a + ".md" },
		ReadHandoff:         readHandoff,
		TruncateHandoffFn:   func(_ string) error { return nil },
		InjectFn:            spy.inject,
		ReadGaugeFn:         stableGauge,
		CrispIdleFn:         func(_, _ string) bool { return true },
		HoldingDispatchFn:   func(_, _ string) bool { return false },
		WriteJournalFn:      jc.write,
	}
	cycler := keeper.NewCycler(cfg, em)

	// Step 1: fire the cycle on prevSID at high pct.
	cf := &keeper.CtxFile{Pct: 95.0, SessionID: prevSID}
	if err := cycler.MaybeRun(context.Background(), cf); err != nil {
		t.Fatalf("first MaybeRun: %v", err)
	}
	if n := len(em.EventsOfType(core.EventTypeSessionKeeperHandoffStarted)); n != 1 {
		t.Fatalf("want 1 handoff_started after first fire; got %d", n)
	}

	// Step 2: new session_id but pct still above WarnPct — must still be suppressed.
	cfNewHighPct := &keeper.CtxFile{Pct: 95.0, SessionID: newSID}
	if err := cycler.MaybeRun(context.Background(), cfNewHighPct); err != nil {
		t.Fatalf("second MaybeRun (new SID, high pct): %v", err)
	}
	if n := len(em.EventsOfType(core.EventTypeSessionKeeperHandoffStarted)); n != 1 {
		t.Errorf("want still 1 handoff_started before low-pct seen; got %d (re-fired prematurely)", n)
	}

	// Step 3: observe pct below WarnPct on newSID — re-arms the cycler.
	cfLowPct := &keeper.CtxFile{Pct: 70.0, SessionID: newSID} // below 80 = warnPct
	if err := cycler.MaybeRun(context.Background(), cfLowPct); err != nil {
		t.Fatalf("MaybeRun (low pct): %v", err)
	}
	// pct < actPct → still gated; but re-arm flag should now be set.
	if n := len(em.EventsOfType(core.EventTypeSessionKeeperHandoffStarted)); n != 1 {
		t.Errorf("want 1 handoff_started (not re-fired at low pct); got %d", n)
	}

	// Step 4: now pct crosses actPct on newSID — should fire.
	cfNewHighPct2 := &keeper.CtxFile{Pct: 95.0, SessionID: newSID}
	if err := cycler.MaybeRun(context.Background(), cfNewHighPct2); err != nil {
		t.Fatalf("MaybeRun (new SID, high pct, armed): %v", err)
	}
	if n := len(em.EventsOfType(core.EventTypeSessionKeeperHandoffStarted)); n != 2 {
		t.Errorf("want 2 handoff_started after re-arm + new fire; got %d", n)
	}
}

// TestCycler_BootRecovery_PhaseCleared verifies that RecoverFromCrash injects
// agent brief when the journal phase is "cleared" (crashed after /clear,
// before /resume).
func TestCycler_BootRecovery_PhaseCleared(t *testing.T) {
	t.Parallel()

	const (
		agent   = "recover-cleared-agent"
		cycleID = "cyc-recover-001"
	)

	em := &keeper.RecordingEmitter{}
	spy := &cycleSpyInjector{}
	js := &journalStore{j: &keeper.CycleJournal{
		CycleID:   cycleID,
		Phase:     "cleared",
		OpenedAt:  time.Now().Add(-5 * time.Minute),
		UpdatedAt: time.Now().Add(-5 * time.Minute),
	}}

	cfg := keeper.CyclerConfig{
		IdleMarkerModTimeFn: idleMarkerFreshNow, // Stop hook wired: model-done on first AwaitModelDone poll (T8)
		AgentName:           agent,
		ProjectDir:          t.TempDir(),
		TmuxTarget:          "fake-pane",
		ActPct:              90.0,
		WarnPct:             80.0,
		IsManagedFn:         func(_, _ string) bool { return true },
		HandoffFilePath:     func(_, a string) string { return "/tmp/HANDOFF-" + a + ".md" },
		TruncateHandoffFn:   func(_ string) error { return nil },
		InjectFn:            spy.inject,
		CrispIdleFn:         func(_, _ string) bool { return true },
		HoldingDispatchFn:   func(_, _ string) bool { return false },
		WriteJournalFn:      js.write,
		ReadJournalFn:       js.read,
	}
	cycler := keeper.NewCycler(cfg, em)

	if err := cycler.RecoverFromCrash(context.Background()); err != nil {
		t.Fatalf("RecoverFromCrash: %v", err)
	}

	// agent brief must have been injected (not /session-resume — T8 / I1).
	texts := spy.texts()
	if len(texts) != 1 {
		t.Fatalf("want 1 inject call; got %d: %v", len(texts), texts)
	}
	if !containsSubstr(texts[0], "agent brief") {
		t.Errorf("inject[0] should contain 'agent brief'; got %q", texts[0])
	}
	if !containsSubstr(texts[0], "keeper-restart") {
		t.Errorf("inject[0] should contain 'keeper-restart'; got %q", texts[0])
	}

	// Journal must be closed (phase = "complete").
	j := js.lastJournal()
	if j == nil {
		t.Fatal("want journal written; got nil")
	}
	if j.Phase != "complete" {
		t.Errorf("journal phase = %q; want \"complete\"", j.Phase)
	}

	// session_keeper_cycle_recovered must be emitted.
	recoveredEvts := em.EventsOfType(core.EventTypeSessionKeeperCycleRecovered)
	if len(recoveredEvts) != 1 {
		t.Fatalf("want 1 cycle_recovered; got %d", len(recoveredEvts))
	}
	var p core.SessionKeeperCycleRecoveredPayload
	if err := json.Unmarshal(recoveredEvts[0].Payload, &p); err != nil {
		t.Fatalf("unmarshal cycle_recovered: %v", err)
	}
	if p.PhaseAtCrash != "cleared" {
		t.Errorf("cycle_recovered.phase_at_crash = %q; want \"cleared\"", p.PhaseAtCrash)
	}
}

// TestCycler_BootRecovery_PhaseHandoff verifies that RecoverFromCrash
// discards (aborts) the journal when phase is "handoff_injected" (crashed
// before /clear — safe to discard, no injection needed).
func TestCycler_BootRecovery_PhaseHandoff(t *testing.T) {
	t.Parallel()

	const (
		agent   = "recover-handoff-agent"
		cycleID = "cyc-recover-handoff"
	)

	em := &keeper.RecordingEmitter{}
	spy := &cycleSpyInjector{}
	js := &journalStore{j: &keeper.CycleJournal{
		CycleID:   cycleID,
		Phase:     "handoff_injected",
		OpenedAt:  time.Now().Add(-5 * time.Minute),
		UpdatedAt: time.Now().Add(-5 * time.Minute),
	}}

	cfg := keeper.CyclerConfig{
		IdleMarkerModTimeFn: idleMarkerFreshNow, // Stop hook wired: model-done on first AwaitModelDone poll (T8)
		AgentName:           agent,
		ProjectDir:          t.TempDir(),
		TmuxTarget:          "fake-pane",
		ActPct:              90.0,
		WarnPct:             80.0,
		IsManagedFn:         func(_, _ string) bool { return true },
		HandoffFilePath:     func(_, a string) string { return "/tmp/HANDOFF-" + a + ".md" },
		TruncateHandoffFn:   func(_ string) error { return nil },
		InjectFn:            spy.inject,
		CrispIdleFn:         func(_, _ string) bool { return true },
		HoldingDispatchFn:   func(_, _ string) bool { return false },
		WriteJournalFn:      js.write,
		ReadJournalFn:       js.read,
	}
	cycler := keeper.NewCycler(cfg, em)

	if err := cycler.RecoverFromCrash(context.Background()); err != nil {
		t.Fatalf("RecoverFromCrash: %v", err)
	}

	// No injection — /clear was never issued.
	if n := len(spy.texts()); n != 0 {
		t.Errorf("want 0 inject calls for handoff_injected phase; got %d: %v", n, spy.texts())
	}

	// Journal must be aborted.
	j := js.lastJournal()
	if j == nil {
		t.Fatal("want journal written; got nil")
	}
	if j.Phase != "aborted" {
		t.Errorf("journal phase = %q; want \"aborted\"", j.Phase)
	}

	// No cycle_recovered event (no action taken).
	recoveredEvts := em.EventsOfType(core.EventTypeSessionKeeperCycleRecovered)
	if len(recoveredEvts) != 0 {
		t.Errorf("want 0 cycle_recovered for handoff phase; got %d", len(recoveredEvts))
	}
}

// TestCycler_BootRecovery_PhaseComplete verifies that RecoverFromCrash is a
// no-op when the journal is already in a terminal state.
func TestCycler_BootRecovery_PhaseComplete(t *testing.T) {
	t.Parallel()

	const (
		agent   = "recover-complete-agent"
		cycleID = "cyc-recover-complete"
	)

	em := &keeper.RecordingEmitter{}
	spy := &cycleSpyInjector{}

	for _, phase := range []string{"complete", "aborted"} {
		phase := phase
		t.Run(phase, func(t *testing.T) {
			t.Parallel()

			js := &journalStore{j: &keeper.CycleJournal{
				CycleID:   cycleID,
				Phase:     phase,
				OpenedAt:  time.Now().Add(-5 * time.Minute),
				UpdatedAt: time.Now().Add(-5 * time.Minute),
			}}
			var writeCount int
			cfg := keeper.CyclerConfig{
				IdleMarkerModTimeFn: idleMarkerFreshNow, // Stop hook wired: model-done on first AwaitModelDone poll (T8)
				AgentName:           agent,
				ProjectDir:          t.TempDir(),
				TmuxTarget:          "fake-pane",
				ActPct:              90.0,
				WarnPct:             80.0,
				IsManagedFn:         func(_, _ string) bool { return true },
				HandoffFilePath:     func(_, a string) string { return "/tmp/HANDOFF-" + a + ".md" },
				TruncateHandoffFn:   func(_ string) error { return nil },
				InjectFn:            spy.inject,
				CrispIdleFn:         func(_, _ string) bool { return true },
				HoldingDispatchFn:   func(_, _ string) bool { return false },
				WriteJournalFn: func(_ string, _ *keeper.CycleJournal) error {
					writeCount++
					return js.write("", &keeper.CycleJournal{})
				},
				ReadJournalFn: js.read,
			}
			cycler := keeper.NewCycler(cfg, em)

			if err := cycler.RecoverFromCrash(context.Background()); err != nil {
				t.Fatalf("RecoverFromCrash: %v", err)
			}

			if n := len(spy.texts()); n != 0 {
				t.Errorf("phase %q: want 0 inject calls; got %d", phase, n)
			}
			if writeCount != 0 {
				t.Errorf("phase %q: want 0 journal writes (no-op); got %d", phase, writeCount)
			}
		})
	}
}

// TestCycler_BootRecovery_NoJournal verifies that RecoverFromCrash is a
// no-op when no journal file exists on disk.
func TestCycler_BootRecovery_NoJournal(t *testing.T) {
	t.Parallel()

	em := &keeper.RecordingEmitter{}
	spy := &cycleSpyInjector{}
	js := &journalStore{} // j == nil → read returns journalNotFoundError

	cfg := keeper.CyclerConfig{
		IdleMarkerModTimeFn: idleMarkerFreshNow, // Stop hook wired: model-done on first AwaitModelDone poll (T8)
		AgentName:           "no-journal-agent",
		ProjectDir:          t.TempDir(),
		TmuxTarget:          "fake-pane",
		ActPct:              90.0,
		WarnPct:             80.0,
		IsManagedFn:         func(_, _ string) bool { return true },
		HandoffFilePath:     func(_, a string) string { return "/tmp/HANDOFF-" + a + ".md" },
		TruncateHandoffFn:   func(_ string) error { return nil },
		InjectFn:            spy.inject,
		CrispIdleFn:         func(_, _ string) bool { return true },
		HoldingDispatchFn:   func(_, _ string) bool { return false },
		WriteJournalFn:      js.write,
		ReadJournalFn:       js.read,
	}
	cycler := keeper.NewCycler(cfg, em)

	if err := cycler.RecoverFromCrash(context.Background()); err != nil {
		t.Fatalf("RecoverFromCrash with no journal: %v", err)
	}

	if n := len(spy.texts()); n != 0 {
		t.Errorf("want 0 inject calls with no journal; got %d", n)
	}
}

// TestCycler_BootRecovery_UnmanagedNoOp verifies that RecoverFromCrash
// respects the .managed guard: no action taken for an unmanaged agent.
func TestCycler_BootRecovery_UnmanagedNoOp(t *testing.T) {
	t.Parallel()

	em := &keeper.RecordingEmitter{}
	spy := &cycleSpyInjector{}
	js := &journalStore{j: &keeper.CycleJournal{
		CycleID:   "cyc-unmanaged-recovery",
		Phase:     "cleared",
		OpenedAt:  time.Now().Add(-5 * time.Minute),
		UpdatedAt: time.Now().Add(-5 * time.Minute),
	}}

	cfg := keeper.CyclerConfig{
		IdleMarkerModTimeFn: idleMarkerFreshNow, // Stop hook wired: model-done on first AwaitModelDone poll (T8)
		AgentName:           "unmanaged-recover-agent",
		ProjectDir:          t.TempDir(),
		TmuxTarget:          "fake-pane",
		IsManagedFn:         func(_, _ string) bool { return false }, // unmanaged
		HandoffFilePath:     func(_, a string) string { return "/tmp/HANDOFF-" + a + ".md" },
		TruncateHandoffFn:   func(_ string) error { return nil },
		InjectFn:            spy.inject,
		CrispIdleFn:         func(_, _ string) bool { return true },
		HoldingDispatchFn:   func(_, _ string) bool { return false },
		WriteJournalFn:      js.write,
		ReadJournalFn:       js.read,
	}
	cycler := keeper.NewCycler(cfg, em)

	if err := cycler.RecoverFromCrash(context.Background()); err != nil {
		t.Fatalf("RecoverFromCrash unmanaged: %v", err)
	}

	if n := len(spy.texts()); n != 0 {
		t.Errorf("unmanaged: want 0 inject calls; got %d", n)
	}
}

// TestCycler_TruncateCalledBeforePoll verifies DEFECT-2: the handoff file is
// truncated (clearing any stale nonce) before the nonce poll begins, so a
// pre-crash leftover cannot pre-satisfy the new cycle's poll.
func TestCycler_TruncateCalledBeforePoll(t *testing.T) {
	t.Parallel()

	const (
		agent      = "truncate-agent"
		newCycleID = "cyc-truncate-new"
		sid        = "sess-truncate"
	)
	newNonce := "<!-- KEEPER:" + newCycleID + " -->"

	var (
		mu             sync.Mutex
		truncateCalled bool
		pollCount      int
	)

	// ReadHandoff: before truncation, return a stale nonce (wrong cycle ID).
	// After truncation, return empty until a threshold, then return new nonce.
	readHandoff := func(_ string) (string, error) {
		mu.Lock()
		defer mu.Unlock()
		pollCount++
		if !truncateCalled {
			// Stale content with an old nonce — must NOT match newNonce.
			return "# Handoff\n\n<!-- KEEPER:cyc-OLD-stale -->\n", nil
		}
		if pollCount > 5 {
			return "# Handoff\n\n" + newNonce + "\n", nil
		}
		return "", context.DeadlineExceeded
	}

	truncateFn := func(_ string) error {
		mu.Lock()
		truncateCalled = true
		mu.Unlock()
		return nil
	}

	em := &keeper.RecordingEmitter{}
	spy := &cycleSpyInjector{}
	jc := &journalCapture{}

	noopGauge := func(_, _ string) (*keeper.CtxFile, time.Time, error) {
		return &keeper.CtxFile{Pct: 95.0, SessionID: sid}, time.Now(), nil
	}

	cfg := keeper.CyclerConfig{
		IdleMarkerModTimeFn: idleMarkerFreshNow, // Stop hook wired: model-done on first AwaitModelDone poll (T8)
		AgentName:           agent,
		ProjectDir:          t.TempDir(),
		TmuxTarget:          "fake-pane",
		ActPct:              90.0,
		WarnPct:             80.0,
		HandoffTimeout:      500 * time.Millisecond,
		ClearSettle:         100 * time.Millisecond,
		PollInterval:        10 * time.Millisecond,
		CycleIDGen:          func() string { return newCycleID },
		IsManagedFn:         func(_, _ string) bool { return true },
		HandoffFilePath:     func(_, a string) string { return "/tmp/HANDOFF-" + a + ".md" },
		ReadHandoff:         readHandoff,
		TruncateHandoffFn:   truncateFn,
		InjectFn:            spy.inject,
		ReadGaugeFn:         noopGauge,
		CrispIdleFn:         func(_, _ string) bool { return true },
		HoldingDispatchFn:   func(_, _ string) bool { return false },
		WriteJournalFn:      jc.write,
	}
	cycler := keeper.NewCycler(cfg, em)

	cf := &keeper.CtxFile{Pct: 95.0, SessionID: sid}
	if err := cycler.MaybeRun(context.Background(), cf); err != nil {
		t.Fatalf("MaybeRun: %v", err)
	}

	mu.Lock()
	called := truncateCalled
	mu.Unlock()

	if !called {
		t.Error("want TruncateHandoffFn called before poll; was not called")
	}

	// Cycle must complete (stale nonce did not pre-satisfy).
	phases := jc.snapshot()
	if len(phases) == 0 {
		t.Fatal("no journal phases recorded")
	}
	last := phases[len(phases)-1]
	if last != "complete" {
		t.Errorf("last journal phase = %q; want \"complete\" (stale nonce must not pre-satisfy)", last)
	}
}

// TestCycler_IdentityPinnedAfterNonceConfirm verifies that after nonce confirmation
// the keeper (a) sets HARMONIK_AGENT before /clear and (b) injects
// briefRestartCmd as step 6 (T8: identity re-pins from soul.md, not handoff).
func TestCycler_BriefRestartAfterNonceConfirm(t *testing.T) {
	t.Parallel()

	const (
		agent   = "flywheel"
		cycleID = "cyc-id-pin-001"
		prevSID = "sess-before-id"
		newSID  = "sess-after-id"
	)

	em := &keeper.RecordingEmitter{}
	jc := &journalCapture{}
	spy := &cycleSpyInjector{}

	nonce := "<!-- KEEPER:" + cycleID + " -->"
	readHandoff := handoffReturnsNonceAfter(1, nonce)
	readGaugeFn := gaugeReturnsNewSIDAfter(1, "", agent, prevSID, newSID)

	var (
		envKey      string
		envVal      string
		envOrder    int
		mu          sync.Mutex
		injectCount int
	)

	spyInject := func(_ context.Context, _, text string) error {
		mu.Lock()
		defer mu.Unlock()
		injectCount++
		_ = spy.inject(context.Background(), "fake-pane", text)
		return nil
	}
	setEnvFn := func(_ context.Context, _, key, value string) error {
		mu.Lock()
		defer mu.Unlock()
		envKey = key
		envVal = value
		envOrder = injectCount
		return nil
	}

	cfg := keeper.CyclerConfig{
		IdleMarkerModTimeFn: idleMarkerFreshNow, // Stop hook wired: model-done on first AwaitModelDone poll (T8)
		AgentName:           agent,
		ProjectDir:          t.TempDir(),
		TmuxTarget:          "fake-pane",
		ActPct:              90.0,
		WarnPct:             80.0,
		HandoffTimeout:      500 * time.Millisecond,
		ClearSettle:         100 * time.Millisecond,
		PollInterval:        10 * time.Millisecond,
		CycleIDGen:          func() string { return cycleID },
		IsManagedFn:         func(_, _ string) bool { return true },
		HandoffFilePath: func(_, a string) string {
			return "/tmp/HANDOFF-" + a + ".md"
		},
		ReadHandoff:       readHandoff,
		TruncateHandoffFn: func(_ string) error { return nil },
		InjectFn:          spyInject,
		ReadGaugeFn:       readGaugeFn,
		CrispIdleFn:       func(_, _ string) bool { return true },
		HoldingDispatchFn: func(_, _ string) bool { return false },
		WriteJournalFn:    jc.write,
		SetTmuxEnvFn:      setEnvFn,
	}
	cycler := keeper.NewCycler(cfg, em)

	cf := &keeper.CtxFile{Pct: 95.0, SessionID: prevSID}
	if err := cycler.MaybeRun(context.Background(), cf); err != nil {
		t.Fatalf("MaybeRun: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()

	// HARMONIK_AGENT must still be set before /clear (step 3b).
	if envKey != "HARMONIK_AGENT" {
		t.Errorf("SetTmuxEnvFn key = %q; want %q", envKey, "HARMONIK_AGENT")
	}
	if envVal != agent {
		t.Errorf("SetTmuxEnvFn value = %q; want %q", envVal, agent)
	}
	// env must be set before /clear (inject call #2).
	if envOrder >= 2 {
		t.Errorf("SetTmuxEnvFn called after /clear (inject count was %d; /clear is call 2)", envOrder)
	}

	// Step 6 inject must be briefRestartCmd, not /session-resume (T8 / I1).
	texts := spy.texts()
	if len(texts) < 3 {
		t.Fatalf("want ≥3 inject calls; got %d: %v", len(texts), texts)
	}
	if !containsSubstr(texts[2], "agent brief") {
		t.Errorf("inject[2] = %q; want agent brief (not /session-resume)", texts[2])
	}
	if !containsSubstr(texts[2], "keeper-restart") {
		t.Errorf("inject[2] = %q; want --wake keeper-restart", texts[2])
	}

	// Cycle must complete cleanly.
	phases := jc.snapshot()
	if len(phases) == 0 {
		t.Fatal("no journal phases recorded")
	}
	if last := phases[len(phases)-1]; last != "complete" {
		t.Errorf("last journal phase = %q; want \"complete\"", last)
	}
}

// TestCycler_AbsoluteTokenGate verifies that the cycle fires based on absolute
// token count rather than percentage when both Tokens and WindowSize are present.
//
// Scenario: a 1M-token window at 28% (280k tokens). With the old pct-based gate
// (ActPct=90) the cycle would NOT fire. With the absolute gate
// (ActAbsTokens=280k, ActPctCeil=0.85) the threshold is min(280k, 850k)=280k
// and the cycle SHOULD fire.
//
// This is the key regression guard for hk-cl74g: prevents the keeper from
// cycling ~3x too late on Opus-1M sessions.
func TestCycler_AbsoluteTokenGate(t *testing.T) {
	t.Parallel()

	const (
		agent   = "abs-gate-agent"
		cycleID = "cyc-abs-001"
		sid     = "sess-abs"
	)

	em := &keeper.RecordingEmitter{}
	spy := &cycleSpyInjector{}
	jc := &journalCapture{}

	nonce := "<!-- KEEPER:" + cycleID + " -->"
	readHandoff := handoffReturnsNonceAfter(0, nonce)
	noopGauge := func(_, _ string) (*keeper.CtxFile, time.Time, error) {
		return &keeper.CtxFile{Pct: 28.0, Tokens: 280_000, WindowSize: 1_000_000, SessionID: sid}, time.Now(), nil
	}

	cfg := keeper.CyclerConfig{
		IdleMarkerModTimeFn: idleMarkerFreshNow, // Stop hook wired: model-done on first AwaitModelDone poll (T8)
		AgentName:           agent,
		ProjectDir:          t.TempDir(),
		TmuxTarget:          "fake-pane",
		ActPct:              90.0, // pct gate would NOT fire at 28%
		WarnPct:             80.0,
		ActAbsTokens:        280_000, // absolute gate fires at exactly 280k
		ActPctCeil:          0.85,
		WarnAbsTokens:       220_000,
		WarnPctCeil:         0.70,
		HandoffTimeout:      200 * time.Millisecond,
		ClearSettle:         50 * time.Millisecond,
		PollInterval:        10 * time.Millisecond,
		CycleIDGen:          func() string { return cycleID },
		IsManagedFn:         func(_, _ string) bool { return true },
		HandoffFilePath: func(_, a string) string {
			return "/tmp/HANDOFF-" + a + ".md"
		},
		ReadHandoff:       readHandoff,
		TruncateHandoffFn: func(_ string) error { return nil },
		InjectFn:          spy.inject,
		ReadGaugeFn:       noopGauge,
		CrispIdleFn:       func(_, _ string) bool { return true },
		HoldingDispatchFn: func(_, _ string) bool { return false },
		WriteJournalFn:    jc.write,
		SetTmuxEnvFn:      func(_ context.Context, _, _, _ string) error { return nil },
	}
	cycler := keeper.NewCycler(cfg, em)

	cf := &keeper.CtxFile{Pct: 28.0, Tokens: 280_000, WindowSize: 1_000_000, SessionID: sid}
	if err := cycler.MaybeRun(context.Background(), cf); err != nil {
		t.Fatalf("MaybeRun: %v", err)
	}

	// Cycle must have fired despite low pct.
	handoffEvts := em.EventsOfType(core.EventTypeSessionKeeperHandoffStarted)
	if len(handoffEvts) != 1 {
		t.Errorf("want 1 handoff_started (absolute-token gate fired); got %d", len(handoffEvts))
	}
}

// TestCycler_AbsoluteTokenGate_BelowThreshold verifies that the cycle does NOT
// fire when total_input_tokens is below the effective abs threshold.
// Scenario: 1M window, 200k tokens (below min(280k, 850k)=280k). Old pct gate
// would also not fire (20% < 90%), but we want to confirm abs-gate path is taken.
func TestCycler_AbsoluteTokenGate_BelowThreshold(t *testing.T) {
	t.Parallel()

	const (
		agent   = "abs-below-agent"
		cycleID = "cyc-abs-below"
		sid     = "sess-abs-below"
	)

	em := &keeper.RecordingEmitter{}
	spy := &cycleSpyInjector{}
	jc := &journalCapture{}

	cfg := keeper.CyclerConfig{
		IdleMarkerModTimeFn: idleMarkerFreshNow, // Stop hook wired: model-done on first AwaitModelDone poll (T8)
		AgentName:           agent,
		ProjectDir:          t.TempDir(),
		TmuxTarget:          "fake-pane",
		ActPct:              90.0,
		WarnPct:             80.0,
		ActAbsTokens:        280_000,
		ActPctCeil:          0.85,
		WarnAbsTokens:       220_000,
		WarnPctCeil:         0.70,
		HandoffTimeout:      100 * time.Millisecond,
		ClearSettle:         30 * time.Millisecond,
		PollInterval:        10 * time.Millisecond,
		CycleIDGen:          func() string { return cycleID },
		IsManagedFn:         func(_, _ string) bool { return true },
		HandoffFilePath: func(_, a string) string {
			return "/tmp/HANDOFF-" + a + ".md"
		},
		ReadHandoff:       func(_ string) (string, error) { return "", nil },
		TruncateHandoffFn: func(_ string) error { return nil },
		InjectFn:          spy.inject,
		ReadGaugeFn: func(_, _ string) (*keeper.CtxFile, time.Time, error) {
			return &keeper.CtxFile{Pct: 20.0, Tokens: 200_000, WindowSize: 1_000_000, SessionID: sid}, time.Now(), nil
		},
		CrispIdleFn:       func(_, _ string) bool { return true },
		HoldingDispatchFn: func(_, _ string) bool { return false },
		WriteJournalFn:    jc.write,
	}
	cycler := keeper.NewCycler(cfg, em)

	cf := &keeper.CtxFile{Pct: 20.0, Tokens: 200_000, WindowSize: 1_000_000, SessionID: sid}
	if err := cycler.MaybeRun(context.Background(), cf); err != nil {
		t.Fatalf("MaybeRun: %v", err)
	}

	if n := len(spy.texts()); n != 0 {
		t.Errorf("want 0 inject calls (below abs threshold); got %d", n)
	}
	if evts := em.EventsOfType(core.EventTypeSessionKeeperHandoffStarted); len(evts) != 0 {
		t.Errorf("want 0 handoff_started below abs threshold; got %d", len(evts))
	}
}

// TestCycler_AbsoluteTokenGate_200kWindow verifies behaviour on a 200k window:
// min(280k, 0.85*200k=170k) = 170k. The cycle should fire at 170k tokens
// even though pct is only 85% (below the 90% pct gate).
func TestCycler_AbsoluteTokenGate_200kWindow(t *testing.T) {
	t.Parallel()

	const (
		agent   = "abs-200k-agent"
		cycleID = "cyc-abs-200k"
		sid     = "sess-200k"
	)

	em := &keeper.RecordingEmitter{}
	spy := &cycleSpyInjector{}
	jc := &journalCapture{}

	nonce := "<!-- KEEPER:" + cycleID + " -->"
	readHandoff := handoffReturnsNonceAfter(0, nonce)
	noopGauge := func(_, _ string) (*keeper.CtxFile, time.Time, error) {
		return &keeper.CtxFile{Pct: 85.0, Tokens: 170_000, WindowSize: 200_000, SessionID: sid}, time.Now(), nil
	}

	cfg := keeper.CyclerConfig{
		IdleMarkerModTimeFn: idleMarkerFreshNow, // Stop hook wired: model-done on first AwaitModelDone poll (T8)
		AgentName:           agent,
		ProjectDir:          t.TempDir(),
		TmuxTarget:          "fake-pane",
		ActPct:              90.0, // pct gate would NOT fire at 85%
		WarnPct:             80.0,
		ActAbsTokens:        280_000, // effective threshold = min(280k, 0.85*200k=170k) = 170k
		ActPctCeil:          0.85,
		WarnAbsTokens:       220_000,
		WarnPctCeil:         0.70,
		HandoffTimeout:      200 * time.Millisecond,
		ClearSettle:         50 * time.Millisecond,
		PollInterval:        10 * time.Millisecond,
		CycleIDGen:          func() string { return cycleID },
		IsManagedFn:         func(_, _ string) bool { return true },
		HandoffFilePath: func(_, a string) string {
			return "/tmp/HANDOFF-" + a + ".md"
		},
		ReadHandoff:       readHandoff,
		TruncateHandoffFn: func(_ string) error { return nil },
		InjectFn:          spy.inject,
		ReadGaugeFn:       noopGauge,
		CrispIdleFn:       func(_, _ string) bool { return true },
		HoldingDispatchFn: func(_, _ string) bool { return false },
		WriteJournalFn:    jc.write,
		SetTmuxEnvFn:      func(_ context.Context, _, _, _ string) error { return nil },
	}
	cycler := keeper.NewCycler(cfg, em)

	cf := &keeper.CtxFile{Pct: 85.0, Tokens: 170_000, WindowSize: 200_000, SessionID: sid}
	if err := cycler.MaybeRun(context.Background(), cf); err != nil {
		t.Fatalf("MaybeRun: %v", err)
	}

	if n := len(em.EventsOfType(core.EventTypeSessionKeeperHandoffStarted)); n != 1 {
		t.Errorf("want 1 handoff_started (pct-ceil gate on 200k window); got %d", n)
	}
}

// TestCycler_UpdatesManagedSessionAfterCycle verifies that SetManagedSessionFn is
// called with the new session_id after a successful cycle so the watcher's session
// binding advances to the resumed session. (Refs: hk-igt — session_id clobber fix)
func TestCycler_UpdatesManagedSessionAfterCycle(t *testing.T) {
	t.Parallel()

	const (
		agent   = "managed-update-agent"
		cycleID = "cyc-managed-001"
		prevSID = "sess-before"
		newSID  = "sess-after"
	)

	em := &keeper.RecordingEmitter{}
	spy := &cycleSpyInjector{}
	jc := &journalCapture{}

	nonce := "<!-- KEEPER:" + cycleID + " -->"
	readHandoff := handoffReturnsNonceAfter(1, nonce)
	readGaugeFn := gaugeReturnsNewSIDAfter(1, "", agent, prevSID, newSID)

	var gotProjectDir, gotAgent, gotSessionID string
	setManagedCalled := 0

	cfg := keeper.CyclerConfig{
		IdleMarkerModTimeFn: idleMarkerFreshNow, // Stop hook wired: model-done on first AwaitModelDone poll (T8)
		AgentName:           agent,
		ProjectDir:          t.TempDir(),
		TmuxTarget:          "fake-pane",
		ActPct:              90.0,
		WarnPct:             80.0,
		HandoffTimeout:      500 * time.Millisecond,
		ClearSettle:         200 * time.Millisecond,
		PollInterval:        10 * time.Millisecond,
		CycleIDGen:          func() string { return cycleID },
		IsManagedFn:         func(_, _ string) bool { return true },
		HandoffFilePath: func(_, a string) string {
			return "/tmp/HANDOFF-" + a + ".md"
		},
		ReadHandoff:       readHandoff,
		TruncateHandoffFn: func(_ string) error { return nil },
		InjectFn:          spy.inject,
		ReadGaugeFn:       readGaugeFn,
		CrispIdleFn:       func(_, _ string) bool { return true },
		HoldingDispatchFn: func(_, _ string) bool { return false },
		WriteJournalFn:    jc.write,
		SetTmuxEnvFn:      func(_ context.Context, _, _, _ string) error { return nil },
		SetManagedSessionFn: func(projectDir, agent, sessionID string) error {
			gotProjectDir = projectDir
			gotAgent = agent
			gotSessionID = sessionID
			setManagedCalled++
			return nil
		},
	}
	cycler := keeper.NewCycler(cfg, em)

	cf := &keeper.CtxFile{Pct: 95.0, SessionID: prevSID}
	if err := cycler.MaybeRun(context.Background(), cf); err != nil {
		t.Fatalf("MaybeRun: %v", err)
	}

	// SetManagedSessionFn must be called once with the new session_id.
	if setManagedCalled != 1 {
		t.Errorf("SetManagedSessionFn called %d times; want 1", setManagedCalled)
	}
	if gotSessionID != newSID {
		t.Errorf("SetManagedSessionFn session_id = %q; want %q", gotSessionID, newSID)
	}
	if gotAgent != agent {
		t.Errorf("SetManagedSessionFn agent = %q; want %q", gotAgent, agent)
	}
	_ = gotProjectDir // verified it was called; project dir value varies per test run
}

// TestCycler_ClearSettleTimeout_ClearsManagedSessionID verifies that when
// waitForNewSessionID times out (ClearSettle deadline expires without a new
// session_id), SetManagedSessionFn is still called — with an empty string —
// so the stale binding is cleared and the .sid channel can rebind the next
// session. (Refs: hk-uxu)
func TestCycler_ClearSettleTimeout_ClearsManagedSessionID(t *testing.T) {
	t.Parallel()

	const (
		agent   = "settle-timeout-agent"
		cycleID = "cyc-settle-001"
		prevSID = "sess-before-timeout"
	)

	em := &keeper.RecordingEmitter{}
	spy := &cycleSpyInjector{}
	jc := &journalCapture{}

	nonce := "<!-- KEEPER:" + cycleID + " -->"
	readHandoff := handoffReturnsNonceAfter(1, nonce)

	// Gauge always returns prevSID — new session_id never arrives; ClearSettle times out.
	readGaugeFn := func(_, _ string) (*keeper.CtxFile, time.Time, error) {
		return &keeper.CtxFile{Pct: 95.0, SessionID: prevSID}, time.Now(), nil
	}

	var gotSessionID string
	setManagedCalled := 0

	cfg := keeper.CyclerConfig{
		IdleMarkerModTimeFn: idleMarkerFreshNow, // Stop hook wired: model-done on first AwaitModelDone poll (T8)
		AgentName:           agent,
		ProjectDir:          t.TempDir(),
		TmuxTarget:          "fake-pane",
		ActPct:              90.0,
		WarnPct:             80.0,
		HandoffTimeout:      500 * time.Millisecond,
		ClearSettle:         50 * time.Millisecond, // short so the test is fast
		PollInterval:        10 * time.Millisecond,
		CycleIDGen:          func() string { return cycleID },
		IsManagedFn:         func(_, _ string) bool { return true },
		HandoffFilePath: func(_, a string) string {
			return "/tmp/HANDOFF-" + a + ".md"
		},
		ReadHandoff:       readHandoff,
		TruncateHandoffFn: func(_ string) error { return nil },
		InjectFn:          spy.inject,
		ReadGaugeFn:       readGaugeFn,
		CrispIdleFn:       func(_, _ string) bool { return true },
		HoldingDispatchFn: func(_, _ string) bool { return false },
		WriteJournalFn:    jc.write,
		SetTmuxEnvFn:      func(_ context.Context, _, _, _ string) error { return nil },
		SetManagedSessionFn: func(_, _, sessionID string) error {
			gotSessionID = sessionID
			setManagedCalled++
			return nil
		},
	}
	cycler := keeper.NewCycler(cfg, em)

	cf := &keeper.CtxFile{Pct: 95.0, SessionID: prevSID}
	if err := cycler.MaybeRun(context.Background(), cf); err != nil {
		t.Fatalf("MaybeRun: %v", err)
	}

	// SetManagedSessionFn must be called once, with empty string (timeout case).
	if setManagedCalled != 1 {
		t.Errorf("SetManagedSessionFn called %d times; want 1", setManagedCalled)
	}
	if gotSessionID != "" {
		t.Errorf("SetManagedSessionFn session_id = %q; want empty string (timeout path)", gotSessionID)
	}
}

// TestCycler_AntiLoopEscapeHatch_ResetOnSameSessionLowPct verifies that when
// lastFiredSID matches the current session_id but the context has dropped below
// WarnPct (real /clear happened; ClearSettle timed out so SID didn't change),
// the anti-loop state is reset so the keeper can re-arm on subsequent ticks.
// (Refs: hk-uxu)
func TestCycler_AntiLoopEscapeHatch_ResetOnSameSessionLowPct(t *testing.T) {
	t.Parallel()

	const (
		agent   = "escape-hatch-agent"
		cycleID = "cyc-escape-001"
		sid     = "sess-persistent"
	)

	em := &keeper.RecordingEmitter{}
	spy := &cycleSpyInjector{}
	jc := &journalCapture{}

	nonce := "<!-- KEEPER:" + cycleID + " -->"
	readHandoff := handoffReturnsNonceAfter(0, nonce) // nonce present immediately

	// Gauge always returns the same SID (timeout path), starting at high pct.
	var gaugePct float64 = 95.0
	readGaugeFn := func(_, _ string) (*keeper.CtxFile, time.Time, error) {
		return &keeper.CtxFile{Pct: gaugePct, SessionID: sid}, time.Now(), nil
	}

	cfg := keeper.CyclerConfig{
		IdleMarkerModTimeFn: idleMarkerFreshNow, // Stop hook wired: model-done on first AwaitModelDone poll (T8)
		AgentName:           agent,
		ProjectDir:          t.TempDir(),
		TmuxTarget:          "fake-pane",
		ActPct:              90.0,
		WarnPct:             80.0,
		HandoffTimeout:      500 * time.Millisecond,
		ClearSettle:         20 * time.Millisecond,
		PollInterval:        5 * time.Millisecond,
		CycleIDGen:          func() string { return cycleID },
		IsManagedFn:         func(_, _ string) bool { return true },
		HandoffFilePath: func(_, a string) string {
			return "/tmp/HANDOFF-" + a + ".md"
		},
		ReadHandoff:         readHandoff,
		TruncateHandoffFn:   func(_ string) error { return nil },
		InjectFn:            spy.inject,
		ReadGaugeFn:         readGaugeFn,
		CrispIdleFn:         func(_, _ string) bool { return true },
		HoldingDispatchFn:   func(_, _ string) bool { return false },
		WriteJournalFn:      jc.write,
		SetTmuxEnvFn:        func(_ context.Context, _, _, _ string) error { return nil },
		SetManagedSessionFn: func(_, _, _ string) error { return nil },
	}

	cycler := keeper.NewCycler(cfg, em)

	// Step 1: first MaybeRun at high pct → cycle fires; lastFiredSID = sid.
	cf := &keeper.CtxFile{Pct: 95.0, SessionID: sid}
	if err := cycler.MaybeRun(context.Background(), cf); err != nil {
		t.Fatalf("MaybeRun (step 1): %v", err)
	}
	want1 := 1
	if got := len(em.EventsOfType(core.EventTypeSessionKeeperCycleComplete)); got != want1 {
		t.Errorf("after step 1: cycle_complete events = %d; want %d", got, want1)
	}

	// Step 2: same session, same high pct → anti-loop suppresses.
	if err := cycler.MaybeRun(context.Background(), cf); err != nil {
		t.Fatalf("MaybeRun (step 2): %v", err)
	}
	if got := len(em.EventsOfType(core.EventTypeSessionKeeperCycleComplete)); got != want1 {
		t.Errorf("after step 2: cycle_complete events = %d; want %d (anti-loop suppressed)", got, want1)
	}

	// Step 3: same session, but pct drops below WarnPct — escape hatch fires.
	// At low pct the act gate rejects the cycle, but lastFiredSID is now reset.
	gaugePct = 70.0
	lowCF := &keeper.CtxFile{Pct: 70.0, SessionID: sid}
	if err := cycler.MaybeRun(context.Background(), lowCF); err != nil {
		t.Fatalf("MaybeRun (step 3): %v", err)
	}
	// Still no new cycle (below ActPct).
	if got := len(em.EventsOfType(core.EventTypeSessionKeeperCycleComplete)); got != want1 {
		t.Errorf("after step 3: cycle_complete events = %d; want %d (below ActPct)", got, want1)
	}

	// Step 4: pct climbs back above ActPct on the same session — should fire again
	// because the escape hatch reset lastFiredSID in step 3.
	gaugePct = 95.0
	cf2 := &keeper.CtxFile{Pct: 95.0, SessionID: sid}
	if err := cycler.MaybeRun(context.Background(), cf2); err != nil {
		t.Fatalf("MaybeRun (step 4): %v", err)
	}
	want4 := 2
	if got := len(em.EventsOfType(core.EventTypeSessionKeeperCycleComplete)); got != want4 {
		t.Errorf("after step 4: cycle_complete events = %d; want %d (escape hatch should allow re-fire)", got, want4)
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

// TestCycler_ForcedClear_BypassesCrispIdle verifies that a perpetually-busy
// agent (CrispIdle always false) still gets cleared when context is at or above
// the forced-clear threshold (ForceActPct). Refs: hk-0uu.
func TestCycler_ForcedClear_BypassesCrispIdle(t *testing.T) {
	t.Parallel()

	const (
		agent   = "busy-agent"
		cycleID = "cyc-force-001"
		prevSID = "sess-busy-before"
		newSID  = "sess-busy-after"
	)

	em := &keeper.RecordingEmitter{}
	spy := &cycleSpyInjector{}
	jc := &journalCapture{}

	nonce := "<!-- KEEPER:" + cycleID + " -->"
	readHandoff := handoffReturnsNonceAfter(1, nonce)
	readGaugeFn := gaugeReturnsNewSIDAfter(1, "", agent, prevSID, newSID)

	cfg := keeper.CyclerConfig{
		IdleMarkerModTimeFn: idleMarkerFreshNow, // Stop hook wired: model-done on first AwaitModelDone poll (T8)
		AgentName:           agent,
		ProjectDir:          t.TempDir(),
		TmuxTarget:          "fake-pane",
		ActPct:              90.0,
		WarnPct:             80.0,
		ForceActPct:         95.0, // hard threshold
		HandoffTimeout:      500 * time.Millisecond,
		ClearSettle:         200 * time.Millisecond,
		PollInterval:        10 * time.Millisecond,
		CycleIDGen:          func() string { return cycleID },
		IsManagedFn:         func(_, _ string) bool { return true },
		HandoffFilePath: func(_, a string) string {
			return "/tmp/HANDOFF-" + a + ".md"
		},
		ReadHandoff:         readHandoff,
		TruncateHandoffFn:   func(_ string) error { return nil },
		InjectFn:            spy.inject,
		ReadGaugeFn:         readGaugeFn,
		CrispIdleFn:         func(_, _ string) bool { return false }, // perpetually busy
		HoldingDispatchFn:   func(_, _ string) bool { return false },
		WriteJournalFn:      jc.write,
		SetTmuxEnvFn:        func(_ context.Context, _, _, _ string) error { return nil },
		SetManagedSessionFn: func(_, _, _ string) error { return nil },
	}
	cycler := keeper.NewCycler(cfg, em)

	// Context at exactly the force threshold — cycle MUST fire despite CrispIdle=false.
	cf := &keeper.CtxFile{Pct: 95.0, SessionID: prevSID}
	if err := cycler.MaybeRun(context.Background(), cf); err != nil {
		t.Fatalf("MaybeRun: %v", err)
	}

	phases := jc.snapshot()
	wantPhases := []string{"opened", "handoff_injected", "confirmed", "cleared", "resumed", "complete"}
	if len(phases) != len(wantPhases) {
		t.Fatalf("journal phases = %v; want %v", phases, wantPhases)
	}
	for i, p := range phases {
		if p != wantPhases[i] {
			t.Errorf("phase[%d] = %q; want %q", i, p, wantPhases[i])
		}
	}

	// Inject sequence: [0] /session-handoff, [1] /clear, [2] agent brief.
	texts := spy.texts()
	if len(texts) < 3 {
		t.Fatalf("inject calls = %d; want >=3 (/session-handoff + /clear + agent brief)", len(texts))
	}
	if !containsSubstr(texts[0], "/session-handoff") {
		t.Errorf("inject[0] = %q; want /session-handoff", texts[0])
	}
	if !containsSubstr(texts[1], "/clear") {
		t.Errorf("inject[1] = %q; want /clear", texts[1])
	}
	if !containsSubstr(texts[2], "agent brief") {
		t.Errorf("inject[2] = %q; want agent brief", texts[2])
	}
	if !containsSubstr(texts[2], "keeper-restart") {
		t.Errorf("inject[2] = %q; want --wake keeper-restart", texts[2])
	}

	completes := em.EventsOfType(core.EventTypeSessionKeeperCycleComplete)
	if len(completes) != 1 {
		t.Fatalf("cycle_complete events = %d; want 1", len(completes))
	}
}

// TestCycler_ForcedClear_RetryAfterInterval verifies the hk-qoz catch-22 fix:
// after a forced-clear abort (handoff_timeout above ForceActPct), the cycle
// must NOT fire again immediately (DEFECT-4 guard), but MUST fire again once
// ForceRetryInterval has elapsed. Without this fix, a session stuck above the
// force threshold after one abort is permanently blocked by Gate 6.
func TestCycler_ForcedClear_RetryAfterInterval(t *testing.T) {
	t.Parallel()

	const (
		agent   = "stuck-agent"
		cycleID = "cyc-force-retry"
		sid     = "sess-stuck"
	)

	em := &keeper.RecordingEmitter{}
	spy := &cycleSpyInjector{}
	jc := &journalCapture{}

	noopGauge := func(_, _ string) (*keeper.CtxFile, time.Time, error) {
		return &keeper.CtxFile{Pct: 97.0, SessionID: sid}, time.Now(), nil
	}

	// hk-h0twl deflake: virtual-time clock. ForceRetryInterval is large in
	// VIRTUAL time so it dwarfs the deterministic virtual time consumed by call
	// 1's abort drive loop (a few polls of Now()-stepping) — call 2's "interval
	// not elapsed" then holds with an enormous margin, and the previously-flaky
	// dependence on real wall-clock between MaybeRun calls is gone. Call 3 crosses
	// the interval via clock.Advance (no real sleep). The auto-stepping Now() also
	// makes the HandoffTimeout abort trip after a deterministic number of polls.
	const forceRetryInterval = 5 * time.Second
	clock := newSteppingAdvanceClock(time.Unix(1_700_000_000, 0), 5*time.Millisecond)

	cfg := keeper.CyclerConfig{
		Clock:               clock,
		IdleMarkerModTimeFn: idleMarkerFreshNow, // Stop hook wired: model-done on first AwaitModelDone poll (T8)
		AgentName:           agent,
		ProjectDir:          t.TempDir(),
		TmuxTarget:          "fake-pane",
		ActPct:              90.0,
		WarnPct:             80.0,
		ForceActPct:         95.0,
		ForceRetryInterval:  forceRetryInterval,
		HandoffTimeout:      30 * time.Millisecond, // short → quick abort (deterministic poll count)
		ClearSettle:         10 * time.Millisecond,
		PollInterval:        5 * time.Millisecond,
		CycleIDGen:          func() string { return cycleID },
		IsManagedFn:         func(_, _ string) bool { return true },
		HandoffFilePath: func(_, a string) string {
			return "/tmp/HANDOFF-" + a + ".md"
		},
		ReadHandoff:         handoffNeverReturnsNonce, // always abort
		TruncateHandoffFn:   func(_ string) error { return nil },
		InjectFn:            spy.inject,
		ReadGaugeFn:         noopGauge,
		CrispIdleFn:         func(_, _ string) bool { return false }, // perpetually busy
		HoldingDispatchFn:   func(_, _ string) bool { return false },
		WriteJournalFn:      jc.write,
		SetTmuxEnvFn:        func(_ context.Context, _, _, _ string) error { return nil },
		SetManagedSessionFn: func(_, _, _ string) error { return nil },
	}
	cycler := keeper.NewCycler(cfg, em)

	// Call 1: fires (above force), aborts (nonce timeout).
	cf := &keeper.CtxFile{Pct: 97.0, SessionID: sid}
	if err := cycler.MaybeRun(context.Background(), cf); err != nil {
		t.Fatalf("MaybeRun #1: %v", err)
	}
	abortedAfter1 := len(em.EventsOfType(core.EventTypeSessionKeeperCycleAborted))
	if abortedAfter1 != 1 {
		t.Fatalf("want 1 cycle_aborted after first call; got %d", abortedAfter1)
	}

	// Call 2 (immediately): Gate 6 must suppress — ForceRetryInterval not elapsed.
	if err := cycler.MaybeRun(context.Background(), cf); err != nil {
		t.Fatalf("MaybeRun #2 (immediate): %v", err)
	}
	abortedAfter2 := len(em.EventsOfType(core.EventTypeSessionKeeperCycleAborted))
	if abortedAfter2 != abortedAfter1 {
		t.Errorf("want no new cycle_aborted immediately after abort; got %d total", abortedAfter2)
	}

	// Cross ForceRetryInterval deterministically in virtual time (no real sleep).
	clock.Advance(forceRetryInterval + 10*time.Millisecond)

	// Call 3 (after interval): must retry the forced-clear, abort again.
	if err := cycler.MaybeRun(context.Background(), cf); err != nil {
		t.Fatalf("MaybeRun #3 (after interval): %v", err)
	}
	abortedAfter3 := len(em.EventsOfType(core.EventTypeSessionKeeperCycleAborted))
	if abortedAfter3 != 2 {
		t.Errorf("want 2 cycle_aborted after retry (interval elapsed); got %d", abortedAfter3)
	}
}

// TestCycler_ForcedClear_EscapeInjected verifies that when SendEscapeFn is set,
// it is called before the /session-handoff inject. The Escape must precede the
// handoff to preempt any in-progress input on a busy pane. Refs: hk-qoz.
func TestCycler_ForcedClear_EscapeInjected(t *testing.T) {
	t.Parallel()

	const (
		agent   = "escape-agent"
		cycleID = "cyc-escape-001"
		prevSID = "sess-busy-esc"
		newSID  = "sess-clear-esc"
	)

	em := &keeper.RecordingEmitter{}
	jc := &journalCapture{}

	var mu sync.Mutex
	var callOrder []string // records "escape" or "inject" in order

	escapeFn := func(_ context.Context, _ string) error {
		mu.Lock()
		defer mu.Unlock()
		callOrder = append(callOrder, "escape")
		return nil
	}
	injectFn := func(_ context.Context, _, text string) error {
		mu.Lock()
		defer mu.Unlock()
		callOrder = append(callOrder, "inject:"+text[:min(len(text), 20)])
		return nil
	}

	nonce := "<!-- KEEPER:" + cycleID + " -->"
	readHandoff := handoffReturnsNonceAfter(1, nonce)
	readGaugeFn := gaugeReturnsNewSIDAfter(1, "", agent, prevSID, newSID)

	cfg := keeper.CyclerConfig{
		IdleMarkerModTimeFn: idleMarkerFreshNow, // Stop hook wired: model-done on first AwaitModelDone poll (T8)
		AgentName:           agent,
		ProjectDir:          t.TempDir(),
		TmuxTarget:          "fake-pane",
		ActPct:              90.0,
		WarnPct:             80.0,
		ForceActPct:         95.0,
		HandoffTimeout:      500 * time.Millisecond,
		ClearSettle:         100 * time.Millisecond,
		PollInterval:        10 * time.Millisecond,
		CycleIDGen:          func() string { return cycleID },
		IsManagedFn:         func(_, _ string) bool { return true },
		HandoffFilePath: func(_, a string) string {
			return "/tmp/HANDOFF-" + a + ".md"
		},
		ReadHandoff:         readHandoff,
		TruncateHandoffFn:   func(_ string) error { return nil },
		InjectFn:            injectFn,
		ReadGaugeFn:         readGaugeFn,
		CrispIdleFn:         func(_, _ string) bool { return false }, // busy pane
		HoldingDispatchFn:   func(_, _ string) bool { return false },
		WriteJournalFn:      jc.write,
		SetTmuxEnvFn:        func(_ context.Context, _, _, _ string) error { return nil },
		SetManagedSessionFn: func(_, _, _ string) error { return nil },
		SendEscapeFn:        escapeFn,
	}
	cycler := keeper.NewCycler(cfg, em)

	// Context at force threshold with CrispIdle=false → forced-clear fires.
	cf := &keeper.CtxFile{Pct: 97.0, SessionID: prevSID}
	if err := cycler.MaybeRun(context.Background(), cf); err != nil {
		t.Fatalf("MaybeRun: %v", err)
	}

	mu.Lock()
	order := make([]string, len(callOrder))
	copy(order, callOrder)
	mu.Unlock()

	// There must be at least one "escape" entry.
	foundEscape := false
	firstHandoffIdx := -1
	for i, e := range order {
		if e == "escape" && !foundEscape {
			foundEscape = true
		}
		if containsSubstr(e, "inject:/session-handoff") && firstHandoffIdx == -1 {
			firstHandoffIdx = i
		}
	}
	if !foundEscape {
		t.Errorf("want SendEscapeFn called; callOrder = %v", order)
	}
	// Escape must appear before the /session-handoff inject.
	firstEscapeIdx := -1
	for i, e := range order {
		if e == "escape" {
			firstEscapeIdx = i
			break
		}
	}
	if firstEscapeIdx != -1 && firstHandoffIdx != -1 && firstEscapeIdx >= firstHandoffIdx {
		t.Errorf("Escape (idx=%d) must precede /session-handoff inject (idx=%d); order=%v",
			firstEscapeIdx, firstHandoffIdx, order)
	}
}

// min is a local helper for the escape test above (avoids importing math).
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// TestCycler_ForcedClear_EscalatesAfterNTimeouts verifies that after
// MaxHandoffTimeouts consecutive handoff timeouts above the force threshold,
// ForceRestartFn is called to hard-restart the agent. Refs: hk-qoz.
func TestCycler_ForcedClear_EscalatesAfterNTimeouts(t *testing.T) {
	t.Parallel()

	const (
		agent   = "escalate-agent"
		cycleID = "cyc-escalate-001"
		sid     = "sess-escalate"
	)

	em := &keeper.RecordingEmitter{}
	spy := &cycleSpyInjector{}
	jc := &journalCapture{}

	var restartCalled int
	var mu sync.Mutex
	forceRestartFn := func(_ context.Context, _ string) error {
		mu.Lock()
		defer mu.Unlock()
		restartCalled++
		return nil
	}

	const maxTimeouts = 3
	// ForceRetryInterval very short so each retry fires immediately in test.
	const forceRetryInterval = 20 * time.Millisecond

	cfg := keeper.CyclerConfig{
		IdleMarkerModTimeFn: idleMarkerFreshNow, // Stop hook wired: model-done on first AwaitModelDone poll (T8)
		AgentName:           agent,
		ProjectDir:          t.TempDir(),
		TmuxTarget:          "fake-pane",
		ActPct:              90.0,
		WarnPct:             80.0,
		ForceActPct:         95.0,
		MaxHandoffTimeouts:  maxTimeouts,
		ForceRetryInterval:  forceRetryInterval,
		HandoffTimeout:      10 * time.Millisecond, // short for test speed
		ClearSettle:         5 * time.Millisecond,
		PollInterval:        2 * time.Millisecond,
		CycleIDGen:          func() string { return cycleID },
		IsManagedFn:         func(_, _ string) bool { return true },
		HandoffFilePath: func(_, a string) string {
			return "/tmp/HANDOFF-" + a + ".md"
		},
		ReadHandoff:       handoffNeverReturnsNonce,
		TruncateHandoffFn: func(_ string) error { return nil },
		InjectFn:          spy.inject,
		ReadGaugeFn: func(_, _ string) (*keeper.CtxFile, time.Time, error) {
			return &keeper.CtxFile{Pct: 97.0, SessionID: sid}, time.Now(), nil
		},
		CrispIdleFn:         func(_, _ string) bool { return false },
		HoldingDispatchFn:   func(_, _ string) bool { return false },
		WriteJournalFn:      jc.write,
		SetTmuxEnvFn:        func(_ context.Context, _, _, _ string) error { return nil },
		SetManagedSessionFn: func(_, _, _ string) error { return nil },
		ForceRestartFn:      forceRestartFn,
	}
	cycler := keeper.NewCycler(cfg, em)

	cf := &keeper.CtxFile{Pct: 97.0, SessionID: sid}

	// Fire maxTimeouts cycles; each aborts (nonce never arrives).
	// Between each we sleep forceRetryInterval so Gate 6 allows re-fire.
	for i := 0; i < maxTimeouts; i++ {
		if err := cycler.MaybeRun(context.Background(), cf); err != nil {
			t.Fatalf("MaybeRun #%d: %v", i+1, err)
		}
		if i < maxTimeouts-1 {
			time.Sleep(forceRetryInterval + 5*time.Millisecond)
		}
	}

	mu.Lock()
	rc := restartCalled
	mu.Unlock()

	if rc != 1 {
		t.Errorf("ForceRestartFn called %d times; want 1 (after %d timeouts)", rc, maxTimeouts)
	}

	aborted := len(em.EventsOfType(core.EventTypeSessionKeeperCycleAborted))
	if aborted != maxTimeouts {
		t.Errorf("cycle_aborted events = %d; want %d", aborted, maxTimeouts)
	}
}

// TestCycler_BootGrace_SuppressesAndThenAllows verifies the hk-4f8 boot-grace
// fix (Defect A — bad trigger timing): when BootGracePeriod is set, MaybeRun
// suppresses cycles for the first BootGracePeriod after a session_id CHANGE.
// Before the grace expires the cycle is blocked; after it expires the cycle fires.
// The grace does NOT apply on the very first session the Cycler ever observes
// (no prior session_id evicted), so a keeper that boots while the agent is
// already running monitors it without delay.
// Uses pct=92 (above ActPct=90, below ForceActPct=95) with CrispIdle=true so the
// force-path exemption (hk-ibb fix 1) does not bypass the grace — the boot-grace
// suppression itself is what is under test here.
func TestCycler_BootGrace_SuppressesAndThenAllows(t *testing.T) {
	t.Parallel()

	const (
		agent   = "boot-grace-agent"
		cycleID = "cyc-boot-001"
		prevSID = "sess-prev" // session seen before the /session-resume (at low pct — no cycle)
		bootSID = "sess-boot" // new session created by /session-resume (at high pct)
	)

	em := &keeper.RecordingEmitter{}
	spy := &cycleSpyInjector{}
	jc := &journalCapture{}

	nonce := "<!-- KEEPER:" + cycleID + " -->"
	readHandoff := handoffReturnsNonceAfter(0, nonce)
	noopGauge := func(_, _ string) (*keeper.CtxFile, time.Time, error) {
		// pct=92: above ActPct (90) but below ForceActPct (95) → grace suppresses;
		// after grace the cycle fires via the normal CrispIdle path.
		return &keeper.CtxFile{Pct: 92.0, SessionID: bootSID}, time.Now(), nil
	}

	const bootGrace = 120 * time.Millisecond

	cfg := keeper.CyclerConfig{
		IdleMarkerModTimeFn: idleMarkerFreshNow, // Stop hook wired: model-done on first AwaitModelDone poll (T8)
		AgentName:           agent,
		ProjectDir:          t.TempDir(),
		TmuxTarget:          "fake-pane",
		ActPct:              90.0,
		WarnPct:             80.0,
		ForceActPct:         95.0,
		HandoffTimeout:      200 * time.Millisecond,
		ClearSettle:         50 * time.Millisecond,
		PollInterval:        10 * time.Millisecond,
		BootGracePeriod:     bootGrace,
		CycleIDGen:          func() string { return cycleID },
		IsManagedFn:         func(_, _ string) bool { return true },
		HandoffFilePath:     func(_, a string) string { return "/tmp/HANDOFF-" + a + ".md" },
		ReadHandoff:         readHandoff,
		TruncateHandoffFn:   func(_ string) error { return nil },
		InjectFn:            spy.inject,
		ReadGaugeFn:         noopGauge,
		CrispIdleFn:         func(_, _ string) bool { return true }, // idle — fires without force path
		HoldingDispatchFn:   func(_, _ string) bool { return false },
		WriteJournalFn:      jc.write,
		SetTmuxEnvFn:        func(_ context.Context, _, _, _ string) error { return nil },
		SetManagedSessionFn: func(_, _, _ string) error { return nil },
	}
	cycler := keeper.NewCycler(cfg, em)

	// ── Part 1: first-boot does NOT trigger grace (no prior session evicted) ──

	// Observe prevSID at low pct (below ActPct): Gate 3 blocks the cycle but
	// establishes prevSID as currentSessionID WITHOUT starting the grace timer
	// (grace only starts on eviction of a non-empty prior session_id).
	cfLow := &keeper.CtxFile{Pct: 70.0, SessionID: prevSID}
	if err := cycler.MaybeRun(context.Background(), cfLow); err != nil {
		t.Fatalf("MaybeRun (prevSID low pct): %v", err)
	}
	// No cycle at low pct.
	if n := len(em.EventsOfType(core.EventTypeSessionKeeperHandoffStarted)); n != 0 {
		t.Errorf("prevSID low pct: want 0 handoff_started; got %d", n)
	}

	// ── Part 2: session_id changes → grace starts; cycle suppressed during grace ──

	// Switch to bootSID at 92% (above ActPct, below ForceActPct). Because prevSID
	// was the prior session, the boot-grace timer starts NOW. Cycle must be
	// suppressed by grace (force-path exemption does not apply below ForceActPct).
	cfBoot := &keeper.CtxFile{Pct: 92.0, SessionID: bootSID}
	if err := cycler.MaybeRun(context.Background(), cfBoot); err != nil {
		t.Fatalf("MaybeRun (during boot grace): %v", err)
	}
	duringGrace := len(em.EventsOfType(core.EventTypeSessionKeeperHandoffStarted))
	if duringGrace != 0 {
		t.Errorf("during boot grace: want 0 handoff_started (suppressed); got %d", duringGrace)
	}

	// Wait for the boot grace to expire.
	time.Sleep(bootGrace + 20*time.Millisecond)

	// ── Part 3: after grace expires, cycle fires normally ──
	if err := cycler.MaybeRun(context.Background(), cfBoot); err != nil {
		t.Fatalf("MaybeRun (after boot grace): %v", err)
	}
	afterGrace := len(em.EventsOfType(core.EventTypeSessionKeeperHandoffStarted))
	if afterGrace != 1 {
		t.Errorf("after boot grace: want 1 handoff_started (grace over, cycle fires); got %d", afterGrace)
	}
}

// TestCycler_YoungSessionGuard_NewBand_AbsTokens pins the YOUNG-SESSION guard
// against the aggressive earlier band (hk-8hr1: act=215K / force=240K on the
// absolute-token path). A session that just changed session_id (post-/session-
// resume) must NOT be restarted while it is younger than BootGracePeriod even
// though it is already ABOVE the new act threshold — otherwise the lowered band
// would clear a freshly-resumed session that has barely begun work. The force-act
// ceiling (240K) is the deliberate exception: a session that full is at genuine
// pane-overflow risk regardless of age, so it bypasses the guard.
func TestCycler_YoungSessionGuard_NewBand_AbsTokens(t *testing.T) {
	t.Parallel()

	const (
		agent   = "young-guard-agent"
		cycleID = "cyc-young-001"
		prevSID = "sess-prev-young"
		bootSID = "sess-boot-young"
		window  = int64(1_000_000)
	)

	em := &keeper.RecordingEmitter{}
	spy := &cycleSpyInjector{}
	jc := &journalCapture{}

	nonce := "<!-- KEEPER:" + cycleID + " -->"
	readHandoff := handoffReturnsNonceAfter(0, nonce)

	// Long grace so the young session stays within it for the whole test (no sleep).
	const bootGrace = 30 * time.Second

	cfg := keeper.CyclerConfig{
		IdleMarkerModTimeFn: idleMarkerFreshNow, // Stop hook wired: model-done on first AwaitModelDone poll (T8)
		AgentName:           agent,
		ProjectDir:          t.TempDir(),
		TmuxTarget:          "fake-pane",
		HandoffTimeout:      200 * time.Millisecond,
		ClearSettle:         50 * time.Millisecond,
		PollInterval:        10 * time.Millisecond,
		BootGracePeriod:     bootGrace,
		CycleIDGen:          func() string { return cycleID },
		IsManagedFn:         func(_, _ string) bool { return true },
		HandoffFilePath:     func(_, a string) string { return "/tmp/HANDOFF-" + a + ".md" },
		ReadHandoff:         readHandoff,
		TruncateHandoffFn:   func(_ string) error { return nil },
		InjectFn:            spy.inject,
		// Use the default abs-token band (act=215K / force=240K); do not override.
		CrispIdleFn:         func(_, _ string) bool { return true },
		HoldingDispatchFn:   func(_, _ string) bool { return false },
		WriteJournalFn:      jc.write,
		SetTmuxEnvFn:        func(_ context.Context, _, _, _ string) error { return nil },
		SetManagedSessionFn: func(_, _, _ string) error { return nil },
	}
	cycler := keeper.NewCycler(cfg, em)

	// Establish prevSID below the act threshold (50K < 215K) — no grace armed yet.
	cfPrev := &keeper.CtxFile{Pct: 5.0, Tokens: 50_000, WindowSize: window, SessionID: prevSID}
	if err := cycler.MaybeRun(context.Background(), cfPrev); err != nil {
		t.Fatalf("MaybeRun (prevSID): %v", err)
	}

	// Switch to bootSID at 230K — ABOVE the new act (215K) but BELOW force (240K).
	// The session_id change arms the boot grace; the young-session guard must
	// suppress the restart even though context is past the aggressive act gate.
	cfYoung := &keeper.CtxFile{Pct: 23.0, Tokens: 230_000, WindowSize: window, SessionID: bootSID}
	if err := cycler.MaybeRun(context.Background(), cfYoung); err != nil {
		t.Fatalf("MaybeRun (young, above act below force): %v", err)
	}
	if n := len(em.EventsOfType(core.EventTypeSessionKeeperHandoffStarted)); n != 0 {
		t.Errorf("young session above act(215K) below force(240K): want 0 handoff_started (guard suppresses); got %d", n)
	}

	// Same young session now crosses the force-act ceiling (245K >= 240K). The
	// force exemption bypasses the young-session guard — pane-overflow risk wins.
	cfForce := &keeper.CtxFile{Pct: 24.5, Tokens: 245_000, WindowSize: window, SessionID: bootSID}
	if err := cycler.MaybeRun(context.Background(), cfForce); err != nil {
		t.Fatalf("MaybeRun (young, above force): %v", err)
	}
	if n := len(em.EventsOfType(core.EventTypeSessionKeeperHandoffStarted)); n != 1 {
		t.Errorf("young session above force(240K): want 1 handoff_started (force ceiling bypasses guard); got %d", n)
	}
}

// TestCycler_CleanHandoffGuard_DispatchingSuppressesAboveForce pins the
// CLEAN-HANDOFF guard against the aggressive band (hk-8hr1). When the agent has
// in-flight queue work (the .dispatching marker is present), the keeper must NOT
// restart it — even ABOVE the force-act ceiling (240K), where the CrispIdle gate
// is otherwise bypassed. This proves Gate 5 (HoldingDispatch) has no force-path
// exemption: the lowered band can never clobber a mid-commit / mid-dispatch
// session. Uses the REAL HoldingDispatch gate + SetDispatching/ClearDispatching
// against an on-disk marker, not a stub.
func TestCycler_CleanHandoffGuard_DispatchingSuppressesAboveForce(t *testing.T) {
	t.Parallel()

	const (
		agent   = "clean-handoff-agent"
		cycleID = "cyc-clean-001"
		sid     = "sess-clean"
		window  = int64(1_000_000)
	)

	projectDir := t.TempDir()
	em := &keeper.RecordingEmitter{}
	spy := &cycleSpyInjector{}
	jc := &journalCapture{}

	nonce := "<!-- KEEPER:" + cycleID + " -->"
	readHandoff := handoffReturnsNonceAfter(0, nonce)

	cfg := keeper.CyclerConfig{
		IdleMarkerModTimeFn: idleMarkerFreshNow, // Stop hook wired: model-done on first AwaitModelDone poll (T8)
		AgentName:           agent,
		ProjectDir:          projectDir,
		TmuxTarget:          "fake-pane",
		HandoffTimeout:      200 * time.Millisecond,
		ClearSettle:         50 * time.Millisecond,
		PollInterval:        10 * time.Millisecond,
		CycleIDGen:          func() string { return cycleID },
		IsManagedFn:         func(_, _ string) bool { return true },
		HandoffFilePath:     func(_, a string) string { return "/tmp/HANDOFF-" + a + ".md" },
		ReadHandoff:         readHandoff,
		TruncateHandoffFn:   func(_ string) error { return nil },
		InjectFn:            spy.inject,
		// CrispIdle false (busy) — above force the cycle would normally bypass it;
		// the clean-handoff guard must still hold.
		CrispIdleFn: func(_, _ string) bool { return false },
		// HoldingDispatchFn intentionally LEFT NIL so applyDefaults wires the real
		// HoldingDispatch (reads the on-disk .dispatching marker).
		WriteJournalFn:      jc.write,
		SetTmuxEnvFn:        func(_ context.Context, _, _, _ string) error { return nil },
		SetManagedSessionFn: func(_, _, _ string) error { return nil },
	}
	cycler := keeper.NewCycler(cfg, em)

	// Mark in-flight dispatch, then drive context ABOVE the force ceiling (245K).
	if err := keeper.SetDispatching(projectDir, agent); err != nil {
		t.Fatalf("SetDispatching: %v", err)
	}
	cfForce := &keeper.CtxFile{Pct: 24.5, Tokens: 245_000, WindowSize: window, SessionID: sid}
	if err := cycler.MaybeRun(context.Background(), cfForce); err != nil {
		t.Fatalf("MaybeRun (dispatching, above force): %v", err)
	}
	if n := len(em.EventsOfType(core.EventTypeSessionKeeperHandoffStarted)); n != 0 {
		t.Errorf("dispatching marker present above force(240K): want 0 handoff_started (clean-handoff guard holds); got %d", n)
	}

	// Clear the marker: with no in-flight work, the force-path cycle now fires.
	if err := keeper.ClearDispatching(projectDir, agent); err != nil {
		t.Fatalf("ClearDispatching: %v", err)
	}
	if err := cycler.MaybeRun(context.Background(), cfForce); err != nil {
		t.Fatalf("MaybeRun (marker cleared, above force): %v", err)
	}
	if n := len(em.EventsOfType(core.EventTypeSessionKeeperHandoffStarted)); n != 1 {
		t.Errorf("marker cleared above force(240K): want 1 handoff_started (guard lifted, cycle fires); got %d", n)
	}
}

// TestCycler_AbortClearsManaged verifies the hk-4f8 no-re-arm fix (Defect B)
// as refined by hk-ibb fix 3: after a handoff_timeout abort that follows a REAL
// session_id change, SetManagedSessionFn must be called with an empty string.
// This clears the .managed binding so the .sid channel can rebind a new
// session_id (post-/session-resume). The clear is gated on currentSessionIDSince
// being non-zero — i.e., a session change was previously observed. The test
// establishes a prior session (prevSID) at low pct before triggering the abort
// on the post-resume session (abortSID), ensuring currentSessionIDSince is set.
func TestCycler_AbortClearsManaged(t *testing.T) {
	t.Parallel()

	const (
		agent    = "abort-managed-agent"
		cycleID  = "cyc-abort-managed"
		prevSID  = "sess-prev-managed"  // prior session — establishes currentSessionIDSince
		abortSID = "sess-abort-managed" // post-resume session — abort fires on this one
	)

	em := &keeper.RecordingEmitter{}
	spy := &cycleSpyInjector{}
	jc := &journalCapture{}

	var (
		managedMu        sync.Mutex
		managedCallCount int
		managedLastValue string
	)
	setManagedFn := func(_, _, sessionID string) error {
		managedMu.Lock()
		defer managedMu.Unlock()
		managedCallCount++
		managedLastValue = sessionID
		return nil
	}

	cfg := keeper.CyclerConfig{
		IdleMarkerModTimeFn: idleMarkerFreshNow, // Stop hook wired: model-done on first AwaitModelDone poll (T8)
		AgentName:           agent,
		ProjectDir:          t.TempDir(),
		TmuxTarget:          "fake-pane",
		ActPct:              90.0,
		WarnPct:             80.0,
		ForceActPct:         95.0,
		HandoffTimeout:      40 * time.Millisecond, // short → quick abort
		ClearSettle:         10 * time.Millisecond,
		PollInterval:        5 * time.Millisecond,
		CycleIDGen:          func() string { return cycleID },
		IsManagedFn:         func(_, _ string) bool { return true },
		HandoffFilePath:     func(_, a string) string { return "/tmp/HANDOFF-" + a + ".md" },
		ReadHandoff:         handoffNeverReturnsNonce,
		TruncateHandoffFn:   func(_ string) error { return nil },
		InjectFn:            spy.inject,
		ReadGaugeFn: func(_, _ string) (*keeper.CtxFile, time.Time, error) {
			return &keeper.CtxFile{Pct: 95.0, SessionID: abortSID}, time.Now(), nil
		},
		CrispIdleFn:         func(_, _ string) bool { return true },
		HoldingDispatchFn:   func(_, _ string) bool { return false },
		WriteJournalFn:      jc.write,
		SetTmuxEnvFn:        func(_ context.Context, _, _, _ string) error { return nil },
		SetManagedSessionFn: setManagedFn,
	}
	cycler := keeper.NewCycler(cfg, em)

	// Step 1: observe prevSID at low pct — establishes currentSessionID=prevSID
	// without starting the grace timer (first SID, currentSessionIDSince stays Zero).
	cfPrev := &keeper.CtxFile{Pct: 70.0, SessionID: prevSID}
	if err := cycler.MaybeRun(context.Background(), cfPrev); err != nil {
		t.Fatalf("MaybeRun (prevSID low pct): %v", err)
	}
	if n := len(em.EventsOfType(core.EventTypeSessionKeeperHandoffStarted)); n != 0 {
		t.Fatalf("prevSID low pct: want 0 handoff_started; got %d", n)
	}

	// Step 2: switch to abortSID at 95% — session change arms currentSessionIDSince.
	// Force-path exemption (hk-ibb fix 1) bypasses boot-grace at 95%, so the cycle
	// fires immediately even though grace was just armed.
	cf := &keeper.CtxFile{Pct: 95.0, SessionID: abortSID}
	if err := cycler.MaybeRun(context.Background(), cf); err != nil {
		t.Fatalf("MaybeRun: %v", err)
	}

	// Verify the cycle aborted (nonce never arrived).
	abortedEvts := em.EventsOfType(core.EventTypeSessionKeeperCycleAborted)
	if len(abortedEvts) != 1 {
		t.Fatalf("want 1 cycle_aborted; got %d", len(abortedEvts))
	}
	var abortPayload core.SessionKeeperCycleAbortedPayload
	if err := json.Unmarshal(abortedEvts[0].Payload, &abortPayload); err != nil {
		t.Fatalf("unmarshal cycle_aborted: %v", err)
	}
	if abortPayload.Reason != "handoff_timeout" {
		t.Errorf("cycle_aborted.reason = %q; want \"handoff_timeout\"", abortPayload.Reason)
	}

	// SetManagedSessionFn must have been called once with empty string: a real
	// session change was observed (currentSessionIDSince != zero) so managed is
	// cleared to allow the .sid channel to rebind the post-resume session.
	managedMu.Lock()
	count := managedCallCount
	last := managedLastValue
	managedMu.Unlock()

	if count != 1 {
		t.Errorf("SetManagedSessionFn called %d times; want 1 (abort with prior session change must clear managed)", count)
	}
	if last != "" {
		t.Errorf("SetManagedSessionFn last value = %q; want \"\" (abort must clear binding)", last)
	}
}

// TestCycler_ForcedClear_BelowThreshold_StillBlocked verifies that when context
// is above ActPct but below ForceActPct, a non-idle agent (CrispIdle=false) is
// still blocked (normal behavior unchanged). Refs: hk-0uu.
func TestCycler_ForcedClear_BelowThreshold_StillBlocked(t *testing.T) {
	t.Parallel()

	const (
		agent   = "busy-agent-below"
		cycleID = "cyc-force-002"
		prevSID = "sess-below"
	)

	em := &keeper.RecordingEmitter{}
	spy := &cycleSpyInjector{}
	jc := &journalCapture{}

	cfg := keeper.CyclerConfig{
		IdleMarkerModTimeFn: idleMarkerFreshNow, // Stop hook wired: model-done on first AwaitModelDone poll (T8)
		AgentName:           agent,
		ProjectDir:          t.TempDir(),
		TmuxTarget:          "fake-pane",
		ActPct:              90.0,
		WarnPct:             80.0,
		ForceActPct:         95.0,
		HandoffTimeout:      100 * time.Millisecond,
		ClearSettle:         50 * time.Millisecond,
		PollInterval:        10 * time.Millisecond,
		CycleIDGen:          func() string { return cycleID },
		IsManagedFn:         func(_, _ string) bool { return true },
		HandoffFilePath: func(_, a string) string {
			return "/tmp/HANDOFF-" + a + ".md"
		},
		ReadHandoff:       func(_ string) (string, error) { return "", nil },
		TruncateHandoffFn: func(_ string) error { return nil },
		InjectFn:          spy.inject,
		ReadGaugeFn: func(_, _ string) (*keeper.CtxFile, time.Time, error) {
			return &keeper.CtxFile{Pct: 92.0, SessionID: prevSID}, time.Now(), nil
		},
		CrispIdleFn:         func(_, _ string) bool { return false }, // perpetually busy
		HoldingDispatchFn:   func(_, _ string) bool { return false },
		WriteJournalFn:      jc.write,
		SetTmuxEnvFn:        func(_ context.Context, _, _, _ string) error { return nil },
		SetManagedSessionFn: func(_, _, _ string) error { return nil },
	}
	cycler := keeper.NewCycler(cfg, em)

	// Context above ActPct (90) but below ForceActPct (95) with CrispIdle=false.
	// Cycle must NOT fire.
	cf := &keeper.CtxFile{Pct: 92.0, SessionID: prevSID}
	if err := cycler.MaybeRun(context.Background(), cf); err != nil {
		t.Fatalf("MaybeRun: %v", err)
	}

	if len(jc.snapshot()) != 0 {
		t.Errorf("journal phases = %v; want none (cycle should be blocked)", jc.snapshot())
	}
	if len(spy.texts()) != 0 {
		t.Errorf("inject calls = %v; want none", spy.texts())
	}
}

// TestCycler_ForceThresholdTracksActPct verifies that when --act-pct is configured
// below the default 90%, the force-clear threshold is derived from the configured
// act-pct (act+5) rather than the hardcoded 95 default. Without the fix, a session
// at 36% would be stuck: above ActPct (35) but below the old ForceActPct (95), so
// CrispIdle=false blocks it permanently. Refs: hk-6el, hk-0uu.
func TestCycler_ForceThresholdTracksActPct(t *testing.T) {
	t.Parallel()

	const (
		agent   = "low-act-agent"
		cycleID = "cyc-low-act"
		prevSID = "sess-low-act-before"
		newSID  = "sess-low-act-after"
	)

	em := &keeper.RecordingEmitter{}
	spy := &cycleSpyInjector{}
	jc := &journalCapture{}

	nonce := "<!-- KEEPER:" + cycleID + " -->"
	readHandoff := handoffReturnsNonceAfter(1, nonce)
	readGaugeFn := gaugeReturnsNewSIDAfter(1, "", agent, prevSID, newSID)

	// ActPct=35, ForceActPct left at zero → must default to 35+5=40.
	// Session at pct=41 (above force threshold) with CrispIdle=false must fire.
	cfg := keeper.CyclerConfig{
		IdleMarkerModTimeFn: idleMarkerFreshNow, // Stop hook wired: model-done on first AwaitModelDone poll (T8)
		AgentName:           agent,
		ProjectDir:          t.TempDir(),
		TmuxTarget:          "fake-pane",
		ActPct:              35.0,
		WarnPct:             25.0,
		// ForceActPct intentionally omitted → must default to ActPct+5 = 40.0
		HandoffTimeout: 500 * time.Millisecond,
		ClearSettle:    200 * time.Millisecond,
		PollInterval:   10 * time.Millisecond,
		CycleIDGen:     func() string { return cycleID },
		IsManagedFn:    func(_, _ string) bool { return true },
		HandoffFilePath: func(_, a string) string {
			return "/tmp/HANDOFF-" + a + ".md"
		},
		ReadHandoff:         readHandoff,
		TruncateHandoffFn:   func(_ string) error { return nil },
		InjectFn:            spy.inject,
		ReadGaugeFn:         readGaugeFn,
		CrispIdleFn:         func(_, _ string) bool { return false }, // perpetually busy
		HoldingDispatchFn:   func(_, _ string) bool { return false },
		WriteJournalFn:      jc.write,
		SetTmuxEnvFn:        func(_ context.Context, _, _, _ string) error { return nil },
		SetManagedSessionFn: func(_, _, _ string) error { return nil },
	}
	cycler := keeper.NewCycler(cfg, em)

	// pct=41: above ActPct (35) and above derived ForceActPct (40). CrispIdle=false
	// must be bypassed so the cycle fires — verifies dead zone is eliminated.
	cf := &keeper.CtxFile{Pct: 41.0, SessionID: prevSID}
	if err := cycler.MaybeRun(context.Background(), cf); err != nil {
		t.Fatalf("MaybeRun: %v", err)
	}

	phases := jc.snapshot()
	wantPhases := []string{"opened", "handoff_injected", "confirmed", "cleared", "resumed", "complete"}
	if len(phases) != len(wantPhases) {
		t.Fatalf("journal phases = %v; want %v", phases, wantPhases)
	}
	for i, p := range phases {
		if p != wantPhases[i] {
			t.Errorf("phase[%d] = %q; want %q", i, p, wantPhases[i])
		}
	}

	// Also verify that pct=37 (in the old dead zone 35-40, but now just above ActPct)
	// is still blocked by CrispIdle since it's below the new ForceActPct=40.
	em2 := &keeper.RecordingEmitter{}
	spy2 := &cycleSpyInjector{}
	jc2 := &journalCapture{}
	cfg2 := cfg
	cfg2.ProjectDir = t.TempDir()
	cfg2.InjectFn = spy2.inject
	cfg2.WriteJournalFn = jc2.write
	cfg2.ReadHandoff = func(_ string) (string, error) { return "", nil }
	cfg2.ReadGaugeFn = func(_, _ string) (*keeper.CtxFile, time.Time, error) {
		return &keeper.CtxFile{Pct: 37.0, SessionID: prevSID}, time.Now(), nil
	}
	cycler2 := keeper.NewCycler(cfg2, em2)

	cf2 := &keeper.CtxFile{Pct: 37.0, SessionID: prevSID}
	if err := cycler2.MaybeRun(context.Background(), cf2); err != nil {
		t.Fatalf("MaybeRun (below force): %v", err)
	}
	if len(jc2.snapshot()) != 0 {
		t.Errorf("below force threshold: journal phases = %v; want none", jc2.snapshot())
	}
}

// TestCycler_BootGrace_ForcePathBypasses verifies hk-ibb fix 1: an agent above
// ForceActPct bypasses the boot-grace window entirely and fires immediately, even
// though the grace timer was just armed by a session_id change. This prevents a
// pane-overflow stall where a 95%+ agent can't be cleared during the grace period.
func TestCycler_BootGrace_ForcePathBypasses(t *testing.T) {
	t.Parallel()

	const (
		agent   = "boot-grace-force-agent"
		cycleID = "cyc-grace-force-001"
		prevSID = "sess-prev-force"
		bootSID = "sess-boot-force"
	)

	em := &keeper.RecordingEmitter{}
	spy := &cycleSpyInjector{}
	jc := &journalCapture{}

	nonce := "<!-- KEEPER:" + cycleID + " -->"
	readHandoff := handoffReturnsNonceAfter(0, nonce)

	const bootGrace = 500 * time.Millisecond // long grace — force-path should bypass it

	readGaugeFn := gaugeReturnsNewSIDAfter(1, "", agent, bootSID, bootSID+"_new")

	cfg := keeper.CyclerConfig{
		IdleMarkerModTimeFn: idleMarkerFreshNow, // Stop hook wired: model-done on first AwaitModelDone poll (T8)
		AgentName:           agent,
		ProjectDir:          t.TempDir(),
		TmuxTarget:          "fake-pane",
		ActPct:              90.0,
		WarnPct:             80.0,
		ForceActPct:         95.0,
		HandoffTimeout:      200 * time.Millisecond,
		ClearSettle:         50 * time.Millisecond,
		PollInterval:        10 * time.Millisecond,
		BootGracePeriod:     bootGrace,
		CycleIDGen:          func() string { return cycleID },
		IsManagedFn:         func(_, _ string) bool { return true },
		HandoffFilePath:     func(_, a string) string { return "/tmp/HANDOFF-" + a + ".md" },
		ReadHandoff:         readHandoff,
		TruncateHandoffFn:   func(_ string) error { return nil },
		InjectFn:            spy.inject,
		ReadGaugeFn:         readGaugeFn,
		CrispIdleFn:         func(_, _ string) bool { return false }, // busy — force-path needed
		HoldingDispatchFn:   func(_, _ string) bool { return false },
		WriteJournalFn:      jc.write,
		SetTmuxEnvFn:        func(_ context.Context, _, _, _ string) error { return nil },
		SetManagedSessionFn: func(_, _, _ string) error { return nil },
	}
	cycler := keeper.NewCycler(cfg, em)

	// Establish prevSID (first session, no grace armed).
	cfPrev := &keeper.CtxFile{Pct: 70.0, SessionID: prevSID}
	if err := cycler.MaybeRun(context.Background(), cfPrev); err != nil {
		t.Fatalf("MaybeRun (prevSID): %v", err)
	}

	// Switch to bootSID at 95% (above ForceActPct=95). Grace timer starts for
	// bootSID, but force-path exemption must bypass the grace immediately.
	cfBoot := &keeper.CtxFile{Pct: 95.0, SessionID: bootSID}
	if err := cycler.MaybeRun(context.Background(), cfBoot); err != nil {
		t.Fatalf("MaybeRun (bootSID force-path): %v", err)
	}

	// The cycle must have fired (force-path bypassed grace) — handoff_started should
	// appear before the bootGrace duration elapses.
	phases := jc.snapshot()
	if len(phases) == 0 {
		t.Fatalf("force-path during boot grace: want cycle to fire (journal phases non-empty); got none")
	}
	if phases[0] != "opened" {
		t.Errorf("first journal phase = %q; want \"opened\"", phases[0])
	}
	evts := em.EventsOfType(core.EventTypeSessionKeeperHandoffStarted)
	if len(evts) != 1 {
		t.Errorf("force-path during boot grace: want 1 handoff_started; got %d", len(evts))
	}
}

// TestCycler_AbortDoesNotClearManaged_FirstSession verifies hk-ibb fix 3: when
// the abort happens on the first (and only) session ever observed — so no prior
// session change was seen and currentSessionIDSince is zero — SetManagedSessionFn
// must NOT be called. The watcher should keep monitoring the existing session so
// Gate-6 same-SID force-retry can handle retries rather than creating a new-SID
// latch that triggers boot-grace and the Gate-6 suppression stall.
func TestCycler_AbortDoesNotClearManaged_FirstSession(t *testing.T) {
	t.Parallel()

	const (
		agent   = "abort-no-clear-agent"
		cycleID = "cyc-abort-no-clear"
		sid     = "sess-abort-no-clear"
	)

	em := &keeper.RecordingEmitter{}
	spy := &cycleSpyInjector{}
	jc := &journalCapture{}

	var (
		managedMu        sync.Mutex
		managedCallCount int
	)
	setManagedFn := func(_, _, _ string) error {
		managedMu.Lock()
		defer managedMu.Unlock()
		managedCallCount++
		return nil
	}

	cfg := keeper.CyclerConfig{
		IdleMarkerModTimeFn: idleMarkerFreshNow, // Stop hook wired: model-done on first AwaitModelDone poll (T8)
		AgentName:           agent,
		ProjectDir:          t.TempDir(),
		TmuxTarget:          "fake-pane",
		ActPct:              90.0,
		WarnPct:             80.0,
		ForceActPct:         95.0,
		HandoffTimeout:      40 * time.Millisecond,
		ClearSettle:         10 * time.Millisecond,
		PollInterval:        5 * time.Millisecond,
		CycleIDGen:          func() string { return cycleID },
		IsManagedFn:         func(_, _ string) bool { return true },
		HandoffFilePath:     func(_, a string) string { return "/tmp/HANDOFF-" + a + ".md" },
		ReadHandoff:         handoffNeverReturnsNonce,
		TruncateHandoffFn:   func(_ string) error { return nil },
		InjectFn:            spy.inject,
		ReadGaugeFn: func(_, _ string) (*keeper.CtxFile, time.Time, error) {
			return &keeper.CtxFile{Pct: 95.0, SessionID: sid}, time.Now(), nil
		},
		CrispIdleFn:         func(_, _ string) bool { return true },
		HoldingDispatchFn:   func(_, _ string) bool { return false },
		WriteJournalFn:      jc.write,
		SetTmuxEnvFn:        func(_ context.Context, _, _, _ string) error { return nil },
		SetManagedSessionFn: setManagedFn,
	}
	cycler := keeper.NewCycler(cfg, em)

	// Directly observe sid as the first (and only) session — no prior session change,
	// so currentSessionIDSince stays Zero.
	cf := &keeper.CtxFile{Pct: 95.0, SessionID: sid}
	if err := cycler.MaybeRun(context.Background(), cf); err != nil {
		t.Fatalf("MaybeRun: %v", err)
	}

	// Verify the cycle aborted.
	abortedEvts := em.EventsOfType(core.EventTypeSessionKeeperCycleAborted)
	if len(abortedEvts) != 1 {
		t.Fatalf("want 1 cycle_aborted; got %d", len(abortedEvts))
	}

	// SetManagedSessionFn must NOT have been called: no real session change was
	// observed before this abort (currentSessionIDSince.IsZero()), so clearing
	// .managed would prematurely allow a new SID to latch (hk-ibb fix 3).
	managedMu.Lock()
	count := managedCallCount
	managedMu.Unlock()

	if count != 0 {
		t.Errorf("SetManagedSessionFn called %d times; want 0 (first-session abort must NOT clear managed)", count)
	}
}

// TestCycler_BootGrace_FlappingSID verifies hk-ibb fix 2: a session_id that has
// already been observed does NOT re-arm the boot-grace timer. When a flapping SID
// alternates between novelSID and prevSID, only the first appearance of novelSID
// arms grace; subsequent appearances do not extend it. The cycle fires after one
// bootGrace duration regardless of how many times the SIDs alternate.
func TestCycler_BootGrace_FlappingSID(t *testing.T) {
	t.Parallel()

	const (
		agent    = "flap-grace-agent"
		cycleID  = "cyc-flap-001"
		prevSID  = "sess-flap-prev"
		novelSID = "sess-flap-novel"
	)

	em := &keeper.RecordingEmitter{}
	spy := &cycleSpyInjector{}
	jc := &journalCapture{}

	nonce := "<!-- KEEPER:" + cycleID + " -->"
	readHandoff := handoffReturnsNonceAfter(0, nonce)

	const bootGrace = 150 * time.Millisecond

	readGaugeFn := gaugeReturnsNewSIDAfter(1, "", agent, novelSID, novelSID+"_resumed")

	cfg := keeper.CyclerConfig{
		IdleMarkerModTimeFn: idleMarkerFreshNow, // Stop hook wired: model-done on first AwaitModelDone poll (T8)
		AgentName:           agent,
		ProjectDir:          t.TempDir(),
		TmuxTarget:          "fake-pane",
		ActPct:              90.0,
		WarnPct:             80.0,
		ForceActPct:         95.0,
		HandoffTimeout:      200 * time.Millisecond,
		ClearSettle:         50 * time.Millisecond,
		PollInterval:        10 * time.Millisecond,
		BootGracePeriod:     bootGrace,
		CycleIDGen:          func() string { return cycleID },
		IsManagedFn:         func(_, _ string) bool { return true },
		HandoffFilePath:     func(_, a string) string { return "/tmp/HANDOFF-" + a + ".md" },
		ReadHandoff:         readHandoff,
		TruncateHandoffFn:   func(_ string) error { return nil },
		InjectFn:            spy.inject,
		ReadGaugeFn:         readGaugeFn,
		CrispIdleFn:         func(_, _ string) bool { return true }, // idle — fires without force path
		HoldingDispatchFn:   func(_, _ string) bool { return false },
		WriteJournalFn:      jc.write,
		SetTmuxEnvFn:        func(_ context.Context, _, _, _ string) error { return nil },
		SetManagedSessionFn: func(_, _, _ string) error { return nil },
	}
	cycler := keeper.NewCycler(cfg, em)

	// ── Step 1: establish prevSID at low pct (no grace armed) ──
	cfPrev := &keeper.CtxFile{Pct: 70.0, SessionID: prevSID}
	if err := cycler.MaybeRun(context.Background(), cfPrev); err != nil {
		t.Fatalf("MaybeRun (prevSID): %v", err)
	}

	// ── Step 2: novelSID first appears at 92% — grace arms ──
	cfNovel := &keeper.CtxFile{Pct: 92.0, SessionID: novelSID}
	if err := cycler.MaybeRun(context.Background(), cfNovel); err != nil {
		t.Fatalf("MaybeRun (novelSID first, during grace): %v", err)
	}
	if n := len(em.EventsOfType(core.EventTypeSessionKeeperHandoffStarted)); n != 0 {
		t.Errorf("novelSID first appearance: want 0 handoff_started (grace suppresses); got %d", n)
	}

	// ── Step 3: SID flaps back to prevSID — already seen, no grace re-arm ──
	if err := cycler.MaybeRun(context.Background(), cfPrev); err != nil {
		t.Fatalf("MaybeRun (prevSID flap): %v", err)
	}

	// ── Step 4: novelSID appears again — already seen, no re-arm ──
	if err := cycler.MaybeRun(context.Background(), cfNovel); err != nil {
		t.Fatalf("MaybeRun (novelSID second, during grace): %v", err)
	}
	if n := len(em.EventsOfType(core.EventTypeSessionKeeperHandoffStarted)); n != 0 {
		t.Errorf("novelSID second appearance (during grace): want 0 handoff_started; got %d", n)
	}

	// ── Step 5: wait for original grace to expire (only one bootGrace from step 2) ──
	time.Sleep(bootGrace + 30*time.Millisecond)

	// ── Step 6: novelSID fires after grace — SID already seen, no new grace ──
	if err := cycler.MaybeRun(context.Background(), cfNovel); err != nil {
		t.Fatalf("MaybeRun (novelSID after grace): %v", err)
	}
	if n := len(em.EventsOfType(core.EventTypeSessionKeeperHandoffStarted)); n != 1 {
		t.Errorf("novelSID after grace: want 1 handoff_started (grace expired, cycle fires); got %d", n)
	}
}

// TestCycler_AbortToResumeGraceToRefire is an integration test for the
// three-fix combination (hk-ibb): abort fires on the first post-resume session
// (with a real prior session change), managed is cleared, a novel resumeSID
// appears (gets grace), and after grace the cycle fires. This exercises fix 1
// (force-path bypass of grace), fix 2 (novel-SID grace arm), and fix 3 (abort
// clears managed only after real session change).
func TestCycler_AbortToResumeGraceToRefire(t *testing.T) {
	t.Parallel()

	const (
		agent     = "abort-resume-agent"
		cycleID1  = "cyc-resume-001"
		cycleID2  = "cyc-resume-002"
		prevSID   = "sess-resume-prev"
		abortSID  = "sess-resume-abort"
		resumeSID = "sess-resume-next"
	)

	em := &keeper.RecordingEmitter{}
	spy := &cycleSpyInjector{}

	// Two separate journal captures — one for each cycle.
	jcAbort := &journalCapture{}
	jcResume := &journalCapture{}
	cycleCount := 0
	writeJournalFn := func(path string, j *keeper.CycleJournal) error {
		if cycleCount == 0 {
			return jcAbort.write(path, j)
		}
		return jcResume.write(path, j)
	}

	var (
		managedMu        sync.Mutex
		managedCallCount int
		managedLastValue string
	)
	setManagedFn := func(_, _, sessionID string) error {
		managedMu.Lock()
		defer managedMu.Unlock()
		managedCallCount++
		managedLastValue = sessionID
		return nil
	}

	cycleIDGen := func() string {
		if cycleCount == 0 {
			return cycleID1
		}
		return cycleID2
	}

	nonce2 := "<!-- KEEPER:" + cycleID2 + " -->"
	// First cycle (abortSID): always timeout. Second cycle (resumeSID): nonce available.
	readHandoff := func(path string) (string, error) {
		if cycleCount == 0 {
			return "", nil // abort: no nonce
		}
		return nonce2, nil // resume: nonce present
	}

	const bootGrace = 150 * time.Millisecond

	// ReadGaugeFn returns a new SID after 1 call (for the resume cycle settle).
	readGaugeFn := gaugeReturnsNewSIDAfter(1, "", agent, resumeSID, resumeSID+"_post")

	cfg := keeper.CyclerConfig{
		IdleMarkerModTimeFn: idleMarkerFreshNow, // Stop hook wired: model-done on first AwaitModelDone poll (T8)
		AgentName:           agent,
		ProjectDir:          t.TempDir(),
		TmuxTarget:          "fake-pane",
		ActPct:              90.0,
		WarnPct:             80.0,
		ForceActPct:         95.0,
		HandoffTimeout:      40 * time.Millisecond, // short for the abort cycle
		ClearSettle:         50 * time.Millisecond,
		PollInterval:        5 * time.Millisecond,
		BootGracePeriod:     bootGrace,
		CycleIDGen:          cycleIDGen,
		IsManagedFn:         func(_, _ string) bool { return true },
		HandoffFilePath:     func(_, a string) string { return "/tmp/HANDOFF-" + a + ".md" },
		ReadHandoff:         readHandoff,
		TruncateHandoffFn:   func(_ string) error { return nil },
		InjectFn:            spy.inject,
		ReadGaugeFn:         readGaugeFn,
		CrispIdleFn:         func(_, _ string) bool { return true },
		HoldingDispatchFn:   func(_, _ string) bool { return false },
		WriteJournalFn:      writeJournalFn,
		SetTmuxEnvFn:        func(_ context.Context, _, _, _ string) error { return nil },
		SetManagedSessionFn: setManagedFn,
	}
	cycler := keeper.NewCycler(cfg, em)

	// ── Phase A: establish prevSID → change to abortSID → abort ──

	// 1. Observe prevSID at low pct — establishes currentSessionIDSince as Zero.
	cfPrev := &keeper.CtxFile{Pct: 70.0, SessionID: prevSID}
	if err := cycler.MaybeRun(context.Background(), cfPrev); err != nil {
		t.Fatalf("MaybeRun (prevSID): %v", err)
	}

	// 2. abortSID at 95% — session change arms currentSessionIDSince; force-path
	//    (fix 1) bypasses the new grace and fires the cycle immediately. The cycle
	//    emits handoff_started before aborting at the nonce-poll step.
	cfAbort := &keeper.CtxFile{Pct: 95.0, SessionID: abortSID}
	if err := cycler.MaybeRun(context.Background(), cfAbort); err != nil {
		t.Fatalf("MaybeRun (abortSID): %v", err)
	}
	if n := len(em.EventsOfType(core.EventTypeSessionKeeperCycleAborted)); n != 1 {
		t.Fatalf("Phase A abort: want 1 cycle_aborted; got %d", n)
	}
	// Record how many handoff_started events Phase A produced (should be 1: the
	// cycle fired via force-path but aborted before completing).
	handoffAfterPhaseA := len(em.EventsOfType(core.EventTypeSessionKeeperHandoffStarted))
	if handoffAfterPhaseA != 1 {
		t.Fatalf("Phase A: want 1 handoff_started (fire+abort); got %d", handoffAfterPhaseA)
	}

	// 3. Fix 3: managed must have been cleared (currentSessionIDSince was non-zero).
	managedMu.Lock()
	abortManagedCalls := managedCallCount
	abortManagedLast := managedLastValue
	managedMu.Unlock()
	if abortManagedCalls != 1 || abortManagedLast != "" {
		t.Errorf("Phase A: SetManagedSessionFn calls=%d last=%q; want 1 call with \"\"",
			abortManagedCalls, abortManagedLast)
	}

	// ── Phase B: observe resumeSID; grace fires; cycle re-fires ──
	cycleCount = 1 // advance to second cycle so IDs and handoff behavior change

	// 4. resumeSID at 92% (below ForceActPct) — novel SID, grace arms (fix 2).
	//    Grace suppresses the cycle. (Gate 6 with lastFiredSID=abortSID and
	//    resumeSID≠abortSID would also suppress due to !seenLowPctAfterLastFire,
	//    but the grace gate fires first.)
	cfResume92 := &keeper.CtxFile{Pct: 92.0, SessionID: resumeSID}
	if err := cycler.MaybeRun(context.Background(), cfResume92); err != nil {
		t.Fatalf("MaybeRun (resumeSID during grace): %v", err)
	}
	// No additional handoff_started events during grace.
	if n := len(em.EventsOfType(core.EventTypeSessionKeeperHandoffStarted)); n != handoffAfterPhaseA {
		t.Errorf("resumeSID during grace: want %d handoff_started (no new fires); got %d",
			handoffAfterPhaseA, n)
	}

	// 5. Wait for grace to expire.
	time.Sleep(bootGrace + 30*time.Millisecond)

	// 6. Observe resumeSID at low pct — unlocks Gate-6 seenLowPctAfterLastFire so
	//    the cycle can re-fire on the next high-pct tick.
	cfResumeLow := &keeper.CtxFile{Pct: 70.0, SessionID: resumeSID}
	if err := cycler.MaybeRun(context.Background(), cfResumeLow); err != nil {
		t.Fatalf("MaybeRun (resumeSID low pct for Gate-6 re-arm): %v", err)
	}

	// 7. Now resumeSID at 92% — grace expired, Gate-6 re-armed → cycle fires!
	cfResume := &keeper.CtxFile{Pct: 92.0, SessionID: resumeSID}
	if err := cycler.MaybeRun(context.Background(), cfResume); err != nil {
		t.Fatalf("MaybeRun (resumeSID refire): %v", err)
	}
	afterGrace := len(em.EventsOfType(core.EventTypeSessionKeeperHandoffStarted))
	wantAfterGrace := handoffAfterPhaseA + 1
	if afterGrace != wantAfterGrace {
		t.Errorf("resumeSID after grace + Gate-6 re-arm: want %d handoff_started; got %d",
			wantAfterGrace, afterGrace)
	}
	finalPhases := jcResume.snapshot()
	if len(finalPhases) == 0 {
		t.Errorf("resumeSID refire: want journal phases; got none")
	}
}

// TestCycler_CrossSID_ForceRetry_AfterAbort verifies hk-hz9 fix 2 (PROBE F):
// after an abort, a novel session_id that stays above WarnPct (never dropping
// below it to re-arm seenLowPctAfterLastFire) must NOT wedge permanently.
// Gate-6 cross-SID force-retry: when above the hard force threshold and
// ForceRetryInterval has elapsed, the cycle fires despite seenLowPctAfterLastFire
// being false.
//
// Two sub-cases are exercised:
//
//	A: prevSID → abortSID (abort) → novelSID stays above WarnPct → recovers via
//	   force-retry once interval elapses and context climbs above ForceActPct.
//	B: first-session abort (no prevSID before the aborting SID) → same recovery.
//	   This exercises the case where currentSessionIDSince is zero throughout.
func TestCycler_CrossSID_ForceRetry_AfterAbort(t *testing.T) {
	t.Parallel()

	// hk-h0twl deflake: forceRetryInterval is large in VIRTUAL time (see the
	// per-subtest steppingAdvanceClock) so it dwarfs the deterministic virtual
	// time consumed by the abort drive loop — the "interval not elapsed" checks
	// hold with a huge margin instead of racing real wall-clock between MaybeRun
	// calls. abortHandoffTimeout stays short so the abort trips after a
	// deterministic handful of virtual-stepped polls.
	const forceRetryInterval = 5 * time.Second
	const abortHandoffTimeout = 25 * time.Millisecond

	for _, tc := range []struct {
		name     string
		prevSID  string // empty = first-session case (no prevSID before abortSID)
		abortSID string
		novelSID string
	}{
		{name: "prev-then-abort", prevSID: "sess-prev-A", abortSID: "sess-abort-A", novelSID: "sess-novel-A"},
		{name: "first-session-abort", prevSID: "", abortSID: "sess-abort-B", novelSID: "sess-novel-B"},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			const cycleID1 = "cyc-cross-001"
			const cycleID2 = "cyc-cross-002"

			em := &keeper.RecordingEmitter{}
			spy := &cycleSpyInjector{}
			jc1, jc2 := &journalCapture{}, &journalCapture{}
			cycleCount := 0
			writeJournalFn := func(path string, j *keeper.CycleJournal) error {
				if cycleCount == 0 {
					return jc1.write(path, j)
				}
				return jc2.write(path, j)
			}

			cycleIDGen := func() string {
				if cycleCount == 0 {
					return cycleID1
				}
				return cycleID2
			}

			nonce2 := "<!-- KEEPER:" + cycleID2 + " -->"
			readHandoff := func(_ string) (string, error) {
				if cycleCount == 0 {
					return "", nil // abort: no nonce
				}
				return nonce2, nil
			}
			readGaugeFn := gaugeReturnsNewSIDAfter(1, "", "agent-cross", tc.novelSID, tc.novelSID+"_post")

			clock := newSteppingAdvanceClock(time.Unix(1_700_000_000, 0), 5*time.Millisecond)

			cfg := keeper.CyclerConfig{
				Clock:               clock,
				IdleMarkerModTimeFn: idleMarkerFreshNow, // Stop hook wired: model-done on first AwaitModelDone poll (T8)
				AgentName:           "agent-cross",
				ProjectDir:          t.TempDir(),
				TmuxTarget:          "fake-pane",
				ActPct:              90.0,
				WarnPct:             80.0,
				ForceActPct:         95.0,
				ForceRetryInterval:  forceRetryInterval,
				HandoffTimeout:      abortHandoffTimeout,
				ClearSettle:         20 * time.Millisecond,
				PollInterval:        5 * time.Millisecond,
				// BootGracePeriod disabled: this test focuses on Gate-6, not boot-grace.
				CycleIDGen:          cycleIDGen,
				IsManagedFn:         func(_, _ string) bool { return true },
				HandoffFilePath:     func(_, a string) string { return "/tmp/HANDOFF-" + a + ".md" },
				ReadHandoff:         readHandoff,
				TruncateHandoffFn:   func(_ string) error { return nil },
				InjectFn:            spy.inject,
				ReadGaugeFn:         readGaugeFn,
				CrispIdleFn:         func(_, _ string) bool { return true },
				HoldingDispatchFn:   func(_, _ string) bool { return false },
				WriteJournalFn:      writeJournalFn,
				SetTmuxEnvFn:        func(_ context.Context, _, _, _ string) error { return nil },
				SetManagedSessionFn: func(_, _, _ string) error { return nil },
			}
			cycler := keeper.NewCycler(cfg, em)
			ctx := context.Background()

			// ── Phase A: establish prevSID (if any) then abort on abortSID ──

			if tc.prevSID != "" {
				// Observe prevSID at low pct to establish currentSessionID without
				// arming boot-grace (currentSessionID was "" so the inner block skips).
				cfPrev := &keeper.CtxFile{Pct: 70.0, SessionID: tc.prevSID}
				if err := cycler.MaybeRun(ctx, cfPrev); err != nil {
					t.Fatalf("MaybeRun (prevSID): %v", err)
				}
			}

			// abortSID at 97% (above ForceActPct) — fires immediately (no boot-grace,
			// force-path bypasses Gate 3). Nonce poll times out → abort.
			cfAbort := &keeper.CtxFile{Pct: 97.0, SessionID: tc.abortSID}
			if err := cycler.MaybeRun(ctx, cfAbort); err != nil {
				t.Fatalf("MaybeRun (abortSID): %v", err)
			}
			if n := len(em.EventsOfType(core.EventTypeSessionKeeperCycleAborted)); n != 1 {
				t.Fatalf("Phase A: want 1 cycle_aborted; got %d", n)
			}
			// lastFiredSID = abortSID, lastForcedAttemptAt = now, seenLowPctAfterLastFire = false.

			// ── Phase B: novelSID appears above WarnPct — must be suppressed ──
			cycleCount = 1

			// novelSID at 85% (WarnPct < 85 < ForceActPct=95): Gate-6 cross-SID
			// suppresses because seenLowPctAfterLastFire=false AND not above force.
			cfNovelMid := &keeper.CtxFile{Pct: 85.0, SessionID: tc.novelSID}
			if err := cycler.MaybeRun(ctx, cfNovelMid); err != nil {
				t.Fatalf("MaybeRun (novelSID Warn<pct<Force): %v", err)
			}
			if n := len(em.EventsOfType(core.EventTypeSessionKeeperHandoffStarted)); n != 1 {
				t.Errorf("novelSID mid-band: want still-1 handoff_started; got %d", n)
			}

			// novelSID at 97% (above ForceActPct), immediately after abort:
			// lastForcedAttemptAt just set → interval not elapsed → still suppressed.
			cfNovelForce := &keeper.CtxFile{Pct: 97.0, SessionID: tc.novelSID}
			if err := cycler.MaybeRun(ctx, cfNovelForce); err != nil {
				t.Fatalf("MaybeRun (novelSID force, interval not elapsed): %v", err)
			}
			if n := len(em.EventsOfType(core.EventTypeSessionKeeperHandoffStarted)); n != 1 {
				t.Errorf("novelSID force, interval not elapsed: want still-1 handoff_started; got %d (force-retry fired too early)", n)
			}

			// Cross ForceRetryInterval deterministically in virtual time (no sleep).
			clock.Advance(forceRetryInterval + 15*time.Millisecond)

			// novelSID at 97% after interval: Gate-6 cross-SID force-retry escape fires.
			if err := cycler.MaybeRun(ctx, cfNovelForce); err != nil {
				t.Fatalf("MaybeRun (novelSID force, after interval): %v", err)
			}
			if n := len(em.EventsOfType(core.EventTypeSessionKeeperHandoffStarted)); n != 2 {
				t.Errorf("novelSID force, after interval: want 2 handoff_started (force-retry fired); got %d", n)
			}
		})
	}
}

// TestCycler_BootGrace_BurstRelativeCap verifies hk-hz9 fix 1 (PROBE H):
// after MaxBootGraceTotal has elapsed for one burst, a brand-new novel SID
// must STILL receive a full boot-grace window (the cap resets per burst, not
// lifetime). Without the fix, bootGraceFirstArmAt is never reset and
// totalExceeded is permanently true ~10 min in, disabling boot-grace forever.
func TestCycler_BootGrace_BurstRelativeCap(t *testing.T) {
	t.Parallel()

	const (
		agent    = "burst-cap-agent"
		prevSID  = "sess-burst-prev"
		firstSID = "sess-burst-first"
		nextSID  = "sess-burst-next"
	)

	em := &keeper.RecordingEmitter{}
	spy := &cycleSpyInjector{}
	jc := &journalCapture{}

	// Set MaxBootGraceTotal very short so we can observe it elapsing.
	const bootGrace = 80 * time.Millisecond
	const maxBootGraceTotal = 60 * time.Millisecond // shorter than bootGrace

	cfg := keeper.CyclerConfig{
		IdleMarkerModTimeFn: idleMarkerFreshNow, // Stop hook wired: model-done on first AwaitModelDone poll (T8)
		AgentName:           agent,
		ProjectDir:          t.TempDir(),
		TmuxTarget:          "fake-pane",
		ActPct:              90.0,
		WarnPct:             80.0,
		ForceActPct:         95.0,
		HandoffTimeout:      300 * time.Millisecond,
		ClearSettle:         20 * time.Millisecond,
		PollInterval:        5 * time.Millisecond,
		BootGracePeriod:     bootGrace,
		MaxBootGraceTotal:   maxBootGraceTotal,
		CycleIDGen:          func() string { return "cyc-burst-cap" },
		IsManagedFn:         func(_, _ string) bool { return true },
		HandoffFilePath:     func(_, a string) string { return "/tmp/HANDOFF-" + a + ".md" },
		ReadHandoff:         func(_ string) (string, error) { return "", nil }, // abort
		TruncateHandoffFn:   func(_ string) error { return nil },
		InjectFn:            spy.inject,
		ReadGaugeFn: func(_, _ string) (*keeper.CtxFile, time.Time, error) {
			return &keeper.CtxFile{Pct: 85.0, SessionID: nextSID}, time.Now(), nil
		},
		CrispIdleFn:         func(_, _ string) bool { return true },
		HoldingDispatchFn:   func(_, _ string) bool { return false },
		WriteJournalFn:      jc.write,
		SetTmuxEnvFn:        func(_ context.Context, _, _, _ string) error { return nil },
		SetManagedSessionFn: func(_, _, _ string) error { return nil },
	}
	cycler := keeper.NewCycler(cfg, em)
	ctx := context.Background()

	// 1. Observe prevSID at low pct — no grace armed (first SID seen, currentSessionID "").
	cfPrev := &keeper.CtxFile{Pct: 70.0, SessionID: prevSID}
	if err := cycler.MaybeRun(ctx, cfPrev); err != nil {
		t.Fatalf("MaybeRun (prevSID): %v", err)
	}

	// 2. firstSID at 92%: session change from prevSID → firstSID arms bootGraceFirstArmAt.
	cfFirst := &keeper.CtxFile{Pct: 92.0, SessionID: firstSID}
	if err := cycler.MaybeRun(ctx, cfFirst); err != nil {
		t.Fatalf("MaybeRun (firstSID grace-arm): %v", err)
	}
	// Grace must suppress.
	if n := len(em.EventsOfType(core.EventTypeSessionKeeperHandoffStarted)); n != 0 {
		t.Fatalf("firstSID during grace: want 0 handoff_started; got %d", n)
	}

	// 3. Wait for MaxBootGraceTotal to elapse — the OLD first burst is now expired.
	time.Sleep(maxBootGraceTotal + 15*time.Millisecond)

	// 4. nextSID at 92%: this is a novel SID. With the fix, bootGraceFirstArmAt
	//    resets because MaxBootGraceTotal has elapsed → a new burst window starts
	//    → boot-grace suppresses the cycle for this new SID.
	cfNext := &keeper.CtxFile{Pct: 92.0, SessionID: nextSID}
	if err := cycler.MaybeRun(ctx, cfNext); err != nil {
		t.Fatalf("MaybeRun (nextSID, should be grace-protected): %v", err)
	}
	if n := len(em.EventsOfType(core.EventTypeSessionKeeperHandoffStarted)); n != 0 {
		t.Errorf("nextSID after old burst expired: want 0 handoff_started (grace reset for new burst); got %d", n)
	}

	// 5. Wait for the new burst's BootGracePeriod to elapse.
	time.Sleep(bootGrace + 15*time.Millisecond)

	// 6. Observe nextSID at low pct to set seenLowPctAfterLastFire (lastFiredSID="" so this is a no-op).
	cfNextLow := &keeper.CtxFile{Pct: 70.0, SessionID: nextSID}
	if err := cycler.MaybeRun(ctx, cfNextLow); err != nil {
		t.Fatalf("MaybeRun (nextSID low): %v", err)
	}

	// 7. nextSID at 92% after new burst grace expires → cycle fires (gate-3 act, gate-4 crisp).
	if err := cycler.MaybeRun(ctx, cfNext); err != nil {
		t.Fatalf("MaybeRun (nextSID after grace): %v", err)
	}
	if n := len(em.EventsOfType(core.EventTypeSessionKeeperHandoffStarted)); n != 1 {
		t.Errorf("nextSID after new burst grace expires: want 1 handoff_started; got %d", n)
	}
}
