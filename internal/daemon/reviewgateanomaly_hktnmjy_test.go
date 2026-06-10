package daemon_test

// reviewgateanomaly_hktnmjy_test.go — unit tests for ReviewGateAnomalyWatcher
// (hk-tnmjy).
//
// These tests verify:
//  1. No alarm fires when consecutive_count < threshold.
//  2. Alarm fires when consecutive_count == threshold (default=3).
//  3. reviewer_verdict resets the counter; subsequent bead_closed events
//     re-arm the watcher from zero.
//  4. After the alarm fires the watcher resets and re-arms for the next batch.
//  5. ReviewGateAnomalyPayload.Valid() accepts well-formed payloads and rejects
//     malformed ones.
//
// Test infrastructure: in-process fake bus that records emitted events.
//
// Bead ref: hk-tnmjy.

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
)

// ---------------------------------------------------------------------------
// ReviewGateAnomalyPayload.Valid() tests
// ---------------------------------------------------------------------------

func TestReviewGateAnomalyPayload_ValidAcceptsWellFormed(t *testing.T) {
	t.Parallel()

	p := core.ReviewGateAnomalyPayload{
		ConsecutiveCount: 3,
		Threshold:        3,
		BeadIDs:          []string{"hk-aaa", "hk-bbb", "hk-ccc"},
		DetectedAt:       time.Now().UTC().Format(time.RFC3339),
	}
	if !p.Valid() {
		t.Error("ReviewGateAnomalyPayload: well-formed payload must be Valid()")
	}
}

func TestReviewGateAnomalyPayload_ValidRejectsZeroConsecutiveCount(t *testing.T) {
	t.Parallel()

	p := core.ReviewGateAnomalyPayload{
		ConsecutiveCount: 0,
		Threshold:        3,
		BeadIDs:          []string{},
		DetectedAt:       time.Now().UTC().Format(time.RFC3339),
	}
	if p.Valid() {
		t.Error("ReviewGateAnomalyPayload: ConsecutiveCount=0 must NOT be Valid()")
	}
}

func TestReviewGateAnomalyPayload_ValidRejectsZeroThreshold(t *testing.T) {
	t.Parallel()

	p := core.ReviewGateAnomalyPayload{
		ConsecutiveCount: 1,
		Threshold:        0,
		BeadIDs:          []string{"hk-aaa"},
		DetectedAt:       time.Now().UTC().Format(time.RFC3339),
	}
	if p.Valid() {
		t.Error("ReviewGateAnomalyPayload: Threshold=0 must NOT be Valid()")
	}
}

func TestReviewGateAnomalyPayload_ValidRejectsNilBeadIDs(t *testing.T) {
	t.Parallel()

	p := core.ReviewGateAnomalyPayload{
		ConsecutiveCount: 1,
		Threshold:        3,
		BeadIDs:          nil,
		DetectedAt:       time.Now().UTC().Format(time.RFC3339),
	}
	if p.Valid() {
		t.Error("ReviewGateAnomalyPayload: nil BeadIDs must NOT be Valid()")
	}
}

func TestReviewGateAnomalyPayload_ValidRejectsBeadIDsLengthMismatch(t *testing.T) {
	t.Parallel()

	p := core.ReviewGateAnomalyPayload{
		ConsecutiveCount: 3,
		Threshold:        3,
		BeadIDs:          []string{"hk-aaa", "hk-bbb"}, // length 2, not 3
		DetectedAt:       time.Now().UTC().Format(time.RFC3339),
	}
	if p.Valid() {
		t.Error("ReviewGateAnomalyPayload: BeadIDs length != ConsecutiveCount must NOT be Valid()")
	}
}

func TestReviewGateAnomalyPayload_ValidRejectsEmptyDetectedAt(t *testing.T) {
	t.Parallel()

	p := core.ReviewGateAnomalyPayload{
		ConsecutiveCount: 1,
		Threshold:        3,
		BeadIDs:          []string{"hk-aaa"},
		DetectedAt:       "",
	}
	if p.Valid() {
		t.Error("ReviewGateAnomalyPayload: empty DetectedAt must NOT be Valid()")
	}
}

// ---------------------------------------------------------------------------
// ReviewGateAnomalyWatcher behavioural tests
// ---------------------------------------------------------------------------

// reviewGateAnomalyFakeBus is a minimal in-process fake bus for testing the
// watcher.  It records emitted events and allows synchronous delivery to
// registered consumers.
type reviewGateAnomalyFakeBus struct {
	mu            sync.Mutex
	subscriptions []core.Subscription
	emitted       []reviewGateAnomalyEmittedEvent
}

type reviewGateAnomalyEmittedEvent struct {
	EventType core.EventType
	Payload   []byte
}

func (b *reviewGateAnomalyFakeBus) Subscribe(sub core.Subscription) (core.Subscription, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.subscriptions = append(b.subscriptions, sub)
	return sub, nil
}

func (b *reviewGateAnomalyFakeBus) Emit(ctx context.Context, eventType core.EventType, payload []byte) error {
	b.mu.Lock()
	b.emitted = append(b.emitted, reviewGateAnomalyEmittedEvent{EventType: eventType, Payload: payload})
	subs := make([]core.Subscription, len(b.subscriptions))
	copy(subs, b.subscriptions)
	b.mu.Unlock()

	for _, sub := range subs {
		if _, ok := sub.EventPattern.Types[string(eventType)]; ok {
			evt := core.Event{
				EventID: core.EventID(uuid.New()),
				Type:    string(eventType),
				Payload: payload,
			}
			if err := sub.Handler(ctx, evt); err != nil {
				return err
			}
		}
	}
	return nil
}

func (b *reviewGateAnomalyFakeBus) EmitWithRunID(_ context.Context, _ core.RunID, eventType core.EventType, payload []byte) error {
	return b.Emit(context.Background(), eventType, payload)
}

func (b *reviewGateAnomalyFakeBus) Seal() error                                           { return nil }
func (b *reviewGateAnomalyFakeBus) ReplayFrom(_ string, _ core.EventID) error             { return nil }
func (b *reviewGateAnomalyFakeBus) DeadLetterReplay(_ string, _ *core.EventPattern) error { return nil }
func (b *reviewGateAnomalyFakeBus) Drain(_ context.Context) error                         { return nil }

// emittedAnomalyCount returns the number of review_gate_anomaly events recorded.
func (b *reviewGateAnomalyFakeBus) emittedAnomalyCount() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := 0
	for _, e := range b.emitted {
		if e.EventType == core.EventTypeReviewGateAnomaly {
			n++
		}
	}
	return n
}

// lastAnomalyPayload returns the payload of the most recent review_gate_anomaly.
func (b *reviewGateAnomalyFakeBus) lastAnomalyPayload(t *testing.T) core.ReviewGateAnomalyPayload {
	t.Helper()
	b.mu.Lock()
	defer b.mu.Unlock()
	for i := len(b.emitted) - 1; i >= 0; i-- {
		if b.emitted[i].EventType == core.EventTypeReviewGateAnomaly {
			var p core.ReviewGateAnomalyPayload
			if err := json.Unmarshal(b.emitted[i].Payload, &p); err != nil {
				t.Fatalf("failed to unmarshal review_gate_anomaly payload: %v", err)
			}
			return p
		}
	}
	t.Fatal("no review_gate_anomaly event found")
	return core.ReviewGateAnomalyPayload{}
}

// sendBeadClosed delivers a synthetic bead_closed event directly to subscribed consumers.
func sendBeadClosed(t *testing.T, bus *reviewGateAnomalyFakeBus, beadID string) {
	t.Helper()
	runID := core.RunID(uuid.New())
	p := core.BeadClosedPayload{RunID: runID, BeadID: core.BeadID(beadID)}
	raw, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("marshal bead_closed payload: %v", err)
	}
	if err := bus.Emit(context.Background(), core.EventTypeBeadClosed, raw); err != nil {
		t.Fatalf("emit bead_closed: %v", err)
	}
}

// sendReviewerVerdict delivers a synthetic reviewer_verdict event.
func sendReviewerVerdict(t *testing.T, bus *reviewGateAnomalyFakeBus) {
	t.Helper()
	runID := core.RunID(uuid.New())
	p := core.ReviewerVerdictPayload{
		RunID:           runID,
		WorkflowMode:    core.WorkflowModeReviewLoop,
		SessionID:       "sess-test",
		ClaudeSessionID: "claude-test",
		IterationCount:  1,
		SchemaVersion:   1,
		Verdict:         core.ReviewerVerdictApprove,
		Flags:           []string{},
		Notes:           "looks good",
	}
	raw, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("marshal reviewer_verdict payload: %v", err)
	}
	if err := bus.Emit(context.Background(), core.EventTypeReviewerVerdict, raw); err != nil {
		t.Fatalf("emit reviewer_verdict: %v", err)
	}
}

// newWatcherWithFakeBus constructs a watcher with a custom threshold wired to
// the provided fake bus.  The watcher's Subscribe is called on the same bus.
func newWatcherWithThreshold(t *testing.T, threshold int) (*daemon.ReviewGateAnomalyWatcher, *reviewGateAnomalyFakeBus) {
	t.Helper()
	fb := &reviewGateAnomalyFakeBus{}
	w := daemon.NewReviewGateAnomalyWatcherWithThreshold(fb, threshold)
	if err := w.Subscribe(fb); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	return w, fb
}

// TestReviewGateAnomalyWatcher_NoAlarmBelowThreshold verifies that the alarm
// does NOT fire when consecutive_count < threshold.
func TestReviewGateAnomalyWatcher_NoAlarmBelowThreshold(t *testing.T) {
	t.Parallel()

	_, fb := newWatcherWithThreshold(t, 3)

	// Send 2 bead_closed events (threshold is 3).
	sendBeadClosed(t, fb, "hk-aaa")
	sendBeadClosed(t, fb, "hk-bbb")

	if n := fb.emittedAnomalyCount(); n != 0 {
		t.Errorf("expected 0 review_gate_anomaly events, got %d", n)
	}
}

// TestReviewGateAnomalyWatcher_AlarmFiresAtThreshold verifies the alarm fires
// when exactly N consecutive bead_closed events occur with no reviewer_verdict.
func TestReviewGateAnomalyWatcher_AlarmFiresAtThreshold(t *testing.T) {
	t.Parallel()

	_, fb := newWatcherWithThreshold(t, 3)

	sendBeadClosed(t, fb, "hk-aaa")
	sendBeadClosed(t, fb, "hk-bbb")
	sendBeadClosed(t, fb, "hk-ccc")

	if n := fb.emittedAnomalyCount(); n != 1 {
		t.Errorf("expected 1 review_gate_anomaly event, got %d", n)
	}

	payload := fb.lastAnomalyPayload(t)
	if !payload.Valid() {
		t.Error("emitted review_gate_anomaly payload must be Valid()")
	}
	if payload.ConsecutiveCount != 3 {
		t.Errorf("ConsecutiveCount: got %d, want 3", payload.ConsecutiveCount)
	}
	if payload.Threshold != 3 {
		t.Errorf("Threshold: got %d, want 3", payload.Threshold)
	}
	if len(payload.BeadIDs) != 3 {
		t.Errorf("BeadIDs length: got %d, want 3", len(payload.BeadIDs))
	}
	wantBeads := []string{"hk-aaa", "hk-bbb", "hk-ccc"}
	for i, want := range wantBeads {
		if payload.BeadIDs[i] != want {
			t.Errorf("BeadIDs[%d]: got %q, want %q", i, payload.BeadIDs[i], want)
		}
	}
	if payload.DetectedAt == "" {
		t.Error("DetectedAt must not be empty")
	}
}

// TestReviewGateAnomalyWatcher_VerdictResetsCounter verifies that a
// reviewer_verdict resets the counter, preventing a spurious alarm.
func TestReviewGateAnomalyWatcher_VerdictResetsCounter(t *testing.T) {
	t.Parallel()

	_, fb := newWatcherWithThreshold(t, 3)

	// Two closes, then a verdict, then one more close: total < threshold since verdict.
	sendBeadClosed(t, fb, "hk-aaa")
	sendBeadClosed(t, fb, "hk-bbb")
	sendReviewerVerdict(t, fb)
	sendBeadClosed(t, fb, "hk-ccc")

	if n := fb.emittedAnomalyCount(); n != 0 {
		t.Errorf("expected 0 review_gate_anomaly events after verdict reset, got %d", n)
	}
}

// TestReviewGateAnomalyWatcher_RearmAfterAlarm verifies that after the alarm
// fires and the counter resets, a new run of N consecutive closes fires a
// second alarm.
func TestReviewGateAnomalyWatcher_RearmAfterAlarm(t *testing.T) {
	t.Parallel()

	_, fb := newWatcherWithThreshold(t, 3)

	// First batch: fires alarm at 3.
	sendBeadClosed(t, fb, "hk-aaa")
	sendBeadClosed(t, fb, "hk-bbb")
	sendBeadClosed(t, fb, "hk-ccc")

	if n := fb.emittedAnomalyCount(); n != 1 {
		t.Errorf("first batch: expected 1 alarm, got %d", n)
	}

	// Second batch (no verdict between): fires alarm again because counter reset.
	sendBeadClosed(t, fb, "hk-ddd")
	sendBeadClosed(t, fb, "hk-eee")
	sendBeadClosed(t, fb, "hk-fff")

	if n := fb.emittedAnomalyCount(); n != 2 {
		t.Errorf("second batch: expected 2 alarms total, got %d", n)
	}
}

// TestReviewGateAnomalyWatcher_VerdictBeforeAnyCloseIsHarmless verifies that a
// verdict with no preceding closes does not cause any panic or error.
func TestReviewGateAnomalyWatcher_VerdictBeforeAnyCloseIsHarmless(t *testing.T) {
	t.Parallel()

	_, fb := newWatcherWithThreshold(t, 3)

	sendReviewerVerdict(t, fb)
	// No panics, no alarms.
	if n := fb.emittedAnomalyCount(); n != 0 {
		t.Errorf("expected 0 alarms, got %d", n)
	}
}

// TestReviewGateAnomalyWatcher_ThresholdOne verifies that a threshold of 1
// fires on the very first bead_closed.
func TestReviewGateAnomalyWatcher_ThresholdOne(t *testing.T) {
	t.Parallel()

	_, fb := newWatcherWithThreshold(t, 1)

	sendBeadClosed(t, fb, "hk-aaa")

	if n := fb.emittedAnomalyCount(); n != 1 {
		t.Errorf("threshold=1: expected 1 alarm after first bead_closed, got %d", n)
	}
}
