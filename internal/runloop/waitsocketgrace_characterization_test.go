package runloop

// waitsocketgrace_characterization_test.go — characterization of the WAIT step
// of the shared launch → dispatch → wait → probe → teardown sequence.
//
// WaitWithSocketGrace is the single "wait for the agent to finish" primitive:
// workloop.go, reviewloop.go, dot_cascade_core.go and dot_gate.go all call it.
// Before this file it had no behavioural tests at all (only the two
// parseOutcomePayload unit tests in waitsocketgrace_test.go), so a Phase-3
// decomposition could have silently changed any of the branches below.
//
// These tests pin OBSERVABLE behaviour only — what the wait does to the session
// (kill/reap), which store queries it issues and in what order, and what it
// returns on each branch. They deliberately do NOT pin the signature or the
// internal helper structure: a refactor that keeps these effects and returns
// the same classification should keep them green.
//
// Behaviour under test (specs/claude-hook-bridge.md §4.7 CHB-020, §4.10 CHB-025):
//   Branch 1/2 — an outcome already in the store is returned WITHOUT paying the
//                Stop-hook grace window.
//   Branch 3   — no outcome within the grace window ⇒ nil outcome, but ExitInfo
//                is still fully populated.
//   The reap   — sess.Wait always runs and its exit metadata always reaches the
//                caller, on every branch.
//   Cancel     — a cancelled context kills the session before the reap, and the
//                post-kill watcher drain is BOUNDED by the injected ClockPort
//                (hk-4c7kw), not unbounded.

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/handler"
	"github.com/gregberns/harmonik/internal/handlercontract"
	hclifecycle "github.com/gregberns/harmonik/internal/handlercontract/lifecycle"
	"github.com/gregberns/harmonik/internal/substrate"
)

// ── fakes ────────────────────────────────────────────────────────────────────

// charSession is a handler.Session that records the kill/reap sequence and
// returns a caller-supplied exit outcome. Only Kill, Wait and Outcome are
// exercised by WaitWithSocketGrace; the remaining methods are contract filler
// and fail the test if the wait step ever reaches for them.
type charSession struct {
	t *testing.T

	mu      sync.Mutex
	calls   []string
	waitErr error
	outcome handler.Outcome
}

func (s *charSession) record(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, name)
}

func (s *charSession) seq() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.calls...)
}

func (s *charSession) called(name string) bool {
	for _, c := range s.seq() {
		if c == name {
			return true
		}
	}
	return false
}

func (s *charSession) Kill(context.Context) error { s.record("Kill"); return nil }

func (s *charSession) Wait(context.Context) error {
	s.record("Wait")
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.waitErr
}

func (s *charSession) Outcome() handler.Outcome {
	s.record("Outcome")
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.outcome
}

func (s *charSession) SendInput(context.Context, string) error {
	s.t.Error("wait step called SendInput — outside its contract")
	return nil
}

func (s *charSession) Stdout() io.Reader {
	s.t.Error("wait step called Stdout — outside its contract")
	return nil
}

func (s *charSession) Stderr() io.Reader {
	s.t.Error("wait step called Stderr — outside its contract")
	return nil
}

func (s *charSession) CloseStdin() error {
	s.t.Error("wait step called CloseStdin — outside its contract")
	return nil
}

func (s *charSession) Machine() *hclifecycle.Machine {
	s.t.Error("wait step called Machine — outside its contract")
	return nil
}

// charHookStore is a HookStore that records which query the wait step issued,
// in order, plus the deadline it attached to the slow-path wait.
type charHookStore struct {
	mu sync.Mutex

	latest     *json.RawMessage
	waitResult json.RawMessage
	waitErr    error

	calls        []string
	waitDeadline time.Duration // remaining budget on the ctx handed to WaitForOutcome
	sawDeadline  bool
}

func (h *charHookStore) LatestOutcome(string, string) *json.RawMessage {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.calls = append(h.calls, "LatestOutcome")
	return h.latest
}

func (h *charHookStore) WaitForOutcome(ctx context.Context, _, _ string) (json.RawMessage, error) {
	h.mu.Lock()
	h.calls = append(h.calls, "WaitForOutcome")
	if dl, ok := ctx.Deadline(); ok {
		h.sawDeadline = true
		h.waitDeadline = time.Until(dl)
	}
	res, err := h.waitResult, h.waitErr
	h.mu.Unlock()
	return res, err
}

func (h *charHookStore) RegisterHookSession(string, string)           {}
func (h *charHookStore) CloseHookSession(string, string)              {}
func (h *charHookStore) SetAgentReadyCallback(string, string, func()) {}

func (h *charHookStore) seq() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string(nil), h.calls...)
}

func rawJSON(t *testing.T, s string) *json.RawMessage {
	t.Helper()
	r := json.RawMessage(s)
	return &r
}

// ── the branches ─────────────────────────────────────────────────────────────

// TestWaitWithSocketGrace_StoredOutcomeSkipsGraceWindow pins the CHB-020
// branch-1/2 fast path: when the Stop hook has ALREADY delivered its outcome,
// the wait returns it without ever consulting the slow path. This is the
// behaviour that keeps the common case off the 3-second grace window — a
// decomposition that always waits the grace would add StopHookGrace to every
// single run in the fleet and this test is what catches that.
func TestWaitWithSocketGrace_StoredOutcomeSkipsGraceWindow(t *testing.T) {
	sess := &charSession{t: t, outcome: handler.Outcome{ExitCode: 0}}
	store := &charHookStore{latest: rawJSON(t, `{"kind":"WORK_COMPLETE"}`)}

	outcome, ei := WaitWithSocketGrace(context.Background(), substrate.SystemClock{},
		store, nil, sess, "run-1", "claude-1")

	if outcome == nil {
		t.Fatal("stored WORK_COMPLETE outcome was not returned (branch 1 lost)")
	}
	if outcome.Kind != "WORK_COMPLETE" {
		t.Errorf("outcome kind = %q, want WORK_COMPLETE", outcome.Kind)
	}
	for _, c := range store.seq() {
		if c == "WaitForOutcome" {
			t.Error("the grace window was entered even though the outcome was already present")
		}
	}
	if ei.ExitCode != 0 {
		t.Errorf("ExitCode = %d, want 0", ei.ExitCode)
	}
}

// TestWaitWithSocketGrace_FailureSignalIsReturnedNotSwallowed pins that a
// FAILURE_SIGNAL outcome reaches the caller intact, sub-reason and suggested
// class included. The wait step classifies nothing itself — it is a carrier —
// and the terminal classification downstream reads exactly these fields.
func TestWaitWithSocketGrace_FailureSignalIsReturnedNotSwallowed(t *testing.T) {
	sess := &charSession{t: t, outcome: handler.Outcome{ExitCode: 1}}
	store := &charHookStore{
		latest: rawJSON(t, `{"kind":"FAILURE_SIGNAL","sub_reason":"tests_failed","suggested_class":"transient"}`),
	}

	outcome, _ := WaitWithSocketGrace(context.Background(), substrate.SystemClock{},
		store, nil, sess, "run-1", "claude-1")

	if outcome == nil {
		t.Fatal("FAILURE_SIGNAL outcome was swallowed")
	}
	if outcome.Kind != "FAILURE_SIGNAL" || outcome.SubReason != "tests_failed" || outcome.SuggestedClass != "transient" {
		t.Errorf("outcome = %+v, want the FAILURE_SIGNAL payload carried through verbatim", *outcome)
	}
}

// TestWaitWithSocketGrace_NoOutcomeReturnsNilWithExitInfo pins CHB-020
// branch 3: the grace window expires with nothing from the Stop hook, so the
// wait returns a nil outcome — and the caller still gets the exit metadata it
// needs to classify the terminal. "No outcome" must never mean "no ExitInfo":
// the whole crashed-agent path downstream is derived from these fields.
func TestWaitWithSocketGrace_NoOutcomeReturnsNilWithExitInfo(t *testing.T) {
	waitErr := errors.New("exit status 137")
	sess := &charSession{
		t:       t,
		waitErr: waitErr,
		outcome: handler.Outcome{ExitCode: 137, StderrTail: []byte("panic: boom")},
	}
	store := &charHookStore{} // nothing stored, nothing arrives

	outcome, ei := WaitWithSocketGrace(context.Background(), substrate.SystemClock{},
		store, nil, sess, "run-1", "claude-1")

	if outcome != nil {
		t.Fatalf("outcome = %+v, want nil (branch 3)", *outcome)
	}
	if ei.ExitCode != 137 {
		t.Errorf("ExitCode = %d, want 137", ei.ExitCode)
	}
	if !errors.Is(ei.WaitErr, waitErr) {
		t.Errorf("WaitErr = %v, want the reap error carried through", ei.WaitErr)
	}
	if string(ei.StderrTail) != "panic: boom" {
		t.Errorf("StderrTail = %q, want the captured tail carried through", ei.StderrTail)
	}
}

// TestWaitWithSocketGrace_QueriesFastPathBeforeSlowPath pins the query ORDER
// and the fact that a miss on the fast path falls through to the slow path.
// The order is load-bearing: an outcome that arrived before the reap must not
// be re-waited for, and an outcome that arrives during the grace must still be
// picked up.
func TestWaitWithSocketGrace_QueriesFastPathBeforeSlowPath(t *testing.T) {
	sess := &charSession{t: t}
	store := &charHookStore{waitResult: json.RawMessage(`{"kind":"REVIEWER_VERDICT"}`)}

	outcome, _ := WaitWithSocketGrace(context.Background(), substrate.SystemClock{},
		store, nil, sess, "run-1", "claude-1")

	if outcome == nil || outcome.Kind != "REVIEWER_VERDICT" {
		t.Fatalf("outcome = %v, want the late-arriving REVIEWER_VERDICT", outcome)
	}
	got := store.seq()
	want := []string{"LatestOutcome", "WaitForOutcome"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("store query order = %v, want %v", got, want)
	}
}

// TestWaitWithSocketGrace_GraceWindowIsBounded pins that the slow-path wait
// carries a deadline rather than blocking forever. A Stop hook that never
// arrives — the crashed-agent case — must not wedge the run goroutine.
//
// It asserts the deadline exists and is no larger than StopHookGrace rather
// than asserting an exact value, so retuning the constant does not break it,
// but removing the bound does.
func TestWaitWithSocketGrace_GraceWindowIsBounded(t *testing.T) {
	sess := &charSession{t: t}
	store := &charHookStore{}

	_, _ = WaitWithSocketGrace(context.Background(), substrate.SystemClock{},
		store, nil, sess, "run-1", "claude-1")

	store.mu.Lock()
	defer store.mu.Unlock()
	if !store.sawDeadline {
		t.Fatal("WaitForOutcome was given an unbounded context — a missing Stop hook would wedge the run")
	}
	if store.waitDeadline <= 0 || store.waitDeadline > StopHookGrace {
		t.Errorf("grace budget = %v, want a positive budget ≤ StopHookGrace (%v)", store.waitDeadline, StopHookGrace)
	}
}

// TestWaitWithSocketGrace_MalformedPayloadsDegradeToBranch3 pins the
// fail-soft parse contract: an outcome the daemon cannot decode is treated as
// "no outcome", never as a panic and never as a partial payload. Both the fast
// and the slow path degrade the same way, and the fast path's failure still
// falls through to the slow path.
func TestWaitWithSocketGrace_MalformedPayloadsDegradeToBranch3(t *testing.T) {
	t.Run("stored payload undecodable", func(t *testing.T) {
		sess := &charSession{t: t, outcome: handler.Outcome{ExitCode: 2}}
		store := &charHookStore{latest: rawJSON(t, `{"kind":`)} // truncated

		outcome, ei := WaitWithSocketGrace(context.Background(), substrate.SystemClock{},
			store, nil, sess, "run-1", "claude-1")

		if outcome != nil {
			t.Fatalf("outcome = %+v, want nil for an undecodable payload", *outcome)
		}
		if ei.ExitCode != 2 {
			t.Errorf("ExitCode = %d, want 2 — ExitInfo survives a parse failure", ei.ExitCode)
		}
		saw := false
		for _, c := range store.seq() {
			if c == "WaitForOutcome" {
				saw = true
			}
		}
		if !saw {
			t.Error("an undecodable stored payload did not fall through to the grace window")
		}
	})

	t.Run("relayed payload undecodable", func(t *testing.T) {
		sess := &charSession{t: t}
		store := &charHookStore{waitResult: json.RawMessage(`not json`)}

		outcome, _ := WaitWithSocketGrace(context.Background(), substrate.SystemClock{},
			store, nil, sess, "run-1", "claude-1")

		if outcome != nil {
			t.Fatalf("outcome = %+v, want nil for an undecodable relay payload", *outcome)
		}
	})
}

// TestWaitWithSocketGrace_SubstrateLiveContextDoesNotKill pins the tmux
// (substrate) happy path: with no watcher and a live context the wait reaps
// the session and never kills it. A decomposition that kills unconditionally
// would terminate every healthy agent at the end of its own run.
func TestWaitWithSocketGrace_SubstrateLiveContextDoesNotKill(t *testing.T) {
	sess := &charSession{t: t}
	store := &charHookStore{}

	_, _ = WaitWithSocketGrace(context.Background(), substrate.SystemClock{},
		store, nil, sess, "run-1", "claude-1")

	if sess.called("Kill") {
		t.Error("a healthy substrate session was killed on the normal completion path")
	}
	if !sess.called("Wait") {
		t.Error("the session was never reaped")
	}
}

// TestWaitWithSocketGrace_SubstrateCancelledContextKillsBeforeReap pins the
// shutdown path for the tmux substrate: a cancelled run context kills the
// session BEFORE the reap, so the reap has something to reap and the deferred
// worktree removal cannot race a live agent (the hk-68pvl ordering that
// ForceTeardownSession also defends).
func TestWaitWithSocketGrace_SubstrateCancelledContextKillsBeforeReap(t *testing.T) {
	sess := &charSession{t: t}
	store := &charHookStore{}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, _ = WaitWithSocketGrace(ctx, substrate.SystemClock{}, store, nil, sess, "run-1", "claude-1")

	seq := sess.seq()
	killIdx, waitIdx := -1, -1
	for i, c := range seq {
		if c == "Kill" && killIdx < 0 {
			killIdx = i
		}
		if c == "Wait" && waitIdx < 0 {
			waitIdx = i
		}
	}
	if killIdx < 0 {
		t.Fatalf("cancelled context did not kill the session (sequence %v)", seq)
	}
	if waitIdx < 0 || killIdx > waitIdx {
		t.Errorf("sequence = %v, want Kill before Wait", seq)
	}
}

// TestWaitWithSocketGrace_NilClockIsBackstopped pins that a caller which
// supplies no ClockPort still completes rather than panicking on a nil
// interface. Struct-literal call sites in the daemon rely on this.
func TestWaitWithSocketGrace_NilClockIsBackstopped(t *testing.T) {
	sess := &charSession{t: t}
	store := &charHookStore{}

	if _, _ = WaitWithSocketGrace(context.Background(), nil, store, nil, sess, "run-1", "claude-1"); !sess.called("Wait") {
		t.Error("nil clock prevented the reap")
	}
}

// ── the exec path: a real watcher ────────────────────────────────────────────

type charDeadLetter struct{}

func (charDeadLetter) Append(core.EventType, []byte, string) error { return nil }

type charEmitter struct{}

func (charEmitter) Emit(context.Context, core.EventType, []byte) error { return nil }
func (charEmitter) EmitWithRunID(context.Context, core.RunID, core.EventType, []byte) error {
	return nil
}

// spawnCharWatcher spawns a real Watcher over a caller-controlled pipe. Closing
// the write end drives the watcher to EOF and closes its Done channel; leaving
// it open models a watcher still draining a grandchild-held stream.
func spawnCharWatcher(t *testing.T) (*handlercontract.Watcher, *io.PipeWriter) {
	t.Helper()
	pr, pw := io.Pipe()
	w := handlercontract.SpawnWatcher(context.Background(), handlercontract.SpawnWatcherConfig{
		SessionID:      core.SessionID("char-sess"),
		ProgressStream: pr,
		Publisher:      charEmitter{},
		DeadLetter:     charDeadLetter{},
	})
	t.Cleanup(func() { _ = pw.Close() })
	return w, pw
}

// TestWaitWithSocketGrace_ExecWatcherExitDoesNotKill pins the exec-path happy
// path: the watcher reaching EOF (the agent exited on its own) satisfies the
// wait, and no kill is issued.
func TestWaitWithSocketGrace_ExecWatcherExitDoesNotKill(t *testing.T) {
	watcher, pw := spawnCharWatcher(t)
	_ = pw.Close() // agent exited: stream EOF
	select {
	case <-watcher.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("watcher never observed EOF — test harness fault, not a product finding")
	}

	sess := &charSession{t: t}
	store := &charHookStore{}

	_, _ = WaitWithSocketGrace(context.Background(), substrate.SystemClock{},
		store, watcher, sess, "run-1", "claude-1")

	if sess.called("Kill") {
		t.Error("a cleanly-exited exec session was killed")
	}
	if !sess.called("Wait") {
		t.Error("the session was never reaped")
	}
}

// TestWaitWithSocketGrace_ExecCancelledDrainIsClockBounded pins hk-4c7kw: when
// the run context is cancelled and the watcher is STILL draining (a grandchild
// is holding the progress stream open), the wait kills the session and then
// gives up on the drain after a bounded window rather than blocking for the
// agent's full remaining runtime.
//
// The bound is driven off the injected ClockPort, so this test proves it in
// virtual time: nothing here waits on the wall clock, and a decomposition that
// reverts the bound to an unbounded receive hangs this test instead of silently
// reintroducing the multi-minute SIGTERM stall.
func TestWaitWithSocketGrace_ExecCancelledDrainIsClockBounded(t *testing.T) {
	watcher, _ := spawnCharWatcher(t) // pipe stays open: watcher never finishes

	sess := &charSession{t: t}
	store := &charHookStore{}
	clock := substrate.NewFakeClock(time.Unix(0, 0))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = WaitWithSocketGrace(ctx, clock, store, watcher, sess, "run-1", "claude-1")
	}()

	// Advance virtual time in small steps, yielding so the wait can arm its
	// bound before the next advance crosses it.
	deadline := time.After(10 * time.Second) // harness-fault guard, not the product bound
	for {
		select {
		case <-done:
			if !sess.called("Kill") {
				t.Error("cancelled exec run did not kill the session before reaping")
			}
			if !sess.called("Wait") {
				t.Error("the session was never reaped after the bounded drain")
			}
			return
		case <-deadline:
			t.Fatal("the post-kill watcher drain never completed — the bound is gone or is not on the injected clock (hk-4c7kw)")
		case <-time.After(2 * time.Millisecond):
			clock.Advance(time.Second)
		}
	}
}
