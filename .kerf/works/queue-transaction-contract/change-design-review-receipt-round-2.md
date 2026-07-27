# Receipt-architecture change-design review

## Round 2

- Reviewer: `/root/cq02_round6_design_review`
- Execution baseline:
  `b5a7cc6d4972e6e7ee4194ba259122a755963648`
- Scope:
  all Round-1 findings and writer dispositions, four active change designs,
  changelog, executable task plan, CQ-02 evidence, approved card/research,
  current source composition roots, active authority/replay boundaries, task
  leases, dependency graph, and accepted intermediate/rollback states
- Verdict: `REQUEST_CHANGES`

## Round-1 disposition

All five Round-1 findings are resolved:

1. The cancelled replacement prebinds origin, kind, source identity/digest,
   destination basename, and exact successor archive-intent ID/bytes/digest.
   The predecessor remains through successor parent durability, and design,
   evidence, and task proof classify replace-only, exact pair, archive-only,
   mismatch/originless, archive rename, legacy, predecessor-removal, and
   successor-removal cuts.
2. `CQ-04` is a hard `JR-03` prerequisite, the receipt crash/restart scenario
   includes `CQ-04`, and rollback cannot remove startup/waiter capability
   while receipt-producing live terminal call sites remain enabled.
3. `CQ-RUN-WAIT` owns an implementable seam: the
   `runBeadSubcommandViaDaemon` watcher call site,
   `viaWatchGroupCompletion` signature/body, and a minimal independent
   QueueStatus RPC helper/injection backed by `viaSendRequest` on a separate
   connection. Submit and append both prove the immediate and every-heartbeat
   query.
4. The exact event owners now include
   `QueueGroupCompletedPayload.Valid`, UUID compatibility tests,
   `EventTypeQueueCancelledOperator`, the XOR payload validator,
   `registerQueueEvents`, per-type compatibility, constructor/decode,
   cohort/count, and Class O coverage.
5. `CQ-RECEIPT` now names eligibility/identity revalidation, unlink failure,
   absence, unlink ambiguity, root-open failure, root-fsync
   failure/ambiguity, reload-before-retry, post-fsync close, and
   admission-nonblocking GC proofs, and forbids partial-GC rollback.

The full evidence graph is acyclic. Same-file overlaps are ordered:

- `CQ-02I -> CQ-RECEIPT` for persistence/intent integration;
- `CQ-01 -> CQ-RECEIPT -> CQ-CALLER-CANCEL` for
  `internal/queue/rpc.go` and the shared queue payload/test cohort;
- `CQ-03 -> CQ-CALLER-GROUP-ACTIVATION -> JR-03 ->
  CQ-CALLER-WORKLOOP-MAINTENANCE` for `workloop.go`; and
- `CQ-CALLER-BOOTSTRAP -> CQ-CALLER-INLINE-EXIT` for `run.go`.

The active designs/evidence continue to preserve authoritative receipt
identity, root and file fsync cuts, queue-ID plus digest CAS, deterministic
exact-ID status/waiter outcomes, whole PL capability and downgrade refusal,
minimal event and EM-015f deltas, QM-053 sole final ownership, and the
prohibition on event authority, delivery state, JSONL recovery, synthesis, or
replay. The worker prepackage classification matches the current 24 in-scope
paths before this reviewer artifact.

## Required change

### R2-1 — GC has no durable retention-age anchor

The approved card requires each receipt to remain through cleanup directory
durability and ownership release, **then for at least 30 days**. The active
queue design repeats that rule, and the task/evidence now test an
ineligible/eligible age boundary.

But receipt v1 contains only `completed_at`, which is fixed before receipt
installation and before CAS cleanup/release. The receipt is immutable.
Neither the receipt, replace intent, another retained queue-owned record, nor
the GC design records when cleanup directory durability and ownership release
actually resolved.

This makes the promised boundary unimplementable across restart:

- using `completed_at` can delete less than 30 days after release when cleanup
  was delayed by crashes or repeated indeterminate cuts;
- observing canonical/intent absence at GC time proves cleanup is complete but
  not when it completed;
- receipt file time also predates release and is not a specified durable
  protocol fact; and
- starting a process-local 30-day timer after observation is safe but not the
  specified bounded retention policy, because every restart can reset it
  indefinitely.

Define one exact, crash-safe eligibility anchor and its ownership before
drafting. Acceptable designs include a durable post-release GC-eligibility
record/metadata transition with its own schema and parent-sync cuts, or a
different explicitly approved rule that both preserves the 30-day
post-release minimum and remains bounded across restart. Do not mutate the
canonical receipt bytes in place or infer release time from JSONL.

Then update:

- queue-model receipt/retention design and the planned changelog;
- process-lifecycle capability/version and downgrade refusal for the chosen
  eligibility mechanism;
- CQ-02 evidence with creation/update, parent durability, reload,
  corrupt/unsupported, and GC interaction cuts;
- `CQ-RECEIPT` files/symbols, failing-first proof, intermediate state, and
  rollback boundary; and
- the completion scenario with delayed cleanup spanning the nominal
  `completed_at + 30 days` point, restart, and proof that GC still waits the
  full required interval after durable release.

## Re-review gate

Re-review after the retention clock is a durable, capability-gated queue fact
with complete crash cuts and an exact task owner. No other Round-1 or
cross-spec change is requested.

## Focused retention re-review

- Scope: R2-1 remediation and its design/task/scenario/evidence consistency
- Verdict: `APPROVE`

R2-1 is resolved. The immutable
`<queue_id>--<receipt_id>.release-v1.json` marker is a minimal queue-owned
retention fact, unambiguously bound by filename and content IDs, final
transaction ID, exact receipt digest, and completed-canonical digest. It is
created only after canonical and final-intent absence are directory-durable
and QueueStore/refusal ownership has released. A crash before marker
durability can only produce a later `released_at`, so it cannot shorten the
required interval.

The retention boundary is exactly `released_at + 720h`; receipt
`completed_at`, filesystem metadata, JSONL, events, and process-local timers
are explicitly excluded. Canonical timestamp validation, overflow rejection,
trusted synchronized UTC, unavailable/unsynchronized/regressed-clock refusal,
and refusal before the boundary preserve the full interval across restart.
Missing markers require fresh proof of durable cleanup, absent old ownership,
and exact receipt binding before a later conservative anchor may be created.
Conflicting, corrupt, mismatched, or unsupported markers are preserved and
disable GC without weakening receipt status or same-name admission.

Marker installation covers temp creation/write/file-fsync/close,
no-replace rename, receipt-root fsync, exact-existing idempotency, ambiguity
reload, and post-fsync close. GC revalidates the exact pair and clock, removes
and root-syncs the receipt first, and only then removes and root-syncs the
marker. Every ambiguous cut reloads exact entries. Thus a crash preserves
either the receipt plus marker or a marker-only retention witness; total
evidence becomes durably absent only after eligibility and durable receipt
absence. Marker-only recovery never recreates a receipt.

Process-lifecycle capability and downgrade refusal include marker v1,
trusted-clock eligibility, and receipt-first GC. `CQ-RECEIPT` owns the entire
schema/path/install/read/clock/GC unit and its rollback boundary. The task
proof, delayed-cleanup scenario, and CQ-02 crash matrix cover pre-release
creation refusal, restart without a marker, all marker namespace and fsync
cuts, invalid-marker classes, the exact 720-hour boundary, clock failure,
receipt-first deletion, marker-only cleanup, and final root durability.

No event-history authority, replay, delivery-state dependency, generic intent
service, or other architecture regression was introduced. The earlier
`REQUEST_CHANGES` is superseded by this focused `APPROVE`.
