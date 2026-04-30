# EM Pilot Coverage Review (r1) — 2026-04-27

**Summary:** EM pilot v0.1.0 covers all 66 active §4 requirements (62 first-class beads after 4 §2.3 coalesces, with each coalesced ID — EM-005a, EM-023a, EM-041a, EM-043a — explicitly named in its respective umbrella row's notes), all 3 active §5 invariants, all 20 §6 schema constructs (6 typed-ID aliases + 8 RECORDs + 5 ENUMs + 1 §6.2 trailer-format declaration), and the single §8 error-taxonomy table (consolidated into one bead, below the §2.11(c) 11+ threshold). All 3 retired §5 invariant IDs (EM-INV-002/-003/-006) are enumerated in the spec-parent description per discipline §2.11(b). Tally arithmetic reconciles. Spec-version reference matches source front matter (v0.3.3). No BLOCKER findings, no MAJOR findings, two MINOR findings (both `class`-tagged per §4.1 over-flagging bias).

---

## Counts cross-check

Source-spec actual counts vs pilot §1 stated counts.

| Class | Source actual | Pilot §1 stated | Match? |
|---|---|---|---|
| §4 active requirements (all `#### EM-NNN[a-z]?` headings) | 66 | 66 | ✓ |
| §4 first-class beads (after coalesces) | 62 (66 − 4 coalesces) | 62 | ✓ |
| §4 retired requirements | 0 | 0 | ✓ |
| §5 active invariants | 3 (EM-INV-001, -004, -005) | 3 | ✓ |
| §5 retired invariants | 3 (EM-INV-002, -003, -006) | 3 | ✓ |
| §6.1 typed-ID aliases | 6 (RunID, StateID, TransitionID, NodeID, BeadID, CommitRange) | 6 | ✓ |
| §6.1 RECORD declarations | 8 (Workflow, Node, Edge, Run, State, Transition, Checkpoint, Outcome) | 8 | ✓ |
| §6.1 ENUM declarations | 5 (NodeType, IdempotencyClass, TransitionKind, OutcomeStatus, OutcomeKind) | 5 | ✓ |
| §6.2 trailer-format declaration | 1 | 1 | ✓ |
| §6 schema constructs (total) | 20 | 20 | ✓ |
| §8 error-taxonomy entries | 6 classes (8.1–8.6) — single taxonomy bead per §2.11(c) <11 threshold | "1 §8 error-taxonomy table (6 failure classes)" | ✓ |
| §2.3 coalesces | 4 (EM-005+EM-005a; EM-023+EM-023a; EM-041+EM-041a; EM-043+EM-043a) | 4 | ✓ |
| §11 OQs | 7 (OQ-EM-001..OQ-EM-007; OQ-EM-007 resolved-but-retained per IDs-do-not-reuse) | 7 | ✓ |
| §4 multi-step protocol requirements (passing §2.2 three-signal test + F8b non-shared-body) | 0 (per pilot's §2.2/F8b reasoning; see F-pilot-EM-1) | 0 | ✓ |

**Section enumeration:** §4.1 (EM-001..EM-005a), §4.2 (EM-006..EM-011), §4.3 (EM-012..EM-015c), §4.4 (EM-016..EM-022), §4.5 (EM-023..EM-026), §4.6 (EM-027..EM-030), §4.7 (EM-031..EM-033), §4.8 (EM-034..EM-037), §4.9 (EM-038..EM-040), §4.10 (EM-041..EM-046b). Tally by subsection:

- §4.1: EM-001, EM-002, EM-003, EM-004, EM-005, EM-005a = 6
- §4.2: EM-006, EM-007, EM-008, EM-009, EM-010, EM-011 = 6
- §4.3: EM-012, EM-013, EM-014, EM-015, EM-015a, EM-015b, EM-015c = 7
- §4.4: EM-016, EM-017, EM-017a, EM-018, EM-018a, EM-019, EM-020, EM-020a, EM-021, EM-022 = 10
- §4.5: EM-023, EM-023a, EM-024, EM-024a, EM-025, EM-025a, EM-026 = 7
- §4.6: EM-027, EM-028, EM-029, EM-030 = 4
- §4.7: EM-031, EM-031a, EM-032, EM-033 = 4
- §4.8: EM-034, EM-034a, EM-034b, EM-034c, EM-035, EM-036, EM-036a, EM-037 = 8
- §4.9: EM-038, EM-039, EM-040 = 3
- §4.10: EM-041, EM-041a, EM-042, EM-042a, EM-043, EM-043a, EM-044, EM-045, EM-046, EM-046a, EM-046b = 11

**Subsection sum:** 6+6+7+10+7+4+4+8+3+11 = **66**. ✓ (Matches `grep -cE "^#### EM-[0-9]+[a-z]?" specs/execution-model.md` = 66.)

All counts in pilot §1 bullets match source.

---

## Missed IDs

**None.** Every active ID enumerated in §3.1 steps 1–4 has a corresponding pilot account:

### §4 reqs (66 active, 62 first-class beads + 4 coalesce names)

Verified by set-diff `comm -23 spec_ids pilot_first_class_rows`:

- 62 first-class rows in pilot §2 (mnemonics `em-001` … `em-046b`, excluding the 4 coalesced sub-IDs) cover 62 of 66 source IDs directly.
- 4 coalesced IDs (EM-005a, EM-023a, EM-041a, EM-043a) are each named explicitly in their umbrella row's `req:` tag list AND notes column per discipline §2.3:
  - **EM-005a** — named in `em-005`'s notes ("EM-005 + EM-005a (coalesced per §2.3)") and tag list (`req:EM-005`, `req:EM-005a`). Pilot description enumerates the new `kind` discriminator + `payload` envelope.
  - **EM-023a** — named in `em-023`'s notes ("EM-023 + EM-023a (coalesced per §2.3)") and tag list (`req:EM-023`, `req:EM-023a`). Pilot description carries the durability decision table (`transition_kind` × `outcome.status`) and the synthesized-Outcome rule.
  - **EM-041a** — named in `em-041`'s notes ("EM-041 + EM-041a (coalesced per §2.3)") and tag list (`req:EM-041`, `req:EM-041a`). Pilot description includes the pre-cascade context-update ordering.
  - **EM-043a** — named in `em-043`'s notes ("EM-043 + EM-043a (coalesced per §2.3)") and tag list (`req:EM-043`, `req:EM-043a`). Pilot description includes the per-`(run_id, edge)` counter storage locus and the git-derived authority on restart.

### §5 invariants (3 active)

- `em-inv-001` (sensor: git is the state-reconstruction source) — row in pilot §3.
- `em-inv-004` (sensor: no subsystem implements workflow-level transactionality) — row in pilot §3.
- `em-inv-005` (sensor: git wins on completion disagreement) — row in pilot §3 (with Finding M1 below noting the empty intra-EM predecessor list, which is a sensor-degeneracy disclosure, NOT a missed-ID).

### §6 schemas (20)

All 20 schema beads present in pilot §4:

- 6 typed-ID aliases: `em-schema.run-id`, `em-schema.state-id`, `em-schema.transition-id`, `em-schema.node-id`, `em-schema.bead-id`, `em-schema.commit-range`. ✓
- 8 RECORDs: `em-schema.workflow`, `em-schema.node`, `em-schema.edge`, `em-schema.run`, `em-schema.state`, `em-schema.transition`, `em-schema.checkpoint`, `em-schema.outcome`. ✓
- 5 ENUMs: `em-schema.node-type`, `em-schema.idempotency-class`, `em-schema.transition-kind`, `em-schema.outcome-status`, `em-schema.outcome-kind`. ✓
- 1 trailer-format declaration: `em-schema.checkpoint-trailers` (covers all 7 §6.2 trailer rows including the two RC-owned rows added in v0.3.3 — see Finding M2 below for a dual-ownership disclosure that is a discipline-author concern, not a missed-ID). ✓

### §8 error-taxonomy

`em-error.taxonomy` row in pilot §4 covers all 6 failure classes (transient, structural, deterministic, canceled, budget_exhausted, compilation_loop). The pilot correctly applies §2.11(c)'s 11+-categories threshold to consolidate into a single bead (BI's `bi-error.taxonomy` precedent). ✓

### Retired IDs (3 §5)

All 3 retired §5 invariants enumerated in pilot's spec-parent description (§1, line 26 of pilot) AND pilot §3 footer (line 124): "EM-INV-002, EM-INV-003, EM-INV-006 (all retired at v0.2.0 as duplicates of §4 reqs per template selection test)." Per discipline §2.11(b), retired IDs are named in spec-parent description; the pilot satisfies this via two redundant locations. ✓

No retired §4 IDs (spec's §4 ID space has been stable since v0.2.0; the v0.3.0 `Status draft → reviewed` revision-history note explicitly states "no IDs retired in v0.3"). Pilot's claim "Zero retired §4 IDs" is correct. ✓

---

## Phantom IDs

**None.** Every pilot bead-id in §2/§3/§4 corresponds to a real ID in the source spec.

Verified by set-diff `comm -13 spec_ids pilot_first_class_rows` returning empty:

- All 62 `em-NNN` mnemonics map to real `EM-NNN` headings in `specs/execution-model.md` §4.
- All 3 `em-inv-NNN` mnemonics map to real `EM-INV-NNN` headings in §5.
- All 20 `em-schema.*` mnemonics map to real §6.1 / §6.2 declarations.
- The single `em-error.taxonomy` mnemonic corresponds to the real §8 6-class taxonomy.

No typos, no fabricated IDs, no IDs that exist in the pilot but not in the spec.

---

## Tally arithmetic

Pilot §7 tally (lines 249–260):

| Class | Stated count | Actual rows in pilot |
|---|---|---|
| Spec parent (`em`) | 1 | 1 (named in §1, line 22 of pilot) |
| Requirement beads | 62 | 62 (verified by `grep -oE "^\| \`em-[0-9]+[a-z]?\`" em-pilot.md \| sort -u \| wc -l` = 62) |
| Step beads | 0 | 0 |
| Sensor / invariant beads | 3 | 3 (rows in pilot §3) |
| Schema beads | 20 | 20 (rows in pilot §4 with `kind:primitive-shape`/`kind:schema`/`kind:enum`) |
| Error-taxonomy beads | 1 | 1 (`em-error.taxonomy` in pilot §4) |
| Test-infrastructure beads | 2 | 2 (rows in pilot §6: `em-test.crash-recovery-harness`, `em-test.validator-fixture`) |
| **Total** | **89** | **89** |

**Arithmetic:** 1 + 62 + 0 + 3 + 20 + 1 + 2 = **89**. ✓ (Pilot's own arithmetic check on line 260 also yields 89.)

**Section subtotals reconcile:**

- Pilot §2 footer (line 106): "Total req beads = 62." — matches §7. ✓
- Pilot §3 footer (line 122): "Sensor-bead count: 3." — matches §7. ✓
- Pilot §4 footer (line 156): "Schema + error-taxonomy bead count: 21 (20 schemas + 1 taxonomy)." — matches §7 (20 + 1 = 21). ✓
- Pilot §6 footer (line 241): "Test-infra bead count: 2." — matches §7. ✓

The §7 BI/AR/EM comparison table (lines 264–273) is internally consistent: BI=66, AR=55, EM=89. EM total again confirmed as 89 in the comparison. ✓

All section subtotals reconcile to the §7 total.

---

## Spec-version cite

- **Source front matter** (line 11 of `specs/execution-model.md`): `version: 0.3.3`, `last-updated: 2026-04-25`, `status: reviewed`.
- **Pilot §1** (lines 3, 9 of `docs/decompose-to-tasks/em-pilot.md`): "drafted 2026-04-27 against `specs/execution-model.md` v0.3.3 (status `reviewed`) … (v0.3.2 → v0.3.3 added EM-005a OutcomeKind discriminator + payload envelope, and added the two RC-owned trailers `Harmonik-Workflow-Class` and `Harmonik-Target-Run-ID` to §6.2)."

**Match.** No stale-version flag. The pilot's narrative also accurately characterizes the v0.3.2 → v0.3.3 delta (EM-005a addition + two new trailer rows), which is the most recent revision-history entry on line 1095 of the spec.

**Discipline-version cite cross-check:** pilot states "discipline v0.6 (post-AR pilot r1+r2 + F-pilot-AR-10 patch)"; `docs/decompose-to-tasks/discipline.md` line 3 confirms `discipline-version: 0.6`. ✓

---

## Findings list

### BLOCKER findings

**No BLOCKER findings.** All 66 active §4 reqs (62 first-class beads + 4 coalesce names), 3 active §5 invariants, 20 §6 schemas, and 1 §8 error-taxonomy are accounted for in the pilot. No phantom IDs. No missing coalesce-comment names.

### MAJOR findings

**No MAJOR findings.** All count statements in pilot §1 bullets match source-spec actual counts. Tally arithmetic in §7 reconciles. Spec-version reference is current. Section-subsection enumeration sums correctly to the source's 66 active §4 IDs.

### MINOR findings

**Finding M1 — em-inv-005 has empty intra-EM predecessor list (sensor-edge degeneracy disclosure, not a coverage miss).**

- **Severity:** MINOR.
- **Lane:** `class`. The pilot already self-flags this as `F-pilot-EM-7` and tags it `class` in its own §8 (line 311), arguing discipline §2.5 may benefit from explicit guidance covering "if a sensor has zero predecessors after applying all three §10.2 sources, this is a smell — verify the invariant body has at least one in-spec cite, and surface as a structural finding if not." Per pilot-review-protocol §4.1 probe 4 (reviewer self-classification), the existing `class` tag from the pilot author routes the finding to the discipline-patch lane unless overruled in synthesis; no overrule is warranted here. Per probe 1 (generality), at least 2 of the remaining 8 specs (RC, BI, EV) have invariants whose enforcement is genuinely cross-spec and could similarly produce empty intra-spec predecessor lists.
- **Coverage relevance:** the pilot does account for EM-INV-005 (row exists in §3 with a populated description). The empty `blocks` edge column is a sensor-edge structural concern, NOT a missed-ID. The protocol §3.1 method does not require sensor predecessors to be non-empty; it only requires that every §5 invariant appear as a row in pilot §3, which EM-INV-005 does. Severity is MINOR because there is no coverage miss; the disclosure is forward-looking discipline-design feedback already captured in pilot §8.
- **Citation:** em-pilot.md §3, line 120 (em-inv-005 row notes); em-pilot.md §8, line 311 (F-pilot-EM-7).
- **Suggested action:** none from coverage reviewer; defer to synthesis.md per protocol §4.

**Finding M2 — `em-schema.checkpoint-trailers` covers two RC-owned trailer rows (dual-ownership disclosure, not a coverage miss).**

- **Severity:** MINOR.
- **Lane:** `class`. The pilot self-flags this as `F-pilot-EM-6` and tags it `class` in its own §8 (line 309), surfacing the question of whether discipline §2.11(d)'s co-owned-event-payload pattern extends to "co-owned trailer rows." Per protocol §4.1 probe 4 (reviewer self-classification), the existing `class` tag routes to the discipline-patch lane. Per probe 1 (generality), other registry-owning specs (RC owns the verdict-event registry; EV owns the event-payload registry) may declare rows whose owning-spec annotation differs from the registry owner — a recurring pattern worth codifying.
- **Coverage relevance:** the pilot does mint a single `em-schema.checkpoint-trailers` bead covering ALL 7 trailer rows (including the two RC-owned `Harmonik-Workflow-Class` and `Harmonik-Target-Run-ID`). The §6.2 spec text explicitly annotates the RC-owned rows ("Owning spec: reconciliation"), which raises a discipline-author question about whether the bead-level row should be split or remain consolidated. Severity is MINOR because the registry IS covered (no missed declaration); the disclosure is structural.
- **Citation:** em-pilot.md §4, line 153 (em-schema.checkpoint-trailers row notes); em-pilot.md §8, line 309 (F-pilot-EM-6).
- **Suggested action:** none from coverage reviewer; defer to synthesis.md per protocol §4.

---

## Method-step coverage statement

- **Step 1** (enumerate §4 IDs): done — 66 active, 0 retired = 66 IDs touched. Confirmed via `grep -cE "^#### EM-[0-9]+[a-z]?" specs/execution-model.md` = 66.
- **Step 2** (enumerate §5 invariants): done — 3 active + 3 retired = 6 IDs touched. Confirmed via `grep -cE "^#### EM-INV-[0-9]+" specs/execution-model.md` = 3, plus the §5 INFORMATIVE block enumerating retired EM-INV-002/-003/-006.
- **Step 3** (enumerate §6 records/interfaces/enums): done — 6 typed-ID aliases + 8 RECORDs + 5 ENUMs + 1 §6.2 trailer-format declaration = 20 schema constructs touched.
- **Step 4** (enumerate §8 errors): done — 6 failure classes (8.1–8.6) consolidated as one taxonomy bead per §2.11(c) <11 threshold.
- **Step 5** (enumerate retired IDs): done — 3 §5 invariant retirements (EM-INV-002/-003/-006); zero §4 retirements.
- **Step 6** (verify pilot accounts for each): done — see Missed IDs section. All 66 §4 reqs accounted for (62 direct + 4 named in coalesce comments per §2.3); all 3 §5 invariants accounted for (rows in §3); all 20 §6 schemas accounted for (rows in §4); the §8 taxonomy accounted for (single bead in §4 per §2.11(c)).
- **Step 7** (verify §1 Counts): done — see Counts cross-check section. Every numeric in pilot §1 bullets reconciles to source.
- **Step 8** (verify §7 tally arithmetic): done — see Tally arithmetic section. 1 + 62 + 0 + 3 + 20 + 1 + 2 = 89. ✓
- **Step 9** (verify §1 spec-version cite): done — see Spec-version cite section. Pilot v0.3.3 = source front-matter v0.3.3. ✓
