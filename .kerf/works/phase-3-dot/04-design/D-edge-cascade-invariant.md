# D-edge-cascade-invariant Design — Lock the 5-Step Cascade + Unconditional-Edge Fallback Invariant (Pass-4, phase-3-dot)

## Decision

The edge-routing cascade is normatively specified as the following 5-step deterministic procedure, with an **engine-MUST-fall-back-to-unconditional-edge invariant** baked in from day one:

### The cascade (5 steps)

Given a source node `N` that has emitted `Outcome o`, the engine selects the next edge by:

1. **Condition match.** Evaluate each outgoing edge's `condition` expression (D5 dialect) against `o` and the workflow `context`. Collect all edges whose `condition` is non-empty and evaluates true. If exactly one matches, route to it. If multiple match, proceed to step (b) using only the matched set as the candidate pool.
2. **Preferred label.** If `o.preferred_label` is set, select among the candidate pool the edge whose `preferred_label` attribute equals `o.preferred_label`. If exactly one matches, route to it; otherwise proceed.
3. **Suggested next IDs.** If `o.suggested_next_ids` is non-empty, select among the candidate pool the edges whose `to_node` appears in the list. If exactly one matches, route to it; if multiple, proceed with the intersection as the pool.
4. **Weight.** Among the remaining candidates, pick the edge with the highest `weight`. If tied, proceed.
5. **Lexical tiebreak.** Order remaining candidates lexicographically by `to_node` ID; pick the first.

### The fallback-to-unconditional-edge invariant

**If the candidate pool after step (a) is empty AND the source node has at least one outgoing edge with an empty (or absent) `condition` — the "unconditional edge" — the engine MUST proceed with the set of unconditional edges as the candidate pool, then continue at step (b).** Only after both the conditional pool *and* the unconditional pool are exhausted may the engine declare no-match per EM-046a (which then raises a `structural` failure with reason `no_outgoing_edge_matches`).

This is the **audit-V3.2 fix** from Attractor: the published audit history surfaced that an earlier implementation skipped unconditional edges when *any* conditional edge was authored, leading to silent dead-ends on outcomes the workflow author hadn't enumerated. The fix is to treat unconditional edges as a *fallback pool*, not as ignored-if-conditionals-exist.

## Why this is a normative invariant, not an implementation note

- **It is a load-bearing safety property.** Without the invariant, a workflow author writing "route on FAIL, with an unconditional edge to a generic next-node" gets a `structural` failure on SUCCESS instead of routing to the unconditional edge. This is the exact failure mode Attractor's audit caught. harmonik must not re-introduce it.
- **It is testable as a scenario.** A two-edge graph (one conditional, one unconditional) emitting an outcome that does NOT match the conditional edge must route to the unconditional edge. This is a single scenario-harness test (G2 scope).
- **It is invisible to handler authors but critical to workflow authors.** Handlers don't see the cascade; workflow authors do. The invariant must live in the spec where workflow authors look, not buried in engine-implementation prose.

## Why bake in from day one

Attractor learned this through audit pain. harmonik has the audit history available now (recon attractor-findings.md, audit V3.2 reference) and can spec it correctly from the outset. The cost of writing the invariant into pass-5 EM-041 prose is one paragraph; the cost of discovering the defect after MVH ships is a real-workflow silent-dead-end incident.

## Framings considered

- **A. Spec the cascade + invariant as a single normative procedure (this decision).** One coherent algorithm. Workflow authors read one section and have full routing semantics.
- **B. Spec only the cascade; leave fallback-to-unconditional as implementation-defined.** Smaller spec footprint; defers a known defect into the implementation layer.
- **C. Spec the cascade and treat unconditional edges as "always part of the conditional pool with `true` condition."** Mechanically equivalent to A; phrased differently. Rejected because workflow authors think of "empty condition" as "no condition," not as "condition = true" — the prose should match the mental model.

## Rationale

- **B is the framing Attractor had before V3.2.** It led to a real bug. We have the post-mortem; we should not relive it.
- **A is one paragraph longer than B.** Negligible cost.
- **C reads worse for the spec-as-teaching-tool.** Workflow authors want to know what "empty condition" means; the cleanest answer is "fallback pool when no conditional matches," not "secretly equivalent to `condition='true'`."

## Cross-references

- **EM-041** (locked, per pass-3 SUMMARY item #3) already names the 5-step cascade. D-edge-cascade-invariant *augments* EM-041 with the fallback invariant; it does not redesign the cascade.
- **EM-046a** (locked, per pass-3 SUMMARY item #7) defines the no-match terminal behavior. D-edge-cascade-invariant is upstream of EM-046a: no-match is declared *only after* the fallback pool is also exhausted.
- **D5** dialect produces the boolean truth values consumed at step (a). The invariant does not constrain D5.
- **D4** whitelist defines what step-(a) conditions may reference. The invariant does not constrain D4.
- **D-attractor-adoption** treats this invariant as one of the five items adopted verbatim from Attractor — specifically the audit-V3.2 fix.

## Implications for pass-5 spec-draft

- EM-041 prose should be amended to spell out the 5 steps explicitly (currently named but not enumerated in spec) and append the unconditional-edge invariant as a sub-clause.
- The audit-V3.2 rationale should be cited in EM-041's commentary section — not as a normative bullet but as a "why this exists" note so future maintainers don't accidentally simplify the invariant away.
- A scenario-harness test (G2 cluster) must exist for the fallback case. C5 example directory should include a tiny `unconditional-fallback.dot` if not implied by other examples.

## Open follow-ups

1. **Empty-condition vs. absent-condition syntactic equivalence.** A DOT edge with `condition=""` vs. no `condition` attribute at all — are both "unconditional"? Lean: yes, both treated identically. C2 parser owns normalization.
2. **Multiple unconditional edges from the same source.** Permitted; the cascade enters step (b)+ with the unconditional pool as candidates. Existing tiebreak logic resolves. No special handling.
3. **Interaction with EM-046a's structural-failure reason string.** When the fallback pool is also empty, EM-046a raises with reason `no_outgoing_edge_matches`. Should the reason distinguish "no conditional matched and no unconditional exists" from "no edges at all"? Lean: single reason string at v1; observability question for pass-5.
