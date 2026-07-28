# C5 research — Existing-contract and LIFT dispositions

## Questions

1. Which extracted artifacts are production-active?
2. Which reviewed artifacts are unused or compatibility-only?
3. What did each LIFT level actually deliver?
4. Which planned moves are blocked?
5. What evidence must each disposition preserve?

## Findings

### Active production inputs

ARCH-00 proves these are landed and production-reachable:

- `internal/runexec` Dispatch/Run machines — pure decisions;
- `internal/runlaunch` deadline/event/teardown leaves;
- `internal/runloop` ports, `PerRunEventTap`,
  `WaitPostAgentReadyProgress`, `WaitWithSocketGrace`, `RunShell`,
  `DispatchSegment`, scenario gate, `RunBridge`, and reviewer-harness
  resolution;
- P2/RT leaf packages for merge, harness, transport, worker, and project
  configuration.

Their residual limitation is consistent: decisions or mechanism leaves moved,
but the giant coordinators still select effects and own lifecycle. They should
be evaluated for `adopt` or `adapt`, never reimplemented in parallel.

### Exhaustive LIFT state

| Artifact | Evidence state | Research constraint |
| --- | --- | --- |
| L0 ports | landed boundary | adapt into constructor-complete narrow ports |
| L1 event tap | landed, called | adopt fan-out; migrate to owned subscriptions |
| L2 post-ready wait | landed leaf | adopt beneath lifecycle owner |
| L3 socket grace | landed, called | adopt observation leaf |
| L4 RunShell/DispatchSegment | landed, called | adopt bounded effect execution |
| L5 scenario gate | landed, tested | adopt leaf, not coordinator |
| L6 RunBridge | landed, called | adapt transitional bridge; forbid growth |
| L7 reviewer harness | landed, called | adopt pure resolution policy |
| L8 reviewloop relocation | uncommitted and uncompilable | retire move-as-written; preserve tests/census |
| L9–L13 | planned/unlanded | defer or supersede by dependency-first DAG; no delivery claim |

The L8 snapshot has 26 daemon-private symbols at 48 typed sites; its dirty move
fails compile. A future relocation gate requires zero daemon-private undefined
symbols before moving the coordinator.

### RL-01 reconciliation

- R0 is staged test-only characterization: adopt as a gate.
- R1 `reviewcycle` is reviewed, pure, and unused: either adopt into review mode
  or retire; no competing kernel.
- R2 `continuity` is reviewed and unused while some core vocabulary is active
  but unproduced: adopt service and vocabulary together or retire together.
- R3 registration/subscription owners are reviewed and compatibility-reachable:
  adopt owners and migrate callers explicitly; remaining phase-scope work is
  absent.
- R4–R9 are planned owner boundaries only.
- R10 thin coordinator/L8 relocation is unlanded and blocked.
- R11/R12 are acceptance/release campaigns, not architecture atoms.

### Queue owners

CQ-00's direct writer inventory is current-state evidence, not a set of owners
to preserve. CQ-02's QueueStore transaction API and bounded startup adapter are
the only target owners. Every direct persistence/mutation caller must be
adapted to that API or retired.

## Required disposition-row evidence

Every design row needs artifact/path, ancestry/production use, semantic value,
target owner, `adopt|adapt|retire|defer`, migration consumer/task, and a
deletion/convergence test. Grouping is permitted only where ARCH-00 proves one
atomic artifact family; “keep for later” is invalid.

## Risks and decision status

The main risks are treating staged code as landed, calling a compatibility API
an owned lifecycle, growing `RunBridge`, or resuming L8 before its dependency
census reaches zero. The evidence is complete enough for design; no unresolved
blocker remains.
