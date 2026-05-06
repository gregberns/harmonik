# RC Pilot — Coverage Reviewer (r1)

**Reviewer**: coverage (per `pilot-review-protocol.md` §3.1)
**Pilot under review**: `docs/decompose-to-tasks/rc-pilot.md` v0.1.0 + `rc-pilot-data.yaml`
**Source spec**: `specs/reconciliation/spec.md` v0.4.0 + `specs/reconciliation/schemas.md` v0.4.0 (status `reviewed` / `supplement`)
**Date**: 2026-04-30
**Note**: RC is the LAST pilot in the corpus.

## 1. Method recap

Per §3.1: enumerate every §4 ID, every §5 invariant, every §6 schema (BOTH spec.md + schemas.md), every §8 category, every retired ID; verify each has a corresponding pilot bead; verify §1 + §9 counts match; verify spec-version reference v0.4.0.

## 2. Enumeration vs pilot coverage

### 2.1 §4 normative requirements (43 active)

Direct count: 48 `#### RC-` headings minus 5 RC-INV (3 retired + 2 active) = **43 §4 reqs**, distributed:

| Section | Count | IDs |
|---|---|---|
| §4.1 | **10** | RC-001, RC-002, RC-003, RC-002a, RC-002b, RC-003a, RC-003b, RC-004, RC-005, RC-006 |
| §4.2 | 3 | RC-007, RC-008, RC-009 |
| §4.3 | **9** | RC-010, RC-011, RC-012, RC-012a, RC-013, RC-014, RC-019a, RC-020a, RC-020b |
| §4.4 | 6 | RC-015, RC-015a, RC-016, RC-017, RC-018, RC-019 |
| §4.5 | 11 | RC-020, RC-021, RC-022, RC-022a, RC-023, RC-024, RC-025, RC-025a, RC-026, RC-026a, RC-027 |
| §4.6 | 3 | RC-028, RC-029, RC-030 |
| §4.7 | 1 | RC-031 |
| **Total** | **43** | |

Cross-check vs pilot bead set (yaml `rc-NNN` mnemonics): all 43 IDs are 1:1 mapped to a first-class `rc-NNN` bead (no §2.3 coalesce, no §2.1a collapse, no §2.2 split applied — F-pilot-RC-2/3/5 confirm).

**Verdict**: §4 coverage CLEAN — every requirement ID has a corresponding bead. No missed IDs. No phantom IDs (every `rc-NNN` mnemonic in yaml maps to a real `RC-NNN` heading).

### 2.2 §5 invariants

Spec `§5` declares 5 RC-INV-NNN headings:
- **Active**: RC-INV-001, RC-INV-004
- **Retired (preserved as stubs)**: RC-INV-002, RC-INV-003, RC-INV-005

Pilot mints: `rc-inv-001`, `rc-inv-004` (2 sensor beads). Retired stubs not minted (per discipline §2.11(b) retired-ID rule).

**Verdict**: §5 coverage CLEAN. STATUS.md narrative ("3 retired during R1, 2 rewritten as cross-subsystem") matches the pilot's posture (3 retired stubs preserved, 2 active rewritten as cross-subsystem cuts: RC-INV-001 across RC-001/002/002a/021/025; RC-INV-004 across detector dispatch + EV-023a). Pilot correctly records the sensor→sensor edge `rc-inv-001 → em-inv-005` per F-pilot-RC-8.

### 2.3 §6 schemas (multi-file: spec.md §6 INDEX + schemas.md §6 normative)

**schemas.md content:**
- §6.1: 8 RECORDs (SnapshotToken, InvestigatorInput, WorkspaceState, VerdictEvent, BudgetExhaustedPayload, StaleVerdictPayload, MalformedVerdictPayload, VerdictExecutedPayload) + 5 ENUMs (GitInProgressOp, Verdict, ReconciliationCategory, MalformationReason, StaleDivergenceReason) = 13 declarations
- §6.4: 1 TRAILER (Harmonik-Verdict-Executed)
- §6.5: 1 ENUM (WorkflowClass) wrapped in EXTENSION OF ExecutionModel.Workflow

Total normative schema items: **15** (8 RECORDs + 5 ENUMs + 1 TRAILER + 1 EXTENSION-ENUM).

**spec.md §6** is an INDEX deferring to schemas.md (per F-pilot-RC-6); contains no original schema declarations.

Pilot mints 15 `rc-schema.<type-kebab>` beads — all 15 source items are accounted for 1:1.

**Verdict**: §6 coverage CLEAN. Multi-file pattern correctly applied per discipline §2.11(a).

### 2.4 §8 error / category taxonomy

Spec §8 declares **11 categories**: §8.1 Cat 0; §8.2 Cat 1; §8.3 Cat 2; §8.4 Cat 3; §8.4a Cat 3a; §8.5 Cat 3b; §8.6 Cat 3c; §8.7 Cat 4; §8.8 Cat 5; §8.11 Cat 6a; §8.11a Cat 6b. (§8.9/§8.10 are intentional holes from earlier numbering; §8.12 = action-mapping table; §8.13 = failure-commit deferral OQ.)

Pilot mints **12 taxonomy beads**: `rc-error.taxonomy` (umbrella) + 11 per-category (`cat-0`..`cat-6b`). Per F-pilot-RC-4 SHAPE-not-COUNT canonical case (each category is independent code/work, not a sentinel value of a single vocabulary — applies §2.11(c) one-bead-per-§8.x rule). Yaml count of taxonomy beads = 12 ✓.

**Verdict**: §8 coverage CLEAN. All 11 categories mapped + umbrella per discipline §2.11(c).

### 2.5 Retired IDs

- RC-INV-002, RC-INV-003, RC-INV-005 — all three retired, stubs preserved in §5, not minted as beads. ✓
- No retired §4 IDs (no `[retired]` markers on RC-NNN headings).

**Verdict**: retired-ID handling CLEAN.

## 3. Tally arithmetic verification

Pilot §1 sanity tally:

| Class | Pilot count | Verified |
|---|---|---|
| Spec parent bead (`rc`) | 1 | ✓ (epic in yaml) |
| Requirement beads | 43 | ✓ (43 active §4 reqs, no coalesce/collapse/split) |
| Sensor / invariant beads | 2 | ✓ (RC-INV-001, RC-INV-004) |
| Schema beads | 15 | ✓ (8 RECORD + 5 ENUM in §6.1, +1 TRAILER §6.4, +1 ENUM §6.5) |
| Error-taxonomy beads | 12 | ✓ (1 umbrella + 11 per-category) |
| Test-infrastructure beads | 7 | ✓ (per §10.2 obligation grouping) |
| **Total** | **80** | **✓** (1 epic + 79 children) |

Yaml child-bead count: **79** (verified via `grep -c "^  - mnem:"`). 1 epic + 79 = 80. Multiplier 79/43 = **1.84×** (matches pilot claim of corpus-highest).

Pilot §9 reproduces the same tally arithmetic. ✓

## 4. Spec-version reference

Pilot front-line claim: drafted against `specs/reconciliation/spec.md` v0.4.0 + `specs/reconciliation/schemas.md` v0.4.0 (status `reviewed` / `supplement`, last-updated 2026-04-24).

Source spec.md front-matter: `version: 0.4.0`, `status: reviewed`, `last-updated: 2026-04-24`. ✓
Source schemas.md front-matter: `version: 0.4.0`, `status: supplement`, `last-updated: 2026-04-24`. ✓

**Verdict**: stale-version flag NOT triggered. Spec-version reference matches.

## 5. Findings

### Finding C-RC-1 — Per-section narrative count typos in §1 (MINOR / local)

Pilot §1 line 16 narrative description claims:

> "Distributed across §4.1 reconciliation-as-workflow (9: ...) ... + §4.3 detectors (8: ...)"

Actual counts: **§4.1 = 10** (RC-001, RC-002, RC-003, RC-002a, RC-002b, RC-003a, RC-003b, RC-004, RC-005, RC-006 — direct count ten); **§4.3 = 9** (RC-010, RC-011, RC-012, RC-012a, RC-013, RC-014, RC-019a, RC-020a, RC-020b — direct count nine). The pilot's same-line enumeration of IDs in parens correctly lists all 10 / 9 IDs respectively, but prefixes the wrong cardinal numerals.

The numeric breakdown 9+3+8+6+11+3+1 = 41 in narrative would be inconsistent with the stated total of 43 — but the actual table coverage is complete (all 43 §4 IDs minted as beads). This is a **cosmetic typo in the prose summary**, not a coverage gap.

- **Severity**: MINOR (cosmetic; the bead set is complete and the table headers in §2 correctly say "9 reqs" for §4.1 and "8 reqs" for §4.3 — those headers are also wrong, same typo class).
- **Lane**: `local` — this is a per-pilot enumeration-arithmetic error in narrative summaries; not a discipline rule defect. The bead-graph structure is correct.
- **Suggested fix**: Update §1 line 16 prose ("9: RC-001..006 + RC-002a + RC-002b + RC-003a + RC-003b" → "10: RC-001..006 + RC-002a/002b/003a/003b"; "8 §4.3" → "9 §4.3"); update §2 table headers ("§4.1 ... 9 reqs incl. ..." → "10 reqs incl. ..."; "§4.3 ... 8 reqs" → "9 reqs"). No bead changes.

### Finding C-RC-2 — Schema RECORD/ENUM split typo (MINOR / local)

Pilot §1 + §5 + §9 sanity tally claims schema beads are "10 RECORDs + 5 ENUMs = 15".

Direct enumeration of the 15 schema beads minted in pilot §5:
- **9 RECORDs**: snapshot-token, investigator-input, workspace-state, verdict-event, budget-exhausted-payload, stale-verdict-payload, malformed-verdict-payload, verdict-executed-payload, verdict-executed-trailer (the trailer is filed as a RECORD-shape per pilot §5 first column).
- **6 ENUMs**: git-in-progress-op, verdict, reconciliation-category, malformation-reason, stale-divergence-reason, workflow-class-extension.

So actual split is **9 RECORDs + 6 ENUMs = 15**, not "10 RECORDs + 5 ENUMs". The total of 15 is correct; the split labels are off-by-one.

- **Severity**: MINOR (the 15 schema beads are all correctly minted; only the labelling of the RECORD-vs-ENUM proportion in narrative is wrong).
- **Lane**: `local` — pilot-level enumeration typo in the narrative summary. The discipline doesn't require this split disclosure.
- **Suggested fix**: Update §1 line 21 / §5 prose / §9 tally ("10 RECORDs + 5 ENUMs" → "9 RECORDs + 6 ENUMs" — assuming verdict-executed-trailer is filed as RECORD-shape per pilot §5; if instead the trailer is independent of the RECORD/ENUM count, then "8 RECORDs + 6 ENUMs + 1 TRAILER + 1 EXTENSION = 15"). The chosen wording should reconcile with whichever schema-shape taxonomy the discipline §2.6 prefers.

## 6. Cross-spec implications (LAST-pilot flag)

RC is the last pilot in the corpus. Coverage review surfaces no issues that propagate to other specs, but two cross-spec observations are worth flagging for the synthesis step:

1. **Counts on already-loaded sister pilots are unaffected.** RC's §4 has 43 reqs; the prior pilots' `forward:rc-*` placeholders resolve against this pilot's `rc-NNN` and `rc-error.cat-N` mnemonics. Per pilot §3.4, RC's `depends-on` list is complete (no cycle-break exclusions), so no upstream pilot needs re-review for missed RC-side targets.

2. **Pilot's schema-count typo (Finding C-RC-2) does not propagate.** Cross-spec edges target individual `rc-schema.<type>` mnemonics, not the aggregate "10/5" count. The schema-count error is purely cosmetic narrative.

3. **Per-section count typos (Finding C-RC-1) similarly do not propagate.** Other pilots cite RC-NNN by ID, never "the 9th §4.1 req" or similar.

No cross-spec corrections are needed. The corpus closes cleanly on coverage grounds with these two MINOR cosmetic findings.

## 7. Summary line

**2 MINOR findings, both `local` lane, both cosmetic narrative typos. §4 / §5 / §6 / §8 bead-graph coverage is COMPLETE.** Pilot has no missed IDs, no phantom IDs, no tally arithmetic errors at the bead-count level (1+43+2+15+12+7 = 80 is correct), no stale-version flags. Spec-version reference v0.4.0 matches both spec.md and schemas.md front-matter. The 11-category §8 + 1-umbrella split (12 taxonomy beads) per F-pilot-RC-4 SHAPE-not-COUNT is correctly applied. Multi-file schema pattern per §2.11(a) is correctly applied (15 schemas across spec.md INDEX + schemas.md normative). Retired invariants (RC-INV-002/003/005) correctly handled as preserved stubs not minted as beads. Pilot is structurally ready for load gate; suggested narrative-typo fixes are bumpable to v0.1.1 prose-only patch (no bead-structure changes).
