---
title: "S04: Agent Runner"
status: seed
type: subsystem
solves: [P02, P05]
uses: [NTM, Claude Code hooks]
related: [docs/components/external/ntm.md, docs/subsystems/hook-system.md, docs/subsystems/workspace-manager.md]
created: 2026-04-13
updated: 2026-04-13
---

# S04: Agent Runner

## Summary
The agent runner spawns, monitors, and manages agent processes. It handles the full lifecycle of each agent: launch in an isolated environment, inject prompts and hooks, monitor health through multiple signals, detect failures (including rate limits and hangs), and recover through retry, respawn, or escalation. It is the subsystem that keeps agents running when humans are not watching (P02).

## Purpose
Agentic systems fail when agents stop. An agent hits a rate limit and exits. An agent's tmux session crashes. An agent enters an infinite loop. An agent completes its task but nobody dispatches the next one. The agent runner solves all of these by treating agent processes as managed services: launched with configuration, monitored continuously, and recovered automatically.

The agent runner also owns agent-specific knowledge. Claude Code, Codex, and Gemini each have different lifecycle patterns, exit sequences, ready-state detection, and failure modes. Rather than abstracting these differences away behind a generic interface, the agent runner embraces them -- each agent type gets its own lifecycle handler.

## Key Responsibilities
- **Agent process lifecycle.** Launch agent processes (via NTM or direct process management), monitor them during execution, and terminate them cleanly when work is done or timeouts are reached.
- **Environment isolation.** Ensure each agent runs in its own workspace (coordinating with workspace manager S06), with its own git worktree, environment variables, and tool access.
- **Prompt injection.** Assemble the agent's initial prompt from workflow context, task definition, policy constraints, and relevant memory. Deliver the prompt at launch time.
- **Hook attachment.** Configure Claude Code hooks (or equivalent) for the agent's session, connecting agent lifecycle events to the hook system (S05).
- **Health monitoring (multi-signal).** Monitor agent health through multiple channels: process status, output activity, file system changes, git activity. No single signal is sufficient -- an agent can be "running" but stuck.
- **Rate limit detection and account rotation.** Detect rate limit responses and either wait, switch to a different API account, or switch to a different model.
- **Failure recovery.** Classify failures (transient, structural, deterministic) and apply the appropriate recovery strategy: retry, respawn with modified prompt, or escalate to human.

## Interfaces

**Inputs:**
- Agent dispatch requests from orchestrator core (S01)
- Policy constraints from policy engine (S02) -- injected into agent configuration
- Workspace assignments from workspace manager (S06)

**Outputs:**
- Agent lifecycle events emitted to event bus (S03): agent_started, agent_output, agent_completed, agent_failed, agent_rate_limited
- Hook triggers sent to hook system (S05) at lifecycle boundaries
- Health status reports (for monitoring dashboards or alerting)

## Design Principles
- **Agent-specific, not agent-generic.** Each agent type (Claude Code, Codex, Gemini) has its own lifecycle handler with type-specific ready-state detection, exit sequence handling, and failure classification. Generic abstractions that hide these differences lead to fragile monitoring.
- **Keep agents running.** The default behavior is persistence. If an agent exits unexpectedly, the runner restarts it with context recovery. If an agent hits a rate limit, the runner waits or rotates. Human intervention is the last resort, not the first.
- **Observable processes.** Every agent process is fully observable: its stdout, its file changes, its git activity, its resource consumption. The runner makes this observability available to other subsystems through the event bus.

## Candidate Implementations
- **NTM-based.** Use Named Tmux Manager for process spawning and session management. Advantage: proven, handles tmux lifecycle well. Risk: tmux-specific, may not generalize to non-terminal agents.
- **Direct process management.** Spawn agents as child processes with stdout/stderr capture. Advantage: no tmux dependency. Risk: loses the interactive session model that makes debugging easier.
- **Hybrid.** NTM for interactive agents (Claude Code), direct process management for headless agents (API-only models).

## Open Questions
1. How should the agent runner handle agents that need to interact with each other mid-task (e.g., a builder asking a reviewer a clarifying question)? Is that handled through agent-mail, or does the runner need direct inter-agent communication support?
2. What is the right granularity for health monitoring? Per-second process checks are expensive; per-minute checks may miss fast failures. Should monitoring frequency adapt based on the agent's current state?
3. How do we handle graceful shutdown of agent processes that are mid-task when a workflow is cancelled or a timeout fires?

## Cross-References
- [S01: Orchestrator Core](orchestrator-core.md) -- dispatches agent work requests
- [S05: Hook System](hook-system.md) -- hooks attach to agent lifecycle events managed by the runner
- [S06: Workspace Manager](workspace-manager.md) -- provides isolated environments for agents
- [P02: Agent Persistence Gap](../problems/agent-persistence-gap.md) -- the runner keeps agents running autonomously
- [P05: Agent Behavior Enforcement](../problems/behavior-enforcement.md) -- hook injection enforces behavior
- [NTM component](../components/external/ntm.md) -- primary process management tool
