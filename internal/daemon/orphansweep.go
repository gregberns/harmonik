package daemon

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/gregberns/harmonik/internal/brcli"
	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/crew"
	"github.com/gregberns/harmonik/internal/dispatch"
	"github.com/gregberns/harmonik/internal/lifecycle"
	ltmux "github.com/gregberns/harmonik/internal/lifecycle/tmux"
	runpkg "github.com/gregberns/harmonik/internal/run"
	"github.com/gregberns/harmonik/internal/workspace"
)

// EnvHarmonikWorktreeMaxAgeDays is the environment variable that sets the
// age threshold (in days) for .harmonik/worktrees/ directories with no
// lease-lock. Directories older than this threshold are removed by the (b3)
// age-prune step. Default: [DefaultHarmonikWorktreeMaxAgeDays].
const EnvHarmonikWorktreeMaxAgeDays = "HARMONIK_WORKTREE_MAX_AGE_DAYS"

// DefaultHarmonikWorktreeMaxAgeDays is the default age threshold for stale
// .harmonik/worktrees/ directories with no lease-lock. 7 days is conservative
// — any run-worktree without a lease-lock that has been sitting for a week is
// almost certainly an orphan (active runs complete in hours).
const DefaultHarmonikWorktreeMaxAgeDays = 7

func harmonikWorktreeMaxAge() time.Duration {
	if v := os.Getenv(EnvHarmonikWorktreeMaxAgeDays); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return time.Duration(n) * 24 * time.Hour
		}
	}
	return DefaultHarmonikWorktreeMaxAgeDays * 24 * time.Hour
}

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

	// Cat3bRunIDs holds target_run_ids from stale reconciliation lock files that
	// did NOT carry "Harmonik-Verdict-Executed: true". These runs must be routed
	// through Cat 3b (verdict-emitted-but-unexecuted) on the next reconciliation
	// pass per specs/reconciliation/spec.md §4.1 RC-002b.
	Cat3bRunIDs []string

	// StaleIntentsObserved is the count of stale intent files that were
	// retained on disk for the reconciliation Cat 3a detector (i.e. the bead
	// has NOT yet reached its IntendedPostState). Does not include files that
	// were GC'd by GCRetiredIntents (see IntentsGCd). When IntentGCLedger is
	// nil (no ledger configured), this is the raw count of all stale files.
	StaleIntentsObserved int

	// IntentsGCd is the count of stale intent files removed during this sweep:
	// step-3 GC (bead already reached IntendedPostState; op landed in a prior run)
	// + step-4 re-drive (bead was still at the pre-state; br write re-issued and
	// the file deleted on success per BI-031 step 6). Zero when IntentGCLedger is nil.
	//
	// Bead ref: hk-cizvu — stale_intents_observed GC.
	// Bead ref: hk-aev8t — step-4 re-drive.
	IntentsGCd int

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

	// WorktreeDirsRemoved is the count of stale .harmonik/worktrees/ directories
	// removed by [workspace.RemoveStaleWorktrees] during the orphan sweep.
	// These are worktrees whose lease-lock recorded a dead PID (identified by
	// [workspace.SweepStaleLeaseLocks]) that were then fully deregistered from
	// git and deleted from disk via `git worktree remove --force --force`.
	// `git worktree prune` alone cannot remove these because they remain
	// registered in git until explicitly removed (hk-ldzp).
	//
	// Bead ref: hk-ldzp — daemon worktree/disk GC.
	WorktreeDirsRemoved int

	// ClaudeWorktreesSwept is the count of orphan .claude/worktrees/ entries
	// identified by the Gap-11 parallel sweep (hk-yhq3m). Reported in both
	// dry-run and live modes; when HARMONIK_SWEEP_CLAUDE_WORKTREES is not "1"
	// no directories are deleted even if this count is > 0.
	//
	// Bead ref: hk-yhq3m — daemon orphan-sweep must also walk .claude/worktrees/.
	ClaudeWorktreesSwept int

	// QueueArchivesObserved is the count of failed-queue archive files found on
	// disk by [lifecycle.ObserveQueueArchives]. The sweep only LOOKS. It has
	// never removed an archive and must not start: deciding which record of a
	// failed run still has value is a judgment, and the daemon does not make
	// judgments (CHARTER.md §5; specs/architecture.md §4.2 AR-006).
	QueueArchivesObserved int

	// QueueArchiveBytes is the total size on disk of the observed archives.
	// Reported so an operator can see the cost of keeping them.
	QueueArchiveBytes int64

	// QueueArchivesOverRetention is the count of archives that exceed the
	// operator's per-queue retention number. It is 0 whenever no operator has
	// set HARMONIK_QUEUE_ARCHIVE_KEEP_COUNT — there is no default, so "over
	// retention" is meaningless until somebody chooses a number. These are
	// candidates for an operator or agent to remove. The daemon removes none
	// of them.
	QueueArchivesOverRetention int

	// CoordinatorSessionsSkipped is the count of coordinator (flywheel) tmux
	// sessions that were SKIPPED by the PL-006d sentinel exclusion: the session
	// matched the project-hash prefix AND the supervisor sentinel file is present
	// AND the supervisor PID is live.
	//
	// Spec ref: process-lifecycle.md §4.2 PL-006d — "sentinel-present+PID-live →
	// SKIP with structured-log orphan_sweep_skipped_coordinator_session."
	// Bead ref: hk-9eury.
	CoordinatorSessionsSkipped int

	// CoordinatorSessionsReaped is the count of coordinator (flywheel) tmux
	// sessions force-killed at boot because their owning supervisor PID was
	// confirmed DEAD (sentinel present but kill(pid,0) → ESRCH).  These sessions
	// are exempt from the generic orphan-classification (sessionIsOrphaned can
	// return false when the supervisor's re-parented children keep the pane
	// "alive"), so a dead supervisor would otherwise leak its flywheel session
	// forever.  hk-9vp51.
	CoordinatorSessionsReaped int

	// CrewSessionsSkipped is the count of live crew tmux sessions excluded
	// from the orphan-kill pass because the crew registry record exists AND
	// the session's first-pane PID is alive (PL-006d mechanism iii).
	//
	// Spec ref: process-lifecycle.md §4.2 PL-006d mechanism (iii).
	// Bead ref: hk-qp3.
	CrewSessionsSkipped int

	// CaptainSessionsSkipped is the count of live captain tmux sessions
	// excluded from the orphan-kill pass because captain.sentinel is present
	// AND the captain PID is alive (PL-006d mechanism ii).
	//
	// Spec ref: process-lifecycle.md §4.2 PL-006d mechanism (ii).
	// Bead ref: hk-qp3.
	CaptainSessionsSkipped int

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
		IntentsGCd:                 r.IntentsGCd,
		BeadInProgressReset:        r.BeadInProgressReset,
		BeadCat3cClosed:            r.BeadCat3cClosed,
		CoordinatorSessionsSkipped: r.CoordinatorSessionsSkipped,
		CoordinatorSessionsReaped:  r.CoordinatorSessionsReaped,
		CrewSessionsSkipped:        r.CrewSessionsSkipped,
		CaptainSessionsSkipped:     r.CaptainSessionsSkipped,
		QueueArchivesObserved:      r.QueueArchivesObserved,
		QueueArchiveBytes:          r.QueueArchiveBytes,
		QueueArchivesOverRetention: r.QueueArchivesOverRetention,
		SweptAt:                    r.SweptAt.UTC().Format(time.RFC3339),
	}
}

// OrphanSweepConfig carries injected dependencies for RunOrphanSweep. Nil
// fields fall back to OS-backed production implementations.
type OrphanSweepConfig struct {
	// DispatchOwnership contains exact identities held by dispatch replay. The
	// generic sweep must not reset or remove these facts.
	DispatchOwnership DispatchReplayOwnership

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
	BeadLedger lifecycle.InFlightBeadLedger

	// BeadResetter is the write surface (br update --status open via the BI
	// adapter) for the bead-reset sweep. Nil → bead-reset sweep is SKIPPED.
	// Production callers MUST supply this (typically the same *brcli.Adapter
	// that backs BeadLedger).
	BeadResetter lifecycle.BeadResetter

	// BeadProvenance is the project-ownership signal for the bead-reset
	// sweep. Nil → ownership is established solely by the local
	// claim-intent fallback (the default). Production callers wire a
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

	// IntentGCLedger, when non-nil, enables GCRetiredIntentsWithRedrive: stale
	// intent files whose bead has already reached IntendedPostState are deleted
	// rather than accumulated indefinitely. When nil, the GC pass is skipped and
	// StaleIntentsObserved counts all stale files (old behavior).
	//
	// Production callers SHOULD supply this (typically the same *brcli.Adapter
	// that backs BeadLedger / BeadResetter). Unit-test callers that do not
	// exercise the GC path may leave it nil.
	//
	// Bead ref: hk-cizvu — stale_intents_observed grows unbounded without GC.
	IntentGCLedger lifecycle.IntentGCLedger

	// IntentRedriveWriter, when non-nil, enables BI-031 step-4 re-drive: for
	// stale intent files where the bead is still at the op's pre-state, the br
	// write is re-issued at startup instead of being retained for Cat 3a.
	// Production callers SHOULD supply this (typically the same *brcli.Adapter
	// that backs IntentGCLedger). Unit-test callers that do not exercise the
	// re-drive path may leave it nil.
	//
	// Spec ref: specs/beads-integration.md §4.10 BI-031 step 4.
	// Bead ref: hk-aev8t.
	IntentRedriveWriter lifecycle.IntentRedriveWriter

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

	// DaemonSpawnSession is the tmux session name the daemon spawns implementer
	// windows into (resolved by tmux.ResolveDaemonSpawnSession at boot). When
	// non-empty it is EXCLUDED from the session-level orphan sweep so the daemon
	// never kills its own spawn-target session — critical for the fix-forward
	// fallback case where the daemon EnsureSessions a fresh "harmonik-<hash>-
	// default" session that has only an idle zsh window at boot (which the generic
	// sweep would otherwise classify as orphaned and kill before the first
	// dispatch). hk-9vp51.
	DaemonSpawnSession string

	// Logger receives diagnostic messages. Nil → silent.
	Logger *log.Logger
}

// DispatchReplayOwnership is the identity set that startup replay retains
// while generic orphan cleanup runs.
type DispatchReplayOwnership struct {
	Beads     map[core.BeadID]struct{}
	Runs      map[core.RunID]struct{}
	Sessions  map[string]struct{}
	Worktrees map[core.RunID]struct{}
	Receipts  map[core.RunID]dispatch.SessionStartReceipt
}

func coordinatorSentinelDir(projectDir string) string {
	return filepath.Join(projectDir, ".harmonik", "cognition")
}

func coordinatorSentinelPath(projectDir string) string {
	return filepath.Join(coordinatorSentinelDir(projectDir), "supervisor.sentinel")
}

func coordinatorPidfilePath(projectDir string) string {
	return filepath.Join(coordinatorSentinelDir(projectDir), "supervisor.pid")
}

type probeCoordinatorSentinelResult struct {
	// Live is true when the coordinator sentinel is present AND the supervisor
	// PID is still running. The flywheel session MUST be excluded from the
	// orphan sweep in this case (PL-006d).
	Live bool

	// SentinelRemoved is true when a stale sentinel file was removed as part
	// of the probe (sentinel present but PID dead or unreadable).
	SentinelRemoved bool
}

func probeCoordinatorSentinel(projectDir string, logger *log.Logger) (probeCoordinatorSentinelResult, error) {
	sentinelPath := coordinatorSentinelPath(projectDir)
	pidfilePath := coordinatorPidfilePath(projectDir)

	if _, statErr := os.Stat(sentinelPath); os.IsNotExist(statErr) {
		return probeCoordinatorSentinelResult{}, nil
	}

	pid, readErr := readSupervisorPID(pidfilePath)
	if readErr != nil {
		if logger != nil {
			logger.Printf("daemon: probeCoordinatorSentinel: cannot read supervisor.pid (%v); removing stale sentinel", readErr)
		}
		removed := removeStaleSentinel(sentinelPath, logger)
		return probeCoordinatorSentinelResult{SentinelRemoved: removed}, nil
	}

	if err := syscall.Kill(pid, 0); err != nil {
		if err == syscall.EPERM {
			if logger != nil {
				logger.Printf("daemon: probeCoordinatorSentinel: supervisor PID %d EPERM (live); skipping flywheel session (PL-006d)", pid)
			}
			return probeCoordinatorSentinelResult{Live: true}, nil
		}
		if logger != nil {
			logger.Printf("daemon: probeCoordinatorSentinel: supervisor PID %d is dead (%v); removing stale sentinel (PL-006d)", pid, err)
		}
		removed := removeStaleSentinel(sentinelPath, logger)
		return probeCoordinatorSentinelResult{SentinelRemoved: removed}, nil
	}

	if logger != nil {
		logger.Printf("daemon: probeCoordinatorSentinel: supervisor PID %d is live; skipping flywheel session (PL-006d)", pid)
	}
	return probeCoordinatorSentinelResult{Live: true}, nil
}

func reapDeadCoordinatorSession(ctx context.Context, projectHash core.ProjectHash, adapter ltmux.Adapter, logger *log.Logger) int {
	if adapter == nil {
		return 0
	}
	flywheelSession := lifecycle.TmuxSessionName(projectHash, "flywheel")

	if !strings.HasSuffix(flywheelSession, "-flywheel") {
		if logger != nil {
			logger.Printf("daemon: reapDeadCoordinatorSession: refusing to reap %q — not a flywheel-suffixed coordinator session (hk-9vp51 self-guard)", flywheelSession)
		}
		return 0
	}

	sessions, listErr := adapter.ListSessions(ctx)
	if listErr != nil {
		if logger != nil {
			logger.Printf("daemon: reapDeadCoordinatorSession: ListSessions error (skipping reap): %v", listErr)
		}
		return 0
	}
	present := false
	for _, s := range sessions {
		if s == flywheelSession {
			present = true
			break
		}
	}
	if !present {
		return 0
	}

	if logger != nil {
		logger.Printf("daemon: reapDeadCoordinatorSession: supervisor dead — reaping leaked coordinator session %q (hk-9vp51)", flywheelSession)
	}
	if killErr := adapter.KillSession(ctx, flywheelSession); killErr != nil {
		if logger != nil {
			logger.Printf("daemon: reapDeadCoordinatorSession: kill-session %q error (proceeding): %v", flywheelSession, killErr)
		}
	}
	return 1
}

func readSupervisorPID(path string) (int, error) {
	//nolint:gosec // G304: path is constructed from operator-controlled projectDir
	f, err := os.Open(path)
	if err != nil {
		return 0, fmt.Errorf("open: %w", err)
	}
	defer func() { _ = f.Close() }() //nolint:errcheck

	scanner := bufio.NewScanner(f)
	if scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		pid, parseErr := strconv.Atoi(line)
		if parseErr != nil {
			return 0, fmt.Errorf("parse pid %q: %w", line, parseErr)
		}
		return pid, nil
	}
	return 0, fmt.Errorf("supervisor.pid is empty")
}

func removeStaleSentinel(sentinelPath string, logger *log.Logger) bool {
	if err := os.Remove(sentinelPath); err != nil && !os.IsNotExist(err) {
		if logger != nil {
			logger.Printf("daemon: removeStaleSentinel: Remove %q: %v (non-fatal)", sentinelPath, err)
		}
		return false
	}
	dir := filepath.Dir(sentinelPath)
	//nolint:gosec // G304: dir is derived from operator-controlled projectDir
	if dirFd, openErr := os.Open(dir); openErr == nil {
		_ = dirFd.Sync()  //nolint:errcheck
		_ = dirFd.Close() //nolint:errcheck
	}
	return true
}

func captainSentinelPath(projectDir string) string {
	return filepath.Join(coordinatorSentinelDir(projectDir), "captain.sentinel")
}

func captainPidfilePath(projectDir string) string {
	return filepath.Join(coordinatorSentinelDir(projectDir), "captain.pid")
}

func probeCaptainSentinel(
	ctx context.Context,
	projectDir string,
	projectHash core.ProjectHash,
	adapter ltmux.Adapter,
	logger *log.Logger,
	sessionSnapshot map[string]struct{},
) bool {
	sentinelPath := captainSentinelPath(projectDir)

	if _, statErr := os.Stat(sentinelPath); os.IsNotExist(statErr) {
		return false
	}

	sessionName := lifecycle.TmuxSessionName(projectHash, "captain")
	if adapter != nil {
		if _, present := sessionSnapshot[sessionName]; present {
			pid, pidErr := adapter.WindowPanePID(ctx, ltmux.WindowHandle(sessionName+":"))
			if pidErr == nil && pid > 0 && pidIsLive(pid) {
				if logger != nil {
					logger.Printf("daemon: probeCaptainSentinel: captain session %q live (pane PID %d); skipping (PL-006d ii)", sessionName, pid)
				}
				return true
			}
		}
	}

	if pid, readErr := readSupervisorPID(captainPidfilePath(projectDir)); readErr == nil && pidIsLive(pid) {
		if logger != nil {
			logger.Printf("daemon: probeCaptainSentinel: captain PID %d live; skipping (PL-006d ii)", pid)
		}
		return true
	}

	if logger != nil {
		logger.Printf("daemon: probeCaptainSentinel: captain session %q and recorded pid both dead/absent; removing stale sentinel", sessionName)
	}
	removeStaleSentinel(sentinelPath, logger)
	return false
}

func pidIsLive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || err == syscall.EPERM
}

func probeRunRegistrySessions(
	ctx context.Context,
	projectDir string,
	adapter ltmux.Adapter,
	logger *log.Logger,
	sessionSnapshot map[string]struct{},
	excludeSessions map[string]struct{},
) (liveRunIDs map[string]struct{}, registryRead bool) {
	liveRunIDs = map[string]struct{}{}
	if adapter == nil {
		return liveRunIDs, true
	}
	registry, err := runpkg.ScanRegistry(projectDir)
	if err != nil {
		msg := fmt.Sprintf("daemon: probeRunRegistrySessions: THE RUN REGISTRY COULD NOT BE READ (%v). "+
			"This boot cannot tell a live run from an orphan, so the sweep will kill no agent session "+
			"and remove no worktree directory. The narrower steps still run: the coordinator reap "+
			"above this point, which reaps the flywheel session only when the supervisor is dead or "+
			"its sentinel is gone, and below it the window sweep, the lease-lock file sweep, the "+
			"subprocess sweeps, the intent GC and the stale-bead passes. One of those is not safe by "+
			"design. The handler sweep kills every parentless process that carries this project hash "+
			"and asks nothing about live runs, so on a platform where it can read process "+
			"environments it can still kill a live agent. Repair or move the unreadable file under "+
			".harmonik/runs/ and restart the daemon to let the full sweep run again.", err)
		if logger != nil {
			logger.Print(msg)
		} else {
			log.Print(msg)
		}
		return liveRunIDs, false
	}

	probe := func(runID, beadID, sessionName string) {
		if sessionName == "" {
			return
		}
		if _, present := sessionSnapshot[sessionName]; !present {
			return
		}
		pid, pidErr := adapter.WindowPanePID(ctx, ltmux.WindowHandle(sessionName+":"))
		if pidErr != nil || pid <= 0 || !pidIsLive(pid) {
			return
		}
		excludeSessions[sessionName] = struct{}{}
		liveRunIDs[runID] = struct{}{}
		if logger != nil {
			logger.Printf("daemon: probeRunRegistrySessions: bead %s is still being worked in session %q (PID %d); not sweeping it",
				beadID, sessionName, pid)
		}
	}
	for _, rec := range registry.Legacy {
		probe(rec.RunID, rec.BeadID, rec.SessionName)
	}
	for _, rec := range registry.Dispatch {
		if rec.SessionName == "" {
			continue
		}
		probe(rec.RunID.String(), string(rec.BeadID), rec.SessionName)
	}
	return liveRunIDs, true
}

func logRegistryStandDown(logger *log.Logger, pass string) {
	const format = "daemon: RunOrphanSweep: SKIPPING THE %s — the run registry is unreadable, so this " +
		"boot cannot prove any run is dead. Orphans leak until the next boot; that is recoverable and " +
		"deleting a live agent's session or worktree is not."
	if logger != nil {
		logger.Printf(format, pass)
		return
	}
	log.Printf(format, pass)
}

func worktreesNotHeldByALiveRun(paths []string, liveRunIDs map[string]struct{}, logger *log.Logger) []string {
	if len(liveRunIDs) == 0 {
		return paths
	}
	kept := make([]string, 0, len(paths))
	for _, p := range paths {
		if _, live := liveRunIDs[filepath.Base(p)]; live {
			if logger != nil {
				logger.Printf("daemon: RunOrphanSweep: worktree %q belongs to a run whose agent is still working; not removing it", p)
			}
			continue
		}
		kept = append(kept, p)
	}
	return kept
}

func probeCrewRegistrySessions(
	ctx context.Context,
	projectDir string,
	projectHash core.ProjectHash,
	adapter ltmux.Adapter,
	logger *log.Logger,
	sessionSnapshot map[string]struct{},
	excludeSessions map[string]struct{},
) int {
	if adapter == nil {
		return 0
	}

	records, err := crew.List(projectDir)
	if err != nil {
		if logger != nil {
			logger.Printf("daemon: probeCrewRegistrySessions: crew.List error (%v); falling back to live-session scan", err)
		}
	}

	skipped := 0
	for _, rec := range records {
		sessionName := lifecycle.TmuxSessionName(projectHash, "crew-"+rec.Name)

		if _, present := sessionSnapshot[sessionName]; !present {
			if logger != nil {
				logger.Printf("daemon: probeCrewRegistrySessions: crew %q session %q absent from snapshot (launch-in-flight?); skipping", rec.Name, sessionName)
			}
			continue
		}

		firstHandle := ltmux.WindowHandle(sessionName + ":")
		pid, pidErr := adapter.WindowPanePID(ctx, firstHandle)
		if pidErr != nil {
			if logger != nil {
				logger.Printf("daemon: probeCrewRegistrySessions: crew %q WindowPanePID error: %v (skipping)", rec.Name, pidErr)
			}
			continue
		}
		if pid <= 0 {
			if logger != nil {
				logger.Printf("daemon: probeCrewRegistrySessions: crew %q session %q pane PID %d invalid (skipping)", rec.Name, sessionName, pid)
			}
			continue
		}

		if err := syscall.Kill(pid, 0); err != nil && err != syscall.EPERM {
			if logger != nil {
				logger.Printf("daemon: probeCrewRegistrySessions: crew %q session %q pane PID %d dead (%v); letting sweep handle it", rec.Name, sessionName, pid, err)
			}
			continue
		}

		excludeSessions[sessionName] = struct{}{}
		skipped++
		if logger != nil {
			logger.Printf("daemon: probeCrewRegistrySessions: crew %q session %q is live (PID %d); skipping (PL-006d iii)", rec.Name, sessionName, pid)
		}
	}

	crewPrefix := lifecycle.TmuxSessionName(projectHash, "crew-")
	for sessionName := range sessionSnapshot {
		if _, alreadyExcluded := excludeSessions[sessionName]; alreadyExcluded {
			continue
		}
		if !strings.HasPrefix(sessionName, crewPrefix) {
			continue
		}
		pid, pidErr := adapter.WindowPanePID(ctx, ltmux.WindowHandle(sessionName+":"))
		if pidErr != nil || pid <= 0 {
			continue
		}
		if !pidIsLive(pid) {
			continue
		}
		excludeSessions[sessionName] = struct{}{}
		skipped++
		if logger != nil {
			logger.Printf("daemon: probeCrewRegistrySessions: live-session fallback: session %q is live (PID %d); skipping (hk-aoapq)", sessionName, pid)
		}
	}

	return skipped
}

// RunOrphanSweep executes the full PL-006 orphan sweep in order:
//
//  1. Probe the coordinator (flywheel) sentinel (PL-006d).
//  2. Kill orphan tmux sessions matching the project-hash prefix (bullet a),
//     excluding any live coordinator sessions per PL-006d.
//  3. Remove stale worktree lease-lock files via workspace.SweepStaleLeaseLocks (bullet b).
//  4. Kill orphan handler subprocesses with matching provenance marker (bullet c).
//  5. Kill orphan br subprocesses re-parented to init (bullet c, br half).
//  6. Enumerate (but do NOT remove) stale intent files (bullet d).
//  7. Remove stale reconciliation lock files (bullet e).
//
// On completion it returns an [OrphanSweepResult] ready to be converted to the
// daemon_orphan_sweep_completed event payload via [OrphanSweepResult.ToPayload].
//
// Spec ref: process-lifecycle.md §4.2 PL-006 — full 6-bullet mandate.
// Spec ref: process-lifecycle.md §4.2 PL-006d — coordinator sentinel exclusion.
func RunOrphanSweep(
	ctx context.Context,
	projectDir string,
	projectHash core.ProjectHash,
	daemonStartTime time.Time,
	cfg OrphanSweepConfig,
) (OrphanSweepResult, error) {
	var result OrphanSweepResult
	var errs []string

	liveRunIDs := map[string]struct{}{}

	liveRunsKnown := true

	for runID := range cfg.DispatchOwnership.Runs {
		liveRunIDs[runID.String()] = struct{}{}
	}
	for runID := range cfg.DispatchOwnership.Worktrees {
		liveRunIDs[runID.String()] = struct{}{}
	}

	excludedTmuxSessions := map[string]struct{}{}
	for sessionName := range cfg.DispatchOwnership.Sessions {
		excludedTmuxSessions[sessionName] = struct{}{}
	}
	if cfg.DaemonSpawnSession != "" {
		excludedTmuxSessions[cfg.DaemonSpawnSession] = struct{}{}
		if cfg.Logger != nil {
			cfg.Logger.Printf("daemon: RunOrphanSweep: excluding daemon spawn-target session %q from sweep (hk-9vp51)", cfg.DaemonSpawnSession)
		}
	}

	sessionSnapshot := map[string]struct{}{}
	if cfg.TmuxAdapter != nil {
		liveSessions, listErr := cfg.TmuxAdapter.ListSessions(ctx)
		if listErr != nil {
			errs = append(errs, fmt.Sprintf("session-snapshot: %v", listErr))
		} else {
			for _, s := range liveSessions {
				sessionSnapshot[s] = struct{}{}
			}
		}
	}

	if projectDir != "" {
		probe, probeErr := probeCoordinatorSentinel(projectDir, cfg.Logger)
		if probeErr != nil {
			errs = append(errs, fmt.Sprintf("coordinator-sentinel: %v", probeErr))
		}
		if probe.Live {
			flywheelSession := lifecycle.TmuxSessionName(projectHash, "flywheel")
			excludedTmuxSessions[flywheelSession] = struct{}{}
			result.CoordinatorSessionsSkipped = 1
			if cfg.Logger != nil {
				cfg.Logger.Printf("daemon: RunOrphanSweep: skipping coordinator session %q (orphan_sweep_skipped_coordinator_session)", flywheelSession)
			}
		} else if probe.SentinelRemoved {
			reaped := reapDeadCoordinatorSession(ctx, projectHash, cfg.TmuxAdapter, cfg.Logger)
			result.CoordinatorSessionsReaped = reaped
			result.TmuxSessionsKilled += reaped
		} else {
			reaped := reapDeadCoordinatorSession(ctx, projectHash, cfg.TmuxAdapter, cfg.Logger)
			result.CoordinatorSessionsReaped += reaped
			result.TmuxSessionsKilled += reaped
		}

		if probeCaptainSentinel(ctx, projectDir, projectHash, cfg.TmuxAdapter, cfg.Logger, sessionSnapshot) {
			captainSession := lifecycle.TmuxSessionName(projectHash, "captain")
			excludedTmuxSessions[captainSession] = struct{}{}
			result.CaptainSessionsSkipped = 1
			if cfg.Logger != nil {
				cfg.Logger.Printf("daemon: RunOrphanSweep: skipping captain session %q (PL-006d ii)", captainSession)
			}
		}

		result.CrewSessionsSkipped = probeCrewRegistrySessions(
			ctx, projectDir, projectHash, cfg.TmuxAdapter, cfg.Logger,
			sessionSnapshot, excludedTmuxSessions,
		)

		registryRunIDs, registryRead := probeRunRegistrySessions(
			ctx, projectDir, cfg.TmuxAdapter, cfg.Logger,
			sessionSnapshot, excludedTmuxSessions,
		)
		if !registryRead {
			liveRunsKnown = false
			errs = append(errs, "run-registry: unreadable; destructive sweep passes stood down")
		}
		for runID := range registryRunIDs {
			liveRunIDs[runID] = struct{}{}
		}
	}

	if liveRunsKnown {
		tmuxKilled, tmuxErr := lifecycle.SweepOrphanTmuxSessions(ctx, projectHash, cfg.TmuxLister, cfg.TmuxKiller, cfg.Logger, excludedTmuxSessions)
		if tmuxErr != nil {
			errs = append(errs, fmt.Sprintf("tmux: %v", tmuxErr))
		}
		result.TmuxSessionsKilled += tmuxKilled

		adapterSessionsKilled, adapterErr := ltmux.SweepOrphanTmuxSessions(ctx, projectHash, cfg.TmuxAdapter, cfg.Logger, excludedTmuxSessions)
		if adapterErr != nil {
			errs = append(errs, fmt.Sprintf("tmux-sessions-adapter: %v", adapterErr))
		}
		result.TmuxSessionsKilled += adapterSessionsKilled
	} else {
		logRegistryStandDown(cfg.Logger, "session-kill passes")
	}

	windowsKilled, err := ltmux.SweepOrphanTmuxWindows(ctx, projectHash, cfg.TmuxAdapter, cfg.Logger)
	if err != nil {
		errs = append(errs, fmt.Sprintf("tmux-windows: %v", err))
	}
	result.TmuxWindowsKilled = windowsKilled

	sweepResult, err := workspace.SweepStaleLeaseLocks(ctx, projectDir, workspace.NoWorktreeRootOverride())
	if err != nil {
		errs = append(errs, fmt.Sprintf("lease-locks: %v", err))
	}
	result.LocksCleared = len(sweepResult.Removed)

	if !liveRunsKnown {
		logRegistryStandDown(cfg.Logger, "worktree force-removal and age-prune")
	}
	if removable := worktreesNotHeldByALiveRun(sweepResult.Removed, liveRunIDs, cfg.Logger); liveRunsKnown && len(removable) > 0 {
		gcResult := workspace.RemoveStaleWorktrees(ctx, projectDir, removable, cfg.Logger)
		result.WorktreeDirsRemoved = len(gcResult.Removed)
		if len(gcResult.Failed) > 0 {
			errs = append(errs, fmt.Sprintf("worktree-dirs-gc: %d of %d removals failed", len(gcResult.Failed), len(removable)))
		}
	}

	agedCandidates := worktreesNotHeldByALiveRun(sweepResult.NoLock, liveRunIDs, cfg.Logger)
	if liveRunsKnown && len(agedCandidates) > 0 {
		agedResult := workspace.RemoveAgedNoLockWorktrees(ctx, projectDir, agedCandidates, harmonikWorktreeMaxAge(), cfg.Logger)
		result.WorktreeDirsRemoved += len(agedResult.Removed)
		if len(agedResult.Failed) > 0 {
			errs = append(errs, fmt.Sprintf("worktree-aged-gc: %d of %d removals failed", len(agedResult.Failed), len(agedCandidates)))
		}
	}

	handlersKilled, err := lifecycle.SweepOrphanHandlers(ctx, projectHash, cfg.HandlerLister, cfg.Logger)
	if err != nil {
		errs = append(errs, fmt.Sprintf("handlers: %v", err))
	}
	result.SubprocessesKilled = handlersKilled

	brSurvived, err := lifecycle.SweepOrphanBr(ctx, cfg.BrLister, cfg.Logger)
	if err != nil {
		errs = append(errs, fmt.Sprintf("br: %v", err))
	}
	_ = brSurvived // survival tracked for Cat 0 precondition, not used in count here

	if cfg.IntentGCLedger != nil {
		gcResult, gcErr := lifecycle.GCRetiredIntentsWithRedrive(ctx, lifecycle.GCRetiredIntentsConfig{
			ProjectDir:      projectDir,
			DaemonStartTime: daemonStartTime,
			Ledger:          cfg.IntentGCLedger,
			RedriveWriter:   cfg.IntentRedriveWriter,
			BrTimeoutCfg:    cfg.BrTimeoutCfg,
			Logger:          cfg.Logger,
		})
		if gcErr != nil {
			errs = append(errs, fmt.Sprintf("intents-gc: %v", gcErr))
		}
		result.StaleIntentsObserved = gcResult.Retained
		result.IntentsGCd = gcResult.Removed + gcResult.RedriveCount
	} else {
		staleIntents, err := lifecycle.EnumerateStaleIntents(projectDir, daemonStartTime)
		if err != nil {
			errs = append(errs, fmt.Sprintf("intents: %v", err))
		}
		result.StaleIntentsObserved = staleIntents
	}

	reconResult, err := lifecycle.SweepStaleReconciliationLocks(projectDir, cfg.Logger)
	if err != nil {
		errs = append(errs, fmt.Sprintf("recon-locks: %v", err))
	}
	result.ReconciliationLocksRemoved = reconResult.Removed
	result.Cat3bRunIDs = reconResult.Cat3bRunIDs

	if cfg.BeadLedger != nil && cfg.BeadResetter != nil {
		queueDispatched, queueOwned := queueOwnershipWithDispatch(cfg)
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
			QueueDispatched: queueDispatched,
			QueueOwned:      queueOwned,
			Logger:          cfg.Logger,
		})
		if beadResetErr != nil {
			errs = append(errs, fmt.Sprintf("bead-reset: %v", beadResetErr))
		}
		result.BeadInProgressReset = sweepResult.ResetCount
		result.BeadCat3cClosed = sweepResult.Cat3cCloseCount
	}

	claudeResult, claudeErr := SweepClaudeWorktrees(ctx, projectDir, cfg.Logger)
	if claudeErr != nil {
		errs = append(errs, fmt.Sprintf("claude-worktrees: %v", claudeErr))
	}
	result.ClaudeWorktreesSwept = len(claudeResult.Orphans)

	result.SweptAt = time.Now()
	archiveReport, archiveErr := lifecycle.ObserveQueueArchives(projectDir, lifecycle.ObserveQueueArchivesConfig{
		Now:    result.SweptAt,
		Logger: cfg.Logger,
	})
	if archiveErr != nil {
		errs = append(errs, fmt.Sprintf("queue-archives: %v", archiveErr))
	}
	result.QueueArchivesObserved = archiveReport.Count
	result.QueueArchiveBytes = archiveReport.TotalBytes
	result.QueueArchivesOverRetention = archiveReport.OverRetention

	if len(errs) > 0 {
		return result, fmt.Errorf("daemon: RunOrphanSweep: %s", strings.Join(errs, "; "))
	}
	return result, nil
}

func queueOwnershipWithDispatch(cfg OrphanSweepConfig) (lifecycle.QueueDispatchedSet, lifecycle.QueueOwnedSet) {
	dispatched := make(lifecycle.QueueDispatchedSet, len(cfg.QueueDispatched)+len(cfg.DispatchOwnership.Beads))
	owned := make(lifecycle.QueueOwnedSet, len(cfg.QueueOwned)+len(cfg.DispatchOwnership.Beads))
	for beadID := range cfg.QueueDispatched {
		dispatched[beadID] = struct{}{}
	}
	for beadID := range cfg.QueueOwned {
		owned[beadID] = struct{}{}
	}
	for beadID := range cfg.DispatchOwnership.Beads {
		dispatched[beadID] = struct{}{}
		owned[beadID] = struct{}{}
	}
	return dispatched, owned
}

func runPeriodicCoordinatorReap(ctx context.Context, projectDir string, projectHash core.ProjectHash, adapter ltmux.Adapter, logger *log.Logger) int {
	if adapter == nil || projectDir == "" {
		return 0
	}
	probe, probeErr := probeCoordinatorSentinel(projectDir, logger)
	if probeErr != nil {
		if logger != nil {
			logger.Printf("daemon: runPeriodicCoordinatorReap: sentinel probe error (skipping): %v", probeErr)
		}
		return 0
	}
	if probe.Live {
		return 0
	}
	return reapDeadCoordinatorSession(ctx, projectHash, adapter, logger)
}
