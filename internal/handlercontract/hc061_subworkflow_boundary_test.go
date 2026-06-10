package handlercontract_test

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/handlercontract"
)

// hc061 — per-bead helper prefix for test helpers in this file.
// (implementer-protocol.md §Helper-prefix discipline; bead hk-emggz)
//
// Spec refs:
//
//	specs/handler-contract.md §4.2a HC-058 — per-node-type Outcome emission obligations table.
//	specs/handler-contract.md §4.2a HC-061 — sub-workflow boundary handlers MUST NOT emit Outcome.

// ─────────────────────────────────────────────────────────────────────────────
// HC-061: spec-corpus sensor
// ─────────────────────────────────────────────────────────────────────────────

// TestHC061_SpecCorpusClause verifies that handler-contract.md contains HC-061
// and the key normative constraint clauses.
func TestHC061_SpecCorpusClause(t *testing.T) {
	t.Parallel()

	spec := twinParityFixtureHCSpec(t)

	if !strings.Contains(spec, "HC-061") {
		t.Error("handler-contract.md missing HC-061 clause")
	}
	// HC-061 must state the prohibition.
	if !strings.Contains(spec, "MUST NOT emit an Outcome") {
		t.Error("handler-contract.md HC-061 missing 'MUST NOT emit an Outcome' prohibition; spec may have drifted")
	}
	// HC-061 must name the sub-reason.
	if !strings.Contains(spec, "subworkflow_boundary_emit") {
		t.Error("handler-contract.md HC-061 missing 'subworkflow_boundary_emit' sub-reason; spec may have drifted")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// HC-061: sub-reason constant sensor
// ─────────────────────────────────────────────────────────────────────────────

// TestHC061_SubReasonConstantValue verifies that SubworkflowBoundaryEmitSubReason
// has the exact wire value required by HC-061.
func TestHC061_SubReasonConstantValue(t *testing.T) {
	t.Parallel()

	const wantValue = "subworkflow_boundary_emit"
	got := handlercontract.SubworkflowBoundaryEmitSubReason
	if got != wantValue {
		t.Errorf("HC-061: SubworkflowBoundaryEmitSubReason = %q; want %q (stable wire value for parity)", got, wantValue)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// HC-061: runtime enforcement — watcher rejects outcome_emitted on sub-workflow
// ─────────────────────────────────────────────────────────────────────────────

// TestHC061_WatcherRejectsOutcomeOnSubWorkflowNode verifies that SpawnWatcher
// emits agent_failed{structural, subworkflow_boundary_emit} when it receives an
// outcome_emitted progress-stream message while cfg.NodeType is
// core.NodeTypeSubWorkflow.
//
// This is the runtime enforcement of HC-061: sub-workflow boundary handlers
// MUST NOT emit an Outcome; the watcher is the structural guard.
func TestHC061_WatcherRejectsOutcomeOnSubWorkflowNode(t *testing.T) {
	t.Parallel()

	outcomeMsg := `{"type":"outcome_emitted","run_id":"00000000-0000-0000-0000-000000000001","session_id":"test-hc061","node_id":"subwf-node","outcome_status":"SUCCESS"}` + "\n"

	pr, pw := io.Pipe()

	go func() {
		_, _ = pw.Write([]byte(outcomeMsg))
		pw.Close()
	}()

	pub := &watcherFixturePublisher{}
	dl := &watcherFixtureDeadLetter{}

	w := handlercontract.SpawnWatcher(context.Background(), handlercontract.SpawnWatcherConfig{
		SessionID:      core.SessionID("test-hc061"),
		ProgressStream: pr,
		Publisher:      pub,
		DeadLetter:     dl,
		NodeType:       core.NodeTypeSubWorkflow,
	})

	select {
	case <-w.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("HC-061: watcher did not terminate within 5s after outcome_emitted on sub-workflow node")
	}

	// The watcher MUST NOT have forwarded outcome_emitted to the bus.
	for _, et := range pub.EventTypes() {
		if et == handlercontract.ProgressMsgTypeOutcomeEmitted {
			t.Error("HC-061: watcher forwarded outcome_emitted to bus; MUST reject it for sub-workflow nodes")
		}
	}

	// The watcher MUST have published agent_failed.
	found := false
	for _, et := range pub.EventTypes() {
		if et == handlercontract.ProgressMsgTypeAgentFailed {
			found = true
		}
	}
	if !found {
		t.Errorf("HC-061: watcher did not emit agent_failed; published event types: %v", pub.EventTypes())
	}
}

// TestHC061_WatcherAllowsOutcomeOnNonSubWorkflowNode verifies that the HC-061
// guard is inactive when NodeType is NOT NodeTypeSubWorkflow. outcome_emitted
// for agentic / non-agentic / gate nodes MUST pass through to the bus normally.
func TestHC061_WatcherAllowsOutcomeOnNonSubWorkflowNode(t *testing.T) {
	t.Parallel()

	outcomeMsg := `{"type":"outcome_emitted","run_id":"00000000-0000-0000-0000-000000000002","session_id":"test-hc061-allow","node_id":"agentic-node","outcome_status":"SUCCESS"}` + "\n"

	pr, pw := io.Pipe()

	go func() {
		_, _ = pw.Write([]byte(outcomeMsg))
		pw.Close()
	}()

	pub := &watcherFixturePublisher{}
	dl := &watcherFixtureDeadLetter{}

	w := handlercontract.SpawnWatcher(context.Background(), handlercontract.SpawnWatcherConfig{
		SessionID:      core.SessionID("test-hc061-allow"),
		ProgressStream: pr,
		Publisher:      pub,
		DeadLetter:     dl,
		NodeType:       core.NodeTypeAgentic,
	})

	select {
	case <-w.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("HC-061: watcher did not terminate within 5s")
	}

	found := false
	for _, et := range pub.EventTypes() {
		if et == handlercontract.ProgressMsgTypeOutcomeEmitted {
			found = true
		}
	}
	if !found {
		t.Errorf("HC-061: watcher did NOT forward outcome_emitted for agentic node; published: %v — guard must be inactive for non-sub-workflow nodes", pub.EventTypes())
	}
}

// TestHC061_WatcherAllowsOutcomeWhenNodeTypeUnset verifies that the HC-061
// guard is inactive when NodeType is the zero value (empty string), preserving
// backward compatibility for callers that predate HC-061.
func TestHC061_WatcherAllowsOutcomeWhenNodeTypeUnset(t *testing.T) {
	t.Parallel()

	outcomeMsg := `{"type":"outcome_emitted","run_id":"00000000-0000-0000-0000-000000000003","session_id":"test-hc061-zero","node_id":"some-node","outcome_status":"SUCCESS"}` + "\n"

	pr, pw := io.Pipe()

	go func() {
		_, _ = pw.Write([]byte(outcomeMsg))
		pw.Close()
	}()

	pub := &watcherFixturePublisher{}
	dl := &watcherFixtureDeadLetter{}

	w := handlercontract.SpawnWatcher(context.Background(), handlercontract.SpawnWatcherConfig{
		SessionID:      core.SessionID("test-hc061-zero"),
		ProgressStream: pr,
		Publisher:      pub,
		DeadLetter:     dl,
		// NodeType deliberately omitted (zero value) — backward-compat path.
	})

	select {
	case <-w.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("HC-061: watcher did not terminate within 5s")
	}

	found := false
	for _, et := range pub.EventTypes() {
		if et == handlercontract.ProgressMsgTypeOutcomeEmitted {
			found = true
		}
	}
	if !found {
		t.Errorf("HC-061: watcher did NOT forward outcome_emitted when NodeType is zero; published: %v — backward-compat must be active", pub.EventTypes())
	}
}
