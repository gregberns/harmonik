# AR Pilot Coverage Review (r1) — 2026-04-27

**Summary:** AR pilot v0.1.0 covers all 50 active §4 requirements, all 4 active §5 invariants, the single §6 schema, and correctly omits beads for the 7 retired IDs (3 §4: AR-008/AR-037/AR-046; 4 §5: AR-INV-002/-004/-005/-006). Tally arithmetic reconciles. Spec-version reference matches source. One MINOR self-corrected narrative miscount; no BLOCKER or MAJOR coverage findings.

---

## Counts cross-check

Source-spec actual counts vs pilot §1 stated counts.

| Class | Source actual | Pilot §1 stated | Match? |
|---|---|---|---|
| §4 active requirements | 50 (AR-001..007, 009..036, 038..045, 047..053) | 50 | ✓ |
| §4 retired requirements (row present) | 2 (AR-037, AR-046) | 2 | ✓ |
| §4 retired requirements (no row) | 1 (AR-008) | 1 | ✓ |
| §5 active invariants | 4 (AR-INV-001, -003, -007, -008) | 4 | ✓ |
| §5 retired invariants | 4 (AR-INV-002, -004, -005, -006) | 4 | ✓ |
| §6 schemas (RECORD/INTERFACE/ENUM/regex) | 1 (`agent_type` regex in §6.1) | 1 | ✓ |
| §8 error taxonomy entries | 0 (no §8 section) | 0 | ✓ |
| §4 multi-step protocol requirements | 0 (no `####` block contains numbered step list) | 0 | ✓ |

All counts in pilot §1 bullets match source. Source-spec front-matter version `0.3.1` (line 11) matches pilot's stated version `v0.3.1` (§1, line 9).

Section enumeration: §4.0 (AR-052/AR-053), §4.1 (AR-001..AR-004), §4.2 (AR-005..AR-007 + retired AR-008 no-row), §4.3 (AR-009..AR-012), §4.4 (AR-013..AR-015), §4.5 (AR-016..AR-019), §4.6 (AR-020..AR-023), §4.7 (AR-024..AR-031), §4.8 (AR-032..AR-036), §4.9 (AR-037 retired-with-row, AR-038..AR-045), §4.10 (AR-046 retired-with-row, AR-047..AR-051). 4+3+4+3+4+4+8+5+8+5+2 = 50 active. ✓

---

## Missed IDs

**None.** Every active ID enumerated in steps 1–4 of §3.1 has a corresponding pilot bead:

- §4 reqs (50): every `AR-NNN` in `{001..007, 009..036, 038..045, 047..053}` appears as a pilot row in §2 with bead-id `ar-NNN`. (Verified by `grep -oE "ar-[0-9]+" ar-pilot.md | sort -u` against the 50-element source set.)
- §5 invariants (4): `ar-inv-001`, `ar-inv-003`, `ar-inv-007`, `ar-inv-008` all appear as rows in pilot §3.
- §6 schemas (1): `ar-schema.agent-type-identifier` appears as a row in pilot §4.
- §8 errors: N/A (source has no §8).
- Retired IDs (7): all 7 are enumerated in the spec-parent description (`§1` block at line 26) per discipline §2.11(b): "AR-008 [v0.2.0], AR-037 → AR-INV-007 [v0.3.0], AR-046 → AR-INV-008 [v0.3.0], AR-INV-002, AR-INV-004, AR-INV-005, AR-INV-006 [all v0.2.0]." ✓

---

## Phantom IDs

**None.** Every pilot bead-id in §2/§3/§4 corresponds to a real ID in the source spec. Specifically:

- All 50 `ar-NNN` mnemonics map to real `AR-NNN` headings in `specs/architecture.md` §4.
- All 4 `ar-inv-NNN` mnemonics map to real `AR-INV-NNN` headings in §5.
- The single `ar-schema.agent-type-identifier` corresponds to the real §6.1 declaration.

No typos, no fabricated IDs.

---

## Tally arithmetic

Pilot §7 tally (line 184–195):

| Class | Stated count | Actual rows in pilot |
|---|---|---|
| Spec parent (`ar`) | 1 | 1 (named in §1, line 22) |
| Requirement beads | 50 | 50 (rows in §2) |
| Step beads | 0 | 0 |
| Sensor / invariant beads | 4 | 4 (rows in §3) |
| Schema beads | 1 | 1 (row in §4) |
| Error-taxonomy beads | 0 | 0 |
| Test-infrastructure beads | 0 | 0 |
| **Total** | **56** | **56** |

Arithmetic: 1 + 50 + 0 + 4 + 1 + 0 + 0 = **56**. ✓

Pilot §2 footer (line 94) states "Total req beads = 50" — matches the §7 line. Pilot §3 footer (line 111) states "Sensor-bead count: 4" — matches. Pilot §4 footer (line 125) states "Schema + error-taxonomy bead count: 1" — matches.

All section subtotals reconcile to the §7 total.

---

## Spec-version cite

- **Source front matter** (line 11): `version: 0.3.1`, `last-updated: 2026-04-24`, `status: reviewed`.
- **Pilot §1** (line 9): "`specs/architecture.md` (AR), v0.3.1, status `reviewed`. (v0.3.1 is the current front-matter version; v0.3.1 was a citation-drift cleanup pass over v0.3.0's invariant promotions and §4.0 envelope-slot rework.)"

**Match.** No stale-version flag.

---

## Findings list

### BLOCKER findings

**No BLOCKER findings.** All 50 active §4 reqs, 4 active §5 invariants, 1 §6 schema, and 0 §8 errors are accounted for in the pilot.

### MAJOR findings

**No MAJOR findings.** All count statements in pilot §1 bullets match source-spec actual counts. Tally arithmetic in §7 reconciles. Spec-version reference is current.

### MINOR findings

**Finding M1 — self-corrected retired-ID count in narrative prose.**

- **Severity:** MINOR.
- **Lane:** `local`. The individual bullet-counts in pilot §1 ("3 retired §4 IDs" implicit in two enumerations, "4 retired §5 IDs" explicit) are each correct; only the summary prose at the trailing bullet undercounts then self-corrects in-line. This is an application-level prose oversight, not a discipline-rule gap — the discipline does not prescribe a summary-prose format for retired-ID totals.
- **Citation:** ar-pilot.md §1, line 17 (within the Counts bullet block):
  > "**5 retired IDs total** across §4 + §5 (3 reqs: AR-008, AR-037, AR-046; plus 4 invariants: AR-INV-002, AR-INV-004, AR-INV-005, AR-INV-006 — wait, 3 + 4 = 7 retired IDs across §4 and §5 combined; the spec-parent description enumerates all seven)."
- **Suggested fix:** Replace the leading "**5 retired IDs total**" with "**7 retired IDs total**" and remove the "— wait, 3 + 4 = 7 ..." mid-sentence correction; the sentence then reads cleanly as "7 retired IDs total across §4 + §5 (3 reqs: AR-008, AR-037, AR-046; plus 4 invariants: AR-INV-002, AR-INV-004, AR-INV-005, AR-INV-006); the spec-parent description enumerates all seven."

---

## Method-step coverage statement

- Step 1 (enumerate §4 IDs): done — 50 active + 2 retired-with-row + 1 retired-no-row = 53 IDs touched.
- Step 2 (enumerate §5 invariants): done — 4 active + 4 retired = 8 IDs touched.
- Step 3 (enumerate §6 records/interfaces/enums): done — 0 of those constructs; 1 regex declaration in §6.1.
- Step 4 (enumerate §8 errors): done — §8 absent.
- Step 5 (enumerate retired IDs): done — 7 total enumerated above.
- Step 6 (verify pilot accounts for each): done — see Missed IDs section.
- Step 7 (verify §1 Counts): done — see Counts cross-check section.
- Step 8 (verify §7 tally arithmetic): done — see Tally arithmetic section.
- Step 9 (verify §1 spec-version cite): done — see Spec-version cite section.
