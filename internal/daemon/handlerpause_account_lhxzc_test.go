package daemon_test

// handlerpause_account_lhxzc_test.go — tests for per-account pause within a
// handler type (hk-lhxzc).
//
// Acceptance criteria:
//   - PauseAccount / IsAccountPaused / ResumeAccount basic round-trip
//   - Account-level pause does not affect handler-level IsPaused
//   - Multiple accounts for the same handler type are independent
//   - Persist → load round-trip for account-level state (schema v2)
//   - v1 backwards compat: loading a v1 paused handler seeds the anonymous account
//
// Bead ref: hk-lhxzc.

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

func newAccountTestController(t *testing.T) *daemon.HandlerPauseController {
	t.Helper()
	bus := eventbus.NewBusImpl()
	if err := bus.Seal(); err != nil {
		t.Fatalf("bus.Seal: %v", err)
	}
	return daemon.NewHandlerPauseController(bus, nil)
}

func newAccountPersistController(t *testing.T, stateDir string) *daemon.HandlerPauseController {
	t.Helper()
	bus := eventbus.NewBusImpl()
	if err := bus.Seal(); err != nil {
		t.Fatalf("bus.Seal: %v", err)
	}
	return daemon.NewHandlerPauseController(bus, daemon.MakeHandlerPausePersistFn(stateDir))
}

func makeAccountCause(runID, beadID string) core.HandlerPauseCause {
	return core.HandlerPauseCause{
		FailureClass: core.FailureClassTransient,
		SubReason:    "rate_limit",
		SourceRunID:  runID,
		SourceBeadID: beadID,
		TrippedAt:    time.Now().UTC().Format(time.RFC3339Nano),
	}
}

// ---------------------------------------------------------------------------
// Basic: PauseAccount / IsAccountPaused
// ---------------------------------------------------------------------------

func TestAccountPause_Basic(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	ctrl := newAccountTestController(t)
	at := core.AgentTypeClaudeCode
	acct := daemon.AccountID("account-1")

	cause := makeAccountCause("run-acct-001", "hk-a001")
	if err := ctrl.PauseAccount(ctx, at, acct, cause, nil); err != nil {
		t.Fatalf("PauseAccount: %v", err)
	}

	if !ctrl.IsAccountPaused(at, acct) {
		t.Error("IsAccountPaused should be true after PauseAccount")
	}

	// Handler-level pause must NOT be set — per-account pause is independent.
	if ctrl.IsPaused(at) {
		t.Error("handler-level IsPaused must not be affected by per-account pause")
	}
}

// ---------------------------------------------------------------------------
// Idempotent: second PauseAccount on same account is a no-op
// ---------------------------------------------------------------------------

func TestAccountPause_Idempotent(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	ctrl := newAccountTestController(t)
	at := core.AgentTypeClaudeCode
	acct := daemon.AccountID("account-idem")

	cause := makeAccountCause("run-idem-001", "hk-idem01")
	if err := ctrl.PauseAccount(ctx, at, acct, cause, nil); err != nil {
		t.Fatalf("first PauseAccount: %v", err)
	}
	// Second call must not error.
	if err := ctrl.PauseAccount(ctx, at, acct, cause, nil); err != nil {
		t.Fatalf("second PauseAccount (idempotent): %v", err)
	}
	if !ctrl.IsAccountPaused(at, acct) {
		t.Error("IsAccountPaused should still be true")
	}
}

// ---------------------------------------------------------------------------
// ResumeAccount: clears account pause
// ---------------------------------------------------------------------------

func TestAccountPause_Resume(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	ctrl := newAccountTestController(t)
	at := core.AgentTypeClaudeCode
	acct := daemon.AccountID("account-resume")

	cause := makeAccountCause("run-res-001", "hk-res01")
	if err := ctrl.PauseAccount(ctx, at, acct, cause, nil); err != nil {
		t.Fatalf("PauseAccount: %v", err)
	}
	if !ctrl.IsAccountPaused(at, acct) {
		t.Fatal("expected account to be paused")
	}

	if err := ctrl.ResumeAccount(ctx, at, acct, core.HandlerResumedByOperator); err != nil {
		t.Fatalf("ResumeAccount: %v", err)
	}

	if ctrl.IsAccountPaused(at, acct) {
		t.Error("IsAccountPaused should be false after ResumeAccount")
	}
}

// ---------------------------------------------------------------------------
// ResumeAccount on a non-paused account returns ErrHandlerNotPaused
// ---------------------------------------------------------------------------

func TestAccountPause_ResumeNotPaused(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	ctrl := newAccountTestController(t)
	at := core.AgentTypeClaudeCode
	acct := daemon.AccountID("account-ghost")

	err := ctrl.ResumeAccount(ctx, at, acct, core.HandlerResumedByOperator)
	if err == nil {
		t.Fatal("expected error when resuming a non-paused account, got nil")
	}
}

// ---------------------------------------------------------------------------
// Multiple accounts are independent
// ---------------------------------------------------------------------------

func TestAccountPause_MultiAccount(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	ctrl := newAccountTestController(t)
	at := core.AgentTypeClaudeCode

	acct1 := daemon.AccountID("account-multi-1")
	acct2 := daemon.AccountID("account-multi-2")

	cause1 := makeAccountCause("run-multi-001", "hk-m001")
	if err := ctrl.PauseAccount(ctx, at, acct1, cause1, nil); err != nil {
		t.Fatalf("PauseAccount acct1: %v", err)
	}

	if !ctrl.IsAccountPaused(at, acct1) {
		t.Error("acct1 should be paused")
	}
	if ctrl.IsAccountPaused(at, acct2) {
		t.Error("acct2 should not be paused (independent)")
	}

	// Resume acct1 — acct2 remains unaffected.
	if err := ctrl.ResumeAccount(ctx, at, acct1, core.HandlerResumedByOperator); err != nil {
		t.Fatalf("ResumeAccount acct1: %v", err)
	}
	if ctrl.IsAccountPaused(at, acct1) {
		t.Error("acct1 should be live after Resume")
	}
}

// ---------------------------------------------------------------------------
// Persist → load round-trip for account-level state (schema v2)
// ---------------------------------------------------------------------------

func TestAccountPause_PersistRoundTrip(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	ctx := context.Background()
	at := core.AgentTypeClaudeCode
	acct := daemon.AccountID("account-persist")

	// Write side.
	ctrl1 := newAccountPersistController(t, dir)
	cause := makeAccountCause("run-persist-001", "hk-p001")
	inFlight := []daemon.InFlightBeadRecord{
		{RunID: "run-persist-001", BeadID: "hk-p001", DispatchedAt: time.Now().UTC().Format(time.RFC3339Nano)},
	}
	if err := ctrl1.PauseAccount(ctx, at, acct, cause, inFlight); err != nil {
		t.Fatalf("PauseAccount: %v", err)
	}

	// Verify file written.
	statePath := filepath.Join(dir, "handler-state.json")
	raw, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("handler-state.json not written: %v", err)
	}

	// Verify schema v2 in the file.
	var top map[string]interface{}
	if err := json.Unmarshal(raw, &top); err != nil {
		t.Fatalf("unmarshal state file: %v", err)
	}
	if sv, _ := top["schema_version"].(float64); int(sv) != 2 {
		t.Errorf("expected schema_version 2 in written file, got %v", top["schema_version"])
	}

	// Read side (simulated restart).
	bus := eventbus.NewBusImpl()
	if err := bus.Seal(); err != nil {
		t.Fatalf("bus.Seal: %v", err)
	}
	ctrl2 := daemon.NewHandlerPauseController(bus, nil)
	if err := daemon.LoadHandlerPauseState(ctx, dir, ctrl2); err != nil {
		t.Fatalf("LoadHandlerPauseState: %v", err)
	}

	if !ctrl2.IsAccountPaused(at, acct) {
		t.Fatal("account should be paused after LoadHandlerPauseState")
	}
}

// ---------------------------------------------------------------------------
// v1 backwards compat: v1 paused handler loads anonymous account
// ---------------------------------------------------------------------------

func TestAccountPause_V1BackwardsCompat(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	ctx := context.Background()

	// Write a v1 schema file with a paused claude-code handler.
	v1State := map[string]interface{}{
		"schema_version": 1,
		"handlers": map[string]interface{}{
			"claude-code": map[string]interface{}{
				"status": "paused",
				"cause": map[string]interface{}{
					"failure_class": "transient",
					"sub_reason":    "rate_limit",
					"source_run_id": "run-v1-001",
					"source_bead_id": "hk-v1a",
					"tripped_at":    time.Now().UTC().Format(time.RFC3339Nano),
				},
				"in_flight_at_pause": []interface{}{},
				"paused_epoch":       1,
			},
		},
	}
	data, _ := json.Marshal(v1State)
	if err := os.WriteFile(filepath.Join(dir, "handler-state.json"), data, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	bus := eventbus.NewBusImpl()
	if err := bus.Seal(); err != nil {
		t.Fatalf("bus.Seal: %v", err)
	}
	ctrl := daemon.NewHandlerPauseController(bus, nil)
	if err := daemon.LoadHandlerPauseState(ctx, dir, ctrl); err != nil {
		t.Fatalf("LoadHandlerPauseState: %v", err)
	}

	at := core.AgentTypeClaudeCode

	// Handler-level pause must be restored (existing behaviour).
	if !ctrl.IsPaused(at) {
		t.Error("handler-level IsPaused should be true after loading v1 paused handler")
	}

	// Anonymous account must also be paused (HP-072 v1 compat).
	if !ctrl.IsAccountPaused(at, daemon.AnonymousAccountID) {
		t.Error("anonymous account should be paused when loading v1 paused handler (HP-072 backwards compat)")
	}
}

// ---------------------------------------------------------------------------
// Account-level pause survives ResumeAccount for other accounts
// ---------------------------------------------------------------------------

func TestAccountPause_IndependentResume(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	ctx := context.Background()
	at := core.AgentTypeClaudeCode

	ctrl := newAccountPersistController(t, dir)

	acctA := daemon.AccountID("account-A")
	acctB := daemon.AccountID("account-B")

	causeA := makeAccountCause("run-indep-A", "hk-iA")
	causeB := makeAccountCause("run-indep-B", "hk-iB")

	if err := ctrl.PauseAccount(ctx, at, acctA, causeA, nil); err != nil {
		t.Fatalf("PauseAccount A: %v", err)
	}
	if err := ctrl.PauseAccount(ctx, at, acctB, causeB, nil); err != nil {
		t.Fatalf("PauseAccount B: %v", err)
	}

	// Resume A — B must remain paused.
	if err := ctrl.ResumeAccount(ctx, at, acctA, core.HandlerResumedByOperator); err != nil {
		t.Fatalf("ResumeAccount A: %v", err)
	}

	if ctrl.IsAccountPaused(at, acctA) {
		t.Error("account A should be live after Resume")
	}
	if !ctrl.IsAccountPaused(at, acctB) {
		t.Error("account B should still be paused (independent)")
	}

	// Reload from disk — B still paused.
	bus := eventbus.NewBusImpl()
	if err := bus.Seal(); err != nil {
		t.Fatalf("bus.Seal: %v", err)
	}
	ctrl2 := daemon.NewHandlerPauseController(bus, nil)
	if err := daemon.LoadHandlerPauseState(ctx, dir, ctrl2); err != nil {
		t.Fatalf("LoadHandlerPauseState: %v", err)
	}
	if ctrl2.IsAccountPaused(at, acctA) {
		t.Error("after reload: account A should be live")
	}
	if !ctrl2.IsAccountPaused(at, acctB) {
		t.Error("after reload: account B should still be paused")
	}
}
