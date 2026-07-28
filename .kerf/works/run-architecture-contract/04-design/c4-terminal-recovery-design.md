# C4 design — Durable terminal and recovery composition

## Current state

Pure run decisions, daemon effects, queue transitions, Beads transitions, Run
records, events, and restart recovery are split. CQ-02 and JR-00 settle the
domain ordering, but no cross-boundary table makes all five ownership
dimensions explicit.

## Target state

RAC will contain a normative transition matrix with columns:

`transition`, `decision owner`, `effect owner`, `serialization owner`,
`recovery owner`, `normative clause`, `event observation edge`.

The exact design rows are:

| Transition | Decision owner | Effect owner | Serialization owner | Recovery owner | Normative source | Event observation edge |
| --- | --- | --- | --- | --- | --- | --- |
| source selection | `WL-03` pure `DispatchCandidate` owner | none | one OuterQueueLoop turn | recompute from immutable snapshot | QM-060/QM-062/QM-064; WL-03 contract | none |
| queued reservation | `runexec.Dispatch` as integrated by CQ-03 | QueueStore transaction owner | normalized queue-name transaction | CQ-02 exact-intent classifier and CQ-04 startup | QM-060/QM-063; `CQ-02.transaction_protocol`, `.result_taxonomy`, `.crash_matrix`; `JR-00.transition_table["eligible queue item -> dispatched stamp"]` | none at reservation; later `run_started` is the dispatch observation, never a reservation authority |
| Run ID patch and Bead claim/activity marker | JR-01 claim/Run phase | QueueStore Run-ID patch, then Beads adapter `ClaimBead` | QueueStore lock/persist, claim semaphore, then `brcli.Adapter.terminalMu` plus BI intent protocol | BI-031 status-check reissue, PL-006 stale-claim reset, JR-01 dispatch-tracker/QM-002a | BI-010d, BI-029, BI-030, BI-031; `JR-00.transition_table["dispatch stamp -> RunID patch -> Bead claimed"]` including `.required_ordering`, `.failure_result`, and `.retry_or_recovery_owner` | no dedicated Bead event; EM-015a/EV §8.1.1 `run_started` occurs only after claim and Run ID |
| claimed Run to memory-only registry registration and goroutine launch | JR-01 claim/Run phase | `runWorkLoop` constructs `RunHandle`, calls `RunRegistry.Register`, then launches the wrapper goroutine | `RunRegistry.mu`; wrapper defer exclusively owns `Unregister` | JR-01 startup dispatch-tracker branch followed by QM-002a; memory registry itself is unrecoverable | EM-014/EM-015a; `JR-00.transition_table["claimed -> in-memory Run registered and goroutine launched"]` including `.run_record_before_after`, `.registry_before_after`, and `.retry_or_recovery_owner` | none at registration; `run_started` is later, after `beadRunOne` worktree provisioning |
| registered Run to conditional independent durable session record | `beadRunOne` independent-session eligibility decision | `internal/run` `runpkg.Write` atomically writes `.harmonik/runs/<RunID>.json`; write failure falls back to shared-session execution | one Run-ID pathname and `runpkg.Write` temp-file/rename boundary; registry remains memory-only | no recovery for a missing record; JR-04 classification begins only after a record exists | `JR-00.transition_table["registered Run -> independent durable session record (conditional)"]` including `.required_ordering`, `.failure_result`, and `.retry_or_recovery_owner` | EM-015a `run_started` precedes the conditional write; the write emits no event |
| session launch/readiness | C3 mode request | PS-02 PhaseLifecycle adapter | one phase scope | PL-005/PL-006/PL-024 plus JR-04 only for authorized adoption | HC-001–HC-004, HC-007, HC-011, HC-039, HC-056; PL-011/PL-012/PL-014/PL-016; `JR-00.transition_table["Run creation/provisioning -> launch"]` | HC-007/HC-011 watcher-owned lifecycle emissions with EV §8.3 schemas; append is observational |
| mode completion classification | mode executor with daemon-authoritative EM class | none (typed result only) | phase scope terminal latch | terminal composer classifies next edge; restart uses durable facts | EM-005/EM-005c, HC-059, RSM-020–RSM-023 | HC-008 `outcome_emitted` when supplied; review/DOT mode events keep their owning EM/EV order |
| merge and Run terminal decision | `runexec.Run`/RSM terminal spine | JR-03 terminal composer invokes merge/gate owner ports | JR-03 terminal spine; merge owner serialization | JR-03 fault matrix and JR-04 for orphaned evidence | RSM-020–RSM-023; EM-015b, EM-052, EM-053; `JR-00.transition_table["active Run -> terminal close or reopen"]` | success follows EM-052 steps 6–8; reopen follows EM-053 `outcome_emitted -> ReopenBead -> run_failed`; EM-015b ordering otherwise |
| terminal Bead transition — **Beads owns terminal Bead transitions** | Run terminal decision / reconciliation verdict as allowed | Beads adapter `CloseBead`/`ReopenBead` | `brcli.Adapter.terminalMu`; BI-029 idempotency and BI-030 intent | BI-031 and JR-03/JR-04 | BI-010, BI-010a, BI-010b, BI-029, BI-030, BI-031; EM-052/EM-053 | no dedicated Bead event (EV §6.5/§8 note); terminal observations ride exact `run_completed`/`run_failed` ordering |
| queue item/group terminal | JR-03 terminal composer decides typed queue outcome | QueueStore transaction owner | normalized queue name | CQ-04 plus CQ-02 classifier | EM-015f; QM-051/QM-052/QM-060/QM-063; `CQ-02.transaction_protocol`, `.event_ordering`; `JR-00.transition_table["terminal Run -> queue item/group/queue release"]` | last `run_completed`/`run_failed` precedes durable commit; then `queue_group_completed`, then successor `queue_group_started` or `queue_paused` per EV §8.10 |
| final receipt/CAS cleanup/release | QueueStore QM-053 owner | QueueStore receipt/CAS primitives | normalized queue name remains owned until release | CQ-04 and exact receipt/intent classifier | QM-003/QM-005/QM-006/QM-053/QM-063; `CQ-02.completion_receipt`, `.crash_matrix`, `.event_ordering` | after receipt durability, exactly one normal-path `queue_group_completed` attempt; restart never retries/synthesizes it |
| normal independent-session record cleanup | normal `beadRunOne` completion, provided daemon context was not canceled | `beadRunOne` defer invokes `internal/run` `runpkg.Remove` for the exact Run ID | one Run-ID pathname; removal occurs before the outer wrapper's memory-registry `Unregister` | an absent record is already clean; other removal failure leaves the record for the next JR-04 startup classification | `JR-00.transition_table["active Run -> terminal close or reopen"].run_record_before_after` and `JR-00.transition_table["terminal Run -> queue item/group/queue release"].run_record_before_after`; JR-03/JR-04 | the Run terminal event is already emitted by the terminal spine; record removal emits and synthesizes no event |
| dead-session durable-record cleanup | JR-04 classifies a durable record whose named session is absent | JR-04 dead-session effect resets the Bead first and invokes `internal/run` `runpkg.Remove` only after reset succeeds; QM-002a later releases the queue item | Run/session identity under `run_session_adoption`; Beads reset intent/adapter, then one Run-ID pathname | retry the same JR-04 classification on the next boot when reset or removal fails | `JR-00.recovery_paths["durable independent run record, dead session"]` exactly; `JR-00.transition_table["daemon cancellation while Run active"].retry_or_recovery_owner`; JR-04 | no adoption or terminal event is synthesized; subsequent QM-002a reconciliation keeps its own observation edge |
| surviving-session adoption and eventual live-record cleanup | JR-04 classifies `durable independent record, session survives`; it must first resolve the recorded conflict by excluding that live record from `reconcileOrphanedRunsOnResume` terminal/reset passes | JR-04 adoption owner monitors the existing session; WL-REC-01 only migrates the caller. On disappearance it reopens the Bead, changes the exact queue item `dispatched -> pending`, clears Run ID, persists/wakes, and only then invokes `internal/run` `runpkg.Remove` | Run/session identity under `workloop_recovery_spine`; one adoption monitor and one Run-ID pathname | repeated JR-04 classification; ambiguity fails closed without reset, redispatch, or record deletion | PL-005/PL-006/PL-024; `JR-00.recovery_paths["durable independent record, session survives"]`; `JR-00.crash_windows["after run_started and independent runpkg.Write while session is alive"]`; JR-04 and WL-REC-01 | no adoption event is synthesized; existing JSONL is corroborative only and cannot authorize adoption, cleanup, or redispatch |

### Composition rule

A `TerminalComposer` owns ordering only. It owns no store and accepts narrow
owner ports. It applies one typed transition at a time, records the last
authoritative fact, and on partial failure returns recovery classification
rather than rolling back a different store.

CQ-02's transaction/result/receipt/CAS ordering is incorporated by the exact
artifact-field citations above without creating a second protocol.
Receipt-aware waiter and capable startup recovery precede live receipt
production. Beads terminal effects remain adapter-owned.

### Events

The matrix has an explicit observation edge. EV-INV-001 is absolute: event
append failure cannot roll back authority; restart cannot replay or synthesize
the missed event; JSONL divergence evidence never becomes the state source.

## Rationale

The matrix exposes split ownership without inventing a cross-store
transaction. It preserves CQ-02 and JR-00 and gives migration tasks exact
handoff points.

## Requirements traceability

All C4 requirements map to the matrix, composition rule, CQ-02 incorporation,
event column, and explicit crash/recovery rows.
