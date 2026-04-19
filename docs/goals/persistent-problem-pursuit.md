---
title: Persistent Problem Pursuit
status: explored
type: goal
solves: [P01, P02]
sources: [refs/AlphaGo-modeled-orch-system.md, symphony, ntm]
related: [docs/problems/human-attention-scarcity.md, docs/problems/agent-persistence-gap.md]
created: 2026-04-13
updated: 2026-04-13
---

# G02: Persistent Problem Pursuit

## Summary

Build systems that continually push on problems without a human constantly restating the objective. Describe a problem once, and the system works on it until it is solved -- with appropriate human gates at key decision points but no requirement for humans to keep the engine running.

## The Problem Today

Current agent systems stop when humans stop pushing. A developer describes a bug, an agent investigates, the session ends, and the next session starts from scratch. The human becomes the persistence layer, re-explaining context, re-establishing goals, and re-motivating progress. This wastes the scarcest resource we have: human attention.

## What Persistence Requires

**Daemon-style execution:** Symphony's model runs continuously -- monitoring for work, picking up tasks, executing them, and checking for more. The system does not wait to be told to start. It operates on a loop: check state, find eligible work, execute, record outcome, repeat.

**Auto-respawn and health monitoring:** NTM's process management ensures agents survive crashes, restarts, and interruptions. If an agent dies mid-task, the system detects it, restores state from the last checkpoint, and resumes. This is infrastructure-level persistence, not prompt-level persistence.

**State management:** Every task has durable state that survives session boundaries. Current progress, partial results, failed approaches, and remaining work items are all persisted. An agent picking up a task can reconstruct context without the human re-explaining anything.

**Progress tracking:** The system must know where it is. A task tracker records what has been attempted, what succeeded, what failed, and what remains. This is not just logging -- it is queryable state that drives scheduling decisions.

**Human gates, not human drivers:** Humans approve specs, review code, and sign off on deployments. But humans do not decide when to start the next task, remind the system what it was doing, or re-establish context after interruptions. The system drives itself; humans steer at checkpoints.

## How This Looks in Practice

1. A human describes a problem: "Our API response times degrade under load."
2. The system creates a persistent task, decomposes it into investigation steps, and begins.
3. Agents profile the system, identify bottlenecks, propose fixes, and run benchmarks.
4. When a fix is ready, the system requests human review.
5. If the human rejects it, the system incorporates feedback and continues.
6. If the system gets stuck, it escalates with a specific question rather than stalling silently.
7. Between human interactions, work continues autonomously.

## Measures of Success

- A task described on Monday is still being actively worked on Friday without any human re-prompting.
- After a system restart, in-progress work resumes within minutes without human intervention.
- Human time per task is limited to review gates: approving specs, reviewing code, answering escalated questions.
- The system can manage multiple concurrent persistent tasks, scheduling and prioritizing without human direction.
- Progress is always visible: a human can check on any task and see exactly where it stands.

## Open Questions

- What is the right escalation policy? When should the system ask for help versus trying another approach?
- How do we prevent runaway compute on tasks that are genuinely unsolvable or poorly defined?
- How do we handle priority changes -- a human decides task B is now more important than task A mid-execution?
- What does "done" look like for open-ended improvement tasks versus discrete bug fixes?
- How much context can realistically be reconstructed from persisted state versus requiring fresh human input?

## Cross-References

- [P01: Human Attention Scarcity](../problems/human-attention-scarcity.md) -- the core constraint this addresses
- [P02: Agent Persistence Gap](../problems/agent-persistence-gap.md) -- the specific failure mode we are solving
- [G04: Learning and Improvement Loops](learning-and-improvement-loops.md) -- persistence enables learning across attempts
- [G05: Idea-to-Implementation Pipeline](idea-to-implementation-pipeline.md) -- the pipeline that persistent pursuit drives
