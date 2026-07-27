# Receipt-architecture change-design review

## Round 1

- Reviewer: `/root/cq02_round6_design_review`
- Execution baseline:
  `b5a7cc6d4972e6e7ee4194ba259122a755963648`
- Scope:
  approved receipt research and its Round-2 approval, all four
  `04-design/*.md` artifacts, `05-changelog.md`, `07-tasks.md`,
  `CQ-02.yaml`, the approved CQ-02 card, current queue/process/event/execution
  specifications, production composition roots, CQ-00 caller/downstream
  evidence, and the previously approved archive-intent consultations
- Verdict: `REQUEST_CHANGES`

## What is sound

The four designs agree on the central receipt architecture:

- QueueStore is the sole runtime transaction owner; immutable candidate state
  and volatile generation checks precede namespace I/O.
- The replacement taxonomy distinguishes rejected, definitely uncommitted,
  directory-durable, and indeterminate outcomes.
- Final success binds one receipt ID, basename, schema, canonical byte string,
  receipt digest, and completed-canonical digest before the first ambiguous
  completed-state namespace cut.
- Receipt-root creation, EEXIST/type checking, queues-parent durability,
  no-replace receipt installation, receipt-root durability, queue-ID plus
  digest CAS cleanup, same-name reuse, retention, and GC are correctly
  queue-owned.
- Exact-ID status is deterministic and fails closed for duplicate, corrupt,
  unsupported, or identity-disagreeing receipt candidates.
- QM-053 is the sole final completion/receipt owner. The event and execution
  designs make `queue_group_completed` a normal-path-only derived observation,
  preserve EV-021/EV-022, and confine the execution-model delta to EM-015f,
  version/date, and one qualifying revision row.
- Process lifecycle owns the complete capability gate, pre-ready receipt-root
  and record recovery, downgrade/rollback refusal, and daemon-unreachable
  cancel exit 17 with no local write.
- The declared task graph is acyclic, and the `CQ-01 -> CQ-RECEIPT ->
  CQ-CALLER-CANCEL` edges serialize the current `internal/queue/rpc.go`
  conflict.

The receipt/root/CAS/GC rows in `CQ-02.yaml` are materially complete and do not
consult event history or replay a queue event.

## Required changes

### R1 — cancellation regressed the approved linked replace-to-archive handoff

`queue-model-design.md` §4 gives the replace intent only an operation kind and
an optional completion-receipt binding. Section 14 then places “cancelled
replacement via replace intent” before creation of an exact archive intent,
but it does not bind the cancellation origin, archive kind, source/candidate
digest, or selected non-overwriting archive basename in the predecessor
replace intent. It also does not require that predecessor to remain until the
exact linked successor archive intent is directory-durable.

That recreates the already-reviewed crash gap between durable cancelled
replacement and archive-intent durability. After a crash in that gap, the
canonical queue does not identify operator cancellation versus graceful
shutdown and supplies no stable selected archive destination. Minting a new
timestamp/basename after restart is not an exact retry.

Restore the approved handoff contract in the active queue design and evidence:

- preselect `archive_origin`, `archive_kind`, exact source/candidate identity
  and digest, and exact destination basename before replace-intent durability;
- bind the exact successor archive-intent identity in the cancelled
  replacement transaction;
- retain the predecessor through archive-intent
  create/write/fsync/close/rename/queue-parent-fsync;
- classify replace-only, exact linked-pair, and archive-only states
  deterministically, and quarantine every mismatched pair;
- delete the predecessor only after the exact successor is durable;
- preserve the `main` legacy rule: archive-intent removal cannot precede
  durable legacy absence through `.harmonik` parent fsync; and
- add explicit evidence/task proofs for every handoff, archive rename,
  queue-parent, legacy unlink, `.harmonik` parent, and intent-removal cut.

The aggregate cancellation row in `CQ-02.yaml` and “every archive cut” wording
in `07-tasks.md` are not substitutes for those classifier states.

### R2 — live final completion can land before startup recovery

`CQ-04` correctly depends on `CQ-RECEIPT` and `JR-01`, and `JR-03` correctly
depends on the waiter and group-activation gates. But `JR-03` does not depend
on `CQ-04`.

Consequently the current acyclic graph permits this deployed intermediate
state:

```text
CQ-RECEIPT + CQ-RUN-WAIT + JR-03 landed
CQ-04 not landed
```

At that point the live workloop may create final replace/receipt/CAS-cleanup
states and make the final observation crash-optional, while the production
startup path still executes the old `reconcileQueueTerminalState` /
`CompleteAndUnlink` behavior and cannot recover those states. A crash during
the very cuts this design introduces is therefore unsafe.

Make `CQ-04` a prerequisite of `JR-03` (or supply an equally strong ordering
that prevents receipt-producing terminal call sites before capable startup
recovery). Update the evidence DAG, accepted intermediate states, and rollback
boundaries. The receipt-governed crash/restart scenario must also depend on
`CQ-04`, directly or transitively; its current dependencies omit the component
that performs restart recovery.

### R3 — the waiter lease cannot implement the required status queries

The approved contract requires one exact-ID status query after accepted
submit/append setup and another after each heartbeat. The task grants
`CQ-RUN-WAIT` exclusive ownership of only
`cmd/harmonik/run_via_daemon.go` `viaWatchGroupCompletion` and focused tests.

The current function receives only the subscription `net.Conn`, queue ID,
group index, watched beads, and notification writer. The project/socket path
and status request client remain in its caller and sibling helpers.
Implementing an independent `queue-status` RPC therefore necessarily changes
at least the `runBeadSubcommandViaDaemon` call site/signature or an explicit
injected status-query seam. Reusing the subscription stream for a second RPC
is not a valid wire contract.

Expand the lease to the exact call site and smallest status-query
helper/interface needed, while continuing to exclude submit/append mutation,
the QueueStatus server, and the event bus. Name failing-first proof for both
the immediate post-accept query and every heartbeat query on fresh-submit and
shared-append paths. Transport failure must remain terminal, while a healthy
pending/active response continues.

### R4 — typed event leases omit required production symbols

Two event changes are not fully owned:

1. `CQ-RECEIPT` names only
   `QueueGroupCompletedPayload.CompletionReceiptID`. The event design also
   requires canonical UUID validation when the optional field is present, so
   the task must own `QueueGroupCompletedPayload.Valid` and its compatibility
   tests.
2. `CQ-CALLER-CANCEL` names `internal/core/eventtype.go` and the queue payload
   file for `queue_cancelled_operator`, but a new typed event is not decodable
   until `internal/core/eventreg_hqwn59.go` `registerQueueEvents` registers its
   constructor. The exact registry symbol and coverage/compatibility tests are
   absent from the lease and proof.

Add those symbols and proofs explicitly. Keep them serialized through the
existing `CQ-RECEIPT -> CQ-CALLER-CANCEL` edge if both tasks edit the queue
payload cohort.

### R5 — the receipt task abbreviates mandatory GC fault proof

The design and evidence matrix correctly distinguish GC eligibility,
identity revalidation, exact unlink, absence, ambiguous unlink, receipt-root
open/fsync, and close-after-successful-fsync. `CQ-RECEIPT` in `07-tasks.md`,
however, reduces this to “≥30-day GC cuts.”

The approved card requires the implementation task itself to name the
flat-file GC unlink/root-open/root-sync/close cuts, including absent and
indeterminate states. Expand the failing-first proof and rollback boundary so
the implementation cannot satisfy the card with only an age-threshold test.

## Re-review gate

Re-review after:

1. the linked cancellation handoff and its exact crash classifier are restored
   in design, evidence, and tasks;
2. startup recovery is ordered before receipt-producing live terminal
   composition;
3. `CQ-RUN-WAIT` owns an implementable status-query seam and both query
   timings;
4. payload validation and event registration have exact owners; and
5. the receipt task enumerates the already-designed GC fault surface.

No change is requested to the authoritative receipt identity, root-fsync,
CAS, exact-ID outcome table, lifecycle capability boundary, minimal
event/execution deltas, or prohibition on event authority/replay.
