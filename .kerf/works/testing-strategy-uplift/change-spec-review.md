# Change Spec Review — testing-strategy-uplift

**Reviewer:** inline review (self-review round 1)  
**Date:** 2026-05-20  
**Verdict:** APPROVE with notes

---

## Review: Requirements Traceability

All 8 tracks from 03-components.md have corresponding specs in 05-specs/:

| Track | Spec File | Requirements Covered |
|-------|-----------|---------------------|
| T1 | T1-unit-gap-audit-spec.md | All 4 ✓ |
| T2 | T2-property-tests-spec.md | All 6 ✓ (with SelectEdge clarification) |
| T3 | T3-integration-tests-spec.md | All 5 ✓ |
| T4 | T4-scenario-coordination-spec.md | All 4 ✓ |
| T5 | T5-lint-depguard-spec.md | All 4 ✓ |
| T6 | T6-coverage-baseline-spec.md | All 5 ✓ |
| T7 | T7-cadence-spec.md | All 5 ✓ |
| T8 | T8-friction-mining-spec.md | All 3 ✓ |

No requirements from 03-components.md are missing. No spec content added without a requirement.

---

## Review: Acceptance Criteria

All acceptance criteria are concrete and testable:
- Shell commands provided for verification in each spec.
- "Must fail when condition X removed" style criteria in T3 (regression guard).
- No vague language detected.

---

## Review: File Path Validation

All file paths reference real locations or known-absent stubs:
- `coverage.baseline` — exists (stub) ✓
- `scripts/coverage-gate.sh` — exists ✓
- `.golangci.yml` — exists ✓
- `internal/daemon/daemon.go` — exists ✓
- `test/integration/integration_stub.go` — exists ✓
- `test/scenario/scenarios_test.go` — exists ✓
- `docs/testing-friction-mining.md` — new file, intentional ✓
- `docs/scenario-test-authoring-checklist.md` — new file, intentional ✓

---

## Review: Research Consistency

Key deviations from 03-components.md addressed in specs:
1. T2: `SelectEdge` function not found → replaced with `BeadID round-trip` + documented in spec. Correct.
2. T4: hk-sc1..hk-sc5 placeholder IDs resolved to hk-p3diy children. Documented. Correct.
3. T5: eventbus cannot be activated as-written → filed as separate bead. Documented. Correct.
4. T6: absolute floor failures expected after population → documented as intentional. Correct.

---

## Review: Error Handling and Edge Cases

- T2: Invalid BeadID inputs handled via constrained generators or skip-on-error. ✓
- T3: ctx.Err() from cancelled daemon.Start is expected, not a failure. ✓
- T6: Pre-existing test failures addressed (dep on hk-b5bc0). ✓
- T5: eventbus rule NOT activated (would fail). ✓

---

## Issues Found

**Minor:**
1. T2 spec says "testify/require" in the integration test pattern but `testify` is not in go.mod. Must use stdlib `t.Helper()` + `t.Fatalf()` for property tests, and only add testify if/when a separate bead adds it. The T3 spec also uses `require.NotNil` — needs to be either: (a) conditional on testify being added, or (b) replaced with `if capturedBus == nil { t.Fatal("...") }`.

**Recommendation:** Add a note to T3 spec: "If testify is not in go.mod, use t.Fatal() directly. A separate bead should add testify/require when needed."

2. T7 spec references adding to `AGENTS.md` — need to confirm AGENTS.md is the right file (could be overwritten by harmonik tooling). Low risk but worth noting.

---

## Verdict: APPROVE

All specs are implementable. Minor note on testify dependency in T3 should be noted in the T3 bead body. No blocking issues.
