# C2 Spec Draft — `specs/execution-model.md` §7.5 (`dot` mode binding) + EM-007 amendment + §10.1 conformance lift

> **Pass-5 (Spec Draft)** for component C2 of kerf work `phase-3-dot`. This file is the **draft text** that pass-6 will transcribe into `specs/execution-model.md` as additive section content. Three discrete amendments:
>
> 1. **§7.5** (new top-level subsection of §7 Protocols and state machines, immediately after §7.4) — five sub-parts, the binding document for `workflow_mode = dot`.
> 2. **EM-007 amendment** (in-place rewording of the existing §4.2.EM-007 requirement) — admits `handler_ref` on `gate` and `non-agentic` nodes.
> 3. **§10.1 conformance lift** (in-place rewording of the §10.1 Core MVH and Post-MVH paragraphs) — removes the `dot` carve-out, gated on the full phase-3-dot bundle landing.
>
> **Cross-spec sequencing.** This draft is predicated on `specs/workflow-graph.md` (C1) being drafted in parallel (pass-5 sibling); §7.5.3 and §7.5.2 cite C1 section names as load-bearing references. If a C1 section name shifts at pass-6 transcription, the §-ref in this draft updates 1-to-1.
>
> **New requirement-IDs assigned.** The existing spec's highest EM-ID is **EM-054** (§4.12). This draft assigns **EM-055 through EM-061** (sequential, no reuse of retired IDs per the spec template). Mapping below.
>
> | New ID | Title | Sub-section |
> |---|---|---|
> | EM-055 | `dot` workflow input contract | §7.5.1 |
> | EM-056 | `dot` dispatch equivalence with §7.4 | §7.5.2 |
> | EM-057 | `dot` validator obligations | §7.5.3 |
> | EM-058 | `dot` node-type dispatch table | §7.5.4 |
> | EM-059 | `dot` conformance lift + parallel-fan-out reservation | §7.5.5 |
> | EM-060 | EM-007 amendment — `handler_ref` admitted on `non-agentic` and `gate` | §4.2 (in-place EM-007 update) |
> | EM-061 | §10.1 conformance lift | §10.1 (in-place §10.1 update) |
>
> EM-060 and EM-061 are bookkeeping handles for the in-place amendments below; their normative content is the rewording prose itself.

---

## §7.5 — Workflow Mode: `dot` (BINDING DOCUMENT)

This subsection binds `workflow_mode = dot` (per §4.3.EM-012 and the resolution walk of §4.3.EM-012a) to the dispatch loop already published in §7.4 and the deterministic cascade already published in §7.3. §7.5 **does not introduce a new state machine, a new dispatch loop, or a new cascade**. Every dispatch primitive a `dot` run consumes — edge-selection cascade (§4.10.EM-041), context-update ordering (§4.10.EM-041a), guards and gates (§4.10.EM-042 / §4.10.EM-042a), per-edge traversal caps (§4.10.EM-043 / §4.10.EM-043a), no-match-edge → `structural` (§4.10.EM-046a), RETRY re-dispatch (§4.10.EM-046b), sub-workflow expansion (§4.8.EM-034 family), durability decision (§4.5.EM-023a), pre-run validation (§4.9.EM-038), and schema evolution (§6.4) — is consumed unchanged. The five sub-parts that follow are exactly: the input contract (§7.5.1), the dispatch-equivalence statement (§7.5.2), the validator obligations specific to `dot` (§7.5.3), the per-node-type dispatch table (§7.5.4), and the conformance lift plus the post-MVH parallel-fan-out reservation (§7.5.5).

The general workflow-graph vocabulary this subsection consumes — DOT artifact schema, node-type catalog, edge-condition LHS whitelist, edge-condition dialect, outcome-carrier routing, cascade invariant, per-workflow context-keys — is owned by [workflow-graph.md] (per D-attractor-adoption, D-edge-cascade-invariant, D-verdict-surfacing, D1, D3, D4, D5). §7.5 references those by section name; it does not re-declare them.

### §7.5.1 — Workflow Input Contract

#### EM-055 — `dot` workflow input contract: ingest, validate, return §6.1 record

For a run whose `workflow_mode = dot` (resolved per §4.3.EM-012a), the daemon's claim path MUST resolve the run's workflow artifact through the following ordered ingestion pipeline, executing entirely before §7.4 `execute_workflow` begins for the run:

1. **Locate the `.dot` artifact.** The artifact path resolves from the run's bead via the bead's `workflow_ref` field per [beads-integration.md §4.3 BI-005] when present. Absent a per-bead override, the artifact path is derived from the workflow-mode resolution chain per §4.3.EM-012a tier 3 (per-daemon configuration) and tier 4 (built-in fallback, where the fallback maps `dot` mode to a canonical default `.dot` artifact registered with the daemon).
2. **Parse to AST.** The daemon MUST parse the `.dot` artifact to an in-memory graph AST conforming to the DOT subset defined by [workflow-graph.md §Schema]. The parser library is an implementation detail; the parsed AST's surface MUST be sufficient to produce a §6.1 `Workflow` record without consulting any other store.
3. **Convert AST to §6.1 `Workflow` record.** Conversion is mechanical: graph attributes (e.g., `workflow_id`, `version`, `schema_version`, `start_node`, `terminal_nodes`) populate the §6.1 `Workflow` header; node attributes populate §6.1 `Node` fields per [workflow-graph.md §Node Types]; edge attributes populate §6.1 `Edge` fields per [workflow-graph.md §Edge Conditions]. The conversion produces the same §6.1 record shape that §4.10 (cascade), §4.8 (sub-workflow), and §4.9 (validation) already consume for `single` and `review-loop` runs.
4. **Run pre-run validator.** The daemon MUST then invoke the §4.9.EM-038 pre-run validator on the produced `Workflow` record, augmented with the `dot`-specific obligations of §7.5.3 below.
5. **Return the `Workflow` record.** Only after steps 1–4 succeed MAY §7.4 `execute_workflow` begin. A failure at any step routes the run via §7.4's `queue.mark_item_failed(item, validation_failed)` path; no `run_started` event per §4.3.EM-015a is emitted.

**Resume semantics.** On daemon restart, runs whose `workflow_mode = dot` MUST re-execute steps 1–4 against the same `.dot` artifact. The daemon MUST NOT trust a serialized prior parse tree; reparse is cheap and removes a serialization surface. The §6.1 `Workflow.workflow_id` and `workflow_version` produced by the post-restart reparse MUST be identical to the pre-restart values sealed on the Run record per §6.1. A mismatch (artifact mutated under-foot between claim time and restart) MUST route to reconciliation per [reconciliation/spec.md §8.4 Cat 3] (artifact-state divergence); the daemon MUST NOT silently proceed with the post-restart parse.

**Concurrency-readiness.** The parser and the produced `Workflow` record MUST be `run_id`-keyed via the run claim; no daemon-global parser state, no shared parse cache across runs. This matches the §4.11 concurrency posture and the EM-051 `max_concurrent` ceiling: each in-flight `dot` run holds its own `Workflow` record.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

### §7.5.2 — Dispatch Equivalence with §7.4

#### EM-056 — Once the `Workflow` record is loaded, §7.4 and §7.3 apply unchanged

Once §7.5.1.EM-055 returns the §6.1 `Workflow` record for a `dot` run, §7.4 `execute_workflow` and §7.3 `select_next_edge` apply unchanged. The cascade evaluates edge `condition` expressions per the dialect declared in [workflow-graph.md §Edge Conditions] (D5) against the LHS whitelist declared in [workflow-graph.md §Edge Condition LHS Whitelist] (D4); routes via `preferred_label` per [workflow-graph.md §Outcome-Carrier Routing] (D-verdict-surfacing); enforces the unconditional-edge fallback invariant per [workflow-graph.md §Cascade Invariant] (D-edge-cascade-invariant); and applies per-edge `traversal_cap` per §4.10.EM-043. No new dispatch-loop state and no new cascade branching is introduced for `dot`.

Four clarifying clauses, each a restatement of an existing requirement (not a new normative obligation):

1. **Condition-evaluation site.** Condition evaluation runs **inside §7.3 `select_next_edge` (daemon-side)**, per the existing §7.3 pseudocode line `evaluate_condition(...)`. Handlers do not evaluate edge conditions; the cascade does. This is the same evaluation site `single` and `review-loop` runs use; `dot` introduces no handler-side condition obligation. (Restatement only; no new requirement.)
2. **Failure-class routing.** When step (a) of the §4.10.EM-041 cascade evaluates a condition referencing `outcome.failure_class` (admitted as a LHS identifier per D1 and the C1 whitelist), the value read is the `Outcome.failure_class` field per §4.1.EM-005 (as amended by [handler-contract.md §Outcome — C3] to add the field). On a FAIL outcome where the handler did not set `failure_class`, the daemon's classifier per §8.1 reclassification rules MUST back-fill the field BEFORE §7.3 `select_next_edge` runs. The cascade reads only post-classifier `Outcome` values.
3. **Sub-workflow inside `dot`.** §4.8.EM-034 / EM-034a / EM-034b / EM-034c apply unchanged. A `dot` workflow MAY contain `sub-workflow`-type nodes referencing other `.dot` artifacts; expansion is namespaced per EM-034a, the reference graph is acyclic per EM-034b, and the expansion pin is durable on the entry checkpoint per EM-034c. **A `dot` workflow MUST NOT reference a `review-loop` sub-workflow** — the review-loop cycle is mode-driven, not graph-driven, per the §4.3.EM-015d carve-out. The §7.5.3 validator enforces.
4. **Review-loop artifacts are not applicable.** The `${workspace_path}/.harmonik/reviewer-feedback.iter-<N>.md` artifact per §4.3.EM-015d (sub-clause EM-015d-RFD) and the `${workspace_path}/.harmonik/review-target.md` artifact per §4.3.EM-015d (sub-clause EM-015d-RIA) are `review-loop`-mode-only. A `dot` run MUST NOT produce them; their absence on a `dot` run is not an authoring error and MUST NOT be flagged by the §7.5.3 validator. (The EM-015d cross-ref placeholder "(cross-ref pending dot-mode spec)" in §4.3.EM-015d is resolved by §7.5.)

Tags: mechanism

### §7.5.3 — Validator Requirements

#### EM-057 — `dot`-specific validator obligations

The §4.9.EM-038 pre-run validator, when invoked per §7.5.1.EM-055 step 4 against a `dot`-ingested `Workflow` record, MUST enforce the following seven static checks. Any check failing causes the validator to return `false`; §7.4's `queue.mark_item_failed(item, validation_failed)` path runs and the run never starts.

1. **Schema-version compatibility.** The `.dot` artifact's graph-level `schema_version` attribute MUST satisfy the §6.4 N-1 readability rule against the daemon's max-known schema version. A `schema_version` more than one version below the daemon's max OR higher than the daemon's max is a validation failure.
2. **Node-type vocabulary.** Every node's `type` attribute MUST be in the closed node-type catalog declared by [workflow-graph.md §Node Types] (per D3: `{agentic, non-agentic, gate, sub-workflow}` at v1; the pre-D3 `control-point` type per §4.2.EM-006 is collapsed under `gate` and `non-agentic` per the C4 amendment to [control-points.md §Node-Type Binding]). Unknown `type` value is a validation failure — refuse-to-run, not warn-and-continue.
3. **Edge-condition LHS whitelist and dialect.** Every edge's `condition` attribute, if non-empty, MUST tokenize per the dialect declared in [workflow-graph.md §Edge Conditions] (D5: restricted equality + `&&`, no precedence resolver) AND reference only LHS identifiers in the whitelist declared in [workflow-graph.md §Edge Condition LHS Whitelist] (D4: the closed 5-LHS set). `context.<key>` references MUST resolve to a key declared in the workflow's [workflow-graph.md §Context-Keys] per-workflow registry.
4. **Reachability.** Every non-terminal node MUST be reachable from the workflow's `start_node` (per §6.1 `Workflow.start_node_id`); every node listed in the workflow's `terminal_node_ids` (per §6.1) MUST be reachable from the `start_node`.
5. **Cycle bounding.** Every directed cycle in the graph MUST traverse at least one edge carrying a positive-integer `traversal_cap` per §4.10.EM-043. Cycle detection runs at validation time (Tarjan's SCC algorithm or equivalent is sufficient), not at dispatch time. A cycle with no capped edge is a validation failure.
6. **Sub-workflow reference acyclicity.** Per §4.8.EM-034b, the sub-workflow reference graph MUST be acyclic. The validator MUST verify acyclicity transitively across the artifact registry resolved per §7.5.1.EM-055 step 1.
7. **Required attributes by node type.** Each node MUST carry the attributes required for its declared `type` per the per-node-type attribute table in [workflow-graph.md §Node Types]:
   - `agentic` nodes MUST carry `handler_ref` resolving to a handler registered per [handler-contract.md §4.1].
   - `non-agentic` nodes MUST carry `handler_ref` resolving to a handler registered per [handler-contract.md §4.1]. (This obligation is the §4.2.EM-007 amendment per §7.5.4-companion / EM-060 below.)
   - `gate` nodes MUST carry `gate_ref` resolving to a Gate policy per [control-points.md §6.3] AND `handler_ref` resolving to the Gate-evaluator handler registered per [control-points.md §Node-Type Binding] (C4). (This obligation is the §4.2.EM-007 amendment per EM-060 below.)
   - `sub-workflow` nodes MUST carry `sub_workflow_ref` resolving to another `.dot` artifact registered with the daemon per §7.5.1.EM-055 step 1. `sub-workflow` nodes MUST NOT carry `handler_ref` (the handler discipline is delegated to the expanded sub-workflow's nodes, per §4.8.EM-034).

Missing or unresolvable required attributes on any node type are validation failures.

Tags: mechanism

### §7.5.4 — Node-type Dispatch Table

#### EM-058 — Normative dispatch table for `dot` node types

For a `dot` run, §7.4 `execute_workflow`'s `dispatch_node` step MUST dispatch the current node according to the following table. The table names the dispatch action and the consumed Outcome contract for each node type in the closed catalog declared by [workflow-graph.md §Node Types] (D3). The table is normative for `dot` mode; for `single` and `review-loop` modes, the historical dispatch fork in `dispatch_node` is unchanged.

| Node type | Dispatch action | Outcome contract |
|---|---|---|
| `agentic` | Launch the handler referenced by the node's `handler_ref` per [handler-contract.md §4.1] (handler subprocess; LaunchSpec per [handler-contract.md §4.2]). | §4.1.EM-005 `Outcome` with `kind = default` per §4.1.EM-005a. Handler MAY emit any `status`; MAY emit `failure_class` as a hint on FAIL per the C3 amendment to [handler-contract.md §Outcome]. |
| `non-agentic` | Invoke the handler referenced by the node's `handler_ref` per [handler-contract.md §4.1]. Same dispatch path as `agentic` at the spec layer; handler-internal determinism is the handler's responsibility per the node's four-axis tags (§4.2.EM-011). | §4.1.EM-005 `Outcome` with `kind = default` per §4.1.EM-005a. |
| `gate` | Launch the Gate-evaluator handler referenced by the node's `handler_ref` per [control-points.md §Node-Type Binding] (C4), passing the node's `gate_ref` as input per [control-points.md §6.3]. | §4.1.EM-005 `Outcome`. Per [control-points.md §Node-Type Binding] (C4), the Gate-evaluator handler's Outcome MAY use `kind = default` at v1; a future `kind = gate_decision` extension is reserved per §4.1.EM-005a's amendment protocol but is NOT required at v1 conformance. |
| `sub-workflow` | Expand per §4.8.EM-034: pin the sub-workflow on the entry checkpoint per §4.8.EM-034c, push the node-ID namespace per §4.8.EM-034a, and descend into the expanded sub-graph. The cascade and durability decision continue to apply within the expansion. | The sub-workflow's terminal-node `Outcome` propagates verbatim to the parent's cascade per §4.8.EM-036a. |

The table is **load-bearing for implementer epics** (the dispatcher in `internal/daemon/` consumes this table to wire the per-type dispatch fork); it is NOT a new state machine. The `agentic`-vs-`non-agentic` distinction collapses at the spec-layer dispatch action (both go through the handler registry) but is preserved at the node-type catalog layer because the four-axis tags per §4.2.EM-011 (`llm-freedom`, `io-determinism`, `replay-safety`, `idempotency`) and the idempotency-class default per §4.2.EM-010 differ between the two types.

Tags: mechanism

### §7.5.5 — Conformance Lift and Forward-Compatibility Reservation

#### EM-059 — `dot` mode conformance lift plus parallel-fan-out reservation

**Conformance lift.** The §10.1 carve-out making `dot` mode a post-MVH extension ("no MVH dispatcher is obligated to drive a `dot` run") is removed per §10.1 / EM-061 below. Once the full phase-3-dot bundle (C1 + C2 + C3 + C4 + C5 + C6) lands, conforming implementations MUST drive `dot` mode per §7.5.1 through §7.5.4. The lift is gated on the C2-related schema bumps being recorded per §6.4: the §4.1.EM-005 `Outcome.failure_class` additive bump (per the C3 amendment) and the §4.2.EM-006 node-type-catalog breaking bump (per the C1 amendment collapsing `control-point` into `gate`/`non-agentic`).

**Parallel fan-out reservation.** MVH `dot` mode dispatches a run's nodes sequentially; parallel-node-type semantics (a node whose dispatch fans out to multiple concurrent sub-dispatches) are deferred to a post-MVH amendment per [architecture.md §4.6]. The deferral is consistent with [workspace-model.md §4.5] and [workspace-model.md §4.7]'s one-agent-per-worktree-at-MVH invariant; any future parallel-fan-out amendment to §7.5 will require a coordinated amendment to workspace-model.md to lift that invariant. No `.dot` schema field, no §6.1 record field, and no §7.4 dispatch primitive is reserved for parallel-fan-out at v1.

**Cross-reference resolutions.** The placeholder text in §4.3.EM-015d ("(cross-ref pending dot-mode spec)") is rewritten to "see §7.5 — `dot` mode binding." The §4.3.EM-012 clause "For `workflow_mode = dot`, reserved keys are out of scope for MVH conformance" is rewritten to "For `workflow_mode = dot`, the per-workflow context-key registry per [workflow-graph.md §Context-Keys] is the authoritative source of allowed `context.<key>` LHS references; this spec reserves no context keys for `dot` mode."

Tags: mechanism

---

## §4.2 — EM-007 Amendment

### EM-007 (amended) — Agentic, non-agentic, and gate nodes carry a handler reference

> **Pass-6 transcription note.** This amendment **replaces in place** the existing §4.2.EM-007 prose. The rewording widens the requirement from "agentic-only" to "agentic, non-agentic, and gate" carrying `handler_ref`. The amendment is consistent with §7.5.3 item 7 and §7.5.4's dispatch table. EM-060 is the bookkeeping handle for this amendment; the normative content lives in the rewritten EM-007 prose below.

#### EM-007 — Agentic, non-agentic, and gate nodes carry a handler reference; sub-workflow nodes do not

A node of type `agentic`, `non-agentic`, or `gate` MUST declare a `handler_ref` resolving to a handler registered per [handler-contract.md §4.1]. A node of type `sub-workflow` MUST NOT declare `handler_ref` (its handler discipline is delegated to the expanded sub-workflow's nodes per §4.8.EM-034). The `handler_ref` semantics per node type:

| Node type | `handler_ref` requirement | Handler role |
|---|---|---|
| `agentic` | MUST declare | Agent subprocess launched per [handler-contract.md §4.1] / [handler-contract.md §4.2]; produces `Outcome` per §4.1.EM-005. |
| `non-agentic` | MUST declare | Deterministic handler invoked per [handler-contract.md §4.1]; produces `Outcome` per §4.1.EM-005. The four-axis tags per §4.2.EM-011 distinguish non-agentic from agentic at authoring time; the spec-layer dispatch action is identical. |
| `gate` | MUST declare | Gate-evaluator handler per [control-points.md §Node-Type Binding] (C4); consumes the node's `gate_ref` and produces `Outcome` per §4.1.EM-005. The pre-C4 prose that `gate` nodes "MUST NOT declare `handler_ref`" is superseded. |
| `sub-workflow` | MUST NOT declare | Handler discipline delegated to the expanded sub-workflow's nodes per §4.8.EM-034. |

The pre-D3 `control-point` node-type per §4.2.EM-006 is collapsed under `gate` and `non-agentic` per the C1 amendment to [workflow-graph.md §Node Types] and the C4 amendment to [control-points.md §Node-Type Binding]; legacy `control-point` nodes are not accepted by the validator post-bundle-landing (the §4.2.EM-006 catalog bump is a §6.4 breaking change).

Reference resolution is validated at workflow ingest per §4.9.EM-038; for `dot`-mode runs the validator additionally enforces §7.5.3 item 7.

Tags: mechanism

---

## §10.1 — Conformance Lift

### §10.1 (amended) — Core MVH and Post-MVH paragraphs

> **Pass-6 transcription note.** This amendment **replaces in place** the §10.1 Core MVH and Post-MVH paragraphs. The rewording removes the `dot`-mode carve-out, gated on the full phase-3-dot bundle landing (C1 + C2 + C3 + C4 + C5 + C6). EM-061 is the bookkeeping handle.

#### EM-061 — Conformance lift: `dot` becomes a first-class mode

**Core MVH (amended).** An implementation conforming to Core MVH MUST pass every requirement in EM-001 through EM-046 (including the sub-requirements enumerated in the prior §10.1 prose), EM-049 through EM-054 (concurrency primitives §4.11, merge-to-main §4.12), **and EM-055 through EM-059 (`dot`-mode binding §7.5)**, plus invariants EM-INV-001, EM-INV-004, and EM-INV-005. For `workflow_mode = single`, the cascade (§4.10), checkpoint cadence (§4.5), and sub-workflow composition (§4.8) apply unchanged. For `workflow_mode = review-loop`, the hardcoded two-node cycle of §4.3.EM-015d MUST be observed; the cap and early-exit rules of §4.3.EM-015e MUST be enforced; the six review-loop events of §6.5 MUST be emitted. **For `workflow_mode = dot`, the input contract (§7.5.1.EM-055), dispatch equivalence (§7.5.2.EM-056), validator obligations (§7.5.3.EM-057), and node-type dispatch table (§7.5.4.EM-058) MUST be observed.** Dispatch input MUST be the active queue per §7.4; daemon fallback to `br ready` is non-conforming. The EM-007 amendment per §4.2 (admitting `handler_ref` on `non-agentic` and `gate` nodes) is normative at Core MVH.

**Post-MVH extensions (amended).** Failure-commit emission (deferred per §4.5.EM-025) and `recoverable-non-idempotent` node-type defaults (§4.2.EM-010) remain additive extensions to Core MVH; neither is required to claim Core MVH conformance. **Parallel fan-out in `dot` mode** (multiple concurrent sub-dispatches from a single node) is a post-MVH extension reserved per §7.5.5.EM-059; the v1 `dot` dispatcher dispatches sequentially. Runtime mutation of `max_concurrent` (§4.11.EM-051) is a post-MVH extension.

**Gating clause.** The `dot`-mode conformance lift above is conditional on the full phase-3-dot bundle landing in the spec corpus: [workflow-graph.md] (C1) drafted at pass-6; [handler-contract.md §Outcome] amendment (C3) drafted at pass-6 with the §4.1.EM-005 `Outcome.failure_class` additive bump recorded per §6.4; [control-points.md §Node-Type Binding] amendment (C4) drafted at pass-6; the canonical [specs/examples/review-loop.dot] artifact (C5) registered with the daemon; and the dispatcher implementation (C6) landed. Until all six are recorded, EM-055 through EM-059 are SHOULD-grade for early implementations and `dot` mode remains a post-MVH extension as in the pre-amendment §10.1 prose. The full lift to MUST-grade for Core MVH lands when the bundle's pass-6 transcription completes (tracked as a coordinated revision-history entry in §12).

Tags: mechanism

---

## Pass-6 transcription checklist

Pass-6 transcribes this draft into `specs/execution-model.md` as follows:

1. **Insert §7.5** (lines following §7.4's end at `specs/execution-model.md:~1303`) — the five EM-055 through EM-059 requirements verbatim from §7.5.1 through §7.5.5 above.
2. **Replace §4.2.EM-007** (lines `specs/execution-model.md:144–148`) — the new EM-007 prose above, including the per-node-type table.
3. **Replace §10.1 Core MVH and Post-MVH paragraphs** (lines `specs/execution-model.md:1380–1382`) — the new prose above, including the gating clause.
4. **Update §3 Glossary** — add a one-line entry for `dot` mode pointing to §7.5 (the existing `workflow_mode` glossary entry at line `:77` already names `dot`; add a §7.5 cross-ref).
5. **Update §4.3.EM-015d** — replace the placeholder "(cross-ref pending dot-mode spec)" with "(see §7.5)."
6. **Update §4.3.EM-012** — replace the clause "For `workflow_mode = dot`, reserved keys are out of scope for MVH conformance" per §7.5.5.EM-059's cross-reference-resolution clause.
7. **Update §12 Revision history** — record the C2 amendment as a coordinated entry naming EM-055 through EM-061, the §10.1 lift gating, and the cross-spec sequencing (C1 + C3 + C4 + C5 + C6).
8. **Update §10.2 Test-surface obligations** — append a bullet for EM-055 through EM-059 naming the dot-ingestion, dispatch-equivalence, validator, and dispatch-table test obligations (round-trip parse of [specs/examples/review-loop.dot]; restart-reparse equivalence; validator negative cases for each of the seven §7.5.3 obligations; dispatch-table coverage for each of the four node types).

---

## Sources consulted

- Design doc: `/Users/gb/.kerf/projects/gregberns-harmonik/phase-3-dot/04-design/C2-execution-model-dot-design.md` (locked APPROVE round-2).
- Spec being extended: `/Users/gb/github/harmonik/specs/execution-model.md` (v0.5.0, status `reviewed`, last-updated 2026-05-14).
- D-decisions consumed: D-attractor-adoption, D-edge-cascade-invariant, D-verdict-surfacing, D1 (failure_class admitted as LHS), D2 (failure_class on Outcome), D3 (4-type node catalog), D4 (LHS whitelist closed at 5), D5 (restricted-equality dialect).
- Cross-component dependencies: C1 (workflow-graph.md drafted in parallel), C3 (handler-contract.md §Outcome amendment), C4 (control-points.md §Node-Type Binding), C5 (specs/examples/review-loop.dot), C6 (dispatcher implementation epic).
