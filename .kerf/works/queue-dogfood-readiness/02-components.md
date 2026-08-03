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
  - The initial controlled batch must be a one-item local stream at concurrency
    one. It must reject remote, Pi, cross-repository, and wave work.
- **Dependencies:** The queue recovery wiring task needs the current queue
  state-machine reading. It does not depend on `queue-status-writer`, which
  intentionally does not wire failed-item resume.

### `specs/execution-model.md`

- **Change summary:** State the daemon behavior for a DOT run that stops after
  commit and before merge.
- **Requirements:**
  - Shutdown must either drain a committed run through its release path or
    retain a durable, reviewable recovery state.
  - The daemon must not silently redispatch work that already committed.
  - The proof must cover daemon stop during this window.
- **Dependencies:** Alpha owns the daemon implementation and tests. The queue
  recovery contract must agree on the release-state handoff.

### `specs/process-lifecycle.md`

- **Change summary:** Record the operational boundary between a controlled
  queue batch, shutdown recovery, and a separate assessor gate.
- **Requirements:**
  - A normal queue worker must not be described as an assessor.
  - The assessor must audit a scratch daemon as a separate role.
  - The controlled batch procedure must retain the required Step 9 and
    core-loop proof artifacts for the gate.
- **Dependencies:** The assessor handoff format remains owned by
  `specs/assessor-handoff-schema.md`. This work consumes that format and does
  not change it unless Research finds a missing field.

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

## Operational documents and records

### `docs/scratch-daemon-runbook.md`

- **Change summary:** Add the controlled-load and retained-artifact procedure
  for the readiness proof if Research finds the current runbook lacks it.
- **Requirements:** The procedure must capture daemon-suite load conditions,
  Step 9 evidence, core-loop result, and batch artifact paths.
- **Dependencies:** It follows the blocker fixes and precedes the assessor gate.

### Assessor mission and report

- **Change summary:** Create a mission that uses the existing assessor handoff
  schema and a report that evaluates the stated canary only.
- **Requirements:** The mission must identify the branch, scope, report path,
  gate owner, and proof artifacts. It must state that the assessor does not
  consume normal queue work.
- **Dependencies:** The operator authorizes the mission creation after a
  read-first inspection of existing registry and mission records.

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

1. Trace the current DOT shutdown and queue recovery paths.
2. Define the shared release and recovery contract.
3. Alpha repairs daemon shutdown drain, `evaluateGroupAdvanceWithOutcome`, and
   every daemon-side durability test.
4. Bravo repairs queue and queue-wiring durability outside daemon, adds an
   explicit failed-item recovery wiring task, and makes ratchets observe the
   durable boundary.
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
| DOT shutdown safety | `execution-model`, daemon task, scratch proof |
| Failed-item recovery | `queue-model`, explicit Bravo recovery wiring task |
| Durable reservation behavior | `queue-model`, Alpha daemon task, Bravo queue and queue-wiring task |
| Trustworthy proof | daemon tests, queue ratchets, scratch-daemon runbook |
| Step 9 and core-loop evidence | scratch-daemon runbook, process lifecycle, assessor report |
| Read-first registry and mission hygiene | beads integration, assessor mission, operator decision |
| Separate assessor gate | process lifecycle, assessor mission and report |
| Narrow first canary | queue model, mission, LANES, assessor report |
