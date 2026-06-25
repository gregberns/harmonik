package daemon

// run_session_adoption_hk78tji_test.go — unit tests for the run-session
// adoption path that keeps bead-runs alive across daemon SIGKILL events.
//
// Coverage:
//
//   adoptDeadRunSessions (startup path):
//     - empty projectDir is a no-op
//     - dead session (not in live list) → ResetBead + registry entry removed
//     - live session (present in live list) → skipped, entry kept
//     - empty session name → treated as dead (ResetBead + removed)
//     - ResetBead error → entry kept for next-boot retry
//     - nil adapter → all sessions treated as dead (can't verify liveness)
//
//   adoptLiveRunSession (workloop monitor goroutine):
//     - context cancelled before session dies → no ReopenBead, no Remove
//     - session disappears (tmux list returns empty) → ReopenBead + Remove
//
//   E2E simulation (SIGKILL scenario):
//     - write registry entry + call adoptDeadRunSessions with no live sessions
//       → simulates daemon SIGKILL'd mid-run; next boot resets bead for
//       re-dispatch
//
// Bead ref: hk-78tji.

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/brcli"
	"github.com/gregberns/harmonik/internal/core"
	ltmux "github.com/gregberns/harmonik/internal/lifecycle/tmux"
	"github.com/gregberns/harmonik/internal/queue"
	runpkg "github.com/gregberns/harmonik/internal/run"
)

// ─────────────────────────────────────────────────────────────────────────────
// Shared stubs
// ─────────────────────────────────────────────────────────────────────────────

// runAdoptionFakeAdapter is a minimal ltmux.Adapter stub whose ListSessions
// can be controlled per-test. All other methods are no-ops.
type runAdoptionFakeAdapter struct {
	mu       sync.Mutex
	sessions []string // list returned by ListSessions
}

func (a *runAdoptionFakeAdapter) setSessions(ss []string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.sessions = ss
}

func (a *runAdoptionFakeAdapter) ListSessions(_ context.Context) ([]string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]string, len(a.sessions))
	copy(out, a.sessions)
	return out, nil
}

// All remaining ltmux.Adapter methods are no-ops.
func (a *runAdoptionFakeAdapter) ProbeTmux(_ context.Context) error { return nil }
func (a *runAdoptionFakeAdapter) ListWindows(_ context.Context, _ string) ([]string, error) {
	return nil, nil
}
func (a *runAdoptionFakeAdapter) NewWindowIn(_ context.Context, _ ltmux.NewWindowIn) ltmux.Outcome {
	return ltmux.Outcome{}
}
func (a *runAdoptionFakeAdapter) KillSession(_ context.Context, _ string) error { return nil }
func (a *runAdoptionFakeAdapter) KillWindow(_ context.Context, _ ltmux.WindowHandle) error {
	return nil
}
func (a *runAdoptionFakeAdapter) WindowPanePID(_ context.Context, _ ltmux.WindowHandle) (int, error) {
	return 0, nil
}
func (a *runAdoptionFakeAdapter) WindowPaneID(_ context.Context, _ ltmux.WindowHandle) (string, error) {
	return "", nil
}
func (a *runAdoptionFakeAdapter) LoadBuffer(_ context.Context, _ string, _ []byte) error {
	return nil
}
func (a *runAdoptionFakeAdapter) PasteBuffer(_ context.Context, _, _ string) error  { return nil }
func (a *runAdoptionFakeAdapter) SendKeysLiteral(_ context.Context, _, _ string) error { return nil }
func (a *runAdoptionFakeAdapter) SendKeysEnter(_ context.Context, _ string) error    { return nil }
func (a *runAdoptionFakeAdapter) SendKeysQuit(_ context.Context, _ string) error     { return nil }
func (a *runAdoptionFakeAdapter) WriteToPane(_ context.Context, _, _ string, _ []byte) error {
	return nil
}

var _ ltmux.Adapter = (*runAdoptionFakeAdapter)(nil)

// runAdoptionFakeResetter is a stub runBeadResetter that records ResetBead calls.
type runAdoptionFakeResetter struct {
	mu     sync.Mutex
	calls  []core.BeadID
	retErr error // returned on every ResetBead call when non-nil
}

func (r *runAdoptionFakeResetter) ResetBead(
	_ context.Context,
	_ string,
	_ brcli.TimeoutConfig,
	beadID core.BeadID,
	_ core.ProjectHash,
	_ int64,
) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, beadID)
	return r.retErr
}

func (r *runAdoptionFakeResetter) resetCalls() []core.BeadID {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]core.BeadID, len(r.calls))
	copy(out, r.calls)
	return out
}

// runAdoptionFixtureProjectDir creates a temp directory with a .harmonik/
// sub-tree suitable as a project root.
func runAdoptionFixtureProjectDir(t *testing.T) string {
	t.Helper()
	return t.TempDir()
}

// runAdoptionFixtureRecord creates and persists a minimal run registry record
// to projectDir/.harmonik/runs/<runID>.json.
func runAdoptionFixtureRecord(t *testing.T, projectDir, runID, beadID, sessionName string) runpkg.Record {
	t.Helper()
	rec := runpkg.Record{
		SchemaVersion: 1,
		RunID:         runID,
		BeadID:        beadID,
		SessionName:   sessionName,
		StartedAt:     time.Now(),
	}
	if err := runpkg.Write(projectDir, rec); err != nil {
		t.Fatalf("runAdoptionFixtureRecord: Write: %v", err)
	}
	return rec
}

// runAdoptionFakeLedger is a minimal beadLedger stub that records ReopenBead calls.
// It is defined here (not reusing workloop_test.go's stubBeadLedger) because this
// file is in package daemon — stubBeadLedger lives in package daemon_test.
type runAdoptionFakeLedger struct {
	mu      sync.Mutex
	opened  []core.BeadID
}

func (l *runAdoptionFakeLedger) Ready(_ context.Context) ([]core.BeadRecord, error) {
	return nil, nil
}
func (l *runAdoptionFakeLedger) ShowBead(_ context.Context, id core.BeadID) (core.BeadRecord, error) {
	return core.BeadRecord{BeadID: id}, nil
}
func (l *runAdoptionFakeLedger) ClaimBead(_ context.Context, _ string, _ brcli.TimeoutConfig, _ core.RunID, _ core.TransitionID, _ core.BeadID) error {
	return nil
}
func (l *runAdoptionFakeLedger) CloseBead(_ context.Context, _ string, _ brcli.TimeoutConfig, _ core.RunID, _ core.TransitionID, _ core.BeadID, _ bool) error {
	return nil
}
func (l *runAdoptionFakeLedger) ReopenBead(_ context.Context, _ string, _ brcli.TimeoutConfig, _ core.RunID, _ core.TransitionID, beadID core.BeadID, _ string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.opened = append(l.opened, beadID)
	return nil
}
func (l *runAdoptionFakeLedger) reopenedIDs() []core.BeadID {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]core.BeadID, len(l.opened))
	copy(out, l.opened)
	return out
}

var _ beadLedger = (*runAdoptionFakeLedger)(nil)

// runAdoptionEntryExists reports whether the registry entry for runID still
// exists on disk under projectDir.
func runAdoptionEntryExists(t *testing.T, projectDir, runID string) bool {
	t.Helper()
	_, err := runpkg.Load(projectDir, runID)
	if err == nil {
		return true
	}
	if errors.Is(err, runpkg.ErrNotFound) {
		return false
	}
	t.Fatalf("runAdoptionEntryExists: Load: %v", err)
	return false
}

// ─────────────────────────────────────────────────────────────────────────────
// adoptDeadRunSessions tests
// ─────────────────────────────────────────────────────────────────────────────

// TestAdoptDeadRunSessions_EmptyProjectDir_IsNoOp verifies that an empty
// projectDir causes adoptDeadRunSessions to return immediately without any
// calls to the resetter or adapter.
//
// Bead ref: hk-78tji.
func TestAdoptDeadRunSessions_EmptyProjectDir_IsNoOp(t *testing.T) {
	t.Parallel()

	adapter := &runAdoptionFakeAdapter{}
	resetter := &runAdoptionFakeResetter{}

	// Should not panic or call resetter.
	adoptDeadRunSessions(t.Context(), "", core.ProjectHash("h1"), 0, "", adapter, resetter)

	if calls := resetter.resetCalls(); len(calls) != 0 {
		t.Errorf("ResetBead called %d time(s) with empty projectDir; want 0", len(calls))
	}
}

// TestAdoptDeadRunSessions_DeadSession_ResetsAndRemovesEntry verifies that when
// a registry entry's session is not in the live-sessions list, adoptDeadRunSessions
// calls ResetBead for the bead and removes the registry entry.
//
// Bead ref: hk-78tji.
func TestAdoptDeadRunSessions_DeadSession_ResetsAndRemovesEntry(t *testing.T) {
	t.Parallel()

	const (
		runID       = "019eff0a-0001-7000-8000-000000000001"
		beadID      = "hk-78tji-dead-001"
		sessionName = "harmonik-deadproj-default" // NOT in live list
	)

	projectDir := runAdoptionFixtureProjectDir(t)
	runAdoptionFixtureRecord(t, projectDir, runID, beadID, sessionName)

	// Adapter returns an empty live-sessions list → session is dead.
	adapter := &runAdoptionFakeAdapter{sessions: nil}
	resetter := &runAdoptionFakeResetter{}

	adoptDeadRunSessions(t.Context(), projectDir, core.ProjectHash("proj-dead"), 0, "", adapter, resetter)

	// ResetBead must have been called for the bead.
	calls := resetter.resetCalls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 ResetBead call; got %d: %v", len(calls), calls)
	}
	if calls[0] != core.BeadID(beadID) {
		t.Errorf("ResetBead beadID = %q; want %q", calls[0], beadID)
	}

	// Registry entry must have been removed.
	if runAdoptionEntryExists(t, projectDir, runID) {
		t.Errorf("registry entry for run %q still exists after adoptDeadRunSessions; want removed", runID)
	}
}

// TestAdoptDeadRunSessions_LiveSession_IsSkipped verifies that when a registry
// entry's session is in the live-sessions list, adoptDeadRunSessions skips it
// (no ResetBead call, entry kept for adoptLiveRunSession to handle).
//
// Bead ref: hk-78tji.
func TestAdoptDeadRunSessions_LiveSession_IsSkipped(t *testing.T) {
	t.Parallel()

	const (
		runID       = "019eff0a-0002-7000-8000-000000000002"
		beadID      = "hk-78tji-live-001"
		sessionName = "harmonik-liveproj-default" // IN the live list
	)

	projectDir := runAdoptionFixtureProjectDir(t)
	runAdoptionFixtureRecord(t, projectDir, runID, beadID, sessionName)

	// Adapter reports the session as still live.
	adapter := &runAdoptionFakeAdapter{sessions: []string{sessionName}}
	resetter := &runAdoptionFakeResetter{}

	adoptDeadRunSessions(t.Context(), projectDir, core.ProjectHash("proj-live"), 0, "", adapter, resetter)

	// No ResetBead call.
	if calls := resetter.resetCalls(); len(calls) != 0 {
		t.Errorf("ResetBead called %d time(s) for live session; want 0", len(calls))
	}

	// Entry must still exist — adoptLiveRunSession will handle it from runWorkLoop.
	if !runAdoptionEntryExists(t, projectDir, runID) {
		t.Errorf("registry entry for run %q was removed; want kept for live-session adoption", runID)
	}
}

// TestAdoptDeadRunSessions_EmptySessionName_TreatsAsDead verifies that an
// entry with an empty SessionName (no session was ever recorded) is treated as
// dead: ResetBead is called and the entry is removed.
//
// Bead ref: hk-78tji.
func TestAdoptDeadRunSessions_EmptySessionName_TreatsAsDead(t *testing.T) {
	t.Parallel()

	const (
		runID  = "019eff0a-0003-7000-8000-000000000003"
		beadID = "hk-78tji-nosess-001"
	)

	projectDir := runAdoptionFixtureProjectDir(t)
	runAdoptionFixtureRecord(t, projectDir, runID, beadID, "") // empty session name

	adapter := &runAdoptionFakeAdapter{sessions: []string{"some-other-session"}}
	resetter := &runAdoptionFakeResetter{}

	adoptDeadRunSessions(t.Context(), projectDir, core.ProjectHash("proj-nosess"), 0, "", adapter, resetter)

	calls := resetter.resetCalls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 ResetBead call for empty session name; got %d", len(calls))
	}
	if calls[0] != core.BeadID(beadID) {
		t.Errorf("ResetBead beadID = %q; want %q", calls[0], beadID)
	}
	if runAdoptionEntryExists(t, projectDir, runID) {
		t.Errorf("registry entry for run %q still exists; want removed for empty-session-name entry", runID)
	}
}

// TestAdoptDeadRunSessions_ResetError_KeepsEntry verifies that when ResetBead
// returns an error, the registry entry is NOT removed so the next daemon boot
// can retry the reset.
//
// Bead ref: hk-78tji.
func TestAdoptDeadRunSessions_ResetError_KeepsEntry(t *testing.T) {
	t.Parallel()

	const (
		runID       = "019eff0a-0004-7000-8000-000000000004"
		beadID      = "hk-78tji-reseterr-001"
		sessionName = "harmonik-errproj-default"
	)

	projectDir := runAdoptionFixtureProjectDir(t)
	runAdoptionFixtureRecord(t, projectDir, runID, beadID, sessionName)

	adapter := &runAdoptionFakeAdapter{sessions: nil} // session dead
	resetter := &runAdoptionFakeResetter{retErr: fmt.Errorf("br: stub reset error")}

	adoptDeadRunSessions(t.Context(), projectDir, core.ProjectHash("proj-err"), 0, "", adapter, resetter)

	// ResetBead was attempted.
	if calls := resetter.resetCalls(); len(calls) != 1 {
		t.Fatalf("expected 1 ResetBead call; got %d", len(calls))
	}

	// Entry must still exist — error is non-fatal; next boot retries.
	if !runAdoptionEntryExists(t, projectDir, runID) {
		t.Errorf("registry entry for run %q was removed despite ResetBead error; want kept for retry", runID)
	}
}

// TestAdoptDeadRunSessions_NilAdapter_TreatsAllAsDead verifies that a nil
// adapter (can't verify liveness) causes all sessions to be treated as dead.
//
// Bead ref: hk-78tji.
func TestAdoptDeadRunSessions_NilAdapter_TreatsAllAsDead(t *testing.T) {
	t.Parallel()

	const (
		runID       = "019eff0a-0005-7000-8000-000000000005"
		beadID      = "hk-78tji-niladapter-001"
		sessionName = "harmonik-niladapter-default"
	)

	projectDir := runAdoptionFixtureProjectDir(t)
	runAdoptionFixtureRecord(t, projectDir, runID, beadID, sessionName)

	resetter := &runAdoptionFakeResetter{}

	// nil adapter → live-session set is empty → session treated as dead
	adoptDeadRunSessions(t.Context(), projectDir, core.ProjectHash("proj-nil"), 0, "", nil, resetter)

	calls := resetter.resetCalls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 ResetBead call with nil adapter; got %d", len(calls))
	}
	if calls[0] != core.BeadID(beadID) {
		t.Errorf("ResetBead beadID = %q; want %q", calls[0], beadID)
	}
	if runAdoptionEntryExists(t, projectDir, runID) {
		t.Errorf("registry entry for run %q still exists with nil adapter; want removed", runID)
	}
}

// TestAdoptDeadRunSessions_MultipleEntries_LiveSkippedDeadReset verifies that
// adoptDeadRunSessions correctly differentiates among multiple registry entries:
// dead ones are reset and removed; live ones are preserved.
//
// Bead ref: hk-78tji.
func TestAdoptDeadRunSessions_MultipleEntries_LiveSkippedDeadReset(t *testing.T) {
	t.Parallel()

	const (
		runIDDead   = "019eff0a-0006-7000-8000-000000000006"
		beadIDDead  = "hk-78tji-multi-dead"
		sessDead    = "harmonik-multi-dead-sess"
		runIDLive   = "019eff0a-0007-7000-8000-000000000007"
		beadIDLive  = "hk-78tji-multi-live"
		sessLive    = "harmonik-multi-live-sess"
	)

	projectDir := runAdoptionFixtureProjectDir(t)
	runAdoptionFixtureRecord(t, projectDir, runIDDead, beadIDDead, sessDead)
	runAdoptionFixtureRecord(t, projectDir, runIDLive, beadIDLive, sessLive)

	// Only the live session is in the active list.
	adapter := &runAdoptionFakeAdapter{sessions: []string{sessLive}}
	resetter := &runAdoptionFakeResetter{}

	adoptDeadRunSessions(t.Context(), projectDir, core.ProjectHash("proj-multi"), 0, "", adapter, resetter)

	// Dead bead: reset called, entry removed.
	calls := resetter.resetCalls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 ResetBead call (dead bead only); got %d: %v", len(calls), calls)
	}
	if calls[0] != core.BeadID(beadIDDead) {
		t.Errorf("ResetBead beadID = %q; want %q (dead bead)", calls[0], beadIDDead)
	}
	if runAdoptionEntryExists(t, projectDir, runIDDead) {
		t.Errorf("dead bead registry entry still exists; want removed")
	}

	// Live bead: no reset, entry kept.
	if !runAdoptionEntryExists(t, projectDir, runIDLive) {
		t.Errorf("live bead registry entry was removed; want kept")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// adoptLiveRunSession tests
// ─────────────────────────────────────────────────────────────────────────────

// TestAdoptLiveRunSession_CtxCancelled_NoAction verifies that cancelling the
// context before the session exits causes adoptLiveRunSession to return without
// calling ReopenBead and without removing the registry entry.
//
// Bead ref: hk-78tji.
func TestAdoptLiveRunSession_CtxCancelled_NoAction(t *testing.T) {
	t.Parallel()

	const (
		runID       = "019eff0a-0008-7000-8000-000000000008"
		beadID      = "hk-78tji-ctxcancel-001"
		sessionName = "harmonik-ctxcancel-default"
	)

	projectDir := runAdoptionFixtureProjectDir(t)
	rec := runAdoptionFixtureRecord(t, projectDir, runID, beadID, sessionName)

	// Adapter always reports the session as live.
	adapter := &runAdoptionFakeAdapter{sessions: []string{sessionName}}
	ledger := &runAdoptionFakeLedger{}

	deps := workLoopDeps{
		brAdapter:  ledger,
		tidGen:     core.NewTransitionIDGenerator(),
		projectDir: projectDir,
	}

	// Cancel the context immediately so the goroutine exits on the first select.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	done := make(chan struct{})
	go func() {
		defer close(done)
		adoptLiveRunSession(ctx, deps, rec, adapter)
	}()

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("adoptLiveRunSession did not exit after context cancellation")
	}

	// No ReopenBead should have been called.
	if ids := ledger.reopenedIDs(); len(ids) != 0 {
		t.Errorf("ReopenBead called %d time(s) after ctx cancel; want 0: %v", len(ids), ids)
	}

	// Registry entry must still exist — the next boot's adoption pass handles it.
	if !runAdoptionEntryExists(t, projectDir, runID) {
		t.Errorf("registry entry for run %q was removed on ctx cancel; want kept for next boot", runID)
	}
}

// TestAdoptLiveRunSession_SessionDies_ReopensBeadAndRemovesEntry verifies that
// when the tmux session disappears (Claude has exited), adoptLiveRunSession
// calls ReopenBead to transition the bead back to open and removes the
// registry entry.
//
// Bead ref: hk-78tji.
func TestAdoptLiveRunSession_SessionDies_ReopensBeadAndRemovesEntry(t *testing.T) {
	t.Parallel()

	const (
		// Valid UUIDv7 format required because adoptLiveRunSession calls uuid.Parse.
		runID       = "019eff0a-0009-7000-8000-000000000009"
		beadID      = "hk-78tji-sessdie-001"
		sessionName = "harmonik-sessdie-default"
	)

	projectDir := runAdoptionFixtureProjectDir(t)
	rec := runAdoptionFixtureRecord(t, projectDir, runID, beadID, sessionName)

	// Adapter returns an empty list → session already gone when the first tick fires.
	adapter := &runAdoptionFakeAdapter{sessions: nil}
	ledger := &runAdoptionFakeLedger{}

	deps := workLoopDeps{
		brAdapter:  ledger,
		tidGen:     core.NewTransitionIDGenerator(),
		projectDir: projectDir,
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		adoptLiveRunSession(t.Context(), deps, rec, adapter)
	}()

	// Allow up to 10 s — the ticker fires every 2 s so the goroutine should
	// detect the dead session on the first tick.
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("adoptLiveRunSession did not exit after session disappeared")
	}

	// ReopenBead must have been called for the bead.
	ids := ledger.reopenedIDs()
	if len(ids) == 0 {
		t.Fatal("ReopenBead was not called after session disappeared; want bead transitioned to open")
	}
	if ids[0] != core.BeadID(beadID) {
		t.Errorf("ReopenBead beadID = %q; want %q", ids[0], beadID)
	}

	// Registry entry must be removed.
	if runAdoptionEntryExists(t, projectDir, runID) {
		t.Errorf("registry entry for run %q still exists after session died; want removed", runID)
	}
}

// TestAdoptLiveRunSession_SessionDies_RevertsQueueItemToPending verifies that
// when the session disappears, adoptLiveRunSession reverts the queue item from
// dispatched → pending so the dispatch loop can re-dispatch the bead.
//
// Bead ref: hk-78tji.
func TestAdoptLiveRunSession_SessionDies_RevertsQueueItemToPending(t *testing.T) {
	t.Parallel()

	const (
		runID       = "019eff0a-000b-7000-8000-00000000000b"
		beadID      = "hk-78tji-queuerevert-001"
		sessionName = "harmonik-queuerevert-default"
		queueID     = "queuerevert-queue-id"
	)

	projectDir := runAdoptionFixtureProjectDir(t)

	// Build a queue with one dispatched item at group 0, item 0.
	runIDPtr := runID
	now := time.Now()
	q := &queue.Queue{
		SchemaVersion: 1,
		Name:          queue.QueueNameMain,
		QueueID:       queueID,
		SubmittedAt:   now,
		Status:        queue.QueueStatusActive,
		Groups: []queue.Group{
			{
				GroupIndex: 0,
				Kind:       queue.GroupKindWave,
				Status:     queue.GroupStatusActive,
				Items: []queue.Item{
					{
						BeadID: core.BeadID(beadID),
						Status: queue.ItemStatusDispatched,
						RunID:  &runIDPtr,
					},
				},
				CreatedAt: now,
			},
		},
	}

	qs := NewQueueStore()
	qs.SetQueue(q)

	rec := runpkg.Record{
		SchemaVersion: 1,
		RunID:         runID,
		BeadID:        beadID,
		QueueName:     queue.QueueNameMain,
		QueueID:       queueID,
		GroupIndex:    0,
		ItemIndex:     0,
		SessionName:   sessionName,
		StartedAt:     now,
	}
	if err := runpkg.Write(projectDir, rec); err != nil {
		t.Fatalf("runpkg.Write: %v", err)
	}

	// Adapter returns empty → session already gone on first tick.
	adapter := &runAdoptionFakeAdapter{sessions: nil}
	ledger := &runAdoptionFakeLedger{}

	deps := workLoopDeps{
		brAdapter:  ledger,
		tidGen:     core.NewTransitionIDGenerator(),
		projectDir: projectDir,
		queueStore: qs,
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		adoptLiveRunSession(t.Context(), deps, rec, adapter)
	}()

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("adoptLiveRunSession did not exit after session disappeared")
	}

	// Queue item must have been reverted from dispatched → pending.
	got := qs.Queue()
	if got == nil {
		t.Fatal("QueueStore queue is nil after session-dies adoption; want the queue preserved")
	}
	if len(got.Groups) == 0 || len(got.Groups[0].Items) == 0 {
		t.Fatal("queue has no groups/items after adoption")
	}
	item := got.Groups[0].Items[0]
	if item.Status != queue.ItemStatusPending {
		t.Errorf("queue item status = %q; want %q (pending, ready for re-dispatch)", item.Status, queue.ItemStatusPending)
	}
	if item.RunID != nil {
		t.Errorf("queue item RunID = %v; want nil after revert", item.RunID)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// E2E SIGKILL simulation
// ─────────────────────────────────────────────────────────────────────────────

// TestAdoptDeadRunSessions_SIGKILLSimulation_NextBootResetsAndRemoves is an
// end-to-end simulation of the daemon-SIGKILL recovery path:
//
//  1. A bead is dispatched; the daemon writes a run registry entry recording
//     the bead ID and its independent tmux session.
//  2. The daemon is SIGKILL'd (simulated here by simply not running any cleanup).
//  3. The next daemon boot calls adoptDeadRunSessions. The independent tmux
//     session has since exited (Claude finished or was cleaned up). The adapter
//     returns no live sessions.
//  4. adoptDeadRunSessions resets the bead to open so QM-002a can revert the
//     queue item from dispatched → pending, making the bead eligible for
//     re-dispatch on the new daemon.
//
// Bead ref: hk-78tji.
func TestAdoptDeadRunSessions_SIGKILLSimulation_NextBootResetsAndRemoves(t *testing.T) {
	t.Parallel()

	const (
		runID       = "019eff0a-000a-7000-8000-00000000000a"
		beadID      = "hk-78tji-e2e-sigkill"
		sessionName = "harmonik-sigkillproj-default"
	)

	projectDir := runAdoptionFixtureProjectDir(t)

	// Step 1 — pre-SIGKILL: daemon writes the run registry entry.
	runAdoptionFixtureRecord(t, projectDir, runID, beadID, sessionName)

	// Step 2 — SIGKILL: no explicit action; just don't run cleanup.

	// Step 3 — next boot: adoptDeadRunSessions is called. The independent session
	// has since exited (adapter sees no live sessions).
	adapter := &runAdoptionFakeAdapter{sessions: nil}
	resetter := &runAdoptionFakeResetter{}

	adoptDeadRunSessions(t.Context(), projectDir, core.ProjectHash("sigkill-proj"), 0, "", adapter, resetter)

	// Step 4 — verify: bead was reset (will be reverted to pending by QM-002a).
	calls := resetter.resetCalls()
	if len(calls) != 1 {
		t.Fatalf("SIGKILL simulation: expected 1 ResetBead call on next boot; got %d", len(calls))
	}
	if calls[0] != core.BeadID(beadID) {
		t.Errorf("SIGKILL simulation: ResetBead beadID = %q; want %q", calls[0], beadID)
	}

	// Registry entry must be cleaned up so it doesn't pollute future boots.
	if runAdoptionEntryExists(t, projectDir, runID) {
		t.Errorf("SIGKILL simulation: registry entry for run %q still exists after recovery; want removed", runID)
	}
}
