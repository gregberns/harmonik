# execution-model — Change Design

## Current state

`specs/execution-model.md` is single-run-shaped. The dispatch loop's normative form is §7.4 (`specs/execution-model.md:1021-1089`), declared "the single source of truth for how a run is created, dispatched, advanced, and terminated" (`:1023`). The MVH-era assumptions baked into §7.4:

- **`:1037`** — `ready_beads = beads.ready_work_query()` reaches into BI-013 (`specs/beads-integration.md:253-258`). This is the dispatch consumer that retires under extqueue.
- **`:1040`** — `pick_one(ready_beads)  -- caller policy; MVH: oldest-first tiebreak`. Selection logic lives inside the daemon.
- **`:1034-1050`** — outer `WHILE NOT shutdown_requested()` reads as serial: `pick_one → resolve_workflow → validate → create_run → execute_workflow → finalize_run` with no fork/spawn step. Parallelism has been grafted on by the implementation (workloop.go) with **no §7.4 anchor**.

The implementation has run ahead of spec on two unspec'd concurrency primitives (findings §Q3):

- **Claim semaphore** (`internal/daemon/workloop.go:360-370`) — buffered-channel semaphore acquired at `:474-479`, released at `:482`. Bounds concurrent SQLite-write surface around `ClaimBead`.
- **Registry-Len capacity gate** (`internal/daemon/workloop.go:382-389`) — separate ceiling on `runRegistry.Len() >= effectiveMax`. The in-flight-bead ceiling.

Both knobs share one `effectiveMax` (the `--max-concurrent N` flag, `cmd/harmonik/main.go:158-162`); neither has any EM-NNN requirement. They are hk-e61c3.* code-only.

§7.1 (`:949-963`) is a per-run state machine. No multi-run / cohort state machine exists anywhere in the corpus (findings §Q2).

EM-015b at `:238-240` is the terminal-event-emission requirement; it is the common downstream of the three CHB-020 terminal branches and the natural attach site for additive payload fields.

## Target state

§7.4 is amended in place (preserve the function signatures `orchestrator_main_loop`, `execute_workflow`, `finalize_run`) so the change is high-leverage but bounded blast radius (findings §Risks). A new §4.11 lands the concurrency primitives. A new EM-NNN block under §4.3 lands the group-advance requirement. §7.1 gets an `INFORMATIVE` carve-out pointing to the queue-group state machine. EM-015b's payload sentence is extended with the optional queue-identity fields.

### TS-1. Amend §7.4 dispatch loop (`:1021-1089`)

Replace lines `:1037-1049` (the `ready_beads → pick_one → resolve_workflow → validate → create_run → emit run_started → execute_workflow → finalize_run` serial block) with a queue-pull block. Target pseudocode shape:

```
queue = active_queue()                          -- [queue-model.md §3 QM-002] in-memory authority loaded from .harmonik/queue.json
IF queue IS None:
    idle_wait_for_queue_submission(); CONTINUE  -- daemon polls socket, NOT br ready
IF queue.status IN {paused-by-failure, paused-by-drain, completed}:
    idle_wait(); CONTINUE                        -- no dispatch while paused
group = queue.active_group()                    -- [queue-model.md §5] head-of-active-group
IF group IS None:
    idle_wait(); CONTINUE
IF in_flight_count() >= max_concurrent:         -- §4.11 EM-NNN-A capacity gate
    wait_for_run_slot(); CONTINUE
item = group.next_dispatchable()                -- [queue-model.md §5] head-of-stream OR
                                                --   any-pending wave member with no open ledger blocker
IF item IS None:
    wait_for_group_progress(); CONTINUE          -- all items dispatched-or-deferred; group still advancing
IF ledger_blocks_open(item.bead_id):             -- [beads-integration.md §4.3 BI-005]
    mark_item(item, deferred-for-ledger-dep)
    emit_event(queue_item_deferred_for_ledger_dep, queue, group, item)
    CONTINUE
workflow = resolve_workflow(item.bead_id)
IF NOT validator.validate(workflow):
    queue.mark_item_failed(item, validation_failed); CONTINUE
acquire_claim_token()                            -- §4.11 EM-NNN-B claim semaphore
run = create_run(item.bead_id, workflow,
                 queue_id=queue.queue_id,
                 queue_group_index=group.group_index)
persist_run_id(run)                              -- BI-009 atomic claim
release_claim_token()
emit_event(run_started, run)                     -- §4.3.EM-015a; payload carries queue_id+queue_group_index
spawn_async execute_workflow(run) THEN finalize_run(run)  -- §4.11 EM-NNN-A fan-out
```

`execute_workflow` and `finalize_run` bodies are unchanged. The single material shape change is the `spawn_async … THEN …` closer, which makes parallel dispatch normative rather than implementation-grafted. The closer paragraph at `:1091` is amended to add: "the queue-pull and slot-acquire branches each correspond to [queue-model.md §5] group-advance rules and §4.11 concurrency requirements respectively."

Anchor lines for the spec writer: replace exactly `:1037-1049`; preserve `:1052-1089` verbatim except for the `emit_event(run_started, run)` line, which gains the `queue_id, queue_group_index` arguments.

### TS-2. New §4.3 EM-NNN — "Queue-group advance on all-terminal"

Land a new requirement under §4.3 (Run model), in the EM-015* neighborhood (between EM-015c at `:245` and EM-015d at `:251`). Target identifier: **EM-015f — Queue-group advance is gated on all-terminal**.

Target text directives:

- The active queue group MUST NOT advance to the next group until every item in the active group has reached terminal status (`completed` or `failed`) per [queue-model.md §5 group state machine].
- When the active group's terminal-count equals its item-count AND zero failures, the group transitions to `complete-success` and the queue advances; the daemon emits `queue_group_completed{final_status:complete-success}` then `queue_group_started` for the successor (per [event-model.md §8.10]).
- When the active group's terminal-count equals its item-count AND ≥1 failure, the group transitions to `complete-with-failures`, the queue transitions to `paused-by-failure`, and the daemon emits `queue_group_completed{final_status:complete-with-failures}` then `queue_paused`. The queue does NOT advance.
- v0.1 has no `resume` operation; `complete-with-failures` is effectively terminal for the queue (queue persists in `.harmonik/queue.json` per [queue-model.md §3 QM-001]; recovery is daemon-restart + re-submit).

Closer: `Tags: mechanism`; `Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent`.

### TS-3. Amend EM-015b (`:238-240`) — optional queue-identity fields on run_* events

Extend the second sentence of EM-015b ("The `run_failed` event payload MUST carry the failure class …") with an additive clause:

> When the run originated from a queued dispatch per [queue-model.md §4 QM-010..012], `run_started`, `run_completed`, and `run_failed` event payloads MUST additionally carry the optional `queue_id` and `queue_group_index` fields per [event-model.md §8.10]. These fields are absent for non-queued dispatch.

This is the EM-015b emission site (findings §Q5): same call-site as `run_completed`/`run_failed`, payload extended with two optional fields. No new event type; no rework of CHB-020's three-branch fan-in.

EM-015a (`:231-236`) gets the same additive clause for `run_started`'s payload list.

### TS-4. New §4.11 — Concurrency primitives

Land a new subsection immediately after §4.10 (currently `:622-690`), before §5 Invariants at `:691`. Title: "§4.11 Concurrency". Houses the previously code-only hk-e61c3.* primitives.

**EM-NNN-A — In-flight-run capacity gate.** The daemon MUST cap the number of concurrently-running runs at `max_concurrent` (the `--max-concurrent N` daemon flag, default 1). Before dispatching a queued item, the daemon MUST ensure `in_flight_count() < max_concurrent`; if the gate is closed, the daemon MUST wait for a slot to open (an existing run reaching terminal) before evaluating the next item. The gate applies uniformly across queue groups: a wave of 8 with `max_concurrent=2` runs 2 at a time within that group; advance-to-next-group does NOT happen until the wave's all-terminal condition is met per EM-015f.

Anchor: workloop.go:382-389 (registry-Len gate) becomes the implementation of EM-NNN-A.

Closer: `Tags: mechanism`; `Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent`.

**EM-NNN-B — Claim-write serialization.** The daemon MUST serialize concurrent `ClaimBead` writes (per [beads-integration.md §4.3 BI-009]) via a token-pool of size `max_concurrent`. Each dispatch path MUST acquire a token before invoking `ClaimBead` and release it after the claim write returns (success or failure). This bounds concurrent SQLite-write surface; it is distinct from EM-NNN-A's in-flight-bead ceiling because claim writes are far shorter than run lifetimes.

Anchor: workloop.go:360-370 + workloop.go:474-482 (claim semaphore) becomes the implementation of EM-NNN-B.

Closer: `Tags: mechanism`; `Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent`.

**EM-NNN-C — `max_concurrent` configuration.** The daemon MUST accept `max_concurrent` as a startup-time integer ≥ 1 (default 1) via the `--max-concurrent` flag per [process-lifecycle.md §4.X]. Runtime mutation is out of scope for v0.1. EM-NNN-A and EM-NNN-B share the same `max_concurrent` value.

Closer: `Tags: configuration`.

### TS-5. §7.1 INFORMATIVE carve-out (`:963`)

Extend the existing INFORMATIVE block at `:963` with a second sentence:

> The queue-group state machine ([queue-model.md §5]) is layered above this per-run machine: a queue item's `dispatched` corresponds to its run reaching `running`; the item's terminal (`completed`/`failed`) corresponds to the run's terminal event per §4.3.EM-015b. The two state machines do not share transitions.

This is the §7.1 INFORMATIVE-carve-out precedent (findings §Q4) — keeps readers from conflating per-run with per-group.

### TS-6. Retirement of MVH-era prose

The MVH-era `pick_one` policy comment at `:1040` ("caller policy; MVH: oldest-first tiebreak") is deleted as part of TS-1's `:1037-1049` replacement. No other deletions; `:175`'s "EXACTLY ONE workflow … EXACTLY ONE input" is per-run-scoped and survives unchanged (findings §Q6).

## Rationale

- **TS-1** retires the `ready_beads → pick_one` shape (D1's "queue is the input"; problem-space goal "daemon has no internal selection logic") and replaces it with a head-of-active-group pull (D1's wave+stream primitives, dispatched by [queue-model.md §5]). The `spawn_async … THEN …` closer makes parallel dispatch normative (problem-space constraint "past MVH; parallelism shipped"), retiring findings §Q6's MVH-era serial-loop assumption.
- **TS-2** anchors D2 ("group-advance is gated on all-terminal") and D3 ("failure does not silently halt or auto-retry; queue pauses"). v0.1 has no `resume`; D6 says ship the minimum.
- **TS-3** anchors the cross-cutting decision to drop `queue_item_completed`/`queue_item_failed` events and rely on optional fields on existing `run_*` events. Attach site is EM-015b/EM-015a per findings §Q5 — single call-site, no new event types, no CHB-020 disturbance.
- **TS-4** addresses findings §Q3's risk that hk-e61c3.* remains code-only. Spec-first discipline requires both primitives have EM-NNN anchors before the queue's parallel-groups concept sits on top of them. EM-NNN-A and EM-NNN-B are kept distinct because they bound different surfaces (registry-Len gate vs. claim-write throttle); collapsing them would obscure the fact that claim writes serialize independently of run lifetimes.
- **TS-5** addresses findings §Q4's pattern: §7.1 is per-run, the new group SM is per-group; the corpus has no multi-state-machine prior art, so the INFORMATIVE carve-out is how we signal "two layers, not one".
- **TS-6** is bookkeeping; the deletion is implied by TS-1 but called out so the spec writer doesn't restore the comment.

## Requirements traceability

| 02-components.md §2 requirement | Amendment |
|---|---|
| Dispatch-loop input is the queue, not `br ready` | TS-1 (replace `:1037-1049`) |
| Algorithm sketch: examine active group, find head-of-eligibility, dispatch | TS-1 (pseudocode body) |
| Group-boundary advance on `complete-success` | TS-2 (EM-015f) |
| Pause on `complete-with-failures`; emit `queue_paused` | TS-2 (EM-015f bullet 3) |
| Ledger-blocking inside a group: `queue_item_deferred_for_ledger_dep` | TS-1 (pseudocode `ledger_blocks_open` branch); event payload per [event-model.md §8.10] |
| Step-9 terminal logic unchanged in shape; adds `queue_id`+`group_index` | TS-3 (EM-015b/EM-015a additive clause) |
| No `br ready` fallback — daemon refuses to dispatch when no queue loaded | TS-1 (`IF queue IS None: idle_wait_for_queue_submission`) |
| Concurrency primitives (un-spec'd, hk-e61c3.*) | TS-4 (§4.11 EM-NNN-A, EM-NNN-B, EM-NNN-C) |
| §7.1 per-run vs. per-group disambiguation | TS-5 (INFORMATIVE carve-out) |
| Retire MVH-era prose | TS-6 |

## Open for spec-draft pass (not blocking design)

- Exact EM-NNN numbers for §4.11 (suggest EM-049, EM-050, EM-051 — current §4.10 ends at EM-046b at `:690`).
- Whether `idle_wait_for_queue_submission` is a distinct named operation or folds into existing `idle_wait()`. Implementation-level; spec can use either.
- Whether EM-NNN-B's "token pool" wording should reference a concrete primitive (semaphore) or stay abstract. Recommend abstract — the buffered-channel implementation at workloop.go:360-370 is one valid implementation, not the only one.
