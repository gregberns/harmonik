# Change Spec: T1 — Unit Test Gap Audit + Convention Enforcement

**Component:** T1  
**Date:** 2026-05-20  
**Research:** 04-research/T1/findings.md

---

## Requirements (from 03-components.md)

1. Run go test -cover and produce per-package coverage report.
2. Identify packages below 85% coverage; produce one bead per gap.
3. Confirm naming conventions (TestXxx, no Test_, no TestSuite structs).
4. Document audit methodology in `docs/testing-friction-mining.md` (shared with T8).

---

## Research Summary

- Every internal package is below its floor: core at 73.1% (floor 95%), queue at 70.3% (floor 90%), etc.
- T1 depends on T6 (populated baseline).
- T1 should produce gap-bead metadata, not implement coverage improvements itself.
- queue/cli has a build failure blocking coverage measurement there.

---

## Approach

T1 is an audit bead that produces child gap beads. It does NOT write production code.

**Audit steps:**
1. Run `go test -covermode=atomic ./internal/...` (after T6 deps resolved).
2. For each package below its floor, compute gap magnitude.
3. Scan for naming violations: `grep -rn "^func Test_\|type.*Suite\b" internal/`
4. Produce one `testing-gap` bead per package below floor:
   - Title: `testing-gap: internal/<pkg> coverage at <N>% — floor <M>%`
   - Labels: `testing-gap`, `codename:testing-strategy-uplift`
   - Body: which functions are at 0% coverage (from `go tool cover -func` output), suggested test approach

**Priority order for gap beads (largest absolute gap first):**
  1. internal/core: 21.9pp below 95% floor → highest priority
  2. internal/queue: 19.7pp below 90% floor
  3. internal/lifecycle: 9.0pp below 90% floor
  4. internal/workspace: 8.5pp below 90% floor
  5. internal/eventbus: 6.9pp below 90% floor
  6. internal/handlercontract: 5.1pp below 90% floor
  7. internal/brcli: 4.2pp below 90% floor
  8. internal/handler: 3.2pp below 90% floor
  9. internal/branching: 2.0pp below 85% utility floor

**Naming convention audit:**
- `Test_` prefix: `grep -rn "^func Test_" internal/` — violations get a bead with label `testing-gap test-convention`
- TestSuite structs: `grep -rn "type.*Suite\b.*struct" internal/` — violations get a refactor bead

**Documentation:**
Produce a brief analysis in `docs/testing-audit-2026-05-20.md` (date-stamped, to distinguish from future audits). Methodology section shared/linked from `docs/testing-friction-mining.md` (T8 owns that file).

---

## Files & Changes

- `docs/testing-audit-2026-05-20.md` — new file: audit results, per-package gaps, methodology
- NO source code changes in T1.
- Child beads filed as `br create` commands (not files).

---

## Acceptance Criteria

1. ≥9 `testing-gap` beads filed (one per internal package below floor).
2. Each bead has title format `testing-gap: internal/<pkg> coverage at <N>% — floor <M>%`.
3. Each bead has labels `testing-gap` and `codename:testing-strategy-uplift`.
4. Naming convention scan completed; any violations are in separate beads.
5. `docs/testing-audit-2026-05-20.md` exists with per-package gap table and methodology.

---

## Verification

```bash
br list --label testing-gap --status open 2>&1 | wc -l  # must be ≥9
ls docs/testing-audit-2026-05-20.md  # must exist
```

---

## Dependencies

- T6 (coverage baseline populated, pre-existing failures fixed)

---

## Bead Candidates

- T1-audit: `T1: unit test gap audit — produce per-package coverage report and testing-gap beads` (type: task, chore)
