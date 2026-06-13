package keeper_test

// watcher_decision_exempt_test.go — hitl-decisions K6 (bead hk-50f): the
// session-keeper's 120s-silent-hang reaper (the maybeRespawn / RespawnCmd path)
// exempts an agent that is the blocked_agent of an OPEN decision and is
// legitimately waiting (a fresh §4 heartbeat keeps it presence-Online). Such an
// agent is BLOCKED, not HUNG — the reap is skipped.
//
// K6 is the complement of K5: K6 protects the LIVE blocked agent; K5 reaps the
// DECISION once the agent is genuinely gone. K6 is READ-ONLY — it consults the
// K3 open-decision projection (presence.OpenDecisions) + the presence registry
// (presence.ComputeRegistry) and emits NOTHING.
//
// The observable in these tests is session_keeper_respawn_attempted: the reaper
// EMITS that event when it kills/respawns; an EXEMPT agent produces ZERO such
// events even though every other respawn gate (gauge absent ≥ RespawnGrace,
// pane idle, cooldown) passes.
//
// Helper prefix: "k6" (decisions K6) per the helper-prefix discipline.
//
// Spec ref: SPEC.md §4 (keeper-alive via heartbeat; the optional .decision_waiting
// gate — NOT used here, the projection lookup is the primary mechanism), §5
// (keeper seam K6), §6 N5 / Risk R2. Bead ref: hk-50f (K6).

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/eventbus"
	"github.com/gregberns/harmonik/internal/keeper"
	"github.com/gregberns/harmonik/internal/presence"
)

// k6EventsPath returns the events.jsonl path the watcher derives for projectDir
// (the same default applyDefaults computes for WatcherConfig.EventsJSONLPath).
func k6EventsPath(projectDir string) string {
	return filepath.Join(projectDir, ".harmonik", core.EventsJSONLPath)
}

// k6Emit emits one event of evType+payload through a real eventbus busImpl +
// JSONLWriter appending to the project's events.jsonl, flushing to disk — the
// same durable-write path the daemon/keeper FileEmitter uses. The event_id is
// bus-minted.
func k6Emit(t *testing.T, ctx context.Context, projectDir string, evType core.EventType, payload any) {
	t.Helper()
	path := k6EventsPath(projectDir)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("k6Emit: mkdir events dir: %v", err)
	}
	writer, err := eventbus.OpenJSONLWriter(path)
	if err != nil {
		t.Fatalf("k6Emit: open JSONL writer: %v", err)
	}
	bus := eventbus.NewBusImplWithWriter(core.NewRedactionRegistry(), writer)
	plBytes, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("k6Emit: marshal payload: %v", err)
	}
	if err := bus.Emit(ctx, evType, plBytes); err != nil {
		t.Fatalf("k6Emit: emit %s: %v", evType, err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("k6Emit: close writer: %v", err)
	}
}

// k6EmitNeeded emits a decision_needed for blockedAgent.
func k6EmitNeeded(t *testing.T, ctx context.Context, projectDir, blockedAgent string) {
	t.Helper()
	k6Emit(t, ctx, projectDir, core.EventTypeDecisionNeeded, core.DecisionNeededPayload{
		Question:     "ship it?",
		Options:      []string{"yes", "no"},
		BlockedAgent: blockedAgent,
	})
}

// k6EmitPresence emits an agent_presence beat for agent with the given status,
// dated lastSeen (controls the presence Online/Stale/Offline classification).
func k6EmitPresence(t *testing.T, ctx context.Context, projectDir, agent string, status core.AgentPresenceStatus, lastSeen time.Time, reason core.AgentPresenceReason) {
	t.Helper()
	k6Emit(t, ctx, projectDir, "agent_presence", core.AgentPresencePayload{
		Agent:    agent,
		Status:   status,
		LastSeen: lastSeen.UTC().Format(time.RFC3339),
		Reason:   reason,
	})
}

// k6RespawnConfig builds a WatcherConfig whose respawn gates ALL pass (gauge
// immediately absent ≥ RespawnGrace, pane always idle) so the ONLY thing that
// can suppress a respawn is the K6 exemption. The events.jsonl path is left to
// applyDefaults (derived from ProjectDir).
func k6RespawnConfig(projectDir, agent string) keeper.WatcherConfig {
	return keeper.WatcherConfig{
		AgentName:    agent,
		ProjectDir:   projectDir,
		PollInterval: 10 * time.Millisecond,
		Staleness:    5 * time.Millisecond, // gauge immediately stale/absent
		RespawnGrace: 5 * time.Millisecond,
		RespawnCmd:   "true",
		TmuxTarget:   "dummy-pane",
		IsPaneIdleFn: func(_ context.Context, _ string) bool { return true }, // pane idle
		InjectFn:     func(_ context.Context, _ string) error { return nil },
		// Deliberately NO gauge file written → gauge absent → respawn path engages.
	}
}

func k6RespawnEvents(em *keeper.RecordingEmitter) []keeper.EmittedEvent {
	return em.EventsOfType(core.EventTypeSessionKeeperRespawnAttempted)
}

// TestWatcher_K6_ExemptsBlockedAgentWithFreshHeartbeat verifies the core K6
// exemption: an agent that is the blocked_agent of an OPEN decision AND has a
// fresh (presence-Online) heartbeat is NOT reaped by the 120s-silent-hang
// respawn path. Asserts ZERO session_keeper_respawn_attempted events.
func TestWatcher_K6_ExemptsBlockedAgentWithFreshHeartbeat(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	projectDir := t.TempDir()
	agent := "blocked-fresh-agent"

	// Open decision for this agent + a fresh presence beat (now → Online).
	k6EmitNeeded(t, ctx, projectDir, agent)
	k6EmitPresence(t, ctx, projectDir, agent, core.AgentPresenceStatusOnline, time.Now(), core.AgentPresenceReasonRefresh)

	// Sanity: the projection sees the open decision and the agent is Online.
	open := presence.OpenDecisions(k6EventsPath(projectDir))
	foundBlocked := false
	for _, d := range open {
		if d.BlockedAgent == agent {
			foundBlocked = true
		}
	}
	if !foundBlocked {
		t.Fatalf("setup: expected an open decision blocking %q; open=%v", agent, open)
	}
	if rec, ok := presence.ComputeRegistry(k6EventsPath(projectDir))[agent]; !ok || presence.GetState(rec) != presence.StateOnline {
		t.Fatalf("setup: expected %q presence Online; ok=%v rec=%+v", agent, ok, rec)
	}

	em := &keeper.RecordingEmitter{}
	runWatcherFor(ctx, k6RespawnConfig(projectDir, agent), em, 200*time.Millisecond)

	if events := k6RespawnEvents(em); len(events) != 0 {
		t.Errorf("K6: blocked-on-decision agent with fresh heartbeat must be EXEMPT from the 120s reaper; "+
			"want 0 session_keeper_respawn_attempted, got %d", len(events))
	}
}

// TestWatcher_K6_ReapsAgentNotInAnyOpenDecision verifies no over-exemption: an
// agent that is NOT the blocked_agent of any open decision is reaped normally by
// the 120s respawn path (one respawn attempt fires). This is the control that
// proves the exemption is targeted, not blanket.
func TestWatcher_K6_ReapsAgentNotInAnyOpenDecision(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	projectDir := t.TempDir()
	agent := "unblocked-agent"

	// An open decision exists, but it blocks a DIFFERENT agent — so `agent` is
	// not exempt. A fresh presence beat for `agent` must NOT shield it (presence
	// alone is not the exemption — the open-decision membership is required).
	k6EmitNeeded(t, ctx, projectDir, "someone-else")
	k6EmitPresence(t, ctx, projectDir, agent, core.AgentPresenceStatusOnline, time.Now(), core.AgentPresenceReasonRefresh)

	em := &keeper.RecordingEmitter{}
	runWatcherFor(ctx, k6RespawnConfig(projectDir, agent), em, 200*time.Millisecond)

	if events := k6RespawnEvents(em); len(events) == 0 {
		t.Errorf("K6 over-exemption guard: an agent NOT in any open decision must be reaped normally; " +
			"want ≥1 session_keeper_respawn_attempted, got 0")
	}
}

// TestWatcher_K6_ReapsAgentWithEmptyOpenSet verifies the baseline: with NO open
// decisions at all, the respawn path behaves exactly as before (the agent is
// reaped). Guards against the exemption silently shielding every agent when the
// projection is empty.
func TestWatcher_K6_ReapsAgentWithEmptyOpenSet(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	projectDir := t.TempDir()
	agent := "no-decisions-agent"

	// Make .harmonik/events dir exist but emit NO decisions.
	if err := os.MkdirAll(filepath.Dir(k6EventsPath(projectDir)), 0o755); err != nil {
		t.Fatalf("mkdir events dir: %v", err)
	}

	em := &keeper.RecordingEmitter{}
	runWatcherFor(ctx, k6RespawnConfig(projectDir, agent), em, 200*time.Millisecond)

	if events := k6RespawnEvents(em); len(events) == 0 {
		t.Errorf("K6 baseline: with no open decisions the 120s reaper must run normally; " +
			"want ≥1 session_keeper_respawn_attempted, got 0")
	}
}

// TestWatcher_K6_ReapsBlockedAgentWithStaleHeartbeat verifies the fresh-heartbeat
// qualifier (SPEC §4/§5): an agent that IS the blocked_agent of an open decision
// but whose heartbeat is genuinely STALE (TTL ≤ age < StaleCutoff) is NOT
// exempted — K6 does not indefinitely shield a quiet/dead agent. (Reaping the
// agent here is consistent with K5 owning the decision-withdrawal once the agent
// is gone; K6 only protects the LIVE, Online-heartbeat blocked agent.)
func TestWatcher_K6_ReapsBlockedAgentWithStaleHeartbeat(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	projectDir := t.TempDir()
	agent := "blocked-stale-agent"

	// Open decision for this agent, but the presence beat is ~3min old:
	// TTL=120s ≤ 3min < StaleCutoff=10min → presence-Stale, NOT Online.
	k6EmitNeeded(t, ctx, projectDir, agent)
	k6EmitPresence(t, ctx, projectDir, agent, core.AgentPresenceStatusOnline,
		time.Now().Add(-3*time.Minute), core.AgentPresenceReasonRefresh)

	// Sanity: presence is Stale (the fresh-heartbeat qualifier must reject it).
	if rec, ok := presence.ComputeRegistry(k6EventsPath(projectDir))[agent]; !ok || presence.GetState(rec) != presence.StateStale {
		t.Fatalf("setup: expected %q presence Stale; ok=%v state=%v", agent, ok, presence.GetState(rec))
	}

	em := &keeper.RecordingEmitter{}
	runWatcherFor(ctx, k6RespawnConfig(projectDir, agent), em, 200*time.Millisecond)

	if events := k6RespawnEvents(em); len(events) == 0 {
		t.Errorf("K6 fresh-heartbeat qualifier: a blocked agent with a STALE (non-Online) heartbeat must NOT be " +
			"exempted (no over-shielding); want ≥1 session_keeper_respawn_attempted, got 0")
	}
}

// TestWatcher_K6_ReapsBlockedAgentWithLeaveBeat verifies that a blocked agent
// that has emitted an explicit `leave` beat (presence Offline) is NOT exempted —
// it is genuinely gone (the K5 reaper will withdraw its decision), so the
// respawn path is free to fire.
func TestWatcher_K6_ReapsBlockedAgentWithLeaveBeat(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	projectDir := t.TempDir()
	agent := "blocked-left-agent"

	k6EmitNeeded(t, ctx, projectDir, agent)
	// A fresh online beat FOLLOWED by a leave beat → presence Offline (the leave
	// short-circuits GetState to StateOffline regardless of recency).
	k6EmitPresence(t, ctx, projectDir, agent, core.AgentPresenceStatusOnline, time.Now(), core.AgentPresenceReasonRefresh)
	k6EmitPresence(t, ctx, projectDir, agent, core.AgentPresenceStatusOffline, time.Now(), core.AgentPresenceReasonLeave)

	if rec, ok := presence.ComputeRegistry(k6EventsPath(projectDir))[agent]; !ok || presence.GetState(rec) != presence.StateOffline {
		t.Fatalf("setup: expected %q presence Offline after leave beat; ok=%v state=%v", agent, ok, presence.GetState(rec))
	}

	em := &keeper.RecordingEmitter{}
	runWatcherFor(ctx, k6RespawnConfig(projectDir, agent), em, 200*time.Millisecond)

	if events := k6RespawnEvents(em); len(events) == 0 {
		t.Errorf("K6: a blocked agent that has `leave`d (presence Offline) is gone, not blocked — must NOT be " +
			"exempted; want ≥1 session_keeper_respawn_attempted, got 0")
	}
}
