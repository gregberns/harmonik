# ARCH-01 — Settle the immutable run-architecture contract

## Dispatch metadata

- Group / priority: run architecture / P0
- Execution profile: `sol_xhigh`
- Reviewer profile: `sol_xhigh`
- Depends on: `ARCH-00`, `RL-01`, `CQ-00`, `JR-00`
- Work type: kerf/spec architecture contract

## Objective

Define the target ownership graph that replaces temporal `workLoopDeps`
assembly and partial-valid `RunEnv`/`RunPorts`/`SharedHandles`. The contract
must separate outer-loop services, immutable per-run facts, process/session
lifecycle, mode execution, and durable terminal effects; evidence decides the
owner/type names and packages. This task settles contracts, not extraction.

## Evidence to verify first

Use `ARCH-00`, normative queue/Run specs, current `internal/runloop` ports,
production boot wiring, the reviewed reviewcycle/continuity work, and every
existing task that owns queue or terminal semantics.

## Exclusive lease

One kerf work, its finalized spec, and architecture evidence/review artifacts.
No Go production files.

## Required work

1. Define ownership and allowed dependency direction for the outer queue loop,
   immutable run plan, phase/session lifecycle, mode executors, and terminal
   effects without predetermining names unsupported by evidence.
2. Define queued versus direct-run variants so invalid identity/queue-field
   combinations cannot be constructed.
3. Define constructor completeness and forbid late nil population or generic
   service-locator bags.
4. Define process Wait/kill/reap ownership and durable transition ordering.
5. Reconcile, do not duplicate, existing runexec, runlaunch, runloop,
   reviewcycle, continuity, queue transaction, and LIFT contracts.
6. Produce a migration DAG with an accepted intermediate state after every task.
7. Record exact per-task symbol span, cognitive-complexity, field-reach, and
   import-budget targets consumed by `ARCH-GATE`; no worker sets its own bar.

## Acceptance

- Every mutable resource and durable state has one owner.
- Required dependencies are constructor-enforced.
- Single, review, and DOT share lifecycle contracts without a generic god port.
- The contract gives exact completion criteria for shrinking the five giant
  functions and replacing `workLoopDeps`.
- Queue and Run cross-group reviewers both approve.

## Verification

Kerf review/finalization, spec validation, dependency-direction review, and
cross-group architecture review.

## Escalate when

Stop for operator direction if the contract reopens a locked decision, requires
an incompatible queue schema, or conflicts with an already-finalized kerf work.

## Return

Use the directory return contract. **COMMIT EXPLICITLY.**
