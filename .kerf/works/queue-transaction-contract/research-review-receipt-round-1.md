# Receipt-architecture research review

## Round 1

- Reviewer: `/root/cq02_round6_design_review`
- Execution baseline:
  `b5a7cc6d4972e6e7ee4194ba259122a755963648`
- Scope:
  `01-problem-space.md`, `02-components.md`, all four
  `03-research/*/findings.md` artifacts,
  `plans/2026-07-24-code-health-audit/tasks/evidence/CQ-02.yaml`, the approved
  CQ-02 card, CQ-00 evidence, current queue/process/event/execution specs,
  production composition roots, the prior research and change-design
  consultations, and the downstream task-pack audit
- Verdict: `REQUEST_CHANGES`

## What is sound

The six research artifacts now agree on the central architecture:

- canonical queue bytes, replace/archive intents, and the queue-owned receipt
  are authoritative; JSONL is observational;
- final success preallocates one receipt identity and binds its exact basename,
  schema, canonical bytes, digest, and completed-canonical digest before the
  first ambiguous completion namespace cut;
- receipt-file and receipt-root durability precede queue-ID plus exact-digest
  CAS cleanup, and cleanup cannot touch a newer same-name queue;
- archive-intent identity, main-legacy ordering, and `.harmonik` parent-sync
  rules from the two earlier architecture consultations are preserved;
- both fresh-submit and shared-append waiters use the accepted queue ID and
  exact watched group, with live status first and a retained receipt only
  after canonical cleanup;
- `queue_group_completed` is a normal-path, fsync-backed attempt whose absence
  cannot gate cleanup, ownership release, readiness, GC, or admission;
- execution-model scope is confined to EM-015f, while process-lifecycle owns
  readiness and whole-capability upgrade/downgrade refusal; and
- the intended completion spine is
  `CQ-02I -> CQ-RECEIPT -> CQ-RUN-WAIT -> JR-03`.

Those conclusions match the current contradictions and composition roots:
`internal/queue/persistence.go` `CompleteAndUnlink`,
`internal/daemon/workloop.go` `evaluateGroupAdvanceWithOutcome`,
`internal/lifecycle/startup_pl005_qm002.go`
`reconcileQueueTerminalState`, `internal/queue/rpc.go`
`HandleQueueStatus`, and `cmd/harmonik/run_via_daemon.go`
`viaWatchGroupCompletion`.

The active research files contain only negative references to effect keys,
outboxes, segmentation, and cursor migration. They do not reintroduce that
architecture.

## Required changes

### R1 — CQ-02 evidence is still the superseded pre-receipt contract

`tasks/evidence/CQ-02.yaml` does not satisfy the approved card's evidence
schema. A direct YAML-key comparison finds these required top-level sections
absent:

```text
execution_baseline
execution_topology
normative_targets
execution_model_scope_proof
cancel_wire_inventory
replace_intent
completion_receipt
worker_prepackage
```

The crash matrix contains zero receipt or receipt-root cuts. Its transaction
and result rows still reserve an ordered effect batch, mutation-refuse a name
after an event failure, and make ordinary success depend on completion of a
success-event batch. Its final-completion rows still call
`queue_group_completed` the logical landmark, inspect event history, replay a
missing final event on restart, and retain completed ownership after append
failure. The `event_ordering` section assigns startup recovery emitters to
queue events. All of those claims contradict the approved receipt architecture,
EV-021/EV-022 preservation, and the six reviewed research artifacts.

Replace those rows rather than layering receipt prose beside them. Evidence
must make event append failure diagnostic-only, set final-event recovery to
never, and prove that restart classifies completion solely from the bound
intent/canonical/temp/receipt facts.

### R2 — the evidence needs the complete receipt durability and GC matrix

The research prose describes the protocol, but the evidence has no executable
cut classification for it. Add rows covering at least:

- receipt-root `mkdir`, EEXIST directory/type verification, queue-parent
  open/fsync/close, and ambiguous first creation;
- receipt temp create/write/fsync/close;
- no-replace install, exact-byte idempotent target, different existing target,
  rename/root-open/root-fsync ambiguity, and close after successful fsync;
- crash after completed-canonical durability but before receipt durability;
- crash after receipt durability but before the normal-path event attempt;
- event append failure and crash before/after the attempt, both continuing
  receipt-governed cleanup without replay;
- CAS cleanup for exact queue ID plus exact completed digest, canonical
  absence, newer same-name queue, same ID with different digest, unlink, queue
  parent open/fsync/close, and ownership/refusal release;
- minimum-retention eligibility and GC read/revalidate/unlink/receipt-root
  open/fsync/close cuts, including ambiguous unlink/root sync and admission
  non-blocking behavior.

Every row must retain the one intent-bound receipt identity. Recovery must
never mint, reconstruct, timestamp again, or choose a completion receipt by
directory scan.

### R3 — exact-ID receipt lookup does not define duplicate-candidate behavior

`03-research/queue-model/findings.md` C10 says an exact-ID lookup “matches an
unexpired receipt with that exact queue ID,” but the request supplies no
`receipt_id` and the flat basename contains both IDs. An implementation
therefore has to enumerate or index queue-ID candidates. The research defines
corrupt and identity-inconsistent handling but not the zero/one/multiple
candidate rule; a first-match implementation would make status and waiter
outcomes directory-order-dependent.

Define the lookup literally:

- exact live queue ID wins;
- otherwise exactly one unexpired receipt whose filename and canonical content
  agree on queue ID, receipt ID, schema, and digest may answer;
- zero candidates is not found;
- multiple valid candidates, a matching-prefix corrupt/unsupported candidate,
  or any filename/content disagreement is an explicit identity-integrity
  error, never first-match success and never name fallback.

This scan is a read-only status lookup. It must not weaken the separate rule
that intent recovery writes only the intent-bound basename and never
scan-selects a receipt.

### R4 — implementation evidence omits the receipt/waiter cards and unsafe
same-file serialization

The evidence has neither `CQ-RECEIPT` nor `CQ-RUN-WAIT`, and its `JR-03`
prerequisites omit the required waiter gate. Rebuild the slices and downstream
edges around:

```text
CQ-02I -> CQ-01 -> CQ-RECEIPT -> CQ-RUN-WAIT -> JR-03
```

The extra `CQ-01 -> CQ-RECEIPT` serialization is required because CQ-01
currently leases all of `internal/queue/rpc.go`, while receipt-backed
`HandleQueueStatus` also writes that file. If the cards instead narrow leases
to exact symbols, the evidence must say so explicitly; same-file agents may
not run concurrently.

Also record:

- `CQ-CALLER-CANCEL` conflicts with `CQ-RECEIPT` on
  `internal/queue/rpc.go`, unless an explicit ordering edge serializes them;
- `CQ-04` requires `CQ-RECEIPT` and `JR-01`, because startup currently invokes
  completion cleanup and must never synthesize the final event;
- `JR-03` requires `CQ-RUN-WAIT` and
  `CQ-CALLER-GROUP-ACTIVATION`;
- `CQ-02I` owns only the generic transaction/intent substrate and no receipt,
  QueueStatus, QM-053, or completion-payload semantics;
- `CQ-RECEIPT` owns receipt/QM-053/status primitives and the conditional
  payload field, but not terminal workloop call sites or the global event
  writer; and
- `CQ-RUN-WAIT` exclusively owns
  `viaWatchGroupCompletion` and both fresh-submit and shared-append tests.

The accepted intermediate states must say that no card enables
crash-optional group-observation delivery before `CQ-RUN-WAIT`.

### R5 — cross-spec, capability, wire, and waiter proof is absent from evidence

The research conclusions are plausible, but the evidence does not yet carry
the card-required proof:

- no `normative_targets` or explicit QM-033/EM-015f disposition;
- no machine proof that the execution-model delta is EM-015f-only;
- no target capability record covering replace/archive intent versions,
  receipt schema/root/install, CAS cleanup, exact-ID status, retention/GC, and
  unresolved-state refusal across startup/upgrade/downgrade/rollback;
- no cancel wire inventory proving daemon-unreachable exit 17 and zero queue,
  temp, archive, intent, legacy, receipt, or event writes; and
- no receipt-backed waiter proof for missing non-final success, final success,
  pause/failure, complete and incomplete own-bead observations, status
  transport failure, and same-name reuse.

Populate those sections from the reviewed research and current source
inventory. Do not infer conformance from the prose alone.

## Stale downstream artifacts

The prior `research-review.md` and `change-design-review.md` are historical
consultations and must remain byte-identical; their superseded effect/outbox
discussion is not an active contract.

The following non-historical downstream artifacts still contain active
effect-key/outbox/segment/cursor or event-landmark designs and must not be
advanced or copied to `specs/`:

- `04-design/queue-model-design.md`
- `04-design/event-model-design.md`
- `04-design/process-lifecycle-design.md`
- `05-spec-drafts/queue-model.md`
- `07-tasks.md`

They require a later pass rewrite from the approved receipt research. In
particular, the current scenario in `07-tasks.md` must replace “exactly one
final `queue_group_completed` JSONL record” with exactly one authoritative
receipt, allow the event to be absent, and gate terminal proof on
`CQ-RUN-WAIT` plus `JR-03`.

## Re-review gate

Re-review after the CQ-02 evidence is regenerated and the R3 lookup rule is
made explicit in the active research. Approval requires:

1. the approved-card YAML validator to pass;
2. no active evidence row to inspect JSONL or replay a queue event for
   completion/recovery;
3. complete receipt/root/CAS/GC and exact-ID waiter cuts;
4. the non-overlapping receipt/waiter DAG and same-file conflicts; and
5. evidence of EM-015f-only scope and whole-capability lifecycle refusal.
