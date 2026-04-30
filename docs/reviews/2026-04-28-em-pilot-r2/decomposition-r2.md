# EM Pilot Decomposition-Quality Review (r2) — 2026-04-28

Reviewer: decomposition-quality reviewer (per `pilot-review-protocol.md` v0.2 §3.2). Pilot under review: `docs/decompose-to-tasks/em-pilot.md` v0.2.0 against `specs/execution-model.md` v0.3.3 (status `reviewed`), with discipline `docs/decompose-to-tasks/discipline.md` v0.7 (post EM-r1 patch — adds §3.1 step 5 invariant-body term-use sub-clause, §2.5 4th source + sensor-predecessor degeneracy + `gated-by-corpus-scale` tag, §2.6 type-alias-MVH-redundant + typed-alias clusters, §2.7 F13 declaration-rule ↔ retrieval-method second worked example, §2.11(d.1) registry-row dual-ownership extension, §3.1 invariant-as-target precedence rule, §3.1 §7 prose no-edge, §3.2 §4.a envelope grandfather carve-out, §3.1.3 forward-deferred wide-fanout tag policy, §2.2 F8b behavioural-spec worked example).

**Result.** The pilot v0.2.0 cleanly addresses all five r1 decomposition-quality findings (1 MAJOR + 4 MINOR), correctly applies every v0.7 mechanical consequence flagged in the synthesis (em-inv-005 new predecessors + tag, em-inv-001 new predecessors, em-040 rationale lead, em-005a rationale tightened, em-046 direct AR edge, §3 sensor-table preamble updated to four sources), and the §8 self-flag updates accurately reflect the v0.7 patch outcomes. No new defects surface in the v0.2.0 re-draft. The pilot is structurally sound at the granularity / coalesce / sensor / schema level; descriptions remain faithful to the spec; the four §2.3 coalesces are sound; the F8b shared-function-body tiebreaker is applied correctly to EM-016 and EM-038; the three sensor beads are real verification mechanisms; cross-spec edges trace cleanly to inline cites or term-uses.

## 1. Sampled beads (15)

Weighted toward riskier classes per protocol §3.2, with particular focus on v0.2.0 changes:

- **All 4 §2.3 coalesces:** `em-005`, `em-023`, `em-041`, `em-043`.
- **All 3 sensors (esp. v0.2.0 changes to em-inv-001 and em-inv-005):** `em-inv-001`, `em-inv-004`, `em-inv-005`.
- **4 schema beads (incl. dual-ownership case):** `em-schema.checkpoint-trailers`, `em-schema.outcome`, `em-schema.transition`, `em-schema.run-id`.
- **4 first-class beads (incl. the v0.2.0-rewritten ones):** `em-040` (rationale rewrite), `em-046` (new AR edge), plus regression check on `em-001` and `em-046b`.

## 2. Per-sample findings

(Sampled beads with NO issue: `em-005` Q2, `em-023` Q2, `em-041` Q2, `em-043` Q2 [coalesces sound — unchanged from r1]; `em-inv-001` Q4 [sensor description unchanged + r1 MINOR fix correctly applied]; `em-inv-004` Q4 [sensor unchanged]; `em-inv-005` Q4+Q1 [r1 MAJOR finding correctly resolved]; `em-schema.checkpoint-trailers` Q5 [registry contents unchanged + dual-ownership annotation accurate]; `em-schema.outcome` Q5; `em-schema.transition` Q5+Q1 [`ar-032` edge unchanged from r1, sound]; `em-schema.run-id` Q5; `em-001` Q1 [description unchanged]; `em-040` Q1 [rationale lead correctly rewritten]; `em-046` Q1 [direct `ar-032` edge correctly added]; `em-046b` Q1. They pass cleanly.)

### 2.1 `em-005` — coalesce sound; v0.2.0 row-note tightening landed correctly

- **Spec reqs covered:** EM-005 + EM-005a (coalesced).
- **Q2 (coalesce).** PASS, unchanged from r1. EM-005 declares the Outcome record; EM-005a extends with `kind` discriminator + `payload` envelope. They share the Outcome shape; inseparable. Pilot description correctly enumerates both source IDs (`req:EM-005, req:EM-005a`) and the derived schema beads.
- **Q1 (v0.2.0 rationale tightening).** PASS. The pilot's row note at em-005 (line 47) was rewritten in v0.2.0 to tighten the supporting-cite rationale: "The `[architecture.md §4.6]` cite attaches to a forward-extension statement ('future variants extend the enum via the amendment protocol') that does not affect MVH-level testability of EM-005a's current discriminator-and-payload shape. Supporting-cite per F-pilot-AR-10 v0.6: removing AR §4.6 leaves EM-005a's MVH normative content (the `kind` discriminator + `payload` envelope) independently testable." The rewrite addresses r1's MINOR finding (decomp §2.1 / synthesis F-em-r1 row "documentation tightening folded into MAJ-4"). Verified against the spec body (line 120): the AR §4.6 cite is indeed in the "future variants" sub-clause, NOT in the MVH normative shape declaration. The rewritten rationale is now explicit about the forward-extension cite mechanics. Resolution stands.
- **Severity.** None. Resolved by v0.2.0 re-draft.

### 2.2 `em-040` — v0.2.0 row-note rewrite leads with F-pilot-AR-r2-2 (correctly)

- **Spec reqs covered:** EM-040.
- **Q1 (v0.2.0 rationale lead rewrite).** PASS. The pilot's em-040 row note at line 95 was rewritten in v0.2.0 to lead with F-pilot-AR-r2-2 invariant-as-target precedence per v0.7 §3.1 step 5 precedence rule. Verified word-for-word against discipline v0.7 §3.1 step 5 (F-em-r1-MAJ-4 clause): "When BOTH the supporting-cite test (F-pilot-AR-10 in step 1 above) AND the invariant-as-target exemption (F-pilot-AR-r2-2 immediately above) could plausibly apply to the same cite, the invariant-as-target exemption takes precedence." The pilot now correctly invokes the precedence rule with the primary structural justification ("emitting `em-040 → ar-inv-007` would reverse the §2.5 F12 sensor↔impl one-way rule") and retains F-pilot-AR-10 as parallel/redundant secondary justification. The §5 supporting-cite reclassifications block (line 186) similarly leads with the invariant-as-target exemption. r1 MINOR finding §2.10 is RESOLVED.
- **Severity.** None. Resolved by v0.2.0 re-draft.

### 2.3 `em-046` — v0.2.0 direct `ar-032` edge correctly added

- **Spec reqs covered:** EM-046.
- **Q1 (v0.2.0 new direct AR edge).** PASS. Verified the spec body at line 552 of `specs/execution-model.md`: "the `Outcome` associated with a context-restore transition is synthesized by the daemon with `status = SUCCESS` and an `actor_role` of `daemon` or the role of the verdict-executing subsystem." This is direct term-use of `actor_role` (the seven-role vocabulary owned by AR-032) WITH named role values (`daemon` is one of the seven roles). Per discipline v0.7 §3.1 step 5 (term-use rule), the direct edge `em-046 → ar-032` is justified — the term-use IS load-bearing for EM-046's claim about which roles synthesize the Outcome. Pilot row at line 102 emits the edge correctly with predecessor list `em-023, em-044, ar-032` and provides clear rationale citing F-em-r1-MIN-10 (explicit-is-better-than-transitive). The r1 MINOR §6.2 finding is RESOLVED.
- **§5 narrative consistency check.** PASS. The new edge appears as item 6 in the §5 enumeration (line 178), the §5 total reads "Total emitted cross-spec edges to AR: 6" (line 180), and the §3 narrative + §7 tally are consistent with this 6-edge total. r1 MINOR §6.1 (off-by-one §5 narrative) and r1 MINOR §6.2 (missing direct edge) are jointly resolved.
- **Severity.** None. Resolved by v0.2.0 re-draft.

### 2.4 `em-inv-001` — v0.2.0 optional direct predecessors correctly added

- **Spec reqs covered:** EM-INV-001.
- **Q4 (sensor as real mechanism).** PASS (unchanged from r1). The sensor description names a concrete restart scenario test ("destroys daemon and confirms state reconstructable from git+Beads without JSONL reads") + cross-spec scenario harness — real verification mechanisms.
- **Q1 (v0.2.0 new direct term-use edges).** PASS. The pilot's em-inv-001 row at line 118 now emits predecessors `em-016, em-017, em-031, em-031a, em-032, em-033` — adding `em-016` and `em-017` via the v0.7 §3.1 step 5 invariant-body term-use sub-clause. Verified: EM-INV-001's body (spec line 572) uses "git checkpoint trail" — a defined term anchored by EM-016 (checkpoint = git commit + sibling-file) and EM-017 (the trailer schema that anchors the trail). Per the v0.7 source #4 (invariant-body inline term-use), both fire as direct sensor predecessors. The pilot's row note correctly cites "discipline v0.7 §3.1 step 5 invariant-body term-use sub-clause" and acknowledges that transitive coverage via em-031..em-033 → em-016/em-017 already exists, justifying the explicit-is-better-than-transitive principle (consistent with F-em-r1-MAJ-1 worked example reasoning).
- **Sensor↔impl one-way preserved.** PASS. None of em-016, em-017, em-031, em-031a, em-032, em-033 emits a reverse edge to em-inv-001 in their pilot rows (verified by spot-check of em-016 row line 61, em-017 row line 62, em-031 row line 81). One-way per discipline §2.5 F12.
- **Severity.** None. Resolved by v0.2.0 re-draft. r1 MINOR §2.7 finding is RESOLVED (folded into MAJOR fix).

### 2.5 `em-inv-005` — v0.2.0 new predecessors + transient tag correctly applied

- **Spec reqs covered:** EM-INV-005.
- **Q4 (sensor as real mechanism).** PASS (unchanged from r1). Two named scenario tests with concrete injection conditions and assertion targets.
- **Q1 (v0.2.0 new term-use predecessors + `gated-by-corpus-scale` tag).** PASS. The pilot's em-inv-005 row at line 120 now emits predecessors `em-schema.checkpoint-trailers, em-014` per the v0.7 §3.1 step 5 invariant-body term-use sub-clause. Verified against EM-INV-005's body (spec line 584): "if Beads reports a bead as `closed` but no merge commit with `Harmonik-Bead-ID` matching that bead exists in the project's git history" — direct term-use of `Harmonik-Bead-ID` (owned by `em-schema.checkpoint-trailers` per the §6.2 trailer registry) AND the bead-tied-runs concept (owned by EM-014 per the canonical bead↔run linkage where `Harmonik-Bead-ID` semantics anchor — verified at spec line 180–182 + the §6.2 registry annotation at spec line 770: "Present iff the run is bead-tied (§4.3.EM-014). Owning spec: execution-model"). Both fire as direct predecessors per v0.7 source #4. Transitive coverage does NOT exist for these dependencies (no §10.2 group cite, no persona block, no sensor-block body-cite covers them) — the term-use rule is the primary edge-derivation mechanism here, exactly as F-em-r1-MAJ-1's worked example anticipates.
- **Tag correctness.** PASS. The bead carries `gated-by-corpus-scale` per v0.7 §2.5 sensor-predecessor degeneracy rule (F-em-r1-MAJ-2). Verified against discipline v0.7 §2.5 (line 142): "When all four §10.2 sources above return empty AND the invariant's body has at least one cross-spec inline cite to a target outside the spec's `depends-on` (forward-deferred, awaiting reciprocal-pilot edge materialization), the sensor bead carries the transient tag `gated-by-corpus-scale`." EM-INV-005's body cites `[reconciliation/spec.md §8.4 Cat 3]` and `[beads-integration.md §4.7 BI-022]` — both targets are NOT in EM's `depends-on: [architecture]`, so the deferred-cite condition is met. **Important nuance:** the v0.7 §2.5 degeneracy rule's literal text says "When all four §10.2 sources above return empty" — but in em-inv-005's case, source #4 (invariant-body term-use) DOES fire (yielding em-schema.checkpoint-trailers + em-014). So strictly, all four sources are NOT empty. The pilot's row note at line 120 acknowledges this: "in-corpus sources resolve only the strict-empty case while body still has out-of-`depends-on` cites awaiting RC/BI reciprocal-pilot resolution" — which reads the tag as covering the residual deferred-cite gap, not the strict-empty case. This is a defensible reading but NOT what the v0.7 §2.5 literal text says. **Documentation-tightening opportunity** — see §5 below for the class-tagged finding.
- **Sensor↔impl one-way preserved.** PASS. Neither `em-schema.checkpoint-trailers` (line 153) nor `em-014` (line 56) emits a reverse edge to em-inv-005. One-way per §2.5 F12.
- **Severity.** None for the predecessors and term-use call. The tag-application reading is borderline (see §5).

### 2.6 `em-inv-004` — sensor unchanged from v0.1.0

- **Spec reqs covered:** EM-INV-004.
- **Q4 result.** PASS, unchanged from r1. Cross-subsystem authoring-surface reviewer-persona scan — real verification mechanism. Pilot row at line 119 emits `em-033` per the sensor-block body inline cite (§2.5 source #3); cross-subsystem targets in workspace-model / beads-integration / reconciliation are forward-deferred per F-pilot-EM-2.
- **Q1 (v0.7 source #4 check).** PASS. Reviewing EM-INV-004's body (spec line 578) for additional defined-term uses: the body inline-cites `[workspace-model.md §4.2]`, `[beads-integration.md §4.10]`, `[reconciliation/spec.md §4.5]` — all forward-deferred (depends-on=architecture). EM-033 is named explicitly. No additional intra-EM term-uses surface that warrant new predecessors under v0.7 source #4. The row's predecessor list `em-033` stands.
- **Severity.** None.

### 2.7 `em-schema.checkpoint-trailers` — dual-ownership pattern correctly applied per v0.7 §2.11(d.1)

- **Spec reqs covered:** §6.2 trailer registry (with two RC-owned rows).
- **Q5 result.** PASS, unchanged from r1. The pilot description enumerates all 7 keys plus the RC-owned `Harmonik-Verdict-Executed` known-extension. RC-ownership annotated in the description per §6.2 informative-note ownership annotation.
- **Q1 (v0.7 §2.11(d.1) compliance).** PASS. Discipline v0.7 §2.11(d.1) (lines 324) codifies the dual-ownership pattern exactly as the pilot applies it: "registry-OWNING spec mints the bead (one bead, not split); dual-owned rows are annotated in the description; the cross-spec edge from a downstream consumer of a dual-owned row goes to the OTHER spec's owning bead per the consumer's term-use rule (§3.1 step 5) — NOT to the registry bead itself." Pilot row at line 153 mints the bead under EM, annotates RC ownership for the two rows, and (importantly) does NOT emit any edges from `em-schema.checkpoint-trailers` to RC beads. The v0.7 codification is faithful to the pilot's v0.1.0 choice. r1 MINOR §2.8 (F-pilot-EM-6 self-flag validated) is RESOLVED.
- **Severity.** None.

### 2.8 `em-schema.outcome` and `em-schema.transition` — schemas with v0.2.0-relevant cross-edges unchanged

- **`em-schema.outcome` Q5.** PASS. 7-field RECORD enumerated faithfully (status, preferred_label, suggested_next_ids, context_updates, notes, kind, payload); v0.3.3 additive `kind` + `payload` envelope correctly noted. Predecessors `em-schema.outcome-status, em-schema.outcome-kind, em-schema.node-id`. No v0.2.0 changes to this row; description matches spec §6.1 verbatim. (Spec line 730 confirms the 7-field shape.)
- **`em-schema.transition` Q5.** PASS. 13-field RECORD enumerated faithfully; `actor_role`, `outcome_status` (v0.3.0-added), reserved evidence keys (`sub_workflow_pin`, `synthesized_outcome`) all present. Predecessor list includes `ar-032` (term-use of role vocabulary per AR §4.8). No v0.2.0 changes to this row.
- **Severity.** None.

### 2.9 `em-001`, `em-046b`, `em-schema.run-id` — regression checks unchanged

- **`em-001` Q1.** PASS, unchanged from r1. Description faithful to spec §4.1 EM-001; supporting-cite reclassification of AR §4.10 is sound.
- **`em-046b` Q1.** PASS, unchanged. RETRY re-dispatch protocol description faithful to spec line 562–564.
- **`em-schema.run-id` Q5.** PASS, unchanged. Typed-alias bead — `kind:primitive-shape`. No v0.2.0 changes; F-em-r1-MIN-8 / F-pilot-EM-5 typed-alias-cluster guidance now codified in v0.7 §2.6 (line 162) confirms the pilot's call to keep all 5 thin aliases as separate primitive-shape beads.
- **Severity.** None.

## 3. Missing-coalesce flags

No new missing-coalesce findings. The r1 review's coalesce-candidate cluster analysis (typed-ID aliases F-pilot-EM-5; outcome-spine EM-027..EM-030; sub-workflow EM-034..EM-034c; run lifecycle EM-015..EM-015c; rollback EM-044..EM-046) all resolve identically under v0.7 — the only one that surfaced as a class finding (typed-alias clusters) is now codified in v0.7 §2.6. No new clusters appear in the v0.2.0 re-draft because no §2 / §3 / §4 row was structurally added or removed.

## 4. Over-split flags

None. Same analysis as r1 §4. The pilot does not over-split. EM-016's 3-step git sequence and EM-038's 6 validator categories remain correctly KEPT as single beads via §2.2 F8b, now with v0.7 worked-example codification (line 74 of the discipline).

## 5. v0.7 mechanical-consequence verification

Per the synthesis §1.3 v0.7 mechanical consequences checklist:

| v0.2.0 mechanical change | Pilot location | Verification |
|---|---|---|
| em-inv-005 + `em-schema.checkpoint-trailers` | em-pilot.md line 120 predecessors | ✓ Edge present; term-use justified (Harmonik-Bead-ID anchored by registry); v0.7 §3.1 step 5 source #4 |
| em-inv-005 + `em-014` | em-pilot.md line 120 predecessors | ✓ Edge present; term-use justified (bead-tied-runs concept anchored by canonical bead↔run linkage); v0.7 §3.1 step 5 source #4 |
| em-inv-005 carries `gated-by-corpus-scale` tag | em-pilot.md line 120 tag list | ✓ Tag present (`mech, req:EM-INV-005, kind:invariant, gated-by-corpus-scale`); covers residual deferred RC/BI body cites |
| em-inv-001 + `em-016`, `em-017` | em-pilot.md line 118 predecessors | ✓ Both edges present; term-use justified ("git checkpoint trail" anchored by EM-016 + EM-017); v0.7 §3.1 step 5 source #4 |
| em-046 → ar-032 direct edge | em-pilot.md line 102 + §5 line 178 | ✓ Edge present in row + enumerated in §5; term-use justified (`actor_role` + named role values per AR-032 vocabulary) |
| em-040 row note leads with F-pilot-AR-r2-2 | em-pilot.md line 95 | ✓ Lead rationale rewritten; F-pilot-AR-10 retained as parallel secondary; v0.7 §3.1 step 5 precedence rule applied |
| em-005a row note tightened | em-pilot.md line 47 | ✓ Forward-extension-cite mechanics now explicit; rationale clarified |
| §3 sensor-table preamble FOUR sources | em-pilot.md line 114 | ✓ Updated from THREE to FOUR; correctly notes sources #1+#3 cover §4-req-transitive predecessors and source #4 covers schemas/impl reqs the invariant body term-uses |
| §5 cross-spec AR edge tally = 6 | em-pilot.md line 180 | ✓ "Total emitted cross-spec edges to AR: 6" |
| §7 tally arithmetic preserved | em-pilot.md line 264 | ✓ 1 + 62 + 0 + 3 + 20 + 1 + 2 = 89 (no structural change to bead count) |

All v0.7 mechanical consequences are correctly applied. No drift, no missed application.

## 6. §8 self-flag verification

Per protocol task 4, re-verify F-pilot-EM-1 / F-pilot-EM-5 / F-pilot-EM-6 / F-pilot-EM-7 self-flag updates:

- **F-pilot-EM-1 (line 293).** Marked `[RESOLVED v0.7 — discipline §2.2 F8b worked example codified per F-em-r1-MIN-7]`. Verified against discipline v0.7 §2.2 (line 74): F8b grew the EM-016 3-step + EM-038 6-category worked example. Self-flag accurate.
- **F-pilot-EM-3 (line 302).** Marked `[RESOLVED v0.7 — §4.a envelope grandfather carve-out per Option E]`. Verified against discipline v0.7 §3.2 (line 398): grandfather carve-out enumerates "EM, HC, CP, WM, PL, RC" as the pre-AR-053 set. Self-flag accurate.
- **F-pilot-EM-5 (line 311).** Marked `[RESOLVED v0.7 — typed-alias-cluster guidance codified per F-em-r1-MIN-8]`. Verified against discipline v0.7 §2.6 (line 162): "typed-alias clusters without anchor structure" guidance landed; references the EM RunID/StateID/TransitionID/NodeID/BeadID example. Self-flag accurate.
- **F-pilot-EM-6 (line 313).** Marked `[RESOLVED v0.7 — discipline §2.11(d.1) registry-row dual-ownership extension per F-em-r1-MIN-9]`. Verified against discipline v0.7 §2.11(d.1) (line 324): codified with the `em-schema.checkpoint-trailers` worked example. Self-flag accurate.
- **F-pilot-EM-7 (line 315).** Marked `[RE-FRAMED v0.7 — actual term-use edges per §3.1 step 5 invariant-body sub-clause; transient `gated-by-corpus-scale` tag covers residual deferred cites]`. Verified against discipline v0.7 §3.1 step 5 source #4 (line 377) + §2.5 sensor-predecessor degeneracy (line 142): the re-framing accurately reflects what the v0.7 patch actually did. Self-flag accurate.

All self-flag updates accurately reflect v0.7 patch outcomes.

## 7. New decomposition issues introduced by the re-draft

One MINOR `class` finding surfaces from close reading of the v0.2.0 row note for em-inv-005 (decomp §2.5 above) — a documentation-precision gap in how the `gated-by-corpus-scale` tag's trigger condition is phrased. Not a structural issue with the pilot; surfaces only because the v0.7 §2.5 degeneracy rule's literal text and em-inv-005's actual condition diverge.

### 7.1 `em-inv-005`'s `gated-by-corpus-scale` tag application — borderline reading of v0.7 §2.5 trigger

- **Concern.** Discipline v0.7 §2.5 (line 142) states the tag fires "When all four §10.2 sources above return empty AND the invariant's body has at least one cross-spec inline cite to a target outside the spec's `depends-on`." For em-inv-005, source #4 (invariant-body term-use) DOES fire (yielding `em-schema.checkpoint-trailers` and `em-014` predecessors) — so the literal "all four sources empty" condition is not met. The pilot's row note at line 120 reads the tag as covering the *residual* deferred-cite gap (the still-deferred RC/BI body cites), not the strict-empty case. This is a defensible compositional reading: the tag signals "the in-corpus portion of the invariant's body is fully resolved, but the cross-spec portion remains forward-deferred." But the v0.7 §2.5 literal text says only the strict-empty case triggers the tag.
- **Operational test.** Either (a) the v0.7 §2.5 trigger should be relaxed to "when at least one cross-spec inline cite is forward-deferred AND the in-corpus sources are insufficient to cover the body's term-uses" (the pilot's reading), or (b) the tag does NOT apply to em-inv-005 (strict reading) and the residual deferred-cite condition is documented some other way (e.g., via the pilot's forward-cite log per F-em-r1-MIN-6). Both are defensible, and the v0.7 patch landed without disambiguating. The pilot v0.2.0 chose (a); the discipline rule literally describes (b).
- **Severity.** MINOR. The pilot's reading is sensible and operationally useful (the tag IS doing the work it's designed to do — flagging that the bead has unresolved cross-spec deps that need reciprocal-pilot resolution). But the discipline text doesn't explicitly endorse the compositional reading.
- **Lane.** `class`. Multiple specs will hit this same condition: an invariant whose body partially term-uses in-spec definitions and partially term-uses cross-spec targets in non-`depends-on` specs. RC and BI invariants will likely surface analogous patterns. Per protocol §4.1 probe 1 (generality) + probe 3 (silence), the discipline gap is a class finding.
- **Suggested fix.** Pilot: no change — the reading is defensible. Discipline: §2.5 (line 142) tag-trigger clause may benefit from explicit clarification that the tag fires when "the four §10.2 sources are EITHER strict-empty OR resolve only the in-corpus portion of the body while at least one cross-spec inline cite remains forward-deferred." The compositional reading codifies what em-inv-005 actually requires. Alternatively, document explicitly that the strict reading wins (tag does NOT apply to em-inv-005; instead use the §3.1.3 forward-cite log for the residual gap). Either resolution is fine; the current ambiguity is the gap.

## 8. Consolidated findings

### BLOCKER (description doesn't match spec — implementation would diverge)

None.

### MAJOR (coalesce/split decision wrong, or sensor predecessor list materially incomplete)

None. r1 MAJOR (em-inv-005 missing term-use edges) is RESOLVED in v0.2.0 via the v0.7 §3.1 step 5 invariant-body term-use sub-clause.

### MINOR (cosmetic / preference / borderline)

| # | Finding | Sample | Severity | Lane |
|---|---|---|---|---|
| 1 | `em-inv-005`'s `gated-by-corpus-scale` tag reads the v0.7 §2.5 trigger compositionally (in-corpus portion fully resolved + cross-spec portion forward-deferred), but the discipline's literal text describes only the strict-empty case. The pilot's reading is operationally sensible; the discipline text may benefit from explicit compositional-reading clause to disambiguate for future invariants with the same shape (RC/BI). | em-inv-005 | MINOR | `class` (discipline v0.7 §2.5 tag-trigger clause has wording ambiguity not addressed by F-em-r1-MAJ-2) |

## 9. Summary

- **0 BLOCKER** findings.
- **0 MAJOR** findings (all r1 MAJORs RESOLVED in v0.2.0 via v0.7 patch).
- **1 MINOR** finding — `class` (discipline §2.5 tag-trigger wording ambiguity surfaces in em-inv-005's compositional reading).
- **All r1 findings RESOLVED:** r1 MAJOR (em-inv-005 missing term-use predecessors) → resolved by v0.7 §3.1 step 5 source #4; r1 MINORs (em-005a rationale thinness, em-inv-001 missing term-use edges, em-040 mis-prioritized rationale, em-046 missing direct AR edge) all resolved by v0.2.0 row-note rewrites + new edges.
- **All v0.7 mechanical consequences correctly applied** (em-inv-005 new predecessors + tag, em-inv-001 new predecessors, em-046 → ar-032 edge, em-040 rationale lead, em-005a rationale tightened, §3 four-sources preamble, §5 6-edge tally).
- **All 4 §2.3 coalesces sound** (em-005, em-023, em-041, em-043) — unchanged from r1.
- **F8b shared-function-body tiebreaker correctly applied** to em-016 and em-038 — unchanged from r1, now backed by v0.7 §2.2 codified worked example.
- **All 6 cross-spec edges to AR justified** by term-use or inline cite (em-011 → ar-001, em-011 → ar-005, em-schema.node → ar-001, em-schema.node → ar-005, em-schema.transition → ar-032, em-046 → ar-032).
- **Three intra-EM bidirectional 2-cycles** still resolved via F13 + F-pilot-AR-10 (em-002↔em-041, em-018↔em-019, em-020↔em-020a) — unchanged from r1; em-018↔em-019 now backed by v0.7 §2.7 second worked example (declaration-rule ↔ retrieval-method).
- **§8 self-flag updates** all accurately reflect v0.7 patch outcomes (F-pilot-EM-1/3/5/6/7 statuses verified).
- **Sensor↔impl one-way** preserved across all new edges (em-inv-005 → em-schema.checkpoint-trailers / em-014; em-inv-001 → em-016 / em-017): no reverse edges from impl beads to sensors.

Per protocol §4.1 probe rules: the single MINOR finding carries a `class` self-tag that routes to the discipline-patch lane unless explicitly overruled in synthesis-r2.md. The pilot v0.2.0 is structurally sound and ready to load; the residual class finding is a documentation-tightening opportunity for v0.8 that does NOT block load (no behavioral change to the bead set).

**Verdict: 0 BLOCKER, 0 MAJOR, 1 MINOR.** v0.2.0 cleanly addresses the r1 cycle. The discipline-tightening clause in §7.1 is non-blocking and may be batched into the next discipline patch (next-pilot pass) per protocol §4.2 MINOR-class-batching policy.
