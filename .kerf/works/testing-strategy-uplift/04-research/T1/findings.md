# T1 — Unit Test Gap Audit + Convention Enforcement: Research Findings

**Track:** T1 — Unit Test Gap Audit + Convention Enforcement  
**Date:** 2026-05-20  
**Status:** complete

---

## Research Questions

1. Which packages are below 85% coverage (the utility floor)?
2. Are naming conventions followed (TestXxx, no Test_ prefix)?
3. What is the right dispatch order for T1 bead-per-gap?
4. Can T1 start before T6 is done?

---

## Findings

### Q1: Packages below the 85% utility floor

From T6 coverage data (2026-05-20):

Below ANY floor (not just utility 85%):
  - internal/core: 73.1% (floor: 95%)
  - internal/queue: 70.3% (floor: 90%)
  - internal/eventbus: 83.1% (floor: 90%, also below 85% utility)
  - internal/workspace: 81.5% (floor: 90%, also below 85% utility)
  - internal/brcli: 85.8% (floor: 90%)
  - internal/handler: 86.8% (floor: 90%)
  - internal/lifecycle: 81.0% (floor: 90%, also below 85% utility)
  - internal/handlercontract: 84.9% (floor: 90%)
  - internal/branching: 83.0% (floor: 85%)

Every internal package is below its floor. T1's job is to produce one bead per gap, prioritized by gap magnitude and criticality.

Priority ordering (largest gap or highest criticality):
  1. internal/core: 21.9pp below floor (critical — catches EV/spec invariants)
  2. internal/queue: 19.7pp below floor (work-loop critical path)
  3. internal/lifecycle: 9.0pp below floor
  4. internal/workspace: 8.5pp below floor
  5. internal/eventbus: 6.9pp below floor
  6. internal/brcli: 4.2pp below floor
  7. internal/handlercontract: 5.1pp below floor
  8. internal/handler: 3.2pp below floor
  9. internal/branching: 2.0pp below utility floor

### Q2: Naming convention check

Checking for convention violations in existing test files:
  - testing.md requires: TestXxx (unit), TestIntegration_Xxx, TestScenario_Xxx, TestCrash_Xxx, TestProp_Xxx
  - Violations: Test_ prefix not explicitly searched; golangci-lint + testifylint should catch misuse

From the test file listing in `02-analysis.md`: the pattern appears consistent. No `TestSuite` structs observed. No `Test_` prefix violations found in a spot check.

T1 bead: formal audit via `grep -rn "^func Test_\|TestSuite\|func test" internal/` to surface violations.

### Q3: Dispatch order for T1 bead-per-gap

T1 produces metadata (a list of beads), not code changes. The T1 bead itself is an audit bead. The actual coverage-improvement beads it spawns are children. This is correct framing: T1 is the audit, not the implementation.

T1 bead covers:
1. Run coverage analysis (depends on T6 baseline)
2. Produce list of packages below floor with gap magnitude
3. Create child beads labeled `testing-gap` + `codename:testing-strategy-uplift`
4. Document audit methodology in `docs/testing-friction-mining.md` (T8 dependency)

### Q4: T1 dependency on T6

T1 strictly depends on T6 having populated `coverage.baseline`. Without a baseline, the coverage data exists (we ran it above) but the regression gate is uncalibrated. T1 can technically run with just the coverage profile output, but should be dispatched AFTER T6 to avoid a stale baseline.

---

## Options and Tradeoffs

T1 bead scope options:

Option A: T1 = audit + file all gap beads in one bead
- Pros: single commit packages all the information
- Cons: large bead; may time out in harmonik run

Option B: T1 = audit only + brief (produces beads list); separate dispatch wave creates gap beads
- Pros: audit is fast; gap bead creation is parallelizable
- Verdict: RECOMMENDED

---

## Risks and Unknowns

1. core's 73.1% coverage includes many functions that are tested only indirectly via daemon-level tests. The raw per-package number may undercount effective coverage. T1 audit should note this.
2. queue/cli has a BUILD FAILED — cannot be measured until the build error is fixed.
