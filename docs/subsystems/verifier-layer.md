---
title: "S07: Verifier Layer (ARCHIVED -- superseded)"
status: archived
type: subsystem
solves: [P04, P07]
uses: [CI tools, linters, test suites, LLM]
related: [docs/subsystems/orchestrator-core.md, docs/subsystems/policy-engine.md, docs/subsystems/scenario-harness.md]
created: 2026-04-13
updated: 2026-04-19
superseded_by: [docs/subsystems/orchestrator-core.md, docs/subsystems/policy-engine.md]
---

# S07: Verifier Layer (ARCHIVED -- superseded)

> **Status: archived 2026-04-19.** The "verifier layer" framing was rejected in design discussion. Verification is not a distinct subsystem -- it is a *kind of node* in the workflow graph. A test run is a non-agentic node; a code review is an agentic node. The orchestrator (S01) executes whichever node type the graph specifies. The policy engine (S02) decides what happens with the result (advance, loop back, escalate).
>
> What lives elsewhere now:
>
> - **Running checks (tests, lints, builds)**: orchestrator dispatches non-agentic nodes that exec scripts and capture stdout/stderr/exit-code. Output capture is a generalized concern that the agent runner (S04) and orchestrator share -- *parked as an open detail; needs a "node types" doc later*.
> - **Pass/fail/loop-back decisions**: policy engine (S02). Transition guards consume node results and decide which edge fires.
> - **Structured feedback to agents**: workflow design pattern -- failed-check output becomes input context to the retry node. Not a subsystem responsibility.
> - **AI-based semantic review**: just an agentic node like any other (a reviewer agent in a review state).
> - **The S07 slot is now occupied by [Scenario Harness](scenario-harness.md)** -- the test infrastructure that runs full workflows against digital twin agent binaries.
>
> The original content below is preserved for historical reference per the methodology's "nothing is deleted" rule.

---

## Summary (original, archived)
The verifier layer validates that agent work meets defined criteria before state transitions advance. It runs automated verification suites -- tests, linters, type checks, structural validation, and optionally AI-based semantic review -- at quality gates throughout the workflow. Verification results determine whether work advances, loops back for revision, or escalates for human review.

## Purpose
Agents produce output. Some of that output is correct, some is subtly wrong, and some is confidently broken. Without automated verification, the only quality signal is human review -- which defeats the purpose of an autonomous system (P01). The verifier layer provides fast, automated quality signals so that the system can self-correct without human attention.

Verification also provides the feedback signal that agents need to improve (P07). When a verification suite fails, the structured failure output tells the agent exactly what went wrong: which test failed, which lint rule was violated, which type check did not pass. This is qualitatively better feedback than "try again" -- it gives the agent specific, actionable information.

## Key Responsibilities
- **Run verification suites.** Execute configured checks against agent output: unit tests, integration tests, linters, formatters, type checkers, build verification, and custom validation scripts.
- **Classify results.** Categorize verification outcomes as pass, fail, or partial. Partial results indicate that some checks passed and others failed -- the system needs to decide whether to advance or loop back.
- **Determine workflow action.** Based on verification results and policy, decide: advance to next state, loop back to the agent with feedback, escalate to human review, or fail the workflow.
- **Provide structured feedback.** When verification fails, produce structured output that agents can act on: specific failure messages, file locations, expected vs. actual values. Not just "tests failed" but "test X in file Y failed because Z."
- **AI-based semantic review.** For checks that cannot be automated deterministically (code quality assessment, architectural coherence, documentation completeness), delegate to an LLM reviewer. This is a ZFC-compliant cognition delegation: the verifier layer routes the question to the model, the model judges, the verifier layer acts on the judgment.

## Interfaces

**Inputs:**
- Verification requests from hook system (S05) or orchestrator core (S01) at quality gates
- Agent output (code, documents, artifacts) in the agent's workspace
- Verification suite definitions from workflow configuration
- Pass/fail thresholds from policy engine (S02)

**Outputs:**
- Verification results emitted to event bus (S03): pass/fail/partial, structured feedback
- Advance/loop-back/escalate decisions sent to orchestrator core (S01)
- Structured feedback injected into agent prompts for retry attempts

## Design Principles
- **Quality-left.** Run fast deterministic checks first (formatting, linting, type checking), expensive checks next (unit tests, integration tests), and AI-based semantic review last. Fail early on cheap signals rather than waiting for expensive ones.
- **Verification cascade.** Following Kilroy's model: format check, then build, then test, then artifact validation, then fidelity check. Each stage gates the next. This prevents wasting compute on later stages when early stages fail.
- **Sensors, not judges.** Following the harness engineering model: the verifier layer is a sensor array that produces measurements. The policy engine and orchestrator decide what to do with those measurements. The verifier reports; it does not decide.

## Candidate Implementations
- **Script-based verification.** Each verification step is a shell script that exits 0 (pass) or non-zero (fail) with structured output on stdout. Advantage: simple, language-agnostic, composable. Risk: limited structured output format.
- **CI pipeline integration.** Use existing CI tools (GitHub Actions, etc.) to run verification suites. Advantage: reuses existing infrastructure. Risk: latency -- CI pipelines are not designed for sub-minute feedback loops.
- **In-process verification.** Run checks within the harmonik process for maximum speed. Advantage: low latency. Risk: process isolation concerns, language-specific.

## Open Questions
1. How should the verifier layer handle flaky tests -- tests that pass and fail non-deterministically? Should it retry automatically, quarantine known-flaky tests, or report flakiness as a distinct result category?
2. What is the right balance between deterministic checks (fast, reliable, limited scope) and AI-based semantic review (slow, expensive, broader scope)? Should AI review be opt-in per workflow, or always-on?
3. How do we prevent verification suites from becoming a bottleneck when many agents are producing output simultaneously? Should verification be parallelized, or queued with priorities?

## Cross-References
- [S05: Hook System](hook-system.md) -- triggers verification at quality gates
- [S01: Orchestrator Core](orchestrator-core.md) -- receives advance/loop-back decisions from verification
- [S02: Policy Engine](policy-engine.md) -- provides pass/fail thresholds
- [S09: Improvement Loop](improvement-loop.md) -- analyzes verification results for system-wide patterns
- [P04: System Coherence at Scale](../problems/system-coherence.md) -- verification maintains coherence
- [P07: Feedback Loop Absence](../problems/feedback-loops.md) -- verification provides the feedback signal
- [Harness Engineering](../concepts/harness-engineering.md) -- sensors model for verification
