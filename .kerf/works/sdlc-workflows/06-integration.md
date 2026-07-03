# Pass 6 — Integration

## How the components compose

The five components share one substrate (the DOT runtime) and one discipline
(`specs/examples/README.md`). They integrate along three edges, all forming a DAG:

1. **C1 → C2 (marquee gate).** C1's `dual-review-consolidate` live smoke (S3) confirms the
   reviewer-commit channel. C2's marquee fixtures (`triple-review-consolidate`,
   `two-reviewer-consensus`) consume that confirmation. Integration concern: do NOT dispatch the
   C2 marquee fixtures before the C1 smoke resolves. Sequencing is enforced by the smoke-first bead
   order (S3 + the implementation batch in 07-tasks).
2. **C1 + C2 → C5/D1 (composition).** D1 (`plan-to-shipped-now`) composes the loops from
   `plan-review-loop`, a spec gate, `decompose-review-load`, the marquee consolidate, and
   `docs-sync`. Integration concern: D1's node names + briefs must stay consistent with the
   composed NOW fixtures so the demo is a faithful chain, not a divergent re-draft.
3. **C4 → C5/D2 (capability composition).** D2 (`plan-to-shipped-faithful`) composes the SOON
   forms (tool `br` steps, green-build gate) + the marquee consolidate. Integration concern: D2 and
   the SOON beads share `hk-l8rpd`; D2's dependency edge is the union of #18's deps.

## Shared-state / contract consistency

- **`.harmonik/review.json` (single-slot).** Every reviewer node — per-axis and consolidate —
  writes it; only the last (consolidate) verdict is read by the branch edge. This contract is
  identical across C1 and C2's consolidation fixtures and D1/D2's consolidate stage. No contradiction.
- **Terminal naming.** Most workflows use `close` / `close-needs-attention`; the planning loops use
  `plan-approved` / `plan-needs-attention`; `sentry-triage-faithful` uses `close` / `close-skip`.
  Each is declared in that workflow's `terminal_node_ids` and classified by identity
  (`*-needs-attention` ⇒ needs-attention; all others ⇒ SUCCESS). Consistent with the hk-z03e8 rule.
- **Cap-hit semantics.** Uniform across all looped workflows: cap-hit returns a Failed decision and
  the run reopens needs-attention; it does NOT fall through to the unconditional fallback. The
  scenario tests assert this uniformly (S2).
- **Capability-dependency edges.** The S5 map is the single source of truth; D2's deps = #18's deps.
  No SOON/DEMO bead carries a dependency not in S5.

## Initialization / ordering

- **Within a session/batch:** smoke-first order (C1) then C2-non-marquee, then C2-marquee (after
  smoke), then C5/D1; C4 + C5/D2 land as their capability beads ship.
- **No runtime init coupling** — these are static fixtures + tests; there is no daemon startup
  ordering concern. The only ordering is the bead-dispatch order, owned by the orchestrator.

## Cross-component error handling

- **Smoke failure (C1 S3).** If the reviewer-commit channel fails the smoke, the contingency
  (implementer-class reviewers) applies to ALL consolidation fixtures (C1+C2) and D1/D2's
  consolidate stage uniformly — a single decision propagates. File the gap; do not land the marquee
  as plain NOW until resolved.
- **Validator rejects tool-node attrs (C4 Q4).** Contained to C4/D2; NOW work (C1/C2/C5-D1)
  unaffected. The bead waits on `hk-l8rpd`.

## SPEC.md assembly note

`SPEC.md` assembles S1–S7 (the change spec) + the S5 capability map + the smoke-first order into a
single normative landing document, adding NO new requirements. It is the artifact a per-workflow
implementation bead reads alongside the FINAL corpus.
