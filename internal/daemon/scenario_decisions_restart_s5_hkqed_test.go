//go:build scenario

package daemon

import (
	"context"
	"encoding/json"
	"path/filepath"
	"reflect"
	"sort"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/eventbus"
)

func ds5EmitNeeded(t *testing.T, ctx context.Context, jsonlPath, question string, options []string, blockedAgent, contextLink string) string {
	t.Helper()
	payload := core.DecisionNeededPayload{
		Question:     question,
		Options:      options,
		BlockedAgent: blockedAgent,
		ContextLink:  contextLink,
	}
	if !payload.Valid() {
		t.Fatalf("ds5EmitNeeded: payload invalid: %+v", payload)
	}
	beforeIDs := ds5OpenKeys(decisionsProjection(jsonlPath))
	ds5Emit(t, ctx, jsonlPath, core.EventTypeDecisionNeeded, payload)

	afterIDs := ds5OpenKeys(decisionsProjection(jsonlPath))
	beforeSet := make(map[string]struct{}, len(beforeIDs))
	for _, id := range beforeIDs {
		beforeSet[id] = struct{}{}
	}
	var minted string
	for _, id := range afterIDs {
		if _, had := beforeSet[id]; !had {
			if minted != "" {
				t.Fatalf("ds5EmitNeeded: more than one new open decision_id after emit (before=%v after=%v)", beforeIDs, afterIDs)
			}
			minted = id
		}
	}
	if minted == "" {
		t.Fatalf("ds5EmitNeeded: no new decision_id appeared after emit (before=%v after=%v)", beforeIDs, afterIDs)
	}
	return minted
}

func ds5Emit(t *testing.T, ctx context.Context, jsonlPath string, evType core.EventType, payload any) {
	t.Helper()
	writer, err := eventbus.OpenJSONLWriter(jsonlPath)
	if err != nil {
		t.Fatalf("ds5Emit: open JSONL writer: %v", err)
	}
	bus := eventbus.NewBusImplWithWriter(core.NewRedactionRegistry(), writer)
	plBytes, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("ds5Emit: marshal payload: %v", err)
	}
	if err := bus.Emit(ctx, evType, plBytes); err != nil {
		t.Fatalf("ds5Emit: emit %s: %v", evType, err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("ds5Emit: close writer: %v", err)
	}
}

func ds5OpenKeys(open map[string]Decision) []string {
	keys := make([]string, 0, len(open))
	for k := range open {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// TestScenario_DecisionsProjection_RestartS5 exercises S5: the open-decision set
// is identical after a simulated daemon restart (re-projection over the durable
// log), and resolved/withdrawn decisions drop out after their terminals land.
func TestScenario_DecisionsProjection_RestartS5(t *testing.T) {
	ctx := context.Background()
	jsonlPath := filepath.Join(t.TempDir(), "events.jsonl")

	d1 := ds5EmitNeeded(t, ctx, jsonlPath, "Ship to prod?", []string{"yes", "no"}, "alice", "hk-aaa")
	d2 := ds5EmitNeeded(t, ctx, jsonlPath, "Pick region", []string{"us", "eu"}, "bob", "hk-bbb")
	d3 := ds5EmitNeeded(t, ctx, jsonlPath, "Approve spend", []string{"approve", "deny"}, "carol", "hk-ccc")
	d4 := ds5EmitNeeded(t, ctx, jsonlPath, "Rename field?", []string{"keep", "rename"}, "dave", "hk-ddd")

	allIDs := []string{d1, d2, d3, d4}
	sort.Strings(allIDs)

	beforeRestart := decisionsProjection(jsonlPath)
	beforeKeys := ds5OpenKeys(beforeRestart)
	if !reflect.DeepEqual(beforeKeys, allIDs) {
		t.Fatalf("before-restart open set = %v, want all four %v", beforeKeys, allIDs)
	}

	afterRestart := decisionsProjection(jsonlPath)
	afterKeys := ds5OpenKeys(afterRestart)
	if !reflect.DeepEqual(afterKeys, beforeKeys) {
		t.Fatalf("S5 VIOLATED: open set changed across restart: before=%v after=%v", beforeKeys, afterKeys)
	}
	if !reflect.DeepEqual(afterRestart, beforeRestart) {
		t.Fatalf("S5 VIOLATED: Decision values changed across restart:\n before=%+v\n after =%+v", beforeRestart, afterRestart)
	}

	ds5Emit(t, ctx, jsonlPath, core.EventTypeDecisionResolved, core.DecisionResolvedPayload{
		DecisionID:   d2,
		ChosenOption: "eu",
		Resolver:     "operator",
	})
	ds5Emit(t, ctx, jsonlPath, core.EventTypeDecisionWithdrawn, core.DecisionWithdrawnPayload{
		DecisionID: d3,
		Reason:     core.DecisionWithdrawnReasonSelfObsoleted,
		By:         "carol",
	})

	postTerminal := decisionsProjection(jsonlPath)
	wantRemaining := []string{d1, d4}
	sort.Strings(wantRemaining)
	gotRemaining := ds5OpenKeys(postTerminal)
	if !reflect.DeepEqual(gotRemaining, wantRemaining) {
		t.Fatalf("after resolve(d2)+withdraw(d3): open set = %v, want %v (resolved/withdrawn must drop out)", gotRemaining, wantRemaining)
	}
	if _, present := postTerminal[d2]; present {
		t.Errorf("d2 (resolved) must NOT remain open")
	}
	if _, present := postTerminal[d3]; present {
		t.Errorf("d3 (withdrawn) must NOT remain open")
	}

	postTerminalRestart := decisionsProjection(jsonlPath)
	if !reflect.DeepEqual(postTerminalRestart, postTerminal) {
		t.Fatalf("S5 VIOLATED post-terminal: open set changed across restart:\n before=%+v\n after =%+v", postTerminal, postTerminalRestart)
	}

	t.Logf("S5 PASS: before-restart=%v after-restart-identical post-terminal-open=%v", beforeKeys, gotRemaining)
}
