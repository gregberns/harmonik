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
	"sync/atomic"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/brcli"
	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
)

func workloopFixtureProjectDir(t *testing.T) (projectDir, jsonlPath string) {
	t.Helper()
	raw := t.TempDir()
	resolved, resolveErr := filepath.EvalSymlinks(raw)
	if resolveErr != nil {
		t.Fatalf("workloopFixtureProjectDir: EvalSymlinks %q: %v", raw, resolveErr)
	}
	projectDir = resolved
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

func workloopFixtureGitRepo(t *testing.T, dir string) {
	t.Helper()
	run := func(args ...string) {
		t.Helper()
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

	bareDir := dir + "-bare"
	//nolint:gosec // G204: git args are test-internal literals; not user input
	cloneCmd := exec.CommandContext(t.Context(), "git", "clone", "--bare", dir, bareDir)
	cloneOut, cloneErr := cloneCmd.CombinedOutput()
	if cloneErr != nil {
		t.Fatalf("workloopFixtureGitRepo: git clone --bare: %v\n%s", cloneErr, cloneOut)
	}
	run("remote", "add", "origin", bareDir)
}

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

var workloopFixtureSingleLabels = []string{"workflow:single"}

type stubBeadLedger struct {
	mu    sync.Mutex
	ready []core.BeadID

	// labels is what this stub reports on every bead it serves.
	//
	// It is load-bearing for any test that drives the real work loop to a close.
	// An UNLABELLED bead resolves to the REVIEWED graph, whose commit_gate node
	// shells out to `make full` inside the run worktree. A work-loop fixture
	// builds a bare git repo holding one README and no Makefile, so that gate can
	// only fail. The run then reopens the bead, the loop picks it up again, and a
	// test that waits for a close waits for ever while reading its own subject as
	// broken. Set it to workflow:single to select the no-review graph — implement
	// then close — which is the shape those tests' own headers describe.
	//
	// Leave it nil where the reviewed graph is what the test means to exercise.
	labels []string

	closed   []core.BeadID
	opened   []core.BeadID
	closeErr error
	onClose  func(error)
	onReopen func()
}

func (s *stubBeadLedger) Ready(_ context.Context) ([]core.BeadRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.ready) == 0 {
		return []core.BeadRecord{}, nil
	}
	id := s.ready[0]
	s.ready = s.ready[1:]
	return []core.BeadRecord{{BeadID: id, Labels: s.labels}}, nil
}

func (s *stubBeadLedger) ShowBead(_ context.Context, id core.BeadID) (core.BeadRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return core.BeadRecord{BeadID: id, Status: core.CoarseStatusOpen, Labels: s.labels}, nil
}

func (s *stubBeadLedger) ClaimBead(_ context.Context, _ string, _ brcli.TimeoutConfig, _ core.RunID, _ core.TransitionID, beadID core.BeadID) error {
	return nil
}

func (s *stubBeadLedger) CloseBead(_ context.Context, _ string, _ brcli.TimeoutConfig, _ core.RunID, _ core.TransitionID, beadID core.BeadID, _ bool) error {
	s.mu.Lock()
	err, onClose := s.closeErr, s.onClose
	if err == nil {
		s.closed = append(s.closed, beadID)
	}
	s.mu.Unlock()
	if onClose != nil {
		onClose(err)
	}
	return err
}

func (s *stubBeadLedger) ReopenBead(_ context.Context, _ string, _ brcli.TimeoutConfig, _ core.RunID, _ core.TransitionID, beadID core.BeadID, _ string) error {
	s.mu.Lock()
	s.opened = append(s.opened, beadID)
	onReopen := s.onReopen
	s.mu.Unlock()
	if onReopen != nil {
		onReopen()
	}
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

type stubEventCollector struct {
	mu     sync.Mutex
	events []stubEmittedEvent
	onEmit func(core.EventType)
}

type stubEmittedEvent struct {
	EventType string
	EventID   core.EventID
	Payload   json.RawMessage
}

func (s *stubEventCollector) Emit(_ context.Context, eventType core.EventType, payload []byte) error {
	s.mu.Lock()
	raw := make(json.RawMessage, len(payload))
	copy(raw, payload)
	s.events = append(s.events, stubEmittedEvent{EventType: string(eventType), Payload: raw})
	onEmit := s.onEmit
	s.mu.Unlock()
	if onEmit != nil {
		onEmit(eventType)
	}
	return nil
}

func (s *stubEventCollector) collect(evt core.Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	raw := make(json.RawMessage, len(evt.Payload))
	copy(raw, evt.Payload)
	s.events = append(s.events, stubEmittedEvent{
		EventType: string(evt.Type),
		EventID:   evt.EventID,
		Payload:   raw,
	})
}

// EmitWithRunID records the event (run_id is stored in payload only for stub
// simplicity; the envelope run_id is not materialised here).
func (s *stubEventCollector) EmitWithRunID(ctx context.Context, _ core.RunID, eventType core.EventType, payload []byte) error {
	return s.Emit(ctx, eventType, payload)
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

func (s *stubEventCollector) allEvents() []stubEmittedEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]stubEmittedEvent, len(s.events))
	copy(out, s.events)
	return out
}

// TestDaemonStart_WorkLoopSkippedWithNoBrPath confirms that daemon.Start with
// BrPath="" skips the work loop and returns promptly, emitting daemon_started
// in the JSONL log.  This is the unit-test mode: useful when test fixtures do
// not have a real br binary.
//
// Spec ref: EARLY_ROADMAP.md row #10; hk-ecrxy — "Skip the work loop when
// BrPath is not configured (unit-test mode)".
func TestDaemonStart_WorkLoopSkippedWithNoBrPath(t *testing.T) {
	t.Parallel()

	projectDir, jsonlPath := workloopFixtureProjectDir(t)
	workloopFixtureGitRepo(t, projectDir)

	cfg := daemon.Config{
		ProjectDir:          projectDir,
		JSONLLogPath:        jsonlPath,
		BrPath:              "", // skip work loop
		WorkflowModeDefault: core.WorkflowModeDot,
	}

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

	const beadID = core.BeadID("test-bead-001")
	ledger := &stubBeadLedger{
		ready:  []core.BeadID{beadID},
		labels: workloopFixtureSingleLabels,
	}
	collector := &stubEventCollector{}

	deps := daemon.ExportedTestRuntime(daemon.TestRuntimeParams{
		BrAdapter:        ledger,
		Bus:              collector,
		ProjectDir:       projectDir,
		HandlerBinary:    "/bin/sh",
		HandlerArgs:      workloopFixtureAdvanceHeadHandlerArgs(t),
		AdapterRegistry2: NewSealedAdapterRegistryForTest(t),
		IntentLogDir:     filepath.Join(projectDir, ".harmonik", "beads-intents"),
	})

	ctx, cancel := context.WithTimeout(context.Background(), workLoopTestBudget)
	defer cancel()

	waitDone := make(chan struct{})
	go func() {
		defer close(waitDone)
		daemon.ExportedRunWorkLoop(ctx, deps)
	}()

	for len(ledger.closedIDs()) == 0 {
		select {
		case <-ctx.Done():
			t.Fatal("timed out waiting for bead to be closed")
		case <-time.After(50 * time.Millisecond):
		}
	}

	cancel()
	awaitLoopTeardown(t, waitDone, "work loop")

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

	deps := daemon.ExportedTestRuntime(daemon.TestRuntimeParams{
		BrAdapter:        ledger,
		Bus:              collector,
		ProjectDir:       projectDir,
		HandlerBinary:    "/bin/sh",
		HandlerArgs:      []string{"-c", "exit 1"},
		AdapterRegistry2: NewSealedAdapterRegistryForTest(t),
		IntentLogDir:     filepath.Join(projectDir, ".harmonik", "beads-intents"),
	})

	ctx, cancel := context.WithTimeout(context.Background(), workLoopTestBudget)
	defer cancel()

	waitDone := make(chan struct{})
	go func() {
		defer close(waitDone)
		daemon.ExportedRunWorkLoop(ctx, deps)
	}()

	for len(ledger.reopenedIDs()) == 0 {
		select {
		case <-ctx.Done():
			t.Fatal("timed out waiting for bead to be reopened")
		case <-time.After(50 * time.Millisecond):
		}
	}

	cancel()
	awaitLoopTeardown(t, waitDone, "work loop")

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

type concurrentFixtureLedger struct {
	mu sync.Mutex

	// ready is the queue of bead IDs to hand out from Ready.
	ready []core.BeadID

	// inFlight tracks how many beads are currently claimed-but-not-yet-closed.
	inFlight int

	// peakInFlight is the high-water mark of inFlight observed across all
	// ClaimBead calls.
	peakInFlight int

	// reopened counts ReopenBead calls. It feeds no assertion — a reopen is
	// legitimate and the test must not fail on one. It is logged, so a retry
	// rate that climbs stays visible instead of being silently absorbed.
	// Refs hk-twoconcurrentbeads-retry-assertion-06hlw.
	reopened int

	// closed records IDs of beads that have been closed.
	closed []core.BeadID

	// claimedCh is closed once two beads have been simultaneously claimed.
	// Used as a rendezvous so the test can assert peak concurrency.
	claimedCh chan struct{}

	// claimedOnce guards the close of claimedCh.
	claimedOnce sync.Once
}

func (c *concurrentFixtureLedger) ShowBead(_ context.Context, id core.BeadID) (core.BeadRecord, error) {
	return core.BeadRecord{BeadID: id, Status: core.CoarseStatusOpen, Labels: workloopFixtureSingleLabels}, nil
}

func (c *concurrentFixtureLedger) Ready(_ context.Context) ([]core.BeadRecord, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.ready) == 0 {
		return []core.BeadRecord{}, nil
	}
	id := c.ready[0]
	c.ready = c.ready[1:]
	return []core.BeadRecord{{BeadID: id, Labels: workloopFixtureSingleLabels}}, nil
}

func (c *concurrentFixtureLedger) ClaimBead(_ context.Context, _ string, _ brcli.TimeoutConfig, _ core.RunID, _ core.TransitionID, _ core.BeadID) error {
	c.mu.Lock()
	c.inFlight++
	if c.inFlight > c.peakInFlight {
		c.peakInFlight = c.inFlight
	}
	peak := c.peakInFlight
	c.mu.Unlock()

	if peak >= 2 {
		c.claimedOnce.Do(func() { close(c.claimedCh) })
	}
	return nil
}

func (c *concurrentFixtureLedger) CloseBead(_ context.Context, _ string, _ brcli.TimeoutConfig, _ core.RunID, _ core.TransitionID, beadID core.BeadID, _ bool) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.inFlight--
	c.closed = append(c.closed, beadID)
	return nil
}

func (c *concurrentFixtureLedger) ReopenBead(_ context.Context, _ string, _ brcli.TimeoutConfig, _ core.RunID, _ core.TransitionID, beadID core.BeadID, _ string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.inFlight--
	c.reopened++
	c.ready = append(c.ready, beadID)
	return nil
}

func (c *concurrentFixtureLedger) reopenedCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.reopened
}

func (c *concurrentFixtureLedger) closedCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.closed)
}

func (c *concurrentFixtureLedger) distinctClosed() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	seen := make(map[core.BeadID]struct{}, len(c.closed))
	for _, id := range c.closed {
		seen[id] = struct{}{}
	}
	return len(seen)
}

func (c *concurrentFixtureLedger) peak() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.peakInFlight
}

// TestWorkLoop_TwoConcurrentBeads verifies that with MaxConcurrent=2 two beads
// are dispatched concurrently: both beads reach the claimed state simultaneously
// (peak in-flight == 2) and both close successfully.
//
// Acceptance criteria (hk-e61c3.2):
//   - With MaxConcurrent>1, two ready beads dispatch concurrently.
//   - Both beads close before the loop exits.
//   - peakInFlight == 2 at some point during the run.
func TestWorkLoop_TwoConcurrentBeads(t *testing.T) {
	t.Parallel()

	projectDir, _ := workloopFixtureProjectDir(t)
	workloopFixtureGitRepo(t, projectDir)

	const (
		beadA = core.BeadID("concurrent-bead-A")
		beadB = core.BeadID("concurrent-bead-B")
	)

	ledger := &concurrentFixtureLedger{
		ready:     []core.BeadID{beadA, beadB},
		claimedCh: make(chan struct{}),
	}
	collector := &stubEventCollector{}
	var runEnvMu sync.Mutex
	runEnvProjectDirs := make(map[string]string)
	runEnvCalls := 0

	deps := daemon.ExportedTestRuntime(daemon.TestRuntimeParams{
		BrAdapter:        ledger,
		Bus:              collector,
		ProjectDir:       projectDir,
		HandlerBinary:    "/bin/sh",
		HandlerArgs:      workloopFixtureAdvanceHeadHandlerArgs(t),
		IntentLogDir:     filepath.Join(projectDir, ".harmonik", "beads-intents"),
		AdapterRegistry2: NewSealedAdapterRegistryForTest(t),
		MaxConcurrent:    2,
		// The factory only records which project dir each run was handed. It must
		// NOT commit: the handler above does that during the run, which is the only
		// order the node's no-advance guard accepts.
		WorktreeFactory: func(ctx context.Context, gotProjectDir, runID, headSHA string) (string, func(), error) {
			runEnvMu.Lock()
			runEnvCalls++
			runEnvProjectDirs[runID] = gotProjectDir
			runEnvMu.Unlock()
			return daemon.ExportedProductionWorktreeFactory(ctx, gotProjectDir, runID, headSHA)
		},
	})

	ctx, cancel := context.WithTimeout(context.Background(), workLoopTestBudget)
	defer cancel()

	loopDone := make(chan struct{})
	go func() {
		defer close(loopDone)
		daemon.ExportedRunWorkLoop(ctx, deps)
	}()

	select {
	case <-ledger.claimedCh:
	case <-ctx.Done():
		t.Fatal("timed out waiting for two simultaneous in-flight beads at MaxConcurrent=2")
	}

	for ledger.closedCount() < 2 {
		select {
		case <-ctx.Done():
			t.Fatalf("timed out waiting for both beads to close; closed=%d", ledger.closedCount())
		case <-time.After(50 * time.Millisecond):
		}
	}

	cancel()
	awaitLoopTeardown(t, loopDone, "work loop")

	if p := ledger.peak(); p < 2 {
		t.Errorf("peakInFlight = %d; want >= 2 (two beads must be simultaneously in-flight at MaxConcurrent=2)", p)
	}

	if n := ledger.closedCount(); n != 2 {
		t.Errorf("closedCount = %d; want 2", n)
	}
	if got := ledger.distinctClosed(); got != 2 {
		t.Errorf("distinct beads closed = %d; want 2 (both beads, not one bead twice)", got)
	}

	runEnvMu.Lock()
	defer runEnvMu.Unlock()
	if reopens := ledger.reopenedCount(); reopens > 0 {
		t.Logf("a run was retried %d time(s); each retry mints one more run ID", reopens)
	}
	if runEnvCalls < 2 {
		t.Errorf("worktree factory calls = %d; want at least 2 (one per bead)", runEnvCalls)
	}
	if len(runEnvProjectDirs) != runEnvCalls {
		t.Errorf("distinct RunEnv.RunID count = %d; want %d (one identity per dispatch); two dispatches shared an ID",
			len(runEnvProjectDirs), runEnvCalls)
	}
	for runID, gotProjectDir := range runEnvProjectDirs {
		if gotProjectDir != projectDir {
			t.Errorf("RunEnv.ProjectDir for run %q = %q; want %q", runID, gotProjectDir, projectDir)
		}
	}

	eventTypes := collector.eventTypes()
	var startedCount, completedCount int
	for _, et := range eventTypes {
		switch et {
		case string(core.EventTypeRunStarted):
			startedCount++
		case string(core.EventTypeRunCompleted):
			completedCount++
		}
	}
	if startedCount < 2 {
		t.Errorf("run_started count = %d; want >= 2 (one per bead)", startedCount)
	}
	if completedCount < 2 {
		t.Errorf("run_completed count = %d; want >= 2 (one per bead)", completedCount)
	}
}

type closeErrFixtureLedger struct {
	mu       sync.Mutex
	inner    *stubBeadLedger
	closeErr error

	// closeCallCount counts CloseBead invocations. Without it this fixture cannot
	// tell "the close failed" from "the close was never reached", and for a long
	// time it was the second while claiming to test the first (hk-vzxg5).
	closeCallCount int
}

func (c *closeErrFixtureLedger) getCloseCallCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.closeCallCount
}

func (c *closeErrFixtureLedger) Ready(ctx context.Context) ([]core.BeadRecord, error) {
	return c.inner.Ready(ctx)
}

func (c *closeErrFixtureLedger) ShowBead(ctx context.Context, id core.BeadID) (core.BeadRecord, error) {
	return c.inner.ShowBead(ctx, id)
}

func (c *closeErrFixtureLedger) ClaimBead(ctx context.Context, d string, cfg brcli.TimeoutConfig, r core.RunID, tid core.TransitionID, bid core.BeadID) error {
	return c.inner.ClaimBead(ctx, d, cfg, r, tid, bid)
}

func (c *closeErrFixtureLedger) CloseBead(_ context.Context, _ string, _ brcli.TimeoutConfig, _ core.RunID, _ core.TransitionID, _ core.BeadID, _ bool) error {
	c.mu.Lock()
	c.closeCallCount++
	c.mu.Unlock()
	return c.closeErr
}

func (c *closeErrFixtureLedger) ReopenBead(ctx context.Context, d string, cfg brcli.TimeoutConfig, r core.RunID, tid core.TransitionID, bid core.BeadID, reason string) error {
	return c.inner.ReopenBead(ctx, d, cfg, r, tid, bid, reason)
}

// TestWorkLoop_CloseBeadError_EmitsRunFailed verifies that when CloseBead
// returns an error the work loop emits run_failed (not run_completed) so that
// JSONL and bead state remain consistent (hk-wfbxf: no split-brain).
//
// THE HANDLER MUST COMMIT (hk-vzxg5). This test used to run `sh -c "exit 0"`,
// which exits clean without advancing HEAD, so the run failed its pre-close guard
// and CloseBead was never called. The run_failed this test waited for was real
// but came from the guard, not from closeErr -- so the test passed without ever
// exercising the behavior it names. The pre-commit WorktreeFactory is not enough
// on its own: the commit has to come from the handler, inside the run. The
// closeCallCount assertion below is what keeps that honest.
func TestWorkLoop_CloseBeadError_EmitsRunFailed(t *testing.T) {
	t.Parallel()

	projectDir, _ := workloopFixtureProjectDir(t)
	workloopFixtureGitRepo(t, projectDir)

	const beadID = core.BeadID("test-bead-closeerr-001")
	inner := &stubBeadLedger{
		ready:  []core.BeadID{beadID},
		labels: workloopFixtureSingleLabels,
	}
	ledger := &closeErrFixtureLedger{
		inner:    inner,
		closeErr: errors.New("disk full"),
	}
	collector := &stubEventCollector{}

	deps := daemon.ExportedTestRuntime(daemon.TestRuntimeParams{
		BrAdapter:        ledger,
		Bus:              collector,
		ProjectDir:       projectDir,
		HandlerBinary:    "/bin/sh",
		HandlerArgs:      workloopFixtureAdvanceHeadHandlerArgs(t),
		AdapterRegistry2: NewSealedAdapterRegistryForTest(t),
		IntentLogDir:     filepath.Join(projectDir, ".harmonik", "beads-intents"),
		WorktreeFactory:  workloopFixturePreCommitWorktreeFactory,
	})

	ctx, cancel := context.WithTimeout(context.Background(), workLoopTestBudget)
	defer cancel()

	waitDone := make(chan struct{})
	go func() {
		defer close(waitDone)
		daemon.ExportedRunWorkLoop(ctx, deps)
	}()

	for {
		if ledger.getCloseCallCount() > 0 {
			types := collector.eventTypes()
			for _, et := range types {
				if et == string(core.EventTypeRunCompleted) || et == string(core.EventTypeRunFailed) {
					goto found
				}
			}
		}
		select {
		case <-ctx.Done():
			t.Fatalf("timed out waiting for a close attempt + run terminal; close attempts=%d, events: %v",
				ledger.getCloseCallCount(), collector.eventTypes())
		case <-time.After(50 * time.Millisecond):
		}
	}
found:

	cancel()
	awaitLoopTeardown(t, waitDone, "work loop")

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

type claimSemFixtureLedger struct {
	mu sync.Mutex

	// ready is the queue of bead IDs to hand out from Ready.
	ready []core.BeadID

	// closed records IDs of beads that have been closed.
	closed []core.BeadID

	// activeClaims is the number of ClaimBead calls currently executing.
	activeClaims atomic.Int64

	// peakClaims is the high-water mark of activeClaims across all calls.
	peakClaims atomic.Int64
}

func (c *claimSemFixtureLedger) Ready(_ context.Context) ([]core.BeadRecord, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.ready) == 0 {
		return []core.BeadRecord{}, nil
	}
	id := c.ready[0]
	c.ready = c.ready[1:]
	return []core.BeadRecord{{BeadID: id, Labels: workloopFixtureSingleLabels}}, nil
}

func (c *claimSemFixtureLedger) ShowBead(_ context.Context, id core.BeadID) (core.BeadRecord, error) {
	return core.BeadRecord{BeadID: id, Status: core.CoarseStatusOpen, Labels: workloopFixtureSingleLabels}, nil
}

func (c *claimSemFixtureLedger) ClaimBead(_ context.Context, _ string, _ brcli.TimeoutConfig, _ core.RunID, _ core.TransitionID, _ core.BeadID) error {
	current := c.activeClaims.Add(1)
	for {
		old := c.peakClaims.Load()
		if current <= old {
			break
		}
		if c.peakClaims.CompareAndSwap(old, current) {
			break
		}
	}
	time.Sleep(time.Millisecond)
	c.activeClaims.Add(-1)
	return nil
}

func (c *claimSemFixtureLedger) CloseBead(_ context.Context, _ string, _ brcli.TimeoutConfig, _ core.RunID, _ core.TransitionID, beadID core.BeadID, _ bool) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = append(c.closed, beadID)
	return nil
}

func (c *claimSemFixtureLedger) ReopenBead(_ context.Context, _ string, _ brcli.TimeoutConfig, _ core.RunID, _ core.TransitionID, beadID core.BeadID, _ string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.ready = append(c.ready, beadID)
	return nil
}

func (c *claimSemFixtureLedger) closedCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.closed)
}

// TestWorkLoop_ClaimSemaphore_BoundsClaimConcurrency verifies that with
// N=10 ready beads and MaxConcurrent=4 the claim semaphore (hk-e61c3.3)
// ensures concurrent ClaimBead calls never exceed MaxConcurrent, and all
// 10 beads are eventually closed.
//
// The outer poll loop is sequential, so in practice only one ClaimBead call
// executes at a time (peak = 1 ≤ MaxConcurrent = 4). The test asserts that
// invariant, verifies all beads close without deadlock, and is run with
// -race to confirm there are no data races introduced by the semaphore.
//
// Acceptance criteria (hk-e61c3.3):
//   - peakConcurrentClaims ≤ MaxConcurrent (4).
//   - All 10 beads are closed before the loop exits.
func TestWorkLoop_ClaimSemaphore_BoundsClaimConcurrency(t *testing.T) {
	t.Parallel()

	projectDir, _ := workloopFixtureProjectDir(t)
	workloopFixtureGitRepo(t, projectDir)

	const (
		beadCount     = 10
		maxConcurrent = 4
	)

	ready := make([]core.BeadID, beadCount)
	for i := range ready {
		ready[i] = core.BeadID("sem-bead-" + string(rune('A'+i)))
	}

	ledger := &claimSemFixtureLedger{ready: ready}
	collector := &stubEventCollector{}

	deps := daemon.ExportedTestRuntime(daemon.TestRuntimeParams{
		BrAdapter:        ledger,
		Bus:              collector,
		ProjectDir:       projectDir,
		HandlerBinary:    "/bin/sh",
		HandlerArgs:      workloopFixtureAdvanceHeadHandlerArgs(t),
		IntentLogDir:     filepath.Join(projectDir, ".harmonik", "beads-intents"),
		AdapterRegistry2: NewSealedAdapterRegistryForTest(t),
		MaxConcurrent:    maxConcurrent,
	})

	ctx, cancel := context.WithTimeout(context.Background(), workLoopTestBudget)
	defer cancel()

	loopDone := make(chan struct{})
	go func() {
		defer close(loopDone)
		daemon.ExportedRunWorkLoop(ctx, deps)
	}()

	for ledger.closedCount() < beadCount {
		select {
		case <-ctx.Done():
			t.Fatalf("timed out waiting for all %d beads to close; closed=%d peak_concurrent_claims=%d",
				beadCount, ledger.closedCount(), ledger.peakClaims.Load())
		case <-time.After(50 * time.Millisecond):
		}
	}

	cancel()
	awaitLoopTeardown(t, loopDone, "work loop")

	peak := ledger.peakClaims.Load()
	if peak > maxConcurrent {
		t.Errorf("hk-e61c3.3: peakConcurrentClaims = %d; want <= %d (semaphore must bound concurrent claims)",
			peak, maxConcurrent)
	}

	if n := ledger.closedCount(); n != beadCount {
		t.Errorf("closedCount = %d; want %d (all beads must close under semaphore)", n, beadCount)
	}
}
