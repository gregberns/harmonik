# WM Pilot — R1 Review Synthesis

`synthesis-version: 1.0` — drafted 2026-04-30 by orchestrator (`hk-ahvq.14`). Combines the three parallel reviewer outputs in this directory. Lane-assignment uses pilot-review-protocol.md §4.1 four-probe triage.

## Reviewer outputs

- `coverage-r1.md` — **1 MINOR / 0 MAJOR / 0 BLOCKER**. Sole issue: tally arithmetic ambiguity (71 vs 72 epic-inclusive).
- `decomposition-r1.md` — **5 MAJOR / 1 MINOR / 0 BLOCKER**, lanes 4 local / 2 class.
- `references-r1.md` — **5 MINOR / 0 MAJOR / 0 BLOCKER**, all local. Reviewer corrected the orchestrator's pre-brief mistake (pl + bi ARE in WM depends-on, lines 22–23). Direction sanity, depends-on, F-pilot-EV-3 ev cites all clean.

## Findings table

| ID | Severity | Lane | Reviewer | Summary |
|---|---|---|---|---|
| F-cov-WM-1 | MINOR | local | Coverage | §1 + §9 sum prints `1+54+0+4+5+1+7=71` but actual = 72 (epic-inclusion ambiguity). Mechanical fix. |
| D-WM-1 | MAJOR | local | Decomposition | Narrative §6 claims 13 consumer→`wm-error.taxonomy` edges; yaml has 11 (wm-002, wm-003 correctly omitted — sentinels referenced in §7.2 pseudocode, not §4 body). Doc/data drift in narrative. |
| D-WM-2 | MAJOR | local | Decomposition | `wm-036` description omits the v0.4.2 `no-op-accept` interrupt-state-clearance side-effect (WM-040 interaction). Impl risk if developer reads bead body in isolation. |
| D-WM-3 | MAJOR | **class** | Decomposition | Missing edge `wm-inv-001 → ar-inv-007`. Pilot suppressed it citing F-pilot-AR-r2-2 invariant-as-target exemption (impl→invariant-only); F-refs-EV-6 (explicit ID cite between invariants) is the applicable rule. Rule-precedence analogous to F-em-r1-MAJ-4. |
| D-WM-4 | MAJOR | local | Decomposition | Missing edge `wm-inv-002 → em-schema.checkpoint-trailers` per §2.5 source 4 (F-em-r1-MAJ-1). |
| D-WM-5 | MAJOR | local | Decomposition | Edge `wm-inv-003 → em-inv-001` fires without explicit ID cite (only §4.7 section-anchor); may not satisfy F-refs-EV-6. Either remove or reclassify per `cite:wide-fanout`. |
| D-WM-6 | MINOR | **class** | Decomposition | F-pilot-WM-2 SHAPE-not-COUNT discriminator confirmed (12-sentinel BI-shape vs RC's 11-multi-stage threshold edge case). |
| R-WM-1 | MINOR | local | Reference | Same issue as D-WM-1 (count drift 13/11). Folded with D-WM-1. |
| R-WM-2 | MINOR | local | Reference | Missing `wm-024 → hc-schema.handler` (HC §4.1 cite); inconsistent with WM-018a's treatment. |
| R-WM-3 | MINOR | local | Reference | Missing `wm-031 → em-schema.run` for EM §7.1 cite; same in §7.1 transition table. |
| R-WM-4 | MINOR | local | Reference | Missing `wm-036 → forward:on-NNN` for `escalate-to-human` ON §4.3 cite. |
| R-WM-5 | MINOR | local | Reference | Missing `wm-schema.workspace → forward:bi-NNN` for BeadID type-alias cite (paragraph also emits CommitSHA + HandlerRef edges). |
| (sub-MINOR) | drift | local | Reference | Yaml top-comment claims `rc: 9` but actual forward-deferred is `rc: 12`. Drift in summary. |

## Triage

### Pilot-lane (10 findings)

All MAJOR-local + MINOR-local findings are mechanical patches. Apply in v0.1.1:

1. **F-cov-WM-1**: fix tally arithmetic in narrative §1 + §9 (epic-inclusive 72, OR exclude epic from sum to land 71)
2. **D-WM-1 / R-WM-1**: fix narrative §6 count claim (13 → 11)
3. **D-WM-2**: extend `wm-036` description with v0.4.2 no-op-accept clause
4. **D-WM-3**: add edge `wm-inv-001 → ar-inv-007` per F-refs-EV-6
5. **D-WM-4**: add edge `wm-inv-002 → em-schema.checkpoint-trailers`
6. **D-WM-5**: remove or reclassify `wm-inv-003 → em-inv-001` (verify whether spec §4.7 has explicit ID cite or only section anchor; if only section anchor, remove the edge or tag `cite:wide-fanout`)
7. **R-WM-2**: add `wm-024 → hc-schema.handler`
8. **R-WM-3**: add `wm-031 → em-schema.run`
9. **R-WM-4**: add `wm-036 → forward:on-NNN` (resolve the specific NNN from WM spec text — likely the `escalate-to-human` mechanic in ON §4.3)
10. **R-WM-5**: add `wm-schema.workspace → forward:bi-NNN` (resolve from spec text — likely BI's BeadID type alias declaration)
11. **(sub-MINOR)**: fix yaml top-comment `rc: 9` → `rc: 12`

### Discipline-lane (2 findings — deferred)

- **D-WM-3 (root cause)** — rule-precedence question between F-pilot-AR-r2-2 (invariant-as-target one-way: impl→invariant forbidden) and F-refs-EV-6 (invariant→invariant explicit ID cite fires). Analogous to F-em-r1-MAJ-4. Discipline batch action: add a §3.1 precedence sub-clause clarifying that F-refs-EV-6 applies to invariant→invariant cites (NOT subsumed by F-pilot-AR-r2-2's impl→invariant exemption).
- **D-WM-6 / F-pilot-WM-2** — §2.11(c) SHAPE-not-COUNT discriminator. Discipline batch action: clarify that the ~11 threshold is heuristic; flat-typed-sentinel umbrella vs. nested-category row-by-row is a SHAPE distinction, not strictly COUNT.

Per §4.2 MAJOR class normally triggers immediate patch + re-draft. **Override rationale**: D-WM-3's class-question is rule-precedence ("which existing rule wins"), not a missing rule or a wrong rule. The pilot-lane fix (add the edge per F-refs-EV-6) is mechanical and unambiguous; the precedence clarification is the discipline-side cleanup. Both rules continue to apply correctly after the pilot fix; the discipline patch tightens guidance for future pilots.

## Discipline-patch batch growing

Now four findings sitting in the discipline-lane queue for the next discipline revision (likely v0.10 after this WM patch lands — or batched after the next pilot lands):

- F-pilot-CP-7 (CP r1) — wide-fanout body-enumerated row-set refinement
- F-refs-CP-3 (CP r1) — wide-fanout tag for section-anchor → meta-prose anchors
- D-WM-3 (WM r1) — rule precedence F-pilot-AR-r2-2 vs F-refs-EV-6 for invariant→invariant
- F-pilot-WM-2 / D-WM-6 (WM r1) — §2.11(c) SHAPE-not-COUNT discriminator

## Re-run plan

Edges-only mode (`--skip-beads`). Expected: 4 net new edges added (D-WM-3, D-WM-4, R-WM-2, R-WM-3, R-WM-4, R-WM-5 = 6 add); 1 edge possibly removed (D-WM-5); 200+ already_exists. Cycles MUST remain 0.

## Outcome

WM r1 review **passes** with one larger-than-CP pilot-lane patch (10–11 mechanical changes) + 2 deferred class-lane findings batched with prior CP class findings for the next discipline patch. Phase-0 progression unaffected.
