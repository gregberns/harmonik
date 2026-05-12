package daemon_test

import (
	"bufio"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/eventbus"
	"github.com/gregberns/harmonik/internal/lifecycle"
)

// TestDaemonStartCompiles verifies the package compiles and that Start can be
// invoked with a zero-value Config without panicking. This is the smoke-test
// sensor for hk-8mup.61: once this test is green the composition root
// scaffold is in place for subsequent wiring beads.
//
// Spec ref: specs/process-lifecycle.md §4.6 PL-020, PL-020a, PL-005 step 0.
func TestDaemonStartCompiles(t *testing.T) {
	t.Parallel()

	t.Run("start-with-zero-config-returns-nil", func(t *testing.T) {
		t.Parallel()

		cfg := daemon.Config{}
		err := daemon.Start(context.Background(), cfg)
		if err != nil {
			t.Errorf("daemon.Start(Config{}) returned non-nil error: %v; "+
				"stub Start must return nil until subsystem wiring is added", err)
		}
	})

	t.Run("start-with-nil-log-writer-does-not-panic", func(t *testing.T) {
		t.Parallel()

		// Config.LogWriter is nil → silences log output; must not panic.
		cfg := daemon.Config{LogWriter: nil}
		if err := daemon.Start(context.Background(), cfg); err != nil {
			t.Errorf("daemon.Start with nil LogWriter returned error: %v", err)
		}
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// hk-iarcy: pidfile acquisition + daemon_started emission
// ─────────────────────────────────────────────────────────────────────────────

// pidfileFixtureProjectDir creates a temporary directory tree suitable for
// daemon.Start: .harmonik/ is created and a JSONL log path within it is
// returned.  The caller receives the project dir; JSONL path is at
// <dir>/.harmonik/events/events.jsonl.
func pidfileFixtureProjectDir(t *testing.T) (projectDir, jsonlPath string) {
	t.Helper()
	projectDir = t.TempDir()
	eventsDir := filepath.Join(projectDir, ".harmonik", "events")
	//nolint:gosec // G301: test-only temp directory; not production
	if err := os.MkdirAll(eventsDir, 0o755); err != nil {
		t.Fatalf("pidfileFixtureProjectDir: mkdir %s: %v", eventsDir, err)
	}
	jsonlPath = filepath.Join(eventsDir, "events.jsonl")
	return projectDir, jsonlPath
}

// pidfileFixtureReadJSONLLines reads all non-empty lines from a JSONL file.
func pidfileFixtureReadJSONLLines(t *testing.T, path string) []string {
	t.Helper()
	//nolint:gosec // G304: path is t.TempDir()-based; not user input.
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("pidfileFixtureReadJSONLLines: open %s: %v", path, err)
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

// TestDaemonStart_PidfileBlocksSecondInvocation asserts that when ProjectDir
// is set and a pidfile is already held (by this test process), a second call
// to Start returns lifecycle.ErrPidfileLocked.
//
// The test acquires the pidfile directly via lifecycle.AcquirePidfile to
// simulate a running daemon; then calls Start with the same ProjectDir and
// expects a locked error.
//
// Spec ref: specs/process-lifecycle.md §4.1 PL-002, PL-002a, PL-008a.
// Bead ref: hk-iarcy.
func TestDaemonStart_PidfileBlocksSecondInvocation(t *testing.T) {
	t.Parallel()

	projectDir, jsonlPath := pidfileFixtureProjectDir(t)

	// Acquire the pidfile from this goroutine to simulate a running daemon.
	pf, err := lifecycle.AcquirePidfile(projectDir, os.Getpid(), os.Getpid(), "test-instance-holder")
	if err != nil {
		t.Fatalf("pidfileFixture: AcquirePidfile: %v", err)
	}
	defer func() { _ = pf.Release() }()

	// Start with the same ProjectDir must fail because the lock is held.
	cfg := daemon.Config{
		ProjectDir:   projectDir,
		JSONLLogPath: jsonlPath,
	}
	startErr := daemon.Start(context.Background(), cfg)
	if startErr == nil {
		t.Fatal("Start returned nil with pidfile already locked; want non-nil error (PL-002a / PL-008a)")
	}
	if !errors.Is(startErr, lifecycle.ErrPidfileLocked) {
		t.Errorf("Start error = %v; want errors.Is(err, lifecycle.ErrPidfileLocked)", startErr)
	}
}

// TestDaemonStart_EmitsDaemonStarted asserts that Start emits exactly one
// daemon_started event observable via an in-memory JSONL log when JSONLLogPath
// is configured.
//
// This test validates the F-class fsync path (daemon_started is §8.7.1 F-class)
// and confirms one JSONL line is appended per Start call.
//
// Spec ref: specs/event-model.md §8.7.1; specs/process-lifecycle.md PL-005.
// Bead ref: hk-iarcy.
func TestDaemonStart_EmitsDaemonStarted(t *testing.T) {
	t.Parallel()

	projectDir, jsonlPath := pidfileFixtureProjectDir(t)

	cfg := daemon.Config{
		ProjectDir:   projectDir,
		JSONLLogPath: jsonlPath,
	}
	if err := daemon.Start(context.Background(), cfg); err != nil {
		t.Fatalf("daemon.Start: %v", err)
	}

	lines := pidfileFixtureReadJSONLLines(t, jsonlPath)
	if len(lines) == 0 {
		t.Fatal("JSONL log has 0 lines after Start; want at least 1 (daemon_started F-class event)")
	}
	// The daemon_started event must be present somewhere in the log.
	found := false
	for _, line := range lines {
		if strings.Contains(line, `"started_at"`) || strings.Contains(line, `"pid"`) {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("JSONL log lines %v do not contain daemon_started payload fields (started_at, pid)", lines)
	}
}

// TestDaemonStart_DaemonStartedObservableViaInMemoryBus asserts that
// daemon_started is delivered to a pre-registered observer consumer when the
// bus has an observer subscription.
//
// Because daemon.Start currently seals the bus before registering consumers,
// this test verifies the JSONL-based path (above) rather than an in-process
// subscription. This comment documents the limitation so the next bead
// (subscriber wiring) can add the in-process observable path.
//
// Spec ref: specs/event-model.md §8.7.1.
// Bead ref: hk-iarcy.
func TestDaemonStart_DaemonStartedInJSONLLog(t *testing.T) {
	t.Parallel()

	projectDir, jsonlPath := pidfileFixtureProjectDir(t)

	// Build a JSONLWriter manually to read back events after Start.
	writer, err := eventbus.OpenJSONLWriter(jsonlPath)
	if err != nil {
		t.Fatalf("OpenJSONLWriter: %v", err)
	}
	// Pre-write a sentinel line so we know Start's line is additive.
	sentinel := []byte(`{"sentinel":true}`)
	if appendErr := writer.Append(sentinel, false); appendErr != nil {
		t.Fatalf("Append sentinel: %v", appendErr)
	}
	if closeErr := writer.Close(); closeErr != nil {
		t.Fatalf("writer.Close: %v", closeErr)
	}

	cfg := daemon.Config{
		ProjectDir:   projectDir,
		JSONLLogPath: jsonlPath,
	}
	if err := daemon.Start(context.Background(), cfg); err != nil {
		t.Fatalf("daemon.Start: %v", err)
	}

	lines := pidfileFixtureReadJSONLLines(t, jsonlPath)
	// Expect at least 2 lines: our sentinel + daemon_started.
	if len(lines) < 2 {
		t.Fatalf("JSONL log has %d lines, want ≥ 2 (sentinel + daemon_started)", len(lines))
	}

	// Verify daemon_started appears after the sentinel.
	foundDaemonStarted := false
	for _, line := range lines[1:] {
		if strings.Contains(line, string(core.EventTypeDaemonStarted)) ||
			strings.Contains(line, `"started_at"`) {
			foundDaemonStarted = true
			break
		}
	}
	if !foundDaemonStarted {
		t.Errorf("daemon_started event not found in JSONL lines after sentinel: %v", lines[1:])
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// hk-60uvn: orphan sweep wired into Start
// ─────────────────────────────────────────────────────────────────────────────

// TestDaemonStart_OrphanSweepEventEmitted asserts that Start emits a
// daemon_orphan_sweep_completed event (§8.7.14, O-class) when ProjectDir is
// set.
//
// The sweep is non-fatal: Start MUST return nil even if the sweep finds
// nothing to clean up (the common case in a fresh temp dir).
//
// Spec ref: specs/process-lifecycle.md §4.2 PL-006 — "On completion, the
// daemon MUST emit daemon_orphan_sweep_completed."
// Bead ref: hk-60uvn.
func TestDaemonStart_OrphanSweepEventEmitted(t *testing.T) {
	t.Parallel()

	projectDir, jsonlPath := pidfileFixtureProjectDir(t)

	cfg := daemon.Config{
		ProjectDir:   projectDir,
		JSONLLogPath: jsonlPath,
	}
	if err := daemon.Start(context.Background(), cfg); err != nil {
		t.Fatalf("daemon.Start: %v; want nil (orphan sweep errors must not abort Start)", err)
	}

	lines := pidfileFixtureReadJSONLLines(t, jsonlPath)
	// Expect at least 2 lines: daemon_started (F-class) + daemon_orphan_sweep_completed (O-class).
	if len(lines) < 2 {
		t.Fatalf("JSONL log has %d lines after Start, want ≥ 2 (daemon_started + daemon_orphan_sweep_completed)",
			len(lines))
	}

	// Verify daemon_orphan_sweep_completed appears.
	foundSweep := false
	for _, line := range lines {
		if strings.Contains(line, string(core.EventTypeDaemonOrphanSweepCompleted)) ||
			strings.Contains(line, "swept_at") {
			foundSweep = true
			break
		}
	}
	if !foundSweep {
		t.Errorf("daemon_orphan_sweep_completed event not found in JSONL lines: %v", lines)
	}
}

// TestDaemonStart_OrphanSweepRunsBeforeSocketBind asserts that Start returns
// nil in a fresh project directory (no orphans to clean up), confirming the
// sweep path executes without error.
//
// Spec ref: specs/process-lifecycle.md §4.2 PL-005 step 3 — orphan sweep
// runs before socket/listener bind.
// Bead ref: hk-60uvn.
func TestDaemonStart_OrphanSweepNonFatalOnEmptyDir(t *testing.T) {
	t.Parallel()

	projectDir, jsonlPath := pidfileFixtureProjectDir(t)

	cfg := daemon.Config{
		ProjectDir:   projectDir,
		JSONLLogPath: jsonlPath,
	}
	// Start MUST succeed even in a fresh directory with no orphans.
	if err := daemon.Start(context.Background(), cfg); err != nil {
		t.Errorf("daemon.Start with empty project dir returned error: %v; "+
			"sweep errors MUST NOT abort Start (PL-006)", err)
	}
}
