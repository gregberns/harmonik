---
title: Independent Process-Following Actors
status: explored
type: goal
solves: [P02, P05]
sources: [refs/AlphaGo-modeled-orch-system.md, openai-harness-engineering]
related: [docs/problems/agent-persistence-gap.md, docs/problems/behavior-enforcement.md]
created: 2026-04-13
updated: 2026-04-13
---

# G03: Independent Process-Following Actors

## Summary

Build independent agents that, when told to follow a process, actually follow it. This is about agent reliability -- not agent intelligence. An agent that brilliantly solves the wrong problem or skips the review step is worse than a mediocre agent that follows the defined workflow every time.

## Why Agents Drift

LLM-based agents naturally drift from instructions. They optimize for what seems locally helpful rather than what the process requires. They skip steps that seem unnecessary, merge phases that should be separate, and take shortcuts that break downstream assumptions. Without enforcement mechanisms, process compliance degrades as task complexity increases.

## Enforcement Mechanisms

**Hook-driven behavior:** Hooks fire at every transition and force agents to produce specific outputs, check specific conditions, and follow specific handoff protocols. The hook system is not advisory -- it is a runtime constraint. An agent that does not satisfy the hook's requirements cannot advance.

**Deterministic edge selection:** Kilroy's model makes transitions between workflow states deterministic. The agent does not choose whether to go to review after coding -- the state machine routes it there. The agent's autonomy exists within a state, not across states.

**Role-based permissions:** The AlphaGo reference doc defines roles with scoped capabilities: Planners create specs but cannot commit code. Builders edit code but cannot approve their own work. Reviewers can reject or approve but cannot silently rewrite. Verifiers run checks but cannot modify artifacts. These constraints are enforced at the tool level, not the prompt level.

**AGENTS.md as behavioral contracts:** OpenAI's harness engineering approach treats agent instructions as contracts, not suggestions. The AGENTS.md file defines what each agent role must do, must not do, and must produce. Compliance is verified by hooks and verifiers, not trusted on faith.

**Scoped tool access:** Each agent role gets access only to the tools it needs. A reviewer does not have write access to source files. A planner does not have access to deployment tools. Restricting the action space makes process violations structurally impossible rather than merely discouraged.

## The Paradox: Constrain to Empower

OpenAI's harness engineering principle applies directly: strict boundaries make agents MORE productive, not less. An agent that knows exactly what it can and cannot do spends zero tokens deliberating about scope, permissions, or whether to take a shortcut. It operates confidently within its lane. The constraint is not a limitation -- it is clarity.

## How This Looks in Practice

- A builder agent receives a task, writes code, runs its scoped tests, and submits for review. It cannot merge, cannot skip review, cannot modify the spec. It does not even have those tools available.
- A reviewer agent receives the submission, evaluates against the spec, and either approves or rejects with specific feedback. It cannot modify the code directly (unless the policy explicitly allows minor fixes).
- If an agent attempts a disallowed action, the system blocks it and logs the violation rather than silently allowing it.

## Measures of Success

- Process compliance is 100% for defined workflows. Agents never skip states, bypass reviews, or take unauthorized actions.
- Adding a new role requires defining its permissions and tools, not rewriting enforcement logic.
- An agent cannot even attempt an action outside its role -- the tools are simply not available.
- Process violations are caught at the system level, not discovered after the fact by humans reviewing logs.
- Agent productivity increases when constraints are tightened, because less time is spent on scope deliberation.

## Open Questions

- How do we handle legitimate edge cases where an agent needs to deviate from process? What is the escalation path?
- How granular should role permissions be? Per-tool? Per-file? Per-action-within-tool?
- Can we auto-generate AGENTS.md contracts from role and state machine definitions?
- How do we test that enforcement is working? What does a "process compliance test suite" look like?
- When an agent is blocked by a constraint, how does it communicate what it needs without the human having to diagnose the situation from scratch?

## Cross-References

- [P05: Agent Behavior Enforcement](../problems/behavior-enforcement.md) -- the specific problem this solves
- [P02: Agent Persistence Gap](../problems/agent-persistence-gap.md) -- reliable actors enable persistent systems
- [G01: Structured Emergent Systems](structured-emergent-systems.md) -- the skeleton these actors operate within
- [G05: Idea-to-Implementation Pipeline](idea-to-implementation-pipeline.md) -- the pipeline that depends on actor reliability
