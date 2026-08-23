package handler_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/handler"
)

func sessionFixtureCmd(t *testing.T, shell string) *exec.Cmd {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "sh", "-c", shell)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pgid: syscall.Getpgrp()}
	return cmd
}

// TestSession_SendInput verifies that SendInput delivers a line to child stdin.
// The child echoes stdin back to stdout; we read stdout to confirm delivery.
func TestSession_SendInput(t *testing.T) {
	t.Parallel()

	cmd := sessionFixtureCmd(t, `read line; echo "got: $line"`)

	sess, err := handler.NewSession(t.Context(), cmd)
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}

	if err := sess.SendInput(t.Context(), "hello"); err != nil {
		t.Fatalf("SendInput: %v", err)
	}

	stdoutBytes, err := io.ReadAll(sess.Stdout())
	if err != nil {
		t.Fatalf("ReadAll stdout: %v", err)
	}

	got := strings.TrimSpace(string(stdoutBytes))
	if got != "got: hello" {
		t.Errorf("stdout: want %q, got %q", "got: hello", got)
	}

	if err := sess.Wait(t.Context()); err != nil {
		t.Errorf("Wait: %v", err)
	}
}

// TestSession_Kill verifies that Kill terminates the subprocess within the
// ctx deadline and that Wait returns afterward.
func TestSession_Kill(t *testing.T) {
	t.Parallel()

	cmd := sessionFixtureCmd(t, "sleep 300")

	sess, err := handler.NewSession(t.Context(), cmd)
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}

	killCtx, killCancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer killCancel()

	if err := sess.Kill(killCtx); err != nil {
		t.Fatalf("Kill: %v", err)
	}

	waitCtx, waitCancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer waitCancel()

	if waitErr := sess.Wait(waitCtx); waitErr == nil {
		t.Error("Wait after Kill returned nil; want the child's non-zero/signalled exit")
	}

	o := sess.Outcome()
	if o.Duration <= 0 {
		t.Errorf("Outcome.Duration should be positive, got %v", o.Duration)
	}
}

// TestSession_Wait_CleanExit verifies that Wait returns nil when the child
// exits cleanly with status 0.
func TestSession_Wait_CleanExit(t *testing.T) {
	t.Parallel()

	cmd := sessionFixtureCmd(t, "exit 0")

	sess, err := handler.NewSession(t.Context(), cmd)
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}

	if err := sess.Wait(t.Context()); err != nil {
		t.Errorf("Wait: expected nil for clean exit, got %v", err)
	}

	o := sess.Outcome()
	if o.ExitCode != 0 {
		t.Errorf("Outcome.ExitCode: want 0, got %d", o.ExitCode)
	}
}

// TestSession_Outcome_NonZeroExit verifies that Outcome.ExitCode reflects a
// non-zero subprocess exit.
func TestSession_Outcome_NonZeroExit(t *testing.T) {
	t.Parallel()

	cmd := sessionFixtureCmd(t, "exit 42")

	sess, err := handler.NewSession(t.Context(), cmd)
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}

	if waitErr := sess.Wait(t.Context()); waitErr == nil {
		t.Error("Wait returned nil for a child that exited 42")
	}

	o := sess.Outcome()
	if o.ExitCode != 42 {
		t.Errorf("Outcome.ExitCode: want 42, got %d", o.ExitCode)
	}
}

// TestSession_Outcome_StderrTail verifies that Outcome.StderrTail captures the
// last bytes written by the child to stderr.
func TestSession_Outcome_StderrTail(t *testing.T) {
	t.Parallel()

	cmd := sessionFixtureCmd(t, `echo "error output" >&2; exit 1`)

	sess, err := handler.NewSession(t.Context(), cmd)
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}

	var exitErr *exec.ExitError
	if waitErr := sess.Wait(t.Context()); !errors.As(waitErr, &exitErr) {
		t.Fatalf("sess.Wait: want *exec.ExitError for the child's exit 1, got %v", waitErr)
	} else if got := exitErr.ExitCode(); got != 1 {
		t.Errorf("sess.Wait: exit code = %d, want 1", got)
	}

	o := sess.Outcome()
	tail := string(o.StderrTail)
	if !strings.Contains(tail, "error output") {
		t.Errorf("Outcome.StderrTail: want %q in tail, got %q", "error output", tail)
	}
}

// TestSession_Stdout_Exposed verifies that Stdout() returns a non-nil Reader
// before Wait is called, enabling row-#7 to wire SpawnWatcher to it.
func TestSession_Stdout_Exposed(t *testing.T) {
	t.Parallel()

	cmd := sessionFixtureCmd(t, `echo "progress"; sleep 1`)

	sess, err := handler.NewSession(t.Context(), cmd)
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}

	if sess.Stdout() == nil {
		t.Fatal("Stdout() returned nil — row-#7 cannot wire watcher")
	}

	if _, readErr := io.ReadAll(sess.Stdout()); readErr != nil {
		t.Fatalf("drain stdout: %v", readErr)
	}
	if waitErr := sess.Wait(t.Context()); waitErr != nil {
		t.Fatalf("sess.Wait: %v", waitErr)
	}
}

// TestSession_Kill_ReapsImmediateChildPromptly verifies that Kill reaps the
// immediate handler subprocess promptly under the PRODUCTION spawn config
// (Setpgid=true, Pgid=<daemon_pgid>; see sessionFixtureCmd), even when that
// child forked a grandchild that is still alive.
//
// Background (hk-4c7kw): the handler is deliberately placed in the DAEMON's
// process group (HC-044 / PL-006a) so the orphan sweep can find re-parented
// descendants by PGID.  The child is therefore NOT its own group leader, so the
// old `kill(-childPid, …)` path returned ESRCH and never reached the
// subprocess — daemon shutdown then blocked for the full handler runtime.  Kill
// now signals the immediate child's positive PID, which reaps it promptly;
// grandchildren are NOT Kill's responsibility — they are torn down by the
// caller's bounded post-kill wait (waitWithSocketGrace) and the daemon's orphan
// sweep (PL-006), exercised by the T3 scenario tests and the orphan-sweep tests.
//
// This test asserts the property Kill DOES own (prompt immediate-child reap) and
// explicitly does NOT assert grandchild death — that would re-encode the invalid
// assumption that masked the bug.  The grandchild is cleaned up at the end to
// avoid leaking a process out of the test.
//
// Spec ref: process-lifecycle.md §4.2 PL-006a; handler-contract.md HC-044.
// Bead ref: hk-44w19 (signal delivery), hk-4c7kw (prompt-reap under prod pgroup).
func TestSession_Kill_ReapsImmediateChildPromptly(t *testing.T) {
	t.Parallel()

	pidFile := t.TempDir() + "/grandchild.pid"

	script := `sh -c 'sleep 300' & echo $! > ` + pidFile + `; wait`

	cmd := sessionFixtureCmd(t, script)
	sess, err := handler.NewSession(t.Context(), cmd)
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}

	deadline := time.Now().Add(5 * time.Second)
	var grandchildPIDBytes []byte
	for time.Now().Before(deadline) {
		//nolint:gosec // G304: path from t.TempDir(); not user-controlled
		b, readErr := os.ReadFile(pidFile)
		if readErr == nil && len(b) > 0 {
			grandchildPIDBytes = b
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if len(grandchildPIDBytes) == 0 {
		t.Fatal("timed out waiting for grandchild PID file")
	}

	var grandchildPID int
	if _, scanErr := fmt.Sscan(strings.TrimSpace(string(grandchildPIDBytes)), &grandchildPID); scanErr != nil {
		t.Fatalf("parse grandchild PID: %v", scanErr)
	}
	defer func() {
		if killErr := syscall.Kill(grandchildPID, syscall.SIGKILL); killErr != nil && !errors.Is(killErr, syscall.ESRCH) {
			t.Errorf("cleanup: kill orphaned grandchild %d: %v", grandchildPID, killErr)
		}
	}()

	if probeErr := syscall.Kill(grandchildPID, 0); probeErr != nil {
		t.Fatalf("grandchild (pid %d) not alive before Kill: %v", grandchildPID, probeErr)
	}

	killCtx, killCancel := context.WithCancel(t.Context())
	killCancel()

	t0 := time.Now()
	if err := sess.Kill(killCtx); err != nil {
		t.Fatalf("Kill: %v", err)
	}

	waitDone := make(chan error, 1)
	go func() { waitDone <- sess.Wait(t.Context()) }()
	select {
	case <-waitDone:
	case <-time.After(5 * time.Second):
		t.Fatalf("Kill+Wait did not return within 5s (immediate child not reaped promptly) — hk-4c7kw regression")
	}
	t.Logf("Kill+Wait returned in %v", time.Since(t0))

	o := sess.Outcome()
	if o.Signal != syscall.SIGKILL && o.ExitCode != -1 {
		t.Errorf("Outcome should reflect signal-termination of the immediate child; got Signal=%d ExitCode=%d", o.Signal, o.ExitCode)
	}
}

// TestSession_Kill_SIGKILL_Escalation verifies that Kill escalates to SIGKILL
// when the ctx deadline fires before the child exits.
//
// We use a child that ignores SIGTERM to force the escalation path.
func TestSession_Kill_SIGKILL_Escalation(t *testing.T) {
	t.Parallel()

	cmd := sessionFixtureCmd(t, "trap '' TERM; sleep 60")

	sess, err := handler.NewSession(t.Context(), cmd)
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}

	killCtx, killCancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
	defer killCancel()

	if err := sess.Kill(killCtx); err != nil {
		t.Fatalf("Kill (with escalation): %v", err)
	}

	waitCtx, waitCancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer waitCancel()

	if waitErr := sess.Wait(waitCtx); waitErr == nil {
		t.Error("Wait after SIGKILL escalation returned nil; want the child's signalled exit")
	}

	o := sess.Outcome()
	if o.Duration <= 0 {
		t.Errorf("Outcome.Duration should be positive, got %v", o.Duration)
	}
}
