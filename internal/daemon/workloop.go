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
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/brcli"
	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/digest"
	"github.com/gregberns/harmonik/internal/handler"
	"github.com/gregberns/harmonik/internal/handlercontract"
	hclifecycle "github.com/gregberns/harmonik/internal/handlercontract/lifecycle"
	"github.com/gregberns/harmonik/internal/lifecycle"
	tmuxpkg "github.com/gregberns/harmonik/internal/lifecycle/tmux"
	"github.com/gregberns/harmonik/internal/queue"
	"github.com/gregberns/harmonik/internal/schedule"
	"github.com/gregberns/harmonik/internal/sentinel"
	"github.com/gregberns/harmonik/internal/workers"
	"github.com/gregberns/harmonik/internal/workflow"
	"github.com/gregberns/harmonik/internal/workflow/dot"
	"github.com/gregberns/harmonik/internal/workspace"
)

// workloopPollInterval is the sleep duration used for retry backoff and the
// br-ready fallback poll path (no queue loaded). It is NOT used for
// queue-loaded idle states, which block indefinitely via workloopIdleWait
// per PL-013 (retired-with-stub): idle daemon MUST wait without a periodic
// re-query timer until a queue-submit wake signal or shutdown arrives.
const workloopPollInterval = 2 * time.Second

// shutdownDrainTimeout is the maximum time exitClean waits for in-flight bead
// goroutines to complete their graceful shutdown sequence after the daemon
// context is cancelled (SIGTERM / SIGINT).
//
// In-flight goroutines detect ctx cancellation and run cleanup code such as
// ReopenBead(context.Background(), ...). The worst-case per-goroutine time is
// brcli write-timeout (10 s) + sigtermGrace (5 s) = ~15 s per goroutine.
// Without a bound, three concurrent beads can hold the process alive for ~45 s
// (the original SIGTERM hang observed in hk-az4fd).
//
// This ceiling caps the total drain wait at a predictable window so SIGTERM
// causes the daemon to exit promptly. Goroutines that have not finished within
// the window leave their beads in the in_progress state; QM-002a on next
// startup resets them to open and the queue item recovers to pending.
//
// Bead ref: hk-vlkh4.
const shutdownDrainTimeout = 10 * time.Second

// windowCleaner is the optional interface implemented by substrates that track
// spawned tmux windows. exitClean probes deps.substrate for this interface and
// calls KillAllWindows after wg.Wait() to clean up orphan tmux windows on wave
// completion or daemon exit (hk-j6npz).
type windowCleaner interface {
	KillAllWindows(ctx context.Context) error
}

// maxItemAttempts mirrors queue.MaxItemAttempts for use in the workloop and
// br-ready path. Kept as a package-level alias for readability; the canonical
// value lives in queue.MaxItemAttempts.
//
// Bead ref: hk-6pspu.
const maxItemAttempts = queue.MaxItemAttempts

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

// periodicCoordinatorReapInterval is the default minimum interval between
// successive periodic coordinator-session reap passes in the work loop
// (hk-t08m). 5 minutes balances prompt cleanup against excess tmux chatter.
// Tests may inject a shorter value via workLoopDeps.coordinatorReapInterval.
const periodicCoordinatorReapInterval = 5 * time.Minute

// diskLowWatermarkDefault is the default free-disk threshold below which the
// daemon pauses new bead dispatch and attempts a go-cache reap (hk-sxlb).
// 10 GiB chosen because --max-concurrent 4 with go build can consume ~3–5 GiB
// of build intermediates and worktree content per concurrent run; 10 GiB
// provides enough headroom to finish in-flight runs while rejecting new ones.
const diskLowWatermarkDefault uint64 = 10 * 1024 * 1024 * 1024 // 10 GiB

// diskCheckInterval is the default minimum interval between successive disk
// free-space probes in the work loop (hk-sxlb). 10 minutes is frequent enough
// to catch rapid accumulation (go build cache) without adding syscall overhead
// on every 2-second poll tick.
const diskCheckInterval = 10 * time.Minute

// goCacheCleanInterval is the default minimum interval between proactive
// `go clean -cache` runs in the work loop (hk-sxlb). 60 minutes prevents the
// build cache from accumulating to 20 GiB between disk-low crossings. The
// reactive path (triggered when disk < watermark) runs independently of this
// timer and may fire sooner.
const goCacheCleanInterval = 60 * time.Minute

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

	// allowedRepos is the safelist of absolute repository paths the daemon is
	// permitted to dispatch cross-repo beads against (hk-xfuc). Sourced from
	// .harmonik/config.yaml daemon.allowed_repos at startup. An empty list means
	// no cross-repo dispatch is allowed. See docs/cross-repo-dispatch.md.
	allowedRepos []string

	// worktreeProvisionFiles is the list of repo-root-relative paths copied from
	// the project root into each freshly created LOCAL run worktree after the
	// worktree is created (hk-z8u). Sourced from .harmonik/config.yaml
	// daemon.worktree_provision_files at startup. Empty = no provisioning (the
	// backward-compatible default). Used to materialise gitignored-but-required
	// files (e.g. a `.env` consumed by a docker compose test gate) that
	// `git worktree add` does not check out. Skipped for remote/SSH-worker runs
	// (those worktrees live on another host).
	worktreeProvisionFiles []string

	// kerfPath is the absolute path to the `kerf` CLI binary, or empty when
	// kerf is not installed. When empty, eagerRefillEval returns immediately
	// without calling kerf next (EM-062 disabled for this daemon instance).
	//
	// Spec ref: specs/execution-model.md §4.13 EM-062, EM-063.
	// Bead ref: hk-9321v.
	kerfPath string

	// brPath is the absolute path to the `br` CLI binary, used by the
	// staged-bead generator to create Phase-2 deploy+verify follow-up beads
	// (flywheel-motion.md §5.4 B). Empty → generator is disabled.
	//
	// Bead ref: hk-f722.
	brPath string

	// followUpLedger is the at-most-once idempotency guard for the staged-bead
	// generator (flywheel-motion.md §5.4 B guardrail 4). Keyed on
	// "<beadID>:<class>"; a hit means a follow-up bead was already emitted this
	// daemon session for that (bead, class) pair.
	// Concurrent access is serialised by followUpLedgerMu.
	//
	// Bead ref: hk-f722.
	followUpLedger   map[string]struct{}
	followUpLedgerMu *sync.Mutex

	// followUpLedgerPath is the absolute path to the durable JSONL ledger file
	// (.harmonik/follow-up-ledger.jsonl). The file is loaded at daemon boot to
	// re-seed followUpLedger so restart does not re-emit staged beads that were
	// already created in a prior session. Empty → disk persistence disabled
	// (unit-test mode).
	//
	// Bead ref: hk-3ndb (AC1 — durable staged-bead ledger).
	followUpLedgerPath string

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
	// value is never stored — daemon.Start fails closed (returns an error) if
	// this field would be empty or invalid (PL-004a); the tier-4 hard fallback
	// is dot, NEVER single (EM-012a / EM-012a-FLOOR; hk-30vlb).
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
	// Spec ref: specs/execution-model.md §4.11 EM-049 (in-flight-run capacity gate).
	// Bead ref: hk-e61c3.2.
	runRegistry *RunRegistry

	// maxConcurrent is the ceiling on simultaneously in-flight bead goroutines.
	// Sourced from daemon.Config.MaxConcurrent (zero → 1 per Config godoc).
	// Row 6 (hk-e61c3.1) adds this field to Config; row 5 (this bead) enforces it.
	//
	// POST_MVH_PARALLELISM_ROADMAP §6: enforcement lives here, NOT in the bus
	// or adapter.
	//
	// When concurrencyCtrl is non-nil, the dispatch gate reads the ceiling from
	// the controller atomically each tick instead of this static field, enabling
	// runtime adjustment via queue-set-concurrency RPC (hk-ohiaf).
	//
	// PL-017a(a): hook-bridge relay grandchildren (harmonik hook-relay ...) are
	// spawned by agent subprocesses, never by the dispatch loop, so they are
	// naturally excluded from this ceiling without any explicit gate.
	//
	// Spec ref: specs/execution-model.md §4.11 EM-051 (max_concurrent configuration).
	// Spec ref: specs/process-lifecycle.md §4.5 PL-017a(a) — relay grandchildren
	// not subject to this ceiling.
	// Bead ref: hk-e61c3.2.
	maxConcurrent int

	// concurrencyCtrl is the optional runtime-mutable ceiling controller.
	// When non-nil the dispatch gate reads from it each tick, superseding the
	// static maxConcurrent field. Set by daemon.Start (hk-ohiaf); nil in tests
	// that do not need live adjustment.
	//
	// Bead ref: hk-ohiaf.
	concurrencyCtrl *ConcurrencyController

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

	// mergeMu, when non-nil, is acquired before every mergeRunBranchToMain call
	// and released after it returns. This serialises the rebase → update-ref →
	// push sequence so that concurrent bead goroutines do not race on
	// refs/heads/main: without serialisation, two goroutines can both
	// successfully rebase onto the same mainTip and then one's push arrives
	// on the remote AFTER the other has already advanced it, producing a
	// "non-fast-forward" rejection.
	//
	// Production: newWorkLoopDeps always sets this to a non-nil &sync.Mutex{} so
	// that merges are serialised globally across ALL queues. With named queues,
	// two beads from different queues can complete simultaneously and both enter
	// mergeRunBranchToMain concurrently — the rebase step narrows the window but
	// does not eliminate the non-FF race (hk-yyso7). Tests that need to inject
	// their own mutex may override via WithMergeMutex / daemonTestHooks.mergeMu.
	//
	// Bead ref: hk-bnm89 (scenario-test harness hardening), hk-yyso7 (race fix).
	mergeMu *sync.Mutex

	// emittedEpics tracks parent epic IDs for which epic_completed has already
	// been emitted this daemon session, providing the at-most-once guard (AC-1).
	// Concurrent access is serialised by emittedEpicsMu.
	// Bead ref: hk-w6y70.
	emittedEpics   map[core.BeadID]struct{}
	emittedEpicsMu *sync.Mutex

	// cpRegistry is the daemon's ControlPoint registry, populated from policy
	// YAML during daemon startup per specs/control-points.md §4.9.CP-043.
	//
	// When non-nil, driveDotWorkflow uses it to resolve gate_ref values to
	// Gate ControlPoints for mechanism/cognition evaluation (hk-karlz).
	// When nil, gate node dispatch returns a structural eval-failure Outcome
	// (status=FAIL) so the cascade routes normally without crashing.
	//
	// Production wires CPRegistry from Config.CPRegistry in newWorkLoopDeps;
	// tests that do not exercise gate dispatch may leave this nil.
	//
	// Spec ref: specs/control-points.md §4.9.CP-043, §4.9.CP-045.
	// Bead ref: hk-karlz.
	cpRegistry core.Registry

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

	// harnessRegistry is the per-agent-type Harness route table (codex-harness
	// C1/T3, hk-hj9ld). The production single-mode dispatch path builds the
	// implementer launch spec via routedLaunchSpecBuilder, which resolves the
	// agent_type (resolveHarness) and looks up the concrete Harness here.
	//
	// CLAUDE-ONLY in T3: only ClaudeHarness is registered, so the default
	// resolution lands on core.AgentTypeClaudeCode and the routed builder
	// delegates to buildClaudeLaunchSpec — byte-identical to the pre-T3 path.
	//
	// May be nil for test fixtures that inject launchSpecBuilder directly (the
	// dispatch path prefers an explicitly-injected launchSpecBuilder and only
	// reaches for harnessRegistry when launchSpecBuilder is nil). newWorkLoopDeps
	// wires a registry with ClaudeHarness registered.
	//
	// Bead ref: hk-hj9ld.
	harnessRegistry *handlercontract.HarnessRegistry

	// substrate is the optional tmux-substrate for handler.Launch.  At MVH this
	// is always nil; handler falls back to exec.CommandContext.  When non-nil it
	// is attached to the LaunchSpec.Substrate field so the handler spawns the
	// subprocess inside a tmux window.
	//
	// Spec ref: specs/process-lifecycle.md §4.7 PL-021b.
	// Bead ref: hk-gql20.14.
	substrate handler.Substrate

	// spawnSubstrateReadyCh, when non-nil, is awaited at the START of runWorkLoop
	// before the first dispatch tick. daemon.Start closes this channel once the
	// spawn substrate has been probed for readiness after a restart-backoff boot
	// (hk-bk33). Nil on normal (no-backoff) boots — the gate is bypassed.
	//
	// Bead ref: hk-bk33.
	spawnSubstrateReadyCh <-chan struct{}

	// agentReadyTimeout is the maximum duration waitAgentReady blocks waiting
	// for an agent_ready event per HC-056.  Zero → defaultAgentReadyTimeout (30s).
	// Sourced from Config.AgentReadyTimeout (also zero-value safe).
	//
	// Spec ref: specs/handler-contract.md §4.9 HC-056.
	// Bead ref: hk-gql20.14.
	agentReadyTimeout time.Duration

	// postAgentReadyHangTimeout is the duration the review-loop's post-agent_ready
	// hang detector waits for any activity after agent_ready before declaring the
	// implementer hung and failing fast (hk-a2okh). Zero → defaultPostAgentReadyHangTimeout
	// (7 min). Only active on the exec path (implWatcher != nil).
	//
	// Bead ref: hk-a2okh.
	postAgentReadyHangTimeout time.Duration

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

	// queueLedger is the queue.BeadLedger seam used by the dispatch loop to
	// re-evaluate deferred-for-ledger-dep items on every tick (queue-model.md
	// §2.8: "when the blocking bead closes, the dispatcher MUST re-evaluate and
	// transition the item back to pending"). Production wires
	// newBRQueueLedger(brAdapter); tests inject a fake. When nil the re-evaluation
	// pass is skipped (queue.ReevaluateDeferred no-ops on a nil ledger), preserving
	// legacy behaviour for callers that do not exercise ledger-dep deferral.
	//
	// Spec ref: specs/queue-model.md §2.8, §6.6 QM-025.
	// Bead ref: hk-nbjht.
	queueLedger queue.BeadLedger

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
	// Dep: hk-m0k0a (persistence) — paused_epoch survives daemon restart once that lands.
	handlerPauseController *HandlerPauseController

	// heldEventDedup tracks (beadID + ":" + epoch) pairs for which a
	// queue_item_held_for_handler_pause event has already been emitted this
	// session, enforcing the at-most-once-per-(bead_id, paused_epoch) contract
	// from event-model.md §8.11.3.
	//
	// Keyed by the string "<beadID>:<pausedEpoch>" (e.g. "hk-abc:2").
	// Only the outer poll loop reads/writes this map — NOT per-bead goroutines.
	// Map is pruned on epoch change (hk-o48pb) so it stays bounded.
	// Access is single-threaded.
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

	// operatorPauseCtrl, when non-nil, is checked at every br-ready dispatch
	// to gate dispatch when the daemon is in an operator-pause state. When nil
	// the gate is disabled (backward-compat for tests that do not exercise
	// operator-pause behaviour). Production wires the daemon-singleton
	// OperatorPauseController.
	//
	// The queue path is already gated via QueueStatusPausedByDrain (set by
	// QueueOperatorEventConsumer on operator_pause_status). This field gates
	// the br-ready fallback path which has no queue-status check.
	//
	// Spec ref: specs/operator-nfr.md §4.3 ON-007–ON-010.
	// Bead ref: hk-ry8q1.
	operatorPauseCtrl *OperatorPauseController

	// decisionBlocker, when non-nil, is checked at every dispatch attempt to
	// gate dispatch for beads blocked by an unacknowledged decision_required
	// event (EV-043).  Populated at startup by LoadDecisionAckState (EV-043a).
	// When nil the gate is disabled (backward-compat for tests that do not
	// exercise decision-blocking behaviour).
	//
	// Spec ref: specs/event-model.md §4.12 EV-043, EV-043a.
	// Bead ref: hk-pbmsq.
	decisionBlocker *DecisionBlocker

	// noAutoPull, when true, disables the br-ready fallback poll path so the
	// work loop only dispatches items that arrive via the queue surface.
	// Sourced from Config.NoAutoPull; see that field's godoc for rationale.
	//
	// Bead ref: hk-exd7m.
	noAutoPull bool

	// skipBrHistoryRotation, when true, disables the pre-close .br_history trim
	// performed by closeBeadWithHistoryTrim before every CloseBead call (hk-hypbi).
	// Mirrors Config.SkipBrHistoryRotation; set to true for tests that operate on
	// temp directories where .beads/.br_history is absent or controlled by fixtures.
	//
	// Bead ref: hk-hypbi.
	skipBrHistoryRotation bool

	// targetBranch is the git branch that completed bead branches are merged
	// into.  Sourced from Config.TargetBranch; normalised to "main" when the
	// config field is empty.  Threaded into lockedMergeRunBranchToMain so the
	// merge sequence targets the configured branch instead of a hard-coded
	// "main" literal.
	//
	// Bead ref: hk-6r6xv.
	targetBranch string

	// protectBranches is the set of branch names the daemon must never merge
	// into.  Sourced from Config.ProtectBranches.  lockedMergeRunBranchToMain
	// fails closed (before any update-ref/push) when targetBranch matches any
	// entry in this set.
	//
	// Bead ref: hk-6r6xv.
	protectBranches []string

	// workerRegistry is the remote-worker registry for the DD1 code-sync path
	// (remote-substrate B8). When non-nil and SelectWorker() returns a worker,
	// beadRunOne inserts fetch-base + remote-worktree + push-branch + box-A-fetch
	// steps around the existing worktree-add and merge. When nil or when no
	// worker is available, the local path is taken (NFR7).
	//
	// Bead ref: hk-rs-b8-codesync-3fk0.
	workerRegistry *workers.Registry

	// beadAuditLogger is retained for the beadExplicitlyReopened predicate and
	// its associated tests (hk-wcv).  The pre-dispatch subsume block that called
	// it was removed by hk-f38n (bare Refs-grep false-positives on partial
	// commits); the function is no longer called in production dispatch paths.
	//
	// Production: wired to (*brcli.Adapter).AuditLog in newWorkLoopDeps.
	beadAuditLogger func(ctx context.Context, id core.BeadID) ([]brcli.AuditEvent, error)

	// scheduleStore, when non-nil, is the daemon-owned recurring-job registry
	// (codename:schedule, hk-0es). The work loop runs runScheduleTick once per
	// poll iteration (after the dispatch-context check, before the capacity gate)
	// to fire any due jobs. When nil the schedule surface is disabled (legacy /
	// unit-test daemons without the surface).
	//
	// Bead ref: hk-0es.
	scheduleStore *schedule.Store

	// crewHandler, when non-nil, is the daemon's crew-start handler. The schedule
	// tick fires spawn-crew actions through HandleCrewStart so subscription-billing
	// guards apply by construction (reuses the same path as `harmonik crew start`).
	// Injected at daemon composition alongside scheduleStore. nil → spawn-crew
	// scheduled actions error out (logged, non-fatal); command actions are unaffected.
	//
	// Bead ref: hk-0es.
	crewHandler crewStarter

	// commsWhoQuerier returns the set of presence-online agent names for the
	// spawn-crew overlap check. Production wires shellCommsWho (shells out to
	// `harmonik comms who --json`); tests inject a double. nil → spawn-crew
	// overlap never blocks (HandleCrewStart's own collision check is the backstop).
	//
	// Bead ref: hk-0es.
	commsWhoQuerier commsWhoQuerier

	// scheduleWakeC, when non-nil, is the channel returned by scheduleStore.WakeCh().
	// The work loop selects on it alongside submitWakeC so a schedule mutation made
	// against the in-memory store wakes the idle loop immediately.
	//
	// Bead ref: hk-0es.
	scheduleWakeC <-chan struct{}

	// coordinatorReapAdapter is the tmux Adapter used by the work-loop periodic
	// coordinator-session reaper (hk-t08m). When nil the periodic reap is disabled
	// (no tmux substrate, or test callers that do not need it).
	//
	// Extracted from cfg.Substrate via substrateWithAdapter at startup; threaded
	// here so the work loop does not touch cfg.Substrate directly.
	//
	// Bead ref: hk-t08m.
	coordinatorReapAdapter tmuxpkg.Adapter

	// coordinatorReapProjectHash is the project hash used to derive the
	// flywheel-coordinator session name for the periodic reaper (hk-t08m).
	// Pre-computed once at startup from cfg.ProjectDir to avoid repeated hashing.
	//
	// Bead ref: hk-t08m.
	coordinatorReapProjectHash core.ProjectHash

	// coordinatorReapInterval is the minimum duration between periodic
	// coordinator-session reap passes. Zero or negative → defaults to
	// periodicCoordinatorReapInterval (5 min). Tests may inject a shorter value
	// (e.g. 0) to exercise the periodic path without real wall-clock delay.
	//
	// Bead ref: hk-t08m.
	coordinatorReapInterval time.Duration

	// lastCoordinatorReap records when the periodic coordinator reaper last ran.
	// Guarded entirely by the work-loop goroutine — no locking needed. Initialised
	// to zero so the first tick always fires.
	//
	// Bead ref: hk-t08m.
	lastCoordinatorReap time.Time

	// lastDiskCheck records when the periodic disk free-space probe last ran.
	// Guarded entirely by the work-loop goroutine — no locking needed. Initialised
	// to zero so the first tick fires after diskCheckInterval elapses; the guard
	// below uses time.Since which returns a large value for a zero time.
	//
	// Bead ref: hk-sxlb.
	lastDiskCheck time.Time

	// lastGoCacheClean records when `go clean -cache` was last run proactively
	// (independently of a disk-low crossing). Guarded by the work-loop goroutine.
	// Zero → first proactive clean fires after goCacheCleanInterval.
	//
	// Bead ref: hk-sxlb.
	lastGoCacheClean time.Time

	// diskLow is true when the most recent disk probe found available space below
	// diskLowWatermarkDefault (or deps.diskLowWatermark). The dispatch loop skips
	// bead claiming while this flag is set, sleeping and retrying each poll tick
	// until disk recovers above the watermark.
	//
	// Bead ref: hk-sxlb.
	diskLow bool

	// diskLowWatermark is the injectable free-space floor for tests. Zero →
	// diskLowWatermarkDefault (10 GiB). Production leaves this zero.
	//
	// Bead ref: hk-sxlb.
	diskLowWatermark uint64

	// diskCheckIntervalOverride overrides diskCheckInterval for tests.
	// Zero → diskCheckInterval (10 min).
	//
	// Bead ref: hk-sxlb.
	diskCheckIntervalOverride time.Duration

	// goCacheCleanIntervalOverride overrides goCacheCleanInterval for tests.
	// Zero → goCacheCleanInterval (60 min).
	//
	// Bead ref: hk-sxlb.
	goCacheCleanIntervalOverride time.Duration

	// diskFreeBytesFunc, when non-nil, replaces the diskFreeBytes call inside
	// runPeriodicDiskCheck.  Tests use this to control the apparent free-space
	// reading without touching the real filesystem.
	//
	// Bead ref: hk-guez.
	diskFreeBytesFunc func(path string) (uint64, error)

	// goCacheCleanFunc, when non-nil, replaces "go clean -cache" execution
	// inside runPeriodicDiskCheck.  Tests use this to capture or stub the
	// reaper without side-effects on the build cache.
	//
	// Bead ref: hk-guez.
	goCacheCleanFunc func() error

	// cacheReapMu, when non-nil, is the reap↔dispatch exclusion lock (hk-y3frr).
	// The cache reaper acquires a Write lock for the ENTIRE duration of
	// `go clean -cache` (up to 5 min); each Register call acquires a Read lock
	// for the duration of the map insert.  This ensures no run can be registered
	// while the reaper is deleting the shared go-build cache, and the reaper
	// cannot start while a dispatch is in progress.
	//
	// Production: always a non-nil *sync.RWMutex (newWorkLoopDeps).
	// Tests may supply their own via WorkLoopDepsParams.CacheReapMu.
	//
	// Bead ref: hk-y3frr.
	cacheReapMu *sync.RWMutex

	// governorState is the mutable state persisted across sentinel governor
	// evaluation cycles (flywheel-motion.md §§1, 6.1; FW2 wire-Evaluate).
	// Nil when no sentinel config is loaded (governor evaluations are no-ops
	// until FW2 adds the Evaluate call). Production wires a non-nil pointer at
	// daemon.Start after newWorkLoopDeps (FW1, hk-y9fn).
	//
	// Bead ref: hk-y9fn (FW1).
	governorState *sentinel.GovernorState

	// governorCfg is the resolved sentinel governor configuration derived from
	// the sentinel: block in .harmonik/config.yaml (flywheel-motion.md §7).
	// Zero value causes sentinel.Evaluate to use compiled defaults.
	// Production populated from digest.LoadSentinelConfig at daemon.Start (FW1, hk-y9fn).
	//
	// Bead ref: hk-y9fn (FW1).
	governorCfg sentinel.Config

	// sentinelMode is the mode from the sentinel: block (flywheel-motion.md §7).
	// "" or "observe" → FW2 observe-only (emit GovernorSignal, no trip, no halt).
	// "act"           → FW3 ACT mode (adds EmitTrip/halt — wired by hk-4toh).
	//
	// Bead ref: hk-z1lr (FW2).
	sentinelMode string

	// sentinelPhase2Classes are the Phase-2 done_definition class names from the
	// sentinel config, used to compute HasUndeployedTail in the governor input.
	// Nil/empty → HasUndeployedTail always false (no br call needed).
	//
	// Bead ref: hk-z1lr (FW2).
	sentinelPhase2Classes []string
}

// closeBeadWithHistoryTrim trims .beads/.br_history to brHistoryCloseTrimKeep
// entries before calling CloseBead, preventing in-session history bloat from
// pushing br close past the 10 s write timeout (hk-hypbi).
//
// Root cause (hk-hypbi): the startup rotation (hk-5dewt) trims to
// brHistoryRotationDefaultKeep (20) entries, but each subsequent br write adds
// a new ~1.2 MB snapshot.  After ~20 dispatches the history reaches 40+ entries
// and br close again takes >10 s, triggering BrUnavailable retry exhaustion and
// leaving the bead stuck in_progress.  Trimming to brHistoryCloseTrimKeep (5)
// before every close caps the scan cost at sub-second latency.
//
// The trim is non-fatal: a failure is logged and the CloseBead proceeds regardless.
// Skipped when skipBrHistoryRotation is true (tests) or projectDir is empty.
//
// On BrUnavailable after retry exhaustion callers SHOULD check
// errors.Is(err, brcli.BrUnavailable) and emit run_completed rather than
// run_failed — the merge already landed; the intent file is retained for BI-031
// recovery on next daemon startup (hk-hypbi).
func (deps *workLoopDeps) closeBeadWithHistoryTrim(
	ctx context.Context,
	runID core.RunID,
	tid core.TransitionID,
	beadID core.BeadID,
	needsAttention bool,
) error {
	if !deps.skipBrHistoryRotation && deps.projectDir != "" {
		_ = runBrHistoryRotationPreflight(ctx, deps.projectDir, brHistoryCloseTrimKeep)
	}
	return deps.brAdapter.CloseBead(ctx, deps.intentLogDir, deps.brTimeoutCfg, runID, tid, beadID, needsAttention)
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
// newLocalRunRegistry creates the run registry owned by the work loop.
// MUST NOT be the shared instance from daemon.go (sharedRunRegistry); using the
// shared registry here would let the pause-policy goroutine snapshot a registry
// that the work loop mutates, causing a silent desync.
func newLocalRunRegistry() *RunRegistry {
	return NewRunRegistry()
}

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
	// Spec ref: specs/execution-model.md §4.11 EM-051 (max_concurrent ≥ 1, default 1, sealed at startup).
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

	// Build the harness route registry (codex-harness C1/T3, hk-hj9ld). The
	// production single-mode dispatch path routes the launchSpecBuilder lookup
	// through this registry. CLAUDE-ONLY: only ClaudeHarness is registered.
	harnessReg, hErr := newHarnessRegistry()
	if hErr != nil {
		return workLoopDeps{}, fmt.Errorf("daemon: newWorkLoopDeps: newHarnessRegistry: %w", hErr)
	}

	// Build the remote-worker registry from cfg.Workers and run the boot-time
	// health check (remote-substrate B4/B6). Returns nil when no worker is
	// enabled so the dispatch path takes the existing local-only branch (NFR7).
	workerReg := buildWorkerRegistry(context.Background(), cfg.Workers, bus)

	// Extract the tmux adapter from cfg.Substrate for the periodic coordinator
	// reaper (hk-t08m). Same extraction pattern as the boot-time sweep above.
	// When no tmux substrate is configured, coordinatorReapAdapter stays nil and
	// the periodic reap is a no-op (safe default).
	var coordinatorReapAdapter tmuxpkg.Adapter
	if sa, ok := cfg.Substrate.(substrateWithAdapter); ok {
		coordinatorReapAdapter = sa.tmuxAdapter()
	}

	return workLoopDeps{
		brAdapter:                  adapter,
		bus:                        bus,
		h:                          h,
		intentLogDir:               intentLogDir,
		projectDir:                 cfg.ProjectDir,
		handlerBinary:              binary,
		daemonBinaryPath:           daemonBinaryPath,
		handlerArgs:                cfg.HandlerArgs,
		handlerEnv:                 handlerEnv,
		brTimeoutCfg:               brcli.TimeoutConfig{},
		tidGen:                     core.NewTransitionIDGenerator(),
		workflowModeDefault:        workflowModeDefault,
		runRegistry:                newLocalRunRegistry(),
		maxConcurrent:              maxConcurrent,
		hookStore:                  store,
		cpRegistry:                 cfg.CPRegistry, // hk-karlz: ControlPoint registry for gate-node dispatch
		adapterRegistry:            registry,
		harnessRegistry:            harnessReg,    // hk-hj9ld: per-agent-type Harness route table (claude-only in T3)
		substrate:                  cfg.Substrate, // nil falls back to exec.CommandContext; set by composition root (hk-kqdpf.4)
		agentReadyTimeout:          cfg.AgentReadyTimeout,
		cancelOnQueueDrain:         cfg.CancelOnQueueDrain,
		projectCfg:                 cfg.ProjectCfg,
		queueStore:                 nil,                       // populated by daemon.Start after wiring QueueStore (hk-45ude)
		queueLedger:                newBRQueueLedger(adapter), // hk-nbjht: re-eval deferred-for-ledger-dep items on every dispatch tick (§2.8)
		staleBlockerCloser:         adapter,                   // hk-rnsjs: auto-close stale blockers on claim failure
		kerfPath:                   cfg.KerfPath,              // hk-9321v: kerf next for EM-062/EM-063 eager-refill
		brPath:                     cfg.BrPath,                // hk-f722: staged-bead generator br create
		followUpLedger:             make(map[string]struct{}), // hk-f722: at-most-once guard per daemon session
		followUpLedgerMu:           &sync.Mutex{},
		followUpLedgerPath:         filepath.Join(cfg.ProjectDir, ".harmonik", followUpLedgerFileName), // hk-3ndb: durable ledger path
		noAutoPull:                 cfg.NoAutoPull,                                                     // hk-exd7m: queue-only mode for flywheel topology
		skipBrHistoryRotation:      cfg.SkipBrHistoryRotation,                                          // hk-hypbi: per-close .br_history trim
		mergeMu:                    &sync.Mutex{},                                                      // hk-yyso7: global merge-serialisation across all queues
		cacheReapMu:                &sync.RWMutex{},                                                    // hk-y3frr: reap↔dispatch exclusion
		emittedEpics:               make(map[core.BeadID]struct{}),                                     // hk-w6y70: at-most-once guard per daemon session
		emittedEpicsMu:             &sync.Mutex{},
		targetBranch:               resolveTargetBranch(cfg.TargetBranch),
		protectBranches:            cfg.ProtectBranches,
		allowedRepos:               cfg.ProjectCfg.Daemon.AllowedRepos,           // hk-xfuc: cross-repo dispatch safelist
		worktreeProvisionFiles:     cfg.ProjectCfg.Daemon.WorktreeProvisionFiles, // hk-z8u: gitignored-but-required files copied into each fresh local worktree
		beadAuditLogger:            adapter.AuditLog,                   // hk-wcv / hk-f38n: retained for tests; pre-dispatch block removed
		workerRegistry:             workerReg,                          // remote-substrate B4/B8: nil → local-only dispatch (NFR7)
		coordinatorReapAdapter:     coordinatorReapAdapter,             // hk-t08m: periodic flywheel-coordinator reaper
		coordinatorReapProjectHash: projectHash,                        // hk-t08m: pre-computed for session name derivation
	}, nil
}

// buildWorkerRegistry turns the loaded workers.Config into a live *workers.Registry
// and runs the boot-time health check (remote-substrate B4/B6).
//
// It returns nil — keeping the dispatch path on the existing local-only branch
// (NFR7) — when no worker is ENABLED. When a worker is enabled it:
//
//  1. Constructs the registry via workers.NewRegistry (B5 selection + slot
//     tracking).
//  2. Runs workers.RunHealthCheck over the worker's transport runner (B6),
//     which probes tmux/claude/git/no-API-key, disables (SetEnabled(false)) any
//     worker that fails a probe, and emits a worker_unhealthy event via bus.Emit.
//     A worker that fails the boot health check is therefore SelectWorker()-skipped
//     so its beads run locally rather than against an unhealthy host.
//
// The runner for the health check is tmux.SSHRunner{Host: worker.Host} for
// transport "ssh" (the only supported transport); other transports run no probes
// and the worker stays enabled as configured.
//
// Bead ref: hk-rs-b4-bootwire-b44z, hk-rs-b6-healthcheck-isda.
func buildWorkerRegistry(ctx context.Context, cfg workers.Config, bus handlercontract.EventEmitter) *workers.Registry {
	return buildWorkerRegistryWithRunner(ctx, cfg, bus, bootHealthRunner(cfg))
}

// buildWorkerRegistryWithRunner is the runner-injectable core of
// buildWorkerRegistry. Production passes the transport-resolved runner from
// bootHealthRunner; tests pass a recording/no-op runner so the boot path is
// exercisable without real ssh.
//
// runner == nil ⇒ the B6 boot health check is skipped (the worker stays enabled
// as configured); this is also the unsupported-transport behaviour.
func buildWorkerRegistryWithRunner(ctx context.Context, cfg workers.Config, bus handlercontract.EventEmitter, runner tmuxpkg.CommandRunner) *workers.Registry {
	if !hasEnabledWorker(cfg) {
		return nil
	}
	reg := workers.NewRegistry(cfg)

	// B6 boot health check: probe each enabled worker over its transport runner.
	// On a probe failure the worker is disabled in-registry and a worker_unhealthy
	// event is emitted, so SelectWorker() skips it and the run falls back to local.
	if runner != nil {
		var emit workers.EmitFunc
		if bus != nil {
			emit = bus.Emit
		}
		_ = workers.RunHealthCheck(ctx, runner, cfg, reg, emit)
	}
	return reg
}

// hasEnabledWorker reports whether cfg has at least one worker with Enabled==true.
func hasEnabledWorker(cfg workers.Config) bool {
	for _, w := range cfg.Workers {
		if w.Enabled {
			return true
		}
	}
	return false
}

// bootHealthRunner resolves the CommandRunner used for the boot health-check
// probes against the (single, v1) enabled worker. Returns an SSHRunner for
// transport "ssh"; nil for any other transport (probes skipped, worker stays
// enabled as configured).
func bootHealthRunner(cfg workers.Config) tmuxpkg.CommandRunner {
	for _, w := range cfg.Workers {
		if !w.Enabled {
			continue
		}
		if w.Transport == "ssh" {
			return tmuxpkg.SSHRunner{Host: w.Host}
		}
		return nil
	}
	return nil
}

// resolveTargetBranch returns branch when non-empty, otherwise the production
// default "main". This mirrors the convention used by the reconciliation
// scanner (daemon.go comment: "defaults to 'main' inside the scanner").
func resolveTargetBranch(branch string) string {
	if branch == "" {
		return "main"
	}
	return branch
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

// queueSelection is the result of selectNextQueue: the snapshot of one eligible
// (queue, active group, first eligible item) chosen by the cross-queue
// round-robin policy under the QueueStore write lock. All fields are local
// copies; the *Queue pointer is NOT retained, so the caller may release the
// lock and re-acquire it for the dispatch stamp (Phase 3) without holding a
// stale reference.
//
// Bead ref: hk-tigaf.4 (NQ-B1).
type queueSelection struct {
	queueName        string
	queueID          string
	groupIndex       int
	itemIdx          int
	itemBeadID       core.BeadID
	itemContext      string
	itemWFMode       string
	itemWFRef        string
	itemTemplateMap  map[string]string
	anyEligible      bool // true if any queue had an active group with eligible items
	anyPausedOrEmpty bool // true if at least one queue existed but contributed nothing
}

// effectiveQueueWorkers resolves the per-queue worker ceiling for q, defaulting
// a zero/absent Workers field to the global cap (QM-066). Mirrors
// queue.DefaultWorkers but lives here so the workloop never imports the global
// cap into the queue package's submit path twice.
//
// Bead ref: hk-tigaf.4 (NQ-B1).
func effectiveQueueWorkers(q *queue.Queue, globalCap int) int {
	return queue.DefaultWorkers(q.Workers, globalCap)
}

// selectNextQueue implements the QM-062/QM-067 two-level capacity gate plus the
// cross-queue round-robin dispatch policy. Called once per dispatch tick while
// holding the QueueStore write lock (via lq). It scans every loaded queue and
// returns the next (queue, active group, first eligible item) to dispatch, or
// (queueSelection{}, false) when no queue can contribute under its own
// per-queue Workers cap and the global ceiling.
//
// Policy (NQ-B1):
//   - A queue is a candidate iff it is QueueStatusActive, has an active group
//     with at least one eligible item, AND its in-flight tally
//     (runRegistry.LenForQueue(name)) is below its effective Workers ceiling.
//   - Candidate queue names are sorted lexicographically (name-ordered), then a
//     daemon-state cursor (*rrCursor, advanced by the CALLER every tick) selects
//     the starting offset. The cursor is NOT reset to 0 each tick: that is what
//     prevents a lexicographically-earlier queue (e.g. "investigate") from
//     perpetually starving a later one (e.g. "main"). This is plain round-robin,
//     explicitly NOT weighted fairness (deferred to v0.2 / N3).
//
// The global ceiling (runRegistry.Len() < globalCap) is enforced by the CALLER
// before invoking selectNextQueue; this function enforces only the per-queue
// cap so the two levels compose to min(group_pending, per_queue_workers -
// queue_running, global_cap - global_running) per QM-062.
//
// Bead ref: hk-tigaf.4 (NQ-B1).
func selectNextQueue(lq *LockedQueueStore, reg *RunRegistry, globalCap, rrCursor int) (queueSelection, bool) {
	names := lq.LockedAllQueueNames()
	if len(names) == 0 {
		return queueSelection{}, false
	}

	// Build the candidate set: queues with eligible work under their own cap.
	candidates := make([]string, 0, len(names))
	sawNonContributing := false
	for _, name := range names {
		q := lq.LockedQueueByName(name)
		if q == nil {
			continue
		}
		if q.Status != queue.QueueStatusActive {
			// Paused-by-failure / paused-by-drain / completed queues contribute
			// nothing but MUST NOT block sibling queues.
			sawNonContributing = true
			continue
		}
		// Per-queue cap: skip when this queue is already at its Workers ceiling.
		if reg.LenForQueue(name) >= effectiveQueueWorkers(q, globalCap) {
			sawNonContributing = true
			continue
		}
		// Must have an active group with at least one eligible item.
		hasEligible := false
		for gi := range q.Groups {
			if q.Groups[gi].Status != queue.GroupStatusActive {
				continue
			}
			if len(queue.EligibleItems(&q.Groups[gi])) > 0 {
				hasEligible = true
			}
			break
		}
		if !hasEligible {
			sawNonContributing = true
			continue
		}
		candidates = append(candidates, name)
	}

	if len(candidates) == 0 {
		return queueSelection{}, false
	}
	sort.Strings(candidates)

	// Round-robin: start at the daemon-state cursor offset (mod candidate count)
	// and pick the first candidate. The caller advances rrCursor every tick so
	// the start offset rotates, guaranteeing no queue starves.
	start := 0
	if n := len(candidates); n > 0 {
		start = ((rrCursor % n) + n) % n // guard against negative cursor
	}
	chosen := candidates[start]

	q := lq.LockedQueueByName(chosen)
	if q == nil { // racing clear — caller retries next tick
		_ = sawNonContributing
		return queueSelection{}, false
	}

	// Locate the chosen queue's active group and its first eligible item.
	for gi := range q.Groups {
		if q.Groups[gi].Status != queue.GroupStatusActive {
			continue
		}
		eligible := queue.EligibleItems(&q.Groups[gi])
		if len(eligible) == 0 {
			break
		}
		head := eligible[0]
		for j := range q.Groups[gi].Items {
			it := &q.Groups[gi].Items[j]
			if it.BeadID == head.BeadID && it.Status == queue.ItemStatusPending {
				return queueSelection{
					queueName:       chosen,
					queueID:         q.QueueID,
					groupIndex:      q.Groups[gi].GroupIndex,
					itemIdx:         j,
					itemBeadID:      it.BeadID,
					itemContext:     it.Context,
					itemWFMode:      it.WorkflowMode,
					itemWFRef:       it.WorkflowRef,
					itemTemplateMap: it.TemplateParams,
					anyEligible:     true,
				}, true
			}
		}
		break
	}
	return queueSelection{anyPausedOrEmpty: sawNonContributing}, false
}

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
	// Spec ref: specs/execution-model.md §4.11 EM-050 (claim-write serialization token-pool of size max_concurrent).
	// Bead ref: hk-e61c3.3.
	claimSem := make(chan struct{}, effectiveMax)

	// Initialise the held-event dedup map (hk-kac8g).  Written only from this
	// goroutine (outer poll loop) — no locking needed.
	if deps.heldEventDedup == nil {
		deps.heldEventDedup = make(map[string]struct{})
	}
	// lastSeenPauseEpoch tracks the most recent pause epoch observed by the
	// dispatcher.  When the epoch advances (pause lifted or new pause window),
	// all prior-epoch dedup entries are stale and pruned (hk-o48pb).
	lastSeenPauseEpoch := 0

	// rrCursor is the cross-queue round-robin cursor (NQ-B1). It is daemon state:
	// declared here and advanced once per successful queue selection across the
	// ENTIRE life of the loop — never reset to 0 each tick. Resetting it would let
	// a lexicographically-earlier queue (e.g. "investigate") win every tick and
	// starve a later one (e.g. "main"); rotating the start offset round-robins
	// dispatch fairly among active queues. Plain round-robin, NOT weighted
	// fairness (QM-067; weighting deferred to v0.2).
	rrCursor := 0

	// readyPathAttempts tracks dispatch attempts for each bead on the br-ready
	// fallback path (no queue). Bounded by maxItemAttempts. Resets on daemon
	// restart (acceptable — the br-ready path is the backward-compat fallback).
	//
	// Bead ref: hk-kupeo (ShowBead bounded retry), hk-6pspu (dispatch bound).
	readyPathAttempts := make(map[core.BeadID]int)

	// sentinelPendingAckToken is the ack_token of the in-flight sentinel trip
	// (ACT mode, FW3 hk-4toh). Empty when no trip is pending. Persists across
	// loop iterations so dormant transitions can clear the correct token.
	var sentinelPendingAckToken string

	// governorHalted is set to true when the G-liveness self-kill gate fires
	// in ACT mode. The loop checks this at the top of each iteration and
	// calls exitClean() — halting dispatch while allowing in-flight runs to drain.
	var governorHalted bool

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

	// exitClean terminates the loop cleanly: it waits for in-flight goroutines
	// (up to shutdownDrainTimeout), kills any orphan tmux windows spawned by this
	// daemon instance (hk-j6npz), then drains any still-active queue to
	// QueueStatusCancelled so the next harmonik run can start without the QM-027
	// "already active" guard blocking it (hk-ppt32). The background context is
	// intentional: by the time exitClean runs, ctx is always cancelled;
	// queue.Persist and KillAllWindows need a live context.
	exitClean := func() error {
		// Wait for in-flight goroutines with a bounded timeout so SIGTERM always
		// exits promptly (hk-vlkh4). Without a bound, goroutines that run
		// ReopenBead(context.Background(), ...) can each block for up to
		// brcli write-timeout + sigtermGrace (~15 s); with N concurrent beads
		// the daemon hangs for N×15 s. The drain timeout caps the total wait.
		// Goroutines that exceed the window leave beads in_progress; QM-002a
		// at next startup resets them to open.
		drainDone := make(chan struct{})
		go func() {
			wg.Wait()
			close(drainDone)
		}()
		select {
		case <-drainDone:
			// All in-flight goroutines drained cleanly.
		case <-time.After(shutdownDrainTimeout):
			remaining := deps.runRegistry.Len()
			fmt.Fprintf(os.Stderr,
				"daemon: workloop: shutdown: drain timeout after %v with %d run(s) still in-flight; exiting (QM-002a recovers on next start)\n",
				shutdownDrainTimeout, remaining)
		}
		// Kill any tmux windows spawned during this run. deps.substrate is nil
		// when tmux hosting is not used (exec.CommandContext path); the type
		// assertion is a no-op in that case.
		if wc, ok := deps.substrate.(windowCleaner); ok {
			_ = wc.KillAllWindows(context.Background())
		}
		drainCancelledQueue(context.Background(), deps)
		return nil
	}

	// hk-bk33: gate post-boot re-dispatch on spawn-substrate readiness.
	// When a restart-backoff was applied and the substrate exposes a readiness
	// probe, daemon.Start starts a goroutine that probes the substrate and closes
	// this channel when done. Waiting here prevents the first dispatch tick from
	// launching a run before the tmux session is ready to accept new windows,
	// avoiding spurious agent_ready_timeout on QM-002a-reverted beads.
	// Nil on normal (no-backoff) boots — the select is skipped entirely.
	if deps.spawnSubstrateReadyCh != nil {
		select {
		case <-deps.spawnSubstrateReadyCh:
		case <-ctx.Done():
			return exitClean()
		}
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

		// G-liveness halt (FW3 hk-4toh): if the governor fired ActivationHalt
		// in ACT mode on a prior tick, drain in-flight runs and exit cleanly.
		// The liveness_halt page event was already emitted when governorHalted
		// was set; here we merely enforce the halt.
		if governorHalted {
			return exitClean()
		}

		// Step 1b: schedule tick — fire any due recurring jobs (codename:schedule,
		// hk-0es). Runs IN-LOOP (reusing this loop's poll cadence + claim-write
		// serialisation), placed after the dispatch-halt check and before the
		// capacity gate so a fired spawn-crew/command action is independent of the
		// bead-dispatch capacity. No-op when scheduleStore is nil.
		runScheduleTick(ctx, deps)

		// Step 1c: periodic coordinator-session reap (hk-t08m).
		//
		// The boot-time sweep (RunOrphanSweep) reaped dead flywheel-coordinator
		// sessions once at startup, but sessions accumulated across hard supervisor
		// crashes that skipped clean shutdown.  Running the same predicate
		// periodically here ensures leaked sessions are cleaned up without requiring
		// a daemon restart.
		//
		// Rate-limited by coordinatorReapInterval (default 5 min) so the tmux
		// adapter is not called on every 2 s poll tick.  The first tick fires
		// immediately (lastCoordinatorReap is zero-valued).  No-op when
		// coordinatorReapAdapter is nil (no tmux substrate).
		{
			interval := deps.coordinatorReapInterval
			if interval <= 0 {
				interval = periodicCoordinatorReapInterval
			}
			if deps.coordinatorReapAdapter != nil && time.Since(deps.lastCoordinatorReap) >= interval {
				runPeriodicCoordinatorReap(ctx, deps.projectDir, deps.coordinatorReapProjectHash, deps.coordinatorReapAdapter, nil)
				deps.lastCoordinatorReap = time.Now()
			}
		}

		// Step 1d: periodic disk watermark check and proactive go-cache reap
		// (hk-sxlb, hk-guez). Two rate-limited sub-steps run from the same block:
		//
		// (A) Disk probe — every diskCheckInterval (default 10 min). When the
		//     probe finds available space below the watermark, deps.diskLow is set
		//     true, a disk_low event is emitted, and `go clean -cache` is run
		//     immediately (reactive reap) — but ONLY when no merge-build is in
		//     flight (runRegistry.Len()==0). If a merge is in flight the reap is
		//     skipped and a loud warning is logged instead (hk-guez fix).
		//     The loop then skips dispatch this iteration (see the gate below).
		//
		// (B) Proactive go-cache reap — every goCacheCleanInterval (default
		//     60 min) even when disk is healthy, preventing the cache from
		//     growing to 20 GiB between low-disk crossings. Also gated on idle
		//     (runRegistry.Len()==0) to avoid racing merge-builds (hk-guez).
		runPeriodicDiskCheck(ctx, &deps)
		if deps.diskLow {
			if sleepErr := workloopSleep(dispatchCtx, workloopPollInterval, deps.submitWakeC); sleepErr != nil {
				return exitClean()
			}
			continue
		}

		// Step 2: capacity gate — if at the concurrent limit, sleep and retry.
		// Read from the controller on every tick when set (hk-ohiaf), so that
		// queue-set-concurrency adjustments take effect without a restart.
		// Raising n lets the gate admit up to n; lowering lets in-flight runs
		// drain naturally and only stops new dispatch once running < n.
		// Spec ref: specs/execution-model.md §4.11 EM-049 (in-flight-run capacity gate).
		gateMax := effectiveMax
		if deps.concurrencyCtrl != nil {
			gateMax = deps.concurrencyCtrl.Get()
		}
		if deps.runRegistry.Len() >= gateMax {
			if sleepErr := workloopSleep(dispatchCtx, workloopPollInterval, deps.submitWakeC); sleepErr != nil {
				return exitClean()
			}
			continue
		}

		// EM-062: eager-refill fires on every poll tick (as well as after every
		// run_terminal event in evaluateGroupAdvanceWithOutcome). This ensures
		// that a deficit opened by a run completion is filled promptly even when
		// the workloop was already idle between terminal events.
		//
		// Spec ref: specs/execution-model.md §4.13 EM-062.
		// Bead ref: hk-9321v.
		eagerRefillEval(ctx, deps)

		// FW2 (hk-z1lr): sentinel governor observe-only evaluation.
		//
		// Fires every tick when the governor is wired (governorState non-nil) and
		// sentinel.mode is "observe" (the default). Emits a governor_signal typed
		// event so operators can watch the staircase decisions against the real
		// event stream before the governor has teeth.
		//
		// OBSERVE-ONLY CONTRACT: no EmitTrip, no halt, no dispatch side-effects.
		// ACT mode (hk-4toh FW3) wires the trip/halt/all-clear block separately.
		//
		// Spec ref: flywheel-motion.md §§1, 6.1. Bead ref: hk-z1lr (FW2).
		if deps.governorState != nil && (deps.sentinelMode == "" || deps.sentinelMode == "observe") {
			now := time.Now()

			// Snapshot hasReadyBeads: ≥1 unblocked open bead exists (§1.3).
			var hasReadyBeads bool
			if readyRecs, readyErr := deps.brAdapter.Ready(ctx); readyErr == nil {
				hasReadyBeads = len(readyRecs) > 0
			}

			// HasUndeployedTail: closed Phase-2-class bead present (§1.3, §5.2).
			// Skip the br call when no Phase-2 classes are configured (fast path).
			var hasUndeployedTail bool
			if len(deps.sentinelPhase2Classes) > 0 && deps.brPath != "" {
				hasUndeployedTail, _ = digest.BuildHasUndeployedTail(ctx, deps.brPath, deps.sentinelPhase2Classes)
			}

			sig := sentinel.Evaluate(ctx, deps.governorState, sentinel.GovernorInput{
				ProjectDir:        deps.projectDir,
				Now:               now,
				HasReadyBeads:     hasReadyBeads,
				HasUndeployedTail: hasUndeployedTail,
			}, deps.governorCfg)

			if raw, mErr := json.Marshal(sig); mErr == nil {
				_ = deps.bus.Emit(ctx, core.EventTypeGovernorSignal, raw)
			}
		}

		// FW3 (hk-4toh): sentinel governor ACT mode.
		//
		// Fires every tick when the governor is wired and sentinel.mode=="act".
		// ACT mode adds real teeth: EmitTrip on sustained-low-movement, ClearTrip
		// on real movement, and a G-liveness halt + page on doom-loop detection.
		// Config default is OFF ("observe"); operators opt in via .harmonik/config.yaml.
		//
		// Trip/clear state is durable: EmitTrip writes an ack-state file to
		// .harmonik/decision_acks/ (EV-043a anchor) AND updates the in-memory
		// DecisionBlocker so IsQueueBlocked("sentinel") gates all dispatch while
		// the trip is pending.
		//
		// Spec ref: flywheel-motion.md §§2, 6.1. Bead ref: hk-4toh (FW3).
		if deps.governorState != nil && deps.sentinelMode == "act" {
			now := time.Now()

			var hasReadyBeads bool
			var readyBeadIDs []string
			if readyRecs, readyErr := deps.brAdapter.Ready(ctx); readyErr == nil {
				hasReadyBeads = len(readyRecs) > 0
				for _, rec := range readyRecs {
					readyBeadIDs = append(readyBeadIDs, string(rec.BeadID))
				}
			}

			var hasUndeployedTail bool
			if len(deps.sentinelPhase2Classes) > 0 && deps.brPath != "" {
				hasUndeployedTail, _ = digest.BuildHasUndeployedTail(ctx, deps.brPath, deps.sentinelPhase2Classes)
			}

			sig := sentinel.Evaluate(ctx, deps.governorState, sentinel.GovernorInput{
				ProjectDir:        deps.projectDir,
				Now:               now,
				HasReadyBeads:     hasReadyBeads,
				HasUndeployedTail: hasUndeployedTail,
			}, deps.governorCfg)

			if raw, mErr := json.Marshal(sig); mErr == nil {
				_ = deps.bus.Emit(ctx, core.EventTypeGovernorSignal, raw)
			}

			switch {
			case sig.Level == sentinel.ActivationHalt:
				// G-liveness doom-loop: halt dispatch + emit liveness_halt page event.
				// Set governorHalted=true so the next iteration calls exitClean().
				governorHalted = true
				halPayload, _ := json.Marshal(map[string]interface{}{
					"consecutive_zero_cycles": sig.ConsecutiveZeroCycles,
					"liveness_no_progress_n":  deps.governorCfg.LivenessNoProgressN,
				})
				_ = deps.bus.Emit(ctx, core.EventTypeLivenessHalt, halPayload)
				fmt.Fprintf(os.Stderr,
					"daemon: workloop: sentinel: G-liveness halt fired after %d zero-progress cycles (threshold=%d); halting dispatch\n",
					sig.ConsecutiveZeroCycles, deps.governorCfg.LivenessNoProgressN)

			case sig.Level == sentinel.ActivationActive && sig.SuppressedBy == "":
				// Sustained low movement with opportunity: emit decision_required trip.
				//
				// AC4 (hk-jvul): reconcile the in-memory pending token against the
				// on-disk ack file. A captain legitimate-halt (record-halt CLI) may
				// have externally acknowledged the file between ticks. When that
				// happens, clear the in-memory token and the DecisionBlocker so the
				// next ActivationActive emits a fresh trip for re-adjudication (spec
				// §2.2 clause 2 — "subject to re-adjudication next pass").
				if sentinelPendingAckToken != "" {
					if externallyAcked, checkErr := sentinel.IsTripAcknowledged(deps.projectDir, sentinelPendingAckToken); checkErr == nil && externallyAcked {
						if deps.decisionBlocker != nil {
							deps.decisionBlocker.Acknowledge(decisionAckSubjectKindQueue, sentinelSubjectIDACT, sentinelPendingAckToken)
						}
						sentinelPendingAckToken = ""
					}
				}
				// Idempotent: EmitTrip returns the existing ack_token if one is pending.
				if sentinelPendingAckToken == "" {
					tok, tripErr := sentinel.EmitTrip(ctx, sentinel.TripInput{
						ProjectDir:        deps.projectDir,
						ReadyBeadIDs:      readyBeadIDs,
						HasUndeployedTail: hasUndeployedTail,
						Now:               now,
					})
					if tripErr != nil {
						fmt.Fprintf(os.Stderr,
							"daemon: workloop: sentinel: EmitTrip failed (non-fatal): %v\n", tripErr)
					} else if tok != "" {
						sentinelPendingAckToken = tok
						if deps.decisionBlocker != nil {
							deps.decisionBlocker.AddQueueBlock(sentinelSubjectIDACT, tok)
						}
					}
				}

				// FW4 (hk-jsvc): spawn a fresh-context adversary crew to adjudicate
				// the trip. The adversary reviews captain comms/commits as a foreign
				// artifact and emits sentinel emit-trip if it confirms the governor's
				// verdict. Overlap-skip: SpawnAdversary is a no-op when the adversary
				// crew is already online (prevents stacked sessions on consecutive trips).
				if deps.crewHandler != nil {
					var onlineAgents map[string]struct{}
					if deps.commsWhoQuerier != nil {
						if agents, whoErr := deps.commsWhoQuerier(ctx); whoErr == nil {
							onlineAgents = agents
						}
					}
					if onlineAgents == nil {
						onlineAgents = map[string]struct{}{}
					}
					if _, spawnErr := sentinel.SpawnAdversary(ctx, sentinel.AdversaryInput{
						ProjectDir: deps.projectDir,
					}, deps.crewHandler, onlineAgents); spawnErr != nil {
						fmt.Fprintf(os.Stderr,
							"daemon: workloop: sentinel: SpawnAdversary failed (non-fatal): %v\n", spawnErr)
					}
				}

			case sig.Level == sentinel.ActivationDormant && sentinelPendingAckToken != "":
				// Real movement detected: clear the pending trip automatically.
				// ClearTrip writes decision_acknowledged to events.jsonl + updates the
				// ack file. Only governor movement (bead_closed / run_completed /
				// HEAD-advance) can clear the trip — not the captain's say-so alone.
				//
				// AC4 (hk-jvul): if the ack was already acknowledged externally (e.g.
				// RecordLegitimateHalt), skip ClearTrip to avoid a spurious
				// governor_movement event on top of the existing legitimate_halt clear.
				// Always clear in-memory state and DecisionBlocker so dispatch resumes.
				alreadyAcked, _ := sentinel.IsTripAcknowledged(deps.projectDir, sentinelPendingAckToken)
				if !alreadyAcked {
					if clearErr := sentinel.ClearTrip(ctx, deps.projectDir, sentinelPendingAckToken, now); clearErr != nil {
						fmt.Fprintf(os.Stderr,
							"daemon: workloop: sentinel: ClearTrip failed (non-fatal): %v\n", clearErr)
						break // preserve sentinelPendingAckToken for retry next tick
					}
				}
				if deps.decisionBlocker != nil {
					deps.decisionBlocker.Acknowledge(decisionAckSubjectKindQueue, sentinelSubjectIDACT, sentinelPendingAckToken)
				}
				sentinelPendingAckToken = ""
			}
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
			beadRecord                 core.BeadRecord
			queueItemIndex             int    // item index within the group (-1 = no queue)
			capturedQueueName          string // NQ-B1: name of the dispatching queue ("" = br-ready)
			queueIDField               *string
			queueGroupIdxFd            *int
			capturedExtraContext       string            // hk-boiwe: per-item context from queue.Item.Context
			capturedItemWFMode         string            // hk-hiqrl: per-item workflow mode from queue.Item.WorkflowMode
			capturedItemWFRef          string            // hk-qo9pq: per-item workflow ref from queue.Item.WorkflowRef
			capturedItemTemplateParams map[string]string // hk-55zv2 / WG-045: template params from queue.Item.TemplateParams
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
				snapItemIdx            int = -1 // -1 → no item found (no queue can contribute)
				snapItemBeadID         core.BeadID
				snapItemContext        string
				snapItemWFMode         string
				snapItemWFRef          string
				snapItemTemplateParams map[string]string
				snapGroupIndex         int
				snapQueueID            string
				snapQueueName          string
			)
			{
				// Two-level capacity gate + cross-queue round-robin (NQ-B1).
				//
				// Prior to named queues this block read the single "main" queue via
				// lq.Queue(). It now scans EVERY loaded queue: it bootstraps each
				// queue's first pending group (hk-veoht), re-evaluates deferred items
				// per active group (hk-nbjht), then selectNextQueue picks the next
				// (queue, group, item) honouring each queue's per-queue Workers cap and
				// the name-ordered round-robin cursor. The global ceiling was already
				// checked at Step 2; selectNextQueue enforces only the per-queue cap so
				// the two compose per QM-062.
				//
				// Spec ref: specs/queue-model.md §9.3 QM-062, §9.7 QM-066, §9.8 QM-067.
				lq := deps.queueStore.LockForMutation()

				// Bootstrap any queue whose first group is still pending. A
				// freshly-submitted/loaded queue persists group 0 as pending and nothing
				// else advances it; activate it inline (under the held write lock) so
				// this same tick can dispatch its items.
				bootstrapped := false
				var bootstrapEvents []core.Event
				for _, name := range lq.LockedAllQueueNames() {
					q := lq.LockedQueueByName(name)
					if q == nil || q.Status != queue.QueueStatusActive {
						continue
					}
					hasActiveGroup := false
					for i := range q.Groups {
						if q.Groups[i].Status == queue.GroupStatusActive {
							hasActiveGroup = true
							break
						}
					}
					if hasActiveGroup {
						continue
					}
					if ok, evts := activateFirstPendingGroupLocked(ctx, deps, lq, q); ok {
						bootstrapped = true
						bootstrapEvents = append(bootstrapEvents, evts...)
					}
				}
				if bootstrapped {
					// A pending group became active — emit started events after lock
					// release, then re-evaluate from the top so the next pass sees the
					// active group and dispatches its items.
					lq.Done()
					for _, evt := range bootstrapEvents {
						raw, mErr := json.Marshal(evt.Payload)
						if mErr != nil {
							raw = evt.Payload
						}
						_ = deps.bus.Emit(ctx, core.EventType(evt.Type), raw)
					}
					continue
				}

				// §2.8 deferred-item re-evaluation across every active queue's active
				// group: transition any deferred-for-ledger-dep item whose blockers all
				// resolved back to pending (hk-nbjht). Mutates groups in place under the
				// write lock; persists the owning queue when any flip occurred.
				for _, name := range lq.LockedAllQueueNames() {
					q := lq.LockedQueueByName(name)
					if q == nil || q.Status != queue.QueueStatusActive {
						continue
					}
					for gi := range q.Groups {
						if q.Groups[gi].Status != queue.GroupStatusActive {
							continue
						}
						if undeferred, reErr := queue.ReevaluateDeferred(ctx, &q.Groups[gi], deps.queueLedger); reErr != nil {
							fmt.Fprintf(os.Stderr, "daemon: workloop: ReevaluateDeferred queueID=%s groupIndex=%d: %v\n",
								q.QueueID, q.Groups[gi].GroupIndex, reErr)
						} else if len(undeferred) > 0 {
							if persistErr := queue.Persist(ctx, deps.projectDir, q); persistErr != nil {
								fmt.Fprintf(os.Stderr, "daemon: workloop: Persist after ReevaluateDeferred queueID=%s: %v\n",
									q.QueueID, persistErr)
							}
						}
						break // only the first active group per queue
					}
				}

				// Round-robin selection across all queues honouring per-queue Workers
				// caps. rrCursor is daemon state (declared before the loop) advanced on
				// every successful selection so the start offset rotates — this is what
				// prevents a lexicographically-earlier queue from starving a later one.
				//
				// Asymmetry: we pass effectiveMax (static startup value) rather than gateMax
				// (bandwidth-tuner runtime value) for the per-queue Workers ceiling.  This is
				// intentional: the global gate at Step 2 already blocks dispatch when the tuner
				// reduces gateMax below deps.runRegistry.Len(), so per-queue candidates are never
				// actually dispatched while the global ceiling is throttled.  Per-queue Workers
				// reflects the queue-owner's permanent concurrency intent, not the current tuner
				// state; scaling it with the tuner would under-count eligible queues in the
				// round-robin even when the global gate is the binding constraint.
				sel, ok := selectNextQueue(lq, deps.runRegistry, effectiveMax, rrCursor)
				// Capture queue count while the lock is still held so we can
				// distinguish "zero queues loaded" from "queues exist but all
				// paused/at-cap" after lq.Done() releases the lock (hk-mgoo7).
				loadedQueueCount := len(lq.LockedAllQueueNames())
				lq.Done()
				if !ok {
					if loadedQueueCount > 0 {
						// Queues are loaded but none can contribute right now (all
						// paused, drained, or at their per-queue cap). Block until a
						// queue-submit wake signal or shutdown.
						//
						// hk-0es: when a schedule with enabled jobs is loaded the daemon
						// must NOT block indefinitely — it has to re-tick to fire due jobs.
						// scheduleAwareIdleWait bounds the wait by the poll interval in that
						// case so runScheduleTick re-runs at sub-minute latency, while
						// degrading to the indefinite block when no schedule is armed.
						if sleepErr := scheduleAwareIdleWait(dispatchCtx, deps); sleepErr != nil {
							return exitClean()
						}
						continue
					}
					// Zero queues loaded — skip snap assignments so snapItemIdx stays
					// at its -1 sentinel and the br-ready fallback path below handles
					// dispatch. This restores --auto-pull and smoke-test behaviour
					// broken by the NQ-B1 refactor (a027808d).
				} else {
					// Advance the round-robin cursor EVERY time we pick a queue so the
					// next tick starts at the next name (no reset-to-0 → no starvation).
					rrCursor++

					snapItemIdx = sel.itemIdx
					snapItemBeadID = sel.itemBeadID
					snapItemContext = sel.itemContext
					snapItemWFMode = sel.itemWFMode
					snapItemWFRef = sel.itemWFRef
					snapItemTemplateParams = sel.itemTemplateMap
					snapGroupIndex = sel.groupIndex
					snapQueueID = sel.queueID
					snapQueueName = sel.queueName
				}
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
					lastSeenPauseEpoch = pruneHeldDedupOnEpochChange(&deps, epoch, lastSeenPauseEpoch)
					if isPaused {
						emitHeldEvent(ctx, deps, snapItemBeadID, core.AgentTypeClaudeCode, epoch)
						if sleepErr := workloopSleep(dispatchCtx, workloopPollInterval, deps.submitWakeC); sleepErr != nil {
							return exitClean()
						}
						continue
					}
				}

				// Decision-required dispatch-blocking gate (EV-043, queue path):
				// if the bead has an unacknowledged decision_required pending,
				// hold it without claiming and retry on the next poll tick.
				//
				// Sentinel queue-level gate (FW3 hk-4toh): also hold when the
				// sentinel governor trip is pending (IsQueueBlocked("sentinel")),
				// which blocks ALL beads — not just a specific one — until real
				// movement clears the trip.
				//
				// Spec ref: specs/event-model.md §4.12 EV-043, EV-043a.
				// Bead ref: hk-pbmsq (bead gate), hk-4toh (sentinel queue gate).
				if deps.decisionBlocker != nil && deps.decisionBlocker.IsBeadBlocked(snapItemBeadID) {
					fmt.Fprintf(os.Stderr,
						"daemon: workloop: bead %s blocked by unacknowledged decision_required (EV-043) — holding\n",
						snapItemBeadID)
					if sleepErr := workloopSleep(dispatchCtx, workloopPollInterval, deps.submitWakeC); sleepErr != nil {
						return exitClean()
					}
					continue
				}
				if deps.decisionBlocker != nil && deps.decisionBlocker.IsQueueBlocked(sentinelSubjectIDACT) {
					fmt.Fprintf(os.Stderr,
						"daemon: workloop: bead %s blocked by sentinel governor trip (EV-043, FW3) — holding until real movement\n",
						snapItemBeadID)
					if sleepErr := workloopSleep(dispatchCtx, workloopPollInterval, deps.submitWakeC); sleepErr != nil {
						return exitClean()
					}
					continue
				}

				// Pre-claim status guard for queue-path (BI-013c): between the
				// dispatcher's selection of a queue item and the claim write to Beads,
				// re-read the bead's status via br show and confirm status = open.
				//
				// If the re-read returns a non-open status, skip the claim, emit
				// bead_claim_skipped, and return the item to its group with status
				// deferred-for-ledger-dep per queue-model.md §6 QM-022.
				//
				//   blocked (hk-n91y0): deps-blocked beads fall through to Phase 3
				//     and ClaimBead, where the dedicated guard handles them.
				//
				// hk-lr5t: preClaimRecord is declared outside the anonymous block so
				// its labels/title/description are available at beadRecord construction
				// below (line ~1658). This avoids a second ShowBead round-trip for the
				// most common case; the post-claim ShowBead at ~line 1954 refreshes if
				// anything changed between the pre-claim read and the claim write.
				var preClaimRecord core.BeadRecord
				{
					rec, preClaimErr := deps.brAdapter.ShowBead(ctx, snapItemBeadID)
					if preClaimErr != nil {
						if dispatchCtx.Err() != nil {
							return exitClean()
						}
						fmt.Fprintf(os.Stderr, "daemon: workloop: ShowBead pre-claim (queue-path) %s error (will retry): %v\n", snapItemBeadID, preClaimErr)
						if sleepErr := workloopSleep(dispatchCtx, workloopPollInterval, deps.submitWakeC); sleepErr != nil {
							return exitClean()
						}
						continue
					}
					preClaimRecord = rec
					if preClaimRecord.Status != core.CoarseStatusOpen && preClaimRecord.Status != core.CoarseStatusBlocked {
						// BI-013c: non-open status observed — skip claim, emit bead_claim_skipped.
						fmt.Fprintf(os.Stderr,
							"daemon: workloop: bead_claim_skipped %s observed_status=%s reason=status_changed_between_select_and_claim (BI-013c)\n",
							snapItemBeadID, preClaimRecord.Status)
						skipPayload := core.BeadClaimSkippedPayload{
							BeadID:         string(snapItemBeadID),
							ObservedStatus: string(preClaimRecord.Status),
							Reason:         "status_changed_between_select_and_claim",
							DetectedAt:     time.Now().UTC().Format(time.RFC3339),
						}
						if raw, mErr := json.Marshal(skipPayload); mErr == nil {
							_ = deps.bus.Emit(ctx, core.EventTypeBeadClaimSkipped, raw)
						}
						// BI-013c terminal path: closed/tombstone beads are done — fail the
						// queue item directly via evaluateGroupAdvanceWithOutcome so the group
						// can reach allItemsTerminal. Non-terminal statuses (in_progress, draft,
						// deferred, pinned) remain deferred-for-ledger-dep to be re-evaluated
						// on the next poll cycle (hk-3kq05).
						if preClaimRecord.Status == core.CoarseStatusClosed ||
							preClaimRecord.Status == core.CoarseStatusTombstone {
							evaluateGroupAdvanceWithOutcome(ctx, deps, snapQueueName, snapQueueID, snapGroupIndex, snapItemIdx, false)
						} else {
							// Set the queue item to deferred-for-ledger-dep under the write lock.
							if deps.queueStore != nil {
								lq := deps.queueStore.LockForMutation()
								liveQ := lq.LockedQueueByName(snapQueueName)
								if liveQ != nil {
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
											liveQ.Groups[gi].Items[snapItemIdx].Status = queue.ItemStatusDeferredForLedgerDep
										}
									}
									lq.LockedSetQueueByName(snapQueueName, liveQ)
									if persistErr := queue.Persist(ctx, deps.projectDir, liveQ); persistErr != nil {
										fmt.Fprintf(os.Stderr, "daemon: workloop: Persist bead_claim_skipped deferred-for-ledger-dep queueID=%s: %v\n",
											liveQ.QueueID, persistErr)
									}
								}
								lq.Done()
							}
						}
						if sleepErr := workloopSleep(dispatchCtx, workloopPollInterval, deps.submitWakeC); sleepErr != nil {
							return exitClean()
						}
						continue
					}
				}

				// Greenlight gate (AC2 — hk-lacr, queue path): staged deploy+verify beads
				// carry "needs-greenlight" and MUST NOT be dispatched until a captain clears
				// the label via `harmonik greenlight <bead-id>`. This check is independent of
				// --no-auto-pull because it reads the live bead label, not a daemon mode flag.
				// The br-ready path is gated at adapter read time (brcli/ready.go).
				{
					greenlightHeld := false
					for _, lbl := range preClaimRecord.Labels {
						if lbl == labelNeedsGreenlight {
							greenlightHeld = true
							break
						}
					}
					if greenlightHeld {
						fmt.Fprintf(os.Stderr,
							"daemon: workloop: bead %s has needs-greenlight label — holding until captain runs `harmonik greenlight %s`\n",
							snapItemBeadID, snapItemBeadID)
						if sleepErr := workloopSleep(dispatchCtx, workloopPollInterval, deps.submitWakeC); sleepErr != nil {
							return exitClean()
						}
						continue
					}
				}

				// Phase 3 — stamp item as dispatched under the write lock (TOCTOU).
				// NQ-B1: operate on the SELECTED queue (snapQueueName), not the "main"
				// slot, so the dispatch stamp lands on the queue the round-robin chose.
				{
					lq := deps.queueStore.LockForMutation()
					liveQ := lq.LockedQueueByName(snapQueueName)
					if liveQ == nil {
						lq.Done()
						if sleepErr := workloopSleep(dispatchCtx, workloopPollInterval, deps.submitWakeC); sleepErr != nil {
							return exitClean()
						}
						continue
					}
					// Cross-queue bead dedup guard (hk-a11re): under the write lock
					// check every OTHER active queue for an in-flight item carrying the
					// same bead_id. If found, the bead is already being executed from
					// another queue — fail this item immediately to prevent two concurrent
					// implementers. The check must happen while the lock is held so that
					// the "dispatched" stamp in the winning queue is visible here; no race
					// is possible between the two queues' Phase 3 blocks because LockForMutation
					// serializes them.
					{
						var crossQueueConflict string
						for _, otherName := range lq.LockedAllQueueNames() {
							if otherName == snapQueueName {
								continue
							}
							otherQ := lq.LockedQueueByName(otherName)
							if otherQ == nil || otherQ.Status != queue.QueueStatusActive {
								continue
							}
							for _, g := range otherQ.Groups {
								for _, item := range g.Items {
									if item.BeadID == snapItemBeadID &&
										(item.Status == queue.ItemStatusDispatched || item.Status == queue.ItemStatusCompleted) {
										crossQueueConflict = otherName
										break
									}
								}
								if crossQueueConflict != "" {
									break
								}
							}
							if crossQueueConflict != "" {
								break
							}
						}
						if crossQueueConflict != "" {
							// Fail the duplicate item so the group can advance rather than stall.
							for gi := range liveQ.Groups {
								if liveQ.Groups[gi].Status != queue.GroupStatusActive {
									continue
								}
								if liveQ.Groups[gi].GroupIndex != snapGroupIndex {
									continue
								}
								if snapItemIdx < len(liveQ.Groups[gi].Items) &&
									liveQ.Groups[gi].Items[snapItemIdx].BeadID == snapItemBeadID {
									liveQ.Groups[gi].Items[snapItemIdx].Status = queue.ItemStatusFailed
									liveQ.Groups[gi].Items[snapItemIdx].LastFailureReason = "cross_queue_duplicate"
								}
							}
							lq.LockedSetQueueByName(snapQueueName, liveQ)
							if persistErr := queue.Persist(ctx, deps.projectDir, liveQ); persistErr != nil {
								fmt.Fprintf(os.Stderr, "daemon: workloop: Persist cross-queue-duplicate queueID=%s: %v\n",
									liveQ.QueueID, persistErr)
							}
							lq.Done()
							fmt.Fprintf(os.Stderr, "daemon: workloop: bead %s already dispatched/completed from queue %q — failing cross-queue duplicate item (hk-a11re, hk-dorz9)\n",
								snapItemBeadID, crossQueueConflict)
							evaluateGroupAdvanceWithOutcome(ctx, deps, snapQueueName, snapQueueID, snapGroupIndex, snapItemIdx, false)
							continue
						}
					}

					// Locate the same group and item in the live snapshot.
					foundItem := false
					maxAttemptsHit := false
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
							// hk-6pspu: increment Attempts and enforce maxItemAttempts.
							liveQ.Groups[gi].Items[snapItemIdx].Attempts++
							if liveQ.Groups[gi].Items[snapItemIdx].Attempts >= maxItemAttempts {
								liveQ.Groups[gi].Items[snapItemIdx].LastFailureReason = "max_attempts_exceeded"
								fmt.Fprintf(os.Stderr, "daemon: workloop: bead %s exceeded maxItemAttempts=%d — failing queue item (hk-6pspu)\n",
									snapItemBeadID, maxItemAttempts)
								maxAttemptsHit = true
								break
							}
							runUUIDStr := "" // filled after uuid generation below
							liveQ.Groups[gi].Items[snapItemIdx].Status = queue.ItemStatusDispatched
							liveQ.Groups[gi].Items[snapItemIdx].RunID = &runUUIDStr // placeholder; updated after
							_ = runUUIDStr                                          // suppress lint
							foundItem = true
						}
					}
					if maxAttemptsHit {
						lq.LockedSetQueueByName(snapQueueName, liveQ)
						if persistErr := queue.Persist(ctx, deps.projectDir, liveQ); persistErr != nil {
							fmt.Fprintf(os.Stderr, "daemon: workloop: Persist max-attempts queueID=%s: %v\n",
								liveQ.QueueID, persistErr)
						}
						lq.Done()
						evaluateGroupAdvanceWithOutcome(ctx, deps, snapQueueName, snapQueueID, snapGroupIndex, snapItemIdx, false)
						continue
					}
					if !foundItem {
						lq.Done()
						// Already dispatched by a concurrent path — retry.
						if sleepErr := workloopSleep(dispatchCtx, workloopPollInterval, deps.submitWakeC); sleepErr != nil {
							return exitClean()
						}
						continue
					}
					lq.LockedSetQueueByName(snapQueueName, liveQ)
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

				// hk-lr5t: initialize beadRecord with the pre-claim ShowBead result so
				// labels (harness:<agent-type>, workflow:<mode>, model:<alias>, etc.)
				// are available to resolveHarness and resolveWorkflowMode even when
				// the post-claim ShowBead below fails. The post-claim ShowBead at
				// ~line 1954 refreshes these fields after claim and remains the
				// authoritative source; this initialization closes the label-load gap
				// where a post-claim ShowBead failure left Labels=nil, causing
				// resolveHarness to fall through to the claude-code built-in fallback
				// despite a harness:codex label on the bead (root cause of hk-lr5t).
				beadRecord = core.BeadRecord{
					BeadID:      snapItemBeadID,
					Labels:      preClaimRecord.Labels,
					Title:       preClaimRecord.Title,
					Description: preClaimRecord.Description,
				}
				queueItemIndex = snapItemIdx
				capturedQueueName = snapQueueName // NQ-B1: tag the run with its queue
				qID := snapQueueID
				gIdx := snapGroupIndex
				queueIDField = &qID
				queueGroupIdxFd = &gIdx
				capturedExtraContext = snapItemContext              // hk-boiwe
				capturedItemWFMode = snapItemWFMode                 // hk-hiqrl
				capturedItemWFRef = snapItemWFRef                   // hk-qo9pq
				capturedItemTemplateParams = snapItemTemplateParams // hk-55zv2 / WG-045
			}
		}

		var beadID core.BeadID
		if queueItemIndex < 0 {
			// No-auto-pull gate (hk-exd7m): when --no-auto-pull is set, suppress the
			// br-ready fallback path entirely so the daemon only dispatches work that
			// arrives via the queue surface (harmonik queue submit / append).  This is
			// the queue-only mode required by the flywheel topology (CL-013/070/071)
			// where a Pi cognition loop curates dispatch timing.
			if deps.noAutoPull {
				if sleepErr := workloopSleep(dispatchCtx, workloopPollInterval, deps.submitWakeC); sleepErr != nil {
					return exitClean()
				}
				continue
			}

			// Operator-pause gate for the br-ready path (hk-ry8q1): when the daemon
			// is operator-paused, hold dispatch without claiming any bead. The queue
			// path is already gated via QueueStatusPausedByDrain.
			//
			// Spec ref: specs/operator-nfr.md §4.3 ON-007–ON-010.
			if deps.operatorPauseCtrl != nil && deps.operatorPauseCtrl.IsPaused() {
				if sleepErr := workloopSleep(dispatchCtx, workloopPollInterval, deps.submitWakeC); sleepErr != nil {
					return exitClean()
				}
				continue
			}

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

			// hk-6pspu: br-ready path attempt bound. Skip beads that have
			// exceeded maxItemAttempts on this path. The counter is incremented
			// on ShowBead/claim failures (not on every poll). Resets on daemon
			// restart (acceptable for the backward-compat fallback path).
			if readyPathAttempts[beadRecord.BeadID] >= maxItemAttempts {
				fmt.Fprintf(os.Stderr, "daemon: workloop: bead %s exceeded maxItemAttempts=%d on br-ready path — skipping (hk-6pspu)\n",
					beadRecord.BeadID, maxItemAttempts)
				if sleepErr := workloopSleep(dispatchCtx, workloopPollInterval, deps.submitWakeC); sleepErr != nil {
					return exitClean()
				}
				continue
			}

			// Handler-pause gate for the br-ready path (hk-kac8g): mirror the same
			// check applied in the queue path above.  The bead remains in the br
			// ready queue (not claimed) while the handler is paused.
			//
			// Bead ref: hk-kac8g.
			if deps.handlerPauseController != nil {
				epoch, isPaused := deps.handlerPauseController.PausedEpochFor(core.AgentTypeClaudeCode)
				lastSeenPauseEpoch = pruneHeldDedupOnEpochChange(&deps, epoch, lastSeenPauseEpoch)
				if isPaused {
					emitHeldEvent(ctx, deps, beadRecord.BeadID, core.AgentTypeClaudeCode, epoch)
					if sleepErr := workloopSleep(dispatchCtx, workloopPollInterval, deps.submitWakeC); sleepErr != nil {
						return exitClean()
					}
					continue
				}
			}

			// Decision-required dispatch-blocking gate (EV-043, br-ready path):
			// mirror the check applied in the queue path above.
			//
			// Sentinel queue-level gate (FW3 hk-4toh): also hold when the sentinel
			// governor trip is pending — all beads are blocked until real movement.
			//
			// Spec ref: specs/event-model.md §4.12 EV-043, EV-043a.
			// Bead ref: hk-pbmsq (bead gate), hk-4toh (sentinel queue gate).
			if deps.decisionBlocker != nil && deps.decisionBlocker.IsBeadBlocked(beadRecord.BeadID) {
				fmt.Fprintf(os.Stderr,
					"daemon: workloop: bead %s blocked by unacknowledged decision_required (EV-043, br-ready path) — holding\n",
					beadRecord.BeadID)
				if sleepErr := workloopSleep(dispatchCtx, workloopPollInterval, deps.submitWakeC); sleepErr != nil {
					return exitClean()
				}
				continue
			}
			if deps.decisionBlocker != nil && deps.decisionBlocker.IsQueueBlocked(sentinelSubjectIDACT) {
				fmt.Fprintf(os.Stderr,
					"daemon: workloop: bead %s blocked by sentinel governor trip (EV-043, FW3, br-ready path) — holding until real movement\n",
					beadRecord.BeadID)
				if sleepErr := workloopSleep(dispatchCtx, workloopPollInterval, deps.submitWakeC); sleepErr != nil {
					return exitClean()
				}
				continue
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
		// NQ-B1: target the selected queue by name (capturedQueueName), not "main".
		if queueItemIndex >= 0 && deps.queueStore != nil {
			lq := deps.queueStore.LockForMutation()
			liveQ := lq.LockedQueueByName(capturedQueueName)
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
				lq.LockedSetQueueByName(capturedQueueName, liveQ)
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
				readyPathAttempts[beadID]++
				if readyPathAttempts[beadID] >= maxItemAttempts {
					fmt.Fprintf(os.Stderr, "daemon: workloop: ShowBead pre-claim check %s failed %d times, skipping bead (hk-kupeo): %v\n",
						beadID, readyPathAttempts[beadID], showErr)
					if sleepErr := workloopSleep(dispatchCtx, workloopPollInterval, deps.submitWakeC); sleepErr != nil {
						return exitClean()
					}
					continue
				}
				fmt.Fprintf(os.Stderr, "daemon: workloop: ShowBead pre-claim check %s error (attempt %d/%d, will retry): %v\n",
					beadID, readyPathAttempts[beadID], maxItemAttempts, showErr)
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
		// Spec ref: specs/execution-model.md §4.11 EM-050 (acquire token before ClaimBead, release after).
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
			// When ClaimBead fails because the bead has open dependencies, reverting
			// to pending and retrying creates a live-lock that starves the remaining
			// pending items in the wave group.
			//
			// Detection: check both the error message from br claim (which includes
			// "cannot claim blocked issue" when deps are open) AND the ShowBead
			// status. A bead can be status=open but still unclaimable due to deps.
			if queueItemIndex >= 0 && deps.queueStore != nil && queueIDField != nil && queueGroupIdxFd != nil {
				claimErrStr := claimErr.Error()
				isBlocked := strings.Contains(claimErrStr, "cannot claim blocked issue") ||
					strings.Contains(claimErrStr, "blocked")
				if !isBlocked {
					if showRecord, showErr := deps.brAdapter.ShowBead(ctx, beadID); showErr == nil &&
						showRecord.Status == core.CoarseStatusBlocked {
						isBlocked = true
					}
				}
				if isBlocked {
					fmt.Fprintf(os.Stderr, "daemon: workloop: ClaimBead %s bead is blocked (deps or status) — failing queue item (hk-n91y0)\n", beadID)
					evaluateGroupAdvanceWithOutcome(ctx, deps, capturedQueueName, *queueIDField, *queueGroupIdxFd, queueItemIndex, false)
					continue
				}
			}

			// Claim conflict or transient error — surface to stderr and retry.
			fmt.Fprintf(os.Stderr, "daemon: workloop: ClaimBead %s error (will retry): %v\n", beadID, claimErr)
			// hk-6pspu: increment br-ready path attempt counter on claim failure.
			if queueItemIndex < 0 {
				readyPathAttempts[beadID]++
			}
			// hk-rnsjs: if the bead is blocked by stale dependencies already in
			// main, auto-close them so the next workloop retry can claim the bead.
			autoCloseStaleBlockersOnClaimFailure(ctx, deps, beadID)
			// On queue-path: revert the item back to pending so the loop can retry.
			// NQ-B1: target the selected queue by name (capturedQueueName).
			if queueItemIndex >= 0 && deps.queueStore != nil {
				lq := deps.queueStore.LockForMutation()
				liveQ := lq.LockedQueueByName(capturedQueueName)
				if liveQ != nil {
					for gi := range liveQ.Groups {
						if queueGroupIdxFd != nil && liveQ.Groups[gi].GroupIndex != *queueGroupIdxFd {
							continue
						}
						if queueItemIndex < len(liveQ.Groups[gi].Items) {
							liveQ.Groups[gi].Items[queueItemIndex].Status = queue.ItemStatusPending
							liveQ.Groups[gi].Items[queueItemIndex].RunID = nil
							// hk-6pspu: record claim failure reason; do NOT reset Attempts (monotonic).
							liveQ.Groups[gi].Items[queueItemIndex].LastFailureReason = claimErr.Error()
						}
					}
					lq.LockedSetQueueByName(capturedQueueName, liveQ)
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
		capturedCtx := capturedExtraContext              // hk-boiwe
		capturedWFMode := capturedItemWFMode             // hk-hiqrl
		capturedWFRef := capturedItemWFRef               // hk-qo9pq
		capturedTmplParams := capturedItemTemplateParams // hk-55zv2 / WG-045

		// Register the run and spawn a goroutine to handle it end-to-end.
		// The goroutine owns Unregister on exit; the outer loop may proceed to
		// claim the next bead immediately (up to effectiveMax).
		//
		// hk-0z5x: create a per-run derived context with its own cancel function
		// so the never-spawned reaper (StaleWatcher.fireNeverSpawnedReaper) can
		// abort THIS run without affecting the daemon or other concurrent runs.
		// The cancel is stored in RunHandle.Cancel for the stale watcher to call.
		runCtx, runCancel := context.WithCancel(ctx)
		// hk-y3frr: hold cacheReapMu.RLock for the duration of Register so the
		// reaper's WLock cannot be acquired while a new run is being inserted into
		// the registry — and so Register blocks while a reap holds the WLock.
		if deps.cacheReapMu != nil {
			deps.cacheReapMu.RLock()
		}
		deps.runRegistry.Register(runID, &RunHandle{
			BeadID: beadID,
			// QueueName tags the run with its dispatching queue so the per-queue
			// capacity tally (LenForQueue) bounds this queue independently of the
			// global ceiling (NQ-B1). Empty for br-ready-fallback runs.
			QueueName: capturedQueueName,
			Labels:    beadRecord.Labels,
			StartedAt: time.Now(),
			Cancel:    runCancel,
		})
		if deps.cacheReapMu != nil {
			deps.cacheReapMu.RUnlock()
		}
		wg.Add(1)
		// NQ-B1: capture the dispatching queue's name so the completion path can
		// resolve the right queue by name (evaluateGroupAdvanceWithOutcome) and
		// the review-loop-failure budget (beadRunOne) updates the right queue.
		// Without this both default to the main-only shim and a non-"main" queue
		// never marks its item terminal → the group stalls forever (hk-tigaf.4).
		go func(runID core.RunID, beadRecord core.BeadRecord, qname string, qid *string, qgidx *int, itemIdx int, extraCtx, itemWFMode, itemWFRef string, tmplParams map[string]string) {
			defer wg.Done()
			defer runCancel() // always release the per-run context, even on panic
			defer deps.runRegistry.Unregister(runID)
			// runSucceeded is set by the emitDone closure inside beadRunOne
			// and read here after beadRunOne returns for EM-015f group-advance.
			var runSucceeded bool
			beadRunOne(runCtx, deps, runID, beadRecord, qname, qid, qgidx, itemIdx, &runSucceeded, extraCtx, itemWFMode, itemWFRef, tmplParams)
			// EM-015f: after run terminal, evaluate queue group advance.
			if itemIdx >= 0 && deps.queueStore != nil && qid != nil && qgidx != nil {
				// hk-ly0hg Fix-1: if the daemon context was cancelled (shutdown),
				// beadRunOne reopened the bead and returned without emitting
				// run_failed. Leave the queue item as 'dispatched' so QM-002a at
				// next startup reverts it to pending (bead is open) rather than
				// permanently recording a false fail.
				if ctx.Err() != nil {
					// Item stays 'dispatched'; QM-002a handles recovery on restart.
				} else {
					evaluateGroupAdvanceWithOutcome(ctx, deps, qname, *qid, *qgidx, itemIdx, runSucceeded)
				}
			}
			// hk-f722 flywheel V9 §5.4 B: on Phase-1 success, emit a staged
			// deploy+verify bead for any Phase-2 class the completed bead belongs to.
			if runSucceeded && ctx.Err() == nil {
				stagedBeadGeneratorEval(ctx, deps, beadRecord.BeadID, beadRecord.Labels)
			}
		}(runID, beadRecord, capturedQueueName, capturedQueueID, capturedQueueGroupIdx, capturedItemIndex, capturedCtx, capturedWFMode, capturedWFRef, capturedTmplParams)
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
func beadRunOne(ctx context.Context, deps workLoopDeps, runID core.RunID, beadRecord core.BeadRecord, queueName string, queueID *string, queueGroupIndex *int, queueItemIndex int, runSucceeded *bool, extraContext string, itemWorkflowMode string, itemWorkflowRef string, itemTemplateParams map[string]string) {
	beadID := beadRecord.BeadID

	// runTipSHA is set (in the DOT failure path) to the worktree HEAD SHA when
	// HEAD has advanced past the parent commit — meaning the implementer produced
	// a commit that the gate later bounced. Included in run_failed so operators
	// can salvage the stranded run-branch commit (hk-8b35c orphan-salvage).
	var runTipSHA *string

	// Resolve owning-epic attribution (hk-7evda, logmine F13): find the parent
	// epic from the bead's edges and look up its assignee (the crew name) so
	// terminal events carry it directly, eliminating captain br round-trips.
	// Best-effort: errors leave the fields empty (non-fatal).
	owningEpicID, owningEpicAssignee := resolveOwningEpicFromRecord(ctx, deps.brAdapter, beadRecord)
	// Propagate to RunHandle so StaleWatcher can read the attribution without
	// its own br calls.
	if handle, ok := deps.runRegistry.Get(runID); ok {
		handle.OwningEpicID = owningEpicID
		handle.OwningEpicAssignee = owningEpicAssignee
	}

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
		emitRunCompleted(ctx, deps.bus, runID, string(beadID), owningEpicID, owningEpicAssignee, success, summary, queueID, queueGroupIndex, runTipSHA)
	}

	// Resolve workflow_mode per execution-model.md §4.3.EM-012a.
	// Four-tier precedence: per-bead label → project config (no-op) →
	// daemon default → dot (hk-30vlb). Resolved once at claim time; immutable for
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

	// Resolve workflow_ref per EM-012a: per-item (queue.Item.WorkflowRef, hk-qo9pq,
	// tier-0) wins over per-bead dot:<name> label (hk-30q6, tier-1); absence falls
	// through to the project-level workflow.dot or embedded standard-bead.dot.
	itemWorkflowRef = resolveWorkflowRef(beadRecord, itemWorkflowRef)

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

	// Determine activeRepo: the repository where the per-bead worktree lives,
	// commits happen, and merges are pushed (hk-xfuc cross-repo dispatch).
	//
	// For local beads (no target_repo or target_repo == projectDir):
	//   activeRepo = deps.projectDir  (unchanged behaviour)
	//
	// For cross-repo beads (target_repo declared in ## Branching):
	//   1. Check the allowed_repos safelist — refuse with CrossRepoUnsafeError
	//      when the target is not in the list (prevents arbitrary path injection).
	//   2. Set activeRepo = target_repo; all git-touching operations below use
	//      activeRepo instead of deps.projectDir.
	//
	// Note: deps.projectDir remains the harmonik project root for non-git
	// operations (daemon socket, queue persistence, br adapter, workflow.dot).
	//
	// Bead: hk-xfuc (cross-repo dispatch follow-up to hk-3r3 guard).
	activeRepo := deps.projectDir

	// Parse the bead body cheaply (tier-1 only; no I/O) to extract target_repo
	// so we can determine activeRepo before resolveParentCommit, which must run
	// against the correct repository.
	earlyBrCfg, _ := parseBranchingSection(beadRecord.Description) // errors treated as absent per BI-009b
	if earlyBrCfg.TargetRepo != "" && earlyBrCfg.TargetRepo != deps.projectDir {
		if !isInAllowedRepos(earlyBrCfg.TargetRepo, deps.allowedRepos) {
			crErr := &CrossRepoUnsafeError{TargetRepo: earlyBrCfg.TargetRepo, ProjectDir: deps.projectDir}
			fmt.Fprintf(os.Stderr, "daemon: workloop: bead %s refused: %v (reopening)\n", beadID, crErr)
			reopenTID, _ := deps.tidGen.Next()
			_ = deps.brAdapter.ReopenBead(ctx, deps.intentLogDir, deps.brTimeoutCfg, runID, reopenTID, beadID,
				crErr.Error())
			return
		}
		activeRepo = earlyBrCfg.TargetRepo
		slog.InfoContext(ctx, "cross_repo_dispatch",
			"bead_id", string(beadID),
			"active_repo", activeRepo,
			"project_dir", deps.projectDir,
		)
	}

	// effectiveMergeProtectBranches is deps.protectBranches for local runs.
	// For cross-repo runs the daemon's ProtectBranches guards harmonik's branches,
	// not the target repo's; pass nil so the merge gate doesn't refuse a legitimate
	// target-repo branch (e.g. merging into kerf's "main" when harmonik protects its
	// own "main"). Hk-xfuc.
	effectiveMergeProtectBranches := deps.protectBranches
	if activeRepo != deps.projectDir {
		effectiveMergeProtectBranches = nil
	}

	// Resolve the parent commit (start_from SHA) for worktree creation per
	// WM-005b / BI-009b. resolveParentCommit parses the bead's ## Branching
	// section and resolves start_from to a commit SHA; it falls back to HEAD
	// when the section is absent or start_from is not set. If start_from is
	// present but names a ref that does not exist locally, the error is
	// surfaced as a typed StartFromRefError and the bead is reopened.
	// Use activeRepo so cross-repo beads resolve against the target repository.
	headSHA, headErr := resolveParentCommit(ctx, activeRepo, string(beadID), beadRecord.Description, deps.targetBranch)
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
	//
	// Protection gate (hk-ncwb3): if the resolved lands_on is in ProtectBranches,
	// the bead must be refused — it would try to merge directly into a branch the
	// operator has declared off-limits for direct pushes. The bead is reopened
	// with a LandsOnProtectedError so the operator can correct the bead body or
	// the project branching config.
	// For cross-repo beads (activeRepo != projectDir), the daemon's ProtectBranches
	// list governs the harmonik project's branches, not the target repo's; skip the
	// protect check for cross-repo runs to avoid refusing legitimate target branches.
	var baseBranch string
	if brCfg, brErr := resolveBranching(ctx, beadRecord.Description, activeRepo, deps.targetBranch); brErr == nil {
		baseBranch = brCfg.LandsOn

		if activeRepo == deps.projectDir {
			for _, protected := range deps.protectBranches {
				if baseBranch == protected {
					protErr := &LandsOnProtectedError{LandsOn: baseBranch}
					fmt.Fprintf(os.Stderr, "daemon: workloop: bead %s refused: %v (reopening)\n", beadID, protErr)
					reopenTID, _ := deps.tidGen.Next()
					_ = deps.brAdapter.ReopenBead(ctx, deps.intentLogDir, deps.brTimeoutCfg, runID, reopenTID, beadID,
						protErr.Error())
					return
				}
			}
		}
	}

	// ── DD1 code-sync: select remote worker (remote-substrate B8) ───────────
	//
	// When a worker is available, three new git steps wrap the existing
	// worktree-add and merge operations:
	//   (a) fetch-base on worker (before worktree-add)
	//   (b) push run-branch from worker to origin (before merge)
	//   (c) fetch run-branch on box A (before merge)
	// Local runs (no registry or no available slot) skip all three steps.
	//
	// remoteBeadCtx is nil for local runs; non-nil for remote runs.
	//
	// rs-tunnel-spawn: tunnelCmd holds the long-lived `ssh -N -R` reverse-tunnel
	// process for this remote run; it is torn down in the run-completion defer
	// (next to ReleaseSlot). workerHookSock is the per-run worker-side reverse-
	// tunnel TCP endpoint the tunnel binds (tcp://127.0.0.1:<port>); the
	// env-override bead (2) injects it as HARMONIK_DAEMON_SOCKET so the
	// worker-side agent's hook relay dials the tunnel rather than box A's
	// unreachable local socket, and the readiness-gate bead (3) references it.
	//
	// hk-ege6: the worker-side bind is a TCP loopback listener, NOT a unix socket.
	// On macOS sshd is root, so a `-R` StreamLocal unix bind is root-owned 0600 and
	// the unprivileged hook user gets connect: permission denied → agent_ready_timeout.
	// A TCP loopback listener has no filesystem permission bits.
	type remoteBeadCtx struct {
		worker         workers.Worker
		sshRunner      tmuxpkg.CommandRunner
		tunnelCmd      *exec.Cmd
		workerHookSock string
	}
	var rbc *remoteBeadCtx
	if deps.workerRegistry != nil {
		if w := deps.workerRegistry.SelectWorker(); w != nil {
			rbc = &remoteBeadCtx{
				worker:    *w,
				sshRunner: tmuxpkg.SSHRunner{Host: w.Host},
			}
			defer deps.workerRegistry.ReleaseSlot()

			// gap #7 Option A: ensure the worker's .harmonik/ dir exists, then
			// start the per-run SSH reverse tunnel — BOTH before any agent Launch.
			//
			//  1. workerHookSock is the per-run worker-side TCP endpoint the tunnel
			//     binds (tcp://127.0.0.1:<port>), shared by beads 1, 2, and 3. The
			//     port is allocated from box A's free ephemeral space as a HINT for
			//     sshd's worker-side bind (collision-safe: see
			//     allocateReverseTunnelPort + ExitOnForwardFailure=yes).
			//  2. ensureWorkerHarmonikDir (bead 2) mkdir-p's the worker's .harmonik/
			//     dir for other per-run artifacts; non-fatal — the readiness gate
			//     (bead 3) is the authority.
			//  3. The tunnel (bead 1) is a SEPARATE long-lived `ssh -N -R`
			//     process: the implementer agent is spawned via a DETACHED ssh
			//     (tmux new-window -d) that returns immediately, so a -R flag on
			//     THAT ssh would tear the tunnel down before the agent's first
			//     hook. The tunnel is keyed to this run and held open for its
			//     lifetime, forwarding the worker-side per-run socket back to box
			//     A's daemon hook socket. Start is non-fatal; teardown defers a
			//     Kill+Wait.
			//
			// NFR7: this block runs only for remote runs (rbc != nil); local runs
			// are byte-identical (no mkdir, no tunnel, no new behaviour).
			// Allocate a free TCP port (hint for sshd's worker-side loopback bind)
			// and form the per-run worker TCP endpoint the hook relay will dial.
			tunnelPort, portErr := allocateReverseTunnelPort()
			if portErr != nil {
				// Non-fatal: log and skip the tunnel; the readiness gate below would
				// fail an empty endpoint, so guard the gate on workerHookSock != "".
				fmt.Fprintf(os.Stderr, "daemon: workloop: reverse-tunnel port alloc bead %s run %s: %v\n",
					beadID, runID.String(), portErr)
			} else {
				rbc.workerHookSock = workerTCPEndpoint(tunnelPort)
			}

			if mkErr := ensureWorkerHarmonikDir(ctx, rbc.sshRunner, rbc.worker.RepoPath); mkErr != nil {
				fmt.Fprintf(os.Stderr,
					"daemon: workloop: ensureWorkerHarmonikDir bead %s run %s: %v (non-fatal; readiness gate is authority)\n",
					beadID, runID.String(), mkErr)
			}

			daemonHookSock := filepath.Join(deps.projectDir, ".harmonik", "daemon.sock")
			// Mirror the SSHRunner host/opts argv pattern (runner.go SSHRunner.Command):
			// extra opts BEFORE the host. Fall back to the worker record's Host when
			// the runner is not an SSHRunner (e.g. a test double).
			tunnelHost, tunnelOpts, hostOK := sshHostOpts(rbc.sshRunner)
			if !hostOK {
				tunnelHost = rbc.worker.Host
			}
			tunnelArgs := buildReverseTunnelArgs(tunnelPort, daemonHookSock, tunnelHost, tunnelOpts)
			rbc.tunnelCmd = reverseTunnelRunner(ctx, "ssh", tunnelArgs...)
			if startErr := rbc.tunnelCmd.Start(); startErr != nil {
				// Non-fatal: a failed tunnel start means the worker-side agent's hooks
				// will not reach box A, but the readiness gate (bead 3) is the
				// authority that fails the run. Log and clear tunnelCmd so the
				// teardown defer is a no-op.
				fmt.Fprintf(os.Stderr, "daemon: workloop: reverse-tunnel start bead %s run %s: %v\n",
					beadID, runID.String(), startErr)
				rbc.tunnelCmd = nil
			}
			defer func() {
				if rbc.tunnelCmd != nil && rbc.tunnelCmd.Process != nil {
					_ = rbc.tunnelCmd.Process.Kill()
					_ = rbc.tunnelCmd.Wait()
				}
			}()

			// gap #7 bead 3: tunnel readiness gate. The worker-side implementer
			// agent can fire its first agent_ready hook BEFORE the `ssh -N -R`
			// forward above is actually live; the hook relay retries only on
			// daemon_not_ready, NOT on a dial failure, so launching the agent
			// before the forward is live yields a silent bridge_dial_failed →
			// agent_ready_timeout. Block until the worker-side per-run TCP listener
			// is confirmed CONNECTABLE (nc -z over the SSHRunner, as the worker
			// user) before any Launch — an existence-only check would false-green a
			// non-connectable endpoint (hk-ege6). On timeout/failure (including a
			// failed port alloc that left workerHookSock empty), do NOT launch:
			// emit worker_tunnel_failed, reopen
			// the bead for re-dispatch, and return — the deferred tunnel teardown
			// (above) and ReleaseSlot run on the way out, so the `ssh -N` process
			// does not leak. The gate runs ONLY here, inside the remote branch
			// (NFR7: local runs never construct a tunnel and never reach it).
			if waitErr := waitWorkerSocketLive(ctx, rbc.sshRunner, rbc.workerHookSock, workerSocketReadyTimeout); waitErr != nil {
				fmt.Fprintf(os.Stderr,
					"daemon: workloop: reverse-tunnel readiness gate bead %s run %s: %v (reopening, not launching)\n",
					beadID, runID.String(), waitErr)
				workers.EmitWorkerTunnelFailedEvent(ctx, runID.String(), string(beadID),
					rbc.worker.Name, rbc.worker.Host, rbc.workerHookSock, waitErr.Error(), deps.bus.Emit)
				reopenTID, _ := deps.tidGen.Next()
				_ = deps.brAdapter.ReopenBead(ctx, deps.intentLogDir, deps.brTimeoutCfg, runID, reopenTID, beadID,
					fmt.Sprintf("reverse-tunnel not ready: %v", waitErr))
				return
			}
		}
	}

	// notifyWorkerOffline emits a worker_offline event and disables the worker
	// in-memory. Active only for remote runs (rbc != nil). Phase is "spawn" for
	// code-sync failures and "liveness" for mid-run probe failures (B11).
	notifyWorkerOffline := func(phase, detail string) {
		if rbc == nil {
			return
		}
		workers.EmitWorkerOfflineEvent(ctx, rbc.worker.Name, rbc.worker.Host, phase, detail, deps.bus.Emit)
		if deps.workerRegistry != nil {
			deps.workerRegistry.SetEnabled(false)
		}
	}

	// Step (a): for remote runs, ensure baseSHA is on the worker before the
	// worktree is created there (DD1 code-sync, remote-substrate B8).
	if rbc != nil {
		if fetchErr := fetchBaseOnWorker(ctx, rbc.sshRunner, rbc.worker.RepoPath, headSHA); fetchErr != nil {
			fmt.Fprintf(os.Stderr, "daemon: workloop: fetchBaseOnWorker bead %s run %s: %v (reopening)\n",
				beadID, runID.String(), fetchErr)
			// B11: SSH connection failure → emit worker_offline + disable worker.
			if tmuxpkg.IsSSHConnectionFailure(fetchErr) {
				notifyWorkerOffline("spawn", fmt.Sprintf("fetchBaseOnWorker: %v", fetchErr))
			}
			reopenTID, _ := deps.tidGen.Next()
			_ = deps.brAdapter.ReopenBead(ctx, deps.intentLogDir, deps.brTimeoutCfg, runID, reopenTID, beadID,
				fmt.Sprintf("fetch base on worker failed: %v", fetchErr))
			return
		}
	}

	// preMergeSync brings the run branch onto box A before the merge. For remote
	// runs it fetches the branch DIRECTLY from the worker repo over SSH
	// (ssh://<host><repoPath>) — hk-7bwx — rather than the old worker→GitHub→box-A
	// round-trip, which failed when the worker had no valid GitHub push credential.
	// Returns an error string on failure (empty string = success). No-op for local
	// runs (rbc == nil). The final mergeRunBranchToMain pushes box A's MAIN to
	// GitHub with box A's own (valid) credentials — unaffected by this change.
	preMergeSync := func() string {
		if rbc == nil {
			return ""
		}
		// host/opts come from the worker SSHRunner so git's ssh:// fetch dials the
		// worker exactly like the rest of the remote path.
		workerHost, sshOpts, _ := sshHostOpts(rbc.sshRunner)
		if err := fetchRunBranchBoxA(ctx, nil, deps.projectDir, runID.String(), workerHost, rbc.worker.RepoPath, sshOpts); err != nil {
			// B11: SSH connection failure → emit worker_offline + disable worker.
			if tmuxpkg.IsSSHConnectionFailure(err) {
				notifyWorkerOffline("spawn", fmt.Sprintf("fetchRunBranchBoxA: %v", err))
			}
			return fmt.Sprintf("fetch run branch from worker on box A: %v", err)
		}
		return ""
	}
	// ── end DD1 code-sync setup ──────────────────────────────────────────────

	wtFactory := deps.worktreeFactory
	if wtFactory == nil {
		if rbc != nil {
			// Remote run: create the worktree on the worker via SSHRunner (B7+B8).
			sshRunner := rbc.sshRunner
			workerRepoPath := rbc.worker.RepoPath
			wtFactory = func(ctx context.Context, _, runID, headSHA string) (string, func(), error) {
				cfg := workspace.NoWorktreeRootOverride().WithRunner(sshRunner)
				if err := workspace.CreateWorktree(ctx, workerRepoPath, runID, headSHA, cfg); err != nil {
					return "", nil, err
				}
				wtPath := workspace.WorktreePath(workerRepoPath, runID, workspace.NoWorktreeRootOverride())
				// B11: return a cleanup func that removes the remote worktree on
				// run completion (GC orphaned remote worktrees via the SSHRunner).
				cleanup := func() {
					cleanCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
					defer cancel()
					rmCmd := sshRunner.Command(cleanCtx, "git", "-C", workerRepoPath, "worktree", "remove", "--force", "--force", wtPath)
					_ = rmCmd.Run()
					pruneCmd := sshRunner.Command(cleanCtx, "git", "-C", workerRepoPath, "worktree", "prune")
					_ = pruneCmd.Run()
				}
				return wtPath, cleanup, nil
			}
		} else {
			wtFactory = productionWorktreeFactory
		}
	}
	// Serialize 'git worktree add' under mergeMu so concurrent beadRunOne
	// goroutines do not race on projectDir/.git/index.lock (hk-h8u7p).
	// mergeMu already guards all git operations on the main repo
	// (merge/rebase/push); worktree creation touches the same index.lock,
	// so it belongs under the same serialisation boundary.
	if deps.mergeMu != nil {
		deps.mergeMu.Lock()
	}
	wtPath, wtCleanup, wtErr := wtFactory(ctx, activeRepo, runID.String(), headSHA)
	if deps.mergeMu != nil {
		deps.mergeMu.Unlock()
	}
	if wtErr != nil {
		fmt.Fprintf(os.Stderr, "daemon: workloop: CreateWorktree for bead %s run %s: %v (reopening)\n", beadID, runID.String(), wtErr)
		// Surface the worktree-create failure as a terminal run_failed event so it
		// lands in events.jsonl. Without this the failure is invisible to operators
		// — they only see a downstream agent_ready_timeout (~90s later) with no
		// cause attached. Emit BEFORE reopening the bead + returning. (hk-3vbc)
		emitDone(false, fmt.Sprintf("worktree_create_failed: %v", wtErr))
		reopenTID, _ := deps.tidGen.Next()
		_ = deps.brAdapter.ReopenBead(ctx, deps.intentLogDir, deps.brTimeoutCfg, runID, reopenTID, beadID,
			fmt.Sprintf("create worktree failed: %v", wtErr))
		return
	}
	if wtCleanup != nil {
		defer wtCleanup()
	}

	// hk-z8u: provision gitignored-but-required files (e.g. a `.env` consumed by a
	// docker compose test gate) into the freshly created worktree. `git worktree
	// add` checks out only TRACKED files, so such files are otherwise absent and a
	// gate that needs them fails before any work runs. LOCAL runs only: remote
	// (SSH-worker) worktrees live on another host and are not reachable through the
	// local filesystem, so they are skipped (rbc != nil). Empty config = no-op (the
	// backward-compatible default). A provision error is non-fatal — warn and
	// continue so a stray config entry never wedges dispatch; ProvisionWorktreeFiles
	// already warn-skips a missing source itself.
	if rbc == nil && len(deps.worktreeProvisionFiles) > 0 {
		if provErr := workspace.ProvisionWorktreeFiles(activeRepo, wtPath, deps.worktreeProvisionFiles); provErr != nil {
			fmt.Fprintf(os.Stderr, "daemon: workloop: ProvisionWorktreeFiles for bead %s run %s: %v (continuing)\n", beadID, runID.String(), provErr)
		}
	}

	// hk-ooexj: snapshot the active repo's pre-existing untracked files at run-start
	// (before the implementer launches) so the post-run escape check can exclude
	// files that already existed and which the implementer never touched. A failed
	// snapshot leaves preRunUntracked nil — the escape check then degrades to its
	// prior, baseline-free behaviour rather than silently suppressing escapes.
	// Use activeRepo: cross-repo runs create the worktree in the target repo.
	preRunUntracked, snapErr := snapshotUntrackedFiles(ctx, activeRepo)
	if snapErr != nil {
		fmt.Fprintf(os.Stderr, "daemon: workloop: snapshotUntrackedFiles for bead %s run %s: %v (escape check will run without baseline)\n", beadID, runID.String(), snapErr)
	}

	// Emit run_started with optional queue_id + queue_group_index per QM-011/QM-012.
	// FR13: include worker_name and worker_os for remote runs; empty for local.
	var runStartedWorkerName, runStartedWorkerOS string
	if rbc != nil {
		runStartedWorkerName = rbc.worker.Name
		runStartedWorkerOS = rbc.worker.OS
	}
	emitRunStarted(ctx, deps.bus, runID, beadID, wtPath, queueID, queueGroupIndex, runStartedWorkerName, runStartedWorkerOS, string(workflowMode))

	// hk-f38n: the pre-dispatch subsumption check (hk-ly0hg Fix-2 / hk-wcv) was
	// REMOVED here. That check called beadAlreadySubsumedInMain — a bare
	// "Refs: <id>" git-log grep — and closed the bead pre-dispatch when it
	// matched. For multi-aspect / partially-committed beads this was a
	// false-positive: old partial commits carrying the same bead ID caused the
	// daemon to conclude the whole bead was done and close it before the
	// remaining work ran (live incident: hk-cmry false-closed, work refiled as
	// hk-zmpd to escape the poisoned grep-match).
	//
	// The crash-restart recovery case (bead commit landed on main, daemon died
	// before CloseBead completed) is correctly handled by the RUNTIME paths
	// instead:
	//   • noChange-timeout (pasteInjectQuitOnCommit): agent makes no commit →
	//     noChangeTimeoutCh fires → beadAlreadySubsumedInMain → CloseBead.
	//   • noCommitGuard (beadRunOne): no HEAD advance + Refs on main →
	//     noCommitGuardShouldReopen=false → auto-close branch.
	// Both paths inspect whether work is actually present before closing, so
	// the crash-restart close is preserved without false-positives.

	// Pre-switch: for DOT mode, resolve and pre-load the graph source (hk-30vlb).
	// Three-tier resolution:
	//   1. itemWorkflowRef set → explicit path (resolved below in the DOT case).
	//   2. <projectDir>/workflow.dot exists → project-level path (resolved below).
	//   3. Neither → use the embedded standard-bead.dot (loaded here).
	//
	// The embedded load happens here — before the switch — so that if it fails we
	// can change workflowMode to review-loop and let the review-loop case execute
	// normally (spec §REVIEW FLOOR item b: fall through to review-loop, NEVER single).
	var preloadedDotGraph *dot.Graph
	if workflowMode == core.WorkflowModeDot && itemWorkflowRef == "" {
		defaultDotPath := filepath.Join(deps.projectDir, "workflow.dot")
		if _, statErr := os.Stat(defaultDotPath); os.IsNotExist(statErr) {
			g, embErr := loadStandardGraph(itemTemplateParams)
			if embErr != nil {
				// Safety floor (hk-30vlb §REVIEW FLOOR item b): embedded graph parse
				// failure — fall through to review-loop, NEVER to single.
				fmt.Fprintf(os.Stderr,
					"daemon: workloop: embedded standard-bead.dot load failed for bead %s run %s: %v (falling back to review-loop)\n",
					beadID, runID.String(), embErr)
				workflowMode = core.WorkflowModeReviewLoop
			} else {
				preloadedDotGraph = g
			}
		}
	}

	// Pre-build the routed launchSpecBuilder once (T12 hk-xhawy) so ALL workflow
	// modes (review-loop, DOT cascade, single) share the same harness-resolved
	// builder. deps.launchSpecBuilder may be pre-injected by test fixtures; leave
	// it untouched in that case. For production (nil), build from harnessRegistry +
	// beadRecord (tier-1 labels) now — before the mode switch — so runReviewLoop and
	// driveDotWorkflow can read deps.launchSpecBuilder instead of calling
	// buildClaudeLaunchSpec directly.
	if deps.launchSpecBuilder == nil {
		if deps.harnessRegistry != nil {
			deps.launchSpecBuilder = routedLaunchSpecBuilder(
				deps.harnessRegistry,
				beadRecord,
				core.AgentType(""), // queue default: per-queue harness field not yet landed (hk-4x3rg)
				core.AgentType(""), // node default: overridden per-node in driveDotWorkflow (T5/T12)
				core.AgentType(""), // global default: built-in fallback resolves to claude-code
				deps.bus,
			)
		} else {
			// No registry (legacy test fixtures): fall back to direct claude builder.
			deps.launchSpecBuilder = buildClaudeLaunchSpec
		}
	}

	// Mode-dispatch: route to the mode-specific driver.
	//
	// review-loop mode (EM-015d): multi-iteration implementer→reviewer cycle
	// handled by runReviewLoop in reviewloop.go. Also the fallback when the
	// embedded DOT graph fails to load (hk-30vlb §REVIEW FLOOR item b).
	//
	// dot mode: DOT-defined workflow graph; loader validates the artifact,
	// then drives the cascade (driveDotWorkflow). Default uses the embedded
	// standard-bead.dot (pre-loaded above into preloadedDotGraph).
	//
	// single mode: one-shot implementer dispatch.
	switch workflowMode {
	case core.WorkflowModeReviewLoop:
		// rlRunner is the per-run CommandRunner threaded into the review loop: the
		// worker's SSHRunner for a REMOTE run (so the implementer-worktree HEAD/diff
		// probes target the worker and the run branch is synced to box A before each
		// reviewer launch), nil for a LOCAL run (byte-identical local path, NFR7).
		var rlRunner tmuxpkg.CommandRunner
		// hk-fxy9: rlWorkerBinary resolves the SessionStart hook command to the WORKER's
		// harmonik path; rlWorkerHookSock is the worker-side reverse-tunnel TCP endpoint
		// the worker's claude dials for the hook relay; rlWorkerSession/Cwd tell the
		// per-run substrate which tmux session to ensure+spawn into ON THE WORKER. All
		// empty for a LOCAL run (rbc == nil) ⇒ byte-identical box-A-local path (NFR7).
		var rlWorkerBinary, rlWorkerHookSock, rlWorkerSession, rlWorkerCwd string
		if rbc != nil {
			rlRunner = rbc.sshRunner
			rlWorkerBinary = workerHarmonikPath(rbc.worker)
			rlWorkerHookSock = rbc.workerHookSock
			rlWorkerCwd = rbc.worker.RepoPath
			if ts, ok := deps.substrate.(*tmuxSubstrate); ok {
				rlWorkerSession = ts.workerSpawnSessionName(rbc.worker.Name)
			}
		}
		rlResult := runReviewLoop(ctx, deps, runID, beadID, beadRecord.Title, beadRecord.Description, wtPath, headSHA, resolvedModel, resolvedEffort, extraContext, baseBranch, rlRunner, rlWorkerBinary, rlWorkerHookSock, rlWorkerSession, rlWorkerCwd)

		transitionTID, _ := deps.tidGen.Next()
		if rlResult.success {
			// Scenario gate (hk-i2ie5): block merge when scenario tests go RED.
			// REMOTE: route via rlRunner so the gate's git-diff, file sniff, and
			// `go test` run on the worker where the worktree lives (nil ⇒ box-A-local).
			if sgr := runScenarioGateIfNeededVia(ctx, rlRunner, wtPath, headSHA); sgr.blocked {
				emitOutcomeEmitted(ctx, deps.bus, runID, beadID, "rejected", sgr.reason)
				reopenTID, _ := deps.tidGen.Next()
				_ = deps.brAdapter.ReopenBead(ctx, deps.intentLogDir, deps.brTimeoutCfg, runID, reopenTID, beadID, sgr.reason)
				emitDone(false, sgr.reason)
				return
			}
			// §4.12.EM-052: merge run-branch to main before CloseBead.
			// Mirrors the single-mode merge path (hk-ftyvo).
			// DD1 code-sync (remote-substrate B8): push-branch + box-A-fetch
			// BEFORE merge when a remote worker was used.
			if syncReason := preMergeSync(); syncReason != "" {
				emitOutcomeEmitted(ctx, deps.bus, runID, beadID, "rejected", syncReason)
				reopenTID, _ := deps.tidGen.Next()
				_ = deps.brAdapter.ReopenBead(ctx, deps.intentLogDir, deps.brTimeoutCfg, runID, reopenTID, beadID,
					fmt.Sprintf("code-sync failed (review-loop): %s", syncReason))
				emitDone(false, fmt.Sprintf("code-sync-failed (review-loop): %s", syncReason))
				return
			}
			// hk-dyim: amend the HEAD commit in the implementer worktree to embed
			// Reviewed-By: and Review-Verdict: trailers before the FF merge, so the
			// review audit trail is visible in git history. Non-fatal: the merge
			// proceeds without trailers if the amend fails (e.g. empty worktree).
			// LOCAL runs only (rbc == nil): for REMOTE runs the implementer worktree
			// lives on the worker and the amend would require SSH routing; the
			// rebase inside mergeRunBranchToMain runs on box-A and sees the
			// worker-side commits, so the trailers land post-rebase on box-A in a
			// follow-up (FLAGGED).
			if rlResult.approveVerdict != nil && rbc == nil {
				if amendErr := appendReviewTrailersToHEAD(ctx, wtPath, rlResult.approveVerdict); amendErr != nil {
					fmt.Fprintf(os.Stderr, "daemon: workloop: appendReviewTrailersToHEAD bead %s: %v (non-fatal)\n", beadID, amendErr)
				}
			}
			mergeRes := lockedMergeRunBranchToMain(ctx, deps.mergeMu, activeRepo, runID, deps.bus, beadID, headSHA, deps.targetBranch, effectiveMergeProtectBranches)
			if !mergeRes.noChange && !mergeRes.success {
				emitOutcomeEmitted(ctx, deps.bus, runID, beadID, "rejected", mergeRes.reason)
				reopenTID, _ := deps.tidGen.Next()
				_ = deps.brAdapter.ReopenBead(ctx, deps.intentLogDir, deps.brTimeoutCfg, runID, reopenTID, beadID,
					fmt.Sprintf("merge-to-main failed: %s", mergeRes.reason))
				emitDone(false, fmt.Sprintf("merge-failed (review-loop): %s", mergeRes.reason))
				return
			}
			emitOutcomeEmitted(ctx, deps.bus, runID, beadID, "approved", "")
			if closeErr := deps.closeBeadWithHistoryTrim(ctx, runID, transitionTID, beadID, false); closeErr != nil {
				fmt.Fprintf(os.Stderr, "daemon: workloop: CloseBead (review-loop APPROVE) %s: %v\n", beadID, closeErr)
				// hk-hypbi: transient BrUnavailable after successful merge → emit
				// success; intent file retained for BI-031 recovery on next startup.
				if errors.Is(closeErr, brcli.BrUnavailable) {
					emitDone(true, "close-transient-merged (review-loop APPROVE)")
				} else {
					emitDone(false, fmt.Sprintf("close-error: %v", closeErr))
				}
			} else {
				emitBeadClosedAndMaybeEpic(ctx, deps, runID, beadID)
				emitDone(true, rlResult.summary)
			}
		} else {
			// Review-loop failed. For queue-dispatched runs with needsAttention=true,
			// increment the per-item ReviewLoopFailures counter and check whether the
			// global retry-spend budget is exhausted (hk-c1ah6).
			//
			// Budget exhaustion (ReviewLoopFailures >= MaxReviewLoopFailures) closes
			// the bead permanently with needsAttention=true so that it does NOT
			// re-enter the ready queue. Without this cap, a structurally-stuck bead
			// burns a full Claude session on every re-dispatch indefinitely.
			budgetExhausted := false
			if rlResult.needsAttention && queueID != nil && queueGroupIndex != nil &&
				queueItemIndex >= 0 && deps.queueStore != nil {
				lq := deps.queueStore.LockForMutation()
				// NQ-B1: resolve the queue BY NAME (capturedQueueName), not the
				// main-only lq.Queue() shim — otherwise the review-loop-failure
				// budget for a non-"main" queue is read/written against the wrong
				// queue (or nil) and the cap never trips (hk-tigaf.4).
				normName := queue.NormaliseQueueName(queueName)
				liveQ := lq.LockedQueueByName(normName)
				if liveQ != nil {
				outerBudgetLoop:
					for gi := range liveQ.Groups {
						if liveQ.Groups[gi].GroupIndex != *queueGroupIndex {
							continue
						}
						if queueItemIndex < len(liveQ.Groups[gi].Items) &&
							liveQ.Groups[gi].Items[queueItemIndex].BeadID == beadID {
							liveQ.Groups[gi].Items[queueItemIndex].ReviewLoopFailures++
							if liveQ.Groups[gi].Items[queueItemIndex].ReviewLoopFailures >= queue.MaxReviewLoopFailures {
								budgetExhausted = true
								liveQ.Groups[gi].Items[queueItemIndex].LastFailureReason = "review_loop_budget_exhausted"
							}
							break outerBudgetLoop
						}
					}
					lq.LockedSetQueueByName(normName, liveQ)
					if persistErr := queue.Persist(ctx, deps.projectDir, liveQ); persistErr != nil {
						fmt.Fprintf(os.Stderr, "daemon: workloop: Persist rl-failures queueID=%s: %v\n",
							liveQ.QueueID, persistErr)
					}
				}
				lq.Done()
			}

			if budgetExhausted {
				// Budget exhausted: permanently close the bead with needs-attention
				// rather than reopening it (hk-c1ah6).
				exhaustedSummary := fmt.Sprintf("review_loop_budget_exhausted (max=%d failures): %s",
					queue.MaxReviewLoopFailures, rlResult.summary)
				fmt.Fprintf(os.Stderr, "daemon: workloop: bead %s run %s review-loop budget exhausted — closing with needs-attention (hk-c1ah6)\n",
					beadID, runID.String())
				emitOutcomeEmitted(ctx, deps.bus, runID, beadID, "rejected", exhaustedSummary)
				budgetTID, _ := deps.tidGen.Next()
				if closeErr := deps.closeBeadWithHistoryTrim(ctx, runID, budgetTID, beadID, true); closeErr != nil {
					fmt.Fprintf(os.Stderr, "daemon: workloop: CloseBead (review-loop budget-exhausted) %s: %v\n",
						beadID, closeErr)
					// hk-hypbi: on transient BrUnavailable, leave bead in_progress for
					// BI-031 recovery — do NOT reopen, which would re-dispatch the bead
					// and bypass the operator-triage needs-attention requirement.
					if !errors.Is(closeErr, brcli.BrUnavailable) {
						reopenTID, _ := deps.tidGen.Next()
						_ = deps.brAdapter.ReopenBead(ctx, deps.intentLogDir, deps.brTimeoutCfg, runID, reopenTID, beadID, exhaustedSummary)
					}
				} else {
					emitBeadClosedAndMaybeEpic(ctx, deps, runID, beadID)
				}
				emitDone(false, exhaustedSummary)
			} else {
				// Budget not exhausted (or no queue): reopen the bead for retry.
				reopenTID, _ := deps.tidGen.Next()
				if reopenErr := deps.brAdapter.ReopenBead(ctx, deps.intentLogDir, deps.brTimeoutCfg, runID, reopenTID, beadID, rlResult.summary); reopenErr != nil {
					fmt.Fprintf(os.Stderr, "daemon: workloop: ReopenBead (review-loop %s) %s: %v\n", rlResult.completionReason, beadID, reopenErr)
				}
				emitDone(false, rlResult.summary)
			}
		}
		return

	case core.WorkflowModeDot:
		// DOT workflow mode: load + validate the .dot artifact, then hand the
		// validated graph to the cascade driver (driveDotWorkflow, dot_cascade.go)
		// which walks the graph node-by-node using workflow.DecideNextNode
		// (hk-9dnak; cascade engine library hk-bf85t).
		//
		// Graph source resolution uses preloadedDotGraph (Tier 3: embedded) when
		// already set by the pre-switch block; otherwise resolves Tier 1/2 from
		// an explicit ref or <projectDir>/workflow.dot (three-tier spec hk-30vlb).
		// Embedded-load failure was already handled above: workflowMode was changed
		// to WorkflowModeReviewLoop and this case is not reached.
		var graph *dot.Graph
		if preloadedDotGraph != nil {
			// Tier 3: embedded standard-bead.dot (already parsed and validated).
			graph = preloadedDotGraph
		} else {
			// Tier 1 or 2: explicit ref or <projectDir>/workflow.dot.
			// WG-046 ordering: read → substitute(itemTemplateParams) → parse → validate → dispatch.
			dotPath := filepath.Join(deps.projectDir, "workflow.dot")
			if itemWorkflowRef != "" {
				if filepath.IsAbs(itemWorkflowRef) {
					dotPath = itemWorkflowRef
				} else {
					dotPath = filepath.Join(deps.projectDir, itemWorkflowRef)
				}
			}
			var loadErr error
			graph, loadErr = workflow.LoadDotWorkflowWithParams(dotPath, itemTemplateParams)
			if loadErr != nil {
				fmt.Fprintf(os.Stderr, "daemon: workloop: DOT workflow load failed for bead %s run %s: %v (reopening)\n",
					beadID, runID.String(), loadErr)
				reopenTID, _ := deps.tidGen.Next()
				_ = deps.brAdapter.ReopenBead(ctx, deps.intentLogDir, deps.brTimeoutCfg, runID, reopenTID, beadID,
					fmt.Sprintf("workflow_load: %v", loadErr))
				emitDone(false, fmt.Sprintf("workflow_load: %v", loadErr))
				return
			}
		}

		// WG-044: thread the (substituted) graph-level goal into every agentic node's
		// brief via the ExtraContext channel.  Prepend so it appears before any
		// operator-supplied --context text.
		dotExtraContext := extraContext
		if graph.Goal != "" {
			goalLine := "Workflow goal: " + graph.Goal
			if dotExtraContext != "" {
				dotExtraContext = goalLine + "\n\n" + dotExtraContext
			} else {
				dotExtraContext = goalLine
			}
		}

		// remote-substrate: for a remote run the cascade's worktree git probes
		// (resolve HEAD, diff-hash) and the agentic-node spawn must target the
		// WORKER via its SSHRunner; box A cannot chdir into the worker's worktree.
		// nil for local runs keeps the cascade byte-identical (NFR7).
		var dotRunner tmuxpkg.CommandRunner
		// hk-538l: dotWorkerBinary resolves each node's SessionStart hook command to the
		// WORKER's harmonik path; dotWorkerHookSock is the worker-side reverse-tunnel TCP
		// endpoint each node's claude dials for the hook relay; dotWorkerSession/Cwd tell
		// the per-run substrate which tmux session to ensure+spawn into ON THE WORKER.
		// All empty for a LOCAL run (rbc == nil) ⇒ byte-identical box-A path (NFR7).
		var dotWorkerBinary, dotWorkerHookSock, dotWorkerSession, dotWorkerCwd string
		if rbc != nil {
			dotRunner = rbc.sshRunner
			dotWorkerBinary = workerHarmonikPath(rbc.worker)
			dotWorkerHookSock = rbc.workerHookSock
			dotWorkerCwd = rbc.worker.RepoPath
			if ts, ok := deps.substrate.(*tmuxSubstrate); ok {
				dotWorkerSession = ts.workerSpawnSessionName(rbc.worker.Name)
			}
		}

		// Drive the cascade: walk start → … → terminal, dispatching each node by
		// type (non-agentic synthesize-success, agentic substrate-dispatch,
		// gate/sub-workflow out-of-scope error).
		dotResult := driveDotWorkflow(ctx, deps, runID, beadID, beadRecord, beadRecord.Title, beadRecord.Description,
			wtPath, headSHA, graph, resolvedModel, resolvedEffort, dotExtraContext, baseBranch, dotRunner,
			dotWorkerBinary, dotWorkerHookSock, dotWorkerSession, dotWorkerCwd)

		transitionTID, _ := deps.tidGen.Next()
		if dotResult.success {
			// Scenario gate: the DOT cascade engine (dispatchDotToolNode) already
			// maps exit codes to outcomes correctly — timeout→transient (commit_gate
			// self-loop retry), non-zero→deterministic (fix-loop back to implement).
			// No separate runScenarioGateIfNeeded call is needed on the DOT path;
			// the gate lives inside standard-bead.dot's commit_gate tool_command.
			// §4.12.EM-052: merge run-branch to main before CloseBead (mirrors the
			// single-mode and review-loop merge path).
			// DD1 code-sync (remote-substrate B8): push-branch + box-A-fetch BEFORE merge.
			if syncReason := preMergeSync(); syncReason != "" {
				emitOutcomeEmitted(ctx, deps.bus, runID, beadID, "rejected", syncReason)
				reopenTID, _ := deps.tidGen.Next()
				_ = deps.brAdapter.ReopenBead(ctx, deps.intentLogDir, deps.brTimeoutCfg, runID, reopenTID, beadID,
					fmt.Sprintf("code-sync failed (dot): %s", syncReason))
				emitDone(false, fmt.Sprintf("code-sync-failed (dot): %s", syncReason))
				return
			}
			// hk-tnui: stamp Reviewed-By / Review-Verdict trailers on the HEAD
			// commit before the FF merge, mirroring the review-loop path (line
			// 3095). LOCAL runs only (rbc == nil): remote runs keep the trailer
			// injection deferred (same FLAGGED note as the review-loop path).
			if dotResult.approveVerdict != nil && rbc == nil {
				if amendErr := appendReviewTrailersToHEAD(ctx, wtPath, dotResult.approveVerdict); amendErr != nil {
					fmt.Fprintf(os.Stderr, "daemon: workloop: appendReviewTrailersToHEAD bead %s (dot): %v (non-fatal)\n", beadID, amendErr)
				}
			}
			mergeRes := lockedMergeRunBranchToMain(ctx, deps.mergeMu, activeRepo, runID, deps.bus, beadID, headSHA, deps.targetBranch, effectiveMergeProtectBranches)
			if !mergeRes.noChange && !mergeRes.success {
				emitOutcomeEmitted(ctx, deps.bus, runID, beadID, "rejected", mergeRes.reason)
				reopenTID, _ := deps.tidGen.Next()
				_ = deps.brAdapter.ReopenBead(ctx, deps.intentLogDir, deps.brTimeoutCfg, runID, reopenTID, beadID,
					fmt.Sprintf("merge-to-main failed: %s", mergeRes.reason))
				emitDone(false, fmt.Sprintf("merge-failed (dot): %s", mergeRes.reason))
				return
			}
			emitOutcomeEmitted(ctx, deps.bus, runID, beadID, "approved", "")
			if closeErr := deps.closeBeadWithHistoryTrim(ctx, runID, transitionTID, beadID, false); closeErr != nil {
				fmt.Fprintf(os.Stderr, "daemon: workloop: CloseBead (dot success) %s: %v\n", beadID, closeErr)
				// hk-hypbi: transient BrUnavailable after successful merge → emit success.
				if errors.Is(closeErr, brcli.BrUnavailable) {
					emitDone(true, "close-transient-merged (dot success)")
				} else {
					emitDone(false, fmt.Sprintf("close-error: %v", closeErr))
				}
			} else {
				emitBeadClosedAndMaybeEpic(ctx, deps, runID, beadID)
				emitDone(true, dotResult.summary)
			}
		} else if dotResult.subsumed {
			// noChange-subsumed: implementer exited without advancing HEAD because
			// the work already landed in main via a prior run. Close the bead
			// (mirrors the builtin noChange-subsumed path in the default switch,
			// workloop.go:1831-1843). No merge needed — no new commits. Bead: hk-9v5yo.
			emitOutcomeEmitted(ctx, deps.bus, runID, beadID, "approved", "")
			if closeErr := deps.closeBeadWithHistoryTrim(ctx, runID, transitionTID, beadID, false); closeErr != nil {
				fmt.Fprintf(os.Stderr, "daemon: workloop: CloseBead (dot noChange-subsumed) %s: %v\n", beadID, closeErr)
				// hk-hypbi: transient BrUnavailable after merge-already-on-main → emit success.
				if errors.Is(closeErr, brcli.BrUnavailable) {
					emitDone(true, "close-transient-merged (dot noChange-subsumed)")
				} else {
					emitDone(false, fmt.Sprintf("close-error: %v", closeErr))
				}
			} else {
				emitBeadClosedAndMaybeEpic(ctx, deps, runID, beadID)
				emitDone(true, "noChange-subsumed: bead found in main")
			}
		} else {
			// Non-success terminal (BLOCK / cap-hit / no-progress / structural
			// failure / gate-out-of-scope / context_cancelled) → reopen the bead
			// so it can be retried or escalated. The needsAttention flag is
			// carried in the summary.
			//
			// Orphan-salvage (hk-8b35c): if the implementer advanced HEAD past the
			// parent (a commit landed on the run branch before the gate bounced it),
			// record the tip SHA in the run_failed payload so the operator can find
			// and manually cherry-pick / merge the stranded work.
			//
			// REMOTE: wtPath is on the worker; resolve via dotRunner so the tip SHA
			// reflects the worker's run branch (nil dotRunner ⇒ box-A-local, NFR7).
			//
			// hk-e3fy: use context.Background() for the HEAD resolve so a
			// context_cancelled DOT failure (daemon shutdown or per-run abort)
			// can still capture the tip SHA for orphan-salvage — ctx is already
			// cancelled when this path fires for that failure class.
			tipResolveCtx := ctx
			if ctx.Err() != nil {
				tipResolveCtx = context.Background()
			}
			if tipSHA, tipErr := resolveWorktreeHEADVia(tipResolveCtx, dotRunner, wtPath); tipErr == nil && tipSHA != "" && tipSHA != headSHA {
				runTipSHA = &tipSHA
			}
			reopenTID, _ := deps.tidGen.Next()
			// hk-e3fy: ReopenBead uses context.Background() so a context_cancelled
			// DOT run (daemon shutdown, per-run abort, or agentic-node budget-cancel)
			// still resets the bead to open. Using ctx here silently discards the
			// call when ctx is already cancelled — exactly the strand class this
			// bead fixes.
			if reopenErr := deps.brAdapter.ReopenBead(context.Background(), deps.intentLogDir, deps.brTimeoutCfg, runID, reopenTID, beadID, dotResult.summary); reopenErr != nil {
				fmt.Fprintf(os.Stderr, "daemon: workloop: ReopenBead (dot) %s: %v\n", beadID, reopenErr)
			}
			emitDone(false, dotResult.summary)
		}
		return

	default:
		// WorkflowModeSingle or any normalised-to-single value: fall through
		// to the single-mode dispatch path below.
	}

	// ─── Single-mode dispatch (production path) ───────────────────────────────

	// Step 1: build the Claude launch spec via buildClaudeLaunchSpec.
	daemonSock := filepath.Join(deps.projectDir, ".harmonik", "daemon.sock")
	// gap #7 bead 2: a REMOTE worker cannot reach box A's local daemon.sock. For
	// remote runs, the implementer agent must dial the worker-side reverse-tunnel
	// TCP endpoint (rbc.workerHookSock, tcp://127.0.0.1:<port>) instead, which the
	// `ssh -N -R` tunnel launched above forwards back to box A's daemon.sock (it is
	// a TCP loopback listener, not a unix socket, so the unprivileged hook user can
	// connect — hk-ege6). resolveAgentDaemonSocket returns
	// rbc.workerHookSock for a remote run and the unchanged box-A daemonSock for a
	// local run (rbc == nil), so local runs remain byte-identical (NFR7). The
	// resolved path flows into rc.daemonSocket → ClaudeEnvVars(HARMONIK_DAEMON_SOCKET).
	var rbcHookSock string
	if rbc != nil {
		rbcHookSock = rbc.workerHookSock
	}
	agentDaemonSock := resolveAgentDaemonSocket(rbcHookSock, daemonSock)
	rc := claudeRunCtx{
		runID:             runID,
		beadID:            string(beadID),
		workspacePath:     wtPath,
		daemonSocket:      agentDaemonSock,
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
		// per HC-055b. Derived from activeRepo (= target repo for cross-repo runs).
		worktreeRootPath: workspace.WorktreeRootPath(activeRepo, workspace.NoWorktreeRootOverride()),
		extraContext:     extraContext, // hk-boiwe: per-item context from queue.Item.Context
		baseBranch:       baseBranch,   // hk-mtm0w: pre-exit rebase target
	}
	// hk-z8ek: for a REMOTE run, thread the worker's SSHRunner into the launch
	// spec so the three materialization writes (.claude/settings.json,
	// .harmonik/agent-task.md, ~/.claude.json trust) land on the WORKER's
	// filesystem — where the worktree actually lives — instead of box A's mirror
	// path. Resolve the hook "command" to the worker's harmonik path too (the
	// hook subprocess runs ON THE WORKER). Nil runner + empty workerBinaryPath
	// for a LOCAL run keeps the materialization byte-identical (NFR7).
	if rbc != nil {
		rc.runner = rbc.sshRunner
		rc.workerBinaryPath = workerHarmonikPath(rbc.worker)
	}
	// Use the pre-built routed specBuilder (set above, before the mode switch).
	// deps.launchSpecBuilder is always non-nil here: the pre-build block ensures it
	// (T12, hk-xhawy). specBuilder is a local alias for clarity.
	specBuilder := deps.launchSpecBuilder
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

	// D2 (fail-closed): refuse to forward ANTHROPIC_API_KEY to a remote worker.
	// A key present in spec.Env for a remote run would bill the worker's own API
	// quota (the 2026-05-30 credential-leak incident). Fail the dispatch rather
	// than silently forwarding it.
	if rbc != nil && hasAPIKeyInEnv(spec.Env) {
		const reason = "remote run: ANTHROPIC_API_KEY in spawn env (D2 fail-closed)"
		fmt.Fprintf(os.Stderr, "daemon: workloop: %s bead %s run %s (reopening)\n",
			reason, beadID, runID.String())
		reopenTID, _ := deps.tidGen.Next()
		_ = deps.brAdapter.ReopenBead(ctx, deps.intentLogDir, deps.brTimeoutCfg, runID, reopenTID, beadID, reason)
		emitDone(false, reason)
		return
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
	//
	// B10: for remote runs pass the SSHRunner so liveness probes (pgrep, ps) and
	// git commit-detect are tunnelled to the worker host instead of executing locally.
	var runSubstrate handler.Substrate = deps.substrate
	var runPasteTarget handler.Substrate = deps.substrate // fallback: shared substrate
	var runRunner tmuxpkg.CommandRunner
	if rbc != nil {
		runRunner = rbc.sshRunner
	}
	if prs := newPerRunSubstrate(deps.substrate, deps.handlerBinary, runRunner); prs != nil {
		// B11: wire the offline callback so mid-run SSH failures emit worker_offline
		// and disable the worker. Nil for local runs (rbc == nil).
		if rbc != nil {
			prs.onConnectionFailure = func(c context.Context, detail string) {
				notifyWorkerOffline("liveness", detail)
			}
			// remote-substrate worker-spawn gap: tell the per-run substrate which
			// tmux session to ENSURE + spawn into ON THE WORKER, and the cwd to use
			// when creating it (the worker's repo_path). Without this the spawn would
			// target box A's local "-default" session — which does not exist on the
			// worker — and the launch wedges at launch_initiated.
			prs.workerSessionName = prs.inner.workerSpawnSessionName(rbc.worker.Name)
			prs.workerSessionCwd = rbc.worker.RepoPath
		}
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
	//
	// hk-4l7zs: launch_initiated is held back and emitted AFTER Launch returns —
	// it must signal that a tmux window actually spawned, not merely that the
	// daemon is about to try (which would mislead operators when SpawnWindow is
	// wedged on a leaked spawn slot).
	implLaunchInitiatedMsg := emitPreExecBeforeLaunch(ctx, deps.bus, runID, artifacts.preExecMsgs)

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
		// hk-4l7zs: a spawn-cap-timeout launch failure is the slot-leak signature.
		// Emit spawn_cap_blocked so operators see WHY the launch failed (pool
		// saturated) instead of an opaque launch-error reopen.
		if errors.Is(launchErr, ErrSpawnCapTimeout) {
			inUse, capSize := substrateSpawnStats(deps.substrate)
			emitSpawnCapBlocked(ctx, deps.bus, runID, time.Since(implementerLaunchedAt), inUse, capSize)
		}
		// hk-r1rup: a tmux-new-window-timeout launch failure is the hung-tmux
		// signature (the no-spawn wedge). Emit tmux_new_window_timeout so operators
		// see WHY the launch failed (tmux new-window did not return) instead of an
		// opaque launch-error reopen.
		if errors.Is(launchErr, ErrTmuxNewWindowTimeout) {
			emitTmuxNewWindowTimeout(ctx, deps.bus, runID, time.Since(implementerLaunchedAt))
		}
		reopenTID, _ := deps.tidGen.Next()
		_ = deps.brAdapter.ReopenBead(ctx, deps.intentLogDir, deps.brTimeoutCfg, runID, reopenTID, beadID,
			fmt.Sprintf("launch error: %v", launchErr))
		emitDone(false, fmt.Sprintf("launch error: %v", launchErr))
		return
	}
	// hk-4l7zs: now that the tmux window has actually spawned (Launch returned a
	// live session), emit the held-back launch_initiated. Emitting it here — not
	// before SpawnWindow — keeps the event truthful when the spawn semaphore is
	// wedged on a leaked slot (in that case Launch returns an error above and
	// launch_initiated is never emitted).
	if implLaunchInitiatedMsg != nil {
		emitPreExecMessage(ctx, deps.bus, runID, implLaunchInitiatedMsg)
	}

	// Store the session's lifecycle Machine in the RunHandle so the stale watcher
	// can read the current state and drive Ready→Failed(silent_hang) before
	// emitting run_stale (SPEC_ACCEPTANCE_GAP fix per hk-xrygh iter-2).
	if handle, ok := deps.runRegistry.Get(runID); ok {
		handle.SetMachine(sess.Machine())
	}

	// hk-68pvl: force-tear-down the implementer session before beadRunOne
	// returns — and therefore before the deferred wtCleanup (registered earlier
	// at the worktree-factory step, so it runs LAST under LIFO defer ordering)
	// removes the worktree. Without this, a ctx-cancel that makes sess.Wait
	// return early (substrate path) lets removeWorktree delete the directory out
	// from under a still-live claude mid-`go test`, producing a false
	// no_commit_during_implementer exit=0. Kill is idempotent, so this is a
	// no-op backstop on the normal exit path where the session already exited.
	defer forceTeardownSession(sess)

	// hk-xnnd: register the implementer identity on the comms bus so peers can
	// attribute escalation messages sent under "<beadID>-impl". Retire on run-end
	// via defer so the leave event fires on every exit path (normal, abort, error).
	emitImplPresence(ctx, deps.bus, beadID, core.AgentPresenceStatusOnline, core.AgentPresenceReasonJoin)
	defer func() {
		emitImplPresence(context.Background(), deps.bus, beadID, core.AgentPresenceStatusOffline, core.AgentPresenceReasonLeave)
	}()

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
			// hk-wths: use EmitWithRunID so the bus envelope carries run_id and the
			// stale watcher's never-spawned reaper sees agentReadySeen = true.
			_ = tap.EmitWithRunID(context.Background(), cbRunID, core.EventTypeAgentReady, nil)
			return
		}
		// hk-wths: use EmitWithRunID so the bus envelope carries run_id and the
		// stale watcher's never-spawned reaper sees agentReadySeen = true.
		_ = tap.EmitWithRunID(context.Background(), cbRunID, core.EventTypeAgentReady, b)
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
	// hk-zlo8: resolve completionMode before the adapter check so it is accessible
	// at the paste-inject gate below (pasteInjectOnLaunch + pasteInjectQuitOnCommit
	// must be skipped for ProcessExit harnesses — same class as hk-f6g7).
	completionMode := handlercontract.CompletionEventStreamThenQuit
	if deps.harnessRegistry != nil {
		if h, hErr := deps.harnessRegistry.ForAgent(artifactAgentType(artifacts)); hErr == nil {
			completionMode = h.Completion()
		}
	}

	adapter, adapterErr := deps.adapterRegistry.ForAgent(artifactAgentType(artifacts))
	if adapterErr != nil {
		// No adapter for the resolved agent type — non-fatal; skip ready-wait.
		fmt.Fprintf(os.Stderr, "daemon: workloop: ForAgent(%s) bead %s: %v (skipping ready-wait)\n",
			artifactAgentType(artifacts), beadID, adapterErr)
	} else {
		// hk-f6g7: skip waitAgentReady for ProcessExit harnesses (codex). These
		// self-terminate on turn completion and never emit agent_ready; calling
		// waitAgentReady unconditionally caused HC-056 timeout in all workflow modes.
		// Spec: specs/harness-contract.md §2 N5.
		// completionMode was resolved above (hk-zlo8) and is used here directly.
		if completionMode != handlercontract.CompletionProcessExit {
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
		// CompletionProcessExit: process self-terminates; fall through directly to
		// waitWithSocketGrace without the agent_ready handshake.
	}

	// Steps 6a/6b: paste-inject — only for interactive TUI harnesses (not ProcessExit).
	// hk-zlo8: CodexHarness (CompletionProcessExit) has no tmux pane; calling
	// pasteInjectOnLaunch causes "WriteLastPane: cant find pane" → no_commit in ~4s.
	// ProcessExit harnesses receive their task via argv (launch spec), not pane paste.
	// Mirrors the existing hk-f6g7 gate above for waitAgentReady.
	//
	// noChangeTimeoutCh is declared unconditionally so the default switch branch at
	// the post-wait select can read it (nil = no watchdog, treated as open channel).
	var noChangeTimeoutCh chan struct{}
	if completionMode != handlercontract.CompletionProcessExit {
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
			handlercontract.ReviewLoopPhase(rc.phase), rc.iterationCount, wtPath,
			deps.bus, runID)

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
		// hk-7srrd: pass a per-run heartbeat channel so pasteInjectQuitOnCommit can
		// track agent_heartbeat events and use heartbeat staleness as the primary
		// kill trigger instead of a fixed wall-clock deadline.
		//
		// hk-37giq: this MUST be an INDEPENDENT subscription (tap.Subscribe()), NOT
		// the same tapCh that waitAgentReady consumes. A Go channel receive is
		// exclusive, so sharing tapCh let waitAgentReady's drain goroutine — which can
		// keep running after readyCancel() until it happens to select ctx.Done() —
		// steal every heartbeat from this watchdog under concurrent dispatch. With the
		// fan-out tap, the watchdog gets its own copy of every event and observes
		// firstHeartbeatSeen, so it advances instead of spinning in the launch-
		// suppression branch forever (launch_stall_detected → run_stale wedge).
		if qs, ok := runPasteTarget.(quitSender); ok {
			noChangeTimeoutCh = make(chan struct{})
			watchdogCh := tap.Subscribe()
			go pasteInjectQuitOnCommit(ctx, qs, sess, wtPath, headSHA, noChangeTimeoutCh, briefDelivered, watchdogCh, deps.bus, runID)
		}
	}

	// Step 7: wait for the watcher to finish (handler exit or ctx cancel) then
	// apply the stop-hook grace window for a pending outcome_emitted payload.
	socketOutcome, ei := waitWithSocketGrace(ctx, deps.hookStore, watcher, sess,
		runID.String(), artifacts.claudeSessionID)

	// hk-0z5x: per-run abort check — fired when the never-spawned reaper in
	// StaleWatcher cancels the per-run context (ctx) because launch_initiated
	// was observed but agent_ready never arrived within NeverSpawnedReaperTimeout.
	//
	// Distinguish from daemon-wide shutdown (where ctx is also cancelled but
	// handle.aborted is NOT set): check handle.aborted before treating this as
	// a per-run abort. Daemon shutdown falls through to the existing ctx.Err()
	// check in the no-commit path (line ~3441) which leaves the item 'dispatched'
	// for QM-002a recovery.
	if ctx.Err() != nil {
		if handle, ok := deps.runRegistry.Get(runID); ok && handle.aborted.Load() {
			const abortReason = "never_spawned_reaper: launch_initiated but agent_ready not received within deadline"
			reopenTID, _ := deps.tidGen.Next()
			_ = deps.brAdapter.ReopenBead(context.Background(), deps.intentLogDir, deps.brTimeoutCfg, runID, reopenTID, beadID, abortReason)
			emitRunCompleted(context.Background(), deps.bus, runID, string(beadID), owningEpicID, owningEpicAssignee, false, abortReason, queueID, queueGroupIndex, nil)
			if runSucceeded != nil {
				*runSucceeded = false
			}
			return
		}
		// ctx cancelled for other reasons (daemon shutdown) — fall through to the
		// existing daemon-shutdown handling in the no-commit path.
	}

	// HC-065: Drive StateTerminating → StateTerminated/StateFailed transitions.
	// The session has exited (waitWithSocketGrace returned). Attempt to advance
	// the Machine through Terminating to a terminal state. Transitions that are
	// invalid for the current state (e.g. machine already in StateFailed from
	// agent_failed) are silently ignored.
	transitionToTerminated(context.Background(), sess.Machine(), runID, deps.bus,
		ei.exitCode, ei.waitErr)

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
	// REMOTE: route via runRunner so HEAD is read from the worker (nil ⇒ box-A-local).
	{
		curHead, _ := resolveWorktreeHEADVia(ctx, runRunner, wtPath)
		commitLanded := curHead != "" && curHead != headSHA
		emitImplementerPhaseComplete(ctx, deps.bus, runID, ei.exitCode, ei.stderrTail,
			commitLanded, time.Since(implementerLaunchedAt))
	}

	// ── Codex daemon-side commit fallback (hk-gd9r) ───────────────────────
	//
	// codex --sandbox workspace-write blocks writes to .git. In a git worktree
	// the .git entry is a FILE pointing to the MAIN repo's
	// .git/worktrees/<run-id>/ directory, which lies OUTSIDE the sandbox
	// workspace root, so codex self-commit fails 100% of the time
	// (fatal: Unable to create .git/index.lock). The daemon runs git OUTSIDE
	// the sandbox; after the codex process exits we call ensureCodexRefsTrailer
	// to stage+commit any worktree changes codex produced but could not commit.
	//
	// The three-way decision table (see codexcommit.go):
	//   • HEAD already carries "Refs: <beadID>" → no-op (codex self-committed).
	//   • HEAD advanced but lacks the trailer → amend HEAD to add it.
	//   • HEAD unchanged, worktree dirty → stage all + create trailer commit.
	//   • HEAD unchanged, worktree clean → no_change (fall through to guard).
	//
	// Fires only for CompletionProcessExit harnesses (codex). claude runs through
	// the interactive TUI and self-commits; this block is a no-op for claude.
	// On error we log and fall through to the no-commit guard which reopens.
	if deps.harnessRegistry != nil {
		if h, hErr := deps.harnessRegistry.ForAgent(artifactAgentType(artifacts)); hErr == nil &&
			h.Completion() == handlercontract.CompletionProcessExit {
			outcome, ensureErr := ensureCodexRefsTrailer(ctx, runRunner, wtPath, headSHA, beadID)
			if ensureErr != nil {
				fmt.Fprintf(os.Stderr, "daemon: workloop: ensureCodexRefsTrailer bead %s: %v (falling through to no-commit guard)\n",
					beadID, ensureErr)
			} else {
				fmt.Fprintf(os.Stderr, "daemon: workloop: ensureCodexRefsTrailer bead %s: %s\n",
					beadID, outcome)
			}
		}
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
	// Bead: hk-6zylj, hk-zguy6.
	//
	// hk-zguy6: hold mergeMu during the escape check so that a sibling's
	// update-ref → reset-hard sequence cannot race with this check. The merge
	// sequence (rebase → update-ref → push → reset-hard) is entirely under
	// mergeMu, so when we hold the lock no sibling can be in a transient dirty
	// state. This makes the check race-free without any path-exclusion heuristic.
	//
	// Bead: hk-6zylj, hk-zguy6, hk-xux36.
	if deps.mergeMu != nil {
		deps.mergeMu.Lock()
	}
	mainDirty, dirtyFiles, escapeErr := checkMainWorkingTreeDirty(ctx, activeRepo, preRunUntracked)
	if deps.mergeMu != nil {
		deps.mergeMu.Unlock()
	}
	if escapeErr == nil && mainDirty {
		emitImplementerEscapedWorktree(ctx, deps.bus, runID, beadID, activeRepo, dirtyFiles)
		failReason := fmt.Sprintf("implementer_escaped_worktree: %d file(s) dirty in main: %s",
			len(dirtyFiles), strings.Join(dirtyFiles, ", "))
		if reopenErr := deps.brAdapter.ReopenBead(ctx, deps.intentLogDir, deps.brTimeoutCfg, runID, transitionTID, beadID, failReason); reopenErr != nil {
			fmt.Fprintf(os.Stderr, "daemon: workloop: ReopenBead FAILED (escaped_worktree) bead %s run %s: %v — bead is stuck in_progress; operator must reopen manually (hk-s20z)\n",
				beadID, runID.String(), reopenErr)
		}
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
	//
	// REMOTE: route the worktree-HEAD probe via runRunner so the no-commit guard
	// reads the WORKER's run-branch HEAD (nil runRunner ⇒ box-A-local, NFR7). The
	// noCommitGuardShouldReopen checks if THIS bead's code landed in the target
	// repo's main branch (cross-repo: activeRepo; local: deps.projectDir).
	if curHeadSHA, curHeadErr := resolveWorktreeHEADVia(ctx, runRunner, wtPath); curHeadErr == nil &&
		noCommitGuardShouldReopen(ctx, activeRepo, curHeadSHA, headSHA, beadID) {
		// hk-4ie1z: the implementer's worktree HEAD never advanced past the
		// parent (NO commit) AND this bead's own work is not on main. The prior
		// escape hatch (hk-cwxow) bypassed the guard whenever refs/heads/main had
		// moved at all — but under concurrent/wave dispatch a SIBLING bead merging
		// to main satisfies "main moved" while THIS bead's code is still absent,
		// so a genuine no-commit run was falsely closed as success (hk-tigaf.4).
		// The positive per-bead Refs-trailer check inside noCommitGuardShouldReopen
		// replaces "did main move?" with "did THIS bead land?". Removing the escape
		// does NOT reintroduce the hk-cwxow false-`non_ff`: mergeRunBranchToMain
		// independently short-circuits to noChange when runTip == headSHA
		// (workloop.go ~2804), regardless of where main points. This mirrors the
		// review-loop no-commit guard (reviewloop.go ~567), which never had the
		// escape.
		failReason := fmt.Sprintf("no_commit_during_implementer: HEAD did not advance past parent %s at iteration 1 exit=%d", headSHA, ei.exitCode)
		if reopenErr := deps.brAdapter.ReopenBead(ctx, deps.intentLogDir, deps.brTimeoutCfg, runID, transitionTID, beadID, failReason); reopenErr != nil {
			fmt.Fprintf(os.Stderr, "daemon: workloop: ReopenBead FAILED (no_commit) bead %s run %s: %v — bead is stuck in_progress; operator must reopen manually (hk-s20z)\n",
				beadID, runID.String(), reopenErr)
		}
		emitDone(false, failReason)
		return
	}

	switch {
	case term.Type == handlercontract.ProgressMsgTypeAgentCompleted:
		// CHB-020 branch 1: stop-hook WORK_COMPLETE or REVIEWER_VERDICT.
		// Scenario gate (hk-i2ie5): block merge when scenario tests go RED.
		// REMOTE: route via runRunner so the gate runs on the worker (nil ⇒ local).
		if sgr := runScenarioGateIfNeededVia(ctx, runRunner, wtPath, headSHA); sgr.blocked {
			emitOutcomeEmitted(ctx, deps.bus, runID, beadID, "rejected", sgr.reason)
			reopenTID, _ := deps.tidGen.Next()
			_ = deps.brAdapter.ReopenBead(ctx, deps.intentLogDir, deps.brTimeoutCfg, runID, reopenTID, beadID, sgr.reason)
			emitDone(false, sgr.reason)
			return
		}
		// §4.12.EM-052: merge run-branch to main before CloseBead.
		// DD1 code-sync (remote-substrate B8): push-branch + box-A-fetch BEFORE merge.
		if syncReason := preMergeSync(); syncReason != "" {
			emitOutcomeEmitted(ctx, deps.bus, runID, beadID, "rejected", syncReason)
			reopenTID, _ := deps.tidGen.Next()
			_ = deps.brAdapter.ReopenBead(ctx, deps.intentLogDir, deps.brTimeoutCfg, runID, reopenTID, beadID,
				fmt.Sprintf("code-sync failed (agent_completed): %s", syncReason))
			emitDone(false, fmt.Sprintf("code-sync-failed (agent_completed): %s", syncReason))
			return
		}
		mergeRes := lockedMergeRunBranchToMain(ctx, deps.mergeMu, activeRepo, runID, deps.bus, beadID, headSHA, deps.targetBranch, effectiveMergeProtectBranches)
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
			if closeErr := deps.closeBeadWithHistoryTrim(ctx, runID, transitionTID, beadID, false); closeErr != nil {
				fmt.Fprintf(os.Stderr, "daemon: workloop: CloseBead (agent_completed) %s: %v\n", beadID, closeErr)
				// hk-hypbi: transient BrUnavailable after successful merge → emit success.
				if errors.Is(closeErr, brcli.BrUnavailable) {
					emitDone(true, "close-transient-merged (agent_completed)")
				} else {
					emitDone(false, fmt.Sprintf("close-error: %v", closeErr))
				}
			} else {
				emitBeadClosedAndMaybeEpic(ctx, deps, runID, beadID)
				emitDone(true, "agent_completed: stop-hook outcome")
			}
		}

	case socketOutcome == nil && ei.exitCode == exitCodeClean && !watcherFailed:
		// No stop-hook arrived AND handler exited 0 without watcher error.
		// Fall back to the pre-bridge close-on-exit-0 heuristic for
		// MVH twin-blind runs.
		//
		// hk-wfbxf: same CloseBead error handling as branch 1.
		// Scenario gate (hk-i2ie5): block merge when scenario tests go RED.
		// REMOTE: route via runRunner so the gate runs on the worker (nil ⇒ local).
		if sgr := runScenarioGateIfNeededVia(ctx, runRunner, wtPath, headSHA); sgr.blocked {
			emitOutcomeEmitted(ctx, deps.bus, runID, beadID, "rejected", sgr.reason)
			reopenTID, _ := deps.tidGen.Next()
			_ = deps.brAdapter.ReopenBead(ctx, deps.intentLogDir, deps.brTimeoutCfg, runID, reopenTID, beadID, sgr.reason)
			emitDone(false, sgr.reason)
			return
		}
		// §4.12.EM-052: merge run-branch to main before CloseBead.
		// DD1 code-sync (remote-substrate B8): push-branch + box-A-fetch BEFORE merge.
		if syncReason := preMergeSync(); syncReason != "" {
			emitOutcomeEmitted(ctx, deps.bus, runID, beadID, "rejected", syncReason)
			reopenTID, _ := deps.tidGen.Next()
			_ = deps.brAdapter.ReopenBead(ctx, deps.intentLogDir, deps.brTimeoutCfg, runID, reopenTID, beadID,
				fmt.Sprintf("code-sync failed (auto-close): %s", syncReason))
			emitDone(false, fmt.Sprintf("code-sync-failed (auto-close): %s", syncReason))
			return
		}
		mergeRes := lockedMergeRunBranchToMain(ctx, deps.mergeMu, activeRepo, runID, deps.bus, beadID, headSHA, deps.targetBranch, effectiveMergeProtectBranches)
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
			if closeErr := deps.closeBeadWithHistoryTrim(ctx, runID, transitionTID, beadID, false); closeErr != nil {
				fmt.Fprintf(os.Stderr, "daemon: workloop: CloseBead %s: %v\n", beadID, closeErr)
				if errors.Is(closeErr, brcli.BrUnavailable) {
					emitDone(true, "close-transient-merged (auto-close)")
				} else {
					emitDone(false, fmt.Sprintf("close-error: %v", closeErr))
				}
			} else {
				emitBeadClosedAndMaybeEpic(ctx, deps, runID, beadID)
				emitDone(true, "auto-close: exit=0")
			}
		}

	default:
		// noChange-timeout path (hk-trjef): pasteInjectQuitOnCommit killed the
		// session after commitPollTimeout fired without a new commit.  Check whether
		// the bead was already subsumed by a prior run that landed on main.
		select {
		case <-noChangeTimeoutCh:
			if beadAlreadySubsumedInMain(ctx, activeRepo, beadID) {
				emitOutcomeEmitted(ctx, deps.bus, runID, beadID, "approved", "")
				if closeErr := deps.closeBeadWithHistoryTrim(ctx, runID, transitionTID, beadID, false); closeErr != nil {
					fmt.Fprintf(os.Stderr, "daemon: workloop: CloseBead (noChange-subsumed) %s: %v\n", beadID, closeErr)
					if errors.Is(closeErr, brcli.BrUnavailable) {
						emitDone(true, "close-transient-merged (noChange-subsumed)")
					} else {
						emitDone(false, fmt.Sprintf("close-error: %v", closeErr))
					}
				} else {
					emitBeadClosedAndMaybeEpic(ctx, deps, runID, beadID)
					emitDone(true, "noChange-subsumed: bead found in main")
				}
			} else {
				if reopenErr := deps.brAdapter.ReopenBead(ctx, deps.intentLogDir, deps.brTimeoutCfg, runID, transitionTID, beadID, "noChange-timeout"); reopenErr != nil {
					fmt.Fprintf(os.Stderr, "daemon: workloop: ReopenBead FAILED (noChange-timeout) bead %s run %s: %v — bead is stuck in_progress; operator must reopen manually (hk-1h5q)\n",
						beadID, runID.String(), reopenErr)
				}
				emitDone(false, "noChange-timeout: no commit in commitPollTimeout window")
			}
		default:
			// hk-ly0hg Fix-1: context-cancel path — daemon is shutting down.
			if ctx.Err() != nil {
				// hk-dnrg: drain committed-but-unmerged runs on shutdown instead of
				// abandoning. A run that already committed in its worktree must be
				// merged before exit — abandoning it causes re-dispatch (wasted or
				// duplicated work) on next boot. Use background context so shutdown
				// does not abort the merge sequence.
				bgCtx := context.Background()
				if curHeadSHA, headErr := resolveWorktreeHEAD(bgCtx, wtPath); headErr == nil && curHeadSHA != "" && curHeadSHA != headSHA {
					mergeRes := lockedMergeRunBranchToMain(bgCtx, deps.mergeMu, activeRepo, runID, deps.bus, beadID, headSHA, deps.targetBranch, effectiveMergeProtectBranches)
					if mergeRes.success || mergeRes.noChange {
						drainTID, _ := deps.tidGen.Next()
						if closeErr := deps.closeBeadWithHistoryTrim(bgCtx, runID, drainTID, beadID, false); closeErr != nil {
							fmt.Fprintf(os.Stderr, "daemon: workloop: CloseBead (shutdown-drain) %s: %v\n", beadID, closeErr)
							if errors.Is(closeErr, brcli.BrUnavailable) {
								emitRunCompleted(bgCtx, deps.bus, runID, string(beadID), owningEpicID, owningEpicAssignee, true, "close-transient-merged (shutdown-drain)", queueID, queueGroupIndex, runTipSHA)
							} else {
								emitRunCompleted(bgCtx, deps.bus, runID, string(beadID), owningEpicID, owningEpicAssignee, false, fmt.Sprintf("close-error (shutdown-drain): %v", closeErr), queueID, queueGroupIndex, runTipSHA)
							}
						} else {
							emitBeadClosedAndMaybeEpic(bgCtx, deps, runID, beadID)
							emitRunCompleted(bgCtx, deps.bus, runID, string(beadID), owningEpicID, owningEpicAssignee, true, "shutdown-drain: committed work merged", queueID, queueGroupIndex, runTipSHA)
						}
						return
					}
					// Merge failed: log and fall through to reopen.
					fmt.Fprintf(os.Stderr, "daemon: workloop: shutdown-drain: merge failed for bead %s: %s; reopening for re-dispatch\n", beadID, mergeRes.reason)
				}
				// No commit in worktree (or HEAD check failed): reopen for re-dispatch.
				// QM-002a at next startup reverts the queue item to pending once it
				// sees the bead is open again.
				reopenTID, _ := deps.tidGen.Next()
				if reopenErr := deps.brAdapter.ReopenBead(bgCtx, deps.intentLogDir, deps.brTimeoutCfg, runID, reopenTID, beadID,
					"context_cancelled: daemon shutdown, requeue pending"); reopenErr != nil {
					fmt.Fprintf(os.Stderr, "daemon: workloop: ReopenBead FAILED (context_cancelled) bead %s run %s: %v — bead is stuck in_progress; operator must reopen manually (hk-1h5q)\n",
						beadID, runID.String(), reopenErr)
				}
				return
			}

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
			if reopenErr := deps.brAdapter.ReopenBead(ctx, deps.intentLogDir, deps.brTimeoutCfg, runID, transitionTID, beadID, failReason); reopenErr != nil {
				fmt.Fprintf(os.Stderr, "daemon: workloop: ReopenBead FAILED (run-failed) bead %s run %s: %v — bead is stuck in_progress; operator must reopen manually (hk-s20z)\n",
					beadID, runID.String(), reopenErr)
			}
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

// noCommitGuardShouldReopen decides whether the single-mode no-commit guard
// must fail the run as `no_commit` and reopen the bead.
//
// It returns true when BOTH:
//   - the implementer's worktree HEAD never advanced past the parent
//     (curHeadSHA == parentSHA → no commit was produced), AND
//   - THIS bead's own work is not already on main (no `Refs: <beadID>` trailer
//     in the recent main history).
//
// This is the positive per-bead replacement for the buggy hk-cwxow
// `mainAdvanced` escape, which asked "did refs/heads/main move at all?" — a
// question that a SIBLING bead landing concurrently answers `true` even though
// THIS bead's code never landed, falsely closing a genuine no-commit run as
// success (hk-4ie1z, observed live on hk-tigaf.4). The only legitimate
// fall-through (the run made no commit, but the bead's work is genuinely on
// main because a prior run subsumed it) is preserved via
// beadAlreadySubsumedInMain. Mirrors the review-loop guard
// (reviewloop.go ~567), which compares HEAD == parentSHA with no escape.
//
// Bead: hk-4ie1z.
func noCommitGuardShouldReopen(ctx context.Context, projectDir, curHeadSHA, parentSHA string, beadID core.BeadID) bool {
	if curHeadSHA != parentSHA {
		// The implementer advanced HEAD — a commit exists; the guard does not fire.
		return false
	}
	// No commit. Fail (reopen) UNLESS this bead's own work is already on main.
	return !beadAlreadySubsumedInMain(ctx, projectDir, beadID)
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
	// hk-ly0hg: use --grep to pre-filter across the full main history rather
	// than reading a fixed window of -20 commits. This prevents false negatives
	// when a restart-interrupted run had its commit land >20 commits ago.
	//
	// --fixed-strings prevents regex interpretation of bead IDs.
	// The line-exact check in Go prevents "Refs: hk-foo.1" from matching a
	// commit whose message contains "Refs: hk-foo.10".
	needle := "Refs: " + string(beadID)
	cmd := exec.CommandContext(ctx, "git", "log", "main", "--format=%B",
		"--fixed-strings", "--grep", needle)
	cmd.Dir = projectDir
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(out), "\n") {
		if strings.TrimRight(line, "\r") == needle {
			return true
		}
	}
	return false
}

// beadExplicitlyReopened returns true when the bead's audit log contains a
// "status_changed" event whose OldValue is "closed", indicating the bead was
// properly closed (not crash-interrupted) and then intentionally reopened.
//
// The pre-dispatch subsume block that called this function was removed by
// hk-f38n (bare Refs-grep false-positives on partial/multi-aspect commits).
// The function is retained for its test coverage (predispatch_reopen_hkwcv_test.go
// via ExportedBeadExplicitlyReopened) and as a building block if a safer
// pre-dispatch check is introduced in the future.
//
// Bead ref: hk-wcv.
func beadExplicitlyReopened(ctx context.Context, auditLogger func(context.Context, core.BeadID) ([]brcli.AuditEvent, error), beadID core.BeadID) bool {
	if auditLogger == nil {
		return false
	}
	events, err := auditLogger(ctx, beadID)
	if err != nil {
		fmt.Fprintf(os.Stderr,
			"daemon: workloop: beadExplicitlyReopened: AuditLog %s: %v (conservative: not bypassing pre-dispatch close)\n",
			beadID, err)
		return false
	}
	for _, e := range events {
		if e.EventType == "status_changed" && e.OldValue == "closed" {
			return true
		}
	}
	return false
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

// drainCancelledQueue transitions all active queues (if any) to
// QueueStatusCancelled and archives their files so that the next harmonik run
// invocation can proceed without the QM-027 "already active" guard blocking it.
//
// Prior to hk-u6m4l this function only drained the "main" queue via the
// backward-compat lq.Queue() shim, leaving named queues (e.g. "cp") on disk
// with status=active after shutdown. The fix iterates all queues in the store
// via AllQueues() so every active named queue is archived on exit.
//
// This is called on every clean exit of runWorkLoop — when ctx is cancelled due
// to SIGINT, SIGTERM, or a timeout — after wg.Wait() ensures all in-flight
// goroutines have completed. The function is a no-op when:
//   - deps.queueStore is nil (no queue surface in use).
//   - All in-memory queues are nil or already in a terminal state
//     (paused-by-failure, completed, cancelled) — evaluateGroupAdvanceWithOutcome
//     already transitioned them.
//
// Uses context.Background() because ctx is always cancelled by the time this
// runs; queue.CancelQueueOnShutdown needs a non-cancelled context for Persist.
//
// Errors are logged to stderr but do not block shutdown; other queues are still
// drained even if one fails.
//
// Spec ref: specs/queue-model.md §8 (shutdown drain).
// Bead ref: hk-ppt32, hk-u6m4l.
func drainCancelledQueue(ctx context.Context, deps workLoopDeps) {
	if deps.queueStore == nil {
		return
	}
	// Snapshot all queues under the read lock. drainCancelledQueue is called
	// after wg.Wait() so there are no concurrent mutations; AllQueues is safe
	// here and avoids holding the write lock across I/O.
	snapshot := deps.queueStore.AllQueues()
	for name, q := range snapshot {
		if q == nil || q.Status != queue.QueueStatusActive {
			continue
		}
		// Queue is still active: transition to cancelled and archive.
		if err := queue.CancelQueueOnShutdown(ctx, deps.projectDir, q); err != nil {
			fmt.Fprintf(os.Stderr, "daemon: workloop: drainCancelledQueue queueID=%s name=%q: %v\n",
				q.QueueID, name, err)
			// Continue draining other queues even if one fails.
		}
		// Clear in-memory state for this queue.
		deps.queueStore.ClearQueueByName(name)
	}
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

// workloopIdleWait blocks indefinitely until a queue-submit wake signal
// arrives on wakeC or ctx is cancelled. Unlike workloopSleep there is no
// periodic re-query timer — the daemon waits without spinning until the
// socket layer delivers a signal. Used for queue-loaded idle states per
// PL-013 (retired-with-stub): idle (queue absent/completed/paused or no
// eligible items) MUST NOT busy-poll; it MUST block until queue-submit or
// shutdown.
//
// wakeC may be nil (e.g. in tests that do not wire QueueStore.WakeCh):
// receive from a nil channel blocks forever, so the function degrades to
// waiting only for ctx cancellation.
//
// Bead ref: hk-dji5z (T71).
func workloopIdleWait(ctx context.Context, wakeC <-chan struct{}) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-wakeC:
		return nil
	}
}

// scheduleAwareIdleWait is the schedule-aware variant of workloopIdleWait used
// at the queues-loaded-but-idle dispatch point. When a schedule with at least
// one enabled job is loaded the daemon MUST re-tick periodically to fire due
// jobs, so this bounds the wait by workloopPollInterval (and selects on the
// schedule wake channel so a CLI mutation wakes the loop immediately). When no
// schedule is armed it degrades to the indefinite block of workloopIdleWait,
// preserving the no-busy-poll idle contract (PL-013).
//
// Bead ref: hk-0es.
func scheduleAwareIdleWait(ctx context.Context, deps workLoopDeps) error {
	if deps.scheduleStore == nil || !hasEnabledScheduledJob(deps.scheduleStore) {
		return workloopIdleWait(ctx, deps.submitWakeC)
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(workloopPollInterval):
		return nil
	case <-deps.submitWakeC:
		return nil
	case <-deps.scheduleWakeC:
		return nil
	}
}

// hasEnabledScheduledJob reports whether the store holds at least one enabled
// job. Used to decide whether the idle wait must be bounded.
func hasEnabledScheduledJob(s *schedule.Store) bool {
	for _, j := range s.List() {
		if j.Enabled {
			return true
		}
	}
	return false
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

// forceTeardownSession force-terminates sess and blocks until the hosted
// process has been reaped, using a non-cancellable background context.
//
// This is the load-bearing guard for hk-68pvl: the worktree-removal cleanup
// (removeWorktree, run via the deferred wtCleanup in beadRunOne) must NEVER
// delete the worktree directory while the implementer/reviewer claude is still
// live inside it. On the tmux substrate path, tmuxSubstrateSession.Wait returns
// ctx.Err() the instant the run ctx is cancelled even though the hosted process
// may still be alive (runWait keeps polling in the background); the subsequent
// `git worktree remove --force` then races a live `go test`, the agent's
// `git add`/commit lands in a deleted directory, and the run is recorded as a
// false `no_commit_during_implementer ... exit=0`.
//
// sess.Kill blocks until the process group is terminated on both paths:
//   - substrate: killProcessWithGrace (SIGTERM → grace poll → SIGKILL) then
//     KillWindow — synchronous, idempotent via killOnce.
//   - exec: SIGTERM the process group, then await reap (escalating to SIGKILL
//     on the background ctx, which never expires, so it waits for exit).
//
// Kill is safe to call more than once (idempotent on substrate; harmless ESRCH
// on exec). Callers register this as a deferred backstop immediately after
// Launch so EVERY return path (success, failure, early error, ctx-cancel) tears
// the session down before the function returns — and therefore before the
// beadRunOne-level deferred wtCleanup runs.
//
// Bead: hk-68pvl.
func forceTeardownSession(sess handler.Session) {
	if sess == nil {
		return
	}
	_ = sess.Kill(context.Background())
}

// removeWorktree removes the git worktree at wtPath and prunes stale metadata
// from the repository at repoRoot. It uses `git worktree remove --force` twice
// to handle locked worktrees (the second --force overrides the lock).
//
// Errors are non-fatal: the work loop continues even if cleanup fails (orphan
// sweep at next startup will recover stale worktrees per PL-006).
//
// hk-68pvl: the caller (beadRunOne via the deferred wtCleanup) MUST ensure the
// run's implementer/reviewer session has been force-torn-down
// (forceTeardownSession) before this runs, so the directory is never deleted
// out from under a live agent mid-`go test`.
func removeWorktree(ctx context.Context, repoRoot, wtPath string) {
	cmd := exec.CommandContext(ctx, "git", "worktree", "remove", "--force", "--force", wtPath)
	cmd.Dir = repoRoot
	_ = cmd.Run()

	// hk-bfvby: GC the per-worktree trust key from ~/.claude.json. harmonik
	// creates one ephemeral worktree per bead and never reuses the path, so
	// without this the trust "projects" map grows unbounded (observed 36.6k
	// leaked keys / 8.6MB bloat that, with the per-call rewrite, produced the
	// ~16-min spawn stall). Best-effort: cleanup failure is non-fatal — the
	// bounded lock inside PruneWorktreeTrust ensures it can never wedge the loop.
	_ = workspace.PruneWorktreeTrust(wtPath)
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

// preExecMsgType extracts the "type" field of a pre-exec message, or "" on
// parse failure.
func preExecMsgType(msg json.RawMessage) string {
	var envelope struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(msg, &envelope); err == nil {
		return envelope.Type
	}
	return ""
}

// emitPreExecBeforeLaunch emits every pre-exec message EXCEPT launch_initiated
// and returns the launch_initiated message (if any) for the caller to emit
// AFTER SpawnWindow/Launch returns.
//
// hk-4l7zs: launch_initiated previously fired BEFORE SpawnWindow. When the spawn
// semaphore was wedged (a leaked slot), SpawnWindow blocked indefinitely yet the
// daemon had already emitted launch_initiated — so operators (and the stale
// watcher) saw a "launched" run that had, in fact, never spawned a tmux window.
// Deferring launch_initiated until the window is actually live makes the event
// mean what it says and lets launch_stall_detected fire correctly when the spawn
// is wedged. Ordering of the other pre-exec messages is preserved.
func emitPreExecBeforeLaunch(ctx context.Context, bus handlercontract.EventEmitter, runID core.RunID, msgs []json.RawMessage) (launchInitiated json.RawMessage) {
	for _, msg := range msgs {
		if preExecMsgType(msg) == string(core.EventTypeLaunchInitiated) {
			launchInitiated = msg
			continue
		}
		emitPreExecMessage(ctx, bus, runID, msg)
	}
	return launchInitiated
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
	// WorkerName and WorkerOS are non-empty for remote runs (FR13); empty for local.
	WorkerName string `json:"worker_name,omitempty"`
	WorkerOS   string `json:"worker_os,omitempty"`
	// WorkflowMode is the resolved workflow mode for this run (hk-y3o51).
	WorkflowMode string `json:"workflow_mode,omitempty"`
}

// workloopRunCompletedPayload is the minimal run_completed / run_failed payload
// emitted by the work loop.
//
// QueueID and QueueGroupIndex are optional: set when the run was dispatched
// from a queue submission per QM-011 / QM-012 (EM-015b).
//
// WorktreeTipSHA is set on run_failed when the implementer's HEAD advanced past
// the parent (a commit was produced) but the run still failed — e.g. when the
// commit_gate bounced a valid commit into a no-progress loop. Operators can use
// this SHA to salvage the committed work from the stranded run branch (hk-8b35c).
//
// BeadID, OwningEpicID, and OwningEpicAssignee are denormalized attribution fields
// (hk-7evda, logmine F13) that eliminate captain br round-trips after observing a
// terminal event.
type workloopRunCompletedPayload struct {
	RunID              string  `json:"run_id"`
	BeadID             string  `json:"bead_id"`
	Success            bool    `json:"success"`
	Summary            string  `json:"summary"`
	EndedAt            string  `json:"ended_at"`
	OwningEpicID       *string `json:"owning_epic_id,omitempty"`
	OwningEpicAssignee *string `json:"owning_epic_assignee,omitempty"`
	QueueID            *string `json:"queue_id,omitempty"`
	QueueGroupIndex    *int    `json:"queue_group_index,omitempty"`
	WorktreeTipSHA     *string `json:"worktree_tip_sha,omitempty"`
}

// hasAPIKeyInEnv reports whether any element of env would forward a *live*
// ANTHROPIC_API_KEY to a remote worker. Used by the D2 fail-closed check to
// prevent billing the worker's own API quota (B10).
//
// Two forms are dangerous and must be refused:
//   - "ANTHROPIC_API_KEY=<value>" with a non-empty value — an explicit live key.
//   - "ANTHROPIC_API_KEY" (bare, no '=') — inherits the daemon process's value.
//
// The empty-override form "ANTHROPIC_API_KEY=" (value after '=' is empty) is
// SAFE and must NOT be refused: ClaudeEnvVars always appends it as a CI-003
// credential-zeroing override (specs/credential-isolation.md §4 CI-003), so it
// appears in spec.Env for *every* launch, local or remote. Treating that empty
// override as a live key would fail-close every remote dispatch (the B12
// localhost e2e surfaced exactly this).
func hasAPIKeyInEnv(env []string) bool {
	for _, e := range env {
		if e == "ANTHROPIC_API_KEY" {
			// Bare key with no '=' → inherits the parent process value. Refuse.
			return true
		}
		if v, ok := strings.CutPrefix(e, "ANTHROPIC_API_KEY="); ok && v != "" {
			// KEY=<non-empty> → a live key. Refuse. KEY= (empty) is the CI-003
			// zeroing override and is safe to forward.
			return true
		}
	}
	return false
}

func emitRunStarted(ctx context.Context, bus handlercontract.EventEmitter, runID core.RunID, beadID core.BeadID, wtPath string, queueID *string, queueGroupIndex *int, workerName, workerOS, workflowMode string) {
	pl := workloopRunStartedPayload{
		RunID:           runID.String(),
		BeadID:          string(beadID),
		WorkspacePath:   wtPath,
		StartedAt:       time.Now().UTC().Format(time.RFC3339),
		QueueID:         queueID,
		QueueGroupIndex: queueGroupIndex,
		WorkerName:      workerName,
		WorkerOS:        workerOS,
		WorkflowMode:    workflowMode,
	}
	b, err := json.Marshal(pl)
	if err != nil {
		return
	}
	_ = bus.EmitWithRunID(ctx, runID, core.EventTypeRunStarted, b)
}

func emitRunCompleted(ctx context.Context, bus handlercontract.EventEmitter, runID core.RunID, beadID, owningEpicID, owningEpicAssignee string, success bool, summary string, queueID *string, queueGroupIndex *int, worktreeTipSHA *string) {
	var epicIDPtr, epicAssigneePtr *string
	if owningEpicID != "" {
		epicIDPtr = &owningEpicID
	}
	if owningEpicAssignee != "" {
		epicAssigneePtr = &owningEpicAssignee
	}
	pl := workloopRunCompletedPayload{
		RunID:              runID.String(),
		BeadID:             beadID,
		Success:            success,
		Summary:            summary,
		EndedAt:            time.Now().UTC().Format(time.RFC3339),
		OwningEpicID:       epicIDPtr,
		OwningEpicAssignee: epicAssigneePtr,
		QueueID:            queueID,
		QueueGroupIndex:    queueGroupIndex,
		WorktreeTipSHA:     worktreeTipSHA,
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

// emitImplPresence emits an agent_presence event for a daemon-spawned implementer
// so that peers on the comms bus can attribute and route messages from the identity
// "<beadID>-impl" (hk-xnnd). Errors are silently swallowed — presence is
// best-effort and must not gate the run lifecycle.
func emitImplPresence(ctx context.Context, bus handlercontract.EventEmitter, beadID core.BeadID, status core.AgentPresenceStatus, reason core.AgentPresenceReason) {
	pl := core.AgentPresencePayload{
		Agent:    string(beadID) + "-impl",
		Status:   status,
		LastSeen: time.Now().UTC().Format(time.RFC3339),
		Reason:   reason,
	}
	b, err := json.Marshal(pl)
	if err != nil {
		return
	}
	_ = bus.Emit(ctx, core.EventType("agent_presence"), b)
}

// resolveOwningEpicFromRecord scans beadRecord.Edges for a parent-child edge
// where the bead is the child and returns the parent epic's bead ID and assignee.
// Returns ("", "") when no parent-child edge is found.
// Returns (epicID, "") when the epic exists but the br show call fails or the
// epic has no assignee. Best-effort: errors are silently swallowed.
// Bead ref: hk-7evda (logmine F13 — kill attribution round-trips).
func resolveOwningEpicFromRecord(ctx context.Context, br beadLedger, record core.BeadRecord) (epicID, assignee string) {
	for _, e := range record.Edges {
		if e.EdgeKind == core.EdgeKindParentChild && e.FromBeadID == record.BeadID {
			epicID = string(e.ToBeadID)
			break
		}
	}
	if epicID == "" {
		return "", ""
	}
	epicRecord, err := br.ShowBead(ctx, core.BeadID(epicID))
	if err != nil {
		return epicID, ""
	}
	return epicID, epicRecord.Assignee
}

// ─────────────────────────────────────────────────────────────────────────────
// activateFirstPendingGroup — bootstrap the first group of a freshly-submitted
// or freshly-loaded queue (hk-veoht).
//
// When a queue is submitted via `harmonik queue submit` (or loaded on boot),
// its first group is persisted GroupStatusPending. The only other caller of
// AdvanceGroup is evaluateGroupAdvanceWithOutcome, which fires on a PRIOR run's
// completion — so absent any prior run, group 0 never transitions
// pending → active and the work loop idle-waits forever (the "no active group"
// branch). This helper closes that gap: when no group is active, it finds the
// lowest-index non-terminal pending group and advances it pending → active
// under the queue write lock, persists, and emits the resulting
// queue_group_started event — matching evaluateGroupAdvanceWithOutcome's
// persist-before-emit (QM-063) idiom exactly.
//
// Returns true when a group was activated (the caller should re-evaluate the
// loop so it now finds an active group and dispatches its eligible items),
// false otherwise (no pending group, or AdvanceGroup did not transition — e.g.
// the queue is not active, so advancePending is a no-op per QM-031).
//
// Idempotency / safety:
//   - Only the FIRST pending group is advanced; subsequent pending groups
//     advance on completion as before (evaluateGroupAdvanceWithOutcome).
//   - Already-active and terminal groups are skipped (the early return when an
//     active group exists, plus AdvanceGroup's QM-032 terminal-absorb guard).
//   - If AdvanceGroup leaves the group pending (no-op), no event is emitted and
//     the queue is not persisted, so calling this repeatedly is harmless.
//
// Spec ref: specs/queue-model.md §5 QM-031 (pending → active); §8 QM-063
// (persist-before-emit).
// Bead ref: hk-veoht.
func activateFirstPendingGroup(ctx context.Context, deps workLoopDeps) bool {
	if deps.queueStore == nil {
		return false
	}

	lq := deps.queueStore.LockForMutation()

	q := lq.Queue()
	if q == nil {
		lq.Done()
		return false
	}

	// Never bootstrap when a group is already active — that case is owned by the
	// normal dispatch path. This guards against racing a concurrent advance.
	for i := range q.Groups {
		if q.Groups[i].Status == queue.GroupStatusActive {
			lq.Done()
			return false
		}
	}

	// Locate the lowest-index non-terminal pending group.
	groupPos := -1
	for i := range q.Groups {
		if q.Groups[i].Status == queue.GroupStatusPending {
			groupPos = i
			break
		}
	}
	if groupPos < 0 {
		// No pending group to activate (all terminal, or no groups).
		lq.Done()
		return false
	}

	newStatus, events, advErr := queue.AdvanceGroup(ctx, &q.Groups[groupPos], q.Status, q.QueueID, time.Now())
	if advErr != nil {
		fmt.Fprintf(os.Stderr, "daemon: workloop: activateFirstPendingGroup AdvanceGroup queueID=%s groupIndex=%d: %v\n",
			q.QueueID, q.Groups[groupPos].GroupIndex, advErr)
		lq.Done()
		return false
	}

	// AdvanceGroup is a no-op for pending groups when the queue is not active
	// (QM-031 guard in advancePending): newStatus stays pending and events is
	// empty. Detect that and avoid a spurious persist/emit.
	if newStatus != queue.GroupStatusActive {
		lq.Done()
		return false
	}

	q.Groups[groupPos].Status = newStatus

	// Persist-before-emit (QM-063): on-disk state must reflect the pending →
	// active transition before the queue_group_started event reaches the bus.
	if err := queue.Persist(ctx, deps.projectDir, q); err != nil {
		fmt.Fprintf(os.Stderr, "daemon: workloop: activateFirstPendingGroup Persist queueID=%s: %v\n",
			q.QueueID, err)
		// Non-fatal: the in-memory transition stands so the loop can dispatch;
		// the file resyncs on the next persist. Suppress events — they describe
		// state not yet durable on disk (mirrors evaluateGroupAdvanceWithOutcome).
		events = nil
	}
	lq.SetQueue(q)
	lq.Done()

	// Emit the queued events after lock release (mirrors
	// evaluateGroupAdvanceWithOutcome; Bus.Emit is non-blocking per EV-002a).
	for _, evt := range events {
		raw, err := json.Marshal(evt.Payload)
		if err != nil {
			raw = evt.Payload
		}
		_ = deps.bus.Emit(ctx, core.EventType(evt.Type), raw)
	}

	return true
}

// activateFirstPendingGroupLocked is the under-lock variant of
// activateFirstPendingGroup for the multi-queue dispatch path (NQ-B1). The
// caller already holds the QueueStore write lock (lq) and supplies the specific
// queue q to bootstrap. It advances q's lowest-index pending group pending →
// active, persists (QM-063), writes the mutated queue back via
// LockedSetQueueByName, and returns true plus the resulting queue_group_started
// events for the CALLER to emit AFTER releasing the lock (preserving the
// EV-002a emit-after-persist-and-unlock idiom).
//
// Returns (false, nil) when q has an active group already, no pending group, or
// AdvanceGroup is a no-op (QM-031 guard).
//
// Spec ref: specs/queue-model.md §5 QM-031; §8 QM-063.
// Bead ref: hk-tigaf.4 (NQ-B1).
func activateFirstPendingGroupLocked(ctx context.Context, deps workLoopDeps, lq *LockedQueueStore, q *queue.Queue) (bool, []core.Event) {
	if q == nil {
		return false, nil
	}
	for i := range q.Groups {
		if q.Groups[i].Status == queue.GroupStatusActive {
			return false, nil
		}
	}
	groupPos := -1
	for i := range q.Groups {
		if q.Groups[i].Status == queue.GroupStatusPending {
			groupPos = i
			break
		}
	}
	if groupPos < 0 {
		return false, nil
	}

	newStatus, events, advErr := queue.AdvanceGroup(ctx, &q.Groups[groupPos], q.Status, q.QueueID, time.Now())
	if advErr != nil {
		fmt.Fprintf(os.Stderr, "daemon: workloop: activateFirstPendingGroupLocked AdvanceGroup queueID=%s groupIndex=%d: %v\n",
			q.QueueID, q.Groups[groupPos].GroupIndex, advErr)
		return false, nil
	}
	if newStatus != queue.GroupStatusActive {
		return false, nil
	}

	q.Groups[groupPos].Status = newStatus
	if err := queue.Persist(ctx, deps.projectDir, q); err != nil {
		fmt.Fprintf(os.Stderr, "daemon: workloop: activateFirstPendingGroupLocked Persist queueID=%s: %v\n",
			q.QueueID, err)
		events = nil // describe only durable state
	}
	lq.LockedSetQueueByName(queue.NormaliseQueueName(q.Name), q)

	// Events are returned for the caller to emit AFTER releasing the QueueStore
	// write lock (EV-002a emit-after-persist-and-unlock idiom, matching
	// activateFirstPendingGroup / evaluateGroupAdvanceWithOutcome).
	return true, events
}

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
// queueName identifies the queue the run was dispatched from (NQ-B1). It is
// the normalised name captured at dispatch time (capturedQueueName). The
// completion path MUST resolve the queue by name — using the main-only
// lq.Queue() shim instead would, for a non-"main" queue, return the wrong
// queue (or nil), trip the QueueID guard, and return early WITHOUT marking the
// item terminal, stalling that queue's group forever (hk-tigaf.4).
//
// Spec ref: specs/execution-model.md §4.3.EM-015f.
// Bead ref: hk-45ude, hk-tigaf.4.
func evaluateGroupAdvanceWithOutcome(ctx context.Context, deps workLoopDeps, queueName string, queueID string, groupIndex int, itemIdx int, success bool) {
	if deps.queueStore == nil {
		return
	}

	lq := deps.queueStore.LockForMutation()

	// NQ-B1: resolve the queue BY NAME (capturedQueueName), mirroring the
	// dispatch path's LockedQueueByName usage. queueName is already normalised
	// (the round-robin selector reads it from the QueueStore's map keys), so it
	// is passed straight through. The QueueID equality check is retained as a
	// staleness guard: it rejects a completion whose queue was cleared and a new
	// queue installed at the same name slot before this goroutine ran.
	q := lq.LockedQueueByName(queue.NormaliseQueueName(queueName))
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
		// NQ-B1: write back to the same name slot we resolved from.
		lq.LockedSetQueueByName(queue.NormaliseQueueName(queueName), q)
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
		// Release the write lock before ClearQueueByName (which acquires its own
		// lock). NQ-B1: clear the slot for THIS queue's name, not the main-only
		// ClearQueue shim — otherwise a completed non-"main" queue lingers in the
		// store and the round-robin selector keeps re-scanning a drained queue.
		deps.queueStore.ClearQueueByName(queue.NormaliseQueueName(queueName))
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
			// Suppress group-advance events — they describe state not yet durable on disk.
			events = nil
		}
		pausedByFailure := q.Status == queue.QueueStatusPausedByFailure
		// NQ-B1: write back to the same name slot we resolved from.
		lq.LockedSetQueueByName(queue.NormaliseQueueName(queueName), q)
		lq.Done()
		// hk-nbjht Gap 2: wake the idle dispatch loop after every run completion.
		// lq.SetQueue (the LockedQueueStore no-wake variant) does NOT fire wakeC,
		// so without this the loop stays parked in workloopIdleWait and never runs
		// its §2.8 deferred-item re-evaluation — a chained stream queue would stall
		// permanently once its head bead completes. Wake touches only wakeC (no
		// queue mutation, no second persist), so there is no double-persist race
		// with the SetQueue above. Fired unconditionally on run_completed: the
		// woken loop re-runs EligibleItems + ReevaluateDeferred, which is cheap and
		// idempotent if no item un-defers.
		deps.queueStore.Wake()
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

	// EM-062: eager-refill fires AFTER all terminal-event processing (merge,
	// reviewer-launch, CloseBead, group-advance evaluation) completes for this
	// run. Finishing in-flight work takes priority over pulling new work.
	//
	// Spec ref: specs/execution-model.md §4.13 EM-062.
	// Bead ref: hk-9321v.
	eagerRefillEval(ctx, deps)
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

// epicCompletedPayload is the JSON payload for the epic_completed event (hk-w6y70).
type epicCompletedPayload struct {
	EpicID          string `json:"epic_id"`
	LastChildBeadID string `json:"last_child_bead_id"`
	ClosedAt        string `json:"closed_at"`
}

// workingTreeRefreshFailedPayload is the JSON payload for the
// working_tree_refresh_failed event (§4.12.EM-054).
type workingTreeRefreshFailedPayload struct {
	RunID  string `json:"run_id"`
	BeadID string `json:"bead_id,omitempty"`
	Error  string `json:"error"`
}

// mergeBuildFailedPayload is the JSON payload for the merge_build_failed event
// (hk-o68j3).
type mergeBuildFailedPayload struct {
	RunID  string `json:"run_id"`
	BeadID string `json:"bead_id"`
	Error  string `json:"error"`
}

// lockedMergeRunBranchToMain wraps mergeRunBranchToMain with an optional mutex
// held across the entire rebase → update-ref → push sequence. Production always
// passes a non-nil mu (set in newWorkLoopDeps per hk-yyso7) so that merges are
// serialised globally across all queues. When mu is nil (unit tests that do not
// need real merge serialisation), the call runs unguarded.
//
// Bead ref: hk-bnm89, hk-yyso7.
func lockedMergeRunBranchToMain(ctx context.Context, mu *sync.Mutex, projectDir string, runID core.RunID, bus handlercontract.EventEmitter, beadID core.BeadID, headSHA string, targetBranch string, protectBranches []string) mergeOutcome {
	if mu != nil {
		mu.Lock()
		defer mu.Unlock()
	}
	return mergeRunBranchToMain(ctx, projectDir, runID, bus, beadID, headSHA, targetBranch, protectBranches)
}

// mergeRunBranchToMain implements the §4.12.EM-052 ordered merge sequence:
//
//  1. Resolve run-branch tip.
//  2. Rebase run-branch onto main (hk-j1aq5; rebase_conflict → EM-053 reopen path).
//  3. Fast-forward check (non-FF → EM-053 reopen path).
//  4. git update-ref refs/heads/main <tip>.
//     4a. Post-merge build gate: go build+vet in wtPath (hk-o68j3/hk-ycp62;
//     merge_build_failed → EM-053 reopen path).
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
// Bead: hk-ftyvo, hk-4goy3, hk-6r6xv.
func mergeRunBranchToMain(ctx context.Context, projectDir string, runID core.RunID, bus handlercontract.EventEmitter, beadID core.BeadID, headSHA string, targetBranch string, protectBranches []string) mergeOutcome {
	// Fail-closed guard (hk-6r6xv): refuse before any update-ref/push when
	// targetBranch is empty or appears in the protect-set.
	if targetBranch == "" {
		return mergeOutcome{
			success: false,
			reason:  "merge_target_empty: targetBranch must not be empty",
		}
	}
	for _, protected := range protectBranches {
		if targetBranch == protected {
			return mergeOutcome{
				success: false,
				reason:  fmt.Sprintf("merge_target_protected: %q is in ProtectBranches", targetBranch),
			}
		}
	}

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
	// from the target branch.  If targetTip == runTip the agent made no commits;
	// treat as no-change.
	mainTipCmd := exec.CommandContext(ctx, "git", "rev-parse", "refs/heads/"+targetBranch)
	mainTipCmd.Dir = projectDir
	mainTipOut, err := mainTipCmd.Output()
	if err != nil {
		return mergeOutcome{
			success: false,
			reason:  fmt.Sprintf("git rev-parse %s: %v", targetBranch, err),
		}
	}
	mainTip := strings.TrimRight(string(mainTipOut), "\n")

	if mainTip == runTip {
		// Run-branch tip == target tip: no commits were made by the agent.
		return mergeOutcome{noChange: true}
	}

	// hk-cwxow: false-positive guard. If runTip equals the fork-point SHA
	// (headSHA), the agent made no commits regardless of where the target branch
	// now points.  Without this check, when the target has advanced past headSHA
	// the is-ancestor test correctly fails and the daemon misreports
	// "non_ff_merge" even though the agent did nothing.
	if headSHA != "" && runTip == headSHA {
		return mergeOutcome{noChange: true}
	}

	// Step 2: rebase run-branch onto current target branch (hk-j1aq5).
	//
	// If the target has advanced since the worktree was cut (parallel agents
	// landing concurrently), rebase the run-branch onto it before the FF check.
	// This turns what would be a non_ff_merge failure into a successful merge as
	// long as there are no conflicts.  On conflict: abort and return
	// rebase_conflict so the bead is reopened (EM-053).
	//
	// Spec ref: specs/execution-model.md §4.12.EM-052 step 2.
	wtPath := workspace.WorktreePath(projectDir, runID.String(), workspace.NoWorktreeRootOverride())
	if _, statErr := os.Stat(wtPath); statErr == nil {
		// Pre-rebase cleanup (hk-3yz2d, hk-aiw63): discard any UNCOMMITTED
		// daemon/agent-owned churn in the worktree before the rebase. `git`
		// refuses to rebase a worktree with unstaged changes ("error: cannot
		// rebase: You have unstaged changes"). Two tracked files get dirtied
		// during every run without the implementer touching them as task work:
		// .beads/issues.jsonl (a `br` SQLite→JSONL flush; canonical source is
		// main) and .claude/settings.json (per-launch MaterializeClaudeSettings
		// hook-bridge merge + claude's own mutations; this repo tracks the file
		// and the root .gitignore does not cover it). discardDirtyChurn restores
		// exactly the isHarmonikChurn allowlist — the same set the post-merge
		// escape check uses — so an implementer that left GENUINE uncommitted
		// work (a non-churn path) still surfaces as a rebase failure rather than
		// being silently reset (hk-i1n7j safety property preserved).
		discardDirtyChurn(ctx, wtPath)

		// hk-rljho class: a review-loop iteration can leave a TRACKED but
		// UNCOMMITTED change in the worktree (e.g. a staged deletion of a test
		// file). discardDirtyChurn deliberately preserves it (hk-i1n7j: don't
		// silently reset real work), so it would survive to `git rebase <target>`
		// and abort with "cannot rebase: You have unstaged changes". Commit the
		// residual tracked delta onto the run-branch — it IS the bead's own work
		// — so the rebase proceeds with the work intact instead of failing.
		commitResidualDelta(ctx, wtPath, runID)

		// hk-g9zz: remove untracked non-gitignored files left by //go:build
		// integration tests. A bead whose agent ran an integration test (e.g.
		// TestIntegration_TwinE2E_OperatorRealEnv) may leave build artifacts —
		// binaries built without an explicit output path, temp objects, etc. —
		// in the run worktree. `git rebase` aborts when an untracked file would
		// be overwritten by a commit being replayed, producing:
		//   "error: The following untracked working tree files would be
		//    overwritten by checkout"
		// At this point commitResidualDelta has already committed any genuine
		// authored untracked files (new source files, etc.), so the files
		// `git clean -fd` touches are only integration test artifacts.
		// Non-fatal: errors are logged; the subsequent rebase surfaces the
		// real dirty-state failure if any genuine artifacts remain.
		cleanUntrackedFiles(ctx, wtPath)

		rebaseCmd := exec.CommandContext(ctx, "git", "rebase", targetBranch)
		rebaseCmd.Dir = wtPath
		if out, rebaseErr := rebaseCmd.CombinedOutput(); rebaseErr != nil {
			// Let git invoke the registered beads-union driver (configured via
			// .gitattributes + ensureBeadsMergeDriver) for .beads/issues.jsonl
			// conflicts. If the driver resolves them, the rebase succeeds and we
			// never reach this branch. If it fails or leaves conflicts unresolved,
			// surface the error rather than silently dropping a side.
			abortCmd := exec.CommandContext(ctx, "git", "rebase", "--abort")
			abortCmd.Dir = wtPath
			_ = abortCmd.Run()
			return mergeOutcome{
				success: false,
				reason:  fmt.Sprintf("rebase_conflict: %v\n%s", rebaseErr, strings.TrimRight(string(out), "\n")),
			}
		}
		// Rebase succeeded — re-resolve runTip and targetTip (both may have changed).
		rebasedTipCmd := exec.CommandContext(ctx, "git", "rev-parse", "refs/heads/"+runBranch)
		rebasedTipCmd.Dir = projectDir
		if rebasedOut, rebasedErr := rebasedTipCmd.Output(); rebasedErr == nil {
			runTip = strings.TrimRight(string(rebasedOut), "\n")
		}
		rebasedMainCmd := exec.CommandContext(ctx, "git", "rev-parse", "refs/heads/"+targetBranch)
		rebasedMainCmd.Dir = projectDir
		if rebasedMainOut, rebasedMainErr := rebasedMainCmd.Output(); rebasedMainErr == nil {
			mainTip = strings.TrimRight(string(rebasedMainOut), "\n")
		}

		// hk-zmpd: rebase-drop guard. Before this block we established that
		// runTip ≠ mainTip (the run-branch had reviewed commits). If the rebase
		// exits 0 but the new runTip equals mainTip, git silently dropped every
		// commit as "already applied" (patch-id match). Pushing a main that lost
		// reviewed work is worse than a visible reopen; fail-closed so the work
		// is salvageable on the run-branch.
		if runTip == mainTip {
			return mergeOutcome{
				success: false,
				reason: fmt.Sprintf(
					"rebase_dropped_commits: rebase of %s onto %s produced no commits"+
						" ahead of target; reviewed work silently dropped — salvage from run-branch",
					runBranch, targetBranch),
			}
		}
	}

	// hk-4je: strip .harmonik/run-context/** from the run-branch before the
	// fast-forward update-ref.  CHB-023 force-commits context.json to the task
	// branch for crash-recovery (EM-031); those paths must not land on the
	// merge target.  The strip commit is created in the run-branch worktree so
	// the subsequent update-ref picks up a clean tree.
	if stripped, stripErr := stripRunContextFromMerge(ctx, wtPath); stripErr != nil {
		return mergeOutcome{
			success: false,
			reason:  fmt.Sprintf("strip_run_context_failed: %v", stripErr),
		}
	} else if stripped {
		// Strip commit advanced run-branch HEAD — re-resolve runTip.
		if newTip, resolveErr := resolveWorktreeHEAD(ctx, wtPath); resolveErr == nil {
			runTip = newTip
		}
	}

	// Steps 3–4: FF-check → update-ref → build gate → push, with non-FF retry.
	//
	// On a non-fast-forward push rejection (origin/<targetBranch> advanced
	// out-of-band, e.g. a captain cherry-pick deploy), roll back the local
	// update-ref, fetch the new remote tip, rebase the run-branch onto it,
	// and retry — up to maxPushAttempts times total.  Any other push failure,
	// a rebase conflict on retry, or exhausted retries is terminal.
	//
	// Bead ref: hk-svieq.
	const maxPushAttempts = 3
	for pushAttempt := 1; pushAttempt <= maxPushAttempts; pushAttempt++ {
		// Step 3: fast-forward check.  The target branch MUST be an ancestor of
		// runTip.  git merge-base --is-ancestor <target> <runTip> exits 0 iff
		// target ⊆ runTip.
		isAncCmd := exec.CommandContext(ctx, "git", "merge-base", "--is-ancestor", mainTip, runTip)
		isAncCmd.Dir = projectDir
		if err := isAncCmd.Run(); err != nil {
			// Non-FF: target branch has diverged from the run-branch.
			return mergeOutcome{
				success: false,
				reason:  fmt.Sprintf("non_ff_merge: %s advanced concurrently", targetBranch),
			}
		}

		// Step 3a: fast-forward target branch to runTip.
		updateRefCmd := exec.CommandContext(ctx, "git", "update-ref", "refs/heads/"+targetBranch, runTip)
		updateRefCmd.Dir = projectDir
		if out, err := updateRefCmd.CombinedOutput(); err != nil {
			return mergeOutcome{
				success: false,
				reason:  fmt.Sprintf("git update-ref %s: %v\n%s", targetBranch, err, out),
			}
		}

		// Step 3b: post-merge build gate (hk-o68j3 / hk-ycp62).
		//
		// Run go build+vet on the MERGED tree to catch compile errors introduced
		// by the merged commit before the push makes them visible to other agents.
		// Build runs in the run-branch worktree (wtPath) when it is still on disk
		// — after the rebase (step 2) the worktree reflects the combined
		// main+agent content, so cross-bead conflicts such as redeclared
		// package-level helpers are caught here.  Falls back to projectDir for
		// runs where the worktree was already removed.
		// Only active when a go.mod is present in the build directory so non-Go
		// projects and bare-repo test fixtures are unaffected.
		// On failure: roll back the update-ref, emit merge_build_failed, and
		// return failure so the caller reopens the bead.
		buildDir := projectDir
		if _, statErr := os.Stat(wtPath); statErr == nil {
			buildDir = wtPath
		}
		if _, goModErr := os.Stat(filepath.Join(buildDir, "go.mod")); goModErr == nil {
			for _, buildArgs := range [][]string{
				{"build", "./..."},
				{"vet", "./..."},
			} {
				buildCmd := exec.CommandContext(ctx, "go", buildArgs...)
				buildCmd.Dir = buildDir
				if out, buildErr := buildCmd.CombinedOutput(); buildErr != nil {
					rollbackCmd := exec.CommandContext(ctx, "git", "update-ref", "refs/heads/"+targetBranch, mainTip)
					rollbackCmd.Dir = projectDir
					_ = rollbackCmd.Run()
					emitMergeBuildFailed(ctx, bus, runID, beadID, buildErr, out)
					return mergeOutcome{
						success: false,
						reason:  fmt.Sprintf("merge_build_failed (go %s): %v\n%s", buildArgs[0], buildErr, strings.TrimRight(string(out), "\n")),
					}
				}
			}

			// Step 3c: post-merge fmt-check gate (hk-k1hn).
			//
			// Run gofumpt -l and gci diff on the merged tree to catch formatting
			// drift before the push. Both tools exit 0 and emit output when files
			// are dirty; non-empty output blocks the merge.
			// Fail-open: if the .tools/ binaries are absent (non-Go repos, bare
			// test fixtures, CI machines without tools installed) the check is
			// skipped entirely. Same rollback+emit pattern as the build gate above.
			if fmtOutcome := runMergeFmtCheck(ctx, buildDir, projectDir, targetBranch, mainTip, runID, beadID, bus); fmtOutcome != nil {
				return *fmtOutcome
			}
		}

		// Step 4: push origin <targetBranch>.
		pushCmd := exec.CommandContext(ctx, "git", "push", "origin", targetBranch)
		pushCmd.Dir = projectDir
		pushOut, pushErr := pushCmd.CombinedOutput()
		if pushErr == nil {
			break // push succeeded; fall through to working-tree refresh
		}

		// Push failed — roll back the local update-ref so the repo is consistent.
		// Best-effort rollback: if it fails the operator will see the target branch
		// pointing to runTip without a matching remote; reconciliation (Cat 3 /
		// EM-INV-005) will catch this on the next startup.
		rollbackCmd := exec.CommandContext(ctx, "git", "update-ref", "refs/heads/"+targetBranch, mainTip)
		rollbackCmd.Dir = projectDir
		_ = rollbackCmd.Run()

		// Non-FF? If so, fetch the new remote tip, rebase the run-branch, and
		// retry the whole sequence.  All other push errors are terminal.
		pushOutStr := string(pushOut)
		isNonFF := strings.Contains(pushOutStr, "non-fast-forward") ||
			strings.Contains(pushOutStr, "[rejected]")
		if !isNonFF || pushAttempt >= maxPushAttempts {
			return mergeOutcome{
				success: false,
				reason:  fmt.Sprintf("push_failed: %v\n%s", pushErr, pushOut),
			}
		}

		// Fetch to update refs/remotes/origin/<targetBranch>.
		fetchCmd := exec.CommandContext(ctx, "git", "fetch", "origin", targetBranch)
		fetchCmd.Dir = projectDir
		if fetchOut, fetchErr := fetchCmd.CombinedOutput(); fetchErr != nil {
			return mergeOutcome{
				success: false,
				reason:  fmt.Sprintf("push_failed_fetch (attempt %d): %v\n%s", pushAttempt, fetchErr, fetchOut),
			}
		}

		// Read the new remote tip.
		remoteRef := "refs/remotes/origin/" + targetBranch
		remoteRevCmd := exec.CommandContext(ctx, "git", "rev-parse", remoteRef)
		remoteRevCmd.Dir = projectDir
		remoteRevOut, remoteRevErr := remoteRevCmd.Output()
		if remoteRevErr != nil {
			return mergeOutcome{
				success: false,
				reason:  fmt.Sprintf("push_failed_rev_parse_remote (attempt %d): %v", pushAttempt, remoteRevErr),
			}
		}
		newMainTip := strings.TrimRight(string(remoteRevOut), "\n")

		// Advance local targetBranch to the fetched remote tip so the rebase
		// and next iteration's FF check have a correct base.
		updateToRemoteCmd := exec.CommandContext(ctx, "git", "update-ref", "refs/heads/"+targetBranch, newMainTip)
		updateToRemoteCmd.Dir = projectDir
		if updateOut, updateErr := updateToRemoteCmd.CombinedOutput(); updateErr != nil {
			return mergeOutcome{
				success: false,
				reason:  fmt.Sprintf("push_failed_update_to_remote (attempt %d): %v\n%s", pushAttempt, updateErr, updateOut),
			}
		}
		mainTip = newMainTip

		// Rebase the run-branch onto the updated local target (in the worktree).
		if _, statErr := os.Stat(wtPath); statErr == nil {
			discardDirtyChurn(ctx, wtPath)
			commitResidualDelta(ctx, wtPath, runID)
			retryRebaseCmd := exec.CommandContext(ctx, "git", "rebase", targetBranch)
			retryRebaseCmd.Dir = wtPath
			if out, rebaseErr := retryRebaseCmd.CombinedOutput(); rebaseErr != nil {
				abortCmd := exec.CommandContext(ctx, "git", "rebase", "--abort")
				abortCmd.Dir = wtPath
				_ = abortCmd.Run()
				return mergeOutcome{
					success: false,
					reason:  fmt.Sprintf("rebase_conflict_on_push_retry (attempt %d): %v\n%s", pushAttempt, rebaseErr, strings.TrimRight(string(out), "\n")),
				}
			}
		}

		// Re-resolve runTip after the retry rebase.
		retryRunTipCmd := exec.CommandContext(ctx, "git", "rev-parse", "refs/heads/"+runBranch)
		retryRunTipCmd.Dir = projectDir
		if retryRunTipOut, retryRunTipErr := retryRunTipCmd.Output(); retryRunTipErr == nil {
			runTip = strings.TrimRight(string(retryRunTipOut), "\n")
		}

		// hk-zmpd: rebase-drop guard on push-retry rebase (same invariant as the
		// initial rebase above — reviewed commits must survive onto the new base).
		if runTip == mainTip {
			return mergeOutcome{
				success: false,
				reason: fmt.Sprintf(
					"rebase_dropped_commits_on_push_retry (attempt %d): rebase of %s"+
						" onto %s produced no commits ahead of target",
					pushAttempt, runBranch, targetBranch),
			}
		}

		// Loop back: FF-check → update-ref → build gate → push with updated mainTip/runTip.
	}

	// Step 5: refresh project working tree to match HEAD (EM-054).
	//
	// Step 5a: restore the staged index before the working-tree reset.
	//
	// git update-ref (step 4) advances HEAD but leaves the index at the
	// pre-merge state.  Any files that were added/modified by the merged commit
	// now appear as "staged deletions" (index behind HEAD) in git status
	// --porcelain.  If git reset --hard HEAD (step 5b) subsequently fails
	// (non-fatal per EM-054), those staged phantom-deletions persist into the
	// next bead's run and trigger false implementer_escaped_worktree positives.
	//
	// git restore --staged . clears the index to match HEAD without touching
	// the working tree.  It is lighter than reset --hard and less likely to
	// fail (no working-tree I/O, no file-lock contention).  Running it first
	// means that even on a reset --hard failure the staged index is already
	// clean for the subsequent escape check.
	//
	// Best-effort / non-fatal: a failure here is harmless because step 5b will
	// attempt the same cleanup (and more) via reset --hard.
	restoreCmd := exec.CommandContext(ctx, "git", "restore", "--staged", ".")
	restoreCmd.Dir = projectDir
	if out, restoreErr := restoreCmd.CombinedOutput(); restoreErr != nil {
		fmt.Fprintf(os.Stderr, "daemon: mergeRunBranchToMain: WARNING: git restore --staged failed (bead %s run %s): %v\n%s",
			beadID, runID.String(), restoreErr, out)
	}

	// Step 5b: git reset --hard HEAD re-syncs both the index and the working
	// tree to the new HEAD (which is now the run-branch tip). This eliminates
	// the "modified" state that appears in git status when update-ref advances
	// HEAD without touching the working tree files.
	//
	// Uncommitted-changes policy (EM-054): if the working tree has uncommitted
	// changes, log a warning and still reset. The daemon owns the project working
	// tree during operation; the operator is expected to keep it clean.
	//
	// Refresh-failure policy (EM-054): if git reset --hard HEAD fails, the merge
	// is already durable. Log a warning, emit working_tree_refresh_failed, and
	// return success=true so the caller proceeds to CloseBead normally.
	// F21: "uncommitted changes before refresh" WARN removed — fires x142/session
	// during normal operation; the reset below always cleans it up unconditionally.
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

// discardDirtyChurn discards UNCOMMITTED changes to daemon/agent-owned churn
// files in the run worktree, restoring each to the run-branch's committed
// version, so `git rebase main` can proceed.
//
// `git rebase` refuses to start when the worktree has unstaged changes
// (it aborts with "error: cannot rebase: You have unstaged changes" before any
// conflict detection). Two distinct tracked files get dirtied during every run
// without the implementer ever touching them as part of its task work:
//
//   - .beads/issues.jsonl — the bead ledger. Becomes dirty whenever a `br`
//     operation flushes its shared SQLite DB to the per-worktree JSONL during the
//     run. Its canonical source of truth is main (the daemon owns all terminal
//     bead transitions).
//   - .claude/settings.json — the Claude hook-bridge settings. The daemon's
//     MaterializeClaudeSettings (CHB-001..005) merges the bridge hooks +
//     permissions.allow into the worktree copy on every launch, and the running
//     claude agent may further mutate it. Because this repo TRACKS the file (the
//     root .gitignore only covers /.claude/worktrees/, not .claude/settings.json),
//     the per-launch materialization leaves it modified-but-unstaged. This is the
//     hk-aiw63 blocker: it persisted after hk-i1n7j (which only discarded the
//     ledger) and aborted every real merge-to-main where claude mutates settings.
//
// Discarding either is safe: both are reconstructed deterministically (the
// ledger from main, the settings from the next MaterializeClaudeSettings call)
// and neither carries implementer task work.
//
// The set of discardable paths is exactly isHarmonikChurn — the same allowlist
// the post-merge escape check (checkMainWorkingTreeDirty) uses to classify
// expected churn. This preserves the hk-i1n7j safety property: a dirty file that
// is NOT recognized churn is left untouched, so an implementer that escaped its
// worktree (left genuine uncommitted work) still fails the rebase loudly rather
// than being silently reset.
//
// Errors are non-fatal and best-effort: if `git status` or a `git checkout`
// fails, the function continues / returns silently and the subsequent rebase
// reports the real failure. It is a no-op when no churn paths are dirty.
//
// Beads: hk-3yz2d (ledger), hk-aiw63 (generalized to .claude/settings.json and
// the full isHarmonikChurn allowlist).
func discardDirtyChurn(ctx context.Context, wtPath string) {
	// Enumerate ALL dirty paths in the worktree once, then discard only those
	// the churn allowlist recognizes. Untracked files (status "??") are not
	// git-checkout-restorable and are excluded by tracked-status filtering below.
	statusCmd := exec.CommandContext(ctx, "git", "status", "--porcelain")
	statusCmd.Dir = wtPath
	statusOut, statusErr := statusCmd.Output()
	if statusErr != nil || len(strings.TrimSpace(string(statusOut))) == 0 {
		return
	}

	var churnPaths []string
	for _, line := range strings.Split(strings.TrimRight(string(statusOut), "\n"), "\n") {
		if len(line) < 4 {
			continue
		}
		// Porcelain v1: "XY <path>". Untracked is "?? <path>" — skip (cannot be
		// `git checkout`-restored).
		xy := line[:2]
		if xy == "??" {
			continue
		}
		path := line[3:]
		// Handle rename "old -> new": restore the destination path.
		if idx := strings.Index(path, " -> "); idx >= 0 {
			path = path[idx+4:]
		}
		path = strings.Trim(path, "\"")
		if isHarmonikChurn(path) {
			churnPaths = append(churnPaths, path)
		}
	}
	if len(churnPaths) == 0 {
		return
	}

	// Restore each churn path to its committed version. Use one checkout per
	// path so a failure on one (e.g. a path that is staged-only) does not block
	// the others; mirrors hk-i1n7j's best-effort/non-fatal style.
	for _, path := range churnPaths {
		checkoutCmd := exec.CommandContext(ctx, "git", "checkout", "--", path)
		checkoutCmd.Dir = wtPath
		if out, err := checkoutCmd.CombinedOutput(); err != nil {
			fmt.Fprintf(os.Stderr, "daemon: discardDirtyChurn: git checkout -- %s: %v\n%s",
				path, err, out)
		}
	}
}

// commitResidualDelta commits any UNCOMMITTED change that survives
// discardDirtyChurn onto the run-branch, immediately before the pre-merge
// `git rebase main`.
//
// Bug (hk-rljho class): a review-loop iteration can leave a TRACKED but
// UNCOMMITTED change in the run worktree (e.g. a staged deletion of a test file
// an iteration removed, whose deletion never got its own commit because the
// daemon's commit-detection had already fired). discardDirtyChurn deliberately
// restores only the isHarmonikChurn allowlist and leaves genuine work untouched
// (hk-i1n7j safety property), so this real iteration delta survives to
// `git rebase main`, which aborts with "cannot rebase: You have unstaged
// changes" — failing the merge even though the bead's work is complete.
//
// Bug (hk-cmry defect #3): the implementer / a review-loop iteration can author
// genuinely NEW files (untracked, status "??") that were never committed — e.g.
// hk-8prq's GREEN added internal/keeper/sessionid.go and a new hook script as
// brand-new files. The original `git add -u` staged only TRACKED modifications
// and SILENTLY DROPPED those new files: the residual commit then carried only
// the tracked changes (often the RED test) and the new-file GREEN was lost when
// the worktree was later cleaned. That is how the daemon silently dropped a
// reviewed GREEN and broke main fleet-wide. The fix stages with `git add -A` so
// authored new files can NEVER be silently dropped.
//
// The fix PRESERVES hk-i1n7j: it does NOT discard the residual work. It COMMITS
// the delta onto the run-branch (it IS the bead's own work — a review-loop edit
// or new source file that never got committed) so the rebase proceeds with the
// work intact.
//
// On the original `git add -u` → `git add -A` concern: -A also stages untracked
// files, which the prior code feared would sweep "stray junk" to main+origin.
// That concern is now mitigated on two grounds: (1) `git add -A` HONORS
// .gitignore, and this repo's .gitignore excludes every class of daemon/runtime
// junk (.harmonik/, .beads/*, .env, build outputs, worktrees, *.test, etc.), so
// none of it is stageable; (2) discardDirtyChurn has already restored the
// isHarmonikChurn allowlist, so any non-gitignored untracked file that SURVIVES
// to this point is the bead's authored work, not stray junk. Dropping it (the
// old behavior) is the actual bug; capturing it is correct.
//
// It is a no-op when no non-churn change remains after churn cleanup (so it
// never manufactures an empty commit). Errors are best-effort/non-fatal in the
// same style as discardDirtyChurn: a failure leaves the residual delta in place
// and the subsequent rebase surfaces the real "unstaged changes" failure rather
// than masking it.
//
// Bead: review-loop residual-delta merge fix (hk-rljho class); untracked-capture
// fix (hk-cmry defect #3).
func commitResidualDelta(ctx context.Context, wtPath string, runID core.RunID) {
	// Enumerate dirty paths once. We commit only if a non-churn change survives
	// churn cleanup. Untracked files ("??") ARE counted now: an untracked file
	// surviving discardDirtyChurn is the bead's authored work (a new source
	// file), and `git add -A` will stage it. Gitignored paths never appear in
	// `git status --porcelain`, so they are excluded here and by `git add -A`.
	statusCmd := exec.CommandContext(ctx, "git", "status", "--porcelain")
	statusCmd.Dir = wtPath
	statusOut, statusErr := statusCmd.Output()
	if statusErr != nil {
		return
	}

	var residual bool
	for _, line := range strings.Split(strings.TrimRight(string(statusOut), "\n"), "\n") {
		if len(line) < 4 {
			continue
		}
		// Porcelain v1: "XY <path>". Untracked is "?? <path>" — a NEW authored
		// file that must be captured, NOT skipped (hk-cmry defect #3).
		path := line[3:]
		// Handle rename "old -> new": classify on the destination path.
		if idx := strings.Index(path, " -> "); idx >= 0 {
			path = path[idx+4:]
		}
		path = strings.Trim(path, "\"")
		if isHarmonikChurn(path) {
			continue // should already be restored by discardDirtyChurn; skip defensively
		}
		residual = true
		break
	}
	if !residual {
		return // no genuine residual delta — do not create an empty commit
	}

	// Stage all residual work — tracked modifications/deletions AND untracked
	// NEW files. `git add -A` HONORS .gitignore (so daemon/runtime/build junk
	// stays excluded), and discardDirtyChurn already restored the churn
	// allowlist, so -A captures exactly the bead's genuine residual iteration
	// delta, including any newly authored source files that would otherwise be
	// silently dropped (hk-cmry defect #3).
	//
	// Hardening (hk-igq3): use explicit pathspec EXCLUSIONS for .claude/ and
	// .harmonik/ so that neither is ever swept into a commit even when a
	// legitimate non-churn change is present in the same worktree.
	// .claude/ is only partially gitignored (worktrees/ + scheduled_tasks.lock),
	// so a blanket -A could stage and push credential-adjacent files to origin.
	// .harmonik/ contains daemon runtime state (review.json, run-context, etc.)
	// that MUST NOT land on the merge target; gitignore covers it but the
	// explicit exclusion is belt-and-suspenders (GH #7, hk-znou: review.json
	// committed via -A caused add/add rebase conflicts on concurrent runs).
	// The :(exclude) pathspec magic is honored by git ≥ 2.0 and matches the
	// directory and all descendants.
	addCmd := exec.CommandContext(ctx, "git", "add", "-A", "--",
		".",
		":(exclude).claude",
		":(exclude).harmonik",
	)
	addCmd.Dir = wtPath
	if out, err := addCmd.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "daemon: commitResidualDelta: git add -A: %v\n%s", err, out)
		return
	}

	commitMsg := fmt.Sprintf(
		"chore(reviewloop): commit residual iteration delta before rebase [%s]",
		runID.String(),
	)
	commitCmd := exec.CommandContext(ctx, "git", "commit", "-m", commitMsg)
	commitCmd.Dir = wtPath
	if out, err := commitCmd.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "daemon: commitResidualDelta: git commit: %v\n%s", err, out)
	}
}

// cleanUntrackedFiles removes untracked non-gitignored files and directories
// from the run worktree using `git clean -fd`. Called after discardDirtyChurn
// and commitResidualDelta as the final pre-rebase cleanup step so that
// integration-test artifacts (binaries built without an output path, temp
// objects, etc.) cannot abort the rebase.
//
// `git rebase` aborts when an untracked file in the working tree would be
// overwritten by a commit being replayed ("error: The following untracked
// working tree files would be overwritten by checkout"). `git clean -fd`
// removes those files — it honours .gitignore, so platform/build artifacts
// already covered by .gitignore are left untouched.
//
// Safety: at this point commitResidualDelta has already committed any genuine
// authored untracked files (new source files the implementer added), so the
// only files that survive to this step are integration-test artifacts, not
// bead work. Gitignored files (*.test, /harmonik-twin-claude, /.harmonik/)
// are unaffected and do not interfere with the rebase.
//
// Non-fatal and best-effort: errors are logged to stderr; the subsequent
// rebase will surface the real dirty-state failure if cleaning did not fully
// succeed.
//
// Bead: hk-g9zz.
func cleanUntrackedFiles(ctx context.Context, wtPath string) {
	cleanCmd := exec.CommandContext(ctx, "git", "clean", "-fd")
	cleanCmd.Dir = wtPath
	if out, err := cleanCmd.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "daemon: cleanUntrackedFiles: git clean -fd: %v\n%s", err, out)
	}
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

// emitMergeBuildFailed emits a merge_build_failed event when go build or go
// vet fails on the freshly fast-forwarded merged tree (hk-o68j3). The
// update-ref has already been rolled back before this is called.
//
// Bead: hk-o68j3.
func emitMergeBuildFailed(ctx context.Context, bus handlercontract.EventEmitter, runID core.RunID, beadID core.BeadID, buildErr error, output []byte) {
	errMsg := buildErr.Error()
	if len(output) > 0 {
		errMsg = fmt.Sprintf("%s\n%s", errMsg, strings.TrimRight(string(output), "\n"))
	}
	pl := mergeBuildFailedPayload{
		RunID:  runID.String(),
		BeadID: string(beadID),
		Error:  errMsg,
	}
	b, err := json.Marshal(pl)
	if err != nil {
		return
	}
	_ = bus.EmitWithRunID(ctx, runID, core.EventTypeMergeBuildFailed, b)
}

// runMergeFmtCheck detects formatting drift (gofumpt, gci) on buildDir before
// the push and auto-heals it when an isolated worktree is available.
//
// Auto-heal path (buildDir != projectDir — worktree exists): runs gofumpt -w
// and/or gci write to reformat in place, then stages and commits the changes
// in the worktree and advances refs/heads/<targetBranch> to the new tip so the
// caller's push step picks up the format commit.
//
// Fallback path (buildDir == projectDir — worktree already removed): rolls
// back the update-ref and emits merge_build_failed, same as the original
// reject behaviour, because committing in the live project directory is unsafe.
//
// Fail-open: if either tool binary is absent in projectDir/.tools/ the check
// is silently skipped (non-Go repos, bare test fixtures, CI without tools).
//
// Beads: hk-k1hn (original gate), hk-0lrt (auto-heal).
func runMergeFmtCheck(ctx context.Context, buildDir, projectDir, targetBranch, mainTip string, runID core.RunID, beadID core.BeadID, bus handlercontract.EventEmitter) *mergeOutcome {
	rollback := func() {
		rb := exec.CommandContext(ctx, "git", "update-ref", "refs/heads/"+targetBranch, mainTip)
		rb.Dir = projectDir
		_ = rb.Run()
	}

	// Auto-format is only safe when an isolated worktree is available.
	// When buildDir == projectDir the worktree has already been removed and
	// we cannot commit a format change without racing the project's own index.
	canAutoFmt := buildDir != projectDir

	needsCommit := false

	gofumptBin := filepath.Join(projectDir, ".tools", "gofumpt")
	gciBin := filepath.Join(projectDir, ".tools", "gci")
	_, gofumptAvail := os.Stat(gofumptBin)
	_, gciAvail := os.Stat(gciBin)
	mod := readGoModule(buildDir)

	// Run gofumpt+gci to a fixpoint: each tool can disturb the other's
	// invariant (gci re-groups imports → gofumpt sees drift; gofumpt adjusts
	// spacing → gci sees drift), so we loop until both are clean or we exhaust
	// the iteration cap.
	const maxFmtIter = 5
	for i := range maxFmtIter {
		iterDirty := false

		if gofumptAvail == nil {
			listCmd := exec.CommandContext(ctx, gofumptBin, "-l", ".")
			listCmd.Dir = buildDir
			// gofumpt -l exits 0; non-empty stdout = unformatted files.
			if out, err := listCmd.Output(); err == nil && len(strings.TrimSpace(string(out))) > 0 {
				if canAutoFmt {
					fmtCmd := exec.CommandContext(ctx, gofumptBin, "-w", ".")
					fmtCmd.Dir = buildDir
					if fmtErr := fmtCmd.Run(); fmtErr != nil {
						rollback()
						msg := "gofumpt -w: " + fmtErr.Error()
						emitMergeBuildFailed(ctx, bus, runID, beadID, errors.New(msg), nil)
						return &mergeOutcome{success: false, reason: "merge_fmt_failed (gofumpt -w): " + fmtErr.Error()}
					}
					needsCommit = true
					iterDirty = true
				} else {
					rollback()
					msg := "gofumpt: unformatted files (run 'make fmt' to fix):\n" + strings.TrimRight(string(out), "\n")
					emitMergeBuildFailed(ctx, bus, runID, beadID, errors.New(msg), nil)
					return &mergeOutcome{
						success: false,
						reason:  "merge_fmt_failed (gofumpt): " + strings.TrimRight(string(out), "\n"),
					}
				}
			}
		}

		if gciAvail == nil && mod != "" {
			diffCmd := exec.CommandContext(ctx, gciBin, "diff", "-s", "standard", "-s", "default", "-s", "prefix("+mod+")", ".")
			diffCmd.Dir = buildDir
			// gci diff exits 0; non-empty stdout = import order drift.
			if out, err := diffCmd.Output(); err == nil && len(strings.TrimSpace(string(out))) > 0 {
				if canAutoFmt {
					writeCmd := exec.CommandContext(ctx, gciBin, "write", "-s", "standard", "-s", "default", "-s", "prefix("+mod+")", ".")
					writeCmd.Dir = buildDir
					if writeErr := writeCmd.Run(); writeErr != nil {
						rollback()
						msg := "gci write: " + writeErr.Error()
						emitMergeBuildFailed(ctx, bus, runID, beadID, errors.New(msg), nil)
						return &mergeOutcome{success: false, reason: "merge_fmt_failed (gci write): " + writeErr.Error()}
					}
					needsCommit = true
					iterDirty = true
				} else {
					rollback()
					msg := "gci: import order drift (run 'make fmt' to fix):\n" + strings.TrimRight(string(out), "\n")
					emitMergeBuildFailed(ctx, bus, runID, beadID, errors.New(msg), nil)
					return &mergeOutcome{
						success: false,
						reason:  "merge_fmt_failed (gci): import order drift detected",
					}
				}
			}
		}

		if !iterDirty {
			break
		}
		if i == maxFmtIter-1 {
			// Both tools are still disagreeing after maxFmtIter passes — a
			// genuine divergence in tool config; fail rather than loop forever.
			rollback()
			emitMergeBuildFailed(ctx, bus, runID, beadID,
				errors.New("gofumpt+gci did not converge after "+fmt.Sprint(maxFmtIter)+" passes"),
				nil)
			return &mergeOutcome{
				success: false,
				reason:  "merge_fmt_failed: gofumpt+gci did not converge after " + fmt.Sprint(maxFmtIter) + " passes (check gci local-prefix config vs module path)",
			}
		}
	}

	if needsCommit {
		// Stage all formatting changes in the worktree.
		addCmd := exec.CommandContext(ctx, "git", "add", "-A")
		addCmd.Dir = buildDir
		if addOut, addErr := addCmd.CombinedOutput(); addErr != nil {
			rollback()
			emitMergeBuildFailed(ctx, bus, runID, beadID, addErr, addOut)
			return &mergeOutcome{success: false, reason: "merge_fmt_failed (git add): " + addErr.Error()}
		}

		commitMsg := fmt.Sprintf("fmt: auto-format via gofumpt+gci\n\nRefs: %s", beadID)
		commitCmd := exec.CommandContext(ctx, "git", "commit", "-m", commitMsg)
		commitCmd.Dir = buildDir
		if commitOut, commitErr := commitCmd.CombinedOutput(); commitErr != nil {
			rollback()
			emitMergeBuildFailed(ctx, bus, runID, beadID, commitErr, commitOut)
			return &mergeOutcome{success: false, reason: "merge_fmt_failed (fmt commit): " + commitErr.Error()}
		}

		// Advance targetBranch to the new formatted tip so step 4 (push)
		// picks up the format commit.
		newTipCmd := exec.CommandContext(ctx, "git", "rev-parse", "HEAD")
		newTipCmd.Dir = buildDir
		if newTipOut, newTipErr := newTipCmd.Output(); newTipErr == nil {
			newTip := strings.TrimRight(string(newTipOut), "\n")
			updateCmd := exec.CommandContext(ctx, "git", "update-ref", "refs/heads/"+targetBranch, newTip)
			updateCmd.Dir = projectDir
			if updateErr := updateCmd.Run(); updateErr != nil {
				rollback()
				emitMergeBuildFailed(ctx, bus, runID, beadID, updateErr, nil)
				return &mergeOutcome{success: false, reason: "merge_fmt_failed (update-ref after fmt): " + updateErr.Error()}
			}
		}
	}

	return nil
}

// readGoModule parses the first "module <path>" directive from dir/go.mod.
// Returns empty string on any error.
func readGoModule(dir string) string {
	data, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[0] == "module" {
			return fields[1]
		}
	}
	return ""
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

// emitBeadClosedAndMaybeEpic emits bead_closed then checks whether the closed
// bead's parent epic just completed (hk-w6y70 C1). It is the single insertion
// point replacing the seven raw emitBeadClosed call sites.
func emitBeadClosedAndMaybeEpic(ctx context.Context, deps workLoopDeps, runID core.RunID, beadID core.BeadID) {
	emitBeadClosed(ctx, deps.bus, runID, beadID)
	maybeEmitEpicCompleted(ctx, deps, runID, beadID)
}

// maybeEmitEpicCompleted checks whether closedBeadID's parent epic now has all
// children closed, and if so emits epic_completed exactly once (AC-1 at-most-once
// per daemon session). Zero-emit on: no parent (AC-4), still-open sibling (AC-3),
// or already-emitted guard hit.
//
// Bead: hk-w6y70.
func maybeEmitEpicCompleted(ctx context.Context, deps workLoopDeps, runID core.RunID, closedBeadID core.BeadID) {
	// Step 1: ShowBead(closedBead) to find the parent via a parent-child edge.
	// The closed bead's outgoing parent-child edge has FromBeadID == closedBead,
	// ToBeadID == parent (per brcli/show.go: dependencies[] → outgoing edges).
	closedRecord, err := deps.brAdapter.ShowBead(ctx, closedBeadID)
	if err != nil {
		return
	}

	var parentID core.BeadID
	for _, e := range closedRecord.Edges {
		if e.EdgeKind == core.EdgeKindParentChild && e.FromBeadID == closedBeadID {
			parentID = e.ToBeadID
			break
		}
	}
	if parentID == "" {
		// AC-4: no parent → zero emit.
		return
	}

	// Step 2: ShowBead(parent) to enumerate all children and check their statuses.
	// Incoming parent-child edges on the parent have ToBeadID == parent,
	// FromBeadID == child (per brcli/show.go: dependents[] → incoming edges).
	parentRecord, err := deps.brAdapter.ShowBead(ctx, parentID)
	if err != nil {
		return
	}

	for _, e := range parentRecord.Edges {
		if e.EdgeKind == core.EdgeKindParentChild && e.ToBeadID == parentID {
			if e.EndpointStatus != core.CoarseStatusClosed {
				// AC-3: at least one child still open → zero emit.
				return
			}
		}
	}

	// All children are closed (or there are none — edge case: epic with no
	// children recorded yet; we emit to avoid silent gaps, consistent with AC-1).

	// Step 3: claim under emittedEpicsMu BEFORE emit (at-most-once guard AC-1).
	deps.emittedEpicsMu.Lock()
	if _, already := deps.emittedEpics[parentID]; already {
		deps.emittedEpicsMu.Unlock()
		return
	}
	deps.emittedEpics[parentID] = struct{}{}
	deps.emittedEpicsMu.Unlock()

	// Step 4: emit epic_completed.
	pl := epicCompletedPayload{
		EpicID:          string(parentID),
		LastChildBeadID: string(closedBeadID),
		ClosedAt:        time.Now().UTC().Format(time.RFC3339),
	}
	b, err := json.Marshal(pl)
	if err != nil {
		return
	}
	_ = deps.bus.EmitWithRunID(ctx, runID, core.EventTypeEpicCompleted, b)
}

// snapshotUntrackedFiles (hk-ooexj) captures the set of paths the main repo's
// working tree reports as dirty/untracked at run-start, BEFORE the implementer
// launches. The returned set is fed to checkMainWorkingTreeDirty after the run
// so that files which already existed (and which the implementer never touched)
// are NOT mistaken for an escape.
//
// It uses the same `git status --porcelain` surface as the escape check (which
// already excludes gitignored paths by default), so a pre-existing
// untracked-but-not-ignored file (e.g. a scratch note in the project root) is
// baselined and excluded, while a NET-NEW file the implementer writes outside
// its worktree still surfaces as an escape.
//
// Errors (e.g. git not in PATH) return (nil, err); the caller treats a failed
// snapshot as "no baseline" — the escape check then degrades to its prior
// behaviour rather than silently suppressing genuine escapes.
func snapshotUntrackedFiles(ctx context.Context, mainPath string) (map[string]struct{}, error) {
	if mainPath == "" {
		return nil, fmt.Errorf("snapshotUntrackedFiles: empty mainPath")
	}
	cmd := exec.CommandContext(ctx, "git", "-C", mainPath, "status", "--porcelain")
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("snapshotUntrackedFiles: git status: %w", err)
	}
	baseline := make(map[string]struct{})
	for _, path := range parsePorcelainPaths(string(out)) {
		baseline[path] = struct{}{}
	}
	return baseline, nil
}

// parsePorcelainPaths extracts the destination path from each line of
// `git status --porcelain` output, stripping the XY status prefix, resolving
// rename "old -> new" to the destination, and unquoting special-char paths.
func parsePorcelainPaths(out string) []string {
	var paths []string
	for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
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
		paths = append(paths, path)
	}
	return paths
}

// checkMainWorkingTreeDirty (hk-6zylj) reports whether the main repo's working
// tree contains dirty files outside the harmonik churn allowlist that did NOT
// exist before the run started.
//
// It runs `git -C <mainPath> status --porcelain` and filters the output:
//   - `.harmonik/...`        — daemon state (expected churn)
//   - `.claude/...`          — orchestrator/Claude state (expected churn)
//   - `.beads/issues.jsonl`  — bead ledger (expected churn from br sync)
//   - `AGENT_COMMS.md`       — orchestrator scratch (expected churn, hk-77q8e)
//   - paths in `baseline`    — pre-existing untracked files (hk-ooexj)
//   - gitignored paths       — never the implementer's escape (hk-ooexj)
//
// `git status --porcelain` already omits gitignored paths by default; the
// explicit check-ignore pass is defense-in-depth against a parent-repo
// `.gitignore` or core.excludesFile that surfaces an ignored path here.
//
// The caller (runAgentImplementer) holds mergeMu across this call (hk-zguy6),
// so no sibling merge can be mid-flight (between update-ref and reset-hard)
// when we inspect the working tree. No path-exclusion heuristic is needed for
// sibling-merge races — the lock provides the full guarantee (hk-xux36).
//
// Anything else dirty is treated as an escape. The returned list contains the
// destination path of each surviving porcelain status line.
//
// Errors (e.g. git not in PATH) return (false, nil, err) so the caller can
// treat the check as informational and skip without failing the run.
func checkMainWorkingTreeDirty(ctx context.Context, mainPath string, baseline map[string]struct{}) (bool, []string, error) {
	if mainPath == "" {
		return false, nil, fmt.Errorf("checkMainWorkingTreeDirty: empty mainPath")
	}
	cmd := exec.CommandContext(ctx, "git", "-C", mainPath, "status", "--porcelain")
	out, err := cmd.Output()
	if err != nil {
		return false, nil, fmt.Errorf("checkMainWorkingTreeDirty: git status: %w", err)
	}

	var candidates []string
	for _, path := range parsePorcelainPaths(string(out)) {
		if isHarmonikChurn(path) {
			continue
		}
		// hk-ooexj: pre-existing untracked file — present at run-start, so the
		// implementer did not create it. Not an escape.
		if _, preexisting := baseline[path]; preexisting {
			continue
		}
		candidates = append(candidates, path)
	}
	// hk-ooexj: drop any gitignored paths (defense-in-depth — git status already
	// omits these by default, but a parent gitignore could surface them).
	dirty := filterIgnoredPaths(ctx, mainPath, candidates)
	return len(dirty) > 0, dirty, nil
}

// filterIgnoredPaths returns paths minus those git considers ignored under
// mainPath. It batches the paths through a single `git check-ignore` call
// (NUL-delimited via --stdin -z). On any real error it returns paths unchanged
// — failing open keeps genuine escapes visible rather than swallowing them.
func filterIgnoredPaths(ctx context.Context, mainPath string, paths []string) []string {
	if len(paths) == 0 {
		return paths
	}
	cmd := exec.CommandContext(ctx, "git", "-C", mainPath, "check-ignore", "--stdin", "-z")
	cmd.Stdin = strings.NewReader(strings.Join(paths, "\x00"))
	out, err := cmd.Output()
	// check-ignore exits 0 when ≥1 path is ignored, 1 when none are ignored
	// (not an error for us), and ≥128 on a real failure. Treat exit 1 (no
	// matches) as "nothing ignored"; treat other non-zero codes as fail-open.
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
			return paths // none ignored
		}
		return paths // fail open: keep all candidates visible
	}
	ignored := make(map[string]struct{})
	for _, p := range strings.Split(strings.TrimRight(string(out), "\x00"), "\x00") {
		if p != "" {
			ignored[p] = struct{}{}
		}
	}
	var kept []string
	for _, p := range paths {
		if _, isIgnored := ignored[p]; isIgnored {
			continue
		}
		kept = append(kept, p)
	}
	return kept
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
	// hk-77q8e: AGENT_COMMS.md was the v0 file-outbox comms channel (retired by
	// hk-8sm4f — use `harmonik comms send/recv` instead). The exemption is kept
	// for the live-transition period: any session still tailing the old file must
	// not cause a false implementer_escape on in-flight beads.
	case path == "AGENT_COMMS.md":
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

// emitSpawnCapBlocked emits a spawn_cap_blocked event (hk-4l7zs) when a launch's
// SpawnWindow times out waiting for a spawn-semaphore slot — the observable
// signature of a slot leak (every slot held by an acquired-but-never-released
// session). Non-fatal: emit-marshal errors are silently discarded; the launch
// failure is already surfaced via the reopen/done path.
//
// capSize/slotsInUse describe the saturated pool; when unknown (0) the payload
// still validates via a minimum capSize of 1 so the event is never dropped.
func emitSpawnCapBlocked(ctx context.Context, bus handlercontract.EventEmitter, runID core.RunID, waited time.Duration, slotsInUse, capSize int) {
	if bus == nil {
		return
	}
	if capSize <= 0 {
		capSize = 1
	}
	waitedMS := waited.Milliseconds()
	if waitedMS <= 0 {
		waitedMS = 1
	}
	pl := core.SpawnCapBlockedPayload{
		RunID:      runID.String(),
		WaitedMS:   waitedMS,
		SlotsInUse: slotsInUse,
		CapSize:    capSize,
	}
	b, err := json.Marshal(pl)
	if err != nil {
		return
	}
	_ = bus.EmitWithRunID(ctx, runID, core.EventTypeSpawnCapBlocked, b)
}

// emitTmuxNewWindowTimeout emits a tmux_new_window_timeout event (hk-r1rup) when
// a launch's SpawnWindow times out waiting for the underlying `tmux new-window`
// call to return — the observable signature of a hung tmux invocation (the
// no-spawn wedge). Non-fatal: emit-marshal errors are silently discarded; the
// launch failure is already surfaced via the reopen/done path.
//
// waited is the duration the new-window call blocked before the bound fired;
// when unknown (<= 0) the payload still validates via a minimum waited_ms of 1
// so the event is never dropped. Mirrors emitSpawnCapBlocked (hk-4l7zs).
func emitTmuxNewWindowTimeout(ctx context.Context, bus handlercontract.EventEmitter, runID core.RunID, waited time.Duration) {
	if bus == nil {
		return
	}
	waitedMS := waited.Milliseconds()
	if waitedMS <= 0 {
		waitedMS = 1
	}
	pl := core.TmuxNewWindowTimeoutPayload{
		RunID:    runID.String(),
		WaitedMS: waitedMS,
	}
	b, err := json.Marshal(pl)
	if err != nil {
		return
	}
	_ = bus.EmitWithRunID(ctx, runID, core.EventTypeTmuxNewWindowTimeout, b)
}

// emitAgentReadyTimeout emits an agent_ready_timeout event (hk-5cox8) when
// the HC-056 timeout fires — no agent_ready relay message arrived within the
// configured deadline. The event carries run_id, claude_session_id, and
// artifactAgentType returns the resolved agent type from claudeRunArtifacts,
// falling back to core.AgentTypeClaudeCode when the field is empty (e.g. from a
// legacy test fixture that builds artifacts directly without going through
// routedLaunchSpecBuilder).
//
// Used to look up the correct Adapter via adapterRegistry.ForAgent instead of
// hardcoding core.AgentTypeClaudeCode (T12, hk-xhawy).
func artifactAgentType(a claudeRunArtifacts) core.AgentType {
	if a.resolvedAgentType.Valid() {
		return a.resolvedAgentType
	}
	return core.AgentTypeClaudeCode
}

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

// transitionToTerminated advances the per-session lifecycle Machine from its
// current state to StateTerminated (clean exit) or StateFailed (error exit),
// driving through StateTerminating if needed (HC-065).
//
// Called by beadRunOne after waitWithSocketGrace returns so that EVERY exit
// path (normal, cancel, crash) reaches a terminal state. Transitions that
// are invalid for the current Machine state (e.g. machine already in
// StateFailed from an agent_failed progress-stream event) are silently
// ignored.
//
// A lifecycle_transition event is emitted to the bus for each successful
// Machine transition. ctx SHOULD be a live (non-cancelled) context so that
// the emission reaches the bus; callers MUST pass context.Background() if
// the run context may already be cancelled.
//
// Spec ref: handler-contract.md §4.13 HC-065; event-model.md §8.3.14.
// Bead ref: hk-xrygh.
func transitionToTerminated(ctx context.Context, m *hclifecycle.Machine, runID core.RunID, bus handlercontract.EventEmitter, exitCode int, waitErr error) {
	if m == nil {
		return
	}
	// Step 1: Terminating (current → Terminating). The machine may already be
	// there (e.g. Kill was called earlier) — the Machine silently rejects
	// invalid transitions.
	emitWorkloopLifecycleTransition(ctx, m, runID, bus,
		hclifecycle.StateTerminating, hclifecycle.ReasonTerminateRequested, "", "")

	// Step 2: Terminal state based on exit outcome.
	if exitCode == 0 && waitErr == nil {
		emitWorkloopLifecycleTransition(ctx, m, runID, bus,
			hclifecycle.StateTerminated, hclifecycle.ReasonTerminateComplete, "", "")
	} else {
		errCode := "exit_error"
		errMsg := fmt.Sprintf("exit=%d", exitCode)
		if waitErr != nil {
			errMsg = waitErr.Error()
		}
		emitWorkloopLifecycleTransition(ctx, m, runID, bus,
			hclifecycle.StateFailed, hclifecycle.ReasonError, errCode, errMsg)
	}
}

// emitWorkloopLifecycleTransition performs a lifecycle Machine transition and
// emits a lifecycle_transition event to the bus (HC-065, §8.3.14).
// Invalid transitions are silently ignored; emission failures are best-effort.
func emitWorkloopLifecycleTransition(ctx context.Context, m *hclifecycle.Machine, runID core.RunID, bus handlercontract.EventEmitter, to hclifecycle.LifecycleState, reason hclifecycle.TransitionReason, errCode, errMsg string) {
	from := m.Current()
	if err := m.Transition(to, reason, errCode, errMsg); err != nil {
		return // invalid transition (e.g. already terminal): silent no-op
	}
	p := core.LifecycleTransitionPayload{
		SessionID:      core.SessionID(m.SessionID()),
		FromState:      from.String(),
		ToState:        to.String(),
		Reason:         string(reason),
		TransitionedAt: time.Now().Format(time.RFC3339Nano),
		ErrCode:        errCode,
		ErrMsg:         errMsg,
	}
	b, err := json.Marshal(p)
	if err != nil {
		return
	}
	_ = bus.EmitWithRunID(ctx, runID, core.EventTypeLifecycleTransition, b)
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
