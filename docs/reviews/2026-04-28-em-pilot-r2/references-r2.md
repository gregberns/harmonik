# EM Pilot Reference Review (r2) — 2026-04-28

Reviewer: Reference reviewer per `pilot-review-protocol.md` v0.2 §3.3 (extended for v0.7).
Source spec: `specs/execution-model.md` v0.3.3 (`reviewed`).
Pilot draft: `docs/decompose-to-tasks/em-pilot.md` v0.2.0.
Discipline: `docs/decompose-to-tasks/discipline.md` v0.7.
Companion: `docs/reviews/2026-04-27-em-pilot-r1/references-r1.md` (prior round; baseline for delta verification).

## Summary

This is the targeted r2 spot-verification of the v0.2.0 re-draft against v0.7 discipline rules. Per protocol §3.3 + r1 baseline, I focused on the v0.2.0-affected sites and edges:

1. **§5 emitted-edge tally rewritten 4 → 6 (pilot-lane fix F-em-r1-MIN-4 + behavioral add F-em-r1-MIN-10).** All 6 edges enumerated in §5 narrative; cross-checked against §2/§4 row tables.
2. **`em-046 → ar-032` is NEW (F-em-r1-MIN-10).** Direct term-use edge for `actor_role` + named role values.
3. **`em-inv-005` predecessors are NEW intra-EM (F-em-r1-MAJ-1 invariant-body term-use sub-clause).** `em-schema.checkpoint-trailers` (term-use of `Harmonik-Bead-ID`) and `em-014` (term-use of bead-tied-runs concept).
4. **`em-inv-001` predecessors gained `em-016`, `em-017`** (optional v0.7 §3.1 step 5 invariant-body term-use direct edges; "git checkpoint trail" defined by EM-016/EM-017).
5. **`em-040` row note rewritten** to lead with F-pilot-AR-r2-2 invariant-as-target precedence (v0.7 §3.1 step 5 precedence rule).
6. **`em-005a` row note tightened** on forward-extension cite mechanics.

I walked the v0.2.0-affected sites in source spec normative prose and verified each edge claim against term-ownership rules. I also re-applied the v0.7 mechanical rules to the 5 already-validated edges (em-011×2, em-schema.node×2, em-schema.transition→ar-032) and the 3 supporting-cite reclassifications (em-001, em-005a, em-040). I cross-checked the forward-cite log (F-pilot-EM-2) and the §3.2 grandfather carve-out (F-pilot-EM-3).

**Result: 0 BLOCKER, 0 MAJOR, 0 MINOR.** The pilot is reference-clean under v0.7 discipline. All v0.2.0 changes are mechanically derivable from term-use precision + invariant-body term-use sub-clause + invariant-as-target precedence. No invented edges, no missed edges within the `depends-on=[architecture]` constraint, no depends-on violations, no bidirectional-cite cycles unresolved.

## Method

Per protocol §3.3 spot-verification mode (r1's full body walk is the baseline; r2 verifies only v0.2.0 deltas):

1. Re-verify the 5 already-validated edges hold under v0.7 (no rule changed them).
2. Verify the 1 new emitted edge (`em-046 → ar-032`) matches an actual term-use in EM-046 body.
3. Verify the 2 new `em-inv-005` predecessor edges (`em-schema.checkpoint-trailers`, `em-014`) match actual term-uses in EM-INV-005 body per v0.7 §3.1 step 5 invariant-body sub-clause.
4. Verify the 2 new `em-inv-001` direct predecessor edges (`em-016`, `em-017`) match actual term-uses in EM-INV-001 body per same sub-clause.
5. Verify §5 narrative enumeration matches §2/§3/§4 row tables.
6. Verify the 3 supporting-cite reclassifications still hold under v0.7 (em-001, em-005a, em-040; em-040 rationale prioritization is documentation-only).
7. Verify the forward-cite log (F-pilot-EM-2) still defers correctly (Option B chosen at synthesis; depends-on unchanged).
8. Verify §3.2 §4.a envelope grandfather carve-out for F-pilot-EM-3.
9. Verify no new bidirectional cycles introduced; existing 3 still resolved correctly.
10. Verify sensor↔impl one-way (§2.5 F12) holds for the new edges.

## Spot-verify table (v0.2.0 changes only)

### v0.2.0 emitted edges to AR — verification

| # | Pilot edge | Source-spec cite (line:text) | v0.7 rule | Verdict |
|---|---|---|---|---|
| 1 | `em-011 → ar-001` | EM-011 (line 162): "every node MUST carry the four-axis tags ... per [architecture.md §4.1, §4.2]" | §3.1 step 5 term-use; AR-001 owns four-axis classification rule per AR §4.1 (line 92, "Four-axis classification applies to every cross-subsystem surface") | VALID. Term-use precision pins to AR-001 (not the wider §4.1 fan-out). UNCHANGED from r1. |
| 2 | `em-011 → ar-005` | EM-011 (line 162): "and the `mechanism \| cognition` tag per [architecture.md §4.1, §4.2]" | §3.1 step 5 term-use; AR-005 owns mechanism/cognition tag per AR §4.2 (line 120, "Every evaluation point is tagged mechanism or cognition") | VALID. Term-use precision pins to AR-005. UNCHANGED from r1. |
| 3 | `em-schema.node → ar-001` | §6.1 line 610 defer block: "AxisTags, ModeTag — defined in [architecture.md §4.1, §4.2]"; line 639: `axes : AxisTags -- four-axis classification per [architecture.md §4.1]` | §3.1 step 5 term-use of `AxisTags` type; AR-001 owns four-axis declaration | VALID. UNCHANGED from r1. |
| 4 | `em-schema.node → ar-005` | §6.1 line 640: `mode_tag : ModeTag` + line 610 defer block "AxisTags, ModeTag — defined in [architecture.md §4.1, §4.2]" | §3.1 step 5 term-use of `ModeTag` type; AR-005 owns mechanism/cognition tag | VALID. UNCHANGED from r1; now correctly enumerated in §5 narrative (was missing in v0.1.0 §5 narrative — F-em-r1-MIN-4 fix verified). |
| 5 | `em-schema.transition → ar-032` | §6.1 line 700: `actor_role : String -- role name per [architecture.md §4.8]; {daemon, reconciliation} for synthesized outcomes per §4.10.EM-046` | §3.1 step 5 term-use of seven-role vocabulary; AR-032 owns roles per AR §4.8 (line 295, "Seven roles named") | VALID. Term-use precision pins to AR-032 (not AR-033..AR-036 in same section). UNCHANGED from r1. |
| 6 | `em-046 → ar-032` | EM-046 body (line 552): "the `Outcome` associated with a context-restore transition is synthesized by the daemon with `status = SUCCESS` and an `actor_role` of `daemon` or the role of the verdict-executing subsystem" | §3.1 step 5 term-use of `actor_role` + named role values; AR-032 owns the seven-role vocabulary | **NEW v0.2.0 (F-em-r1-MIN-10).** VALID. EM-046 body explicitly term-uses `actor_role` and names role values from AR-032's seven-role vocabulary ("daemon" + "role of the verdict-executing subsystem"). Direct edge is preferred for clarity over transitive coverage via `em-schema.transition → ar-032` per F-em-r1-MIN-10 (explicit-is-better-than-transitive at corpus scale). One-way per §2.5 F12 — no inverse edge from ar-032 to em-046 (cross-spec; AR is upstream). |

**§5 narrative ↔ §2/§4 row table cross-check.**

§5 narrative enumerates 6 edges (lines 168-178 of pilot):
- `em-011 → ar-001` — confirmed in em-011 row (line 53): `ar-001` listed.
- `em-011 → ar-005` — confirmed in em-011 row: `ar-005` listed.
- `em-schema.node → ar-001` — confirmed in em-schema.node row (line 141): `ar-001` listed.
- `em-schema.node → ar-005` — confirmed in em-schema.node row: `ar-005` listed.
- `em-schema.transition → ar-032` — confirmed in em-schema.transition row (line 145): `ar-032` listed.
- `em-046 → ar-032` — confirmed in em-046 row (line 102): `ar-032` listed.

**Tally: §5 says "Total emitted cross-spec edges to AR: 6"; §2/§4 row tables enumerate 6.** F-em-r1-MIN-4 (off-by-one tally) closed; F-em-r1-MIN-10 (em-046 direct edge) materialized.

### v0.2.0 invariant-body term-use edges — verification (F-em-r1-MAJ-1)

Per discipline v0.7 §3.1 step 5 invariant-body term-use sub-clause:

> When an `<prefix>-INV-NNN` invariant's BODY (the §5 invariant prose, with or without inline cite) uses a defined term whose definition is owned by an in-spec or in-`depends-on` requirement, schema, or trailer-registry row, emit a `blocks` edge FROM the invariant's sensor bead TO the defining bead.

| Sensor bead | New predecessor | Term used in invariant body | Defining bead | Verdict |
|---|---|---|---|---|
| `em-inv-005` | `em-schema.checkpoint-trailers` | EM-INV-005 (line 584): "Beads reports a bead as `closed` but no merge commit with `Harmonik-Bead-ID` matching that bead exists in the project's git history" — the term `Harmonik-Bead-ID` is a trailer key | `em-schema.checkpoint-trailers` (the §6.2 trailer registry, which owns the 7-key trailer set including `Harmonik-Bead-ID`) | **NEW v0.2.0.** VALID. `Harmonik-Bead-ID` is registered as a row in EM §6.2 (line 770). Per v0.7 §2.5 source (4) + §3.1 step 5 invariant-body sub-clause, the trailer-registry row's owning bead is the predecessor. One-way per §2.5 F12. |
| `em-inv-005` | `em-014` | EM-INV-005 (line 584): "no merge commit with `Harmonik-Bead-ID` matching that bead exists in the project's git history" — the bead-tied-runs concept (the bead↔run linkage that anchors `Harmonik-Bead-ID` semantics) | `em-014` (Bead-to-run relationship is many-runs-per-bead — the canonical owner of the bead↔run concept) | **NEW v0.2.0.** VALID. EM-014 (line 180-182) owns the bead↔run linkage rule and the `bead_id` field on `Run`; the invariant's "merge commit with `Harmonik-Bead-ID` matching that bead" body language depends on this linkage's semantic. Per v0.7 §3.1 step 5 invariant-body sub-clause, em-014 is a direct sensor predecessor. One-way per §2.5 F12. |
| `em-inv-001` | `em-016` | EM-INV-001 (line 572): "the git checkpoint trail MUST be sufficient ... to reconstruct any run's current durable state" — the term "git checkpoint trail" is anchored by EM-016's checkpoint definition (commit + sibling-file pair) | `em-016` (Checkpoint is a git commit whose tree carries the work product and the transition record — the canonical owner of "checkpoint" as a git commit) | **NEW v0.2.0 (optional per synthesis §1.3).** VALID. EM-016 (line 214-216) owns the checkpoint definition; the "git checkpoint trail" term-use depends on this rule's existence. Per v0.7 §3.1 step 5 invariant-body sub-clause, the explicit edge is admissible (transitive coverage via em-031..em-033 also exists). One-way per §2.5 F12. The pilot's "explicit-is-better-than-transitive at corpus scale" rationale is sound (per F-em-r1-MIN-10 paralleling logic). |
| `em-inv-001` | `em-017` | EM-INV-001 (line 572): "the git checkpoint trail" — anchored also by EM-017's trailer registry (which provides the `Harmonik-Run-ID` index that makes the trail walkable) | `em-017` (Checkpoint commit carries structured trailers — the trailer registry that anchors the trail) | **NEW v0.2.0 (optional per synthesis §1.3).** VALID. EM-017 (line 225-230) declares the trailer set that makes the checkpoint trail discoverable; the "git checkpoint trail" body term is anchored jointly by EM-016 (the commits) and EM-017 (the trailers indexing them). Per v0.7 §3.1 step 5 invariant-body sub-clause, the explicit edge is admissible. One-way per §2.5 F12. |

**Behavioral consequence.** `em-inv-005`'s blocks-list grew from "(none — see notes)" in v0.1.0 to `em-schema.checkpoint-trailers, em-014` in v0.2.0. `em-inv-001`'s blocks-list grew from `em-031, em-031a, em-032, em-033` to `em-016, em-017, em-031, em-031a, em-032, em-033`. Both are mechanical applications of the v0.7 §3.1 step 5 invariant-body sub-clause + §2.5 source (4).

**`gated-by-corpus-scale` tag.** `em-inv-005` carries the transient tag (per v0.7 §2.5 sensor-predecessor degeneracy) because the invariant body STILL has out-of-`depends-on` cross-spec inline cites to `[reconciliation/spec.md §8.4 Cat 3]` and `[beads-integration.md §4.7 BI-022]` (forward-deferred). The tag drops at edge-fire time when RC and BI pilots resolve their reciprocal cites. Verified consistent with the v0.7 §2.5 + §3.1.3 tag-policy framing. `em-inv-001` does NOT carry the tag (its body has no out-of-`depends-on` cross-spec inline cites — only intra-EM and the AR-implied terms covered by transitive coverage).

### Sensor↔impl one-way check (§2.5 F12)

For the new sensor-predecessor edges to em-inv-001 and em-inv-005, verify no impl bead blocks-on the sensor (which would be wrong-direction):

- `em-016` row (line 61) blocks-on: `em-018`, `em-schema.checkpoint`. No `em-inv-001` in blocks. ✓
- `em-017` row (line 62) blocks-on: `em-016`, `em-018`, `em-schema.checkpoint-trailers`. No `em-inv-001` in blocks. ✓
- `em-014` row (line 56) blocks-on: `em-schema.run`, `em-schema.bead-id`. No `em-inv-005` in blocks. ✓
- `em-schema.checkpoint-trailers` row (line 153) blocks-on: `(none)`. ✓

All sensor↔impl edges remain one-way. No cycles introduced by the new invariant-body term-use edges.

### Supporting-cite reclassifications under v0.7

Per discipline v0.7 §3.1 step 1 (F-pilot-AR-10 supporting-cite test) + §3.1 step 5 precedence rule (F-em-r1-MAJ-4: invariant-as-target exemption beats supporting-cite when both could apply):

| Reclassification | v0.7 rule | Pilot rationale (v0.2.0) | Verdict |
|---|---|---|---|
| EM-001 → AR §4.10 (no edge) | F-pilot-AR-10 supporting-cite test | "EM-001's claim is independently testable with the AR-§4.10 reference removed" (em-001 row notes, line 43) | VALID. Operational test: remove AR §4.10. EM-001 still claims "On-disk representation is a DOT document" as an EM-local rule. Independently testable. UNCHANGED from r1. |
| EM-005a → AR §4.6 (no edge) | F-pilot-AR-10 supporting-cite test (TIGHTENED v0.2.0) | "the AR §4.6 cite attaches to a forward-extension statement ('future variants extend the enum via the amendment protocol') that does not affect MVH-level testability of EM-005a's current discriminator-and-payload shape" (em-005 row notes, line 47) | VALID. The tightened rationale clarifies that the cite attaches to a sub-clause about future extensions; main MVH-level claim is independently testable. F-em-r1-MIN-4 / synthesis §1.3 documentation-tightening applied; rationale improved over v0.1.0. |
| EM-040 → AR §4.9 (no edge) | F-pilot-AR-r2-2 invariant-as-target exemption (PRIMARY) + F-pilot-AR-10 supporting-cite test (parallel) | "AR §4.9 houses AR-INV-007 (centralized-controller invariant promoted from AR-037). Per discipline v0.7 §3.1 step 5 precedence rule, invariant-as-target exemption (F-pilot-AR-r2-2) takes precedence over supporting-cite analysis (F-pilot-AR-10): no edge fires because emitting `em-040 → ar-inv-007` would reverse the §2.5 F12 sensor↔impl one-way rule" (em-040 row notes, line 95) | VALID. Verified: AR §4.9 contains AR-INV-007 (line 435 of architecture.md, `AR-INV-007 — Centralized-controller invariant`). Per v0.7 §3.1 step 5 precedence rule (F-em-r1-MAJ-4), invariant-as-target exemption applies as the primary structural reason. The supporting-cite analysis is parallel and concurs. The pilot's v0.2.0 rationale-lead-rewrite (precedence rule first, supporting-cite parallel) is correct. F-em-r1-MAJ-4 / synthesis §1.3 documentation-tightening applied. |

All 3 reclassifications hold under v0.7. The em-040 rationale-lead rewrite is documentation-only (the no-edge outcome is invariant under either rationale); the v0.2.0 rationale ordering correctly prioritizes the structural rule (invariant-as-target) over the heuristic (supporting-cite).

### Forward-cite log (F-pilot-EM-2) deferral check

§3.2 validation: every emitted cross-spec edge target MUST appear in EM's `depends-on: [architecture]`. Six emitted edges all target AR; no depends-on violations.

Forward-cite log (pilot §5 forward-cite findings table, lines 192-202): ~50+ inline cites in normative §4/§5/§6 prose to non-`depends-on` specs (event-model, handler-contract, control-points, reconciliation, workspace-model, beads-integration, operator-nfr, process-lifecycle). Per synthesis §2.1 Option B decision: depends-on is unchanged; reciprocal-direction edges from sibling pilots will materialize most deferred deps at corpus scale. Pilot does NOT emit edges to non-AR targets (which would violate §3.2). VERIFIED.

The forward-deferred wide-fanout cites (EM-025→EV §8 ~14 events; EM-033→RC §8 ~12 categories; EM-INV-004→RC §8) do NOT pre-emit `cite:wide-fanout` placeholders per v0.7 §3.1.3 forward-deferred wide-fanout tag policy (tag fires at edge-fire time, not at deferral time). VERIFIED.

### `§4.a` envelope grandfather carve-out (F-pilot-EM-3)

Per v0.7 §3.2 §4.a envelope grandfather carve-out (Option E): EM is grandfathered alongside HC/CP/WM/PL/RC because EM was `reviewed` before AR-053 landed. No spec edit; no pilot patch. Pilot §8 F-pilot-EM-3 closes as RESOLVED v0.7 (line 302). VERIFIED.

### Bidirectional-cite cycles

Pilot §5 (lines 211-226) lists 3 intra-EM 2-cycles resolved via discipline §2.7 F13 + F-pilot-AR-10:

| 2-cycle | Resolution | v0.7 rule | Verdict |
|---|---|---|---|
| em-002 ↔ em-041 | Emit `em-041 → em-002`; reclassify reverse as informational | F13 cascade↔shape pattern (slot-rule first worked example) | VALID. UNCHANGED from r1. |
| em-018 ↔ em-019 | Emit `em-018 → em-019`; reclassify reverse as informational | F13 declaration-rule ↔ retrieval-method pattern (v0.7 NEW worked example, F-em-r1-MIN-5) | VALID. v0.7 §2.7 grew the second worked example codifying this sub-pattern; the pilot's chosen direction is now mechanically supported by the discipline. UNCHANGED from r1. |
| em-020 ↔ em-020a | Emit `em-020a → em-020`; reclassify reverse as supporting | F-pilot-AR-10 supporting-cite test (no slot rule applies) | VALID. UNCHANGED from r1. |

No new bidirectional structures introduced by v0.2.0's new edges. The new sensor-body term-use edges (em-inv-001 → em-016/em-017; em-inv-005 → em-schema.checkpoint-trailers/em-014) are one-way per §2.5 F12; their target beads do not edge back to the sensors.

## Findings

**0 BLOCKER** — no invented edges; all 6 emitted edges trace to actual term-uses in source spec normative prose.

**0 MAJOR** — no missed edges within the `depends-on=[architecture]` admissible universe; all 5 r1-validated edges still hold; the new edge (em-046 → ar-032) and 4 new sensor predecessors (em-inv-005 ×2, em-inv-001 ×2) are mechanically derivable from v0.7 rules; no depends-on violations.

**0 MINOR** — §5 tally now matches §2/§4 row tables (was MIN-4 in r1, fixed in v0.2.0); rationales tightened correctly (em-005a, em-040); forward-cite deferral and `§4.a` carve-out compliance verified.

### Severity rollup

- **0 BLOCKER**
- **0 MAJOR**
- **0 MINOR**

### Lane breakdown

N/A (no findings).

## Verification evidence per pilot row affected by v0.2.0

| Pilot row | v0.2.0 change | r2 verification |
|---|---|---|
| `em-001` (line 43) | Row note unchanged (already correct in v0.1.0 per r1); v0.7 rules unchanged | VALID. Supporting-cite reclassification holds. |
| `em-005` (line 47) | Row note tightened to clarify forward-extension cite mechanics | VALID. Tightened rationale is mechanically correct under v0.7 §3.1 step 1 F-pilot-AR-10 supporting-cite test. |
| `em-011` (line 53) | Row table unchanged; row note unchanged | VALID. Two edges (ar-001, ar-005) confirmed against term-ownership rules. |
| `em-040` (line 95) | Row note rewritten to lead with F-pilot-AR-r2-2 invariant-as-target precedence (v0.7 §3.1 step 5 precedence rule) | VALID. AR §4.9 contains AR-INV-007; precedence rule fires; supporting-cite analysis parallel and consistent. No-edge outcome is invariant under either rationale. |
| `em-046` (line 102) | Row table gained `ar-032`; row note added direct term-use edge rationale | VALID. EM-046 body (line 552) term-uses `actor_role` + names role values from AR-032's seven-role vocabulary. Direct edge admissible per F-em-r1-MIN-10. |
| `em-inv-001` (line 118) | Predecessors gained `em-016, em-017`; row note explains direct term-use per v0.7 §3.1 step 5 invariant-body sub-clause | VALID. EM-INV-001 body (line 572) term-uses "git checkpoint trail" — anchored by EM-016 (checkpoint def) + EM-017 (trailers anchor the trail). Per v0.7 §2.5 source (4) + §3.1 step 5 invariant-body sub-clause, both are direct sensor predecessors. Optional per synthesis §1.3; explicit-is-better-than-transitive at corpus scale. |
| `em-inv-005` (line 120) | Predecessors changed from "(none — see notes)" to `em-schema.checkpoint-trailers, em-014`; gained `gated-by-corpus-scale` tag; row note re-framed per v0.7 §3.1 step 5 sub-clause + §2.5 sensor-predecessor degeneracy | VALID. EM-INV-005 body (line 584) term-uses `Harmonik-Bead-ID` (owned by `em-schema.checkpoint-trailers` registry row per §6.2) and bead-tied-runs concept (owned by `em-014`); both fire as direct sensor predecessors. Residual out-of-`depends-on` cross-spec body cites (RC §8.4 Cat 3, BI §4.7 BI-022) → `gated-by-corpus-scale` tag per v0.7 §2.5 degeneracy clause (drops at edge-fire time when RC/BI pilots resolve). F-pilot-EM-7 RE-FRAMED correctly: not invented sensor-degeneracy clause but actual term-use edges per v0.7 source (4). |
| §3 sensor table preamble (line 114) | Updated from THREE §10.2 sources to FOUR | VALID. Reflects v0.7 §2.5 fourth source (invariant-body inline term-use) per F-em-r1-MAJ-1. |
| §5 narrative (lines 168-178) | Tally rewritten 4 → 6; new bullet for `em-046 → ar-032`; previously-missing `em-schema.node → ar-005` enumerated | VALID. F-em-r1-MIN-4 (off-by-one tally) closed; F-em-r1-MIN-10 (em-046 direct edge) materialized. §5 narrative now matches §2/§4 row tables (6 emitted edges). |
| §8 F-pilot-EM-1, F-pilot-EM-3, F-pilot-EM-5, F-pilot-EM-6, F-pilot-EM-7 | Resolution status reflects v0.7 patches (RESOLVED / RE-FRAMED) | VALID. Each finding's resolution traces to a v0.7 discipline patch (§2.2 F8b worked example, §3.2 grandfather carve-out, §2.6 typed-alias-cluster guidance, §2.11(d.1) registry-row dual-ownership, §3.1 step 5 sub-clause + §2.5 degeneracy). |

## Closing

The EM pilot v0.2.0 is reference-clean under discipline v0.7. The six emitted AR cross-spec edges (em-011 → ar-001/ar-005; em-schema.node → ar-001/ar-005; em-schema.transition → ar-032; em-046 → ar-032) are all mechanically derivable from term-use precision (§3.1 step 5). The four new intra-EM sensor-body term-use edges (em-inv-001 → em-016/em-017; em-inv-005 → em-schema.checkpoint-trailers/em-014) are mechanically derivable from v0.7 §3.1 step 5 invariant-body sub-clause + §2.5 source (4). The `gated-by-corpus-scale` tag on em-inv-005 correctly captures the residual forward-deferred body cites. The three intra-EM bidirectional 2-cycles (em-002↔em-041, em-018↔em-019, em-020↔em-020a) remain resolved correctly; the em-018↔em-019 case is now mechanically supported by v0.7's second F13 worked example (declaration-rule ↔ retrieval-method pattern). All r1 findings are closed or RE-FRAMED per synthesis lane handling.

**Pilot is ready to load** pending coverage-r2 and decomposition-r2 outcomes.
