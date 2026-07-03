# C2 Design — `specs/execution-model.md` §`dot` mode (Pass-4, phase-3-dot)

> Pass-4 change-design for component C2 of `phase-3-dot`. Scope: the binding amendment to `specs/execution-model.md` that turns `workflow_mode = dot` from a reserved placeholder (EM-012a, §10.1 conformance carve-out) into a live dispatch path. This is a **design document**, not the spec text — pass-5 spec-draft transcribes the design into normative prose.

## 0. Framing — this is a binding amendment, not a new state machine

The C2 amendment is a **binding document**. Research (`03-research/execution-model-dot/findings.md` §0, §1) established that `specs/execution-model.md` already publishes everything a walker needs:

- §7.4 `execute_workflow` is the dispatch loop. It is mode-agnostic: it reads `current_node`, calls `dispatch_node`, applies the §4.5.EM-023a durability decision, runs `select_next_edge`, and advances. There is no `workflow_mode` switch in the loop body.
- §7.3 `select_next_edge` is the 5-step cascade. It already consumes `Outcome.preferred_label`, `Outcome.suggested_next_ids`, edge `condition` expressions, edge `weight`, and `ordering_key` — precisely the Attractor-lineage cascade D-attractor-adoption locks in verbatim, and precisely the surface D1/D4/D5/D-verdict-surfacing widen for `dot` workflows.
- §4.10.EM-041 (cascade), §4.10.EM-042/EM-042a (guards/gates), §4.10.EM-043/EM-043a (traversal caps), §4.10.EM-046a (no-match→`structural`), §4.10.EM-046b (RETRY re-dispatch), §4.8.EM-034 (sub-workflow expansion), §4.5.EM-023a (durability decision), §6.4 (schema evolution), §8 (failure classes) — all already pinned and mode-agnostic.

What is missing for `dot` is exactly: (i) the **input contract** — how a `.dot` file becomes the `Workflow` record §6.1 expects; (ii) the **equivalence statement** — that §7.4 + §7.3 apply unchanged once the Workflow record is loaded; (iii) the **validator obligations** — what static checks gate `dot` runs at claim time; (iv) the **mode-specific carve-outs** — what review-loop artifacts (`reviewer-feedback.iter-N.md`, etc.) are explicitly *not* applicable; and (v) the **conformance lift** — removing `dot` from §10.1's "reserved" carve-out.

The amendment **MUST NOT** re-spec dispatch mechanics already pinned elsewhere. Re-specification risk is the primary failure mode (`findings.md` §0 "Implication"); the design below explicitly cites the existing requirement at every dispatch step instead of restating it.

## 1. Current state — what `execution-model.md` says today about `dot`

### 1.1 Pinned mode-agnostic surface (consumed unchanged by `dot`)

- **EM-005** (`execution-model.md:113`). Outcome envelope: `{status, preferred_label, suggested_next_ids, context_updates, notes, kind, payload}`. The cascade reads this. `dot` mode does not extend it (the `failure_class` addition is D2's amendment to EM-005, not C2's).
- **EM-002** (`execution-model.md:95`). Edge fields: deterministic selection inputs including `condition`, `preferred_label`, `weight`, `ordering_key`, `traversal_cap`. Already the surface a `.dot` edge parses into.
- **EM-006** (`execution-model.md:138`). Node-type taxonomy — locked at 5 today (`agentic, non-agentic, gate, control-point, sub-workflow`). D3 collapses to 4 (`agentic, non-agentic, gate, sub-workflow`); that is D3/C1's bump, not C2's.
- **EM-012 / EM-012a** (`execution-model.md:178`, `:186`). `workflow_mode ∈ {single, review-loop, dot}`, resolved once at claim time, immutable for the run's lifetime, sealed on Run record, surfaced on `run_started` payload.
- **EM-015d / EM-015e** (`execution-model.md:276`, `:343`). Review-loop lifecycle + iteration-cap. EM-015d carves out review-loop from EM-034 sub-workflow expansion ("review-loop is mode-driven, not graph-driven"). EM-015d explicitly says the review-loop sub-graph "is representable as an instance of the general workflow-graph model defined for `dot` mode (cross-ref pending dot-mode spec)" — that cross-ref *is* this amendment.
- **EM-034 / EM-034a..c** (`execution-model.md:§4.8`). Sub-workflow expansion: namespaced node IDs, acyclic reference graph, durable pin on entry checkpoint. Applies verbatim to sub-workflow nodes inside a `.dot` graph.
- **EM-041 / EM-041a** (`execution-model.md:§4.10`). Cascade is the routing authority; context_updates are applied first per EM-041a.
- **EM-042 / EM-042a**. Guards (pre-cascade reorder) and gates (post-cascade permit/deny + `gate-pending` sub-state).
- **EM-043 / EM-043a**. Per-edge `traversal_cap`; counter per-`(run_id, edge)`, in-memory-authoritative, git-derivable on restart.
- **EM-046a / EM-046b**. No-match-edge → `structural`; RETRY re-dispatches same node with retry-cap.
- **§6.1 Workflow record schema**, **§6.4 schema evolution** (`execution-model.md:1125`), **§7.4 main loop**, **§7.3 cascade pseudocode**, **§8 failure-class taxonomy**, **§8.1 reclassification rules**.

### 1.2 What is *missing* or actively-carved-out for `dot` today

- **§10.1 conformance** carves `dot` out: "no MVH dispatcher is obligated to drive it." The amendment lifts this carve-out (gated on phase-3-dot landing).
- **EM-012** (`:182`). "For `workflow_mode = dot`, reserved keys are out of scope for MVH conformance." Lifts to "no keys are reserved by execution-model; per-workflow context-key registration is the workflow author's responsibility per [workflow-graph.md §Context-Keys]" (D8's surface, referenced not specified here).
- **EM-015d cross-ref placeholder**: "(cross-ref pending dot-mode spec)" — gets resolved to a concrete §-ref by the amendment.
- **No `.dot`-file ingestion contract.** Where the file lives (`specs/examples/` for canonical; per-bead `workflow.dot` for instance), how it is parsed, when validation runs, what produces the `Workflow` record §6.1 expects — none of this is normative today. The amendment owns this.
- **No "dispatch by node-type" prose.** §7.4's `dispatch_node` is opaque; the per-node-type dispatch contract (agentic→handler-ref launch, non-agentic→handler-ref invoke, gate→Gate-kind ControlPoint evaluator per D3, sub-workflow→EM-034 expansion) has no spec presence because `single`/`review-loop` don't need a table. The amendment introduces a normative dispatch table — see §5.3 below for the position taken on the open question.

## 2. Target state — structure of the new §7.5 `dot` mode binding subsection

Pass-5 spec-draft will append a new top-level subsection to §7 (Protocols and state machines), numbered **§7.5 — `dot` mode binding**, immediately after §7.4's main-loop pseudocode. Five sub-parts; total ≤2 pages of prose:

### §7.5.1 — Workflow Input Contract

What `.dot` artifact the daemon consumes and how it becomes the §6.1 `Workflow` record.

- **Source of truth.** A `.dot` file conforming to `specs/workflow-graph.md` (C1) §Schema. Path resolution: per-bead `workflow_ref` attribute (Beads field) → canonical `.dot` path; absent a per-bead override, the workflow defaults to the workflow-mode resolution chain per EM-012a tier 3 + 4 (label-derived).
- **Ingestion phase.** Ingestion is a **separate pre-run pass**, not folded into `execute_workflow`. The daemon's claim path (§7.4 lines `resolve_workflow(item.bead_id)` and `validator.validate(workflow)`) is the host. For `workflow_mode = dot`, `resolve_workflow` MUST:
  1. Locate the `.dot` artifact (per source-of-truth rule).
  2. Parse to AST via the C2-chosen DOT library (see §5.5 below).
  3. Convert AST → §6.1 `Workflow` record. Conversion is mechanical (node attributes → Node fields; edge attributes → Edge fields; graph attributes → Workflow header). Schema-version verification runs here (§7.5.3 below).
  4. Return the `Workflow` record to the §7.4 main loop, which then runs `validator.validate(workflow)` per §4.9.EM-038.
- **Resume semantics.** On daemon restart, runs whose `workflow_mode = dot` re-ingest by re-running steps 1–3 against the same source artifact, then re-validate per §4.9.EM-038. The §6.1 `Workflow.workflow_id` + `workflow_version` MUST be identical between pre-restart and post-restart parses; mismatch routes to reconciliation Cat 3 per [reconciliation/spec.md §8.4] (artifact mutated under-foot). The daemon does NOT trust a serialized prior parse tree; reparse is cheap and removes a serialization surface. (Closes `findings.md` §0 OQ-Q7.)
- **Concurrency-readiness.** The parser and `Workflow` record are `run_id`-keyed via the run claim; no daemon-global parser state. Matches STATUS.md §3 concurrency posture.

### §7.5.2 — Dispatch Equivalence with §7.4

The load-bearing equivalence statement, designed to fit in one sentence-cluster:

> *Once `resolve_workflow` returns the §6.1 `Workflow` record for a `dot` run, §7.4 `execute_workflow` and §7.3 `select_next_edge` apply unchanged. The cascade evaluates edge `condition` expressions per the dialect declared in [workflow-graph.md §Edge Conditions] (D5) against the LHS whitelist declared there (D4); routes via `preferred_label` per [workflow-graph.md §Outcome-Carrier Routing] (D-verdict-surfacing); enforces the unconditional-edge fallback invariant per [workflow-graph.md §Cascade Invariant] (D-edge-cascade-invariant); and applies `traversal_cap` per EM-043. No new dispatch-loop state or branching is introduced for `dot`.*

Plus four small clarifying clauses on cross-references that pre-empt confusion:

- **Condition evaluation site (closes OPEN-2 from `findings.md` §2).** Condition evaluation runs **inside `select_next_edge` (daemon-side)**, per the existing §7.3 pseudocode line `evaluate_condition(...)`. Handlers do not evaluate conditions; the cascade does. Restated only to remove ambiguity — no new requirement.
- **Failure-class routing.** When step (a) of the cascade evaluates a condition referencing `outcome.failure_class` (admitted by D1), the value read is `Outcome.failure_class` (the EM-005 field added by D2). On a FAIL outcome where the handler did not set `failure_class`, the daemon's HC-020 sentinel classification runs first and back-fills the field *before* `select_next_edge`. Cite EM-005 v2 (D2's bump) for the back-fill rule.
- **Sub-workflow inside `dot`.** EM-034a/b/c apply. A `dot` workflow may contain `sub-workflow`-type nodes referencing other `.dot` artifacts; expansion is namespaced per EM-034a, acyclic per EM-034b, pinned on entry per EM-034c. **A `dot` workflow MAY NOT reference a `review-loop` sub-workflow** (EM-015d carve-out: review-loop is mode-driven, not graph-driven). The validator enforces.
- **Review-loop artifacts not applicable.** `.harmonik/reviewer-feedback.iter-N.md` and `.harmonik/review-target.md` per EM-015d-RFD / EM-015d-RIA are review-loop-only. `dot` runs do not produce them; their absence on a `dot` run is not an authoring error.

### §7.5.3 — Validator Requirements (the C2-owned static checks)

Static validation gates that run as part of `validator.validate(workflow)` (§4.9.EM-038) on a `dot`-ingested Workflow. Each is normative:

1. **Schema-version compatibility.** The `.dot` artifact's graph-level `schema_version` attribute MUST satisfy the §6.4 N-1 readability rule against the daemon's max-known version; lower-by-more-than-1 OR higher-than-known is a validation failure.
2. **Node-type vocabulary.** Every node's `type` attribute MUST be in the C1 closed catalog (`{agentic, non-agentic, gate, sub-workflow}` per D3). Unknown type is a validation failure (refuse-to-run, not warn-and-continue — closes OQ-Q3 from `findings.md` §0; rationale: silent acceptance of unknown nodes would defeat the catalog's purpose, and the cost of authoring discipline at v1 is trivial).
3. **Edge-condition LHS whitelist.** Every edge's `condition`, if non-empty, MUST tokenize per D5 grammar and reference only D4-whitelisted LHS identifiers. `context.<key>` references MUST be in the workflow's per-workflow registered-key list (D8's surface, referenced not specified here).
4. **Reachability.** Every non-terminal node MUST be reachable from some `start_node`; every terminal node in `terminal_node_ids` MUST be reachable.
5. **Cycle bounding.** Every directed cycle in the graph MUST traverse ≥1 edge carrying a positive-integer `traversal_cap` per EM-043. Cycle detection runs at validation, not at dispatch — Tarjan's SCC is sufficient.
6. **Sub-workflow reference acyclicity.** Per EM-034b; transitively verified across the artifact registry.
7. **Required attributes.** Per D3 + C1's per-node-type attribute table: `agentic` nodes MUST carry `handler_ref`; `gate` nodes MUST carry `gate_ref` AND a Gate-evaluator `handler_ref` (resolves D3 open follow-up #2 in C2's favor: gate nodes do dispatch through the handler registry, with the Gate-kind ControlPoint evaluator as the handler); `sub-workflow` nodes MUST carry `sub_workflow_ref`; `non-agentic` nodes MUST carry `handler_ref`. **NOTE:** current EM-007 prose lists `handler_ref` as `agentic`-only and lists `non-agentic` as MUST-NOT-carry-`handler_ref`. Both the `gate` and `non-agentic` requirements above are pass-5 coordinated amendments to EM-007 (single table row + one sentence each — see §7 follow-up #1).

Failure-mode: any of (1)–(7) failing causes `validator.validate` to return false, and §7.4's `queue.mark_item_failed(item, validation_failed)` path runs — the run never starts.

### §7.5.4 — Node-type Dispatch Table (a normative, but small, addition)

**Position on OPEN-2 from `findings.md` §9 + the user's "Whether to introduce a normative node-type dispatch table" question:** YES — introduce a *small* normative table. (Rationale in §5 below.)

The table maps each of D3's 4 node types to the dispatch action `execute_workflow.dispatch_node` performs. Three columns: node-type, dispatch action, Outcome contract reference.

| Node type | Dispatch action | Outcome contract |
|---|---|---|
| `agentic` | Launch the handler referenced by `handler_ref` per [handler-contract.md §4.1]. | EM-005 `default` kind. Handler MAY emit any `status`; MAY emit `failure_class` as hint on FAIL. |
| `non-agentic` | Invoke the handler referenced by `handler_ref` per [handler-contract.md §4.1]; same dispatch path as `agentic` at the spec layer. | EM-005 `default` kind. |
| `gate` | Launch the Gate-evaluator handler referenced by `handler_ref`, passing the `gate_ref` as input per [control-points.md §7.2]. | EM-005 with `kind = gate_decision` (per D7, pending) OR `kind = default` if D7 hasn't landed at pass-5 time. |
| `sub-workflow` | Expand per §4.8.EM-034: pin sub-workflow on entry checkpoint, push namespace, descend. | The sub-workflow's terminal-node Outcome propagates verbatim to the parent's cascade per EM-036a. |

The table is **3 rows of prose-level distinction** (agentic and non-agentic collapse at the dispatch layer — both go through the handler registry; this collapsing is itself part of the pass-5 EM-007 amendment, since current EM-007 prose says non-agentic MUST NOT carry `handler_ref`). It does NOT introduce a new state machine; it pins the existing dispatch fork that `execute_workflow.dispatch_node` already performs in code but that has no spec presence today.

### §7.5.5 — Conformance Lift + Forward-Compatibility Reservation

- **§10.1 lift.** The `dot` carve-out in §10.1 ("no MVH dispatcher is obligated to drive it") is removed. `dot` becomes a first-class mode at conformance; implementations are required to drive it per §7.5.1–§7.5.4.
- **Parallel fan-out reservation (closes OPEN-3 from `findings.md` §9 + user's question on reserving language).** One sentence: *"MVH `dot` mode dispatches a run's nodes sequentially; parallel-node-type semantics are deferred to a post-MVH amendment per [architecture.md §4.6]."* This pre-positions the worktree-model implication (workspace-model.md §4.5 + §4.7 line 294 — one-agent-per-worktree at MVH) without committing to the fan-out spec shape.
- **EM-015d cross-ref resolution.** EM-015d's "(cross-ref pending dot-mode spec)" is rewritten to "see §7.5 — `dot` mode binding."
- **EM-012 reserved-keys clause.** The "for `workflow_mode = dot`, reserved keys are out of scope for MVH conformance" clause is rewritten to point at C1's per-workflow context-key registration (D8's surface).

## 3. Rationale — why this shape, with cross-refs

### 3.1 Why "binding document," not "new walker subsystem"

`findings.md` §1 surveyed three candidate state-machine shapes and leaned 1A or 1B. The design adopts a **1B-flavored** approach: a discoverable, named §7.5 subsection (mirrors how EM-015d documents review-loop), but its **content** is 1A-shape — a binding statement, not a re-derived state machine. Best of both: discoverability without redundant re-specification.

Adopting 1C (a "walker subsystem" with redeclared dispatch state) was rejected by research and is rejected here for the same reason: it collides with §7.4's authority. There is no observed requirement §7.4 + §7.3 fail to satisfy for `dot` once D1/D4/D5/D-verdict-surfacing + C1 + D3 land. The amendment is **invention-minimal by design**.

### 3.2 Why ingestion is a separate pre-run pass

Two candidates from `findings.md` §C2 OQs: (i) driver owns ingestion (parse+validate at run start); (ii) ingestion is a separate pre-run pass. The amendment adopts (ii). Rationale:

- §7.4 already has `resolve_workflow` and `validator.validate` as named, ordered phases *before* `create_run` runs. Folding parse + validate into these existing hooks costs zero new spec surface.
- The parse + validate cost is paid once per claim, not per-node-dispatch. Restart re-parses; this is `O(file_size)`, cheap.
- Separation keeps `execute_workflow` mode-agnostic in body, which is the entire load-bearing property of the design.

### 3.3 Why edge-condition evaluation is daemon-side, restated

`findings.md` §2 leaned (a) — daemon-side via §7.3's existing `evaluate_condition(...)` call. The design re-states this explicitly in §7.5.2 because:

- The §7.3 pseudocode already does it (no spec text change required); but
- A reader landing in §7.5 for "where do I look for `dot` mechanics?" may assume conditions are a handler concern (since handlers produce Outcome). The one-sentence restatement removes the ambiguity at zero cost.

Rejected: (b) policy-engine evaluation (duplicates EM-042 guard/gate surface), (c) handler-side (breaks Kilroy/Attractor parity per `findings.md` §2). D5's restricted-equality dialect is small enough that daemon-side evaluation is trivial — no expression-tree, no precedence resolver, per D5's design.

### 3.4 Why status-primary, with `failure_class` as condition-LHS only (closes OPEN-1)

**Position on `findings.md` OPEN-1 + user's open question on "status-primary vs. node-level `retry_target`":** adopt α (status-primary, no node-level `retry_target` / `fallback_retry_target` attributes).

Rationale (consistent with D1 and the Attractor-adoption decision):

- D1 admitted `failure_class` as edge-condition LHS. A workflow author wanting "retry on transient, terminate on structural" writes two edges with `condition="outcome.failure_class == 'transient'"` and `condition="outcome.failure_class == 'structural'"`. The graph is the sole routing surface.
- β (Attractor's node-level retry attributes) would introduce a second, parallel routing channel — graph-expressed edges *plus* per-node `retry_target` attributes. This is "where does routing live?" reader confusion. harmonik's centralized-controller principle (recon-Attractor §Adopt #6) and Phase 3's "graph is the routing artifact" framing both push toward α.
- β is also strictly more spec surface (new Node attributes, new dispatch fork in `execute_workflow`). α reuses the cascade.
- If post-MVH evidence accumulates that graph-only routing is unwieldy for failure-recovery patterns, β can land as an *additive* amendment (new optional Node attributes; cascade consults them as a fallback after the existing cascade exhausts). No v1 ratchet.

### 3.5 Why a small normative dispatch table (closes user's open question)

**Position on "Whether to introduce a normative node-type dispatch table":** YES, but make it small.

`findings.md` §8 noted: "the amendment should name 'node-type dispatch table' as a daemon-internal concept; it currently has no spec presence because `single` / `review-loop` don't need it." The design adopts this: the table in §7.5.4 is **4 rows × 3 columns**, citing existing requirements (handler-contract.md §4.1, control-points.md §7.2, §4.8.EM-034) rather than restating them.

Alternative rejected: "leave dispatch-by-type as an implementation detail." Rejected because:

- D3's 4-type catalog has *meaningfully different* dispatch shapes (handler-ref launch vs. sub-workflow expansion vs. Gate-evaluator). Without a table, a reader has to derive the dispatch fork from prose across three specs.
- The table is the only place `gate`'s dispatch contract is named in execution-model (it currently isn't, because gate-node-as-first-class is D3's new framing).
- The table is the load-bearing artifact `phase3-dispatcher` (C6 bead) reads to know which handler-launch path to wire per type. Without it, the implementer derives from three documents.

### 3.6 Why reserve language for parallel fan-out (closes user's open question)

**Position on "Whether to reserve language for post-MVH parallel fan-out":** YES, one sentence in §7.5.5.

`findings.md` §9 OPEN-3 leaned this way. The cost is one sentence; the benefit is that post-MVH parallel fan-out can land as an additive amendment to §7.5 without re-touching §7.4's main loop. The reservation also implicitly aligns with workspace-model.md §4.5 + §4.7 line 294 (one-agent-per-worktree-at-MVH); a future parallel-fan-out amendment will have to amend workspace-model too, and the §7.5.5 reservation flags that as future scope.

Rejected: silence on parallel. Silence is fine technically (the spec doesn't promise parallel, so it isn't there) but it creates a "where would parallel land?" question for future contributors. One sentence pre-answers.

### 3.7 Why §10.1 conformance lift is gated, not unconditional

The amendment lifts §10.1's `dot` carve-out *only when the phase-3-dot work lands as a whole* — i.e., C1, C2, C3 (D2's EM-005 bump), C4, C5 are all spec-draft-complete and the dispatcher (C6 `phase3-dispatcher` bead) is implementation-ready. Pass-5 spec-draft should phrase the lift as conditional on the schema-version bumps in EM-005 (additive, v1→v2 per D2) and EM-006 (breaking, v1→v2 per D3) being recorded per §6.4.

## 4. Requirements traceability — `02-components.md` §C2 → this design

| C2 requirement (02-components.md §C2) | Design section addressing it |
|---|---|
| G3 — `dot` dispatch driver spec | §7.5.1 (input contract) + §7.5.2 (dispatch equivalence) + §7.5.4 (dispatch table) |
| G5 partial — runtime failure-class routing | §7.5.2 clarifying clause on failure-class routing (cites D1 + D2's EM-005 back-fill) |
| Depends on C1 (validated parse tree shape) | §7.5.1 sources `.dot` → `Workflow` record per C1 §Schema; §7.5.3 validator obligations enumerate the C1-defined surfaces (node-type catalog, edge-condition LHS whitelist, schema-version, sub-workflow registry) |
| Cross-checks existing execution-model | §1.1 catalogs the pinned mode-agnostic surface; design body explicitly cites every consumed requirement; §7.5.2 phrasing is "applies unchanged" — no new dispatch state |
| Concurrency-readiness (`run_id`-keyed) | §7.5.1 concurrency-readiness clause; ingestion produces a per-run `Workflow` record, no daemon-global parser state |
| Q3 (unknown-attribute policy) | §7.5.3 item 2: refuse-to-run; rationale in §5.5 (validator strictness rationale subsumed in §3.5's discussion of catalog purpose) |
| OQ — driver owns ingestion vs separate pass | §7.5.1 + §3.2 rationale: separate pre-run pass |
| OQ — driver re-parses on resume vs trusts prior parse | §7.5.1 resume-semantics clause: re-parse, no serialized parse trust |
| OQ — DOT parsing library | §5.5 below: `gonum.org/v1/gonum/graph/encoding/dot` (already a likely-present dep; survey at pass-5 confirms) |
| Referenced by C6 dispatcher bead | The 4-row dispatch table in §7.5.4 is the implementer's spec surface |

## 5. Cross-component design dependencies and open items

### 5.1 C1 (`specs/workflow-graph.md`, in-progress)

C2 cites C1 §Schema, §Node Types, §Edge Conditions, §Outcome-Carrier Routing, §Cascade Invariant, §Context-Keys. C1 must freeze these section names (or pass-5 has to use placeholders + revisit). C2's validator obligations (§7.5.3) are formally **predicated** on C1 — pass-5 spec-draft for C2 must follow pass-5 spec-draft for C1.

### 5.2 C3 (`specs/handler-contract.md` §Outcome, in-progress)

C3 adds `failure_class` field on Outcome (per D2). C2's failure-class routing clarification (§7.5.2) consumes this. C3 must land EM-005 v2 (additive bump) before pass-5 C2 draft can reference the field as normatively present.

### 5.3 C4 (`specs/control-points.md` §node-type binding, in-progress)

C4 owns the `gate` node-type's policy-side wiring per D3. C2's §7.5.4 dispatch-table row for `gate` cross-refs control-points.md §7.2 (Gate evaluator cognition). Open: C4 + EM-007 amendment for `gate` carrying `handler_ref` (D3 open follow-up #2). C2 design takes the position that `gate` MUST carry `handler_ref` to participate in `dispatch_node` — flagged in §7.5.3 item 7 and referenced as pass-5 reconciliation work.

### 5.4 C5 (`specs/examples/*.dot`, depends-on C1+C2+C3)

C5's `review-loop.dot` (and optional `bead-process.dot`) are the round-trip test of §7.5.1–§7.5.4. The §7.5.3 validator obligations must accept the C5 example. Pass-5 sequencing: C2 before C5 (C5 needs §7.5 §-refs to exist).

### 5.5 Open items C2 does NOT close (deferred to pass-5 or beyond)

1. **DOT parsing library choice.** Lean: `gonum.org/v1/gonum/graph/encoding/dot` (already in the Go ecosystem; sufficient for the DOT subset C1 specs). Pass-5 spec-draft surveys go.mod and either confirms or names the alternative.
2. **Reconciliation interplay on re-parse mismatch.** §7.5.1 says reparse-mismatch routes to Cat 3. The exact reconciliation procedure (which artifact wins, how the operator is surfaced) is reconciliation/spec.md's territory; C2 only declares the routing edge.
3. **N-1 readability for `dot` artifacts on schema-version bump.** §6.4 says additive bumps are non-breaking; D2 (EM-005 additive) and D3 (EM-006 breaking) interact with C2 differently. Pass-5 must spell out N-1 reader behavior on `dot`-mode runs that consume v2 artifacts.
4. **Audit-tool detection of `dot`-mode transition records.** EM-020a's existing audit-tool rule covers transition records generically; whether `dot`-mode runs produce any new transition-record fields is settled (no — EM-005 + EM-002 cover all per-node and per-edge state). Pass-5 confirms.
5. **Pathological multi-cycle `traversal_cap` audit.** `findings.md` §4 flagged §11 OQ-EM-004 as still-TODO. Not a C2 blocker; informative cross-ref in §7.5.3 item 5.

## 6. Trade-offs accepted

- **§7.5 cannot stand alone.** A reader who reads only §7.5 without the cited D-decisions and the C1 vocabulary will not understand the cascade, the condition dialect, or the per-node-type Outcome surface. Accepted: §7.5 is a *binding* document, by construction. Discoverability is via cross-refs.
- **The dispatch table duplicates an implicit fork in §7.4's `dispatch_node`.** Accepted (per §3.5): the table is the only place that fork is named in spec, and naming it is load-bearing for C6.
- **`gate` AND `non-agentic` MUST carry `handler_ref`** contradicts the current EM-007 prose, which lists `handler_ref` as agentic-only (and explicitly bans it on non-agentic). Accepted: pass-5 amends EM-007 to cover BOTH cases in concert with the §7.5.3 item 7 requirement. The amendment is small (one row in EM-007's table covering each, plus a one-sentence rewording of the agentic-only constraint).
- **No `outcome.payload.*` LHS at v1.** Per D4. A future `dot` workflow that wants to route on gate-decision rationale or other payload-internal fields has to promote those fields to `preferred_label` or a registered `context.<key>`. Accepted; matches D4's reasoning.
- **No second `bead-process.dot` example required by C2.** C5 (separate component) owns example scope. C2 is round-trip-tested against C5's `review-loop.dot` minimum.

## 7. Open follow-up questions surfaced by this design

These are NOT closed by C2; they become pass-5 spec-draft work or future-amendment scope:

1. **EM-007 amendment scope.** Pass-5 must amend EM-007 to permit `handler_ref` on BOTH `gate` AND `non-agentic` nodes. The amendment is a one-sentence rewording of the agentic-only constraint + a table row update covering both node types; flag as a coordinated change with §7.5.3 item 7 and the §7.5.4 dispatch table.
2. **Outcome `kind = gate_decision` (D7) timing.** D7 has not landed. The §7.5.4 dispatch-table row for `gate` is written with a conditional ("`kind = gate_decision` per D7, pending"). If D7 lands before pass-5 C2 draft, the conditional collapses; if not, pass-5 carries the conditional and a D7-landing follow-up bead in C6.
3. **DOT library survey.** `gonum.org/v1/gonum/graph/encoding/dot` vs. alternatives. Survey at pass-5; no implementer choice committed in spec text.
4. **Reconciliation Cat 3 procedure for re-parse mismatch.** Cross-spec coordination with reconciliation/spec.md §8.4. Not in C2's surface but flagged for pass-5 cross-spec review.
5. **`run_failed` event payload's `failure_class` field already exists** (per pass-3 SUMMARY item #13). Confirm at pass-5 that the cascade-time `Outcome.failure_class` (D2) and the event-time `run_failed.failure_class` carry the same value derived from the terminal Outcome. (D2 open follow-up #3; mirrored here so pass-5 C2 draft does not silently re-derive.)
6. **Conformance-lift gating.** §10.1 lift is gated on the whole phase-3-dot bundle landing. Pass-5 must word the lift conditional so a partial-landing does not accidentally promote `dot` to first-class before C1/C5/C6 are ready.

---

## 8. Sources

- `/Users/gb/.kerf/projects/gregberns-harmonik/phase-3-dot/01-problem-space.md` §3 G3, §3 G5, §6 success criteria, §9 Q3.
- `/Users/gb/.kerf/projects/gregberns-harmonik/phase-3-dot/02-components.md` §C2 (path, kind, purpose, source gaps absorbed, dependencies, open questions Q3/Q5/Q7).
- `/Users/gb/.kerf/projects/gregberns-harmonik/phase-3-dot/03-research/execution-model-dot/findings.md` §0 (already-pinned surface), §1 (state-machine shape candidates; lean 1A/1B), §2 (evaluation site; lean (a) daemon-side), §3.2 (status-primary vs. retry_target; lean α), §4 (cycle bounding confirmed via EM-043), §5 (sub-workflow expansion EM-034 applies), §6 (walker × worktree isolation), §8 (daemon code surprises: no graph parameterization, no dispatch table, `workflowMode` already plumbed), §9 (OPEN-1/-2/-3).
- `/Users/gb/.kerf/projects/gregberns-harmonik/phase-3-dot/03-research/SUMMARY.md` (cross-validates already-resolved items #1, #3, #4, #6, #11, #13).
- `/Users/gb/.kerf/projects/gregberns-harmonik/phase-3-dot/04-design/D-attractor-adoption.md` (Outcome envelope, 5-step cascade, context-bag, unconditional-edge invariant, lowercase status — all adopted verbatim).
- `/Users/gb/.kerf/projects/gregberns-harmonik/phase-3-dot/04-design/D-edge-cascade-invariant.md` (5-step cascade + fallback-to-unconditional invariant; cited in §7.5.2).
- `/Users/gb/.kerf/projects/gregberns-harmonik/phase-3-dot/04-design/D1-edge-condition-lhs-admit-failure-class.md` (admit `failure_class` as LHS — closes OPEN-1 mechanically).
- `/Users/gb/.kerf/projects/gregberns-harmonik/phase-3-dot/04-design/D2-failure-class-placement.md` (top-level Outcome field; daemon back-fills on missing; consumed by §7.5.2).
- `/Users/gb/.kerf/projects/gregberns-harmonik/phase-3-dot/04-design/D4-edge-condition-lhs-whitelist.md` (closed 5-LHS whitelist; consumed by §7.5.3 item 3).
- `/Users/gb/.kerf/projects/gregberns-harmonik/phase-3-dot/04-design/D5-edge-condition-dialect.md` (restricted equality + `&&`; consumed by §7.5.3 item 3).
- `/Users/gb/.kerf/projects/gregberns-harmonik/phase-3-dot/04-design/D-verdict-surfacing.md` (preferred_label is the verdict carrier; consumed by §7.5.2).
- `/Users/gb/.kerf/projects/gregberns-harmonik/phase-3-dot/04-design/control-point-node-type-design.md` (D3: 4-type catalog; consumed by §7.5.3 item 2 and §7.5.4 dispatch table).
- `/Users/gb/github/harmonik/specs/execution-model.md` §3 (workflow_mode glossary), §4.1.EM-005 (Outcome envelope), §4.2.EM-006 (node-type taxonomy), §4.3.EM-012/EM-012a (workflow_mode resolution), §4.3.EM-015d/e (review-loop carve-outs), §4.5.EM-023a (durability), §4.8.EM-034..EM-037 (sub-workflow), §4.9.EM-038 (validator), §4.10.EM-041..EM-046b (cascade + caps + RETRY), §6.1 (Workflow/Run record schemas), §6.4 (schema evolution), §7.3 (cascade pseudocode), §7.4 (main-loop pseudocode), §8 (failure-class taxonomy), §10.1 (conformance carve-out — to be lifted).

---

## Footer — Reviewer note

Reviewer sub-agent was not dispatched in this pass; the Agent tool is not available in this thread. Per the C2 brief, a **fresh-context re-read pass** was performed against (a) the binding-document framing (§0), (b) the five sub-parts of §7.5 (§2.1–§2.5 → §7.5.1–§7.5.5), (c) the four open-decision positions taken (condition-evaluation site, α status-primary, dispatch table inclusion, parallel-fan-out reservation), and (d) the requirements traceability matrix (§4). Re-read confirmed: no design choice contradicts a landed D-decision (D-attractor-adoption, D-edge-cascade-invariant, D1, D2, D3, D4, D5, D-verdict-surfacing); no §7.5 element re-specifies an EM requirement already pinned; the EM-007 amendment for `gate` carrying `handler_ref` is correctly surfaced as a pass-5 follow-up rather than papered over; the §10.1 conformance lift is correctly gated on the full phase-3-dot bundle landing. No BLOCK-grade issues identified in re-read. Recommend a fresh-context reviewer agent run before pass-5 spec-draft begins.
