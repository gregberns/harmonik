# Research — Agent self-managed context: feasibility, primitives, failure modes

> Component: `agent-self-managed-context`. Round-2. Source: sub-agent (opus), MemGPT/Letta + Anthropic docs + first principles, 2026-05-27.

## TL;DR
- **Feasible, with a caveat.** A model CAN be told its own fullness and a tool call CAN trigger a harness-driven reset. Anthropic already ships native context-awareness (**Claude Sonnet 4.5 tracks available tokens**) + a memory tool whose notes survive a reset. So "Claude doesn't support this" is **half-true: the primitives exist; the agent-decides-WHEN control loop isn't the default** (Claude Code auto-compacts on a deterministic ~95% schedule, hands the model no "reset yourself" lever).
- **Minimal mechanism (3 parts):** (1) expose a fullness signal each turn (% + instruction); (2) a `compact_context` tool the harness honors by capturing notes → fresh context seeded with [stable prefix + notes/digest] → continue; (3) a system-prompt clause "write notes BEFORE you compact."
- **Necessary backstop (load-bearing):** pure self-direction WILL fail intermittently (models skip tool calls under load). Steal **MemGPT's two-threshold safety net**: soft "memory-pressure" warning at ~70% (agent decides), **hard forced flush at ~100%** the SYSTEM executes whether or not the agent acted (after auto-persisting a digest). Judgment primary + guaranteed floor.

## Q1 — Can the model see its own size?
Channels on the Anthropic Messages API: (1) harness injects a token-count line each turn from the `usage` object (input+output+cache_read+cache_creation vs window); (2) a `get_context_usage` tool (weaker — on-demand polling is what an overloaded model forgets); (3) pre-flight `count_tokens` (harness-side); (4) **NATIVE:** Sonnet 4.5 "built-in context awareness — tracking available tokens." Reasoning quality when told: better-than-nothing but **imperfectly calibrated** — models treat "71%" as advisory and under-react under load → inject as a % WITH an explicit instruction ("at >70% you MUST flush notes + compact"), not a raw number. This is why the signal alone is insufficient (Q4).

## Q2 — Self-reset primitives (exist vs build)
"Agent resets itself" = a tool call → harness: capture just-written notes → tear down → reseed [stable instructions + notes/digest] → continue. Who built pieces:
- **Letta/MemGPT — canonical "agent manages its own memory."** OS-inspired: main context (RAM) / recall (SSD) / archival (disk). Agent edits **bounded** in-context memory blocks ("a few hundred–couple thousand tokens", deliberately capped to force curation) via tool calls in its normal loop. Closest analog to flywheel.
- **Anthropic memory tool**: client-side file dir the model CRUDs; persists across sessions; **notes survive a reset** = the "write notes first" primitive, shipping.
- **Anthropic context editing** (`clear_tool_uses_20250919`): clears oldest tool results past a threshold (84% token save / 39% perf gain on 100-turn). BUT harness-threshold-driven, not agent-triggered, AND mid-stream surgical (cache-hostile, Q5).
- **Agent SDK `compaction_control`** (`context_token_threshold`, default 100k) + **Claude Code `/compact`** (human cmd; auto ~95%): both developer/threshold-driven, not model-callable.
**Verdict:** building blocks (fullness signal, durable notes, reset op) all exist; what must be BUILT is thin glue: a model-callable `compact_context` the harness honors with a clean reseed. Small.

## Q3 — "Claude doesn't support this": exact gap
Every shipping Anthropic context mechanism is **deterministic/threshold-owned, not agent-owned.** The gap isn't capability — it's **control authority**. Minimal closure: a `compact_context(reason, keep_hint)` tool + a system-prompt clause ("you can see your fullness; when right, and always after writing notes, call it") + a harness that on the call persists notes → reseeds → resumes. flywheel is a custom harness (Pi) → it can wire exactly this.

## Q4 — Failure modes + the hybrid backstop (LOAD-BEARING)
Pure self-management fails: forgets to reset (→100%, messy force-truncate, context-rot mid-task); forgets to write notes first (under-seeded fresh context); resets too eagerly (thrash, re-derive, waste); resets too late (already degraded — the LLM-under-load tool-skip problem); bad keep/drop judgment.
**Hybrid (recommended):** agent judgment PRIMARY + a deterministic FLOOR (MemGPT):
- **~70% soft warning (agent decides):** inject "memory pressure 70%, flush notes + consider compacting." Normal path.
- **~90% escalated nudge:** stronger instruction, last chance for agent-led action.
- **~100% hard forced flush (system decides):** if agent hasn't acted, harness FORCES it — auto-extract a digest (memory files + recent turns), persist, evict, reseed. Guaranteed floor; agent judgment bypassed only as safety net.
Why pure-self is unacceptable: one missed tool call → silent degradation, no recovery. The backstop converts a correctness failure into a rare sub-optimal-but-safe auto-reset.

## Q4b — MemGPT's exact mechanism (STEAL, don't reinvent)
Queue Manager + two token thresholds: **Warning (~70%)** → inserts a memory-pressure warning so the LLM saves essentials BEFORE overflow (agent acts). **Flush (~100%)** → flushes unilaterally: evicts ~50% of the window into a **recursive summary** (new = f(old summary, evicted)) (system acts, no permission). **Heartbeats/`request_heartbeat`**: a tool flag that returns control to the agent to chain multi-step memory edits; on tool failure a heartbeat is auto-requested so it self-corrects. So MemGPT is explicitly a HYBRID (agent-prompted at 70%, system-forced at 100%) — exactly flywheel's backstop.

## Q5 — Caching interaction
Clean agent-triggered **RESET is cache-FRIENDLY; mid-stream surgical trimming is cache-HOSTILE.** Caching keys on a byte-stable prefix (read 0.1x, 5-min TTL). Surgical/incremental editing changes the prefix every request → cache miss for the whole tail. A clean reset = brand-new `[byte-stable prefix | notes/digest | fresh work]` → prefix cache HIT from turn one. → **Prefer discrete reset over incremental trimming**; stable content (system+tools+instructions) at the front with a breakpoint, volatile notes/digest after. (More cache-efficient than Anthropic's own context-editing, which trades cache for in-place savings.)

## Recommended design (one paragraph)
Expose fullness every turn (% + instruction). Give the agent a `compact_context` tool + a memory-file surface for durable notes (use Anthropic's memory tool directly, or Pi custom entries). System prompt: "write notes, then compact, when you judge it — definitely by 70%." Harness honors the tool by reseeding a fresh, prefix-cache-stable context from [stable instructions + notes digest]. Wrap in MemGPT's two-threshold floor: soft warning ~70% (agent-led), **hard forced flush ~100%** (system-led, auto-digest) so a skipped tool call can never degrade the run. Model judgment primary (Yegge); deterministic safety guaranteed (the review's demand).

## Sources
arXiv 2310.08560 (MemGPT); Letta docs (memory blocks, legacy MemGPT agents); Anthropic memory-tool + context-editing + context-management blog + Messages API usage + token-counting + Agent-SDK compaction_control + prompt-caching docs; Claude Code ~95% auto-compact.
