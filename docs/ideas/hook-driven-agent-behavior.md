---
title: "I01: Hook-Driven Agent Behavior"
status: seed
type: idea
relates-to: [S05, S01, P05, P06]
sources: [refs/AlphaGo-modeled-orch-system.md, docs/concepts/gas-town-hooks.md]
created: 2026-04-13
updated: 2026-04-13
---

# I01: Hook-Driven Agent Behavior

## Core Concept

Hooks attached to agent lifecycle events -- especially "agent loop completed" -- trigger deterministic state transitions. Based on the agent's role and current state, the hook forces actions that drive the workflow forward. This is the key mechanism that makes harmonik work: agents are probabilistic, but hooks are deterministic. The hook system bridges the gap.

## How It Works

The mechanism relies on Claude Code's existing hook system. When an "agent loop has completed" event fires, the hook executes a command. That command inspects the current workflow state, determines the next required action based on role and context, and triggers it. The agent does not decide what happens next -- the hook does.

### Example Flow

1. An implementation agent completes its work on a task.
2. The "agent loop completed" hook fires. The hook command inspects state: did the agent complete the task? If so, call `trigger-review`.
3. The `trigger-review` command spawns a review agent with the appropriate context.
4. The review agent evaluates the implementation and completes.
5. Another hook fires on the review agent's completion. The hook inspects the review result: if changes are needed, post the review feedback back to the implementor and re-enter the implementation state. If the code is good, mark the task as reviewed.
6. On review approval, a state change event fires to the coordinator agent, which confirms completion and takes the next step in the broader workflow.

At no point does any agent decide the workflow. The workflow is defined by state transitions and hooks. Agents fill each state with intelligence; hooks enforce the progression between states.

## Broader Vision

Multiple workflows can be composed together via hooks. Events in one workflow kick off events in another. A few coordinator agents tie everything together and keep work moving through the system. The hook system becomes the connective tissue of the entire orchestration layer.

This means workflow composition is not a special feature -- it is an emergent property of the hook mechanism. Any event can trigger any transition. Building a new workflow is just defining new states and wiring hooks between them.

## Why This Matters

Without hooks, workflow progression depends on agents choosing to advance the workflow. That choice is probabilistic. An agent might forget to request review. Might skip a verification step. Might not notify the coordinator. Hooks remove the choice: progression happens because the runtime demands it, not because the agent decides it.

## Open Questions

1. What is the full set of hook events available in Claude Code's hook system? Are they sufficient, or do we need additional events?
2. How do hooks handle failures? If a spawned review agent crashes, what hook fires?
3. Can hooks be composed declaratively (a config file mapping events to actions), or must they be imperative scripts?
4. How do we avoid infinite loops in hook chains? A hook fires a transition, which fires a hook, which fires a transition...

## Cross-References
- [S05: Hook System](../subsystems/hook-system.md) -- The subsystem that implements this idea
- [S01: Orchestrator Core](../subsystems/orchestrator-core.md) -- Consumes hook events to drive state transitions
- [P05: Behavior Enforcement](../problems/behavior-enforcement.md) -- The problem this directly solves
- [P06: Workflow Composition](../problems/workflow-composition.md) -- Hook composition enables workflow composition
