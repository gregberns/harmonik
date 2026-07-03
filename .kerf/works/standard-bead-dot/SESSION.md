# Session log — standard-bead-dot (epic hk-o7j)

## 2026-06-11 — crew gurney (standard-bead-dot lane)

Re-tasked from drained churn/keeper lanes to drive `standard-bead-dot` from
spec-draft → finalize. The DOT engine (`phase-3-dot`) is landed; this work makes
the per-bead DOT default + sub-workflow-dispatch contract normative and fills the
one code gap.

- **Survey (read-only):** gap-map of bench + landed engine. Found the kerf bench
  passes 01–04 empty; the design lives in landed code + EM/WG specs.
- **Spec-draft pass (Pass 5):** authored 3 drafts — NEW `sub-workflow-dispatch.md`
  (SW-001..010), UPDATE `execution-model.md` (EM-012a tier-4 single→dot + floor),
  UPDATE `workflow-graph.md` (§17 standard-bead exemplar, WG-047..052). Reviewer
  APPROVE, 0 blockers (preservation/topology/wiring/failure-class verified). Filed
  4 acceptance test beads: hk-982, hk-gwy, hk-x9l, hk-jlp.
- **Integration pass (Pass 6):** cross-spec contradiction sweep caught 3 sibling
  specs still asserting "default = single" — authored consistency flips for
  `process-lifecycle.md` PL-004a (the tier-3 anchor EM-012a cites by name),
  `operator-nfr.md` ON-004a, `beads-integration.md` BI-009a. `handler-contract.md`
  HC-006 checked = benign. Applied 3 reviewer nits. 6-file draft set, consistent.
- **Tasks pass (Pass 7):** reviewer caught that the default-flip is ALREADY LANDED
  (hk-30vlb). Re-scoped: T1 = sub-workflow node dispatch (the one real code gap,
  `dot_cascade.go:523` stub); T2 = verify landed default + stale-comment fix; T3–T6 =
  the 4 test beads. Re-review APPROVE.
- **Finalize:** spec drafts copied into `specs/` on branch `standard-bead-dot`.

Provenance: bench passes 01–04 were backfilled minimally (problem-space, components)
to satisfy `kerf square`; the substantive design is in the landed code + the drafts.
