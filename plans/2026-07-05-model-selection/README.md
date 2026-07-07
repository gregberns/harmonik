# Model-Selection Research Program

**Crew:** shannon (research) · **Reports to:** admiral · **Started:** 2026-07-05

**Operator's question:** *How do we choose the right model for the right work?*

This is a living research program, not a one-shot task. Findings accrue incrementally
into per-thread docs. Product of this crew = **written research + concrete proposals**;
shannon proposes, admiral decides and dispatches. No product code / spec edits here.

## Threads

| # | Thread | Doc | Status |
|---|--------|-----|--------|
| 1 | Evals — per-model quality per task category | [thread-1-evals.md](thread-1-evals.md) | v1 drafted — extends existing eval-prog |
| 2 | Token efficiency + cost | [thread-2-cost.md](thread-2-cost.md) | v1 drafted — 🔴 stale-price bug found |
| 3 | Task/model fit (synthesis of 1+2) | [thread-3-fit.md](thread-3-fit.md) | framework + priors, awaits data |
| 4 | Pareto curve (quality vs cost frontier) | [thread-4-pareto.md](thread-4-pareto.md) | framework, awaits data |
| 5 | **The USE mechanism — routing into dispatch** (the payoff) | [thread-5-routing.md](thread-5-routing.md) | v1 proposal — phased, no-train |

## Key framing (decided at boot)

- **Build on the existing eval program, don't restart it.** There is an active
  `eval-prog` / `eval-metrics` / `eval-harness` workstream (WS1–WS5) already curating a
  task corpus and collecting per-model quality/token/time metrics. Thread 1 extends it.
- **Cost is an operator decision.** Pricing policy (budget ceilings, willingness-to-pay
  per task tier) is surfaced to admiral early — thread 2 raises the questions, does not
  guess the policy.
- **The payoff is thread 5.** Everything feeds a routing policy: per-task, which model
  runs it, hooked into harmonik's dispatch, config-driven and overridable.

## Open decisions for the operator (running list)

_(populated as threads surface them — mirrored to admiral via `--topic question`)_

- [ ] **Cost/pricing policy** (thread 2.4): budget ceiling per task tier? willingness-to-pay for the quality delta? self-hosted-vs-API break-even volume?
- [ ] **Eval-taxonomy scope** (thread 1 §1.2): existing eval program is coding-implementation only. Widen to review/plan/triage/research eval tasks, or route those on production telemetry (thread 5 Phase 3) only?
- [ ] **Prioritize eval critical path** WS1f→WS3c→WS3d to produce the first real cross-model report (the y-axis for threads 3/4/5)?
- [ ] **Per-task open-weight model routing** (thread 5.2): Pi model is daemon-global (restart to switch). Lift to per-dispatch, or accept Claude-tier-only routing for now?

## 🔴 Concrete quick win found (proposal to admiral)

`internal/sessiondata/sessiondata.go:66-77` prices Opus-4.8 at **$15/$75** — it's Opus-3-era, current is
**$5/$25**, so every emitted Opus `cost_usd` is **~3× overstated**; Fable-5/Sonnet-5/Haiku-4.5 are missing
(emit no cost at all). Small well-scoped bead: correct the table (ideally make it config-loadable). This is
a prerequisite for any trustworthy cost/Pareto number. See thread-2 §2.3.
