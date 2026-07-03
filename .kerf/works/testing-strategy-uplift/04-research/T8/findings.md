# T8 — Friction-Mining Loop: Research Findings

**Track:** T8 — Friction-Mining Loop  
**Date:** 2026-05-20  
**Status:** complete

---

## Research Questions

1. What does `docs/testing-friction-mining.md` need to contain?
2. Can the examples (hk-37zy8, hk-2hb2y, hk-yjduq, hk-012af) be mapped to tiers?
3. What is the `testing-gap` label status in the bead ledger?
4. What integration-tier equivalents from T3 need a bead filed in T8?

---

## Findings

### Q1: Content requirements for testing-friction-mining.md

The T8 bead must create a document that captures the pattern:
  "runtime failure that unit tests passed → missing integration/scenario/property test → testing-gap bead"

Required sections:
  1. Pattern definition (3-line description)
  2. Tier mapping (which failure mode belongs to which test tier)
  3. Bead template (title format, labels, which tier, spec/behavior covered)
  4. Worked examples (the 4 named bugs mapped to tiers)
  5. Cadence (when to file, not just how)

### Q2: Example bug-to-tier mapping

Bugs referenced in `03-components.md` T8 requirements:

| Bug | Class | Tier that would catch it | Test needed |
|-----|-------|--------------------------|-------------|
| hk-37zy8 | Policy goroutine never Subscribe()'d | Integration (T3) | TestIntegration_HandlerPausePolicyGoroutine_Subscribed |
| hk-2hb2y | implSpec/revSpec.Substrate unwired | Integration (T3) | TestIntegration_SubstrateWiring_NonNil |
| hk-yjduq | revWatcher.Done() on nil | Integration (T3) | same as hk-2hb2y with -race |
| hk-012af | perRunSubstrate pane target isolation | Scenario (T4) | SC-7 (hk-04azt) + SC-2 (hk-92v9m) |

Pattern: The first 3 bugs are composition-root wiring bugs → integration tier. The hk-012af bug is a runtime isolation bug → scenario tier.

### Q3: testing-gap label status

No beads currently have label `testing-gap` (verified via `br list --label testing-gap --status open` → 0 results).

The `testing-gap` label is NOT currently defined in the bead ledger as a first-class label. It is a convention to be established. T8 creates the first beads with this label, which effectively defines it.

hk-p3diy children (hk-x3s1p, hk-92v9m, etc.) are labeled `test-infra` and `twin`, not `testing-gap`. The distinction: `test-infra` = infrastructure work; `testing-gap` = gap in coverage that allowed a bug to reach production.

### Q4: Integration-tier equivalents to file in T8

From T3, the integration tests that T8 should reference:
  1. hk-37zy8 class → file a `testing-gap` bead for integration tier: "missing TestIntegration_ for policy goroutine subscription"
  2. hk-2hb2y class → file a `testing-gap` bead: "missing TestIntegration_ for substrate wiring"
  3. hk-yjduq class → file a `testing-gap` bead: "missing TestIntegration_ for nil watcher race"

These beads blocked by T3 (need T3 to exist first so the bead can reference the test that covers it).

---

## Options and Tradeoffs

**testing-friction-mining.md location:**

`docs/testing-friction-mining.md` is the right location per T8 requirement. Alternative (`docs/foundation/project-level/testing.md` section) would make testing.md very long. Standalone doc is correct.

**When to file the testing-gap beads:**

T8 scope: file ≥1 testing-gap bead. Recommend filing all 3 integration-tier gaps at T8 dispatch time. They're small beads, and batching them in T8 is cleaner than deferring.

---

## Risks and Unknowns

1. T8 depends on T3 and T4 — the examples need to reference actual test files that catch the bugs. T8 cannot be finalized until T3 tests are at least planned.
2. The `testing-gap` label may conflict with existing label conventions. T8 bead should check `br list` label namespace before creating.
