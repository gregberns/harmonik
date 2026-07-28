# C1 research — Construction graph and ownership

## Questions

1. Where is the current run graph constructed, and when does it become valid?
2. Which mutable resources have split owners?
3. Can queued and direct run identity combinations be represented safely?
4. Which existing dependency seams can the target graph retain?
5. What conflicts constrain the design?

## Findings

### Construction is temporal

ARCH-00's `temporal_construction` trace is reproduced by production symbols:

1. `internal/daemon/workloop.go` `newWorkLoopDeps` constructs an 81-field
   `workLoopDeps`, but boot-owned capabilities are injected later by
   `bootState.injectWorkLoopDeps`.
2. `runWorkLoop` selects work, mints identity, mutates queue/Beads state, and
   calls `workLoopDeps.runEnv`, `runPorts`, and `sharedHandles`.
3. `internal/daemon/runports.go` `runPorts` intentionally leaves `Worktree`,
   `Launch`, and `LaunchBuilder` nil.
4. `beadRunOne` completes those ports after remote/worktree resolution.

The types therefore admit a value that is syntactically constructed but not
safe for its eventual consumer. `RunEnv` has 30 value fields, `RunPorts` has
eight behavior fields, and `SharedHandles` has 17 shared handles. Their
comments document byte-identical migration intent, not a final validity
boundary.

### Ownership is split at every major transition

ARCH-00's ownership graph identifies:

| Resource | Current decision/effect locations | Research conclusion |
| --- | --- | --- |
| Queue selection/reservation | `runWorkLoop`, queue helpers, direct persistence callers | Outer loop may decide eligibility; CQ-02 transaction owner alone may mutate durable queue state |
| Bead claim/terminal | `runWorkLoop`, `beadRunOne`, Bead adapter | Decision and adapter effect must be explicit; Beads remains terminal owner |
| Run identity/record/registry | `runWorkLoop`, `beadRunOne`, `RunRegistry`, startup reconciliation | Identity facts must be sealed once and handles acquired before mode execution |
| Per-run counters/semaphores | broad `SharedHandles`, goroutine defers | One acquired per-run scope must release exactly once |
| Process/session | single/review/DOT mode bodies plus runloop/runlaunch leaves | Requires C2 phase owner |
| Terminal effects | `runexec.Run`, `RunBridge`, daemon hooks, queue advance, Bead adapter | Pure decision can remain separate from one serialized effect composer |

CQ-00 proves that direct queue mutations and persistence are spread across RPC,
workloop, budget, eager-fill, operator, startup, and CLI callers. CQ-02 fixes
the target owner: project `QueueStore` is the sole runtime writer, with only a
bounded startup adapter before installation.

### Queued/direct variants need a sum type

`RunEnv` currently permits `QueueName` with nullable `QueueID` and
`QueueGroupIndex`, plus an unconditional `QueueItemIndex`. This represents
forbidden mixed states. The target requires two constructor-complete variants:

- queued facts: queue name, queue ID, group index, item index, and immutable
  selected item are all present;
- direct facts: no queue identity or coordinates exist.

Shared claim-time facts (`RunID`, Bead record, workflow/mode selection,
placement, target branch) can be a common immutable payload. Queue coordinates
must not be nullable fields in that common payload.

### Existing seams worth retaining

- `internal/runexec.Dispatch` and `Run` are pure, tested decision machines.
- `internal/runloop` owns daemon-free ports and bounded leaves:
  `DispatchSegment`, `RunShell`, `WaitWithSocketGrace`, `PerRunEventTap`,
  scenario gate, reviewer-harness resolution, and transitional `RunBridge`.
- `internal/runlaunch` contains launch deadline/event/teardown capabilities.
- The established import direction is `internal/daemon ->
  internal/runloop`; runloop must not import daemon.

These are inputs, not proof that the graph is complete. Consumer-owned narrow
ports are the corpus pattern to follow; another broad bundle or service bag is
not.

## Risks, conflicts, and design constraints

- Moving `beadRunOne` or another hotspot intact would reverse the daemon/runloop
  direction because it still consumes daemon-private symbols.
- Queue candidates must be immutable inputs to the CQ transaction owner; a run
  constructor must not acquire a live queue pointer.
- Per-run handle acquisition can fail partway. Design needs an ordered scope
  whose rollback releases only successfully acquired handles.
- A direct plan must not bypass Beads/Run ownership merely because it lacks
  queue coordinates.
- Current comments claiming every bundle field is populated conflict with
  `runPorts`' deliberately nil fields; the target must rely on constructors,
  not comments.

## Patterns and decision status

Follow distinct constructors, immutable value records, consumer-owned
one-to-three-method ports, and an explicit per-run scope. Treat current bundle
assembly as a compiling migration state, not the target. No unresolved blocker
prevents design.
