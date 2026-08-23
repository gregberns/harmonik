package runloop

import (
	"context"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/runexec"
	"github.com/gregberns/harmonik/internal/substrate"
)

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
type RunEffectors struct {
	// Asynchronous agent-session ops (fire-and-forget; results arrive on the tap).
	LaunchAgent   func(ctx context.Context, sess runexec.SessionRef, specRef string)
	DeliverInput  func(ctx context.Context, sess runexec.SessionRef, id runexec.InputID, kind runexec.InputKind)
	KillAgent     func(ctx context.Context, sess runexec.SessionRef)
	LifecycleTerm func(ctx context.Context, exitCode int, waitErr string)

	// Synchronous run ops (return follow-up events the shell enqueues).
	CreateWorktree func(ctx context.Context) []runexec.Event
	RunGate        func(ctx context.Context) []runexec.Event
	CheckEscape    func(ctx context.Context) []runexec.Event
	PrepareMerge   func(ctx context.Context)
	SubmitMerge    func(ctx context.Context, label string) []runexec.Event
	ReAmendTrailer func(ctx context.Context)
	CloseBead      func(ctx context.Context, summary string, needsAttention bool) []runexec.Event
	ReopenBead     func(ctx context.Context, reason string)

	// Emission ops.
	Emit            func(ctx context.Context, typ core.EventType, detail string)
	EmitRunTerminal func(ctx context.Context, success bool, summary string)
}

// runShell is the per-run imperative shell. It is single-goroutine-owned (the
// run's own goroutine); the pure machines it drives are not safe for concurrent
// use, and neither is this. timers holds the armed reactor deadlines
// (keeper shell.go:145 mechanics); pending holds synchronous follow-up events
// awaiting feed (drained before each select so a port result advances the
// machine without re-entrant Step).
type RunShell struct {
	clock  substrate.ClockPort
	eff    RunEffectors
	events <-chan runexec.Event // the per-run tap: async agent/watchdog signals

	timers  map[runexec.TimerKind]time.Time
	pending []runexec.Event
}

// newRunShell constructs a shell over the given clock, effector bundle, and
// per-run event tap. Any unset hook is defaulted to a no-op (best-effort policy)
// so the effector switch can call every arm unconditionally — a partially-wired
// shell (tests, transitional composition) stays drivable.
func NewRunShell(clock substrate.ClockPort, eff RunEffectors, events <-chan runexec.Event) *RunShell {
	eff.normalize()
	return &RunShell{
		clock:  clock,
		eff:    eff,
		events: events,
		timers: make(map[runexec.TimerKind]time.Time),
	}
}

func (e *RunEffectors) normalize() {
	noEvents := func(context.Context) []runexec.Event { return nil }
	if e.LaunchAgent == nil {
		e.LaunchAgent = func(context.Context, runexec.SessionRef, string) {}
	}
	if e.DeliverInput == nil {
		e.DeliverInput = func(context.Context, runexec.SessionRef, runexec.InputID, runexec.InputKind) {}
	}
	if e.KillAgent == nil {
		e.KillAgent = func(context.Context, runexec.SessionRef) {}
	}
	if e.LifecycleTerm == nil {
		e.LifecycleTerm = func(context.Context, int, string) {}
	}
	if e.CreateWorktree == nil {
		e.CreateWorktree = noEvents
	}
	if e.RunGate == nil {
		e.RunGate = noEvents
	}
	if e.CheckEscape == nil {
		e.CheckEscape = noEvents
	}
	if e.PrepareMerge == nil {
		e.PrepareMerge = func(context.Context) {}
	}
	if e.SubmitMerge == nil {
		e.SubmitMerge = func(context.Context, string) []runexec.Event { return nil }
	}
	if e.ReAmendTrailer == nil {
		e.ReAmendTrailer = func(context.Context) {}
	}
	if e.CloseBead == nil {
		e.CloseBead = func(context.Context, string, bool) []runexec.Event { return nil }
	}
	if e.ReopenBead == nil {
		e.ReopenBead = func(context.Context, string) {}
	}
	if e.Emit == nil {
		e.Emit = func(context.Context, core.EventType, string) {}
	}
	if e.EmitRunTerminal == nil {
		e.EmitRunTerminal = func(context.Context, bool, string) {}
	}
}

func (sh *RunShell) execute(ctx context.Context, a runexec.Action) {
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

func (sh *RunShell) executeAgentAction(ctx context.Context, a runexec.Action) {
	switch a.Kind {
	case runexec.ActLaunchAgent:
		sh.eff.LaunchAgent(ctx, a.Session, a.SpecRef)
	case runexec.ActDeliverInput:
		sh.eff.DeliverInput(ctx, a.Session, a.InputID, a.InputKind)
	case runexec.ActKillAgent:
		sh.eff.KillAgent(ctx, a.Session)
	case runexec.ActDriveLifecycleTerminated:
		sh.eff.LifecycleTerm(ctx, a.ExitCode, a.WaitErr)
	default: // routed elsewhere by execute
	}
}

func (sh *RunShell) executeRunAction(ctx context.Context, a runexec.Action) {
	switch a.Kind {
	case runexec.ActCreateWorktree:
		sh.pending = append(sh.pending, sh.eff.CreateWorktree(ctx)...)
	case runexec.ActRunGate:
		sh.pending = append(sh.pending, sh.eff.RunGate(ctx)...)
	case runexec.ActCheckEscape:
		sh.pending = append(sh.pending, sh.eff.CheckEscape(ctx)...)
	case runexec.ActPrepareMerge:
		sh.eff.PrepareMerge(ctx)
	case runexec.ActSubmitMerge:
		sh.pending = append(sh.pending, sh.eff.SubmitMerge(ctx, a.Label)...)
	case runexec.ActReAmendTrailer:
		sh.eff.ReAmendTrailer(ctx)
	case runexec.ActCloseBead:
		sh.pending = append(sh.pending, sh.eff.CloseBead(ctx, a.Summary, a.NeedsAttention)...)
	case runexec.ActReopenBead:
		sh.eff.ReopenBead(ctx, a.Reason)
	default: // routed elsewhere by execute
	}
}

func (sh *RunShell) executeEmitOrTimer(ctx context.Context, a runexec.Action) {
	switch a.Kind {
	case runexec.ActEmit:
		sh.eff.Emit(ctx, a.Type, a.Detail)
	case runexec.ActEmitRunTerminal:
		sh.eff.EmitRunTerminal(ctx, a.Success, a.Summary)
	case runexec.ActArmTimer:
		sh.timers[a.Timer] = sh.clock.Now().Add(a.D)
	case runexec.ActCancelTimer:
		delete(sh.timers, a.Timer)
	default: // routed elsewhere by execute
	}
}

func (sh *RunShell) feed(ctx context.Context, m runReactor, ev runexec.Event) {
	for _, a := range m.Step(ev) {
		sh.execute(ctx, a)
	}
}

func (sh *RunShell) drive(ctx context.Context, m runReactor) {
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

func (sh *RunShell) drainPending(ctx context.Context, m runReactor) bool {
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

func (sh *RunShell) driveOnce(ctx context.Context, m runReactor) {
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

func (sh *RunShell) nearestDeadline() (time.Duration, bool) {
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

func (sh *RunShell) fireElapsedTimers(ctx context.Context, m runReactor) {
	now := sh.clock.Now()
	for kind, dl := range sh.timers {
		if now.Before(dl) {
			continue
		}
		delete(sh.timers, kind)
		sh.feed(ctx, m, runexec.Event{Kind: runexec.EvTimerFired, Timer: kind, At: now})
	}
}

func (sh *RunShell) fireOnCancel(ctx context.Context, m runReactor) {
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
func (sh *RunShell) RunDispatch(ctx context.Context, m *runexec.Dispatch, sess runexec.SessionRef, specRef string) runexec.DispatchState {
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

func dispatchSegmentActive(m *runexec.Dispatch) bool {
	return m.InFlight() && m.State().Phase != runexec.DispatchWorking
}

func (sh *RunShell) driveDispatchOnce(ctx context.Context, m *runexec.Dispatch) {
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
func (sh *RunShell) DriveRun(ctx context.Context, m *runexec.Run, mode string) runexec.RunState {
	sh.feed(ctx, m, runexec.Event{Kind: runexec.EvStartRun, Mode: mode, At: sh.clock.Now()})
	sh.drive(ctx, m)
	return m.State()
}
