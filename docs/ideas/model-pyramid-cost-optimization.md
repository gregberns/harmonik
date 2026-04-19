---
title: "I05: Model Pyramid Cost Optimization"
status: seed
type: idea
relates-to: [ZFC, Kilroy]
sources: [docs/concepts/zero-framework-cognition.md, docs/concepts/kilroy.md]
created: 2026-04-13
updated: 2026-04-13
---

# I05: Model Pyramid Cost Optimization

## Core Concept

Not all tasks require the most expensive model. A system that routes every task to the largest available model wastes money and latency on work that a smaller model handles equally well. The model pyramid decomposes work into cognitive tiers and routes each task to the smallest model capable of handling it reliably.

## The Pyramid

From ZFC's analysis, cognitive tasks fall into tiers:

- **Tier 1 (Mechanical)**: File manipulation, formatting, simple code generation from clear specs, running commands. These tasks have well-defined inputs and outputs. Small, fast models handle them well.
- **Tier 2 (Analytical)**: Code review against defined criteria, test generation, refactoring within known patterns. These require understanding but not deep reasoning. Mid-tier models suffice.
- **Tier 3 (Reasoning)**: Architectural planning, spec critique, debugging subtle issues, cross-system analysis. These require the full reasoning capability of the most powerful models.

The pyramid shape reflects the natural distribution of work: most tasks are Tier 1, fewer are Tier 2, fewest are Tier 3. Routing correctly means the majority of compute goes to cheap models.

## Classification Mechanism

The critical question is: who decides which tier a task belongs to? From ZFC, the answer is to have the AI itself classify the cognitive tier, then route mechanically. A small classifier prompt examines the task description and assigns a tier. The orchestrator then routes to the appropriate model. The classification itself is a Tier 1 task -- cheap and fast.

Alternatively, the tier can be determined structurally: the workflow state implies the tier. Planning states use Tier 3 models. Implementation states use Tier 1. Review states use Tier 2. This is simpler and fully deterministic but less flexible.

## Evidence from Kilroy

Kilroy's model stylesheets demonstrate this in practice. Different roles are assigned different models:

- Coding agents use fast, cheap models (the work is well-specified by the time it reaches them)
- Review agents use mid-tier models (they need to evaluate quality, not just generate)
- Planning agents use reasoning models (they need to decompose complex goals)

The stylesheet is a static mapping: role to model. This is the simplest form of the model pyramid -- no dynamic classification, just role-based routing.

## Cost Impact

The cost difference between tiers is significant. A Tier 1 model might cost 10-50x less per token than a Tier 3 reasoning model. If 70% of tasks are Tier 1, 20% are Tier 2, and 10% are Tier 3, correct routing could reduce total model costs by 5-10x compared to routing everything to Tier 3.

Latency improvements are equally significant. Smaller models respond faster. For tasks in tight loops (code-test-fix cycles), using a fast model for the coding step and reserving the reasoning model for diagnosis makes the system substantially more responsive.

## Open Questions

1. How do we validate that a cheaper model produced adequate quality? The verifier layer can catch regressions, but subtle quality degradation may not trigger verification failures.
2. Should tier assignment be static (role-based) or dynamic (per-task classification)? Hybrid?
3. How do we handle tasks that turn out to be harder than their initial classification? Escalation from Tier 1 to Tier 3 mid-task?
4. What metrics should we track to continuously optimize the routing? Cost per task, quality per tier, escalation rate?

## Cross-References
- [Zero Framework Cognition](../concepts/zero-framework-cognition.md) -- Model pyramid concept, AI-driven classification
- [Kilroy](../concepts/kilroy.md) -- Model stylesheets as practical implementation
- [I06: Agent Specialization](agent-specialization-through-constraints.md) -- Specialization and model routing are complementary
