# specs/examples/ — Canonical Workflow Graphs

## Purpose

This directory holds canonical workflow graphs that harmonik's spec uses as worked examples. Each `.dot` file under `specs/examples/` MUST round-trip through C2's validator and MUST execute cleanly under the C2 dispatch driver against C3's Outcome contract. Examples here are normative in the sense that the spec pins them: a spec section that names an example by filename is asserting that the file is the worked demonstration of that section's claims.

All examples under this directory pin to `schema_version=1` at v1. Mixed-version fixtures (forward-compat probes, drift tests) live under `internal/workflow/testdata/` rather than here; this directory contains specification artifacts only.

## Examples

### `review-loop.dot`

**Purpose.** The implementer-reviewer review loop, expressed as a workflow graph. Demonstrates a cyclic agentic workflow with verdict-based routing and a daemon-enforced iteration cap that drains via an unconditional fallback edge.

**Schema version.** `schema_version=1`.

**Spec anchors.**

- Pinned by `specs/execution-model.md §EM-015d` (review-loop topology) and `§EM-015e` (iteration cap + no-progress detector). The Go-hardcoded review loop in `internal/daemon/workloop.go` is the migration source; this file is the migration target.
- Pinned by `specs/workflow-graph.md §8 WG-021..WG-023` (terminal-node declaration mechanism). The `terminal_node_ids` graph-level attribute and the two terminal nodes (`close`, `close-needs-attention`) demonstrate the WG-021..WG-023 contract.
- Uses `type="agentic"` for the implementer/reviewer nodes and `type="non-agentic"` (with `handler_ref="noop"`) for the entry + terminal nodes — per `specs/workflow-graph.md §4 WG-001/WG-002` (closed node-type enum).
- Uses `start_node="start"` graph-level attribute — per `specs/workflow-graph.md §9 WG-027`.
- Uses `handler_ref` on every node — per `specs/handler-contract.md §4.1 HC-001`.
- Uses `outcome.preferred_label` as an edge-condition LHS — per the D4 LHS whitelist (row 3, `outcome.*`) and per `specs/handler-contract.md §4.2a + §6.1 RECORD Outcome`. Verdict literals are uppercase (`APPROVE` / `REQUEST_CHANGES` / `BLOCK`) per the EM-015d verdict enum and the agent-reviewer JSON schema v1.
- Uses the D5 v1 edge-condition dialect (equality + `&&` only; no `<`/`>`) — per `specs/workflow-graph.md §6 WG-013`.
- Contains a final unconditional `reviewer -> close-needs-attention` edge that satisfies the D-edge-cascade-invariant fallback requirement — per `specs/workflow-graph.md §5 WG-011`.

**Gap coverage.** Closes `G4` (no example `.dot` files in repo) for the highest-leverage migration target. Partially closes `G6` (DOT schema-versioning + repo convention) by establishing `specs/examples/` as the canonical path.

**Test surface.**

- Static round-trip: `internal/workflow/examples_test.go` (loads every `.dot` in this directory and runs it through the C2 validator).
- Scenario harness: `internal/workflow/scenario/review_loop_test.go` (drives mock handler responses against the five scenarios enumerated in C5 design §3.5 — APPROVE-immediately, two-round retry then APPROVE, BLOCK, cap-hit fallback, and no-progress early exit — each asserting both the terminal node reached and the emitted event sequence against a golden trace).

### `implement-review-fix.dot`

**Purpose.** The canonical SDLC implement-review-fix loop. An implementer produces or revises a change; a reviewer renders a verdict (APPROVE / REQUEST_CHANGES / BLOCK); REQUEST_CHANGES loops back to the implementer (capped); BLOCK or cap-hit routes to needs-attention. This is the reference topology that every other NOW workflow in the corpus extends, and it is isomorphic to the hardcoded review loop in `internal/daemon/workloop.go`. Role-variant subsumptions (same topology, different reviewer `role` brief): `plan-single-pass`, `spec-draft-review-approve`, `test-authoring-loop`, `bugfix-loop-now`. The security-axis variant is its own fixture (`security-review-loop.dot`).

**Schema version.** `schema_version=1`.

**Spec anchors.**

- Pinned by `docs/sdlc-workflow-corpus.md §1` (implement-review-fix, the reference topology) and the corpus dialect contract (§"Dialect contract").
- Pinned by `specs/execution-model.md §EM-015d` (review-loop topology) and `§EM-015e` (iteration cap + no-progress detector). The Go-hardcoded review loop in `internal/daemon/workloop.go` is the migration source; this file and `review-loop.dot` are the migration targets.
- Pinned by `specs/workflow-graph.md §8 WG-021..WG-023` (terminal-node declaration mechanism). The `terminal_node_ids` graph-level attribute and the two terminal nodes (`close`, `close-needs-attention`) demonstrate the WG-021..WG-023 contract.
- Uses `type="agentic"` for the `implement`/`review` nodes and `type="non-agentic"` (with `handler_ref="noop"`) for the entry + terminal nodes — per `specs/workflow-graph.md §4 WG-001/WG-002`.
- Uses `start_node="start"` graph-level attribute — per `specs/workflow-graph.md §9 WG-027`.
- Uses `handler_ref` on every node — per `specs/handler-contract.md §4.1 HC-001`.
- Uses `outcome.preferred_label` as an edge-condition LHS — per the D4 LHS whitelist and `specs/handler-contract.md §4.2a + §6.1 RECORD Outcome`. Verdict literals are uppercase (`APPROVE` / `REQUEST_CHANGES` / `BLOCK`) per the EM-015d verdict enum.
- Uses `traversal_cap="3"` on the `review→implement` back-edge — per `specs/workflow-graph.md §6 WG-028` and `specs/execution-model.md §EM-043`.
- Uses the D5 v1 edge-condition dialect (equality + `&&` only) — per `specs/workflow-graph.md §6 WG-013`.
- Contains a final unconditional `review -> "close-needs-attention"` edge satisfying the D-edge-cascade-invariant — per `specs/workflow-graph.md §5 WG-011`.

**Test surface.**

- Static round-trip: `internal/workflow/examples_test.go` (loads every `.dot` in this directory through the C2 validator automatically).
- Scenario harness: `internal/workflow/scenario/implement_review_fix_test.go` (drives mock handler responses against five scenarios — APPROVE-immediately, two-round retry then APPROVE, BLOCK, cap-hit fallback, and unconditional-fallback — each asserting both the terminal node reached and the dispatch decision sequence).

### Future examples

`bead-process.dot` is **deferred** until its prerequisites land (tool-node handler contract, merge-node primitive, sub-workflow composition for review-loop). The candidate follow-up bead is `phase3-bead-process-example`. When the prerequisites land, `bead-process.dot` will be added as a sibling to `review-loop.dot` and will receive its own subsection here.

## How to add a new example

Examples are spec artifacts. Adding one requires both a spec change and reviewer approval; this is not a place to drop one-off `.dot` files for experimentation.

The discipline:

1. **Identify the pinning spec section.** A new example must be referenced by name from at least one normative section in `specs/` (typically in `workflow-graph.md`, `execution-model.md`, or `handler-contract.md`). If no spec section pins it, the example belongs under `internal/workflow/testdata/`, not here.
2. **Author the `.dot` file.** Inline DOT comments on every non-terminal node naming its role are required. Inline comments on every conditional edge naming the D-decision or spec section it derives from are required.
3. **Add a subsection to this README** matching the structure of the `review-loop.dot` subsection above (Purpose / Schema version / Spec anchors / Gap coverage / Test surface). Every attribute, node-type, and edge-condition LHS the example uses must trace to a normative spec anchor.
4. **Wire the static round-trip test.** The example is automatically picked up by `internal/workflow/examples_test.go`'s directory walk; no test code change is required for layer-1 coverage.
5. **Author at least one scenario-harness test** with a golden trace, asserting the terminal-node path and the event sequence. The scenario test must exercise at least one cascade fallback (unconditional edge) if the example has one.
6. **Reviewer approval.** A separate reviewer agent (or fresh-context re-read) must approve the addition before merge. The reviewer's checklist: (a) every attribute used appears in the spec; (b) every edge condition uses only the D5 dialect; (c) every terminal node is declared in `terminal_node_ids`; (d) at least one scenario test exists.

## Validation

Every example in this directory is exercised by two test layers; both are required.

**Layer 1 — static round-trip.** `internal/workflow/examples_test.go` parses every `.dot` file under `specs/examples/` and runs it through the C2 validator. CI fails on any parse error or any validation error (unknown attribute, unknown node-type, malformed edge condition, missing terminal-node declaration, etc.). This layer catches schema drift between the spec and the examples — if C1's vocabulary changes, the static test surfaces every example that drifted.

**Layer 2 — scenario harness with golden trace.** A scenario test per example, written against `specs/scenario-harness.md`, loads the example, drives mock handler responses, and asserts (a) the terminal node reached and (b) the emitted event sequence against a checked-in `expected-trace.golden.jsonl`. Golden-trace files live with the test code under `internal/workflow/scenario/testdata/`, NOT under `specs/examples/` — this directory contains specs, not test fixtures.

The two layers catch disjoint defect classes. Layer 1 catches structural errors; layer 2 catches dispatcher routing errors, cascade-ordering bugs, and terminal-state classification bugs. Both layers are required for every example.
