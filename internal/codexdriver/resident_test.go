package codexdriver

// White-box tests for the hk-160yb G1b resident-session owner: reconnect across
// child death via thread/resume, and the G3 BoundedInputQueue wired to
// ResidentSession.SubmitInput as its production caller. In-package so the test
// can inspect the swapped-in child (r.cur) and its Outcome. Rides the shared
// twin re-exec harness (driver_test.go TestMain / runTwin).

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/handler"
)

func residentSpawn(mode string) handler.SubstrateSpawn {
	return handler.SubstrateSpawn{
		WindowName: "twin-resident",
		Argv:       []string{os.Args[0], "-test.run=NONE"},
		Env: append(os.Environ(),
			"CODEXDRIVER_TWIN=1",
			"CODEXDRIVER_TWIN_MODE="+mode,
		),
	}
}

// The core G1b behavior: a child that dies after one turn is transparently
// respawned on the next submit, re-attaching to the SAME server thread via
// thread/resume — so the caller sees one durable InputPort.
func TestResidentResumesAcrossChildDeath(t *testing.T) {
	r := NewResidentSession(Options{}, residentSpawn("dieafterturn"), 4)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = r.Close(ctx)
	})

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	// Turn 1: fresh spawn → thread/start → ack → child exits.
	ack1, err := r.SubmitInput(ctx, handler.InputRequest{Payload: []byte("turn one")})
	if err != nil {
		t.Fatalf("SubmitInput #1: %v", err)
	}
	if ack1.Outcome != handler.Delivered {
		t.Fatalf("#1 outcome = %v, want Delivered", ack1.Outcome)
	}
	if got := r.ThreadID(); got != "th_1" {
		t.Fatalf("threadID after #1 = %q, want th_1", got)
	}

	// Make the death deterministic before the next submit so we exercise the
	// revive path, not a race.
	r.mu.Lock()
	s1 := r.cur
	r.mu.Unlock()
	if err := s1.Wait(ctx); err != nil {
		t.Fatalf("wait for child #1 death: %v", err)
	}

	// Turn 2: child is dead → respawn → thread/resume th_1 → ack.
	ack2, err := r.SubmitInput(ctx, handler.InputRequest{Payload: []byte("turn two")})
	if err != nil {
		t.Fatalf("SubmitInput #2 (after death): %v", err)
	}
	if ack2.Outcome != handler.Delivered {
		t.Fatalf("#2 outcome = %v, want Delivered", ack2.Outcome)
	}

	r.mu.Lock()
	s2 := r.cur
	r.mu.Unlock()
	if s2 == s1 {
		t.Fatal("child #2 is the same session as #1 — no respawn occurred")
	}
	if got := r.ThreadID(); got != "th_1" {
		t.Fatalf("threadID after #2 = %q, want th_1 (resumed same thread)", got)
	}

	// Prove #2 re-attached via thread/resume, not a fresh thread/start.
	if err := s2.Wait(ctx); err != nil {
		t.Fatalf("wait for child #2 death: %v", err)
	}
	if tail := string(s2.Outcome().StderrTail); !strings.Contains(tail, "TWIN_RESUME_RECEIVED th_1") {
		t.Fatalf("child #2 stderr %q missing resume marker — respawn did not resume", tail)
	}
}

// The G3 wire-up: Enqueue → the queue's single drainer → ResidentSession
// .SubmitInput → live child. This is the production caller the residual-gap
// audit required (clears the queue's x-missing-wire-up).
func TestResidentEnqueueDeliversThroughQueue(t *testing.T) {
	r := NewResidentSession(Options{}, residentSpawn("happy"), 4)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = r.Close(ctx)
	})

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	resCh, err := r.Enqueue(ctx, handler.InputRequest{Payload: []byte("queued turn")})
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	select {
	case res := <-resCh:
		if res.Err != nil {
			t.Fatalf("queued result err: %v", res.Err)
		}
		if res.Ack.Outcome != handler.Delivered {
			t.Fatalf("queued outcome = %v, want Delivered", res.Ack.Outcome)
		}
	case <-ctx.Done():
		t.Fatal("queued submission never resolved")
	}
}

// After Close, Enqueue is rejected and no child leaks.
func TestResidentEnqueueAfterCloseRejected(t *testing.T) {
	r := NewResidentSession(Options{}, residentSpawn("happy"), 4)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := r.Close(ctx); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := r.Enqueue(ctx, handler.InputRequest{Payload: []byte("late")}); err != ErrResidentClosed {
		t.Fatalf("Enqueue after Close err = %v, want ErrResidentClosed", err)
	}
}
