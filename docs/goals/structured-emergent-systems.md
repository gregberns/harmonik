---
title: Structured Emergent Systems
status: explored
type: goal
solves: [P01, P04, P06]
sources: [refs/AlphaGo-modeled-orch-system.md, openai-harness-engineering]
related: [docs/problems/system-coherence.md, docs/problems/workflow-composition.md]
created: 2026-04-13
updated: 2026-04-13
---

# G01: Structured Emergent Systems

## Summary

Build highly structured systems that allow emergent behavior. The core principle is "deterministic skeleton, probabilistic organs" -- rigid orchestration controlling the flow of work, with autonomous agents operating freely within bounded spaces.

## The Tension

Too much structure kills creativity. An agent locked into a rigid script cannot adapt to novel problems or discover unexpected solutions. Too little structure produces chaos. Agents without constraints wander, contradict each other, and produce incoherent output. The sweet spot is deterministic orchestration with probabilistic search within bounded spaces.

## Key Pattern: Cellular Automata

Cellular automata demonstrate that simple deterministic rules can produce complex emergent behavior. Conway's Game of Life has four rules. From those four rules emerge gliders, oscillators, and Turing-complete computation. Harmonik should work the same way: a small set of transition rules, role constraints, and handoff protocols that produce sophisticated collaborative behavior when agents operate within them.

## How This Looks in Practice

**Deterministic skeleton:** The state machine defines legal transitions. Work moves through `idea_backlog -> spec_drafting -> spec_review -> implementation_ready -> coding -> code_review -> verification -> done`. These transitions are enforced, not suggested. Hooks fire on every transition. Roles determine who can trigger which transitions.

**Probabilistic organs:** Within each state, agents have autonomy. A builder agent in the `coding` state can choose its approach, decompose the problem, try multiple strategies, backtrack, and explore. The orchestrator does not micromanage the work -- it micromanages the flow.

**The AlphaGo parallel:** AlphaGo's MCTS explored branches freely (emergent) within a game tree bounded by legal moves and guided by learned policy priors (structured). The value network pruned bad branches without prescribing good ones. Harmonik's orchestrator should do the same: constrain the search space, provide evaluation signals, but let agents explore within bounds.

**Kilroy's model:** Graph-based deterministic workflow with agent autonomy within nodes. The graph edges are fixed. The node behavior is flexible. This is the right decomposition.

## Measures of Success

- The orchestrator can be fully described by its state machine, role definitions, and transition rules -- no implicit behavior.
- Agents within a state can solve problems the system designers did not anticipate, because they have freedom within their bounded context.
- Adding a new workflow requires defining new states and transitions, not rewriting agent logic.
- The system produces coherent output across multiple agents without requiring a central "coordinator agent" to reconcile conflicts.
- System behavior remains predictable at the orchestration level even as individual agent actions vary.

## Open Questions

- How granular should states be? Too few states and agents have too much freedom; too many and we lose the emergent property.
- How do we handle situations where an agent discovers the state machine itself needs modification? What is the governance path for changing transition rules?
- Can we measure "emergence quality" -- distinguish productive novel behavior from unproductive randomness?
- What is the right balance between pre-defined evaluation signals and agent-generated quality assessments?

## Cross-References

- [P04: System Coherence at Scale](../problems/system-coherence.md) -- the problem this most directly addresses
- [P06: Workflow Composition Complexity](../problems/workflow-composition.md) -- structured composition enables scaling
- [G03: Independent Process-Following Actors](independent-process-following-actors.md) -- the actors that operate within the skeleton
- [G05: Idea-to-Implementation Pipeline](idea-to-implementation-pipeline.md) -- the primary workflow built on this structure
