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
// claimed bead. The in-flight count is gated by MaxConcurrent via RunRegistry.
// At MaxConcurrent=1 (the MVH default), the loop is semantically equivalent
// to the prior serial implementation: only one goroutine is ever in-flight,
// so behaviour is byte-identical to the pre-parallelism code.
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
// Consequence 1 — no pre-claim body validation: the daemon dispatches a bead
// with an empty or malformed body identically to one with a fully-populated
// brief.  If pre-claim body validation is needed in the future, add ShowBead
// to this interface and call it between Ready and ClaimBead.
//
// Consequence 2 — handler contract: the handler subprocess is responsible for
// calling `br show <beadID> --format json` to obtain the work spec.  For MVH,
// the bead ID is supplied to the handler via the implementer-protocol brief
// in the SCOPE line (i.e., as content of the prompt passed by the operator to
// claude).  Programmatic injection of the bead ID (e.g. a HARMONIK_BEAD_ID
// env var) is a post-MVH hardening task; no bead exists for that yet.
type beadLedger interface {
	Ready(ctx context.Context) ([]core.BeadRecord, error)
	ClaimBead(ctx context.Context, intentLogDir string, cfg brcli.TimeoutConfig, runID core.RunID, transitionID core.TransitionID, beadID core.BeadID) error
	CloseBead(ctx context.Context, intentLogDir string, cfg brcli.TimeoutConfig, runID core.RunID, transitionID core.TransitionID, beadID core.BeadID, needsAttention bool) error
	ReopenBead(ctx context.Context, intentLogDir string, cfg brcli.TimeoutConfig, runID core.RunID, transitionID core.TransitionID, beadID core.BeadID, reason string) error
}

// newWorkLoopDeps constructs the production workLoopDeps from daemon.Config,
// the shared event bus, and the pre-resolved workflowModeDefault.
//
// workflowModeDefault MUST already be normalised by the caller (daemon.Start
// step 0) — it must be a valid WorkflowMode; zero value is never passed in.
func newWorkLoopDeps(cfg Config, bus handlercontract.EventEmitter, workflowModeDefault core.WorkflowMode) (workLoopDeps, error) {
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

	h := handler.NewHandler(bus, handlercontract.NoopWatcherDeadLetter{})

	binary := cfg.HandlerBinary
	if binary == "" {
		binary = "claude"
	}

	// Normalise MaxConcurrent: zero value → 1 (MVH single-threaded default).
	maxConcurrent := cfg.MaxConcurrent
	if maxConcurrent <= 0 {
		maxConcurrent = 1
	}

	return workLoopDeps{
		brAdapter:           adapter,
		bus:                 bus,
		h:                   h,
		intentLogDir:        intentLogDir,
		projectDir:          cfg.ProjectDir,
		handlerBinary:       binary,
		handlerArgs:         cfg.HandlerArgs,
		handlerEnv:          cfg.HandlerEnv,
		brTimeoutCfg:        brcli.TimeoutConfig{},
		tidGen:              core.NewTransitionIDGenerator(),
		workflowModeDefault: workflowModeDefault,
		runRegistry:         NewRunRegistry(),
		maxConcurrent:       maxConcurrent,
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

		claimErr := deps.brAdapter.ClaimBead(ctx, deps.intentLogDir, deps.brTimeoutCfg, runID, claimTID, beadID)
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

	// Resolve HEAD as the parent commit to avoid racing with operator activity
	// in the main worktree per workspace-model.md §4.1 WM-003.
	headSHA, headErr := resolveHEAD(ctx, deps.projectDir)
	if headErr != nil {
		fmt.Fprintf(os.Stderr, "daemon: workloop: resolveHEAD for bead %s: %v (reopening)\n", beadID, headErr)
		reopenTID, _ := deps.tidGen.Next()
		_ = deps.brAdapter.ReopenBead(ctx, deps.intentLogDir, deps.brTimeoutCfg, runID, reopenTID, beadID,
			fmt.Sprintf("resolve HEAD failed: %v", headErr))
		return
	}

	wtErr := workspace.CreateWorktree(ctx, deps.projectDir, runID.String(), headSHA, workspace.NoWorktreeRootOverride())
	if wtErr != nil {
		fmt.Fprintf(os.Stderr, "daemon: workloop: CreateWorktree for bead %s run %s: %v (reopening)\n", beadID, runID.String(), wtErr)
		reopenTID, _ := deps.tidGen.Next()
		_ = deps.brAdapter.ReopenBead(ctx, deps.intentLogDir, deps.brTimeoutCfg, runID, reopenTID, beadID,
			fmt.Sprintf("create worktree failed: %v", wtErr))
		return
	}

	wtPath := workspace.WorktreePath(deps.projectDir, runID.String(), workspace.NoWorktreeRootOverride())
	defer removeWorktree(ctx, deps.projectDir, wtPath)

	// Emit run_started.
	emitRunStarted(ctx, deps.bus, runID, beadID, wtPath)

	// Mode-dispatch: route to the mode-specific driver.
	//
	// review-loop mode (EM-015d): multi-iteration implementer→reviewer cycle
	// handled by runReviewLoop in reviewloop.go.
	//
	// single mode (default MVH): one-shot implementer dispatch.
	if workflowMode == core.WorkflowModeReviewLoop {
		rlResult := runReviewLoop(ctx, deps, runID, beadID, wtPath, headSHA)

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

	// single mode: launch one implementer subprocess and close or reopen on exit.
	spec := handler.LaunchSpec{
		Binary:  deps.handlerBinary,
		Args:    deps.handlerArgs,
		Env:     deps.handlerEnv,
		WorkDir: wtPath,
		Role:    "implementer",
	}
	sess, watcher, launchErr := deps.h.Launch(ctx, spec)
	if launchErr != nil {
		// Launch failure — reopen the bead and surface to stderr.
		fmt.Fprintf(os.Stderr, "daemon: workloop: Launch bead %s run %s: %v (reopening)\n", beadID, runID.String(), launchErr)
		reopenTID, _ := deps.tidGen.Next()
		_ = deps.brAdapter.ReopenBead(ctx, deps.intentLogDir, deps.brTimeoutCfg, runID, reopenTID, beadID,
			fmt.Sprintf("launch error: %v", launchErr))
		emitRunCompleted(ctx, deps.bus, runID, false, fmt.Sprintf("launch error: %v", launchErr))
		return
	}

	// Await watcher completion.
	<-watcher.Done()

	// Reap the subprocess.
	_ = sess.Wait(ctx)
	outcome := sess.Outcome()

	// Emit run_completed/run_failed, close or reopen the bead.
	//
	// hk-9cob3: a watcher error that is NOT context-cancellation means the
	// watcher fired agent_failed (malformed NDJSON, line-too-long, panic, I/O
	// error). Treat as failed even on exit=0 — work product may be corrupt.
	//
	// hk-wfbxf: CloseBead errors must not be silently discarded. If CloseBead
	// fails the bead remains in_progress while JSONL would record
	// run_completed=true — split-brain. Emit run_failed instead.
	watcherErr := watcher.Err()
	watcherFailed := watcherErr != nil && !errors.Is(watcherErr, handlercontract.ErrCanceled)

	transitionTID, _ := deps.tidGen.Next()
	if outcome.ExitCode == 0 && !watcherFailed {
		if closeErr := deps.brAdapter.CloseBead(ctx, deps.intentLogDir, deps.brTimeoutCfg, runID, transitionTID, beadID, false); closeErr != nil {
			fmt.Fprintf(os.Stderr, "daemon: workloop: CloseBead %s: %v\n", beadID, closeErr)
			emitRunCompleted(ctx, deps.bus, runID, false, fmt.Sprintf("close-error: %v", closeErr))
		} else {
			emitRunCompleted(ctx, deps.bus, runID, true, "auto-close: exit=0")
		}
	} else {
		var failReason string
		if watcherFailed {
			failReason = fmt.Sprintf("watcher error: %v exit=%d run_id=%s",
				watcherErr, outcome.ExitCode, runID.String())
		} else {
			failReason = fmt.Sprintf("exit=%d run_id=%s", outcome.ExitCode, runID.String())
		}
		_ = deps.brAdapter.ReopenBead(ctx, deps.intentLogDir, deps.brTimeoutCfg, runID, transitionTID, beadID, failReason)
		emitRunCompleted(ctx, deps.bus, runID, false, fmt.Sprintf("auto-reopen: exit=%d watcher_failed=%v", outcome.ExitCode, watcherFailed))
	}
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
	_ = bus.Emit(ctx, core.EventTypeRunStarted, b)
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
	_ = bus.Emit(ctx, eventType, b)
}
