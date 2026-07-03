# Research — Turn/sub-turn structure & cache-safe context management

> Component: `turn-structure-and-cache`. Round-2. Source: sub-agent (opus), Pi `agent-loop.ts` + Anthropic caching docs, 2026-05-27. Directly resolves the user's two objections: (A) "modify context every model call?" and (B) "won't a changing digest bust the cache?"

## TL;DR
- **(A) You do NOT rewrite context every model-call.** A *turn* = one external input that spins an inner loop of N model-calls (model → tool calls → tool results → model → …) until the model emits no more tool calls. Context management fires **only at turn boundaries**. Pi enforces this: steering/follow-up injection + `shouldStopAfterTurn`/`prepareNextTurn` all run AFTER `turn_end`, never inside a tool batch (`agent-loop.ts:174-253`). Editing mid-turn orphans a toolCall/toolResult pair the model is mid-reasoning about and the API rejects it.
- **(B) A changing digest does NOT bust the cache — if it sits AFTER the cache breakpoint.** Anthropic caches `tools → system → messages` up to & including the `cache_control` block. Everything after is full-price input but NOT in the cache hash → changing it cannot invalidate the prefix. Put stable prefix (identity + instructions + tool schemas) above the breakpoint; volatile digest + conversation below.
- **Cost shape:** fresh digest each turn = full-price on a few hundred–low-thousand tokens; the multi-KB stable prefix keeps hitting cache at 0.1x. A reset re-sends a byte-identical prefix → cache HIT. So agent-triggered resets are both reasoning-safe AND cache-friendly.

## 1. Anatomy of a turn (confirmed vs Pi)
Two nested loops in `runLoop` (`agent-loop.ts:155-269`): outer (`while(true)`, :170) re-enters only when `getFollowUpMessages()` returns work after the agent would stop (:257); inner (`while hasMoreToolCalls || pendingMessages>0`, :174) is the model-call engine — each pass: one `streamAssistantResponse` model-call (:193) → filter tool calls (:203) → `executeToolCalls` pushes ToolResult messages into `currentContext.messages` (:208-215) → if tools were called, iterate (another model-call with results appended). **One turn = 1–N model-calls** (a coding turn is easily 5–30). Between model-calls, only the assistant message + its toolResult(s) are appended; nothing else mutates the array. This IS the "make a bunch of tool calls and think" sequence — you cannot touch context between these inner iterations.

## 2. Where context management belongs: turn boundaries ONLY
The boundary sits at `turn_end` (:218) through :253: `prepareNextTurn` (:226, can swap whole context/model/thinkingLevel), `shouldStopAfterTurn` (:241, docstring: "end gracefully before context gets too full"), `getSteeringMessages` (:253) / `getFollowUpMessages` (:257) — injected as pending, flushed at the top of the next inner iteration (:181), before a model-call, never between a toolCall and its toolResult.
**Why mid-turn edits corrupt reasoning:** the Anthropic protocol requires every `tool_use` block to be answered by a matching `tool_result` in the next message. Dropping/rewriting a turn's pair while `hasMoreToolCalls` is true → malformed sequence (API reject) OR erases observations the model is chaining ("thinks" it ran a tool whose result vanished). A tool batch is one indivisible reasoning unit. Turn boundaries are the only safe seams (model has closed reasoning, conversation is consistent/replayable).
`transformContext` (:282, per-model-call) is the ONE mid-turn hook → use ONLY for idempotent append-only shaping (e.g. regenerate an after-breakpoint digest block), NEVER for pruning the live conversation. Structural trimming/reset → `prepareNextTurn`/`shouldStopAfterTurn`.

## 3. Cache layout (resolves objection B)
Anthropic caches the contiguous prefix `tools → system → messages` up to & including the `cache_control` block; everything after is full-price but NOT in the cache hash → changes freely each turn with zero prefix effect. Flywheel layout:
```
[ tool schemas ]                    <- stable
[ system: identity + instructions ] <- stable
   --- cache_control breakpoint ---
[ volatile: status digest ]         <- regenerated each turn (full price, no bust)
[ conversation turns ]              <- grows; also after breakpoint
```
**Quantified (Opus: base $5/MTok, read 0.1x=$0.50, 5-min write 1.25x=$6.25):** digest IN the cached region (wrong) → every change re-hashes the prefix → full re-write of the whole prefix each turn (20K-tok prefix ≈ $0.125/turn, forever). Digest AFTER the breakpoint (right) → prefix written once (~$0.125), then READ at 0.1x each turn (≈$0.01) + full price only on the ~500-tok digest (~$0.0025) → ~$0.0125/turn vs ~$0.125 ≈ **10x reduction**. NOTE: Pi's default also rolls a breakpoint onto the last user message (`anthropic.ts:916,948,1152,1203`) to cache history via the 20-block lookback; for flywheel use a FIXED breakpoint after the stable prefix (never moves, always hits) + optionally a 2nd rolling one after the conversation (up to 4 allowed).

## 4. Cache on reset vs incremental trim
**Full reset** = `[stable prefix | fixed breakpoint | fresh digest | seed]` → prefix bytes identical → **cache HIT at 0.1x** (within TTL: 5-min default / 1-hr at 2x writes). Cheapest recovery. **Incremental trim** (dropping middle conversation blocks after the breakpoint): fixed prefix still hits, BUT deleting a middle block changes every subsequent block's cumulative hash → post-trim conversation re-read at full price (+ risk pushing the rolling breakpoint >20 blocks past last write → silent lookback miss). **Rule: trim the tail, RESET the middle.** Once you'd rewrite the middle, a clean reset is simpler and usually cheaper.

## 5. Reconcile with self-management
Agent-triggered reset composes cleanly: the reset tool call is part of a normal turn → returns a toolResult → model emits no more tool calls → inner loop ends → `turn_end` → harness handles reset at the boundary (`shouldStopAfterTurn` true to end the run, or `prepareNextTurn` swaps a freshly-built context). Because consumed at a boundary, the conversation is consistent (no dangling tool calls) and the rebuilt context reuses the byte-identical prefix → cache HIT. **Agent-triggered reset = turn-boundary op = reasoning-safe AND cache-safe.** Agent decides WHEN; harness executes the rebuild BETWEEN turns.

## 6. Practical rule set
1. **One FIXED `cache_control` breakpoint** immediately after the stable prefix (tools + system identity/instructions); optionally a 2nd rolling breakpoint after the conversation (≤4 total).
2. **Above the breakpoint (cached, stable):** tool schemas, identity, immutable operating instructions. Nothing per-turn here.
3. **Below the breakpoint (volatile, full-price, never busts prefix):** status digest (its own block) then conversation turns.
4. **Management fires at turn boundaries only** (`prepareNextTurn`/`shouldStopAfterTurn`); never inside the inner tool-call loop; `transformContext` may regenerate the after-breakpoint digest idempotently but must not prune.
5. **Digest injection:** regenerate fresh at each turn boundary, place after the fixed breakpoint — full price on the digest only, zero prefix invalidation.
6. **Reset > trim for deep cleanup** (identical prefix re-hits at 0.1x); trim only the tail; agent-triggered resets run at the next turn boundary, reasoning- and cache-safe.

## Citations
Pi: `agent-loop.ts:155-269,282-286`; `types.ts:186,204-217`; `anthropic.ts:896,916-934,948,1152-1174,1203`. Anthropic prompt-caching docs (prefix=tools→system→messages to breakpoint; post-breakpoint full-price not-in-hash; read 0.1x / 5-min write 1.25x / 1-hr 2x; min prefix 1024–4096 tok; ≤4 breakpoints, 20-block lookback; free refresh on hit).
