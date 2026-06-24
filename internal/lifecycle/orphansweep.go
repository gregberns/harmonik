package lifecycle

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"log"
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

// ──────────────────────────────────────────────────────────────────────────────
// (a) Tmux session sweep
// ──────────────────────────────────────────────────────────────────────────────

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
// If tmux is not installed or no sessions exist, the command may exit non-zero;
// those cases are treated as an empty list (not an error) to keep the sweep
// non-fatal on systems without tmux.
type OSTmuxSessionLister struct{}

// ListTmuxSessions implements TmuxSessionLister.
func (OSTmuxSessionLister) ListTmuxSessions(ctx context.Context) ([]string, error) {
	//nolint:gosec // G204: arguments are hard-coded constants, not user input
	out, err := exec.CommandContext(ctx, "tmux", "list-sessions", "-F", "#{session_name}").Output()
	if err != nil {
		// tmux exits non-zero when there are no sessions or tmux is not running.
		// Return empty list rather than propagating a hard error.
		return nil, nil //nolint:nilerr // intentional: no-tmux / no-sessions is not an error
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
	//nolint:gosec // G204: sessionName is a validated harmonik-<hash>- prefixed name, not raw user input
	out, err := exec.CommandContext(ctx, "tmux", "kill-session", "-t", sessionName).CombinedOutput()
	if err != nil {
		return fmt.Errorf("lifecycle: KillTmuxSession %q: %w (output: %s)", sessionName, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// tmuxPollInterval is the cadence at which SweepOrphanTmuxSessions polls for
// process exit after kill-session. Tests shorten this without changing the
// production call-site signature.
//
// Spec ref: process-lifecycle.md §4.2 PL-006 — "poll for underlying process
// exit at a 100 ms cadence up to a 2-second ceiling (configurable per OQ-PL-002)."
var tmuxPollInterval = 100 * time.Millisecond

// tmuxPollCeiling is the maximum time SweepOrphanTmuxSessions waits after
// kill-session before proceeding. Configurable per OQ-PL-002.
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
			// Non-fatal: a session that has already gone is fine.
		}
		killed++
	}

	if killed == 0 {
		return 0, nil
	}

	// Poll for process exit at 100 ms cadence up to the ceiling.
	// The sweep does NOT track individual session PIDs here — the polling is
	// best-effort after the kill-session commands have been sent.
	deadline := time.Now().Add(tmuxPollCeiling)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			orphanLog(logger, "SweepOrphanTmuxSessions: context cancelled during poll; proceeding")
			return killed, nil
		case <-time.After(tmuxPollInterval):
		}

		// Re-list to check whether our target sessions are still present.
		remaining, listErr := lister.ListTmuxSessions(ctx)
		if listErr != nil {
			break // list failed; treat as done
		}
		anyRemain := false
		for _, name := range remaining {
			if strings.HasPrefix(name, prefix) {
				anyRemain = true
				break
			}
		}
		if !anyRemain {
			orphanLog(logger, "SweepOrphanTmuxSessions: all matching sessions exited after kill")
			break
		}
	}

	return killed, nil
}

// ──────────────────────────────────────────────────────────────────────────────
// (c) Handler subprocess sweep
// ──────────────────────────────────────────────────────────────────────────────

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
	//nolint:gosec // G204: arguments are hard-coded constants, not user input
	out, err := exec.CommandContext(ctx, "ps", "-eo", "pid,ppid").Output()
	if err != nil {
		return nil, fmt.Errorf("lifecycle: OSHandlerProcessLister: ps: %w", err)
	}

	var candidates []int
	lines := strings.Split(string(out), "\n")
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

	var matched []int
	for _, pid := range candidates {
		env, err := ReadProcessEnviron(pid)
		if err != nil {
			// /proc not available (darwin) or permission denied: skip.
			continue
		}
		if !MatchesProvenanceMarker(env, projectHash) {
			continue
		}
		// PL-017a(b): relay grandchildren (harmonik hook-relay ...) are spawned by
		// agent subprocesses and MUST NOT be targeted by the orphan-sweep.  They
		// exit on their own when the agent completes its hook invocation; survivors
		// (parent agent died mid-invocation) are reaped by OS init-reparenting at
		// daemon death.  Identify them by argv[1] == "hook-relay" via /proc/cmdline.
		args, cmdErr := ReadProcessCmdlineArgs(pid)
		if cmdErr == nil && IsRelayGrandchild(args) {
			continue
		}
		matched = append(matched, pid)
	}
	return matched, nil
}

// handlerSweepGracePeriod is the time SweepOrphanHandlers waits after SIGTERM
// before escalating to SIGKILL. Matches HC-018's 5-second cleanup bound.
// Tests may shorten this without changing the production call-site.
//
// Spec ref: process-lifecycle.md §4.2 PL-006 — "SIGTERM followed by SIGKILL
// after a bounded 5-second interval consistent with handler-contract.md §4.4 HC-018."
var handlerSweepGracePeriod = 5 * time.Second

// handlerSweepPollInterval is the cadence at which SweepOrphanHandlers polls
// during the grace period.
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

	// Phase 1: SIGTERM all candidates.
	for _, pid := range pids {
		if sigErr := syscall.Kill(pid, syscall.SIGTERM); sigErr != nil {
			orphanLog(logger, "SweepOrphanHandlers: SIGTERM pid %d: %v (may have already exited)", pid, sigErr)
		} else {
			orphanLog(logger, "SweepOrphanHandlers: sent SIGTERM to pid %d", pid)
		}
	}

	// Phase 2: wait up to 5 s polling at 100 ms.
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

	// Phase 3: SIGKILL any still-alive processes.
	for pid := range alive {
		orphanLog(logger, "SweepOrphanHandlers: pid %d survived SIGTERM grace; sending SIGKILL", pid)
		if sigErr := syscall.Kill(pid, syscall.SIGKILL); sigErr != nil {
			orphanLog(logger, "SweepOrphanHandlers: SIGKILL pid %d: %v", pid, sigErr)
		}
	}

	// Count how many were successfully killed (not still alive after SIGKILL).
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

// gcRetiredIntentsMaxScan is the maximum number of stale intent files that
// GCRetiredIntents will query via ShowBead in a single daemon-startup pass.
// Each ShowBead call shells out to `br show` (~0.24 s); capping prevents a
// startup hang when hundreds of files accumulate (hk-hf9i8).  Files beyond
// the cap are left on disk and picked up on subsequent boots.
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
				// Bead was purged from the ledger; its terminal op is moot — nothing to reconcile.
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
			// Step 3: op has landed — delete the leftover intent file.
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

		// Op has NOT landed.  Decide between step 4 (pre-state: re-drive) and
		// step 5 (diverged: neither pre-state nor post-state → Cat 3a retain).
		if cfg.RedriveWriter == nil {
			// No write surface — legacy behavior: retain for Cat 3a.
			orphanLog(cfg.Logger, "GCRetiredIntentsWithRedrive: bead %s status=%s (want op=%s to land); retaining for Cat 3a (no RedriveWriter)", intentEntry.BeadID, record.Status, intentEntry.Op)
			result.Retained++
			continue
		}

		preState, knownOp := gcIntentOpPreState(intentEntry.Op)
		if !knownOp {
			// Unknown op — cannot determine pre-state; retain conservatively.
			orphanLog(cfg.Logger, "GCRetiredIntentsWithRedrive: bead %s unknown op %q; retaining for Cat 3a", intentEntry.BeadID, intentEntry.Op)
			result.Retained++
			continue
		}

		if record.Status != preState {
			// Step 5: bead is neither at pre-state nor post-state — torn write /
			// external mutation.  Route to Cat 3a reconciliation.
			orphanLog(cfg.Logger, "GCRetiredIntentsWithRedrive: bead %s status=%s is neither pre-state (%s) nor post-state (%s) for op=%s; retaining for Cat 3a (divergence)", intentEntry.BeadID, record.Status, preState, intentEntry.IntendedPostState, intentEntry.Op)
			result.Retained++
			continue
		}

		// Step 4: bead is at pre-state — re-issue the br write.
		orphanLog(cfg.Logger, "GCRetiredIntentsWithRedrive: bead %s at pre-state %s for op=%s; re-driving via BI-031 step 4", intentEntry.BeadID, preState, intentEntry.Op)
		redriveErr := cfg.RedriveWriter.ReissueTerminalTransition(ctx, intentsDir, cfg.BrTimeoutCfg, intentEntry)
		if redriveErr == nil {
			// 4a success: intent file deleted by ReissueTerminalTransition.
			orphanLog(cfg.Logger, "GCRetiredIntentsWithRedrive: re-drive succeeded for bead %s op=%s", intentEntry.BeadID, intentEntry.Op)
			result.RedriveCount++
		} else {
			// 4b unconfirmed / 4d unavailable / 4e schema mismatch / 4f other:
			// intent file retained; Cat 3a auto-resolver will handle it.
			orphanLog(cfg.Logger, "GCRetiredIntentsWithRedrive: re-drive failed for bead %s op=%s (%v); retaining for Cat 3a", intentEntry.BeadID, intentEntry.Op, redriveErr)
			result.Retained++
		}
	}

	if result.Skipped > 0 {
		orphanLog(cfg.Logger, "GCRetiredIntentsWithRedrive: cap reached (%d); deferred %d stale files to next boot",
			gcRetiredIntentsMaxScan, result.Skipped)
	}

	// fsync the parent directory once after all removals.
	if result.Removed > 0 {
		if dirFd, openErr := os.Open(intentsDir); openErr == nil {
			_ = dirFd.Sync()  //nolint:errcheck
			_ = dirFd.Close() //nolint:errcheck
		}
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

// gcIntentOpPreState returns the Beads status that must hold BEFORE the op
// can be issued (BI-031 step 4 pre-state check).
//
// Returns (preState, true) for known ops; (empty, false) for unknown ops.
// The caller MUST treat an unknown op as ambiguous and retain the intent file.
//
// Reset note: reset returns (in_progress, true) but step-4 re-drive is never
// reached for reset in practice. gcIntentOpLanded treats in_progress as
// "landed" for reset (the bead was re-opened and then claimed), so the only
// non-landed, non-post-state reachable status for reset is closed — which
// diverges from the returned pre-state (in_progress) and routes to Cat 3a.
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

// gcIntentOpLanded reports whether the op described by an intent file has
// definitely completed, given the bead's current status.
//
// "Landed" means the bead is in the IntendedPostState OR has advanced past it
// in the lifecycle — either way re-driving the op is not needed.
//
// The hk-hf9i8 fix: the original code used exact equality
// (record.Status == IntendedPostState), which retained claim intents when
// beads had advanced from in_progress to closed.  With 991 such files on
// disk, every daemon restart called br show 991 times (~4 min hang).
//
// Per-op rules (conservative — retain on any ambiguity):
//
//	claim  (→ in_progress): landed if status ≠ open (bead left the pre-claim state)
//	close  (→ closed):      landed if status = closed or tombstone
//	reopen (→ open):        landed if status = open, in_progress, or tombstone
//	reset  (→ open):        same as reopen
func gcIntentOpLanded(op core.TerminalOp, currentStatus, intendedPostState core.CoarseStatus) bool {
	if currentStatus == intendedPostState {
		return true
	}
	switch op {
	case core.TerminalOpClaim:
		// claim: open → in_progress.
		// If the bead is no longer open it was claimed; it may have since
		// advanced to closed or tombstone.
		return currentStatus != core.CoarseStatusOpen
	case core.TerminalOpClose:
		// close: in_progress → closed.
		// tombstone is the only state reachable after closed.
		return currentStatus == core.CoarseStatusTombstone
	case core.TerminalOpReopen, core.TerminalOpReset:
		// reopen/reset: closed → open.
		// If the bead is in_progress the reopen ran and the bead was claimed.
		// If the bead is tombstone the reopen ran and the bead was eventually
		// purged.  If the bead is still closed the situation is ambiguous
		// (op may not have run yet) — retain conservatively.
		return currentStatus == core.CoarseStatusInProgress ||
			currentStatus == core.CoarseStatusTombstone
	}
	return false
}

// ──────────────────────────────────────────────────────────────────────────────
// (e) Stale reconciliation lock sweep
// ──────────────────────────────────────────────────────────────────────────────

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

		stale, probeErr := reconLockIsStale(lockPath)
		if probeErr != nil {
			orphanLog(logger, "SweepStaleReconciliationLocks: probe %q: %v (skipping)", name, probeErr)
			continue
		}
		if !stale {
			orphanLog(logger, "SweepStaleReconciliationLocks: %q is active (EWOULDBLOCK or live PID); skipping", name)
			continue
		}

		// RC-002b: read verdict-executed state and run_id before unlinking.
		runID, hasVerdictExecuted, metaErr := reconLockReadMeta(lockPath)
		if metaErr != nil {
			// Cannot read meta; treat conservatively as no-verdict-executed so the run
			// is routed to Cat 3b (the safer choice: Cat 3b re-execution is idempotent).
			orphanLog(logger, "SweepStaleReconciliationLocks: read meta %q: %v (treating as no-verdict)", name, metaErr)
			hasVerdictExecuted = false
			runID = strings.TrimSuffix(name, ".lock")
		}

		// Stale: remove via unlink + fsync(parent dir).
		if removeErr := reconLockUnlinkAndFsync(lockPath, lockDir, logger); removeErr != nil {
			orphanLog(logger, "SweepStaleReconciliationLocks: remove %q: %v", name, removeErr)
			lastRemoveErr = removeErr
			continue
		}
		orphanLog(logger, "SweepStaleReconciliationLocks: removed stale lock %q (verdict_executed=%v)", name, hasVerdictExecuted)
		result.Removed++

		if !hasVerdictExecuted {
			// No verdict-executed line: route the target run through Cat 3b
			// (verdict-emitted-but-unexecuted) per RC-002b / §8.5.
			result.Cat3bRunIDs = append(result.Cat3bRunIDs, runID)
			orphanLog(logger, "SweepStaleReconciliationLocks: run %q queued for Cat 3b routing (no verdict-executed)", runID)
		}
	}

	if lastRemoveErr != nil {
		return result, fmt.Errorf("lifecycle: SweepStaleReconciliationLocks: some removals failed (last: %w)", lastRemoveErr)
	}
	return result, nil
}

// reconLockIsStale reports whether a reconciliation lock file is stale:
//   - flock(LOCK_EX|LOCK_NB) succeeds (no live lock holder), AND
//   - the recorded creator_pid does not respond to kill(pid, 0).
//
// Returns (false, nil) if the lock is actively held (EWOULDBLOCK).
// Returns (false, err) if the file cannot be opened or the PID line cannot
// be parsed.
func reconLockIsStale(lockPath string) (stale bool, err error) {
	//nolint:gosec // G304: path is constructed from projectDir + .harmonik/reconciliation-locks/ + entry name, not user input
	f, err := os.OpenFile(lockPath, os.O_RDWR, 0o600)
	if err != nil {
		return false, fmt.Errorf("reconLockIsStale: open %q: %w", lockPath, err)
	}
	defer func() { _ = f.Close() }() //nolint:errcheck // cleanup error unactionable

	flockErr := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if flockErr != nil {
		// EWOULDBLOCK: lock is actively held — not stale.
		return false, nil
	}
	// Release immediately; we only wanted the liveness probe.
	_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN) //nolint:errcheck // release error unactionable

	// Parse creator_pid from file content.
	pid, parseErr := reconLockReadCreatorPID(f)
	if parseErr != nil {
		// Cannot parse: treat as stale (remove it).
		return true, nil
	}

	// Stale iff the creator PID is dead.
	return !orphanSweepIsPidLive(pid), nil
}

// reconLockReadCreatorPID reads the creator_pid field from an already-open
// reconciliation lock file. The file format is line-based: one line is
// "creator_pid=<integer>".
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

// reconLockReadMeta reads the run_id and verdict-executed state from a
// reconciliation lock file at lockPath.
//
// Per RC-002b, the verdict-executor writes "Harmonik-Verdict-Executed: true" to
// the lock file just before releasing the lock. If this line is present the
// verdict was already committed to git; the lock only outlived its useful purpose.
// If absent, the verdict was not executed before the daemon crashed.
//
// runID falls back to the filename-derived value (strip ".lock" suffix) if the
// "run_id=" line is not found in the file.
func reconLockReadMeta(lockPath string) (runID string, hasVerdictExecuted bool, err error) {
	//nolint:gosec // G304: lockPath is constructed from projectDir + known relative path, not user input
	f, err := os.Open(lockPath)
	if err != nil {
		return "", false, fmt.Errorf("reconLockReadMeta: open %q: %w", lockPath, err)
	}
	defer func() { _ = f.Close() }() //nolint:errcheck // cleanup error unactionable

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
		// Filename is the canonical source when run_id line is absent.
		runID = strings.TrimSuffix(filepath.Base(lockPath), ".lock")
	}
	return runID, hasVerdictExecuted, nil
}

// reconLockUnlinkAndFsync removes lockPath and fsyncs the parent directory,
// per PL-006's "unlink followed by fsync(parent_directory_fd)" discipline.
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
	defer func() { _ = dirFd.Close() }() //nolint:errcheck // cleanup error unactionable
	if syncErr := dirFd.Sync(); syncErr != nil {
		orphanLog(logger, "reconLockUnlinkAndFsync: fsync parent dir: %v (non-fatal)", syncErr)
	}
	return nil
}

// ──────────────────────────────────────────────────────────────────────────────
// (d) Stale intent file enumeration
// ──────────────────────────────────────────────────────────────────────────────

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
