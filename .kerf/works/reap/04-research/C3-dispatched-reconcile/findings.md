# C3 — Dispatched queue-item reconciliation on boot — Research findings

Component: on boot, reconcile `dispatched` queue items whose run did not survive — re-queue to
`pending` or terminally fail — so no item is stranded as phantom in-flight forever.
Bead hk-9eury, analysis gap #3, goal G2. Anchors verified at `2e49a8df`.

## RQ1 — Does the boot path ALREADY reconcile dispatched items? (critical scoping question)

**Finding — PARTIALLY, and this materially narrows C3.** `LoadQueueAtStartup` (`internal/lifecycle/startup_pl005_qm002.go:100`) already runs TWO reconcile passes on every clean queue load, and the daemon calls it with a real Beads ledger (`daemon.go:829-843`, passing `brAdapterForQueue` + the event bus):

- **QM-002a (`reconcileDispatchedItems`, ~line 169):** for every item with `status=dispatched`, calls `ShowBead`. If Beads reports the bead **open**, it reverts the item `dispatched → pending`, re-persists (QM-001), and emits `queue_item_reconciled{reason=claim_write_lost}`. Persist-before-emit per QM-063.
- **QM-002b (`reconcileThreeWay`, ~line 151/47):** full three-way reconciliation catching mismatch classes QM-002a (dispatched-only) misses.

Item statuses: `ItemStatusDispatched="dispatched"`, `ItemStatusPending`, `ItemStatusCompleted`, `ItemStatusFailed`, `ItemStatusDeferredForLedgerDep` (`internal/queue/types.go:94-108`). Terminal = completed|failed (`queue/state.go:138`).

**So the "claim-write-lost" sub-case (Beads shows the bead open → revert to pending) is DONE.** The residual gap is the cases QM-002a does NOT cover.

## RQ2 — Exactly which dispatched cases remain unhandled (the genuine C3 work)?

**Finding:** QM-002a reverts ONLY when `ShowBead` returns open. It does NOT handle:
1. **Dead run, bead still `in_progress` in Beads, no live worktree/lease, no claim-intent recovering it** — the run died after claiming but `ShowBead` still shows in_progress (not open). QM-002a's "only confirmed-open triggers revert" rule (doc comment, `startup_pl005_qm002.go:86-89`) leaves this item `dispatched` AND the bead `in_progress`. C2's sweep may reset the bead to open on a LATER boot, but the queue item is still `dispatched` this boot → phantom in-flight (C3-R2). This is the incident's residual stranding.
2. **Terminal-failure evidence** (C3-R2 → `failed`): a `Refs:`-less worktree with no commit and a dead run is terminal-failed work, but nothing transitions the dispatched item to `failed`; QM-002a only ever reverts to pending or leaves alone.

So C3 must add: for each `dispatched` item whose run did NOT survive (not in EM-031a re-attached set, no in-flight worktree+lease, no recovering claim-intent), transition per evidence — `pending` when no terminal signal, `failed` when terminal-failure evidence (worktree present, no `Refs:` commit). C3-R3: a SURVIVED dispatched item (run re-attached) stays `dispatched` and is re-driven by the normal recovery path (no double-dispatch).

## RQ3 — Where does C3 hook, and how does it avoid double-handling with C2 (C3-R4)?

**Finding:** C3 extends `reconcileDispatchedItems` (or adds a sibling pass in `LoadQueueAtStartup`) — the right seam because it already owns the `dispatched`-item scan, the persist-before-emit ordering (QM-063), and the `queue_item_reconciled` emission. It needs the EM-031a re-attached run set (`internal/lifecycle/activerun_em031a.go`) and the worktree/lease discovery (`internal/workspace/discoverworktrees.go`) as inputs — the same "did the run survive" signals C1 uses.

**Ordering vs C2 (C3-R4):** `LoadQueueAtStartup` runs at PL-005 step 8a; `RunOrphanSweep` (C2 reset) runs at PL-005 step 3 (BEFORE socket bind, `daemon.go:743`). So the orphan sweep / C2 reset fires FIRST, then queue-load/C3. The decomposition's concern (C3-R4: a re-queued pending item's bead must be treated as queue-owned-and-recoverable, not double-handled) resolves because: C2 runs first and excludes queue-dispatched beads from reset (exclusion (a)); then C3 re-queues the now-unowned dispatched items to pending. The change spec must confirm the step-3-then-step-8a ordering holds and that a C3 pending revert does not need a second C2 pass (it doesn't — the bead reset already happened or was correctly skipped).

## RQ4 — Observability (C3-R5) and test surface (C3-R6)

**Finding:** The `queue_item_reconciled` event already carries a `reason` field (`reason=claim_write_lost` today). C3 adds new reasons (e.g. `reason=dead_run_requeued`, `reason=dead_run_failed`) — additive on the existing event, no new event type needed; OR a count `dispatched_items_reconciled` (split requeued vs failed) per C3-R5. Tests: `LoadQueueAtStartup` already has scenario tests with a recording fake emitter (the `QueueEventEmitter` seam, `startup_pl005_qm002.go`) and a fake ledger (`StatusByBead`-style); C3-R6 adds three cases — (i) dead-run dispatched → pending, (ii) terminal-failure dispatched → failed, (iii) live-run dispatched → unchanged.

## RQ5 — Are pending reverts safe to re-dispatch (no lost work)?

**Finding:** Yes — a `pending` item is re-eligible and the daemon's normal dispatch loop (`workloop.go`, capacity-gated at EM-049) will re-claim it. The risk is double-WORK if the original run actually committed but the merge/close did not land before crash; C2's Cat-3c merge-commit scanner (`GitMergeCommitScanner`) already catches "already merged" → the bead is closed, so a re-dispatched pending item whose work merged will hit the noChange/subsumed path. The change spec should cross-reference: a dispatched item whose bead has a merge-commit (Cat-3c) should be `completed`/dropped, not re-queued — reuse the `MergeCommitScanner` signal C2 wires.

## Risks / unknowns
- **R1 (terminal-vs-retriable classification):** distinguishing "dead run with no commit = retriable (pending)" from "dead run that already merged = complete" vs "dead run with a poisoned worktree = failed" requires the merge-commit + worktree-evidence signals; the classification table is a change-spec deliverable (reuse `crashevidence.go` classes).
- **R2 (idempotency across boots, C3-R4):** re-running C3 on a second boot must be a no-op for items already reconciled — naturally satisfied because a reverted item is `pending` (not `dispatched`) so it is no longer in the dispatched scan.

## No-blocker assertion
No blocker prevents a C3 change spec. The scope is SMALLER than the decomposition assumed: the
`dispatched → pending` claim-write-lost case is already implemented (QM-002a). C3 adds the dead-run
(no-open-confirmation) and terminal-failure branches plus the survive-check inputs (EM-031a set +
worktree evidence). One change-spec deliverable: the dead-run classification table.
