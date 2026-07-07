# Agent World Models — research (schmidhuber, for admiral/operator)

Started 2026-07-05. Research crew **schmidhuber** (reports to admiral). Product = written
research + a decisive recommendation, NOT merged code.

## The question
The operator finds "agent world models" interesting and knows little about them. Start near-zero,
build understanding, then answer: **could harmonik use an agent world model, and how?**

## Artifacts (build in order)
1. `01-explainer.md` — what an agent world model IS (from the Qwen AgentWorld primary source),
   in plain English. How it differs from a normal LLM agent and from classic (Ha & Schmidhuber
   2018) world models.
2. `02-field-survey.md` — who else builds these (DeepMind Genie/SIMA, Dreamer lineage, LLM world
   models). Real capability vs hype, primary sources.
3. `03-harmonik-mapping.md` — could it help harmonik (a factory of LLM coding agents), and where:
   testing / implementation / validation. Honest "no, not yet" where that's the answer.
4. `RECOMMENDATION.md` — short, decisive: what it is, is there a real use, and the smallest
   experiment that would prove or kill it.

## Status
- [x] Boot: comms joined, admiral notified, workspace created.
- [x] Primary source deep-read (Qwen-AgentWorld, arxiv 2606.24597) → `01-explainer.md`.
- [x] Field survey (Genie/SIMA, Dreamer, LLM world models) → `02-field-survey.md`.
- [x] Harmonik mapping → `03-harmonik-mapping.md`.
- [x] Recommendation → `RECOMMENDATION.md`.

**v1 complete 2026-07-05.** Headline: don't build a world model — it aims at our hardest surface
(daemon races/timing) with the technique's weakest property (factual fidelity). Run the one-afternoon
zero-build validation probe to settle the only sub-claim that could change that; put "break the binary"
energy into a real-failure-mined scenario harness. Awaiting admiral direction / deeper dives.
