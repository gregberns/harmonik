---
title: kerf
status: explored
type: component
category: internal
source: ~/github/kerf
related: [docs/components/external/ntm.md, docs/components/external/agent-mail.md, docs/problems/agent-persistence-gap.md]
created: 2026-04-13
updated: 2026-04-13
---

# kerf

## Summary
Kerf is a spec-writing CLI tool for AI agents. Single Go binary. It manages the "thinking before coding" phase -- structured planning that decomposes problems into implementable units before any code is written. Kerf enforces a spec-first workflow where work progresses through defined stages, and its jig system provides process templates that agents follow without improvisation.

## Key Capabilities

### Spec-First Workflow
Work progresses through a fixed stage sequence: problem-space, decompose, research, spec, tasks, ready. Each stage has defined entry/exit criteria. This prevents agents from jumping straight to implementation without adequate planning.

### Jigs (Process Templates)
Jigs define the passes an agent makes over a piece of work. Built-in jigs: plan, spec, bug, implementation, spike, retrofit. Each jig prescribes what the agent does at each pass, what artifacts it produces, and what checks must pass before advancing. Custom jigs are supported for project-specific workflows.

### Work Management
Works have codenames, statuses, and sessions. They live on a bench (~/.kerf/) outside of git until finalization. This keeps speculative and in-progress planning out of the repository until it reaches a publishable state.

### Multi-Agent Coordination via Agent-Mail
The orchestrator registers with Agent Mail, spawns workers via NTM, sends beads (task units) per worker, and polls for completion. This is kerf's native multi-agent dispatch model.

### Beads as Task Decomposition
Plans break into implementation beads -- discrete, implementable units with explicit dependency graphs. Beads declare what they depend on, enabling layer-based parallelization where independent beads execute concurrently.

### Session and Resumability
SESSION.md preserves context across agent invocations. Shelve/resume operations reload full context. Stale session detection prevents agents from working with outdated state.

### Dependency Tracking
Works can declare dependencies on other works, including cross-project dependencies. This enables multi-repo planning where a change in one project requires coordinated changes in others.

### Verification (Square Checks)
Structural verification that work matches jig expectations. Square checks validate artifacts, stage completeness, and structural correctness without requiring LLM judgment.

### Finalization to Git
Moves specs from the bench into the repository with pre-flight checks. This is the publish gate -- work is not visible to other agents or humans until it passes finalization.

## Integration Points for Harmonik

Kerf is the **planning and specification layer**. Its role in the system:

- **Upstream of execution**: Kerf produces the specs and bead graphs that the workflow execution layer (Kilroy or equivalent) consumes. No code is written without a kerf-produced plan.
- **Agent-Mail native**: Kerf already integrates with Agent Mail for multi-agent coordination. This is the primary communication channel for dispatching beads to workers.
- **NTM as process substrate**: Kerf spawns worker agents through NTM. The planning layer directly drives the process management layer.
- **Bead graphs feed parallelization**: The dependency structure in bead graphs maps to fan-out/fan-in execution patterns. Layers of independent beads can execute concurrently.
- **Session persistence addresses P02**: Kerf's session and shelve/resume model directly addresses the Agent Persistence Gap (P02) for planning work.

## Limitations and Gaps

- **No runtime replanning**: Once beads are dispatched, the plan is fixed. If implementation reveals the spec was wrong, there is no built-in mechanism to revise the plan mid-execution.
- **Single-orchestrator model**: The current agent-mail integration assumes one orchestrator coordinating workers. Nested or hierarchical orchestration is not yet supported.
- **Bench is local**: The ~/.kerf/ bench is machine-local. Multi-machine planning requires manual coordination or external sync.

## Open Questions

1. How should kerf's bead graphs integrate with Kilroy's DOT pipeline format? Is there a natural mapping, or do we need an adapter?
2. Should kerf own the "what to work on next" decision, or should that be a separate orchestration concern?
3. How do we handle plan invalidation when implementation feedback contradicts the spec?
