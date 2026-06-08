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
		AppendHandoffFn:   func(_, _ string) error { return nil }, // no-op in most tests
		SetTmuxEnvFn:      func(_ context.Context, _, _, _ string) error { return nil }, // no-op in most tests
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
		AgentName:         agent,
		ProjectDir:        t.TempDir(),
		TmuxTarget:        "fake-pane",
		ActPct:            actPct,
		WarnPct:           warnPct,
		HandoffTimeout:    500 * time.Millisecond,
		ClearSettle:       50 * time.Millisecond,
		PollInterval:      10 * time.Millisecond,
		CycleIDGen:        func() string { return cycleID },
		IsManagedFn:       func(_, _ string) bool { return true },
		HandoffFilePath:   func(_, a string) string { return "/tmp/HANDOFF-" + a + ".md" },
		ReadHandoff:       readHandoff,
		TruncateHandoffFn: func(_ string) error { return nil },
		InjectFn:          spy.inject,
		ReadGaugeFn:       stableGauge,
		CrispIdleFn:       func(_, _ string) bool { return true },
		HoldingDispatchFn: func(_, _ string) bool { return false },
		WriteJournalFn:    jc.write,
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
// /session-resume when the journal phase is "cleared" (crashed after /clear,
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
		AgentName:         agent,
		ProjectDir:        t.TempDir(),
		TmuxTarget:        "fake-pane",
		ActPct:            90.0,
		WarnPct:           80.0,
		IsManagedFn:       func(_, _ string) bool { return true },
		HandoffFilePath:   func(_, a string) string { return "/tmp/HANDOFF-" + a + ".md" },
		TruncateHandoffFn: func(_ string) error { return nil },
		InjectFn:          spy.inject,
		CrispIdleFn:       func(_, _ string) bool { return true },
		HoldingDispatchFn: func(_, _ string) bool { return false },
		WriteJournalFn:    js.write,
		ReadJournalFn:     js.read,
	}
	cycler := keeper.NewCycler(cfg, em)

	if err := cycler.RecoverFromCrash(context.Background()); err != nil {
		t.Fatalf("RecoverFromCrash: %v", err)
	}

	// /session-resume must have been injected.
	texts := spy.texts()
	if len(texts) != 1 {
		t.Fatalf("want 1 inject call; got %d: %v", len(texts), texts)
	}
	if !containsSubstr(texts[0], "/session-resume") {
		t.Errorf("inject[0] should contain '/session-resume'; got %q", texts[0])
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
		AgentName:         agent,
		ProjectDir:        t.TempDir(),
		TmuxTarget:        "fake-pane",
		ActPct:            90.0,
		WarnPct:           80.0,
		IsManagedFn:       func(_, _ string) bool { return true },
		HandoffFilePath:   func(_, a string) string { return "/tmp/HANDOFF-" + a + ".md" },
		TruncateHandoffFn: func(_ string) error { return nil },
		InjectFn:          spy.inject,
		CrispIdleFn:       func(_, _ string) bool { return true },
		HoldingDispatchFn: func(_, _ string) bool { return false },
		WriteJournalFn:    js.write,
		ReadJournalFn:     js.read,
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
				AgentName:         agent,
				ProjectDir:        t.TempDir(),
				TmuxTarget:        "fake-pane",
				ActPct:            90.0,
				WarnPct:           80.0,
				IsManagedFn:       func(_, _ string) bool { return true },
				HandoffFilePath:   func(_, a string) string { return "/tmp/HANDOFF-" + a + ".md" },
				TruncateHandoffFn: func(_ string) error { return nil },
				InjectFn:          spy.inject,
				CrispIdleFn:       func(_, _ string) bool { return true },
				HoldingDispatchFn: func(_, _ string) bool { return false },
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
		AgentName:         "no-journal-agent",
		ProjectDir:        t.TempDir(),
		TmuxTarget:        "fake-pane",
		ActPct:            90.0,
		WarnPct:           80.0,
		IsManagedFn:       func(_, _ string) bool { return true },
		HandoffFilePath:   func(_, a string) string { return "/tmp/HANDOFF-" + a + ".md" },
		TruncateHandoffFn: func(_ string) error { return nil },
		InjectFn:          spy.inject,
		CrispIdleFn:       func(_, _ string) bool { return true },
		HoldingDispatchFn: func(_, _ string) bool { return false },
		WriteJournalFn:    js.write,
		ReadJournalFn:     js.read,
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
		AgentName:         "unmanaged-recover-agent",
		ProjectDir:        t.TempDir(),
		TmuxTarget:        "fake-pane",
		IsManagedFn:       func(_, _ string) bool { return false }, // unmanaged
		HandoffFilePath:   func(_, a string) string { return "/tmp/HANDOFF-" + a + ".md" },
		TruncateHandoffFn: func(_ string) error { return nil },
		InjectFn:          spy.inject,
		CrispIdleFn:       func(_, _ string) bool { return true },
		HoldingDispatchFn: func(_, _ string) bool { return false },
		WriteJournalFn:    js.write,
		ReadJournalFn:     js.read,
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
		AgentName:         agent,
		ProjectDir:        t.TempDir(),
		TmuxTarget:        "fake-pane",
		ActPct:            90.0,
		WarnPct:           80.0,
		HandoffTimeout:    500 * time.Millisecond,
		ClearSettle:       100 * time.Millisecond,
		PollInterval:      10 * time.Millisecond,
		CycleIDGen:        func() string { return newCycleID },
		IsManagedFn:       func(_, _ string) bool { return true },
		HandoffFilePath:   func(_, a string) string { return "/tmp/HANDOFF-" + a + ".md" },
		ReadHandoff:       readHandoff,
		TruncateHandoffFn: truncateFn,
		InjectFn:          spy.inject,
		ReadGaugeFn:       noopGauge,
		CrispIdleFn:       func(_, _ string) bool { return true },
		HoldingDispatchFn: func(_, _ string) bool { return false },
		WriteJournalFn:    jc.write,
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
// the keeper (a) appends a KEEPER-IDENTITY block to the handoff file and (b) calls
// SetTmuxEnvFn with HARMONIK_AGENT=<agent>. Both must happen before /clear.
func TestCycler_IdentityPinnedAfterNonceConfirm(t *testing.T) {
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
		appendedText string
		appendOrder  int // injection-call count when append was called
		envKey       string
		envVal       string
		envOrder     int
		mu           sync.Mutex
		injectCount  int
	)

	spyInject := func(_ context.Context, _, text string) error {
		mu.Lock()
		defer mu.Unlock()
		injectCount++
		_ = spy.inject(context.Background(), "fake-pane", text)
		return nil
	}
	appendFn := func(_, text string) error {
		mu.Lock()
		defer mu.Unlock()
		appendedText = text
		appendOrder = injectCount
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
		AgentName:      agent,
		ProjectDir:     t.TempDir(),
		TmuxTarget:     "fake-pane",
		ActPct:         90.0,
		WarnPct:        80.0,
		HandoffTimeout: 500 * time.Millisecond,
		ClearSettle:    100 * time.Millisecond,
		PollInterval:   10 * time.Millisecond,
		CycleIDGen:     func() string { return cycleID },
		IsManagedFn:    func(_, _ string) bool { return true },
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
		AppendHandoffFn:   appendFn,
		SetTmuxEnvFn:      setEnvFn,
	}
	cycler := keeper.NewCycler(cfg, em)

	cf := &keeper.CtxFile{Pct: 95.0, SessionID: prevSID}
	if err := cycler.MaybeRun(context.Background(), cf); err != nil {
		t.Fatalf("MaybeRun: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()

	// Identity block must contain the agent name.
	if !containsSubstr(appendedText, agent) {
		t.Errorf("AppendHandoffFn text does not contain agent name %q; got: %q", agent, appendedText)
	}
	if !containsSubstr(appendedText, "KEEPER-IDENTITY") {
		t.Errorf("AppendHandoffFn text does not contain KEEPER-IDENTITY marker; got: %q", appendedText)
	}

	// HARMONIK_AGENT must be set to the agent name.
	if envKey != "HARMONIK_AGENT" {
		t.Errorf("SetTmuxEnvFn key = %q; want %q", envKey, "HARMONIK_AGENT")
	}
	if envVal != agent {
		t.Errorf("SetTmuxEnvFn value = %q; want %q", envVal, agent)
	}

	// Both must happen before /clear (inject[1] is /clear).
	// appendOrder and envOrder are the inject-call counts at the time each
	// fn was called; /clear is inject call #2 (0-indexed: handoff=#1, clear=#2).
	if appendOrder >= 2 {
		t.Errorf("AppendHandoffFn called after /clear (inject count was %d; /clear is call 2)", appendOrder)
	}
	if envOrder >= 2 {
		t.Errorf("SetTmuxEnvFn called after /clear (inject count was %d; /clear is call 2)", envOrder)
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
		AgentName:      agent,
		ProjectDir:     t.TempDir(),
		TmuxTarget:     "fake-pane",
		ActPct:         90.0,         // pct gate would NOT fire at 28%
		WarnPct:        80.0,
		ActAbsTokens:   280_000,      // absolute gate fires at exactly 280k
		ActPctCeil:     0.85,
		WarnAbsTokens:  220_000,
		WarnPctCeil:    0.70,
		HandoffTimeout: 200 * time.Millisecond,
		ClearSettle:    50 * time.Millisecond,
		PollInterval:   10 * time.Millisecond,
		CycleIDGen:     func() string { return cycleID },
		IsManagedFn:    func(_, _ string) bool { return true },
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
		AppendHandoffFn:   func(_, _ string) error { return nil },
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
		AgentName:      agent,
		ProjectDir:     t.TempDir(),
		TmuxTarget:     "fake-pane",
		ActPct:         90.0,
		WarnPct:        80.0,
		ActAbsTokens:   280_000,
		ActPctCeil:     0.85,
		WarnAbsTokens:  220_000,
		WarnPctCeil:    0.70,
		HandoffTimeout: 100 * time.Millisecond,
		ClearSettle:    30 * time.Millisecond,
		PollInterval:   10 * time.Millisecond,
		CycleIDGen:     func() string { return cycleID },
		IsManagedFn:    func(_, _ string) bool { return true },
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
		AgentName:      agent,
		ProjectDir:     t.TempDir(),
		TmuxTarget:     "fake-pane",
		ActPct:         90.0,    // pct gate would NOT fire at 85%
		WarnPct:        80.0,
		ActAbsTokens:   280_000, // effective threshold = min(280k, 0.85*200k=170k) = 170k
		ActPctCeil:     0.85,
		WarnAbsTokens:  220_000,
		WarnPctCeil:    0.70,
		HandoffTimeout: 200 * time.Millisecond,
		ClearSettle:    50 * time.Millisecond,
		PollInterval:   10 * time.Millisecond,
		CycleIDGen:     func() string { return cycleID },
		IsManagedFn:    func(_, _ string) bool { return true },
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
		AppendHandoffFn:   func(_, _ string) error { return nil },
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
