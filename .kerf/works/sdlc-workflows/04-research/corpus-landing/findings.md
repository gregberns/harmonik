# Pass 4 — Research: corpus landing

Research is consolidated across the five components (C1–C5) because they share one execution
substrate (the DOT runtime) and one landing discipline (`specs/examples/README.md`). The questions
below are the load-bearing unknowns; the rest of the corpus's design research already lives in the
7 slice files + the parity research + the two completed reviews and is not re-derived here.

## Q1 — Can a reviewer-class node durably commit findings, and can a later node read them?

**Why it matters.** The entire consolidation family (C1's `dual-review-consolidate`, C2's
`triple-review-consolidate` / `two-reviewer-consensus`, D1/D2's consolidate stage) depends on the
answer. `.harmonik/review.json` is single-slot — overwritten on every reviewer launch
(`internal/workspace/reviewverdict.go:528`), so only the LAST verdict survives in
`outcome.preferred_label`. Per-axis reviewer findings must travel through a durable channel.

**Findings.**
- The HEAD-advance gate ("implementer didn't advance HEAD") in `internal/daemon/dot_cascade.go`
  applies to **implementer-class** agentic nodes only. A **reviewer-class** node
  (`agent_type="reviewer"`, `idempotency_class="idempotent"`) SUCCEEDS by writing `review.json`; it
  does NOT require HEAD to advance. Therefore a reviewer MAY commit a findings file without
  tripping a gate.
- A reviewer that writes NO `review.json` **errors** — `review.json`'s appearance is what triggers
  `/quit`. So every reviewer node (per-axis AND consolidate) must write a `review.json`, even
  though only the consolidate's verdict is read by the branch edge.
- **Evidence basis.** Corpus §"Marquee brief discipline" verified against the engine read in the
  slices; `reviewverdict.go:528` single-slot behavior; `dot_cascade.go` agentic-completion check.

**Options / tradeoffs.**
- **(A) Commit-first brief discipline (chosen).** Reviewer brief: write+commit
  `reviews/reviewer-<axis>.md` FIRST, then write `.harmonik/review.json`. Consolidate reads the
  files, severity-joins, writes the final `review.json`. Pros: no new capability; keeps the clean
  `review.json -> preferred_label` wiring. Cons: relies on the reviewer commit surviving the
  worktree/merge flow — **must be smoke-confirmed**.
- **(B) Model reviewers as implementer-class.** Pros: commit is mandatory (guaranteed durable).
  Cons: loses the `review.json -> preferred_label` wiring; the consolidate would have to re-derive
  verdicts from commit content. Rejected unless (A) fails the smoke.
- **(C) Pull `hk-69asi` forward.** Non-committing agentic with a writable channel. Rejected — not
  needed if (A) works; reserved for SOON zero-commit nodes only.

**Risk / mitigation.** The one real unknown. **Mitigation: `dual-review-consolidate` is the first
smoke-first bead** (cap=2 -> cap-hit reachable fast). If the reviewer commit is not durable, fall
back to (B) for the marquee and file the gap.

## Q2 — What is the exact scenario-test pattern a new fixture must follow?

**Findings.** `internal/workflow/scenario_reviewloop_full_hkisp3y_test.go` is the template:
- `LoadDotWorkflow(<repoRoot>/specs/examples/<name>.dot)` then drive `workflow.DecideNextNode(graph,
  fromNode, outcome, run, cycles)` step by step.
- Build outcomes with a helper: `core.Outcome{Status, Kind: OutcomeKindDefault, PreferredLabel}`.
- Assert `dec.Advance` and `dec.NextNodeID` at each hop, and the terminal classification at the end.
- A `core.NewCycleCounter()` drives `traversal_cap` enforcement — cap-hit returns a Failed decision
  (run reopens needs-attention), it does NOT fall through to the fallback.
- The static Layer-1 round-trip is automatic via the `specs/examples/` directory walk
  (`scenario_roundtrip_em75_*` / the examples walk) — no test code needed for Layer-1.

**Conclusion.** Every workflow bead authors one `scenario_<name>_<bead>_test.go` in
`internal/workflow/` (package `workflow_test`), copying this shape with the workflow's node names
and paths. Golden-trace files (if used) live under the test-package testdata, NOT under
`specs/examples/`.

## Q3 — Does each example need a NEW normative spec section, or does the README subsection suffice?

**Findings.** `specs/examples/README.md` step 1 requires an example be "referenced by name from at
least one normative section in `specs/`." The README subsection itself (Purpose / Schema version /
Spec anchors / Gap coverage / Test surface) is the per-example pin, and the Spec-anchors block
traces every attribute/node-type/edge-LHS the example uses back to an existing
`workflow-graph.md` / `execution-model.md` / `handler-contract.md` clause.

**Conclusion.** For the corpus, **most workflows reuse the same dialect surface** already pinned by
`review-loop.dot` (verdict routing, traversal_cap, terminal-by-list, unconditional fallback). So
the per-workflow obligation is: (a) add the README subsection with the Spec-anchors trace, and
(b) add a one-line "demonstrates" reference from the nearest normative section ONLY when the
workflow exercises a claim not already pinned (e.g. WG-019 arbitrary `preferred_label` for
`docs-sync`'s `CODE_CHANGE`; `failure_class` routing for #12). No new WG-/EM- IDs are minted; this
is reference-wiring, not dialect change.

## Q4 — Which SOON workflows can land a Layer-1 round-trip NOW vs. only after the capability?

**Findings.**
- `quality-gate-policy` (#19) — the `gate` node type **parses** today (validator accepts
  `gate_ref`+`handler_ref`); the cascade errors at run. So its `.dot` can land + a Layer-1
  round-trip test + a scenario test asserting the reviewer cascade portion, with the gate-routing
  live test gated on `hk-karlz`.
- `green-build-merge-gate` / `regression-gate` / `release-with-rollback` (#15–17) — use
  `type="tool"`, `handler_ref="shell"`, `tool_command`. Whether the validator accepts these
  attributes today is to be confirmed at implementation; if it rejects unknown node-type/attrs,
  the `.dot` lands when `hk-l8rpd` adds them. Each bead depends on `hk-l8rpd`.
- `sentry-triage-faithful` (#18) — uses `non_committing`, `class`, inline `prompt` attrs; depends
  on all three of `hk-l8rpd`+`hk-sdnzj`+`hk-69asi`. Lands fully only post-parity.

**Conclusion.** SOON beads are filed with explicit dependency edges (C4). Each can land its fixture
the moment its keystone capability ships; #19 may land a partial (parse + reviewer-cascade) test
earlier.

## Q5 — Are the DEMO arcs CI-gated or demo-only?

**Findings.** D1/D2 are 12–14 node graphs. A live agentic walk is multi-hour and not a CI gate.
The scenario test (synthetic outcomes via `DecideNextNode`, same as Q2) walks the happy path +
>=1 escalation and IS a CI gate (cheap, deterministic). The live walk is an acceptance/demo activity.
D1's in-session build gate (review fix #2) is encoded in node *briefs* — the scenario test asserts
the topology routes a BLOCK from `consolidate`/`docs_review` to needs-attention; the actual
`go build`/`go test` execution happens only in a live walk.

## Unknowns carried forward

- The reviewer-commit-channel confirmation (Q1) — resolved by the `dual-review-consolidate` smoke,
  which is gated as the first implementation bead.
- Tool-node attribute acceptance by today's validator (Q4) — resolved at `hk-l8rpd` implementation;
  does not block NOW work.
