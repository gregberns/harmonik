package daemon_test

// pasteinject_hktvy3e_test.go — regression pin for hk-tvy3e: post-quit
// watchdog must fire Kill even when the per-run ctx is cancelled before the
// grace period elapses.
//
// # Bug context
//
// Two production runs (hk-vm8ym, hk-mpel5) committed real work but their
// claude-code sessions never auto-exited. implementer_phase_complete never
// fired, holding local concurrency slots for ~55 minutes. The daemon's
// synthetic 5-minute heartbeat extended the stale timer, masking the hang as
// "alive, just long." The captain recovered by sending /quit manually.
//
// The post-quit watchdog goroutine in pasteInjectQuitOnCommit previously had:
//
//   select {
//   case <-ctx.Done(): return   // ← exits WITHOUT Kill if ctx cancelled
//   case <-time.After(grace):
//   }
//   killer.Kill(ctx)            // ← may fail if ctx already cancelled
//
// If the per-run ctx is cancelled before the grace timer fires (e.g. stale-
// watcher timeout, daemon shutdown), the goroutine exits without calling Kill.
// Kill(ctx) would also fail if the ctx were already cancelled by the time the
// timer fires. Both paths leave sess.Wait blocked.
//
// # Fix (hk-tvy3e)
//
// Remove the ctx.Done escape hatch and use context.Background() for Kill:
//
//   <-time.After(grace)
//   killer.Kill(context.Background())
//
// Kill is idempotent (killOnce guard in the production substrate), so firing
// it unconditionally is safe.
//
// # What this test asserts
//
// pasteInjectQuitOnCommit detects a commit, sends /quit, launches the kill
// goroutine, and returns. The test then cancels the per-run ctx immediately,
// simulating an early ctx cancellation. Kill MUST still be called within the
// grace window.
//
// Helper prefix: hktvy3e — per implementer-protocol §Helper-prefix discipline.
// Bead: hk-tvy3e.

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/daemon"
)

// ─────────────────────────────────────────────────────────────────────────────
// Stubs
// ─────────────────────────────────────────────────────────────────────────────

// hktvy3eQuitSender is a minimal no-op quitSender.
type hktvy3eQuitSender struct{}

func (q *hktvy3eQuitSender) SendQuitToLastPane(_ context.Context) error { return nil }

// hktvy3eKiller closes killedCh on the first Kill call so the test can
// assert the watchdog fired.
type hktvy3eKiller struct {
	killedCh chan struct{}
	once     sync.Once
}

func newHktvy3eKiller() *hktvy3eKiller {
	return &hktvy3eKiller{killedCh: make(chan struct{})}
}

func (k *hktvy3eKiller) Kill(_ context.Context) error {
	k.once.Do(func() { close(k.killedCh) })
	return nil
}

// ─────────────────────────────────────────────────────────────────────────────
// Fixtures
// ─────────────────────────────────────────────────────────────────────────────

func hktvy3eProjectDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		//nolint:gosec // G204: git args are test-internal literals
		cmd := exec.CommandContext(t.Context(), "git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("hktvy3eProjectDir: git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "--initial-branch=main")
	run("config", "user.email", "test@harmonik.local")
	run("config", "user.name", "Harmonik Test")
	if err := os.WriteFile(filepath.Join(dir, "README"), []byte("hk-tvy3e\n"), 0o644); err != nil {
		t.Fatalf("hktvy3eProjectDir: WriteFile: %v", err)
	}
	run("add", "README")
	run("commit", "-m", "init")
	return dir
}

func hktvy3eWorktree(t *testing.T, projectDir string) (wtPath, parentSHA string) {
	t.Helper()
	headCmd := exec.CommandContext(t.Context(), "git", "rev-parse", "HEAD")
	headCmd.Dir = projectDir
	out, err := headCmd.Output()
	if err != nil {
		t.Fatalf("hktvy3eWorktree: git rev-parse HEAD: %v", err)
	}
	parentSHA = strings.TrimSuffix(string(out), "\n")

	wtDir := t.TempDir()
	wtPath = filepath.Join(wtDir, "wt")
	//nolint:gosec // G204: git args are test-internal
	addCmd := exec.CommandContext(t.Context(), "git", "worktree", "add", "--detach", wtPath, parentSHA)
	addCmd.Dir = projectDir
	if out, err := addCmd.CombinedOutput(); err != nil {
		t.Fatalf("hktvy3eWorktree: git worktree add: %v\n%s", err, out)
	}
	//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
	if err := os.MkdirAll(filepath.Join(wtPath, ".harmonik"), 0o755); err != nil {
		t.Fatalf("hktvy3eWorktree: mkdir .harmonik: %v", err)
	}
	t.Cleanup(func() {
		//nolint:gosec // G204: git args are test-internal
		rmCmd := exec.Command("git", "worktree", "remove", "--force", "--force", wtPath)
		rmCmd.Dir = projectDir
		_ = rmCmd.Run()
	})
	return wtPath, parentSHA
}

// hktvy3eCommit writes a file and commits it in the worktree so HEAD changes.
func hktvy3eCommit(t *testing.T, wtPath string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(wtPath, "impl.txt"), []byte("hk-tvy3e impl\n"), 0o644); err != nil {
		t.Fatalf("hktvy3eCommit: WriteFile: %v", err)
	}
	run := func(args ...string) {
		t.Helper()
		//nolint:gosec // G204: git args are test-internal
		cmd := exec.CommandContext(t.Context(), "git", args...)
		cmd.Dir = wtPath
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("hktvy3eCommit: git %v: %v\n%s", args, err, out)
		}
	}
	run("add", "impl.txt")
	run("-c", "user.email=test@harmonik.local",
		"-c", "user.name=Test",
		"commit", "-m", "hk-tvy3e impl", "--no-gpg-sign")
}

// ─────────────────────────────────────────────────────────────────────────────
// Test
// ─────────────────────────────────────────────────────────────────────────────

// TestPostQuitWatchdog_IgnoresCtxCancel_hktvy3e verifies that the post-commit
// Kill watchdog fires even when the per-run ctx is cancelled before the grace
// period elapses.
//
// Setup:
//  1. A commit is made in the worktree before calling pasteInjectQuitOnCommit,
//     so the function detects the commit on its first poll and launches the
//     kill goroutine, then returns.
//  2. The per-run ctx is cancelled immediately after the function returns,
//     simulating the failure mode: ctx cancelled while the kill goroutine is
//     still waiting for the grace period.
//  3. Kill MUST be called within 2s (well past the 300ms test grace). With the
//     BUG the goroutine exits via ctx.Done and Kill is never called.
//
// Bead: hk-tvy3e.
func TestPostQuitWatchdog_IgnoresCtxCancel_hktvy3e(t *testing.T) {
	// Short grace so the test completes fast.
	// NOT t.Parallel() — modifies package-level vars.
	origGrace := *daemon.ExportedPostQuitKillGrace
	origPoll := *daemon.ExportedCommitPollInterval
	*daemon.ExportedPostQuitKillGrace = 300 * time.Millisecond
	*daemon.ExportedCommitPollInterval = 20 * time.Millisecond
	defer func() {
		// Sleep briefly so the kill goroutine's time.After(grace) has
		// resolved before we restore the package vars, avoiding a race
		// between the goroutine's timer and the restore.
		time.Sleep(200 * time.Millisecond)
		*daemon.ExportedPostQuitKillGrace = origGrace
		*daemon.ExportedCommitPollInterval = origPoll
	}()

	projectDir := hktvy3eProjectDir(t)
	wtPath, parentSHA := hktvy3eWorktree(t, projectDir)

	// Commit BEFORE calling pasteInjectQuitOnCommit so HEAD != parentSHA.
	// The function detects this on its first poll (20ms) and launches the
	// kill goroutine, then returns immediately.
	hktvy3eCommit(t, wtPath)

	qs := &hktvy3eQuitSender{}
	killer := newHktvy3eKiller()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	noChangeCh := make(chan struct{}, 1)

	done := make(chan struct{})
	go func() {
		defer close(done)
		daemon.ExportedPasteInjectQuitOnCommit(
			ctx, qs, killer,
			wtPath, parentSHA,
			noChangeCh, nil, nil,
		)
	}()

	// Wait for pasteInjectQuitOnCommit to return — it returns as soon as it
	// detects the commit (after launching the kill goroutine).
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("hk-tvy3e: pasteInjectQuitOnCommit did not return within 5s after commit")
	}

	// Cancel the per-run ctx now. The kill goroutine is still waiting for
	// its grace period (300ms). With the BUG, ctx.Done() fires first and
	// the goroutine exits WITHOUT calling Kill. With the FIX, the goroutine
	// ignores ctx and always fires Kill after the grace period.
	cancel()

	// Kill MUST be called within 2s (well past the 300ms grace period).
	select {
	case <-killer.killedCh:
		// PASS: Kill was called despite the cancelled ctx (hk-tvy3e fix working).
	case <-time.After(2 * time.Second):
		t.Error("hk-tvy3e: Kill NOT called after grace period — " +
			"ctx cancellation incorrectly blocked the post-quit watchdog; " +
			"fix missing?")
	}
}
