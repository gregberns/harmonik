# Research/Design — Queue-pressure / keep-the-queue-full mechanism

> Component: `queue-pressure-aggressiveness`. Source: research sub-agent (opus), grounded in harmonik code, 2026-05-27. **[FACT]**=sourced, **[DESIGN]**=proposal, **[OPEN]**=needs user.

## TL;DR
- **WIP-limited PULL system, not push.** One quantified target: `in_flight == max_concurrent` whenever `ready > 0`; any tick where a slot sits idle *while ready work exists* is a measured defect (`refill_misses`). The daemon's `in_flight_count() < max_concurrent` gate is already a pull primitive; flywheel adds **eager refill** so a freed slot is backfilled within the same tick.
- **The over-aggression guard (the kerf "fan-out" failure) IS the under-aggression target — same number.** Ceiling and refill-target are both `max_concurrent`, so the loop *physically cannot* fan out beyond N in-flight — there is no separate "start more" knob. Refill pulls only from the *already-ready* queue; new-work generation is a separate, rate-limited, low-watermark path.
- **Pure code handles the common case; LLM woken only at the empty-queue boundary.** Refill (`available = N − in_flight; pull top from kerf next; pre-screen; append`) is deterministic. LLM wakes only when `ready` drops below a low-watermark AND in-flight is also low (genuinely draining), never to keep a busy queue busy.

## Q1 — The quantified target (WIP-limited pull)
| Signal | Definition | Value |
|---|---|---|
| WIP cap (hard ceiling) | never dispatch when `in_flight >= N` | N (= max_concurrent) |
| Refill target | desired in-flight when ready work exists | N (same number) |
| Low-watermark on in-flight | trigger eager refill | `in_flight < N` AND `ready > 0` |
| Ready-depth floor `R_min` | wake LLM to replenish ready queue | `ready < N` AND `in_flight ≤ ⌈N/2⌉` |

[FACT] Daemon already enforces `in_flight_count() >= max_concurrent → sleep` (execution-model §4.11 EM-049; `workloop.go:512` `effectiveMax`); max_concurrent sealed at startup, default 1 (EM-051). **The ceiling exists; the refill *obligation* is missing** — today a freed slot is backfilled only if the active group still has pending items; the loop never reaches out to `kerf next` to top up a draining queue. [FACT/queueing] Little's Law: `throughput = WIP / cycle_time`; with WIP pinned at N, throughput is maximized by minimizing cycle_time, NOT inflating WIP → why ceiling==target.

## Q2 — The refill loop (deterministic, in the daemon)
```
on run_terminal OR poll_tick:
    available = max_concurrent - in_flight_count()      # EM-049
    if available <= 0: return                            # WIP cap — hard stop
    deficit = available - pending_in_active_group
    if deficit <= 0: return                              # existing gate handles it
    candidates = pre_screen(kerf_next(limit=deficit*OVERFETCH))  # ranked feed, drop already-landed
    if candidates: queue_append(active_stream_group, candidates[:deficit])
```
[DESIGN] Pure Go, no LLM — the common case ("slot freed, ready work exists, pull next ranked item") has no ambiguity; `kerf next` already encodes prioritization. Refill at code-speed (sub-second) closes idle slots fast; LLM-speed (tens of s) would not. [FACT] `kerf next` ranked feed; pre-screen = `git log --all --grep "Refs: <id>"`; `queue append --queue-id <uuid> <group> <ids>` exists (`socket.go:377`, queue-model §7.3). [OPEN] OVERFETCH factor (propose 2-3×) so pre-screen rejections don't leave a slot empty.

## Q3 — Over-aggression guard (THE critical structural property)
[FACT] Recorded opposite failure: harmonik agent "fanning out into NEW work instead of draining" (plans/014_process_management_reframe). Must be *structurally impossible*, not policy. [DESIGN] Three stacked guards:
1. **Identical ceiling-and-target (structural):** refill never dispatches above max_concurrent, and the target IS max_concurrent → no code path starts an (N+1)th run. WIP explosion impossible regardless of ready depth. **The keep-full mechanism and the over-aggression cap are the same gate.**
2. **Drain bias (ordering):** when both a freed slot and an advanceable in-flight run exist, finishing wins — refill fires *last* in a tick, after all terminal/merge/review processing. Optionally Symphony-style per-state caps (cap "merging"/"reviewing" below N) so the loop prefers pushing in-flight to done.
3. **No speculative bead generation in the refill path (provenance):** refill pulls ONLY from `kerf next` (already-`ready` work that existed independently of the loop's need to fill a slot). Beads the loop *creates* go through the separate rate-limited Q4 path and land `open`/`blocked`, never auto-dispatched in the tick they're created. **A slot idle because `ready==0` is CORRECT, not a defect.**
**Operator framing:** *Keep N slots full from the ready queue; never exceed N; never create work just to fill a slot.*

## Q4 — Empty-queue problem (when to wake the LLM)
[DESIGN] Wake trigger: `ready < N` AND `in_flight ≤ ⌈N/2⌉` (both required — genuinely draining, not momentary churn). Woken LLM does real-work replenishment, ranked: (1) drain `kerf triage` (real-but-unsurfaced beads); (2) file follow-ups from completed runs (test gaps, leftover TODOs); (3) surface dependency unblocks (`br ready` re-query); (4) only if 1-3 empty, propose genuinely new beads — as *proposals* (land `open`, not auto-dispatched), loop idles until vetted. If replenish yields zero ready beads → **idle is a legitimate terminal state** (emit `queue_drained`); manufacturing work is the kerf anti-pattern. Rate-limit replenish to once / 5-min cooldown. [OPEN] Should LLM-generated beads ever auto-dispatch after a delay, or always require human/kerf-pin? Recommend: never auto-dispatch in v1.

## Q5 — Metrics / self-correction (loop sees its own idleness)
[DESIGN] Emit a periodic `queue_pressure` digest event: `slot_utilization` (in_flight/N; <0.8 while ready>0 = DEFECT), `idle_slot_seconds` (while ready>0; any nonzero = defect), `throughput` (beads completed/hr), `wip_over_time` (should hug N), `ready_depth`, **`refill_misses`** (ticks where available>0 ∧ ready>0 but no dispatch — *the headline defect counter*). [FACT] Fits existing events.jsonl + Monitor surface → the orchestrator's Monitor literally *sees* its own idleness as a notification, closing the human-attention-gating loop. Non-zero refill_misses with ready>0 → loud WARN (refill-path bug).

## Q6 — Stream vs wave + head-of-line blocking (a real spec decision)
[FACT] Stream groups accept mid-flight appends but **HOL-block** (`EligibleItems`/`streamEligible()` returns only the head until it terminates; queue-model §2.4; `workloop.go:674`) → a single stream at max_concurrent>1 runs only 1 concurrently (CLAUDE.md: "use `--wave` when max-concurrent>1"). Wave groups dispatch concurrently up to N but **reject appends**. **Flywheel needs BOTH concurrent dispatch AND mid-flight refill — today's primitives force a choice.** [DESIGN] Options for user:
- **(a) New group kind `pool`** = append-able + concurrent (wave's concurrency + stream's appendability). Cleanest/explicit. **Recommended.**
- (b) Per-stream `max_parallel` attribute; HOL-block only when ordering matters (default concurrent for flywheel queues).
- (c) Keep wave, drop appends, submit a fresh wave when prior drains — simplest, no spec change, but loses true mid-flight refill (tail-of-wave idle) AND needs hk-24xn1.
[FACT] **Blocking gap regardless: hk-24xn1** — daemon doesn't wake on `queue-submit`/`queue-append` when idle; a `submitWakeC` channel exists (`workloop.go` `deps.submitWakeC`) but isn't signaled on append-while-idle. **Eager-refill REQUIRES wake-on-append to land first**, or refill latency = poll interval. [DESIGN] Recommend option (a) `pool` gated behind closing hk-24xn1.

## Open questions for user
1. Q6 queue semantics: pool-group (a) vs per-stream max_parallel (b) vs fresh-wave (c)? **Load-bearing spec decision** — flywheel's refill story depends on a queue that's both append-able and concurrent.
2. R_min + wake dual condition — right boundaries, or LLM-replenish purely manual in v1?
3. Auto-dispatch loop-generated beads after vetting, or always gate? (Recommend gate in v1.)
4. Per-state slot caps worth v1 complexity, or single WIP cap sufficient?

## Files
specs/queue-model.md (group kinds/append §2.4,§7.3 — amend for Q6); specs/execution-model.md (capacity gate EM-049/050/051 §4.11, dispatch §7.4, group-advance EM-015f); internal/daemon/workloop.go (capacity gate ~512, EligibleItems ~674, submitWakeC); socket.go:377 (append); docs/orchestration-protocol-v2.md (the human loop flywheel automates).
