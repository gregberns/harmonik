# Research — Pi self-management capability audit

> Component: `pi-self-management`. Round-2. Source: sub-agent (opus) deep-read of earendil-works/pi (the `pi-coding-agent` SDK + `pi-agent-core` + `pi-tui`), 2026-05-27. Answers the user's "Pi has a TON of customizability — what if they already support this?"

## TL;DR (does Pi support agent-self-managed context?)
- **PARTIAL, strongly leaning YES — NO FORK needed for v1.** Pi exposes context-size + a compaction trigger + a clean tool/extension surface on its **public API**. Out-of-the-box it doesn't hand the *model itself* a tool to read its token count or trigger its own compaction, but both are ~50-line glue jobs over stable public APIs (one extension). The extension `execute(ctx)` already carries `ctx.getContextUsage()` and `ctx.compact()`.
- **"Write notes before reset, continue across reset" is natively supported:** `session_before_compact` hook (override/cancel the compaction payload) + `appendEntry()`/`sendMessage()` durable custom entries + `newSession({setup, withSession})` clean reseed. Pi's built-in summarizer prompt is even already a structured "Goal / Progress / Next Steps / Critical Context" digest.
- **The one sharp edge:** the agent-callable `compact()` **aborts the in-flight turn** (`agent-session.ts:1633` `_disconnectFromAgent(); await this.abort()`). So a tool calling `ctx.compact()` mid-think interrupts itself → use **deferred compaction**: the tool records intent; the actual reset fires at the **turn boundary** (where Pi's auto-compaction already runs, after `agent_end`, before next prompt, `agent-session.ts:1780`).

## Q1 — Context-size awareness (the model can know how full it is)
`getContextUsage(): {tokens, contextWindow, percent}` on `AgentSession` (`coding-agent/src/core/agent-session.ts:2953`) + on the extension ctx (`extensions/types.ts:322`, `ContextUsage` `:281`). Token math prefers REAL provider usage from the last assistant message (`agent/src/harness/compaction/compaction.ts:165`), summing input+output+cacheRead+cacheWrite (`:119`) — total context, not just cache. Model can't see it by default → make it self-aware via (a) a `get_context_usage` tool returning `ctx.getContextUsage()`, or (b) inject the % into context each turn via the per-call `context`/`transformContext` hook (read-only injection is fine).

## Q2 — Agent-triggered reset/compact
Host primitives exist, one wrapper from model-facing: `AgentHarness.compact(customInstructions?)` (`agent-harness.ts:708`), `AgentSession.compact()` (`agent-session.ts:1633`), `ctx.compact(options)` (fire-and-forget, `agent-session.ts:2249`). A custom tool's `execute()` gets `ctx` → implement the user's exact idea: tool first writes notes (`sendMessage`/`appendEntry`), then triggers reset. **But honor the abort caveat** → record a flag + let the turn-boundary auto-compactor run, OR accept the abort as the reset.

## Q3 — Hook surface: per-call vs per-turn (resolves "modify context every model call?")
A *turn* = one assistant message + its tool batch (`agent-loop.ts:170-254`, inner loop).
- `transformContext(messages, signal)` — **per model-call**, before `convertToLlm` (`agent-loop.ts:282`). **DANGER ZONE** — fires on every LLM call incl. mid-tool-sequence. Use for READ-ONLY injection (e.g. inject context-% or digest), NEVER destructive pruning.
- `prepareNextTurn(ctx)` — **per turn**, after `turn_end` (`agent-loop.ts:220`). Returns replacement context/model/thinkingLevel. **Safe seam to swap context.**
- `shouldStopAfterTurn(ctx)` — **per turn**, after `turn_end` (`agent-loop.ts:241`; contract `agent/src/types.ts:198`). Docs say explicitly: "Use this to request a graceful stop … before context gets too full." **The clean stop→reseed trigger.**
- `getSteeringMessages()` (drains after each tool batch, before next assistant call) / `getFollowUpMessages()` (drains only when agent would stop) — `agent-loop.ts:174,256`.
- `session_before_compact` — fires only inside `compact()` (host or turn-boundary auto), NEVER mid-think (`agent-harness.ts:723`). Can `cancel` or supply a custom `compaction` payload → **where you force the harmonik digest format.**
- Also `beforeToolCall`/`afterToolCall` (per tool), `before_agent_start` (per prompt).
**Rule: manage context in `shouldStopAfterTurn` + `prepareNextTurn` + `session_before_compact`; never in `transformContext`.**

## Q4 — Custom tools (first-class)
Register via `createAgentSession({customTools: ToolDefinition[]})` (`sdk.ts:71`) or `pi.registerTool()` (`extensions/types.ts:1134`). Tool `execute(id, params, signal, onUpdate, ctx)` gets `ctx: ExtensionContext` (`:455`) → can `ctx.compact()`, `getContextUsage()`, `abort()`, `shutdown()`, and return `terminate:true` to stop the batch (`agent/src/types.ts:344`). So `note`, `reset_my_context`, `harmonik_dispatch` are all straightforward. Tools carry `promptSnippet`/`promptGuidelines` to self-document into the system prompt (`:433`).

## Q5 — Event injection into a live session (no respawn) — standout strength
Queues: `steer()` (inject before next assistant call), `followUp()` (inject when agent would stop), `nextTurn()` — on `Agent` (`agent.ts:264`) and `AgentSession` (`:1204`). Host/extension: `pi.sendMessage(msg, {deliverAs:"steer"|"followUp"|"nextTurn", triggerTurn})` / `pi.sendUserMessage()` (`extensions/types.ts:1179`). A background process can enqueue into a streaming session WITHOUT respawning. **RPC mode** (`modes/rpc/rpc-types.ts`): JSON-lines-on-stdin headless protocol — `prompt` (w/ `streamingBehavior: steer|followUp`), `steer`, `follow_up`, `abort`, `compact`, `new_session`, `get_state` (`:19-69`). **Cleanest external-event channel for flywheel — a long-running `pi --mode rpc` fed daemon events.** Interrupt via `ctx.abort()`.

## Q6 — Persistence / restart
Session = JSONL **tree** (header `version:3` + entry lines; `jsonl-storage.ts:59,87`, append-only). Entry types: message, compaction, branch_summary, **custom/custom_message** (the notes vehicle, `appendEntry()` `extensions/types.ts:1194`), label, leaf (`harness/types.ts:409`). Resume: fresh process → `createAgentSession()` → `buildSessionContext()` → `agent.state.messages = existingSession.messages` (`sdk.ts:225,396`). Clean reseed: `ctx.newSession({parentSession, setup, withSession})` (`extensions/types.ts:337`) → fresh JSONL seeded with digest+notes before continuing = the "clean restart with a digest" we want. `fork()`/`navigateTree()` keep history reachable.

## Q7 — TUI (your visibility requirement)
`@earendil-works/pi-tui` = component toolkit (editor/box/markdown/select-list/etc.). Customizable from extensions WITHOUT touching tui internals: `ctx.ui.setFooter()/setHeader()/setWidget()` (above/below editor), `setStatus(key,text)`, `custom()` overlays, `setWorkingMessage` (`extensions/types.ts:124-274`). Footer factory gets git branch + extension statuses; token/model/context-% from `ctx.getContextUsage()`. → render a custom status/digest panel with durations + fullness-% cleanly.

## Bottom line — smallest path to flywheel v1 (NO FORK)
Embed `@earendil-works/pi-coding-agent` (SDK; pulls `pi-agent-core` + `pi-ai`) + write ONE extension that:
1. registers a `note` tool (durable custom entries) + a `context_usage` tool (or inject % via `transformContext` read-only);
2. uses `turn_end` + `getContextUsage().percent` threshold in turn-boundary logic to decide stop/reset;
3. on `session_before_compact`, forces the harmonik digest format (custom `compaction` payload) — or let Pi's built-in structured summarizer run;
4. drives external daemon events in via `sendMessage({deliverAs})` or RPC stdin;
5. renders the digest/status TUI panel.
**Intelligence-about-when lives in the model** (it calls `note` then `request_reset`); **boundary-safety lives in the extension** (don't reset mid-tool-call → defer to turn boundary). **Files to touch ONLY if you fork:** `agent-session.ts` (a non-aborting agent-callable compaction — the one genuine gap) + `extensions/types.ts` (add a model-facing context-usage tool to the default set). Everything else = composition over the public extension API.
