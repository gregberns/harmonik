# Core architectural review — 2026-08-09

Reviewer: agent `charlie`

## Purpose

This review asks whether the current core is a safe base for more work.
It does not ask whether the whole product is complete.

The charter defines the core as this path:

> config → event bus → queue → bead-ledger adapter → worktrees → harness registry + one substrate → work loop → merge

The review starts with this path. It then checks daemon survival where that path depends on it.

## Review process

### 1. Fix the scope

Use `CHARTER.md` section 3 as the scope source.
Resolve each stage to its live packages and composition code.
Do not add a subsystem because the current code imports it.
Treat that import as possible coupling until the code proves otherwise.

### 2. Trace one bead

Follow one queued bead from submit to merge.
Record every state owner, durable write, process launch, lock, and recovery action.
Mark each place that can close, reopen, retry, or abandon the bead.

### 3. Test each engineering principle

Score the path against `PRINCIPLES.md`.

1. Put effects at the edge.
2. Parse input into safe types once.
3. Give every admitted input a defined result.
4. Let consumers own narrow ports.
5. Compose small functions.
6. Use one writer and one explicit state machine.
7. Keep only tests that defend a claim.
8. Make behavior repeatable.
9. Prove one vertical path before wider work.

### 4. Test the boundary

Try to name and build the core without optional subsystems.
Inspect imports at the package boundary.
Inspect the composition root for objects that exist while their subsystem is off.
Treat a test-only boundary as weak evidence.

### 5. Test failure ownership

For each external write, identify the owner of rollback or recovery.
Check process death between adjacent writes.
Check daemon stop during a live run.
Check restart with memory, disk, and the bead ledger in different states.

### 6. Test the tests

Map each critical claim to a test.
Confirm that the test reaches the claimed production path.
Use a deliberate break only when the test can run safely in isolation.
Do not count a green suite as proof without this check.

### 7. Rank findings

Use these levels:

- **Critical:** More work will deepen the wrong architecture or risk lost work.
- **High:** The defect blocks a clean core boundary or makes failures hard to reason about.
- **Medium:** The defect raises cost or weakens proof, but it does not control the main shape.
- **Low:** Local cleanup with no clear effect on the core design.

Rank structure before local style.
Rank the queue and bead path before daemon support code.

### 8. Define an exit test

The core is ready for more feature work when one vertical bead path has these properties:

- A small composition root can construct it without optional subsystems.
- One explicit run state machine owns every bead outcome.
- One durable transaction owner coordinates queue and ledger state.
- The scheduler selects work but does not execute run policy.
- The run shell performs effects but does not decide lifecycle policy.
- Focused tests prove each transition and one real end-to-end bead run.

## Artifacts

- `EVIDENCE.md` records current-tree facts and commands.
- `FINDINGS.md` ranks the architectural findings.
- `NEXT.md` gives the next review slices and the stop conditions.

## Limits of this pass

This pass uses the current checkout at commit `d5a12348f`.
It reads the live production path and its package boundaries.
It does not validate every old issue or every test.
It does not claim that a static finding causes a live failure.

The current branch was already one commit ahead of its remote.
This review does not change or reinterpret that commit.
