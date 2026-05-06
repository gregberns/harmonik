# PL Pilot — R1 Review Synthesis

`synthesis-version: 1.0` — drafted 2026-04-30 by orchestrator (`hk-ahvq.21`). Combines the three parallel reviewer outputs in this directory. Lane-assignment uses pilot-review-protocol.md §4.1 four-probe triage.

## Reviewer outputs

- `coverage-r1.md` — **1 MINOR / 0 MAJOR / 0 BLOCKER**. Wording polish in §10.
- `decomposition-r1.md` — **0 BLOCKER / 2 MAJOR / 5 MINOR**. Both MAJORs `class`-tagged.
- `references-r1.md` — **0 BLOCKER / 3 MAJOR / 3 MINOR**. F-pilot-PL-4 explicit lane recommendation: `class`, MEDIUM priority.

## Findings table

| ID | Severity | Lane | Reviewer | Summary |
|---|---|---|---|---|
| F-cov-PL-1 | MINOR | local | Coverage | §10 revision-history wording: enumeration sums to 60 but prose says 59. No count error; pure polish. |
| D-PL-1 | MAJOR | **class** | Decomposition | PL-005's 11-step F8b collapse may exceed precedent envelope. Steps have radically diverse code paths (composition-root bootstrap / lock acquisition / git log walk / Beads query / in-memory model build / marker reads). F8b precedent (BI-031 state-machine, EM-016 3-op git atomic) involves shared code path, not delegated multi-target sequence. |
| D-PL-2 | MAJOR | **class** | Decomposition | PL-011's 9-step F8b collapse; weaker than D-PL-1 but step 3 is a 3-case sub-protocol; steps 6/7/8/9 each have independent failure modes. |
| D-PL-3..7 | MINOR | local | Decomposition | Sub-clause omissions in 5 bead descriptions: PL-005 step 5/6 mechanics, PL-011 step-3 aggregation rule, PL-014a `min(4096, hard)` + warn-on-failure, PL-009 bullet collapse, PL-INV-005 sensor borderline restatement. |
| F-pilot-PL-4 | (re-review) | **class** | Reference (re-confirms author's class-tag) | 26 `forward:on-*` edges despite ON not in depends-on. All four §4.1 probes return class/yes. Recommended carve-out clause for §3.2 — `cycle-break-named-obligation` — analogous to §2.11(d.2) co-owned event payload pattern. Pilot-side: drop 26 edges and re-emit under new rule when it lands. |
| F-refs-PL-7 | MAJOR | local | Reference | **Critical: load-time cycle risk.** `pl-018 → ar-inv-007`, `pl-019 → ar-inv-007`, `pl-020a → ar-inv-007` violate F-pilot-AR-r2-2 invariant-as-target exemption (impl→invariant forbidden). F-refs-EV-6 covers sensor→sensor only. Only `pl-inv-002 → ar-inv-007` is correct. |
| F-refs-PL-1 | MAJOR | class→**local override** | Reference | PL-INV-001 body cites `[beads-integration.md §4.10 BI-030]` and `[workspace-model.md §4.3 WM-013a]` but yaml emits no edges. Per §2.5 source 4 (F-em-r1-MAJ-1) these should fire. Reviewer self-classified `class`; orchestrator overrides to `local` because the rule exists in v0.9 and the pilot simply missed applying it (CP/WM applied this rule correctly). |
| F-refs-PL-3 | MINOR | class | Reference | Zero `cite:wide-fanout` tags in pilot despite multiple section-anchor cites (CP §4.1, EV §4.1/§4.3/§6.2, etc.). Sibling of F-refs-CP-3 (CP r1) — when is the tag mandatory? |

## Triage

### Pilot-lane (v0.1.1 patch)

1. **F-refs-PL-7 (MAJOR, critical)** — REMOVE 3 inverted edges: `pl-018 → ar-inv-007`, `pl-019 → ar-inv-007`, `pl-020a → ar-inv-007`. Keep `pl-inv-002 → ar-inv-007`. Critical for next-reload cycle prevention.
2. **F-refs-PL-1 (override to local)** — ADD 2 edges: `pl-inv-001 → bi-030`, `pl-inv-001 → wm-013a`.
3. **F-refs-PL-3 (MINOR)** — ADD `cite:wide-fanout` `extra_labels` block to citing beads with section-anchor cites. Patch agent should identify the specific beads from spec walk.
4. **D-PL-3..7 (5 MINOR)** — extend bead descriptions for sub-clause omissions. Patch agent reads spec for each (PL-005 step 5/6 mechanics, PL-011 step-3 aggregation, PL-014a `min(4096, hard)` + warn-on-failure, PL-009 bullet, PL-INV-005 refinement).
5. **F-cov-PL-1 (MINOR)** — wording polish in §10 revision-history.

Bump pilot-version `0.1.0 → 0.1.1`. Update top-comment with patch summary. Update pl-pilot.md §10.

### Discipline-lane (deferred — batched)

Three findings join the discipline-batch queue (now seven total):

- **D-PL-1, D-PL-2** — F8b envelope clarification. Where exactly does "shared function body" end? PL-005's 11 steps with diverse code paths arguably exceed BI-031 / EM-016 precedent. Discipline patch action: tighten F8b's applicability — possibly "steps must share a single Go function's body OR a tight cluster of mutually-recursive sub-functions." Affects ~4 PL beads (PL-005, PL-006, PL-011, PL-027) potentially expanding to ~30 step beads if patch lands strict — DEFERRED, NOT applied in v0.1.1.
- **F-pilot-PL-4** — §3.2 `cycle-break-named-obligation` carve-out. The reviewer's recommended patch text is in `references-r1.md`. Affects 26 PL forward:on-* edges + similar pattern in CP yaml (4 forward:on-*) per `hk-ahvq.46` tracker. Pilot-lane removal action **deferred** until discipline lands.
- **F-refs-PL-3 root** — `cite:wide-fanout` mandatory-threshold. Sibling of F-refs-CP-3 — discipline-lane batch already includes the wide-fanout question.

### Override rationale: F-refs-PL-1 class→local

Reviewer self-tagged `class` arguing the rule's invariant-body term-use sub-clause needs more visibility. Override: §2.5 source 4 + F-em-r1-MAJ-1 are explicit in v0.9 discipline. CP and WM pilots both applied the rule correctly (e.g., WM's r1 added similar invariant-body edges per D-WM-4). PL just missed it. Per §4.1 last paragraph: override permitted "with a stated reason." Stated reason: rule exists; application error.

### Override rationale: D-PL-1, D-PL-2 → discipline-lane DEFERRED (no immediate pilot rewrite)

Per §4.2 MAJOR-class normally triggers immediate discipline patch + re-draft. Override: re-drafting PL with split multi-step beads would expand 4 beads → ~30 beads, invalidating loaded mnem-map and forcing a full re-load + cross-spec edge re-resolution. Cost is high, the discipline question is genuinely under debate (where does F8b's envelope end?), and the pilot's loaded state is structurally clean (cycles=0, no impl divergence). Track in discipline batch for considered revision; if v0.10 lands a stricter F8b, plan a coordinated re-draft pass for PL/RC/ON.

## Discipline-patch lane batch growing

**Seven findings now queued** for next discipline revision (likely v0.10 after RC or post-corpus):
- F-pilot-CP-7 + F-refs-CP-3 (CP r1) — wide-fanout body-enumerated row-set + section-anchor wide-fanout
- D-WM-3 (WM r1) — invariant→invariant rule precedence
- F-pilot-WM-2 / D-WM-6 (WM r1) — §2.11(c) SHAPE-not-COUNT
- D-PL-1, D-PL-2 (PL r1) — F8b envelope tightening
- F-pilot-PL-4 (PL r1) — §3.2 cycle-break-named-obligation carve-out
- F-refs-PL-3 (PL r1) — `cite:wide-fanout` mandatory threshold (sibling of F-refs-CP-3)

## Re-run plan

`--skip-beads` edges-only mode. Net edge changes:
- REMOVE: 3 (F-refs-PL-7 inverted edges)
- ADD: 2 (F-refs-PL-1 invariant-body edges) + N from F-refs-PL-3 wide-fanout-tag patches (label-only, not edge changes)

Loader expects: ~5 net edge changes; ~196 already_exists; cycles MUST remain 0.

## Outcome

PL r1 review **passes with a critical pilot-lane fix (F-refs-PL-7) and 7 mechanical fold-ins for v0.1.1**, plus 3 deferred class-lane findings batched with prior CP+WM class findings for the next discipline patch. F-pilot-PL-4 §3.2 violations remain in yaml pending discipline-lane resolution (tracked under `hk-ahvq.46`). Phase-0 progression unaffected.
