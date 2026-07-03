# Research/Design — Event capture & reaction loop

> Component: `event-capture-reaction`. Source: research sub-agent (opus), grounded in harmonik code, 2026-05-27.

## TL;DR
- The loop is a **deterministic event router that wakes the LLM only on a small allowlist of "needs-judgment" events**; ~90% of harmonik's event volume (heartbeats, agent lifecycle, happy-path run_completed, queue transitions) is handled in pure code that advances the watermark and costs zero tokens.
- Mechanism is **HYBRID**: a long-lived `harmonik subscribe` push stream (LANDED on main incl. 60s heartbeat AND `since_event_id` replay) is the primary transport; the consistency thread's watermark + reacted_ledger is the durability/effectively-once layer underneath. Polling `ScanAfter(watermark)` is the cold-start/reconnect fallback, not steady state.
- **Idle is free:** when nothing actionable happens the loop blocks on the subscribe socket; the daemon heartbeat (carrying `last_event_id` + `active_runs`) advances the watermark and confirms liveness with ZERO LLM. The LLM is woken by a debounced *digest*, never by a raw event.

## CRITICAL FACT — subscribe is LANDED (corrects the brief)
`harmonik subscribe` landed (hk-6ynv4, sha d73bdbc, 2026-05-22); `since_event_id` replay landed **2026-05-27** (hk-a5sil, sha 994c6d2). `internal/daemon/subscribe.go`: NDJSON, server-side type filtering, 60s heartbeat, `since_event_id` resume (replay-then-dedup, `:331-368`), drop-oldest shed with `subscription_gap{dropped:N}` notice (`:40-44`), heartbeat payload carries `last_event_id` (`:408,487`) + `active_runs[]` with `run_id`/`bead_id`/`age_seconds` (`:421`). **Stale doc drift:** CLI help text still says `--since-event-id` "NOT YET IMPLEMENTED" — contradicts landed code; flag a one-line fix.

## Q1 — Poll vs push vs hybrid
**Push for transport, watermark for durability.** Decouple two clocks: a transport tick (push events arrive whenever) and a separate LLM-wake clock (fires only on actionable digests). Cache warmth is a non-goal during quiet periods — let the 5-min TTL lapse; a cold cache on a genuine wake is cheap vs keeping the LLM hot for hours (optimizing warmth would require sub-5min LLM ticks = the token waste we avoid). Latency: push ~immediate vs up-to-30s poll (matters for storms). Crash-resilience: on reconnect re-subscribe with `since_event_id=<watermark>` → gap-free across context recycles (this is the gap hk-a5sil closed today; before it, the cursor was accepted-but-ignored). Advance deterministically whenever reaction is a pure function of event+ledger; wake only when reaction needs choosing among options or composing a prompt.

## Q2 — The wake filter (most important)
Partition every event type into 3 tiers (volumes from on-disk tally):
- **Tier 0 — IGNORE** (advance watermark, no action; ~70% of volume): `agent_heartbeat`(269), `agent_ready`(227), `implementer_phase_complete`(170), happy `review_loop_cycle_complete`(159), `daemon_started`/`orphan_sweep`(154), `skills_provisioned`, `session_log_location`, `launch_initiated`, `handler_capabilities`, `reviewer_launched`, `state_entered/exited`, `agent_output_chunk`, `metric`/budget_accrual. Record `reaction:noop`, advance watermark.
- **Tier 1 — DETERMINISTIC REACTION** (code, no LLM): `run_completed`(happy)→backfill queue if slot opened + advance; `queue_group_completed{complete-success}`→dispatch next group; `reviewer_verdict{APPROVE}`→noop (daemon merges); `bead_closed`→`kerf triage --ack`; subscription `heartbeat`→advance watermark to `last_event_id`, refresh `active_runs`.
- **Tier 2 — WAKE THE LLM** (bounded judgment set): `run_failed` ONLY on 2nd failure of same bead this session (1st→Tier-1 re-enqueue with `failed-once` marker); `reviewer_verdict{REQUEST_CHANGES|BLOCK}` after `iteration_cap_hit`/`review_loop_cycle_complete{cap_hit|blocked|no_progress|error}`; `merge_conflict_escalation`/stuck `workspace_merge_status`; `queue_paused{group_failure}`; queue-empty (derived: group_completed w/ no successor AND `kerf next` empty); `operator_escalation_required`, `daemon_degraded`, malformed/stale `reconciliation_*`; `iteration_cap_hit`, `no_progress_detected`. The filter is a static allowlist keyed on `(type, payload-predicate, ledger-state)`.

## Q3 — Batching / debounce
The LLM never sees raw events — it sees a **digest** the router composes after a debounce window. When a Tier-2 event arrives, open a **2s debounce** (close early if the subscribe channel drains = burst over, NTM-style condition-based close). Drain all events in the window, classify, hand the LLM ONE digest (e.g. `run_failed×2: hk-abc(2nd,no_commit), hk-def(2nd,context_cancelled) / reviewer BLOCK: hk-ghi / queue: group-2 paused / active_runs: 3`). Collapses a 10-event burst into one wake; LLM reasons over post-event *state*, reaction idempotent keyed on highest event_id (`reaction:<event_id>`). 2s latency << any human deadline and << 5-min TTL.

## Q4 — Backpressure / storms
1. **Coalesce same-class within a digest:** 10 run_failed → ONE line "run_failed×10 across [ids]"+flag, not 10 investigator dispatches; LLM decides "ten bugs vs one broken batch/daemon fault."
2. **Circuit-breaker on reaction rate:** if loop would dispatch >N investigators/re-dispatches in a rolling window (propose N=3/5min), trip: stop auto-dispatch, file ONE umbrella bead, surface to user. Encodes CLAUDE.md "never re-dispatch >twice without investigation" as a loop invariant.
3. **Transport-level shed exists:** subscribe drop-oldest + `subscription_gap{dropped:N}`. On a gap notice the loop MUST force-reconcile via `ScanAfter(watermark)` (a dropped event could be a Tier-2 it must not miss).

## Q5 — The quiet problem (idle = free)
When nothing actionable: loop **blocks on the subscribe socket** — zero CPU, zero tokens. Heartbeat (60s) does 3 jobs free: (1) liveness ("daemon alive, nothing to do", no wake); (2) watermark advance during quiet (payload `last_event_id`) → crash during a quiet hour resumes correctly; (3) stall detection (payload `active_runs[].age_seconds`) → if a run crosses a stall threshold w/ no terminal, promote to synthetic Tier-2 "run X stalled" — catches silent-hang without polling. If heartbeat itself stops (>2 missed = >120s) → daemon dead → reconnect with `since_event_id=<watermark>`; persistent failure = Tier-2 "daemon down."

## Q6 — Harmonik-specific
Subscribe is the primary path now (no interim needed). Over `tail -F`+ScanAfter it gives: server-side type filtering (less router work), 60s heartbeat (liveness + quiet-watermark-advance + active_runs stall data), gap-free reconnect (replay-then-dedup). `ScanAfter(watermark)` remains the fallback for: cold start, forced re-sync after `subscription_gap`, reconnect to an older binary lacking replay.

## Open questions for user
1. **Where does the router/classifier live** — inside the cognition loop's harness (LLM session) OR a thin Go pre-filter (`harmonik subscribe --wake-filter`) so Tier-0/1 never reach the LLM context? Proposal: Go-side classifier — **the single biggest token-economy lever** (otherwise the loop pays context cost reading-and-discarding telemetry). Build decision, not runtime-resolvable.
2. Debounce 2s and circuit-breaker N=3/5min are guesses — tune or make config.
3. Stale CLI help (`--since-event-id` says unimplemented) — confirm a one-line fix.
