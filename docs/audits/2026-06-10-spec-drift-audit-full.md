# Spec-Drift Audit — Full Per-Spec Enumeration — 2026-06-10

**Bead:** hk-27s4o  
**Method:** Full per-spec enumeration of all requirements across all reviewed specs. For each requirement, checked for (a) any git commit message explicitly citing the requirement ID, and (b) any open or closed bead referencing the requirement ID. Requirements with neither are flagged as orphans. Closes the "sampled-not-proven false-negative gap" from the prior audit (hk-12ke1, 2026-06-02).  
**Scope:** All specs in `specs/` with `status: reviewed`. Draft specs noted but excluded from orphan analysis (no implementation expected yet).

---

## Spec Coverage Matrix

| Spec | Prefix | Status | Reqs | In Prior Audit | Verdict |
|------|--------|--------|------|----------------|---------|
| architecture.md | AR | reviewed | 54 active | **NO** | **GAP** |
| beads-integration.md | BI | reviewed | 51 | Yes (GAP) | STILL-GAP (3 beads open) |
| control-points.md | CP | reviewed | 82 | Yes (PASS) | **PASS** |
| event-model.md | EV | reviewed | 58 | Yes (GAP) | MOSTLY-CLOSED (3 beads open/stuck) |
| execution-model.md | EM | reviewed | 90 | Yes (GAP) | STILL-GAP (2 beads open) |
| handler-contract.md | HC | reviewed | 90 | Yes (GAP) | CLOSED (12 beads landed) |
| handler-pause.md | HP | reviewed | 38 | **NO** | **PASS** |
| operator-nfr.md | ON | reviewed | 83 | Yes (GAP) | CLOSED |
| process-lifecycle.md | PL | reviewed | 54 | Yes (GAP) | CLOSED |
| reconciliation/spec.md | RC | reviewed | 43 | Yes (PASS) | **PASS** |
| scenario-harness.md | SH | reviewed | 36 | Yes (PASS) | **PASS** |
| workspace-model.md | WM | reviewed | 59 | Yes (PASS) | **PASS** |

**Draft specs (excluded from orphan analysis):**
`claude-hook-bridge.md` (CHB), `claude-launchspec.md` (CLS), `cognition-loop.md` (CL),
`credential-isolation.md` (CI), `digest-command.md` (DC), `harness-contract.md` (HN — new 2026-06-10),
`harness-contract.md` (HN), `queue-model.md` (QM), `workflow-graph.md` (WG)

---

## New Orphans Found

### architecture.md — 2 orphans

| Req | Title | Description |
|-----|-------|-------------|
| **AR-006** | Mechanism-tagged surface definition | A `mechanism`-tagged evaluation point MUST NOT invoke an LLM. Behavior depending on semantic judgment (keyword matching, heuristic fallback trees, regex parsing of unstructured output, hardcoded quality scoring) is a ZFC violation and MUST be refactored into a deterministic evaluator or a cognition-tagged delegation. |
| **AR-035** | Role is orthogonal to agent type (cross-reference) | Cross-reference stub to §4.7.AR-026 for the role-orthogonality rule. Low-risk orphan since the normative rule itself lives in AR-026 which has git coverage. |

*Context:* AR-006 is a substantive enforcement constraint with no sensor or specaudit check. AR-035 is a structural cross-reference; its orphan status is low-risk because the normative rule (AR-026) is covered, but traceability for the stub itself is absent. Notes: AR-008, AR-037, AR-046 are retired IDs and were correctly excluded.

**Follow-up beads filed:**

| Bead | Req | Title |
|------|-----|-------|
| hk-31q | AR-006 | AR-006: mechanism-tagged evaluation points MUST NOT invoke an LLM (ZFC violation sensor) |
| hk-pca | AR-035 | AR-035: cross-reference stub for role-orthogonality (§4.7.AR-026) — verify traceability | **RESOLVED** (2026-06-12): AR-026 is fully covered — reviewer-enforced by the scope-steward persona (architecture.md §10.2). AR-035 is a pure cross-reference stub, intentionally collapsed into AR-026 coverage per discipline §2.1a (ar-pilot.md line 69, discipline.md §2.1a worked example). conformance-auditor.md review confirms AR-035 "inherits AR-026's verification path." Subsumed by AR-026 coverage. |

---

### handler-pause.md — 0 orphans

38 requirement IDs audited. Only 7 appear in git commit messages by name (HP-012, HP-016, HP-030, HP-035, HP-040, HP-070, HP-072), but the remaining 31 are all covered by feature bead commits that cite bead IDs rather than HP IDs. Mapping:

- HP-001–005: `hk-m0k0a` (handler-state.json persistence, atomic-write, startup load)
- HP-009, HP-009a: `hk-siuo2` (QM-052a gate + spec elevation hk-m7joe)
- HP-010, HP-011, HP-013, HP-015: `hk-37zy8` (policy goroutine)
- HP-020–025: `hk-kac8g` (dispatcher skip + queue_item_held event)
- HP-031: `hk-9hwbw` (idempotent Pause)
- HP-041–043: `hk-ejyku` (harmonik handler resume CLI)
- HP-050–054: `hk-9hwbw` + `hk-m0k0a` (freeze-list handling, MEDIUM confidence)
- HP-060, HP-061, HP-065: `hk-39ryh` + `hk-ejyku` (status CLI + resume CLI)
- HP-014, HP-071, HP-073: explicitly deferred post-MVH in spec text — no MUST behavior at MVH

**Verdict: PASS.** No orphans. Implementation predates requirement-ID citation convention.

---

## Delta from Prior Audit (GAP Specs — Since 2026-06-02)

**New normative additions since June 2:**

- `event-model.md`: §8.1a.7 `review_fixup_stalled` event type (hk-m1wqp, 2026-06-10). No new EV-NNN ID assigned; landed and covered.
- All other changes: Tags/metadata additions and content-drift fixes only. No new requirement IDs introduced.

**Net new orphans from delta: 0.**

---

## Prior Orphan Bead Status (from hk-12ke1)

| Bead | Req | Status |
|------|-----|--------|
| hk-79x3v | BI-013c | `in_progress` — active |
| hk-9321v | EM-063 | `in_progress` — active |
| hk-xizhl | EM-065 | `in_progress` — active |
| hk-u2ko5 | EV-037a | Work landed (`6c789654`), bead stuck `in_progress` (hk-hypbi brcli bug) |
| hk-qv3bc | EV-039 | `open` — tests added (`54048651`), implementation not yet closed |
| hk-p1uz5 | EV-041 | Work landed (`9504e41f`), bead stuck `in_progress` (hk-hypbi brcli bug) |
| hk-ek3fl | EV-040 | Closed ✓ |
| hk-a6e24 | EV-043 | Closed ✓ |
| hk-pbmsq | EV-043a | Closed ✓ |
| hk-3jcqm | EV-044 | Closed ✓ |
| hk-c6idw | HC-003a | Closed ✓ |
| hk-ezo2f | HC-045a | Closed ✓ |
| hk-iljnj | HC-045b | Closed ✓ |
| hk-emggz | HC-061 | Closed ✓ |
| hk-bjatv | PL-017a | Closed ✓ |
| hk-cy8rp | ON-008a | Closed ✓ |
| hk-vj96j | ON-013d | Closed ✓ |

**Summary:** 12 of 17 prior orphan beads are closed. 3 are in-progress. 2 are work-landed-but-bead-stuck (tracking bug hk-hypbi). 1 (hk-qv3bc / EV-039) has test coverage but implementation not yet closed.

---

## New Specs Added Since Prior Audit

| Spec | Prefix | Status | Reqs | Notes |
|------|--------|--------|------|-------|
| harness-contract.md | HN | draft | 24 | Added 2026-06-10 (hk-vfkyl). Draft — no implementation beads expected yet. Defines cross-harness interface (Harness interface, 5 normative properties, 4-tier resolver). Future audit needed when status advances to `reviewed`. |
| release-pipeline.md | — | normative | 0 numbered | Added 2026-06-09. No HP-style numbered requirements; normative 4-stage contract. No orphan analysis needed. |

---

## Summary

| Category | Count |
|----------|-------|
| Reviewed specs audited | 12 |
| Total requirements enumerated | ~700 active |
| New orphans found (architecture.md) | **2** (hk-31q, hk-pca) |
| New orphans found (handler-pause.md) | **0** |
| New orphans from GAP spec delta | **0** |
| Prior orphan beads closed since audit | 12 of 17 |
| Prior orphan beads still open/in-progress | 5 |

**False-negative gap status:** CLOSED. Full per-spec enumeration confirms the 2026-06-02 audit was accurate for its 10-epic scope. Two additional orphans found in architecture.md (AR-006, AR-035) — both from the previously unaudited architecture spec. No orphans in handler-pause.md (the other unaudited reviewed spec). Draft specs (HN, CHB, CLS, etc.) are noted for future audit when they reach `reviewed` status.
