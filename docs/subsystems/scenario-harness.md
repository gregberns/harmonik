---
title: "S07: Scenario Harness"
status: seed
type: subsystem
solves: [P04, P05, P07]
uses: [orchestrator-core, agent-runner, event-bus, workspace-manager, digital twin binaries]
related: [docs/concepts/digital-twins.md, docs/goals/end-to-end-testability.md, docs/subsystems/agent-runner.md, docs/subsystems/orchestrator-core.md]
created: 2026-04-19
updated: 2026-04-19
---

# S07: Scenario Harness

## Summary

The scenario harness is the end-to-end test infrastructure for harmonik. It loads scenario definitions, configures the system to launch digital twin agent binaries instead of real ones, runs full workflows against the twins, and asserts on observable outcomes (state transitions, file changes, hook executions, event bus contents). It is how harmonik proves to itself that the orchestration substrate works -- without spending tokens or making real model calls.

## Purpose

Real model calls are slow, expensive, and non-deterministic. Tests built on them are run rarely, cover few edge cases, and are flaky enough to be ignored when they fail. That is incompatible with G07 (end-to-end testability) and unacceptable for a system that aims to self-build (G06).

The scenario harness inverts the test model: real model calls are the exception (covered by a small conformance suite), and the dominant test population runs against twins. Each twin scenario is fast, deterministic, and exhaustive over edge cases the real agent would never encounter on demand (rate limits, hangs, malformed output, mid-task crashes).

## Key Responsibilities

- **Scenario definition loading.** Read scenario specs (format TBD -- YAML or Go-as-code) from a known location. A scenario specifies: workflow to run, twin behaviors per agent role, expected observable outcomes.
- **Twin binary substitution.** Override the agent runner's binary selection so that workflow agents are routed to twin binaries (`claude-twin`, `pi-twin`, ...) instead of real ones. The override is configuration-level, not a code path inside the runner.
- **Workflow execution.** Drive the full harmonik stack -- orchestrator, policy engine, event bus, agent runner, hook system, workspace manager -- exactly as production would, with the only substitution being the agent binaries themselves.
- **Outcome assertions.** Inspect the observable surface after the workflow completes (or times out): expected state transitions emitted, files modified in workspaces, hooks fired, events on the bus, exit codes. Assert against the scenario's expected outcomes.
- **Fixture management.** Set up clean repository state, ephemeral workspaces, and isolated event-log directories per scenario. Tear down on completion.
- **Scenario composition.** Allow scenarios to be parameterized and composed (e.g., "the basic implement-review-merge workflow" parameterized by which step fails).

## Interfaces

**Inputs:**
- Scenario definitions (filesystem-resident, version-controlled)
- A real harmonik build (orchestrator, runner, etc.)
- Twin binaries on PATH (or at scenario-specified locations)

**Outputs:**
- Test pass/fail results to the caller (CI, developer terminal)
- Optional artifacts: captured event JSONL, workspace snapshots, twin output transcripts -- for debugging failed scenarios
- Coverage data feeding back into CI gates

## Design Principles

- **Same code paths as production.** The harness substitutes binaries, nothing else. No "test mode" branches in the orchestrator, runner, hook system, or any other subsystem. If a scenario passes against twins, the only remaining variable in production is the twin/real output gap, which is covered by a separate conformance suite.
- **Scenarios are the spec.** A scenario is a precise statement: "given this workflow and these scripted agent behaviors, the system must produce these observable outcomes." Scenarios double as living documentation of intended workflow behavior.
- **Fast first, exhaustive second.** A subset of scenarios runs on every PR (smoke), the full suite runs on merge (regression), nightly runs include scale and stress scenarios. The harness must support all three cadences.
- **Tokens are an exceptional cost.** Tests that require real model calls live in a separate conformance suite, are clearly marked, and are run on a schedule (not per-commit).

## Candidate Implementations

- **Go test framework + scenario YAML.** Scenarios as YAML files, loaded by a Go test helper that drives the harness. Advantage: declarative scenarios are easy to author and review. Risk: YAML expressiveness limits.
- **Go test framework + scenario as Go code.** Scenarios written directly as Go test functions using a scenario builder API. Advantage: full programmatic flexibility. Risk: scenarios become opaque to non-Go reviewers.
- **Hybrid.** YAML for declarative scenarios (the common case), Go for complex scenarios needing programmatic control flow.

## Open Questions

- Scenario definition format (YAML vs Go vs DSL) -- decision needed before implementation begins.
- How are assertions specified? Comparison against expected event JSONL? Predicates over final workspace state? Both?
- Where do twin binaries live -- in the harmonik repo (same release cadence as the system) or vendored separately?
- How does the harness handle non-determinism that twins do not eliminate (e.g., timing-dependent race conditions in the real orchestrator)? Repeatable seeds? Deterministic schedulers in test mode?
- Can scenarios be generated by the improvement loop (S09) when it observes new failure patterns in production -- closing the loop "production failure -> regression scenario -> never again"?

## Cross-References

- [docs/concepts/digital-twins.md](../concepts/digital-twins.md) -- The pattern this subsystem depends on
- [G07: End-to-End Testability](../goals/end-to-end-testability.md) -- The goal this subsystem delivers
- [G06: Bootstrapping](../goals/bootstrapping-self-building.md) -- Self-build cycles depend on the scenario suite as a regression net
- [S04: Agent Runner](agent-runner.md) -- Owns the binary-selection mechanism the harness exploits
- [S01: Orchestrator Core](orchestrator-core.md) -- Driven end-to-end by the harness
- [S03: Event Bus](event-bus.md) -- Event JSONL is the primary assertion surface
- [docs/subsystems/verifier-layer.md](verifier-layer.md) -- The archived predecessor of this S07 slot
