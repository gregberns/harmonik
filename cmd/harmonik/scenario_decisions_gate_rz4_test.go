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

type rz4RecvDepsSetter interface {
	SetRecvDeps(pollStore, liveStore *daemon.CursorStore, eventsJSONLPath string)
}

type rz4DecisionsRaiser interface {
	HandleDecisionsRaise(ctx context.Context, payload json.RawMessage) (json.RawMessage, error)
}
type rz4DecisionsAnswerer interface {
	HandleDecisionsAnswer(ctx context.Context, payload json.RawMessage) (json.RawMessage, error)
}

type rz4Rig struct {
	bus        eventbus.EventBus
	handler    daemon.CommsSendHandler
	sockPath   string
	eventsPath string
}

func rz4MustShortTempDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "rz4-")
	if err != nil {
		t.Fatalf("rz4MustShortTempDir: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

func rz4StartRig(t *testing.T) *rz4Rig {
	t.Helper()
	dir := rz4MustShortTempDir(t)
	if err := os.MkdirAll(filepath.Join(dir, ".harmonik", "events"), 0o755); err != nil {
		t.Fatalf("rz4StartRig: mkdir: %v", err)
	}
	sockPath := filepath.Join(dir, ".harmonik", "daemon.sock")
	eventsPath := filepath.Join(dir, ".harmonik", "events", "events.jsonl")

	writer, err := eventbus.OpenJSONLWriter(eventsPath)
	if err != nil {
		t.Fatalf("rz4StartRig: open JSONL writer: %v", err)
	}
	bus := eventbus.NewBusImplWithWriter(core.NewRedactionRegistry(), writer)

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

	handler := daemon.NewCommsSendHandler(bus)
	if handler == nil {
		t.Fatalf("rz4StartRig: NewCommsSendHandler returned nil (bus must satisfy CommsMessageEmitter)")
	}
	depSetter, ok := handler.(rz4RecvDepsSetter)
	if !ok {
		t.Fatalf("rz4StartRig: handler does not expose SetRecvDeps")
	}
	rz4CursorStore := daemon.NewCursorStore(filepath.Join(dir, "cursors"))
	depSetter.SetRecvDeps(rz4CursorStore, rz4CursorStore, eventsPath)

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
		t.Fatalf("rz4StartRig: socket %s did not bind within 5s", sockPath)
	}

	return &rz4Rig{bus: bus, handler: handler, sockPath: sockPath, eventsPath: eventsPath}
}

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

func rz4CountResolvedFor(t *testing.T, eventsPath, decisionID string) int {
	t.Helper()
	var zeroID core.EventID
	n := 0
	for evt := range eventbus.ScanAfter(eventsPath, zeroID) {
		if evt.Type != core.EventTypeDecisionResolved {
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

	decisionID := rz4Raise(t, rig, "Ship v2 to prod?", []string{"ship", "hold"}, "alice", "hk-rz4")

	codeCh, outCh := rz4RunWaitCapturingStdout(t, absProject, rig.sockPath, decisionID)

	time.Sleep(300 * time.Millisecond)

	select {
	case code := <-codeCh:
		t.Fatalf("Part A: wait returned (code=%d) BEFORE any answer — it should be blocked on the armed stream", code)
	default:
	}

	ans := rz4Answer(t, rig, decisionID, "ship", "operator")
	if ans.NoOp {
		t.Fatalf("Part A: first answer was a NO-OP (decision should have been OPEN)")
	}
	if ans.EventID == "" {
		t.Fatalf("Part A: first answer returned empty event_id (expected a real resolve)")
	}

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

	if got := rz4CountResolvedFor(t, rig.eventsPath, decisionID); got != 1 {
		t.Fatalf("S3/S8 VIOLATED: decision_resolved count in durable log = %d, want exactly 1 after first answer", got)
	}

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

	raceDID := rz4Raise(t, rig, "Pick deploy region?", []string{"us", "eu"}, "bob", "hk-rz4")

	raceAns := rz4Answer(t, rig, raceDID, "eu", "operator")
	if raceAns.NoOp || raceAns.EventID == "" {
		t.Fatalf("Part B: pre-arm answer should have been a real resolve (got NoOp=%v event_id=%q)", raceAns.NoOp, raceAns.EventID)
	}

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
