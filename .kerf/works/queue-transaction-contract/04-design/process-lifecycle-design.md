# Process-lifecycle queue capability design

## Mode

Change design for the minimal `specs/process-lifecycle.md` amendment required
by the approved receipt architecture.

## Current state

PL-004/PL-005 inventory queue startup and reconciliation but do not know the
flat receipt root, replacement/archive intents, completion receipts, or their
whole-capability requirement. PL-027 covers process handoff but cannot refuse a
target that parses only part of the queue recovery protocol.

The queue-cancel CLI currently has a daemonless filesystem fallback in
`internal/queue/cli/cancel.go`, while `internal/daemon/socketdispatch.go`
already exposes the live `queue-cancel` RPC. Startup recovery roots in
`internal/lifecycle/startup_pl005_qm002.go` `LoadQueueAtStartup`,
`loadOneQueueAtStartup`, and `reconcileQueueTerminalState`.

## Target state

### 1. Wire inventory

The lifecycle amendment records existing queue wire behavior:

| Operation | Required retained wire |
|---|---|
| submit | `groups`, `schema_version`, `name`, `workers`, `spend_cap_usd`, `default_harness`; daemon-minted queue ID |
| append | `queue_id`, `name`, `group_index`, `bead_ids`; ID wins, then name, then main |
| status | `name`, `queue_id`, `watched_group_index`; name wins, then ID, then main; response retains `queue` and `max_concurrent` plus receipt completion |
| cancel | preserve shipped JSON `queue` and `force`; add optional `queue_id`; both must resolve to the same identity, neither is invalid (no main default); response retains `queue_id`, `prior_status` |

QueueStatus exact-ID responses may be live or receipt-backed as defined by
queue-model. `hk queue status` exposes `--watched-group-index`; lifecycle does
not redefine content.

### 2. Daemon-unreachable cancellation

`RunQueueCancel` becomes a daemon socket client only. When the target daemon is
unreachable it returns exit 17 and performs no:

- canonical or legacy queue write;
- temp, archive, replace-intent, or archive-intent write;
- receipt/root/GC write;
- event append.

No capability flag authorizes local fallback. Only the live target daemon’s
QueueStore or same-process startup adapter writes queue state.

### 3. Startup queue subphase

Before queue readiness:

```text
verify queues parent
-> create/validate flat receipt root
-> first-create queues-parent fsync
-> verify complete target capability
-> enumerate queue-owned protocol records
-> recover intents/receipts/cleanup/migration
-> load canonical queues
-> perform ledger reconciliation
-> advertise queue readiness
```

Receipt-root rules:

- mkdir definite failure leaves root uncreated;
- EEXIST succeeds only after open/type verification;
- first creation becomes durable after `.harmonik/queues` open/fsync/close;
- mkdir or parent-sync ambiguity reloads before recovery;
- wrong type or permissions fail readiness closed;
- the root is not enumerated as a queue.

Startup may run eligible GC after readiness, but unresolved completion cleanup
must resolve before the affected name becomes available.

### 4. Whole capability descriptor

The target daemon advertises one complete queue transaction capability
covering:

- replace-intent version and exact prior/candidate/temp classifier;
- archive-intent version and archive/legacy classifier;
- linked cancelled-replacement handoff: prebound origin/kind/source digest/
  destination/exact successor intent, replace-only/linked-pair/archive-only
  classification, and mismatch refusal;
- completion-receipt v1 identity/content;
- immutable completion-release marker v1 identity/content, post-cleanup and
  post-release creation order, trusted-UTC 720-hour boundary, and
  receipt-first GC;
- final replace-intent receipt binding;
- flat receipt-root create/EEXIST/type/parent-sync behavior;
- no-replace/exact-byte-idempotent receipt and release-marker install;
- queue-ID plus completed-digest CAS cleanup;
- deterministic exact-ID live/receipt status including duplicate/corrupt
  candidate failure;
- minimum retention and crash-safe GC;
- unresolved-state ownership/refusal and operator diagnostics.

Supporting only a record parser is insufficient. Missing cleanup,
release-marker, clock-regression, status, retention, or ambiguity behavior
makes the target incompatible.

### 5. Upgrade, exec, downgrade, and rollback

Before ownership handoff:

1. inspect on-disk queue-owned record/schema surface, including release
   markers and their temps;
2. compare target capability as a complete unit;
3. proceed only if the target can classify every reachable state;
4. otherwise refuse before exec/handoff and preserve all records.

An incompatible target cannot discard, reinterpret, or “clean up” newer
records. A target lacking completion-release marker v1 or its receipt-first GC
classifier must refuse handoff whenever receipts, markers, or marker temps may
exist; it cannot fall back to receipt `completed_at`. Downgrade/rollback
failure reports the record type/version or missing behavior. It does not
prescribe JSONL repair or event replay.

### 6. Startup recovery authority

The bounded startup adapter may:

- establish the receipt root;
- read and classify supported records;
- finish intent-selected replacement/archive/receipt/cleanup/migration;
- create a missing completion-release marker only after proving durable old
  canonical and intent absence and release of old ownership; classify valid,
  corrupt, unsupported, and marker-only states without events;
- install recovered canonical state in QueueStore;
- retain per-name quarantine/refusal.

It may not:

- synthesize/retry `queue_group_completed`;
- consult JSONL for completion or cleanup;
- mint/reconstruct/retimestamp/scan-select a receipt;
- create a second writer;
- generalize the protocol into repository-wide durability.

`reconcileQueueTerminalState` must consume the receipt/QM-053 primitive after
`CQ-RECEIPT`, never its current event-landmark cleanup sequence.

### 7. Readiness and degraded distinction

Unsupported/corrupt queue infrastructure before ready blocks queue readiness
and exposes operator-visible recovery state. Post-ready
`commit_indeterminate` retains per-name refusal and infrastructure diagnostics.
This does not redefine PL-010’s existing degraded vocabulary.

Observation append failure after queue-owned durability:

- never changes readiness;
- never retains a name refusal;
- never causes startup replay;
- never blocks receipt GC or admission.

### 8. Shutdown versus operator cancellation

Graceful shutdown preserves PL-011 ordering and uses queue-owned durable
cancel/archive primitives. It never emits `queue_cancelled_operator`.

Live operator cancellation:

1. completes durable cancelled replacement/archive/legacy cleanup;
2. removes archive intent durably;
3. releases QueueStore/refusal;
4. attempts the typed audit;
5. returns RPC success.

Audit failure is diagnostic only. Restart never replays the audit or response.
Neither path creates a completion receipt.

### 9. Conformance

Required lifecycle proofs:

- receipt-root first-create cuts and wrong-type EEXIST;
- valid/corrupt/unsupported/coexisting intent, receipt, and release-marker
  cases;
- delayed cleanup past receipt `completed_at + 30d`, restart, marker creation,
  clock regression/unsynchronized refusal, and the full post-release 720-hour
  boundary;
- complete-capability upgrade;
- partial-capability downgrade/rollback refusal with byte preservation;
- startup final cleanup without final-event synthesis;
- daemon-unreachable cancel exit 17 with byte-identical queue/temp/archive/
  intent/legacy/receipt/event state;
- graceful shutdown emits no operator audit;
- event failure does not change readiness/admission.

## Rationale

Queue-model owns state bytes and decisions, but lifecycle determines when a
process is safe to assume ownership. A whole-capability gate prevents a target
from accepting a schema it cannot clean up or retain correctly. Removing the
daemonless cancel writer restores the single-owner guarantee. Keeping startup
free of event replay preserves EV-021/EV-022.

## Requirements traceability

| Component | Target sections |
|---|---|
| PL1 wire/cancel | 1, 2 |
| PL2 startup/root/recovery | 3, 6, 7 |
| PL3 capability/handoff/shutdown | 4, 5, 8 |
| conformance | 9 |

## Explicit non-changes

- No new lifecycle state enum.
- No repository-wide intent protocol.
- No event-log repair surface.
- No local queue writer.
- No change to non-queue startup phases.

## Design readiness

Ready for independent change-design review with the queue, event, and
execution designs.
