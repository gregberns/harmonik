---
title: "S02: Policy Engine"
status: seed
type: subsystem
solves: [P05]
uses: [orchestrator-core, workflow definitions]
related: [docs/concepts/alphago-system.md, docs/concepts/harness-engineering.md, docs/subsystems/orchestrator-core.md]
created: 2026-04-13
updated: 2026-04-13
---

# S02: Policy Engine

## Summary
The policy engine defines and enforces what agents are allowed to do in each workflow state. It provides role-based permissions, transition guards, freedom profiles, budget constraints, and approval gates. Policies are data -- expressed in YAML or as DOT graph attributes -- not code. The policy engine is the constraint layer that makes composed workflows trustworthy.

## Purpose
Agents are probabilistic (P05). Telling an agent "do not merge without review" is a suggestion, not a guarantee. The policy engine turns suggestions into structural constraints. It sits between the orchestrator core and agent dispatch, evaluating every proposed transition against the active policy set. If a transition violates policy, it is blocked before it executes -- not flagged after the fact.

The policy engine also manages freedom profiles: per-state configurations that control how much exploration an agent is permitted. A planning state might allow broad exploration; an implementation state might constrain the agent to a specific file set; a review state might restrict the agent to read-only operations.

## Key Responsibilities
- **Evaluate transition guards.** Before the orchestrator advances state, the policy engine checks whether the transition is permitted given the current role, state, and constraints.
- **Enforce role permissions.** Define which agent roles can perform which actions. A reviewer cannot merge. A builder cannot approve its own work. A planner cannot modify production code.
- **Manage freedom profiles.** Each state in a workflow can specify a freedom profile: which tools the agent may use, which files it may modify, how many tokens it may consume, how long it may run.
- **Apply budget and timeout constraints.** Track cumulative token spend and wall-clock time per workflow, per agent, and per state. Enforce hard limits.
- **Require approvals at configured gates.** Certain transitions require human or senior-agent approval. The policy engine holds the transition until approval arrives.

## Interfaces

**Inputs:**
- Transition proposals from orchestrator core (S01)
- Policy definitions (YAML files, DOT graph attributes)
- Agent role assignments
- Budget/timeout configuration

**Outputs:**
- Allow/deny decisions on proposed transitions
- Constraint sets injected into agent prompts (via hook system S05)
- Policy violation events emitted to event bus (S03)
- Approval requests routed to humans or governance agents

## Design Principles
- **Policies are data, not code.** Policy definitions live in version-controlled YAML or DOT attributes. Changing a policy is a config change, not a code change. This makes policies auditable, diffable, and reviewable.
- **Mechanism, not cognition.** The policy engine checks structural rules: "Is this agent in a role that permits this transition?" It does not make semantic judgments: "Is this code good enough to transition?" That distinction -- mechanism vs. cognition -- is the ZFC boundary. Semantic gates delegate to LLMs via the hook system.
- **Constrain to empower.** Following the harness engineering principle: well-designed constraints free agents to work confidently within safe boundaries. An agent that knows it cannot accidentally merge to main can explore more boldly during development.

## Candidate Implementations
- **YAML policy files.** Simple declarative format. Each workflow state lists allowed roles, permitted transitions, freedom profile, and budget limits. The policy engine is a rule evaluator that checks proposals against these definitions.
- **DOT attribute extensions.** Embed policies directly in workflow graph definitions as node and edge attributes. Advantage: policy and workflow are co-located. Risk: DOT format may not be expressive enough for complex policies.
- **OPA (Open Policy Agent).** Use an established policy engine. Advantage: mature, well-tested, supports Rego for complex rules. Risk: may be heavyweight for the initial system.

## Open Questions
1. How granular should freedom profiles be? Per-state tool restrictions are straightforward, but should policies also constrain which files an agent may read, or which MCP tools it may call?
2. Can agents propose policy changes through the improvement loop (S09), and if so, what governance controls prevent policy drift toward permissiveness?
3. How should policy conflicts be resolved when multiple policies apply to the same transition (e.g., a workflow-level policy and a role-level policy disagree)?

## Cross-References
- [S01: Orchestrator Core](orchestrator-core.md) -- consults the policy engine before every state transition
- [S05: Hook System](hook-system.md) -- injects policy constraints into agent sessions
- [S09: Improvement Loop](improvement-loop.md) -- may propose policy parameter adjustments
- [P05: Agent Behavior Enforcement](../problems/behavior-enforcement.md) -- the core problem this subsystem addresses
- [Zero Framework Cognition](../concepts/zero-framework-cognition.md) -- defines the mechanism/cognition boundary
- [Harness Engineering](../concepts/harness-engineering.md) -- "constrain to empower" principle
