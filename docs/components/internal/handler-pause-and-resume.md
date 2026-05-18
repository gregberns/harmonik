# Handler Pause-and-Resume

> **Status:** design (knowledge-base; not yet a normative spec).
> **Owner-of-record:** harmonik daemon (queue-model + handler-contract cross-cutting).
> **Last updated:** 2026-05-18.
> **Cross-refs:** [`specs/queue-model.md`](../../../specs/queue-model.md) (queue-level pause, `paused-by-failure`), [`specs/handler-contract.md`](../../../specs/handler-contract.md) §4.5 §4.6 (sentinels, rate-limit), [`specs/execution-model.md`](../../../specs/execution-model.md) §8 (failure-class taxonomy), [`.kerf/.../phase-3-dot/04-design/D2-failure-class-placement.md`](../../../.kerf) (failure_class is top-level Outcome field).

## 1. Problem statement

- Today the daemon treats every failed bead as a per-bead FAIL: it closes the run, advances to the next dispatchable bead, and immediately re-hits the same wall when the failure is *handler-fatal* (e.g., a Claude Code rate-limit or session-token cap will fire identically on the next dispatch).
- The queue-model spec defines exactly one pause class targeted at run failures: `paused-by-failure` (QM-052), entered on `complete-with-failures`. That pause is **whole-queue** — there is no notion of pausing one handler type while another remains live.
- Phase-2 dogfooding (HANDOFF v47 §3) surfaced this as a real risk: a single Claude rate-limit observed near the head of a 50-bead wave would tomb the entire wave with FAIL records that are not actually about the work.
- Forward-looking: harmonik will host multiple handler types (`claude-code`, `codex`, `pi-handler`, …); a Claude outage must not pause Codex work, and vice-versa. The unit of pause must be the **handler type**, not the queue.
- We do not yet have a programmatic operator-status surface for "is handler X paused?", which is the minimum signal an agent-loop submitter needs in order to hold off.

## 2. Scope

### 2.1 MVH (in scope here)

1. Per-handler-type pause state, gated by a closed taxonomy of handler-fatal failure classes.
2. Daemon-side ingestion of handler outcomes → centralized policy decision → emission of `handler_paused` event.
3. In-flight bead tracking: beads that were `dispatched` for a handler at pause time stay `IN_PROGRESS`; the daemon records the freeze-list so operators can decide retry vs close.
4. Persistence in `.harmonik/handler-state.json`, atomic-write, survives daemon restart, no auto-resume on restart.
5. Operator CLI: `harmonik handler status [--type T]` and `harmonik handler resume <type>`.
6. Submitter-agent query API: `harmonik handler status --format=json` is the programmatic surface.
7. Log surface: WARN on pause, INFO on resume.
8. Forward-looking seam in `handler-contract` for per-handler diagnostic tooling (declared, not implemented).

### 2.2 Post-MVH (out of scope)

- Per-handler diagnostic-tool framework (run-on-pause / verify-on-resume).
- Auto-resume on timed backoff (`retry_after` derived window).
- External-trigger resume (webhook, SIGUSR1, file-marker).
- Cross-handler task transfer (e.g., reroute a Claude-Code-bound bead to Codex while Claude is paused) — research-only.
- Per-account pause inside a single handler type (e.g., pause only the rate-limited account in a Claude account pool; today the handler type as a whole pauses).
- Operator UI richer than CLI text.

## 3. Trigger taxonomy

A failure is **handler-fatal** iff: with high confidence, every subsequent invocation of the same handler will fail until external resolution. The cost of false-positive (pausing too eagerly) is a few minutes of operator action; the cost of false-negative (failing 50 beads in a queue against a known-broken handler) is corrupted work history. Skew toward pausing.

Drawing from execution-model §8 (6 classes) plus handler-contract §4.6 (rate-limit is non-fatal-per-spec but operationally handler-wide):

| §8 class | Handler-fatal? | Rationale |
|---|---|---|
| `transient` | **Conditional.** | Only the rate-limit sub-class is handler-fatal; generic transient (single DNS hiccup) is not. Distinguish via handler-contract's `agent_rate_limited` signal vs ordinary `ErrTransient`. |
| `structural` | No (per-bead) | Different beads can fail structurally for different reasons; one structural failure does not predict the next. |
| `deterministic` | No | Single-bead determinism is per-bead by definition. |
| `canceled` | No | Operator action; not a handler problem. |
| `budget_exhausted` | **Conditional.** | If the budget is per-handler-account (e.g., session-token cap, daily-quota), it is handler-fatal until reset. If it is per-run (per-node budget), it is per-bead. The `budget_scope` field on the budget point per `control-points.md §4.5` decides. At MVH only the **account-level** sub-case is handler-fatal. |
| `compilation_loop` | No | Daemon-observed traversal cap; the handler is fine. |

**MVH handler-fatal set:**

1. `transient` **with sub-reason `rate_limit`** — surfaced today as `agent_rate_limited` (handler-contract §4.6.HC-025). After two consecutive runs hit `agent_rate_limited` without intervening `agent_rate_limit_cleared`, mark the handler paused. (One isolated rate-limit may resolve within the same run; two in a row is structural.)
2. `transient` **with sub-reason `auth-expired`** — proposed new sub-reason for handler-contract; today not formally surfaced. Until added, this case rides on rate-limit's path; tracked as a follow-up.
3. `transient` **with sub-reason `api-unreachable`** — proposed new sub-reason; covers persistent 5xx / network-down patterns. Same migration story as auth-expired.
4. `budget_exhausted` **with `budget_scope = handler-account`** — session-token cap / daily-quota. The classifier knows this from the budget-point's policy attributes.

**Excluded but commonly mistaken for handler-fatal:** `ErrSkillProvisioningFailed` (per-bead config issue), `daemon_not_ready` (process-lifecycle problem, not handler), `workspace_held_by_orphan` (workspace-model concern).

**Open: hysteresis vs immediate trip.** MVH proposal: immediate trip on `budget_exhausted{handler-account}`; two-strike trip on `agent_rate_limited`. Tunable post-MVH per handler.

## 4. Event flow

```
handler subprocess
  emits agent_rate_limited (progress-stream)
     │
     ▼
watcher publishes bus event agent_rate_limited
     │
     ▼
daemon's handler-policy goroutine subscribes:
   - counts consecutive rate-limit events per agent_type
   - on trip-condition → calls HandlerPauseController.Pause(type, cause)
     │
     ▼
HandlerPauseController:
   1. acquire single-writer lock
   2. set handler_state[type].status = paused
   3. record cause = {failure_class, sub_reason, source_run_id, ts}
   4. snapshot in_flight_runs[type] = []{run_id, bead_id, ts_dispatched}
   5. persist .harmonik/handler-state.json (atomic, fsync)
   6. emit bus event handler_paused {agent_type, cause, in_flight_count}
   7. log at WARN with the bus payload
     │
     ▼
dispatch loop:
   - on every pre-dispatch eligibility check, queries HandlerPauseController.IsPaused(type)
   - if paused, skips dispatch of all items whose resolved agent_type is paused
   - emits queue_item_held_for_handler_pause once per skipped item (low-frequency, dedup'd by run_id+pause_epoch)
     │
     ▼
operator runs `harmonik handler resume claude-code`
     │
     ▼
HandlerPauseController.Resume(type):
   1. acquire single-writer lock
   2. clear handler_state[type] (status → live, cause cleared, in_flight_runs cleared)
   3. persist
   4. emit handler_resumed {agent_type, by: operator, prior_cause}
   5. log at INFO
```

In-flight runs are NOT touched by Pause — they continue until their natural terminal (success, fail, or canceled by separate operator action). The freeze-list is a *record* for operator visibility, not an interruption mechanism.

## 5. State model

### 5.1 New on-disk file: `.harmonik/handler-state.json`

Sibling to `queue.json`; same WM-026 atomic-write discipline:

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
        { "run_id": "0190b3...0042", "bead_id": "hk-ajchp",
          "dispatched_at": "2026-05-18T14:20:01.331Z" }
      ],
      "paused_epoch": 3
    },
    "codex": { "status": "live", "cause": null, "in_flight_at_pause": [], "paused_epoch": 0 }
  }
}
```

Why a new file rather than extending `queue.json`:

- `queue.json` is queue-singleton (QM-027 single active queue) and lifecycle-bound to one queue. Handler-state is daemon-singleton and orthogonal to whether a queue exists.
- A handler can be paused while no queue is active (e.g., a direct-dispatch run hit rate-limit), and a new queue submission later must observe that pause. Stuffing handler-state into the queue file would either (a) lose the pause on queue completion or (b) keep the queue file alive past its lifecycle.
- Crash-recovery is structurally simpler: load each file independently at startup.

### 5.2 Schema-versioning

- v1 only at MVH. N-1 readability per ON-018 once a v2 arrives.
- `paused_epoch` is a monotonic counter incremented on every pause → resume cycle; used by the dispatcher to dedup `queue_item_held_for_handler_pause` events.

### 5.3 Survivability

- On daemon restart, `.harmonik/handler-state.json` is read at PL-005 step 8a (same point as `queue.json`).
- Loaded `status: paused` MUST remain paused. No auto-resume across restart, mirroring QM-055.
- File-absent ⇒ all handlers default to `live`.
- File-unparseable ⇒ refuse startup with exit code 2 (forward-incompatibility), mirroring QM-002 forward-incompatible.

## 6. In-flight bead handling

When the daemon trips a pause, the dispatcher MAY already have N runs of the affected handler-type in flight. Behavior:

1. **Do not interrupt.** Sibling runs proceed per QM-034. Hard-killing them at pause time would corrupt the run-branch and leave the bead in an undefined state.
2. **Record the freeze-list.** `in_flight_at_pause` captures the run/bead/ts triple at the moment of pause.
3. **Let them terminate naturally.** If they succeed, `bead_closed` per normal path; if they fail, they may individually trip the same pause (the controller dedups by epoch).
4. **Operator-queryable via `harmonik handler status`.** Output includes the freeze-list so operators can decide: wait it out, cancel, or — post-MVH — reroute.
5. **`in_flight_at_pause` is informational.** It is not the live in-flight set; the live set is owned by the dispatcher. The freeze-list is a snapshot for the pause-cause incident.

What the daemon does NOT do at MVH:

- Does NOT reopen the in-flight beads (they may still succeed).
- Does NOT cancel them (operator's call).
- Does NOT auto-re-dispatch them on resume (each bead's status drives that via the normal dispatch path).

## 7. Resume protocol

**CLI:**

```
harmonik handler resume <agent-type> [--force]
```

Behavior:

1. Connects to the daemon over the Unix socket (process-lifecycle.md §4.4).
2. Daemon validates: handler-type known, currently paused.
3. Daemon executes Resume per the event-flow diagram.
4. CLI prints: prior cause, count of in-flight-at-pause runs, current dispatcher backlog awaiting this handler.
5. Exit 0 on success, 2 on unknown type, 3 on already-live (without `--force`), 4 on socket-unreachable.

`--force` is a no-op at MVH (reserved for post-MVH diagnostic-tool integration where Resume may want to skip a diagnostic re-check).

**What Resume does NOT do at MVH:**

- Does NOT verify the underlying issue is actually resolved (post-MVH diagnostic hook).
- Does NOT re-trigger any specific bead (the dispatcher picks up naturally on its next tick).
- Does NOT clear an active `paused-by-failure` queue state (orthogonal concern; QM-052 handles that).

## 8. Operator alerting

### 8.1 Log surface

- Pause trip: `WARN handler_paused agent_type=claude-code cause=transient/rate_limit source_run=... in_flight=2`.
- Dispatch skip (first per epoch): `INFO queue_item_held_for_handler_pause agent_type=claude-code bead_id=hk-...`.
- Resume: `INFO handler_resumed agent_type=claude-code by=operator prior_cause=...`.

### 8.2 CLI status

```
harmonik handler status                    # all handlers
harmonik handler status --type claude-code # one
harmonik handler status --format json      # programmatic surface
```

JSON shape mirrors the on-disk file plus a derived `held_count` field — the number of pending queue items whose `agent_type` resolves to this handler. Submitter agents (e.g., the kerf-driven dispatcher loop) MUST consult `status` before issuing `queue-submit` for a wave containing beads bound to a paused handler; the daemon ALSO rejects the dispatch attempt via a new validation reason `handler_paused` (this requires extending `QueueValidationReason`).

### 8.3 Programmatic query API

The CLI's `--format json` is the MVH programmatic surface. A future JSON-RPC method `handler-status` is reserved (`-32020`..`-32029` block on process-lifecycle.md §4.4 PL-003a, to be allocated when used).

## 9. Forward-looking seams

### 9.1 Per-handler diagnostic-tool hook (post-MVH)

Reserve in `handler-contract.md §4.3 Adapter`: an OPTIONAL `Diagnose(ctx) -> (DiagnosticReport, error)` method. When implemented, the controller calls it (a) on pause-trip to enrich the `cause` record, and (b) on Resume to verify the issue has cleared. MVH adapters MAY return `ErrDeterministic` ("not supported") and the controller skips. The mechanism itself is post-MVH; the seam in the interface is forward-looking design intent only.

### 9.2 Cross-handler task transfer (research-only)

A paused Claude-Code dispatcher could in principle have its queue items re-bound to a `codex`-bound equivalent node, *if* the workflow declares `agent_type` as a fallback list rather than a singleton. This is a workflow-graph-level concept (would touch `specs/execution-model.md §4.2` node attributes) and is research-only at MVH.

### 9.3 Per-account pause (post-MVH)

Today a single handler maps to a single account. A future Claude-Code adapter with account-pool rotation (handler-contract.md §4.3.HC-014 RotateAccount) could pause individual accounts rather than the whole handler type. The on-disk schema's `handlers.<type>` slot would gain a per-account map.

## 10. Gap analysis on existing `queue_paused` machinery

Already wired (verified at HEAD):

- **`specs/queue-model.md` §8.3 QM-052** — `paused-by-failure` lifecycle state, entered on group `complete-with-failures`. Whole-queue, not handler-scoped.
- **`specs/queue-model.md` §8.7 QM-056** — `QueuePauseReason` enum: `group_failure | operator_drain`. No `handler_paused` reason.
- **`internal/queue/state.go` + `internal/queue/persistence.go`** — Queue state machine, atomic-write discipline. Tested.
- **`internal/scenario/queue_paused_test.go`** (518 lines, 8 tests) — Covers (a) group → complete-with-failures transitions, (b) queue → paused-by-failure transition, (c) queue_paused event with `reason=group_failure`, (d) restart-persistence of both `paused-by-failure` and `paused-by-drain`. Does NOT cover handler-level pause (which doesn't exist yet).
- **`internal/handler/adapter_claudecode.go`** — `DetectRateLimit()` returns `(true, retry_after)` on `agent_rate_limited` events. Surfaces the signal; no policy layer above it.
- **`specs/handler-contract.md` §4.6.HC-025** — Rate-limit emission rule. Says "the daemon's policy for rate-limit handling is exponential backoff within wall-clock budget." That policy layer is the missing piece this design specifies.
- **`specs/handler-contract.md` §4.5.HC-020** — Five primary sentinels + two structural sub-sentinels. No daemon-level handler-pause policy.

Missing (this design's delivery):

1. **No handler-scoped pause concept anywhere.** Queue-level pause exists; handler-level doesn't.
2. **No `handler_paused` / `handler_resumed` events.** Event-model.md §8.3 has agent-rate-limit events but no handler-pause cohort.
3. **No `.harmonik/handler-state.json` schema.** Persistence file does not exist.
4. **No policy goroutine.** The watcher publishes `agent_rate_limited`, the rate-limited event is logged, no further policy is applied. The "exponential backoff within wall-clock budget" referenced by HC-025 has no implementation locus today.
5. **No dispatcher hook to skip-on-handler-paused.** Dispatcher only consults queue `status`.
6. **No CLI surface `harmonik handler [status|resume]`.** `cmd/harmonik/main.go` has no `handler` subcommand.
7. **No QueueValidationReason for handler-paused submissions.** Submits go through even if the bead's handler is paused.
8. **No freeze-list mechanism.** When pause arrives, in-flight beads are tracked only by the live dispatcher map (which is not persisted and not queryable).
9. **No Adapter.Diagnose() seam.** Forward-looking; nothing reserved.

## 11. Open questions deferred

1. **Exact hysteresis for `agent_rate_limited`.** Two-strike is the starting rule. Should it be N-strikes-in-T-window? Tunable per handler-type? Deferred.
2. **`auth-expired` and `api-unreachable` sub-reasons** are mentioned in §3 but do not exist as formal sentinels today. Either add them to `handler-contract` §4.5 as `ErrTransient` sub-reasons, or ship MVH against only `rate_limit + budget_exhausted{handler-account}` and add the others when first encountered.
3. **Budget-scope discrimination.** `budget_exhausted{handler-account}` requires the budget-point policy to declare `budget_scope`. That field does not exist in `control-points.md §4.5`. Either add it or rely on a heuristic from the budget name. Deferred to control-points amendment.
4. **Does `handler_paused` halt a `queue_paused-by-drain`?** Open: if a queue is mid-drain and a handler trips a pause, do we want to record the handler pause anyway? Proposal: yes — the handler pause persists across drain/restart and applies when the queue resumes. Confirm in pass-5.
5. **Operator UX for the freeze-list.** Should `handler resume` offer to `--force-fail` the freeze-list beads in one step? At MVH no — separate the surfaces, keep resume cheap.
6. **Programmatic query channel.** CLI `--format json` is MVH. Is JSON-RPC `handler-status` the post-MVH path, or do we promote it via an HTTP status server? Deferred to operator-nfr.
7. **Interaction with reconciliation Cat 3a (workflow lock).** A paused handler's in-flight beads, if they fail later, run through reconciliation. The reconciliation investigator should NOT redispatch them while the handler is paused. Proposal: reconciliation reads handler-state on startup and respects pauses. Confirm in pass-5.

---

## Appendix A: Proposed spec edits (not yet applied)

### A.1 `specs/queue-model.md`

**Add new sub-section §8.3a — QM-052a Pause-by-handler interaction (between §8.3 and §8.4):**

> The daemon-scoped handler-pause machinery (see [docs/components/internal/handler-pause-and-resume.md]) is orthogonal to `paused-by-failure`. When a handler-type is paused, the dispatcher SKIPS pending items whose resolved `agent_type` matches the paused handler; it does NOT transition `Queue.status` to `paused-by-failure` on this account. Held items remain `ItemStatus: pending`; the daemon emits at most one `queue_item_held_for_handler_pause` event per (item, paused_epoch). The queue's overall `status` continues to reflect run-failure conditions per QM-052 only.

**Extend §6.10 QM-029 `QueueValidationReason` enum:**

> Add `handler_paused` — returned by `queue-submit` when any bead in the request resolves to a currently-paused `agent_type`. Maps to JSON-RPC error code `-32018` (currently reserved). Payload includes `agent_type` and the bead IDs that would dispatch to it.

**Extend §A.3 v0.1 deferred surface:** add note that `handler-resume` and `handler-status` are operator-level surfaces outside queue-model.

### A.2 `specs/handler-contract.md`

**Add new sub-section §4.5a — HC-020a Handler-fatal classification (after §4.5):**

> Certain failure classes are HANDLER-FATAL: they indicate that every subsequent invocation of the same `agent_type` will fail until external resolution. The closed handler-fatal set at MVH is: (i) `transient` with `agent_rate_limited` observed two times consecutively without an intervening `agent_rate_limit_cleared`, and (ii) `budget_exhausted` whose underlying budget point declares `budget_scope = handler-account`. The daemon's handler-pause controller (see [docs/components/internal/handler-pause-and-resume.md]) is the policy-layer consumer of these signals; this spec is normative for the signal-emission, not for the controller's behavior.

**Add new sub-section §4.3a — HC-014a Diagnostic seam (after §4.3 Adapter):**

> An Adapter MAY implement `Diagnose(ctx) -> (DiagnosticReport, error)` as a forward-looking seam. At MVH this method is not invoked by the daemon; post-MVH the handler-pause controller MAY invoke it on pause-trip and on resume to enrich the pause `cause` record and verify resolution. Adapters not implementing it MUST return `ErrDeterministic`. The `DiagnosticReport` shape is reserved for post-MVH; no MVH consumer.

### A.3 `specs/execution-model.md`

**Add an INFORMATIVE note at the end of §8 (Error and failure taxonomy):**

> INFORMATIVE: Of the six classes in this section, two carry handler-fatal sub-cases in the daemon's handler-pause policy: `transient` (specifically `agent_rate_limited` observed twice consecutively) and `budget_exhausted` (when the underlying budget point declares `budget_scope = handler-account`). Classification authority remains in this spec (§8); the handler-pause controller is a downstream policy consumer described in [docs/components/internal/handler-pause-and-resume.md]. The taxonomy in §8 is unchanged by handler-pause.

### A.4 `specs/event-model.md` (§8 — out of scope for this design but flagged)

Three new event types to register:

- `handler_paused` (Class F — durability follows the queue-pause class).
- `handler_resumed` (Class F).
- `queue_item_held_for_handler_pause` (Class O — observability; deduplicated per (item, paused_epoch)).

Payload schemas left to a follow-on event-model amendment under the handler-pause beads.
