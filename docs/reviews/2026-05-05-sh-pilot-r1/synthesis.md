# sh-pilot R1 — Synthesis

**Date:** 2026-05-05
**Reviewer pass:** R1 (3 parallel: coverage, decomposition-quality, reference)
**Pilot:** v0.1.0 → v0.1.1
**Inputs:** `coverage-r1.md`, `decomposition-r1.md`, `references-r1.md` (this directory)
**Targets patched:** `docs/decompose-to-tasks/sh-pilot.md`, `docs/decompose-to-tasks/sh-pilot-data.yaml`
**Discipline pin:** v0.9 (drafted against); v0.10 has since landed and is load-compatible (see Load gate posture).

## Aggregate

- BLOCKER: 0
- MAJOR: 4 — 2 from coverage (F-cov-MAJ-1 tally; F-cov-MAJ-2 = F-pilot-SH-4 §4.a envelope), 0 from decomposition, 2 from reference (F-ref-SH-2, F-ref-SH-4) — 3 `local`, 1 `class`
- MINOR: 12 — 3 coverage (Findings 2, 3, 5), 3 decomposition (F-decomp-SH-1/2/3), 6 reference (F-ref-SH-1/3/5/6/7/8) — 10 `local`, 2 `class` (F-ref-SH-1, F-ref-SH-8)

**Lane totals:** 13 `local` · 3 `class`.

## Triage by lane

### Pilot-patch lane (applied to sh-pilot.md / sh-pilot-data.yaml)

1. **F-ref-SH-2 (MAJOR `local`).** Added cross-spec edge `sh-schema.fixture-setup → hc-schema.launch-spec` (yaml `Cross-spec HC edges` block; pilot.md §5 HC list; pilot.md §4 schema row). Cite: `[handler-contract.md §6.1 LaunchSpec]` from the §6.1 SH `FixtureSetup` `skill_search_paths` field — textbook §3.1 step 4 type-cite. Target verified in `hc-mnem-map.csv` (`hc-schema.launch-spec, hk-8i31.74`).
2. **F-ref-SH-4 (MAJOR `local`).** Added cross-spec edge `sh-schema.scenario-file → em-schema.workflow` (yaml `Cross-spec EM edges` block; pilot.md §5 EM list; pilot.md §4 schema row). Cite: `workflow_id per [execution-model.md §4.1]` from the §6.1 SH `ScenarioFile` workflow_id field — type-cite to EM-001 Workflow record's `workflow_id` field. Target verified in `em-mnem-map.csv` (`em-schema.workflow, hk-b3f.72`).
3. **F-cov-MAJ-1 + F-cov-MIN-2 + reference §2.3 tally bonus surfacing (MAJOR `local`, knock-on MINOR).** Tally arithmetic corrected:
   - §5 closing line: 38 → 42 (was arithmetic-wrong even relative to v0.1.0's own subtotals; v0.1.0 sum-of-parts was 40, not 38).
   - §8 cross-spec subtotals: HC 13→14, EM 4→5 (other subtotals unchanged).
   - §8 total-edge claim: ~133 → 133 (v0.1.0 had drifted; actual yaml row count was 131; v0.1.1 patches add 2 edges → 133).
   - §8 intra-SH count: 93 → 91 (v0.1.0 was over-counted by 2).
4. **F-decomp-SH-3 (MINOR `local`).** Restored `detected by reconciliation mid-scenario` qualifier to `sh-019`'s store-divergence enumeration. Tightening; description-fidelity to spec body.
5. **Front matter:** version `0.1.0` → `0.1.1` in both files.
6. **Revision history:** new v0.1.1 row in pilot.md §10.
7. **Yaml header note:** updated discipline-applied note to reference the v0.1.1 patch wave.

### Discipline-patch lane

- **F-pilot-SH-4 / F-cov-MAJ-2 (§4.a envelope absence; MAJOR `class`).** Routed to v0.10 (already landed in parallel). Discipline §3.2 explicitly says: "carve-out set FROZEN at the original 7-spec set `{EM, HC, CP, WM, PL, RC, EV}`. Late-drafted specs (e.g., SH, drafted post-AR-053) MUST author §4.a per AR-053; F-pilot-SH-4 logged as a tracked follow-up (separate v0.2.1 spec patch, not authored by this discipline pass)." No SH-spec or discipline edit performed in this synthesis. The v0.2.1 spec patch is the natural resolution and is owned by a separate spec-edit work item.
- **F-ref-SH-1, F-ref-SH-8 (section-anchor `cite:wide-fanout` handling; MINOR `class`).** v0.10 *did* address this with the `cite:wide-fanout` mandatory-threshold clause (§3.1 step 3, F-refs-CP-3 / F-refs-PL-3): the tag is mandatory only when the section-anchor cite resolves to ≥2 emitted edges. SH's HC §4.8 cite (sh-011 → hc-035 + hc-036) DOES resolve to 2 edges — under v0.10 this would now MANDATE the `cite:wide-fanout` tag. Likewise SH-012's WM §4.2 cite (currently no-edge per F-pilot-AR-10 supporting reading) needs explicit no-edge logging or fan-out + tag. **Status:** v0.10 is the discipline patch this would have warranted; the rule is now in place. SH's pilot data does not currently carry the tag on `sh-011`. **Action recommended (deferred to a v0.1.2 cleanup pass or first-load reconciliation):** add `cite:wide-fanout` tag to `sh-011`'s labels in pilot data + logger commentary. Logging here so the next pilot pass (or load-reconciliation work) catches it; not blocking for load.

### Skipped (cosmetic / duplicates / supporting-cite reading defensible)

- **F-cov-MIN-3 (ON edge informational confirmation).** No-action; reference reviewer concurred.
- **F-cov-MIN-5 (§9.3 co-references confirmation).** No-action.
- **F-decomp-SH-1 (`sh-inv-005 → sh-001`/`sh-003` weakly justified).** Pilot's over-gate is defensible per F4 ("over-gating is never wrong"). No patch.
- **F-decomp-SH-2 (`sh-009`+`sh-010` borderline coalesce).** Pilot's split is defensible and consistent with BI-025a..e cumulative discipline. No patch.
- **F-ref-SH-3 (`sh-schema.agent-override → hc-043`).** Reviewer leans "supporting" — HC-043 cite is parenthetical describing the runtime-check binding owned by SH-009; schema-shape is testable from the field type alone. No edge added.
- **F-ref-SH-5 (SH-021 `[event-model.md §8.1.8]` mistargets).** Spec-text inconsistency in the SH source spec, not a pilot bug. Pilot's prose-following resolution (`run_completed`/`run_failed`) is defensible. The spec-edit fix (§8.1.8 → §8.1.2/§8.1.3 or §8.1) is a separate spec-patch task; not authored here.
- **F-ref-SH-6 (`sh-schema.event-expectation` pin to `ev-001` vs `ev-schema.event`).** Either pin defensible; pilot's choice is fine. Author discretion.
- **F-ref-SH-7 (`sh-schema.outcome-expectation` no EV edge despite §8.1.8 cite).** Bundles with F-ref-SH-5; same spec-text inconsistency. Pilot's choice to edge only EM (per type-alias-resolves-to-single-MVH-variant) is defensible.

## Convergent themes

1. **Tally arithmetic** (coverage F-cov-MAJ-1 + reference §2.3 surfacing) — applied. Single root cause: v0.1.0 §5 closing line summed v0.1.0 subtotals incorrectly (claimed 38; correct sum was 40); §8 totals had a separate parallel drift; v0.1.1 reconciles all to actual yaml row counts (91 intra + 42 cross = 133).
2. **F-pilot-SH-4 §4.a envelope absence** (coverage MAJOR `class`; pilot author self-flagged) — routed to v0.10 discipline patch (already landed); v0.10 explicitly froze the carve-out and recorded SH §4.a as a v0.2.1 spec-patch follow-up. No action here.
3. **Two missed type-cite edges** (reference F-ref-SH-2 + F-ref-SH-4) — applied. Both follow discipline §3.1 step 4 type-cite rule mechanically; the pilot author missed them at draft. New total 42 cross-spec edges (was 40 actual / 38 claimed); zero-forward-deferred property preserved (both new targets resolve to in-corpus mnemonics already loaded).

## Strengths

- **Cite-vs-edge match rate 95.2%** (40/42 expected at v0.1.0; reference reviewer's verdict). v0.1.1 patches close the gap to 100%.
- **Zero-forward-deferred achievement** (verified by reference reviewer at 30/30 distinct cross-spec target mnemonics). SH is the first pilot in the corpus with this property; new edges added in v0.1.1 also resolve to in-corpus targets, preserving the achievement.
- **Strong discipline application on hard cases.** F8b shared-function-body tiebreaker on SH-015 5-step teardown, single-table form on §8 8-class taxonomy (under threshold), §2.11(c.2) consumer→owner direction on `sh-error.taxonomy` (17 inbound edges, no inverse), and the F-pilot-AR-r2-2 invariant-as-target exemption on `sh-027 → sh-inv-004` are all textbook cumulative-discipline applications.
- **Description fidelity is uniformly high** — quantitative limits, forbidden-token lists, cross-spec cite paths, OQ forward-references, and carve-outs are all preserved verbatim in bead descriptions across the 14-bead decomposition sample. The single MINOR tightening (F-decomp-SH-3 store-divergence qualifier) is the only fidelity nit across 54 beads.

## Load gate posture

Per pilot-review-protocol §5:

- **Pilot's discipline version pin:** v0.9 (the version SH was drafted against; not speculatively re-pinned).
- **`br dep cycles`:** clean — verified post-edit on the live `.beads/` (no SH beads loaded yet, so the run reflects the prior corpus state; SH cycle-freeness will re-verify at load time).
- **Cycle expectation at SH load:** SH is the most-downstream spec in the corpus (DAG leaf); reference reviewer verified no upstream spec cites SH back. New v0.1.1 edges (`sh-schema.fixture-setup → hc-schema.launch-spec`, `sh-schema.scenario-file → em-schema.workflow`) are leaf-direction (SH→HC, SH→EM) and cannot introduce a cycle.
- **Discipline-version compatibility:** pilot is pinned v0.9; v0.10 has since landed in parallel. v0.10's behavioral changes (cycle-break carve-out for PL↔ON; wide-fanout mandatory-threshold; SHAPE-not-COUNT discriminator; backfill workflow discipline) do NOT affect SH's edge structure — SH has zero forward-deferred edges, no PL-style cycle-break, no §8 11+ split, no backfill obligations. Pilot is **load-eligible against v0.9 OR v0.10**.
- **Class-lane residue:** F-pilot-SH-4 §4.a envelope is a tracked v0.2.1 spec-patch follow-up (per v0.10 discipline decision); does not block load. F-ref-SH-1 / F-ref-SH-8 `cite:wide-fanout` tagging is a v0.1.2 cleanup candidate; does not block load.

**Ready-to-load: YES** (pinned to v0.9; v0.10-compatible).
