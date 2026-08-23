package runloop

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/gregberns/harmonik/internal/handler"
	"github.com/gregberns/harmonik/internal/handlercontract"
	"github.com/gregberns/harmonik/internal/substrate"
)

// stopHookGrace is the time the daemon waits after cmd.Wait() returns for a
// pending Stop hook relay to deliver its outcome_emitted payload.
//
// Rationale (OQ2): the Stop hook fires inside Claude's shutdown — Claude must
// exit before the hook finishes running and the relay completes the socket
// write.  3 s covers hook execution + relay process startup + socket
// round-trip with margin; longer windows risk operator-perceived hangs on
// crash cases where no hook will arrive.
const StopHookGrace = 3 * time.Second

const killWatcherReapGrace = 3 * time.Second

// exitInfo carries the process exit metadata captured after sess.Wait() returns.
type ExitInfo struct {
	ExitCode   int
	WaitErr    error
	StderrTail []byte // last ~4 KiB of subprocess stderr; nil for substrate sessions

	// AgentAnnouncedEnd says the agent announced the end of its turn on its own
	// event stream and the daemon then terminated it ON that announcement. The
	// exit status of such a run describes what the DAEMON did, not how the agent
	// ended, so its signal death is not evidence that the agent crashed.
	//
	// Read the name literally. It is NOT a claim that the work succeeded, and it
	// must not grow into one: pi emits the same announcement for a turn it
	// aborted as for a turn it finished, and the harness parser reads no field
	// of the event that would tell the two apart. All this field withdraws is a
	// false claim about how the process died. What the run actually produced is
	// decided after it by the git layer, which the harness contract (HN-009,
	// HN-INV-001) makes the sole authority on whether work is done — a process
	// exit may refine that outcome and may never substitute for it.
	//
	// WaitWithSocketGrace never sets this and cannot: only the kill site knows
	// why it killed, and by the time a wait returns every kill looks alike —
	// SIGTERM, exit code -1, "signal: terminated" — whether the agent said it
	// was stopping or a watchdog found it wedged. runAgentLaunch sets it after
	// this function returns, from the callback that fires the announcement kill.
	//
	// The zero value is the safe answer: a launch that arms no announcement kill
	// leaves it false and is judged on its exit code exactly as before.
	AgentAnnouncedEnd bool
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
// clk is the determinism port for the killWatcherReapGrace bound (P2 E5 RT19c);
// nil is backstopped to substrate.SystemClock{} for struct-literal test callers.
//
// Spec: specs/claude-hook-bridge.md §4.7 CHB-020, §4.10 CHB-025.
// Bead: hk-gql20.22.
func WaitWithSocketGrace(
	ctx context.Context,
	clk substrate.ClockPort,
	store HookStore,
	watcher *handlercontract.Watcher,
	sess handler.Session,
	runID, claudeSessID string,
) (*handler.ExportedOutcomeEmittedPayload, ExitInfo) {
	if clk == nil {
		clk = substrate.SystemClock{}
	}

	if watcher != nil {
		select {
		case <-watcher.Done():
		case <-ctx.Done():
			if killErr := sess.Kill(ctx); killErr != nil {
				slog.WarnContext(ctx, "runloop: kill session on cancellation", "err", killErr)
			}
			select {
			case <-watcher.Done():
			case <-substrate.After(clk, killWatcherReapGrace): //nolint:contextcheck // ClockPort reap deadline, deliberately not ctx-scoped (the ctx here is already cancelled)
			}
		}
	} else if ctx.Err() != nil {
		if killErr := sess.Kill(ctx); killErr != nil {
			slog.WarnContext(ctx, "runloop: kill substrate session on cancellation", "err", killErr)
		}
	}

	waitErr := sess.Wait(ctx)
	outcome := sess.Outcome()
	ei := ExitInfo{ExitCode: outcome.ExitCode, WaitErr: waitErr, StderrTail: outcome.StderrTail}

	if outcome := parseLatestOutcome(store.LatestOutcome(runID, claudeSessID)); outcome != nil {
		return outcome, ei
	}

	graceCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), StopHookGrace)
	defer cancel()
	rawOutcome, waitOutcomeErr := store.WaitForOutcome(graceCtx, runID, claudeSessID)
	if waitOutcomeErr != nil {
		rawOutcome = nil
	}
	if rawOutcome != nil {
		if outcome := parseOutcomePayload(rawOutcome); outcome != nil {
			return outcome, ei
		}
	}

	return nil, ei
}

func parseLatestOutcome(raw *json.RawMessage) *handler.ExportedOutcomeEmittedPayload {
	if raw == nil {
		return nil
	}
	return parseOutcomePayload(*raw)
}

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
