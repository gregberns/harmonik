# Components — standard-bead-dot

## Engine (landed; do not re-implement)

- `internal/daemon/dot_cascade.go` — DOT cascade driver (`driveDotWorkflow`). Contains
  the **sub-workflow dispatch stub at line 523** (the one code gap).
- `internal/daemon/moderesolve.go` — four-tier workflow-mode resolution; tier-4 returns
  `WorkflowModeDot` (line ~101). (landed, hk-30vlb)
- `internal/daemon/workloop.go` — composition-root `workflowModeDefault`; pre-loads the
  embedded `standard-bead.dot` and applies the review-loop floor (~1975-1992); routes
  `WorkflowModeDot` → `driveDotWorkflow` (~2025). Stale comment at line 172.
- `cmd/harmonik/main.go` — CLI default workflow mode = `dot` (~line 568). (landed)

## Sub-workflow plumbing (types landed; dispatch caller missing)

- `internal/handler/runtime.go` — `SubWorkflowRunner` interface + `SubWorkflowRunSpec`
  (parent run reused, no new RunID).
- `internal/core/subworkflowexpansion.go` — in-place expansion + node-ID namespacing
  `<parent>/<sub>`.
- `internal/core/subworkflowenteredpayload.go` / `subworkflowexitedpayload.go` — events
  carrying the parent run_id.
- `internal/core/nodetype.go` — `NodeTypeSubWorkflow`.

## Canonical graph

- `specs/examples/standard-bead.dot` — the canonical 6-node graph.
- `internal/daemon/standard-bead.dot` — build-embedded byte-identical copy.

## Spec files (normative targets of this work)

- `specs/sub-workflow-dispatch.md` — **NEW** (SW-001..010): sub-workflow dispatch contract.
- `specs/execution-model.md` — EM-012a tier-4 default `single`→`dot` + EM-012a-FLOOR; §4.8 EM-034/036 bindings.
- `specs/workflow-graph.md` — §17 canonical `standard-bead.dot` exemplar (WG-047..052) + SOLE-inbound APPROVE→close invariant.
- `specs/process-lifecycle.md` — PL-004a default-mode flip (the tier-3 anchor EM-012a cites).
- `specs/operator-nfr.md` — ON-004a default-value flip.
- `specs/beads-integration.md` — BI-009a resolution-chain-tail flip.

## Tests

- `internal/workflow/scenario_standard_bead_hkp0kum_test.go` — existing golden test (8 `TestSB_*`), covers §17 topology incl. SOLE-inbound invariant.
- New: sub-workflow dispatch unit tests + the 4 filed acceptance beads (hk-982, hk-gwy, hk-x9l, hk-jlp).
