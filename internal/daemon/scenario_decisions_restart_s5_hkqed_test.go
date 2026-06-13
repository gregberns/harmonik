//go:build scenario

package daemon

// scenario_decisions_restart_s5_hkqed_test.go — S5 restart-survivability
// scenario for the open-decision projection (hitl-decisions component K3, bead
// hk-qed).
//
// # What is tested (SPEC §8 S5)
//
// The open-decision queue is DURABLE: the projected open set is identical after
// a daemon/session restart, with resolved/withdrawn decisions removed (SPEC §3
// replay). Because decisionsProjection is a PURE fold over the durable
// events.jsonl log, restart-survivability is "free": a restarted daemon replays
// the same log on boot, so re-projecting from the on-disk file reconstructs the
// identical open set.
//
// # How the restart is simulated
//
// This mirrors the durable round-trip idiom used by the epic_completed restart
// scenario (epiccompleted_scenario_hktfxjp_test.go AC-5):
//
//  1. Emit several decision_needed events through a REAL eventbus busImpl +
//     JSONLWriter (exactly as the daemon does — F-class fsync to events.jsonl),
//     then Close the writer to flush. Read the minted decision_ids back from the
//     durable log (the decision_id IS the decision_needed event_id, SPEC §1).
//  2. Project the open set from the on-disk log → S5 "before restart" set.
//  3. SIMULATE RESTART: with no in-memory state carried over, re-run
//     decisionsProjection against the SAME on-disk events.jsonl (a fresh daemon
//     replays this exact log on boot). Assert the open set is byte-for-byte
//     identical to step 2.
//  4. Emit decision_resolved / decision_withdrawn for a subset through a fresh
//     real bus+writer appending to the same log; re-project; assert the
//     resolved/withdrawn decisions DROP OUT and the remainder is unchanged.
//
// This covers S5 — the coverage gap the independent reviewer flagged in the two
// gate beads (hk-rz4, hk-1vl), which do NOT cover restart durability. The K3
// task owns this scenario (07-tasks.md §B.2).
//
// # Why no full daemon.Start boot
//
// decisionsProjection is pure and on-demand (no aggregator process — SPEC C3 /
// S6). Restart-survivability is precisely that the open set is reconstructed
// from the durable log alone, across any process boundary. The faithful proof
// is the durable-emit → re-project round-trip above; booting daemon.Start (with
// a HandlerBinary, br, worktrees) would add nothing to the S5 assertion and is
// out of scope here.
//
// # Helper prefix
//
// Helpers use the prefix "ds5" (decisions-S5) per the helper-prefix discipline.
//
// Run independently (the daemon gate skips //go:build scenario):
//
//	go test -tags scenario -run TestScenario_DecisionsProjection_RestartS5 ./internal/daemon/...
//
// Spec ref: SPEC.md §3 (projection), §8 S5 (restart durability).
// Bead ref: hk-qed (K3).

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

// ds5EmitNeeded emits a single decision_needed event through a real bus+writer
// appending to jsonlPath (F-class fsync), then returns the minted decision_id
// (== the decision_needed event_id) read back from the durable log.
//
// It opens a fresh writer/bus per call and closes the writer to flush, so each
// call's line is durably on disk before the next read — modelling the daemon's
// per-emit durability boundary.
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

	// The new decision_id is the single open key that was not present before.
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

// ds5Emit emits one event of the given type+payload through a real eventbus
// busImpl + JSONLWriter appending to jsonlPath, then closes the writer to flush
// the line to disk — the same durable-write path the daemon uses.
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

// ds5OpenKeys returns the sorted decision_id keys of an open set.
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

	// ── Phase 1: emit several decision_needed through the real durable bus ──
	d1 := ds5EmitNeeded(t, ctx, jsonlPath, "Ship to prod?", []string{"yes", "no"}, "alice", "hk-aaa")
	d2 := ds5EmitNeeded(t, ctx, jsonlPath, "Pick region", []string{"us", "eu"}, "bob", "hk-bbb")
	d3 := ds5EmitNeeded(t, ctx, jsonlPath, "Approve spend", []string{"approve", "deny"}, "carol", "hk-ccc")
	d4 := ds5EmitNeeded(t, ctx, jsonlPath, "Rename field?", []string{"keep", "rename"}, "dave", "hk-ddd")

	allIDs := []string{d1, d2, d3, d4}
	sort.Strings(allIDs)

	// ── Phase 2: project the "before restart" open set ──
	beforeRestart := decisionsProjection(jsonlPath)
	beforeKeys := ds5OpenKeys(beforeRestart)
	if !reflect.DeepEqual(beforeKeys, allIDs) {
		t.Fatalf("before-restart open set = %v, want all four %v", beforeKeys, allIDs)
	}

	// ── Phase 3: SIMULATE RESTART — re-project from the SAME durable log with no
	// carried-over in-memory state. A restarted daemon replays this exact log. ──
	afterRestart := decisionsProjection(jsonlPath)
	afterKeys := ds5OpenKeys(afterRestart)
	if !reflect.DeepEqual(afterKeys, beforeKeys) {
		t.Fatalf("S5 VIOLATED: open set changed across restart: before=%v after=%v", beforeKeys, afterKeys)
	}
	// Deep-equal the full Decision values, not just the keys — restart must
	// preserve the rendered fields verbatim.
	if !reflect.DeepEqual(afterRestart, beforeRestart) {
		t.Fatalf("S5 VIOLATED: Decision values changed across restart:\n before=%+v\n after =%+v", beforeRestart, afterRestart)
	}

	// ── Phase 4: resolve d2, withdraw d3 (terminals land on the same log) ──
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

	// Re-project (and again simulate a restart by re-projecting from disk).
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

	// One more restart round-trip after the terminals: still {d1, d4}.
	postTerminalRestart := decisionsProjection(jsonlPath)
	if !reflect.DeepEqual(postTerminalRestart, postTerminal) {
		t.Fatalf("S5 VIOLATED post-terminal: open set changed across restart:\n before=%+v\n after =%+v", postTerminal, postTerminalRestart)
	}

	t.Logf("S5 PASS: before-restart=%v after-restart-identical post-terminal-open=%v", beforeKeys, gotRemaining)
}
