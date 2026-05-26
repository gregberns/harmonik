package daemon

// workloop.go — main work loop for the harmonik daemon.
//
// RunWorkLoop polls the bead ledger for ready work, claims beads up to
// MaxConcurrent at a time, materialises git worktrees, spawns handler
// subprocesses, and closes (or reopens) beads based on outcome.
//
// # Concurrency model (hk-e61c3.2, POST_MVH_PARALLELISM_ROADMAP row 5)
//
// Goroutine-per-active-bead: the outer poll loop spawns one goroutine per
// claimed bead. The in-flight count is gated by MaxConcurrent via RunRegistry's
// claim semaphore (hk-e61c3.3). Parallelism roadmap rows 1–6 are shipped.
// At MaxConcurrent=1 (the default), the loop is semantically equivalent to the
// prior serial implementation: only one goroutine is ever in-flight, so
// behaviour is byte-identical to the pre-parallelism code.
//
// Anti-pattern (roadmap §6): do NOT use a worker-pool-fed-by-queue. One
// goroutine per active bead — in-flight count MUST equal runRegistry.Len().
//
// # Configurable binary
//
// HandlerBinary on daemon.Config controls which binary is spawned. The
// exploratory testing wave injects a twin binary rather than "claude" so that
// no API credits are consumed during wave runs. If HandlerBinary is empty the
// loop defaults to "claude".
//
// Spec ref: MVH_ROADMAP.md row #10; specs/execution-model.md §4.3 EM-013 (run_id
// as join key); specs/event-model.md §8.1 (run_started / run_completed events).
// Beads: hk-ecrxy (MVH loop), hk-e61c3.2 (parallelism).

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/brcli"
	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/handler"
	"github.com/gregberns/harmonik/internal/handlercontract"
	"github.com/gregberns/harmonik/internal/lifecycle"
	"github.com/gregberns/harmonik/internal/queue"
	"github.com/gregberns/harmonik/internal/workflow"
	"github.com/gregberns/harmonik/internal/workspace"
)

// workloopPollInterval is the sleep duration between bead-ledger polls when the
// ready queue is empty.
const workloopPollInterval = 2 * time.Second

// agentReadyKillReapTimeout is the maximum time to wait for watcher.Done()
// after Kill() in the HC-056 agent_ready_timeout path. Kill() itself sends
// SIGTERM then SIGKILL (3 s grace); this additional 10 s covers watcher
// teardown after SIGKILL lands. If the watcher does not exit within this
// window, reaping is abandoned and the bead is still reopened — the stuck
// watcher goroutine will eventually unblock when ctx is cancelled.
//
// Spec ref: specs/handler-contract.md §4.9 HC-056.
// Bead ref: hk-do7te.
const agentReadyKillReapTimeout = 10 * time.Second

// workLoopDeps bundles the injectable dependencies of the work loop.  All
// fields are required (non-nil).  Use newWorkLoopDeps to construct the
// production set from daemon.Config.
//
// The dependency bundle exists so that workloop_test.go can substitute stub
// implementations without forking the loop logic.
type workLoopDeps struct {
	// brAdapter is the Beads CLI adapter.  Used for Ready, ClaimBead, CloseBead,
	// ReopenBead.
	brAdapter beadLedger

	// bus is the in-process event bus.  The work loop uses only Emit.
	bus handlercontract.EventEmitter

	// h is the handler factory.
	h handler.Handler

	// intentLogDir is the absolute path to the beads-intents/ directory for
	// the BI-030 intent-log protocol.
	intentLogDir string

	// projectDir is the absolute path of the harmonik project root.
	projectDir string

	// handlerBinary is the binary to spawn per iteration.  Empty → "claude".
	handlerBinary string

	// daemonBinaryPath is the absolute path to the running harmonik binary,
	// resolved via os.Executable() at daemon startup (hk-kqdpf.6). Threaded
	// into claudeRunCtx so MaterializeClaudeSettings emits absolute-path hook
	// commands instead of bare "harmonik". When empty, falls back to "harmonik".
	daemonBinaryPath string

	// handlerArgs are extra arguments appended to the handler binary invocation
	// for every bead dispatch (hk-4e5b5).  Nil → no extra args.
	handlerArgs []string

	// handlerEnv is the environment for the handler subprocess ("KEY=VALUE" pairs).
	// Nil → child inherits no environment; the production caller MUST inject at
	// minimum HARMONIK_PROJECT_HASH per lifecycle.ProvenanceEnvVar.
	handlerEnv []string

	// brTimeoutCfg is the timeout configuration for br CLI invocations.
	brTimeoutCfg brcli.TimeoutConfig

	// tidGen is the TransitionID generator.  A single shared generator enforces
	// strict monotonicity across the loop per execution-model.md §4.4 EM-018a.
	tidGen *core.TransitionIDGenerator

	// workflowModeDefault is the daemon-level default workflow mode cached at
	// PL-005 step 0 per §PL-004a.  It is the third-tier fallback in the
	// four-tier resolution chain (execution-model.md §4.3 EM-012a); the claim
	// path (T-WM-009) reads this field when neither a per-bead label nor a
	// per-project override is present.  Always a valid WorkflowMode value; zero
	// value is never stored (Start normalises it to WorkflowModeSingle).
	//
	// Bead ref: hk-7om2q.8.
	workflowModeDefault core.WorkflowMode

	// runRegistry tracks in-flight bead runs (hk-e61c3.2). The outer poll loop
	// gates goroutine creation on runRegistry.Len() < maxConcurrent. Each
	// dispatched goroutine calls Register on claim and Unregister on exit.
	//
	// MUST be a field on workLoopDeps — NOT a package-level variable (see
	// POST_MVH_PARALLELISM_ROADMAP.md §6 anti-pattern).
	//
	// Bead ref: hk-e61c3.2.
	runRegistry *RunRegistry

	// maxConcurrent is the ceiling on simultaneously in-flight bead goroutines.
	// Sourced from daemon.Config.MaxConcurrent (zero → 1 per Config godoc).
	// Row 6 (hk-e61c3.1) adds this field to Config; row 5 (this bead) enforces it.
	//
	// POST_MVH_PARALLELISM_ROADMAP §6: enforcement lives here, NOT in the bus
	// or adapter.
	//
	// Bead ref: hk-e61c3.2.
	maxConcurrent int

	// hookStore is the daemon-wide hook-session registry. It implements
	// HookRelayHandler and is passed to RunSocketListener as the hr argument so
	// that incoming hook-relay envelopes are dispatched to the store (rather than
	// rejected with bad_envelope when hr is nil). The work loop consults the store
	// via WaitForOutcome in the completion path (hk-gql20.22).
	//
	// Constructed once at daemon.Start and shared between the socket listener and
	// the work loop. Concurrent access is serialised by hookSessionStore.mu.
	//
	// Spec ref: specs/claude-hook-bridge.md §4.10 CHB-025.
	// Bead ref: hk-gql20.21.
	hookStore hookStoreIface

	// launchSpecBuilder builds the handler.LaunchSpec and claudeRunArtifacts for
	// a given bead run. Production always uses buildClaudeLaunchSpec. Test fixtures
	// that do not need real bridge setup (e.g. MaterializeClaudeSettings fsyncs)
	// may inject a lightweight stub via ExportedWorkLoopDeps.
	//
	// When nil, buildClaudeLaunchSpec is used (production default).
	//
	// Bead ref: hk-kqdpf.1.
	launchSpecBuilder func(context.Context, claudeRunCtx) (handler.LaunchSpec, claudeRunArtifacts, error)

	// worktreeFactory creates a worktree directory for a bead run and returns its
	// absolute path. Production always uses workspace.CreateWorktree and then
	// derives the path via workspace.WorktreePath. Test fixtures that do not need
	// a real git worktree may inject a lightweight stub that creates a temp
	// directory instead, avoiding git worktree contention under parallel load.
	//
	// The returned cleanup function (if non-nil) is called on defer to remove
	// the worktree after the bead run completes. Production wires removeWorktree.
	//
	// When nil, the production path (workspace.CreateWorktree) is used.
	//
	// Bead ref: hk-kqdpf.1.
	worktreeFactory func(ctx context.Context, projectDir, runID, headSHA string) (wtPath string, cleanup func(), err error)

	// adapterRegistry is the sealed adapter registry used to look up the
	// per-agent-type Adapter (for Adapter.DetectReady) in the single-mode
	// completion path (HC-056 / hk-gql20.14).
	//
	// The work loop calls ForAgent(core.AgentTypeClaudeCode) on each dispatch to
	// obtain the adapter. MUST be non-nil — newWorkLoopDeps rejects nil
	// (hk-d8u1y: nil-guard branches deleted; precondition now enforced).
	//
	// Bead ref: hk-gql20.14; hk-d8u1y.
	adapterRegistry *handlercontract.AdapterRegistry

	// substrate is the optional tmux-substrate for handler.Launch.  At MVH this
	// is always nil; handler falls back to exec.CommandContext.  When non-nil it
	// is attached to the LaunchSpec.Substrate field so the handler spawns the
	// subprocess inside a tmux window.
	//
	// Spec ref: specs/process-lifecycle.md §4.7 PL-021b.
	// Bead ref: hk-gql20.14.
	substrate handler.Substrate

	// agentReadyTimeout is the maximum duration waitAgentReady blocks waiting
	// for an agent_ready event per HC-056.  Zero → defaultAgentReadyTimeout (30s).
	// Sourced from Config.AgentReadyTimeout (also zero-value safe).
	//
	// Spec ref: specs/handler-contract.md §4.9 HC-056.
	// Bead ref: hk-gql20.14.
	agentReadyTimeout time.Duration

	// projectCfg is the decoded .harmonik/config.yaml loaded once at startup
	// (EM-012b tier-2). The zero value is safe: LookupAgent returns ("","") for
	// all agent types. Passed to ResolveModelPreference at claim time.
	//
	// Spec ref: specs/execution-model.md §4.3 EM-012b.
	// Bead ref: hk-bfvk7.
	projectCfg ProjectConfig

	// queueStore is the daemon-singleton holder for the active *queue.Queue.
	// When non-nil and a queue is loaded, the dispatch loop pulls work from the
	// active queue group rather than polling br ready. When nil or when no queue
	// is loaded, the loop falls back to the br-ready poll path (backward-compat
	// for tests that do not use queues).
	//
	// Spec ref: specs/execution-model.md §7.4 (TS-1 dispatch loop); §4.3.EM-015f
	// (group-advance gate).
	// Bead ref: hk-45ude.
	queueStore *QueueStore

	// submitWakeC, when non-nil, is the channel returned by queueStore.WakeCh().
	// The workloop's idle sleeps select on this channel so that a queue-submit
	// RPC immediately wakes the loop rather than waiting for the next poll tick
	// (hk-24xn1). When nil (no queue surface / legacy path) the select case
	// on a nil channel blocks forever and is effectively skipped — workloopSleep
	// falls back to the timer-only path.
	//
	// Wired from QueueStore.WakeCh() by daemon.Start alongside deps.queueStore.
	//
	// Bead ref: hk-24xn1.
	submitWakeC <-chan struct{}

	// cancelOnQueueDrain, when non-nil, is called once after the queue
	// transitions to all-success and ClearQueue completes. Used by the
	// `harmonik run <bead-id>` subcommand (hk-icecw) to exit the daemon
	// cleanly after a single-bead queue drains.
	//
	// The zero value (nil) preserves normal daemon behaviour: the work loop
	// continues running after the queue drains.
	//
	// Bead ref: hk-icecw.
	cancelOnQueueDrain context.CancelFunc

	// cancelOnQueueExit, when non-nil, is called once when the queue reaches
	// any terminal state: all-success (after ClearQueue) OR paused-by-failure
	// (after Persist). This ensures harmonik run <bead-id> exits on failure
	// instead of hanging indefinitely waiting for more work (hk-8jh26 Fix 1).
	//
	// The zero value (nil) preserves normal daemon behaviour.
	//
	// Bead ref: hk-8jh26.
	cancelOnQueueExit context.CancelFunc

	// stopDispatchCtx, when non-nil, is the context checked by the outer poll
	// loop to decide whether to stop pulling new beads. When this context is
	// cancelled the loop exits via exitClean() and waits for in-flight goroutines
	// to drain — but in-flight goroutines continue running on the main ctx
	// passed to runWorkLoop.
	//
	// This separates "stop dispatching" from "cancel in-flight work" so that
	// CancelOnQueueDrain/CancelOnQueueExit do not propagate into reviewer
	// goroutines (hk-2o2i9).
	//
	// When nil, the outer poll loop falls back to the main ctx (backward-compat).
	//
	// Bead ref: hk-2o2i9.
	stopDispatchCtx context.Context

	// handlerPauseController, when non-nil, is consulted before every dispatch
	// to implement the skip-on-paused gate (hk-kac8g).  When nil the gate is
	// disabled: all items are dispatched regardless of handler pause state.
	// Production wires the daemon-singleton HandlerPauseController; tests that
	// do not exercise handler-pause behaviour leave this nil (safe default).
	//
	// The controller also tracks the current paused_epoch per agent type, which
	// the dispatcher uses to enforce the at-most-once dedup contract for
	// queue_item_held_for_handler_pause events (§8.11.3).
	//
	// Spec ref: specs/handler-pause.md §6.
	// Bead ref: hk-kac8g, hk-m0k0a.
	handlerPauseController *HandlerPauseController

	// heldEventDedup tracks (beadID + ":" + epoch) pairs for which a
	// queue_item_held_for_handler_pause event has already been emitted this
	// session, enforcing the at-most-once-per-(bead_id, paused_epoch) contract
	// from event-model.md §8.11.3.
	//
	// Keyed by the string "<beadID>:<pausedEpoch>" (e.g. "hk-abc:2").
	// Only the outer poll loop reads/writes this map — NOT per-bead goroutines.
	// Map stays bounded.  Access is single-threaded.
	//
	// Bead ref: hk-kac8g, hk-m0k0a.
	heldEventDedup map[string]struct{}

	// staleBlockerCloser, when non-nil, is used by the claim-failure path to
	// auto-close stale blockers (beads already subsumed in main) so the blocked
	// bead can be retried on the next workloop iteration. When nil the
	// auto-close behaviour is disabled (backward-compat for test stubs that do
	// not need it). Production wires the *brcli.Adapter (which satisfies
	// lifecycle.BeadCat3cCloser via SweepCloseBead).
	//
	// Bead ref: hk-rnsjs.
	staleBlockerCloser lifecycle.BeadCat3cCloser
}

// beadLedger is the subset of brcli.Adapter used by the work loop.  It is
// extracted as an interface so that workloop_test.go can substitute a stub.
//
// # Bead body access — architectural note (hk-33tcf / T6 finding F-T6-004)
//
// The work loop intentionally does NOT read the bead body (description field)
// from Beads-SQLite before or after claiming.  The bead body is the agent's
// work brief, not the daemon's.  The daemon's responsibility is lifecycle
// management (Ready → claim → dispatch → close/reopen); interpretation of the
// brief is the handler subprocess's responsibility.
//
// Consequence — handler contract: the handler subprocess is responsible for
// calling `br show <beadID> --format json` to obtain the work spec.  For MVH,
// the bead ID is supplied to the handler via the implementer-protocol brief
// in the SCOPE line (i.e., as content of the prompt passed by the operator to
// claude).  Programmatic injection of the bead ID (e.g. a HARMONIK_BEAD_ID
// env var) is a post-MVH hardening task; no bead exists for that yet.
//
// # ShowBead — pre-claim status guard (hk-p4xbw)
//
// ShowBead is called between Ready and ClaimBead to confirm the bead is still
// "open" before dispatching.  This is the harmonik-side guard against double-
// dispatch when two concurrent work loops both observe the same bead in the
// Ready list.  The guard has a TOCTOU window (another loop could claim between
// Show and Claim), but this is acceptable at MaxConcurrent>1 because the claim
// semaphore (hk-e61c3.3) serialises claims on this daemon to N at a time.
// Cross-daemon double-dispatch (post-MVH multi-daemon) is addressed by the
// deferred upstream br patch (option 2, out of scope for this bead).
type beadLedger interface {
	Ready(ctx context.Context) ([]core.BeadRecord, error)
	ShowBead(ctx context.Context, id core.BeadID) (core.BeadRecord, error)
	ClaimBead(ctx context.Context, intentLogDir string, cfg brcli.TimeoutConfig, runID core.RunID, transitionID core.TransitionID, beadID core.BeadID) error
	CloseBead(ctx context.Context, intentLogDir string, cfg brcli.TimeoutConfig, runID core.RunID, transitionID core.TransitionID, beadID core.BeadID, needsAttention bool) error
	ReopenBead(ctx context.Context, intentLogDir string, cfg brcli.TimeoutConfig, runID core.RunID, transitionID core.TransitionID, beadID core.BeadID, reason string) error
}

// newWorkLoopDeps constructs the production workLoopDeps from daemon.Config,
// the shared event bus, the pre-resolved workflowModeDefault, and the shared
// hookSessionStore.
//
// workflowModeDefault MUST already be normalised by the caller (daemon.Start
// step 0) — it must be a valid WorkflowMode; zero value is never passed in.
//
// store MUST be non-nil; it is the daemon-wide hook-session registry shared
// between RunSocketListener (as HookRelayHandler) and the work loop completion
// path (WaitForOutcome).
func newWorkLoopDeps(cfg Config, bus handlercontract.EventEmitter, workflowModeDefault core.WorkflowMode, registry *handlercontract.AdapterRegistry, store hookStoreIface) (workLoopDeps, error) {
	if cfg.BrPath == "" {
		return workLoopDeps{}, fmt.Errorf("daemon: newWorkLoopDeps: Config.BrPath is empty; production callers must resolve br from PATH at startup")
	}
	if cfg.ProjectDir == "" {
		return workLoopDeps{}, fmt.Errorf("daemon: newWorkLoopDeps: Config.ProjectDir is empty; required for worktree creation")
	}
	if registry == nil {
		return workLoopDeps{}, fmt.Errorf("daemon: newWorkLoopDeps: adapterRegistry is nil; required by waitAgentReady (hk-d8u1y deleted the nil-guard)")
	}

	// NewForProject pins cmd.Dir to cfg.ProjectDir so `br` discovers the
	// .beads database under the project root, not wherever the operator
	// launched harmonik from (hk-c1ln2: root-cause fix for silent no-claim).
	adapter, err := brcli.NewForProject(cfg.BrPath, cfg.ProjectDir)
	if err != nil {
		return workLoopDeps{}, fmt.Errorf("daemon: newWorkLoopDeps: brcli.NewForProject: %w", err)
	}

	intentLogDir := lifecycle.BeadsIntentsDir(cfg.ProjectDir)

	h := handler.NewHandler(bus, handlercontract.NoopWatcherDeadLetter{}, registry)

	binary := cfg.HandlerBinary
	if binary == "" {
		binary = "claude"
	}

	// Resolve daemonBinaryPath: use the value from Config if set, otherwise fall
	// back to "harmonik" for legacy unit-test callers that don't set the field.
	// Production cmd/harmonik/main.go always sets this via os.Executable().
	daemonBinaryPath := cfg.DaemonBinaryPath
	if daemonBinaryPath == "" {
		daemonBinaryPath = "harmonik"
	}

	// Normalise MaxConcurrent: zero value → 1 (default single-threaded behavior when unset).
	maxConcurrent := cfg.MaxConcurrent
	if maxConcurrent <= 0 {
		maxConcurrent = 1
	}

	// Inject HARMONIK_PROJECT_HASH into every handler subprocess env (hk-nvrvp).
	//
	// The provenance marker is prepended so it is present even when
	// Config.HandlerEnv is nil (the MVH default).  Callers that supply their own
	// HandlerEnv retain all their entries; the hash entry is first so it is easy
	// to spot in /proc/<pid>/environ debugging.
	//
	// Spec ref: docs/dogfood-smoke-trace.md §4; process-lifecycle.md §4.2 PL-006a.
	projectHash := lifecycle.ComputeProjectHash(cfg.ProjectDir)
	handlerEnv := make([]string, 0, 1+len(cfg.HandlerEnv))
	handlerEnv = append(handlerEnv, lifecycle.ProvenanceEnvVar(projectHash))
	handlerEnv = append(handlerEnv, cfg.HandlerEnv...)

	return workLoopDeps{
		brAdapter:           adapter,
		bus:                 bus,
		h:                   h,
		intentLogDir:        intentLogDir,
		projectDir:          cfg.ProjectDir,
		handlerBinary:       binary,
		daemonBinaryPath:    daemonBinaryPath,
		handlerArgs:         cfg.HandlerArgs,
		handlerEnv:          handlerEnv,
		brTimeoutCfg:        brcli.TimeoutConfig{},
		tidGen:              core.NewTransitionIDGenerator(),
		workflowModeDefault: workflowModeDefault,
		runRegistry:         NewRunRegistry(),
		maxConcurrent:       maxConcurrent,
		hookStore:           store,
		adapterRegistry:     registry,
		substrate:           cfg.Substrate, // nil falls back to exec.CommandContext; set by composition root (hk-kqdpf.4)
		agentReadyTimeout:   cfg.AgentReadyTimeout,
		cancelOnQueueDrain:  cfg.CancelOnQueueDrain,
		projectCfg:          cfg.ProjectCfg,
		queueStore:          nil,    // populated by daemon.Start after wiring QueueStore (hk-45ude)
		staleBlockerCloser:  adapter, // hk-rnsjs: auto-close stale blockers on claim failure
	}, nil
}

// runWorkLoop is the main dispatch goroutine. It blocks until ctx is cancelled
// (typically from SIGINT/SIGTERM received by the daemon process). On context
// cancellation it stops accepting new beads, waits for all in-flight goroutines
// to finish, then returns nil. Non-nil errors indicate a fatal setup failure
// within the loop itself (never an error from a single bead run — those are
// absorbed and result in ReopenBead).
//
// Goroutine-per-bead model (hk-e61c3.2, POST_MVH_PARALLELISM_ROADMAP row 5):
//
// Each iteration of the outer poll loop:
//  1. Check context cancellation.
//  2. If runRegistry.Len() >= maxConcurrent: sleep and retry (at capacity).
//  3. Queue-pull path (when queueStore is set and has an active queue):
//     3a. If queue status is paused or completed, idle-wait.
//     3b. Get the active group's eligible items via EligibleItems().
//     3c. Pick the first eligible item, claim it, and dispatch.
//     (EM-015f group-advance evaluation fires in the goroutine on run completion.)
//  4. Fallback br-ready poll (when no queue is loaded): poll br ready; if none
//     sleep and retry. This path preserves backward compatibility for tests and
//     single-bead dispatch that do not use the queue surface.
//  5. Spawn goroutine: Register → dispatch (worktree+handler) → Unregister.
//
// Goroutine dispatch path:
//  1. resolveHEAD + CreateWorktree.
//  2. emitRunStarted (with optional queue_id + queue_group_index).
//  3. Route to mode-specific driver (review-loop or single).
//  4. CloseBead or ReopenBead based on outcome.
//  5. On queue-dispatched run: update item status + evaluate EM-015f group advance.
//  6. removeWorktree.
//  7. Unregister from runRegistry.
//
// At MaxConcurrent=1 the loop is semantically equivalent to the prior serial
// implementation: only one goroutine is ever in-flight, so the poll loop
// blocks on capacity before polling again.
//
// Shutdown: when ctx is cancelled the outer loop exits immediately. The
// embedded WaitGroup wg waits for all in-flight goroutines to drain before
// runWorkLoop returns, satisfying the per-run Drain guarantee (hk-fx6zl).
//
// Spec ref: specs/execution-model.md §7.4 (TS-1 dispatch loop pseudocode);
// §4.3.EM-015f (group-advance gate).
// Bead ref: hk-45ude.
func runWorkLoop(ctx context.Context, deps workLoopDeps) error {
	// wg tracks all in-flight bead goroutines. runWorkLoop waits on this before
	// returning so callers know all bead work is complete on return.
	var wg sync.WaitGroup

	// effectiveMax: 0-value → 1 to preserve MVH single-threaded default.
	effectiveMax := deps.maxConcurrent
	if effectiveMax <= 0 {
		effectiveMax = 1
	}

	// claimSem is a buffered-channel semaphore (hk-e61c3.3, POST_MVH_PARALLELISM_ROADMAP
	// row 9) that bounds the number of simultaneous ClaimBead SQLite write calls to
	// effectiveMax. A token is acquired before ClaimBead and released immediately
	// after, keeping the SQLite write surface narrow even as effectiveMax goroutines
	// run concurrently. This prevents "BrDbLocked" storms under N>5 ready beads.
	//
	// Anti-pattern (roadmap §6): do NOT push the semaphore into brAdapter. The
	// ceiling belongs here in the work-loop scheduler.
	//
	// Bead ref: hk-e61c3.3.
	claimSem := make(chan struct{}, effectiveMax)

	// Initialise the held-event dedup map (hk-kac8g).  Written only from this
	// goroutine (outer poll loop) — no locking needed.
	if deps.heldEventDedup == nil {
		deps.heldEventDedup = make(map[string]struct{})
	}

	// dispatchCtx is the context checked by the outer poll loop to decide
	// whether to halt dispatch. It is separate from ctx (the main daemon context)
	// so that CancelOnQueueDrain/CancelOnQueueExit can stop the dispatch loop
	// without cancelling in-flight goroutines (hk-2o2i9).
	//
	// When deps.stopDispatchCtx is set (wired from Config.StopDispatchCtx by the
	// harmonik run subcommand), the outer loop halts when stopDispatchCtx is
	// cancelled. In-flight goroutines still receive ctx and are unaffected.
	//
	// When deps.stopDispatchCtx is nil, dispatchCtx falls back to ctx, preserving
	// the prior behavior for normal daemon operation and existing tests.
	dispatchCtx := ctx
	if deps.stopDispatchCtx != nil {
		dispatchCtx = deps.stopDispatchCtx
	}

	// exitClean terminates the loop cleanly: it waits for in-flight goroutines,
	// then drains any still-active queue to QueueStatusCancelled so the next
	// harmonik run can start without the QM-027 "already active" guard blocking
	// it (hk-ppt32). The background context is intentional: by the time exitClean
	// runs, ctx is always cancelled; queue.Persist needs a live context.
	exitClean := func() error {
		wg.Wait()
		drainCancelledQueue(context.Background(), deps)
		return nil
	}

	for {
		// Step 1: check for dispatch-halt before pulling new work.
		// Uses dispatchCtx (not ctx) so that CancelOnQueueDrain/CancelOnQueueExit
		// stop dispatch without cancelling in-flight goroutines (hk-2o2i9).
		select {
		case <-dispatchCtx.Done():
			return exitClean()
		default:
		}

		// Step 2: capacity gate — if at the concurrent limit, sleep and retry.
		if deps.runRegistry.Len() >= effectiveMax {
			if sleepErr := workloopSleep(dispatchCtx, workloopPollInterval, deps.submitWakeC); sleepErr != nil {
				return exitClean()
			}
			continue
		}

		// Step 3: dispatch source — queue-pull or br-ready fallback.
		//
		// When queueStore is set and has an active queue, pull from the head of
		// the active group per execution-model.md §7.4 (TS-1). The br-ready
		// poll path is the backward-compatible fallback for tests and single-bead
		// dispatch that do not use the queue surface (spec: daemon MUST NOT fall
		// back to br ready when a queue is loaded, per EM-015f).
		//
		// Bead ref: hk-45ude.

		var (
			beadRecord           core.BeadRecord
			queueItemIndex       int // item index within the group (-1 = no queue)
			queueIDField         *string
			queueGroupIdxFd      *int
			capturedExtraContext string // hk-boiwe: per-item context from queue.Item.Context
			capturedItemWFMode   string // hk-hiqrl: per-item workflow mode from queue.Item.WorkflowMode
			capturedItemWFRef    string // hk-qo9pq: per-item workflow ref from queue.Item.WorkflowRef
		)
		queueItemIndex = -1 // sentinel: not queue-dispatched

		if deps.queueStore != nil {
			// Phase 1 — snapshot queue state under write lock.
			//
			// The previous pattern called deps.queueStore.Queue() (which immediately
			// releases the read lock) and then read q.Status, group statuses, and item
			// statuses without holding any lock. This raced with per-run goroutines that
			// write those fields inside evaluateGroupAdvanceWithOutcome under the write
			// lock. Fix: hold the write lock for the entire initial read so the two
			// never overlap.
			//
			// After Phase 1 the lock is released; all queue-derived values are captured
			// as local copies. Phase 2 (handler-pause gate) runs without the lock.
			// Phase 3 (dispatch stamp) re-acquires the write lock for a TOCTOU check.
			var (
				snapItemIdx     int = -1 // -1 → no item found (q==nil or nothing ready)
				snapItemBeadID  core.BeadID
				snapItemContext string
				snapItemWFMode  string
				snapItemWFRef   string
				snapGroupIndex  int
				snapQueueID     string
			)
			{
				lq := deps.queueStore.LockForMutation()
				q := lq.Queue()
				if q != nil {
					// Queue is loaded: check its status per §7.4 pseudocode.
					switch q.Status {
					case queue.QueueStatusPausedByFailure, queue.QueueStatusPausedByDrain, queue.QueueStatusCompleted:
						// Queue is paused or completed — no dispatch; idle-wait.
						lq.Done()
						if sleepErr := workloopSleep(dispatchCtx, workloopPollInterval, deps.submitWakeC); sleepErr != nil {
							return exitClean()
						}
						continue
					}

					// Find the active group and its eligible items.
					activeGroupIdx := -1
					for i := range q.Groups {
						if q.Groups[i].Status == queue.GroupStatusActive {
							activeGroupIdx = i
							break
						}
					}
					if activeGroupIdx < 0 {
						// No active group yet (all pending or all terminal) — idle-wait.
						lq.Done()
						if sleepErr := workloopSleep(dispatchCtx, workloopPollInterval, deps.submitWakeC); sleepErr != nil {
							return exitClean()
						}
						continue
					}

					items := queue.EligibleItems(&q.Groups[activeGroupIdx])
					if len(items) == 0 {
						// All items dispatched or deferred — wait for group progress.
						lq.Done()
						if sleepErr := workloopSleep(dispatchCtx, workloopPollInterval, deps.submitWakeC); sleepErr != nil {
							return exitClean()
						}
						continue
					}

					// Pick the first eligible item and find its index.
					item := items[0]
					for j := range q.Groups[activeGroupIdx].Items {
						it := &q.Groups[activeGroupIdx].Items[j]
						if it.BeadID == item.BeadID && it.Status == queue.ItemStatusPending {
							snapItemIdx = j
							break
						}
					}
					if snapItemIdx < 0 {
						// Item not found (stale reference) — retry.
						lq.Done()
						if sleepErr := workloopSleep(dispatchCtx, workloopPollInterval, deps.submitWakeC); sleepErr != nil {
							return exitClean()
						}
						continue
					}

					// Capture all values needed after lock release as local copies so
					// we do not access the shared *Queue pointer outside the lock.
					snapItemBeadID = item.BeadID
					snapItemContext = item.Context
					snapItemWFMode = item.WorkflowMode
					snapItemWFRef = item.WorkflowRef
					snapGroupIndex = q.Groups[activeGroupIdx].GroupIndex
					snapQueueID = q.QueueID
				}
				lq.Done()
			}

			if snapItemIdx >= 0 {
				// Phase 2 — handler-pause gate (hk-kac8g): check whether the resolved
				// agent type is paused before claiming/dispatching the item.  At MVH all
				// beads map to AgentTypeClaudeCode; multi-agent resolution is post-MVH.
				//
				// When paused:
				//   - The item remains ItemStatus=pending (no stamp, no claim).
				//   - Emit queue_item_held_for_handler_pause at-most-once per
				//     (bead_id, paused_epoch) per §8.11.3 dedup contract.
				//   - Idle-wait and retry on next poll tick.
				//
				// Spec ref: specs/handler-pause.md §6.
				// Bead ref: hk-kac8g.
				if deps.handlerPauseController != nil {
					epoch, isPaused := deps.handlerPauseController.PausedEpochFor(core.AgentTypeClaudeCode)
					if isPaused {
						emitHeldEvent(ctx, deps, snapItemBeadID, core.AgentTypeClaudeCode, epoch)
						if sleepErr := workloopSleep(dispatchCtx, workloopPollInterval, deps.submitWakeC); sleepErr != nil {
							return exitClean()
						}
						continue
					}
				}

				// Phase 3 — stamp item as dispatched under the write lock (TOCTOU).
				{
					lq := deps.queueStore.LockForMutation()
					liveQ := lq.Queue()
					if liveQ == nil {
						lq.Done()
						if sleepErr := workloopSleep(dispatchCtx, workloopPollInterval, deps.submitWakeC); sleepErr != nil {
							return exitClean()
						}
						continue
					}
					// Locate the same group and item in the live snapshot.
					foundItem := false
					for gi := range liveQ.Groups {
						if liveQ.Groups[gi].Status != queue.GroupStatusActive {
							continue
						}
						if liveQ.Groups[gi].GroupIndex != snapGroupIndex {
							continue
						}
						if snapItemIdx < len(liveQ.Groups[gi].Items) &&
							liveQ.Groups[gi].Items[snapItemIdx].BeadID == snapItemBeadID &&
							liveQ.Groups[gi].Items[snapItemIdx].Status == queue.ItemStatusPending {
							runUUIDStr := "" // filled after uuid generation below
							liveQ.Groups[gi].Items[snapItemIdx].Status = queue.ItemStatusDispatched
							liveQ.Groups[gi].Items[snapItemIdx].RunID = &runUUIDStr // placeholder; updated after
							_ = runUUIDStr                                          // suppress lint
							foundItem = true
						}
					}
					if !foundItem {
						lq.Done()
						// Already dispatched by a concurrent path — retry.
						if sleepErr := workloopSleep(dispatchCtx, workloopPollInterval, deps.submitWakeC); sleepErr != nil {
							return exitClean()
						}
						continue
					}
					lq.SetQueue(liveQ)
					// Persist the dispatched-stamp so queue.json reflects the
					// in-memory state (hk-xsutm). Non-fatal: RunID placeholder
					// will be patched shortly; the important invariant is that
					// the item is marked dispatched before any other path reads it.
					if persistErr := queue.Persist(ctx, deps.projectDir, liveQ); persistErr != nil {
						fmt.Fprintf(os.Stderr, "daemon: workloop: Persist dispatch-stamp queueID=%s: %v\n",
							liveQ.QueueID, persistErr)
					}
					lq.Done()
				}

				beadRecord = core.BeadRecord{BeadID: snapItemBeadID}
				queueItemIndex = snapItemIdx
				qID := snapQueueID
				gIdx := snapGroupIndex
				queueIDField = &qID
				queueGroupIdxFd = &gIdx
				capturedExtraContext = snapItemContext // hk-boiwe
				capturedItemWFMode = snapItemWFMode   // hk-hiqrl
				capturedItemWFRef = snapItemWFRef     // hk-qo9pq
			}
		}

		var beadID core.BeadID
		if queueItemIndex < 0 {
			// No queue active — fall back to br-ready poll.
			readyRecords, err := deps.brAdapter.Ready(ctx)
			if err != nil {
				// Treat poll errors as transient: log and backoff.
				if dispatchCtx.Err() != nil {
					return exitClean()
				}
				// Non-fatal: surface to stderr so operators can diagnose CWD/PATH
				// misconfiguration (hk-c1ln2: silent-failure fix).
				fmt.Fprintf(os.Stderr, "daemon: workloop: Ready poll error (will retry): %v\n", err)
				if sleepErr := workloopSleep(dispatchCtx, workloopPollInterval, deps.submitWakeC); sleepErr != nil {
					return exitClean()
				}
				continue
			}

			if len(readyRecords) == 0 {
				if sleepErr := workloopSleep(dispatchCtx, workloopPollInterval, deps.submitWakeC); sleepErr != nil {
					return exitClean()
				}
				continue
			}

			// Pick first ready bead; labels carry workflow:<mode> for mode resolution.
			beadRecord = readyRecords[0]

			// Handler-pause gate for the br-ready path (hk-kac8g): mirror the same
			// check applied in the queue path above.  The bead remains in the br
			// ready queue (not claimed) while the handler is paused.
			//
			// Bead ref: hk-kac8g.
			if deps.handlerPauseController != nil {
				epoch, isPaused := deps.handlerPauseController.PausedEpochFor(core.AgentTypeClaudeCode)
				if isPaused {
					emitHeldEvent(ctx, deps, beadRecord.BeadID, core.AgentTypeClaudeCode, epoch)
					if sleepErr := workloopSleep(dispatchCtx, workloopPollInterval, deps.submitWakeC); sleepErr != nil {
						return exitClean()
					}
					continue
				}
			}
		}
		beadID = beadRecord.BeadID

		runUUID, uuidErr := uuid.NewV7()
		if uuidErr != nil {
			// UUID generation failure is fatal — system entropy problem.
			wg.Wait()
			return fmt.Errorf("daemon: workloop: generate RunID: %w", uuidErr)
		}
		runID := core.RunID(runUUID)

		// Patch the placeholder RunID string in the queue item now that we have it.
		if queueItemIndex >= 0 && deps.queueStore != nil {
			lq := deps.queueStore.LockForMutation()
			liveQ := lq.Queue()
			if liveQ != nil {
				for gi := range liveQ.Groups {
					if liveQ.Groups[gi].Status != queue.GroupStatusActive {
						continue
					}
					if queueGroupIdxFd != nil && liveQ.Groups[gi].GroupIndex != *queueGroupIdxFd {
						continue
					}
					if queueItemIndex < len(liveQ.Groups[gi].Items) &&
						liveQ.Groups[gi].Items[queueItemIndex].Status == queue.ItemStatusDispatched {
						runIDStr := runID.String()
						liveQ.Groups[gi].Items[queueItemIndex].RunID = &runIDStr
					}
				}
				lq.SetQueue(liveQ)
				// Persist the RunID patch (hk-xsutm).
				if persistErr := queue.Persist(ctx, deps.projectDir, liveQ); persistErr != nil {
					fmt.Fprintf(os.Stderr, "daemon: workloop: Persist RunID-patch queueID=%s: %v\n",
						liveQ.QueueID, persistErr)
				}
			}
			lq.Done()
		}

		claimTID, tidErr := deps.tidGen.Next()
		if tidErr != nil {
			wg.Wait()
			return fmt.Errorf("daemon: workloop: generate claim TransitionID: %w", tidErr)
		}

		if queueItemIndex < 0 {
			// br-ready path: pre-claim status guard (hk-p4xbw) + label hydration
			// (hk-a0htu).
			//
			// ShowBead serves two purposes here:
			//   1. Guard: confirm the bead is still open before claiming (TOCTOU
			//      window is acceptable per the claim-semaphore note above).
			//   2. Label hydration: `br ready --format json` (br v0.1.45) does not
			//      include the `labels` field, so BeadRecord.Labels from Ready() is
			//      always nil.  ShowBead returns the full record including labels;
			//      we overwrite beadRecord.Labels so resolveWorkflowMode (tier-1)
			//      and ResolveModelPreference can read per-bead overrides correctly.
			//
			// Queue-path items are already exclusively owned by this loop (set to
			// dispatched under write lock), so the guard is skipped there; their
			// label hydration is handled below after the claim write.
			showRecord, showErr := deps.brAdapter.ShowBead(ctx, beadID)
			if showErr != nil {
				if dispatchCtx.Err() != nil {
					return exitClean()
				}
				fmt.Fprintf(os.Stderr, "daemon: workloop: ShowBead pre-claim check %s error (will retry): %v\n", beadID, showErr)
				if sleepErr := workloopSleep(dispatchCtx, workloopPollInterval, deps.submitWakeC); sleepErr != nil {
					return exitClean()
				}
				continue
			}
			if showRecord.Status != core.CoarseStatusOpen {
				fmt.Fprintf(os.Stderr, "daemon: workloop: bead_claim_skipped %s status=%s (competing claim won)\n", beadID, showRecord.Status)
				if sleepErr := workloopSleep(dispatchCtx, workloopPollInterval, deps.submitWakeC); sleepErr != nil {
					return exitClean()
				}
				continue
			}
			// Hydrate from the full ShowBead record (hk-a0htu).
			beadRecord.Labels = showRecord.Labels
			beadRecord.Title = showRecord.Title
			beadRecord.Description = showRecord.Description
		}

		// Acquire the claim semaphore before the SQLite write (hk-e61c3.3).
		// The select allows dispatch-halt to abort the acquire so the loop
		// does not block indefinitely on shutdown (hk-2o2i9: use dispatchCtx).
		select {
		case claimSem <- struct{}{}:
		case <-dispatchCtx.Done():
			return exitClean()
		}
		claimErr := deps.brAdapter.ClaimBead(ctx, deps.intentLogDir, deps.brTimeoutCfg, runID, claimTID, beadID)
		// Release the semaphore immediately after the write completes.
		<-claimSem
		if claimErr != nil {
			if dispatchCtx.Err() != nil {
				return exitClean()
			}

			// Queue-path: detect bead-level blocking (hk-n91y0).
			//
			// When ClaimBead fails for a queue item, check whether the bead is in
			// "blocked" status in the Beads ledger.  A blocked bead cannot be claimed
			// (the open→in_progress transition is rejected), so reverting to pending
			// and retrying creates a live-lock that starves the remaining pending
			// items in the wave group.
			//
			// If blocked: mark the item failed and call evaluateGroupAdvanceWithOutcome
			// so the wave can proceed without re-queuing this item.
			//
			// If the ShowBead call itself fails (transient ledger error), or the bead
			// is in any other status, fall through to the existing retry path.
			if queueItemIndex >= 0 && deps.queueStore != nil && queueIDField != nil && queueGroupIdxFd != nil {
				if showRecord, showErr := deps.brAdapter.ShowBead(ctx, beadID); showErr == nil &&
					showRecord.Status == core.CoarseStatusBlocked {
					fmt.Fprintf(os.Stderr, "daemon: workloop: ClaimBead %s bead is blocked in ledger — failing queue item (hk-n91y0)\n", beadID)
					evaluateGroupAdvanceWithOutcome(ctx, deps, *queueIDField, *queueGroupIdxFd, queueItemIndex, false)
					continue
				}
			}

			// Claim conflict or transient error — surface to stderr and retry.
			fmt.Fprintf(os.Stderr, "daemon: workloop: ClaimBead %s error (will retry): %v\n", beadID, claimErr)
			// hk-rnsjs: if the bead is blocked by stale dependencies already in
			// main, auto-close them so the next workloop retry can claim the bead.
			autoCloseStaleBlockersOnClaimFailure(ctx, deps, beadID)
			// On queue-path: revert the item back to pending so the loop can retry.
			if queueItemIndex >= 0 && deps.queueStore != nil {
				lq := deps.queueStore.LockForMutation()
				liveQ := lq.Queue()
				if liveQ != nil {
					for gi := range liveQ.Groups {
						if queueGroupIdxFd != nil && liveQ.Groups[gi].GroupIndex != *queueGroupIdxFd {
							continue
						}
						if queueItemIndex < len(liveQ.Groups[gi].Items) {
							liveQ.Groups[gi].Items[queueItemIndex].Status = queue.ItemStatusPending
							liveQ.Groups[gi].Items[queueItemIndex].RunID = nil
						}
					}
					lq.SetQueue(liveQ)
					// Persist the claim-failure revert (hk-xsutm).
					if persistErr := queue.Persist(ctx, deps.projectDir, liveQ); persistErr != nil {
						fmt.Fprintf(os.Stderr, "daemon: workloop: Persist claim-revert queueID=%s: %v\n",
							liveQ.QueueID, persistErr)
					}
				}
				lq.Done()
			}
			if sleepErr := workloopSleep(dispatchCtx, workloopPollInterval, deps.submitWakeC); sleepErr != nil {
				return exitClean()
			}
			continue
		}

		// Queue-path label hydration (hk-a0htu): queue Item carries only BeadID;
		// call ShowBead now (after claim) to populate Labels for resolveWorkflowMode
		// and ResolveModelPreference.  The br-ready path hydrated labels earlier
		// from its pre-claim ShowBead response; this block handles the queue path.
		// Hydration failure is non-fatal: log to stderr and proceed with nil labels
		// (resolveWorkflowMode falls through to tier-3/4 as before the fix).
		if queueItemIndex >= 0 {
			showRecord, showErr := deps.brAdapter.ShowBead(ctx, beadID)
			if showErr != nil {
				fmt.Fprintf(os.Stderr, "daemon: workloop: ShowBead label-hydrate %s error (labels nil, falling through): %v\n", beadID, showErr)
			} else {
				beadRecord.Labels = showRecord.Labels
				beadRecord.Title = showRecord.Title
				beadRecord.Description = showRecord.Description
			}
		}

		// Capture queue context for the goroutine (may be nil for non-queue dispatch).
		capturedQueueID := queueIDField
		capturedQueueGroupIdx := queueGroupIdxFd
		capturedItemIndex := queueItemIndex
		// Per-item overrides captured here; empty for br-ready path.
		capturedCtx := capturedExtraContext  // hk-boiwe
		capturedWFMode := capturedItemWFMode // hk-hiqrl
		capturedWFRef := capturedItemWFRef   // hk-qo9pq

		// Register the run and spawn a goroutine to handle it end-to-end.
		// The goroutine owns Unregister on exit; the outer loop may proceed to
		// claim the next bead immediately (up to effectiveMax).
		deps.runRegistry.Register(runID, &RunHandle{
			BeadID:    beadID,
			StartedAt: time.Now(),
		})
		wg.Add(1)
		go func(runID core.RunID, beadRecord core.BeadRecord, qid *string, qgidx *int, itemIdx int, extraCtx, itemWFMode, itemWFRef string) {
			defer wg.Done()
			defer deps.runRegistry.Unregister(runID)
			// runSucceeded is set by the emitDone closure inside beadRunOne
			// and read here after beadRunOne returns for EM-015f group-advance.
			var runSucceeded bool
			beadRunOne(ctx, deps, runID, beadRecord, qid, qgidx, &runSucceeded, extraCtx, itemWFMode, itemWFRef)
			// EM-015f: after run terminal, evaluate queue group advance.
			if itemIdx >= 0 && deps.queueStore != nil && qid != nil && qgidx != nil {
				evaluateGroupAdvanceWithOutcome(ctx, deps, *qid, *qgidx, itemIdx, runSucceeded)
			}
		}(runID, beadRecord, capturedQueueID, capturedQueueGroupIdx, capturedItemIndex, capturedCtx, capturedWFMode, capturedWFRef)
	}
}

// beadRunOne executes a single claimed bead end-to-end: worktree creation,
// mode dispatch, close/reopen, worktree removal. It is called from within a
// goroutine spawned by the outer poll loop of runWorkLoop.
//
// The function never returns an error; all per-bead failures result in
// ReopenBead so the bead re-enters the ready queue for retry. Fatal conditions
// (UUID generation, worktree setup) are surfaced to stderr and cause the bead
// to be reopened rather than aborting the daemon.
//
// queueID and queueGroupIndex are optional: when non-nil they are stamped into
// run_started / run_completed / run_failed payloads per EM-015a/EM-015b and
// QM-011/QM-012. They are nil for non-queue-dispatched runs.
//
// runSucceeded is a non-nil output pointer (provided by the goroutine wrapper in
// runWorkLoop) that is set by emitDone when the run emits its terminal event.
// The caller reads it after beadRunOne returns to drive the EM-015f group-advance
// evaluation. When nil (legacy callers), success is not tracked.
//
// Bead ref: hk-e61c3.2, hk-45ude.
func beadRunOne(ctx context.Context, deps workLoopDeps, runID core.RunID, beadRecord core.BeadRecord, queueID *string, queueGroupIndex *int, runSucceeded *bool, extraContext string, itemWorkflowMode string, itemWorkflowRef string) {
	beadID := beadRecord.BeadID

	// emitDone is a local wrapper that stamps queue_id + queue_group_index onto
	// every run_completed / run_failed event emitted from this function. Using a
	// closure avoids threading the optional queue fields through every call site.
	// It also records the success outcome via runSucceeded for EM-015f tracking.
	//
	// Spec ref: specs/execution-model.md §4.3.EM-015b; QM-011/QM-012.
	// Bead ref: hk-45ude.
	emitDone := func(success bool, summary string) {
		if runSucceeded != nil {
			*runSucceeded = success
		}
		emitRunCompleted(ctx, deps.bus, runID, success, summary, queueID, queueGroupIndex)
	}

	// Resolve workflow_mode per execution-model.md §4.3.EM-012a.
	// Four-tier precedence: per-bead label → project config (no-op) →
	// daemon default → single. Resolved once at claim time; immutable for
	// the run's lifetime. See moderesolve.go.
	//
	// hk-hiqrl: itemWorkflowMode is a tier-0 per-item override set by the
	// CLI --review-loop flag via queue.Item.WorkflowMode. When set and valid
	// it takes precedence over the full EM-012a walk.
	workflowMode := resolveWorkflowMode(ctx, beadRecord, deps.workflowModeDefault, deps.bus)
	if itemWorkflowMode != "" {
		if candidate := core.WorkflowMode(itemWorkflowMode); candidate.Valid() {
			workflowMode = candidate
		}
	}

	// Resolve (model, effort) per EM-012b four-tier precedence walk.
	// Resolved once at claim time; sealed into the run for its lifetime.
	// The agentType is claude-code for the production path at MVH; it is
	// sourced from the handler binary convention (single adapter at MVH).
	// See modelpreference.go for the resolver and tier-3 defaults.
	resolvedModel, resolvedEffort := ResolveModelPreference(
		ctx,
		beadRecord.Labels,
		core.AgentTypeClaudeCode,
		deps.projectCfg,
		deps.bus,
		string(beadID),
	)

	// Resolve the parent commit (start_from SHA) for worktree creation per
	// WM-005b / BI-009b. resolveParentCommit parses the bead's ## Branching
	// section and resolves start_from to a commit SHA; it falls back to HEAD
	// when the section is absent or start_from is not set. If start_from is
	// present but names a ref that does not exist locally, the error is
	// surfaced as a typed StartFromRefError and the bead is reopened.
	headSHA, headErr := resolveParentCommit(ctx, deps.projectDir, string(beadID), beadRecord.Description)
	if headErr != nil {
		fmt.Fprintf(os.Stderr, "daemon: workloop: resolveParentCommit for bead %s: %v (reopening)\n", beadID, headErr)
		reopenTID, _ := deps.tidGen.Next()
		_ = deps.brAdapter.ReopenBead(ctx, deps.intentLogDir, deps.brTimeoutCfg, runID, reopenTID, beadID,
			fmt.Sprintf("resolve start_from failed: %v", headErr))
		return
	}

	// Resolve lands_on (base branch) for the pre-exit rebase step (hk-mtm0w).
	// resolveBranching is called a second time here (also called inside
	// resolveParentCommit) to extract LandsOn without restructuring
	// resolveParentCommit's return type. The call is cheap (YAML parse + stat).
	// Non-fatal: if resolveBranching fails here, baseBranch is left empty and
	// the agent-task header omits the base_branch line.
	var baseBranch string
	if brCfg, brErr := resolveBranching(ctx, beadRecord.Description, deps.projectDir); brErr == nil {
		baseBranch = brCfg.LandsOn
	}

	wtFactory := deps.worktreeFactory
	if wtFactory == nil {
		wtFactory = productionWorktreeFactory
	}
	wtPath, wtCleanup, wtErr := wtFactory(ctx, deps.projectDir, runID.String(), headSHA)
	if wtErr != nil {
		fmt.Fprintf(os.Stderr, "daemon: workloop: CreateWorktree for bead %s run %s: %v (reopening)\n", beadID, runID.String(), wtErr)
		reopenTID, _ := deps.tidGen.Next()
		_ = deps.brAdapter.ReopenBead(ctx, deps.intentLogDir, deps.brTimeoutCfg, runID, reopenTID, beadID,
			fmt.Sprintf("create worktree failed: %v", wtErr))
		return
	}
	if wtCleanup != nil {
		defer wtCleanup()
	}

	// Emit run_started with optional queue_id + queue_group_index per QM-011/QM-012.
	emitRunStarted(ctx, deps.bus, runID, beadID, wtPath, queueID, queueGroupIndex)

	// Mode-dispatch: route to the mode-specific driver.
	//
	// review-loop mode (EM-015d): multi-iteration implementer→reviewer cycle
	// handled by runReviewLoop in reviewloop.go.
	//
	// dot mode: DOT-defined workflow graph; loader validates the artifact,
	// then (stub) falls through to single-mode until the cascade engine
	// (hk-bf85t) lands.
	//
	// single mode (default MVH): one-shot implementer dispatch.
	switch workflowMode {
	case core.WorkflowModeReviewLoop:
		rlResult := runReviewLoop(ctx, deps, runID, beadID, beadRecord.Title, beadRecord.Description, wtPath, headSHA, resolvedModel, resolvedEffort, extraContext, baseBranch)

		transitionTID, _ := deps.tidGen.Next()
		if rlResult.success {
			// §4.12.EM-052: merge run-branch to main before CloseBead.
			// Mirrors the single-mode merge path (hk-ftyvo).
			mergeRes := mergeRunBranchToMain(ctx, deps.projectDir, runID, deps.bus, beadID, headSHA)
			if !mergeRes.noChange && !mergeRes.success {
				emitOutcomeEmitted(ctx, deps.bus, runID, beadID, "rejected", mergeRes.reason)
				reopenTID, _ := deps.tidGen.Next()
				_ = deps.brAdapter.ReopenBead(ctx, deps.intentLogDir, deps.brTimeoutCfg, runID, reopenTID, beadID,
					fmt.Sprintf("merge-to-main failed: %s", mergeRes.reason))
				emitDone(false, fmt.Sprintf("merge-failed (review-loop): %s", mergeRes.reason))
				return
			}
			emitOutcomeEmitted(ctx, deps.bus, runID, beadID, "approved", "")
			if closeErr := deps.brAdapter.CloseBead(ctx, deps.intentLogDir, deps.brTimeoutCfg, runID, transitionTID, beadID, false); closeErr != nil {
				fmt.Fprintf(os.Stderr, "daemon: workloop: CloseBead (review-loop APPROVE) %s: %v\n", beadID, closeErr)
				emitDone(false, fmt.Sprintf("close-error: %v", closeErr))
			} else {
				emitBeadClosed(ctx, deps.bus, runID, beadID)
				emitDone(true, rlResult.summary)
			}
		} else {
			// Review-loop failed (no_commit, error, etc.) — reopen the bead so
			// it can be retried, rather than closing it with a failed status.
			reopenTID, _ := deps.tidGen.Next()
			if reopenErr := deps.brAdapter.ReopenBead(ctx, deps.intentLogDir, deps.brTimeoutCfg, runID, reopenTID, beadID, rlResult.summary); reopenErr != nil {
				fmt.Fprintf(os.Stderr, "daemon: workloop: ReopenBead (review-loop %s) %s: %v\n", rlResult.completionReason, beadID, reopenErr)
			}
			emitDone(false, rlResult.summary)
		}
		return

	case core.WorkflowModeDot:
		// DOT workflow mode (hk-waj4b): load and validate the .dot artifact,
		// then fall through to single-mode dispatch as a stub until the full
		// cascade engine (hk-bf85t) is implemented.
		//
		// Resolve .dot path: use itemWorkflowRef when set (hk-qo9pq CLI --workflow-ref);
		// fall back to the project-level convention <projectDir>/workflow.dot.
		// Relative itemWorkflowRef is resolved against projectDir.
		dotPath := filepath.Join(deps.projectDir, "workflow.dot")
		if itemWorkflowRef != "" {
			if filepath.IsAbs(itemWorkflowRef) {
				dotPath = itemWorkflowRef
			} else {
				dotPath = filepath.Join(deps.projectDir, itemWorkflowRef)
			}
		}
		graph, loadErr := workflow.LoadDotWorkflow(dotPath)
		if loadErr != nil {
			fmt.Fprintf(os.Stderr, "daemon: workloop: DOT workflow load failed for bead %s run %s: %v (reopening)\n",
				beadID, runID.String(), loadErr)
			reopenTID, _ := deps.tidGen.Next()
			_ = deps.brAdapter.ReopenBead(ctx, deps.intentLogDir, deps.brTimeoutCfg, runID, reopenTID, beadID,
				fmt.Sprintf("workflow_load: %v", loadErr))
			emitDone(false, fmt.Sprintf("workflow_load: %v", loadErr))
			return
		}
		// DOT artifact loaded + validated successfully.
		// TODO(hk-bf85t): hand graph to the cascade engine for full DOT-driven
		// dispatch. Until then, log success and fall through to single-mode.
		fmt.Fprintf(os.Stderr, "daemon: workloop: DOT workflow loaded for bead %s (graph %q, %d nodes, %d edges); falling through to single-mode (cascade engine not yet wired — hk-bf85t)\n",
			beadID, graph.Name, len(graph.Nodes), len(graph.Edges))
		// Fall through to single-mode dispatch below.

	default:
		// WorkflowModeSingle or any normalised-to-single value: fall through
		// to the single-mode dispatch path below.
	}

	// ─── Single-mode dispatch (production path) ───────────────────────────────

	// Step 1: build the Claude launch spec via buildClaudeLaunchSpec.
	daemonSock := filepath.Join(deps.projectDir, ".harmonik", "daemon.sock")
	rc := claudeRunCtx{
		runID:             runID,
		beadID:            string(beadID),
		workspacePath:     wtPath,
		daemonSocket:      daemonSock,
		workflowMode:      workflowMode,
		phase:             "", // empty = single-mode
		iterationCount:    1,
		priorClaudeSessID: nil,
		handlerBinary:     deps.handlerBinary,
		daemonBinaryPath:  deps.daemonBinaryPath,
		baseEnv:           deps.handlerEnv,
		beadTitle:         beadRecord.Title,
		beadDescription:   beadRecord.Description,
		model:             resolvedModel,
		effort:            resolvedEffort,
		// worktreeRootPath is used by buildClaudeLaunchSpec to check whether the
		// workspace is a harmonik-managed worktree for --dangerously-skip-permissions
		// per HC-055b. Derived from projectDir via the standard worktree root formula.
		worktreeRootPath: workspace.WorktreeRootPath(deps.projectDir, workspace.NoWorktreeRootOverride()),
		extraContext:     extraContext, // hk-boiwe: per-item context from queue.Item.Context
		baseBranch:       baseBranch,   // hk-mtm0w: pre-exit rebase target
	}
	specBuilder := deps.launchSpecBuilder
	if specBuilder == nil {
		specBuilder = buildClaudeLaunchSpec
	}
	spec, artifacts, specErr := specBuilder(ctx, rc)
	if specErr != nil {
		fmt.Fprintf(os.Stderr, "daemon: workloop: buildClaudeLaunchSpec bead %s run %s: %v (reopening)\n",
			beadID, runID.String(), specErr)
		reopenTID, _ := deps.tidGen.Next()
		_ = deps.brAdapter.ReopenBead(ctx, deps.intentLogDir, deps.brTimeoutCfg, runID, reopenTID, beadID,
			fmt.Sprintf("build launch spec error: %v", specErr))
		emitDone(false, fmt.Sprintf("build launch spec error: %v", specErr))
		return
	}

	// In production HandlerArgs is always nil and spec.Args already contains the
	// bridge flags (--session-id or --resume) from buildClaudeLaunchSpec.
	// For test fixtures that supply HandlerArgs (e.g. ["-c", "exit 0"]), prepend
	// them so that the bridge flags become extra positional args the fixture can
	// safely ignore (e.g. /bin/sh -c "exit 0" sh --session-id <uuid>).
	if len(deps.handlerArgs) > 0 {
		spec.Args = append(deps.handlerArgs, spec.Args...)
	}

	// Attach the optional tmux substrate (nil at MVH; set from deps.substrate).
	//
	// hk-012af: when deps.substrate is a *tmuxSubstrate, wrap it in a
	// perRunSubstrate so this goroutine gets its own isolated pane handle.
	// Under MaxConcurrent>1, each concurrent beadRunOne call would otherwise
	// race on a shared pane-target; the second SpawnWindow would overwrite the
	// first run's target, causing paste-inject messages to land in the wrong pane
	// and stalling both runs indefinitely (7-hour silent gap in 22:29 UTC dogfood).
	// perRunSubstrate captures the pane ID of *this* goroutine's spawned window
	// and routes all paste-inject I/O there. (hk-jfh59: shared-state methods on
	// tmuxSubstrate removed.)
	var runSubstrate handler.Substrate = deps.substrate
	var runPasteTarget handler.Substrate = deps.substrate // fallback: shared substrate
	if prs := newPerRunSubstrate(deps.substrate); prs != nil {
		runSubstrate = prs
		runPasteTarget = prs
	}
	spec.Substrate = runSubstrate

	// Step 2: register the hook session so incoming Stop-hook relays are routed
	// to this run's hookSessionStore entry (CHB-025).
	deps.hookStore.RegisterHookSession(runID.String(), artifacts.claudeSessionID)
	defer deps.hookStore.CloseHookSession(runID.String(), artifacts.claudeSessionID)

	// Step 3: emit pre-exec messages on the bus BEFORE Launch (CHB-018 ordering).
	// Each message carries a "type" field that maps directly to a core.EventType.
	// Parse it from the raw JSON and use it as the envelope type.
	for _, msg := range artifacts.preExecMsgs {
		emitPreExecMessage(ctx, deps.bus, runID, msg)
	}

	// Step 4: create a per-run tapping emitter so waitAgentReady can observe
	// watcher events without a post-seal bus subscription (EV-009).
	tap, tapCh := newPerRunEventTap(deps.bus, runID)
	// Precondition: deps.adapterRegistry must be non-nil (enforced by
	// newWorkLoopDeps). NewHandler panics on a nil registry (hk-d8u1y).
	runH := handler.NewHandler(tap, handlercontract.NoopWatcherDeadLetter{}, deps.adapterRegistry)

	implementerLaunchedAt := time.Now()
	sess, watcher, launchErr := runH.Launch(ctx, spec)
	if launchErr != nil {
		fmt.Fprintf(os.Stderr, "daemon: workloop: Launch bead %s run %s: %v (reopening)\n",
			beadID, runID.String(), launchErr)
		reopenTID, _ := deps.tidGen.Next()
		_ = deps.brAdapter.ReopenBead(ctx, deps.intentLogDir, deps.brTimeoutCfg, runID, reopenTID, beadID,
			fmt.Sprintf("launch error: %v", launchErr))
		emitDone(false, fmt.Sprintf("launch error: %v", launchErr))
		return
	}

	// Step 4a: wire the agent-ready callback so that incoming agent_ready relay
	// messages from the hook-relay subprocess (CHB-013 / HC-039) are forwarded
	// into tapCh, which waitAgentReady blocks on.
	//
	// Without this call, hookSessionStore.notifyAgentReady finds agentReadyCallback
	// == nil and is a no-op: tapCh stays empty and waitAgentReady always fires
	// ErrAgentReadyTimeout (HC-056). This is the root cause identified in smoke v6
	// (docs/dogfood-smoke-run-2026-05-13-bridge-substrate-v6.md §9, bead hk-lj1p9.4).
	//
	// The callback is invoked from the socket-acceptor goroutine and MUST be
	// non-blocking. tap.Emit is used to forward the event through the same path
	// as watcher events, ensuring waitAgentReady's observer goroutine receives it.
	// context.Background() is intentional: the callback fires asynchronously from
	// a socket-acceptor goroutine whose lifetime is decoupled from ctx; bus.Emit
	// with Background is non-blocking and safe to call after ctx is cancelled.
	//
	// The defer CloseHookSession (step 2 above) ensures the callback is never
	// called after the hook session is torn down: notifyAgentReady reads the
	// callback under the mutex, and CloseHookSession deletes the session entry,
	// so any post-close relay message returns unknown_session before reaching the
	// callback.
	//
	// Ordering: tap is created before Launch (step 4), Launch returns before this
	// call (step 4a), and waitAgentReady is called after (step 6). This ensures
	// the callback is registered before waitAgentReady blocks on tapCh.
	//
	// Spec ref: specs/claude-hook-bridge.md §4.11 CHB-013; specs/handler-contract.md §4.9 HC-056.
	// Bead ref: hk-lj1p9.4.
	// Capture values for the callback closure; claudeSessionID is a plain string
	// (not core.SessionID) so copy it explicitly to avoid capturing a loop var.
	cbRunID := runID
	cbClaudeSessionID := artifacts.claudeSessionID
	deps.hookStore.SetAgentReadyCallback(runID.String(), artifacts.claudeSessionID, func() {
		// hk-5cox8 observability: populate run_id, claude_session_id, and provenance
		// so the emitted agent_ready event in events.jsonl can be correlated per-run.
		// Previously this called tap.Emit with nil payload, producing payload:null
		// in the JSONL and making it impossible to determine which runs received
		// agent_ready and which timed out.
		pl := core.AgentReadyPayload{
			RunID:           cbRunID,
			SessionID:       core.SessionID(cbClaudeSessionID),
			Capabilities:    []string{},
			ClaudeSessionID: cbClaudeSessionID,
			Provenance:      "claude_session_start",
		}
		b, marshalErr := json.Marshal(pl)
		if marshalErr != nil {
			// Fallback: emit without payload rather than silently dropping the event.
			_ = tap.Emit(context.Background(), core.EventTypeAgentReady, nil)
			return
		}
		_ = tap.Emit(context.Background(), core.EventTypeAgentReady, b)
	})

	// Step 4b: paste-inject the kick-off message into the Claude pane (hk-zrj83).
	//
	// Step 5: start CHB-019 heartbeat goroutine.  Daemon-owned per OQ5 resolution.
	hbDone := make(chan struct{})
	go handler.RunHeartbeatLoop(ctx, artifacts.handlerSessionID,
		handler.HeartbeatInterval, hbDone,
		newDaemonHeartbeatEmitter(tap, runID))
	defer close(hbDone)

	// Step 6: waitAgentReady — HC-056 agent_ready timeout guard.
	//
	// Precondition: deps.adapterRegistry is non-nil (enforced by newWorkLoopDeps;
	// hk-d8u1y). Obtain the adapter from the registry for DetectReady.
	//
	// HC-056 timeout semantics: we only treat this as a hard failure requiring
	// reopen if the SPECIFIC HC-056 timeout sentinel (ErrAgentReadyTimeout)
	// fires. If the watcher exits first (handler crash, clean exit without
	// agent_ready) the watcher-done cancel fires first, returning
	// context.Canceled — in that case we skip the reopen and fall through to
	// the normal waitWithSocketGrace path which handles the exit correctly per
	// CHB-020 branch 3.
	adapter, adapterErr := deps.adapterRegistry.ForAgent(core.AgentTypeClaudeCode)
	if adapterErr != nil {
		// No adapter for claude-code — non-fatal; skip ready-wait.
		fmt.Fprintf(os.Stderr, "daemon: workloop: ForAgent(claude-code) bead %s: %v (skipping ready-wait)\n",
			beadID, adapterErr)
	} else {
		// Derive a child context that cancels when the watcher finishes (handler
		// exit). This prevents waitAgentReady from blocking for the full timeout
		// when the handler exits before emitting agent_ready (e.g. a crash).
		//
		// Substrate path: watcher is nil when deps.substrate != nil (tmux-hosted
		// sessions return watcher=nil; completion flows via HookSessionStore.WaitForOutcome).
		// Skip the watcher-done goroutine in that case — readyCtx is still valid
		// and will be cancelled by the outer ctx or readyCancel below.
		readyCtx, readyCancel := context.WithCancel(ctx)
		if watcher != nil {
			go func() {
				select {
				case <-watcher.Done():
					readyCancel()
				case <-readyCtx.Done():
				}
			}()
		}

		eventSrc := newChanAgentEventSource(tapCh)
		readyErr := waitAgentReady(readyCtx, runID, eventSrc, adapter, deps.agentReadyTimeout)
		readyCancel() // always release the watcher-done goroutine above

		if readyErr == ErrAgentReadyTimeout {
			// HC-056: agent_ready_timeout — kill, reap, reopen.
			fmt.Fprintf(os.Stderr, "daemon: workloop: waitAgentReady bead %s run %s: %v (reopening)\n",
				beadID, runID.String(), readyErr)
			_ = sess.Kill(ctx)
			if watcher != nil {
				// Wait for the watcher goroutine to exit, but do not block
				// indefinitely — agentReadyKillReapTimeout guards against a
				// hung watcher after SIGKILL. The bead is still reopened even
				// if reaping times out; the watcher goroutine will unblock
				// when the outer ctx is eventually cancelled.
				// Bead ref: hk-do7te.
				select {
				case <-watcher.Done():
				case <-time.After(agentReadyKillReapTimeout):
					fmt.Fprintf(os.Stderr, "daemon: workloop: watcher.Done() reap timed out bead %s run %s after Kill — continuing\n",
						beadID, runID.String())
				}
			}
			_ = sess.Wait(ctx)
			reopenTID, _ := deps.tidGen.Next()
			if reopenErr := deps.brAdapter.ReopenBead(ctx, deps.intentLogDir, deps.brTimeoutCfg, runID, reopenTID, beadID,
				"agent_ready_timeout"); reopenErr != nil {
				// ReopenBead failed: the bead remains in_progress and will NOT be
				// re-dispatched by the poll loop (Ready only returns open beads).
				// Log loudly so the operator can detect the stuck bead and recover
				// manually (e.g. `br update <id> --status open`).
				// Bead ref: hk-kqdpf.8.
				fmt.Fprintf(os.Stderr, "daemon: workloop: ReopenBead FAILED bead %s run %s: %v — bead is stuck in_progress; operator must reopen manually\n",
					beadID, runID.String(), reopenErr)
			}
			// hk-5cox8 observability: emit agent_ready_timeout to events.jsonl so
			// post-hoc analysis can distinguish "never ready" runs from runs that
			// received agent_ready. Previously the only evidence of this failure
			// was a stderr log line and ErrAgentReadyTimeout return; neither was
			// captured in the durable event stream.
			emitAgentReadyTimeout(ctx, deps.bus, runID, cbClaudeSessionID, deps.agentReadyTimeout)
			emitDone(false, "agent_ready_timeout")
			return
		}
		// readyErr == nil (agent_ready observed) OR context.Canceled (watcher
		// exited first, outer ctx cancelled, or watcher-done cancel).
		// Fall through to waitWithSocketGrace.
	}

	// Step 6a: pasteInjectOnLaunch — deliver "Please read .harmonik/agent-task.md
	// and begin." (or phase-appropriate equivalent) to the tmux pane via
	// WriteLastPane.
	//
	// MUST run AFTER waitAgentReady returns (smoke v9 RED, hk-zchbu): when
	// paste-inject fires before agent_ready, the trailing \n is consumed by
	// Claude Code's welcome-splash render before the REPL input state is
	// active; the buffered text sits in the input bar unsubmitted, claude
	// never reads agent-task.md, HC-056 never fires (the splash itself
	// doesn't emit SessionStart on its own), and the run hangs.
	//
	// Errors are logged to stderr but non-fatal (PL-021d).
	//
	// Spec ref: specs/process-lifecycle.md §4.7 PL-021d; specs/claude-hook-bridge.md §4.11 CHB-028.
	// Bead ref: hk-lj1p9.4 (wiring), hk-zchbu (ordering).
	briefDelivered := pasteInjectOnLaunch(ctx, runPasteTarget, artifacts.claudeSessionID,
		handlercontract.ReviewLoopPhase(rc.phase), rc.iterationCount, wtPath)

	// Step 6b: pasteInjectQuitOnCommit — after the task commit lands in the
	// worktree, send `/quit Enter` to Claude Code's REPL to trigger the Stop
	// hook and unblock the workloop (CHB-028 session-completion-instruction,
	// hk-cmybm).
	//
	// Background: in interactive TUI mode the Stop hook fires on session exit
	// (/quit or Ctrl-C) — NOT after each assistant response.  Claude Code agents
	// cannot execute slash commands from their tool API; the daemon detects the
	// commit and injects /quit programmatically via tmux send-keys.
	//
	// The goroutine polls the worktree HEAD every 500ms.  When HEAD changes from
	// headSHA (the pre-commit parent), it sends /quit.  Non-fatal on error.
	//
	// hk-012af: use runPasteTarget (per-run substrate) so /quit targets this
	// run's pane, not the shared "last pane" which may have been overwritten by
	// a concurrent beadRunOne goroutine.
	//
	// hk-930o3: briefDelivered is passed so pasteInjectQuitOnCommit blocks on
	// brief delivery before starting the commit poll loop, preventing a stale
	// tmux pane /exit race.
	//
	// Spec ref: specs/claude-hook-bridge.md §4.11 CHB-028.
	// Beads: hk-cmybm, hk-930o3.
	// noChangeTimeoutCh is closed by pasteInjectQuitOnCommit when it kills the
	// session after commitPollTimeout without a new commit (hk-trjef).  The
	// workloop checks it non-blockingly in the default switch branch to
	// distinguish a forced-kill from a genuine agent failure.
	//
	// hk-7srrd: pass tapCh so pasteInjectQuitOnCommit can track agent_heartbeat
	// events and use heartbeat staleness as the primary kill trigger instead of
	// a fixed wall-clock deadline.  tapCh is the same channel used by
	// waitAgentReady; both goroutines consume from it concurrently (each sees
	// a copy of each event because perRunEventTap fans out to the channel).
	// waitAgentReady returns before this goroutine is launched (step 6 above),
	// so there is no competition for the agent_ready event.
	var noChangeTimeoutCh chan struct{}
	if qs, ok := runPasteTarget.(quitSender); ok {
		noChangeTimeoutCh = make(chan struct{})
		go pasteInjectQuitOnCommit(ctx, qs, sess, wtPath, headSHA, noChangeTimeoutCh, briefDelivered, tapCh)
	}

	// Step 7: wait for the watcher to finish (handler exit or ctx cancel) then
	// apply the stop-hook grace window for a pending outcome_emitted payload.
	socketOutcome, ei := waitWithSocketGrace(ctx, deps.hookStore, watcher, sess,
		runID.String(), artifacts.claudeSessionID)

	// hk-e6mtt: destroy the tmux window after the session completes so dead panes
	// do not persist after run-fail/cancel. On the natural-exit path (claude /quit),
	// only the process exited; the tmux pane window remains until explicitly killed.
	// Kill is idempotent on the substrate path (killOnce guard in tmuxSubstrateSession);
	// the cancel path already called Kill inside waitWithSocketGrace so this is a no-op.
	// Guarded by watcher==nil which is the tmux-substrate indicator (exec path: watcher!=nil).
	if watcher == nil {
		_ = sess.Kill(context.Background())
	}

	// Step 7a: emit implementer_phase_complete (hk-cd8yu).
	//
	// Fires immediately after the implementer session ends regardless of how —
	// normal exit, noChange-timeout kill, or context cancellation — closing the
	// diagnostic gap between run_started and reviewer_launched where silent
	// implementer failures previously produced no structured event.
	//
	// commitLanded is determined by comparing the current worktree HEAD against
	// headSHA.  resolveWorktreeHEAD errors are treated as "not landed" (conservative).
	{
		curHead, _ := resolveWorktreeHEAD(ctx, wtPath)
		commitLanded := curHead != "" && curHead != headSHA
		emitImplementerPhaseComplete(ctx, deps.bus, runID, ei.exitCode, ei.stderrTail,
			commitLanded, time.Since(implementerLaunchedAt))
	}

	// Step 8: map Wait-return to a terminal event (CHB-020 branches 1/2/3).
	term := handler.MapWaitReturnToTerminalEvent(
		artifacts.handlerSessionID, ei.exitCode, ei.waitErr, socketOutcome,
	)

	// Step 9: emit terminal event and close or reopen the bead.
	//
	// Bridge-wired path (CHB-020): the terminal event type drives the decision.
	// When no stop-hook outcome arrived (branch 3) AND the handler exited 0
	// without a watcher error, we fall back to the pre-bridge close-on-exit-0
	// heuristic so that existing test fixtures (shell scripts that exit 0) and
	// MVH twin-blind runs continue to work as expected.
	//
	// The fallback does NOT apply when a stop-hook outcome was observed but
	// contained FAILURE_SIGNAL (branch 2), or when the watcher itself failed
	// (malformed NDJSON, panic, line-too-long) — those are genuine failures.
	//
	// hk-wfbxf: CloseBead errors must not be silently discarded. If CloseBead
	// fails the bead remains in_progress while JSONL would record
	// run_completed=true — split-brain. Emit run_failed instead.
	// Substrate path: watcher is nil; treat as no watcher error.
	var watcherErr error
	if watcher != nil {
		watcherErr = watcher.Err()
	}
	watcherFailed := watcherErr != nil && !isWatcherErrCanceled(watcherErr)
	transitionTID, _ := deps.tidGen.Next()

	// ── Implementer-escaped-worktree guard (hk-6zylj) ─────────────────
	//
	// Defense-in-depth check for implementer cross-contamination: after the
	// implementer exits, inspect the MAIN repo's working tree. If dirty
	// paths exist outside the normal harmonik churn allowlist
	// (.harmonik/, .claude/, .beads/issues.jsonl), the implementer wrote
	// files into the main repo via absolute MAIN-repo paths instead of
	// staying inside its worktree — its run branch will have no commit
	// but main is now dirty. Layer 1 of the fix (worktree-discipline
	// guidance injected into agent-task.md) prevents the escape at the
	// source; this Layer 2 check catches escapes that slip past Layer 1
	// and fails the run loudly instead of letting it appear as a
	// silent no-commit run.
	//
	// Fires BEFORE the no-commit guard so that escape (the more specific
	// failure mode) is reported as such rather than as a generic
	// "no commit" failure. We do NOT auto-restore main: forensic state is
	// more useful than a clean tree, and the operator can recover via
	// `git -C <main> diff` + manual cherry-pick or via the
	// /tmp/escape-recovery.patch pattern.
	//
	// Bead: hk-6zylj.
	if mainDirty, dirtyFiles, escapeErr := checkMainWorkingTreeDirty(ctx, deps.projectDir); escapeErr == nil && mainDirty {
		emitImplementerEscapedWorktree(ctx, deps.bus, runID, beadID, deps.projectDir, dirtyFiles)
		failReason := fmt.Sprintf("implementer_escaped_worktree: %d file(s) dirty in main: %s",
			len(dirtyFiles), strings.Join(dirtyFiles, ", "))
		_ = deps.brAdapter.ReopenBead(ctx, deps.intentLogDir, deps.brTimeoutCfg, runID, transitionTID, beadID, failReason)
		emitDone(false, failReason)
		return
	}

	// ── No-commit guard (hk-mmh8f) ────────────────────────────────────
	//
	// Mirror of the review-loop no-commit guard (hk-9c1v4, reviewloop.go).
	// If the single-mode implementer exits without advancing the worktree
	// HEAD past parentSHA, there is no work to merge or close.  Previously
	// this fell through to the auto-close branch (mergeRes.noChange=true
	// → outcome_emitted=approved + bead_closed + run_completed success=true)
	// even though no code was produced.
	//
	// Per EM-015d (implementer MUST advance HEAD): short-circuit with a
	// failed run when HEAD == headSHA.
	//
	// Bead: hk-mmh8f.
	if curHeadSHA, curHeadErr := resolveWorktreeHEAD(ctx, wtPath); curHeadErr == nil && curHeadSHA == headSHA {
		failReason := fmt.Sprintf("no_commit_during_implementer: HEAD did not advance past parent %s at iteration 1 exit=%d", headSHA, ei.exitCode)
		_ = deps.brAdapter.ReopenBead(ctx, deps.intentLogDir, deps.brTimeoutCfg, runID, transitionTID, beadID, failReason)
		emitDone(false, failReason)
		return
	}

	switch {
	case term.Type == handlercontract.ProgressMsgTypeAgentCompleted:
		// CHB-020 branch 1: stop-hook WORK_COMPLETE or REVIEWER_VERDICT.
		// §4.12.EM-052: merge run-branch to main before CloseBead.
		mergeRes := mergeRunBranchToMain(ctx, deps.projectDir, runID, deps.bus, beadID, headSHA)
		if !mergeRes.noChange && !mergeRes.success {
			// EM-053: non-FF or push failure → reopen.
			emitOutcomeEmitted(ctx, deps.bus, runID, beadID, "rejected", mergeRes.reason)
			reopenTID, _ := deps.tidGen.Next()
			_ = deps.brAdapter.ReopenBead(ctx, deps.intentLogDir, deps.brTimeoutCfg, runID, reopenTID, beadID,
				fmt.Sprintf("merge-to-main failed: %s", mergeRes.reason))
			emitDone(false, fmt.Sprintf("merge-failed (agent_completed): %s", mergeRes.reason))
		} else {
			// Merge succeeded (or no-change); proceed with CloseBead.
			emitOutcomeEmitted(ctx, deps.bus, runID, beadID, "approved", "")
			if closeErr := deps.brAdapter.CloseBead(ctx, deps.intentLogDir, deps.brTimeoutCfg, runID, transitionTID, beadID, false); closeErr != nil {
				fmt.Fprintf(os.Stderr, "daemon: workloop: CloseBead (agent_completed) %s: %v\n", beadID, closeErr)
				emitDone(false, fmt.Sprintf("close-error: %v", closeErr))
			} else {
				emitBeadClosed(ctx, deps.bus, runID, beadID)
				emitDone(true, "agent_completed: stop-hook outcome")
			}
		}

	case socketOutcome == nil && ei.exitCode == exitCodeClean && !watcherFailed:
		// No stop-hook arrived AND handler exited 0 without watcher error.
		// Fall back to the pre-bridge close-on-exit-0 heuristic for
		// MVH twin-blind runs.
		//
		// hk-wfbxf: same CloseBead error handling as branch 1.
		// §4.12.EM-052: merge run-branch to main before CloseBead.
		mergeRes := mergeRunBranchToMain(ctx, deps.projectDir, runID, deps.bus, beadID, headSHA)
		if !mergeRes.noChange && !mergeRes.success {
			// EM-053: non-FF or push failure → reopen.
			emitOutcomeEmitted(ctx, deps.bus, runID, beadID, "rejected", mergeRes.reason)
			reopenTID, _ := deps.tidGen.Next()
			_ = deps.brAdapter.ReopenBead(ctx, deps.intentLogDir, deps.brTimeoutCfg, runID, reopenTID, beadID,
				fmt.Sprintf("merge-to-main failed: %s", mergeRes.reason))
			emitDone(false, fmt.Sprintf("merge-failed (auto-close): %s", mergeRes.reason))
		} else {
			// Merge succeeded (or no-change); proceed with CloseBead.
			emitOutcomeEmitted(ctx, deps.bus, runID, beadID, "approved", "")
			if closeErr := deps.brAdapter.CloseBead(ctx, deps.intentLogDir, deps.brTimeoutCfg, runID, transitionTID, beadID, false); closeErr != nil {
				fmt.Fprintf(os.Stderr, "daemon: workloop: CloseBead %s: %v\n", beadID, closeErr)
				emitDone(false, fmt.Sprintf("close-error: %v", closeErr))
			} else {
				emitBeadClosed(ctx, deps.bus, runID, beadID)
				emitDone(true, "auto-close: exit=0")
			}
		}

	default:
		// noChange-timeout path (hk-trjef): pasteInjectQuitOnCommit killed the
		// session after commitPollTimeout fired without a new commit.  Check whether
		// the bead was already subsumed by a prior run that landed on main.
		select {
		case <-noChangeTimeoutCh:
			if beadAlreadySubsumedInMain(ctx, deps.projectDir, beadID) {
				emitOutcomeEmitted(ctx, deps.bus, runID, beadID, "approved", "")
				if closeErr := deps.brAdapter.CloseBead(ctx, deps.intentLogDir, deps.brTimeoutCfg, runID, transitionTID, beadID, false); closeErr != nil {
					fmt.Fprintf(os.Stderr, "daemon: workloop: CloseBead (noChange-subsumed) %s: %v\n", beadID, closeErr)
					emitDone(false, fmt.Sprintf("close-error: %v", closeErr))
				} else {
					emitBeadClosed(ctx, deps.bus, runID, beadID)
					emitDone(true, "noChange-subsumed: bead found in main")
				}
			} else {
				_ = deps.brAdapter.ReopenBead(ctx, deps.intentLogDir, deps.brTimeoutCfg, runID, transitionTID, beadID, "noChange-timeout")
				emitDone(false, "noChange-timeout: no commit in commitPollTimeout window")
			}
		default:
			// CHB-020 branch 2 (FAILURE_SIGNAL), branch 3 with non-zero exit, or
			// watcher failure (malformed NDJSON, panic, etc.).
			var failReason string
			if watcherFailed {
				failReason = fmt.Sprintf("watcher error: %v exit=%d run_id=%s",
					watcherErr, ei.exitCode, runID.String())
			} else if term.SubReason != "" {
				failReason = fmt.Sprintf("agent_failed class=%s sub_reason=%s exit=%d run_id=%s",
					term.Class, term.SubReason, ei.exitCode, runID.String())
			} else {
				failReason = fmt.Sprintf("exit=%d run_id=%s", ei.exitCode, runID.String())
			}
			// Surface stderr tail when available — helps diagnose exit=-1 crashes
			// where the agent produced no NDJSON output (hk-ajhqw).
			if len(ei.stderrTail) > 0 {
				const maxTailInReason = 200
				tail := ei.stderrTail
				truncated := ""
				if len(tail) > maxTailInReason {
					tail = tail[len(tail)-maxTailInReason:]
					truncated = " (truncated)"
				}
				fmt.Fprintf(os.Stderr, "daemon: workloop: bead %s run %s stderr tail%s:\n%s\n",
					beadID, runID.String(), truncated, tail)
				failReason += fmt.Sprintf(" stderr_tail%s=%q", truncated, tail)
			}
			_ = deps.brAdapter.ReopenBead(ctx, deps.intentLogDir, deps.brTimeoutCfg, runID, transitionTID, beadID, failReason)
			emitDone(false, fmt.Sprintf("auto-reopen: %s", failReason))
		}
	}
}

// isWatcherErrCanceled reports whether err is the ErrCanceled sentinel that
// the watcher sets when the session context is cancelled cleanly (not a
// genuine watcher failure).
//
// This mirrors the pre-bridge check in the original single-mode path:
// "watcherFailed := watcherErr != nil && !errors.Is(watcherErr, handlercontract.ErrCanceled)"
//
// Bead ref: hk-gql20.14.
func isWatcherErrCanceled(err error) bool {
	return errors.Is(err, handlercontract.ErrCanceled)
}

// beadAlreadySubsumedInMain checks whether beadID appears as a "Refs: <id>"
// trailer in any of the last 20 commits on main in projectDir.
//
// This is used after a noChange-timeout kill to determine whether the work
// was already completed by a prior run that merged to main — in which case
// the bead should be closed (not reopened).
//
// Returns false on any git error (conservative: treat as not subsumed).
//
// Bead: hk-trjef.
func beadAlreadySubsumedInMain(ctx context.Context, projectDir string, beadID core.BeadID) bool {
	cmd := exec.CommandContext(ctx, "git", "log", "main", "--format=%B", "-20")
	cmd.Dir = projectDir
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	return strings.Contains(string(out), "Refs: "+string(beadID))
}

// autoCloseStaleBlockersOnClaimFailure is called after a ClaimBead failure to
// detect and auto-close stale blocker beads whose implementations have already
// landed on main. When br rejects a claim because the target bead is "blocked"
// (has open dependencies not yet closed in Beads), but those dependencies were
// already merged to main, the bead cannot be claimed until the stale blocker
// records are closed.
//
// The function:
//  1. Calls ShowBead to confirm the bead's current status is CoarseStatusBlocked.
//  2. Collects all bead IDs referenced in the bead's edge list (both directions).
//  3. For each candidate blocker, calls beadAlreadySubsumedInMain.
//  4. If subsumed, calls SweepCloseBead to close the stale record.
//
// On the next workloop retry the bead should no longer be blocked and
// ClaimBead will succeed.
//
// No-op when deps.staleBlockerCloser is nil (backward-compat for test stubs
// that do not set this field).
//
// Bead ref: hk-rnsjs.
func autoCloseStaleBlockersOnClaimFailure(ctx context.Context, deps workLoopDeps, beadID core.BeadID) {
	if deps.staleBlockerCloser == nil {
		return
	}
	record, err := deps.brAdapter.ShowBead(ctx, beadID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "daemon: workloop: autoCloseStaleBlockers ShowBead %s: %v\n", beadID, err)
		return
	}
	if record.Status != core.CoarseStatusBlocked {
		return
	}
	// Collect unique bead IDs referenced in edges that are not beadID itself.
	// Both directions are scanned: ShowBead encodes Dependencies (beads this
	// bead blocks; edge From=beadID) and Dependents (beads that block this
	// bead; edge To=beadID). Any bead appearing in either direction is a
	// candidate stale blocker.
	seen := make(map[core.BeadID]struct{})
	for _, edge := range record.Edges {
		if edge.FromBeadID != beadID {
			seen[edge.FromBeadID] = struct{}{}
		}
		if edge.ToBeadID != beadID {
			seen[edge.ToBeadID] = struct{}{}
		}
	}
	for blockerID := range seen {
		if !beadAlreadySubsumedInMain(ctx, deps.projectDir, blockerID) {
			continue
		}
		fmt.Fprintf(os.Stderr, "daemon: workloop: claim-failure auto-close stale blocker %s (subsumed in main, unblocks %s)\n", blockerID, beadID)
		if closeErr := deps.staleBlockerCloser.SweepCloseBead(ctx, deps.brTimeoutCfg, blockerID); closeErr != nil {
			fmt.Fprintf(os.Stderr, "daemon: workloop: SweepCloseBead stale blocker %s: %v\n", blockerID, closeErr)
		}
	}
}

// drainCancelledQueue transitions the active queue (if any) to
// QueueStatusCancelled and archives the file so that the next harmonik run
// invocation can proceed without the QM-027 "already active" guard blocking it.
//
// This is called on every clean exit of runWorkLoop — when ctx is cancelled due
// to SIGINT, SIGTERM, or a timeout — after wg.Wait() ensures all in-flight
// goroutines have completed. The function is a no-op when:
//   - deps.queueStore is nil (no queue surface in use).
//   - The in-memory queue is nil (already cleared by ClearQueue).
//   - The queue is already in a terminal state (paused-by-failure, completed,
//     cancelled) — evaluateGroupAdvanceWithOutcome already transitioned it.
//
// Uses context.Background() because ctx is always cancelled by the time this
// runs; queue.CancelQueueOnShutdown needs a non-cancelled context for Persist.
//
// Errors are logged to stderr but do not block shutdown.
//
// Spec ref: specs/queue-model.md §8 (shutdown drain).
// Bead ref: hk-ppt32.
func drainCancelledQueue(ctx context.Context, deps workLoopDeps) {
	if deps.queueStore == nil {
		return
	}
	lq := deps.queueStore.LockForMutation()
	q := lq.Queue()
	if q == nil || q.Status != queue.QueueStatusActive {
		lq.Done()
		return
	}
	// Queue is still active: transition to cancelled and archive.
	lq.Done() // release lock before I/O
	if err := queue.CancelQueueOnShutdown(ctx, deps.projectDir, q); err != nil {
		fmt.Fprintf(os.Stderr, "daemon: workloop: drainCancelledQueue queueID=%s: %v\n", q.QueueID, err)
		return
	}
	// Clear in-memory state so callers that inspect qs.Queue() see nil.
	deps.queueStore.ClearQueue()
}

// workloopSleep sleeps for d or until ctx is cancelled. Returns a non-nil
// error only when ctx is cancelled. wakeC may be nil: receive from a nil
// channel blocks forever, so the nil case is never selected and the function
// degrades to a plain timer sleep. Bead ref: hk-24xn1 (wakeC parameter).
func workloopSleep(ctx context.Context, d time.Duration, wakeC <-chan struct{}) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(d):
		return nil
	case <-wakeC:
		return nil
	}
}

// productionWorktreeFactory is the default worktreeFactory: creates a real git
// worktree under the project's .harmonik/worktrees/ directory and returns the
// path plus a cleanup function that removes it.
//
// Bead ref: hk-kqdpf.1.
func productionWorktreeFactory(ctx context.Context, projectDir, runID, headSHA string) (string, func(), error) {
	if err := workspace.CreateWorktree(ctx, projectDir, runID, headSHA, workspace.NoWorktreeRootOverride()); err != nil {
		return "", nil, err
	}
	wtPath := workspace.WorktreePath(projectDir, runID, workspace.NoWorktreeRootOverride())
	// The cleanup uses background context so removal is attempted even when the
	// per-bead context has been cancelled (e.g. on daemon shutdown or test
	// cancellation). This mirrors the intent of the original `defer removeWorktree`
	// call — git worktree prune is best-effort.
	cleanup := func() {
		removeWorktree(context.Background(), projectDir, wtPath)
	}
	return wtPath, cleanup, nil
}

// resolveHEAD resolves the current HEAD commit SHA of the git repository at
// repoRoot. Used as the parent-commit start-point for CreateWorktree.
func resolveHEAD(ctx context.Context, repoRoot string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "HEAD")
	cmd.Dir = repoRoot
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("daemon: resolveHEAD: git rev-parse HEAD: %w", err)
	}
	sha := string(out)
	// Trim trailing newline.
	for len(sha) > 0 && sha[len(sha)-1] == '\n' {
		sha = sha[:len(sha)-1]
	}
	if sha == "" {
		return "", fmt.Errorf("daemon: resolveHEAD: git rev-parse HEAD returned empty output")
	}
	return sha, nil
}

// ─────────────────────────────────────────────────────────────────────────────
// Worktree cleanup helpers (hk-fgdgz)
// ─────────────────────────────────────────────────────────────────────────────

// removeWorktree removes the git worktree at wtPath and prunes stale metadata
// from the repository at repoRoot. It uses `git worktree remove --force` twice
// to handle locked worktrees (the second --force overrides the lock).
//
// Errors are non-fatal: the work loop continues even if cleanup fails (orphan
// sweep at next startup will recover stale worktrees per PL-006).
func removeWorktree(ctx context.Context, repoRoot, wtPath string) {
	cmd := exec.CommandContext(ctx, "git", "worktree", "remove", "--force", "--force", wtPath)
	cmd.Dir = repoRoot
	_ = cmd.Run()
}

// emitPreExecMessage emits a single CHB-018 pre-exec progress message on the
// bus using the message's embedded "type" field as the event type.
//
// Each pre-exec message is compact JSON with a top-level "type" field matching
// one of the §8.3 event-type constants (handler_capabilities,
// session_log_location, skills_provisioned, agent_ready). Parsing the type
// avoids emitting all four under a single catch-all envelope, which would
// break per-type JSONL filtering for consumers.
//
// If the type field cannot be parsed the message is still emitted under the
// agent_ready type as a safe fallback (no information is lost; the payload
// is the ground truth).
//
// Spec: specs/claude-hook-bridge.md §4.7 CHB-018.
// Bead: hk-gql20.14.
func emitPreExecMessage(ctx context.Context, bus handlercontract.EventEmitter, runID core.RunID, msg json.RawMessage) {
	var envelope struct {
		Type string `json:"type"`
	}
	eventType := core.EventTypeAgentReady // safe fallback
	if err := json.Unmarshal(msg, &envelope); err == nil && envelope.Type != "" {
		eventType = core.EventType(envelope.Type)
	}
	_ = bus.EmitWithRunID(ctx, runID, eventType, msg)
}

// ─────────────────────────────────────────────────────────────────────────────
// Event helpers
// ─────────────────────────────────────────────────────────────────────────────

// workloopRunStartedPayload is the minimal run_started payload emitted by the
// work loop.  Full RunStartedPayload requires WorkflowID / WorkflowVersion
// which are post-MVH; we emit a raw map so the event is observable without
// requiring a valid RunStartedPayload.Valid() call.
//
// QueueID and QueueGroupIndex are optional: set when the run was dispatched
// from a queue submission per QM-011 / QM-012 (EM-015a).
type workloopRunStartedPayload struct {
	RunID           string  `json:"run_id"`
	BeadID          string  `json:"bead_id"`
	WorkspacePath   string  `json:"workspace_path"`
	StartedAt       string  `json:"started_at"`
	QueueID         *string `json:"queue_id,omitempty"`
	QueueGroupIndex *int    `json:"queue_group_index,omitempty"`
}

// workloopRunCompletedPayload is the minimal run_completed / run_failed payload
// emitted by the work loop.
//
// QueueID and QueueGroupIndex are optional: set when the run was dispatched
// from a queue submission per QM-011 / QM-012 (EM-015b).
type workloopRunCompletedPayload struct {
	RunID           string  `json:"run_id"`
	Success         bool    `json:"success"`
	Summary         string  `json:"summary"`
	EndedAt         string  `json:"ended_at"`
	QueueID         *string `json:"queue_id,omitempty"`
	QueueGroupIndex *int    `json:"queue_group_index,omitempty"`
}

func emitRunStarted(ctx context.Context, bus handlercontract.EventEmitter, runID core.RunID, beadID core.BeadID, wtPath string, queueID *string, queueGroupIndex *int) {
	pl := workloopRunStartedPayload{
		RunID:           runID.String(),
		BeadID:          string(beadID),
		WorkspacePath:   wtPath,
		StartedAt:       time.Now().UTC().Format(time.RFC3339),
		QueueID:         queueID,
		QueueGroupIndex: queueGroupIndex,
	}
	b, err := json.Marshal(pl)
	if err != nil {
		return
	}
	_ = bus.EmitWithRunID(ctx, runID, core.EventTypeRunStarted, b)
}

func emitRunCompleted(ctx context.Context, bus handlercontract.EventEmitter, runID core.RunID, success bool, summary string, queueID *string, queueGroupIndex *int) {
	pl := workloopRunCompletedPayload{
		RunID:           runID.String(),
		Success:         success,
		Summary:         summary,
		EndedAt:         time.Now().UTC().Format(time.RFC3339),
		QueueID:         queueID,
		QueueGroupIndex: queueGroupIndex,
	}
	b, err := json.Marshal(pl)
	if err != nil {
		return
	}
	eventType := core.EventTypeRunCompleted
	if !success {
		eventType = core.EventTypeRunFailed
	}
	_ = bus.EmitWithRunID(ctx, runID, eventType, b)
}

// ─────────────────────────────────────────────────────────────────────────────
// evaluateGroupAdvance — EM-015f group-advance gate (hk-45ude)
// ─────────────────────────────────────────────────────────────────────────────

// evaluateGroupAdvanceWithOutcome is called from the per-run goroutine after a run that
// the run's success outcome from the goroutine wrapper in runWorkLoop.
//
// It marks the queue item terminal (completed/failed), calls AdvanceGroup, and
// emits the resulting group events. If the group transitions to complete-success,
// it also activates the next group (pending → active). If complete-with-failures,
// it marks the queue status as paused-by-failure.
//
// Spec ref: specs/execution-model.md §4.3.EM-015f.
// Bead ref: hk-45ude.
func evaluateGroupAdvanceWithOutcome(ctx context.Context, deps workLoopDeps, queueID string, groupIndex int, itemIdx int, success bool) {
	if deps.queueStore == nil {
		return
	}

	lq := deps.queueStore.LockForMutation()

	q := lq.Queue()
	if q == nil || q.QueueID != queueID {
		lq.Done()
		return
	}

	// Locate the target group.
	groupPos := -1
	for i := range q.Groups {
		if q.Groups[i].GroupIndex == groupIndex {
			groupPos = i
			break
		}
	}
	if groupPos < 0 || itemIdx >= len(q.Groups[groupPos].Items) {
		lq.Done()
		return
	}

	// Mark the item terminal.
	if success {
		q.Groups[groupPos].Items[itemIdx].Status = queue.ItemStatusCompleted
	} else {
		q.Groups[groupPos].Items[itemIdx].Status = queue.ItemStatusFailed
	}

	// Evaluate group-advance gate (EM-015f all-terminal rule).
	newStatus, events, advErr := queue.AdvanceGroup(ctx, &q.Groups[groupPos], q.Status, queueID, time.Now())
	if advErr != nil {
		fmt.Fprintf(os.Stderr, "daemon: workloop: AdvanceGroup queueID=%s groupIndex=%d: %v\n",
			queueID, groupIndex, advErr)
		lq.SetQueue(q)
		lq.Done()
		return
	}

	// Apply new group status.
	q.Groups[groupPos].Status = newStatus

	// If group reached complete-with-failures → queue transitions to paused-by-failure.
	if newStatus == queue.GroupStatusCompleteWithFailures {
		q.Status = queue.QueueStatusPausedByFailure
	}

	// If group reached complete-success → activate the next group.
	if newStatus == queue.GroupStatusCompleteSuccess {
		for i := range q.Groups {
			if q.Groups[i].Status == queue.GroupStatusPending {
				nextStatus, nextEvents, nextErr := queue.AdvanceGroup(ctx, &q.Groups[i], q.Status, queueID, time.Now())
				if nextErr != nil {
					fmt.Fprintf(os.Stderr, "daemon: workloop: AdvanceGroup next group queueID=%s groupIndex=%d: %v\n",
						queueID, q.Groups[i].GroupIndex, nextErr)
				} else {
					q.Groups[i].Status = nextStatus
					events = append(events, nextEvents...)
				}
				break
			}
		}
	}

	// Determine whether the queue has completed: all groups reached
	// complete-success (hk-xsutm). This is the sole condition that triggers
	// CompleteAndUnlink (QM-003). A paused-by-failure queue retains queue.json
	// for operator-driven resume or reset; only the happy-path full-success case
	// removes it.
	allSucceeded := len(q.Groups) > 0
	for i := range q.Groups {
		if q.Groups[i].Status != queue.GroupStatusCompleteSuccess {
			allSucceeded = false
			break
		}
	}

	if allSucceeded {
		// All groups complete-success → CompleteAndUnlink (QM-003 / QM-053).
		// This internally sets q.Status = completed and persists before
		// unlinking queue.json (hk-xsutm).
		if err := queue.CompleteAndUnlink(ctx, deps.projectDir, q); err != nil {
			fmt.Fprintf(os.Stderr, "daemon: workloop: CompleteAndUnlink queueID=%s: %v\n",
				queueID, err)
			// Fall through: still clear in-memory state so the loop isn't stuck.
		}
		lq.Done()
		// Release the write lock before ClearQueue (which acquires its own lock).
		deps.queueStore.ClearQueue()
		// hk-icecw: if a drain-cancel is registered (harmonik run path), cancel
		// the daemon context now so the work loop exits cleanly instead of
		// idle-spinning waiting for more work.
		if deps.cancelOnQueueDrain != nil {
			deps.cancelOnQueueDrain()
		}
		// hk-8jh26 Fix 1: if a queue-exit cancel is registered, fire it on the
		// success path too (covers the case where only cancelOnQueueExit is set).
		if deps.cancelOnQueueExit != nil {
			deps.cancelOnQueueExit()
		}
	} else {
		// Intermediate state or paused-by-failure: persist the updated queue.json
		// so on-disk state matches in-memory after each item completion (hk-xsutm).
		if err := queue.Persist(ctx, deps.projectDir, q); err != nil {
			fmt.Fprintf(os.Stderr, "daemon: workloop: Persist queueID=%s after item completion: %v\n",
				queueID, err)
			// Non-fatal: in-memory state is still updated; file will resync on next persist.
		}
		pausedByFailure := q.Status == queue.QueueStatusPausedByFailure
		lq.SetQueue(q)
		lq.Done()
		// hk-8jh26 Fix 1: if the queue is now paused-by-failure and an exit-cancel
		// is registered (harmonik run path), cancel the daemon context so the work
		// loop exits promptly instead of idle-spinning waiting for more work.
		// pausedByFailure is captured before lq.Done() to avoid a data race with
		// another goroutine that may call CompleteAndUnlink (which writes q.Status)
		// after acquiring the lock we just released.
		if pausedByFailure && deps.cancelOnQueueExit != nil {
			deps.cancelOnQueueExit()
		}
	}

	// Emit the queued events (after lock release above). Bus.Emit is non-blocking
	// per EV-002a so ordering relative to the lock release is acceptable.
	for _, evt := range events {
		raw, err := json.Marshal(evt.Payload)
		if err != nil {
			raw = evt.Payload
		}
		_ = deps.bus.Emit(ctx, core.EventType(evt.Type), raw)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// mergeRunBranchToMain — Step 9 merge-to-main helper (§4.12.EM-052/EM-053)
// ─────────────────────────────────────────────────────────────────────────────

// mergeOutcome carries the result of mergeRunBranchToMain so the caller can
// decide which terminal event sequence to emit.
type mergeOutcome struct {
	// success is true when the merge-and-push completed without error.
	success bool
	// reason is the failure reason for emit/logging when success is false.
	reason string
	// noChange is true when the run-branch has no commits beyond its merge-base
	// with main, i.e. the agent made no commits. The caller proceeds to CloseBead
	// normally (no merge required).
	noChange bool
}

// mergeRunBranchToMainPayload is the JSON payload for outcome_emitted and
// bead_closed events emitted during the merge-to-main sequence.
type mergeRunBranchToMainPayload struct {
	RunID  string `json:"run_id"`
	BeadID string `json:"bead_id"`
	Kind   string `json:"kind"`
	Reason string `json:"reason,omitempty"`
}

// beadClosedPayload is the JSON payload for the bead_closed event.
type beadClosedPayload struct {
	RunID  string `json:"run_id"`
	BeadID string `json:"bead_id"`
}

// workingTreeRefreshFailedPayload is the JSON payload for the
// working_tree_refresh_failed event (§4.12.EM-054).
type workingTreeRefreshFailedPayload struct {
	RunID  string `json:"run_id"`
	BeadID string `json:"bead_id,omitempty"`
	Error  string `json:"error"`
}

// mergeRunBranchToMain implements the §4.12.EM-052 ordered merge sequence:
//
//  1. Resolve run-branch tip.
//  2. Rebase run-branch onto main (hk-j1aq5; rebase_conflict → EM-053 reopen path).
//  3. Fast-forward check (non-FF → EM-053 reopen path).
//  4. git update-ref refs/heads/main <tip>.
//  5. git push origin main.
//  6. git reset --hard HEAD (working-tree refresh, EM-054).
//
// Returns a mergeOutcome. The caller is responsible for all event emission
// and the CloseBead call; this function is a pure git-operation helper.
//
// The bus and beadID parameters are used only for the EM-054 refresh path:
// if git reset --hard HEAD fails, a working_tree_refresh_failed event is
// emitted and the function still returns success=true (the merge succeeded).
//
// Spec ref: specs/execution-model.md §4.12 EM-052, EM-053, EM-054.
// Bead: hk-ftyvo, hk-4goy3.
func mergeRunBranchToMain(ctx context.Context, projectDir string, runID core.RunID, bus handlercontract.EventEmitter, beadID core.BeadID, headSHA string) mergeOutcome {
	runBranch := workspace.TaskBranchName(runID.String())

	// Step 1: resolve run-branch tip.
	runTipCmd := exec.CommandContext(ctx, "git", "rev-parse", "refs/heads/"+runBranch)
	runTipCmd.Dir = projectDir
	runTipOut, err := runTipCmd.Output()
	if err != nil {
		// Branch does not exist — no commits were made; treat as no-change.
		return mergeOutcome{noChange: true}
	}
	runTip := strings.TrimRight(string(runTipOut), "\n")

	// Step 1b: check whether the run-branch has commits beyond its fork point
	// from main.  If mainSHA == runTip the agent made no commits; treat as no-change.
	mainTipCmd := exec.CommandContext(ctx, "git", "rev-parse", "refs/heads/main")
	mainTipCmd.Dir = projectDir
	mainTipOut, err := mainTipCmd.Output()
	if err != nil {
		return mergeOutcome{
			success: false,
			reason:  fmt.Sprintf("git rev-parse main: %v", err),
		}
	}
	mainTip := strings.TrimRight(string(mainTipOut), "\n")

	if mainTip == runTip {
		// Run-branch tip == main tip: no commits were made by the agent.
		return mergeOutcome{noChange: true}
	}

	// hk-cwxow: false-positive guard. If runTip equals the fork-point SHA
	// (headSHA), the agent made no commits regardless of where main now points.
	// Without this check, when main has advanced past headSHA the is-ancestor
	// test correctly fails and the daemon misreports "non_ff_merge" even though
	// the agent did nothing.
	if headSHA != "" && runTip == headSHA {
		return mergeOutcome{noChange: true}
	}

	// Step 2: rebase run-branch onto current main (hk-j1aq5).
	//
	// If main has advanced since the worktree was cut (parallel agents landing
	// concurrently), rebase the run-branch onto main before the FF check.  This
	// turns what would be a non_ff_merge failure into a successful merge as long
	// as there are no conflicts.  On conflict: abort and return rebase_conflict
	// so the bead is reopened (EM-053).
	//
	// Spec ref: specs/execution-model.md §4.12.EM-052 step 2.
	wtPath := workspace.WorktreePath(projectDir, runID.String(), workspace.NoWorktreeRootOverride())
	if _, statErr := os.Stat(wtPath); statErr == nil {
		rebaseCmd := exec.CommandContext(ctx, "git", "rebase", "main")
		rebaseCmd.Dir = wtPath
		if out, rebaseErr := rebaseCmd.CombinedOutput(); rebaseErr != nil {
			// hk-pphof: auto-resolve if ONLY .beads/issues.jsonl is conflicting.
			// The beads ledger file is a JSONL append-only log whose canonical
			// source of truth is main; taking --theirs (main's version) is safe
			// because the daemon owns all terminal bead transitions.
			if conflictOut, autoResolved := mergeRebaseAutoResolveBeadsLedger(ctx, wtPath, out, rebaseErr); autoResolved {
				out = conflictOut // replace output so re-resolve block below runs
			} else {
				abortCmd := exec.CommandContext(ctx, "git", "rebase", "--abort")
				abortCmd.Dir = wtPath
				_ = abortCmd.Run()
				return mergeOutcome{
					success: false,
					reason:  fmt.Sprintf("rebase_conflict: %v\n%s", rebaseErr, strings.TrimRight(string(out), "\n")),
				}
			}
		}
		// Rebase succeeded — re-resolve runTip and mainTip (both may have changed).
		rebasedTipCmd := exec.CommandContext(ctx, "git", "rev-parse", "refs/heads/"+runBranch)
		rebasedTipCmd.Dir = projectDir
		if rebasedOut, rebasedErr := rebasedTipCmd.Output(); rebasedErr == nil {
			runTip = strings.TrimRight(string(rebasedOut), "\n")
		}
		rebasedMainCmd := exec.CommandContext(ctx, "git", "rev-parse", "refs/heads/main")
		rebasedMainCmd.Dir = projectDir
		if rebasedMainOut, rebasedMainErr := rebasedMainCmd.Output(); rebasedMainErr == nil {
			mainTip = strings.TrimRight(string(rebasedMainOut), "\n")
		}
	}

	// Step 3: fast-forward check.  main MUST be an ancestor of runTip.
	// git merge-base --is-ancestor <main> <runTip> exits 0 iff main ⊆ runTip.
	isAncCmd := exec.CommandContext(ctx, "git", "merge-base", "--is-ancestor", mainTip, runTip)
	isAncCmd.Dir = projectDir
	if err := isAncCmd.Run(); err != nil {
		// Non-FF: main has diverged from the run-branch.
		return mergeOutcome{
			success: false,
			reason:  "non_ff_merge: main advanced concurrently",
		}
	}

	// Step 3: fast-forward main to runTip.
	updateRefCmd := exec.CommandContext(ctx, "git", "update-ref", "refs/heads/main", runTip)
	updateRefCmd.Dir = projectDir
	if out, err := updateRefCmd.CombinedOutput(); err != nil {
		return mergeOutcome{
			success: false,
			reason:  fmt.Sprintf("git update-ref main: %v\n%s", err, out),
		}
	}

	// Step 4: push origin main.
	pushCmd := exec.CommandContext(ctx, "git", "push", "origin", "main")
	pushCmd.Dir = projectDir
	if out, err := pushCmd.CombinedOutput(); err != nil {
		// Push failed — roll back the local update-ref so the repo is consistent.
		// Best-effort rollback: if it fails the operator will see main pointing to
		// runTip without a matching remote; reconciliation (Cat 3 / EM-INV-005) will
		// catch this on the next startup.
		rollbackCmd := exec.CommandContext(ctx, "git", "update-ref", "refs/heads/main", mainTip)
		rollbackCmd.Dir = projectDir
		_ = rollbackCmd.Run()
		return mergeOutcome{
			success: false,
			reason:  fmt.Sprintf("push_failed: %v\n%s", err, out),
		}
	}

	// Step 5: refresh project working tree to match HEAD (EM-054).
	//
	// git reset --hard HEAD re-syncs both the index and the working tree to the
	// new HEAD (which is now the run-branch tip). This eliminates the "modified"
	// state that appears in git status when update-ref advances HEAD without
	// touching the working tree files.
	//
	// Uncommitted-changes policy (EM-054): if the working tree has uncommitted
	// changes, log a warning and still reset. The daemon owns the project working
	// tree during operation; the operator is expected to keep it clean.
	//
	// Refresh-failure policy (EM-054): if git reset --hard HEAD fails, the merge
	// is already durable. Log a warning, emit working_tree_refresh_failed, and
	// return success=true so the caller proceeds to CloseBead normally.
	statusCmd := exec.CommandContext(ctx, "git", "status", "--porcelain")
	statusCmd.Dir = projectDir
	if statusOut, statusErr := statusCmd.Output(); statusErr == nil && len(statusOut) > 0 {
		fmt.Fprintf(os.Stderr, "daemon: mergeRunBranchToMain: WARNING: uncommitted changes in project working tree before refresh (bead %s run %s):\n%s",
			beadID, runID.String(), statusOut)
	}

	resetCmd := exec.CommandContext(ctx, "git", "reset", "--hard", "HEAD")
	resetCmd.Dir = projectDir
	if out, resetErr := resetCmd.CombinedOutput(); resetErr != nil {
		// Refresh failed — merge succeeded; emit event and continue.
		fmt.Fprintf(os.Stderr, "daemon: mergeRunBranchToMain: WARNING: git reset --hard HEAD failed (bead %s run %s): %v\n%s",
			beadID, runID.String(), resetErr, out)
		emitWorkingTreeRefreshFailed(ctx, bus, runID, beadID, resetErr)
	}

	return mergeOutcome{success: true}
}

// mergeRebaseAutoResolveBeadsLedger attempts to auto-resolve a rebase conflict
// when the ONLY conflicting file is .beads/issues.jsonl.
//
// The beads ledger is an append-only JSONL log.  main's copy is the canonical
// source of truth (the daemon owns all terminal bead transitions), so "theirs"
// (main's version) always wins.  This eliminates the most common class of
// merge_conflict outcomes caused by parallel agents writing bead close/reopen
// records into the ledger on the same CI run.
//
// If the conflict set contains ANY file other than .beads/issues.jsonl the
// function does nothing and returns (nil, false) so the caller can abort and
// escalate as before.
//
// On success the function:
//  1. Runs git checkout --theirs .beads/issues.jsonl inside the worktree.
//  2. Stages the resolved file with git add.
//  3. Continues the rebase with git rebase --continue.
//
// Returns the combined output of git rebase --continue (for logging) and true
// on success, or (nil, false) if auto-resolve is not applicable or fails.
//
// Bead: hk-pphof.
func mergeRebaseAutoResolveBeadsLedger(ctx context.Context, wtPath string, _ []byte, _ error) ([]byte, bool) {
	const beadsLedgerPath = ".beads/issues.jsonl"

	// Identify conflicting files.
	conflictCmd := exec.CommandContext(ctx, "git", "diff", "--name-only", "--diff-filter=U")
	conflictCmd.Dir = wtPath
	conflictOut, conflictErr := conflictCmd.Output()
	if conflictErr != nil {
		// Cannot determine conflict set; fall back to abort.
		return nil, false
	}

	conflicting := strings.Fields(strings.TrimRight(string(conflictOut), "\n"))
	if len(conflicting) == 0 {
		// No files listed as unmerged — not a conflict we recognise.
		return nil, false
	}

	for _, f := range conflicting {
		if f != beadsLedgerPath {
			// Real code-file conflict; escalate.
			return nil, false
		}
	}

	// Only .beads/issues.jsonl is conflicting — resolve with main's version.
	checkoutCmd := exec.CommandContext(ctx, "git", "checkout", "--theirs", beadsLedgerPath)
	checkoutCmd.Dir = wtPath
	if out, err := checkoutCmd.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "daemon: mergeRebaseAutoResolveBeadsLedger: git checkout --theirs: %v\n%s", err, out)
		return nil, false
	}

	addCmd := exec.CommandContext(ctx, "git", "add", beadsLedgerPath)
	addCmd.Dir = wtPath
	if out, err := addCmd.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "daemon: mergeRebaseAutoResolveBeadsLedger: git add: %v\n%s", err, out)
		return nil, false
	}

	continueCmd := exec.CommandContext(ctx, "git", "-c", "core.editor=true", "rebase", "--continue")
	continueCmd.Dir = wtPath
	continueOut, continueErr := continueCmd.CombinedOutput()
	if continueErr != nil {
		// --continue failed; the caller should abort.
		fmt.Fprintf(os.Stderr, "daemon: mergeRebaseAutoResolveBeadsLedger: git rebase --continue: %v\n%s", continueErr, continueOut)
		// Abort so the worktree is left in a clean state.
		abortCmd := exec.CommandContext(ctx, "git", "rebase", "--abort")
		abortCmd.Dir = wtPath
		_ = abortCmd.Run()
		return nil, false
	}

	return continueOut, true
}

// emitOutcomeEmitted emits an outcome_emitted event with the given kind and
// optional reason. kind is "approved" on success, "rejected" on failure.
//
// Spec ref: specs/execution-model.md §4.12.EM-052, EM-053.
// Bead: hk-ftyvo.
func emitOutcomeEmitted(ctx context.Context, bus handlercontract.EventEmitter, runID core.RunID, beadID core.BeadID, kind, reason string) {
	pl := mergeRunBranchToMainPayload{
		RunID:  runID.String(),
		BeadID: string(beadID),
		Kind:   kind,
		Reason: reason,
	}
	b, err := json.Marshal(pl)
	if err != nil {
		return
	}
	_ = bus.Emit(ctx, core.EventTypeOutcomeEmitted, b)
}

// emitWorkingTreeRefreshFailed emits a working_tree_refresh_failed event when
// git reset --hard HEAD fails after a successful merge-to-main (EM-054).
// The event is informational: the merge is already durable.
//
// Spec ref: specs/execution-model.md §4.12 EM-054.
// Bead: hk-4goy3.
func emitWorkingTreeRefreshFailed(ctx context.Context, bus handlercontract.EventEmitter, runID core.RunID, beadID core.BeadID, refreshErr error) {
	pl := workingTreeRefreshFailedPayload{
		RunID:  runID.String(),
		BeadID: string(beadID),
		Error:  refreshErr.Error(),
	}
	b, err := json.Marshal(pl)
	if err != nil {
		return
	}
	_ = bus.EmitWithRunID(ctx, runID, core.EventTypeWorkingTreeRefreshFailed, b)
}

// emitBeadClosed emits a bead_closed event after a successful CloseBead call.
//
// Spec ref: specs/execution-model.md §4.12.EM-052.
// Bead: hk-ftyvo.
func emitBeadClosed(ctx context.Context, bus handlercontract.EventEmitter, runID core.RunID, beadID core.BeadID) {
	pl := beadClosedPayload{
		RunID:  runID.String(),
		BeadID: string(beadID),
	}
	b, err := json.Marshal(pl)
	if err != nil {
		return
	}
	_ = bus.Emit(ctx, core.EventTypeBeadClosed, b)
}

// checkMainWorkingTreeDirty (hk-6zylj) reports whether the main repo's working
// tree contains dirty files outside the harmonik churn allowlist.
//
// It runs `git -C <mainPath> status --porcelain` and filters the output:
//   - `.harmonik/...`           — daemon state (expected churn)
//   - `.claude/...`             — orchestrator/Claude state (expected churn)
//   - `.beads/issues.jsonl`     — bead ledger (expected churn from br sync)
//
// Anything else dirty is treated as an escape. The returned list contains the
// path portion of each porcelain status line (with the leading XY-and-space
// prefix stripped).
//
// Errors (e.g. git not in PATH) return (false, nil, err) so the caller can
// treat the check as informational and skip without failing the run.
func checkMainWorkingTreeDirty(ctx context.Context, mainPath string) (bool, []string, error) {
	if mainPath == "" {
		return false, nil, fmt.Errorf("checkMainWorkingTreeDirty: empty mainPath")
	}
	cmd := exec.CommandContext(ctx, "git", "-C", mainPath, "status", "--porcelain")
	out, err := cmd.Output()
	if err != nil {
		return false, nil, fmt.Errorf("checkMainWorkingTreeDirty: git status: %w", err)
	}
	var dirty []string
	for _, line := range strings.Split(strings.TrimRight(string(out), "\n"), "\n") {
		if line == "" {
			continue
		}
		// Porcelain v1 format: "XY <path>" (rename: "XY <oldpath> -> <newpath>").
		// The first three runes are the XY status and the separating space.
		if len(line) < 4 {
			continue
		}
		path := line[3:]
		// Handle rename "old -> new": consider the destination path.
		if idx := strings.Index(path, " -> "); idx >= 0 {
			path = path[idx+4:]
		}
		// Strip surrounding quotes (git quotes paths with special chars).
		path = strings.Trim(path, "\"")
		if isHarmonikChurn(path) {
			continue
		}
		dirty = append(dirty, path)
	}
	return len(dirty) > 0, dirty, nil
}

// isHarmonikChurn reports whether a path is part of the expected harmonik
// churn surface that should be excluded from the escape check.
func isHarmonikChurn(path string) bool {
	switch {
	case strings.HasPrefix(path, ".harmonik/"), path == ".harmonik":
		return true
	case strings.HasPrefix(path, ".claude/"), path == ".claude":
		return true
	case path == ".beads/issues.jsonl":
		return true
	}
	return false
}

// emitImplementerPhaseComplete emits an implementer_phase_complete event
// (hk-cd8yu) immediately after the implementer session ends.
//
// stderrTail is the raw stderr bytes captured by waitWithSocketGrace; only the
// first 200 bytes are included in the event payload per the spec.
// duration is the wall-clock time from implementer launch to session end.
//
// Spec ref: hk-cd8yu.
func emitImplementerPhaseComplete(ctx context.Context, bus handlercontract.EventEmitter, runID core.RunID, exitCode int, stderrTail []byte, commitLanded bool, duration time.Duration) {
	const maxStderrHead = 200
	stderrHead := ""
	if len(stderrTail) > 0 {
		head := stderrTail
		if len(head) > maxStderrHead {
			head = head[:maxStderrHead]
		}
		stderrHead = string(head)
	}
	pl := core.ImplementerPhaseCompletePayload{
		RunID:           runID,
		ExitCode:        exitCode,
		StderrTailHead:  stderrHead,
		CommitLanded:    commitLanded,
		DurationSeconds: duration.Seconds(),
	}
	b, err := json.Marshal(pl)
	if err != nil {
		return
	}
	_ = bus.EmitWithRunID(ctx, runID, core.EventTypeImplementerPhaseComplete, b)
}

// emitAgentReadyTimeout emits an agent_ready_timeout event (hk-5cox8) when
// the HC-056 timeout fires — no agent_ready relay message arrived within the
// configured deadline. The event carries run_id, claude_session_id, and
// timeout_ms so post-hoc analysis can correlate which runs never became ready.
//
// effectiveTimeout: zero is replaced by defaultAgentReadyTimeout (30s) to
// match the semantics of waitAgentReady.
func emitAgentReadyTimeout(ctx context.Context, bus handlercontract.EventEmitter, runID core.RunID, claudeSessionID string, effectiveTimeout time.Duration) {
	if effectiveTimeout <= 0 {
		effectiveTimeout = defaultAgentReadyTimeout
	}
	pl := core.AgentReadyTimeoutPayload{
		RunID:           runID,
		ClaudeSessionID: claudeSessionID,
		TimeoutMs:       effectiveTimeout.Milliseconds(),
	}
	b, err := json.Marshal(pl)
	if err != nil {
		return
	}
	_ = bus.EmitWithRunID(ctx, runID, core.EventTypeAgentReadyTimeout, b)
}

// emitImplementerEscapedWorktree emits an implementer_escaped_worktree event
// (hk-6zylj) when the daemon detects post-implementer-exit dirty state in the
// main repo working tree outside the churn allowlist.
func emitImplementerEscapedWorktree(ctx context.Context, bus handlercontract.EventEmitter, runID core.RunID, beadID core.BeadID, mainPath string, dirtyFiles []string) {
	pl := core.ImplementerEscapedWorktreePayload{
		RunID:      runID,
		BeadID:     string(beadID),
		MainPath:   mainPath,
		DirtyFiles: dirtyFiles,
	}
	b, err := json.Marshal(pl)
	if err != nil {
		return
	}
	_ = bus.EmitWithRunID(ctx, runID, core.EventTypeImplementerEscapedWorktree, b)
}
