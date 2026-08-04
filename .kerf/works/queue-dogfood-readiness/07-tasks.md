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

---

# Amendment A — 2026-08-03, oversight audit

An oversight pass audited both lanes, the tree, and this plan. It added the
cards below and changed six of the cards above. The cards above stay as
written unless this amendment names them. Every claim here was reproduced with
a live command.

**What the audit changed about the shape of the work.** The plan assumed the
tree was ready to receive implementation. It is not. The commit gate is red, so
every bead a dogfood run dispatches fails its gate. The two lanes' work sits on
two branches and no commit holds both. And the run posture the pass depends on
does not exist. None of that had a task. T0, T0a and T8a below are the entry
condition for T1 through T12.

**Read the program plan too.** This kerf work is not the whole plan.
`plans/2026-07-27-delete-and-rewrite/DECOMPOSITION-MAP.md` Step 12 already owns
the subsystem-switch completion work, and `NEXT_STEPS.md` §5.2 already owns the
load-sensitive test family. Any further amendment must name which program-plan
step it extends. An amendment that touches only this file cannot keep the plan.

**Two new conventions for every card.**

- **`Owner:`** — ownership lives only in `01-problem-space.md` and in
  `LANES.md`, and a crew loads neither with its mission. Name the lane on the
  card.
- **`Observed:`** — say whether the card fixes something that fails a test or a
  live command today, or hardens against a failure nobody has seen here. The
  two are mixed together today and a reader cannot tell them apart.

**Two packages are in no lane.** `internal/runexec` holds `drainReopen`, which
is the exact function T5's acceptance requires changing. `internal/workflow/dot`
holds the graph-identity rule that broke ten tests. `LANES.md` names neither.
Assign both before this work is handed out.

## New cards

### T0 — Restore the commit gate

- **What:** Make `go vet ./...` exit 0 and repair the test fixtures that the
  last two days of work invalidated. Three test packages no longer compile
  against the retyped `core.WorkflowID` (`internal/brcli`, `internal/handler`,
  `internal/scenario`). Ten inline graph fixtures in `internal/daemon` lack the
  graph-level `workflow_id` attribute the new identity rule requires. Three
  merge-family tests commit inside the worktree factory before the agent
  launches, so the pre-launch baseline already holds the commit and the
  no-advance guard correctly refuses the node.
- **Why it blocks everything:** the graph commit-gate node runs
  `go build ./... && go vet ./... && go test -run=^$ ./... && bash
  scripts/scenario-gate.sh`. Vet is the second link. Every dispatched bead fails
  its gate, loops back to implement, and burns the retry cap.
  `internal/scenario` is the scenario harness, so the whole scenario tier is
  absent rather than half-skipping.
- **Do not treat the merge-family failure as a product regression.** The
  no-advance guard began firing because of a fix — the graph node now refuses a
  baseline it could not read, where before an unreadable baseline left the guard
  permanently disabled and a node that did no work returned success. Move the
  commit out of the worktree factory and into the fake handler command, so the
  fixture models an agent that commits during its run. Keep one new assertion: a
  node whose worktree arrives already committed and whose agent does nothing
  MUST still fail.
- **Scope the acceptance to what pass one needs.** The short daemon suite shows
  about 34 top-level failures in about 308 seconds. Ten are the graph-identity
  fixtures. Three are the merge family. Six are cold-start-token failures at 30
  to 60 seconds each that reach a real remote endpoint — remote work is out of
  scope for pass one, and `NEXT_STEPS.md` §5.2 owns that family. Two
  subscribe-hub failures are goroutine leaks from a parallel cold-start test and
  go green when it does. The remaining thirteen need one triage run on a quiet
  box before anyone calls them product defects.
- **Deliverables:** the three test packages, the `internal/daemon` graph
  fixtures, the three merge-family fixtures.
- **Acceptance:** `go vet ./...` exits 0. `go test -short ./internal/daemon`
  shows no failure in the graph-identity fixtures, the merge-to-main family, or
  the work-loop close tests. The repo's own commit-gate command exits 0. A
  triage record names each remaining failure as fixture rot, load artifact, or
  product defect, measured on a quiet host.
- **Depends on:** none. **Blocks:** every other card.
- **Owner:** alpha. **Observed:** fails today; reproduced.
- **Size:** hours for the fixtures, plus one quiet-box triage run.

### T0a — Reconcile the branches onto one candidate

- **What:** Merge `work/queue-dogfood-readiness` into
  `phase1-session-restart-substrate`. That branch holds all the queue code and
  no handoff or lane contract names it. Resolve the conflicts, tidy the
  artifacts, and tag the result.
- **The merge is measured, not estimated.** A trial merge in a throwaway clone
  produced 34 conflicts. Every one is under
  `.kerf/works/queue-dogfood-readiness/`. Zero Go files and zero spec files
  conflict. After resolving the document conflicts the merged tree builds clean
  and vet fails on exactly the same three pre-existing packages and nothing
  else.
- **The merge also lands six normative spec files** — `event-model.md`,
  `execution-model.md`, `operator-nfr.md`, `process-lifecycle.md`,
  `queue-model.md`, `run-state-machine.md`. The requirement identifiers QM-058,
  QM-058a, QM-059, PL-032 and PL-033 exist nowhere on the candidate branch today
  and appear only after this merge.
- **Do not land `specs/scratch-daemon-runbook.md`.** It duplicates the existing
  `docs/scratch-daemon-runbook.md`, it is absent from `specs/_registry.yaml`,
  and its own text says the script wins when the two disagree. That is a
  statement that it is not normative.
- **Diff every branch before deleting it.** Five branches sit in the bravo
  namespace. One, `work/bravo-queue-dogfood-artifacts`, was committed on the day
  of the audit and appears in no document; its tip message duplicates a commit
  already on the candidate under a different hash. Record a verdict per branch
  in the merge commit. Recover `ledger-triage.md` before deleting `work/bravo`.
  One branch is checked out under `/private/tmp`, which the operating system
  reclaims.
- **Also in this card:** commit `.kerf/works/codex-continuity/`,
  `.kerf/works/queue-status-writer/` and `scripts/codex-nudge.sh`. A tracked
  plan document already cites that script by path and neither kerf work exists
  on any branch. Delete the orphan artifact tree that `kerf finalize` wrote
  outside `.kerf/works/`. Update `LANES.md` with the real branch name in the
  same commit.
- **Acceptance:** one commit carries both the drain work and the queue-recovery
  work. `go build ./...` and `go vet ./...` behave exactly as they did on the
  candidate before the merge — that is the acceptance test, not a conflict
  count. `kerf square` still passes. No artifact exists at two paths.
  `git status` is clean. Hand the assessor a tag, not a branch name.
- **Depends on:** none. **Owner:** alpha, who owns the merge under `LANES.md`.
- **Observed:** verified by trial merge. **Size:** half a day.

### T1a — Reservation and quarantine durability

- **What:** Make a queue's quarantine clear only on a committed transaction, and
  route reservation release and undo through the transaction owner. Read
  `TransactionResult.CleanupErr` in the reservation path, so a commit that
  quarantined the queue is not reported as a successful dispatch.
- **Why it is a new card:** T7 is a test task and its acceptance currently
  carries this production obligation. A test task must not own an
  implementation.
- **Spec sections:** `queue-model.md` QM-001 and QM-060.
- **Deliverables:** the four raw quarantine clears in
  `internal/queuewiring/store.go`, the reservation give-back sites in
  `internal/daemon/scheduler.go` and `internal/daemon/scheduler_reservation.go`,
  and a locked transaction variant that does not deadlock the dispatch loop.
- **Acceptance:** a fault-injected failed reservation release leaves memory and
  disk equal. A later ordinary write does not reopen a quarantined queue. The
  write-error report re-fires after requarantine. A committed result carrying a
  cleanup error is treated as a write failure. Deleting any named persist call
  fails a test.
- **Depends on:** T1. **Feeds:** T7. **Owner:** bravo.
- **Observed:** present in code on the candidate branch.

### T1c — Reconcile the failed-queue archive path

- **What:** Bring the three readers of the failed-queue archive layout onto one
  path constant, and state in writing how the new
  `QueueStore.RecoverFailed` transaction relates to the archive-and-rename path
  that already exists.
- **The defect:** `lifecycle.SweepQueueArchives` is called at boot from
  `internal/daemon/orphansweep.go` and bounds archive growth to five per
  category. It reads `<project>/.harmonik` and skips any name that does not start
  with `queue.json.`. The real archives are one directory deeper and under the
  queue's own name — `.harmonik/queues/<queue>.json.failed-<timestamp>`.
  `internal/daemon/statedisk.go` and `internal/daemon/draindetect.go` both glob
  the real path, so two readers agree and the sweep does not. On this machine 68
  archives match the real layout and zero match the sweep's. The sweep has never
  deleted one.
- **Why it matters beyond hygiene:** there is already an archive-and-rename
  failure path with a durable on-disk record and a drain detector that reads it.
  Nobody has said whether `RecoverFailed` replaces that path, layers on it, or
  is a second mechanism for the same state. Two mechanisms for one state is the
  thing the charter exists to remove.
- **Acceptance:** one path constant, three readers. The sweep's test asserts
  against a fixture written by the real archive writer. A written statement of
  where `RecoverFailed` sits relative to the archive path.
- **Depends on:** T1. **Owner:** bravo. **Observed:** verified on the live store.

### T2a — Name the recovery verb and register its socket op

- **What:** Decide between a new verb and a selector on the existing
  `queue resume`, then register the operation on the daemon socket. Sweep the
  sixteen test beads and the process-lifecycle draft to the chosen name.
- **The defect:** `internal/queue/cli/recover.go` sends the socket operation
  `queue-recover`. `internal/daemon/socketdispatch.go` registers seven queue
  operations and that is not one of them.
  `QueueStore.RecoverFailed` has no production caller on either branch. The verb
  dead-ends. Separately, `harmonik queue resume` sends `operator-resume`, which
  only lifts a queue paused by drain, so it reports success against a queue
  paused by failure and nothing dispatches.
- **Why it blocks the pass:** if the canary item fails, there is no way out and
  the pass ends there.
- **Deliverables:** the registration in `internal/daemon/socketdispatch.go`, the
  CLI verb, an updated method-registry test.
- **Acceptance:** the chosen verb reaches a live daemon and returns the typed
  codes. The drain-release meaning of `queue resume` is preserved or explicitly
  retired. No bead, spec or draft names the other verb.
- **Depends on:** T1, T2. **Owner:** cross-lane — the registration sits in
  alpha's package and the feature is bravo's. This needs the written
  announcement `LANES.md` requires. **Observed:** verified on both branches.

### T5b — Reconcile the drain reopen ladder and its trigger

- **What:** Choose between the shipped silent reopen and the draft's
  reopen-then-`run_failed` on all four release-failure paths, implement the
  choice in `internal/runexec/run.go` `drainReopen`, and rewrite whichever test
  pins the other behavior. In the same card: gate the drain on a real shutdown
  rather than on any context cancellation, and read the run's own classified
  result before releasing its commits.
- **Why:** T5's acceptance and the spec draft both require the reopen ladder on
  all four paths. The code does it on close failure only and a test pins the
  silence as intended. Nobody reconciled the code with the spec draft the same
  work produced. Separately the drain fires on any context cancellation, so
  reaping one hung run merges its work and advances the queue group as a
  success. `RunHandle.Aborted()` exists, is on the port, and has no production
  caller — the signal is already threaded to the call site. And the drain does
  not read the run's verdict, so a run that failed review can have its commits
  merged on the cancellation edge.
- **Spec sections:** `execution-model.md` EM-053a,
  `run-state-machine.md` RSM-021. **Note RSM-021 is not fixed by the merge.** It
  still requires the shutdown-drain path to have no pre-merge synchronize, it is
  byte-identical on both branches, and the shipped drain synchronizes a remote
  run branch before merging. The tree carries a normative MUST that the shipped
  release path breaks. Land the amendment or record the divergence.
- **Acceptance:** one behavior, one test, one spec sentence. A stale-run abort
  keeps the pre-existing reopen path instead of merging. A run that failed
  review is not merged by the drain. A drained merge carries the review
  trailers, or the drain refuses to merge. One test starts the drain under a
  live context and cancels it mid-flight.
- **Depends on:** T5. **Owner:** unassigned — `internal/runexec` appears nowhere
  in `LANES.md`. Assign it first. **Observed:** verified in code.
- **Priority:** below the line for pass one. Assign it; do not hold the
  candidate for it.

### T8a — Queue-only run posture

- **What:** Author and prove the configuration the readiness canary runs under,
  and record what the daemon still constructs with it applied.
- **This extends `DECOMPOSITION-MAP.md` Step 12**, which already scopes the
  remaining switch coverage at about 600 lines and low risk. Do not re-derive
  the residue list. Take Step 12's split decision.
- **Write it to a tracked file.** `.harmonik/config.yaml` is gitignored, so an
  artifact written there cannot be committed, cannot travel with the candidate
  tag, and cannot be handed to the assessor. The tracked home already exists and
  is already used: `scripts/scratch-config-overlay.yaml`, which
  `scratch-daemon.sh init` appends onto the generated config. Note the script
  currently skips the overlay when a `harnesses:` key is present; fix that skip
  or place the block elsewhere.
- **The harness already supplies most of the posture.** The assessor runs in a
  throwaway clone driven by `scripts/scratch-daemon.sh`, not in this checkout.
  That script defaults concurrency to one, repoints origin at a throwaway bare
  repository and rewrites `branching.yaml`, disables eager refill, and strips
  the Anthropic credentials. Do not put those in this card's acceptance. What
  remains is the `subsystems:` block itself.
- **Switching the socket listener off is not "queue only" — it removes the
  queue.** Every `harmonik queue` verb is a socket call and the queue handler
  adapter is constructed inside the socket gate. The posture is socket listener
  on and the other twelve off.
- **Four things have no switch at all** and need a stated mitigation: the
  supervisor watchdog (see T8c), the ops-monitor schedule, the eager refill
  (environment variable only, already handled by the harness), and the boot
  orphan sweep.
- **Acceptance:** a scratch daemon boots under it. The record names every
  subsystem still constructed and why. This is the charter's own test of "done"
  and nothing in the repo has it.
- **Depends on:** none. **Blocks:** T9, T12a. **Owner:** alpha.
- **Observed:** no `subsystems:` block exists today; verified. **Size:** about
  two hours plus one scratch boot.

### T8b — Close the stale graph findings with evidence

- **What:** Verify the four graph findings against current source, close the
  stale ones citing the fixing commit and the test that pins the new behavior,
  and file any residue as a new scoped bead. State the evidence standard in the
  card. Set the kerf work's `bead_filter` to the codename label, record the task
  ordering as bead dependency edges, and rewrite the one test bead that encodes
  the design the integration pass refuted.
- **Why:** the four findings the plan calls stale-closure candidates are fixed
  in code, including the only open priority-0 bead. The ledger is stale, not the
  code, and it misleads the operator's own view.
- **Acceptance:** no open priority-0 bead describes behavior that is fixed.
  Every closure names a commit and a test. `kerf show` no longer reports an
  empty bead filter.
- **Depends on:** T0, so a green test can be cited. **Owner:** alpha.
- **Observed:** verified. **Size:** about an hour.

### T8c — Give the supervisor watchdog a switch

- **What:** Add the supervisor watchdog to the switchable set, or make the
  assessor's drain procedure kill the supervisor first and record the
  two-process teardown.
- **Why it is a hard prerequisite, not residue:** `cmd/harmonik/main.go`
  constructs `supervise.NewSupervisorWatchdog` in a bare block. There is no
  config key and no condition, and it is not one of the thirteen names in
  `projectconfig.knownSubsystems`, so no `subsystems:` block can switch it off.
  Its revive command is `harmonik supervise restart --watch-restart` and its
  check interval defaults to 60 seconds. On a fresh scratch clone no supervisor
  has ever run, so the first tick after boot spawns one. Sixty seconds after the
  assessor kills the daemon to observe the clean-shutdown drain, a supervisor
  comes back. **The shutdown drain is the half of the candidate lane alpha
  built, and it cannot be observed while this stands.**
- **Also fix the false comment** in `scripts/scratch-daemon.sh`, which tells
  every future operator that a standalone start means no auto-revive. That is
  not true against the current daemon: the script does not start a supervisor,
  the daemon starts one for it.
- **Deliverables:** a fourteenth entry in `knownSubsystems` plus the gate at the
  seam in `cmd/harmonik/main.go`, or the documented two-process teardown; and
  the corrected script comment.
- **Acceptance:** the assessor can kill the daemon and observe the drain to a
  terminal result with no process respawning under it.
- **Depends on:** none. **Blocks:** the drain leg of T12. **Owner:** alpha.
- **Observed:** verified in source. **Size:** small; it belongs in
  `DECOMPOSITION-MAP.md` Step 12.

### T9a — Retained event capture

- **What:** Give the scratch batch a retained capture path instead of a
  temporary file it deletes on exit. Widen its event-type filter beyond the
  three run events to include bead closure, outcome emission and the recovery
  event. Document the existing offline fold option in the script's help text.
- **Deliverables:** `scripts/scratch-daemon.sh`.
- **Acceptance:** a batch run leaves a capture file the validator can read. The
  four test beads that need ordering evidence can be satisfied.
- **Depends on:** T3, T8a. **Owner:** alpha. **Observed:** verified.

### T12a — Assessor mission and gate definition

- **What:** Decide whether this is a merge gate under the existing schema or a
  new gate kind. Write the mission file and the report path. Name the candidate
  bead and write the operational test for "repeat-safe". Pick the host-load
  number the validator compares against. State how the assessor is launched
  while the fleet daemon is down. State which of the assessor's mandatory legs
  are waived and why.
- **Guidance on the canary bead:** a single-file, idempotent, behavior-free edit
  inside the scratch clone that nothing tests and a reviewer can approve on
  first read. It must not touch `internal/daemon`; that package's own test run
  measures about 308 seconds inside an 1800-second gate.
- **Guidance on the host:** load average at or below the CPU count, exactly one
  daemon running, free disk above 10 GB. State the number. The assessor must not
  have to guess.
- **Deliverables:** `.harmonik/crew/missions/assessor-queue-dogfood-readiness.md`,
  the report path, and a written waiver for any waived leg.
- **Acceptance:** the mission validates against the handoff schema. The
  assessor's first action is to read the validation record and refuse if it is
  missing or failing.
- **Depends on:** T8a, T8c, T9. **Owner:** operator decision, not an agent's.

## Changed cards

- **T1** — add to the acceptance the bead-ledger preflight and the typed error
  codes `-32030` through `-32036`. Neither exists in the tree today and both are
  already in T1's own acceptance text, so this is a completeness note, not new
  scope. Correct the one line in `06-integration.md` that states the recovery
  error block as `-32020..-32026`; that range collides with a reserved block and
  the drafts and T1 both use `-32030..-32036` correctly.
- **T5a** — change `Depends on: T5` to `Depends on: none`. The claim record
  type, its writer and its reader are independent of whether a drain consumes
  them; only the wiring needs T5. The false edge idles a whole lane. Add the
  missing cross-boundary review as a precondition — three separate review
  records ask for it and it does not exist.
- **T6** — split. **T6-local** depends on T5a and reconstructs from the run
  branch, the recomputed merge target and current bead state. Of the three
  fields the release claim carries, the merge target is recomputable at restart
  and the run branch survives worktree reclaim, so only the remote worker
  endpoint is genuinely unrecoverable. **T6-remote** is deferred with the rest of
  remote work. Move the remote halves of T5's and T6's acceptance into
  T6-remote; pass one excludes remote work by its own stated limits, so those
  criteria can never be met as written.
- **T7** — change `Depends on: T1, T3` to `Depends on: T1, T1a, T3`. Remove the
  production behavior from its acceptance now that T1a owns it. Add the three
  dispatch-path edges to the ratchet's durable-requirements table — the
  reservation write, the claim-failure revert, and the terminal group-advance
  write. The table covers eight edges today and none of them is on the path this
  pass exercises.
- **T10** — split. Each implementation task lands its own spec amendment in the
  same commit as its code. Keep a small T10 for the two operational records and
  the plan-document updates. Change its method from "copy the draft over the
  target" to "apply only the sections named in the changelog, then diff the
  result" — both live targets have moved since the drafts were cut, so a
  whole-file copy would revert newer measured content.
- **T11** — change `Depends on: T10` to depend on the implementation cards it
  exercises: T1, T1a, T2, T2a, T3, T4, T5, T5a, T5b, T6-local, plus T8a and T9.
  Scenario validation must not wait on document finalization.
- **T12** — add T12a and T8c as dependencies.
- **T1 through T4** — renumber the dangling `QM-052b` citations to the
  identifiers that actually landed: QM-058, QM-058a, QM-059, PL-032, PL-033 and
  EM-053a. **Sequence this after T0a.** Those identifiers do not exist on the
  candidate branch today and arrive only with the merge. An implementer doing
  the renumber first would renumber to identifiers that do not exist.
- **T1b** (if it is added) — mark it `Observed: hardening; not seen here`. The
  replace-intent mechanism is real and `ClassifyReplaceIntent` genuinely has no
  production caller, but across 113 live queue files that have survived months of
  crashes and restarts there is not one stale intent or candidate file. Keep it
  below the line for pass one.

## Revised dependency graph

```
T0  ──▶ everything
T0a ──▶ everything

T1 ─┬─ T2 ─ T2a
    ├─ T1a ─ T7
    ├─ T1c
    ├─ T3 ─ T7
    └─ T4

T5 ─ T5b
T5a (independent) ─ T6-local
T5a + remote work ─ T6-remote   [deferred]

T8  ─ T9 ─ T9a
T8a ─ T9
T8b (independent)
T8c (independent)

implementation set ─ T11 ─ T12
T8a + T8c + T9 ─ T12a ─ T12
T10 runs in parallel with T11
```

**If only four cards land: T0, T0a, T8a, T8c.** Those four stand between the
current tree and a candidate an assessor can judge.

## What needs the operator

Five calls an agent must not make. The first three block the candidate.

1. **Which task plan survives the merge.** Two incompatible plans live under
   this same work name: a seven-task plan on the branch that carries all the
   queue code, and T1 through T12 plus T5a here. Recommend this one wins, and
   the merge commit records which of the seven map to which T-number so the code
   is not re-scored against the wrong plan.
2. **Does the reviewer run in pass one.** The tracked `workflow.dot` routes
   every bead through two Claude Opus reviewer nodes plus an 1800-second
   whole-repo commit gate. The reviewer is not locked — a per-item workflow
   reference outranks it and a reviewer-free graph already ships. Recommend the
   full path once T0 lands, because the commit gate is part of the path under
   test. Fall back to the reviewer-free graph only with the result labeled
   "queue and merge path proved; commit gate and review path not exercised".
3. **What an assessor PASS authorizes.** The normative schema constrains the
   gate field to merge or deploy. This plan describes an advisory verdict that
   informs only the controlled daemon activation. Recommend `gate: merge` with
   one sentence in the mission stating what a PASS authorizes, rather than a
   schema version bump for one pass.
4. **Who owns `internal/runexec` and `internal/workflow/dot`.** Recommend
   alpha; both break alpha's tests. One line each in `LANES.md`.
5. **Whether to reconcile the branch history with the mainline.** The mainline
   is 681 commits behind. That gap is why the lint delta, the required check and
   every new worktree base are stale. Nobody has measured what closing it costs,
   and it is the one item here that is hard to reverse.
