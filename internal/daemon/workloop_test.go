package daemon_test

// workloop_test.go — tests for the main work loop (hk-ecrxy).
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
	"sync/atomic"
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
	// Resolve symlinks so that br — which rejects symlinked paths — receives the
	// canonical path. On macOS t.TempDir() returns /var/folders/... but /var is a
	// symlink to /private/var, which br refuses (same fix as smokeFixtureProjectDir).
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

// workloopFixtureGitRepo initialises a bare git repository with a single
// initial commit in dir.  Required because CreateWorktree calls `git worktree
// add` and needs an existing git repo with a resolvable HEAD.
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

	// Create a bare clone as "origin" so that mergeRunBranchToMain's
	// `git push origin main` step succeeds in tests that produce worktree
	// commits (e.g. via workloopFixturePreCommitWorktreeFactory).
	bareDir := dir + "-bare"
	//nolint:gosec // G204: git args are test-internal literals; not user input
	cloneCmd := exec.CommandContext(t.Context(), "git", "clone", "--bare", dir, bareDir)
	cloneOut, cloneErr := cloneCmd.CombinedOutput()
	if cloneErr != nil {
		t.Fatalf("workloopFixtureGitRepo: git clone --bare: %v\n%s", cloneErr, cloneOut)
	}
	run("remote", "add", "origin", bareDir)
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

// workloopFixtureSingleLabels is the label set every work-loop fixture bead
// carries. The reason it is not empty is written out on stubBeadLedger.labels:
// an unlabelled bead selects the reviewed graph, whose commit gate runs
// `make full` in a fixture repo that holds one README and no Makefile.
var workloopFixtureSingleLabels = []string{"workflow:single"}

// stubBeadLedger implements brcli.Adapter-compatible calls as a lightweight
// in-memory stub for work loop tests.  Concurrency: all methods are safe to
// call concurrently.
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
	// Dequeue one bead per Ready call — simulates a draining queue.
	id := s.ready[0]
	s.ready = s.ready[1:]
	return []core.BeadRecord{{BeadID: id, Labels: s.labels}}, nil
}

func (s *stubBeadLedger) ShowBead(_ context.Context, id core.BeadID) (core.BeadRecord, error) {
	// Stub always reports "open" — pre-claim guard passes unconditionally.
	//
	// The labels must be here as well as on Ready. The work loop HYDRATES from
	// ShowBead and overwrites whatever Ready reported, because `br ready --format
	// json` omits the labels field. A stub that labels only Ready loses them.
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

// ─────────────────────────────────────────────────────────────────────────────
// Stub event collector
// ─────────────────────────────────────────────────────────────────────────────

// stubEventCollector is an EventEmitter that records emitted events.
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

// collect records a full core.Event envelope (including EventID) as emitted by
// the real event bus. Used by test fixtures that wire a bus subscription instead
// of a direct EventEmitter stub.
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

// allEvents returns a snapshot of all recorded events (type + raw payload).
func (s *stubEventCollector) allEvents() []stubEmittedEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]stubEmittedEvent, len(s.events))
	copy(out, s.events)
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
		ready:  []core.BeadID{beadID},
		labels: workloopFixtureSingleLabels,
	}
	collector := &stubEventCollector{}

	// The handler is a shell that makes one empty commit and exits 0. The commit
	// happens DURING the run, which is what the node's no-advance guard asks for;
	// see workloopFixtureAdvanceHeadHandlerArgs.
	deps := daemon.ExportedTestRuntime(daemon.TestRuntimeParams{
		BrAdapter:        ledger,
		Bus:              collector,
		ProjectDir:       projectDir,
		HandlerBinary:    "/bin/sh",
		HandlerArgs:      workloopFixtureAdvanceHeadHandlerArgs(t),
		AdapterRegistry2: NewSealedAdapterRegistryForTest(t),
		IntentLogDir:     filepath.Join(projectDir, ".harmonik", "beads-intents"),
	})

	// Real productionWorktreeFactory + buildClaudeLaunchSpec run; stopHookGrace
	// (~3s) fires per bead (hk-ngw3d).
	ctx, cancel := context.WithTimeout(context.Background(), workLoopTestBudget)
	defer cancel()

	// The loop will dispatch the bead, close it, then find the queue empty and
	// sleep. Cancel the context a short time after to stop the loop cleanly.
	waitDone := make(chan struct{})
	go func() {
		defer close(waitDone)
		daemon.ExportedRunWorkLoop(ctx, deps)
	}()

	// Poll until the bead is closed. The context is the one budget — a second,
	// smaller poll deadline only adds a way to fail while the loop still works.
	for len(ledger.closedIDs()) == 0 {
		select {
		case <-ctx.Done():
			t.Fatal("timed out waiting for bead to be closed")
		case <-time.After(50 * time.Millisecond):
		}
	}

	// Cancel the context to stop the loop goroutine.
	cancel()
	awaitLoopTeardown(t, waitDone, "work loop")

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

	deps := daemon.ExportedTestRuntime(daemon.TestRuntimeParams{
		BrAdapter:        ledger,
		Bus:              collector,
		ProjectDir:       projectDir,
		HandlerBinary:    "/bin/sh",
		HandlerArgs:      []string{"-c", "exit 1"},
		AdapterRegistry2: NewSealedAdapterRegistryForTest(t),
		IntentLogDir:     filepath.Join(projectDir, ".harmonik", "beads-intents"),
	})

	// Real productionWorktreeFactory + buildClaudeLaunchSpec run; stopHookGrace
	// (~3s) fires per bead (hk-ngw3d).
	ctx, cancel := context.WithTimeout(context.Background(), workLoopTestBudget)
	defer cancel()

	waitDone := make(chan struct{})
	go func() {
		defer close(waitDone)
		daemon.ExportedRunWorkLoop(ctx, deps)
	}()

	// Poll until the bead is reopened, bounded by the context.
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
// TestWorkLoop_TwoConcurrentBeads — hk-e61c3.2 concurrent dispatch at N=2
// ─────────────────────────────────────────────────────────────────────────────

// concurrentFixtureLedger is a stub beadLedger used by
// TestWorkLoop_TwoConcurrentBeads to observe when two beads are simultaneously
// claimed.  It records peak in-flight count by tracking how many CloseBead
// calls have not yet occurred at the time each ClaimBead succeeds.
//
// Helper prefix: concurrentFixture (per implementer-protocol §Helper-prefix;
// bead hk-e61c3.2).
type concurrentFixtureLedger struct {
	mu sync.Mutex

	// ready is the queue of bead IDs to hand out from Ready.
	ready []core.BeadID

	// inFlight tracks how many beads are currently claimed-but-not-yet-closed.
	inFlight int

	// peakInFlight is the high-water mark of inFlight observed across all
	// ClaimBead calls.
	peakInFlight int

	// closed records IDs of beads that have been closed.
	closed []core.BeadID

	// claimedCh is closed once two beads have been simultaneously claimed.
	// Used as a rendezvous so the test can assert peak concurrency.
	claimedCh chan struct{}

	// claimedOnce guards the close of claimedCh.
	claimedOnce sync.Once
}

func (c *concurrentFixtureLedger) ShowBead(_ context.Context, id core.BeadID) (core.BeadRecord, error) {
	// Stub always reports "open" — pre-claim guard passes unconditionally.
	// workflow:single is load-bearing; see stubBeadLedger.labels for why.
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

	// Signal once two beads are simultaneously in-flight.
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
	c.ready = append(c.ready, beadID)
	return nil
}

func (c *concurrentFixtureLedger) closedCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.closed)
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

	// Handler: sleep briefly so both goroutines are simultaneously in-flight,
	// then exit 0 so both beads are closed.
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
			runEnvProjectDirs[runID] = gotProjectDir
			runEnvMu.Unlock()
			return daemon.ExportedProductionWorktreeFactory(ctx, gotProjectDir, runID, headSHA)
		},
	})

	// Real productionWorktreeFactory + buildClaudeLaunchSpec run concurrently for
	// both beads; stopHookGrace (~3s) per bead runs in parallel at MaxConcurrent=2
	// (hk-ngw3d).
	ctx, cancel := context.WithTimeout(context.Background(), workLoopTestBudget)
	defer cancel()

	loopDone := make(chan struct{})
	go func() {
		defer close(loopDone)
		daemon.ExportedRunWorkLoop(ctx, deps)
	}()

	// Wait until both beads are simultaneously claimed, bounded by the context.
	select {
	case <-ledger.claimedCh:
		// Both beads claimed — concurrency confirmed.
	case <-ctx.Done():
		t.Fatal("timed out waiting for two simultaneous in-flight beads at MaxConcurrent=2")
	}

	// Wait for both beads to close.
	for ledger.closedCount() < 2 {
		select {
		case <-ctx.Done():
			t.Fatalf("timed out waiting for both beads to close; closed=%d", ledger.closedCount())
		case <-time.After(50 * time.Millisecond):
		}
	}

	cancel()
	awaitLoopTeardown(t, loopDone, "work loop")

	// Assert peak concurrency was 2.
	if p := ledger.peak(); p < 2 {
		t.Errorf("peakInFlight = %d; want >= 2 (two beads must be simultaneously in-flight at MaxConcurrent=2)", p)
	}

	// Assert both beads were closed (not reopened).
	if n := ledger.closedCount(); n != 2 {
		t.Errorf("closedCount = %d; want 2", n)
	}

	// The worktree port is the first run-path consumer of ProjectDir and RunID.
	// Both concurrent runs must keep the shared directory and distinct identities.
	runEnvMu.Lock()
	defer runEnvMu.Unlock()
	if len(runEnvProjectDirs) != 2 {
		t.Errorf("distinct RunEnv.RunID count = %d; want 2", len(runEnvProjectDirs))
	}
	for runID, gotProjectDir := range runEnvProjectDirs {
		if gotProjectDir != projectDir {
			t.Errorf("RunEnv.ProjectDir for run %q = %q; want %q", runID, gotProjectDir, projectDir)
		}
	}

	// Assert run_started and run_completed events emitted for both runs.
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

func (c *closeErrFixtureLedger) ShowBead(ctx context.Context, id core.BeadID) (core.BeadRecord, error) {
	return c.inner.ShowBead(ctx, id)
}

func (c *closeErrFixtureLedger) ClaimBead(ctx context.Context, d string, cfg brcli.TimeoutConfig, r core.RunID, tid core.TransitionID, bid core.BeadID) error {
	return c.inner.ClaimBead(ctx, d, cfg, r, tid, bid)
}

func (c *closeErrFixtureLedger) CloseBead(_ context.Context, _ string, _ brcli.TimeoutConfig, _ core.RunID, _ core.TransitionID, _ core.BeadID, _ bool) error {
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
	deps := daemon.ExportedTestRuntime(daemon.TestRuntimeParams{
		BrAdapter:        ledger,
		Bus:              collector,
		ProjectDir:       projectDir,
		HandlerBinary:    "/bin/sh",
		HandlerArgs:      []string{"-c", "exit 0"},
		AdapterRegistry2: NewSealedAdapterRegistryForTest(t),
		IntentLogDir:     filepath.Join(projectDir, ".harmonik", "beads-intents"),
		WorktreeFactory:  workloopFixturePreCommitWorktreeFactory,
	})

	// Real productionWorktreeFactory + buildClaudeLaunchSpec run; stopHookGrace
	// (~3s) fires per bead (hk-ngw3d).
	ctx, cancel := context.WithTimeout(context.Background(), workLoopTestBudget)
	defer cancel()

	waitDone := make(chan struct{})
	go func() {
		defer close(waitDone)
		daemon.ExportedRunWorkLoop(ctx, deps)
	}()

	// Poll until a run_failed event is emitted, bounded by the context.
	for {
		types := collector.eventTypes()
		for _, et := range types {
			if et == string(core.EventTypeRunFailed) {
				goto found
			}
		}
		select {
		case <-ctx.Done():
			t.Fatalf("timed out waiting for run_failed event; got events: %v", collector.eventTypes())
		case <-time.After(50 * time.Millisecond):
		}
	}
found:

	cancel()
	awaitLoopTeardown(t, waitDone, "work loop")

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

// ─────────────────────────────────────────────────────────────────────────────
// TestWorkLoop_ClaimSemaphore — hk-e61c3.3 claim semaphore bounded concurrency
// ─────────────────────────────────────────────────────────────────────────────

// claimSemFixtureLedger is a stub beadLedger for
// TestWorkLoop_ClaimSemaphore_BoundsClaimConcurrency. It tracks the peak number
// of simultaneously active ClaimBead calls via an atomic counter.
//
// Helper prefix: claimSemFixture (per implementer-protocol.md §Helper-prefix;
// bead hk-e61c3.3).
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
	// workflow:single is load-bearing; see stubBeadLedger.labels for why.
	return core.BeadRecord{BeadID: id, Status: core.CoarseStatusOpen, Labels: workloopFixtureSingleLabels}, nil
}

func (c *claimSemFixtureLedger) ClaimBead(_ context.Context, _ string, _ brcli.TimeoutConfig, _ core.RunID, _ core.TransitionID, _ core.BeadID) error {
	// Increment active counter and update peak before doing work.
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
	// Yield briefly to give the race detector a chance to observe concurrent
	// access — in the sequential outer loop this never overlaps, but the
	// instrumentation is useful when the test runs with -race.
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
	// Re-enqueue the bead so transient errors (e.g. git worktree races under
	// parallel test load) do not permanently lose beads and deadlock the poll loop
	// (hk-kqdpf.1).
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

	// Handler makes one empty commit and exits — we want all 10 beads to process
	// quickly, and the commit has to land during the run for the node to pass.
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

	// Real buildClaudeLaunchSpec + productionWorktreeFactory run; stopHookGrace
	// (~3s) fires per bead, and this test dispatches ten of them four at a time,
	// so three waves of grace are a 9-second floor before any git work (hk-ngw3d).
	ctx, cancel := context.WithTimeout(context.Background(), workLoopTestBudget)
	defer cancel()

	loopDone := make(chan struct{})
	go func() {
		defer close(loopDone)
		daemon.ExportedRunWorkLoop(ctx, deps)
	}()

	// Poll until all 10 beads are closed, bounded by the context.
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

	// Assert concurrent ClaimBead calls never exceeded MaxConcurrent.
	peak := ledger.peakClaims.Load()
	if peak > maxConcurrent {
		t.Errorf("hk-e61c3.3: peakConcurrentClaims = %d; want <= %d (semaphore must bound concurrent claims)",
			peak, maxConcurrent)
	}

	// Assert all beads were closed (not reopened due to semaphore deadlock).
	if n := ledger.closedCount(); n != beadCount {
		t.Errorf("closedCount = %d; want %d (all beads must close under semaphore)", n, beadCount)
	}
}
