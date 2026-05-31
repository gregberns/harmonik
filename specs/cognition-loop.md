---
title: Cognition Loop
spec-id: cognition-loop
requirement-prefix: CL
status: draft
spec-shape: requirements-first
spec-category: runtime-subsystem
version: 0.1.2
spec-template-version: 1.1
owner: flywheel-author
last-updated: 2026-05-31
depends-on:
  - architecture
  - process-lifecycle
  - event-model
  - execution-model
  - queue-model
  - reconciliation
  - beads-integration
  - workspace-model
  - operator-nfr
  - claude-launchspec
  - handler-contract
  - handler-pause
  - control-points
  - credential-isolation
---

# Cognition Loop

## 1. Purpose
This spec defines harmonik's **cognition loop**: the long-running, agent-led process that supervises a harmonik daemon — prioritizing dispatch, reacting to run outcomes, investigating failures, and escalating when judgment is required. The cognition loop is the realization of the orchestrator-agent role named (and explicitly post-MVH) by [process-lifecycle.md §4.6 PL-019]; this spec promotes that role from an informative placeholder to a specified long-running process with its own lifecycle, consistency contract, and composition root.

The loop sits **above** the daemon's deterministic work-loop. The daemon work-loop is LLM-free by [process-lifecycle.md §4.6 PL-018] (a locked invariant) and remains so. The cognition layer is the only place in harmonik where judgment-class decisions consult a model. Where the two interact, the daemon's contracts (atomic Beads claim, git-as-completion-authority, idempotency-keyed adapters, reconciliation taxonomy) are the substrate; this spec adds exactly two new state artifacts (the cognition watermark and the durable notes log) and zero new locks beyond a process-singleton.

The spec exists separately from `process-lifecycle.md` because cognition is a posture decision — agent-led judgment with a deterministic floor — distinct from process-shape. PL owns the daemon's process lifecycle and the orchestrator-agent OS-level boundary; CL owns *how the agent thinks* (context lifecycle, digest discipline, reaction idempotency) and *when the harness MUST take over* (the MemGPT 70/90/100 fullness floor and the urgent-event interrupt class).

## 2. Scope
### 2.1 In scope
- The cognition-loop process: its boundary against the daemon, single-instance discipline, lifecycle owned by `harmonik supervise`.
- Architecture posture: agent-led judgment + deterministic-floor harness; the ZFC division between model-owned and code-owned decisions.
- Turn-boundary discipline: context management MUST occur only at turn boundaries; mid-turn mutation is prohibited.
- Context lifecycle: regime-B fresh context per recycle; byte-stable cacheable prefix; cache-control breakpoint; volatile digest-and-conversation tail; cache-stability runtime invariant.
- The state digest (`CL-DIGEST`): producer, inputs, determinism, placement.
- The notes log (`CL-NOTES`): durable, tool-driven, append-only kinded-entry surface that survives every reset.
- Consistency contract: watermark + reacted-ledger, two-phase done, ordering invariant, never-regress, per-reaction-class idempotency, loop-singleton lock.
- Event reaction: `harmonik subscribe` consumer contract, three-tier wake filter, urgent-interrupt class, debounce of bursts.
- Queue-pressure: WIP-limited pull, ceiling-equals-refill-target, eager pure-code refill, LLM woken only at the empty-queue boundary.
- Operator surfaces: lifecycle ownership by `harmonik supervise`; tmux-inspectability; the live status-sheet via `harmonik digest --watch`.
- Safety: per-day budget kill-switch metering BOTH the cognition layer (Pi turns) AND the execution layer (daemon-spawned `claude` sessions), reaction-rate circuit breaker, untrusted-input threat boundary.
- Credential isolation: the loop's Pi process is the sole credential holder per [credential-isolation.md §4.1 CI-001]; the daemon and its spawned `claude` children hold no credential.

### 2.2 Out of scope
- The deterministic daemon work-loop, run-reconciliation taxonomy, run-level idempotency-keyed commit/merge/close adapters — owned by [process-lifecycle.md §4.2], [reconciliation/spec.md §4.3-4.4], [execution-model.md §4.4].
- Worker-agent persistence and session resume across daemon restarts — handler-contract concerns ([handler-contract.md §4.1, §4.3]).
- The exact prompt text of the stable prefix, per-skill markdown bodies, and digest field wording — owned by [claude-launchspec.md §4] and the project-local `.flywheel/skills/*.md` library.
- The `harmonik subscribe` wire format, heartbeat schema, `since_event_id` replay surface — owned by [event-model.md §4.3, §4.5]; this spec consumes that contract.
- The substrate choice (raw Anthropic Messages API vs. Pi's `pi-agent-core` SDK) at the LLM transport layer — informative; this spec is normative on the cognition contract above transport.
- The credential-holder discipline, deny-list, scrub guarantee, and scoped-injection mechanism — owned by [credential-isolation.md §4]; this spec consumes the sole-holder rule (CI-001) and is out of scope for the scrub/injection mechanics.
- Tuning of the 70/90/100 fullness thresholds, 400 ms burst-debounce, 600 s run-stall threshold, 5 min quiet-threshold — operational constants for v0.1; revised by measurement, not by spec amendment.

## 3. Glossary
- **cognition loop** — the long-running agent-bearing process that supervises a harmonik daemon; realization of PL-019. (see §4.1)
- **harness** — the deterministic Go-or-TS code that wraps the model, enforces the fullness floor, owns the watermark, exposes the digest. Mechanism-tagged. (see §4.2)
- **turn** — one external input that drives an inner loop of N model-calls (model → tool-calls → tool-results → ...) until the model emits no more tool-calls. (see §4.4)
- **turn boundary** — the seam after `turn_end` and before the next model-call where context management MAY occur safely. (see §4.4)
- **stable prefix** — byte-identical leading prompt above the `cache_control` breakpoint: tool schemas, identity, operational instructions, skill index, glossary. Reused across recycles. (see §4.5)
- **digest** — deterministically-computed status sheet read by the model each turn; placed below the breakpoint; produced by `harmonik digest`. (see §4.6)
- **note** — durable, kinded, append-only entry written by the model via the `note` tool: `decision | hypothesis | warning | defer`. (see §4.7)
- **recycle** — turn-boundary teardown of the live conversation + reseed of a fresh context from `[stable prefix | breakpoint | digest | seed]`. (see §4.5, §4.8)
- **fullness floor** — MemGPT 70 / 90 / 100 % threshold ladder; at 100% the harness recycles whether or not the agent acted. (see §4.4)
- **watermark** — durable record `last_processed_event_id` + `reacted_ledger` at `.harmonik/cognition/state.json` that bounds event-log gap replayed on resume. (see §4.8)
- **two-phase done** — a bead is DONE only when both the `run_completed{success}` event has fired AND the `Refs: <bead-id>` trailer is present on `origin/main`. (see §4.8)
- **wake-filter** — three-tier classifier (ignore / deterministic / wake-LLM) applied to every event from `harmonik subscribe`. (see §4.9)
- **urgent class** — subset of events (currently `merge_conflict` only) that MAY abort the in-flight turn. (see §4.9)
- **digest producer** — external `harmonik digest` command (Go, pure-code, deterministic). (see §4.6)
- **unified spend meter** — the per-day governance meter that sums the loop's own Pi model-turn cost AND daemon-spawned `claude` session cost against one cap. (see §4.11)

## 4. Normative requirements

### 4.1 Process model
- **CL-001 — Separate process from the daemon.** The cognition loop MUST run as a separate OS process; MUST NOT share an address space with the daemon. Interacts via the daemon's documented surfaces only (socket per PL-003a; `harmonik subscribe`; git working tree read-only; `br`). MUST NOT call into daemon-internal Go packages. Preserves PL-018.
- **CL-002 — Single-instance lock at `.harmonik/cognition/loop.lock`.** At most one loop per project. Acquire `flock(LOCK_EX|LOCK_NB)` before `ready`. Second invocation MUST exit with `loop-locked`. Kernel-released on termination; no operator intervention required.
- **CL-003 — Lifecycle owned by `harmonik supervise`.** Spawned/supervised/torn-down by `harmonik supervise start|stop` per PL §4.9 and ON §4.3. Ad-hoc invocation is non-conformant for production.
- **CL-003a — Per-project file surface.** The loop owns `.harmonik/cognition/`: `loop.lock`, `state.json`, `notes.jsonl`, `heartbeat`, `dispatch-log.jsonl`. MUST NOT write to harmonik-owned files outside this surface (`queue.json`, `events/`, `beads-intents/`, `worktrees/`).

### 4.2 Architecture invariants
- **CL-010 — Agent-led judgment.** MUST consult the model for every judgment-class decision: prioritization overrides, root-cause investigation, escalation, batch composition with conflicts, pattern detection. Routing any of these through deterministic glue is FORBIDDEN. Harness MAY present digests/candidates/skill bodies but MUST NOT pre-select an option on the model's behalf.
- **CL-011 — Deterministic floor (MemGPT fullness ladder).** Harness MUST enforce 70/90/100 at every turn boundary:
  - At every model-call: inject context-usage % below the cache_control breakpoint. Bands: `<70` `healthy` — bare status; `≥70,<90` `warning` — soft nudge (save notes; reset_context soon); `≥90,<100` `critical` — strong nudge (reset_context THIS turn); `≥100` `emergency` — harness forces next turn (per CL-014).
  - At every turn boundary: if `≥100`, harness MUST force save-and-reseed regardless of agent action.
  - 90 % nudge emitted at most once per crossing; flag cleared on next recycle.
- **CL-012 — Turn-boundary discipline.** Context management MUST occur only at turn boundaries. MUST NOT mutate the messages array between a `tool_use` and its `tool_result`. Permitted hooks: `prepareNextTurn`/`shouldStopAfterTurn` (per-turn, MAY swap/end); `context`/`transformContext` (per-call, READ-ONLY append-only below-breakpoint shaping; MUST NOT prune live conversation); `session_before_compact` (MAY supply custom payload). `reset_context` MUST defer via flag — calling substrate-level `compact()` from `execute()` is FORBIDDEN.
- **CL-013 — Mechanism / cognition split.** Harness is mechanism-tagged (watermark, digest fetch, cache layout, fullness floor, singleton lock, event-stream tail, turn-boundary hooks). Model is cognition-tagged (prioritization, dispatch composition, investigation, escalation). Cross-tagged "harness picks the bead and tells the model" structures are FORBIDDEN.
- **CL-014 — Band-name vocabulary alignment.** The four fullness bands defined by CL-011 MUST use the names `{healthy, warning, critical, emergency}` for `{<70, ≥70<90, ≥90<100, ≥100}` respectively, replacing the prior `{nominal, soft, strong, critical}` names. Threshold values unchanged. `FullnessBand` type in §6 MUST be updated. Rationale: vendor-neutral readability alignment with the flywheel_gateway context-health vocabulary (`apps/gateway/src/services/context-rotation.ts`) without altering behavior.
- **CL-015 — Recycle is the only strategy.** The harness recognizes exactly one rotation action at the `emergency` band: fresh-start recycle per CL-024 (substrate teardown + fresh session seeded with stable prefix + digest). The gateway-vocabulary strategies `summarize_and_continue`, `checkpoint_and_restart`, and `graceful_handoff` are explicitly NON-CONFORMANT for flywheel because: (a) `summarize_and_continue` requires LLM-generated text in the deterministic floor (violates CL-INV-001) and mid-stream message-array mutation (violates CL-020); (b) `checkpoint_and_restart` transfers full `conversationHistory` into the new session, defeating the prefix-cache hit CL-021/CL-023 require, and its state-preservation role is already filled by durable notes (CL-040–042) + digest (CL-030); (c) `graceful_handoff` mutates the stable prefix with an LLM-authored handoff document (violates CL-INV-003) and presupposes multi-agent topology incompatible with CL-002. Operator-configurable strategy selection is NOT exposed at v0.1.

### 4.3 Context lifecycle
- **CL-020 — Regime-B fresh context per recycle.** Every recycle MUST start a fresh substrate session (Pi `newSession` or equiv) seeded with the deterministic digest. Incremental mid-stream trimming is FORBIDDEN at v0.1 (changes the cumulative hash and invalidates conversation cache). Recycle replays the byte-identical stable prefix → prefix cache hits at 0.1× read price from turn 1.
- **CL-021 — Cacheable prefix layout.** `[tool schemas | system: identity+safety | project goal | operational contract | skill index + glossary] -- cache_control fixed breakpoint -- [digest | conversation | fullness injection]`. Fixed breakpoint immediately after L3a. Everything above is byte-stable across the loop's lifetime AND byte-identical across recycles. Combined L0-L3a SHOULD exceed 4096 tokens (Opus min-cacheable floor); "bulked" with skill index/glossary/inline tool schemas.
- **CL-022 — Cache TTL discipline.** SHOULD use 1-hour cache TTL (Anthropic `cache_control: {type:"ephemeral", ttl:"1h"}` or equiv). 5-min default insufficient for quiet-period idle waits. 1-hr doubles write price (2×), amortized over the longer hit window.
- **CL-023 — Cache-stability runtime invariant.** MUST monitor `cache_read_input_tokens`; invariant `cache_read_input_tokens ≈ size_of_stable_prefix_in_tokens`. MUST alert (structured warning per ON-035) on sustained drop below 80% for ≥3 consecutive turns. Alert MUST name the suspect class: prefix mutated, TTL expired, model-version pinned to a non-caching variant, or breakpoint placement drifted.
- **CL-024 — Recycle is a turn-boundary operation.** Recycle sequence: (1) model called `reset_context` OR harness detected `≥100%` at `turn_end`; (2) at boundary, harness reads `keep_hint`, persists digest, tears down live conversation; (3) opens fresh substrate session seeded with byte-identical stable prefix + first-user-message digest payload; (4) prefix cache MUST be re-used (bytes above breakpoint match prior session bit-for-bit); (5) new session resumes (next batch of subscribe events or sleep). Recycle MUST NOT discard durable notes or unacknowledged decisions.

### 4.4 The state digest (`CL-DIGEST`)
- **CL-030 — Digest produced by `harmonik digest`.** External Go command, mechanism-only, no LLM. Loop MUST shell to `harmonik digest --format=json` at every turn boundary. MUST NOT compute the digest itself in substrate code; MUST NOT cache stale digests across turn boundaries. The recycle-path seed digest (per CL-024 step 3) MUST come from the same `harmonik digest` producer; a substrate-computed placeholder digest on recycle is non-conforming.
- **CL-031 — Digest inputs (deterministic only).** From durable artifacts: `queue.json`; `origin/main` git history; `events.jsonl` via `ScanAfter(watermark)`; `br ready`+`br list --status in_progress`; `notes.jsonl`; `kerf next --format=json`. MUST NOT consult the LLM, MUST NOT include LLM-generated summary text. Status sheet; if a field would require judgment, it is omitted.
- **CL-032 — Digest size budget.** Body ≤40 lines under ordinary conditions (≤10 active runs, ≤20 open notes, ≤5 unresolved decisions). When larger, MUST truncate lowest-priority entries and append `[+N more truncated]`. Model MAY request a fuller view via `harmonik digest --full` (session-local read, not part of cacheable layout).
- **CL-033 — Schema-versioned.** JSON form MUST carry `schema_version`. N-1 compatibility per EV §6.4. Loop MUST refuse a digest whose `schema_version` is unknown (forward-incompatible).

### 4.5 Notes (`CL-NOTES`)
- **CL-040 — Notes log at `.harmonik/cognition/notes.jsonl`.** Append-only; MUST NOT overwrite or compact in place. Envelope per line: `{schema_version, ts, tool_call_id, session_id, kind, refs, text, resolved_at?, resolved_by?}`. `kind ∈ {decision, hypothesis, warning, defer}`. Survives every recycle, loop restart, daemon restart.
- **CL-041 — Note resolution discipline.** Active while `resolved_at` is null. Digest MUST include all active `decision/hypothesis/warning/defer` (capped by CL-032).
  - `defer` — MAY be auto-resolved when every bead in `refs` reaches two-phase-done (CL-051). Auto-resolution writes `resolved_at`+`resolved_by="auto"` entry referring to original `tool_call_id`.
  - `hypothesis`+`warning` — MUST NOT be auto-resolved. Require explicit `note_resolve` (judgment-class observation).
  - `decision` — MUST NOT be resolved at all. Historical; surfaced while referenced beads remain open (heuristic). Archived but not deleted.
  - Log MUST NOT be rewritten or compacted at v0.1.
- **CL-042 — Tool contracts.** `note({kind, refs, text})`: harness MUST write entry AND mirror via `appendEntry` so model sees its note in current turn's history. Returns synchronously. `note_resolve({tool_call_id, reason})` appends a resolution entry. `note_resolve` on a `decision` note is a no-op + error; model SHOULD NOT call it on decisions.

### 4.7 Consistency contract (`CL-CONS`)
> §4.6 narrative-bridge: digest §4.4 precedes notes §4.5 because the digest's CL-031 inputs reference the notes log; consistency §4.7 follows so two-phase done can reference both.

- **CL-050 — Loop is a second consumer; daemon owns run-state.** MUST NOT take reconciliation locks (RC-002a), MUST NOT walk `events.jsonl` to reconstruct run state (EV §4.5), MUST NOT issue `Refs: hk-` trailer commits, MUST NOT touch `.harmonik/queue.json` or `.harmonik/beads-intents/`. Daemon owns those. Loop is a second consumer of the durable substrate; only new obligation is **idempotent reaction with its own durable watermark.**
- **CL-051 — Two-phase done.** Bead `hk-XYZ` is DONE only when BOTH:
  1. `run_completed{success}` with `bead_id = hk-XYZ` observed in `events.jsonl`.
  2. `git log origin/main --grep "Refs: hk-XYZ" --max-count=1` non-empty.
  - **Harness obligation.** The harness MUST NOT mark a bead DONE, MUST NOT auto-resolve a `defer` note referencing the bead (per CL-041), and MUST NOT advance the bead past in-flight on the strength of Condition 1 (`run_completed{success}`) alone. The harness MUST confirm Condition 2 before treating the bead as done. A deterministic-tier `run_completed` (CL-061) that advances the watermark MUST NOT, by that advance, mark the referenced bead done; the two-phase confirmation is a separate, mandatory check.
  - Condition 1 only (event without trailer on origin/main) = daemon's terminal-multi-step window ([execution-model.md §4.12 EM-052/EM-053]; push may have failed after the merge succeeded). Loop MUST treat as in-flight and re-poll Condition 2; MUST NOT advance the bead.
  - Condition 2 only (trailer without terminal event) = converse window. Loop MUST emit `loop_observed_phantom_done{bead_id}` warning and route to Tier-2 reconciliation ([reconciliation/spec.md §4.4]); MUST NOT act directly.
  - The two-phase confirmation is idempotency-keyed per CL-055 against the triggering `run_completed.event_id` via the reacted-ledger, so it is effectively-once across crashes (CL-056).
- **CL-052 — Watermark at `.harmonik/cognition/state.json`.** Min schema `{schema_version, last_processed_event_id: UUIDv7, reacted_ledger: {event_id: reaction_key}, updated_at}`. UUIDv7 ONLY (NOT byte offset / line number — both break on rotation). Atomic write per WM-026 (temp+rename+fsync(parent_dir_fd)). Torn/unparseable triggers cold-start fallback (CL-054).
- **CL-053 — Ordering invariant effect → ledger → watermark.** (1) Execute side effect; (2) append `(event_id, reaction_key)` to reacted_ledger AND fsync state.json; (3) advance `last_processed_event_id` AND fsync state.json. Step 2 MUST precede step 3 in fsync order. Crash between 1-2: effect happened but ledger doesn't record → resume reprocesses → per-class idempotency makes retry a no-op. Crash between 2-3: ledger has it but watermark unchanged → resume finds ledger entry and skips. Step 3 fsynced before step 2 is FORBIDDEN.
- **CL-054 — Watermark never regresses; corrupt-state cold start.** Persisted `last_processed_event_id` MUST never move backward in absolute UUIDv7 order. Effective watermark on resume = `max(persisted_watermark, latest_subscribe_heartbeat.last_event_id_observed)`. Torn/unparseable state.json triggers cold start: replay from JSONL floor using empty reacted_ledger as dedup authority. Cold start emits `loop_cold_start{reason}` warning (ON-035).
  > NOTE: `ScanAfter(missing_or_unparseable_event_id)` is NOT graceful — a garbage-but-parseable watermark silently skips the gap. Loop's own corrupt-state detector — NOT ScanAfter — MUST trigger cold-start. Loop's state.json reader MUST validate `last_processed_event_id` as syntactically-valid UUIDv7; parse failure ⇒ corrupt.
- **CL-055 — Per-reaction-class idempotency.** Every reaction MUST be idempotent against re-replay, keyed by class:
  | Reaction class | Idempotency key | Mechanism |
  |---|---|---|
  | File follow-up bead on `run_failed` | `reaction:<event_id>` label on bead | `br list --label reaction:<event_id>` check before `br create` |
  | Write a decision note | `tool_call_id` (UUIDv7-derived) | Append-only log; digest dedups by `tool_call_id` |
  | Dispatch a batch on empty-queue | `dispatch_intent:<event_id>:<bead_id>` in dispatch-log | Check dispatch-log AND queue.json AND `git log --grep "Refs:<bead_id>"` before submit |
  | Ack a `decision_required` | `ack:<decision_required.event_id>` in reacted ledger | Daemon-side ack is itself idempotency-keyed per EV §6.3 |
  Key MUST be deterministic from triggering event's `event_id`. Loop MUST NOT rely on daemon's idempotency-keyed adapters for own reactions.
- **CL-056 — Effectively-once across crashes.** Combining CL-053/054/055, loop MUST achieve effectively-once reaction across any single-process crash, daemon restart, machine reboot, or operator-induced restart. Acceptance scenario: "10 in flight, recycle while 4 complete and 1 fails" — loop resumes, replays exactly the gap, dispatches zero duplicates, reacts to all 4 completions and 1 failure exactly once, leaves the 5 still-running beads untouched.

### 4.8 Event reaction
- **CL-060 — `harmonik subscribe` is the event channel.** MUST consume via `harmonik subscribe --json --types <list> --heartbeat 60s`. MUST NOT tail events.jsonl directly except in cold-start path. Reconnect with exponential backoff (5s/10s/30s/30s steady). Exit-17 (daemon socket missing per PL-008a) = "daemon down"; MUST pause LLM dispatch until reconnect succeeds.
- **CL-061 — Three-tier wake filter.** Static table indexed by `event.type` + small payload discriminators. MUST NOT consult the model. New types default to **Wake-LLM** (fail-towards-judgment).
  1. **Ignore** — pure observability (no semantic effect): `state_entered`, `state_exited`, `node_dispatch_requested`. Log+discard; watermark MAY advance.
  2. **Deterministic** — handled in pure code: `run_completed{success}` for expected completion (triggers deterministic-refill), `state_entered` for routine forward transition, `heartbeat`. No model wake.
  3. **Wake-LLM** — requires judgment: `run_failed`, `reviewer_verdict{REQUEST_CHANGES|BLOCK}`, `merge_conflict`, `decision_required`, `pattern_detected`, `bus_overflow`.
- **CL-062 — Burst debounce.** Debounce window 400 ms (v0.1, revisable). Within window: all Wake-LLM events accumulate; at expiry, fresh digest is computed, a turn-input names the events (e.g. "since last turn: 3 completed [hk-x,hk-y,hk-z], 1 failed [hk-a: merge_conflict], 2 approved"); harness fires `followUp` if turn in flight, else `prompt` if idle. Urgent class (CL-063) bypasses the window. MUST NOT exceed 2 seconds.
- **CL-063 — Urgent class MAY interrupt.** Urgent set at v0.1 = `{merge_conflict}` only. On urgent event: (1) `harness.abort()`; (2) wait for substrate idle; (3) build urgent-digest naming the event; (4) `harness.prompt(urgent_digest)`. Pre-abort reasoning is discarded; branch state is invalid anyway. Non-urgent MUST NOT abort. New types default to non-urgent; promotion requires coordinated spec+test changes.
- **CL-064 — Watchdog timers.** Arm at every idle-wait boundary. On wake, inspect:
  - **Quiet:** `now - lastEventAt > 5min` AND `active_runs` non-empty → wake model w/ digest + "verify daemon alive and runs progressing".
  - **Run stall:** any `active_runs[].age_seconds > 600` → wake with stalled IDs; model's response is judgment-class.
  - **Daemon heartbeat dropped:** `now - lastHeartbeatAt > 3 × heartbeat_interval` (180s default) → flag "daemon down", pause LLM dispatch, attempt subscribe reconnect per CL-060.
  Constants tunable by operator flag.

### 4.9 Queue pressure
- **CL-070 — WIP-limited pull; ceiling = refill target.** At most `max_concurrent` runs in-flight; refill target on a freed slot = `max_concurrent`. Single configured value; presenting them separately invites structural over-aggression. Set at supervise start; MAY be revised mid-session by LLM-emitted `set_max_concurrent` tool call (subject to daemon's per-daemon ceiling PL-014a).
- **CL-071 — Eager pure-code refill on slot release.** On `run_completed`/`run_failed`/`run_canceled`, harness MUST refill eagerly without waking the model: (1) `kerf next --format=json --only=bead`; (2) for each candidate in rank order, apply CL-072 guards until non-skipped found; (3) dispatch the survivor against the active **stream** group via `harmonik queue append`; (4) if `kerf next` empty → wake the model (empty-queue is judgment-class). Eager refill MUST NOT consult the model.
  - **Dispatch surface (concrete).** The curated-dispatch path targets a `stream` group per [queue-model.md §2.4, §7 QM-040] — wave groups reject the append this requirement issues. When no queue is active, the **first fill** creates the stream group via `harmonik queue submit`; `queue-submit` returning `status: active` IS the "start" semantics (no separate start verb; [queue-model.md §8.1 QM-050]). Refill on subsequent slot releases uses `harmonik queue append` against that active stream group. This surface introduces no new queue-method ([queue-model.md §6]).
  - **Daemon-side mirror.** This is the harness/loop side of the daemon's eager-refill obligation ([execution-model.md §4.13 EM-062]); the pre-screen guards of CL-072 mirror the daemon-side two-phase pre-screen ([execution-model.md §4.13 EM-063]); the submit/append-targets-the-active-stream-group and read-order discipline mirror [execution-model.md §4.14 EM-064/EM-065].
  - **Mechanism-tagged (CL-013, CL-INV-001).** The `kerf next` read, the CL-072 guards, and the `queue submit`/`append` calls are mechanism; they MUST NOT round-trip the model. Only the empty-queue wake of CL-073 is cognition.
  - **Idempotency (CL-055, CL-056).** Each submit/append is keyed `dispatch_intent:<event_id>:<bead_id>` checked against the dispatch-log AND queue.json AND `git log origin/main --grep "Refs:<bead_id>"` before dispatch, so refill is effectively-once across crashes.
  - **`kerf next` is advisory; queue.json is authoritative.** When `kerf next` and the loop's in-memory queue.json view disagree on whether a bead is already queued, queue.json wins (consistent with [execution-model.md §4.14 EM-064] tier 1); `kerf next` rank order is the dispatch ordering input only.
- **CL-072 — Pre-screen guards.** Apply in order; skip on hit:
  1. **Already in queue.** queue.json shows bead `∈ {pending, dispatched, completed}` → skip.
  2. **Already landed.** `git log origin/main --grep "Refs: <bead-id>" --max-count=1` non-empty → skip + emit deferred close-stale-bead intent.
  3. **Failed twice this session.** Session counter shows bead failed twice without investigation → HALT refill, wake model.
  4. **Conflicts with in-flight.** Deterministic conflict-set check (e.g. same target file per project policy) MAY skip; conflict detection at v0.1 is best-effort, MUST NOT block dispatch absent a signal.
  Every skip appends to `.harmonik/cognition/dispatch-log.jsonl`: `{ts, candidate_bead, skipped_reason, picked_instead}`. Audit-only, not replayed.
- **CL-073 — Wake-LLM only at empty-queue boundary.** Model woken for queue composition exactly when `kerf next` empty (or only failed-twice / pre-screened-out) AND ≥1 slot free. Harness builds a "queue empty; compose-next-batch" turn with digest + `kerf next` output; model decides between (a) wait, (b) escalate, (c) `br create` new beads, (d) revise priorities via skill consultation. Speculative bead generation in eager-refill is FORBIDDEN.

### 4.10 Operator surfaces
- **CL-080 — Lifecycle via `harmonik supervise`.** Commands flow through `harmonik supervise start/stop/status/pause/resume` per PL §4.9 + ON §4.3. `harmonik supervise pause/resume` is the single canonical verb form for the daemon pause/resume control surface; the bare `pause`/`resume` names survive only as the RPC `CommandName` wire values. The command verb, its agent-callable obligation, and its `operator_pause_status` producer are owned by [operator-nfr.md §4.3 ON-056/ON-057]; this requirement references the verb name and binds the loop's obligation only. The loop (or any agent driving it) MAY itself issue `harmonik supervise pause`/`resume` over the PL-003a transport without human intervention per ON-056. Loop MUST honor daemon `paused`/`pausing` states by not initiating new dispatch.
  > INFORMATIVE: The comments in `.pi/extensions/flywheel/budget.ts` and `.pi/extensions/flywheel/circuit-breaker.ts` that direct the operator to run `harmonik supervise resume` describe the verb wired by [operator-nfr.md §4.3 ON-056]; they are correct in intent.
- **CL-081 — Tmux inspectability preserved (locked C9).** MUST run in a named tmux pane discoverable from `tmux list-windows`; surface live model conversation in that pane. `tmux attach` SHOULD show live transcript. Pane name `flywheel-<project_hash6>`, created via `tmux new-window` against operator-owned session inherited from `$TMUX` at supervise start.
- **CL-082 — `harmonik digest --watch` is the live status view.** Polls digest producer at 1s cadence; renders structured human-readable view of in-flight runs, recent completions, open notes, watermark age. Read-only; MUST NOT consult model, MUST NOT mutate state. Pi TUI custom-panel surface renders the same digest from the same producer; CLI parity with TUI panel is the design intent.
- **CL-083 — Heartbeat file at `.harmonik/cognition/heartbeat`.** Loop writes current monotonic timestamp at every outer-loop wake (turn boundary, debounce flush, watchdog tick). Single line: unix-millis integer + newline. External observer MAY treat heartbeat older than `2 × watchdog_interval` as stale-loop signal and emit `flywheel_stale` event. Loop MUST NOT respawn itself — recovery is supervisor's job (CL-003).

### 4.11 Safety and budget
- **CL-090 — Per-day unified spend meter and kill-switch.** The loop MUST enforce a per-day USD cap (`--budget-usd-per-day` / `FLYWHEEL_BUDGET_USD_PER_DAY`). The meter MUST sum BOTH (a) the loop's own Pi model-turn cost AND (b) daemon-spawned `claude` implementer/reviewer/resume session cost, attributing both to one shared per-day cap. The loop MUST stop dispatching new turns and new daemon `claude` sessions when the cap is exceeded.
  - **Cost attribution.** Daemon-spawned `claude` cost reaches the meter by consuming the `budget_accrual` event ([event-model.md §8.4.2], payload `{run_id, session_id, chunk_index?, cost_units, cost_basis}`) from the `harmonik subscribe` stream the loop already consumes; no new cost event is minted. When `cost_basis` is not USD (e.g. tokens), the per-model rate table converts to USD. When `budget_accrual` events are absent or lost (the type is durability-class L, lossy-tail-ok per [event-model.md §6.3]), the loop estimates per-run cost from `run_started`→`run_completed` plus the per-model rate table.
  - **Default.** The per-day USD cap default MUST be finite (recommended 20 USD; operator-tunable per [operator-nfr.md §4.1 ON-004]). An unlimited budget MUST be an explicit operator opt-out (`--budget-usd-per-day=unlimited` or an empty `FLYWHEEL_BUDGET_USD_PER_DAY`). The loop MUST NOT default to an unbounded cap.
  - **Eventual consistency.** Because daemon `claude` cost reaches the Pi-side meter via the event stream, the USD meter is eventually consistent: a burst of daemon spawns between accrual events MAY momentarily exceed the cap before the meter catches up. The max-runs ceiling (CL-090a) bounds the burst.
  - **Exhaustion event and hard-halt.** On exhaustion (cumulative USD ratio ≥ 1.0, OR `runsToday ≥ max-runs` per CL-090a), the meter MUST emit `budget_exhausted{budget_scope=handler_account, spent_usd, cap_usd, ...}` ([event-model.md §8.4.3], for which the cognition loop is a registered producer) so that the existing handler-pause policy fires ([handler-pause.md §4 HP-012]): the `claude` handler type is paused immediately and no new `claude` implementer/reviewer sessions launch. The loop SHOULD additionally surface the cognition-layer signal `flywheel_budget_exhausted{spent_usd, cap_usd, model}` and enter `budget-paused` (§6).
  - **Day boundary and reset.** Day boundary = local-midnight in operator TZ at v0.1. Reset is NOT automatic; the operator MUST clear the pause via the existing handler-resume surface (`harmonik supervise resume` per [operator-nfr.md §4.3]). A new-day rollover resets the per-day USD total and the max-runs counter together (CL-090a); there is one reset semantics and one resume action.
- **CL-090a — Per-day max-runs ceiling.** Alongside the per-day USD cap, the loop MUST enforce a per-day max-runs ceiling: a count of daemon `run_started` events observed since the last day-boundary rollover. When `runsToday ≥ max-runs`, the loop MUST halt dispatch identically to the USD cap (CL-090). `run_started` is durability-class F (fsync-backed, not lossy), so the max-runs ceiling is the deterministic, loss-proof backstop against both cost-estimate error and `budget_accrual` event loss. The max-runs default MUST be finite (operator-tunable per [operator-nfr.md §4.1 ON-004]). The max-runs counter resets on the same day-boundary rollover as the USD total (CL-090).
- **CL-090b — Operator-tunable model selection (informative).** The Pi judgment model is an operator-tunable tier via `FLYWHEEL_MODEL_TIER1/2/3`; the judgment tier defaults to Sonnet with Opus gated behind explicit opt-in. The daemon-spawned `claude` baseline is a single operator-facing default. Both are normative operator knobs specified as config-inventory entries in [operator-nfr.md §4.1 ON-004]; this requirement records that model selection is a composition-root concern wired only at [§4.12 CL-100].
- **CL-090c — Retries draw the budget (informative).** Each review-loop iteration and each re-dispatch launches a paid daemon `claude` session and therefore counts toward the max-runs ceiling (CL-090a): paid retries draw down the same finite budget rather than being free. There is no separate retry budget. The max-runs ceiling is the global backstop; the per-bead `iteration_cap_hit` / `no_progress_detected` termination ([operator-nfr.md §4.1 ON-002]) is the local bound; whichever fires first terminates the bead's retry sequence.
- **CL-090d — Dry-run / plan-only mode (informative).** A daemon `--dry-run`/plan-only mode previews the intended spawn set (per bead: would-launch implementer + reviewer at model X, across M beads) WITHOUT launching any `claude`, reading the credential source ([credential-isolation.md §4.4 CI-006]), or emitting spend. It mirrors `harmonik queue dry-run`'s validate-without-execute behavior. The normative flag is a config-inventory entry in [operator-nfr.md §4.1 ON-004].
- **CL-091 — Reaction-rate circuit breaker.** Track own reaction rate (turns/min) over sliding window. Sustained rate above operator-set threshold (default 10/min) MUST trigger circuit-breaker pause: dispatch suspended, `flywheel_circuit_tripped{rate, threshold}` emitted, loop enters `circuit-tripped` until operator resume. Bounds runaway behavior (misclassified event class, prompt-injection self-loop, daemon bug emitting events in tight loop).
- **CL-092 — Untrusted-input threat boundary.** Treat as untrusted: bead descriptions/labels, reviewer verdict text, event payload free-text fields, `notes.jsonl` resolved-by entries from prior loop sessions. Mitigations: (a) MUST NOT pass untrusted input into any tool call that mutates persistent state (`git push`, `br update --status`) without a model-reviewed intermediate step; tool calls acting on untrusted-input fragments MUST validate against syntactic schema first. (b) MUST NEVER execute `git push --force` or `--force-with-lease` regardless of model output — any model-issued attempt MUST be refused by the harness with a structured warning. (c) System-prompt safety layer (L0) MUST contain an explicit injection-defense clause naming the untrusted-input boundary.

### 4.12 Composition root
- **CL-100 — Composition root.** Substrate-extension entry point (`./.pi/extensions/flywheel/index.ts` for Pi; `cmd/flywheel/main.go` if Go-native) is the cognition-loop composition root. Only this root may wire the harness, substrate, watermark store, digest fetcher, wake-filter table, budget tracker (the unified spend meter of CL-090/CL-090a), model-tier selection (CL-090b), and the scoped credential-injection env for the Pi holder process per [credential-isolation.md §4.3 CI-005]. Corresponding internal packages follow subsystem-envelope rule of [architecture.md §4.4].

## 5. Invariants
- **CL-INV-001 — Mechanism/cognition separation is byte-clean.** Every line identifiable as mechanism (deterministic) or cognition (model-read prompt/skills). Cross-tagged code is a defect per CL-013.
- **CL-INV-002 — Turn-boundary discipline.** No code path mutates messages between `tool_use` and matching `tool_result`. Substrate-level assertion at every model-call site.
- **CL-INV-003 — Stable prefix is byte-stable.** Bytes above breakpoint byte-identical across every model-call of a session AND every recycle. Diagnosed by CL-023 cache-hit monitoring.
- **CL-INV-004 — Watermark never regresses.** Persisted `last_processed_event_id` only moves forward in UUIDv7 order. Verified by integration test on the §4.7 crash matrix.
- **CL-INV-005 — Effectively-once reaction.** No reaction-class effect occurs more than once for the same triggering event_id, across any crash class. Verified by 10-in-flight scenario (CL-056).
- **CL-INV-006 — Spend meter covers both layers.** The per-day budget meter (CL-090) accounts for daemon-spawned `claude` session cost, not Pi turns alone; the max-runs ceiling (CL-090a) is the loss-proof backstop. Verified by a spend-meter scenario test exercising daemon `budget_accrual` consumption and the `budget_exhausted{handler_account}` halt.

## 6. Types
| Type | Tags | Notes |
|---|---|---|
| `Note` (§4.5) | mechanism | `{schema_version, ts, tool_call_id, session_id, kind ∈ {decision,hypothesis,warning,defer}, refs []string, text string, resolved_at?, resolved_by?}` |
| `WatermarkState` (§4.7) | mechanism | `{schema_version, last_processed_event_id UUIDv7, reacted_ledger map[event_id]reaction_key, updated_at}` |
| `DigestJSON` (§4.4) | mechanism | schema-versioned per CL-033; pure-code-produced; ≤40 lines body per CL-032 |
| `WakeTier` (§4.8) | mechanism | `{ignore, deterministic, wake_llm}` |
| `FullnessBand` (§4.2) | mechanism | `{healthy, warning, critical, emergency}` for `<70 / ≥70<90 / ≥90<100 / ≥100` (renamed per CL-014) |
| `LoopStatus` (§4.10) | mechanism | `{starting, ready, paused, budget-paused, circuit-tripped, draining, stopped}` |
| `BudgetState` (§4.11) | mechanism | `{schema_version, day_key, spent_usd, runs_today, cap_usd, max_runs}`; `spent_usd` and `runs_today` reset together on `day_key` rollover (CL-090/CL-090a) |

## 7. Conformance
**Core v0.1.** Conformant when CL-001..CL-100 (incl. sub-requirements) and all six invariants pass. Acceptance scenarios:
1. MemGPT 100% floor fires when agent skips a `reset_context` call (CL-011).
2. Cache-stability invariant holds across a recycle: post-recycle `cache_read_input_tokens ≥ 0.8 × stable_prefix_size` (CL-023).
3. The 10-in-flight crash scenario completes with zero double-dispatch, zero dropped completion, zero missed failure (CL-056).
4. A `merge_conflict` event aborts the in-flight turn and wakes the model with urgent-digest within 2s (CL-063).
5. Empty-queue boundary wakes the model exactly once per boundary; subsequent slot-free events refill via deterministic path without waking (CL-073).
6. The unified spend meter halts dispatch: with a finite per-day cap, daemon `budget_accrual` events drive cumulative spend to the cap, the meter emits `budget_exhausted{budget_scope=handler_account}`, the `claude` handler type pauses (HP-012), and the loop enters `budget-paused` (CL-090); separately, reaching `max-runs` halts dispatch even with no USD over-spend (CL-090a).
7. On `run_completed`/`run_failed`/`run_canceled` with ≥1 free slot and a non-empty `kerf next`, the harness appends the next ranked, non-pre-screened-out bead to the active stream group via `harmonik queue append` WITHOUT waking the model; the model is woken for queue composition only when `kerf next` is empty or yields only pre-screened-out candidates (CL-071, CL-073).
8. A `run_completed{success}` for `hk-XYZ` whose `Refs:` trailer is absent on `origin/main` does NOT mark the bead done: the harness treats it as in-flight and re-polls; conversely a `Refs:` trailer present on `origin/main` with no terminal event emits `loop_observed_phantom_done` and routes to Tier-2 reconciliation without direct action (CL-051).

## 8. Open questions
- **OQ-CL-001.** Is the digest's `kerf next` input authoritative when `kerf next` and the loop's in-memory queue.json view disagree? Working: queue.json wins; kerf advisory. Confirm with kerf owner.
- **OQ-CL-002.** Does `harmonik digest` ship as separate binary, `harmonik` subcommand, or Go library? Working: subcommand (parity with `harmonik subscribe`). Pending integration pass.
- **OQ-CL-003.** Pi substrate vs raw Anthropic Messages API at transport layer — cognition contract is substrate-agnostic but harness wiring is not. v0.1 = Pi extension; later substrate switch is non-spec implementation change as long as INV-001..005 hold.
- **OQ-CL-004.** Should the loop emit its own events into `events.jsonl` (`loop_started`, `loop_recycled`, `loop_budget_exhausted`) or a separate `.harmonik/cognition/cognition-events.jsonl`? Working: separate file at v0.1 (avoids racing daemon's writer); migrate to shared bus once daemon's writer supports external producers. NOTE: the `budget_exhausted{handler_account}` event of CL-090 is emitted to the SHARED bus (not the separate cognition file), because the handler-pause consumer ([handler-pause.md §4 HP-012]) reads the shared stream; this is the documented exception to the separate-file working answer.
- **OQ-CL-005.** Does loop's reaction set include `reviewer_verdict{APPROVE}`? Working: no — APPROVE is daemon happy path, no judgment required. Only REQUEST_CHANGES and BLOCK are wake-LLM.

## 9. Cross-spec coordination
- **[process-lifecycle.md §4.6 PL-019].** This spec promotes PL-019's orchestrator-agent role from OPTIONAL/post-MVH to a specified long-running process. PL-019 informative paragraph SHOULD reference `cognition-loop.md` rather than describing the role inline. PL-018 LLM-free invariant unchanged.
- **[event-model.md §4.3, §6.3, §8.4].** Consumes `harmonik subscribe` contract (CL-060) + heartbeat payload format + the `budget_accrual` cost surface (§8.4.2). The cognition loop is a registered producer of `budget_exhausted` (§8.4.3) for the account-scoped exhaustion of CL-090. Newly consumed types: `merge_conflict` (urgent), `decision_required` (NEW, owned by event-model), `pattern_detected` (NEW post-v0.1).
- **[handler-pause.md §4 HP-012].** The unified meter's `budget_exhausted{budget_scope=handler_account}` (CL-090) trips the existing budget-exhaustion handler-pause policy; no new halt path is introduced.
- **[control-points.md §4.5 CP-022].** The `budget_scope=handler_account` value maps to the Budget primitive's `scope` field value `handler_account`.
- **[credential-isolation.md §4.1 CI-001, §4.3 CI-005].** The loop's Pi process is the sole credential holder; its composition root (CL-100) builds the scoped credential-injection env.
- **[reconciliation/spec.md §4.4].** A bead reaching trailer-on-origin/main-but-no-terminal-event state (CL-051) routes to Tier-2 reconciliation, not direct loop action.
- **[queue-model.md §6].** Consumes `queue-submit` and `queue-append` per CL-071; no new queue-method surface required.
- **[operator-nfr.md §4.1 ON-004, §4.3].** `harmonik supervise` is the operator-facing surface; new pause-reason states (`budget-paused`, `circuit-tripped`) supervise SHOULD surface. The per-day cap, max-runs, model tiers, daemon baseline, and `--dry-run` flag are ON-004 config-inventory entries.

## 10. Revision history
| Date | Version | Author | Summary |
|---|---|---|---|
| 2026-05-31 | 0.1.2 | agent (kerf `pilot` work) | **Pi-driven dispatch & control-plane amendments (A2).** CL-051: added an explicit harness obligation (MUST NOT mark done / auto-resolve `defer` / advance past in-flight on Condition 1 alone; deterministic-tier completion does not by itself mark done); restated the two single-condition windows as harness routing obligations; named the CL-055 idempotency key; corrected the daemon-window citation to [execution-model.md §4.12 EM-052/EM-053]. CL-071: annotated the concrete Pi dispatch surface — stream-group target ([queue-model.md §2.4/§7 QM-040]), first-fill via `harmonik queue submit` (submit-as-start, no separate start verb), refill via `harmonik queue append`; cross-linked the daemon-side mirror [execution-model.md §4.13 EM-062/EM-063] + [§4.14 EM-064/EM-065]; reaffirmed the mechanism-tag at the call-site (CL-013/CL-INV-001) and the CL-055 dispatch-intent key; resolved OQ-CL-001 (queue.json authoritative, `kerf next` advisory). CL-080: confirmed `harmonik supervise pause/resume` as the single canonical verb form, deferred the producer + agent-callable obligation to [operator-nfr.md §4.3 ON-056/ON-057], added the loop-may-self-issue clause and an informative note that the `budget.ts`/`circuit-breaker.ts` comments are correct-in-intent. CL-030: stated the recycle-path seed digest MUST come from `harmonik digest` (substrate placeholder is non-conforming). §7: added acceptance scenarios 7 (CL-071 eager-append-without-wake) and 8 (CL-051 two-phase routing). No CL requirement IDs renumbered or retired. Source: kerf `pilot` 04-design/cognition-loop-design.md. |
| 2026-05-31 | 0.1.2 | agent (kerf `credfence` work) | **Unified spend meter (CL-090 rewrite) + max-runs (CL-090a) + finite default.** Rewrites CL-090 from a Pi-turns-only "substrate spend" kill-switch into a unified meter summing Pi turns AND daemon-spawned `claude` cost (via `budget_accrual` consumption) against one finite-default per-day cap; adds the CL-090a max-runs ceiling as the loss-proof backstop; wires exhaustion to `budget_exhausted{budget_scope=handler_account}` so the existing handler-pause policy ([handler-pause.md] HP-012) halts dispatch. Adds informative CL-090b (operator model knobs), CL-090c (retries draw max-runs), CL-090d (dry-run). Adds CL-INV-006, the `BudgetState` type row, conformance scenario 6, and `credential-isolation`/`handler-pause`/`control-points` to `depends-on`. §2.1 budget bullet broadened to "both layers." Source: kerf `credfence` change design. |
| 2026-05-30 | 0.1.1 | agent (flywheel spec-bundle hk-j7o3i) | **Round-4 vet amendments: CL-014 (band-name rename) + CL-015 (recycle-only strategy).** CL-014: renames the four `FullnessBand` values from `{nominal, soft, strong, critical}` to `{healthy, warning, critical, emergency}` at unchanged 70/90/100 thresholds — aligns with flywheel_gateway context-health vocabulary without altering behavior. CL-015: declares `summarize_and_continue`, `checkpoint_and_restart`, and `graceful_handoff` strategies explicitly NON-CONFORMANT for flywheel, with rationale citing CL-INV-001, CL-020, CL-021, CL-023, CL-INV-003, CL-002. Updates §6 FullnessBand type row. Source: context-health-vocabulary-vet findings.md §5. |
| 2026-05-30 | 0.1.0 | flywheel-author | Initial draft. Defines CL-001 through CL-100 over kerf `flywheel` work's round-2 design (`04-design/self-managing-architecture.md`). |
