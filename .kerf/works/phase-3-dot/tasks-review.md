# Pass-7 Tasks Review

**Reviewer:** general-purpose sub-agent, fresh context, round 1.
**Date:** 2026-05-23.
**Artifact reviewed:** `07-tasks.md`.

## Verdict

```json
{
  "schema_version": 1,
  "verdict": "APPROVE",
  "flags": ["nit-existing-workflowvalidator-package-not-cross-referenced", "nit-should-fix-count-wording-6-vs-7"],
  "notes": "Pass-7 task decomposition meets all jig done-criteria: 100% coverage of changelog requirement IDs (WG-001..WG-038 + WG-031a, EM-055..EM-061 + EM-005/a/b/c + EM-007, HC-058..HC-062, CP-053..CP-058 + CP-038a) verified against §8 matrix; pass-6 BLOCKER is wave-0 (T-FIX-C5-BLOCK), all 7 SHOULD-FIX (Contradictions 2/3/4 + CI-1/2/3/8) and 6 NIT items appear as explicit T-FIX-* IDs folded into spec-transcription tasks; DAG has no cycles and all 'Depends on' refs resolve; parallelization waves are file-disjoint on spot-check (Wave-1A C1||C4, Wave-1B C2||C5, Wave-2 IMPL-001||IMPL-005); all 10 pre-filed test beads present as T-TEST-* with sensible deps; granularity within half-day to 2-day bound with T-IMPL-008 pre-marked for split. Minor non-blocking nit: existing `internal/workflowvalidator/` package isn't cross-referenced as a possible integration point for T-IMPL-002's `internal/workflow/dot/validator.go` — worth a heads-up for the implementer but not a pass-7 fix. Advance to pass-8 (Square)."
}
```

## Reviewer findings (preamble)

1. **Coverage matrix**: All requirement IDs from `05-changelog.md` appear in §8 coverage matrix. WG-001..WG-038 + WG-031a, EM-005 family + EM-007 + EM-055..EM-061, HC-058..HC-062, CP-053..CP-058 + CP-038a. **Complete.**

2. **Pass-6 carry-in**: BLOCKER = T-FIX-C5-BLOCK (wave-0). 7 SHOULD-FIX (Contradictions 2/3/4 + CI-1/CI-2/CI-3/CI-8) = T-FIX-SHOULD-01..07. 6 NIT (CI-4..CI-7 + citation sweep + event-model confirm) = T-FIX-NIT-01..06. **All accounted for.**

3. **DAG validity**: Walked the dependency graph at §5. No cycles. All "Depends on" refs resolve to declared tasks.

4. **Parallelization spot-checks**:
   - Wave 1A: T-SPEC-C1 (workflow-graph.md NEW) || T-SPEC-C4 (control-points.md + event-model.md) — file-disjoint. ✓
   - Wave 1B: T-SPEC-C2 (execution-model.md) || T-SPEC-C5 (specs/examples/ NEW) — file-disjoint. ✓
   - Wave 2: T-IMPL-001 (`internal/workflow/dot/parser.go`) || T-IMPL-005 (`internal/handler/outcome.go`) — file-disjoint. ✓
   - Concern: T-IMPL-012 extends T-IMPL-002's `validator.go`; §6 Wave-3 puts T-IMPL-012 parallel with T-IMPL-006/007/003 — but T-IMPL-012's "First impl file" IS validator.go (same file as T-IMPL-002). If T-IMPL-002 is wave-2 done, T-IMPL-012 in wave-3 is fine. Self-consistent.

5. **First-impl-file plausibility**: `internal/workflow/` doesn't exist yet (only `workflowvalidator/`). New package `internal/workflow/dot/` is plausibly new. `internal/handler/` exists. `internal/daemon/` exists. `cmd/harmonik/run.go` exists. Minor concern: existing `internal/workflowvalidator/` package (with `dotparser.go` + `validator.go`) suggests potential overlap with the proposed `internal/workflow/dot/validator.go` — could be an integration friction point but not a pass-7 blocker.

6. **Test-bead linkage**: All 10 (hk-fiq55, hk-lphyf, hk-aoz34, hk-yfm05, hk-isp3y, hk-w3eip, hk-4fvid, hk-6zvki, hk-zqr6f, hk-geype) present with sensible impl deps.

7. **Granularity**: T-IMPL-002 (validator covering ~25 WG-NNN reqs) at 2 days is tight but pre-flagged with split point. T-IMPL-008 (cascade) at 1.5-2 days with split-point identified. T-IMPL-011 (sub-workflow + acyclicity) at 2 days reasonable. OK.

8. **Acceptance criteria**: Spot-checking T-IMPL-002 (concrete grep-style rejection rules), T-IMPL-008 (5-step cascade enumerated + specific event name), T-IMPL-010 (specific event names, payload kind). Concrete and testable.

**Minor observations** (non-blocking):
- The existing `internal/workflowvalidator/` package isn't mentioned in T-IMPL-002's "first impl file" — could be deliberate (new DOT-specific validator vs. existing spec-validator) but worth a NIT.
- T-FIX-SHOULD-* numbering is 01..07 (7 items) but the §2 narrative says "all 6 SHOULD-FIX" in places. Internally consistent because §9 done-criteria says "6 SHOULD-FIX items (incl. CI-8…)" — counting Contradictions 2/3/4 + CI-1/CI-2/CI-3 = 6, then CI-8 addendum = 7. T-FIX-SHOULD-01..07 correctly enumerates 7. Wording inconsistency only.

Verdict: APPROVE. Task list is complete, traceable, DAG-valid, parallelization realistic, granularity within bounds. Ready for pass-8.
