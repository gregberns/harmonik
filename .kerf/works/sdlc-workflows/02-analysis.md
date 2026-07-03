# Pass 2 — Analysis

## What exists today (the starting point)

- **DOT runtime, proven live.** `internal/workflow/dot/{parser,edges,validator}.go`,
  `internal/daemon/dot_cascade.go`, `internal/workspace/reviewverdict.go`. The `review-loop`
  topology runs end-to-end (HANDOFF v69; 5 blockers fixed, hk-z03e8 terminal-by-identity fix
  landed).
- **Two pinned examples.** `specs/examples/review-loop.dot` (the canonical implementer↔reviewer
  loop) and `review-loop-finalize.dot` (terminal-by-identity through a non-agentic intermediate).
  Both are pinned by `specs/execution-model.md §EM-015d/e` and `specs/workflow-graph.md §8`.
- **Test scaffolding.** `internal/workflow/scenario_reviewloop_full_hkisp3y_test.go` and
  `scenario_roundtrip_em75_hklphyf_test.go` drive `workflow.LoadDotWorkflow` + `DecideNextNode`
  with synthetic `core.Outcome` values across the five named review-loop scenarios, asserting both
  the next-node decision and the terminal classification. This is the exact pattern every new
  scenario test follows.
- **Examples discipline.** `specs/examples/README.md` — the normative checklist for adding an
  example (pinning section, inline comments, README subsection, Layer-1 static round-trip,
  Layer-2 scenario test with golden trace, reviewer approval).
- **The FINAL corpus.** `/tmp/sdlc-corpus/_final.md` — 21 workflows, all dialect-validated, with
  complete drop-in DOT for every NOW workflow + the brief-discipline notes.

## The shape of the work

This is fundamentally a **transcription + test-authoring** effort, repeated per workflow, NOT a
design effort (design is done; the corpus is FINAL). Each workflow bead is:

1. Add `specs/examples/<name>.dot` (the drop-in DOT from the corpus, with inline comments per the
   README discipline).
2. Add a pinning spec section — a subsection in `specs/examples/README.md` (Purpose / Schema
   version / Spec anchors / Gap coverage / Test surface) and, where the workflow demonstrates a
   normative claim not already pinned, a one-line reference from `specs/workflow-graph.md` or
   `execution-model.md`.
3. Add a scenario test (`internal/workflow/scenario_<name>_<bead>_test.go`) asserting the terminal
   reached and the edge sequence for the relevant paths (APPROVE / REQUEST_CHANGES-loop / BLOCK /
   cap-hit-fallback / custom-label / failure-class as applicable).

Layer-1 (static round-trip) is automatic once the `.dot` lands under `specs/examples/` — the
directory walk picks it up. Layer-2 (scenario) is per-workflow test code.

## Three categories of test goal (from the brief)

1. **Tests exercising TODAY's functionality.** The 14 NOW workflows. These land first; their
   scenario tests run green on the current engine. The consolidation family (#2/#3/#4) is in this
   category *because of the marquee brief discipline* — reviewers commit findings, no `hk-69asi`.
2. **Tests enabled once parity beads land.** The 5 SOON workflows. Their `.dot` files use tool/gate
   node types or per-node prompts that the validator may accept (parses) but the cascade cannot run
   until the capability bead ships. Each SOON bead depends on its capability bead. Some (e.g.
   #19 `quality-gate-policy`) *parse* today and can land a Layer-1 round-trip + a
   parse-only-asserting scenario test now, with the live-run test gated on the capability.
3. **Whole-SDLC demos.** D1 (`plan-to-shipped-now`, all-NOW topology, in-session build gate) and
   D2 (`plan-to-shipped-faithful`, post-parity). These are long showcase arcs, landed as fixtures
   + a scenario test walking the happy path and at least one escalation.

## Risk analysis

- **Reviewer-commit channel (the one real unknown).** The marquee depends on a reviewer-class node
  being able to durably commit `reviews/reviewer-*.md` AND on the consolidate node reading it. The
  brief discipline asserts this works (reviewers don't require HEAD-advance but MAY commit; the
  commit survives the worktree/merge flow). **Mitigation: smoke `dual-review-consolidate` live
  FIRST.** If the commit channel does not work, the fallback is to model reviewers as
  implementer-class (loses the clean `review.json → preferred_label` wiring) or to pull `hk-69asi`
  forward — but this is judged unlikely. This is why `dual-review-consolidate` is the first
  smoke-first bead.
- **Failure-class branches can't be forced live.** #12 `review-route-by-failure-class` routes on
  `outcome.failure_class`, which an agent cannot be reliably made to emit. The scenario test drives
  these branches with synthetic outcomes (the existing test pattern already does exactly this);
  deterministic *live* branch coverage waits on the `hk-1xsyu` stub handler. So #12's scenario test
  lands NOW (synthetic), live-branch coverage is a follow-up.
- **Scope creep into the corpus.** The corpus is FINAL; beads transcribe, they do not redesign. A
  bead that wants to change a workflow's topology is a signal to revisit the corpus, not to
  free-hand the fixture.
- **DEMO arcs are long.** D1/D2 are 12–14 node graphs. The scenario test walks the path with
  synthetic outcomes, so it is not gated on a live multi-hour agentic run; the live walk is a
  demo/acceptance activity, not a CI gate.

## Why this order

Smoke-first batch = `dual-review-consolidate`, `implement-review-fix`, `plan-review-loop`,
`security-review-loop`. Rationale: `implement-review-fix` is near-isomorphic to the proven
`review-loop.dot` (zero-risk baseline); `dual-review-consolidate` resolves the one real unknown
(reviewer-commit channel) at the smallest scale; `plan-review-loop` and `security-review-loop` are
both #1 re-roled (low risk) and broaden phase/axis coverage immediately. Everything else extends
from these four.
