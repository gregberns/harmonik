---
title: "S04: Agent Runner"
status: seed
type: subsystem
solves: [P02, P05]
uses: [NTM, Claude Code, Pi (pi-mono), Claude Code hooks]
language: Go
related: [docs/components/external/ntm.md, docs/concepts/digital-twins.md, docs/subsystems/hook-system.md, docs/subsystems/workspace-manager.md, docs/subsystems/scenario-harness.md, docs/subsystems/memory-layer.md]
created: 2026-04-13
updated: 2026-04-19
---

# S04: Agent Runner

## Summary
The agent runner spawns, monitors, and manages agent processes. It handles the full lifecycle of each agent: launch in an isolated environment, inject prompts and hooks, monitor health through multiple signals, detect failures (including rate limits and hangs), and recover through retry, respawn, or escalation. It is the subsystem that keeps agents running when humans are not watching (P02).

## Purpose
Agentic systems fail when agents stop. An agent hits a rate limit and exits. An agent's tmux session crashes. An agent enters an infinite loop. An agent completes its task but nobody dispatches the next one. The agent runner solves all of these by treating agent processes as managed services: launched with configuration, monitored continuously, and recovered automatically.

The agent runner also owns agent-specific knowledge. Claude Code, Codex, and Gemini each have different lifecycle patterns, exit sequences, ready-state detection, and failure modes. Rather than abstracting these differences away behind a generic interface, the agent runner embraces them -- each agent type gets its own lifecycle handler.

## Key Responsibilities
- **Agent process lifecycle.** Launch agent processes via an NTM wrapper, monitor them during execution, and terminate them cleanly when work is done or timeouts are reached.
- **Binary selection (real vs twin).** Each agent role's workflow/policy configuration names the binary to launch (e.g., `claude` vs `claude-twin`). The handler launches whatever the configuration specifies. There is no test-mode branch in the runner -- twin support comes from binary substitution at the configuration layer. See [docs/concepts/digital-twins.md](../concepts/digital-twins.md).
- **Environment isolation.** Ensure each agent runs in its own workspace (coordinating with workspace manager S06), with its own git worktree, environment variables, and tool access.
- **Prompt injection.** Assemble the agent's initial prompt from workflow context, task definition, policy constraints, and relevant memory. Deliver the prompt at launch time.
- **Hook attachment.** For the `claude-code` agent type, hook attachment is governed by [specs/claude-hook-bridge.md](../../specs/claude-hook-bridge.md), which defines `.claude/settings.json` materialization at workspace creation, the `harmonik hook-relay` subcommand that translates Claude hook events into harmonik progress-stream messages on the daemon socket, and the pre-generated `claude_session_id` flow used for routing and `phase = implementer-resume` continuity. Other agent types re-realize the bridge surface per their own bridge specs (post-MVH).
- **Session-log location emission.** At agent launch, emit a `session_log_location` event to the event bus identifying where the agent's own log file will be written (e.g., for Claude: `~/.claude/projects/<project-slug>/<session-uuid>.jsonl`; for Pi: its equivalent format). This is how the memory layer (S08) and CASS find the right files to ingest. Each handler owns the per-agent-type knowledge of where logs land.
- **Health monitoring (multi-signal).** Monitor agent health through multiple channels: process status, output activity, file system changes, git activity. No single signal is sufficient -- an agent can be "running" but stuck.
- **Rate limit detection and account rotation.** Detect rate limit responses and either wait, switch to a different API account, or switch to a different model.
- **Failure recovery.** Classify failures (transient, structural, deterministic) and apply the appropriate recovery strategy: retry, respawn with modified prompt, or escalate to human.

## Initial Handler Set

| Agent type | Real binary | Twin binary | Source | Notes |
|---|---|---|---|---|
| Claude Code | `claude` | `claude-twin` | Anthropic | First-class, primary agent. Handler reads `~/.claude/projects/<slug>/<uuid>.jsonl` for session log. |
| Pi | `pi` (with model flag, e.g., `--model glm-4.6`) | `pi-twin` | [badlogic/pi-mono](https://github.com/badlogic/pi-mono) | Second-class, validates the "agent type abstraction" by being meaningfully different from Claude Code. Pi's log format / location TBD by handler. |

Future handlers (Codex, Gemini, others) follow the same pattern: a real binary, a twin binary, a Go handler that knows the agent type's lifecycle and log conventions.

## Inspectability via tmux

NTM-based launch is *not* an arbitrary implementation choice -- it is a requirement that the running system be inspectable by attaching to tmux windows. Operators must be able to walk through the tmux session list, attach to any active agent, and see exactly what that agent is doing in real time. Direct-process or headless implementations may be added later for non-interactive contexts, but the default and primary mode is tmux-visible.

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

## Implementation Direction

- **Go wrapper around NTM.** A Go package that wraps NTM's CLI for programmatic launch / monitoring / kill. This wrapper is the primary surface other subsystems use; they do not shell out to NTM directly.
- **Per-agent-type handler.** Each handler is a Go interface implementation that knows: how to assemble the launch command for its agent type, how to detect ready-state in the agent's output, how to detect completion, where the agent writes its session log, what exit codes mean what.
- **Stdout/stderr capture.** Generalized capture mechanism is shared across handlers (parked detail; needs definition alongside the orchestrator's "node types" doc -- non-agentic process nodes have the same need).
- **Twin binaries shipped per real handler.** Every real handler ships with its twin binary. A handler does not land in main without its twin.

## Open Questions
1. How should the agent runner handle agents that need to interact with each other mid-task (e.g., a builder asking a reviewer a clarifying question)? With agent-mail dropped from the workspace plan, this needs a different answer -- possibly through the orchestrator (one agent's "I need help" output triggers a workflow transition that dispatches another agent).
2. What is the right granularity for health monitoring? Per-second process checks are expensive; per-minute checks may miss fast failures. Should monitoring frequency adapt based on the agent's current state?
3. How do we handle graceful shutdown of agent processes that are mid-task when a workflow is cancelled or a timeout fires?
4. Pi's session-log format and location -- needs concrete investigation before the Pi handler can support log capture for memory.

## Cross-References
- [S01: Orchestrator Core](orchestrator-core.md) -- dispatches agent work requests
- [S05: Hook System](hook-system.md) -- hooks attach to agent lifecycle events managed by the runner
- [S06: Workspace Manager](workspace-manager.md) -- provides isolated environments for agents
- [P02: Agent Persistence Gap](../problems/agent-persistence-gap.md) -- the runner keeps agents running autonomously
- [P05: Agent Behavior Enforcement](../problems/behavior-enforcement.md) -- hook injection enforces behavior
- [NTM component](../components/external/ntm.md) -- primary process management tool
