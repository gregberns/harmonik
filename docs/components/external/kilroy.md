---
title: Kilroy
status: explored
type: component
category: external
source: https://github.com/danshapiro/kilroy
related: [docs/concepts/kilroy.md, docs/components/internal/kerf.md, docs/components/external/ntm.md]
created: 2026-04-13
updated: 2026-04-13
---

# Kilroy

## Summary
Kilroy is a Go binary that orchestrates multi-stage AI workflows using Graphviz DOT files as the pipeline definition format. The graph IS the workflow -- declarative, visual, version-controllable, and diffable. Kilroy provides deterministic routing, git-native checkpointing, multi-model fan-out, and failure classification. It includes a battle-tested reference pipeline covering the full cycle from bootstrap through implementation to postmortem.

For a deeper treatment of Kilroy's concepts and design patterns, see the [concept digest](../../concepts/kilroy.md).

## Key Capabilities

### DOT Graph Pipelines
Workflows are Graphviz DOT files. Each node is a handler (LLM call, tool invocation, human gate, conditional branch). Edges define transitions. The graph is the single source of truth for execution order, and it is renderable as a visual diagram.

### Deterministic Routing
A 5-step priority cascade for edge selection: condition match, label match, suggested IDs, weight, lexical order. Given the same state, the same edge is always selected. This makes workflow execution reproducible and debuggable.

### Git-Native Checkpointing
One commit per node completion. Runs are resumable from any point via filesystem state, context database, or git branch. Crashes and interruptions are cheap -- no work is lost and execution can resume exactly where it stopped.

### Multi-Provider Agents
Supports OpenAI, Anthropic, Google, and other model providers. Model stylesheets (CSS-like) assign models to node types and roles, separating model selection from workflow logic.

### Fan-Out and Fan-In
Parallel execution with isolation via git worktrees and forked context. Each parallel branch operates independently with no shared mutable state. Results merge at the fan-in point.

### Failure Classification
Three failure classes: deterministic (will always fail -- skip), structural (needs different approach), transient infrastructure (retry). Cycle detection with signature tracking prevents infinite retry loops. This taxonomy makes failure handling systematic rather than ad-hoc.

### Goal Gates
Critical nodes that cannot be bypassed. The pipeline cannot complete until goal gates pass. This provides hard guarantees about workflow completion -- certain quality checks are non-negotiable.

### Reference Pipeline
A battle-tested pipeline template: bootstrap, definition of done, planning, implementation, verification, review, postmortem. This encodes a proven workflow pattern that new projects can adopt and customize.

### HTTP Server Mode
REST API and SSE for remote orchestration. Pipelines can be launched and monitored programmatically, not just from the command line.

### Natural Language Ingestion
English requirements can be converted to validated DOT pipelines via LLM. The system can bootstrap its own workflow definitions from human-language descriptions.

## Integration Points for Harmonik

Kilroy is a **candidate for the workflow execution engine**. Its role in the system:

- **Workflow definition and execution**: Kilroy's DOT graph model provides a production-tested format for defining multi-stage agent workflows. This could be the format harmonik uses to express orchestration logic.
- **Checkpoint/resume supports durability**: Git-native checkpointing means workflows survive crashes, meeting the persistence requirements of G02 and addressing P02.
- **Failure handling at scale**: The three-class failure taxonomy and cycle detection are essential for running agent workflows that may fail in complex ways. This is operational maturity that would be expensive to build from scratch.
- **Fan-out maps to bead parallelization**: Kilroy's parallel isolation via worktrees maps to kerf's layer-based bead parallelization. Independent beads could execute as fan-out branches in a Kilroy pipeline.
- **Goal gates enforce quality**: Non-bypassable quality checks align with harmonik's need for structural verification and process enforcement (G03).
- **Integration with kerf**: Kerf produces specs and bead graphs; Kilroy could consume them as pipeline definitions. The spec-to-pipeline translation is a key integration point.

## Limitations and Gaps

- **Static graph model**: The DOT file is fixed at pipeline start. Kilroy does not support adding or removing nodes at runtime based on agent discoveries. Harmonik likely needs dynamic workflow modification.
- **No native Agent Mail integration**: Kilroy does not use Agent Mail for coordination. Integrating it into the harmonik communication layer would require extension or wrapping.
- **Single-pipeline focus**: Kilroy manages one pipeline at a time. Orchestrating multiple concurrent pipelines (e.g., several kerf works executing simultaneously) would need an additional layer.
- **External project**: Kilroy is not under our control. API changes, diverging priorities, or abandonment are risks that come with depending on an external project.

## Open Questions

1. Should harmonik adopt Kilroy as the workflow engine, build its own, or use Kilroy's patterns in a custom implementation?
2. How would dynamic workflow modification work? Could Kilroy's DOT model be extended to support runtime graph mutation, or is a different model needed?
3. What is the right adapter between kerf's bead graphs and Kilroy's DOT format? Is the mapping straightforward, or are there semantic gaps?
