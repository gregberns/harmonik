# C3 — Dispatched queue-item reconciliation on boot — Change Spec

**Component:** On boot, reconcile `dispatched` queue items whose run did not survive — re-queue to `pending` or terminally fail — so no item is stranded as a phantom in-flight forever.
**Bead:** hk-9eury · **Goal:** G2 · **Analysis gap:** #3
**Spec home:** `specs/queue-model.md` (new sub-clause **QM-002c**, extending the QM-002a/QM-002b boot reconcile passes); `specs/execution-model.md` cross-ref (EM-031a survive-check inputs); `specs/event-model.md §8.10.7` (additive `reason` enum extension on the class-F `queue_item_reconciled`).

---

## Requirements (carried forward from 03-components.md)

- **C3-R1** — During `LoadQueueAtStartup` (PL-005 step 8a), for each `queue.json` item in status `dispatched`, the daemon MUST determine whether its run survived (re-attached live run at PL-005 step 7, OR an in-flight worktree+lease present, OR a claim-intent recovering it).
- **C3-R2** — A `dispatched` item whose run did NOT survive MUST be transitioned per the queue/reconciliation rules — to `pending` when no terminal evidence exists, OR to `failed` when reconciliation finds a terminal-failure signal. It MUST NOT remain `dispatched`.
- **C3-R3** — A `dispatched` item whose run DID survive MUST be left `dispatched` and re-driven by the normal recovery path (no double-dispatch).
- **C3-R4** — The reconciliation MUST be idempotent across repeated boots and MUST NOT interfere with the C2 bead-reset exclusion (a).
- **C3-R5** — A typed observation (event or sweep-payload count) MUST record how many items were re-queued vs failed.
- **C3-R6** — A test MUST cover: (i) dead-run dispatched → pending; (ii) terminal-failure dispatched → failed; (iii) live-run dispatched → unchanged.

## Research summary (from 04-research/C3)

**The scope is SMALLER than the decomposition assumed.** `LoadQueueAtStartup` (`internal/lifecycle/startup_pl005_qm002.go:100`) already runs two reconcile passes with a real Beads ledger (`daemon.go:829-843`): **QM-002a** (`reconcileDispatchedItems`, ~line 169) reverts a `dispatched` item to `pending` and emits `queue_item_reconciled{reason=claim_write_lost}` when `ShowBead` reports the bead **open**; **QM-002b** (`reconcileThreeWay`) catches broader mismatch classes. So the "claim-write-lost → revert to pending" case is DONE. The residual gap is precisely the cases QM-002a does NOT cover: (1) **dead run, bead still `in_progress`** (not open) with no live worktree/lease and no recovering claim-intent — QM-002a's "only confirmed-open triggers revert" rule leaves it `dispatched` forever (the incident's residual stranding); (2) **terminal-failure evidence** — a `Refs:`-less worktree with no commit and a dead run is terminal-failed work but nothing transitions the item to `failed`. C3 adds, for each `dispatched` item whose run did NOT survive (not in the EM-031a re-attached set, no in-flight worktree+lease, no recovering claim-intent): transition per evidence — `pending` when no terminal signal, `failed` when terminal-failure evidence. Ordering vs C2: `RunOrphanSweep` (C2 reset) runs at PL-005 step 3, BEFORE `LoadQueueAtStartup` (C3) at step 8a — so C2 first excludes queue-dispatched beads from reset (exclusion (a)), then C3 re-queues the now-unowned dispatched items; no double-handling (C3-R4). A dispatched item whose bead has a merge commit (Cat-3c) is `completed`/dropped, not re-queued (reuse `MergeCommitScanner`).

## Approach

Extend the boot reconcile with a new pass **QM-002c** (a sibling to QM-002a in `LoadQueueAtStartup`), governed by a new normative sub-clause **QM-002c**. Decisions recorded:

- **Decision 1 (survive-check inputs + source):** "did the run survive" = (run_id in the **pre-sweep `DiscoverActiveRuns` set**) OR (an in-flight worktree+lease present for the run_id) OR (a recovering claim-intent file present). These are the SAME signals C1 uses for worktree liveness — share the inputs. **Source note:** the active-run set is the step-3 pre-sweep `DiscoverActiveRuns` result (EM-031a; Beads non-terminal query + git task-branch-tip scan), NOT the step-7 in-memory model. `LoadQueueAtStartup` runs at PL-005 step 8a — AFTER step 7 — so the step-7 model set is technically available by step 8a; however, to keep ONE survive-check source shared with C1 (which runs at step 3, before step 7), the daemon threads the SAME pre-sweep `ActiveRunSet` (built once at step 3 per C1) into `LoadQueueAtStartup`. This avoids two divergent active-run sets (one at step 3 for C1, one at step 7 for C3) racing against the same worktree/lease filesystem state. See C1's "Survive-check source" note — the step-3 `DiscoverActiveRuns` set is the single shared source for BOTH C1 (worktree reap) and C3 (dispatched reconcile).
- **Decision 2 (dead-run classification table):** for a `dispatched` item whose run did NOT survive, classify by evidence (reusing the `internal/workspace/crashevidence.go` / WM-003a classes — the bare-worktree-with-unreconciled-sidecar evidence that C1 PL-006e (iii) preserves until step 8):

  | Evidence | Item transition | Reason tag |
  |---|---|---|
  | Merge commit bearing the bead ID exists (Cat-3c) | `completed` | `dead_run_merged` |
  | Worktree present, NO `Refs:` commit, dead run (poisoned/failed work) | `failed` | `dead_run_failed` |
  | No worktree / no terminal evidence (run died before producing artifacts) | `pending` | `dead_run_requeued` |
  | Bead reports `open` in Beads (claim-write-lost) | `pending` | `claim_write_lost` (existing QM-002a) |

- **Decision 3 (ordering / idempotency, C3-R4):** QM-002c runs within `LoadQueueAtStartup` (step 8a), strictly AFTER `RunOrphanSweep`/C2 (step 3). A reverted item becomes `pending` (not `dispatched`), so it is out of the dispatched scan on a second boot → idempotent (C3-R4 / research R2). A `completed`/`failed` item is terminal → never re-scanned.
- **Decision 4 (observability):** reuse the existing `queue_item_reconciled` event with the new `reason` values above (additive on the existing event, no new type), AND add a `dispatched_items_reconciled` integer split (`requeued` / `failed` / `completed` sub-counts) to the boot-reconcile summary for C3-R5.

### Spec text to add — QM-002c (queue-model.md, after QM-002b)

> **QM-002c — Dead-run dispatched-item reconciliation on boot**
>
> The boot reconcile of §QM-002a reverts a `dispatched` item to `pending` ONLY when the Beads ledger reports its bead `open` (the claim-write-lost case). A `dispatched` item whose run died AFTER claiming — so the bead is still `in_progress` in Beads, with no live re-attached run, no in-flight worktree+lease, and no recovering claim-intent — is NOT covered by §QM-002a and would otherwise remain `dispatched` permanently (a phantom in-flight; the incident of 2026-05-30 stranded such items). The daemon MUST reconcile these on boot.
>
> During `LoadQueueAtStartup` ([process-lifecycle.md §PL-005 step 8a]), after §QM-002a, for each item still in status `dispatched`, the daemon MUST determine whether the item's run SURVIVED. A run survived iff ANY of: (i) its `run_id` is in the **pre-sweep active-run set** computed at §PL-005 step 3 by `DiscoverActiveRuns` (EM-031a; the Beads non-terminal query + git task-branch-tip scan) — the SAME set used by §PL-006e clause (ii) for worktree-reap survival, threaded into `LoadQueueAtStartup` so C1 and C3 share one survive-check source rather than building a second set at step 7; (ii) an in-flight worktree with a live lease-lock exists for the `run_id`; (iii) a recovering `claim` intent file is present for the bead per [beads-integration.md BI-031]. A SURVIVED item MUST be left `dispatched` and re-driven by the normal recovery path; the daemon MUST NOT double-dispatch it.
>
> For a `dispatched` item whose run did NOT survive, the daemon MUST transition it by evidence, in this order:
>
> - (a) a merge commit bearing `Harmonik-Bead-ID: <bead_id>` exists on the target branch (Cat-3c per [process-lifecycle.md §PL-006c], via `MergeCommitScanner`) → transition to `completed` (the work landed); reason `dead_run_merged`;
> - (b) a worktree exists for the `run_id` with NO `Refs:`-bearing commit and a dead run (poisoned / failed work per the `internal/workspace/crashevidence.go` WM-003a classification, whose evidence §PL-006e (iii) preserves until this reconcile reads it) → transition to `failed`; reason `dead_run_failed`;
> - (c) otherwise (no worktree / no terminal artifact — the run died before producing work) → transition to `pending` (re-eligible for dispatch); reason `dead_run_requeued`.
>
> Each transition MUST persist the queue before emitting (persist-before-emit per §QM-063; `queue_item_reconciled` is a class-F fsync-backed event per [event-model.md §8.10.7], so loss must not orphan the startup reconciliation correction) and MUST emit `queue_item_reconciled` with the corresponding `reason`. The daemon MUST additionally record a `dispatched_items_reconciled` summary count split by outcome (`requeued` / `failed` / `completed`).
>
> **Ordering and non-interference (with the orphan sweep).** This reconcile runs at §PL-005 step 8a, strictly AFTER the §PL-006 orphan sweep (step 3). The orphan sweep's stale-`in_progress` reset excludes queue-dispatched beads (exclusion (a) of §PL-006), so a bead whose item is still `dispatched` at sweep time is NOT reset by the sweep; this reconcile then re-queues the item to `pending` (or terminally transitions it) on the SAME boot. A re-queued (`pending`) item's bead is left for the normal dispatch loop; no second orphan-sweep pass is required. The reconcile MUST be idempotent across boots: a reverted item is `pending` (not `dispatched`) and a terminal item is `completed`/`failed`, so neither re-enters the dispatched scan on a subsequent boot.
>
> Tags: mechanism
> Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

### Spec text to amend — execution-model.md + event-model.md

- **execution-model.md** — add a cross-reference note at EM-031a (active-run reconstruction): "the active-run set built by `DiscoverActiveRuns` is consumed at PL-005 step 3 by the §PL-006e worktree reap AND, threaded into `LoadQueueAtStartup`, by the boot dispatched-item reconcile of [queue-model.md §QM-002c] as the shared survive-check input."
- **event-model.md §8.10.7** — extend the `queue_item_reconciled` event's `reason` enum (currently `claim_write_lost` only, per the §8.10.7 taxonomy row and the §6.3 payload schema at line 1205-1211) to include `dead_run_merged`, `dead_run_failed`, `dead_run_requeued`; additive, no new event type. The event remains **class F** (fsync-backed) per the §8.10 Section-Axes list — the new reasons do not change its durability class. Amend BOTH the §8.10.7 taxonomy-row enum AND the §6.3 `reason` payload enum.

## Files & changes

| File | Change | Why |
|---|---|---|
| `specs/queue-model.md` | Add QM-002c (above) after QM-002b; changelog row. | Normative dead-run reconcile contract. |
| `specs/execution-model.md` | EM-031a cross-ref note. | Survive-check input traceability. |
| `specs/event-model.md` | Extend `queue_item_reconciled.reason` enum (additive). | New reason values. |
| `internal/lifecycle/startup_pl005_qm002.go` | Add `reconcileDeadRunDispatchedItems` pass after `reconcileDispatchedItems` (~line 169); classify by evidence; persist-before-emit; new reasons. | The C3 reconcile pass. |
| `internal/lifecycle/startup_pl005_qm002.go` (inputs) | Thread the EM-031a re-attached set + worktree/lease discovery + `MergeCommitScanner` into `LoadQueueAtStartup`. | Survive-check + evidence inputs. |
| `internal/daemon/daemon.go` (~line 829-843) | Pass the new inputs at the `LoadQueueAtStartup` call site. | Production wiring. |
| `internal/queue/...` | Add the `dispatched_items_reconciled` summary count (or fold into the existing reconcile-summary structure). | C3-R5 observability. |

## Acceptance criteria

- **AC1 (C3-R6 i, dead-run → pending):** `dispatched` item, bead `in_progress` in Beads, no re-attached run, no worktree, no claim-intent → item becomes `pending`; `queue_item_reconciled{reason=dead_run_requeued}` emitted; persisted before emit.
- **AC2 (C3-R6 ii, terminal-failure → failed):** `dispatched` item, worktree present with no `Refs:` commit, dead run → item becomes `failed`; `queue_item_reconciled{reason=dead_run_failed}`.
- **AC2b (merged → completed):** `dispatched` item whose bead has a Cat-3c merge commit → item becomes `completed`; `queue_item_reconciled{reason=dead_run_merged}`; NOT re-queued.
- **AC3 (C3-R3, live-run → unchanged):** `dispatched` item whose run_id IS in the pre-sweep `DiscoverActiveRuns` set (the same set C1 used) → item stays `dispatched`; no double-dispatch; no event.
- **AC4 (C3-R4 idempotency):** Re-running `LoadQueueAtStartup` on a second boot is a no-op for items reconciled on the first (pending/failed/completed items are not in the dispatched scan).
- **AC5 (C3-R5):** `dispatched_items_reconciled` summary reflects the requeued/failed/completed split.
- **AC6 (non-interference with C2):** A bead whose item is `dispatched` at sweep time is NOT reset by the orphan sweep (exclusion (a)); the C3 reconcile re-queues it the same boot.

## Verification

```bash
go test ./internal/lifecycle/... -run 'LoadQueueAtStartup|Reconcile|QM002|DispatchedItem' -count=1
go test ./internal/queue/...     -run 'Reconcile|Dispatched|State'                        -count=1
```

Manual: craft a `queue.json` with a `dispatched` item, a dead run, varying evidence (no worktree / `Refs:`-less worktree / merge commit); boot; assert item state + `queue_item_reconciled` reason per the classification table.

## Error handling & edge cases

- **`ShowBead` failure** during survive-check — treat as inconclusive; leave the item `dispatched` (conservative, retried next boot) and log per ON-035 rather than mis-transitioning.
- **Worktree present but lease-lock liveness ambiguous** — defer to the C1 liveness rule (lease-lock PID dead AND not in EM-031a set = dead run); if ambiguous, conservative leave-dispatched.
- **Race with C1 worktree removal** — C1 (step 3, sweep) removes a bare-stale worktree BEFORE C3 (step 8a) reads evidence; the change spec must ensure C3's "worktree present" evidence is read against the post-sweep filesystem OR C1 preserves worktrees with unprocessed sidecars (it does, per C1 PL-006e (iii)) so terminal-failure evidence survives to C3. Cross-reference: C1 PL-006e sidecar-preservation is the guard that keeps `dead_run_failed` evidence available.
- **Double-work risk** — a `pending` revert whose original run actually committed-but-did-not-merge: the bead's Cat-3c merge scanner (when the merge lands) closes it, so a re-dispatched pending item hits the noChange/subsumed path; classification rule (a) prefers `completed` when a merge commit already exists.

## Migration / backwards compatibility

Additive: QM-002c is a new pass after the existing QM-002a/b; it only acts on items QM-002a left `dispatched`. The new `reason` enum values are additive; consumers ignoring unknown reasons are unaffected. No queue.json schema change.

## Test beads

- **Scenario:** the AC1/AC2/AC3 three-case table IS the scenario test. CLI under test: `harmonik daemon` (boot queue-load). Lifecycle state: `dispatched` item with a dead run, varying terminal evidence. Observable terminal condition: queue item status (`pending`/`failed`/`completed`) in the reloaded queue + `queue_item_reconciled{reason=...}` in `.harmonik/events/events.jsonl`.
- See the shared test-bead block in `C6-boot-backoff-spec.md` §Test beads for the filed bead IDs.
