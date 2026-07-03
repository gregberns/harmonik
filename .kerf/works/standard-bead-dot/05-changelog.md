# Spec-draft changelog — standard-bead-dot (epic hk-o7j)

Goal of this change: wire the canonical per-bead DOT graph (`standard-bead.dot`)
as the **live default** workflow every dispatched bead runs through, and make the
already-landed sub-workflow node **dispatch** contract normative. The DOT engine
(codename `phase-3-dot`) is already landed; these spec changes document the
remaining default-wiring + sub-workflow-dispatch behavior so the impl beads have a
normative contract to satisfy.

Note on provenance: kerf bench passes 01–04 were an empty skeleton for this work;
the design is embodied in already-landed engine code (`internal/daemon/dot_cascade.go`,
`internal/daemon/moderesolve.go`, `internal/daemon/workloop.go`,
`internal/handler/runtime.go`, `internal/core/subworkflow*.go`) and existing system
specs (execution-model §4.3/§4.8, workflow-graph §4). These drafts make that reality
normative. See the bench gap-map survey (2026-06-11) for the motivating audit.

| Target spec file | Status | What changed | Motivating gap |
|---|---|---|---|
| `specs/sub-workflow-dispatch.md` | **new** | New spec, prefix `SW`. Defines SW-001..SW-010 + SW-INV-001/002: in-place expansion (no new RunID, binds EM-034), node-ID namespacing `<parent>/<sub>` (EM-034a), expansion-time acyclicity reject (EM-034b/WG-029), three-tier graph resolution (WG-006), entered/exited events carrying parent run_id (EM-036), verbatim terminal-outcome escape (EM-036a), SubWorkflowRunner handler boundary, registered-key context discipline (HC), dot/single-only constraint, no review-loop sub-workflows. §Conformance lists 6 impl-test obligations. | Gap-a: sub-workflow node **dispatch** currently stubbed at `dot_cascade.go:523` ("out of scope, separate bead") — the keystone impl. |
| `specs/execution-model.md` | **modified** | Amended §4.3 EM-012a tier-4 built-in fallback `single`→`dot` (embedded `standard-bead.dot`); added EM-012a-FLOOR review-floor (on embedded load failure fall to `review-loop`, never `single`); restatement-sync of EM-012 Run-clause + §6.1 `Run.workflow_mode`; front-matter 0.8.2→0.9.0; §12 revision row. Tiers 1–3 + all EM-034/036 content preserved verbatim. `single` now reachable only via explicit `workflow:single` label. | Gap-c: live-default wiring — make every bead w/o an explicit mode run the standard DOT graph; matches landed `moderesolve.go:100` (returns Dot) + review-floor fallback in `workloop.go`. |
| `specs/workflow-graph.md` | **modified** | Added §17 "Canonical exemplar: standard-bead.dot" (WG-047..WG-052): six-node catalog + type/handler bindings, ten edges + traversal caps (det-fix 3 / transient 2 / REQUEST_CHANGES 3), verdict+failure-class routing inputs, SOLE-inbound-edge-to-`close` review-floor invariant (WG-050), canonical-default cross-ref to EM-012a/EM-055, and a golden-test obligation citing `internal/workflow/scenario_standard_bead_hkp0kum_test.go` (verified to exist). Added §18 Revision History (file had none). All prior content preserved. | Topology contract: pin the canonical default graph + the review-floor invariant the default relies on. |
| `specs/process-lifecycle.md` | **modified** | PL-004a default workflow mode `single`→`dot` (embedded standard-bead.dot) + review-floor (never `single`); PL-005 step-0 cached-default coupled sync; tier-4 restatement updated; cross-ref EM-012a/EM-012a-FLOOR (two-way anchor agreement); ver 0.4.8→0.5.1. | **Integration-pass contradiction (load-bearing):** PL-004a is the tier-3 anchor EM-012a cites BY NAME; it asserted default MUST be `single` → would contradict the flip. |
| `specs/operator-nfr.md` | **modified** | ON-004a default value + tier-4 built-in fallback `single`→`dot` + review-floor; cross-ref EM-012a; ver 0.5.3→0.5.4. | **Integration-pass contradiction:** operator-facing default-mode config asserted `single`. |
| `specs/beads-integration.md` | **modified** | BI-009a resolution-chain tail built-in fallback `single`→`dot` + review-floor; cross-ref EM-012a; ver 0.6.2→0.6.3. | **Integration-pass contradiction:** bead workflow-mode resolution chain ended at `single`. |

> `specs/handler-contract.md` HC-006 was checked (integration pass): its `workflow_mode` presence rule ("present iff non-**default** mode") is default-RELATIVE, not hardcoded `single` — it stays correct after the flip. **No draft-update needed.**

## Cross-reference reconciliation (for the reviewer)

- `sub-workflow-dispatch.md` did not exist when the two UPDATE drafts were authored; they cite it as a forward-reference. It now exists on the bench — confirm all three resolve as a set at finalize. **RESOLVED (Pass 6 / integration):** all three drafts exist on the bench and the forward-refs resolve as a set; verified in `06-integration.md`.
- `workflow-graph.md` WG-006 internally points sub-workflow expansion at `execution-model.md §4.10`; the live anchor is **§4.8** (upstream stale §-number). The new SW spec cites §4.8. Reviewer to confirm whether to also fix WG-006's anchor here. **RESOLVED (Pass 6 nit 3):** WG-006's three `§4.10 EM-034*` anchors retargeted to **§4.8** (the live EM-034-family home, confirmed at EM draft §4.8 line 647 vs §4.10 cascade at line 737). Same-file same-blast-radius repair also fixed an in-WG-006 content swap (namespacing is EM-034a, acyclicity EM-034b).
- SW-003/004/010 expansion-time reject uses the `structural` failure class as the closest published anchor (vs `validation_failed`); reviewer to confirm the intended class. **OPEN — carried to tasks pass.** Reviewer-APPROVED `structural` as the closest published class; the impl/tasks pass should confirm against the landed failure-class enum and flag if a dedicated class is wanted.
- `execution-model.md` tier-4 cross-ref to the workflow-graph "canonical exemplar" should be tightened to the new §17 (WG-047..052) once both land. **RESOLVED (Pass 6 nit 1):** EM-012a tier-4 now points specifically at [workflow-graph.md §17 WG-047..WG-052] as the normative topology contract, retaining the §12 WG-036 cross-ref as a secondary catalog pointer.

## Reviewer-nit reconciliation (Pass 6 / integration — 2026-06-11)

Three reviewer nits applied to the bench drafts (bench-only; no `specs/` or code edits):
1. **EM-012a tier-4 cross-ref tightened** (`execution-model.md`) — now anchors the canonical exemplar at `workflow-graph.md §17 WG-047..WG-052`. RESOLVED.
2. **SW-009 harmonized** (`sub-workflow-dispatch.md`) — §2.1 in-scope one-liner changed from "DOT-mode-only" to "graph-driven-mode (dot + the `single` carve-out, never `review-loop`)" so it agrees with the SW-009 title and §4.6 body. RESOLVED.
3. **WG-006 stale anchor fixed** (`workflow-graph.md`) — `execution-model.md §4.10`→`§4.8` for the EM-034 family (three refs). RESOLVED.

## Validation / acceptance test beads (filed for this change)

Per the spec jig, ≥2 test beads per substantially-changed area are filed before
advancing to Integration (see epic hk-o7j, label `codename:standard-bead-dot`).
