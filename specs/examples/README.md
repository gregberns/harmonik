# specs/examples/ — Canonical Workflow Graphs

```yaml
---
title: specs/examples/ — Canonical Workflow Graphs
spec-id: examples-readme
status: draft
spec-shape: index
spec-category: foundation-cross-cutting
version: 1.0.0
spec-template-version: 1.1
owner: foundation-author
last-updated: 2026-06-09
---
```

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

### `dual-review-consolidate.dot`

**Purpose.** The two-reviewer consolidation pattern, and the recommended first live smoke of the consolidation family. Two reviewers (correctness + tests, and design) each commit their findings to `reviews/reviewer-*.md` and write `.harmonik/review.json`; a consolidate reviewer severity-joins them and writes the verdict the branch edges read. The smaller cap (=2) makes the cap-hit path reachable quickly, confirming that reviewer-committed findings are readable by the consolidate node before landing the three-reviewer marquee.

**Schema version.** `schema_version=1`.

**Spec anchors.**

- Pinned by `docs/sdlc-workflow-corpus.md §3` (dual-review-consolidate, the everyday MARQUEE smoke) and the corpus dialect contract (§"Dialect contract").
- Uses the marquee brief discipline from `docs/sdlc-workflow-corpus.md §"Marquee brief discipline"`: each reviewer writes+commits `reviews/reviewer-<axis>.md` FIRST, then writes `.harmonik/review.json`; the consolidate node reads all findings files, severity-joins (`BLOCK > REQUEST_CHANGES > APPROVE`), and writes the final verdict.
- Pinned by `specs/workflow-graph.md §8 WG-021..WG-023` (terminal-node declaration). `terminal_node_ids="close,close-needs-attention"` demonstrates the WG-021..WG-023 contract.
- Uses `type="agentic"` with `agent_type="implementer"` for the implement node, `agent_type="reviewer"` for the two per-axis and the consolidate nodes, and `type="non-agentic"` (with `handler_ref="noop"`) for entry + terminal nodes — per `specs/workflow-graph.md §4 WG-001/WG-002`.
- Uses `start_node="start"` — per `specs/workflow-graph.md §9 WG-027`.
- Uses `handler_ref` on every node — per `specs/handler-contract.md §4.1 HC-001`.
- Uses `outcome.preferred_label` as the edge-condition LHS on the consolidate node's outgoing edges — per the D4 LHS whitelist and `specs/handler-contract.md §4.2a + §6.1 RECORD Outcome`.
- Uses `traversal_cap="2"` on the `consolidate→implement` back-edge — per `specs/workflow-graph.md §6 WG-028` and `specs/execution-model.md §EM-043`.
- Uses the D5 v1 edge-condition dialect (equality only) — per `specs/workflow-graph.md §6 WG-013`.
- Contains a final unconditional `consolidate -> "close-needs-attention"` edge satisfying the D-edge-cascade-invariant — per `specs/workflow-graph.md §5 WG-011`.

**Test surface.**

- Static round-trip: `internal/workflow/examples_test.go` (loads every `.dot` in this directory through the C2 validator automatically).
- Scenario harness: `internal/workflow/scenario/dual_review_consolidate_test.go` (drives mock handler responses against scenarios including APPROVE-on-first-pass, cap-hit fallback, and BLOCK escalation, each asserting the terminal node reached and the dispatch decision sequence).

### `triple-review-consolidate.dot`

**Purpose.** The headline three-reviewer consolidation pattern (THE MARQUEE). One implementer, three reviewers on distinct axes (correctness, design/idioms, tests), and a consolidate reviewer that severity-joins (`BLOCK > REQUEST_CHANGES > APPROVE`) their findings. A capped back-edge returns to the implementer until there is nothing left to fix. Each per-axis reviewer writes+commits findings to a durable worktree file before writing `.harmonik/review.json`, so the consolidate node can read all three. Subsumes the review-slice `multi-perspective-code-review`.

**Schema version.** `schema_version=1`.

**Spec anchors.**

- Pinned by `docs/sdlc-workflow-corpus.md §2` (triple-review-consolidate, the MARQUEE) and the corpus dialect contract (§"Dialect contract").
- Uses the marquee brief discipline from `docs/sdlc-workflow-corpus.md §"Marquee brief discipline"`: per-axis reviewers write+commit `reviews/reviewer-<axis>.md` FIRST, then write `.harmonik/review.json`; the consolidate node severity-joins all findings.
- Pinned by `specs/workflow-graph.md §8 WG-021..WG-023` (terminal-node declaration). `terminal_node_ids="close,close-needs-attention"`.
- Uses `type="agentic"` with `agent_type="implementer"` for the implement node, `agent_type="reviewer"` for the three per-axis and the consolidate nodes, and `type="non-agentic"` (with `handler_ref="noop"`) for entry + terminal nodes — per `specs/workflow-graph.md §4 WG-001/WG-002`.
- Uses `start_node="start"` — per `specs/workflow-graph.md §9 WG-027`.
- Uses `handler_ref` on every node — per `specs/handler-contract.md §4.1 HC-001`.
- Uses `outcome.preferred_label` as the edge-condition LHS on the consolidate node's outgoing edges — per the D4 LHS whitelist and `specs/handler-contract.md §4.2a + §6.1 RECORD Outcome`.
- Uses `traversal_cap="3"` on the `consolidate→implement` back-edge — per `specs/workflow-graph.md §6 WG-028` and `specs/execution-model.md §EM-043`.
- Uses the D5 v1 edge-condition dialect (equality only) — per `specs/workflow-graph.md §6 WG-013`.
- Contains a final unconditional `consolidate -> "close-needs-attention"` edge satisfying the D-edge-cascade-invariant — per `specs/workflow-graph.md §5 WG-011`.

**Test surface.**

- Static round-trip: `internal/workflow/examples_test.go` (loads every `.dot` in this directory through the C2 validator automatically).
- Scenario harness: `internal/workflow/scenario/triple_review_consolidate_test.go` (drives mock handler responses against scenarios including APPROVE-on-first-pass, cap-hit fallback, and BLOCK escalation, each asserting the terminal node reached and the dispatch decision sequence).

### `two-reviewer-consensus.dot`

**Purpose.** Unanimous-APPROVE consensus: two independent reviewers each commit their verdicts to `reviews/reviewer-*.md`; a consolidate reviewer computes a boolean AND — APPROVE only if both approved, BLOCK if either blocked, REQUEST_CHANGES otherwise. The canonical answer to "n-of-m consensus" in the sequential v1 dialect: cross-node verdict combination lives inside a consolidate agent because an edge can only see the current node's `outcome.*`. Same single-slot brief discipline as `triple-review-consolidate`.

**Schema version.** `schema_version=1`.

**Spec anchors.**

- Pinned by `docs/sdlc-workflow-corpus.md §4` (two-reviewer-consensus) and the corpus dialect contract (§"Dialect contract").
- Uses the marquee brief discipline from `docs/sdlc-workflow-corpus.md §"Marquee brief discipline"`: reviewer_a and reviewer_b each write+commit their findings FIRST, then write `.harmonik/review.json`; consolidate reads both and applies the AND-of-verdicts rule.
- Pinned by `specs/workflow-graph.md §8 WG-021..WG-023` (terminal-node declaration). `terminal_node_ids="close,close-needs-attention"`.
- Uses `type="agentic"` with `agent_type="implementer"` for implement, `agent_type="reviewer"` for reviewer_a, reviewer_b, and consolidate, and `type="non-agentic"` for entry + terminal nodes — per `specs/workflow-graph.md §4 WG-001/WG-002`.
- Uses `start_node="start"` — per `specs/workflow-graph.md §9 WG-027`.
- Uses `handler_ref` on every node — per `specs/handler-contract.md §4.1 HC-001`.
- Uses `outcome.preferred_label` as the edge-condition LHS on the consolidate node's outgoing edges — per the D4 LHS whitelist and `specs/handler-contract.md §4.2a + §6.1 RECORD Outcome`.
- Uses `traversal_cap="3"` on the `consolidate→implement` back-edge — per `specs/workflow-graph.md §6 WG-028` and `specs/execution-model.md §EM-043`.
- Uses the D5 v1 edge-condition dialect (equality only) — per `specs/workflow-graph.md §6 WG-013`.
- Contains a final unconditional `consolidate -> "close-needs-attention"` edge satisfying the D-edge-cascade-invariant — per `specs/workflow-graph.md §5 WG-011`.

**Test surface.**

- Static round-trip: `internal/workflow/examples_test.go` (loads every `.dot` in this directory through the C2 validator automatically).
- Scenario harness: `internal/workflow/scenario/two_reviewer_consensus_test.go` (drives mock handler responses against scenarios including unanimous APPROVE, dissenting REQUEST_CHANGES, BLOCK escalation, and cap-hit fallback, each asserting the terminal node and dispatch sequence).

### `plan-review-loop.dot`

**Purpose.** The planning analogue of `implement-review-fix`: a `draft_plan` implementer writes or revises `plans/<codename>.md` (commit required each visit), a `plan_review` reviewer gates on scope, grounding, and decomposition quality; REQUEST_CHANGES loops back (capped at 3); BLOCK or cap-hit escalates to needs-attention. Subsumes `plan-single-pass` (cap=0 variant) and `plan-two-round` (cap=2 variant) — the round count is the `traversal_cap` value.

**Schema version.** `schema_version=1`.

**Spec anchors.**

- Pinned by `docs/sdlc-workflow-corpus.md §5` (plan-review-loop, the planning analogue of #1) and the corpus dialect contract (§"Dialect contract").
- Pinned by `specs/execution-model.md §EM-015d` (review-loop topology) and `§EM-015e` (iteration cap + no-progress detector). Applies the same capped back-edge discipline as the canonical review loop, targeting the planning phase.
- Pinned by `specs/workflow-graph.md §8 WG-021..WG-023` (terminal-node declaration). `terminal_node_ids="plan-approved,plan-needs-attention"`.
- Uses `type="agentic"` with `agent_type="implementer"` for `draft_plan` and `agent_type="reviewer"` for `plan_review`, and `type="non-agentic"` (with `handler_ref="noop"`) for entry + terminal nodes — per `specs/workflow-graph.md §4 WG-001/WG-002`.
- Uses `start_node="start"` — per `specs/workflow-graph.md §9 WG-027`.
- Uses `handler_ref` on every node — per `specs/handler-contract.md §4.1 HC-001`.
- Uses `outcome.preferred_label` as the edge-condition LHS — per the D4 LHS whitelist and `specs/handler-contract.md §4.2a + §6.1 RECORD Outcome`.
- Uses `traversal_cap="3"` on the `plan_review→draft_plan` back-edge — per `specs/workflow-graph.md §6 WG-028` and `specs/execution-model.md §EM-043`.
- Uses the D5 v1 edge-condition dialect (equality only) — per `specs/workflow-graph.md §6 WG-013`.
- Contains a final unconditional `plan_review -> "plan-needs-attention"` edge satisfying the D-edge-cascade-invariant — per `specs/workflow-graph.md §5 WG-011`.

**Test surface.**

- Static round-trip: `internal/workflow/examples_test.go` (loads every `.dot` in this directory through the C2 validator automatically).
- Scenario harness: `internal/workflow/scenario/plan_review_loop_test.go` (drives mock handler responses against scenarios including APPROVE-on-first-pass, multi-round REQUEST_CHANGES, BLOCK escalation, and cap-hit fallback, each asserting the terminal node and dispatch sequence).

### `plan-review-finalize.dot`

**Purpose.** Extends `plan-review-loop` with a non-agentic `finalize_plan` node between APPROVE and the success terminal — a future seam for a bead-emitting tool node (`hk-l8rpd`). Validates terminal-by-identity classification through an intermediate non-agentic node: SUCCESS is determined by the terminal node's identity (`plan-approved`), not by the topology of edges leading to it. Isomorphic to `review-loop-finalize.dot` in the planning domain (the hk-z03e8 path).

**Schema version.** `schema_version=1`.

**Spec anchors.**

- Pinned by `docs/sdlc-workflow-corpus.md §6` (plan-review-finalize) and the corpus dialect contract (§"Dialect contract").
- Pinned by `specs/workflow-graph.md §8 WG-021..WG-023` (terminal-node declaration). `terminal_node_ids="plan-approved,plan-needs-attention"` with an intermediate non-agentic `finalize_plan` node between the APPROVE verdict and the terminal — validating that terminal classification is by node identity, not inbound-edge topology (the hk-z03e8 fix).
- Uses `type="agentic"` with `agent_type="implementer"` for `draft_plan` and `agent_type="reviewer"` for `plan_review`, and `type="non-agentic"` (with `handler_ref="noop"`) for entry, `finalize_plan`, and terminal nodes — per `specs/workflow-graph.md §4 WG-001/WG-002`.
- Uses `start_node="start"` — per `specs/workflow-graph.md §9 WG-027`.
- Uses `handler_ref` on every node — per `specs/handler-contract.md §4.1 HC-001`.
- Uses `outcome.preferred_label` as the edge-condition LHS — per the D4 LHS whitelist and `specs/handler-contract.md §4.2a + §6.1 RECORD Outcome`.
- Uses `traversal_cap="3"` on the `plan_review→draft_plan` back-edge — per `specs/workflow-graph.md §6 WG-028` and `specs/execution-model.md §EM-043`.
- Uses the D5 v1 edge-condition dialect (equality only) — per `specs/workflow-graph.md §6 WG-013`.
- Contains a final unconditional `plan_review -> "plan-needs-attention"` edge satisfying the D-edge-cascade-invariant — per `specs/workflow-graph.md §5 WG-011`.

**Test surface.**

- Static round-trip: `internal/workflow/examples_test.go` (loads every `.dot` in this directory through the C2 validator automatically).
- Scenario harness: `internal/workflow/scenario/plan_review_finalize_test.go` (drives mock handler responses against scenarios including APPROVE-through-finalize, BLOCK escalation, and cap-hit fallback, each asserting the terminal node reached via the intermediate non-agentic seam).

### `security-review-loop.dot`

**Purpose.** A re-role of the canonical `implement-review-fix` with a security-axis reviewer brief. Same topology, same dialect, same engine path — the only difference is the reviewer's `role`: instead of a general correctness verdict, the `security_review` node renders a verdict on the security posture of the change (injection, authz, secret handling, unsafe deserialization, supply-chain). BLOCK is the "ship-blocking security defect" escalation. Promoted to its own fixture to make "harmonik can run a security review" a checkable claim rather than a footnote in #1's role-variants note.

**Schema version.** `schema_version=1`.

**Spec anchors.**

- Pinned by `docs/sdlc-workflow-corpus.md §14` (security-review-loop, review fix #1) and the corpus dialect contract (§"Dialect contract").
- Pinned by `specs/execution-model.md §EM-015d` (review-loop topology) and `§EM-015e` (iteration cap + no-progress detector). Identical topology to `implement-review-fix.dot`; the security specialization is entirely in the reviewer node's `role` brief.
- Pinned by `specs/workflow-graph.md §8 WG-021..WG-023` (terminal-node declaration). `terminal_node_ids="close,close-needs-attention"`.
- Uses `type="agentic"` with `agent_type="implementer"` for the implement node and `agent_type="reviewer"` for `security_review`, and `type="non-agentic"` (with `handler_ref="noop"`) for entry + terminal nodes — per `specs/workflow-graph.md §4 WG-001/WG-002`.
- Uses `start_node="start"` — per `specs/workflow-graph.md §9 WG-027`.
- Uses `handler_ref` on every node — per `specs/handler-contract.md §4.1 HC-001`.
- Uses `outcome.preferred_label` as the edge-condition LHS — per the D4 LHS whitelist and `specs/handler-contract.md §4.2a + §6.1 RECORD Outcome`. Verdict literals stay the canonical triad (`APPROVE` / `REQUEST_CHANGES` / `BLOCK`); `BLOCK` = ship-blocking security defect.
- Uses `traversal_cap="3"` on the `security_review→implement` back-edge — per `specs/workflow-graph.md §6 WG-028` and `specs/execution-model.md §EM-043`.
- Uses the D5 v1 edge-condition dialect (equality only) — per `specs/workflow-graph.md §6 WG-013`.
- Contains a final unconditional `security_review -> "close-needs-attention"` edge satisfying the D-edge-cascade-invariant — per `specs/workflow-graph.md §5 WG-011`.

**Test surface.**

- Static round-trip: `internal/workflow/examples_test.go` (loads every `.dot` in this directory through the C2 validator automatically).
- Scenario harness: `internal/workflow/scenario/security_review_loop_test.go` (drives mock handler responses against scenarios including APPROVE-on-first-pass, REQUEST_CHANGES with security feedback, BLOCK escalation, and cap-hit fallback, each asserting the terminal node and dispatch sequence).

### `spec-R1-R2-cycle.dot`

**Purpose.** Two distinct review postures in sequence: R1 constructive (buildability + design critic), an `integrate_r1` author pass to fold R1 feedback, then R2 adversarial (skeptic + adversary), a final `integrate_r2`, then close. Mixes `outcome.preferred_label` cascades (reviewer edges) with `outcome.status == 'SUCCESS'` cascades (the integrate/author commit gates). R2 REQUEST_CHANGES loops back to `integrate_r1` (nearest author surface) to avoid needlessly re-running R1. Subsumes the spec-slice `spec-R1-multiperspective`.

**Schema version.** `schema_version=1`.

**Spec anchors.**

- Pinned by `docs/sdlc-workflow-corpus.md §7` (spec-R1-R2-cycle) and the corpus dialect contract (§"Dialect contract").
- Pinned by `specs/workflow-graph.md §8 WG-021..WG-023` (terminal-node declaration). `terminal_node_ids="close,close-needs-attention"`.
- Uses `type="agentic"` with `agent_type="implementer"` for `author`, `integrate_r1`, and `integrate_r2`, and `agent_type="reviewer"` for `r1_build`, `r1_critic`, `r2_skeptic`, and `r2_adversary`, and `type="non-agentic"` for entry + terminal nodes — per `specs/workflow-graph.md §4 WG-001/WG-002`.
- Uses `start_node="start"` — per `specs/workflow-graph.md §9 WG-027`.
- Uses `handler_ref` on every node — per `specs/handler-contract.md §4.1 HC-001`.
- Uses both `outcome.preferred_label` (reviewer verdict edges) and `outcome.status == 'SUCCESS'` (commit gates on `integrate_r1` and `integrate_r2`) as edge-condition LHS values — per the D4 LHS whitelist and `specs/handler-contract.md §4.2a + §6.1 RECORD Outcome`.
- Uses `traversal_cap="3"` on multiple back-edges (`r1_build→author`, `r1_critic→author`, `r2_skeptic→integrate_r1`, `r2_adversary→integrate_r1`) — per `specs/workflow-graph.md §6 WG-028` and `specs/execution-model.md §EM-043`.
- Uses the D5 v1 edge-condition dialect (equality only) — per `specs/workflow-graph.md §6 WG-013`.
- Every branching node carries a final unconditional fallback edge satisfying the D-edge-cascade-invariant — per `specs/workflow-graph.md §5 WG-011`.

**Test surface.**

- Static round-trip: `internal/workflow/examples_test.go` (loads every `.dot` in this directory through the C2 validator automatically).
- Scenario harness: `internal/workflow/scenario/spec_r1_r2_cycle_test.go` (drives mock handler responses against scenarios including clean R1→R2 pass, R1 REQUEST_CHANGES loop, R2 adversarial REQUEST_CHANGES looping back to integrate_r1, BLOCK escalation, and cap-hit fallback, each asserting the terminal node and dispatch sequence).

### `spec-citation-cleanup.dot`

**Purpose.** A two-phase spec authoring workflow: a `content_review` reviewer gates the spec body (ignoring citation formatting), then on APPROVE a dedicated `citation_fixer` implementer corrects all stale cross-references, and a `citation_verify` reviewer checks that every cross-reference resolves — with a tight fixer↔verifier sub-loop (capped). Demonstrates role-differentiation via node `role` briefs and `outcome.status == 'SUCCESS'` as the commit-gated handoff from an implementer to the next reviewer. Also covers the `spec-drift-rereview` pattern.

**Schema version.** `schema_version=1`.

**Spec anchors.**

- Pinned by `docs/sdlc-workflow-corpus.md §8` (spec-citation-cleanup) and the corpus dialect contract (§"Dialect contract").
- Pinned by `specs/workflow-graph.md §8 WG-021..WG-023` (terminal-node declaration). `terminal_node_ids="close,close-needs-attention"`.
- Uses `type="agentic"` with `agent_type="implementer"` for `author` and `citation_fixer`, and `agent_type="reviewer"` for `content_review` and `citation_verify`, and `type="non-agentic"` for entry + terminal nodes — per `specs/workflow-graph.md §4 WG-001/WG-002`.
- Uses `start_node="start"` — per `specs/workflow-graph.md §9 WG-027`.
- Uses `handler_ref` on every node — per `specs/handler-contract.md §4.1 HC-001`.
- Uses both `outcome.preferred_label` (reviewer verdict edges on `content_review` and `citation_verify`) and `outcome.status == 'SUCCESS'` (commit gate on `citation_fixer`) as edge-condition LHS values — per the D4 LHS whitelist and `specs/handler-contract.md §4.2a + §6.1 RECORD Outcome`.
- Uses `traversal_cap="3"` on both the `content_review→author` and the `citation_verify→citation_fixer` back-edges — per `specs/workflow-graph.md §6 WG-028` and `specs/execution-model.md §EM-043`.
- Uses the D5 v1 edge-condition dialect (equality only) — per `specs/workflow-graph.md §6 WG-013`.
- Every branching node carries a final unconditional fallback edge satisfying the D-edge-cascade-invariant — per `specs/workflow-graph.md §5 WG-011`.

**Test surface.**

- Static round-trip: `internal/workflow/examples_test.go` (loads every `.dot` in this directory through the C2 validator automatically).
- Scenario harness: `internal/workflow/scenario/spec_citation_cleanup_test.go` (drives mock handler responses against scenarios including clean content+citations pass, content REQUEST_CHANGES loop, citation-fixer sub-loop, and BLOCK escalation, each asserting the terminal node and dispatch sequence).

### `decompose-review-load.dot`

**Purpose.** Minimal spec-to-beads chain: a `decompose` implementer produces a committed `tasks.md`, a `decomp_review` reviewer gates on coverage and decomposition quality, then on APPROVE a `load_beads` implementer creates the beads (commits the `.beads` JSONL diff). The `load_beads` step uses `outcome.status == 'SUCCESS'` as its commit gate to `close`. Subsumes `decompose-quality-gate-loop` (add a multi-lens reviewer chain → consolidate before the load) and the load-tail of `spec-change-redecompose`.

**Schema version.** `schema_version=1`.

**Spec anchors.**

- Pinned by `docs/sdlc-workflow-corpus.md §9` (decompose-review-load) and the corpus dialect contract (§"Dialect contract").
- Pinned by `specs/workflow-graph.md §8 WG-021..WG-023` (terminal-node declaration). `terminal_node_ids="close,close-needs-attention"`.
- Uses `type="agentic"` with `agent_type="implementer"` for `decompose` and `load_beads`, and `agent_type="reviewer"` for `decomp_review`, and `type="non-agentic"` for entry + terminal nodes — per `specs/workflow-graph.md §4 WG-001/WG-002`.
- Uses `start_node="start"` — per `specs/workflow-graph.md §9 WG-027`.
- Uses `handler_ref` on every node — per `specs/handler-contract.md §4.1 HC-001`.
- Uses both `outcome.preferred_label` (reviewer verdict edges on `decomp_review`) and `outcome.status == 'SUCCESS'` (commit gate on `load_beads`) as edge-condition LHS values — per the D4 LHS whitelist and `specs/handler-contract.md §4.2a + §6.1 RECORD Outcome`.
- Uses `traversal_cap="3"` on the `decomp_review→decompose` back-edge — per `specs/workflow-graph.md §6 WG-028` and `specs/execution-model.md §EM-043`.
- Uses the D5 v1 edge-condition dialect (equality only) — per `specs/workflow-graph.md §6 WG-013`.
- Every branching node carries a final unconditional fallback edge satisfying the D-edge-cascade-invariant — per `specs/workflow-graph.md §5 WG-011`.

**Test surface.**

- Static round-trip: `internal/workflow/examples_test.go` (loads every `.dot` in this directory through the C2 validator automatically).
- Scenario harness: `internal/workflow/scenario/decompose_review_load_test.go` (drives mock handler responses against scenarios including APPROVE-then-load, decomposition REQUEST_CHANGES, BLOCK, and load-commit failure, each asserting the terminal node and dispatch sequence).

### `dependency-cycle-fix-loop.dot`

**Purpose.** Detect → fix → recheck loop where the loop pivot is a non-verdict `preferred_label`: a `cycle_check` implementer runs `br dep cycles`, commits a `cycle-report.md`, and surfaces the result as a custom label (`CYCLE` / `ACYCLIC`). Demonstrates arbitrary `preferred_label` values (WG-019), `traversal_cap` on a `fix_cycle→cycle_check` back-edge, and `outcome.failure_class == 'structural'` routing for a tool-level error. The SOON-cleaner form replaces `cycle_check` with an `hk-l8rpd` tool node.

**Schema version.** `schema_version=1`.

**Spec anchors.**

- Pinned by `docs/sdlc-workflow-corpus.md §10` (dependency-cycle-fix-loop) and the corpus dialect contract (§"Dialect contract").
- Pinned by `specs/workflow-graph.md §8 WG-021..WG-023` (terminal-node declaration). `terminal_node_ids="close,close-needs-attention"`.
- Uses `type="agentic"` with `agent_type="implementer"` for both `cycle_check` and `fix_cycle`, and `type="non-agentic"` for entry + terminal nodes — per `specs/workflow-graph.md §4 WG-001/WG-002`.
- Uses `start_node="start"` — per `specs/workflow-graph.md §9 WG-027`.
- Uses `handler_ref` on every node — per `specs/handler-contract.md §4.1 HC-001`.
- Uses `outcome.preferred_label` with custom labels `CYCLE` and `ACYCLIC` on the `cycle_check` outgoing edges — demonstrating arbitrary preferred_label values per `specs/workflow-graph.md §6 WG-019` (authors may mint domain labels beyond the APPROVE/REQUEST_CHANGES/BLOCK triad).
- Uses `outcome.failure_class == 'structural'` as an edge-condition LHS on the `cycle_check` failure path — per the D4 LHS whitelist (closed `failure_class` enum).
- Uses `traversal_cap="3"` on the `fix_cycle→cycle_check` back-edge — per `specs/workflow-graph.md §6 WG-028` and `specs/execution-model.md §EM-043`.
- Uses the D5 v1 edge-condition dialect (equality only) — per `specs/workflow-graph.md §6 WG-013`.
- Contains a final unconditional `cycle_check -> "close-needs-attention"` fallback edge satisfying the D-edge-cascade-invariant — per `specs/workflow-graph.md §5 WG-011`.

**Test surface.**

- Static round-trip: `internal/workflow/examples_test.go` (loads every `.dot` in this directory through the C2 validator automatically).
- Scenario harness: `internal/workflow/scenario/dependency_cycle_fix_loop_test.go` (drives mock handler responses against scenarios including ACYCLIC-on-first-check, CYCLE-then-fix-then-ACYCLIC, structural failure escalation, and cap-hit fallback, each asserting the terminal node and dispatch sequence).

### `docs-sync.dot`

**Purpose.** A two-implementer spine (code change → docs update) followed by a reviewer that routes back to EITHER upstream implementer via a custom `preferred_label`: `REQUEST_CHANGES` → `update_docs` (docs-only fix); `CODE_CHANGE` → `change_code` (code needs rework). Demonstrates that `preferred_label` is an arbitrary string (WG-019) — authors can mint domain labels beyond the canonical APPROVE/REQUEST_CHANGES/BLOCK triad as long as the reviewer brief names them.

**Schema version.** `schema_version=1`.

**Spec anchors.**

- Pinned by `docs/sdlc-workflow-corpus.md §11` (docs-sync) and the corpus dialect contract (§"Dialect contract").
- Pinned by `specs/workflow-graph.md §8 WG-021..WG-023` (terminal-node declaration). `terminal_node_ids="close,close-needs-attention"`.
- Uses `type="agentic"` with `agent_type="implementer"` for `change_code` and `update_docs`, and `agent_type="reviewer"` for `review_sync`, and `type="non-agentic"` for entry + terminal nodes — per `specs/workflow-graph.md §4 WG-001/WG-002`.
- Uses `start_node="start"` — per `specs/workflow-graph.md §9 WG-027`.
- Uses `handler_ref` on every node — per `specs/handler-contract.md §4.1 HC-001`.
- Uses `outcome.preferred_label` with custom label `CODE_CHANGE` (routes back to `change_code`) alongside the standard `REQUEST_CHANGES` (routes back to `update_docs`) — demonstrating arbitrary preferred_label values per `specs/workflow-graph.md §6 WG-019`.
- Uses `traversal_cap="3"` on `review_sync→update_docs` and `traversal_cap="2"` on `review_sync→change_code` — per `specs/workflow-graph.md §6 WG-028` and `specs/execution-model.md §EM-043`.
- Uses the D5 v1 edge-condition dialect (equality only) — per `specs/workflow-graph.md §6 WG-013`.
- Contains a final unconditional `review_sync -> "close-needs-attention"` edge satisfying the D-edge-cascade-invariant — per `specs/workflow-graph.md §5 WG-011`.

**Test surface.**

- Static round-trip: `internal/workflow/examples_test.go` (loads every `.dot` in this directory through the C2 validator automatically).
- Scenario harness: `internal/workflow/scenario/docs_sync_test.go` (drives mock handler responses against scenarios including APPROVE, REQUEST_CHANGES (docs-only fix), CODE_CHANGE (code requires rework), BLOCK, and cap-hit fallback, each asserting the terminal node and dispatch sequence).

### `review-route-by-failure-class.dot`

**Purpose.** Branches on `outcome.failure_class` (the third LHS whitelist entry), mapping the closed failure-class taxonomy to disposition: `transient` → retry the implementer (capped); `structural` / `deterministic` / `canceled` / `budget_exhausted` / `compilation_loop` → needs-attention. A SUCCESS outcome carries no `failure_class`, so those edges miss and fall through to the unconditional handoff to the reviewer. Also covers the `review-escalate-to-human` pattern: BLOCK and cap-hit are both escalation triggers routed to the needs-attention terminal. Live-run honesty: agents cannot be reliably forced to emit a given `failure_class`, so the scenario test drives the non-transient branches with synthetic outcomes.

**Schema version.** `schema_version=1`.

**Spec anchors.**

- Pinned by `docs/sdlc-workflow-corpus.md §12` (review-route-by-failure-class) and the corpus dialect contract (§"Dialect contract").
- Pinned by `specs/workflow-graph.md §8 WG-021..WG-023` (terminal-node declaration). `terminal_node_ids="close,close-needs-attention"`.
- Uses `type="agentic"` with `agent_type="implementer"` for `implementer` and `agent_type="reviewer"` for `reviewer`, and `type="non-agentic"` for entry + terminal nodes — per `specs/workflow-graph.md §4 WG-001/WG-002`.
- Uses `start_node="start"` — per `specs/workflow-graph.md §9 WG-027`.
- Uses `handler_ref` on every node — per `specs/handler-contract.md §4.1 HC-001`.
- Uses `outcome.failure_class` as the edge-condition LHS for the `implementer` outgoing edges, exercising all closed failure-class values (`transient`, `structural`, `deterministic`, `canceled`, `budget_exhausted`, `compilation_loop`) — per the D4 LHS whitelist and the closed `failure_class` enum in the dialect contract.
- Uses `outcome.preferred_label` on the `reviewer` outgoing edges — per `specs/handler-contract.md §4.2a + §6.1 RECORD Outcome`.
- Uses `traversal_cap="3"` on the `implementer→implementer` transient-retry self-edge and `traversal_cap="3"` on the `reviewer→implementer` back-edge — per `specs/workflow-graph.md §6 WG-028` and `specs/execution-model.md §EM-043`.
- Uses the D5 v1 edge-condition dialect (equality only) — per `specs/workflow-graph.md §6 WG-013`.
- Contains unconditional fallback edges on both `implementer` (final unconditional edge to `reviewer`) and `reviewer` (final unconditional edge to `"close-needs-attention"`) satisfying the D-edge-cascade-invariant — per `specs/workflow-graph.md §5 WG-011`.

**Test surface.**

- Static round-trip: `internal/workflow/examples_test.go` (loads every `.dot` in this directory through the C2 validator automatically).
- Scenario harness: `internal/workflow/scenario/review_route_by_failure_class_test.go` (drives synthetic outcomes against the failure-class taxonomy — transient retry, structural escalation, deterministic escalation, canceled escalation — and the verdict paths APPROVE/REQUEST_CHANGES/BLOCK, each asserting the terminal node and dispatch sequence).

### `characterize-refactor-verify.dot`

**Purpose.** Behavior-preserving refactor with a safety net: a `characterize` implementer commits tests pinning current behavior (the oracle), a `refactor` implementer restructures while keeping those tests green, and a `verify_review` reviewer confirms behavior is preserved. The REQUEST_CHANGES loop returns to `refactor` (NOT `characterize`) — the oracle must not be rewritten during the fix loop. Proves a back-edge can re-enter a mid-graph implementer node while leaving an earlier commit untouched, and that the canonical verdict enum serves "behavior preservation" intent purely via the brief.

**Schema version.** `schema_version=1`.

**Spec anchors.**

- Pinned by `docs/sdlc-workflow-corpus.md §13` (characterize-refactor-verify) and the corpus dialect contract (§"Dialect contract").
- Pinned by `specs/workflow-graph.md §8 WG-021..WG-023` (terminal-node declaration). `terminal_node_ids="close,close-needs-attention"`.
- Uses `type="agentic"` with `agent_type="implementer"` for `characterize` and `refactor`, and `agent_type="reviewer"` for `verify_review`, and `type="non-agentic"` for entry + terminal nodes — per `specs/workflow-graph.md §4 WG-001/WG-002`.
- Uses `start_node="start"` — per `specs/workflow-graph.md §9 WG-027`.
- Uses `handler_ref` on every node — per `specs/handler-contract.md §4.1 HC-001`.
- Uses `outcome.preferred_label` as the edge-condition LHS on `verify_review`'s outgoing edges — per the D4 LHS whitelist and `specs/handler-contract.md §4.2a + §6.1 RECORD Outcome`.
- Uses `traversal_cap="3"` on the `verify_review→refactor` back-edge (NOT back to `characterize`) — per `specs/workflow-graph.md §6 WG-028` and `specs/execution-model.md §EM-043`. The back-edge targets `refactor` rather than `characterize` so the oracle survives the fix loop unmodified.
- Uses the D5 v1 edge-condition dialect (equality only) — per `specs/workflow-graph.md §6 WG-013`.
- Contains a final unconditional `verify_review -> "close-needs-attention"` edge satisfying the D-edge-cascade-invariant — per `specs/workflow-graph.md §5 WG-011`.

**Test surface.**

- Static round-trip: `internal/workflow/examples_test.go` (loads every `.dot` in this directory through the C2 validator automatically).
- Scenario harness: `internal/workflow/scenario/characterize_refactor_verify_test.go` (drives mock handler responses against scenarios including APPROVE-on-first-verify, REQUEST_CHANGES loop back to refactor (not characterize), BLOCK escalation, and cap-hit fallback, each asserting the terminal node and dispatch sequence).

### `plan-to-shipped-now.dot` (DEMO D1)

**Purpose.** End-to-end SDLC arc on TODAY's primitives (all-NOW topology): idea → plan → spec → tasking → implement → multi-review-consolidate → docs → close. Built entirely from proven NOW loops and composing `plan-review-loop` (#5), a spec gate, `decompose-review-load` (#9 style), the MARQUEE `dual-review-consolidate` (#3), and `docs-sync` (#11). Classed DEMO because a full clean walk is 14+ agentic nodes; it is heavy for routine use but demonstrates the whole SDLC in one graph. **★ Green-build gate (review fix #2):** because the deterministic tool node is SOON (`hk-l8rpd`), the `consolidate` reviewer and the `docs_review` reviewer each run `go build ./... && go test ./...` in-session and BLOCK on red (red build = ship-blocking defect). D2 (`plan-to-shipped-faithful`) replaces this in-session check with a real tool node once `hk-l8rpd` lands.

**Schema version.** `schema_version=1`.

**Spec anchors.**

- Pinned by `docs/sdlc-workflow-corpus.md §D1` (plan-to-shipped-now, all-NOW topology, DEMO arc) and the corpus dialect contract (§"Dialect contract").
- Uses the marquee brief discipline from `docs/sdlc-workflow-corpus.md §"Marquee brief discipline"`: `rev_correct` and `rev_design` write+commit `reviews/reviewer-<axis>.md` FIRST, then write `.harmonik/review.json`; `consolidate` reads all findings files, severity-joins, and writes the final verdict.
- Green-build gate documented in `docs/sdlc-workflow-corpus.md §"Changes from _consolidated.md"` fix #2: `consolidate` and `docs_review` briefs run `go build ./... && go test ./...` in-session and BLOCK on red until `hk-l8rpd` lands a real tool node.
- Pinned by `specs/workflow-graph.md §8 WG-021..WG-023` (terminal-node declaration). `terminal_node_ids="close,close-needs-attention"`.
- Uses `type="agentic"` with `agent_type="implementer"` for `draft_plan`, `draft_spec`, `decompose`, `load_beads`, `implement`, `update_docs`; `agent_type="reviewer"` for `plan_review`, `spec_review`, `rev_correct`, `rev_design`, `consolidate`, `docs_review`; and `type="non-agentic"` (with `handler_ref="noop"`) for entry + terminal nodes — per `specs/workflow-graph.md §4 WG-001/WG-002`.
- Uses `start_node="start"` — per `specs/workflow-graph.md §9 WG-027`.
- Uses `handler_ref` on every node — per `specs/handler-contract.md §4.1 HC-001`.
- Uses `outcome.preferred_label` as the edge-condition LHS on all verdict branches — per the D4 LHS whitelist and `specs/handler-contract.md §4.2a + §6.1 RECORD Outcome`. Verdict literals are uppercase (`APPROVE` / `REQUEST_CHANGES` / `BLOCK`).
- Uses `outcome.status == 'SUCCESS'` as the edge-condition LHS on the `load_beads→implement` commit-gate edge — per the D4 LHS whitelist (row 1, `outcome.status`).
- Uses `traversal_cap="3"` on all verdict back-edges (`plan_review→draft_plan`, `spec_review→draft_spec`, `consolidate→implement`, `docs_review→update_docs`) — per `specs/workflow-graph.md §6 WG-028` and `specs/execution-model.md §EM-043`.
- Uses the D5 v1 edge-condition dialect (equality only) — per `specs/workflow-graph.md §6 WG-013`.
- Every branching node carries a final unconditional fallback edge satisfying the D-edge-cascade-invariant — per `specs/workflow-graph.md §5 WG-011`.
- The `rev_correct→rev_design→consolidate` spine is fully unconditional: both per-axis reviewers always run before the consolidate node branches per `docs/sdlc-workflow-corpus.md §3` marquee discipline.

**Test surface.**

- Static round-trip: `internal/workflow/examples_test.go` (loads every `.dot` in this directory through the C2 validator automatically).
- Scenario harness: `internal/workflow/scenario/plan_to_shipped_now_test.go` (drives mock handler responses against eight scenarios covering the full S2 path obligations: happy-path full arc, plan-review BLOCK early exit, spec-review RC loop, load-beads non-SUCCESS commit gate, consolidate BLOCK including the red-build path, consolidate cap-hit, docs-review APPROVE, and docs-review unrecognized-label fallback).

### `plan-to-shipped-faithful.dot` (DEMO D2)

**Purpose.** The north-star post-parity SDLC arc: the same phases as D1 but with the deterministic steps done right. New primitives enabled by the parity beads:

- `frame_problem` — non-committing analysis node (`non_committing="true"`, pending `hk-69asi` for zero-commit SUCCESS) that frames the problem space before drafting the plan.
- `load_beads` + `cycle_check` — tool-node proxies (`type="non-agentic"`, `handler_ref="shell"`, pending `hk-l8rpd` for `type="tool"`) replacing the agentic `load_beads` implementer from D1. `cycle_check` runs `br dep cycles` and blocks on a detected cycle before any implementation starts.
- `green_build` — deterministic build gate (`go build ./... && go vet ./... && go test ./...`) replacing D1's in-session build check. Uses the `outcome.status == 'FAIL' && outcome.failure_class == 'deterministic'` compound condition (D5 v1 `&&` dialect) to loop back to `implement` on a fixable build failure.
- Per-node `prompt=` on all implementer nodes (live for implementers per HC-006a `§III.3`; pending `hk-sdnzj` for reviewer-class nodes where it is accepted-but-inert at v1).

**Topology.** idea → frame_problem → plan → spec → tasking (load_beads + cycle_check) → implement → triple-reviewer-consolidate (rev_correct + rev_tests + consolidate) → green_build → close.

**PENDING capabilities.** The three `type="non-agentic"` shell-node proxies (`load_beads`, `cycle_check`, `green_build`) will be replaced with `type="tool"` nodes once `hk-l8rpd` lands. The `non_committing="true"` on `frame_problem` is already live for implementer-class nodes; on reviewer nodes (`rev_correct`, `rev_tests`, `consolidate`) it is a v1 warning (retained, not dispatched — reviewers do not require HEAD advance). The `class="hard"` on `draft_plan` and `decompose` are permissive warnings (retained in `UnknownAttrs`; not dispatched until `hk-q8nqr` lands).

**Schema version.** `schema_version=1`.

**Spec anchors.**

- Pinned by `docs/sdlc-workflow-corpus.md §D2` (plan-to-shipped-faithful, DEMO post-parity target) and the corpus dialect contract (§"Dialect contract").
- Uses the marquee brief discipline from `docs/sdlc-workflow-corpus.md §"Marquee brief discipline"`: `rev_correct` and `rev_tests` write+commit `reviews/reviewer-<axis>.md` FIRST, then write `.harmonik/review.json`; `consolidate` reads all findings files, severity-joins, and writes the final verdict.
- Pinned by `specs/workflow-graph.md §8 WG-021..WG-023` (terminal-node declaration). `terminal_node_ids="close,close-needs-attention"`.
- Uses `type="agentic"` with `agent_type="implementer"` and `non_committing="true"` for `frame_problem` (hk-69asi pending); `agent_type="implementer"` for `draft_plan`, `draft_spec`, `decompose`, `implement`; `agent_type="reviewer"` for `plan_review`, `spec_review`, `rev_correct`, `rev_tests`, `consolidate`; and `type="non-agentic"` for entry + terminal + tool-proxy nodes — per `specs/workflow-graph.md §4 WG-001/WG-002`.
- Uses `type="non-agentic"` with `handler_ref="shell"` and `tool_command=` for `load_beads`, `cycle_check`, and `green_build` as valid v1 proxies for the pending `type="tool"` (hk-l8rpd) per WG-039.
- Uses `start_node="start"` — per `specs/workflow-graph.md §9 WG-027`.
- Uses `handler_ref` on every node — per `specs/handler-contract.md §4.1 HC-001`.
- Uses `outcome.preferred_label` as the edge-condition LHS on all verdict branches — per the D4 LHS whitelist and `specs/handler-contract.md §4.2a + §6.1 RECORD Outcome`. Verdict literals are uppercase (`APPROVE` / `REQUEST_CHANGES` / `BLOCK`).
- Uses `outcome.status == 'SUCCESS'` as the edge-condition LHS on the `load_beads→cycle_check` and `cycle_check→implement` commit-gate edges — per the D4 LHS whitelist (row 1, `outcome.status`).
- Uses the compound condition `outcome.status == 'FAIL' && outcome.failure_class == 'deterministic'` on the `green_build→implement` back-edge — per the D5 v1 edge-condition dialect (`&&` operator, equality only) per `specs/workflow-graph.md §6 WG-013`.
- Uses `traversal_cap="3"` on all verdict back-edges (`plan_review→draft_plan`, `spec_review→draft_spec`, `consolidate→implement`, `green_build→implement`) — per `specs/workflow-graph.md §6 WG-028` and `specs/execution-model.md §EM-043`.
- Uses the D5 v1 edge-condition dialect (equality + `&&`) — per `specs/workflow-graph.md §6 WG-013`.
- Every branching node carries a final unconditional fallback edge satisfying the D-edge-cascade-invariant — per `specs/workflow-graph.md §5 WG-011`.
- The `rev_correct→rev_tests→consolidate` spine is fully unconditional: both per-axis reviewers always run before the consolidate node branches per marquee brief discipline.
- Uses `non_committing="true"` on `frame_problem` per `specs/workflow-graph.md §4 WG-041` and `specs/examples/authoring-notes.md §1` (non_committing contract).
- Uses `prompt=` on implementer nodes per HC-006a `§III.3` (CHB-028 Body channel replacement). Accepted-but-inert on reviewer nodes at v1 per WG-040 §I.3; use `role=` to specialize reviewer briefs today.

**Test surface.**

- Static round-trip: `internal/workflow/examples_test.go` (loads every `.dot` in this directory through the C2 validator automatically). Two permissive warnings for `class="hard"` on `draft_plan` and `decompose` are expected (retained in `UnknownAttrs`, not dispatched at v1).
- Scenario harness: `internal/workflow/scenario/plan_to_shipped_faithful_test.go` (drives mock handler responses against nine scenarios covering the full S2 path obligations: happy-path full arc, plan-review BLOCK early exit, plan-review RC loop, load-beads non-SUCCESS tool gate, cycle-check non-SUCCESS tool gate, consolidate BLOCK, consolidate cap-hit, green-build deterministic fail → implement loop, and green-build non-deterministic fail → unconditional fallback).

### `regression-gate.dot`

**Purpose.** Reproduce-before-fix encoded as a graph structure: a shell tool node whose **FAIL is the desired forward path** (the bug must reproduce before we trust a fix), followed by an agentic fix node, followed by a full regression suite that catches both "fix didn't take" and "fix broke something else." Subsumes the debugging-slice `sentry-bugfix-faithful` and the degraded-NOW `bugfix-loop-now` (the latter folds the shell gates into the agent's session, trading deterministic exit codes for an LLM opinion). This is SDLC corpus workflow #16 (SOON — depends on the tool/shell node handler).

**Schema version.** `schema_version=1`.

**Spec anchors.**

- Pinned by `docs/sdlc-workflow-corpus.md §16` (regression-gate topology) and the corpus dialect contract (§"Dialect contract").
- Uses `type="non-agentic"` with `handler_ref="shell"` and `tool_command=` for `reproduce` and `regression_suite` — per `specs/workflow-graph.md §4 WG-039` (tool_command / shell handler). The shell handler maps exit codes to Outcomes per `specs/execution-model.md §II.7 EM-057 item 7` (exit 0 → SUCCESS; non-zero → FAIL+deterministic) and `§II.8 EM-058` (non-agentic row). In-process handler contract: `specs/handler-contract.md §III.1 HC-063`.
- Uses `type="agentic"` with `agent_type="implementer"` and `handler_ref="claude-implementer"` for `fix_bug` — per `specs/workflow-graph.md §4 WG-001/WG-002` (closed node-type enum) and `specs/handler-contract.md §4.1 HC-001` (handler_ref required).
- Uses `type="non-agentic"` with `handler_ref="noop"` for `start`, `cannot_reproduce`, `close`, and `close-needs-attention` — per WG-001/WG-002.
- Uses `outcome.status` as the edge-condition LHS on the `reproduce→fix_bug` (`== 'FAIL'`) and `reproduce→cannot_reproduce` (`== 'SUCCESS'`) edges, and on the `regression_suite→close` (`== 'SUCCESS'`) edge — per the D4 LHS whitelist (row 1, `outcome.status`).
- Uses the compound condition `outcome.status == 'FAIL' && outcome.failure_class == 'deterministic'` on the `regression_suite→fix_bug` back-edge — per the D5 v1 edge-condition dialect (`&&` operator, equality only) per `specs/workflow-graph.md §6 WG-013`.
- Uses `traversal_cap="3"` on the `regression_suite→fix_bug` back-edge — per `specs/workflow-graph.md §6 WG-028` and `specs/execution-model.md §EM-043`.
- Uses `start_node="start"` — per `specs/workflow-graph.md §9 WG-027`.
- Uses `handler_ref` on every node — per `specs/handler-contract.md §4.1 HC-001`.
- Uses `terminal_node_ids="close,close-needs-attention"` — per `specs/workflow-graph.md §8 WG-021..WG-023`.
- Every branching node (`reproduce`, `regression_suite`) carries a final unconditional fallback edge satisfying the D-edge-cascade-invariant — per `specs/workflow-graph.md §5 WG-011`.
- Uses the D5 v1 edge-condition dialect (equality + `&&` only; no `<`/`>`) — per `specs/workflow-graph.md §6 WG-013`.

**Gap coverage.** Demonstrates the "FAIL-as-forward-path" pattern: a tool node where non-zero exit is the *expected* success path for the overall workflow (the bug must reproduce before a fix is trusted). Extends the tool-node pattern of `tool-node.dot` with a compound `&&` condition on a capped back-edge.

**Test surface.**

- Static round-trip: `internal/workflow/examples_test.go` (loads every `.dot` in this directory through the C2 validator automatically).
- Scenario harness: `internal/workflow/scenario/regression_gate_test.go` (drives mock handler responses against six scenarios covering the S2 path obligations: happy-path bug-reproduced arc, cannot-reproduce routing, reproduce infra fallback, regression-suite fix-loop, regression-suite cap-hit, and regression-suite transient fallback).

### `release-with-rollback.dot`

**Purpose.** Release pipeline with a compensating-action node: a failed `publish` routes to a `rollback` shell node (delete tag, revert release commit) before terminating at `close-needs-attention`, leaving the repo clean for a retry. Idiomatic "on failure, undo, then escalate" within the sequential v1 dialect — rollback is a routed node, not a parallel try/finally compensator. Subsumes the simpler `release-readiness` topology (where publish-fail goes straight to `close-needs-attention` without rollback). This is SDLC corpus workflow #17 (SOON — depends on the tool/shell node handler).

**Schema version.** `schema_version=1`.

**Spec anchors.**

- Pinned by `docs/sdlc-workflow-corpus.md §17` (release-with-rollback topology) and the corpus dialect contract (§"Dialect contract").
- Uses `type="non-agentic"` with `handler_ref="shell"` and `tool_command=` for `build_artifacts`, `publish`, and `rollback` — per `specs/workflow-graph.md §4 WG-039` (tool_command / shell handler). The shell handler maps exit codes to Outcomes per `specs/execution-model.md §II.7 EM-057 item 7` (exit 0 → SUCCESS; non-zero → FAIL+deterministic) and `§II.8 EM-058` (non-agentic row). In-process handler contract: `specs/handler-contract.md §III.1 HC-063`.
- Uses `type="agentic"` with `agent_type="implementer"` and `handler_ref="claude-implementer"` for `cut_release` — per `specs/workflow-graph.md §4 WG-001/WG-002` (closed node-type enum) and `specs/handler-contract.md §4.1 HC-001` (handler_ref required).
- Uses `type="non-agentic"` with `handler_ref="noop"` for `start`, `close`, and `close-needs-attention` — per WG-001/WG-002.
- Uses `outcome.status` as the edge-condition LHS on the `build_artifacts→publish` (`== 'SUCCESS'`), `publish→close` (`== 'SUCCESS'`), and `publish→rollback` (`== 'FAIL'`) edges — per the D4 LHS whitelist (row 1, `outcome.status`).
- Uses `start_node="start"` — per `specs/workflow-graph.md §9 WG-027`.
- Uses `handler_ref` on every node — per `specs/handler-contract.md §4.1 HC-001`.
- Uses `terminal_node_ids="close,close-needs-attention"` — per `specs/workflow-graph.md §8 WG-021..WG-023`.
- Every branching node (`build_artifacts`, `publish`) carries a final unconditional fallback edge satisfying the D-edge-cascade-invariant — per `specs/workflow-graph.md §5 WG-011`.
- `publish` carries both an explicit `== 'FAIL'` condition AND an unconditional fallback, both routing to `rollback`: the explicit condition handles the deterministic non-zero exit, while the fallback handles any other non-SUCCESS state (RETRY, infra error, budget_exhausted). Together they guarantee that any non-SUCCESS publish triggers the compensating action.
- `rollback` is idempotent (deleting a non-existent tag or reverting a clean tree is a no-op) and routes unconditionally to `close-needs-attention` — a successful rollback still means the release did not land.
- No `traversal_cap` anywhere: the graph is a DAG with no back-edges. Retries are operator-driven by re-submitting from the top.

**Gap coverage.** Demonstrates the compensating-action pattern: a dedicated rollback node that fires on any publish failure, leaving the repo in a clean state before escalating. Extends the tool-node pattern of `tool-node.dot` with a multi-tool sequential chain (`build_artifacts → publish → rollback`) and shows that an explicit conditional edge and an unconditional fallback can coexist on the same source node when both route to the same target (covering disjoint failure modes).

**Test surface.**

- Static round-trip: `internal/workflow/examples_test.go` (loads every `.dot` in this directory through the C2 validator automatically).
- Scenario harness: `internal/workflow/scenario/release_with_rollback_test.go` (drives mock handler responses against five scenarios covering the S2 path obligations: happy-path full arc, build-failure fallback, build-infra fallback, publish-failure rollback via explicit FAIL condition, and publish-fallback rollback via unconditional fallback).

### `quality-gate-policy.dot`

**Purpose.** Policy gate node (`allow` / `deny` / `escalate-to-human`) inserted after a subjective reviewer, demonstrating the distinction between a reviewer's human-judgment verdict and a `gate` node's deterministic ControlPoint decision. The reviewer renders `APPROVE / REQUEST_CHANGES / BLOCK`; an `APPROVE` then enters a `gate` node that evaluates the named `merge-quality-policy` ControlPoint — a deterministic quality-bar check that can allow the merge, deny it (loop back to the implementer), or escalate to a human operator. Subsumes the review-slice `security-cognition-gate` (same gate node topology with a security-policy `gate_ref`). This is SDLC corpus workflow #19 (SOON — required hk-karlz, now landed as 58c6348f).

**Schema version.** `schema_version=1`.

**Spec anchors.**

- Pinned by `docs/sdlc-workflow-corpus.md §19` (quality-gate-policy topology) and the corpus dialect contract (§"Dialect contract").
- Uses `type="gate"` with `gate_ref="merge-quality-policy"` and `handler_ref="gate-evaluator"` for `quality_gate` — per `specs/workflow-graph.md §4 WG-005` (gate node attribute set: MUST carry both `gate_ref` AND `handler_ref`; MUST NOT carry `agent_type`, `idempotency_class`, or `policy_ref`).
- Gate decisions (allow/deny/escalate-to-human) are ALL `status=SUCCESS` per `specs/control-points.md §6.1.8 CP-058`. Routing is on `outcome.preferred_label` only (not `outcome.status`), per `specs/execution-model.md §4.1 EM-005b`.
- Gate eval-failure (registry nil, structural error) yields `status=FAIL`, caught by the unconditional fallback edge per `specs/workflow-graph.md §5 WG-011`.
- Uses `type="agentic"` with `agent_type="implementer"` and `handler_ref="claude-implementer"` for `implementer`, and `agent_type="reviewer"` and `handler_ref="claude-reviewer"` for `reviewer` — per `specs/workflow-graph.md §4 WG-001/WG-002`.
- Uses `type="non-agentic"` with `handler_ref="noop"` for `start`, `close`, and `close-needs-attention` — per WG-001/WG-002.
- Uses `start_node="start"` — per `specs/workflow-graph.md §9 WG-027`.
- Uses `handler_ref` on every node — per `specs/handler-contract.md §4.1 HC-001`.
- Uses `terminal_node_ids="close,close-needs-attention"` — per `specs/workflow-graph.md §8 WG-021..WG-023`.
- Uses `outcome.preferred_label` as the edge-condition LHS on both reviewer outgoing edges (`== 'APPROVE'`, `== 'REQUEST_CHANGES'`, `== 'BLOCK'`) and gate outgoing edges (`== 'allow'`, `== 'deny'`, `== 'escalate-to-human'`) — per the D4 LHS whitelist (row 3, `outcome.*`) and `specs/handler-contract.md §4.2a + §6.1 RECORD Outcome`.
- Uses `traversal_cap="3"` on both the `reviewer→implementer` (REQUEST_CHANGES) and `quality_gate→implementer` (deny) back-edges — per `specs/workflow-graph.md §6 WG-028` and `specs/execution-model.md §EM-043`. The two caps are independent (separate edge keys in the cycle counter).
- Uses the D5 v1 edge-condition dialect (equality only; no `<`/`>`) — per `specs/workflow-graph.md §6 WG-013`.
- Every branching node (`reviewer`, `quality_gate`) carries a final unconditional fallback edge satisfying the D-edge-cascade-invariant — per `specs/workflow-graph.md §5 WG-011`.

**Gap coverage.** First example demonstrating the `gate` node type (WG-005 / CP-058). Shows that (a) gate decisions are status=SUCCESS and MUST be distinguished by `preferred_label`, not by `outcome.status`; (b) a gate node has no `idempotency_class` (it is a policy decision, not an agent dispatch); and (c) two independent `traversal_cap` back-edges can coexist on different source nodes (reviewer RC cap and gate deny cap are counted separately).

**Test surface.**

- Static round-trip: `internal/workflow/examples_test.go` (loads every `.dot` in this directory through the C2 validator automatically).
- Scenario harness: `internal/workflow/scenario_quality_gate_policy_hko52fm21_test.go` (drives synthetic outcomes through the real loader → cascade pipeline against nine scenarios covering the S2 path obligations: gate-allow happy path, gate-deny loop, gate-escalate-to-human, reviewer-REQUEST_CHANGES loop → approve, reviewer-BLOCK escalation, reviewer-fallback, gate-fallback/eval-failure, reviewer-cap-hit, and gate-deny-cap-hit).

### `sub-workflow-example.dot` + `sub-workflow-commit-gate.dot`

**Purpose.** Canonical worked examples for sub-workflow node composition. Two files are authored as a pair:

- **`sub-workflow-example.dot`** (parent) — a bead workflow whose commit-gate phase is extracted into a child sub-workflow. Topology: start → implement → commit_gate (sub-workflow) → review → close / close-needs-attention. The sub-workflow node's terminal Outcome (SUCCESS from the child's `tests` node, FAIL from `gate_fail`) escapes verbatim to route the parent's `commit_gate → review` or `commit_gate → close-needs-attention` edge per SW-006.

- **`sub-workflow-commit-gate.dot`** (child) — a reusable two-step commit gate: `build_check` (build + vet) routes to `tests` (scenario gate, terminal) on SUCCESS; the unconditional fallback routes to `gate_fail` (exit 1, terminal) on any non-SUCCESS. Both terminal nodes are shell nodes whose exit code IS the escaped Outcome per EM-036a; the `gate_fail` terminal exits 1 explicitly to produce a FAIL Outcome that the parent cascade routes as a gate failure.

Together the two files demonstrate: (a) how to declare a `type="sub-workflow"` node with the required `sub_workflow_ref` and `workflow_version` attributes (WG-006); (b) how three-tier resolution (SW-004) locates the child graph; (c) how the child's terminal node outcome escapes to the parent (SW-006 / SW-INV-002); and (d) the authoring rule that a child terminal node must produce a meaningful Outcome — a noop terminal always returns SUCCESS, so a FAIL path needs a shell terminal with `exit 1`.

**Schema version.** `schema_version=1`.

**Spec anchors.**

- Pinned by `specs/sub-workflow-dispatch.md §7 SW-EX-001` (worked examples, sub-workflow dispatch contract).
- Pinned by `specs/sub-workflow-dispatch.md SW-001` — in-place expansion, single run identity. The parent `commit_gate` node expands into the child's nodes within the parent run; no new RunID is allocated.
- Pinned by `specs/sub-workflow-dispatch.md SW-004` — three-tier graph resolution. `sub_workflow_ref="specs/examples/sub-workflow-commit-gate.dot"` resolves via tier-1 explicit ref (project-relative path).
- Pinned by `specs/sub-workflow-dispatch.md SW-005` — lifecycle events carry the parent `run_id`. Both `sub_workflow_entered` and `sub_workflow_exited` are emitted with the parent run_id.
- Pinned by `specs/sub-workflow-dispatch.md SW-006` — terminal outcome escape. The parent cascade routes on the child's terminal shell node exit code (SUCCESS/FAIL), not on a synthesized value.
- Uses `type="sub-workflow"` with required `sub_workflow_ref` and `workflow_version` on the `commit_gate` node — per `specs/workflow-graph.md §4 WG-006`.
- `sub-workflow` node carries NO `idempotency_class` (forbidden per `specs/workflow-graph.md §4 WG-007/WG-008`).
- Uses `type="non-agentic"` with `handler_ref="shell"` and `tool_command=` for the gate nodes — per `specs/workflow-graph.md §4 WG-039` and `specs/handler-contract.md §III.1 HC-063`.
- Uses `start_node` graph-level attribute — per `specs/workflow-graph.md §9 WG-027`.
- Uses `terminal_node_ids` on both graphs — per `specs/workflow-graph.md §8 WG-021..WG-023`.
- Uses `outcome.status == 'SUCCESS'` and `outcome.preferred_label` as edge-condition LHS — per the D4 LHS whitelist and `specs/handler-contract.md §4.2a + §6.1 RECORD Outcome`.
- Uses `traversal_cap="3"` on the `review→implement` back-edge — per `specs/workflow-graph.md §6 WG-028` and `specs/execution-model.md §EM-043`.
- Every branching node carries a final unconditional fallback edge — per `specs/workflow-graph.md §5 WG-011`.
- Uses the D5 v1 edge-condition dialect (equality + `&&` only) — per `specs/workflow-graph.md §6 WG-013`.

**Test surface.**

- Static round-trip: both files pass `harmonik graph validate` and round-trip through the C2 DOT validator. They will be picked up by `internal/workflow/examples_test.go` (the auto-discovery test, once authored per the `Future examples` note in this README).
- Scenario harness: end-to-end sub-workflow dispatch is covered by `internal/daemon/scenario_subworkflow_dispatch_hkx9l_test.go` (bead hk-x9l, three tests — SW-001/SW-INV-001, SW-006/SW-INV-002 success path, SW-006/SW-INV-002 fail path). All three pass as of 2026-06-11.

### Future examples

`bead-process.dot` is **deferred** until its prerequisites land (tool-node handler contract, merge-node primitive, sub-workflow composition for review-loop). The candidate follow-up bead is `phase3-bead-process-example`. When the prerequisites land, `bead-process.dot` will be added as a sibling to `review-loop.dot` and will receive its own subsection here.

## Authoring and porting notes (non-normative)

See [`authoring-notes.md`](authoring-notes.md) for guidance on:

- **`auto_status` is rejected** — use `non_committing="true"` instead (the ingest
  error is actionable and names the replacement).
- **Pairing `non_committing` nodes** with a downstream validating tool node
  (WG-041 authoring obligation).
- **Reviewer-node `prompt`** — accepted by the parser but inert at v1; use `role`
  to specialize the reviewer brief today.
- **`class` / `model_stylesheet`** — not dispatched by harmonik; translate to
  direct `model=` attributes per WG-042/WG-043.

`authoring-notes.md` is the "canonical example sidecar" referenced by
`specs/workflow-graph.md §4 WG-041`.

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
