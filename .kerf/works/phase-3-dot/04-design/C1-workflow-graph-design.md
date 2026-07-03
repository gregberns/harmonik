# C1 Design — `specs/workflow-graph.md` (NEW spec file)

> Pass-4 (`change-design`) for component C1 of `phase-3-dot`. This document specifies the *target shape* of the new normative spec `specs/workflow-graph.md` — section structure, content shape, and rationale — not the final spec prose itself. Pass-5 (`spec-draft`) writes the prose against this design.

## 1. Current state

There is no `specs/workflow-graph.md`. The vocabulary that such a spec must own is **scattered across three existing specs**, with no single document an author can read to learn "what a harmonik workflow graph is":

- **`specs/execution-model.md`** — owns the bulk of the substrate: EM-001 (workflow = named versioned directed graph), EM-002 (edge fields), EM-005/005a (Outcome shape + `kind`/`payload` discriminator), EM-006 (5-node taxonomy — soon to collapse to 4 per D3), EM-007 (`handler_ref` exclusivity), EM-008 (per-node refs), EM-009/010 (idempotency class), EM-011 (four-axis tags), EM-041 (5-step edge-selection cascade), EM-046a (no-match → `structural` failure), EM-046b (RETRY protocol), §6.4 (schema-version contract, N-1 readable), §8 (6-class failure taxonomy).
- **`specs/handler-contract.md`** — owns the Outcome wire shape (HC-008, HC-008a) and the per-phase LaunchSpec table that constrains what handlers can return; the Outcome envelope itself is locked in EM-005 but the *field semantics* per-phase live here.
- **`specs/control-points.md`** — owns the by-name DOT→YAML attribute binding (CP-002, CP-036) and the per-Kind semantics table (CP-005) that determines which CP Kinds are node-shaped (Gate only, per D3).

This scatter has two costs: **(i)** a new contributor cannot answer "what node types exist, what attributes each carries, what Outcome shapes each may return, and how edges route between them?" from a single document, and **(ii)** the audit-identified gaps (G1 node-type catalog, G2 edge-condition syntax, G5 failure-class taxonomy + routing, G6 schema versioning + repo convention) have no spec home and so cannot be closed by extension — they require a new consolidating spec.

C1 is that spec.

## 2. Target state — section structure of `specs/workflow-graph.md`

The new file is a **normative consolidation spec**: it cites the locked items above by their identifier (EM-006, EM-041, CP-036, etc.) and adds the genuinely-new vocabulary that the cited items do not cover. It does *not* re-define what is already locked. Estimated final length: 600–900 lines of spec prose.

### §1 Purpose

One-paragraph statement: workflow-graph.md is the canonical vocabulary spec for `workflow_mode=dot` workflows; it consolidates node-type catalog, edge-condition syntax, failure-routing semantics, and on-disk schema versioning into a single normative surface. Cites EM-001 (workflow is a directed graph) as the substrate and points at C2 (`execution-model.md §dot`) for runtime mechanics.

### §2 Scope

- **In scope:** the static artifact (node types, attributes, edges, conditions); the schema-version contract for `.dot` files; the repo convention for canonical example workflows; cross-references to per-node-type Outcome surfaces.
- **Out of scope:** runtime dispatch mechanics (C2 owns); the handler-Outcome wire protocol (C3 owns); the Gate/Hook/Guard/Budget Kind contracts (C4 owns); generation of `.dot` from natural-language goals (no spec); dynamic graph mutation (locked-deferred); parallel fan-out (architecture.md §4.6 defers).

### §3 Glossary

Five entries: **workflow graph** (the DOT artifact), **node** (vertex in the graph), **edge** (directed transition), **node type** (one of 4 — per D3), **schema version** (graph-level integer per §6.4). Each entry is two sentences and cites the authoritative locked item.

### §4 Normative requirements

Subsections WG-001 through WG-NNN; numbering follows the existing-spec convention.

#### §4.1 Node-type catalog (closes G1)

**Content shape.** A normative table with one row per node type and columns:

| `type` (ID) | category | required attrs | optional attrs | legal Outcome statuses | Outcome `kind` surface | handler-contract anchor |

Four rows: `agentic`, `non-agentic`, `gate`, `sub-workflow`. The `control-point` row is removed per D3 (`control-point-node-type-design.md`); collapse is recorded as a §6.4-breaking schema bump (v1 → v2) — but since no v1 corpus exists in production, the bump is documentation-only.

Following the table, WG-NNN sub-requirements:
- WG-N01: enumerates the four types; declares the set closed (additions = schema bump).
- WG-N02: agent-type sub-catalog within `agentic`. Adopts research-finding-C from G1 candidates: **open-set posture** — any `agent_type` string is legal; the four-axis tags (EM-011) carry validation weight; `handler-contract.md` lists known agent-types non-normatively. Rationale: pinning a closed enum here would couple C1 to every new handler addition (and harmonik adds handlers frequently); the axis-tag discipline already disciplines what an agentic node can do.
- WG-N03: non-agentic subtype posture is **identical to WG-N02** — open-set, axis-tag-validated. Lint/test/typecheck/build/merge are example subtypes, not a closed enum.
- WG-N04: `gate` node attribute set — `gate_ref` REQUIRED; binds to a Gate-kind ControlPoint in policy YAML per CP-036. Per D3 §"Open follow-up #2", whether `gate` nodes also carry `handler_ref` is flagged for pass-5 reconciliation with EM-007.
- WG-N05: `sub-workflow` node attribute set — `workflow_ref` REQUIRED (target workflow's `name`), `workflow_version` REQUIRED, `input_mapping` OPTIONAL. Cites EM-034 for expansion semantics.
- WG-N06: legal-status matrix per node type — drawn from EM-005 closed status enum `{SUCCESS, FAIL, RETRY, PARTIAL_SUCCESS}`. Gate nodes may NOT return `PARTIAL_SUCCESS` (semantic incoherence — a gate either permits or denies); other types may return any of the four.
- WG-N07: per-D3 ¶"Implications for C1", terminal-node differentiation (close vs. close-needs-attention) — resolved here per D12 (see §4.5 below).

**Rationale.** D3 already settled the catalog shape (4 types, not 5). The open posture on agent-type / subtype enumeration (research candidate C) is picked because (a) EM-011 four-axis tags already do the validation job a closed enum would do, (b) handlers are added frequently and a closed enum would force a C1 amendment per handler, and (c) the recon's Kilroy `box`/`hexagon`/`parallelogram` taxonomy is *visual*; harmonik's taxonomy is *role-and-tag*-based and benefits from open extension.

#### §4.2 Edge schema and edge-selection cascade (closes G2 partial, references EM-041)

**Content shape.**

- WG-E01: edge fields are EM-002's locked set — `from_node, to_node, condition, preferred_label, weight, ordering_key`. No additions.
- WG-E02: edge-selection cascade is EM-041 verbatim. The §"Pass-3 SUMMARY" already-resolved #3 and D-edge-cascade-invariant lock this; C1 cites and does not redefine.
- WG-E03: **unconditional-edge fallback invariant** — restates D-edge-cascade-invariant: if the first four cascade steps yield no match AND an unconditional edge exists, the engine MUST take it before declaring `no_outgoing_edge_matches`. Non-negotiable; cited as a normative invariant on the cascade.
- WG-E04: empty-match-set fallback — cites EM-046a (`structural` failure with reason `no_outgoing_edge_matches`); no redefinition.

**Rationale.** This subsection is almost entirely citation-and-restatement. The audit framed edge fields and cascade as gaps; research established they are locked. The one *new* normative line is WG-E03 (the fallback invariant), which D-edge-cascade-invariant promotes from "audit V3.2 fix" to "day-one invariant." Including it in §4.2 alongside the cascade keeps the invariant visible to anyone reading the cascade rules.

#### §4.3 Edge-condition language (closes G2)

**Content shape.**

- WG-C01: dialect — restricted equality mini-language per D5 (`D5-edge-condition-dialect.md`). NOT CP §6.4's full predicate language. Grammar: `<lhs> == <literal>` or `<lhs> != <literal>` joined by `&&` (logical AND); no parentheses, no OR, no comparison operators beyond equality, no function calls. Pass-5 spec-draft renders the EBNF grammar inline.
- WG-C02: LHS whitelist enumeration per D4 (`D4-edge-condition-lhs-whitelist.md`). The whitelist:
  - `outcome.status` (one of EM-005's four)
  - `outcome.preferred_label` (string)
  - `outcome.failure_class` (one of §8's six; LHS legal per D1)
  - `outcome.kind` (EM-005a discriminator)
  - whitelisted `context.<key>` references for keys declared in the workflow's `context_schema` (per D8, but D8 is open — see §6 below).
- WG-C03: RHS literal types — strings (single-quoted), integers, enum members from §8 (`transient`, etc.) and EM-005 (`SUCCESS`, etc.). No expression on the RHS.
- WG-C04: edge-condition vs. guard-predicate boundary — these are *different dialects*. Edge conditions are the WG-C01 mini-language; guard predicates are CP §6.4. C4 ↔ C1 cross-reference makes the boundary explicit. Rationale: edge conditions are written by workflow authors (high-frequency, needs simplicity); guard predicates are written by policy authors (lower-frequency, can tolerate expressivity).

**Rationale.** D5 and D4 settled dialect and whitelist. The restricted dialect is picked over CP §6.4 wholesale (G2 candidate A) because most authors will write conditions, few will write guard predicates; the cognitive load of two dialects is offset by the per-dialect simplicity. The split also keeps C4's guard surface separable from C1's edge surface, which matches the existing C4 / C1 component boundary.

#### §4.4 Failure-class taxonomy and routing (closes G5)

**Content shape.**

- WG-F01: failure-class taxonomy is §8's locked 6-class set (`transient, structural, deterministic, canceled, budget_exhausted, compilation_loop`). C1 cites; does not redefine.
- WG-F02: classification authority is `handler-contract.md §4.5` (mechanism-tagged `ErrX` sentinels). C1 cites; does not redefine. Note: this preserves the harmonik-vs-Kilroy posture (already-resolved #13 in SUMMARY).
- WG-F03: `failure_class` placement on Outcome per D2 — **top-level field on FAIL outcomes**. Cites C3 for the wire-protocol detail. Required on FAIL; absent on SUCCESS/PARTIAL_SUCCESS; on RETRY the field is permitted but advisory.
- WG-F04: `failure_class` is a legal edge-condition LHS per D1 — workflows MAY route on `outcome.failure_class == 'transient'` etc. Recapped in §4.3's whitelist (cross-reference).
- WG-F05: **no `retry_target` / `fallback_retry_target` attributes** at v1.0 per D16-α — status-primary routing only. RETRY status is handled by EM-046b's same-node re-dispatch protocol; cross-node retry is expressed via `condition="outcome.status == 'FAIL' && outcome.failure_class == 'transient'"` edges. Rationale: the second routing channel (Attractor-style `retry_target`) would compete with the edge cascade and create a "where does retry routing live?" ambiguity. Status-primary keeps the cascade as the single routing authority. D16-β (introducing the attributes later) remains future-tractable.

**Rationale.** D1, D2, D16, D-attractor-adoption all settle the load-bearing calls. WG-F05 is the most consequential subsection — it explicitly *rejects* the Attractor `retry_target` primitive in favor of edge-condition routing, on the grounds that harmonik's mechanism-tagged classification + D1's failure-class-LHS gives equivalent expressivity through a single channel.

#### §4.5 Verdict surfacing and terminal nodes (closes G4 partial, G1 partial)

**Content shape.**

- WG-T01: verdict surfacing for review-loop-style workflows is **via `outcome.preferred_label`** per D-verdict-surfacing. Edges route on `outcome.preferred_label == 'APPROVE'` etc. No first-class `verdict` field.
- WG-T02 (closes D12): **terminal-node differentiation.** Decision: **distinct terminal node IDs** are the canonical mechanism (`close` and `close-needs-attention` are separate nodes in the graph); NO `terminal_kind` attribute, NO edge-label inspection. The walker terminates when it reaches a node with no outgoing edges; the terminal node's *ID* communicates the outcome to the orchestrator. Rationale below.
- WG-T03: reserved terminal-node IDs at v1.0 — `close` (normal completion) and `close-needs-attention` (operator-attention required, e.g. BLOCK verdict, cap-hit, no-progress). Per EM-015d these IDs are already reserved for review-loop mode; C1 makes the reservation general (applies to any `dot` workflow). Additional terminal IDs may be declared by a workflow author (e.g. `close-paused`) — open-set posture, consistent with §4.1 WG-N02/N03.
- WG-T04: terminal-node detection — cites EM-015c (terminal-state detection rule) verbatim.

**D12 rationale (decision: distinct terminal IDs).** Three options were on the table:

1. **`terminal_kind` attribute** on a single `close` node — `terminal_kind={"normal","needs_attention"}`. Pros: one node ID across all workflows; routing logic external to the graph. Cons: introduces a new attribute that downstream consumers (orchestrator, event stream, scenario harness) must read separately from the node ID; the attribute can drift from the node's actual semantics; violates the principle that "node identity = node semantics."
2. **Distinct terminal IDs** (`close`, `close-needs-attention`). Pros: node identity carries the outcome with zero indirection; the orchestrator reads `final_node.id` and routes; matches EM-015d's existing reservation verbatim; the `.dot` file makes the two outcomes visually distinct at the graph layer.
3. **Edge-label inspection** — the last edge taken carries the outcome via `preferred_label`. Pros: no new node mechanism. Cons: requires storing the last-traversed-edge in run state; double-counts with WG-T01 (preferred_label is *route input*, not *route output*); blurs the edge/node boundary.

**Decision: option 2.** Node identity is the cheapest, most direct mechanism; it is already what EM-015d does for review-loop. Generalizing it to all `dot` workflows costs nothing and aligns the catalog with existing review-loop semantics. The cost is "the workflow author must declare two terminal nodes" which is trivially worth the gain.

#### §4.6 Schema versioning (closes G6 partial)

**Content shape.**

- WG-S01: every `.dot` graph carries a graph-level `schema_version="1"` attribute. Required.
- WG-S02 (closes D10): **graph-level only; no per-node `schema_version`.** Per-node versioning was researched as Q4 (pass-1 §9) and the lean was "no." Rationale: harmonik's §6.4 contract is by-field, not by-node-type; additive node-attribute changes within a major version are forward-compatible by definition (per D9, see below); a v2 graph-level bump signals "new vocabulary," not "new mix of old + new vocabulary." Per-node schema_version would create a per-node parse path and would need to compose with the graph-level field in a 2x2 matrix that has no observable benefit.
- WG-S03: §6.4's N-1 readability contract applies — the engine MUST read v1.0 graphs for at least one major-version transition (i.e. a v2 engine reads v1 graphs).
- WG-S04: the workflow's *own* version (EM-001's `version` field) is distinct from `schema_version` — workflow version tracks author-intent revision; `schema_version` tracks vocabulary-format revision. Both REQUIRED, both graph-level, neither composable with the other.
- WG-S05 (closes D9): **unknown-attribute policy is MIXED (Option C).**
  - **Strict** for: (i) the closed `type` enum (the four node types from §4.1), (ii) reserved attributes (`type`, `handler_ref`, `idempotency_class`, `schema_version`, the `*_ref` family from CP-036, `gate_ref`/`workflow_ref`/etc.), (iii) the §8 failure-class enum on RHS literals. An unknown value in a strict position is an ingest error; the run cannot start.
  - **Permissive** for: (i) unknown node/edge attributes not in the reserved set — warning event emitted, attribute retained in the parsed AST (not silently dropped — so debugging is possible) but unused by the dispatcher; (ii) unknown `agent_type` strings on `agentic` nodes (consistent with WG-N02's open-set posture).
  - **Rationale.** Strict-on-enums protects the engine from "v2 graph runs against v1 engine" subtly producing wrong dispatches; permissive-on-attributes preserves §6.4's additive-bump posture, where a new attribute introduced in v1.1 must not break v1.0 readers. Option C threads both requirements; Option A (fully strict) breaks additive compatibility; Option B (fully permissive) breaks the closed-enum guarantees that the cascade depends on.

#### §4.7 Repo convention for `.dot` artifacts (closes G6 partial)

**Content shape.**

- WG-R01 (closes D11): **canonical example workflows live in `specs/examples/`**. The path is normative — the engine's example-loader looks there; the test harness round-trips files in this directory through the validator. Filenames are `<workflow-name>.dot`. At v1.0: `specs/examples/review-loop.dot` is the minimum (per D13, `bead-process.dot` is deferred to a post-Phase-3 follow-up bead).
- WG-R02: **project-local workflow path is out of scope for v1.0.** A future addendum may declare `.harmonik/workflows/` for user-authored workflows, but the v1 engine accepts a `.dot` path argument from any location; there is no normative requirement on project-local layout. Rationale: locking a project-local convention prematurely would couple C1 to a project-layout decision that hasn't been made elsewhere.
- WG-R03: each `.dot` file under `specs/examples/` MUST have a sibling `<name>.md` documenting (a) which spec sections it exercises, (b) the expected golden trace (per C5's two-layer testing plan), (c) any reserved-context-key dependencies. README-mapping per the C5 design.

**D11 rationale.** Declaring `specs/examples/` only (not also a project-local path) is the minimal commitment that closes G6. Adding `.harmonik/workflows/` would require deciding whether that path is read-by-default, override-by-flag, or discovered — none of which are tractable without C2's loader contract being further along. Defer.

#### §4.8 Cross-references

A final §4.8 lists all cross-cutting cites to other specs, so a reader can navigate:

- EM-001/002/005/005a/006 (taxonomy + edge fields + Outcome)
- EM-041 / EM-046a / EM-046b (cascade + failure paths)
- EM-007/008/009/010/011 (node attributes)
- EM-034/034a-c / EM-036a (sub-workflow expansion)
- §6.4 (schema-version contract)
- §8 (failure-class taxonomy)
- HC-005 through HC-008a (handler wire protocol)
- CP-002 / CP-005 / CP-036 (CP attribute binding)
- architecture.md §4.10 (three-artifact separation)
- C2 (this work — execution-model §dot binding)
- C3 (this work — handler-contract §Outcome)
- C4 (this work — control-points §node-type binding)
- C5 (this work — examples directory)

### §5 Non-normative material

Sub-sections that are explanatory, not normative:

- §5.1 Worked example — annotated walk through a minimal three-node graph (`start → work → close`). Shows: node-type attribute, edge `condition`, schema_version line. ~30 lines.
- §5.2 Vocabulary diff table — for each item in C1, which existing spec section it derives from. Helps a reviewer verify nothing was re-invented.
- §5.3 Rationale for `control-point` removal from EM-006 — one paragraph pointer to D3 (`control-point-node-type-design.md`).

### §6 Open items (non-normative — flag for future amendments)

- D7 (gate-node Outcome payload — `kind=gate_decision`) — pending.
- D8 (`context_updates` typing discipline) — pending; affects WG-C02's `context.<key>` LHS resolution.
- D14 (`policy_ref` overload — typed `skills_ref`) — pending; affects WG-N04 attribute table.
- D15 (mechanism-tagged Gate schema-drift handling) — pending.
- D17 (tool-node handler contract) — pending; only material if `bead-process.dot` is revived.
- The EM-007 `handler_ref` exclusion vs. `gate` node-type evaluator dispatch path (D3 follow-up #2) — pass-5 must reconcile.

## 3. Decisions committed in this design (D9–D12 recap)

| ID | Position | Rationale anchor |
|---|---|---|
| **D9 — unknown-attribute policy** | **Mixed** (Option C): strict for the `type` enum + reserved attributes + §8 failure-class RHS literals; permissive for non-reserved attributes (warning, retained in AST). | WG-S05 — threads §6.4's additive-bump contract with the cascade's closed-enum guarantee. |
| **D10 — `schema_version` placement** | **Graph-level only.** No per-node `schema_version`. | WG-S02 — §6.4 versioning is by-field; per-node versioning duplicates the graph-level field without observable benefit. |
| **D11 — in-repo `.dot` paths** | `specs/examples/` for canonical examples. Project-local path **out of scope at v1.0**. | WG-R01/R02 — minimum commitment to close G6; project-local path coupled to a loader decision not yet made. |
| **D12 — terminal-node differentiation** | **Distinct terminal node IDs** (`close`, `close-needs-attention` at v1.0; open-set for additions). NOT `terminal_kind` attribute; NOT edge-label inspection. | WG-T02/T03 — node identity = node semantics; matches EM-015d's existing review-loop reservation; cheapest path. |

## 4. Requirements traceability

`02-components.md §C1` lists the source gaps absorbed and the open questions to resolve. Coverage check:

| C1 requirement (from 02-components.md §C1) | This design's coverage |
|---|---|
| Closes G1 (node-type catalog) — full §Node Types | §4.1 (4-row catalog table + WG-N01–N07); D3 already collapsed catalog 5→4. |
| Closes G2 (edge-condition syntax) — full §Edge Conditions | §4.2 (edge fields + cascade) + §4.3 (dialect via D5, whitelist via D4, failure-class LHS via D1). |
| Closes G5 (failure-class taxonomy + routing) — full §Failure Classes, partial routing | §4.4 (taxonomy cite + placement via D2 + LHS via D1 + retry-channel decision via D16-α). Runtime routing is C2's. |
| Closes G6 (schema versioning + repo convention) — full §Schema Versioning | §4.6 (versioning via D9, D10) + §4.7 (repo convention via D11). |
| Open Q1 (control-point as node-type vs. flag) | Resolved by D3 (`control-point-node-type-design.md`); §4.1 catalog is 4-row. |
| Open: parallel / parallel.fan_in deferral posture | Out-of-scope per pass-1 §5; §2 (Scope) restates. WG-N01 closed-set enum rejects unknown types — `parallel` is absent rather than stubbed. (Aligns with `architecture.md §4.6`.) |
| Open: failure-class enum location (C1 vs. C3) | Enum stays in §8 of execution-model.md (cited from §4.4); the Outcome *field* placement is C3's via D2. Clean split. |
| Open: edge-condition LHS context-key whitelist | §4.3 WG-C02 admits `context.<key>` references; D8 pins the per-workflow registered-key schema (D8 itself pending → §6 open items). |

Pass-1 §6 success-criterion #1 (existence of `specs/workflow-graph.md` covering G1 + G2 + G5 + G6): the §4 structure above maps 1:1 to all four gaps.

## 5. Open questions surfaced (NOT decided here)

These are NOT new D-rows; they are items that arise in C1 but properly belong to other components or future pass-4 rounds:

1. **EM-007 vs. gate-node dispatch path.** EM-007 says non-agentic / gate / sub-workflow MUST NOT carry `handler_ref`. But a Gate evaluator needs *some* dispatch surface (CP §7.2 cognition pseudocode). Pass-5 spec-draft for §4.1 WG-N04 must reconcile — either amend EM-007 to permit `handler_ref` on `gate` for the Gate-evaluator subset, or document a separate dispatch path (CP-Kind evaluator registry) the gate node routes through. (Originally surfaced as D3 open-follow-up #2.)
2. **EM-006 enum collapse breaking-bump.** D3 declared the 5→4 enum collapse a §6.4-breaking change. Pass-5 must (a) bump `execution-model.md`'s schema_version (small documentation change), (b) record the bump rationale in EM-006's history, (c) confirm no in-tree workflow references `control-point` as a node-type value (a trivial grep). C1 §4.1 cites EM-006 by ID; the bump itself is an execution-model.md edit, not a C1 edit.
3. **C2 ingestion site for unknown-attribute warnings.** D9's "permissive — warning event emitted" requires C2 (the loader / validator) to decide *which event* and *at which lifecycle moment* the warning fires. C1 declares the policy; C2 implements the surface.
4. **Schema_version vs. workflow `version` discoverability.** Both fields are graph-level (WG-S01 and WG-S04). Pass-5 must pick the DOT attribute spelling for each (e.g. `schema_version="1"` vs. `version="1.0"`) and document so the two are visually distinguishable in the `.dot` source. (Likely just naming hygiene; flagged so it isn't forgotten.)

---

## 6. Cross-references to other in-progress designs

- **C2 (execution-model.md §dot)** — C1 declares the static contract; C2 declares the dispatcher that consumes it. C1 §4.2/§4.3 are the input to C2's cascade implementation; C1 §4.6 (D9 mixed policy) is the input to C2's validator.
- **C3 (handler-contract.md §Outcome)** — C1 §4.4 cites D2's `failure_class` placement; the field's wire-protocol detail is C3. C1 §4.5 verdict surfacing cites D-verdict-surfacing; C3 declares `preferred_label`'s on-the-wire semantics.
- **C4 (control-points.md §node-type binding)** — C1 §4.1 WG-N04's `gate_ref` attribute is the dual of C4's CP-036 binding. C4 confirms by-name binding; C1 declares which node-type carries which `*_ref`.
- **C5 (specs/examples/)** — C1 §4.7 declares the directory; C5 ships `review-loop.dot` under it (D13 defers `bead-process.dot`). C5's example round-trips through C1 §4.1's catalog + §4.3's dialect + §4.6's schema versioning — it is C1's *executable test case*.

---

## 7. Reviewer note

This is a pass-4 *design document*, not the final spec. Pass-5 (`spec-draft`) writes the prose against this design; a fresh-context reviewer agent should run before pass-5 to confirm (a) no locked decision is reopened, (b) the four D9–D12 positions committed here are internally consistent with the seven landed D-decisions cited (D-attractor-adoption, D-edge-cascade-invariant, D-verdict-surfacing, D1, D2, D4, D5) and with D3, and (c) the §4 section structure covers G1+G2+G5+G6 exhaustively. Open follow-ups in §5 are explicitly NOT closed by this doc and should be tracked into the pass-4 batch that includes D7, D8, D14, D15, D17.
