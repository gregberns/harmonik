---
title: "I06: Agent Specialization Through Constraints"
status: seed
type: idea
relates-to: [G03, P05]
sources: [refs/AlphaGo-modeled-orch-system.md, openai.com/index/harness-engineering]
created: 2026-04-13
updated: 2026-04-13
---

# I06: Agent Specialization Through Constraints

## Core Concept

Instead of giving every agent access to every tool and permission, scope capabilities per role. A planner cannot write code. A coder cannot merge. A reviewer can flag issues but not fix them. Constraints are not limitations -- they are the mechanism that makes agents effective. By reducing the action space, you reduce off-task behavior and increase the probability of good outcomes within the defined scope.

## Why Constraints Improve Performance

The intuition runs counter to common assumptions. Surely an agent with more tools is more capable? In practice, the opposite holds. OpenAI's harness engineering research found that "constraining the solution space makes agents more productive." The reason is straightforward: LLMs are next-token predictors. A larger action space means more possible next tokens, which means more ways to go wrong. A constrained agent has fewer choices, and those choices are more likely to be relevant.

Consider a reviewer agent with access to file-writing tools. It might decide to "just fix" an issue instead of documenting it for the implementor. This is locally rational but globally harmful -- it bypasses the review-implementation separation that ensures quality. Remove the file-writing tools from the reviewer's toolkit, and the behavior is structurally impossible.

## Implementation Mechanisms

### AGENTS.md Per Role
Each agent role gets a tailored AGENTS.md file that defines its purpose, constraints, available tools, and expected outputs. The file acts as both a prompt (shaping the LLM's behavior) and documentation (telling humans what the agent should do).

### Scoped MCP Tool Sets
MCP (Model Context Protocol) tool servers can expose different tool sets to different roles. A planner sees research and planning tools. A builder sees file manipulation and testing tools. A reviewer sees read-only file access and annotation tools. The tool server enforces the boundary -- the agent cannot call tools it hasn't been given.

### Workspace Permissions
File system permissions control what each agent can read and write. A reviewer has read access to the implementation but no write access. A builder has write access to its assigned module but not to other modules. The operating system enforces the boundary.

### Hook-Enforced Boundaries
Hooks validate agent actions against role constraints. If a builder attempts to modify a file outside its assigned scope, a pre-action hook blocks the operation. This is defense in depth: even if other mechanisms fail, the hook catches violations.

## The Specialization Spectrum

Agents exist on a spectrum from generalist to specialist:

- **Fully generalist**: One agent does everything. Maximum flexibility, minimum reliability.
- **Role-specialized**: Agents have defined roles but broad toolkits within those roles. Good balance for most workflows.
- **Task-specialized**: Each specific task type has a purpose-built agent configuration. Maximum reliability, higher maintenance cost.

Harmonik should default to role-specialized and allow task-specialization where warranted by failure patterns.

## Open Questions

1. How do we determine the right constraint set for each role? Too tight = agents cannot handle edge cases. Too loose = back to the generalist problem.
2. Should constraints be static (defined at system design time) or adaptive (tightened when agents misbehave)?
3. How do we handle tasks that genuinely require capabilities from multiple roles? Sequential handoff, or temporary capability escalation?

## Cross-References
- [G03: Independent Process-Following Actors](../goals/independent-process-following-actors.md) -- Constraints enable process adherence
- [P05: Behavior Enforcement](../problems/behavior-enforcement.md) -- Constraints are an enforcement mechanism
- [Harness Engineering](../concepts/harness-engineering.md) -- Guides and sensors as constraint mechanisms
