# Change Design — Per-node model/effort selection (D, hk-q8nqr)

> Pass 4 (`change-design`) of `attractor-parity`, component **D**. Normative design for letting a DOT agentic node pick its own `model`/`effort`, overriding the run-level default. Grounded in `03-research/per-node-model/findings.md`. Resolves **OQ-2** (stylesheet normative vs informative).

## 1. Design summary

Add two normative per-node optional attrs — `model` and `effort` — to the `agentic` node type. A node carrying either takes precedence **for that node only** over the run-level `ModelPreference` sealed at claim time (EM-012b). This slots as a new **tier-0** in the EM-012b resolution precedence: a static-graph layer read at dispatch time, ahead of tier-1 (bead labels). The kilroy `class` + `model_stylesheet` CSS mechanism is **INFORMATIVE at v1** (accepted permissively, retained, never dispatched on); a documented authoring-convention note maps it onto the direct attrs. Backwards-compatible: a graph using neither attr behaves exactly as today (every node inherits the run-level pair).

## 2. OQ-2 resolution (normative)

**Per-node `model`/`effort` are NORMATIVE; `class` + `model_stylesheet` are INFORMATIVE at v1.** (Full rationale: research F-D4.) Summary: direct attrs map 1:1 onto the existing `claudeRunCtx.model/.effort` launch seam with zero new mechanism; a stylesheet would re-introduce a parallel selection channel that `01-problem-space.md` §3 explicitly rejects, and add a CSS resolver neither live pipeline's 2-class usage needs. The CSS is sugar over a two-valued intent ("hard node -> stronger model") that `model="opus"` on the classed nodes expresses directly.

## 3. Normative attr definitions

### 3.1 WG-002 catalog rows (workflow-graph.md §4)

Amend the `agentic` row's **optional attrs** column to add `model` and `effort`:

| `type` | required | optional (amended) |
|---|---|---|
| `agentic` | `agent_type`, `handler_ref` | `idempotency_class`, `axis_tags`, `skills_ref`, `freedom_profile_ref`, `budget_ref`, `hook_ref`, `guard_ref`, **`model`**, **`effort`** |

- **`model`** (string, optional). An opaque model alias, validated for **shape only** (`validateModel`: non-empty, ≤128 chars) per EM-012b's value-opacity invariant; harmonik does NOT verify the value names a real model. Handler-side launch failure is the authoritative compatibility check.
- **`effort`** (string, optional). MUST be drawn from the EM-012b closed enum `{low, medium, high, xhigh, max}`. An out-of-enum value is a **strict ingest error** (the graph is static — fail at load, not launch; this is stricter than EM-012b tier-1's runtime "treat-as-absent + emit `bead_label_conflict`", which applies only to runtime bead labels).
- Both are valid ONLY on `agentic` nodes. On `non-agentic`, `gate`, or `sub-workflow` nodes they are reserved-out-of-position strict errors (per WG-031), consistent with those node types not launching an LLM. (A tool/shell node has no model; component A.)

Add a WG-002 note: "`model` and `effort` per-node attrs are the highest-precedence (tier-0) input to the model/effort resolution chain of [execution-model.md §4.3 EM-012b]; they override the run-level default for the node that carries them. `class` and `model_stylesheet` are INFORMATIVE-only at v1 (see WG-00X informative note); a loader MUST accept them permissively per §10 WG-031/WG-032 and MUST NOT dispatch on them."

### 3.2 WG-031 reserved set (workflow-graph.md §10)

Add `model` and `effort` to the reserved attribute-name set so a misplaced occurrence (e.g. `model` on an edge, or on a `non-agentic` node) is a strict ingest error rather than a silently-retained permissive attr. **Do NOT add `class` or `model_stylesheet`** to the reserved set — they remain permissive/informative (warned + retained in `UnknownAttrs`).

Code touchpoint: the reserved-name handling in `internal/workflow/dot/parser.go` node-attr branch (around `parser.go:645-665`) and `Node` struct fields in `internal/workflow/dot/ast.go:86-154` (add `Model string` and `Effort string` typed fields).

### 3.3 EM-012b tier-0 layer (execution-model.md §4.3)

EM-012b currently: "resolved exactly once per run at claim time … MUST NOT be re-evaluated for the lifetime of the run." Amend to add the per-node layer **without contradicting** the resolve-once invariant (research F-D6):

Add a sub-clause (EM-012b-NODE):

> **Per-node override (tier 0).** A `workflow_mode=dot` run's run-level `(model, effort)` pair resolved by the tier-1..4 walk above is the **default** for every node in the run. An `agentic` node MAY carry its own `model` and/or `effort` attribute (per [workflow-graph.md §4 WG-002]); when present, the node's value takes precedence over the run-level default **for that node's dispatch only**. The per-node value is **static graph data** read from the already-loaded, already-validated graph at the node's dispatch — it is NOT a second resolution walk and does NOT re-evaluate bead labels, project config, or compiled defaults. The run-level `ModelPreference` sealed into the Run record (§6.1) at claim time is unchanged by per-node overrides; the override is applied at the launch-spec build for the individual node. `model` and `effort` override independently: a node carrying only `model` inherits the run-level `effort` (and vice versa). This preserves replay determinism — the resolution inputs (run-level seal + static graph attrs) are both fixed at claim/load time.

Tags: mechanism. Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent.

### 3.4 Informative stylesheet note (workflow-graph.md, new informative requirement, e.g. WG-00X)

A new INFORMATIVE-tagged requirement documenting the kilroy `model_stylesheet` + `class` idiom and its recommended port:

> **`class` / `model_stylesheet` (informative).** Upstream kilroy pipelines select per-node models via a graph-level CSS `model_stylesheet` (a `*`-default plus `.hard`-class override) and per-node `class="hard"`. harmonik does NOT interpret these at v1: a loader accepts them permissively (§10 WG-031/WG-032 — warned, retained in the AST's UnknownAttrs) and MUST NOT dispatch on them. To port a kilroy pipeline, translate `.hard { llm_model: <model> }` + `class="hard"` into a direct `model="<alias>"` attr on each classed node (per §4 WG-002 / EM-012b tier-0), and drop `llm_provider` (harmonik's handler binding is fixed per [handler-contract.md §4.1 HC-003]). Promoting the stylesheet to normative (e.g. for >2 model tiers or selector indirection) is a clean future amendment; the direct attrs remain the floor.

## 4. Model-resolution layering (the override mechanism)

Precedence, highest first:

```
tier 0  per-node attr (model="opus" / effort="high" on the agentic node)   [NEW]
tier 1  per-task bead label (model:<alias> / effort:<level>)               (EM-012b, run-level)
tier 2  per-project .harmonik/config.yaml                                  (EM-012b, run-level)
tier 3  per-agent-type compiled default                                    (EM-012b, run-level)
tier 4  built-in fallback (empty)                                          (EM-012b, run-level)
```

Tiers 1–4 produce the run-level pair once at claim time (unchanged). Tier 0 is read per-node at dispatch and substitutes the run-level value into that node's launch spec, independently for `model` and `effort`.

## 5. Code touchpoints (informative — confirms clean-add)

- `internal/workflow/dot/ast.go:86` — add `Model string`, `Effort string` to `Node`.
- `internal/workflow/dot/parser.go:~645-665` — parse `model`/`effort` into the typed fields; add them to the node reserved-name set; reject `effort` outside the closed enum at parse; reject `model`/`effort` on non-`agentic` nodes.
- `internal/workflow/dot/validator.go` — (if validation is staged separately) enforce the `effort` enum + node-type constraint.
- `internal/daemon/dot_cascade.go:204-214` (the `NodeTypeAgentic` branch) / `dispatchDotAgenticNode:414-415` — compute the effective `(model, effort)`: `nodeModel := node.Model; if nodeModel == "" { nodeModel = resolvedModel }` (same for effort), then set `rc.model = nodeModel; rc.effort = nodeEffort`. This is the entire mechanism.
- No change to `internal/daemon/claudelaunchspec.go` — `rc.model`/`rc.effort` already validate (`validateModel`/`validateEffort`, `claudelaunchspec.go:328-353`) and emit `--model`/`--effort` argv. The override rides the existing seam.

## 6. Backwards-compatibility

- **Additive only.** No node-type, edge-field, or enum-member change. Schema stays minor, N-1 readable (WG-034). `schema_version` stays `1`.
- A graph with no per-node `model`/`effort` is byte-for-byte unchanged in behavior (every node inherits the run-level seal).
- Existing graphs carrying a kilroy-style `model_stylesheet`/`class` continue to parse (now formally INFORMATIVE — previously they were already accepted as unknown permissive attrs, so no behavior change).
- The review-loop (v69 live workload) is untouched: its implementer/reviewer nodes carry no `model`/`effort` attr, so they keep using the run-level pair.

## 7. Items NOT clean-additive (flag for integration)

- **EM-012b wording (research R-D1, F-D6).** The tier-0 sub-clause must be worded as static-data layering, NOT a runtime re-resolution, so it does not contradict EM-012b's existing "resolve exactly once / never re-evaluate" invariant. This is the one place this component edits a load-bearing invariant clause — a clarification, not a relaxation. **Pass-6 integration must land this wording precisely.** Everything else is purely additive.
- No cross-component coupling: D is independent of A/B/C/E (it touches `dispatchDotAgenticNode`'s rc-build, a line distinct from the brief-assembly path B/C edit, and unrelated to E's loader change). Parallelizable.
