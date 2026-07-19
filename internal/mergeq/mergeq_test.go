package mergeq

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// startQueue spins up a Queue with a background-cancellable owner goroutine and
// registers cleanup.
func startQueue(t *testing.T) *Queue {
	t.Helper()
	q := New(nil)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	q.Start(ctx)
	return q
}

// TestSubmitAfterQueueStopped confirms a submission does not leak once the
// owner goroutine has stopped: Submit returns ErrQueueStopped at intake
// instead of enqueueing onto a queue nothing will ever drain, and the critical
// never runs.
func TestSubmitAfterQueueStopped(t *testing.T) {
	q := New(nil)
	ctx, cancel := context.WithCancel(context.Background())
	q.Start(ctx)
	cancel()
	// Wait for the owner to observe cancellation and close done.
	<-q.done

	var ran atomic.Bool
	err := q.Submit(context.Background(), "after-stop", func(context.Context) error {
		ran.Store(true)
		return nil
	})
	if !errors.Is(err, ErrQueueStopped) {
		t.Fatalf("want ErrQueueStopped, got %v", err)
	}
	if ran.Load() {
		t.Fatal("critical ran after queue stopped")
	}
}

// TestSubmitRunsCriticalAndReturnsError confirms the critical section runs
// under the owner goroutine and its error propagates back to the submitter.
func TestSubmitRunsCriticalAndReturnsError(t *testing.T) {
	q := startQueue(t)

	sentinel := errors.New("boom")
	var ran bool
	err := q.Submit(context.Background(), "unit", func(context.Context) error {
		ran = true
		return sentinel
	})
	if !ran {
		t.Fatal("critical did not run")
	}
	if !errors.Is(err, sentinel) {
		t.Fatalf("want sentinel error, got %v", err)
	}
}

// TestFIFOOrderUnderConcurrentSubmits launches many GENUINELY concurrent
// submitters (a start gate, no sleep-tuned choreography, no reliance on
// channel-sendq release order) and asserts that execution order exactly
// matches intake-sequence order, and that critical sections never overlap.
//
// Shape: the executor is parked on a gate job while all n submitters race
// through intake. Whatever interleaving the scheduler picks, each submission's
// FIFO position is fixed by the seq number assigned under the queue mutex at
// intake. Once every submission is enqueued, the test snapshots the pending
// list (in-package access) — verifying seqs are dense, ascending 0-based
// intake order — then releases the gate and asserts the execution order equals
// that snapshot. Run with -race to also catch data races on the recorder.
func TestFIFOOrderUnderConcurrentSubmits(t *testing.T) {
	q := startQueue(t)

	const n = 200

	// Gate: occupy the executor so all real submissions accumulate as pending.
	gateReleased := make(chan struct{})
	gateBusy := make(chan struct{})
	go func() {
		if err := q.Submit(context.Background(), "gate", func(context.Context) error {
			close(gateBusy)
			<-gateReleased
			return nil
		}); err != nil {
			t.Errorf("gate submit failed: %v", err)
		}
	}()
	<-gateBusy // executor is now busy; nothing pending will be served yet.

	var mu sync.Mutex
	var executed []string
	var concurrent int32
	var wg sync.WaitGroup

	start := make(chan struct{}) // starting gate: maximize genuine intake contention
	for i := 0; i < n; i++ {
		wg.Add(1)
		label := fmt.Sprintf("job-%d", i)
		go func() {
			defer wg.Done()
			<-start
			if err := q.Submit(context.Background(), label, func(context.Context) error {
				if atomic.AddInt32(&concurrent, 1) != 1 {
					t.Errorf("critical sections overlapped")
				}
				mu.Lock()
				executed = append(executed, label)
				mu.Unlock()
				atomic.AddInt32(&concurrent, -1)
				return nil
			}); err != nil {
				t.Errorf("%s submit failed: %v", label, err)
			}
		}()
	}
	close(start)

	// Wait (condition-driven, not tuned) until every racing submission has
	// completed intake, then snapshot the intake order the seq numbers fixed.
	var intakeOrder []string
	deadline := time.Now().Add(10 * time.Second)
	for {
		q.mu.Lock()
		if len(q.pending) == n {
			base := q.pending[0].seq
			for i, j := range q.pending {
				if j.seq != base+uint64(i) { //nolint:gosec // G115: i is a non-negative loop index over q.pending; no overflow
					q.mu.Unlock()
					t.Fatalf("pending not in dense seq order at %d: seq %d (base %d)", i, j.seq, base)
				}
				intakeOrder = append(intakeOrder, j.label)
			}
			q.mu.Unlock()
			break
		}
		q.mu.Unlock()
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for all submissions to complete intake")
		}
		runtime.Gosched()
	}

	close(gateReleased) // drain
	wg.Wait()

	if len(executed) != n {
		t.Fatalf("want %d executions, got %d", n, len(executed))
	}
	for i, got := range executed {
		if got != intakeOrder[i] {
			t.Fatalf("intake-sequence order violated at execution slot %d: got %s, want %s\nfull order: %v",
				i, got, intakeOrder[i], executed)
		}
	}
}

// TestCtxCancelBeforeExecution confirms a submission whose ctx is cancelled
// while it waits in the queue is skipped (never executed) and returns ctx.Err().
func TestCtxCancelBeforeExecution(t *testing.T) {
	q := startQueue(t)

	// Gate the executor so the target submission cannot start.
	gateReleased := make(chan struct{})
	gateBusy := make(chan struct{})
	go func() {
		if err := q.Submit(context.Background(), "gate", func(context.Context) error {
			close(gateBusy)
			<-gateReleased
			return nil
		}); err != nil {
			t.Errorf("gate submit failed: %v", err)
		}
	}()
	<-gateBusy

	ctx, cancel := context.WithCancel(context.Background())
	var ran atomic.Bool
	done := make(chan error, 1)
	go func() {
		done <- q.Submit(ctx, "victim", func(context.Context) error {
			ran.Store(true)
			return nil
		})
	}()

	time.Sleep(2 * time.Millisecond) // ensure victim is enqueued behind the gate
	cancel()                         // cancel BEFORE execution starts
	close(gateReleased)              // release executor; it should skip the victim

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("want context.Canceled, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Submit did not return after cancellation")
	}
	if ran.Load() {
		t.Fatal("critical ran despite pre-execution cancellation")
	}
}

// TestCtxCancelBeforeEnqueue confirms a ctx already cancelled at Submit time
// returns immediately without ever entering the domain.
func TestCtxCancelBeforeEnqueue(t *testing.T) {
	q := startQueue(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	var ran bool
	err := q.Submit(ctx, "dead", func(context.Context) error {
		ran = true
		return nil
	})
	if ran {
		t.Fatal("critical ran for an already-cancelled ctx")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("want context.Canceled, got %v", err)
	}
}

// TestOnceStartedRunsToCompletion confirms that once a critical section has
// begun, a later ctx cancellation does not abort it — its result is returned.
func TestOnceStartedRunsToCompletion(t *testing.T) {
	q := startQueue(t)

	ctx, cancel := context.WithCancel(context.Background())
	started := make(chan struct{})
	err := q.Submit(ctx, "long", func(context.Context) error {
		close(started)
		// Simulate work; cancel the parent mid-flight.
		cancel()
		time.Sleep(5 * time.Millisecond)
		return nil
	})
	select {
	case <-started:
	default:
		t.Fatal("critical never started")
	}
	if err != nil {
		t.Fatalf("critical should run to completion despite cancel, got %v", err)
	}
}

// --- RSM-INV-005 inventory-test harness scaffolding (RSM-014) ---------------
//
// recordingRunner is a fake command runner the daemon-side prepare/commit
// closures inject in place of the real git/exec runner. It records every
// command label and the phase ("inside"/"outside" the exclusion domain) so the
// mechanical DoD check (merge-queue-design §5) can assert that no build-class
// command executes between Submit-entry and Submit-exit, and that the commit
// phase's command inventory matches the enumerated allowlist. The daemon-side
// wiring lands with M3-4; this scaffolding fixes the harness shape so RT2 can
// stand up the invariant test against internal/mergeq alone.

type recordedCmd struct {
	phase string // "inside" (within a Submit critical) or "outside"
	label string // command label, e.g. "git rebase", "git push"
}

type recordingRunner struct {
	mu       sync.Mutex
	inside   atomic.Bool
	commands []recordedCmd
}

// run records a command under the current phase.
func (r *recordingRunner) run(label string) {
	phase := "outside"
	if r.inside.Load() {
		phase = "inside"
	}
	r.mu.Lock()
	r.commands = append(r.commands, recordedCmd{phase: phase, label: label})
	r.mu.Unlock()
}

// insideLabels returns the labels recorded while inside the exclusion domain.
func (r *recordingRunner) insideLabels() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []string
	for _, c := range r.commands {
		if c.phase == "inside" {
			out = append(out, c.label)
		}
	}
	return out
}

// buildClassCommands are the commands that MUST NOT run inside the exclusion
// domain (merge-queue-design §5) — the critical-section-creep change detector.
var buildClassCommands = map[string]bool{
	"go build":   true,
	"go vet":     true,
	"gofumpt":    true,
	"gci":        true,
	"git rebase": true,
}

// commitAllowlist is the exact command inventory the commit phase may run
// (merge-queue-design §5). The inventory test asserts equality once the daemon
// wiring lands; the harness pins the allowlist here.
var commitAllowlist = []string{
	"git rev-parse",
	"git merge-base",
	"git update-ref",
	"git push",
	"git fetch",
	"git restore",
	"git reset",
	"git diff",
	"br sync",
}

// TestInventoryHarness_NoBuildClassInsideDomain exercises the harness: a
// prepare closure runs build-class work OUTSIDE Submit and the commit closure
// runs only allowlisted commands INSIDE Submit. The DoD assertion is that the
// inside-domain inventory contains zero build-class commands and is a subset of
// the commit allowlist. This stands the RSM-INV-005 harness up against
// internal/mergeq; the real daemon closures are injected at M3-4.
func TestInventoryHarness_NoBuildClassInsideDomain(t *testing.T) {
	q := startQueue(t)
	rr := &recordingRunner{}

	// prepare phase — OUTSIDE the domain: build-class work is expected here.
	rr.run("git rebase")
	rr.run("go build")
	rr.run("go vet")
	rr.run("gofumpt")

	// commit phase — INSIDE the domain via Submit.
	err := q.Submit(context.Background(), "commit-merge", func(context.Context) error {
		rr.inside.Store(true)
		defer rr.inside.Store(false)
		for _, cmd := range []string{
			"git rev-parse", "git merge-base", "git update-ref",
			"git push", "git restore", "git reset", "br sync",
		} {
			rr.run(cmd)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("commit submit failed: %v", err)
	}

	allow := make(map[string]bool, len(commitAllowlist))
	for _, c := range commitAllowlist {
		allow[c] = true
	}
	for _, label := range rr.insideLabels() {
		if buildClassCommands[label] {
			t.Errorf("build-class command %q ran inside the exclusion domain", label)
		}
		if !allow[label] {
			t.Errorf("command %q inside domain is not in the commit allowlist", label)
		}
	}
	if len(rr.insideLabels()) == 0 {
		t.Fatal("harness recorded no inside-domain commands")
	}
}
