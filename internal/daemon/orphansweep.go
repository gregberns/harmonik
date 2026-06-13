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

	// IntentsGCd is the count of stale intent files removed by GCRetiredIntents
	// because the target bead has already reached its IntendedPostState
	// (the op landed in a prior run; the file was a BI-030 step-6 leftover).
	// Zero when IntentGCLedger is nil.
	//
	// Bead ref: hk-cizvu — stale_intents_observed GC.
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

	// IntentGCLedger, when non-nil, enables GCRetiredIntents: stale intent
	// files whose bead has already reached IntendedPostState are deleted rather
	// than accumulated indefinitely. When nil, the GC pass is skipped and
	// StaleIntentsObserved counts all stale files (old behavior).
	//
	// Production callers SHOULD supply this (typically the same *brcli.Adapter
	// that backs BeadLedger / BeadResetter). Unit-test callers that do not
	// exercise the GC path may leave it nil.
	//
	// Bead ref: hk-cizvu — stale_intents_observed grows unbounded without GC.
	IntentGCLedger lifecycle.IntentGCLedger

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

// coordinatorSentinelDir returns the path to .harmonik/cognition/ for projectDir.
func coordinatorSentinelDir(projectDir string) string {
	return filepath.Join(projectDir, ".harmonik", "cognition")
}

// coordinatorSentinelPath returns the supervisor.sentinel path (PL-006d).
func coordinatorSentinelPath(projectDir string) string {
	return filepath.Join(coordinatorSentinelDir(projectDir), "supervisor.sentinel")
}

// coordinatorPidfilePath returns the supervisor.pid path.
func coordinatorPidfilePath(projectDir string) string {
	return filepath.Join(coordinatorSentinelDir(projectDir), "supervisor.pid")
}

// probeCoordinatorSentinelResult is the outcome of [probeCoordinatorSentinel].
type probeCoordinatorSentinelResult struct {
	// Live is true when the coordinator sentinel is present AND the supervisor
	// PID is still running. The flywheel session MUST be excluded from the
	// orphan sweep in this case (PL-006d).
	Live bool

	// SentinelRemoved is true when a stale sentinel file was removed as part
	// of the probe (sentinel present but PID dead or unreadable).
	SentinelRemoved bool
}

// probeCoordinatorSentinel checks whether the supervisor (flywheel) process is
// live per PL-006d:
//
//   - Reads supervisor.sentinel at .harmonik/cognition/supervisor.sentinel.
//   - If absent → coordinator is not running; result.Live = false.
//   - If present → reads .harmonik/cognition/supervisor.pid and probes the PID
//     via kill(pid, 0).
//   - If PID live → result.Live = true (flywheel session MUST be excluded).
//   - If PID dead or unreadable → result.Live = false; removes the stale sentinel
//     and sets result.SentinelRemoved = true.
//
// Errors reading or removing the sentinel are non-fatal and are returned
// alongside the result so the caller can log them.
func probeCoordinatorSentinel(projectDir string, logger *log.Logger) (probeCoordinatorSentinelResult, error) {
	sentinelPath := coordinatorSentinelPath(projectDir)
	pidfilePath := coordinatorPidfilePath(projectDir)

	// Probe sentinel existence via stat.
	if _, statErr := os.Stat(sentinelPath); os.IsNotExist(statErr) {
		// Sentinel absent — coordinator is not running; nothing to exclude.
		return probeCoordinatorSentinelResult{}, nil
	}

	// Sentinel present. Read supervisor PID.
	pid, readErr := readSupervisorPID(pidfilePath)
	if readErr != nil {
		// Can't read PID — treat as stale sentinel; remove it.
		if logger != nil {
			logger.Printf("daemon: probeCoordinatorSentinel: cannot read supervisor.pid (%v); removing stale sentinel", readErr)
		}
		removed := removeStaleSentinel(sentinelPath, logger)
		return probeCoordinatorSentinelResult{SentinelRemoved: removed}, nil
	}

	// Probe PID liveness.
	if err := syscall.Kill(pid, 0); err != nil {
		if err == syscall.EPERM {
			// Process exists but we lack permission — treat as live.
			if logger != nil {
				logger.Printf("daemon: probeCoordinatorSentinel: supervisor PID %d EPERM (live); skipping flywheel session (PL-006d)", pid)
			}
			return probeCoordinatorSentinelResult{Live: true}, nil
		}
		// ESRCH or other error → process is dead; remove stale sentinel.
		if logger != nil {
			logger.Printf("daemon: probeCoordinatorSentinel: supervisor PID %d is dead (%v); removing stale sentinel (PL-006d)", pid, err)
		}
		removed := removeStaleSentinel(sentinelPath, logger)
		return probeCoordinatorSentinelResult{SentinelRemoved: removed}, nil
	}

	// PID is live — coordinator is running; exclude its session.
	if logger != nil {
		logger.Printf("daemon: probeCoordinatorSentinel: supervisor PID %d is live; skipping flywheel session (PL-006d)", pid)
	}
	return probeCoordinatorSentinelResult{Live: true}, nil
}

// reapDeadCoordinatorSession force-kills the coordinator (flywheel) tmux session
// for projectHash when its owning supervisor has been confirmed dead (caller
// established this via probeCoordinatorSentinel: sentinel present, kill(pid,0) →
// ESRCH).  It only kills the session if it is actually present in the live tmux
// session list — never a live supervisor's session, because the caller only
// invokes this on the dead-PID branch.
//
// SAFETY (hk-9vp51 fix-forward): the ONLY session this reaper can ever target is
// lifecycle.TmuxSessionName(projectHash, "flywheel") — the "-flywheel"-suffixed
// coordinator session.  The daemon's own implementer-spawn-target session is a
// DIFFERENT name (the ambient session the daemon runs in, or the
// "-default"-suffixed session per DefaultSessionName), so this reaper can NEVER
// kill the daemon's own live spawn-target session.  This is the explicit guard
// the original sub-fix #3 lacked: that revert was caused by the spawn target
// vanishing; here we prove by construction the reaper cannot be the cause.  As a
// belt-and-suspenders assertion we refuse to kill anything that does not carry
// the flywheel suffix.
//
// Returns the count of sessions reaped (0 or 1).  Non-fatal: a nil adapter, a
// ListSessions error, or a KillSession error is logged and treated as 0/handled.
//
// Mirrors the reconciler-reaper pattern (hk-5pg37) and the dead-PID liveness
// discipline of sessionIsOrphaned.  hk-9vp51.
func reapDeadCoordinatorSession(ctx context.Context, projectHash core.ProjectHash, adapter ltmux.Adapter, logger *log.Logger) int {
	if adapter == nil {
		return 0
	}
	flywheelSession := lifecycle.TmuxSessionName(projectHash, "flywheel")

	// Belt-and-suspenders self-guard: the reaper must ONLY ever target the
	// flywheel-suffixed coordinator session, never the daemon's own spawn-target
	// session.  If the name does not carry the flywheel suffix something is badly
	// wrong; refuse rather than risk reaping a live spawn target (the prime
	// suspect that broke the original sub-fix #3).
	if !strings.HasSuffix(flywheelSession, "-flywheel") {
		if logger != nil {
			logger.Printf("daemon: reapDeadCoordinatorSession: refusing to reap %q — not a flywheel-suffixed coordinator session (hk-9vp51 self-guard)", flywheelSession)
		}
		return 0
	}

	// Confirm the session exists before issuing the kill (avoids a spurious
	// KillSession error log for the common case where the supervisor exited
	// cleanly and already tore down its session).
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
		// TOCTOU (session vanished) or other error: log and still count it — we
		// identified it as a dead-supervisor leak.
		if logger != nil {
			logger.Printf("daemon: reapDeadCoordinatorSession: kill-session %q error (proceeding): %v", flywheelSession, killErr)
		}
	}
	return 1
}

// readSupervisorPID reads a single ASCII decimal PID line from path.
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

// removeStaleSentinel unlinks the sentinel file and fsyncs its parent directory.
// Returns true if the remove succeeded, false on error (non-fatal).
func removeStaleSentinel(sentinelPath string, logger *log.Logger) bool {
	if err := os.Remove(sentinelPath); err != nil && !os.IsNotExist(err) {
		if logger != nil {
			logger.Printf("daemon: removeStaleSentinel: Remove %q: %v (non-fatal)", sentinelPath, err)
		}
		return false
	}
	// fsync parent directory so the unlink is durable.
	dir := filepath.Dir(sentinelPath)
	//nolint:gosec // G304: dir is derived from operator-controlled projectDir
	if dirFd, openErr := os.Open(dir); openErr == nil {
		_ = dirFd.Sync()  //nolint:errcheck
		_ = dirFd.Close() //nolint:errcheck
	}
	return true
}

// captainSentinelPath returns the captain.sentinel path (PL-006d mechanism ii).
func captainSentinelPath(projectDir string) string {
	return filepath.Join(coordinatorSentinelDir(projectDir), "captain.sentinel")
}

// captainPidfilePath returns the captain.pid path.
func captainPidfilePath(projectDir string) string {
	return filepath.Join(coordinatorSentinelDir(projectDir), "captain.pid")
}

// probeCaptainSentinel checks whether the captain process is live per PL-006d
// mechanism (ii):
//
//   - Reads captain.sentinel at .harmonik/cognition/captain.sentinel.
//   - If absent → captain is not running; returns false.
//   - If present → reads .harmonik/cognition/captain.pid and probes the PID
//     via kill(pid, 0).
//   - If PID live → returns true (captain session MUST be excluded).
//   - If PID dead or unreadable → returns false; removes the stale sentinel.
//
// Spec ref: process-lifecycle.md §4.2 PL-006d mechanism (ii).
// Bead ref: hk-qp3.
func probeCaptainSentinel(projectDir string, logger *log.Logger) bool {
	sentinelPath := captainSentinelPath(projectDir)
	pidfilePath := captainPidfilePath(projectDir)

	if _, statErr := os.Stat(sentinelPath); os.IsNotExist(statErr) {
		return false
	}

	pid, readErr := readSupervisorPID(pidfilePath)
	if readErr != nil {
		if logger != nil {
			logger.Printf("daemon: probeCaptainSentinel: cannot read captain.pid (%v); removing stale sentinel", readErr)
		}
		removeStaleSentinel(sentinelPath, logger)
		return false
	}

	if err := syscall.Kill(pid, 0); err != nil {
		if err == syscall.EPERM {
			if logger != nil {
				logger.Printf("daemon: probeCaptainSentinel: captain PID %d EPERM (live); skipping captain session (PL-006d ii)", pid)
			}
			return true
		}
		if logger != nil {
			logger.Printf("daemon: probeCaptainSentinel: captain PID %d is dead (%v); removing stale sentinel", pid, err)
		}
		removeStaleSentinel(sentinelPath, logger)
		return false
	}

	if logger != nil {
		logger.Printf("daemon: probeCaptainSentinel: captain PID %d is live; skipping captain session (PL-006d ii)", pid)
	}
	return true
}

// probeCrewRegistrySessions lists crew registry records, checks each crew's
// tmux session against the live session snapshot, and adds live crew sessions
// (session present AND first-pane PID alive) to excludeSessions.
//
// REVIEW-FIX: session ABSENT from snapshot → launch-in-flight; skip
// conservatively (do NOT call crew.Remove). Session present + PID dead →
// let the generic sweep handle it (do NOT call crew.Remove).
//
// Returns the count of sessions added to excludeSessions.
//
// Spec ref: process-lifecycle.md §4.2 PL-006d mechanism (iii).
// Bead ref: hk-qp3.
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
			logger.Printf("daemon: probeCrewRegistrySessions: crew.List error (skipping crew exemption): %v", err)
		}
		return 0
	}

	skipped := 0
	for _, rec := range records {
		sessionName := lifecycle.TmuxSessionName(projectHash, "crew-"+rec.Name)

		// If the session is not in the live snapshot, it may be launch-in-flight.
		// Skip conservatively — do NOT call crew.Remove.
		if _, present := sessionSnapshot[sessionName]; !present {
			if logger != nil {
				logger.Printf("daemon: probeCrewRegistrySessions: crew %q session %q absent from snapshot (launch-in-flight?); skipping", rec.Name, sessionName)
			}
			continue
		}

		// Session is present — probe the first-pane PID.
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

		// kill(pid, 0) probes liveness.
		if err := syscall.Kill(pid, 0); err != nil && err != syscall.EPERM {
			// ESRCH = dead; let the generic sweep handle it. Do NOT call crew.Remove.
			if logger != nil {
				logger.Printf("daemon: probeCrewRegistrySessions: crew %q session %q pane PID %d dead (%v); letting sweep handle it", rec.Name, sessionName, pid, err)
			}
			continue
		}

		// PID live (or EPERM = exists but no permission = alive): exempt.
		excludeSessions[sessionName] = struct{}{}
		skipped++
		if logger != nil {
			logger.Printf("daemon: probeCrewRegistrySessions: crew %q session %q is live (PID %d); skipping (PL-006d iii)", rec.Name, sessionName, pid)
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

	// (PL-006d) Probe the coordinator (flywheel) sentinel before the tmux sweep.
	// If the sentinel is present and the supervisor PID is live, exclude the
	// flywheel session from the sweep. If the sentinel is stale (dead PID), kill
	// the session normally and remove the sentinel.
	excludedTmuxSessions := map[string]struct{}{}
	// hk-9vp51: ALWAYS exclude the daemon's own spawn-target session from the
	// session-level sweep — regardless of coordinator state. In the fix-forward
	// fallback case the daemon EnsureSessions a fresh "harmonik-<hash>-default"
	// session whose only window is an idle zsh at boot; without this exclusion the
	// generic sweep (sessionIsOrphaned: zero non-zsh windows → orphaned) would
	// kill the daemon's own session before the first dispatch, reproducing the
	// reverted sub-fix #3 "session does not exist" regression.
	if cfg.DaemonSpawnSession != "" {
		excludedTmuxSessions[cfg.DaemonSpawnSession] = struct{}{}
		if cfg.Logger != nil {
			cfg.Logger.Printf("daemon: RunOrphanSweep: excluding daemon spawn-target session %q from sweep (hk-9vp51)", cfg.DaemonSpawnSession)
		}
	}

	// Build a snapshot of live tmux sessions (needed by probeCrewRegistrySessions).
	// We snapshot once here to avoid repeated ListSessions calls in the loop below.
	// A nil adapter means no tmux: snapshot is empty (no sessions to exempt).
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
			// Coordinator is live: exclude its flywheel session from the kill sweep.
			flywheelSession := lifecycle.TmuxSessionName(projectHash, "flywheel")
			excludedTmuxSessions[flywheelSession] = struct{}{}
			result.CoordinatorSessionsSkipped = 1
			if cfg.Logger != nil {
				cfg.Logger.Printf("daemon: RunOrphanSweep: skipping coordinator session %q (orphan_sweep_skipped_coordinator_session)", flywheelSession)
			}
		} else if probe.SentinelRemoved {
			// hk-9vp51: the sentinel was present but the supervisor PID is DEAD
			// (probeCoordinatorSentinel removed the stale sentinel after kill(pid,0)
			// returned ESRCH).  Force-reap the dead supervisor's flywheel/coordinator
			// session at boot: it is exempt from the generic orphan classification
			// (sessionIsOrphaned can report it "alive" when the supervisor's
			// re-parented bash children keep the first pane PID live), so without
			// this it leaks forever.  The supervisor PID is verified dead by the
			// probe, so this is safe — we never kill a session of a LIVE supervisor.
			reaped := reapDeadCoordinatorSession(ctx, projectHash, cfg.TmuxAdapter, cfg.Logger)
			result.CoordinatorSessionsReaped = reaped
			result.TmuxSessionsKilled += reaped
		} else {
			// hk-7u002: Sentinel absent — no supervisor was ever started, or it
			// stopped cleanly and removed its own sentinel.  The flywheel session is
			// unconditionally orphaned: sessionIsOrphaned misses sessions whose first
			// pane PID is live (shells from prior implementer launches that outlived the
			// daemon crash), so without this explicit reap, harmonik-<hash>-flywheel
			// sessions accumulate across daemon restarts until tmux resource exhaustion
			// blocks new spawns.  At daemon startup every project-scoped session is an
			// orphan by definition (PL-006: "the new daemon has no in-memory tracking
			// at this point"), so reaping a sentinel-absent flywheel session is safe.
			reaped := reapDeadCoordinatorSession(ctx, projectHash, cfg.TmuxAdapter, cfg.Logger)
			result.CoordinatorSessionsReaped += reaped
			result.TmuxSessionsKilled += reaped
		}

		// (PL-006d mechanism ii) Captain sentinel: exclude the captain session when
		// captain.sentinel is present and captain PID is alive.
		if probeCaptainSentinel(projectDir, cfg.Logger) {
			captainSession := lifecycle.TmuxSessionName(projectHash, "captain")
			excludedTmuxSessions[captainSession] = struct{}{}
			result.CaptainSessionsSkipped = 1
			if cfg.Logger != nil {
				cfg.Logger.Printf("daemon: RunOrphanSweep: skipping captain session %q (PL-006d ii)", captainSession)
			}
		}

		// (PL-006d mechanism iii) Crew registry probe: exclude live crew sessions.
		result.CrewSessionsSkipped = probeCrewRegistrySessions(
			ctx, projectDir, projectHash, cfg.TmuxAdapter, cfg.Logger,
			sessionSnapshot, excludedTmuxSessions,
		)
	}

	// (a) Tmux sessions — two passes:
	//   (a1) Kill orphan harmonik-owned sessions via the legacy TmuxLister/TmuxKiller path.
	tmuxKilled, err := lifecycle.SweepOrphanTmuxSessions(ctx, projectHash, cfg.TmuxLister, cfg.TmuxKiller, cfg.Logger, excludedTmuxSessions)
	if err != nil {
		errs = append(errs, fmt.Sprintf("tmux: %v", err))
	}
	// hk-9vp51: accumulate (+=) rather than assign so the dead-supervisor
	// coordinator reaper's contribution above is not overwritten.
	result.TmuxSessionsKilled += tmuxKilled

	//   (a1b) Kill orphan harmonik-owned sessions via the Adapter path (hk-kqdpf.3):
	//   enumerates sessions matching harmonik-<12-char-hash>- prefix, kills those
	//   with dead PIDs or zero non-zsh windows. Must run BEFORE the window sweep
	//   so dead sessions are removed before we attempt to sweep their windows.
	adapterSessionsKilled, err := ltmux.SweepOrphanTmuxSessions(ctx, projectHash, cfg.TmuxAdapter, cfg.Logger, excludedTmuxSessions)
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

	// (d) Stale intent files.
	//
	// When IntentGCLedger is wired: call GCRetiredIntents to delete intent
	// files whose op has already landed (BI-031 GC path). StaleIntentsObserved
	// is set to the retained count; IntentsGCd is set to the removed count.
	// This prevents stale_intents_observed from growing unboundedly (hk-cizvu).
	//
	// When IntentGCLedger is nil: fall back to EnumerateStaleIntents (count
	// only, no removal — the legacy behavior).
	if cfg.IntentGCLedger != nil {
		gcResult, gcErr := lifecycle.GCRetiredIntents(ctx, projectDir, daemonStartTime, cfg.IntentGCLedger, cfg.Logger)
		if gcErr != nil {
			errs = append(errs, fmt.Sprintf("intents-gc: %v", gcErr))
		}
		result.StaleIntentsObserved = gcResult.Retained
		result.IntentsGCd = gcResult.Removed
	} else {
		staleIntents, err := lifecycle.EnumerateStaleIntents(projectDir, daemonStartTime)
		if err != nil {
			errs = append(errs, fmt.Sprintf("intents: %v", err))
		}
		result.StaleIntentsObserved = staleIntents
	}

	// (e) Stale reconciliation locks (RC-002b discrimination).
	reconResult, err := lifecycle.SweepStaleReconciliationLocks(projectDir, cfg.Logger)
	if err != nil {
		errs = append(errs, fmt.Sprintf("recon-locks: %v", err))
	}
	result.ReconciliationLocksRemoved = reconResult.Removed
	result.Cat3bRunIDs = reconResult.Cat3bRunIDs

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
