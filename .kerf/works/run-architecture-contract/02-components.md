# Run Architecture Contract — Decomposition

> Pass 2 (`decompose`) of `run-architecture-contract`. This work produces one
> new normative specification. The eight existing boundary specifications are
> inputs and remain unchanged; the new contract composes their already-owned
> semantics by citation.

## 1. Decomposition strategy

The production problem is one composition problem, not six independent spec
changes. Ownership, construction, lifecycle, execution, terminal effects, and
migration must agree on one acyclic graph. Splitting those decisions across
existing specifications would reproduce the ambiguity this work is intended
to remove.

The only normative output is:

- `specs/run-architecture-contract.md` — a new cross-boundary architecture
  contract for the run path.

Its corpus identity is fixed at this pass:

```yaml
spec-id: run-architecture-contract
requirement-prefix: RAC
spec-category: foundation-cross-cutting
depends-on:
  - architecture
  - execution-model
  - run-state-machine
  - handler-contract
  - queue-model
  - process-lifecycle
  - beads-integration
  - event-model
```

The spec introduces no runtime subsystem. As a
`foundation-cross-cutting` spec it has no AR-053 subsystem envelope.

The contract is decomposed into six research/design components. They are
sections of that one specification, not new packages or independently owned
subsystems.

| Component | Contract concern | Goals covered |
| --- | --- | --- |
| C1 | Construction graph and ownership | G1, G2, G3 |
| C2 | Process/session lifecycle | G1, G4 |
| C3 | Mode execution boundaries | G2, G4 |
| C4 | Durable terminal and recovery composition | G1, G5 |
| C5 | Existing-contract and LIFT dispositions | G6 |
| C6 | Migration DAG and structural gates | G7, G8 |

## 2. New specification components

### C1 — Construction graph and ownership

Defines the allowed dependency direction from the outer queue loop to a
constructor-complete run plan, per-run handles, lifecycle execution, mode
execution, terminal effects, and recovery.

It must:

- assign exactly one owner to every mutable run-path resource and durable
  state;
- distinguish decision, effect, serialization, and recovery ownership;
- define distinct queued and direct plan constructors so queue identity and
  queue coordinates cannot be absent or spuriously present;
- separate immutable claim-time facts from registries, counters, semaphores,
  process/session handles, and effectful factories;
- forbid nil-required fields, post-construction required-field population, a
  generic service locator, and a replacement god-port bundle;
- define the final disposition of `workLoopDeps`, `RunEnv`, `RunPorts`, and
  `SharedHandles`; and
- state package dependency directions, including the existing
  `internal/daemon -> internal/runloop` direction and the prohibition on a
  reverse import.

Research must trace the actual constructors and every field consumer before
the design assigns owners.

### C2 — Process/session lifecycle

Defines the shared lifecycle shell used by single, review, and DOT modes
without transferring mode policy into lifecycle code.

It must cover:

- construction, registration, launch, observation, input, cancellation,
  shutdown, Wait, signal/kill, reap, close, and session cleanup;
- ownership and lifetime of process/session handles, event subscriptions,
  watchers, inputs, join/wait obligations, and sleep/cancel controls;
- the boundary between process facts, session facts, Run registry facts, and
  mode results;
- failure and cancellation behavior before launch, during launch, while
  running, during Wait/reap, and during cleanup; and
- restart behavior when durable Run facts exist but process/session state is
  absent, stale, or only partially observed.

`specs/handler-contract.md` owns handler launch, the `Handler` / `Session`
interfaces, `Session.Wait`, the daemon-owned watcher, progress observation,
the `InputPort` seam, and handler/session cancellation timing.
`specs/process-lifecycle.md` owns daemon-level process cleanup, shutdown,
signal/kill, reap, close, and restart composition. C2 must cite the exact
existing requirement that owns each lifecycle edge rather than treating
either specification as owner of the combined surface.

Input composition in this contract stops at the `handler-contract`
`InputPort` boundary. Behavior below that port remains owned by
`specs/agent-input.md` and is neither redefined nor made a dependency of this
cross-cutting contract.

### C3 — Mode execution boundaries

Defines a mode-neutral invocation/result contract and narrow mode-specific
executors for single, review, and DOT execution.

It must:

- state which lifecycle capabilities each mode may consume;
- keep lifecycle orchestration independent of mode implementations;
- preserve each mode's workflow, checkpoint, review, and DOT semantics;
- define how mode success, failure, cancellation, retry, and needs-attention
  results feed the terminal/recovery component; and
- establish concrete ownership for the current `runWorkLoop`,
  `beadRunOne`, `runReviewLoop`, `driveDotWorkflow`, and
  `dispatchDotAgenticNode` responsibilities rather than merely relocating
  those functions intact.

The component must not invent a generic handler or durability layer.

### C4 — Durable terminal and recovery composition

Produces the complete transition table from admission/claim through terminal
effects, queue completion, cleanup, and restart recovery.

For every durable transition the table must name:

1. the decision owner;
2. the effect owner;
3. the serialization owner;
4. the recovery owner; and
5. the existing normative requirement or finalized contract that owns the
   semantics.

It must preserve CQ-02 transaction ordering, QueueStore single-writer
semantics, completion receipts, and recovery decisions exactly. It must also
preserve Run terminal-ledger ordering, Beads terminal ownership, and the rule
that events are diagnostic observations rather than replayed durable
authority.

`specs/event-model.md` EV-INV-001 owns the event-authority rule. Its emission,
JSONL durability, fsync ordering, observational replay, and divergence-
evidence requirements remain distinct from the authoritative queue, Run/git,
and Beads facts used for reconstruction. Each transition row must cite both
the domain owner of the transition and the exact event-model requirement for
any emitted or observed event edge.

The table must include failure and cancellation seams and all crash windows
identified by JR-00. If an existing boundary leaves a semantic contradiction,
this work stops instead of resolving it by amendment.

### C5 — Existing-contract and LIFT dispositions

Provides an exhaustive `adopt`, `adapt`, `retire`, or `defer` matrix for:

- `internal/runexec`;
- `internal/runlaunch`;
- `internal/runloop`;
- `internal/reviewcycle`;
- `internal/continuity`;
- current queue-owner artifacts; and
- every ARCH-00 LIFT artifact, including landed safe leaves, transitional
  bridges, the blocked L8 relocation, and planned/unlanded L9–L13 work.

Each row must state current production use, preserved semantics, target owner,
migration consumer, and deletion or convergence condition. The RL-01 open
choices for reviewed-but-unused `reviewcycle` and `continuity` contracts must
be decided explicitly; “available for later” is not a disposition.

### C6 — Migration DAG and structural gates

Turns the target contract into an implementable, collision-safe sequence.

Every migration node must name:

- an existing task ID or an evidence-only missing-task proposal;
- prerequisites;
- exact writable paths;
- collision-lease family;
- accepted compiling and testable intermediate state;
- rollback boundary; and
- the contract condition unlocked by the node.

The DAG must respect current active spines and CQ-02/JR ordering. It must not
create task records in `TASK-INDEX.yaml`.

The same component defines reproducible baselines and literal integer
ARCH-GATE targets for:

- source span;
- cognitive complexity;
- dependency/field reach; and
- forbidden imports.

Targets must cover `workLoopDeps`, `runWorkLoop`, `beadRunOne`,
`runReviewLoop`, `driveDotWorkflow`, and `dispatchDotAgenticNode`. Completion
criteria must prove that temporal `workLoopDeps` assembly is eliminated and
that the five hotspot functions are decomposed by responsibility, not moved
unchanged.

## 3. Existing normative boundaries — read only

| Existing specification | Semantics consumed by the new contract | ARCH-01 disposition |
| --- | --- | --- |
| `specs/architecture.md` | Central controller, Go-native architecture, mechanism/policy direction, and no verifier subsystem | Cite unchanged |
| `specs/execution-model.md` | Run identity, sealed claim-time facts, workflow modes, terminal events, and ledger ordering | Cite unchanged |
| `specs/run-state-machine.md` | Pure Dispatch/Run reactors, daemon shell, consumer-owned ports, shared-reference limits, and terminal spine | Cite unchanged |
| `specs/handler-contract.md` | Handler launch, `Handler` / `Session`, `Session.Wait`, daemon-owned watcher, progress observation, `InputPort`, and handler/session cancellation | Cite exact lifecycle requirements unchanged; input composition stops at `InputPort` |
| `specs/queue-model.md` | QueueStore ownership, queue lifecycle, persistence, completion receipts, cleanup, serialization, and recovery | Cite unchanged |
| `specs/process-lifecycle.md` | Daemon-level process startup/cleanup, shutdown, signal/kill, reap, close, and restart composition | Cite exact process requirements unchanged |
| `specs/beads-integration.md` | Beads adapter boundary and terminal Bead-transition ownership | Cite unchanged |
| `specs/event-model.md` | Event envelope/emission durability, fsync ordering, observational replay, divergence evidence, and EV-INV-001 non-authority | Cite exact event requirements unchanged |

No amendment to these files is in scope. A discovered contradiction is a
blocking design finding, not permission to edit an existing specification.

## 4. Dependency map

```text
architecture + execution-model + run-state-machine
                         |
                         v
             C1 construction/ownership
                |                 |
                v                 v
handler-contract +          C3 mode executors
process-lifecycle
         |
         v
 C2 lifecycle shell
                \                 /
                 v               v
queue-model + CQ-02 + JR-00 + beads-integration + event-model
                         |
                         v
              C4 terminal/recovery

ARCH-00 + RL-01 --------> C5 dispositions

C1 + C2 + C3 + C4 + C5 -> C6 migration/gates
                         |
                         v
          specs/run-architecture-contract.md
```

C1 establishes directions and owners before C2–C4 can settle interfaces. C2
and C3 may be researched in parallel but their boundary is designed together.
C2 depends directly on the unchanged handler and process-lifecycle contracts
and stops input composition at the handler-owned `InputPort`. C4 depends on
C2/C3 result vocabulary and on the unchanged queue, Run, Beads, process, and
event contracts. C5 can be researched independently but must converge before
C6 maps artifacts to migration nodes. C6 is last because its intermediate
states must be valid under the complete target graph.

## 5. Goal coverage and exclusions

| Problem-space goal | Components |
| --- | --- |
| One owner for every mutable resource and durable state | C1, C4 |
| Acyclic dependency direction | C1, C2, C3 |
| Constructor-complete queued/direct variants | C1 |
| Shared lifecycle with narrow mode boundaries | C2, C3 |
| Preserve CQ-02 and cite every durable transition | C4 |
| Explicit disposition for existing and LIFT artifacts | C5 |
| Acyclic, collision-safe migration DAG | C6 |
| Literal reproducible structural targets | C6 |

Explicitly excluded are production edits, amendments to existing normative
specifications, new durability/verifier subsystems, queue semantic changes,
new task records, and Kerf finalization.

## 6. Decompose-review questions

An independent reviewer should confirm:

1. one new normative spec is sufficient and the eight existing specs are
   correctly treated as read-only semantic owners;
2. C1–C6 cover every goal without creating a new subsystem;
3. lifecycle and mode execution have a non-cyclic boundary;
4. queue, terminal, Beads, and recovery semantics remain with their existing
   owners;
5. the disposition matrix includes every required artifact family; and
6. the migration/gate component is concrete enough to require exact tasks,
   paths, leases, intermediate states, rollback points, baselines, and integer
   targets in later passes.
