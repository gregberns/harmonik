# SPEC — sdlc-workflows: land the SDLC corpus as specs/examples fixtures + scenario tests

> Assembled from `05-specs/corpus-landing-spec.md` (S1–S7) + `06-integration.md`. Adds no new
> requirements. The authoritative workflow definitions live in `/tmp/sdlc-corpus/_final.md`
> (FINAL corpus, 21 workflows: 14 NOW, 5 SOON, 2 DEMO + 1 DEFERRED-recorded-only).

## Goal

Land the FINAL SDLC corpus as `specs/examples/<name>.dot` fixtures, each with a pinning README
subsection and a scenario test, establishing harmonik's canonical SDLC-workflow demonstration +
regression bank. One bead per workflow. No new runtime capability, no dialect change.

## Per-workflow landing unit (S1)

Each workflow bead produces, in one commit (or tight series):
1. `specs/examples/<name>.dot` — drop-in DOT from the corpus, inline-commented per the README.
2. A `specs/examples/README.md` subsection (Purpose / Schema version=1 / Spec anchors / Gap
   coverage / Test surface), tracing every attribute/node-type/edge-LHS to an existing clause.
3. Reference-wiring from the nearest normative section ONLY for claims not already pinned by
   `review-loop.dot` (`docs-sync`→WG-019; `review-route-by-failure-class`→failure_class LHS;
   `plan-review-finalize`→hk-z03e8 terminal-by-identity; `dependency-cycle-fix-loop`→custom labels).
4. `internal/workflow/scenario_<name>_<beadshort>_test.go` (package `workflow_test`) following the
   `scenario_reviewloop_full_hkisp3y_test.go` template (`LoadDotWorkflow` + `DecideNextNode` +
   `NewCycleCounter`).

**Acceptance per bead:** `go test ./internal/workflow/...` green incl. the new test; the `.dot`
round-trips through the C2 validator (Layer-1 walk); README subsection present with full anchor trace.

## Scenario-test path obligations (S2)

- Verdict-loop: APPROVE→success; REQUEST_CHANGES→loop→APPROVE; BLOCK→needs-attention; cap-hit→Failed.
- Consolidation: reviewer spine advances unconditionally to consolidate; consolidate verdict routing.
- Commit-gated handoff: `outcome.status == 'SUCCESS'` advances; non-SUCCESS→needs-attention.
- Custom-label / non-verdict: custom label routes; fallback catches unrecognized labels.
- Failure-class: synthetic per-class routing now; live-branch coverage gated on `hk-1xsyu`.
- Terminal-by-identity: APPROVE→finalize→terminal classified SUCCESS.

## Marquee smoke obligation (S3)

`dual-review-consolidate` (cap=2) smokes LIVE first and confirms: per-axis reviewers write+commit
`reviews/reviewer-<axis>.md` (commit survives worktree/merge); consolidate reads them and writes the
final `.harmonik/review.json` driving the branch. On confirmation, `triple-review-consolidate` /
`two-reviewer-consensus` land as plain NOW. On failure: implementer-class reviewers + filed gap.

## SOON gating + capability-dependency map (S4 / S5)

| SOON / DEMO workflow | depends on |
|---|---|
| `green-build-merge-gate` | `hk-l8rpd` |
| `regression-gate` | `hk-l8rpd` |
| `release-with-rollback` | `hk-l8rpd` |
| `sentry-triage-faithful` | `hk-l8rpd`, `hk-sdnzj`, `hk-69asi` |
| `quality-gate-policy` | `hk-karlz` |
| `plan-to-shipped-faithful` (D2) | `hk-l8rpd`, `hk-sdnzj`, `hk-69asi` |

The 14 NOW workflows and D1 carry NO capability dependency.

## DEMO arcs (S6)

D1 `plan-to-shipped-now` — all-NOW topology; `consolidate`+`docs_review` briefs run
`go build ./... && go test ./...` in-session and BLOCK on red (in-session green-build gate, review
fix #2). D2 `plan-to-shipped-faithful` — post-parity north-star, real tool/gate/non-committing nodes.

## Smoke-first implementation order

`dual-review-consolidate` → `implement-review-fix` → `plan-review-loop` → `security-review-loop`,
then `triple-review-consolidate`, then the C2 remainder, then D1, then C4/D2 as capabilities ship.

## Out of scope (S7)

`parallel-review-consolidate` (DEFERRED, EM-059) — recorded only, NO bead. No capability-bead
implementation here.
