# Change Spec: T6 — Coverage Baseline Population

**Component:** T6  
**Date:** 2026-05-20  
**Research:** 04-research/T6/findings.md

---

## Requirements (from 03-components.md)

1. Run `go test -coverprofile` and `go tool cover -func` to extract per-package coverage.
2. Populate `coverage.baseline` with one entry per `internal/` package. Format: `<pkg-path> <pct>`.
3. Single-concern commit with `codename:testing-strategy-uplift` in body.
4. After commit: `make check` runs the gate with the populated baseline.
5. Verify the gate correctly fails if a package is artificially dropped.

---

## Research Summary

- Overall coverage: 75.2%; no internal package meets its absolute floor.
- Pre-existing test failures in core, brcli, handler, lifecycle, daemon; queue/cli has a build failure.
- Populating baseline with current numbers is correct: regression gate calibrated, floor gates fail loudly (intentional).
- T6 bead should declare dep on hk-b5bc0 (fix preexisting fails) and the queue/cli build fix.

---

## Approach

1. Declare dependency on hk-b5bc0 and the queue/cli build fix (separate beads).
2. After those deps merge, run: `go test -coverprofile=/tmp/harmonik-cov.out -covermode=atomic ./internal/...`
   - Exclude queue/cli if it still has build errors
3. Run `go tool cover -func /tmp/harmonik-cov.out` and extract lines ending in `(statements)`.
4. Strip `github.com/gregberns/harmonik/` prefix and `%` suffix; write one line per package to `coverage.baseline`.
5. Commit `coverage.baseline` only (no code changes). Body: `codename:testing-strategy-uplift`.
6. Run `make check` to confirm gate runs (will fail on floor gates — document this as expected).

**Key decision:** Do NOT lower floor thresholds to match current reality. The floor failures after T6 are correct signal. Agents and developers will see red on `make check` until T1 coverage-gap beads improve coverage.

---

## Files & Changes

- `coverage.baseline` — currently a comment-only stub. Add one line per `internal/` package:
  ```
  internal/core  73.1
  internal/brcli  85.8
  internal/eventbus  83.1
  internal/workspace  81.5
  internal/queue  70.3
  internal/handler  86.8
  internal/lifecycle  81.0
  internal/handlercontract  84.9
  internal/branching  83.0
  ```
  (Exact numbers from the clean post-hk-b5bc0 run — not from the T6 research numbers which include failing tests.)

No other files change in this bead.

---

## Acceptance Criteria

1. `coverage.baseline` contains ≥9 non-comment lines.
2. Each line has format `internal/<pkg>  <number>` (no `%` suffix).
3. `scripts/coverage-gate.sh` exits with gate failure messages for absolute floors (core below 95%, others below 90%) — this is CORRECT behavior, not a failure of T6.
4. Gate 3 (regression) produces no failures (baseline = current, no regression by definition).
5. If a single test function is removed from `internal/brcli`, coverage drops ≥1pp and Gate 3 fires with a regression message.
6. Commit body contains `codename:testing-strategy-uplift`.

---

## Verification

```bash
cat coverage.baseline          # should show 9+ package lines
make check 2>&1 | grep FAIL    # should show FLOOR failures for core, queue, etc. (expected)
make check 2>&1 | grep REGRESSION  # should show NOTHING (no regression vs self)
```

---

## Dependencies

- hk-b5bc0 (fix preexisting test failures)
- queue/cli build fix (separate bead — assign in T1 audit)

---

## Bead Candidates

- Primary bead: `T6: populate coverage.baseline with current per-package numbers` (type: chore, labels: `testing-infra`, `codename:testing-strategy-uplift`)

## Validation Beads
- Exploratory-test bead: hk-tgiu9 — explore: T6 coverage baseline — verify make check emits floor failures after baseline populated
