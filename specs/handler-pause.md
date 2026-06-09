# Handler Pause

```yaml
---
title: Handler Pause
spec-id: handler-pause
requirement-prefix: HP
status: reviewed
spec-shape: requirements-first
spec-category: runtime-subsystem
version: 0.1.0
spec-template-version: 1.1
owner: foundation-author
last-updated: 2026-05-18
depends-on:
  - architecture
  - execution-model
  - event-model
  - handler-contract
  - queue-model
  - process-lifecycle
  - operator-nfr
---
```

## 1. Purpose and scope

This spec defines the daemon-owned handler-pause subsystem: per-handler-type pause state, the closed trigger taxonomy that trips a pause, the HandlerPauseController behavioral contract, the `.harmonik/handler-state.json` persistence schema, the dispatcher-gate locus, multi-mode resume semantics, and the operator CLI surface. It is normative for every component that pauses, queries, resumes, or reasons about the liveness of a handler type.

The key design principle: handler-type pause is **orthogonal** to queue-level pause. A handler pause skips affected items in the dispatcher without transitioning `Queue.status`; the queue continues advancing for items bound to live handlers. See §8.3a (below) for the explicit cross-link.

### 1.1 In scope

- The `HandlerState` record, `HandlerStatus` enum, and `PauseCause` sub-record.
- `.harmonik/handler-state.json` persistence: write discipline, startup read, schema versioning.
- The closed handler-fatal trigger taxonomy and hysteresis rules.
- The `HandlerPauseController` behavioral contract (Pause / IsPaused / Resume operations).
- Dispatcher-gate locus: pre-dispatch eligibility check in the daemon work-loop.
- Freeze-list semantics: snapshot of in-flight runs at pause-trip time.
- Operator CLI: `harmonik handler status` and `harmonik handler resume`.
- Submitter-agent programmatic query API.
- Log surface obligations.
- Forward-looking seams: diagnostic-tool hook, cross-handler transfer (declared, not implemented).

### 1.2 Out of scope

- Per-handler diagnostic-tool framework (run-on-pause / verify-on-resume) — post-MVH; seam declared in §9.1.
- Auto-resume on timed backoff (`retry_after` derived window) — post-MVH.
- External-trigger resume (webhook, SIGUSR1, file-marker) — post-MVH.
- Cross-handler task transfer — research-only; seam declared in §9.2.
- Per-account pause within a single handler type — post-MVH; seam declared in §9.3.
- `handler-status` JSON-RPC method — post-MVH; error-code block reserved at §8.3 (process-lifecycle §4.4 PL-003a `-32020..−32029`).
- Operator UI richer than CLI text.

## 2. Glossary

- **handler type** (`agent_type`) — the string key identifying a handler implementation (e.g., `claude-code`, `codex`). One HandlerState entry per handler type.
- **handler-fatal** — a failure class (or sub-case thereof) where, with high confidence, every subsequent invocation of the same handler type will fail until external resolution.
- **pause-trip** — the moment the HandlerPauseController transitions a handler from `live` to `paused`.
- **paused_epoch** — a monotonic counter incremented on every pause→resume cycle; used by the dispatcher to deduplicate `queue_item_held_for_handler_pause` events per (item, epoch).
- **freeze-list** — the `in_flight_at_pause` snapshot: the set of run_id/bead_id/ts triples that were in-flight at pause-trip time. Informational; not the live in-flight set.
- **dispatcher gate** — the pre-dispatch eligibility check inside the daemon work-loop where `HandlerPauseController.IsPaused(agent_type)` is evaluated and, if true, the item is held rather than dispatched.

## 3. Data model

### 3.1 RECORD HandlerState

```
RECORD HandlerState:
  status            : HandlerStatus       -- current liveness of this handler type
  cause             : PauseCause | null   -- populated while status == paused; null when live
  in_flight_at_pause: List<InFlightEntry> -- freeze-list snapshot; populated at pause-trip; cleared on resume
  paused_epoch      : Integer             -- monotonic; 0 = never paused; incremented at every pause-trip
```

### 3.2 ENUM HandlerStatus

```
ENUM HandlerStatus:
  live    -- handler is available for dispatch; default on startup when file absent
  paused  -- handler is paused; dispatcher skips items bound to this agent_type
```

### 3.3 RECORD PauseCause

```
RECORD PauseCause:
  failure_class  : String    -- one of the EM-§8 six classes: "transient" | "budget_exhausted" | ...
  sub_reason     : String    -- fine-grained classifier: "rate_limit" | "budget_exhausted_handler_account" | ...
  source_run_id  : RunID     -- the run that tripped the pause
  source_bead_id : BeadID    -- the bead that tripped the pause
  tripped_at     : Timestamp -- ISO 8601 with ms, UTC
```

### 3.4 RECORD InFlightEntry

```
RECORD InFlightEntry:
  run_id        : RunID
  bead_id       : BeadID
  dispatched_at : Timestamp  -- ISO 8601 with ms, UTC
```

### 3.5 On-disk file: `.harmonik/handler-state.json`

Schema version 1. Sibling to `queue.json`; same WM-026 atomic-write discipline (temp-file + `rename(2)` + `fsync(parent_directory_fd)`):

```json
{
  "schema_version": 1,
  "handlers": {
    "claude-code": {
      "status": "paused",
      "cause": {
        "failure_class": "transient",
        "sub_reason": "rate_limit",
        "source_run_id": "0190b3...",
        "source_bead_id": "hk-cd92e",
        "tripped_at": "2026-05-18T14:22:11.482Z"
      },
      "in_flight_at_pause": [
        {
          "run_id": "0190b3...0042",
          "bead_id": "hk-ajchp",
          "dispatched_at": "2026-05-18T14:20:01.331Z"
        }
      ],
      "paused_epoch": 3
    },
    "codex": {
      "status": "live",
      "cause": null,
      "in_flight_at_pause": [],
      "paused_epoch": 0
    }
  }
}
```

**HP-001 — File absence default.** When `.harmonik/handler-state.json` is absent on startup, the daemon MUST treat all handler types as `live`. No file is created until the first pause-trip or a daemon restart after one.

**HP-002 — Forward-incompatible schema.** A `schema_version` value not in the supported-read-set `{1}` MUST refuse daemon startup with exit code 2, mirroring [/Users/gb/github/harmonik/specs/queue-model.md §3.2 QM-002]. The daemon MUST log the unsupported version value before exiting.

**HP-003 — Unparseable file.** A file present but unparseable as valid JSON MUST refuse daemon startup with exit code 2 (same policy as HP-002). The daemon MUST NOT silently treat the file as absent.

**HP-004 — Atomic write.** Every mutation to handler-state (pause-trip, resume) MUST be persisted via atomic-write (temp file + `rename(2)`) followed by `fsync(parent_directory_fd)`, mirroring [/Users/gb/github/harmonik/specs/queue-model.md §3.1 QM-001].

**HP-005 — Separation from queue.json.** Handler-state MUST NOT be embedded in `queue.json`. Handler-state is daemon-singleton; queue-state is queue-singleton. A handler can be paused while no queue is active; stuffing handler-state into the queue file would lose the pause on queue completion (see §3.5 rationale in [docs/components/internal/handler-pause-and-resume.md]).

## 4. Identity

### 4.a Subsystem envelope

#### HP-ENV-001 — Envelope declaration

Envelope for the handler-pause subsystem per [/Users/gb/github/harmonik/specs/architecture.md §4.0 AR-053]. The handler-pause subsystem is a daemon-owned, singleton, orchestrator-side subsystem; it owns the `HandlerState` records, `.harmonik/handler-state.json` persistence, the `HandlerPauseController` interface, the dispatcher gate, the operator CLI surface, and the programmatic query API.

(a) Events produced:
  - `handler_paused` — emission rule §7.1 HP-030; payload schema in [/Users/gb/github/harmonik/specs/event-model.md §8.11.1]. Class F. Fsync-backed before `HandlerPauseController.Pause()` returns.
  - `handler_resumed` — emission rule §7.3 HP-040; payload schema in [/Users/gb/github/harmonik/specs/event-model.md §8.11.2]. Class F. Fsync-backed before `HandlerPauseController.Resume()` returns.
  - `queue_item_held_for_handler_pause` — emission rule §6 HP-025; payload schema in [/Users/gb/github/harmonik/specs/event-model.md §8.11.3]. Class O. Deduplicated per `(bead_id, paused_epoch)`.

(b) Events consumed:
  - `agent_rate_limited` — published by the daemon watcher from handler progress-stream; the handler-policy goroutine subscribes to count consecutive occurrences per `agent_type` per §5.2 HP-015.
  - `agent_rate_limit_cleared` — resets the consecutive-count hysteresis per §5.2 HP-015.
  - `run_completed`, `run_failed` — the handler-policy goroutine may consume these to update live in-flight tracking (implementation detail; not normative here).

(c) Types introduced (cross-subsystem):
  | Type | `Tags:` | `Axes:` (if non-baseline) |
  |---|---|---|
  | `HandlerState` (§3.1) | mechanism | baseline |
  | `HandlerStatus` (§3.2 ENUM) | mechanism | baseline |
  | `PauseCause` (§3.3) | mechanism | baseline |
  | `InFlightEntry` (§3.4) | mechanism | baseline |
  | `HandlerPauseController` (§7) | mechanism | baseline |
  | `handler_paused` event type (co-owned with [event-model.md §8.11]) | mechanism | baseline |
  | `handler_resumed` event type (co-owned with [event-model.md §8.11]) | mechanism | baseline |
  | `queue_item_held_for_handler_pause` event type (co-owned with [event-model.md §8.11]) | mechanism | baseline |

(d) Handlers implemented: none. The handler-pause subsystem does not expose a handler-contract handler. The CLI surface (`harmonik handler status`, `harmonik handler resume`) is carried over the daemon's Unix socket per [/Users/gb/github/harmonik/specs/process-lifecycle.md §4.4 PL-003a].

(e) State owned:
  - `HandlerState` records (§3.1) — daemon-singleton; in-memory authority, one entry per known handler type.
  - `.harmonik/handler-state.json` (§3.5) — on-disk crash-recovery sync; atomic-write per HP-004.

(f) Control points provided: none. The handler-pause subsystem is a mechanism-tagged subsystem; its operations are not gate/hook/guard/budget points per [/Users/gb/github/harmonik/specs/control-points.md §4.1]. The dispatcher gate (§6) is an internal eligibility check, not a control-points gate primitive.

(g) NFRs inherited / overridden:
  - Inherited: `ON-018` N-1 schema compatibility — HP-002 applies it to `handler-state.json`'s `schema_version`.
  - Inherited: `ON-027` graceful-shutdown ordering — handler-pause state persists across `paused-by-drain` transitions; a handler paused at shutdown remains paused on restart per HP-008.
  - Overridden: none.

(h) Boundary classification per operation:
  | Operation | `Tags:` | Axes |
  |---|---|---|
  | `handler_pause_trip` (§7.1 HP-030) | mechanism | `llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent` |
  | `handler_resume` (§7.3 HP-040) | mechanism | `llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent` |
  | `handler_status_read` (§8.2) | mechanism | `llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent` |
  | `dispatcher_gate_check` (§6 HP-025) | mechanism | `llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent` |
  | `persist_handler_state` (§3.5 HP-004) | mechanism | `llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent` |
  | `startup_load_handler_state` (§3.5 HP-001..HP-003) | mechanism | `llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent` |

Tags: mechanism

## 5. Trigger taxonomy

### 5.1 Handler-fatal definition

**HP-010 — Handler-fatal criterion.** A failure class (or sub-case thereof) is handler-fatal iff, with high confidence, every subsequent invocation of the same handler type will fail until external resolution. The cost of false-positive (pausing too eagerly) is bounded operator action; the cost of false-negative (failing N beads in a queue against a known-broken handler) is corrupted work history. The policy MUST skew toward pausing.

Drawing from [/Users/gb/github/harmonik/specs/execution-model.md §8] six failure classes and [/Users/gb/github/harmonik/specs/handler-contract.md §4.6 HC-025]:

| §8 class | Handler-fatal? | Rationale |
|---|---|---|
| `transient` | **Conditional** | Only the rate-limit sub-class is handler-fatal; generic transient (single DNS hiccup) is not. |
| `structural` | No | Different beads can fail structurally for independent reasons; one failure does not predict the next. |
| `deterministic` | No | Single-bead determinism is per-bead by definition. |
| `canceled` | No | Operator action; not a handler problem. |
| `budget_exhausted` | **Conditional** | Handler-fatal only when the budget is per-handler-account (session-token cap, daily-quota). Per-run budget exhaustion is per-bead. |
| `compilation_loop` | No | Daemon-observed traversal cap; the handler itself is not broken. |

### 5.2 MVH handler-fatal set

**HP-011 — Rate-limit hysteresis trip.** A handler type MUST be paused when the daemon observes two consecutive `agent_rate_limited` events for that `agent_type` without an intervening `agent_rate_limit_cleared`. Rationale: one isolated rate-limit may resolve within the same run; two in a row indicates structural handler-wide saturation. The consecutive-count is per `agent_type` and is reset to zero on every `agent_rate_limit_cleared` or on a successful run completion for that handler type.

**HP-012 — Account-budget exhaustion trip.** A handler type MUST be paused immediately (no hysteresis) when a `budget_exhausted` failure is observed with `budget_scope = handler-account`. Per-run budget exhaustion (per-node budget) is NOT handler-fatal and MUST NOT trip a handler pause.

> NOTE: `budget_scope = handler-account` denotes the Budget primitive's `scope` field carrying value `handler_account` per [/Users/gb/github/harmonik/specs/control-points.md §4.5 CP-022] (there is one field, `scope`, not a parallel `budget_scope` field). The producer of the qualifying account-scoped `budget_exhausted` is the cognition loop's unified per-day spend meter ([/Users/gb/github/harmonik/specs/cognition-loop.md §4.11 CL-090]) — the "daily-quota" case named in the §8 classification table. The end-to-end exhaustion path is documented in §11a.

**HP-013 — MVH exclusions.** The following failure signals MUST NOT trip a handler pause:
- `ErrSkillProvisioningFailed` — per-bead config issue.
- `daemon_not_ready` — process-lifecycle concern, not handler.
- `workspace_held_by_orphan` — workspace-model concern.
- Any `structural` or `deterministic` failure class regardless of sub-reason.

**HP-014 — Post-MVH sub-reasons (deferred).** `auth-expired` and `api-unreachable` are recognized handler-fatal sub-cases but are not formally surfaced as handler-contract sentinels at MVH. Until added to [/Users/gb/github/harmonik/specs/handler-contract.md §4.5], these cases ride the rate-limit hysteresis path via `agent_rate_limited`. Addition is tracked as a follow-up.

**HP-015 — Hysteresis is per epoch.** The consecutive-rate-limit counter MUST reset to zero at every resume (i.e., on a new epoch). Stale counts from a prior epoch MUST NOT carry forward.

## 6. Dispatcher gate

**HP-020 — Gate locus.** The dispatcher gate for handler-pause is the **pre-dispatch eligibility check inside the daemon work-loop**, evaluated before any `LaunchSpec` is issued for an item. This is NOT a queue-level state transition; `Queue.status` is not modified by the handler-pause gate. The gate is orthogonal to the queue-model state machine.

**HP-021 — Gate check.** On every iteration of the dispatch work-loop, for each item candidate, the daemon MUST call `HandlerPauseController.IsPaused(agent_type)`. If the result is `true`, the item MUST NOT be dispatched in that iteration.

**HP-022 — Held item status.** An item skipped by the dispatcher gate remains at `ItemStatus: pending`. The queue continues advancing items bound to live handler types; the held item resumes eligibility when `IsPaused(agent_type)` returns `false` (i.e., after operator resume).

**HP-023 — Held-event deduplication.** The daemon MUST emit at most one `queue_item_held_for_handler_pause` event per `(bead_id, paused_epoch)`. Subsequent dispatch-loop iterations that continue to skip the same item within the same pause epoch MUST NOT emit additional events.

**HP-024 — No automatic re-dispatch on resume.** When a handler resumes, the dispatcher picks up naturally on its next tick. There is no forced re-scheduling or priority bump for previously held items.

**HP-025 — Submission-time validation.** The daemon's `queue-submit` and `queue-append` validation MUST check `HandlerPauseController.IsPaused(agent_type)` for each item in the request. If any item resolves to a paused handler type, the request MUST be rejected with `QueueValidationReason: handler_paused`. The rejection payload MUST include `agent_type` and the list of bead IDs that would dispatch to the paused handler. This requirement cross-references [/Users/gb/github/harmonik/specs/queue-model.md §6.11a QM-029b].

## 7. HandlerPauseController behavioral contract

### 7.1 Pause(agent_type, cause)

**HP-030 — Pause sequence.** When the handler-policy goroutine determines a pause-trip condition is met (per §5.2), the daemon MUST execute the following sequence atomically from the perspective of other readers:

1. Acquire the HandlerPauseController single-writer lock.
2. Set `handler_state[agent_type].status = paused`.
3. Record `cause = {failure_class, sub_reason, source_run_id, source_bead_id, tripped_at}`.
4. Snapshot `in_flight_at_pause` from the dispatcher's live in-flight map for `agent_type` at this moment.
5. Increment `paused_epoch` by 1.
6. Persist `.harmonik/handler-state.json` per HP-004 (atomic-write + fsync).
7. Emit `handler_paused{agent_type, cause, in_flight_count, paused_epoch}` per [/Users/gb/github/harmonik/specs/event-model.md §8.11.1]. The event MUST be fsync-backed before `Pause()` returns.
8. Log at WARN: `handler_paused agent_type=<type> cause=<failure_class>/<sub_reason> source_run=<run_id> in_flight=<count>`.
9. Release lock.

**HP-031 — Idempotent Pause.** If `Pause()` is called for a handler type that is already `paused`, the call MUST be a no-op (log at DEBUG, no epoch increment, no re-emit). The existing pause state takes precedence.

### 7.2 IsPaused(agent_type) bool

**HP-035 — Read contract.** `IsPaused()` MUST be lock-free for readers (e.g., via an `atomic.Value` or RWMutex read-lock). It returns `true` iff `handler_state[agent_type].status == paused`. Unknown `agent_type` values return `false` (treated as live by default per HP-001).

### 7.3 Resume(agent_type)

**HP-040 — Resume sequence.** On operator `harmonik handler resume <agent_type>`:

1. Acquire the HandlerPauseController single-writer lock.
2. Validate: handler type is known; currently `paused`. If `status == live` without `--force`, return error code 3.
3. Capture `prior_cause` from the current `handler_state[agent_type].cause`.
4. Clear the handler state: `status → live`, `cause → null`, `in_flight_at_pause → []`.
5. `paused_epoch` is NOT reset (it is monotonic for the handler type's lifetime; used for dedup).
6. Persist `.harmonik/handler-state.json` per HP-004.
7. Emit `handler_resumed{agent_type, by: "operator", prior_cause, paused_epoch}` per [/Users/gb/github/harmonik/specs/event-model.md §8.11.2]. Fsync-backed before `Resume()` returns.
8. Log at INFO: `handler_resumed agent_type=<type> by=operator prior_cause=<failure_class>/<sub_reason>`.
9. Release lock.

**HP-041 — Resume does not verify.** At MVH, `Resume()` does NOT verify that the underlying issue is resolved. The operator is responsible for confirming the handler is operational before resuming. Post-MVH the diagnostic hook (§9.1) may add verification.

**HP-042 — Resume does not re-trigger beads.** Resume does NOT force re-dispatch of any previously held bead. The dispatcher picks up eligible items on its next tick via normal eligibility evaluation.

**HP-043 — Resume does not clear queue pause.** Resuming a handler does NOT clear a `paused-by-failure` or `paused-by-drain` `Queue.status`. These are orthogonal state machines. See §8.3a.

## 8. Persistence and survivability

### 8.1 HP-006 — Startup load sequence

The daemon MUST load `.harmonik/handler-state.json` at [/Users/gb/github/harmonik/specs/process-lifecycle.md §4.1 PL-005 step 8a], the same startup step as `queue.json`. Load order within step 8a is unspecified (the two files are independent).

### 8.2 HP-007 — Startup load outcomes

Three outcomes on attempting to read `.harmonik/handler-state.json` at startup:

1. **File absent** → all handlers default to `live` per HP-001. No error; proceed normally.
2. **File present and parseable** → apply the loaded state. Handler types with `status: paused` MUST remain paused. No auto-resume on restart.
3. **File present but unparseable or unsupported `schema_version`** → refuse startup with exit code 2 per HP-002 / HP-003.

### 8.3 HP-008 — No auto-resume on restart

A handler loaded with `status: paused` from disk MUST remain paused after daemon restart. Auto-resume on restart is explicitly forbidden, mirroring [/Users/gb/github/harmonik/specs/queue-model.md §8.6 QM-055] for queue-level pause.

### 8.3a — Orthogonality to queue-level pause (QM-052 cross-link)

**HP-009 — Handler pause is orthogonal to queue-level pause.**

This subsection is the explicit cross-link to [/Users/gb/github/harmonik/specs/queue-model.md §8.3 QM-052]:

- `QM-052 paused-by-failure` is a **whole-queue** state entered when an active group reaches `complete-with-failures`. It stops all dispatch.
- Handler-type pause (this spec) is a **per-handler-type** gate. It skips items whose `agent_type` resolves to the paused handler while items bound to live handlers continue to dispatch normally.
- When a handler type is paused, the daemon MUST NOT transition `Queue.status` to `paused-by-failure` on that account. The queue's overall `status` reflects run-failure conditions per QM-052 only.
- When a queue enters `paused-by-failure` or `paused-by-drain`, any currently-active handler-type pauses MUST persist unmodified. The handler state is daemon-scoped; queue state is queue-scoped.
- Resuming a handler (§7.3 HP-040) does NOT resume a `paused-by-failure` queue. These are independent operator actions.

**HP-009a — QueueValidationReason extension.** The `QueueValidationReason` enum is extended with `handler_paused` per HP-025. This cross-references [/Users/gb/github/harmonik/specs/queue-model.md §6.11a QM-029b] (the `handler_paused` validation reason, JSON-RPC error code `-32018`).

### 8.4 HP-016 — Schema versioning

`schema_version: 1` is the only supported version at MVH. N-1 readability per [/Users/gb/github/harmonik/specs/operator-nfr.md §4.5 ON-018] applies once a v2 is introduced: a v2 daemon MUST be able to read and migrate a v1 file. Until then no migration logic is required.

## 9. In-flight bead handling

**HP-050 — No interrupt at pause.** When the daemon trips a pause, in-flight runs for the affected handler type MUST NOT be interrupted. Sibling runs proceed per [/Users/gb/github/harmonik/specs/queue-model.md §5.7 QM-034]. Hard-killing them at pause time would corrupt the run-branch and leave affected beads in an undefined state.

**HP-051 — Freeze-list is a snapshot.** `in_flight_at_pause` captures the run/bead/ts triple at the moment of pause-trip. It is informational; it is NOT the live in-flight set. The live set is owned by the dispatcher. The freeze-list is a snapshot for the pause-cause incident, queryable via `harmonik handler status`.

**HP-052 — Natural termination.** In-flight runs at pause time MUST terminate via their natural paths: success closes the bead per normal path; individual failure may trip additional handler-pause dedup checks per HP-031 (epoch guards the re-trip).

**HP-053 — No auto-re-dispatch on resume.** The daemon MUST NOT automatically re-dispatch beads from the freeze-list on resume. Each bead's `ItemStatus` drives re-dispatch via the normal dispatch eligibility path.

**HP-054 — Freeze-list not cleared on individual run termination.** `in_flight_at_pause` is cleared only on `Resume()` (HP-040 step 4), not when individual freeze-list members reach their terminal state. This preserves the historical record for operator inspection throughout the pause epoch.

## 10. Operator surface

### 10.1 CLI

**HP-060 — Status command.**

```
harmonik handler status                     # all known handler types
harmonik handler status --type <agent_type> # one handler type
harmonik handler status --format json       # programmatic surface
```

The JSON response mirrors the `HandlerState` record per §3.1 for each handler type, plus a derived `held_count` field: the number of pending queue items whose `agent_type` resolves to this handler. `held_count` is 0 when no queue is active.

**HP-061 — Resume command.**

```
harmonik handler resume <agent_type> [--force]
```

Behavior:
1. Connects to the daemon over the Unix socket per [/Users/gb/github/harmonik/specs/process-lifecycle.md §4.4 PL-003a].
2. Daemon validates: handler type known; currently paused.
3. Daemon executes Resume per §7.3 HP-040.
4. CLI prints: prior cause, count of in-flight-at-pause runs, current dispatcher backlog awaiting this handler.
5. Exit codes: `0` success; `2` unknown type; `3` already live (without `--force`); `4` socket unreachable.

`--force` is reserved at MVH (no-op). Post-MVH it will bypass the diagnostic re-check if the diagnostic hook (§9.1) is implemented.

### 10.2 Log surface

**HP-065 — Log obligations.**

- Pause trip: `WARN handler_paused agent_type=<type> cause=<failure_class>/<sub_reason> source_run=<run_id> in_flight=<N>`.
- Dispatch skip, first per (item, epoch): `INFO queue_item_held_for_handler_pause agent_type=<type> bead_id=<id>`.
- Resume: `INFO handler_resumed agent_type=<type> by=operator prior_cause=<failure_class>/<sub_reason>`.

## 11. Event cross-reference

The three handler-pause event types are registered in [/Users/gb/github/harmonik/specs/event-model.md §8.11]:

- **§8.11.1 `handler_paused`** — Class F. Payload: `agent_type`, `cause` (`failure_class`, `sub_reason`, `source_run_id`, `source_bead_id`, `tripped_at`), `in_flight_count`, `paused_epoch`. Fsync-backed per HP-030 step 7. Emitter: daemon-core `HandlerPauseController`.
- **§8.11.2 `handler_resumed`** — Class F. Payload: `agent_type`, `by` (enum: `operator`), `prior_cause`, `paused_epoch`. Fsync-backed per HP-040 step 7. Emitter: daemon-core `HandlerPauseController`.
- **§8.11.3 `queue_item_held_for_handler_pause`** — Class O. Payload: `agent_type`, `bead_id`, `paused_epoch`. Deduplicated per `(bead_id, paused_epoch)` per HP-023. Emitter: daemon dispatch work-loop.

Emission ordering within a single pause epoch per [/Users/gb/github/harmonik/specs/event-model.md §8.11]: (a) `handler_paused` once on pause-trip, MUST precede any `queue_item_held_for_handler_pause` for that epoch; (b) zero or more `queue_item_held_for_handler_pause`; (c) `handler_resumed` once, terminates the epoch.

The handler-pause events are NOT paired-phase lifecycle events per [/Users/gb/github/harmonik/specs/event-model.md §8.9(h)]: Pause and Resume are distinct terminal-distinct outcomes with independent payload shapes.

## 11a. Unified-budget exhaustion hard-halt path (informative)

This subsection documents the end-to-end path by which the cognition-loop unified per-day spend meter halts `claude` dispatch through the existing HP-012 policy. The handler-pause controller behavior (HP-012) is unchanged; this path records how the qualifying event reaches it.

1. The unified spend meter ([/Users/gb/github/harmonik/specs/cognition-loop.md §4.11 CL-090]) sums Pi turns + daemon-spawned `claude` cost (consumed from `budget_accrual` per [/Users/gb/github/harmonik/specs/event-model.md §8.4.2]) and reaches its per-day USD cap, OR the per-day max-runs ceiling ([/Users/gb/github/harmonik/specs/cognition-loop.md §4.11 CL-090a]) is reached.
2. The meter emits `budget_exhausted{budget_scope = handler_account, spent_usd, cap_usd, ...}` into the shared event stream; the cognition loop is a registered producer of this account-scoped variant per [/Users/gb/github/harmonik/specs/event-model.md §8.4.3].
3. The registered budget-exhaustion handler-pause policy observes the event; HP-012 fires: the `claude` handler type is paused immediately (no hysteresis). No new `claude` implementer/reviewer sessions launch (held at submission-time validation per HP-025).
4. The cognition loop enters `budget-paused` per [/Users/gb/github/harmonik/specs/cognition-loop.md §6].
5. Reset is non-automatic. The operator clears the handler pause via the existing handler-resume surface (`harmonik supervise resume` / the HP-resume path of §6 in this spec, per [/Users/gb/github/harmonik/specs/operator-nfr.md §4.3]). The cognition-loop-side pause/resume *producer* (a separate agent-callable control surface) is out of scope here; this path relies only on the existing handler-resume verb to clear the budget-exhaustion handler pause.

## 12. Forward-looking seams

### 12.1 Per-handler diagnostic-tool hook (post-MVH)

**HP-070 — Diagnostic seam.** A forward-looking `Diagnose(ctx) -> (DiagnosticReport, error)` method is reserved in the `Adapter` interface per [/Users/gb/github/harmonik/specs/handler-contract.md §4.3a HC-014a]. At MVH this method is not invoked by the daemon; post-MVH the HandlerPauseController MAY invoke it (a) on pause-trip to enrich the `cause` record, and (b) on Resume to verify resolution. Adapters not implementing it MUST return `ErrDeterministic`. The `DiagnosticReport` shape is reserved for post-MVH; no MVH consumer.

### 12.2 Cross-handler task transfer (research-only)

**HP-071 — Transfer seam.** A paused handler's held items could in principle be re-bound to a fallback handler type if the workflow node declares `agent_type` as a fallback list. This is a workflow-graph-level concept (would touch [/Users/gb/github/harmonik/specs/execution-model.md §4.2] node attributes) and is research-only at MVH. No contract is reserved here.

### 12.3 Per-account pause (post-MVH)

**HP-072 — Account-pool seam.** Today one handler type maps to one account. A future adapter with account-pool rotation (see [/Users/gb/github/harmonik/specs/handler-contract.md §4.3 HC-014] RotateAccount seam) could pause individual accounts rather than the whole handler type. The `handlers.<type>` slot in `handler-state.json` would gain a per-account sub-map. Schema v2 would introduce this; v1 has no per-account fields.

### 12.4 JSON-RPC handler-status method (post-MVH)

**HP-073 — JSON-RPC seam.** A `handler-status` JSON-RPC method is reserved in the `-32020..−32029` error-code block on [/Users/gb/github/harmonik/specs/process-lifecycle.md §4.4 PL-003a]. The MVH programmatic surface is the CLI `--format json` path per HP-060. Promotion to JSON-RPC is deferred; the code block is allocated to prevent collisions.

## 13. Open questions deferred

1. **Exact hysteresis for `agent_rate_limited`.** Two-strike (HP-011) is the starting rule. Should it be N-strikes-in-T-window? Tunable per handler type? Deferred.
2. **`auth-expired` and `api-unreachable` sub-reasons** — not yet formal handler-contract sentinels. Tracked as HP-014 follow-up.
3. **Budget-scope discrimination.** ~~`budget_exhausted{handler-account}` requires `budget_scope` on the budget-point policy. That field does not exist in [/Users/gb/github/harmonik/specs/control-points.md §4.5]. Deferred to control-points amendment.~~ **RESOLVED (kerf `credfence` work).** [/Users/gb/github/harmonik/specs/control-points.md §4.5 CP-022]'s `scope` enum now includes `handler_account`; the `budget_scope = handler-account` wording of HP-012 maps to the Budget primitive's `scope` field carrying value `handler_account` (one field, not a parallel `budget_scope`). The cognition-loop unified per-day spend meter ([/Users/gb/github/harmonik/specs/cognition-loop.md §4.11 CL-090]) is the producer of the qualifying account-scoped `budget_exhausted` per [/Users/gb/github/harmonik/specs/event-model.md §8.4.3]. See §11a for the end-to-end path.
4. **Handler pause during `paused-by-drain`.** If the queue is mid-drain and a handler trips a pause, the handler pause persists across drain/restart and applies when the queue resumes. Confirm in a future pass.
5. **Reconciliation interaction.** The reconciliation investigator MUST NOT redispatch in-flight beads from the freeze-list while the handler is paused. Proposal: reconciliation reads `handler-state.json` on startup and respects pauses. Confirm in a future pass.

---

## Appendix A: Cross-spec amendments captured here

This section collects the proposed spec edits from [docs/components/internal/handler-pause-and-resume.md Appendix A] that have now been superseded by this normative spec. The amendments to the sibling specs are noted below; implementers of those specs should update them to reference `specs/handler-pause.md` (this spec) as the normative source.

### A.1 `specs/queue-model.md` amendments

Per §8.3a of this spec:
- **Add QM-052a** (between §8.3 and §8.4): orthogonality clause — handler-type pause is orthogonal to `paused-by-failure`; see HP-009 in this spec.
- **Extend QM-029 / QM-029b** (§6.10/§6.11a): add `handler_paused` to `QueueValidationReason` enum; JSON-RPC error code `-32018`; see HP-025 / HP-009a in this spec.
- **Extend §A.3 v0.1 deferred surface**: note that `handler-resume` and `handler-status` are operator-level surfaces outside queue-model scope.

### A.2 `specs/handler-contract.md` amendments

Per §5 and §12.1 of this spec:
- **Add HC-020a** (§4.5a): handler-fatal classification; references HP-010..HP-012 in this spec as the normative controller behavior.
- **Add HC-014a** (§4.3a): diagnostic seam; see HP-070 in this spec.

### A.3 `specs/execution-model.md` amendment

Per §5 of this spec:
- **Add INFORMATIVE note at end of §8**: two failure classes carry handler-fatal sub-cases in the handler-pause policy (`transient` rate-limit and `budget_exhausted{handler-account}`); classification authority remains in execution-model §8; handler-pause (this spec) is the downstream policy consumer.

### A.4 credfence amendments (budget-exhaustion hard-halt)

Captured by the kerf `credfence` work; these sibling edits make HP-012 fire from the unified per-day spend meter without a new halt path:
- **[specs/control-points.md §4.5 CP-022]**: extend the Budget `scope` enum with `handler_account` (the value HP-012 requires); resolves §13 deferred item #3.
- **[specs/event-model.md §8.4.3]**: add `cognition-loop (flywheel)` to the `budget_exhausted` producer set, with `budget_scope`/`spent_usd`/`cap_usd` optional payload fields, so the Pi-side unified meter emits the account-scoped event HP-012's consumer reads.
- **[specs/cognition-loop.md §4.11 CL-090/CL-090a]**: the unified meter is the producer of the qualifying `budget_exhausted{budget_scope=handler_account}` (see §11a path).

---

## Version history

| Version | Date | Author | Notes |
|---|---|---|---|
| 0.1.1 | 2026-05-31 | agent (kerf `credfence` work) | Budget-exhaustion hard-halt wiring. HP-012 text UNCHANGED; added a clarifying note mapping `budget_scope = handler-account` to the Budget primitive `scope` value `handler_account` ([control-points.md CP-022]) and naming the cognition-loop unified meter ([cognition-loop.md CL-090]) as the producer. Added §11a end-to-end exhaustion-path doc. Marked §13 deferred item #3 RESOLVED. Added Appendix A.4 listing the control-points / event-model / cognition-loop sibling amendments. No requirement renumbered; the existing handler-resume surface clears the budget-exhaustion pause. Source: kerf `credfence` change design. |
| 0.1.0 | 2026-05-18 | foundation-author | Initial normative elevation from [docs/components/internal/handler-pause-and-resume.md]. Added HP-### requirement IDs, §4.a subsystem envelope, §8.3a orthogonality cross-link to queue-model.md §8.3 QM-052, §11 event cross-reference to event-model.md §8.11. Design rationale retained in design doc (marked SUPERSEDED). |
