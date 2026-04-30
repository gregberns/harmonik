# AR Pilot Coverage Review (r2) — 2026-04-27

**Summary:** AR pilot v0.2.1 cleanly covers all 50 active §4 requirements (49 first-class beads + AR-035 collapsed to a notes-line on `ar-026` with co-tag `req:AR-035` per discipline §2.1a), all 4 active §5 invariants, the single §6 primitive-shape schema, and correctly omits beads for the 7 retired IDs (3 §4: AR-008/AR-037/AR-046; 4 §5: AR-INV-002/-004/-005/-006). §1 Counts state 49 active first-class req beads. §7 tally arithmetic resolves to 55 (1 + 49 + 0 + 4 + 1 + 0 + 0). Spec-version reference v0.3.1 matches source. **No BLOCKER, no MAJOR, no MINOR findings.**

The r1 MINOR finding M1 (narrative miscount of "5 retired IDs total") is resolved in v0.2.0+ — pilot §1 line 17 now correctly states "**7 retired IDs total**" with no mid-sentence self-correction.

---

## Counts cross-check

Source-spec actual counts vs pilot §1 stated counts (v0.2.1).

| Class | Source actual | Pilot §1 stated (v0.2.1) | Match? |
|---|---|---|---|
| §4 active requirements (header IDs) | 50 (AR-001..007, 009..036, 038..045, 047..053) | 50 active §4 normative reqs | ✓ |
| §4 first-class req beads (after AR-035 §2.1a collapse) | 49 | 49 | ✓ |
| §4 retired requirements (row present in source) | 2 (AR-037, AR-046) | 2 enumerated | ✓ |
| §4 retired requirements (no row in source body) | 1 (AR-008) | 1 enumerated | ✓ |
| §5 active invariants | 4 (AR-INV-001, -003, -007, -008) | 4 | ✓ |
| §5 retired invariants | 4 (AR-INV-002, -004, -005, -006) | 4 | ✓ |
| §6 schemas (RECORD/INTERFACE/ENUM/constrained-primitive) | 1 (`agent_type` regex in §6.1) | 1 (now `kind:primitive-shape`) | ✓ |
| §8 error taxonomy entries | 0 (no §8 section) | 0 | ✓ |
| §4 multi-step protocol requirements | 0 | 0 | ✓ |

All counts in pilot §1 bullets match source. Source-spec front-matter version `0.3.1` (line 11) matches pilot's stated version `v0.3.1` (§1, line 9).

Source §4 active-ID enumeration: §4.0 (AR-052/AR-053), §4.1 (AR-001..004), §4.2 (AR-005..007 + retired AR-008 no-row), §4.3 (AR-009..012), §4.4 (AR-013..015), §4.5 (AR-016..019), §4.6 (AR-020..023), §4.7 (AR-024..031), §4.8 (AR-032..036), §4.9 (AR-037 retired-with-row, AR-038..045), §4.10 (AR-046 retired-with-row, AR-047..051). 4+3+4+3+4+4+8+5+8+5+2 = 50 active. ✓

§4 first-class req-bead row count in pilot §2: verified by `grep -E '^\| ` + backtick + `ar-[0-9]+'` to be exactly **49** rows (AR-035 is NOT a row; only appears as a `req:AR-035` co-tag on the `ar-026` row at line 69 plus prose mentions in §1, §2 footer, §8, and revision history).

---

## Missed IDs

**None.** Every active ID enumerated in steps 1–4 of §3.1 has a corresponding pilot bead:

- §4 reqs (50 → 49 + 1 collapse): every `AR-NNN` in `{001..007, 009..034, 036, 038..045, 047..053}` appears as a first-class pilot row in §2 with bead-id `ar-NNN`. AR-035 is NOT a separate row — it is collapsed to a notes-line on `ar-026` per discipline §2.1a, with co-tag `req:AR-035` on the `ar-026` row (verified at pilot line 69: `mech, `req:AR-026`, `req:AR-035``). The notes column for `ar-026` reads "AR-035 (role-orthogonality cross-reference) collapsed here per discipline §2.1a — see source spec §4.8.AR-035." This is a clean §2.1a application.
- §5 invariants (4): `ar-inv-001`, `ar-inv-003`, `ar-inv-007`, `ar-inv-008` all appear as rows in pilot §3 (lines 105–108).
- §6 schemas (1): `ar-schema.agent-type-identifier` appears as a row in pilot §4 (line 122) tagged `kind:primitive-shape` per discipline v0.5 §2.6.
- §8 errors: N/A (source has no §8).
- Retired IDs (7): all 7 are enumerated in the spec-parent description (§1 block at line 26) per discipline §2.11(b): "AR-008 [v0.2.0], AR-037 → AR-INV-007 [v0.3.0], AR-046 → AR-INV-008 [v0.3.0], AR-INV-002, AR-INV-004, AR-INV-005, AR-INV-006 [all v0.2.0]."

---

## Phantom IDs

**None.** Every pilot bead-id in §2/§3/§4 corresponds to a real ID in the source spec:

- All 49 first-class `ar-NNN` mnemonics map to real `AR-NNN` headings in `specs/architecture.md` §4.
- All 4 `ar-inv-NNN` mnemonics map to real `AR-INV-NNN` headings in §5.
- The single `ar-schema.agent-type-identifier` corresponds to the real §6.1 declaration.
- No row references retired IDs (no `ar-008`, no `ar-035`, no `ar-037`, no `ar-046` first-class rows).
- No typos, no fabricated IDs.

---

## Tally arithmetic

Pilot §7 tally (lines 184–195):

| Class | Stated count | Actual rows in pilot |
|---|---|---|
| Spec parent (`ar`) | 1 | 1 (named in §1, line 22) |
| Requirement beads (1 §2.1a collapse: AR-035 → notes on `ar-026`) | 49 | 49 (rows in §2; verified by row count) |
| Step beads | 0 | 0 |
| Sensor / invariant beads | 4 | 4 (rows in §3) |
| Schema beads (1 `kind:primitive-shape`) | 1 | 1 (row in §4) |
| Error-taxonomy beads | 0 | 0 |
| Test-infrastructure beads | 0 | 0 |
| **Total** | **55** | **55** |

Per-subtotal verification:

- 1 (spec parent) + 49 (req) + 0 (step) + 4 (sensor) + 1 (schema) + 0 (error-tax) + 0 (test-infra) = **55**. ✓
- Pilot §2 footer (line 93) states "Total req beads = 49." — matches.
- Pilot §3 footer (line 110) states "Sensor-bead count: 4." — matches.
- Pilot §4 footer (line 124) states "Schema + error-taxonomy bead count: 1 (1 schema + 0 error taxonomy)." — matches.
- Pilot §6 footer (line 176) states "Test-infra bead count: 0." — matches.
- Pilot §7 arithmetic-check line (line 195) explicitly states "1 + 49 + 0 + 4 + 1 + 0 + 0 = **55**. ✓ (v0.1.0 reported 56; v0.2.0 drops 1 from AR-035 collapsing to a notes line on `ar-026` per discipline §2.1a.)" — matches.

All section subtotals reconcile to the §7 total of 55.

---

## Spec-version cite

- **Source front matter** (line 11): `version: 0.3.1`, `last-updated: 2026-04-24`, `status: reviewed`.
- **Pilot §1** (line 9): "`specs/architecture.md` (AR), v0.3.1, status `reviewed`. (v0.3.1 is the current front-matter version; v0.3.1 was a citation-drift cleanup pass over v0.3.0's invariant promotions and §4.0 envelope-slot rework.)"

**Match.** No stale-version flag.

---

## Findings list

### BLOCKER findings

**No BLOCKER findings.** All 50 active §4 reqs (49 first-class + AR-035 collapse), 4 active §5 invariants, 1 §6 schema, and 0 §8 errors are accounted for in the pilot.

### MAJOR findings

**No MAJOR findings.** All count statements in pilot §1 bullets match source-spec actual counts. §7 tally arithmetic reconciles. §2/§3/§4 footer subtotals reconcile to the §7 total. Spec-version reference is current.

### MINOR findings

**No MINOR findings.** The r1 narrative-miscount finding (M1, "5 retired IDs total") is resolved in v0.2.0+ — pilot §1 line 17 now correctly reads "**7 retired IDs total** across §4 + §5 (3 reqs: AR-008, AR-037, AR-046; plus 4 invariants: AR-INV-002, AR-INV-004, AR-INV-005, AR-INV-006); the spec-parent description enumerates all seven." No mid-sentence self-correction remains.

---

## Delta from r1

What's now clean:
- **AR-035 collapse (v0.2.0).** AR-035 is no longer a first-class bead row; it is collapsed to a notes-line on `ar-026` with co-tag `req:AR-035` per discipline v0.5 §2.1a. The §1 Counts now correctly state 49 active first-class req beads (down from 50 in v0.1.0).
- **r1 M1 prose miscount (v0.2.0).** The "5 retired IDs total" narrative oversight in v0.1.0 §1 is fixed; v0.2.1 reads "7 retired IDs total" with the mid-sentence self-correction removed.
- **Tally arithmetic (v0.2.0).** §7 total is now 55 (was 56 in v0.1.0); arithmetic 1 + 49 + 0 + 4 + 1 + 0 + 0 = 55 reconciles cleanly with all section footers.
- **`ar-schema.agent-type-identifier` retag (v0.2.0).** Now tagged `kind:primitive-shape` per discipline v0.5 §2.6 (was `kind:schema` in v0.1.0). Coverage-class-only check; tag form is correct for a constrained-primitive declaration.

What was never flagged in r1 and remains clean:
- All 50 source §4 active IDs accounted for; no missed IDs.
- All 4 source §5 active invariants accounted for; no missed invariants.
- 1 §6 schema accounted for.
- 7 retired IDs correctly omitted as beads and enumerated in spec-parent description per discipline §2.11(b).
- Spec-version cite v0.3.1 still matches source front matter.
- No phantom IDs (no rows reference retired AR-008/AR-037/AR-046 or the now-collapsed AR-035 as a bead-id).

What's still flagged:
- Nothing. Coverage lane is clean for v0.2.1.

(Cross-spec edge findings — including F-pilot-AR-9's AR-011↔AR-029 cycle that was uncaught in r1 and surfaced + fixed in v0.2.1 — are owned by the Reference reviewer, not the Coverage reviewer. Coverage's remit per protocol §3.1 is requirement/invariant/schema accounting, count cross-checks, tally arithmetic, and spec-version cite.)

---

## Method-step coverage statement

- Step 1 (enumerate §4 IDs): done — 50 active + 2 retired-with-row + 1 retired-no-row = 53 IDs touched.
- Step 2 (enumerate §5 invariants): done — 4 active + 4 retired = 8 IDs touched.
- Step 3 (enumerate §6 records/interfaces/enums): done — 0 of those constructs; 1 constrained-primitive declaration in §6.1.
- Step 4 (enumerate §8 errors): done — §8 absent.
- Step 5 (enumerate retired IDs): done — 7 total.
- Step 6 (verify pilot accounts for each, including AR-035 collapse): done — see Missed IDs section. AR-035 verified collapsed to a notes-line on `ar-026` (pilot line 69) with `req:AR-035` co-tag; no `ar-035` first-class row exists.
- Step 7 (verify §1 Counts state 49 active first-class req beads): done — pilot §1 line 11 states "49 active §4 first-class req beads"; matches actual 49 §2 rows.
- Step 8 (verify §7 tally arithmetic adds to 55 with all subtotals correct): done — 1 + 49 + 0 + 4 + 1 + 0 + 0 = 55, every subtotal verified against section footers.
- Step 9 (verify §1 spec-version cite v0.3.1): done — see Spec-version cite section.
