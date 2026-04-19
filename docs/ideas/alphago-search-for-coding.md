---
title: "I04: AlphaGo Search for Coding"
status: seed
type: idea
relates-to: [G01, AlphaGo]
sources: [refs/AlphaGo-modeled-orch-system.md]
created: 2026-04-13
updated: 2026-04-13
---

# I04: AlphaGo Search for Coding

## Core Concept

For problems with verifiable outcomes -- tests pass, types check, performance improves, linter is clean -- use tree search over solution candidates. Explore multiple solution paths in parallel, evaluate each against verification criteria, and expand the most promising. This is AlphaGo's core loop applied to software engineering: search, evaluate, expand.

## Best Problem Types

Not all coding tasks benefit from search. The key requirement is a verifiable outcome signal -- something that tells you whether a candidate solution worked. Problems with clear verification:

- **Bug fixing**: Does the bug reproduce? Does the fix resolve it without breaking other tests?
- **Refactoring**: Do all existing tests still pass? Are the new abstractions cleaner (measurable via complexity metrics)?
- **Performance optimization**: Is the benchmark faster? By how much?
- **Type error repair**: Does the type checker pass? Are the types semantically correct?
- **Migration tasks**: Does the migrated code pass the same test suite as the original?
- **SQL optimization**: Is the query faster? Does it return the same results?

Problems WITHOUT clear verification -- "is this API design good?" or "is this architecture sound?" -- are less suited to search. They require judgment, not measurement.

## The Practical Pattern

The minimal viable search pattern has three components:

1. **Search**: Generate N candidate solutions for a problem. These can come from the same model with different prompts, different models, or the same model with different temperatures. The key is diversity of approaches.
2. **Verifier**: Run each candidate against the verification criteria. Tests, type checks, benchmarks, linters -- whatever signals are available. Produce a score or pass/fail for each.
3. **Trace collection**: Record everything -- the problem statement, each candidate, the verification results, the final selection. Traces enable debugging ("why did it pick that approach?") and learning ("which search strategies produce better candidates?").

You do not need learned evaluation models initially. Programmatic verifiers (test suites, type checkers) are sufficient. Learned models can be added later to evaluate softer criteria.

## The Fan-Out Pattern

Kilroy's reference template already implements a version of this: three agents independently propose solutions, then a synthesizer agent combines the best elements. This is breadth-first search with a synthesis step. It works well for problems where different approaches may each have good parts worth combining.

The fan-out count is a tunable parameter. More candidates = better coverage of the solution space, but higher cost. For well-understood problem types, even two candidates with a verifier is a significant improvement over a single attempt.

## Relationship to Broader System

Search fits naturally into harmonik's architecture. The orchestrator spawns search candidates as parallel workflow instances. The verifier layer evaluates each. The orchestrator selects the best result and transitions to the next state. The deterministic skeleton manages the search process; the probabilistic organs generate the candidates.

## Open Questions

1. How do we determine the right fan-out factor for a given problem? Static config, or adaptive based on problem complexity?
2. Can we use early termination -- stop searching when a candidate scores above a threshold?
3. How do we handle non-independent search paths (where one candidate's approach should inform another)?
4. What is the cost/benefit breakeven? At what point does the verification overhead exceed the value of multiple candidates?

## Cross-References
- [G01: Structured Emergent Systems](../goals/structured-emergent-systems.md) -- Search is a structured mechanism for emergent solutions
- [AlphaGo-Modeled System](../concepts/alphago-system.md) -- The architectural source of this pattern
- [Kilroy](../concepts/kilroy.md) -- Fan-out pattern in practice
