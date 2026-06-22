package daemon

// reviewgateanomaly_hktnmjy.go — review_gate_anomaly alarm (hk-tnmjy).
//
// ReviewGateAnomalyWatcher emits review_gate_anomaly when N consecutive
// bead_closed events fire with zero intervening reviewer_verdict events.
//
// This is the alarm that should have fired on 2026-06-01 when ~117 beads were
// dispatched and closed without any review-loop verdicts (the review-loop
// workflow_mode default was missing from the daemon config after a deploy).
//
// Logic:
//   - On reviewer_verdict: reset the consecutive counter.
//   - On bead_closed: append the bead_id to the running window; when count
//     reaches the threshold emit review_gate_anomaly and reset the counter so
//     subsequent batches re-arm independently.
//
// The default threshold is 3 (anomaly fires after 3 consecutive unreviewed
// closes). Operators may override via HARMONIK_REVIEW_GATE_ANOMALY_THRESHOLD.
//
// Consumer class: asynchronous (bus worker-pool goroutine; not on the critical
// path of any emit caller).
//
// Spec ref: specs/event-model.md §8.14 (hk-tnmjy).
// Bead ref: hk-tnmjy.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/eventbus"
)

const (
	// defaultReviewGateAnomalyThreshold is the number of consecutive
	// bead_closed events without a reviewer_verdict before the alarm fires.
	defaultReviewGateAnomalyThreshold = 3

	// envReviewGateAnomalyThreshold is the environment variable an operator
	// can set to override the threshold.  Value must be a positive integer ≥ 1.
	envReviewGateAnomalyThreshold = "HARMONIK_REVIEW_GATE_ANOMALY_THRESHOLD"
)

// ReviewGateAnomalyWatcher tracks consecutive bead_closed events and emits
// review_gate_anomaly when the threshold is reached without any intervening
// reviewer_verdict.
//
// All exported methods are safe for concurrent use.
type ReviewGateAnomalyWatcher struct {
	mu sync.Mutex

	// mutable state — reset on verdict or after firing the alarm.
	consecutive int      // count of bead_closed since last reset
	beadIDs     []string // bead IDs in the current consecutive window

	// configuration — immutable after construction.
	threshold int
	bus       eventbus.EventBus
}

// NewReviewGateAnomalyWatcher constructs a watcher, reading the threshold from
// the process environment (HARMONIK_REVIEW_GATE_ANOMALY_THRESHOLD; default 3).
func NewReviewGateAnomalyWatcher(bus eventbus.EventBus) *ReviewGateAnomalyWatcher {
	threshold := defaultReviewGateAnomalyThreshold
	if v := os.Getenv(envReviewGateAnomalyThreshold); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 1 {
			threshold = n
		}
	}
	return &ReviewGateAnomalyWatcher{
		threshold: threshold,
		bus:       bus,
	}
}

// NewReviewGateAnomalyWatcherWithThreshold constructs a watcher with an
// explicit threshold, bypassing the environment variable.  Intended for tests.
func NewReviewGateAnomalyWatcherWithThreshold(bus eventbus.EventBus, threshold int) *ReviewGateAnomalyWatcher {
	if threshold < 1 {
		threshold = defaultReviewGateAnomalyThreshold
	}
	return &ReviewGateAnomalyWatcher{
		threshold: threshold,
		bus:       bus,
	}
}

// Subscribe registers two asynchronous consumers with the bus:
//   - review-gate-anomaly-bead-closed    (bead_closed)
//   - review-gate-anomaly-reviewer-verdict (reviewer_verdict)
//
// Must be called before bus.Seal per EV-009.
func (w *ReviewGateAnomalyWatcher) Subscribe(bus eventbus.EventBus) error {
	beadClosedSub := core.Subscription{
		ConsumerID:    "review-gate-anomaly-bead-closed",
		ConsumerClass: core.ConsumerClassAsynchronous,
		EventPattern: core.EventPattern{
			Types: map[core.EventType]struct{}{
				core.EventTypeBeadClosed: {},
			},
		},
		OnPanic: core.OnPanicRecoverAndLog,
		Handler: w.handleBeadClosed,
	}
	if _, err := bus.Subscribe(beadClosedSub); err != nil {
		return fmt.Errorf("ReviewGateAnomalyWatcher.Subscribe: bead_closed consumer: %w", err)
	}

	verdictSub := core.Subscription{
		ConsumerID:    "review-gate-anomaly-reviewer-verdict",
		ConsumerClass: core.ConsumerClassAsynchronous,
		EventPattern: core.EventPattern{
			Types: map[core.EventType]struct{}{
				core.EventTypeReviewerVerdict: {},
			},
		},
		OnPanic: core.OnPanicRecoverAndLog,
		Handler: w.handleReviewerVerdict,
	}
	if _, err := bus.Subscribe(verdictSub); err != nil {
		return fmt.Errorf("ReviewGateAnomalyWatcher.Subscribe: reviewer_verdict consumer: %w", err)
	}

	return nil
}

// handleBeadClosed increments the consecutive counter and fires the alarm when
// the threshold is reached.
func (w *ReviewGateAnomalyWatcher) handleBeadClosed(ctx context.Context, evt core.Event) error {
	var payload core.BeadClosedPayload
	if err := json.Unmarshal(evt.Payload, &payload); err != nil {
		// Malformed payload — skip; bus dead-letter path handles persistent failures.
		return nil
	}

	w.mu.Lock()
	w.consecutive++
	w.beadIDs = append(w.beadIDs, string(payload.BeadID))
	count := w.consecutive
	ids := make([]string, len(w.beadIDs))
	copy(ids, w.beadIDs)
	threshold := w.threshold
	w.mu.Unlock()

	if count >= threshold {
		return w.emitAnomaly(ctx, count, threshold, ids)
	}
	return nil
}

// handleReviewerVerdict resets the consecutive counter — a verdict means the
// review gate is working.
func (w *ReviewGateAnomalyWatcher) handleReviewerVerdict(_ context.Context, _ core.Event) error {
	w.mu.Lock()
	w.consecutive = 0
	w.beadIDs = w.beadIDs[:0]
	w.mu.Unlock()
	return nil
}

// emitAnomaly emits review_gate_anomaly and resets the counter so subsequent
// batches re-arm independently.
func (w *ReviewGateAnomalyWatcher) emitAnomaly(ctx context.Context, count, threshold int, beadIDs []string) error {
	// Reset before emitting to re-arm for future batches. We reset here (before
	// emit) rather than after so that concurrent bead_closed events that arrive
	// while emit is in progress start a fresh window rather than double-firing.
	w.mu.Lock()
	w.consecutive = 0
	w.beadIDs = w.beadIDs[:0]
	w.mu.Unlock()

	alarmPayload := core.ReviewGateAnomalyPayload{
		ConsecutiveCount: count,
		Threshold:        threshold,
		BeadIDs:          beadIDs,
		DetectedAt:       time.Now().UTC().Format(time.RFC3339),
	}
	payloadJSON, err := json.Marshal(alarmPayload)
	if err != nil {
		return fmt.Errorf("ReviewGateAnomalyWatcher.emitAnomaly: marshal: %w", err)
	}
	if err := w.bus.Emit(ctx, core.EventTypeReviewGateAnomaly, payloadJSON); err != nil {
		return fmt.Errorf("ReviewGateAnomalyWatcher.emitAnomaly: emit review_gate_anomaly: %w", err)
	}
	return nil
}
