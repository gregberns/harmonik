# Live Bead State Model

```yaml
---
title: Live Bead State Model
spec-id: live-bead-state
requirement-prefix: LB
status: draft
spec-category: foundation-cross-cutting
spec-shape: requirements-first
version: 0.1.0
spec-template-version: 1.1
owner: foundation-author
last-updated: 2026-08-11
depends-on:
  - queue-model
  - execution-model
  - beads-integration
  - workspace-model
  - process-lifecycle
---
```

This model joins the durable facts for one queued bead run. It does not add a
new source of truth. It defines the combinations that C20 and C21 must support.
The existing subsystem specs still own each record format and write rule.

## 1. Authority order

### LB-001 — Each lifecycle question has one authority

Use the narrowest durable fact that owns the question.

| Question | Authority | Facts that are not authority |
| --- | --- | --- |
| May this queue item be offered? | Canonical queue state | Run registry, events, session process |
| Did one dispatch reserve the item? | Canonical queue item plus its bound run ID | Bead status alone |
| Did Beads accept the claim? | Beads status | Queue state or an adapter record alone |
| Which dispatch owns the claim? | The dispatch intent binding | An `in_progress` status with no binding |
| Is a run live in this process? | Run registry entry | A durable run record or a live process alone |
| Can a run be adopted after restart? | Durable run record plus exact live-session identity | Run registry memory |
| Who owns the worktree? | Worktree lease bound to run ID | Directory presence alone |
| Is work ready to land? | Immutable release claim in git | Dispatch intent, worktree state, events, or run memory |
| Did work land? | Git completion evidence for the release claim | Dispatch intent, event, or source-branch commit alone |
| Is the bead complete? | Beads `closed` plus matching git completion evidence | Either fact by itself |
| Did the queue consume the terminal result? | Canonical queue transaction state | Bead status or event presence |

When two authorities disagree, recovery keeps evidence and returns a typed
`repair-required` result. It does not select a winner from timing or log text.

## 2. State vocabulary

### LB-002 — One dispatch uses one identity set

The model uses these facts.

| Fact | Values used by this model |
| --- | --- |
| Queue item | `pending`, `deferred-for-ledger-dep`, `reserved(run_id)`, `terminal(outcome)` |
| Bead | `open`, `in_progress`, `closed`, or `other(status)` |
| Dispatch intent | `absent`, `prepared`, `claim_refused`, `claim_durable`, `run_durable`, `handoff_durable` |
| Run registry | `absent` or `live(run_id)` |
| Durable run record | `absent`, `present(run_id, execution_location)`, or `present(run_id, execution_location, session_id)` |
| Worktree | `absent`, `leased(run_id)`, or `retained_for_repair(run_id)` |
| Session | `absent`, `live(session_id)`, or `dead(session_id)` |
| Release claim | `absent` or `present(run_id, dispatch_head, target)` |
| Merge | `not_landed` or `landed(run_id, target_tip)` |
| Queue terminal application | `pending` or `durable` |

Every identifier in one row must match. A row with two different run IDs or
two different session IDs is invalid. Recovery returns `repair-required` and
does not delete either side.

## 3. Valid steady states

### LB-003 — The live bead lifecycle has named steady states

These are the only steady combinations. A write can expose an adjacent crash
cut from section 4. It must not create another steady state.

| State | Queue item | Bead | Dispatch intent | Registry | Run record | Worktree | Session | Release claim | Merge | Queue terminal | Owner of next transition |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| S0 offerable | pending | open | absent | absent | absent | absent | absent | absent | not_landed | pending | scheduler |
| S0d dispatch deferred | deferred-for-ledger-dep | open, in_progress, or other(status) | absent | absent | absent | absent | absent | absent | not_landed | pending | scheduler status and dependency check |
| S1 reserved | reserved | open | prepared | absent | absent | absent | absent | absent | not_landed | pending | dispatch transaction |
| S2 claimed | reserved | in_progress | claim_durable | absent | absent | absent | absent | absent | not_landed | pending | dispatch transaction |
| S3 recoverable run | reserved | in_progress | run_durable | absent | present | absent or leased | absent | absent | not_landed | pending | startup replay |
| S4 handed off | reserved | in_progress | handoff_durable | live | present | leased | live | absent | not_landed | pending | run supervisor |
| S5 stopped with evidence | reserved | in_progress | handoff_durable | absent | present | retained_for_repair | dead or absent | absent or present | not_landed | pending | startup replay or reconciliation |
| S6 ready to release | reserved | in_progress | handoff_durable | live or absent | present | leased or retained_for_repair | dead or absent | present | not_landed | pending | release transaction |
| S7 landed | reserved | in_progress | handoff_durable | live or absent | present | leased or retained_for_repair | dead or absent | present | landed | pending | bead terminal adapter |
| S8 bead closed | reserved | closed | handoff_durable | live or absent | present or absent | leased, retained_for_repair, or absent | dead or absent | present | landed | pending | queue terminal transaction |
| S8f bead reopened | reserved | open | handoff_durable | live or absent | present or absent | leased, retained_for_repair, or absent | dead or absent | present or absent | not_landed | pending | queue terminal transaction |
| S9 consumed | terminal | closed | absent | absent | absent | absent or retained_for_repair | absent | present | landed | durable | queue group policy |
| S9f retryable failure consumed | terminal(failed) | open | absent | absent | absent | absent or retained_for_repair | absent | present or absent | not_landed | durable | queue group policy |
| S9x unreopened failure consumed | terminal(failed) | in_progress | absent | absent | absent | retained_for_repair or absent | absent | present or absent | not_landed | durable | reconciliation |

`S3` permits an already-created leased worktree because worktree creation can
finish before session handoff. The durable run record must exist before this
state can be treated as recoverable.

`S5` is not a success state. It is a stable recovery input. The worktree stays
when it can contain evidence that no durable fact has yet classified.

`S8` permits cleanup lag. Cleanup cannot change the completion decision.

`S8f` and `S9f` are the retry path. A failed run reopens the bead before the
queue transaction consumes the failed item outcome.

`S9x` keeps the bead `in_progress`. Recovery cannot infer its failure class.
It retains evidence and returns `repair-required`. There is no replayable
pre-transaction S8x state because no durable fact carries the failure class.

## 4. Process-death cuts

### LB-004 — Each process-death cut has one recovery result

Each cut has one recovery result. C21 must implement these results before the
scheduler offers new work.

| Cut | Durable facts after restart | Required recovery result | Forbidden result |
| --- | --- | --- | --- |
| D0 before reservation | S0 facts | `offerable` | Mint a run or claim the bead |
| D1 after intent prepare, before queue reservation | Prepared intent, offerable item, open bead | `replay-reservation` with the same IDs | Mint replacement IDs |
| D2 after queue reservation, before claim | Reserved item, prepared intent, open bead | `replay-claim` | Release from elapsed time alone |
| D3 claim refused | Reserved item, prepared intent, typed claim result | Make the exact `claim_refused` phase durable | Compensate before refusal durability |
| D3a after refusal phase | Reserved item, claim_refused intent | Apply the exact typed fail or release action | Re-run ClaimBead |
| D3b after dependency compensation | Exact failed item with matching preclaim binding, claim_refused intent | Finalize the exact failed-item group decision | Remove the intent before group durability |
| D3c after refusal finalization | Durable exact group result, or exact release to pending | Remove the exact intent | Repeat compensation |
| D3r after reservation terminal write | Prepared intent and matching max-attempt or cross-queue preclaim binding | Finalize the exact failed-item group decision, then remove the intent | Infer completion from item status alone |
| D4 after claim, before claim phase is durable in intent | Reserved item, prepared intent, current bead status | Use table 4.2 | Reset or dispatch again before classification |
| D5 after durable claim, before run record | Reserved item, claim_durable intent, current bead status | Use table 4.3 | Leave a permanent bare claim |
| D6 after run record, before intent phase update | Exact run record, claim_durable intent | `advance-run-phase` | Write a second run record |
| D6a after run phase update, before worktree | S3 with no worktree | `resume-provision` | Create another run ID |
| D7 after worktree, before session handoff | S3 with leased worktree | `resume-handoff` with the same worktree | Delete the worktree before classification |
| D8 after handoff identity is durable, before intent phase update | Run record with exact session identity, leased worktree, run_durable intent | `advance-handoff-phase` | Spawn the session before handoff identity is durable |
| D8a after handoff phase is durable, before session spawn | Handoff intent, run record, leased worktree, absent exact session | `replay-session-start` with the same identity | Mint a session identity or reset the bead |
| D9 during agent work | Handoff intent and exact live session facts | Use table 4.5 | Double-launch |
| D10 after work commit, before release claim | Run branch ahead, no release claim | `retain-and-repair` | Merge from branch shape alone |
| D10f after a failed run, before Beads routing | No durable failure-class authority, bead in_progress | `repair-required` | Infer the failure class from an event or memory |
| D11 after release claim, before merge | S6 facts | `replay-release` from the immutable claim | Recompute target or dispatch head |
| D12 during merge | Release claim and exact ref facts | Use table 4.6 | Use an event as merge proof |
| D13 after merge, before bead close | S7 facts | `replay-close` with the same terminal-write identity | Re-run agent work |
| D14 after close, before close result is durable | Merge proof, close binding, current bead status | Use table 4.7 | Reopen because memory lacks a result |
| D15 after bead close, before queue terminal apply | S8 facts | `replay-queue-terminal` | Dispatch the item again |
| D15f after bead reopen, before queue terminal apply | S8f facts | `replay-queue-terminal-failure` | Dispatch the item again |
| D16 during queue terminal transaction | S8 or S8f facts plus a supported queue transaction cut | Use the queue transaction recovery result | Derive queue state from an event |
| D16x during a no-Beads-write failure | Old canonical is reserved, or new canonical is terminal(failed) | Old canonical returns `repair-required`. New canonical reaches S9x. | Infer failure from an event |
| D17 after queue terminal apply, before cleanup | S9 or S9f facts with cleanup residue | `cleanup-only` | Change bead, merge, or queue outcome |
| D17x after an unreopened failure is consumed | S9x facts | `repair-required` and retain evidence | Infer the failure class or delete evidence |

### 4.1 Claim refusal

| Exact fact | Recovery result |
| --- | --- |
| Typed dependency refusal with a prepared intent | `advance-claim-refusal` |
| Durable dependency refusal in a claim_refused intent | `fail-item` |
| Typed already-assigned refusal with a different owner | `repair-required` |
| Bead is closed with matching git completion evidence | `advance-close` |
| Supported non-open refusal with a prepared intent | `advance-claim-refusal` |
| Durable supported non-open refusal in a claim_refused intent | `release` |
| Adapter or ledger result has uncertain identity | `repair-required` |

### 4.2 Claim write classification

| Exact fact | Recovery result |
| --- | --- |
| Bead is open | `replay-claim` with the same transition ID |
| Bead is in_progress and the C20 intent owns the same dispatch | `advance-claim` |
| Bead is in_progress and ownership differs or cannot be proved | `repair-required` |
| Bead is closed with matching git completion evidence | `advance-close` |
| Bead has `other(status)` | `repair-required` |

The adapter terminal-write record can be absent after a successful write. The
Beads status is the claim result. The C20 intent supplies dispatch ownership.

### 4.3 Claimed bead with no run record

| Exact fact | Recovery result |
| --- | --- |
| Bead is in_progress and the intent identity matches | `write-run-record` |
| Bead is open | `replay-claim` |
| Bead is closed with matching git completion evidence | `advance-close` |
| Bead status conflicts with the intent | `repair-required` |

### 4.4 Session handoff classification

| Exact fact | Recovery result |
| --- | --- |
| Run-durable intent and exact run record has no session identity | `prepare-handoff` with the same run and worktree |
| Run-durable intent and exact run record has the selected session identity | `advance-handoff-phase` |
| Handoff-durable intent and exact session is absent | `replay-session-start` with the same identity |
| Handoff-durable intent and exact session is live | `adopt-live` |
| Session, run record, worktree, or intent identity differs or cannot be read | `repair-required` |

### 4.5 Active run classification

| Exact fact | Recovery result |
| --- | --- |
| Exact session is live and accepts adoption | `adopt-live` |
| Exact session is dead and a durable run outcome exists | `advance-run-outcome` |
| Exact session is dead and no durable outcome exists | `resume-dead` with the same run and worktree |
| Session, run record, worktree, or intent identity differs | `repair-required` |

### 4.6 Merge classification

| Exact fact | Recovery result |
| --- | --- |
| Bead is non-terminal and the run branch tip is ahead of `dispatch_head_sha` | `replay-release` from the claim |
| Bead is non-terminal and the run branch tip equals `dispatch_head_sha` | `reopen` from the claim |
| Bead is closed and terminal git evidence matches | `landed` |
| Bead is terminal and terminal git evidence is absent or conflicts | `repair-required` |
| The claim, bead, branch, or target ref cannot be read exactly | `repair-required` |

`merge_target_sha` records the target value at claim time. It is audit evidence.
Recovery does not use it as a merge precondition. The dispatch intent does not
supply or replace a release-claim field.

### 4.7 Close classification

| Exact fact | Recovery result |
| --- | --- |
| Bead is in_progress and merge evidence matches | `replay-close` with the same close transition ID |
| Bead is closed and merge evidence matches | `advance-close` |
| Bead is open after a matching merge | `repair-required` |
| Bead or git evidence conflicts with the close binding | `repair-required` |

## 5. Invalid combinations

### LB-005 — Conflicting identity requires repair

The following combinations always return `repair-required`.

- One bead or artifact is bound to different run IDs.
- A live registry entry has no matching reserved queue item.
- A durable run record has no dispatch intent after C20 is active.
- A session identity differs from the durable run record.
- A worktree lease differs from the dispatch run ID.
- A merge exists with no immutable release claim.
- A closed bead has no matching git completion evidence.
- A terminal queue item names an outcome that conflicts with the bead and merge facts.
- An unsupported or corrupt record is present at any exact path.
- Two records claim the same queue item, bead, run, session, worktree, or release operation.

Recovery preserves all durable evidence for an invalid combination. It can
quarantine the queue name or bead. It cannot turn an invalid combination into
an offerable item without a typed repair result.

## 6. Transition owners

### LB-006 — One component owns each transition

| Transition | Single owner after C21 |
| --- | --- |
| S0 to S1 | Dispatch transaction |
| S1 to S2 | Dispatch transaction through the Beads adapter |
| S2 to S3 | Dispatch transaction |
| S3 to S4 | Run supervisor through a durable handoff result |
| S4 to S5 or S6 | Run machine |
| S4 to S8f | Run machine through BI-010a routing |
| S4 to S9x | Run machine through the queue transaction after BI-010a selects no Beads write |
| S5 recovery | Startup dispatch-intent replay |
| S6 to S7 | Release transaction |
| S7 to S8 | Beads terminal adapter |
| S8 to S9 | Queue terminal transaction |
| S8f to S9f | Queue terminal transaction |
| Cleanup after S9 or S9f | Resource owners for run record, session, and worktree |
| S9x recovery | Reconciliation |

The scheduler selects S0 work. It does not own later transitions. The run
registry reports live process ownership. It does not recover durable state.
Events report observed transitions. They do not authorize a transition.

## 7. Dispatch transaction and replay obligations

### LB-007 — The dispatch intent binds all durable phases

The dispatch transaction must define an intent with these immutable bindings:

- queue ID, queue name, group index, and item index
- bead ID and run ID
- claim transition ID
- exact durable run-record identity
- exact session identity at handoff
- worktree lease identity

The type must not admit a claimed phase without all queue, bead, and run
bindings. A later phase can add a binding that the prior phase could not know.
It cannot change a binding that is already durable.

`claim_refused` is a terminal branch from `prepared`. It stores one actionable
definite refusal before queue compensation. `dependency_refusal` authorizes an
exact failed-item write. `supported_non_open` authorizes an exact reservation
release. An already-assigned or uncertain owner stays `prepared` and requires
repair from current Beads ownership. A refusal phase cannot carry a run or
handoff binding.

The run record alone owns the execution location. The intent binds that record
by exact run ID. Replay must decode and validate the record before it uses the
location.

The run record exists for every claimed dispatch. `run_durable` does not
require a session identity. A remote run and a shared-session run are valid at
this phase. Before session start, the run record adds the selected handoff
identity with exact compare-and-swap. The intent then advances to
`handoff_durable` with that same identity. Only `handoff_durable` authorizes
session start.

The C20 value contract was not wired to a production writer before this
amendment. Its schema-v1 `run.session_name` field therefore has no durable
production compatibility obligation. Strict decoding rejects that old draft
shape. The handoff binding remains the only intent field that carries
`session_name`.

The dispatch intent adds no new fields after handoff. Its durable record stays
until queue terminal application. Git owns the later release claim. The Beads
terminal adapter owns close. The queue transaction owns terminal outcome.

### LB-008 — Dispatch operations return one result class

The dispatch transaction must return one of these result classes:

- `committed`: the requested phase is durable.
- `replayable`: exact prior facts permit the same operation to continue.
- `refused`: no admitted durable change occurred.
- `repair-required`: facts conflict, are corrupt, or have uncertain identity.

### LB-009 — Startup classifies dispatch intents before dispatch

Startup must scan and classify intents before queue loading enables dispatch. It
must return one row from the process-death table for every supported record.
It must fail startup closed for corrupt roots or conflicting identity. A
recoverable per-intent failure may quarantine only that dispatch if the
remaining queue names can be proved independent.

## 8. Explicit non-authorities

### LB-010 — Observations do not authorize recovery

Do not use these facts to settle a crash cut:

- event presence or event order
- stderr text
- process names without exact session identity
- directory presence without a valid lease
- run registry memory after restart
- elapsed time by itself
- a branch being ahead without an immutable release claim
- a bead being `in_progress` without an exact dispatch binding
