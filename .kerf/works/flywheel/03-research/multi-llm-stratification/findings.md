# Research/Design — Multi-LLM stratification

> Component: `multi-llm-stratification`. Round-3. Source: sub-agent (sonnet), Anthropic pricing docs 2026-05-30 verified.

## TL;DR
- **Stratify from day 1.** Haiku-for-flush / Sonnet-for-normal / Opus-for-judgment cuts Yegge's 50-80% premium-model waste immediately; routing hook is ~10 lines.
- **Default Sonnet 4.6** (session); **Opus 4.7** = deliberate one-turn upgrade; **Haiku 4.5** for mechanical flushes. Matches natural flywheel turn distribution.
- **Pin a separate 4096-tok Opus prefix cache** when entering judgment turns. One cache miss ≈ $0.30 at Opus on a 40k-tok context — acceptable once; pin it if ≥2 judgment turns/session.

## Decision classes
| Tier | Label | Examples | Impl | Frequency |
|---|---|---|---|---|
| 0 | Routine | heartbeat note, bookmark advance, happy-path ACK | deterministic, no LLM | very high |
| 1 | Normal | compose batch from `kerf next` top-N, classify a verdict, format short note | Haiku 4.5 | high |
| 2 | Triage | one bead failed twice → investigate vs re-dispatch, draft follow-up bead body | Sonnet 4.6 (default) | occasional |
| 3 | Judgment | root-cause across multiple events, pattern_detected analysis, ambiguous priority override, escalation message | Opus 4.7 | rare |

## Pricing (Anthropic, 2026-05-30 verified)
| Model | Input | 5-min write | 1-hr write | Read | Output | Min cacheable |
|---|---|---|---|---|---|---|
| Haiku 4.5 | $1.00/MTok | $1.25 | $2.00 | $0.10 | $5.00 | **4096 tok** |
| Sonnet 4.6 | $3.00 | $3.75 | $6.00 | $0.30 | $15.00 | **1024 tok** |
| Opus 4.7 | $5.00 | $6.25 | $10.00 | $0.50 | $25.00 | **4096 tok** |

**Opus 4.7 tokenizer:** up to **+35% tokens** for same text vs 4.6. Effective input ≈ $5×1.35 = **$6.75/MTok**; output ≈ $33.75 equivalent. Use 1.2× expected / 1.35× ceiling in budget forecasts.

## Routing — `prepareNextTurn` shape (~10 lines)
```typescript
function prepareNextTurn(ctx, digest, wakeEvent) {
  if (wakeEvent.tier === 0) return { skip: true };
  if (wakeEvent.tier === 1 && !digest.exception_flag)
    return { model: "claude-haiku-4-5-20251001", thinkingLevel: "none" };
  if (digest.exception_flag
      || wakeEvent.cause === "bead_failed_twice"
      || wakeEvent.cause === "pattern_detected"
      || wakeEvent.cause === "escalate_user") {
    digest.exception_flag = false;            // one-shot
    return { model: "claude-opus-4-7-20260219", thinkingLevel: "high",
             cacheNamespace: "judgment" };
  }
  return { model: "claude-sonnet-4-6-20251022", thinkingLevel: "low" };
}
```
- `wakeEvent.cause` from event-bridge classification is primary signal.
- `exception_flag` is one-shot (prevents runaway Opus chains).
- `cacheNamespace: "judgment"` = separate prefix-cache block for Opus.

## Cache interaction (cold-start cost)
Cache is **per-model**. Warm Sonnet 40k-tok context: $0.30×0.1 = $0.03/turn. Switching to Opus cold: first 40k Opus read = full cache write at $6.25/MTok → **~$0.25-0.30 one-time** (with 1.2× tokenizer ≈ 48k tok). Subsequent Opus reads at $0.50/MTok = $0.024/turn.
**Policy:** 1 judgment turn/session burst → eat cold start ($0.30 is noise). 2+ judgment turns in a row (batch autopsy on 5 failures) → pre-prime the Opus cache at session start; amortized 5 turns: $0.30 write + 4×$0.024 reads = $0.40 vs $1.50 if not cached. **Rule:** pre-prime if expecting ≥3 Tier-3 turns/session.
Haiku/Opus require **4096 min** cacheable → judgment prefix must clear 4k tokens or caching silently skips.

## Thinking-level dial
| Tier | Model | `thinkingLevel` | Budget tokens |
|---|---|---|---|
| 1 | Haiku | `none` | 0 |
| 2 | Sonnet | `low` | ~2000 |
| 3 | Opus | `high` | ~8000 |
Opus high adds ~8000 output tok × $25/MTok = $0.20/judgment turn.

## Budget policy + kill-switch
```typescript
function applyBudgetPressure(config, budget) {
  const r = budget.spent / budget.limit;
  if (r >= 1.0) return { halt: true, reason: "daily_budget_exhausted" };
  if (r >= 0.80 && config.model.startsWith("claude-opus"))
    return { ...config, model: "claude-sonnet-4-6-20251022",
             note: "budget_pressure_downgrade" };
  if (r >= 0.90 && config.model.startsWith("claude-sonnet") && config.tier <= 2)
    return { ...config, model: "claude-haiku-4-5-20251001" };
  return config;
}
```
Emit transitions as events to `.harmonik/events/events.jsonl` for orchestrator observability.

## Pure-Sonnet v1 vs stratify-from-day-1
Pure-Sonnet pros: zero routing complexity, predictable. Right if session volume is low (<20 turns/day).
Stratify pros: routing hook is ~10 lines, Yegge waste is a confirmed pattern, Haiku is 5× cheaper input than Sonnet ("take `kerf next` top-5 and format as `harmonik run`" isn't hard reasoning), within $200/mo: pure-Sonnet at flywheel volume (50-100 turns/day × ~10k tok avg) = ~$15-30/day → could hit daily cap. Stratification cuts 40-60%.
**Recommend: stratify from day 1.** Routing trivial, savings immediate, deferring it = wasted unconstrained loop.

## Sources
platform.claude.com/docs/en/about-claude/pricing; .../build-with-claude/prompt-caching; finout.io blog on Opus 4.7 pricing; cloudzero blog 2026.
