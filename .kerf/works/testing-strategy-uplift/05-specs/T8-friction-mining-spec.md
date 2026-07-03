# Change Spec: T8 — Friction-Mining Loop

**Component:** T8  
**Date:** 2026-05-20  
**Research:** 04-research/T8/findings.md

---

## Requirements (from 03-components.md)

1. Create `docs/testing-friction-mining.md` with: pattern definition, bead template, examples, cadence.
2. File ≥1 `testing-gap` bead using the template for the integration-tier gaps from T3.
3. Define `testing-gap` label in the bead ledger.

---

## Research Summary

- No beads currently have `testing-gap` label (label does not exist in ledger).
- 4 bugs map cleanly to integration tier: hk-37zy8, hk-2hb2y, hk-yjduq → T3; hk-012af → T4/scenario.
- T8 depends on T3 (needs concrete test examples) and T4 (needs scenario bead references).
- The checklist document is a lightweight doc; T8 is mostly documentation + bead filing.

---

## Approach

**Create docs/testing-friction-mining.md**

Sections:
1. **Pattern definition** (3 lines): "Runtime failure that unit tests passed → missing integration/scenario/property test → testing-gap bead."
2. **Tier mapping** (table): which class of production bug maps to which test tier.
3. **Bead template** (copy-pasteable):
   ```
   Title: testing-gap: <pkg>/<component> — <brief description of missing test>
   Type: task
   Labels: testing-gap, codename:testing-strategy-uplift (or successor work)
   Body:
     - Tier: [integration|scenario|property|crash]
     - Spec/behavior covered: <spec reference>
     - Test function name (if known): TestXxx_...
     - Bug this would have caught: <bead-id> — <one-line description>
   ```
4. **Worked examples** (the 4 bugs):
   - hk-37zy8 → Integration tier → TestIntegration_HandlerPausePolicyGoroutine_Subscribed (T3)
   - hk-2hb2y → Integration tier → TestIntegration_SubstrateWiring_Spy (T3)
   - hk-yjduq → Integration tier → same as hk-2hb2y with -race
   - hk-012af → Scenario tier → hk-92v9m (SC-2) + hk-04azt (SC-7)
5. **Cadence**: "After every session where a reviewer-APPROVED bead had a runtime bug that existing tests did not catch: file ≥1 testing-gap bead before session-end."

**File integration-tier testing-gap beads**

File 3 beads (using br create):
  1. `testing-gap: daemon/HandlerPausePolicyGoroutine_Subscribed — missing integration test` (catches hk-37zy8)
  2. `testing-gap: daemon/SubstrateWiring — missing integration test for implSpec/revSpec.Substrate` (catches hk-2hb2y, hk-yjduq)
  3. Note: hk-012af scenario-tier gap is already tracked via hk-p3diy children — no new bead needed.

These beads are the integration-tier equivalents of the hk-p3diy scenario beads.

**Define testing-gap label**

No explicit label registry exists in the project. Convention: the first `br create` with `--label testing-gap` implicitly defines it. Document in testing-friction-mining.md §"Label convention": "testing-gap is a project label for beads representing missing test coverage that allowed a production bug to pass review."

---

## Files & Changes

- `docs/testing-friction-mining.md` — new file
- No source code changes. Beads created via `br create`.

---

## Acceptance Criteria

1. `docs/testing-friction-mining.md` exists with all 5 sections.
2. ≥2 beads filed with label `testing-gap` (the integration-tier equivalents).
3. Each filed bead's body follows the template in the doc.
4. `br list --label testing-gap --status open` returns ≥2 results.

---

## Verification

```bash
ls docs/testing-friction-mining.md  # must exist
br list --label testing-gap --status open 2>&1 | wc -l  # must be ≥2
```

---

## Dependencies

- T3 (tests that the filed beads reference must be planned — not necessarily implemented)

---

## Bead Candidates

- T8: `T8: create docs/testing-friction-mining.md + file integration-tier testing-gap beads` (type: docs+task)
