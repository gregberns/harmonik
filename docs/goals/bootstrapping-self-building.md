---
title: Bootstrapping and Self-Building
status: seed
type: goal
solves: [P01, P02, P07]
sources: []
related: [docs/bootstrap.md, docs/goals/learning-and-improvement-loops.md, docs/subsystems/improvement-loop.md]
created: 2026-04-19
updated: 2026-04-19
---

# G06: Bootstrapping and Self-Building

## Summary

Harmonik must bootstrap itself: a small, hand-written core is built first, then the system uses itself to build the rest of itself. The end state is a self-improving codebase where harmonik authors and refactors harmonik. Three control points keep the loop safe: stop on major issue, pause to run an improvement cycle, pause to upgrade to a newer version of harmonik mid-build.

## The Tension

Building harmonik manually -- subsystem by subsystem, hand-written by humans and assistants -- is slow and contradicts the system's own thesis (P01: human attention is scarce). But running a self-building loop on day zero requires the system to already exist. The bootstrapping question is: *what is the minimum viable harmonik that can extend itself?*

The goal forces explicit answers to:
- What capabilities must exist before self-build can start?
- How does the system safely modify itself without bricking the loop?
- How does an in-flight build adopt a newer version of the harmonik core that produced it?

## Bootstrap Then Self-Build

Two distinct phases:

**Phase 1: Bootstrap.** Hand-written code (humans + assistants in conventional sessions) until the minimum viable harmonik exists. Minimum viable means: enough orchestrator, agent runner, hook system, and workspace manager that a workflow can run agents end-to-end against the harmonik repository itself.

**Phase 2: Self-build.** Harmonik runs against its own repository. Workflows take subsystem specs as input and produce implementation. Humans review at gates. Each completed cycle increases the system's ability to drive its next cycle.

The transition point matters. Going self-build too early produces brittle output that costs more to repair than to write by hand. Going self-build too late wastes the leverage the system was built to provide.

## Three Control Mechanisms

The self-build loop must support three operator interventions. These are *general harmonik features* (any project running harmonik gets the same controls) -- self-build is one important consumer of them. They operate at task-boundary granularity: pause means "finish in-flight tasks, stop pulling new ones," not "interrupt mid-task."

| Control | Purpose | Trigger |
|---|---|---|
| **Stop on major issue** | Halt the orchestrator when a critical failure is detected (build broken, security regression, infinite loop, runaway cost). Default graceful (let in-flight finish); immediate-abort variant available for severe issues. | Verifier signal, policy violation, manual operator command |
| **Pause for improvement cycle (between tasks)** | After every N completed tasks, pause the queue. Improvement loop (S09) analyzes recent task logs for excessive retries, poor-performing prompts, slow nodes. Approved changes are applied. Resume the queue under the updated configuration. | Configured cadence (every N tasks), manual operator command, or improvement-loop-initiated |
| **Pause to upgrade harmonik version (between tasks)** | At a task boundary, swap the running harmonik binary for a newer version (e.g., one produced by a recent self-build cycle). Subsequent tasks run on the new core. | Operator command, configured policy ("upgrade at task-N boundary"), or improvement-loop-recommended |

These are first-class features of the orchestrator and policy engine, not add-ons. Without them harmonik is unsafe to run unattended -- and self-build in particular is impossible.

See [docs/bootstrap.md §4](../bootstrap.md) for the detailed specification of each control.

## Measures of Success

- The bootstrap phase produces a documented "minimum viable harmonik" deliverable -- a known-good baseline that future self-build cycles can reset to.
- A self-build cycle can complete a subsystem implementation from a spec without human intervention except at policy-defined review gates.
- The three control mechanisms can be exercised at any point during a self-build cycle without losing in-flight work.
- A version upgrade mid-build preserves the work-in-progress and resumes against the new core without manual reconciliation.
- The system can be left running unattended for the duration of a single subsystem's implementation cycle.

## Open Questions

- What is the precise definition of "minimum viable harmonik"? Which subsystems are required, and at what fidelity, before self-build can start?
- How is the bootstrap phase itself organized? Is it driven by harmonik's knowledge base (this repo), executed by humans + assistants in conventional sessions, with the same review/gate discipline that self-build will use?
- How do we prevent self-build cycles from producing changes that break the self-build capability itself? Is there a regression suite for "harmonik can still build harmonik"?
- What is the rollback story when a self-build cycle produces something worse than the prior version? How do we detect "worse" without human review on every change?
- Should the three control mechanisms be exposed via the same interface (operator CLI, dashboard, API) or differentiated by risk profile?

## Cross-References

- [docs/bootstrap.md](../bootstrap.md) -- Detailed thinking-through document for this goal
- [G04: Learning and Improvement Loops](learning-and-improvement-loops.md) -- The improvement loop is the engine that makes self-build sustainable
- [G05: Idea-to-Implementation Pipeline](idea-to-implementation-pipeline.md) -- Self-build is the pipeline applied to harmonik itself
- [S09: Improvement Loop](../subsystems/improvement-loop.md) -- Drives the "pause for improvement cycle" control
- [P01: Human Attention Scarcity](../problems/human-attention-scarcity.md) -- The scarcity that makes self-build worth pursuing
- [P02: Agent Persistence Gap](../problems/agent-persistence-gap.md) -- Self-build requires the persistence problem to be solved
