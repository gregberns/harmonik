---
title: "P02: Agent Persistence Gap"
status: explored
type: problem
sources: [docs/00_objective.md, refs/AlphaGo-modeled-orch-system.md]
related: [docs/problems/human-attention-scarcity.md, docs/problems/knowledge-loss.md]
created: 2026-04-13
updated: 2026-04-13
---

# P02: Agent Persistence Gap

## Summary
Agents work when told to and stop when the session ends. There is no built-in mechanism for an agent to persist on a problem across sessions, track its own progress, or resume where it left off. Humans fill this gap manually, carrying the state of every open problem in their heads and re-initiating work each time.

## The Problem

Current agent sessions are transactional: a human starts a session, the agent works, the session ends. If the problem is not fully solved within that session, the burden of continuity falls entirely on the human. This means:

- **No autonomous follow-through.** If a task requires multiple sessions -- because the context window fills, the agent hits a blocker, or the work is simply large -- there is no mechanism for the agent to "come back to it." The human must remember, re-frame, and re-launch.
- **No progress tracking.** The agent does not know what it accomplished last time, what approaches it tried, or what failed. Each session starts from zero unless the human manually provides a summary.
- **No idle-time utilization.** When a human is asleep, in meetings, or working on something else, agents sit idle. There is no scheduler that keeps work moving during these gaps.

The AlphaGo reference architecture describes a system where search, evaluation, and learning run continuously. The coding-agent analogue should similarly be able to sustain work across time, not just within a single interactive window.

## Why It Matters

Complex software problems -- architectural refactors, multi-step migrations, large feature implementations -- rarely fit in a single session. The persistence gap means these high-value problems get fragmented into disconnected sessions with human-supplied glue between them. The glue itself is expensive (see P01) and lossy (see P03).

What we want is closer to assigning a task to a junior engineer: you describe the goal, set review checkpoints, and expect them to keep working on it -- filing updates, asking questions at defined gates, and ultimately delivering a result. The agent should own continuity; the human should own direction and approval.

## Concrete Manifestations

- A multi-file refactor requires three sessions. Each time, the engineer spends 15 minutes explaining what was done and what remains. The agent re-discovers constraints it already found in session one.
- An agent identifies a promising approach but runs out of context. The human does not resume the work for two days. By then, the approach details are lost and must be reconstructed.
- A scheduled nightly test run fails. No agent picks it up until a human notices the failure the next morning and manually launches an investigation.

## Open Questions

1. What is the right persistence primitive -- checkpointed agent state, a durable task queue with structured handoff, or something else?
2. How do we balance persistence with human control? An agent that "keeps working" without gates is dangerous. What is the minimal approval structure that preserves autonomy without losing safety?
3. Can persistence be layered on top of existing tools (tmux sessions, cron, message queues) or does it require a purpose-built runtime?

## Cross-References
- [P01: Human Attention Scarcity](human-attention-scarcity.md) -- Persistence directly reduces attention cost
- [P03: Knowledge Loss Across Sessions](knowledge-loss.md) -- Persistence without memory is just repeated cold starts
- [refs/AlphaGo-modeled-orch-system.md](../../refs/AlphaGo-modeled-orch-system.md) -- Continuous loop architecture
