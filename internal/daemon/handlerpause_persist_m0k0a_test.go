package daemon_test

// handlerpause_persist_m0k0a_test.go — tests for handler-pause persistence
// layer (hk-m0k0a).
//
// Acceptance criteria per bead spec:
//   - persist → load round-trip (paused state survives write+read)
//   - restart preserves paused status (LoadHandlerPauseState seeds controller)
//   - file-absent baseline (no error, all handlers live)
//   - file-unparseable → error returned (refuse startup)
//   - forward-incompatible schema_version → ErrHandlerStateSchemaUnsupported
//   - live handlers in file are not re-seeded as paused
//
// Bead ref: hk-m0k0a.

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/eventbus"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// newPersistController creates a HandlerPauseController wired with a real
// persistFn targeting stateDir.
func newPersistController(t *testing.T, stateDir string) *daemon.HandlerPauseController {
	t.Helper()
	bus := eventbus.NewBusImpl()
	if err := bus.Seal(); err != nil {
		t.Fatalf("bus.Seal: %v", err)
	}
	persistFn := daemon.MakeHandlerPausePersistFn(stateDir)
	return daemon.NewHandlerPauseController(bus, persistFn)
}

// newLoadController creates a HandlerPauseController with nil persistFn for
// receiving loaded state (simulates a fresh daemon restart where the controller
// is constructed first and then seeded by LoadHandlerPauseState).
func newLoadController(t *testing.T) *daemon.HandlerPauseController {
	t.Helper()
	bus := eventbus.NewBusImpl()
	if err := bus.Seal(); err != nil {
		t.Fatalf("bus.Seal: %v", err)
	}
	return daemon.NewHandlerPauseController(bus, nil)
}

// makeTestCause returns a valid HandlerPauseCause for tests.
func makeTestCause(runID, beadID string) core.HandlerPauseCause {
	return core.HandlerPauseCause{
		FailureClass: core.FailureClassTransient,
		SubReason:    "rate_limit",
		SourceRunID:  runID,
		SourceBeadID: beadID,
		TrippedAt:    time.Now().UTC().Format(time.RFC3339Nano),
	}
}

// ---------------------------------------------------------------------------
// Round-trip: Pause → write → load → controller reflects paused state
// ---------------------------------------------------------------------------

func TestHandlerPausePersist_RoundTrip(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	ctx := context.Background()
	at := core.AgentTypeClaudeCode

	// --- Write side ---
	ctrl1 := newPersistController(t, dir)
	cause := makeTestCause("run-rt-001", "hk-rt01")
	inFlight := []daemon.InFlightBeadRecord{
		{RunID: "run-rt-001", BeadID: "hk-rt01", DispatchedAt: time.Now().UTC().Format(time.RFC3339Nano)},
	}
	if err := ctrl1.Pause(ctx, at, cause, inFlight); err != nil {
		t.Fatalf("Pause: %v", err)
	}

	// Verify the file was written.
	statePath := filepath.Join(dir, "handler-state.json")
	if _, err := os.Stat(statePath); err != nil {
		t.Fatalf("handler-state.json not written: %v", err)
	}

	// --- Read side (simulated restart) ---
	ctrl2 := newLoadController(t)
	if err := daemon.LoadHandlerPauseState(ctx, dir, ctrl2); err != nil {
		t.Fatalf("LoadHandlerPauseState: %v", err)
	}

	// Controller should reflect the paused state.
	if !ctrl2.IsPaused(at) {
		t.Fatal("expected handler to be paused after LoadHandlerPauseState")
	}

	snaps := ctrl2.Status(at)
	if len(snaps) != 1 {
		t.Fatalf("Status returned %d snapshots, want 1", len(snaps))
	}
	snap := snaps[0]
	if !snap.Paused {
		t.Error("snapshot.Paused should be true")
	}
	if snap.Cause == nil {
		t.Fatal("snapshot.Cause should not be nil")
	}
	if snap.Cause.SubReason != "rate_limit" {
		t.Errorf("Cause.SubReason = %q, want rate_limit", snap.Cause.SubReason)
	}
	if snap.Cause.SourceRunID != "run-rt-001" {
		t.Errorf("Cause.SourceRunID = %q, want run-rt-001", snap.Cause.SourceRunID)
	}
	if len(snap.InFlightAtPause) != 1 {
		t.Errorf("InFlightAtPause len = %d, want 1", len(snap.InFlightAtPause))
	}
}

// ---------------------------------------------------------------------------
// Restart preserves paused status (no auto-resume on restart)
// ---------------------------------------------------------------------------

func TestHandlerPausePersist_RestartPreservesPaused(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	ctx := context.Background()
	at := core.AgentTypeClaudeCode

	// First daemon lifecycle: pause the handler.
	ctrl1 := newPersistController(t, dir)
	cause := makeTestCause("run-restart-001", "hk-rs01")
	if err := ctrl1.Pause(ctx, at, cause, nil); err != nil {
		t.Fatalf("Pause: %v", err)
	}

	// Simulate second daemon lifecycle: load state into a fresh controller.
	ctrl2 := newLoadController(t)
	if err := daemon.LoadHandlerPauseState(ctx, dir, ctrl2); err != nil {
		t.Fatalf("LoadHandlerPauseState: %v", err)
	}

	// Handler must still be paused — no auto-resume on restart (QM-055 analog).
	if !ctrl2.IsPaused(at) {
		t.Fatal("paused state was not preserved across simulated restart")
	}

	// Simulate third daemon lifecycle to ensure stability.
	ctrl3 := newLoadController(t)
	if err := daemon.LoadHandlerPauseState(ctx, dir, ctrl3); err != nil {
		t.Fatalf("LoadHandlerPauseState (3rd): %v", err)
	}
	if !ctrl3.IsPaused(at) {
		t.Fatal("paused state not preserved on 3rd simulated restart")
	}
}

// ---------------------------------------------------------------------------
// File absent → no error, all handlers default live
// ---------------------------------------------------------------------------

func TestHandlerPausePersist_FileAbsent(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	ctx := context.Background()

	// Load from a directory that contains no handler-state.json.
	ctrl := newLoadController(t)
	if err := daemon.LoadHandlerPauseState(ctx, dir, ctrl); err != nil {
		t.Fatalf("LoadHandlerPauseState with absent file: %v", err)
	}

	// No handlers should be paused.
	if ctrl.IsPaused(core.AgentTypeClaudeCode) {
		t.Error("handler should be live when file is absent")
	}
	if ctrl.IsPaused(core.AgentTypePi) {
		t.Error("pi handler should be live when file is absent")
	}
}

// ---------------------------------------------------------------------------
// File unparseable → error (refuse startup)
// ---------------------------------------------------------------------------

func TestHandlerPausePersist_FileUnparseable(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	ctx := context.Background()

	// Write garbage JSON.
	statePath := filepath.Join(dir, "handler-state.json")
	if err := os.WriteFile(statePath, []byte("not valid json }{"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	ctrl := newLoadController(t)
	err := daemon.LoadHandlerPauseState(ctx, dir, ctrl)
	if err == nil {
		t.Fatal("expected error for unparseable file, got nil")
	}
}

// ---------------------------------------------------------------------------
// Forward-incompatible schema_version → ErrHandlerStateSchemaUnsupported
// ---------------------------------------------------------------------------

func TestHandlerPausePersist_ForwardIncompatSchema(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	ctx := context.Background()

	// Write a file with schema_version far in the future.
	state := map[string]interface{}{
		"schema_version": 9999,
		"handlers":       map[string]interface{}{},
	}
	data, _ := json.Marshal(state)
	statePath := filepath.Join(dir, "handler-state.json")
	if err := os.WriteFile(statePath, data, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	ctrl := newLoadController(t)
	err := daemon.LoadHandlerPauseState(ctx, dir, ctrl)
	if err == nil {
		t.Fatal("expected error for forward-incompatible schema_version, got nil")
	}
	if !daemon.IsErrHandlerStateSchemaUnsupported(err) {
		t.Errorf("expected ErrHandlerStateSchemaUnsupported, got %T: %v", err, err)
	}
}

// ---------------------------------------------------------------------------
// Resume after load: handler resumes correctly and file is updated
// ---------------------------------------------------------------------------

func TestHandlerPausePersist_ResumeAfterLoad(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	ctx := context.Background()
	at := core.AgentTypeClaudeCode

	// First lifecycle: pause.
	ctrl1 := newPersistController(t, dir)
	cause := makeTestCause("run-resume-001", "hk-re01")
	if err := ctrl1.Pause(ctx, at, cause, nil); err != nil {
		t.Fatalf("Pause: %v", err)
	}

	// Second lifecycle: load then resume.
	ctrl2 := newPersistController(t, dir) // use persist so Resume writes back
	if err := daemon.LoadHandlerPauseState(ctx, dir, ctrl2); err != nil {
		t.Fatalf("LoadHandlerPauseState: %v", err)
	}
	if !ctrl2.IsPaused(at) {
		t.Fatal("handler should be paused after load")
	}
	if err := ctrl2.Resume(ctx, at, core.HandlerResumedByOperator); err != nil {
		t.Fatalf("Resume: %v", err)
	}
	if ctrl2.IsPaused(at) {
		t.Fatal("handler should be live after Resume")
	}

	// Third lifecycle: load again — handler should now be live.
	ctrl3 := newLoadController(t)
	if err := daemon.LoadHandlerPauseState(ctx, dir, ctrl3); err != nil {
		t.Fatalf("LoadHandlerPauseState (after resume): %v", err)
	}
	if ctrl3.IsPaused(at) {
		t.Fatal("handler should be live after resume + load")
	}
}

// ---------------------------------------------------------------------------
// Multiple agent types preserved across restart
// ---------------------------------------------------------------------------

func TestHandlerPausePersist_MultipleAgentTypes(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	ctx := context.Background()

	ctrl1 := newPersistController(t, dir)

	for _, at := range []core.AgentType{core.AgentTypeClaudeCode, core.AgentTypePi} {
		cause := makeTestCause("run-multi", string(at))
		if err := ctrl1.Pause(ctx, at, cause, nil); err != nil {
			t.Fatalf("Pause %q: %v", at, err)
		}
	}

	ctrl2 := newLoadController(t)
	if err := daemon.LoadHandlerPauseState(ctx, dir, ctrl2); err != nil {
		t.Fatalf("LoadHandlerPauseState: %v", err)
	}

	for _, at := range []core.AgentType{core.AgentTypeClaudeCode, core.AgentTypePi} {
		if !ctrl2.IsPaused(at) {
			t.Errorf("expected %q to be paused after load", at)
		}
	}
}

// ---------------------------------------------------------------------------
// Live handlers in the file are not re-seeded as paused
// ---------------------------------------------------------------------------

func TestHandlerPausePersist_LiveHandlerNotSeeded(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	ctx := context.Background()

	// Write a file that explicitly records a handler as "live".
	state := map[string]interface{}{
		"schema_version": 1,
		"handlers": map[string]interface{}{
			"claude-code": map[string]interface{}{
				"status":             "live",
				"cause":              nil,
				"in_flight_at_pause": []interface{}{},
				"paused_epoch":       2,
			},
		},
	}
	data, _ := json.Marshal(state)
	statePath := filepath.Join(dir, "handler-state.json")
	if err := os.WriteFile(statePath, data, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	ctrl := newLoadController(t)
	if err := daemon.LoadHandlerPauseState(ctx, dir, ctrl); err != nil {
		t.Fatalf("LoadHandlerPauseState: %v", err)
	}

	// Handler recorded as live in the file must not be paused.
	if ctrl.IsPaused(core.AgentTypeClaudeCode) {
		t.Error("handler recorded as live in file must not be seeded as paused")
	}
}
