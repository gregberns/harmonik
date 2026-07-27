# Queue transaction contract decomposition

## Mode

Four-spec change design. Queue-owned state is authoritative; event delivery is
an explicitly separate observation component.

## Affected normative specifications

### `specs/queue-model.md`

#### C1 — Topology and protocol records

- Canonical live queues are
  `.harmonik/queues/<normalized-name>.json`.
- `.harmonik/queue.json` is a `main` migration input only.
- Sibling `<name>.replace-intent` records ordinary replacement comparison
  evidence.
- Sibling `<name>.archive-intent` records exact archive/migration handoff.
- `.harmonik/queues/.completion-receipts/` is a flat capability-owned root.
- Canonical queue envelopes remain schema version 1; protocol records have
  independent, capability-gated versions.

Dependencies: queue-model QM-001/QM-002/QM-002c and PL startup inventory.

#### C2 — QueueStore transaction owner

- QueueStore owns a per-normalized-name transaction domain.
- Stored state is immutable input; mutation uses detached snapshots and private
  candidates.
- A volatile process-local generation rejects stale candidates before I/O.
- Multi-name callers acquire names lexically and receive independent results.
- Direct persistence remains legal only inside the QueueStore transaction
  implementation or its bounded same-process startup adapter.

Dependencies: `internal/queuewiring/store.go` ownership surface and all CQ-00
writer inventory.

#### C3 — Replacement and archive intents

- Candidate bytes and temp are fixed and durable before replace-intent
  creation.
- The replace intent binds transaction/operation/name/queue identity,
  prior/candidate digests, exact temp basename, and Wake requirement.
- It is comparison evidence, not an event outbox.
- Archive intents bind exact source bytes/digest and selected non-overwriting
  archive destination before rename or legacy removal.
- Recovery uses exact intent/canonical/temp/archive facts and never JSONL.

Dependencies: C1/C2. Queue-model owns schema and classifiers; process-lifecycle
owns capability refusal.

#### C4 — Mutation result and visibility

- `rejected`: validation, conflict, or stale generation before namespace I/O.
- `not_committed`: definite failure before namespace mutation or a classifier
  proving prior state.
- `committed_durable`: intended namespace state parent-synced.
- `commit_indeterminate`: namespace mutation occurred without established
  parent durability; reload/compare/sync is mandatory.
- Install/generation advance occurs only for durable selected state.
- Wake follows durable install and may repeat. Ordinary events are attempted
  only on their specified normal paths and never determine transaction state.

Dependencies: C2/C3.

#### C5 — Admission, activation, and normal mutations

- Submit and first-group activation are one candidate, one replacement, one
  install.
- Append, reservation, Run-ID patch, advance, pause/resume, eager refill,
  budget/review charge, maintenance, startup, inline/bootstrap,
  crew-placeholder creation, and cancelled-state replacement all consume C2–C4.
- An exact-byte unchanged candidate collapses before temp/intent creation and
  emits no Wake or success event.

Dependencies: C2–C4; implementation callers remain separately leased.

#### C6 — Queue-owned completion receipt

- Final `complete-success` preallocates one UUIDv7 receipt ID.
- Before completed canonical install, the replace intent binds the exact flat
  basename, schema version, canonical receipt bytes, and digest.
- Receipt v1 binds transaction ID, name, final group facts, completion time,
  and the exact completed canonical digest.
- Recovery writes only the bound bytes to the bound basename.
- The receipt becomes authoritative only after its file and root directory are
  durable.

Dependencies: C1/C3/C4. This component owns no event delivery state.

#### C7 — QM-053 CAS cleanup, retention, and status

- Receipt durability precedes canonical unlink.
- Cleanup unlinks only an exact queue-ID plus completed-digest match.
- Canonical absence is idempotent; same-name reuse is never touched.
- Ownership releases only after canonical absence is directory-durable or
  exactly reconciled.
- Receipt retention lasts through cleanup/release and at least 30 days.
- GC uses unlink plus receipt-root fsync, resolves ambiguous cuts by reload,
  and never blocks admission.
- QueueStatus exact-ID lookup checks live exact ID first, then requires exactly
  one unexpired receipt whose filename/content/digest identities agree. Zero
  is not found; multiple, corrupt, unsupported, or disagreeing candidates are
  identity-integrity errors, never directory-order first-match success. Name
  lookup and default-main behavior remain distinct.

Dependencies: C6. Implementation owner: `CQ-RECEIPT`.

#### C8 — Cancellation, archive, and migration

- Live operator cancellation uses the same replacement primitive for the
  cancelled candidate, then an archive intent and exact archive rename.
- Graceful shutdown follows the durable cancellation/archive policy but emits
  no operator audit.
- Daemon-down cancellation writes nothing.
- Legacy migration classifies legacy-only, canonical-only, equivalent-both,
  divergent-both, invalid, neither, and every directory-sync cut.
- Cancellation never creates a completion receipt.

Dependencies: C1–C4 and process-lifecycle transport.

#### C9 — Conformance and task DAG

- Fault tests cover every intent/temp/canonical/receipt/root/unlink/archive/
  migration namespace cut.
- Same-name-reuse tests prove old cleanup/status cannot select a new queue.
- The required completion spine is:

```text
CQ-02I -> CQ-01 -> CQ-RECEIPT -> CQ-RUN-WAIT -> JR-03
```

- `CQ-01` precedes `CQ-RECEIPT` because both edit
  `internal/queue/rpc.go`; the shared file requires explicit serialization.
- `CQ-RECEIPT -> CQ-04 -> JR-03` is also mandatory; startup recovery and the
  waiter join before live terminal composition.
- `CQ-RECEIPT` precedes any caller executing QM-053.
- CQ-MIG and non-completion callers retain CQ-02I prerequisites.
- There is no `CQ-EFFECT`, `CQ-EFFECT-DAEMON`, or `CQ-EFFECT-READERS`.

### `specs/process-lifecycle.md`

#### PL1 — Wire and cancel inventory

- Inventory queue submit `name`/`workers`, append `name`, QueueStatus selector,
  and queue-cancel method/payload.
- Daemon-unreachable cancellation returns exit 17 and performs no queue,
  temp, archive, intent, legacy, receipt, or event write.

#### PL2 — Startup receipt-root and recovery phase

- Before queue readiness/recovery, create or validate the flat receipt root.
- First create is durable only after `.harmonik/queues` is opened, synced, and
  closed.
- Resolve supported replacement/archive intents and receipts before loading
  queues or admitting RPCs.
- Unknown/corrupt schemas or incomplete capability fail startup closed with
  operator-visible recovery state.

#### PL3 — Upgrade, downgrade, rollback, and shutdown

- Target binaries must advertise exact replace/archive-intent and
  completion-receipt versions plus CAS cleanup and retention/GC behavior.
- A target lacking any required capability refuses upgrade/exec/downgrade
  before queue recovery.
- Capability support applies only inside the daemon/startup owner and never
  authorizes local fallback writes.
- Shutdown drain placement and exit semantics remain lifecycle-owned.

### `specs/event-model.md`

#### EV1 — Derived `queue_group_completed`

- Add optional `completion_receipt_id`.
- It is present exactly on final `complete-success` after receipt durability.
- It is absent on non-final success and every `complete-with-failures`.
- The producer attempts the event once on the normal path.
- Crash/append failure may leave it absent; restart never synthesizes or
  retries it.
- EV-021/EV-022, EventID, `ScanAfter`, JSONL, torn-tail, writer, and consumer
  contracts remain unchanged.

#### EV2 — Typed `queue_cancelled_operator`

- Class O, queue source, parseable/corrupt payload XOR.
- Includes exact queue/archive identity and successful operator-cancel facts.
- Attempted after durable archive/legacy cleanup, archive-intent removal, and
  QueueStore/refusal release, before successful RPC response.
- Best-effort, non-replayed, no completion receipt, absent on graceful
  shutdown, and excluded from all intents.

### `specs/execution-model.md`

#### EM1 — EM-015f clarification only

- “MUST emit” means a normal-path producer obligation after authoritative
  queue-state commit.
- It is not a crash-surviving delivery guarantee or reconstruction landmark.
- When the final attempt occurs, the last item terminal event still precedes
  `queue_group_completed`.
- No other Run, item, workflow, or execution behavior changes.

## Read-only specifications

- `specs/beads-integration.md`: terminal Bead state and ledger ownership.
- `specs/workspace-model.md`: workspace contracts.
- `.kerf/works/durable-state-contract/**`: separate generic state work that
  explicitly does not absorb queue persistence.

## Dependency graph

```text
C1 topology
 ├── C2 QueueStore owner
 ├── PL2 startup inventory
 └── C6 receipt-root path

C2 + C3 intents -> C4 result classifier -> C5 ordinary mutations
C3 + C4 -> C6 receipt -> C7 cleanup/status
C3 + C4 -> C8 cancel/archive/migration
C6 -> EV1 receipt correlation
C7 -> CQ-RUN-WAIT exact-ID fallback
PL2 + PL3 gate C3/C6/C7/C8 recovery
EM1 constrains EV1 normal-path semantics
```

## Goal coverage

| Goal | Components |
|---|---|
| topology and one writer | C1, C2 |
| transaction and restart classification | C3, C4 |
| normal mutation migration | C5 |
| authoritative completion | C6, C7 |
| cancellation and migration | C8, EV2, PL1 |
| startup/upgrade safety | PL2, PL3 |
| non-authoritative observations | EV1, EM1 |
| dispatchable implementation | C9 |

## Decomposition review questions

1. Can recovery choose every action from queue-owned bytes without JSONL?
2. Is receipt identity fixed before the first ambiguous completion cut?
3. Is every newly created directory entry paired with the correct parent sync?
4. Can old cleanup or status ever select a newer same-name queue?
5. Can either submit or append wait forever after an omitted observation?
6. Does any active text reintroduce event delivery state or global replay work?
7. Is the execution-model delta confined to EM-015f?
