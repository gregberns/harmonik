package supervisecmd

// shim_hk5gdqu_test.go — subprocess test: _shim with no Command enters
// watchdog-only mode (stays up) instead of exiting 1 (hk-5gdqu).
//
// The subprocess pattern is necessary because RunShim blocks on the
// DaemonWatchdog until SIGTERM is received. The parent sends SIGTERM after
// confirming the watchdog-only announcement appeared on stdout.
//
// Helper prefix: watchdogOnlyFixture (per implementer-protocol.md §Helper-prefix
// discipline; bead hk-5gdqu).

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// watchdogOnlyFixtureSubprocessEnv gates the subprocess worker role.
const watchdogOnlyFixtureSubprocessEnv = "HK_TEST_WATCHDOG_ONLY_HELPER"

// watchdogOnlyFixtureDirEnv carries the temp project dir from parent to subprocess.
const watchdogOnlyFixtureDirEnv = "HK_TEST_WATCHDOG_ONLY_DIR"

// TestShim_EmptyCommand_EntersWatchdogOnly verifies that when config.json has
// no Command field, harmonik supervise _shim enters watchdog-only mode rather
// than exiting immediately with "config.json missing 'command' field" (hk-5gdqu).
//
// Acceptance: subprocess stays alive >200ms after launch AND prints
// "watchdog-only mode" on stdout AND does NOT print "missing 'command' field".
//
// NOT parallel: uses subprocess with fixed env vars; subprocess role modifies
// global signal state.
func TestShim_EmptyCommand_EntersWatchdogOnly(t *testing.T) {
	if os.Getenv(watchdogOnlyFixtureSubprocessEnv) == "1" {
		watchdogOnlyFixtureRunShim()
		// watchdogOnlyFixtureRunShim exits via os.Exit; reaching here is a bug.
		os.Exit(99)
	}

	// Parent role: build config in a fresh temp dir.
	dir := t.TempDir()

	// Pre-create cognition dir so the lock file open in RunShim succeeds.
	if err := os.MkdirAll(filepath.Join(dir, ".harmonik", "cognition"), 0o755); err != nil {
		t.Fatalf("mkdir cognition: %v", err)
	}

	// Write a config with no Command — the trigger for watchdog-only mode.
	cfg := Config{SchemaVersion: 1}
	if err := WriteConfigAtomic(dir, cfg); err != nil {
		t.Fatalf("WriteConfigAtomic: %v", err)
	}

	testBin := os.Args[0]
	args := []string{
		"-test.run=TestShim_EmptyCommand_EntersWatchdogOnly",
		"-test.v",
		"-test.timeout=15s",
	}
	//nolint:gosec // G204: testBin is os.Args[0], subprocess-pattern test-only
	cmd := exec.Command(testBin, args...)
	cmd.Env = append(os.Environ(),
		watchdogOnlyFixtureSubprocessEnv+"=1",
		watchdogOnlyFixtureDirEnv+"="+dir,
	)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		t.Fatalf("start subprocess: %v", err)
	}

	// Allow time for the shim to:
	//   1. acquire the lock
	//   2. write config / pidfile
	//   3. print the watchdog-only announcement
	//   4. install signal handlers
	time.Sleep(500 * time.Millisecond)

	// Subprocess must still be alive — it blocks on the DaemonWatchdog, not
	// on an instant-return error path.
	if err := cmd.Process.Signal(syscall.Signal(0)); err != nil {
		t.Errorf("subprocess exited before SIGTERM was sent (likely hit the old exit-1 path): %v\nstdout: %q\nstderr: %q",
			err, stdout.String(), stderr.String())
	}

	// Terminate cleanly.
	_ = cmd.Process.Signal(syscall.SIGTERM)

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatalf("subprocess did not exit within 5s after SIGTERM")
	}

	// Verify watchdog-only announcement appeared.
	if !strings.Contains(stdout.String(), "watchdog-only mode") {
		t.Errorf("expected 'watchdog-only mode' in stdout\nstdout: %q\nstderr: %q",
			stdout.String(), stderr.String())
	}

	// Verify the old "nothing to run" error message did NOT appear.
	if strings.Contains(stderr.String(), "missing 'command' field") {
		t.Errorf("old error message appeared in stderr (shim took exit-1 path):\n%s", stderr.String())
	}
}

// watchdogOnlyFixtureRunShim is the subprocess worker role: writes the
// supervisor lock/sentinel/pidfile and calls RunShim. It exits via os.Exit
// when RunShim returns or panics.
func watchdogOnlyFixtureRunShim() {
	dir := os.Getenv(watchdogOnlyFixtureDirEnv)
	if dir == "" {
		os.Stderr.WriteString("watchdog-only helper: " + watchdogOnlyFixtureDirEnv + " not set\n")
		os.Exit(1)
	}

	code := RunShim([]string{dir}, os.Stdout, os.Stderr)
	os.Exit(code)
}
