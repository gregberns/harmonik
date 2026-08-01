# Graph-guided core decomposition

**Status:** research complete. The lane table carries the execution order.

This plan tests whether the rewrite can split at package boundaries. It does
not replace the delete-and-rewrite charter. It gives the two active lanes a
way to make new independent packages before they divide the run machine.

## Method

The `codebase-organism` project now analyzes an explicit checkout. Its symbol
graph ignores Go methods as package symbols. That change removes false call
edges from the core package.

Run this from `/Users/gb/github/codebase-organism` against the branch to plan:

```sh
repo=/Users/gb/github/harmonik-wt/bravo
out=/tmp/harmonik-graph

python3 harmonik-analysis/extract_graph.py \
  "$repo" github.com/gregberns/harmonik "$out/shape"
python3 planner/symbol_graph.py "$repo/internal/daemon" "$out/daemon"
python3 planner/seam_finder.py "$out/daemon_symbols.json" --k 12
python3 planner/seam_finder.py "$out/daemon_symbols.json" \
  --file workloop.go --k 10
```

Regenerate before each new wave. The committed graph artifacts describe an
older revision. The planner reads Go text. It does not resolve selector calls
or cross-package type use. Treat every proposed seam as a contract checklist.

## Findings

The completed live-pass work moved `runWorkLoop` to
`internal/daemon/scheduler.go`. It did not finish the acceptance work. The
build still omits the fail and hang twin binaries. Bravo owns that remaining
check.

The graph groups `beadRunOne` with five helpers. The helpers emit run events,
close a bead, find an owning epic, and end a run. This makes an explicit run
port the next serial daemon change. It does not prove that the whole function
can move to another package.

Three outer subsystems offer safer package cuts. The comms and decisions handler
is an isolated 404-line daemon service. It has twelve local boundary ties and
no run-machine connection. The tmux host joins
`internal/daemon/tmuxsubstrate.go` and `internal/daemon/pasteinject.go`. The
local socket joins the socket listener, dispatch, state, and dashboard files.
Each unit needs an alpha-owned contract before bravo can move it.

The comms handler, cursor store, notification stream, and local socket sit
outside the charter core. They are parallel-capacity work. They never delay the
queue transition or the tmux host, which serve the charter core directly.

## Execution model

Each extraction has three commits. Alpha first makes a narrow contract with
tests. Bravo then creates and tests the new package without a daemon
dependency. Alpha finally deletes the old daemon adapter. For the comms
handler, alpha replaces `SetRecvDeps` with constructor configuration and a
daemon-free cursor contract. Bravo moves the cursor store before the handler.

The tmux host contract must replace daemon concrete assertions and unexported
capability methods. It covers `workloop.go`, `dot_cascade_core.go`,
`agentlaunch.go`, and `crewstart.go`. Bravo changes the final host construction
in `cmd/harmonik/main.go` and `cmd/harmonik/run.go`. Alpha then removes the
daemon adapter. Before bravo starts, alpha adds a narrow depguard allow-list for
the new host package. That rule denies a direct daemon import.

The socket contract must live in a daemon-free API package. It freezes each
operation envelope, consumer-owned handler shape, and state output. Bravo moves
the listener, router adapter, and command models. Alpha deletes the daemon
adapter. Alpha first adds a narrow depguard allow-list and direct daemon deny.
Each final cutover keeps wire and scenario tests byte-compatible.

Do not split `workloop.go`, `scheduler.go`, `dot_cascade_core.go`, or
`workloop_runplan.go` before the dependency bundle shrinks behind owned ports.
Do not move queue spend, stale watching, state projection, or dashboard
projection by exporting daemon internals.

## Done means

The analysis is reproducible from an exact checkout. Each extraction starts
only after its contract is tested. Alpha and bravo do not edit the same daemon
file or boundary type. Each new package passes `go list -deps` without a daemon
dependency. The lane table records the handoff, cutover owner, and test evidence
for every wave.
