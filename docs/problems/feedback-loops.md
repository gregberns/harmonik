---
title: "P07: Feedback Loop Absence"
status: explored
type: problem
sources: [refs/AlphaGo-modeled-orch-system.md]
related: [docs/problems/knowledge-loss.md, docs/problems/workflow-composition.md]
created: 2026-04-13
updated: 2026-04-13
---

# P07: Feedback Loop Absence

## Summary
Current agent systems are open-loop: they execute tasks but do not learn from the results. There is no mechanism for a system to observe its own execution, analyze what worked and what did not, and improve its processes for next time. Without closed feedback loops, systems cannot get better -- they just repeat.

## The Problem

An open-loop system executes a plan and delivers a result. A closed-loop system executes, observes the outcome, compares it against expectations, and adjusts its approach. Nearly all current agent workflows are open-loop:

- **No execution observation.** When an agent completes a task, the result is delivered but the execution trace is discarded. How long did it take? How many retries were needed? Which approaches failed before one succeeded? This data exists transiently in the context window and then vanishes.
- **No outcome analysis.** Even when results are visible (tests pass, deployment succeeds), nobody asks the structural question: was this the best path to the result? Could the workflow have been shorter, cheaper, or more reliable? There is no analysis step.
- **No process improvement.** Without observation and analysis, there is no input for improvement. The system runs the same workflow with the same parameters regardless of whether the last ten executions revealed a consistent bottleneck or failure pattern.

The AlphaGo reference document describes a multi-layered loop architecture: an execution loop that does work, an analysis loop that evaluates outcomes, a policy proposal loop that suggests improvements, and a governance loop that approves changes. This is the closed-loop structure that current agent systems lack.

## Why It Matters

The difference between a junior engineer and a senior one is not raw capability -- it is accumulated feedback. The senior engineer has seen what works, what fails, and why. They have internalized patterns from hundreds of past projects. Current agent systems are permanently junior: they never accumulate this feedback because their execution data is not captured, analyzed, or applied.

The CASS memory system's ACE pipeline (Generate, Reflect, Validate, Curate) provides a concrete pattern for one type of feedback loop: turning raw session observations into validated, reusable knowledge. But feedback loops extend beyond memory:

- **Workflow optimization.** This workflow consistently takes three review cycles. Why? Can the spec be improved to reduce rejections?
- **Agent selection.** Agent A solves this class of problem in half the time as Agent B. Route accordingly.
- **Policy refinement.** The state machine transition from "implementation" to "review" should require a test report. We learned this because three reviews were wasted on untested code.

## The Loop Structure

A complete feedback loop has four phases:

1. **Execute.** Run the workflow and produce a result.
2. **Observe.** Capture structured data about the execution: duration, retries, errors, resource usage, outcome quality.
3. **Analyze.** Compare observations against baselines and expectations. Identify patterns across multiple executions.
4. **Improve.** Propose and apply changes to workflows, policies, agent configurations, or resource allocation.

Each phase feeds the next, and the cycle repeats. The system improves with every iteration, converging on better processes the way AlphaGo's self-play converged on better strategies.

## Concrete Manifestations

- A deployment workflow fails 30% of the time due to a race condition in the test suite. Each failure is treated as a novel event. No agent identifies the pattern or proposes a fix to the workflow itself.
- A review agent consistently requests the same class of changes (missing error handling). The implementation agent never learns to include error handling proactively because there is no feedback path from review outcomes to implementation prompts.
- A project completes after 200 agent sessions. The team has no data on which sessions were efficient, which were wasteful, or what the overall cost-per-feature was.

## Open Questions

1. What is the minimum viable feedback loop? Can we start with just execution observation and manual analysis, then automate analysis and improvement incrementally?
2. How do we prevent feedback loops from introducing instability -- where a process change based on recent data makes things worse for a different class of tasks?
3. Who governs the feedback loop? Should process improvements be auto-applied, proposed for human review, or gated behind a "governance agent" that evaluates proposed changes?

## Cross-References
- [P03: Knowledge Loss Across Sessions](knowledge-loss.md) -- Memory is a prerequisite for feedback; feedback is what makes memory useful
- [P06: Workflow Composition Complexity](workflow-composition.md) -- Composed workflows are the richest source of feedback data
- [refs/AlphaGo-modeled-orch-system.md](../../refs/AlphaGo-modeled-orch-system.md) -- Multi-loop architecture and self-play as feedback
