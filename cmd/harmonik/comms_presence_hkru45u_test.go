package main

// comms_presence_hkru45u_test.go — tests for the hk-ru45u presence improvements:
//
//   (A) comms join --reason=refresh uses reason:"refresh" (not persisted, reduces log noise).
//   (B) comms join --reason=<invalid> returns exit 1 with no event emitted.
//   (C) runCommsRecvFollowIO emits a leave beat on context cancellation (leave-on-teardown).
//
// Bead ref: hk-ru45u.

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/eventbus"
)

// ---------------------------------------------------------------------------
// Fixture helpers
// ---------------------------------------------------------------------------

// ru45uFixtureStartListener starts a real daemon socket listener that supports
// "comms-presence" (for join/refresh/leave beats) and "subscribe" (for the
// --follow read loop), backed by an in-memory bus that captures every
// agent_presence event emitted.
func ru45uFixtureStartListener(t *testing.T, sockPath string) (*[]core.Event, *sync.Mutex) {
	t.Helper()

	bus := eventbus.NewBusImpl()
	var mu sync.Mutex
	var captured []core.Event

	sub := core.Subscription{
		ConsumerID:    "test-ru45u-capture",
		ConsumerClass: core.ConsumerClassSynchronous,
		EventPattern: core.EventPattern{
			Wildcard: false,
			Types:    map[core.EventType]struct{}{"agent_presence": {}},
		},
		Handler: func(_ context.Context, evt core.Event) error {
			mu.Lock()
			captured = append(captured, evt)
			mu.Unlock()
			return nil
		},
	}
	if _, err := bus.Subscribe(sub); err != nil {
		t.Fatalf("ru45uFixtureStartListener: Subscribe: %v", err)
	}
	if err := bus.Seal(); err != nil {
		t.Fatalf("ru45uFixtureStartListener: Seal: %v", err)
	}

	hub := daemon.NewSubscribeHub(daemon.SubscribeHubConfig{Bus: nil})
	ch := daemon.NewCommsSendHandler(bus)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		if err := daemon.RunSocketListenerFull(ctx, sockPath, nil, nil, hub, nil, ch); err != nil {
			t.Logf("RunSocketListenerFull: %v", err)
		}
	}()
	t.Cleanup(func() {
		cancel()
		<-done
	})

	deadline := time.Now().Add(3 * time.Second)
	var lastDialErr error
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("unix", sockPath, 50*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			lastDialErr = nil
			break
		}
		lastDialErr = err
		time.Sleep(5 * time.Millisecond)
	}
	if lastDialErr != nil {
		t.Fatalf("ru45uFixtureStartListener: socket %s never became dialable: %v", sockPath, lastDialErr)
	}

	return &captured, &mu
}

// ru45uCapturedByReason returns all captured events whose payload.reason equals reason.
func ru45uCapturedByReason(captured []core.Event, mu *sync.Mutex, reason string) []core.Event {
	mu.Lock()
	defer mu.Unlock()
	var out []core.Event
	for _, evt := range captured {
		var p core.AgentPresencePayload
		if err := json.Unmarshal(evt.Payload, &p); err != nil {
			continue
		}
		if string(p.Reason) == reason {
			out = append(out, evt)
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// (A) comms join --reason=refresh uses reason:"refresh"
// ---------------------------------------------------------------------------

// TestCommsJoin_ReasonRefreshFlag verifies that `harmonik comms join --reason=refresh`
// emits an agent_presence event with reason:"refresh" (not "join"), so the beat
// is treated as a non-persistent heartbeat tick (hk-ru45u).
func TestCommsJoin_ReasonRefreshFlag(t *testing.T) {
	sockPath := "/tmp/hkru45u-join-refresh.sock"
	_ = os.Remove(sockPath)
	t.Cleanup(func() { _ = os.Remove(sockPath) })

	captured, mu := ru45uFixtureStartListener(t, sockPath)

	code := runCommsPresenceSubcommand([]string{
		"--name", "alice",
		"--socket", sockPath,
		"--reason", "refresh",
	}, "join")
	if code != 0 {
		t.Fatalf("comms join --reason=refresh: expected exit 0, got %d", code)
	}

	// Give the synchronous consumer a moment to process (should be instant).
	time.Sleep(10 * time.Millisecond)

	refreshEvents := ru45uCapturedByReason(*captured, mu, "refresh")
	if len(refreshEvents) == 0 {
		t.Fatal("comms join --reason=refresh: expected ≥1 agent_presence event with reason=refresh, got 0")
	}

	var p core.AgentPresencePayload
	if err := json.Unmarshal(refreshEvents[0].Payload, &p); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if p.Agent != "alice" {
		t.Errorf("payload.agent=%q, want %q", p.Agent, "alice")
	}
	if p.Status != core.AgentPresenceStatusOnline {
		t.Errorf("payload.status=%q, want %q", p.Status, core.AgentPresenceStatusOnline)
	}
	if p.Reason != core.AgentPresenceReasonRefresh {
		t.Errorf("payload.reason=%q, want %q", p.Reason, core.AgentPresenceReasonRefresh)
	}

	// No join event should have been emitted.
	joinEvents := ru45uCapturedByReason(*captured, mu, "join")
	if len(joinEvents) > 0 {
		t.Errorf("comms join --reason=refresh: got unexpected reason=join event(s): %d", len(joinEvents))
	}
}

// TestCommsJoin_DefaultReasonIsJoin verifies that bare `comms join` (no --reason)
// still emits reason:"join" (backward-compatible default).
func TestCommsJoin_DefaultReasonIsJoin(t *testing.T) {
	sockPath := "/tmp/hkru45u-join-default.sock"
	_ = os.Remove(sockPath)
	t.Cleanup(func() { _ = os.Remove(sockPath) })

	captured, mu := ru45uFixtureStartListener(t, sockPath)

	code := runCommsPresenceSubcommand([]string{
		"--name", "bob",
		"--socket", sockPath,
	}, "join")
	if code != 0 {
		t.Fatalf("comms join (no --reason): expected exit 0, got %d", code)
	}

	time.Sleep(10 * time.Millisecond)

	joinEvents := ru45uCapturedByReason(*captured, mu, "join")
	if len(joinEvents) == 0 {
		t.Fatal("comms join (no --reason): expected ≥1 agent_presence event with reason=join, got 0")
	}
}

// ---------------------------------------------------------------------------
// (B) comms join --reason=<invalid> returns exit 1
// ---------------------------------------------------------------------------

// TestCommsJoin_ReasonLeaveRejected verifies that --reason=leave is rejected for
// the join verb (use `comms leave` instead). Exit code must be 1.
func TestCommsJoin_ReasonLeaveRejected(t *testing.T) {
	code := runCommsPresenceSubcommand([]string{
		"--name", "charlie",
		"--socket", "/tmp/hkru45u-never-dial.sock",
		"--reason", "leave",
	}, "join")
	if code != 1 {
		t.Errorf("comms join --reason=leave: expected exit 1, got %d", code)
	}
}

// TestCommsJoin_ReasonUnknownRejected verifies that an unrecognised --reason value
// is rejected immediately (no socket dial). Exit code must be 1.
func TestCommsJoin_ReasonUnknownRejected(t *testing.T) {
	code := runCommsPresenceSubcommand([]string{
		"--name", "charlie",
		"--socket", "/tmp/hkru45u-never-dial.sock",
		"--reason", "bogus",
	}, "join")
	if code != 1 {
		t.Errorf("comms join --reason=bogus: expected exit 1, got %d", code)
	}
}

// TestCommsLeave_ReasonFlagIgnored verifies that --reason has no effect on the
// leave verb: the reason is always "leave".
func TestCommsLeave_ReasonFlagIgnored(t *testing.T) {
	sockPath := "/tmp/hkru45u-leave-reason.sock"
	_ = os.Remove(sockPath)
	t.Cleanup(func() { _ = os.Remove(sockPath) })

	captured, mu := ru45uFixtureStartListener(t, sockPath)

	// --reason flag on leave is silently ignored (leave always uses reason:leave).
	code := runCommsPresenceSubcommand([]string{
		"--name", "dave",
		"--socket", sockPath,
	}, "leave")
	if code != 0 {
		t.Fatalf("comms leave: expected exit 0, got %d", code)
	}

	time.Sleep(10 * time.Millisecond)

	leaveEvents := ru45uCapturedByReason(*captured, mu, "leave")
	if len(leaveEvents) == 0 {
		t.Fatal("comms leave: expected ≥1 agent_presence event with reason=leave, got 0")
	}
}

// ---------------------------------------------------------------------------
// (C) runCommsRecvFollowIO emits a leave beat on context cancellation
// ---------------------------------------------------------------------------

// TestCommsRecvFollow_LeaveBeatOnTeardown verifies that when runCommsRecvFollowIO
// exits (via context cancel), it emits an agent_presence{offline, reason:"leave"}
// beat for the subscribing agent (leave-on-teardown, hk-ru45u).
func TestCommsRecvFollow_LeaveBeatOnTeardown(t *testing.T) {
	sockPath := "/tmp/hkru45u-follow-leave.sock"
	_ = os.Remove(sockPath)
	t.Cleanup(func() { _ = os.Remove(sockPath) })

	captured, mu := ru45uFixtureStartListener(t, sockPath)

	// Shorten the beat interval so the goroutine doesn't sit for 60s.
	origInterval := commsFollowPresenceBeatInterval
	commsFollowPresenceBeatInterval = 5 * time.Second
	t.Cleanup(func() { commsFollowPresenceBeatInterval = origInterval })

	outFile, _ := os.CreateTemp(t.TempDir(), "follow-leave-*.txt")
	t.Cleanup(func() { _ = outFile.Close() })

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		runCommsRecvFollowIO(ctx, sockPath, "leaveme", "", "", "", true, outFile)
	}()

	// Wait a moment for the subscribe connection to establish and the initial
	// subscribe.go refresh to fire, then cancel to trigger teardown.
	time.Sleep(100 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("runCommsRecvFollowIO did not exit within 5s after cancel")
	}

	// The leave beat must have been emitted synchronously before the function returned.
	leaveEvents := ru45uCapturedByReason(*captured, mu, "leave")
	if len(leaveEvents) == 0 {
		t.Fatal("leave-on-teardown: expected ≥1 agent_presence{leave} event after context cancel, got 0")
	}

	var p core.AgentPresencePayload
	if err := json.Unmarshal(leaveEvents[0].Payload, &p); err != nil {
		t.Fatalf("unmarshal leave payload: %v", err)
	}
	if p.Agent != "leaveme" {
		t.Errorf("leave beat agent=%q, want %q", p.Agent, "leaveme")
	}
	if p.Status != core.AgentPresenceStatusOffline {
		t.Errorf("leave beat status=%q, want %q", p.Status, core.AgentPresenceStatusOffline)
	}
	if p.Reason != core.AgentPresenceReasonLeave {
		t.Errorf("leave beat reason=%q, want %q", p.Reason, core.AgentPresenceReasonLeave)
	}
}
