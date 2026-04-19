---
title: "P04: System Coherence at Scale"
status: explored
type: problem
sources: [docs/01_architecture.md, openai.com/index/harness-engineering]
related: [docs/problems/behavior-enforcement.md, docs/problems/workflow-composition.md]
created: 2026-04-13
updated: 2026-04-13
---

# P04: System Coherence at Scale

## Summary
As systems grow, entropy increases. Scripts get placed randomly, documentation gets dumped arbitrarily, architectural boundaries erode, and the codebase drifts from its intended structure. Without active enforcement, agent-built systems degrade faster than human-built ones because agents produce volume without structural awareness.

## The Problem

The architecture document (`docs/01_architecture.md`) states the issue directly: "There needs to be a dedicated process or integrated agent process that needs to ensure the system is being built for long term sustainability. No randomly placed scripts, docs dumped arbitrarily."

This is not a theoretical concern. It is an observed pattern in agent-assisted development:

- **Structural drift.** Agents place files where they seem locally reasonable, ignoring project-wide conventions. A utility function ends up in a controller directory. A test helper lands in the source tree. Over time, the architecture becomes illegible.
- **Convention erosion.** Naming conventions, import patterns, error handling strategies, and logging formats diverge as different sessions (or different agents) make locally consistent but globally inconsistent choices.
- **Documentation rot.** Agents generate documentation that describes what was built at the time, but nobody updates it when the code changes. Stale docs become misleading, then ignored, then harmful.
- **Dependency sprawl.** Each session may introduce dependencies to solve immediate problems without considering the project-wide dependency graph. The result is bloated, conflicting, or redundant dependencies.

OpenAI's harness engineering work found this pattern explicitly: entropy management is non-optional. Their response was scheduled cleanup agents that periodically audit and correct structural drift. Without such mechanisms, quality degradation is not a risk -- it is a certainty.

## Why It Matters

The architecture document calls for layered design (from a component perspective) and hexagonal architecture (from an integration perspective), enforced through deterministic checks. These are not aesthetic preferences. Layered architecture makes the system understandable. Hexagonal architecture makes it adaptable. Deterministic enforcement makes these properties durable.

Without coherence, every other problem becomes harder. Knowledge loss (P03) worsens because there is no consistent structure to anchor knowledge to. Behavior enforcement (P05) becomes fragile because the system it is enforcing against keeps shifting. Workflow composition (P06) breaks because composed workflows assume structural stability.

## Concrete Manifestations

- An agent creates a new module but places it outside the established directory hierarchy. Three sessions later, another agent creates a duplicate module in the correct location because it could not find the first one.
- A codebase starts with a clean hexagonal architecture. After 50 agent sessions, adapters are calling domain logic directly, ports have leaked implementation details, and the dependency graph has cycles.
- A CI pipeline passes because it only checks what is explicitly configured. Structural violations -- wrong directory, missing index file, broken cross-reference -- accumulate silently.

## Open Questions

1. Can structural coherence be maintained purely through deterministic checks (linters, architecture tests, directory validators), or does it also require an active "coherence agent" that audits and corrects?
2. What is the right frequency for coherence checks -- per-commit, per-session, scheduled intervals, or event-driven?
3. How do we define "coherence" precisely enough to check it automatically without making the system so rigid that it cannot evolve?

## Cross-References
- [P05: Agent Behavior Enforcement](behavior-enforcement.md) -- Enforcement is the mechanism; coherence is the goal
- [docs/01_architecture.md](../01_architecture.md) -- Origin of the architectural requirements
- [docs/01_components.md](../01_components.md) -- OpenAI harness engineering reference
