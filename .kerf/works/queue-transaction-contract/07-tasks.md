# Queue transaction contract — executable implementation task plan

CQ-02 is planning-only and does not edit the machine-local Beads ledger or
`TASK-INDEX.yaml`. This artifact defines proposed cards and exact amendments
for the coordinator to materialize after four-spec approval.

## Dependency graph

```text
CQ-02 + CQ-MIG-01 -> CQ-02I
CQ-02I -> CQ-01

CQ-02I -> CQ-CALLER-EAGER
CQ-02I -> CQ-CALLER-OPERATOR
CQ-02I -> CQ-CALLER-BUDGET
CQ-02I -> CQ-CALLER-CREW
CQ-02I + JR-02 -> CQ-CALLER-REVIEW
CQ-02I + CQ-01 -> CQ-CALLER-BOOTSTRAP

CQ-02I + CQ-01 -> CQ-RECEIPT
CQ-RECEIPT -> CQ-RUN-WAIT
CQ-RECEIPT -> CQ-CALLER-CANCEL

CQ-01 + CQ-02I + WL-03 -> CQ-03
CQ-03 + JR-00 + BR-00 -> JR-01
CQ-03 -> CQ-CALLER-GROUP-ACTIVATION

CQ-RECEIPT + JR-01 -> CQ-04

CQ-RUN-WAIT + CQ-04 + JR-01 + JR-02 + BR-03
  + CQ-CALLER-GROUP-ACTIVATION -> JR-03

CQ-03 + JR-03 -> CQ-CALLER-WORKLOOP-MAINTENANCE
CQ-CALLER-BOOTSTRAP + JR-03 -> CQ-CALLER-INLINE-EXIT
CQ-04 + JR-03 -> JR-04
ARCH-GATE + JR-04 + BR-04 + CQ-03 + WL-03 -> WL-REC-01
```

The mandatory receipt/waiter spine and startup-recovery join are:

```text
CQ-02I -> CQ-01 -> CQ-RECEIPT -> CQ-RUN-WAIT
                         |             |
                         +-> CQ-04 ----+-> JR-03
```

`CQ-01`, `CQ-RECEIPT`, and `CQ-CALLER-CANCEL` serialize their
`internal/queue/rpc.go` work through edges. They must not run concurrently.
`CQ-RECEIPT` precedes every caller that may execute QM-053. No card enables
crash-optional group-observation delivery before both `CQ-RUN-WAIT` and
`CQ-04`; the `CQ-04 -> JR-03` edge ensures capable startup recovery exists
before live receipt-producing terminal call sites.

## Routing

| Slice | Implementer | Independent review | Reason |
|---|---|---|---|
| CQ-02I | `gpt-5.6-sol`, xhigh | fresh Sol xhigh | durable transaction API and ambiguity classifiers |
| CQ-01 | `gpt-5.6-terra`, high | Sol xhigh | bounded admission RPC after substrate |
| CQ-RECEIPT | `gpt-5.6-sol`, xhigh | fresh Sol xhigh | receipt identity, namespace cuts, CAS/GC/status |
| CQ-RUN-WAIT | `gpt-5.6-terra`, high | Sol high | bounded waiter state machine after authority exists |
| CANCEL / REVIEW | `gpt-5.6-sol`, high | Sol xhigh | cross-contract RPC/terminal semantics |
| file-disjoint caller adapters | `gpt-5.6-terra`, high | Sol high at integration | mechanical transaction consumption |
| dispatch-spine work | `gpt-5.6-sol`, xhigh, serialized | fresh Sol xhigh | shared workloop ordering |
| fault-table/fixture expansion | local Nemotron/Pi after oracle fixed | Terra/Sol | mechanical matrices only |

Nemotron/Pi must not choose durability dispositions, receipt identity,
recovery promotion, or shared-spine ordering.

## Core card amendments

### CQ-02I — Generic transaction and intent substrate

- **Prerequisites:** CQ-02, CQ-MIG-01.
- **Lease:** `queue_persistence`.
- **Production ownership:** `internal/queue/persistence.go` generic typed
  result/write primitives; proposed `internal/queue/transaction.go`;
  replace/archive-intent schema and classifiers including the optional exact
  linked archive-handoff record; QueueStore snapshot/generation API in
  `internal/queuewiring/store.go`; focused tests.
- **Explicit exclusions:** completion receipt/root/status, QM-053 semantics,
  `completion_receipt_id`, terminal workloop call sites, global event writer.
- **Failing-first proof:** every candidate/intent/canonical rename-parent-sync
  cut returns the exact taxonomy; stale snapshots perform no I/O; recovery
  selects only intent-bound facts.
- **Accepted intermediate state:** generic ordinary replacement/archive
  substrate exists; receipt and optional group-observation delivery remain
  disabled.
- **Rollback:** revert the substrate/API as one unit before caller migrations.

### CQ-01 — Admission transaction

- **Prerequisites:** CQ-02I.
- **Lease:** `queue_rpc`.
- **Production ownership:** `internal/queue/rpc.go`
  `HandleQueueSubmit`, `HandlerAdapter.HandleQueueSubmit`,
  `HandleQueueAppend`, `HandlerAdapter.HandleQueueAppend`,
  `appendUnderLock`, and exact admission tests.
- **Failing-first proof:** submit plus group-zero activation is one candidate;
  append is clone-first; persist failure exposes no live mutation or success
  observation.
- **Accepted intermediate state:** submit/append consume generic transaction;
  receipt/status remains disabled and `internal/queue/rpc.go` lease releases
  before CQ-RECEIPT.
- **Rollback:** admission symbols only.

### CQ-03 — Durable dispatch reservation

- **Profile:** Sol xhigh; fresh Sol xhigh reviewer.
- **Prerequisites:** CQ-01, CQ-02I, WL-03.
- **Lease/conflicts:** `dispatch_spine`; serialized against
  CQ-CALLER-GROUP-ACTIVATION, JR-03, and
  CQ-CALLER-WORKLOOP-MAINTENANCE.
- **Production ownership:** `internal/daemon/workloop.go` dispatch-selection
  reservation region that moves the selected `queue.Item` from pending to
  dispatched and the adjacent Run-ID patch after `startRun`; the exact
  QueueStore locked-snapshot APIs they consume in
  `internal/queuewiring/store.go`; focused reservation/claim-loss/restart
  tests. It must not absorb group activation, terminal outcome, deferred
  reevaluation, max-attempt, or cancellation regions.
- **Named-queue consumption:** consume `WL-03`'s immutable
  `orchestrator.FleetSnapshot`/`orchestrator.Selection`; revalidate global
  `RunRegistry.Len()` capacity and selected-name
  `RunRegistry.LenForQueueLocal`/`Queue.Workers` capacity before reservation.
  `selectNextQueue`, `snapshotFleet`, and QM-067 cursor advance remain `WL-03`
  selection ownership, not reservation ownership. WL-03's failing-first
  source-arbitration tests enable fallback with `--auto-pull`, treat
  `fleet.named_queues IS EMPTY` as the only fallback-eligible queue state, and
  prove that a non-empty fleet whose queues are all paused, completed, full,
  or otherwise ineligible returns idle without consulting `br ready`.
- **Failing-first proof:** reservation candidate is durable before Beads claim
  or handler launch; persist failure leaves live item pending and starts
  nothing; stale generation retries selection; crash after reservation before
  claim converges through `reconcileDispatchedItems`; Run-ID patch failure does
  not expose an unclassified live mutation; retry never double-dispatches.
- **Accepted intermediate state:** the dispatch/Run-ID reservation path uses
  QueueStore transactions while terminal, activation, and maintenance paths
  remain behind their downstream cards.
- **Rollback:** revert only the reservation/Run-ID patch region and its narrow
  store API; no receipt, completion cleanup, or final-event behavior.

### JR-01 — Claim-to-Run ownership

- **Profile:** Sol xhigh; fresh Sol xhigh reviewer.
- **Prerequisites:** CQ-03, JR-00, BR-00.
- **Lease/conflicts:** `dispatch_spine`; CQ-03 precedes and JR-03 follows.
- **Production ownership:** `internal/daemon/workloop.go` region beginning at
  `ClaimBead` and ending after the durable Run record and
  `runRegistry.Register`, the claim/Run phase owner defined by BR-00, and only
  the on-disk Run-record/registry adapters required by that region.
- **Failing-first proof:** claim failure is durably returned or fails closed;
  crashes before/after Run-record creation and registry registration converge
  to exactly one reservation-correlated Run ID before handler launch.
- **Accepted intermediate state:** durable reservation and claim/Run ownership
  agree; terminal composition remains JR-03-owned.
- **Rollback:** claim-through-registration region and narrow adapters only.

### CQ-04 — Pure startup recovery

- **Amended prerequisites:** CQ-RECEIPT and JR-01. CQ-RECEIPT subsumes its
  required CQ-02I/CQ-01 chain; JR-01 makes claim/Run ownership unambiguous.
- **Lease:** `queue_startup_recovery`.
- **Production ownership:** `internal/lifecycle/startup_pl005_qm002.go`
  queue startup phase including `LoadQueueAtStartup`,
  `loadOneQueueAtStartup`, `reconcileQueueTerminalState`,
  `reconcileDispatchedItems`, and `reconcileThreeWay`; exact startup adapter
  calls into QueueStore transaction/receipt primitives; capability and
  recovery tests. `reconcileDispatchedItems` owns QM-002a
  dispatched-vs-Beads-open correction. `reconcileThreeWay` owns all QM-002b
  A/B/C/D classification, its one combined A/D mutation transaction, and
  persist-before-observe order.
- **Failing-first proof:** each supported intent/receipt/canonical combination
  reaches one fixed point; unsupported/corrupt states fail closed; startup
  completes receipt-governed cleanup without emitting final group event;
  dispatched correction failure leaves the prior queue installed nowhere;
  A/D mutations commit together before A/D/C/B observation attempts; restart
  of either reconciliation helper converges without duplicate mutation.
- **Accepted intermediate state:** restart is queue-fact-only; terminal live
  workloop wiring remains JR-03-owned.
- **Rollback:** startup queue-recovery phase only.

### JR-03 — Terminal state and release

- **Amended prerequisites:** CQ-RUN-WAIT, CQ-04, JR-01, JR-02, BR-03, and
  CQ-CALLER-GROUP-ACTIVATION.
- **Lease:** `dispatch_spine`.
- **Production ownership:** `internal/daemon/workloop.go`
  `evaluateGroupAdvanceWithOutcome`, terminal claim-release composition,
  QM-053 call sites, and `drainCancelledQueue`; focused race/fault/restart
  tests.
- **Failing-first proof:** final transition calls receipt primitive once,
  missing observations cannot hang daemon-backed run, claim release never
  precedes durable queue outcome, shutdown never emits operator audit.
- **Accepted intermediate state:** only at JR-03 integration, after both
  authoritative waiter fallback and capable startup recovery have landed, may
  the normal producer adopt crash-optional group-observation delivery.
- **Rollback:** JR-03 terminal composition only; rolling back CQ-04 or
  CQ-RUN-WAIT first requires disabling/rolling back receipt-producing terminal
  call sites.

### JR-04 — Fixed-point recovery

- **Profile:** Sol xhigh; fresh Sol xhigh reviewer.
- **Prerequisites:** CQ-04 and JR-03.
- **Lease/conflicts:** exclusive `run_session_adoption` owner; follows CQ-04
  and JR-03 and precedes WL-REC-01. Workloop caller sites are read-only here.
- **Production ownership:**
  `internal/daemon/runinflightreconcile_hkr73qr.go`
  `reconcileOrphanedRunsOnResume`, `internal/daemon/run_session_adoption.go`
  `adoptLiveRunSession`, and exact focused recovery/adoption tests.
  `internal/lifecycle/startup_pl005_qm002.go` remains CQ-04-owned;
  `evaluateGroupAdvanceWithOutcome` and `drainCancelledQueue` remain
  JR-03-owned.
- **Failing-first proof:** repeated orphan/claim recovery and live-session
  adoption is idempotent; PID/session ambiguity fails closed; adopted and
  retried runs cannot both own one item; queue receipt decisions are consumed
  without JSONL completion inference.
- **Accepted intermediate state:** the typed recovery/adoption owner is ready;
  inline workloop call sites remain until WL-REC-01.
- **Rollback:** only `runinflightreconcile_hkr73qr.go`,
  `run_session_adoption.go`, `adoptLiveRunSession`, their typed boundary, and
  focused tests.

### WL-REC-01 — Route workloop recovery through the adoption owner

- **Profile:** Terra high; Sol xhigh reviewer.
- **Prerequisites:** ARCH-GATE, JR-04, BR-04, CQ-03, and WL-03.
- **Lease/conflicts:** sole `workloop_recovery_spine` writer; JR-04 production
  files are read-only; serialize with every other `workloop.go` card.
- **Production ownership:** recovery/adoption call sites in
  `internal/daemon/workloop.go`, narrow adapter wiring, removal of inline
  adoption policy, and focused mutation/fault/restart tests.
- **Failing-first proof:** repeated loop iterations cannot adopt and retry the
  same Run; ambiguous session/provenance fails closed; call sites apply only
  typed JR-04 effects.
- **Accepted intermediate state:** recovery policy exists only in JR-04 owner;
  `runWorkLoop` retains scheduling/effect application only.
- **Rollback:** workloop recovery/adoption call-site migration and narrow
  adapter wiring only.

## New proposed cards

### CQ-RECEIPT — Add authoritative queue completion receipts

- **Profile:** Sol xhigh; independent Sol xhigh reviewer.
- **Prerequisites:** CQ-02I, CQ-01.
- **Conflicts/serialization:** follows CQ-01 on `internal/queue/rpc.go`;
  precedes CQ-CALLER-CANCEL on the same file; precedes CQ-04 and every QM-053
  caller.
- **Lease family:** `queue_receipt`.
- **Production files/symbols:**
  - proposed `internal/queue/receipt.go` and focused tests: receipt v1,
    completion-release marker v1, canonical encoding, deterministic
    receipt/marker paths and identity validation, root establishment,
    no-replace install/read, CAS cleanup, trusted-clock eligibility, and
    receipt-first marker-last GC;
  - `internal/queue/persistence.go`: receipt/root/cleanup typed namespace
    primitives and replacement-intent final binding integration;
  - `internal/queue/types.go`: receipt-backed QueueStatus response additions;
    preserve `QueueStatusResponse.Queue` and `MaxConcurrent`; add completed
    receipt fields without renaming/removing either;
  - `internal/queue/types.go` `QueueStatusRequest`: preserve `Name` and
    `QueueID`, add optional `WatchedGroupIndex` capable of representing zero;
  - `internal/queue/rpc.go` `HandleQueueStatus`, `findQueueByID`, and
    `HandlerAdapter.HandleQueueStatus`: name-over-ID selector behavior for
    operator status, exact-ID live/receipt selection for the waiter, and
    watched-group validation;
  - `internal/queue/cli/status.go` `RunQueueStatus`: preserve `--queue` and
    `--queue-id`, add `--watched-group-index`, send the canonical request
    rather than a drifted local payload;
  - `internal/queue/cli/client.go` `renderQueueStatusText`: preserve live text
    output and render receipt-backed completion/receipt ID; JSON output must
    round-trip the complete canonical response;
  - focused request/response JSON compatibility, selector-precedence,
    watched-index-zero, live text/JSON, and receipt-completed text/JSON tests;
  - `internal/core/queueevents_extqueue.go`
    `QueueGroupCompletedPayload.CompletionReceiptID` and
    `QueueGroupCompletedPayload.Valid`, plus
    `internal/core/queueevents_extqueue_test.go` UUID/additive compatibility
    tests;
  - `internal/queue/state.go` final-only payload construction boundary only;
  - QM-053 primitive API consumed later by startup and workloop.
- **Objective:** establish exactly one intent-bound receipt as final-success
  authority, provide deterministic exact-ID status, and make cleanup/GC safe
  across restart and same-name reuse.
- **Failing-first proof:**
  - root mkdir/EEXIST/type/queues-parent cuts;
  - temp/no-replace/exact-byte/conflicting-target/root-sync cuts;
  - recovery reuses bound identity and never mints/reconstructs/scan-selects;
  - exact-ID status: live wins; one valid receipt succeeds; zero not found;
    multiple/corrupt/unsupported/disagreeing candidates fail identity integrity;
  - queue-ID plus completed-digest CAS leaves newer same-name queue untouched;
  - unlink/parent-sync/release cuts;
  - release marker is impossible before durable canonical/intent absence and
    ownership release; its temp/write/file-sync/close/no-replace/root-sync
    cuts, exact-existing/corrupt/unsupported classifiers, missing-marker
    restart creation with a later conservative timestamp, and marker-only
    orphan handling;
  - delayed cleanup past `completed_at + 30d`, restart, trusted UTC,
    regression/unsynchronized refusal, exact `released_at + 720h`
    ineligible/eligible boundary, identity revalidation, receipt-first then
    marker-last exact unlink definite failure, already-absent idempotency,
    ambiguity, receipt-root open failure, root fsync failure/ambiguity,
    reload-before-retry, and close-after-successful-fsync diagnostic behavior;
    every case proves GC never blocks admission or uses events;
  - final event append failure remains diagnostic and cleanup proceeds;
  - final-only conditional receipt field.
- **Accepted intermediate state:** receipt/release-marker/root/status/QM-053
  primitives and conditional payload exist, but no terminal caller enables
  crash-optional observation delivery; global event mechanics untouched.
- **Rollback:** receipt/status/QM-053/root/release-marker/retention/GC changes
  are one reviewed unit. Do not retain a receipt producer or partial GC without
  release-marker capability and complete receipt-first/marker-last
  unlink/root-open/root-sync/close ambiguity handling; before
  CQ-RUN-WAIT/JR-03 the whole unit may be reverted.

### CQ-RUN-WAIT — Make daemon-backed run wait on authoritative queue state

- **Profile:** Terra high; Sol high reviewer.
- **Prerequisite:** CQ-RECEIPT.
- **Lease family:** `run_via_daemon_waiter`.
- **Exclusive production ownership:** `cmd/harmonik/run_via_daemon.go`
  `runBeadSubcommandViaDaemon` watcher call site,
  `viaWatchGroupCompletion` signature/body, and the smallest proposed
  `viaQueueStatusByID` helper or injected `viaStatusQuery` interface backed by
  `viaSendRequest` over a separate RPC connection, plus focused tests. It does
  not own `viaSubmitOrAppend`/`viaAppendToActiveQueue` mutation, the
  QueueStatus server, subscription transport, or event bus.
- **Behavior:**
  - the call site supplies `harmonikDir` or an injected status-query closure;
    the watcher never reuses the subscription connection for RPC;
  - immediately after accepted submit/append plus subscription setup, query
    QueueStatus by accepted queue ID and exact watched group;
  - after every existing heartbeat, issue the same query over the independent
    RPC helper;
  - pending/active continues;
  - live non-final `complete-success` terminates watched group even with absent
    observation and later live groups;
  - final receipt proves success when its final index covers watched index;
  - failed group, paused, or cancelled exits 1;
  - complete own-bead observations preserve current attribution; incomplete
    observations use durable group/receipt result;
  - missing group, bad receipt index, zero/multiple/corrupt/unsupported/
    inconsistent receipt, or transport failure exits 1;
  - newer same-name queue and JSONL are never fallback.
- **Failing-first proof:** both fresh-submit and shared-append paths issue one
  immediate post-accept/setup query and one query after every heartbeat;
  healthy pending/active continues and any status transport failure terminates.
  Then prove healthy heartbeat/status with
  `queue_group_completed` absent for non-final success, final receipt-only
  success, paused-by-failure with group/pause observations absent, complete and
  incomplete own-bead sets, same-name reuse before each query, integrity
  errors, and transport failure.
- **Accepted intermediate state:** both wait branches are safe; optional
  group-observation delivery remains disabled until JR-03 integration.
- **Rollback:** watcher call site, signature/body, status-query seam/helper,
  and focused tests only.

### CQ-CALLER-CANCEL — Migrate live and CLI cancellation

- **Profile:** Sol high; Sol xhigh reviewer.
- **Prerequisite:** CQ-RECEIPT, which serializes after CQ-01.
- **Conflict:** same `internal/queue/rpc.go` file as CQ-RECEIPT; edge is
  mandatory.
- **Lease family:** `queue_cancel_rpc`.
- **Production ownership:** `internal/queue/rpc.go`
  `HandlerAdapter.HandleQueueCancel` and canonical request decode;
  `internal/queue/types.go` `QueueCancelRequest`/`QueueCancelResponse`,
  preserving literal shipped JSON `queue` and `force` while additively adding
  `queue_id`; both selectors must agree and neither-present is invalid;
  `internal/queue/cli/cancel.go`
  `RunQueueCancel`, `tryDaemonQueueCancel`, `cancelFindByID`, `journalCancel`,
  and `emitQueueCancelEvent`, including removal of local archive/event
  fallback and N-1 `{queue,force}` compatibility tests;
  `internal/core/eventtype.go` `EventTypeQueueCancelledOperator`;
  `internal/core/queueevents_extqueue.go`
  `QueueCancelledOperatorPayload` and its XOR `Valid`;
  `internal/core/eventreg_hqwn59.go` `registerQueueEvents`; per-type
  compatibility row in `internal/core/pertypecompat_hqwn38.go`;
  `internal/core/queueevents_extqueue_test.go`,
  `internal/core/pertypecompat_hqwn38_test.go`, and
  `internal/core/eventtype_coverage_gjyks_test.go` constructor/decode,
  cohort/count, Class-O, and compatibility coverage.
- **Objective:** prebind origin/kind/source digest/destination/exact successor
  intent in cancelled replace intent → exact linked archive intent durability
  → predecessor removal → archive/legacy durability → successor cleanup →
  release → best-effort audit → RPC success.
- **Failing-first proof:** daemon-down exit 17 with byte-identical filesystem
  and event state; replace-only, exact linked-pair, archive-only, mismatched
  pair, and originless-cancelled classifiers; successor-intent
  create/write/fsync/close/rename/queue-parent-sync; predecessor deletion and
  parent-sync; archive rename and queue-parent-sync; main legacy unlink and
  `.harmonik` parent-sync; successor removal and queue-parent-sync; corrupt XOR
  audit; registered decode/compatibility/cohort coverage; audit append failure
  preserves success; restart never replays audit/response.
- **Accepted intermediate state:** operator cancellation is single-owner;
  shutdown audit behavior remains JR-03-owned.
- **Rollback:** linked cancellation handoff, cancel CLI/RPC, and complete typed
  audit registration are one slice; do not retain a cancelled replacement
  producer without its exact successor-intent recovery.

### CQ-CALLER-GROUP-ACTIVATION — Remove standalone normal activation

- **Profile:** Sol xhigh; Sol xhigh reviewer.
- **Prerequisite:** CQ-03.
- **Lease:** dispatch spine, serialized before JR-03.
- **Production ownership:** `internal/daemon/workloop.go`
  `activateFirstPendingGroup`, `activateFirstPendingGroupLocked`, callers, and
  focused tests.
- **Failing-first proof:** submit and advance persist once; no normal active
  queue has every group pending; legacy recovery activation requires explicit
  classified state/current generation.
- **Accepted intermediate state:** normal standalone activation removed;
  terminal work remains untouched.
- **Rollback:** two activation helpers/callers only.

### CQ-CALLER-WORKLOOP-MAINTENANCE — Migrate residual maintenance mutations

- **Profile:** Sol xhigh; fresh Sol xhigh reviewer.
- **Prerequisites:** CQ-03, JR-03.
- **Lease:** dispatch spine after JR-03.
- **Production ownership:** only the deferred reevaluation, claim-skipped
  dependency, cross-queue duplicate, and max-attempt direct-persist regions in
  `internal/daemon/workloop.go`, plus focused tests.
- **Failing-first proof:** each persist failure leaves installed state
  unchanged and emits no success fact; retry converges; no reservation,
  terminal, activation, or adoption semantics reopen.
- **Accepted intermediate state:** residual maintenance consumes transaction
  API.
- **Rollback:** four named branches only.

### CQ-CALLER-INLINE-EXIT — Migrate post-daemon inline exit archive

- **Profile:** Terra high; Sol xhigh reviewer.
- **Prerequisites:** CQ-CALLER-BOOTSTRAP, JR-03.
- **Lease:** `queue_inline_exit`.
- **Production ownership:** `cmd/harmonik/run.go` `runBeadSubcommandIO`
  post-daemon paused-by-failure archive/exit region and focused tests.
- **Failing-first proof:** CLI never archives directly; failure retains owner/
  refusal and truthful exit; durable archive precedes reported success.
- **Accepted intermediate state:** inline run has no post-daemon direct writer.
- **Rollback:** post-daemon exit region only.

## Existing file-disjoint caller amendments

- **CQ-CALLER-EAGER:** prerequisite CQ-02I; lease `queue_eager`; owns
  `internal/daemon/eagerfill_em063.go` `eagerRefillEval`, its call to
  `snapshotFleet`, the complete-fleet EM-063 duplicate pre-screen, deterministic
  normalized-name refill target, and queue mutation around its direct
  `queue.Persist`. Failing-first proves every named queue is scanned, sibling
  duplicates are rejected, paused/full siblings do not block an eligible
  stream, both capacity gates bound deficit, and persistence failure installs
  no refill or success observation. Accepted intermediate is transaction-backed
  named eager refill; rollback is that file-disjoint region.
- **CQ-CALLER-OPERATOR:** prerequisite CQ-02I; lease
  `queue_operator_events`; owns
  `internal/queuewiring/operatorevents.go`
  `QueueOperatorEventConsumer.transitionToPausedByDrain` and
  `QueueOperatorEventConsumer.transitionToActive`. It is file-disjoint from
  `workloop.go`, `drainCancelledQueue`, and queue-cancel. Failing-first proves
  pause/resume candidate failure leaves live state unchanged; intermediate
  retains old terminal paths; rollback is only pause/resume; conflicts with
  no other planned slice.
- **CQ-CALLER-BUDGET:** prerequisite CQ-02I; lease `queue_budget`; owns
  `internal/daemon/perqueuespendmeter_tigaf11.go`
  `pauseQueueByBudget`/`unpauseBudgetPausedQueues`. Failing-first proves batch
  rollback and retry; intermediate retains other callers; rollback is those
  two symbols; file-disjoint.
- **CQ-CALLER-CREW:** prerequisite CQ-02I; lease `queue_crew`; owns
  `internal/daemon/crewstart.go` `crewHandlerImpl.ensureQueue` minimal queue
  placeholder direct-persist
  region. Failing-first proves no placeholder installation on failure;
  rollback is that region; file-disjoint.
- **CQ-CALLER-REVIEW:** prerequisites CQ-02I and JR-02; lease
  `queue_review_charge`; owns `internal/daemon/runports.go`
  `ChargeReviewLoopFailure` and its narrow port. Failing-first proves charge
  failure installs no live budget mutation; rollback is the port/region;
  file-disjoint.
- **CQ-CALLER-BOOTSTRAP:** prerequisites CQ-02I and CQ-01; lease
  `queue_bootstrap`; owns `cmd/harmonik/run.go` initial `queue.Persist`,
  pre-daemon `ArchiveFailedQueue` paused/cancelled/proven-orphan regions, and
  QueueStore adoption, and must route them behind the started/adopted owner.
  Failing-first proves no
  direct writer remains and adoption happens only after durable classification;
  accepted intermediate leaves post-daemon exit to CQ-CALLER-INLINE-EXIT;
  rollback is bootstrap/adoption only; conflicts with that downstream card.

Each uses clone/validate/transaction, proves persist failure leaves memory
unchanged, and owns no receipt or event-delivery behavior.

## Scenario task — receipt-governed completion

```yaml
title: "scenario: queue receipt survives missing final observation"
labels: [scenario-test, codename:queue-transaction-contract]
dependencies: [CQ-RECEIPT, CQ-RUN-WAIT, CQ-04, JR-03]
faults:
  - crash after completed canonical durability before receipt durability
  - suppress queue_group_completed append after receipt durability
  - crash after canonical unlink before parent sync
  - delay cleanup beyond completed_at plus 30 days
  - crash after durable cleanup and ownership release before release marker
  - restart and install a conservative later release marker
  - regress or mark host UTC unsynchronized before marker boundary
  - crash between receipt GC/root sync and marker GC/root sync
  - same-name resubmit before old status/GC query
acceptance:
  - exactly one authoritative intent-bound receipt
  - event may be absent
  - daemon-backed run exits from exact-ID live/receipt status
  - newer same-name queue is untouched
  - restart emits no final event
  - no GC is possible without a valid bound release marker
  - marker released_at follows durable cleanup and ownership release
  - GC refuses until trusted UTC reaches released_at plus 720 hours
  - receipt is durably absent before its marker may be removed
```

## Exploratory task — cancellation and capability

```yaml
title: "explore: queue cancel and capability fault surface"
labels: [exploratory-test, codename:queue-transaction-contract]
dependencies: [CQ-CALLER-CANCEL, CQ-04]
acceptance:
  - daemon-down exit 17 and zero writes
  - archive intent converges across every parent-sync cut
  - incapable downgrade preserves records and refuses
  - operator audit failure does not change cancellation success
```

## Forbidden tasks

No proposed task may be named `CQ-EFFECT`, `CQ-EFFECT-DAEMON`, or
`CQ-EFFECT-READERS`, or own effect keys, segmented event storage,
ReplayCursorV2, `ScanAfter` migration, global reader migration, full-log
lookup, truncation, repair CLI, event lock, or writer census.
