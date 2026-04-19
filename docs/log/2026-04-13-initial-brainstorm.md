---
title: "Initial Brainstorm Session"
type: log-entry
date: 2026-04-13
participants: [human, claude-opus]
---

# 2026-04-13: Initial Brainstorm Session

## Context

First comprehensive brainstorming session for harmonik. The goal was to capture the full problem space, design goals, external concepts, component inventory, subsystem decomposition, and key ideas in a structured knowledge base. This session transformed a collection of reference documents and rough ideas into an organized documentation structure.

## What Was Captured

### Problem Space (7 problems)
Identified the core problems harmonik aims to solve: human attention scarcity (P01), agent persistence gap (P02), knowledge loss across sessions (P03), system coherence at scale (P04), agent behavior enforcement (P05), workflow composition complexity (P06), and feedback loop absence (P07). Each problem was documented with concrete manifestations and connections to the goal space.

### Design Goals (5 goals)
Translated problems into desired capabilities: structured emergent systems (G01), persistent problem pursuit (G02), independent process-following actors (G03), learning and improvement loops (G04), and the idea-to-implementation pipeline (G05). Goals bridge problems (what is wrong) and subsystems (what we build).

### External Concepts (6 digests)
Distilled key ideas from reference materials: Kilroy (graph-as-workflow, checkpoint/resume), Symphony (daemon orchestration, prompt-as-policy), harness engineering (guides+sensors, entropy management), Zero Framework Cognition (thin shells, mechanism vs cognition), AlphaGo-modeled system (tree search, deterministic skeleton), and Gas Town hooks model (bead lifecycle events).

### Component Inventory
Cataloged internal components (kerf, adze, NTM, agent-mail, CASS, CASS Memory) and external dependencies (Claude Code, MCP servers). Mapped capabilities and gaps for each.

### Subsystem Decomposition (9 subsystems)
Defined the architectural seams: orchestrator core (S01), policy engine (S02), event bus (S03), agent runner (S04), hook system (S05), workspace manager (S06), verifier layer (S07), memory layer (S08), and improvement loop (S09). Established the dependency map between subsystems.

### Key Ideas (7 ideas)
Captured brainstorm ideas for mechanisms and approaches: hook-driven agent behavior (I01), deterministic skeleton / probabilistic organs (I02), composable multi-workflow systems (I03), AlphaGo search for coding (I04), model pyramid cost optimization (I05), agent specialization through constraints (I06), and filesystem as coordination substrate (I07).

## Key Decisions

1. **Adopted "deterministic skeleton, probabilistic organs" as the core architectural principle.** This is the single most important design decision. The orchestration layer is fully deterministic. The intelligence layer is fully probabilistic. The boundary between them is clean and enforced.

2. **Organized knowledge base into a layered taxonomy: problems, goals, concepts, components, subsystems, ideas.** Each layer connects to its neighbors. Problems motivate goals. Goals guide subsystem design. Concepts inform approaches. Components provide building blocks. Ideas propose mechanisms.

3. **Identified 7 core problems, 5 goals, 9 subsystems, and 7 key ideas.** The numbers feel right -- enough to cover the space without artificial decomposition. Each item is distinct and necessary.

4. **Positioned the hook system (S05) as the critical bridge between agent behavior and workflow progression.** Hooks are not just one subsystem among nine -- they are the mechanism that makes the whole architecture work. Without hooks, agents are probabilistic islands. With hooks, agents are orchestrated participants in deterministic workflows.

5. **Established the AlphaGo reference architecture as the north star.** When design decisions conflict, the AlphaGo-modeled system's principles take precedence. Other concepts (Kilroy, Symphony, ZFC) are important inputs, but the AlphaGo architecture is the integration point.

## Open Questions for Next Session

1. **Workflow definition format.** DOT (Kilroy-style) vs YAML vs custom DSL? DOT has graph tooling but limited expressiveness. YAML is ubiquitous but verbose. A custom DSL could be optimal but adds a learning curve. What are the real trade-offs?

2. **Build on Kilroy or start fresh?** Kilroy has graph-as-workflow and checkpoint/resume already working. But it may be over-specialized for its original use case. Is it cheaper to adapt Kilroy or build a simpler custom state machine?

3. **The ZFC tension.** We need deterministic workflows (mechanism) but ZFC says no cognition in the framework. Where exactly is the line? When a workflow transition depends on "did the review pass?" -- is that mechanism (deterministic evaluation of a boolean) or cognition (understanding what the review said)?

4. **The MVP question.** What is the smallest useful slice of this system? A single workflow (spec to implementation to review) with two agent roles and a hook-driven state machine? What can be deferred?

5. **Multi-repo orchestration.** Real products span multiple repositories. How do we coordinate agents working across repo boundaries? The product bus concept from agent-mail is one approach, but it needs deeper analysis.

6. **Workflow verification.** How do we verify composed workflows (deadlock detection, reachability analysis) before execution? Petri net formalism is one path. Are there simpler approaches that give us enough confidence?

7. **Work item granularity.** What is the right size for "beads" / work items? Too large and agents struggle with context limits. Too small and coordination overhead dominates. Is there a heuristic, or does it depend on the task domain?

8. **Bootstrapping.** The system needs to be built by the system, but the system does not exist yet. How do we bootstrap? Build the simplest possible version manually, then use it to build the next version?

## External Resources to Explore Further

- **Petri net formalism** for workflow verification -- can we borrow established theory for deadlock/reachability analysis?
- **Temporal workflow engine** as a potential orchestration substrate -- does it solve problems we would otherwise build from scratch?
- **LangGraph / LangChain agent middleware patterns** -- what have they learned about agent orchestration at scale?
- **Formal methods for multi-agent coordination** -- academic literature on multi-agent systems and coordination protocols

## Components That Need Deeper Investigation

- **Claude Code hooks API**: What events are available? What can hooks do? What are the limitations? This is load-bearing for the entire architecture (I01, S05) and we need precise knowledge of its capabilities.
- **Kilroy's HTTP server mode**: Could it serve as a remote orchestration backend? What is the API surface? How does it handle concurrent workflows?
- **NTM + agent-mail integration patterns**: How exactly do they compose? What is the message format? How does cross-repo communication work?
- **adze for agent workspace provisioning**: Can it create isolated, pre-configured workspaces on demand? What is the setup time? How does teardown work?

## Emerging Patterns

Several patterns recurred across different topics during this session:

- **Files over messages.** Every successful reference system (Symphony, kerf, harness engineering, agent-mail) coordinates through the filesystem, not through conversation or API calls. This is not a coincidence -- it is the only coordination mechanism that survives session boundaries, scales to multiple agents, and provides auditability through version control.
- **Roles as boundaries.** Effective multi-agent systems do not create generalist agents. They create specialized agents with scoped permissions. The specialization is not just a prompt -- it is structural (tool access, file permissions, available actions). This appeared in the AlphaGo reference, in harness engineering, and in ZFC.
- **Hooks as the universal connector.** The hook mechanism appeared in nearly every discussion. Hooks connect agents to workflows, workflows to each other, and the deterministic layer to the probabilistic layer. If harmonik has one indispensable mechanism, it is hooks.
- **Verification as a forcing function.** Verifiable outcomes change everything. When you can check whether something worked, you can use search, retry, fan-out, and improvement loops. When you cannot check, you are limited to single-pass generation with human review. Maximizing the surface area of automated verification is a meta-goal.

## Risks and Concerns

- **Over-engineering risk.** Nine subsystems is a lot of architecture for a system that does not yet exist. There is a real risk of spending months on framework design before any agent does useful work. The MVP question is critical.
- **Hook system dependency.** The entire architecture depends on Claude Code's hook system being sufficiently capable. If hooks are limited (e.g., no custom events, no async execution, no state access), the architecture needs significant reworking.
- **Complexity budget.** Composed multi-workflow systems with verification, model routing, and improvement loops is a complex system. Each piece adds value, but the integration complexity may be non-linear. Need to be disciplined about what enters the MVP.

## Session Notes

This was a breadth-first exploration. Every topic was touched; none was fully resolved. The next session should pick one or two open questions and go deep. The MVP question (open question 4) is probably the highest priority -- it determines what we build first and when we can start using the system to build itself.

The documentation structure itself is an asset. Having problems, goals, concepts, components, subsystems, and ideas as separate, cross-linked documents means we can evolve each independently and trace how decisions connect to motivations. The structure should be maintained as the project evolves.
