// runshell.go — the imperative SHELL around the two pure runexec reactors
// (RSM-003/006/007, RSM-019; runexec-design §5). It is the composition root that
// owns the runexec Dispatch + Run machines for a SINGLE bead run: it samples I/O
// into Events, executes reactor Actions through the effector table, arms/cancels
// the reactor timers on the ClockPort, and pumps each machine to its terminal
// with a nearest-deadline wake and a ctx-cancel → phase-timeout mapping.
//
// It mirrors internal/keeper/shell.go structure exactly (execute switch / drive
// loop / nearestDeadline / fireOnCancel). The pure machines (internal/runexec)
// read no clock, mint no ids, and perform no I/O; every timestamp on a fed Event
// is shell-stamped from the ClockPort (RSM-001).
//
// RT7 status (single-mode migration): this file lands the shell scaffold — the
// effector table (per-action failure policy) and the two drive loops — composed
// over the RT4 RunPorts and a bundle of per-run effector hooks. The production
// hook bundle is assembled at the composition root inside beadRunOne; the
// single-mode P11–P18 re-drive that replaces the imperative block with
// shell.RunDispatch + driveRun, and the deferred WorktreePort/LaunchPort/
// BudgetPort per-run wiring, are threaded in the RT7→RT9 sequence. The shell is
// exercised end-to-end by a FakeClock ready-timeout→reopen test.

package daemon

import (
	"context"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/runexec"
	"github.com/gregberns/harmonik/internal/substrate"
)

// runReactor is the shared surface of both pure machines the shell drives
// (runexec.Dispatch and runexec.Run). Both expose Step + InFlight verbatim
// (runexec dispatch.go / run.go), so one drive loop pumps either.
type runReactor interface {
	Step(ev runexec.Event) []runexec.Action
	InFlight() bool
}

// runEffectors is the per-run effector-hook bundle. Each hook binds one Action
// group onto its concrete daemon side effect; the shell owns the SWITCH and the
// per-action FAILURE POLICY (runexec-design §5/§2), the hooks own only the
// binding. The synchronous port-backed operations (worktree create, gate,
// escape, merge submit, bead close) return the follow-up Event(s) the shell
// enqueues; the asynchronous agent-session operations (launch, deliver, kill,
// lifecycle-terminated) return nothing — their results arrive on the event tap
// from the relay/watchdog/wait goroutines.
//
// The bundle is assembled per-run at the composition root (beadRunOne) where the
// resolved remote-branch context (WorktreePort), the pre-built routed launch
// spec (LaunchPort), and the LockForMutation budget block (BudgetPort) are in
// scope. A nil hook is a no-op (best-effort policy), so a partially-wired shell
// stays drivable in tests.
type runEffectors struct {
	// Asynchronous agent-session ops (fire-and-forget; results arrive on the tap).
	launchAgent   func(ctx context.Context, sess runexec.SessionRef, specRef string)
	deliverInput  func(ctx context.Context, sess runexec.SessionRef, id runexec.InputID, kind runexec.InputKind)
	killAgent     func(ctx context.Context, sess runexec.SessionRef)
	lifecycleTerm func(ctx context.Context, exitCode int, waitErr string)

	// Synchronous run ops (return follow-up events the shell enqueues).
	createWorktree func(ctx context.Context) []runexec.Event
	runGate        func(ctx context.Context) []runexec.Event
	checkEscape    func(ctx context.Context) []runexec.Event
	prepareMerge   func(ctx context.Context)
	submitMerge    func(ctx context.Context, label string) []runexec.Event
	reAmendTrailer func(ctx context.Context)
	closeBead      func(ctx context.Context, summary string, needsAttention bool) []runexec.Event
	reopenBead     func(ctx context.Context, reason string)

	// Emission ops.
	emit            func(ctx context.Context, typ core.EventType, detail string)
	emitRunTerminal func(ctx context.Context, success bool, summary string)
}

// runShell is the per-run imperative shell. It is single-goroutine-owned (the
// run's own goroutine); the pure machines it drives are not safe for concurrent
// use, and neither is this. timers holds the armed reactor deadlines
// (keeper shell.go:145 mechanics); pending holds synchronous follow-up events
// awaiting feed (drained before each select so a port result advances the
// machine without re-entrant Step).
type runShell struct {
	clock  substrate.ClockPort
	eff    runEffectors
	events <-chan runexec.Event // the per-run tap: async agent/watchdog signals

	timers  map[runexec.TimerKind]time.Time
	pending []runexec.Event
}

// newRunShell constructs a shell over the given clock, effector bundle, and
// per-run event tap. Any unset hook is defaulted to a no-op (best-effort policy)
// so the effector switch can call every arm unconditionally — a partially-wired
// shell (tests, transitional composition) stays drivable.
func newRunShell(clock substrate.ClockPort, eff runEffectors, events <-chan runexec.Event) *runShell {
	eff.normalize()
	return &runShell{
		clock:  clock,
		eff:    eff,
		events: events,
		timers: make(map[runexec.TimerKind]time.Time),
	}
}

// normalize fills any nil hook with a no-op, so the effector switch needs no
// per-arm nil guard (best-effort failure policy: a missing binding is a no-op).
func (e *runEffectors) normalize() {
	noEvents := func(context.Context) []runexec.Event { return nil }
	if e.launchAgent == nil {
		e.launchAgent = func(context.Context, runexec.SessionRef, string) {}
	}
	if e.deliverInput == nil {
		e.deliverInput = func(context.Context, runexec.SessionRef, runexec.InputID, runexec.InputKind) {}
	}
	if e.killAgent == nil {
		e.killAgent = func(context.Context, runexec.SessionRef) {}
	}
	if e.lifecycleTerm == nil {
		e.lifecycleTerm = func(context.Context, int, string) {}
	}
	if e.createWorktree == nil {
		e.createWorktree = noEvents
	}
	if e.runGate == nil {
		e.runGate = noEvents
	}
	if e.checkEscape == nil {
		e.checkEscape = noEvents
	}
	if e.prepareMerge == nil {
		e.prepareMerge = func(context.Context) {}
	}
	if e.submitMerge == nil {
		e.submitMerge = func(context.Context, string) []runexec.Event { return nil }
	}
	if e.reAmendTrailer == nil {
		e.reAmendTrailer = func(context.Context) {}
	}
	if e.closeBead == nil {
		e.closeBead = func(context.Context, string, bool) []runexec.Event { return nil }
	}
	if e.reopenBead == nil {
		e.reopenBead = func(context.Context, string) {}
	}
	if e.emit == nil {
		e.emit = func(context.Context, core.EventType, string) {}
	}
	if e.emitRunTerminal == nil {
		e.emitRunTerminal = func(context.Context, bool, string) {}
	}
}

// execute is the reactor's effector: it maps one Action onto the per-run hooks
// with today's per-action failure policy (runexec-design §2/§5). Every side
// effect is best-effort — a nil hook is a no-op, and the only "fatal" run
// operations (worktree create, launch) FEED FAILURE EVENTS rather than erroring
// the drive loop (design §5), so execute never returns an error to substrate.Run.
func (sh *runShell) execute(ctx context.Context, a runexec.Action) {
	switch a.Kind {
	case runexec.ActLaunchAgent, runexec.ActDeliverInput, runexec.ActKillAgent,
		runexec.ActDriveLifecycleTerminated:
		sh.executeAgentAction(ctx, a)
	case runexec.ActEmit, runexec.ActEmitRunTerminal,
		runexec.ActArmTimer, runexec.ActCancelTimer:
		sh.executeEmitOrTimer(ctx, a)
	default:
		sh.executeRunAction(ctx, a)
	}
}

// executeAgentAction maps the Dispatch (agent-session) actions onto their hooks
// (all fire-and-forget; results arrive on the tap).
func (sh *runShell) executeAgentAction(ctx context.Context, a runexec.Action) {
	switch a.Kind {
	case runexec.ActLaunchAgent:
		sh.eff.launchAgent(ctx, a.Session, a.SpecRef)
	case runexec.ActDeliverInput:
		sh.eff.deliverInput(ctx, a.Session, a.InputID, a.InputKind)
	case runexec.ActKillAgent:
		sh.eff.killAgent(ctx, a.Session)
	case runexec.ActDriveLifecycleTerminated:
		sh.eff.lifecycleTerm(ctx, a.ExitCode, a.WaitErr)
	default: // routed elsewhere by execute
	}
}

// executeRunAction maps the Run actions; the synchronous port ops (worktree
// create, gate, escape, merge submit, bead close) enqueue their follow-up events.
func (sh *runShell) executeRunAction(ctx context.Context, a runexec.Action) {
	switch a.Kind {
	case runexec.ActCreateWorktree:
		sh.pending = append(sh.pending, sh.eff.createWorktree(ctx)...)
	case runexec.ActRunGate:
		sh.pending = append(sh.pending, sh.eff.runGate(ctx)...)
	case runexec.ActCheckEscape:
		sh.pending = append(sh.pending, sh.eff.checkEscape(ctx)...)
	case runexec.ActPrepareMerge:
		sh.eff.prepareMerge(ctx)
	case runexec.ActSubmitMerge:
		sh.pending = append(sh.pending, sh.eff.submitMerge(ctx, a.Label)...)
	case runexec.ActReAmendTrailer:
		sh.eff.reAmendTrailer(ctx)
	case runexec.ActCloseBead:
		sh.pending = append(sh.pending, sh.eff.closeBead(ctx, a.Summary, a.NeedsAttention)...)
	case runexec.ActReopenBead:
		sh.eff.reopenBead(ctx, a.Reason)
	default: // routed elsewhere by execute
	}
}

// executeEmitOrTimer maps the emission and shared timer actions. Timer arms are
// anchored at execution time (after any preceding actions in the batch), like
// keeper shell.go:145.
func (sh *runShell) executeEmitOrTimer(ctx context.Context, a runexec.Action) {
	switch a.Kind {
	case runexec.ActEmit:
		sh.eff.emit(ctx, a.Type, a.Detail)
	case runexec.ActEmitRunTerminal:
		sh.eff.emitRunTerminal(ctx, a.Success, a.Summary)
	case runexec.ActArmTimer:
		sh.timers[a.Timer] = sh.clock.Now().Add(a.D)
	case runexec.ActCancelTimer:
		delete(sh.timers, a.Timer)
	default: // routed elsewhere by execute
	}
}

// feed steps the machine on one event and executes the resulting actions in
// order (which may enqueue synchronous follow-ups into sh.pending).
func (sh *runShell) feed(ctx context.Context, m runReactor, ev runexec.Event) {
	for _, a := range m.Step(ev) {
		sh.execute(ctx, a)
	}
}

// drive pumps a reactor to its terminal (keeper shell.go:225 pattern). Each
// outer generation drains any synchronous port follow-ups, then blocks on the
// per-run event tap, a nearest-deadline wake (so a timer fires punctually even
// under a delayed scheduler), or ctx cancellation (mapped onto the live phase
// timeout edge — never a silent wait, RSM-INV-002).
func (sh *runShell) drive(ctx context.Context, m runReactor) {
	for m.InFlight() {
		if sh.drainPending(ctx, m) {
			continue
		}
		if !m.InFlight() {
			return
		}
		sh.driveOnce(ctx, m)
	}
}

// drainPending feeds all queued synchronous follow-up events; returns true when
// it fed at least one (so drive re-checks InFlight before blocking).
func (sh *runShell) drainPending(ctx context.Context, m runReactor) bool {
	if len(sh.pending) == 0 {
		return false
	}
	queued := sh.pending
	sh.pending = nil
	for _, ev := range queued {
		sh.feed(ctx, m, ev)
	}
	return true
}

// driveOnce blocks for one external signal and feeds it.
func (sh *runShell) driveOnce(ctx context.Context, m runReactor) {
	var deadlineC <-chan time.Time
	var deadlineTicker substrate.Ticker
	if remaining, ok := sh.nearestDeadline(); ok {
		deadlineTicker = sh.clock.NewTicker(remaining)
		deadlineC = deadlineTicker.C()
	}
	defer func() {
		if deadlineTicker != nil {
			deadlineTicker.Stop()
		}
	}()

	select {
	case <-ctx.Done():
		sh.fireOnCancel(ctx, m)
	case ev, ok := <-sh.events:
		if !ok {
			// Tap closed with the machine still in flight: cancel-equivalent so
			// the run terminates on its phase edge rather than spinning.
			sh.fireOnCancel(ctx, m)
			return
		}
		if ev.At.IsZero() {
			ev.At = sh.clock.Now()
		}
		sh.feed(ctx, m, ev)
	case <-deadlineC:
		sh.fireElapsedTimers(ctx, m)
	}
}

// nearestDeadline returns the remaining duration to the earliest armed reactor
// deadline (keeper shell.go:262 mechanics), clamped to 1ns (NewTicker needs
// d>0). ok is false when no timer is armed.
func (sh *runShell) nearestDeadline() (time.Duration, bool) {
	var best time.Time
	var ok bool
	for _, dl := range sh.timers {
		if !ok || dl.Before(best) {
			best, ok = dl, true
		}
	}
	if !ok {
		return 0, false
	}
	remaining := best.Sub(sh.clock.Now())
	if remaining <= 0 {
		remaining = time.Nanosecond
	}
	return remaining, true
}

// fireElapsedTimers feeds an EvTimerFired for every timer whose deadline has
// elapsed (the reactor's timer edge; RSM-INV-002 guarantees each is an outgoing
// action, never silence). The timer is disarmed before feeding so its edge is
// not re-fired.
func (sh *runShell) fireElapsedTimers(ctx context.Context, m runReactor) {
	now := sh.clock.Now()
	for kind, dl := range sh.timers {
		if now.Before(dl) {
			continue
		}
		delete(sh.timers, kind)
		sh.feed(ctx, m, runexec.Event{Kind: runexec.EvTimerFired, Timer: kind, At: now})
	}
}

// fireOnCancel maps a parent-ctx cancellation onto the live phase-timeout edge
// (keeper shell.go:370): it fires the nearest armed timer so a cancelled run
// rides the same fail-closed edge as its natural timeout (kill + reopen), never
// stranding a machine mid-flight. With no timer armed there is no phase edge to
// take; the caller's outer InFlight loop exits when the tap closes.
func (sh *runShell) fireOnCancel(ctx context.Context, m runReactor) {
	var bestKind runexec.TimerKind
	var best time.Time
	var ok bool
	for kind, dl := range sh.timers {
		if !ok || dl.Before(best) {
			bestKind, best, ok = kind, dl, true
		}
	}
	if !ok {
		return
	}
	delete(sh.timers, bestKind)
	sh.feed(ctx, m, runexec.Event{Kind: runexec.EvTimerFired, Timer: bestKind, At: sh.clock.Now()})
}

// RunDispatch drives one Dispatch instance through its launch/ready/brief
// segment and returns the resulting state (runexec-design §5: the sub-drivers
// call this instead of open-coded Launch+waitAgentReady+caulk). The shell
// feeds EvStartDispatch to launch, then pumps the tap/timer/cancel loop until
// the machine is terminal OR has settled into Working — the RT8 segment
// boundary: the Working-phase completion wait (waitWithSocketGrace + the
// frozen commit watchdog) stays with the sub-driver until the M5-adjacent
// full-reactorization (00-decisions "Open items"). Every failure class
// (launch failure, the RSM-005 ready-timeout edge, input undeliverable,
// abort) reaches its terminal INSIDE this loop, so the SR9 bound is owned by
// the machine's TimerAgentReady on the ClockPort, never a wall-clock wait.
//
// Cancellation maps onto the Dispatch machine's uniform EvAborted edge
// (runexec-design §3 "any non-terminal") rather than fireOnCancel's
// fire-nearest-timer mapping: pre-RT8 a ctx cancel during the ready wait fell
// through WITHOUT emitting agent_ready_timeout, and firing the ready timer
// here would fabricate that emission on every shutdown (an unsanctioned
// stream divergence, RSM-029).
func (sh *runShell) RunDispatch(ctx context.Context, m *runexec.Dispatch, sess runexec.SessionRef, specRef string) runexec.DispatchState {
	sh.feed(ctx, m, runexec.Event{Kind: runexec.EvStartDispatch, Session: sess, Detail: specRef, At: sh.clock.Now()})
	for dispatchSegmentActive(m) {
		if sh.drainPending(ctx, m) {
			continue
		}
		if !dispatchSegmentActive(m) {
			break
		}
		sh.driveDispatchOnce(ctx, m)
	}
	return m.State()
}

// dispatchSegmentActive reports whether the RunDispatch segment loop must keep
// pumping: the machine is in flight and has not yet reached Working (the RT8
// segment boundary).
func dispatchSegmentActive(m *runexec.Dispatch) bool {
	return m.InFlight() && m.State().Phase != runexec.DispatchWorking
}

// driveDispatchOnce blocks for one external signal and feeds it — driveOnce's
// dispatch-segment variant: ctx cancellation (and a closed tap) map onto the
// machine's EvAborted edge instead of fireOnCancel (see RunDispatch).
func (sh *runShell) driveDispatchOnce(ctx context.Context, m *runexec.Dispatch) {
	var deadlineC <-chan time.Time
	var deadlineTicker substrate.Ticker
	if remaining, ok := sh.nearestDeadline(); ok {
		deadlineTicker = sh.clock.NewTicker(remaining)
		deadlineC = deadlineTicker.C()
	}
	defer func() {
		if deadlineTicker != nil {
			deadlineTicker.Stop()
		}
	}()

	select {
	case <-ctx.Done():
		sh.feed(ctx, m, runexec.Event{Kind: runexec.EvAborted, Reason: "context cancelled", At: sh.clock.Now()})
	case ev, ok := <-sh.events:
		if !ok {
			sh.feed(ctx, m, runexec.Event{Kind: runexec.EvAborted, Reason: "event tap closed", At: sh.clock.Now()})
			return
		}
		if ev.At.IsZero() {
			ev.At = sh.clock.Now()
		}
		sh.feed(ctx, m, ev)
	case <-deadlineC:
		sh.fireElapsedTimers(ctx, m)
	}
}

// driveRun feeds EvStartRun (guards already passed shell-side) and pumps the Run
// machine to its Done terminal, returning the terminal state (whose Success the
// shell reads for group advancement / worktree retention, RSM-022).
func (sh *runShell) driveRun(ctx context.Context, m *runexec.Run, mode string) runexec.RunState {
	sh.feed(ctx, m, runexec.Event{Kind: runexec.EvStartRun, Mode: mode, At: sh.clock.Now()})
	sh.drive(ctx, m)
	return m.State()
}
