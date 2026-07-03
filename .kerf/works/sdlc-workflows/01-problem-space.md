# Pass 1 — Problem Space

## Summary

The phase-3-dot work delivered harmonik's workflow-graph (DOT) runtime: a parser, validator,
cascade dispatcher, and two pinned examples (`specs/examples/review-loop.dot`,
`review-loop-finalize.dot`). The runtime is proven live end-to-end (HANDOFF v69). What is missing
is a **corpus of worked SDLC workflows** that exercises the runtime across the whole software
lifecycle — planning, spec authoring, decomposition, implementation, review, testing, debugging,
release — and that doubles as a regression-test bank and a demo surface.

This work lands the FINAL SDLC corpus (`/tmp/sdlc-corpus/_final.md`, 21 workflows) as
`specs/examples/<name>.dot` fixtures plus scenario tests, following the discipline in
`specs/examples/README.md` (each example needs a pinning spec section + a scenario test asserting
the terminal reached and the emitted event/edge sequence). It is a **planning** work: it decides
which workflows land, in what order, which are runnable today vs. which need a capability bead, and
proposes the implementation bead set. It does NOT itself modify `specs/` — that happens at
implementation time, one bead per workflow.

## Goals

- Land the 14 NOW workflows as `specs/examples/<name>.dot` fixtures, each with a pinning spec
  section and a scenario test (terminal-node assertion + edge/event sequence), establishing the
  corpus as the canonical SDLC-workflow demonstration bank.
- Land the 5 SOON workflows once their capability beads ship (`hk-l8rpd` tool/shell node,
  `hk-sdnzj` per-node prompt, `hk-69asi` non-committing agentic, `hk-karlz` gate evaluator), each
  gated on its dependency.
- Land the 2 whole-SDLC DEMO arcs (D1 all-NOW, D2 post-parity) as showcase fixtures / acceptance
  targets.
- Prove the **marquee consolidation family** (`dual-review-consolidate`,
  `triple-review-consolidate`, `two-reviewer-consensus`) runs on today's engine via the reviewer
  brief discipline (write+commit `reviews/reviewer-*.md` FIRST, then write `.harmonik/review.json`).
- Produce a smoke-first landing order: `dual-review-consolidate`, `implement-review-fix`,
  `plan-review-loop`, `security-review-loop`.

## Non-goals

- **No new runtime capability** is built here. The SOON workflows wait on existing capability beads
  (`hk-l8rpd`/`hk-sdnzj`/`hk-69asi`/`hk-karlz`); this work does not implement them.
- **No parallel fan-out / join.** `parallel-review-consolidate` is DEFERRED (EM-059) and recorded
  in the corpus as a design target only — NOT filed as a bead.
- **No spec-dialect change.** Every NOW workflow obeys the proven v1 dialect; if a workflow needed
  a dialect extension it would be SOON or DEFERRED, not NOW.
- **No repo edits during planning.** The corpus lives in `/tmp`; the kerf artifacts live on the
  bench. Repo `specs/` edits happen per-bead at implementation time.
- This work does not re-derive the corpus — the 7 slice files, parity research, and two completed
  reviews are inputs; the FINAL corpus is the authoritative artifact.

## Constraints

- **Dialect contract (closed).** Agentic vs. non-agentic node shapes; the edge-condition LHS
  whitelist (`outcome.status|preferred_label|failure_class|kind`, `context.<key>`); operators
  `==`/`!=`/`&&` only; mandatory unconditional fallback edge declared LAST; `traversal_cap` bounds
  loops and cap-hit reopens as needs-attention; terminals classified by node identity
  (`close-needs-attention` ⇒ needs-attention, any other ⇒ SUCCESS). Sequential walk only.
- **`specs/examples/` discipline (README).** An example must be pinned by a normative spec section,
  carry inline DOT comments on every non-terminal node + conditional edge, get a README subsection
  (Purpose / Schema version / Spec anchors / Gap coverage / Test surface), be picked up by the
  Layer-1 static round-trip walk, and have ≥1 Layer-2 scenario test with a golden trace.
- **Reviewer single-slot.** `.harmonik/review.json` is overwritten on each reviewer launch
  (`reviewverdict.go:528`); the consolidation family works around this with the commit-first brief
  discipline. A reviewer that writes no `review.json` errors.
- **Capability dependencies.** SOON beads MUST declare a dependency on their capability bead so
  they are not dispatched before the capability lands.

## Success criteria

- A new agent can read the FINAL corpus + these kerf artifacts and land any single workflow
  (fixture + spec section + scenario test) without re-deriving design.
- The 14 NOW workflow `.dot` files round-trip through the C2 validator (Layer-1) and each has a
  scenario test asserting the terminal reached and the edge/event sequence for at least the
  APPROVE, REQUEST_CHANGES-loop, BLOCK, and (where present) cap-hit-fallback paths (Layer-2).
- `dual-review-consolidate` smokes live first and confirms reviewer-committed findings are readable
  by the consolidate node; on confirmation `triple-review-consolidate` / `two-reviewer-consensus`
  land as plain NOW.
- The proposed bead set is one bead per workflow (21) + a parent epic, with NOW=P1, DEMO=P1,
  SOON=P2-with-capability-dependency, all labelled `codename:sdlc-workflows`, ordered smoke-first.
