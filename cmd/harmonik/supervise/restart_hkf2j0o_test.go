package supervisecmd

// restart_hkf2j0o_test.go — regression tests for hk-f2j0o.
//
// Two bugs prevented 'harmonik supervise restart' from reliably reviving the
// supervisor (and therefore from letting the DaemonWatchdog auto-restart the
// daemon):
//
//  1. RunStop returned exit 1 when supervisor.pid was absent because
//     os.IsNotExist(fmt.Errorf("...: %w", pathErr)) returns false in Go 1.13+.
//     A missing pidfile means the supervisor is simply not running — exit 0.
//
//  2. RunRestart forwarded no --command to RunStart, so RunStart wrote
//     config.json with Command=nil.  The shim then immediately exited
//     ("config.json missing 'command' field"), killing the DaemonWatchdog.

import (
	"bytes"
	"net"
	"os"
	"os/exec"
	"testing"
)

// TestRunStop_NoPidfile_ReturnsZero verifies that RunStop exits 0 when
// supervisor.pid is absent (supervisor already gone — nothing to stop).
// Bug: os.IsNotExist(fmt.Errorf("...: %w", pathErr)) returns false in
// Go 1.13+ so the wrapped ENOENT was treated as a hard error (exit 1).
func TestRunStop_NoPidfile_ReturnsZero(t *testing.T) {
	t.Parallel()

	dir := socketSafeTempDir(t)
	// Create cognition dir so the path lookup doesn't fail for unrelated reasons.
	if err := os.MkdirAll(CognitionDir(dir), 0o755); err != nil {
		t.Fatal(err)
	}
	// Explicitly do not write supervisor.pid.

	var out, errOut bytes.Buffer
	code := RunStop([]string{"--project", dir}, &out, &errOut)
	if code != 0 {
		t.Errorf("RunStop no pidfile: exit %d; stderr=%q", code, errOut.String())
	}
}

// TestRunRestart_PreservesCommand verifies that RunRestart forwards the
// existing Command from config.json to RunStart so the new shim knows what
// to run.  Without the fix, RunStart wrote config.json with Command=nil and
// the shim exited immediately, leaving the DaemonWatchdog goroutine with no
// chance to auto-revive the daemon (hk-f2j0o).
//
// The test seeds a pidfile pointing to a just-exited subprocess so RunStop
// takes the "supervisor already exited" path (exit 0) and RunRestart proceeds
// to call RunStart.
func TestRunRestart_PreservesCommand(t *testing.T) {
	t.Parallel()

	dir := socketSafeTempDir(t)

	// Fake daemon socket so the probeDaemonSocket probe in RunStart passes.
	sockDir := dir + "/.harmonik"
	if err := os.MkdirAll(sockDir, 0o755); err != nil {
		t.Fatal(err)
	}
	l, err := net.Listen("unix", sockDir+"/daemon.sock")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = l.Close() })

	// Kill the flywheel tmux session on teardown (best-effort; no-op if absent).
	t.Cleanup(func() {
		exec.Command("tmux", "kill-session", "-t", FlywheelSessionName(dir)).Run() //nolint:errcheck
	})

	// Write initial config.json with a non-empty Command.
	wantCmd := []string{"sh", "-c", "echo hi"}
	if err := WriteConfigAtomic(dir, Config{
		SchemaVersion: 1,
		RestartPolicy: "on-failure",
		RestartMax:    5,
		Command:       wantCmd,
	}); err != nil {
		t.Fatalf("WriteConfigAtomic: %v", err)
	}

	// Write a pidfile that points to a freshly-exited subprocess so RunStop
	// follows the "already exited" (ErrProcessDone) branch and returns 0.
	dead := exec.Command("true")
	if err := dead.Run(); err != nil {
		t.Fatalf("dead subprocess: %v", err)
	}
	if err := WritePidfile(dir, dead.Process.Pid); err != nil {
		t.Fatalf("WritePidfile: %v", err)
	}

	var out, errOut bytes.Buffer
	// exit code may be non-zero when tmux is absent; we only care about config.json.
	_ = RunRestart([]string{"--project", dir, "--watch-restart"}, &out, &errOut)

	got, err := ReadConfig(dir)
	if err != nil {
		// RunStart rolled back config.json entirely (daemon socket + tmux both absent).
		t.Skipf("config.json not re-written: %v", err)
	}

	if len(got.Command) != len(wantCmd) {
		t.Fatalf("Command after restart: got %v want %v (stderr: %s)", got.Command, wantCmd, errOut.String())
	}
	for i, tok := range wantCmd {
		if got.Command[i] != tok {
			t.Errorf("Command[%d]: got %q want %q", i, got.Command[i], tok)
		}
	}
}
