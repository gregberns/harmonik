---
title: Learning and Improvement Loops
status: explored
type: goal
solves: [P03, P07]
sources: [refs/AlphaGo-modeled-orch-system.md, cass-memory, ace-pipeline]
related: [docs/problems/knowledge-loss.md, docs/problems/feedback-loops.md]
created: 2026-04-13
updated: 2026-04-13
---

# G04: Learning and Improvement Loops

## Summary

Build components that enable the system to learn from its own execution and improve over time. This requires two things: upstream components that log enough structured information to make learning possible, and downstream components that analyze that information and feed improvements back into the system.

## The Learning Stack

**Layer 1 -- Structured logging at transitions.** Every state transition must produce a structured record containing: prior state, actor role, candidate actions considered, chosen action, policy version used, evidence consulted, outcome observed, and verifier metrics. This is the raw material for all learning. Without it, the system is flying blind.

**Layer 2 -- Episodic memory.** Individual execution traces stored as searchable episodes. CASS memory's three-layer cognitive architecture provides the model: working memory (current task context), episodic memory (past execution traces), and semantic memory (distilled knowledge). Session search allows agents to query past episodes: "How did we handle a similar API migration last month?"

**Layer 3 -- Procedural knowledge extraction.** The ACE pipeline pattern: raw execution data is analyzed to extract reusable procedures. If the system successfully resolved a class of problem three times using a similar approach, that approach should be codified as a procedure rather than rediscovered each time.

## The Improvement Meta-Loop

The system improves through a four-phase cycle:

1. **Execution** -- Agents perform work, generating structured traces.
2. **Analysis** -- A retrospective agent reviews traces, identifies patterns: which approaches worked, which failed, where time was wasted, which estimates were wrong.
3. **Policy proposal** -- Based on analysis, the system proposes concrete changes: updated transition rules, revised agent instructions, new verification checks, adjusted scheduling heuristics.
4. **Governance** -- Proposed changes go through a review process (human or automated, depending on risk level) before being applied to the live policy.

This mirrors the AlphaGo loop: execution generates data, analysis extracts signal, and distillation compresses that signal back into the policy that guides future execution.

## What Gets Logged

Every decision point should produce a record with these fields:

- `state`: The system state when the decision was made.
- `actor_role`: Which role made the decision.
- `candidates`: What actions were considered.
- `chosen_action`: What was actually done.
- `policy_version`: Which version of the rules was in effect.
- `evidence`: What information the actor consulted.
- `outcome`: What happened as a result.
- `verifier_metrics`: Objective quality signals (tests passed, lint score, type coverage).
- `duration`: How long the action took.
- `cost`: Token and compute cost.

## Measures of Success

- The system demonstrably makes fewer mistakes on problem types it has encountered before.
- Agents can query past episodes and find relevant precedents without human curation.
- Policy changes can be traced back to specific execution data that motivated them.
- The ratio of novel problems to repeated problems shifts over time as procedural knowledge accumulates.
- Time-to-completion for recurring task types decreases measurably across iterations.

## Open Questions

- How do we prevent the learning loop from overfitting to recent experience and losing generality?
- What is the governance threshold? Which policy changes can be auto-applied versus requiring human review?
- How do we handle contradictory lessons -- two episodes suggest opposite approaches?
- What is the right retention policy for episodic memory? How long do traces stay searchable before archiving?
- How do we measure learning quality independently of task completion quality?

## Cross-References

- [P03: Knowledge Loss Across Sessions](../problems/knowledge-loss.md) -- the core problem this addresses
- [P07: Feedback Loop Absence](../problems/feedback-loops.md) -- the systemic gap this fills
- [G02: Persistent Problem Pursuit](persistent-problem-pursuit.md) -- persistence generates the data that learning requires
- [G05: Idea-to-Implementation Pipeline](idea-to-implementation-pipeline.md) -- the pipeline that improves through learning
