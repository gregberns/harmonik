---
title: Idea-to-Implementation Pipeline
status: explored
type: goal
solves: [P04, P06]
sources: [refs/AlphaGo-modeled-orch-system.md, kerf, kilroy, symphony]
related: [docs/problems/system-coherence.md, docs/problems/workflow-composition.md]
created: 2026-04-13
updated: 2026-04-13
---

# G05: Idea-to-Implementation Pipeline

## Summary

Build a system that takes a goal and uses defined structures to bring a task from initial idea through to working, verified implementation. This is the end-to-end workflow that all other goals support. It should be concrete, auditable, and repeatable.

## The Full Lifecycle

The pipeline moves work through defined phases, each with clear entry criteria, responsible roles, and exit gates:

**1. Problem Space (idea_backlog):** A goal or problem is described in natural language. The planner agent assesses scope, identifies dependencies, and determines if the idea is ready for decomposition or needs research first.

**2. Decomposition (spec_drafting):** The idea is broken into concrete, implementable pieces. Kerf's spec-first workflow applies: problem-space analysis, decomposition into sub-problems, targeted research on unknowns, and structured spec generation. Each spec defines acceptance criteria, not just requirements.

**3. Review (spec_review):** A reviewer agent (or human) evaluates the spec for completeness, feasibility, and alignment with system architecture. Rejected specs return to drafting with specific critique. This gate prevents wasted implementation effort.

**4. Scheduling (implementation_ready):** Approved specs enter a queue. The scheduler assigns work based on priority, dependencies, and available agent capacity. Work items include everything an agent needs to begin: the spec, relevant context, tool access, and verification criteria.

**5. Implementation (coding):** A builder agent implements against the spec. It has autonomy in approach but is scoped to its assigned work. It runs local checks as it works. When satisfied, it submits for review.

**6. Code Review (code_review):** A reviewer evaluates the implementation against the spec. Does it meet acceptance criteria? Does it follow architectural conventions? Is it coherent with adjacent code? Rejections include specific feedback and return to the coding state.

**7. Verification (verification):** Automated verifiers run the full check suite: tests, linting, type checking, architecture conformance, and any custom quality gates. This is objective, deterministic evaluation with no subjective judgment.

**8. Completion (done):** Verified work is merged, documented, and archived. A retrospective trace is generated capturing what worked, what was revised, and how long each phase took.

## What Makes This Different

**Spec-first, not code-first.** Kerf's approach ensures implementation only begins after the problem is well-defined. This prevents the common failure mode where agents start coding before understanding the actual requirement.

**Review gates are mandatory, not optional.** Kilroy's model enforces that work cannot advance without explicit approval at gate points. This is structural, enforced by the state machine, not advisory.

**Tracker-to-PR automation.** Symphony's pattern connects the task tracker directly to implementation artifacts. A task in the tracker maps to a branch, a PR, and a verification run. There is no gap between "task is done" and "code is merged."

**End-to-end traceability.** Every artifact can be traced back to the spec that required it, the goal that motivated it, and the problem it addresses. This traceability is maintained by structured metadata, not manual bookkeeping.

## Measures of Success

- A new goal can enter the pipeline and reach verified implementation with zero human re-prompting beyond review gates.
- Every merged artifact traces back to an approved spec, which traces back to a defined goal.
- Rejected work returns to the correct prior phase with actionable feedback, not vague objections.
- Pipeline throughput is measurable: average time per phase, rejection rates, rework frequency.
- The pipeline handles both small tasks (single-file changes) and large initiatives (multi-agent, multi-day efforts) using the same structure.

## Open Questions

- How do we handle tasks that reveal spec deficiencies during implementation? What is the fast path back to spec revision?
- What is the right granularity for specs? Too coarse and builders lack direction; too fine and specs become pseudocode.
- How do we parallelize work items that have dependencies on each other?
- What happens when verification passes but human review reveals a subtle design problem? How does that feedback loop work?
- How do we measure pipeline health beyond throughput -- quality of specs, accuracy of estimates, appropriateness of decomposition?

## Cross-References

- [P04: System Coherence at Scale](../problems/system-coherence.md) -- the pipeline maintains coherence through structured phases
- [P06: Workflow Composition Complexity](../problems/workflow-composition.md) -- each phase is a composable workflow unit
- [G01: Structured Emergent Systems](structured-emergent-systems.md) -- the pipeline is the primary workflow built on the deterministic skeleton
- [G03: Independent Process-Following Actors](independent-process-following-actors.md) -- reliable actors are what make each phase trustworthy
