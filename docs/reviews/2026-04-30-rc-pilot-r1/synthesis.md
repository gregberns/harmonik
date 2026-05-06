# RC Pilot — R1 Review Synthesis

`synthesis-version: 1.0` — drafted 2026-04-30 by orchestrator (`hk-ahvq.35`). Combines the three parallel reviewer outputs.

## Reviewer outputs

- `coverage-r1.md` — **2 MINOR cosmetic typos**, bead-graph coverage COMPLETE
- `decomposition-r1.md` — **3 MINOR cosmetic + 2 RESOLVED-CONFIRMATION class findings** (F-pilot-RC-4 + F-pilot-RC-5 verdicts both (a))
- `references-r1.md` — **4 MAJOR / 3 MINOR**, all local mechanical edge omissions

## Headline findings

### F-pilot-RC-4 verdict: (a) RESOLVED — RC IS the canonical large-§8 case
RC's 11 categories have independent detectors, dispatch paths, emitted events, and 4 distinct response shapes. Per-category split is correct; **first canonical clean-direction example for discipline v0.9 §2.11(c.2)**. **RESOLVES F-pilot-WM-2 SHAPE-not-COUNT** discriminator question.

### F-pilot-RC-5 verdict: (a) RESOLVED — F8b worked-example pair complete
RC-018 + RC-025a both collapsed correctly via cohesive-function-body. PL + EM + RC = 3-pilot precedent for cohesive-function-body collapse; ON-027 = single precedent for delegating-orchestrator split. **F8b worked-example pair complete** for v0.10 docs patch.

## Findings table

| ID | Severity | Lane | Summary |
|---|---|---|---|
| C-RC-1 | MINOR | local | Pilot §1 prose count typos (§4.1=10 not 9; §4.3=9 not 8; sum 41≠stated 43). Bead set complete. |
| C-RC-2 | MINOR | local | Schema split label drift (claimed "10R + 5E"; actual 9R + 6E or similar). 15-total correct. |
| D-RC-1 | MINOR | local | `rc-schema.workflow-class-extension`: `kind:enum` extra_label conflicts with `schema_kind: record`. |
| D-RC-2 | MINOR | local | Pilot prose §6 lists umbrella consumer edges yaml emits to specific per-category beads (yaml correct). |
| D-RC-3 | MINOR | local | Pilot prose lists `rc-018 → rc-025a` direct; yaml emits via `rc-026a` transitive (yaml correct under F-pilot-AR-10). |
| R-1 | MAJOR | local | Missed `rc-012a → on-037` (RC-012a body cites `[operator-nfr.md §4.9 ON-037]`). |
| R-2 | MAJOR | local | Missed `rc-015 → bi-028` (RC-015 body cites BOTH BI-027 + BI-028; only BI-027 edge emitted). |
| R-3 | MAJOR | local | Missed `rc-018 → ev-events.budget-exhausted` (RC-018 body cites both `budget_exhausted` and `reconciliation_budget_exhausted`; only latter emitted). |
| R-4 | MAJOR | local | Missed `rc-025 → em-005` (schemas.md §6.2 verdict-execution table cites `[execution-model.md §4.1 EM-005]` for `resume-with-context`). |
| R-5 | MINOR | **class** | F-pilot-RC-8 sensor→sensor edge `rc-inv-001 → em-inv-005` cites §9.1 attribution rather than invariant body; F-refs-EV-6's "body" predicate is ambiguous. Discipline-batch refinement candidate. |
| R-6 | MINOR | local | Pilot.md §6 narrative enumeration drifts from yaml (still totals 26). |
| R-7 | MINOR | **class** | Discipline silent on whether per-category §8 beads emit their own cross-spec edges (vs only the umbrella). |

## Triage

### Pilot-lane (v0.1.1, ~9 changes)

1. **R-1, R-2, R-3, R-4** (4 MAJOR): ADD 4 missing cross-spec edges
2. **C-RC-1, C-RC-2** (cosmetic): fix narrative count typos in §1 + §6
3. **D-RC-1** (cosmetic): fix `kind:enum` ↔ `schema_kind: record` label conflict on `rc-schema.workflow-class-extension` (verify which is correct against schemas.md)
4. **D-RC-2, D-RC-3, R-6** (narrative drift): tighten narrative to match yaml; yaml is canonical

### Discipline-lane batch additions (now 14 entries)

- **R-5**: F-refs-EV-6 "body" predicate — does §9.1 attribution count as body? Discipline clarification.
- **R-7**: Per-category §8 bead cross-spec edge guidance — when cite is to specific category, edge target is per-category bead; when cite is to taxonomy generally, edge target is umbrella. Discipline §3.1 step 3 sub-clause candidate.
- **F-pilot-RC-4 RESOLVED-CONFIRMATION**: replaces F-pilot-WM-2 SHAPE-not-COUNT discriminator entry. Documentation patch only.
- **F-pilot-RC-5 RESOLVED-CONFIRMATION**: completes F8b worked-example pair (PL/EM cohesive + ON delegating + RC cohesive); collapses with D-PL-1/D-PL-2 + F-pilot-ON-5 into ONE documentation patch.

## Re-run plan

Edges-only mode (`--skip-beads`). Net: ADD 4 edges (R-1..R-4); 0 removals. Cycles must remain 0.

## Outcome

RC r1 review **passes with 9 mechanical pilot-lane fixes for v0.1.1** + 2 RESOLVED-CONFIRMATION class findings (no patch — RC is canonical case for both) + 2 discipline-lane refinement candidates (R-5, R-7).

**LAST PILOT MILESTONE**: After v0.1.1 patches land, RC backfill (`hk-ahvq.37`) closes the corpus-wide forward-deferred cycle. Every prior pilot's `forward:rc-NNN` placeholders become resolvable.
