package daemon

// quiesce_test.go — wake-latency and reliability tests for QuiesceArbiter (hk-jeby, Risk 2).
//
// These tests exercise the QuiesceArbiter state machine without a real tmux
// or brAdapter by injecting stubs through the config fields.
//
// Key scenarios covered:
//
//   - DRAINED → park: sleep marker written, park comms sent.
//   - epic_completed event → captain woken within the wake-latency budget.
//   - agent_message{to="captain"} → captain woken.
//   - agent_message{to="crew"} → captain NOT woken (routing isolation).
//   - Max-sleep failsafe: session sleeping beyond the ceiling is auto-woken.
//   - Queue submit wake: crew bound to a queue is woken when items land.
//   - Sleep markers cleaned up after wake.

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/crew"
	"github.com/gregberns/harmonik/internal/eventbus"
	"github.com/gregberns/harmonik/internal/queue"
)

// --- stubs ---

// capturedNudge records SendKeysEnter calls for assertions.
type capturedNudge struct {
	mu      sync.Mutex
	targets []string
}

func (c *capturedNudge) SendKeysEnter(_ context.Context, paneTarget string) error {
	c.mu.Lock()
	c.targets = append(c.targets, paneTarget)
	c.mu.Unlock()
	return nil
}

// awaitNudge blocks until at least n nudges have been captured or the deadline passes.
func (c *capturedNudge) awaitNudge(t *testing.T, n int, deadline time.Duration) {
	t.Helper()
	endAt := time.Now().Add(deadline)
	for {
		c.mu.Lock()
		got := len(c.targets)
		c.mu.Unlock()
		if got >= n {
			return
		}
		if time.Now().After(endAt) {
			c.mu.Lock()
			t.Fatalf("awaitNudge: want ≥%d nudges, got %d after %v; targets=%v", n, len(c.targets), deadline, c.targets)
			c.mu.Unlock()
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// hasTarget checks whether a pane target was nudged.
func (c *capturedNudge) hasTarget(target string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, t := range c.targets {
		if t == target {
			return true
		}
	}
	return false
}

// capturedComms captures EmitAgentMessage calls.
type capturedComms struct {
	mu   sync.Mutex
	msgs []core.AgentMessagePayload
}

func (c *capturedComms) EmitAgentMessage(_ context.Context, p core.AgentMessagePayload) (core.EventID, error) {
	c.mu.Lock()
	c.msgs = append(c.msgs, p)
	c.mu.Unlock()
	return core.EventID{}, nil
}

// hasMsg checks whether a park message to the given agent was captured.
func (c *capturedComms) hasMsg(to, topic string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, m := range c.msgs {
		if m.To == to && m.Topic == topic {
			return true
		}
	}
	return false
}

// --- helpers ---

// newTestQuiesceArbiter creates a QuiesceArbiter wired for unit testing.
// Returns the arbiter, the nudge recorder, and the comms recorder.
func newTestQuiesceArbiter(t *testing.T, projectDir string, qs *QueueStore, drain *DrainDetector, poll, maxSleep time.Duration) (*QuiesceArbiter, *capturedNudge, *capturedComms) {
	t.Helper()
	nudges := &capturedNudge{}
	comms := &capturedComms{}

	arbiter := NewQuiesceArbiter(QuiesceArbiterConfig{
		Drain:            drain,
		ProjectDir:       projectDir,
		ProjectHash:      core.ProjectHash("test0000"),
		Adapter:          nudges, // capturedNudge implements paneNudger
		QueueStore:       qs,
		CommsBus:         comms,
		PollInterval:     poll,
		MaxSleepDuration: maxSleep,
	})
	return arbiter, nudges, comms
}

// mustEmitEpicCompleted emits an epic_completed event directly onto the arbiter's handler.
func mustEmitEpicCompleted(t *testing.T, arbiter *QuiesceArbiter) {
	t.Helper()
	pl := core.EpicCompletedPayload{
		EpicID:          core.BeadID("hk-test-epic"),
		LastChildBeadID: core.BeadID("hk-test-child"),
	}
	payload, err := json.Marshal(pl)
	if err != nil {
		t.Fatalf("marshal epic_completed: %v", err)
	}
	evt := core.Event{
		Type:    string(core.EventTypeEpicCompleted),
		Payload: payload,
	}
	if err := arbiter.handleEpicCompleted(context.Background(), evt); err != nil {
		t.Fatalf("handleEpicCompleted: %v", err)
	}
}

// mustEmitAgentMessage emits an agent_message event directly onto the arbiter's handler.
func mustEmitAgentMessage(t *testing.T, arbiter *QuiesceArbiter, from, to string) {
	t.Helper()
	pl := core.AgentMessagePayload{From: from, To: to, Topic: "test"}
	payload, err := json.Marshal(pl)
	if err != nil {
		t.Fatalf("marshal agent_message: %v", err)
	}
	evt := core.Event{
		Type:    "agent_message",
		Payload: payload,
	}
	if err := arbiter.handleAgentMessage(context.Background(), evt); err != nil {
		t.Fatalf("handleAgentMessage: %v", err)
	}
}

// forceSleepRecord injects a sleep record directly into the arbiter for test setup.
func forceSleepRecord(arbiter *QuiesceArbiter, rec sessionSleepRecord) {
	arbiter.mu.Lock()
	arbiter.sleeping[rec.agentName] = rec
	arbiter.mu.Unlock()
}

// --- tests ---

// TestQuiesceArbiterSubscribeNoError ensures Subscribe returns no error against
// a real eventbus (pre-seal).
func TestQuiesceArbiterSubscribeNoError(t *testing.T) {
	bus := eventbus.NewBusImpl()
	arbiter, _, _ := newTestQuiesceArbiter(t, t.TempDir(), nil, nil, 100*time.Millisecond, time.Hour)

	if err := arbiter.Subscribe(bus); err != nil {
		t.Fatalf("Subscribe: unexpected error: %v", err)
	}
}

// TestQuiesceArbiterEpicCompletedWakesCaptain exercises Risk 4:
// an epic_completed event wakes the captain if it is sleeping.
func TestQuiesceArbiterEpicCompletedWakesCaptain(t *testing.T) {
	captainPane := "harmonik-test0000-captain:0.0"
	arbiter, nudges, _ := newTestQuiesceArbiter(t, t.TempDir(), nil, nil, 5*time.Second, time.Hour)

	// Inject a sleeping captain record.
	forceSleepRecord(arbiter, sessionSleepRecord{
		agentName:  captainAgentName,
		paneTarget: captainPane,
		sessionID:  "test-captain-sess",
		sleptAt:    time.Now(),
	})

	// Emit an epic_completed event.
	mustEmitEpicCompleted(t, arbiter)

	// Start the arbiter's run loop to drain wakeC.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	arbiter.Start(ctx)

	// Wait for the nudge.
	nudges.awaitNudge(t, 1, 2*time.Second)
	if !nudges.hasTarget(captainPane) {
		t.Errorf("captain pane %q was not nudged; all nudged panes: %v", captainPane, nudges.targets)
	}

	// Captain should be removed from sleeping map.
	arbiter.mu.Lock()
	_, still := arbiter.sleeping[captainAgentName]
	arbiter.mu.Unlock()
	if still {
		t.Error("captain still in sleeping map after wake")
	}
}

// TestQuiesceArbiterAgentMessageToCaptainWakes exercises Risk 4 for the comms
// path: a message directed at "captain" wakes the captain.
func TestQuiesceArbiterAgentMessageToCaptainWakes(t *testing.T) {
	captainPane := "harmonik-test0000-captain:0.0"
	arbiter, nudges, _ := newTestQuiesceArbiter(t, t.TempDir(), nil, nil, 5*time.Second, time.Hour)

	forceSleepRecord(arbiter, sessionSleepRecord{
		agentName:  captainAgentName,
		paneTarget: captainPane,
		sessionID:  "test-sess",
		sleptAt:    time.Now(),
	})

	mustEmitAgentMessage(t, arbiter, "paul", captainAgentName)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	arbiter.Start(ctx)

	nudges.awaitNudge(t, 1, 2*time.Second)
	if !nudges.hasTarget(captainPane) {
		t.Errorf("captain pane not nudged; got %v", nudges.targets)
	}
}

// TestQuiesceArbiterAgentMessageToOtherDoesNotWakeCaptain verifies that
// messages directed at a non-captain session do NOT wake the captain (routing isolation).
func TestQuiesceArbiterAgentMessageToOtherDoesNotWakeCaptain(t *testing.T) {
	captainPane := "harmonik-test0000-captain:0.0"
	arbiter, nudges, _ := newTestQuiesceArbiter(t, t.TempDir(), nil, nil, 5*time.Second, time.Hour)

	forceSleepRecord(arbiter, sessionSleepRecord{
		agentName:  captainAgentName,
		paneTarget: captainPane,
		sessionID:  "test-sess",
		sleptAt:    time.Now(),
	})

	// Message to a crew member, NOT to captain.
	mustEmitAgentMessage(t, arbiter, captainAgentName, "paul")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	arbiter.Start(ctx)

	// Give the arbiter time to process the event and check that no nudge fires.
	time.Sleep(200 * time.Millisecond)
	nudges.mu.Lock()
	got := len(nudges.targets)
	nudges.mu.Unlock()
	if got != 0 {
		t.Errorf("expected no nudge for non-captain message; got %d nudges to %v", got, nudges.targets)
	}
}

// TestQuiesceArbiterMaxSleepFailsafe exercises Risk 2:
// a session that has been sleeping beyond maxSleepDuration is auto-woken on tick.
func TestQuiesceArbiterMaxSleepFailsafe(t *testing.T) {
	projectDir := t.TempDir()
	captainPane := "harmonik-test0000-captain:0.0"

	arbiter, nudges, _ := newTestQuiesceArbiter(t, projectDir, nil, nil, 50*time.Millisecond, 100*time.Millisecond)

	// Inject a captain sleep record with sleptAt in the past (already expired).
	forceSleepRecord(arbiter, sessionSleepRecord{
		agentName:  captainAgentName,
		paneTarget: captainPane,
		sessionID:  "test-sess",
		sleptAt:    time.Now().Add(-200 * time.Millisecond), // already past maxSleep of 100ms
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	arbiter.Start(ctx)

	// The first tick should fire within 50 ms (poll interval) and auto-wake the captain.
	nudges.awaitNudge(t, 1, 2*time.Second)
	if !nudges.hasTarget(captainPane) {
		t.Errorf("failsafe did not nudge captain pane %q; got %v", captainPane, nudges.targets)
	}
}

// TestQuiesceArbiterSleepMarkerWrittenAndCleared checks that .sleeping.<session_id>
// is created when a session is parked and removed when it is woken.
func TestQuiesceArbiterSleepMarkerWrittenAndCleared(t *testing.T) {
	projectDir := t.TempDir()
	captainPane := "harmonik-test0000-captain:0.0"
	sessionID := "test-marker-sess"

	arbiter, nudges, _ := newTestQuiesceArbiter(t, projectDir, nil, nil, 5*time.Second, time.Hour)

	// Park the captain directly.
	arbiter.parkSession(context.Background(), captainAgentName, "", sessionID, captainPane, SleepSourceCaptain, SleepLevelDrain)

	// Marker file should exist.
	markerPath := filepath.Join(projectDir, sleepingMarkerDir, ".sleeping."+sessionID)
	if _, err := os.Stat(markerPath); os.IsNotExist(err) {
		t.Fatalf("sleep marker %q not created after parkSession", markerPath)
	}

	// Now force a sleep record (parkSession may have set it, but let's also ensure
	// the map is populated for the wake path).
	forceSleepRecord(arbiter, sessionSleepRecord{
		agentName:  captainAgentName,
		paneTarget: captainPane,
		sessionID:  sessionID,
		sleptAt:    time.Now(),
	})
	mustEmitEpicCompleted(t, arbiter)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	arbiter.Start(ctx)
	nudges.awaitNudge(t, 1, 2*time.Second)

	// Marker file should be gone.
	if _, err := os.Stat(markerPath); !os.IsNotExist(err) {
		t.Errorf("sleep marker %q still exists after wake", markerPath)
	}
}

// TestQuiesceArbiterQueueSubmitWakesCrew exercises Risk 3:
// a queue submission wakes only the crew bound to that queue.
func TestQuiesceArbiterQueueSubmitWakesCrew(t *testing.T) {
	crewPane := "harmonik-test0000-paul:hk-crew-paul.0"
	captainPane := "harmonik-test0000-captain:0.0"

	// Build a QueueStore with a named queue that has a pending item.
	qs := NewQueueStore()
	q := &queue.Queue{
		SchemaVersion: 1,
		Name:          "crew-paul-queue",
		Groups: []queue.Group{
			{
				Items: []queue.Item{
					{BeadID: "hk-test", Status: queue.ItemStatusPending},
				},
			},
		},
	}
	qs.setQueueForTest("crew-paul-queue", q)

	arbiter, nudges, _ := newTestQuiesceArbiter(t, t.TempDir(), qs, nil, 5*time.Second, time.Hour)

	// Inject a sleeping captain and a sleeping crew record.
	forceSleepRecord(arbiter, sessionSleepRecord{
		agentName:  captainAgentName,
		paneTarget: captainPane,
		sessionID:  "cap-sess",
		sleptAt:    time.Now(),
	})
	forceSleepRecord(arbiter, sessionSleepRecord{
		agentName:  "paul",
		queueName:  "crew-paul-queue",
		paneTarget: crewPane,
		sessionID:  "paul-sess",
		sleptAt:    time.Now(),
	})

	// Call handleQueueSubmit directly (crew registry is empty in this unit test;
	// the routing falls back to matching sleeping records by queueName).
	arbiter.handleQueueSubmit(context.Background())

	// Only the crew should be nudged, not the captain.
	if !nudges.hasTarget(crewPane) {
		t.Errorf("crew pane %q not nudged; got %v", crewPane, nudges.targets)
	}
	if nudges.hasTarget(captainPane) {
		t.Errorf("captain pane %q should NOT be nudged by crew queue submit; got %v", captainPane, nudges.targets)
	}
}

// TestQuiesceArbiterDuplicateWakeIsIdempotent ensures that firing two wake
// signals for the same sleeping session does not double-nudge.
func TestQuiesceArbiterDuplicateWakeIsIdempotent(t *testing.T) {
	captainPane := "harmonik-test0000-captain:0.0"
	arbiter, nudges, _ := newTestQuiesceArbiter(t, t.TempDir(), nil, nil, 5*time.Second, time.Hour)

	forceSleepRecord(arbiter, sessionSleepRecord{
		agentName:  captainAgentName,
		paneTarget: captainPane,
		sessionID:  "test-sess",
		sleptAt:    time.Now(),
	})

	// Execute two wake signals for captain.
	arbiter.executeWake(context.Background(), wakeSignal{captainWake: true, reason: "first"})
	arbiter.executeWake(context.Background(), wakeSignal{captainWake: true, reason: "second"})

	nudges.mu.Lock()
	count := len(nudges.targets)
	nudges.mu.Unlock()
	if count != 1 {
		t.Errorf("expected exactly 1 nudge on double wake; got %d; panes=%v", count, nudges.targets)
	}
}

// --- helpers for QueueStore setup without a brAdapter ---

// setQueueForTest inserts a queue directly into the QueueStore for test setup.
// This bypasses the JSON-file round-trip used in production.
func (s *QueueStore) setQueueForTest(name string, q *queue.Queue) {
	s.queueMu.Lock()
	defer s.queueMu.Unlock()
	if s.queues == nil {
		s.queues = make(map[string]*queue.Queue)
	}
	s.queues[name] = q
}

// Test that crew.List with empty projectDir returns empty (no panic).
func TestListCrewRecordsEmptyDir(t *testing.T) {
	arbiter := &QuiesceArbiter{}
	recs := arbiter.listCrewRecords()
	if recs != nil {
		t.Errorf("expected nil crew records for empty projectDir; got %v", recs)
	}
}

// TestQuiesceArbiterParkIdempotent verifies that parking a session twice
// writes only one sleep marker and issues only one comms message.
func TestQuiesceArbiterParkIdempotent(t *testing.T) {
	projectDir := t.TempDir()
	arbiter, _, comms := newTestQuiesceArbiter(t, projectDir, nil, nil, 5*time.Second, time.Hour)

	arbiter.parkSession(context.Background(), captainAgentName, "", "sess-1", "pane:0.0", SleepSourceCaptain, SleepLevelDrain)
	arbiter.parkSession(context.Background(), captainAgentName, "", "sess-1", "pane:0.0", SleepSourceCaptain, SleepLevelDrain)

	comms.mu.Lock()
	n := len(comms.msgs)
	comms.mu.Unlock()
	if n != 1 {
		t.Errorf("expected exactly 1 park comms message; got %d", n)
	}

	arbiter.mu.Lock()
	_, ok := arbiter.sleeping[captainAgentName]
	arbiter.mu.Unlock()
	if !ok {
		t.Error("captain should still be in sleeping map after double-park")
	}
}

// TestQuiesceArbiterCrewRecordIntegration checks that when a real crew.Record
// is available in the registry, parkAllSessions parks the crew session correctly.
// Uses a temporary projectDir but does not require a real tmux server.
func TestQuiesceArbiterCrewRecordIntegration(t *testing.T) {
	projectDir := t.TempDir()

	// Write a minimal crew record so crew.List returns it.
	if err := crew.Write(projectDir, crew.Record{
		Name:      "paul",
		SessionID: "paul-session-123",
		Queue:     "paul-queue",
		Handle:    "harmonik-abc123-paul:hk-crew-paul",
	}); err != nil {
		t.Fatalf("crew.Write: %v", err)
	}

	arbiter, _, comms := newTestQuiesceArbiter(t, projectDir, nil, nil, 5*time.Second, time.Hour)

	// Park all sessions (drain scenario).
	arbiter.parkAllSessions(context.Background(), SleepSourceCaptain, SleepLevelDrain)

	// Paul should be sleeping.
	arbiter.mu.Lock()
	_, paulSleeping := arbiter.sleeping["paul"]
	arbiter.mu.Unlock()
	if !paulSleeping {
		t.Error("paul not sleeping after parkAllSessions")
	}

	// Park comms should have been sent to paul.
	time.Sleep(50 * time.Millisecond)
	if !comms.hasMsg("paul", "park") {
		comms.mu.Lock()
		t.Errorf("no park comms to paul; got msgs: %v", comms.msgs)
		comms.mu.Unlock()
	}

	// Sleep marker file should exist for paul.
	markerPath := filepath.Join(projectDir, sleepingMarkerDir, ".sleeping.paul-session-123")
	if _, err := os.Stat(markerPath); os.IsNotExist(err) {
		t.Errorf("sleep marker for paul not found at %q", markerPath)
	}
}

// TestSleepMarkerSourceLevelWritten verifies that parkSession records the park
// source + level on the on-disk marker (hk-caaf / codename:fleet-state).
func TestSleepMarkerSourceLevelWritten(t *testing.T) {
	projectDir := t.TempDir()
	arbiter, _, _ := newTestQuiesceArbiter(t, projectDir, nil, nil, 5*time.Second, time.Hour)

	arbiter.parkSession(context.Background(), captainAgentName, "", "sess-src", "pane:0.0", SleepSourceOperator, SleepLevelHandoff)

	markerPath := filepath.Join(projectDir, sleepingMarkerDir, ".sleeping.sess-src")
	m, err := arbiter.readSleepMarker(markerPath)
	if err != nil {
		t.Fatalf("readSleepMarker: %v", err)
	}
	if m.Source != SleepSourceOperator {
		t.Errorf("source: got %q want %q", m.Source, SleepSourceOperator)
	}
	if m.Level != SleepLevelHandoff {
		t.Errorf("level: got %q want %q", m.Level, SleepLevelHandoff)
	}
	if m.SessionID != "sess-src" {
		t.Errorf("session_id: got %q want %q", m.SessionID, "sess-src")
	}
}

// TestSleepMarkerBackwardCompatDefaults verifies that a legacy marker carrying
// only session_id + parked_at (written by an older daemon) parses cleanly and
// receives the backward-compatible defaults: operator source, L1 drain level
// (hk-caaf).
func TestSleepMarkerBackwardCompatDefaults(t *testing.T) {
	projectDir := t.TempDir()
	arbiter, _, _ := newTestQuiesceArbiter(t, projectDir, nil, nil, 5*time.Second, time.Hour)

	dir := filepath.Join(projectDir, sleepingMarkerDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	legacy := `{"session_id":"old-sess","parked_at":"2026-06-20T00:00:00Z"}`
	markerPath := filepath.Join(dir, ".sleeping.old-sess")
	if err := os.WriteFile(markerPath, []byte(legacy), 0o644); err != nil {
		t.Fatalf("write legacy marker: %v", err)
	}

	m, err := arbiter.readSleepMarker(markerPath)
	if err != nil {
		t.Fatalf("readSleepMarker on legacy marker: %v", err)
	}
	if m.Source != defaultSleepSource {
		t.Errorf("legacy source default: got %q want %q", m.Source, defaultSleepSource)
	}
	if m.Level != defaultSleepLevel {
		t.Errorf("legacy level default: got %q want %q", m.Level, defaultSleepLevel)
	}
}
