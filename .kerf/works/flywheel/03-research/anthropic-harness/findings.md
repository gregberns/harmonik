# Research — Anthropic harness design & prompt-caching mechanics

> Component: `anthropic-harness`. Source: research sub-agent (sonnet), 2026-05-27. Primary sources: Anthropic engineering blog + platform docs (URLs below).

## TL;DR
- **Anthropic's canonical answer to "run beyond one context window" IS flywheel's design**: *"Context resets — clearing the context window entirely and starting a fresh agent, combined with a structured handoff that carries the previous agent's state and the next steps."* Compaction is offered only as a fallback and repeatedly flagged lossy. **No conflict with our no-compaction constraint — we are aligned with their published guidance.**
- **Caching mechanics confirmed**: cache breakpoint goes on the LAST STABLE block; hierarchy tools→system→messages; exact-prefix (byte-identical) match; 5-min TTL default (1-hour at 2× write); read = 0.1×, write = 1.25× (5m)/2× (1h); 20-block lookback window; **min cacheable length = 4096 tokens on Opus 4.x, 1024 on Sonnet** (below it, `cache_control` is silently ignored).
- **30-40-line instruction rule corroborated** ("right altitude — heuristics not brittle hardcoded logic"); fetch-on-demand endorsed; externalized-state-in-files is their main recommended pattern.

## Caching mechanics (load-bearing) — src: platform.claude.com/docs prompt-caching
- `cache_control: {type:"ephemeral"}` on a content block writes one cache entry = hash of the entire prefix up to & including that block. Up to **4 explicit breakpoints**.
- **Exact prefix match**: "Cache hits require 100% identical prompt segments… up to and including the block marked with cache control." One char change anywhere in the prefix → miss.
- **Golden rule**: static content first (tools, system, context, examples), volatile last; **put the breakpoint on the last block that stays identical, NOT the varying block** (the most common documented mistake).
- **Hierarchy + invalidation**: order is tools → system → messages; a change at a level invalidates that level and all downstream. Modifying ANY tool definition (name/desc/params) nukes the whole cache. Toggling web search/citations changes system → invalidates system+messages. Changing thinking budget / image presence / tool_choice invalidates message blocks.
- **Lookback = 20 blocks** per breakpoint; a growing conversation that pushes the breakpoint >20 blocks back misses → mitigate with a 2nd breakpoint near the growing edge.
- **TTL**: 5-min default, refreshed free on each use; 1-hour option at 2× write cost. Read = 0.1× base input; write = 1.25× (5m).
- **Min cacheable**: Opus 4.7/4.6 = 4096 tokens; Sonnet 4.6 = 1024. Below min → `cache_control` silently ignored (no error). ← **important: the fixed prefix must clear 4096 tokens to be cacheable on Opus.**
- **Sliding-window dropping is forbidden BY THE MECHANICS** — dropping middle messages changes the prefix hash → guaranteed miss. Validates our constraint.
- Thinking blocks can't be cached via `cache_control` directly but cache alongside other content in prior assistant turns.

## Beyond-one-context-window guidance (the core)
- **Context reset + structured handoff** (harness-design article): fresh agent reads a `claude-progress.txt` + git history; no summarized blob. *This is flywheel.*
- **Managed Agents** (managed-agents article): an **append-only event session log OUTSIDE the context window**, accessed via `getEvents()` (can rewind a few events before a moment); harness rebuilds context from durable events, not summaries. Crash-safe via `wake(sessionId)`/`getSession(id)` replay. Calls compaction "irreversible data loss from selective token retention."
- **Memory tool** (context-management blog): Claude CRUD on a dedicated memory dir persisting across conversations; + **context editing** automatically clears stale tool call/result pairs near the limit (84% token reduction over 100 turns; +39% vs baseline w/ memory). Note: context editing drops stale *tool pairs only*, not instructions/user messages.
- **Multi-agent research system**: lead agent saves plan to Memory; subagents store work in external systems and pass **lightweight references** back to the coordinator. → favors orchestrator-with-small-context + externalized worker outputs.

## Advice mapped to flywheel
- Q (state across resets, no summarization): file-based handoff / external session log = recommended. ✓ aligned.
- Q (fixed instructions + fetch): endorsed ("right altitude"); `init.sh`-style "next agent reads it at start" saves tokens every session. ✓ corroborates Greg's 30-40-line rule.
- Q (externalize state to files): their MAIN pattern — agents keep "lightweight identifiers (file paths, stored queries, links)" and load on demand. File reads land as cacheable tool-results; if stable, put a breakpoint after them.
- Q (compaction): legitimate but "overly aggressive compaction → loss of subtle but critical context"; produces "context anxiety." Not mandated. **AVOID for flywheel — consistent with their own preference.**
- AVOID: any message-list mutation beyond stale tool pairs → poisons cache. The only safe shrink = fresh context + stable prefix + file-based digest as volatile suffix = flywheel.

## Gaps / footguns flagged
- No published Anthropic pattern named "fixed prefix + small digest" — flywheel goes further than any single published rec, but every mechanic supports it.
- **5-min TTL is the real loop constraint**: if cycle gap >5 min, prefix cache goes cold and write cost re-fires → evaluate 1-hour TTL or a keep-warm ping (hand to prompt-caching-economics thread).
- **20-block lookback footgun** for long histories → keep conversation short (fresh context per cycle) or add a 2nd breakpoint near the edge.

## Sources
- anthropic.com/engineering/harness-design-long-running-apps · /effective-harnesses-for-long-running-agents · /effective-context-engineering-for-ai-agents · /managed-agents · /multi-agent-research-system
- claude.com/blog/context-management · platform.claude.com/docs/en/docs/build-with-claude/prompt-caching
