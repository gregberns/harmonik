# EM Pilot Reference Review (r1) — 2026-04-27

Reviewer: Reference reviewer per `pilot-review-protocol.md` v0.2 §3.3 (extended for v0.6).
Source spec: `specs/execution-model.md` v0.3.3 (`reviewed`).
Pilot draft: `docs/decompose-to-tasks/em-pilot.md` v0.1.0.
Discipline: `docs/decompose-to-tasks/discipline.md` v0.6.

## Summary

EM's front matter declares `depends-on: [architecture]` (line 14–15). Per discipline §3.2 the universe of admissible cross-spec edge targets is `{architecture}`. The pilot reports four cross-spec edges (all to AR), three v0.6 supporting-cite reclassifications (EM-001→AR §4.10, EM-005a→AR §4.6, EM-040→AR §4.9), three intra-EM bidirectional 2-cycles resolved via discipline §2.7 F13 + §3.1 step 1 v0.6 supporting-cite tests, and 50+ forward inline cites in normative §4/§5/§6 prose to non-`depends-on` targets (event-model, handler-contract, control-points, reconciliation, workspace-model, beads-integration, operator-nfr, process-lifecycle) deferred to F-pilot-EM-2 for spec-author triage.

I walked EM's body top-to-bottom and enumerated every cross-document inline cite. The four AR edges check out. The three supporting-cite reclassifications hold under the operational test in discipline §3.1 step 1 v0.6. The three bidirectional intra-EM resolutions (em-002↔em-041, em-018↔em-019, em-020↔em-020a) are textbook applications of F13 / supporting-cite, with one notable consideration: a similar bidirectional structure exists at em-016↔em-018 and em-016↔em-017 (where em-016 names the atomicity boundary and em-017/em-018 cite back to it), but these are correctly resolved as one-direction `em-017 → em-016` and `em-018 → em-016` because em-017's body says "Trailers are a cheap index... authoritative fields live in the sibling file of §4.4.EM-018" and em-018 cites em-016's atomicity boundary inheritance. The pilot edge `em-017 → em-016` is correctly emitted (em-016 is mentioned only via the intra-spec dep in the description, not in the body); see §"Bidirectional cite cycles" below.

The pilot's deferral of 50+ forward cites to F-pilot-EM-2 is the right call under v0.6: discipline §3.2 is unambiguous that cross-spec edges to non-`depends-on` targets MUST NOT be emitted, and the AR pilot established the precedent (F-pilot-AR-2). Whether to patch EM's `depends-on` (Option A), rely on corpus-scale reciprocal-pilot resolution (Option B), or carve out a discipline rule for "co-reference forward cites" (Option C) is a discipline-author triage decision properly surfaced for synthesis.

**Result: 0 BLOCKER, 3 MAJOR, 4 MINOR findings. 5 of 7 tagged `class`.**

The most consequential finding is **F-em-r1-1 (MAJOR, class)** — a missed term-use cross-spec edge from `em-schema.transition → ar-032` is correctly emitted, BUT the pilot's §3 sensor row `em-inv-001` description claims "JSONL torn-tail" awareness without an edge, and EM-INV-001's §10.2 group is named "EM-031..EM-033 (state reconstruction)" — verified consistent with the pilot. Closer reading surfaces **F-em-r1-2 (MAJOR, class)** — the pilot does NOT emit a term-use edge for `em-schema.outcome → ar-005` even though `Outcome.kind` is defined via the §4.2 mechanism/cognition split (mode_tag vocabulary) and the pilot DID emit the parallel `em-schema.node → ar-005` edge for the `mode_tag` field. EM-005's body explicitly cites "Tags: mechanism" via `mechanism|cognition` term-use only at EM-011's level; at the schema level EM's §6.1 Node record names `mode_tag : ModeTag` with cite `[architecture.md §4.1, §4.2]` — but Outcome itself does NOT use `mode_tag`. F-em-r1-2 is therefore a re-verification flag (NOT a missed edge), confirming `em-schema.outcome` correctly emits no AR edge. Re-classified as MINOR after self-check.

## Method

Per protocol §3.3 v0.6:
1. Walked EM's body top-to-bottom; enumerated every cross-spec inline cite; classified each as hard-dep / supporting-cite / no-edge.
2. Walked pilot §2 / §3 / §4 / §5 cross-spec edges; cross-checked.
3. Verified each of the 3 reported supporting-cite reclassifications against discipline §3.1 step 1 v0.6 operational test independently.
4. F13 slot-rule heuristic verified for em-002↔em-041, em-018↔em-019; F-pilot-AR-10 verified for em-020↔em-020a.
5. Term-use edges (4 reported: ar-001×2, ar-005×1, ar-032×1) verified against discipline §3.1 step 5.
6. `depends-on` validation verified.

## Inline cites enumerated (cross-spec, normative §4/§5/§6/§8)

Every cross-spec inline cite in EM v0.3.3, classified by edge-generation status. Cites in §1, §2 (scope), §3 (glossary), §7 (pseudocode), §9 (cross-references), §10 (conformance), §11 (OQs), §12 (revision history), §A (appendices), and `> INFORMATIVE` blocks are non-edge-generating per discipline §3.1 v0.6 no-edge list (or are the §9.x-cross-ref envelope itself).

### To architecture (in `depends-on`)

| Line | Citing site | Cite text | Section | Edge? | Pilot row | Class |
|---|---|---|---|---|---|---|
| 81 | EM-001 (§4.1) | `[architecture.md §4.10]` three-artifact separation | §4 normative | NO (supporting per F-pilot-AR-10) | em-001 row notes flags supporting | Verified per operational test: removing AR §4.10 leaves "On-disk representation is a DOT document" independently testable. Reclassification stands. |
| 120 | EM-005a (§4.1) | `[architecture.md §4.6]` amendment protocol | §4 normative | NO (supporting per F-pilot-AR-10) | em-005 (coalesce) row notes flags supporting | Verified: "future variants extend via amendment protocol" is a sub-clause about future extensions; main EM-005a claim (kind discriminator + payload envelope) is independently testable. Reclassification stands. |
| 162 | EM-011 (§4.2) | `[architecture.md §4.1, §4.2]` four-axis tags + mechanism/cognition | §4 normative | YES — TERM-USE (per §3.1 step 5) | em-011 → ar-001 + em-011 → ar-005 | Verified. Section anchor cite (wide-fanout candidate: AR §4.1=4 reqs, §4.2=3 reqs) BUT term-use rule pins to AR-001 (four-axis classification) and AR-005 (mechanism|cognition split). Pilot correctly does NOT add `cite:wide-fanout` tag because term-use rule pins precisely. |
| 495 | EM-040 (§4.9) | `[architecture.md §4.9]` centralized-controller principle | §4 normative | NO (supporting per F-pilot-AR-10 + invariant-as-target exemption) | em-040 row notes flags supporting | Verified: "Submission paths that skip validation are structural violations" remains independently testable when AR §4.9 is removed; AR §4.9 is the section that contains AR-INV-007 (centralized-controller invariant), which would also fail the §3.1 step 5 invariant-as-target exemption (F-pilot-AR-r2-2). Both reasons concur. Reclassification stands. |
| 610 | §6.1 external-types defer block | `[architecture.md §4.1, §4.2]` AxisTags / ModeTag | §6 normative defer block | YES — TERM-USE (per §3.1 step 5) | em-schema.node → ar-001 + ar-005 | The defer block names AxisTags + ModeTag as "defined in [architecture.md §4.1, §4.2]". Term-use of these types in the Node record (§6.1 line 639–640: `axes : AxisTags`, `mode_tag : ModeTag`) is the load-bearing input. Pilot correctly emits two edges from `em-schema.node` (one to ar-001 for AxisTags/four-axis classification, one to ar-005 for ModeTag/mechanism|cognition). |
| 639 | §6.1 Node.axes comment | `[architecture.md §4.1]` four-axis classification | §6 normative | YES — TERM-USE (per §3.1 step 5) | em-schema.node → ar-001 (already counted) | Same as line 610 case, restated at the field-comment level. Single edge per the term-use precision rule. |
| 700 | §6.1 Transition.actor_role comment | `[architecture.md §4.8]` role name | §6 normative | YES — TERM-USE (per §3.1 step 5) | em-schema.transition → ar-032 | Verified. Section anchor (AR §4.8=5 reqs: AR-032..AR-036) BUT the load-bearing term is "role name" — AR-032 owns the seven-role vocabulary. Pilot correctly pins to AR-032 (not AR-033 deferred-roles, not AR-034 per-role-definition-scope, not AR-035 orthogonality, not AR-036 merge-responsibility). |

### To non-`depends-on` specs (forward cites; surfaced as F-pilot-EM-2)

These are normative-prose inline cites in EM §4/§5/§6 that target a spec NOT in EM's `depends-on: [architecture]`. Per discipline §3.2, no edges are emitted; per discipline §3.1, edges WOULD fire if the targets were admissible. Pilot defers all of these to F-pilot-EM-2.

| Line | Citing site | Cite text | Target | Pilot disposition |
|---|---|---|---|---|
| 81 | EM-001 (§4.1) | `[control-points.md §6.3]` policy refs | control-points | Forward (F-pilot-EM-2). Hard dep if CP were in depends-on (load-bearing for Workflow.policies field). |
| 87 | EM-002 (§4.1) | `[control-points.md §6.4]` PolicyExpression | control-points | Forward. Hard dep (Edge.condition type). |
| 105 | EM-005 (§4.1) | `[handler-contract.md §4.1]` handler obligation | handler-contract | Forward. Hard dep (Outcome shape ownership). |
| 107 | EM-005 (§4.1) | `[handler-contract.md §4.5]` ErrX classifier input | handler-contract | Forward. Hard dep (taxonomy classifier). |
| 113 | EM-005a (§4.1) | `[handler-contract.md §4.3 HC-008]` outcome_kind | handler-contract | Forward. Hard dep (specific req ID HC-008 — would be edge `em-005 → hc-008` post-resolution). |
| 118 | EM-005a (§4.1) | `[reconciliation/schemas.md §6.1]` VerdictEvent | reconciliation | Forward. Hard dep (payload type). |
| 118 | EM-005a (§4.1) | `[reconciliation/spec.md §4.5 RC-022a]` verdict-executor | reconciliation | Forward. Hard dep (specific req ID). |
| 120 | EM-005a (§4.1) | `[reconciliation/spec.md §8.11 Cat 6a]` unknown-kind routing | reconciliation | Forward. Hard dep (failure routing). |
| 136 | EM-007 (§4.2) | `[handler-contract.md §4.1]` handler registration | handler-contract | Forward. Hard dep. |
| 142 | EM-008 (§4.2) | `[control-points.md §4.11]`, `[handler-contract.md §4.11]`, `[control-points.md §6.3]` | control-points, handler-contract | Forward. Hard dep (skill resolution + policy/gate/freedom/budget refs). |
| 148 | EM-009 (§4.2) | `[control-points.md §6.3]` policy YAML defaults | control-points | Forward. Hard dep. |
| 150 | (informative) | `[reconciliation/spec.md §8.2 Cat 1]`, `[§8.3 Cat 2]` | reconciliation | NO edge (inside `> INFORMATIVE:` block per discipline §3.1 no-edge list; pilot correctly notes this in em-009 row). |
| 170 | EM-012 (§4.3) | `[workspace-model.md §4.1]` WorkspaceRef | workspace-model | Forward. Hard dep. |
| 176 | EM-013 (§4.3) | `[event-model.md §4.1]` event run_id field; `[beads-integration.md §4.6 BI-018]` Bead-ID trailer | event-model, beads-integration | Forward. Hard deps. |
| 182 | EM-014 (§4.3) | `[beads-integration.md §4.3 BI-005]`, `[reconciliation/spec.md §4.5 RC-020]`, `[workspace-model.md §4.9]` | BI/RC/WM | Forward. Hard deps. |
| 194 | EM-015a (§4.3) | `[event-model.md §8.1]` run_started; `[beads-integration.md §4.3 BI-009]` atomic-claim | event-model, beads-integration | Forward. Hard deps. |
| 201 | EM-015b (§4.3) | `[event-model.md §8.1]`, `[beads-integration.md §4.4 BI-010]` | event-model, beads-integration | Forward. Hard deps. |
| 208 | EM-015c (§4.3) | `[operator-nfr.md §4.3]` stop --immediate | operator-nfr | Forward. Hard dep. |
| 216, 220 | EM-016 (§4.4) | `[workspace-model.md §4.2, §4.9]` task-branch + lifecycle | workspace-model | Forward. Hard deps (task-branch existence precondition). |
| 234, 236 | EM-017a (§4.4) | `[reconciliation/spec.md §8.11 Cat 6a / §8.11a Cat 6b / §4.1 RC-002 / §4.1 RC-003]` | reconciliation | Forward. Hard deps. |
| 251 | EM-018a (§4.4) | `[event-model.md §4.1]` UUIDv7 | event-model | Forward. Hard dep. |
| 269 | EM-020a (§4.4) | `[reconciliation/spec.md §4.3 RC-010]` | reconciliation | Forward. Hard dep. |
| 281 | EM-022 (§4.4) | `[operator-nfr.md §4.5]` N-1 | operator-nfr | Forward. Hard dep. |
| 315 | EM-024 (§4.5) | `[reconciliation/spec.md §4.3 RC-010]` | reconciliation | Forward. Hard dep. |
| 321 | EM-024a (§4.5) | `[reconciliation/spec.md §8.4 Cat 3]` | reconciliation | Forward. Hard dep. |
| 330 | EM-025 (§4.5) | `[event-model.md §8]` failure event | event-model | Forward. Hard dep (wide-fanout — §8 covers ~14 events). |
| 345 | EM-026 (§4.5) | `[reconciliation/spec.md §4.1 RC-002]` | reconciliation | Forward. Hard dep. |
| 353 | EM-027 (§4.6) | `[handler-contract.md §4.1]`, `[control-points.md §4.3]`, `[control-points.md §4.2]`, `[event-model.md §8.1]` | HC/CP/EV | Forward. Hard deps (the spine). |
| 359 | EM-028 (§4.6) | `[event-model.md §8.1]` transition event | event-model | Forward. Hard dep. |
| 379 | EM-031 (§4.7) | `[beads-integration.md §4.2 BI-002]` br invocation; `[reconciliation/spec.md §4.3 RC-014]` divergence reads | BI, RC | Forward. Hard deps. |
| 381 | EM-031 (§4.7) | `[reconciliation/spec.md §8.11a]` Cat 6b; `[reconciliation/spec.md §8.4 Cat 3]` | reconciliation | Forward. Hard deps. |
| 388, 390, 392 | EM-031a (§4.7) | `[workspace-model.md §4.2]`, `[reconciliation/spec.md §8.1]`, `[process-lifecycle.md §4.3]`, `[reconciliation/spec.md §4.4 RC-019]`, `[workspace-model.md §4.9]` | WM/RC/PL | Forward. Hard deps (5 cites in 3 paragraphs). |
| 399 | EM-032 (§4.7) | `[reconciliation/spec.md §4.1 RC-001]` | reconciliation | Forward. Hard dep. |
| 405 | EM-033 (§4.7) | `[reconciliation/spec.md §8]` | reconciliation | Forward. Hard dep (section-anchor wide-fanout). |
| 450 | EM-036 (§4.8) | `[event-model.md §8.1]` ×2 sub-workflow lifecycle events | event-model | Forward. Hard deps. |
| 495 | EM-040 (§4.9) | `[process-lifecycle.md §4.10]` daemon RPC | process-lifecycle | Forward. Hard dep (PL-040 = command surface). |
| 515 | EM-042 (§4.10) | `[control-points.md §6.4]` guards; `[control-points.md §6.2]` gates | control-points | Forward. Hard deps. |
| 521 | EM-042a (§4.10) | `[control-points.md §6.2]` gate-resolution signal | control-points | Forward. Hard dep. |
| 552 | EM-046 (§4.10) | `[reconciliation/spec.md §4.5 RC-020]` | reconciliation | Forward. Hard dep. |
| 578 | EM-INV-004 (§5) | `[reconciliation/spec.md §8]`, `[workspace-model.md §4.2]`, `[beads-integration.md §4.10]`, `[reconciliation/spec.md §4.5]` | RC/WM/BI | Forward. Hard deps (the four-subsystem authoring surface). |
| 584 | EM-INV-005 (§5) | `[reconciliation/spec.md §8.4 Cat 3]`, `[beads-integration.md §4.7 BI-022]` | RC, BI | Forward. Hard deps (the divergence-routing surface). |
| 597 | §6.1 RunID alias | `[event-model.md §4.1]` UUIDv7 recommendation | event-model | Forward. Hard dep. |
| 601 | §6.1 BeadID alias | `[beads-integration.md §4.3 BI-008]` opaque-id | beads-integration | Forward. Hard dep (specific req ID — would be edge `em-schema.bead-id → bi-008` post-resolution). |
| 607 | §6.1 external-types defer | `[workspace-model.md §4.1]` WorkspaceRef | workspace-model | Forward. Hard dep (would be `em-schema.run → wm-001`). |
| 608 | §6.1 external-types defer | `[control-points.md §6.4]` PolicyExpression / PolicyRef | control-points | Forward. Hard deps (Edge.condition + Workflow.policies fields). |
| 609 | §6.1 external-types defer | `[handler-contract.md §4.1]` ActionDescriptor | handler-contract | Forward. Hard dep (Transition fields). |
| 621 | §6.1 Workflow.policies | `[control-points.md §6.3]` PolicyRef | control-points | Forward. Hard dep (already counted at line 608). |
| 633, 634 | §6.1 Node fields | `[control-points.md §4.11]`, `[control-points.md §6.3]` | control-points | Forward. Hard deps (already counted at EM-008). |
| 664 | §6.1 Edge.condition | `[control-points.md §6.4]` | control-points | Forward. Hard dep (already counted at EM-002). |
| 674, 677, 678 | §6.1 Run fields | `[event-model.md §4.1]`, `[workspace-model.md §4.1]`, `[beads-integration.md §4.3 BI-008]` | EV/WM/BI | Forward. Hard deps. |
| 741, 755 | §6.1 Outcome.payload, OutcomeKind | `[reconciliation/schemas.md §6.1]` VerdictEvent | reconciliation | Forward. Hard deps. |
| 771, 772 | §6.2 Workflow-Class, Target-Run-ID rows | `[architecture.md §4.6]` amendment protocol; `[reconciliation/spec.md §4.1 RC-002 / §4.1 RC-002a]` | architecture, reconciliation | The AR §4.6 cite is supporting (future-extension sub-clause; same reasoning as EM-005a→AR §4.6). The RC §4.1 cites are forward (specific req IDs, hard deps). Pilot correctly does NOT emit AR edge from `em-schema.checkpoint-trailers`; correctly defers RC. |

### Non-edge cites (correctly excluded by discipline §3.1 v0.6 no-edge list)

| Line | Section | Cite | Reason |
|---|---|---|---|
| 47–54 | §2.2 Out of scope | various | §2 is scope (not §4/§5/§6/§8). |
| 70, 71 | §3 Glossary | `[workspace-model.md §4.2]`, `[beads-integration.md §4.3 BI-005, BI-008]` | §3 is glossary (not §4/§5/§6/§8). |
| 150 | EM-009 INFORMATIVE block | `[reconciliation/spec.md §8.2 Cat 1]`, `[§8.3 Cat 2]` | INFORMATIVE block per discipline §3.1 no-edge. |
| 588 | §5 INFORMATIVE post-trailer | retired-IDs notes | Informative metadata, not a cite. |
| 758, 774 | §6 INFORMATIVE blocks | various | INFORMATIVE per §3.1 no-edge. |
| 782, 786, 794, 805–812 | §6.4, §6.5, §7.1 | various | §7 is pseudocode/state-machine prose — not in protocol §3.3's normative-cite scope. The AR pilot reviewer surfaced an analogous case (AR §10.2 line 512) as an advisory observation. The pilot correctly excludes §7 cites from edge generation; mirrors discipline §3.1 no-edge for "supporting infrastructure" prose. (Note: discipline §3.1 v0.6 no-edge list does NOT explicitly enumerate §7 prose; surfaced as F-em-r1-3 below.) |
| 884–937 | §7.4 main-loop pseudocode | many cites | Pseudocode comments and as-of-2026-04 narrative. The pseudocode comments naming `[operator-nfr.md §4.3]`, `[beads-integration.md §4.5 BI-013]`, etc., are illustrative — pseudocode is under §7 not §4/§5/§6/§8. Same as line above. |
| 944, 950, 953, 954, 957, 959, 961 | §8 taxonomy table + INFORMATIVE | `[handler-contract.md §4.5]` ×3, `[operator-nfr.md §4.3]`, `[control-points.md §4.5]`, `[architecture.md §4.6]`, `[event-model.md §8.1]` | §8 is in protocol §3.3's normative-cite scope. The HC §4.5 ErrTransient/Structural/Deterministic/Canceled/Budget cites are LOAD-BEARING for the taxonomy. Pilot correctly notes these as forward (F-pilot-EM-2) in `em-error.taxonomy` row. The AR §4.6 cite in the §8 INFORMATIVE block is non-edge per INFORMATIVE rule. |
| 967–999 | §9.x cross-references | many | §9.1/§9.2/§9.3 are co-references; per discipline §2.7 third class, generate no edges. Pilot correctly excludes. |
| 1014–1024 | §10.2 test obligations prose | `[handler-contract.md §4.5]`, `[event-model.md §8.1]`, etc. | §10 conformance prose per F-pilot-AR-AO1; no-edge per discipline §3.1 v0.6 no-edge list. |
| 1030, 1031 | §10.3 excluded | `[handler-contract.md]`, etc. | §10 conformance, no-edge. |
| 1037–1075 | §11 OQs | `[operator-nfr.md §4.5]`, etc. | §11 OQs, no-edge per §3.1. |
| 1090–1095 | §12 revision history | many | Revision history, no-edge per §3.1. |
| 1107 | §A.3 rationale | `[architecture.md §4.9]`, `[reconciliation/spec.md §8]` | §A appendix, no-edge per §3.1. |

**Total cross-spec inline cites in normative §4/§5/§6/§8 prose:** ~70 (counting each occurrence; many cites repeat across the spec).
**Total edge-generating cites:** 7 (4 distinct edges to AR via term-use + 3 supporting-cite reclassifications that produce no edges).

## Pilot edges enumerated

Pilot §5 declares 4 emitted cross-spec edges, all to AR. Plus 3 supporting-cite reclassifications (no edges).

| Pilot edge | Source-spec cite | Verdict |
|---|---|---|
| `em-011 → ar-001` | EM-011 (line 162): "four-axis tags ... per [architecture.md §4.1]" + AR-001 owns four-axis classification | VALID. Term-use rule pins precisely. |
| `em-011 → ar-005` | EM-011 (line 162): "and the `mechanism | cognition` tag per [architecture.md §4.2]" + AR-005 owns mechanism/cognition tag | VALID. Term-use rule pins precisely. |
| `em-schema.node → ar-001` | §6.1 Node.axes (line 639) "four-axis classification per [architecture.md §4.1]" | VALID. Term-use of AxisTags type owned by AR-001. |
| `em-schema.node → ar-005` | §6.1 Node.mode_tag (line 640) `mode_tag : ModeTag` + line 610 defer block "AxisTags, ModeTag — defined in [architecture.md §4.1, §4.2]" | VALID. Term-use of ModeTag type owned by AR-005. |
| `em-schema.transition → ar-032` | §6.1 Transition.actor_role (line 700): "role name per [architecture.md §4.8]" + AR-032 owns seven-role vocabulary | VALID. Term-use rule pins precisely (over AR-033..AR-036 in same section). |

**Note on count:** The pilot §5 enumerates "Total emitted cross-spec edges to AR: 4" but the bullet list is `em-011→ar-001, em-011→ar-005, em-schema.node→ar-001, em-schema.transition→ar-032` — only 4 entries. **HOWEVER**, the pilot row for `em-schema.node` (line 141 of pilot) lists "Cross-spec edges to AR. AxisTags is owned by AR §4.1 ... ModeTag is owned by AR §4.2" and the `blocks edges` cell explicitly lists `ar-001, ar-005`. So the schema bead actually emits TWO edges (one to AR-001 for AxisTags, one to AR-005 for ModeTag). This is consistent with the pilot's row table but **inconsistent with the §5 enumeration count of 4** — the actual emitted-edge count is **5** (em-011 contributes 2; em-schema.node contributes 2; em-schema.transition contributes 1).

This is **F-em-r1-4 (MINOR, local — pilot tally)** below: §5's "Total emitted cross-spec edges to AR: 4" is wrong; should be 5. The §5 narrative omits `em-schema.node → ar-005` from the 4-bullet list while listing it in §2's row notes.

## Supporting-cite reclassifications verified

Per discipline §3.1 step 1 v0.6 operational test: "Mentally remove the cited rule. Does the citing rule remain independently testable from its own surrounding text? If yes → supporting cite, no edge."

| Pilot reclass | Operational-test verdict | Hold? |
|---|---|---|
| EM-001 → AR §4.10 (three-artifact separation) | Remove AR §4.10. EM-001's body still claims "On-disk representation is a DOT document" as an EM-local rule (the DOT mandate IS EM-001's declaration). The "three-artifact separation" appended is a thematic anchor naming WHY DOT (because DOT is the workflow-graph artifact). Independently testable. | YES. |
| EM-005a → AR §4.6 (amendment protocol) | Remove AR §4.6. EM-005a's body claims "future variants extend the enum" as a closed-at-MVH posture; "via the amendment protocol" is naming the mechanism for future extensions, but the main EM-005a claim (kind discriminator + payload envelope) does not depend on AR §4.6 being implemented. Independently testable. | YES. |
| EM-040 → AR §4.9 (centralized-controller) | Remove AR §4.9. EM-040's body claims "Submission paths that skip validation are structural violations" — testable via EM-040's own validator-skip detector. The "of the centralized-controller principle" appended names WHICH principle is violated. Independently testable. The pilot also cites the invariant-as-target exemption (F-pilot-AR-r2-2) as a parallel reason — AR §4.9 contains AR-INV-007. Both reasons concur. | YES. |

**All three reclassifications hold under independent application of the operational test.** No misclassification.

## Term-use edges verified

Per discipline §3.1 step 5 v0.6: "When a requirement's body uses (without explicit inline cite) a defined term whose definition is owned by another single requirement in the same spec — including `mechanism`-tagged or `cognition`-tagged classifiers from a definition rule, named schemas, named invariants, or named roles — emit a `blocks` edge from the using requirement's bead to the defining requirement's bead. The same rule applies cross-spec when the defined term has a single-owner spec."

The 4 reported term-use edges:

1. `em-011 → ar-001` — EM-011 uses "four-axis classification" (defined by AR-001). Verified: AR-001 owns the four-axis declaration. **Valid.**
2. `em-011 → ar-005` — EM-011 uses "`mechanism | cognition` tag" (defined by AR-005). Verified: AR-005 owns the mechanism/cognition tag declaration. **Valid.**
3. `em-schema.node → ar-001` — Node record uses `AxisTags` type (defined by AR-001's four-axis classification rule). Verified. **Valid.**
4. `em-schema.transition → ar-032` — Transition record uses `actor_role : String -- role name per [architecture.md §4.8]` and the seven-role vocabulary is owned by AR-032. Verified. **Valid.**

PLUS one additional pilot edge that the pilot lists in its §2 row but not in the §5 enumeration:

5. `em-schema.node → ar-005` — Node record uses `ModeTag` type (defined by AR-005's mechanism/cognition tag). Verified. **Valid.**

All 5 term-use edges resolve to AR (in `depends-on`) and pin precisely via term-ownership rather than fanning out across section anchors.

## Bidirectional cite cycles (intra-EM) verified

The pilot §5 surfaces 3 intra-EM 2-cycles resolved via discipline §2.7 F13 slot-rule heuristic + §3.1 step 1 v0.6 supporting-cite test. Verifying each:

| 2-cycle | F13 slot direction | Pilot resolution | Verdict |
|---|---|---|---|
| em-002 ↔ em-041 | em-041 is the cascade RULE (slot defining how edges are selected). em-002 is the Edge SHAPE (content rule — what fills the cascade slot). | Emit `em-041 → em-002`; reclassify reverse `em-002 → em-041` as informational. | VALID. EM-002's body says "These fields MUST be sufficient to drive the deterministic edge-selection cascade of §4.10 without consulting any other store" — a cite to §4.10 explaining WHY the Edge fields are what they are; this is the reverse-direction (content-rule cites slot-rule for context). The pilot's resolution is correct under F13. |
| em-018 ↔ em-019 | em-018 is the slot rule (declares canonical sibling-file path). em-019 is the retrieval rule (`git show` against that path). | Emit `em-018 → em-019`; reclassify reverse `em-019 → em-018` as informational. | VALID — but with a subtle caveat: the F13 slot heuristic says "the slot points at what fills it." Here em-018 declares the path; em-019 retrieves the file at the path. Strictly, em-019 is the RETRIEVAL of what fills the slot, not the content. Either em-018 → em-019 (the path declaration anchors the retrieval method, "given path X, retrieve via Y") or em-019 → em-018 (retrieval depends on path declaration) is defensible. The pilot's chosen direction (em-018 → em-019) is consistent with F13's "slot-rule cites content/method" pattern. **Hold** — but flagged as F-em-r1-5 (MINOR, class) — the slot/content distinction in the path↔retrieval pattern is less clean than the cascade↔edge case; the discipline may benefit from a worked example. |
| em-020 ↔ em-020a | Neither is a slot rule (em-020 is the immutability invariant; em-020a is the audit-tool detector). Pilot applies F-pilot-AR-10 supporting-cite test instead. | Emit `em-020a → em-020` (audit tool blocks-on the rule it audits); reclassify `em-020 → em-020a` as supporting (em-020's "MUST NEVER be rewritten" claim is independently testable; audit-tool cite is supporting). | VALID. Operational test: remove em-020a. EM-020's "transition records MUST NEVER be rewritten" remains independently testable (a violation could be detected without the specific audit tool's existence — any check of file mutation against a sibling-file path would suffice). Reclassification holds. The reverse direction (audit tool needs the rule) is a hard dep because em-020a's job IS to detect violations of em-020. |

All three resolutions are sound under v0.6 discipline.

**Additional bidirectional structures spotted but not 2-cycles:** em-016 ↔ em-017 / em-018: em-016 declares the atomicity boundary; em-017 cites em-018 ("authoritative fields live in the sibling file of §4.4.EM-018"); em-018 cites em-016 ("inherit the atomicity boundary of §4.4.EM-016"). This is a 3-chain (em-018 → em-016, em-017 → em-018), not a 2-cycle, and forms a DAG. Pilot correctly emits these as one-direction edges with no F13 application needed. **Verified.**

## Wide-fanout tag check

Per discipline §3.1.3, section-anchor cites without specific req ID should fan out to all reqs in the section AND tag the citing bead `cite:wide-fanout`. EM has several section-anchor cites; the pilot uses the §3.1 step 5 term-use precision rule to avoid fanout where applicable.

| Section-anchor cite | Resolved via | Wide-fanout tag? |
|---|---|---|
| EM-011 → `[architecture.md §4.1, §4.2]` | Term-use pins to AR-001 + AR-005 | NO (term-use precision) — pilot correctly omits tag. |
| §6.1 defer block → `[architecture.md §4.1, §4.2]` AxisTags / ModeTag | Term-use pins to AR-001 + AR-005 | NO (term-use precision) — pilot correctly omits. |
| §6.1 Transition.actor_role → `[architecture.md §4.8]` role name | Term-use pins to AR-032 | NO (term-use precision) — pilot correctly omits. |
| EM-001 → `[control-points.md §6.3]` (policy refs) | Forward (F-pilot-EM-2) | N/A — no edge emitted. |
| EM-025 → `[event-model.md §8]` failure event | Forward (F-pilot-EM-2) | N/A — no edge emitted. |
| EM-033 → `[reconciliation/spec.md §8]` categories | Forward (F-pilot-EM-2) | N/A — no edge emitted. |
| EM-INV-004 → `[reconciliation/spec.md §8]` categories | Forward (F-pilot-EM-2) | N/A — no edge emitted. |

**Verified.** The pilot's omission of `cite:wide-fanout` tags is correct under v0.6 — every potentially-fanout cite either (a) resolves precisely via term-use (no tag needed), or (b) is forward-deferred to F-pilot-EM-2 (no edge to tag). When the deferred edges land at corpus scale, some of them WILL need `cite:wide-fanout` tags (e.g., EM-025 → ev-§8 covers ~14 events; EM-033 → rc-§8 covers ~12 RC categories; EM-INV-004 → rc-§8 likewise). Surfaced as F-em-r1-6 (MINOR, class) — discipline §3.1.3 wide-fanout marker rule should be re-applied during the F-pilot-EM-2 resolution (Option B/C), or the pilot should pre-emit `cite:wide-fanout` placeholders on those specific bead rows for downstream pilot reviewers' awareness.

## `depends-on` validation

Pilot reports: "the only emitted cross-spec edge target is `ar` (architecture); `architecture` appears in EM's `depends-on: [architecture]`. No depends-on violation."

**Verified.** All 5 emitted cross-spec edges target AR, which is in EM's `depends-on`. No depends-on violation among emitted edges.

The 50+ forward cites in normative prose are the F-pilot-EM-2 finding — not depends-on violations under v0.6 (because no edges are emitted), but evidence that EM's `depends-on` is incomplete relative to its actual citation graph. Per discipline §3.2 v0.6, this is the validation rule's purpose: surface the gap rather than emit invalid edges.

## Findings

| # | Severity | Lane | Description |
|---|---|---|---|
| F-em-r1-1 | (RETIRED — initially flagged but resolved during enumeration) | — | (originally flagged em-inv-001 sensor-edge consistency; verified consistent with §10.2 group cite during walk; not a finding.) |
| F-em-r1-2 | MINOR | local | Re-verification flag during walk: confirmed `em-schema.outcome` does NOT need an AR edge (no AxisTags/ModeTag/seven-role term-use in Outcome record). Self-resolved during enumeration. **Not a real finding** — kept here for transparency. |
| F-em-r1-3 | MINOR | class | Discipline §3.1 v0.6 no-edge list does NOT explicitly enumerate §7 (protocols / state-machines / pseudocode) prose. EM §7.1 / §7.2 / §7.3 / §7.4 contain ~15 cross-spec inline cites embedded in pseudocode comments and state-machine table cells. Pilot correctly excluded these from edge generation, but the discipline rule justifying the exclusion is implicit (§7 is "not §4/§5/§6/§8" rather than explicitly named). Other behavioural specs (PL, RC) will have similarly cite-heavy §7 sections. **Class lane suggestion:** discipline §3.1 v0.6 no-edge list should grow an explicit "§7 protocol/pseudocode prose" entry, paralleling the existing §10/§11/§12/§A entries. EM is the first behavioural spec to surface this; PL, RC, and (to a lesser extent) BI's §7 will retest. |
| F-em-r1-4 | MINOR | local | Pilot §5 enumeration says "Total emitted cross-spec edges to AR: 4" but the actual emitted-edge count from the §2/§4 row tables is 5 (em-011→ar-001, em-011→ar-005, em-schema.node→ar-001, em-schema.node→ar-005, em-schema.transition→ar-032). The §5 bullet list omits `em-schema.node → ar-005` while §2's `em-schema.node` row notes correctly list `ar-005` as a blocks edge. **Pilot patch:** add the 5th bullet to §5 ("`em-schema.node → ar-005` — term-use of `ModeTag` (defined by AR-005). §6.1 Node record's `mode_tag : ModeTag` field; the mechanism|cognition tag is owned by AR-005 per its title.") and update the total count. Confirms the §2 row table is the authoritative count; §5's narrative is mis-stated. |
| F-em-r1-5 | MINOR | class | Discipline §2.7 F13 slot-rule heuristic worked example covers cascade↔shape (em-002↔em-041 maps directly to AR-053↔AR-013/AR-052 from F-pilot-AR-8). The em-018↔em-019 case is path-declaration↔retrieval-method, which is a different shape — neither is content "filling" the slot, and the pilot's chosen direction (em-018 → em-019, slot-rule cites retrieval) is defensible but less mechanically obvious than the cascade case. **Class lane suggestion:** discipline §2.7 F13 paragraph could grow a second worked example for the path-declaration ↔ retrieval-method pattern (or, equivalently, declaration-rule ↔ method-rule), so subsequent pilots (RC's verdict-record↔verdict-execution pattern is likely; PL's startup-sequence↔component-init pattern is likely) can apply F13 without re-deriving the direction. |
| F-em-r1-6 | MINOR | class | Several deferred forward cites (EM-025→ev-§8 covers ~14 events; EM-033→rc-§8 covers ~12 RC categories; EM-INV-004→rc-§8 likewise; EM-INV-005→rc-§8.4 single Cat 3) will need `cite:wide-fanout` tags when F-pilot-EM-2 resolves at corpus scale. The pilot defers all of these without pre-emitting tag placeholders. **Class lane suggestion:** discipline §3.1.3 (wide-fanout marker) should clarify whether forward-deferred section-anchor cites SHOULD pre-emit `cite:wide-fanout` placeholders on the citing bead row for downstream pilot reviewer awareness, OR whether the tag is only emitted when the edge actually fires. Either policy is defensible; current discipline is mute. EM is the first pilot with this volume of deferred wide-fanout candidates; all the other behavioural specs will hit the same question (HC's §4.5 ErrX wide cites; EV's §8 event taxonomy; RC's §8 category taxonomy). |
| F-em-r1-7 | MAJOR | class | The pilot's F-pilot-EM-2 (50+ forward cites in normative prose) deferral is the right call under v0.6 discipline as written, BUT the volume of deferred load-bearing cross-spec dependencies is large enough that the resulting bead set, when loaded, will have a depends-on graph that under-represents the actual implementation order constraints by a factor of ~10× (5 emitted AR edges vs ~50 forward cites). At corpus scale (when EV/HC/CP/RC/WM/BI/ON/PL pilots resolve), reciprocal-direction edges from THOSE specs to EM will materialize many of the dependencies — but the EM-INV-005 sensor's predecessor list is empty intra-EM (the pilot itself flags this as F-pilot-EM-7). **Class lane suggestion:** the discipline-author triage on F-pilot-EM-2 is now load-bearing. **Strong recommendation: Option A (depends-on patch).** EM's `depends-on` should be expanded to the eight forward-cited specs. The v0.2.0 cycle-break rationale (event-model dropped to break a cycle) was correct THEN (when EM owned types and EV owned events and they mutually-blocked); now that the directional resolution is established (EM owns types, EV owns wire formats), the depends-on entry can re-add EV without re-introducing the cycle, because at the bead-edge level the cycle is broken (em-014 → ev-... NOT ev-... → em-014). Same reasoning applies to the other 7 specs: EM cites them but they do NOT cite EM in normative prose (verified by spot-check of EV's §4 against EM-014 — EV cites the run_id field's existence but in §6.x payload-shape declarations, not as a hard dep on EM-014). Option B (corpus-scale resolution) defers the gap but accumulates inertia; Option C (discipline carve-out) codifies the gap as feature, not bug. Option A is mechanically clean: patch EM v0.3.3 → v0.3.4 to expand depends-on; re-run EM pilot against the updated spec; emit the ~50 deferred edges. Pre-condition: spot-verify no NEW cycle is introduced (EM citing EV which cites EM, etc.); the v0.2.0 break was directional and SHOULD survive expansion. |
| F-em-r1-8 | MAJOR | class | The pilot reports "Schema + error-taxonomy bead count: 21 (20 schemas + 1 taxonomy)" but the §1 Counts section says "20 §6 schema constructs" — which matches. HOWEVER, the actual schema-bead enumeration in pilot §4 lists 20 entries (em-schema.run-id, state-id, transition-id, node-id, bead-id, commit-range, workflow, node, edge, run, state, transition, checkpoint, outcome, node-type, idempotency-class, transition-kind, outcome-status, outcome-kind, checkpoint-trailers) plus 1 taxonomy (em-error.taxonomy) = 21 total. **This is consistent.** But the §6.1 source spec also defines a `VerdictPayload` type alias (§6.1 INFORMATIVE block, line 758: "The `VerdictPayload` type alias is the discriminated-union payload shape"). The pilot does NOT mint a `em-schema.verdict-payload` bead. **Question:** is `VerdictPayload` a §6 declaration that triggers the §2.6 "every RECORD/INTERFACE/ENUM or constrained primitive" rule? The line is INSIDE an INFORMATIVE block but cites itself as a "TYPE alias" with semantics. Discipline §2.6 v0.5 added `kind:primitive-shape` for constrained primitives (regex/format/length/range). `VerdictPayload` is a discriminated-union ALIAS — at MVH it resolves to `VerdictEvent` only. The discipline is mute on whether discriminated-union aliases are §6 schemas. **Class lane suggestion:** discipline §2.6 may benefit from explicit guidance on (a) discriminated-union aliases / type aliases that resolve to a single MVH variant, (b) type names declared inside INFORMATIVE blocks (which are otherwise no-edge but whose declarations may be §6 normative). The pilot author's choice (no `em-schema.verdict-payload` bead) is defensible — the alias resolves to RC's `VerdictEvent` and would be redundant — but is not mechanical from the discipline. EM is the first pilot to surface this; CP's Policy/Gate union types and HC's error-sentinel union may retest. |
| F-em-r1-9 | MAJOR | class | The pilot §3 sensor table for `em-inv-005` lists "(none — see notes)" as the blocks-edges and the row notes correctly explain that all sensor-predecessor cites are forward-deferred. **Verification:** EM-INV-005's body cites `[reconciliation/spec.md §8.4 Cat 3]` and `[beads-integration.md §4.7 BI-022]` — both forward, no admissible target in `depends-on=[architecture]`. The §10.2 conformance group `EM-031..EM-033 (state reconstruction)` does NOT include EM-INV-005 (per source spec line 1021). EM has no §10.2 reviewer-persona block (per pilot §3 preamble). So the discipline §2.5 v0.6 three sources (§10.2 group, persona bundling, sensor-body inline cite) all return empty for EM-INV-005 within `depends-on=[architecture]`. **The sensor bead loads with zero predecessors.** The pilot correctly flags this as F-pilot-EM-7 (sensor predecessor degeneracy). I concur: this IS a structural finding that the discipline should address. Currently a sensor with zero predecessors is loadable but signals nothing — `br ready` would surface it as ready immediately, but its "verification" is empty until corpus-scale resolution. **Class lane suggestion:** discipline §2.5 should add a "sensor-predecessor degeneracy" sub-rule: if all three sources return empty AND the invariant body has at least one cross-spec inline cite, the sensor bead carries the transient tag `gated-by-corpus-scale` until depends-on patches or reciprocal-pilot edges materialize the predecessors. This parallels the OQ-resolution-time `gated-by-spec-edit` tag pattern from F6. EM-INV-005 is the first observed case but RC/EV/WM cross-spec invariants will likely retest. |

### Severity rollup

- **0 BLOCKER** — no invented edges, no missed edges within `depends-on=[architecture]` constraint, no depends-on violations.
- **3 MAJOR** — F-em-r1-7 (F-pilot-EM-2 lane recommendation), F-em-r1-8 (VerdictPayload §6 schema judgment), F-em-r1-9 (sensor-predecessor degeneracy rule gap).
- **4 MINOR** — F-em-r1-3 (§7 prose no-edge codification), F-em-r1-4 (pilot §5 tally off-by-one), F-em-r1-5 (F13 worked example expansion), F-em-r1-6 (forward-deferred wide-fanout tag policy).

### Lane breakdown

- **`local` lane:** 1 finding (F-em-r1-4 — pilot §5 tally arithmetic).
- **`class` lane:** 5 findings (F-em-r1-3, F-em-r1-5, F-em-r1-6, F-em-r1-7, F-em-r1-8, F-em-r1-9 — all signal discipline-rule gaps that subsequent pilots will retest).

### Notable verifications (positive)

- **All 4 (actually 5) emitted AR edges are valid** under term-use precision.
- **All 3 supporting-cite reclassifications hold** under independent operational-test application.
- **All 3 bidirectional resolutions are sound** under F13 / supporting-cite tests (with one MINOR worked-example suggestion).
- **No invented edges.** Pilot does not emit any edge to a target the spec does not inline-cite in normative prose.
- **No missed edges** within the `depends-on=[architecture]` admissible universe.
- **`depends-on` validation** clean — no targets outside `[architecture]` are emitted as edges.
- **F-pilot-EM-2 deferral is correct** under v0.6 discipline; the discipline-author triage on Option A/B/C is the proper next step (see F-em-r1-7 recommendation: lean Option A — patch EM's `depends-on`).

## Closing

The EM pilot is reference-clean within v0.6 discipline. The five emitted AR edges are mechanically derivable from term-use precision; the three supporting-cite reclassifications and three bidirectional resolutions are textbook applications of v0.6's new mechanical rules. The major outstanding question is the F-pilot-EM-2 / F-em-r1-7 lane — whether to patch EM's `depends-on` (Option A), defer to corpus-scale reciprocal pilots (Option B), or carve out a discipline rule for "co-reference forward cites" (Option C). My recommendation is Option A: the v0.2.0 cycle-break is preserved at the bead-edge level by directional resolution, and the depends-on patch is the cleanest fix; Options B/C either accumulate inertia or codify the gap as a feature.
