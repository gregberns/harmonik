---
title: "I03: Composable Multi-Workflow Systems"
status: seed
type: idea
relates-to: [S01, S05, P06, Kilroy]
sources: [refs/AlphaGo-modeled-orch-system.md, docs/concepts/kilroy.md]
created: 2026-04-13
updated: 2026-04-13
---

# I03: Composable Multi-Workflow Systems

## Core Concept

Individual workflows -- spec-writing, implementation, review, testing, deployment -- are defined independently as self-contained state machines. Composition connects them: events emitted by one workflow trigger transitions in another. The composed system is larger than any single workflow, but each component remains simple, testable, and understandable.

This is the user's original vision: "There could be several workflows composed together that certain events in one place would kick off events in another -- building a whole system that worked together."

## How Composition Works

Each workflow exposes a set of entry events (things that can start it) and exit events (things it emits when it completes or reaches certain states). Composition is the mapping of exit events from one workflow to entry events of another.

### Concrete Examples

- **Spec to implementation**: kerf's spec workflow produces beads (work items). Each bead's "ready" event triggers the orchestrator to dispatch it into an implementation workflow instance.
- **Implementation to review**: An implementation workflow's "complete" event triggers a review workflow instance. The review agent receives the implementation artifacts as input.
- **Review rejection loop**: A review workflow's "changes-requested" event triggers the implementation workflow to re-enter its coding state with review feedback attached.
- **Completion to deployment**: When all implementation and review workflows for a batch of beads report "approved," a deployment workflow is triggered.

## Verification Before Execution

Composed workflows can be analyzed before they run. Because the composition is a graph of state machines connected by events, standard formal techniques apply:

- **Deadlock detection**: Are there states where no workflow can make progress because each is waiting for an event the other hasn't emitted?
- **Infinite loop detection**: Can a cycle of events keep firing indefinitely without reaching a terminal state? (Kilroy's cycle detection is relevant here.)
- **Unreachable state analysis**: Are there states that no sequence of events can reach? These indicate wiring errors.
- **Formal verification**: Petri net analysis or process algebra techniques can prove properties about the composed system before any agent runs.

This is a significant advantage over ad-hoc orchestration. You can know the system is well-formed before spending compute on agents.

## The Composition Interface

A workflow definition needs to declare:
1. Its internal states and transitions (the state machine)
2. Its entry points (which events start it, with what input)
3. Its exit points (which events it emits, with what output)
4. Its composition constraints (can it run in parallel with itself? Does it require exclusive access to a resource?)

The orchestrator core (S01) manages composition: it maintains the global event graph, routes events between workflows, and enforces composition constraints.

## Open Questions

1. What format for workflow definitions? DOT graphs (Kilroy-style), YAML, or a custom DSL?
2. How granular should composition events be? Too few events = rigid coupling. Too many = complex wiring.
3. How do we handle composition across repositories? The product bus concept from agent-mail?
4. Can composed workflows be dynamically reconfigured, or must composition be defined statically?

## Cross-References
- [S01: Orchestrator Core](../subsystems/orchestrator-core.md) -- Executes composed workflows
- [S05: Hook System](../subsystems/hook-system.md) -- Hooks are the mechanism for inter-workflow events
- [P06: Workflow Composition](../problems/workflow-composition.md) -- The problem this directly addresses
- [Kilroy](../concepts/kilroy.md) -- Graph-as-workflow model and cycle detection
