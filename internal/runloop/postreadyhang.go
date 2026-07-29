package runloop

// postreadyhang.go — post-agent_ready hang detector (hk-a2okh).
//
// After waitAgentReady returns nil (agent_ready observed), the review-loop
// implementer may still hang without making any progress — no tool calls,
// no output, no events — burning the full commitPollTimeout (30 min) before
// the daemon can declare a no_commit failure.
//
// This file provides waitPostAgentReadyProgress: a lightweight goroutine-safe
// function that watches a per-run tap subscription for the FIRST event after
// agent_ready. If none arrives within the configured timeout the session is
// declared hung and ErrPostAgentReadyHang is returned, allowing the caller
// to fail fast.
//
// The detector is exec-path only (implWatcher != nil): in the tmux path the
// only post-ready signal would be daemon heartbeats, which fire unconditionally
// and cannot distinguish a hung agent from a working one.
//
// # NO PRODUCTION CALLER as of the review-loop retirement (2026-07-29)
//
// Stated here so nobody reads the green tests below as evidence this is wired.
// WaitPostAgentReadyProgress had exactly two production call sites, BOTH in
// internal/daemon/reviewloop.go. That driver was deleted when
// workflow_mode=review-loop was retired (execution-model.md §4.3.EM-015d), so
// every remaining reference is a test or an export seam. RunEnv's
// PostAgentReadyHangTimeout field is still assigned in runports.go and read by
// nothing.
//
// It was NOT ported to the dot cascade, because the property it defends —
// "the implementer became ready and then went silent" — is already covered
// there, by a different and TIGHTER mechanism. driveDotWorkflow passes a live
// heartbeat tap into pasteInjectQuitOnCommit (internal/daemon/pasteinject.go),
// which arms two bounds this detector does not improve on:
//
//   - launchHeartbeatTimeout (180s): the FIRST agent_heartbeat after brief
//     delivery must arrive inside this window or the session is killed. This
//     detector's equivalent bound was DefaultPostAgentReadyHangTimeout, 7
//     minutes — more than twice as loose.
//   - heartbeatStalenessThreshold (8 min): ongoing silence after the first
//     heartbeat, which this detector never covered at all (it only ever waited
//     for the FIRST post-ready event, then returned).
//
// So this is a redundant second implementation whose surviving equivalent is
// strictly stronger, not a capability that left the product. Recorded rather
// than deleted because the decision to delete it takes four test files with it
// (export_readywait_test.go, export_workloopdeps_test.go,
// twinparity_timing_property_test.go, workingphasewatchdog_rt19c_test.go), and
// two of those exercise real timing properties of the shared dispatch path that
// want re-homing rather than deletion.
//
// Resolve one way or the other — delete it with its dead RunEnv plumbing, or
// wire it into driveDotWorkflow if the 180s/8min pair is judged insufficient.
// Tracked as hk-q5scy.

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/handlercontract"
	"github.com/gregberns/harmonik/internal/substrate"
)

// DefaultPostAgentReadyHangTimeout is the default timeout used by
// WaitPostAgentReadyProgress when the caller passes zero.
//
// 7 minutes: generous enough that a legitimately slow-starting agent (large
// context load, extended planning) is not falsely detected, yet short enough
// to reclaim most of the 30-min commitPollTimeout budget when a session is
// truly hung.
var DefaultPostAgentReadyHangTimeout = 7 * time.Minute

// minPostAgentReadyHangTimeout is the floor WaitPostAgentReadyProgress clamps to
// when both the caller's timeout AND DefaultPostAgentReadyHangTimeout are
// non-positive. It exists only to keep a zero out of NewTicker, which panics on
// one; the fire-immediately semantics it produces match the stdlib timer this
// bound used before P2 E5 RT19c.
const minPostAgentReadyHangTimeout = time.Nanosecond

// ErrPostAgentReadyHang is returned by waitPostAgentReadyProgress when the
// implementer emitted agent_ready but no subsequent event arrived within the
// configured timeout (hk-a2okh).
var ErrPostAgentReadyHang = errors.New("daemon: post-agent_ready hang: implementer made no observable progress after becoming ready")

// WaitPostAgentReadyProgress blocks until one of:
//   - any event arrives on eventCh  → returns nil (progress observed)
//   - timeout elapses               → returns ErrPostAgentReadyHang
//   - ctx is cancelled              → returns ctx.Err()
//
// eventCh MUST be a fresh tap.Subscribe() channel obtained AFTER
// waitAgentReady returns nil, so the agent_ready event itself is not counted
// as "progress."  If timeout is zero, defaultPostAgentReadyHangTimeout is used.
//
// The function is safe to call from a goroutine; it owns no shared mutable
// state and exits cleanly on context cancellation.
//
// clk is the determinism port for the hang bound (P2 E5 RT19c) so a FakeClock
// can drive the timeout branch without real elapsed time; nil is backstopped to
// substrate.SystemClock{} for struct-literal test callers.
func WaitPostAgentReadyProgress(ctx context.Context, clk substrate.ClockPort, eventCh <-chan core.EventEnvelope, timeout time.Duration) error {
	if timeout <= 0 {
		timeout = DefaultPostAgentReadyHangTimeout
	}
	// Re-check AFTER the substitution. defaultPostAgentReadyHangTimeout is a
	// mutable package var that export_test.go hands to tests, so the substituted
	// value can itself be non-positive — and the RT19c ticker is far less
	// forgiving than the timer it replaced: a stdlib timer with a non-positive
	// duration fired immediately, but a stdlib ticker PANICS on one, and a
	// FakeClock ticker would hang instead (nextEventBefore only considers
	// instants strictly after now). Clamping to the smallest positive interval
	// keeps the pre-RT19c behaviour: the bound fires at once and the caller gets
	// ErrPostAgentReadyHang.
	if timeout <= 0 {
		timeout = minPostAgentReadyHangTimeout
	}
	if clk == nil {
		clk = substrate.SystemClock{}
	}
	// A one-shot ticker, not substrate.After, replaces the pre-RT19c
	// time.NewTimer: ClockPort has no timer, and a ticker is the only ClockPort
	// deadline that can still be RELEASED on an early return. The select reads it
	// at most once, so the repeat is never observed, and Stop preserves the
	// original `defer timer.Stop()` cleanup on the progress-observed and
	// ctx-cancelled paths.
	hang := clk.NewTicker(timeout)
	defer hang.Stop()

	select {
	case _, ok := <-eventCh:
		if ok {
			return nil // first event after agent_ready = progress observed
		}
		// closed channel: treat as hang (producer shut down without events)
		return ErrPostAgentReadyHang
	case <-hang.C():
		return ErrPostAgentReadyHang
	case <-ctx.Done():
		return ctx.Err()
	}
}

// EmitPostAgentReadyHang emits a post_agent_ready_hang event onto the bus
// (hk-a2okh). Non-fatal: marshal or emit errors are silently dropped.
func EmitPostAgentReadyHang(
	ctx context.Context,
	bus handlercontract.EventEmitter,
	runID core.RunID,
	claudeSessionID string,
	timeout time.Duration,
	iterationCount int,
	phase string,
) {
	if timeout <= 0 {
		timeout = DefaultPostAgentReadyHangTimeout
	}
	pl := core.PostAgentReadyHangPayload{
		RunID:           runID,
		ClaudeSessionID: claudeSessionID,
		TimeoutMs:       timeout.Milliseconds(),
		IterationCount:  iterationCount,
		Phase:           phase,
	}
	b, err := json.Marshal(pl)
	if err != nil {
		return
	}
	_ = bus.EmitWithRunID(ctx, runID, core.EventTypePostAgentReadyHang, b)
}
