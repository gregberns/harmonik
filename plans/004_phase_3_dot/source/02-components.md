# Phase 3 — DOT-Defined Bead Processes: Decomposition

> Pass 2 (`decompose`) of the `phase-3-dot` spec work. Maps the 6 audit-identified spec gaps from `01-problem-space.md` to a concrete set of spec components (new files + extensions to existing specs). Drift items (D1–D5) are integration-pass concerns; tracked here only insofar as they touch component dependencies.

## 1. Decomposition strategy

The 6 audit gaps cluster naturally along two axes: **(a) what a workflow graph *is*** (the static artifact + its vocabulary) and **(b) how the run-loop *uses* it** (the driver and its contract with the rest of the system). Examples ride on (a); the failure-class taxonomy bridges both. This yields **5 spec components**:

| # | Component | Kind | Gaps absorbed |
|---|---|---|---|
| C1 | `specs/workflow-graph.md` | new file | G1 (node-type catalog), G2 (edge-condition syntax), G5 (failure-class taxonomy + routing), G6 (schema versioning + repo convention) |
| C2 | `specs/execution-model.md` §`dot` mode | extension | G3 (dispatch-driver contract, ingestion, validation) |
| C3 | `specs/handler-contract.md` §Outcome | extension (cross-check) | G1 partial (per-node-type outcome surface), G5 partial (failure-class field on Outcome) |
| C4 | `specs/control-points.md` §node-type binding | extension (cross-check) | G1 partial (control-point as first-class node type vs. flag — Q1 from pass 1) |
| C5 | `specs/examples/` + canonical `.dot` files | new directory | G4 (example workflows), G6 partial (repo convention validated by example) |

Plus an **implementation-epic decomposition** (C6) — not a spec file, but the beads-graph that pass-6 (`tasks`) will emit. Surfaced here so dependencies are explicit.

Rationale for splitting C1 vs. C2 along the static/dynamic seam: keeping graph-vocabulary in `workflow-graph.md` and dispatch-mechanics in `execution-model.md` mirrors the existing repo split (data specs ≠ runtime specs) and lets implementers parse the graph in isolation from the dispatch loop. The seam is the validated parse tree.

## 2. Component specifications

### C1 — `specs/workflow-graph.md` (NEW)

**Path:** `specs/workflow-graph.md`
**Kind:** New normative spec.
**Purpose:** Defines what a harmonik workflow graph *is* — its node vocabulary, edge semantics, failure taxonomy, and on-disk schema. The primary Phase-3 deliverable.

**Source gaps absorbed:**
- G1 (node-type catalog) — full §Node Types section.
- G2 (edge-condition syntax) — full §Edge Conditions section.
- G5 (failure-class taxonomy + routing) — full §Failure Classes section, partial routing (the rest is in C2).
- G6 (schema versioning + repo convention) — full §Schema Versioning section.

**Dependencies:**
- *Reads:* `.kerf/recon/kilroy-findings.md` (node taxonomy, edge cascade), `.kerf/recon/attractor-findings.md` (Outcome shape, failure classes).
- *Depends on:* none — this is the foundation document. Other components reference *it*.
- *Referenced by:* C2 (driver consumes node-type contract), C3 (Outcome surface per node-type), C4 (control-point node binding), C5 (examples validate against this).

**Open questions to resolve in pass-3 (research) / pass-4 (design):**
- Q1 (from pass-1 §9): is `control-point` a separate node type or an attribute on an existing type? — answer affects both this spec and C4.
- Are `parallel` / `parallel.fan_in` explicitly **deferred** in v1.0 (named-but-stubbed) or **absent** (unrecognized = parse error)? — affects forward-compat semantics in §Schema Versioning.
- Should the failure-class enum live here or in C3 (`handler-contract.md`)? Current lean: enum here, field-on-Outcome there.
- Concrete edge-condition LHS whitelist: which `context_updates` keys are legal to reference? (Research pass should enumerate by surveying existing handler contracts.)

---

### C2 — `specs/execution-model.md` §`dot` mode (EXTENSION)

**Path:** `specs/execution-model.md` (existing; add new section)
**Kind:** Extension to existing spec.
**Purpose:** Specifies the run-loop binding for `workflow_mode = dot` — the dispatch driver, ingestion pipeline, and runtime validation rules. This is the load-bearing integration: everything else in Phase 3 is data feeding this driver.

**Source gaps absorbed:**
- G3 (`dot` dispatch driver) — full coverage.
- G5 partial — runtime side of failure-class-routing (how the driver consumes `failure_class` from Outcome when applying the edge cascade).

**Dependencies:**
- *Depends on:* C1 (driver consumes the validated parse tree + edge-cascade rules defined there).
- *Cross-checks:* existing `execution-model.md` content — must not contradict the run-loop contract already documented. Concurrency-readiness clause (`run_id`-keyed) must be reaffirmed.
- *Referenced by:* C6 (implementation epic — dispatcher bead derives its acceptance criteria from this section).

**Open questions to resolve in pass-3 / pass-4:**
- Q3 (from pass-1 §9): unknown-attribute policy — warn-and-continue vs. refuse-to-run. Driver-level decision.
- Does the driver own ingestion (parse + validate at run start) or is ingestion a separate pre-run pass? Current lean: ingestion is a separate validation pass; the driver assumes a validated graph.
- How does the driver compose with reconciliation startup? On resume, does it re-parse or trust the prior parse tree?
- DOT parsing library: existing dep (e.g. `gonum.org/v1/gonum/graph/encoding/dot`) or a new one? Research pass deliverable.

---

### C3 — `specs/handler-contract.md` §Outcome (EXTENSION)

**Path:** `specs/handler-contract.md` (existing; cross-check + extend §Outcome)
**Kind:** Extension / alignment.
**Purpose:** Ensure the handler-Outcome contract surfaces every field the C1 edge-condition syntax can reference: at minimum `status`, `preferred_label`, `suggested_next_ids`, `context_updates`, and (if adopted) `failure_class`.

**Source gaps absorbed:**
- G1 partial — the per-node-type outcome surface column in C1's node-type table is normatively pinned by this spec.
- G5 partial — `failure_class` field-on-Outcome (the enum lives in C1).

**Dependencies:**
- *Depends on:* C1 (must know the edge-condition LHS whitelist to know what fields Outcome must expose).
- *Cross-checks:* current `handler-contract.md` may already match Attractor's shape; this pass verifies and amends only if drift exists.

**Open questions to resolve in pass-3 / pass-4:**
- Q2 (from pass-1 §9): is `failure_class` required on FAIL outcomes, or engine-inferable? Affects whether this extension adds a required field.
- Does `context_updates` need a typed schema, or remains a free-form map per existing spec?

---

### C4 — `specs/control-points.md` §node-type binding (EXTENSION)

**Path:** `specs/control-points.md` (existing; add cross-reference section)
**Kind:** Extension / cross-check.
**Purpose:** Resolve Q1 — whether `control-point` is a first-class node type in the C1 catalog or a flag (`control_point=true`) on existing types. Wherever it lands, this spec must explicitly point at C1 so the two cannot drift.

**Source gaps absorbed:**
- G1 partial — closes Q1.

**Dependencies:**
- *Depends on:* C1 (the node-type catalog is the authority).
- *Cross-checks:* existing control-points.md — must not contradict the current control-point semantics; this is alignment work, not redesign.

**Open questions to resolve in pass-3 / pass-4:**
- Q1 itself.
- Does the policy-as-YAML binding (operator policy attached to a control point) reference the DOT node by ID, by label, or by both?

---

### C5 — `specs/examples/` + canonical `.dot` files (NEW directory)

**Path:** `specs/examples/review-loop.dot`, `specs/examples/bead-process.dot` (minimum).
**Kind:** New artifacts (data, not prose specs — but normative as "these must round-trip through the validator").
**Purpose:** Anchor the spec with concrete artifacts. Each example doubles as a worked test case for the C2 dispatch driver.

**Source gaps absorbed:**
- G4 (no examples in repo) — full.
- G6 partial — `specs/examples/*.dot` location validates the repo-convention rule in C1.

**Dependencies:**
- *Depends on:* C1 (must conform to schema), C2 (must dispatch cleanly under the documented driver), C3 (handlers referenced must satisfy the Outcome contract).
- *Referenced by:* C6 (dogfood-migration bead targets `review-loop.dot`).

**Open questions to resolve in pass-3 / pass-4:**
- Is a third example warranted (e.g. one exercising failure-class routing explicitly)? Decide once C1 §Failure Classes is drafted.
- Should examples carry inline DOT comments explaining each node's role, or stay terse and rely on a sibling `README.md`?

---

### C6 — Implementation epic decomposition (BEADS GRAPH, not a spec)

**Path:** beads ledger (`.beads/beads.jsonl`); label `epic:phase-3-dot`.
**Kind:** Output of pass-6 (`tasks`), surfaced here so dependencies are explicit.
**Purpose:** Translate the spec components into actionable, dependency-graphed beads for implementation.

**Anticipated beads (NOT created in this pass — pass-6 owns creation):**
- `phase3-parser` — DOT parser (consumes C1 schema).
- `phase3-validator` — schema-version + reachability + start/exit linting (consumes C1).
- `phase3-dispatcher` — the `dot` workflow-mode driver (consumes C2).
- `phase3-outcome-alignment` — handler-contract Outcome field additions (consumes C3).
- `phase3-control-point-binding` — control-points.md alignment (consumes C4).
- `phase3-examples` — author `review-loop.dot` + `bead-process.dot` (consumes C5).
- `phase3-dogfood-migration` — migrate the current Go-coded smoke review-loop to `review-loop.dot` (depends on parser + dispatcher + examples).
- `phase3-drift-cleanup` — close D1–D5 from pass-1 §4 (orchestrator-core.md, concepts/kilroy.md, QUESTIONS.md, components/external/kilroy.md, 00_objective.md).

**Dependencies:** all of C1–C5 must be in spec-draft state before pass-6 creates these beads.

## 3. Dependency graph between components

```
C1 (workflow-graph.md) ──┬──> C2 (execution-model.md §dot)
                         ├──> C3 (handler-contract.md §Outcome)
                         ├──> C4 (control-points.md binding)
                         └──> C5 (specs/examples/*.dot)
                                   └─ also depends on C2, C3

C1..C5 ──> C6 (implementation epic beads, pass-6)
```

C1 is the root; everything else depends on it. C5 depends on C1, C2, and C3 transitively (an example needs schema, dispatch semantics, and handler contracts to round-trip).

## 4. Proposed sequencing

Single-author serial (one agent walking spec-draft) — natural order matches the dependency graph:

1. **C1** first, in full. Without the node-type catalog and edge-condition syntax, nothing else can be drafted.
2. **C3** + **C4** in parallel — both are alignment extensions that depend only on C1's vocabulary; non-conflicting edits to different files.
3. **C2** after C1 is stable — depends on the validated parse tree shape from C1.
4. **C5** last among the specs — needs C1, C2, C3 all stable to write examples that validate cleanly.
5. **C6** is pass-6's deliverable; only kicks off once C1–C5 are spec-draft-complete.

Parallel-author variant (if pass-5 spec-draft is fanned to sub-agents): C3, C4 can run concurrently with C2 *after* C1 lands. C5 is always last.

## 5. Goal-to-component mapping (audit gap coverage check)

| Pass-1 success criterion (§6) | Component(s) |
|---|---|
| #1 `specs/workflow-graph.md` exists, covers G1+G2+G5+G6 | C1 |
| #2 `specs/execution-model.md` has `dot` mode section (G3) | C2 |
| #3 `specs/examples/review-loop.dot` + `bead-process.dot` exist (G4) | C5 |
| #4 5 drift items reconciled (D1–D5) | C6 (`phase3-drift-cleanup` bead) — pass-6 surfaces, integration pass executes |
| #5 Implementation epic decomposed into beads | C6 |
| #6 No locked decision reopened | enforced across all components by reviewer agent at each pass |

All 6 pass-1 success criteria map to at least one component. All 6 gaps map; no component exists that isn't justified by a gap.

## 6. Definition of done (this pass)

Decomposition is complete when:

1. Every audit gap (G1–G6) is assigned to a concrete component above. ✓
2. Every component has: name, path, purpose, source-gaps-absorbed, dependencies, open questions. ✓
3. Dependency graph is acyclic and renderable as text (§3). ✓
4. Every pass-1 success criterion maps to a component (§5). ✓
5. Sequencing is proposed and parallelizable lanes are identified (§4). ✓
6. Reviewer sub-agent has reviewed and returned APPROVE (or REQUEST_CHANGES applied). — to do
7. Status advanced from `decompose` to `research`. — to do

## 7. Open questions surfaced for pass-3 (research)

Carried forward from pass-1 §9 plus newly surfaced in this pass:

- **Q1** (carried): control-point as node type vs. flag — affects C1 + C4.
- **Q2** (carried): `failure_class` required vs. inferable — affects C3.
- **Q3** (carried): unknown-attribute policy — affects C1 + C2.
- **Q4** (carried): graph-level vs. per-node `schema_version` — affects C1.
- **Q5** (new): which Go DOT parsing library — affects C2 (and may add an external-dep constraint check).
- **Q6** (new): edge-condition LHS whitelist enumeration — affects C1 (research pass should survey existing handler-contract outputs).
- **Q7** (new): does the driver re-parse on resume, or trust prior validation — affects C2 + reconciliation interplay.

These do not block pass-2 completion; they are the agenda for pass-3.

---

**Sources:** `01-problem-space.md` (pass-1 of this work); existing specs `specs/execution-model.md`, `specs/handler-contract.md`, `specs/control-points.md` (verified present at pass-2 time); `.kerf/recon/{kilroy,attractor}-findings.md` (prior recon, unchanged).
