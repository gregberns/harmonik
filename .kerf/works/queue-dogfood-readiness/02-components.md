# Components — Queue dogfood readiness

## Affected existing specs

### `specs/queue-model.md`

- **Change summary:** Define the durable recovery behavior that a production
  `queue resume` command exposes. Align failed-item re-arm behavior with the
  queue state machine and queue mutation owner.
- **Requirements:**
  - A failed queue must have one documented recovery path.
  - Recovery must re-arm the intended failed items through the durable queue
    transaction boundary before new dispatch occurs.
  - Raw writes must not clear a quarantine created by a failed durable write.
  - Reservation release and undo must leave memory and persistent state equal.
  - The recovery transition must remain distinct from handler-type resume.
- **Dependencies:** `process-lifecycle.md` owns the command and RPC surface.
  `event-model.md` owns the recovery event. `handler-pause.md` must preserve
  control-plane separation. The recovery-wiring task needs the current queue
  state-machine reading. It does not depend on `queue-status-writer`, which
  intentionally does not wire failed-item resume.

### `specs/execution-model.md`

- **Change summary:** State the daemon behavior for a DOT run that stops after
  commit and before merge.
- **Requirements:**
  - Shutdown must either drain a committed run through its release path or
    retain a durable, reviewable recovery state.
  - Before release begins, the daemon must write an immutable Git-backed
    release claim in the committed transition record. The claim must retain
    the dispatch-head SHA, the resolved merge-target ref and SHA, and, for a
    remote worker, its worker name, host, and repository path.
  - Restart must use that claim and the current Bead state to reconstruct
    unfinished release. JSONL and a daemon-local registry must not supply a
    missing release fact.
  - The daemon must not silently redispatch work that already committed.
  - The proof must cover daemon stop during this window.
- **Dependencies:** Alpha owns the daemon implementation and tests. The queue
  recovery contract must agree on the release-state handoff. The release-claim
  checkpoint contract is a shared core boundary and must land before restart
  reconstruction. The detailed terminal-spine and merge rules remain in
  `run-state-machine.md`.

### `specs/run-state-machine.md`

- **Change summary:** Align the shutdown-drain terminal edge with the remote
  branch synchronization that a committed DOT run needs before merge.
- **Requirements:**
  - A shutdown drain must use a context that remains live after daemon
    cancellation for every required recovery operation.
  - A remote run branch must synchronize before a drain merge when the commit
    is not already local.
  - A failed synchronization must reopen the work item. It must not close it.
  - The terminal spine must retain its one close-or-reopen outcome.
- **Dependencies:** This contract refines `execution-model.md` and the
  shutdown order in `operator-nfr.md`. It is complete in code but needs
  normative alignment before the readiness gate can claim the behavior.

### `specs/operator-nfr.md`

- **Change summary:** Define the shutdown-drain ordering that protects a
  committed graph run and the controlled-load rule for daemon test evidence.
- **Requirements:**
  - Shutdown order must prevent a committed DOT run from being silently
    redispatched or closed before its durable merge outcome is known.
  - The procedure must distinguish a merge success from a durable reopen that
    an operator can review.
  - Daemon-suite evidence must record machine load and state the allowed test
    concurrency for a readiness result.
- **Dependencies:** The per-run behavior is owned by `run-state-machine.md`.
  The queue pause transition remains owned by `queue-model.md`.

### `specs/process-lifecycle.md`

- **Change summary:** Define the operator command and RPC boundary for failed
  queue recovery. Record the boundary between a controlled batch, shutdown
  recovery, and a separate assessor gate.
- **Requirements:**
  - The recovery command must say which queue and failed items it changes.
  - It must report success only after the durable queue transition succeeds.
  - It must state the daemon-down result and must not imply that a handler
    resume or daemon restart has recovered a failed queue.
  - The assessor launch contract must remain separate from normal queue work.
  - The controlled-batch procedure must retain Step 9 and core-loop proof
    artifacts for the gate.
- **Dependencies:** `queue-model.md` owns recovery state semantics.
  `event-model.md` owns event payloads. `handler-pause.md` preserves the
  distinct handler control path.

### `specs/event-model.md`

- **Change summary:** Define the observable recovery lifecycle when failed
  items are re-armed, or explicitly retain no recovery event if that event
  would add no durable fact.
- **Requirements:**
  - The event contract must match the queue recovery transition and preserve
    queue state as the authority.
  - Any recovery event must define its payload, ordering, durability class,
    and replay behavior.
  - Existing pause event meanings must stay distinct from recovery.
- **Dependencies:** The decision follows `queue-model.md` and
  `process-lifecycle.md`. An event addition requires the normal foundation
  amendment process.

### `specs/handler-pause.md`

- **Change summary:** Preserve the separation between a handler-type resume
  and a queue failed-item recovery command.
- **Requirements:**
  - A handler resume must not re-arm failed queue items or clear queue state.
  - Queue recovery must not clear a handler pause.
- **Dependencies:** This is a compatibility check on the queue recovery
  contract in `queue-model.md` and the operator surface in
  `process-lifecycle.md`.

### `specs/beads-integration.md`

- **Change summary:** Define the ledger preparation needed before the first
  controlled batch.
- **Requirements:**
  - Stale graph findings must be inspected against current source before close.
  - A confirmed remaining condition must be represented by a new scoped record.
  - The first mission must name only open, repeat-safe work that fits the
    first-canary limits.
- **Dependencies:** Registry and mission actions are read-first and need an
  explicit operator decision before any mutation.

### `specs/assessor-handoff-schema.md`

- **Change summary:** Verify that the existing handoff schema can carry the
  controlled-batch evidence. Change it only if a required readiness fact has
  no valid field.
- **Requirements:** The handoff must name the candidate branch, scope, report
  path, gate owner, and proof artifacts. It must state the narrow canary and
  the assessor's independent role.
- **Dependencies:** The assessor mission and report use this schema after the
  operator authorizes creation. A schema change depends on that field-gap
  review.

## Operational documents and records

### `docs/scratch-daemon-runbook.md`

- **Change summary:** Add the controlled-load and retained-artifact procedure
  for the readiness proof if Research finds the current runbook lacks it.
- **Requirements:** The procedure must capture daemon-suite load conditions,
  Step 9 evidence, core-loop result, batch artifact paths, and fixed canary
  limits: one local repeat-safe stream item at concurrency one, with no remote,
  Pi, cross-repository, or wave work.
- **Dependencies:** It follows the blocker fixes and precedes the assessor gate.

### Assessor mission and report

- **Change summary:** After operator authority, create
  `.harmonik/crew/missions/assessor-queue-dogfood-readiness.md` and require the
  report at `.harmonik/reports/queue-dogfood-readiness-gate.md`. Both use the
  existing assessor handoff schema.
- **Requirements:** The mission must identify the branch, scope, report path,
  gate owner, and proof artifacts. It must state that the assessor does not
  consume normal queue work. The report must evaluate only the stated canary.
- **Dependencies:** The operator authorizes the mission creation after a
  read-first inspection of existing registry and mission records.

### Durability proof tests and ratchets

- **Change summary:** Repair the daemon, queue, queue-wiring, and scenario
  proofs that claim a durable reservation or release transition.
- **Requirements:** Each proof must fail if its observed persistence write is
  removed. A ratchet must test the production path, not only a test fixture or
  a function name. The proof record must say which durable state it observes.
- **Dependencies:** The production durability contract comes from
  `queue-model.md`, `execution-model.md`, and `run-state-machine.md`. Alpha
  owns daemon tests. Bravo owns queue-side tests and ratchets.

### Step 9 and core-loop proof artifacts

- **Change summary:** Define the retained readiness evidence produced by
  `scripts/scratch-daemon.sh`, `scripts/core-loop-matrix.sh`, and the Step 9
  record in `DECOMPOSITION-MAP.md`.
- **Requirements:** The record must retain the scratch batch result at
  `<scratch>/.harmonik/batch-<name>-<queue_id>.json`, the core-loop result, the
  tested branch, the selected canary, and the controlled-load conditions. It
  must distinguish a machine-contention result from a product failure.
- **Dependencies:** This proof follows blocker fixes and precedes the assessor
  gate. The assessor mission cites the retained paths.

### Beads ledger and event artifacts

- **Change summary:** Define the read-first hygiene record for the source
  ledger and event inputs that seed the controlled batch.
- **Requirements:** Record the source paths, capture time, and open-item set
  before selecting the canary. Close stale graph work only with current-source
  evidence. Preserve unresolved findings as scoped work. Do not use a scratch
  event result as authority for the fleet ledger.
- **Dependencies:** This record precedes mission creation and batch selection.
  `beads-integration.md` owns bead-state semantics. Scratch artifacts are
  attached to the assessor report only after the controlled run.

### `LANES.md` and live handoffs

- **Change summary:** Record task ownership, the strict canary limits, and the
  release order.
- **Requirements:** Alpha owns all daemon code and tests. Bravo owns queue and
  queue-wiring work outside daemon, queue CLI behavior, ratchets, and triage.
  The shared release contract is agreed before either side changes its boundary.
- **Dependencies:** Update after task design and before implementation starts.

## New specs

No new normative spec is assumed in this pass. Research must first decide
whether the existing queue, execution, process-lifecycle, and beads contracts
can state every required behavior without a new document.

## Dependency map

1. Trace the current DOT shutdown, release, and queue recovery paths.
2. Align the shared shutdown contract in `execution-model.md`,
   `run-state-machine.md`, and `operator-nfr.md`.
3. Define queue recovery as one contract across `queue-model.md`,
   `process-lifecycle.md`, `event-model.md`, and `handler-pause.md`.
4. Alpha repairs the remaining daemon release paths and daemon-side durability
   tests. Bravo repairs queue and queue-wiring durability, adds an explicit
   failed-item recovery wiring task, and makes ratchets observe the durable
   boundary.
5. Run controlled daemon tests. Decide whether code changes or the one-suite
   operating rule solves test reliability.
6. Verify or replace stale graph findings without changing beads during this
   planning work.
7. Inspect registry and mission state. Obtain operator authority for any reset,
   retirement, or new mission.
8. Run Step 9 and core-loop proof in a scratch daemon using the fixed canary
   limits.
9. Create the assessor handoff and run the independent gate.
10. The operator makes the narrow controlled-batch go or no-go decision.

## Goal to area traceability

| Goal from problem space | Areas that satisfy it |
|---|---|
| DOT shutdown safety | `execution-model`, `run-state-machine`, `operator-nfr`, daemon task, scratch proof |
| Failed-item recovery | `queue-model`, `process-lifecycle`, `event-model`, `handler-pause`, explicit Bravo recovery wiring task |
| Durable reservation behavior | `queue-model`, Alpha daemon task, Bravo queue and queue-wiring task |
| Durability-proof gaps | durability tests and ratchets that observe the persistent boundary |
| Trustworthy daemon tests | `operator-nfr`, scratch-daemon runbook, controlled-load record, one-suite operating rule |
| Step 9 and core-loop evidence | scratch-daemon runbook, process lifecycle, assessor report |
| Read-first registry and mission hygiene | beads integration, assessor handoff schema, mission, operator decision |
| Separate assessor gate | process lifecycle, assessor handoff schema, mission and report |
| Narrow first canary | scratch-daemon runbook, mission, LANES, assessor report |
| Bead and event-input hygiene | beads integration, ledger and event-artifact record, operator decision |

---

## Folded in from lane bravo's parallel pass (2026-08-03)

Lane bravo ran the same pass on branch `work/queue-dogfood-readiness` before any
lane contract named that branch. The two passes reached the same shape. Bravo's
text is kept below because it names evidence, measurements and review records
this document does not. The plan of record stays T1..T12 plus T5a in
`07-tasks.md`. Where the two disagree on behaviour, the section above wins.

### Components — Queue dogfood readiness

#### Existing normative specs

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

#### Operational artifacts

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

#### Deliberate non-areas

`specs/beads-integration.md` is not changed in this work unless research finds
a Beads transition that the recovery contract must define. Stale-bead review,
mission selection, and closure evidence are operational procedure. They do not
alone justify a normative Beads change.

No new spec file is needed. Existing specs already own queue state, run state,
shutdown, worktree retention, lifecycle commands, observations, and assessor
handoff. The integration pass will publish one cross-spec recovery matrix.

#### Dependency map

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

#### First-canary profile

The first batch has exactly one initial queue item in one local stream group.
It permits no append. It runs at `max-concurrent=1` against the active local
repository. The item must be repeat-safe: a second run after a restart must not
change a user-facing system or another repository. It uses no remote or Pi
harness, no cross-repository work, and no wave group. The controlled activation
gate and mission, not the general queue model, enforce this temporary profile.

#### Goal to area traceability

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
