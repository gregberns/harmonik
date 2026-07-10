package daemon

// subscribe_test.go — unit tests for SubscribeHub / subscriptionStream
// (bead hk-6ynv4). Tests cover:
//
//   - back-pressure: slow subscriber doesn't stall the bus; drop-oldest emits
//     subscription_gap with accumulated drop count.
//   - type filter: subscriber requesting [a,b] doesn't receive [c] events.
//   - heartbeat: idle subscription emits heartbeat at configured cadence.
//   - graceful close on client disconnect.
//   - goroutine leak: goleak.VerifyNone after multiple close cycles (hk-6232r).
//   - heartbeat clamp boundaries: 9s and 601s clamped to [10, 600] (hk-6232r).
//   - slow-fast multi-subscriber asymmetry: fast drains, slow drops (hk-6232r).
//   - daemon-shutdown-mid-subscribe: context cancel causes clean return (hk-6232r).

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.uber.org/goleak"

	"github.com/gregberns/harmonik/internal/core"
)

// subscribeTestMakeEvent constructs a minimal core.Event with the given type.
func subscribeTestMakeEvent(t *testing.T, evtType string) core.Event {
	t.Helper()
	evID, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("subscribeTestMakeEvent: uuid: %v", err)
	}
	return core.Event{
		EventID:         core.EventID(evID),
		SchemaVersion:   1,
		Type:            evtType,
		TimestampWall:   time.Now(),
		SourceSubsystem: "test",
		Payload:         json.RawMessage(`{}`),
	}
}

// TestSubscriptionStream_TypeFilter verifies offer() drops events that don't
// match the type filter before consuming a channel slot.
func TestSubscriptionStream_TypeFilter(t *testing.T) {
	t.Parallel()

	s := &subscriptionStream{
		ch:         make(chan core.Event, 4),
		typeFilter: map[string]struct{}{"a": {}, "b": {}},
		wildcard:   false,
	}
	s.offer(subscribeTestMakeEvent(t, "a"))
	s.offer(subscribeTestMakeEvent(t, "c")) // filtered
	s.offer(subscribeTestMakeEvent(t, "b"))
	s.offer(subscribeTestMakeEvent(t, "d")) // filtered

	if got := len(s.ch); got != 2 {
		t.Fatalf("channel depth after filter: got %d, want 2", got)
	}
	got1 := <-s.ch
	got2 := <-s.ch
	if got1.Type != "a" || got2.Type != "b" {
		t.Errorf("got types %q,%q; want a,b", got1.Type, got2.Type)
	}
}

// TestSubscriptionStream_DropOldestBackpressure verifies that when the channel
// fills, offer() drops the OLDEST queued event (not the incoming one) and
// increments the drop counter — the bus dispatch path never blocks.
func TestSubscriptionStream_DropOldestBackpressure(t *testing.T) {
	t.Parallel()

	s := &subscriptionStream{
		ch:       make(chan core.Event, 2),
		wildcard: true,
	}
	// Fill: 2 events in a 2-cap channel.
	e1 := subscribeTestMakeEvent(t, "first")
	e2 := subscribeTestMakeEvent(t, "second")
	s.offer(e1)
	s.offer(e2)
	// Overflow: each subsequent offer drops one oldest and enqueues new.
	e3 := subscribeTestMakeEvent(t, "third")
	e4 := subscribeTestMakeEvent(t, "fourth")

	// Start a goroutine to verify offer() never blocks the producer.
	done := make(chan struct{})
	go func() {
		s.offer(e3)
		s.offer(e4)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("offer() blocked the producer — drop-oldest invariant violated (EV-012)")
	}

	if got := s.dropped.Load(); got != 2 {
		t.Errorf("dropped counter: got %d, want 2", got)
	}
	// Channel should now hold the two newest events.
	g1 := <-s.ch
	g2 := <-s.ch
	if g1.Type != "third" || g2.Type != "fourth" {
		t.Errorf("channel content after drop-oldest: got %q,%q; want third,fourth", g1.Type, g2.Type)
	}

	if swapped := s.swapDropped(); swapped != 2 {
		t.Errorf("swapDropped: got %d, want 2", swapped)
	}
	if reswapped := s.swapDropped(); reswapped != 0 {
		t.Errorf("swapDropped (second call): got %d, want 0 (counter must reset)", reswapped)
	}
}

// TestSubscribeHub_HeartbeatFires verifies the heartbeat line is written
// when the subscription is idle for the configured cadence.
func TestSubscribeHub_HeartbeatFires(t *testing.T) {
	t.Parallel()

	hub := NewSubscribeHub(SubscribeHubConfig{
		Bus: nil, // dispatch path not exercised here
	})

	// Use HeartbeatSeconds=10 (the minimum after clamp) but rely on the
	// clamp-min so test latency is bounded. We override the heartbeat ticker
	// by setting HeartbeatSeconds to a small value pre-clamp; the clamp will
	// raise it to 10s, which exceeds our 5s test timeout, so instead we use
	// a custom code path: invoke makeHeartbeat() directly to validate the
	// heartbeat payload shape, and assert that HandleSubscribe writes it
	// when the timer fires (separate timing-tolerant subtest).

	// Validate heartbeat payload shape directly.
	hb := hub.makeHeartbeat()
	if hb.Type != "heartbeat" {
		t.Errorf("heartbeat type: got %q, want %q", hb.Type, "heartbeat")
	}
	if hb.Timestamp == "" {
		t.Errorf("heartbeat ts is empty")
	}
	if hb.ActiveRuns == nil {
		t.Errorf("heartbeat active_runs must be non-nil slice (even if empty) for JSON consumers")
	}
}

// TestSubscribeHub_HeartbeatTimerFiresOnIdle drives a real HandleSubscribe
// session and asserts at least one heartbeat line arrives during an idle
// window. A fake clock is injected (hk-6232r) so the timer fires after
// ~20ms instead of the real 10s minimum — eliminating CI flakiness.
func TestSubscribeHub_HeartbeatTimerFiresOnIdle(t *testing.T) {
	t.Parallel()

	// fakeTimer fires once after 20ms regardless of the requested duration, then
	// blocks so the test can observe the single heartbeat and terminate.
	fakeTimer := func(_ time.Duration) (<-chan time.Time, func() bool, func(time.Duration)) {
		ch := make(chan time.Time, 1)
		go func() {
			time.Sleep(20 * time.Millisecond)
			ch <- time.Now()
		}()
		stopped := false
		stop := func() bool {
			if stopped {
				return false
			}
			stopped = true
			return true
		}
		reset := func(time.Duration) {} // no-op; test exits after first heartbeat
		return ch, stop, reset
	}

	hub := NewSubscribeHub(SubscribeHubConfig{
		Bus:      nil,
		NewTimer: fakeTimer,
	})

	srv, cli := net.Pipe()
	defer func() { _ = srv.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	done := make(chan struct{})
	go func() {
		hub.HandleSubscribe(ctx, srv, SubscribeRequest{HeartbeatSeconds: 60})
		close(done)
	}()

	// Read until we see a heartbeat line or 500ms timeout.
	rdr := bufio.NewReader(cli)
	deadline := time.Now().Add(500 * time.Millisecond)
	var sawHeartbeat bool
	for time.Now().Before(deadline) {
		_ = cli.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
		line, err := rdr.ReadBytes('\n')
		if err != nil {
			break
		}
		var probe struct {
			Type string `json:"type"`
		}
		if jsonErr := json.Unmarshal(line, &probe); jsonErr != nil {
			continue
		}
		if probe.Type == "heartbeat" {
			sawHeartbeat = true
			break
		}
	}
	_ = cli.Close()
	<-done

	if !sawHeartbeat {
		t.Errorf("no heartbeat observed within 500ms using fake clock; timer injection may be broken")
	}
}

// TestSubscribeHub_GracefulCloseOnClientDisconnect verifies that closing
// the client side of the conn causes HandleSubscribe to return cleanly.
func TestSubscribeHub_GracefulCloseOnClientDisconnect(t *testing.T) {
	t.Parallel()

	hub := NewSubscribeHub(SubscribeHubConfig{Bus: nil})

	srv, cli := net.Pipe()
	defer func() { _ = srv.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	done := make(chan struct{})
	go func() {
		hub.HandleSubscribe(ctx, srv, SubscribeRequest{HeartbeatSeconds: 600})
		close(done)
	}()

	// Wait for hub to register the subscriber.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		hub.mu.RLock()
		n := len(hub.subscribers)
		hub.mu.RUnlock()
		if n == 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Close the client side; the read goroutine in HandleSubscribe should
	// detect EOF and cancel the inner context, causing HandleSubscribe to
	// return.
	_ = cli.Close()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("HandleSubscribe did not return within 3s of client close")
	}

	// Subscriber must be deregistered.
	hub.mu.RLock()
	n := len(hub.subscribers)
	hub.mu.RUnlock()
	if n != 0 {
		t.Errorf("subscriber not deregistered after HandleSubscribe returned; got %d", n)
	}
}

// TestSubscribeHub_DispatchFanOut wires dispatch() against two subscribers
// with overlapping filters and asserts each receives only its matching events.
func TestSubscribeHub_DispatchFanOut(t *testing.T) {
	t.Parallel()

	hub := NewSubscribeHub(SubscribeHubConfig{Bus: nil})

	s1 := &subscriptionStream{ch: make(chan core.Event, 8), typeFilter: map[string]struct{}{"a": {}}}
	s2 := &subscriptionStream{ch: make(chan core.Event, 8), wildcard: true}
	hub.mu.Lock()
	hub.subscribers[s1] = struct{}{}
	hub.subscribers[s2] = struct{}{}
	hub.mu.Unlock()

	// Drive dispatch with three events.
	_ = hub.dispatch(context.Background(), subscribeTestMakeEvent(t, "a"))
	_ = hub.dispatch(context.Background(), subscribeTestMakeEvent(t, "b"))
	_ = hub.dispatch(context.Background(), subscribeTestMakeEvent(t, "a"))

	if got := len(s1.ch); got != 2 {
		t.Errorf("s1 (filter=[a]): got %d events, want 2", got)
	}
	if got := len(s2.ch); got != 3 {
		t.Errorf("s2 (wildcard): got %d events, want 3", got)
	}

	// last_event_id must reflect the most recently dispatched event.
	if hub.loadLastEventID() == "" {
		t.Error("last_event_id should be non-empty after dispatch")
	}
}

// TestSubscribeHub_NoGoroutineLeak_OnMultipleCloseCycles spawns and tears
// down many subscriber sessions and asserts: (a) the hub's subscriber map is
// empty at the end, and (b) no goroutines were leaked (goleak.VerifyNone,
// hk-6232r). Not marked t.Parallel() because goleak snapshots all goroutines
// in the process — running concurrently with other parallel tests produces
// false positives from those tests' goroutines.
func TestSubscribeHub_NoGoroutineLeak_OnMultipleCloseCycles(t *testing.T) {
	hub := NewSubscribeHub(SubscribeHubConfig{Bus: nil})

	var wg sync.WaitGroup
	const N = 20
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			srv, cli := net.Pipe()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			inner := make(chan struct{})
			go func() {
				hub.HandleSubscribe(ctx, srv, SubscribeRequest{HeartbeatSeconds: 600})
				close(inner)
			}()
			// Allow registration to take effect.
			time.Sleep(5 * time.Millisecond)
			_ = cli.Close()
			_ = srv.Close()
			<-inner
		}()
	}
	wg.Wait()

	// (a) subscriber map must be empty.
	hub.mu.RLock()
	n := len(hub.subscribers)
	hub.mu.RUnlock()
	if n != 0 {
		t.Errorf("subscribers map leaked: got %d after all sessions closed", n)
	}

	// (b) goroutine leak check — goleak retries for ~400ms to allow any
	// lingering conn-read goroutines to drain after the pipe closes.
	goleak.VerifyNone(t)
}

// TestSubscribeHub_HeartbeatActiveRunsFromRegistry verifies that the
// heartbeat payload's active_runs array reflects a non-nil ActiveRunsSource.
func TestSubscribeHub_HeartbeatActiveRunsFromRegistry(t *testing.T) {
	t.Parallel()

	reg := NewRunRegistry()
	runID, _ := uuid.NewV7()
	startedAt := time.Now().Add(-30 * time.Second)
	reg.Register(core.RunID(runID), &RunHandle{
		BeadID:    core.BeadID("hk-test-123"),
		StartedAt: startedAt,
	})

	hub := NewSubscribeHub(SubscribeHubConfig{
		Bus:        nil,
		ActiveRuns: reg,
		Now:        func() time.Time { return startedAt.Add(45 * time.Second) },
	})

	hb := hub.makeHeartbeat()
	if len(hb.ActiveRuns) != 1 {
		t.Fatalf("active_runs: got %d entries, want 1", len(hb.ActiveRuns))
	}
	if hb.ActiveRuns[0].BeadID != "hk-test-123" {
		t.Errorf("active_runs[0].bead_id: got %q", hb.ActiveRuns[0].BeadID)
	}
	if hb.ActiveRuns[0].AgeSeconds != 45 {
		t.Errorf("active_runs[0].age_seconds: got %d, want 45", hb.ActiveRuns[0].AgeSeconds)
	}
}

// subscribeTestStartSocketHub starts a socket listener backed by a SubscribeHub
// and returns the socket path. The listener is torn down via t.Cleanup.
func subscribeTestStartSocketHub(t *testing.T, hub *SubscribeHub) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "hk-subscribe-")
	if err != nil {
		t.Fatalf("mkdtemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	sockPath := filepath.Join(dir, "daemon.sock")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = RunSocketListenerWithSubscribe(ctx, sockPath, nil, nil, hub)
	}()
	t.Cleanup(func() {
		cancel()
		<-done
	})

	// Wait for socket to bind.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(sockPath); err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	return sockPath
}

// subscribeTestDial opens a connection and sends a subscribe request.
// Caller must close the returned conn.
func subscribeTestDial(t *testing.T, sockPath string, req map[string]any) (net.Conn, *bufio.Reader) {
	t.Helper()
	conn, err := net.Dial("unix", sockPath)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	reqBytes, _ := json.Marshal(req)
	if _, err := conn.Write(reqBytes); err != nil {
		_ = conn.Close()
		t.Fatalf("write request: %v", err)
	}
	return conn, bufio.NewReader(conn)
}

// TestSubscribe_RejectsMalformedSinceEventID verifies that a subscribe request
// with a non-empty but non-UUID since_event_id is rejected with an error. Valid
// UUIDs are accepted and proceed to replay (tested in TestSubscribe_ReplaySinceEventID).
// Replaces the old TestSubscribe_RejectsSinceEventID test; replay lands via hk-a5sil.
func TestSubscribe_RejectsMalformedSinceEventID(t *testing.T) {
	t.Parallel()

	hub := NewSubscribeHub(SubscribeHubConfig{})
	sockPath := subscribeTestStartSocketHub(t, hub)

	conn, rdr := subscribeTestDial(t, sockPath, map[string]any{
		"op":             "subscribe",
		"since_event_id": "not-a-uuid",
	})
	defer func() { _ = conn.Close() }()

	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	line, err := rdr.ReadBytes('\n')
	if len(line) == 0 && err != nil {
		t.Fatalf("read response: %v", err)
	}

	var resp SocketResponse
	if jsonErr := json.Unmarshal(line, &resp); jsonErr != nil {
		t.Fatalf("decode response %q: %v", string(line), jsonErr)
	}
	if resp.Ok {
		t.Fatalf("subscribe with malformed since_event_id should be rejected; got ok=true")
	}
	if resp.Error == "" {
		t.Fatalf("rejection missing error message")
	}
	if !contains(resp.Error, "since_event_id") {
		t.Errorf("error %q should mention since_event_id", resp.Error)
	}
}

// TestSubscribe_ReplaySinceEventID verifies replay-then-live:
//  1. JSONL is pre-seeded with two events E1 and E2.
//  2. Subscribe with since_event_id=E0 (before E1).
//  3. E1 and E2 are replayed from JSONL first.
//  4. A live event E3 dispatched after the subscriber registers arrives next.
func TestSubscribe_ReplaySinceEventID(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("/tmp", "hk-replay-")
	if err != nil {
		t.Fatalf("mkdtemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	jsonlPath := filepath.Join(dir, "events.jsonl")

	makeEvt := func(evtType string) core.Event {
		id, uuidErr := uuid.NewV7()
		if uuidErr != nil {
			t.Fatalf("uuid: %v", uuidErr)
		}
		time.Sleep(2 * time.Millisecond) // ensure strictly ordered UUIDv7
		return core.Event{
			EventID:         core.EventID(id),
			SchemaVersion:   1,
			Type:            evtType,
			TimestampWall:   time.Now(),
			SourceSubsystem: "test",
			Payload:         json.RawMessage(`{}`),
		}
	}

	e0 := makeEvt("cursor")
	e1 := makeEvt("historical_1")
	e2 := makeEvt("historical_2")

	// Write E1 and E2 to JSONL.
	f, openErr := os.Create(jsonlPath)
	if openErr != nil {
		t.Fatalf("create jsonl: %v", openErr)
	}
	enc := json.NewEncoder(f)
	if err := enc.Encode(e1); err != nil {
		t.Fatalf("encode e1: %v", err)
	}
	if err := enc.Encode(e2); err != nil {
		t.Fatalf("encode e2: %v", err)
	}
	_ = f.Close()

	hub := NewSubscribeHub(SubscribeHubConfig{
		Bus:             nil,
		EventsJSONLPath: jsonlPath,
	})
	sockPath := subscribeTestStartSocketHub(t, hub)

	conn, rdr := subscribeTestDial(t, sockPath, map[string]any{
		"op":                "subscribe",
		"since_event_id":    uuid.UUID(e0.EventID).String(),
		"heartbeat_seconds": 600,
	})
	defer func() { _ = conn.Close() }()

	readEvent := func(label string) core.Event {
		t.Helper()
		_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		line, readErr := rdr.ReadBytes('\n')
		if len(line) == 0 && readErr != nil {
			t.Fatalf("%s: read: %v", label, readErr)
		}
		var ev core.Event
		if jsonErr := json.Unmarshal(line, &ev); jsonErr != nil {
			var errResp SocketResponse
			if json.Unmarshal(line, &errResp) == nil && !errResp.Ok {
				t.Fatalf("%s: error response: %s", label, errResp.Error)
			}
			t.Fatalf("%s: decode: %v (line=%q)", label, jsonErr, string(line))
		}
		return ev
	}

	got1 := readEvent("replayed e1")
	if got1.Type != "historical_1" {
		t.Errorf("replayed[0].type: got %q, want historical_1", got1.Type)
	}
	if got1.EventID != e1.EventID {
		t.Errorf("replayed[0].event_id mismatch")
	}

	got2 := readEvent("replayed e2")
	if got2.Type != "historical_2" {
		t.Errorf("replayed[1].type: got %q, want historical_2", got2.Type)
	}
	if got2.EventID != e2.EventID {
		t.Errorf("replayed[1].event_id mismatch")
	}

	// Send a live event and verify it arrives after replay.
	e3 := makeEvt("live_1")
	hub.dispatch(context.Background(), e3) //nolint:errcheck

	got3 := readEvent("live e3")
	if got3.Type != "live_1" {
		t.Errorf("live[0].type: got %q, want live_1", got3.Type)
	}
}

// TestSubscribe_ReplayTypeFilter verifies the type filter is honoured during
// JSONL replay: only events matching the subscriber's requested types appear.
func TestSubscribe_ReplayTypeFilter(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("/tmp", "hk-replay-filter-")
	if err != nil {
		t.Fatalf("mkdtemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	jsonlPath := filepath.Join(dir, "events.jsonl")

	makeEvt := func(evtType string) core.Event {
		id, _ := uuid.NewV7()
		time.Sleep(time.Millisecond)
		return core.Event{
			EventID:         core.EventID(id),
			SchemaVersion:   1,
			Type:            evtType,
			TimestampWall:   time.Now(),
			SourceSubsystem: "test",
			Payload:         json.RawMessage(`{}`),
		}
	}

	e0 := makeEvt("cursor")
	eA := makeEvt("want")
	eB := makeEvt("skip")
	eC := makeEvt("want")

	f, _ := os.Create(jsonlPath)
	enc := json.NewEncoder(f)
	_ = enc.Encode(eA)
	_ = enc.Encode(eB)
	_ = enc.Encode(eC)
	_ = f.Close()

	hub := NewSubscribeHub(SubscribeHubConfig{
		Bus:             nil,
		EventsJSONLPath: jsonlPath,
	})
	sockPath := subscribeTestStartSocketHub(t, hub)

	conn, rdr := subscribeTestDial(t, sockPath, map[string]any{
		"op":                "subscribe",
		"types":             []string{"want"},
		"since_event_id":    uuid.UUID(e0.EventID).String(),
		"heartbeat_seconds": 600,
	})
	defer func() { _ = conn.Close() }()

	readType := func(label string) string {
		t.Helper()
		_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		line, readErr := rdr.ReadBytes('\n')
		if len(line) == 0 && readErr != nil {
			t.Fatalf("%s: read: %v", label, readErr)
		}
		var ev core.Event
		if jsonErr := json.Unmarshal(line, &ev); jsonErr != nil {
			var errResp SocketResponse
			if json.Unmarshal(line, &errResp) == nil && !errResp.Ok {
				t.Fatalf("%s: error response: %s", label, errResp.Error)
			}
			t.Fatalf("%s: decode: %v (line=%q)", label, jsonErr, string(line))
		}
		return ev.Type
	}

	// eA and eC ("want") should arrive; eB ("skip") should be filtered.
	if got := readType("first"); got != "want" {
		t.Errorf("first replayed type: got %q, want %q", got, "want")
	}
	if got := readType("second"); got != "want" {
		t.Errorf("second replayed type: got %q, want %q", got, "want")
	}
}

// TestSubscribe_ReplayEmptyLog verifies that since_event_id against a
// non-existent JSONL file proceeds cleanly as a live-only stream.
func TestSubscribe_ReplayEmptyLog(t *testing.T) {
	t.Parallel()

	hub := NewSubscribeHub(SubscribeHubConfig{
		Bus:             nil,
		EventsJSONLPath: "/tmp/hk-replay-nonexistent-events-" + t.Name() + ".jsonl",
	})
	sockPath := subscribeTestStartSocketHub(t, hub)

	cursorID, _ := uuid.NewV7()
	conn, rdr := subscribeTestDial(t, sockPath, map[string]any{
		"op":                "subscribe",
		"since_event_id":    cursorID.String(),
		"heartbeat_seconds": 600,
	})
	defer func() { _ = conn.Close() }()

	// Allow HandleSubscribe to register and complete (empty) replay.
	time.Sleep(20 * time.Millisecond)

	// Dispatch a live event — confirms the stream is active after empty replay.
	liveEvt := subscribeTestMakeEvent(t, "live_after_empty_replay")
	hub.dispatch(context.Background(), liveEvt) //nolint:errcheck

	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	line, readErr := rdr.ReadBytes('\n')
	if len(line) == 0 && readErr != nil {
		t.Fatalf("read: %v", readErr)
	}

	// Must NOT be a SocketResponse error (distinguished by non-empty Error field).
	// A core.Event has no "ok" field, so errResp.Ok will be false on unmarshal;
	// we detect a real error response by checking Error != "".
	var errResp SocketResponse
	if jsonErr := json.Unmarshal(line, &errResp); jsonErr == nil && !errResp.Ok && errResp.Error != "" {
		t.Fatalf("unexpected error from replay on missing JSONL: %s", errResp.Error)
	}
	// Live event or heartbeat proves the stream is active after empty replay.
	var hb struct {
		Type string `json:"type"`
	}
	if json.Unmarshal(line, &hb) == nil && (hb.Type != "") {
		// Got a typed line (event or heartbeat) — stream is live.
		return
	}
	t.Errorf("first line is neither a valid event nor heartbeat: %q", string(line))
}

// contains is a tiny substring helper to avoid pulling in strings just for this test.
func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// TestSubscribeHub_CapacityExceeded verifies that once MaxConnections active
// HandleSubscribe goroutines are in flight, the (MaxConnections+1)-th call
// receives a "subscribe_capacity_exceeded" error written to the connection
// and returns immediately without consuming a subscriber slot.
func TestSubscribeHub_CapacityExceeded(t *testing.T) {
	t.Parallel()

	const cap = 3
	hub := NewSubscribeHub(SubscribeHubConfig{
		Bus:            nil,
		MaxConnections: cap,
	})

	// Hold connections open: cap × (srv, cli) pairs where HandleSubscribe blocks.
	type pairHolder struct {
		srv, cli net.Conn
		done     chan struct{}
		cancel   context.CancelFunc
	}
	holders := make([]pairHolder, cap)
	for i := range holders {
		srv, cli := net.Pipe()
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() {
			hub.HandleSubscribe(ctx, srv, SubscribeRequest{HeartbeatSeconds: 600})
			close(done)
		}()
		holders[i] = pairHolder{srv: srv, cli: cli, done: done, cancel: cancel}
	}
	// Wait until all cap slots are registered.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if hub.connCount.Load() == cap {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if got := hub.connCount.Load(); got != cap {
		t.Fatalf("connCount after %d accepted connections: got %d, want %d", cap, got, cap)
	}

	// The (cap+1)-th connect must be rejected immediately.
	srv1, cli1 := net.Pipe()
	rejDone := make(chan struct{})
	go func() {
		hub.HandleSubscribe(context.Background(), srv1, SubscribeRequest{})
		close(rejDone)
	}()

	// Read the error from the client side.
	_ = cli1.SetReadDeadline(time.Now().Add(3 * time.Second))
	rdr := bufio.NewReader(cli1)
	line, err := rdr.ReadBytes('\n')
	// Server writes then returns (srv1 not closed here yet), so EOF after
	// the line is fine; what matters is we got bytes.
	if len(line) == 0 {
		t.Fatalf("expected error line from capacity-exceeded reject; got err=%v", err)
	}
	var resp SocketResponse
	if jsonErr := json.Unmarshal(line, &resp); jsonErr != nil {
		t.Fatalf("decode capacity-exceeded response %q: %v", string(line), jsonErr)
	}
	if resp.Ok {
		t.Fatalf("capacity-exceeded subscribe should be rejected; got ok=true")
	}
	if !contains(resp.Error, "subscribe_capacity_exceeded") {
		t.Errorf("error %q should contain %q", resp.Error, "subscribe_capacity_exceeded")
	}

	// HandleSubscribe for the rejected connection must have returned.
	select {
	case <-rejDone:
	case <-time.After(3 * time.Second):
		t.Fatal("HandleSubscribe did not return after capacity rejection")
	}

	// connCount must not have been incremented for the rejected connection.
	if got := hub.connCount.Load(); got != cap {
		t.Errorf("connCount after rejection: got %d, want %d (cap)", got, cap)
	}

	// Cleanup: cancel all held connections and verify count returns to 0.
	_ = srv1.Close()
	_ = cli1.Close()
	for _, h := range holders {
		h.cancel()
		_ = h.srv.Close()
		_ = h.cli.Close()
		<-h.done
	}

	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if hub.connCount.Load() == 0 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if got := hub.connCount.Load(); got != 0 {
		t.Errorf("connCount after all connections closed: got %d, want 0", got)
	}
}

// ── New tests added by hk-6232r ───────────────────────────────────────────────

// TestSubscribeHub_HeartbeatClampBoundaries verifies that heartbeat_seconds
// values outside the permitted range [10, 600] are clamped to the boundary.
// The test injects a probe timer that records the first requested duration,
// allowing us to confirm the clamp is applied before the timer is created.
func TestSubscribeHub_HeartbeatClampBoundaries(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name         string
		requestedSec int
		wantSec      int
	}{
		{"below-min: 9s clamped to 10s", 9, subscribeHeartbeatMin},
		{"above-max: 601s clamped to 600s", 601, subscribeHeartbeatMax},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var capturedDuration time.Duration
			captureDone := make(chan struct{})

			// Timer factory that records the first call's duration, then fires
			// immediately so HandleSubscribe writes one heartbeat and can be
			// shut down. Subsequent reset calls (after capture) also fire quickly.
			mockTimer := func(d time.Duration) (<-chan time.Time, func() bool, func(time.Duration)) {
				select {
				case <-captureDone:
				default:
					capturedDuration = d
					close(captureDone)
				}
				ch := make(chan time.Time, 1)
				ch <- time.Now() // fire immediately
				return ch, func() bool { return true }, func(time.Duration) {}
			}

			hub := NewSubscribeHub(SubscribeHubConfig{
				Bus:      nil,
				NewTimer: mockTimer,
			})

			srv, cli := net.Pipe()
			defer func() { _ = srv.Close() }()

			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()

			done := make(chan struct{})
			go func() {
				hub.HandleSubscribe(ctx, srv, SubscribeRequest{HeartbeatSeconds: tc.requestedSec})
				close(done)
			}()

			// Wait until the timer factory captures the duration.
			select {
			case <-captureDone:
			case <-ctx.Done():
				t.Fatal("timed out waiting for timer factory to be called")
			}

			_ = cli.Close()
			<-done

			wantDur := time.Duration(tc.wantSec) * time.Second
			if capturedDuration != wantDur {
				t.Errorf("heartbeat interval: requested %ds → got %v, want %v (clamp not applied)",
					tc.requestedSec, capturedDuration, wantDur)
			}
		})
	}
}

// TestSubscribeHub_SlowFastMultiSubscriberAsymmetry verifies that a slow
// subscriber and a fast subscriber coexist without the slow one blocking the
// fast one. Fast consumer receives all events without drops; slow consumer
// (1-slot channel) accumulates drops when the hub dispatches in bulk.
func TestSubscribeHub_SlowFastMultiSubscriberAsymmetry(t *testing.T) {
	t.Parallel()

	hub := NewSubscribeHub(SubscribeHubConfig{Bus: nil})

	// fast: large buffer — should never drop.
	fast := &subscriptionStream{ch: make(chan core.Event, 64), wildcard: true}
	// slow: 1-slot buffer — drops oldest on overflow.
	slow := &subscriptionStream{ch: make(chan core.Event, 1), wildcard: true}

	hub.mu.Lock()
	hub.subscribers[fast] = struct{}{}
	hub.subscribers[slow] = struct{}{}
	hub.mu.Unlock()

	const total = 20
	for i := 0; i < total; i++ {
		_ = hub.dispatch(context.Background(), subscribeTestMakeEvent(t, "ping"))
	}

	// Fast consumer must have received all events without drops.
	fastGot := len(fast.ch)
	fastDropped := fast.dropped.Load()
	if fastGot != total {
		t.Errorf("fast consumer: got %d events, want %d", fastGot, total)
	}
	if fastDropped != 0 {
		t.Errorf("fast consumer: unexpected drops %d", fastDropped)
	}

	// Slow consumer must have dropped some events (channel capacity = 1 < total).
	slowGot := len(slow.ch)
	slowDropped := slow.dropped.Load()
	if slowDropped == 0 {
		t.Errorf("slow consumer (cap=1): expected drops for %d dispatched events, got none (channel=%d)",
			total, slowGot)
	}
	// Items in channel + drop count must account for all events.
	if int64(slowGot)+slowDropped != total {
		t.Errorf("slow consumer: got(%d) + dropped(%d) = %d, want %d",
			slowGot, slowDropped, int64(slowGot)+slowDropped, total)
	}
}

// TestSubscribeHub_DaemonShutdownMidSubscribe verifies that cancelling the
// daemon context (simulating daemon shutdown) causes HandleSubscribe to return
// cleanly, with no subscriber left in the map. Not marked t.Parallel() for
// the same reason as TestSubscribeHub_NoGoroutineLeak_OnMultipleCloseCycles:
// goleak snapshots the full process goroutine set.
func TestSubscribeHub_DaemonShutdownMidSubscribe(t *testing.T) {
	hub := NewSubscribeHub(SubscribeHubConfig{Bus: nil})

	srv, cli := net.Pipe()

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		hub.HandleSubscribe(ctx, srv, SubscribeRequest{HeartbeatSeconds: 600})
		close(done)
	}()

	// Wait for the subscriber to register.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		hub.mu.RLock()
		n := len(hub.subscribers)
		hub.mu.RUnlock()
		if n == 1 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	// Simulate daemon shutdown: cancel the context. HandleSubscribe must return.
	cancel()
	// Close both conn ends so the read-goroutine inside HandleSubscribe unblocks.
	_ = cli.Close()
	_ = srv.Close()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("HandleSubscribe did not return within 2s of daemon context cancellation")
	}

	// Subscriber must be deregistered on shutdown.
	hub.mu.RLock()
	n := len(hub.subscribers)
	hub.mu.RUnlock()
	if n != 0 {
		t.Errorf("subscriber not deregistered after daemon shutdown; got %d", n)
	}

	// Goroutine leak check: after cancel + conn close, all internal goroutines
	// (read-goroutine, heartbeat) must have exited.
	goleak.VerifyNone(t)
}

// TestSubscribeHub_ReapsStalledWriteOnDeadPeer reproduces hk-qsz0p: a
// subscriber whose peer stops reading (dead/cleared session whose socket is
// still open — no read-side EOF ever arrives) must not permanently strand its
// connCount/subscribers slot. A short WriteTimeout forces the stalled write to
// error out, so HandleSubscribe returns and its deferred cleanup releases the
// slot without requiring a daemon restart.
func TestSubscribeHub_ReapsStalledWriteOnDeadPeer(t *testing.T) {
	t.Parallel()

	hub := NewSubscribeHub(SubscribeHubConfig{Bus: nil, WriteTimeout: 50 * time.Millisecond})

	srv, cli := net.Pipe()
	defer func() { _ = cli.Close() }()
	defer func() { _ = srv.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	done := make(chan struct{})
	go func() {
		// Long heartbeat so the reap is driven by the dispatched event's
		// stalled write, not an incidental heartbeat write.
		hub.HandleSubscribe(ctx, srv, SubscribeRequest{HeartbeatSeconds: 600})
		close(done)
	}()

	// Wait for registration.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		hub.mu.RLock()
		n := len(hub.subscribers)
		hub.mu.RUnlock()
		if n == 1 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if got := hub.connCount.Load(); got != 1 {
		t.Fatalf("connCount before dispatch = %d, want 1", got)
	}

	// Deliver an event. The client (cli) never reads it, so the server-side
	// write blocks on net.Pipe's synchronous rendezvous until WriteTimeout
	// fires and errors the write out.
	if err := hub.dispatch(ctx, subscribeTestMakeEvent(t, "run_completed")); err != nil {
		t.Fatalf("dispatch: %v", err)
	}

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("HandleSubscribe did not return within 3s of stalled write timeout")
	}

	hub.mu.RLock()
	n := len(hub.subscribers)
	hub.mu.RUnlock()
	if n != 0 {
		t.Errorf("subscriber not deregistered after stalled-write reap; got %d", n)
	}
	if got := hub.connCount.Load(); got != 0 {
		t.Errorf("connCount not released after stalled-write reap; got %d", got)
	}
}
