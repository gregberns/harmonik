# RC Pilot r1 — Reference Reviewer Findings

`reviewer: reference` · `pilot-version: 0.1.0` · `discipline-version: 0.9` · `protocol-version: 0.2`

Reviewed `docs/decompose-to-tasks/rc-pilot.md` v0.1.0 + `docs/decompose-to-tasks/rc-pilot-data.yaml` against `specs/reconciliation/spec.md` v0.4.0 + `specs/reconciliation/schemas.md` v0.4.0 per pilot-review-protocol §3.3. RC has the broadest `depends-on` in the corpus (9 specs); all 9 cross-spec mnem-maps were consumed; ZERO forward-deferred placeholders expected.

## Headline counts

- Total edges: **280** (matches yaml top-comment claim).
- By target prefix: intra-rc=169, ar=5, em=22, ev=34, hc=9, cp=7, bi=11, wm=4, pl=13, on=6 — every claim matches direct count.
- Forward-deferred placeholders: **0** confirmed (3 grep hits are explanatory comments).
- Cross-spec edge targets: **all resolve** in the 9 consumed mnem-maps (verified each cross-spec target string against `/tmp/{ar,em,ev,hc,cp,bi,wm,pl,on}-mnem-map.csv`). No depends-on violations.
- Bidirectional cite cycles per F-pilot-RC-10: **3 of 3 resolved correctly** (verdict below).
- Sensor→sensor edge `rc-inv-001 → em-inv-005`: emitted, but body-cite predicate is questionable (see Finding R-3).

## F-pilot-RC-10 cycle resolution verdict

All three intra-RC bidirectional cite cycles resolved per F13 slot-rule + F-pilot-AR-10 supporting-cite test:

- **(a) rc-014 ↔ rc-019a.** Kept `rc-014 → rc-019a` (yaml line 1344); reverse not emitted. RC-014's normative body declares the JSONL-scope SLOT and emits `store_divergence_detected`; RC-019a's body fills the corroboration CONTENT — slot→content kept. F13 applies cleanly. ✓
- **(b) rc-017 ↔ rc-004.** Kept `rc-004 → rc-017` (line 1317); reverse explicitly NOT emitted with NOTE rationale at line 1360–1365. RC-004 packages YAML naming RC-017's contract; RC-017's "S01-shipped packaging (RC-004)" reference is supporting-only per F-pilot-AR-10. Removing RC-004 leaves RC-017 testable. ✓
- **(c) rc-022a ↔ rc-025a.** Kept `rc-025a → rc-022a` (line 1399); reverse explicitly NOT emitted with NOTE rationale at lines 1381–1386. RC-025a's body explicitly consumes RC-022a's outcome envelope (load-bearing); RC-022a's "daemon's reconciliation-verdict-executor (RC-025)" is supporting-only. ✓

**Verdict: F-pilot-RC-10 resolutions are sound.** The slot/content load-bearing analyses are correct; the explanatory NOTEs in yaml document the F-pilot-AR-10 reasoning at the edge sites.

## Findings

### R-1 — MAJOR — MISSED EDGE: `rc-012a → on-037` [local]

RC-012a's body (spec.md line 392) explicitly cites `[operator-nfr.md §4.9 ON-037]` health probe ("surface the failing prerequisite via [operator-nfr.md §4.9 ON-037] health probe"). `on-037` is in `/tmp/on-mnem-map.csv`. yaml emits `rc-012a → {rc-012, ev-events.daemon-degraded, pl-009, pl-010}` plus test-infra — **no edge to `on-037`**.

The pilot.md prose §3.1 explicitly lists "ON-037 health probe at RC-012a" but the yaml didn't emit it. ON is in depends-on. This is a clean missed edge by the discipline §2.7 inline-cite rule.

**Severity:** MAJOR (omits a real cross-spec dependency).
**Lane:** local (mechanical application miss; rule was clear, edge was lost between narrative and yaml).
**Fix:** Add `{from: rc-012a, to: on-037}` to the ON cross-spec section.

### R-2 — MAJOR — MISSED EDGE: `rc-015 → bi-028` [local]

RC-015's body (spec.md line 457) explicitly cites both `BI-027` and `BI-028` ("Beads-CLI skill per [beads-integration.md §4.9 BI-027/BI-028]"). yaml emits only `rc-015 → bi-027`; `bi-028` is in `/tmp/bi-mnem-map.csv` (bi-028 = "Beads-CLI skill present in every agent's launch context"). Two distinct IDs in normative prose — two edges expected per §2.7.

**Severity:** MAJOR.
**Lane:** local.
**Fix:** Add `{from: rc-015, to: bi-028}`.

### R-3 — MAJOR — MISSED EDGE: `rc-018 → ev-events.budget-exhausted` [local]

RC-018's body (spec.md line 502) explicitly cites `budget_exhausted` event "(class F per [event-model.md §8.4.3])" — distinct from `reconciliation_budget_exhausted`. yaml emits `rc-018 → ev-events.reconciliation-budget-exhausted` only; `ev-events.budget-exhausted` IS in `/tmp/ev-mnem-map.csv` (line 97, "§8.4.3"). The pilot.md F-pilot-RC-7 even calls out "RC-018 cites `[event-model.md §8]`" but doesn't capture the §8.4.3 specifically.

**Severity:** MAJOR (the §8.4.3 cite is normative-prose; the row exists; the edge is missed).
**Lane:** local.
**Fix:** Add `{from: rc-018, to: ev-events.budget-exhausted}`.

### R-4 — MAJOR — MISSED EDGE: schemas.md §6.2 cite to `em-005` [local]

`schemas.md` §6.2 (line 161) cites `[execution-model.md §4.1 EM-005]` for `resume-with-context` mechanical action ("context injected into the run's shared context per ..."). EM-005 is in mnem-map (em-005 = "Outcome is the handler-produced node result"). The verdict-execution table is consumed by RC-025; the edge should be `rc-025 → em-005` (or `rc-schema.verdict-event → em-005`). No such edge in yaml.

Note: the RC author may have intended a different EM ID — em-005's description is "Outcome ... + kind discriminator" rather than "shared context." This may be a spec-level citation mistake (a question for the coverage reviewer). For the reference reviewer, the cite is in normative prose (the verdict-execution table) and the target resolves; the edge should be emitted as written.

**Severity:** MAJOR.
**Lane:** local.
**Fix:** Add `{from: rc-025, to: em-005}` (or the corrected target if EM-005 is the wrong anchor — flag for coverage reviewer).

### R-5 — MINOR — F-pilot-RC-8 sensor→sensor edge body-cite predicate is weak [class]

The pilot's F-pilot-RC-8 claim is that RC-INV-001's BODY explicitly cites `[execution-model.md §5 EM-INV-005]`, firing the F-refs-EV-6 v0.8 sensor→sensor edge. **Verification: RC-INV-001's body (spec.md lines 668–670) does NOT contain "EM-INV-005" nor `[execution-model.md §5 EM-INV-005]`.** The cite appears only in §9.1 cross-references (line 789: "RC-INV-001 cites this as the upstream execution-model invariant").

The §9.1 entry is a normative-prose attribution by the spec author (the section is under §9 "Cross-references" → "Depends on"), but F-refs-EV-6's text explicitly says "When an invariant body explicitly cites another invariant by `<prefix>-INV-NNN` ID, a sensor→sensor `blocks` edge fires." §9.1 is a *cross-reference declaration* about what the invariant cites — not the invariant body itself.

Two interpretations:
- **Strict:** F-refs-EV-6 requires the cite in the §5 invariant body. RC-INV-001 fails this. The edge `rc-inv-001 → em-inv-005` is INVENTED. (BLOCKER per §3.3 if strict.)
- **Lenient:** §9.1 dependency declarations count as part of the spec's "explicit ID cite" surface for the invariant. Edge stands. (No issue.)

The pilot adopted the lenient interpretation. Comparable EV pilot precedent (`ev-inv-001 → em-inv-001`, the v0.8 worked example) had the cite IN THE INVARIANT BODY, not in §9.1. RC's case stretches the rule.

**Severity:** MINOR if the lenient interpretation is accepted; MAJOR/BLOCKER if strict.
**Lane:** class (the discipline rule's "body" qualifier is ambiguous; F-refs-EV-6 should be tightened to disambiguate body-vs-§9-attribution).
**Fix candidates:**
- (a) Tighten F-refs-EV-6 in discipline v0.10 to specify "body of the §5 invariant block" (excluding §9 cross-refs); drop the edge.
- (b) Loosen F-refs-EV-6 to count §9.1 declarative attributions; add worked-example clarification using RC-INV-001 → EM-INV-005.
- (c) Patch RC-INV-001's body to include the explicit `[execution-model.md §5 EM-INV-005]` cite (making the edge unambiguously valid under any interpretation).

Recommend (c) — patch RC's invariant body to add the cite — since the §9.1 entry asserts the relationship anyway. Cheapest disambiguation.

### R-6 — MINOR — Pilot.md §6 narrative undercount/mis-attribution of consumer→taxonomy edges [local]

The pilot.md §6 prose claims 26 consumer→taxonomy edges via a specific breakdown (`RC-007 → umbrella; RC-008 → umbrella + 8 per-cat; ... RC-013 → umbrella; ... RC-018 → cat-3b; RC-022a + RC-025 + RC-025a → umbrella; ... RC-INV-001 → umbrella; RC-INV-004 → cat-6b`). The yaml emits 26 edges as claimed — but the **enumeration in pilot.md prose disagrees with what's in the yaml** at several points:

| Claimed in pilot.md §6 | In yaml? |
|---|---|
| `rc-013 → taxonomy` | NO (no edge from rc-013 to a taxonomy bead) |
| `rc-018 → cat-3b` | NO (rc-018 → rc-026a only; no direct cat-3b edge) |
| `rc-022a → taxonomy` | NO |
| `rc-025 → taxonomy` | NO |
| `rc-025a → taxonomy` | NO |
| `rc-inv-001 → taxonomy` | NO |
| `rc-010 → cat-5` | YES (in yaml; not in pilot.md narrative) |
| `rc-016 → cat-2/cat-3/cat-6a` | YES (3 edges in yaml; not in pilot.md narrative) |

The **count happens to be 26 either way**, but the documentation and yaml are out of sync. This is cosmetic at the count level but confuses any reviewer cross-walking the pilot.md narrative against the yaml. No discipline-rule issue.

**Severity:** MINOR (cosmetic — pilot.md narrative drift from yaml).
**Lane:** local.
**Fix:** Update pilot.md §6 prose to enumerate the actual consumer→taxonomy edges in the yaml (or update yaml to match pilot.md if some of the missing edges should have been emitted — RC-013's body cites "§8 category" generically, which may justify an edge per §2.11(c.2)).

### R-7 — MINOR — Potentially missed `rc-error.cat-6a → em-018` per-category edge [class]

§8.11 Cat 6a's detection rule (spec.md line 205) cites `[execution-model.md §4.4 EM-018]` ("trailer-vs-sibling-file mismatch ... per EM-018"). The discipline §2.11(c.2) edge-direction rule routes consumer→category-bead, not category-bead→external. The §8.11 detection rule narrative is part of the per-category bead's body. RC-014's edge `rc-014 → em-018` already covers the cite at the §4.3 level, so the relationship is captured globally — but if per-category beads carry their own normative cites in their detection rules, they should arguably emit their own cross-spec edges.

This is more a **discipline question** (do per-category §8 beads emit cross-spec edges from their detection-rule prose?) than a clear missed edge. The pilot is silent on the question. Not flagged as a blocker.

**Severity:** MINOR.
**Lane:** class (discipline §2.11(c) is mute on whether per-category beads emit their own cross-spec citation edges; current practice routes everything via §4 reqs).
**Fix:** Document the convention explicitly in v0.10 §2.11(c.2) (likely "no — cross-spec edges from §8.x detection-rule prose are routed via the citing §4 req, not the per-category bead").

## Section-anchor cite-handling

Per §3.1.3, `cite:wide-fanout` tag verified. yaml beads carrying `cite:wide-fanout`:
- rc-001 ✓
- rc-014 ✓
- rc-018 ✓
- rc-022 ✓
- rc-025 ✓

Pilot.md §1 + F-pilot-RC-7 narrative claim 6 tags including "the spec.md §6.5 dual-table reference RC-007 cites which expands to action-mapping rows." But yaml shows only 5 wide-fanout tags (RC-007 not tagged). RC-007 cites `[schemas.md §8.12]` and `[schemas.md §6.3]` — both single-anchor; arguably not wide-fanout. The "6th" claim in F-pilot-RC-7 narrative is unsupported. Cosmetic; no edge consequence.

## Direction-sanity

- **26 consumer→`rc-error.taxonomy` (or per-category) edges** — count verified at 26 (see R-6 for breakdown discrepancy). Direction is consumer→owner per §2.11(c.2) ✓.
- **28 RC→EV-row edges** per §2.11(d.2) — direct count of `rc-* → ev-events.*` edges in yaml = **22** (not 28), plus `rc-error.cat-* → ev-events.*` edges = 12 → total `*→ev-events.*` = 22 + 12 = **34**. The "28" figure in the prompt likely refers to the §6.5 events list cross-product (12 events × ~2-3 emission sites each); the actual count is 34. Direction is correct (consumer→event-row) per §2.11(d.2). ✓
- **F-pilot-RC-8 sensor→sensor edge `rc-inv-001 → em-inv-005`** — emitted (line 1537). Body-cite predicate is suspect; see R-5.

## Forward-deferred check

**ZERO forward-deferred placeholders** as expected. RC is the LAST pilot in the corpus; every cross-spec target resolves directly. Confirmed by `grep -c "forward:" rc-pilot-data.yaml = 3` — all three matches are explanatory comments, not edge entries.

## Corpus-completion implication

RC's `/tmp/rc-mnem-map.csv` publication closes the corpus-wide forward-deferred backfill cycle. Heavily-cited RC beads (high inbound from prior pilots' `forward:rc-NNN` placeholders) will be:

- **`rc-error.taxonomy`** — umbrella; cited by every spec consuming the reconciliation taxonomy (PL, ON, EM, EV, WM at minimum).
- **`rc-error.cat-3a`** — heavily cited by BI (intent-log idempotency) per BI-029/BI-031/BI-031b.
- **`rc-error.cat-3b`** — cited by EM/PL for verdict-unexecuted classification.
- **`rc-014`** — cited by EM (§4.7 forbidden-uses) and EV (§4.5 divergence-evidence reader).
- **`rc-019a` / `rc-inv-004`** — cited by EV (EV-023a corroboration projection).
- **`rc-002a` / `rc-002b`** — cited by PL (PL-002a / PL-006 lock orchestration).
- **`rc-015` / `rc-015a` / `rc-022a` / `rc-025a`** — cited by HC (HC-006 / HC-008 / HC-011 outcome envelope).
- **`rc-027`** — cited by ON (operator-CLI grammar surface).
- **`rc-schema.workflow-class-extension`** — cited by EM (Workflow record extension acceptance per OQ-RC-002).
- **`rc-schema.verdict` (7-value enum)** — cited by WM-036 (worktree disposition table per OQ-RC-011).

The eventual `hk-ahvq.NN` Beads-IDs for these beads will form the bulk of the corpus-wide backfill scope (per HANDOFF.md / `hk-ahvq.37`).

## Summary — for synthesis

| ID | Severity | Lane | Headline |
|---|---|---|---|
| R-1 | MAJOR | local | Missed edge `rc-012a → on-037` (ON-037 cited in body) |
| R-2 | MAJOR | local | Missed edge `rc-015 → bi-028` (BI-028 cited in body, separate from BI-027) |
| R-3 | MAJOR | local | Missed edge `rc-018 → ev-events.budget-exhausted` (§8.4.3 class F event distinct from reconciliation_budget_exhausted) |
| R-4 | MAJOR | local | Missed edge to `em-005` from schemas.md §6.2 verdict-execution table |
| R-5 | MINOR | class | F-pilot-RC-8 sensor→sensor edge stretches F-refs-EV-6 (cite in §9.1 not invariant body); recommend body-cite patch |
| R-6 | MINOR | local | Pilot.md §6 narrative enumeration of consumer→taxonomy edges drifts from yaml |
| R-7 | MINOR | class | Per-category §8 bead detection-rule cross-spec cites — discipline silent |

**No invented edges, no depends-on violations, no unresolved cycles, zero forward-deferreds.** Cross-spec edge target resolution: 100% (every cross-spec target string in yaml is in the corresponding mnem-map). Direction discipline (§2.11(c.2) consumer→owner; §2.11(d.2) consumer→event-row; sensor↔impl one-way; F13 slot→content) applied correctly.

The four MAJOR findings (R-1 through R-4) are mechanical citation-walk misses of cross-spec edges whose targets exist in the consumed mnem-maps; all four are local-lane and patch into the pilot directly. R-5 is a class-lane discipline question (body-vs-§9 cite predicate) worth raising in synthesis. R-6/R-7 are cosmetic.
