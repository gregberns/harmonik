---
title: Symphony
status: explored
type: concept
source: https://github.com/openai/symphony
related: [kilroy.md, harness-engineering.md, gas-town-hooks.md]
created: 2026-04-13
updated: 2026-04-13
---

# Symphony

## What It Is
Symphony is OpenAI's daemon-based agent orchestration system. It runs as a persistent service that monitors a tracker (issue board), dispatches agents to work items, and manages retries and lifecycle. It is not a CLI tool -- it is a long-running scheduler.

## Key Concepts

### Daemon-as-Orchestrator
A continuously running process that polls a tracker, claims unclaimed work items, dispatches agents, and handles completion/failure. The daemon is the heartbeat of the system -- always running, always watching.

### Prompt-as-Policy
The WORKFLOW.md file (YAML frontmatter + Markdown body) is the central control mechanism. It defines agent behavior, retry policy, concurrency limits, and workflow rules. Version-controlled and hot-reloadable -- change the file, change the behavior.

### Multi-Turn Execution
Each dispatch runs multiple agent turns in a loop, re-checking tracker state between turns. Agents are not one-shot -- they run until their work item reaches a terminal state or they exhaust their turn budget.

### State Machine
Work items flow through: Unclaimed, Claimed, Running, RetryQueued, Released. Claim-before-dispatch prevents duplicate work. State transitions are atomic and tracked.

### Continuation vs Retry
Clean exits get a 1-second continuation check (did the state actually advance?). Failures get exponential backoff. Different semantics for different exit types -- this distinction matters for agent reliability.

### Skills as Markdown
Reusable procedural knowledge encoded as instruction documents that agents follow. Skills are extensible without code changes -- add a new markdown file, agents gain a new capability.

### Implicit Coordination
Agents never communicate directly with each other. All coordination happens through shared tracker state. Agent A changes a ticket status; Agent B sees the change and acts. Simple, decoupled, effective.

### Per-State Concurrency Limits
Fine-grained control over parallelism: e.g., 8 agents can work "In Progress" items simultaneously, but only 2 can be "Merging" at once. This prevents bottleneck states from being overwhelmed.

### Dynamic Tool Injection
The orchestrator extends agent capabilities at runtime by registering tools. Agents don't have fixed toolsets -- the daemon can add or remove tools based on context, state, or policy.

### Workspace Lifecycle Hooks
Shell scripts execute at create/run/remove stages for arbitrary integration. Hooks provide the extension points for environment setup, cleanup, and external system coordination.

## Relevance to Harmonik

Symphony provides the **daemon/scheduler model** and the **prompt-as-policy** pattern. Its contributions:

- **Persistent orchestration**: The daemon model is the right shape for harmonik's coordinator. Always-on, always-watching, driving work forward.
- **Tracker-driven coordination**: Using a shared state store (not direct agent messaging) for coordination is simple and robust. Harmonik's bead system maps well to this.
- **Lifecycle management**: The claim/dispatch/retry lifecycle is production-tested and handles real failure modes.
- **Prompt-as-policy**: Version-controlled policy documents that define agent behavior without code changes. This aligns with harmonik's goal of composable configuration.
- **Continuation vs retry distinction**: Harmonik should preserve this semantic difference. A clean completion that didn't advance state is fundamentally different from a crash, and the response strategy should differ accordingly.
- **Implicit coordination**: The pattern of agents coordinating through shared state rather than direct communication reduces coupling and simplifies the system. Harmonik's bead state serves this role.

The main limitation: Symphony is tightly coupled to a linear issue-tracker model. Harmonik needs richer workflow shapes (DAGs, cycles, parallel branches with join semantics). Symphony also lacks the deterministic workflow verification that Kilroy and the AlphaGo model provide. However, its simplicity is a virtue -- the daemon/tracker pattern is easy to reason about and debug.
