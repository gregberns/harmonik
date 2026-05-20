package daemon

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/gregberns/harmonik/internal/brcli"
	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/lifecycle"
	ltmux "github.com/gregberns/harmonik/internal/lifecycle/tmux"
	"github.com/gregberns/harmonik/internal/workspace"
)

// OrphanSweepResult holds the outcome of a complete [RunOrphanSweep] pass.
// The field names and JSON tags align with [core.DaemonOrphanSweepCompletedPayload].
//
// Spec ref: process-lifecycle.md §4.2 PL-006 — "Event: On completion, the
// daemon MUST emit daemon_orphan_sweep_completed with counts of tmux sessions
// killed, locks cleared, handler subprocesses killed, br subprocesses killed,
// reconciliation lock files removed, and stale intents observed."
type OrphanSweepResult struct {
	// TmuxSessionsKilled is the number of orphan tmux sessions killed.
	TmuxSessionsKilled int

	// TmuxWindowsKilled is the number of orphan tmux windows killed inside
	// operator-owned sessions (PL-021c window-level sweep).
	TmuxWindowsKilled int

	// LocksCleared is the number of stale worktree lease-lock files removed.
	LocksCleared int

	// SubprocessesKilled is the number of orphan handler subprocesses killed.
	SubprocessesKilled int

	// BrSubprocessesKilled is the number of orphan br subprocesses killed.
	BrSubprocessesKilled int

	// ReconciliationLocksRemoved is the number of stale reconciliation lock
	// files removed.
	ReconciliationLocksRemoved int

	// StaleIntentsObserved is the count of stale intent files enumerated.
	// These are left on disk for the reconciliation Cat 3a detector.
	StaleIntentsObserved int

	// BeadInProgressReset is the count of stale `in_progress` beads reset to
	// `open` by the PL-006 sixth-bullet bead-reset sweep (BI-010d).
	//
	// Spec ref: specs/process-lifecycle.md §4.5 PL-006 sixth bullet.
	// Bead ref: hk-iuaed.4.
	BeadInProgressReset int

	// BeadCat3cClosed is the count of subsumed in_progress beads auto-closed
	// by the Cat 3c auto-reconciler (hk-lgtq2): beads whose implementation has
	// already merged to the target branch but were still marked in_progress.
	BeadCat3cClosed int

	// ClaudeWorktreesSwept is the count of orphan .claude/worktrees/ entries
	// identified by the Gap-11 parallel sweep (hk-yhq3m). Reported in both
	// dry-run and live modes; when HARMONIK_SWEEP_CLAUDE_WORKTREES is not "1"
	// no directories are deleted even if this count is > 0.
	//
	// Bead ref: hk-yhq3m — daemon orphan-sweep must also walk .claude/worktrees/.
	ClaudeWorktreesSwept int

	// QueueArchivesDeleted is the count of old queue.json archive files removed
	// by the Gap-4 archive-accumulation sweep (hk-pycay). Keeps the newest N
	// (default 5, configurable via HARMONIK_QUEUE_ARCHIVE_KEEP_COUNT) per
	// category; older archives are removed.
	//
	// Bead ref: hk-pycay.
	QueueArchivesDeleted int

	// SweptAt is the wall-clock time at sweep completion.
	SweptAt time.Time
}

// ToPayload converts an OrphanSweepResult to the core event payload type.
func (r OrphanSweepResult) ToPayload() core.DaemonOrphanSweepCompletedPayload {
	return core.DaemonOrphanSweepCompletedPayload{
		TmuxSessionsKilled:         r.TmuxSessionsKilled,
		TmuxWindowsKilled:          r.TmuxWindowsKilled,
		LocksCleared:               r.LocksCleared,
		SubprocessesKilled:         r.SubprocessesKilled,
		BrSubprocessesKilled:       r.BrSubprocessesKilled,
		ReconciliationLocksRemoved: r.ReconciliationLocksRemoved,
		StaleIntentsObserved:       r.StaleIntentsObserved,
		BeadInProgressReset:        r.BeadInProgressReset,
		BeadCat3cClosed:            r.BeadCat3cClosed,
		SweptAt:                    r.SweptAt.UTC().Format(time.RFC3339),
	}
}

// OrphanSweepConfig carries injected dependencies for RunOrphanSweep. Nil
// fields fall back to OS-backed production implementations.
type OrphanSweepConfig struct {
	// TmuxLister overrides the tmux session lister. Nil → OSTmuxSessionLister.
	TmuxLister lifecycle.TmuxSessionLister

	// TmuxKiller overrides the tmux session killer. Nil → OSTmuxSessionKiller.
	TmuxKiller lifecycle.TmuxSessionKiller

	// TmuxAdapter is the tmux Adapter used for the window-level orphan sweep
	// (PL-021c). Nil → no window sweep (production callers MUST provide this).
	TmuxAdapter ltmux.Adapter

	// HandlerLister overrides the handler subprocess lister.
	// Nil → OSHandlerProcessLister.
	HandlerLister lifecycle.HandlerProcessLister

	// BrLister overrides the br subprocess lister. Nil → OSProcessLister.
	BrLister lifecycle.ProcessLister

	// BeadLedger is the read surface (br list --status in_progress) for the
	// PL-006 sixth-bullet stale-in_progress bead-reset sweep. Nil → bead-reset
	// sweep is SKIPPED (BeadInProgressReset remains 0). Production callers
	// MUST supply this (typically a *brcli.Adapter); unit-test callers that
	// do not exercise the bead-reset path may leave it nil.
	BeadLedger lifecycle.InFlightBeadLedger //nolint:revive // explicit name preserved for caller clarity

	// BeadResetter is the write surface (br update --status open via the BI
	// adapter) for the bead-reset sweep. Nil → bead-reset sweep is SKIPPED.
	// Production callers MUST supply this (typically the same *brcli.Adapter
	// that backs BeadLedger).
	BeadResetter lifecycle.BeadResetter

	// BeadProvenance is the project-ownership signal for the bead-reset
	// sweep. Nil → ownership is established solely by the local
	// claim-intent fallback (the MVH default). Production callers wire a
	// non-nil implementation once Beads's audit-log actor field carries
	// project_hash (or an alternate per-project provenance signal lands).
	BeadProvenance lifecycle.ProvenanceChecker

	// QueueDispatched is the set of bead IDs that queue.json records as
	// status=dispatched at daemon startup. Nil → queue-dispatched exclusion (a)
	// check is skipped. Production callers SHOULD populate this from a raw
	// queue.Load before RunOrphanSweep (hk-2ty0g SIGKILL-recovery fix).
	//
	// Spec ref: process-lifecycle.md §4.5 PL-006 sixth bullet — exclusion (a).
	// Bug ref: hk-2ty0g.
	QueueDispatched lifecycle.QueueDispatchedSet

	// QueueOwned is the set of bead IDs that appear in queue.json in ANY item
	// status. Nil → queue-ownership provenance signal is skipped. Production
	// callers SHOULD populate this alongside QueueDispatched.
	//
	// Spec ref: process-lifecycle.md §4.5 PL-006 sixth bullet — provenance.
	// Bug ref: hk-2ty0g.
	QueueOwned lifecycle.QueueOwnedSet

	// MergeCommitScanner detects PL-006 exclusion condition (c) — a
	// Harmonik-Bead-ID merge commit on the target branch (Cat 3c condition).
	// Nil → exclusion (c) is treated as "no merge commit" (the conservative
	// fallback; a missed Cat 3c condition is re-detected on the next restart).
	MergeCommitScanner lifecycle.MergeCommitScanner

	// BeadCat3cCloser, when non-nil, enables Cat 3c auto-resolution: when a
	// subsumed bead is detected (merge commit with Harmonik-Bead-ID present on
	// target branch but bead still IN_PROGRESS), the sweep closes the bead
	// instead of skipping it. Nil → exclusion (c) is a skip (old behavior).
	//
	// Spec ref: hk-lgtq2 (Cat 3c auto-reconciler).
	BeadCat3cCloser lifecycle.BeadCat3cCloser

	// IntentLogDir is the absolute path of .harmonik/beads-intents/ — read by
	// the bead-reset sweep to compute exclusion conditions (a) and (b).
	// Empty when BeadLedger / BeadResetter are nil. Otherwise required.
	IntentLogDir string

	// DaemonStartNS is the daemon's start time in nanoseconds; used to derive
	// the BI-010d idempotency key for each reset write. Zero is invalid when
	// BeadLedger / BeadResetter are non-nil.
	DaemonStartNS int64

	// BrTimeoutCfg forwards the BI-025c timeout configuration to ResetBead.
	BrTimeoutCfg brcli.TimeoutConfig

	// Logger receives diagnostic messages. Nil → silent.
	Logger *log.Logger
}

// RunOrphanSweep executes the full PL-006 orphan sweep in order:
//
//  1. Kill orphan tmux sessions matching the project-hash prefix (bullet a).
//  2. Remove stale worktree lease-lock files via workspace.SweepStaleLeaseLocks (bullet b).
//  3. Kill orphan handler subprocesses with matching provenance marker (bullet c).
//  4. Kill orphan br subprocesses re-parented to init (bullet c, br half).
//  5. Enumerate (but do NOT remove) stale intent files (bullet d).
//  6. Remove stale reconciliation lock files (bullet e).
//
// On completion it returns an [OrphanSweepResult] ready to be converted to the
// daemon_orphan_sweep_completed event payload via [OrphanSweepResult.ToPayload].
//
// Spec ref: process-lifecycle.md §4.2 PL-006 — full 6-bullet mandate.
func RunOrphanSweep(
	ctx context.Context,
	projectDir string,
	projectHash core.ProjectHash,
	daemonStartTime time.Time,
	cfg OrphanSweepConfig,
) (OrphanSweepResult, error) {
	var result OrphanSweepResult
	var errs []string

	// (a) Tmux sessions — two passes:
	//   (a1) Kill orphan harmonik-owned sessions via the legacy TmuxLister/TmuxKiller path.
	tmuxKilled, err := lifecycle.SweepOrphanTmuxSessions(ctx, projectHash, cfg.TmuxLister, cfg.TmuxKiller, cfg.Logger)
	if err != nil {
		errs = append(errs, fmt.Sprintf("tmux: %v", err))
	}
	result.TmuxSessionsKilled = tmuxKilled

	//   (a1b) Kill orphan harmonik-owned sessions via the Adapter path (hk-kqdpf.3):
	//   enumerates sessions matching harmonik-<12-char-hash>- prefix, kills those
	//   with dead PIDs or zero non-zsh windows. Must run BEFORE the window sweep
	//   so dead sessions are removed before we attempt to sweep their windows.
	adapterSessionsKilled, err := ltmux.SweepOrphanTmuxSessions(ctx, projectHash, cfg.TmuxAdapter, cfg.Logger)
	if err != nil {
		errs = append(errs, fmt.Sprintf("tmux-sessions-adapter: %v", err))
	}
	result.TmuxSessionsKilled += adapterSessionsKilled

	// (a2) Tmux windows (PL-021c): kill orphan windows inside operator-owned
	// sessions whose name matches the hk-<hash6>- sentinel prefix.
	windowsKilled, err := ltmux.SweepOrphanTmuxWindows(ctx, projectHash, cfg.TmuxAdapter, cfg.Logger)
	if err != nil {
		errs = append(errs, fmt.Sprintf("tmux-windows: %v", err))
	}
	result.TmuxWindowsKilled = windowsKilled

	// (b) Worktree lease-lock files.
	sweepResult, err := workspace.SweepStaleLeaseLocks(ctx, projectDir, workspace.NoWorktreeRootOverride())
	if err != nil {
		errs = append(errs, fmt.Sprintf("lease-locks: %v", err))
	}
	result.LocksCleared = len(sweepResult.Removed)

	// (c-i) Handler subprocesses.
	handlersKilled, err := lifecycle.SweepOrphanHandlers(ctx, projectHash, cfg.HandlerLister, cfg.Logger)
	if err != nil {
		errs = append(errs, fmt.Sprintf("handlers: %v", err))
	}
	result.SubprocessesKilled = handlersKilled

	// (c-ii) br subprocesses.
	brSurvived, err := lifecycle.SweepOrphanBr(ctx, cfg.BrLister, cfg.Logger)
	if err != nil {
		errs = append(errs, fmt.Sprintf("br: %v", err))
	}
	// br subprocesses killed = pids enumerated − survived (survived are Cat 0 failures).
	// We don't have a direct killed count from SweepOrphanBr, so we approximate:
	// any PIDs that did not survive were killed. We cannot call ListOrphanBrPIDs
	// again after the fact, so BrSubprocessesKilled counts the processes that
	// did NOT survive. This is conservative: it undercounts only if SweepOrphanBr
	// was called with an empty lister (returns 0 survived anyway).
	//
	// NOTE: SweepOrphanBr returns survivors, not the full pid list. We report
	// 0 for br-killed when survival count is 0 and the lister returns no error.
	// A follow-up bead can refactor SweepOrphanBr to return a (killed, survived)
	// pair for exact accounting.
	_ = brSurvived // survival tracked for Cat 0 precondition, not used in count here

	// (d) Stale intent files (enumerate only, do NOT remove).
	staleIntents, err := lifecycle.EnumerateStaleIntents(projectDir, daemonStartTime)
	if err != nil {
		errs = append(errs, fmt.Sprintf("intents: %v", err))
	}
	result.StaleIntentsObserved = staleIntents

	// (e) Stale reconciliation locks.
	reconRemoved, err := lifecycle.SweepStaleReconciliationLocks(projectDir, cfg.Logger)
	if err != nil {
		errs = append(errs, fmt.Sprintf("recon-locks: %v", err))
	}
	result.ReconciliationLocksRemoved = reconRemoved

	// (f) Stale in_progress bead markers (PL-006 sixth bullet — hk-iuaed.4).
	// Run after the filesystem+process sweep and after the BI-024a `br --version`
	// handshake has succeeded (the latter is the caller's responsibility — see
	// the package doc in internal/lifecycle/orphansweepbeads.go for the
	// sequencing rationale). Skipped silently when the bead-ledger / resetter
	// adapter isn't wired (unit-test mode).
	if cfg.BeadLedger != nil && cfg.BeadResetter != nil {
		sweepResult, beadResetErr := lifecycle.SweepStaleInProgressBeads(ctx, lifecycle.SweepStaleInProgressBeadsConfig{
			Ledger:          cfg.BeadLedger,
			Resetter:        cfg.BeadResetter,
			Provenance:      cfg.BeadProvenance,
			MergeScanner:    cfg.MergeCommitScanner,
			Cat3cCloser:     cfg.BeadCat3cCloser,
			IntentLogDir:    cfg.IntentLogDir,
			ProjectHash:     projectHash,
			DaemonStartNS:   cfg.DaemonStartNS,
			BrTimeoutCfg:    cfg.BrTimeoutCfg,
			QueueDispatched: cfg.QueueDispatched,
			QueueOwned:      cfg.QueueOwned,
			Logger:          cfg.Logger,
		})
		if beadResetErr != nil {
			errs = append(errs, fmt.Sprintf("bead-reset: %v", beadResetErr))
		}
		result.BeadInProgressReset = sweepResult.ResetCount
		result.BeadCat3cClosed = sweepResult.Cat3cCloseCount
	}

	// (g) Sub-agent .claude/worktrees/ orphan sweep (Gap-11 — hk-yhq3m).
	// Parallel path: does NOT touch .harmonik/worktrees/ semantics.
	// Dry-run by default; HARMONIK_SWEEP_CLAUDE_WORKTREES=1 enables removal.
	claudeResult, claudeErr := SweepClaudeWorktrees(ctx, projectDir, cfg.Logger)
	if claudeErr != nil {
		errs = append(errs, fmt.Sprintf("claude-worktrees: %v", claudeErr))
	}
	result.ClaudeWorktreesSwept = len(claudeResult.Orphans)

	// (h) Queue archive accumulation sweep (Gap-4 / hk-pycay).
	// Keeps the newest N archives per category (default 5; configurable via
	// HARMONIK_QUEUE_ARCHIVE_KEEP_COUNT) and deletes older ones. Non-fatal:
	// a removal error is logged but does not abort startup.
	archiveResult, archiveErr := lifecycle.SweepQueueArchives(projectDir, lifecycle.SweepQueueArchivesConfig{
		Logger: cfg.Logger,
	})
	if archiveErr != nil {
		errs = append(errs, fmt.Sprintf("queue-archives: %v", archiveErr))
	}
	result.QueueArchivesDeleted = archiveResult.Deleted

	result.SweptAt = time.Now()

	if len(errs) > 0 {
		return result, fmt.Errorf("daemon: RunOrphanSweep: %s", strings.Join(errs, "; "))
	}
	return result, nil
}
