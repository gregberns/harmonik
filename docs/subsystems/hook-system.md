---
title: "S05: Hook System"
status: seed
type: subsystem
solves: [P05, P06]
uses: [Claude Code hooks, agent-runner, orchestrator-core]
related: [docs/concepts/zero-framework-cognition.md, docs/problems/behavior-enforcement.md, docs/subsystems/orchestrator-core.md, docs/subsystems/agent-runner.md]
created: 2026-04-13
updated: 2026-04-13
---

# S05: Hook System

## Summary
The hook system is the bridge between probabilistic agents and deterministic workflows. It injects deterministic behavior at agent lifecycle points: when an agent completes work, the hook system inspects the result, determines the appropriate state transition, and triggers it. Hooks are the glue that makes composed workflows work -- they connect "agent thinks it's done" to "system knows what happens next."

## Purpose
The core pattern is simple and powerful: an implementation agent finishes coding. A hook fires. The hook evaluates completion criteria -- possibly by asking an LLM "did this agent complete the task?" (a ZFC-compliant cognition delegation). If complete, the hook calls `trigger-review`, which causes the orchestrator to advance state and dispatch a review agent. The review agent finishes. Another hook fires. The cycle continues until the workflow reaches a terminal state.

Without hooks, workflow composition (P06) requires agents to understand the entire workflow and manually trigger the next step. That is both unreliable (P05 -- agents skip steps) and tightly coupled (every agent must know about every other agent). Hooks decouple agents from workflow structure: each agent focuses on its task, and the hook system handles what happens next.

## Key Responsibilities
- **Lifecycle event interception.** Attach hooks to defined moments in the agent lifecycle. Each hook fires at a specific point and can inspect agent state, output, and context.
- **Completion evaluation.** When an agent signals completion, the hook evaluates whether the task is actually done. This may be a deterministic check (tests pass, file exists) or a delegated cognitive check (LLM evaluates output quality).
- **State transition triggering.** Successful hook evaluation triggers the appropriate state transition in the orchestrator core. The hook translates agent-level events into workflow-level transitions.
- **Error handling and escalation.** When hooks detect failures, they route to the appropriate recovery path: retry the agent, dispatch a different agent, or escalate to a human.

## Hook Types

| Hook | Fires When | Typical Action |
|------|-----------|----------------|
| `on_agent_started` | Agent process launches | Log start, verify workspace ready |
| `on_agent_output` | Agent produces intermediate output | Stream to event bus, check for anomalies |
| `on_agent_completed` | Agent signals task completion | Evaluate completion, trigger next state |
| `on_timeout` | Agent exceeds time budget | Save progress, escalate or retry |
| `on_review_required` | Work reaches a review gate | Dispatch review agent, notify humans |
| `on_error` | Agent process fails | Classify error, retry or escalate |
| `on_state_transition` | Orchestrator advances state | Inject context for next agent, update memory |

## Interfaces

**Inputs:**
- Agent lifecycle events from agent runner (S04)
- Completion criteria from workflow definitions
- Policy constraints from policy engine (S02)

**Outputs:**
- State transition triggers sent to orchestrator core (S01)
- Agent dispatch requests (indirect, via orchestrator)
- Hook execution events emitted to event bus (S03)
- Escalation requests to humans or governance agents

## Design Principles
- **Hooks are mechanism, completion evaluation is cognition.** The hook firing is deterministic (it always fires at the defined lifecycle point). But evaluating whether work is "done" may require semantic judgment. That judgment is delegated to an LLM -- the hook calls the model, the model decides, the hook acts on the decision. This is ZFC-compliant: the framework routes, the model judges.
- **Hooks are composable.** Multiple hooks can attach to the same lifecycle point. They execute in defined order. A pre-commit hook runs tests; a post-commit hook triggers review. The composition of hooks defines the workflow's behavioral guarantees.
- **Hooks are the user's key idea.** "Once implementation is completed by an implementation agent, a hook fires: 'Did you complete the task? If so, call trigger-review.' That causes another agent to fire off and start a review." This pattern -- hook-driven workflow advancement -- is the fundamental mechanism that makes harmonik's composed workflows work.

## Candidate Implementations
- **Claude Code hooks.** Use Claude Code's native hook system (`.claude/hooks.json`) for agents running in Claude Code. Advantage: native integration, no custom infrastructure. Limitation: only works for Claude Code agents.
- **Custom hook framework.** Build a hook registry and execution engine that works across agent types. Advantage: universal. Risk: duplicating Claude Code's hook system.
- **Hybrid.** Claude Code hooks for Claude Code agents, custom hooks for other agent types, with a unified event model that normalizes hook outputs.

## Open Questions
1. How should hooks handle ambiguous completion signals -- when the LLM evaluator is uncertain whether work is done? Should there be a confidence threshold, or should ambiguity always escalate to a human?
2. What is the right execution model for hooks -- synchronous (block until complete) or asynchronous (fire and continue)? Synchronous is safer but slower; asynchronous risks race conditions.
3. How do we test hooks in isolation? A hook's behavior depends on agent output, workflow state, and policy constraints -- how do we create realistic test fixtures for this?
