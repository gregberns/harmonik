package codexdriver

// White-box tests for the hk-daegv writable-roots stamp: when Options.WritableRoots
// is wired, the driver must stamp the returned paths as `runtimeWorkspaceRoots` on
// every thread/start AND thread/resume handshake, computing them from the session's
// worktree cwd (SubstrateSpawn.Cwd — NOT cmd.Dir, which is unset on the remote
// path). This makes codex's OWN `git commit` land under 0.142.0's effective
// workspace-write seatbelt, where the worktree's out-of-root git common dir is
// otherwise denied. The twin echoes the received roots as a stderr marker
// (emitWritableRootsMarker); these ride the same twin re-exec harness as
// posture_internal_test.go.

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/handler"
)

// driveWritableRoots spawns a twin with the given Options, worktree cwd, and resume
// id, drives one submission to force the handshake, winds the session down, and
// returns the twin's stderr tail (which carries the writable-roots marker).
func driveWritableRoots(t *testing.T, opts Options, cwd, resumeID string) string {
	t.Helper()
	sub, ok := NewCodexSubstrate(opts).(*codexSubstrate)
	if !ok {
		t.Fatal("NewCodexSubstrate did not return *codexSubstrate")
	}
	sess, err := sub.spawn(context.Background(), handler.SubstrateSpawn{
		WindowName: "twin-writableroots",
		Cwd:        cwd,
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
	if _, err := port.SubmitInput(ctx, handler.InputRequest{Payload: []byte("writable-roots turn")}); err != nil {
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

// TestWritableRootsStampedOnThreadStart: with a WritableRoots hook set, the fresh
// thread/start carries runtimeWorkspaceRoots = the hook's output (worktree cwd +
// git common dir), computed from the spawn cwd.
func TestWritableRootsStampedOnThreadStart(t *testing.T) {
	cwd := t.TempDir()
	gitDir := cwd + "/.git-common"
	opts := Options{
		WritableRoots: func(worktreeCwd string) []string {
			return []string{worktreeCwd, gitDir}
		},
	}
	tail := driveWritableRoots(t, opts, cwd, "")
	want := "TWIN_WRITABLE_ROOTS_START roots=" + cwd + "," + gitDir
	if !strings.Contains(tail, want) {
		t.Fatalf("stderr tail %q missing %q — writable roots not stamped on thread/start", tail, want)
	}
}

// TestWritableRootsStampedOnThreadResume: the writable roots must ride the resume
// handshake too — a respawn re-attaches via thread/resume, and the reconnected
// session must keep the git common dir writable so its commit still lands.
func TestWritableRootsStampedOnThreadResume(t *testing.T) {
	cwd := t.TempDir()
	gitDir := cwd + "/.git-common"
	opts := Options{
		WritableRoots: func(worktreeCwd string) []string {
			return []string{worktreeCwd, gitDir}
		},
	}
	tail := driveWritableRoots(t, opts, cwd, "th_resume_writableroots")
	want := "TWIN_WRITABLE_ROOTS_RESUME roots=" + cwd + "," + gitDir
	if !strings.Contains(tail, want) {
		t.Fatalf("stderr tail %q missing %q — writable roots not stamped on thread/resume", tail, want)
	}
}

// TestWritableRootsOmittedWithoutHook: no WritableRoots hook ⇒ runtimeWorkspaceRoots
// is omitted entirely (empty marker), leaving codex's default single-root behavior.
func TestWritableRootsOmittedWithoutHook(t *testing.T) {
	cwd := t.TempDir()
	tail := driveWritableRoots(t, Options{}, cwd, "")
	if want := "TWIN_WRITABLE_ROOTS_START roots=\n"; !strings.Contains(tail, want) {
		t.Fatalf("stderr tail %q missing empty-roots marker %q — roots unexpectedly stamped without a hook", tail, want)
	}
}
