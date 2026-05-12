package daemon

// workloop.go — MVH main work loop (MVH_ROADMAP row #10, hk-ecrxy).
//
// RunWorkLoop polls the bead ledger for ready work, claims one bead at a time,
// materialises a git worktree, spawns the handler subprocess, waits for it to
// complete, and closes (or reopens) the bead based on the outcome.
//
// # Concurrency
//
// MVH: MaxConcurrent = 1 (one in-flight bead). The loop serialises work:
// claim → worktree → launch → wait → close → repeat. Concurrent runs are a
// post-MVH unlock.
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
// Bead: hk-ecrxy.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
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
}

// beadLedger is the subset of brcli.Adapter used by the work loop.  It is
// extracted as an interface so that workloop_test.go can substitute a stub.
type beadLedger interface {
	Ready(ctx context.Context) ([]core.BeadID, error)
	ClaimBead(ctx context.Context, intentLogDir string, cfg brcli.TimeoutConfig, runID core.RunID, transitionID core.TransitionID, beadID core.BeadID) error
	CloseBead(ctx context.Context, intentLogDir string, cfg brcli.TimeoutConfig, runID core.RunID, transitionID core.TransitionID, beadID core.BeadID) error
	ReopenBead(ctx context.Context, intentLogDir string, cfg brcli.TimeoutConfig, runID core.RunID, transitionID core.TransitionID, beadID core.BeadID, reason string) error
}

// newWorkLoopDeps constructs the production workLoopDeps from daemon.Config and
// the shared event bus.  Returns an error if any required config field is missing.
func newWorkLoopDeps(cfg Config, bus handlercontract.EventEmitter) (workLoopDeps, error) {
	if cfg.BrPath == "" {
		return workLoopDeps{}, fmt.Errorf("daemon: newWorkLoopDeps: Config.BrPath is empty; production callers must resolve br from PATH at startup")
	}
	if cfg.ProjectDir == "" {
		return workLoopDeps{}, fmt.Errorf("daemon: newWorkLoopDeps: Config.ProjectDir is empty; required for worktree creation")
	}

	adapter, err := brcli.New(cfg.BrPath)
	if err != nil {
		return workLoopDeps{}, fmt.Errorf("daemon: newWorkLoopDeps: brcli.New: %w", err)
	}

	intentLogDir := lifecycle.BeadsIntentsDir(cfg.ProjectDir)

	h := handler.NewHandler(bus, handlercontract.NoopWatcherDeadLetter{})

	binary := cfg.HandlerBinary
	if binary == "" {
		binary = "claude"
	}

	return workLoopDeps{
		brAdapter:     adapter,
		bus:           bus,
		h:             h,
		intentLogDir:  intentLogDir,
		projectDir:    cfg.ProjectDir,
		handlerBinary: binary,
		handlerArgs:   cfg.HandlerArgs,
		handlerEnv:    cfg.HandlerEnv,
		brTimeoutCfg:  brcli.TimeoutConfig{},
		tidGen:        core.NewTransitionIDGenerator(),
	}, nil
}

// runWorkLoop is the main dispatch goroutine. It blocks until ctx is cancelled
// (typically from SIGINT/SIGTERM received by the daemon process). On context
// cancellation it returns nil; non-nil errors indicate a fatal setup failure
// within the loop itself (never an error from a single bead run — those are
// absorbed and result in ReopenBead).
//
// One iteration of the outer loop:
//  1. Poll Ready beads.
//  2. If none, sleep workloopPollInterval and try again.
//  3. Pick beads[0], generate a fresh RunID (UUIDv7), claim it.
//  4. Create the git worktree.
//  5. Emit run_started.
//  6. Launch handler subprocess.
//  7. Wait for <-watcher.Done().
//  8. sess.Wait to reap; read Outcome.
//  9. Emit run_completed.
//
// 10. CloseBead on success or ReopenBead on failure.
func runWorkLoop(ctx context.Context, deps workLoopDeps) error {
	for {
		// Check for cancellation first so we don't spin after ctx is done.
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		// Step 1: poll the ledger for ready beads.
		readyIDs, err := deps.brAdapter.Ready(ctx)
		if err != nil {
			// Treat poll errors as transient: log and backoff.
			if ctx.Err() != nil {
				return nil
			}
			// Non-fatal: wait and retry.
			if sleepErr := workloopSleep(ctx, workloopPollInterval); sleepErr != nil {
				return nil
			}
			continue
		}

		// Step 2: nothing ready — sleep and retry.
		if len(readyIDs) == 0 {
			if sleepErr := workloopSleep(ctx, workloopPollInterval); sleepErr != nil {
				return nil
			}
			continue
		}

		// Step 3: pick first ready bead, generate RunID, claim it.
		beadID := readyIDs[0]

		runUUID, uuidErr := uuid.NewV7()
		if uuidErr != nil {
			// UUID generation failure is fatal — system entropy problem.
			return fmt.Errorf("daemon: workloop: generate RunID: %w", uuidErr)
		}
		runID := core.RunID(runUUID)

		claimTID, tidErr := deps.tidGen.Next()
		if tidErr != nil {
			return fmt.Errorf("daemon: workloop: generate claim TransitionID: %w", tidErr)
		}

		claimErr := deps.brAdapter.ClaimBead(ctx, deps.intentLogDir, deps.brTimeoutCfg, runID, claimTID, beadID)
		if claimErr != nil {
			// Claim conflict or transient error — try next poll cycle.
			if ctx.Err() != nil {
				return nil
			}
			if sleepErr := workloopSleep(ctx, workloopPollInterval); sleepErr != nil {
				return nil
			}
			continue
		}

		// Step 4: create the git worktree.
		//
		// Resolve HEAD as the parent commit to avoid racing with operator activity
		// in the main worktree per workspace-model.md §4.1 WM-003.
		headSHA, headErr := resolveHEAD(ctx, deps.projectDir)
		if headErr != nil {
			// Worktree creation failed — reopen the bead so it can be retried.
			reopenTID, _ := deps.tidGen.Next()
			_ = deps.brAdapter.ReopenBead(ctx, deps.intentLogDir, deps.brTimeoutCfg, runID, reopenTID, beadID,
				fmt.Sprintf("resolve HEAD failed: %v", headErr))
			if ctx.Err() != nil {
				return nil
			}
			if sleepErr := workloopSleep(ctx, workloopPollInterval); sleepErr != nil {
				return nil
			}
			continue
		}

		wtErr := workspace.CreateWorktree(ctx, deps.projectDir, runID.String(), headSHA, workspace.NoWorktreeRootOverride())
		if wtErr != nil {
			reopenTID, _ := deps.tidGen.Next()
			_ = deps.brAdapter.ReopenBead(ctx, deps.intentLogDir, deps.brTimeoutCfg, runID, reopenTID, beadID,
				fmt.Sprintf("create worktree failed: %v", wtErr))
			if ctx.Err() != nil {
				return nil
			}
			if sleepErr := workloopSleep(ctx, workloopPollInterval); sleepErr != nil {
				return nil
			}
			continue
		}

		wtPath := workspace.WorktreePath(deps.projectDir, runID.String(), workspace.NoWorktreeRootOverride())

		// Step 5: emit run_started.
		emitRunStarted(ctx, deps.bus, runID, beadID, wtPath)

		// Step 6: launch the handler subprocess.
		spec := handler.LaunchSpec{
			Binary:  deps.handlerBinary,
			Args:    deps.handlerArgs,
			Env:     deps.handlerEnv,
			WorkDir: wtPath,
			Role:    "implementer",
		}
		sess, watcher, launchErr := deps.h.Launch(ctx, spec)
		if launchErr != nil {
			// Launch failure — reopen the bead.
			reopenTID, _ := deps.tidGen.Next()
			_ = deps.brAdapter.ReopenBead(ctx, deps.intentLogDir, deps.brTimeoutCfg, runID, reopenTID, beadID,
				fmt.Sprintf("launch error: %v", launchErr))
			emitRunCompleted(ctx, deps.bus, runID, false, fmt.Sprintf("launch error: %v", launchErr))
			// Clean up the worktree even on launch failure (hk-fgdgz).
			removeWorktree(ctx, deps.projectDir, wtPath)
			if ctx.Err() != nil {
				return nil
			}
			if sleepErr := workloopSleep(ctx, workloopPollInterval); sleepErr != nil {
				return nil
			}
			continue
		}

		// Step 7: await watcher completion.
		<-watcher.Done()

		// Step 8: reap the subprocess.
		_ = sess.Wait(ctx)
		outcome := sess.Outcome()

		// Steps 9 & 10: emit run_completed, close or reopen the bead.
		//
		// Failure condition (hk-9cob3): a watcher error that is NOT a
		// context-cancellation (ErrCanceled) means the watcher fired agent_failed
		// due to a protocol violation (malformed NDJSON, line-too-long, panic,
		// I/O error). In that case the run is treated as failed even when the
		// subprocess exits 0, because the work product may be incomplete or
		// corrupt.
		watcherErr := watcher.Err()
		watcherFailed := watcherErr != nil && !errors.Is(watcherErr, handlercontract.ErrCanceled)

		transitionTID, _ := deps.tidGen.Next()
		if outcome.ExitCode == 0 && !watcherFailed {
			_ = deps.brAdapter.CloseBead(ctx, deps.intentLogDir, deps.brTimeoutCfg, runID, transitionTID, beadID)
			emitRunCompleted(ctx, deps.bus, runID, true, "auto-close: exit=0")
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

		// Step 11 (hk-fgdgz): clean up the git worktree after every dispatch
		// (success or failure). Non-fatal: failure to prune is logged but does not
		// abort the loop.
		removeWorktree(ctx, deps.projectDir, wtPath)

		// Check for cancellation before next iteration.
		if ctx.Err() != nil {
			return nil
		}
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
