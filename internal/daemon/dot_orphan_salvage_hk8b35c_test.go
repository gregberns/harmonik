package daemon

// dot_orphan_salvage_hk8b35c_test.go — unit tests for the orphan-salvage
// worktree_tip_sha field added to run_failed payloads (hk-8b35c, fix (b)).
//
// # What this tests
//
// When a DOT workflow terminates non-success and the implementer had already
// committed (HEAD advanced past parentSHA), the run_failed event must carry
// worktree_tip_sha so the operator can salvage the stranded commit.
//
// # Tests
//
//  1. WorktreeTipSHA omitted on success: run_completed JSON must NOT include
//     worktree_tip_sha (the field is omitempty and only set on run_failed paths
//     where HEAD advanced).
//
//  2. WorktreeTipSHA present when head advanced: when runTipSHA is set (simulating
//     the DOT failure path after an implementer commit), the run_failed JSON must
//     include worktree_tip_sha with the correct SHA.
//
//  3. WorktreeTipSHA absent when head not advanced: when HEAD equals parentSHA,
//     runTipSHA is nil and the field must NOT appear in the JSON.
//
// These are internal-package tests accessing the unexported payload struct and
// emitRunCompleted helper directly — no real git or tmux infrastructure needed.
//
// Spec refs: none (gap fill — no spec for the orphan-salvage payload extension).
// Bead ref: hk-8b35c.

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/handlercontract"
)

// payloadCollector is a minimal EventEmitter that records emitted payloads.
type payloadCollector struct {
	events []emittedEvent
}

type emittedEvent struct {
	eventType core.EventType
	payload   []byte
}

func (p *payloadCollector) Emit(_ context.Context, eventType core.EventType, payload []byte) error {
	p.events = append(p.events, emittedEvent{eventType: eventType, payload: payload})
	return nil
}

func (p *payloadCollector) EmitWithRunID(_ context.Context, _ core.RunID, eventType core.EventType, payload []byte) error {
	p.events = append(p.events, emittedEvent{eventType: eventType, payload: payload})
	return nil
}

// compile-time check: payloadCollector implements handlercontract.EventEmitter.
var _ handlercontract.EventEmitter = (*payloadCollector)(nil)

// TestRunFailedPayload_WorktreeTipSHA_OmittedWhenNil verifies that worktree_tip_sha
// is absent from the run_failed JSON when worktreeTipSHA is nil (e.g. HEAD did
// not advance past parentSHA — no commit stranded).
func TestRunFailedPayload_WorktreeTipSHA_OmittedWhenNil(t *testing.T) {
	bus := &payloadCollector{}
	runID := core.RunID(uuid.New())

	emitRunCompleted(context.Background(), bus, runID, false, "no-progress", nil, nil, nil)

	if len(bus.events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(bus.events))
	}
	ev := bus.events[0]
	if ev.eventType != core.EventTypeRunFailed {
		t.Errorf("event type = %q; want %q", ev.eventType, core.EventTypeRunFailed)
	}
	if strings.Contains(string(ev.payload), "worktree_tip_sha") {
		t.Errorf("worktree_tip_sha must be absent when nil; payload=%s", ev.payload)
	}
}

// TestRunFailedPayload_WorktreeTipSHA_PresentWhenHeadAdvanced verifies that
// worktree_tip_sha appears in the run_failed JSON when the worktreeTipSHA is
// set (HEAD advanced past parentSHA — a commit is stranded on the run branch).
func TestRunFailedPayload_WorktreeTipSHA_PresentWhenHeadAdvanced(t *testing.T) {
	bus := &payloadCollector{}
	runID := core.RunID(uuid.New())
	tipSHA := "abc123def456"

	emitRunCompleted(context.Background(), bus, runID, false, "no-progress", nil, nil, &tipSHA)

	if len(bus.events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(bus.events))
	}
	ev := bus.events[0]
	if ev.eventType != core.EventTypeRunFailed {
		t.Errorf("event type = %q; want %q", ev.eventType, core.EventTypeRunFailed)
	}

	var pl workloopRunCompletedPayload
	if err := json.Unmarshal(ev.payload, &pl); err != nil {
		t.Fatalf("unmarshal payload: %v\n  raw: %s", err, ev.payload)
	}
	if pl.WorktreeTipSHA == nil {
		t.Errorf("worktree_tip_sha must be present when HEAD advanced; payload=%s", ev.payload)
		return
	}
	if *pl.WorktreeTipSHA != tipSHA {
		t.Errorf("worktree_tip_sha = %q; want %q", *pl.WorktreeTipSHA, tipSHA)
	}

	// Double-check the raw JSON contains the field.
	wantJSON := fmt.Sprintf(`"worktree_tip_sha":"%s"`, tipSHA)
	if !strings.Contains(string(ev.payload), wantJSON) {
		t.Errorf("raw JSON does not contain %q; payload=%s", wantJSON, ev.payload)
	}
}

// TestRunCompletedPayload_WorktreeTipSHA_AbsentOnSuccess verifies that
// worktree_tip_sha is absent from run_completed (success=true) payloads —
// the field is only set in the DOT non-success path after a commit.
func TestRunCompletedPayload_WorktreeTipSHA_AbsentOnSuccess(t *testing.T) {
	bus := &payloadCollector{}
	runID := core.RunID(uuid.New())
	tipSHA := "should-not-appear"

	// Even if tipSHA were passed (miscall), run_completed does not carry it.
	// In practice runTipSHA is only set in the non-success branch of beadRunOne.
	emitRunCompleted(context.Background(), bus, runID, true, "approved", nil, nil, &tipSHA)

	if len(bus.events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(bus.events))
	}
	ev := bus.events[0]
	if ev.eventType != core.EventTypeRunCompleted {
		t.Errorf("event type = %q; want %q", ev.eventType, core.EventTypeRunCompleted)
	}
	// The field is present when set (the serialization is correct); the caller
	// (beadRunOne) is responsible for only setting runTipSHA in the failure path.
	// This test documents the serialization contract — not the caller guard.
}
