# WM Pilot r1 — Coverage Review

**Reviewer:** coverage reviewer (per `pilot-review-protocol.md` §3.1)
**Date:** 2026-04-30
**Inputs:** `specs/workspace-model.md` v0.4.2 (1244 lines, status `reviewed`), `docs/decompose-to-tasks/wm-pilot.md` v0.1.0 (274 lines), `docs/decompose-to-tasks/wm-pilot-data.yaml` (1250 lines, 71 child beads), `docs/decompose-to-tasks/discipline.md` v0.9.

**Headline:** **NEAR-CLEAN** — coverage of every WM ID, invariant, schema, and error class is verified 1:1 with no missed and no phantom IDs. One MINOR arithmetic inconsistency in the §1 + §9 sanity tallies (1+54+0+4+5+1+7 = **72**, not 71 as printed) is the only finding; no BLOCKER or MAJOR issues.

---

## 1. Method

Per pilot-review-protocol §3.1, I:

1. Enumerated every WM-NNN ID in spec §4 (including `a/b/c/d/e` letter suffixes and `WM-ENV-NNN`).
2. Enumerated every `WM-INV-NNN` in §5.
3. Enumerated every record / enum in §6.
4. Enumerated every error class in §8.
5. Enumerated every requirement explicitly marked `[retired]`.
6. For each enumerated ID, verified pilot accounts via yaml `req:` field, single-bead taxonomy enumeration, or recorded retirement.
7. Verified pilot §1 + §9 counts and arithmetic.
8. Verified spec-version reference.

---

## 2. Spec enumeration (authoritative count)

### 2.1 §4 numbered + envelope reqs

From `grep -n "^#### WM-" specs/workspace-model.md` (60 total `#### WM-` headers):

- **§4.a envelope (2):** `WM-ENV-001`, `WM-ENV-002`.
- **§4.1–§4.10 numbered (53 headers, of which 1 retired):** `WM-001`, `WM-002`, `WM-003`, `WM-003a`, `WM-004`, `WM-005`, `WM-005a`, `WM-006`, `WM-006a`, `WM-007`, `WM-008`, `WM-009`, `WM-010`, `WM-011`, `WM-012`, `WM-013`, `WM-013a`, `WM-013b`, `WM-013c`, `WM-013d`, `WM-013e`, `WM-014`, `WM-015`, `WM-016`, **`WM-017` (retired, line 370)**, `WM-018`, `WM-018a`, `WM-019`, `WM-019a`, `WM-020`, `WM-021`, `WM-022`, `WM-022a`, `WM-023`, `WM-024`, `WM-025`, `WM-026`, `WM-027`, `WM-028`, `WM-029`, `WM-030`, `WM-031`, `WM-032`, `WM-033`, `WM-034`, `WM-035`, `WM-036`, `WM-037`, `WM-037a`, `WM-038`, `WM-038a`, `WM-039`, `WM-040`.

**Active §4 reqs:** 53 numbered − 1 retired (WM-017) + 2 envelope = **54 active**. ✓ matches yaml claim.

### 2.2 §5 invariants

From `grep "^#### WM-INV-"` (5 headers):

- **Active (4):** `WM-INV-001`, `WM-INV-002`, `WM-INV-003`, `WM-INV-005`.
- **Retired (1):** `WM-INV-004` (spec §5 lines 674–678; retired in v0.3.0 as duplicate of §4.6.WM-022).

✓ matches yaml claim.

### 2.3 §6 schemas (5 records/enums)

From `specs/workspace-model.md §6.1` (lines 690–749):

1. `Workspace` (RECORD, 12 fields)
2. `LeaseLockFile` (RECORD, 4 fields)
3. `WorkspaceState` (ENUM, 7 values: `created`, `ready`, `leased`, `merge-pending`, `conflict-resolving`, `merged`, `discarded`)
4. `InterruptState` (ENUM, 5 values: `none`, `operator-paused`, `operator-stopped-graceful`, `operator-stopped-immediate`, `daemon-crash-suspected`)
5. `SessionMetadataSidecar` (RECORD, 7 fields)

✓ matches yaml claim of 5.

### 2.4 §8 error / failure classes

From `specs/workspace-model.md §8` table (lines 952–965): 12 typed sentinel classes — `WorkspaceAlreadyExists`, `RunIdReuseForbidden`, `WorktreeCreationFailed`, `LeaseLockHeldByOrphan`, `SidecarWriteFailed`, `MergeConflictUnresolvable`, `InterruptOnTerminalWorkspace`, `RefNameInvalid`, `BareWorktreeNoLease`, `SidecarWithoutLease`, `GitignoreWriteForbidden`, `GitVersionTooOld`.

✓ matches yaml claim of 12.

### 2.5 Retired IDs

- `WM-017` — retired v0.3.0 (spec §4.4 line 370–372). Reason: payload-field declarations folded into EV-025.
- `WM-INV-004` — retired v0.3.0 (spec §5 line 674–678). Reason: single-subsystem rule subsumed into WM-022 + WM-024.

Both ID-frozen per v0.4.0 NOTE; MUST NOT be reused.

---

## 3. Pilot accounting verification

### 3.1 §4 reqs — yaml `req:` field cross-check

`grep -E "^    req: WM-" wm-pilot-data.yaml | sort -u` yields **58 unique req values**:
- 54 §4 reqs (incl. 2 envelope) — every spec ID accounted for, none duplicated.
- 4 invariant reqs (WM-INV-001/002/003/005).

**Result:** 1:1 mapping. No missed, no phantom, no duplicate.

### 3.2 §5 invariants

- `wm-inv-001` → WM-INV-001 ✓
- `wm-inv-002` → WM-INV-002 ✓
- `wm-inv-003` → WM-INV-003 ✓
- `wm-inv-005` → WM-INV-005 ✓
- WM-INV-004 retired and correctly NOT bead-loaded ✓ (pilot §4 footer notes the retirement explicitly).

### 3.3 §6 schemas

- `wm-schema.workspace` ✓
- `wm-schema.lease-lock-file` ✓
- `wm-schema.workspace-state` ✓
- `wm-schema.interrupt-state` ✓
- `wm-schema.session-metadata-sidecar` ✓

5/5 ✓.

### 3.4 §8 error taxonomy

Single bead `wm-error.taxonomy` enumerates all 12 spec sentinel classes verbatim with each class's transition consequence + downstream routing rule. ✓ Per discipline §2.6 BI-shape rule (single bead for flat typed-sentinel set, vs multi-stage RC-shape that splits per §8.x). The 12-class count is one above RC's descriptive ~11 figure in discipline §2.11(c) — the SHAPE discriminator (flat vs nested) is what drives the decision per F-pilot-WM-2; classification is correct.

### 3.5 Retired IDs

- WM-017 — correctly NOT minted as a bead; pilot §2 footer ("Retired §4 IDs: WM-017") and yaml omission both confirm. ✓
- WM-INV-004 — correctly NOT minted as a bead; pilot §4 footer ("Note on retired invariant") confirms. ✓

### 3.6 Coalesce-rejection verification (pilot's claim of 0 §2.3 coalesces)

Per pilot F-pilot-WM-1, two candidates were considered and rejected:
- **WM-022 + WM-022a** — both present in spec at lines 429 and 446 (separate `####` headers). Both materialize as separate beads (`wm-022`, `wm-022a`). The split is justified: WM-022 declares the sidecar-walk identification mechanism; WM-022a declares the all-mechanical-branch escalation rule (skip re-dispatch, emit `merge_conflict_escalation` directly). They share a function context but the escalation path is its own testable rule. Rejection of coalesce is **correct per discipline §2.3 three-AND test**.
- **WM-037 + WM-037a** — both present in spec at lines 587 and 595 (separate `####` headers). Both materialize as separate beads (`wm-037`, `wm-037a`). WM-037 declares orthogonality + the 5-value enum; WM-037a is the terminal-state-clearance rule with its own silent-reject sensor (`InterruptOnTerminalWorkspace`). Rejection of coalesce is **correct**.

Both candidate-rejections are well-grounded.

---

## 4. Tally + version verification

### 4.1 Spec-version reference

- Spec front-matter line 11: `version: 0.4.2` ✓
- Spec front-matter line 14: `last-updated: 2026-04-25` ✓
- Pilot line 1 / line 13: `v0.4.2 ... last-updated 2026-04-25` ✓
- Yaml line 250: `spec_version: "0.4.2"` ✓

**Result:** No stale-version flag.

### 4.2 Pilot §1 sanity tally — arithmetic check

Pilot §1 table (replicated identically in §9):

| Class | Count |
|---|---|
| Spec parent bead (`wm`) | 1 |
| Requirement beads | 54 |
| Step beads | 0 |
| Sensor / invariant beads | 4 |
| Schema beads | 5 |
| Error-taxonomy beads | 1 |
| Test-infrastructure beads | 7 |
| **Total WM beads** | **71** |

Stated arithmetic (pilot §1 line 40 + §9 line 252): `1 + 54 + 0 + 4 + 5 + 1 + 7 = 71`.

**Actual arithmetic:** `1 + 54 + 0 + 4 + 5 + 1 + 7 = 72`.

**Yaml inventory:** 71 child `mnem:` entries in `beads:` array (verified via `grep -c "^  - mnem: "`). The epic `wm` is declared in a separate `epic:` block above `beads:`, NOT counted as a `mnem:` entry inside `beads:`.

**The inconsistency:** the table lists "Spec parent bead = 1" as a row contributing to the sum, then states the total is 71. Either:
- (a) the row is correctly part of the sum and the total should read **72**, or
- (b) the total is correctly 71 and reflects the count of beads inside the `beads:` array (54 req + 4 inv + 5 schema + 1 taxonomy + 7 test-infra = 71), in which case the "Spec parent bead = 1" row should not have been in the sum.

Either reading is internally fixable; the issue is that the printed arithmetic does not actually equal 71. This is a **MINOR** finding — the bead inventory itself is correct (71 child beads + 1 epic), only the presented arithmetic is wrong by one.

Pilot lines affected: §1 line 40 (`1 + 54 + 0 + 4 + 5 + 1 + 7 = **71**`), §9 line 252 (`Arithmetic: 1 + 54 + 0 + 4 + 5 + 1 + 7 = **71**. ✓`). Suggested fix: either correct the arithmetic to `= 72` and make "Total WM beads" 72 (epic-inclusive reading), OR drop the "Spec parent bead = 1" row from the sum and keep total at 71 (children-only reading) — for parity with EM/HC/CP pilots the latter is likely intended.

### 4.3 Component-count verification

- `kind: req` count in yaml: **54** ✓ matches table
- `kind: invariant` count: **4** ✓
- `kind: schema` count: **5** ✓
- `kind: error-taxonomy` count: **1** ✓
- `kind: test-infra` count: **7** ✓
- Step beads: **0** (pilot claims 0 multi-step splits per F-pilot-WM-3 F8b shared-function-body tiebreaker) ✓
- Total non-epic: 54 + 0 + 4 + 5 + 1 + 7 = **71** ✓ matches yaml's 71 child beads

The components themselves are accurately enumerated and counted; the issue is purely the row labeled "Spec parent bead" being included in the sum-line while the printed total excludes it.

---

## 5. Findings summary

| Severity | Count | Findings |
|---|---|---|
| BLOCKER | 0 | — |
| MAJOR | 0 | — |
| MINOR | 1 | F-coverage-WM-r1-1 — Sanity-tally arithmetic mismatch (`1 + 54 + 0 + 4 + 5 + 1 + 7 = 72`, printed as 71) in pilot §1 line 40 + §9 line 252. Component counts are correct; only the sum line is wrong. |

### F-coverage-WM-r1-1 — Tally arithmetic inconsistency

**Severity:** MINOR
**Lane:** `local` — applies to a single pilot's printed arithmetic; not a discipline-doc issue.
**Lane justification:** The discrepancy is internal to wm-pilot.md's tally row; no other pilot's tally is affected and no discipline rule is at issue. Mechanical fix: pilot author chooses one of the two readings (72 inclusive of epic, or 71 with the "Spec parent bead" row dropped from the sum) and updates §1 line 40 + §9 line 252 to match.
**Files:** `docs/decompose-to-tasks/wm-pilot.md` lines 40, 252.

### CLEAN summary

- ✓ Every active spec §4 ID (52 numbered + 2 envelope = 54) maps 1:1 to a `kind: req` bead.
- ✓ Every active §5 invariant (4) maps 1:1 to a `kind: invariant` bead.
- ✓ Every §6 schema (5) maps 1:1 to a `kind: schema` bead.
- ✓ All 12 §8 sentinel classes are enumerated inside the single `wm-error.taxonomy` umbrella per discipline §2.6 BI-shape rule.
- ✓ Both retired IDs (WM-017, WM-INV-004) are correctly NOT bead-loaded and explicitly noted.
- ✓ No phantom IDs in pilot.
- ✓ No missed IDs in pilot.
- ✓ Coalesce-rejection on the two candidates (WM-022/022a, WM-037/037a) is well-grounded per §2.3 three-AND test.
- ✓ Spec-version references (`v0.4.2`) match across spec front-matter, pilot prose, and yaml metadata.
- ⚠ Tally arithmetic mismatch (printed 71, actual 72) — MINOR, local lane.

---

## 6. Output to triage

**Total findings:** 1 (1 MINOR).
**Headline:** Coverage is 1:1 across every spec ID, invariant, schema, and error sentinel. The pilot is solidly built; only mechanical issue is a one-off arithmetic typo in the sanity tally that the pilot author can fix in place.

**Recommendation:** Pilot is APPROVED for r1 review on coverage grounds. The MINOR finding can be addressed during the patch pass; it does not block coverage acceptance.
