package daemon_test

// workloop_test.go — tests for the MVH main work loop (hk-ecrxy).
//
// Helper prefix: workloopFixture (per implementer-protocol.md §Helper-prefix
// discipline; bead hk-ecrxy).

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
)

// ─────────────────────────────────────────────────────────────────────────────
// Fixtures
// ─────────────────────────────────────────────────────────────────────────────

// workloopFixtureProjectDir creates a minimal project directory tree for daemon
// integration tests: .harmonik/events/, .harmonik/beads-intents/.  Returns the
// project dir and the JSONL log path.
func workloopFixtureProjectDir(t *testing.T) (projectDir, jsonlPath string) {
	t.Helper()
	projectDir = t.TempDir()
	eventsDir := filepath.Join(projectDir, ".harmonik", "events")
	//nolint:gosec // G301: test-only temp directory; not production
	if err := os.MkdirAll(eventsDir, 0o755); err != nil {
		t.Fatalf("workloopFixtureProjectDir: mkdir events: %v", err)
	}
	intentsDir := filepath.Join(projectDir, ".harmonik", "beads-intents")
	//nolint:gosec // G301: test-only temp directory; not production
	if err := os.MkdirAll(intentsDir, 0o755); err != nil {
		t.Fatalf("workloopFixtureProjectDir: mkdir beads-intents: %v", err)
	}
	jsonlPath = filepath.Join(eventsDir, "events.jsonl")
	return projectDir, jsonlPath
}

// workloopFixtureGitRepo initialises a bare git repository with a single
// initial commit in dir.  Required because CreateWorktree calls `git worktree
// add` and needs an existing git repo with a resolvable HEAD.
func workloopFixtureGitRepo(t *testing.T, dir string) {
	t.Helper()
	run := func(args ...string) {
		t.Helper()
		//nolint:gosec // G204: git args are test-internal literals; not user input
		cmd := exec.CommandContext(t.Context(), "git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("workloopFixtureGitRepo: git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "--initial-branch=main")
	run("config", "user.email", "test@harmonik.local")
	run("config", "user.name", "Harmonik Test")
	initFile := filepath.Join(dir, "README")
	if err := os.WriteFile(initFile, []byte("harmonik test repo\n"), 0o644); err != nil {
		t.Fatalf("workloopFixtureGitRepo: WriteFile: %v", err)
	}
	run("add", "README")
	run("commit", "-m", "Initial commit")
}

// workloopFixtureReadJSONLLines reads all non-empty JSONL lines from path.
func workloopFixtureReadJSONLLines(t *testing.T, path string) []string {
	t.Helper()
	//nolint:gosec // G304: path is t.TempDir()-based; not user input
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("workloopFixtureReadJSONLLines: open %s: %v", path, err)
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

// ─────────────────────────────────────────────────────────────────────────────
// Stub bead ledger
// ─────────────────────────────────────────────────────────────────────────────

// stubBeadLedger implements brcli.Adapter-compatible calls as a lightweight
// in-memory stub for work loop tests.  Concurrency: all methods are safe to
// call concurrently.
type stubBeadLedger struct {
	mu     sync.Mutex
	ready  []core.BeadID
	closed []core.BeadID
	opened []core.BeadID
}

func (s *stubBeadLedger) Ready(_ context.Context) ([]core.BeadRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.ready) == 0 {
		return []core.BeadRecord{}, nil
	}
	// Dequeue one bead per Ready call — simulates a draining queue.
	id := s.ready[0]
	s.ready = s.ready[1:]
	return []core.BeadRecord{{BeadID: id}}, nil
}

func (s *stubBeadLedger) ClaimBead(_ context.Context, _ string, _ brcli.TimeoutConfig, _ core.RunID, _ core.TransitionID, beadID core.BeadID) error {
	return nil
}

func (s *stubBeadLedger) CloseBead(_ context.Context, _ string, _ brcli.TimeoutConfig, _ core.RunID, _ core.TransitionID, beadID core.BeadID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = append(s.closed, beadID)
	return nil
}

func (s *stubBeadLedger) ReopenBead(_ context.Context, _ string, _ brcli.TimeoutConfig, _ core.RunID, _ core.TransitionID, beadID core.BeadID, _ string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.opened = append(s.opened, beadID)
	return nil
}

func (s *stubBeadLedger) closedIDs() []core.BeadID {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]core.BeadID, len(s.closed))
	copy(out, s.closed)
	return out
}

func (s *stubBeadLedger) reopenedIDs() []core.BeadID {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]core.BeadID, len(s.opened))
	copy(out, s.opened)
	return out
}

// ─────────────────────────────────────────────────────────────────────────────
// Stub event collector
// ─────────────────────────────────────────────────────────────────────────────

// stubEventCollector is an EventEmitter that records emitted events.
type stubEventCollector struct {
	mu     sync.Mutex
	events []stubEmittedEvent
}

type stubEmittedEvent struct {
	EventType string
	Payload   json.RawMessage
}

func (s *stubEventCollector) Emit(_ context.Context, eventType core.EventType, payload []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	raw := make(json.RawMessage, len(payload))
	copy(raw, payload)
	s.events = append(s.events, stubEmittedEvent{EventType: string(eventType), Payload: raw})
	return nil
}

// EmitWithRunID records the event (run_id is stored in payload only for stub
// simplicity; the envelope run_id is not materialised here).
func (s *stubEventCollector) EmitWithRunID(_ context.Context, _ core.RunID, eventType core.EventType, payload []byte) error {
	return s.Emit(context.Background(), eventType, payload)
}

func (s *stubEventCollector) eventTypes() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, len(s.events))
	for i, e := range s.events {
		out[i] = e.EventType
	}
	return out
}

// ─────────────────────────────────────────────────────────────────────────────
// TestDaemonStart_WorkLoopSkippedWithNoBrPath
// ─────────────────────────────────────────────────────────────────────────────

// TestDaemonStart_WorkLoopSkippedWithNoBrPath confirms that daemon.Start with
// BrPath="" skips the work loop and returns promptly, emitting daemon_started
// in the JSONL log.  This is the unit-test mode: useful when test fixtures do
// not have a real br binary.
//
// Spec ref: MVH_ROADMAP.md row #10; hk-ecrxy — "Skip the work loop when
// BrPath is not configured (unit-test mode)".
func TestDaemonStart_WorkLoopSkippedWithNoBrPath(t *testing.T) {
	t.Parallel()

	projectDir, jsonlPath := workloopFixtureProjectDir(t)
	workloopFixtureGitRepo(t, projectDir)

	cfg := daemon.Config{
		ProjectDir:   projectDir,
		JSONLLogPath: jsonlPath,
		BrPath:       "", // skip work loop
	}

	// daemon.Start should return promptly (no blocking work loop).
	if err := daemon.Start(context.Background(), cfg); err != nil {
		t.Fatalf("daemon.Start: %v", err)
	}

	lines := workloopFixtureReadJSONLLines(t, jsonlPath)
	if len(lines) == 0 {
		t.Error("JSONL log has 0 lines after Start; want at least daemon_started")
	}
	foundStarted := false
	for _, line := range lines {
		if strings.Contains(line, string(core.EventTypeDaemonStarted)) ||
			strings.Contains(line, `"started_at"`) {
			foundStarted = true
			break
		}
	}
	if !foundStarted {
		t.Errorf("daemon_started not found in JSONL lines: %v", lines)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// TestWorkLoop_DispatchClosesBead — unit test against injected deps
// ─────────────────────────────────────────────────────────────────────────────

// TestWorkLoop_DispatchClosesBead injects stub deps directly into the work loop
// to test the full claim → launch → wait → close cycle without requiring a real
// br binary or Claude Code.
//
// Acceptance criteria (per bead body):
//   - bead was closed (stubBeadLedger.closedIDs contains the seeded bead ID).
//   - run_completed event was emitted.
func TestWorkLoop_DispatchClosesBead(t *testing.T) {
	t.Parallel()

	projectDir, _ := workloopFixtureProjectDir(t)
	workloopFixtureGitRepo(t, projectDir)

	// Seed one ready bead.
	const beadID = core.BeadID("test-bead-001")
	ledger := &stubBeadLedger{
		ready: []core.BeadID{beadID},
	}
	collector := &stubEventCollector{}

	// The handler binary will be sh -c 'exit 0' — exits immediately with code 0.
	deps := daemon.ExportedWorkLoopDeps(daemon.WorkLoopDepsParams{
		BrAdapter:     ledger,
		Bus:           collector,
		ProjectDir:    projectDir,
		HandlerBinary: "/bin/sh",
		HandlerArgs:   []string{"-c", "exit 0"},
		IntentLogDir:  filepath.Join(projectDir, ".harmonik", "beads-intents"),
	})

	// Run with a 5-second timeout per bead body spec.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// The loop will dispatch the bead, close it, then find the queue empty and
	// sleep. Cancel the context a short time after to stop the loop cleanly.
	waitDone := make(chan struct{})
	go func() {
		defer close(waitDone)
		daemon.ExportedRunWorkLoop(ctx, deps)
	}()

	// Poll until the bead is closed or timeout.
	deadline := time.After(4 * time.Second)
	for {
		if len(ledger.closedIDs()) > 0 {
			break
		}
		select {
		case <-deadline:
			t.Fatal("timed out waiting for bead to be closed")
		case <-time.After(50 * time.Millisecond):
		}
	}

	// Cancel the context to stop the loop goroutine.
	cancel()
	select {
	case <-waitDone:
	case <-time.After(2 * time.Second):
		t.Fatal("work loop did not exit after context cancellation")
	}

	// Assert bead was closed.
	closedIDs := ledger.closedIDs()
	if len(closedIDs) == 0 {
		t.Fatal("no beads were closed; expected test-bead-001 to be closed")
	}
	if closedIDs[0] != beadID {
		t.Errorf("closed bead = %q; want %q", closedIDs[0], beadID)
	}
	if len(ledger.reopenedIDs()) > 0 {
		t.Errorf("unexpected ReopenBead calls: %v", ledger.reopenedIDs())
	}

	// Assert run_completed event was emitted.
	eventTypes := collector.eventTypes()
	foundCompleted := false
	for _, et := range eventTypes {
		if et == string(core.EventTypeRunCompleted) {
			foundCompleted = true
			break
		}
	}
	if !foundCompleted {
		t.Errorf("run_completed event not found; got event types: %v", eventTypes)
	}

	// run_started must also have been emitted before run_completed.
	foundStarted := false
	for _, et := range eventTypes {
		if et == string(core.EventTypeRunStarted) {
			foundStarted = true
			break
		}
	}
	if !foundStarted {
		t.Errorf("run_started event not found; got event types: %v", eventTypes)
	}
}

// TestWorkLoop_FailedHandlerReopensBead verifies that a non-zero subprocess
// exit causes ReopenBead rather than CloseBead.
func TestWorkLoop_FailedHandlerReopensBead(t *testing.T) {
	t.Parallel()

	projectDir, _ := workloopFixtureProjectDir(t)
	workloopFixtureGitRepo(t, projectDir)

	const beadID = core.BeadID("test-bead-fail-001")
	ledger := &stubBeadLedger{
		ready: []core.BeadID{beadID},
	}
	collector := &stubEventCollector{}

	deps := daemon.ExportedWorkLoopDeps(daemon.WorkLoopDepsParams{
		BrAdapter:     ledger,
		Bus:           collector,
		ProjectDir:    projectDir,
		HandlerBinary: "/bin/sh",
		HandlerArgs:   []string{"-c", "exit 1"},
		IntentLogDir:  filepath.Join(projectDir, ".harmonik", "beads-intents"),
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	waitDone := make(chan struct{})
	go func() {
		defer close(waitDone)
		daemon.ExportedRunWorkLoop(ctx, deps)
	}()

	// Poll until the bead is reopened.
	deadline := time.After(4 * time.Second)
	for {
		if len(ledger.reopenedIDs()) > 0 {
			break
		}
		select {
		case <-deadline:
			t.Fatal("timed out waiting for bead to be reopened")
		case <-time.After(50 * time.Millisecond):
		}
	}

	cancel()
	select {
	case <-waitDone:
	case <-time.After(2 * time.Second):
		t.Fatal("work loop did not exit after context cancellation")
	}

	reopenedIDs := ledger.reopenedIDs()
	if len(reopenedIDs) == 0 {
		t.Fatal("no beads were reopened; expected test-bead-fail-001 to be reopened")
	}
	if reopenedIDs[0] != beadID {
		t.Errorf("reopened bead = %q; want %q", reopenedIDs[0], beadID)
	}
	if len(ledger.closedIDs()) > 0 {
		t.Errorf("unexpected CloseBead calls: %v", ledger.closedIDs())
	}

	// run_failed event expected.
	eventTypes := collector.eventTypes()
	foundFailed := false
	for _, et := range eventTypes {
		if et == string(core.EventTypeRunFailed) {
			foundFailed = true
			break
		}
	}
	if !foundFailed {
		t.Errorf("run_failed event not found; got event types: %v", eventTypes)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// closeErrFixture — stub ledger for CloseBead-error path (hk-wfbxf)
// ─────────────────────────────────────────────────────────────────────────────

// closeErrFixtureLedger is a stub beadLedger that returns an error from
// CloseBead.  All other methods delegate to the inner stubBeadLedger so the
// normal claim/reopen recording is available.
type closeErrFixtureLedger struct {
	inner    *stubBeadLedger
	closeErr error
}

func (c *closeErrFixtureLedger) Ready(ctx context.Context) ([]core.BeadRecord, error) {
	return c.inner.Ready(ctx)
}

func (c *closeErrFixtureLedger) ClaimBead(ctx context.Context, d string, cfg brcli.TimeoutConfig, r core.RunID, tid core.TransitionID, bid core.BeadID) error {
	return c.inner.ClaimBead(ctx, d, cfg, r, tid, bid)
}

func (c *closeErrFixtureLedger) CloseBead(_ context.Context, _ string, _ brcli.TimeoutConfig, _ core.RunID, _ core.TransitionID, _ core.BeadID) error {
	return c.closeErr
}

func (c *closeErrFixtureLedger) ReopenBead(ctx context.Context, d string, cfg brcli.TimeoutConfig, r core.RunID, tid core.TransitionID, bid core.BeadID, reason string) error {
	return c.inner.ReopenBead(ctx, d, cfg, r, tid, bid, reason)
}

// TestWorkLoop_CloseBeadError_EmitsRunFailed verifies that when CloseBead
// returns an error the work loop emits run_failed (not run_completed) so that
// JSONL and bead state remain consistent (hk-wfbxf: no split-brain).
func TestWorkLoop_CloseBeadError_EmitsRunFailed(t *testing.T) {
	t.Parallel()

	projectDir, _ := workloopFixtureProjectDir(t)
	workloopFixtureGitRepo(t, projectDir)

	const beadID = core.BeadID("test-bead-closeerr-001")
	inner := &stubBeadLedger{
		ready: []core.BeadID{beadID},
	}
	ledger := &closeErrFixtureLedger{
		inner:    inner,
		closeErr: errors.New("disk full"),
	}
	collector := &stubEventCollector{}

	// Handler exits 0 so the loop attempts CloseBead.
	deps := daemon.ExportedWorkLoopDeps(daemon.WorkLoopDepsParams{
		BrAdapter:     ledger,
		Bus:           collector,
		ProjectDir:    projectDir,
		HandlerBinary: "/bin/sh",
		HandlerArgs:   []string{"-c", "exit 0"},
		IntentLogDir:  filepath.Join(projectDir, ".harmonik", "beads-intents"),
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	waitDone := make(chan struct{})
	go func() {
		defer close(waitDone)
		daemon.ExportedRunWorkLoop(ctx, deps)
	}()

	// Poll until a run_failed event is emitted or timeout.
	deadline := time.After(4 * time.Second)
	for {
		types := collector.eventTypes()
		for _, et := range types {
			if et == string(core.EventTypeRunFailed) {
				goto found
			}
		}
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for run_failed event; got events: %v", collector.eventTypes())
		case <-time.After(50 * time.Millisecond):
		}
	}
found:

	cancel()
	select {
	case <-waitDone:
	case <-time.After(2 * time.Second):
		t.Fatal("work loop did not exit after context cancellation")
	}

	// Must have emitted run_failed, NOT run_completed.
	types := collector.eventTypes()
	for _, et := range types {
		if et == string(core.EventTypeRunCompleted) {
			t.Errorf("hk-wfbxf: run_completed emitted despite CloseBead error; events: %v", types)
		}
	}
	foundFailed := false
	for _, et := range types {
		if et == string(core.EventTypeRunFailed) {
			foundFailed = true
			break
		}
	}
	if !foundFailed {
		t.Errorf("hk-wfbxf: run_failed not emitted on CloseBead error; events: %v", types)
	}
}
