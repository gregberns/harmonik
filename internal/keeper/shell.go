package keeper

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/substrate"
)

// shell.go — the IMPERATIVE SHELL around the pure Cycle reactor (T7,
// session-keeper-design §3d "Shell" / §2c / §6.1). It owns every port call,
// the ClockPort, the timer deadlines the reactor arms as ArmTimer/CancelTimer
// actions, and the 200ms detection poll that dissolves the two pre-rebuild
// blocking loops (pollForNonce, waitForNewSessionIDWithBackstop).
//
// REPRODUCE-THE-FREEZE (SK-017 / D11 — the biggest parity risk): the drive
// loop below runs SYNCHRONOUSLY inside the entry call (MaybeRun /
// RunForPrecompact / RunForIdle), exactly as the pre-rebuild runCycle blocked
// the watcher tick. While the machine is off-Idle the calling goroutine — the
// watcher's tick loop — processes NOTHING else: no warns, no precompact
// detection, no heartbeat, reaper, or hard-ceiling. That synchronous block IS
// the InCycle suppression; the watcher additionally consults Cycler.InCycle()
// at the top of its tick (watcher.go) so the parked-processing contract is
// explicit and holds even if the reactor is ever driven asynchronously.
// Relaxing this is a later, separately-measured change (D11, deferred).

// execute is the reactor's effector: it maps one Action onto the ports.
// Failure policy is byte-compatible with the pre-rebuild call sites: every
// side effect is best-effort (`_ =`) EXCEPT the "opened" journal write, whose
// error aborts the cycle start (runCycle returned it).
func (c *Cycler) execute(ctx context.Context, a Action) error {
	switch a.Kind {
	case ActWriteJournal:
		return c.executeWriteJournal(a)
	case ActEmit:
		c.executeEmit(ctx, a)
	case ActTruncateHandoff:
		_ = c.handoff.TruncateHandoff() //nolint:errcheck // non-fatal; poll fails gracefully
	case ActSendEscape:
		_ = c.pane.SendEscape(ctx, c.cfg.TmuxTarget) //nolint:errcheck // non-fatal; clears partial input
	case ActInjectHandoffCmd:
		c.executeInjectHandoffCmd(ctx, a)
	case ActInjectClear:
		_ = c.pane.Inject(ctx, c.cfg.TmuxTarget, "/clear") //nolint:errcheck // non-fatal; a dropped /clear is caught by the Clearing poll
	case ActInjectBrief:
		_ = c.pane.Inject(ctx, c.cfg.TmuxTarget, briefRestartCmd) //nolint:errcheck // non-fatal; brief re-injection is retried by the reactor
	case ActSetTmuxEnv:
		_ = c.pane.SetEnv(ctx, c.cfg.TmuxTarget, a.Key, a.Value) //nolint:errcheck // non-fatal; env is advisory, watcher rebinds on next tick
	case ActSetManagedSession:
		c.executeSetManagedSession(ctx, a)
	case ActClearPrecompact:
		_ = c.gauge.ClearPrecompactTrigger() //nolint:errcheck // non-fatal; a stale precompact trigger is re-cleared next cycle
	case ActSetHold:
		// Best-effort (SetHold fails silently when .sid is absent) — Gate 5d.
		_, _ = c.gauge.SetHold() //nolint:errcheck // best-effort; SetHold no-ops without a .sid (Gate 5d)
	case ActForceRestart:
		c.executeForceRestart(ctx)
	case ActArmTimer:
		c.executeArmTimer(a)
	case ActCancelTimer:
		delete(c.timers, a.Timer)
	}
	return nil
}

// executeWriteJournal is the ActWriteJournal arm. Only the "opened" journal
// write is fatal (the cycle must not start unjournaled — pre-rebuild runCycle
// returned its error); all other journal writes were `_ =` best-effort.
func (c *Cycler) executeWriteJournal(a Action) error {
	j := a.Journal
	if err := c.handoff.WriteJournal(&j); err != nil {
		if j.Phase == "opened" {
			return err // fatal: the cycle must not start unjournaled
		}
		// all other journal writes were `_ =` best-effort
	}
	return nil
}

// executeInjectHandoffCmd is the ActInjectHandoffCmd arm. The injection
// instant is captured BEFORE injecting (hk-fi78d): the freshness-recovery
// sampler treats a handoff as written-for-this-cycle only when its mtime is
// at/after this moment.
func (c *Cycler) executeInjectHandoffCmd(ctx context.Context, a Action) {
	c.handoffInjectedAt = c.cfg.Clock.Now()
	handoffCmd := fmt.Sprintf(
		"/session-handoff %s\n\nIMPORTANT: include exactly this line verbatim in the handoff file: %s",
		c.handoff.HandoffPath(), nonceMarker(a.CycleID),
	)
	// Non-fatal: the confirm step catches any delivery failure.
	_ = c.pane.Inject(ctx, c.cfg.TmuxTarget, handoffCmd) //nolint:errcheck // non-fatal; the nonce-confirm step catches a dropped injection
}

// executeSetManagedSession is the ActSetManagedSession arm. Non-fatal: the
// watcher latch path rebinds on the next tick.
func (c *Cycler) executeSetManagedSession(ctx context.Context, a Action) {
	if err := c.gauge.SetManagedSession(a.SID); err != nil {
		slog.WarnContext(ctx, "keeper: update managed session_id",
			"agent", c.cfg.AgentName, "sid", a.SID, "err", err)
	}
}

// executeForceRestart is the ActForceRestart arm (hk-qoz escalation).
func (c *Cycler) executeForceRestart(ctx context.Context) {
	if c.respawn == nil {
		return
	}
	slog.WarnContext(ctx, "keeper: escalating to hard restart after repeated handoff timeouts",
		"agent", c.cfg.AgentName)
	if restartErr := c.respawn.ForceRestart(ctx, c.cfg.AgentName); restartErr != nil {
		slog.WarnContext(ctx, "keeper: hard restart failed",
			"agent", c.cfg.AgentName, "err", restartErr)
	}
}

// executeEmit is the ActEmit arm. D9 / SK-013: emit failures for the four
// §8.20 interior events MUST NOT be silently swallowed — log, non-fatal
// (O-class: no retry, no block; the failure just becomes observable). Every
// other keeper emit keeps the pre-rebuild best-effort discard.
func (c *Cycler) executeEmit(ctx context.Context, a Action) {
	err := c.emitter.EmitWithRunID(ctx, core.RunID{}, a.Type, a.Payload)
	if err == nil {
		return
	}
	switch a.Type {
	case core.EventTypeSessionKeeperHandoffWritten,
		core.EventTypeSessionKeeperModelDone,
		core.EventTypeSessionKeeperClearSent,
		core.EventTypeSessionKeeperNewSessionUp:
		slog.WarnContext(ctx, "keeper: interior event emit failed",
			"agent", c.cfg.AgentName, "type", string(a.Type), "err", err)
	default:
		// Every other keeper emit keeps the pre-rebuild best-effort discard.
	}
}

// executeArmTimer is the ActArmTimer arm. The deadline is anchored at
// EXECUTION time (after the preceding actions in the batch, e.g. the inject)
// — matching where the pre-rebuild code created its context.WithTimeout /
// backstop deadline.
func (c *Cycler) executeArmTimer(a Action) {
	if a.Timer == TimerHandoffTimeout && c.cfg.TmuxTarget == "" {
		// hk-fi78d parity: with an empty tmux target no ActInjectHandoffCmd
		// is emitted, so the freshness anchor is never stamped there. The
		// pre-rebuild runCycle stamped handoffInjectedAt = Clock.Now()
		// UNCONDITIONALLY before the `if TmuxTarget != ""` injection branch,
		// so an empty-target handoff-timeout compares the handoff mtime
		// against a per-cycle anchor (stale handoff → ABORT), not a zero/
		// prior-cycle anchor (which would wrongly take the recovery path and
		// flip LastFireWasAbort). Re-stamp per firing cycle here; the
		// non-empty-target path keeps its exact before-inject stamp.
		c.handoffInjectedAt = c.cfg.Clock.Now()
	}
	if c.timers == nil {
		c.timers = make(map[TimerKind]time.Time)
	}
	c.timers[a.Timer] = c.cfg.Clock.Now().Add(a.D)
	c.timersArmed = true
}

// feed runs one event through the pure reactor and executes the resulting
// actions in order. The only propagated failure is the fatal opened-journal
// write, which rolls the machine back to Idle (failOpen) exactly as the
// pre-rebuild runCycle returned before any injection.
func (c *Cycler) feed(ctx context.Context, ev Event) error {
	for _, a := range c.machine.Step(ev) {
		if err := c.execute(ctx, a); err != nil {
			c.machine.failOpen()
			return err
		}
	}
	return nil
}

// runEntry is the shared shell entry: peek the pure ladder to decide whether
// this entry fires (so the cycle id is minted — and the handoff content
// sampled for the stale-nonce predicate — ONLY for firing entries, keeping
// CycleIDGen / ReadHandoff call counts fire-aligned with the pre-rebuild
// code), feed the event, then drive the machine to its terminal.
func (c *Cycler) runEntry(ctx context.Context, ev Event) error {
	if c.machine.peekFires(ev) {
		ev.CycleID = c.cfg.CycleIDGen()
		content, err := c.handoff.ReadHandoff()
		ev.HandoffContent = content
		ev.HandoffReadOK = err == nil
	}
	if err := c.feed(ctx, ev); err != nil {
		return err
	}
	return c.drive(ctx)
}

// drive pumps the reactor to a terminal while a cycle is in flight. It is the
// event-loop form of the two dissolved poll loops:
//
//   - a fresh Clock.NewTicker(PollInterval) detection ticker per armed-timer
//     generation reproduces first-tick-after-interval semantics per phase and
//     per settle attempt (parity risk #4: pollForNonce held ONE ticker for
//     the whole nonce wait; waitForNewSessionID created a NEW ticker per
//     settle window);
//   - timer deadlines are checked ON detection ticks, timeout-before-read at
//     the boundary; the clear backstop is evaluated only when a settle window
//     ends, exactly like the pre-rebuild wrapper's per-attempt deadline check;
//   - a second, one-shot-style wake fires exactly at the nearest armed-timer
//     deadline, so timeout DETECTION stays punctual (within scheduler jitter)
//     like the pre-rebuild context.WithTimeout, instead of being quantized to
//     a possibly-contention-delayed poll tick. Without it, a delayed tick can
//     stretch a firing cycle's wall time past ForceRetryInterval and defeat
//     Gate 6's forced-clear suppression (observed as a 3rd cycle_aborted in
//     TestCycler_ForcedClear_RetryAfterInterval under full-package load);
//   - ctx cancellation maps onto the same timer edges the pre-rebuild
//     ctx.Done() selects hit (handoff wait → handoff_timeout with the
//     freshness sample; clear wait → the backstop's unconfirmed path).
//
// AwaitModelDone (T8, SK-014) is a real detection phase like the other two:
// pollOnce reads the .idle marker (primary) and the assistant transcript turn
// (backstop) against t_nonce, and the armed model_done_timeout fails open to
// Clearing degraded. SK-018's old-corpus ModelDone synthesis (measurement
// wave) keeps pre-rebuild replay goldens on the clear-right-after-confirm
// shape this phase used to pass through.
func (c *Cycler) drive(ctx context.Context) error {
	for c.machine.InCycle() {
		ticker := c.cfg.Clock.NewTicker(c.cfg.PollInterval)
		// Punctual deadline wake: arm a dedicated ticker at the nearest armed
		// deadline so its expiry is observed on time even when the detection
		// ticker's next tick is scheduler-delayed. It funnels into the same
		// pollOnce timeout-before-read path; the detection cadence itself is
		// untouched. All waiting stays on the ClockPort (replay/FakeClock safe).
		var deadlineTicker substrate.Ticker
		var deadlineC <-chan time.Time
		if remaining, ok := c.nearestDeadline(); ok {
			deadlineTicker = c.cfg.Clock.NewTicker(remaining)
			deadlineC = deadlineTicker.C()
		}
		c.timersArmed = false
		for c.machine.InCycle() && !c.timersArmed {
			select {
			case <-ctx.Done():
				c.fireOnCancel(ctx)
			case <-ticker.C():
				c.pollOnce(ctx)
			case <-deadlineC:
				// One-shot semantics (hk-n8yha): the deadline wake is a
				// repeating substrate.Ticker used only to punctually observe
				// the nearest armed deadline ONCE per generation. Disable it
				// after the first fire (a nil channel blocks in select) so an
				// already-elapsed deadline — nearestDeadline clamps a non-
				// positive remaining to 1ns, e.g. a ClearConfirmBackstop that
				// fell due between pollClearing's boundary sample and this
				// generation's nearestDeadline sample under a non-aligned
				// config — cannot re-fire every 1ns and spin the loop (tight
				// CPU + a ReadGauge disk-read storm). After the one wake,
				// detection stays on the PollInterval ticker until a timer
				// re-arms, which starts a fresh generation with a fresh
				// deadline wake; the backstop is still consulted at the settle-
				// window end exactly as before.
				deadlineC = nil
				c.pollOnce(ctx)
			}
		}
		ticker.Stop()
		if deadlineTicker != nil {
			deadlineTicker.Stop()
		}
	}
	return nil
}

// nearestDeadline returns the remaining duration to the earliest armed timer
// deadline, clamped to a 1ns minimum (Clock.NewTicker requires d > 0; an
// already-elapsed deadline fires on the very next wake). ok is false when no
// timer is armed for the current generation.
func (c *Cycler) nearestDeadline() (remaining time.Duration, ok bool) {
	var best time.Time
	for _, dl := range c.timers {
		if !ok || dl.Before(best) {
			best, ok = dl, true
		}
	}
	if !ok {
		return 0, false
	}
	remaining = best.Sub(c.cfg.Clock.Now())
	if remaining <= 0 {
		remaining = time.Nanosecond
	}
	return remaining, true
}

// pollOnce is one detection tick: fire an elapsed timer for the current
// phase, else read the phase-appropriate source and emit the detection event.
func (c *Cycler) pollOnce(ctx context.Context) {
	at := c.cfg.Clock.Now()
	st := c.machine.State()
	switch st.Phase {
	case PhaseAwaitingHandoff:
		c.pollAwaitingHandoff(ctx, st, at)
	case PhaseAwaitModelDone:
		c.pollAwaitModelDone(ctx, st, at)
	case PhaseClearing:
		c.pollClearing(ctx, st, at)
	default:
	}
}

// pollAwaitingHandoff is the AwaitingHandoff detection tick: handoff-timeout
// expiry (with the freshness sample) first, else the nonce-echo read.
func (c *Cycler) pollAwaitingHandoff(ctx context.Context, st CycleState, at time.Time) {
	// T8 (SK-035): in-cycle operator-attached TOCTOU re-check. Gate-7 samples
	// operator-attached ONCE at cycle entry (ports.go:190); it is NOT re-checked
	// across the up-to-300s handoff wait, so an operator who starts typing AFTER
	// entry would be clobbered when the wait resolves into the destructive /clear.
	// Re-sample it here each wait tick and gate BOTH edges that reach /clear:
	// the nonce-confirm (below) and the handoff-timeout freshness recovery
	// (hk-fi78d, sampleHandoffFreshness). Scoped to the pane path (TmuxTarget set);
	// the comms path writes no pane so it is harmless there. Emits nothing (Gate-7's
	// operator_attached emission is a deliberate NO-OP, logmine TA3/F55). No
	// threshold/timing constant changes (NG1).
	attached := c.cfg.TmuxTarget != "" && c.cfg.OperatorAttachedFn(c.cfg.TmuxTarget)
	if dl, ok := c.timers[TimerHandoffTimeout]; ok && !at.Before(dl) {
		delete(c.timers, TimerHandoffTimeout)
		// While an operator is attached, SKIP the freshness recovery so the timeout
		// aborts warn-only (→ stepAbort, never /clear) rather than /clear over the
		// operator via the recovered edge. The timeout still fires, so the wait is
		// bounded (it does not hold forever).
		if !attached {
			c.sampleHandoffFreshness(ctx, st, at)
		}
		_ = c.feed(ctx, Event{Kind: EvTimerFired, Timer: TimerHandoffTimeout, CycleID: st.CycleID, At: at}) //nolint:errcheck // non-fatal; a poll-fed event fails the cycle open, never the poll tick
		return
	}
	// While attached, HOLD the nonce-confirm (the sole gate to /clear) — we never
	// /clear over the operator's in-flight turn. The wait continues; on the next
	// tick after they detach the nonce is read and the cycle proceeds.
	if attached {
		return
	}
	content, err := c.handoff.ReadHandoff()
	if err == nil && strings.Contains(content, nonceMarker(st.CycleID)) {
		_ = c.feed(ctx, Event{Kind: EvNonceObserved, CycleID: st.CycleID, At: at}) //nolint:errcheck // non-fatal; a poll-fed event fails the cycle open, never the poll tick
	}
}

// pollAwaitModelDone is the AwaitModelDone detection tick (T8, SK-014).
func (c *Cycler) pollAwaitModelDone(ctx context.Context, st CycleState, at time.Time) {
	if dl, ok := c.timers[TimerModelDone]; ok && !at.Before(dl) {
		// Fail-open bound (SK-014/SR9): the reactor proceeds to Clearing
		// degraded; never silence.
		delete(c.timers, TimerModelDone)
		_ = c.feed(ctx, Event{Kind: EvTimerFired, Timer: TimerModelDone, CycleID: st.CycleID, At: at}) //nolint:errcheck // non-fatal; a poll-fed event fails the cycle open, never the poll tick
		return
	}
	// Primary source: the Stop-hook .idle marker. The first
	// mtime(.idle) ≥ t_nonce after handoff confirmation means the model
	// reached an await-input boundary AFTER the turn that wrote the
	// handoff. STRICT compare against the nonce instant — no
	// crispIdleTolerance fudge (that tolerance discounts passive .ctx
	// repaints, irrelevant against t_nonce). SK-014 / design §5.
	if mt, ok := c.gauge.IdleMarkerModTime(); ok && !mt.Before(st.NonceConfirmedAt) {
		_ = c.feed(ctx, Event{ //nolint:errcheck // non-fatal; a poll-fed event fails the cycle open, never the poll tick
			Kind: EvModelDone, CycleID: st.CycleID,
			SessionID: st.PrevSID, Source: "idle_marker", At: at,
		})
		return
	}
	// Backstop source: a real assistant transcript turn at/after t_nonce
	// (agents whose Stop hook isn't wired). Heavier (JSONL tail scan);
	// consulted only when the .idle read yields nothing.
	if tt, ok := c.gauge.LastAssistantTurn(st.PrevSID); ok && !tt.Before(st.NonceConfirmedAt) {
		_ = c.feed(ctx, Event{ //nolint:errcheck // non-fatal; a poll-fed event fails the cycle open, never the poll tick
			Kind: EvModelDone, CycleID: st.CycleID,
			SessionID: st.PrevSID, Source: "transcript_turn", At: at,
		})
	}
}

// pollClearing is the Clearing detection tick: settle-window / backstop
// expiry first, else the session-id rebind read.
func (c *Cycler) pollClearing(ctx context.Context, st CycleState, at time.Time) {
	if dl, ok := c.timers[TimerClearSettle]; ok && !at.Before(dl) {
		// The wall-clock backstop is consulted at settle-window ends,
		// exactly like waitForNewSessionIDWithBackstop's post-attempt
		// deadline check — it never cuts a settle window short.
		if bdl, bok := c.timers[TimerClearBackstop]; bok && !at.Before(bdl) {
			_ = c.feed(ctx, Event{Kind: EvTimerFired, Timer: TimerClearBackstop, CycleID: st.CycleID, At: at}) //nolint:errcheck // non-fatal; a poll-fed event fails the cycle open, never the poll tick
			return
		}
		_ = c.feed(ctx, Event{Kind: EvTimerFired, Timer: TimerClearSettle, CycleID: st.CycleID, At: at}) //nolint:errcheck // non-fatal; a poll-fed event fails the cycle open, never the poll tick
		return
	}
	cf, _, err := c.gauge.ReadGauge()
	if err == nil && cf.SessionID != "" && cf.SessionID != st.PrevSID {
		_ = c.feed(ctx, Event{ //nolint:errcheck // non-fatal; a poll-fed event fails the cycle open, never the poll tick
			Kind: EvSessionChanged, CycleID: st.CycleID,
			PrevSID: st.PrevSID, NewSID: cf.SessionID, At: at,
		})
	}
}

// fireOnCancel maps a parent-ctx cancellation onto the phase's timeout edge,
// matching the pre-rebuild ctx.Done() select arms: the nonce wait aborted
// (after the freshness check) and the clear wait fell to the unconfirmed
// backstop path.
func (c *Cycler) fireOnCancel(ctx context.Context) {
	at := c.cfg.Clock.Now()
	st := c.machine.State()
	switch st.Phase {
	case PhaseAwaitingHandoff:
		delete(c.timers, TimerHandoffTimeout)
		c.sampleHandoffFreshness(ctx, st, at)
		_ = c.feed(ctx, Event{Kind: EvTimerFired, Timer: TimerHandoffTimeout, CycleID: st.CycleID, At: at}) //nolint:errcheck // non-fatal; a poll-fed event fails the cycle open, never the poll tick
	case PhaseAwaitModelDone:
		// Cancellation maps onto the fail-open timeout edge (SK-014/SR9): the
		// handoff is confirmed, so the cycle proceeds to Clearing degraded
		// rather than stranding a written handoff (never silence).
		delete(c.timers, TimerModelDone)
		_ = c.feed(ctx, Event{Kind: EvTimerFired, Timer: TimerModelDone, CycleID: st.CycleID, At: at}) //nolint:errcheck // non-fatal; a poll-fed event fails the cycle open, never the poll tick
	case PhaseClearing:
		_ = c.feed(ctx, Event{Kind: EvTimerFired, Timer: TimerClearBackstop, CycleID: st.CycleID, At: at}) //nolint:errcheck // non-fatal; a poll-fed event fails the cycle open, never the poll tick
	default:
	}
}

// sampleHandoffFreshness performs the hk-fi78d recovery sample at
// handoff-timeout expiry — the pre-rebuild handoffWrittenAndFresh, verbatim:
// the handoff counts as written-for-this-cycle only when it has real content
// AND its mtime is at/after the injection instant (never a look-back window;
// see the long rationale that traveled with handoffWrittenAndFresh). On a
// fresh handoff it feeds HandoffFreshSeen so the pure TimerFired edge takes
// the recovery path.
func (c *Cycler) sampleHandoffFreshness(ctx context.Context, st CycleState, at time.Time) {
	content, err := c.handoff.ReadHandoff()
	if err != nil || strings.TrimSpace(content) == "" {
		return
	}
	mt, ok := c.handoff.HandoffModTime()
	if !ok || mt.Before(c.handoffInjectedAt) {
		return
	}
	slog.WarnContext(ctx, "keeper: nonce echo timed out but a fresh handoff was written — recovering (proceeding with /clear + brief)",
		"agent", c.cfg.AgentName, "cycle_id", st.CycleID, "session_id", st.PrevSID)
	_ = c.feed(ctx, Event{Kind: EvHandoffFreshSeen, CycleID: st.CycleID, Mtime: mt, At: at}) //nolint:errcheck // non-fatal; a poll-fed event fails the cycle open, never the poll tick
}
