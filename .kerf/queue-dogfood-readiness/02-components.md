# Components — Queue dogfood readiness

## Existing normative specs

### `specs/queue-model.md`

- **Change summary:** Define the durable failed-item recovery transition and
  make reservation release use the queue transaction owner.
- **Requirements:** Recovery must define which failed queue, group, and item
  states are eligible. It must durably re-arm only those items, clear or
  replace obsolete run identity, and preserve completed items as terminal.
  It must be safe to retry after a restart. The transaction must either finish
  before a new dispatch or leave a recoverable state. A raw writer must not
  clear a quarantine made by a failed durable write. Reservation undo and
  release must leave memory and persistent state equal.
- **Dependencies:** This is the shared contract for the daemon release paths.
  It depends on the shutdown discriminator in `execution-model.md` and on the
  command surface in `process-lifecycle.md`.

### `specs/execution-model.md`

- **Change summary:** Define the committed-but-unmerged DOT recovery state and
  its single owner.
- **Requirements:** A graceful stop after commit and before merge must either
  drain the run through the existing merge and terminal ladder, or persist a
  reviewable recovery state. The recovery state must distinguish a run that
  can resume the terminal ladder from a new dispatch. Reconstruction must not
  dispatch, merge, or close the work a second time.
- **Dependencies:** It aligns reconstruction and terminal-to-queue advance
  with `queue-model.md`, ordered shutdown in `operator-nfr.md`, and workspace
  retention in `workspace-model.md`.

### `specs/run-state-machine.md`

- **Change summary:** Confirm whether its shutdown-drain and merge-queue
  states remain normative. If they do, align them with the selected recovery
  state.
- **Requirements:** The state machine must name the committed-but-unmerged
  state, its durable handoff, and its allowed recovery transitions. It must
  exclude a second dispatch, merge, or close for the same run.
- **Dependencies:** Research first establishes the file's current normative
  status. If it is no longer normative, this area is removed and the decision
  is recorded in the integration contract.

### `specs/operator-nfr.md`

- **Change summary:** Extend ordered daemon drain to cover committed but
  unmerged work.
- **Requirements:** The drain contract must say when that work drains, when it
  is persisted for recovery, how timeout changes the result, and what restart
  may do next. Its evidence requirement must test the drain window under a
  controlled load.
- **Dependencies:** It depends on the execution recovery discriminator and
  feeds the lifecycle command and operational proof procedure.

### `specs/process-lifecycle.md`

- **Change summary:** Define the production recovery command and the boundary
  between a controlled batch and its assessor gate.
- **Requirements:** The command surface must state how a failed queue resumes,
  its availability and errors, and its relation to graceful stop and restart.
  A controlled batch must have one named decision scope. An assessor must audit
  that scope in a scratch daemon and must not consume normal queue work.
- **Dependencies:** The command invokes the queue recovery transition. The
  controlled-batch definition depends on the assessor handoff and the runbook.

### `specs/workspace-model.md`

- **Change summary:** Preserve the worktree lease and evidence for a
  committed-but-unmerged run until its selected recovery action completes.
- **Requirements:** A stop or restart must not reclaim the worktree before the
  drain or recovery owner completes. Recovery must retain the evidence needed
  to decide whether the terminal ladder can continue.
- **Dependencies:** It follows the execution recovery discriminator and the
  ordered drain contract.

### `specs/event-model.md`

- **Change summary:** Make recovery observable through an event or explicitly
  name `queue status` as the sole recovery observation.
- **Requirements:** Operators and the assessor must be able to tell that a
  queue entered failure, that recovery was requested, and whether it completed
  or was rejected. The chosen observation must preserve the queue, group, item,
  and run identity needed for an audit.
- **Dependencies:** It follows the queue state machine and lifecycle command.

### `specs/assessor-handoff-schema.md`

- **Change summary:** Make a controlled queue-readiness gate representable and
  auditable by the assessor schema.
- **Requirements:** The schema must support the readiness gate type or define
  its valid deploy-gate encoding. It must identify the gate owner and decision
  boundary. It must require machine-readable references to the candidate,
  exact canary profile, and retained proof artifacts. The assessor result must
  state PASS or BLOCK for only that profile.
- **Dependencies:** It follows the controlled-batch scope in
  `process-lifecycle.md`. It does not make the assessor a queue worker.

## Operational artifacts

### `docs/scratch-daemon-runbook.md`

- **Change summary:** Define how to run and retain the isolated readiness
  proof.
- **Requirements:** The runbook must require a verifiable candidate commit, a
  private scratch daemon, controlled daemon-suite load, and the retained Step
  9, core-loop, event, log, and batch artifacts. It must describe the
  assessor handoff without granting release authority.
- **Dependencies:** It follows blocker fixes and precedes the assessor gate.

### Assessor mission and report

- **Change summary:** Add a fresh, operator-authorized assessor mission only
  after registry and mission inspection.
- **Requirements:** The mission must use the assessor schema. It must name the
  branch, gate owner, report location, one-item canary profile, and proof
  artifacts. It must state that the assessor is separate from normal queue
  work.
- **Dependencies:** It follows the read-first, operator-authorized registry
  operation and the scratch proof.

### `plans/2026-07-27-delete-and-rewrite/LANES.md` and live handoffs

- **Change summary:** Record implementation ownership and the controlled
  activation sequence.
- **Requirements:** Alpha owns every daemon change and daemon test. Bravo owns
  queue, queuewiring, CLI, proof repairs outside the daemon, and ledger triage.
  The two owners agree on the release contract before either changes a shared
  boundary.
- **Dependencies:** Update after task design and before implementation starts.

## Deliberate non-areas

`specs/beads-integration.md` is not changed in this work unless research finds
a Beads transition that the recovery contract must define. Stale-bead review,
mission selection, and closure evidence are operational procedure. They do not
alone justify a normative Beads change.

No new spec file is needed. Existing specs already own queue state, run state,
shutdown, worktree retention, lifecycle commands, observations, and assessor
handoff. The integration pass will publish one cross-spec recovery matrix.

## Dependency map

1. Confirm the normative status of `run-state-machine.md` and trace the DOT
   shutdown, reconstruction, release, and failed-item recovery paths.
2. Define one durable recovery matrix across queue, execution, shutdown, and
   workspace ownership.
3. Define the lifecycle command and its observation.
4. Repair daemon shutdown drain and every daemon-side durability proof.
5. Repair queue and queuewiring durability, explicit failed-item recovery
   wiring, and ratchets that observe the durable boundary.
6. Run controlled daemon tests and set the controlled-load operating rule.
7. Verify or replace stale graph findings without changing ledger state during
   planning.
8. Inspect registry and mission state. Obtain operator authority for each
   reset, retirement, or new mission.
9. Run Step 9 and core-loop proof in a scratch daemon with the profile below.
10. Create the assessor handoff and run the independent gate. The operator
    then makes the narrow activation decision.

## First-canary profile

The first batch has exactly one initial queue item in one local stream group.
It permits no append. It runs at `max-concurrent=1` against the active local
repository. The item must be repeat-safe: a second run after a restart must not
change a user-facing system or another repository. It uses no remote or Pi
harness, no cross-repository work, and no wave group. The controlled activation
gate and mission, not the general queue model, enforce this temporary profile.

## Goal to area traceability

| Goal from problem space | Areas that satisfy it |
|---|---|
| DOT shutdown safety | execution model, run state, operator NFR, workspace model, daemon proof |
| Failed-item recovery | queue model, lifecycle command, event observation |
| Durable reservation behavior | queue model, daemon and queue proof |
| Trustworthy proof | owner-spec proofs, controlled-load runbook |
| Step 9 and core-loop evidence | runbook, lifecycle, assessor schema and report |
| Registry and mission hygiene | mission, runbook, operator-authorized procedure |
| Separate assessor gate | lifecycle, assessor schema and mission |
| Narrow first canary | lifecycle, runbook, mission, assessor schema and report |
