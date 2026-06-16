package daemon_test

// pasteinject_hk60t8_test.go — unit tests for the hk-60t8 reviewer-timeout
// configurable ceiling and heartbeat-based active-reasoning extension.
//
// Problem: paul canary run 019ed1ad-77a3 had its opus/high reviewer (node
// review_correctness) killed at exactly 20:00 while ACTIVELY reasoning — 4×5-min
// agent_heartbeat events had fired.  The budget was 20 min (2000-line diff,
// diff-scaled per hk-sah87) and the hard ceiling was 30 min.  The liveness check
// (PaneHasActiveProcess) did not extend the deadline in that run.
//
// Fix (hk-60t8):
//  1. Raise reviewFileHardCeiling from 30 to 40 min (more headroom for opus/high).
//  2. Add heartbeat-based extension: if a recent agent_heartbeat was received
//     within reviewerHeartbeatActiveGrace (10 min = 2×HeartbeatInterval), treat
//     the reviewer as actively reasoning and extend the deadline.
//  3. Honor a per-node overrideCeiling (from the DOT timeout= attribute) so
//     authors can declare a longer budget for specific reviewer nodes.
//
// Tests (helper prefix: hk60t8):
//  A. No heartbeat + no liveness → kill fires at budget deadline (hung reviewer).
//  B. Recent heartbeat → deadline extended past budget; reviewer completes.
//  C. overrideCeiling > 0 overrides the default hard ceiling.
//
// Bead: hk-60t8.

import (
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
)

// ─────────────────────────────────────────────────────────────────────────────
// stubs
// ─────────────────────────────────────────────────────────────────────────────

type hk60t8QuitSender struct{ calls atomic.Int64 }

func (q *hk60t8QuitSender) SendQuitToLastPane(_ context.Context) error {
	q.calls.Add(1)
	return nil
}

type hk60t8Killer struct{ calls atomic.Int64 }

func (k *hk60t8Killer) Kill(_ context.Context) error {
	k.calls.Add(1)
	return nil
}

// ─────────────────────────────────────────────────────────────────────────────
// helpers
// ─────────────────────────────────────────────────────────────────────────────

// hk60t8ShortTimeouts overrides timing vars (reviewFileTimeout,
// reviewFilePollInterval, noChangeKillDelay, postQuitKillGrace,
// reviewerHeartbeatActiveGrace) to test-scale durations.
// Returns a restore function; call defer restore() immediately.
func hk60t8ShortTimeouts(reviewTimeout, reviewPoll, killDelay, postQuit, hbGrace time.Duration) func() {
	origRT := *daemon.ExportedReviewFileTimeout
	origRP := *daemon.ExportedReviewFilePollInterval
	origKD := *daemon.ExportedNoChangeKillDelay
	origPQ := *daemon.ExportedPostQuitKillGrace
	origHG := *daemon.ExportedReviewerHeartbeatActiveGrace
	*daemon.ExportedReviewFileTimeout = reviewTimeout
	*daemon.ExportedReviewFilePollInterval = reviewPoll
	*daemon.ExportedNoChangeKillDelay = killDelay
	*daemon.ExportedPostQuitKillGrace = postQuit
	*daemon.ExportedReviewerHeartbeatActiveGrace = hbGrace
	return func() {
		*daemon.ExportedReviewFileTimeout = origRT
		*daemon.ExportedReviewFilePollInterval = origRP
		*daemon.ExportedNoChangeKillDelay = origKD
		*daemon.ExportedPostQuitKillGrace = origPQ
		*daemon.ExportedReviewerHeartbeatActiveGrace = origHG
	}
}

// hk60t8WriteVerdict writes a non-empty review.json into <wtPath>/.harmonik/.
func hk60t8WriteVerdict(t *testing.T, wtPath string) {
	t.Helper()
	dir := filepath.Join(wtPath, ".harmonik")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("hk60t8WriteVerdict: mkdir: %v", err)
	}
	content := `{"schema_version":1,"verdict":"APPROVE","flags":[],"notes":"ok"}`
	if err := os.WriteFile(filepath.Join(dir, "review.json"), []byte(content), 0o644); err != nil {
		t.Fatalf("hk60t8WriteVerdict: write: %v", err)
	}
}

// hk60t8MakeHeartbeatCh returns a channel and a sender func that emits
// agent_heartbeat envelopes.
func hk60t8MakeHeartbeatCh() (ch chan core.EventEnvelope, sendHB func()) {
	ch = make(chan core.EventEnvelope, 32)
	sendHB = func() {
		ch <- core.EventEnvelope{Type: string(core.EventTypeAgentHeartbeat)}
	}
	return ch, sendHB
}

// ─────────────────────────────────────────────────────────────────────────────
// Test A: no heartbeat → kill fires at budget (hung reviewer, no extension)
// ─────────────────────────────────────────────────────────────────────────────

// TestReviewerTimeout_NoHeartbeat_KillsAtBudget verifies that when no
// agent_heartbeat is received and the reviewer has no active process (or the
// liveness checker is nil, which is the case with our stub), the kill fires at
// the budget deadline — not extended by the heartbeat path.
//
// Scenario: budget=40ms, poll=5ms, hbGrace=1000ms (long, so a heartbeat WOULD
// extend), but no heartbeat is sent → kill fires at ~40ms.
//
// Bead: hk-60t8.
func TestReviewerTimeout_NoHeartbeat_KillsAtBudget(t *testing.T) {
	restore := hk60t8ShortTimeouts(
		40*time.Millisecond,   // reviewFileTimeout (= budget, no diff so changedLines=-1)
		5*time.Millisecond,    // reviewFilePollInterval
		20*time.Millisecond,   // noChangeKillDelay
		1*time.Hour,           // postQuitKillGrace (verdict path — irrelevant)
		1000*time.Millisecond, // reviewerHeartbeatActiveGrace (long so it WOULD extend if HB present)
	)
	defer restore()

	// Override hard ceiling to be > budget so ceiling is NOT the limiting factor.
	origCeiling := *daemon.ExportedReviewFileHardCeiling
	*daemon.ExportedReviewFileHardCeiling = 500 * time.Millisecond
	defer func() { *daemon.ExportedReviewFileHardCeiling = origCeiling }()

	wtPath := t.TempDir()
	qs := &hk60t8QuitSender{}
	kl := &hk60t8Killer{}

	// nil eventCh = no heartbeat source.
	eventCh := (chan core.EventEnvelope)(nil)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Synchronous: budget fires, kill follows, function returns.
	daemon.ExportedPasteInjectQuitOnReviewFile(ctx, qs, kl, nil, "", wtPath, nil, eventCh, 0)

	if got := qs.calls.Load(); got != 1 {
		t.Errorf("SendQuitToLastPane: want 1 (budget kill), got %d", got)
	}
	if got := kl.calls.Load(); got != 1 {
		t.Errorf("Kill: want 1, got %d", got)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Test B: recent heartbeat → budget extended; reviewer writes verdict in time
// ─────────────────────────────────────────────────────────────────────────────

// TestReviewerTimeout_RecentHeartbeat_ExtendsDeadlineAndCompletes verifies
// that when an agent_heartbeat arrives BEFORE the budget deadline, the watchdog
// extends the deadline and the reviewer can complete by writing review.json
// within the extended window — instead of being killed at the original budget.
//
// Scenario:
//   - budget=40ms (short), hbGrace=200ms (longer than budget)
//   - Hard ceiling=300ms (> budget, so extension is allowed)
//   - A heartbeat is sent at ~20ms (before the 40ms budget fires)
//   - review.json is written at ~60ms (AFTER the original budget but BEFORE
//     the hard ceiling; the extension should allow the verdict to be detected)
//
// Bead: hk-60t8.
func TestReviewerTimeout_RecentHeartbeat_ExtendsDeadlineAndCompletes(t *testing.T) {
	restore := hk60t8ShortTimeouts(
		40*time.Millisecond,  // reviewFileTimeout (base budget)
		5*time.Millisecond,   // reviewFilePollInterval
		1*time.Hour,          // noChangeKillDelay (not reached — reviewer succeeds)
		20*time.Millisecond,  // postQuitKillGrace (verdict path kill)
		200*time.Millisecond, // reviewerHeartbeatActiveGrace (> budget → extends)
	)
	defer restore()

	// Set hard ceiling well above the extended deadline so ceiling is not the blocker.
	origCeiling := *daemon.ExportedReviewFileHardCeiling
	*daemon.ExportedReviewFileHardCeiling = 300 * time.Millisecond
	defer func() { *daemon.ExportedReviewFileHardCeiling = origCeiling }()

	wtPath := t.TempDir()
	qs := &hk60t8QuitSender{}
	kl := &hk60t8Killer{}

	eventCh, sendHB := hk60t8MakeHeartbeatCh()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	done := make(chan struct{})
	go func() {
		defer close(done)
		daemon.ExportedPasteInjectQuitOnReviewFile(ctx, qs, kl, nil, "", wtPath, nil, eventCh, 0)
	}()

	// Send heartbeat at 20ms (before the 40ms budget fires).
	time.Sleep(20 * time.Millisecond)
	sendHB()

	// Write verdict at 65ms — after the original 40ms budget but within the
	// extended window (heartbeat-based extension adds another reviewFileTimeout=40ms,
	// giving a new deadline of ~40ms+40ms=80ms from loop start, well before the
	// 300ms hard ceiling).
	time.Sleep(45 * time.Millisecond) // total ~65ms from start
	hk60t8WriteVerdict(t, wtPath)

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("pasteInjectQuitOnReviewFile did not return after verdict written")
	}

	// The function should have detected the verdict (not killed) — SendQuitToLastPane
	// fires for BOTH the verdict-detected path and the budget-kill path, so check Kill:
	// budget-kill calls Kill after noChangeKillDelay (1h — unreachable here),
	// verdict-detected calls Kill after postQuitKillGrace (20ms). Either way Kill=1.
	// The key assertion is that the reviewer was NOT killed BEFORE the verdict was written.
	// We verify this indirectly: the goroutine completed (done closed) after the verdict
	// was written, not before.
	if got := qs.calls.Load(); got != 1 {
		t.Errorf("SendQuitToLastPane: want 1, got %d", got)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Test C: overrideCeiling overrides the default hard ceiling
// ─────────────────────────────────────────────────────────────────────────────

// TestReviewerTimeout_OverrideCeiling_UsedInsteadOfDefault verifies that when
// overrideCeiling > 0 is passed, the watchdog uses it as the hard ceiling
// instead of reviewFileHardCeiling.  A reviewer that would be killed under the
// default ceiling completes when a larger override is provided.
//
// Scenario:
//   - budget=40ms, default hard ceiling overridden to 50ms (< 100ms)
//   - overrideCeiling=200ms → reviewer can complete at 80ms (> 50ms but < 200ms)
//
// Bead: hk-60t8.
func TestReviewerTimeout_OverrideCeiling_UsedInsteadOfDefault(t *testing.T) {
	restore := hk60t8ShortTimeouts(
		40*time.Millisecond,  // reviewFileTimeout (base budget)
		5*time.Millisecond,   // reviewFilePollInterval
		1*time.Hour,          // noChangeKillDelay
		20*time.Millisecond,  // postQuitKillGrace
		200*time.Millisecond, // reviewerHeartbeatActiveGrace (long so HB would extend)
	)
	defer restore()

	// Set package default ceiling to 50ms (budget 40ms → extension to ~80ms would
	// exceed 50ms → kill under the default ceiling).
	origCeiling := *daemon.ExportedReviewFileHardCeiling
	*daemon.ExportedReviewFileHardCeiling = 50 * time.Millisecond
	defer func() { *daemon.ExportedReviewFileHardCeiling = origCeiling }()

	wtPath := t.TempDir()
	qs := &hk60t8QuitSender{}
	kl := &hk60t8Killer{}

	// Send a heartbeat so the extension fires; the override ceiling of 200ms lets it.
	eventCh, sendHB := hk60t8MakeHeartbeatCh()

	// overrideCeiling=200ms (much larger than the 50ms default).
	const overrideCeiling = 200 * time.Millisecond

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	done := make(chan struct{})
	go func() {
		defer close(done)
		daemon.ExportedPasteInjectQuitOnReviewFile(ctx, qs, kl, nil, "", wtPath, nil, eventCh, overrideCeiling)
	}()

	// Send heartbeat before the 40ms budget fires.
	time.Sleep(20 * time.Millisecond)
	sendHB()

	// Write verdict at 80ms (> 50ms default ceiling, < 200ms override ceiling).
	// Without the override the kill would have fired at the 50ms hard ceiling.
	// With the override the reviewer can complete.
	time.Sleep(60 * time.Millisecond) // total ~80ms from start
	hk60t8WriteVerdict(t, wtPath)

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("pasteInjectQuitOnReviewFile did not return after verdict written with override ceiling")
	}

	if got := qs.calls.Load(); got != 1 {
		t.Errorf("SendQuitToLastPane: want 1, got %d", got)
	}
}
