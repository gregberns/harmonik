---
title: Subsystem Catalog
status: seed
type: index
related: [docs/01_architecture.md, docs/components/INDEX.md, docs/problems/INDEX.md]
created: 2026-04-13
updated: 2026-04-13
---

# Subsystem Catalog

## Overview
Harmonik decomposes into nine subsystems. Each subsystem is a logical boundary -- a distinct concern that can be developed and tested independently. Subsystems are not necessarily separate binaries; they are architectural seams that enforce separation of concerns and enable incremental delivery.

Subsystems consume components (documented in [components/INDEX.md](../components/INDEX.md)) and solve problems (documented in [problems/INDEX.md](../problems/INDEX.md)).

## Subsystem Table

| ID | Name | Purpose | Solves | Uses | Status |
|----|------|---------|--------|------|--------|
| S01 | [Orchestrator Core](orchestrator-core.md) | State machine and workflow execution engine | P02, P06 | kilroy, event bus | seed |
| S02 | [Policy Engine](policy-engine.md) | Role-based permissions and transition guards | P05 | orchestrator-core, workflow definitions | seed |
| S03 | [Event Bus](event-bus.md) | Publish-subscribe communication backbone | P06 | all subsystems | seed |
| S04 | [Agent Runner](agent-runner.md) | Agent process spawning, monitoring, and lifecycle | P02, P05 | NTM, Claude Code hooks | seed |
| S05 | [Hook System](hook-system.md) | Bridge between probabilistic agents and deterministic workflows | P05, P06 | Claude Code hooks, agent-runner, orchestrator-core | seed |
| S06 | [Workspace Manager](workspace-manager.md) | Isolated working environments for agents | P04 | adze, agent-mail, git | seed |
| S07 | [Verifier Layer](verifier-layer.md) | Automated validation at quality gates | P04, P07 | CI tools, linters, test suites, LLM | seed |
| S08 | [Memory Layer](memory-layer.md) | Long-term knowledge storage and retrieval | P03, P07 | CASS, CASS Memory | seed |
| S09 | [Improvement Loop](improvement-loop.md) | Self-improving meta-process | P07, P03 | memory-layer, event-bus | seed |

## Dependency Map

The subsystems form a layered dependency structure:

```
  S09 Improvement Loop
   |        |
   v        v
  S08      S03 Event Bus  <--+-- all subsystems produce/consume
  Memory    |                 |
            v                 |
  S01 Orchestrator Core ------+
   |        |        |
   v        v        v
  S02      S04      S05
  Policy   Agent    Hook
  Engine   Runner   System
            |        |
            v        v
           S06      S07
           Workspace Verifier
           Manager   Layer
```

## Design Principles

1. **Composable, not monolithic.** Subsystems communicate through events and defined interfaces. No subsystem reaches into another's internals.
2. **Deterministic skeleton, probabilistic organs.** The orchestrator, policy engine, and event bus are fully deterministic. Agents and the improvement loop introduce controlled non-determinism.
3. **ZFC-compliant boundaries.** Subsystems that belong to the framework (S01-S03, S06) contain zero cognition. Subsystems that involve semantic judgment (S07 review, S09 analysis) delegate cognition to models.

## Documents
- [S01: Orchestrator Core](orchestrator-core.md) -- State machine and workflow execution
- [S02: Policy Engine](policy-engine.md) -- Permissions, constraints, freedom profiles
- [S03: Event Bus](event-bus.md) -- Communication backbone
- [S04: Agent Runner](agent-runner.md) -- Agent process lifecycle
- [S05: Hook System](hook-system.md) -- Probabilistic-to-deterministic bridge
- [S06: Workspace Manager](workspace-manager.md) -- Isolated environments
- [S07: Verifier Layer](verifier-layer.md) -- Quality gates
- [S08: Memory Layer](memory-layer.md) -- Knowledge storage and retrieval
- [S09: Improvement Loop](improvement-loop.md) -- Self-improving meta-process
