package main

// comms_recv_follow_presencebeat_hkqw63o_test.go — tests for the idle
// `comms recv --follow` presence-beat (B2, bead hk-qw63o).
//
// Problem: idle --follow did not refresh presence, so subscribed-but-quiet
// crews aged out of `comms who` (~120s TTL) and false-flagged as zombies.
// Fix: the --follow loop runs an independent timer that emits a lightweight
// comms-presence refresh beat on its own connection, without disturbing the
// subscribe read path.
//
// This test verifies that runCommsRecvFollowIO, run against a listener that
// supports both "subscribe" and "comms-presence", emits at least one
// agent_presence{reason:"refresh"} event for the agent within a couple of
// beat intervals — using a short interval override so the test runs fast.

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

// presenceBeatFixtureStartListener starts a real daemon socket listener that
// supports both "subscribe" (for the --follow read loop) and "comms-presence"
// (for the periodic beat), backed by an in-memory bus that captures every
// agent_presence event emitted.
func presenceBeatFixtureStartListener(t *testing.T, sockPath string) (*[]core.Event, *sync.Mutex) {
	t.Helper()

	bus := eventbus.NewBusImpl()
	var mu sync.Mutex
	var captured []core.Event

	sub := core.Subscription{
		ConsumerID:    "test-presence-beat-capture",
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
		t.Fatalf("presenceBeatFixtureStartListener: Subscribe: %v", err)
	}
	if err := bus.Seal(); err != nil {
		t.Fatalf("presenceBeatFixtureStartListener: Seal: %v", err)
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
		t.Fatalf("presenceBeatFixtureStartListener: socket %s never became dialable: %v", sockPath, lastDialErr)
	}

	return &captured, &mu
}

// TestCommsRecvFollow_IdlePresenceBeat verifies that an idle --follow loop
// (no messages sent or received) still emits a periodic comms-presence
// refresh beat for its agent, keeping it out of the stale/offline bucket.
func TestCommsRecvFollow_IdlePresenceBeat(t *testing.T) {
	dir := t.TempDir()
	// Short path under /tmp to stay within the 104-byte macOS sun_path limit
	// (struct sockaddr_un), matching the pattern used elsewhere in this package.
	sockPath := "/tmp/hkqw63o-beat.sock"
	_ = os.Remove(sockPath)
	t.Cleanup(func() { _ = os.Remove(sockPath) })

	captured, mu := presenceBeatFixtureStartListener(t, sockPath)

	// Override the beat cadence so the test doesn't wait 60s.
	origInterval := commsFollowPresenceBeatInterval
	commsFollowPresenceBeatInterval = 50 * time.Millisecond
	t.Cleanup(func() { commsFollowPresenceBeatInterval = origInterval })

	outFile, _ := os.CreateTemp(dir, "idle-beat-*.txt")
	t.Cleanup(func() { _ = outFile.Close() })

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		runCommsRecvFollowIO(ctx, sockPath, "idlebot", "", "", "", true, outFile)
	}()
	t.Cleanup(func() { cancel(); <-done })

	// Give the beat goroutine several intervals to fire, then check.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := len(*captured)
		mu.Unlock()
		if n > 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(*captured) == 0 {
		t.Fatal("idle --follow: expected at least one agent_presence refresh beat, got none")
	}

	var found bool
	for _, evt := range *captured {
		var p core.AgentPresencePayload
		if err := json.Unmarshal(evt.Payload, &p); err != nil {
			t.Fatalf("unmarshal agent_presence payload: %v", err)
		}
		if p.Agent != "idlebot" {
			t.Errorf("agent_presence.agent=%q, want %q", p.Agent, "idlebot")
			continue
		}
		if p.Reason != core.AgentPresenceReasonRefresh {
			t.Errorf("agent_presence.reason=%q, want %q", p.Reason, core.AgentPresenceReasonRefresh)
			continue
		}
		if p.Status != core.AgentPresenceStatusOnline {
			t.Errorf("agent_presence.status=%q, want %q", p.Status, core.AgentPresenceStatusOnline)
			continue
		}
		found = true
	}
	if !found {
		t.Fatal("idle --follow: no captured agent_presence event matched agent=idlebot reason=refresh status=online")
	}
}
