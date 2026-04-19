---
title: Kilroy
status: explored
type: concept
source: https://github.com/danshapiro/kilroy
related: [alphago-system.md, zero-framework-cognition.md]
created: 2026-04-13
updated: 2026-04-13
---

# Kilroy

## What It Is
Kilroy is a graph-based agentic workflow engine where pipelines are defined as Graphviz DOT files. The graph IS the workflow -- no imperative control flow, no hidden routing logic. Pipelines are declarative, visual, version-controllable, and diffable.

## Key Concepts

### Graph-as-Workflow
Pipelines are DOT files rendered by Graphviz. Each node is a handler (LLM call, tool invocation, human gate, conditional branch). Edges define transitions. The graph is the single source of truth for execution order.

### Deterministic Edge Selection
A 5-step priority cascade governs which edge to follow next: condition match, label match, suggested IDs, weight, lexical order. This makes routing fully reproducible -- given the same state, the same edge is always selected.

### Git-Native Checkpointing
One commit per node completion. Runs are resumable from any point and fully auditable. Three resume sources: filesystem state, CXDB (context database), or git branch. This means crashes and interruptions are cheap.

### Handler Types
Different node shapes map to different operations: codergen (code generation), wait.human (human-in-the-loop gate), conditional (branching), parallel (fan-out), tool (external invocation), stack.manager_loop (recursive orchestration).

### Multi-Model Fan-Out
The same prompt sent to 3 different models, then results synthesized. Used for definition of done generation, planning, and review. Model stylesheets assign models to roles -- separation of model selection from workflow logic.

### Failure Classification + Cycle Detection
Failure signatures are tracked to prevent infinite retry loops. Three failure classes: deterministic (will always fail -- skip), structural (needs different approach), transient_infra (retry). Cycle detection caps repeated traversals.

### Goal Gates
Critical nodes that cannot be bypassed. The pipeline cannot exit until goal gates pass. This provides hard guarantees about workflow completion quality.

### Parallel Isolation
Each parallel branch gets an isolated git worktree and forked context. Branches execute independently and merge results. No shared mutable state between parallel paths.

### Fidelity Modes
Context management strategies for LLM nodes: full (everything), truncate (cut old content), compact (structured compression), summary (LLM-generated summary). Different nodes can use different strategies.

### Ingestion from Natural Language
English requirements are converted to validated DOT pipelines via LLM. The system can bootstrap its own workflow definitions from human-language descriptions.

### Separation of Concerns
Three distinct layers: orchestration engine (graph traversal, edge selection), agent execution loop (handler invocation, retries), LLM client (model calls, context management). Clean interfaces between each.

## Relevance to Harmonik

Kilroy provides the strongest model for **deterministic workflow definition and execution**. Its contributions:

- **Graph DSL**: Declarative workflow definition that is visual, diffable, and version-controlled. Harmonik should adopt a similar declarative approach.
- **Checkpoint/resume**: Git-native state persistence makes workflows resumable. This pattern maps directly to harmonik's need for durable execution.
- **Failure handling**: The three-class failure taxonomy and cycle detection solve real problems in agentic loops.
- **Parallel isolation**: Git worktree-based isolation is a practical pattern for concurrent agent work.

The main limitation: Kilroy's graph model is static -- the DOT file is fixed at pipeline start. Harmonik needs dynamic workflow modification (adding/removing nodes at runtime based on agent discoveries).
