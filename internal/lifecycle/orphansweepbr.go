package lifecycle

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// ProcessLister is the interface that SweepOrphanBr uses to enumerate candidate
// orphan `br` processes. Implementations query the OS process table; tests
// inject a deterministic fake.
//
// ListOrphanBrPIDs returns the PIDs of processes whose binary name is "br"
// and whose parent PID is 1 (re-parented to init).
//
// Spec ref: beads-integration.md §4.5 BI-014a — "enumerate processes whose
// binary path matches the pinned `br` location and whose parent PID is 1
// (re-parented to init)."
type ProcessLister interface {
	ListOrphanBrPIDs(ctx context.Context) ([]int, error)
}

// OSProcessLister is the production ProcessLister. It enumerates processes via
// `ps -eo pid,ppid,comm` and filters for name=="br" with PPID==1.
//
// On both Linux and macOS, `ps -eo pid,ppid,comm` is available and outputs one
// line per process with parent PID and command basename. The comm field is
// limited to 15 characters on Linux (sufficient for "br"); on macOS it is the
// full basename.
//
// Spec ref: beads-integration.md §4.5 BI-014a — "enumerate processes whose
// binary path matches the pinned `br` location and whose parent PID is 1."
type OSProcessLister struct{}

// ListOrphanBrPIDs implements ProcessLister using `ps -eo pid,ppid,comm`.
func (OSProcessLister) ListOrphanBrPIDs(ctx context.Context) ([]int, error) {
	out, err := exec.CommandContext(ctx, "ps", "-eo", "pid,ppid,comm").Output()
	if err != nil {
		return nil, fmt.Errorf("lifecycle: OSProcessLister: ps: %w", err)
	}

	lines := strings.Split(string(out), "\n")
	pids := make([]int, 0, len(lines))
	for _, line := range lines[1:] { // skip header line
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		pidStr := fields[0]
		ppidStr := fields[1]
		comm := fields[2]

		if comm != "br" {
			continue
		}
		ppid, err := strconv.Atoi(ppidStr)
		if err != nil {
			continue
		}
		if ppid != 1 {
			continue
		}
		pid, err := strconv.Atoi(pidStr)
		if err != nil {
			continue
		}
		pids = append(pids, pid)
	}
	return pids, nil
}

var orphanSweepGracePeriod = 5 * time.Second

var orphanSweepPollInterval = 100 * time.Millisecond

// SweepOrphanBr enumerates `br` processes re-parented to init (PPID==1),
// sends SIGTERM to each, waits up to 5 s, then sends SIGKILL to those still
// alive. It returns the PIDs of any processes that survived SIGKILL (the caller
// routes these to Cat 0).
//
// If lister is nil, OSProcessLister is used.
// If logger is nil, log messages are silently discarded.
//
// Spec ref: beads-integration.md §4.5 BI-014a — "enumerate processes whose
// binary path matches the pinned `br` location and whose parent PID is 1
// (re-parented to init). For each match, the adapter MUST send SIGTERM and wait
// up to 5s, then SIGKILL, mirroring the BI-025c termination discipline. Orphan
// `br` subprocesses surviving the sweep are a Cat 0 prerequisite failure
// (SQLite WAL contention)."
func SweepOrphanBr(ctx context.Context, lister ProcessLister, logger *log.Logger) (survived []int, err error) {
	if lister == nil {
		lister = OSProcessLister{}
	}

	pids, err := lister.ListOrphanBrPIDs(ctx)
	if err != nil {
		return nil, fmt.Errorf("lifecycle: SweepOrphanBr: enumerate: %w", err)
	}
	if len(pids) == 0 {
		return nil, nil
	}

	orphanLog(logger, "SweepOrphanBr: found %d orphan br process(es): %v", len(pids), pids)

	for _, pid := range pids {
		if sigErr := syscall.Kill(pid, syscall.SIGTERM); sigErr != nil {
			orphanLog(logger, "SweepOrphanBr: SIGTERM pid %d: %v (may have already exited)", pid, sigErr)
		} else {
			orphanLog(logger, "SweepOrphanBr: sent SIGTERM to pid %d", pid)
		}
	}

	deadline := time.Now().Add(orphanSweepGracePeriod)
	alive := make(map[int]bool, len(pids))
	for _, pid := range pids {
		alive[pid] = true
	}

	for time.Now().Before(deadline) && len(alive) > 0 {
		for pid := range alive {
			if !orphanSweepIsPidLive(pid) {
				delete(alive, pid)
				orphanLog(logger, "SweepOrphanBr: pid %d exited after SIGTERM", pid)
			}
		}
		if len(alive) == 0 {
			break
		}
		select {
		case <-ctx.Done():
			orphanLog(logger, "SweepOrphanBr: context cancelled during grace period; escalating to SIGKILL")
		case <-time.After(orphanSweepPollInterval):
		}
		if ctx.Err() != nil {
			break
		}
	}

	if len(alive) > 0 {
		fresh, freshErr := lister.ListOrphanBrPIDs(ctx)
		if freshErr != nil {
			orphanLog(logger, "SweepOrphanBr: re-enumerate before SIGKILL failed: %v; skipping SIGKILL (PID-reuse guard)", freshErr)
			for pid := range alive {
				survived = append(survived, pid)
			}
			return survived, nil
		}
		alive = reverifyCandidatePIDs(alive, fresh)
	}
	if len(alive) > 0 {
		orphanLog(logger, "SweepOrphanBr: %d process(es) survived SIGTERM grace period; sending SIGKILL", len(alive))
	}
	for pid := range alive {
		if sigErr := syscall.Kill(pid, syscall.SIGKILL); sigErr != nil {
			orphanLog(logger, "SweepOrphanBr: SIGKILL pid %d: %v (may have already exited)", pid, sigErr)
		} else {
			orphanLog(logger, "SweepOrphanBr: sent SIGKILL to pid %d", pid)
		}
	}

	for pid := range alive {
		if orphanSweepIsPidLive(pid) {
			survived = append(survived, pid)
			orphanLog(logger, "SweepOrphanBr: pid %d survived SIGKILL — Cat 0 prerequisite failure", pid)
		}
	}

	return survived, nil
}

func reverifyCandidatePIDs(alive map[int]bool, fresh []int) map[int]bool {
	freshSet := make(map[int]bool, len(fresh))
	for _, pid := range fresh {
		freshSet[pid] = true
	}
	confirmed := make(map[int]bool, len(alive))
	for pid := range alive {
		if freshSet[pid] {
			confirmed[pid] = true
		}
	}
	return confirmed
}

func orphanSweepIsPidLive(pid int) bool {
	err := syscall.Kill(pid, 0)
	if err == nil {
		return true
	}
	if errors.Is(err, syscall.ESRCH) {
		return false
	}
	return true
}

func orphanLog(logger *log.Logger, format string, args ...any) {
	if logger == nil {
		return
	}
	logger.Printf(format, args...)
}
