package codexdriver

// White-box test for the hk-160yb G1 resume handshake: spawn(...) with a
// non-empty resumeThreadID must complete its launch handshake via
// `thread/resume <id>` (the G2 wire method) instead of `thread/start`, re-adopt
// the prior server-side thread id, and reach Ready so a submission acks. The
// resume spawn seam is unexported (the resident owner is its only production
// caller — G1b), so this exercises it in-package. It rides the same twin
// re-exec harness as driver_test.go (shared TestMain / runTwin).

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/handler"
)

func TestResumeHandshakeReattachesThread(t *testing.T) {
	sub, ok := NewCodexSubstrate(Options{}).(*codexSubstrate)
	if !ok {
		t.Fatal("NewCodexSubstrate did not return *codexSubstrate")
	}

	const resumeID = "th_resume_1"
	sess, err := sub.spawn(context.Background(), handler.SubstrateSpawn{
		WindowName: "twin-resume",
		Argv:       []string{os.Args[0], "-test.run=NONE"},
		Env: append(os.Environ(),
			"CODEXDRIVER_TWIN=1",
			"CODEXDRIVER_TWIN_MODE=happy",
		),
	}, resumeID)
	if err != nil {
		t.Fatalf("spawn(resume): %v", err)
	}
	cs, ok := sess.(*codexSession)
	if !ok {
		t.Fatalf("spawn returned %T, want *codexSession", sess)
	}
	t.Cleanup(func() {
		_ = sess.Kill(context.Background()) //nolint:errcheck // test teardown; best-effort
		waitCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = sess.Wait(waitCtx) //nolint:errcheck // test teardown; best-effort
	})

	port, ok := handler.AsInputPort(sess)
	if !ok {
		t.Fatal("session does not satisfy handler.InputPort")
	}

	// A successful ack proves the handshake completed — i.e. the reactor reached
	// Ready — which on the resume path can only happen via the thread/resume
	// response (thread/start is never sent when resumeThreadID is set).
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	ack, err := port.SubmitInput(ctx, handler.InputRequest{Payload: []byte("resumed turn")})
	if err != nil {
		t.Fatalf("SubmitInput after resume: %v", err)
	}
	if ack.Outcome != handler.Delivered {
		t.Fatalf("outcome = %v, want Delivered", ack.Outcome)
	}

	// The resumed thread id must be the one we asked to re-attach to, not a
	// fresh thread/start id.
	if got := cs.currentThreadID(); got != resumeID {
		t.Fatalf("threadID = %q, want %q (re-adopted via thread/resume)", got, resumeID)
	}

	// Wind the session down cleanly so Outcome.StderrTail is populated, then
	// prove the twin received thread/resume for the requested id (not
	// thread/start).
	if err := sess.Kill(context.Background()); err != nil {
		t.Fatalf("Kill: %v", err)
	}
	waitCtx, waitCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer waitCancel()
	if err := sess.Wait(waitCtx); err != nil {
		t.Fatalf("Wait: %v", err)
	}
	tail := string(sess.Outcome().StderrTail)
	if want := "TWIN_RESUME_RECEIVED " + resumeID; !strings.Contains(tail, want) {
		t.Fatalf("stderr tail %q missing %q — resume branch not taken", tail, want)
	}
}

// Sanity guard: the ordinary (fresh-thread) spawn path must NOT emit the resume
// marker — the resume branch is strictly gated on a non-empty resumeThreadID.
func TestFreshSpawnDoesNotResume(t *testing.T) {
	sub, ok := NewCodexSubstrate(Options{}).(*codexSubstrate)
	if !ok {
		t.Fatal("NewCodexSubstrate did not return *codexSubstrate")
	}
	sess, err := sub.spawn(context.Background(), handler.SubstrateSpawn{
		WindowName: "twin-fresh",
		Argv:       []string{os.Args[0], "-test.run=NONE"},
		Env: append(os.Environ(),
			"CODEXDRIVER_TWIN=1",
			"CODEXDRIVER_TWIN_MODE=happy",
		),
	}, "")
	if err != nil {
		t.Fatalf("spawn(fresh): %v", err)
	}
	cs, ok := sess.(*codexSession)
	if !ok {
		t.Fatalf("spawn returned %T, want *codexSession", sess)
	}
	port, ok := handler.AsInputPort(sess)
	if !ok {
		t.Fatal("session does not satisfy handler.InputPort")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if _, err := port.SubmitInput(ctx, handler.InputRequest{Payload: []byte("fresh turn")}); err != nil {
		t.Fatalf("SubmitInput: %v", err)
	}
	if got := cs.currentThreadID(); got != "th_1" {
		t.Fatalf("threadID = %q, want th_1 (fresh thread/start)", got)
	}
	_ = sess.Kill(context.Background()) //nolint:errcheck // test teardown; best-effort
	waitCtx, waitCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer waitCancel()
	_ = sess.Wait(waitCtx) //nolint:errcheck // test teardown; best-effort
	if tail := string(sess.Outcome().StderrTail); strings.Contains(tail, "TWIN_RESUME_RECEIVED") {
		t.Fatalf("fresh spawn unexpectedly took resume path: %q", tail)
	}
}
