# Queue-model transaction and completion-receipt design

## Mode

Change design for `specs/queue-model.md`, based on the independently approved
receipt research. This document describes the target contract; it is not
normative spec text.

## Current state

The current specification and implementation have five coupled defects:

1. QM-001 specifies atomic replacement but does not distinguish failure before
   namespace mutation from ambiguity after rename.
2. QM-060 names a writer but production callers still mutate live pointers and
   call `Persist`, `Unlink`, or `ArchiveFailedQueue` directly.
3. QM-003 unlinks completed canonical state while QM-033 calls the final
   `queue_group_completed` event the durable landmark, contradicting
   event-model EV-021/EV-022.
4. `HandleQueueStatus` can find live queues by name or ID but has no retained
   exact-ID completion authority after unlink.
5. Cancellation/migration have no universal restart-visible handoff record for
   every rename/unlink ambiguity.

Production roots are `internal/queue/persistence.go` `Persist`,
`CompleteAndUnlink`, `ArchiveFailedQueue`, `Unlink`, and
`MigrateFromLegacy`; `internal/queuewiring/store.go` `QueueStore`; and
`internal/queue/rpc.go` submit/append/status/cancel handlers.

## Target state

### 1. Canonical topology

The live-write layout is:

```text
.harmonik/queues/<normalized-name>.json
```

`.harmonik/queue.json` is readable only as the legacy input for `main`.
Canonical queue envelopes remain schema version 1. Protocol records are
siblings or infrastructure children:

```text
<name>.replace-intent
<name>.archive-intent
.completion-receipts/<queue_id>--<receipt_id>.json
```

The receipt root is flat and excluded from live-queue enumeration. No
per-queue receipt directory exists.

Topology reconciliation:

| Disk state | Decision |
|---|---|
| valid legacy only | create directory-durable `main`, then remove legacy durably |
| valid canonical only | load canonical |
| both valid and byte-equivalent | retain canonical, remove legacy durably |
| both valid but divergent | preserve both and refuse |
| either relevant copy corrupt/unsupported | preserve and refuse |
| neither | no queue |

### 2. Sole transaction owner

The single daemon/project `QueueStore` owns every runtime mutation. A bounded
same-process startup adapter resolves queue-owned records before QueueStore
installation; it is not a second writer.

Each normalized name has one transaction domain. Multi-name operations acquire
names lexically and return independent results. The transaction input is an
immutable detached snapshot plus volatile process-local generation:

```text
lock name
-> snapshot + expected generation
-> validate request/generation
-> deep clone/create candidate
-> mutate and validate candidate
-> marshal exact bytes and applicable normal-path payload
-> persist/classify
-> install selected durable state
-> advance generation once
-> Wake / normal-path observation
```

Stale generation is rejected before I/O. Generation is never persisted and
never restart evidence.

### 3. Result taxonomy

| Result | Disk guarantee | Memory/install | Next action |
|---|---|---|---|
| `rejected` | no namespace I/O | retain prior generation | correct input/resnapshot |
| `not_committed` | intended namespace mutation definitely absent | retain prior | bounded exact retry or refusal |
| `committed_durable` | selected namespace state parent-synced | install selected state/absence | continue remaining protocol only |
| `commit_indeterminate` | namespace mutated; parent durability unresolved | retain prior install, quarantine name | exact reload/compare/sync |

Validation, conflict, size, and stale generation are `rejected`.
Marshal, derived-payload construction, and candidate-temp failures before the
intent are `not_committed`. Once the intent is durable, classification is from
the exact intent/canonical/temp/receipt facts. A directory-close failure after
successful fsync is diagnostic.

No event outcome changes one of these results.

### 4. Universal replace intent

Every ordinary canonical replacement uses:

```yaml
schema_version: 1
transaction_id: <UUIDv7>
operation_kind: <enum>
normalized_name: <name>
queue_id: <UUIDv7>
canonical_basename: <name>.json
prior:
  state: absent | present
  sha256: <digest when present>
candidate:
  sha256: <digest>
  temp_basename: <already-durable selected temp>
wake_required: <boolean>
completion_receipt: null | <final-success binding>
archive_handoff: null | <cancel/archive successor binding>
```

Covered operations include create, submit, append, reservation, Run-ID patch,
activation/advance, pause/resume, eager refill, budget/review charge,
maintenance/reconciliation, startup, inline/bootstrap, crew placeholder, and
cancelled-state replacement before archive.

Protocol:

1. snapshot/validate/clone/mutate;
2. marshal exact canonical bytes and construct any normal-path payload;
3. create/write/fsync/close candidate temp;
4. create/write/fsync/close and directory-durably install replace intent;
5. rename candidate to canonical and sync queue directory;
6. install memory/generation;
7. Wake and attempt applicable observations;
8. remove intent and sync only after its queue-owned work resolves.

The exact ordinary path is
`.harmonik/queues/<normalized-name>.replace-intent`; installation is
temp/file-sync/no-replace/queue-parent-sync. Exact existing bytes are
idempotent, while different/corrupt/unsupported bytes refuse. Ordinary success
unlinks that exact intent and syncs the queue parent before releasing the name.
Recovery exhaustively distinguishes exact candidate canonical, exact selected
temp plus prior, prior/absence with an abandoned selected temp, prior/absence
with no temp, and mismatch/corruption. Rename, unlink, and parent-sync
ambiguities always reload exact paths; cleanup never guesses from generation
or events.

Exact prior/candidate equality collapses before temp/intent creation.

The intent stores no event ID, effect key, cursor, event-delivery state,
acknowledgement, payload batch, or RPC response.

For a cancelled-state replacement that must flow into archive, the predecessor
replace intent additionally fixes before its own durability:

```yaml
archive_handoff:
  archive_origin: operator-cancel | graceful-shutdown | inline-failure | recovery-corrupt
  archive_kind: cancelled | failed | corrupt
  source_identity: <queue ID or corrupt-source identity>
  source_candidate_sha256: <exact cancelled/candidate bytes digest>
  destination_basename: <selected non-overwriting archive basename>
  successor_archive_intent_id: <UUIDv7>
  successor_archive_intent_bytes_base64: <exact canonical bytes>
  successor_archive_intent_sha256: <digest>
```

This prebinding distinguishes operator cancellation from shutdown after crash
and prevents destination retimestamping. The predecessor remains until that
exact successor archive intent completes create/write/fsync/close, rename, and
queue-parent fsync.

The exact successor path is
`.harmonik/queues/<normalized-name>.archive-intent`. Its v1 schema binds
archive intent ID, predecessor transaction ID, origin, kind, normalized name,
parseable queue ID or exact corrupt-source identity, source digest, and
destination basename. It does not contain a digest of the final predecessor
record. Installation and destination archive are
no-replace/exact-byte-idempotent. Recovery covers predecessor-only, exact pair,
successor-only, changed source/destination, definite failures, and ambiguous
rename/unlink/parent-sync. It removes/syncs the predecessor only after
successor durability and removes/syncs the successor only after exact archive
and legacy durability; ownership releases only after both intent absences are
durable.

Recovery:

- exact candidate canonical: finish parent sync and promote;
- exact prior/absence: replacement not committed; clean only selected temp;
- exact candidate temp plus prior: retry selected rename;
- third/corrupt/identity-mismatched state: preserve, quarantine, refuse;
- never consult JSONL or a persisted generation.

### 5. Ordinary mutation visibility

Submit and first-group activation are one candidate and one replacement.
Successful normal-path observation order is Wake, `queue_submitted`,
`queue_group_started{0}`, then response. Observation failure is diagnostic and
does not retain transaction refusal.

Advance similarly commits current terminal state together with successor
activation. Failure handling commits the group's `complete-with-failures` and
queue `paused-by-failure` in one candidate/replace transaction, then attempts
the group-completed and paused observations. No intermediate one-sided state
is visible. QM-051/QM-052 own state transition, not final cleanup or event
authority.

### 6. Final-success intent binding

For final `complete-success`, before completed canonical installation:

1. allocate one canonical lowercase UUIDv7 `receipt_id`;
2. fix one completion timestamp;
3. marshal exact completed canonical bytes;
4. construct exact canonical receipt v1 bytes;
5. calculate their SHA-256 digest;
6. record ID, basename, schema, base64 bytes, and digest in the durable replace
   intent.

The binding is:

```yaml
completion_receipt:
  receipt_id: <UUIDv7>
  basename: <queue_id>--<receipt_id>.json
  schema_version: 1
  canonical_bytes_base64: <exact bytes>
  sha256: <digest of exact receipt bytes>
```

The intent remains until receipt durability and canonical-unlink directory
durability are classified. Recovery reuses the exact binding; it never mints,
reconstructs, retimestamps, or scan-selects a receipt.

### 7. Completion receipt v1

Path:

```text
.harmonik/queues/.completion-receipts/<queue_id>--<receipt_id>.json
```

Canonical content binds:

```yaml
schema_version: 1
queue_id: <UUIDv7>
receipt_id: <UUIDv7>
transaction_id: <UUIDv7>
normalized_name: <name>
final_group_index: <integer>
final_status: complete-success
success_count: <integer>
fail_count: 0
completed_at: <timestamp>
completed_queue_sha256: <digest of exact completed canonical bytes>
```

Filename and content identities must agree. `(queue_id, receipt_id)` is the
receipt identity; it is immutable and survives canonical unlink and same-name
reuse.

### 8. Receipt-root establishment

Before readiness or queue recovery:

1. `mkdir(.completion-receipts)`;
2. EEXIST succeeds only after open/type verification;
3. after first creation, open/fsync/close `.harmonik/queues`;
4. ambiguity reloads and reclassifies before recovery.

Wrong type or unsupported capability fails startup closed. The root itself is
capability-owned infrastructure, not a queue.

### 9. Receipt installation

Installation uses:

```text
unique temp in durable receipt root
-> write exact intent-bound bytes
-> fsync file
-> close
-> no-replace rename to exact bound basename
-> open/fsync/close receipt root
```

An existing target is idempotent success only when exact canonical bytes,
digest, filename IDs, content IDs, and schema agree. A different target is
preserved and fails closed. Rename/root-sync ambiguity reloads the exact bound
target/temp. Receipt durability is established only after root fsync; later
close failure is diagnostic.

### 10. QM-053 completion order

QM-053 is the sole final completion owner:

```text
completed canonical candidate and receipt binding fixed
-> replace intent durable
-> completed canonical durable/install
-> exact receipt and receipt root durable
-> attempt final queue_group_completed once on normal path
-> queue-ID + completed-digest CAS
-> canonical unlink + queue-directory fsync
-> remove resolved intent + queue-directory fsync
-> clear/release QueueStore ownership
-> install immutable completion-release marker + receipt-root fsync
```

Every unresolved intent continues to own/refuse the name. A free name can
never coexist with unclassified canonical or intent state. Release-marker
installation is deliberately after release and does not gate admission:
failure or a crash in that final step retains the receipt and disables GC,
but does not reacquire the old name.

Restart may finish receipt creation and receipt-governed cleanup. It never
synthesizes or retries the final observation.

### 11. CAS-safe canonical cleanup

| Canonical state | Action |
|---|---|
| exact queue ID and exact completed digest | unlink and queue-parent sync |
| absent | idempotent cleanup; establish parent durability |
| same name, different queue ID | newer queue; never touch |
| same queue ID, different digest/corrupt | preserve, quarantine, refuse |

Unlink/parent-sync ambiguity retains completion ownership until exact reload
classifies absence, old exact canonical, or newer queue. Same-name reuse is
legal after old absence durability and ownership release, regardless of event
outcome.

### 12. Retention and GC

Receipt `completed_at`, receipt filesystem times, JSONL, and process-local
timers are never retention authority. The durable retention anchor is a
separate immutable completion-release marker:

```text
.harmonik/queues/.completion-receipts/
  <queue_id>--<receipt_id>.release-v1.json
```

Canonical marker v1 binds:

```yaml
record_type: completion-release
schema_version: 1
queue_id: <receipt queue UUIDv7>
receipt_id: <receipt UUIDv7>
transaction_id: <final replacement transaction UUIDv7>
receipt_sha256: <digest of exact canonical receipt bytes>
completed_queue_sha256: <digest copied from and equal to the receipt>
released_at: <UTC timestamp sampled after QueueStore ownership release>
gc_not_before: <released_at plus exactly 720 hours>
```

The filename IDs, marker IDs, transaction ID, both digests, and referenced
receipt must agree. Marker bytes and timestamps are immutable. A valid existing
marker at the deterministic basename is authoritative idempotent success;
different, corrupt, or unsupported bytes are preserved, diagnosed, and disable
GC for that receipt. They do not weaken receipt-backed status or block
same-name admission.

The normal path samples `released_at` only after canonical absence and resolved
intent absence are directory-durable and after QueueStore/refusal ownership is
released. It then uses the receipt installation primitive:

```text
unique temp in receipt root
-> write canonical marker bytes
-> fsync file
-> close
-> no-replace rename to deterministic marker basename
-> open/fsync/close receipt root
```

A rename or receipt-root-sync indeterminate result reloads the deterministic
target and temp. If a valid bound target exists, its original timestamps win.
If the target is durably absent, a later attempt may sample a later
`released_at`; this is conservative. Close after successful root fsync is
diagnostic.

After a crash with a receipt but no marker, startup or queue-owned maintenance
may create the marker only after exact classification proves all of:

1. the old completed canonical is durably absent or the canonical name belongs
   to a different queue ID;
2. no unresolved replace/archive intent belongs to the completed transaction;
3. no in-memory old QueueStore/refusal owner survives (startup has none, or the
   live owner has explicitly released it); and
4. the receipt and all marker bindings validate.

The recovery attempt samples a new `released_at` after that proof. It never
reconstructs an earlier release time, uses `completed_at`, or consults events.
Thus a crash before marker durability can only extend retention.

Time arithmetic is UTC elapsed duration: `gc_not_before = released_at + 720h`,
with overflow or non-canonical timestamps rejected. GC requires a trusted,
synchronized host UTC clock. If clock synchronization is unavailable, the
clock is reported regressed, `now < released_at`, or `now <
gc_not_before`, the receipt is ineligible. A backward clock step therefore
only delays deletion; restart does not reset or shorten the marker interval.
An operator must disable GC whenever host UTC cannot satisfy this clock
contract—there is no portable crash-surviving monotonic clock from which to
infer elapsed time.

Eligible GC:

1. load the exact receipt and marker and revalidate schema, filename, identity,
   transaction, digests, timestamp arithmetic, and `now >= gc_not_before`;
2. unlink the exact receipt, treating already-absent as idempotent only for a
   still-valid marker;
3. open/fsync/close the receipt root, reloading after unlink/root-sync
   ambiguity;
4. only after durable receipt absence, unlink the exact marker; and
5. open/fsync/close the receipt root again, reloading after marker
   unlink/root-sync ambiguity.

Receipt-first deletion ensures a crash cannot leave a receipt without its
retention anchor. A marker-only orphan is retained until its own
`gc_not_before`, then the same exact-unlink/root-sync classifier may remove it.
GC never touches canonical queues, never removes a directory, and never blocks
readiness or admission.

Delayed-cleanup conformance case: completion is fixed at `T0`, but repeated
cleanup crashes keep canonical/intent ownership unresolved past `T0 + 30d`.
No release marker exists and GC remains forbidden. Cleanup finally becomes
durable and ownership releases at `T1`; the marker is made durable with
`released_at = T1` and `gc_not_before = T1 + 720h`. Across restart and even if
the clock regresses, GC must refuse through every instant before that marker
boundary and may begin only at or after it.

### 13. QueueStatus selector and receipt fallback

Precedence:

1. nonempty `name` selects by name and ignores `queue_id`;
2. empty name plus nonempty `queue_id` selects exact ID;
3. both absent select `main`.

Exact-ID selection:

1. exact live queue ID wins and returns queue plus exact watched-group
   status/items/counts;
2. otherwise read-only enumerate/index exact
   `<queue_id>--<receipt_id>.json` receipt candidates, excluding
   `.release-v1.json` markers and temps;
3. exactly one unexpired candidate whose filename/content/schema/IDs/digest
   agree may answer;
4. zero is not found;
5. multiple valid, corrupt/unsupported matching-prefix, or any identity
   disagreement is an identity-integrity error;
6. never choose directory-order first match or a newer same-name queue.

Receipt response includes completed status, final status/index/counts/time, and
receipt ID. It proves a watched group only if final index is at least the
watched index.

Status enumeration does not weaken recovery: receipt recovery uses only the
intent-bound basename and bytes.

### 14. Cancellation and archive intent

The request preserves the shipped JSON fields `queue` and `force`. An optional
`queue_id` is an additive extension; `queue` is not renamed to `name`. A
non-empty `queue` alone selects the normalized name, `queue_id` alone selects
exact identity, and both must resolve to the same queue or fail with
`queue_selector_conflict`. Neither present is invalid and never defaults to
`main`. This keeps N-1 `{queue, force}` clients valid while allowing a new CLI
to avoid local ID-to-name authority.

Live cancellation:

```text
preselect origin/kind/source digest/destination/exact successor intent
-> QueueStore cancelled replacement via linked replace intent
-> exact linked archive intent create/write/fsync/close/rename/parent-fsync
-> remove predecessor replace intent + queue-parent fsync
-> canonical-to-selected-archive rename + queue-parent fsync
-> legacy cleanup + .harmonik parent fsync when applicable
-> archive-intent removal + queue-parent fsync
-> QueueStore/refusal release
-> best-effort operator audit
-> RPC success
```

The archive intent binds its predecessor transaction ID, origin, kind, name,
queue ID or corrupt identity evidence, exact source/candidate digest, and
selected non-overwriting archive basename. Both IDs are preallocated before
canonical bytes are built. The predecessor binds the exact successor bytes
and their digest; pair validation cross-checks both IDs and every duplicated
handoff fact. Successor-only recovery validates the complete schema plus exact
source and destination namespace facts rather than accepting schema version
alone.

Handoff classifier:

| State | Decision |
|---|---|
| predecessor replace only | verify exact cancelled candidate/binding; create only the prebound successor |
| exact linked predecessor + successor | verify byte/digest/identity equality; parent-sync successor, then remove predecessor |
| archive successor only | valid only after exact predecessor deletion; continue its selected archive |
| mismatched predecessor/successor | preserve both, quarantine, refuse |
| cancelled canonical with neither origin-bearing record | never guess origin/kind/destination; quarantine |

Successor-intent temp/create/write/fsync/close failure leaves the predecessor.
Successor rename or queue-parent-sync ambiguity reloads the exact pair. The
predecessor deletion itself is queue-parent-synced; ambiguity reloads both and
accepts archive-only only when the exact successor is durable.

Archive rename uses the selected destination and exact source digest.
Definite failure retains source/intent; rename or queue-parent-sync ambiguity
reloads source and destination. A destination exact match permits continuation;
a different destination or changed source quarantines.

For `main`, archive-intent removal is forbidden until legacy absence is
directory-durable through `.harmonik` open/fsync/close. Legacy unlink and
`.harmonik` parent-sync ambiguity reload before retry. Only after archive,
legacy absence, and archive-intent removal are durable may ownership release.

Cancellation creates no completion receipt. Shutdown uses the same linked
handoff with `archive_origin=graceful-shutdown` and no operator audit.
Daemon-unreachable CLI writes nothing.

### 15. Implementation boundaries

The serialized receipt/waiter spine and startup join are:

```text
CQ-02I -> CQ-01 -> CQ-RECEIPT -> CQ-RUN-WAIT
                         |             |
                         +-> CQ-04 ----+-> JR-03
```

`CQ-01` precedes `CQ-RECEIPT` because both edit `internal/queue/rpc.go`.
`CQ-CALLER-CANCEL` also serializes after `CQ-RECEIPT` on that file.
`CQ-04` additionally requires `JR-01`; `JR-03` requires both CQ-04 and
CQ-RUN-WAIT.

- `CQ-02I`: generic transaction/result and replace/archive-intent substrate.
- `CQ-RECEIPT`: receipt/root/status/QM-053 primitives and conditional payload.
- `CQ-RUN-WAIT`: exclusively the `runBeadSubcommandViaDaemon` watcher call
  site, `viaWatchGroupCompletion` signature/body, and a smallest independent
  queue-status RPC seam/helper in `cmd/harmonik/run_via_daemon.go`, with both
  wait branches. It queries once immediately after accepted setup and after
  every heartbeat; it never reuses the subscription connection.
- `CQ-04`: startup recovery after `CQ-RECEIPT` and `JR-01`.
- `JR-03`: terminal call sites only after `CQ-04`, waiter, reservation, and
  activation gates. Live receipt-producing completion cannot land before
  startup can recover every introduced state.

No slice owns a global event writer/replay migration. No card enables
crash-optional group observation before `CQ-RUN-WAIT`.

## Crash-cut design summary

The evidence matrix is normative input to drafting and implementation. Each
namespace mutation has:

- definite pre-mutation failure → `not_committed`;
- successful parent sync → `committed_durable`;
- post-mutation pre-parent-sync ambiguity → `commit_indeterminate`;
- exact reload comparison before retry;
- no event-history decision.

This applies separately to candidate, replace intent, canonical, receipt root,
receipt file, canonical unlink, archive, legacy removal, and receipt GC.

## Rationale

The receipt resolves the QM-003/QM-033 contradiction while preserving
EV-021/EV-022. Binding identity and exact bytes before completed installation
eliminates duplicate-receipt minting after ambiguous cuts. Queue-ID plus digest
CAS prevents old cleanup from deleting a same-name replacement. Exact-ID
status allows command waiters to terminate without making JSONL authoritative.
The event-free intent keeps the transaction substrate bounded to queue state.

## Requirements traceability

| Component | Target sections |
|---|---|
| C1 topology | 1 |
| C2 owner | 2 |
| C3 intents | 4, 14 |
| C4 results | 3, 5 |
| C5 mutations | 5 |
| C6 receipt | 6–9 |
| C7 cleanup/status/GC | 10–13 |
| C8 cancel/migration | 1, 14 |
| C9 task DAG | 15 |

## Explicit non-changes

- Canonical queue envelope schema/status enum.
- Bead or Run terminal ownership.
- Global JSONL/EventID/replay/torn-tail/consumer mechanics.
- Generic durable-state work.
- Multi-process writer model.

## Design readiness

Ready for independent change-design review together with the three other
spec-area designs, changelog, tasks, approved research, and CQ-02 evidence.
