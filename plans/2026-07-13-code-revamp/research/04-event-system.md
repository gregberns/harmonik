# Dossier: The existing event infrastructure (record → replay base layer)

Factual inventory of what record→replay can be built on **today**. All references are `file:line` against the repo at HEAD (branch `main`, 2026-07-13). This documents what IS; it does not recommend.

---

## 0. Map of the subsystem

| Concern | File | Role |
|---|---|---|
| Envelope struct | `internal/core/event.go` | The `Event` record every event carries |
| Type registry | `internal/core/eventregistry.go` | constructor map, schema-version table, `DecodePayload`, `ValidateEnvelopeSchemaVersion` |
| Decode dispatch | `internal/core/eventdispatch.go` | `DispatchObservational` / `DispatchSynchronous` (thin wrappers over `DecodePayload`) |
| Per-type compat table | `internal/core/pertypecompat_hqwn38.go` | N-1 compatibility declarations (one row per type) |
| EventType enum | `internal/core/eventtype.go` | 175 `EventType` string constants |
| Emission API | `internal/eventbus/eventbus.go` (interface), `internal/eventbus/busimpl.go` (impl) | `Emit` / `EmitWithRunID` / `EmitTyped` |
| JSONL writer | `internal/eventbus/jsonlwriter.go` | append-only durable log writer (`Append`) |
| JSONL reader / replay | `internal/eventbus/jsonlwriter.go` | `ScanAfter` (since-cursor iterator), `Filter` (by run_id) |
| Subscribe CLI | `cmd/harmonik/subscribe.go` | `harmonik subscribe` over the daemon Unix socket |
| Subscribe hub (server) | `internal/daemon/subscribe.go` | daemon-side `HandleSubscribe` (replay + live stream) |
| On-disk path constant | `internal/core/jsonlformat_hqwn58.go:20` | `const EventsJSONLPath = "events/events.jsonl"` |

---

## 1. The event envelope

Defined at `internal/core/event.go:27-97`. Verbatim struct:

```go
type Event struct {
	EventID           EventID          `json:"event_id"`                     // UUIDv7, stamped by emitter/daemon
	SchemaVersion     int              `json:"schema_version"`               // envelope schema version; non-zero
	Type              string           `json:"type"`                         // §8 type name; NOT yet the EventType enum
	TimestampWall     time.Time        `json:"timestamp_wall"`               // RFC3339; ADVISORY ONLY for ordering (EV-006)
	TimestampMonoNsec *int64           `json:"timestamp_mono_nsec,omitempty"`// process-scoped monotonic; intra-process only
	RunID             *RunID           `json:"run_id,omitempty"`             // present only for run-scoped events
	StateID           *StateID         `json:"state_id,omitempty"`
	SourceSubsystem   string           `json:"source_subsystem"`             // emitting Go package id; non-empty
	TraceContext      *TraceContext    `json:"trace_context,omitempty"`
	Payload           json.RawMessage  `json:"payload"`                      // type-specific body, raw bytes
}
```

Key facts:

- **Payload is NOT typed on the envelope — it is `json.RawMessage`** (`event.go:96`). The tagged-union pattern (EV-032) keeps the envelope monomorphic and decodes the payload lazily via the per-type registry keyed on `Type`. This is the crux for replay: you can read the whole stream as `Event` values without knowing any payload type, then decode selectively.
- **`Type` is a `string`, not the `EventType` enum** (`event.go:47`). The enum (`internal/core/eventtype.go`, 175 constants) exists but the envelope was never hoisted to it (documented as "non-breaking hoist" at `event.go:17-19`, `event.go:45-46`). So the on-disk type is free-form text.
- **Ordering is by `EventID` (UUIDv7), never by timestamp** (`event.go:54-59`). `TimestampWall` is explicitly "advisory only for cross-process ordering." UUIDv7 lexicographic byte order == chronological order; the readers rely on this (see §5).
- **Schema version is per-envelope AND per-type.** The envelope `SchemaVersion` field (`event.go:41`) is stamped from the per-type registry at emit time (see §2). Every live type is at version 1 today (see §4 / `pertypecompat_hqwn38.go`).
- **`Valid()` (`event.go:113-145`)** enforces the required-field invariant. **`EquivalentTo()` (`event.go:166-188`)** defines idempotency-equivalence (same EventID + Type + structurally-equal Payload) for re-emission during recovery — a data-shape sensor, not a dedupe mechanism.

---

## 2. Emission

### The interface (`internal/eventbus/eventbus.go:160-245`)

```go
type EventBus interface {
	Emit(ctx context.Context, eventType core.EventType, payload []byte) error
	EmitWithRunID(ctx context.Context, runID core.RunID, eventType core.EventType, payload []byte) error
	Subscribe(sub core.Subscription) (core.Subscription, error)
	Seal() error
	ReplayFrom(consumerID string, since core.EventID) error
	DeadLetterReplay(consumerName string, filter *core.EventPattern) error
	Drain(ctx context.Context) error
}
```

Optional capability interfaces (type-asserted off the same `EventBus` value): `RunDrainer.DrainRun` (`eventbus.go:32`), `CommsMessageEmitter.EmitAgentMessage` (`eventbus.go:59`), `CommsPresenceEmitter.EmitAgentPresence` (`eventbus.go:81`), `TypedEmitter.EmitTyped` (`eventbus.go:108`) — the last three return the minted `EventID`.

### The implementation — `busImpl.Emit` (`internal/eventbus/busimpl.go:412-529`)

Emit is a **six-step synchronous pipeline** on the caller's goroutine:

1. Unmarshal payload to a map for redaction (`busimpl.go:414-419`).
2. Apply the HC-031/HC-032 secret-redaction middleware **before** persistence (`busimpl.go:423`).
3. Re-marshal the redacted payload (`busimpl.go:426`).
4a. Build the complete envelope — **`EventID`, `TimestampWall`, `SchemaVersion` are stamped here inside the emitter** (`busimpl.go:437-455`). `SourceSubsystem` is hardcoded `"eventbus"` (`busimpl.go:453`; noted as MVH placeholder pending daemon-watcher stamping EV-002b). Schema version comes from `core.LookupTypeSchemaVersion` (`busimpl.go:441`) — **this is the one live consumer of the registry's version table.**
4b. **JSONL append + conditional fsync** (`busimpl.go:462-472`): marshal the whole envelope to one line, `b.jsonlWriter.Append(envelopeBytes, needsSync)`. `needsSync = isFsyncBoundaryEvent(eventType)`.
5-6. Snapshot subscriptions under lock, then dispatch per consumer class (`busimpl.go:476-527`).

### Representative call site (`internal/daemon/workloop.go:5984-6001`)

```go
func emitRunStarted(ctx context.Context, bus handlercontract.EventEmitter, runID core.RunID, ...) {
	pl := workloopRunStartedPayload{ RunID: runID.String(), BeadID: string(beadID), ... }
	b, err := json.Marshal(pl)
	if err != nil { return }
	_ = bus.EmitWithRunID(ctx, runID, core.EventTypeRunStarted, b)
}
```

### Is emission synchronous? Does it block? Durability?

- **Persistence (steps 1-4b) is synchronous and blocks the caller** until the JSONL line is written. For **F-class (fsync-boundary)** events the `Append` call also blocks on `fsync(2)` before returning (`busimpl.go:466-469`).
- **Consumer dispatch is split by class** (`busimpl.go:490-526`): *synchronous* consumers run inline and block Emit (`busimpl.go:491-497`); *async / observer* consumers are dispatched on a fresh goroutine per dispatch and do **not** block Emit (`busimpl.go:499-525`, with panic recovery → dead-letter sink).
- **Delivery guarantee is at-least-once, best-effort.** Note the call site: `_ = bus.EmitWithRunID(...)` — **the error return is discarded** at nearly every production emit site (`workloop.go:6000`, all keeper sites in §7). Emission failures are silently dropped. Durability rests entirely on the JSONL append + fsync for F-class; O-class events can be lost on crash before the OS flushes the page cache.

### F-class (fsync-before-return) type set (`internal/eventbus/busimpl.go:142-199`)

`isFsyncBoundaryEvent` looks up a **static map** `fsyncBoundaryEventTypes`. F-class types (durable before Emit returns): `run_started`, `run_completed`, `run_failed`, `transition_event`, `checkpoint_written`, `reviewer_verdict`, `review_loop_cycle_complete`, `policy_expression_exceeded_cost`, `gate_definition_drift`, `gate_redefined_under_cat_6`, `workspace_merge_status`, `daemon_started`, `daemon_ready`, `daemon_shutdown`, `daemon_startup_failed`, `operator_upgrade_completed`, `queue_submitted`, `queue_group_completed`, `queue_paused`, `queue_item_reconciled`, `handler_paused`, `handler_resumed`, `decision_required`, `decision_acknowledged`, `agent_message`, `decision_needed`, `decision_resolved`, `decision_withdrawn`, `bead_sync_failed`. **Everything else is O-class (no fsync) — the vast majority of types.**

The writer itself batches: concurrent `Append(sync=true)` calls are coalesced into one write + one fsync per batch by a drainer goroutine (`jsonlwriter.go:42-75`), so P99 emit latency is O(1×fsync) not O(N×fsync) under concurrency.

---

## 3. Persistence

### Where it lands

- Path constant: `const EventsJSONLPath = "events/events.jsonl"` (`internal/core/jsonlformat_hqwn58.go:20`), joined onto `.harmonik/`. Resolved absolute path: **`.harmonik/events/events.jsonl`** (e.g. `cmd/harmonik/comms.go:646`, `internal/keeper/watcher.go:768`).
- The daemon wires `EventsJSONLPath: cfg.JSONLLogPath` (`internal/daemon/daemon.go:1024`); `JSONLLogPath` is set per launch path (`cmd/harmonik/run.go:706`, `cmd/harmonik/harness.go:459`).

### The format

**One JSON object per line (NDJSON), append-only.** The writer opens with `O_CREATE|O_WRONLY|O_APPEND` (`jsonlwriter.go:108`) so the kernel positions every write at EOF (POSIX append atomicity within `PIPE_BUF`). It **MUST NOT rewrite, truncate, or reorder** existing lines (EV-020, `jsonlwriter.go:29-35`). A torn tail (final line with no trailing `\n`) is discarded silently by readers on post-crash startup.

The writer (`internal/eventbus/jsonlwriter.go:76-295`):

```go
func (w *JSONLWriter) Append(line []byte, sync bool) error   // jsonlwriter.go:246
```

Each `Append` enqueues a `writeRequest{buf, doSync, result}` on a buffered channel (cap 128, `jsonlwriter.go:117`) and blocks on the result channel; the drainer coalesces queued writes and issues one fsync per batch if any request set `doSync`. `Close` drains and stops the goroutine (`jsonlwriter.go:285-295`). There is a `nullJSONLWriter` (`busimpl.go:37`) used when no log path is configured — `Append` is a no-op.

### There is NO rotation

No size/time rotation, no naming scheme beyond the single fixed file. The "baseline" directory is a **manual frozen snapshot**, not a rotation product.

### Size / count (verified against `.harmonik/events/`)

| File | Bytes | Lines (events) |
|---|---|---|
| `.harmonik/events/baseline-2026-07-13/events.jsonl` | 89,089,568 (~85 MiB / 89 MB) | **237,099** ✔ matches the stated baseline |
| `.harmonik/events/events.jsonl` (live) | 89,437,354 | 238,372 (grown since the freeze) |

The baseline dir is read-only (`dr-xr-xr-x`, files `-r--r--r--`) and also carries `issues.jsonl` (5.8 MB) and `issues.closed.jsonl` (3.4 MB) beads snapshots. First baseline line (real):

```json
{"event_id":"019e286e-b12d-7f03-a5f7-72a4799d1571","schema_version":1,"type":"daemon_started","timestamp_wall":"2026-05-14T14:40:03.501984-07:00","source_subsystem":"eventbus","payload":{"binary_commit_hash":"unknown","pid":98527,"started_at":"2026-05-14T21:40:03Z"}}
```

So the log spans ~2 months (2026-05-14 → 2026-07-13) of a single project's daemon activity.

---

## 4. The event catalog (which verticals are instrumented)

Authoritative enumeration: the `EventType` enum (`internal/core/eventtype.go`, **175 constants**) and the per-type compat table `allPayloadCompatEntries` (`internal/core/pertypecompat_hqwn38.go:86-369`). **Every declared type is at schema version 1** (`CurrentVersion: 1, PreviousVersion: 0`) — no type has ever been version-bumped. Grouped by subsystem (§ headings are from the compat table):

- **§8.1 Run lifecycle:** `run_started`, `run_completed`, `run_failed`, `state_entered`, `state_exited`, `transition_event`, `checkpoint_written`, `outcome_emitted`, `sub_workflow_entered`, `sub_workflow_exited`, `node_dispatch_requested`, `node_dispatch_decided`, `bead_closed`, `epic_completed`, `working_tree_refresh_failed`, `implementer_escaped_worktree`, `implementer_phase_complete`, `merge_build_failed`
- **§8.1a Review loop:** `implementer_resumed`, `reviewer_launched`, `reviewer_verdict`, `iteration_cap_hit`, `no_progress_detected`, `review_loop_cycle_complete`, `review_bypassed`, `review_fixup_stalled`
- **§8.2 Control-point / gate / hook:** `hook_fired`, `hook_failed`, `hook_verdict_persisted`, `gate_allowed`, `gate_denied`, `gate_escalated`, `gate_decision_recorded`, `skills_resolved`, `guard_reordered`, `guard_failed`, `control_points_registered`, `control_points_registration_started`, `verdict_envelope_mismatch`, `policy_expression_exceeded_cost`, `gate_definition_drift`, `gate_redefined_under_cat_6`
- **§8.3 Agent/handler lifecycle (largest group):** `agent_started`, `agent_ready`, `agent_output_chunk`, `agent_completed`, `agent_failed`, `agent_heartbeat`, `agent_rate_limit_status`, `session_log_location`, `skills_provisioned`, `handler_capabilities`, `agent_warning_silent_hang`, `agent_resumed_after_warning`, `agent_soft_terminating`, `agent_hard_terminating`, `launch_initiated`, `agent_ready_timeout`, `post_agent_ready_hang`, `lifecycle_transition`, `pasteinject_failed`, `launch_stall_detected`, `agent_ready_stall_detected`, `spawn_cap_blocked`, `implementer_budget_exceeded`, `reviewer_budget_exceeded`, `tmux_new_window_timeout`, `codex_billing_guard`, `pi_billing_guard`, `harness_selected`, `model_selected`, `provider_selected`
- **agent-comms (§8.3):** `agent_message`, `agent_presence`
- **hitl-decisions (§8.3 / §8.12):** `decision_needed`, `decision_resolved`, `decision_withdrawn`
- **§8.4 Budget:** `budget_accrual`, `budget_warning`, `budget_exhausted`
- **§8.5 Workspace:** `workspace_created`, `workspace_leased`, `workspace_discarded`, `workspace_interrupted`, `workspace_merge_status`
- **§8.6 Reconciliation (large):** `reconciliation_started`, `reconciliation_completed`, `reconciliation_mismatch_observed`, `reconciliation_category_assigned`, `reconciliation_verdict_emitted`, `reconciliation_verdict_executed`, `reconciliation_verdict_malformed`, `reconciliation_verdict_stale`, `reconciliation_verdict_execution_retry`, `reconciliation_budget_exhausted`, `reconciliation_dispatch_deduplicated`, `reconciliation_detector_panic`, `bead_terminal_transition_recovered`, `operator_escalation_required`, `divergence_inconclusive`, `store_divergence_detected`
- **§8.7 Operator / daemon lifecycle:** `daemon_started`, `daemon_ready`, `daemon_shutdown`, `daemon_degraded`, `daemon_startup_failed`, `daemon_orphan_sweep_completed`, `operator_upgrade_rejected`, `operator_upgrade_completed`, `operator_upgrading`, `operator_stopped`, `operator_resuming`, `operator_pause_status`, `operator_command_failed`, `operator_command_rejected`, `operator_escalation_cleared`, `infrastructure_unavailable`, `daemon_config`, `merge_conflict_escalation`, `dispatch_deferred`, `supervisor_revival`
- **§8.8 Observability / bus-internal:** `consumer_failed`, `dead_letter_enqueued`, `bus_overflow`, `metric`, `redaction_failed`, `bead_label_conflict`, `bead_claim_skipped`
- **§8.10 Queue:** `queue_submitted`, `queue_group_started`, `queue_group_completed`, `queue_paused`, `queue_appended`, `queue_item_deferred_for_ledger_dep`, `queue_item_reconciled`, `queue_item_held_for_handler_pause`
- **§8.11 Handler-pause:** `handler_paused`, `handler_resumed`
- **§8.12 Staleness / decision-required:** `run_stale`, `decision_required`, `decision_acknowledged`
- **§8.13 Session-keeper (large):** `session_keeper_warn`, `session_keeper_no_gauge`, `session_keeper_handoff_started`, `session_keeper_cycle_complete`, `session_keeper_cycle_aborted`, `session_keeper_clear_unconfirmed`, `session_keeper_cycle_recovered`, `session_keeper_precompact_blocked`, `session_keeper_respawn_attempted`, `session_keeper_operator_attached`, `session_keeper_restart_now_blocked`, `session_keeper_blind`, `session_keeper_hard_ceiling`, `session_keeper_idle_crew`, `session_keeper_config_rejected`, `session_keeper_watcher_dead`, `session_keeper_live_pane_recover`, `session_keeper_ack_timeout`
- **§8.14–§8.19 misc:** `review_gate_anomaly`, `disk_low`, `stall_detected`
- **§8.15 Bead-ledger merge:** `bead_sync_failed`, `bead_ledger_recovered`, `bead_ledger_corrupt`, `bead_ledger_conflict_audit`, `orphaned_child_bead`
- **§8.7.21–22 Dashboard:** `dashboard_stale`, `dashboard_refreshed`

**Instrumentation weight:** run lifecycle, agent/handler lifecycle, reconciliation, queue, operator/daemon, and session-keeper are richly event-covered. See §7 for the dark spots.

---

## 5. Replay / subscribe (reading the stream back)

There are **two independent read paths**, both alive.

### (a) Offline file replay — `eventbus.ScanAfter` / `eventbus.Filter` (`internal/eventbus/jsonlwriter.go`)

```go
func ScanAfter(path string, sinceID core.EventID) iter.Seq[core.Event]   // jsonlwriter.go:312
func Filter(path string, runID core.RunID) iter.Seq[core.Event]          // jsonlwriter.go:380
```

- **`ScanAfter`** yields every `Event` whose `EventID` bytes are lexicographically `> sinceID` — i.e. chronological, because UUIDv7 (`jsonlwriter.go:334-341`). Zero `EventID` == "from the beginning." Missing file == empty (no error). Malformed lines are skipped with a `log.Printf`. **This is the canonical offline replay primitive.** It is pure/read-only (never mutates the file).
- **`Filter`** yields only events whose **envelope `RunID` pointer dereferences to** `runID` (`jsonlwriter.go:398`). Deterministic per-run selection, but see the caveat below — it matches the *envelope* run_id only, not payload-embedded run_id.

**`ScanAfter` is heavily consumed in production** (not dead): the digest builder (`internal/digest/builder.go:371,516`, `internal/digest/resolver.go:170`), the stall sentinel (`internal/sentinel/signals.go:214`, `governor.go:329`), presence & hitl-decisions projections (`internal/presence/presence.go:163`, `internal/presence/decisions.go:126`), the watch ledger (`internal/watch/ledger.go:158,176`), supervisor-revival detection (`internal/daemon/supervisorrevival_hkrnkuy.go:48`), run-in-flight reconcile (`internal/daemon/runinflightreconcile_hkr73qr.go:100`), dashboard gather (`internal/daemon/dashboardgather.go:160`), daemon startup replay (`internal/daemon/daemon.go:2067`), and the comms scan CLI (`cmd/harmonik/comms.go:667`, `cmd/harmonik/decisions.go:524,552`). These are the **projection/fold layer** — every one is a forward fold over `ScanAfter`.

### (b) Live stream — `harmonik subscribe` over the Unix socket

Client: `cmd/harmonik/subscribe.go`. Opens `.harmonik/daemon.sock` (`subscribe.go:158`), sends one `{"op":"subscribe", ...}` JSON request, copies the NDJSON reply stream to stdout until EOF/signal (`subscribe.go:239`). Flags (`subscribe.go:58-139`): `--types t1,t2` (type filter), `--since-event-id ID` (replay cursor — replays events strictly after ID **then** joins the live stream), `--follow` (auto-reconnect with 1→10s backoff, resumes from last-seen watermark, `subscribe.go:269-434`), `--to/--from/--topic` (agent_message addressing filters), `--json` (**no-op alias — output is already NDJSON**, `subscribe.go:133`).

Server: `internal/daemon/subscribe.go` `HandleSubscribe`. On a `since_event_id` it drives the **same `eventbus.ScanAfter`** to replay before live delivery (`subscribe.go:468`), applying the type filter (`subscribe.go:475`) and addressing filters, then tails live. This unifies the offline and live paths on one reader.

### Can events be filtered by run_id / agent / type deterministically?

- **By type:** yes — `--types` (live) or filter in the `ScanAfter` fold (offline). Deterministic.
- **By run_id:** deterministic **only via structured match** — `eventbus.Filter(path, runID)` (envelope run_id) or `jq 'select(.run_id == "...")'`. See the caveat below.
- **By agent:** only for `agent_message` events, via `--to/--from/--topic` addressing filters (`subscribe.go:180-188`); there is no generic per-agent envelope field (agent identity lives in payloads).

### Why "NEVER hand-grep events.jsonl by run_id"

From `.claude/skills/major-issue-fanout/SKILL.md:28-44`:

> **NEVER hand-grep `events.jsonl` by `run_id`.** … `grep "019eae67" events.jsonl` — WRONG — produces false negatives; drove 18h of wrong diagnoses. … **Events may carry `run_id` under a nested key or may not carry it at all at the top level. Substring grep silently drops those events. Structured `jq select()` does a full-object match.**

Grounded in the code: the envelope `RunID` is **optional** (`*RunID`, `omitempty`, `event.go:77`) and is stamped **only by `EmitWithRunID`** — daemon-level events emitted via plain `Emit` carry no envelope `run_id` at all. Meanwhile payloads embed their own run_id string (e.g. `workloopRunStartedPayload.RunID` at `workloop.go:5986`). A substring `grep <uuid>` conflates the two and misses events where the run_id lives only in the payload or is absent from the envelope. The correct query is a full-object structured match: `harmonik subscribe --json` (with `--since-event-id`) or `jq 'select(.run_id == "<uuid>")' .harmonik/events/events.jsonl`, or programmatically `eventbus.Filter(path, runID)`.

---

## 6. The registry surface the census called "production-dead" — CONFIRMED

Grep results for **non-test** consumers:

| Symbol | Definition | Non-test consumers found |
|---|---|---|
| `Event.DecodePayload` | `eventregistry.go:218` | **Only** `eventdispatch.go:54` and `eventdispatch.go:76` |
| `DispatchObservational` / `DispatchSynchronous` | `eventdispatch.go:52`, `:77` | **None** (grep returned only the doc-comment mention at `eventregistry.go:216`) |
| `ValidateEnvelopeSchemaVersion` | `eventregistry.go:165` | **None** — every hit is a doc comment or the definition itself |
| `LookupPayloadCompatEntry` / `AllPayloadCompatEntries` | `pertypecompat_hqwn38.go:373`, `:384` | **None outside the file's own test** |

**Conclusion (verbatim from the grep):** The **typed-decode + version-validate half of the registry is production-dead.** `DecodePayload` is called only by the two `Dispatch*` helpers, and those helpers have **no production caller** — only tests exercise them. `ValidateEnvelopeSchemaVersion` has **zero** callers. The entire per-type compat table (`pertypecompat_hqwn38.go`) is consumed only by its own conformance test.

**What IS live from the registry:** `RegisterEventType` (populates the map at init), `LookupTypeSchemaVersion` — used at `busimpl.go:441` to **stamp** the envelope `schema_version` at emit time — and `ScanRegisteredPayloadsForSecretFields` (EV-036 startup guard). In other words: the registry is used to *write* (stamp version + secret-scan), but nothing in production ever *reads events back through the typed decode path*. Every production reader (§5) does its own `json.Unmarshal` on the envelope + selective payload decode, bypassing `DecodePayload` entirely.

**Implication for record→replay:** the read side you would build on is `ScanAfter`/`Filter` + ad-hoc payload unmarshal, NOT the `DecodePayload`/`DispatchObservational` machinery. The latter would need to be revived (or is available dead-code to adopt) if replay wants type-safe payload decoding + schema-version assertion — which is exactly the invariant-checking the revamp wants, and it is currently unused.

---

## 7. Dark spots (subsystems emitting NO events) — VERIFIED

### Remote / workspace materialization: **emits zero events** ✔

`grep -rn "\.Emit(|EmitWithRunID|EmitTyped|emitter" internal/workspace/` (non-test) returns **nothing**. The remote-materialization file `internal/workspace/remotematerialize.go` has **no reference to `Emit`, `emitter`, `EventBus`, or `bus`** at all. The entire `internal/workspace/` package (the remote/worktree subsystem) is event-dark — it does its work without emitting run-observable events. Confirms the synthesis claim that remote "currently emits none." `workspace_created/leased/discarded/interrupted/merge_status` types are *declared* (§8.5) but are emitted from elsewhere (the daemon workloop), not from the workspace package itself.

### Keeper: **outer cycle emits; interior steps are transient** ✔ (partially dark)

`internal/keeper/` **does** emit — but only coarse cycle-boundary events, all via `EmitWithRunID(ctx, core.RunID{}, ...)` with an **empty/zero run_id** and **discarded error** (`_ =`):

- `cycle.go:1421` `session_keeper_idle_crew`, `:1484` `..._precompact_blocked`, `:1586` `..._handoff_started`, `:1598` `..._cycle_complete`, `:1610` `..._cycle_aborted`, `:1621` `..._clear_unconfirmed`, `:1632` `..._cycle_recovered`
- `awaitack.go:204` `session_keeper_ack_timeout`
- The emitter is an injected minimal interface (`watcher.go:22-24`): just `EmitWithRunID`.

So the keeper emits at **cycle transitions** (handoff started / cycle complete / aborted / recovered), but the **interior mechanics of a restart** — gauge polling, the actual `/clear` keystroke injection, the `/session-resume` drive, pane-liveness probing — produce **no events**; they are transient in-process state. And because keeper events carry `RunID{}` (zero), they are **not** joinable by run_id — they are session/agent-scoped, not run-scoped, and are invisible to `Filter(path, runID)`.

### Net dark-spot picture for the revamp

- **Fully dark:** `internal/workspace/` (remote + worktree materialization).
- **Coarsely instrumented (interior dark):** `internal/keeper/` — cycle bookends only, zero run_id.
- **Richly instrumented:** daemon workloop (run/agent/queue/reconciliation lifecycle), sentinel, presence, watch, hitl-decisions, agent-comms.

A record→replay base layer built on the current stream would faithfully replay run/agent/queue/gate/reconciliation lifecycle, but would be **blind to remote materialization entirely** and would see keeper only as start/stop bookends.
