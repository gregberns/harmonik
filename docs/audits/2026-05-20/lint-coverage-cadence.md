# Lint / Coverage / Cadence Audit — 2026-05-20

Source: read-only investigation under user directive 2026-05-20.

## Status Summary

| Area | Status | Grade |
|---|---|---|
| Linting (golangci-lint) | 21 linters active | A |
| Depguard component matrix | 6/17 rules active, 11 stubs | C |
| Coverage gates | 3 gates functional; logic misaligned with spec | B+ |
| Test cadence | 3-tier gauntlet documented | A- |
| Property testing | Planned, not started | C |
| Per-package floors | Spec clear, code fuzzy | B- |
| Pre-commit hooks | Configured, not installed | B |
| Test structure | 692 test files; mature | A |

## Top Fixups

1. **Activate 11 commented-out depguard stubs (.golangci.yml lines 156-266)** — `eventbus`, `policy`, `handler-contract`, `adapter-br`, `adapter-ntm`, `workspace`, `agentrunner`, `hook`, `memory`, `handler-impls`, `scenario`. HIGH priority — protects component graph as packages land.
2. **Align coverage-gate.sh CORE_PATTERN with testing.md** — currently only `internal/core/**` gets 95% threshold; spec requires it for `orchestrator`, `workspace`, `eventbus`, `handler`, `reconciler` too. MEDIUM.
3. **Bootstrap property testing** — add `pgregory.net/rapid` to go.mod; add a sample `*_prop_test.go` in a leaf package. MEDIUM.
4. **Populate coverage.baseline** — currently empty; regression gate vacuously satisfied. Track via lint-coverage-cadence beads. MEDIUM.
5. **Create `scripts/validate-commit-msg.sh`** — referenced by lefthook.yml:37 but doesn't exist. Hook fails if installed. HIGH.
6. **Wire `tools/forbid-import` properly in `make check`** — Makefile:140 prints "not yet present" but the directory exists.

## Three-Tier Gauntlet (intact)

| Tier | Command | Budget | Pre-hook |
|---|---|---|---|
| 1 | `make check-fast` | <15s | pre-commit |
| 2 | `make check` | 3-5m | pre-push |
| 3 | `make check-full` | 10-15m | agent-declared-done |

- Tier 1: gofumpt/gci/vet/build/lint-delta/test-short on staged files.
- Tier 2: full tree lint, race tests, mod-tidy diff, coverage gate, vulncheck.
- Tier 3: Tier 2 + integration + scenario + crash (fast subset).

Race detection: off in Tier 1; on in Tier 2 + scenario; off in crash.

## Notes

- `internal/testhelpers` correctly excluded from coverage floor.
- 692 test files; `e2e_real_claude` and `scenario` build tags in use.
- `lefthook.yml` complete but not installed (user-run bootstrap).
- testify + rapid-style property testing chosen.

Full investigation: agent a18a0f632378bf913 output.
