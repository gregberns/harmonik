package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/gitprobe"
	"github.com/gregberns/harmonik/internal/handler"
	"github.com/gregberns/harmonik/internal/handlercontract"
	"github.com/gregberns/harmonik/internal/harness/codex"
	"github.com/gregberns/harmonik/internal/harness/pi"
	"github.com/gregberns/harmonik/internal/harness/shared"
	tmuxpkg "github.com/gregberns/harmonik/internal/lifecycle/tmux"
	"github.com/gregberns/harmonik/internal/runexec"
	"github.com/gregberns/harmonik/internal/runlaunch"
	"github.com/gregberns/harmonik/internal/runlease"
	"github.com/gregberns/harmonik/internal/runloop"
	"github.com/gregberns/harmonik/internal/substrate"
)

type agentLaunchFail int

const (
	agentLaunchOK agentLaunchFail = iota

	agentLaunchPrelaunchFailed

	agentLaunchErrored

	agentLaunchReadyTimeout
)

type agentDeliverCtx struct {
	// Session is the live session. Non-nil whenever Deliver is called.
	Session handler.Session

	// PasteTarget is the substrate to paste the brief into. It is NIL when the
	// resolved harness captures its own session id (codex, pi): those harnesses
	// receive their task via argv and run on the exec path with no tmux pane, so
	// a paste would land nowhere ("seed marker absent" → pasteinject_failed).
	PasteTarget handler.Substrate

	// Tap is the per-run fan-out event tap. Watchdogs MUST take their own
	// tap.Subscribe() channel rather than sharing the ready pump's (hk-37giq).
	Tap *runloop.PerRunEventTap

	// ProcessExit reports whether the resolved harness self-terminates on turn
	// completion (codex, pi) rather than waiting for a /quit injection.
	ProcessExit bool
}

type agentLaunchInput struct {
	Env     runloop.RunEnv
	Ports   runloop.RunPorts
	Handles runloop.SharedHandles

	RunID core.RunID

	// LogPrefix identifies this launch in stderr diagnostics, e.g.
	// `daemon: dot: bead hk-x run 01J…`. Formatted messages are appended to it.
	LogPrefix string

	// Spec and Artifacts are the built launch spec and its artifacts. Spec is
	// taken by value and mutated in here (substrate, argv wrap, stdout wrapper,
	// terminal flag); the caller's copy is untouched.
	Spec      handler.LaunchSpec
	Artifacts shared.LaunchArtifacts

	WorktreePath string
	DaemonSocket string

	// Runner is the per-run CommandRunner: an SSHRunner for a remote run, nil
	// for a local one.
	Runner tmuxpkg.CommandRunner

	// Remote marks a run whose agent executes on a worker host. It selects the
	// longer agent-ready window, arms the D2 credential refusal, and takes the
	// cold-start token. Those three are the whole remote/local difference in this
	// function, and they all read this one field rather than three spellings of
	// it.
	Remote bool

	// BaseSubstrate is the substrate to wrap per-run. The caller has already
	// made any reviewer-substrate choice (hk-qxvc2).
	BaseSubstrate handler.Substrate

	// WorkerSessionName / WorkerSessionCwd tell the per-run substrate which tmux
	// session to ensure and spawn into ON THE WORKER, and the cwd to create it
	// with. Applied only for a remote run (hk-538l).
	WorkerSessionName string
	WorkerSessionCwd  string

	// ConfigurePerRunSubstrate is site-specific per-run substrate wiring applied
	// right after the wrap (single-mode's worker-offline callback and its
	// independent-session/run-registry setup). nil for sites with none.
	ConfigurePerRunSubstrate func(prs *perRunSubstrate)

	// Terminal marks a spawn that draws from the reserved +1 spawn slot: the
	// single-mode implementer always, a DOT terminal/consolidate node
	// conditionally, a cognition gate never.
	Terminal bool

	// IsResume / ProbeResume mark a `--resume` relaunch. Only the DOT cascade's
	// implementer back-edge resumes today.
	IsResume    bool
	ProbeResume bool

	// HeartbeatViaTap routes the CHB-019 daemon heartbeat through the per-run
	// tap (so an implementer budget watchdog subscribed to the tap sees progress
	// — hk-e7n76) instead of straight to the bus. It must be FALSE for a
	// reviewer node: routing reviewer heartbeats through the tap feeds
	// pasteInjectQuitOnReviewFile's "still reasoning" signal forever and blocks
	// the kill until the 60-minute hard ceiling (hk-sj6a).
	HeartbeatViaTap bool

	// Deliver is the site's brief delivery + completion watchdog arming. It runs
	// on the machine's post-ready deliver edge, and again as the fall-through
	// when the readiness handshake was skipped entirely.
	Deliver func(ctx context.Context, dc agentDeliverCtx)

	// OnBeforeLaunch is site-specific work that must happen AFTER the CHB-018
	// pre-exec messages and immediately BEFORE the spawn — the DOT cascade emits
	// reviewer_launched here so it is the last event before the spawn wait, which
	// is what gives a reviewer node the stale watcher's longer launch floor.
	// nil for sites with none.
	OnBeforeLaunch func(ctx context.Context)

	// OnLaunchedExtra is site-specific post-launch work (single-mode's
	// runregistry.RunHandle machine set and its comms presence join). nil for sites with none.
	OnLaunchedExtra func(ctx context.Context, sess handler.Session)

	// RunScope is the scope holding the RUN's resources — the worktree, the
	// tunnel, the worker slot. The launch nests a CHILD of it and puts its own
	// two resources there, because a graph run takes one hook session and one
	// agent session per node while it holds one worktree for the whole run
	// (RSM-038).
	//
	// nil gives the launch a scope of its own, with no parent. The two DOT sites
	// pass nil. Reaching the run's scope from a graph node means threading it
	// through five functions that already take twenty-odd positional parameters
	// each, and the only thing a parent adds is a give-back for a launch whose
	// caller never closed the child — which neither DOT caller can be, because
	// both register a deferred Cleanup with no return in between.
	RunScope *runlease.Scope

	// RunExit reports the run's exit facts, which is what the launch's release
	// sites ask [runlease.Decide] about. It is a FUNCTION because the facts
	// change while the launch runs — the daemon may start stopping at any point
	// — and because the caller re-points its own source once the post-launch
	// facts exist.
	//
	// nil reports the zero exit, which decides reclaim: give everything back.
	// The two DOT sites pass nil, and that is now a KNOWN GAP rather than the
	// right answer.
	//
	// It used to be the right answer, and the reason is worth keeping because it
	// is what changed: a graph run could not satisfy the survive condition at all,
	// since nothing set the independent-session fact for it. A graph run now takes
	// a tmux session of its own, so the condition IS reachable, and these two
	// launches still read the zero exit and kill the agent session when the daemon
	// stops.
	//
	// What that costs today is the difference between a run whose AGENT keeps
	// working across a restart and one whose RECORD does. The run's own scope
	// reads the real facts, so the record and the worktree survive and the next
	// boot adopts the run, resets the bead and reclaims the directory in order.
	// The agent itself is still killed. Closing this means reaching the run's exit
	// facts from a graph node, which is the same threading problem the RunScope
	// field above describes.
	RunExit func() runlease.Exit
}

type agentLaunchResult struct {
	Session handler.Session
	Watcher *handlercontract.Watcher

	// Harness is the resolved harness, or nil when there is no registry or the
	// registry has no entry for the artifacts' agent type.
	Harness handlercontract.Harness

	// LaunchedAt is the clock reading taken immediately before the dispatch
	// segment runs — the start of the phase-duration measurement.
	LaunchedAt time.Time

	// PiCaptureDir is the directory holding the captured pi stdout, or "" when
	// this was not a pi run or the capture could not be set up. The caller may
	// co-locate a post-mortem stderr capture there.
	PiCaptureDir string

	// CapturedSessionID is the session identifier the HARNESS itself reported on
	// its stdout — a codex thread_id, a pi session id. It is empty for a claude
	// launch (no interceptor is wired) and for a SessionIDCaptured harness that
	// exited before it announced one.
	//
	// It exists because a back-edge resume has to target it and nothing else. The
	// interceptor already captured this value; it went into a channel with no
	// reader, so the cascade resumed with the minted TRACKING uuid instead and
	// codex answered "no rollout found for thread id …"
	// (hk-codex-resume-wrong-threadid-5rmtc).
	CapturedSessionID string

	Dispatch runexec.DispatchState

	// Cleanup stops the CHB-019 heartbeat and then closes the launch's scope,
	// which gives the agent session and the hook session back under the run's
	// one disposition. The caller MUST `defer launch.Cleanup()` immediately after
	// the call — it is never nil, it is idempotent, and it does nothing for a
	// resource that was never taken.
	//
	// The two halves are NOT the same kind of thing, and the difference is
	// load-bearing. Stopping the heartbeat is a STEP: it always runs, whatever
	// the run gives back, because the heartbeat belongs to this process and not
	// to the agent. Closing the scope is the give-back, and that is the half the
	// disposition answers.
	//
	// It is handed back rather than run inside because the heartbeat must keep
	// beating through the CALLER's post-run phase: it is what holds the stale
	// watcher's dead-process reap off a long post-exit step such as a DOT node's
	// auto_status `go build`.
	Cleanup func()

	Fail    agentLaunchFail
	FailErr error

	SocketOutcome *handler.ExportedOutcomeEmittedPayload
	Exit          runloop.ExitInfo
}

func newAgentLaunchLogf(w io.Writer, prefix string) func(format string, args ...any) {
	return func(format string, args ...any) {
		_, _ = fmt.Fprintf(w, "%s: "+format+"\n", append([]any{prefix}, args...)...) //nolint:errcheck // best-effort diagnostic write; a failed log must never fail a launch
	}
}

type sessionSlot struct {
	mu          sync.Mutex
	sess        handler.Session
	killLatched bool
}

func (s *sessionSlot) set(sess handler.Session) {
	s.mu.Lock()
	s.sess = sess
	latched := s.killLatched
	s.killLatched = false
	s.mu.Unlock()
	if latched && sess != nil {
		killAnnounced(sess)
	}
}

func (s *sessionSlot) get() handler.Session {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sess
}

func (s *sessionSlot) killOrLatch() bool {
	s.mu.Lock()
	sess := s.sess
	if sess == nil {
		s.killLatched = true
		s.mu.Unlock()
		return false
	}
	s.mu.Unlock()
	killAnnounced(sess)
	return true
}

func killAnnounced(sess handler.Session) {
	ctx, cancel := context.WithTimeout(context.Background(), runlaunch.KillReapTimeout)
	defer cancel()
	_ = sess.Kill(ctx) //nolint:errcheck // best-effort backstop kill (pre-RT8 idiom)
}

// runAgentLaunch spawns the agent described by in.Spec, proves it is alive,
// drives it to a dead session and returns the exit facts.
//
// It never decides what a failure MEANS — no bead is reopened, no node outcome
// is synthesized, no verdict is read here. It reports which boundary was hit and
// leaves the interpretation to the caller.
//
// It is long on purpose. The whole point of this function is that the launch
// sequence exists ONCE; splitting it back into pieces to satisfy a complexity
// threshold would re-create exactly the seams the three copies drifted through.
//
//nolint:funlen,gocognit,cyclop // length is the design — see the paragraph above
func runAgentLaunch(ctx context.Context, in agentLaunchInput) agentLaunchResult {
	env, ports, handles := in.Env, in.Ports, in.Handles
	emit := ports.Emitter
	runID := in.RunID
	spec := in.Spec
	artifacts := in.Artifacts
	agentType := shared.ArtifactAgentType(artifacts)

	logf := newAgentLaunchLogf(os.Stderr, in.LogPrefix)

	res := agentLaunchResult{Cleanup: func() {}}

	if handles.HarnessRegistry != nil {
		if h, hErr := handles.HarnessRegistry.ForAgent(agentType); hErr == nil {
			res.Harness = h
		}
	}
	sessionIDCaptured := res.Harness != nil &&
		res.Harness.SessionIDPolicy() == handlercontract.SessionIDCaptured
	completionMode := handlercontract.CompletionEventStreamThenQuit
	if res.Harness != nil {
		completionMode = res.Harness.Completion()
	}
	processExit := completionMode == handlercontract.CompletionProcessExit

	prs := newPerRunSubstrate(in.BaseSubstrate, env.HandlerBinary, in.Runner)
	runSubstrate := in.BaseSubstrate
	pasteTarget := in.BaseSubstrate
	if prs != nil {
		if in.Runner != nil && in.WorkerSessionName != "" {
			prs.workerSessionName = in.WorkerSessionName
			prs.workerSessionCwd = in.WorkerSessionCwd
		} else if in.Runner == nil && env.RunSessionID != "" {
			prs.runSessionID = env.RunSessionID
		}
		if in.ConfigurePerRunSubstrate != nil {
			in.ConfigurePerRunSubstrate(prs)
		}
		runSubstrate = prs
		pasteTarget = prs
	}

	if handles.RunRegistry != nil {
		if rh, ok := handles.RunRegistry.Get(runID); ok && rh != nil {
			rh.SetAgentType(agentType)
		}
	}

	sandboxSpawn := sandboxSpawnForRun(env.SandboxCfg, resolveGateAgentType(res.Harness, agentType), SandboxProfileInput{
		WorktreePath:           in.WorktreePath,
		GitDir:                 filepath.Join(env.ProjectDir, ".git"),
		RunID:                  runID.String(),
		DaemonSockPath:         in.DaemonSocket,
		AllowedDomains:         env.SandboxCfg.Network.AllowedDomains,
		AllowLocalBinding:      env.SandboxCfg.Network.AllowLocalBinding,
		WeakerNetworkIsolation: env.SandboxCfg.Network.WeakerNetworkIsolation,
		SharedReadCacheDirs:    env.SandboxCfg.Cache.WarmRead,
		PrivateWriteCacheDirs:  env.SandboxCfg.Cache.PrivateWrite,
	})
	if sandboxSpawn != nil {
		if prs != nil {
			prs.sandboxSpawn = sandboxSpawn
		}
		canaryPath := srtEngagementCanaryPath(env.ProjectDir, runID.String())
		if engageErr := verifySandboxEngaged(ctx, sandboxSpawn, canaryPath, logf); engageErr != nil {
			res.Fail = agentLaunchPrelaunchFailed
			res.FailErr = fmt.Errorf("srt sandbox engagement verification failed: %w", engageErr)
			logf("%v", res.FailErr)
			return res
		}
	}

	var piStdoutFile *os.File
	var piStdoutLog *pi.StdoutLogWriter
	if agentType == core.AgentTypePi {
		if h, ok := handles.RunRegistry.Get(runID); ok {
			h.SetCapturedAgentOutput()
		}
		captureDir := filepath.Join(in.WorktreePath, ".harmonik", "pi-agent")
		if mkErr := os.MkdirAll(captureDir, 0o700); mkErr != nil { //dirmode:allow tighter on purpose: pi agent credential dir, matches internal/harness/pi.BuildLaunchSpec
			logf("hk-j6wm7: create pi capture dir %q: %v (stdout capture disabled)", captureDir, mkErr)
		} else if f, ferr := os.Create(filepath.Join(captureDir, "pi-stdout.log")); ferr != nil { //nolint:gosec // G304: path is the run's own worktree capture dir, not caller-supplied
			res.PiCaptureDir = captureDir
			logf("hk-j6wm7: create pi-stdout.log: %v (stdout capture disabled)", ferr)
		} else {
			res.PiCaptureDir = captureDir
			piStdoutFile = f
			piStdoutLog = pi.NewStdoutLogWriter(f)
			defer func() {
				if flushErr := piStdoutLog.Close(); flushErr != nil {
					logf("hk-k4jrh: flush pi-stdout.log: %v", flushErr)
				}
				if closeErr := piStdoutFile.Close(); closeErr != nil {
					logf("hk-j6wm7: close pi-stdout.log: %v", closeErr)
				}
			}()
		}
	}

	var sess sessionSlot
	var emitCapturedSpawnProof func()
	var agentAnnouncedEnd, killedForFailure atomic.Bool
	var capturedSessionIDCh chan string
	if sessionIDCaptured {
		spec.Substrate = nil
		pasteTarget = nil

		wrapBin, wrapArgs, wrapEnv, wrapErr := sandboxWrapExecArgv(sandboxSpawn, spec.Binary, spec.Args)
		if wrapErr != nil {
			res.Fail = agentLaunchPrelaunchFailed
			res.FailErr = fmt.Errorf("srt argv-wrap error: %w", wrapErr)
			logf("%v", res.FailErr)
			return res
		}
		spec.Binary = wrapBin
		spec.Args = wrapArgs
		spec.Env = append(spec.Env, wrapEnv...)

		if sandboxSpawn != nil {
			spec.Env = append(spec.Env,
				"GOCACHE="+filepath.Join(in.WorktreePath, ".harmonik", "go-cache"),
				"GOPATH="+filepath.Join(in.WorktreePath, ".harmonik", "go-path"),
			)
		}

		capturedH := res.Harness
		capturedSessionIDCh = make(chan string, 1)
		agentEndCb := func() { //nolint:contextcheck // PI-014 backstop kill fires from the stdout interceptor goroutine, which outlives any request ctx
			agentAnnouncedEnd.Store(true)
			if !sess.killOrLatch() {
				logf("agent_end arrived before the session was handed back; the kill is held until it is")
			}
		}
		spec.StdoutWrapper = func(r io.Reader) io.Reader {
			src := r
			if piStdoutLog != nil {
				src = io.TeeReader(r, piStdoutLog)
			}
			return capturedH.NewSessionIDInterceptor(src, func(id string) {
				if emitCapturedSpawnProof != nil {
					emitCapturedSpawnProof()
				}
				capturedSessionIDCh <- id
			}, agentEndCb)
		}
	} else {
		spec.Substrate = runSubstrate
	}
	spec.Terminal = in.Terminal

	launchScope := in.RunScope
	if launchScope == nil {
		launchScope = &runlease.Scope{}
	}
	launchScope = launchScope.Nest()

	disposition := func() runlease.Disposition {
		if in.RunExit == nil {
			return runlease.Decide(runlease.Exit{})
		}
		return runlease.Decide(in.RunExit())
	}

	stopHeartbeat := func() {}

	res.Cleanup = func() {
		stopHeartbeat()
		launchScope.Close(disposition())
	}

	var closeHookSession func() error
	if handles.HookStore != nil {
		handles.HookStore.RegisterHookSession(runID.String(), artifacts.ClaudeSessionID)
		closeHookSession = func() error {
			handles.HookStore.CloseHookSession(runID.String(), artifacts.ClaudeSessionID)
			return nil
		}
	}
	hookSession := launchScope.Hold(runlease.HookSession, closeHookSession)
	giveBackHookSession := func() { hookSession.Give(disposition()) }

	refuseLaunch := func(reason string) {
		giveBackHookSession()
		logf("%s (refusing launch)", reason)
		res.Fail = agentLaunchPrelaunchFailed
		res.FailErr = errors.New(reason)
	}

	tap, tapCh := runloop.NewPerRunEventTap(emit, runID)
	runH := handler.NewHandler(tap, handlercontract.NoopWatcherDeadLetter{}, handles.AdapterRegistry)
	emitCapturedSpawnProof = newCapturedSpawnProof(ctx, tap, runID)

	if refusal, refused := d2RemoteAPIKeyRefusal(in.Remote, spec.Env); refused {
		reason := string(refusal)
		refuseLaunch(reason)
		return res
	}

	launchInitiatedMsg := runlaunch.EmitPreExecBeforeLaunch(ctx, emit, runID, artifacts.PreExecMsgs)

	if in.OnBeforeLaunch != nil {
		in.OnBeforeLaunch(ctx)
	}

	adapter, adapterErr := handles.AdapterRegistry.ForAgent(agentType)
	if adapterErr != nil {
		logf("ForAgent(%s): %v (skipping ready-wait)", agentType, adapterErr)
		adapter = nil
	}

	effectiveReadyTimeout := runlaunch.EffectiveAgentReadyTimeout(
		env.AgentReadyTimeout, env.RemoteAgentReadyTimeout, in.Remote)

	var watcher *handlercontract.Watcher
	var launchErr error

	deliver := func(dctx context.Context) {
		if in.Deliver == nil {
			return
		}
		in.Deliver(dctx, agentDeliverCtx{
			Session:     sess.get(),
			PasteTarget: pasteTarget,
			Tap:         tap,
			ProcessExit: processExit,
		})
	}

	coldStart := runlease.Hold(runlease.ColdStartToken, nil)
	if in.Remote && handles.AgentSpawnSem != nil {
		select {
		case handles.AgentSpawnSem <- struct{}{}:
		case <-ctx.Done():
			refuseLaunch(fmt.Sprintf("cancelled awaiting cold-start token: %v", ctx.Err()))
			return res
		}
		coldStart = launchScope.Hold(runlease.ColdStartToken, func() error {
			<-handles.AgentSpawnSem
			return nil
		})
	}

	res.LaunchedAt = ports.Clock.Now()

	var stallCh <-chan runexec.Event
	releaseStalls := func() {}
	if handles.StallFeed != nil {
		stallCh, releaseStalls = handles.StallFeed.Register(runID.String())
	}
	defer releaseStalls()

	seg := &runloop.DispatchSegment{
		Clock: ports.Clock,
		RunID: runID,
		Config: runexec.DispatchConfig{
			SkipReadyHandshake: processExit,
			IsResume:           in.IsResume,
			MaxInputAttempts:   1,
			ReadyTimeout:       effectiveReadyTimeout,
			InputAck:           runloop.DispatchSegmentInputAckWindow,
			ReadyKillReap:      runlaunch.KillReapTimeout,
		},
		// nil adapter → the segment feeds a synthetic ready so the brief is still
		// delivered without a wait.
		Adapter:     adapter,
		ProbeResume: in.ProbeResume,
		Tap:         tap,
		TapCh:       tapCh,
		Launch: func(lctx context.Context) (<-chan struct{}, error) {
			var launched handler.Session
			launched, watcher, launchErr = runH.Launch(lctx, spec)
			sess.set(launched) //nolint:contextcheck // the held kill is the announcement's own, off Background for the reason above
			if launchErr != nil {
				return nil, launchErr
			}
			if watcher != nil {
				return watcher.Done(), nil
			}
			return nil, nil
		},
		OnLaunchFailed: func(lctx context.Context, lErr error) {
			logf("Launch: %v", lErr)
			giveBackHookSession()
			if errors.Is(lErr, ErrSpawnCapTimeout) {
				inUse, capSize := substrateSpawnStats(in.BaseSubstrate)
				runlaunch.EmitSpawnCapBlocked(lctx, emit, runID, ports.Clock.Since(res.LaunchedAt), inUse, capSize)
			}
			if errors.Is(lErr, ErrTmuxNewWindowTimeout) {
				runlaunch.EmitTmuxNewWindowTimeout(lctx, emit, runID, ports.Clock.Since(res.LaunchedAt))
			}
		},
		OnLaunched: func(lctx context.Context) {
			if launchInitiatedMsg != nil {
				runlaunch.EmitPreExecMessage(lctx, emit, runID, launchInitiatedMsg)
			}

			if in.OnLaunchedExtra != nil {
				in.OnLaunchedExtra(lctx, sess.get())
			}

			if handles.HookStore != nil {
				capturedRunID := runID
				capturedSessID := artifacts.ClaudeSessionID
				capturedTap := tap
				handles.HookStore.SetAgentReadyCallback(runID.String(), capturedSessID, func() { //nolint:contextcheck // relay callback runs off any request ctx (pre-RT8 idiom)
					pl := core.AgentReadyPayload{
						RunID:           capturedRunID,
						SessionID:       core.SessionID(capturedSessID),
						Capabilities:    []string{},
						ClaudeSessionID: capturedSessID,
						Provenance:      "claude_session_start",
					}
					b, marshalErr := json.Marshal(pl)
					if marshalErr != nil {
						_ = capturedTap.EmitWithRunID(context.Background(), capturedRunID, core.EventTypeAgentReady, nil) //nolint:errcheck // best-effort emit (pre-RT8 idiom)
						return
					}
					_ = capturedTap.EmitWithRunID(context.Background(), capturedRunID, core.EventTypeAgentReady, b) //nolint:errcheck // best-effort emit (pre-RT8 idiom)
				})
			}

			hbTarget := emit
			if in.HeartbeatViaTap {
				hbTarget = tap
			}
			hbDone := make(chan struct{})
			stopHeartbeat = sync.OnceFunc(func() { close(hbDone) })
			go handler.RunHeartbeatLoop(ctx, artifacts.HandlerSessionID,
				handler.HeartbeatInterval, hbDone,
				newDaemonHeartbeatEmitter(hbTarget, runID))
		},
		Deliver: deliver,
		KillReady: func(kctx context.Context) {
			logf("waitAgentReady: %v", runlaunch.ErrAgentReadyTimeout)
			killedForFailure.Store(true)
			readySess := sess.get()
			_ = readySess.Kill(kctx) //nolint:errcheck // kill is best-effort; the reap below bounds it (pre-RT8 idiom)
			if watcher != nil {
				select {
				case <-watcher.Done():
				case <-substrate.After(ports.Clock, runlaunch.KillReapTimeout): //nolint:contextcheck // ClockPort reap deadline, deliberately not ctx-scoped (pre-RT8 idiom)
					logf("watcher.Done() reap timed out after Kill — continuing")
				}
			}
			waitCtx, waitCancel := context.WithTimeout(context.Background(), runlaunch.KillReapTimeout)
			_ = readySess.Wait(waitCtx) //nolint:errcheck,contextcheck // bounded reap off the (possibly cancelled) run ctx; error non-actionable (pre-RT8 idiom)
			waitCancel()
			giveBackHookSession()
		},
		EmitReadyTimeout: func(context.Context) {
			runlaunch.EmitAgentReadyTimeout(context.Background(), emit, runID, artifacts.ClaudeSessionID, effectiveReadyTimeout) //nolint:contextcheck // Background is deliberate: the emission must survive a reaper-cancelled run ctx
		},
		KillAbort: func(context.Context) {
			if !disposition().Releases(runlease.AgentSession) {
				return
			}
			if abortSess := sess.get(); abortSess != nil {
				killedForFailure.Store(true)
				_ = abortSess.Kill(context.Background()) //nolint:errcheck,contextcheck // idempotent abort kill off the cancelled ctx; teardown follows
			}
		},
		Stalls: stallCh,
		KillStalled: func(_ context.Context, reason string) {
			logf("stall watchdog: %s — killing the agent", reason)
			stalledSess := sess.get()
			if stalledSess == nil {
				return
			}
			if !disposition().Releases(runlease.AgentSession) {
				logf("stall watchdog: the run keeps its session across this shutdown; not killing")
				return
			}
			killedForFailure.Store(true)
			killCtx, killCancel := context.WithTimeout(context.Background(), runlaunch.KillReapTimeout)
			_ = stalledSess.Kill(killCtx) //nolint:errcheck,contextcheck // bounded kill off the (possibly cancelled) run ctx; the error is not actionable
			killCancel()
			if watcher != nil {
				select {
				case <-watcher.Done():
				case <-substrate.After(ports.Clock, runlaunch.KillReapTimeout): //nolint:contextcheck // ClockPort reap deadline, deliberately not ctx-scoped
					logf("stall watchdog: watcher.Done() reap timed out after Kill — continuing")
				}
			}
		},
		SpawnCapTimeout:      ErrSpawnCapTimeout,
		TmuxNewWindowTimeout: ErrTmuxNewWindowTimeout,
	}

	res.Dispatch = seg.Run(ctx)
	defer seg.StopWorkingWatch()
	res.Session = sess.get()
	res.Watcher = watcher

	if launchErr != nil {
		res.Fail = agentLaunchErrored
		res.FailErr = launchErr
		return res
	}

	launchScope.Hold(runlease.AgentSession, func() error { //nolint:contextcheck // the give-back takes no ctx (pre-RT8 idiom); ForceTeardownSession reaps on context.Background() so the kill completes even after the run ctx is cancelled
		runlaunch.ForceTeardownSession(sess.get())
		return nil
	})

	if res.Dispatch.Phase == runexec.DispatchFailed && res.Dispatch.Reason == "agent_ready_timeout" {
		res.Fail = agentLaunchReadyTimeout
		return res
	}

	if res.Dispatch.Phase == runexec.DispatchWorking && seg.Config.SkipReadyHandshake {
		deliver(ctx)
	}

	// hk-5z1f0: agent_ready has resolved, or the handshake was skipped — the
	// cold-start window is over, so the token goes back now and not at the end of
	// the run. Held for the run body, the capacity would bound concurrent RUNS
	// rather than concurrent cold starts, and three long runs would park every
	// later remote dispatch for hours.
	//
	// Release and not Give: no disposition keeps this token. It is this process's
	// own bookkeeping, and a surviving agent does not hold it.
	//
	// The readiness-timeout return ABOVE skips this line on purpose. That path
	// has no cold-start window left to close and the scope's close is the only
	// thing that returns its token.
	//nolint:errcheck // the give-back is a receive on a channel this launch filled; it cannot fail
	_ = coldStart.Release()

	res.SocketOutcome, res.Exit = runloop.WaitWithSocketGrace(ctx, ports.Clock, handles.HookStore, watcher, sess.get(),
		runID.String(), artifacts.ClaudeSessionID)

	res.Exit.AgentAnnouncedEnd = announcementIsCleanExit(agentAnnouncedEnd.Load(), killedForFailure.Load())

	select {
	case id := <-capturedSessionIDCh:
		res.CapturedSessionID = id
	default:
	}

	if watcher == nil && disposition().Releases(runlease.AgentSession) {
		_ = sess.get().Kill(context.Background()) //nolint:errcheck,contextcheck // best-effort window kill on a deliberately non-cancellable ctx: the run ctx may already be cancelled and the pane must still die (pre-RT8 idiom)
	}

	giveBackHookSession()
	return res
}

type agentPostExitInput struct {
	Env   runloop.RunEnv
	Ports runloop.RunPorts

	RunID  core.RunID
	BeadID core.BeadID

	// LogPrefix identifies this run in stderr diagnostics. Give it the same
	// prefix the matching runAgentLaunch call was given.
	LogPrefix string

	// Launch is what runAgentLaunch handed back. The caller has already decided
	// what Launch.Fail means; this reads the exit facts, the session's lifecycle
	// machine and the resolved harness.
	Launch agentLaunchResult

	// Runner and WorktreePath address the run's worktree. Runner is the per-run
	// CommandRunner — an SSHRunner for a remote run, nil for a local one — so a
	// remote run reads HEAD on the worker (NFR7).
	Runner       tmuxpkg.CommandRunner
	WorktreePath string

	// BaselineSHA is the SHA that "did HEAD advance" is measured against, and it
	// is also the parent the commit fallback amends against. Single mode passes
	// the repo HEAD its worktree was cut from. A graph node passes the HEAD
	// probed immediately before THAT node launched, because node N's baseline is
	// node N−1's tip.
	BaselineSHA string

	// AgentType is the run's resolved agent type and it picks the commit
	// fallback's harness wrapper. Pass the artifacts' agent type — the same value
	// runAgentLaunch resolved Launch.Harness from.
	AgentType core.AgentType

	// Implementer marks an implementer-class agent. A reviewer node gets the
	// lifecycle transition and the cancellation check and nothing else: it
	// produces reviewer_verdict rather than implementer_phase_complete, and it
	// has no commit to fall back on.
	Implementer bool

	// CancelReason answers one question: the run context is cancelled — must
	// this run stop, and with what reason? It is consulted ONLY when the context
	// is already cancelled, and an EMPTY answer means keep going.
	//
	// The two modes answer differently on purpose. A graph node always stops. In
	// single mode only a per-run abort stops here; daemon-wide shutdown cancels
	// the same context and must fall through, because the terminal switch below
	// the call drains committed-but-unmerged work and leaves a surviving run's
	// bead alone. nil never stops.
	CancelReason func() string
}

type agentPostExitResult struct {
	// CancelReason is non-empty when the run context was cancelled AND the site
	// said to stop. The caller turns it into its own terminal — a reopened bead
	// in single mode, a node error in the graph — and MUST NOT continue.
	//
	// implementer_phase_complete has already been emitted by the time this can
	// be set, which is the whole point of checking cancellation here rather than
	// before the emit (hk-aekon).
	CancelReason string
}

func runAgentPostExit(ctx context.Context, in agentPostExitInput) agentPostExitResult {
	ports := in.Ports
	emit := ports.Emitter
	launch := in.Launch
	logf := newAgentLaunchLogf(os.Stderr, in.LogPrefix)

	transitionToTerminated(context.Background(), launch.Session.Machine(), in.RunID, emit, //nolint:contextcheck // RSM-022: the lifecycle_transition emission must survive a reaper-cancelled run ctx; Background swap by design
		launch.Exit)

	phaseDur := ports.Clock.Since(launch.LaunchedAt)
	if in.Implementer {
		curHead, _ := gitprobe.ResolveWorktreeHEADVia(ctx, in.Runner, in.WorktreePath) //nolint:errcheck // a probe error reads as "not landed" — the conservative answer for a diagnostic, and the caller's guard runs its own probe
		commitLanded := curHead != "" && curHead != in.BaselineSHA
		runlaunch.EmitImplementerPhaseComplete(ctx, emit, in.RunID, launch.Exit.ExitCode,
			launch.Exit.StderrTail, commitLanded, phaseDur)
	}

	if ctx.Err() != nil && in.CancelReason != nil {
		if reason := in.CancelReason(); reason != "" {
			return agentPostExitResult{CancelReason: reason}
		}
	}
	if !in.Implementer {
		return agentPostExitResult{}
	}

	if launch.Harness == nil || launch.Harness.Completion() != handlercontract.CompletionProcessExit {
		return agentPostExitResult{}
	}

	var outcome shared.RefsOutcome
	var ensureErr error
	label := "ensureCodexRefsTrailer"
	if in.AgentType == core.AgentTypePi {
		label = "ensurePiRefsTrailer"
		outcome, ensureErr = pi.EnsureRefsTrailer(ctx, in.Runner, in.WorktreePath, in.BaselineSHA, in.BeadID)
	} else {
		outcome, ensureErr = codex.EnsureRefsTrailer(ctx, in.Runner, in.WorktreePath, in.BaselineSHA, in.BeadID)
	}
	if ensureErr != nil {
		logf("%s: %v (falling through to the no-commit guard)", label, ensureErr)
		return agentPostExitResult{}
	}
	logf("%s: %s", label, outcome)

	if codex.NoWorkSuspected(outcome, phaseDur, 0) {
		floor := codex.NoWorkFloor(0)
		logf("implementer produced NO commit and a clean worktree after only %v (floor %v) — suspected no-work run (hk-368i4)",
			phaseDur, floor)
		codex.EmitImplementerNoWorkSuspected(ctx, emit, in.RunID, in.BeadID, phaseDur, floor)
	}
	return agentPostExitResult{}
}

func announcementIsCleanExit(announcedEnd, killedForFailure bool) bool {
	return announcedEnd && !killedForFailure
}
