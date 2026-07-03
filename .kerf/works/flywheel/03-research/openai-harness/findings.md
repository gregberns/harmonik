# Research — OpenAI harness engineering

> Component: `openai-harness`. Source: research sub-agent (sonnet), 2026-05-27. Cross-vendor; we run on Claude, so principles transfer, platform mechanics don't.

## TL;DR
- **"Agent = Model + Harness."** The harness is every non-model piece (loop control, context selection, tool dispatch, state persistence, recovery). The 2026 shift: the harness is the primary engineering surface, not the prompt. Failures are *harness bugs to fix deterministically*, not prompts to tweak — "engineer a solution every time an agent makes a mistake so it never makes that one again." → reframes flywheel's crash/recycle as harness fixes.
- For long-horizon state, OpenAI's non-summarization approach (Strategy B) = **a fixed-prefix system prompt carrying structured, machine-written state digests (YAML/MD injected via hooks)** + aggressive trimming, NOT LLM compaction. **OpenAI does this in production** (Agents SDK `RunContextWrapper` + `MemoryHooks`).
- Codex's transferable cache insight: **prompt-cache integrity requires a static prefix before all variable content + stateless requests** (Codex deliberately avoids `previous_response_id` server-side continuation to preserve cache + compliance) — every request carries its own full history.

## Loop architecture
ReAct inner loop: state → reason → tool call → observe → repeat until terminal. Codex: incorporate input → query Responses API → assistant msg or tool call → execute, append → re-query → repeat (one "turn" can hold hundreds of tool calls). The harness owns the loop; the model is called inside it. Error/recovery (Augment Code's three layers): feedforward constraints (rules/types/lint prevent invalid actions) + feedback loops (errors reinjected for self-correction) + quality gates (staleness/dependency checks block bad outputs at commit boundaries). April-2026 Agents SDK adds first-class crash-resume bookkeeping (durable state, resume from last checkpoint without replaying full conversation) + tree-structured messages with full-text search for hours-long tasks (surgical retrieval, not full replay).

## Context / long-horizon (the relevant part)
Three strategies, only two avoid summarization:
- **A Trimming (deterministic):** Agents SDK `TrimmingSession` keeps last N turns verbatim, drops older wholesale. Zero latency, deterministic bound. Abrupt loss beyond N.
- **B Structured state injection into fixed prefix (most relevant):** typed state object (`RunContextWrapper`/dataclass) rendered as YAML+MD into the system prompt before each run via `MemoryHooks`; `inject_session_memories_next_turn` flag triggers reinsertion *after* trimming — **at the NEXT turn's prefix, not mid-turn** (avoids mid-turn prefix mutation that busts cache). Post-session consolidation (dedup, recency-wins) merges notes into global memory for the next cold-start prefix. = exactly flywheel's "fixed cache-stable prefix + deterministic digest."
- **C Compaction (`/responses/compact`):** OpenAI-specific (Responses API), LLM-dependent. **Avoid for flywheel** (and unavailable on Claude).
Universal principle: *"Memory is not a plugin you bolt on… it sits in the path between system and model on every turn."* State lives outside any single harness instance (filesystem/SQL, not RAM).

## Caching / token economy
Codex: *"With cache hits, sampling becomes linear rather than quadratic. Cache hits only occur for exact prefix matches."* Static content MUST precede variable; config changes mid-conversation (tool list/model switch) cause misses. **Flywheel implication: the fixed prefix is load-bearing for cost LINEARITY** — variable content in prefix position flips per-step cost O(1)→O(n). Even 272k/128k windows "can be overwhelmed by uncurated histories" — discipline on tool-output size is mandatory.

## Multi-agent
Orchestrator-worker is canonical (decompose → delegate → aggregate). Coordinator-Implementor-Verifier (Augment) ≈ harmonik's own model; the **living spec / structured handoff artifact is the shared state surface, not a shared context window**. April-2026 SDK: control transfer between specialized agents, sub-agents with isolated coordination, approval gates at handoff boundaries, built-in tracing. **Maps to flywheel:** orchestrator = thin routing loop, minimal context (mostly fixed prefix); workers carry task context, die freely (results externalized before death).

## Genuinely useful (not obvious) vs generic
Useful: (1) `inject_session_memories_next_turn` — reinsert state at NEXT turn, not mid-turn (cache-safe); (2) stateless-by-design despite apparent inefficiency (Codex carries full history rather than server-side continuation, for cache+compliance) — right call for flywheel; (3) tree-structured indexable artifacts (flat append insufficient for hours-long work); (4) harness-as-reliability-loop framing. Generic/known: ReAct, orchestrator-worker, "map not manual," external state persistence (harmonik already has these).

## Sources
openai.com/index/harness-engineering (403 on fetch; via decodingai.com/p/agentic-harness-engineering + zenml.io Codex-CLI analysis); pingcap.com/blog/ai-agent-harness-state-layer; developers.openai.com/cookbook (agents_sdk session_memory, context_personalization); augmentcode.com/guides/harness-engineering-ai-coding-agents; agilesoftlabs.com + community.openai.com (April-2026 Agents SDK).
