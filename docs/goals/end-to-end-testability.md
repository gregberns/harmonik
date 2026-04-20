---
title: End-to-End Testability Without Real Agents
status: seed
type: goal
solves: [P04, P05, P07]
sources: []
related: [docs/concepts/digital-twins.md, docs/subsystems/scenario-harness.md, docs/subsystems/agent-runner.md]
created: 2026-04-19
updated: 2026-04-19
---

# G07: End-to-End Testability Without Real Agents

## Summary

The harmonik system must be testable end-to-end -- full workflows, all subsystems, edge cases -- without invoking real agent processes or spending tokens. Each agent runner type has a digital twin: a separate binary, invoked identically to the real agent, that simulates lifecycle and output without making model calls. Scenario tests drive workflows against twins to verify behavior cheaply, deterministically, and exhaustively.

## The Tension

A system that orchestrates probabilistic agents is itself difficult to test. If every test requires a real model call, tests are slow, expensive, non-deterministic, and inevitably under-covered -- nobody runs the full suite often enough to catch regressions. Yet the orchestration logic, hooks, policy decisions, workspace management, and event flow are exactly where bugs hide and exactly what needs exhaustive testing.

Decoupling the orchestration substrate from real model calls is non-negotiable for system quality. The mechanism is the digital twin: a binary that obeys the same launch protocol, emits the same output format, and exits with the same codes as the real agent -- but produces deterministic scripted output instead of model-generated output.

## Required Capabilities

- **Digital twin per agent runner type.** A `claude-twin` binary that mimics Claude Code's process model. A `pi-twin` binary that mimics Pi. New agent types ship with their twin alongside the handler.
- **Drop-in invocation.** Twins are launched with the same command, environment variables, working directory, and lifecycle as real agents. The agent runner's handler code does not branch on "is this a twin?" -- it launches whatever binary the workflow/policy specifies.
- **Scriptable behavior.** Twin output is determined by a scenario script: "given this prompt, produce this sequence of tool calls, then this final message, then exit with code 0." Scenarios cover golden paths, partial failures, rate limits, hangs, malformed output, and adversarial outputs.
- **Scenario test harness.** A subsystem (S07: Scenario Harness) that loads scenarios, launches workflows against twin binaries, and asserts on observable outcomes: state transitions emitted, files modified in workspaces, hook executions, event bus contents.
- **Coverage discipline.** Test coverage targets across all subsystems including error paths. Architecture decisions (interface design, dependency injection) chosen to make the system testable, not just to make production work.
- **Fast feedback loop.** The scenario suite runs in seconds-to-minutes, not hours. Twins are designed for speed; they do not sleep or wait unless a scenario requires it.

## Measures of Success

- The full scenario suite runs without any real agent process and without any token spend.
- A new agent runner type cannot be merged without its twin binary and a baseline set of scenario tests.
- Edge cases that are expensive or impossible to reproduce with real agents (rate limits, timeouts, partial output) are first-class scenarios.
- Test coverage targets are enforced in CI for every subsystem, including error handling paths.
- A developer can write a new workflow and validate it against the scenario harness before any real agent ever sees it.
- Regressions in orchestration, hooks, policy, or workspace logic are caught by the scenario suite, not in production.

## Open Questions

- What is the right scenario definition format? YAML script (declarative, simple), Go code (programmatic, flexible), or DSL (specialized)?
- Do twins share infrastructure (a common twin framework with per-agent-type configuration), or is each twin a fully independent binary?
- How do scenario tests assert on event bus contents and state transitions -- do they consume the event JSONL files, subscribe live, or both?
- Where does property-based testing fit? Are there workflow invariants we can express and have generated scenarios attempt to violate?
- How do we keep twins honest -- prevent twin behavior from drifting from real agent behavior over time? Periodic conformance tests against real agents?

## Cross-References

- [docs/concepts/digital-twins.md](../concepts/digital-twins.md) -- The pattern this goal depends on
- [S04: Agent Runner](../subsystems/agent-runner.md) -- Owns the handler interface that twins must satisfy
- [S07: Scenario Harness](../subsystems/scenario-harness.md) -- The subsystem that runs scenarios against twins
- [G06: Bootstrapping](bootstrapping-self-building.md) -- Self-build cycles depend on scenario tests as a regression net
- [P04: System Coherence at Scale](../problems/system-coherence.md) -- Testing maintains coherence as the system grows
- [P05: Agent Behavior Enforcement](../problems/behavior-enforcement.md) -- Scenarios verify enforcement actually enforces
- [P07: Feedback Loop Absence](../problems/feedback-loops.md) -- Scenarios are the fastest feedback loop the system has
