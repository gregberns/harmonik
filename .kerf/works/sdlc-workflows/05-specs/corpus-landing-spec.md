# Pass 5 — Change Spec: corpus landing

This change spec is the normative landing recipe applied per workflow. Source: `/tmp/sdlc-corpus/_final.md`
(FINAL corpus, 21 workflows). It governs every implementation bead in the `codename:sdlc-workflows`
set. It does NOT mint new dialect (no new WG-/EM- IDs); it lands fixtures + tests + reference-wiring
under the existing `specs/examples/README.md` discipline.

## S1 — Per-workflow landing unit (applies to all 21 beads)

Each workflow bead MUST, in one commit (or a tight series), produce:

1. **Fixture.** `specs/examples/<name>.dot` — the drop-in DOT from the FINAL corpus for `<name>`,
   with inline DOT comments on every non-terminal node (naming its role) and every conditional edge
   (naming the dialect rule / spec clause it derives from), per `specs/examples/README.md` step 2.
   `<name>` is the corpus `workflow_id` (e.g. `dual-review-consolidate`).
2. **README subsection.** Append a subsection to `specs/examples/README.md` matching the
   `review-loop.dot` structure: Purpose / Schema version (=1) / Spec anchors / Gap coverage /
   Test surface. The Spec-anchors block MUST trace every attribute, node-type, and edge-condition
   LHS used to an existing normative clause.
3. **Reference-wiring (conditional).** When the workflow demonstrates a normative claim NOT already
   pinned by `review-loop.dot`, add a one-line "demonstrated by `specs/examples/<name>.dot`"
   reference from the nearest existing section:
   - `docs-sync` → WG-019 (arbitrary `preferred_label` string; the `CODE_CHANGE` label).
   - `review-route-by-failure-class` → the `outcome.failure_class` LHS clause.
   - `plan-review-finalize` → the hk-z03e8 terminal-by-identity clause (alongside
     `review-loop-finalize.dot`).
   - `dependency-cycle-fix-loop` → custom non-verdict labels (`CYCLE`/`ACYCLIC`) under WG-019.
   Others reuse `review-loop.dot`'s anchors; no new reference needed.
4. **Scenario test.** `internal/workflow/scenario_<name>_<beadshort>_test.go` (package
   `workflow_test`), following the `scenario_reviewloop_full_hkisp3y_test.go` template: load via
   `workflow.LoadDotWorkflow`, drive `workflow.DecideNextNode` with synthetic `core.Outcome`s,
   assert `dec.Advance`/`dec.NextNodeID` at each hop and the terminal classification at the end.
   Helper-prefix discipline per `implementer-protocol.md`.

**Acceptance (per bead).**
- `go test ./internal/workflow/...` passes including the new scenario test.
- The new `.dot` is picked up by the Layer-1 directory walk and round-trips through the C2
  validator with no error.
- `specs/examples/README.md` has the new subsection; every attribute/LHS used traces to an anchor.

## S2 — Scenario-test path obligations (per workflow class)

The scenario test MUST assert, at minimum, these paths for the workflow's class:

- **Verdict-loop workflows** (`implement-review-fix`, `plan-review-loop`, `security-review-loop`,
  `characterize-refactor-verify`): APPROVE→success-terminal; 1–2×REQUEST_CHANGES→loop→APPROVE;
  BLOCK→needs-attention; cap-hit→Failed-decision (run reopens needs-attention; NOT fallthrough).
- **Consolidation workflows** (`dual-review-consolidate`, `triple-review-consolidate`,
  `two-reviewer-consensus`): the linear reviewer spine advances unconditionally to `consolidate`;
  `consolidate` APPROVE→close, REQUEST_CHANGES→implement (respecting the workflow's cap),
  BLOCK→needs-attention. (`two-reviewer-consensus`: assert the AND-rule mapping in the consolidate's
  emitted verdict via the brief — the test drives the consolidate outcome directly.)
- **Commit-gated handoff workflows** (`spec-citation-cleanup`, `decompose-review-load`,
  `spec-R1-R2-cycle`): assert `outcome.status == 'SUCCESS'` advances the author→next-node edge and
  a non-SUCCESS falls through to needs-attention.
- **Custom-label / non-verdict workflows** (`docs-sync`, `dependency-cycle-fix-loop`): assert the
  custom label routes (`CODE_CHANGE`→change_code; `CYCLE`→fix_cycle; `ACYCLIC`→close) and the
  fallback edge catches an unrecognized label.
- **Failure-class workflow** (`review-route-by-failure-class`): drive each `failure_class` value
  with a synthetic outcome and assert routing (`transient`→self-loop capped; others→needs-attention;
  SUCCESS→reviewer). Live-branch coverage gated on `hk-1xsyu` (test follow-up, not this bead).
- **Terminal-by-identity workflow** (`plan-review-finalize`): assert APPROVE→finalize_plan→
  plan-approved is classified SUCCESS despite the unconditional inbound edge to the terminal.

## S3 — Marquee smoke obligation (C1 / `dual-review-consolidate`)

Before `triple-review-consolidate` / `two-reviewer-consensus` land, a **live smoke** of
`dual-review-consolidate` (cap=2) MUST confirm: (a) each per-axis reviewer writes+commits
`reviews/reviewer-<axis>.md` and that commit survives the worktree/merge flow; (b) the
`consolidate` reviewer reads those files and writes a final `.harmonik/review.json` whose verdict
drives the branch edge. On confirmation, `triple`/`consensus` land as plain NOW. On failure, the
fallback is implementer-class reviewers (S1 unchanged; reviewer briefs re-roled) and a filed gap.

## S4 — SOON gating (C4)

Each SOON bead (`green-build-merge-gate`, `regression-gate`, `release-with-rollback`,
`sentry-triage-faithful`, `quality-gate-policy`) MUST carry a `br dep` edge on its capability bead
(S5 map). Its fixture lands when the capability ships. `quality-gate-policy` MAY land a partial
Layer-1 round-trip + reviewer-cascade scenario test before `hk-karlz`, with the gate-routing live
assertion gated on the capability.

## S5 — Capability-dependency map

| SOON / DEMO workflow | depends on |
|---|---|
| `green-build-merge-gate` | `hk-l8rpd` |
| `regression-gate` | `hk-l8rpd` |
| `release-with-rollback` | `hk-l8rpd` |
| `sentry-triage-faithful` | `hk-l8rpd`, `hk-sdnzj`, `hk-69asi` |
| `quality-gate-policy` | `hk-karlz` |
| `plan-to-shipped-faithful` (D2) | `hk-l8rpd`, `hk-sdnzj`, `hk-69asi` |

`plan-to-shipped-now` (D1) and all 14 NOW workflows carry NO capability dependency.

## S6 — DEMO arcs (C5)

D1 `plan-to-shipped-now`: land the all-NOW fixture; its `consolidate` + `docs_review` node briefs
encode the in-session green-build gate (`go build ./... && go test ./...`, BLOCK on red — review
fix #2). Scenario test walks the happy arc + one BLOCK escalation. D2 `plan-to-shipped-faithful`:
land post-parity (S4 deps); the north-star acceptance fixture.

## S7 — Out of scope (explicit)

- `parallel-review-consolidate` — DEFERRED (EM-059). Recorded in the corpus only; NO bead.
- No new runtime capability, no dialect change, no `hk-l8rpd`/`hk-sdnzj`/`hk-69asi`/`hk-karlz`
  implementation here.
