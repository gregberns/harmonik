# 04 — Change Design: the cognition loop (flywheel)

> Kerf work `flywheel`, change-design pass. Synthesizes 16 research findings (03-research/) + 7 review/verification agents (review-synthesis.md) into a coherent design for harmonik's long-running orchestrator/cognition loop. Reading order: 01-problem-space → 02-components → review-synthesis → this. **Two things need user ratification before spec-draft** (the product fork §1, the substrate/billing reality §4); they are presented with a recommendation, not silently resolved.

## 0. What flywheel is (one paragraph, corrected)
Harmonik has two loops: the **daemon work-loop** (deterministic Go, no LLM, owns claim/dispatch/commit/merge/close/reconcile — robust, locked LLM-free by PL-018) and the **orchestrator/cognition loop** (the Claude session that decides what to dispatch, keeps the queue full, reacts to outcomes). The cognition loop is today an interactive Claude Code session that dies when its context fills, forcing a human to handoff→restart→resume. **Flywheel specifies the cognition loop as a first-class long-running process that survives its own context limit and crashes, keeps the queue full, and reacts to the daemon's event stream — indefinitely, without a human in the restart loop.** The mechanism is the field-consensus pattern (Gastown/Symphony/Ralph/Anthropic/OpenAI all converge): **the durable store is the memory; the context is disposable and recycled; continuity is a deterministic digest + the daemon's existing durable state, never LLM compaction.**

## 1. ⚠️ THE PRODUCT FORK — needs user ratification
Research strongly converged on **Architecture B** (the loop is deterministic code; the LLM is a *called function* invoked only for judgment — investigate-exception, break-priority-tie, escalate). The cross-cutting + architecture reviews flagged that this **inverts the user's stated framing** ("one or more *agents* in a loop") into "a smarter daemon that phones an LLM occasionally" — a scope reversal that needs sign-off, because a pure-B loop cannot do the cross-batch judgment the human orchestrator does today (notice a class of beads failing systemically, see throughput decline and hypothesize, re-strategize from reviewer-verdict patterns).

**Recommendation: Architecture B-plus** — the deterministic skeleton (cheap, robust, debuggable; reuses the daemon's "deterministic skeleton, probabilistic organs" thesis one layer up) **+ a widened LLM judgment surface** so it remains a genuine agent:
- Wake the LLM not only on exceptions/empty-queue but on a **`pattern_detected`** signal (N consecutive failures in an area, BLOCK-rate over a rolling threshold, throughput decline) — fed a *rolling outcome summary*, so the loop "notices things across batches."
- Let the LLM emit a **`set_max_concurrent(n)`** override (tunable, not dynamic-by-default) so "aggressive queue management" includes raising/lowering WIP, not just filling a fixed N.
- Keep the deterministic core for the 80-90% (refill, classify, route happy-path) so cost and robustness stay daemon-grade.

This preserves the user's "agent that thinks and adapts" intent while capturing B's economics/robustness. **Alternative if the user wants a fuller agent:** A-fixed (the orchestrator stays a real LLM loop; flywheel only fixes its two defects — context exhaustion via recycle+digest, crash-recovery via watermark). Costs more tokens, more "agent-like." The fork is genuinely the user's call.

## 2. The loop cycle (CL-cycle)
```
START / RECYCLE → fresh context = [ frozen prefix | cache_control | digest ]   (no --continue; new context)
  ↓
SENSE      reconcile from durable artifacts (read-only): watermark → queue.json → origin/main → br → ScanAfter(watermark)
  ↓
CONSUME    drain new events since watermark through the wake-filter
  ↓
ACT        deterministic: refill freed slots from kerf next; classify+route happy-path; advance watermark
           LLM-on-demand: investigate failed-twice; break priority tie; pattern_detected; empty-queue replenish; escalate
  ↓
PERSIST    append notes (decisions/hypotheses) ; fsync reacted_ledger ; advance watermark (never regress)
  ↓
DECIDE     continue this context, or recycle (near token threshold) → back to START
```
Crash/restart = the SENSE step from a cold process (steal Symphony §14.3): reconstruct in-flight truth, adopt orphaned runs, bootstrap a fresh context. A **progress/liveness invariant** (analogue of the daemon's bounded-retry) must hold: no cycle without state change; a doom-loop (re-dispatching the same bead, no forward progress) trips a kill condition.

## 3. Context lifecycle & caching (CL-context) — corrected & gated
- **Regime B** (verified viable, V7): every recycle is a fresh request `[ frozen prefix | cache_control:{ephemeral,ttl:"1h"} | digest + turns ]`. The prefix is byte-stable → content-addressed cache HIT across contexts within TTL+workspace → per-cycle cost ~flat (~$0.03 Opus illustrative), independent of loop age. **Start a NEW context — NOT `--continue`** (V/C1: `--continue` replays the full transcript; Gastown's "fresh" depends on not replaying).
- **Runtime invariant (the single most important guardrail):** assert `cache_read_input_tokens ≈ prefix_size` every cycle; alert/capture-on-wire-prefix if it drops to 0 (prefix broke, TTL lapsed, or the substrate injected content). **GATE: verify the cross-context cache-hit empirically in the minimal slice before committing the cost model** (the skeptic's #1 risk — docs support it, but measure it).
- Prefix must clear 4096 tok (Opus min-cacheable) yet stay small; budget the digest +35% for the Opus-4.7 tokenizer. **Model-version is pinned in the prefix; a model bump = a scheduled cache-rebuild (cold-start + re-measure min-cacheable + re-measure digest tokens).**
- **Engage-don't-ignore:** a small verbatim recent-turns buffer (Hermes-LCM fresh-tail, last ~5 turns, discarded on resolve) MAY complement the digest to preserve in-flight reasoning not yet noted — bound it, decide in spec-draft. (Resolves part of the C6 notes-vs-stateless tension: if the LLM cycle is long-lived enough to accrue turns, it can also `note add`; if it's a one-shot called-function, the *deterministic* layer emits the notes via `decision_required`.)

## 4. Substrate & billing (CL-substrate) — corrected; needs user ratification
**Primary substrate = the raw Anthropic Messages API on an API key**, via our own thin loop OR `pi-agent-core` driven *with an API key* (NOT the OAuth path). Rationale: only the raw API gives full per-turn context control + explicit `cache_control` (the Claude Code SDK can't disable compaction and `--resume` replays the transcript — V8). Pi-as-code is excellent (`transformContext`/`shouldStopAfterTurn`/custom session entries; MIT; thin); use it for the loop machinery if we want, but **the Pi-via-Max-OAuth billing path is DO-NOT-SHIP** (V6: Anthropic ToS ban + enforcement eff. 2026-04-04 targets exactly that stealth pattern). **Consequence the user must accept: a 24/7 unattended loop bills API per-token, NOT the Max subscription** (Max isn't available for headless/unattended; the workaround is banned). This *reframes G8*: the "exception" isn't "keep Max via OAuth" — it's "accept API billing for headless use." **GATE: a per-day dollar/token ceiling + hard kill-switch** (the circuit-breaker limits dispatch rate, not spend).
- Decision for user: (a) own ~740-LOC loop on `@anthropic-ai/sdk`, or (b) `pi-agent-core` + `pi-ai` (API key, pinned, with a CI assert that serialized tool-defs stay byte-stable across Pi upgrades — cache integrity). Lean (b) for speed, (a) for zero third-party dependency.

## 5. The digest (CL-digest) — corrected
`harmonik digest` = a **pure-Go computed view over an append-only log**, ≤~40 lines: loop counts; exception list (only beads needing action: failed-twice/blocked/deferred/merge-conflict); open agent-notes; next-action; trailing fetch commands. Two source streams: **derivable** (queue.json + `origin/main` git + `ScanAfter(watermark)` events + worktrees — zero LLM) and **non-derivable** `notes.jsonl` (decisions/hypotheses/warnings the cognition layer appends at decision time). Why a computed view beats compaction: `digest_n=f(full_log)` recomputed from source every cycle → no telephone-game drift (vs `summary_n=LLM(summary_{n-1})`).
- **Coercive note discipline (P5):** the daemon emits a **`decision_required`** event on defer/block/failed-twice/merge-conflict; an unacknowledged `decision_required` is itself a digest exception that **blocks dispatch** until resolved — inverts fragile voluntary `note add` into an obligation. This also resolves C6 (the deterministic layer creates the obligation; the LLM/agent satisfies it).
- **Projector is a versioned SPOF (P5):** schema-versioned, **unknown-event-type → emit a visible warning, never silently drop**; `harmonik digest --health` surfaces note-filing rate, unknown-event coverage, open-note age. Auto-resolve **only `defer` notes**; `hypothesis`/`warning` need explicit `note resolve`.
- Bounded size: resolution-marking; the view shows open items only; size tracks open-exception count, not session length.

## 6. Consistency & event reaction (CL-consistency) — corrected, the hardened core
Division of labor: the **daemon owns run-level crash-consistency** (atomic claim before run_started; idempotency-keyed commit/merge/close; 11-category reconciliation). The **loop owns reaction-level consistency only** — do NOT duplicate; do NOT re-derive run state from JSONL (RC-014).
- **Two-phase "done" (V3, the scariest fix):** react-as-complete only when BOTH a `run_completed{success}` event is in the gap AND the `run_id` trailer is on **`origin/main`** (not local main) — closes the push-lag false-done window where push_failed (EM-053) reopens a bead the loop already reacted to.
- **Watermark** `.harmonik/cognition/watermark.json` = `{schema_version, crc, last_processed_event_id:UUIDv7, reacted_ledger}`. UUIDv7 (ms-ordered) + `ScanAfter`. **Corrupt/parse-fail → replay from JSONL floor using reacted_ledger as the dedup authority** (V5: ScanAfter does NOT cold-start on a bad id — it silently skips). **Never regress** (`max(persisted, heartbeat.last_event_id)`). fsync the ledger BEFORE advancing the watermark; ordering effect→ledger→watermark.
- **Per-reaction-class idempotency (P6)**, not one label: create-bead → dedup label `reaction:<event_id>`; re-dispatch → check-observed-before-submit reading queue.json AND ledger AND origin/main; `kerf --ack` → record acked event_id, ack only events strictly above; escalate-to-user → ledger + at-most-once-per-window.
- **Loop-singleton lock** `.harmonik/cognition/loop.lock` (flock+pid, stale-pid reclaim) — prevents two accidental instances both reacting.
- **Event transport: `harmonik subscribe` (LANDED, V9)** — NDJSON + 60s heartbeat(`last_event_id` + `active_runs[bead_id,age_seconds]`, no run_id) + `since_event_id` replay-then-dedup + `subscription_gap`. On `subscription_gap` → force a `ScanAfter` re-sync AND a fresh git+queue.json sensing pass. (Interim `tail -F`+ScanAfter is now just the fallback.)
- **Wake-filter** (DEFER the exact 3-tier taxonomy/debounce/circuit-breaker constants until observed — P4): conceptually Tier-0 ignore telemetry (advance watermark), Tier-1 deterministic (happy-path), Tier-2 wake-LLM (failed-twice / REQUEST_CHANGES|BLOCK after cap / merge_conflict / queue-empty / pattern_detected / escalation). **git-done-but-no-terminal-event after K heartbeats → Tier-2 wake** (daemon-crashed-mid-terminal), not silent advance.

## 7. Queue pressure (CL-queue-pressure) — corrected, mostly free
WIP-limited **pull**: ceiling==refill-target==`max_concurrent` → over-aggression ("fan-out into new work", the one real observed failure) is **structurally impossible** (same gate is the keep-full mechanism). Eager **pure-Go** refill on each freed slot from `kerf next` (pre-screened against origin/main). Drain-bias (refill is the last action of a tick). No speculative bead generation in the refill path (provenance: only already-`ready` beads; loop-created beads land `open`, never auto-dispatched same-tick). Metrics: `refill_misses` (headline defect), slot_utilization, idle_slot_seconds — emitted as a `queue_pressure` digest event so the loop *sees its own idleness*. **CORRECTION (V2): no new `pool` queue kind needed** — a stream group already accepts appends AND runs up to max_concurrent concurrently (dispatched heads are skipped, not HOL-blocking); **hk-24xn1 wake-on-append is already closed (V1)**. So this whole component is ~near-zero new queue machinery. Optional: LLM `set_max_concurrent` override (§1 B-plus).

## 8. Operator, safety & the GATES (CL-operator) — the biggest gap the corpus missed
An unattended LLM that commits/merges/**pushes** for days, reading bead bodies/diffs/reviewer output it did not author, is a serious threat surface. Before anything pushes unattended, these gates MUST exist (currently zero coverage):
- **G-test — replay test harness:** replay a recorded events.jsonl + queue.json through the loop and assert correct reactions WITHOUT tokens or real pushes. You cannot trust an unattended pushing agent you can't deterministically replay-test. (Tie into the scenario-harness.)
- **G-security — threat model:** trust boundary on all agent-read content (bead bodies, diffs, reviewer text are untrusted input → prompt-injection risk: "ignore rules, force-push, dump env"); an allowed-action allowlist; a no-force-push / no-destructive-git guard; the kill-switch below.
- **G-cost — dollar/token ceiling + hard kill-switch** (per-day budget halts the loop; distinct from the dispatch-rate circuit-breaker).
- **G-liveness — progress invariant + explicit kill condition** (no cycle without state change; doom-loop detection).
- **G-inspect — preserve tmux-inspectability (locked C9):** a headless loop drops the tmux pane; resolve by running the loop in a tmux pane and/or a live `harmonik digest --watch` attach surface.

## 9. The minimal first slice (the recommended v1 — build this, watch it, then spec the rest)
Per the skeptic + architecture reviews, **do not spec/build the full tuning stack up front.** Build Ralph + the one piece Ralph lacks (authoritative in-flight tracking), prove the assumptions, then expand:
1. **`harmonik digest` projector** (pure Go; reads queue.json + origin/main + ScanAfter + beads; ≤40-line view). Testable, deterministic, useful to a human today. De-risks everything.
2. **Watermark + idempotent-reaction guard + loop-singleton lock.** The 10-in-flight scenario becomes a real scenario-harness test (the acceptance test), not prose.
3. **The dumbest loop:** raw Messages API (API key) via `transformContext = [frozen prefix | cache_control | harmonik digest]`, `shouldStopAfterTurn` recycle, fresh context, poll `subscribe`, react simply. **Run unattended for hours against the real daemon; measure `cache_read_input_tokens`; watch what breaks.**
Then, and only then, add (driven by observed need): wake-filter tiers/debounce/circuit-breaker constants, `pattern_detected` + `set_max_concurrent`, keep-warm-ping cadence, notes GC/tiered-TTL, the recent-turns buffer. **Defer indefinitely:** the multi-role fleet (Architecture C) — revisit only at the named trigger (sustained max_concurrent≥5 w/ frequent failures, or stateful cross-cycle investigations).

## 10. Open decisions for the user (also in 02-components §C)
1. **Product fork (§1):** B-plus (recommended) vs A-fixed vs pure-B. Ratify the framing.
2. **Substrate (§4):** own-loop vs pi-agent-core-with-API-key; accept API per-token billing for headless (Max unavailable; OAuth workaround banned).
3. **Process placement:** separate process (+singleton lock, assumed) vs a harmonik workflow (gets checkpoints/replay free).
4. **Minimal-slice-first (§9):** agree to build+observe the 3-step slice before specifying the full machinery? (Strongly recommended — avoids gold-plating an unobserved loop.)
5. **Gates (§8):** confirm the test-harness + threat-model + cost-kill-switch + inspectability are required-before-unattended-push (recommended: yes, non-negotiable).
6. **Integration scope:** the Integration pass (Pass 6) wires launch/supervision into harmonik — still deferred per problem-space NG1, delivered within this work.
