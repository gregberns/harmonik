package daemon

// waitsocketgrace.go — waitWithSocketGrace helper for the workloop completion path.
//
// Implements OQ2 resolution: Stop hook wins over bare exit code.  After
// cmd.Wait() returns, the daemon first checks the hookSessionStore for an
// already-arrived outcome_emitted payload.  If none is present it blocks on
// WaitForOutcome with a stopHookGrace timeout, giving the Stop hook relay
// time to complete the socket round-trip.
//
// CHB-020 branch semantics (specs/claude-hook-bridge.md §4.7):
//   Branch 1/2: outcome_emitted present (WORK_COMPLETE / REVIEWER_VERDICT /
//               FAILURE_SIGNAL) → returned with exitInfo.
//   Branch 3:   grace window expires with no outcome → nil outcome returned;
//               caller uses MapWaitReturnToTerminalEvent branch 3.
//
// Spec refs:
//   - specs/claude-hook-bridge.md §4.7 CHB-020 (terminal-event mapping)
//   - specs/claude-hook-bridge.md §4.10 CHB-025 (last-received-wins)
//
// Bead: hk-gql20.22.

import (
	"context"
	"encoding/json"
	"time"

	"github.com/gregberns/harmonik/internal/handler"
	"github.com/gregberns/harmonik/internal/handlercontract"
)

// stopHookGrace is the time the daemon waits after cmd.Wait() returns for a
// pending Stop hook relay to deliver its outcome_emitted payload.
//
// Rationale (OQ2): the Stop hook fires inside Claude's shutdown — Claude must
// exit before the hook finishes running and the relay completes the socket
// write.  3 s covers hook execution + relay process startup + socket
// round-trip with margin; longer windows risk operator-perceived hangs on
// crash cases where no hook will arrive.
const stopHookGrace = 3 * time.Second

// exitInfo carries the process exit metadata captured after sess.Wait() returns.
type exitInfo struct {
	exitCode int
	waitErr  error
}

// waitWithSocketGrace races ctx cancellation against watcher completion, reaps
// the subprocess, then checks the hookSessionStore for a Stop-hook outcome.
//
// Flow:
//  1. Race watcher.Done() vs ctx.Done().
//     - ctx fires first → Kill the session and drain watcher.
//  2. Call sess.Wait(ctx) to reap; capture exit code from sess.Outcome().
//  3. Check store for an already-present outcome (fast path — branch 1/2).
//  4. If absent, block on WaitForOutcome with a stopHookGrace context (slow
//     path).
//  5. Return whatever arrived, or nil if the grace window expired (branch 3).
//
// The returned *handler.ExportedOutcomeEmittedPayload is nil on branch 3.
// exitInfo is always populated from the completed sess.Wait return.
//
// Spec: specs/claude-hook-bridge.md §4.7 CHB-020, §4.10 CHB-025.
// Bead: hk-gql20.22.
func waitWithSocketGrace(
	ctx context.Context,
	store hookStoreIface,
	watcher *handlercontract.Watcher,
	sess handler.Session,
	runID, claudeSessID string,
) (*handler.ExportedOutcomeEmittedPayload, exitInfo) {
	// Step 1: race watcher completion vs context cancellation.
	select {
	case <-watcher.Done():
		// Normal exit: handler process terminated.
	case <-ctx.Done():
		// Operator/daemon cancellation: kill the session and wait for the
		// watcher to drain before proceeding.
		_ = sess.Kill(ctx)
		<-watcher.Done()
	}

	// Step 2: reap the subprocess.
	waitErr := sess.Wait(ctx)
	outcome := sess.Outcome()
	ei := exitInfo{exitCode: outcome.ExitCode, waitErr: waitErr}

	// Step 3: fast path — check for an outcome already present in the store.
	if outcome := parseLatestOutcome(store.LatestOutcome(runID, claudeSessID)); outcome != nil {
		return outcome, ei
	}

	// Step 4: slow path — wait up to stopHookGrace for a Stop hook relay.
	graceCtx, cancel := context.WithTimeout(context.Background(), stopHookGrace)
	defer cancel()
	rawOutcome, _ := store.WaitForOutcome(graceCtx, runID, claudeSessID)
	if rawOutcome != nil {
		if outcome := parseOutcomePayload(rawOutcome); outcome != nil {
			return outcome, ei
		}
	}

	// Step 5: grace expired or no relay arrived — branch 3.
	return nil, ei
}

// parseLatestOutcome unmarshals a *json.RawMessage from hookSessionStore.LatestOutcome
// into an ExportedOutcomeEmittedPayload.  Returns nil when raw is nil or
// unmarshalling fails.
func parseLatestOutcome(raw *json.RawMessage) *handler.ExportedOutcomeEmittedPayload {
	if raw == nil {
		return nil
	}
	return parseOutcomePayload(*raw)
}

// parseOutcomePayload unmarshals a json.RawMessage into an
// ExportedOutcomeEmittedPayload.  Returns nil on empty input or unmarshal
// error — callers treat unmarshal failure as "no outcome present" and fall
// through to branch 3.
func parseOutcomePayload(raw json.RawMessage) *handler.ExportedOutcomeEmittedPayload {
	if len(raw) == 0 {
		return nil
	}
	var p handler.ExportedOutcomeEmittedPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil
	}
	return &p
}
