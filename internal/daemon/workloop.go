package daemon

// workloop.go — main work loop for the harmonik daemon.
//
// RunWorkLoop polls the bead ledger for ready work, claims beads up to
// MaxConcurrent at a time, materialises git worktrees, spawns handler
// subprocesses, and closes (or reopens) beads based on outcome.
//
// # Concurrency model (hk-e61c3.2, POST_OPERATIONAL_PARALLELISM_ROADMAP row 5)
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
// Spec refs: specs/execution-model.md §4.11 EM-049 (in-flight-run capacity gate:
// daemon MUST cap concurrent runs at max_concurrent); §4.11 EM-050 (claim-write
// serialization: token-pool of size max_concurrent before ClaimBead);
// §4.11 EM-051 (max_concurrent configuration: ≥ 1, default 1, sealed at startup).
//
// # Configurable binary
//
// HandlerBinary on daemon.Config controls which binary is spawned. The
// exploratory testing wave injects a twin binary rather than "claude" so that
// no API credits are consumed during wave runs. If HandlerBinary is empty the
// loop defaults to "claude".
//
// Spec ref: EARLY_ROADMAP.md row #10; specs/execution-model.md §4.3 EM-013 (run_id
// as join key); specs/event-model.md §8.1 (run_started / run_completed events).
// Beads: hk-ecrxy (work loop), hk-e61c3.2 (parallelism).

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
	"sync/atomic"
	"time"

	"github.com/gregberns/harmonik/internal/brcli"
	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon/bootconfig"
	"github.com/gregberns/harmonik/internal/gitprobe"
	"github.com/gregberns/harmonik/internal/handler"
	"github.com/gregberns/harmonik/internal/handlercontract"
	hclifecycle "github.com/gregberns/harmonik/internal/handlercontract/lifecycle"
	"github.com/gregberns/harmonik/internal/harness/claude"
	"github.com/gregberns/harmonik/internal/harness/codex"
	"github.com/gregberns/harmonik/internal/harness/pi"
	"github.com/gregberns/harmonik/internal/harness/shared"
	"github.com/gregberns/harmonik/internal/lifecycle"
	tmuxpkg "github.com/gregberns/harmonik/internal/lifecycle/tmux"
	"github.com/gregberns/harmonik/internal/mergeq"
	"github.com/gregberns/harmonik/internal/projectconfig"
	"github.com/gregberns/harmonik/internal/queue"
	"github.com/gregberns/harmonik/internal/queuewiring"
	runpkg "github.com/gregberns/harmonik/internal/run"
	"github.com/gregberns/harmonik/internal/runexec"
	"github.com/gregberns/harmonik/internal/runlaunch"
	"github.com/gregberns/harmonik/internal/runlease"
	"github.com/gregberns/harmonik/internal/runloop"
	"github.com/gregberns/harmonik/internal/runmerge"
	"github.com/gregberns/harmonik/internal/schedule"
	"github.com/gregberns/harmonik/internal/sentinel"
	"github.com/gregberns/harmonik/internal/sessiondata"
	"github.com/gregberns/harmonik/internal/substrate"
	codesyncpkg "github.com/gregberns/harmonik/internal/transport/codesync"
	tunnelpkg "github.com/gregberns/harmonik/internal/transport/tunnel"
	"github.com/gregberns/harmonik/internal/workers"
	"github.com/gregberns/harmonik/internal/workflow"
	"github.com/gregberns/harmonik/internal/workflow/dot"
	"github.com/gregberns/harmonik/internal/workspace"
)

// dashboardGateEvalInterval is the minimum interval between successive
// dashboard forcing-gate evaluations (hk-xg6rw). The gate reads two small
// JSON files (dashboard.json, lanes.json) plus config.yaml; rate-limiting
// avoids doing that disk I/O on every 2s poll tick while still reacting
// promptly to a captain's refresh.
const dashboardGateEvalInterval = 30 * time.Second

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
	// into shared.LaunchCtx so MaterializeClaudeSettings emits absolute-path hook
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
	// uses runRegistry.Len() for liveness/accounting, but the dispatch gate
	// (hk-hs7ex) uses localInFlight (local-only sub-cap) rather than Len() so
	// that remote worker runs are not counted against the local ceiling. Each
	// dispatched goroutine calls Register on claim and Unregister on exit.
	//
	// MUST be a field on workLoopDeps — NOT a package-level variable (see
	// POST_OPERATIONAL_PARALLELISM_ROADMAP.md §6 anti-pattern).
	//
	// Spec ref: specs/execution-model.md §4.11 EM-049 (in-flight-run capacity gate).
	// Bead ref: hk-e61c3.2.
	runRegistry *RunRegistry

	// maxConcurrent is the ceiling on simultaneously in-flight bead goroutines.
	// Sourced from daemon.Config.MaxConcurrent (zero → 1 per Config godoc).
	// Row 6 (hk-e61c3.1) adds this field to Config; row 5 (this bead) enforces it.
	//
	// POST_OPERATIONAL_PARALLELISM_ROADMAP §6: enforcement lives here, NOT in the bus
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

	// localInFlight counts bead runs currently executing locally (not routed to
	// a remote worker). The split capacity gate (hk-hs7ex) uses this to enforce
	// the local hard sub-cap (= maxConcurrent) independently of remote-worker
	// slot accounting. Incremented in the outer poll loop before the goroutine
	// starts; decremented by the goroutine's defer (or corrected in beadRunOne
	// when a fallback SelectWorker succeeds and turns a "local" dispatch remote).
	//
	// Pointer so all goroutines spawned by runWorkLoop share the same atomic.
	//
	// Bead ref: hk-hs7ex.
	localInFlight *atomic.Int32

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

	// launchSpecBuilder builds the handler.LaunchSpec and shared.LaunchArtifacts for
	// a given bead run. Production always uses buildClaudeLaunchSpec. Test fixtures
	// that do not need real bridge setup (e.g. MaterializeClaudeSettings fsyncs)
	// may inject a lightweight stub via ExportedWorkLoopDeps.
	//
	// When nil, buildClaudeLaunchSpec is used (production default).
	//
	// Bead ref: hk-kqdpf.1.
	launchSpecBuilder func(context.Context, shared.LaunchCtx) (handler.LaunchSpec, shared.LaunchArtifacts, error)

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

	// mergeQ is the merge exclusion domain (RSM-015): an explicit, strictly-FIFO
	// single-owner queue that replaces the historical mergeMu mutex. Every member
	// of the domain — the commit-phase merge (update-ref → push → working-tree
	// reset), the post-merge escaped-worktree check, and the remote base-sync +
	// worktree-add — runs its critical section via mergeQ.Submit, so no two
	// overlap on refs/heads/<target> or the main checkout (hk-yyso7). Build-class
	// work (rebase, go build/vet, gofumpt/gci) runs OUTSIDE the domain in the
	// prepare phase (RSM-017 / RSM-INV-005).
	//
	// Production: newWorkLoopDeps sets this to a non-nil mergeq.New(...) queue;
	// runWorkLoop Start()s its owner goroutine on a background-derived context so
	// the shutdown-drain submission (bgCtx) still executes, and cancels it only
	// after all in-flight bead goroutines have drained. When nil (unit tests that
	// drive a single beadRunOne directly), mergeSubmitFunc runs the critical
	// section inline. Concurrent tests inject a shared, pre-started queue via
	// WithMergeQueue / daemonTestHooks.mergeQ.
	//
	// Bead ref: hk-bnm89 (scenario-test harness hardening), hk-yyso7 (race fix),
	// RSM-012..016 (mergeMu → mergeq split).
	mergeQ *mergeq.Queue

	// worktreeCreateMu, when non-nil, is threaded into workspace.WorktreeRootConfig
	// for remote bead runs via WithCreateMutex so that the git-worktree-add +
	// HEAD-resolve retry loop in workspace.CreateWorktree is serialised across all
	// concurrent remote dispatch goroutines (hk-5qp7z).
	//
	// Rationale: N concurrent "git worktree add" calls against the same shared
	// worker repo race on HEAD/index resolution even when each individual call is
	// retried — the race persists across every retry attempt because the concurrent
	// siblings are also running. A single mutex around the full retry loop prevents
	// any two creates from overlapping on the worker, eliminating the empty-HEAD
	// failure mode at its source.
	//
	// Distinct from mergeMu: mergeMu governs merge/rebase/push serialisation;
	// worktreeCreateMu governs worktree creation only, so the two operations can
	// proceed independently (a merge does not block a create, and vice versa —
	// unlike the current implicit serialisation under mergeMu which also serialises
	// the codesync fetch-base with the create for hk-lt091 correctness; that invariant is
	// preserved because both fetch and create remain inside mergeMu).
	//
	// Production: newWorkLoopDeps always sets this to a non-nil &sync.Mutex{}.
	// Bead ref: hk-5qp7z.
	//
	// SINGLE-WORKER ASSUMPTION (V1): one mutex for the whole daemon, while the
	// thing it protects — one shared worker repo racing on HEAD/index — is a
	// property of ONE worker box. With a second execution target this over-
	// serialises: two targets with separate repos would take turns for no
	// reason. It needs to become one lock per target, keyed the same way the
	// cold-start cap will be. Same note on agentSpawnSem, for the mirror-image
	// reason (it under-serialises rather than over-serialises).
	worktreeCreateMu *sync.Mutex

	// agentSpawnSem, when non-nil, is a counting semaphore (capacity 3) that
	// bounds how many claude cold-start agent spawns may be in flight
	// concurrently on a single remote worker (hk-5z1f0). Acquired immediately
	// before the remote agent Launch and released once agent_ready resolves
	// (success, failure, or timeout).
	//
	// Rationale: under the 10-concurrent ramp a worker may host up to 6
	// simultaneous agents (implementer + reviewer per run). The 2nd cold-start
	// claude spawn — the REVIEW-stage agent — competes for CPU/disk/tunnel
	// readiness and recurrently trips agent_ready_timeout over the reverse SSH
	// tunnel. Bounding concurrent cold-starts to 3 keeps each spawn's warm-up
	// window inside the agent_ready deadline without serialising the whole run
	// (the semaphore is released as soon as the agent is ready, so it gates
	// cold-start only, not the run body).
	//
	// SINGLE-WORKER ASSUMPTION (V1): this is ONE channel for the whole daemon,
	// created once in newWorkLoopDeps. It reads as per-worker only because
	// workers.Load rejects a second worker entry (ErrTooManyWorkers), so the
	// daemon-wide cap and the per-worker cap are the same number today.
	//
	// The resource it protects is per-worker, not per-daemon. Everything inside
	// the window happens on the worker box — the tmux new-window, the agent's
	// own boot, the hook dial back. The proof is the guard: a LOCAL cold start
	// costs the daemon strictly more, because the agent runs there, and takes no
	// token at all. So when more than one execution target becomes possible,
	// each target needs its own cold-start cap or one busy target will starve
	// the others. Same note on worktreeCreateMu, for the same reason.
	//
	// Do NOT pre-build that as per-worker state in internal/workers: the SSH
	// remote-worker model is being replaced rather than extended (see
	// plans/2026-07-21-p3-distributed-execution). The constraint belongs to
	// whatever admits the second target. Recorded there.
	//
	// Remote-only: acquisition is guarded on rbc != nil, mirroring the
	// reverse-tunnel readiness gate — local runs never construct a tunnel and
	// are never gated.
	//
	// Production: newWorkLoopDeps always sets this to a non-nil channel of cap 3.
	// Bead ref: hk-5z1f0.
	agentSpawnSem chan struct{}

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

	// substrate is the optional tmux-substrate for handler.Launch.  This is
	// always nil; handler falls back to exec.CommandContext.  When non-nil it
	// is attached to the LaunchSpec.Substrate field so the handler spawns the
	// subprocess inside a tmux window.
	//
	// Spec ref: specs/process-lifecycle.md §4.7 PL-021b.
	// Bead ref: hk-gql20.14.
	substrate handler.Substrate

	// reviewerSubstrate is the claude reviewer/gate substrate from
	// cfg.ReviewerSubstrate; nil falls back to substrate.
	reviewerSubstrate handler.Substrate

	// clock is the determinism port through which the RUN path reads time
	// (RSM-013 / M3-D4). Production wires substrate.SystemClock{}; tests inject
	// substrate.FakeClock so agent-ready / reap timeouts replay in virtual time
	// without wall-clock sleeps. Mirrors the P1 T5 keeper ClockPort migration.
	clock substrate.ClockPort

	// spawnSubstrateReadyCh, when non-nil, is awaited at the START of runWorkLoop
	// before the first dispatch tick. daemon.Start closes this channel once the
	// spawn substrate has been probed for readiness after a restart-backoff boot
	// (hk-bk33). Nil on normal (no-backoff) boots — the gate is bypassed.
	//
	// Bead ref: hk-bk33.
	spawnSubstrateReadyCh <-chan struct{}

	// agentReadyTimeout is the maximum duration waitAgentReady blocks waiting
	// for an agent_ready event per HC-056.  Zero → runlaunch.DefaultAgentReadyTimeout (30s).
	// Sourced from Config.AgentReadyTimeout (also zero-value safe).
	//
	// Spec ref: specs/handler-contract.md §4.9 HC-056.
	// Bead ref: hk-gql20.14.
	agentReadyTimeout time.Duration

	// remoteAgentReadyTimeout is agentReadyTimeout's counterpart for a dispatch
	// routed to a REMOTE (SSH worker) node. Zero → runlaunch.DefaultRemoteAgentReadyTimeout
	// (210s). Sourced from Config.RemoteAgentReadyTimeout (zero-value safe).
	// Resolved via runlaunch.EffectiveAgentReadyTimeout at each waitAgentReady call site
	// that has a remote/local signal in scope.
	//
	// Spec ref: specs/handler-contract.md §4.9 HC-056.
	// Bead ref: hk-96d7w (LOCAL slice of hk-5z1f0).
	remoteAgentReadyTimeout time.Duration

	// projectCfg is the decoded .harmonik/config.yaml loaded once at startup
	// (EM-012b tier-2). The zero value is safe: LookupAgent returns ("","") for
	// all agent types. Passed to ResolveModelPreference at claim time.
	//
	// Spec ref: specs/execution-model.md §4.3 EM-012b.
	// Bead ref: hk-bfvk7.
	projectCfg projectconfig.ProjectConfig

	// defaultHarness is the tier-4 (global) default for the harness-selection
	// precedence walk (resolveHarness in harnessresolve.go). Sourced from
	// Config.DefaultHarness; empty means fall through to the built-in
	// claude-code fallback. Wired from newWorkLoopDeps and threaded into
	// routedLaunchSpecBuilder on every bead dispatch (hk-ytzj2).
	//
	// Bead ref: hk-ytzj2.
	defaultHarness core.AgentType

	// queueStore is the daemon-singleton holder for the active *queue.Queue.
	// When non-nil and a queue is loaded, the dispatch loop pulls work from the
	// active queue group rather than polling br ready. When nil or when no queue
	// is loaded, the loop falls back to the br-ready poll path (backward-compat
	// for tests that do not use queues).
	//
	// Spec ref: specs/execution-model.md §7.4 (TS-1 dispatch loop); §4.3.EM-015f
	// (group-advance gate).
	// Bead ref: hk-45ude.
	queueStore *queuewiring.QueueStore

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
	// queuewiring.NewBRQueueLedger(brAdapter); tests inject a fake. When nil the re-evaluation
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

	// queueWriteErrorReported tracks queue names for which the QM-001 failed-write
	// report has already gone out. The store quarantines a queue after a failed
	// write, so every later tick re-derives the same failure; without this the
	// dispatch loop would re-emit the pair every poll interval forever.
	//
	// Only the outer poll loop reads/writes this map — NOT per-bead goroutines.
	// Access is single-threaded, matching heldEventDedup.
	queueWriteErrorReported map[string]struct{}

	// staleBlockerCloser, when non-nil, is used by the claim-failure path to
	// auto-close stale blockers (beads already subsumed in main) so the blocked
	// bead can be retried on the next workloop iteration. When nil the
	// auto-close behaviour is disabled (backward-compat for test stubs that do
	// not need it). Production wires the *brcli.Adapter (which satisfies
	// lifecycle.BeadCat3cCloser via SweepCloseBead).
	//
	// Bead ref: hk-rnsjs.
	staleBlockerCloser lifecycle.BeadCat3cCloser

	// strandedInProgressResetter, when non-nil, auto-resets an in_progress bead
	// observed at pre-claim with no active run — breaking the bead_claim_skipped
	// live-lock that starves sibling queue items (hk-l2xd1). Nil in test stubs
	// that do not exercise this path (backward-compat default).
	//
	// Bead ref: hk-l2xd1.
	strandedInProgressResetter strandedInProgressResetter

	// strandedResetProjectHash is the project hash used in ResetBead idempotency
	// keys for stranded-bead auto-resets (hk-l2xd1). Same value as
	// coordinatorReapProjectHash; stored separately for clarity.
	//
	// Bead ref: hk-l2xd1.
	strandedResetProjectHash core.ProjectHash

	// strandedResetDaemonNS is the daemon-session epoch (nanoseconds since the
	// Unix epoch, captured once at newWorkLoopDeps time) used to scope ResetBead
	// idempotency keys to a single daemon session (hk-l2xd1).
	//
	// Bead ref: hk-l2xd1.
	strandedResetDaemonNS int64

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

	// runner is the CommandRunner threaded into the DOT run path for remote-aware
	// marker-file reads (hk-hd2w6). nil for local runs (NFR7: byte-identical
	// box-A path). Set from Config.Runner at startup so the contract test can
	// inject a RecordingRunner without a full remote-substrate setup; overridden
	// at dispatch time by rbc.sshRunner for live remote runs.
	//
	// Bead ref: hk-hd2w6.
	runner tmuxpkg.CommandRunner

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

	// commsSend fires a comms-send schedule action (WE6). Production wires
	// shellCommsSend (execs harmonik comms send directly — no bash -c wrapper);
	// tests inject a recording double. nil → comms-send actions return an error
	// (no sender configured).
	//
	// Bead ref: hk-we6-watch-scheduled-send-6onfu.
	commsSend commsSendFunc

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

	// NOTE (RSM-011): the periodic-maintenance VALUE fields formerly here —
	// lastCoordinatorReap, lastDiskCheck, diskLow — were lifted out onto
	// runWorkLoop-local loopMaintenanceState. workLoopDeps is passed BY VALUE
	// into every run goroutine, so a mutation of these fields from a run
	// goroutine would be a silent no-op (PF §3 hazard). Keeping them off the
	// bundle makes the ownership (the single work-loop goroutine) structural.

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

	// codexNoWorkDurationFloor overrides codexNoWorkDurationFloorDefault (10s),
	// the implement-phase duration below which a codexRefsNoChange outcome is
	// flagged as a no-work run.  Zero → the default.  Production leaves this
	// zero; the measured no-work/real-work gap is ~5x wide, so the value is not
	// delicate.
	//
	// Bead ref: hk-368i4.
	codexNoWorkDurationFloor time.Duration

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

	// worktreeReclaimFunc, when non-nil, replaces the `git worktree remove`
	// sequence in reclaimStaleWorktrees. Tests inject this to capture which
	// stale paths would have been removed without touching the real filesystem.
	// When nil, the production git-worktree-remove+prune sequence is used.
	//
	// Bead ref: hk-5uezz.
	worktreeReclaimFunc func(ctx context.Context, projectDir string, stalePaths []string) error

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

	// sandboxCfg holds the sandbox: block from .harmonik/config.yaml (hk-6596l).
	// When Backend == "" the block was absent and no sandboxing occurs. When
	// Backend == "srt", beadRunOne wires SrtSpawnConfig onto the perRunSubstrate
	// for every harness listed in Harnesses. Zero value = no sandboxing.
	//
	// Bead ref: hk-6596l.
	sandboxCfg projectconfig.SandboxConfig
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

// beadLedger is the work loop's Beads-ledger interface (the subset of
// brcli.Adapter it uses, extracted so tests can substitute a stub). The
// interface itself moved to internal/runloop (LIFT L0) because SharedHandles —
// which carries it — now lives there; this alias keeps the daemon's uses (the
// workLoopDeps.brAdapter field, resolveOwningEpicFromRecord, ~25 test stubs)
// spelled with the local name. The architectural note (bead-body access, the
// pre-claim ShowBead guard hk-p4xbw / hk-33tcf) lives with the definition in
// internal/runloop/ports.go.
type beadLedger = runloop.BeadLedger

// strandedInProgressResetter is the subset of brcli.Adapter used to auto-reset
// an in_progress bead that has no active run (hk-l2xd1). Separated from
// beadLedger so existing test stubs do not need to implement ResetBead.
type strandedInProgressResetter interface {
	ResetBead(
		ctx context.Context,
		intentLogDir string,
		cfg brcli.TimeoutConfig,
		beadID core.BeadID,
		projectHash core.ProjectHash,
		daemonStartNS int64,
	) error
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
func newWorkLoopDeps(ctx context.Context, cfg Config, bus handlercontract.EventEmitter, workflowModeDefault core.WorkflowMode, registry *handlercontract.AdapterRegistry, store hookStoreIface) (workLoopDeps, error) {
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
	// Config.HandlerEnv is nil (the default).  Callers that supply their own
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
	// through this registry. Pi config is threaded from the decoded
	// .harmonik/config.yaml harnesses.pi block (hk-f8u5j: config→harness seam).
	harnessReg, hErr := newHarnessRegistry(cfg.ProjectCfg.Harnesses.Pi)
	if hErr != nil {
		return workLoopDeps{}, fmt.Errorf("daemon: newWorkLoopDeps: newHarnessRegistry: %w", hErr)
	}

	// Build the remote-worker registry from cfg.Workers and run the boot-time
	// health check (remote-substrate B4/B6). Returns nil when no worker is
	// enabled so the dispatch path takes the existing local-only branch (NFR7).
	// The nil guard is load-bearing: bus is a handlercontract.EventEmitter
	// INTERFACE, so `bus.Emit` on a nil bus is a method value on a nil interface
	// and panics at the call site. Building the workers.EmitFunc here keeps the
	// pre-E4c behaviour — a nil bus degrades to no-emit, it does not crash boot.
	var workerEmit workers.EmitFunc
	if bus != nil {
		workerEmit = bus.Emit
	}
	workerReg := workers.BuildRegistry(ctx, cfg.Workers, workerEmit)

	// M4-C3: hand the SAME live registry to the composition root's Codex
	// runner-selection seam so a worker-selected codexdriver run routes over
	// SSHRunner. No-op for the tmux path (observer nil). The driver never sees
	// this — selection stays at the wire/root (RS-017 twin-blindness).
	if cfg.WorkerRegistryObserver != nil {
		cfg.WorkerRegistryObserver(workerReg)
	}

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
		localInFlight:              new(atomic.Int32), // hk-hs7ex: split gate — local sub-cap counter
		hookStore:                  store,
		cpRegistry:                 cfg.CPRegistry, // hk-karlz: ControlPoint registry for gate-node dispatch
		adapterRegistry:            registry,
		harnessRegistry:            harnessReg,    // hk-hj9ld: per-agent-type Harness route table (claude-only in T3)
		substrate:                  cfg.Substrate, // nil falls back to exec.CommandContext; set by composition root (hk-kqdpf.4)
		reviewerSubstrate:          cfg.ReviewerSubstrate,
		clock:                      substrate.SystemClock{}, // RSM-013 / M3-D4: run-path determinism port (SystemClock in prod, FakeClock in tests)
		agentReadyTimeout:          cfg.AgentReadyTimeout,
		remoteAgentReadyTimeout:    cfg.RemoteAgentReadyTimeout, // hk-96d7w: remote-worker agent_ready wait window
		cancelOnQueueDrain:         cfg.CancelOnQueueDrain,
		projectCfg:                 cfg.ProjectCfg,
		defaultHarness:             cfg.DefaultHarness,                    // hk-ytzj2: tier-4 global harness default wired from Config
		queueStore:                 nil,                                   // populated by daemon.Start after wiring QueueStore (hk-45ude)
		queueLedger:                queuewiring.NewBRQueueLedger(adapter), // hk-nbjht: re-eval deferred-for-ledger-dep items on every dispatch tick (§2.8)
		staleBlockerCloser:         adapter,                               // hk-rnsjs: auto-close stale blockers on claim failure
		strandedInProgressResetter: adapter,                               // hk-l2xd1: auto-reset in_progress bead with no run
		strandedResetProjectHash:   projectHash,                           // hk-l2xd1: idempotency key component
		strandedResetDaemonNS:      time.Now().UnixNano(),                 // hk-l2xd1: daemon-session epoch for idempotency key scoping
		kerfPath:                   cfg.KerfPath,                          // hk-9321v: kerf next for EM-062/EM-063 eager-refill
		brPath:                     cfg.BrPath,                            // hk-f722: staged-bead generator br create
		followUpLedger:             make(map[string]struct{}),             // hk-f722: at-most-once guard per daemon session
		followUpLedgerMu:           &sync.Mutex{},
		followUpLedgerPath:         filepath.Join(cfg.ProjectDir, ".harmonik", followUpLedgerFileName), // hk-3ndb: durable ledger path
		noAutoPull:                 cfg.NoAutoPull,                                                     // hk-exd7m: queue-only mode for flywheel topology
		skipBrHistoryRotation:      cfg.SkipBrHistoryRotation,                                          // hk-hypbi: per-close .br_history trim
		// mergeQ (RSM-015 merge exclusion domain) is left nil here: runWorkLoop
		// creates AND owns the production queue (starts its owner, cancels on
		// return after the drain). A test may inject a pre-started queue via
		// WithMergeQueue, which runWorkLoop then leaves untouched (hk-yyso7).
		worktreeCreateMu:           &sync.Mutex{},                  // hk-5qp7z: global worktree-create serialisation for remote runs
		agentSpawnSem:              make(chan struct{}, 3),         // hk-5z1f0: cold-start spawn semaphore (cap 3, remote-only). ONE per daemon; per-worker only because v1 admits one worker — see the take site.
		cacheReapMu:                &sync.RWMutex{},                // hk-y3frr: reap↔dispatch exclusion
		emittedEpics:               make(map[core.BeadID]struct{}), // hk-w6y70: at-most-once guard per daemon session
		emittedEpicsMu:             &sync.Mutex{},
		targetBranch:               bootconfig.ResolveTargetBranch(cfg.TargetBranch),
		protectBranches:            cfg.ProtectBranches,
		allowedRepos:               cfg.ProjectCfg.Daemon.AllowedRepos, // hk-xfuc: cross-repo dispatch safelist
		workerRegistry:             workerReg,                          // remote-substrate B4/B8: nil → local-only dispatch (NFR7)
		coordinatorReapAdapter:     coordinatorReapAdapter,             // hk-t08m: periodic flywheel-coordinator reaper
		coordinatorReapProjectHash: projectHash,                        // hk-t08m: pre-computed for session name derivation
		runner:                     cfg.Runner,                         // hk-hd2w6: test injection / Config.Runner seam
		sandboxCfg:                 cfg.ProjectCfg.Sandbox,             // hk-6596l: srt sandbox config block
	}, nil
}

// buildRunBundles assembles the per-run RunPorts + SharedHandles for a bead run
// and resolves the routed launch builder ONCE, threading it onto RunPorts so
// every sub-driver reaches it via ports.LaunchBuilder / ports.Launch. It is the
// caller-side relocation of the resolver that used to live inside beadRunOne
// (RT18.11): performing it here — where deps is still live — is what lets
// beadRunOne take the bundles as parameters instead of deps.
//
// The routing selection is load-bearing. A test fixture may pre-inject
// deps.launchSpecBuilder; that carve-out is preserved byte-for-byte. Production
// (nil) resolves from the harness registry + the bead's tier-1 labels, so a
// codex/pi-labelled bead routes to its harness rather than silently falling to
// the claude builder.
func (deps *workLoopDeps) buildRunBundles(env runloop.RunEnv) (runloop.RunPorts, runloop.SharedHandles) {
	rp := deps.runPorts()
	handles := deps.sharedHandles()
	builder := deps.launchSpecBuilder
	if builder == nil {
		if handles.HarnessRegistry != nil {
			builder = routedLaunchSpecBuilder(
				handles.HarnessRegistry,
				env.BeadRecord,
				env.QueueDefaultHarness,
				core.AgentType(""), // node default: overridden per-node in driveDotWorkflow (T5/T12)
				env.DefaultHarness, // global default: Config.DefaultHarness (empty → built-in claude-code)
				rp.Emitter,
			)
		} else {
			// No registry (legacy test fixtures): fall back to direct claude builder.
			builder = claude.BuildLaunchSpec
		}
	}
	rp.Launch = launchPort(builder)
	rp.LaunchBuilder = builder
	return rp, handles
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
// env carries the immutable per-run values (RSM-010): the daemon-level config
// plus the dispatched item's identity and per-item overrides. env.QueueID and
// env.QueueGroupIndex are optional: when non-nil they are stamped into
// run_started / run_completed / run_failed payloads per EM-015a/EM-015b and
// QM-011/QM-012. They are nil for non-queue-dispatched runs.
//
// The returned success flag is the Run machine's terminal state (RSM-022): true
// only when the run reached Done{closed, success}. The goroutine wrapper in
// runWorkLoop reads it to drive the EM-015f group-advance evaluation and the
// hk-f722 staged-generator eval. Early guard returns (before the Run bridge
// exists) report false.
//
// Bead ref: hk-e61c3.2, hk-45ude.
//
//nolint:funlen,gocognit,cyclop // pre-existing: beadRunOne is the run-path giant the RT ports stream (RT15-RT20) exists to decompose; the signature change re-anchors the grandfathered findings and splitting the body here would defeat the behaviour-preserving property of the slice
func beadRunOne(ctx context.Context, env runloop.RunEnv, rp runloop.RunPorts, handles runloop.SharedHandles, extraContext string, preSelectedWorker *workers.Worker, localSlotHeld bool) (succeeded bool) {
	// RSM-010: alias the per-run values off env under the names the body already
	// uses. Aliasing rather than rewriting ~140 reads is what keeps the
	// signature change behaviour-obvious.
	//
	// The four per-item override fields are NOT aliased here. Every one of them
	// is a tier-0 INPUT to the run plan below, and the body must read the plan's
	// RESOLVED answer instead. The workflow ref is the reason this matters: an
	// alias of the raw field would shadow the resolved one, and a reader that
	// kept the alias would silently take the unresolved value. Leaving the name
	// undefined makes such a reader a build failure rather than a bug.
	runID, beadRecord := env.RunID, env.BeadRecord
	queueName, queueID := env.QueueName, env.QueueID
	queueGroupIndex, queueItemIndex := env.QueueGroupIndex, env.QueueItemIndex
	itemTemplateParams := env.ItemTemplateParams
	// mport.Submit() is the merge exclusion-domain submit surface (RSM-015).
	mport := rp.Merge
	// RSM-010: the run's EmitterPort, off the bundle rp already holds.
	// EmitterPort is a type ALIAS for handlercontract.EventEmitter
	// (runports.go), so this is the same value and the same static type the
	// 24 emissions below already used — only the spelling changes.
	emit := rp.Emitter
	beadID := beadRecord.BeadID

	// ── The run's resources (RSM-036 … RSM-038) ─────────────────────────────
	//
	// runScope holds every resource this run takes, and the deferred close gives
	// them back in the reverse of the order they were taken, under ONE
	// disposition read once for the whole run rather than a predicate per
	// release site.
	//
	// The close is registered HERE, above every acquisition, for two reasons.
	// It must run after every other give-back, because the outermost resources —
	// the worker slot and the local count — are given back last. And a resource
	// taken on a path that has already passed this line would be held by a scope
	// nobody will close again.
	//
	// runExit is a function because two of the three facts the disposition
	// depends on do not exist yet: whether the agent got a tmux session of its
	// own, and whether a Pi run failed with output worth reading, are both known
	// only after the launch. It is replaced below, once they exist. Until then
	// the run holds only resources that every disposition gives back, so the
	// partial answer here cannot keep anything standing.
	runScope := &runlease.Scope{}
	runExit := func() runlease.Exit { return runlease.Exit{DaemonStopping: ctx.Err() != nil} }
	defer func() {
		if relErr := runScope.Close(runlease.Decide(runExit())).Err(); relErr != nil {
			fmt.Fprintf(os.Stderr, "daemon: workloop: bead %s run %s: giving resources back: %v\n",
				beadID, runID.String(), relErr)
		}
	}()

	// hk-hs7ex: the outer loop increments localInFlight before it starts this run,
	// and this run gives it back. The lease replaces a mutable flag: the fallback
	// worker selection below turns a "local" dispatch into a remote one, and it
	// gives the count back THERE, through this lease, rather than switching off a
	// deferred cleanup. A run that never held the count gets a lease born spent,
	// so the give-back site needs no test for whether there is anything to give.
	localSlot := runlease.Hold(runlease.LocalSlot, nil)
	if localSlotHeld && handles.LocalInFlight != nil {
		localSlot = runScope.Hold(runlease.LocalSlot, func() error {
			handles.LocalInFlight.Add(-1)
			return nil
		})
	}

	// hk-3hozm: give the pre-reserved REMOTE worker slot back on ANY exit path,
	// including the four refuse-before-launch early returns below (bad pi profile,
	// CrossRepoUnsafeError, unresolvable start_from/parent commit, LandsOnProtected).
	// The outer dispatch loop pre-reserved this slot via SelectWorker and the
	// caller MUST balance it with ReleaseSlot. That release was once registered
	// only at the remote-runner setup far below, AFTER those early returns — so a
	// refused remote bead reopened and returned without releasing, permanently
	// over-counting the registry until HasFreeSlot() was false for ever and the
	// remote path wedged. Holding it here, above every early return, is what fixed
	// that, and the scope is what makes it fire exactly once.
	//
	// Keyed on preSelectedWorker so it is inert for the fallback path, which is
	// mutually exclusive — it runs only when rbc == nil, so preSelectedWorker is
	// nil — and takes a slot of its own after these early returns.
	if preSelectedWorker != nil && handles.Workers != nil {
		runScope.Hold(runlease.WorkerSlot, func() error {
			handles.Workers.ReleaseSlot()
			return nil
		})
	}

	// runTipSHA is set (in the DOT failure path) to the worktree HEAD SHA when
	// HEAD has advanced past the parent commit — meaning the implementer produced
	// a commit that the gate later bounced. Included in run_failed so operators
	// can salvage the stranded run-branch commit (hk-8b35c orphan-salvage).
	var runTipSHA *string

	// Resolve owning-epic attribution (hk-7evda, logmine F13): find the parent
	// epic from the bead's edges and look up its assignee (the crew name) so
	// terminal events carry it directly, eliminating captain br round-trips.
	// Best-effort: errors leave the fields empty (non-fatal).
	owningEpicID, owningEpicAssignee := resolveOwningEpicFromRecord(ctx, handles.BrAdapter, beadRecord)
	// Propagate to RunHandle so StaleWatcher can read the attribution without
	// its own br calls.
	if handle, ok := handles.RunRegistry.Get(runID); ok {
		handle.SetOwningEpic(owningEpicID, owningEpicAssignee)
	}

	// sdStartedAt, sdModel, sdHarness are captured by the run-terminal effector
	// for the sessiondata.Collect goroutine. They are assigned after their
	// respective resolutions below (ResolveModelPreference, implHarnessWL).
	sdStartedAt := rp.Clock.Now()
	var sdModel, sdHarness string

	// emitRunTerminalEff is the ActEmitRunTerminal effector binding (RT9,
	// RSM-020/022): it stamps queue_id + queue_group_index onto every
	// run_completed / run_failed event emitted from this run and fires the
	// sessiondata collection. The hk-e3fy background-context swap (a cancelled
	// per-run ctx must not drop the terminal — the daemon is still running) and
	// the RSM-021 drain policy (background batch, NO sessiondata — the pre-RT9
	// drain block never collected) live HERE, as effector policy, instead of
	// being open-coded at each terminal block.
	//
	// Spec ref: specs/execution-model.md §4.3.EM-015b; QM-011/QM-012; RSM-021.
	// Bead ref: hk-45ude.
	emitRunTerminalEff := func(emitCtx context.Context, success bool, summary string, draining bool) { //nolint:contextcheck // hk-e3fy: a cancelled per-run ctx must not drop the terminal; Background swap by design
		if emitCtx.Err() != nil {
			emitCtx = context.Background()
		}
		emitRunCompleted(emitCtx, emit, runID, string(beadID), owningEpicID, owningEpicAssignee, success, summary, queueID, queueGroupIndex, runTipSHA)
		if draining {
			return // RSM-021: the drain batch collects no sessiondata.
		}
		// Fire sessiondata.Collect off the hot path (hk-eval-prog-sessiondata-hook-vmxrk).
		// Best-effort: errors are silently discarded — a missed record is preferable
		// to a panicking goroutine that could affect the daemon.
		sdEndedAt := rp.Clock.Now()
		sdQID := ""
		if queueID != nil {
			sdQID = *queueID
		}
		sdCommitSHA := ""
		if runTipSHA != nil {
			sdCommitSHA = *runTipSHA
		}
		go func() {
			_ = sessiondata.Collect(sessiondata.CollectParams{
				RunID:             runID.String(),
				BeadID:            string(beadID),
				QueueID:           sdQID,
				Harness:           sdHarness,
				Model:             sdModel,
				Success:           success,
				CommitSHA:         sdCommitSHA,
				StartedAt:         sdStartedAt,
				EndedAt:           sdEndedAt,
				ProjectDir:        env.ProjectDir,
				ClaudeProjectsDir: filepath.Join(os.Getenv("HOME"), ".claude", "projects"),
			})
		}()
	}

	// ── The run plan: every decision that precedes an acquisition ────────────
	//
	// Ten decisions resolve here, in workloop_runplan.go: workflow mode and
	// ref, harness, model and effort, Pi provider profile, active repo,
	// protected branches, parent commit, lands_on, and merge target. All ten
	// sit above every acquisition in this function — the worktree, the tunnel
	// port, the agent process, the ssh session on a worker. Five of the
	// decisions can refuse the bead, and a refusal reopens it and returns having
	// taken nothing. Keep that order: nothing between here and the
	// worker-selection block below may acquire a resource.
	//
	// The plan carries the RUN-level harness tuple. A DOT run corrects the
	// harness and the model per node further down — see the note on runPlan.
	plan := resolveRunPlan(ctx, runPlanRequest{
		Env:               env,
		Emit:              emit,
		Handles:           handles,
		PreSelectedWorker: preSelectedWorker,
	})
	if plan.Verdict != runPlanReady {
		refuseRunPlan(ctx, env, handles, emit, plan.Refusal)
		return false
	}

	// Alias the plan's answers under the names the body below already uses.
	workflowMode := plan.WorkflowMode
	resolvedModel, resolvedEffort := plan.Model, plan.Effort
	resolvedProfile := plan.PiProfile
	activeRepo := plan.ActiveRepo
	effectiveMergeProtectBranches := plan.MergeProtectBranches
	headSHA := plan.ParentSHA
	baseBranch := plan.BaseBranch
	mergeTarget := plan.MergeTarget
	sdModel = resolvedModel

	// ── RT7/RT9: the per-run Run reactor bridge (RSM-007, RSM-020..022) ──────
	//
	// The pure Run machine (internal/runexec, composed in runbridge.go) owns the
	// terminal spine for ALL FOUR terminal paths (review-loop, DOT,
	// agent-completed, exit-0): every reopen + run-terminal / close-ladder
	// pairing below rides its actions instead of open-coded blocks. Constructed
	// here, before the worktree critical section, so provisioning-phase failures
	// ride the reopen spine via EvProvisionFailed (RSM-032). The run's success
	// is the machine's terminal state (bridge.Success(), RSM-022).
	// Guard paths that return before (or without) feeding the machine yield the
	// zero value (false); every terminal-spine path returns bridge.Success().
	bridge := runloop.NewRunBridge(env, rp, handles, runID, beadID, workflowMode, emitRunTerminalEff)
	failRun := func(reason, summary string) { bridge.Fail(ctx, reason, summary) }

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
	// rs-tunnel-spawn: the long-lived `ssh -N -R` reverse-tunnel process for this
	// remote run is held on the run's scope, which kills it. It is a local at the
	// spawn site rather than a field here, because nothing outside that site ever
	// read it. workerHookSock is the per-run worker-side reverse-
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
		workerHookSock string
	}
	var rbc *remoteBeadCtx
	// hk-hs7ex: use the worker pre-selected at dispatch time when provided. This
	// avoids a double SelectWorker call and keeps slot accounting consistent with
	// the split gate. The pre-selection was performed by the outer dispatch loop
	// after ClaimBead and before runRegistry.Register.
	if preSelectedWorker != nil {
		rbc = &remoteBeadCtx{
			worker: *preSelectedWorker,
			// hk-zexsj: pin the tmux SSHRunner off the shared SSH ControlMaster.
			sshRunner: tmuxpkg.SSHRunner{Host: preSelectedWorker.Host, Opts: []string{"-o", "ControlMaster=no", "-o", "ControlPath=none"}},
		}
		// hk-3hozm: this pre-reserved slot is held on the run's scope at the top of
		// beadRunOne, so the give-back also covers the refuse-before-launch early
		// returns above. Nothing to do here — a second hold would give the slot
		// back twice.
	}
	// hk-f10xl [L5 Move 2]: per-queue routing gate fallback. Applies when
	// preSelectedWorker is nil (e.g. br-ready path with no available worker at
	// dispatch time, or a race where a worker slot freed up after the outer loop's
	// HasFreeSlot peek). This path is rare after the hk-hs7ex hoist but kept for
	// correctness.
	if rbc == nil && !plan.LocalOnly && handles.Workers != nil {
		var w *workers.Worker
		if plan.WorkerTarget != "" {
			w = handles.Workers.SelectWorkerByName(plan.WorkerTarget)
		} else {
			w = handles.Workers.SelectWorker()
		}
		if w != nil {
			rbc = &remoteBeadCtx{
				worker: *w,
				// hk-zexsj: pin the tmux SSHRunner off the shared SSH ControlMaster
				// (mirroring internal/transport/tunnel's tunnel opts). A churning multiplexed
				// master can silently drop a multiplexed load-buffer / paste-buffer
				// mid-write (the hk-cnp17 truncation family), discarding the seed
				// paste → agent never starts → 30-min timeout. A dedicated,
				// non-multiplexed connection per tmux command removes that failure
				// mode. ssh uses the first value per option, so these precede any
				// worker opts.
				sshRunner: tmuxpkg.SSHRunner{Host: w.Host, Opts: []string{"-o", "ControlMaster=no", "-o", "ControlPath=none"}},
			}
			runScope.Hold(runlease.WorkerSlot, func() error {
				handles.Workers.ReleaseSlot()
				return nil
			})
			// hk-hs7ex: the outer loop incremented localInFlight thinking this was
			// a local run. A worker slot became available between the gate and here,
			// so this run is actually remote and never needed the local count.
			// Giving it back NOW rather than at the end is the point: the increment
			// was made on a guess that is now known to be wrong, and the lease makes
			// the end-of-run give-back a no-op rather than a double decrement.
			//nolint:errcheck // the give-back is an atomic decrement; it cannot fail
			_ = localSlot.Release()
			// hk-4tjt6: mirror the Remote flag update so LenForQueueLocal
			// stops counting this run against the per-queue local cap.
			if h, ok := handles.RunRegistry.Get(runID); ok {
				h.SetRemote(true)
			}
		}
	}
	// gap #7 Option A: ensure the worker's .harmonik/ dir exists, then
	// start the per-run SSH reverse tunnel — BOTH before any agent Launch.
	//
	//  1. workerHookSock is the per-run worker-side TCP endpoint the tunnel
	//     binds (tcp://127.0.0.1:<port>), shared by beads 1, 2, and 3. The
	//     port is allocated from box A's free ephemeral space as a HINT for
	//     sshd's worker-side bind (collision-safe: see
	//     tunnel.AllocatePort + ExitOnForwardFailure=yes).
	//  2. tunnel.EnsureWorkerHarmonikDir (bead 2) mkdir-p's the worker's .harmonik/
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

	// hk-hs7ex: this block is now outside both the pre-selected and fallback
	// worker selection blocks, so it runs for ALL remote runs (rbc != nil)
	// regardless of which selection path set rbc. NFR7: local runs (rbc == nil)
	// skip this block entirely — byte-identical to prior behavior.
	if rbc != nil {
		daemonHookSock := filepath.Join(env.ProjectDir, ".harmonik", "daemon.sock")

		// refuseTunnel reports a fatal reverse-tunnel problem the one way all
		// three gates below report it: a stderr line, a worker_tunnel_failed
		// event, and a best-effort bead reopen. Each caller returns immediately
		// after, so no resource acquired past this point is left held. The three
		// gates used to carry their own copy of this triple and drifted in the
		// stage wording; keeping one reporter is what lets a new gate be added
		// without inventing a fourth spelling.
		refuseTunnel := func(stage, sockPath string, cause error) {
			fmt.Fprintf(os.Stderr,
				"daemon: workloop: reverse-tunnel %s bead %s run %s: %v (reopening, not launching)\n",
				stage, beadID, runID.String(), cause)
			workers.EmitWorkerTunnelFailedEvent(ctx, runID.String(), string(beadID),
				rbc.worker.Name, rbc.worker.Host, sockPath, cause.Error(), emit.Emit)
			// A TID failure is not actionable: the reopen below is best-effort
			// either way and runs with the zero TID.
			reopenTID, _ := handles.TIDGen.Next()                                                               //nolint:errcheck // see comment above
			_ = handles.BrAdapter.ReopenBead(ctx, env.IntentLogDir, env.BrTimeoutCfg, runID, reopenTID, beadID, //nolint:errcheck // best-effort reopen; on failure the bead stays in_progress for manual reopen (hk-s20z)
				fmt.Sprintf("reverse-tunnel not ready: %v", cause))
		}

		// Allocate a free TCP port (hint for sshd's worker-side loopback bind)
		// and form the per-run worker TCP endpoint the hook relay will dial.
		// A failed alloc is fatal HERE rather than at the readiness gate below:
		// falling through spent an ssh round trip and started a real
		// `ssh -N -R 127.0.0.1:0:...` that cannot carry traffic, and the gate
		// then failed the run anyway. Refusing at the point of failure takes
		// nothing on a run that is already lost.
		tunnelPort, portErr := tunnelpkg.AllocatePort()
		if portErr != nil {
			refuseTunnel("port alloc", daemonHookSock, portErr)
			// succeeded is never assigned before this point, so the explicit
			// false is byte-equivalent to a naked return (nakedret).
			return false
		}
		rbc.workerHookSock = tunnelpkg.WorkerTCPEndpoint(tunnelPort)
		// hk-cnp17: free the reserved port when this run ends, so a later
		// run may reuse it (the reservation prevents two concurrent runs
		// from being handed the same worker-side hint port).
		runScope.Hold(runlease.TunnelPort, func() error {
			tunnelpkg.ReleasePort(tunnelPort)
			return nil
		})

		if mkErr := tunnelpkg.EnsureWorkerHarmonikDir(ctx, rbc.sshRunner, rbc.worker.RepoPath); mkErr != nil {
			fmt.Fprintf(os.Stderr,
				"daemon: workloop: tunnel.EnsureWorkerHarmonikDir bead %s run %s: %v (non-fatal; readiness gate is authority)\n",
				beadID, runID.String(), mkErr)
		}

		// This check also runs in the run plan, which refuses BEFORE the port and
		// the ssh round trip above. The plan can only cover a run whose worker was
		// pre-selected. A run that got its worker from the fallback selection was
		// not yet known to be remote when the plan ran, so this copy is that
		// path's guard. The two never both refuse: a pre-selected run that failed
		// the plan returned before this block.
		//
		// hk-ta6dg: `ssh -N -R <port>:<daemonHookSock>` never validates this local
		// forward destination at tunnel start — only when a connection actually
		// needs forwarding — so a too-long daemonHookSock would let the tunnel
		// come up and the readiness gate below (a TCP probe against the WORKER's
		// listener only) pass, then silently swallow every hook-relay connection
		// once traffic actually tries to flow. Fail loud here, before spawning the
		// doomed tunnel, exactly like a readiness-gate failure: no Launch, reopen
		// the bead.
		if lenErr := lifecycle.ValidateSocketPathLength(daemonHookSock); lenErr != nil {
			refuseTunnel("socket-path", daemonHookSock, lenErr)
			return
		}
		// Mirror the SSHRunner host/opts argv pattern (runner.go SSHRunner.Command):
		// extra opts BEFORE the host. Fall back to the worker record's Host when
		// the runner is not an SSHRunner (e.g. a test double).
		tunnelHost, tunnelOpts, hostOK := tunnelpkg.SSHHostOpts(rbc.sshRunner)
		if !hostOK {
			tunnelHost = rbc.worker.Host
		}
		tunnelArgs := tunnelpkg.BuildArgs(tunnelPort, daemonHookSock, tunnelHost, tunnelOpts)
		tunnelCmd := tunnelpkg.ReverseTunnelRunner(ctx, "ssh", tunnelArgs...)
		if startErr := tunnelCmd.Start(); startErr != nil {
			// Non-fatal: a failed tunnel start means the worker-side agent's hooks
			// will not reach box A, but the readiness gate (bead 3) is the
			// authority that fails the run. Nothing is held: a start that failed
			// left no process to kill.
			fmt.Fprintf(os.Stderr, "daemon: workloop: reverse-tunnel start bead %s run %s: %v\n",
				beadID, runID.String(), startErr)
		} else {
			// A successful Start leaves a live process, so the lease needs no test
			// for whether there is one. That is the whole reason the hold is in this
			// arm rather than below the branch.
			runScope.Hold(runlease.TunnelProcess, func() error {
				// Neither call's error is actionable and both are expected: Wait
				// reports the signal the Kill just sent. The lease is what makes the
				// pair run once, which the bare defer here relied on having a single
				// caller for.
				_ = tunnelCmd.Process.Kill() //nolint:errcheck // best-effort kill of a tunnel that is ending either way (pre-RT8 idiom)
				_ = tunnelCmd.Wait()         //nolint:errcheck // reaps the killed process; the error is the signal we sent
				return nil
			})
		}

		// gap #7 bead 3: tunnel readiness gate. The worker-side implementer
		// agent can fire its first agent_ready hook BEFORE the `ssh -N -R`
		// forward above is actually live. The hook relay does retry a refused
		// dial on a TCP endpoint, on a backoff inside a bounded window, so a
		// forward that comes up late is survivable — but a forward that never
		// comes up burns that whole window and gives up, which reads as a
		// silent bridge_daemon_startup_window_exceeded → agent_ready_timeout.
		// This gate is the authority on that case. Block until the
		// worker-side per-run TCP listener is confirmed CONNECTABLE (nc -z over
		// the SSHRunner, as the worker user) before any Launch — an
		// existence-only check would false-green a non-connectable endpoint
		// (hk-ege6). On timeout or failure, do NOT launch: refuse and return —
		// the run's scope closes on the way out and kills the tunnel it holds, so
		// the `ssh -N` process does not leak. The gate runs ONLY here,
		// inside the remote branch (NFR7: local runs never construct a tunnel
		// and never reach it).
		if waitErr := tunnelpkg.WaitWorkerSocketLive(ctx, rbc.sshRunner, rbc.workerHookSock, tunnelpkg.WorkerSocketReadyTimeout); waitErr != nil {
			refuseTunnel("readiness gate", rbc.workerHookSock, waitErr)
			return
		}
	}

	// notifyWorkerOffline emits a worker_offline event and disables the worker
	// in-memory. Active only for remote runs (rbc != nil). Phase is "spawn" for
	// code-sync failures and "liveness" for mid-run probe failures (B11).
	notifyWorkerOffline := func(phase, detail string) {
		if rbc == nil {
			return
		}
		workers.EmitWorkerOfflineEvent(ctx, rbc.worker.Name, rbc.worker.Host, phase, detail, emit.Emit)
		if handles.Workers != nil {
			handles.Workers.SetEnabled(false)
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
		workerHost, sshOpts, _ := tunnelpkg.SSHHostOpts(rbc.sshRunner)
		if err := codesyncpkg.FetchRunBranchBoxA(ctx, nil, env.ProjectDir, runID.String(), workerHost, rbc.worker.RepoPath, sshOpts); err != nil {
			// B11: SSH connection failure → emit worker_offline + disable worker.
			if tmuxpkg.IsSSHConnectionFailure(err) {
				notifyWorkerOffline("spawn", fmt.Sprintf("codesync.FetchRunBranchBoxA: %v", err))
			}
			return fmt.Sprintf("fetch run branch from worker on box A: %v", err)
		}
		return ""
	}
	// ── end DD1 code-sync setup ──────────────────────────────────────────────

	wtFactory := handles.WorktreeFactory
	if wtFactory == nil {
		if rbc != nil {
			// Remote run: create the worktree on the worker via SSHRunner (B7+B8).
			sshRunner := rbc.sshRunner
			workerRepoPath := rbc.worker.RepoPath
			wtFactory = func(ctx context.Context, _, runID, headSHA string) (string, func(), error) {
				// hk-5qp7z: thread worktreeCreateMu into the config so CreateWorktree
				// serialises the git-worktree-add + HEAD-resolve loop across all
				// concurrent remote dispatch goroutines (prevents empty-HEAD race).
				cfg := workspace.NoWorktreeRootOverride().WithRunner(sshRunner).WithCreateMutex(handles.WorktreeCreateMu)
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
	// RSM-010 (RT7): thread the assembled factory onto RunPorts as WorktreePort.
	// The create call site below reaches it via rp.Worktree — byte-identical to
	// calling wtFactory directly (ports-design §6).
	rp.Worktree = worktreePort(wtFactory)
	// Serialize codesync.EnsureBaseOnWorker (step a, DD1 code-sync) + 'git worktree add'
	// inside the merge exclusion domain (mergeq, RSM-018) so concurrent
	// beadRunOne goroutines do not run concurrent git operations on the same
	// remote worker (hk-lt091) or race on projectDir/.git/index.lock (hk-h8u7p),
	// and so this box-A .git + worker git work excludes concurrent merge commits.
	//
	// hk-lt091: before hk-zexsj added -o ControlMaster=no, all SSH commands to a
	// worker shared one TCP connection, so the remote OS serialised them naturally.
	// With ControlMaster=no each SSH command is an independent TCP connection; a
	// sibling bead's codesync fetch-base (git-fetch) can therefore race git-worktree-add
	// at the remote-OS level, leaving the worktree dir created but HEAD uninitialised
	// — the empty-HEAD race that hk-iaj1w retries cannot fix because the race persists
	// across all retry attempts. Running codesync fetch-base + worktree-add as ONE
	// critical section in the domain eliminates the race at its source.
	var baseSyncErr error
	var wtPath string
	var wtCleanup func()
	var wtErr error
	if subErr := mport.Submit()(ctx, "base-sync-create", func(qctx context.Context) error {
		// Step (a): for remote runs, ensure baseSHA is on the worker before the
		// worktree is created there (DD1 code-sync, remote-substrate B8).
		if rbc != nil {
			// hk-2hfyt: use codesync.EnsureBaseOnWorker (not a bare fetch-base) so
			// an unpushed base commit triggers a direct push from box A to the worker
			// rather than leaving an empty-HEAD worktree.
			workerHostEBOW, sshOptsEBOW, _ := tunnelpkg.SSHHostOpts(rbc.sshRunner)
			baseSyncErr = codesyncpkg.EnsureBaseOnWorker(qctx, rbc.sshRunner, rbc.worker.RepoPath, headSHA,
				nil, env.ProjectDir, workerHostEBOW, sshOptsEBOW)
		}
		// baseSyncErr (a business outcome, handled after the critical section) does
		// not fail the critical section itself; only skip the worktree-add on it.
		if baseSyncErr == nil {
			wtPath, wtCleanup, wtErr = rp.Worktree.Create(qctx, activeRepo, runID.String(), headSHA)
		}
		return nil
	}); subErr != nil {
		// The critical section never entered the domain (ctx cancelled before
		// execution, or the queue owner stopped) — surface as a worktree-create
		// failure so the run reopens rather than proceeding with an empty wtPath.
		wtErr = subErr
	}
	if baseSyncErr != nil {
		fmt.Fprintf(os.Stderr, "daemon: workloop: codesync.EnsureBaseOnWorker bead %s run %s: %v (reopening)\n",
			beadID, runID.String(), baseSyncErr)
		// B11: SSH connection failure → emit worker_offline + disable worker.
		if tmuxpkg.IsSSHConnectionFailure(baseSyncErr) {
			notifyWorkerOffline("spawn", fmt.Sprintf("codesync.EnsureBaseOnWorker: %v", baseSyncErr))
		}
		reopenTID, tidErr := handles.TIDGen.Next()
		if tidErr != nil {
			fmt.Fprintf(os.Stderr, "daemon: workloop: tidGen.Next (codesync.EnsureBaseOnWorker reopen) bead %s: %v\n", beadID, tidErr)
		}
		if reopenErr := handles.BrAdapter.ReopenBead(ctx, env.IntentLogDir, env.BrTimeoutCfg, runID, reopenTID, beadID,
			fmt.Sprintf("ensure base on worker failed: %v", baseSyncErr)); reopenErr != nil {
			fmt.Fprintf(os.Stderr, "daemon: workloop: ReopenBead (codesync.EnsureBaseOnWorker) bead %s run %s: %v\n",
				beadID, runID.String(), reopenErr)
		}
		return succeeded
	}
	if wtErr != nil {
		fmt.Fprintf(os.Stderr, "daemon: workloop: CreateWorktree for bead %s run %s: %v (reopening)\n", beadID, runID.String(), wtErr)
		// RT7 / RSM-032: the worktree-create failure rides the machine's reopen
		// spine (EvProvisionFailed) carrying BOTH strings — the distinct terminal
		// summary keeps the failure visible in events.jsonl (hk-3vbc).
		failRun(fmt.Sprintf("create worktree failed: %v", wtErr),
			fmt.Sprintf("worktree_create_failed: %v", wtErr))
		return
	}
	// useIndepSession is set true when this run is launched in an independent tmux
	// session (runSessionSpawner path, hk-o85ye). Deferred cleanup (wtCleanup,
	// runlaunch.ForceTeardownSession) is skipped on daemon shutdown so the session and its
	// worktree survive SIGKILL; on normal exit cleanup runs as usual.
	useIndepSession := false

	// hk-j6wm7: retain the run worktree (which contains the pi-agent dir + the
	// captured stdout/stderr, see below) on FAILURE for Pi runs so the fast-fail
	// error — e.g. the ~4.5s exit0-no-commit against a locally-hosted
	// OpenAI-compatible endpoint (ornith) — is observable post-mortem. runIsPi is
	// set true once the harness resolves to Pi (below); the run outcome is the
	// Run machine's terminal state (bridge.Success(), RSM-022). This mirrors the hk-o85ye survive-cleanup
	// gate: skip the deferred wtCleanup on an abnormal outcome so the artifacts
	// survive, instead of deleting the only evidence of why the run failed.
	// Successful Pi runs and ALL non-Pi runs clean up exactly as before, so there
	// is no disk-leak regression on the happy path.
	runIsPi := false

	// Both post-launch facts now exist, so the scope's close can read the whole
	// exit rather than the shutdown fact alone. Decide (RSM-037) is where the
	// polarity lives: survival needs an independent session AND a stopping
	// daemon, and it wins over retained evidence when both apply.
	runExit = func() runlease.Exit {
		return runlease.Exit{
			SessionRunsIndependently: useIndepSession,
			DaemonStopping:           ctx.Err() != nil,
			EvidenceWorthKeeping:     runIsPi && !bridge.Success(),
		}
	}

	if wtCleanup != nil {
		// The worktree is the resource two of the three dispositions keep, for two
		// different reasons: a surviving run still has an agent working inside it,
		// and a run whose captured output is the only record of why it failed
		// (hk-j6wm7) has nothing else to show an operator. The run no longer asks
		// which reason applies. It asks once, at the close.
		runScope.Hold(runlease.Worktree, func() error {
			wtCleanup()
			return nil
		})
		// The give-back is the scope's. This notice is not, because this is the
		// only place that knows WHERE the worktree is, and an operator who has to
		// read a failed run's captured output needs the path. It reads the same
		// one answer the close reads.
		defer func() {
			d := runlease.Decide(runExit())
			if d.Releases(runlease.Worktree) {
				return
			}
			// Only a run kept FOR its evidence has captured output to point at. A
			// surviving run is kept because an agent is still working in there.
			capture := ""
			if d == runlease.RetainEvidence {
				capture = fmt.Sprintf(" — the captured output is under %s/.harmonik/pi-agent/", wtPath)
			}
			fmt.Fprintf(os.Stderr,
				"daemon: workloop: run %s (bead %s) ends %s — its worktree is kept at %s%s\n",
				runID.String(), beadID, d, wtPath, capture)
		}()
	}

	// hk-ooexj: snapshot the active repo's pre-existing untracked files at run-start
	// (before the implementer launches) so the post-run escape check can exclude
	// files that already existed and which the implementer never touched. A failed
	// snapshot leaves preRunUntracked nil — the escape check then degrades to its
	// prior, baseline-free behaviour rather than silently suppressing escapes.
	// Use activeRepo: cross-repo runs create the worktree in the target repo.
	preRunUntracked, snapErr := runmerge.SnapshotUntrackedFiles(ctx, activeRepo)
	if snapErr != nil {
		fmt.Fprintf(os.Stderr, "daemon: workloop: runmerge.SnapshotUntrackedFiles for bead %s run %s: %v (escape check will run without baseline)\n", beadID, runID.String(), snapErr)
	}

	// Emit run_started with optional queue_id + queue_group_index per QM-011/QM-012.
	// FR13: include worker_name and worker_os for remote runs; empty for local.
	var runStartedWorkerName, runStartedWorkerOS string
	if rbc != nil {
		runStartedWorkerName = rbc.worker.Name
		runStartedWorkerOS = rbc.worker.OS
	}
	emitRunStarted(ctx, emit, runID, beadID, wtPath, queueID, queueGroupIndex, runStartedWorkerName, runStartedWorkerOS, string(workflowMode))

	// Do NOT add a pre-dispatch "already landed on main?" check here. One used to
	// sit at this point and closed the bead when shared.MainHistoryHasRefsTrailer
	// — a bare "Refs: <id>" git-log grep — matched. On a bead worked in several
	// parts an older partial commit carries the same ID, so the grep matched and
	// the daemon closed a bead whose remaining work had not run. The crash-restart
	// case the check was meant to cover is handled at runtime instead, by the
	// noChange timeout and by noCommitGuardShouldReopen, and both of those check
	// whether the work is present before closing. Full record: hk-f38n, and the
	// informative note under BI-022 in specs/beads-integration.md §4.7.

	// Pre-switch: for DOT mode, resolve and pre-load the graph source (hk-30vlb).
	// Three-tier resolution:
	//   1. plan.WorkflowRef set → explicit path (resolved below in the DOT case).
	//   2. <projectDir>/workflow.dot exists → project-level path (resolved below).
	//   3. Neither → use the embedded standard-bead.dot (loaded here).
	//
	// The embedded load happens here — before the switch — so a failure can fail
	// the run before anything is dispatched.
	//
	// Review floor (EM-012a-FLOOR, amended): the floor's guarantee is that a bead
	// resolved below tier 1 is NEVER dispatched without a review gate. That is
	// delivered by the embedded graph itself — standard-bead.dot carries a
	// reviewer node on the sole inbound edge to close — plus this branch, which
	// FAILS THE RUN rather than dispatching under some other shape.
	//
	// This used to demote to review-loop. It no longer does, for two reasons the
	// amendment records: review-loop is retired (it was a hand-written particular
	// of the general graph walker this very branch is loading), and a demotion was
	// dishonest — run_started stamps workflow_mode ABOVE this line, so a demoted
	// run's own start event named a mode it did not execute.
	var preloadedDotGraph *dot.Graph
	if workflowMode == core.WorkflowModeDot && plan.WorkflowRef == "" {
		defaultDotPath := filepath.Join(env.ProjectDir, "workflow.dot")
		if _, statErr := os.Stat(defaultDotPath); os.IsNotExist(statErr) {
			g, embErr := loadStandardGraph(itemTemplateParams)
			if embErr != nil {
				fmt.Fprintf(os.Stderr,
					"daemon: workloop: embedded standard-bead.dot failed to load for bead %s run %s: %v — failing the run; "+
						"the daemon will NOT dispatch this bead under a different workflow shape (EM-012a-FLOOR)\n",
					beadID, runID.String(), embErr)
				// Same reopen spine as the tier-1/tier-2 load failure below.
				reason := fmt.Sprintf("workflow_load: embedded standard-bead.dot: %v", embErr)
				failRun(reason, reason)
				return bridge.Success()
			}
			preloadedDotGraph = g
		}
	}

	// The routed launch builder (T12 hk-xhawy) is resolved ONCE by the caller in
	// buildRunBundles and threaded here on rp.Launch / rp.LaunchBuilder, so ALL
	// workflow modes (review-loop, DOT cascade, single) share the same
	// harness-resolved builder. RT18.11 relocated that resolution to the caller —
	// where deps is live — which is what let this function drop the deps param; the
	// old by-value deps.launchSpecBuilder smuggle is gone.

	// Mode-dispatch: route to the mode-specific driver.
	//
	// dot mode: DOT-defined workflow graph; loader validates the artifact,
	// then drives the cascade (driveDotWorkflow). Default uses the embedded
	// standard-bead.dot (pre-loaded above into preloadedDotGraph).
	//
	// single mode: one-shot implementer dispatch. Reachable ONLY via an explicit
	// per-bead workflow:single label, audited via review_bypassed (EM-012a).
	//
	// review-loop mode was RETIRED (EM-015d): it was a hand-written particular of
	// the graph the dot walker executes generally, and the dot path is the
	// production default.
	switch workflowMode {
	case core.WorkflowModeDot:
		// DOT workflow mode: load + validate the .dot artifact, then hand the
		// validated graph to the cascade driver (driveDotWorkflow, dot_cascade.go)
		// which walks the graph node-by-node using workflow.DecideNextNode
		// (hk-9dnak; cascade engine library hk-bf85t).
		//
		// Graph source resolution uses preloadedDotGraph (Tier 3: embedded) when
		// already set by the pre-switch block; otherwise resolves Tier 1/2 from
		// an explicit ref or <projectDir>/workflow.dot (three-tier spec hk-30vlb).
		// Embedded-load failure was already handled above: the run was failed and
		// the bead reopened with a workflow_load reason, so this case is not reached.
		var graph *dot.Graph
		if preloadedDotGraph != nil {
			// Tier 3: embedded standard-bead.dot (already parsed and validated).
			graph = preloadedDotGraph
		} else {
			// Tier 1 or 2: explicit ref or <projectDir>/workflow.dot.
			// WG-046 ordering: read → substitute(itemTemplateParams) → parse → validate → dispatch.
			dotPath := filepath.Join(env.ProjectDir, "workflow.dot")
			if plan.WorkflowRef != "" {
				if filepath.IsAbs(plan.WorkflowRef) {
					dotPath = plan.WorkflowRef
				} else {
					dotPath = filepath.Join(env.ProjectDir, plan.WorkflowRef)
				}
			}
			var loadErr error
			graph, loadErr = workflow.LoadDotWorkflowWithParams(dotPath, itemTemplateParams)
			if loadErr != nil {
				fmt.Fprintf(os.Stderr, "daemon: workloop: DOT workflow load failed for bead %s run %s: %v (reopening)\n",
					beadID, runID.String(), loadErr)
				// RT9: the load failure rides the machine's reopen spine (reopen +
				// run_failed with the same workflow_load reason, RSM-009/032).
				reason := fmt.Sprintf("workflow_load: %v", loadErr)
				failRun(reason, reason)
				return bridge.Success()
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
			dotWorkerBinary = tunnelpkg.WorkerHarmonikPath(rbc.worker)
			dotWorkerHookSock = rbc.workerHookSock
			dotWorkerCwd = rbc.worker.RepoPath
			if ts, ok := handles.Substrate.(*tmuxSubstrate); ok {
				dotWorkerSession = ts.workerSpawnSessionName(rbc.worker.Name)
			}
		} else if handles.Runner != nil {
			dotRunner = handles.Runner // hk-hd2w6: Config.Runner injection (test seam)
		}

		// Drive the cascade: walk start → … → terminal, dispatching each node by
		// type (non-agentic synthesize-success, agentic substrate-dispatch,
		// gate/sub-workflow out-of-scope error).
		dotResult := driveDotWorkflow(ctx, env, rp, handles, runID, beadID, beadRecord, beadRecord.Title, beadRecord.Description,
			wtPath, headSHA, graph, resolvedModel, resolvedEffort, dotExtraContext, baseBranch, dotRunner,
			dotWorkerBinary, dotWorkerHookSock, dotWorkerSession, dotWorkerCwd)

		// ── RT9: the DOT terminal rides the Run tail (RSM-020) ────────────────
		//
		// No post-mode scenario gate on this path: the DOT cascade engine
		// (dispatchDotToolNode) runs its gate inside the graph (standard-bead.dot
		// commit_gate tool node) — skipGate records the pass. The hk-whru3 /
		// hk-vbv3b already-approved-on-main carve-out rides as the carveOut
		// classifier onto the machine's AlreadyApprovedOnMain row; the hk-tnui
		// trailer stamp is the amendTrailers policy (single attempt — DOT has no
		// merge-retry loop).
		transitionTID, _ := handles.TIDGen.Next()
		bridge.Start(ctx, workflowMode)
		bridge.WireSpine(runloop.SpineArgs{
			RunRunner:       dotRunner,
			WTPath:          wtPath,
			HeadSHA:         headSHA,
			PreMergeSync:    preMergeSync,
			MPort:           mport,
			ActiveRepo:      activeRepo,
			ProtectBranches: effectiveMergeProtectBranches,
			TransitionTID:   transitionTID,
			EmitBeadClosed: func(c context.Context) {
				emitBeadClosedAndMaybeEpic(c, rp, handles, runID, beadID)
			},
			MergeTarget: mergeTarget, // hk-lgykq: per-bead integration-branch landing target (resolved baseBranch w/ fallback)
			SkipGate:    true,
			// hk-f9xzs: classify transient merge failures (rebase_conflict,
			// non_ff_merge, merge_fmt_failed) as retryable so the spine spends the
			// 3-attempt budget runBridgeConfig now grants DOT. Inherited from the
			// retired review-loop path, which was the only mode that carried it;
			// the rationale is on runBridgeConfig.
			Retryable: runmerge.IsRetryableReason,
			// hk-tnui: stamp Reviewed-By / Review-Verdict trailers on the HEAD
			// commit before the FF merge. LOCAL runs only (rbc == nil): remote runs
			// keep the trailer injection deferred (FLAGGED).
			//
			// Re-amends before EACH retry rather than only the first (RF :3899):
			// now that merges retry, the prior inner rebase may have rewritten HEAD,
			// so a once-only amend would leave the retried merge carrying no
			// trailers. The amend is idempotent.
			AmendTrailers: func(c context.Context, retry int) {
				if dotResult.approveVerdict == nil || rbc != nil {
					return
				}
				if amendErr := runmerge.AppendReviewTrailersToHEAD(c, wtPath, dotResult.approveVerdict); amendErr != nil {
					fmt.Fprintf(os.Stderr, "daemon: workloop: runmerge.AppendReviewTrailersToHEAD (dot, merge retry %d) bead %s: %v (non-fatal)\n",
						retry, beadID, amendErr)
				}
			},
			// hk-whru3: advisory-RC + rebase_dropped_commits → work already on
			// main; a prior run merged the same patch and the rebase dropped the
			// commit. hk-vbv3b: extended to the genuine APPROVE terminal path
			// (terminalNodeID == "close") and the hk-8ps7q approved-and-done path
			// (approveVerdict != nil). Falls through to CloseBead so the infinite
			// re-dispatch loop terminates instead of re-queuing.
			CarveOut: func(reason string) bool {
				alreadyApprovedOnMain := dotResult.advisoryRC ||
					dotResult.terminalNodeID == "close" ||
					dotResult.approveVerdict != nil
				return alreadyApprovedOnMain && strings.Contains(reason, "rebase_dropped_commits")
			},
		})
		switch {
		case dotResult.success:
			bridge.Feed(ctx, runexec.Event{
				Kind: runexec.EvModeOutcome, ModeOutcome: runexec.ModeSuccess,
				PathLabel: "dot", Detail: dotResult.summary,
			})
		case dotResult.subsumed:
			// noChange-subsumed: implementer exited without advancing HEAD because
			// the work already landed in main via a prior run. Approved close, no
			// merge — no new commits (hk-9v5yo); RSM-035 event-carried strings.
			bridge.Feed(ctx, runexec.Event{
				Kind: runexec.EvModeOutcome, ModeOutcome: runexec.ModeSubsumed,
				EmitOutcome: true, PathLabel: "dot noChange-subsumed",
				Detail: "noChange-subsumed: bead found in main",
			})
		default:
			// Non-success terminal (BLOCK / cap-hit / no-progress / structural
			// failure / gate-out-of-scope / context_cancelled) → the reopen spine.
			//
			// Orphan-salvage (hk-8b35c): if the implementer advanced HEAD past the
			// parent (a commit landed on the run branch before the gate bounced
			// it), record the tip SHA in the run_failed payload so the operator
			// can find and manually cherry-pick / merge the stranded work.
			// REMOTE: resolve via dotRunner (nil ⇒ box-A-local, NFR7).
			//
			// hk-e3fy: use context.Background() for the HEAD resolve so a
			// context_cancelled DOT failure (daemon shutdown or per-run abort) can
			// still capture the tip SHA — ctx is already cancelled on that class.
			// The reopen itself rides the machine's spine; the reopen hook applies
			// the same cancellation-free fallback (RSM-022).
			tipResolveCtx := ctx
			if ctx.Err() != nil {
				tipResolveCtx = context.Background()
			}
			if tipSHA, tipErr := gitprobe.ResolveWorktreeHEADVia(tipResolveCtx, dotRunner, wtPath); tipErr == nil && tipSHA != "" && tipSHA != headSHA {
				runTipSHA = &tipSHA
			}

			// Retry-spend budget (hk-c1ah6). Inherited from the retired review-loop
			// path, which was its only caller: without it a bead that cannot pass
			// review is reopened and re-dispatched forever, paying for a fresh set
			// of sessions each time. The ladder charges the per-item failure counter
			// and, once the budget is spent, closes the bead flagged
			// needs-attention instead of reopening it — so a human sees it and the
			// spend stops.
			//
			// Charged only when the cascade asked for attention: a transient or
			// structural failure that is not the bead's fault should not consume a
			// budget meant for "this work keeps failing review".
			budgetExhausted := false
			if dotResult.needsAttention {
				budgetExhausted = handles.Budget.ChargeReviewLoopFailure(
					ctx, queueName, queueID, queueGroupIndex, queueItemIndex, beadID)
			}
			if budgetExhausted {
				exhaustedSummary := fmt.Sprintf("run_budget_exhausted (max=%d failures): %s",
					queue.MaxReviewLoopFailures, dotResult.summary)
				fmt.Fprintf(os.Stderr, "daemon: workloop: bead %s run %s retry budget exhausted — closing with needs-attention (hk-c1ah6)\n",
					beadID, runID.String())
				bridge.SetRejectReason(exhaustedSummary)
				bridge.Feed(ctx, runexec.Event{
					Kind: runexec.EvModeOutcome, ModeOutcome: runexec.ModeBudget,
					NeedsAttention: true, Detail: exhaustedSummary,
				})
				break
			}

			bridge.Feed(ctx, runexec.Event{
				Kind: runexec.EvModeOutcome, ModeOutcome: runexec.ModeFailure,
				Reason: dotResult.summary, Detail: dotResult.summary,
			})
		}
		return bridge.Success()

	default:
		// WorkflowModeSingle or any normalised-to-single value: fall through
		// to the single-mode dispatch path below.
	}

	// ─── Single-mode dispatch (production path) ───────────────────────────────

	// Step 1: build the Claude launch spec via buildClaudeLaunchSpec.
	daemonSock := filepath.Join(env.ProjectDir, ".harmonik", "daemon.sock")
	// gap #7 bead 2: a REMOTE worker cannot reach box A's local daemon.sock. For
	// remote runs, the implementer agent must dial the worker-side reverse-tunnel
	// TCP endpoint (rbc.workerHookSock, tcp://127.0.0.1:<port>) instead, which the
	// `ssh -N -R` tunnel launched above forwards back to box A's daemon.sock (it is
	// a TCP loopback listener, not a unix socket, so the unprivileged hook user can
	// connect — hk-ege6). tunnel.ResolveAgentDaemonSocket returns
	// rbc.workerHookSock for a remote run and the unchanged box-A daemonSock for a
	// local run (rbc == nil), so local runs remain byte-identical (NFR7). The
	// resolved path flows into rc.daemonSocket → ClaudeEnvVars(HARMONIK_DAEMON_SOCKET).
	var rbcHookSock string
	if rbc != nil {
		rbcHookSock = rbc.workerHookSock
	}
	agentDaemonSock := tunnelpkg.ResolveAgentDaemonSocket(rbcHookSock, daemonSock)
	rc := shared.LaunchCtx{
		RunID:             runID,
		BeadID:            string(beadID),
		WorkspacePath:     wtPath,
		DaemonSocket:      agentDaemonSock,
		WorkflowMode:      workflowMode,
		Phase:             "", // empty = single-mode
		IterationCount:    1,
		PriorClaudeSessID: nil,
		HandlerBinary:     env.HandlerBinary,
		DaemonBinaryPath:  env.DaemonBinaryPath,
		BaseEnv:           env.HandlerEnv,
		BeadTitle:         beadRecord.Title,
		BeadDescription:   beadRecord.Description,
		Model:             resolvedModel,
		Effort:            resolvedEffort,
		Provider:          resolvedProfile.Provider,
		APIKeyEnv:         resolvedProfile.APIKeyEnv,
		APIKeyFile:        resolvedProfile.APIKeyFile,
		BaseURL:           resolvedProfile.BaseURL,
		API:               resolvedProfile.API,
		// worktreeRootPath is used by buildClaudeLaunchSpec to check whether the
		// workspace is a harmonik-managed worktree for --dangerously-skip-permissions
		// per HC-055b. Derived from activeRepo (= target repo for cross-repo runs).
		WorktreeRootPath: workspace.WorktreeRootPath(activeRepo, workspace.NoWorktreeRootOverride()),
		ExtraContext:     extraContext, // hk-boiwe: per-item context from queue.Item.Context
		BaseBranch:       baseBranch,   // hk-mtm0w: pre-exit rebase target
	}
	// hk-z8ek: for a REMOTE run, thread the worker's SSHRunner into the launch
	// spec so the three materialization writes (.claude/settings.json,
	// .harmonik/agent-task.md, ~/.claude.json trust) land on the WORKER's
	// filesystem — where the worktree actually lives — instead of box A's mirror
	// path. Resolve the hook "command" to the worker's harmonik path too (the
	// hook subprocess runs ON THE WORKER). Nil runner + empty workerBinaryPath
	// for a LOCAL run keeps the materialization byte-identical (NFR7).
	if rbc != nil {
		rc.Runner = rbc.sshRunner
		rc.WorkerBinaryPath = tunnelpkg.WorkerHarmonikPath(rbc.worker)
	}
	// RSM-010 (RT7): build the launch spec through LaunchPort (assembled above,
	// before the mode switch, over the pre-built routed builder). Byte-identical to
	// the pre-port `deps.launchSpecBuilder(ctx, rc)` (T12, hk-xhawy).
	spec, artifacts, specErr := rp.Launch.BuildSpec(ctx, rc)
	if specErr != nil {
		fmt.Fprintf(os.Stderr, "daemon: workloop: buildClaudeLaunchSpec bead %s run %s: %v (reopening)\n",
			beadID, runID.String(), specErr)
		reason := fmt.Sprintf("build launch spec error: %v", specErr)
		failRun(reason, reason)
		return
	}
	// PI-073: record the resolved agent type on the RunHandle so that
	// bandwidthTunerBackstop can filter Pi rate-limit events from the global
	// tuner. The type is only known after specBuilder resolves the harness.
	if rh, ok := handles.RunRegistry.Get(runID); ok && rh != nil {
		rh.SetAgentType(shared.ArtifactAgentType(artifacts))
	}
	// hk-j6wm7: record whether this run is a Pi run so the deferred wtCleanup can
	// retain the worktree (and the captured pi output under it) on failure.
	if shared.ArtifactAgentType(artifacts) == core.AgentTypePi {
		runIsPi = true
	}

	// In production HandlerArgs is always nil and spec.Args already contains the
	// bridge flags (--session-id or --resume) from buildClaudeLaunchSpec.
	// For test fixtures that supply HandlerArgs (e.g. ["-c", "exit 0"]), prepend
	// them so that the bridge flags become extra positional args the fixture can
	// safely ignore (e.g. /bin/sh -c "exit 0" sh --session-id <uuid>).
	if len(env.HandlerArgs) > 0 {
		spec.Args = append(env.HandlerArgs, spec.Args...)
	}

	// B10: for remote runs the SSHRunner tunnels liveness probes (pgrep, ps) and
	// commit-detection to the worker host instead of executing them locally.
	var runRunner tmuxpkg.CommandRunner
	if rbc != nil {
		runRunner = rbc.sshRunner
	}

	// noChangeTimeoutCh is declared here so the default switch branch at the
	// post-wait select can read it (nil = no watchdog, treated as an open
	// channel). The deliver hook below is what assigns it (hk-trjef).
	var noChangeTimeoutCh chan struct{}

	// hk-5z1f0: cold-start spawn semaphore, ONE for the whole daemon. Acquire immediately before
	// the remote agent Launch so no more than cap (3) claude cold-starts run
	// concurrently on a single worker — the 2nd (reviewer) cold-start over the
	// reverse tunnel otherwise trips agent_ready_timeout under 6-concurrent remote
	// load. Remote-only (rbc != nil): local runs never construct a tunnel and are
	// never gated. Given back once agent_ready resolves (success/failure/timeout),
	// through the lease below.
	//
	// A local run gets a lease that is born spent: it names the resource, it can
	// never fire, and the give-back site needs no test for whether there is
	// anything to give back (RSM-036).
	spawnSlot := runlease.Hold(runlease.ColdStartToken, nil)
	if rbc != nil && handles.AgentSpawnSem != nil {
		select {
		case handles.AgentSpawnSem <- struct{}{}:
		case <-ctx.Done():
			// ctx cancelled while waiting for a slot — reopen and bail before Launch.
			reason := fmt.Sprintf("cancelled awaiting cold-start spawn slot: %v", ctx.Err())
			failRun(reason, reason)
			return
		}
		// The scope is the backstop the deferred give-back used to be: the token
		// comes back on the readiness edge below, and on a path that never reaches
		// that edge the close returns it. The lease runs the give-back at most
		// once across both, which is what the sync.Once here used to do.
		spawnSlot = runScope.Hold(runlease.ColdStartToken, func() error {
			<-handles.AgentSpawnSem
			return nil
		})
	}

	// RT7: provisioning is complete — start the Run machine so every
	// dispatch-phase failure below rides EvModeOutcome{failure} (RSM-031) and
	// the terminal spine rides the machine.
	bridge.Start(ctx, workflowMode)

	// The launch itself is the ONE path in agentlaunch.go. This site keeps only
	// what to launch (above) and what the exit means (below).
	launch := runAgentLaunch(ctx, agentLaunchInput{
		Env:     env,
		Ports:   rp,
		Handles: handles,
		RunID:   runID,
		LogPrefix: fmt.Sprintf("daemon: workloop: bead %s run %s",
			beadID, runID.String()),
		Spec:          spec,
		Artifacts:     artifacts,
		WorktreePath:  wtPath,
		DaemonSocket:  agentDaemonSock,
		Runner:        runRunner,
		Remote:        rbc != nil,
		BaseSubstrate: handles.Substrate,
		ConfigurePerRunSubstrate: func(prs *perRunSubstrate) {
			if rbc != nil {
				// B11: wire the offline callback so a mid-run SSH failure emits
				// worker_offline and disables the worker.
				prs.onConnectionFailure = func(c context.Context, detail string) {
					notifyWorkerOffline("liveness", detail)
				}
				// remote-substrate worker-spawn gap: name the tmux session to ENSURE
				// + spawn into ON THE WORKER and the cwd to create it with (the
				// worker's repo_path). Without this the spawn targets box A's local
				// "-default" session — which does not exist on the worker — and the
				// launch wedges at launch_initiated.
				prs.workerSessionName = prs.inner.workerSpawnSessionName(rbc.worker.Name)
				prs.workerSessionCwd = rbc.worker.RepoPath
				return
			}
			// hk-o85ye (Move 3): route a LOCAL run through an independent tmux
			// session so it survives a daemon SIGKILL. Only where the substrate
			// implements runSessionSpawner AND the underlying tmux adapter supports
			// independent session creation (sessionCreator). Adapters that lack it
			// (test stubs, the $TMUX-reuse mode) fall through to the shared-session
			// path — no behavior change for them.
			if env.ProjectDir == "" {
				return
			}
			canIndepSession := false
			if ts, tsOK := handles.Substrate.(*tmuxSubstrate); tsOK {
				_, canIndepSession = ts.adapter.(sessionCreator)
			}
			if _, ok := handles.Substrate.(runSessionSpawner); !ok || !canIndepSession {
				return
			}
			prs.runSessionID = runID.String()
			useIndepSession = true
			// Pre-compute the session name for the registry (best-effort; empty is fine).
			sessName := ""
			if ts, tsOK := handles.Substrate.(*tmuxSubstrate); tsOK {
				if sn, snErr := ts.runSessionName(runID.String()); snErr == nil {
					sessName = sn
				}
			}
			queueIDStr := ""
			if queueID != nil {
				queueIDStr = *queueID
			}
			queueGroupIdx := -1
			if queueGroupIndex != nil {
				queueGroupIdx = *queueGroupIndex
			}
			if writeErr := runpkg.Write(env.ProjectDir, runpkg.Record{
				SchemaVersion: 1,
				RunID:         runID.String(),
				BeadID:        string(beadID),
				QueueName:     queueName,
				QueueID:       queueIDStr,
				GroupIndex:    queueGroupIdx,
				ItemIndex:     queueItemIndex,
				SessionName:   sessName,
				StartedAt:     rp.Clock.Now(),
			}); writeErr != nil {
				// Registry write failed: fall back to the shared-session path (no
				// survive-restart).
				fmt.Fprintf(os.Stderr, "daemon: workloop: run registry write failed for %s: %v (using shared session)\n", runID.String(), writeErr)
				prs.runSessionID = ""
				useIndepSession = false
				return
			}
			// The record exists, so the run holds it. A surviving run leaves it
			// standing — it is how the next boot finds this agent's session by
			// name — and every other ending gives it back. The hold is HERE rather
			// than at the top of the run because a record that was never written
			// is not a resource, and the write is the only place that knows.
			runScope.Hold(runlease.RunRecord, func() error {
				// An absent record is the already-cleaned case, not a failure. Any
				// other failure is retried by the next boot's adoption sweep, so it
				// is reported and not acted on — the same handling
				// adoptDeadRunSessions gives this call.
				if remErr := runpkg.Remove(env.ProjectDir, runID.String()); remErr != nil &&
					!errors.Is(remErr, runpkg.ErrNotFound) {
					return remErr
				}
				return nil
			})
		},
		// hk-wnqos: the single-mode implementer is the terminal/merge spawn — it
		// draws from the reserved +1 slot in spawnSem so a saturated non-terminal
		// pool cannot starve it at launch.
		Terminal: true,
		// Single-mode beadRunOne is always a fresh launch: it has no iteration
		// counter and never issues `claude --resume`.
		IsResume:        false,
		ProbeResume:     false,
		HeartbeatViaTap: true,
		Deliver: func(dctx context.Context, dc agentDeliverCtx) {
			// hk-zlo8: a ProcessExit harness (codex) has no tmux pane and receives
			// its task via argv; pasteInjectOnLaunch would fail with "WriteLastPane:
			// cant find pane" → no_commit in ~4s.
			if dc.ProcessExit {
				return
			}
			// pasteInjectOnLaunch delivers "Please read .harmonik/agent-task.md and
			// begin." to the pane. It MUST run on the post-ready deliver edge (smoke
			// v9 RED, hk-zchbu): pasted before agent_ready, the trailing \n is
			// consumed by Claude Code's welcome-splash render, the text sits in the
			// input bar unsubmitted, and the run hangs. Errors are non-fatal (PL-021d).
			briefDelivered := pasteInjectOnLaunch(dctx, rp.Clock, dc.PasteTarget, artifacts.ClaudeSessionID,
				rc.Phase, rc.IterationCount, wtPath, emit, runID)

			// pasteInjectQuitOnCommit: in interactive TUI mode the Stop hook fires on
			// session exit, not after each response, and a Claude Code agent cannot
			// run a slash command from its tool API — so once the task commit lands
			// the daemon injects `/quit` via tmux send-keys to trigger the hook and
			// unblock the workloop (CHB-028, hk-cmybm). briefDelivered gates the
			// commit poll so a stale pane cannot be /exit-raced (hk-930o3).
			//
			// noChangeTimeoutCh is closed by the watchdog when it kills the session
			// after commitPollTimeout without a new commit; the post-wait switch reads
			// it non-blockingly to tell a forced kill from a genuine agent failure
			// (hk-trjef).
			//
			// hk-37giq: the watchdog MUST take its own tap subscription. Sharing the
			// ready pump's channel let the ready-side drain goroutine steal every
			// heartbeat under concurrent dispatch, wedging the watchdog in its
			// launch-suppression branch forever.
			qs, ok := dc.PasteTarget.(quitSender)
			if !ok {
				return
			}
			noChangeTimeoutCh = make(chan struct{})
			watchdogCh := dc.Tap.Subscribe()
			go pasteInjectQuitOnCommit(ctx, rp.Clock, qs, dc.Session, wtPath, headSHA, noChangeTimeoutCh, briefDelivered, watchdogCh, emit, runID)
		},
		OnLaunchedExtra: func(lctx context.Context, sess handler.Session) {
			// Store the session's lifecycle Machine in the RunHandle so the stale
			// watcher can read the current state and drive Ready→Failed(silent_hang)
			// before emitting run_stale (hk-xrygh iter-2).
			if handle, ok := handles.RunRegistry.Get(runID); ok {
				handle.SetMachine(sess.Machine())
			}
			// hk-xnnd: register the implementer identity on the comms bus so peers
			// can attribute escalation messages sent under "<beadID>-impl". Retired
			// by the defer registered below, which fires on every exit path.
			emitImplPresence(lctx, emit, beadID, core.AgentPresenceStatusOnline, core.AgentPresenceReasonJoin)
		},
		// hk-o85ye: SITE-SPECIFIC. Unlike the DOT paths, whose teardown is
		// unconditional, single-mode must NOT kill an independent-session run on
		// daemon shutdown: that session outlives SIGKILL and the next boot's
		// adoption pass monitors it, which is why the shutdown branch below returns
		// without ReopenBead. Killing here would strand the bead in_progress with no
		// live session to adopt. The abort edge, the teardown and the post-wait
		// window kill all carry the identical guard.
		SkipAbortKill: func() bool { return useIndepSession && ctx.Err() != nil },
		SkipTeardown:  func() bool { return useIndepSession && ctx.Err() != nil },
		// hk-5z1f0: agent_ready has resolved (or was skipped) — the cold-start
		// window is over, so give the token back rather than holding it for the
		// whole run body. The scope's close still covers the paths that never
		// reach this edge.
		AfterReadyResolved: func() {
			//nolint:errcheck // the give-back is a receive on a channel this run filled; it cannot fail
			_ = spawnSlot.Release()
		},
	})

	// The resolved harness identity stamps the run-terminal diagnostic. It must
	// be set before any failRun below, which reads sdHarness.
	if launch.Harness != nil {
		sdHarness = string(launch.Harness.AgentType())
	}

	sess := launch.Session
	watcher := launch.Watcher

	switch launch.Fail {
	case agentLaunchPrelaunchFailed:
		// A pre-launch guard refused: srt engagement verification, the srt argv
		// wrap, or the D2 remote-credential check. No session was created.
		reason := launch.FailErr.Error()
		failRun(reason, reason)
		return false
	case agentLaunchErrored:
		reason := fmt.Sprintf("launch error: %v", launch.FailErr)
		failRun(reason, reason)
		// succeeded is never assigned before this point, so the explicit false is
		// byte-equivalent to the pre-RT14 naked return (nakedret).
		return false
	case agentLaunchReadyTimeout, agentLaunchOK:
		// A session exists; register its cleanup defers before deciding.
	}

	// hk-j6wm7: on a Pi FAILURE, persist the session's stderr tail alongside the
	// captured stdout so the fast-fail error output survives with the retained
	// worktree. Registered here so it runs BEFORE the deferred wtCleanup (LIFO:
	// wtCleanup was registered earlier at the worktree-factory step). On success
	// this is a no-op — the worktree is cleaned up so no capture is needed.
	// sess.Outcome() blocks until the run's Wait completes, which has already
	// happened by the time this defer fires.
	if runIsPi && launch.PiCaptureDir != "" {
		capturedSess := sess
		capturedCaptureDir := launch.PiCaptureDir
		defer func() {
			if bridge.Success() {
				return
			}
			if capturedSess == nil {
				return
			}
			tail := capturedSess.Outcome().StderrTail
			if len(tail) == 0 {
				return
			}
			stderrPath := filepath.Join(capturedCaptureDir, "pi-stderr.log")
			if wErr := os.WriteFile(stderrPath, tail, 0o644); wErr != nil { //nolint:gosec // G306: post-mortem log, not a secret
				fmt.Fprintf(os.Stderr, "daemon: workloop: hk-j6wm7: write pi-stderr.log: %v\n", wErr)
			}
		}()
	}

	// Stop the heartbeat and tear the session down, in that order. Deferred
	// rather than run inside the launch so the CHB-019 heartbeat keeps beating
	// through the whole post-run spine below — it is what holds the stale
	// watcher's dead-process reap off a long merge or no-commit inspection.
	// Registered after the Pi capture defer so, under LIFO, teardown still runs
	// BEFORE that defer reads sess.Outcome().
	defer launch.Cleanup()

	// hk-xnnd: retire the implementer identity on the comms bus. The join is
	// emitted by the launch's onLaunched hook; this defer fires the leave on
	// every exit path (normal, abort, error).
	defer func() {
		emitImplPresence(context.Background(), emit, beadID, core.AgentPresenceStatusOffline, core.AgentPresenceReasonLeave)
	}()

	if launch.Fail == agentLaunchReadyTimeout {
		// RT7 / RSM-031 row 1: the ready-timeout Dispatch terminal maps onto the
		// Run reopen spine (reopen "agent_ready_timeout" + run_failed).
		failRun("agent_ready_timeout", "agent_ready_timeout")
		// succeeded is never assigned before this point, so the explicit false is
		// byte-equivalent to the pre-RT14 naked return (nakedret).
		return false
	}

	socketOutcome, ei := launch.SocketOutcome, launch.Exit

	// hk-0z5x: per-run abort check — fired when the never-spawned reaper in
	// StaleWatcher cancels the per-run context (ctx) because launch_initiated
	// was observed but agent_ready never arrived within NeverSpawnedReaperTimeout.
	//
	// Distinguish from daemon-wide shutdown (where ctx is also cancelled but
	// handle.aborted is NOT set): check handle.aborted before treating this as
	// a per-run abort. Daemon shutdown falls through to the existing ctx.Err()
	// check in the no-commit path which leaves the item 'dispatched' for QM-002a
	// recovery.
	if ctx.Err() != nil {
		if handle, ok := handles.RunRegistry.Get(runID); ok && handle.Aborted() {
			// RT7 / RSM-031 row 1b: the never-spawned-reaper abort is the Aborted
			// dispatch-terminal class; its reason rides the mode-failure event
			// (reopen + run_failed via the spine, Background ctx per RSM-022).
			const abortReason = "never_spawned_reaper: launch_initiated but agent_ready not received within deadline"
			failRun(abortReason, abortReason)
			return
		}
		// ctx cancelled for other reasons (daemon shutdown) — fall through to the
		// existing daemon-shutdown handling in the no-commit path.
	}

	// HC-065: Drive StateTerminating → StateTerminated/StateFailed transitions.
	// The session has exited (the completion wait returned). Attempt to advance
	// the Machine through Terminating to a terminal state. Transitions that are
	// invalid for the current state (e.g. machine already in StateFailed from
	// agent_failed) are silently ignored.
	transitionToTerminated(context.Background(), sess.Machine(), runID, emit,
		ei.ExitCode, ei.WaitErr)

	// Step 7a: emit implementer_phase_complete (hk-cd8yu).
	//
	// Fires immediately after the implementer session ends regardless of how —
	// normal exit, noChange-timeout kill, or context cancellation — closing the
	// diagnostic gap between run_started and reviewer_launched where silent
	// implementer failures previously produced no structured event.
	//
	// commitLanded is determined by comparing the current worktree HEAD against
	// headSHA.  gitprobe.ResolveWorktreeHEAD errors are treated as "not landed" (conservative).
	// REMOTE: route via runRunner so HEAD is read from the worker (nil ⇒ box-A-local).
	//
	// hk-368i4: implementerPhaseDur is captured ONCE here and reused by the
	// no-work detector below, so the event's duration_seconds and the detector's
	// verdict are computed from the same measurement — a reader correlating the
	// two can never see them disagree.
	implementerPhaseDur := rp.Clock.Since(launch.LaunchedAt)
	{
		curHead, _ := gitprobe.ResolveWorktreeHEADVia(ctx, runRunner, wtPath)
		commitLanded := curHead != "" && curHead != headSHA
		runlaunch.EmitImplementerPhaseComplete(ctx, emit, runID, ei.ExitCode, ei.StderrTail,
			commitLanded, implementerPhaseDur)
	}

	// ── ProcessExit daemon-side commit fallback (hk-gd9r / hk-mazln) ─────────
	//
	// codex: --sandbox workspace-write blocks writes to .git. The daemon runs
	// git OUTSIDE the sandbox and calls codex.EnsureRefsTrailer to stage+commit
	// any worktree changes codex produced but could not commit.
	//
	// Pi: unsandboxed, so Pi can self-commit, but a weak free model may not (or
	// may omit the trailer). ensurePiRefsTrailer applies the same deterministic
	// fallback so the standard trailer-detection path succeeds.
	//
	// Shared decision table (see internal/harness/codex/commit.go / internal/harness/pi/commit.go):
	//   • HEAD already carries "Refs: <beadID>" → no-op (agent self-committed).
	//   • HEAD advanced but lacks the trailer → amend HEAD to add it.
	//   • HEAD unchanged, worktree dirty → stage all + create trailer commit.
	//   • HEAD unchanged, worktree clean → no_change (fall through to guard).
	//
	// Fires only for CompletionProcessExit harnesses (codex, pi). claude runs
	// through the interactive TUI and self-commits; this block is a no-op for
	// claude. On error we log and fall through to the no-commit guard.
	if handles.HarnessRegistry != nil {
		agType := shared.ArtifactAgentType(artifacts)
		if h, hErr := handles.HarnessRegistry.ForAgent(agType); hErr == nil &&
			h.Completion() == handlercontract.CompletionProcessExit {
			if agType == core.AgentTypePi {
				outcome, ensureErr := pi.EnsureRefsTrailer(ctx, runRunner, wtPath, headSHA, beadID)
				if ensureErr != nil {
					fmt.Fprintf(os.Stderr, "daemon: workloop: ensurePiRefsTrailer bead %s: %v (falling through to no-commit guard)\n",
						beadID, ensureErr)
				} else {
					fmt.Fprintf(os.Stderr, "daemon: workloop: ensurePiRefsTrailer bead %s: %s\n",
						beadID, outcome)
				}
			} else {
				outcome, ensureErr := codex.EnsureRefsTrailer(ctx, runRunner, wtPath, headSHA, beadID)
				if ensureErr != nil {
					fmt.Fprintf(os.Stderr, "daemon: workloop: ensureCodexRefsTrailer bead %s: %v (falling through to no-commit guard)\n",
						beadID, ensureErr)
				} else {
					fmt.Fprintf(os.Stderr, "daemon: workloop: ensureCodexRefsTrailer bead %s: %s\n",
						beadID, outcome)
					// hk-368i4: a no-change outcome from a phase that finished in
					// seconds is a no-work run, not a bead that had nothing to do.
					// Diagnostic only — the run is already failing via the
					// no-commit guard; this records WHY, which is what was
					// missing when hk-jcrzn went undetected.
					if codex.NoWorkSuspected(outcome, implementerPhaseDur, env.CodexNoWorkDurationFloor) {
						floor := codex.NoWorkFloor(env.CodexNoWorkDurationFloor)
						fmt.Fprintf(os.Stderr,
							"daemon: workloop: bead %s: implementer produced NO commit and a clean worktree after only %v (floor %v) — suspected no-work run (hk-368i4)\n",
							beadID, implementerPhaseDur, floor)
						codex.EmitImplementerNoWorkSuspected(ctx, emit, runID, beadID, implementerPhaseDur, floor)
					}
				}
			}
		}
	}

	// Step 8: map Wait-return to a terminal event (CHB-020 branches 1/2/3).
	term := handler.MapWaitReturnToTerminalEvent(
		artifacts.HandlerSessionID, ei.ExitCode, ei.WaitErr, socketOutcome,
	)

	// Step 9: emit terminal event and close or reopen the bead.
	//
	// Bridge-wired path (CHB-020): the terminal event type drives the decision.
	// When no stop-hook outcome arrived (branch 3) AND the handler exited 0
	// without a watcher error, we fall back to the pre-bridge close-on-exit-0
	// heuristic so that existing test fixtures (shell scripts that exit 0) and
	// twin-blind runs continue to work as expected.
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
	transitionTID, _ := handles.TIDGen.Next()

	// RT7: wire the single-mode terminal-spine hooks (gate → code-sync → merge →
	// close/reopen) now that the merge-window context is in scope (runbridge.go).
	bridge.WireSpine(runloop.SpineArgs{
		RunRunner:       runRunner,
		WTPath:          wtPath,
		HeadSHA:         headSHA,
		PreMergeSync:    preMergeSync,
		MPort:           mport,
		ActiveRepo:      activeRepo,
		ProtectBranches: effectiveMergeProtectBranches,
		TransitionTID:   transitionTID,
		EmitBeadClosed: func(c context.Context) {
			emitBeadClosedAndMaybeEpic(c, rp, handles, runID, beadID)
		},
		MergeTarget: mergeTarget, // hk-lgykq: per-bead integration-branch landing target (resolved baseBranch w/ fallback)
	})

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
	// hk-zguy6 / RSM-018: run the escape check inside the merge exclusion domain
	// (mergeq) so that a sibling's commit-phase update-ref → reset-hard sequence
	// cannot race with this read. The commit phase runs via the same queue, so
	// while this read-only slot holds the domain no sibling can be in a transient
	// dirty state — race-free without any path-exclusion heuristic.
	//
	// Bead: hk-6zylj, hk-zguy6, hk-xux36.
	var mainDirty bool
	var dirtyFiles []string
	var escapeErr error
	if subErr := mport.Submit()(ctx, "escape-check", func(qctx context.Context) error {
		mainDirty, dirtyFiles, escapeErr = runmerge.CheckMainWorkingTreeDirty(qctx, activeRepo, preRunUntracked)
		return nil
	}); subErr != nil {
		// Domain unavailable (shutdown) — treat as an errored check (no escape flag).
		escapeErr = subErr
	}
	if escapeErr == nil && mainDirty {
		// The full-payload durable event is emitted here (the machine's reopen
		// spine then carries the classified reason, RSM-031/033 row 2 — the
		// guards run for every dispatch-terminal class, so escape maps onto the
		// mode-failure edge rather than the close-class-only Guarding phase).
		emitImplementerEscapedWorktree(ctx, emit, runID, beadID, activeRepo, dirtyFiles)
		failReason := fmt.Sprintf("implementer_escaped_worktree: %d file(s) dirty in main: %s",
			len(dirtyFiles), strings.Join(dirtyFiles, ", "))
		failRun(failReason, failReason)
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
	// repo's main branch (cross-repo: activeRepo; local: env.ProjectDir).
	if curHeadSHA, curHeadErr := gitprobe.ResolveWorktreeHEADVia(ctx, runRunner, wtPath); curHeadErr == nil &&
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
		failReason := fmt.Sprintf("no_commit_during_implementer: HEAD did not advance past parent %s at iteration 1 exit=%d", headSHA, ei.ExitCode)
		failRun(failReason, failReason)
		return
	}

	// ── RT7: dispatch-terminal classification → the Run machine ──────────────
	//
	// The shell classifies the Dispatch terminal (CHB-020 branches + the frozen-
	// commit watchdog) and synthesizes the corresponding Run event (A1 §3); the
	// machine then owns the gate → code-sync → merge → close/reopen tail via the
	// spine hooks wired above.
	switch {
	case term.Type == handlercontract.ProgressMsgTypeAgentCompleted:
		// CHB-020 branch 1: stop-hook WORK_COMPLETE or REVIEWER_VERDICT. Latches
		// path label "agent_completed" + its close summary (RSM-033).
		bridge.Feed(ctx, runexec.Event{Kind: runexec.EvAgentCompleted, Detail: "agent_completed: stop-hook outcome"})

	case socketOutcome == nil && ei.ExitCode == exitCodeClean && !watcherFailed:
		// No stop-hook arrived AND handler exited 0 without watcher error: the
		// pre-bridge close-on-exit-0 heuristic for twin-blind runs. Latches
		// path label "auto-close" (RSM-033).
		bridge.Feed(ctx, runexec.Event{Kind: runexec.EvCleanExit, Detail: "auto-close: exit=0"})

	default:
		// noChange-timeout path (hk-trjef): pasteInjectQuitOnCommit killed the
		// session after commitPollTimeout fired without a new commit.  Check whether
		// the bead was already subsumed by a prior run that landed on main.
		select {
		case <-noChangeTimeoutCh:
			if shared.MainHistoryHasRefsTrailer(ctx, activeRepo, beadID) {
				// RSM-035: subsumed-but-stalled closes with an approved outcome;
				// the emit-approved flag + close summary ride the event.
				bridge.Feed(ctx, runexec.Event{
					Kind: runexec.EvModeOutcome, ModeOutcome: runexec.ModeSubsumed,
					EmitOutcome: true, Detail: "noChange-subsumed: bead found in main",
				})
			} else {
				// The reopen hook applies the hk-e3fy Background fallback when the
				// stale watcher cancelled the per-run ctx (RSM-022).
				failRun("noChange-timeout", "noChange-timeout: no commit in commitPollTimeout window")
			}
		default:
			// hk-ly0hg Fix-1: context-cancel path — daemon is shutting down.
			if ctx.Err() != nil {
				// hk-o85ye: independent session path — leave session and worktree alive.
				// The session survives SIGKILL; on next boot the adoption pass detects
				// it, waits for Claude to finish, then resets the bead for re-dispatch.
				// The deferred cleanup (wtCleanup, runlaunch.ForceTeardownSession) is skipped by
				// the useIndepSession guard. No ReopenBead: bead stays in_progress so
				// QM-002a on next boot leaves the queue item dispatched (alive) ✓.
				if useIndepSession {
					return
				}
				// hk-dnrg: drain committed-but-unmerged runs on shutdown instead of
				// abandoning. A run that already committed in its worktree must be
				// merged before exit — abandoning it causes re-dispatch (wasted or
				// duplicated work) on next boot. RT9 / RSM-021: the drain rides the
				// machine's EvShutdownDrain edge; the background-context and
				// no-sessiondata policies live in the effector (runbridge), the
				// drain summaries + requeue-recovery reopen reason in the machine.
				// No commit (or HEAD probe failure) feeds an empty SHA → the
				// requeue reopen with no run terminal (QM-002a reverts the queue
				// item to pending at next startup, hk-ly0hg Fix-1 / hk-1h5q).
				drainSHA := ""
				if curHeadSHA, headErr := gitprobe.ResolveWorktreeHEAD(context.Background(), wtPath); headErr == nil && curHeadSHA != "" && curHeadSHA != headSHA {
					drainSHA = curHeadSHA
				}
				bridge.Drain(ctx, drainSHA)
				return bridge.Success()
			}

			// CHB-020 branch 2 (FAILURE_SIGNAL), branch 3 with non-zero exit, or
			// watcher failure (malformed NDJSON, panic, etc.).
			var failReason string
			if watcherFailed {
				failReason = fmt.Sprintf("watcher error: %v exit=%d run_id=%s",
					watcherErr, ei.ExitCode, runID.String())
			} else if term.SubReason != "" {
				failReason = fmt.Sprintf("agent_failed class=%s sub_reason=%s exit=%d run_id=%s",
					term.Class, term.SubReason, ei.ExitCode, runID.String())
			} else {
				failReason = fmt.Sprintf("exit=%d run_id=%s", ei.ExitCode, runID.String())
			}
			// Surface stderr tail when available — helps diagnose exit=-1 crashes
			// where the agent produced no NDJSON output (hk-ajhqw).
			if len(ei.StderrTail) > 0 {
				const maxTailInReason = 200
				tail := ei.StderrTail
				truncated := ""
				if len(tail) > maxTailInReason {
					tail = tail[len(tail)-maxTailInReason:]
					truncated = " (truncated)"
				}
				fmt.Fprintf(os.Stderr, "daemon: workloop: bead %s run %s stderr tail%s:\n%s\n",
					beadID, runID.String(), truncated, tail)
				failReason += fmt.Sprintf(" stderr_tail%s=%q", truncated, tail)
			}
			failRun(failReason, "auto-reopen: "+failReason)
		}
	}
	return bridge.Success()
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
// shared.MainHistoryHasRefsTrailer. Mirrors the review-loop guard
// (reviewloop.go ~567), which compares HEAD == parentSHA with no escape.
//
// Bead: hk-4ie1z.
func noCommitGuardShouldReopen(ctx context.Context, projectDir, curHeadSHA, parentSHA string, beadID core.BeadID) bool {
	if curHeadSHA != parentSHA {
		// The implementer advanced HEAD — a commit exists; the guard does not fire.
		return false
	}
	// No commit. Fail (reopen) UNLESS this bead's own work is already on main.
	return !shared.MainHistoryHasRefsTrailer(ctx, projectDir, beadID)
}

// productionWorktreeFactory is the default worktreeFactory: creates a real git
// worktree under the project's .harmonik/worktrees/ directory and returns the
// path plus a cleanup function that removes it.
//
// After the git worktree is ready, it symlinks <projectDir>/.tools into the
// worktree so that Makefile fmt/lint targets (gci, golangci-lint, gofumpt) can
// resolve their pinned binaries. .tools/ is gitignored and only installed in the
// main repo; without this link agents skip formatting silently and codex
// auto-commits land unchecked (hk-gb3ln). The symlink itself is ignored by
// /.tools in the root .gitignore (no trailing slash) so git add -A in the
// worktree never stages it. Creation is best-effort: if .tools/ is absent from
// projectDir (e.g. a fresh clone without `make tools`) the worktree still
// launches normally.
//
// Bead ref: hk-kqdpf.1, hk-gb3ln.
func productionWorktreeFactory(ctx context.Context, projectDir, runID, headSHA string) (string, func(), error) {
	if err := workspace.CreateWorktree(ctx, projectDir, runID, headSHA, workspace.NoWorktreeRootOverride()); err != nil {
		return "", nil, err
	}
	wtPath := workspace.WorktreePath(projectDir, runID, workspace.NoWorktreeRootOverride())

	// Symlink .tools from the project root into the worktree so Makefile
	// fmt/lint targets resolve their pinned binaries (hk-gb3ln).
	toolsSrc := filepath.Join(projectDir, ".tools")
	if _, statErr := os.Lstat(toolsSrc); statErr == nil {
		_ = os.Symlink(toolsSrc, filepath.Join(wtPath, ".tools"))
	}

	// The cleanup uses background context so removal is attempted even when the
	// per-bead context has been cancelled (e.g. on daemon shutdown or test
	// cancellation). This mirrors the intent of the original `defer removeWorktree`
	// call — git worktree prune is best-effort.
	cleanup := func() {
		runmerge.RemoveWorktree(context.Background(), projectDir, wtPath)
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
// Event helpers
// ─────────────────────────────────────────────────────────────────────────────

// workloopRunStartedPayload is the minimal run_started payload emitted by the
// work loop.  Full RunStartedPayload requires WorkflowID / WorkflowVersion
// which are deferred; we emit a raw map so the event is observable without
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
	// WorkflowMode is the resolved workflow mode for this run (hk-zhysl observability).
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

type d2Refusal string

//nolint:gosec // G101: refusal text names an environment variable; it contains no credential.
const d2APIKeyRefusal d2Refusal = "remote run: ANTHROPIC_API_KEY in spawn env (D2 fail-closed)"

// d2RemoteAPIKeyRefusal makes the post-build, pre-launch D2 decision. Keeping
// the remote/local distinction in this predicate makes every harness use the
// same fail-closed behavior without coupling the decision to an agent type.
func d2RemoteAPIKeyRefusal(remote bool, env []string) (d2Refusal, bool) {
	if remote && hasAPIKeyInEnv(env) {
		return d2APIKeyRefusal, true
	}
	return "", false
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
// bead-closed / epic-completion emission (the merge path itself moved to
// internal/runmerge in P2 unit E5 RT13; these helpers stay in the daemon shell
// but now reach the emittedEpics dedupe set / mutex and the ledger+emitter
// through the SharedHandles + RunPorts bundles rather than raw workLoopDeps,
// so the runBridge close hook can drop deps entirely (RT18.9)).
// ─────────────────────────────────────────────────────────────────────────────

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
func emitBeadClosedAndMaybeEpic(ctx context.Context, ports runloop.RunPorts, handles runloop.SharedHandles, runID core.RunID, beadID core.BeadID) {
	emitBeadClosed(ctx, ports.Emitter, runID, beadID)
	maybeEmitEpicCompleted(ctx, ports, handles, runID, beadID)
}

// maybeEmitEpicCompleted checks whether closedBeadID's parent epic now has all
// children closed, and if so emits epic_completed exactly once (AC-1 at-most-once
// per daemon session). Zero-emit on: no parent (AC-4), still-open sibling (AC-3),
// or already-emitted guard hit.
//
// Bead: hk-w6y70.
func maybeEmitEpicCompleted(ctx context.Context, ports runloop.RunPorts, handles runloop.SharedHandles, runID core.RunID, closedBeadID core.BeadID) {
	ledger := ports.Ledger
	// Step 1: ShowBead(closedBead) to find the parent via a parent-child edge.
	// The closed bead's outgoing parent-child edge has FromBeadID == closedBead,
	// ToBeadID == parent (per brcli/show.go: dependencies[] → outgoing edges).
	closedRecord, err := ledger.ShowBead(ctx, closedBeadID)
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
	parentRecord, err := ledger.ShowBead(ctx, parentID)
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
	handles.EmittedEpicsMu.Lock()
	if _, already := handles.EmittedEpics[parentID]; already {
		handles.EmittedEpicsMu.Unlock()
		return
	}
	handles.EmittedEpics[parentID] = struct{}{}
	handles.EmittedEpicsMu.Unlock()

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
	_ = ports.Emitter.EmitWithRunID(ctx, runID, core.EventTypeEpicCompleted, b)
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
