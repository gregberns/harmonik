# Thread 4 — Pareto Curve (Quality vs Cost Frontier)

**Status:** drafted (needs eval numbers from thread 1 + corrected cost from thread 2) · **Reports to:** admiral

## 4.1 What a cost–quality Pareto frontier is

Plot every model as a point in 2-D space: **x = cost per task** (or per Mtok), **y = quality** (eval score /
success-rate). A model is **Pareto-optimal** if no other model is both cheaper *and* better. The
**frontier** is the upper-left convex hull of those points — the best quality achievable at each price band.
Models *below* the hull are dominated (never rationally chosen). The formal treatment for LLMs is
**RouterBench** (arXiv 2403.12031): it takes the non-decreasing convex hull of a router's outputs as its
frontier.

## 4.2 The headline result that motivates routing

A **router's** operating curve can sit **above** the single-model frontier: by sending easy tasks to the
cheap model and hard tasks to the strong one, you get **higher quality at the same cost** (or the same
quality cheaper) than *any fixed model choice*. Published headroom: RouterBench/LLMRouterBench report
**+~4% quality over the best single model and ~30–32% cost reduction at matched quality**; FrugalGPT's
cascade reports up to **98% cost reduction** at GPT-4-matched quality on favorable workloads. This "beat
any fixed model" property is the entire economic case for thread 5.

## 4.3 Sketching OUR frontier (axes + where our models land)

- **Y-axis (quality):** per-category success-rate from the eval program (thread 1) — merge-gate pass +
  agent-reviewer APPROVE + within-budget, per task category.
- **X-axis (cost):** `cost_usd` per task from `session-data.jsonl` (thread 2) — **once the stale price
  table is fixed**; today Opus cost is 3× overstated and Pi/ornith cost is unrecorded, so the current
  frontier would be garbage.

**Expected qualitative placement** (to be replaced with measured points):

```
quality
  ^        Fable-5 ($10/$50)     ← top-right; only worth it for the hardest tasks
  |     Opus-4.8 ($5/$25)  ●     ← current default
  |   Sonnet-5 ($3/$15) ●
  | Haiku-4.5 ($1/$5) ●
  | qwen3-coder / MiniMax ($0.2–0.25 in)  ●
  | ornith (self-hosted ~$0)  ●
  +--------------------------------------------> cost
```

The open question the eval data answers: **for each task category, which of these is on the frontier?**
E.g. if Sonnet-5 matches Opus on `review` and `mechanical-edit` at 60% of the cost, Sonnet is the
frontier point for those categories and Opus is dominated there.

## 4.4 Live external frontiers to cross-check against

- **Artificial Analysis** — Intelligence-vs-Price scatter + coding index; defines "efficiency frontier".
  https://artificialanalysis.ai/leaderboards/models
- **LMArena** — Elo/price, coding arena. https://lmarena.ai
- **llm-stats.com** — 300+ models by intelligence/speed/price.

These calibrate *external* capability; but the routing decision must use **harmonik-specific** eval scores
(thread 1) because our task distribution (Go daemon work, DOT multi-node, review) is narrower than a
general benchmark.

## Open items
- [ ] Populate real (cost, quality) points per model per category once thread 1 eval numbers + thread 2
      price fix land.
- [ ] Identify, per category, the frontier model (= the cheapest model whose quality clears the bar).
- [ ] Feed the frontier into thread 5's routing table.
