# AR Pilot Reference Review (r2) — 2026-04-27

Reviewer: Reference reviewer per `pilot-review-protocol.md` v0.2 §3.3.
Source spec: `specs/architecture.md` v0.3.1 (`reviewed`).
Pilot draft: `docs/decompose-to-tasks/ar-pilot.md` v0.2.1 (re-drafted from v0.2.0 to close F-pilot-AR-9).
Discipline: `docs/decompose-to-tasks/discipline.md` v0.5.
Prior review: `docs/reviews/2026-04-27-ar-pilot-r1/references-r1.md` (clean — 0/0/0; 3 advisory class observations later codified into discipline v0.5).

## Summary

AR's front-matter declares `depends-on: []` (line 16, unchanged), so the universe of admissible cross-spec edge targets is empty per discipline §3.2 and the pilot is required to emit zero outbound cross-spec edges. Pilot v0.2.1 §5 declares "Result: ZERO outbound cross-spec edges from AR" with seven enumerated rationales and a `depends-on` validation note, and the §2 / §3 / §4 / §6 tables are consistent with that declaration — every "blocks edges" entry is intra-spec or `(none)`.

Walked AR's body top-to-bottom. The cross-document cite landscape is unchanged from r1 (AR's content has not been edited between v0.3.1 and now; only the pilot doc was re-drafted). The same ~50 cross-document cites appear, all classified non-edge-generating. The three advisory class observations from r1 are now codified into discipline v0.5 (§3.1 no-edge-list expansion), which mechanizes the previously-judgment-based handling of (a) §10 conformance prose, (b) `docs/components/internal/` doc cites, and (c) self-cite illustrative examples.

The new term-use rule (discipline v0.5 §3.1 step 5) does not produce any cross-spec edges for AR because AR is the foundation root: every defined term that AR mentions whose definition is owned by another *spec* is, currently, defined in `docs/foundation/components.md` (a bootstrap stub, not a spec ID). Per the same step-5 rule, this is correctly non-edge-generating today. (Migration debt is already tracked as F-pilot-AR-2 carry-forward TODO.)

The F13 slot-rule resolution (AR-053 / AR-013 / AR-052) is intra-spec only and does not introduce any cross-spec edge.

**Result: zero BLOCKER, zero MAJOR, zero MINOR findings. Zero `class` observations** — the three r1 `class` advisories are closed by discipline v0.5; r2 has no new class advisories to surface.

## Delta from r1 (focus areas requested for r2)

### 1. Discipline v0.5 §3.1 expanded no-edge list — pilot's behavior consistent?

The three previously-advisory classes are now explicit non-edge entries in discipline §3.1:

| Class | r1 status | v0.5 status | AR pilot v0.2.1 behavior |
|---|---|---|---|
| §10 conformance / test-surface obligations prose | r1 advisory observation 1 (sole real spec-ID cite at line 512: `control-points.md §6.6`) | Codified as F-pilot-AR-AO1 → §3.1 no-edge bullet | **Consistent.** Pilot §5 emits no edge for this cite; pilot's rationale list does not enumerate it explicitly, but the empty-set declaration plus the new §3.1 explicit no-edge entry makes the case mechanical. PASS. |
| `docs/components/internal/` doc cites | r1 advisory observation 3 (`build-practices.md` cites at lines 224, 425, 504) | Codified as F-pilot-AR-AO3 → §3.1 no-edge bullet | **Consistent.** Pilot §5 emits no edge; the three sites (AR-021 delegation path role naming, AR-INV-001 sensor persona, §10.2 INFORMATIVE block) all remain non-edge under the now-explicit rule. PASS. |
| Self-cite illustrative examples | r1 advisory observation 2 (AR-023 line 237: `[architecture.md §4.1]`, `[architecture.md AR-016]`) | Codified as F-pilot-AR-AO2 → §3.1 no-edge bullet | **Consistent.** Pilot §5 does not emit an edge for these `touches:`-shape illustrative examples. Pilot's rationale list does not enumerate them explicitly, but the v0.5 rule makes the case mechanical. PASS. |

Verified by exhaustive walk: pilot v0.2.1 emits zero edges for any of these three classes, exactly as v0.5's expanded no-edge list now requires. No reclassification needed.

### 2. `depends-on: []` invariant — still holds?

**PASS.** Verified at `specs/architecture.md` line 16: `depends-on: []`. Unchanged from r1. Pilot v0.2.1 §1 ("Front-matter `depends-on`: `[]` (empty). AR is the root of the foundation corpus") and §5 ("**Result: ZERO outbound cross-spec edges from AR**") still match.

### 3. Term-use cross-spec walk (new rule per discipline v0.5 §3.1 step 5)

Discipline v0.5 §3.1 step 5 introduces term-use edges: when a requirement uses (without explicit cite) a defined term whose definition is owned by another req in the same spec — or single-owner cross-spec — emit a `blocks` edge to the defining req's bead.

Pilot v0.2.1 walked term-use intra-AR and emitted **9 new term-use edges** (delta vs v0.1.0): `ar-052 → ar-016`, `ar-004 → ar-013`, `ar-014 → ar-005`, `ar-017 → ar-013`, `ar-019 → ar-013`, `ar-028 → ar-016`, `ar-029 → ar-032`, `ar-030 → ar-005`, `ar-031 → ar-029`. All intra-spec.

**Cross-spec term-use check:** I scanned for terms used in AR whose definitions might be owned by another *finalized* spec under `specs/`. Result: **none.** The candidate terms I checked:

- `local-patchback` / `architectural-rollback` / `policy-rollback` / `context-restore` (AR-010 line 152) — definition is currently in `docs/foundation/components.md §2`, NOT in any spec under `specs/`. The bootstrap stub locus means the term-use edge is non-edge per the step-5 rule (definition is not owned by a single spec).
- `freedom-profile` (AR-010 line 152, AR-027 line 263, §6 defer table line 454, §9.3 line 485) — same: defined in `docs/foundation/components.md §6`, migrating to control-points.md when finalized.
- `LaunchSpec.agent_type` (AR-027 line 263) — defined in `docs/foundation/components.md §4`, migrating to handler-contract.md when finalized.
- `agent_started` (AR-027 line 263) — defined in `docs/foundation/components.md §3`, migrating to event-model.md when finalized.
- `node-type` enum / `actor_role=Reviewer` (AR-011, AR-029, AR-030) — `node-type` enum lives in `docs/foundation/components.md §2`; `Reviewer` is defined intra-AR (AR-032), already covered by the intra-spec term-use edges.
- `Outcome` shape (AR-030 outcome status) — `docs/foundation/components.md §2`, migrating to execution-model.md.
- `Transition` record (AR-012 trace shape) — `docs/foundation/components.md §2`.

Every term-use whose definition is "owned by another single-owner spec" resolves to a `docs/foundation/components.md` stub today, not a spec under `specs/`. Per discipline §3.1 (only `[<other-spec-id>.md §N]` cites generate edges) and step 5 (same restriction implied by "single-owner spec"), zero cross-spec term-use edges fire today.

**`depends-on: []` consistency check:** if AR were using a term defined in another spec right now, that would be a `depends-on` violation per discipline §3.2 (the term-defining spec would have to be in AR's `depends-on`). Since no such cross-spec term-use exists, `depends-on: []` is consistent. PASS.

**Carry-forward implication (informational, not a finding).** When EM / HC / CP / EV / PL / RC specs finalize and AR migrates the bootstrap-stub cites, AR's `depends-on` will need to grow. AR's foundation-root status will end at that point — AR will become a foundation-root that depends on its sibling foundation specs for term ownership. This is exactly the F-pilot-AR-2 carry-forward TODO already tracked in pilot v0.2.1 §8 and is not a discipline gap.

### 4. F13 slot-rule resolution — accidentally introduce a cross-spec edge?

**No.** F13 (discipline v0.5 §2.7) was applied to the AR-013 / AR-052 / AR-053 envelope-slot trio. All three IDs are intra-AR. Pilot v0.2.1 emits `ar-053 → {ar-013, ar-052}` (slot-rule → content) and `ar-052 → ar-016` (the non-bidirectional half) as intra-spec edges; reverse cites `ar-013 → ar-053` and `ar-052 → ar-053` are reclassified informational with no edge emitted. Zero edges introduced cross-spec. PASS.

### 5. AR-011 ↔ AR-029 cycle (F-pilot-AR-9, closed in v0.2.1)

**Verified closed.** v0.2.1's patch dropped the invented edge `ar-011 → ar-029`. Spot-checked AR-011's body (lines 156–160 of `specs/architecture.md`):

> Verification MUST be realized as a role-function of a workflow node (assigned to an `agentic` node with `actor_role=Reviewer` or a `non-agentic` node running a deterministic checker), NOT as a distinct member of the node-type enum and NOT as a subsystem. No subsystem in harmonik MAY be named "verifier" at the subsystem level. A verification-capable node's inputs, outputs, and emission obligations are: inputs cite what is being verified (a prior state, a work product reference, evidence paths); outputs conform to the verification-result shape (see §4.7.AR-030); completion MUST emit an event naming the verification outcome (event type declared in [docs/foundation/components.md §3]).

AR-011 inline-cites AR-030 only. No reference to AR-029. Pilot v0.2.1's edge set `ar-011 → {ar-030, ar-032}` is correct. (AR-032 is the term-use edge for the role name "Reviewer".) No cross-spec leakage. PASS.

This is intra-spec, so it's not a cross-spec reference review concern, but I include it because the prompt specifically asked to verify F13 and the pilot's edge graph after the v0.2.1 fix.

## Inline-cite enumeration (carried forward from r1; verified unchanged)

The full table of ~50 cross-document cites at lines 45–622 of `specs/architecture.md` is unchanged from r1's enumeration. Spot-checked five high-density rows:

| Line | Citing site | Cite | Edge? | r1 verdict | r2 verdict |
|---|---|---|---|---|---|
| 152 | AR-010 (§4.3) | `[docs/foundation/components.md §2]` ×2, `§6` | No | Bootstrap stub | Same. PASS. |
| 224 | AR-021 (§4.6) | `build-practices.md §Agent review` | No | Internal-component doc | Same; now explicitly enumerated in v0.5 §3.1 no-edge list. PASS. |
| 237 | AR-023 (§4.6) | `[architecture.md §4.1]`, `[architecture.md AR-016]` | No | Self-cite illustrative example | Same; now explicitly enumerated in v0.5 §3.1 no-edge list. PASS. |
| 263 | AR-027 (§4.7) | `[docs/foundation/components.md §6/§2/§4/§3]` | No | Bootstrap stub | Same. PASS. |
| 512 | §10.2 | `control-points.md §6.6` | No | r1 advisory class | Same; now explicitly enumerated in v0.5 §3.1 no-edge list (§10 conformance prose). PASS. |

For the full enumeration of all ~50 cites, see `references-r1.md`. The cite landscape did not change between v0.3.1 (the source-spec version reviewed at r1) and the same v0.3.1 reviewed here at r2.

## Missed edges

**None.** With `depends-on: []` plus zero inline cites to other normative spec IDs in AR's normative §4 / §5 / §6 prose, no edge can be missed. Verified by exhaustive walk and grep for cross-spec ID patterns (`BI-`, `EM-`, `EV-`, `HC-`, `RC-`, `ON-`, `PL-`, `CP-`, `WM-`):

```
$ grep -nE '\b(BI|EM|EV|HC|RC|ON|PL|CP|WM)-[A-Z0-9]+-?[0-9]*\b' specs/architecture.md
```

Returns matches only for: (a) the `<PREFIX>-ENV-NNN` envelope-section ID *examples* in AR-053 (line 86: `EM-ENV-001, HC-ENV-001` — illustrative, not cross-spec dependency cites); (b) the AR-MIG-001 informative migration block at line 267 (`HC-008` inside `> INFORMATIVE:`, not an edge); (c) prose mentions of "execution-model EM-006" inside §12 revision history at line 580 (revision history, non-edge per discipline §3.1). None of these is an inline edge-generating cite. PASS.

## Invented edges

**None.** Pilot v0.2.1's §5 declaration is the empty set ("Result: ZERO outbound cross-spec edges from AR"). Zero entries in pilot §2 / §3 / §4 / §6 tables reference any other spec's mnemonic prefix. Spot-checked the "blocks edges" column in pilot §2 — every entry is `(none)`, an intra-AR mnemonic (`ar-NNN`), or the schema-bead mnemonic `ar-schema.agent-type-identifier`. No `bi-*`, `em-*`, `ev-*`, `cp-*`, `hc-*`, `on-*`, `pl-*`, `rc-*`, `wm-*` references. PASS.

## Depends-on violations

**None.** With `depends-on: []`, any cross-spec edge would be a violation; the pilot emits zero edges, so zero violations. Pilot §5 rationale-1 explicitly cites the empty `depends-on` (line 16) and discipline §3.2 as the basis. Correct application.

The cross-spec term-use walk above also surfaces no implicit `depends-on` violation: every term AR uses whose definition lives in another *spec* still lives in `docs/foundation/components.md` (not a spec under `specs/`), so `depends-on: []` remains consistent.

## Wide-fanout tag check

**Not applicable.** Zero section-anchor cites resolve to spec IDs (every section-anchor cite in AR points at `docs/foundation/components.md`, not a spec). Pilot has no `cite:wide-fanout` tags and is not required to have any.

## Bidirectional cycles

**None at the cross-spec level.** AR has no outbound cite to any other normative spec in foundation prose. The reverse direction (every other foundation spec depending on AR) is unilateral by design — AR is the root of the corpus per §9.1 line 474.

**Intra-spec bidirectional resolution (informational):** F13 was correctly applied to the AR-013 / AR-052 / AR-053 envelope-slot trio; F-pilot-AR-9 (AR-011 ↔ AR-029) was correctly closed in v0.2.1 by dropping the invented edge `ar-011 → ar-029`. Both resolutions are intra-spec.

## Findings list

**No BLOCKER findings.**

**No MAJOR findings.**

**No MINOR findings.**

**No `class` observations.** (The three r1 class advisories — §10 conformance prose, internal-component-doc cites, self-cite illustrative examples — are now codified in discipline v0.5 §3.1's expanded no-edge list. r2 has no new class observations to surface.)

## Lane assignments

No findings, no observations. Nothing to route to discipline-patch lane or pilot-edit lane.

## Conclusion

Pilot v0.2.1 is REFERENCE-CLEAN. The empty-set claim "Result: ZERO outbound cross-spec edges from AR" is verified by exhaustive walk plus the new term-use rule check; AR's `depends-on: []` is consistent with this empty-edge result; F13 slot-rule resolution introduced no cross-spec edges; v0.2.1's F-pilot-AR-9 fix correctly removed the invented `ar-011 → ar-029` edge. Discipline v0.5's expanded no-edge list (§3.1) mechanizes what r1 had to handle as advisory observations, and pilot v0.2.1's behavior is consistent with the codified rules. No patches required for the references dimension of the review.
