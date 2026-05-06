# SH Pilot Coverage Review (r1)

`review-date: 2026-05-05` · reviewer: coverage · target: `docs/decompose-to-tasks/sh-pilot.md` v0.1.0 against `specs/scenario-harness.md` v0.2.0 · method: `docs/decompose-to-tasks/pilot-review-protocol.md` v0.2 §3.1.

## Summary

The SH pilot is **structurally complete** on requirement, invariant, and schema coverage: every §4 normative requirement (SH-001..SH-034 plus SH-015a, SH-016a — 36 active), every §5 invariant (SH-INV-001..SH-INV-005 — 5 active), every §6.1 RECORD (11), and the §8 error taxonomy (single umbrella bead for the 8 sub-sections per discipline §2.11(c) below-threshold rule) are accounted for. There are **zero missed §4 or §5 IDs**, **zero phantom IDs**, and the spec-version pin (`v0.2.0`) and discipline-version pin (`v0.9`) match current. The findings below are arithmetic / cosmetic in nature, plus one carry-forward of a pilot-author-flagged class finding (F-pilot-SH-4 §4.a envelope absence) that the coverage reviewer is obligated to surface independently.

**Counts (this review):**
- BLOCKER: 0
- MAJOR: 2 (one tally arithmetic, one §4.a envelope class flag)
- MINOR: 3 (cosmetic / cross-spec edge tally rounding / total-edge claim drift)

**Lane breakdown (all findings):** `local: 4` · `class: 1`

---

## 1. Enumeration of source-spec IDs

Per §3.1 method steps 1-5, exhaustive enumeration of `specs/scenario-harness.md` v0.2.0:

### §4 normative requirements (36 active, 0 retired)

| Section | IDs |
|---|---|
| §4.1 Scenario file format | SH-001, SH-002, SH-003, SH-004, SH-005 |
| §4.2 Suite loading + ordering | SH-006, SH-007 |
| §4.3 Twin substitution | SH-008, SH-009, SH-010, SH-011 |
| §4.4 Workspace fixture lifecycle | SH-012, SH-013, SH-014, SH-015, **SH-015a**, SH-016, **SH-016a** |
| §4.5 Orchestration drive | SH-017, SH-018, SH-019 |
| §4.6 Event-log capture + assertions | SH-020, SH-021, SH-022, SH-023, SH-024 |
| §4.7 Scenario timeout | SH-025, SH-026 |
| §4.8 Repeatability + offline | SH-027, SH-028 |
| §4.9 Cadence support | SH-029 |
| §4.10 Scenario composition (matrix) | SH-030 |
| §4.11 Concurrency policy | SH-031 |
| §4.12 Harness CLI surface | **SH-032, SH-033** |
| §4.13 Result emission | **SH-034** |

Spec text contains no `[retired]` markers. Bold = `(new in v0.2)` per spec headings (5 v0.2-additions: SH-015a, SH-016a, SH-032, SH-033, SH-034).

### §5 invariants (5 active, 0 retired)

SH-INV-001, SH-INV-002, SH-INV-003, SH-INV-004, SH-INV-005.

### §6 schema RECORDs (11)

`ScenarioFile`, `AgentOverride`, `FixtureSetup`, `GitSeedOp`, `FileSeed`, `EventExpectation`, `WorkspacePredicate`, `OutcomeExpectation`, `ScenarioResult`, `AssertionResult`, `SuiteResult`. (`GitSeedOp` and `FileSeed` are nested inside `FixtureSetup` but declared as independent top-level `RECORD` blocks at lines 474 and 480.)

### §6.2 co-owned event surface

Spec body (line 548) declares: "This spec emits no cross-bus events of its own." → no `sh-events.*` row beads required. Cross-spec edges to EV row beads where SH §4 reqs term-use specific events.

### §6.3 per-kind interpretation tables

Two prose tables: `WorkspacePredicate.expected` per-`kind` interpretation and `GitSeedOp.args` per-`op` required keys. Per discipline §2.6, these are content of their owning RECORDs, not separate beads.

### §8 error / failure classes (8 sub-sections, 1 precedence table)

§8.0 precedence (new in v0.2), then §8.1 `scenario-load-failure`, §8.2 `twin-binary-not-found`, §8.3 `fixture-setup-failed`, §8.4 `orchestration-internal-error`, §8.5 `assertion-failed`, §8.6 `scenario-timeout`, §8.7 `harness-internal-error`, §8.8 `cleanup-failed`. 8 classes < 11-row §2.11(c) threshold → single-umbrella `sh-error.taxonomy` bead per discipline §2.6.

### §11 open questions (13)

OQ-SH-001 through OQ-SH-013. Per discipline §2.8 OQs are NOT bead-loaded; verified.

---

## 2. Coverage cross-check (spec ID → pilot bead)

### 2.1 §4 reqs → pilot §2 rows

All 36 IDs present as first-class beads in the pilot §2 table AND in `sh-pilot-data.yaml` `req:` fields. Verified with `grep -E '^    req: SH-' sh-pilot-data.yaml | sort -u` returning exactly 36 distinct §4 IDs plus 5 §5 IDs.

**Missed IDs:** none.
**Phantom IDs in pilot:** none. (No `req:SH-NNN` in the pilot §2 table that is absent from the spec.)

### 2.2 §5 invariants → pilot §3 rows

All 5 SH-INV-NNN IDs present as `kind: invariant` beads in the pilot §3 table.

**Missed IDs:** none.
**Phantom IDs in pilot:** none.

### 2.3 §6 RECORDs → pilot §4 schema rows

All 11 RECORDs are minted as `sh-schema.<record-name>` beads in pilot §4.

**Missed RECORDs:** none.
**Phantom RECORDs in pilot:** none.

### 2.4 §8 error classes → pilot §4 taxonomy bead

The 8 §8.1..§8.8 classes plus §8.0 precedence are folded into the single `sh-error.taxonomy` bead's description, per discipline §2.11(c) below-threshold single-umbrella rule. Description text in pilot §4 verbatim enumerates all 8 classes and the precedence ordering.

**Missed classes:** none.
**Phantom classes:** none.

### 2.5 §1 "Counts" section vs actual

Pilot §1 declares: 36 §4 reqs / 5 invariants / 11 RECORDs / 8 §8 classes / 13 OQs / 7 `depends-on` entries. **Every count matches the spec.**

### 2.6 Spec-version reference

Pilot §1 line 9: `'specs/scenario-harness.md (SH), v0.2.0, status reviewed'`. YAML line 121: `spec_version: "0.2.0"`. Spec front-matter: `version: 0.2.0`, `status: reviewed`. **Match.**

Discipline pin: pilot line 3 declares `discipline v0.9`; discipline file front-matter confirms `discipline-version: 0.9`. **Match — no stale-version flag.**

### 2.7 Bead-count tally (pilot §8)

| Class | Pilot claim | Verified actual (yaml) |
|---|---|---|
| Spec parent bead (`sh`) | 1 | 1 (epic block at yaml line 129) |
| Requirement beads | 36 | 36 |
| Step beads | 0 | 0 |
| Sensor / invariant beads | 5 | 5 |
| Schema beads | 11 | 11 |
| Error-taxonomy bead | 1 | 1 |
| Test-infra beads | 0 | 0 |
| **Total** | **54** | **54** |

Bead-count arithmetic checks out: 1 + 36 + 0 + 5 + 11 + 1 + 0 = **54** ✓.

### 2.8 Edge tally (pilot §5 and §8)

Verified by direct count of `^  - {from:` lines in `sh-pilot-data.yaml`:

| Bucket | Pilot §5 / §8 claim | Actual yaml |
|---|---|---|
| Intra-SH | 93 | **91** |
| Cross-spec AR | 2 | 2 ✓ |
| Cross-spec HC | 13 | 13 ✓ |
| Cross-spec EM | 4 | 4 ✓ |
| Cross-spec EV | 6 | 6 ✓ |
| Cross-spec WM | 4 | 4 ✓ |
| Cross-spec PL | 9 | 9 ✓ |
| Cross-spec ON | 2 | 2 ✓ |
| **Cross-spec sum** | **38** (claimed in §5 closing) | **40** (sum of per-spec rows) |
| **Total edges** | ~133 | **131** |

The per-spec breakdowns match the yaml exactly. The two arithmetic errors are:
- §5 summary line 179: `Total cross-spec edges: 38 (2 AR + 13 HC + 4 EM + 6 EV + 4 WM + 9 PL + 2 ON)` — sum is 40, not 38.
- §8 closing: `Total edges: ~133` and `Intra-SH: 93 edges` — actual yaml count is 131 total / 91 intra. The pilot's parenthetical at line 257 acknowledges "Actual count of YAML edge entries: 131 lines" which contradicts the §8 ~133 claim immediately above.

See Finding 1 below.

---

## 3. Findings

### 3.1 Finding 1 — Tally arithmetic error: cross-spec edge sum (MAJOR, `local`)

**What.** Pilot §5 line 179 states `Total cross-spec edges: 38 (2 AR + 13 HC + 4 EM + 6 EV + 4 WM + 9 PL + 2 ON)`. The arithmetic sum of those summands is **40**, not 38. The per-spec subtotals are individually correct (verified against yaml row-counts); only the summary integer is wrong.

**Severity.** MAJOR — this is precisely the class of bug pilot-review-protocol §1 was instituted to catch (the BI smoke-load tally arithmetic finding).

**Lane.** `local`. The discipline rule (§2.7 cross-spec edge convention, §3.2 depends-on validation) was applied correctly per row; only the closing summation is wrong. Generality probe: BI had the same kind of error and was the motivating example. The arithmetic mistake is per-pilot, not a discipline gap.

**Suggested patch.** Update pilot line 179 to read `Total cross-spec edges: 40 (2 AR + 13 HC + 4 EM + 6 EV + 4 WM + 9 PL + 2 ON)`. Knock-on §8 edge totals (Intra-SH 93 → 91; Total ~133 → 131) should be reconciled to the actual yaml line count of 131 to match the parenthetical disclaimer at line 257; see Finding 2.

### 3.2 Finding 2 — Tally arithmetic drift: §8 total-edge / intra-SH count (MINOR, `local`)

**What.** Pilot §8 declares `Intra-SH: 93 edges` and `Total edges: ~133`. Actual yaml has 91 intra-SH edges and 131 total. The pilot author acknowledges variance at line 257 ("Actual count of YAML edge entries: 131 lines; minor variance ... reviewers tally from `sh-pilot-data.yaml` directly") which is honest but leaves the body counts wrong.

**Severity.** MINOR — the disclaimer points reviewers at the authoritative source (the yaml). No structural integrity is at risk; the tally claims merely don't match the yaml exactly.

**Lane.** `local`. Same arithmetic drift as Finding 1; not a discipline rule issue.

**Suggested patch.** Either (a) update §8 to read `Intra-SH: 91 edges` and `Total edges: 131`, or (b) leave the prose but drop the leading-bullet integer summary in favor of the disclaimer; option (a) is preferable for a clean tally. The "~" prefix on `~133` was the pilot author's hedge; tightening to 131 and removing the `~` is consistent with how the per-spec edge counts (which lack `~`) are stated.

### 3.3 Finding 3 — Cross-spec ON edge count summary stale relative to v0.2 spec patches (MINOR, `local`)

**What.** Pilot §5 lists 2 ON edges (`sh-015 → on-029`, `sh-026 → on-029`) and the yaml has exactly those 2 edges. The spec's §9.1 cross-references include `[operator-nfr.md §4.5]` (cited in §6.3 schema-evolution prose for the future `schema_version` field) and `[operator-nfr.md §4.7 ON-029]`. The §4.5 cite is in informative §6.3 schema-evolution prose, NOT in normative-prose `§4`/`§5`/`§6.1`/`§8`, so per pilot-review-protocol §3.1 method step 1, no edge is required. (The §4.5 cite is consumed informatively only; treating it as supporting and not emitting an edge is correct discipline application.) Flagging here only as a transparency note for the reference-reviewer to confirm independently.

**Severity.** MINOR.

**Lane.** `local`. Discipline §3.1 supporting-cite test was applied; the cite is informative-only.

**Suggested patch.** None. (Surfaced for the reference-reviewer's cross-check; no coverage-side action.)

### 3.4 Finding 4 — §4.a Subsystem envelope absence flagged by author; coverage-reviewer corroboration (MAJOR, `class`)

**What.** Per AR-053 (the §4.a Subsystem-envelope rule), every spec authored after AR-053 landed (2026-04-24) should declare a §4.a envelope unless explicitly grandfathered. SH was authored 2026-05-05, post-AR-053, and is NOT in the discipline v0.7 §3.2 grandfather carve-out set `{EM, HC, CP, WM, PL, RC, EV}` (the v0.8 extension added EV; SH is not named). Inspection of `specs/scenario-harness.md` confirms `### 4.1 Scenario file format` is the first §4 sub-section — no `§4.a` envelope is declared.

The pilot author surfaced this themselves at F-pilot-SH-4 and explicitly tagged it as a class-lane finding ("the discipline's grandfather carve-out logic should be re-evaluated for specs drafted post-2026-04-24"). The coverage reviewer confirms independently.

**Severity.** MAJOR — this is a structural spec gap that the coverage reviewer is independently obligated to catch even if the pilot author did not flag it. The spec failing to declare an envelope means a `sh-001a` envelope-bead cannot be minted, and any future bead that should descend from the envelope (e.g., a subsystem-level test-infra extraction) loses its anchor.

**Lane.** `class`. The pilot author already self-classified this as `class` (per pilot-review-protocol §3.1 lane-tag rule, any reviewer's `class` tag routes to discipline-patch lane unless overruled in synthesis). The four §4.1 triage probes:
- Generality: yes — any spec drafted post-2026-04-24 outside the carve-out set hits the same gap. SH is the first; if more pre-MVH specs are authored before MVH, they will hit it too.
- Rule-vs-application: discipline rule (carve-out scope) is silent on late-drafted specs. The pilot followed the existing rule correctly and still produced a non-conforming output.
- Silence: yes — discipline §3.2 carve-out names a closed set as of 2026-04-24; specs drafted later are silent.
- Reviewer self-classification: pilot author self-tagged `class`; coverage reviewer concurs.

**Suggested resolution.** Discipline-patch lane. Either (a) extend the discipline §3.2 carve-out set to include SH explicitly with a "same-day boundary" justification analogous to EV's v0.8 extension, OR (b) add a discipline rule that any spec authored post-AR-053 MUST declare §4.a, with SH triggering a spec-edit task to add the envelope. Coverage reviewer prefers (b) — the carve-out exists for grandfathering, not for accepting late-drafted gaps; SH should add §4.a and re-mint a `sh-001a` envelope bead in pilot v0.1.1.

### 3.5 Finding 5 — `§9.3 co-references` table count claim (MINOR, `local`)

**What.** Pilot §1 line 21 declares `§9.3 co-references: 5 entries` and enumerates them. Spec §9.3 (lines 747–751) lists exactly 5 entries: `digital-twins.md`, `end-to-end-testability.md`, `bootstrapping-self-building.md`, `bootstrap.md §5 step 8`, `subsystems/scenario-harness.md`. Count matches. Per discipline §3.1 no-edge list (`docs/components/internal/` doc cites and analogous goal/concept docs), no edges fire. Verified.

**Severity.** MINOR (informational confirmation, not a finding).

**Lane.** `local`.

**Suggested patch.** None.

---

## 4. Phantom-ID and stale-reference checks

### 4.1 Phantom IDs (in pilot, not in spec)

`grep -E 'req:SH-[0-9]+[a-z]?' sh-pilot.md | grep -oE 'SH-[0-9]+[a-z]?' | sort -u` returns exactly the 36 §4 IDs in the spec plus a single `SH-5` artifact (a stray prose mention of "(per SH-5)" — not a `req:` tag, not bead-bound; discounted). No phantom IDs.

`grep -E 'req:.*SH-INV' sh-pilot.md` returns SH-INV-001..SH-INV-005 — exact match to spec §5.

### 4.2 Stale references

- Spec version: `0.2.0` (front-matter) ↔ pilot pin `v0.2.0` (line 9) ↔ yaml `spec_version: "0.2.0"`. **No stale flag.**
- Discipline version: `0.9` (front-matter) ↔ pilot pin `v0.9` (line 3) ↔ yaml comment `Discipline version applied: v0.9` (line 4). **No stale flag.**
- Sibling cross-spec mnem-map references: yaml `cross_specs:` block points at `docs/decompose-to-tasks/mnem-maps/{ar,em,ev,hc,wm,pl,on}-mnem-map.csv`. Verified all 7 files exist (per `git status` showing all mnem-map csvs were added in current branch).

---

## 5. Final tally

| Severity | Count |
|---|---|
| BLOCKER | 0 |
| MAJOR | 2 (Finding 1 tally arithmetic; Finding 4 §4.a envelope class flag) |
| MINOR | 3 (Finding 2 §8 edge-count drift; Finding 3 ON ref informational; Finding 5 §9.3 co-ref confirmation) |

**Lane breakdown:**
- `local`: 4 (Findings 1, 2, 3, 5)
- `class`: 1 (Finding 4)

**No BLOCKER findings.** The pilot is structurally complete and, modulo the arithmetic patches in Findings 1-2 and the discipline-lane handling of Finding 4 (§4.a envelope), is loadable into Beads.

---

## 6. Notes on coverage-reviewer remit boundaries

This review covers §3.1 method only. The following are explicitly out of scope and routed to the parallel reviewers:

- **Decomposition-quality concerns** — F-pilot-SH-1 (three coalesce candidates evaluated and rejected), F-pilot-SH-7 (SH-018 / SH-INV-001 forbidden-token-list shared but kept separate per §2.5 F12 sensor↔impl one-way), and the pilot author's open question OQ-pilot-SH-1 ("§2.3 three-AND coalesce test too strict?") are the decomposition-quality reviewer's call.
- **Cross-spec edge audit** — Whether the 40 cross-spec edges precisely match every normative-prose inline cite in the source spec, and whether `sh-024 → ev-schema.jsonl-line` should be emitted (F-pilot-SH-5 / OQ-pilot-SH-4), is the reference reviewer's call.
- **Test-infra extraction gap** — F-pilot-SH-6 (network-sandbox harness, `getppid()`-walk inspector, determinism-rerun harness) is the decomposition-quality reviewer's call.
- **Spec correctness** — Whether SH-016a's daemon-CWD-coupling is the right mechanism, whether OQ-SH-013's macOS `pf` mechanism is sound, etc., is spec review (already concluded at v0.2.0 reviewed status); not in scope here.

The §4.a envelope flag at Finding 4 was raised here because the §4 enumeration step necessarily traversed §4 headings and discovered the structural gap at the §4-section boundary; that probe falls within coverage remit.

---

## 7. Appendix — Full coverage matrix (audit trail)

Per discipline §1 ("the discipline must produce repeatable output"), the appendix enumerates every spec ID one row at a time, naming the bead that covers it and the pilot-section reference. This is the artifact a second coverage reviewer would diff their pass against to localise any disagreement.

### 7.1 §4 normative requirements (36)

| Spec ID | Pilot bead mnem | Pilot section | Verified |
|---|---|---|---|
| SH-001 | `sh-001` | §2 row 1 | yes |
| SH-002 | `sh-002` | §2 row 2 | yes |
| SH-003 | `sh-003` | §2 row 3 | yes |
| SH-004 | `sh-004` | §2 row 4 | yes |
| SH-005 | `sh-005` | §2 row 5 | yes |
| SH-006 | `sh-006` | §2 row 6 | yes |
| SH-007 | `sh-007` | §2 row 7 | yes |
| SH-008 | `sh-008` | §2 row 8 | yes |
| SH-009 | `sh-009` | §2 row 9 | yes |
| SH-010 | `sh-010` | §2 row 10 | yes |
| SH-011 | `sh-011` | §2 row 11 | yes |
| SH-012 | `sh-012` | §2 row 12 | yes |
| SH-013 | `sh-013` | §2 row 13 | yes |
| SH-014 | `sh-014` | §2 row 14 | yes |
| SH-015 | `sh-015` | §2 row 15 | yes |
| SH-015a | `sh-015a` | §2 row 16 | yes |
| SH-016 | `sh-016` | §2 row 17 | yes |
| SH-016a | `sh-016a` | §2 row 18 | yes |
| SH-017 | `sh-017` | §2 row 19 | yes |
| SH-018 | `sh-018` | §2 row 20 | yes |
| SH-019 | `sh-019` | §2 row 21 | yes |
| SH-020 | `sh-020` | §2 row 22 | yes |
| SH-021 | `sh-021` | §2 row 23 | yes |
| SH-022 | `sh-022` | §2 row 24 | yes |
| SH-023 | `sh-023` | §2 row 25 | yes |
| SH-024 | `sh-024` | §2 row 26 | yes |
| SH-025 | `sh-025` | §2 row 27 | yes |
| SH-026 | `sh-026` | §2 row 28 | yes |
| SH-027 | `sh-027` | §2 row 29 | yes |
| SH-028 | `sh-028` | §2 row 30 | yes |
| SH-029 | `sh-029` | §2 row 31 | yes |
| SH-030 | `sh-030` | §2 row 32 | yes |
| SH-031 | `sh-031` | §2 row 33 | yes |
| SH-032 | `sh-032` | §2 row 34 | yes |
| SH-033 | `sh-033` | §2 row 35 | yes |
| SH-034 | `sh-034` | §2 row 36 | yes |

### 7.2 §5 invariants (5)

| Spec ID | Pilot bead mnem | Pilot section | Verified |
|---|---|---|---|
| SH-INV-001 | `sh-inv-001` | §3 row 1 | yes |
| SH-INV-002 | `sh-inv-002` | §3 row 2 | yes |
| SH-INV-003 | `sh-inv-003` | §3 row 3 | yes |
| SH-INV-004 | `sh-inv-004` | §3 row 4 | yes |
| SH-INV-005 | `sh-inv-005` | §3 row 5 | yes |

### 7.3 §6.1 RECORDs (11)

| Spec RECORD | Pilot bead mnem | Pilot section | Verified |
|---|---|---|---|
| ScenarioFile | `sh-schema.scenario-file` | §4 row 1 | yes |
| AgentOverride | `sh-schema.agent-override` | §4 row 2 | yes |
| FixtureSetup | `sh-schema.fixture-setup` | §4 row 3 | yes |
| GitSeedOp | `sh-schema.git-seed-op` | §4 row 4 | yes |
| FileSeed | `sh-schema.file-seed` | §4 row 5 | yes |
| EventExpectation | `sh-schema.event-expectation` | §4 row 6 | yes |
| WorkspacePredicate | `sh-schema.workspace-predicate` | §4 row 7 | yes |
| OutcomeExpectation | `sh-schema.outcome-expectation` | §4 row 8 | yes |
| ScenarioResult | `sh-schema.scenario-result` | §4 row 9 | yes |
| AssertionResult | `sh-schema.assertion-result` | §4 row 10 | yes |
| SuiteResult | `sh-schema.suite-result` | §4 row 11 | yes |

### 7.4 §8 error classes (8 + precedence)

All folded into single `sh-error.taxonomy` bead per discipline §2.11(c) below-threshold rule (8 < 11). Description text in pilot §4 row 12 verbatim enumerates: §8.0 precedence ordering; §8.1 `scenario-load-failure`; §8.2 `twin-binary-not-found`; §8.3 `fixture-setup-failed`; §8.4 `orchestration-internal-error`; §8.5 `assertion-failed`; §8.6 `scenario-timeout`; §8.7 `harness-internal-error`; §8.8 `cleanup-failed`. **All 8 classes covered.**
