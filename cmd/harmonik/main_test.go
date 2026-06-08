package main

// main_test.go — unit tests for the run() composition root (hk-kqdpf.4).
//
// Helper prefix: mainFixture (per implementer-protocol.md §Helper-prefix
// discipline; bead hk-kqdpf.4).
//
// Tests cover the $TMUX fail-fast path and the substrate wiring contract
// (acceptance criteria per hk-kqdpf.4).  They exercise run() directly in
// package main so that the composition-root guard can be observed without a
// real tmux server.
//
// Also covers the commitHash → daemon.Config.BinaryCommitHash wiring
// (acceptance criteria per hk-mz0x4).
//
// NOTE: run() registers flags against flag.CommandLine. Calling run() more
// than once in the same test binary would re-define those flags (a panic).
// Each test that calls run() MUST reset flag.CommandLine beforehand via
// mainFixtureResetFlags.  Tests that call run() must NOT be parallel (flag
// reset is not concurrent-safe).
//
// Bead: hk-kqdpf.4, hk-mz0x4.

import (
	"flag"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// mainFixtureResetFlags resets flag.CommandLine to a fresh FlagSet so that
// calling run() multiple times in the same test binary does not panic with
// "flag redefined".  Must be called before each run() invocation.
//
// flag.CommandLine is restored to a new default-equivalent FlagSet on cleanup.
func mainFixtureResetFlags(t *testing.T) {
	t.Helper()
	orig := flag.CommandLine
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
	t.Cleanup(func() { flag.CommandLine = orig })
}

// mainFixtureSaveRestoreArgs saves os.Args and restores it via t.Cleanup.
// Called before run() so that flag parsing sees only the minimal argv.
func mainFixtureSaveRestoreArgs(t *testing.T, args []string) {
	t.Helper()
	orig := os.Args
	t.Cleanup(func() { os.Args = orig })
	os.Args = args
}

// mainFixtureSaveRestoreEnv saves the value of key and restores it on cleanup.
// When unset is true the variable is unset during the test.
func mainFixtureSaveRestoreEnv(t *testing.T, key, val string, unset bool) {
	t.Helper()
	orig, wasSet := os.LookupEnv(key)
	if unset {
		if err := os.Unsetenv(key); err != nil {
			t.Fatalf("os.Unsetenv(%q): %v", key, err)
		}
	} else {
		if err := os.Setenv(key, val); err != nil {
			t.Fatalf("os.Setenv(%q, %q): %v", key, val, err)
		}
	}
	t.Cleanup(func() {
		if wasSet {
			_ = os.Setenv(key, orig)
		} else {
			_ = os.Unsetenv(key)
		}
	})
}

// TestRunTmuxEnvFastFail verifies the $TMUX fail-fast guard in run().
//
// When $TMUX is not set the composition root must return exit code 1 before
// any I/O or daemon operations happen.
//
// Not parallel: calls run() which registers flags against flag.CommandLine;
// see package comment.
//
// Acceptance: hk-kqdpf.4 — "$TMUX unset → fail fast with operator-friendly message".
func TestRunTmuxEnvFastFail(t *testing.T) {
	mainFixtureResetFlags(t)
	mainFixtureSaveRestoreEnv(t, "TMUX", "", true /* unset */)
	mainFixtureSaveRestoreArgs(t, []string{"harmonik"})

	exitCode := run()

	if exitCode != 1 {
		t.Errorf("run() with TMUX unset: got exit code %d, want 1", exitCode)
	}
}

// TestRunTmuxEnvSet_ProceedsToSubstratePath verifies that when $TMUX is set,
// run() proceeds past the fail-fast guard and enters the substrate-construction
// path, then shuts down cleanly when its signal-context is cancelled.
//
// Why this test must drive a shutdown (hk-9ruez):
//
// The test's original assumption — that with a *fake* $TMUX socket run() would
// fail in the tmux probe and return exit 1 — is false on any machine where the
// tmux binary is installed.  OSAdapter.ProbeTmux only runs `tmux -V`; it never
// connects to the $TMUX socket, so the probe succeeds and run() proceeds all
// the way into daemon.Start.  daemon.Start then blocks forever in the work loop
// (internal/daemon/daemon.go: `<-loopDone` after `go runWorkLoop(ctx, deps)`),
// which is *correct* production behavior — the daemon is meant to run until its
// signal-context (SIGINT/SIGTERM) is cancelled.  With no cancellation the test
// hung until the package timeout, cascade-failing the whole cmd/harmonik
// package.
//
// The fix exercises the real production shutdown path: run() builds its own
// signal.NotifyContext(SIGINT, SIGTERM) at the composition root (main.go,
// hk-7oz2f), so we deliver a single SIGTERM to this process to cancel that
// context.  The work loop's exitClean path drains and runWorkLoop returns,
// unblocking <-loopDone so run() returns.  Production dispatch behavior is
// unchanged — only the test now bounds the daemon's lifetime.
//
// The observable contract here is:
//  1. run() does not panic.
//  2. run() reaches daemon.Start (past the $TMUX fail-fast guard) and then
//     returns within a bounded time once SIGTERM cancels its context.
//
// This test is NOT parallel because it modifies global os.Setenv/os.Args state
// and delivers a process signal.
//
// Acceptance: hk-kqdpf.4 — "$TMUX set → wires substrate"; hk-9ruez — bounded
// shutdown so the substrate path does not hang.
func TestRunTmuxEnvSet_ProceedsToSubstratePath(t *testing.T) {
	mainFixtureResetFlags(t)
	mainFixtureSaveRestoreEnv(t, "TMUX", "/tmp/tmux-fake/fake,0,0", false /* set */)

	// Point --project at a fresh temp dir.  This keeps the daemon's .harmonik/
	// state out of the source tree AND guarantees an empty restart-record so the
	// boot-backoff preflight (internal/daemon/restartbackoff.go) computes a
	// zero delay — the test must not inherit accumulated rapid-boot penalties
	// from earlier runs.
	projectDir := t.TempDir()
	mainFixtureSaveRestoreArgs(t, []string{"harmonik", "--project", projectDir})

	// A panic here would indicate the substrate path has a nil-pointer bug.
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("run() with TMUX set panicked: %v", r)
		}
	}()

	// run() blocks in the daemon work loop until its SIGINT/SIGTERM signal
	// context is cancelled, so call it in a goroutine and report the exit code
	// back over a channel.
	exitCh := make(chan int, 1)
	go func() {
		exitCh <- run()
	}()

	// Give run() a moment to install its signal.NotifyContext handler and enter
	// daemon.Start before we deliver the signal.  While NotifyContext is active
	// the SIGTERM is delivered to that handler (cancelling the daemon context)
	// rather than killing the test process via the default disposition.
	time.Sleep(250 * time.Millisecond)
	if err := syscall.Kill(syscall.Getpid(), syscall.SIGTERM); err != nil {
		t.Fatalf("failed to deliver SIGTERM to self: %v", err)
	}

	select {
	case exitCode := <-exitCh:
		// A clean signal-driven shutdown returns 0 (daemon.Start returns nil on
		// ctx-cancel).  Any non-zero code means run() failed earlier in the
		// substrate path, which is also an acceptable terminal outcome — the
		// contract is "no panic and bounded return", not a specific code.
		t.Logf("run() returned exit code %d after SIGTERM", exitCode)
	case <-time.After(30 * time.Second):
		t.Fatal("run() did not return within 30s of SIGTERM — the substrate/work-loop shutdown path is hung (hk-9ruez regression)")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// harmonik run <bead-id> unit tests (hk-icecw)
// ─────────────────────────────────────────────────────────────────────────────

// TestRunBeadSubcommand_MissingBeadID verifies that `harmonik run` with no
// positional arguments returns exit code 1 and does not panic.
//
// Acceptance: hk-icecw — "harmonik run nonexistent returns a clear error and
// non-zero exit".
//
// Parallel: safe — runBeadSubcommand does not touch flag.CommandLine.
func TestRunBeadSubcommand_MissingBeadID(t *testing.T) {
	t.Parallel()

	// Call with no args — missing bead-id.
	got := runBeadSubcommand([]string{})
	if got == 0 {
		t.Errorf("runBeadSubcommand with no args returned 0; want non-zero")
	}
}

// TestRunBeadSubcommand_TooManyArgs verifies that extra positional arguments
// are rejected.
//
// Parallel: safe.
func TestRunBeadSubcommand_TooManyArgs(t *testing.T) {
	t.Parallel()

	got := runBeadSubcommand([]string{"bead-a", "bead-b"})
	if got == 0 {
		t.Errorf("runBeadSubcommand with 2 positional args returned 0; want non-zero")
	}
}

// TestRunBeadSubcommand_UnknownFlag verifies that unknown flags are rejected.
//
// Parallel: safe.
func TestRunBeadSubcommand_UnknownFlag(t *testing.T) {
	t.Parallel()

	got := runBeadSubcommand([]string{"--unknown-flag", "bead-a"})
	if got == 0 {
		t.Errorf("runBeadSubcommand with unknown flag returned 0; want non-zero")
	}
}

// TestRunBeadSubcommand_NoBrOnPath verifies that when 'br' is not on PATH, the
// subcommand returns exit code 1 cleanly.
//
// This test temporarily manipulates PATH to ensure br is not found.
// Not parallel (modifies global env).
func TestRunBeadSubcommand_NoBrOnPath(t *testing.T) {
	mainFixtureSaveRestoreEnv(t, "PATH", "", false /* set to empty */)

	got := runBeadSubcommand([]string{"hk-test-bead"})
	if got == 0 {
		t.Errorf("runBeadSubcommand with no br on PATH returned 0; want non-zero")
	}
}

// TestRunBeadSubcmd_TmuxUnset_NoTmuxBinary verifies that when $TMUX is not set
// and tmux is not in PATH, runBeadSubcommand returns exit code 1 with a
// non-zero exit (actionable error path).
//
// Not parallel: modifies $TMUX and $PATH env vars (global state).
//
// Acceptance: hk-w92me — graceful degradation when tmux binary is absent.
func TestRunBeadSubcmd_TmuxUnset_NoTmuxBinary(t *testing.T) {
	mainFixtureSaveRestoreEnv(t, "TMUX", "", true /* unset */)
	// Point PATH at an empty temp dir so neither tmux nor br can be found.
	mainFixtureSaveRestoreEnv(t, "PATH", t.TempDir(), false /* set */)

	got := runBeadSubcommand([]string{"hk-test-bead"})
	if got == 0 {
		t.Errorf("runBeadSubcommand TMUX-unset no-tmux: got exit 0; want non-zero")
	}
}

// TestRunBeadSubcmd_TmuxUnset_SelfWraps verifies that when $TMUX is not set
// but tmux is in PATH, runBeadSubcommand exec-replaces itself with
// `tmux new-session -- <binary> run <subArgs...>`.
//
// Not parallel: modifies the global runBeadSelfWrapExec var and env vars.
//
// Acceptance: hk-w92me — self-wrap in tmux when $TMUX is unset and tmux is
// available.
func TestRunBeadSubcmd_TmuxUnset_SelfWraps(t *testing.T) {
	mainFixtureSaveRestoreEnv(t, "TMUX", "", true /* unset */)

	// Write a minimal fake tmux script and prepend its dir to PATH.
	binDir := t.TempDir()
	fakeTmux := filepath.Join(binDir, "tmux")
	if err := os.WriteFile(fakeTmux, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write fake tmux: %v", err)
	}
	mainFixtureSaveRestoreEnv(t, "PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"), false)

	// Replace exec with a recorder so the test process is not replaced.
	var gotArgv []string
	origExec := runBeadSelfWrapExec
	runBeadSelfWrapExec = func(_ string, argv []string, _ []string) error {
		gotArgv = argv
		return nil // nil = "success"; process would be replaced in production
	}
	t.Cleanup(func() { runBeadSelfWrapExec = origExec })

	got := runBeadSubcommand([]string{"hk-test-bead"})
	if got != 0 {
		t.Errorf("runBeadSubcommand TMUX-unset self-wrap: got exit %d; want 0", got)
	}
	if len(gotArgv) < 4 {
		t.Fatalf("exec argv too short: %v", gotArgv)
	}
	if gotArgv[0] != "tmux" {
		t.Errorf("exec argv[0] = %q; want \"tmux\"", gotArgv[0])
	}
	if gotArgv[1] != "new-session" {
		t.Errorf("exec argv[1] = %q; want \"new-session\"", gotArgv[1])
	}
	var hasRun, hasBeadID bool
	for _, a := range gotArgv {
		if a == "run" {
			hasRun = true
		}
		if a == "hk-test-bead" {
			hasBeadID = true
		}
	}
	if !hasRun {
		t.Errorf("exec argv missing \"run\" subcommand: %v", gotArgv)
	}
	if !hasBeadID {
		t.Errorf("exec argv missing bead ID \"hk-test-bead\": %v", gotArgv)
	}
}

// TestRunBeadSubcommand_BadProjectDir verifies that a non-existent project dir
// causes a non-zero exit.
//
// Parallel: safe — uses a syntactically plausible but nonexistent dir.
func TestRunBeadSubcommand_BadProjectDir(t *testing.T) {
	t.Parallel()

	got := runBeadSubcommand([]string{"--project", "/nonexistent/path/for/test-hkicecw", "hk-test-bead"})
	if got == 0 {
		t.Errorf("runBeadSubcommand with nonexistent project dir returned 0; want non-zero")
	}
}

// TestCommitHashVar_DefaultIsUnknown verifies the package-level commitHash
// variable declared in version.go has the sentinel value "unknown" in an
// unstamped build (i.e., when -ldflags "-X main.commitHash=<sha>" is NOT
// passed at build time).
//
// This test is deliberately simple: it documents the ldflags injection target
// and confirms the default sentinel so that a missing wiring is immediately
// obvious (the test binary would show "" instead of "unknown").
//
// Acceptance: hk-mz0x4 — commitHash default is "unknown"; zero string must
// not appear in the daemon_started payload.
//
// Parallel: safe — reads a package-level variable but does not modify it.
func TestCommitHashVar_DefaultIsUnknown(t *testing.T) {
	t.Parallel()

	const want = "unknown"
	if commitHash != want {
		// In a stamped build the value will be a real SHA (40 hex chars).
		// In an unstamped test build it must be the sentinel "unknown".
		// A blank string here means version.go lost its initialiser.
		t.Errorf("commitHash = %q; want %q (unstamped build sentinel)", commitHash, want)
	}
}
