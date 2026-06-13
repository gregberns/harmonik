//go:build scenario

package main

// scenario_decisions_gate_rz4_test.go — the hitl-decisions GATE scenario
// (component K-gate, bead hk-rz4): the end-to-end raise→block→answer→wake round
// trip, plus N3 first-writer-wins and the N8 arm-then-check race guard.
//
// # What is tested (SPEC §8 S1, S3, S4, S8 + §6 N3 / N8)
//
//	S1/S4 (raise → block → answer → wake):
//	  1. An agent RAISES a decision_needed (question + ≥2 options + blocked_agent +
//	     context) through the REAL daemon handler and gets back a decision_id.
//	  2. The agent BLOCKS on the REAL §4 blocked-wait (decisionsBlockedWait) — the
//	     production N8 arm-then-check wait: ARM a live subscribe stream FIRST, THEN
//	     re-project the durable log; if not yet terminal, block on the stream.
//	  3. An operator ANSWERS (decision_resolved, chosen_option) through the REAL
//	     daemon handler.
//	  4. The blocked agent WAKES via the live subscribe stream with the correct
//	     chosen_option (exit 0; chosen_option on stdout).
//
//	S3/S8 (durable terminal): the decision_resolved JSONL record lands in
//	events.jsonl (asserted by re-scanning the durable log), and the wait returns
//	the chosen_option read off the live stream.
//
//	N3 (first-writer-wins): a SECOND answer for the same decision_id is a NO-OP —
//	the daemon emits NO second decision_resolved (the answer handler returns
//	NoOp=true), so there is no second wake and EXACTLY ONE decision_resolved is in
//	the durable log.
//
//	N8 race guard (answer-before-arm): if the answer fires BEFORE the wait arms +
//	re-projects, the arm-then-check STILL returns — the step-2 re-project catches
//	the already-logged terminal and the wait returns immediately without blocking.
//
// # Approach (a) — in-process integration against the REAL wait + REAL handlers
//
// This is approach (a) from the bead brief, chosen because it exercises the
// ACTUAL production wake path rather than a stub:
//
//   - The wait under test is the REAL cmd/harmonik decisionsBlockedWait (package
//     main, same package as this test) — the production N8 arm-then-check. It
//     dials a real daemon socket, arms a real `subscribe` stream, and re-projects
//     the real durable log. Nothing about the wait is reimplemented or stubbed.
//   - The raise / answer are the REAL daemon handlers HandleDecisionsRaise /
//     HandleDecisionsAnswer on the REAL *commsSendHandlerImpl (via
//     daemon.NewCommsSendHandler), emitting through a REAL eventbus busImpl that
//     fsyncs the F-class decision_* events to events.jsonl and fans them out to a
//     REAL daemon.SubscribeHub subscribed to that same bus.
//   - The wake therefore travels the genuine path: answer handler → bus.EmitTyped
//     → JSONL append (durable) + subscriber fan-out → the SubscribeHub →
//     the live socket subscribe stream → decisionsBlockedWait's blocking scan.
//
// We do NOT boot daemon.Start (HandlerBinary, br, worktrees) — that machinery is
// irrelevant to the decision round trip and would add nothing to the S1/S3/S4/S8
// assertions. We DO use every real component on the decision data path.
//
// # Helper prefix
//
// Helpers use the prefix "rz4" per the helper-prefix discipline.
//
// Run independently (the daemon gate skips //go:build scenario):
//
//	go test -tags scenario -run TestScenario_DecisionsGate_RZ4 ./cmd/harmonik/...
//
// Spec ref: SPEC.md §1, §3 (projection), §4 (N8 blocked-wait), §8 S1/S3/S4/S8, §6 N3.
// Bead ref: hk-rz4 (K-gate).

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

// rz4RecvDepsSetter is the subset of *commsSendHandlerImpl this test needs to
// configure. daemon.NewCommsSendHandler returns a CommsSendHandler interface
// whose concrete type is the unexported *commsSendHandlerImpl; we cannot name
// that type from package main, but we CAN assert the value to this local
// interface (the concrete type satisfies it) to wire the events-JSONL path the
// list/answer handlers read (the same SetRecvDeps the daemon wiring calls).
type rz4RecvDepsSetter interface {
	SetRecvDeps(store *daemon.CursorStore, eventsJSONLPath string)
}

// rz4DecisionsRaiser/Answerer are the subsets of the daemon's DecisionsHandler
// this test drives. The concrete handler satisfies both; we type-assert to these
// local interfaces so package main can invoke the real handlers directly.
type rz4DecisionsRaiser interface {
	HandleDecisionsRaise(ctx context.Context, payload json.RawMessage) (json.RawMessage, error)
}
type rz4DecisionsAnswerer interface {
	HandleDecisionsAnswer(ctx context.Context, payload json.RawMessage) (json.RawMessage, error)
}

// rz4Rig is the real-component rig for the gate scenario: a real bus + hub +
// decisions handler over a real daemon socket, writing to a real events.jsonl.
type rz4Rig struct {
	bus        eventbus.EventBus
	handler    daemon.CommsSendHandler
	sockPath   string
	eventsPath string
}

// rz4MustShortTempDir returns a short-path temp dir under /tmp so the unix socket
// path stays under the 104-char macOS sockaddr_un limit (matches the S5/S7 and
// hk6ynv4 rigs).
func rz4MustShortTempDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "rz4-")
	if err != nil {
		t.Fatalf("rz4MustShortTempDir: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

// rz4StartRig wires a real bus, a SubscribeHub subscribed to it, and the real
// decisions handler over a real daemon socket listener, then waits for the socket
// to bind. The hub is subscribed BEFORE bus.Seal (EV-009); the listener serves
// both the `subscribe` op (for the wait's stream) and the decisions-* ops.
func rz4StartRig(t *testing.T) *rz4Rig {
	t.Helper()
	dir := rz4MustShortTempDir(t)
	if err := os.MkdirAll(filepath.Join(dir, ".harmonik", "events"), 0o755); err != nil {
		t.Fatalf("rz4StartRig: mkdir: %v", err)
	}
	sockPath := filepath.Join(dir, ".harmonik", "daemon.sock")
	eventsPath := filepath.Join(dir, ".harmonik", "events", "events.jsonl")

	// Real durable bus writing to the real events.jsonl (F-class fsync for the
	// three decision_* types — the same path production uses).
	writer, err := eventbus.OpenJSONLWriter(eventsPath)
	if err != nil {
		t.Fatalf("rz4StartRig: open JSONL writer: %v", err)
	}
	bus := eventbus.NewBusImplWithWriter(core.NewRedactionRegistry(), writer)

	// Real SubscribeHub feeding the wait's live stream. EventsJSONLPath lets the
	// hub replay on since_event_id, but the §4 wait arms live-only (no since), so
	// replay is not exercised — the durable re-project is the wait's own concern.
	hub := daemon.NewSubscribeHub(daemon.SubscribeHubConfig{
		Bus:             bus,
		EventsJSONLPath: eventsPath,
	})
	if err := hub.Subscribe(bus); err != nil {
		t.Fatalf("rz4StartRig: hub.Subscribe: %v", err)
	}
	if err := bus.Seal(); err != nil {
		t.Fatalf("rz4StartRig: bus.Seal: %v", err)
	}

	// Real decisions handler (K2 raise/withdraw + K4 list/answer) on the real bus.
	// SetRecvDeps wires the events-JSONL path the answer/list handlers project
	// over (the same call the daemon wiring makes); the cursor store is unused by
	// the decisions ops but required by the signature.
	handler := daemon.NewCommsSendHandler(bus)
	if handler == nil {
		t.Fatalf("rz4StartRig: NewCommsSendHandler returned nil (bus must satisfy CommsMessageEmitter)")
	}
	depSetter, ok := handler.(rz4RecvDepsSetter)
	if !ok {
		t.Fatalf("rz4StartRig: handler does not expose SetRecvDeps")
	}
	depSetter.SetRecvDeps(daemon.NewCursorStore(filepath.Join(dir, "cursors")), eventsPath)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		// Full listener: subscribe handler (for the wait's stream) + comms/decisions
		// handler (for raise/answer). RequestHandler/HookRelay/OperatorControl unused.
		_ = daemon.RunSocketListenerFull(ctx, sockPath, nil, nil, hub, nil, handler)
	}()
	t.Cleanup(func() {
		cancel()
		<-done
	})

	// Wait for the socket to bind.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, statErr := os.Stat(sockPath); statErr == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if _, statErr := os.Stat(sockPath); statErr != nil {
		t.Fatalf("rz4StartRig: socket %s did not bind within 5s", sockPath)
	}

	return &rz4Rig{bus: bus, handler: handler, sockPath: sockPath, eventsPath: eventsPath}
}

// rz4Raise drives the REAL HandleDecisionsRaise handler and returns the minted
// decision_id (the decision_needed event's own event_id, SPEC §1).
func rz4Raise(t *testing.T, rig *rz4Rig, question string, options []string, blockedAgent, contextLink string) string {
	t.Helper()
	raiser, ok := rig.handler.(rz4DecisionsRaiser)
	if !ok {
		t.Fatalf("rz4Raise: handler does not implement HandleDecisionsRaise")
	}
	reqBytes, _ := json.Marshal(daemon.DecisionsRaiseRequest{
		Question:     question,
		Options:      options,
		BlockedAgent: blockedAgent,
		ContextLink:  contextLink,
	})
	resBytes, err := raiser.HandleDecisionsRaise(context.Background(), reqBytes)
	if err != nil {
		t.Fatalf("rz4Raise: HandleDecisionsRaise: %v", err)
	}
	var res daemon.DecisionsRaiseResult
	if jerr := json.Unmarshal(resBytes, &res); jerr != nil {
		t.Fatalf("rz4Raise: decode raise result: %v", jerr)
	}
	if res.DecisionID == "" {
		t.Fatalf("rz4Raise: empty decision_id from raise")
	}
	return res.DecisionID
}

// rz4Answer drives the REAL HandleDecisionsAnswer handler and returns the result
// (EventID set on a real resolve; NoOp=true on the N3 first-writer-wins no-op).
func rz4Answer(t *testing.T, rig *rz4Rig, decisionID, chosenOption, resolver string) daemon.DecisionsAnswerResult {
	t.Helper()
	answerer, ok := rig.handler.(rz4DecisionsAnswerer)
	if !ok {
		t.Fatalf("rz4Answer: handler does not implement HandleDecisionsAnswer")
	}
	reqBytes, _ := json.Marshal(daemon.DecisionsAnswerRequest{
		DecisionID:   decisionID,
		ChosenOption: chosenOption,
		Resolver:     resolver,
	})
	resBytes, err := answerer.HandleDecisionsAnswer(context.Background(), reqBytes)
	if err != nil {
		t.Fatalf("rz4Answer: HandleDecisionsAnswer(%s,%s): %v", decisionID, chosenOption, err)
	}
	var res daemon.DecisionsAnswerResult
	if jerr := json.Unmarshal(resBytes, &res); jerr != nil {
		t.Fatalf("rz4Answer: decode answer result: %v", jerr)
	}
	return res
}

// rz4CountResolvedFor scans the durable events.jsonl and returns how many
// decision_resolved records target decisionID. Used to assert N3 first-writer-wins
// (EXACTLY ONE resolve is durably applied).
func rz4CountResolvedFor(t *testing.T, eventsPath, decisionID string) int {
	t.Helper()
	var zeroID core.EventID
	n := 0
	for evt := range eventbus.ScanAfter(eventsPath, zeroID) {
		if evt.Type != string(core.EventTypeDecisionResolved) {
			continue
		}
		var p core.DecisionResolvedPayload
		if err := json.Unmarshal(evt.Payload, &p); err != nil {
			continue
		}
		if p.DecisionID == decisionID {
			n++
		}
	}
	return n
}

// rz4RunWaitCapturingStdout runs the REAL decisionsBlockedWait on its own
// goroutine with os.Stdout redirected to a pipe, and returns channels for the
// wait's exit code and the captured stdout (the printed chosen_option). The
// answer handlers write to the bus/JSONL, NOT to stdout, so redirecting stdout
// for the wait's duration does not race with them.
//
// absProject is the project dir; the wait derives <absProject>/.harmonik/events/
// events.jsonl for its re-project and uses sockPath for the arm.
func rz4RunWaitCapturingStdout(t *testing.T, absProject, sockPath, decisionID string) (codeCh <-chan int, outCh <-chan string) {
	t.Helper()
	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("rz4RunWaitCapturingStdout: pipe: %v", err)
	}
	os.Stdout = w

	rc := make(chan int, 1)
	out := make(chan string, 1)

	// Reader goroutine drains the pipe until the write end closes.
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
		out <- b.String()
	}()

	// Wait goroutine: the REAL §4 blocked-wait. On return, restore stdout and
	// close the pipe's write end so the reader finishes.
	go func() {
		code := decisionsBlockedWait(absProject, sockPath, decisionID)
		_ = w.Close()
		os.Stdout = oldStdout
		rc <- code
	}()

	return rc, out
}

// TestScenario_DecisionsGate_RZ4 exercises the full gate: raise → block → answer
// → wake (S1/S4), the durable decision_resolved record (S3/S8), N3 first-writer-
// wins (a second answer is a no-op; exactly one resolve in the log), and the N8
// arm-then-check race guard (answer-before-arm still returns).
func TestScenario_DecisionsGate_RZ4(t *testing.T) {
	rig := rz4StartRig(t)
	absProject := filepath.Dir(filepath.Dir(filepath.Dir(rig.eventsPath))) // <dir> from <dir>/.harmonik/events/events.jsonl

	// ─────────────────────────────────────────────────────────────────────────
	// Part A — raise → block → answer → wake (S1, S3, S4, S8) + N3 first-writer-wins
	// ─────────────────────────────────────────────────────────────────────────

	// 1. Agent RAISES a decision_needed (≥2 options, blocked_agent, context).
	decisionID := rz4Raise(t, rig, "Ship v2 to prod?", []string{"ship", "hold"}, "alice", "hk-rz4")

	// 2. Agent BLOCKS on the REAL §4 blocked-wait (N8 arm-then-check). At this
	// point the decision is OPEN (no terminal logged), so the re-project does not
	// short-circuit — the wait arms the live stream and blocks on it.
	codeCh, outCh := rz4RunWaitCapturingStdout(t, absProject, rig.sockPath, decisionID)

	// Give the wait a moment to arm its subscribe stream + finish the re-project
	// (so the answer below is delivered live, exercising the BLOCK-then-wake path
	// rather than the race-guard re-project path — that is covered in Part B).
	time.Sleep(300 * time.Millisecond)

	// Sanity: the wait must still be blocked (no terminal yet).
	select {
	case code := <-codeCh:
		t.Fatalf("Part A: wait returned (code=%d) BEFORE any answer — it should be blocked on the armed stream", code)
	default:
	}

	// 3. Operator ANSWERS (decision_resolved, chosen_option="ship").
	ans := rz4Answer(t, rig, decisionID, "ship", "operator")
	if ans.NoOp {
		t.Fatalf("Part A: first answer was a NO-OP (decision should have been OPEN)")
	}
	if ans.EventID == "" {
		t.Fatalf("Part A: first answer returned empty event_id (expected a real resolve)")
	}

	// 4. The blocked agent WAKES with the correct chosen_option (exit 0).
	var waitCode int
	select {
	case waitCode = <-codeCh:
	case <-time.After(5 * time.Second):
		t.Fatalf("Part A VIOLATED: blocked agent did NOT wake within 5s of the answer (S4 wake failed)")
	}
	if waitCode != 0 {
		t.Fatalf("Part A: wait exit = %d, want 0 (terminal arrived)", waitCode)
	}
	gotOut := strings.TrimSpace(<-outCh)
	if gotOut != "ship" {
		t.Fatalf("Part A VIOLATED: wait woke with chosen_option %q, want \"ship\" (S4)", gotOut)
	}

	// S3/S8: the decision_resolved JSONL record is durable in events.jsonl.
	if got := rz4CountResolvedFor(t, rig.eventsPath, decisionID); got != 1 {
		t.Fatalf("S3/S8 VIOLATED: decision_resolved count in durable log = %d, want exactly 1 after first answer", got)
	}

	// N3 first-writer-wins: a SECOND answer for the same decision_id is a NO-OP.
	// The decision left the open set on the first resolve, so the answer handler
	// swallows this (NoOp=true, no event), and there is NO second wake and EXACTLY
	// ONE decision_resolved in the durable log.
	ans2 := rz4Answer(t, rig, decisionID, "hold", "operator")
	if !ans2.NoOp {
		t.Fatalf("N3 VIOLATED: second answer was NOT a no-op (got event_id=%q) — first-writer-wins broken", ans2.EventID)
	}
	if ans2.EventID != "" {
		t.Fatalf("N3 VIOLATED: second answer minted a second event_id %q (no second resolve allowed)", ans2.EventID)
	}
	if got := rz4CountResolvedFor(t, rig.eventsPath, decisionID); got != 1 {
		t.Fatalf("N3 VIOLATED: decision_resolved count = %d after a second answer, want EXACTLY 1 (no second resolve applied)", got)
	}

	t.Logf("Part A PASS: decision %s raised → blocked → answered(ship) → woke with %q; exactly one durable resolve; second answer no-op (N3)", decisionID, gotOut)

	// ─────────────────────────────────────────────────────────────────────────
	// Part B — N8 race guard: answer BEFORE the wait arms + re-projects.
	// The arm-then-check's step-2 re-project must catch the already-logged
	// terminal and the wait must return IMMEDIATELY (no block, no missed wake).
	// ─────────────────────────────────────────────────────────────────────────

	raceDID := rz4Raise(t, rig, "Pick deploy region?", []string{"us", "eu"}, "bob", "hk-rz4")

	// Answer FIRST — the terminal is durably logged BEFORE any wait arms. This is
	// the answer-lands-between-read-and-arm race the N8 ordering defends against:
	// because the wait arms the stream FIRST and only THEN re-projects the log, an
	// already-logged terminal is caught by the re-project and returned immediately.
	raceAns := rz4Answer(t, rig, raceDID, "eu", "operator")
	if raceAns.NoOp || raceAns.EventID == "" {
		t.Fatalf("Part B: pre-arm answer should have been a real resolve (got NoOp=%v event_id=%q)", raceAns.NoOp, raceAns.EventID)
	}

	// NOW run the wait. It must return immediately via the re-project (the stream
	// will never deliver this terminal — it was emitted before the arm).
	raceCodeCh, raceOutCh := rz4RunWaitCapturingStdout(t, absProject, rig.sockPath, raceDID)
	var raceCode int
	select {
	case raceCode = <-raceCodeCh:
	case <-time.After(5 * time.Second):
		t.Fatalf("N8 RACE GUARD VIOLATED: wait did NOT return for an already-logged terminal — the re-project failed to catch it (agent would wait forever)")
	}
	if raceCode != 0 {
		t.Fatalf("Part B: race-guard wait exit = %d, want 0", raceCode)
	}
	raceOut := strings.TrimSpace(<-raceOutCh)
	if raceOut != "eu" {
		t.Fatalf("N8 RACE GUARD VIOLATED: wait returned chosen_option %q, want \"eu\" (re-project must read the logged terminal)", raceOut)
	}

	t.Logf("Part B PASS: N8 race guard — answer-before-arm decision %s returned immediately via re-project with %q", raceDID, raceOut)
}
