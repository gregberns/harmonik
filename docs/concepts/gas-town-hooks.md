---
title: Gas Town Hooks Model
status: seed
type: concept
source: user-described pattern (conversation)
related: [alphago-system.md, symphony.md, kilroy.md]
created: 2026-04-13
updated: 2026-04-13
---

# Gas Town Hooks Model

## What It Is
A pattern for connecting probabilistic agent behavior to deterministic workflow progression using lifecycle hooks. Hooks attached to beads (tasks) fire at key moments -- especially when an agent believes it has completed work -- and trigger deterministic state changes, reviews, and coordination actions.

## Key Concepts

### Hook-Driven Behavior
Hooks are attached to beads (the fundamental work unit). At lifecycle transitions -- start, output, completion, failure, timeout -- hooks fire and execute deterministic logic. The agent does not control what happens after it finishes; the hook does.

### Completion Detection via Hooks
The critical pattern: when an agent thinks it has completed a task, a hook fires. The hook inspects the agent's role and the current bead state, then forces a state change that triggers additional actions. The agent declares intent ("I'm done"); the system verifies and routes.

### Example Flow
A concrete walkthrough of the pattern in action:

1. **Implementation agent** completes its work on a bead
2. **Hook fires**: "Did you complete the task? If so, call `trigger-review`"
3. **Review agent** spawns, evaluates the implementation
4. **Review completes** -- hook fires with one of two outcomes:
   - Review finds issues: post review results back to implementor, bead returns to implementation state
   - Review approves: state change fires to coordinator
5. **Coordinator agent** confirms completion, updates dependencies, dispatches next work

The agent never decides "what happens next" -- hooks enforce the workflow.

### Workflow Composition via Events
Multiple workflows composed together where events in one workflow trigger events in another. A completion event in the implementation workflow fires a start event in the review workflow. A completion event in the review workflow fires a progress event in the coordination workflow.

Several coordinator agents tie everything together, keeping work moving through the system. No single workflow knows about all the others -- they are connected through event propagation.

### Claude Code Hooks as Enforcement Mechanism
The existing Claude Code hook system (pre-tool-use, post-tool-use, on-agent-loop-start, on-agent-loop-end) provides concrete attachment points. By injecting deterministic behavior at these points, probabilistic agents are channeled into structured workflow progression.

This means harmonik can use existing infrastructure -- Claude Code's hook system is already built and tested. The innovation is in what the hooks DO, not in building a new hook system.

## The Bridge Pattern

Hooks solve a fundamental problem in agentic orchestration: **how do you get reliable workflow progression from unreliable agents?**

The answer: don't ask agents to manage workflow state. Let them focus on their cognitive task (write code, review code, plan work). When they produce output, hooks take over and mechanically advance the workflow. The agent is the organ; the hook is the skeleton.

This maps directly to the AlphaGo system's "deterministic skeleton, probabilistic organs" principle. Hooks are the connective tissue between the two.

## Relevance to Harmonik

This is the **key mechanism** for harmonik's architecture. Without hooks:
- Agents must be trusted to correctly transition workflow state (they can't be)
- Completion detection requires semantic analysis in the framework (violates ZFC)
- Workflow composition requires agents to know about each other (creates coupling)

With hooks:
- Agents focus purely on their cognitive task
- The framework mechanically enforces workflow progression
- Workflows compose through event propagation, not agent awareness
- Every transition is deterministic, logged, and auditable

The gas town hooks model is the implementation strategy for the AlphaGo system's hook system (`on_agent_completed`, `on_review_required`, etc.). It takes those abstract hooks and shows how they work in practice with real agent interactions.

## Open Questions (Seed Status)
- Hook ordering: when multiple hooks fire on the same event, what determines execution order?
- Hook failure: what happens when a hook itself fails? Retry? Escalate? Dead-letter?
- Hook composition: can hooks trigger other hooks? What prevents infinite chains?
- Performance: does synchronous hook execution create bottlenecks in high-throughput scenarios?
