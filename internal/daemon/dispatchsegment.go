package daemon

// dispatchsegment.go — the RT8 launch/ready/brief segment adaptor: it binds one
// sub-driver agent launch (review-loop implementer/reviewer, DOT agentic node)
// onto the pure runexec Dispatch machine driven by shell.RunDispatch
// (RSM-004/005, RSM-INV-002; runexec-design §5).
//
// Pre-RT8 these sites open-coded Launch + waitAgentReady + (for the review-loop
// resume phase) a fixed 2s ready-fallback grace caulk. RT8 dissolves that: the
// machine's TimerAgentReady — armed at Idle→Launching on the ClockPort — is the
// ONE ready bound for every dispatch, fresh or resume, review-loop or DOT
// (M3-D7: DOT back-edge resumes gain the bound). The ready-timeout edge is
// structural (kill + reap + agent_ready_timeout emission, never a silent
// wait), and being ClockPort-timed it is FakeClock-drivable in tests, which
// the wall-clock time.After inside waitAgentReady never was.
//
// The segment covers Idle → … → Working (or a failure terminal). The
// Working-phase completion wait (waitWithSocketGrace + the frozen commit
// watchdog) stays with the sub-driver's imperative control flow until the
// M5-adjacent full reactorization (00-decisions "Open items", M3-D2).
//
// Effector binding (runexec-design §2: the hooks own only the binding; the
// per-site failure strings/emissions stay with their sites for RSM-029 parity):
//   ActLaunchAgent  → launch closure (the site's handler.Launch call)
//   ActEmit(launch_initiated) → onLaunched (held-back emit + post-launch wiring)
//   ActDeliverInput → deliver (hang-detector arm + paste-inject) + a synthetic
//                     EvInputAck — the M3-D11 transitional tmux ack; the M2
//                     agent-input driver replaces it at this seam
//   ActKillAgent    → killReady (ready-timeout kill+reap, phase ReadyTimeout)
//                     or killAbort (the EvAborted edge)
//   ActEmit(agent_ready_timeout) → emitReadyTimeout (site-verbatim emission;
//                     nil suppresses it — the reviewer phase never emitted one)
//
// Spec: specs/run-state-machine.md RSM-005, RSM-024, RSM-INV-002, RSM-029.
// Design: 04-design/runexec-design.md §5; 04-design/00-decisions.md M3-D7/D11.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/handlercontract"
	"github.com/gregberns/harmonik/internal/runexec"
	"github.com/gregberns/harmonik/internal/substrate"
)

// resumeReadyProbeDelay is the transitional readiness probe's splash-dismiss-
// class delay (M3-D7). A `claude --resume <uuid>` reattach does not reliably
// re-fire a SessionStart hook (hk-isq02), so under the tmux substrate the
// relay never synthesizes agent_ready for a resume; until the M2 agent-input
// driver provides a positive readiness signal, the probe emits a run_id-
// stamped agent_ready after this delay so the resume proceeds once the
// welcome splash has cleared (preserving the hk-kunm4 "do not paste before
// the REPL accepts input" invariant). Unlike the deleted fixed-grace caulk
// it replaces, the probe is NOT the liveness bound: the
// Dispatch machine's TimerAgentReady owns ready-or-fail (RSM-024's ready
// sub-bound), and a genuine relay agent_ready arriving first simply wins.
const resumeReadyProbeDelay = 2 * time.Second

// dispatchSegmentInputAckWindow is the provisional M3-D11 input-ack bound. The
// transitional tmux implementation acks synthetically immediately after the
// paste-inject deliver hook returns, so this timer never fires today; the M2
// agent-input driver supplies the real per-submission bound at this seam
// ([agent-input.md] AIS-INV-001).
const dispatchSegmentInputAckWindow = 30 * time.Second

// dispatchSegment binds one agent launch onto the Dispatch machine. All hook
// closures are site-owned; a nil hook is a no-op. Single-goroutine-owned by
// the run's own goroutine, like the shell it drives.
type dispatchSegment struct {
	clock substrate.ClockPort
	runID core.RunID
	cfg   runexec.DispatchConfig

	// adapter detects agent_ready envelopes on the tap. nil (no adapter for the
	// resolved agent type) preserves the pre-RT8 "skip ready-wait" posture: a
	// synthetic EvAgentReady is fed immediately after launch so the brief is
	// still delivered without a wait.
	adapter handlercontract.Adapter

	// probeResume arms the transitional resume readiness probe (M3-D7) once
	// launched with no watcher (the tmux substrate path).
	probeResume bool

	// tap is the per-run tapping emitter; the probe emits its run_id-stamped
	// agent_ready through it so the synthetic ready is bus-visible exactly like
	// the relay-synthesized one. tapCh is the tap subscription the ready pump
	// consumes (the channel waitAgentReady formerly blocked on).
	tap   *perRunEventTap
	tapCh <-chan core.EventEnvelope

	// launch performs the site's handler.Launch. It returns the watcher's Done
	// channel (nil on the tmux substrate path) — the shell converts its close
	// into EvAgentExited, replacing the pre-RT8 watcher-done → ready-ctx-cancel
	// fall-through.
	launch func(ctx context.Context) (watcherDone <-chan struct{}, err error)

	onLaunchFailed   func(ctx context.Context, err error)
	onLaunched       func(ctx context.Context)
	deliver          func(ctx context.Context)
	killReady        func(ctx context.Context)
	killAbort        func(ctx context.Context)
	emitReadyTimeout func(ctx context.Context)
}

// run drives the segment to Working-or-terminal and returns the machine state.
func (g *dispatchSegment) run(ctx context.Context) runexec.DispatchState {
	segCtx, segCancel := context.WithCancel(context.Background())
	defer segCancel()

	r := &dispatchSegmentRun{
		g:      g,
		m:      runexec.NewDispatch(g.cfg),
		events: make(chan runexec.Event),
		done:   segCtx.Done(),
	}
	go r.readyPump()

	r.sh = newRunShell(g.clock, runEffectors{
		launchAgent:  r.launchAgent,
		deliverInput: r.deliverInput,
		killAgent:    r.killAgent,
		emit:         r.emit,
	}, r.events)
	return r.sh.RunDispatch(ctx, r.m, runexec.SessionRef(g.runID.String()), "")
}

// dispatchSegmentRun is the per-run() wiring of one segment: the machine, the
// shell, the internal event channel, and the segment-scope done signal that
// releases the helper goroutines when run() returns. Extracted from run() so
// each effector arm is a small named method.
type dispatchSegmentRun struct {
	g      *dispatchSegment
	m      *runexec.Dispatch
	sh     *runShell
	events chan runexec.Event
	done   <-chan struct{}
}

// launchAgent is the ActLaunchAgent effector arm: the site's Launch, the
// watcher-exit event source, the transitional resume probe, and the EvLaunched
// (plus adapter-missing synthetic ready) follow-ups.
func (r *dispatchSegmentRun) launchAgent(actx context.Context, _ runexec.SessionRef, _ string) {
	watcherDone, launchErr := r.g.launch(actx)
	if launchErr != nil {
		if r.g.onLaunchFailed != nil {
			r.g.onLaunchFailed(actx, launchErr)
		}
		r.sh.pending = append(r.sh.pending, runexec.Event{
			Kind: runexec.EvLaunchFailed, Reason: classifyLaunchFailure(launchErr),
		})
		return
	}
	if watcherDone != nil {
		go r.watchWatcherExit(watcherDone)
	}
	if r.g.probeResume && watcherDone == nil {
		// M3-D7 transitional resume readiness probe (tmux path only; the
		// exec path's watcher provides crash detection and the relay fires
		// SessionStart on a fresh --session-id launch).
		go r.resumeReadyProbe() //nolint:contextcheck // ClockPort wake + Background emit by design (see probe doc)
	}
	r.sh.pending = append(r.sh.pending, runexec.Event{Kind: runexec.EvLaunched})
	if r.g.adapter == nil && !r.g.cfg.SkipReadyHandshake {
		// No adapter for the resolved agent type: pre-RT8 the sites skipped
		// the ready-wait but still delivered the brief — feed a synthetic
		// ready so the deliver hook runs without a wait.
		r.sh.pending = append(r.sh.pending, runexec.Event{Kind: runexec.EvAgentReady})
	}
}

// watchWatcherExit converts the exec-path watcher's Done close into
// EvAgentExited, replacing the pre-RT8 watcher-done → ready-ctx-cancel
// fall-through.
func (r *dispatchSegmentRun) watchWatcherExit(watcherDone <-chan struct{}) {
	select {
	case <-watcherDone:
		select {
		case r.events <- runexec.Event{Kind: runexec.EvAgentExited}:
		case <-r.done:
		}
	case <-r.done:
	}
}

// resumeReadyProbe emits the run_id-stamped synthetic agent_ready after the
// splash-dismiss delay (M3-D7). Run_id stamping is the RSM-029 sanctioned
// divergence from the deleted caulk's unattributed emit: the stale watcher's
// never-spawned reaper and the RX replay checkers can now join the synthetic
// ready to its run. The emit deliberately uses a Background context — the
// probe must not inherit a per-drive action context that may already be gone
// by the time the delay elapses.
func (r *dispatchSegmentRun) resumeReadyProbe() {
	select {
	case <-clockAfter(r.g.clock, resumeReadyProbeDelay):
		if emitErr := r.g.tap.EmitWithRunID(context.Background(), r.g.runID, core.EventTypeAgentReady, nil); emitErr != nil {
			fmt.Fprintf(os.Stderr, "daemon: dispatchsegment: resume readiness probe emit run %s: %v (best-effort)\n",
				r.g.runID.String(), emitErr)
		}
	case <-r.done:
	}
}

// deliverInput is the ActDeliverInput effector arm: the site's brief delivery
// followed by the M3-D11 transitional synthetic ack — the tmux paste-inject
// has no positive delivery confirmation; the M2 agent-input driver replaces
// this with the real Ack at the same seam.
func (r *dispatchSegmentRun) deliverInput(actx context.Context, _ runexec.SessionRef, _ runexec.InputID, _ runexec.InputKind) {
	if r.g.deliver != nil {
		r.g.deliver(actx)
	}
	r.sh.pending = append(r.sh.pending, runexec.Event{Kind: runexec.EvInputAck})
}

// killAgent is the ActKillAgent effector arm, split by phase: the RSM-005
// ready-timeout edge runs the site's full synchronous kill + watcher-reap +
// Wait sequence, so the reap completes before the agent_ready_timeout
// emission exactly as pre-RT8, and the follow-up EvAgentExited settles
// ReadyTimeout → Failed without waiting out the reap timer. Any other phase
// (the EvAborted edge) takes the plain kill.
func (r *dispatchSegmentRun) killAgent(actx context.Context, _ runexec.SessionRef) {
	if r.m.State().Phase == runexec.DispatchReadyTimeout {
		if r.g.killReady != nil {
			r.g.killReady(actx)
		}
		r.sh.pending = append(r.sh.pending, runexec.Event{Kind: runexec.EvAgentExited})
		return
	}
	if r.g.killAbort != nil {
		r.g.killAbort(actx)
	}
}

// emit is the ActEmit effector arm. launch_initiated routes to the site's
// onLaunched (held-back emit, hk-4l7zs semantics preserved: the machine emits
// it only on EvLaunched, plus the post-launch wiring — heartbeat loop,
// agent-ready callback). agent_ready_timeout routes to the site's verbatim
// emission (nil suppresses it). Everything else is suppressed:
// spawn_cap_blocked / tmux_new_window_timeout carry rich per-site payloads
// emitted by onLaunchFailed, and a generic launch error emits nothing
// (pre-RT8 parity).
func (r *dispatchSegmentRun) emit(actx context.Context, typ core.EventType, _ string) {
	switch typ {
	case core.EventTypeLaunchInitiated:
		if r.g.onLaunched != nil {
			r.g.onLaunched(actx)
		}
	case core.EventTypeAgentReadyTimeout:
		if r.g.emitReadyTimeout != nil {
			r.g.emitReadyTimeout(actx)
		}
	default:
	}
}

// readyPump converts the tap subscription into EvAgentReady, replacing
// waitAgentReady's observer goroutine. It exits after the first ready (the
// fan-out tap drops unconsumed events non-blockingly) or when the segment ends.
func (r *dispatchSegmentRun) readyPump() {
	for {
		select {
		case <-r.done:
			return
		case env, ok := <-r.g.tapCh:
			if !ok {
				return
			}
			if r.g.adapter != nil && r.g.adapter.DetectReady(env) {
				select {
				case r.events <- runexec.Event{Kind: runexec.EvAgentReady}:
				case <-r.done:
				}
				return
			}
		}
	}
}

// classifyLaunchFailure maps a launch error onto the EvLaunchFailed reason
// vocabulary (runexec-design §1): the two structural wedge classes keep their
// event-type names; anything else carries its message (the machine emits no
// event for it — see the emit hook above).
func classifyLaunchFailure(err error) string {
	switch {
	case errors.Is(err, ErrSpawnCapTimeout):
		return string(core.EventTypeSpawnCapBlocked)
	case errors.Is(err, ErrTmuxNewWindowTimeout):
		return string(core.EventTypeTmuxNewWindowTimeout)
	default:
		return err.Error()
	}
}
