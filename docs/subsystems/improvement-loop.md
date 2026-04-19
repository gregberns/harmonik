---
title: "S09: Improvement Loop"
status: seed
type: subsystem
solves: [P07, P03]
uses: [memory-layer, event-bus]
related: [docs/concepts/alphago-system.md, docs/concepts/harness-engineering.md, docs/subsystems/memory-layer.md, docs/subsystems/event-bus.md]
created: 2026-04-13
updated: 2026-04-13
---

# S09: Improvement Loop

## Summary
The improvement loop is the meta-process that makes harmonik self-improving. It analyzes system execution data, identifies patterns and bottlenecks, proposes improvements to workflows and prompts, validates those improvements, and applies them with governance controls. It operates on a slower timescale than workflow execution -- it watches the system run, learns from what it observes, and gradually tunes the system to perform better.

## Purpose
A system that executes workflows but never improves its own execution is leaving value on the table. Every workflow run generates data: which steps took longest, which agents failed and why, which verification checks caught real problems vs. produced false positives, which prompts led to better outcomes. The improvement loop mines this data for actionable insights and feeds them back into the system.

Without the improvement loop, harmonik can only improve through manual human intervention -- someone notices a pattern, writes a better prompt, adjusts a workflow. With the improvement loop, the system proposes its own improvements, tests them, and applies the ones that work. This closes the feedback gap (P07) and ensures that knowledge is not lost between sessions (P03).

## Key Responsibilities
- **Execution analysis.** Consume the event stream from the event bus. Build execution profiles: time-per-state, failure rates, retry counts, agent utilization, verification pass rates.
- **Pattern detection.** Identify recurring patterns: agents that consistently fail at specific steps, verification checks that always pass (and may be unnecessary), workflows that bottleneck at specific nodes.
- **Improvement proposal.** Generate concrete improvement proposals: modified prompts, adjusted workflow structures, tuned policy parameters, new verification checks, updated procedural memory entries.
- **Validation.** Test proposed improvements before deploying them. Run A/B comparisons where possible. Measure whether the improvement actually improves the target metric.
- **Governance.** Route improvement proposals through appropriate review. Low-risk changes (prompt tweaks, parameter tuning) may be auto-applied. High-risk changes (workflow structure, policy modifications, merge rules) require human review.

## Four Nested Loops

The improvement loop operates at four timescales, each feeding the next:

| Loop | Timescale | Activity | Output |
|------|-----------|----------|--------|
| Execution | Minutes | Run workflows, produce events | Event stream, verification results |
| Analysis | Hours | Examine logs, find patterns | Execution profiles, bottleneck reports |
| Proposal | Days | Suggest workflow/prompt/parameter changes | Improvement proposals with expected impact |
| Governance | Days-Weeks | Review and approve changes | Approved changes applied to system config |

## Interfaces

**Inputs:**
- All events from event bus (S03) -- the primary data source
- Verification results from verifier layer (S07)
- Agent session logs from memory layer (S08)
- Execution metrics: completion times, error rates, retry counts, token consumption

**Outputs:**
- Improvement proposals (versioned, reviewable documents)
- Approved changes applied to: workflow definitions, agent prompts, policy configurations, verification suites
- Analysis reports emitted to event bus (S03)
- New procedural memory entries sent to memory layer (S08)

## Design Principles
- **Self-improvement boundaries.** Not all changes are equally safe. Prompt improvements and task decomposition changes are low-risk and can be proposed freely. Workflow structure changes require more scrutiny. Governance and role policy changes require the strictest review. The improvement loop enforces these boundaries on itself.
- **Verifiable improvements.** Every proposed change includes a hypothesis ("this prompt change will reduce retry rates for code review by 20%") and a measurement plan. Changes that cannot be measured are not applied. This prevents well-intentioned changes that actually degrade performance.
- **Slow timescale.** The improvement loop operates deliberately. It does not react to individual failures -- it identifies patterns over many executions. A single agent failure is noise; ten failures at the same step is a signal.

## Candidate Implementations
- **Event stream analysis + LLM proposal.** Periodically run an analysis agent over the event stream. The agent identifies patterns and drafts improvement proposals. A governance agent (or human) reviews and approves. Advantage: leverages LLM capability for pattern recognition. Risk: LLM may propose changes that sound good but degrade performance.
- **Statistical analysis + rule-based proposal.** Use deterministic statistical analysis (mean completion time, failure rate trends) to identify problems. Generate proposals from a template library. Advantage: fully auditable, no LLM uncertainty. Risk: limited to patterns the templates can express.
- **Hybrid.** Statistical analysis for detection, LLM for proposal generation, deterministic validation for acceptance. Each stage has appropriate controls.

## Open Questions
1. How do we prevent improvement drift -- where a sequence of individually reasonable changes gradually moves the system to a worse overall state? Should there be periodic "reset to baseline" checkpoints?
2. What metrics should the improvement loop optimize for? Raw speed, error rate, human intervention frequency, token cost, or some weighted combination? Who sets those weights?
3. How do we handle the cold start problem -- the improvement loop needs execution data to make proposals, but a new system has no execution data? Should there be a bootstrap phase with synthetic data or manual seeding?
