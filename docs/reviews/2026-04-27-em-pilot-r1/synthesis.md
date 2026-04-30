# EM Pilot Review (r1) — Synthesis

Date: 2026-04-27. Pilot under review: `docs/decompose-to-tasks/em-pilot.md` v0.1.0. Discipline at time of review: `discipline.md` v0.6. Protocol: `pilot-review-protocol.md` v0.2.

Reviewers (parallel, completed):
- Coverage — `coverage-r1.md` (0 BLOCKER / 0 MAJOR / 2 MINOR, both `class`).
- Decomposition-quality — `decomposition-r1.md` (0 BLOCKER / 1 MAJOR class / 4 MINOR — 3 `class` + 1 `local`).
- Reference — `references-r1.md` (0 BLOCKER / 3 MAJOR class / 4 MINOR — 3 `class` + 1 `local`).

## 1. Outcome

**0 BLOCKER, 4 MAJOR (all `class`), 6 MINOR (4 `class` + 2 `local`) = 10 distinct findings + 3 policy decisions to record.**

The pilot is structurally sound. All four MAJOR findings are documentation-tightening or rule-codification (not pilot-structure errors). The four `class`-tagged MAJORs route to the discipline-patch lane per protocol §4.1. Two `local` MINORs route to the pilot-patch lane.

### 1.1 Discipline-lane findings (10 — drive v0.7 patch)

| Tag | Severity | Source | Description | v0.7 patch target |
|---|---|---|---|---|
| F-em-r1-MAJ-1 | MAJOR `class` | decomp §2.6 / refs F-em-r1-9 / cov M1 | Discipline silent on whether sensor beads emit term-use edges from invariant body terms. `em-inv-005`'s body uses `Harmonik-Bead-ID` (owned by `em-schema.checkpoint-trailers`) and merge-commit/bead-run linkage (em-014); these MUST fire as predecessors per §3.1 step 5. **Behavioral**: changes `em-inv-005`'s blocks list. | §3.1 step 5: invariant-body term-uses derive sensor predecessor edges; §2.5 §10.2 enumeration grows a 4th source (invariant-body inline term-use). |
| F-em-r1-MAJ-2 | MAJOR `class` | refs F-em-r1-7 (re-framed) / decomp §2.10 / cov M1 | Sensor with empty predecessor list under `depends-on=[architecture]` and forward-deferred body cites. Codify the `gated-by-corpus-scale` transient tag (analog of F6 `gated-by-spec-edit`) so degenerate sensors are visible to ops without firing `br ready` falsely. | §2.5 sensor-predecessor degeneracy sub-rule + new `gated-by-corpus-scale` tag definition. |
| F-em-r1-MAJ-3 | MAJOR `class` | refs F-em-r1-8 | Discriminated-union type-alias declarations (e.g., `VerdictPayload` resolving to RC's `VerdictEvent`) inside INFORMATIVE blocks not addressed by §2.6 schema-bead rule. CP/HC will retest (Policy/Gate union, error-sentinel union). | §2.6: type-alias-resolves-to-single-MVH-variant policy (don't mint a thin redundant bead). |
| F-em-r1-MAJ-4 | MAJOR `class` | decomp §2.10 / em-040 | Rule-precedence ambiguity when both F-pilot-AR-10 (supporting-cite) and F-pilot-AR-r2-2 (invariant-as-target exemption) plausibly apply. Multiple specs will cite cross-cutting principles now promoted to `<PREFIX>-INV-NNN`. | §3.1 step 5: invariant-as-target exemption takes precedence over supporting-cite analysis when both could apply. |
| F-em-r1-MIN-3 | MINOR `class` | refs F-em-r1-3 | §3.1 v0.6 no-edge list is implicit on §7 prose (protocols / state machines / pseudocode). PL/RC §7 sections will retest. | §3.1 no-edge list grows explicit "§7 protocol/pseudocode/state-machine prose" entry. |
| F-em-r1-MIN-5 | MINOR `class` | refs F-em-r1-5 | §2.7 F13 slot-rule heuristic worked example covers cascade↔shape (em-002↔em-041) but path-declaration↔retrieval-method (em-018↔em-019) is a different sub-pattern. RC verdict-record↔verdict-execution and PL startup-sequence↔component-init will retest. | §2.7: second worked example for declaration-rule ↔ method-rule pattern. |
| F-em-r1-MIN-6 | MINOR `class` | refs F-em-r1-6 | Forward-deferred wide-fanout cites (e.g., EM-025→EV §8 ~14 events) — should they pre-emit `cite:wide-fanout` placeholders, or only tag when the edge fires? Discipline mute. | §3.1.3: clarify forward-deferred wide-fanout tag policy (recommend: emit tag at edge-fire time, not at deferral time; record deferred fanouts in pilot's forward-cite log). |
| F-em-r1-MIN-7 (F-pilot-EM-1) | MINOR `class` | self-flag, validated by decomp §2.5 | §2.2 F8b shared-function-body tiebreaker has BI-031 letter-step example but no behavioural-spec example. EM-016 (3-step git atomicity) and EM-038 (6-category validator) are textbook applications. | §2.2: worked example for F8b firing on a behavioural sequence. |
| F-em-r1-MIN-8 (F-pilot-EM-5) | MINOR `class` | self-flag, validated by decomp §3 | Typed-ID-alias clusters (RunID/StateID/TransitionID/NodeID/BeadID) are peer rules without anchor structure; strict §2.3 says no coalesce, but EV/RC will hit the same shape. | §2.6: typed-alias-cluster guidance — peer aliases stay separate; document the §2.3 non-fire reasoning. |
| F-em-r1-MIN-9 (F-pilot-EM-6) | MINOR `class` | self-flag, validated by decomp §2.8 / cov M2 | Registry-with-row-level-dual-ownership pattern (`em-schema.checkpoint-trailers` owns 7 keys, 2 of which are RC-owned per §6.2 informative annotation). Structurally analogous to §2.11(d) co-owned event payloads but at row level. | §2.11(d): extension to registry-row dual-ownership; the registry-owning spec mints the bead, dual-owned rows are annotated in description. |

**Forward-extension cite worked example** (decomp 2.1 / em-005's row note rationale was thin) is folded into F-em-r1-MAJ-4 — the precedence patch implicitly clarifies forward-extension cites (which is what F-pilot-AR-10 actually handles).

### 1.2 Pilot-lane findings (2 — apply at re-draft)

| Tag | Severity | Source | Action |
|---|---|---|---|
| F-em-r1-MIN-4 | MINOR `local` | refs F-em-r1-4 | §5 tally: "Total emitted cross-spec edges to AR: 4" → **5**. The 5th is `em-schema.node → ar-005` (already in §2 row table; missed in §5 narrative). Rewrite §5 narrative; update §7 if affected. |
| F-em-r1-MIN-10 | MINOR `local` | decomp §6.2 | `em-046` may emit direct `ar-032` term-use edge for `actor_role`/role vocabulary. Transitive coverage exists via `em-schema.transition → ar-032`; emit the direct edge for clarity. |

### 1.3 Pilot-lane fixes that ARE behavioral consequences of v0.7 (apply at re-draft against v0.7)

These are not findings per se; they fall out of v0.7 mechanically once F-em-r1-MAJ-1 lands.

| Affected bead | Change | Source rule |
|---|---|---|
| `em-inv-005` | Add predecessors `em-schema.checkpoint-trailers` (term-use of `Harmonik-Bead-ID` trailer key) and `em-014` (term-use of bead-run linkage / bead-tied-runs concept). Drop the "(none — see notes)" predecessor cell. F-pilot-EM-7 self-flag is RE-FRAMED (not invented sensor-degeneracy clause but actual term-use edges) — also keep `gated-by-corpus-scale` tag for the still-deferred body cites to `[reconciliation/spec.md §8.4 Cat 3]` and `[beads-integration.md §4.7 BI-022]`. | v0.7 §3.1 step 5 (invariant-body term-use) + §2.5 sensor-predecessor degeneracy. |
| `em-inv-001` | Optionally add `em-016`, `em-017` as direct term-use predecessors (transitive coverage via `em-031..em-033` already exists). Defer to re-draft author judgment; transitive read is sufficient. | v0.7 §3.1 step 5 (invariant-body term-use). |
| `em-040` row note | Lead rationale with F-pilot-AR-r2-2 invariant-as-target exemption (AR §4.9 houses AR-INV-007); F-pilot-AR-10 supporting-cite is parallel/redundant under v0.7 precedence rule. | v0.7 §3.1 step 5 precedence rule (F-em-r1-MAJ-4). |
| `em-005a` row note | Tighten rationale: "the cite attaches to a forward-extension statement that does not affect MVH-level testability of EM-005a's current discriminator-and-payload shape; supporting-cite per F-pilot-AR-10." | v0.6 + v0.7 documentation-tightening. |

## 2. Three policy decisions (recorded — no spec-body edits)

Per HANDOFF.md "Decisions Parked" 2026-04-27:

### 2.1 Option B — EM `depends-on` kept as-is

**Decision.** Do NOT patch the EM spec to expand `depends-on`. The pilot's deferral of ~50 forward cites stands. Reciprocal-direction edges from EV/HC/CP/RC/WM/BI/ON/PL pilots will materialize most of these dependencies when they run.

**Rationale.** The reviewer's Option A recommendation was only spot-verified for EV. Preserving the spec author's deliberate v0.2.0 cycle-break (event-model dropped to break a mutually-blocking cycle) is safer than a broad depends-on expansion that may re-introduce cycles the v0.2.0 break was designed to prevent. Revisit only if a downstream load surfaces a missing-predecessor problem that can't be resolved via the reciprocal direction.

**Mechanical consequence.** F-em-r1-7's MAJOR class severity is acknowledged but the recommended action (Option A) is overruled in favor of Option B. The override is recorded here per protocol §4.1 "any class tag from any reviewer routes to the discipline lane unless explicitly overruled in synthesis.md with a stated reason." F-em-r1-7 itself is closed by F-em-r1-MAJ-2 (sensor-predecessor degeneracy) — the `gated-by-corpus-scale` tag is the documentation mechanism that surfaces deferred-cite gaps without forcing premature `depends-on` expansion.

### 2.2 Option E — §4.a envelope grandfather carve-out

**Decision.** Do NOT patch EM (or HC/CP/WM/PL/RC) specs to add §4.a envelopes. The v0.7 discipline patch grows a carve-out: specs that were `reviewed` before AR-053 landed are grandfathered until their next revision.

**Rationale.** AR is foundation-cross-cutting and exempt by AR-052. The runtime-subsystem specs (HC/CP/WM/PL) that are pre-AR-053 are grandfathered. RC is mixed but enters the same grandfathering window. Retroactive enforcement is a separate conversation if/when the user wants it.

**Mechanical consequence.** F-pilot-EM-3 (pilot's self-flag of EM's missing §4.a envelope) is documented as carve-out compliant; no spec edit; no pilot patch. Add v0.7 carve-out language to discipline §3.2 / §6 envelope rule. Future pilots (EV) that ARE post-AR-053 do require §4.a; the carve-out applies only to the named pre-AR-053 set.

### 2.3 Option G — VerdictPayload not minted

**Decision.** `em-schema.verdict-payload` is NOT a separate bead. The type alias lives inside an INFORMATIVE block (normally non-edge-generating per §3.1) and resolves to RC's `VerdictEvent` at MVH. Minting would add a thin redundant layer.

**Rationale.** F-em-r1-8 surfaces a real discipline gap (§2.6 mute on discriminated-union aliases inside INFORMATIVE blocks). The v0.7 patch grows policy F-em-r1-MAJ-3: type-alias-resolves-to-single-MVH-variant declarations in INFORMATIVE blocks do NOT trigger the §2.6 schema-bead rule. CP and HC may surface analogous patterns (Policy/Gate union, error-sentinel union); each evaluated under the same rule.

**Mechanical consequence.** No `em-schema.verdict-payload` bead; pilot tally stays at 89.

## 3. Lane decision

All 4 MAJOR findings + 4 MINOR `class` findings route to the discipline-patch lane per protocol §4.1's class-bias rule. Per §4.2, MAJOR class findings normally block load.

### 3.1 Override criterion application

Apply override criterion (synthesis-r2.md §4 from AR cycle):

| Finding | Behavioral? | Pilot bead set under v0.6 vs v0.7 |
|---|---|---|
| F-em-r1-MAJ-1 (invariant-body term-use) | **YES** | em-inv-005 gets 2 new predecessor edges; em-inv-001 may get 2 more (optional). Bead set differs. **No override.** Pilot MUST re-draft against v0.7 before load. |
| F-em-r1-MAJ-2 (degeneracy + new tag) | NO (documentation; tag is metadata, not edge) | Tag added to em-inv-005 description; bead set otherwise unchanged. Override-eligible if MAJ-1 weren't behavioral. |
| F-em-r1-MAJ-3 (type-alias-MVH-redundant) | NO | Bead set already aligns with the policy (no `em-schema.verdict-payload` bead minted). Override-eligible. |
| F-em-r1-MAJ-4 (rule precedence) | NO (rationale rewrite, no edge change) | em-040's no-edge outcome is invariant under either rationale. Override-eligible. |

**MAJ-1 forces re-draft regardless** — the bead set IS NOT invariant under v0.7. So strict §4.2 fires; pilot re-drafts against v0.7; load gate respects version-match clause. The override criterion does NOT apply at the spec-level here (unlike AR's r2 cycle).

### 3.2 Sequence

1. **Patch `discipline.md` v0.6 → v0.7** with all 12 patch items (10 EM r1 findings + Option E carve-out + Option G policy). The behavioral one (F-em-r1-MAJ-1) is the load-bearing edit; the rest are documentation-tightening.
2. **Re-draft `em-pilot.md` v0.1.0 → v0.2.0** against discipline v0.7. Apply pilot-lane fixes (F-em-r1-MIN-4 §5 tally, F-em-r1-MIN-10 em-046 edge, em-040 rationale lead, em-005a rationale tighten) at re-draft time. Apply v0.7 mechanical consequences (em-inv-005 new edges, em-inv-001 optional edges, `gated-by-corpus-scale` tag on deferred-cite sensors).
3. **Run r2 reviewers** (coverage / decomposition / references) against em-pilot v0.2.0 + discipline v0.7. Output to `docs/reviews/2026-04-27-em-pilot-r2/`.
4. **Synthesize r2.** If clean, load. If new findings, apply lane handling (override criterion may apply if r2 findings are documentation-only).
5. **Load EM beads under prefix `hk`** to existing `.beads/` workspace (BI ∪ AR baseline preserved). Verify `br dep cycles` clean across BI ∪ AR ∪ EM.

## 4. Why this is the right call

The v0.6 → v0.7 patch is unavoidable: F-em-r1-MAJ-1 is behavioral, so AR's "documentation-tightening override" path doesn't apply. The other 3 MAJORs ride along as documentation-tightening within the same v0.7 bump. The two `local` MINORs are pilot-patch-lane and apply mechanically at re-draft.

The three Option-B/E/G policy decisions resolve all of EM's parked spec-edit questions without modifying the spec body, preserving the "ID-FROZEN" invariant from the autonomy scope. Each policy is documented in v0.7 so subsequent pilots (CP/HC for type-alias unions; EV's depends-on shape) can reference the rule rather than re-litigate.

The override criterion from AR r2 is preserved for future use but does not apply here. F-em-r1-MAJ-1's behavioral consequence (new edges on em-inv-005) is exactly the scenario the strict §4.2 gate is for.
