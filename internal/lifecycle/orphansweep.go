package lifecycle

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/gregberns/harmonik/internal/brcli"
	"github.com/gregberns/harmonik/internal/core"
)

// TmuxSessionLister enumerates live tmux sessions by name. Production
// implementations invoke the real tmux binary; tests inject a deterministic fake.
//
// Spec ref: process-lifecycle.md §4.2 PL-006 — "The daemon MUST list tmux
// sessions matching the project's harmonik naming convention
// (prefix harmonik-<project_hash>-)."
type TmuxSessionLister interface {
	ListTmuxSessions(ctx context.Context) ([]string, error)
}

// TmuxSessionKiller kills a named tmux session. Production implementations
// invoke `tmux kill-session -t <name>`; tests inject a fake that records kills.
//
// Spec ref: process-lifecycle.md §4.2 PL-006 — "kill every matching session
// via tmux kill-session."
type TmuxSessionKiller interface {
	KillTmuxSession(ctx context.Context, sessionName string) error
}

// OSTmuxSessionLister is the production TmuxSessionLister. It invokes
// `tmux list-sessions -F "#{session_name}"` and returns the session names.
//
// tmux exits non-zero both when no server is running and when the call
// genuinely failed. Only "no server running" is reported as an empty list; every
// other failure — including tmux not being installed — is now returned, so a
// broken tmux can no longer masquerade as "zero sessions" and silently reap
// nothing. RunOrphanSweep accumulates that error and continues, and the boot
// call site discards it per PL-006, so a tmux-less host still boots cleanly.
type OSTmuxSessionLister struct{}

func tmuxServerAbsent(out []byte) bool {
	return strings.Contains(strings.ToLower(strings.TrimSpace(string(out))), "no server running")
}

// ListTmuxSessions implements TmuxSessionLister.
func (OSTmuxSessionLister) ListTmuxSessions(ctx context.Context) ([]string, error) {
	out, err := exec.CommandContext(ctx, "tmux", "list-sessions", "-F", "#{session_name}").CombinedOutput()
	if err != nil {
		if tmuxServerAbsent(out) {
			return nil, nil
		}
		return nil, fmt.Errorf("lifecycle: ListTmuxSessions: %w (output: %s)", err, strings.TrimSpace(string(out)))
	}
	var names []string
	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	for scanner.Scan() {
		name := strings.TrimSpace(scanner.Text())
		if name != "" {
			names = append(names, name)
		}
	}
	return names, nil
}

// OSTmuxSessionKiller is the production TmuxSessionKiller. It invokes
// `tmux kill-session -t <sessionName>`.
type OSTmuxSessionKiller struct{}

// KillTmuxSession implements TmuxSessionKiller.
func (OSTmuxSessionKiller) KillTmuxSession(ctx context.Context, sessionName string) error {
	out, err := exec.CommandContext(ctx, "tmux", "kill-session", "-t", sessionName).CombinedOutput()
	if err != nil {
		return fmt.Errorf("lifecycle: KillTmuxSession %q: %w (output: %s)", sessionName, err, strings.TrimSpace(string(out)))
	}
	return nil
}

var tmuxPollInterval = 100 * time.Millisecond

var tmuxPollCeiling = 2 * time.Second

// SweepOrphanTmuxSessions lists all tmux sessions, filters those whose name
// matches the project-hash prefix harmonik-<projectHash>-, kills each via
// kill-session, polls for exit, then returns the count killed.
//
// excludeSessions is an optional set of session names to skip (used by the
// PL-006d coordinator sentinel exclusion — sessions with a live supervisor
// process must not be killed). Nil or empty map means no exclusions.
//
// If lister is nil, OSTmuxSessionLister is used.
// If killer is nil, OSTmuxSessionKiller is used.
// If logger is nil, log messages are discarded.
//
// Spec ref: process-lifecycle.md §4.2 PL-006 — "Tmux sessions. The daemon MUST
// list tmux sessions matching the project's harmonik naming convention (prefix
// harmonik-<project_hash>-) and kill every matching session via tmux kill-session.
// After kill, the daemon MUST poll for underlying process exit at a 100 ms
// cadence up to a 2-second ceiling. After the ceiling expires, the daemon
// proceeds regardless."
func SweepOrphanTmuxSessions(
	ctx context.Context,
	projectHash core.ProjectHash,
	lister TmuxSessionLister,
	killer TmuxSessionKiller,
	logger *log.Logger,
	excludeSessions map[string]struct{},
) (killed int, err error) {
	if lister == nil {
		lister = OSTmuxSessionLister{}
	}
	if killer == nil {
		killer = OSTmuxSessionKiller{}
	}

	sessions, err := lister.ListTmuxSessions(ctx)
	if err != nil {
		return 0, fmt.Errorf("lifecycle: SweepOrphanTmuxSessions: list: %w", err)
	}

	prefix := TmuxSessionPrefix(projectHash)
	for _, name := range sessions {
		if !strings.HasPrefix(name, prefix) {
			continue
		}
		if _, skip := excludeSessions[name]; skip {
			orphanLog(logger, "SweepOrphanTmuxSessions: skipping coordinator session %q (PL-006d exclusion)", name)
			continue
		}
		orphanLog(logger, "SweepOrphanTmuxSessions: killing session %q", name)
		if killErr := killer.KillTmuxSession(ctx, name); killErr != nil {
			orphanLog(logger, "SweepOrphanTmuxSessions: kill-session %q error (proceeding): %v", name, killErr)
		}
		killed++
	}

	if killed == 0 {
		return 0, nil
	}

	waitForTmuxSessionsGone(ctx, lister, prefix, logger)

	return killed, nil
}

func waitForTmuxSessionsGone(ctx context.Context, lister TmuxSessionLister, prefix string, logger *log.Logger) {
	deadline := time.Now().Add(tmuxPollCeiling)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			orphanLog(logger, "SweepOrphanTmuxSessions: context cancelled during poll; proceeding")
			return
		case <-time.After(tmuxPollInterval):
		}

		remaining, listErr := lister.ListTmuxSessions(ctx)
		if listErr != nil {
			orphanLog(logger, "SweepOrphanTmuxSessions: re-list during exit poll failed (retrying): %v", listErr)
			continue
		}
		if !anyNameHasPrefix(remaining, prefix) {
			orphanLog(logger, "SweepOrphanTmuxSessions: all matching sessions exited after kill")
			return
		}
	}
}

func anyNameHasPrefix(names []string, prefix string) bool {
	for _, name := range names {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

// HandlerProcessLister enumerates candidate orphan handler subprocesses.
// Implementations query the OS process table; tests inject a deterministic fake.
//
// ListOrphanHandlerPIDs returns the PIDs of processes that:
//   - have been re-parented to init (parent PID == 1), AND
//   - carry the HARMONIK_PROJECT_HASH env var matching projectHash.
//
// Spec ref: process-lifecycle.md §4.2 PL-006 — "The daemon MUST identify
// processes that have been re-parented to init (parent pid 1) whose provenance
// marker per PL-006a matches this project's project hash."
// "Enumeration MUST cover BOTH (i) handler subprocesses AND (ii) br subprocesses."
type HandlerProcessLister interface {
	ListOrphanHandlerPIDs(ctx context.Context, projectHash core.ProjectHash) ([]int, error)
}

// OSHandlerProcessLister is the production HandlerProcessLister.
// On Linux it reads /proc/<pid>/environ for provenance-marker matching.
// On darwin it enumerates processes via `ps -eo pid,ppid` and attempts a
// best-effort PGID match (OQ-PL-008 tracks the darwin limitation).
//
// Spec ref: process-lifecycle.md §4.2 PL-006a — "readable via /proc/<pid>/environ
// on Linux."
type OSHandlerProcessLister struct{}

// ListOrphanHandlerPIDs implements HandlerProcessLister.
//
// Implementation strategy:
//  1. Use `ps -eo pid,ppid` to enumerate processes with PPID==1.
//  2. For each, read /proc/<pid>/environ (Linux) or fall back to the
//     PGID check (darwin, OQ-PL-008).
//  3. Return PIDs whose provenance marker matches projectHash.
func (OSHandlerProcessLister) ListOrphanHandlerPIDs(ctx context.Context, projectHash core.ProjectHash) ([]int, error) {
	out, err := exec.CommandContext(ctx, "ps", "-eo", "pid,ppid").Output()
	if err != nil {
		return nil, fmt.Errorf("lifecycle: OSHandlerProcessLister: ps: %w", err)
	}

	lines := strings.Split(string(out), "\n")
	candidates := make([]int, 0, len(lines))
	for _, line := range lines[1:] { // skip header
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		pidStr, ppidStr := fields[0], fields[1]
		ppid, err := strconv.Atoi(ppidStr)
		if err != nil || ppid != 1 {
			continue
		}
		pid, err := strconv.Atoi(pidStr)
		if err != nil {
			continue
		}
		candidates = append(candidates, pid)
	}

	matched := make([]int, 0, len(candidates))
	for _, pid := range candidates {
		env, err := ReadProcessEnviron(pid)
		if err != nil {
			continue
		}
		if !MatchesProvenanceMarker(env, projectHash) {
			continue
		}
		args, cmdErr := ReadProcessCmdlineArgs(pid)
		if cmdErr == nil && IsRelayGrandchild(args) {
			continue
		}
		matched = append(matched, pid)
	}
	return matched, nil
}

var handlerSweepGracePeriod = 5 * time.Second

var handlerSweepPollInterval = 100 * time.Millisecond

// SweepOrphanHandlers enumerates handler subprocesses re-parented to init
// (PPID==1) whose provenance marker matches projectHash, sends SIGTERM, waits
// up to 5 s, then SIGKILL. Returns the count of processes killed.
//
// If lister is nil, OSHandlerProcessLister is used.
// If logger is nil, log messages are discarded.
//
// Spec ref: process-lifecycle.md §4.2 PL-006 — "Subprocess cleanup. The daemon
// MUST identify processes that have been re-parented to init (parent pid 1)
// whose provenance marker per PL-006a matches this project's project hash, and
// kill them via SIGTERM followed by SIGKILL after a bounded 5-second interval
// consistent with handler-contract.md §4.4 HC-018."
func SweepOrphanHandlers(
	ctx context.Context,
	projectHash core.ProjectHash,
	lister HandlerProcessLister,
	logger *log.Logger,
) (killed int, err error) {
	if lister == nil {
		lister = OSHandlerProcessLister{}
	}

	pids, err := lister.ListOrphanHandlerPIDs(ctx, projectHash)
	if err != nil {
		return 0, fmt.Errorf("lifecycle: SweepOrphanHandlers: enumerate: %w", err)
	}
	if len(pids) == 0 {
		return 0, nil
	}

	orphanLog(logger, "SweepOrphanHandlers: found %d orphan handler process(es): %v", len(pids), pids)

	for _, pid := range pids {
		if sigErr := syscall.Kill(pid, syscall.SIGTERM); sigErr != nil {
			orphanLog(logger, "SweepOrphanHandlers: SIGTERM pid %d: %v (may have already exited)", pid, sigErr)
		} else {
			orphanLog(logger, "SweepOrphanHandlers: sent SIGTERM to pid %d", pid)
		}
	}

	deadline := time.Now().Add(handlerSweepGracePeriod)
	alive := make(map[int]bool, len(pids))
	for _, pid := range pids {
		alive[pid] = true
	}

	for time.Now().Before(deadline) && len(alive) > 0 {
		for pid := range alive {
			if !orphanSweepIsPidLive(pid) {
				delete(alive, pid)
				orphanLog(logger, "SweepOrphanHandlers: pid %d exited after SIGTERM", pid)
			}
		}
		if len(alive) == 0 {
			break
		}
		select {
		case <-ctx.Done():
			orphanLog(logger, "SweepOrphanHandlers: context cancelled; escalating to SIGKILL")
		case <-time.After(handlerSweepPollInterval):
		}
		if ctx.Err() != nil {
			break
		}
	}

	if len(alive) > 0 {
		fresh, freshErr := lister.ListOrphanHandlerPIDs(ctx, projectHash)
		if freshErr != nil {
			orphanLog(logger, "SweepOrphanHandlers: re-enumerate before SIGKILL failed: %v; skipping SIGKILL (PID-reuse guard)", freshErr)
			alive = map[int]bool{}
		} else {
			alive = reverifyCandidatePIDs(alive, fresh)
		}
	}
	for pid := range alive {
		orphanLog(logger, "SweepOrphanHandlers: pid %d survived SIGTERM grace; sending SIGKILL", pid)
		if sigErr := syscall.Kill(pid, syscall.SIGKILL); sigErr != nil {
			orphanLog(logger, "SweepOrphanHandlers: SIGKILL pid %d: %v", pid, sigErr)
		}
	}

	for _, pid := range pids {
		if !orphanSweepIsPidLive(pid) {
			killed++
		}
	}

	return killed, nil
}

// IntentGCLedger is the read surface consumed by [GCRetiredIntents] to check
// whether the operation described by a stale intent file has already landed in
// the Beads ledger.
//
// The production implementation is *brcli.Adapter (satisfies ShowBead via
// brcli.Adapter.ShowBead). Tests inject a deterministic fake.
type IntentGCLedger interface {
	// ShowBead returns the current BeadRecord for the given bead ID.
	ShowBead(ctx context.Context, id core.BeadID) (core.BeadRecord, error)
}

const gcRetiredIntentsMaxScan = 250

// GCRetiredIntentsResult reports the outcome of a [GCRetiredIntents] pass.
type GCRetiredIntentsResult struct {
	// Removed is the count of stale intent files deleted because the bead
	// has already reached its IntendedPostState (the op landed in a prior run;
	// the file is a leftover from a crash between BI-030 step 5 success and
	// step 6 delete).
	Removed int
	// Retained is the count of stale intent files left on disk because the
	// bead has NOT yet reached its IntendedPostState — the Cat 3a detector
	// may need to re-drive the br operation.
	Retained int
	// Skipped is the count of stale intent files that were not queried this
	// pass because the per-boot cap (gcRetiredIntentsMaxScan) was reached.
	// These files remain on disk and will be processed on subsequent boots.
	Skipped int
	// RedriveCount is the count of stale intent files where the bead was at
	// the pre-state and the br write was successfully re-issued and the intent
	// file was deleted. Zero when IntentRedriveWriter is nil.
	//
	// Spec ref: specs/beads-integration.md §4.10 BI-031 step 4 (4a success).
	// Bead ref: hk-aev8t.
	RedriveCount int
}

// IntentRedriveWriter is the write surface consumed by
// [GCRetiredIntentsWithRedrive] to re-issue a stale terminal-transition write
// at daemon startup (BI-031 step 4). It is satisfied by *brcli.Adapter in
// production (Adapter.ReissueTerminalTransition) and by a fake in tests.
//
// Spec ref: specs/beads-integration.md §4.10 BI-031 step 4.
// Bead ref: hk-aev8t (G3 — step-4 re-drive missing).
type IntentRedriveWriter interface {
	// ReissueTerminalTransition re-issues the br write from entry using the
	// same idempotency_key. On success the intent file is deleted (BI-031 step
	// 6); on failure the intent file is retained for Cat 3a routing.
	ReissueTerminalTransition(ctx context.Context, intentLogDir string, cfg brcli.TimeoutConfig, entry core.IntentLogEntry) error
}

// GCRetiredIntentsConfig holds the parameters for [GCRetiredIntentsWithRedrive].
//
// Bead ref: hk-aev8t.
type GCRetiredIntentsConfig struct {
	// ProjectDir is the harmonik project root (parent of .harmonik/).
	ProjectDir string

	// DaemonStartTime is the daemon's startup wall-clock timestamp. Only intent
	// files with mtime strictly before this value are considered stale.
	DaemonStartTime time.Time

	// Ledger is the read surface for ShowBead status checks. REQUIRED (non-nil).
	Ledger IntentGCLedger

	// RedriveWriter, when non-nil, enables BI-031 step-4 re-drive: for stale
	// intent files where the bead is still at the op's pre-state, the write is
	// re-issued via ReissueTerminalTransition instead of being retained for
	// Cat 3a. When nil, non-landed files are retained (the legacy behavior).
	RedriveWriter IntentRedriveWriter

	// BrTimeoutCfg is the BI-025c timeout configuration forwarded to
	// RedriveWriter.ReissueTerminalTransition. Zero value → BI-025c defaults.
	BrTimeoutCfg brcli.TimeoutConfig

	// Logger receives diagnostic messages. Nil → silent.
	Logger *log.Logger
}

// GCRetiredIntentsWithRedrive extends [GCRetiredIntents] with BI-031 step-4
// re-drive: when cfg.RedriveWriter is non-nil and a stale intent file's bead
// is still at the pre-state for the recorded op, the br write is re-issued
// rather than retained for Cat 3a. Successfully re-driven files are counted in
// [GCRetiredIntentsResult.RedriveCount].
//
// When cfg.RedriveWriter is nil the behavior is identical to [GCRetiredIntents].
//
// Spec ref: specs/beads-integration.md §4.10 BI-031 step 4.
// Bead ref: hk-aev8t.
func GCRetiredIntentsWithRedrive(ctx context.Context, cfg GCRetiredIntentsConfig) (GCRetiredIntentsResult, error) {
	var result GCRetiredIntentsResult

	intentsDir := filepath.Join(cfg.ProjectDir, ".harmonik", "beads-intents")
	entries, err := os.ReadDir(intentsDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return result, nil
		}
		return result, fmt.Errorf("lifecycle: GCRetiredIntentsWithRedrive: ReadDir %q: %w", intentsDir, err)
	}

	scanned := 0
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".json") {
			continue
		}
		if strings.Contains(name, ".tmp-") {
			continue
		}

		info, infoErr := entry.Info()
		if infoErr != nil {
			orphanLog(cfg.Logger, "GCRetiredIntentsWithRedrive: skipping %q: stat error: %v", name, infoErr)
			result.Retained++
			continue
		}
		if !info.ModTime().Before(cfg.DaemonStartTime) {
			continue
		}

		if scanned >= gcRetiredIntentsMaxScan {
			result.Skipped++
			continue
		}
		scanned++

		intentPath := filepath.Join(intentsDir, name)
		intentEntry, readErr := core.ReadIntentLogEntry(intentPath)
		if readErr != nil {
			orphanLog(cfg.Logger, "GCRetiredIntentsWithRedrive: %q unreadable (%v); retaining for Cat 3a", name, readErr)
			result.Retained++
			continue
		}

		record, showErr := cfg.Ledger.ShowBead(ctx, intentEntry.BeadID)
		if showErr != nil {
			if errors.Is(showErr, brcli.ErrBeadNotFound) {
				if removeErr := os.Remove(intentPath); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
					orphanLog(cfg.Logger, "GCRetiredIntentsWithRedrive: remove purged-bead intent %q failed (%v); retaining", name, removeErr)
					result.Retained++
					continue
				}
				orphanLog(cfg.Logger, "GCRetiredIntentsWithRedrive: removed intent %q for purged bead %s", name, intentEntry.BeadID)
				result.Removed++
				continue
			}
			orphanLog(cfg.Logger, "GCRetiredIntentsWithRedrive: ShowBead(%s) failed (%v); retaining intent for Cat 3a", intentEntry.BeadID, showErr)
			result.Retained++
			continue
		}

		if gcIntentOpLanded(intentEntry.Op, record.Status, intentEntry.IntendedPostState) {
			if removeErr := os.Remove(intentPath); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
				orphanLog(cfg.Logger, "GCRetiredIntentsWithRedrive: remove %q failed (%v); retaining", name, removeErr)
				result.Retained++
				continue
			}
			orphanLog(cfg.Logger, "GCRetiredIntentsWithRedrive: removed retired intent %q (bead %s op=%s landed, now %s)",
				name, intentEntry.BeadID, intentEntry.Op, record.Status)
			result.Removed++
			continue
		}

		if cfg.RedriveWriter == nil {
			orphanLog(cfg.Logger, "GCRetiredIntentsWithRedrive: bead %s status=%s (want op=%s to land); retaining for Cat 3a (no RedriveWriter)", intentEntry.BeadID, record.Status, intentEntry.Op)
			result.Retained++
			continue
		}

		preState, knownOp := gcIntentOpPreState(intentEntry.Op)
		if !knownOp {
			orphanLog(cfg.Logger, "GCRetiredIntentsWithRedrive: bead %s unknown op %q; retaining for Cat 3a", intentEntry.BeadID, intentEntry.Op)
			result.Retained++
			continue
		}

		if record.Status != preState {
			orphanLog(cfg.Logger, "GCRetiredIntentsWithRedrive: bead %s status=%s is neither pre-state (%s) nor post-state (%s) for op=%s; retaining for Cat 3a (divergence)", intentEntry.BeadID, record.Status, preState, intentEntry.IntendedPostState, intentEntry.Op)
			result.Retained++
			continue
		}

		orphanLog(cfg.Logger, "GCRetiredIntentsWithRedrive: bead %s at pre-state %s for op=%s; re-driving via BI-031 step 4", intentEntry.BeadID, preState, intentEntry.Op)
		redriveErr := cfg.RedriveWriter.ReissueTerminalTransition(ctx, intentsDir, cfg.BrTimeoutCfg, intentEntry)
		if redriveErr == nil {
			orphanLog(cfg.Logger, "GCRetiredIntentsWithRedrive: re-drive succeeded for bead %s op=%s", intentEntry.BeadID, intentEntry.Op)
			result.RedriveCount++
		} else {
			orphanLog(cfg.Logger, "GCRetiredIntentsWithRedrive: re-drive failed for bead %s op=%s (%v); retaining for Cat 3a", intentEntry.BeadID, intentEntry.Op, redriveErr)
			result.Retained++
		}
	}

	if result.Skipped > 0 {
		orphanLog(cfg.Logger, "GCRetiredIntentsWithRedrive: cap reached (%d); deferred %d stale files to next boot",
			gcRetiredIntentsMaxScan, result.Skipped)
	}

	if result.Removed > 0 {
		fsyncDirBestEffort(intentsDir, cfg.Logger, "GCRetiredIntentsWithRedrive")
	}

	return result, nil
}

// GCRetiredIntents is the backward-compatible entry point for intent-log GC.
// It scans projectDir/.harmonik/beads-intents/ for stale intent files and
// deletes those whose op has already landed (step 3). Non-landed files are
// retained for Cat 3a — no step-4 re-drive is attempted (RedriveWriter = nil).
//
// Callers that want BI-031 step-4 re-drive SHOULD use [GCRetiredIntentsWithRedrive]
// with a non-nil RedriveWriter instead.
func GCRetiredIntents(
	ctx context.Context,
	projectDir string,
	daemonStartTime time.Time,
	ledger IntentGCLedger,
	logger *log.Logger,
) (GCRetiredIntentsResult, error) {
	return GCRetiredIntentsWithRedrive(ctx, GCRetiredIntentsConfig{
		ProjectDir:      projectDir,
		DaemonStartTime: daemonStartTime,
		Ledger:          ledger,
		Logger:          logger,
	})
}

func gcIntentOpPreState(op core.TerminalOp) (core.CoarseStatus, bool) {
	switch op {
	case core.TerminalOpClaim:
		return core.CoarseStatusOpen, true
	case core.TerminalOpClose:
		return core.CoarseStatusInProgress, true
	case core.TerminalOpReopen:
		return core.CoarseStatusClosed, true
	case core.TerminalOpReset:
		return core.CoarseStatusInProgress, true
	default:
		return "", false
	}
}

func gcIntentOpLanded(op core.TerminalOp, currentStatus, intendedPostState core.CoarseStatus) bool {
	if currentStatus == intendedPostState {
		return true
	}
	switch op {
	case core.TerminalOpClaim:
		return currentStatus != core.CoarseStatusOpen
	case core.TerminalOpClose:
		return currentStatus == core.CoarseStatusTombstone || currentStatus == core.CoarseStatusOpen
	case core.TerminalOpReopen, core.TerminalOpReset:
		return currentStatus == core.CoarseStatusInProgress ||
			currentStatus == core.CoarseStatusTombstone
	}
	return false
}

// SweepReconciliationLocksResult is the result of [SweepStaleReconciliationLocks].
//
// Spec ref: specs/reconciliation/spec.md §4.1 RC-002b — stale lock files are
// removed unconditionally; their verdict-executed state discriminates downstream routing.
type SweepReconciliationLocksResult struct {
	// Removed is the count of stale lock files unlinked.
	Removed int

	// Cat3bRunIDs contains the target_run_id values from stale lock files that
	// did NOT carry the "Harmonik-Verdict-Executed: true" line. Per RC-002b,
	// these runs must be routed through Cat 3b (verdict-emitted-but-unexecuted)
	// on the next reconciliation pass (§8.5).
	//
	// Spec ref: specs/reconciliation/spec.md §4.1 RC-002b.
	Cat3bRunIDs []string
}

// SweepStaleReconciliationLocks enumerates .harmonik/reconciliation-locks/*.lock,
// probes each file with flock(LOCK_EX|LOCK_NB), and removes files that are both
// unlocked and whose recorded creator_pid does not respond to kill(pid, 0).
//
// For each removed file, it checks whether the lock file carries the
// "Harmonik-Verdict-Executed: true" line (written by the verdict-executor per
// RC-002b just before releasing the lock). Stale locks WITHOUT this line are
// returned in [SweepReconciliationLocksResult.Cat3bRunIDs] so the daemon can
// route those runs through Cat 3b on the next reconciliation pass.
//
// Returns a zero-value result (no error) if the reconciliation-locks directory
// does not exist.
//
// Spec ref: process-lifecycle.md §4.2 PL-006 — "Stale reconciliation locks.
// The daemon MUST enumerate .harmonik/reconciliation-locks/*.lock. For each
// lock file, the daemon MUST attempt flock(LOCK_EX|LOCK_NB) to determine
// liveness (kernel auto-releases the advisory lock on the prior lock-holder's
// termination per PL-002a discipline); a successful acquisition followed by
// flock(LOCK_UN) confirms no live process holds the lock. Stale lock files
// (acquirable + the recorded creator-PID does NOT respond to kill(pid, 0)) MUST
// be removed via unlink followed by fsync(parent_directory_fd). The sweep MUST
// NOT racily unlink a lock file currently being acquired by another daemon
// process — the flock(LOCK_EX|LOCK_NB) probe is the serialization point; if
// EWOULDBLOCK is observed the lock is in active use and MUST NOT be removed."
// Also: specs/reconciliation/spec.md §4.1 RC-002b — "stale lock with verdict-executed
// trailer: delete, no re-classification; stale lock without: delete and route Cat 3b."
func SweepStaleReconciliationLocks(projectDir string, logger *log.Logger) (SweepReconciliationLocksResult, error) {
	var result SweepReconciliationLocksResult

	lockDir := filepath.Join(projectDir, ".harmonik", "reconciliation-locks")

	entries, err := os.ReadDir(lockDir)
	if err != nil {
		if os.IsNotExist(err) {
			return result, nil
		}
		return result, fmt.Errorf("lifecycle: SweepStaleReconciliationLocks: ReadDir: %w", err)
	}

	var lastRemoveErr error
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".lock") {
			continue
		}
		lockPath := filepath.Join(lockDir, name)

		held, stale, probeErr := reconLockProbeStale(lockPath)
		if probeErr != nil {
			orphanLog(logger, "SweepStaleReconciliationLocks: probe %q: %v (skipping)", name, probeErr)
			continue
		}
		if !stale {
			orphanLog(logger, "SweepStaleReconciliationLocks: %q is active (EWOULDBLOCK or live PID); skipping", name)
			continue
		}

		runID, hasVerdictExecuted, metaErr := reconLockReadMeta(lockPath)
		if metaErr != nil {
			orphanLog(logger, "SweepStaleReconciliationLocks: read meta %q: %v (treating as no-verdict)", name, metaErr)
			hasVerdictExecuted = false
			runID = strings.TrimSuffix(name, ".lock")
		}

		removeErr := reconLockUnlinkAndFsync(lockPath, lockDir, logger)
		if closeErr := held.Close(); closeErr != nil {
			orphanLog(logger, "SweepStaleReconciliationLocks: close held lock %q after unlink: %v", name, closeErr)
		}
		if removeErr != nil {
			orphanLog(logger, "SweepStaleReconciliationLocks: remove %q: %v", name, removeErr)
			lastRemoveErr = removeErr
			continue
		}
		orphanLog(logger, "SweepStaleReconciliationLocks: removed stale lock %q (verdict_executed=%v)", name, hasVerdictExecuted)
		result.Removed++

		if !hasVerdictExecuted {
			result.Cat3bRunIDs = append(result.Cat3bRunIDs, runID)
			orphanLog(logger, "SweepStaleReconciliationLocks: run %q queued for Cat 3b routing (no verdict-executed)", runID)
		}
	}

	if lastRemoveErr != nil {
		return result, fmt.Errorf("lifecycle: SweepStaleReconciliationLocks: some removals failed (last: %w)", lastRemoveErr)
	}
	return result, nil
}

func reconLockProbeStale(lockPath string) (held *os.File, stale bool, err error) {
	//nolint:gosec // G304: path is constructed from projectDir + .harmonik/reconciliation-locks/ + entry name, not user input
	f, err := os.OpenFile(lockPath, os.O_RDWR, 0o600)
	if err != nil {
		return nil, false, fmt.Errorf("reconLockProbeStale: open %q: %w", lockPath, err)
	}

	flockErr := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if flockErr != nil {
		if closeErr := f.Close(); closeErr != nil {
			slog.WarnContext(context.Background(), "lifecycle: reconLockProbeStale: close probe fd after flock failure", "err", closeErr, "path", lockPath)
		}
		if errors.Is(flockErr, syscall.EWOULDBLOCK) || errors.Is(flockErr, syscall.EAGAIN) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("reconLockProbeStale: flock %q: %w", lockPath, flockErr)
	}

	pid, parseErr := reconLockReadCreatorPID(f)
	if parseErr != nil {
		if closeErr := f.Close(); closeErr != nil {
			slog.WarnContext(context.Background(), "lifecycle: reconLockProbeStale: close probe fd after parse failure", "err", closeErr, "path", lockPath)
		}
		return nil, false, fmt.Errorf("reconLockProbeStale: %w", parseErr)
	}

	if orphanSweepIsPidLive(pid) {
		if closeErr := f.Close(); closeErr != nil {
			slog.WarnContext(context.Background(), "lifecycle: reconLockProbeStale: close probe fd (live creator)", "err", closeErr, "path", lockPath)
		}
		return nil, false, nil
	}

	return f, true, nil
}

func reconLockReadCreatorPID(f *os.File) (int, error) {
	if _, err := f.Seek(0, 0); err != nil {
		return 0, fmt.Errorf("reconLockReadCreatorPID: seek: %w", err)
	}
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		const prefix = "creator_pid="
		if strings.HasPrefix(line, prefix) {
			pidStr := strings.TrimPrefix(line, prefix)
			pid, err := strconv.Atoi(pidStr)
			if err != nil {
				return 0, fmt.Errorf("reconLockReadCreatorPID: parse %q: %w", pidStr, err)
			}
			return pid, nil
		}
	}
	return 0, fmt.Errorf("reconLockReadCreatorPID: creator_pid line not found in %q", f.Name())
}

func reconLockReadMeta(lockPath string) (runID string, hasVerdictExecuted bool, err error) {
	//nolint:gosec // G304: lockPath is constructed from projectDir + known relative path, not user input
	f, err := os.Open(lockPath)
	if err != nil {
		return "", false, fmt.Errorf("reconLockReadMeta: open %q: %w", lockPath, err)
	}
	defer func() {
		if closeErr := f.Close(); closeErr != nil {
			slog.WarnContext(context.Background(), "lifecycle: reconLockReadMeta: close lock fd", "err", closeErr, "path", lockPath)
		}
	}()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "run_id=") {
			runID = strings.TrimPrefix(line, "run_id=")
		}
		if line == "Harmonik-Verdict-Executed: true" {
			hasVerdictExecuted = true
		}
	}
	if runID == "" {
		runID = strings.TrimSuffix(filepath.Base(lockPath), ".lock")
	}
	return runID, hasVerdictExecuted, nil
}

func reconLockUnlinkAndFsync(lockPath, lockDir string, logger *log.Logger) error {
	if err := os.Remove(lockPath); err != nil {
		return fmt.Errorf("reconLockUnlinkAndFsync: Remove %q: %w", lockPath, err)
	}
	// fsync the parent directory so the unlink is durable.
	//nolint:gosec // G304: lockDir is constructed from projectDir + known relative path, not user input
	dirFd, err := os.Open(lockDir)
	if err != nil {
		orphanLog(logger, "reconLockUnlinkAndFsync: open parent dir for fsync: %v (proceeding)", err)
		return nil
	}
	defer func() {
		if closeErr := dirFd.Close(); closeErr != nil {
			orphanLog(logger, "reconLockUnlinkAndFsync: close parent dir after fsync: %v", closeErr)
		}
	}()
	if syncErr := dirFd.Sync(); syncErr != nil {
		orphanLog(logger, "reconLockUnlinkAndFsync: fsync parent dir: %v (non-fatal)", syncErr)
	}
	return nil
}

// EnumerateStaleIntents counts intent files under .harmonik/beads-intents/ whose
// mtime is before daemonStartTime. The files are NOT removed — they are left on
// disk for the reconciliation Cat 3a detector (RC-013). Returns 0 (no error)
// if the directory does not exist.
//
// Spec ref: process-lifecycle.md §4.2 PL-006 — "Stale intent files. The daemon
// MUST enumerate .harmonik/beads-intents/ for entries older than the current
// daemon's start time. Stale entries MUST be LEFT on disk for classification by
// the reconciliation Cat 3a detector per [reconciliation/spec.md §4.3 RC-013]
// during §PL-005 step 8; the orphan sweep itself MUST NOT invoke reconciliation
// detectors."
func EnumerateStaleIntents(projectDir string, daemonStartTime time.Time) (count int, err error) {
	intentsDir := filepath.Join(projectDir, ".harmonik", "beads-intents")
	entries, err := os.ReadDir(intentsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("lifecycle: EnumerateStaleIntents: ReadDir: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		info, infoErr := entry.Info()
		if infoErr != nil {
			continue
		}
		if info.ModTime().Before(daemonStartTime) {
			count++
		}
	}
	return count, nil
}

func fsyncDirBestEffort(dir string, logger *log.Logger, caller string) {
	dirFd, openErr := os.Open(dir)
	if openErr != nil {
		orphanLog(logger, "%s: open %q for fsync failed (proceeding): %v", caller, dir, openErr)
		return
	}
	if syncErr := dirFd.Sync(); syncErr != nil {
		orphanLog(logger, "%s: fsync %q failed (proceeding): %v", caller, dir, syncErr)
	}
	if closeErr := dirFd.Close(); closeErr != nil {
		orphanLog(logger, "%s: close %q after fsync failed: %v", caller, dir, closeErr)
	}
}
