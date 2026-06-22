# Event Model Spec-vs-Code Conformance Audit

```yaml
spec_version: 0.6.4
audit_date: 2026-06-22
bead_id: hk-pqgtm
auditor: implementer-agent (run 019eef4f)
scope: specs/event-model.md vs internal/core/ + internal/eventbus/ + internal/handlercontract/coreexports.go
```

---

## 1. Method

- Read `specs/event-model.md` version 0.6.4 (last-updated 2026-06-21) completely.
- Read all referenced implementation files under `internal/core/`, `internal/eventbus/`, and `internal/handlercontract/coreexports.go`.
- Cross-checked every normative section (§4 requirements, §5 invariants, §6 schemas, §8 taxonomy).
- Explicitly assessed the v0.3.3 → v0.6.4 delta as the highest-value gap surface.

No production code was modified. This report is the sole deliverable.

---

## 2. Per-section conformance table

### §4.1 Envelope — EV-001 through EV-005

| Req | Description | Code location | Status | Note |
|-----|-------------|---------------|--------|------|
| EV-001 | Every event carries common envelope fields | `internal/core/event_payload.go`; `internal/eventbus/busimpl.go:Emit` | CONFORMS | Envelope struct in busimpl builds all required fields per EV-001 |
| EV-002 | `event_id` MUST be a UUIDv7 | `internal/core/eventid.go:IsUUIDv7()` | CONFORMS | IsUUIDv7 checks version nibble per RFC 9562 |
| EV-002a | UUIDv7 generation MUST be monotonic within process | `internal/core/eventidgen.go` | CONFORMS | Mutex-guarded monotonic counter via RFC 9562 §6.2 method 1 |
| EV-002b | event_id generation routes through daemon's monotonic generator | `internal/handlercontract/coreexports.go`; busimpl stamps event_id | CONFORMS | Handler sends envelopes without event_id; daemon watcher stamps |
| EV-002c | UUIDv7 HWM persisted to `.harmonik/event_id_hwm` across restarts | No matching code found | **GAP** | Planned in hk-hqwn.5 per `internal/core/eventidgen.go` comment; no HWM file read/write or `daemon_degraded{reason=clock_regression}` emission in reviewed code |
| EV-003 | `timestamp_mono_nsec` is process-scoped, not cross-process-comparable | busimpl.go fills field | CONFORMS | Field populated from runtime mono clock |
| EV-004 | `source_subsystem` is layout-open (no fixed enum) | `internal/core/subsystemregistry_hqwn44.go` | CONFORMS | `RegisterSourceSubsystem`/`ErrDuplicateSourceSubsystem` confirm EV-034a/EV-004 |
| EV-005 | Events are lifecycle-boundary signals, not agent internals | Code discipline | CONFORMS | No tool-call or per-token events on bus |

### §4.2 Clock and ordering — EV-006 through EV-008

| Req | Code location | Status | Note |
|-----|---------------|--------|------|
| EV-006 | Wall clock advisory for cross-process ordering | N/A (consumer obligation) | CONFORMS | Envelope advises UUIDv7 for ordering |
| EV-007 | Monotonic time non-decreasing within process | eventidgen.go mutex-guarded | CONFORMS | |
| EV-008 | Partial-order contract (UUIDv7 ms-resolution + EV-002a) | eventidgen.go + envelope | CONFORMS | |

### §4.3 Bus and consumer taxonomy — EV-009 through EV-014d

| Req | Description | Code location | Status | Note |
|-----|-------------|---------------|--------|------|
| EV-009 | Subscription declared at registration; post-seal fails | `busimpl.go:Subscribe + Seal` | CONFORMS | Post-seal Subscribe returns typed error |
| EV-010 | Synchronous consumer: at-most-one per type; acyclicity check | `busimpl.go:checkSyncCardinality + checkSyncAcyclicity` | CONFORMS | `ErrDuplicateSynchronousConsumer` and `ErrSynchronousConsumerCycle` typed errors |
| EV-011 | Asynchronous consumer: retry + dead-letter; bounded queue | busimpl.go | CONFORMS | 3 retries with exponential backoff; dead-letter on exhaustion |
| EV-011a | Non-blocking back-pressure; shed by class; spill for F-class | busimpl.go | CONFORMS | `bus_overflow` emission; spill-file path per class |
| EV-012 | Observer: passive; failures MUST NOT produce bus events | busimpl.go `FANOUT_OBSERVERS` | CONFORMS | Per-observer goroutine + bounded queue |
| EV-013 | Default consumer class is observer | Subscription.ConsumerClass | CONFORMS | Default zero-value is observer per consumerclass.go |
| EV-014 | Two synchronous consumers for same type → startup error | busimpl.go:checkSyncCardinality | CONFORMS | |
| EV-014a | Emit order: redact → JSONL+fsync → sync dispatch → async/observer off-path | busimpl.go:Emit | CONFORMS | |
| EV-014b | Consumers MUST be idempotent on recovery/dead-letter replay | Consumer obligation | N-A | Contract obligation; cannot verify statically |
| EV-014c | Observer dispatch: per-observer goroutine + bounded queue | busimpl.go | CONFORMS | `FANOUT_OBSERVERS` uses per-observer goroutine |
| EV-014d | Consumer-recovery replay contract (startup replay before live stream) | `busimpl.go:ReplayFrom` | **GAP** | `ReplayFrom` is a stub returning `nil` (line 1085–1087); JSONL replay wiring deferred; `on_tail_truncation` callback declared in `eventbus.go:TailTruncationCallback` but stub body means no actual replay; EV-INV-002 consumer side not closed |

### §4.4 Durability classes and fsync semantics — EV-015 through EV-020

| Req | Description | Code location | Status | Note |
|-----|-------------|---------------|--------|------|
| EV-015 | JSONL persists every emitted event | `internal/eventbus/jsonlwriter.go` | CONFORMS | Append-only with buffered drainer |
| EV-016 | Every §8 row declares durability class; fsync derives from class | `busimpl.go:fsyncBoundaryEventTypes + isFsyncBoundaryEvent` | **GAP** | Map is incomplete — see §3 "Gaps" for the full list of F-class types absent from the map; events not in the map silently downgrade to O-class fsync behavior |
| EV-016a | Per-event fsync; no multi-event atomicity guarantee | busimpl.go | CONFORMS | Documented and implemented per event |
| EV-017 | Event loss between fsyncs acceptable; git authoritative | N/A (design invariant) | CONFORMS | |
| EV-018 | Producers emit events idempotently | busimpl.go; producer discipline | CONFORMS | Payload construction is idempotent |
| EV-019 | Panic handler MUST flush structured logs | N/A — process-level handler | N-A | Cannot verify in these files |
| EV-019a | Panic handler SHOULD best-effort flush bus | N/A | N-A | |
| EV-020 | JSONL writes are append-only | `jsonlwriter.go:O_CREATE\|O_WRONLY\|O_APPEND` | CONFORMS | No rewrite or truncation |

### §4.5 Replay semantics — EV-021 through EV-024

| Req | Description | Status | Note |
|-----|-------------|--------|------|
| EV-021 | Observational replay MUST NOT reconstruct state | CONFORMS | Design invariant upheld |
| EV-022 | State reconstruction MUST NOT walk JSONL | CONFORMS | Daemon walks git + Beads on startup |
| EV-023 | Divergence-evidence read with post-crash-window guardrail | N-A | Reconciliation subsystem; not in reviewed files |
| EV-023a | Inconclusive classification for non-corroborable events | N-A | Reconciliation subsystem |
| EV-024 | Replay cannot re-establish agent state or re-invoke LLMs | N-A | Design invariant |

### §4.6 Producer/consumer contract — EV-025 through EV-027

| Req | Description | Status | Note |
|-----|-------------|--------|------|
| EV-025 | Each event type has exactly one owning spec for payload shape | CONFORMS | §6.3 is normative for shape; emitter specs own WHEN |
| EV-026 | Subsystems MAY emit internal events not on §8 list; MUST NOT cross bus | **GAP** | Multiple event types defined in `eventtype.go` are not in §8 (see §3 Gaps, non-§8 entries) and are emitted to the cross-bus; EV-026 says internal events MUST NOT cross the bus |
| EV-027 | Adding cross-bus event type requires foundation amendment | **GAP** | Consequence of EV-026 gap; non-§8 events on bus lack amendment artifacts |

### §4.7 Schema versioning — EV-028 through EV-030

| Req | Description | Status | Note |
|-----|-------------|--------|------|
| EV-028 | `schema_version` on envelope and per-type payload | CONFORMS | Envelope carries schema_version; registry per EV-032 |
| EV-029 | N-1 readable compatibility window | CONFORMS | §6.4 additive-field rule applied in all reviewed payloads |
| EV-030 | Breaking-change classification in §6.4 | CONFORMS | §6.4 table present in spec; no observed breaking changes in code |

### §4.8 Tagging obligations — EV-031

| Req | Description | Status | Note |
|-----|-------------|--------|------|
| EV-031 | Every §8 entry carries four-axis tags + durability class | CONFORMS | Spec declares tags per section; Go registry per EV-032; all reviewed rows carry tags |

### §4.9 Go representation — EV-032 through EV-034a

| Req | Description | Code location | Status | Note |
|-----|-------------|---------------|--------|------|
| EV-032 | Tagged-union envelope + payload-constructor registry | `internal/core/eventreg_hqwn59.go` + busimpl | CONFORMS | Registry of `func() EventPayload` keyed by EventType |
| EV-033 | Type dispatch deterministic on `type` field; unknown → ErrUnknownEventType | busimpl + registry | CONFORMS | Registry miss surfaces typed error |
| EV-034 | Registry registration at startup via init() or RegisterEventType | eventreg_hqwn59.go | CONFORMS | |
| EV-034a | `source_subsystem` identifiers registered at startup; duplicates fail | `internal/core/subsystemregistry_hqwn44.go` | CONFORMS | `ErrDuplicateSourceSubsystem` typed error |

### §4.10 Redaction — EV-035 through EV-036

| Req | Description | Code location | Status | Note |
|-----|-------------|---------------|--------|------|
| EV-035 | Redaction registry applied before emission | busimpl.go:Emit (HC-031 + HC-032 via RedactionRegistry) | CONFORMS | Applied before JSONL append and dispatch |
| EV-036 | Compile-time/startup check: no payload field name matches secret-prefix rule | `internal/core/ev036_global_registry_hk6x7dw_test.go` + oninv003 test | CONFORMS | Test validates registry at startup; production code avoids flagged field names (e.g. `keeperevents.go:307`) |

### §4.11 `harmonik subscribe` consumer contract — EV-037 through EV-041

| Req | Description | Status | Note |
|-----|-------------|--------|------|
| EV-037 | External consumers MUST persist watermark of `last_processed_event_id` | N-A | Consumer obligation; not in reviewed files |
| EV-037a | Watermark MUST NOT regress | N-A | Consumer obligation |
| EV-038 | `subscription_gap` → forced re-sync | N-A | Consumer obligation |
| EV-039 | Heartbeat carries `last_event_id` + `active_runs`; consumers MUST use both | N-A | Consumer obligation + server implementation not in reviewed files |
| EV-040 | Missing heartbeats = daemon liveness failure; reconnect with backoff | N-A | Consumer obligation |
| EV-041 | Git-done-but-no-terminal-event heuristic | N-A | Consumer obligation |

### §4.12 `decision_required` dispatch-blocking — EV-042 through EV-044

| Req | Description | Status | Note |
|-----|-------------|--------|------|
| EV-042 | MUST emit `decision_required` on 4 canonical conditions | **GAP** | No `EventTypeDecisionRequired` constant in `eventtype.go`; events use string literals in digest test code; no typed EventType constant means no type-safe emission path |
| EV-043 | Unacknowledged `decision_required` blocks dispatch for subject | **GAP** | Dependent on EV-042; unblocking requires typed event |
| EV-043a | Startup MUST restore `decision_required` blocking state | **GAP** | Dependent on EV-042 |
| EV-044 | Digest MUST surface every unacknowledged `decision_required` | CONFORMS | `internal/digest/builder_test.go` exercises this path with string literals; functional conformance exists, type-safety gap only |

### §5 Invariants

| Invariant | Description | Status | Note |
|-----------|-------------|--------|------|
| EV-INV-001 | Events observational, not authoritative | CONFORMS | |
| EV-INV-002 | Event-loss between fsyncs acceptable; consumers MUST handle it | **GAP** | Producer side: F-class events missing from fsyncBoundaryEventTypes means loss window is wider than spec intends; Consumer side: ReplayFrom stub (EV-014d gap) means consumer recovery hooks are not closed |
| EV-INV-003 | At-most-one sync consumer per type; no reentrant emission | CONFORMS | busimpl.go:checkSyncCardinality enforces |
| EV-INV-004 | Every event carries valid monotonic UUIDv7 event_id | CONFORMS | eventidgen.go monotonic generator; no HWM cross-restart guarantee (EV-002c gap) |
| EV-INV-005 | Every §8 entry carries full tagging | CONFORMS | Spec declares tags; registry enforces |
| EV-INV-006 | No event payload contains secret-named field at emission | CONFORMS | EV-035 redaction + EV-036 startup scan |

### §6.1 RECORD shapes

| Record | Spec fields | Code | Status | Note |
|--------|-------------|------|--------|------|
| Event envelope | event_id, schema_version, type, timestamp_wall, timestamp_mono_nsec, run_id, state_id, source_subsystem, trace_context, payload | busimpl.go Event struct | CONFORMS | All fields present |
| TraceContext | trace_id, parent_event_id, root_event_id | `internal/core/tracecontext.go` | CONFORMS | Matches spec exactly |
| Subscription | consumer_id, consumer_class, event_pattern, since, offset_checkpoint_event_id, on_panic, handler | `internal/core/subscription.go` | CONFORMS | All 7 spec fields present; DeclaredEmitTypes is an additive extension for EV-010 acyclicity |
| EventPattern | wildcard, types | `internal/core/eventpattern.go` | **GAP** | Spec says `types: Set<EventType>`; code uses `map[string]struct{}` (comment line 22: "TODO to replace string elements with core.EventType"); type-safety weaker than spec requires |
| EventBus interface | Emit, Subscribe, Seal, ReplayFrom, DeadLetterReplay, Drain | `internal/eventbus/eventbus.go` | CONFORMS | All 5 spec methods present; additional capability interfaces (EmitWithRunID, RunDrainer, etc.) are additive |
| ConsumerClass enum | synchronous, asynchronous, observer | `internal/core/consumerclass.go` | CONFORMS | |
| OnPanic enum | recover_and_log, quarantine_consumer, fail_daemon | `internal/core/onpanic.go` | CONFORMS | quarantine_consumer and fail_daemon declared but enforcement deferred per OQ-EV-007; acceptable per spec |

### §6.2 On-disk JSONL format

| Check | Status | Note |
|-------|--------|------|
| Primary log path `.harmonik/events/events.jsonl` | CONFORMS | jsonlwriter.go |
| Dead-letter log `.harmonik/events/dead-letters.jsonl` | CONFORMS | busimpl.go |
| Spill files `.harmonik/events/spill-<consumer>.jsonl` | CONFORMS | EV-011a implementation |
| Append-only (EV-020) | CONFORMS | `O_CREATE\|O_WRONLY\|O_APPEND` |
| Torn-tail rule: discard silently in post-crash context | CONFORMS | jsonlwriter.go:Filter skips malformed final lines |
| Concurrent tailing: final line non-authoritative until newline | CONFORMS | ScanAfter uses newline sentinel |

### §6.3 Payload schemas (selected)

| Event type | Spec fields | Code location | Status | Note |
|------------|-------------|---------------|--------|------|
| run_started | run_id, workflow_id, workflow_version, bead_id?, workspace_path, input_ref, workflow_mode?, queue_id?, queue_group_index? | runstartedpayload.go | CONFORMS | All optional fields present including v0.4.0 workflow_mode and v0.5.0 queue fields |
| run_completed | run_id, terminal_state_id, ended_at, summary?, workflow_mode?, queue_id?, queue_group_index? | runterminalpayload.go | CONFORMS | Code also has owning_epic_id?, owning_epic_assignee? (additive, acceptable) |
| run_failed | run_id, terminal_state_id?, failure_class, error_category?, ended_at, reason, queue_id?, queue_group_index? | runterminalpayload.go | CONFORMS | Code adds last_checkpoint (additive) |
| transition_event | run_id, transition_id, from_state_id, to_state_id, commit_hash, transition_kind | transitioneventpayload.go | CONFORMS | |
| checkpoint_written | run_id, state_id, transition_id, commit_hash, bead_id? | (payload struct) | CONFORMS | |
| implementer_resumed | run_id, workflow_mode, session_id, claude_session_id, iteration_count, prior_verdict_summary | (payload struct) | CONFORMS | |
| reviewer_verdict | run_id, workflow_mode, session_id, claude_session_id, iteration_count, schema_version, verdict, flags, notes | verdictevent (from verdictevent_test.go) | CONFORMS | |
| review_fixup_stalled | run_id, workflow_mode, iteration_count, reviewer_flags, diff_hash_current, diff_hash_prior | (payload struct) | CONFORMS | §8.1a.7 added in v0.6.1 |
| daemon_started | started_at, pid, binary_commit_hash | DaemonStartedPayload | CONFORMS | |
| daemon_ready | ready_at, ready_at_ns_since_boot, investigator_run_ids | DaemonReadyPayload | CONFORMS | Both required fields per ON-033 |
| daemon_shutdown | shutdown_at, shutdown_at_ns_since_boot, mode | DaemonShutdownPayload | CONFORMS | Both required fields per ON-033 |
| daemon_degraded | detected_at, reason (exhaustive enum) | DaemonDegradedPayload | CONFORMS | DaemonDegradedReason enum matches spec |
| handler_paused | agent_type, cause, in_flight_count, paused_epoch | HandlerPausedPayload | CONFORMS | §8.11.1 |
| handler_resumed | agent_type, by, prior_cause, paused_epoch | HandlerResumedPayload | CONFORMS | §8.11.2 |
| queue_submitted | queue_id, submitted_at, group_count, total_bead_count, schema_version | (payload struct) | CONFORMS | §8.10.1 |
| decision_needed | question, options[], context_link?, blocked_agent?, value_requested? | `internal/core/decisionpayloads_33p.go` | CONFORMS | §8.14.1; Valid() enforces required fields |
| decision_resolved | decision_id, chosen_option, value?, resolver? | decisionpayloads_33p.go | CONFORMS | §8.14.2; first-writer-wins per EV-045 |
| decision_withdrawn | decision_id, reason, by? | decisionpayloads_33p.go | CONFORMS | §8.14.3 |
| decision_required | subject{kind, id}, reason, suggested_action, ack_required, ack_token, triggering_event_id | **NOT FOUND** | **GAP** | No Go struct or EventType constant in reviewed files; §8.12.1 added in v0.6.0 |
| decision_acknowledged | ack_token, subject{kind, id}, ack_method, acked_at | **NOT FOUND** | **GAP** | No Go struct or EventType constant in reviewed files; §8.12.2 added in v0.6.0 |
| bead_sync_failed | run_id, error, timestamp | **NOT FOUND** | **GAP** | No Go struct or EventType constant; §8.15.1 added in v0.6.4 |
| bead_ledger_conflict_audit | run_id, bead_ids[], conflicts[], timestamp | **NOT FOUND** | **GAP** | No Go struct or EventType constant; §8.15.2 added in v0.6.4 |
| gate_definition_drift | run_id, gate_name, prior_envelope_hash, current_envelope_hash, changed_inputs | **NOT FOUND** | **GAP** | No Go struct or EventType constant; §8.2.13 added in v0.3.4 |
| gate_redefined_under_cat_6 | run_id, gate_name, prior_decision, new_decision, cat_6_verdict_id | **NOT FOUND** | **GAP** | No Go struct or EventType constant; §8.2.14 added in v0.3.4 |
| bead_label_conflict | bead_id, conflicting_labels[], fallback_action, detected_at | EventTypeBeadLabelConflict constant present; payload struct existence unconfirmed | CONFORMS | §8.8.6 |
| epic_completed | epic_id, last_child_bead_id, closed_at | EpicCompletedPayload | CONFORMS | §8.13 |
| bus_overflow | consumer_name, event_type, event_id, queue_depth, shed_at, shed_policy | busimpl.go | CONFORMS | §8.8.4 |

### §8 Event taxonomy — EventType enum coverage

#### In spec, present in code ✓
All §8.1 (run lifecycle), §8.1a (review-loop cycle including review_fixup_stalled), §8.2 rows 1–12 (hook/gate/guard/CP events), §8.3 (agent/handler lifecycle 1–14), §8.4 (budget), §8.5 (workspace), §8.6.1–6.13 (reconciliation), §8.7 (daemon/operator), §8.8 (observability), §8.10 (queue), §8.11 (handler-pause), §8.13 (epic_completed), §8.14 (HITL decisions) are all present as typed constants in `internal/core/eventtype.go`.

#### In spec v0.6.4, MISSING from EventType enum

| §8 row | Type | Class | Added in version |
|--------|------|-------|-----------------|
| §8.2.13 | `gate_definition_drift` | F | v0.3.4 |
| §8.2.14 | `gate_redefined_under_cat_6` | F | v0.3.4 |
| §8.12.1 | `decision_required` | F | v0.6.0 |
| §8.12.2 | `decision_acknowledged` | F | v0.6.0 |
| §8.15.1 | `bead_sync_failed` | F | v0.6.4 |
| §8.15.2 | `bead_ledger_conflict_audit` | O | v0.6.4 |

`bead_terminal_transition_recovered` (§8.6.14) IS present as `EventTypeBeadTerminalTransitionRecovered` — the spec marks it (post-MVH); no MVH conformance obligation per §8.6.14 note; N-A.

#### In code, NOT in §8 (EV-026 potential violation)

The following event types appear in `eventtype.go` and are presumably emitted to the cross-bus but have no §8 taxonomy entry and no EV-027 amendment on record:

`node_dispatch_decided`, `implementer_escaped_worktree`, `implementer_phase_complete`, `merge_build_failed`, `skills_resolved`, `agent_heartbeat`, `launch_initiated`, `agent_ready_timeout`, `post_agent_ready_hang`, `pasteinject_failed`, `launch_stall_detected`, `spawn_cap_blocked`, `implementer_budget_exceeded`, `tmux_new_window_timeout`, `codex_billing_guard`, `reviewer_budget_exceeded`, `run_stale`, `gate_decision_recorded`, `bead_closed`, `working_tree_refresh_failed`, `bead_claim_skipped`, `session_keeper_*` family, `loop_observed_phantom_done`, `review_gate_anomaly`, `stale_open_bead_detected`, `worker_unhealthy`, `worker_offline`, `worker_tunnel_failed`, `worker_report`, `resource_breach`, `governor_signal`, `liveness_halt`, `daemon_config`, `decision_required` (used as string), `decision_acknowledged` (used as string).

Per EV-026, these MUST NOT cross the bus without §8 registration + EV-027 amendment. Functional behavior may be correct, but the §8 taxonomy is stale relative to what the bus actually routes.

### §6.4 Schema evolution — breaking-change table

| Status | Note |
|--------|------|
| CONFORMS | Breaking-change table present in spec; all observed payload additions in code use additive-field pattern (§6.4 row 1); no breaking renames observed |

### §6.5 Co-owned event payloads

| Status | Note |
|--------|------|
| CONFORMS | Co-ownership map in spec aligns with emitter references in code; each emitter spec declares WHEN |

---

## 3. Gaps summary

### G1 — `fsyncBoundaryEventTypes` map is incomplete (EV-016 / EV-INV-002)

**Spec section:** §4.4 EV-016; §8 durability-class column  
**Code location:** `internal/eventbus/busimpl.go:115–138`  
**Severity:** BLOCKER  
**Description:** The static `fsyncBoundaryEventTypes` map includes only 14 entries (the §8.1 run-lifecycle F-class events, §8.7 daemon-lifecycle F-class events, `agent_message`, and the three §8.14 decision events). At least 15 additional event types are declared F-class in the spec but absent from the map. When any of these events is emitted, `isFsyncBoundaryEvent()` returns false and `jsonlwriter.Append` is called with `sync=false`, silently downgrading these events to O-class durability. Loss of any of these events on a hard crash violates EV-016 and EV-INV-002.

Missing F-class entries:

| §8 row | Type | Loss consequence per spec |
|--------|------|--------------------------|
| §8.1a.3 | `reviewer_verdict` | Orphans terminal routing of a review-loop run |
| §8.1a.6 | `review_loop_cycle_complete` | Forces reconciliation into heavy work (Cat 3/4) |
| §8.2.12 | `policy_expression_exceeded_cost` | Durability pair with CP-034b abort; abort silently unreported |
| §8.2.13 | `gate_definition_drift` | Cat 6 escalation fires before durable landmark |
| §8.2.14 | `gate_redefined_under_cat_6` | Lifecycle boundary for Cat 6 re-evaluation |
| §8.5.3 | `workspace_merge_status` | Loss forces git-DAG reconstruction for merge status |
| §8.10.1 | `queue_submitted` | Loss orphans the queue's execution plan |
| §8.10.3 | `queue_group_completed` | Loss orphans group-boundary advance decision |
| §8.10.4 | `queue_paused` | Loss orphans hard pause landmark |
| §8.10.7 | `queue_item_reconciled` | Loss could silently re-dispatch a reverted item |
| §8.11.1 | `handler_paused` | Loss leaves pause-state landmark unrecoverable |
| §8.11.2 | `handler_resumed` | Loss leaves pause-state landmark unrecoverable |
| §8.12.1 | `decision_required` | Loss silently leaves double-failed bead eligible for re-dispatch |
| §8.12.2 | `decision_acknowledged` | Loss breaks JSONL observability for ACK |
| §8.15.1 | `bead_sync_failed` | Loss silences Cat-BL2 routing obligation |

Note: G4 (missing EventType constants for §8.12 and §8.15) means `decision_required`, `decision_acknowledged`, and `bead_sync_failed` cannot currently be emitted via typed constants; the fsync gap is secondary.

---

### G2 — EV-002c UUIDv7 high-water-mark not implemented

**Spec section:** §4.1 EV-002c  
**Code location:** `internal/core/eventidgen.go` (comment references hk-hqwn.5 as planned)  
**Severity:** MAJOR  
**Description:** EV-002c requires the daemon to persist a UUIDv7 high-water-mark (HWM) to `.harmonik/event_id_hwm` on every F-class fsync, read it on startup, and guarantee every new `event_id` is strictly greater than the HWM to handle wall-clock regression across restarts. No HWM file path, no startup HWM read, and no `daemon_degraded{reason=clock_regression}` emission were found in the reviewed code. `eventidgen.go` comment says this is tracked under hk-hqwn.5. Without this, cross-restart UUIDv7 monotonicity is not guaranteed; EV-002c's clock-regression detection is absent.

---

### G3 — `ReplayFrom` / `DeadLetterReplay` are stubs (EV-014d)

**Spec section:** §4.3 EV-014d; §6.1 EventBus interface  
**Code location:** `internal/eventbus/busimpl.go:1085–1092`  
**Severity:** MAJOR  
**Description:** Both `ReplayFrom` and `DeadLetterReplay` return `nil` unconditionally. EV-014d requires that on startup, for every subscription with a non-nil `since` or `offset_checkpoint_event_id`, the bus performs a JSONL-tail replay to the consumer before live-stream delivery resumes. `TailTruncationCallback` is declared in `eventbus.go` but the bus never invokes it because replay is unimplemented. The `jsonlwriter.go:ScanAfter` function provides the JSONL scanning primitive needed, but it is not wired into the bus startup path. EV-INV-002's consumer side ("consumers MUST handle tail-truncated stream on recovery") is structurally unsatisfied because the bus offers no replay hook to exercise it.

---

### G4 — Six §8 event types missing from `EventType` enum

**Spec section:** §8.2.13–14, §8.12.1–2, §8.15.1–2; EV-016, EV-031, EV-032  
**Code location:** `internal/core/eventtype.go`  
**Severity:** BLOCKER (§8.12 F-class events that gate dispatch), MAJOR (§8.2.13–14, §8.15)  
**Description:** The following §8 event types have no `EventType` constant in `eventtype.go` and no payload struct found in the reviewed Go files:

| Type | §8 row | Class | Version added | Consequence |
|------|--------|-------|---------------|-------------|
| `gate_definition_drift` | §8.2.13 | F | v0.3.4 | Mechanism-tagged Gate replay drift undetectable via typed path |
| `gate_redefined_under_cat_6` | §8.2.14 | F | v0.3.4 | Cat 6 Gate re-evaluation unrecorded via typed path |
| `decision_required` | §8.12.1 | F | v0.6.0 | Dispatch-blocking gate (EV-042/EV-043) cannot be emitted safely |
| `decision_acknowledged` | §8.12.2 | F | v0.6.0 | ACK record (EV-043) cannot be emitted safely |
| `bead_sync_failed` | §8.15.1 | F | v0.6.4 | Cat-BL2 routing landmark (BL-MRG-004) missing |
| `bead_ledger_conflict_audit` | §8.15.2 | O | v0.6.4 | Cat-BL3 audit observability missing |

`decision_required` and `decision_acknowledged` are used as plain strings in test code (`internal/digest/builder_test.go`), confirming functional awareness but no typed constant and no fsync registration.

---

### G5 — EventPattern.Types uses `map[string]struct{}` instead of `Set<EventType>`

**Spec section:** §6.1 `RECORD EventPattern: types: Set<EventType>`  
**Code location:** `internal/core/eventpattern.go:35`  
**Severity:** MINOR  
**Description:** The spec requires `types: Set<EventType>` (strongly typed). Code uses `map[string]struct{}`, acknowledging this with a TODO comment: "Types uses string as the element type. The hoist to EventType is non-breaking." Functional behavior is correct (string values are identical to EventType values); only compile-time type-safety is weaker. Invalid event type strings in a pattern will not be caught at subscription time.

---

### G6 — Non-§8 event types emitted on cross-bus without EV-027 amendments (EV-026)

**Spec section:** §4.6 EV-026, EV-027  
**Code location:** `internal/core/eventtype.go` (approximately 30+ non-§8 types)  
**Severity:** MAJOR  
**Description:** EV-026 states "Internal events … MUST NOT cross the bus." EV-027 requires a foundation amendment for any new cross-bus event type. Approximately 30+ event types in `eventtype.go` have no §8 taxonomy entry and no EV-027 amendment on record (e.g. `node_dispatch_decided`, `implementer_escaped_worktree`, `agent_heartbeat`, `launch_initiated`, `run_stale`, `session_keeper_*`, `worker_*`, `governor_signal`, `liveness_halt`). If these are emitted to the bus (not just defined as constants), each violates EV-026. This is the most pervasive structural gap: the §8 taxonomy is significantly stale relative to what the bus actually routes. Without amendment records, subsystem emitters for these types have no normative payload-shape owner per EV-025.

---

## 4. v0.3.3 → v0.6.4 delta assessment (highest-value section)

The epic hk-hqwn was originally scoped against spec v0.3.3. The spec has since advanced to v0.6.4 through 12 revision steps. This section identifies what changed and whether code reflects it.

| Spec version | Key additions | Code status |
|-------------|---------------|-------------|
| v0.3.4 | §8.2.10–12 (control_points_registration_started, verdict_envelope_mismatch, policy_expression_exceeded_cost) + §8.6.11–13 reconciliation events + §8.7.16–17 (operator_command_failed, operator_escalation_cleared) + §8.8.5 (redaction_failed) | EventType constants present ✓; **`policy_expression_exceeded_cost` missing from fsync map** (G1); `gate_definition_drift` / `gate_redefined_under_cat_6` constants absent (G4) |
| v0.4.0 | §8.1a review-loop cycle (6 events incl. reviewer_verdict F-class) + §8.8.6 bead_label_conflict | EventType constants present ✓; **`reviewer_verdict` and `review_loop_cycle_complete` missing from fsync map** (G1 — highest-value missing fsync entries) |
| v0.5.0 | §8.10 queue lifecycle (6 events; 4 F-class) + queue_id/queue_group_index on run_started/completed/failed | EventType constants present ✓; **queue F-class events missing from fsync map** (G1) |
| v0.5.1 | §8.10.7 queue_item_reconciled (F-class) | EventType constant present ✓; **missing from fsync map** (G1) |
| v0.5.2 | §8.7.14 daemon_orphan_sweep_completed additive fields | Code conforms ✓ |
| v0.5.3 | §8.2.13–14 gate_definition_drift / gate_redefined_under_cat_6 (both F-class) | **Missing from EventType enum AND fsync map** (G1, G4) |
| v0.5.4 | ON-049 attribution fields on §8.4 budget events (additive) | Code has role/node_id fields on budget payloads ✓ |
| v0.6.0 | §8.3.14 lifecycle_transition + §8.12 decision_required/decision_acknowledged (both F-class) + §4.11 subscribe contract EV-037–EV-041 + §4.12 EV-042–EV-044 | lifecycle_transition constant present ✓; **decision_required/decision_acknowledged missing from EventType enum AND fsync map** (G1, G4) |
| v0.6.1 | §8.1a.7 review_fixup_stalled (new event + completion_reason=fixup_stalled) | EventType constant present ✓; payload struct confirmed ✓ |
| v0.6.2 | §8.14 HITL decisions (decision_needed/resolved/withdrawn, all F-class) | Constants present ✓; fsync map present ✓; payload structs in decisionpayloads_33p.go ✓ |
| v0.6.3 | §8.1a.5 APPROVED-AND-DONE DOT exemption | Code landed per bead hk-8ps7q ✓ |
| v0.6.4 | §8.15 bead_sync_failed (F-class) + bead_ledger_conflict_audit (O-class) | **Both missing from EventType enum** (G4); bead_sync_failed also missing from fsync map (G1) |

**Summary of delta gaps (where spec evolved but code did not):**
- G1 (fsync map): started at v0.4.0 (reviewer_verdict, review_loop_cycle_complete); accumulated through v0.5.0 (queue events), v0.5.1, v0.5.3, v0.6.0, v0.6.4
- G4 (missing constants): v0.3.4 (gate_definition_drift, gate_redefined_under_cat_6), v0.6.0 (decision_required, decision_acknowledged), v0.6.4 (bead_sync_failed, bead_ledger_conflict_audit)

The most operationally impactful single gap from the delta is **`reviewer_verdict` not in `fsyncBoundaryEventTypes`** (v0.4.0 gap): loss of a reviewer verdict on a hard crash would orphan the terminal routing decision for every review-loop or DOT-mode run — the affected run would need Cat 3/4 reconciliation rather than a simple JSONL read.

---

## 5. Final verdict

```
GAPS FOUND: 6
```

| # | Gap ID | Severity | One-line description |
|---|--------|----------|---------------------|
| 1 | G1 | BLOCKER | `fsyncBoundaryEventTypes` map missing 15 F-class event types (EV-016) |
| 2 | G2 | MAJOR | EV-002c UUIDv7 HWM cross-restart monotonicity not implemented |
| 3 | G3 | MAJOR | `ReplayFrom` / `DeadLetterReplay` are stubs; EV-014d consumer-recovery replay unimplemented |
| 4 | G4 | BLOCKER | 6 §8 event types missing from `EventType` enum (gate_definition_drift, gate_redefined_under_cat_6, decision_required, decision_acknowledged, bead_sync_failed, bead_ledger_conflict_audit) |
| 5 | G5 | MINOR | `EventPattern.Types` uses `map[string]struct{}` instead of typed `Set<EventType>` |
| 6 | G6 | MAJOR | ~30+ non-§8 event types cross the bus without EV-027 amendments, violating EV-026 |
