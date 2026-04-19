---
title: NTM (Named Tmux Manager)
status: explored
type: component
category: external
source: https://github.com/Dicklesworthstone/ntm
related: [docs/components/internal/kerf.md, docs/components/external/agent-mail.md, docs/problems/agent-persistence-gap.md]
created: 2026-04-13
updated: 2026-04-13
---

# NTM (Named Tmux Manager)

## Summary
NTM is a Go binary that manages the physical execution of AI agent processes through tmux. It handles spawning, monitoring, health-checking, and recovering agent processes. NTM treats agents as long-running processes with known behavioral patterns -- each agent type (Claude, Codex, Gemini, Cursor, etc.) has its own exit sequences, ready-state detection, and rate limit patterns. This agent-aware process management is what distinguishes it from generic process supervisors.

## Key Capabilities

### Swarm Orchestration
Plan-based execution model. A SwarmPlan defines sessions, panes, agent assignments, and prompts. NTM creates the tmux infrastructure, launches agents, injects prompts, and monitors execution. This is the primary interface for running multi-agent workloads.

### Multi-Agent Lifecycle Management
Full lifecycle coverage: launching, health monitoring with multiple signal types, rate limit detection, auto-respawn with graceful degradation. When an agent hits a rate limit or crashes, NTM handles recovery without human intervention.

### Agent-Specific Knowledge
Each supported agent type has a profile defining its exit sequence, ready-state patterns, rate limit indicators, and behavioral quirks. This means NTM knows how to detect when Claude is waiting for input vs. when Codex has hit a limit -- knowledge that would otherwise require human observation.

### Pipeline System
Sequential multi-stage pipelines where output from one agent flows to the next. This supports workflows where different agents handle different phases (e.g., planning agent feeds implementation agent feeds review agent).

### Event System
Pub-sub event bus with bounded concurrency, wildcard subscriptions, and a history buffer. Components can subscribe to agent lifecycle events (started, completed, failed, rate-limited) and react programmatically.

### Checkpoint and Recovery
Complete session snapshots capturing tmux layout, pane states, git state, and scrollback. Sessions can be restored after crashes or machine restarts, preserving the full execution context.

### File Reservations
Watches pane output for file edit indicators and automatically reserves files through Agent Mail. This prevents concurrent agents from editing the same file -- a coordination concern handled transparently at the process management layer.

### Supervisor Pattern
Manages long-running daemon processes (not just one-shot agent tasks) with health monitoring and auto-restart. Suitable for persistent services that agents depend on.

### Account Rotation
Multi-account management via the caam CLI for rate limit recovery. When one account is rate-limited, NTM can rotate to another, maintaining throughput across extended workloads.

## Integration Points for Harmonik

NTM is the **process management layer**. Its role in the system:

- **Execution substrate for kerf**: When kerf dispatches beads to workers, NTM is the mechanism that spawns and manages the actual agent processes.
- **Agent Mail bridge**: NTM's file reservation feature directly integrates with Agent Mail, providing automatic coordination without agents needing to explicitly manage reservations.
- **Health and lifecycle signals**: NTM's event system provides the raw signals that a monitoring subsystem needs -- agent starts, completions, failures, rate limits.
- **Swarm plans as execution format**: SwarmPlans could serve as the execution representation that the workflow engine produces and NTM consumes.
- **Rate limit resilience**: Account rotation and auto-respawn keep agent workloads running through disruptions that would otherwise require human intervention, directly addressing P01.

## Limitations and Gaps

- **Tmux dependency**: Requires tmux, which limits deployment to Unix-like systems with terminal access. Not suitable for headless cloud execution without modification.
- **Local-only**: NTM manages processes on a single machine. Multi-machine agent swarms require running NTM instances on each machine with external coordination.
- **Prompt injection model**: Agents receive work through prompt injection into tmux panes. This is pragmatic but fragile -- it depends on the agent's input handling and can break with agent updates.
- **No workflow semantics**: NTM manages processes, not workflows. It does not understand task dependencies, success criteria, or workflow graphs. That logic must live elsewhere.

## Open Questions

1. How should NTM's SwarmPlan format relate to Kilroy's DOT pipeline format? Should they be the same, or should there be a translation layer?
2. Can NTM's event system serve as the foundation for harmonik's monitoring subsystem, or does monitoring need its own infrastructure?
3. What is the right boundary between NTM's process-level health monitoring and application-level health checks that understand task semantics?
