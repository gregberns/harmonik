package lifecycle

// agentwatcherreap.go — hk-6629b: launch-path reap of prior same-agent
// `comms recv --follow` / `subscribe --follow` watcher processes.
//
// Root cause (hk-6629b): a `comms recv --follow` / `subscribe --follow` child
// process survives its spawning session's /clear cycle — reparented to init
// (ppid=1) — and keeps holding a daemon subscribe slot indefinitely. Over
// successive captain/crew /clear cycles these accumulate until the daemon's
// subscribe capacity is exhausted, blinding every NEW watcher.
//
// Captain ruling (bead comment 2176): implement launch-path reap (option b),
// NOT a daemon subscribe-server edit (that is the separate follow-up bead,
// hk-f9gna). CRITICAL ACCEPTANCE NUANCE: reap prior same-agent watchers
// REGARDLESS OF LIVENESS — the observed leak was a fully live, still-reading
// duplicate (reparented to init) that survived a prior session's /clear and
// was actively double-delivering + holding a slot. A "kill only dead procs"
// heuristic (e.g. gating on ppid==1, as BI-014a's ProcessLister does for `br`)
// would MISS exactly the case that motivated this fix — so identification
// here is by COMMAND LINE + AGENT IDENTITY only, never by liveness/parentage.
//
// # Two bounds on that ruling
//
// "Regardless of liveness" says when a match may be killed. It says nothing
// about what counts as a match, and read as a licence to match loosely it
// authorises two kills this reap must never make.
//
// (1) Project scope. Agent names are per-project, not per-machine: several
// harmonik projects share a box and each runs a crew called "mike". A reap
// keyed on the name alone kills every project's watcher, not just this one's.
// So a candidate MUST positively prove it belongs to the caller's project —
// via a --project flag if it carries one, otherwise via its HARMONIK_PROJECT
// environment variable — and a candidate that can prove neither is LEFT
// ALONE. Failing closed here costs a missed reap (the pre-existing leak);
// failing open costs another project's live watcher.
//
// (2) The process must BE the watcher, not merely mention it. Agents routinely
// run the watcher command through a wrapper — `/bin/zsh -c '... harmonik comms
// recv --agent mike --follow ...'`, often wrapped again in a re-arming
// `while true` loop. A substring scan matches the wrapper as readily as the
// watcher, and killing the wrapper takes out a live agent's tool subprocess.
// So the subcommand is matched POSITIONALLY: argv[0] must itself be harmonik.
//
// Bead: hk-6629b.

import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// ProjectPathEnvKey is the environment variable carrying a harmonik agent's
// project root. The crew launch spec sets it on every agent it starts (see
// internal/daemon/crewlaunchspec.go), and it is inherited by the watcher
// processes that agent spawns — which is what lets the launch-path reap tell
// this project's watchers from a peer project's.
//
// Distinct from ProvenanceEnvKey (HARMONIK_PROJECT_HASH), which the DAEMON
// sets on handler subprocesses only and which is absent from the agent-side
// population this reap enumerates.
const ProjectPathEnvKey = "HARMONIK_PROJECT"

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
type OSAgentWatcherLister struct {
	// Project is the root of the project whose watchers may be reaped. It is
	// REQUIRED: a zero Project matches nothing, so a caller that forgets to
	// set it reaps none of its own watchers rather than every project's.
	Project string
}

// ListAgentFollowWatcherPIDs implements AgentWatcherLister using
// `ps -eo pid,args`, returning only processes that both match the watcher
// shape for agent and prove membership of l.Project.
func (l OSAgentWatcherLister) ListAgentFollowWatcherPIDs(ctx context.Context, agent string) ([]int, error) {
	if agent == "" || l.Project == "" {
		return nil, nil
	}
	//nolint:gosec // G204: arguments are hard-coded constants, not user input
	out, err := exec.CommandContext(ctx, "ps", "-eo", "pid,args").Output()
	if err != nil {
		return nil, fmt.Errorf("lifecycle: OSAgentWatcherLister: ps: %w", err)
	}
	want := canonicalProjectPath(l.Project)

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
		cmdline := strings.TrimSpace(fields[1])
		if !matchesAgentFollowWatcher(cmdline, agent) {
			continue
		}
		if !candidateInProject(ctx, pid, cmdline, want) {
			continue
		}
		pids = append(pids, pid)
	}
	return pids, nil
}

// candidateInProject reports whether the process identified by pid/cmdline can
// PROVE it belongs to the project rooted at want (already canonicalized).
//
// Two proofs are accepted, in order of cost: an explicit --project flag on the
// command line, then the HARMONIK_PROJECT environment variable the agent
// passed down to it. A process offering neither proof returns false and is
// left alone — see bound (1) in the package doc.
func candidateInProject(ctx context.Context, pid int, cmdline, want string) bool {
	if want == "" {
		return false
	}
	if got, ok := watcherProjectFromCmdline(cmdline); ok {
		return canonicalProjectPath(got) == want
	}
	if got, ok := processEnvValue(ctx, pid, ProjectPathEnvKey); ok {
		return canonicalProjectPath(got) == want
	}
	return false
}

// watcherProjectFromCmdline returns the value of an explicit --project flag,
// in either the "--project=<path>" or "--project <path>" form.
func watcherProjectFromCmdline(cmdline string) (string, bool) {
	tokens := strings.Fields(cmdline)
	for i, tok := range tokens {
		flag, val, ok := splitFlagToken(tok)
		if !ok && tok == "--project" && i+1 < len(tokens) {
			flag, val, ok = tok, tokens[i+1], true
		}
		if ok && flag == "--project" && val != "" {
			return val, true
		}
	}
	return "", false
}

// canonicalProjectPath reduces a project root to a comparable form: absolute,
// symlinks resolved, trailing separators removed. Resolution is best-effort —
// a path that cannot be resolved (e.g. it no longer exists) degrades to its
// lexically cleaned form rather than to the empty string, so two spellings of
// a live path still compare equal.
func canonicalProjectPath(path string) string {
	if path == "" {
		return ""
	}
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		path = resolved
	}
	return filepath.Clean(path)
}

// matchesAgentFollowWatcher reports whether cmdline IS an invocation of
// `harmonik comms recv --follow` or `harmonik subscribe --follow` addressed to
// agent via a "--agent"/"--to" flag.
//
// Every element is matched by argv POSITION, never by substring:
//
//   - argv[0]'s basename must be "harmonik", so a shell running the watcher
//     (`/bin/zsh -c '... harmonik comms recv --agent mike --follow ...'`) does
//     not match — killing that shell would take out a live agent's tool
//     subprocess, not a stray watcher. See bound (2) in the package doc.
//   - the subcommand must occupy argv[1:], so a `harmonik` invocation that
//     merely mentions "subscribe" in a later argument does not match.
//   - the agent name must be a whole flag value, so "captain" does not match a
//     watcher addressed to "captain2".
func matchesAgentFollowWatcher(cmdline, agent string) bool {
	if agent == "" {
		return false
	}
	args, ok := watcherSubcommandArgs(strings.Fields(cmdline))
	if !ok {
		return false
	}
	return argsAddressFollowedAgent(args, agent)
}

// watcherSubcommandArgs reports whether tokens are the argv of a harmonik
// watcher invocation, returning the arguments that follow the subcommand.
//
// Both the binary and the subcommand are read by POSITION, which is what
// excludes a shell whose script text merely contains the watcher command.
func watcherSubcommandArgs(tokens []string) ([]string, bool) {
	if len(tokens) < 2 || filepath.Base(tokens[0]) != "harmonik" {
		return nil, false
	}
	switch {
	case tokens[1] == "subscribe":
		return tokens[2:], true
	case tokens[1] == "comms" && len(tokens) > 2 && tokens[2] == "recv":
		return tokens[3:], true
	}
	return nil, false
}

// argsAddressFollowedAgent reports whether args carry both --follow and an
// --agent/--to naming agent, in either the "--flag=value" or "--flag value"
// form. The agent name must be a whole flag value, so "captain" does not match
// a watcher addressed to "captain2".
func argsAddressFollowedAgent(args []string, agent string) bool {
	var following, addressed bool
	for i, tok := range args {
		flag, val, ok := splitFlagToken(tok)
		if !ok && (tok == "--agent" || tok == "--to") && i+1 < len(args) {
			flag, val, ok = tok, args[i+1], true
		}
		switch {
		case tok == "--follow", ok && flag == "--follow":
			following = true
		case ok && (flag == "--agent" || flag == "--to") && val == agent:
			addressed = true
		}
	}
	return following && addressed
}

// splitFlagToken splits a "--flag=value" token into (flag, value, true).
// Returns ("", "", false) for a token with no "=" (the caller checks the
// "--flag value" two-token form separately).
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

// agentWatcherReapGracePeriod / PollInterval mirror SweepOrphanBr's SIGTERM→
// SIGKILL escalation timing (vars so tests can shorten them).
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
// project is the root of the project whose watchers may be reaped, and is
// REQUIRED: agent names are per-project, so without it this would reap every
// same-named watcher on the machine, including peer projects'. An empty
// project reaps nothing.
//
// If lister is nil, an OSAgentWatcherLister scoped to project is used. If
// logger is nil, log messages are silently discarded.
//
// Bead: hk-6629b.
func ReapPriorAgentFollowWatchers(ctx context.Context, lister AgentWatcherLister, agent, project string, excludePID int, logger *log.Logger) (survived []int, err error) {
	if agent == "" {
		return nil, nil
	}
	if lister == nil {
		if project == "" {
			orphanLog(logger, "ReapPriorAgentFollowWatchers: no project scope for agent %q; skipping reap (a name alone cannot distinguish this project's watchers from a peer project's)", agent)
			return nil, nil
		}
		lister = OSAgentWatcherLister{Project: project}
	}

	pids, err := lister.ListAgentFollowWatcherPIDs(ctx, agent)
	if err != nil {
		return nil, fmt.Errorf("lifecycle: ReapPriorAgentFollowWatchers: enumerate: %w", err)
	}
	pids = withoutPID(pids, excludePID)
	if len(pids) == 0 {
		return nil, nil
	}

	orphanLog(logger, "ReapPriorAgentFollowWatchers: found %d prior --follow watcher(s) for agent %q: %v", len(pids), agent, pids)

	signalWatchers(pids, syscall.SIGTERM, "SIGTERM", logger)
	alive := awaitWatcherExit(ctx, pids, logger)

	// Before SIGKILL, re-verify identity via a fresh command-line enumeration
	// so a recycled PID (unrelated process that inherited the number after the
	// watcher exited) is never SIGKILLed. This does NOT reintroduce a liveness
	// gate on the initial candidate set (the hk-6629b ruling): identification
	// stays command-line + agent-identity only; the recheck only confirms the
	// PID still belongs to a matching watcher before escalation.
	if len(alive) > 0 {
		fresh, freshErr := lister.ListAgentFollowWatcherPIDs(ctx, agent)
		if freshErr != nil {
			orphanLog(logger, "ReapPriorAgentFollowWatchers: re-enumerate before SIGKILL failed: %v; skipping SIGKILL (PID-reuse guard)", freshErr)
			return pidSetSlice(alive), nil
		}
		alive = reverifyCandidatePIDs(alive, fresh)
	}
	if len(alive) > 0 {
		orphanLog(logger, "ReapPriorAgentFollowWatchers: %d process(es) survived SIGTERM grace period; sending SIGKILL", len(alive))
		signalWatchers(pidSetSlice(alive), syscall.SIGKILL, "SIGKILL", logger)
	}

	for pid := range alive {
		if orphanSweepIsPidLive(pid) {
			survived = append(survived, pid)
			orphanLog(logger, "ReapPriorAgentFollowWatchers: pid %d survived SIGKILL", pid)
		}
	}

	return survived, nil
}

// withoutPID returns pids with excludePID removed. A zero excludePID removes
// nothing.
func withoutPID(pids []int, excludePID int) []int {
	if excludePID == 0 {
		return pids
	}
	out := make([]int, 0, len(pids))
	for _, pid := range pids {
		if pid == excludePID {
			continue
		}
		out = append(out, pid)
	}
	return out
}

// signalWatchers sends sig to every pid, logging each outcome under name. A
// signal failure is logged and passed over rather than returned: the process
// having already exited is the ordinary case, not an error.
func signalWatchers(pids []int, sig syscall.Signal, name string, logger *log.Logger) {
	for _, pid := range pids {
		if sigErr := syscall.Kill(pid, sig); sigErr != nil {
			orphanLog(logger, "ReapPriorAgentFollowWatchers: %s pid %d: %v (may have already exited)", name, pid, sigErr)
			continue
		}
		orphanLog(logger, "ReapPriorAgentFollowWatchers: sent %s to pid %d", name, pid)
	}
}

// awaitWatcherExit polls pids until each has exited or the grace period
// lapses, returning the set still alive. A cancelled context ends the wait
// early and escalates rather than abandoning the reap half-done.
func awaitWatcherExit(ctx context.Context, pids []int, logger *log.Logger) map[int]bool {
	alive := make(map[int]bool, len(pids))
	for _, pid := range pids {
		alive[pid] = true
	}
	deadline := time.Now().Add(agentWatcherReapGracePeriod)
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
	return alive
}

// pidSetSlice returns the members of set as a slice.
func pidSetSlice(set map[int]bool) []int {
	out := make([]int, 0, len(set))
	for pid := range set {
		out = append(out, pid)
	}
	return out
}
