package daemon_test

// t3_exploratory_test.go — Tester T3 daemon lifecycle boundary cases.
//
// Tests:
//   T3-01: Double invocation with same project dir — second must fail with ErrPidfileLocked.
//   T3-02: SIGINT mid-run — in-flight bead ReopenBead'd; worktree left behind (known gap hk-fgdgz).
//   T3-03: SIGTERM mid-run — same questions as T3-02.
//   T3-04: Stale pidfile (process dead) — second daemon should acquire lock.
//   T3-05: Stale worktree on disk — orphan sweep cleans stale lease-lock files.
//
// Helper prefix: t3Fixture (per implementer-protocol §Helper-prefix discipline).
//
// These tests are NOT parallel: several send SIGINT to the test process, which
// interferes with signal.NotifyContext in parallel tests.

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/brcli"
	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/lifecycle"
	"github.com/gregberns/harmonik/internal/workspace"
)

// ─────────────────────────────────────────────────────────────────────────────
// Shared fixture helpers
// ─────────────────────────────────────────────────────────────────────────────

// t3FixtureProjectDir creates a minimal project dir with .harmonik/events/,
// .harmonik/beads-intents/ sub-dirs. Returns (projectDir, jsonlPath).
func t3FixtureProjectDir(t *testing.T) (string, string) {
	t.Helper()
	projectDir := t.TempDir()
	for _, sub := range []string{
		filepath.Join(".harmonik", "events"),
		filepath.Join(".harmonik", "beads-intents"),
	} {
		//nolint:gosec // G301: test-only temp directory; not production
		if err := os.MkdirAll(filepath.Join(projectDir, sub), 0o755); err != nil {
			t.Fatalf("t3FixtureProjectDir: mkdir %s: %v", sub, err)
		}
	}
	return projectDir, filepath.Join(projectDir, ".harmonik", "events", "events.jsonl")
}

// t3FixtureGitRepo initialises a minimal git repo with one commit.
func t3FixtureGitRepo(t *testing.T, dir string) {
	t.Helper()
	run := func(args ...string) {
		t.Helper()
		//nolint:gosec // G204: git args are test-internal literals; not user input
		cmd := exec.CommandContext(t.Context(), "git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("t3FixtureGitRepo: git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "--initial-branch=main")
	run("config", "user.email", "test@harmonik.local")
	run("config", "user.name", "Harmonik Test")
	initPath := filepath.Join(dir, "README")
	if err := os.WriteFile(initPath, []byte("t3 test repo\n"), 0o644); err != nil {
		t.Fatalf("t3FixtureGitRepo: WriteFile: %v", err)
	}
	run("add", "README")
	run("commit", "-m", "Initial commit")
}

// t3FixtureBrPath locates `br` or skips the test.
func t3FixtureBrPath(t *testing.T) string {
	t.Helper()
	p, err := exec.LookPath("br")
	if err != nil {
		t.Skip("br required (not on PATH)")
	}
	return p
}

// t3FixtureBrWrapper writes a wrapper script that prepends --db <dbPath> to all br args.
func t3FixtureBrWrapper(t *testing.T, realBrPath, dbPath string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "br")
	content := "#!/bin/sh\nexec " + realBrPath + " --db " + dbPath + " \"$@\"\n"
	//nolint:gosec // G306: script is test-only; chmod 0755 required for execution
	if err := os.WriteFile(p, []byte(content), 0o755); err != nil {
		t.Fatalf("t3FixtureBrWrapper: WriteFile: %v", err)
	}
	return p
}

// t3FixtureSlowHandlerScript writes a /bin/sh script that sleeps 30 s then exits 0.
// Used for signal tests where we need the handler to be in-flight when the signal arrives.
func t3FixtureSlowHandlerScript(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "slow-handler.sh")
	//nolint:gosec // G306: script is test-only; chmod 0755 required for execution
	if err := os.WriteFile(p, []byte("#!/bin/sh\nsleep 30\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("t3FixtureSlowHandlerScript: WriteFile: %v", err)
	}
	return p
}

// t3FixtureFastHandlerScript writes a /bin/sh script that exits 0 immediately.
func t3FixtureFastHandlerScript(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "fast-handler.sh")
	//nolint:gosec // G306: script is test-only; chmod 0755 required for execution
	if err := os.WriteFile(p, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("t3FixtureFastHandlerScript: WriteFile: %v", err)
	}
	return p
}

// t3FixtureInitBr initialises a beads workspace in projectDir and returns a ready bead ID.
func t3FixtureInitBr(t *testing.T, realBrPath, projectDir, brWrapper string) string {
	t.Helper()
	//nolint:gosec // G204: br args are test-internal literals; not user input
	initCmd := exec.CommandContext(t.Context(), realBrPath, "init", "--prefix", "t3")
	initCmd.Dir = projectDir
	if out, err := initCmd.CombinedOutput(); err != nil {
		t.Fatalf("t3FixtureInitBr: br init: %v\n%s", err, out)
	}
	//nolint:gosec // G204: br args are test-internal literals; not user input
	createCmd := exec.CommandContext(t.Context(), brWrapper, "create", "t3 test bead", "--status", "open", "--silent")
	out, err := createCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("t3FixtureInitBr: br create: %v\n%s", err, out)
	}
	id := strings.TrimSpace(string(out))
	if id == "" {
		t.Fatal("t3FixtureInitBr: br create returned empty ID")
	}
	return id
}

// t3FixtureMarkBeadReady marks a bead as ready via `br update`.
func t3FixtureMarkBeadReady(t *testing.T, brWrapper, beadID string) {
	t.Helper()
	//nolint:gosec // G204: br args are test-internal literals; not user input
	cmd := exec.CommandContext(t.Context(), brWrapper, "update", beadID, "--status", "ready")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("t3FixtureMarkBeadReady: br update %s --status ready: %v\n%s", beadID, err, out)
	}
}

// t3FixtureBeadStatus returns the current status string for a bead.
func t3FixtureBeadStatus(t *testing.T, brWrapper, beadID string) string {
	t.Helper()
	//nolint:gosec // G204: br args are test-internal literals; not user input
	cmd := exec.CommandContext(t.Context(), brWrapper, "show", beadID, "--format", "json")
	out, err := cmd.Output()
	if err != nil {
		return "unknown"
	}
	var items []struct {
		Status string `json:"status"`
	}
	if jsonErr := json.Unmarshal(out, &items); jsonErr == nil && len(items) == 1 {
		return items[0].Status
	}
	return "unknown"
}

// t3FixtureReadJSONLLines returns all non-empty lines from a JSONL file.
func t3FixtureReadJSONLLines(t *testing.T, path string) []string {
	t.Helper()
	//nolint:gosec // G304: path is t.TempDir()-based; not user input
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer func() { _ = f.Close() }()
	var lines []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if l := strings.TrimSpace(sc.Text()); l != "" {
			lines = append(lines, l)
		}
	}
	return lines
}

// t3FixturePidfileExists returns true if the daemon pidfile exists under projectDir.
func t3FixturePidfileExists(projectDir string) bool {
	_, err := os.Stat(filepath.Join(projectDir, ".harmonik", "daemon.pid"))
	return err == nil
}

// ─────────────────────────────────────────────────────────────────────────────
// T3-01: Double invocation — second must fail with ErrPidfileLocked
// ─────────────────────────────────────────────────────────────────────────────

// TestT3_DoubleInvocation verifies that a second daemon.Start on the same
// project directory fails immediately with an error wrapping ErrPidfileLocked
// while the first daemon is running.
func TestT3_DoubleInvocation(t *testing.T) {
	realBr := t3FixtureBrPath(t)
	projectDir, jsonlPath := t3FixtureProjectDir(t)
	t3FixtureGitRepo(t, projectDir)

	dbPath := filepath.Join(projectDir, ".beads", "beads.db")
	brWrapper := t3FixtureBrWrapper(t, realBr, dbPath)
	slowHandler := t3FixtureSlowHandlerScript(t)

	// Create bead with status "open" — brcli.Ready() returns open beads.
	// Do NOT call t3FixtureMarkBeadReady; that transitions to "ready" status
	// which removes the bead from br ready output (wrong).
	beadID := t3FixtureInitBr(t, realBr, projectDir, brWrapper)
	t.Logf("T3-01: seeded bead %s", beadID)

	cfg := daemon.Config{
		ProjectDir:    projectDir,
		JSONLLogPath:  jsonlPath,
		BrPath:        brWrapper,
		HandlerBinary: slowHandler,
	}

	// Start daemon 1 in background — it will pick up the bead and block on slow handler.
	// ctx cancellation is the stop mechanism (hk-i4mtq: converted from syscall.Kill self-signal).
	d1Ctx, d1Cancel := context.WithCancel(context.Background())
	defer d1Cancel()
	d1Done := make(chan error, 1)
	go func() { d1Done <- daemon.Start(d1Ctx, cfg) }()

	// Give daemon 1 time to acquire the pidfile and claim the bead.
	time.Sleep(500 * time.Millisecond)

	// Confirm pidfile exists.
	if !t3FixturePidfileExists(projectDir) {
		t.Error("T3-01: pidfile not created by daemon 1 after 500ms")
	}

	// Attempt daemon 2 — must fail with ErrPidfileLocked.
	d2Err := daemon.Start(context.Background(), cfg)
	t.Logf("T3-01: daemon 2 error: %v", d2Err)

	if d2Err == nil {
		t.Error("T3-01: FINDING — second daemon.Start returned nil (should have returned ErrPidfileLocked); two daemons may be running concurrently on the same project dir")
	} else if !errors.Is(d2Err, lifecycle.ErrPidfileLocked) {
		t.Errorf("T3-01: FINDING — second daemon.Start returned %v (want ErrPidfileLocked); double-check error wrapping", d2Err)
	} else {
		t.Logf("T3-01: PASS — second daemon correctly rejected with ErrPidfileLocked")
	}

	// Stop daemon 1 via context cancellation (replaces syscall.Kill self-signal per hk-i4mtq).
	// Testing ctx cancellation IS testing the same code path as SIGINT: the production caller
	// (cmd/harmonik/main.go) translates SIGINT → ctx.Done via signal.NotifyContext.
	d1Cancel()
	select {
	case err := <-d1Done:
		t.Logf("T3-01: daemon 1 stopped: %v", err)
	case <-time.After(5 * time.Second):
		t.Error("T3-01: daemon 1 did not stop within 5s after cancel")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// T3-02: SIGINT mid-run
// ─────────────────────────────────────────────────────────────────────────────

// TestT3_SIGINTMidRun starts a daemon with a slow handler (30 s sleep), waits
// until the bead is in-flight (claimed), then sends SIGINT and verifies:
//
//	a. daemon.Start returns nil (clean shutdown).
//	b. The bead is reopened (not left in "claimed" state).
//	c. The worktree is left behind (known gap hk-fgdgz — record actual behaviour).
func TestT3_SIGINTMidRun(t *testing.T) {
	realBr := t3FixtureBrPath(t)
	projectDir, jsonlPath := t3FixtureProjectDir(t)
	t3FixtureGitRepo(t, projectDir)

	dbPath := filepath.Join(projectDir, ".beads", "beads.db")
	brWrapper := t3FixtureBrWrapper(t, realBr, dbPath)
	slowHandler := t3FixtureSlowHandlerScript(t)

	// Create bead with status "open" — brcli.Ready() returns open beads.
	beadID := t3FixtureInitBr(t, realBr, projectDir, brWrapper)
	t.Logf("T3-02: seeded bead %s", beadID)

	cfg := daemon.Config{
		ProjectDir:    projectDir,
		JSONLLogPath:  jsonlPath,
		BrPath:        brWrapper,
		HandlerBinary: slowHandler,
	}

	// ctx cancellation replaces syscall.Kill self-signal (hk-i4mtq). Testing ctx
	// cancellation IS testing the SIGINT code path: production main.go translates
	// SIGINT → ctx.Done via signal.NotifyContext.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- daemon.Start(ctx, cfg) }()

	// Wait for bead to be claimed (status transitions from "open" to "in_progress").
	// Work loop poll interval is 2s; allow 15s to claim.
	deadline := time.Now().Add(15 * time.Second)
	claimed := false
	for time.Now().Before(deadline) {
		st := t3FixtureBeadStatus(t, brWrapper, beadID)
		if st == "in_progress" {
			t.Logf("T3-02: bead %s is now in_progress (handler in-flight)", beadID)
			claimed = true
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if !claimed {
		t.Logf("T3-02: bead not claimed within 15s (bead status: %s); cancelling anyway", t3FixtureBeadStatus(t, brWrapper, beadID))
	}

	// Give the work loop time to create the worktree and launch the handler
	// AFTER ClaimBead (which set in_progress). The work loop does:
	// ClaimBead → resolveHEAD → CreateWorktree → Emit run_started → Launch.
	// A short wait ensures the slow handler is actually running.
	if claimed {
		time.Sleep(2 * time.Second)
	}

	// Record worktree state before cancellation.
	wtGlob := filepath.Join(projectDir, ".harmonik", "worktrees", "*")
	beforeWTs, _ := filepath.Glob(wtGlob)
	t.Logf("T3-02: worktrees before cancel: %v", beforeWTs)

	// Cancel context to stop the daemon (replaces SIGINT self-signal per hk-i4mtq).
	t.Log("T3-02: cancelling context")
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("T3-02: daemon.Start returned error after cancel: %v", err)
		} else {
			t.Log("T3-02: daemon.Start returned nil (clean)")
		}
	case <-time.After(10 * time.Second):
		t.Error("T3-02: daemon.Start did not return within 10s after cancel")
	}

	// Check bead status after shutdown.
	// Expected: bead should return to "open" (ReopenBead called due to non-zero exit).
	// FINDING if: bead is left in "in_progress" (ReopenBead calls `br reopen` which
	// only works on closed→open; it cannot transition in_progress→open).
	beadStatusAfter := t3FixtureBeadStatus(t, brWrapper, beadID)
	t.Logf("T3-02: bead %s status after cancel+shutdown: %q", beadID, beadStatusAfter)
	switch beadStatusAfter {
	case "open":
		t.Log("T3-02: PASS — bead returned to 'open' after cancel mid-run")
	case "in_progress":
		if claimed {
			t.Log("T3-02: FINDING — bead left in 'in_progress' after cancel mid-run; ReopenBead uses `br reopen` (closed→open) but bead is in_progress; bead stuck — cannot be dispatched on next run")
		} else {
			t.Log("T3-02: NOTE — bead in 'in_progress' but was never confirmed claimed (timing); manual investigation needed")
		}
	case "closed":
		t.Log("T3-02: NOTE — bead was closed (handler finished before cancel was processed)")
	default:
		t.Logf("T3-02: NOTE — bead in unexpected status %q after cancel", beadStatusAfter)
	}

	// Record worktree state after shutdown — expected: worktree left behind (known gap hk-fgdgz).
	afterWTs, _ := filepath.Glob(wtGlob)
	t.Logf("T3-02: worktrees after cancel: %v", afterWTs)
	if len(afterWTs) > 0 {
		t.Logf("T3-02: FINDING (expected gap hk-fgdgz) — worktree(s) left behind after cancel: %v", afterWTs)
	} else if claimed {
		t.Log("T3-02: NOTE — no worktree found after cancel; check workspace.WorktreePath for correct glob pattern")
	}

	// Check JSONL for run events.
	lines := t3FixtureReadJSONLLines(t, jsonlPath)
	t.Logf("T3-02: JSONL line count = %d", len(lines))
}

// ─────────────────────────────────────────────────────────────────────────────
// T3-03: SIGTERM mid-run
// ─────────────────────────────────────────────────────────────────────────────

// TestT3_SIGTERMMidRun is the same scenario as T3-02 but simulates a SIGTERM-driven
// shutdown. With ctx-based stop (hk-i4mtq), both T3-02 and T3-03 cancel the context;
// the distinction between SIGINT and SIGTERM is now solely in cmd/harmonik/main.go
// which translates both signals to ctx.Done via signal.NotifyContext. Testing ctx
// cancellation here covers the same work-loop code path as either signal.
func TestT3_SIGTERMMidRun(t *testing.T) {
	realBr := t3FixtureBrPath(t)
	projectDir, jsonlPath := t3FixtureProjectDir(t)
	t3FixtureGitRepo(t, projectDir)

	dbPath := filepath.Join(projectDir, ".beads", "beads.db")
	brWrapper := t3FixtureBrWrapper(t, realBr, dbPath)
	slowHandler := t3FixtureSlowHandlerScript(t)

	// Create bead with status "open" — brcli.Ready() returns open beads.
	beadID := t3FixtureInitBr(t, realBr, projectDir, brWrapper)
	t.Logf("T3-03: seeded bead %s", beadID)

	cfg := daemon.Config{
		ProjectDir:    projectDir,
		JSONLLogPath:  jsonlPath,
		BrPath:        brWrapper,
		HandlerBinary: slowHandler,
	}

	// ctx cancellation replaces syscall.Kill self-signal (hk-i4mtq).
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- daemon.Start(ctx, cfg) }()

	// Wait for bead to be claimed (status → in_progress).
	deadline := time.Now().Add(15 * time.Second)
	claimed := false
	for time.Now().Before(deadline) {
		st := t3FixtureBeadStatus(t, brWrapper, beadID)
		if st == "in_progress" {
			t.Logf("T3-03: bead %s is now in_progress (handler in-flight)", beadID)
			claimed = true
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if !claimed {
		t.Logf("T3-03: bead not claimed within 15s (bead status: %s); cancelling anyway", t3FixtureBeadStatus(t, brWrapper, beadID))
	}

	// Give the work loop time to create the worktree and launch the slow handler.
	if claimed {
		time.Sleep(2 * time.Second)
	}

	wtGlob := filepath.Join(projectDir, ".harmonik", "worktrees", "*")
	beforeWTs, _ := filepath.Glob(wtGlob)
	t.Logf("T3-03: worktrees before cancel: %v", beforeWTs)

	// Cancel context (replaces SIGTERM self-signal per hk-i4mtq).
	t.Log("T3-03: cancelling context")
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("T3-03: daemon.Start returned error after cancel: %v", err)
		} else {
			t.Log("T3-03: daemon.Start returned nil (clean)")
		}
	case <-time.After(10 * time.Second):
		t.Error("T3-03: daemon.Start did not return within 10s after cancel")
	}

	beadStatusAfter := t3FixtureBeadStatus(t, brWrapper, beadID)
	t.Logf("T3-03: bead %s status after cancel+shutdown: %q", beadID, beadStatusAfter)
	switch beadStatusAfter {
	case "open":
		t.Log("T3-03: PASS — bead returned to 'open' after cancel mid-run")
	case "in_progress":
		if claimed {
			t.Log("T3-03: FINDING — bead left in 'in_progress' after cancel mid-run; same root cause as T3-02 (ReopenBead uses closed→open semantics)")
		} else {
			t.Log("T3-03: NOTE — bead in 'in_progress' but claim not confirmed; timing issue")
		}
	case "closed":
		t.Log("T3-03: NOTE — bead was closed (handler finished before cancel was processed)")
	default:
		t.Logf("T3-03: NOTE — bead in unexpected status %q after cancel", beadStatusAfter)
	}

	afterWTs, _ := filepath.Glob(wtGlob)
	t.Logf("T3-03: worktrees after cancel: %v", afterWTs)
	if len(afterWTs) > 0 {
		t.Logf("T3-03: FINDING (expected gap hk-fgdgz) — worktree(s) left behind after cancel: %v", afterWTs)
	}
	lines := t3FixtureReadJSONLLines(t, jsonlPath)
	t.Logf("T3-03: JSONL line count = %d", len(lines))
}

// ─────────────────────────────────────────────────────────────────────────────
// T3-04: Stale pidfile (PID no longer exists) — second daemon must acquire
// ─────────────────────────────────────────────────────────────────────────────

// TestT3_StalePidfile verifies that after a daemon crashes (process terminates
// without releasing the pidfile), a new daemon invocation detects the stale
// pidfile via ProbePidfileLock, removes it, and acquires the lock successfully.
//
// Mechanism: We call AcquirePidfile with a deliberately dead PID (PID 1 on
// macOS is launchd, which is live, so we use a synthetic dead PID = 99999999
// which is guaranteed to not exist). The pidfile is written directly to simulate
// a crash residue. Then daemon.Start is called and must succeed.
func TestT3_StalePidfile(t *testing.T) {
	projectDir, jsonlPath := t3FixtureProjectDir(t)
	t3FixtureGitRepo(t, projectDir)

	// Write a stale pidfile manually: dead PID, dead PGID, stale instance ID.
	// We use PID 99999999 which is far above any real macOS or Linux PID limit.
	const stalePID = 99999999
	const stalePGID = 99999998
	pidfilePath := filepath.Join(projectDir, ".harmonik", "daemon.pid")
	pidfileContent := []byte("99999999\n99999998\nstale-instance-dead\n")
	if err := os.WriteFile(pidfilePath, pidfileContent, 0o600); err != nil {
		t.Fatalf("T3-04: write stale pidfile: %v", err)
	}
	t.Logf("T3-04: wrote stale pidfile with pid=%d pgid=%d", stalePID, stalePGID)

	// Verify the pidfile is there and not flock-held (no live process holding it).
	status, probedPID, probeErr := lifecycle.ProbePidfileLock(projectDir)
	t.Logf("T3-04: ProbePidfileLock → status=%d pid=%d err=%v", status, probedPID, probeErr)

	// Now call daemon.Start with no BrPath so it acquires pidfile and returns immediately.
	cfg := daemon.Config{
		ProjectDir:   projectDir,
		JSONLLogPath: jsonlPath,
		BrPath:       "", // no work loop; just test pidfile acquisition
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- daemon.Start(ctx, cfg) }()

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("T3-04: FINDING — daemon.Start failed on stale pidfile: %v (should have recovered and acquired)", err)
		} else {
			t.Log("T3-04: PASS — daemon.Start succeeded after stale pidfile")
		}
	case <-time.After(5 * time.Second):
		t.Error("T3-04: daemon.Start did not return within 5s (hung?)")
		cancel() // emergency stop; replaces syscall.Kill self-signal per hk-i4mtq
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// T3-05: Stale worktree on disk — orphan sweep should clean lease-lock files
// ─────────────────────────────────────────────────────────────────────────────

// TestT3_StaleWorktreeOrphanSweep verifies that when a stale worktree lease-lock
// file is present (left by a crashed daemon), the next daemon startup's orphan
// sweep removes it and emits daemon_orphan_sweep_completed with locks_cleared > 0.
func TestT3_StaleWorktreeOrphanSweep(t *testing.T) {
	projectDir, jsonlPath := t3FixtureProjectDir(t)
	t3FixtureGitRepo(t, projectDir)

	// Seed a stale lease-lock file by creating a worktree directory and
	// its corresponding lease-lock file as if a previous daemon crashed mid-run.
	staleRunID := "t3-stale-run-0000000000000000000000000000"
	wtPath := workspace.WorktreePath(projectDir, staleRunID, workspace.NoWorktreeRootOverride())
	//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
	if err := os.MkdirAll(wtPath, 0o755); err != nil {
		t.Fatalf("T3-05: mkdir stale worktree: %v", err)
	}

	// Write a stale lease-lock file to the expected location.
	// workspace.SweepStaleLeaseLocks looks for .harmonik/worktrees/<runID>/.lease-lock
	lockPath := filepath.Join(wtPath, ".lease-lock")
	if err := os.WriteFile(lockPath, []byte("stale-lock\n"), 0o600); err != nil {
		t.Fatalf("T3-05: write stale lease-lock: %v", err)
	}
	t.Logf("T3-05: seeded stale lease-lock at %s", lockPath)

	cfg := daemon.Config{
		ProjectDir:   projectDir,
		JSONLLogPath: jsonlPath,
		BrPath:       "", // no work loop; just orphan sweep on startup
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- daemon.Start(ctx, cfg) }()

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("T3-05: daemon.Start failed: %v", err)
		} else {
			t.Log("T3-05: daemon.Start returned successfully")
		}
	case <-time.After(5 * time.Second):
		t.Error("T3-05: daemon.Start hung for 5s")
		cancel() // emergency stop; replaces syscall.Kill self-signal per hk-i4mtq
	}

	// Check JSONL for daemon_orphan_sweep_completed with locks_cleared > 0.
	lines := t3FixtureReadJSONLLines(t, jsonlPath)
	t.Logf("T3-05: JSONL lines: %d", len(lines))

	foundSweep := false
	for _, line := range lines {
		if strings.Contains(line, "daemon_orphan_sweep_completed") || strings.Contains(line, "orphan_sweep_completed") {
			foundSweep = true
			t.Logf("T3-05: sweep event: %s", line)
			// Parse for locks_cleared.
			var ev map[string]interface{}
			if json.Unmarshal([]byte(line), &ev) == nil {
				if payload, ok := ev["payload"]; ok {
					if pl, ok2 := payload.(map[string]interface{}); ok2 {
						if lc, ok3 := pl["locks_cleared"]; ok3 {
							t.Logf("T3-05: locks_cleared = %v", lc)
							if lc.(float64) > 0 {
								t.Log("T3-05: PASS — locks_cleared > 0 after stale worktree on disk")
							} else {
								t.Log("T3-05: FINDING — locks_cleared == 0; stale lease-lock may not have been swept")
							}
						}
					}
				}
			}
			break
		}
	}
	if !foundSweep {
		t.Log("T3-05: FINDING — no daemon_orphan_sweep_completed event found in JSONL; event may not be emitted or type mismatch")
	}

	// Verify the lease-lock file was cleaned up.
	if _, err := os.Stat(lockPath); errors.Is(err, os.ErrNotExist) {
		t.Log("T3-05: PASS — stale lease-lock file removed by orphan sweep")
	} else {
		t.Log("T3-05: FINDING — stale lease-lock file still present after startup orphan sweep")
	}

	// Verify pidfile was released.
	if t3FixturePidfileExists(projectDir) {
		t.Log("T3-05: pidfile still on disk (kernel will release flock on process exit)")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// T3-06: Signal behavior — daemon stops but in-flight bead is NOT ReopenBead'd
// (context cancellation happens before wait loop completes)
// ─────────────────────────────────────────────────────────────────────────────

// TestT3_SignalBeforeHandlerLaunch verifies the edge case where SIGINT arrives
// between ClaimBead and handler Launch. The work loop should detect ctx.Done()
// and still call ReopenBead for the claimed bead.
//
// This test uses the stub-adapter path (ExportedWorkLoopDeps / ExportedRunWorkLoop)
// to inject a controlled bead ledger that pauses between claim and launch.
func TestT3_SignalBeforeHandlerLaunch(t *testing.T) {
	projectDir, _ := t3FixtureProjectDir(t)
	t3FixtureGitRepo(t, projectDir)

	const beadID = core.BeadID("t3-claim-signal-test")

	// Stub ledger: Ready returns one bead; ClaimBead records call; ReopenBead records call.
	ledger := &t3StubLedger{
		readyIDs: []core.BeadID{beadID},
	}

	bus := &t3StubBus{}

	ctx, cancel := context.WithCancel(context.Background())

	// Cancel the context immediately after ClaimBead is called (simulated by
	// a short delay — the stub blocks ClaimBead momentarily then the test cancels).
	go func() {
		deadline := time.Now().Add(3 * time.Second)
		for time.Now().Before(deadline) {
			if ledger.claimCount() > 0 {
				cancel()
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
		cancel()
	}()

	intentDir := filepath.Join(projectDir, ".harmonik", "beads-intents")
	deps := daemon.ExportedWorkLoopDeps(daemon.WorkLoopDepsParams{
		BrAdapter:     ledger,
		Bus:           bus,
		ProjectDir:    projectDir,
		HandlerBinary: "/bin/sh",
		HandlerArgs:   []string{"-c", "sleep 30"},
		IntentLogDir:  intentDir,
	})

	err := daemon.ExportedRunWorkLoop(ctx, deps)
	t.Logf("T3-06: runWorkLoop returned: %v", err)

	t.Logf("T3-06: claim count=%d, reopen count=%d, close count=%d",
		ledger.claimCount(), ledger.reopenCount(), ledger.closeCount())

	if ledger.claimCount() > 0 && ledger.reopenCount() > 0 {
		t.Log("T3-06: PASS — bead was claimed then reopened on context cancel")
	} else if ledger.claimCount() > 0 && ledger.reopenCount() == 0 {
		t.Log("T3-06: FINDING — bead was claimed but NOT reopened after context cancel; orphaned in 'claimed' state")
	} else {
		t.Logf("T3-06: NOTE — bead was not claimed (claim=%d); context may have cancelled before first poll", ledger.claimCount())
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Stub types for T3-06
// ─────────────────────────────────────────────────────────────────────────────

// t3StubLedger is a minimal beadLedger implementation for T3 stub-based tests.
type t3StubLedger struct {
	readyIDs []core.BeadID
	mu       sync.Mutex //nolint:unused // see init below
	claims   int
	reopens  int
	closes   int
}

func (s *t3StubLedger) Ready(_ context.Context) ([]core.BeadID, error) {
	return s.readyIDs, nil
}
func (s *t3StubLedger) ClaimBead(_ context.Context, _ string, _ brcli.TimeoutConfig, _ core.RunID, _ core.TransitionID, _ core.BeadID) error {
	s.mu.Lock()
	s.claims++
	s.mu.Unlock()
	return nil
}
func (s *t3StubLedger) CloseBead(_ context.Context, _ string, _ brcli.TimeoutConfig, _ core.RunID, _ core.TransitionID, _ core.BeadID) error {
	s.mu.Lock()
	s.closes++
	s.mu.Unlock()
	return nil
}
func (s *t3StubLedger) ReopenBead(_ context.Context, _ string, _ brcli.TimeoutConfig, _ core.RunID, _ core.TransitionID, _ core.BeadID, _ string) error {
	s.mu.Lock()
	s.reopens++
	s.mu.Unlock()
	return nil
}
func (s *t3StubLedger) claimCount() int  { s.mu.Lock(); defer s.mu.Unlock(); return s.claims }
func (s *t3StubLedger) reopenCount() int { s.mu.Lock(); defer s.mu.Unlock(); return s.reopens }
func (s *t3StubLedger) closeCount() int  { s.mu.Lock(); defer s.mu.Unlock(); return s.closes }

// t3StubBus is a no-op EventEmitter.
type t3StubBus struct{}

func (*t3StubBus) Emit(_ context.Context, _ core.EventType, _ []byte) error { return nil }

// EmitWithRunID is a no-op (test stub; run_id propagation not exercised in t3 tests).
func (*t3StubBus) EmitWithRunID(_ context.Context, _ core.RunID, _ core.EventType, _ []byte) error {
	return nil
}
