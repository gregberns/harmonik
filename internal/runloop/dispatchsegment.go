package runloop

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/handlercontract"
	"github.com/gregberns/harmonik/internal/runexec"
	"github.com/gregberns/harmonik/internal/substrate"
)

const resumeReadyProbeDelay = 2 * time.Second

// dispatchSegmentInputAckWindow is the provisional M3-D11 input-ack bound. The
// transitional tmux implementation acks synthetically immediately after the
// paste-inject deliver hook returns, so this timer never fires today; the M2
// agent-input driver supplies the real per-submission bound at this seam
// ([agent-input.md] AIS-INV-001).
const DispatchSegmentInputAckWindow = 30 * time.Second

// dispatchSegment binds one agent launch onto the Dispatch machine. All hook
// closures are site-owned; a nil hook is a no-op. Single-goroutine-owned by
// the run's own goroutine, like the shell it drives.
type DispatchSegment struct {
	Clock substrate.ClockPort
	RunID core.RunID
	// Config is exported temporarily solely for daemon/dot_cascade.go to read
	// SkipReadyHandshake after Run returns. That consumer remains in daemon
	// through LIFT L6; narrow Config when dot_cascade moves in LIFT L12.
	Config runexec.DispatchConfig

	// adapter detects agent_ready envelopes on the tap. nil (no adapter for the
	// resolved agent type) preserves the pre-RT8 "skip ready-wait" posture: a
	// synthetic EvAgentReady is fed immediately after launch so the brief is
	// still delivered without a wait.
	Adapter handlercontract.Adapter

	// probeResume arms the transitional resume readiness probe (M3-D7) once
	// launched with no watcher (the tmux substrate path).
	ProbeResume bool

	// tap is the per-run tapping emitter; the probe emits its run_id-stamped
	// agent_ready through it so the synthetic ready is bus-visible exactly like
	// the relay-synthesized one. tapCh is the tap subscription the ready pump
	// consumes (the channel waitAgentReady formerly blocked on).
	Tap   *PerRunEventTap
	TapCh <-chan core.EventEnvelope

	// launch performs the site's handler.Launch. It returns the watcher's Done
	// channel (nil on the tmux substrate path) — the shell converts its close
	// into EvAgentExited, replacing the pre-RT8 watcher-done → ready-ctx-cancel
	// fall-through.
	Launch func(ctx context.Context) (watcherDone <-chan struct{}, err error)

	OnLaunchFailed   func(ctx context.Context, err error)
	OnLaunched       func(ctx context.Context)
	Deliver          func(ctx context.Context)
	KillReady        func(ctx context.Context)
	KillAbort        func(ctx context.Context)
	EmitReadyTimeout func(ctx context.Context)

	// Stalls is the run's stall feed (StallFeed.Register). It carries the
	// shell-fed frozen-agent signals — EvNoChangeTimeout and EvHeartbeatStale —
	// that the machine turns into ActKillAgent during the Working phase. nil
	// leaves the run with no freeze protection, which is what every launch had
	// before this field existed.
	Stalls <-chan runexec.Event

	// KillStalled is the site's kill for a frozen agent. reason is the stall
	// signature the machine recorded, so the site's diagnostic can say WHICH
	// watchdog fired. nil falls back to KillAbort.
	KillStalled func(ctx context.Context, reason string)

	// stopWorkingWatch ends the Working-phase stall watch and waits for its
	// goroutine. Set by Run when the watch starts; nil otherwise.
	stopWorkingWatch func()

	SpawnCapTimeout      error
	TmuxNewWindowTimeout error
}

// Run drives the segment to Working-or-terminal and returns the machine state.
//
// When the machine settles into Working AND the run has a stall feed, the
// segment keeps the machine alive on a watch goroutine instead of releasing it.
// That is what makes the machine's stall edge reachable: the Working-phase
// completion wait belongs to the sub-driver, so before this the segment tore
// its shell down at exactly the moment the freeze protection would have been
// needed, and stepDispatchWorking's kill arm could never be stepped by anyone.
//
// The caller MUST call StopWorkingWatch when the run is over. That call ends the
// watch AND releases the two launch-phase helper goroutines, which are held to
// the same segment context for the same extended span.
func (g *DispatchSegment) Run(ctx context.Context) runexec.DispatchState {
	segCtx, segCancel := context.WithCancel(context.Background())
	watching := false
	defer func() {
		if !watching {
			segCancel()
		}
	}()

	r := &dispatchSegmentRun{
		g:      g,
		m:      runexec.NewDispatch(g.Config),
		events: make(chan runexec.Event),
		done:   segCtx.Done(),
	}
	go r.readyPump()

	r.sh = NewRunShell(g.Clock, RunEffectors{
		LaunchAgent:  r.launchAgent,
		DeliverInput: r.deliverInput,
		KillAgent:    r.killAgent,
		Emit:         r.emit,
	}, r.events)
	final := r.sh.RunDispatch(ctx, r.m, runexec.SessionRef(g.RunID.String()), "")

	if final.Phase == runexec.DispatchWorking && g.Stalls != nil {
		watching = true
		g.startWorkingWatch(r, segCancel) //nolint:contextcheck // the watch outlives this call by design: it runs across the sub-driver's completion wait on a segment-scoped context, and a ctx-derived one would end the freeze protection at the wrong moment
	}
	return final
}

func (g *DispatchSegment) startWorkingWatch(r *dispatchSegmentRun, segCancel context.CancelFunc) {
	stopped := make(chan struct{})
	g.stopWorkingWatch = sync.OnceFunc(func() {
		segCancel()
		<-stopped
	})

	go func() {
		defer close(stopped)
		defer segCancel()
		for {
			select {
			case <-r.done:
				return
			case ev, ok := <-g.Stalls:
				if !ok {
					return
				}
				if ev.At.IsZero() {
					ev.At = g.Clock.Now()
				}
				r.sh.feed(context.Background(), r.m, ev)
				if r.m.State().Phase != runexec.DispatchWorking {
					return
				}
			}
		}
	}()
}

// StopWorkingWatch ends the Working-phase stall watch and waits for its
// goroutine to finish, so no kill hook runs after the caller has torn the
// session down. It is idempotent and safe to call on a segment that never
// started a watch.
func (g *DispatchSegment) StopWorkingWatch() {
	if g.stopWorkingWatch != nil {
		g.stopWorkingWatch()
	}
}

type dispatchSegmentRun struct {
	g      *DispatchSegment
	m      *runexec.Dispatch
	sh     *RunShell
	events chan runexec.Event
	done   <-chan struct{}
}

func (r *dispatchSegmentRun) launchAgent(actx context.Context, _ runexec.SessionRef, _ string) {
	watcherDone, launchErr := r.g.Launch(actx)
	if launchErr != nil {
		if r.g.OnLaunchFailed != nil {
			r.g.OnLaunchFailed(actx, launchErr)
		}
		r.sh.pending = append(r.sh.pending, runexec.Event{
			Kind: runexec.EvLaunchFailed, Reason: r.g.classifyLaunchFailure(launchErr),
		})
		return
	}
	if watcherDone != nil {
		go r.watchWatcherExit(watcherDone)
	}
	if r.g.ProbeResume && watcherDone == nil {
		go r.resumeReadyProbe() //nolint:contextcheck // ClockPort wake + Background emit by design (see probe doc)
	}
	r.sh.pending = append(r.sh.pending, runexec.Event{Kind: runexec.EvLaunched})
	if r.g.Adapter == nil && !r.g.Config.SkipReadyHandshake {
		r.sh.pending = append(r.sh.pending, runexec.Event{Kind: runexec.EvAgentReady})
	}
}

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

func (r *dispatchSegmentRun) resumeReadyProbe() {
	select {
	case <-substrate.After(r.g.Clock, resumeReadyProbeDelay):
		if emitErr := r.g.Tap.EmitWithRunID(context.Background(), r.g.RunID, core.EventTypeAgentReady, nil); emitErr != nil {
			fmt.Fprintf(os.Stderr, "daemon: dispatchsegment: resume readiness probe emit run %s: %v (best-effort)\n",
				r.g.RunID.String(), emitErr)
		}
	case <-r.done:
	}
}

func (r *dispatchSegmentRun) deliverInput(actx context.Context, _ runexec.SessionRef, _ runexec.InputID, _ runexec.InputKind) {
	if r.g.Deliver != nil {
		r.g.Deliver(actx)
	}
	r.sh.pending = append(r.sh.pending, runexec.Event{Kind: runexec.EvInputAck})
}

func (r *dispatchSegmentRun) killAgent(actx context.Context, _ runexec.SessionRef) {
	st := r.m.State()
	if st.Phase == runexec.DispatchReadyTimeout {
		if r.g.KillReady != nil {
			r.g.KillReady(actx)
		}
		r.sh.pending = append(r.sh.pending, runexec.Event{Kind: runexec.EvAgentExited})
		return
	}
	if st.Phase == runexec.DispatchStalled && r.g.KillStalled != nil {
		r.g.KillStalled(actx, st.Reason)
		return
	}
	if r.g.KillAbort != nil {
		r.g.KillAbort(actx)
	}
}

func (r *dispatchSegmentRun) emit(actx context.Context, typ core.EventType, _ string) {
	switch typ {
	case core.EventTypeLaunchInitiated:
		if r.g.OnLaunched != nil {
			r.g.OnLaunched(actx)
		}
	case core.EventTypeAgentReadyTimeout:
		if r.g.EmitReadyTimeout != nil {
			r.g.EmitReadyTimeout(actx)
		}
	default:
	}
}

func (r *dispatchSegmentRun) readyPump() {
	for {
		select {
		case <-r.done:
			return
		case env, ok := <-r.g.TapCh:
			if !ok {
				return
			}
			if r.g.Adapter != nil && r.g.Adapter.DetectReady(env) {
				select {
				case r.events <- runexec.Event{Kind: runexec.EvAgentReady}:
				case <-r.done:
				}
				return
			}
		}
	}
}

func (g *DispatchSegment) classifyLaunchFailure(err error) string {
	switch {
	case g.SpawnCapTimeout != nil && errors.Is(err, g.SpawnCapTimeout):
		return string(core.EventTypeSpawnCapBlocked)
	case g.TmuxNewWindowTimeout != nil && errors.Is(err, g.TmuxNewWindowTimeout):
		return string(core.EventTypeTmuxNewWindowTimeout)
	default:
		return err.Error()
	}
}
