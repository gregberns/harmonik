//go:build scenario

package keeper_test

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

func ds7EmitPresence(t *testing.T, ctx context.Context, jsonlPath, agent, status string, lastSeen time.Time, reason core.AgentPresenceReason) {
	t.Helper()
	ds7Emit(t, ctx, jsonlPath, core.EventType("agent_presence"), core.AgentPresencePayload{
		Agent:    agent,
		Status:   core.AgentPresenceStatus(status),
		LastSeen: lastSeen.UTC().Format(time.RFC3339),
		Reason:   reason,
	})
}

func ds7OpenKeys(open map[string]presence.Decision) []string {
	keys := make([]string, 0, len(open))
	for k := range open {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func ds7RunOneReapTick(t *testing.T, projectDir, eventsPath string) []keeper.EmittedEvent {
	t.Helper()
	em := &keeper.RecordingEmitter{}
	cfg := keeper.WatcherConfig{
		AgentName:            "reaper-keeper",
		ProjectDir:           projectDir,
		PollInterval:         10 * time.Millisecond,
		ReapDecisionsCadence: 10 * time.Millisecond, // fire on every tick (match PollInterval)
		SuppressNoGauge:      true,
		ReapDecisions:        true,
		EventsJSONLPath:      eventsPath,
		DecisionEmitter:      em,
	}
	w := keeper.NewWatcher(cfg, em)
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	_ = w.Run(ctx) //nolint:errcheck // context.DeadlineExceeded expected
	return em.EventsOfType(core.EventTypeDecisionWithdrawn)
}

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

	goneDID := ds7EmitNeeded(t, ctx, eventsPath, "Ship gone-x?", []string{"yes", "no"}, "gone-x")
	staleDID := ds7EmitNeeded(t, ctx, eventsPath, "Ship stale-y?", []string{"yes", "no"}, "stale-y")

	ds7EmitPresence(t, ctx, eventsPath, "gone-x", "offline", time.Now(), core.AgentPresenceReasonLeave)
	ds7EmitPresence(t, ctx, eventsPath, "stale-y", "online", time.Now().Add(-3*time.Minute), core.AgentPresenceReasonRefresh)

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

	withdrawn := ds7RunOneReapTick(t, projectDir, eventsPath)
	reapedSet := ds7WithdrawnFor(t, withdrawn)

	if !reapedSet[goneDID] {
		t.Fatalf("S7a VIOLATED: keeper tick did NOT withdraw the orphaned decision %s (gone-x)", goneDID)
	}
	if reapedSet[staleDID] {
		t.Fatalf("S7a VIOLATED: keeper tick wrongly reaped the STALE-but-alive decision %s (stale-y)", staleDID)
	}

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

	withdrawn2 := ds7RunOneReapTick(t, projectDir, eventsPath)
	reapedSet2 := ds7WithdrawnFor(t, withdrawn2)
	if reapedSet2[goneDID] {
		t.Fatalf("S7b VIOLATED: second reap tick re-withdrew already-withdrawn %s (not idempotent)", goneDID)
	}

	backDID := ds7EmitNeeded(t, ctx, eventsPath, "Ship back-z?", []string{"go", "stop"}, "back-z")
	ds7EmitPresence(t, ctx, eventsPath, "back-z", "online", time.Now(), core.AgentPresenceReasonJoin)
	withdrawn3 := ds7RunOneReapTick(t, projectDir, eventsPath)
	if ds7WithdrawnFor(t, withdrawn3)[backDID] {
		t.Fatalf("S7b VIOLATED: keeper tick wrongly reaped the live (Online) back-z decision %s", backDID)
	}
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
