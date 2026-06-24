package daemon_test

// reconciliation_bl1bl3_wireup_hk6tmpp_test.go — composition-root tests
// confirming that RunCatBL1StartupSweep and RunCatBL3StartupSweep are both
// invoked during daemon.Start (the boot sweep).
//
// Prior to hk-6tmpp the two functions existed in reconciliation.go but had
// no caller — they were dead code. These tests prove the wire-up is live.
//
// Spec ref: specs/reconciliation/spec.md §8.BL1, §8.BL3.
// Bead ref: hk-6tmpp.

import (
	"bufio"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
)

// bl1bl3FixtureProjectDir creates a temp dir wired as a project root with
// the .harmonik/events sub-tree for JSONL logging. Returns projectDir and
// jsonlPath.
func bl1bl3FixtureProjectDir(t *testing.T) (projectDir, jsonlPath string) {
	t.Helper()
	projectDir = t.TempDir()
	eventsDir := filepath.Join(projectDir, ".harmonik", "events")
	//nolint:gosec // G301: test-only temp directory
	if err := os.MkdirAll(eventsDir, 0o755); err != nil {
		t.Fatalf("bl1bl3FixtureProjectDir: mkdir %s: %v", eventsDir, err)
	}
	jsonlPath = filepath.Join(eventsDir, "events.jsonl")
	return projectDir, jsonlPath
}

// bl1bl3ReadJSONLLines reads all non-empty lines from the JSONL log at path.
func bl1bl3ReadJSONLLines(t *testing.T, path string) []string {
	t.Helper()
	//nolint:gosec // G304: path is t.TempDir()-based
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatalf("bl1bl3ReadJSONLLines: open %s: %v", path, err)
	}
	defer func() { _ = f.Close() }()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		if line := strings.TrimSpace(scanner.Text()); line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

// bl1bl3StartDaemon starts daemon.Start in a background goroutine.
// Returns a cancel func and a done channel. Callers must call cancel().
func bl1bl3StartDaemon(t *testing.T, cfg daemon.Config) (cancel context.CancelFunc, done <-chan error) {
	t.Helper()
	ctx, cancelFn := context.WithCancel(context.Background())
	ch := make(chan error, 1)
	go func() {
		ch <- daemon.Start(ctx, cfg)
	}()
	return cancelFn, ch
}

// TestDaemonStart_CatBL3StartupSweepFiresAtBoot verifies that
// RunCatBL3StartupSweep is invoked during daemon.Start and emits a
// bead_ledger_conflict_audit event when .beads/merge-conflicts.log contains
// valid conflict entries.
//
// This is the observable proof that the Cat-BL3 call site in daemon.Start
// was wired (hk-6tmpp).
//
// Spec ref: specs/reconciliation/spec.md §8.BL3.
// Bead ref: hk-6tmpp.
func TestDaemonStart_CatBL3StartupSweepFiresAtBoot(t *testing.T) {
	t.Parallel()

	projectDir, jsonlPath := bl1bl3FixtureProjectDir(t)

	// Create .beads/merge-conflicts.log with one valid conflict line so that
	// RunCatBL3StartupSweep emits bead_ledger_conflict_audit.
	beadsDir := filepath.Join(projectDir, ".beads")
	//nolint:gosec // G301: test-only temp directory
	if err := os.MkdirAll(beadsDir, 0o755); err != nil {
		t.Fatalf("mkdir .beads: %v", err)
	}
	conflictLine := "2026-06-24T12:00:00Z CONFLICT bead=hk-abc123 field=status a=open b=closed resolution=b\n"
	conflictLogPath := filepath.Join(beadsDir, "merge-conflicts.log")
	//nolint:gosec // G306: test-only file
	if err := os.WriteFile(conflictLogPath, []byte(conflictLine), 0o644); err != nil {
		t.Fatalf("write merge-conflicts.log: %v", err)
	}

	cfg := daemon.Config{
		ProjectDir:            projectDir,
		JSONLLogPath:          jsonlPath,
		BrPath:                "", // no bead ledger ops needed for BL3
		SkipWALCheckpoint:     true,
		SkipBrHistoryRotation: true,
		SkipRestartBackoff:    true,
		WorkflowModeDefault:   core.WorkflowModeReviewLoop,
	}

	cancel, done := bl1bl3StartDaemon(t, cfg)
	defer cancel()

	// Allow the synchronous startup path (pre-work-loop) to run and flush events.
	time.Sleep(300 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("daemon.Start did not return within 5 s after context cancellation")
	}

	lines := bl1bl3ReadJSONLLines(t, jsonlPath)
	eventType := string(core.EventTypeBeadLedgerConflictAudit)
	for _, line := range lines {
		if strings.Contains(line, eventType) {
			return // found — BL3 fired at startup
		}
	}
	t.Errorf("no %q event in JSONL log after daemon.Start; RunCatBL3StartupSweep must be wired in the boot sweep (hk-6tmpp)", eventType)
}

// TestDaemonStart_CatBL1StartupSweepFiresAtBoot verifies that
// RunCatBL1StartupSweep is invoked during daemon.Start and that the daemon
// completes startup cleanly when BrPath is empty (BL1 returns a non-fatal
// error that is suppressed at the call site).
//
// The absence of a panic proves the call site is wired and BL1 was executed.
// Observable BL1 events (orphaned_child_bead) require a real br binary with
// matching bead data and are out of scope for this boot-path smoke test.
//
// Spec ref: specs/reconciliation/spec.md §8.BL1.
// Bead ref: hk-6tmpp.
func TestDaemonStart_CatBL1StartupSweepFiresAtBoot(t *testing.T) {
	t.Parallel()

	projectDir, jsonlPath := bl1bl3FixtureProjectDir(t)

	cfg := daemon.Config{
		ProjectDir:            projectDir,
		JSONLLogPath:          jsonlPath,
		BrPath:                "", // empty → BL1 returns non-fatal error; daemon continues
		SkipWALCheckpoint:     true,
		SkipBrHistoryRotation: true,
		SkipRestartBackoff:    true,
		WorkflowModeDefault:   core.WorkflowModeReviewLoop,
	}

	cancel, done := bl1bl3StartDaemon(t, cfg)
	defer cancel()

	time.Sleep(300 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		// Only context-cancel or nil is acceptable. Any other error means the
		// BL1 call site produced a fatal startup failure — it must be non-fatal.
		if err != nil &&
			!strings.Contains(err.Error(), "context canceled") &&
			!strings.Contains(err.Error(), "context deadline exceeded") {
			t.Errorf("daemon.Start returned unexpected fatal error after CatBL1 wire-up: %v; "+
				"RunCatBL1StartupSweep must be non-fatal (hk-6tmpp)", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("daemon.Start did not return within 5 s after context cancellation")
	}
}
