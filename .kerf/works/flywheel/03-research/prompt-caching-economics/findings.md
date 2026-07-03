# Research — Prompt-caching economics & loop cache/token strategy

> Component: `prompt-caching-economics`. Source: research sub-agent (opus), Anthropic docs + reasoning, 2026-05-27. The decisive, genuinely-novel surface of the design.

## TL;DR (the crux)
- **Caching is content-addressed, NOT conversation-keyed.** Cache key = cryptographic hash of the prefix (tools→system→messages, byte-identical to the breakpoint), scoped per org+workspace. A **brand-new request** with a byte-stable prefix gets a **cache HIT** within TTL → **"recycle to fresh context" is nearly free on the prefix.** CONFIRMED by docs.
- This makes **regime (B) — periodic fresh context — viable and cheap**: per-cycle cost = prefix@0.1× (cache read) + digest@1.0× + new turns. **Stays ~flat with loop age.** Regime (A), one growing context, grows linearly forever.
- The whole scheme hinges on **byte-stability of the prefix** + **TTL management**. 5-min TTL evaporates between slow cycles → use 1-hour TTL (2× write) or a keep-warm ping for cadences >5 min.

## Q1 — Cache mechanics (CONFIRMED, docs)
Up to **4 explicit** `cache_control:{type:"ephemeral"}` breakpoints. Cached prefix = start-of-request through the marked block, order tools→system→messages. **Hit rule (exact quote):** *"Cache hits require 100% identical prompt segments, including all text and images up to and including the block marked with cache control."* + **20-block lookback** per breakpoint. Pricing (Opus 4.7, base $5/MTok): 5-min write 1.25× ($6.25), 1-hr write 2.0× ($10), read 0.1× ($0.50). Break-even: 5-min cache pays off after 1 read; 1-hr after 2. **TTL refreshed free on each use** (sliding renewal). **Min cacheable: 4,096 tok Opus / 1,024 Sonnet** — below it, cache_control silently ignored (verify via `usage`: both cache_* = 0 means nothing cached). Invalidation hierarchical: tool-def change busts tools+system+messages; system change busts system+messages; any byte diff (incl. whitespace) at/before breakpoint = miss.

## Q2 — Ordering (CONFIRMED)
tools → system → messages. Static→dynamic. Everything BEFORE last breakpoint = cached (0.1×); everything AFTER = full-price input. So: immutable instruction prefix + tool defs + stable context before the last breakpoint; volatile digest + new turns after it.

## Q3 — The loop problem (crux; CONFIRMED + one inference)
- **Regime (A) one growing context:** billed input grows without bound (0.1× on an ever-larger cached prefix + 1.0× new tail; the 0.1× line climbs every cycle). Violates "must not grow with loop age."
- **Regime (B) periodic fresh context:** new request = `[immutable prefix | breakpoint | fresh digest + turns]`. **CONFIRMED a new request with a byte-identical prefix hits the cache** — keyed on cryptographic hash of prefix content, scoped org+workspace, NO conversation-id linkage. Doc: *"Caches are isolated between organizations… as of Feb 5 2026 also isolated per workspace."* Corroboration: *"Cache keys are generated using a cryptographic hash of the prompts up to the cache control point."* So regime (B) gets a prefix HIT in the new context within TTL+workspace.
- **REASONED caveat:** docs describe sharing via content-identity + org/workspace scope, never "same conversation required"; the concurrent-request note (*"a cache entry only becomes available after the first response begins"*) constrains timing, not identity. **Safest assumption:** treat prefix cache as shared by content within TTL+workspace (docs support), but (a) keep prefix byte-identical with zero per-request variation, (b) build the cost model so even a prefix MISS (full 1.25× write) on a fresh cycle is affordable → graceful degradation.

## Q4 — TTL vs cadence
5-min TTL: cycles >5 min apart with no intervening request → prefix evaporates → next fresh context pays 1.25× write not 0.1× read. Strategy menu: (1) cadence <5min → default 5-min TTL free-renewing, cheapest; (2) cadence 5-60min → 1-hr TTL (2× write once, then 0.1× reads for an hour); (3) keep-warm ping (tiny request re-reading the cached prefix before expiry, resets clock free; the read itself ~$0.0025 for a 5k-tok Opus prefix). Quant (Opus, 5k prefix): 5-min write $0.031; read $0.0025; 1-hr write $0.050 → a keep-warm read is 12× cheaper than a re-write.

## Q5 — Digest design for caching
Digest is volatile → MUST sit AFTER the last breakpoint → always full-price → minimize tokens. Format: terse fixed-schema line-oriented (`key: value`/compact), NOT prose, NOT pretty JSON. Content: deltas/counts/IDs over full objects (`ready=12 in_flight=3 failed:hk-9dnak,hk-aoz34`). Keep stable vs volatile cleanly separated. **Opus-4.7 tokenizer note (CONFIRMED):** new tokenizer may use up to **35% more tokens** for the same text → budget the digest +35% (a 300-tok digest on older models ≈ 400 on Opus 4.7).

## Q6 — Cost model (illustrative, Opus 4.7)
P=5000 prefix, D=400 digest, T=1500 new turns, O=800 output.
- **Regime (B) per cycle (warm prefix):** prefix read 5000×$0.50/M=$0.0025 + digest 400×$5/M=$0.0020 + turns 1500×$5/M=$0.0075 + output 800×$25/M=$0.020 = **≈$0.032/cycle, FLAT** regardless of loop age. First cycle adds one-time 1.25× write on P (~$0.031), or 2× for 1-hr.
- **Regime (A) at age N=50:** cached prefix ≈5000+50×1500=80,000 tok → read line alone $0.040/cycle and climbing; N=200 → ~$0.15/cycle. **Linear growth → fails the constraint.**
Regime (B) wins decisively past ~3-5 cycles; gap widens forever.

## Q7 — Pitfalls (silent cache-breakers)
1. Reordering tools / any tool name|desc|schema byte → busts whole cache (tools first in hierarchy). Freeze tool defs; never sort dynamically. 2. Nondeterministic JSON key order → use canonical serializer. 3. Timestamps/run-ids/counters in the prefix → permanent miss; push all volatile into the post-breakpoint digest. 4. Dynamic system prompt ("current time") → busts system+messages; keep system 100% static. 5. Claude Code/SDK injecting content before your breakpoint (system reminders, wrappers) → verify the on-wire prefix via `usage`, not your intended one. 6. Whitespace/trailing-newline drift from templating. 7. Sub-4096-tok prefix on Opus → never caches; pad or accept full price. 8. Workspace/org switch / Bedrock vs first-party → different scope, cold start. 9. 5-min TTL lapse between slow cycles → silent read→write downgrade; monitor `cache_read_input_tokens`. 10. Concurrent fresh-context launches before first write completes → all miss; serialize the first, then fan out.

## Recommended strategy
1. **Adopt regime (B):** every cycle = fresh request `[immutable prefix | cache_control | digest + new turns]`; never append to a long-lived context.
2. **Prefix layout (one breakpoint, last in static zone):** tools(frozen) → system(frozen prefix + stable loop context) → `cache_control:{ephemeral,ttl:"1h"}` on the last static block.
3. **Use 1-hour TTL** unless cycles reliably <5 min apart.
4. **Keep-warm only if** cycles sparse/irregular.
5. **Digest:** terse fixed-schema, deltas not payloads, ≤500 tok (budget +35% Opus 4.7), all volatile fields here.
6. **Instrument every cycle:** assert `cache_read_input_tokens ≈ prefix_size`; alert if it drops to 0 (prefix broke or TTL lapsed). **The single most important guardrail** — turns "vibes" into a measured invariant.
7. **Prefix ≥ 4,096 tok** (Opus) or it won't cache.
Result: ~constant per-cycle cost (≈$0.03 Opus), independent of loop age, no compaction, no sliding-window — exactly the locked constraints.

## Sources
platform.claude.com/docs/.../prompt-caching (mechanics, hit rule, lookback, invalidation, ordering, minimums, org/workspace scope, TTL refresh, concurrent rule); .../pricing (1.25×/2×/0.1×, Opus-4.7 tokenizer note); mager.co/blog/2026-04-29-claude-prompt-caching.
