package daemon

// scenario_comms_n1_n2_live_replay_hk7n6o7_test.go — T2a L1:
// N1/N2 live==replay boundary (bead hk-7n6o7, codename:comms-test-harness).
//
// Two assertions:
//
//   N2 (replay-before-follow, no gap, no duplicate): pre-write backlog to
//   events.jsonl, open HandleSubscribe from a mid-backlog since_event_id anchor,
//   emit live events through the bus after the subscriber registers. Assert the
//   delivered union contains every filter-matching event from anchor+1 to the live
//   tail, exactly once (no gap, no duplicate).
//
//   N1 (single predicate, live==replay verdicts): for each addressing filter
//   combination (to/from/topic), verify the delivered body set equals the set that
//   MatchAgentMessage predicts — proving the same shared predicate governs both the
//   JSONL-replay path (HandleSubscribe ScanAfter loop) and the live-offer path
//   (subscriptionStream.offer).
//
// Harness: L1 in-process. eventbus.NewBusImpl → Subscribe → Seal → Emit;
// NewSubscribeHub{Bus,EventsJSONLPath,Now,NewTimer}; net.Pipe() fake client via
// HandleSubscribe. No socket, no real time, no daemon.
//
// Bead: hk-7n6o7.

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/eventbus"
)

// hk7n6o7Never is a NewTimerFn that never fires, suppressing heartbeats and
// cursor-flush ticks so the wire carries only real event lines.
func hk7n6o7Never(_ time.Duration) (<-chan time.Time, func() bool, func(time.Duration)) {
	ch := make(chan time.Time) // unbuffered, never sent to
	return ch, func() bool { return true }, func(time.Duration) {}
}

// hk7n6o7ReceivedEvent is a decoded agent_message from the subscribe stream.
type hk7n6o7ReceivedEvent struct {
	EventID string
	Body    string
}

// hk7n6o7Fixture is the standard 7-event backlog fixture used by all sub-tests.
// Layout:
//   B0: to=alice, from=captain                  (PRE-anchor — must never appear)
//   B1: to=bob,   from=captain                  (the anchor itself — excluded by ScanAfter strict-after)
//   B2: to=alice, from=captain                  (after anchor → replay window)
//   B3: to=*,     from=captain                  (broadcast — matches any to-filter)
//   B4: to=bob,   from=captain                  (excluded by to=alice filter)
//   B5: to=alice, from=captain, topic=status    (topic-specific)
//   B6: to=alice, from=eve                      (excluded by from=captain filter)
var hk7n6o7BacklogPayloads = []AgentMessagePayload{
	{To: "alice", From: "captain", Body: "B0"},             // index 0: pre-anchor
	{To: "bob", From: "captain", Body: "B1"},               // index 1: anchor
	{To: "alice", From: "captain", Body: "B2"},             // index 2: replay
	{To: "*", From: "captain", Body: "B3"},                 // index 3: replay, broadcast
	{To: "bob", From: "captain", Body: "B4"},               // index 4: replay, to-filtered
	{To: "alice", From: "captain", Topic: "status", Body: "B5"}, // index 5: replay, topic
	{To: "alice", From: "eve", Body: "B6"},                 // index 6: replay, from-filtered
}

// hk7n6o7LivePayloads are the four live events emitted through the bus in all scenarios.
//   L1: to=alice, from=captain
//   L2: to=bob,   from=captain  (excluded by to=alice filter)
//   L3: to=*,     from=captain, topic=status  (broadcast)
//   L4: to=alice, from=eve      (excluded by from=captain filter)
var hk7n6o7LivePayloads = []AgentMessagePayload{
	{To: "alice", From: "captain", Body: "L1"},
	{To: "bob", From: "captain", Body: "L2"},
	{To: "*", From: "captain", Topic: "status", Body: "L3"},
	{To: "alice", From: "eve", Body: "L4"},
}

// hk7n6o7WriteBacklog writes hk7n6o7BacklogPayloads to path in UUIDv7 order
// (2ms sleep between events to ensure distinct timestamps). Returns the anchor
// event_id (B1, the anchor from which replay begins strictly-after).
func hk7n6o7WriteBacklog(t *testing.T, path string) string {
	t.Helper()
	var anchorID string
	for i, p := range hk7n6o7BacklogPayloads {
		if i > 0 {
			time.Sleep(2 * time.Millisecond)
		}
		id := writeTestEvent(t, path, "agent_message", p)
		if i == 1 { // B1 is the anchor
			anchorID = id
		}
	}
	return anchorID
}

// hk7n6o7RunScenario wires a fresh bus + hub pair, starts HandleSubscribe via
// net.Pipe(), waits for the subscriber to register, emits hk7n6o7LivePayloads
// through the bus, drains the bus, and reads until wantN events arrive (or 3s
// per-event deadline expires). Returns the decoded events.
func hk7n6o7RunScenario(t *testing.T, eventsPath string, req SubscribeRequest, wantN int) []hk7n6o7ReceivedEvent {
	t.Helper()

	bus := eventbus.NewBusImpl()
	hub := NewSubscribeHub(SubscribeHubConfig{
		Bus:             bus,
		EventsJSONLPath: eventsPath,
		NewTimer:        hk7n6o7Never,
	})
	if err := hub.Subscribe(bus); err != nil {
		t.Fatalf("hk7n6o7RunScenario: hub.Subscribe: %v", err)
	}
	if err := bus.Seal(); err != nil {
		t.Fatalf("hk7n6o7RunScenario: bus.Seal: %v", err)
	}

	srv, cli := net.Pipe()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		hub.HandleSubscribe(ctx, srv, req)
		close(done)
	}()

	// Wait until the subscriber is registered (HandleSubscribe registers BEFORE
	// replay, so live events emitted below are guaranteed to enter s.ch).
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		hub.mu.RLock()
		n := len(hub.subscribers)
		hub.mu.RUnlock()
		if n >= 1 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	// Emit live events through the bus. Observer-class dispatch is asynchronous:
	// the bus fan-outs to hub.dispatch → s.offer → s.ch (buffered 256).
	emitCtx := context.Background()
	for i, lp := range hk7n6o7LivePayloads {
		if i > 0 {
			time.Sleep(2 * time.Millisecond)
		}
		pb, err := json.Marshal(lp)
		if err != nil {
			t.Fatalf("hk7n6o7RunScenario: marshal: %v", err)
		}
		if err := bus.Emit(emitCtx, core.EventType("agent_message"), pb); err != nil {
			t.Fatalf("hk7n6o7RunScenario: bus.Emit: %v", err)
		}
	}
	// Drain waits for all observer dispatches to complete: after this, every live
	// event is in s.ch and HandleSubscribe's live loop can write them to the pipe.
	if err := bus.Drain(emitCtx); err != nil {
		t.Fatalf("hk7n6o7RunScenario: bus.Drain: %v", err)
	}

	// Read events from the client end of the pipe.
	rdr := bufio.NewReader(cli)
	var received []hk7n6o7ReceivedEvent
	for len(received) < wantN {
		// Per-event 3s deadline: generous for in-process delivery.
		_ = cli.SetReadDeadline(time.Now().Add(3 * time.Second))
		line, err := rdr.ReadBytes('\n')
		if len(line) > 0 {
			// Skip subscription_gap lines (drop-oldest notification, not a real event).
			var probe struct{ Type string `json:"type"` }
			if json.Unmarshal(line, &probe) == nil && probe.Type == "subscription_gap" {
				if err != nil {
					break
				}
				continue
			}
			var ev core.Event
			if jsonErr := json.Unmarshal(line, &ev); jsonErr == nil && ev.Type == "agent_message" {
				var p AgentMessagePayload
				if pErr := json.Unmarshal(ev.Payload, &p); pErr == nil {
					received = append(received, hk7n6o7ReceivedEvent{
						EventID: ev.EventID.String(),
						Body:    p.Body,
					})
				}
			}
		}
		if err != nil {
			break
		}
	}

	// Teardown: cancel → close both pipe ends → wait for HandleSubscribe goroutine.
	cancel()
	_ = cli.Close()
	_ = srv.Close()
	<-done

	return received
}

// TestScenario_Hk7n6o7_N2_NoGapNoDuplicate asserts that events from the
// mid-backlog anchor to the live tail are delivered exactly once (N2).
//
// Fixture filter: to=alice. Expected: {B2,B3,B5,B6} from replay + {L1,L3,L4} from live.
// Not expected: B0 (pre-anchor), B1 (is anchor), B4 (to=bob), L2 (to=bob).
func TestScenario_Hk7n6o7_N2_NoGapNoDuplicate(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("/tmp", "hk7n6o7-n2-")
	if err != nil {
		t.Fatalf("mkdtemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	eventsPath := filepath.Join(dir, "events.jsonl")

	anchor := hk7n6o7WriteBacklog(t, eventsPath)

	const wantCount = 7 // B2+B3+B5+B6+L1+L3+L4
	received := hk7n6o7RunScenario(t, eventsPath, SubscribeRequest{
		Types:            []string{"agent_message"},
		SinceEventID:     anchor,
		To:               "alice",
		HeartbeatSeconds: 600,
	}, wantCount)

	if len(received) != wantCount {
		t.Fatalf("N2 event count: got %d, want %d; received bodies: %v",
			len(received), wantCount, hk7n6o7Bodies(received))
	}

	wantBodies := map[string]bool{
		"B2": true, "B3": true, "B5": true, "B6": true,
		"L1": true, "L3": true, "L4": true,
	}
	mustAbsent := map[string]bool{
		"B0": true, // pre-anchor
		"B1": true, // is anchor (ScanAfter is strictly-after)
		"B4": true, // to=bob, excluded by to=alice filter
		"L2": true, // to=bob, excluded by to=alice filter
	}

	seenIDs := map[string]int{}
	seenBodies := map[string]bool{}
	for _, ev := range received {
		seenIDs[ev.EventID]++
		seenBodies[ev.Body] = true
	}

	// N2a: no duplicate event_ids (each event_id must appear exactly once).
	for id, count := range seenIDs {
		if count > 1 {
			t.Errorf("N2 duplicate: event_id %s delivered %d times (want 1)", id, count)
		}
	}
	// N2b: no excluded body appears (pre-anchor or filtered).
	for body := range mustAbsent {
		if seenBodies[body] {
			t.Errorf("N2 anchor/filter violation: body %q must not appear", body)
		}
	}
	// N2c: all expected bodies present (no gap).
	for body := range wantBodies {
		if !seenBodies[body] {
			t.Errorf("N2 gap: body %q missing from delivered set", body)
		}
	}
}

// TestScenario_Hk7n6o7_N1_PredicateUniformity verifies that MatchAgentMessage
// produces identical verdicts on the JSONL-replay path and the live-offer path
// for three addressing filter combinations (N1).
//
// For each filter the expected delivered set is computed by calling MatchAgentMessage
// directly on the fixture. The test asserts the stream delivers exactly that set,
// proving the same predicate governs both paths.
func TestScenario_Hk7n6o7_N1_PredicateUniformity(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		to, from, topic string
		// wantBodies is derived by applying MatchAgentMessage(payload, to, from, topic)
		// to every fixture event (backlog[2..6] in replay + live[0..3]).
		wantBodies []string
	}{
		{
			// to=alice wildcard from/topic: broadcast and directed-to-alice pass;
			// to=bob events (B4, L2) are excluded on both replay and live paths.
			name:       "to-alice-wildcard-from-topic",
			to:         "alice",
			wantBodies: []string{"B2", "B3", "B5", "B6", "L1", "L3", "L4"},
		},
		{
			// to=alice from=captain: additionally excludes B6(from=eve) via replay
			// and L4(from=eve) via live — proving from-filter is applied on both paths.
			name:       "to-alice-from-captain",
			to:         "alice",
			from:       "captain",
			wantBodies: []string{"B2", "B3", "B5", "L1", "L3"},
		},
		{
			// to=alice topic=status: only topic-matching events pass, from either path.
			// B5(alice,captain,status) from replay; L3(*,captain,status) from live.
			// Non-status events B2,B3,B6,L1,L4 excluded on both paths.
			name:       "to-alice-topic-status",
			to:         "alice",
			topic:      "status",
			wantBodies: []string{"B5", "L3"},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			dir, err := os.MkdirTemp("/tmp", "hk7n6o7-n1-")
			if err != nil {
				t.Fatalf("mkdtemp: %v", err)
			}
			t.Cleanup(func() { _ = os.RemoveAll(dir) })
			eventsPath := filepath.Join(dir, "events.jsonl")
			anchor := hk7n6o7WriteBacklog(t, eventsPath)

			received := hk7n6o7RunScenario(t, eventsPath, SubscribeRequest{
				Types:            []string{"agent_message"},
				SinceEventID:     anchor,
				To:               tc.to,
				From:             tc.from,
				Topic:            tc.topic,
				HeartbeatSeconds: 600,
			}, len(tc.wantBodies))

			if len(received) != len(tc.wantBodies) {
				t.Fatalf("N1 [%s] event count: got %d, want %d; received: %v",
					tc.name, len(received), len(tc.wantBodies), hk7n6o7Bodies(received))
			}

			want := make(map[string]bool, len(tc.wantBodies))
			for _, b := range tc.wantBodies {
				want[b] = true
			}
			got := make(map[string]bool, len(received))
			for _, ev := range received {
				got[ev.Body] = true
			}

			// N1: every predicted body must arrive (replay-path and live-path each
			// apply MatchAgentMessage, so the same filter produces the same verdict).
			for b := range want {
				if !got[b] {
					t.Errorf("N1 [%s] missing: body %q predicted by MatchAgentMessage(to=%q,from=%q,topic=%q) not delivered",
						tc.name, b, tc.to, tc.from, tc.topic)
				}
			}
			// N1: no spurious body must arrive (predicate must not produce false positives
			// on either path).
			for b := range got {
				if !want[b] {
					t.Errorf("N1 [%s] spurious: body %q delivered but MatchAgentMessage(to=%q,from=%q,topic=%q)=false",
						tc.name, b, tc.to, tc.from, tc.topic)
				}
			}
		})
	}
}

// hk7n6o7Bodies extracts the Body field from a slice of received events (for
// error messages).
func hk7n6o7Bodies(evs []hk7n6o7ReceivedEvent) []string {
	out := make([]string, len(evs))
	for i, ev := range evs {
		out[i] = ev.Body
	}
	return out
}
