# EM Pilot Coverage Review (r2) — 2026-04-28

**Summary:** EM pilot v0.2.0 (re-drafted 2026-04-28 against discipline v0.7) covers all 66 active §4 requirements (62 first-class beads after 4 §2.3 coalesces, with each coalesced ID — EM-005a, EM-023a, EM-041a, EM-043a — explicitly named in its respective umbrella row's notes), all 3 active §5 invariants, all 20 §6 schema constructs (6 typed-ID aliases + 8 RECORDs + 5 ENUMs + 1 §6.2 trailer-format declaration), and the single §8 error-taxonomy table (consolidated into one bead, below the §2.11(c) 11+ threshold). All 3 retired §5 invariant IDs (EM-INV-002/-003/-006) are enumerated in the spec-parent description per discipline §2.11(b). Tally arithmetic reconciles. Spec-version reference matches source front matter (v0.3.3). Discipline-version reference matches discipline v0.7. **All v0.1.0 findings resolved: §5 tally corrected (6 edges, not 4), em-040 row note rewritten with v0.7 precedence rule, em-046 row gains direct ar-032 edge, em-inv-005 row gains new predecessors with gated-by-corpus-scale tag, em-inv-001 row shows 6 predecessors (per v0.7 term-use sub-clause), em-005a note tightened.** No BLOCKER findings, no MAJOR findings, no MINOR findings. Clean pass.

---

## Counts cross-check

Source-spec actual counts vs pilot §1 stated counts. (These are identical to r1; re-verified for v0.2.0 consistency.)

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

**All counts in pilot §1 match source. No deltas from r1 in this table; counts unchanged in v0.2.0.**

---

## Missed IDs

**None.** Every active ID enumerated in §3.1 steps 1–4 has a corresponding pilot account.

### §4 reqs (66 active, 62 first-class beads + 4 coalesce names)

Verified by set-diff: 62 first-class rows in pilot §2 (mnemonics `em-001` … `em-046b`, excluding the 4 coalesced sub-IDs) cover 62 of 66 source IDs directly. 4 coalesced IDs (EM-005a, EM-023a, EM-041a, EM-043a) are each named explicitly in their umbrella row's `req:` tag list AND notes column per discipline §2.3 — all verified in v0.1.0, carried forward to v0.2.0 unchanged.

### §5 invariants (3 active)

- `em-inv-001` — row in pilot §3. **v0.2.0 change:** row now lists 6 predecessors (em-016, em-017, em-031, em-031a, em-032, em-033) instead of the r1 partial list. Per v0.7 §3.1 step 5 invariant-body term-use sub-clause, the two new direct edges `em-inv-001 → em-016` and `em-inv-001 → em-017` are emitted as explicit term-uses of "git checkpoint trail" (EM-016 owns the checkpoint concept; EM-017 owns the trailer registry that anchors the trail). ✓
- `em-inv-004` — row in pilot §3. Unchanged from r1.
- `em-inv-005` — row in pilot §3. **v0.2.0 change:** row now lists 2 new predecessors `em-schema.checkpoint-trailers` and `em-014` (term-use of `Harmonik-Bead-ID` trailer key + bead-tied-runs concept per v0.7 §3.1 step 5). Row also carries transient `gated-by-corpus-scale` tag per v0.7 §2.5 sensor-predecessor degeneracy rule (F-em-r1-MAJ-2) because body still has out-of-`depends-on` forward cites to RC/BI awaiting reciprocal-pilot resolution. This resolves F-pilot-EM-7 (r1 finding on empty predecessor list). ✓

### §6 schemas (20)

All 20 schema beads present in pilot §4 (unchanged from r1): 6 typed-ID aliases, 8 RECORDs, 5 ENUMs, 1 trailer-format declaration. ✓

### §8 error-taxonomy

`em-error.taxonomy` row in pilot §4 covers all 6 failure classes (unchanged from r1). ✓

### Retired IDs (3 §5)

All 3 retired §5 invariants enumerated in pilot's spec-parent description AND pilot §3 footer — unchanged from r1. ✓

No missed IDs. No phantom IDs.

---

## Tally arithmetic

Pilot §7 tally (lines 249–288 in v0.2.0):

| Class | Stated count | Actual rows in pilot |
|---|---|---|
| Spec parent (`em`) | 1 | 1 |
| Requirement beads | 62 | 62 |
| Step beads | 0 | 0 |
| Sensor / invariant beads | 3 | 3 |
| Schema beads | 20 | 20 |
| Error-taxonomy beads | 1 | 1 |
| Test-infrastructure beads | 2 | 2 |
| **Total** | **89** | **89** |

**Arithmetic:** 1 + 62 + 0 + 3 + 20 + 1 + 2 = **89**. ✓

**Pilot's own arithmetic check on line 263 also yields 89.** ✓

**Section subtotals reconcile:** Pilot §2 footer (line 106 in v0.1.0, carried forward): "Total req beads = 62." — matches §7. ✓ Pilot §3 footer (line 124 in r1, updated in v0.2.0): "Sensor-bead count: 3." — matches §7. ✓ Pilot §4 footer (line 156 in r1): "Schema + error-taxonomy bead count: 21 (20 schemas + 1 taxonomy)." — matches §7 (20 + 1 = 21). ✓ Pilot §6 footer (line 241 in r1): "Test-infra bead count: 2." — matches §7. ✓

**Pilot §7 BI/AR/EM comparison table (post-v0.2.0 row with fresh EM count): EM total confirmed as 89.** ✓

All section subtotals reconcile to the §7 total. **Tally clean.**

---

## Cross-spec AR edge summary (§5)

Per protocol §3.1 coverage reviewer output specification, verify §5 enumeration matches the §2 row table and the new edge count (v0.2.0 claim is 6 edges, up from v0.1.0's mis-stated count of 4).

**Pilot §5 stated:** "Total emitted cross-spec edges to AR: 6."

**Enumerated in §5 (lines 162–179 in v0.2.0):**

1. `em-011 → ar-001` (four-axis classification) — ✓ appears in §2 em-011 row, blocks-edges column
2. `em-011 → ar-005` (mechanism|cognition tag) — ✓ appears in §2 em-011 row, blocks-edges column
3. `em-schema.node → ar-001` (AxisTags type) — ✓ appears in §2 em-schema.node row, blocks-edges column
4. `em-schema.node → ar-005` (ModeTag type) — ✓ appears in §2 em-schema.node row, blocks-edges column
5. `em-schema.transition → ar-032` (actor_role vocabulary) — ✓ appears in §2 em-schema.transition row, blocks-edges column
6. `em-046 → ar-032` (actor_role direct term-use) — **v0.2.0 NEW:** ✓ appears in §2 em-046 row, blocks-edges column. Added per F-em-r1-MIN-10 (explicit-is-better-than-transitive at corpus scale; transitive coverage via em-schema.transition → ar-032 already existed).

**Verification:** All 6 edges enumerated in §5 are also listed in the §2 row table blocks-edges columns. ✓ The v0.1.0 narrative error ("4 edges") is corrected: v0.1.0 listed 4 edges in §2 initially, but em-schema.node → ar-005 was already in the §2 table and missed in the §5 narrative. v0.2.0 adds em-046 → ar-032, bringing the §5 enumeration to 6 edges which now matches §2. The pilot §7 revision-history entry confirms this fix at line 323: "§5 cross-spec AR edge tally rewritten — v0.1.0's 'Total emitted cross-spec edges to AR: 4' was off-by-one (the `em-schema.node → ar-005` edge was already correctly listed in §2's row table but missed in the §5 narrative); v0.2.0 enumerates 6 edges total after also adding the new `em-046 → ar-032` direct term-use edge."

**Cross-spec AR edge tally: CLEAN.** ✓

---

## Spec-version cite

- **Source front matter** (`specs/execution-model.md` line 10): `version: 0.3.3`, `last-updated: 2026-04-25`, `status: reviewed`.
- **Pilot §1** (lines 3, 9 in v0.2.0): "drafted 2026-04-27 (v0.1.0) and re-drafted 2026-04-28 (v0.2.0) against `specs/execution-model.md` v0.3.3 (status `reviewed`)"

**Match.** ✓ Spec-version reference unchanged from r1, still correct at v0.3.3.

---

## Discipline-version cite

- **Discipline front matter** (`docs/decompose-to-tasks/discipline.md` line 1): `discipline-version: 0.7`.
- **Pilot §1** (line 3 in v0.2.0): "discipline v0.7 (post EM r1 patch — adds invariant-body term-use sub-clause, sensor-predecessor degeneracy + `gated-by-corpus-scale` tag, ...)"

**Match.** ✓ v0.2.0 cites discipline v0.7, which is the current version.

---

## v0.2.0 change verification

Per synthesis §1.2 "Pilot-lane fixes that ARE behavioral consequences of v0.7", verify all changes described in the revision-history entry (pilot §7, line 323) are present in the live v0.2.0 document.

### §5 tally fix

**Claimed in revision history:** "§5 cross-spec AR edge tally rewritten — v0.1.0's 'Total emitted cross-spec edges to AR: 4' was off-by-one ... v0.2.0 enumerates 6 edges total after also adding the new `em-046 → ar-032` direct term-use edge."

**Verification:**
- Line 180 in v0.2.0: "**Total emitted cross-spec edges to AR: 6.**" ✓
- Lines 162–179 enumerate all 6 edges including em-046 → ar-032. ✓
- em-046 → ar-032 entry (lines 177–178): "6. `em-046 → ar-032` — term-use of `actor_role` role-name vocabulary. EM-046 body uses `actor_role` (with values `daemon` and 'role of the verdict-executing subsystem') — term-use of the seven-role vocabulary owned by AR-032 per discipline v0.7 §3.1 step 5. Direct edge `em-046 → ar-032` emitted (transitive coverage via `em-schema.transition → ar-032` exists; the direct edge is preferred for clarity per F-em-r1-MIN-10)." ✓

**Status:** FIXED. ✓

### em-040 row note rewrite

**Claimed in revision history:** "em-040 row note rewritten to LEAD with F-pilot-AR-r2-2 invariant-as-target precedence per v0.7 §3.1 step 5 precedence rule (AR §4.9 houses AR-INV-007); F-pilot-AR-10 supporting-cite analysis is parallel/redundant under the new precedence rule."

**Verification:** Pilot §2, em-040 row (line 95 in v0.2.0):

"The `[architecture.md §4.9]` cite — AR §4.9 houses AR-INV-007 (centralized-controller invariant promoted from AR-037). Per discipline v0.7 §3.1 step 5 precedence rule, invariant-as-target exemption (F-pilot-AR-r2-2) takes precedence over supporting-cite analysis (F-pilot-AR-10): no edge fires because emitting `em-040 → ar-inv-007` would reverse the §2.5 F12 sensor↔impl one-way rule. Supporting-cite test would also yield no-edge ... but the invariant-as-target rule is the primary justification. The `[process-lifecycle.md §4.10]` cite is forward (F-pilot-EM-2). No AR edge emitted."

**Status:** FIXED — note leads with invariant-as-target precedence (F-pilot-AR-r2-2), then notes supporting-cite analysis as parallel/redundant. ✓

### em-046 row gains direct ar-032 edge

**Claimed in revision history:** "v0.2.0 enumerates 6 edges total after also adding the new `em-046 → ar-032` direct term-use edge (per F-em-r1-MIN-10)."

**Verification:** Pilot §2, em-046 row (line 102 in v0.2.0):

"blocks edges (citing bead → prerequisite)" column includes: `em-023`, `em-044`, `ar-032`. The em-046 row note (lines 102–103) confirms: "**Cross-spec edge to AR.** EM-046 body uses `actor_role` ... Direct edge `em-046 → ar-032` emitted (transitive coverage via `em-schema.transition → ar-032` exists; the direct edge is preferred for clarity per F-em-r1-MIN-10)."

**Status:** FIXED. ✓

### em-005a row note tightened

**Claimed in revision history:** "em-005a row note tightened to clarify forward-extension cite mechanics (the AR §4.6 cite attaches to 'future variants extend the enum via the amendment protocol' — does not affect MVH-level testability)."

**Verification:** Pilot §2, em-005 row (line 47 in v0.2.0), note section:

"The `[architecture.md §4.6]` cite attaches to a forward-extension statement ('future variants extend the enum via the amendment protocol') that does not affect MVH-level testability of EM-005a's current discriminator-and-payload shape. Supporting-cite per F-pilot-AR-10 v0.6: removing AR §4.6 leaves EM-005a's MVH normative content (the `kind` discriminator + `payload` envelope) independently testable. No edge fires from any of those three cites."

**Status:** FIXED — note now tightens the cite attachment and clarifies the independence test. ✓

### em-inv-005 gains predecessors with gated-by-corpus-scale tag

**Claimed in revision history:** "em-inv-005 gains predecessors `em-schema.checkpoint-trailers` and `em-014` (term-use of `Harmonik-Bead-ID` trailer key + bead-tied-runs concept); also carries the transient `gated-by-corpus-scale` tag (per v0.7 §2.5 sensor-predecessor degeneracy, F-em-r1-MAJ-2) for still-deferred RC/BI body cites."

**Verification:** Pilot §3, em-inv-005 row (line 120 in v0.2.0):

- **predecessors column (blocks edges):** `em-schema.checkpoint-trailers`, `em-014` ✓
- **tags column:** includes `gated-by-corpus-scale` ✓
- **note:** "Per discipline v0.7 §3.1 step 5 invariant-body term-use sub-clause (F-em-r1-MAJ-1): invariant body uses `Harmonik-Bead-ID` (owned by `em-schema.checkpoint-trailers` — the trailer registry) and the bead-tied-runs concept (owned by `em-014` — the canonical bead↔run linkage where `Harmonik-Bead-ID` semantics anchor). Both fire as direct sensor predecessors. The remaining invariant-body cross-spec inline cites (`[reconciliation/spec.md §8.4 Cat 3]` and `[beads-integration.md §4.7 BI-022]`) are forward-deferred (depends-on=architecture only); per discipline v0.7 §2.5 sensor-predecessor degeneracy rule, the bead carries the transient `gated-by-corpus-scale` tag because in-corpus sources resolve only the strict-empty case while body still has out-of-`depends-on` cites awaiting RC/BI reciprocal-pilot resolution. F-pilot-EM-7 self-flag is RE-FRAMED: not an invented sensor-degeneracy clause but actual term-use edges per v0.7 source (4); transient tag carried for forward-deferred body cites. One-way per §2.5 F12."

**Status:** FIXED. ✓

### em-inv-001 gains optional em-016, em-017 predecessors

**Claimed in revision history:** "em-inv-001 gains optional direct `em-016`, `em-017` predecessors per the same sub-clause (transitive coverage via em-031..em-033 already exists; explicit-is-better-than-transitive at corpus scale)."

**Verification:** Pilot §3, em-inv-001 row (line 118 in v0.2.0):

- **predecessors column (blocks edges):** `em-016`, `em-017`, `em-031`, `em-031a`, `em-032`, `em-033` (6 edges total) ✓
- **note:** "Sensor predecessor list derives from §10.2 conformance-group prose cite ('EM-031..EM-033 (state reconstruction)') plus discipline v0.7 §3.1 step 5 invariant-body term-use sub-clause: invariant body uses 'git checkpoint trail' — defined by EM-016 (checkpoint = git commit + sibling-file) and EM-017 (trailer registry that anchors the trail). Direct edges `em-inv-001 → em-016, em-017` emitted; transitive coverage via `em-031..em-033` exists but explicit-is-better-than-transitive at corpus scale per F-em-r1-MAJ-1 worked example (reciprocal pilots may not preserve the chain). One-way per §2.5 F12."

**Status:** FIXED. ✓

### §3 sensor-table preamble updated

**Claimed in revision history:** "§3 sensor-table preamble updated from THREE §10.2 sources to FOUR (the new invariant-body inline term-use source)."

**Verification:** Pilot §3, preamble (lines 113–115 in v0.2.0):

"Per discipline §2.5 v0.7, sensor predecessors derive from FOUR distinct §10.2 sources (conformance-group cites, persona bundling with named-ID-only trigger, sensor-block body inline cites, and invariant-body inline term-use per F-em-r1-MAJ-1); EM has no §10.2 reviewer-persona block, so source #2 (persona bundling) does not fire here. Sources #1 and #3 cover the §4-req-transitive predecessors; source #4 covers schemas / impl reqs that the invariant body term-uses directly."

**Status:** FIXED — preamble now explicitly names four sources (r1 had implicit three). ✓

### All policy decisions applied

**Claimed in revision history:**

- "F-pilot-EM-3 closed by v0.7 §3.2 §4.a envelope grandfather carve-out (Option E): EM is grandfathered alongside HC/CP/WM/PL/RC; no spec edit; no pilot patch." — No structural change needed. ✓
- "F-pilot-EM-6 codified into v0.7 §2.11(d.1) registry-row dual-ownership extension." — No bead-set change needed. ✓
- "F-pilot-EM-7 RE-FRAMED — not a sensor-degeneracy clause but actual term-use edges per v0.7 source (4); the residual deferred-cite gap is now covered by the `gated-by-corpus-scale` tag mechanism." — Addressed by em-inv-005 predecessors + tag changes above. ✓
- "F-pilot-EM-1, F-pilot-EM-5 closed by v0.7 worked examples / typed-alias-cluster guidance." — Documentation-only changes in discipline, no pilot bead-set impact. ✓
- "F-pilot-EM-2 partially resolved — Option B chosen at synthesis (EM `depends-on` kept as-is per spec author's deliberate v0.2.0 cycle-break); reciprocal-direction edges from sibling pilots will materialize most deferred deps." — No bead-set change needed. ✓

**Status:** All policy decisions recorded and applied as documented. ✓

---

## Findings list

### BLOCKER findings

**No BLOCKER findings.** All 66 active §4 reqs (62 first-class beads + 4 coalesce names), 3 active §5 invariants, 20 §6 schemas, and 1 §8 error-taxonomy are accounted for in the pilot. All v0.1.0 findings were `class`-tagged (discipline-lane) and resolved by v0.7 patch + v0.2.0 re-draft. No structural gaps.

### MAJOR findings

**No MAJOR findings.** All v0.1.0 MAJORs were resolved:
- F-em-r1-MAJ-1 (invariant-body term-use sub-clause) — v0.7 patch applied; em-inv-005 and em-inv-001 rows updated with new predecessors. ✓
- F-em-r1-MAJ-2 (sensor-predecessor degeneracy + tag) — v0.7 patch applied; `gated-by-corpus-scale` tag added to em-inv-005. ✓
- F-em-r1-MAJ-3 (type-alias-MVH-redundant) — v0.7 patch applied; bead set unchanged (verdict-payload not minted per policy). ✓
- F-em-r1-MAJ-4 (rule precedence) — v0.7 patch applied; em-040 note rewritten with precedence rule leading. ✓

### MINOR findings

**No MINOR findings.** All v0.1.0 MINORs were resolved:
- F-em-r1-MIN-1, F-em-r1-MIN-2 (coverage MINOR class findings) — documentation-only in discipline v0.7. ✓
- F-em-r1-MIN-3 through F-em-r1-MIN-9 (decomposition/reference/self-flag class findings) — all addressed by v0.7 patches and pilot-lane fixes. ✓
- F-em-r1-MIN-4 (pilot-lane, §5 tally) — fixed in v0.2.0 re-draft. ✓
- F-em-r1-MIN-10 (pilot-lane, em-046 edge) — fixed in v0.2.0 re-draft. ✓

---

## Summary

**0 BLOCKER, 0 MAJOR, 0 MINOR.** EM pilot v0.2.0 is clean.

**Evidence:**

1. **Counts:** All 66 active §4 reqs, 3 active §5 invariants, 20 §6 schemas, 1 §8 taxonomy accounted for. Four §2.3 coalesces properly named. Bead count unchanged at 89 (1 spec-parent + 62 req + 0 step + 3 sensor + 20 schema + 1 taxonomy + 2 test-infra). ✓

2. **Spec-version cite:** v0.3.3, matches source front matter. ✓

3. **Discipline-version cite:** v0.7, matches current discipline version. ✓

4. **§5 AR edge tally:** 6 edges enumerated, all present in §2 row table blocks-edges. Tally arithmetic correct. ✓

5. **§7 tally:** 89 beads. Arithmetic check: 1 + 62 + 0 + 3 + 20 + 1 + 2 = 89. ✓

6. **v0.2.0 changes verified:**
   - §5 tally corrected from 4 to 6. ✓
   - em-040 note rewritten with v0.7 precedence rule. ✓
   - em-046 row gains direct ar-032 edge. ✓
   - em-005a note tightened on forward-extension cite. ✓
   - em-inv-005 gains predecessors + `gated-by-corpus-scale` tag. ✓
   - em-inv-001 gains em-016, em-017 predecessors (6 total). ✓
   - §3 preamble updated from THREE to FOUR §10.2 sources. ✓

7. **No missed IDs, no phantom IDs:** All spec IDs accounted for; all pilot IDs trace to spec. ✓

8. **Retired IDs enumerated:** EM-INV-002, EM-INV-003, EM-INV-006 in spec-parent description. ✓

**Readiness:** EM pilot v0.2.0 is ready for load against discipline v0.7. No re-draft needed. All r1 findings resolved. All v0.7 changes in place. Bead set stable at 89.

---

Reviewed against protocol `pilot-review-protocol.md` v0.2.
