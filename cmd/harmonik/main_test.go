package main

import (
	"bytes"
	"context"
	"flag"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/lifecycle/tmux"
)

func mainFixtureResetFlags(t *testing.T) {
	t.Helper()
	orig := flag.CommandLine
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
	t.Cleanup(func() { flag.CommandLine = orig })
}

func mainFixtureSaveRestoreArgs(t *testing.T, args []string) {
	t.Helper()
	orig := os.Args
	t.Cleanup(func() { os.Args = orig })
	os.Args = args
}

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
			if err := os.Setenv(key, orig); err != nil {
				t.Errorf("restore os.Setenv(%q): %v", key, err)
			}
		} else {
			if err := os.Unsetenv(key); err != nil {
				t.Errorf("restore os.Unsetenv(%q): %v", key, err)
			}
		}
	})
}

// TestRunTmuxEnvUnset_BootsAndReachesDispatchLoop is the end-to-end proof of the
// 2026-07-28 change: with $TMUX unset the daemon must BOOT and reach its dispatch
// loop, then return only when its signal context is cancelled.
//
// This replaces TestRunTmuxEnvFastFail, which asserted the opposite (exit 1
// before any I/O). That guard was written when tmux was the only substrate and
// before the daemon-owned-session fallback existed; the operator reopened locked
// decision #4 on 2026-07-28 to remove it. See cmd/harmonik/tmuxhosting.go.
//
// The distinguishing assertion is NOT the exit code — it is that run() is still
// executing after the settle window. A regression to the old fail-fast returns 1
// immediately, so the "returned before signal delivery" branch fails the test
// rather than passing quietly.
//
// Requires a real tmux: with $TMUX unset the daemon creates its own session, and
// this test asserts that session really exists (the inspectability half of the
// contract). Skipped where tmux is absent — the hermetic contract tests in
// tmuxhosting_test.go cover that host.
//
// Not parallel: calls run() (flag.CommandLine), mutates env, signals the process.
func TestRunTmuxEnvUnset_BootsAndReachesDispatchLoop(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not on PATH; hermetic coverage lives in tmuxhosting_test.go")
	}
	mainFixtureResetFlags(t)
	mainFixtureSaveRestoreEnv(t, "TMUX", "", true /* unset */)

	projectDir := t.TempDir()
	mainFixtureSaveRestoreArgs(t, []string{"harmonik", "start", "daemon", "--project", projectDir})

	sessionName := tmux.DefaultSessionName(projectDir)
	t.Cleanup(func() {
		//nolint:gosec,errcheck // G204: session name derives from t.TempDir(); best-effort cleanup, a missing session is fine
		_ = exec.CommandContext(context.Background(), "tmux", "kill-session", "-t", "="+sessionName).Run()
	})

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM)
	t.Cleanup(func() { signal.Stop(sigCh) })

	exitCh := make(chan int, 1)
	go func() { exitCh <- run() }()

	time.Sleep(2 * time.Second)

	select {
	case exitCode := <-exitCh:
		t.Fatalf("run() with $TMUX unset returned exit code %d before reaching the dispatch loop; want it still running — the daemon must no longer refuse to boot outside tmux", exitCode)
	default:
	}

	// Inspectability half: the daemon created a session the operator can attach to.
	//nolint:gosec // G204: session name is derived from t.TempDir()
	if err := exec.CommandContext(context.Background(), "tmux", "has-session", "-t", "="+sessionName).Run(); err != nil {
		t.Errorf("daemon-owned tmux session %q does not exist (%v); a daemon booted without $TMUX must still be inspectable via `tmux attach`", sessionName, err)
	}

	if err := syscall.Kill(syscall.Getpid(), syscall.SIGTERM); err != nil {
		t.Fatalf("failed to deliver SIGTERM to self: %v", err)
	}
	select {
	case exitCode := <-exitCh:
		if exitCode != 0 {
			t.Errorf("run() returned exit code %d after SIGTERM; want 0 (clean signal-driven shutdown)", exitCode)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("run() did not return within 30s of SIGTERM — the work-loop shutdown path is hung")
	}
}

// TestRunTmuxUnusable_TmuxSubstrateStillRefuses is the other half of the
// 2026-07-28 change, and the assertion that keeps it honest: removing the
// $TMUX-unset guard must NOT make the daemon boot when tmux is genuinely
// unusable AND the tmux substrate is the one being wired. Every agent spawn on
// that substrate routes through `tmux new-window`, so booting would succeed and
// then fail on the first dispatch — the one failure mode this work must avoid.
//
// tmux is made unusable by emptying $PATH, which is what the daemon actually
// depends on (the tmux BINARY), rather than the ambient $TMUX client it used to
// demand. This is the run()-level companion to the reporter-level
// TestReportNoTmuxHosting_TmuxSubstrateIsFatal, and it replaces the refusal
// coverage that deleting TestRunTmuxEnvFastFail would otherwise have lost.
//
// Not parallel: calls run() (flag.CommandLine) and mutates env.
func TestRunTmuxUnusable_TmuxSubstrateStillRefuses(t *testing.T) {
	mainFixtureResetFlags(t)
	mainFixtureSaveRestoreEnv(t, "TMUX", "", true /* unset */)
	mainFixtureSaveRestoreEnv(t, "HARMONIK_SUBSTRATE", "", true /* unset */)
	mainFixtureSaveRestoreEnv(t, "PATH", "", false /* set */)

	mainFixtureSaveRestoreArgs(t, []string{"harmonik", "start", "daemon", "--project", t.TempDir()})

	exitCh := make(chan int, 1)
	go func() { exitCh <- run() }()

	select {
	case exitCode := <-exitCh:
		if exitCode != 1 {
			t.Errorf("run() with no tmux binary and the tmux substrate selected: got exit code %d, want 1", exitCode)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("run() did not return with no tmux binary — it must refuse rather than boot into a daemon whose every dispatch would fail")
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

	projectDir := t.TempDir()
	mainFixtureSaveRestoreArgs(t, []string{"harmonik", "start", "daemon", "--project", projectDir})

	defer func() {
		if r := recover(); r != nil {
			t.Errorf("run() with TMUX set panicked: %v", r)
		}
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM)
	t.Cleanup(func() { signal.Stop(sigCh) })

	exitCh := make(chan int, 1)
	go func() {
		exitCh <- run()
	}()

	time.Sleep(250 * time.Millisecond)

	select {
	case exitCode := <-exitCh:
		t.Logf("run() returned exit code %d before signal delivery (tmux-absent fast path)", exitCode)
		return
	default:
	}

	if err := syscall.Kill(syscall.Getpid(), syscall.SIGTERM); err != nil {
		t.Fatalf("failed to deliver SIGTERM to self: %v", err)
	}

	select {
	case exitCode := <-exitCh:
		t.Logf("run() returned exit code %d after SIGTERM", exitCode)
	case <-time.After(30 * time.Second):
		t.Fatal("run() did not return within 30s of SIGTERM — the substrate/work-loop shutdown path is hung (hk-9ruez regression)")
	}
}

// TestRunBeadSubcommand_MissingBeadID verifies that `harmonik run` with no
// positional arguments returns exit code 1 and does not panic.
//
// Acceptance: hk-icecw — "harmonik run nonexistent returns a clear error and
// non-zero exit".
//
// Parallel: safe — runBeadSubcommand does not touch flag.CommandLine.
func TestRunBeadSubcommand_MissingBeadID(t *testing.T) {
	t.Parallel()

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

	binDir := t.TempDir()
	fakeTmux := filepath.Join(binDir, "tmux")
	//nolint:gosec // G306: executable mode is required for the fake tmux script fixture
	if err := os.WriteFile(fakeTmux, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write fake tmux: %v", err)
	}
	mainFixtureSaveRestoreEnv(t, "PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"), false)

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
		t.Errorf("commitHash = %q; want %q (unstamped build sentinel)", commitHash, want)
	}
}

// TestVersionVar_DefaultIsDev verifies the package-level version variable
// declared in version.go has the sentinel value "dev" in an unstamped build
// (i.e., when -ldflags "-X main.version=v0.y.z" is NOT passed at build time).
//
// Acceptance: specs/release-pipeline.md §2.3 — version default is "dev".
//
// Parallel: safe — reads a package-level variable but does not modify it.
func TestVersionVar_DefaultIsDev(t *testing.T) {
	t.Parallel()

	const want = "dev"
	if version != want {
		t.Errorf("version = %q; want %q (unstamped build sentinel)", version, want)
	}
}

// TestVersionSubcommand_ExitsZeroAndPrintsVersionLine is a smoke test that
// verifies `harmonik version` exits 0 and prints a line matching the normative
// format from specs/release-pipeline.md §2.3:
//
//	harmonik <version> (commit: <commitHash>)
//
// The version subcommand is dispatched before flag.Parse, so this test does
// not need mainFixtureResetFlags. It MUST NOT be parallel because it modifies
// os.Args and os.Stdout.
//
// Acceptance: bead hk-ww7ee — `harmonik version` exits 0 and prints version line.
func TestVersionSubcommand_ExitsZeroAndPrintsVersionLine(t *testing.T) {
	mainFixtureSaveRestoreArgs(t, []string{"harmonik", "version"})

	origStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	os.Stdout = w
	t.Cleanup(func() { os.Stdout = origStdout })

	exitCode := run()

	if err := w.Close(); err != nil {
		t.Fatalf("close stdout writer: %v", err)
	}

	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatalf("copy captured stdout: %v", err)
	}
	if err := r.Close(); err != nil {
		t.Fatalf("close stdout reader: %v", err)
	}

	if exitCode != 0 {
		t.Errorf("run() with 'version' subcommand: exit code %d, want 0", exitCode)
	}

	got := strings.TrimSpace(buf.String())
	if !strings.HasPrefix(got, "harmonik ") {
		t.Errorf("version output %q does not start with %q", got, "harmonik ")
	}
	if !strings.Contains(got, "(commit: ") {
		t.Errorf("version output %q missing %q", got, "(commit: ")
	}
}

// TestVersionFlagLong_ExitsZero verifies that `harmonik --version` is handled
// identically to `harmonik version` — exits 0 without entering flag.Parse.
//
// Not parallel: modifies os.Args and os.Stdout.
//
// Acceptance: bead hk-ww7ee — --version flag alias exits 0.
func TestVersionFlagLong_ExitsZero(t *testing.T) {
	mainFixtureSaveRestoreArgs(t, []string{"harmonik", "--version"})

	origStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	os.Stdout = w
	drained := make(chan struct{})
	go func() {
		defer close(drained)
		//nolint:errcheck // a drain that fails still ends the goroutine; nothing asserts on the bytes
		_, _ = io.Copy(io.Discard, r)
	}()
	t.Cleanup(func() {
		os.Stdout = origStdout
		_ = w.Close() // releases the drain goroutine
		<-drained
		_ = r.Close()
	})

	exitCode := run()

	if exitCode != 0 {
		t.Errorf("run() with '--version' flag: exit code %d, want 0", exitCode)
	}
}
