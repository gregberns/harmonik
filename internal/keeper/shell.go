package keeper

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/substrate"
)

func (c *Cycler) execute(ctx context.Context, a Action) error {
	switch a.Kind {
	case ActWriteJournal:
		return c.executeWriteJournal(a)
	case ActEmit:
		c.executeEmit(ctx, a)
	case ActTruncateHandoff:
		_ = c.handoff.ScrubNonce() //nolint:errcheck // non-fatal; poll fails gracefully
	case ActSendEscape:
		_ = c.pane.SendEscape(ctx, c.cfg.TmuxTarget) //nolint:errcheck // non-fatal; clears partial input
	case ActInjectHandoffCmd:
		c.executeInjectHandoffCmd(ctx, a)
	case ActInjectClear:
		_ = c.pane.Inject(ctx, c.cfg.TmuxTarget, "/clear") //nolint:errcheck // non-fatal; a dropped /clear is caught by the Clearing poll
	case ActInjectBrief:
		_ = c.pane.Inject(ctx, c.cfg.TmuxTarget, briefRestartCmd(c.cfg.AgentName, c.cfg.ProjectDir)) //nolint:errcheck // non-fatal; brief re-injection is retried by the reactor
	case ActSetTmuxEnv:
		_ = c.pane.SetEnv(ctx, c.cfg.TmuxTarget, a.Key, a.Value) //nolint:errcheck // non-fatal; env is advisory, watcher rebinds on next tick
	case ActSetManagedSession:
		c.executeSetManagedSession(ctx, a)
	case ActClearPrecompact:
		_ = c.context.ClearPrecompactTrigger() //nolint:errcheck // non-fatal; a stale precompact trigger is re-cleared next cycle
	case ActForceRestart:
		c.executeForceRestart(ctx)
	case ActArmTimer:
		c.executeArmTimer(a)
	case ActCancelTimer:
		delete(c.timers, a.Timer)
	}
	return nil
}

func (c *Cycler) executeWriteJournal(a Action) error {
	j := a.Journal
	if err := c.journal.Write(&j); err != nil {
		if j.Phase == "opened" {
			return err // fatal: the cycle must not start unjournaled
		}
	}
	return nil
}

func (c *Cycler) executeInjectHandoffCmd(ctx context.Context, a Action) {
	c.handoffInjectedAt = c.cfg.Clock.Now()
	handoffCmd := handoffDirective(c.handoff.Path(), nonceMarker(a.CycleID))
	_ = c.pane.Inject(ctx, c.cfg.TmuxTarget, handoffCmd) //nolint:errcheck // non-fatal; the nonce-confirm step catches a dropped injection
}

func (c *Cycler) executeSetManagedSession(ctx context.Context, a Action) {
	if err := c.context.SetManagedSession(a.SID); err != nil {
		slog.WarnContext(ctx, "keeper: update managed session_id",
			"agent", c.cfg.AgentName, "sid", a.SID, "err", err)
	}
}

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
	}
}

func (c *Cycler) executeArmTimer(a Action) {
	if a.Timer == TimerHandoffTimeout && c.cfg.TmuxTarget == "" {
		c.handoffInjectedAt = c.cfg.Clock.Now()
	}
	if c.timers == nil {
		c.timers = make(map[TimerKind]time.Time)
	}
	c.timers[a.Timer] = c.cfg.Clock.Now().Add(a.D)
	c.timersArmed = true
}

func (c *Cycler) feed(ctx context.Context, ev Event) error {
	for _, a := range c.machine.Step(ev) {
		if err := c.execute(ctx, a); err != nil {
			c.machine.failOpen()
			return err
		}
	}
	return nil
}

func (c *Cycler) runEntry(ctx context.Context, ev Event) error {
	if c.machine.peekFires(ev) {
		ev.CycleID = c.cycleIDs.Next()
		content, err := c.handoff.Read()
		ev.HandoffContent = content
		ev.HandoffReadOK = err == nil
	}
	if err := c.feed(ctx, ev); err != nil {
		return err
	}
	return c.drive(ctx)
}

func (c *Cycler) drive(ctx context.Context) error {
	for c.machine.InCycle() {
		ticker := c.cfg.Clock.NewTicker(c.cfg.PollInterval)
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

func (c *Cycler) pollAwaitingHandoff(ctx context.Context, st CycleState, at time.Time) {
	content, readErr := c.handoff.Read()
	nonceSeen := readErr == nil && strings.Contains(content, nonceMarker(st.CycleID))
	operatorTurn := c.recentOperatorTurn(st, at)
	if nonceSeen {
		if operatorTurn {
			c.parkForOperator(ctx, st, at)
			return
		}
		_ = c.feed(ctx, Event{Kind: EvNonceObserved, CycleID: st.CycleID, At: at}) //nolint:errcheck // a failed poll event leaves the cycle safe
		return
	}
	if dl, ok := c.timers[TimerHandoffTimeout]; ok && !at.Before(dl) {
		delete(c.timers, TimerHandoffTimeout)
		if _, fresh := c.observeHandoffFreshness(); fresh && operatorTurn {
			c.parkForOperator(ctx, st, at)
			return
		}
		c.sampleHandoffFreshness(ctx, st, at)
		_ = c.feed(ctx, Event{Kind: EvTimerFired, Timer: TimerHandoffTimeout, CycleID: st.CycleID, At: at}) //nolint:errcheck // non-fatal; a poll-fed event fails the cycle open, never the poll tick
	}
}

const injectionArtifactWindow = 2 * time.Second

func (c *Cycler) recentOperatorTurn(st CycleState, at time.Time) bool {
	if c.cfg.OperatorTurnLookback <= 0 || st.PrevSID == "" {
		return false
	}
	turnAt, ok := c.activity.LastUserTurn(st.PrevSID)
	if !ok || turnAt.After(at) || at.Sub(turnAt) > c.cfg.OperatorTurnLookback {
		return false
	}
	return st.InjectedAt.IsZero() || turnAt.After(st.InjectedAt.Add(injectionArtifactWindow))
}

func (c *Cycler) parkForOperator(ctx context.Context, st CycleState, at time.Time) {
	slog.WarnContext(ctx, "keeper: cycle parked because a recent operator turn arrived during handoff wait",
		"agent", c.cfg.AgentName, "cycle_id", st.CycleID, "session_id", st.PrevSID)
	_ = c.feed(ctx, Event{Kind: EvOperatorTurnRecent, CycleID: st.CycleID, At: at}) //nolint:errcheck // a failed poll event leaves the cycle safe
}

func (c *Cycler) pollAwaitModelDone(ctx context.Context, st CycleState, at time.Time) {
	if dl, ok := c.timers[TimerModelDone]; ok && !at.Before(dl) {
		delete(c.timers, TimerModelDone)
		_ = c.feed(ctx, Event{Kind: EvTimerFired, Timer: TimerModelDone, CycleID: st.CycleID, At: at}) //nolint:errcheck // non-fatal; a poll-fed event fails the cycle open, never the poll tick
		return
	}
	if mt, ok := c.activity.IdleMarkerModTime(); ok && !mt.Before(st.NonceConfirmedAt) {
		_ = c.feed(ctx, Event{ //nolint:errcheck // non-fatal; a poll-fed event fails the cycle open, never the poll tick
			Kind: EvModelDone, CycleID: st.CycleID,
			SessionID: st.PrevSID, Source: "idle_marker", At: at,
		})
		return
	}
	if tt, ok := c.activity.LastAssistantTurn(st.PrevSID); ok && !tt.Before(st.NonceConfirmedAt) {
		_ = c.feed(ctx, Event{ //nolint:errcheck // non-fatal; a poll-fed event fails the cycle open, never the poll tick
			Kind: EvModelDone, CycleID: st.CycleID,
			SessionID: st.PrevSID, Source: "transcript_turn", At: at,
		})
	}
}

func (c *Cycler) pollClearing(ctx context.Context, st CycleState, at time.Time) {
	if dl, ok := c.timers[TimerClearSettle]; ok && !at.Before(dl) {
		if bdl, bok := c.timers[TimerClearBackstop]; bok && !at.Before(bdl) {
			_ = c.feed(ctx, Event{Kind: EvTimerFired, Timer: TimerClearBackstop, CycleID: st.CycleID, At: at}) //nolint:errcheck // non-fatal; a poll-fed event fails the cycle open, never the poll tick
			return
		}
		clearCF, _, gerr := c.context.ReadGauge()
		if gerr != nil {
			clearCF = nil
		}
		_ = c.feed(ctx, Event{Kind: EvTimerFired, Timer: TimerClearSettle, CycleID: st.CycleID, At: at, CF: clearCF}) //nolint:errcheck // non-fatal; a poll-fed event fails the cycle open, never the poll tick
		return
	}
	cf, _, err := c.context.ReadGauge()
	if err == nil && cf.SessionID != "" && cf.SessionID != st.PrevSID {
		_ = c.feed(ctx, Event{ //nolint:errcheck // non-fatal; a poll-fed event fails the cycle open, never the poll tick
			Kind: EvSessionChanged, CycleID: st.CycleID,
			PrevSID: st.PrevSID, NewSID: cf.SessionID, At: at,
		})
	}
}

func (c *Cycler) fireOnCancel(ctx context.Context) {
	at := c.cfg.Clock.Now()
	st := c.machine.State()
	switch st.Phase {
	case PhaseAwaitingHandoff:
		delete(c.timers, TimerHandoffTimeout)
		c.sampleHandoffFreshness(ctx, st, at)
		_ = c.feed(ctx, Event{Kind: EvTimerFired, Timer: TimerHandoffTimeout, CycleID: st.CycleID, At: at}) //nolint:errcheck // non-fatal; a poll-fed event fails the cycle open, never the poll tick
	case PhaseAwaitModelDone:
		delete(c.timers, TimerModelDone)
		_ = c.feed(ctx, Event{Kind: EvTimerFired, Timer: TimerModelDone, CycleID: st.CycleID, At: at}) //nolint:errcheck // non-fatal; a poll-fed event fails the cycle open, never the poll tick
	case PhaseClearing:
		_ = c.feed(ctx, Event{Kind: EvTimerFired, Timer: TimerClearBackstop, CycleID: st.CycleID, At: at}) //nolint:errcheck // non-fatal; a poll-fed event fails the cycle open, never the poll tick
	default:
	}
}

func (c *Cycler) observeHandoffFreshness() (time.Time, bool) {
	content, err := c.handoff.Read()
	if err != nil || strings.TrimSpace(content) == "" {
		return time.Time{}, false
	}
	mt, ok := c.handoff.ModTime()
	if !ok || mt.Before(c.handoffInjectedAt) {
		return time.Time{}, false
	}
	return mt, true
}

func (c *Cycler) sampleHandoffFreshness(ctx context.Context, st CycleState, at time.Time) {
	mt, ok := c.observeHandoffFreshness()
	if !ok {
		return
	}
	slog.WarnContext(ctx, "keeper: nonce echo timed out but a fresh handoff was written — recovering (proceeding with /clear + brief)",
		"agent", c.cfg.AgentName, "cycle_id", st.CycleID, "session_id", st.PrevSID)
	_ = c.feed(ctx, Event{Kind: EvHandoffFreshSeen, CycleID: st.CycleID, Mtime: mt, At: at}) //nolint:errcheck // non-fatal; a poll-fed event fails the cycle open, never the poll tick
}
