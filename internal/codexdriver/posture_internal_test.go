package codexdriver

// White-box tests for the hk-5h759 headless crew-orchestration posture: the
// driver must stamp Options.Sandbox + Options.ApprovalPolicy onto every
// thread/start AND thread/resume handshake, and OMIT them (leaving codex's
// default posture) when Options carries none (NFR7). The twin echoes the
// received posture as a stderr marker (emitPostureMarker); these ride the same
// twin re-exec harness as driver_test.go / resume_internal_test.go.

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/handler"
)

// drivePosture spawns a twin with the given posture Options and resume id,
// drives one submission to force the handshake, winds the session down, and
// returns the twin's stderr tail (which carries the posture marker).
func drivePosture(t *testing.T, opts Options, resumeID string) string {
	t.Helper()
	sub, ok := NewCodexSubstrate(opts).(*codexSubstrate)
	if !ok {
		t.Fatal("NewCodexSubstrate did not return *codexSubstrate")
	}
	sess, err := sub.spawn(context.Background(), handler.SubstrateSpawn{
		WindowName: "twin-posture",
		Argv:       []string{os.Args[0], "-test.run=NONE"},
		Env: append(os.Environ(),
			"CODEXDRIVER_TWIN=1",
			"CODEXDRIVER_TWIN_MODE=happy",
		),
	}, resumeID)
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}
	t.Cleanup(func() {
		if err := sess.Kill(context.Background()); err != nil {
			t.Logf("cleanup Kill: %v", err)
		}
		waitCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := sess.Wait(waitCtx); err != nil {
			t.Logf("cleanup Wait: %v", err)
		}
	})

	port, ok := handler.AsInputPort(sess)
	if !ok {
		t.Fatal("session does not satisfy handler.InputPort")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if _, err := port.SubmitInput(ctx, handler.InputRequest{Payload: []byte("posture turn")}); err != nil {
		t.Fatalf("SubmitInput: %v", err)
	}

	if err := sess.Kill(context.Background()); err != nil {
		t.Fatalf("Kill: %v", err)
	}
	waitCtx, waitCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer waitCancel()
	if err := sess.Wait(waitCtx); err != nil {
		t.Fatalf("Wait: %v", err)
	}
	return string(sess.Outcome().StderrTail)
}

// TestHeadlessPostureStampedOnThreadStart: with the headless posture set, the
// fresh thread/start carries danger-full-access + never.
func TestHeadlessPostureStampedOnThreadStart(t *testing.T) {
	tail := drivePosture(t, Options{Sandbox: "danger-full-access", ApprovalPolicy: "never"}, "")
	if want := "TWIN_POSTURE_START sandbox=danger-full-access approval=never"; !strings.Contains(tail, want) {
		t.Fatalf("stderr tail %q missing %q — headless posture not stamped on thread/start", tail, want)
	}
}

// TestHeadlessPostureStampedOnThreadResume: the posture must ride the resume
// handshake too — a respawn re-attaches via thread/resume, and the reconnected
// session must keep running non-interactively.
func TestHeadlessPostureStampedOnThreadResume(t *testing.T) {
	tail := drivePosture(t, Options{Sandbox: "danger-full-access", ApprovalPolicy: "never"}, "th_resume_posture")
	if want := "TWIN_POSTURE_RESUME sandbox=danger-full-access approval=never"; !strings.Contains(tail, want) {
		t.Fatalf("stderr tail %q missing %q — headless posture not stamped on thread/resume", tail, want)
	}
}

// TestNoPostureOmittedByDefault: an unconfigured driver (no composition-root
// posture) omits sandbox/approvalPolicy entirely — codex keeps its own default
// policy (NFR7). The marker shows both fields empty.
func TestNoPostureOmittedByDefault(t *testing.T) {
	tail := drivePosture(t, Options{}, "")
	if want := "TWIN_POSTURE_START sandbox= approval="; !strings.Contains(tail, want) {
		t.Fatalf("stderr tail %q missing %q — posture unexpectedly present on default driver", tail, want)
	}
}
