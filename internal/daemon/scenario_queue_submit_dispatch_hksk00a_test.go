//go:build scenario

package daemon_test

// scenario_queue_submit_dispatch_hksk00a_test.go — submit-RPC → dispatch → close
// bridge scenario test (hk-sk00a).
//
// Gaps covered:
//   - hk-24xn1: idle daemon does not see a newly submitted queue until the wake
//     channel fires; this test submits via HandleQueueSubmit while the daemon is
//     idle and asserts the bead reaches "closed".
//   - hk-nbjht: a deferred-for-ledger-dep item in a stream group is never
//     re-evaluated after its in-group blocker completes; this test submits [A, B]
//     where B is deferred behind A, lets A close, and asserts B un-defers and
//     also reaches "closed".
//   - hk-4ie1z: exercised vacuously — any run that reaches run_completed without
//     a commit would formerly false-close the bead; the twin scenario emits a
//     valid outcome so the run completes cleanly.
//
// Bridge: queue_setqueue_wiring_test.go (HandleQueueSubmit path, no dispatch)
//         scenario_happypath_n1_test.go (dispatch path, no queue submit)
// Both paths are connected here end-to-end.
//
// Helper prefix: queueSubmitDispatch (per implementer-protocol.md §Helper-prefix).
//
// Spec refs:
//   - specs/queue-model.md §2.8 QM-025 (deferred-for-ledger-dep)
//   - specs/queue-model.md §3.2 QM-002 (queue-active → idle wake-on-submit)
//   - specs/execution-model.md §7.4 TS-1 (queue-pull dispatch)
//   - specs/scenario-harness.md §4 (assertion vocabulary)
//
// Bead: hk-sk00a.

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/daemon/scenariotest"
	"github.com/gregberns/harmonik/internal/queue"
)

// ─────────────────────────────────────────────────────────────────────────────
// Shared fixture helpers (queueSubmitDispatch prefix)
// ─────────────────────────────────────────────────────────────────────────────

// queueSubmitDispatchProjectDir creates the minimal project directory layout.
// Returns (projectDir, jsonlPath).
func queueSubmitDispatchProjectDir(t *testing.T) (string, string) {
	t.Helper()
	raw := t.TempDir()
	dir, err := filepath.EvalSymlinks(raw)
	require.NoError(t, err, "queueSubmitDispatchProjectDir: EvalSymlinks")
	for _, sub := range []string{
		filepath.Join(".harmonik", "events"),
		filepath.Join(".harmonik", "beads-intents"),
		filepath.Join(".harmonik", "queues"),
	} {
		//nolint:gosec // G301: 0755 matches .harmonik dir conventions
		require.NoError(t, os.MkdirAll(filepath.Join(dir, sub), 0o755),
			"queueSubmitDispatchProjectDir: MkdirAll %s", sub)
	}
	return dir, filepath.Join(dir, ".harmonik", "events", "events.jsonl")
}

// queueSubmitDispatchGitRepo initialises a git repository with one commit in dir.
func queueSubmitDispatchGitRepo(t *testing.T, dir string) {
	t.Helper()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.CommandContext(t.Context(), "git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "queueSubmitDispatchGitRepo: git %v\n%s", args, out)
	}
	run("init", "--initial-branch=main")
	run("config", "user.email", "test@harmonik.local")
	run("config", "user.name", "Harmonik Test")
	readmePath := filepath.Join(dir, "README")
	require.NoError(t, os.WriteFile(readmePath, []byte("queue-submit-dispatch scenario\n"), 0o644),
		"queueSubmitDispatchGitRepo: WriteFile README")
	run("add", "README")
	run("commit", "-m", "Initial commit")
}

// queueSubmitDispatchBrPath returns the path to the real br binary. Skips if absent.
func queueSubmitDispatchBrPath(t *testing.T) string {
	t.Helper()
	path, err := exec.LookPath("br")
	if err != nil {
		t.Skip("br required for queue-submit-dispatch scenario test (not on PATH)")
	}
	return path
}

// queueSubmitDispatchBrWrapper writes a /bin/sh wrapper that invokes brPath
// with --db dbPath prepended to all args. Returns the wrapper path.
func queueSubmitDispatchBrWrapper(t *testing.T, brPath, dbPath string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "br")
	content := "#!/bin/sh\nexec " + brPath + " --db " + dbPath + " \"$@\"\n"
	//nolint:gosec // G306: script is test-only; 0755 required for execution
	require.NoError(t, os.WriteFile(path, []byte(content), 0o755),
		"queueSubmitDispatchBrWrapper: WriteFile")
	return path
}

// queueSubmitDispatchTwinWrapper writes a /bin/sh wrapper that invokes twinPath
// with --scenario single-happy-path, ignoring all args from the daemon.
func queueSubmitDispatchTwinWrapper(t *testing.T, twinPath string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "twin-wrapper.sh")
	content := "#!/bin/sh\nexec " + twinPath + " --scenario single-happy-path\n"
	//nolint:gosec // G306: script is test-only; 0755 required for execution
	require.NoError(t, os.WriteFile(path, []byte(content), 0o755),
		"queueSubmitDispatchTwinWrapper: WriteFile")
	return path
}

// queueSubmitDispatchInitBr initialises a br workspace in projectDir.
// Creates one open bead and returns its ID.
func queueSubmitDispatchInitBr(t *testing.T, brPath, projectDir, brWrapper string) core.BeadID {
	t.Helper()
	initCmd := exec.CommandContext(t.Context(), brPath, "init", "--prefix", "qsd")
	initCmd.Dir = projectDir
	out, err := initCmd.CombinedOutput()
	require.NoError(t, err, "queueSubmitDispatchInitBr: br init\n%s", out)

	createCmd := exec.CommandContext(t.Context(), brWrapper, "create",
		"queue-submit idle-wake test bead", "--status", "open", "--silent")
	createOut, createErr := createCmd.CombinedOutput()
	require.NoError(t, createErr, "queueSubmitDispatchInitBr: br create\n%s", createOut)
	id := strings.TrimSpace(string(createOut))
	require.NotEmpty(t, id, "queueSubmitDispatchInitBr: br create returned empty ID")
	return core.BeadID(id)
}

// queueSubmitDispatchInitBrWithDep initialises a br workspace with two open
// beads (A and B) and a dependency edge B→A (B depends on A; A blocks B).
// Returns (aID, bID).
func queueSubmitDispatchInitBrWithDep(t *testing.T, brPath, projectDir, brWrapper string) (core.BeadID, core.BeadID) {
	t.Helper()
	initCmd := exec.CommandContext(t.Context(), brPath, "init", "--prefix", "qsd2")
	initCmd.Dir = projectDir
	out, err := initCmd.CombinedOutput()
	require.NoError(t, err, "queueSubmitDispatchInitBrWithDep: br init\n%s", out)

	// Create bead A (blocker).
	createA := exec.CommandContext(t.Context(), brWrapper, "create",
		"deferred-undefer: bead A (blocker)", "--status", "open", "--silent")
	outA, errA := createA.CombinedOutput()
	require.NoError(t, errA, "queueSubmitDispatchInitBrWithDep: br create A\n%s", outA)
	aID := core.BeadID(strings.TrimSpace(string(outA)))
	require.NotEmpty(t, aID, "queueSubmitDispatchInitBrWithDep: br create A returned empty ID")

	// Create bead B (blocked by A).
	createB := exec.CommandContext(t.Context(), brWrapper, "create",
		"deferred-undefer: bead B (blocked)", "--status", "open", "--silent")
	outB, errB := createB.CombinedOutput()
	require.NoError(t, errB, "queueSubmitDispatchInitBrWithDep: br create B\n%s", outB)
	bID := core.BeadID(strings.TrimSpace(string(outB)))
	require.NotEmpty(t, bID, "queueSubmitDispatchInitBrWithDep: br create B returned empty ID")

	// Add dependency: B depends on A.
	// "br dep add <issue> <depends-on>" — issue=B, depends-on=A.
	depCmd := exec.CommandContext(t.Context(), brWrapper, "dep", "add",
		string(bID), string(aID))
	depOut, depErr := depCmd.CombinedOutput()
	require.NoError(t, depErr, "queueSubmitDispatchInitBrWithDep: br dep add B A\n%s", depOut)

	return aID, bID
}

// queueSubmitDispatchPollBeadClosed polls br show <id> until status=="closed"
// or budget expires. Returns true when closed.
func queueSubmitDispatchPollBeadClosed(t *testing.T, brWrapper string, id core.BeadID, budget time.Duration) bool {
	t.Helper()
	deadline := time.Now().Add(budget)
	for time.Now().Before(deadline) {
		cmd := exec.CommandContext(t.Context(), brWrapper, "show", string(id), "--format", "json")
		out, err := cmd.Output()
		if err == nil {
			var records []struct {
				Status string `json:"status"`
			}
			if json.Unmarshal(out, &records) == nil && len(records) > 0 && records[0].Status == "closed" {
				return true
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	return false
}

// queueSubmitDispatchWaitRunTerminal polls the JSONL log for a terminal
// run event (run_completed or run_failed) for up to budget. Returns true when found.
func queueSubmitDispatchWaitRunTerminal(t *testing.T, jsonlPath string, budget time.Duration) bool {
	t.Helper()
	deadline := time.Now().Add(budget)
	for time.Now().Before(deadline) {
		//nolint:gosec // G304: path is t.TempDir()-based; not user input
		f, err := os.Open(jsonlPath)
		if err == nil {
			found := false
			scanner := bufio.NewScanner(f)
			for scanner.Scan() {
				line := scanner.Text()
				if strings.Contains(line, string(core.EventTypeRunCompleted)) ||
					strings.Contains(line, string(core.EventTypeRunFailed)) {
					found = true
					break
				}
			}
			if closeErr := f.Close(); closeErr != nil {
				t.Logf("queueSubmitDispatchWaitRunTerminal: close: %v", closeErr)
			}
			if found {
				return true
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	return false
}

// queueSubmitDispatchCountRunTerminal returns the number of run_completed or
// run_failed events in the JSONL log.
func queueSubmitDispatchCountRunTerminal(t *testing.T, jsonlPath string) int {
	t.Helper()
	//nolint:gosec // G304: path is t.TempDir()-based; not user input
	f, err := os.Open(jsonlPath)
	if err != nil {
		return 0
	}
	defer func() {
		if closeErr := f.Close(); closeErr != nil {
			t.Logf("queueSubmitDispatchCountRunTerminal: close: %v", closeErr)
		}
	}()
	n := 0
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.Contains(line, string(core.EventTypeRunCompleted)) ||
			strings.Contains(line, string(core.EventTypeRunFailed)) {
			n++
		}
	}
	return n
}

// queueSubmitDispatchCountRunStarted returns the number of run_started events
// in the JSONL log.
func queueSubmitDispatchCountRunStarted(t *testing.T, jsonlPath string) int {
	t.Helper()
	//nolint:gosec // G304: path is t.TempDir()-based; not user input
	f, err := os.Open(jsonlPath)
	if err != nil {
		return 0
	}
	defer func() {
		if closeErr := f.Close(); closeErr != nil {
			t.Logf("queueSubmitDispatchCountRunStarted: close: %v", closeErr)
		}
	}()
	n := 0
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		if strings.Contains(scanner.Text(), string(core.EventTypeRunStarted)) {
			n++
		}
	}
	return n
}

// ─────────────────────────────────────────────────────────────────────────────
// Fake BeadLedger implementations
// ─────────────────────────────────────────────────────────────────────────────

// qsdOpenLedger is a minimal queue.BeadLedger that marks every bead as open
// with no blocking edges. Used when the test beads have no dependencies.
type qsdOpenLedger struct{}

func (l *qsdOpenLedger) LookupStatus(_ context.Context, _ core.BeadID) (queue.BeadStatus, error) {
	return queue.BeadStatusOpen, nil
}

func (l *qsdOpenLedger) BlocksEdge(_ context.Context, _, _ core.BeadID) (bool, error) {
	return false, nil
}

// qsdBlockingLedger is a minimal queue.BeadLedger that marks every bead as
// open, and reports that blocker blocks blocked for the specified pair.
// Used to trigger submit-time QM-025 deferral of the blocked item.
type qsdBlockingLedger struct {
	blocker core.BeadID
	blocked core.BeadID
}

func (l *qsdBlockingLedger) LookupStatus(_ context.Context, _ core.BeadID) (queue.BeadStatus, error) {
	return queue.BeadStatusOpen, nil
}

func (l *qsdBlockingLedger) BlocksEdge(_ context.Context, blocker, blocked core.BeadID) (bool, error) {
	return blocker == l.blocker && blocked == l.blocked, nil
}

// ─────────────────────────────────────────────────────────────────────────────
// TestScenario_QueueSubmit_IdleWake_hk24xn1
// ─────────────────────────────────────────────────────────────────────────────

// TestScenario_QueueSubmit_IdleWake_hk24xn1 verifies that a queue submitted
// via HandleQueueSubmit to an IDLE daemon (no active queue, NoAutoPull=true)
// causes the daemon to wake on the submit-wake channel, dispatch the bead, and
// close it — end-to-end from submit RPC through dispatch to bead closure.
//
// This pins the gap in hk-24xn1: the idle work loop must listen on
// QueueStore.WakeCh() so a newly submitted queue does not wait for the next
// poll tick.
//
// Setup:
//  1. TempDir project with git + br DB.
//  2. One open bead seeded via br create.
//  3. daemon.Start wired with harmonik-twin-claude and NoAutoPull=true.
//  4. After daemon starts (idle), submit via HandlerAdapter.HandleQueueSubmit.
//
// Assertions:
//  1. run_started event appears in JSONL (dispatch occurred).
//  2. run_completed event appears in JSONL (bead ran to completion).
//  3. Bead status == "closed" in the br DB.
//
// Bead: hk-sk00a; regression guard for hk-24xn1.
func TestScenario_QueueSubmit_IdleWake_hk24xn1(t *testing.T) {
	// Not parallel: uses os.Setenv(HARMONIK_CLAUDE_CONFIG_PATH).
	twinPath, ok := scenariotest.TwinBinaryPath()
	if !ok {
		t.Skip("harmonik-twin-claude binary not found; set HARMONIK_TWIN_CLAUDE or build the binary")
	}

	realBrPath := queueSubmitDispatchBrPath(t)
	projectDir, jsonlPath := queueSubmitDispatchProjectDir(t)
	queueSubmitDispatchGitRepo(t, projectDir)

	dbPath := filepath.Join(projectDir, ".beads", "beads.db")
	brWrapper := queueSubmitDispatchBrWrapper(t, realBrPath, dbPath)
	beadID := queueSubmitDispatchInitBr(t, realBrPath, projectDir, brWrapper)
	t.Logf("queueSubmitDispatch idle-wake: seeded bead %s", beadID)

	twinWrapper := queueSubmitDispatchTwinWrapper(t, twinPath)

	claudeConfigPath := filepath.Join(t.TempDir(), ".claude.json")
	require.NoError(t, os.Setenv("HARMONIK_CLAUDE_CONFIG_PATH", claudeConfigPath))
	t.Cleanup(func() { _ = os.Unsetenv("HARMONIK_CLAUDE_CONFIG_PATH") })

	// Pre-create the QueueStore so the test holds the pointer for submit.
	qs := daemon.ExportedNewQueueStore()

	loopCtx, loopCancel := context.WithCancel(context.Background())
	defer loopCancel()

	cfg := daemon.Config{
		ProjectDir:            projectDir,
		JSONLLogPath:          jsonlPath,
		BrPath:                brWrapper,
		HandlerBinary:         twinWrapper,
		HandlerEnv:            nil,
		SkipWALCheckpoint:     true,
		SkipBrHistoryRotation: true,
		AgentReadyTimeout:     5 * time.Second,
		NoAutoPull:            true, // queue-only dispatch; prevents br-ready pre-emption (hk-24xn1)
		QueueStore:            qs,
		LogWriter:             testLogWriter{t: t},
	}

	startDone := make(chan error, 1)
	go func() {
		startDone <- daemon.Start(loopCtx, cfg)
	}()

	// Brief pause: give the daemon's workloop time to reach its idle select.
	// The exact timing is not critical; the wake channel is buffered (depth 1)
	// so a submit before the loop reaches select still wakes it.
	time.Sleep(200 * time.Millisecond)

	// ── Submit a queue with the single open bead ────────────────────────────

	// The HandlerAdapter uses the same QueueStore the daemon holds; SetQueue
	// fires the wake channel (hk-24xn1) so the idle loop wakes immediately.
	adapter := queue.NewHandlerAdapter(
		&qsdOpenLedger{}, // all beads open, no deps
		projectDir,
		qs,  // same QueueStore the daemon is watching
		nil, // no event bus seam needed in the test
	)

	submitReq := queue.QueueSubmitRequest{
		SchemaVersion: 1,
		Groups: []queue.Group{
			{
				Kind:  queue.GroupKindStream,
				Items: []queue.Item{{BeadID: beadID, Status: queue.ItemStatusPending}},
			},
		},
	}
	params, err := json.Marshal(submitReq)
	require.NoError(t, err, "marshal QueueSubmitRequest")

	raw, rpcErr := adapter.HandleQueueSubmit(t.Context(), params)
	require.Nil(t, rpcErr, "HandleQueueSubmit: unexpected RPCError: %v", rpcErr)
	require.NotNil(t, raw, "HandleQueueSubmit: nil response")

	var submitResp queue.QueueSubmitResponse
	require.NoError(t, json.Unmarshal(raw, &submitResp), "decode QueueSubmitResponse")
	t.Logf("queueSubmitDispatch idle-wake: submitted queue_id=%s", submitResp.QueueID)

	// ── Wait for the single terminal event ─────────────────────────────────

	const terminalBudget = 25 * time.Second
	scenariotest.MustCompleteWithin(t, jsonlPath, "", nil, terminalBudget, func() {
		for {
			if queueSubmitDispatchWaitRunTerminal(t, jsonlPath, 50*time.Millisecond) {
				return
			}
		}
	})

	loopCancel()
	scenariotest.MustCompleteWithin(t, jsonlPath, "", nil, 5*time.Second, func() {
		if err := <-startDone; err != nil {
			t.Errorf("daemon.Start returned error after cancel: %v", err)
		}
	})

	// ── Assertions ──────────────────────────────────────────────────────────

	// 1. run_started must appear: the daemon dispatched the bead.
	require.True(t,
		scenariotest.WaitForEvent(t, jsonlPath, string(core.EventTypeRunStarted), "", 100*time.Millisecond),
		"run_started must appear in JSONL after idle-wake dispatch (hk-24xn1)")

	// 2. run_completed must appear: the bead ran to completion.
	require.True(t,
		scenariotest.WaitForEvent(t, jsonlPath, string(core.EventTypeRunCompleted), "", 100*time.Millisecond),
		"run_completed must appear in JSONL after dispatch (hk-24xn1)")

	// 3. Bead must be closed in br.
	scenariotest.AssertBeadStatus(t, brWrapper, string(beadID), "closed")

	// 4. Causality: run_started must precede run_completed.
	scenariotest.AssertEventSequence(t, jsonlPath, []scenariotest.ExpectedEvent{
		{Type: string(core.EventTypeRunStarted)},
		{Type: string(core.EventTypeRunCompleted)},
	})

	t.Logf("TestScenario_QueueSubmit_IdleWake_hk24xn1: PASS bead=%s", beadID)
}

// ─────────────────────────────────────────────────────────────────────────────
// TestScenario_QueueSubmit_DeferredUndefer_hknbjht
// ─────────────────────────────────────────────────────────────────────────────

// TestScenario_QueueSubmit_DeferredUndefer_hknbjht verifies the full
// deferred-for-ledger-dep lifecycle end-to-end through daemon.Start + real br
// + harmonik-twin-claude:
//
//  1. Submit a stream queue containing [A(pending), B(deferred)] where B is
//     deferred at submit time because A blocks B per the ledger.
//  2. A dispatches first (B is still deferred; head-of-line is A).
//  3. After A completes, evaluateGroupAdvanceWithOutcome wakes the loop.
//  4. On the next workloop tick, ReevaluateDeferred detects A is terminal in
//     the queue and un-defers B to pending.
//  5. B dispatches and completes.
//  6. Both A and B are "closed" in br.
//
// The br dependency edge (B depends on A) is set up via `br dep add` so the
// daemon's internal brQueueLedger reports BlocksEdge(A, B) = true during the
// ReevaluateDeferred pass (§2.8 un-defer condition check).
//
// Bead: hk-sk00a; regression guard for hk-nbjht.
func TestScenario_QueueSubmit_DeferredUndefer_hknbjht(t *testing.T) {
	// Not parallel: uses os.Setenv(HARMONIK_CLAUDE_CONFIG_PATH).
	twinPath, ok := scenariotest.TwinBinaryPath()
	if !ok {
		t.Skip("harmonik-twin-claude binary not found; set HARMONIK_TWIN_CLAUDE or build the binary")
	}

	realBrPath := queueSubmitDispatchBrPath(t)
	projectDir, jsonlPath := queueSubmitDispatchProjectDir(t)
	queueSubmitDispatchGitRepo(t, projectDir)

	dbPath := filepath.Join(projectDir, ".beads", "beads.db")
	brWrapper := queueSubmitDispatchBrWrapper(t, realBrPath, dbPath)
	aID, bID := queueSubmitDispatchInitBrWithDep(t, realBrPath, projectDir, brWrapper)
	t.Logf("queueSubmitDispatch deferred-undefer: A=%s B=%s (B depends on A)", aID, bID)

	twinWrapper := queueSubmitDispatchTwinWrapper(t, twinPath)

	claudeConfigPath := filepath.Join(t.TempDir(), ".claude.json")
	require.NoError(t, os.Setenv("HARMONIK_CLAUDE_CONFIG_PATH", claudeConfigPath))
	t.Cleanup(func() { _ = os.Unsetenv("HARMONIK_CLAUDE_CONFIG_PATH") })

	qs := daemon.ExportedNewQueueStore()

	loopCtx, loopCancel := context.WithCancel(context.Background())
	defer loopCancel()

	cfg := daemon.Config{
		ProjectDir:            projectDir,
		JSONLLogPath:          jsonlPath,
		BrPath:                brWrapper,
		HandlerBinary:         twinWrapper,
		HandlerEnv:            nil,
		SkipWALCheckpoint:     true,
		SkipBrHistoryRotation: true,
		AgentReadyTimeout:     5 * time.Second,
		NoAutoPull:            true, // queue-only; prevents br-ready from racing the submit
		QueueStore:            qs,
		LogWriter:             testLogWriter{t: t},
	}

	startDone := make(chan error, 1)
	go func() {
		startDone <- daemon.Start(loopCtx, cfg)
	}()

	// Brief pause to let the workloop reach its idle select.
	time.Sleep(200 * time.Millisecond)

	// ── Submit [A(pending), B(deferred)] via HandlerAdapter ─────────────────

	// qsdBlockingLedger mirrors the real br dep (A blocks B) at submit time so
	// QM-025 marks B as deferred-for-ledger-dep in the persisted queue.json.
	// The daemon's internal brQueueLedger independently reads the same dep from
	// br for the §2.8 ReevaluateDeferred pass.
	adapter := queue.NewHandlerAdapter(
		&qsdBlockingLedger{blocker: aID, blocked: bID},
		projectDir,
		qs,
		nil,
	)

	submitReq := queue.QueueSubmitRequest{
		SchemaVersion: 1,
		Groups: []queue.Group{
			{
				Kind: queue.GroupKindStream,
				Items: []queue.Item{
					{BeadID: aID, Status: queue.ItemStatusPending},
					{BeadID: bID, Status: queue.ItemStatusPending},
				},
			},
		},
	}
	params, err := json.Marshal(submitReq)
	require.NoError(t, err, "marshal QueueSubmitRequest")

	raw, rpcErr := adapter.HandleQueueSubmit(t.Context(), params)
	require.Nil(t, rpcErr, "HandleQueueSubmit: unexpected RPCError: %v", rpcErr)
	require.NotNil(t, raw, "HandleQueueSubmit: nil response")

	var submitResp queue.QueueSubmitResponse
	require.NoError(t, json.Unmarshal(raw, &submitResp), "decode QueueSubmitResponse")
	t.Logf("queueSubmitDispatch deferred-undefer: submitted queue_id=%s", submitResp.QueueID)

	// Verify B was deferred at submit time: queue.json must show B as
	// deferred-for-ledger-dep immediately after HandleQueueSubmit returns.
	scenariotest.AssertQueueJSON(t, projectDir, scenariotest.QueueExpectation{
		ItemStatuses: []string{
			string(queue.ItemStatusPending),           // A: pending (eligible head)
			string(queue.ItemStatusDeferredForLedgerDep), // B: deferred behind A
		},
	})
	t.Log("queueSubmitDispatch deferred-undefer: B confirmed deferred-for-ledger-dep at submit time")

	// ── Wait for BOTH A and B to complete ───────────────────────────────────

	// Two separate run_completed / run_failed events must land: one for A,
	// one for B. Budget = 2 × single-bead budget + headroom.
	const terminalBudget = 45 * time.Second
	scenariotest.MustCompleteWithin(t, jsonlPath, "", nil, terminalBudget, func() {
		for {
			if queueSubmitDispatchCountRunTerminal(t, jsonlPath) >= 2 {
				return
			}
			time.Sleep(50 * time.Millisecond)
		}
	})

	loopCancel()
	scenariotest.MustCompleteWithin(t, jsonlPath, "", nil, 5*time.Second, func() {
		if err := <-startDone; err != nil {
			t.Errorf("daemon.Start returned error after cancel: %v", err)
		}
	})

	// ── Assertions ──────────────────────────────────────────────────────────

	// 1. Both beads must be closed in br.
	scenariotest.AssertBeadStatus(t, brWrapper, string(aID), "closed")
	scenariotest.AssertBeadStatus(t, brWrapper, string(bID), "closed")

	// 2. Two run_started events must appear: one dispatch per bead.
	runStartedCount := queueSubmitDispatchCountRunStarted(t, jsonlPath)
	require.GreaterOrEqual(t, runStartedCount, 2,
		"expected ≥2 run_started events (one per bead); got %d (hk-nbjht un-defer regression guard)",
		runStartedCount)

	// 3. Event sequence: run_started(A) → run_completed(A) → run_started(B) → run_completed(B).
	// Subsequence check — intervening events are allowed.
	scenariotest.AssertEventSequence(t, jsonlPath, []scenariotest.ExpectedEvent{
		{Type: string(core.EventTypeRunStarted)},
		{Type: string(core.EventTypeRunCompleted)},
		{Type: string(core.EventTypeRunStarted)},
		{Type: string(core.EventTypeRunCompleted)},
	})

	// 4. No orphan tmux windows (nil adapter → skipped in non-tmux environments).
	scenariotest.AssertNoOrphanTmuxWindows(t, nil)

	t.Logf("TestScenario_QueueSubmit_DeferredUndefer_hknbjht: PASS A=%s B=%s", aID, bID)
}
