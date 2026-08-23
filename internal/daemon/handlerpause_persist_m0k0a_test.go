package daemon_test

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

func newPersistController(t *testing.T, stateDir string) *daemon.HandlerPauseController {
	t.Helper()
	bus := eventbus.NewBusImpl()
	if err := bus.Seal(); err != nil {
		t.Fatalf("bus.Seal: %v", err)
	}
	persistFn := daemon.MakeHandlerPausePersistFn(stateDir)
	return daemon.NewHandlerPauseController(bus, persistFn)
}

func newLoadController(t *testing.T) *daemon.HandlerPauseController {
	t.Helper()
	bus := eventbus.NewBusImpl()
	if err := bus.Seal(); err != nil {
		t.Fatalf("bus.Seal: %v", err)
	}
	return daemon.NewHandlerPauseController(bus, nil)
}

func makeTestCause(runID, beadID string) core.HandlerPauseCause {
	return core.HandlerPauseCause{
		FailureClass: core.FailureClassTransient,
		SubReason:    "rate_limit",
		SourceRunID:  runID,
		SourceBeadID: beadID,
		TrippedAt:    time.Now().UTC().Format(time.RFC3339Nano),
	}
}

func TestHandlerPausePersist_RoundTrip(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	ctx := context.Background()
	at := core.AgentTypeClaudeCode

	ctrl1 := newPersistController(t, dir)
	cause := makeTestCause("run-rt-001", "hk-rt01")
	inFlight := []daemon.InFlightBeadRecord{
		{RunID: "run-rt-001", BeadID: "hk-rt01", DispatchedAt: time.Now().UTC().Format(time.RFC3339Nano)},
	}
	if err := ctrl1.Pause(ctx, at, cause, inFlight); err != nil {
		t.Fatalf("Pause: %v", err)
	}

	statePath := filepath.Join(dir, "handler-state.json")
	if _, err := os.Stat(statePath); err != nil {
		t.Fatalf("handler-state.json not written: %v", err)
	}

	ctrl2 := newLoadController(t)
	if err := daemon.LoadHandlerPauseState(ctx, dir, ctrl2); err != nil {
		t.Fatalf("LoadHandlerPauseState: %v", err)
	}

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

func TestHandlerPausePersist_RestartPreservesPaused(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	ctx := context.Background()
	at := core.AgentTypeClaudeCode

	ctrl1 := newPersistController(t, dir)
	cause := makeTestCause("run-restart-001", "hk-rs01")
	if err := ctrl1.Pause(ctx, at, cause, nil); err != nil {
		t.Fatalf("Pause: %v", err)
	}

	ctrl2 := newLoadController(t)
	if err := daemon.LoadHandlerPauseState(ctx, dir, ctrl2); err != nil {
		t.Fatalf("LoadHandlerPauseState: %v", err)
	}

	if !ctrl2.IsPaused(at) {
		t.Fatal("paused state was not preserved across simulated restart")
	}

	ctrl3 := newLoadController(t)
	if err := daemon.LoadHandlerPauseState(ctx, dir, ctrl3); err != nil {
		t.Fatalf("LoadHandlerPauseState (3rd): %v", err)
	}
	if !ctrl3.IsPaused(at) {
		t.Fatal("paused state not preserved on 3rd simulated restart")
	}
}

func TestHandlerPausePersist_FileAbsent(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	ctx := context.Background()

	ctrl := newLoadController(t)
	if err := daemon.LoadHandlerPauseState(ctx, dir, ctrl); err != nil {
		t.Fatalf("LoadHandlerPauseState with absent file: %v", err)
	}

	if ctrl.IsPaused(core.AgentTypeClaudeCode) {
		t.Error("handler should be live when file is absent")
	}
	if ctrl.IsPaused(core.AgentTypePi) {
		t.Error("pi handler should be live when file is absent")
	}
}

func TestHandlerPausePersist_FileUnparseable(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	ctx := context.Background()

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

func TestHandlerPausePersist_ForwardIncompatSchema(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	ctx := context.Background()

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

func TestHandlerPausePersist_ResumeAfterLoad(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	ctx := context.Background()
	at := core.AgentTypeClaudeCode

	ctrl1 := newPersistController(t, dir)
	cause := makeTestCause("run-resume-001", "hk-re01")
	if err := ctrl1.Pause(ctx, at, cause, nil); err != nil {
		t.Fatalf("Pause: %v", err)
	}

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

	ctrl3 := newLoadController(t)
	if err := daemon.LoadHandlerPauseState(ctx, dir, ctrl3); err != nil {
		t.Fatalf("LoadHandlerPauseState (after resume): %v", err)
	}
	if ctrl3.IsPaused(at) {
		t.Fatal("handler should be live after resume + load")
	}
}

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

func TestHandlerPausePersist_LiveHandlerNotSeeded(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	ctx := context.Background()

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

	if ctrl.IsPaused(core.AgentTypeClaudeCode) {
		t.Error("handler recorded as live in file must not be seeded as paused")
	}
}
