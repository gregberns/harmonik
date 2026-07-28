# C1 design — Construction graph and ownership

## Current state

No existing spec composes the complete run dependency graph. Production uses
temporally completed `workLoopDeps`, `RunEnv`, `RunPorts`, and
`SharedHandles`; queued identity is represented by nullable fields; queue,
Run, Bead, handle, lifecycle, and terminal ownership cross the two giant
workloop functions.

## Target state

The new RAC spec will define this one-way graph:

```text
daemon composition root
  -> OuterQueueLoop
      -> immutable QueuedRunPlan | immutable DirectRunPlan
          -> PerRunScope
              -> ModeExecutor
                  -> PhaseLifecycle
              -> TerminalComposer
```

### Owners

- `OuterQueueLoop` owns maintenance cadence, gates, fleet projection, source
  arbitration, capacity, and selection. It never persists queue state.
- CQ-02 `QueueStore` transaction owner alone reserves or mutates a queue.
- A `RunPlanFactory` seals Run ID, Bead, target/worktree intent, mode,
  placement, and direct/queued origin exactly once.
- `PerRunScope` owns acquired registry entry, local-in-flight count, semaphore
  permit, worker lease, worktree scope, and lifecycle/terminal handles. Its
  ordered close releases each acquired handle once.
- C2 owns session/process phase resources; C3 mode policy; C4 durable
  composition.

### Unrepresentable invalid states

RAC will require distinct constructors:

```text
NewQueuedRunPlan(CommonFacts, QueuedOrigin) -> QueuedRunPlan
NewDirectRunPlan(CommonFacts, DirectOrigin) -> DirectRunPlan
```

`QueuedOrigin` contains non-null queue name/ID/group/item coordinates and the
immutable selected candidate. `DirectOrigin` has no queue fields. Neither plan
exposes setters. Required ports are constructor arguments; optional behavior
uses explicit variants, never nil.

### Dependency rules

- extracted run packages may import core/leaf packages, never
  `internal/daemon`;
- lifecycle is mode-neutral and does not import modes;
- modes consume lifecycle and narrow mechanism ports;
- terminal composition consumes mode results and owner ports, not concrete
  stores;
- no live queue pointer, service locator, raw `*exec.Cmd`, or generic cleanup
  bag crosses the graph.

### Migration treatment

`workLoopDeps` and the three bundles are accepted only as transitional
adapters. New constructors are introduced alongside them, callers migrate
leaf-first, and the adapters are deleted after zero production uses. No new
field may be added to them during migration except a behavior-preserving
compatibility shim paired with a deletion node.

### Locked-decision preservation target

RAC and ARCH-01 evidence will carry this complete, literal mapping:

| ID | Locked decision (verbatim) | Preservation target |
| ---: | --- | --- |
| 1 | Go implementation. | All target owners and ports are Go types/packages. |
| 2 | Go-native orchestrator. | OuterQueueLoop and composition remain Go-native. |
| 3 | In-process pub/sub plus JSONL source of truth. | Event edges retain the bus/JSONL contract and EV-INV-001 authority limit. |
| 4 | tmux-inspectable NTM runner. | tmux remains a C2 local adapter/inspection surface, not policy owner. |
| 5 | Claude Code and Pi handlers. | Handler choice remains behind the unchanged HC seam. |
| 6 | Separate twin binaries. | RAC adds no in-process twin branch or merged binary. |
| 7 | Workflow worktrees and merges without agent-mail reservations. | C1 scope and C4 merge ordering retain existing owners and add no reservation service. |
| 8 | CASS-only initial memory. | No new memory owner or retrieval subsystem is introduced. |
| 9 | No verifier subsystem. | Gates remain mechanism/role functions; RAC creates no verifier package. |
| 10 | Operator controls between tasks. | Outer-loop gates preserve operator control boundaries. |

The later locked rule “Beads owns terminal Bead transitions” appears
separately and literally in C4's terminal-Bead row with BI-010, BI-010a, and
BI-029–BI-031 as authority. Evidence `locked_decisions` rows use exactly the
ten strings above and `impact: preserved`.

## Rationale

This design converts C1 research's temporal validity into type-enforced
validity, preserves CQ-02 ownership, and follows the existing consumer-owned
port/import-direction pattern without inventing a subsystem.

## Requirements traceability

| C1 requirement | Target |
| --- | --- |
| One owner per resource/state | Owner list and PerRunScope |
| Decision/effect/serialization/recovery distinction | C1 owner split plus C4 |
| Distinct queued/direct constructors | Sum-type plan constructors |
| Separate immutable facts and mutable services | Plans versus PerRunScope |
| Forbid nil/late population/service bag | Constructor and dependency rules |
| Dispose current bundles | Transitional-adapter deletion |
| Preserve package direction | Explicit import rules |
| Preserve locked decisions | Complete literal mapping |
