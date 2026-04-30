# AR Pilot Reference Review (r1) — 2026-04-27

Reviewer: Reference reviewer per `pilot-review-protocol.md` v0.2 §3.3.
Source spec: `specs/architecture.md` v0.3.1 (`reviewed`).
Pilot draft: `docs/decompose-to-tasks/ar-pilot.md` v0.1.0.
Discipline: `docs/decompose-to-tasks/discipline.md` v0.4.

## Summary

AR's front-matter declares `depends-on: []` (line 16), so per discipline §3.2 the universe of admissible cross-spec edge targets is empty and the pilot is required to emit zero outbound cross-spec edges. The pilot draft §5 declares "Result: ZERO outbound cross-spec edges from AR" and the §2 / §3 / §4 / §6 tables are consistent with that declaration — every "blocks edges" entry is intra-spec or `(none)`.

I walked AR's body top-to-bottom and enumerated every cross-document cite. Three classes appear: (1) `docs/foundation/components.md §N` bootstrap stubs (the bulk — these are not spec IDs and per discipline §3.1 generate no edges); (2) `[architecture.md §X]` self-cite *examples* inside AR-023's `touches:` shape illustration (self-cite, not cross-spec); (3) one `[event-model.md §6.3]` placeholder pair inside §A.1 envelope exemplar (appendix, non-edge-generating per discipline §3.1) plus several similar appendix placeholders. There are zero inline cites to other normative spec IDs (`[event-model.md §N]`, `[control-points.md §N]`, etc.) in AR's normative §4 / §5 / §6 prose. The pilot's claim of zero outbound edges is correct.

**Result: zero BLOCKER, zero MAJOR, zero MINOR findings.** One advisory observation about a cite in §10.2 prose that is correctly excluded from edge generation but worth flagging for the discipline author since it is a normative-spec-ID cite outside the §4/§5/§6/§8 envelope the protocol §3.3 specifically names.

## Inline cites enumerated

Every cross-document cite in AR's body, by line number, classified by edge-generation status per discipline §3.1.

| Line | Citing site | Cite text | Section | Edge-generating? | Rationale |
|---|---|---|---|---|---|
| 45 | §2.2 Out of scope | `[docs/foundation/components.md §3]` | §2 (out-of-scope, descriptive) | No | Not a spec ID; §2 is scope (not §4/§5/§6/§8). |
| 46 | §2.2 Out of scope | `[docs/foundation/components.md §2]` | §2 | No | Same. |
| 47 | §2.2 Out of scope | `[docs/foundation/components.md §4]` | §2 | No | Same. |
| 48 | §2.2 Out of scope | `[docs/foundation/components.md §6]` | §2 | No | Same. |
| 49 | §2.2 Out of scope | `[docs/foundation/components.md §8]` | §2 | No | Same. |
| 50 | §2.2 Out of scope | `[docs/foundation/components.md §9]` | §2 | No | Same. |
| 51 | §2.2 Out of scope | `[docs/foundation/components.md §10]` | §2 | No | Same. |
| 52 | §2.2 Out of scope | `[docs/foundation/components.md §7]` | §2 | No | Same. |
| 152 | AR-010 (§4.3) | `[docs/foundation/components.md §2]` ×2, `[docs/foundation/components.md §6]` | §4 normative | No | `docs/foundation/components.md` is a bootstrap stub, not a spec ID; per discipline §3.1 only `[<other-spec-id>.md §N]` cites generate edges. |
| 158 | AR-011 (§4.3) | `[docs/foundation/components.md §3]` | §4 normative | No | Same. |
| 192 | AR-016 (§4.5) | `[docs/foundation/components.md §8]` | §4 normative | No | Same. |
| 198 | AR-017 (§4.5) | `[docs/foundation/components.md §4]`, `[docs/foundation/components.md §8]`, `[docs/foundation/components.md §10]` | §4 normative | No | Same. |
| 204 | AR-018 (§4.5) | `[docs/foundation/components.md §9]` | §4 normative | No | Same. |
| 224 | AR-021 (§4.6) | `build-practices.md §Agent review` | §4 normative | No | `build-practices.md` is an internal-component doc under `docs/components/internal/`, not a spec under `specs/`; per discipline §3.1 only `[<other-spec-id>.md §N]` cites generate edges. |
| 237 | AR-023 (§4.6) | `[architecture.md §4.1]`, `[architecture.md AR-016]` | §4 normative | No | These are illustrative `touches:`-shape *examples* embedded in the requirement text. They are self-cites (AR citing itself) and serve to show the SHAPE of a `touches:` entry; they are not assertions that AR-023 depends on AR-016 / §4.1. Self-cites are not cross-spec edges. |
| 245 | AR-024 (§4.7) | `[docs/foundation/components.md §4]` ×3, `[docs/foundation/components.md §3]` | §4 normative | No | Bootstrap stub. |
| 263 | AR-027 (§4.7) | `[docs/foundation/components.md §6]`, `[docs/foundation/components.md §2]`, `[docs/foundation/components.md §4]`, `[docs/foundation/components.md §3]` | §4 normative | No | Bootstrap stub. |
| 267 | AR-027 (§4.7) | `event-model.md §8.3.2`, `event-model.md §8.3.8`, `handler-contract.md HC-008`, `workspace-model.md §5.3a` | INFORMATIVE block under AR-027 | No | Inside `> INFORMATIVE:` block (the AR-MIG-001 migration note); per discipline §3.1 informative blocks generate no edges. |
| 277 | AR-029 (§4.7) | `[docs/foundation/components.md §2]` | §4 normative | No | Bootstrap stub. |
| 283 | AR-030 (§4.7) | `[docs/foundation/components.md §2]` | §4 normative | No | Bootstrap stub. |
| 289 | AR-031 (§4.7) | `[docs/foundation/components.md §6]` | §4 normative | No | Bootstrap stub. |
| 309 | AR-034 (§4.8) | `[docs/foundation/components.md §6]` | §4 normative | No | Bootstrap stub. |
| 321 | AR-036 (§4.8) | `[docs/foundation/components.md §5]` | §4 normative | No | Bootstrap stub. |
| 341 | AR-039 (§4.9) | `[docs/foundation/components.md §5]` | §4 normative | No | Bootstrap stub. |
| 347 | AR-040 (§4.9) | `[docs/foundation/components.md §9]` | §4 normative | No | Bootstrap stub. |
| 391 | AR-047 (§4.10) | `docs/foundation/spec-template.md` | §4 normative | No | Internal-doc reference, not a spec under `specs/`. |
| 397 | AR-048 (§4.10) | `[docs/foundation/components.md §2]` | §4 normative | No | Bootstrap stub. |
| 403 | AR-049 (§4.10) | `[docs/foundation/components.md §10]` | §4 normative | No | Bootstrap stub. |
| 409 | AR-050 (§4.10) | `[docs/foundation/components.md §10]` | §4 normative | No | Bootstrap stub. |
| 425 | AR-INV-001 (§5) | `build-practices.md §Agent review` | §5 normative | No | Internal-component doc, not a spec. |
| 437 | AR-INV-007 (§5) | `[docs/foundation/components.md §8]` | §5 normative | No | Bootstrap stub. |
| 451–454 | §6 Schemas-and-data-shapes defer table | `[docs/foundation/components.md §4/§3/§2/§6]` | §6 informative defer table | No | Bootstrap stubs; the §6 prose is "the schemas live in their owning specs" — a description of where they live, not a dependency declaration. AR's only own §6 shape is the agent-type regex. |
| 482–488 | §9.3 Co-references | `[docs/foundation/components.md §2/§3/§4/§6/§8/§9/§10]` | §9.3 | No | §9.3 co-references generate no edges per discipline §2.7 third class; also bootstrap stubs. |
| 490 | §9.3 trailer | (informative note about bootstrap migration) | §9.3 INFORMATIVE | No | Informative note, not a cite. |
| 504 | §10.2 reviewer-persona block | `docs/components/internal/build-practices.md §Agent review on every commit` | §10.2 INFORMATIVE block | No | Internal-component doc + INFORMATIVE block. |
| 512 | §10.2 AR-032..AR-036 group | `control-points.md §6.6` | §10.2 prose | No | §10.2 is the test-surface-obligations section; protocol §3.3 lists normative-edge sections as §4/§5/§6/§8. Furthermore the cite is descriptive ("control-points.md §6.6 cites this spec's §4.8 for names" — a fact ABOUT control-points, not an obligation AR-032 has on control-points). See advisory observation below. |
| 516 | §10.2 trailer | `[testing.md §<layer>]` | §10.2 future-tense prose | No | Future-tense (`occurs within one revision cycle after testing.md is finalized`) — not a present-tense cite; testing.md is not a current spec. |
| 528 | OQ-AR-001 | `[testing.md §<layer>]` | §11 OQ | No | OQ section, non-edge-generating per discipline §3.1. |
| 580–582 | §12 revision history | various | §12 | No | Revision history, non-edge-generating per discipline §3.1. |
| 596 | §A.1 envelope exemplar | `[event-model.md §6.3]` | §A appendix | No | Appendix, non-edge-generating per discipline §3.1. The cite is a placeholder INSIDE a copy-pasteable template. |
| 600 | §A.1 envelope exemplar | `[event-model.md §6.3]` | §A appendix | No | Same. |
| 610 | §A.1 envelope exemplar | `[handler-contract.md §N]` | §A appendix | No | Same. |
| 614 | §A.1 envelope exemplar | `[execution-model.md §6.1]` | §A appendix | No | Same. |
| 618 | §A.1 envelope exemplar | `[control-points.md §N]` | §A appendix | No | Same. |
| 622 | §A.1 envelope exemplar | `[operator-nfr.md §N]` | §A appendix | No | Same. |

**Total cross-document cites:** ~50 (counting each `docs/foundation/components.md §N` occurrence, the appendix placeholders, the two informative-block migration-site mentions, and the §10.2 line-512 cite).

**Total edge-generating cites:** 0.

This matches the pilot's §5 declaration of zero outbound cross-spec edges.

## Missed edges

None. AR's `depends-on: []` (line 16) plus the absence of any `[<spec-id>.md §N]`-style normative-prose cite to a spec in the foundation corpus means no edge can be missed. Verified by exhaustive walk above.

## Invented edges

None. The pilot §5 is a declarative empty-set statement ("Result: ZERO outbound cross-spec edges from AR"); there are no edge entries in pilot §2 / §3 / §4 / §6 tables to invent. Spot-check of the "blocks edges (citing bead → prerequisite)" column in pilot §2:

- All entries are either `(none)`, intra-spec mnemonic IDs (`ar-XXX`), or schema-bead mnemonics (`ar-schema.agent-type-identifier`).
- Zero entries reference any other spec's mnemonic prefix (no `bi-*`, `em-*`, `ev-*`, `cp-*`, `hc-*`, `on-*`, `pl-*`, `rc-*`, `wm-*`).
- Same for §3 sensor table and §4 schema table.

## Depends-on violations

None. With `depends-on: []`, any cross-spec edge would be a violation; the pilot emits zero edges, so zero violations. The pilot's §5 rationale-1 explicitly cites the empty `depends-on` (line 16) and discipline §3.2 as the basis. Correct application.

## Wide-fanout tag check

Not applicable. There are zero section-anchor cites that resolve to spec IDs (every section-anchor cite in AR points at `docs/foundation/components.md`, which is not a spec). The pilot has no `cite:wide-fanout` tags and is not required to have any.

## Bidirectional cycles

None. No outbound cite from AR to any other normative spec in foundation prose. (The reverse direction — every other foundation spec depending on AR — is by design and is unilateral; AR is the root of the corpus per §9.1 line 474. There is no path back from AR to those specs in normative prose, so the bidirectional condition cannot fire.)

## AR-specific spec checks

Per the prompt's "Additional spec-specific checks for AR":

1. **`docs/foundation/components.md` stub cites correctly recognized as non-edge-generating.** PASS. The pilot §5 rationale-3 explicitly states this and identifies them as bootstrap stubs that will migrate to spec cites within one revision cycle once each target spec finalizes (per AR's §9.3 informative trailer at line 490). The pilot also surfaces this as flag `F-pilot-AR-2` for the discipline author — recognizing that, once those specs are finalized and the cites migrate, AR's `depends-on` will need to grow accordingly. This is a future-tense concern correctly deferred per the discipline.

2. **§A.1 envelope-exemplar cites correctly recognized as appendix.** PASS. Pilot §5 rationale-5 enumerates them (`[event-model.md §6.3]`, `[handler-contract.md §N]`, `[execution-model.md §6.1]`, `[control-points.md §N]`, `[operator-nfr.md §N]`) and correctly cites discipline §3.1 to exclude them. Verified that they appear only inside the §A.1 markdown code block (lines 590–632) and nowhere else in normative prose.

3. **Pilot §5 declares empty.** PASS. Pilot §5 is exactly the form expected: a "Result: ZERO outbound cross-spec edges from AR" declaration plus seven enumerated rationales, plus a `depends-on: []` validation note, plus an intra-spec cycle-check spot-pass.

## Findings list

**No BLOCKER findings.**

**No MAJOR findings.**

**No MINOR findings.**

## Advisory observations (non-finding)

These are not findings under the protocol's severity ladder; they are observations that may interest the discipline author per protocol §4.1's bias toward over-flagging. Tagged `class` per reviewer self-classification rules in §3.3 because they involve the discipline's silence on edge cases that recur across specs.

1. **§10.2 cite to `control-points.md §6.6` (line 512).** This is the ONLY non-`docs/foundation/components.md` and non-`build-practices.md` and non-appendix cross-spec cite in AR's body that is to a real normative-spec ID. It sits inside the §10.2 test-surface-obligations bullet for the AR-032..AR-036 (role taxonomy) group: "control-points.md §6.6 cites this spec's §4.8 for names." Three reasons it is correctly NOT edge-generating:

   (a) Protocol §3.3 lists normative-edge-bearing sections as §4 / §5 / §6 / §8. §10.2 is conformance, not in that list.

   (b) The cite is *descriptive* — it states a fact about control-points (that it cites AR), not an obligation that AR-032..AR-036 has on control-points. The implementation work for AR-032 is "name the seven roles in §4.8 of architecture.md"; AR has no dependency on control-points.md being implemented.

   (c) If anything, this is the inverse-direction edge: control-points cites AR's §4.8, so when CP's pilot runs, it will emit `cp-XXX → ar-032..ar-036` edges (or similar). AR is the prerequisite, not the dependent.

   The pilot does not enumerate this cite in §5's rationale list. That's a minor cosmetic gap, not a correctness gap. **Discipline-level observation:** discipline §3.1's no-edge list ("§9.3 co-references, informative blocks, §A appendices, §11 OQs, §12 revision history") does not explicitly call out §10 conformance / test-surface obligations. Adding "§10 conformance prose" to the no-edge list would close the silence and make this case mechanical for future pilots. Filing as a `class` observation per protocol §4.1.

2. **AR-023's `[architecture.md §4.1]` and `[architecture.md AR-016]` self-cite examples (line 237).** These are illustrative `touches:`-shape examples, not actual self-dependencies. The pilot does not enumerate them in §5's rationale list. Treated correctly (self-cites are not cross-spec edges), but — like observation 1 — discipline §3.1 does not explicitly handle the "self-cite as illustrative example" case. A future spec that uses similar `touches:`-style example syntax could trip a less-careful reviewer. Filing as a `class` observation. Severity: well below MINOR; documenting only because protocol §4.1 biases toward over-flagging on early pilots.

3. **`docs/components/internal/build-practices.md` cites (lines 224, 425, 504).** AR-021 and AR-INV-001 cite `build-practices.md §Agent review` to ground the architect / conformance-auditor / critic / scope-steward persona definitions. `build-practices.md` lives at `docs/components/internal/`, not under `specs/`, and is not a normative spec. Per discipline §3.1, only `[<other-spec-id>.md §N]` cites where `<other-spec-id>` matches a spec under `specs/` generate edges. Treated correctly (no edges emitted). Discipline §3.1 doesn't explicitly enumerate "internal-component-doc cites" as non-edge-generating (it does enumerate `docs/foundation/components.md` implicitly via the no-spec-ID rule); a future-tense polish would explicitly list this class. Filing as a minor `class` observation.

## Lane assignments

All three advisory observations are tagged `class` (the discipline rule has silences that any pilot in this position would notice). Per protocol §4.1 they would route to discipline-patch-lane consideration; per §4.2 the discipline author may batch them at MINOR severity into the next discipline patch rather than triggering one. None blocks the AR pilot from loading.

No `local` findings.

## Conclusion

The AR pilot's reference handling is correct. The empty-set claim ("Result: ZERO outbound cross-spec edges from AR") is verified by exhaustive walk of AR's normative §4 / §5 / §6 / §8 prose: every cross-document cite in those sections targets either `docs/foundation/components.md §N` (a bootstrap stub, not a spec ID), `docs/components/internal/build-practices.md §...` (an internal-component doc, not a spec), or appendix / informative / OQ / revision-history blocks — none of which generate edges per discipline §3.1. AR's `depends-on: []` is consistent with this empty-edge result. The pilot is REFERENCE-CLEAN as drafted; no patches required for the references dimension of the review.
