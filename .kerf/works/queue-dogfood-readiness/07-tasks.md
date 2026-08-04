# Implementation Tasks

## Task List

### T1 — Queue recovery transaction

- **What:** Implement the named QueueStore recovery transaction for one
  `paused-by-failure` queue. It must preflight all failed Beads as open, reset
  only the defined retry fields, retain audit fields, and return typed recovery
  failures without mutation.
- **Spec sections:** `queue-model.md` §2.6, §2.10, §8.3b QM-052b;
  `beads-integration.md` §4.5a BI-013f.
- **Deliverables:** `internal/queue/types.go`, `resume.go`,
  `transaction_store.go`, `errors.go`, RPC records, and focused unit and fault
  tests.
- **Acceptance:** Tests prove all-open preflight, no-write rejection, exact
  field transitions, stale/write failure rollback, typed codes `-32030..-32036`,
  and post-commit-only success.
- **Depends on:** none.

### T2 — Queue-resume transport and CLI

- **What:** Wire `queue-recover` through the live daemon socket and `hk queue
  resume`. Keep it separate from operator resume, drain release, and handler
  resume.
- **Spec sections:** `process-lifecycle.md` §4.1 PL-003a, §4.10 PL-028 and
  PL-028c; `queue-model.md` §2.10 and §8.3b QM-052b.
- **Deliverables:** `internal/queuewiring/**`, `internal/queue/cli/**`,
  `cmd/harmonik/**`, and socket and CLI tests.
- **Acceptance:** Tests cover selector precedence, response fields, typed
  recovery errors, daemon-down exit 17 with zero local writes, and no route to
  operator or handler resume.
- **Depends on:** T1.

### T3 — Recovery event registration and ordering

- **What:** Add `queue_recovered` as a class-O cross-bus event and emit it once
  after QueueStore install and before dispatch wake.
- **Spec sections:** `event-model.md` §8.10 `queue_recovered`, EV-027, EV-050;
  `queue-model.md` §8.3b QM-052b.
- **Deliverables:** `internal/core/eventtype.go`, event registry and payload
  files, `eventtype_coverage_gjyks_test.go`, queue emission code, and tests.
- **Acceptance:** Payload validation and N-1 compat pass. The ordinary cohort
  includes the event. `TestCrossBusEventTypeCohortCount` exists with an updated
  named `wantCount`. Tests prove install precedes event and event precedes wake.
- **Depends on:** T1.

### T4 — Handler-pause separation

- **What:** Preserve handler-pause state through queue recovery and prove that
  each resume surface affects only its own state.
- **Spec sections:** `handler-pause.md` §6 HP-025 and §7 HP-009a;
  `queue-model.md` §8.3a QM-052a and §8.3b QM-052b.
- **Deliverables:** handler-pause integration seam, queue admission checks, and
  focused tests in the owning queue and handler packages.
- **Acceptance:** Recovery never emits `handler_resumed` or alters handler
  pause. Handler resume never recovers a queue. A paused handler still blocks a
  recovered item at dispatch or submit as the base contract requires.
- **Depends on:** T1.

### T5 — Committed-DOT graceful drain

- **What:** Make normal SIGINT, SIGTERM, and graceful stop finish committed DOT
  release through synchronize then close-or-reopen. Do not emit a normal
  outcome on this drain edge.
- **Spec sections:** `execution-model.md` §4.12 EM-053a;
  `run-state-machine.md` §7 RSM-021; `operator-nfr.md` §4.7 ON-027b;
  `process-lifecycle.md` §4.4 PL-011.
- **Deliverables:** `internal/daemon/**`, `internal/runloop/**`, and focused
  daemon/runloop regression tests.
- **Acceptance:** A committed local and remote DOT fixture proves sync before
  merge, close then `bead_closed` then `run_completed`, and reopen then
  `run_failed` on every release failure. It proves no `outcome_emitted` and no
  normal-timeout bypass before the terminal result.
- **Depends on:** none.

### T5a — Immutable release-claim checkpoint

- **What:** Before normal release or shutdown drain, persist a typed immutable
  release claim in the final pre-release Git checkpoint. The claim records the
  dispatch-head SHA, resolved merge-target ref and SHA, and the optional remote
  worker name, host, and repository path.
- **Spec sections:** `execution-model.md` §4.7 EM-031b and §6.1
  `Transition`, `ReleaseClaim`, and `RemoteEndpoint`.
- **Deliverables:** typed core release-claim records, checkpoint writer and
  reader wiring, and focused local and remote checkpoint tests.
- **Acceptance:** Tests prove the claim is durable before any synchronize,
  merge, close, or reopen call; local claims omit the endpoint; remote claims
  retain every endpoint field; and a later transition cannot rewrite the claim.
- **Depends on:** T5.

### T6 — Restart reconstruction of unfinished release

- **What:** Reconstruct committed but unsettled DOT release after restart from
  the immutable Git release claim and current Bead state, then reach one drain
  close-or-reopen result. Do not use JSONL or a daemon-local registry as a
  release-state source.
- **Spec sections:** `execution-model.md` §4.7 EM-031b and §4.12 EM-053a;
  `run-state-machine.md` §7 RSM-021; `operator-nfr.md` §4.8 ON-030.
- **Deliverables:** daemon restart/reconciliation path and restart fault tests.
- **Acceptance:** Tests cover local and remote unfinished release, missing tip,
  sync failure, merge failure, and close failure. They prove recovery uses the
  recorded target and remote endpoint after restart with JSONL and any
  daemon-local registry absent. A missing, corrupt, or inconsistent claim
  retains the branch and reconciles without merge, close, reopen, or
  redispatch.
- **Depends on:** T5a.

### T7 — Durability proof and ratchet repair

- **What:** Repair the queue durability proofs and add a release-contract test
  table that covers recovery, reservations, quarantine, and post-commit event
  behavior.
- **Spec sections:** `durability-proof-tests.md`; `queue-model.md` QM-001,
  QM-052b, QM-060, and QM-063; `beads-ledger-events.md`.
- **Deliverables:** queue transaction and fault tests, any ratchet scripts or
  fixtures, and `docs/queue-readiness-durability-proofs.md`.
- **Acceptance:** Fault injection proves no partial recovery, sticky quarantine
  on replacement failure, durable reservation release, and no observation-led
  recovery. The test record names retained proof output.
- **Depends on:** T1, T3.

### T8 — Read-only readiness evidence record

- **What:** Implement the retained readiness snapshot and event-evidence record
  without changing fleet Beads state during selection or review.
- **Spec sections:** `beads-integration.md` §4.5 BI-013e;
  `beads-ledger-events.md`; `lanes-handoffs.md` queue-dogfood directive.
- **Deliverables:** read-only evidence command or helper, evidence schema, and
  `docs/queue-readiness-ledger-events.md`.
- **Acceptance:** The record captures the candidate, exclusions, current source
  checks, event path, and terminal-intent inspection. Tests prove it creates no
  Beads status mutation and flags stale findings separately from current ones.
- **Depends on:** none.

### T9 — Local readiness and validator targets

- **What:** Add the local scratch readiness target and its validator. The
  validator must reject unsafe evidence and make no fleet-daemon or Beads call.
- **Spec sections:** `scratch-daemon-runbook.md` Queue-readiness procedure;
  `step9-core-loop-artifacts.md` Queue dogfood readiness evidence;
  `operator-nfr.md` ON-032a.
- **Deliverables:** `Makefile`, `scripts/scratch-daemon.sh` or a dedicated
  helper, target tests, retained evidence layout, and updated scratch runbook.
- **Acceptance:** `make queue-dogfood-readiness` requires scratch, evidence,
  bead, and reason inputs. It rejects Pi, remote, cross-repository, wave,
  feedback, and non-one concurrency. The validator rejects missing or unsafe
  evidence and writes `validation.json` without fleet access.
- **Depends on:** T8.

### T10 — Final document integration

- **What:** Finalize the approved full-spec drafts and operational records into
  their declared targets. Update lane status and the Step 9 map only after the
  implementation evidence exists.
- **Spec sections:** Every target in `05-changelog.md`, especially
  `lanes-handoffs.md` and `step9-core-loop-artifacts.md`.
- **Deliverables:** updated `specs/`, `docs/`, and plan targets named in the
  changelog.
- **Acceptance:** The final targets match the approved drafts, source-of-truth
  rules are preserved, and new operational records exist at their declared
  `docs/` paths.
- **Depends on:** T1–T9 and T5a.

### T11 — Scenario validation

- **What:** Run the required scenario tests after their implementation tasks
  land. These are explicit existing test beads: `hk-jthdp`, `hk-ddug1`,
  `hk-0ok7x`, `hk-q5l0b`, `hk-w0gt8`, `hk-qhjtb`, `hk-nvewa`, and `hk-bwpms`.
- **Spec sections:** `queue-model.md` QM-052b; `execution-model.md` EM-053a;
  `run-state-machine.md` RSM-021; `operator-nfr.md` ON-027b;
  `process-lifecycle.md` PL-003a; `event-model.md` §8.10.
- **Deliverables:** scenario fixtures, retained reports, and completed test-bead
  evidence.
- **Acceptance:** The scenarios exercise durable recovery, committed-DOT drain,
  RPC recovery, event ordering, handler separation, and Beads snapshot behavior
  on a controlled local fixture.
- **Depends on:** T10.

### T12 — Exploratory operator validation

- **What:** Run the required exploratory tests after the scenario pass. These
  are explicit existing test beads: `hk-y3hvr`, `hk-5qhlb`, `hk-xouwd`,
  `hk-60996`, `hk-awbvt`, `hk-g3t4c`, `hk-a5mvu`, and `hk-11vgl`.
- **Spec sections:** `queue-model.md` QM-052b; `process-lifecycle.md` PL-028;
  `event-model.md` EV-027; `scratch-daemon-runbook.md` Queue-readiness
  procedure.
- **Deliverables:** exploratory reports, retained CLI output, and completed
  test-bead evidence.
- **Acceptance:** The pass proves safe rejection states, daemon-down behavior,
  release reconstruction, event payload validation, handler separation, and
  scoped current-finding handling. It must classify host contention separately
  from product failure.
- **Depends on:** T11.

## Dependency Graph

```
T1 ─┬─ T2
    ├─ T3 ─ T7
    └─ T4
T5 ─ T5a ─ T6
T8 ─ T9
T1,T2,T3,T4,T5,T5a,T6,T7,T8,T9 ─ T10 ─ T11 ─ T12
```

## Parallelization Plan

T1, T5, and T8 can begin in parallel because they own separate contracts and
packages. T3 starts after T1's queue transaction shape is fixed. T2 and T4
then run in parallel after T1. T5a writes the release claim after T5. T6 uses
that claim after T5a. T7 follows T1 and T3. T9 follows the read-only evidence
record from T8. T10 serializes final target publication after all
implementation evidence. T11 then validates the integrated result; T12 follows
T11 so exploratory findings use the same controlled fixture.
