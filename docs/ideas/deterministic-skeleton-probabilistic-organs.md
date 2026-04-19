---
title: "I02: Deterministic Skeleton, Probabilistic Organs"
status: seed
type: idea
relates-to: [G01, ZFC, AlphaGo]
sources: [refs/AlphaGo-modeled-orch-system.md, docs/concepts/zero-framework-cognition.md]
created: 2026-04-13
updated: 2026-04-13
---

# I02: Deterministic Skeleton, Probabilistic Organs

## Core Concept

The orchestration layer -- workflow definitions, state transitions, governance rules, permission checks -- is fully deterministic. The intelligence layer -- planning, critique, synthesis, code generation -- is probabilistic and LLM-driven. These two layers are architecturally distinct. The skeleton constrains the organs. The organs fill the skeleton with intelligence.

This is not a compromise between structure and flexibility. It is the claim that structure INCREASES rather than suppresses emergence. Like cellular automata producing complex behavior from simple rules, a rigid state machine with well-defined transitions creates the conditions for agents to do their best work within bounded spaces.

## Practical Meaning

State machine transitions are fixed. Given a state and an event, the next state is deterministic. There is no randomness in the workflow engine. What agents do WITHIN a state is flexible -- they plan, reason, generate, critique using the full power of LLMs. The more structured the skeleton, the more creative agents can be within their bounded spaces, because the structure guarantees that creative output will be channeled into the right place.

Consider the difference: an unconstrained agent might plan, code, review, and deploy all in one session, with no guarantee of quality at any step. A skeleton-constrained agent operates in a "coding" state where it can only write code. When it finishes, the skeleton transitions to "review" -- the agent has no ability to skip this. The coding agent can be maximally creative within its state because the skeleton guarantees a reviewer will check its work.

## Controlled Openings

Each state has a "freedom profile" that defines how much exploration is appropriate:

- **High exploration**: Spec drafting, ideation, planning. Many paths are valid. The agent should range widely.
- **Medium exploration**: Implementation. The spec constrains the goal, but the approach is flexible.
- **Low exploration**: Verification, deployment. The path is narrow and well-defined. Creativity here is a bug.

Freedom profiles are a property of the state definition, not a property of the agent. The same agent model operates with different freedom in different states because the skeleton tells it how much latitude it has.

## The AlphaGo Analogy

In AlphaGo, the game rules (deterministic) constrain the neural network's move selection (probabilistic). The rules do not suppress the network's intelligence -- they channel it. Without the rules, the network would produce nonsense. With the rules, it produces superhuman play. The same principle applies: deterministic workflow rules channel probabilistic agent intelligence into useful outcomes.

## Relationship to ZFC

Zero Framework Cognition draws a hard line: no cognition in the framework. The skeleton is mechanism; the organs are cognition. This aligns perfectly. The orchestrator core, event bus, and policy engine contain zero LLM calls. The agents, verifiers (when using LLM-based review), and improvement loop contain all the cognition. The boundary is clean and enforceable.

## Open Questions

1. How rigid should the skeleton be? Can states have conditional transitions, or must all transitions be unconditional?
2. How do we handle genuinely novel situations where no predefined state covers the scenario? Is there an "escape hatch" state?
3. Can freedom profiles be learned from execution history, or must they always be human-defined?

## Cross-References
- [G01: Structured Emergent Systems](../goals/structured-emergent-systems.md) -- This idea is the primary mechanism for achieving G01
- [AlphaGo-Modeled System](../concepts/alphago-system.md) -- The architectural north star that defines this principle
- [Zero Framework Cognition](../concepts/zero-framework-cognition.md) -- The mechanism-vs-cognition boundary
