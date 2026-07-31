package runlease

// Resource names one thing a bead run holds. The list is closed, and it is the
// axis a [Disposition] answers against: adding a value here without adding it
// to the two switches below is a lint failure, which is the point.
//
// The nine live in three lifetimes. Per run: the worker slot, the local
// in-flight count, the tunnel port, the tunnel process, and the worktree. Per
// launch, and a graph run has one of these per node: the hook session, the
// agent session, and the substrate spawn slot. Per remote run only: the
// cold-start token. [RunRecord] is a tenth thing — the run registry entry is
// not a resource the run competes for, but the same disposition decides whether
// it stays, so it belongs on this axis.
type Resource int

// Resource values. ResourceUnnamed is the zero value; a lease should never
// carry it, and a disposition gives it back rather than leaving it standing.
const (
	ResourceUnnamed Resource = iota
	// WorkerSlot is the remote worker's concurrency slot, reserved by the
	// dispatch loop before the run function is entered.
	WorkerSlot
	// LocalSlot is the daemon's count of runs in flight on this machine.
	LocalSlot
	// TunnelPort is the reservation of a port number for a reverse tunnel.
	TunnelPort
	// TunnelProcess is the reverse-tunnel process itself.
	TunnelProcess
	// Worktree is the run's git worktree.
	Worktree
	// RunRecord is the run registry entry that lets a later daemon boot find
	// this run's session by name.
	RunRecord
	// HookSession is the registration that lets the agent's hooks report back
	// to this daemon.
	HookSession
	// AgentSession is the tmux session or window the agent runs in.
	AgentSession
	// SpawnSlot is the substrate's cap on how many agents may be starting at
	// once. It is given back when the agent reports ready, not at run end.
	SpawnSlot
	// ColdStartToken is the daemon-global cap on how many remote runs may be
	// in their cold-start window at once.
	ColdStartToken
)

// String returns a stable name for diagnostics and test failures.
func (r Resource) String() string {
	switch r {
	case WorkerSlot:
		return "worker-slot"
	case LocalSlot:
		return "local-slot"
	case TunnelPort:
		return "tunnel-port"
	case TunnelProcess:
		return "tunnel-process"
	case Worktree:
		return "worktree"
	case RunRecord:
		return "run-record"
	case HookSession:
		return "hook-session"
	case AgentSession:
		return "agent-session"
	case SpawnSlot:
		return "spawn-slot"
	case ColdStartToken:
		return "cold-start-token"
	case ResourceUnnamed:
		return "unnamed"
	}
	return "unnamed"
}

// Disposition is the single answer to "what does this run give back", decided
// once for the whole run by [Decide] and read by every release site.
//
// The zero value is [Reclaim]. A caller who never decided gives everything
// back, which is the recoverable mistake: a run reclaimed when it should have
// survived reopens its bead and is dispatched again, while a run that survives
// when it should not leaves a worktree and a session behind and strands the
// bead in progress with nothing alive to adopt it.
type Disposition int

// Disposition values.
const (
	// Reclaim gives every resource back. A run that finished, for any reason
	// the operator does not need to read, ends here.
	Reclaim Disposition = iota
	// Survive leaves standing everything a later daemon boot needs to find the
	// agent and everything the agent still needs to work and to report: the
	// session, the worktree, the run record, the hook session, and the tunnel.
	// The accounting slots are still given back — they are this process's
	// bookkeeping, and the surviving agent does not hold them.
	//
	// Read the package doc before trusting this: the system does not currently
	// deliver survival.
	Survive
	// RetainEvidence gives everything back except the worktree, because the
	// worktree holds the captured agent output and that is the only record of
	// why the run failed.
	RetainEvidence
)

// String returns a stable name for diagnostics and test failures.
func (d Disposition) String() string {
	switch d {
	case Reclaim:
		return "reclaim"
	case Survive:
		return "survive"
	case RetainEvidence:
		return "retain-evidence"
	}
	return "reclaim"
}

// Releases reports whether a run ending with disposition d must give r back.
//
// It is total over both axes. Anything a disposition does not name as kept is
// given back, so a resource added later leaks nothing by default — it fails the
// exhaustiveness check in [survivesWithTheRun] instead, and someone decides.
func (d Disposition) Releases(r Resource) bool {
	switch d {
	case Reclaim:
		return true
	case Survive:
		return !survivesWithTheRun(r)
	case RetainEvidence:
		return r != Worktree
	}
	return true
}

// survivesWithTheRun reports whether r is part of what a surviving run leaves
// standing — either the next boot needs it to find the agent, or the agent
// still needs it to work and to report.
//
// The tunnel and the hook session are in this list and were in no equivalent
// list before this package existed. They were torn down regardless, which left
// a surviving agent holding a session it could no longer report through.
func survivesWithTheRun(r Resource) bool {
	switch r {
	case AgentSession, Worktree, RunRecord, HookSession, TunnelProcess, TunnelPort:
		return true
	case WorkerSlot, LocalSlot, SpawnSlot, ColdStartToken, ResourceUnnamed:
		return false
	}
	return false
}

// LeavesBeadInProgress reports whether the run must leave its bead in progress
// for a later daemon boot to adopt, rather than deciding the bead's fate itself.
// Only a surviving run does, because only a surviving run has an agent that is
// still working.
//
// False does not mean "reopen the bead". It means the bead's fate is the run's
// own outcome to settle in the usual way.
func (d Disposition) LeavesBeadInProgress() bool { return d == Survive }

// Exit holds the facts a run's disposition depends on, and it holds all of
// them. It is the input to [Decide] and it is a plain value, so the decision is
// testable without a daemon.
type Exit struct {
	// SessionRunsIndependently is true when the agent runs in a tmux session of
	// its own, which outlives this process, rather than in a window of the
	// daemon's own session, which does not.
	SessionRunsIndependently bool
	// DaemonStopping is true when the run is ending because the daemon is
	// shutting down, rather than because the work reached an end.
	DaemonStopping bool
	// EvidenceWorthKeeping is true when the run failed in a way whose captured
	// output an operator has to read.
	EvidenceWorthKeeping bool
}

// Decide returns the one disposition for a run that ended with these facts. It
// is pure and total.
//
// Survival needs both of its facts. An independent session on a normal run end
// is reclaimed like any other, and a stopping daemon whose agent shares the
// daemon's own session has nothing that could outlive it.
//
// Survival wins over evidence when both apply: a surviving run keeps its
// worktree anyway, and it also keeps the session that the evidence case would
// tear down.
func Decide(e Exit) Disposition {
	if e.SessionRunsIndependently && e.DaemonStopping {
		return Survive
	}
	if e.EvidenceWorthKeeping {
		return RetainEvidence
	}
	return Reclaim
}
