//go:build scenario

package keeper_test

// scenario_decisions_orphan_reap_s7_hk061_test.go — S7 orphan-reap + re-wait
// scenario for the hitl-decisions orphan reaper (component K5, bead hk-061).
//
// # What is tested (SPEC §8 S7)
//
// S7a (orphan reap, sole-emitter, Stale-not-reaped):
//   - emit decision_needed (agent "gone-x" blocked) + a SECOND decision_needed
//     (agent "stale-y" blocked) into a real events.jsonl;
//   - make "gone-x" OFFLINE (an explicit `leave` beat → presence.GetState
//     short-circuits to StateOffline) and "stale-y" merely STALE (an online beat
//     ~3min old: TTL=120s ≤ age < StaleCutoff=10m);
//   - run the REAL keeper watch tick (Watcher.Run with ReapDecisions=true) over
//     that log;
//   - assert the tick emitted decision_withdrawn(reason=orphaned, by="keeper")
//     for gone-x AND did NOT emit one for stale-y (Stale ≠ gone, N9);
//   - append the recorded withdrawal back to the log (modelling the FileEmitter)
//     and re-project: gone-x LEAVES the open set (no zombie), stale-y REMAINS.
//
// S7b (restart + re-wait):
//   - the orphaned decision (gone-x) is now withdrawn. Simulate the agent
//     restarting and re-deriving its open decisions from the projection: gone-x
//     is no longer open → the restarted agent is cleanly already-withdrawn (no
//     zombie, no double-apply). A SEPARATE still-open decision (a freshly-raised
//     "back-z") is answered → it leaves the open set with the chosen option, i.e.
//     a restarted agent that re-waits still resolves on the answer.
//
// # Why drive the real Watcher tick (not just call the reaper)
//
// K5's normative contract is that the KEEPER WATCH TICK is the sole emitter of
// orphaned withdrawals (SPEC §5 / N9), bounded by the tick cadence (not the 1h
// sweep). Driving Watcher.Run with a short PollInterval and ReapDecisions=true
// exercises that exact seam: the reaper fires from the ticker, unconditionally,
// before the gauge state machine — proving the emission is keeper-tick-resident
// and not coupled to this agent's own gauge.
//
// # Helper prefix
//
// Helpers use the prefix "ds7" (decisions-S7) per the helper-prefix discipline.
//
// Run independently (the daemon gate skips //go:build scenario):
//
//	go test -tags scenario -run TestScenario_DecisionsOrphanReap_S7 ./internal/keeper/...
//
// Spec ref: SPEC.md §5 (orphan reaper predicate + latency bound), §8 S7.
// Bead ref: hk-061 (K5).

import (
	"context"
	"encoding/json"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/eventbus"
	"github.com/gregberns/harmonik/internal/keeper"
	"github.com/gregberns/harmonik/internal/presence"
)

// ds7Emit emits one event of the given type+payload through a real eventbus
// busImpl + JSONLWriter appending to jsonlPath, then closes the writer to flush
// the line to disk — the same durable-write path the daemon/keeper FileEmitter
// uses. The event_id is bus-minted; callers that need the minted decision_id read
// it back from the projection (see ds7EmitNeeded).
func ds7Emit(t *testing.T, ctx context.Context, jsonlPath string, evType core.EventType, payload any) {
	t.Helper()
	writer, err := eventbus.OpenJSONLWriter(jsonlPath)
	if err != nil {
		t.Fatalf("ds7Emit: open JSONL writer: %v", err)
	}
	bus := eventbus.NewBusImplWithWriter(core.NewRedactionRegistry(), writer)
	plBytes, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("ds7Emit: marshal payload: %v", err)
	}
	if err := bus.Emit(ctx, evType, plBytes); err != nil {
		t.Fatalf("ds7Emit: emit %s: %v", evType, err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("ds7Emit: close writer: %v", err)
	}
}

// ds7EmitNeeded emits a decision_needed and returns the minted decision_id
// (== the decision_needed event_id, SPEC §1) read back from the durable log.
func ds7EmitNeeded(t *testing.T, ctx context.Context, jsonlPath, question string, options []string, blockedAgent string) string {
	t.Helper()
	before := ds7OpenKeys(presence.OpenDecisions(jsonlPath))
	ds7Emit(t, ctx, jsonlPath, core.EventTypeDecisionNeeded, core.DecisionNeededPayload{
		Question:     question,
		Options:      options,
		BlockedAgent: blockedAgent,
	})
	after := presence.OpenDecisions(jsonlPath)
	beforeSet := make(map[string]struct{}, len(before))
	for _, k := range before {
		beforeSet[k] = struct{}{}
	}
	var minted string
	for k, d := range after {
		if _, had := beforeSet[k]; had {
			continue
		}
		if d.BlockedAgent != blockedAgent {
			continue
		}
		if minted != "" {
			t.Fatalf("ds7EmitNeeded: more than one new decision for agent %q", blockedAgent)
		}
		minted = k
	}
	if minted == "" {
		t.Fatalf("ds7EmitNeeded: no new open decision appeared for agent %q", blockedAgent)
	}
	return minted
}

// ds7EmitPresence emits an agent_presence beat. status "offline" models a clean
// `leave` beat (→ presence.StateOffline); status "online" with an aged last_seen
// models the stale/online window depending on age.
func ds7EmitPresence(t *testing.T, ctx context.Context, jsonlPath, agent, status string, lastSeen time.Time, reason core.AgentPresenceReason) {
	t.Helper()
	// agent_presence has no exported EventType constant in internal/core (the
	// presence projection folds on the bare string); use the literal type here.
	ds7Emit(t, ctx, jsonlPath, core.EventType("agent_presence"), core.AgentPresencePayload{
		Agent:    agent,
		Status:   core.AgentPresenceStatus(status),
		LastSeen: lastSeen.UTC().Format(time.RFC3339),
		Reason:   reason,
	})
}

// ds7OpenKeys returns the sorted decision_id keys of an open set.
func ds7OpenKeys(open map[string]presence.Decision) []string {
	keys := make([]string, 0, len(open))
	for k := range open {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// ds7RunOneReapTick boots a real keeper Watcher with ReapDecisions=true and runs
// it just long enough to fire several ticks, then returns the decision_withdrawn
// events it recorded. The watcher uses SuppressNoGauge so the absent gauge does
// not pollute the recorder, and a RecordingEmitter so withdrawals are captured
// (not appended to the log) — the caller decides whether to replay them.
func ds7RunOneReapTick(t *testing.T, projectDir, eventsPath string) []keeper.EmittedEvent {
	t.Helper()
	em := &keeper.RecordingEmitter{}
	cfg := keeper.WatcherConfig{
		AgentName:       "reaper-keeper",
		ProjectDir:      projectDir,
		PollInterval:    10 * time.Millisecond,
		SuppressNoGauge: true,
		ReapDecisions:   true,
		EventsJSONLPath: eventsPath,
		DecisionEmitter: em,
	}
	w := keeper.NewWatcher(cfg, em)
	// Run ~150ms → ~15 ticks; the reaper fires on every tick. Idempotent, so
	// multiple fires for the same already-recorded decision do not change the
	// open set (the recorder just gets the same withdrawal again — we dedupe on
	// decision_id below).
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	_ = w.Run(ctx) //nolint:errcheck // context.DeadlineExceeded expected
	return em.EventsOfType(core.EventTypeDecisionWithdrawn)
}

// ds7WithdrawnFor decodes the recorded decision_withdrawn events and returns the
// set of decision_ids withdrawn with reason=orphaned by="keeper" (deduped).
func ds7WithdrawnFor(t *testing.T, events []keeper.EmittedEvent) map[string]bool {
	t.Helper()
	out := make(map[string]bool)
	for _, e := range events {
		var p core.DecisionWithdrawnPayload
		if err := json.Unmarshal(e.Payload, &p); err != nil {
			t.Fatalf("ds7WithdrawnFor: decode withdrawn payload: %v", err)
		}
		if p.Reason != core.DecisionWithdrawnReasonOrphaned {
			t.Errorf("withdrawn reason = %q, want orphaned (N9: keeper only emits orphaned)", p.Reason)
		}
		if p.By != "keeper" {
			t.Errorf("withdrawn by = %q, want keeper (N9 sole emitter)", p.By)
		}
		out[p.DecisionID] = true
	}
	return out
}

// TestScenario_DecisionsOrphanReap_S7 exercises S7a (orphan reap + sole emitter +
// Stale-not-reaped + no zombie) and S7b (restart re-wait / clean-already-withdrawn).
func TestScenario_DecisionsOrphanReap_S7(t *testing.T) {
	ctx := context.Background()
	projectDir := t.TempDir()
	eventsPath := filepath.Join(projectDir, "events.jsonl")

	// ── S7a setup: two blocked decisions, one gone agent, one stale agent ──
	goneDID := ds7EmitNeeded(t, ctx, eventsPath, "Ship gone-x?", []string{"yes", "no"}, "gone-x")
	staleDID := ds7EmitNeeded(t, ctx, eventsPath, "Ship stale-y?", []string{"yes", "no"}, "stale-y")

	// gone-x: explicit leave beat → presence.StateOffline (the N9 (a) clause).
	ds7EmitPresence(t, ctx, eventsPath, "gone-x", "offline", time.Now(), core.AgentPresenceReasonLeave)
	// stale-y: online beat ~3min old → TTL(120s) ≤ age < StaleCutoff(10m) = Stale.
	ds7EmitPresence(t, ctx, eventsPath, "stale-y", "online", time.Now().Add(-3*time.Minute), core.AgentPresenceReasonRefresh)

	// Sanity: both decisions are open before the reap; presence states as intended.
	openBefore := presence.OpenDecisions(eventsPath)
	if _, ok := openBefore[goneDID]; !ok {
		t.Fatalf("gone-x decision %s must be open before reap", goneDID)
	}
	if _, ok := openBefore[staleDID]; !ok {
		t.Fatalf("stale-y decision %s must be open before reap", staleDID)
	}
	reg := presence.ComputeRegistry(eventsPath)
	if got := presence.GetState(reg["gone-x"]); got != presence.StateOffline {
		t.Fatalf("gone-x presence = %v, want StateOffline (leave beat)", got)
	}
	if got := presence.GetState(reg["stale-y"]); got != presence.StateStale {
		t.Fatalf("stale-y presence = %v, want StateStale (3min-old online beat)", got)
	}

	// ── S7a: run the REAL keeper tick reaper ──
	withdrawn := ds7RunOneReapTick(t, projectDir, eventsPath)
	reapedSet := ds7WithdrawnFor(t, withdrawn)

	// The gone agent's decision MUST be withdrawn (orphaned, by=keeper).
	if !reapedSet[goneDID] {
		t.Fatalf("S7a VIOLATED: keeper tick did NOT withdraw the orphaned decision %s (gone-x)", goneDID)
	}
	// The Stale agent's decision MUST NOT be reaped (Stale ≠ gone, N9).
	if reapedSet[staleDID] {
		t.Fatalf("S7a VIOLATED: keeper tick wrongly reaped the STALE-but-alive decision %s (stale-y)", staleDID)
	}

	// Replay the orphaned withdrawal into the durable log (the real FileEmitter
	// appends it), then re-project: gone-x LEAVES the open set (no zombie),
	// stale-y REMAINS open.
	ds7Emit(t, ctx, eventsPath, core.EventTypeDecisionWithdrawn, core.DecisionWithdrawnPayload{
		DecisionID: goneDID,
		Reason:     core.DecisionWithdrawnReasonOrphaned,
		By:         "keeper",
	})
	openAfter := presence.OpenDecisions(eventsPath)
	if _, present := openAfter[goneDID]; present {
		t.Fatalf("S7a VIOLATED: orphaned decision %s (gone-x) is a ZOMBIE — still open after withdraw", goneDID)
	}
	if _, present := openAfter[staleDID]; !present {
		t.Fatalf("S7a VIOLATED: stale-y decision %s wrongly left the open set (must remain — not reaped)", staleDID)
	}

	// ── S7b: restart + re-wait ──
	// (1) The restarted gone-x agent re-derives its open decisions from the
	// projection. goneDID is no longer open → it is cleanly ALREADY-WITHDRAWN
	// (no double-apply, no zombie). Verify a second reap tick does NOT re-withdraw
	// it (idempotent — N3): the open set no longer contains it.
	withdrawn2 := ds7RunOneReapTick(t, projectDir, eventsPath)
	reapedSet2 := ds7WithdrawnFor(t, withdrawn2)
	if reapedSet2[goneDID] {
		t.Fatalf("S7b VIOLATED: second reap tick re-withdrew already-withdrawn %s (not idempotent)", goneDID)
	}

	// (2) A restarted agent that re-establishes the wait STILL resolves on the
	// answer: raise a fresh decision for a now-back agent, answer it, assert it
	// leaves the open set with the chosen option (models S7b's "wakes with
	// chosen_option" path via the projection's first-writer-wins fold).
	backDID := ds7EmitNeeded(t, ctx, eventsPath, "Ship back-z?", []string{"go", "stop"}, "back-z")
	// back-z is online (fresh) — it is NOT reaped by the tick (presence Online).
	ds7EmitPresence(t, ctx, eventsPath, "back-z", "online", time.Now(), core.AgentPresenceReasonJoin)
	withdrawn3 := ds7RunOneReapTick(t, projectDir, eventsPath)
	if ds7WithdrawnFor(t, withdrawn3)[backDID] {
		t.Fatalf("S7b VIOLATED: keeper tick wrongly reaped the live (Online) back-z decision %s", backDID)
	}
	// Answer back-z (the human resolves it) → it leaves the open set.
	ds7Emit(t, ctx, eventsPath, core.EventTypeDecisionResolved, core.DecisionResolvedPayload{
		DecisionID:   backDID,
		ChosenOption: "go",
		Resolver:     "operator",
	})
	openFinal := presence.OpenDecisions(eventsPath)
	if _, present := openFinal[backDID]; present {
		t.Fatalf("S7b VIOLATED: answered decision %s (back-z) is still open — answer did not resolve", backDID)
	}

	t.Logf("S7 PASS: orphaned %s reaped (orphaned,by=keeper); stale %s preserved; idempotent re-tick; back-z %s resolved on answer",
		goneDID, staleDID, backDID)
}
