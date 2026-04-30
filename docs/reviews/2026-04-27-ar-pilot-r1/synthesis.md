# AR Pilot Review (r1) — Synthesis

Date: 2026-04-27. Pilot under review: `docs/decompose-to-tasks/ar-pilot.md` v0.1.0. Discipline at time of review: `discipline.md` v0.4. Protocol applied: `pilot-review-protocol.md` v0.2.

Reviewers (parallel, completed):
- Coverage — `coverage-r1.md` (1 MINOR finding, 0 class).
- Decomposition-quality — `decomposition-r1.md` (2 BLOCKER, 2 MAJOR, 4 MINOR; 6 class-touched).
- Reference — `references-r1.md` (0 findings, 3 advisory class observations).

## 1. Triage table

For each BLOCKER and MAJOR finding, the four probes from protocol §4.1 are applied. MINOR findings are also tabled where they are class-tagged or batchable, since the discipline patch is already firing on the MAJOR class co-tags.

| # | Source | Sev | Probes (G / R / S / Self) | Lane | Disposition |
|---|---|---|---|---|---|
| M1 | Cov | MINOR | n / n / n / local | pilot | Fix §1 narrative miscount: "5 retired IDs" → "7 retired IDs total". |
| 2.1 | Decomp | BLOCKER | n / n / n / local | pilot | Drop invented edge `ar-029 → ar-016`. AR-029 inline-cites AR-011 + AR-018 only. |
| 2.2 | Decomp | BLOCKER | n / n / n / local | pilot | Drop invented edge `ar-035 → ar-032`. AR-035 inline-cites AR-026 only. |
| 2.3 | Decomp | MAJOR | y / y / y / class | **discipline** | F13 silently collapsed AR-013↔AR-053 bidirectional cite without surface-for-resolution. F13 needs a default-resolution heuristic for slot-rule vs content-rule pairs. |
| 2.4 | Decomp | MAJOR | y / y / y / class | **discipline** | Same root cause as 2.3 — second bidirectional pair AR-052↔AR-053 in the same envelope-slot trio. Confirms F13 silence is recurring. Pilot also missed `ar-052 → ar-016` (the non-bidirectional half) — local; rolls into the re-draft. |
| 2.5 | Decomp | MINOR | y / y / y / class | **discipline** (batched) | Discipline silent on term-use edges. Pilot emitted `ar-006/007 → ar-005`. AR has 4+ such cases; EM/HC/CP/RC behavioural specs will have many more. |
| 2.6 | Decomp | MINOR | y / y / y / class | **discipline** (batched) | Same root cause: term-use edge `ar-039 → ar-036`. |
| 2.7 | Decomp | MINOR | y / y / y / class | **discipline** (batched) | Same root cause; also surfaces internal pilot inconsistency (`ar-029/030/031` term-use edges incoherent). |
| 2.8 | Decomp | MINOR | y / y / y / class | **discipline** (batched) | §10.2 reviewer-persona bundling silent on whether it counts as an inline cite for sensor edges. |
| AO1 | Ref | (advisory) | y / n / y / class | **discipline** (batched) | §10 conformance prose not enumerated as non-edge-generating in §3.1's no-edge list. |
| AO2 | Ref | (advisory) | n / n / y / class | **discipline** (batched) | Self-cite-as-example case (AR-023's `[architecture.md §4.1]`) silent in discipline. Below-MINOR. |
| AO3 | Ref | (advisory) | y / n / y / class | **discipline** (batched) | `docs/components/internal/` doc cites not explicitly enumerated as non-edge-generating. |

Probe legend: G = Generality (≥2 specs would hit this), R = Rule-vs-application (followed discipline correctly + still bad output), S = Silence (discipline mute on this content), Self = reviewer self-classification.

## 2. Lane decisions

### 2.1 Pilot-patch lane (3 items)

Applied to AR pilot in the **re-draft** that follows the discipline patch (per protocol §4.2: "the affected pilot is RE-DRAFTED against the new discipline version; do not patch the pilot in place"):

- **M1.** §1 narrative: "5 retired IDs total" → "7 retired IDs total" + remove inline self-correction.
- **2.1.** Drop `ar-016` from `ar-029` blocks edges. Final: `ar-029` blocks on `ar-011, ar-018`.
- **2.2.** Drop `ar-032` from `ar-035` blocks edges. Final: `ar-035` blocks on `ar-026`. (May also collapse to a notes line on `ar-026` per F-pilot-AR-1 if discipline §2.1 grows the cross-reference exception — see discipline patch below.)

### 2.2 Discipline-patch lane (driving v0.4 → v0.5)

Six changes batched into one discipline patch, driven by 2.3 + 2.4 (MAJOR class) and joined by the four MINOR class findings + three advisory class observations:

1. **§2.7 F13 default-resolution heuristic for slot-rule vs content-rule pairs (PRIMARY — drives the patch).** When A defines a slot/section/envelope-element rule and B defines the content that fills the slot, A→B is the normative dep (slot points at what fills it); B→A is informational plumbing ("declare in the slot reserved by §X") — reclassify B→A to no-edge. Worked example: AR-053 (slot rule) vs AR-013 (content of slot) — `ar-053 → ar-013` survives, `ar-013 → ar-053` is informational. Same shape AR-053 (slot) vs AR-052 (subsystem categorization that names the slot obligation) — `ar-053 → ar-052` survives, `ar-052 → ar-053` is informational. Pilot §8 of AR will record this resolution as F-pilot-AR-8.

2. **§3.1 / §2.7 term-use edge rule (NEW CLAUSE).** Decision: **emit term-use edges as `blocks`**, mirroring the conservative bias of F4's `waits-for → blocks` collapse. Rule text: "Inline use of a defined term whose definition is owned by another requirement in the SAME spec (e.g., a `mechanism`-tagged or `cognition`-tagged classifier from a definition rule, a named schema, a named invariant, a named role) generates a `blocks` edge to the defining requirement bead. The same rule applies cross-spec when the defined term has a single-owner spec." Rationale: (a) over-gating is never wrong; (b) under-gating risks readiness-workflow misordering; (c) the pilot's intuitive behavior (emit-the-edge) was already correct — this codifies it. Worked examples to add: AR-006/007 → AR-005 (mech/cog tag classifiers); AR-039 → AR-036 (merge-agent term).

3. **§3.1 no-edge list update.** Add three classes to the explicit no-edge enumeration:
   - **§10 conformance / test-surface obligations prose** — non-edge-generating (per AO1).
   - **`docs/components/internal/` doc cites** — non-edge-generating (per AO3, since they're not specs under `specs/`).
   - **Self-cite illustrative examples** in `touches:`-shape blocks or similar (per AO2) — non-edge-generating.

4. **§2.5 sensor-edge clarification re §10.2 reviewer-persona bundling (per 2.8).** Decision: the §10.2 reviewer-persona group bundle DOES count as a sensor-edge source IF the persona-name explicitly bundles the invariant with the §4 req. Add a clause: when §10.2 names a persona "checks X-NNN, Y-NNN, Z-INV-NNN" together, the invariant's sensor bead `blocks-on` X-NNN and Y-NNN. Rationale: the §10.2 bundle is the spec's own statement that these reqs are inseparable from the invariant's verification.

5. **§2.1 fourth exception — pure cross-reference requirement (per F-pilot-AR-1).** Add an exception: a requirement whose normative body is a verbatim "see §N.OTHER-NNN" pointer (no independent normative content) becomes a notes line on the cited bead, not its own bead. Worked example: AR-035 ("Role is orthogonal to agent type — see §4.7.AR-026") collapses to a notes line on `ar-026`. (This finding also resolves 2.2's spurious-edge issue at the structural level.)

6. **§2.6 schema typology gap for primitive shapes (per F-pilot-AR-3).** Add a fourth schema category: **constrained primitive types** (regex-validated strings, format-restricted scalars). Naming: `<spec>-schema.<type-kebab>` same as RECORD/INTERFACE/ENUM. Worked example: AR §6.1's `agent_type := ^[a-z][a-z0-9-]{1,62}$` becomes `ar-schema.agent-type-identifier` (already minted by the pilot intuitively; this codifies the rule). Tag: `kind:primitive-shape` (new value alongside `kind:schema`/`kind:interface`/`kind:enum`).

Six changes, ~80 lines of discipline edits. Update §6 revision history. Tag the patch row F-pilot-AR-{1..8} for traceability.

### 2.3 Items NOT batched into v0.5

- **F-pilot-AR-2 (bootstrap-stub cite migration debt).** Real concern but not a discipline gap — `docs/foundation/components.md` cites are correctly non-edge-generating. The migration-debt tracking is a meta-concern about when AR's cites should be rewritten as the target specs finalize. Defer to a separate "carry-forward" tracking mechanism, not part of the decompose-to-tasks discipline. Capture in AR pilot §8 with a TODO marker.
- **F-pilot-AR-4 (cognition-bead description length cap).** No reviewer escalated this. The pilot's ~300-char description for AR-021 is internally fine; discipline §4 only caps title length. Not a finding; not patched.
- **F-pilot-AR-5 (foundation-cross-cutting self-classification).** Same — not a finding; the pilot handled it correctly. Will recur for ON, RC, BI but in a way the discipline already handles (no §4.a slot for foundation-cross-cutting specs).

## 3. Sequence of execution

Per protocol §5 load-gate version-match clause: AR pilot v0.1.0 is pinned to discipline v0.4. After v0.5 lands, the v0.1.0 draft is dead. Sequence:

1. Patch `discipline.md` v0.4 → v0.5 with the six changes above. Record F-pilot-AR-{1..8} in §6 revision history.
2. Re-draft `ar-pilot.md` v0.1.0 → v0.2.0 against v0.5:
   - Apply pilot-lane fixes (M1, 2.1, 2.2).
   - Apply v0.5 rules: term-use edges emitted; F13 resolution applied (fix AR-013↔AR-053 + AR-052↔AR-053); AR-035 collapsed to a notes line on `ar-026` (or kept as bead if cleaner — author choice); add `ar-052 → ar-016` (the non-bidirectional half of finding 2.4); confirm `ar-schema.agent-type-identifier` retains `kind:primitive-shape` tag.
   - Update pilot §8 to reflect resolved findings (mark F-pilot-AR-1 / -3 / -8 closed via discipline patch; F-pilot-AR-2 retained as carry-forward TODO).
   - Bump pilot revision history.
3. **Do NOT proceed to EM/EV until v0.5 lands and AR is re-drafted.** The whole point of the discipline lane is to prevent bug propagation across the remaining 8 pilots.
4. Once AR v0.2.0 is in, the pilot re-runs through reviewers OR (if patches are mechanical and don't restructure beads) the relevant subset re-runs per protocol §2 "after pilot patch" rule. Term-use edge addition restructures the bead set substantially, so all three reviewers should re-run. (To be confirmed in r2.)

## 4. Open question for the discipline author

The term-use edge rule (item 2 in §2.2 above) is a structural choice. The pilot intuitively emitted these edges; this synthesis codifies that direction. The alternative was to explicitly EXCLUDE term-use from edge generation (no edge unless inline-cite by ID). Either is internally consistent; this synthesis picked emit-the-edge for the reasons listed (over-gating safety, matches F4 collapse rationale).

If the discipline author later finds the term-use rule produces too many edges (e.g., EM behavioural spec has 100+ term-use sites) and the bead graph becomes hard to read, the rule can be tightened in v0.6 — possibly bounded to "term-use edges only when the citing req is `mechanism`-tagged AND the cited req is a definition rule." Defer that tightening until evidence emerges.
