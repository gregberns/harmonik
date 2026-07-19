package codexdriver

// White-box tests for the hk-160yb G4 PreSpawn seam: the injection point the
// daemon uses to run the stale-WAL guard before every (re)spawn
// (ungraceful-kill recovery on the app-server path). It must run before the
// child launches, and a failure must fail-closed (abort the spawn, no child).

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/handler"
)

func TestPreSpawnHookRunsBeforeChild(t *testing.T) {
	ran := 0
	opts := Options{PreSpawn: func(context.Context) error { ran++; return nil }}
	sub, ok := NewCodexSubstrate(opts).(*codexSubstrate)
	if !ok {
		t.Fatal("NewCodexSubstrate did not return *codexSubstrate")
	}

	sess, err := sub.spawn(context.Background(), handler.SubstrateSpawn{
		WindowName: "twin-prespawn",
		Argv:       []string{os.Args[0], "-test.run=NONE"},
		Env:        append(os.Environ(), "CODEXDRIVER_TWIN=1", "CODEXDRIVER_TWIN_MODE=happy"),
	}, "")
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}
	t.Cleanup(func() {
		_ = sess.Kill(context.Background())
		waitCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = sess.Wait(waitCtx)
	})
	if ran != 1 {
		t.Fatalf("PreSpawn ran %d times, want exactly 1 (before the child)", ran)
	}

	// The child is live: a submit acks, proving PreSpawn did not block the launch.
	port, ok := handler.AsInputPort(sess)
	if !ok {
		t.Fatal("session does not satisfy handler.InputPort")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if _, err := port.SubmitInput(ctx, handler.InputRequest{Payload: []byte("hi")}); err != nil {
		t.Fatalf("SubmitInput: %v", err)
	}
}

func TestPreSpawnFailClosedAbortsSpawn(t *testing.T) {
	guardErr := errors.New("stale WAL held by a live process")
	opts := Options{PreSpawn: func(context.Context) error { return guardErr }}
	sub, ok := NewCodexSubstrate(opts).(*codexSubstrate)
	if !ok {
		t.Fatal("NewCodexSubstrate did not return *codexSubstrate")
	}

	sess, err := sub.spawn(context.Background(), handler.SubstrateSpawn{
		WindowName: "twin-prespawn-fail",
		Argv:       []string{os.Args[0], "-test.run=NONE"},
		Env:        append(os.Environ(), "CODEXDRIVER_TWIN=1", "CODEXDRIVER_TWIN_MODE=happy"),
	}, "")
	if err == nil {
		_ = sess.Kill(context.Background())
		t.Fatal("spawn succeeded despite a failing PreSpawn guard — not fail-closed")
	}
	if !errors.Is(err, guardErr) {
		t.Fatalf("spawn err = %v, want it to wrap the guard error", err)
	}
	if sess != nil {
		t.Fatalf("spawn returned a non-nil session on guard failure: %T", sess)
	}
}
