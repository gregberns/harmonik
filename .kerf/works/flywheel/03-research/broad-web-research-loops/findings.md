# Research — Broad web survey: long-running agent loops & context mgmt

> Component: `broad-web-research-loops`. Source: research sub-agent (sonnet), 2026-05-27.

## TL;DR
- The field has converged on **"filesystem/store-as-ground-truth + fresh-context restarts"** (Ralph loop, Codified Context) as the dominant pattern for avoiding lossy compaction — the LLM context is ephemeral, files/git are durable, each restart re-derives its digest from artifacts.
- **Prompt-cache stability in long loops = treat the prefix as read-only infrastructure** — any mutation (tool-output format change, dynamic system-prompt, sliding-window drop) destroys hits; "Don't Break the Cache" (arXiv 2601.06007) quantifies **50-70% latency savings** when discipline holds.
- **Controller-worker / planner-worker is the production consensus** over single long-lived agents — decomposes doom-loop risk, contains drift, cuts cost ~90% by routing execution to cheaper workers under one orchestrator context.

## Patterns
**1. Ralph Wiggum loop (Huntley, Jan 2026):** `while true; do cat PROMPT.md | claude-code; done`. Each iteration = fresh context seeded by a prompt *regenerated from durable artifacts* (`@fix_plan.md` priority list updated by the prior pass, `@specs/*`, `@AGENT.md`). No compaction/summarization. "Context waste is acceptable overhead" — re-pays context each pass because clean signal beats polluted long context. Cache: not addressed; restart = new API session → cache hits only within a run. Failure modes + fixes: assumption-errors (explicit "don't assume not-implemented"), placeholder impls (anti-placeholder directive), doom loops (`--max-iterations` cap + escape-hatch), context overflow (route to subagents). Result: $50K contract for $297 compute; *iteration count*, not context size, is the knob.
**2. Codified Context (arXiv 2602.20478):** 3-tier static knowledge loaded deterministically — Tier1 ~660-line "constitution" (always loaded, fixed = ideal cache-stable prefix), Tier2 19 domain-agent specs, Tier3 34 on-demand docs via MCP keyword search. Lossless by construction (codified at authoring time, loaded verbatim). 74 sessions/4wk consistent. **Achilles heel: spec staleness** → biweekly maintenance, or "freshness inverts into confident wrongness."
**3. CAT — Context as a Tool (arXiv 2512.22087):** context mgmt as a *callable tool* the agent invokes; 3 layers (stable semantics / condensed long-term / high-fidelity short-term). Partial losslessness — condensed-long-term still LLM-summarized.
**4. LCM / Hermes-LCM (Apr 2026):** DAG context engine; ingest→compact→condense→escalate→assemble; "fresh tail" 64 msgs verbatim; older → leaf summaries merging upward; full material recoverable via `lcm_grep`/`lcm_expand`. `/new` carries configurable DAG depth across sessions = closest published "restart-with-digest" that keeps lossless retrievability. Cache: "cache-friendly, not fully cache-aware" (deferred). Known bugs: compression-on-restart can carry the original first message as if no progress (loops); compression exhaustion → infinite retry.
**5. OpenHands condenser:** rolling history + LLM summary when events exceed max_size; first `keep_first` always kept verbatim; event-sourced (append-only, replay reconstructs). **Lossy by design** (no retrieval back to originals once condensed); cache degrades as summaries vary.
**6. Aider:** repo map (tree-sitter index, lossless *selection* not summary) + `--cache-prompts` (cache_control on system+read-only-files+repo-map = stable prefix) + `--cache-keepalive-pings` (every 5min to keep cache warm / extend to 1hr). Single growing session, manual `/clear` for very long; no restart-with-digest.

## Cross-cutting cache findings ("Don't Break the Cache" arXiv 2601.06007)
Kills hits: tool-output formatting inconsistency, dynamic system-prompt mutation, sliding-window drop, implicit state changes before the breakpoint. Preserves: deterministic serialization of static content, version-controlled prompt templates, explicit immutable-prefix/mutable-tail separation, breakpoint after last static block. Empirical: **50-70% latency reduction + cost savings on 10+ turn tasks** when stable. Rule: *treat the cache boundary as a contract; everything before the breakpoint byte-stable across all turns; everything after is the mutable hot zone.*

## Controller-worker consensus + the 35-minute rule
Planner-worker decisively won in production 2026 (Cursor/Codex/Claude Code/Devin/Windsurf/Grok all shipped multi-agent in the same window); cost arg: planner calls frontier once, workers use cheap models → ~90% cut. **"35-minute rule":** LLM performance degrades measurably after ~35min wall-clock; failure rate grows non-linearly (2× duration → 4× failure). Controller-worker mitigates — each worker gets fresh, narrow-scope context. Context isolation = a correctness property (one worker's errors don't drift into another).

## What flywheel should STEAL
1. Deterministic fixed-prefix state digest assembled from durable artifacts (Ralph + Codified) — NOT a summary, a structured read of ground-truth; cache stability + restart resilience fall out free.
2. Explicit stable-prefix / hot-zone separation + monitor cache-hit rate as a first-class metric (Aider + Don't-Break-the-Cache).
3. Fresh-tail + retrievable archive (Hermes-LCM): last N turns verbatim, older externalized to a queryable store the agent pulls via a tool call.
4. Controller-worker decomposition (consensus): orchestrator holds goal+digest; workers get fresh scoped context, crash cheaply.
5. Escape-hatch + max-iterations (Ralph): every loop needs a hard cap + "if stuck after N, emit a structured blocker report."

## What flywheel should AVOID
1. LLM-generated summaries as the continuity medium (OpenHands condenser, CAT long-term) — hallucinate, degrade under schema evolution, non-deterministic across model versions.
2. Sliding-window message dropping — destroys cache + can drop active tool-call chains.
3. Compression at restart (Hermes bug) — can inject the original first message as if no progress → infinite loop. Restart logic must CONSUME the digest, not re-compress history.
4. Dynamic system-prompt mutation (timestamps/run-ids) — invalidates the cache prefix every call.
5. Single long-lived agent for multi-hour tasks — 35-min rule; the field has moved on.

## Sources
ghuntley.com/ralph; github.com/anthropics/claude-code plugins/ralph-wiggum; docs.openhands.dev/sdk/guides/context-condenser; aider.chat/docs/usage/caching + repomap; arXiv 2512.22087, 2601.06007, 2602.20478; github.com/stephenschoettler/hermes-lcm; langchain.com/blog/context-engineering-for-agents.
