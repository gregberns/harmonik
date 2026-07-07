package daemon

// scenario_comms_t2d_backpressure_hkquzth_test.go — T2d L1:
// 256-slot drop-oldest back-pressure + subscription_gap emission
// (bead hk-quzth, codename:comms-test-harness).
//
// Two assertions:
//
//   LIVENESS: when the 256-slot subscribe channel overflows, the drop-oldest
//   mechanism emits subscription_gap{dropped:N} (N > 0) on the live stream
//   before the next delivered event — giving the consumer the forced-resync
//   trigger mandated by EV-038.
//
//   DURABLE-LOG-INTACT: back-pressure drops affect only the in-memory
//   subscribe channel. events.jsonl is append-only and unaffected by channel
//   overflow. ScanAfter(eventsPath, zeroID) returns the FULL history — every
//   event written to the durable log, including those dropped from the stream.
//
// # How back-pressure is triggered (L1 in-process, no real time)
//
// HandleSubscribe runs in a goroutine behind a net.Pipe() fake client that is
// not draining initially. The hub dispatches 400 events synchronously:
//
//   - The hub's dispatch() call fans each event into the subscriber's 256-slot
//     buffered channel via offer() (non-blocking, drop-oldest on overflow).
//   - HandleSubscribe wakes up and tries to write event 1 to the synchronous
//     net.Pipe — BLOCKS (nobody reading cli yet).
//   - While HandleSubscribe is blocked, dispatch() continues: events 2..257 fill
//     the 256-slot channel; events 258..400 each drop the oldest and accumulate
//     the drop counter. Deterministic drops = 400 − 256 = 144.
//   - When the test starts reading cli, HandleSubscribe unblocks, emits
//     subscription_gap{dropped:144}, then delivers the queued events.
//
// Bead: hk-quzth. Design: comms-test-harness §2 L1, G4.
// Spec ref: EV-038 (subscription_gap forced-resync trigger).

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/eventbus"
)

// TestScenario_HkQuzth_T2d_BackpressureDropOldestGap asserts two invariants:
//
//  1. LIVENESS: subscription_gap{dropped:N} (N > 0) appears in the live wire
//     when the 256-slot subscribe channel overflows (EV-038 forced-resync trigger).
//  2. DURABLE-LOG-INTACT: events.jsonl retains every event written to it
//     regardless of channel overflow — drops affect the stream only.
func TestScenario_HkQuzth_T2d_BackpressureDropOldestGap(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("/tmp", "hkquzth-")
	if err != nil {
		t.Fatalf("mkdtemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	eventsPath := filepath.Join(dir, "events.jsonl")

	// ── Fixture: write 400 events to the durable log (events.jsonl) ──────────
	//
	// In production the bus writes every event to events.jsonl regardless of
	// subscriber state. We replicate that here manually. subscribeChannelCapacity
	// is 256; 400 events guarantees ≥ 144 drops (= 400 − 256) regardless of
	// goroutine scheduling, because HandleSubscribe blocks on the first pipe
	// write and cannot drain the channel during dispatch.
	const total = 400
	for i := 0; i < total; i++ {
		writeTestEvent(t, eventsPath, "run_started", map[string]string{"seq": fmt.Sprintf("%d", i)})
	}

	// ── Wire SubscribeHub: hk7n6o7Never timer suppresses heartbeats ───────────
	hub := NewSubscribeHub(SubscribeHubConfig{
		Bus:             nil,
		EventsJSONLPath: eventsPath,
		NewTimer:        hk7n6o7Never,
	})

	// ── Arm subscriber via net.Pipe() fake client (live-only, no replay) ─────
	//
	// No SinceEventID: this is a pure live stream. Using SinceEventID would
	// cause synchronous JSONL replay of all 400 events before the live loop,
	// bypassing the back-pressure scenario entirely.
	srv, cli := net.Pipe()
	ctx, cancel := context.WithCancel(context.Background())
	followDone := make(chan struct{})
	go func() {
		hub.HandleSubscribe(ctx, srv, SubscribeRequest{
			HeartbeatSeconds: 600,
		})
		close(followDone)
	}()
	t.Cleanup(func() {
		cancel()
		_ = srv.Close()
		_ = cli.Close()
		<-followDone
	})

	// ── Wait for subscriber to register ──────────────────────────────────────
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		hub.mu.RLock()
		n := len(hub.subscribers)
		hub.mu.RUnlock()
		if n == 1 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	hub.mu.RLock()
	registeredCount := len(hub.subscribers)
	hub.mu.RUnlock()
	if registeredCount != 1 {
		t.Fatalf("subscriber did not register within 3s")
	}

	// ── Flood: dispatch 400 events synchronously ──────────────────────────────
	//
	// dispatch() → offer() is non-blocking: it uses a select-default loop to
	// drop the oldest slot on overflow. We dispatch synchronously so all 400
	// events are enqueued / dropped before we start reading cli.
	//
	// While we dispatch, HandleSubscribe's goroutine wakes on the first event,
	// tries to write it to the synchronous net.Pipe srv, and BLOCKS — the cli
	// read side has no reader yet. The remaining 399 dispatch calls proceed
	// uncontested: events 2..257 fill the 256-slot channel; events 258..400
	// each drop the channel's oldest, accumulating dropped = 400 − 256 = 144.
	dispatchCtx := context.Background()
	for i := 0; i < total; i++ {
		evID, evErr := uuid.NewV7()
		if evErr != nil {
			t.Fatalf("uuid.NewV7: %v", evErr)
		}
		_ = hub.dispatch(dispatchCtx, core.Event{
			EventID:         core.EventID(evID),
			SchemaVersion:   1,
			Type:            "run_started",
			TimestampWall:   time.Now(),
			SourceSubsystem: "test",
			Payload:         json.RawMessage(`{}`),
		})
	}

	// ── Read wire output: collect subscription_gap and event count ─────────────
	//
	// Starting the read unblocks HandleSubscribe's first pipe write. On the NEXT
	// event read from s.ch, swapDropped() returns the accumulated count and the
	// hub emits subscription_gap{dropped:N} before that event.
	rdr := bufio.NewReader(cli)
	var gapDropped int64
	var gapSeen bool
	var streamEventCount int

	// Collect until we have seen a gap line and at least 10 subsequent events,
	// or until 5 s elapses (should not be reached under normal conditions).
	readDeadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(readDeadline) {
		if gapSeen && streamEventCount >= 10 {
			break
		}
		_ = cli.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
		line, readErr := rdr.ReadBytes('\n')
		if len(line) == 0 {
			if readErr != nil {
				break
			}
			continue
		}
		var probe struct {
			Type    string `json:"type"`
			Dropped int64  `json:"dropped"`
		}
		if jsonErr := json.Unmarshal(line, &probe); jsonErr != nil {
			continue
		}
		switch probe.Type {
		case "subscription_gap":
			gapDropped = probe.Dropped
			gapSeen = true
		case "heartbeat":
			// hk7n6o7Never suppresses these; guard defensively.
		case "":
			// ignore unparseable lines
		default:
			streamEventCount++
		}
	}

	// ── Assertion 1: subscription_gap{dropped:N} was emitted (N > 0) ─────────
	if !gapSeen || gapDropped == 0 {
		t.Errorf("liveness assertion failed: no subscription_gap{dropped:N} (N>0) observed on subscribe wire; "+
			"expected at least 1 drop for %d events through a %d-slot channel (gapSeen=%v, gapDropped=%d)",
			total, subscribeChannelCapacity, gapSeen, gapDropped)
	} else {
		t.Logf("PASS liveness: subscription_gap{dropped:%d} emitted; %d events delivered before read stopped",
			gapDropped, streamEventCount)
	}

	// ── Assertion 2: durable log is intact (EV-038 re-sync path available) ────
	//
	// ScanAfter with the zero UUID as anchor returns all events in events.jsonl.
	// Back-pressure drops do NOT remove events from the append-only log; the
	// full history remains available for the forced-resync path described in
	// EV-038: consumer reads subscription_gap, calls ScanAfter(watermark) on
	// events.jsonl to recover the dropped events.
	zeroID := core.EventID(uuid.UUID{})
	var durableCount int
	for range eventbus.ScanAfter(eventsPath, zeroID) {
		durableCount++
	}
	if durableCount != total {
		t.Errorf("durable-log assertion failed: events.jsonl has %d events, want %d; "+
			"back-pressure drops must not remove events from the append-only log (EV-038 re-sync requires full history)",
			durableCount, total)
	} else {
		t.Logf("PASS durable-log: %d/%d events intact in events.jsonl (channel drops do not affect the log)",
			durableCount, total)
	}
}
