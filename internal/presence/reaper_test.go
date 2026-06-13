package presence_test

// reaper_test.go — unit tests for the hitl-decisions orphan reaper (component
// K5, bead hk-061) and the lifted open-decision projection. These are plain
// (non-scenario) unit tests: they build a synthetic events.jsonl on disk and
// assert the N9 predicate (Offline → reaped; Stale/Online/unknown → not reaped),
// idempotency (N3), and the sole-emitter "by=keeper" / reason=orphaned contract.

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/eventbus"
	"github.com/gregberns/harmonik/internal/presence"
)

// recEmitter records EmitWithRunID calls for assertions.
type recEmitter struct {
	calls []recCall
}

type recCall struct {
	typ     core.EventType
	payload []byte
}

func (r *recEmitter) EmitWithRunID(_ context.Context, _ core.RunID, t core.EventType, p []byte) error {
	cp := make([]byte, len(p))
	copy(cp, p)
	r.calls = append(r.calls, recCall{typ: t, payload: cp})
	return nil
}

// emit appends one event to jsonlPath through a real bus+writer (durable path).
func emit(t *testing.T, jsonlPath string, evType core.EventType, payload any) {
	t.Helper()
	writer, err := eventbus.OpenJSONLWriter(jsonlPath)
	if err != nil {
		t.Fatalf("emit: open writer: %v", err)
	}
	bus := eventbus.NewBusImplWithWriter(core.NewRedactionRegistry(), writer)
	b, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("emit: marshal: %v", err)
	}
	if err := bus.Emit(context.Background(), evType, b); err != nil {
		t.Fatalf("emit: %s: %v", evType, err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("emit: close: %v", err)
	}
}

// emitNeeded emits a decision_needed and returns the minted decision_id.
func emitNeeded(t *testing.T, jsonlPath, blockedAgent string) string {
	t.Helper()
	before := presence.OpenDecisions(jsonlPath)
	emit(t, jsonlPath, core.EventTypeDecisionNeeded, core.DecisionNeededPayload{
		Question:     "Q for " + blockedAgent,
		Options:      []string{"a", "b"},
		BlockedAgent: blockedAgent,
	})
	after := presence.OpenDecisions(jsonlPath)
	for k, d := range after {
		if _, had := before[k]; had {
			continue
		}
		if d.BlockedAgent == blockedAgent {
			return k
		}
	}
	t.Fatalf("emitNeeded: no new decision for %q", blockedAgent)
	return ""
}

func emitPresence(t *testing.T, jsonlPath, agent, status string, lastSeen time.Time) {
	t.Helper()
	emit(t, jsonlPath, core.EventType("agent_presence"), core.AgentPresencePayload{
		Agent:    agent,
		Status:   core.AgentPresenceStatus(status),
		LastSeen: lastSeen.UTC().Format(time.RFC3339),
	})
}

// TestReapOrphanedDecisions_N9Predicate asserts the core N9 predicate: only an
// Offline blocked_agent (leave beat OR age ≥ StaleCutoff) is reaped; Stale,
// Online, and unknown agents are NOT.
func TestReapOrphanedDecisions_N9Predicate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")

	goneLeave := emitNeeded(t, path, "gone-leave") // → leave beat (Offline)
	goneAged := emitNeeded(t, path, "gone-aged")   // → 11min-old online (Offline)
	staleA := emitNeeded(t, path, "stale-a")       // → 3min-old online (Stale)
	onlineA := emitNeeded(t, path, "online-a")     // → fresh online (Online)
	unknownA := emitNeeded(t, path, "unknown-a")   // → no presence record at all
	noAgent := emitNeededNoAgent(t, path)          // → empty blocked_agent

	emitPresence(t, path, "gone-leave", "offline", time.Now())                    // leave
	emitPresence(t, path, "gone-aged", "online", time.Now().Add(-11*time.Minute)) // > StaleCutoff
	emitPresence(t, path, "stale-a", "online", time.Now().Add(-3*time.Minute))    // Stale window
	emitPresence(t, path, "online-a", "online", time.Now())                       // Online

	em := &recEmitter{}
	res, err := presence.ReapOrphanedDecisions(context.Background(), path, em)
	if err != nil {
		t.Fatalf("ReapOrphanedDecisions: %v", err)
	}

	reaped := withdrawnIDs(t, em)
	mustReap := map[string]bool{goneLeave: true, goneAged: true}
	mustNotReap := []string{staleA, onlineA, unknownA, noAgent}

	for id := range mustReap {
		if !reaped[id] {
			t.Errorf("decision %s (Offline agent) MUST be reaped but was not", id)
		}
	}
	for _, id := range mustNotReap {
		if reaped[id] {
			t.Errorf("decision %s MUST NOT be reaped (Stale/Online/unknown/no-agent)", id)
		}
	}
	if res.Reaped != 2 {
		t.Errorf("res.Reaped = %d, want 2 (gone-leave + gone-aged)", res.Reaped)
	}
}

// emitNeededNoAgent emits a decision_needed with an empty blocked_agent.
func emitNeededNoAgent(t *testing.T, jsonlPath string) string {
	t.Helper()
	before := presence.OpenDecisions(jsonlPath)
	emit(t, jsonlPath, core.EventTypeDecisionNeeded, core.DecisionNeededPayload{
		Question: "no-agent",
		Options:  []string{"x"},
	})
	after := presence.OpenDecisions(jsonlPath)
	for k, d := range after {
		if _, had := before[k]; had {
			continue
		}
		if d.BlockedAgent == "" {
			return k
		}
	}
	t.Fatalf("emitNeededNoAgent: no new decision")
	return ""
}

// withdrawnIDs returns the set of decision_ids the emitter recorded, asserting
// each is reason=orphaned by=keeper (the N9 sole-emitter contract).
func withdrawnIDs(t *testing.T, em *recEmitter) map[string]bool {
	t.Helper()
	out := make(map[string]bool)
	for _, c := range em.calls {
		if c.typ != core.EventTypeDecisionWithdrawn {
			t.Errorf("reaper emitted unexpected event type %q (want only decision_withdrawn)", c.typ)
			continue
		}
		var p core.DecisionWithdrawnPayload
		if err := json.Unmarshal(c.payload, &p); err != nil {
			t.Fatalf("withdrawnIDs: decode: %v", err)
		}
		if p.Reason != core.DecisionWithdrawnReasonOrphaned {
			t.Errorf("reason = %q, want orphaned", p.Reason)
		}
		if p.By != "keeper" {
			t.Errorf("by = %q, want keeper", p.By)
		}
		out[p.DecisionID] = true
	}
	return out
}

// TestReapOrphanedDecisions_Idempotent asserts that re-reaping after the
// withdrawal is in the log is a no-op (the decision already left the open set —
// N3 answer-vs-reap race safety).
func TestReapOrphanedDecisions_Idempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	did := emitNeeded(t, path, "gone")
	emitPresence(t, path, "gone", "offline", time.Now())

	// First reap: withdraws once.
	em1 := &recEmitter{}
	if _, err := presence.ReapOrphanedDecisions(context.Background(), path, em1); err != nil {
		t.Fatalf("reap 1: %v", err)
	}
	if !withdrawnIDs(t, em1)[did] {
		t.Fatalf("first reap must withdraw %s", did)
	}

	// Replay the withdrawal into the log (the FileEmitter would), then reap again.
	emit(t, path, core.EventTypeDecisionWithdrawn, core.DecisionWithdrawnPayload{
		DecisionID: did, Reason: core.DecisionWithdrawnReasonOrphaned, By: "keeper",
	})
	em2 := &recEmitter{}
	res2, err := presence.ReapOrphanedDecisions(context.Background(), path, em2)
	if err != nil {
		t.Fatalf("reap 2: %v", err)
	}
	if res2.Reaped != 0 {
		t.Errorf("second reap re-withdrew %d decisions; want 0 (idempotent, %s already gone)", res2.Reaped, did)
	}
	if len(em2.calls) != 0 {
		t.Errorf("second reap emitted %d events; want 0", len(em2.calls))
	}
}

// TestReapOrphanedDecisions_AnswerRace asserts that a decision answered between
// the raise and the reap is NOT re-withdrawn (it already left the open set).
func TestReapOrphanedDecisions_AnswerRace(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	did := emitNeeded(t, path, "gone")
	emitPresence(t, path, "gone", "offline", time.Now())
	// Answer lands first.
	emit(t, path, core.EventTypeDecisionResolved, core.DecisionResolvedPayload{
		DecisionID: did, ChosenOption: "a", Resolver: "operator",
	})
	em := &recEmitter{}
	res, err := presence.ReapOrphanedDecisions(context.Background(), path, em)
	if err != nil {
		t.Fatalf("reap: %v", err)
	}
	if res.Reaped != 0 || len(em.calls) != 0 {
		t.Errorf("reaper withdrew an already-answered decision (reaped=%d emits=%d); want 0/0", res.Reaped, len(em.calls))
	}
}

// TestReapOrphanedDecisions_NilEmitter asserts a nil emitter errors without panic.
func TestReapOrphanedDecisions_NilEmitter(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	if _, err := presence.ReapOrphanedDecisions(context.Background(), path, nil); err == nil {
		t.Errorf("nil emitter must return an error")
	}
}

// TestReapOrphanedDecisions_EmptyLog asserts an empty/absent log is a clean no-op.
func TestReapOrphanedDecisions_EmptyLog(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	// no file written yet
	em := &recEmitter{}
	res, err := presence.ReapOrphanedDecisions(context.Background(), path, em)
	if err != nil {
		t.Fatalf("empty-log reap: %v", err)
	}
	if res.Open != 0 || res.Reaped != 0 || len(em.calls) != 0 {
		t.Errorf("empty-log reap = %+v, %d emits; want all zero", res, len(em.calls))
	}
	_ = os.Remove(path)
}
