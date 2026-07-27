# CQ-02 implementation task-pack review

## Verdict

**REQUEST_CHANGES**

The materialized pack has the correct high-level decomposition and truthful
planning closure, but it is not yet safe to approve for dispatch. Three
contract gaps leave worker scope/proof incomplete, and one conflict declaration
does not fully encode the required same-file serialization.

## Blocking findings

### 1. CQ-01 omits two approved production symbols

The tracked Kerf contract in
`.kerf/works/queue-transaction-contract/07-tasks.md` assigns CQ-01 all of:

- `internal/queue/rpc.go` `HandleQueueSubmit`
- `HandlerAdapter.HandleQueueSubmit`
- `HandleQueueAppend`
- `HandlerAdapter.HandleQueueAppend`
- `appendUnderLock`

`tasks/CQ-01.md` names the submit symbols and `appendUnderLock`, but omits
`HandleQueueAppend` and `HandlerAdapter.HandleQueueAppend`. The objective's
generic reference to append is not an exact production lease. Add both symbols
to the card's exact ownership/evidence boundary and make their failing-first
coverage and rollback explicit.

### 2. CQ-RECEIPT and CQ-CALLER-CANCEL do not materialize their indexed mutation proof

`TASK-INDEX.yaml` requires `mutation` for both `CQ-RECEIPT` and
`CQ-CALLER-CANCEL`, but neither task card defines a mutation oracle or includes
mutation in its completion gate.

- `tasks/CQ-RECEIPT.md` ends with fault/property/race/compatibility tests only.
  It must state which receipt/status/QM-053 mutations must be rejected and
  require that proof at completion.
- `tasks/CQ-CALLER-CANCEL.md` ends with
  fault/restart/compatibility/race tests only. It must state mutations that,
  at minimum, restore daemon-down local writes or violate the linked
  replacement/archive ordering and require that proof at completion.

Because workers are dispatched from their cards, index-only proof labels are
not a self-contained executable contract. Each card should also identify the
production RPC/CLI end-to-end path that exercises the mutation oracle, rather
than leaving the proof as a list of classifier cases.

### 3. CQ-CALLER-WORKLOOP-MAINTENANCE is not fully serialized against WL-REC-01

Both cards edit `internal/daemon/workloop.go`, have no prerequisite edge
between them, and use different lease families (`dispatch_spine` and
`workloop_recovery_spine`). `WL-REC-01.conflicts_with` names
`CQ-CALLER-WORKLOOP-MAINTENANCE`, but the maintenance card/index entry does not
name `WL-REC-01`; its prose only conflicts with active `dispatch_spine` cards.

Make this collision directionally explicit in both index entries and both
cards, or add an intentional hard ordering edge. The current one-sided record
does not preserve the index's bidirectional conflict invariant and can permit
parallel same-file claims in a directional conflict check.

### 4. CQ-CALLER-WORKLOOP-MAINTENANCE lacks an exact symbol boundary

The card names four semantic regions in `internal/daemon/workloop.go` but no
containing production symbol or stable branch anchors. This is the only new
production card whose “exact ownership” section is not symbol-executable.
Name `runWorkLoop` and the exact current helper/call/branch anchors for deferred
reevaluation, claim-skipped dependency handling, cross-queue duplicates, and
max-attempt persistence. Apply the same anchors to its mutation and rollback
scope.

## Checks that passed

- The pack creates exactly the six approved production cards:
  `CQ-RECEIPT`, `CQ-RUN-WAIT`, `CQ-CALLER-CANCEL`,
  `CQ-CALLER-GROUP-ACTIVATION`,
  `CQ-CALLER-WORKLOOP-MAINTENANCE`, and
  `CQ-CALLER-INLINE-EXIT`.
- `CQ-EXPLORE-CANCEL` is separately justified, depends on
  `CQ-CALLER-CANCEL` and `CQ-04`, is fixture/report-only, and restricts
  Nemotron/Pi to mechanical expansion of an approved oracle. It does not
  repurpose closed `CQ-DEF-01`.
- The 91-task index parses with unique IDs, no unknown hard dependency, and no
  dependency cycle.
- The receipt/waiter/startup/terminal spine and the queue-RPC serialization
  edges match tracked `07-tasks.md`.
- The named existing production symbols resolve in the baseline tree.
- CQ-02 is truthfully closed as a planning/spec package: the approved package
  commit is an ancestor of the recorded integrated SHA, proofs and reviews are
  present, and the note explicitly leaves implementation to open downstream
  cards.
- CQ-07 and JR-05 no longer treat `queue_group_completed` or JSONL as terminal
  authority; they require exact queue-ID live/receipt truth and cover
  suppressed observation plus same-name reuse.
- The worktree has no implementation, spec, Kerf, Beads, or unrelated state
  drift. Apart from this review, changes are confined to the task index,
  task cards, and the writer summary.

## Approval condition

Resolve all four findings, then re-run the YAML graph/conflict audit and a
fresh independent card review. No production implementation should be
dispatched from this pack before that review approves it.

---

## Focused re-review — 2026-07-27

### Verdict

**APPROVE**

All prior blocking findings are resolved:

1. `CQ-01` now assigns `HandleQueueAppend` and
   `HandlerAdapter.HandleQueueAppend` alongside both submit entry points and
   `appendUnderLock`. Its failing-first coverage and rollback boundary include
   all four production entry points.
2. `CQ-RECEIPT` now defines a production daemon-RPC/CLI end-to-end mutation
   oracle, representative receipt/status/QM-053 mutations, detached-worktree
   failing-first evidence, restored-source passing evidence, and an explicit
   mutation completion gate.
3. `CQ-CALLER-CANCEL` now defines the production
   `RunQueueCancel` → `tryDaemonQueueCancel` →
   `HandlerAdapter.HandleQueueCancel` end-to-end path, daemon-down and linked
   handoff mutations, detached-worktree failing-first evidence,
   restored-source passing evidence, and an explicit mutation completion gate.
4. `CQ-CALLER-WORKLOOP-MAINTENANCE` and `WL-REC-01` now name each other in
   both task cards and in both `TASK-INDEX.yaml` conflict lists. They remain
   unordered but cannot be claimed concurrently despite using different lease
   families.
5. `CQ-CALLER-WORKLOOP-MAINTENANCE` now names `runWorkLoop` and stable current
   anchors for deferred reevaluation, claim-skipped dependency handling,
   cross-queue duplicate handling, and max-attempt handling. Its proof and
   rollback scopes use the same four anchors.

The amended index still contains 91 unique tasks, no unknown prerequisite, and
no dependency cycle. The named append and `runWorkLoop` anchors resolve in the
current production tree. `git diff --check` is clean, and the correction
introduced no implementation, spec, Kerf, Beads, or unrelated dependency/scope
change.

The CQ-02 implementation task pack is self-contained and safe to advance
through the normal per-card independent review and dispatch gates.
