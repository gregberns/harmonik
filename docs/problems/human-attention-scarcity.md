---
title: "P01: Human Attention Scarcity"
status: explored
type: problem
sources: [docs/00_objective.md, openai.com/index/harness-engineering]
related: [docs/goals/INDEX.md, docs/problems/agent-persistence-gap.md]
created: 2026-04-13
updated: 2026-04-13
---

# P01: Human Attention Scarcity

## Summary
Human engineering attention is the single most constrained resource in software development. Compute scales horizontally. Models improve on their own release cadence. But the number of hours a human can spend directing, correcting, and reviewing agent work is fixed and non-fungible. Every system design decision in harmonik traces back to this constraint.

## The Problem

Modern AI agents are capable of substantial autonomous work -- writing code, running tests, debugging failures, drafting documentation. Yet in practice, they require constant human supervision to stay productive. The human must:

- **Initiate work.** Agents do not decide what to do next. A human must frame the task, provide context, and launch the session.
- **Re-orient after drift.** Agents lose track of objectives, pursue tangents, or misinterpret requirements. A human must notice and correct.
- **Re-explain across sessions.** Context from a prior session is gone. The human must re-establish what was learned, what was decided, and what remains.
- **Review and approve.** Every meaningful output requires human judgment before it can be trusted.

Each of these interactions consumes the scarce resource. The compound effect is that a team's throughput is bottlenecked not by how fast agents can work, but by how many agents a human can effectively supervise.

## Why It Matters

A well-run organization does not require the CEO to sit in every meeting, re-explain the company strategy to each employee every morning, and personally approve every line item. It establishes direction, delegates through structure, and intervenes only at decision points. The gap between how organizations work and how current human-agent workflows work is the core design space for harmonik.

The objective from `docs/00_objective.md` is explicit: "maximize our one truly scarce resource: human time and attention." This is not an aspirational statement -- it is the design constraint that should govern every architectural decision.

## Concrete Manifestations

- An engineer spends 30 minutes setting up context for a Claude Code session, gets 2 hours of productive work, then must spend another 30 minutes on a new session because the context window filled.
- A team runs three agent workflows in parallel but one engineer must context-switch between all three to keep them moving, reducing the value of parallelism.
- A review cycle stalls for hours because the human reviewer is busy with other work, while the agent sits idle.

## Open Questions

1. What is the minimum viable human touchpoint per unit of agent work? Can we quantify an "attention cost" per workflow type?
2. How do we distinguish between human oversight that is genuinely necessary (value judgments, ambiguous requirements) and oversight that exists only because the system lacks structure (re-explaining context, re-initiating stalled work)?
3. At what ratio of agents-to-human does the supervision model break down, and what structural changes extend that ratio?

## Cross-References
- [P02: Agent Persistence Gap](agent-persistence-gap.md) -- Persistence reduces re-initiation cost
- [P03: Knowledge Loss Across Sessions](knowledge-loss.md) -- Memory reduces re-explanation cost
- [docs/00_objective.md](../00_objective.md) -- Origin of the core constraint
