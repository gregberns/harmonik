# Remaining Readiness Beads — Tracking-Bead Filing (2026-05-06)

**Author:** remaining-readiness bead-filing agent
**Source gap:** [`docs/foundation/phase-1-readiness-gap-analysis.md`](../foundation/phase-1-readiness-gap-analysis.md) v0.2 (2026-05-05)
**Companion fills (parallel agents):**
- [`build-scaffold-gap-2026-05-05.md`](build-scaffold-gap-2026-05-05.md) — 9 beads under `hk-pvcs` (Makefile, golangci, lefthook, coverage tooling, etc.).
- [`twin-binary-gap-2026-05-05.md`](twin-binary-gap-2026-05-05.md) — 9 beads under `hk-ahvq.48` (twin-binary scaffolding + first 3 conformance scenarios).

## TL;DR

- New meta-epics created: **2** (`hk-jhob` operational-skills, `hk-kle6` Phase-1-validation).
- New beads filed: **9** total (2 epics + 6 child tasks + 1 standalone EM bead).
- Beads NOT filed: **4** explicitly voided per §Z.2 (parked-state withdrawn) and §Z.1 (CI excluded) user clarifications.
- Dependency edges added: **10** (8 `blocks`, 2 `related`).
- `br dep cycles` final state: **clean.**
- Final `scope:bootstrap` corpus count: **373** (up from 364 pre-fill).

## 1. Inputs reconciled

The readiness-gap analysis §"Recommended new tracking beads" (v0.1) named ~17 candidates. After cross-referencing the parallel fills and §Z user clarifications:

| Candidate | Status | Reason |
|---|---|---|
| `p1-readiness-workflow-definition` | **VOIDED** | §Z.2 — parked-state rule withdrawn. |
| `p1-readiness-parked-state` | **VOIDED** | §Z.2 — parked-state rule withdrawn. |
| `p1-readiness-gate-bead` | **VOIDED** | §Z.2 — parked-state rule withdrawn. |
| `p1-build-ci-workflow` | **VOIDED** | §Z.1 — CI excluded; local-only review pipeline. |
| `p1-build-makefile` | Filed elsewhere | `hk-pvcs.2` (build-scaffold pass). |
| `p1-build-golangci-yml` | Filed elsewhere | `hk-pvcs.3` (build-scaffold pass). |
| `p1-build-lefthook-yml` | Filed elsewhere | `hk-pvcs.4` (build-scaffold pass). |
| `p1-build-coverage-gate` | Filed elsewhere | `hk-pvcs.5` (build-scaffold pass). |
| `p1-build-forbid-import` | Filed elsewhere | `hk-pvcs.7` (build-scaffold pass). |
| `p1-twin-claude-binary-scaffold` | Filed elsewhere | `hk-ahvq.48.1..5` (twin-binary pass; expanded to 5 concrete code beads). |
| `p1-twin-conformance-scenarios` | Filed elsewhere | `hk-ahvq.48.6/.7/.8` (twin-binary pass; first 3 SH §10.1 scenarios). |
| `p1-skill-agent-reviewer` | **FILED HERE** | `hk-jhob.1`. |
| `p1-skill-beads-cli` | **FILED HERE** | `hk-jhob.2`. |
| `p1-skill-agent-config-reviewer` | **FILED HERE** | `hk-jhob.3`. |
| `p1-skill-go-subsystem-add` | **FILED HERE** | `hk-jhob.4`. |
| `p1-policy-engine-noop-mode` | **FILED HERE** | `hk-b3f.89`. |
| `p1-trivial-slice-walkthrough` | **FILED HERE** | `hk-kle6.1`. |
| `p1-corpus-label-reconciliation` | **FILED HERE** | `hk-kle6.2`. |

## 2. New meta-epics

### `hk-jhob` — Operational-skills meta-epic

**Title:** Operational-skills meta-epic — agent-reviewer, beads-cli, agent-config-reviewer, go-subsystem-add skills

**Labels:** `kind:meta-parent`, `phase:0`, `scope:bootstrap`, `tag:meta`

**Status:** open (dispatchable per §Z.2)

**Addresses gap:** §A4 (MAJOR — meta-beads for review pipeline + skill registry), §B4 (MAJOR — agent-reviewer pipeline), §C2 (MAJOR — commit cadence + reviewer prompt).

**Children:** `hk-jhob.1` … `hk-jhob.4`.

### `hk-kle6` — Phase-1-validation meta-epic

**Title:** Phase-1-validation meta-epic — trivial-slice walkthrough + corpus label reconciliation

**Labels:** `kind:meta-parent`, `phase:0`, `scope:bootstrap`, `tag:meta`

**Status:** open

**Addresses gap:** §A2 (MAJOR — corpus labelling), §D (first self-build cycle analysis), §E (validation criterion recommendation), §"Recommended Phase 1 entry gate" items 2 + 4.

**Children:** `hk-kle6.1` and `hk-kle6.2`.

## 3. Beads filed

| Bead | Title | Parent | Labels | Addresses |
|---|---|---|---|---|
| `hk-jhob` | Operational-skills meta-epic | (top-level) | `kind:meta-parent`, `phase:0`, `scope:bootstrap`, `tag:meta` | §A4, §B4 |
| `hk-jhob.1` | Author `.claude/skills/agent-reviewer/` skill + JSON-verdict schema v1 | `hk-jhob` | `kind:scaffold`, `phase:0`, `scope:bootstrap`, `tag:meta` | §A4, §B4, §C2 |
| `hk-jhob.2` | Author `.claude/skills/beads-cli/` skill (agent-facing `br` wrapper per BI-027/BI-028) | `hk-jhob` | `kind:scaffold`, `phase:0`, `scope:bootstrap`, `tag:meta` | §A4 |
| `hk-jhob.3` | Author `.claude/skills/agent-config-reviewer/` skill (Tier 2 session-boundary reviewer) | `hk-jhob` | `kind:scaffold`, `phase:0`, `scope:bootstrap`, `tag:meta` | §A4 (sub-bullet) |
| `hk-jhob.4` | Author `.claude/skills/go-subsystem-add/` skill (package-scaffold skill, fires at first-add) | `hk-jhob` | `kind:scaffold`, `phase:0`, `scope:bootstrap`, `tag:meta` | §A4 (sub-bullet) |
| `hk-kle6` | Phase-1-validation meta-epic | (top-level) | `kind:meta-parent`, `phase:0`, `scope:bootstrap`, `tag:meta` | §A2, §D, §E |
| `hk-kle6.1` | Author trivial-slice paper walkthrough doc (`docs/foundation/trivial-slice-walkthrough.md`) | `hk-kle6` | `kind:doc`, `phase:0`, `scope:bootstrap`, `tag:meta` | §D1, §D2, §E |
| `hk-kle6.2` | Apply post-mvh / scope:bootstrap labels to the 527 untagged beads (corpus label reconciliation) | `hk-kle6` | `kind:workflow`, `phase:0`, `scope:bootstrap`, `tag:meta` | §A2 |
| `hk-b3f.89` | MVH composition-root wires no-op PolicyEngine as production interface | `hk-b3f` (EM impl epic) | `kind:mechanism`, `scope:bootstrap`, `spec:execution-model`, `tag:mechanism` | §A5 |

**Total: 2 epics + 6 child tasks + 1 standalone EM bead = 9 new beads.**

All 9 carry `scope:bootstrap` per mission constraint. None has `parked` status (per §Z.2). All are `open` and dispatchable.

A note on the 2 candidates marked "not bootstrap" in the readiness-gap doc itself (`p1-skill-agent-config-reviewer`, `p1-skill-go-subsystem-add`): the mission's blanket "scope:bootstrap mandatory on every new bead" rule is honored — both `hk-jhob.3` and `hk-jhob.4` carry `scope:bootstrap`. If the doc's "not bootstrap" framing should prevail (these skills genuinely fire at session-boundary / first-add events post-MVH), a follow-up label-reconciliation pass (the `hk-kle6.2` work) can demote them to `post-mvh` and re-promote when they become load-bearing.

## 4. Dependency edges added

10 edges total (8 `blocks`, 2 `related`):

```
hk-pvcs.4 -> hk-jhob.1            (related)  lefthook hook references make agent-review which calls the skill
hk-pvcs.2 -> hk-jhob.1            (related)  Makefile make agent-review target stub-calls the skill until it lands
hk-kle6.1 -> hk-ahvq.41           (blocks)   walkthrough validates the bootstrap subset that .41 identifies
hk-kle6.2 -> hk-ahvq.41           (blocks)   label-reconciliation aligns the 527 against cluster-report enumerations
hk-kle6.1 -> hk-ahvq.48.6         (related)  walkthrough may reference first conformance scenario (parallel artifacts)
hk-ahvq.42 -> hk-kle6.1           (blocks)   Phase 0 close requires walkthrough done (entry-gate item 4)
hk-ahvq.42 -> hk-kle6.2           (blocks)   Phase 0 close requires labelling reconciled (entry-gate item 2)
hk-ahvq.42 -> hk-pvcs             (blocks)   Phase 0 close requires build-scaffolding landed (entry-gate item 5)
hk-ahvq.42 -> hk-jhob.1           (blocks)   Phase 0 close requires agent-reviewer skill (MAJOR per §A4)
hk-ahvq.42 -> hk-jhob.2           (blocks)   Phase 0 close requires beads-cli skill (MAJOR per §A4)
```

Edges considered and **not** added:
- `hk-jhob.1 -> hk-pvcs.1` (go.mod) — the skill is markdown + JSON-schema, not Go; no module dependency. Skipped.
- `hk-b3f.89 -> *` — design clarification, not a code bead; no upstream blocks. Lives alongside cluster-A EM work and informs the implementer when they reach the dispatcher boundary.
- `hk-jhob.3` / `.4 -> hk-jhob.1` — distinct skills, parallel authoring is fine. Skipped.
- `hk-kle6.1 -> hk-pvcs.*` — the walkthrough is paper, not executable; no build dependency. Skipped.

`br dep cycles` final state: **clean** (`✓ No dependency cycles detected`).

## 5. Compliance with mission constraints

- **All 9 new beads carry `scope:bootstrap`.** ✓ (Including the 2 candidates the gap-doc marked "not bootstrap" — see note in §3.)
- **No `parked` statuses anywhere in corpus.** ✓ Verified: 0 parked beads.
- **No specs modified.** ✓ All beads file tasks; spec-edit follow-ups (e.g., the `hk-b3f.89` no-op PolicyEngine clarification) reference EM spec text but do not edit it.
- **No pilot files modified.** ✓
- **No code written.** ✓ Task-filing only.
- **CI excluded.** ✓ Per §Z.1; `hk-jhob.1` description explicitly notes "no GitHub Actions surface."
- **Parked candidates voided.** ✓ Per §Z.2; the 3 readiness-workflow candidates are not filed.
- **No re-filing of already-filed beads.** ✓ Verified via `br list` title-search and label-search before each create.

## 6. Final corpus state

| Metric | Pre-fill | Post-fill | Delta |
|---|---|---|---|
| Total beads | 897 | 906 | +9 |
| `scope:bootstrap` | 364 | 373 | +9 |
| `post-mvh` | 5 | 5 | 0 |
| Untagged (neither tag) | 528 | 528 | 0 |
| Parked status | 0 | 0 | 0 |
| Dependency cycles | 0 | 0 | 0 |

The "untagged 528" is the `hk-kle6.2` work surface; this fill did not address it (that's the next agent's job).

## 7. Surprises

1. **Mission rule conflicts with gap-doc designation on 2 beads.** The readiness-gap doc explicitly flags `p1-skill-agent-config-reviewer` and `p1-skill-go-subsystem-add` as "not bootstrap" — but the mission says "scope:bootstrap mandatory on every new bead." Honored mission; flagged in §3 for follow-up if the gap-doc framing should win.
2. **Pre-fill total was 897, not the gap-doc's stated 823.** The gap-doc was written 2026-05-05 against an 823-count corpus; the build-scaffold pass added 9 (`hk-pvcs.*`), the twin-binary pass added 10 (`hk-ahvq.48`+9 children), and apparently 55 other beads landed during the parallel work day-of. The "untagged 528" matches the 527 + 1 expectation (off-by-one is an epic-envelope or similar) — labelling gap surface unchanged.
3. **`hk-pvcs.2` and `hk-pvcs.4` carry `related` edges to `hk-jhob.1` rather than `blocks`.** Per the build-scaffold pass's own commentary ("a stub if the skill doesn't exist yet fallback so the scaffold can land before the skill — no cross-epic dependency, no blocking"), these are soft dependencies. `related` preserves the linkage without introducing a build-vs-skill ordering constraint that contradicts the build-scaffold author's intent.
4. **Phase 0 milestone close (`hk-ahvq.42`) gains 5 new blockers from this pass.** This is the right answer (gate items 2/4/5 + the §A4 MAJORs) but does mean Phase 0 close is now meaningfully gated; flagged for the next agent as a coordination point.

## 8. Recommended next-step actions

For the agent that picks this up:

1. **Run `hk-kle6.2` (label reconciliation) early.** It informs everything else — once the 528 untagged beads are sorted into bootstrap vs post-mvh, the Phase-1-entry-gate dependency graph stabilizes. Cluster-report references at `docs/decompose-to-tasks/bootstrap-subset/{pl,wm,em,hc,ev,bi,ar,rc,sh}-bootstrap.md` §5 enumerate the deferred sets — option (a) from §A2 ("the 527 are all implicit `post-mvh`, label them as such") is the cheap path.
2. **Author the trivial-slice walkthrough (`hk-kle6.1`) BEFORE first cluster-A code lands.** It's the validation surface; running it after code starts means it can't surface gaps in the bootstrap subset (the implementer will paper over them).
3. **`hk-jhob.1` (agent-reviewer skill) is the natural near-term unblocker for `hk-pvcs.4` (lefthook).** Since `hk-pvcs.4`'s description already documents the stub-fallback, lefthook can land WITHOUT the skill — but full functionality requires it. Schedule .1 in parallel with .4.
4. **`hk-b3f.89` (no-op PolicyEngine) is a 2-line spec clarification + 1 composition-root wire.** Can land any time; consider folding into the first cluster-A EM task to avoid a discrete commit.
5. **SH pilot review + load is still a Phase 0 BLOCKER (§A1).** Not addressed by this pass nor by the parallel fills. Flagged: the `hk-ahvq.41` bootstrap-subset bead's gate items (cycle check, forward-zero, mnem-consolidation) need re-running after SH loads.
6. **Consider splitting `hk-kle6` to top-level and renaming `hk-jhob` → something more mnemonic.** Both epics live as top-level codenames; the corpus convention prefers mnemonic codenames over short hashes (cf. `hk-ahvq` Phase 0, `hk-b3f` EM, `hk-pvcs` build-scaffold). Beads-CLI auto-assigned `hk-jhob` and `hk-kle6` per its standard policy; the hash-codenames are functional but less greppable.

## 9. Verification trail

```
br list -l scope:bootstrap --limit 0 | wc -l            # baseline 364
br --json list --limit 0 | jq '.issues | length'        # baseline 897
br dep cycles                                            # baseline clean

br create ... (9 new beads, 8 with --parent)
br update hk-jhob.3 --add-label scope:bootstrap          # mission compliance
br update hk-jhob.4 --add-label scope:bootstrap          # mission compliance
br dep add ... (10 edges)
br dep cycles                                            # post clean
br --json list --limit 0 | jq '.issues | length'        # post 906
br list -l scope:bootstrap --limit 0 | wc -l            # post 373
```

Sources cited:
- `docs/foundation/phase-1-readiness-gap-analysis.md` v0.2 — full document.
- `docs/decompose-to-tasks/build-scaffold-gap-2026-05-05.md` — companion fill.
- `docs/decompose-to-tasks/twin-binary-gap-2026-05-05.md` — companion fill.
- `docs/decompose-to-tasks/discipline.md` — label conventions §2.5/§2.8/§2.9/§2.11.
- `docs/decompose-to-tasks/bootstrap-subset.md` — §1 working-definition, §2 corpus snapshot.
- `specs/scenario-harness.md` v0.2.0 — §4.7 SH-018 no-test-mode-branches.
- `specs/beads-integration.md` — §4.4 BI-027/BI-028 Beads-CLI access path.
- `specs/handler-contract.md` v0.3.3 — §4.11 skill provisioning.
- `specs/control-points.md` — CP-031 default skills.
- `docs/foundation/project-level/build-practices.md` — agent-review-every-commit.
- `docs/foundation/project-level/quality-checks.md` — §Agent-enforceability.
- `docs/foundation/project-level/agent-configuration.md` — §Skills.
- `docs/foundation/project-level/subsystem-organization.md` — §Go module layout.
