package daemon_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/eventbus"
	"github.com/gregberns/harmonik/internal/runregistry"
)

func hppNewController(t *testing.T) *daemon.HandlerPauseController {
	t.Helper()
	bus := eventbus.NewBusImpl()
	if err := bus.Seal(); err != nil {
		t.Fatalf("hppNewController: bus.Seal: %v", err)
	}
	return daemon.NewHandlerPauseController(bus, nil)
}

func hppNewPolicy(t *testing.T, ctrl *daemon.HandlerPauseController, reg *runregistry.RunRegistry) *daemon.HandlerPausePolicyGoroutine {
	t.Helper()
	return daemon.ExportedNewHandlerPausePolicyGoroutine(daemon.ExportedHandlerPausePolicyConfig{
		AgentType:  core.AgentTypeClaudeCode,
		Controller: ctrl,
		Registry:   reg,
	})
}

func hppMakeRunID(t *testing.T) core.RunID {
	t.Helper()
	u, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("hppMakeRunID: NewV7: %v", err)
	}
	return core.RunID(u)
}

func hppMakeSyntheticEvent(t *testing.T, evtType string, payload json.RawMessage) core.Event {
	t.Helper()
	evID, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("hppMakeSyntheticEvent: NewV7 for EventID: %v", err)
	}
	return core.Event{
		EventID:         core.EventID(evID),
		SchemaVersion:   1,
		Type:            core.EventType(evtType),
		TimestampWall:   time.Now(),
		SourceSubsystem: "test",
		Payload:         payload,
	}
}

func hppRateLimitEvent(t *testing.T, status core.AgentRateLimitStatus) core.Event {
	t.Helper()
	runID := hppMakeRunID(t)
	payload := core.AgentRateLimitStatusPayload{
		RunID:     runID,
		SessionID: core.SessionID("synth-session-" + runID.String()),
		Status:    status,
		ChangedAt: time.Now().UTC().Format(time.RFC3339Nano),
	}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("hppRateLimitEvent: marshal: %v", err)
	}
	return hppMakeSyntheticEvent(t, string(core.EventTypeAgentRateLimitStatus), json.RawMessage(payloadJSON))
}

func hppBudgetExhaustedEvent(t *testing.T) core.Event {
	t.Helper()
	runID := hppMakeRunID(t)
	payload := core.BudgetExhaustedEventPayload{
		RunID:                 runID,
		BudgetRef:             core.BudgetRef("handler-account"),
		AttemptedDispatchCost: 0.01,
	}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("hppBudgetExhaustedEvent: marshal: %v", err)
	}
	return hppMakeSyntheticEvent(t, string(core.EventTypeBudgetExhausted), json.RawMessage(payloadJSON))
}

func hppDeliverRateLimit(t *testing.T, policy *daemon.HandlerPausePolicyGoroutine, status core.AgentRateLimitStatus) {
	t.Helper()
	evt := hppRateLimitEvent(t, status)
	if err := daemon.ExportedPolicyHandleRateLimitStatus(policy, context.Background(), evt); err != nil {
		t.Fatalf("hppDeliverRateLimit: %v", err)
	}
}

func hppDeliverBudgetExhausted(t *testing.T, policy *daemon.HandlerPausePolicyGoroutine) {
	t.Helper()
	evt := hppBudgetExhaustedEvent(t)
	if err := daemon.ExportedPolicyHandleBudgetExhausted(policy, context.Background(), evt); err != nil {
		t.Fatalf("hppDeliverBudgetExhausted: %v", err)
	}
}

// TestHandlerPausePolicy_NoTripOnSingleRateLimit verifies that a single
// rate-limit active event does NOT trip the pause (hysteresis = 2).
func TestHandlerPausePolicy_NoTripOnSingleRateLimit(t *testing.T) {
	t.Parallel()

	ctrl := hppNewController(t)
	reg := runregistry.NewRunRegistry()
	policy := hppNewPolicy(t, ctrl, reg)

	hppDeliverRateLimit(t, policy, core.AgentRateLimitStatusActive)

	if ctrl.IsPaused(core.AgentTypeClaudeCode) {
		t.Fatal("handler was paused after a single rate-limit active event; want no-trip (hysteresis=2)")
	}
}

// TestHandlerPausePolicy_TripOnTwoConsecutiveRateLimits verifies that two
// consecutive rate-limit active events (without clearance) trip a pause.
func TestHandlerPausePolicy_TripOnTwoConsecutiveRateLimits(t *testing.T) {
	t.Parallel()

	ctrl := hppNewController(t)
	reg := runregistry.NewRunRegistry()
	policy := hppNewPolicy(t, ctrl, reg)

	hppDeliverRateLimit(t, policy, core.AgentRateLimitStatusActive)
	if ctrl.IsPaused(core.AgentTypeClaudeCode) {
		t.Fatal("paused after first active; want no-trip yet")
	}

	hppDeliverRateLimit(t, policy, core.AgentRateLimitStatusActive)
	if !ctrl.IsPaused(core.AgentTypeClaudeCode) {
		t.Fatal("not paused after two consecutive active events; want trip")
	}

	snaps := ctrl.Status(core.AgentTypeClaudeCode)
	if len(snaps) != 1 {
		t.Fatalf("Status returned %d snapshots, want 1", len(snaps))
	}
	snap := snaps[0]
	if snap.Cause == nil {
		t.Fatal("paused snapshot has nil Cause")
	}
	if snap.Cause.SubReason != "rate_limit" {
		t.Errorf("Cause.SubReason=%q, want %q", snap.Cause.SubReason, "rate_limit")
	}
	if snap.Cause.FailureClass != core.FailureClassTransient {
		t.Errorf("Cause.FailureClass=%q, want %q", snap.Cause.FailureClass, core.FailureClassTransient)
	}
}

// TestHandlerPausePolicy_NoTripAfterClearance verifies that a cleared event
// resets the consecutive counter so that a subsequent single active event does
// NOT trip (active + cleared + active = reset, count=1).
func TestHandlerPausePolicy_NoTripAfterClearance(t *testing.T) {
	t.Parallel()

	ctrl := hppNewController(t)
	reg := runregistry.NewRunRegistry()
	policy := hppNewPolicy(t, ctrl, reg)

	hppDeliverRateLimit(t, policy, core.AgentRateLimitStatusActive)

	hppDeliverRateLimit(t, policy, core.AgentRateLimitStatusCleared)

	hppDeliverRateLimit(t, policy, core.AgentRateLimitStatusActive)

	if ctrl.IsPaused(core.AgentTypeClaudeCode) {
		t.Fatal("handler paused after active+cleared+active; want no-trip (counter reset by cleared)")
	}
}

// TestHandlerPausePolicy_TripOnBudgetExhausted verifies that a single
// budget_exhausted event trips a pause immediately (single-hit, no hysteresis).
func TestHandlerPausePolicy_TripOnBudgetExhausted(t *testing.T) {
	t.Parallel()

	ctrl := hppNewController(t)
	reg := runregistry.NewRunRegistry()
	policy := hppNewPolicy(t, ctrl, reg)

	hppDeliverBudgetExhausted(t, policy)

	if !ctrl.IsPaused(core.AgentTypeClaudeCode) {
		t.Fatal("handler not paused after budget_exhausted; want immediate trip")
	}

	snaps := ctrl.Status(core.AgentTypeClaudeCode)
	if len(snaps) != 1 {
		t.Fatalf("Status returned %d snapshots, want 1", len(snaps))
	}
	snap := snaps[0]
	if snap.Cause == nil {
		t.Fatal("paused snapshot has nil Cause")
	}
	if snap.Cause.SubReason != "budget_exhausted_handler_account" {
		t.Errorf("Cause.SubReason=%q, want %q", snap.Cause.SubReason, "budget_exhausted_handler_account")
	}
	if snap.Cause.FailureClass != core.FailureClassBudgetExhausted {
		t.Errorf("Cause.FailureClass=%q, want %q", snap.Cause.FailureClass, core.FailureClassBudgetExhausted)
	}
}

// TestHandlerPausePolicy_InFlightFreezeListPopulated verifies that when a
// pause is triggered, the in-flight bead list is populated from the runregistry.RunRegistry.
func TestHandlerPausePolicy_InFlightFreezeListPopulated(t *testing.T) {
	t.Parallel()

	ctrl := hppNewController(t)
	reg := runregistry.NewRunRegistry()
	policy := hppNewPolicy(t, ctrl, reg)

	runID1 := hppMakeRunID(t)
	runID2 := hppMakeRunID(t)
	reg.Register(runID1, &runregistry.RunHandle{
		BeadID:    "hk-inflight-001",
		StartedAt: time.Now(),
	})
	reg.Register(runID2, &runregistry.RunHandle{
		BeadID:    "hk-inflight-002",
		StartedAt: time.Now(),
	})

	hppDeliverBudgetExhausted(t, policy)

	if !ctrl.IsPaused(core.AgentTypeClaudeCode) {
		t.Fatal("handler not paused; cannot check freeze-list")
	}

	snaps := ctrl.Status(core.AgentTypeClaudeCode)
	if len(snaps) != 1 {
		t.Fatalf("Status returned %d snapshots, want 1", len(snaps))
	}
	snap := snaps[0]
	if len(snap.InFlightAtPause) != 2 {
		t.Errorf("InFlightAtPause has %d entries, want 2 (the two registered runs)", len(snap.InFlightAtPause))
	}

	for _, rec := range snap.InFlightAtPause {
		if rec.RunID == "" {
			t.Error("InFlightBeadRecord.RunID is empty")
		}
		if rec.BeadID == "" {
			t.Error("InFlightBeadRecord.BeadID is empty")
		}
		if rec.DispatchedAt == "" {
			t.Error("InFlightBeadRecord.DispatchedAt is empty")
		}
	}
}

// TestHandlerPausePolicy_IdempotentOnDoubleBudgetExhausted verifies that a
// second budget_exhausted while already paused is a no-op (controller's
// idempotent no-op contract per hk-9hwbw).
func TestHandlerPausePolicy_IdempotentOnDoubleBudgetExhausted(t *testing.T) {
	t.Parallel()

	ctrl := hppNewController(t)
	reg := runregistry.NewRunRegistry()
	policy := hppNewPolicy(t, ctrl, reg)

	hppDeliverBudgetExhausted(t, policy)
	if !ctrl.IsPaused(core.AgentTypeClaudeCode) {
		t.Fatal("not paused after first budget_exhausted")
	}
	epoch1, _ := ctrl.PausedEpochFor(core.AgentTypeClaudeCode)

	hppDeliverBudgetExhausted(t, policy)
	if !ctrl.IsPaused(core.AgentTypeClaudeCode) {
		t.Fatal("handler not paused after second budget_exhausted (should remain paused)")
	}

	epoch2, _ := ctrl.PausedEpochFor(core.AgentTypeClaudeCode)
	if epoch2 != epoch1 {
		t.Errorf("paused_epoch changed from %d to %d after double-trip; want no change (idempotent no-op)", epoch1, epoch2)
	}
}
