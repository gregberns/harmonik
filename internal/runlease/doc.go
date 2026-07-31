// Package runlease holds the resource discipline for one bead run.
//
// Three types, and the point of all three is that a resource cannot be
// forgotten and cannot be given back twice:
//
//   - [Lease] — one held resource and the single call that gives it back. The
//     call runs at most once, from any goroutine.
//   - [Scope] — the ordered set of leases a run holds. Closing it gives them
//     back innermost first. A [Scope] can [Scope.Nest] a child, because a run's
//     per-launch resources have a shorter life than its per-run ones: a graph
//     run takes and gives back one session per node while it still holds one
//     worktree and one tunnel for the whole run.
//   - [Disposition] — the single answer to "what does this run give back", for
//     the whole run. [Decide] is the one place its polarity lives.
//
// Nothing here does I/O, reads a clock, or knows that a daemon exists. A lease
// carries the caller's release call as a value, so the whole package is
// testable with a recorder and no fakes.
//
// # Why the disposition is one value
//
// Before this package, one condition — an agent in its own tmux session plus a
// daemon that is shutting down — reached four places in the daemon's run
// function, spelled three different ways, two of the spellings 24 lines apart
// and the third 600 lines later. It missed two of the resources it should have
// covered. A skip flag per resource reproduces that spread. One value decided
// once for the run makes the incoherent combinations unrepresentable rather
// than merely unwritten.
//
// # What Survive does not promise
//
// [Survive] says what a run ASKS for. The system does not deliver it today: the
// daemon's boot orphan sweep kills every tmux session carrying the project
// prefix that is not in its exclusion set, with no liveness test, and it runs
// before the pass that looks for a surviving run. Do not read [Survive] as a
// guarantee that the agent is still there at the next boot. It is recorded as
// its own defect and it is not this package's to fix.
//
// Normative rules: specs/run-state-machine.md §4a (RSM-036 … RSM-038).
package runlease
