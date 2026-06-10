package daemon

// crewstart_hk5tg5o_test.go — unit tests for the C2 crew-start / crew-stop handler.
//
// AC-1: crew-start exits 0, session_id non-empty, registry record written,
//        queue ensured, .managed marker created.
// AC-3: crew-stop tears down pane (best-effort), removes .managed, removes
//        registry record; --pause-queue triggers OperatorControlHandler.
//
// Bead ref: hk-5tg5o. Task check: go test ./internal/daemon/ -run CrewStart.

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/gregberns/harmonik/internal/crew"
	"github.com/gregberns/harmonik/internal/handler"
)

// ─────────────────────────────────────────────────────────────────────────────
// Test doubles
// ─────────────────────────────────────────────────────────────────────────────

// fakeSubstrate is a minimal handler.Substrate test double.
// It records the most recent SpawnWindow call and returns a fakeSession.
type fakeSubstrate struct {
	spawnCalled bool
	spawnArg    handler.SubstrateSpawn
	spawnErr    error
	stopCalled  bool
	stopHandle  string
	stopErr     error
}

func (f *fakeSubstrate) SpawnWindow(_ context.Context, spawn handler.SubstrateSpawn) (handler.SubstrateSession, error) {
	f.spawnCalled = true
	f.spawnArg = spawn
	if f.spawnErr != nil {
		return nil, f.spawnErr
	}
	return &fakeSession{handle: "fake-window-handle"}, nil
}

// StopWindowByHandle implements crewPaneStopper so crew-stop exercises teardown.
func (f *fakeSubstrate) StopWindowByHandle(_ context.Context, handle string) error {
	f.stopCalled = true
	f.stopHandle = handle
	return f.stopErr
}

// fakeSession implements handler.SubstrateSession + windowHandleExposer.
type fakeSession struct {
	handle string
}

func (s *fakeSession) Kill(_ context.Context) error { return nil }
func (s *fakeSession) Wait(_ context.Context) error { return nil }
func (s *fakeSession) Outcome() handler.Outcome     { return handler.Outcome{} }
func (s *fakeSession) PID() int                     { return 0 }
func (s *fakeSession) Stdout() io.Reader            { return nil }
func (s *fakeSession) WindowHandle() string         { return s.handle }

// fakePauseCtrl records HandleOperatorPause calls.
type fakePauseCtrl struct {
	pausedQueue string
	pauseErr    error
}

func (f *fakePauseCtrl) HandleOperatorPause(_ context.Context, queueName string) error {
	f.pausedQueue = queueName
	return f.pauseErr
}

func (f *fakePauseCtrl) HandleOperatorResume(_ context.Context, _ string) error { return nil }

// ─────────────────────────────────────────────────────────────────────────────
// Helpers
// ─────────────────────────────────────────────────────────────────────────────

func newTestCrewHandler(t *testing.T, sub handler.Substrate, opCtrl OperatorControlHandler) (CrewHandler, string) {
	t.Helper()
	dir := t.TempDir()
	return NewCrewHandler("claude", dir, sub, opCtrl), dir
}

func mustCrewStart(t *testing.T, h CrewHandler, req CrewStartRequest) CrewStartResult {
	t.Helper()
	raw, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal CrewStartRequest: %v", err)
	}
	out, err := h.HandleCrewStart(context.Background(), json.RawMessage(raw))
	if err != nil {
		t.Fatalf("HandleCrewStart: %v", err)
	}
	var result CrewStartResult
	if err := json.Unmarshal(out, &result); err != nil {
		t.Fatalf("unmarshal CrewStartResult: %v", err)
	}
	return result
}

func mustCrewStop(t *testing.T, h CrewHandler, req CrewStopRequest) {
	t.Helper()
	raw, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal CrewStopRequest: %v", err)
	}
	_, err = h.HandleCrewStop(context.Background(), json.RawMessage(raw))
	if err != nil {
		t.Fatalf("HandleCrewStop: %v", err)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// AC-1: crew-start happy path
// ─────────────────────────────────────────────────────────────────────────────

// TestCrewStart_HappyPath verifies AC-1: session_id non-empty, registry record
// written before launch, queue ensured, .managed marker created.
func TestCrewStart_HappyPath(t *testing.T) {
	sub := &fakeSubstrate{}
	h, dir := newTestCrewHandler(t, sub, nil)

	result := mustCrewStart(t, h, CrewStartRequest{
		Name:  "alpha",
		Queue: "crew-q",
	})

	// session_id must be a non-empty string.
	if result.SessionID == "" {
		t.Error("crew-start: expected non-empty session_id")
	}
	if result.Name != "alpha" {
		t.Errorf("crew-start: name = %q, want %q", result.Name, "alpha")
	}

	// Registry record must be written with matching fields.
	rec, err := crew.Load(dir, "alpha")
	if err != nil {
		t.Fatalf("crew.Load: %v", err)
	}
	if rec.SessionID != result.SessionID {
		t.Errorf("registry session_id = %q, want %q", rec.SessionID, result.SessionID)
	}
	if rec.Queue != "crew-q" {
		t.Errorf("registry queue = %q, want %q", rec.Queue, "crew-q")
	}

	// Queue file must exist under .harmonik/queues/.
	queueFile := filepath.Join(dir, ".harmonik", "queues", "crew-q.json")
	if _, statErr := os.Stat(queueFile); statErr != nil {
		t.Errorf("queue file not created: %v", statErr)
	}

	// .managed marker must exist.
	markerPath := filepath.Join(dir, ".harmonik", "keeper", "alpha.managed")
	if _, statErr := os.Stat(markerPath); statErr != nil {
		t.Errorf(".managed marker not created: %v", statErr)
	}

	// substrate.SpawnWindow must have been called.
	if !sub.spawnCalled {
		t.Error("substrate.SpawnWindow was not called")
	}
}

// TestCrewStart_SessionIDMintedBeforeLaunch verifies the registry record exists
// before the pane is spawned (AC-1 ordering guarantee).
func TestCrewStart_SessionIDMintedBeforeLaunch(t *testing.T) {
	var registryExistsDuringSpawn bool

	// Use a sub whose SpawnWindow checks the registry mid-flight.
	var dir string
	check := &spawnCheckSubstrate{
		checkFn: func(_ handler.SubstrateSpawn) {
			_, err := crew.Load(dir, "beta")
			registryExistsDuringSpawn = err == nil
		},
	}

	h := NewCrewHandler("claude", "", check, nil).(*crewHandlerImpl)
	// Override projectDir after construction so we can set dir.
	dir = t.TempDir()
	h.projectDir = dir

	mustCrewStart(t, h, CrewStartRequest{Name: "beta", Queue: "q2"})

	if !registryExistsDuringSpawn {
		t.Error("registry record was NOT written before SpawnWindow was called")
	}
}

// spawnCheckSubstrate is a substrate that calls checkFn inside SpawnWindow.
type spawnCheckSubstrate struct {
	checkFn func(handler.SubstrateSpawn)
}

func (s *spawnCheckSubstrate) SpawnWindow(_ context.Context, spawn handler.SubstrateSpawn) (handler.SubstrateSession, error) {
	if s.checkFn != nil {
		s.checkFn(spawn)
	}
	return &fakeSession{handle: "check-handle"}, nil
}

func (s *spawnCheckSubstrate) StopWindowByHandle(_ context.Context, _ string) error { return nil }

// TestCrewStart_QueueEnsureIdempotent verifies that starting the same crew twice
// does not return an error on the second start (re-launch path) and the queue
// file is not duplicated.
func TestCrewStart_QueueEnsureIdempotent(t *testing.T) {
	h, dir := newTestCrewHandler(t, &fakeSubstrate{}, nil)

	// First start.
	r1 := mustCrewStart(t, h, CrewStartRequest{Name: "gamma", Queue: "shared-q"})
	if r1.SessionID == "" {
		t.Fatal("first start: empty session_id")
	}

	// Second start reuses the same name → re-launch path (resume=true).
	r2 := mustCrewStart(t, h, CrewStartRequest{Name: "gamma", Queue: "shared-q"})
	// session_id must match the first (re-launch reuses it).
	if r2.SessionID != r1.SessionID {
		t.Errorf("re-launch session_id = %q, want %q (first session)", r2.SessionID, r1.SessionID)
	}

	queueFile := filepath.Join(dir, ".harmonik", "queues", "shared-q.json")
	if _, err := os.Stat(queueFile); err != nil {
		t.Errorf("queue file missing after re-launch: %v", err)
	}
}

// TestCrewStart_QueueConflict verifies that two different crew names can not
// bind to the same queue.
func TestCrewStart_QueueConflict(t *testing.T) {
	h, _ := newTestCrewHandler(t, &fakeSubstrate{}, nil)

	mustCrewStart(t, h, CrewStartRequest{Name: "first", Queue: "contested"})

	raw, _ := json.Marshal(CrewStartRequest{Name: "second", Queue: "contested"})
	_, err := h.HandleCrewStart(context.Background(), json.RawMessage(raw))
	if err == nil {
		t.Error("expected error for queue conflict, got nil")
	}
}

// TestCrewStart_SpawnFailRollback verifies that on spawn failure the registry
// record is removed (rollback).
func TestCrewStart_SpawnFailRollback(t *testing.T) {
	sub := &fakeSubstrate{spawnErr: errors.New("tmux: no session")}
	h, dir := newTestCrewHandler(t, sub, nil)

	raw, _ := json.Marshal(CrewStartRequest{Name: "delta", Queue: "q-delta"})
	_, err := h.HandleCrewStart(context.Background(), json.RawMessage(raw))
	if err == nil {
		t.Fatal("expected spawn error, got nil")
	}

	_, loadErr := crew.Load(dir, "delta")
	if !errors.Is(loadErr, crew.ErrNotFound) {
		t.Errorf("expected registry rollback (ErrNotFound), got %v", loadErr)
	}
}

// TestCrewStart_RequiresName verifies that an empty name returns an error.
func TestCrewStart_RequiresName(t *testing.T) {
	h, _ := newTestCrewHandler(t, &fakeSubstrate{}, nil)
	raw, _ := json.Marshal(CrewStartRequest{Name: "", Queue: "q"})
	_, err := h.HandleCrewStart(context.Background(), json.RawMessage(raw))
	if err == nil {
		t.Error("expected error for empty name, got nil")
	}
}

// TestCrewStart_RequiresQueue verifies that an empty queue returns an error.
func TestCrewStart_RequiresQueue(t *testing.T) {
	h, _ := newTestCrewHandler(t, &fakeSubstrate{}, nil)
	raw, _ := json.Marshal(CrewStartRequest{Name: "epsilon", Queue: ""})
	_, err := h.HandleCrewStart(context.Background(), json.RawMessage(raw))
	if err == nil {
		t.Error("expected error for empty queue, got nil")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// AC-3: crew-stop happy path
// ─────────────────────────────────────────────────────────────────────────────

// TestCrewStart_StopHappyPath verifies AC-3: stop tears down pane, removes
// .managed, removes registry record.
func TestCrewStart_StopHappyPath(t *testing.T) {
	sub := &fakeSubstrate{}
	h, dir := newTestCrewHandler(t, sub, nil)

	// Start first to populate the registry + .managed.
	mustCrewStart(t, h, CrewStartRequest{Name: "zeta", Queue: "q-zeta"})

	// Verify .managed exists before stop.
	markerPath := filepath.Join(dir, ".harmonik", "keeper", "zeta.managed")
	if _, err := os.Stat(markerPath); err != nil {
		t.Fatalf(".managed not created before stop: %v", err)
	}

	mustCrewStop(t, h, CrewStopRequest{Name: "zeta"})

	// Registry record must be gone.
	_, err := crew.Load(dir, "zeta")
	if !errors.Is(err, crew.ErrNotFound) {
		t.Errorf("expected registry removed after stop, got %v", err)
	}

	// .managed must be gone.
	if _, err := os.Stat(markerPath); !os.IsNotExist(err) {
		t.Errorf(".managed not removed after stop")
	}

	// substrate.StopWindowByHandle must have been called.
	if !sub.stopCalled {
		t.Error("substrate.StopWindowByHandle was not called")
	}
}

// TestCrewStart_StopNotFound verifies that stopping a non-existent crew returns
// an error.
func TestCrewStart_StopNotFound(t *testing.T) {
	h, _ := newTestCrewHandler(t, &fakeSubstrate{}, nil)
	raw, _ := json.Marshal(CrewStopRequest{Name: "ghost"})
	_, err := h.HandleCrewStop(context.Background(), json.RawMessage(raw))
	if err == nil {
		t.Error("expected error for unknown crew, got nil")
	}
}

// TestCrewStart_StopPauseQueue verifies AC-3 --pause-queue: OperatorPause is
// called with the crew's queue name.
func TestCrewStart_StopPauseQueue(t *testing.T) {
	sub := &fakeSubstrate{}
	ctrl := &fakePauseCtrl{}
	h, _ := newTestCrewHandler(t, sub, ctrl)

	mustCrewStart(t, h, CrewStartRequest{Name: "eta", Queue: "q-eta"})
	mustCrewStop(t, h, CrewStopRequest{Name: "eta", PauseQueue: true})

	if ctrl.pausedQueue != "q-eta" {
		t.Errorf("paused queue = %q, want %q", ctrl.pausedQueue, "q-eta")
	}
}

// TestCrewStart_StopNoPauseQueue verifies that without --pause-queue,
// OperatorPause is NOT called.
func TestCrewStart_StopNoPauseQueue(t *testing.T) {
	ctrl := &fakePauseCtrl{}
	h, _ := newTestCrewHandler(t, &fakeSubstrate{}, ctrl)

	mustCrewStart(t, h, CrewStartRequest{Name: "theta", Queue: "q-theta"})
	mustCrewStop(t, h, CrewStopRequest{Name: "theta", PauseQueue: false})

	if ctrl.pausedQueue != "" {
		t.Errorf("expected no pause, but paused queue = %q", ctrl.pausedQueue)
	}
}
