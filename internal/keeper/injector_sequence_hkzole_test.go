package keeper

// injector_sequence_hkzole_test.go — drives the FULL InjectText / sendEnter
// bracketed-paste sequence against a fake tmux runner so the paste→settle→
// Enter→retry mechanism is EXERCISED, not just its timing constants asserted.
//
// Background (hk-zole): the existing injector_test.go covered only the
// empty-target guard, sleepCtx, and three constant-value checks. InjectText was
// ~10.5% covered and sendEnter 0% — the suite asserted the timing magic numbers
// but never RAN the sequence those numbers govern (the literal production
// failure surface, hk-89g). These tests close that gap deterministically by
// swapping the package-level tmuxRunFn seam — no real tmux, no real claude.
//
// Package keeper (not keeper_test) so the tests can reach tmuxRunFn and the
// unexported sendEnter / submitSettle / submitRetryDelay symbols.

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

// tmuxCall records one invocation of the tmux seam: the argv and the stdin.
type tmuxCall struct {
	args  []string
	stdin string
}

// fakeTmuxRunner is a deterministic stand-in for runTmuxCombined. It records
// every call and lets a test decide the (output, error) per call via failOn:
// if failOn[args[0]] (the tmux subcommand) is set, that call returns the error.
type fakeTmuxRunner struct {
	mu     sync.Mutex
	calls  []tmuxCall
	failOn map[string]error // keyed by tmux subcommand, e.g. "send-keys"
}

func (f *fakeTmuxRunner) run(_ context.Context, stdin string, args ...string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, tmuxCall{args: append([]string(nil), args...), stdin: stdin})
	if f.failOn != nil && len(args) > 0 {
		if err := f.failOn[args[0]]; err != nil {
			return []byte("boom"), err
		}
	}
	return nil, nil
}

// subcommands returns the tmux subcommand (args[0]) of each recorded call in order.
func (f *fakeTmuxRunner) subcommands() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, 0, len(f.calls))
	for _, c := range f.calls {
		if len(c.args) > 0 {
			out = append(out, c.args[0])
		}
	}
	return out
}

// installFakeTmux swaps tmuxRunFn for the fake and restores it on cleanup. It
// also zeroes the settle/retry delays so the test runs instantly while still
// EXECUTING the settle/retry code paths (sleepCtx is called with d==0, returns
// true). Returns the fake for assertions.
func installFakeTmux(t *testing.T, failOn map[string]error) *fakeTmuxRunner {
	t.Helper()
	f := &fakeTmuxRunner{failOn: failOn}
	origRun := tmuxRunFn
	origSettle := submitSettle
	origRetryDelay := submitRetryDelay
	tmuxRunFn = f.run
	submitSettle = 0
	submitRetryDelay = 0
	t.Cleanup(func() {
		tmuxRunFn = origRun
		submitSettle = origSettle
		submitRetryDelay = origRetryDelay
	})
	return f
}

// ── full happy-path sequence ────────────────────────────────────────────────

// TestInjectText_RunsFullPasteSequence drives InjectText end-to-end against the
// fake runner and asserts the exact tmux command sequence: load-buffer (with the
// text on stdin) → paste-buffer → send-keys Enter (the load-bearing first Enter)
// → submitRetries additional send-keys Enter retries. This EXERCISES the
// mechanism the old suite only asserted constants for.
func TestInjectText_RunsFullPasteSequence(t *testing.T) {
	f := installFakeTmux(t, nil)

	const target = "sess:0.0"
	const text = "/session-resume\n"
	if err := InjectText(context.Background(), target, text); err != nil {
		t.Fatalf("InjectText: unexpected error: %v", err)
	}

	got := f.subcommands()
	// 1 load-buffer + 1 paste-buffer + 1 first Enter + submitRetries retries.
	want := []string{"load-buffer", "paste-buffer"}
	for i := 0; i < 1+submitRetries; i++ {
		want = append(want, "send-keys")
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("tmux sequence mismatch:\n got=%v\nwant=%v", got, want)
	}

	// load-buffer must carry the injected text on stdin (the bracketed paste).
	if f.calls[0].stdin != text {
		t.Errorf("load-buffer stdin = %q; want %q", f.calls[0].stdin, text)
	}
	// paste-buffer and every send-keys must address the target pane.
	for i := 1; i < len(f.calls); i++ {
		if !argvContains(f.calls[i].args, target) {
			t.Errorf("call %d (%v) does not address target %q", i, f.calls[i].args, target)
		}
	}
	// every send-keys must send Enter (a real key event, not a paste).
	for _, c := range f.calls {
		if len(c.args) > 0 && c.args[0] == "send-keys" && !argvContains(c.args, "Enter") {
			t.Errorf("send-keys call %v missing Enter", c.args)
		}
	}
}

// ── settle + retry behavior is actually executed ────────────────────────────

// TestInjectText_SettleAndRetriesExecuted proves the settle wait and the bounded
// retry loop run by counting the send-keys calls (1 first Enter + submitRetries)
// and confirming the settle path is taken (a non-zero settle is observed to
// elapse via a real, tiny duration rather than asserting the constant).
func TestInjectText_SettleAndRetriesExecuted(t *testing.T) {
	f := &fakeTmuxRunner{}
	origRun := tmuxRunFn
	origSettle := submitSettle
	origRetry := submitRetryDelay
	tmuxRunFn = f.run
	submitSettle = 3 * time.Millisecond
	submitRetryDelay = 2 * time.Millisecond
	t.Cleanup(func() {
		tmuxRunFn = origRun
		submitSettle = origSettle
		submitRetryDelay = origRetry
	})

	start := time.Now()
	if err := InjectText(context.Background(), "t:0", "hi"); err != nil {
		t.Fatalf("InjectText: %v", err)
	}
	elapsed := time.Since(start)

	enters := 0
	for _, sc := range f.subcommands() {
		if sc == "send-keys" {
			enters++
		}
	}
	if enters != 1+submitRetries {
		t.Errorf("send-keys count = %d; want %d (1 first + %d retries)", enters, 1+submitRetries, submitRetries)
	}
	// settle (3ms) + submitRetries*retryDelay (2*2ms) must all have elapsed:
	// the sequence cannot have skipped the settle/retry waits.
	min := submitSettle + time.Duration(submitRetries)*submitRetryDelay
	if elapsed < min {
		t.Errorf("elapsed %v < min %v — settle/retry waits were skipped", elapsed, min)
	}
}

// ── error paths ─────────────────────────────────────────────────────────────

// TestInjectText_LoadBufferErrorStops: a load-buffer failure aborts before any
// paste/Enter.
func TestInjectText_LoadBufferErrorStops(t *testing.T) {
	sentinel := errors.New("load failed")
	f := installFakeTmux(t, map[string]error{"load-buffer": sentinel})
	err := InjectText(context.Background(), "t:0", "x")
	if err == nil || !strings.Contains(err.Error(), "load-buffer") {
		t.Fatalf("want load-buffer error; got %v", err)
	}
	if subs := f.subcommands(); len(subs) != 1 || subs[0] != "load-buffer" {
		t.Errorf("expected to stop after load-buffer; got calls %v", subs)
	}
}

// TestInjectText_PasteBufferErrorStops: a paste-buffer failure aborts before the
// settle / Enter sequence.
func TestInjectText_PasteBufferErrorStops(t *testing.T) {
	f := installFakeTmux(t, map[string]error{"paste-buffer": errors.New("no pane")})
	err := InjectText(context.Background(), "t:0", "x")
	if err == nil || !strings.Contains(err.Error(), "paste-buffer") {
		t.Fatalf("want paste-buffer error; got %v", err)
	}
	if subs := f.subcommands(); strings.Join(subs, ",") != "load-buffer,paste-buffer" {
		t.Errorf("expected stop after paste-buffer; got %v", subs)
	}
}

// TestInjectText_FirstEnterErrorIsFatal: the first submit Enter is load-bearing —
// its error is surfaced. Retries still must NOT run after a fatal first Enter.
func TestInjectText_FirstEnterErrorIsFatal(t *testing.T) {
	f := installFakeTmux(t, map[string]error{"send-keys": errors.New("enter dropped")})
	err := InjectText(context.Background(), "t:0", "x")
	if err == nil || !strings.Contains(err.Error(), "send-keys Enter") {
		t.Fatalf("want fatal first-Enter error; got %v", err)
	}
	// load-buffer, paste-buffer, exactly ONE send-keys (the failed first Enter).
	sk := 0
	for _, sc := range f.subcommands() {
		if sc == "send-keys" {
			sk++
		}
	}
	if sk != 1 {
		t.Errorf("first-Enter failure must not run retries; send-keys count = %d, want 1", sk)
	}
}

// TestInjectText_RetryEntersAreBestEffort: an error on a RETRY Enter (after a
// successful first Enter) is swallowed — InjectText still returns nil. Modeled
// by failing send-keys only from the 2nd send-keys onward.
func TestInjectText_RetryEntersAreBestEffort(t *testing.T) {
	f := &fakeTmuxRunner{}
	origRun := tmuxRunFn
	origSettle := submitSettle
	origRetry := submitRetryDelay
	var skSeen int
	tmuxRunFn = func(ctx context.Context, stdin string, args ...string) ([]byte, error) {
		if len(args) > 0 && args[0] == "send-keys" {
			skSeen++
			if skSeen >= 2 { // first Enter ok; retries fail
				_, _ = f.run(ctx, stdin, args...) // record only
				return []byte("retry boom"), errors.New("retry dropped")
			}
		}
		return f.run(ctx, stdin, args...)
	}
	submitSettle = 0
	submitRetryDelay = 0
	t.Cleanup(func() {
		tmuxRunFn = origRun
		submitSettle = origSettle
		submitRetryDelay = origRetry
	})

	if err := InjectText(context.Background(), "t:0", "x"); err != nil {
		t.Fatalf("retry-Enter errors must be best-effort (nil); got %v", err)
	}
	if skSeen != 1+submitRetries {
		t.Errorf("retry loop short-circuited: send-keys seen = %d, want %d", skSeen, 1+submitRetries)
	}
}

// ── cancel paths ────────────────────────────────────────────────────────────

// TestInjectText_CancelDuringSettleStops: a context cancelled during the settle
// returns ctx.Err() and never sends the first Enter.
func TestInjectText_CancelDuringSettleStops(t *testing.T) {
	f := &fakeTmuxRunner{}
	origRun := tmuxRunFn
	origSettle := submitSettle
	tmuxRunFn = f.run
	submitSettle = time.Hour // long settle so cancel wins deterministically
	t.Cleanup(func() { tmuxRunFn = origRun; submitSettle = origSettle })

	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(2 * time.Millisecond); cancel() }()

	err := InjectText(ctx, "t:0", "x")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("want context.Canceled from settle; got %v", err)
	}
	// load-buffer + paste-buffer ran; NO send-keys (cancel preempted the Enter).
	for _, sc := range f.subcommands() {
		if sc == "send-keys" {
			t.Errorf("send-keys must not run after a cancelled settle; calls=%v", f.subcommands())
		}
	}
}

// TestSendEnter_RunsAndReportsError exercises sendEnter directly (0% before
// hk-zole): success path issues exactly one send-keys Enter; the error path
// wraps the tmux stderr.
func TestSendEnter_RunsAndReportsError(t *testing.T) {
	// success
	f := installFakeTmux(t, nil)
	if err := sendEnter(context.Background(), "p:0"); err != nil {
		t.Fatalf("sendEnter success: %v", err)
	}
	if subs := f.subcommands(); len(subs) != 1 || subs[0] != "send-keys" {
		t.Fatalf("sendEnter must issue exactly one send-keys; got %v", subs)
	}
	if !argvContains(f.calls[0].args, "Enter") {
		t.Errorf("sendEnter argv %v missing Enter", f.calls[0].args)
	}

	// error
	f2 := &fakeTmuxRunner{failOn: map[string]error{"send-keys": errors.New("x")}}
	origRun := tmuxRunFn
	tmuxRunFn = f2.run
	defer func() { tmuxRunFn = origRun }()
	if err := sendEnter(context.Background(), "p:0"); err == nil {
		t.Error("sendEnter: want error when tmux fails; got nil")
	}
}

// argvContains reports whether want appears as an element of argv.
func argvContains(argv []string, want string) bool {
	for _, a := range argv {
		if a == want {
			return true
		}
	}
	return false
}
