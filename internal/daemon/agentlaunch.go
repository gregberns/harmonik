package daemon

// agentlaunch.go — the ONE agent-launch path.
//
// Before this file there were three hand-written launch paths in this package —
// single-mode (workloop.go beadRunOne), the DOT cascade's agentic node
// (dot_cascade_core.go dispatchDotAgenticNode), and the cognition gate
// (dot_gate.go executeCognitionGate). Each built its own runloop.DispatchSegment
// and hand-rolled the same surrounding steps around it: per-run substrate wrap,
// the sandbox gate, stdout capture, the session-id interceptor, the spawn proof,
// hook-session register/close, the per-run event tap, the pre-exec emissions, the
// adapter lookup, the segment skeleton, the ready-handshake-skip deliver
// fallback, the teardown pair, the completion wait, and the window kill.
//
// Because they were separate, guards drifted: a step got added to one and
// silently not the others, and the compiler said nothing. Seventeen such
// divergences were catalogued. runAgentLaunch collapses the three into one
// function so a large subset of them cannot recur — one path cannot drift from
// itself.
//
// # The seam
//
// runAgentLaunch owns: "given a built launch spec + artifacts, spawn the agent,
// prove it is alive, drive it to a dead session, hand back the exit facts."
//
// Its CALLER owns "what to launch" (model/profile resolution, worktree creation,
// brief pre-writes, shared.LaunchCtx assembly and spec-builder pinning) and
// "what the exit means" (terminal classification, verdict reads, the merge/close
// spine).
//
// # Why this lives in internal/daemon and not internal/runloop
//
// It needs daemon-internal helpers — sandboxSpawnForRun, verifySandboxEngaged,
// newPerRunSubstrate, newCapturedSpawnProof, substrateSpawnStats,
// d2RemoteAPIKeyRefusal — and runloop → daemon is a hard-denied depguard edge.
// The collapse is not worth re-opening that edge for.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/handler"
	"github.com/gregberns/harmonik/internal/handlercontract"
	"github.com/gregberns/harmonik/internal/harness/shared"
	tmuxpkg "github.com/gregberns/harmonik/internal/lifecycle/tmux"
	"github.com/gregberns/harmonik/internal/runexec"
	"github.com/gregberns/harmonik/internal/runlaunch"
	"github.com/gregberns/harmonik/internal/runlease"
	"github.com/gregberns/harmonik/internal/runloop"
	"github.com/gregberns/harmonik/internal/substrate"
)

// The srt sandbox gate used to be scoped per call site: the cognition gate never
// sandboxed, the graph-node path sandboxed only harnesses that capture their own
// session id, and single mode sandboxed everything. That divergence was carried
// as a parameter through the launch-path collapse and RESOLVED on 2026-07-29 by
// consolidating on the widest scope — every launch asks the gate, and the gate
// alone decides.
//
// There is exactly one gate now, and it is `sandboxSpawnForRun`: it wraps a run
// only when the backend is "srt", the harness is listed in sandbox.harnesses,
// and the run is local. The per-site scope was a SECOND gate stacked on that
// one, and a second gate can only ever subtract — which is what made it a silent
// non-enforcement risk. No run loses sandboxing, because the gate is a pure
// function of config and harness identity and no call site changed what it
// passes. What changes is that config is now the only switch, so listing a
// harness in sandbox.harnesses means it is sandboxed on every path rather than
// on some.
//
// Read that as a live consequence, not only a repair. Adding a harness to
// sandbox.harnesses is now one config line that sandboxes it everywhere at once,
// including the cognition gate, and arms verifySandboxEngaged in front of every
// one of those launches. That check is fail-closed, so a harness the sandbox
// cannot engage for stops launching rather than launching unprotected. Intended,
// and worth knowing before editing that list.
//
// Measured before the change, against the live config (backend "srt", harnesses
// ["pi"]): pi captures its session id, so the graph path already sandboxed it.
// Claude mints its own, and is not listed, so neither the graph path nor the
// gate could sandbox it. The consolidation is therefore inert for two of the
// three sites today. The one live change is that the exec-path build-cache
// redirect below now applies to graph runs too.

// agentLaunchFail discriminates HOW a launch stopped short of a completed
// session. The caller owns what each one MEANS (reopen, node failure, gate
// error) — this only says which boundary was hit.
type agentLaunchFail int

const (
	// agentLaunchOK means the agent launched, was driven to a dead session, and
	// the exit facts in the result are populated.
	agentLaunchOK agentLaunchFail = iota

	// agentLaunchPrelaunchFailed means a pre-launch guard refused: srt sandbox
	// engagement verification, the srt argv wrap, or the D2 remote-credential
	// refusal. FailErr carries the full, caller-printable reason.
	agentLaunchPrelaunchFailed

	// agentLaunchErrored means handler.Launch itself failed. FailErr is the raw
	// launch error so the caller can wrap it in its own phrasing.
	agentLaunchErrored

	// agentLaunchReadyTimeout means the dispatch machine reached its
	// agent_ready_timeout terminal. The diagnostic event has already been
	// emitted and the session killed and reaped.
	agentLaunchReadyTimeout
)

// agentDeliverCtx is what the site's brief-delivery hook needs from inside the
// launch. The three sites deliver genuinely different briefs and arm genuinely
// different completion watchdogs, so delivery stays site-owned — but the
// substrate, the session and the tap it needs are all created in here.
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

// agentLaunchInput is everything runAgentLaunch needs. The hook fields at the
// bottom are the deliberate per-site variation; everything above them is the
// part that used to be copied three times.
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
	// longer agent-ready window and arms the D2 credential refusal.
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
	// RunHandle machine set and its comms presence join). nil for sites with none.
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
	// The two DOT sites pass nil, and that is their correct answer rather than a
	// stub. A graph run cannot satisfy the survive condition — the
	// independent-session fact is set only inside single mode's per-run
	// substrate wiring, and only the work loop passes that — so reclaim is what
	// those launches did before this field existed and what they do now.
	RunExit func() runlease.Exit

	// AfterReadyResolved runs once the readiness phase has settled and before
	// the completion wait — single-mode releases its cold-start spawn semaphore
	// slot here so the gate stays scoped to cold-start. nil for sites with none.
	AfterReadyResolved func()
}

// agentLaunchResult is the exit facts. Fail says which boundary was hit;
// everything from SocketOutcome down is populated only when Fail is
// agentLaunchOK.
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

// newAgentLaunchLogf returns the per-launch stderr diagnostic logger, writing
// `<prefix>: <formatted message>` lines to w.
//
// The prefix is passed as an ARGUMENT and never spliced into the format string.
// It is caller-supplied and routinely carries a bead id or an operator-authored
// DOT node id; a single `%` in either would otherwise be read as a verb and
// garble EVERY diagnostic line for that run — including the ones inside
// verifySandboxEngaged, which is where the operator is least able to afford
// unreadable output.
func newAgentLaunchLogf(w io.Writer, prefix string) func(format string, args ...any) {
	return func(format string, args ...any) {
		_, _ = fmt.Fprintf(w, "%s: "+format+"\n", append([]any{prefix}, args...)...) //nolint:errcheck // best-effort diagnostic write; a failed log must never fail a launch
	}
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

	// Cleanup is non-nil from the first return onward so a caller can defer it
	// unconditionally. It is replaced with the launch scope's close as soon as
	// that scope exists, which is before the launch takes anything.
	res := agentLaunchResult{Cleanup: func() {}}

	// ── Resolve the harness once ────────────────────────────────────────────
	// Every downstream policy question — does it capture its own session id, does
	// it self-terminate, is it sandbox-listed — is answered from this one lookup
	// instead of three repeated ones.
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
	// DERIVED, not a parameter: a harness that self-terminates on turn completion
	// never emits agent_ready, so waiting for the handshake is a guaranteed
	// HC-056 timeout (hk-f6g7; specs/harness-contract.md §2 N5).
	processExit := completionMode == handlercontract.CompletionProcessExit

	// ── Per-run substrate ───────────────────────────────────────────────────
	// Each run gets its own pane handle; under MaxConcurrent>1 a shared
	// pane-target lets the second SpawnWindow overwrite the first run's target
	// and both runs stall (hk-012af).
	prs := newPerRunSubstrate(in.BaseSubstrate, env.HandlerBinary, in.Runner)
	runSubstrate := in.BaseSubstrate
	pasteTarget := in.BaseSubstrate
	if prs != nil {
		if in.Runner != nil && in.WorkerSessionName != "" {
			prs.workerSessionName = in.WorkerSessionName
			prs.workerSessionCwd = in.WorkerSessionCwd
		}
		if in.ConfigurePerRunSubstrate != nil {
			in.ConfigurePerRunSubstrate(prs)
		}
		runSubstrate = prs
		pasteTarget = prs
	}

	// ── Sandbox gate ────────────────────────────────────────────────────────
	// Every launch asks. sandboxSpawnForRun is the only thing that decides. See
	// the note above the launch-input type for what this replaced and why.
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
		// hk-5wdon: srt's own exit code is not evidence the sandbox engaged —
		// under fork saturation sandbox_init can silently fail to apply while srt
		// still exits 0 (hk-tch4t). Prove engagement or refuse to launch.
		canaryPath := srtEngagementCanaryPath(env.ProjectDir, runID.String())
		if engageErr := verifySandboxEngaged(ctx, sandboxSpawn, canaryPath, logf); engageErr != nil {
			res.Fail = agentLaunchPrelaunchFailed
			res.FailErr = fmt.Errorf("srt sandbox engagement verification failed: %w", engageErr)
			logf("%v", res.FailErr)
			return res
		}
	}

	// ── Pi stdout capture ───────────────────────────────────────────────────
	// hk-j6wm7: keep a copy of the child's stdout under the run worktree so a
	// fast-fail NDJSON error is readable post-mortem when the worktree is
	// retained. TEE'd, never redirected, so the session-id interceptor below
	// still sees every byte.
	var piStdoutFile *os.File
	if agentType == core.AgentTypePi {
		// Tell the run that a launch of it captures agent output into the run's
		// worktree, so a failed run keeps that worktree instead of deleting the
		// only record of why it failed (RSM-037 retain-evidence).
		//
		// The mark is made HERE because this function is the one place BOTH
		// workflow modes pass through. The run used to set the same fact in its
		// single-mode tail, which sits below the graph branch's return, so a graph
		// run never reported it — while the graph cascade calls this function once
		// per node and writes the capture below just the same. That asymmetry is
		// the defect this replaces.
		//
		// It is keyed on the RESOLVED HARNESS and NOT on whether the capture
		// directory below was created, and that is deliberate. On a remote run the
		// worktree lives on the WORKER, while the os.MkdirAll below runs on THIS
		// box against a path that belongs to another machine. Whether it succeeds
		// here says nothing about the worker worktree that retention keeps, so
		// keying the fact on it would answer a question about one machine with a
		// measurement from another. The fact the run reported before was the
		// resolved harness alone — mode-independent AND location-independent — and
		// it stays that way.
		if h, ok := handles.RunRegistry.Get(runID); ok {
			h.SetCapturedAgentOutput()
		}
		captureDir := filepath.Join(in.WorktreePath, ".harmonik", "pi-agent")
		// 0o700, NOT core.HarmonikDirMode: this is the same directory
		// pi.BuildLaunchSpec creates as PI_CODING_AGENT_DIR, which holds agent
		// credentials and is deliberately 0o700. MkdirAll does not chmod an
		// existing dir, so whichever creator runs first decides the mode —
		// matching the credential owner is the only safe choice.
		// PiCaptureDir is published as soon as the DIRECTORY exists, not once the
		// stdout file does. The caller gates its post-mortem stderr capture on this
		// being non-empty, and a failed os.Create below is exactly the case where
		// that capture matters most — publishing it only on full success would
		// disable the post-mortem precisely when there is a post-mortem to do.
		if mkErr := os.MkdirAll(captureDir, 0o700); mkErr != nil { //dirmode:allow tighter on purpose: pi agent credential dir, matches internal/harness/pi.BuildLaunchSpec
			logf("hk-j6wm7: create pi capture dir %q: %v (stdout capture disabled)", captureDir, mkErr)
		} else if f, ferr := os.Create(filepath.Join(captureDir, "pi-stdout.log")); ferr != nil { //nolint:gosec // G304: path is the run's own worktree capture dir, not caller-supplied
			res.PiCaptureDir = captureDir
			logf("hk-j6wm7: create pi-stdout.log: %v (stdout capture disabled)", ferr)
		} else {
			res.PiCaptureDir = captureDir
			piStdoutFile = f
			defer func() {
				if closeErr := piStdoutFile.Close(); closeErr != nil {
					logf("hk-j6wm7: close pi-stdout.log: %v", closeErr)
				}
			}()
		}
	}

	// ── Launch path selection + session-id interceptor ──────────────────────
	// hk-47u9z / hk-z4nif: the spawn proof is assigned after the tap exists; it
	// is captured by reference here because the interceptor cannot fire until
	// Launch has started the child and its stdout is flowing.
	var sess handler.Session
	var emitCapturedSpawnProof func()
	if sessionIDCaptured {
		// The tmux substrate returns Stdout()==nil, so StdoutWrapper would never
		// be called and the session-id capture would silently no-op. Force the
		// exec path, which wires a real stdout pipe (PI-012a / hk-mzgh).
		spec.Substrate = nil
		// These harnesses receive their task via argv, not via pane paste.
		pasteTarget = nil

		wrapBin, wrapArgs, wrapErr := sandboxWrapExecArgv(sandboxSpawn, spec.Binary, spec.Args)
		if wrapErr != nil {
			res.Fail = agentLaunchPrelaunchFailed
			res.FailErr = fmt.Errorf("srt argv-wrap error: %w", wrapErr)
			logf("%v", res.FailErr)
			return res
		}
		spec.Binary = wrapBin
		spec.Args = wrapArgs

		if sandboxSpawn != nil {
			// hk-cdpxu: the sandbox denies writes to the default Go cache
			// locations under $HOME, so any in-sandbox `go build`/`go test`
			// fails on the cache write even though `go` itself resolves. Point
			// them at the run worktree, which is already in the profile's
			// allowWrite set. Additive: spec.Env wins on duplicate keys.
			//
			// Go is the only toolchain handled here because it is the only one
			// this repo builds. The same problem applies to every language with
			// a writable cache under $HOME — Rust's CARGO_HOME is the next one
			// we expect to need. When a second toolchain arrives, this stops
			// being a Go-specific block and becomes a per-language cache
			// redirect the sandbox config names. See NEXT_STEPS.md.
			//
			// Was previously scoped to single-mode launches only. Consolidated
			// 2026-07-29 — a graph run that builds inside the sandbox hit the
			// same denied write and had no redirect.
			spec.Env = append(spec.Env,
				"GOCACHE="+filepath.Join(in.WorktreePath, ".harmonik", "go-cache"),
				"GOPATH="+filepath.Join(in.WorktreePath, ".harmonik", "go-path"),
			)
		}

		capturedH := res.Harness
		capturedSessionIDCh := make(chan string, 1) // buffered; no site reads it back
		agentEndCb := func() {                      //nolint:contextcheck // PI-014 backstop kill fires from the stdout interceptor goroutine, which outlives any request ctx
			// PI-014: pi's process exit is unreliable, so agent_end is the
			// event-driven kill backstop.
			if sess != nil {
				_ = sess.Kill(context.Background()) //nolint:errcheck // best-effort backstop kill (pre-RT8 idiom)
			}
		}
		spec.StdoutWrapper = func(r io.Reader) io.Reader {
			src := r
			if piStdoutFile != nil {
				src = io.TeeReader(r, piStdoutFile)
			}
			return capturedH.NewSessionIDInterceptor(src, func(id string) {
				// NORMALIZED (was: DOT cascade only). Capturing a session id off
				// the harness's own stdout is PROOF the child spawned and is
				// producing output — the SessionIDCaptured analogue of the Claude
				// SessionStart hook's agent_ready. Without it the stale watcher's
				// never-spawned reaper NEVER disarms for codex/pi, because
				// agent_ready is emitted only by the Claude hook path: the
				// launch-stall guard silently becomes an ABSOLUTE 30-minute
				// wall-clock cap and kills demonstrably healthy runs
				// (commit_landed=true, exit 0) at ~30m, mislabelled as "context
				// cancelled". Single-mode codex/pi runs had this hole; now they
				// cannot, because there is only one interceptor.
				//
				// This answers "did it ever spawn?", not "is it still making
				// progress?" — a child that spawns and then wedges is still
				// caught by nothing here (hk-spqhh, pre-existing).
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

	// ── The launch's resources (RSM-036 … RSM-038) ──────────────────────────
	//
	// A launch takes two things a run takes several of: a hook session and an
	// agent session, one of each per graph node. They therefore live in a scope
	// of their own, nested inside the run's, and the run's outer resources —
	// the worktree, the tunnel — outlive every one of these.
	//
	// The nest happens HERE rather than at the top of the function because the
	// per-run substrate wiring above may take a RUN-level resource (single mode
	// writes the run registry record inside it), and a run resource taken after
	// the child scope was pushed would be given back before the agent session
	// dies.
	launchScope := in.RunScope
	if launchScope == nil {
		launchScope = &runlease.Scope{}
	}
	launchScope = launchScope.Nest()

	// disposition is the ONE answer every release site in this function reads
	// (RSM-037). It is re-read at each site rather than captured once, because
	// the daemon can start stopping at any point during a launch and the answer
	// is only correct at the moment it is asked.
	disposition := func() runlease.Disposition {
		if in.RunExit == nil {
			return runlease.Decide(runlease.Exit{})
		}
		return runlease.Decide(in.RunExit())
	}

	// stopHeartbeat is armed once the heartbeat exists. It is a STEP and not a
	// give-back: it runs whatever the run's disposition is, because a surviving
	// agent does not hold this process's heartbeat goroutine.
	stopHeartbeat := func() {}

	// Cleanup is the caller's handle on this scope. It is installed as soon as
	// the scope exists, so a launch that refuses before it takes anything still
	// hands back a call the caller can defer.
	res.Cleanup = func() {
		stopHeartbeat()
		launchScope.Close(disposition())
	}

	// ── Hook session ────────────────────────────────────────────────────────
	// Registered before Launch so an incoming Stop-hook relay is routed to this
	// run (CHB-025). Given back on every exit below — unless the run survives
	// the daemon, in which case the agent still needs it to report through.
	//
	// A launch with no hook store holds a lease born spent: it names the
	// resource, it can never fire, and no give-back site needs to test whether
	// there is anything to give (RSM-036).
	var closeHookSession func() error
	if handles.HookStore != nil {
		handles.HookStore.RegisterHookSession(runID.String(), artifacts.ClaudeSessionID)
		closeHookSession = func() error {
			handles.HookStore.CloseHookSession(runID.String(), artifacts.ClaudeSessionID)
			return nil
		}
	}
	hookSession := launchScope.Hold(runlease.HookSession, closeHookSession)
	// giveBackHookSession hands the hook session back EARLY, before the scope
	// closes, under the run's one answer. It is [runlease.Lease.Give] and not
	// Release on purpose: Release gives back unconditionally and would tear down
	// the reporting channel of an agent this daemon means to leave running.
	giveBackHookSession := func() { hookSession.Give(disposition()) }

	// refuseLaunch records a pre-launch refusal on the result and gives the hook
	// session back. It is this path's analogue of beadRunOne's failRun: the
	// launch reports WHY it refused, and the caller decides what that means
	// (reopen the bead, fail the node, error the gate). The D2 conformance
	// sensor (conformance_m4c7_test.go) requires the credential guard to report
	// through this call and return immediately, with nothing in between.
	refuseLaunch := func(reason string) {
		giveBackHookSession()
		logf("%s (refusing launch)", reason)
		res.Fail = agentLaunchPrelaunchFailed
		res.FailErr = errors.New(reason)
	}

	tap, tapCh := runloop.NewPerRunEventTap(emit, runID)
	runH := handler.NewHandler(tap, handlercontract.NoopWatcherDeadLetter{}, handles.AdapterRegistry)
	// NORMALIZED (was: armed only on the DOT cascade path). The proof is armed
	// whenever an interceptor was installed above, which is the only condition
	// under which it can fire.
	emitCapturedSpawnProof = newCapturedSpawnProof(ctx, tap, runID)

	// ── D2 fail-closed credential refusal ───────────────────────────────────
	// NORMALIZED (was: single-mode only). Inspect the FINAL spawn environment at
	// the launch boundary, after every spec mutation, and refuse live Anthropic
	// credentials on a remote worker (the 2026-05-30 credential-leak incident).
	// Before the collapse a remote DOT graph node or cognition gate could be
	// launched with live API credentials in its environment; that is now
	// impossible by construction rather than by three sites remembering.
	if refusal, refused := d2RemoteAPIKeyRefusal(in.Remote, spec.Env); refused {
		reason := string(refusal)
		refuseLaunch(reason)
		return res
	}

	// ── Pre-exec emissions ──────────────────────────────────────────────────
	// hk-goczd: emit the CHB-018 pre-exec messages BEFORE Launch, holding back
	// launch_initiated until the window is actually live. The stale watcher's
	// launch_stall_detected keys solely on launch_initiated absence, so skipping
	// this fires a false stall on every dispatch; emitting it early makes it lie
	// when SpawnWindow is wedged on a leaked slot.
	launchInitiatedMsg := runlaunch.EmitPreExecBeforeLaunch(ctx, emit, runID, artifacts.PreExecMsgs)

	if in.OnBeforeLaunch != nil {
		in.OnBeforeLaunch(ctx)
	}

	adapter, adapterErr := handles.AdapterRegistry.ForAgent(agentType)
	if adapterErr != nil {
		// No adapter for the resolved agent type — non-fatal; the segment feeds a
		// synthetic ready so the brief is still delivered without a wait.
		logf("ForAgent(%s): %v (skipping ready-wait)", agentType, adapterErr)
		adapter = nil
	}

	// hk-96d7w: a remote run gets the longer cold-start window. Captured once so
	// the timeout the machine ARMS and the timeout the diagnostic REPORTS are the
	// same number (see EmitReadyTimeout below).
	effectiveReadyTimeout := runlaunch.EffectiveAgentReadyTimeout(
		env.AgentReadyTimeout, env.RemoteAgentReadyTimeout, in.Remote)

	// RT14: predeclared so the segment's hooks can assign them from inside their
	// closures. Safe because RunDispatch drives every effector inline on this
	// goroutine (runshell.go RunDispatch).
	var watcher *handlercontract.Watcher
	var launchErr error

	deliver := func(dctx context.Context) {
		if in.Deliver == nil {
			return
		}
		in.Deliver(dctx, agentDeliverCtx{
			Session:     sess,
			PasteTarget: pasteTarget,
			Tap:         tap,
			ProcessExit: processExit,
		})
	}

	res.LaunchedAt = ports.Clock.Now()

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
			sess, watcher, launchErr = runH.Launch(lctx, spec)
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
			// NORMALIZED (was: absent on the cognition-gate path, which set both
			// classifier errors and emitted neither — so an operator saw no reason
			// at all for a failed gate launch). Both structural launch-timeout
			// failures now surface their dedicated diagnostic everywhere: a
			// saturated spawn pool and a hung `tmux new-window` are different
			// operator problems and must not both read as an opaque launch error.
			if errors.Is(lErr, ErrSpawnCapTimeout) {
				// in.BaseSubstrate, not handles.Substrate: the spawn cap that
				// blocked this launch belongs to the substrate this run actually
				// spawned on. A reviewer-class launch runs on ReviewerSubstrate,
				// so the old spelling reported the implementer pool's saturation
				// for a launch the implementer pool never touched.
				inUse, capSize := substrateSpawnStats(in.BaseSubstrate)
				runlaunch.EmitSpawnCapBlocked(lctx, emit, runID, ports.Clock.Since(res.LaunchedAt), inUse, capSize)
			}
			if errors.Is(lErr, ErrTmuxNewWindowTimeout) {
				runlaunch.EmitTmuxNewWindowTimeout(lctx, emit, runID, ports.Clock.Since(res.LaunchedAt))
			}
		},
		OnLaunched: func(lctx context.Context) {
			// The window has actually spawned — emit the held-back
			// launch_initiated now (hk-goczd / hk-4l7zs).
			if launchInitiatedMsg != nil {
				runlaunch.EmitPreExecMessage(lctx, emit, runID, launchInitiatedMsg)
			}

			if in.OnLaunchedExtra != nil {
				in.OnLaunchedExtra(lctx, sess)
			}

			if handles.HookStore != nil {
				capturedRunID := runID
				capturedSessID := artifacts.ClaudeSessionID
				capturedTap := tap
				handles.HookStore.SetAgentReadyCallback(runID.String(), capturedSessID, func() { //nolint:contextcheck // relay callback runs off any request ctx (pre-RT8 idiom)
					// NORMALIZED (was: the cognition gate emitted with NEITHER a run
					// id NOR a payload). Both halves are load-bearing:
					//
					//   - EmitWithRunID: the stale watcher's observe() SKIPS any
					//     envelope whose RunID is nil, so a bare Emit looks correct
					//     and does nothing — agentReadySeen stays false and the
					//     never-spawned reaper stays armed for the WHOLE run,
					//     cancelling the per-run context at ~30 minutes on any gate
					//     that takes longer than that (hk-wths).
					//   - the payload: a nil payload writes payload:null to
					//     events.jsonl, making it impossible after the fact to tell
					//     which runs received agent_ready and which timed out
					//     (hk-5cox8).
					//
					// context.Background() is intentional: the callback fires from a
					// socket-acceptor goroutine whose lifetime is decoupled from ctx.
					pl := core.AgentReadyPayload{
						RunID:           capturedRunID,
						SessionID:       core.SessionID(capturedSessID),
						Capabilities:    []string{},
						ClaudeSessionID: capturedSessID,
						Provenance:      "claude_session_start",
					}
					b, marshalErr := json.Marshal(pl)
					if marshalErr != nil {
						// Fall back to a payload-less emit rather than dropping the
						// event: the reaper disarm matters more than the payload.
						_ = capturedTap.EmitWithRunID(context.Background(), capturedRunID, core.EventTypeAgentReady, nil) //nolint:errcheck // best-effort emit (pre-RT8 idiom)
						return
					}
					_ = capturedTap.EmitWithRunID(context.Background(), capturedRunID, core.EventTypeAgentReady, b) //nolint:errcheck // best-effort emit (pre-RT8 idiom)
				})
			}

			// CHB-019 heartbeat (daemon-owned per OQ5). Without it lastEventType
			// stays frozen at launch_initiated for the whole run and the stale
			// watcher fires a false-positive run_stale on every dispatch (hk-nvjk).
			hbTarget := emit
			if in.HeartbeatViaTap {
				hbTarget = tap
			}
			hbDone := make(chan struct{})
			// The once lives with the channel it closes, rather than in the
			// caller's cleanup, so the stop is idempotent wherever it is called
			// from. This is what replaces the cleanup's own sync.Once: the other
			// half of that cleanup is now a scope close, which is idempotent
			// already.
			stopHeartbeat = sync.OnceFunc(func() { close(hbDone) })
			go handler.RunHeartbeatLoop(ctx, artifacts.HandlerSessionID,
				handler.HeartbeatInterval, hbDone,
				newDaemonHeartbeatEmitter(hbTarget, runID))
		},
		Deliver: deliver,
		KillReady: func(kctx context.Context) {
			logf("waitAgentReady: %v", runlaunch.ErrAgentReadyTimeout)
			_ = sess.Kill(kctx) //nolint:errcheck // kill is best-effort; the reap below bounds it (pre-RT8 idiom)
			if watcher != nil {
				select {
				case <-watcher.Done():
				case <-substrate.After(ports.Clock, runlaunch.KillReapTimeout): //nolint:contextcheck // ClockPort reap deadline, deliberately not ctx-scoped (pre-RT8 idiom)
					logf("watcher.Done() reap timed out after Kill — continuing")
				}
			}
			// NORMALIZED (was: unbounded on both DOT paths, whose comment called a
			// bound "a logic change" — it is, and it is the intended one). An agent
			// that ignores the kill must not hold this goroutine up to the
			// never-spawned reaper's 30-minute deadline. context.Background() as
			// parent makes the bound independent of the per-run ctx, which the
			// reaper may already have cancelled (hk-4hso5).
			waitCtx, waitCancel := context.WithTimeout(context.Background(), runlaunch.KillReapTimeout)
			_ = sess.Wait(waitCtx) //nolint:errcheck,contextcheck // bounded reap off the (possibly cancelled) run ctx; error non-actionable (pre-RT8 idiom)
			waitCancel()
			giveBackHookSession()
		},
		EmitReadyTimeout: func(context.Context) {
			// NORMALIZED on both axes.
			//
			//   - context.Background(): the emission must survive a run ctx the
			//     never-spawned reaper has already cancelled, or the one event that
			//     explains the failure is the one that gets dropped (hk-4hso5).
			//   - effectiveReadyTimeout, not env.AgentReadyTimeout: all three sites
			//     passed the LOCAL configured value to a parameter named
			//     effectiveTimeout, so a remote run reported a bound shorter than
			//     the one that actually fired.
			runlaunch.EmitAgentReadyTimeout(context.Background(), emit, runID, artifacts.ClaudeSessionID, effectiveReadyTimeout) //nolint:contextcheck // Background is deliberate: the emission must survive a reaper-cancelled run ctx
		},
		KillAbort: func(context.Context) {
			// One disposition read, the same one the teardown and the post-wait
			// window kill make. A run whose agent has a session of its own and
			// whose daemon is stopping keeps that session: it outlives SIGKILL,
			// the next boot's adoption pass looks for it, and killing it here
			// would strand the bead in progress with nothing alive to adopt.
			if !disposition().Releases(runlease.AgentSession) {
				return
			}
			// Ctx-cancel abort edge: Kill is idempotent and the teardown pair
			// rides behind it either way.
			if sess != nil {
				_ = sess.Kill(context.Background()) //nolint:errcheck,contextcheck // idempotent abort kill off the cancelled ctx; teardown follows
			}
		},
		SpawnCapTimeout:      ErrSpawnCapTimeout,
		TmuxNewWindowTimeout: ErrTmuxNewWindowTimeout,
	}

	res.Dispatch = seg.Run(ctx)
	res.Session = sess
	res.Watcher = watcher

	if launchErr != nil {
		// No session was created, so there is nothing to tear down and no
		// heartbeat to stop. OnLaunchFailed already gave the hook session back.
		res.Fail = agentLaunchErrored
		res.FailErr = launchErr
		return res
	}

	// ── The agent session ───────────────────────────────────────────────────
	// The session exists, so the launch holds it. Its give-back runs at the
	// scope's close, which the caller reaches through Cleanup — the heartbeat
	// must keep beating through the caller's post-run phase, and stopping it at
	// this return would let a DOT node's auto_status `go build` be cancelled
	// past the five-minute dead-process reap and recorded as a node failure.
	//
	// The teardown is the hk-68pvl guard: the caller's worktree cleanup must
	// never remove the directory while an agent is still live inside it. The
	// scope's reverse order is what keeps that true — the session lease sits
	// INSIDE the run scope that holds the worktree, so the session always dies
	// first.
	//
	// NORMALIZED ORDER: stop the heartbeat, THEN tear the session down. The
	// cognition gate registered these as two defers whose LIFO order inverted
	// them — tearing the session down while its heartbeat loop was still running
	// — and carried a comment claiming that inversion as deliberate. It is
	// superseded here.
	launchScope.Hold(runlease.AgentSession, func() error { //nolint:contextcheck // the give-back takes no ctx (pre-RT8 idiom); ForceTeardownSession reaps on context.Background() so the kill completes even after the run ctx is cancelled
		runlaunch.ForceTeardownSession(sess)
		return nil
	})

	if res.Dispatch.Phase == runexec.DispatchFailed && res.Dispatch.Reason == "agent_ready_timeout" {
		res.Fail = agentLaunchReadyTimeout
		return res
	}

	// The readiness handshake was skipped (Launching → Working directly), so the
	// machine never traversed the deliver edge — invoke it here.
	if res.Dispatch.Phase == runexec.DispatchWorking && seg.Config.SkipReadyHandshake {
		deliver(ctx)
	}
	// Working / Exited / Aborted otherwise: fall through to the completion wait.

	if in.AfterReadyResolved != nil {
		in.AfterReadyResolved()
	}

	res.SocketOutcome, res.Exit = runloop.WaitWithSocketGrace(ctx, ports.Clock, handles.HookStore, watcher, sess,
		runID.String(), artifacts.ClaudeSessionID)

	// Substrate path: completion is signalled through the hook store, not a
	// watcher, so nothing has killed the window yet. The same disposition read
	// gates it, for the same reason it gates the abort kill and the session's
	// give-back — a session the run means to outlive this process must not be
	// killed here either. (In practice the substrate's killOnce has already been
	// burned by a no-op kill inside the completion wait on that path, so this
	// guard is belt to that brace. It is stated explicitly rather than relied
	// upon implicitly.)
	if watcher == nil && disposition().Releases(runlease.AgentSession) {
		_ = sess.Kill(context.Background()) //nolint:errcheck,contextcheck // best-effort window kill on a deliberately non-cancellable ctx: the run ctx may already be cancelled and the pane must still die (pre-RT8 idiom)
	}

	giveBackHookSession()
	return res
}
