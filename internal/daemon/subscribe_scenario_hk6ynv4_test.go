package daemon_test

// subscribe_scenario_hk6ynv4_test.go — end-to-end scenario test for the
// "subscribe" socket op (bead hk-6ynv4).
//
// This test wires the full socket → SubscribeHandler → SubscribeHub → EventBus
// path against a real busImpl + Unix-domain socket, then asserts:
//
//   - run_started arrives on the subscriber stream (causality test #1).
//   - A subsequent reviewer_launched arrives in order after run_started
//     (causality test #2 — would have caught hk-5s7tg's missing-event class).
//   - Type filter excludes events the subscriber did not request.
//   - The subscription closes cleanly when the daemon context is cancelled.
//
// # Why not daemon.Start?
//
// daemon.Start in unit-test mode (no BrPath, no work loop) returns
// immediately after wiring, which closes the JSONL writer and the bus is
// no longer usable for synthetic emissions. Wiring socket+bus directly is
// the idiomatic scenario shape for testing the subscribe surface without
// pulling in the bead-dispatch path.

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/eventbus"
	"github.com/gregberns/harmonik/internal/handlercontract"
)

// mustMakeShortTempDir returns a short-path temp directory under /tmp/ so the
// unix socket path stays under the 104-char macOS sockaddr_un limit.
func mustMakeShortTempDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "hk6ynv4-")
	if err != nil {
		t.Fatalf("mustMakeShortTempDir: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

// hk6ynv4ScenarioRig wires bus + hub + socket listener and returns the
// path to the bound socket and a function to tear down cleanly.
type hk6ynv4ScenarioRig struct {
	bus      eventbus.EventBus
	sockPath string
	cancel   context.CancelFunc
	done     chan struct{}
}

func startHk6ynv4Rig(t *testing.T) *hk6ynv4ScenarioRig {
	t.Helper()
	dir := mustMakeShortTempDir(t)
	if err := os.MkdirAll(filepath.Join(dir, ".harmonik"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	sockPath := filepath.Join(dir, ".harmonik", "daemon.sock")

	registry := handlercontract.NewRedactionRegistry()
	bus := eventbus.NewBusImplWithWriter(registry, nil)
	hub := daemon.NewSubscribeHub(daemon.SubscribeHubConfig{
		Bus: bus,
	})
	if err := hub.Subscribe(bus); err != nil {
		t.Fatalf("hub.Subscribe: %v", err)
	}
	if err := bus.Seal(); err != nil {
		t.Fatalf("bus.Seal: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = daemon.RunSocketListenerWithSubscribe(ctx, sockPath, nil, nil, hub)
	}()

	// Wait for socket to appear.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(sockPath); err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if _, err := os.Stat(sockPath); err != nil {
		cancel()
		<-done
		t.Fatalf("socket not bound within 5s: %v", err)
	}

	t.Cleanup(func() {
		cancel()
		<-done
	})

	return &hk6ynv4ScenarioRig{bus: bus, sockPath: sockPath, cancel: cancel, done: done}
}

// TestScenario_Hk6ynv4_SubscribeStream_EndToEnd asserts:
//   - run_started arrives on the subscriber wire (no JSONL tail involved).
//   - reviewer_launched arrives next, in order (would catch hk-5s7tg
//     missing-event class).
//   - Subscription closes cleanly on daemon cancel.
func TestScenario_Hk6ynv4_SubscribeStream_EndToEnd(t *testing.T) {
	skipRealDaemonE2EInShort(t)
	t.Parallel()
	rig := startHk6ynv4Rig(t)

	conn, err := net.Dial("unix", rig.sockPath)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.Close() }()

	req := map[string]any{
		"op":                "subscribe",
		"types":             []string{"run_started", "reviewer_launched"},
		"heartbeat_seconds": 600,
	}
	reqBytes, _ := json.Marshal(req)
	if _, err := conn.Write(reqBytes); err != nil {
		t.Fatalf("write subscribe request: %v", err)
	}

	// Give the handler a moment to register the subscriber.
	time.Sleep(100 * time.Millisecond)

	runID, _ := uuid.NewV7()
	rid := core.RunID(runID)
	ridStr := rid.String()
	emitCtx, emitCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer emitCancel()
	if err := rig.bus.EmitWithRunID(emitCtx, rid, core.EventTypeRunStarted,
		json.RawMessage(`{"run_id":"`+ridStr+`","bead_id":"hk-test"}`)); err != nil {
		t.Fatalf("emit run_started: %v", err)
	}
	if err := rig.bus.EmitWithRunID(emitCtx, rid, core.EventTypeReviewerLaunched,
		json.RawMessage(`{"run_id":"`+ridStr+`"}`)); err != nil {
		t.Fatalf("emit reviewer_launched: %v", err)
	}

	_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	rdr := bufio.NewReader(conn)

	type wireEvent struct {
		Type string `json:"type"`
	}
	var got []string
	for len(got) < 2 {
		line, err := rdr.ReadBytes('\n')
		if err != nil {
			t.Fatalf("read subscribe stream (got=%v): %v", got, err)
		}
		var we wireEvent
		if jsonErr := json.Unmarshal(line, &we); jsonErr != nil {
			continue
		}
		switch we.Type {
		case "run_started", "reviewer_launched":
			got = append(got, we.Type)
		}
	}
	if got[0] != "run_started" || got[1] != "reviewer_launched" {
		t.Errorf("subscribe stream order: got %v, want [run_started reviewer_launched]; "+
			"causality assertion would have flagged hk-5s7tg-class missing event", got)
	}

	// Cancel daemon; subscription should close cleanly.
	rig.cancel()
	select {
	case <-rig.done:
	case <-time.After(5 * time.Second):
		t.Fatal("daemon goroutine did not exit within 5s of ctx cancel")
	}
}

// TestScenario_Hk6ynv4_SubscribeStream_TypeFilterIsolation asserts that a
// subscriber requesting [run_completed] does NOT receive run_started events.
func TestScenario_Hk6ynv4_SubscribeStream_TypeFilterIsolation(t *testing.T) {
	t.Parallel()
	rig := startHk6ynv4Rig(t)

	conn, err := net.Dial("unix", rig.sockPath)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.Close() }()

	req := map[string]any{
		"op":                "subscribe",
		"types":             []string{"run_completed"},
		"heartbeat_seconds": 600,
	}
	reqBytes, _ := json.Marshal(req)
	if _, err := conn.Write(reqBytes); err != nil {
		t.Fatalf("write: %v", err)
	}
	time.Sleep(100 * time.Millisecond)

	runID, _ := uuid.NewV7()
	rid := core.RunID(runID)
	ridStr := rid.String()
	ctx := context.Background()
	if err := rig.bus.EmitWithRunID(ctx, rid, core.EventTypeRunStarted,
		json.RawMessage(`{"run_id":"`+ridStr+`","bead_id":"hk-x"}`)); err != nil {
		t.Fatalf("emit run_started: %v", err)
	}
	if err := rig.bus.EmitWithRunID(ctx, rid, core.EventTypeRunCompleted,
		json.RawMessage(`{"run_id":"`+ridStr+`","success":true}`)); err != nil {
		t.Fatalf("emit run_completed: %v", err)
	}

	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	rdr := bufio.NewReader(conn)
	for {
		line, err := rdr.ReadBytes('\n')
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		var we struct {
			Type string `json:"type"`
		}
		if jsonErr := json.Unmarshal(line, &we); jsonErr != nil {
			continue
		}
		if we.Type == "run_started" {
			t.Fatalf("type-filter leak: subscriber requested [run_completed] but received run_started")
		}
		if we.Type == "run_completed" {
			return // success
		}
	}
}
