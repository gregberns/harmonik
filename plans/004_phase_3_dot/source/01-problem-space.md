# Phase 3 — DOT-Defined Bead Processes: Problem Space

> Pass 1 (`problem-space`) of the `phase-3-dot` spec work. Absorbs the 2026-05-15 Phase-3 DOT audit findings (6 spec gaps + 5 drift items) into a structured planning surface so subsequent passes (decompose → research → design → spec-draft → integration → tasks) have a single normative input. Prior recon already done; this doc synthesizes, it does not redo research.

## 1. Problem framing

Phase 3 of harmonik's North Star — "DOT-defined bead processes" — is the project's **strategic gap**. Phase 1 (operational smoke GREEN) landed 2026-05-14; Phase 2 (orchestrator dispatches *via* harmonik instead of via sub-agents) is in flight. Phase 3 is the unstated product thesis: a bead's *process* (how it gets worked) is itself a graph artifact, not a code path baked into the binary. The agent reads the DOT, walks the nodes, dispatches at each step, and routes on outcomes.

Current state: **decided + reconned but not implemented.**
- Decision #11 (DOT as workflow representation) is locked.
- The 5-node taxonomy (agentic / non-agentic / gate / control-point / sub-workflow) is locked.
- Three-artifact separation (spec / workflow / bead) is locked.
- Kilroy's "one node = one commit" checkpoint invariant is the substrate model.
- Attractor's Outcome-shape (status + preferred_label + suggested_next_ids + context_updates) is the routing input.
- Two prior recon docs are in `.kerf/recon/`: `kilroy-findings.md` (195 lines, full attractor-spec + kilroy-metaspec digest) and `attractor-findings.md` (144 lines, Attractor-vs-Kilroy-vs-DTW comparison + adopt/don't-adopt list).

What is **not** decided / not written: the concrete catalog of node types harmonik will accept, the edge-condition syntax it will parse, the dispatch driver that switches `workflow_mode = dot` in the run loop, any example `.dot` files in-repo, the failure-class → edge-routing cascade, the DOT schema-version field, and the in-repo location for `.dot` artifacts. Until these are spelled out as normative specs, Phase 3 cannot start. The audit at session top identified these as **6 gaps + 5 drift items**.

## 2. Locked decisions referenced

| Decision | Anchor | Source |
|---|---|---|
| #11 — DOT as workflow representation | locked 2026-04-20 | `STATUS.md:104` |
| 5-node taxonomy (agentic / non-agentic / gate / control-point / sub-workflow) | locked | `docs/subsystems/orchestrator-core.md` (node-types section) |
| Three-artifact separation (spec ≠ workflow ≠ bead) | locked | `~/.claude/.../memory/project_harmonik_state_source_of_truth.md` |
| Checkpoint-per-node-as-git-commit (Kilroy invariant) | locked | `.kerf/recon/kilroy-findings.md` §Checkpointing |
| Outcome shape (status + preferred_label + suggested_next_ids + context_updates) | locked | `.kerf/recon/attractor-findings.md` §Workflow model |
| Edge-selection cascade (5-step: condition → preferred_label → suggested_ids → weight → lexical) | adopted from Attractor | `.kerf/recon/attractor-findings.md` §Replay |
| Beads as task ledger; `br` CLI only | locked #13 | `STATUS.md:104` |
| Handler-contract skill injection | locked #14 | `STATUS.md:104` |
| No DTW (no Temporal-style event-sourced replay) | locked #12 | `STATUS.md:104` |
| Concurrency-readiness (run_id-keyed, no shared globals) | locked 2026-05-08 | `STATUS.md:134-140` |

Reopening any of these requires explicit user direction; this work assumes all are in force.

## 3. The 6 spec gaps

Each gap is "must be answered before Phase 3 implementation can start." Proposed-resolution-path is the candidate Pass-4-design direction; not yet committed.

### Gap 1 — Node-type catalog is not concrete

The 5-node taxonomy names categories but does not enumerate the concrete types in each. Kilroy ships 8 (start, exit, codergen, wait.human, conditional, parallel, parallel.fan_in, tool); Attractor adds nothing beyond. Harmonik has not yet decided which of those it adopts, which it renames, which it splits (e.g. is `wait.human` one type or several?), which it adds for harmonik-specific needs (control-point as first-class type), or which it explicitly defers.

**Proposed resolution path:** spec-draft pass produces `specs/workflow-graph.md` §Node Types — a normative table mapping every accepted node type to its category, its handler contract, its allowed attributes, and its outcome surface. Use Kilroy's 8 as the starting set; add `control-point` and `sub-workflow` as harmonik-specific; defer `parallel`/`parallel.fan_in` to a post-Phase-3 addendum if user wants concurrent-branches deferred.

### Gap 2 — Edge-condition syntax is unspecified

The edge-selection cascade is locked (5 steps), but the **syntax** of step 1 ("`condition` match") is not. Kilroy uses small expressions like `condition="outcome=fail"` and `condition="outcome=needs_dod"`. Are these literal key=value strings? CEL? A bespoke mini-language? What's the type system? Can a condition reference `context_updates` keys, or only `Outcome.status`? Failed-edge-fallback behavior (when all conditions miss) is not specified.

**Proposed resolution path:** spec-draft produces `specs/workflow-graph.md` §Edge Conditions — adopt Kilroy's `key=value` literal form for v1 (no CEL); enumerate the keys accessible on the LHS (`outcome`, `failure_class`, plus a whitelist of context keys); define the "no condition matches" fallback path (route to unconditional edges; if none, terminate with `structural` failure).

### Gap 3 — `dot` workflow-mode dispatch driver is not written

`workflow_mode = dot` is referenced as the Phase-3 unlock but the dispatch driver doesn't exist. The driver is the binding between (a) the run-loop ("we just finished node X with outcome Y, what's next?") and (b) the DOT parse tree ("apply the edge-selection cascade against the outgoing edges of X"). It is the load-bearing integration point; everything else in Phase 3 is data.

**Proposed resolution path:** spec-draft produces `specs/execution-model.md` §`dot` mode — define the driver's contract as a pure function `(graph, current_node, outcome, context) → next_node | terminate`; specify how it composes with the existing run-loop; specify ingestion (`graph := parse(.dot file)`) and validation (reachability, ASCII identifiers, start/exit linting per Kilroy's ingestor-spec).

### Gap 4 — No example `.dot` files in repo

There is no canonical `review-loop.dot`, no `bead-process.dot`, nothing implementers and reviewers can point to. The recon recommends `consensus_task.dot`-style examples; harmonik has zero. Without examples the spec is unanchored.

**Proposed resolution path:** spec-draft includes a minimum of **two** example `.dot` files committed under `specs/examples/`: (i) `review-loop.dot` — single agentic node + reviewer node + retry edge (the dogfood smoke pattern); (ii) `bead-process.dot` — claim → work → review → close with control-point gates. Each example double-acts as a worked test case for the dispatch driver.

### Gap 5 — Failure-class → edge-routing cascade unspecified

Attractor defines 6 failure classes (`transient_infra`, `budget_exhausted`, `compilation_loop`, `deterministic`, `canceled`, `structural`); Kilroy's concept digest compresses to 3 (lossy summary). Harmonik has not picked which set is canonical, nor specified how a failure class influences edge routing beyond the basic `condition="outcome=fail"` form. The recon flags this as an open Attractor-vs-Kilroy tension: status-driven (Kilroy spec) vs class-driven (Kilroy code) — harmonik must pick one as primary.

**Proposed resolution path:** spec-draft produces `specs/workflow-graph.md` §Failure Classes — adopt the full 6-class Attractor taxonomy; declare **status** as the primary routing input (matching Attractor spec, against Kilroy code's drift); allow `failure_class=X` as a secondary condition LHS for more granular routing; specify retry cascade as `fail-edge → retry_target → fallback_retry_target → terminate` (Attractor §3.7).

### Gap 6 — DOT schema versioning + repo convention unspecified

No `schema_version` attribute is required on the graph; no in-repo location is mandated for `.dot` files; no rule about whether `.dot` files belong in `specs/` (as examples) vs. an out-of-tree workflow library. Forward compatibility is therefore undefined: if v2 adds a new node type, how does the engine handle a v1 graph?

**Proposed resolution path:** spec-draft produces `specs/workflow-graph.md` §Schema Versioning — require a graph-level `schema_version="1.0"` attribute; the engine refuses to run graphs with a major version it does not recognize; minor-version skew is permissive (unknown attributes ignored with a warning). Repo convention: `specs/examples/*.dot` for canonical examples shipped with the spec; user-authored workflows live in a project-local `.harmonik/workflows/` directory (out of repo); the engine accepts both.

## 4. The 5 drift items

These are existing docs that contradict the locked decisions; they must be reconciled as part of this work (likely in the spec-draft or integration pass — not pass 1).

| # | Document | Drift | Source |
|---|---|---|---|
| D1 | `docs/subsystems/orchestrator-core.md` | Misclassifies Attractor as DTW reference; should be reframed as graph-as-workflow runner | `.kerf/recon/attractor-findings.md` §Corollary for the docs |
| D2 | `docs/concepts/kilroy.md` | Stale fidelity-mode count (says 4; spec says 6) | `.kerf/recon/kilroy-findings.md` §Context/fidelity |
| D3 | `QUESTIONS.md` | Missing resolution notes for Q-R1 (replay-determinism scope), Q-R2 (failure-class authority), Q-A2 (node-contract shape) — all now answered by locked decisions + recon | audit session top |
| D4 | `docs/components/external/kilroy.md` | No Kilroy+Attractor coverage of differences; no harmonik-divergence section (e.g. non-FF merges, dynamic graph mutation, cross-pipeline coordination) | audit session top |
| D5 | `docs/00_objective.md` / OVERVIEW | DOT-in-specs (examples) vs. real-runtime-`.dot` distinction unclear to a new reader | audit session top |

## 5. Out of scope for this work

Explicit non-goals, to keep this pass-tractable:

- **NL → DOT generation.** Translating a natural-language goal into a DOT workflow is its own research project; outside Phase 3.
- **Parallel branches (`parallel` / `parallel.fan_in`).** Locked-deferred per the concurrency-readiness decision; revisit post-Phase-3 once single-threaded `dot` mode is operational.
- **Sub-pipeline composition (Kilroy's `stack.manager_loop`).** Kilroy itself stubbed this to FAIL in v1; harmonik should explicitly defer.
- **Dynamic graph mutation at runtime.** Static-after-parse is the locked posture (matches Kilroy).
- **Non-`dot` workflow modes.** Other modes (e.g. a hypothetical YAML mode) may exist later; Phase 3 specs only `workflow_mode = dot`.
- **CXDB / non-git substrate.** Harmonik's substrate is git per the checkpoint-per-node decision; no CXDB equivalent in scope.
- **Migration of existing hardcoded workflows** (e.g. the dogfood smoke review-loop currently coded in Go) to DOT. Migration is a Phase-3-follow-up, not Phase 3 itself; but `review-loop.dot` shipping as an example doubles as the migration target.
- **3-way merge fan-in.** Kilroy is FF-only; harmonik may eventually want real merges (Gas Town posture) but not in Phase 3.

## 6. Success criteria

`phase-3-dot` work is complete when:

1. **`specs/workflow-graph.md` exists**, normative, covering: node-type catalog (Gap 1), edge-condition syntax (Gap 2), failure-class taxonomy + routing (Gap 5), schema versioning + repo convention (Gap 6).
2. **`specs/execution-model.md` has a `dot` mode section** (Gap 3) specifying the dispatch-driver contract, ingestion, and validation rules.
3. **`specs/examples/review-loop.dot` and `specs/examples/bead-process.dot` exist** (Gap 4) and round-trip through the documented validation rules.
4. **The 5 drift items are reconciled** — orchestrator-core.md / concepts/kilroy.md / QUESTIONS.md / components/external/kilroy.md / 00_objective.md updated; each change cites the locked decision or recon source that motivates it.
5. **An implementation epic is decomposed into beads** — at minimum: parser bead, validator bead, dispatch-driver bead, review-loop-example dogfood bead, drift-doc-cleanup bead — each with a clean dependency graph and a single labeled `epic:phase-3-dot`.
6. **No locked decision was reopened** without explicit user sign-off recorded in the integration pass artifact.

## 7. Preliminary spec areas affected

For pass-2 (decompose) seed only — not committed:

- `specs/workflow-graph.md` (new) — primary deliverable
- `specs/execution-model.md` (extend) — `dot` mode driver
- `specs/handler-contract.md` (extend) — Outcome shape if the current contract lags Attractor's
- `specs/control-points.md` (cross-check) — control-point node type must align with existing control-points spec
- `specs/examples/*.dot` (new directory) — canonical examples

## 8. Constraints

- **Backwards compatibility:** the run-loop already exists and dispatches hardcoded workflows; `dot` mode must be an additive workflow_mode, not a replacement. Existing smoke tests must continue to pass.
- **Concurrency-readiness:** the dispatch driver must be `run_id`-keyed; no shared global state across runs (per STATUS.md:134-140).
- **Git substrate assumed:** the spec may assume a clean git working tree and a per-run branch; non-git substrates are out of scope.
- **No new external dependencies** without explicit user sign-off; DOT parsing should use an existing Go library already in the dep graph if possible.

## 9. Open questions for pass-2

(Not for resolution here — surfaced so decompose pass picks them up.)

- Q1: Does `control-point` deserve its own node type, or is it a flag (`control_point=true`) on an existing type, paralleling `goal_gate=true`?
- Q2: Should `Outcome.failure_class` be a required field on FAIL outcomes, or inferable by the engine as a safety net (per Kilroy's spec-code tension)?
- Q3: Where does the spec-vs-code authority sit when the parser sees an unknown attribute — warn-and-continue (forward-compat) or refuse-to-run (strict)?
- Q4: Is `schema_version` graph-level only, or also per-node (to allow node-type additions without bumping the whole graph)?

---

**Sources:** `.kerf/recon/kilroy-findings.md`, `.kerf/recon/attractor-findings.md`, `STATUS.md` §Decisions, `docs/subsystems/orchestrator-core.md`, `QUESTIONS.md`, 2026-05-15 Phase-3 DOT audit (session transcript).
