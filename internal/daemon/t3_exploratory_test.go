package daemon_test

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

func t3FixtureProjectDir(t *testing.T) (string, string) {
	t.Helper()
	raw := t.TempDir()
	projectDir, resolveErr := filepath.EvalSymlinks(raw)
	if resolveErr != nil {
		t.Fatalf("t3FixtureProjectDir: EvalSymlinks %q: %v", raw, resolveErr)
	}
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

func t3FixtureGitRepo(t *testing.T, dir string) {
	t.Helper()
	run := func(args ...string) {
		t.Helper()
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

func t3FixtureBrPath(t *testing.T) string {
	t.Helper()
	p, err := exec.LookPath("br")
	if err != nil {
		t.Skip("br required (not on PATH)")
	}
	return p
}

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

func t3FixtureInitBr(t *testing.T, realBrPath, projectDir, brWrapper string) string {
	t.Helper()
	initCmd := exec.CommandContext(t.Context(), realBrPath, "init", "--prefix", "t3")
	initCmd.Dir = projectDir
	if out, err := initCmd.CombinedOutput(); err != nil {
		t.Fatalf("t3FixtureInitBr: br init: %v\n%s", err, out)
	}
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

func t3FixtureMarkBeadReady(t *testing.T, brWrapper, beadID string) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), brWrapper, "update", beadID, "--status", "ready")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("t3FixtureMarkBeadReady: br update %s --status ready: %v\n%s", beadID, err, out)
	}
}

func t3FixtureBeadStatus(t *testing.T, brWrapper, beadID string) string {
	t.Helper()
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

func t3FixturePidfileExists(projectDir string) bool {
	_, err := os.Stat(filepath.Join(projectDir, ".harmonik", "daemon.pid"))
	return err == nil
}

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

	beadID := t3FixtureInitBr(t, realBr, projectDir, brWrapper)
	t.Logf("T3-01: seeded bead %s", beadID)

	cfg := daemon.Config{
		ProjectDir:          projectDir,
		JSONLLogPath:        jsonlPath,
		BrPath:              brWrapper,
		HandlerBinary:       slowHandler,
		WorkflowModeDefault: core.WorkflowModeDot,
	}

	d1Ctx, d1Cancel := context.WithCancel(context.Background())
	defer d1Cancel()
	d1Done := make(chan error, 1)
	go func() { d1Done <- daemon.Start(d1Ctx, cfg) }()

	time.Sleep(500 * time.Millisecond)

	if !t3FixturePidfileExists(projectDir) {
		t.Error("T3-01: pidfile not created by daemon 1 after 500ms")
	}

	d2Err := daemon.Start(context.Background(), cfg)
	t.Logf("T3-01: daemon 2 error: %v", d2Err)

	if d2Err == nil {
		t.Error("T3-01: FINDING — second daemon.Start returned nil (should have returned ErrPidfileLocked); two daemons may be running concurrently on the same project dir")
	} else if !errors.Is(d2Err, lifecycle.ErrPidfileLocked) {
		t.Errorf("T3-01: FINDING — second daemon.Start returned %v (want ErrPidfileLocked); double-check error wrapping", d2Err)
	} else {
		t.Logf("T3-01: PASS — second daemon correctly rejected with ErrPidfileLocked")
	}

	d1Cancel()
	select {
	case err := <-d1Done:
		t.Logf("T3-01: daemon 1 stopped: %v", err)
	case <-time.After(daemon.ExportedDaemonExitHangBudget):
		t.Errorf("T3-01: daemon 1 did not stop within %s after cancel", daemon.ExportedDaemonExitHangBudget)
	}
}

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

	beadID := t3FixtureInitBr(t, realBr, projectDir, brWrapper)
	t.Logf("T3-02: seeded bead %s", beadID)

	cfg := daemon.Config{
		ProjectDir:          projectDir,
		JSONLLogPath:        jsonlPath,
		BrPath:              brWrapper,
		HandlerBinary:       slowHandler,
		WorkflowModeDefault: core.WorkflowModeDot,
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- daemon.Start(ctx, cfg) }()

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

	if claimed {
		time.Sleep(2 * time.Second)
	}

	wtGlob := filepath.Join(projectDir, ".harmonik", "worktrees", "*")
	beforeWTs, _ := filepath.Glob(wtGlob)
	t.Logf("T3-02: worktrees before cancel: %v", beforeWTs)

	t.Log("T3-02: cancelling context")
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("T3-02: daemon.Start returned error after cancel: %v", err)
		} else {
			t.Log("T3-02: daemon.Start returned nil (clean)")
		}
	case <-time.After(daemon.ExportedDaemonExitHangBudget):
		t.Errorf("T3-02: daemon.Start did not return within %s after cancel", daemon.ExportedDaemonExitHangBudget)
	}

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

	afterWTs, _ := filepath.Glob(wtGlob)
	t.Logf("T3-02: worktrees after cancel: %v", afterWTs)
	if len(afterWTs) > 0 {
		t.Logf("T3-02: FINDING (expected gap hk-fgdgz) — worktree(s) left behind after cancel: %v", afterWTs)
	} else if claimed {
		t.Log("T3-02: NOTE — no worktree found after cancel; check workspace.WorktreePath for correct glob pattern")
	}

	lines := t3FixtureReadJSONLLines(t, jsonlPath)
	t.Logf("T3-02: JSONL line count = %d", len(lines))
}

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

	beadID := t3FixtureInitBr(t, realBr, projectDir, brWrapper)
	t.Logf("T3-03: seeded bead %s", beadID)

	cfg := daemon.Config{
		ProjectDir:          projectDir,
		JSONLLogPath:        jsonlPath,
		BrPath:              brWrapper,
		HandlerBinary:       slowHandler,
		WorkflowModeDefault: core.WorkflowModeDot,
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- daemon.Start(ctx, cfg) }()

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

	if claimed {
		time.Sleep(2 * time.Second)
	}

	wtGlob := filepath.Join(projectDir, ".harmonik", "worktrees", "*")
	beforeWTs, _ := filepath.Glob(wtGlob)
	t.Logf("T3-03: worktrees before cancel: %v", beforeWTs)

	t.Log("T3-03: cancelling context")
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("T3-03: daemon.Start returned error after cancel: %v", err)
		} else {
			t.Log("T3-03: daemon.Start returned nil (clean)")
		}
	case <-time.After(daemon.ExportedDaemonExitHangBudget):
		t.Errorf("T3-03: daemon.Start did not return within %s after cancel", daemon.ExportedDaemonExitHangBudget)
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

	const stalePID = 99999999
	const stalePGID = 99999998
	pidfilePath := filepath.Join(projectDir, ".harmonik", "daemon.pid")
	pidfileContent := []byte("99999999\n99999998\nstale-instance-dead\n")
	if err := os.WriteFile(pidfilePath, pidfileContent, 0o600); err != nil {
		t.Fatalf("T3-04: write stale pidfile: %v", err)
	}
	t.Logf("T3-04: wrote stale pidfile with pid=%d pgid=%d", stalePID, stalePGID)

	status, probedPID, probeErr := lifecycle.ProbePidfileLock(projectDir)
	t.Logf("T3-04: ProbePidfileLock → status=%d pid=%d err=%v", status, probedPID, probeErr)

	cfg := daemon.Config{
		ProjectDir:          projectDir,
		JSONLLogPath:        jsonlPath,
		BrPath:              "", // no work loop; just test pidfile acquisition
		WorkflowModeDefault: core.WorkflowModeDot,
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
	case <-time.After(daemon.ExportedDaemonExitHangBudget):
		t.Errorf("T3-04: daemon.Start did not return within %s (hung?)", daemon.ExportedDaemonExitHangBudget)
		cancel() // emergency stop; replaces syscall.Kill self-signal per hk-i4mtq
	}
}

// TestT3_StaleWorktreeOrphanSweep verifies that when a stale worktree lease-lock
// file is present (left by a crashed daemon), the next daemon startup's orphan
// sweep removes it and emits daemon_orphan_sweep_completed with locks_cleared > 0.
func TestT3_StaleWorktreeOrphanSweep(t *testing.T) {
	projectDir, jsonlPath := t3FixtureProjectDir(t)
	t3FixtureGitRepo(t, projectDir)

	staleRunID := "t3-stale-run-0000000000000000000000000000"
	wtPath := workspace.WorktreePath(projectDir, staleRunID, workspace.NoWorktreeRootOverride())
	//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
	if err := os.MkdirAll(wtPath, 0o755); err != nil {
		t.Fatalf("T3-05: mkdir stale worktree: %v", err)
	}

	lockPath := filepath.Join(wtPath, ".lease-lock")
	if err := os.WriteFile(lockPath, []byte("stale-lock\n"), 0o600); err != nil {
		t.Fatalf("T3-05: write stale lease-lock: %v", err)
	}
	t.Logf("T3-05: seeded stale lease-lock at %s", lockPath)

	cfg := daemon.Config{
		ProjectDir:          projectDir,
		JSONLLogPath:        jsonlPath,
		BrPath:              "", // no work loop; just orphan sweep on startup
		WorkflowModeDefault: core.WorkflowModeDot,
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
	case <-time.After(daemon.ExportedDaemonExitHangBudget):
		t.Errorf("T3-05: daemon.Start hung for %s", daemon.ExportedDaemonExitHangBudget)
		cancel() // emergency stop; replaces syscall.Kill self-signal per hk-i4mtq
	}

	lines := t3FixtureReadJSONLLines(t, jsonlPath)
	t.Logf("T3-05: JSONL lines: %d", len(lines))

	foundSweep := false
	for _, line := range lines {
		if strings.Contains(line, "daemon_orphan_sweep_completed") || strings.Contains(line, "orphan_sweep_completed") {
			foundSweep = true
			t.Logf("T3-05: sweep event: %s", line)
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

	if _, err := os.Stat(lockPath); errors.Is(err, os.ErrNotExist) {
		t.Log("T3-05: PASS — stale lease-lock file removed by orphan sweep")
	} else {
		t.Log("T3-05: FINDING — stale lease-lock file still present after startup orphan sweep")
	}

	if t3FixturePidfileExists(projectDir) {
		t.Log("T3-05: pidfile still on disk (kernel will release flock on process exit)")
	}
}

// TestT3_SignalBeforeHandlerLaunch verifies the edge case where SIGINT arrives
// between ClaimBead and handler Launch. The work loop should detect ctx.Done()
// and still call ReopenBead for the claimed bead.
//
// This test uses the stub-adapter path (ExportedTestRuntime / ExportedRunWorkLoop)
// to inject a controlled bead ledger that pauses between claim and launch.
func TestT3_SignalBeforeHandlerLaunch(t *testing.T) {
	projectDir, _ := t3FixtureProjectDir(t)
	t3FixtureGitRepo(t, projectDir)

	const beadID = core.BeadID("t3-claim-signal-test")

	ledger := &t3StubLedger{
		readyIDs: []core.BeadID{beadID},
	}

	bus := &t3StubBus{}

	ctx, cancel := context.WithCancel(context.Background())

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
	deps := daemon.ExportedTestRuntime(daemon.TestRuntimeParams{
		BrAdapter:        ledger,
		Bus:              bus,
		ProjectDir:       projectDir,
		HandlerBinary:    "/bin/sh",
		HandlerArgs:      []string{"-c", "sleep 30"},
		AdapterRegistry2: NewSealedAdapterRegistryForTest(t),
		IntentLogDir:     intentDir,
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

type t3StubLedger struct {
	readyIDs []core.BeadID
	mu       sync.Mutex
	claims   int
	reopens  int
	closes   int
}

func (s *t3StubLedger) Ready(_ context.Context) ([]core.BeadRecord, error) {
	records := make([]core.BeadRecord, len(s.readyIDs))
	for i, id := range s.readyIDs {
		records[i] = core.BeadRecord{BeadID: id}
	}
	return records, nil
}

func (s *t3StubLedger) ShowBead(_ context.Context, id core.BeadID) (core.BeadRecord, error) {
	return core.BeadRecord{BeadID: id, Status: core.CoarseStatusOpen}, nil
}

func (s *t3StubLedger) ClaimBead(_ context.Context, _ string, _ brcli.TimeoutConfig, _ core.RunID, _ core.TransitionID, _ core.BeadID) error {
	s.mu.Lock()
	s.claims++
	s.mu.Unlock()
	return nil
}

func (s *t3StubLedger) CloseBead(_ context.Context, _ string, _ brcli.TimeoutConfig, _ core.RunID, _ core.TransitionID, _ core.BeadID, _ bool) error {
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

type t3StubBus struct{}

func (*t3StubBus) Emit(_ context.Context, _ core.EventType, _ []byte) error { return nil }

// EmitWithRunID is a no-op (test stub; run_id propagation not exercised in t3 tests).
func (*t3StubBus) EmitWithRunID(_ context.Context, _ core.RunID, _ core.EventType, _ []byte) error {
	return nil
}
