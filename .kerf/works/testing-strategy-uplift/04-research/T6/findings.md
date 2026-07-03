# T6 — Coverage Baseline Population: Research Findings

**Track:** T6 — Coverage Baseline Population  
**Date:** 2026-05-20  
**Status:** complete

---

## Research Questions

1. What is the current per-package coverage?
2. Does `scripts/coverage-gate.sh` parse `go tool cover -func` output correctly?
3. What format does `coverage.baseline` need?
4. Are there pre-existing test failures that would skew the baseline?
5. Does `make check` call the gate correctly?

---

## Findings

### Q1: Current per-package coverage (2026-05-20)

Overall: 75.2%

| Package | Coverage | Gate Floor | Status |
|---------|----------|------------|--------|
| internal/core | 73.1% | ≥95% | BELOW |
| internal/brcli | 85.8% | ≥90% | BELOW |
| internal/eventbus | 83.1% | ≥90% | BELOW |
| internal/workspace | 81.5% | ≥90% | BELOW |
| internal/queue | 70.3% | ≥90% | BELOW |
| internal/handler | 86.8% | ≥90% | BELOW |
| internal/lifecycle | 81.0% | ≥90% | BELOW |
| internal/handlercontract | 84.9% | ≥90% | BELOW |
| internal/branching | 83.0% | ≥85% | BELOW |

No internal package currently meets its gate floor. This means:
- Populating baseline with current numbers correctly calibrates the REGRESSION gate (no regressions from today's baseline)
- The ABSOLUTE FLOOR gates will immediately fail, which is correct/intentional behavior (honest gate, not vacuous pass)

### Q2: Gate script format correctness

Script parses `go tool cover -func` output correctly. Per-package lines end in `(statements) <pct>%`. Script strips the module prefix and `%` suffix. Format is correct.

### Q3: coverage.baseline format

One entry per line: `<pkg-path-suffix>  <pct>` (without % sign). E.g.:
  internal/core  73.1
  internal/eventbus  83.1

### Q4: Pre-existing test failures

Several packages fail currently:
- `internal/core`: TestBRServeBI003_ForbiddenInvocation (worktree path interference — `.harmonik/worktrees/` paths leak into the scan)
- `internal/brcli`: 2 failures (TestRunWithDBLockedRetryBrUnavailableRetriedHkyjsk8, TestBI010c_SpecContainsWorkflowLabelDiscipline)
- `internal/handler`: 4 failures (session/launch tests)
- `internal/lifecycle`: 2 failures (pidfile lock tests)
- `internal/daemon`: BUILD FAILED (due to queue/cli build error)
- `internal/queue/cli`: BUILD FAILED (assignment mismatch — `parseQueueFlags` returns 4 values, callers expect 3)

hk-b5bc0 ("preexisting fails") is already tracked. T6 bead must declare dep on hk-b5bc0 or explicitly scope to packages that do compile+pass.

### Q5: make check gate invocation

Gate runs. With empty baseline, passes vacuously. After T6 population, will correctly fail on floor gates. The devx impact: pre-push hook (Tier 2 = `make check`) will start failing. This is correct behavior but should be communicated so agents know it is expected.

---

## Options and Tradeoffs

Option A: Populate baseline now, accept gate floor failures
- Pros: honest gate, regression protection starts immediately
- Cons: pre-push hook breaks until coverage improves; agents may be surprised

Option B: Fix hk-b5bc0 and queue/cli build error first, THEN populate baseline
- Pros: clean run; known-good numbers; no noise from pre-existing failures
- Cons: delays T6; dependency on other work
- Verdict: RECOMMENDED — T6 bead should dep on hk-b5bc0 and the queue/cli build fix

Option C: Populate baseline with CURRENT numbers but disable floor gates temporarily
- Cons: defeats the spec-defined gate intent
- Verdict: NOT recommended

---

## Risks and Unknowns

1. The core 73.1% vs 95% floor gap is large. T6 by itself does not close it — that requires T1+T3+T2 coverage improvements. This is expected; T6 is the calibration step.
2. queue/cli build failure blocks `./...` runs; investigation needed (separate from T6).
3. The worktree-path leak in TestBRServeBI003 (scans `.harmonik/worktrees/` paths) is a test isolation bug, not a coverage gap. Should be filed as a separate bead if not already tracked.
