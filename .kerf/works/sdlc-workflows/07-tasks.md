# Pass 7 — Tasks (proposed bead set)

> One bead per workflow = "Land specs/examples/<name>.dot + scenario test for <name>." Plus a
> parent epic. The orchestrator files these (this work does NOT create beads). All workflow beads:
> `type=task`, label `codename:sdlc-workflows`, parent = the epic. NOW=P1, DEMO=P1, SOON=P2 with a
> `br dep` edge on the capability bead. Bead body MUST cite `/tmp/sdlc-corpus/_final.md` (or the
> landed corpus path) and the SPEC.md landing unit (S1) + path obligations (S2).

## Parent epic

- **EPIC** — `Land the SDLC workflow corpus (specs/examples fixtures + scenario tests)`
  - type=epic, priority=P1, label `codename:sdlc-workflows`.
  - Body: rolls up the 21 workflow beads; carries the FINAL corpus path, smoke-first order, and the
    S5 capability-dependency map. Close when 14 NOW + 2 DEMO land and the 5 SOON beads are
    filed-and-blocked on their capability beads.

## Smoke-first batch (land in THIS order)

- **W-DUAL** — `Land specs/examples/dual-review-consolidate.dot + scenario test` — P1, NOW.
  FIRST. Includes the live smoke (S3) confirming the reviewer-commit channel. Blocks W-TRIPLE,
  W-CONSENSUS.
- **W-IRF** — `Land specs/examples/implement-review-fix.dot + scenario test` — P1, NOW.
  Reference baseline (isomorphic to review-loop.dot); fixture header lists subsumed role-variants.
- **W-PLANLOOP** — `Land specs/examples/plan-review-loop.dot + scenario test` — P1, NOW.
- **W-SECREVIEW** — `Land specs/examples/security-review-loop.dot + scenario test` — P1, NOW.
  The ADDED security-axis re-role (review fix #1).

## NOW remainder (P1, no capability dep)

- **W-TRIPLE** — `Land specs/examples/triple-review-consolidate.dot + scenario test` — P1, NOW.
  The MARQUEE. `br dep add W-TRIPLE W-DUAL` (lands after the smoke confirms the channel).
- **W-CONSENSUS** — `Land specs/examples/two-reviewer-consensus.dot + scenario test` — P1, NOW.
  `br dep add W-CONSENSUS W-DUAL`.
- **W-PLANFINAL** — `Land specs/examples/plan-review-finalize.dot + scenario test` — P1, NOW.
  Re-validates terminal-by-identity (hk-z03e8) through the non-agentic finalize seam.
- **W-SPECR1R2** — `Land specs/examples/spec-R1-R2-cycle.dot + scenario test` — P1, NOW.
- **W-SPECCITE** — `Land specs/examples/spec-citation-cleanup.dot + scenario test` — P1, NOW.
- **W-DECOMP** — `Land specs/examples/decompose-review-load.dot + scenario test` — P1, NOW.
- **W-CYCLEFIX** — `Land specs/examples/dependency-cycle-fix-loop.dot + scenario test` — P1, NOW.
- **W-DOCSSYNC** — `Land specs/examples/docs-sync.dot + scenario test` — P1, NOW. WG-019 custom label.
- **W-FAILCLASS** — `Land specs/examples/review-route-by-failure-class.dot + scenario test` — P1,
  NOW. Scenario test uses synthetic outcomes; note "live-branch coverage gated on hk-1xsyu" in body.
- **W-CHARREF** — `Land specs/examples/characterize-refactor-verify.dot + scenario test` — P1, NOW.

## SOON batch (P2, capability-dependent)

- **W-GREENBUILD** — `Land specs/examples/green-build-merge-gate.dot + scenario test` — P2, SOON.
  `br dep add W-GREENBUILD hk-l8rpd`.
- **W-REGRESSION** — `Land specs/examples/regression-gate.dot + scenario test` — P2, SOON.
  `br dep add W-REGRESSION hk-l8rpd`.
- **W-RELEASE** — `Land specs/examples/release-with-rollback.dot + scenario test` — P2, SOON.
  `br dep add W-RELEASE hk-l8rpd`.
- **W-SENTRY** — `Land specs/examples/sentry-triage-faithful.dot + scenario test` — P2, SOON.
  `br dep add W-SENTRY hk-l8rpd` + `hk-sdnzj` + `hk-69asi`.
- **W-QUALGATE** — `Land specs/examples/quality-gate-policy.dot + scenario test` — P2, SOON.
  `br dep add W-QUALGATE hk-karlz`. (MAY land partial parse + reviewer-cascade test earlier.)

## DEMO arcs

- **W-D1** — `Land specs/examples/plan-to-shipped-now.dot + scenario test` — P1, DEMO.
  All-NOW topology; in-session build gate in consolidate+docs_review briefs (review fix #2).
  `br dep add W-D1 W-DUAL` (composes the marquee). Lands after the C2 NOW set.
- **W-D2** — `Land specs/examples/plan-to-shipped-faithful.dot + scenario test` — P1, DEMO.
  `br dep add W-D2 hk-l8rpd` + `hk-sdnzj` + `hk-69asi`. The north-star post-parity fixture.

## Coverage / DAG check

- **Every SPEC.md section covered:** S1 (landing unit) → all 21 W-* beads; S2 (path obligations) →
  each scenario test; S3 (smoke) → W-DUAL; S4/S5 (gating/dep map) → W-GREENBUILD..W-QUALGATE + W-D2;
  S6 (demos) → W-D1/W-D2; S7 (out of scope) → no bead (correct, DEFERRED).
- **Scenario-test bead:** every W-* IS a scenario-test bead (S1.4). **Exploratory/smoke bead:**
  W-DUAL carries the live smoke (S3) — the exploratory-test obligation.
- **DAG:** W-DUAL → {W-TRIPLE, W-CONSENSUS, W-D1}; capability beads → {SOON beads, W-D2}. No cycles.

## Notes for the filer

- `<beadshort>` in the test filename = the assigned `hk-` id's short form once filed.
- Pin each filed bead to this kerf work: `kerf pin <id> sdlc-workflows`. Set the work's
  `bead_filter` to `codename:sdlc-workflows` so `kerf next` surfaces them.
