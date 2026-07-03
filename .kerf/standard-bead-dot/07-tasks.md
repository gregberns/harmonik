# Implementation Tasks — standard-bead-dot (epic hk-o7j)

**Pre-screen correction (tasks-pass reviewer, 2026-06-11):** the default-flip
(`workflow_mode` default `single`→`dot`) is **ALREADY LANDED** via hk-30vlb
(`9e4011f8`, `639d71cb`, `811a0012`): `internal/daemon/moderesolve.go:101` tier-4
returns `WorkflowModeDot`, the CLI default `cmd/harmonik/main.go:568` is `dot`,
`daemon.Start` requires an explicit `cfg.WorkflowModeDefault` (zero-value is
fail-closed), and the review-loop floor is in place. The operator's headline goal
("every bead runs standard-bead.dot by default") is therefore satisfied **in code**
already; this kerf work's remaining deliverables are (1) **finalize the 6 spec
drafts** so that landed behavior is normative (the spec lagged the code), (2) the
one genuine code gap — **sub-workflow node dispatch** — and (3) a small
**verification + stale-comment** task. The canonical `standard-bead.dot` graph
(workflow-graph §17) uses only landed node types (no sub-workflow node), so T1 and
T2 are independent.

## Task List

### T1 — Sub-workflow node dispatch (gap-a, KEYSTONE — the one real code gap)

- **What:** Replace the out-of-scope stub at `internal/daemon/dot_cascade.go:523`
  (`NodeTypeSubWorkflow` → returns failure "out of scope (separate bead)") with a
  working dispatch path: resolve the sub-workflow graph (explicit `sub_workflow_ref`
  → project `workflow.dot` → error), validate acyclicity, expand in place with
  node-ID namespacing, call `SubWorkflowRunner.Run` reusing the parent run (NO new
  RunID), emit `sub_workflow_entered`/`sub_workflow_exited` (parent run_id), and
  escape the expanded graph's terminal outcome verbatim to the parent cascade.
- **Spec sections:** sub-workflow-dispatch.md SW-001..SW-010 + SW-INV-001/002 (binds execution-model.md §4.8 EM-034/EM-034a/EM-034b/EM-036/EM-036a; workflow-graph.md WG-006/WG-029; handler-contract HC).
- **Deliverables:** `internal/daemon/dot_cascade.go` dispatch case wired to `internal/handler/runtime.go` `SubWorkflowRunner` + `internal/core/subworkflowexpansion.go`; unit tests for the SW conformance obligations.
- **Acceptance:** the 6 SW §Conformance obligations — (a) in-place expansion, parent run reused (no new RunID); (b) namespacing `<parent>/<sub>`; (c) acyclicity reject at expansion (failure class `structural`); (d) entered/exited events carry parent run_id; (e) terminal outcome escapes verbatim; (f) DOT-mode-only / no review-loop sub-workflows enforced.
- **Depends on:** none.

### T2 — Verify landed dot-default conformance + stale-comment cleanup (gap-c, VERIFICATION ONLY)

- **What:** The default-flip code already shipped (hk-30vlb). This task verifies the
  landed behavior conforms to the now-normative specs and removes stale text. NO new
  default-flip code. (a) Add/confirm tests pinning: unlabeled bead → dot over
  standard-bead.dot; `workflow:single` label overrides; embedded-load failure → 
  `review-loop`, never `single`. (b) Fix the stale comment at
  `internal/daemon/workloop.go:172` ("Start normalises to Single") — it contradicts
  the fail-closed `cfg.WorkflowModeDefault` requirement and the dot default.
- **Spec sections:** execution-model.md §4.3 EM-012a + EM-012a-FLOOR; process-lifecycle.md PL-004a + PL-005 step 0; operator-nfr.md ON-004a; beads-integration.md BI-009a.
- **Deliverables:** verification tests (if not already covered by `scenario_standard_bead_hkp0kum_test.go` / mode-resolution tests); the one-line stale-comment fix at workloop.go:172.
- **Acceptance:** tests assert the landed dot-default + tier-1 override + review-floor; no stale "default = single" text remains in `internal/daemon`.
- **Depends on:** none (independent of T1; different files).

### T3 — scenario test: default-flip is live (bead **hk-982**, scenario-test)

- **What:** End-to-end: an unlabeled bead dispatches via standard-bead.dot DOT mode by default (verifying the landed flip).
- **Spec sections:** execution-model.md §4.3 EM-012a.
- **Acceptance:** run asserts dot-mode dispatch over standard-bead.dot for an unlabeled bead.
- **Depends on:** T2.

### T4 — exploratory test: default + override (bead **hk-gwy**, exploratory-test)

- **What:** Operator surface: daemon default `workflow_mode=dot`; explicit `workflow:single` label still overrides.
- **Spec sections:** EM-012a; ON-004a; BI-009a.
- **Acceptance:** unlabeled bead runs dot; `workflow:single` bead runs single.
- **Depends on:** T2.

### T5 — scenario test: sub-workflow expansion (bead **hk-x9l**, scenario-test)

- **What:** A DOT workflow with a sub-workflow node expands in-place (no new RunID), runs the nested cascade, escapes the terminal outcome.
- **Spec sections:** sub-workflow-dispatch.md SW-001/SW-002/SW-005/SW-006.
- **Acceptance:** nested cascade runs under the parent run_id; namespaced node IDs; entered/exited events; terminal outcome propagates.
- **Depends on:** T1.

### T6 — exploratory test: author + dispatch a sub-workflow graph (bead **hk-jlp**, exploratory-test)

- **What:** Author a `workflow.dot` containing a sub-workflow node and dispatch it end-to-end.
- **Spec sections:** sub-workflow-dispatch.md SW-004 (three-tier resolution) + SW-007 (runner boundary).
- **Acceptance:** a hand-authored graph with a sub-workflow node dispatches and completes via the runner.
- **Depends on:** T1.

## Dependency Graph

```
T1 (sub-workflow dispatch) ──> T5 (scenario sub-wf)  [hk-x9l]
                           └──> T6 (explore sub-wf)   [hk-jlp]

T2 (verify dot-default)    ──> T3 (scenario default)  [hk-982]
                           └──> T4 (explore default)   [hk-gwy]
```

Valid DAG, no cycles. T1 (`dot_cascade.go`) and T2 (`workloop.go` comment + tests)
share no files/symbols — confirmed independent by the tasks-pass reviewer
(`dot_cascade.go` has zero refs to `workflowModeDefault`/`moderesolve`;
`workloop.go`/`moderesolve.go` zero refs to `NodeTypeSubWorkflow`).

## Parallelization Plan

- **Wave 1 (parallel):** T1 ∥ T2 — independent, different files. T2 is the smallest
  (verification + a one-line comment fix) and can land immediately; T1 is the keystone
  capability. Same-package (`internal/daemon`) but different files — per the
  churn/keeper-lane lesson, dispatch one, let it MERGE, then the other, to avoid a
  clean-merge-but-broken-build.
- **Wave 2:** after T1 → T5 ∥ T6; after T2 → T3 ∥ T4.

## Validation / Acceptance Test Tasks

Four test tasks (T3, T4, T5, T6) map 1:1 to the test beads filed in the spec-draft
pass: hk-982 (scenario-test), hk-gwy (exploratory-test), hk-x9l (scenario-test),
hk-jlp (exploratory-test) — all labeled `codename:standard-bead-dot` — each depending
on the impl/verification task it validates. The workflow-graph §17 topology
(WG-047..052) is covered by the existing golden test
`internal/workflow/scenario_standard_bead_hkp0kum_test.go` (8 `TestSB_*` functions,
verified passing).
