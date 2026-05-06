# PL Pilot r1 — Coverage Review

- **Reviewer role:** coverage reviewer (per `pilot-review-protocol.md` §3.1)
- **Date:** 2026-04-30
- **Spec under review:** `specs/process-lifecycle.md` v0.4.1 (status `reviewed`, 891 lines, last-updated 2026-04-25)
- **Pilot artifacts under review:**
  - `docs/decompose-to-tasks/pl-pilot.md` (narrative, 291 lines, pilot-version 0.1.0)
  - `docs/decompose-to-tasks/pl-pilot-data.yaml` (canonical data, 1196 lines, 59 child beads + 1 epic)
- **Discipline applied by pilot:** v0.9
- **Method:** §3.1 of `pilot-review-protocol.md` — enumerate every numbered ID / invariant / schema / error-class in the spec; verify pilot bead per `kind:` discriminator; verify counts; verify spec-version reference.

---

## Summary line

**CLEAN.** Zero missed IDs, zero phantom IDs, zero tally inconsistencies, zero stale-version flags. One MINOR cosmetic note about "59 vs 60" total-bead phrasing. Coverage is exact: 43 §4 reqs + 5 invariants + 1 §6.1 schema + 0 §8 (per spec delegation) + 10 test-infra = **59 child beads** (60 with epic). All requirement IDs in the spec map 1:1 to a yaml bead with the correct `kind:` discriminator. No `[retired]` markers exist in the spec. v0.4.1 is referenced consistently across spec front-matter, pilot narrative, yaml top-comment, yaml `spec_version:` field, and yaml epic description.

---

## 1. Method recap

Per `pilot-review-protocol.md` §3.1 the coverage pass enumerates and cross-references:

1. Every numbered ID in PL §4 (`PL-NNN`, `PL-NNNa/b`, plus `PL-ENV-001`).
2. Every invariant in §5 (`PL-INV-NNN`).
3. Every named record / interface / enum in §6.
4. Every error class / category in §8 (PL §8 explicitly delegates — verify the prose).
5. Every requirement marked `[retired]`.
6. Each ID has a corresponding pilot bead per yaml `kind:` discriminator.
7. Pilot §1 counts match steps 1–4 actuals.
8. Pilot §9 tally arithmetic.
9. Spec-version reference v0.4.1 matches `specs/process-lifecycle.md` front-matter.

---

## 2. Step 1 — §4 numbered IDs (43 expected)

Enumeration (from `grep -nE "^#### (PL-|PL-INV-|PL-ENV-)" specs/process-lifecycle.md`):

**§4.a Subsystem envelope (1):** PL-ENV-001
**§4.1 Per-project daemon scope (8):** PL-001, PL-002, PL-002a, PL-002b, PL-003, PL-003a, PL-003b, PL-004
**§4.2 Startup sequence (6):** PL-005, PL-006, PL-006a, PL-007, PL-008, PL-008a
**§4.3 Ready-state transition (4):** PL-009, PL-009a, PL-009b, PL-010
**§4.4 Shutdown (4):** PL-011, PL-011a, PL-012, PL-013
**§4.5 Agent-subprocess management (5):** PL-014, PL-014a, PL-015, PL-016, PL-017
**§4.6 Daemon vs orchestrator-agent (5):** PL-018, PL-018a, PL-019, PL-020, PL-020a
**§4.7 ntm adapter scope (4):** PL-021, PL-021a, PL-022, PL-023
**§4.8 Crash semantics (4):** PL-024, PL-025, PL-025a, PL-026
**§4.9 Upgrade contract (1):** PL-027
**§4.10 Command surface (1):** PL-028

Sub-total: 1 + 8 + 6 + 4 + 4 + 5 + 5 + 4 + 4 + 1 + 1 = **43** ✓ (matches yaml's claim and pilot §1's per-section breakdown).

Yaml has exactly 43 beads with `kind: req` (verified via `grep -E "^    kind: req"` count = 43 — see `pl-pilot-data.yaml` lines 350–727 for the run of `kind: req` beads ending at `pl-env-001`). The 43 yaml `req:` discriminator strings are an exact set-match to the 43 spec IDs. **No missed IDs. No phantom IDs.**

---

## 3. Step 2 — §5 invariants (5 expected)

Spec enumeration: PL-INV-001, PL-INV-002, PL-INV-003, PL-INV-004, PL-INV-005.

Yaml has exactly 5 beads with `kind: invariant` (lines 729–765 of `pl-pilot-data.yaml`). The five `req:` discriminators on those beads are PL-INV-001..005. **Set match. No missed, no phantom.**

---

## 4. Step 3 — §6 schema constructs (1 expected)

Spec §6 declares:

- §6.1 `DaemonStatus` ENUM (the only PL-owned schema; 7 values: starting / reconciling / degraded / ready / paused / draining / stopped).
- §6.2 Co-owned event payloads (8 daemon-lifecycle events whose schemas are owned by EV per discipline §2.11(d.2) — these correctly do NOT mint local schema beads; they fire `pl-NNN → ev-events.<name>` edges instead).
- §6.3 explicitly declares no on-disk schema.

Yaml has exactly 1 bead with `kind: schema` — `pl-schema.daemon-status` (lines 772–777, `schema_kind: enum`). **Match. No missed, no phantom.**

The discipline §2.11(d.2) treatment of the 8 §6.2 co-owned event payloads as cross-spec edges to EV (rather than local schema beads) is the correct disposition per the v0.9 lockstep release; this is not a coverage gap.

---

## 5. Step 4 — §8 error/failure taxonomy (0 expected per yaml claim)

Verification of yaml's claim that PL §8 delegates ownership entirely:

PL §8 prose (`specs/process-lifecycle.md` line 682) reads verbatim:

> "This spec does not own a failure taxonomy. Startup failure modes are cataloged per §PL-008 (obligation owned by [operator-nfr.md §4.1 ON-003]); §PL-008a names the codes this spec consumes from the authoritative ON §8 taxonomy. ... Run-failure taxonomy is owned by [execution-model.md §6.3]. Reconciliation-category taxonomy is owned by [reconciliation/spec.md §8]. Crash semantics (§PL-024, §PL-025, §PL-026) route through those taxonomies rather than defining their own."

This is a clean three-way delegation to ON §8 + EM §6.3 + RC §8. **Yaml's claim is correct: 0 PL-side error-taxonomy beads.** Yaml mints 0 (verified via absence of `kind: error-taxonomy` / `kind: error` lines in `pl-pilot-data.yaml`). F-pilot-PL-1 is a faithful reading of the spec. No coverage gap.

This makes PL the **first pilot in the corpus to declare zero §8 ownership**, consistent with pilot §1 and yaml top-comment claims. The consumer-side codes (5, 6, 7, 8, 9, 10, 14, 19, 22, 23) cited by PL-008a correctly surface as `forward:on-NNN` cross-spec edges (10 codes = 10 edges into the §3 forward log per F-pilot-PL-4), not as local taxonomy beads.

---

## 6. Step 5 — Retired requirements (0 expected)

`grep -niE "retired|\[retired\]" specs/process-lifecycle.md` returns:

- Two hits in §12 revision history: v0.4.0 row "no IDs retired"; v0.3.0 row "Retired. No requirement IDs retired in this pass."

There are **no `[retired]` markers in §4 or §5**. The spec has had no ID retirement across its lifecycle (PL reached `reviewed` cleanly at v0.4.0 per pilot §1). Yaml correctly omits any retirement-handling. No coverage gap.

---

## 7. Step 6 — Per-bead `kind:` discriminator verification

Yaml `kind:` distribution (verified by `grep -cE "^    kind:" pl-pilot-data.yaml`):

| `kind:` value | Count | Expected | Match |
|---|---|---|---|
| `req` | 43 | 43 (§4) | ✓ |
| `invariant` | 5 | 5 (§5) | ✓ |
| `schema` | 1 | 1 (§6.1) | ✓ |
| `test-infra` | 10 | 10 (yaml claim per §10.2) | ✓ |
| (error-taxonomy) | 0 | 0 (§8 delegated) | ✓ |
| **Total child beads** | **59** | **59** | ✓ |

Each `kind: req` bead carries a `req: PL-...` discriminator naming the spec ID it implements; same for `kind: invariant`. The mapping is 1:1 onto the spec.

---

## 8. Step 7 — Pilot §1 counts vs steps 1–4 actuals

Pilot `pl-pilot.md` §1 sanity tally table (lines 29–38):

| Class | Pilot §1 count | Step 1–4 actual | Match |
|---|---|---|---|
| Spec parent bead (`pl`) | 1 | (epic, not in §1–4 scope) | ✓ |
| Requirement beads (43 active §4 reqs) | 43 | 43 | ✓ |
| Step beads | 0 | 0 (4 multi-step candidates collapsed via F8b) | ✓ |
| Sensor / invariant beads | 5 | 5 | ✓ |
| Schema beads | 1 | 1 | ✓ |
| Error-taxonomy beads | 0 | 0 | ✓ |
| Test-infrastructure beads | 10 | 10 | ✓ |
| Total | **60** | **60** | ✓ |

Pilot §1 also enumerates the per-section §4 breakdown (8+6+4+4+5+5+4+4+1+1+1 envelope = 43 — verified above). All section counts match. **No tally inconsistencies in §1.**

---

## 9. Step 8 — Pilot §9 tally arithmetic

Pilot §9 (lines 254–267) repeats the §1 sanity table verbatim and adds:

> "Arithmetic: 1 + 43 + 0 + 5 + 1 + 0 + 10 = **60** (1 epic + 59 children). Bead-to-§4-req multiplier: 59 / 43 = **1.37×**."

Verification:

- 1 + 43 + 0 + 5 + 1 + 0 + 10 = **60** ✓
- 59 / 43 = 1.3721... → rounded to **1.37×** ✓
- Yaml top-comment line 86 also writes "59 / 43 = 1.37×" ✓

**Arithmetic clean.**

---

## 10. Step 9 — Spec-version reference (v0.4.1)

Cross-document check:

| Location | Reference | Match |
|---|---|---|
| `specs/process-lifecycle.md` line 11 (front-matter) | `version: 0.4.1` | (source) |
| `pl-pilot.md` line 3 | "v0.4.1 (status `reviewed`, last-updated 2026-04-25)" | ✓ |
| `pl-pilot.md` line 13 | "v0.4.1, status `reviewed` (891 lines)" | ✓ |
| `pl-pilot.md` line 46 | "Implements specs/process-lifecycle.md v0.4.1" (epic description) | ✓ |
| `pl-pilot.md` line 287 | "Initial PL pilot draft against `specs/process-lifecycle.md` v0.4.1" (rev hist) | ✓ |
| `pl-pilot-data.yaml` line 2 | "Drafted 2026-04-30 against specs/process-lifecycle.md v0.4.1 (status: reviewed)." | ✓ |
| `pl-pilot-data.yaml` line 315 | `spec_version: "0.4.1"` | ✓ |
| `pl-pilot-data.yaml` line 340 | "Implements specs/process-lifecycle.md v0.4.1 ..." | ✓ |
| `pl-pilot-data.yaml` line 734 | "line-3 check applies for v0.4.1+ pidfiles" | ✓ |

All v0.4.1 references are consistent. **No stale-version flags.**

The 891-line count cited in pilot §1 also matches `wc -l specs/process-lifecycle.md` (891). The 2026-04-25 last-updated date matches the spec front-matter and the §12 revision history's most-recent row.

---

## 11. Findings

### BLOCKER: 0
None.

### MAJOR: 0
None.

### MINOR: 1

**MINOR-001 [local lane] — "Total: 59 beads" wording in §10 revision history is inconsistent with §1 / §9 "60" framing.**

- **Where:** `pl-pilot.md` line 287, revision-history cell for v0.1.0 reads "Total: **59 beads** (1 spec-parent + 43 req + 0 step + 5 sensor + 1 schema + 0 taxonomy + 10 test-infra)."
- **Why MINOR not MAJOR:** the arithmetic in the breakdown sums to 60 (1+43+0+5+1+0+10 = 60), and §1 + §9 both consistently use "60 (1 epic + 59 children)" framing. The §10 sentence appears to count "59 child beads" but labels the total as "59" while including the spec-parent in the parenthetical breakdown. This is a labeling/wording inconsistency, not a count error — every actual bead is accounted for in both framings.
- **Lane:** local (PL-pilot wording fix only; no discipline-document impact; no other pilot exhibits this exact phrasing pattern).
- **Suggested fix:** rewrite as "Total: **60 beads** (1 spec-parent + 43 req + 0 step + 5 sensor + 1 schema + 0 taxonomy + 10 test-infra; arithmetic 1+43+0+5+1+0+10 = 60 = 1 epic + 59 children)" OR rewrite as "Total child beads: **59** (43 req + 0 step + 5 sensor + 1 schema + 0 taxonomy + 10 test-infra), plus 1 spec-parent epic = 60 beads loaded."
- **Severity rationale:** does not affect coverage, does not affect load correctness, does not affect any reviewer's ability to spot-check beads. Pure wording polish.

---

## 12. Cross-cutting observations (informative; not findings)

These are noted in the spirit of pilot-review-protocol.md §3.1's "informative findings welcomed" clause; they are NOT blocker / major / minor on the coverage axis.

1. **Schema bead minimalism is correct and matches §6 prose.** Of the four schema-eligible items mentionable in §6 (DaemonStatus enum, the eight co-owned event payloads, §6.3 schema-evolution rule, the in-text records like `PidfileLock` / `OrphanSweepReport` named in PL-ENV-001's envelope `(c) types introduced` slot), only DaemonStatus carries a §6.1 normative ENUM declaration. The yaml correctly mints exactly one schema bead for it. The PL-ENV-001 envelope's `(c) types introduced` list is a declaration-by-enumeration that locates types in their owning specs (Run in EM, event payloads in EV, etc.) — these are not §6 declarations and correctly do not mint additional schema beads in this pilot.

2. **§7.1 state-machine coverage is implicit via PL-009 / PL-010 / PL-011 / PL-011a / PL-012 / PL-013 / PL-024 / PL-025 + PL-INV-001 sensor.** The state-machine table at §7.1 lines 665–676 enumerates 11 transition rows; each transition's emit-event and guard is owned by a §4 requirement that has a yaml bead (verified by spot-check: (init)→starting from PL-005 step 2; starting→reconciling from PL-006/PL-007; starting→degraded from PL-010; degraded→reconciling from PL-010 retry; reconciling→ready from PL-009; ready→draining from PL-011; draining→stopped from PL-011 step 9; SIGKILL/panic from PL-024/PL-025). No transition is orphaned from a §4 owner. State-machine coverage is therefore folded into §4 coverage and does not require independent state-machine beads.

3. **The 4 multi-step F8b collapses (PL-005 / PL-006 / PL-011 / PL-027) are visibly preserved as sub-bullets in their bead descriptions.** Spot-checked PL-005 (yaml line 433 — 11 steps 0..9 plus 3a + 8a all present), PL-006 (line 442 — 6 bullets a..f all present), PL-011 (line 523 — 9 steps all present), PL-027 (line 713 — 5 sub-rules (i)..(v) all present). F8b shared-function-body collapse is a discipline-sanctioned pattern and the "step beads = 0" tally is honestly representative — no internal step is hidden.

4. **`PL-013 daemon does not exit on queue-empty` and `PL-022 ntm adapter MUST NOT consume workflow-semantic features` and `PL-023 handler contract is the ntm boundary` are negative-rule requirements** with prose that is shorter than typical. They each correctly mint a single `kind: req` bead even though their "what to build" is "lint or refusal logic, not a feature." This is consistent with how WM, BI, HC pilots handled negative-rule reqs and is not a coverage gap.

5. **PL-INV-001's sensor predicate explicitly references `daemon_instance_id` line-3 of the pidfile** (added in v0.4.1 per §12 revision-history item (4)). The yaml invariant-bead description (line 734) carries this updated predicate verbatim including the v0.4.0 backwards-compat fall-through. No version drift.

6. **Forward-deferred edge counts are NOT a coverage-pass concern**, but as a sanity confirmation the §3.2 forward log claims "13 rc + 26 on = 39 forward edges" and the discipline-lane finding F-pilot-PL-4 is class-lane (the named-obligation cycle-break tension). This is not a coverage failure; it surfaces correctly via the protocol's discipline-lane class queue, not the coverage-lane local queue.

---

## 13. Conclusion

The PL pilot's coverage of `specs/process-lifecycle.md` v0.4.1 is **complete and faithful**. Every numbered requirement, every invariant, the sole §6 schema construct, and the (zero-by-delegation) §8 surface have a correctly-discriminated yaml bead. Pilot §1 and §9 counts match the spec actuals. Tally arithmetic is correct. v0.4.1 references are consistent across all artifacts. No requirements are retired and the pilot correctly omits any retirement handling.

The single MINOR finding is wording polish on the §10 revision-history line; it does not affect any load operation, reviewer spot-check, or downstream consumer.

**Coverage verdict: CLEAN.**

---

## 14. Files referenced

- `/Users/gb/github/harmonik/specs/process-lifecycle.md` (spec, v0.4.1)
- `/Users/gb/github/harmonik/docs/decompose-to-tasks/pl-pilot.md` (pilot narrative, v0.1.0)
- `/Users/gb/github/harmonik/docs/decompose-to-tasks/pl-pilot-data.yaml` (canonical bead+edge data)
- `/Users/gb/github/harmonik/docs/decompose-to-tasks/discipline.md` (v0.9 — applied by pilot)
- `/Users/gb/github/harmonik/docs/decompose-to-tasks/pilot-review-protocol.md` (§3.1 — review method)
