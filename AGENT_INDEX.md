# Harmonik -- Agent Discovery Index

> **Start here.** This is the master index for the harmonik knowledge base. Every document in the project is reachable from this file within two hops.

## What Is Harmonik?

Harmonik is a composable agentic orchestration system. Its objective: **maximize our one truly scarce resource -- human time and attention** -- by building structured, self-improving systems where agents autonomously move work from idea through implementation.

Core architectural principle: **deterministic skeleton, probabilistic organs**. The orchestration layer (workflows, transitions, governance) is fully deterministic. The intelligence layer (planning, critique, synthesis) is probabilistic (LLM-driven).

## How This Knowledge Base Works

- [Methodology](docs/methodology/METHODOLOGY.md) -- Document conventions, status lifecycle, cross-reference rules
- [Agent Guide](docs/methodology/AGENT_GUIDE.md) -- How to navigate, create, and update documents

## Knowledge Base Map

### Problems -- What We're Solving
[docs/problems/INDEX.md](docs/problems/INDEX.md)

| ID | Problem | Summary |
|----|---------|---------|
| P01 | [Human Attention Scarcity](docs/problems/human-attention-scarcity.md) | Human engineers are the bottleneck; every re-explanation wastes the scarcest resource |
| P02 | [Agent Persistence Gap](docs/problems/agent-persistence-gap.md) | Agents stop when humans stop pushing; no autonomous problem pursuit |
| P03 | [Knowledge Loss](docs/problems/knowledge-loss.md) | What agents learn dies with the session; no institutional memory |
| P04 | [System Coherence at Scale](docs/problems/system-coherence.md) | Entropy grows as systems get larger; architecture degrades without enforcement |
| P05 | [Agent Behavior Enforcement](docs/problems/behavior-enforcement.md) | Agents are probabilistic; need deterministic mechanisms to ensure process compliance |
| P06 | [Workflow Composition](docs/problems/workflow-composition.md) | Connecting independent workflows into larger systems is complex |
| P07 | [Feedback Loop Absence](docs/problems/feedback-loops.md) | Systems that execute but don't learn from their own execution |

### Goals -- What We Want to Achieve
[docs/goals/INDEX.md](docs/goals/INDEX.md)

| ID | Goal | Summary |
|----|------|---------|
| G01 | [Structured-Emergent Systems](docs/goals/structured-emergent-systems.md) | Highly structured systems that allow emergent behavior |
| G02 | [Persistent Problem Pursuit](docs/goals/persistent-problem-pursuit.md) | Systems that keep pushing on problems without constant human re-direction |
| G03 | [Independent Process-Following Actors](docs/goals/independent-process-following-actors.md) | Agents that reliably follow defined processes |
| G04 | [Learning and Improvement Loops](docs/goals/learning-and-improvement-loops.md) | Systems that learn from execution and improve over time |
| G05 | [Idea-to-Implementation Pipeline](docs/goals/idea-to-implementation-pipeline.md) | End-to-end automation from goal to working software |

### Concepts -- External Ideas We're Drawing From
[docs/concepts/INDEX.md](docs/concepts/INDEX.md)

| Source | Key Ideas |
|--------|-----------|
| [Kilroy](docs/concepts/kilroy.md) | Graph-as-workflow, deterministic routing, git-native checkpointing |
| [Symphony](docs/concepts/symphony.md) | Daemon orchestration, prompt-as-policy, multi-turn execution |
| [Harness Engineering](docs/concepts/harness-engineering.md) | Guides+sensors, constrain-to-empower, entropy management |
| [Zero Framework Cognition](docs/concepts/zero-framework-cognition.md) | Thin shells, delegate all cognition, mechanism vs policy |
| [AlphaGo-Modeled System](docs/concepts/alphago-system.md) | Deterministic skeleton, controlled openings, meta-process |
| [Gas Town Hooks](docs/concepts/gas-town-hooks.md) | Hook-driven behavior enforcement, bead lifecycle events |

### Components -- Tools We Have
[docs/components/INDEX.md](docs/components/INDEX.md)

**Internal** (our tools):
| Component | Purpose |
|-----------|---------|
| [kerf](docs/components/internal/kerf.md) | Spec-writing CLI. Planning/specification layer. Jigs, beads, works. |
| [adze](docs/components/internal/adze.md) | Machine configuration. Environment setup. Dependency graphs. |

**External** (third-party tools):
| Component | Purpose |
|-----------|---------|
| [ntm](docs/components/external/ntm.md) | Tmux-based agent process management. Swarm orchestration. |
| [agent-mail](docs/components/external/agent-mail.md) | Agent communication. File reservations. Identity. MCP tools. |
| [CASS](docs/components/external/cass.md) | Session search. Episodic memory. Cross-agent knowledge. |
| [CASS Memory](docs/components/external/cass-memory.md) | Institutional memory. Three-layer cognition. Playbook rules. |
| [Kilroy](docs/components/external/kilroy.md) | Workflow execution engine. DOT graph pipelines. |

### Subsystems -- How Harmonik Decomposes
[docs/subsystems/INDEX.md](docs/subsystems/INDEX.md)

| ID | Subsystem | Purpose | Key Components |
|----|-----------|---------|----------------|
| S01 | [Orchestrator Core](docs/subsystems/orchestrator-core.md) | State machine + workflow execution | kilroy (or custom) |
| S02 | [Policy Engine](docs/subsystems/policy-engine.md) | Role permissions + transition guards | config/YAML |
| S03 | [Event Bus](docs/subsystems/event-bus.md) | Pub-sub communication backbone | custom or external |
| S04 | [Agent Runner](docs/subsystems/agent-runner.md) | Spawn, monitor, manage agent processes | ntm |
| S05 | [Hook System](docs/subsystems/hook-system.md) | Bridge between agents and workflow | Claude Code hooks |
| S06 | [Workspace Manager](docs/subsystems/workspace-manager.md) | Isolated work environments | adze, agent-mail |
| S07 | [Verifier Layer](docs/subsystems/verifier-layer.md) | Quality gates + automated verification | CI tools, LLM |
| S08 | [Memory Layer](docs/subsystems/memory-layer.md) | Long-term knowledge storage + retrieval | CASS, CASS memory |
| S09 | [Improvement Loop](docs/subsystems/improvement-loop.md) | Self-improving meta-process | memory, event bus |

### Ideas -- Brainstorm Captures
[docs/ideas/INDEX.md](docs/ideas/INDEX.md)

| ID | Idea | Priority |
|----|------|----------|
| I01 | [Hook-Driven Agent Behavior](docs/ideas/hook-driven-agent-behavior.md) | High -- core mechanism |
| I02 | [Deterministic Skeleton, Probabilistic Organs](docs/ideas/deterministic-skeleton-probabilistic-organs.md) | High -- architectural principle |
| I03 | [Composable Multi-Workflow Systems](docs/ideas/composable-multi-workflow-systems.md) | High -- system composition |
| I04 | [AlphaGo Search for Coding](docs/ideas/alphago-search-for-coding.md) | Medium -- optimization |
| I05 | [Model Pyramid Cost Optimization](docs/ideas/model-pyramid-cost-optimization.md) | Medium -- cost management |
| I06 | [Agent Specialization Through Constraints](docs/ideas/agent-specialization-through-constraints.md) | Medium -- agent design |
| I07 | [Filesystem as Coordination Substrate](docs/ideas/filesystem-as-coordination-substrate.md) | Medium -- coordination |

### Collaboration Log
[docs/log/INDEX.md](docs/log/INDEX.md)

Most recent entries:
- [2026-04-13: Initial Brainstorm Session](docs/log/2026-04-13-initial-brainstorm.md) -- First comprehensive capture

## Deep References
- [AlphaGo-Modeled Orchestration System](refs/AlphaGo-modeled-orch-system.md) -- 800+ line architectural reference document

## Original Notes (preserved)
- [docs/00_objective.md](docs/00_objective.md) -- Original objective statement
- [docs/01_architecture.md](docs/01_architecture.md) -- Architecture principles
- [docs/01_components.md](docs/01_components.md) -- Component references
- [docs/02_spec-management.md](docs/02_spec-management.md) -- Spec management references
