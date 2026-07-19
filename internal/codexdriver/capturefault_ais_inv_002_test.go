package codexdriver_test

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/codexdriver"
	"github.com/gregberns/harmonik/internal/handler"
)

// alwaysFailWriter is a capture sink that errors on every Write — a full disk /
// broken redaction sink. Under AIS-INV-002 the tee must degrade to uncaptured
// and the live run must be entirely unaffected.
type alwaysFailWriter struct{ n int }

func (w *alwaysFailWriter) Write(p []byte) (int, error) {
	w.n++
	return 0, errors.New("capture: disk full")
}

// TestCaptureFaultDoesNotAbortRun is the AIS-INV-002 acceptance: a failing
// capture writer on BOTH directions must not abort or wedge the live agent run.
// SubmitInput still reaches its Delivered terminal and the session still exits
// cleanly.
func TestCaptureFaultDoesNotAbortRun(t *testing.T) {
	inCap := &alwaysFailWriter{}
	outCap := &alwaysFailWriter{}
	sub := codexdriver.NewCodexSubstrate(codexdriver.Options{InCapture: inCap, OutCapture: outCap})
	sess, err := sub.SpawnWindow(context.Background(), handler.SubstrateSpawn{
		Argv: []string{os.Args[0], "-test.run=NONE"},
		Env:  append(os.Environ(), twinEnv+"=1", twinModeEnv+"=happy"),
	})
	if err != nil {
		t.Fatalf("SpawnWindow: %v", err)
	}
	port := asPort(t, sess)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	ack, err := port.SubmitInput(ctx, handler.InputRequest{Payload: []byte("live payload")})
	if err != nil {
		t.Fatalf("SubmitInput aborted by capture fault (AIS-INV-002 violated): %v", err)
	}
	if ack.Outcome != handler.Delivered {
		t.Fatalf("ack outcome = %v, want Delivered (run degraded by capture fault)", ack.Outcome)
	}
	if err := port.CloseInput(ctx); err != nil {
		t.Fatalf("CloseInput: %v", err)
	}
	if err := sess.Wait(ctx); err != nil {
		t.Fatalf("Wait: %v (capture fault must not wedge the run)", err)
	}
	// The capture sinks were exercised (proving the tee tried) yet the run was
	// unaffected — degrade-to-uncaptured, not fail-closed.
	if inCap.n == 0 && outCap.n == 0 {
		t.Fatalf("capture sinks never written — test did not exercise the tee")
	}
}
