# Thread 2 — Token Efficiency + Cost

**Status:** in progress · **Reports to:** admiral
**Prices as of:** 2026-06-24 (claude-api skill cache) — confirm live at `platform.claude.com/docs/en/pricing.md`.

## 2.1 Model price table (the models harmonik can dispatch to)

| Model | ID | Input $/Mtok | Output $/Mtok | Role in harmonik |
|---|---|---:|---:|---|
| Claude Fable 5 | `claude-fable-5` | 10.00 | 50.00 | "most capable", only on explicit ask |
| Claude Opus 4.8 | `claude-opus-4-8` | 5.00 | 25.00 | **default** implementer/orchestrator |
| Claude Sonnet 5 | `claude-sonnet-5` | 3.00 (2.00 intro→08-31) | 15.00 (10.00 intro) | balanced, near-Opus |
| Claude Haiku 4.5 | `claude-haiku-4-5` | 1.00 | 5.00 | fast/cheap (status polling, mechanical) |
| Pi / OpenRouter MiniMax M2 | `openrouter/minimax/…` | 0.255 | 1.02 | bulk cheap agentic (eval pass) |
| Pi / OpenRouter Qwen3-Coder 480B | `openrouter/qwen/qwen3-coder` | 0.22 | 1.80 | open-weight, config default |
| Pi / ornith (DGX vLLM Qwen3-Coder) | local `base_url` | **~0 marginal** | ~0 | self-hosted; cost = amortized GPU |
| Codex | (inside Codex CLI) | — | — | model not surfaced; no cost computed |

**Cache pricing (not per-model):** writes 1.25× base input (5m TTL) / 2× (1h TTL); reads ~0.1× base input.
Break-even: cache-write pays off at ≥2 reads (5m) / ≥3 reads (1h). Min cacheable prefix 4096 tok (Opus/Haiku), 2048 (Fable/Sonnet).

**Spread that matters:** Opus is **~20×** the input price and **~14–25×** the output price of the Pi/open-weight
tier, and **5×** Haiku. This spread is the entire economic reason to route (thread 5) rather than run Opus on everything.

## 2.2 How harmonik measures per-task cost today

- **`internal/sessiondata/sessiondata.go`** — always-on collector, fires from `emitDone` on every run.
  Appends one `Record` per run to `.harmonik/session-data.jsonl`. `harmonik usage` reads it.
  - `Record`: `run_id, bead_id, queue_id, harness, model, success, started_at, ended_at, wall_time_s,`
    `nodes[], tokens_total, cost_usd (*float64), turn_count, commit_sha`.
  - `NodeRecord`: `node_id, wall_time_s, tokens` (per-node attribution by dispatch-time window for DOT runs).
  - `TokenUsage`: `input, output, cache_creation, cache_read`.
  - Token source = `usage` block of each assistant turn in the **Claude transcript JSONL**.
    ⚠️ **Pi/Codex token extraction NOT implemented (P2)** — so cost data today is Claude-only. Any routing
    that wants to compare Opus-vs-Pi *cost-per-outcome* is blind on the Pi side until this lands.
- **`cost_usd` = tokens × price** via `ComputeCost(u, model)` against an in-repo `pricingTable`. `nil` when
  model is empty (codex) or has no price entry (ornith/Pi).
- **`.harmonik/metrics.json`** = quality feeders only (gofmt/vet/gocyclo/todo/diff-lines/within-budget/…), **no cost fields**.
- **`ccusage`** (`npx ccusage`) = external cross-check on the same transcripts; not wired in, lacks
  productive/idle split and per-outcome attribution (which is why sessiondata exists).

## 2.3 🔴 LOAD-BEARING BUG — the in-repo price table is stale (surface to admiral)

`sessiondata.go:66-77` prices **`claude-opus-4-8` at $15 / $75** input/output (Opus-3-era), but current
pricing is **$5 / $25**. Consequences:
- **Every `cost_usd` emitted for an Opus-4.8 run is overstated ~3×.**
- The table is **missing** `claude-fable-5`, `claude-sonnet-5`, and `claude-haiku-4-5` (only has legacy
  `haiku-4-8` + sonnet-4) → those models emit `cost_usd: nil` (no cost recorded at all).
- Any Pareto/cost analysis (thread 4) built on current `session-data.jsonl` cost figures is **wrong**
  until this table is corrected to §2.1.

**Proposed fix (for admiral to dispatch):** update `pricingTable` to §2.1 values; add Fable-5/Sonnet-5/
Haiku-4.5 entries; ideally make it config-loadable (not hard-coded) so price changes don't need a rebuild.
This is a small, well-scoped bead. *shannon proposes; admiral decides/dispatches.*

## 2.3b Cache economics + subscription reality (measured 2026-07-05)

Two facts dominate the *actual* cost picture and must front-load any analysis:

**(1) We're ~94% cached.** From our own `.harmonik/session-data.jsonl` (76 daemon runs, all model=`sonnet`):
- uncached input 291K · cache_creation 32.6M · cache_read **551M** · output 6.2M.
- **cache_read = 94.4% of all input-side tokens.** ccusage (machine-wide) agrees: cache_read dwarfs
  fresh input every day (e.g. 07-04: 2.95B cache_read vs 1.4M input).
- Cache read is **~0.1× input price**, so effective input cost collapses. Caching saves **~79%** vs a
  no-cache counterfactual at every tier:

| Tier | Notional (76 runs, cached) | $/run | If NO caching | Cache saves |
|---|---:|---:|---:|---:|
| Haiku-4.5 | $127 | $1.67 | $615 | 79% |
| Sonnet-5 | $382 | $5.02 | $1,845 | 79% |
| Opus-4.8 | $636 | $8.37 | $3,075 | 79% |

→ **Cost analysis MUST price the four token classes separately** (uncached-in / cache-write / cache-read /
out), not lump "input." The stale table in 2.3 does have cache columns, but wrong values.

**(2) We pay a Claude *subscription*, not per-token.** So the real marginal $ of a Claude run is ≈ $0 up to
plan limits — the numbers above and ccusage's totals are the **notional API-equivalent** ("what we'd spend
on metered tokens"), useful for (a) comparing Claude-tier vs metered open-weight/Pi routing, and (b)
sizing when subscription headroom runs out. ccusage machine-wide notional over the current window:
**~$14,011 total, ~$1,164 on the 07-04 peak day** (all Claude Code sessions, not just the daemon).

**Routing implication:** the cost lever is NOT "Opus→Sonnet within Claude" (all ~$0 marginal on the
subscription) — it's **Claude(subscription, ~$0 marginal) vs metered Pi/open-weight (real $/token) vs
self-hosted ornith (~$0 marginal, fixed GPU cost)**. On a subscription, the reason to route *down* within
Claude is **plan-limit headroom + latency/throughput**, not dollars; the reason to route *out* to metered
models is only if it buys throughput the subscription can't. This reframes thread 3/5 — flagged for admiral.

## 2.4 Cost is an operator decision — questions for admiral (`--topic question`)

The dispatch path has **no budget / willingness-to-pay knobs** today. Operator must set:
1. **Per-task-tier budget ceilings** — max $/bead per tier (trivial fix vs cross-subsystem refactor vs scenario-test author)?
2. **Model-to-tier policy** — which tiers justify Opus ($5/$25) vs Sonnet ($3/$15) vs Haiku ($1/$5) vs self-hosted qwen3-coder (~$0)?
3. **Willingness-to-pay for the quality delta** — how many extra $/task is a higher tier's success-rate lift worth? (feeds cost-per-outcome, thread 3)
4. **Self-hosted vs API break-even** — at what run volume does DGX/ornith fixed cost beat per-token API pricing?
5. **Cache-economics posture** — pay the 1.25×/2× write premium given harmonik's re-dispatch patterns?

## Open items
- [ ] Land the pricingTable fix (2.3) — prerequisite for any trustworthy cost figure.
- [ ] Land Pi/Codex token extraction (P2) — prerequisite for cross-harness cost-per-outcome.
- [ ] Get operator answers to 2.4 before proposing concrete routing thresholds in thread 5.
