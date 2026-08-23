//go:build scenario

package main

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

type v1lRecvDepsSetter interface {
	SetRecvDeps(pollStore, liveStore *daemon.CursorStore, eventsJSONLPath string)
}
type v1lDecisionsRaiser interface {
	HandleDecisionsRaise(ctx context.Context, payload json.RawMessage) (json.RawMessage, error)
}

type v1lRig struct {
	bus        eventbus.EventBus
	handler    daemon.CommsSendHandler
	sockPath   string
	eventsPath string
	absProject string
}

func v1lMustShortTempDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "1vl-")
	if err != nil {
		t.Fatalf("v1lMustShortTempDir: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

func v1lStartRig(t *testing.T) *v1lRig {
	t.Helper()
	dir := v1lMustShortTempDir(t)
	if err := os.MkdirAll(filepath.Join(dir, ".harmonik", "events"), 0o755); err != nil {
		t.Fatalf("v1lStartRig: mkdir: %v", err)
	}
	sockPath := filepath.Join(dir, ".harmonik", "daemon.sock")
	eventsPath := filepath.Join(dir, ".harmonik", "events", "events.jsonl")

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

	absProject := filepath.Dir(filepath.Dir(filepath.Dir(eventsPath)))
	return &v1lRig{bus: bus, handler: handler, sockPath: sockPath, eventsPath: eventsPath, absProject: absProject}
}

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
	if err := rig.bus.Emit(context.Background(), core.EventType("agent_presence"), payloadBytes); err != nil {
		t.Fatalf("v1lEmitPresence: Emit(agent_presence) for %s: %v", agent, err)
	}
}

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

	code := runDecisionsListOrShowParsed("", "", jsonFlag, rig.sockPath, rig.absProject, "list")

	_ = w.Close()
	os.Stdout = oldStdout
	return code, <-outCh
}

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

	aliceDID := v1lRaise(t, rig, "Ship v2 to prod?", []string{"ship", "hold"}, "alice", "hk-alice-ctx")
	bobDID := v1lRaise(t, rig, "Pick deploy region?", []string{"us", "eu"}, "bob", "hk-bob-ctx")

	carolDID := v1lRaise(t, rig, "Roll back migration?", []string{"yes", "no"}, "carol", "hk-carol-ctx")

	code, out := v1lRunListCapturingStdout(t, rig, false /*text*/)
	if code != 0 {
		t.Fatalf("Part A: list exit = %d, want 0\nstdout:\n%s", code, out)
	}

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

	if _, preFlag := orphanedFlagInText(out, carolDID); preFlag {
		t.Fatalf("Part C pre-cond: carol's decision was flagged orphaned-pending BEFORE any offline beat (no presence record must NOT flag)")
	}

	v1lEmitPresence(t, rig, "carol", core.AgentPresenceStatusOffline, core.AgentPresenceReasonLeave)

	logBefore, countBefore := v1lReadLog(t, rig.eventsPath)

	code3, out3 := v1lRunListCapturingStdout(t, rig, false)
	if code3 != 0 {
		t.Fatalf("Part C: list exit = %d, want 0\nstdout:\n%s", code3, out3)
	}

	logAfter, countAfter := v1lReadLog(t, rig.eventsPath)
	if countAfter != countBefore {
		t.Fatalf("N9 READ-PURITY VIOLATED: events.jsonl event count changed across the list call (before=%d after=%d) — the orphaned-pending flag MUST NOT emit", countBefore, countAfter)
	}
	if string(logAfter) != string(logBefore) {
		t.Fatalf("N9 READ-PURITY VIOLATED: events.jsonl bytes changed across the list call (before %d bytes, after %d bytes) — the list op must be a pure read", len(logBefore), len(logAfter))
	}

	carolRow, carolFlagged := orphanedFlagInText(out3, carolDID)
	if carolRow == "" {
		t.Fatalf("Part C: carol's decision %s vanished from the list after she went offline (it is still OPEN — should still render)", carolDID)
	}
	if !carolFlagged {
		t.Fatalf("N9 VIOLATED: carol's decision %s is NOT flagged orphaned-pending though carol is Offline (explicit leave beat)\nrow: %q", carolDID, carolRow)
	}

	for _, c := range []fieldCase{cases[0], cases[1]} {
		row, flagged := orphanedFlagInText(out3, c.did)
		if flagged {
			t.Fatalf("N9 VIOLATED: %s (blocked_agent %s, no offline beat) is wrongly flagged orphaned-pending\nrow: %q", c.did, c.blocked, row)
		}
	}
	t.Logf("Part C PASS (N9): carol Offline → flagged orphaned-pending; alice/bob NOT flagged; events.jsonl byte-for-byte unchanged (read-pure, %d events before+after)", countBefore)

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

func orphanedFlagInText(out, decisionID string) (row string, flagged bool) {
	for _, ln := range strings.Split(out, "\n") {
		if strings.Contains(ln, decisionID) {
			return ln, strings.Contains(ln, "[orphaned-pending]")
		}
	}
	return "", false
}
