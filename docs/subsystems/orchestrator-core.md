---
title: "S01: Orchestrator Core"
status: seed
type: subsystem
solves: [P02, P06]
uses: [kilroy, event bus]
related: [docs/concepts/kilroy.md, docs/concepts/alphago-system.md, docs/subsystems/policy-engine.md, docs/subsystems/event-bus.md]
created: 2026-04-13
updated: 2026-04-13
---

# S01: Orchestrator Core

## Summary
The orchestrator core is the central state machine and workflow execution engine. It manages the lifecycle of work items through defined states, executes deterministic workflows, dispatches agents, and tracks progress. It is the heartbeat of harmonik -- the mechanical process that moves work forward without making any semantic judgments about the work itself.

## Purpose
Every composed workflow needs something to drive it forward: advance states, select transitions, handle branching, detect cycles, and resume after failures. The orchestrator core is that driver. It reads workflow definitions (DOT/YAML), walks the graph, and dispatches work to agents at each node. It does not evaluate whether the work is good -- that is the job of the verifier layer (S07) and hook system (S05). The orchestrator only asks: "What state are we in? What transition fires next? Who handles the next node?"

## Key Responsibilities
- **State machine execution.** Load a workflow graph, track current state for each active work item, advance state on valid events.
- **Edge selection and transition logic.** Deterministic edge selection following Kilroy's priority cascade: condition match, label match, suggested IDs, weight, lexical order.
- **Checkpoint and resume.** Persist workflow state at each transition so that any crash or interruption is recoverable. Git-native checkpointing following Kilroy's one-commit-per-node model.
- **Parallel branch isolation.** Fan out to multiple agents working in parallel, each on an isolated branch, then merge results at convergence nodes.
- **Cycle detection.** Track traversal counts per edge. Cap repeated traversals to prevent infinite retry loops. Classify failures as deterministic, structural, or transient.
- **Goal gates.** Define critical nodes that cannot be bypassed. The workflow cannot complete until all goal gates pass.

## Interfaces

**Inputs:**
- Workflow definitions (DOT files, YAML, or hybrid format)
- Events from agents (via hook system S05 or agent-mail)
- Policy decisions from policy engine (S02) -- transition guards, approval gates

**Outputs:**
- State transition events emitted to event bus (S03)
- Agent dispatch requests sent to agent runner (S04)
- Checkpoint writes (git commits or file-system state)
- Workflow completion/failure signals

## Design Principles
- **Deterministic skeleton.** This subsystem is fully deterministic. No LLM calls. Given the same workflow definition and the same sequence of events, the orchestrator produces the same sequence of transitions. This is the "deterministic skeleton" from the AlphaGo reference architecture.
- **ZFC-compliant.** The orchestrator is pure mechanism. It routes, transitions, and dispatches. It never inspects agent output semantically. If a semantic decision is needed (e.g., "is this code review sufficient?"), the orchestrator delegates that question to an LLM via the hook system.
- **Graph-as-workflow.** Following Kilroy's model, the workflow definition IS the execution plan. No hidden control flow. The graph is visual, diffable, and version-controlled.

## Candidate Implementations
- **Kilroy's engine directly.** Use Kilroy as the execution engine, wrapping its DOT-based pipeline model. Advantage: mature, tested. Risk: may be too rigid for dynamic workflow modification.
- **Custom state machine.** Build a minimal state machine library tuned to harmonik's needs. Advantage: full control. Risk: reinventing tested patterns.
- **Hybrid.** Use Kilroy for static workflow execution, with a thin custom layer for dynamic node addition/removal at runtime.

## Open Questions
1. How do we handle dynamic workflow modification -- adding or removing nodes at runtime based on agent discoveries -- without losing the deterministic guarantees of the static graph?
2. What is the right persistence model for checkpoints: one git commit per transition (Kilroy-style), a dedicated state store, or an event-sourced model backed by the event bus?
3. How should the orchestrator handle workflows that span multiple repositories or require coordination across independent harmonik instances?

## Cross-References
- [S02: Policy Engine](policy-engine.md) -- evaluates transition guards before the orchestrator advances state
- [S03: Event Bus](event-bus.md) -- receives all state transition events from the orchestrator
- [S04: Agent Runner](agent-runner.md) -- receives dispatch requests from the orchestrator
- [S05: Hook System](hook-system.md) -- feeds agent completion events back into the orchestrator
- [P02: Agent Persistence Gap](../problems/agent-persistence-gap.md) -- checkpoint/resume addresses persistence
- [P06: Workflow Composition](../problems/workflow-composition.md) -- the orchestrator is the engine that composes workflows
- [Kilroy concept digest](../concepts/kilroy.md) -- primary inspiration for graph-as-workflow model
