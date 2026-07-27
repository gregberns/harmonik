# Receipt-architecture research review — Round 1 writer disposition

This is a writer response to
`research-review-receipt-round-1.md`. It does not alter that reviewer artifact
or assert approval.

## R1 — Evidence contract replaced

Rebuilt `tasks/evidence/CQ-02.yaml` to the approved card’s exact top-level
schema. Removed the ordered-event-batch, event-landmark, event-history recovery,
restart replay, and observation-failure refusal claims. Completion recovery now
uses only intent/canonical/temp/receipt facts; final-event recovery is `never`
and failure is diagnostic-only.

## R2 — Receipt durability and GC cuts added

Added executable rows for:

- receipt-root mkdir, EEXIST/type verification, first-create queue-parent
  open/fsync/close, and ambiguity;
- receipt temp create/write/fsync/close, no-replace install, exact-byte
  idempotency, conflicting target, root open/fsync/close, and ambiguity;
- completed-canonical-before-receipt and receipt-before-event crashes;
- append failure and crash before/after the normal-path event attempt;
- queue-ID plus exact-digest CAS outcomes, unlink and queue-parent durability;
- ownership-release prohibition while queue-owned state is unresolved;
- retention eligibility and every flat-file GC unlink/root-sync state; and
- submit/append waiter, capability, cancellation, migration, and stale
  generation cuts.

Every completion row retains the one intent-bound receipt identity and forbids
minting, reconstruction, retimestamping, or recovery scan-selection.

## R3 — Exact-ID multiplicity is deterministic

Updated queue research, component decomposition, and evidence. Exact live ID
wins. Otherwise exactly one unexpired receipt whose filename, canonical
content, schema, IDs, and digest agree may answer. Zero is not found. Multiple
valid candidates, a matching-prefix corrupt/unsupported candidate, or any
identity disagreement is an explicit identity-integrity error. Directory-order
first match and name fallback are forbidden.

The enumeration is read-only status lookup. Intent recovery still writes only
the bound basename/bytes and never scan-selects a receipt.

## R4 — Slices and conflicts rebuilt

Evidence now records:

```text
CQ-02I -> CQ-01 -> CQ-RECEIPT -> CQ-RUN-WAIT -> JR-03
```

`CQ-01` precedes `CQ-RECEIPT` because both touch
`internal/queue/rpc.go`. `CQ-CALLER-CANCEL` also serializes after
`CQ-RECEIPT`. `CQ-04` requires `CQ-RECEIPT` and `JR-01`; `JR-03` requires
`CQ-RUN-WAIT` and `CQ-CALLER-GROUP-ACTIVATION`.

The generic substrate excludes receipt/status/QM-053/payload ownership;
`CQ-RECEIPT` excludes terminal workloop call sites and global event mechanics;
`CQ-RUN-WAIT` exclusively owns `viaWatchGroupCompletion` and both submit and
append tests. Every intermediate state keeps optional group-observation
delivery disabled through waiter integration.

## R5 — Cross-spec and operational proof added

Evidence now includes all four normative targets, explicit QM-033/EM-015f and
EV-021/EV-022 dispositions, the literal-preamble EM-015f scope proof, complete
startup/upgrade/downgrade/rollback capability, cancellation wire/exit-17
inventory, and missing-observation waiter cases including same-name reuse,
candidate multiplicity, group/index integrity, and transport failure.

## Downstream stale artifacts

The three active design files, queue-model draft, changelog, and task file now
carry explicit superseded stop markers. Their rejected effect/outbox/segment/
cursor/event-landmark content exists only in historical snapshots and cannot
be copied or dispatched. They will be rebuilt only after research approval.
