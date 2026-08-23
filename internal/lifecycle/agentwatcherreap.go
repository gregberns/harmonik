package lifecycle

import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// AgentWatcherLister enumerates the PIDs of prior `--follow` comms-watcher
// processes addressed to a given agent name. Implementations match on the
// full command line, not on process-tree liveness or parentage — see the
// package doc comment for why.
type AgentWatcherLister interface {
	ListAgentFollowWatcherPIDs(ctx context.Context, agent string) ([]int, error)
}

// OSAgentWatcherLister is the production AgentWatcherLister. It enumerates
// every process via `ps -eo pid,args` (full command line; portable across
// Linux and macOS, unlike `ps -eo pid,ppid,comm`'s truncated basename) and
// matches the two watcher shapes:
//
//	harmonik comms recv --agent <name> ... --follow ...
//	harmonik subscribe  --to    <name> ... --follow ...
type OSAgentWatcherLister struct{}

// ListAgentFollowWatcherPIDs implements AgentWatcherLister using
// `ps -eo pid,args`.
func (OSAgentWatcherLister) ListAgentFollowWatcherPIDs(ctx context.Context, agent string) ([]int, error) {
	if agent == "" {
		return nil, nil
	}
	out, err := exec.CommandContext(ctx, "ps", "-eo", "pid,args").Output()
	if err != nil {
		return nil, fmt.Errorf("lifecycle: OSAgentWatcherLister: ps: %w", err)
	}

	var pids []int
	lines := strings.Split(string(out), "\n")
	for _, line := range lines[1:] { // skip header line
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.SplitN(line, " ", 2)
		if len(fields) < 2 {
			continue
		}
		pid, perr := strconv.Atoi(fields[0])
		if perr != nil {
			continue
		}
		if matchesAgentFollowWatcher(strings.TrimSpace(fields[1]), agent) {
			pids = append(pids, pid)
		}
	}
	return pids, nil
}

func matchesAgentFollowWatcher(cmdline, agent string) bool {
	if agent == "" || !strings.Contains(cmdline, "harmonik") || !strings.Contains(cmdline, "--follow") {
		return false
	}
	isCommsRecv := strings.Contains(cmdline, "comms") && strings.Contains(cmdline, "recv")
	isSubscribe := strings.Contains(cmdline, "subscribe")
	if !isCommsRecv && !isSubscribe {
		return false
	}

	tokens := strings.Fields(cmdline)
	for i, tok := range tokens {
		flag, val, ok := splitFlagToken(tok)
		if !ok && (tok == "--agent" || tok == "--to") && i+1 < len(tokens) {
			flag, val, ok = tok, tokens[i+1], true
		}
		if ok && (flag == "--agent" || flag == "--to") && val == agent {
			return true
		}
	}
	return false
}

func splitFlagToken(tok string) (flag, val string, ok bool) {
	if !strings.HasPrefix(tok, "--") {
		return "", "", false
	}
	eq := strings.IndexByte(tok, '=')
	if eq < 0 {
		return "", "", false
	}
	return tok[:eq], tok[eq+1:], true
}

var (
	agentWatcherReapGracePeriod  = 5 * time.Second
	agentWatcherReapPollInterval = 100 * time.Millisecond
)

// ReapPriorAgentFollowWatchers enumerates and terminates every prior
// `comms recv --follow` / `subscribe --follow` watcher process addressed to
// agent — REGARDLESS of liveness or process-tree reparenting (hk-6629b
// captain ruling). Intended to run on the launch path (captain/crew start),
// before the newly-launched agent arms its OWN watchers, so a relaunch after
// /clear never leaves its predecessor holding a daemon subscribe slot.
//
// excludePID, when non-zero, is skipped even if it matches — a defensive
// guard against the launcher process itself coincidentally matching.
//
// SIGTERM is sent first, giving the victim a chance to run its own
// leave-beat/cleanup path (e.g. cmd/harmonik's sendPresenceLeaveBeat, which
// fires on SIGTERM), then SIGKILL after a grace period for any survivor —
// mirroring SweepOrphanBr's termination discipline (BI-014a).
//
// If lister is nil, OSAgentWatcherLister is used. If logger is nil, log
// messages are silently discarded.
//
// Bead: hk-6629b.
func ReapPriorAgentFollowWatchers(ctx context.Context, lister AgentWatcherLister, agent string, excludePID int, logger *log.Logger) (survived []int, err error) {
	if agent == "" {
		return nil, nil
	}
	if lister == nil {
		lister = OSAgentWatcherLister{}
	}

	pids, err := lister.ListAgentFollowWatcherPIDs(ctx, agent)
	if err != nil {
		return nil, fmt.Errorf("lifecycle: ReapPriorAgentFollowWatchers: enumerate: %w", err)
	}
	filtered := make([]int, 0, len(pids))
	for _, pid := range pids {
		if excludePID != 0 && pid == excludePID {
			continue
		}
		filtered = append(filtered, pid)
	}
	pids = filtered
	if len(pids) == 0 {
		return nil, nil
	}

	orphanLog(logger, "ReapPriorAgentFollowWatchers: found %d prior --follow watcher(s) for agent %q: %v", len(pids), agent, pids)

	for _, pid := range pids {
		if sigErr := syscall.Kill(pid, syscall.SIGTERM); sigErr != nil {
			orphanLog(logger, "ReapPriorAgentFollowWatchers: SIGTERM pid %d: %v (may have already exited)", pid, sigErr)
		} else {
			orphanLog(logger, "ReapPriorAgentFollowWatchers: sent SIGTERM to pid %d", pid)
		}
	}

	deadline := time.Now().Add(agentWatcherReapGracePeriod)
	alive := make(map[int]bool, len(pids))
	for _, pid := range pids {
		alive[pid] = true
	}
	for time.Now().Before(deadline) && len(alive) > 0 {
		for pid := range alive {
			if !orphanSweepIsPidLive(pid) {
				delete(alive, pid)
				orphanLog(logger, "ReapPriorAgentFollowWatchers: pid %d exited after SIGTERM", pid)
			}
		}
		if len(alive) == 0 {
			break
		}
		select {
		case <-ctx.Done():
			orphanLog(logger, "ReapPriorAgentFollowWatchers: context cancelled during grace period; escalating to SIGKILL")
		case <-time.After(agentWatcherReapPollInterval):
		}
		if ctx.Err() != nil {
			break
		}
	}

	if len(alive) > 0 {
		fresh, freshErr := lister.ListAgentFollowWatcherPIDs(ctx, agent)
		if freshErr != nil {
			orphanLog(logger, "ReapPriorAgentFollowWatchers: re-enumerate before SIGKILL failed: %v; skipping SIGKILL (PID-reuse guard)", freshErr)
			for pid := range alive {
				survived = append(survived, pid)
			}
			return survived, nil
		}
		alive = reverifyCandidatePIDs(alive, fresh)
	}
	if len(alive) > 0 {
		orphanLog(logger, "ReapPriorAgentFollowWatchers: %d process(es) survived SIGTERM grace period; sending SIGKILL", len(alive))
	}
	for pid := range alive {
		if sigErr := syscall.Kill(pid, syscall.SIGKILL); sigErr != nil {
			orphanLog(logger, "ReapPriorAgentFollowWatchers: SIGKILL pid %d: %v (may have already exited)", pid, sigErr)
		} else {
			orphanLog(logger, "ReapPriorAgentFollowWatchers: sent SIGKILL to pid %d", pid)
		}
	}

	for pid := range alive {
		if orphanSweepIsPidLive(pid) {
			survived = append(survived, pid)
			orphanLog(logger, "ReapPriorAgentFollowWatchers: pid %d survived SIGKILL", pid)
		}
	}

	return survived, nil
}
