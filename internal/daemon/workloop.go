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
	"github.com/gregberns/harmonik/internal/workspace"
)

// workloopPollInterval is the sleep duration between bead-ledger polls when the
// ready queue is empty.
const workloopPollInterval = 2 * time.Second

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
	// obtain the adapter. When nil, waitAgentReady is skipped (test mode).
	//
	// Bead ref: hk-gql20.14.
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
		projectCfg:          cfg.ProjectCfg,
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
//  3. Poll Ready beads.
//  4. If none, sleep workloopPollInterval and retry.
//  5. Pick beads[0], generate RunID, claim it.
//  6. Spawn goroutine: Register → dispatch (worktree+handler) → Unregister.
//
// Goroutine dispatch path:
//  1. resolveHEAD + CreateWorktree.
//  2. emitRunStarted.
//  3. Route to mode-specific driver (review-loop or single).
//  4. CloseBead or ReopenBead based on outcome.
//  5. removeWorktree.
//  6. Unregister from runRegistry.
//
// At MaxConcurrent=1 the loop is semantically equivalent to the prior serial
// implementation: only one goroutine is ever in-flight, so the poll loop
// blocks on capacity before polling again.
//
// Shutdown: when ctx is cancelled the outer loop exits immediately. The
// embedded WaitGroup wg waits for all in-flight goroutines to drain before
// runWorkLoop returns, satisfying the per-run Drain guarantee (hk-fx6zl).
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

	for {
		// Step 1: check for cancellation before any new dispatch.
		select {
		case <-ctx.Done():
			// Stop accepting new work; wait for in-flight goroutines.
			wg.Wait()
			return nil
		default:
		}

		// Step 2: capacity gate — if at the concurrent limit, sleep and retry.
		if deps.runRegistry.Len() >= effectiveMax {
			if sleepErr := workloopSleep(ctx, workloopPollInterval); sleepErr != nil {
				wg.Wait()
				return nil
			}
			continue
		}

		// Step 3: poll the ledger for ready beads.
		readyRecords, err := deps.brAdapter.Ready(ctx)
		if err != nil {
			// Treat poll errors as transient: log and backoff.
			if ctx.Err() != nil {
				wg.Wait()
				return nil
			}
			// Non-fatal: surface to stderr so operators can diagnose CWD/PATH
			// misconfiguration (hk-c1ln2: silent-failure fix).
			fmt.Fprintf(os.Stderr, "daemon: workloop: Ready poll error (will retry): %v\n", err)
			if sleepErr := workloopSleep(ctx, workloopPollInterval); sleepErr != nil {
				wg.Wait()
				return nil
			}
			continue
		}

		// Step 4: nothing ready — sleep and retry.
		if len(readyRecords) == 0 {
			if sleepErr := workloopSleep(ctx, workloopPollInterval); sleepErr != nil {
				wg.Wait()
				return nil
			}
			continue
		}

		// Step 5: pick first ready bead, generate RunID, claim it.
		// Labels (including any workflow:<mode> per BI-009a) are available on
		// the record for workflow-mode resolution (BI-013); mode-resolution
		// dispatch is implemented in T-WM-009.
		beadRecord := readyRecords[0]
		beadID := beadRecord.BeadID

		runUUID, uuidErr := uuid.NewV7()
		if uuidErr != nil {
			// UUID generation failure is fatal — system entropy problem.
			wg.Wait()
			return fmt.Errorf("daemon: workloop: generate RunID: %w", uuidErr)
		}
		runID := core.RunID(runUUID)

		claimTID, tidErr := deps.tidGen.Next()
		if tidErr != nil {
			wg.Wait()
			return fmt.Errorf("daemon: workloop: generate claim TransitionID: %w", tidErr)
		}

		// Pre-claim status guard (hk-p4xbw): read the bead's current status and
		// skip dispatch if it is no longer "open".  This is the harmonik-side
		// guard against double-dispatch when two concurrent work loops both
		// observe the same bead from Ready before either has claimed it.
		//
		// TOCTOU note: a competing loop could claim the bead in the window
		// between ShowBead and ClaimBead below.  In that case ClaimBead still
		// proceeds and br returns exit 0 (br v0.1.45 does not reject a second
		// concurrent claim); the claim semaphore (hk-e61c3.3) ensures this
		// window is narrow.  Cross-daemon double-dispatch is deferred to the
		// upstream br patch (option 2, out of scope).
		showRecord, showErr := deps.brAdapter.ShowBead(ctx, beadID)
		if showErr != nil {
			if ctx.Err() != nil {
				return nil
			}
			fmt.Fprintf(os.Stderr, "daemon: workloop: ShowBead pre-claim check %s error (will retry): %v\n", beadID, showErr)
			if sleepErr := workloopSleep(ctx, workloopPollInterval); sleepErr != nil {
				return nil
			}
			continue
		}
		if showRecord.Status != core.CoarseStatusOpen {
			// Bead was claimed (or otherwise transitioned) by a competing loop.
			// Skip dispatch — the other loop owns this bead.
			fmt.Fprintf(os.Stderr, "daemon: workloop: bead_claim_skipped %s status=%s (competing claim won)\n", beadID, showRecord.Status)
			if sleepErr := workloopSleep(ctx, workloopPollInterval); sleepErr != nil {
				return nil
			}
			continue
		}

		// Acquire the claim semaphore before the SQLite write (hk-e61c3.3).
		// The select allows ctx cancellation to abort the acquire so the loop
		// does not block indefinitely on shutdown.
		select {
		case claimSem <- struct{}{}:
		case <-ctx.Done():
			wg.Wait()
			return nil
		}
		claimErr := deps.brAdapter.ClaimBead(ctx, deps.intentLogDir, deps.brTimeoutCfg, runID, claimTID, beadID)
		// Release the semaphore immediately after the write completes.
		<-claimSem
		if claimErr != nil {
			// Claim conflict or transient error — surface to stderr and retry.
			if ctx.Err() != nil {
				wg.Wait()
				return nil
			}
			fmt.Fprintf(os.Stderr, "daemon: workloop: ClaimBead %s error (will retry): %v\n", beadID, claimErr)
			if sleepErr := workloopSleep(ctx, workloopPollInterval); sleepErr != nil {
				wg.Wait()
				return nil
			}
			continue
		}

		// Step 6: Register the run and spawn a goroutine to handle it end-to-end.
		// The goroutine owns Unregister on exit; the outer loop may proceed to
		// claim the next bead immediately (up to effectiveMax).
		deps.runRegistry.Register(runID, &RunHandle{
			BeadID:    beadID,
			StartedAt: time.Now(),
		})
		wg.Add(1)
		go func(runID core.RunID, beadRecord core.BeadRecord) {
			defer wg.Done()
			defer deps.runRegistry.Unregister(runID)
			beadRunOne(ctx, deps, runID, beadRecord)
		}(runID, beadRecord)
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
// Bead ref: hk-e61c3.2.
func beadRunOne(ctx context.Context, deps workLoopDeps, runID core.RunID, beadRecord core.BeadRecord) {
	beadID := beadRecord.BeadID

	// Resolve workflow_mode per execution-model.md §4.3.EM-012a.
	// Four-tier precedence: per-bead label → project config (no-op) →
	// daemon default → single. Resolved once at claim time; immutable for
	// the run's lifetime. See moderesolve.go.
	workflowMode := resolveWorkflowMode(ctx, beadRecord, deps.workflowModeDefault, deps.bus)

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

	// Emit run_started.
	emitRunStarted(ctx, deps.bus, runID, beadID, wtPath)

	// Mode-dispatch: route to the mode-specific driver.
	//
	// review-loop mode (EM-015d): multi-iteration implementer→reviewer cycle
	// handled by runReviewLoop in reviewloop.go.
	//
	// single mode (default MVH): one-shot implementer dispatch.
	if workflowMode == core.WorkflowModeReviewLoop {
		rlResult := runReviewLoop(ctx, deps, runID, beadID, beadRecord.Title, beadRecord.Description, wtPath, headSHA, resolvedModel, resolvedEffort)

		transitionTID, _ := deps.tidGen.Next()
		if rlResult.success {
			if closeErr := deps.brAdapter.CloseBead(ctx, deps.intentLogDir, deps.brTimeoutCfg, runID, transitionTID, beadID, false); closeErr != nil {
				fmt.Fprintf(os.Stderr, "daemon: workloop: CloseBead (review-loop APPROVE) %s: %v\n", beadID, closeErr)
				emitRunCompleted(ctx, deps.bus, runID, false, fmt.Sprintf("close-error: %v", closeErr))
			} else {
				emitRunCompleted(ctx, deps.bus, runID, true, rlResult.summary)
			}
		} else {
			if closeErr := deps.brAdapter.CloseBead(ctx, deps.intentLogDir, deps.brTimeoutCfg, runID, transitionTID, beadID, rlResult.needsAttention); closeErr != nil {
				fmt.Fprintf(os.Stderr, "daemon: workloop: CloseBead (review-loop %s) %s: %v\n", rlResult.completionReason, beadID, closeErr)
			}
			emitRunCompleted(ctx, deps.bus, runID, false, rlResult.summary)
		}
		return
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
		emitRunCompleted(ctx, deps.bus, runID, false, fmt.Sprintf("build launch spec error: %v", specErr))
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
	spec.Substrate = deps.substrate

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
	// Use deps.adapterRegistry when available; fall back to a fresh empty
	// registry when nil. NewHandler panics on nil registry.
	tapRegistry := deps.adapterRegistry
	if tapRegistry == nil {
		tapRegistry = handlercontract.NewAdapterRegistry()
	}
	runH := handler.NewHandler(tap, handlercontract.NoopWatcherDeadLetter{}, tapRegistry)

	sess, watcher, launchErr := runH.Launch(ctx, spec)
	if launchErr != nil {
		fmt.Fprintf(os.Stderr, "daemon: workloop: Launch bead %s run %s: %v (reopening)\n",
			beadID, runID.String(), launchErr)
		reopenTID, _ := deps.tidGen.Next()
		_ = deps.brAdapter.ReopenBead(ctx, deps.intentLogDir, deps.brTimeoutCfg, runID, reopenTID, beadID,
			fmt.Sprintf("launch error: %v", launchErr))
		emitRunCompleted(ctx, deps.bus, runID, false, fmt.Sprintf("launch error: %v", launchErr))
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
	deps.hookStore.SetAgentReadyCallback(runID.String(), artifacts.claudeSessionID, func() {
		_ = tap.Emit(context.Background(), core.EventTypeAgentReady, nil)
	})

	// Step 4b: paste-inject the kick-off message into the Claude pane (hk-zrj83).
	//
	// Step 5: start CHB-019 heartbeat goroutine.  Daemon-owned per OQ5 resolution.
	hbDone := make(chan struct{})
	go handler.RunHeartbeatLoop(ctx, artifacts.handlerSessionID,
		handler.HeartbeatInterval, hbDone,
		newDaemonHeartbeatEmitter(deps.bus, runID))
	defer close(hbDone)

	// Step 6: waitAgentReady — HC-056 agent_ready timeout guard.
	// Obtain the adapter from the registry for DetectReady; skip when registry
	// is nil (test mode with no adapters registered).
	//
	// HC-056 timeout semantics: we only treat this as a hard failure requiring
	// reopen if the SPECIFIC HC-056 timeout sentinel (ErrAgentReadyTimeout)
	// fires. If the watcher exits first (handler crash, clean exit without
	// agent_ready) the watcher-done cancel fires first, returning
	// context.Canceled — in that case we skip the reopen and fall through to
	// the normal waitWithSocketGrace path which handles the exit correctly per
	// CHB-020 branch 3.
	if deps.adapterRegistry != nil {
		adapter, adapterErr := deps.adapterRegistry.ForAgent(core.AgentTypeClaudeCode)
		if adapterErr != nil {
			// No adapter for claude-code — non-fatal at MVH; skip ready-wait.
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
					<-watcher.Done()
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
				emitRunCompleted(ctx, deps.bus, runID, false, "agent_ready_timeout")
				return
			}
			// readyErr == nil (agent_ready observed) OR context.Canceled (watcher
			// exited first, outer ctx cancelled, or watcher-done cancel).
			// Fall through to waitWithSocketGrace.
		}
	}
	_ = tapCh // suppress unused-variable lint when adapterRegistry is nil

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
	go pasteInjectOnLaunch(ctx, deps.substrate, artifacts.claudeSessionID,
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
	// Spec ref: specs/claude-hook-bridge.md §4.11 CHB-028.
	// Bead: hk-cmybm.
	if qs, ok := deps.substrate.(quitSender); ok {
		go pasteInjectQuitOnCommit(ctx, qs, wtPath, headSHA)
	}

	// Step 7: wait for the watcher to finish (handler exit or ctx cancel) then
	// apply the stop-hook grace window for a pending outcome_emitted payload.
	socketOutcome, ei := waitWithSocketGrace(ctx, deps.hookStore, watcher, sess,
		runID.String(), artifacts.claudeSessionID)

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

	switch {
	case term.Type == handlercontract.ProgressMsgTypeAgentCompleted:
		// CHB-020 branch 1: stop-hook WORK_COMPLETE or REVIEWER_VERDICT.
		// §4.12.EM-052: merge run-branch to main before CloseBead.
		mergeRes := mergeRunBranchToMain(ctx, deps.projectDir, runID, deps.bus, beadID)
		if !mergeRes.noChange && !mergeRes.success {
			// EM-053: non-FF or push failure → reopen.
			emitOutcomeEmitted(ctx, deps.bus, runID, beadID, "rejected", mergeRes.reason)
			reopenTID, _ := deps.tidGen.Next()
			_ = deps.brAdapter.ReopenBead(ctx, deps.intentLogDir, deps.brTimeoutCfg, runID, reopenTID, beadID,
				fmt.Sprintf("merge-to-main failed: %s", mergeRes.reason))
			emitRunCompleted(ctx, deps.bus, runID, false, fmt.Sprintf("merge-failed (agent_completed): %s", mergeRes.reason))
		} else {
			// Merge succeeded (or no-change); proceed with CloseBead.
			emitOutcomeEmitted(ctx, deps.bus, runID, beadID, "approved", "")
			if closeErr := deps.brAdapter.CloseBead(ctx, deps.intentLogDir, deps.brTimeoutCfg, runID, transitionTID, beadID, false); closeErr != nil {
				fmt.Fprintf(os.Stderr, "daemon: workloop: CloseBead (agent_completed) %s: %v\n", beadID, closeErr)
				emitRunCompleted(ctx, deps.bus, runID, false, fmt.Sprintf("close-error: %v", closeErr))
			} else {
				emitBeadClosed(ctx, deps.bus, runID, beadID)
				emitRunCompleted(ctx, deps.bus, runID, true, "agent_completed: stop-hook outcome")
			}
		}

	case socketOutcome == nil && ei.exitCode == 0 && !watcherFailed:
		// No stop-hook arrived AND handler exited 0 without watcher error.
		// Fall back to the pre-bridge close-on-exit-0 heuristic for
		// MVH twin-blind runs.
		//
		// hk-wfbxf: same CloseBead error handling as branch 1.
		// §4.12.EM-052: merge run-branch to main before CloseBead.
		mergeRes := mergeRunBranchToMain(ctx, deps.projectDir, runID, deps.bus, beadID)
		if !mergeRes.noChange && !mergeRes.success {
			// EM-053: non-FF or push failure → reopen.
			emitOutcomeEmitted(ctx, deps.bus, runID, beadID, "rejected", mergeRes.reason)
			reopenTID, _ := deps.tidGen.Next()
			_ = deps.brAdapter.ReopenBead(ctx, deps.intentLogDir, deps.brTimeoutCfg, runID, reopenTID, beadID,
				fmt.Sprintf("merge-to-main failed: %s", mergeRes.reason))
			emitRunCompleted(ctx, deps.bus, runID, false, fmt.Sprintf("merge-failed (auto-close): %s", mergeRes.reason))
		} else {
			// Merge succeeded (or no-change); proceed with CloseBead.
			emitOutcomeEmitted(ctx, deps.bus, runID, beadID, "approved", "")
			if closeErr := deps.brAdapter.CloseBead(ctx, deps.intentLogDir, deps.brTimeoutCfg, runID, transitionTID, beadID, false); closeErr != nil {
				fmt.Fprintf(os.Stderr, "daemon: workloop: CloseBead %s: %v\n", beadID, closeErr)
				emitRunCompleted(ctx, deps.bus, runID, false, fmt.Sprintf("close-error: %v", closeErr))
			} else {
				emitBeadClosed(ctx, deps.bus, runID, beadID)
				emitRunCompleted(ctx, deps.bus, runID, true, "auto-close: exit=0")
			}
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
		_ = deps.brAdapter.ReopenBead(ctx, deps.intentLogDir, deps.brTimeoutCfg, runID, transitionTID, beadID, failReason)
		emitRunCompleted(ctx, deps.bus, runID, false, fmt.Sprintf("auto-reopen: %s", failReason))
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

// workloopSleep sleeps for d or until ctx is cancelled. Returns a non-nil
// error only when ctx is cancelled (non-nil ctx.Err()).
func workloopSleep(ctx context.Context, d time.Duration) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(d):
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
type workloopRunStartedPayload struct {
	RunID         string `json:"run_id"`
	BeadID        string `json:"bead_id"`
	WorkspacePath string `json:"workspace_path"`
	StartedAt     string `json:"started_at"`
}

// workloopRunCompletedPayload is the minimal run_completed / run_failed payload
// emitted by the work loop.
type workloopRunCompletedPayload struct {
	RunID   string `json:"run_id"`
	Success bool   `json:"success"`
	Summary string `json:"summary"`
	EndedAt string `json:"ended_at"`
}

func emitRunStarted(ctx context.Context, bus handlercontract.EventEmitter, runID core.RunID, beadID core.BeadID, wtPath string) {
	pl := workloopRunStartedPayload{
		RunID:         runID.String(),
		BeadID:        string(beadID),
		WorkspacePath: wtPath,
		StartedAt:     time.Now().UTC().Format(time.RFC3339),
	}
	b, err := json.Marshal(pl)
	if err != nil {
		return
	}
	_ = bus.EmitWithRunID(ctx, runID, core.EventTypeRunStarted, b)
}

func emitRunCompleted(ctx context.Context, bus handlercontract.EventEmitter, runID core.RunID, success bool, summary string) {
	pl := workloopRunCompletedPayload{
		RunID:   runID.String(),
		Success: success,
		Summary: summary,
		EndedAt: time.Now().UTC().Format(time.RFC3339),
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
//  2. Fast-forward check (non-FF → EM-053 reopen path).
//  3. git update-ref refs/heads/main <tip>.
//  4. git push origin main.
//  5. git reset --hard HEAD (working-tree refresh, EM-054).
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
func mergeRunBranchToMain(ctx context.Context, projectDir string, runID core.RunID, bus handlercontract.EventEmitter, beadID core.BeadID) mergeOutcome {
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

	// Step 2: fast-forward check.  main MUST be an ancestor of runTip.
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
