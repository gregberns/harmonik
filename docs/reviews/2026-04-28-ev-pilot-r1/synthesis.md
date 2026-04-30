# EV Pilot Review (r1) — Synthesis

Date: 2026-04-28. Pilot under review: `docs/decompose-to-tasks/ev-pilot.md` v0.1.0. Discipline at time of review: `discipline.md` v0.7. Protocol: `pilot-review-protocol.md` v0.2.

Reviewers (parallel, completed):
- Coverage — `coverage-r1.md` (0/0/0).
- Decomposition-quality — `decomposition-r1.md` (3 BLOCKER / 1 MAJOR / 4 MINOR).
- Reference — `references-r1.md` (1 MAJOR class / 1 MAJOR local / 4 MINOR).

## 1. Outcome

**3 BLOCKER, 2 MAJOR, ~8 MINOR.** Most route to pilot-patch lane (mechanical fix-ups). One MAJOR routes to discipline-patch lane (§4.a envelope grandfather extension for same-day boundary).

### 1.1 Pilot-lane findings (apply at re-draft v0.1.0 → v0.1.1)

| Tag | Severity | Action |
|---|---|---|
| F-decomp-EV-3 | BLOCKER `local` | §5.7 cycle-walk identified 9 row-table patches; none applied to §2 row table. Apply all 9 patches. |
| F-decomp-EV-4 | BLOCKER `local` | Missed bidirectional cycle `ev-002c ↔ ev-events.daemon-degraded`. Resolve per F13 / F-pilot-AR-10. |
| F-decomp-EV-1 | BLOCKER `class` (override → `local`) | Sensor predecessors under-emit per §2.5 source #1. Discipline IS clear; pilot mis-applied. Override the `class` tag: this is application error, not discipline gap. Re-emit predecessors for all 4 of 6 affected sensors per the conformance-group ranges (e.g., `EV-009..EV-014b` = 9 reqs). |
| F-decomp-EV-2 | MAJOR `local` | `ev-027 → ar-019` wrong target. AR-020 owns the proposal procedure. Fix to `ev-027 → ar-020`. |
| F-refs-EV-3 | MINOR `local` | `ev-events.outcome-emitted → em-schema.outcome-status` target doesn't exist. Resolvable target is `em-schema.outcome` (or `em-schema.outcome-status` ENUM if that's the actual EM name — verify). |
| F-refs-EV-4 | MINOR `local` | §5.5 forward-cite count tally off. Recompute. |
| F-refs-EV-5 | MINOR `local` | CP forward-cite includes a "SC-10" stale-cite that resolves to AR-005, not CP. Net ~31, not ~32. |
| F-decomp-EV-5/6/7 | MINOR `local` | `ev-001` note misidentifies AR-018; `ev-008` notes claim a predecessor not in column; §5.5 forward-cite under-tallied. |

### 1.2 Discipline-lane findings (drive v0.7 → v0.8 patch)

| Tag | Severity | Lane | v0.8 patch |
|---|---|---|---|
| F-pilot-EV-4 / F-refs-EV-2 | MAJOR `class` | discipline | EV is post-AR-053 reviewed but same-day boundary. v0.7 grandfather carve-out names {EM, HC, CP, WM, PL, RC} — EV is NOT in this set. Two options: (D1) extend the grandfather set to include EV (same-day boundary case acknowledged); (D2) require EV to add §4.a envelope (spec edit). **Decision: D1** (extend grandfather to include EV) — preserves the spec author's deliberate review decision; same rationale as Option E for the others. The carve-out becomes "all pre-corpus-completion reviewed specs as of 2026-04-28: EM, HC, CP, WM, PL, RC, EV." Post-EV-reviewed specs (none in current corpus) will require §4.a. |
| F-refs-EV-6 | MINOR `class` | discipline | §2.5 F10 invariant-as-edge-target rule is silent on SENSOR→SENSOR (invariant→invariant). Pilot's `ev-inv-001 → em-inv-001` emission is defensible under "explicit ID cite" reading but not codified. v0.8 clarifies: explicit ID cite of another `<prefix>-INV-NNN` from an invariant body fires a sensor→sensor edge. |
| F-pilot-EV-1/2/7 | MINOR `class` | discipline | §2.11(c.1) clause for §6.3-payloads-co-located-with-§8-rows; `post-mvh` transient tag; §2.11(d.2) event-row dual-co-ownership cross-reference. **Defer to v0.8** — codify alongside the §4.a carve-out extension. |

## 2. Lane decision

Apply override criterion:

- **Would v0.8 change EV's bead set?** Yes for §4.a grandfather (no edits required to EV body; the carve-out absorbs the finding). **No** for F-refs-EV-6 (sensor→sensor edge already emitted). **No** for the §2.11(c.1) / `post-mvh` / §2.11(d.2) clauses (codification of pilot's defensible judgment calls). Net: bead set IS invariant under v0.8. The override criterion COULD apply if not for the BLOCKER local findings.
- The 3 BLOCKER findings (F-decomp-EV-3, F-decomp-EV-4, F-decomp-EV-1) are pilot-application errors and MUST be fixed regardless. So a re-draft is mandatory.

**Decision: patch discipline v0.7 → v0.8 first, then re-draft EV against v0.8.** The v0.8 patch is documentation-tightening; the EV re-draft applies pilot-lane fixes against the new discipline. Future pilots (HC/CP/WM/PL/ON/RC) draft against v0.8.

## 3. Sequence

1. **Patch `discipline.md` v0.7 → v0.8** with:
   - §3.2 §4.a grandfather carve-out extended to include EV (same-day boundary).
   - §2.5 F10 sensor→sensor explicit-ID-cite clarification.
   - §2.11(c.1) §6.3-payload-co-located-with-§8-row clause (codifies F-pilot-EV-1).
   - `post-mvh` transient tag definition (analog of `gated-by-spec-edit`).
   - §2.11(d.2) cross-reference between event-row dual-ownership and (d.1) registry-row dual-ownership.
2. **Re-draft `ev-pilot.md` v0.1.0 → v0.1.1** against discipline v0.8. Apply pilot-lane fixes:
   - 9 §5.7 row-table patches.
   - `ev-002c ↔ ev-events.daemon-degraded` cycle resolution.
   - 4-of-6 sensor predecessor expansions per §2.5 source #1.
   - `ev-027 → ar-020` fix (was `ar-019`).
   - `ev-events.outcome-emitted → em-schema.outcome` target name fix.
   - §5.5 forward-cite count corrections.
   - Other minor row-note fixes.
3. **r2 reviewers** against v0.1.1.
4. **r2 synthesis + load gate.**
5. **Load EV beads** under prefix `hk` to existing `.beads/`.

## 4. Why this is the right call

The EV pilot's underlying decomposition is sound — the BLOCKERs are all "identified but not applied" (cycle patches) or "mechanical rule under-applied" (sensor predecessors). No structural re-thinking. The §4.a grandfather extension is the only genuinely-new discipline patch; the rest of v0.8 is small clauses codifying judgment calls already defensibly made.

Override criterion DOESN'T apply (BLOCKERs require re-draft anyway), but the v0.8 patch is small enough that the discipline-first / re-draft-against-new-version order is cheap.
