# EM Pilot Review (r2) — Synthesis

Date: 2026-04-28. Pilot under review: `docs/decompose-to-tasks/em-pilot.md` v0.2.0. Discipline at time of review: `discipline.md` v0.7. Protocol: `pilot-review-protocol.md` v0.2.

Reviewers (parallel, completed):
- Coverage — `coverage-r2.md` (0/0/0).
- Decomposition-quality — `decomposition-r2.md` (0/0/1 MINOR class).
- Reference — `references-r2.md` (0/0/0).

## 1. Outcome

**0 BLOCKER, 0 MAJOR, 1 MINOR `class`.**

All r1 BLOCKERs/MAJORs and pilot-lane fixes verify CLEAN in v0.2.0:
- F-em-r1-MAJ-1 (invariant-body term-use) — em-inv-005 + em-inv-001 new edges valid per v0.7 §3.1 step 5 source (4).
- F-em-r1-MAJ-2 (sensor-predecessor degeneracy) — `gated-by-corpus-scale` tag correctly applied to em-inv-005.
- F-em-r1-MAJ-3 (type-alias-MVH-redundant) — `em-schema.verdict-payload` correctly NOT minted; tally stable at 89.
- F-em-r1-MAJ-4 (rule precedence) — em-040 row note now leads with F-pilot-AR-r2-2; supporting-cite parallel.
- F-em-r1-MIN-3/5/6/7/8/9 — all closed by v0.7 documentation patches.
- F-em-r1-MIN-4 (§5 tally) + F-em-r1-MIN-10 (em-046 ar-032 edge) — applied at re-draft.

One new MINOR class finding from decomposition-r2 §7.1:

| # | Finding | Severity | Lane | Discipline § |
|---|---|---|---|---|
| F-em-r2-1 | v0.7 §2.5 sensor-predecessor degeneracy clause's `gated-by-corpus-scale` trigger literally describes the strict-empty case, but the pilot's em-inv-005 application is compositional ("in-corpus portion resolved + cross-spec portion deferred"). The pilot's reading is operationally defensible (the sensor's verification scope is genuinely incomplete until corpus-scale resolution) but the discipline text has a wording ambiguity for invariants that have BOTH in-corpus AND cross-spec cite components. | MINOR | `class` | §2.5 |

## 2. Lane decision

F-em-r2-1 routes to discipline-patch lane per protocol §4.1's class-bias rule.

Apply override criterion (synthesis-r2.md §4 from AR cycle):

- **Would v0.8 (with disambiguating clause) change EM's bead set?** No. The pilot's compositional reading already produces the correct outcome (em-inv-005 has 2 in-corpus predecessors + `gated-by-corpus-scale` tag for residual cross-spec deferrals). A v0.8 wording fix would codify the pilot's interpretation; the bead set is invariant under the patch.
- **Would the bug propagate if EM loads now?** No. EM's beads are correct under either reading.
- **Would the bug propagate if EV/HC/CP/RC/WM/PL/ON pilots draft against v0.7?** Possibly — RC and BI invariants with the same compositional shape may apply the rule inconsistently. But this is a MINOR class finding, and §4.2 says MINOR class findings "may be batched into the next discipline patch rather than triggering one."

**Decision: BATCH F-em-r2-1 with the next non-trivial discipline patch.** Load EM v0.2.0 now. Future pilots that surface invariant-body-with-mixed-in-corpus-and-cross-spec-cites cases will accumulate as evidence; if a class genuinely surfaces, v0.8 codifies. If they don't, the wording ambiguity remains as a noted-but-not-blocking discipline silence.

This override is recorded explicitly per protocol §4.1.

## 3. Sequence

1. **Load `em-pilot.md` v0.2.0 into Beads** under prefix `hk` (existing workspace already holds BI's 66 beads under epic `hk-872` and AR's 55 children under `hk-zs0`). Verify `br dep cycles` clean across union (BI ∪ AR ∪ EM).
2. **Maintain mnemonic-to-assigned-ID map** at `/tmp/em-mnem-map.csv` per discipline §2.10.
3. **Continue with EV → HC → CP → WM → PL → ON → RC pilots** in order. Checkpoint #2 planned after EV.

## 4. Why this is the right call

The protocol's override criterion was designed for exactly this situation: a class finding whose codification would not change the just-drafted pilot's bead set. F-em-r2-1 is documentation-tightening at the discipline level, not a structural concern.

EM v0.2.0 is structurally clean across all three reviewer dimensions. The 6 cross-spec AR edges are mechanically derivable. The 5 intra-spec sensor predecessor additions (em-inv-005 ×2, em-inv-001 ×2) align with v0.7 §3.1 step 5 invariant-body sub-clause. The four pilot-lane fixes from r1 are applied. No new structural concerns surfaced.

Load proceeds.
