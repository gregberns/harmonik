# Research/Design — Single vs multi-agent cognition layer

> Component: `single-vs-multi-agent`. Source: research sub-agent (opus), grounded in harmonik code/specs, 2026-05-27.

## TL;DR
- **RECOMMENDATION: Architecture B — a deterministic Go/shell tick-loop with the LLM as a *called function*, not the loop.** The daemon already owns dispatch/commit/merge/close/reopen/retry-cap; the cognition layer is a thin scheduler that invokes a *fresh* LLM cycle only for genuine judgment (exception investigation, ambiguous prioritization, escalation).
- **Single-vs-multi-agent is the WRONG axis. The right cut is code-vs-LLM per responsibility.** ~4 of 6 responsibilities are already deterministic in the daemon; the LLM is needed for ~3 judgment calls *per batch*, not per tick. Greg's own kerf instinct (`while true; do kerf next | harmonik run; sleep`) is correct for the skeleton.
- **A persistent multi-role fleet (Architecture C) is over-abstraction now** — it re-implements coordination the daemon + git + queue.json already provide (Gastown needs roles because it has *no* central controller; harmonik *has* one). Adopt C only at a named scale trigger.

## Q1 — Responsibility-by-responsibility: LLM vs code (the load-bearing table)
CODE = no LLM; LLM = genuine judgment; CODE→LLM = code by default, escalates on typed exception.

| # | Responsibility | Verdict | Why / where |
|---|---|---|---|
| a | Prioritize/pick work | CODE→LLM (~90% code) | `kerf next` already emits a *ranked* feed; code takes top-N passing eligibility. LLM only on genuine ambiguity (two P1s; priority looks wrong vs recent failures). |
| b | Enqueue/dispatch | CODE | `harmonik run --beads … --max-concurrent N` is one shell call; daemon claims/materializes/gates (`workloop.go` goroutine-per-bead gated by MaxConcurrent). Zero LLM. |
| c | React to outcomes | CODE→LLM (~80% code) | Classification already deterministic: §8 failure taxonomy (`failure_class.go`) + review-loop APPROVE/REQUEST_CHANGES/BLOCK/cap-hit (EM-015d/e). transient→re-dispatch once; cap-hit/BLOCK→needs-attention (no auto-retry). LLM only on same-bead-failed-twice → investigator. |
| d | Investigate exceptions | **LLM** | The one irreducibly-cognitive job: read events+diff+bead, form root-cause hypothesis. The *primary reason* an LLM is in the layer. |
| e | Keep-queue-full backfill | CODE | "in-flight < max_concurrent AND feed non-empty → append next group." stream queues accept mid-flight appends; retry-cap `MaxItemAttempts=3` in code. Zero LLM. |
| f | Triage/file follow-ups | CODE→LLM (~70% code) | `kerf triage` drift report + `--ack` + `git log --grep Refs:` pre-screen = code. Writing a *new* follow-up bead needs LLM phrasing — batched, not per-tick. |

**The line:** the loop's control flow (wake, check capacity, fill slots, classify, route deterministic classes) is ALL code. The LLM is reserved for exactly three judgment calls: **(d) root-cause an exception, (a) break a genuine priority tie, decide-to-escalate-to-user** — ~3 LLM invocations per *batch*, not per *tick*.

## Q2 — Three architectures
- **(A) Single LLM loop does everything** — simplest mental model BUT **fails the 35-minute rule** (the loop *is* the long-duration task), pays frontier tokens every tick for 80%-deterministic work, zero failure isolation (one bad compaction poisons all 6 responsibilities), poor debuggability. Worst fit — duplicates daemon logic in probabilistic prose.
- **(B) Deterministic code loop + LLM-on-demand** — cost ≈ Symphony (LLM tokens only on exceptions); context-management trivial (each LLM call = fresh context + small digest, returns, nothing accumulates → no compaction ever); failure isolation (investigator blowup ≠ dispatch stall); debuggable Go/shell trace. **Best fit — same "deterministic skeleton, probabilistic organs" philosophy one layer up.** Con: must port eligibility/routing rules to code (but they already exist as prose in daemon + CLAUDE.md).
- **(C) Multiple persistent role-agents** (prioritizer/dispatcher/reconciler/investigator) — clean context-scoping, true parallel investigations, maps to Gastown. BUT coordination cost is the killer: Gastown needs a mail bus *because* roles are peers with no controller; harmonik has the daemon. Persistent role-sessions each hit the 35-min rule + need their own respawn/watchdog (rebuilding Gastown's Deacon/Witness). For current volume (2-5 beads/batch) it solves a scale problem we don't have. Premature.

## Q3 — Shared state without a new coordination layer
Substrate already answers it — DON'T invent a layer: **queue.json** = dispatch blackboard (claim token-pool prevents double-claim); **git** = completion authority (`Refs:` trailer is the signal, no agent tells another); **events.jsonl + `harmonik subscribe`** = message bus (NDJSON, type-filtered, heartbeat, `since_event_id` resume — *already a mail bus*); **beads** = status + `needs-attention` (operator-drained queue); **notes.jsonl + watermark (PROPOSED, greenfield — confirmed no refs exist)** = the cognition layer's OWN durable digest, single-writer append-only, watermark = the `since_event_id` cursor → "the store is the memory," no locks. If C ever happens, roles share via these stores read-mostly, daemon as sole multi-writer. No agent-to-agent channel, no file reservations.

## Q4 — Recommendation rationale
Build B. Harmonik's thesis is "deterministic skeleton, probabilistic organs"; the daemon already IS that skeleton for execution. The cognition layer should extend it one level up, not contradict it with a probabilistic loop. 4/6 responsibilities are already code; the residual judgment is naturally a called function with fresh context per call — the only context strategy that runs indefinitely without compaction. ~90% cost reduction (frontier tokens only on exceptions), perfect failure isolation, debuggable trace. **What would change my mind → promote investigator to a persistent role (partial C):** (i) exception *volume* makes a single serialized judgment-LLM the throughput bottleneck, OR (ii) investigations routinely need to *persist* a working hypothesis across many cycles (stateful debugging). Concrete trigger: sustained `--max-concurrent ≥ 5` with frequent failures.

## Q5 — Anti-anchoring verdict
Skeptical case (B wins): a role-fleet is abstraction the user warns against; Gastown's hierarchy solves a no-controller problem harmonik doesn't have; persistent agents resurrect the 35-min failure per-role + demand watchdogs. For-multi case: clean context-scoping, parallel slow investigations, Anthropic lead+subagents proven, DOT North Star eventually wants heterogeneous roles. **Judgment: the for-case argues for parallel *workers* (harmonik already has them: goroutine-per-bead + worktrees), not parallel *cognition roles*. The one cognitive job (investigation) is low-frequency today, so the parallelism win is hypothetical. B now; C is a clearly-marked upgrade path, not a starting point.** Don't build the fleet because it sounds sophisticated — build the called-function and let measured exception-volume tell you when to split.

## Open questions for user
1. Escalate-to-user channel: reuse `needs-attention` bead label (existing operator-drained queue) vs new notification path? (Lean: reuse label.)
2. notes.jsonl schema: confirm single-writer append-only, watermark as only cursor — or does the daemon also write it (reintroduces multi-writer lock Q)?
3. Tick cadence: event-driven via `harmonik subscribe` (already built, strictly better) vs 30s poll — confirm we drop polled-tick.
4. Is the cognition layer itself a harmonik workflow (your "reconciliation runs as a harmonik workflow" framing) or a sidecar process? Decides whether it gets checkpoints/replay for free.
