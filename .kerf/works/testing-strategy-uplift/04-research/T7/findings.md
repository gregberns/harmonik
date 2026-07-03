# T7 — Cadence: make test-all + Lefthook + Session Checklist: Research Findings

**Track:** T7 — Cadence  
**Date:** 2026-05-20  
**Status:** complete

---

## Research Questions

1. Does `make check-full` serve as the canonical "run all tests" target?
2. Does `scripts/coverage-gate.sh` emit a delta summary line?
3. Is the lefthook pre-push hook operational?
4. Where does the per-session checklist belong?

---

## Findings

### Q1: make check-full as test-all

`make check-full` is already defined (Makefile line ~149):
  - Runs `make check` (Tier 2: lint + race + tidy + coverage gate + govulncheck)
  - Then `go test -race -tags=integration ./...`
  - Then `go test -race -tags=scenario ./test/scenario/...`
  - Then `go test -tags=crash ./test/crash/...`

This covers all tiers. A `make test-all` alias would just redirect to `check-full`. Value: the name `test-all` is more discoverable than `check-full` for developers unfamiliar with the tier naming. Low-effort alias.

### Q2: Coverage delta summary in gate script

`scripts/coverage-gate.sh` does NOT currently emit a delta summary line. It emits:
  - Per-package pass/fail lines
  - Final PASS/FAIL with failure list

A "coverage delta vs baseline: +X.Xpp" line would require the script to:
1. Compute total coverage (already implicitly has per-package data)
2. Sum vs baseline total
3. Emit the delta

This is a small addition to the existing script (5-10 lines of bash). No new script needed.

### Q3: Lefthook pre-push hook operational check

From `02-analysis.md`: `lefthook.yml` is fully wired. Pre-push hook runs `make check`.

Cannot verify "operational" without running `lefthook run pre-push` in this research pass (would run the full check suite, including coverage gate which will fail). Research conclusion: the hook is CONFIGURED. Whether it runs cleanly depends on T6 (coverage baseline population). After T6, running `lefthook run pre-push` will fail on absolute floor gates — expected and intentional.

T7 bead should include: run `lefthook run pre-push` after T6, document the expected output (gate failures are expected until T1 coverage-gap beads are resolved).

### Q4: Per-session checklist location

Current `HANDOFF.md` has a detailed orchestration directives section but no "before session-end, run make check" checklist item.

Options:
  a. Add to `HANDOFF.md` under a "Session Protocol" section
  b. Create `docs/session-protocol.md` (new standalone doc)
  c. Add to `AGENTS.md` under a "Session End" section

Verdict: Add to `AGENTS.md` (session-end section) — it's the canonical agent-facing doc. Also add a note to HANDOFF.md template. T7 bead creates the AGENTS.md addition.

---

## Options and Tradeoffs

T7 is a small cleanup track. All 4 requirements are achievable in a single bead:
1. `make test-all` alias (3 lines of Makefile)
2. Coverage delta line in coverage-gate.sh (~10 lines)
3. Lefthook operational check (manual step + documentation)
4. AGENTS.md session-checklist addition

Single bead scope is appropriate. Depends on T6 for meaningful delta output.

---

## Risks and Unknowns

1. `make check-full` comment says "Agent declared-done MUST pass this" — this is aspirational. Currently it would fail on integration/crash stubs passing but the absolute coverage floors failing. T7 should NOT require check-full to be green; it should document the expected failure modes.
2. The `lefthook.yml` pre-commit also runs `make agent-review` which gracefully stubs. No issue.
