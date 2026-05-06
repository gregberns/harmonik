# Phase 0 — Milestone Close

**Date:** 2026-05-06
**Bead:** `hk-ahvq.42` (Phase 0 exit — milestone)
**Predecessors:** `hk-ahvq.38` (full-union cycle check), `hk-ahvq.39` (forward-zero verification), `hk-ahvq.40` (mnem-map consolidation), `hk-ahvq.41` (bootstrap subset), `hk-pvcs` (build/test scaffolding meta-epic), `hk-jhob.1`, `hk-jhob.2` (foundation skills), `hk-kle6.1`, `hk-kle6.2` (Phase-1 validation epic)
**Companion:** [`docs/foundation/phase-1-readiness-gap-analysis.md`](phase-1-readiness-gap-analysis.md) v0.2 (the gap analysis this close discharges)

## Outcome

Phase 0 is closed. The plan has been refined to a normative spec corpus (11 specs, all `reviewed`), the corpus has been decomposed into a task ledger (`.beads/`, ~905 live beads, 3,589 dependency edges, zero cycles), the bootstrap subset has been identified and labelled (`scope:bootstrap` = 376 beads — 348 corpus + 28 meta-epic), and the readiness gaps surfaced by `phase-1-readiness-gap-analysis.md` have been closed in beads. Agents can now begin Phase 1 implementation by claiming work via `br ready -l scope:bootstrap`. The natural starting point is the build/test scaffolding epic `hk-pvcs` (8 beads), which unblocks all subsequent code commits by establishing the local `make check-fast` / `check` / `check-full` gauntlet plus agent-reviewer hook.

## Spec corpus snapshot

11 normative specs reviewed and frozen at the IDs listed below. Every spec has had two review rounds (R1 + R2) plus integration; SH was added in this session at v0.2.0 and patched to v0.2.1 to add the §4.a subsystem envelope per AR-053 (`hk-ahvq.47`). All requirement IDs are PERMANENTLY FROZEN — no renumbering or ID reuse.

| Spec | File(s) | Version | Status | §4 req IDs |
|---|---|---|---|---:|
| architecture | `architecture.md` | 0.3.1 | reviewed | 53 |
| execution-model | `execution-model.md` | 0.3.3 | reviewed | 65 (+EM-005a) |
| event-model | `event-model.md` | 0.3.4 | reviewed | 48 |
| handler-contract | `handler-contract.md` | 0.3.3 | reviewed | 63 (+HC-016a, HC-026b) |
| control-points | `control-points.md` | 0.3.2 | reviewed | 55 |
| workspace-model | `workspace-model.md` | 0.4.2 | reviewed | 53 |
| process-lifecycle | `process-lifecycle.md` | 0.4.1 | reviewed | 42 |
| operator-nfr | `operator-nfr.md` | 0.4.1 | reviewed | 61 |
| reconciliation | `reconciliation/{spec,schemas}.md` | 0.4.0 | reviewed/supplement | 43 |
| beads-integration | `beads-integration.md` | 0.4.1 | reviewed | 43 |
| scenario-harness (SH / S07) | `scenario-harness.md` | 0.2.1 | reviewed | 36 |

Cumulative: **~562 unique requirement IDs** across the 11 specs. Today's net-new IDs (this session and Wave-1 closeout): EM-005a, HC-016a, HC-026b, plus three new EV §8.2 event-row identifiers (`§8.2.10` `control_points_registration_started`, `§8.2.11` `verdict_envelope_mismatch`, `§8.2.12` `policy_expression_exceeded_cost`) added by the EV r2 spec patch (`hk-ahvq.45`); these resolve the F-pilot-CP-3 EV-completeness gap. The §8.2 row identifiers are not §4 requirement IDs and are not counted in the 562 total.

## Decomposition snapshot

11 pilots authored, reviewed (3-reviewer protocol per `pilot-review-protocol.md`), and loaded into `.beads/` under prefix `hk`:

| Pilot | Pilot doc | Spec epic | Beads (corpus children) |
|---|---|---|---:|
| AR | `ar-pilot.md` v0.2.x | `hk-zs0` | 55 |
| EM | `em-pilot.md` v0.1.x | `hk-b3f` | 90 |
| EV | `ev-pilot.md` v0.1.x | `hk-hqwn` | 145 |
| HC | `hc-pilot.md` v0.1.x | `hk-8i31` | 86 |
| CP | `cp-pilot.md` v0.1.2 | `hk-a8bg` | 86 |
| WM | `wm-pilot.md` v0.1.x | `hk-8mwo` | 72 |
| PL | `pl-pilot.md` v0.1.x | `hk-8mup` | 60 |
| ON | `on-pilot.md` v0.1.x | `hk-sx9r` | 85 |
| RC | `rc-pilot.md` v0.1.x | `hk-63oh` | 80 |
| BI | `bi-pilot.md` v0.1.3 | `hk-872` | 66 |
| SH | `sh-pilot.md` v0.1.2 | `hk-i0tw` | 58 |

**Live `.beads/` totals (post-Wave-1, post-Wave-2 closeout):**

- 16 epics (10 spec implementation + SH + Phase-0 parent + 4 meta-epics: `hk-pvcs`, `hk-jhob`, `hk-kle6`, `hk-ahvq.48`).
- 905 live issues (issues minus closed minus tombstoned). Db-resident: 951 total (44 closed, 2 tombstoned).
- 3,589 edges in total: 2,652 `blocks`, 934 `parent-child`, 3 `related`.
- `br dep cycles` = clean (zero cycles) corpus-wide. Verified at `hk-ahvq.38` close (2026-05-01) and re-verified across every Wave-1 and Wave-2 yaml mutation since.

## Bootstrap-subset snapshot

`scope:bootstrap` label applied to 376 beads:

- **348 spec-corpus beads.** 345 from the v0.2 synthesis (`bootstrap-subset.md`) plus 3 EV §8.2 rows (`hk-hqwn.59.79/.80/.81`) minted today by the EV r2 patch and labelled at mint per discipline §2.13. The 345 split: PL 37, WM 45, EM 65, HC 46 (45+1 PULL_IN), EV 47 (42 + 5 PULL_INs), BI 36, SH 54, AR 5, ON 6, RC 4, CP 0 (fully deferred per `bootstrap-subset.md` §1).
- **28 meta-epic beads.** `hk-pvcs` (1 epic + 8 children = 9 beads) — local Makefile/golangci/lefthook/coverage scaffolding; `hk-ahvq.48` (1 mini-epic + 9 children = 10 beads) — twin-binary scaffolding plus first 3 conformance scenarios; `hk-jhob.1` and `hk-jhob.2` (2 of 4 children) — agent-reviewer + beads-cli skills (other two `hk-jhob.3` and `hk-jhob.4` are unscoped post-MVH); `hk-kle6` (1 epic + 2 children = 3 beads) — Phase-1 validation; `hk-b3f.89` (standalone) — MVH composition-root no-op PolicyEngine, resolves §A5; plus `hk-jhob` (the operational-skills meta-epic itself) and `hk-kle6` (validation epic). Sum: 8 + 10 + 2 + 3 + 1 + 2 epic envelopes + 2 outliers = 28.

The corpus-wide `br list -l scope:bootstrap` returns 376 (verified 2026-05-06).

`post-mvh` label: 5 beads (distributed-tracing, metrics-exposition, multi-tenancy, binary-signing, `bead_terminal_transition_recovered`). The remaining ~520 spec-corpus beads carry neither tag and are tracked as `hk-kle6.2` for the Phase-1-entry corpus label-reconciliation pass; that bead is itself in `scope:bootstrap` so the pass runs as the first labelled task once an agent claims it.

`br dep cycles` clean across the union after every closeout-pass yaml mutation.

## Discipline state

`docs/decompose-to-tasks/discipline.md` at **v0.12.** Versioned history: v0.1 (initial 10 rules from BI pilot) → v0.2 → v0.3 (F2 BLOCKER: parked-state lifecycle replaced with native `draft`) → v0.4 (BI smoke-load 6-finding pass) → v0.5 (AR r1 6 findings) → v0.6 (AR r2 4 findings) → v0.7 (EM r1 10-finding pass + 2 policy decisions) → v0.8 (EV r1 5-patch pass) → v0.9 (HC r1 §2.11(c.2) error-taxonomy direction nail-down) → v0.10 (15-finding corpus-final pass: CP/ON/PL/RC/WM r1 syntheses + forward-zero F-pilot-PL-4 cycle-break carve-out + new §2.13 backfill workflow discipline + §3.2 §4.a-envelope grandfather freeze) → v0.11 (parked-state lifecycle prose fully withdrawn per user 2026-05-05 directive) → **v0.12** (F-pilot-PL-4 tag-locus relaxed from edge-level to bead-level, matching the `cite:wide-fanout` precedent — surfaced by the 2026-05-06 forward-zero re-verification). Total: 12 versions, 16 numbered class-lane findings absorbed plus per-pilot findings.

## Readiness gap closure

The five gap clusters from `phase-1-readiness-gap-analysis.md` v0.2 are addressed as follows:

- **§A1 SH beads not loaded — CLOSED.** SH spec authored (`scenario-harness.md` v0.2.0 → v0.2.1), pilot reviewed (3-reviewer pass on `sh-pilot.md`, synthesis at `docs/reviews/2026-05-05-sh-pilot-r1/synthesis.md`), beads loaded into `.beads/` under epic `hk-i0tw` (54 beads at v0.1.1; 4 cycle-rejected intra-spec edges captured in `sh-load-findings.md` and fixed at v0.1.2). 50 of the 54 SH beads carry `scope:bootstrap`.
- **§A2 Corpus labelling — DEFERRED via tracked bead.** `hk-kle6.2` files the per-cluster reconciliation pass; in `scope:bootstrap` so it runs early in Phase 1, ahead of any code that depends on closed-set queries.
- **§A3 Readiness workflow — VOIDED** per the 2026-05-05 user directive (memory `feedback_agent_flow_priority`). Loaded beads transition directly to a dispatchable status; operator safety lives at the queue level (`harmonik stop`/`pause`/`upgrade`), not at the bead-lifecycle layer. The candidate beads `p1-readiness-*` were not filed.
- **§A4 / §B4 Operational skills — TRACKED.** New epic `hk-jhob` (4 beads): `agent-reviewer` skill + JSON-verdict schema (`hk-jhob.1`, `scope:bootstrap`), `beads-cli` skill (`hk-jhob.2`, `scope:bootstrap`), `agent-config-reviewer` (`hk-jhob.3`, post-bootstrap), `go-subsystem-add` (`hk-jhob.4`, post-bootstrap).
- **§A5 Policy-engine bypass — TRACKED.** Standalone bead `hk-b3f.89` (`scope:bootstrap`, under EM epic): MVH composition root wires no-op PolicyEngine as the production interface, no `policyEngine == nil` branch (per SH-018 ban on test-mode branches in production).
- **§B1 Twin binary — TRACKED.** Mini-epic `hk-ahvq.48` (1 mini-epic envelope + 9 children, all `scope:bootstrap`): main-loop scaffold, NDJSON-over-Unix-socket parity loop (HC-007/HC-007a/HC-008), script-driver, build-time ldflags commit-hash stamp (HC-043), Makefile target/artifact placement, three first conformance scenarios (smoke/twin-launch-and-ready, smoke/checkpoint-and-merge, regression/twin-failure-classification per SH §10.1), and one spec-edit task to define the twin script-file format normatively in HC §4.8 or SH §4.3.
- **§B2 Build/test scaffolding — TRACKED.** New epic `hk-pvcs` (1 epic + 8 children, all `scope:bootstrap`): go.mod + go.sum + .gitignore extension, Makefile (three-tier check targets), `.golangci.yml` (depguard component matrix + LLM-SDK ban per PL-INV-002), `lefthook.yml` (local pre-commit + pre-push hooks), coverage-gate script (95%-core / 90%-floor / <0.3% regression per `quality-checks.md`), test-helper scaffold, forbid-import tool, BUILDING.md onboarding doc. CI surface (`.github/workflows/ci.yml`) is voided per Z.1 (local pass IS the gate; no GitHub Actions).
- **§E Validation criterion — TRACKED.** `hk-kle6.1` files the trivial-slice paper walkthrough doc (paper map of the ~25-op trivial-slice flow to bootstrap-bead owners; surfaces any "no owner" gaps).

## Closeout ceremony — bead-by-bead disposition

Wave 1 (`a4d9288`, `d874f96`, `293d7aa`) and Wave 2 (this commit) drained the closeout list. Each bead and its terminal disposition:

- **`hk-ahvq.38` Full-union cycle check — CLOSED 2026-05-01.** `br dep cycles` clean across the loaded 10-spec union. Re-verified after every Wave 2 yaml mutation; no regression.
- **`hk-ahvq.39` Forward-zero verification — CLOSED 2026-05-06.** Final grep at the end of the Wave-2 yaml-cleanup pass: 26 surviving `forward:*` entries, all PL→ON, all legitimately tagged `cite:cycle-break:on` per the F-pilot-PL-4 v0.10 carve-out (with v0.12 bead-level tag-locus relaxation). Zero stale entries; zero unaccounted-for entries. The 3 EV-event-completeness violations cleared by `.45`; the 10 CP §3.2 violations cleared by `.46`.
- **`hk-ahvq.40` Mnem-map consolidation — CLOSED earlier session.** All 10 `<spec>-pilot.md` mnem-maps consolidated to canonical paths under `docs/decompose-to-tasks/`. SH mnem-map joined the canonical set.
- **`hk-ahvq.41` Bootstrap subset — CLOSED with v0.2 synthesis.** `bootstrap-subset.md` v0.2 enumerates 345 beads in the decompose-to-tasks subset (291 v0.1 + 54 SH). Total `scope:bootstrap` carry-through after meta-epic minting: 376.
- **`hk-ahvq.42` Phase 0 milestone — CLOSED on landing of this doc** (Wave 2 commit). Sentinel.
- **`hk-ahvq.45` EV r2 spec patch — CLOSED 2026-05-06.** Three CP-emitted events added to `event-model.md` §8.2 (rows `.10`/`.11`/`.12`); EV bumped 0.3.3 → 0.3.4. Companion bead minting (`hk-hqwn.59.79/.80/.81`) labelled `scope:bootstrap` and edged from CP citing beads.
- **`hk-ahvq.46` CP r2 pilot patch — CLOSED 2026-05-06.** 10 §3.2-violating `forward:*` yaml entries deleted from `cp-pilot-data.yaml` (1 wm + 4 on + 4 rc + 1 bi); cites retained as F-pilot-EV-3 informational findings in `cp-pilot.md` §3.2; pilot bumped 0.1.1 → 0.1.2. CP carries no §9.3 cycle-break NOTE so the F-pilot-PL-4 carve-out does not apply (correctly DELETEd, not legitimised). 3 EV-row entries (cp-034b/041/043) retained — resolved by `.45`.
- **`hk-ahvq.47` SH §4.a envelope addition — CLOSED 2026-05-06.** Per discipline v0.10 §3.2 §4.a-envelope grandfather freeze and F-pilot-SH-4: SH was drafted post-AR-053 (2026-04-24) and is therefore not in the grandfathered 7-spec set; SH spec patched to v0.2.1 with the §4.a subsystem envelope per AR-053.
- **`hk-pvcs` build-scaffold mini-epic — FILED.** 9 beads (epic + 8 children) in `scope:bootstrap`. Implementation gates each starting Phase-1 commit.
- **`hk-ahvq.48` twin-binary mini-epic — FILED.** 10 beads (mini-epic + 9 children) in `scope:bootstrap`. Required before second-cycle agentic slice.
- **`hk-jhob` operational-skills epic — FILED.** 5 beads (epic + 4 children); 3 of 5 in `scope:bootstrap`.
- **`hk-kle6` Phase-1-validation epic — FILED.** 3 beads (epic + 2 children); all in `scope:bootstrap`.
- **`hk-b3f.89` MVH composition-root no-op PolicyEngine — FILED.** Standalone EM bead, `scope:bootstrap`. Resolves §A5.

Small-cleanup ceremony items also drained:

- **`sh-pilot.md` v0.1.2.** 4 bidirectional-cite slips at `sh-pilot-data.yaml` lines 639/643/645/657 (rejected by the loader as 2-cycles) fixed; pilot bumped 0.1.1 → 0.1.2; `br dep cycles` re-verified clean.
- **`discipline.md` v0.10 → v0.11 → v0.12.** v0.11: parked-state lifecycle prose fully withdrawn at §2.9 and the `post-mvh` tag prose in §3.1 (per Z.2 user directive). v0.12: F-pilot-PL-4 tag-locus relaxed from edge-level (structurally unsatisfiable in the yaml schema) to bead-level (`tags:` list, matching the `cite:wide-fanout` precedent the rule itself names as analogous).
- **`operator-nfr.md` v0.4.1.** The single-line `in_flight(run)` reference to a "parked lifecycle position" in §3 glossary was confirmed already-landed in v0.4.1 ahead of this session; no v0.4.2 patch authored. The HANDOFF entry was stale.

## What Phase 0 deliberately did not do

- **No code.** Phase 0 was plan refinement → spec drafting → review → decomposition → labelling → readiness-gap fill. No `.go` files were authored; the `cmd/`, `internal/`, `pkg/` directories that the bootstrap subset assumes do not yet exist on disk. They are tracked as Phase-1 tasks under the spec implementation epics.
- **No kerf this session.** Per `STATUS.md` and `CLAUDE.md`, kerf was paused for Phase 0 ("disregard kerf for now; come back when something's working"). Whether kerf reactivates in Phase 1 is a per-need decision; the bootstrap subset is implementation-ready against the existing specs, so kerf MAY remain paused through Phase 1 unless an implementation cycle surfaces a real spec gap or post-MVH specs (CP gates, S09) start being authored.
- **No implementation gates remaining.** The "loaded beads must not auto-start (parked + readiness workflow)" rule was withdrawn (Z.2). Operator queue-level controls (`harmonik stop`/`pause`/`upgrade`) remain as the single safety surface; they operate between tasks, not on individual bead lifecycle.
- **No CI pipeline.** Per Z.1 user clarification, no `.github/workflows/ci.yml` is in scope; the `make check-full` local gauntlet is the gate. Agent-reviewer-every-commit operates against the local gate.

## What unblocks now

Agents claim work via `br ready -l scope:bootstrap`. The natural starting order:

1. **`hk-pvcs` (build/test scaffolding, 8 beads).** Establishes the local `make check-fast` / `check` / `check-full` gauntlet, golangci-lint v2 with depguard component matrix, lefthook hooks, coverage-gate script, BUILDING.md onboarding. Without this, no subsequent commit is verifiable. Author and merge first.
2. **`hk-jhob.1` agent-reviewer skill + JSON-verdict schema v1.** Required before commit cadence locks in. First few hand-commits can be human-reviewed; long-term, the reviewer-skill-must-not-rot commitment in `quality-checks.md` requires this skill exists.
3. **`hk-jhob.2` beads-cli skill.** Thin file; satisfies HC §4.11 + BI §6 foundation skill requirement.
4. **`hk-kle6.1` trivial-slice paper walkthrough.** Validation artifact: maps the ~25-op trivial-slice runtime flow to bootstrap-bead owners; any "no owner" finding is either a corpus gap (file new bead) or a labelling oversight (apply `scope:bootstrap` to the missing bead).
5. **`hk-kle6.2` corpus label reconciliation.** Brings the ~520 currently-untagged beads into `scope:bootstrap` ∪ `post-mvh`, closing §A2.
6. **PL cluster A (37 beads) + WM cluster B-WM (45 beads) + EM cluster B-EM+F (65 beads).** The minimum trivial-slice happy path: daemon starts → bead in queue → workspace lease → workflow execution → checkpoint commit → bead closed.

After the trivial-slice runs end-to-end, the second cycle adds the twin handler subprocess (`hk-ahvq.48` 10 beads + HC cluster C 46 beads). That cycle exercises the §1 working-definition acceptance test (`bootstrap-subset.md` §1 item 3) and is where SH-driven scenario assertions begin gating commits.

## References

- [`docs/foundation/phase-1-readiness-gap-analysis.md`](phase-1-readiness-gap-analysis.md) v0.2 — the gap analysis this milestone-close discharges.
- [`docs/decompose-to-tasks/bootstrap-subset.md`](../decompose-to-tasks/bootstrap-subset.md) v0.2 — consolidated bootstrap-subset doc; authoritative for the 345 spec-corpus beads.
- [`docs/decompose-to-tasks/discipline.md`](../decompose-to-tasks/discipline.md) v0.12 — decomposition rule set with full revision history.
- [`docs/decompose-to-tasks/pilot-review-protocol.md`](../decompose-to-tasks/pilot-review-protocol.md) — 3-reviewer protocol gating each pilot.
- [`docs/foundation/project-level/`](project-level/) — locked decisions (build practices, quality checks, subsystem organization, agent configuration).
- [`STATUS.md`](../../STATUS.md) — flipped to "Phase 1 active" alongside this commit.
- [`TASKS.md`](../../TASKS.md) — Phase 0 list reformatted as historical; Phase 1 implementation gate lifted.

## Revision history

- **v1.0 (2026-05-06).** Authored as the Phase-0 milestone close per `hk-ahvq.42`. Records: 11 reviewed specs (~562 req IDs), 905 live beads (3,589 edges, zero cycles), 376 `scope:bootstrap` beads (348 corpus + 28 meta), discipline at v0.12 (12 versions, 16 class-lane findings), readiness gaps closed in beads (`hk-pvcs` build-scaffold, `hk-ahvq.48` twin-binary, `hk-jhob` operational-skills, `hk-kle6` Phase-1-validation, `hk-b3f.89` no-op PolicyEngine), parked-state withdrawn, six closeout-ceremony beads closed (`.38`/`.39`/`.40`/`.41`/`.42`/`.45`/`.46`/`.47`).
