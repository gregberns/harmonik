# Thread 3 — Task/Model Fit (Synthesis of Quality × Cost)

**Status:** framework drafted — awaits real eval numbers (thread 1) + corrected cost (thread 2) · **Reports to:** admiral

## 3.1 The decision rule

For each task category, pick the **cheapest model whose quality clears the bar for that category** — i.e.
the frontier point (thread 4) at the required quality level. "The bar" is category-specific: a
mechanical-edit needs only gate-pass; a cross-subsystem refactor needs high judge-grade *and* gate-pass.

Fit = `argmin_model cost(model)  subject to  quality(model, category) ≥ bar(category)`.

## 3.2 Fit table — HYPOTHESES to be confirmed by data (do not treat as decided)

The eval program measures only coding-implementation categories today (thread 1 §1.2 gap). These are
**priors** — replace each with the measured frontier model once `harmonik eval report` runs.

| Category | Quality bar | Prior best-fit | Rationale (to verify) |
|---|---|---|---|
| mechanical-edit (rename, fmt, single-file) | gate-pass only | **Haiku 4.5** | deterministic verifier catches errors; 5× cheaper than Opus |
| triage / classify | moderate | **Haiku / Sonnet** | short-context judgment; cheap |
| review (agent-reviewer) | high precision | **Sonnet 5** | near-Opus judgment at 60% cost; judge itself pinned to Opus for blind-grading integrity |
| implement (greenfield/bugfix) | gate-pass + judge-grade | **Sonnet → Opus cascade** | try Sonnet, escalate to Opus on gate fail (thread 5 Phase 2) |
| plan / cross-subsystem | highest | **Opus 4.8** | reasoning-heavy, cheap relative to a bad plan's cost |
| research | high, long-context | **Sonnet / Opus** | breadth over precision; Sonnet often sufficient |
| bulk / low-stakes throughput | gate-pass | **Pi (MiniMax/qwen3-coder)** | ~20× cheaper input; only where objective verifier fully guards |

**Why cascade beats fixed choice for `implement`:** with an objective verifier (tests/compile/lint), a
Sonnet-first→Opus-on-fail cascade captures most tasks at Sonnet cost and only pays Opus on the hard tail —
the published FrugalGPT/RouterBench "above the frontier" result (thread 4 §4.2).

## 3.3 The two things that turn this from hypothesis into policy
1. **Real per-category quality** — thread 1 critical path (WS1f→WS3c→WS3d) producing `harmonik eval report`.
2. **Real per-category cost** — thread 2 price-table fix + Pi/Codex token extraction, so `cost_usd` is trustworthy.

Until both land, thread 3 is a **framework with priors**, not a policy. That's the honest status to give admiral.

## Open items
- [ ] Replace every "prior best-fit" with the measured frontier model per category.
- [ ] Confirm the quality bar per category with admiral (this encodes willingness-to-pay — thread 2.4).
