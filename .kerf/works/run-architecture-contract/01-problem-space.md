# Problem Space — Immutable Run Architecture Contract

## Summary

Harmonik’s production run path has accumulated a temporal construction graph:
`workLoopDeps` is created incomplete, boot wiring injects more services later,
and `runWorkLoop` finishes broad `RunEnv`, `RunPorts`, and `SharedHandles`
bundles immediately before calling `beadRunOne`. The resulting validity
contract is “fields happen to be populated before use,” not a constructor-
enforced type boundary.

At the same time, queue admission and maintenance, Run identity, process and
session lifecycle, workflow-mode execution, durable terminal effects, and
restart recovery meet inside a few very large functions. Decision ownership,
effect ownership, serialization ownership, and recovery ownership are split
but not expressed by one normative dependency graph. This makes later
decomposition risky: moving a function or field can silently move an existing
semantic owner, introduce a second durable-effect owner, or create a cycle
between the daemon shell and extracted packages.

This work defines a new `run-architecture-contract` specification. It will
settle ownership, construction, dependency direction, migration states, and
literal structural targets without changing existing queue, Run, Beads,
event, or process-lifecycle behavior.

The operator-approved ARCH-01 task card supplies and confirms this problem
space, its scope, and its constraints.

## Why This Must Change

- `workLoopDeps` has 81 fields and is valid only after staged mutation across
  boot and workloop helpers.
- `RunEnv`, `RunPorts`, and `SharedHandles` mix immutable facts, mutable
  lifecycle capabilities, registries, factories, and terminal-effect handles.
- `runWorkLoop`, `beadRunOne`, `runReviewLoop`, `driveDotWorkflow`, and
  `dispatchDotAgenticNode` remain structural hotspots; relocating them intact
  would preserve the same coupling.
- Single, review, and DOT execution repeat or partially share process/session
  lifecycle behavior without one mode-neutral lifecycle contract.
- Durable transitions span queue state, Beads, Run records, event
  observations, process/session facts, and recovery. Each domain already has
  normative owners, but their composition is not captured in one
  architecture contract.
- Reviewed `runexec`, `runlaunch`, `runloop`, `reviewcycle`, `continuity`, and
  LIFT artifacts contain useful seams and incomplete or superseded directions
  that need explicit adopt/adapt/retire/defer dispositions.

## Goals

1. Define exactly one owner for every mutable resource and durable state in
   the run path, while distinguishing decision, effect, serialization, and
   recovery ownership.
2. Define an allowed dependency direction from the outer queue loop through
   immutable run plans, per-run handles, lifecycle execution, mode execution,
   and terminal effects.
3. Define separate constructor-enforced queued and direct run variants so
   invalid queue identity and queue-coordinate combinations cannot be
   represented.
4. Define a shared lifecycle contract for single, review, and DOT execution
   without a generic service-locator or god-port bundle.
5. Preserve every CQ-02 queue transaction and recovery decision and cite the
   existing normative owner for every durable transition.
6. Classify existing extracted contracts and every LIFT artifact as
   `adopt`, `adapt`, `retire`, or `defer`, including all RL-01 open
   adopt/retire decisions.
7. Produce an acyclic migration DAG whose nodes have exact prerequisites,
   writable paths, lease families, accepted compiling/testable intermediate
   states, and rollback boundaries.
8. Establish literal numeric ARCH-GATE targets for span, cognitive
   complexity, dependency/field reach, and forbidden imports at every
   structural hotspot.

## Non-Goals

- No production Go refactor, test change, task-index edit, or task-card edit.
- No change to queue schemas, queue transaction ordering, completion receipts,
  queue recovery, or QueueStore single-writer semantics.
- No change to Run identity, workflow behavior, checkpoint behavior, event
  payloads, terminal ledger ordering, Beads transitions, or process Wait /
  signal / reap semantics.
- No new verifier subsystem, generic durability layer, generic god port,
  service locator, multi-process queue writer, or cross-domain transaction.
- No reopening of the ten locked platform decisions or the later rule that
  Beads owns terminal Bead transitions.
- No claim that moving a large function unchanged satisfies decomposition.
- No creation of missing task records in `TASK-INDEX.yaml`; missing slices are
  proposals in ARCH-01 evidence only.
- No `kerf finalize`.

## Constraints

### Normative boundaries

- `specs/architecture.md` owns daemon/process and dependency invariants.
- `specs/execution-model.md` owns Run identity, sealed claim-time facts,
  workflow modes, terminal events, and terminal ledger ordering.
- `specs/run-state-machine.md` owns pure reactors, the daemon shell,
  consumer-owned ports, permitted shared references, and terminal spine.
- `specs/handler-contract.md` owns handler launch, the `Handler` / `Session`
  interfaces, daemon-owned watcher, progress observation, `Session.Wait`,
  `InputPort` seam, and handler/session cancellation timing.
- `specs/queue-model.md` and the finalized CQ-02 contract own queue
  transactions, persistence, observation ordering, cleanup, and recovery.
- `specs/process-lifecycle.md` owns process Wait, signal/kill, reap, close, and
  restart composition.
- `specs/beads-integration.md` owns terminal Bead transitions.
- `specs/event-model.md` owns event envelopes, emission durability and
  ordering, observational replay, and the prohibition on using events as
  authoritative state reconstruction.

The new specification composes these contracts by citation. If composition
requires amending one, ARCH-01 stops rather than widening scope.

### Locked decisions

The contract preserves Go, a Go-native orchestrator, in-process pub/sub plus
JSONL source of truth, tmux-inspectable NTM execution, Claude Code and Pi
handlers, separate twin binaries, workflow worktrees/merges without
agent-mail reservations, CASS-only initial memory, no verifier subsystem, and
operator controls between tasks.

### Construction and ownership

- Required dependencies are constructor-enforced.
- Queued and direct plan variants are distinct.
- Immutable facts do not depend on mutable lifecycle services.
- Mode executors depend on narrow lifecycle and terminal interfaces; the
  lifecycle shell does not import mode implementations.
- A durable transition has one decision owner, one effect owner, one
  serialization owner, and one recovery owner, with an existing normative
  citation.
- Every active semantic-writer spine is serialized.

### Work and packaging

Writes are limited to the Kerf work, the eventual byte-identical normative
draft, one exact `RAC` registry row after approval, and ARCH-01 evidence.
Independent queue-contract and Run/lifecycle reviewers must approve before
packaging. The finalization command is intentionally not used.

## Success Criteria

The completed specification and evidence:

- enumerate the complete ownership and dependency graph from queue-loop
  admission through recovery;
- make invalid queued/direct identity combinations unrepresentable;
- make incomplete late-populated run bundles non-conforming;
- define common lifecycle transitions and mode-specific execution boundaries
  for single, review, and DOT modes;
- preserve existing failure, cancellation, shutdown-drain, terminal, cleanup,
  and restart semantics by exact normative citation;
- give every existing contract/LIFT artifact one explicit disposition;
- provide an acyclic, collision-safe migration DAG with testable intermediate
  states and rollback boundaries;
- provide reproducible baselines and literal integer targets for every
  ARCH-GATE consumer;
- state exact completion tests for removal of temporal `workLoopDeps`
  assembly and decomposition of all five named hotspot functions;
- contain no production or out-of-lease change; and
- receive independent APPROVE verdicts from both required reviewers.

## Preliminary Spec Areas

1. Ownership and dependency direction.
2. Immutable queued/direct run-plan variants.
3. Per-run identity, registry, counter, and semaphore handles.
4. Process/session lifecycle state machine.
5. Single, review, and DOT mode execution boundaries.
6. Durable terminal-effect and recovery composition.
7. Existing-contract and LIFT disposition matrix.
8. Migration DAG, collision rules, and accepted intermediate states.
9. Reproducible structural baselines and ARCH-GATE targets.

## Source Inputs

- ARCH-00 live run-architecture graph and structural baselines.
- RL-01 review-loop decomposition reconciliation and LIFT dispositions.
- CQ-00 queue lifecycle and persistence caller inventory.
- JR-00 claim-to-terminal ownership and crash-window trace.
- Finalized CQ-02 queue transaction/durability contract and independent
  reviews.
- The eight normative boundary specifications listed above.
