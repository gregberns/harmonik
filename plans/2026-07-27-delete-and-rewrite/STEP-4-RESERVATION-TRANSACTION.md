# Step 4 — the reservation transaction: the measured map, and what it changes about the step

Measured 2026-07-31 against `1e22c0141`. Every number below was counted at that commit with a
code/comment/blank splitter, not inherited from the plan. The splitter was calibrated against a
figure given independently: it reports `beadRunOne` in `internal/daemon/workloop.go` as **1,705
lines, 815 code and 824 comment**, which matches the brief exactly.

Other agents are editing `internal/daemon/agentlaunch.go`, `workloop.go`'s tunnel block,
`workloop_runplan.go`, `internal/transport/tunnel/` and `DECOMPOSITION-MAP.md`. Nothing in this map
depends on those regions, so the staleness risk here is low. The regions this map does depend on —
`internal/daemon/scheduler.go`, `internal/daemon/scheduler_reservation.go`,
`internal/queuewiring/store.go`, `internal/queue/state.go` — were read at `1e22c0141`.

---

## 1. The forward half of Step 4 is built. The reverse half is not, and the step never named it

**Built, and it is real.** `internal/daemon/scheduler_reservation.go` exists: **356 lines, 212 code
and 111 comment.** `reserveQueueItem` sets `Item.Status = Dispatched` and `Item.RunID` in one
`QueueStore.Transact` write, with the RunID minted before the write. The empty-string placeholder is
gone. A write that does not reach disk returns `reservationWriteFailed`, and the call site abandons
the dispatch — no claim, no launch, item stays pending. The call site in `runWorkLoop` is **64 lines,
44 code**. So the production part of Step 4 that landed is **256 code lines**, against a step priced
at "~150 lines".

**The emission is built too, and the step's own warning is discharged.** The Step 4 entry says
"That emission, not the transaction, is the part of Step 4 that must actually be written." It has
been written. `reportQueueWriteError` emits `infrastructure_unavailable{failed_prerequisite:
queue_write_error}` and `daemon_degraded`, bounds itself to once per queue, and is driven end-to-end
by `TestReservationWriteFailure_NeverClaimsAndNeverLaunches`. That test stubs the disk-free reading,
so it is not one of the 32 fixtures `OPEN-DEFECTS.md` names as never reaching the loop body. **Delete
the stale-premise warning from the step. It has been acted on.**

**The tests that landed with it are larger than the code.**
`internal/daemon/scheduler_reservation_internal_test.go` is 423 lines, **329 code**.
`internal/daemon/reservationwritefail_e2e_test.go` is 156 lines, **123 code**. So 452 code lines of
test against 256 code lines of production.

**What is not built is the reverse half.** A reservation is an acquire. Every acquire in this system
has a matching release, and Step 4 costed only the acquire. Four paths give a reservation back, and
**none of them is a transaction**:

| Path | Symbol | Mechanism | Failure handling |
|---|---|---|---|
| Claim failed, retry | the claim-error block in `runWorkLoop` | `LockForMutation` → `LockedSetQueueByName` → `queue.Persist` | logged, then continues as if it worked |
| Item reached terminal | `evaluateGroupAdvanceWithOutcome` | same raw pattern | logged, then suppresses its events but keeps the memory write |
| Pre-claim `br show` gave up | `markQueueItemFailureReason` | writes memory only, **no persist at all** | relies on the caller's persist |
| Daemon restarted, session dead | `adoptLiveRunSession` | same raw pattern | logged |

These are **95 + 47 + 20 + 60 = 222 code lines** across four functions, all writing the same two
fields the reservation writes, none of them going through the owner that Step 4 built.

**So the honest split of Step 4 is:** the acquire is done and well tested. The release is entirely
unwritten, and it is roughly the same size as the acquire was. The step is not finished. It is half
finished, and the map records it as closed.

---

## 2. Four races. Two are open, one is closed, and one the plan implies does not exist

**Open — a raw write reopens a quarantined queue.** This is the headline. When a reservation write
fails, `Transact` quarantines the queue name, which is QM-001's "refuse further mutations". But
`LockedSetQueueByName`, `LockedQueueStore.SetQueue` and `SetQueueByName` each end with
`delete(s.quarantined, name)`. **There are 14 production call sites of `LockedSetQueueByName` alone.**
Any one of them silently reopens a queue the store had shut.

> Proven by an executable probe, not by reading. A throwaway test in `internal/queuewiring` failed a
> reservation write against a read-only directory, confirmed `QuarantineReason("alpha")` was non-nil,
> then ran one raw `LockForMutation` → `LockedSetQueueByName` → `Done`. The quarantine read back
> `<nil>`, and a second reservation write then committed durably. The probe was deleted after the
> run. Nothing in the tree pins this, in either direction.

The consequence is not a lost write. The dispatch is still abandoned each time, so D3 holds. The
consequence is that **the operator is told once and then never again**: `reportQueueWriteError`
bounds itself to once per queue and never clears its map, so after some other writer reopens the
queue, the daemon fails, requarantines, and reports nothing, every poll interval, forever.

**Open — the undo of a reservation is not durable, and nothing notices.** The claim-failure revert
sets the item back to Pending and the RunID to nil in memory, then persists, then logs the persist
error and carries on. If that persist fails, memory says Pending and disk says Dispatched with a
RunID for a run that was never claimed and never launched. The next boot reads disk. **See §5: a
mutation proved no test in the tree fails when that persist is deleted outright.**

**Closed, and the plan should stop implying otherwise — two queues cannot reserve the same bead.**
`crossQueueDuplicateGuard` runs as the `Transact` `Precondition`, which executes under the store
write lock, after the generation and snapshot-byte checks and before `Mutate`. That is the same lock
hold as the write it guards, which is exactly what constraint 4 of the Step 3 correction asked for.
It is well defended — see §5.

**Not a race — the attempts counter cannot retire an item silently.** One reading of this step
suggests that repeated crashes burn `Item.Attempts` until the item is invisible: `waveEligible` and
`streamEligible` both skip a Pending item with `Attempts >= MaxItemAttempts` (which is 3), so an item
that reached the bound while Pending would never be selected again and its group would never reach
all-terminal. **That state is not reachable.** Selection requires `Attempts < 3`, so a selected item
carries 0, 1 or 2. The reservation increments and then tests `>= 3`, and the arm that fires at 3
sets the item to Failed **in the same mutation**. So the item leaves the Pending state at the moment
it reaches the bound, and a Pending item cannot carry 3. The over-limit skip in
`internal/queue/state.go` is dead defence-in-depth. Say so, rather than treating it as a live wedge.

**Not measured.** I did not establish what happens when the daemon is killed in the window between
the reservation write committing and `ClaimBead` returning. The reservation is durable at that
point and the bead is not claimed, so the next boot sees a Dispatched item with a RunID and no run
record. `internal/lifecycle/startup_pl005_qm002.go` (QM-002a) is the pass that should repair it. I
read enough to know the pass exists and cross-checks the bead ledger. I did not trace all three
sub-cases (run alive, run dead, run never existed). **Trace this before writing the recovery
commit** — it decides whether the release commits need a boot-time counterpart.

---

## 3. Three ordering edges are hard. The rest of the step is free

Hard, and each breaks loudly or silently if inverted:

1. **The reservation must commit before `ClaimBead`.** This is D3 itself. Reserve-then-claim leaves
   a reservation that outlives a failed claim, which a boot pass can repair. Claim-then-reserve
   leaves a claimed bead with no durable record, which needs a bead reopen and a ledger round trip.
   The current order is right. Do not "simplify" it by moving the RunID mint back down beside the
   claim.
2. **The cross-queue guard must run inside the same lock hold as the write.** It does, as the
   `Transact` `Precondition`. A pre-lock predicate cannot see the other queue's stamp land.
3. **`Attempts++` must be inside the same mutation as the stamp.** It is. Outside it, an item either
   double-counts or loses the property that only a real stamp attempt consumes budget.

A fourth edge arrives with the work in §7 and does not exist yet: **the release must go through the
same owner as the acquire**, because the raw path is what clears the quarantine. That is not an
ordering constraint on today's code. It is the constraint that makes the migration worth doing.

Free — and this is what lets the step be split:

- Which of the four release paths migrates first. They share no state and no caller.
- Whether `markQueueItemFailureReason` keeps its own identity or folds into the group-advance write.
  It writes memory and no disk today, so folding it changes nothing observable.
- Where the quarantine fix lands relative to the release migration. The two are independent.
- The order of the three test-hole commits against each other.

---

## 4. Step 3b half belongs here. The other half never did

The plan folded "cross-queue deduplication and one of the attempts bounds" into Step 4 on the
argument that they are reservation problems in a gate costume. Checked against the code:

**Cross-queue dedup — the argument is right for the gate the plan meant, and wrong as a general
statement, because the name covers three different things.**

- **The dispatch-time guard (`hk-a11re`)** is a reservation concern. It needs the write lock, it
  needs to be atomic with the stamp, and a pure pre-claim predicate cannot express it. Correctly
  folded, and landed as `crossQueueDuplicateGuard`.
- **The submit-time guard (EM-065)** is a genuine admission gate and does not belong here. It lives
  in `internal/queue/validation.go`, is fed by `loadOtherQueues` in `internal/queue/rpc.go`, runs on
  an RPC handler goroutine rather than the dispatch loop, and rejects the submit with
  `ReasonBeadAlreadyDispatched`. Different package, different moment, different failure mode.
  It has a cross-**group** sub-case as well, which the dispatch-time guard has no equivalent of.
- **The boot-time occupancy check** in `internal/lifecycle/startup_pl005_qm002.go` is a third
  spelling of the same rule at a third moment.

So "fold cross-queue dedup into Step 4" is a true statement about one of three sites. Written without
that qualifier it invites a later agent to pull the submit-time gate into the dispatch loop, where it
cannot work. **Name the site, not the rule.**

**The attempts bound — one of the three fits, and the plan is right to say so, but the other two are
not the same shape and should stop being counted as siblings.**

- **(a) the stamp bound** is durable, lives on `Item.Attempts`, and is now inside the reservation
  mutation. Correctly folded, and landed.
- **(b) the pre-claim `br show` bound (`hk-pina9`)** counts *consecutive subprocess failures* in an
  in-memory map keyed by queue, group, item and bead, and a single success deletes the entry. It is
  a rate limiter on a subprocess, which is exactly the description the plan gives for cooldown —
  and the plan already rules that cooldown stays where it is.
- **(c) the br-ready bound** is the same shape as (b), in a second in-memory map, on a path that has
  no queue item to reserve at all.

(b) and (c) reset on daemon restart. (a) does not. They are not one bound with three sites. They are
one durable budget and two in-memory circuit breakers. **They belong with cooldown, which the plan
already parks.**

---

## 5. The tests: two are strong, three cannot fail, and one hole is unpinned in both directions

Mutation results below were run before the machine load made further runs unwise. Each mutation was
confirmed applied by `git diff --numstat` and by a successful build before the run.

**Well defended — these are the guard rails for the remaining work.**

- **The cross-queue guard.** Replacing the `Precondition` with a function that always returns nil
  turned three tests RED, including an end-to-end one:
  `TestReserveQueueItem_RefusesBeadInFlightFromAnotherQueue`,
  `TestAdmissionOrder_CrossQueueDedupPrecedesTheClaim`, and
  `TestAdmissionOrder_TerminalDedupLeavesAWakeTokenPending`.
- **The attempts bound.** Deleting `item.Attempts++` turned four tests RED, including two that pin
  the bound's *position* rather than its value:
  `TestReserveQueueItem_StampsStatusAndRunIDInOneWrite`,
  `TestReserveQueueItem_AttemptBoundFailsItemDurably`,
  `TestAdmissionOrder_LocalCapGuardReadsThePreIncrementCount`, and
  `TestAdmissionOrder_AttemptsBoundStaysFusedToTheStamp`.

**Hole one — the claim-failure revert has no durability test.** Deleting the `queue.Persist` call in
the claim-error block of `runWorkLoop` left the scoped suite GREEN
(`internal/daemon` and `internal/scenario`, matching `Admission|Reserv|L5saf|Sentinel|Claim|Crash|`
`Recovery|Strand|Requeue|SetQueue|Wiring`). A full `internal/daemon` run with the mutation applied
produced only failures from the flaky family described below. The admission tests do depend on the
revert — they need the item back at Pending to count claims across four ticks — but they read the
**in-memory** store, so they pin the memory write and are blind to the disk write.

**Hole two — the group-advance write has no test at all, and one test is named after it.** Deleting
the `queue.Persist` call in `evaluateGroupAdvanceWithOutcome` left the **entire `internal/daemon`
package GREEN** on a clean run. `internal/scenario/queue_setqueue_wiring_test.go` opens with "hk-xsutm:
`evaluateGroupAdvanceWithOutcome` now calls `queue.Persist` after…". All four of its tests were
confirmed to RUN, by name, under `-v`, and all four PASSED with that exact call deleted. This is
Step 6 §9's shape again: a test that cannot fail looks exactly like a test that passes, and here the
file header names the behaviour it does not defend.

**Hole three — the quarantine is unpinned in both directions.** No test asserts that a raw write
clears the quarantine, and no test asserts that it survives one. The behaviour is therefore free to
flip either way without a signal. The probe in §2 is the only evidence, and it is deleted.

**A finding about the oracle itself, which the remaining work must plan around.** The
`internal/daemon` package does not give a stable baseline. Three separate `-short` runs of the same
unmodified tree produced three different failure sets:

- run 1: `TestWorkLoop_ClaimSemaphore_BoundsClaimConcurrency`
- run 2: `TestSubsystemPartition_BootSubsystems_DisabledDaemonStillReachesWorkLoop`,
  `TestSurviveShutdown_ADaemonStoppingDuringACompletionWaitNeverTouchesAnOwnSession`,
  `TestSurviveShutdown_AFailedPiRunKeepsTheWorktreeItsCapturedOutputIsIn`,
  `TestSurviveShutdown_AFailedGraphModePiRunKeepsTheWorktreeItsCapturedOutputIsIn`
- a fourth, later run of the whole package: all green

A package that fails differently on each run cannot be used to decide whether a mutation was caught.
Every mutation above was therefore scoped to a named test set that was first shown green twice in a
row. **Do the same for the remaining work, and do not read a full-package pass as evidence.**

**Proposed experiments, NOT RUN — the machine was too loaded to trust a result.**

1. Delete `delete(lq.s.quarantined, name)` from `LockedSetQueueByName` in
   `internal/queuewiring/store.go`. Predicted GREEN everywhere, which would confirm that nothing
   depends on a raw write reopening a queue and that the fix is therefore safe to make.
2. Delete the `queue.Persist` call in the queue-revert block of `adoptLiveRunSession` in
   `internal/daemon/scheduler.go`. Predicted GREEN, on the same reasoning as hole one.
3. Delete the `item.RunID = nil` assignment in the claim-failure revert, keeping the status write.
   Predicted GREEN. If so, nothing pins that a reverted item stops naming a run that never ran.

---

## 6. Carry-forward facts that must survive verbatim

Confirmed present and correct at `1e22c0141`:

- **`Transact` runs the `Precondition` under the write lock, after the generation check and the
  snapshot byte-equality check, and before `Mutate`.** All three properties are load-bearing. The
  byte check is what lets a `Transact` detect a mutation made through the raw `LockForMutation` path,
  which does bump the generation but is not otherwise visible to it.
- **A `Precondition` veto rejects before any I/O, so it does not quarantine and it does not run
  `Mutate`.** One consequence is easy to miss: a cross-queue duplicate never reaches `item.Attempts++`,
  so that path spends no attempt budget. `failQueueItem` then makes a **second** durable write to fail
  the item. **The cross-queue path is two writes, not one.** Do not describe Step 4 as "one durable
  write" without saying "on the happy path".
- **`OutcomeRejected` is deliberately not quarantined** and `reserveQueueItem` maps it to
  `retry_later`, while `ErrQueueQuarantined` maps to `write_failed`. Reporting the second as the first
  is how a shut queue becomes a silent two-second spin. The comment saying so is correct and should
  not be tidied away.
- **The br-ready path has no reservation and correctly should not have one.** A br-ready bead has no
  queue item, so there is nothing to reserve. `runIDReserved` is the flag that tells the shared tail
  to mint its own RunID. This is not an asymmetry to "fix".
- **The reservation reserves on the SELECTED queue name, not the "main" slot.** The `NQ-B1` comment
  at the call site is load-bearing on a multi-queue daemon.

---

## 7. Shape of the work

In order where §3 requires it, and each one independently reviewable.

1. Close the three test holes in §5 first — the claim-failure revert's durability, the group-advance
   write's durability, and the quarantine's survival across a raw write. They are the guard rails for
   everything below, so pinning them afterwards would defend nothing. Pair every "nothing happened"
   claim with positive evidence that the machinery ran, and scope each run to a named test set proven
   green twice, because §5's oracle finding says a full-package pass means little.
2. Make the quarantine unclearable by a raw write: remove the three `delete(…quarantined…)` calls
   from `internal/queuewiring/store.go` and decide, once, who may clear it. This is a behaviour
   change and it needs the operator's answer to one question — see §8.
3. Give the claim-failure revert a `releaseReservation` beside `reserveQueueItem`, through the same
   `Transact` owner, returning a verdict the caller must read rather than an error it may log.
4. Move `adoptLiveRunSession`'s queue revert onto the same `releaseReservation`, and give it the
   identity check it does not have — it holds `rec.QueueID` and never compares it.
5. Move `evaluateGroupAdvanceWithOutcome`'s durable write onto `Transact`, and fold
   `markQueueItemFailureReason` into it. This is the largest single piece: 95 code lines that mutate
   in place under the lock and must become clone-then-mutate-then-commit.
6. Only after 3 to 5: decide whether a failed release is fatal the way a failed reservation is. It
   probably is not — refusing to release strands the item the release exists to free — but the answer
   must be written down rather than falling out of whichever `if` was easiest.

Not part of this step, and named so it is not smuggled in: making `QueueStore` the sole writer is
Step 11, and it is a bigger question than the reservation lifecycle. This step migrates the four
paths that give a reservation back. It leaves the other production `queue.Persist` sites alone.

**The price.** Measured: 18 production `queue.Persist` call sites in 9 files (one of the nine,
`internal/daemon/scenariotest/concurrent_merge.go`, is test support), 6 of them in `scheduler.go`.
14 production `LockForMutation()` call sites in 5 files, plus 2 `LockForMutationView()` calls in
`internal/queue/rpc.go` that take the same lock. The four release paths are 222 code lines today.

Rewriting them through the transaction owner is close to a wash on production line count — call it
**150 to 200 code lines net**, which at this repo's roughly one-to-one comment ratio is 300 to 400
raw lines. The tests are the larger half, as they were for the part that landed: the acquire took 452
code lines of test against 256 of production, and the three holes plus one end-to-end recovery test
will not be cheaper. Budget **250 to 350 code lines of test**.

So the honest replacement for "~150 lines" is: **256 code lines of production and 452 of test are
already spent, and 150 to 200 more code lines of production and 250 to 350 of test remain.** The step
as originally priced was about a third of the work, and the third it named is the third that was
easiest.

Risk stays MEDIUM, for a reason the original entry did not give: the failure mode of getting the
release half wrong is a queue item stranded at Dispatched forever, which looks like a slow daemon
rather than an error.

---

## 8. The one question for the operator

**When may a quarantined queue be reopened?** QM-001 says a queue that failed a write refuses further
mutations. Today any raw write reopens it by accident. Removing that accident is the fix, but it
leaves the queue shut until the daemon restarts, and `harmonik queue resume` will not reopen it —
see the defect list below. The choices are: a restart is the only cure (simplest, matches the
existing comment "recovery is an operator restart"), or an explicit operator verb clears it, or the
store retries the write once on a later tick and clears the quarantine if it succeeds. This is the
same class of question as D3 and it should be answered the same way, once, before the code is
written.

---

## 9. Corrections `DECOMPOSITION-MAP.md` needs

I did not edit that file. Every change it needs is listed here.

1. **§2 Seam B, "⚠ Today the reservation is torn, deliberately."** False at `1e22c0141`. The
   reservation is one `Transact` write that sets status and RunID together, and a failed write
   abandons the dispatch. The paragraph describing the `RunID = &""` placeholder and the two
   non-fatal persists describes code that no longer exists. §4b already marks the placeholder as
   scar tissue, and §2 was not updated to match.

2. **Step 4 heading, "(~150 lines, MEDIUM risk)".** Measured: 256 code lines of production landed,
   plus 452 code lines of test. The remaining release half is another 150 to 200 code lines of
   production and 250 to 350 of test. Risk stays MEDIUM. See §7 above for the full price.

3. **Step 4, "⚠ The premise of this step is stale … That emission, not the transaction, is the part
   of Step 4 that must actually be written."** Discharged. `reportQueueWriteError` emits both events
   QM-001 requires, bounds itself once per queue, and is pinned end-to-end. Delete the warning or
   mark it done — a reader today acts on a warning that was satisfied the same day it was written.

4. **Step 4, "all ten `.Transact(` call sites are in `queuewiring/store_transaction_test.go`.
   Production callers: zero."** Now **19 call sites, of which 2 are production**, both in
   `internal/daemon/scheduler_reservation.go` (`reserveQueueItem` and `failQueueItem`).

5. **Step 4, "The dispatch path uses bare `queue.Persist` — ten calls in
   `internal/daemon/scheduler.go`."** Now **6** in that file, and **18** across 9 production files.

6. **Step 4, "Nothing is open. Re-checked 2026-07-30."** False. The four paths that give a
   reservation back are all still raw writes with logged-and-ignored errors, and a raw write clears
   the quarantine the reservation depends on. The step is half done.

7. **Step 4, "Step 3b's cross-queue-dedup fold landed here."** True of the dispatch-time guard only.
   Add that EM-065's submit-time guard in `internal/queue/validation.go` is a separate gate on a
   separate goroutine and was never in scope, and that a third spelling exists at boot in
   `internal/lifecycle/startup_pl005_qm002.go`.

8. **Step 3b bullet, "cross-queue-dedup and attempts-bound(a) are a reservation-transaction problem
   wearing a gate costume."** Right, and it should say which of the three cross-queue sites and which
   of the three attempts bounds it means. The other two attempts bounds are in-memory subprocess rate
   limiters that reset on restart, which is the description the plan already gives cooldown — so they
   should be parked with cooldown, not counted as siblings of the durable one.

9. **Step 11, "`queuewiring.QueueStore.LockForMutation` has 18 production call sites in 6 files."**
   Measured **14 production call sites in 5 files across 2 packages**, plus 2 `LockForMutationView()`
   calls in `internal/queue/rpc.go` that acquire the same lock — 16 acquisitions. The figure 18 is the
   `queue.Persist` count, which is 18 sites in 9 files. The two numbers appear to have been swapped.

10. **Step 3, constraint list.** Constraint 4 says a pure pre-claim predicate cannot hold the write
    lock and that without it "two implementers run one bead again". That is now satisfied by the
    `Transact` `Precondition`, and the mechanism is worth recording, because it is the one shape that
    lets an admission-style check keep a lock guarantee: the check runs inside the store's own write
    lock, in the same hold as the write it guards.

---

## 10. Defects found while mapping

Filed nowhere yet. This worktree has its own `beads.db`, which is discarded with the worktree, so
these are recorded here by title and paragraph for the coordinator to file.

**A raw queue write clears the quarantine that a failed reservation set.**
`LockedSetQueueByName`, `LockedQueueStore.SetQueue` and `SetQueueByName` in
`internal/queuewiring/store.go` each end with `delete(s.quarantined, name)`. There are 14 production
call sites of `LockedSetQueueByName` alone. QM-001 requires the daemon to refuse further mutations to
a queue after an I/O error in the atomic-write sequence, and the refusal is meant to be sticky. Proven
at runtime by a throwaway probe: after a failed reservation write quarantined a queue, one raw
`LockForMutation` write cleared it, and a second reservation write then committed. The dispatch is
still abandoned each time, so no work is launched on a lost write, but the operator is told once and
then never again, because `reportQueueWriteError` reports once per queue and never clears its map.

**The undo of a reservation is not a transaction, and its durability has no test.** Four production
paths give a reservation back — the claim-failure revert in `runWorkLoop`,
`evaluateGroupAdvanceWithOutcome`, `markQueueItemFailureReason`, and `adoptLiveRunSession`. All four
use the raw `LockForMutation` plus `queue.Persist` pattern, and all four log the persist error and
carry on. If the persist fails, memory says Pending and disk says Dispatched with a RunID for a run
that never started. Deleting the `queue.Persist` call in the claim-failure revert left the scoped
suite green.

**A test named for the group-advance persist passes with that persist deleted.**
`internal/scenario/queue_setqueue_wiring_test.go` opens by claiming it covers "hk-xsutm:
`evaluateGroupAdvanceWithOutcome` now calls `queue.Persist` after…". With that exact call deleted,
all four of its tests were confirmed to run by name under `-v` and all four passed, and the whole
`internal/daemon` package stayed green. This is a test that cannot fail, and its header advertises
the behaviour it does not defend.

**The claim-failure revert identifies its item by position only.** The revert block in `runWorkLoop`
locates the item by group index and item index and writes Pending plus a nil RunID without checking
`Item.BeadID` or the queue id, unlike `activeQueueItem`, which checks the bead. A wrong-position
write would silently reopen a different item for dispatch. Not reachable today, because the only path
that sets `queueItemIndex >= 0` also sets the group index in the same block — but the guard the code
does have (`queueGroupIdxFd != nil`) protects against the impossible case and not against the one
that matters.

**`adoptLiveRunSession` indexes groups positionally and never uses the queue id it holds.** It reads
`q.Groups[rec.GroupIndex]` as a slice position, while `activeQueueItem` and
`evaluateGroupAdvanceWithOutcome` both scan for a group whose `GroupIndex` field matches. It also
tests `rec.QueueID != ""` in its guard and then never compares that value to `q.QueueID`. The bead-id
and Dispatched-status checks it does make catch most of the resulting mismatches, which is why this
has not bitten.

**`harmonik queue resume` does not re-arm failed items, and at least three comments say it does.**
`ResumeFromFailure` and `RearmFailedItems` in `internal/queue/resume.go` have **no production
caller**. The `operator-resume` verb reaches `OperatorPauseController.HandleOperatorResume`, which
emits `operator_resuming`. The consumer, `transitionToActive` in
`internal/queuewiring/operatorevents.go`, only moves queues out of `paused-by-drain` and skips every
other status. So a queue in `paused-by-failure` has no product path back, and the hk-pina9 comment in
`internal/daemon/scheduler.go` that reassures the reader "`queue resume` resets failed items to
pending with Attempts=0, so this is recoverable" is false. This is the same complaint RA-2 in
`NEXT_STEPS.md` makes from the other end.

**The `internal/daemon` test package is not a stable oracle.** Three `-short` runs of the same
unmodified tree at `1e22c0141` produced three different results: one failure, four failures, and all
green. The failing names came from the survive-shutdown family, the claim-semaphore concurrency test,
the subsystem-partition boot test and the orphan-marker reconcile test. A package that fails
differently on each run cannot tell anyone whether a change broke something, and it silently
converts mutation testing into guesswork.

---

## 11. What I could not verify

- **The crash window between the reservation commit and a successful `ClaimBead`.** I did not trace
  QM-002a's three sub-cases in `internal/lifecycle/startup_pl005_qm002.go`. See §2.
- **Whether `failQueueItem`'s second write can spin without bound.** On the cross-queue-duplicate
  path the `Precondition` veto happens before `Mutate`, so no attempt is charged. If `failQueueItem`
  then returns `retry_later` repeatedly, the item is re-picked every tick with an unchanged attempt
  count. I did not establish whether that is reachable.
- **The three proposed mutations in §5.** Written down with the exact edit and the predicted result.
  None was run. The machine was too loaded for the result to mean anything.
