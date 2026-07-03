# Research — Claude Code SDK / headless (`claude -p`) as substrate

> Component: `claude-code-sdk-headless`. Source: claude-code-guide sub-agent over official Claude Code + Agent SDK docs, 2026-05-27. **Verdict: MAYBE — viable as a loop driver, but VIOLATES the hard no-compaction + cache-stable constraint. To honor those, drop to the raw Anthropic Messages API (which is what Pi does) — see `pi-coding-agent`.**

## TL;DR
- `claude -p` / Agent SDK give a real headless loop (streaming JSON I/O, hooks, session resume), BUT **auto-compaction CANNOT be disabled** (server-side, fires at a token threshold, rewrites history → invalidates cached prefix) and **`--resume` replays the ENTIRE transcript** (no digest-only resume). Both are fatal to flywheel's locked constraints.
- **Billing (effective 2026-06-15):** Agent SDK + `claude -p` draw from a SEPARATE monthly Agent-SDK credit pool ($20 Pro / $100 Max-5x / $200 Max-20x), NOT the interactive Max quota; on depletion it falls back to standard API per-token rates (or halts). So "headless = forfeits interactive Max" — it's a *reallocation* to the SDK credit pool, monitorable.
- **To get true no-compaction + explicit cache control you must use the raw Anthropic Messages API and build the loop yourself.** That is exactly Pi's architecture → Pi (or a custom Go/TS loop on the Messages API) is the better substrate.

## 1. Headless entry points
- `claude -p "<prompt>"` (single-shot); `--output-format stream-json --include-partial-messages` (token-level events); `--json-schema` (structured output); `--continue` / `--resume <session_id>` (append to existing transcript).
- **Agent SDK** (Python/TS): `query()` async generator yields typed lifecycle messages; native hooks (PreToolUse/PostToolUse/SessionStart/SessionEnd/UserPromptSubmit); session_id on init, `resume=session_id`. NOT a separate runtime — it forks the Claude Code binary internally.
- **Managed Agents** (REST beta): server-side persistent sessions, pay-per-token (API key), event-driven. Distinct from both; adds a hosting/billing boundary.
- For an indefinite *local* loop: `claude -p --resume` + Agent SDK is the only local option; Managed Agents is Anthropic-hosted.

## 2. Session/resume — critical limitation
Per docs (code.claude.com/docs/sessions): *"Resuming a session with `claude --continue` or `claude --resume` reopens it under the same session ID and appends new messages to the existing conversation."* There is **no light/digest-only resume**. The session JSONL at `~/.claude/projects/<project>/<id>.jsonl` grows forever (until compaction rewrites it); the full transcript is always in context at init. **flywheel's "fresh context + small digest" is NOT achievable via SDK resume — the session IS the (growing) state.**

## 3. Context control — compaction breaks the model
- how-claude-code-works: *"Claude Code manages context automatically… clears older tool outputs first, then summarizes the conversation if needed."*
- compaction docs: auto-triggers above a configurable threshold (default 150k, min 50k); **"cannot be disabled once enabled"**; rewrites history into a `compaction` block → any cache breakpoints before it go stale.
- `--no-session-persistence` makes each `-p` call stateless but then you lose loop state → defeats the purpose.
- **Conclusion: cannot honor "no LLM compaction + prompt-cache integrity" with Claude Code/Agent SDK. Compaction is server-side and not CLI-tunable.**

## 4. Prompt caching — works, but eroded by compaction/replay
- Caching DOES work across separate invocations (workspace-level, 5-min default / 1-hr; read 0.1×, write 1.25×). System prompt + tools + first N message blocks (20-block lookback) cacheable.
- BUT: resume replays full transcript (prefix grows); compaction rewrites history (prefix hash shifts → miss). Net savings modest and eroding. For truly stable cross-cycle cache you'd implement your own prefix hashing + session-less API calls = the raw Client SDK path.

## 5. Billing/auth (decisive, effective 2026-06-15)
support.claude.com article "Use the Claude Agent SDK with your Claude plan": Agent SDK + `claude -p` draw from a **separate monthly Agent-SDK credit pool** (Pro $20 / Max-5x $100 / Max-20x $200 / Team-Premium $100 / Enterprise-Premium $200), consumed *before* other sources; interactive Claude Code still uses subscription limits unchanged. On depletion → standard API rates (if usage-credits enabled) else halt; credits don't roll over; not shareable. **Not necessarily a cost increase — a reallocation** — but requires monitoring; enable usage-credits backup so the loop doesn't hang mid-cycle.

## 6. Hooks/events & crash recovery
- Hooks (PreToolUse/PostToolUse/SessionStart/SessionEnd/UserPromptSubmit) are local to the invocation — **no daemon→orchestrator pubsub**. To react to externally-arriving beads, query harmonik queue on SessionEnd and re-prompt. Effectively a manual event loop, not reactive pubsub.
- Crash recovery: session JSONL persists; resume by captured session_id. But session can grow unbounded / corrupt; no built-in in-flight-work tracking (harmonik daemon owns that).

## 7. Synthesis / decision input
| Aspect | SDK status | flywheel impact |
|---|---|---|
| Headless loop | ✓ supported | ok |
| No-compaction | ✗ can't disable | **fatal to constraint** |
| Digest-only resume | ✗ replays full transcript | **fatal to constraint** |
| Cache stability | partial; eroded by compaction | weak |
| Max billing | ✓ via separate credit pool (official) | acceptable, monitor |
| Reactive events | ✗ manual loop | workable |

**Biggest blocker:** auto-compaction is mandatory & server-side. **Alternative the agent recommends (and which the Pi findings confirm is real):** use the **raw Anthropic Messages API** (Client SDK) and build the agentic loop yourself — full context control (no auto-compaction), explicit `cache_control` breakpoints, but pay-per-token + API key (no Max). **Pi already implements exactly this loop AND reproduces the Claude-Max OAuth — so Pi gets the control of the raw API with (unofficial) Max billing. That is why Pi edges out the SDK for this project.**

## Sources
code.claude.com/docs: headless, agent-sdk/overview, sessions, how-claude-code-works · platform.claude.com/docs: build-with-claude/prompt-caching, build-with-claude/compaction, managed-agents/overview · support.claude.com article 15036540 (Agent SDK billing, eff. 2026-06-15).
