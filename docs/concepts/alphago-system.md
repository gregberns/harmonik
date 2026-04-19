---
title: AlphaGo-Modeled Orchestration System
status: explored
type: concept
source: refs/AlphaGo-modeled-orch-system.md
related: [kilroy.md, zero-framework-cognition.md, harness-engineering.md, gas-town-hooks.md]
created: 2026-04-13
updated: 2026-04-13
---

# AlphaGo-Modeled Orchestration System

## What It Is
The project's own deep architectural reference, modeling an agentic orchestration system after AlphaGo's core patterns: tree search over possibilities, verification of results, and trace-based learning. This document is harmonik's architectural north star.

## Key Concepts

### Search + Verifier + Traces
The minimal viable pattern for reliable agentic systems. **Search** explores solution candidates. **Verifiers** validate results against criteria. **Traces** record everything for learning and debugging. All three are required -- remove any one and the system degrades.

### Deterministic Skeleton, Probabilistic Organs
The core architectural principle. The **skeleton** (workflow transitions, state machines, governance rules) is deterministic and verifiable. The **organs** (planning, critique, synthesis, code generation) are probabilistic and model-driven. The skeleton constrains the organs; the organs fill the skeleton with intelligence.

### State Machine Orchestration
A defined progression: idea_backlog, spec_drafting, spec_review, implementation_ready, coding, code_review, verification, done. Transitions are explicit. Backtracking is controlled -- you can go backward, but only through defined paths.

### Role-Based Agent System
Seven roles with different permissions and model assignments:
- **Planner**: Decomposes goals into tasks
- **Researcher**: Gathers context and information
- **Builder**: Writes code and artifacts
- **Reviewer**: Evaluates quality against criteria
- **Verifier**: Runs tests, checks constraints
- **Scheduler**: Assigns work, manages priorities
- **Governor**: Enforces policy, resolves conflicts

### Hook System
Lifecycle events that enforce progress:
- `on_agent_started`: Initialize context, set constraints
- `on_agent_output`: Validate format, check bounds
- `on_agent_completed`: Trigger state transition, notify dependents
- `on_timeout`: Escalate, reassign, or abort
- `on_review_required`: Spawn reviewer, block progression

### Controlled Openings
Freedom profiles vary by state. High exploration in spec drafting and ideation (many paths are valid). Low exploration in verification (the path is narrow and well-defined). The system opens up where creativity helps and tightens where precision matters.

### Comprehensive Transition Logging
Every state transition records: prior state, actor role, candidate actions, chosen action, policy version, parameter vector, evidence, outcome, verifier metrics, next state, confidence. This enables replay, debugging, and policy learning.

### Backtracking as First-Class
Four types of rollback, all explicitly supported:
- **Local patchback**: Fix the current output and retry
- **Architectural rollback**: Return to a prior design decision
- **Policy rollback**: Revert a policy change that made things worse
- **Context rollback**: Restore a previous context window

### Meta-Process
Four nested loops: execution loop (do the work), analysis loop (evaluate results), policy proposal loop (suggest improvements), governance loop (approve/reject changes). Each loop operates at a different timescale.

### Composable Abstractions
The primitive building blocks: State, Transition, Gate, OpenZone, RolePolicy, Event, MetricHook, BacktrackRule, PolicyPatch, Experiment. Complex workflows are assembled from these primitives.

### Positive Emergence Structures
Conditions that promote good emergent behavior: clear ownership (one agent per task), tight feedback loops (fast verification), stable formats (consistent interfaces), small tasks (bounded scope), auto-retries with caps (resilience without runaway), cheap rollback (low cost of mistakes).

## Relevance to Harmonik

This IS harmonik's architectural vision. Most concepts from other digests map into this framework:

- Kilroy's graph-as-workflow maps to the deterministic skeleton
- Symphony's daemon model maps to the scheduler/governor roles
- Harness engineering's guides+sensors maps to hooks and verification
- ZFC's mechanism-vs-cognition maps to skeleton-vs-organs

This document should be treated as the **north star**. When other concepts conflict with this architecture, this one wins. When new patterns are evaluated, they should be assessed against whether they fit into this framework's abstractions.
