# Current keeper structure and modularity

## Conclusion

The keeper has a sound package boundary and a good pure cycle seam. It is not
yet modular enough to add Codex behavior safely. Preserve the Claude vertical,
then split the observation, policy, and delivery work before a second harness
uses it.

`internal/keeper` has 18 production files and 8,163 production lines. It has
59 test files and 21,112 test lines. This is a critical subsystem with a large
test surface, not a small utility.

## What is already modular

`internal/keeper/step.go` has the best seam. `Cycle.Step` takes typed events,
updates `CycleState`, and returns typed actions without I/O. `Cycler` in
`internal/keeper/cycle.go` and the effect loop in `internal/keeper/shell.go`
drive it. `internal/keeper/ports.go` also names useful ports for the pane,
gauge, handoff, events, clock, and respawn.

This supports the project principles well:

- The cycle can be replayed with values and a virtual clock.
- The action sequence gives a precise test oracle.
- The package does not import `internal/daemon`.
- Claude integration is already mostly at the edge: status-line and hook
  scripts write external files, while tmux injection is in one adapter area.

## Where the boundary leaks

The package boundary is stronger than the executable boundary.

- `Watcher.Run` in `internal/keeper/watcher.go` is a 2,110-line polling
  controller. It combines gauge reads, identity checks, heartbeat, warning
  delivery, stale-gauge recovery, live-pane recovery, dashboard nagging, and
  decision reaping.
- `WatcherConfig` has 63 fields. `CyclerConfig` has 57. Each joins policy,
  defaults, timing, ports, direct I/O functions, and test seams. The two
  structures duplicate threshold and gate knowledge.
- The generic lifecycle path assumes Claude concepts: `.ctx`, `.sid`,
  transcript scans, UUIDv4 session IDs, a handoff file, `/clear`, and an
  injected brief. A new harness must not inherit these assumptions.
- `dashboardnag.go` and `delivery_decision_0nlqs.go` make the keeper own
  unrelated product jobs. They pull in `dashboard`, `digest`, `presence`, and a
  CLI self-invocation path.
- The composition path in `cmd/harmonik/keeper_cmd.go` must understand the
  large mixed configuration surface. This is a second sign that the policy
  boundary is too weak.

The rest of the program depends on the keeper from `cmd/harmonik`,
`internal/agentlaunch`, and several daemon and run packages. Therefore a
rewrite must preserve the existing CLI and file contracts until a Claude
vertical proves compatibility.

## Target shape

Do not replace a harness handler with another harness handler. Make four small
mechanisms that a harness can compose.

| Mechanism | Owns | Claude adapter | Codex adapter |
| --- | --- | --- | --- |
| Observation | typed activity, identity, context, stop, and health signals | status-line, hooks, transcript, pane probes | Stop-hook input first; app-server events later |
| Work state | remaining work, blocked, awaiting assignment, or terminal | crew queue and mission state | same crew state |
| Policy | pure, idempotent decision from state and signal | shared | shared |
| Delivery | continuation, context reset, alert, or no action | tmux and handoff-clear-resume | Stop-hook JSON response; app-server command later |

Keep harness codecs outside the keeper core. A codec produces a normalized
signal and executes a typed delivery request. The keeper core must not parse a
Codex rollout file or know a Claude command.

Make a handoff-clear-resume action strategy. It is one Claude delivery method,
not the keeper's universal lifecycle model.

## Safe extraction order

1. Freeze the present Claude external contracts with replay and canary tests.
2. Move the pure keeper decision model out of `Watcher.Run`. Keep its public
   behavior unchanged.
3. Replace the two function-field bags with typed policy groups and explicit
   observation and delivery ports.
4. Wire a Claude codec to the new ports and prove parity.
5. Add the Codex Stop-hook codec. Extract only the shared types it uses.
6. Move dashboard nagging and decision reaping to independently wired
   observers. Do this only after the keeper decision core is stable.

This sequence follows "prove one vertical, then generalize." It avoids a
generic harness framework built from assumptions.

## Lane boundary

The keeper is not alpha or bravo work. `CHARTER.md` excludes it from the core.
The later lane owns `internal/keeper`, keeper CLI and configuration surfaces,
keeper scripts and skill assets, and `internal/agentlaunch/keeperargv.go`. It
does not own the core workflow packages. Its entry condition is a working core
with the keeper disabled. Its exit proof is Claude compatibility plus a Codex
vertical that adds no branch to the pure policy core.
