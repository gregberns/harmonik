# Decompose Review — `run-architecture-contract`

## Verdict

**APPROVE**

The corrected decomposition satisfies the Decompose pass criteria. It defines
one justified cross-cutting specification, maps every problem-space goal to a
concrete component, preserves the existing semantic owners, and provides a
complete dependency graph for later research and design.

## Prior Findings — Resolution

### 1. `handler-contract` lifecycle ownership — RESOLVED

`specs/handler-contract.md` is now present in the problem-space normative
boundaries, source-input set, new-spec `depends-on` list, read-only boundary
table, and dependency graph.

C2 correctly separates:

- handler-owned launch, `Handler` / `Session`, `Session.Wait`, watcher,
  progress observation, `InputPort`, and handler/session cancellation; from
- process-lifecycle-owned daemon process cleanup, shutdown, signal/kill,
  reap, close, and restart composition.

It also requires exact requirement citations for each lifecycle edge. Input
composition explicitly stops at the handler-owned `InputPort`, leaving
below-port behavior with `specs/agent-input.md` without unnecessarily adding
that implementation-detail owner to this contract's dependency set.

### 2. `event-model` authority — RESOLVED

`specs/event-model.md` is now present in the problem-space normative
boundaries, new-spec `depends-on` list, read-only boundary table, and
dependency graph.

C4 assigns the non-authority rule to `EV-INV-001` and distinguishes event
emission, JSONL durability/fsync ordering, observational replay, and
divergence evidence from authoritative queue, Run/git, and Beads facts. It
requires each transition row to cite both its domain owner and the exact
event-model requirement governing emitted or observed event edges.

### 3. New-spec metadata and category — RESOLVED

The decomposition fixes the new specification's corpus identity as:

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

This is the exact eight-spec read-only dependency set established by the
corrected boundary analysis. The decomposition also explicitly states that
the contract introduces no runtime subsystem and therefore has no `AR-053`
subsystem envelope.

## Criteria Assessment

| Decompose criterion | Assessment |
| --- | --- |
| Each affected spec has scope, post-change requirements, and dependencies | **Met.** The new spec has a fixed filename, identity, category, exact dependency set, and concrete C1–C6 requirements. All eight existing specifications are identified as read-only semantic owners with their consumed scope. |
| Every problem-space goal maps to a spec area | **Met.** G1–G8 map completely to C1–C6. |
| No unjustified spec area | **Met.** One foundation-cross-cutting contract is proportionate; C1–C6 are sections of it rather than invented runtime subsystems. |
| Requirements describe the desired state, not textual edits | **Met.** The component requirements are behavioral and structural postconditions. |
| All relevant existing specs are accounted for | **Met.** Architecture, execution model, Run state machine, handler contract, queue model, process lifecycle, Beads integration, and event model cover the normative surfaces this work composes. |
| Dependencies are correct | **Met.** C1 establishes ownership before C2/C3; C2 composes handler/process lifecycle; C4 follows the C2/C3 result boundary and preserves queue/Run/Beads/event authorities; C5 converges before C6; C6 produces the migration/gate contract last. |

## Review Notes for Later Passes

These are confirmation points, not Decompose blockers:

- Research should preserve the C2 distinction between `Session.Wait` and
  daemon-level process wait/reap duties.
- C4 transition rows should use exact requirement identifiers rather than
  spec-level citations.
- C5 should treat every ARCH-00 LIFT artifact and both RL-01 open choices as
  exhaustive-set obligations.
- C6 should retain literal integer targets and reproducible baseline commands,
  not qualitative improvement goals.

## Review Boundary

This review changes no production code, existing specification, task card,
task index, or Kerf status. The Decompose pass is approved for the work owner
to advance to Research.
