---
title: "P05: Agent Behavior Enforcement"
status: explored
type: problem
sources: [refs/AlphaGo-modeled-orch-system.md, openai.com/index/harness-engineering]
related: [docs/problems/system-coherence.md, docs/problems/workflow-composition.md]
created: 2026-04-13
updated: 2026-04-13
---

# P05: Agent Behavior Enforcement

## Summary
Agents are probabilistic. They may or may not follow instructions, regardless of how clearly those instructions are written. Relying on prompts alone to constrain agent behavior is fundamentally unreliable. Deterministic mechanisms -- hooks, validation gates, structural constraints -- are required to force agents into defined processes.

## The Problem

LLM-based agents operate by predicting likely next tokens given their context. This means every instruction is a suggestion weighted by training data, not a binding contract. In practice:

- **Instruction adherence is probabilistic.** Tell an agent "always run tests before committing" and it will do so most of the time. But under pressure (long context, complex task, ambiguous situation), it may skip the step. "Most of the time" is not acceptable for process-critical behaviors.
- **Emphatic documentation is insufficient.** Steve Yegge's observation applies directly: code structure is the only effective constraint. Writing "IMPORTANT: ALWAYS DO X" in a prompt or AGENTS.md file increases the probability of compliance but does not guarantee it. The agent has no mechanism to distinguish emphasis from noise at the architectural level.
- **Drift is silent.** When an agent skips a step or deviates from a process, it does not announce the deviation. The human discovers it later, during review, or worse, in production.

The AlphaGo reference architecture addresses this through its state machine design: agents operate within defined transition systems where the allowed next actions are structurally constrained, not merely suggested. The hook system described there (`on_agent_completed`, `on_review_required`, `on_policy_violation`) makes compliance a property of the runtime, not a property of the prompt.

## Why It Matters

In a system where multiple agents collaborate across workflows (P06), unreliable behavior in one agent cascades. If a builder agent skips tests, the reviewer agent receives untested code. If a planner agent ignores architectural constraints, the builder agent creates structurally incoherent work. The whole system's reliability is bounded by the weakest behavioral guarantee.

Hook systems -- like Claude Code's hook mechanism and the bead hooks pattern from Gas Town -- demonstrate the approach: inject deterministic behavior at defined moments in the agent lifecycle. Before commit, run tests. Before merge, require review sign-off. Before state transition, validate preconditions. These are not suggestions to the agent; they are code that executes regardless of what the agent decides.

## Concrete Manifestations

- An AGENTS.md file says "run `npm test` before every commit." The agent follows this for 15 commits, then skips it on commit 16 when the context is long and the task is complex. The broken commit is discovered hours later.
- A workflow requires peer review before merge. The agent, under a permissive prompt, merges directly because nothing structurally prevents it.
- An agent is told to use the project's error handling pattern. It invents a new pattern instead, because the LLM finds the new pattern locally coherent even though it violates project conventions.

## Enforcement Mechanisms

Effective enforcement requires layering:

1. **Pre-action hooks.** Code that runs before an agent action and can block it. Example: a pre-commit hook that runs tests and rejects the commit if they fail.
2. **Post-action validators.** Checks that run after an agent produces output, before the output is promoted. Example: an architecture linter that rejects files placed in the wrong directory.
3. **State machine constraints.** The orchestrator only offers legal next actions based on current state and role. The agent cannot skip review because "submit for review" is not an available action from the "coding" state.
4. **Role-based permissions.** Agents in certain roles are structurally unable to perform certain actions. A reviewer cannot merge; a builder cannot approve its own work.

## Open Questions

1. Where is the line between enforcement and rigidity? Over-constraining agents reduces their ability to handle novel situations. How do we design constraints that enforce process without eliminating useful flexibility?
2. How should enforcement failures be handled -- block and retry, block and escalate to human, or log and continue with a warning?
3. Can enforcement rules themselves be evolved through the feedback loop (P07), or must they always be human-authored?

## Cross-References
- [P04: System Coherence at Scale](system-coherence.md) -- Enforcement serves coherence
- [P06: Workflow Composition Complexity](workflow-composition.md) -- Composed workflows depend on reliable behavior at each step
- [refs/AlphaGo-modeled-orch-system.md](../../refs/AlphaGo-modeled-orch-system.md) -- State machine and hook architecture
