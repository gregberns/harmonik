//go:build scenario

package main

// scenario_decisions_list_1vl_test.go — the hitl-decisions `decisions list`
// what-needs-me EXPLORATORY scenario (component K-gate, bead hk-1vl): the
// OPERATOR-facing cross-agent decision queue (the read-side of K4 + the N9
// orphaned-pending flag), exercised against the REAL list path.
//
// # What is tested (SPEC §8 S2, S6 + §6 N9 read-side)
//
//	S2 (cross-agent what-needs-me queue):
//	  ≥2 DISTINCT blocked agents each RAISE a decision_needed; a SINGLE
//	  `decisions list` renders BOTH, each with all FIVE fields —
//	  question · options · blocked_agent · context_link · decision_id.
//
//	S6 (no aggregator):
//	  The list is a PURE on-demand projection. This rig runs NO persistent
//	  aggregator process — only a socket listener over the decisions handler that
//	  folds the durable log on each call — and the list still renders correctly.
//	  (The projection is a fold over events.jsonl: presence.OpenDecisions.)
//
//	N9 (orphaned-pending flag is READ-PURE — display only, NO emit):
//	  A decision whose blocked_agent is OFFLINE (here: an explicit "offline"
//	  agent_presence leave beat — GetState short-circuits to StateOffline) is
//	  FLAGGED "orphaned-pending" in the list output. The flag is DISPLAY-ONLY:
//	  the list call emits NOTHING. We assert this two ways — (a) the durable
//	  events.jsonl is byte-for-byte UNCHANGED across the list call, and (b) the
//	  event COUNT in the log is unchanged. A merely-STALE blocked_agent (in the
//	  120s..10m window) is NOT flagged.
//
//	--json shape: the machine-readable rows carry the five fields plus the
//	  orphaned_pending boolean, and the Offline agent's row has it set.
//
// # Approach — the REAL operator list path, end to end
//
// This exercises the ACTUAL operator surface, not a trivial projection check:
//
//   - The raise is the REAL daemon handler HandleDecisionsRaise on the REAL
//     *commsSendHandlerImpl, emitting through a REAL eventbus busImpl that fsyncs
//     decision_needed to events.jsonl (same path production uses).
//   - The list is driven through the REAL CLI entry point
//     runDecisionsListOrShowParsed (package main) with --socket/--project pointed
//     at the rig. That dials the REAL daemon socket → REAL HandleDecisionsList
//     (folds presence.OpenDecisions) → REAL flagOrphanedPending (computes the N9
//     Offline flag from the SAME events.jsonl via presence.ComputeRegistry) →
//     REAL renderDecisionRows / JSON marshal. Nothing about the list/flag/render
//     is reimplemented or stubbed — we assert the genuine operator-facing output.
//   - Offline is induced the production way: an "offline" agent_presence beat for
//     the blocked agent (the explicit-leave path of presence.GetState). No clock
//     manipulation is needed — a leave beat short-circuits to StateOffline.
//
// We do NOT boot daemon.Start (HandlerBinary, br, worktrees) — irrelevant to the
// list read path. We DO use every real component on the list data path.
//
// # Helper prefix
//
// Helpers use the prefix "v1l" per the helper-prefix discipline.
//
// Run independently (the daemon gate skips //go:build scenario):
//
//	go test -tags scenario -run TestScenario_DecisionsList_1VL ./cmd/harmonik/...
//
// Spec ref: SPEC.md §2 (the five fields), §3 (pure projection), §5/§6 N9 (Offline
// flag, read-pure), §8 S2/S6.
// Bead ref: hk-1vl (K-gate exploratory).

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/eventbus"
)

// v1lRecvDepsSetter / v1lDecisionsRaiser mirror the rz4 type-asserted handler
// interfaces: package main cannot name the unexported *commsSendHandlerImpl, but
// the concrete type satisfies these local interfaces, so we can wire the
// events-JSONL path and drive the real raise handler directly.
type v1lRecvDepsSetter interface {
	SetRecvDeps(pollStore, liveStore *daemon.CursorStore, eventsJSONLPath string)
}
type v1lDecisionsRaiser interface {
	HandleDecisionsRaise(ctx context.Context, payload json.RawMessage) (json.RawMessage, error)
}

// v1lRig is the real-component rig for the list scenario: a real bus + the real
// decisions handler over a real daemon socket, writing to a real events.jsonl.
// (No SubscribeHub is needed — the list path never arms a stream — but the
// listener still requires its hub arg, so we pass a real one for completeness.)
type v1lRig struct {
	bus        eventbus.EventBus
	handler    daemon.CommsSendHandler
	sockPath   string
	eventsPath string
	absProject string
}

// v1lMustShortTempDir returns a short-path temp dir under /tmp so the unix socket
// path stays under the 104-char macOS sockaddr_un limit.
func v1lMustShortTempDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "1vl-")
	if err != nil {
		t.Fatalf("v1lMustShortTempDir: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

// v1lStartRig wires a real durable bus, a SubscribeHub subscribed to it (for the
// listener signature), and the real decisions handler over a real daemon socket
// listener, then waits for the socket to bind. NO persistent aggregator process
// is started — the list op folds the log on demand (S6).
func v1lStartRig(t *testing.T) *v1lRig {
	t.Helper()
	dir := v1lMustShortTempDir(t)
	if err := os.MkdirAll(filepath.Join(dir, ".harmonik", "events"), 0o755); err != nil {
		t.Fatalf("v1lStartRig: mkdir: %v", err)
	}
	sockPath := filepath.Join(dir, ".harmonik", "daemon.sock")
	eventsPath := filepath.Join(dir, ".harmonik", "events", "events.jsonl")

	// Real durable bus → real events.jsonl (decision_needed is F-class fsync, the
	// same path production uses).
	writer, err := eventbus.OpenJSONLWriter(eventsPath)
	if err != nil {
		t.Fatalf("v1lStartRig: open JSONL writer: %v", err)
	}
	bus := eventbus.NewBusImplWithWriter(core.NewRedactionRegistry(), writer)

	hub := daemon.NewSubscribeHub(daemon.SubscribeHubConfig{
		Bus:             bus,
		EventsJSONLPath: eventsPath,
	})
	if err := hub.Subscribe(bus); err != nil {
		t.Fatalf("v1lStartRig: hub.Subscribe: %v", err)
	}
	if err := bus.Seal(); err != nil {
		t.Fatalf("v1lStartRig: bus.Seal: %v", err)
	}

	// Real decisions handler (K2 raise + K4 list) on the real bus. SetRecvDeps
	// wires the events-JSONL path the list handler projects over (same call the
	// daemon wiring makes).
	handler := daemon.NewCommsSendHandler(bus)
	if handler == nil {
		t.Fatalf("v1lStartRig: NewCommsSendHandler returned nil (bus must satisfy CommsMessageEmitter)")
	}
	depSetter, ok := handler.(v1lRecvDepsSetter)
	if !ok {
		t.Fatalf("v1lStartRig: handler does not expose SetRecvDeps")
	}
	v1lCursorStore := daemon.NewCursorStore(filepath.Join(dir, "cursors"))
	depSetter.SetRecvDeps(v1lCursorStore, v1lCursorStore, eventsPath)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = daemon.RunSocketListenerFull(ctx, sockPath, nil, nil, hub, nil, handler)
	}()
	t.Cleanup(func() {
		cancel()
		<-done
	})

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, statErr := os.Stat(sockPath); statErr == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if _, statErr := os.Stat(sockPath); statErr != nil {
		t.Fatalf("v1lStartRig: socket %s did not bind within 5s", sockPath)
	}

	// absProject = <dir> from <dir>/.harmonik/events/events.jsonl (the CLI derives
	// <absProject>/.harmonik/events/events.jsonl for the presence projection).
	absProject := filepath.Dir(filepath.Dir(filepath.Dir(eventsPath)))
	return &v1lRig{bus: bus, handler: handler, sockPath: sockPath, eventsPath: eventsPath, absProject: absProject}
}

// v1lRaise drives the REAL HandleDecisionsRaise handler and returns the minted
// decision_id (the decision_needed event's own event_id, SPEC §1).
func v1lRaise(t *testing.T, rig *v1lRig, question string, options []string, blockedAgent, contextLink string) string {
	t.Helper()
	raiser, ok := rig.handler.(v1lDecisionsRaiser)
	if !ok {
		t.Fatalf("v1lRaise: handler does not implement HandleDecisionsRaise")
	}
	reqBytes, _ := json.Marshal(daemon.DecisionsRaiseRequest{
		Question:     question,
		Options:      options,
		BlockedAgent: blockedAgent,
		ContextLink:  contextLink,
	})
	resBytes, err := raiser.HandleDecisionsRaise(context.Background(), reqBytes)
	if err != nil {
		t.Fatalf("v1lRaise: HandleDecisionsRaise: %v", err)
	}
	var res daemon.DecisionsRaiseResult
	if jerr := json.Unmarshal(resBytes, &res); jerr != nil {
		t.Fatalf("v1lRaise: decode raise result: %v", jerr)
	}
	if res.DecisionID == "" {
		t.Fatalf("v1lRaise: empty decision_id from raise")
	}
	return res.DecisionID
}

// v1lEmitPresence emits a real agent_presence beat for agent through the SAME bus
// (→ same events.jsonl), so the CLI's presence projection (presence.ComputeRegistry,
// which folds "agent_presence" events) sees it. status "offline" + reason "leave"
// is the explicit-leave path that GetState short-circuits to StateOffline — no
// clock manipulation required to make an agent Offline for the orphaned-pending
// flag.
func v1lEmitPresence(t *testing.T, rig *v1lRig, agent string, status core.AgentPresenceStatus, reason core.AgentPresenceReason) {
	t.Helper()
	p := core.AgentPresencePayload{
		Agent:    agent,
		Status:   status,
		LastSeen: time.Now().UTC().Format(time.RFC3339),
		Reason:   reason,
	}
	if !p.Valid() {
		t.Fatalf("v1lEmitPresence: payload for %s is invalid", agent)
	}
	payloadBytes, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("v1lEmitPresence: marshal: %v", err)
	}
	// Emit (not EmitTyped) is the EventBus-interface method that appends the event
	// to the SAME durable events.jsonl via the same redaction+append path; the
	// presence projection folds the resulting "agent_presence" record. There is no
	// named EventType constant for agent_presence, so we pass the literal type
	// string (presence.ComputeRegistry switches on the same string).
	if err := rig.bus.Emit(context.Background(), core.EventType("agent_presence"), payloadBytes); err != nil {
		t.Fatalf("v1lEmitPresence: Emit(agent_presence) for %s: %v", agent, err)
	}
}

// v1lRunListCapturingStdout runs the REAL CLI list entry point
// (runDecisionsListOrShowParsed) with os.Stdout redirected to a pipe, and returns
// the exit code and captured stdout. jsonFlag toggles text vs --json output.
// This dials the rig's REAL daemon socket and runs the REAL list+flag+render path.
func v1lRunListCapturingStdout(t *testing.T, rig *v1lRig, jsonFlag bool) (int, string) {
	t.Helper()
	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("v1lRunListCapturingStdout: pipe: %v", err)
	}
	os.Stdout = w

	outCh := make(chan string, 1)
	go func() {
		var b strings.Builder
		buf := make([]byte, 4096)
		for {
			nr, rerr := r.Read(buf)
			if nr > 0 {
				b.Write(buf[:nr])
			}
			if rerr != nil {
				break
			}
		}
		_ = r.Close()
		outCh <- b.String()
	}()

	// filterID="" → full list; verb="list". socketFlag pins the rig socket;
	// projectFlag pins absProject so flagOrphanedPending reads the rig's log.
	code := runDecisionsListOrShowParsed("", jsonFlag, rig.sockPath, rig.absProject, "list")

	_ = w.Close()
	os.Stdout = oldStdout
	return code, <-outCh
}

// v1lReadLog returns the raw bytes of events.jsonl (for byte-for-byte read-purity
// assertions) and the count of event lines (for an event-count assertion).
func v1lReadLog(t *testing.T, eventsPath string) ([]byte, int) {
	t.Helper()
	raw, err := os.ReadFile(eventsPath)
	if err != nil {
		t.Fatalf("v1lReadLog: read %s: %v", eventsPath, err)
	}
	var zeroID core.EventID
	n := 0
	for range eventbus.ScanAfter(eventsPath, zeroID) {
		n++
	}
	return raw, n
}

// v1lRowFor returns the JSON row for decisionID from a --json list output, or
// fails. The --json output is an array of {decision_id,question,options,
// blocked_agent,context_link,orphaned_pending,...}.
func v1lRowFor(t *testing.T, jsonOut, decisionID string) map[string]any {
	t.Helper()
	var rows []map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(jsonOut)), &rows); err != nil {
		t.Fatalf("v1lRowFor: unmarshal --json output: %v\nraw: %s", err, jsonOut)
	}
	for _, row := range rows {
		if row["decision_id"] == decisionID {
			return row
		}
	}
	t.Fatalf("v1lRowFor: no --json row for decision_id %q in:\n%s", decisionID, jsonOut)
	return nil
}

// TestScenario_DecisionsList_1VL exercises the operator `decisions list` surface:
// the cross-agent what-needs-me queue (S2), the no-aggregator pure projection
// (S6), and the read-pure orphaned-pending Offline flag (N9), through the REAL
// CLI list path (dial → HandleDecisionsList → flagOrphanedPending → render).
func TestScenario_DecisionsList_1VL(t *testing.T) {
	rig := v1lStartRig(t)

	// ──────────────────────────────────────────────────────────────────────────
	// Part A — S2: ≥2 DISTINCT blocked agents render in ONE list, all five fields.
	// ──────────────────────────────────────────────────────────────────────────

	// Two distinct blocked agents (alice, bob) each raise a decision_needed.
	aliceDID := v1lRaise(t, rig, "Ship v2 to prod?", []string{"ship", "hold"}, "alice", "hk-alice-ctx")
	bobDID := v1lRaise(t, rig, "Pick deploy region?", []string{"us", "eu"}, "bob", "hk-bob-ctx")

	// A third decision blocked on carol — carol will be made OFFLINE below for the
	// N9 orphaned-pending assertion (Part C).
	carolDID := v1lRaise(t, rig, "Roll back migration?", []string{"yes", "no"}, "carol", "hk-carol-ctx")

	// A SINGLE list call must render ALL THREE open decisions (cross-agent queue).
	code, out := v1lRunListCapturingStdout(t, rig, false /*text*/)
	if code != 0 {
		t.Fatalf("Part A: list exit = %d, want 0\nstdout:\n%s", code, out)
	}

	// Each decision must render with ALL FIVE fields:
	// question · options · blocked_agent · context_link · decision_id.
	type fieldCase struct {
		did, question, options, blocked, ctx string
	}
	cases := []fieldCase{
		{aliceDID, "Ship v2 to prod?", "ship|hold", "alice", "hk-alice-ctx"},
		{bobDID, "Pick deploy region?", "us|eu", "bob", "hk-bob-ctx"},
		{carolDID, "Roll back migration?", "yes|no", "carol", "hk-carol-ctx"},
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	for _, c := range cases {
		var row string
		for _, ln := range lines {
			if strings.Contains(ln, c.did) {
				row = ln
				break
			}
		}
		if row == "" {
			t.Fatalf("S2 VIOLATED: decision %s (blocked_agent %s) did NOT appear in the single `decisions list` output:\n%s", c.did, c.blocked, out)
		}
		// All five fields present in the rendered row.
		for label, want := range map[string]string{
			"question":      c.question,
			"options":       c.options,
			"blocked_agent": c.blocked,
			"context_link":  c.ctx,
			"decision_id":   c.did,
		} {
			if !strings.Contains(row, want) {
				t.Fatalf("S2 VIOLATED: row for %s is missing the %s field %q (all five fields required: question·options·blocked_agent·context_link·decision_id)\nrow: %q", c.did, label, want, row)
			}
		}
	}
	t.Logf("Part A PASS (S2): three distinct blocked agents (alice,bob,carol) all render in ONE list, each with all five fields")

	// ──────────────────────────────────────────────────────────────────────────
	// Part B — S6: pure projection, NO aggregator. The rig runs no aggregator
	// process; the list rendered correctly above off an on-demand fold of the log.
	// Assert the projection is restart-survivable / stateless: a SECOND list call
	// (no state carried) renders the identical open set.
	// ──────────────────────────────────────────────────────────────────────────

	code2, out2 := v1lRunListCapturingStdout(t, rig, false)
	if code2 != 0 {
		t.Fatalf("Part B: second list exit = %d, want 0", code2)
	}
	for _, c := range cases {
		if !strings.Contains(out2, c.did) {
			t.Fatalf("S6 VIOLATED: decision %s missing from a second on-demand list (projection must be stateless/no-aggregator)", c.did)
		}
	}
	t.Logf("Part B PASS (S6): list is a pure on-demand projection — no aggregator process, identical open set on a repeat call")

	// ──────────────────────────────────────────────────────────────────────────
	// Part C — N9: orphaned-pending flag is READ-PURE (display only, NO emit).
	// Make carol OFFLINE (explicit "offline"/leave agent_presence beat). carol's
	// decision must be flagged orphaned-pending; alice/bob (no presence record →
	// not-offline) must NOT be. The list call MUST emit NOTHING.
	// ──────────────────────────────────────────────────────────────────────────

	// Pre-condition: BEFORE carol goes offline, her decision must NOT be flagged
	// (a blocked_agent with no presence record is not-offline → not flagged).
	if _, preFlag := orphanedFlagInText(out, carolDID); preFlag {
		t.Fatalf("Part C pre-cond: carol's decision was flagged orphaned-pending BEFORE any offline beat (no presence record must NOT flag)")
	}

	// Make carol Offline via an explicit leave beat (GetState → StateOffline).
	v1lEmitPresence(t, rig, "carol", core.AgentPresenceStatusOffline, core.AgentPresenceReasonLeave)

	// Snapshot the durable log immediately BEFORE the list call (read-purity ref).
	logBefore, countBefore := v1lReadLog(t, rig.eventsPath)

	// THE LIST CALL UNDER TEST — drives the REAL flagOrphanedPending Offline flag.
	code3, out3 := v1lRunListCapturingStdout(t, rig, false)
	if code3 != 0 {
		t.Fatalf("Part C: list exit = %d, want 0\nstdout:\n%s", code3, out3)
	}

	// READ-PURITY (N9 display-only): the events.jsonl is UNCHANGED by the list call
	// — byte-for-byte AND by event count. The flag is computed, never emitted.
	logAfter, countAfter := v1lReadLog(t, rig.eventsPath)
	if countAfter != countBefore {
		t.Fatalf("N9 READ-PURITY VIOLATED: events.jsonl event count changed across the list call (before=%d after=%d) — the orphaned-pending flag MUST NOT emit", countBefore, countAfter)
	}
	if string(logAfter) != string(logBefore) {
		t.Fatalf("N9 READ-PURITY VIOLATED: events.jsonl bytes changed across the list call (before %d bytes, after %d bytes) — the list op must be a pure read", len(logBefore), len(logAfter))
	}

	// carol's row IS flagged orphaned-pending (Offline blocked_agent).
	carolRow, carolFlagged := orphanedFlagInText(out3, carolDID)
	if carolRow == "" {
		t.Fatalf("Part C: carol's decision %s vanished from the list after she went offline (it is still OPEN — should still render)", carolDID)
	}
	if !carolFlagged {
		t.Fatalf("N9 VIOLATED: carol's decision %s is NOT flagged orphaned-pending though carol is Offline (explicit leave beat)\nrow: %q", carolDID, carolRow)
	}

	// alice/bob are NOT flagged (no presence record → not-offline → not flagged).
	for _, c := range []fieldCase{cases[0], cases[1]} {
		row, flagged := orphanedFlagInText(out3, c.did)
		if flagged {
			t.Fatalf("N9 VIOLATED: %s (blocked_agent %s, no offline beat) is wrongly flagged orphaned-pending\nrow: %q", c.did, c.blocked, row)
		}
	}
	t.Logf("Part C PASS (N9): carol Offline → flagged orphaned-pending; alice/bob NOT flagged; events.jsonl byte-for-byte unchanged (read-pure, %d events before+after)", countBefore)

	// ──────────────────────────────────────────────────────────────────────────
	// Part D — STALE (not Offline) is NOT flagged. carol just went Offline via a
	// leave beat; a STALE agent (TTL ≤ age < 10m, no leave) must NOT flag. We
	// verify the predicate boundary using a fresh agent "dave" with a recent
	// ONLINE beat (Online → not flagged) — proving the flag keys on Offline only,
	// not on "has any presence record".
	// ──────────────────────────────────────────────────────────────────────────

	daveDID := v1lRaise(t, rig, "Bump dependency?", []string{"bump", "skip"}, "dave", "hk-dave-ctx")
	v1lEmitPresence(t, rig, "dave", core.AgentPresenceStatusOnline, core.AgentPresenceReasonJoin)

	code4, out4 := v1lRunListCapturingStdout(t, rig, false)
	if code4 != 0 {
		t.Fatalf("Part D: list exit = %d, want 0", code4)
	}
	daveRow, daveFlagged := orphanedFlagInText(out4, daveDID)
	if daveRow == "" {
		t.Fatalf("Part D: dave's decision %s missing from list", daveDID)
	}
	if daveFlagged {
		t.Fatalf("N9 VIOLATED: dave (Online presence beat) is wrongly flagged orphaned-pending — only Offline must flag\nrow: %q", daveRow)
	}
	t.Logf("Part D PASS: an Online blocked_agent (dave) is NOT flagged — the flag keys on Offline, not on presence existence")

	// ──────────────────────────────────────────────────────────────────────────
	// Part E — --json output shape (the machine-readable rows). Each row carries
	// the five fields + orphaned_pending; carol's row has orphaned_pending=true.
	// ──────────────────────────────────────────────────────────────────────────

	codeJ, outJ := v1lRunListCapturingStdout(t, rig, true /*json*/)
	if codeJ != 0 {
		t.Fatalf("Part E: --json list exit = %d, want 0\nstdout:\n%s", codeJ, outJ)
	}

	aliceJSON := v1lRowFor(t, outJ, aliceDID)
	for _, key := range []string{"decision_id", "question", "options", "blocked_agent", "context_link", "orphaned_pending"} {
		if _, ok := aliceJSON[key]; !ok {
			t.Fatalf("Part E (--json): alice row is missing key %q (the machine-readable shape must carry the five fields + orphaned_pending)\nrow: %+v", key, aliceJSON)
		}
	}
	if aliceJSON["question"] != "Ship v2 to prod?" {
		t.Fatalf("Part E (--json): alice question = %v, want \"Ship v2 to prod?\"", aliceJSON["question"])
	}
	if aliceJSON["blocked_agent"] != "alice" {
		t.Fatalf("Part E (--json): alice blocked_agent = %v, want \"alice\"", aliceJSON["blocked_agent"])
	}
	if aliceJSON["context_link"] != "hk-alice-ctx" {
		t.Fatalf("Part E (--json): alice context_link = %v, want \"hk-alice-ctx\"", aliceJSON["context_link"])
	}
	// options is a JSON array.
	if opts, ok := aliceJSON["options"].([]any); !ok || len(opts) != 2 {
		t.Fatalf("Part E (--json): alice options = %v, want a 2-element array [ship hold]", aliceJSON["options"])
	}
	if aliceJSON["orphaned_pending"] != false {
		t.Fatalf("Part E (--json): alice orphaned_pending = %v, want false (alice has no offline beat)", aliceJSON["orphaned_pending"])
	}

	carolJSON := v1lRowFor(t, outJ, carolDID)
	if carolJSON["orphaned_pending"] != true {
		t.Fatalf("Part E (--json): carol orphaned_pending = %v, want true (carol is Offline)\nrow: %+v", carolJSON["orphaned_pending"], carolJSON)
	}
	t.Logf("Part E PASS (--json): rows carry the five fields + orphaned_pending; carol orphaned_pending=true, alice=false")
}

// orphanedFlagInText finds the rendered text row containing decisionID and reports
// whether it carries the "[orphaned-pending]" display marker. Returns ("", false)
// when no row matches.
func orphanedFlagInText(out, decisionID string) (row string, flagged bool) {
	for _, ln := range strings.Split(out, "\n") {
		if strings.Contains(ln, decisionID) {
			return ln, strings.Contains(ln, "[orphaned-pending]")
		}
	}
	return "", false
}
