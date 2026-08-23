package daemon

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
	"github.com/gregberns/harmonik/internal/keeper"
	"github.com/gregberns/harmonik/internal/queue"
	"github.com/gregberns/harmonik/internal/queuewiring"
)

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

func newTestQuiesceArbiter(t *testing.T, projectDir string, qs *queuewiring.QueueStore, poll, maxSleep time.Duration) (*QuiesceArbiter, *capturedNudge, *capturedComms) {
	t.Helper()
	nudges := &capturedNudge{}
	comms := &capturedComms{}

	arbiter := NewQuiesceArbiter(QuiesceArbiterConfig{
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
		Type:    core.EventTypeEpicCompleted,
		Payload: payload,
	}
	if err := arbiter.handleEpicCompleted(context.Background(), evt); err != nil {
		t.Fatalf("handleEpicCompleted: %v", err)
	}
}

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

func forceSleepRecord(arbiter *QuiesceArbiter, rec sessionSleepRecord) {
	arbiter.mu.Lock()
	arbiter.sleeping[rec.agentName] = rec
	arbiter.mu.Unlock()
}

// TestQuiesceArbiterSubscribeNoError ensures Subscribe returns no error against
// a real eventbus (pre-seal).
func TestQuiesceArbiterSubscribeNoError(t *testing.T) {
	bus := eventbus.NewBusImpl()
	arbiter, _, _ := newTestQuiesceArbiter(t, t.TempDir(), nil, 100*time.Millisecond, time.Hour)

	if err := arbiter.Subscribe(bus); err != nil {
		t.Fatalf("Subscribe: unexpected error: %v", err)
	}
}

// TestQuiesceArbiterEpicCompletedWakesCaptain exercises Risk 4:
// an epic_completed event wakes the captain if it is sleeping.
func TestQuiesceArbiterEpicCompletedWakesCaptain(t *testing.T) {
	captainPane := "harmonik-test0000-captain:0.0"
	arbiter, nudges, _ := newTestQuiesceArbiter(t, t.TempDir(), nil, 5*time.Second, time.Hour)

	forceSleepRecord(arbiter, sessionSleepRecord{
		agentName:  captainAgentName,
		paneTarget: captainPane,
		sessionID:  "test-captain-sess",
		sleptAt:    time.Now(),
	})

	mustEmitEpicCompleted(t, arbiter)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	arbiter.Start(ctx)

	nudges.awaitNudge(t, 1, 2*time.Second)
	if !nudges.hasTarget(captainPane) {
		t.Errorf("captain pane %q was not nudged; all nudged panes: %v", captainPane, nudges.targets)
	}

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
	arbiter, nudges, _ := newTestQuiesceArbiter(t, t.TempDir(), nil, 5*time.Second, time.Hour)

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
	arbiter, nudges, _ := newTestQuiesceArbiter(t, t.TempDir(), nil, 5*time.Second, time.Hour)

	forceSleepRecord(arbiter, sessionSleepRecord{
		agentName:  captainAgentName,
		paneTarget: captainPane,
		sessionID:  "test-sess",
		sleptAt:    time.Now(),
	})

	mustEmitAgentMessage(t, arbiter, captainAgentName, "paul")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	arbiter.Start(ctx)

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

	arbiter, nudges, _ := newTestQuiesceArbiter(t, projectDir, nil, 50*time.Millisecond, 100*time.Millisecond)

	forceSleepRecord(arbiter, sessionSleepRecord{
		agentName:  captainAgentName,
		paneTarget: captainPane,
		sessionID:  "test-sess",
		sleptAt:    time.Now().Add(-200 * time.Millisecond), // already past maxSleep of 100ms
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	arbiter.Start(ctx)

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

	arbiter, nudges, _ := newTestQuiesceArbiter(t, projectDir, nil, 5*time.Second, time.Hour)

	arbiter.parkSession(context.Background(), captainAgentName, "", sessionID, captainPane, SleepSourceCaptain, SleepLevelDrain)

	markerPath := filepath.Join(projectDir, sleepingMarkerDir, ".sleeping."+sessionID)
	if _, err := os.Stat(markerPath); os.IsNotExist(err) {
		t.Fatalf("sleep marker %q not created after parkSession", markerPath)
	}

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

	if _, err := os.Stat(markerPath); !os.IsNotExist(err) {
		t.Errorf("sleep marker %q still exists after wake", markerPath)
	}
}

// TestQuiesceArbiterQueueSubmitWakesCrew exercises Risk 3:
// a queue submission wakes only the crew bound to that queue.
func TestQuiesceArbiterQueueSubmitWakesCrew(t *testing.T) {
	crewPane := "harmonik-test0000-paul:hk-crew-paul.0"
	captainPane := "harmonik-test0000-captain:0.0"

	qs := queuewiring.NewQueueStore()
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
	qs.SetQueueByName("crew-paul-queue", q)

	arbiter, nudges, _ := newTestQuiesceArbiter(t, t.TempDir(), qs, 5*time.Second, time.Hour)

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

	arbiter.handleQueueSubmit(context.Background())

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
	arbiter, nudges, _ := newTestQuiesceArbiter(t, t.TempDir(), nil, 5*time.Second, time.Hour)

	forceSleepRecord(arbiter, sessionSleepRecord{
		agentName:  captainAgentName,
		paneTarget: captainPane,
		sessionID:  "test-sess",
		sleptAt:    time.Now(),
	})

	arbiter.executeWake(context.Background(), wakeSignal{captainWake: true, reason: "first"})
	arbiter.executeWake(context.Background(), wakeSignal{captainWake: true, reason: "second"})

	nudges.mu.Lock()
	count := len(nudges.targets)
	nudges.mu.Unlock()
	if count != 1 {
		t.Errorf("expected exactly 1 nudge on double wake; got %d; panes=%v", count, nudges.targets)
	}
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
	arbiter, _, comms := newTestQuiesceArbiter(t, projectDir, nil, 5*time.Second, time.Hour)

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

	if err := crew.Write(projectDir, crew.Record{
		Name:      "paul",
		SessionID: "paul-session-123",
		Queue:     "paul-queue",
		Handle:    "harmonik-abc123-paul:hk-crew-paul",
	}); err != nil {
		t.Fatalf("crew.Write: %v", err)
	}

	arbiter, _, comms := newTestQuiesceArbiter(t, projectDir, nil, 5*time.Second, time.Hour)

	arbiter.parkAllSessions(context.Background(), SleepSourceCaptain, SleepLevelDrain)

	arbiter.mu.Lock()
	_, paulSleeping := arbiter.sleeping["paul"]
	arbiter.mu.Unlock()
	if !paulSleeping {
		t.Error("paul not sleeping after parkAllSessions")
	}

	time.Sleep(50 * time.Millisecond)
	if !comms.hasMsg("paul", "park") {
		comms.mu.Lock()
		t.Errorf("no park comms to paul; got msgs: %v", comms.msgs)
		comms.mu.Unlock()
	}

	markerPath := filepath.Join(projectDir, sleepingMarkerDir, ".sleeping.paul-session-123")
	if _, err := os.Stat(markerPath); os.IsNotExist(err) {
		t.Errorf("sleep marker for paul not found at %q", markerPath)
	}
}

// TestReconcileOrphanedMarkers verifies that a daemon restart re-loads orphaned
// on-disk .sleeping.* markers into the in-memory map, preserving the ORIGINAL
// parked_at as sleptAt so the max-sleep failsafe measures from the real park
// time (hk-x03v / codename:fleet-state).
func TestReconcileOrphanedMarkers(t *testing.T) {
	projectDir := t.TempDir()

	if err := crew.Write(projectDir, crew.Record{
		Name:      "paul",
		SessionID: "paul-orphan-sid",
		Queue:     "paul-queue",
		Handle:    "harmonik-abc123-paul:hk-crew-paul",
	}); err != nil {
		t.Fatalf("crew.Write: %v", err)
	}
	dir := filepath.Join(projectDir, sleepingMarkerDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	parkedAt := time.Now().Add(-3 * time.Hour).UTC().Format(time.RFC3339)
	markerBody := `{"session_id":"paul-orphan-sid","parked_at":"` + parkedAt + `","source":"operator","level":"L2"}`
	markerPath := filepath.Join(dir, ".sleeping.paul-orphan-sid")
	if err := os.WriteFile(markerPath, []byte(markerBody), 0o644); err != nil {
		t.Fatalf("write orphan marker: %v", err)
	}

	arbiter, _, _ := newTestQuiesceArbiter(t, projectDir, nil, 5*time.Second, time.Hour)
	arbiter.reconcileOrphanedMarkers(t.Context())

	arbiter.mu.Lock()
	rec, ok := arbiter.sleeping["paul"]
	arbiter.mu.Unlock()
	if !ok {
		t.Fatal("reconcile did not re-load the orphaned crew marker into the sleeping map")
	}
	if rec.sessionID != "paul-orphan-sid" {
		t.Errorf("sessionID: got %q want %q", rec.sessionID, "paul-orphan-sid")
	}
	if rec.queueName != "paul-queue" {
		t.Errorf("queueName: got %q want %q", rec.queueName, "paul-queue")
	}
	if rec.paneTarget != "harmonik-abc123-paul:hk-crew-paul.0" {
		t.Errorf("paneTarget: got %q", rec.paneTarget)
	}
	if rec.source != SleepSourceOperator || rec.level != SleepLevelHandoff {
		t.Errorf("source/level: got %q/%q want operator/L2", rec.source, rec.level)
	}
	if time.Since(rec.sleptAt) < 2*time.Hour {
		t.Errorf("sleptAt not seeded from parked_at; since=%v (want ~3h)", time.Since(rec.sleptAt))
	}
}

// TestReconcileOrphanedMarkersFailsafeWakes verifies that an orphaned marker
// re-loaded at boot is then woken by the max-sleep failsafe (hk-x03v): a marker
// parked past the ceiling is nudged on the first tick after restart, and its
// on-disk file is removed — lifting the indefinite keeper suppression.
func TestReconcileOrphanedMarkersFailsafeWakes(t *testing.T) {
	projectDir := t.TempDir()
	if err := crew.Write(projectDir, crew.Record{
		Name:      "paul",
		SessionID: "paul-orphan-sid",
		Queue:     "paul-queue",
		Handle:    "harmonik-abc123-paul:hk-crew-paul",
	}); err != nil {
		t.Fatalf("crew.Write: %v", err)
	}
	dir := filepath.Join(projectDir, sleepingMarkerDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	parkedAt := time.Now().Add(-time.Minute).UTC().Format(time.RFC3339)
	markerPath := filepath.Join(dir, ".sleeping.paul-orphan-sid")
	if err := os.WriteFile(markerPath,
		[]byte(`{"session_id":"paul-orphan-sid","parked_at":"`+parkedAt+`"}`), 0o644); err != nil {
		t.Fatalf("write orphan marker: %v", err)
	}

	arbiter, nudges, _ := newTestQuiesceArbiter(t, projectDir, nil, 50*time.Millisecond, 100*time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	arbiter.Start(ctx) // runs reconcile, then the loop with the short failsafe

	nudges.awaitNudge(t, 1, 2*time.Second)
	if !nudges.hasTarget("harmonik-abc123-paul:hk-crew-paul.0") {
		t.Errorf("failsafe did not nudge the reconciled crew pane; got %v", nudges.targets)
	}
	if _, err := os.Stat(markerPath); !os.IsNotExist(err) {
		t.Errorf("orphan marker %q still present after failsafe wake", markerPath)
	}
}

// TestResolveCaptainTargetLastResort verifies the resolution fallback chain
// (hk-fv40): with no live tmux session, resolveCaptainTarget returns the
// convention-derived "<session>:agent" target (never the old hard-coded
// "<session>:0.0"), so the failsafe still has a plausible target.
func TestResolveCaptainTargetLastResort(t *testing.T) {
	projectDir := t.TempDir()
	arbiter, _, _ := newTestQuiesceArbiter(t, projectDir, nil, 5*time.Second, time.Hour)

	if tmuxHasSession(t.Context(), captainAgentName) {
		t.Skip("a live bare 'captain' tmux session exists; last-resort path not exercised")
	}

	got := arbiter.resolveCaptainTarget(t.Context())

	want := keeper.HarmonikSessionName(projectDir, captainAgentName) + ":agent"
	if got != want {
		t.Errorf("resolveCaptainTarget last-resort: got %q want %q", got, want)
	}
	if got == keeper.HarmonikSessionName(projectDir, captainAgentName)+":0.0" {
		t.Error("resolveCaptainTarget still produced the old hard-coded :0.0 target")
	}
}

// TestSleepMarkerSourceLevelWritten verifies that parkSession records the park
// source + level on the on-disk marker (hk-caaf / codename:fleet-state).
func TestSleepMarkerSourceLevelWritten(t *testing.T) {
	projectDir := t.TempDir()
	arbiter, _, _ := newTestQuiesceArbiter(t, projectDir, nil, 5*time.Second, time.Hour)

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

// TestQuiesceArbiterAgentMessageToWatchWakesWatch is the WE5 RED→GREEN test:
// a parked watch session (a.sleeping["watch"]) must be woken when an agent_message
// with To=="watch" arrives.  The existing captain-wake path must remain unchanged.
func TestQuiesceArbiterAgentMessageToWatchWakesWatch(t *testing.T) {
	watchPane := "harmonik-test0000-watch:hk-crew-watch.0"
	captainPane := "harmonik-test0000-captain:0.0"
	arbiter, nudges, _ := newTestQuiesceArbiter(t, t.TempDir(), nil, 5*time.Second, time.Hour)

	forceSleepRecord(arbiter, sessionSleepRecord{
		agentName:  watchAgentName,
		paneTarget: watchPane,
		sessionID:  "watch-sess",
		sleptAt:    time.Now(),
	})
	forceSleepRecord(arbiter, sessionSleepRecord{
		agentName:  captainAgentName,
		paneTarget: captainPane,
		sessionID:  "captain-sess",
		sleptAt:    time.Now(),
	})

	mustEmitAgentMessage(t, arbiter, "paul", watchAgentName)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	arbiter.Start(ctx)

	nudges.awaitNudge(t, 1, 2*time.Second)

	if !nudges.hasTarget(watchPane) {
		t.Errorf("watch pane %q not nudged; got %v", watchPane, nudges.targets)
	}
	if nudges.hasTarget(captainPane) {
		t.Errorf("captain pane %q should NOT be nudged by a message to watch; got %v", captainPane, nudges.targets)
	}

	arbiter.mu.Lock()
	_, watchStillSleeping := arbiter.sleeping[watchAgentName]
	_, captainStillSleeping := arbiter.sleeping[captainAgentName]
	arbiter.mu.Unlock()
	if watchStillSleeping {
		t.Error("watch still in sleeping map after wake")
	}
	if !captainStillSleeping {
		t.Error("captain should still be sleeping after a watch-directed message")
	}
}

// TestQuiesceArbiterAgentMessageToWatchDoesNotWakeCaptain verifies the
// routing-isolation invariant: a message to "watch" must not wake the captain.
func TestQuiesceArbiterAgentMessageToWatchDoesNotWakeCaptain(t *testing.T) {
	captainPane := "harmonik-test0000-captain:0.0"
	arbiter, nudges, _ := newTestQuiesceArbiter(t, t.TempDir(), nil, 5*time.Second, time.Hour)

	forceSleepRecord(arbiter, sessionSleepRecord{
		agentName:  captainAgentName,
		paneTarget: captainPane,
		sessionID:  "captain-sess",
		sleptAt:    time.Now(),
	})

	mustEmitAgentMessage(t, arbiter, "paul", watchAgentName)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	arbiter.Start(ctx)

	time.Sleep(200 * time.Millisecond)
	nudges.mu.Lock()
	got := len(nudges.targets)
	nudges.mu.Unlock()
	if got != 0 {
		t.Errorf("captain must not be woken by a message to watch; got %d nudge(s) to %v", got, nudges.targets)
	}
}

// TestSleepMarkerBackwardCompatDefaults verifies that a legacy marker carrying
// only session_id + parked_at (written by an older daemon) parses cleanly and
// receives the backward-compatible defaults: operator source, L1 drain level
// (hk-caaf).
func TestSleepMarkerBackwardCompatDefaults(t *testing.T) {
	projectDir := t.TempDir()
	arbiter, _, _ := newTestQuiesceArbiter(t, projectDir, nil, 5*time.Second, time.Hour)

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
