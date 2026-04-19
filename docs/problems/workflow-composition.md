---
title: "P06: Workflow Composition Complexity"
status: explored
type: problem
sources: [refs/AlphaGo-modeled-orch-system.md]
related: [docs/problems/behavior-enforcement.md, docs/problems/feedback-loops.md]
created: 2026-04-13
updated: 2026-04-13
---

# P06: Workflow Composition Complexity

## Summary
Individual agent workflows -- implement a function, review a pull request, run a test suite -- are tractable problems with known patterns. Composing these workflows into larger systems, where events in one workflow trigger transitions in another and the whole chain must be observable and recoverable, is a fundamentally harder problem that current tooling does not address.

## The Problem

A single workflow is manageable: define the steps, assign an agent, run to completion. But real software work is composed workflows:

- Implement a feature, then review it, then fix review feedback, then re-review, then merge, then deploy, then monitor for regressions.
- Draft a spec, get critique, revise, get approval, split into tasks, assign tasks, track completion, run integration tests, write release notes.

Each transition point introduces complexity:

- **Event propagation.** When a builder agent completes implementation, the review workflow must be triggered. When a reviewer rejects, the implementation workflow must resume with specific feedback. These are not independent jobs -- they are causally linked.
- **State coordination.** The composed system must track which phase each unit of work is in, what blockers exist, and what the dependency graph looks like. A merge cannot happen before review approval. A deploy cannot happen before tests pass. These ordering constraints must be enforced across workflow boundaries.
- **Error handling across boundaries.** If a test fails after merge, the system must trace back through the composition to determine whether the failure originated in implementation, review oversight, or a pre-existing issue. Debugging composed workflows is harder than debugging individual ones.
- **Partial completion and recovery.** A composed workflow that fails at step 5 of 8 must be recoverable from step 5, not restarted from step 1. This requires checkpointing at composition boundaries, not just within individual workflows.

## Relevant Approaches

The AlphaGo reference document describes a state machine architecture where transitions between states are explicit and each state has defined allowed actions, owner roles, and exit conditions. This maps directly to workflow composition: each state is a workflow phase, transitions are inter-workflow handoffs, and the state machine enforces ordering and prerequisites.

Kilroy's graph-based approach models workflows as directed graphs where nodes are work units and edges are dependencies. This is useful because it makes the composition structure explicit and inspectable -- you can visualize the entire workflow, identify bottlenecks, and verify that all paths lead to valid terminal states before execution begins.

Both approaches share a key insight: composition must be a first-class, inspectable structure, not an emergent property of agents sending messages to each other.

## Concrete Manifestations

- A team sets up an implement-review-merge pipeline. The review step rejects with feedback, but there is no mechanism to route the feedback back to the implementation agent with the right context. A human must manually copy the feedback into a new session.
- A composed workflow has five steps. Step 3 fails. The system has no checkpoint, so re-running means re-executing steps 1 and 2, wasting compute and time.
- Two composed workflows share a dependency (both need a library updated). Neither workflow is aware of the other, leading to conflicting changes that only surface at merge time.

## Open Questions

1. Should workflow composition be defined declaratively (a graph/config that is parsed and executed) or imperatively (code that calls workflow primitives)? What are the tradeoffs?
2. How do we handle composition at different time scales -- some workflows complete in minutes, others span days with human review gates? Does the composition model need to accommodate both?
3. What is the right observability model for composed workflows? Flat logs are insufficient. Do we need a trace-based model (like distributed tracing in microservices) for workflow execution?

## Cross-References
- [P05: Agent Behavior Enforcement](behavior-enforcement.md) -- Each step in a composed workflow depends on reliable behavior
- [P07: Feedback Loop Absence](feedback-loops.md) -- Composed workflows generate the data that feedback loops need
- [refs/AlphaGo-modeled-orch-system.md](../../refs/AlphaGo-modeled-orch-system.md) -- State machine and graph-based composition
