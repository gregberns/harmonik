# Spec Draft review — Queue dogfood readiness

## Round 1 findings

Two independent read-only reviews found these blockers:

1. Stale queue-recover deferrals remained in queue-model and process-lifecycle.
2. `queue_recovered` lacked typed registration, compatibility, count, ownership,
   and test obligations.
3. Failed-item recovery did not reset attempts.
4. The shutdown context scope did not explicitly cover all terminal operations.
5. The lane and Step 9 drafts did not identify their final targets or contain
   complete target copies.

## Resolution

The drafts now define recovery as a durable QueueStore transaction. They add the
direct RPC and CLI surface, `queue_recovered` payload/registration obligations,
and the full cancellation-free shutdown edge. The lane and Step 9 drafts are
complete copies of their mapped target documents. The changelog declares all
operational target paths.

## Round 2 result

**APPROVE.**

The contract reviewer confirmed that all five contract findings are fixed. The
completeness reviewer confirmed all fourteen drafts, full-copy obligations, and
target mappings. Neither reviewer changed files or operational state.

## Gate before Integration

The Kerf jig requires scenario and exploratory test beads before Integration.
The stopped-daemon ledger directive authorized their creation. The open test
tasks are `hk-jthdp`, `hk-y3hvr`, `hk-ddug1`, `hk-5qhlb`, `hk-0ok7x`, `hk-xouwd`,
`hk-q5l0b`, `hk-60996`, `hk-w0gt8`, `hk-awbvt`, `hk-qhjtb`, `hk-g3t4c`,
`hk-nvewa`, `hk-a5mvu`, `hk-bwpms`, and `hk-11vgl`. This gate is satisfied.

## Post-review amendment

The approval above predates the release-claim amendment. `EM-031b` now needs a
cross-boundary review of the immutable Git-backed claim and its recovery use.
That review must confirm that the claim contains the dispatch head, resolved
merge target, and optional remote endpoint, and that neither JSONL nor a
daemon-local registry can fill a missing field.

---

## Folded in from lane bravo's parallel pass (2026-08-03)

Lane bravo ran the same pass on branch `work/queue-dogfood-readiness` before any
lane contract named that branch. The two passes reached the same shape. Bravo's
text is kept below because it names evidence, measurements and review records
this document does not. The plan of record stays T1..T12 plus T5a in
`07-tasks.md`. Where the two disagree on behaviour, the section above wins.

### Spec Draft review — Queue dogfood readiness

#### Round 1 — APPROVED

The draft directory contains all nine target documents and a changelog. Each
draft began as the current complete document. The only changes are version
updates and the nine reviewed amendments. The amendment identifiers are free in
their source documents. The cross-references form one direction: queue recovery
→ terminal recovery record → run drain → lifecycle, operator, workspace, and
event duties → assessor and scratch proof.

The readiness gate is version 3 of the assessor schema. Its required candidate,
profile, and artifact fields prevent a PASS from widening the controlled batch.
The validation ledger has one scenario and one exploratory test for each target
area. No draft changes production behavior until a later implementation pass.
