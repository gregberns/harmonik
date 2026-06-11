package daemon

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

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/handlercontract"
)

// defaultPostAgentReadyHangTimeout is the default timeout used by
// waitPostAgentReadyProgress when the caller passes zero.
//
// 7 minutes: generous enough that a legitimately slow-starting agent (large
// context load, extended planning) is not falsely detected, yet short enough
// to reclaim most of the 30-min commitPollTimeout budget when a session is
// truly hung.
var defaultPostAgentReadyHangTimeout = 7 * time.Minute

// ErrPostAgentReadyHang is returned by waitPostAgentReadyProgress when the
// implementer emitted agent_ready but no subsequent event arrived within the
// configured timeout (hk-a2okh).
var ErrPostAgentReadyHang = errors.New("daemon: post-agent_ready hang: implementer made no observable progress after becoming ready")

// waitPostAgentReadyProgress blocks until one of:
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
func waitPostAgentReadyProgress(ctx context.Context, eventCh <-chan core.EventEnvelope, timeout time.Duration) error {
	if timeout <= 0 {
		timeout = defaultPostAgentReadyHangTimeout
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case _, ok := <-eventCh:
		if ok {
			return nil // first event after agent_ready = progress observed
		}
		// closed channel: treat as hang (producer shut down without events)
		return ErrPostAgentReadyHang
	case <-timer.C:
		return ErrPostAgentReadyHang
	case <-ctx.Done():
		return ctx.Err()
	}
}

// emitPostAgentReadyHang emits a post_agent_ready_hang event onto the bus
// (hk-a2okh). Non-fatal: marshal or emit errors are silently dropped.
func emitPostAgentReadyHang(
	ctx context.Context,
	bus handlercontract.EventEmitter,
	runID core.RunID,
	claudeSessionID string,
	timeout time.Duration,
	iterationCount int,
	phase string,
) {
	if timeout <= 0 {
		timeout = defaultPostAgentReadyHangTimeout
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
