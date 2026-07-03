# Spec Draft — Per-node model/effort selection (component D)

> Pass 5 (`spec-draft`) of `attractor-parity`, component **D**. Normative requirement text for per-node `model`/`effort` attribute selection on `agentic` DOT nodes, layered as a tier-0 static-data input over the run-level resolution of [execution-model.md §4.3 EM-012b]. Source design: `04-design/per-node-model-design.md`. Resolves OQ-2 (stylesheet normative vs informative).
>
> **Target spec files:** `specs/workflow-graph.md` (§4 WG-002 row, §10 WG-031 reserved set, a new INFORMATIVE requirement) and `specs/execution-model.md` (§4.3 EM-012b amendment, §6.1 Node RECORD). This draft states the requirement text to be merged into those files at integration (pass 6); it is NOT a full-file replacement, per the component-scoped split agreed for this work.

---

## A. `specs/workflow-graph.md` amendments

### A.1 WG-002 — Node type catalog table (amended `agentic` row)

The `agentic` row's **optional attrs** column gains `model` and `effort`. The amended row reads:

| `type` (ID) | category | required attrs | optional attrs | legal outcome statuses | Outcome `kind` surface | handler-contract anchor |
|---|---|---|---|---|---|---|
| `agentic` | LLM-driven | `agent_type`, `handler_ref` | `idempotency_class`, `axis_tags`, `skills_ref`, `freedom_profile_ref`, `budget_ref`, `hook_ref`, `guard_ref`, `model`, `effort` | SUCCESS, FAIL, RETRY, PARTIAL_SUCCESS | `handler_outcome` | [handler-contract.md §4.5] |

All other rows (`non-agentic`, `gate`, `sub-workflow`) are unchanged.

A new note is added to the §4 WG-002 Notes list:

> - `model` and `effort` are valid ONLY on `agentic` nodes. They are the highest-precedence (tier-0) input to the model/effort resolution chain of [execution-model.md §4.3 EM-012b]; when present they override the run-level default for the node that carries them. `class` and `model_stylesheet` are INFORMATIVE-only at v1.0 (see §4 WG-002a); a loader MUST accept them permissively per §10 WG-031/WG-032 and MUST NOT dispatch on them.

### A.2 WG-002 (new requirement) — Per-node `model` and `effort` attributes

> #### WG-002 (per-node model/effort) — Per-node `model` / `effort` attributes on `agentic` nodes
>
> An `agentic` node MAY carry an optional `model` attribute and/or an optional `effort` attribute. When present, the attribute overrides the run-level `(model, effort)` pair sealed at claim time (per [execution-model.md §4.3 EM-012b]) **for that node's dispatch only** (the override mechanism is normative in [execution-model.md §4.3 EM-012b]).
>
> - **`model`** (string, optional). An opaque model alias. A loader MUST validate it for **shape only** — non-empty when present, matching `^[A-Za-z0-9._:/-]+$`, at most 128 characters — per the value-opacity invariant of [execution-model.md §4.3 EM-012b] and [execution-model.md §6.1] `ModelPreference`. harmonik MUST NOT verify that the value names a real model; handler-side launch failure is the authoritative compatibility check.
> - **`effort`** (string, optional). A loader MUST require the value to be a member of the closed enum `{low, medium, high, xhigh, max}` per [execution-model.md §6.1] `EffortLevel`. An out-of-enum `effort` value on a node attribute is an **ingest-time strict error**: the graph is static, so the loader MUST reject it at load and the run MUST NOT start. (This is stricter than tier-1's runtime bead-label path in [execution-model.md §4.3 EM-012b], which treats an unrecognised label as absent and emits `bead_label_conflict`; that runtime relaxation applies only to bead labels, not to static node attributes.)
> - `model` and `effort` are independent: a node MAY carry one without the other. A node carrying only `model` inherits the run-level `effort`, and vice versa.
> - Both attributes are valid ONLY on `agentic` nodes. A `model` or `effort` attribute on a `non-agentic`, `gate`, or `sub-workflow` node, on an edge, or at the graph level, is a reserved-attribute-out-of-position strict error per §10 WG-031.
>
> Tags: mechanism, normative

### A.3 WG-002a (new requirement, INFORMATIVE) — `class` / `model_stylesheet` authoring convention

> #### WG-002a — `class` / `model_stylesheet` (informative)
>
> Some upstream pipelines select per-node models via a graph-level CSS `model_stylesheet` (a `*`-default selector plus a `.hard`-class override) together with a per-node `class` attribute. harmonik does NOT interpret `class` or `model_stylesheet` at v1.0: a loader accepts them permissively per §10 WG-031/WG-032 (warned, retained in `UnknownAttrs`) and the dispatcher MUST NOT route on them. Neither name is added to the §10 WG-031 reserved set.
>
> To port such a pipeline to harmonik, translate each `.hard { llm_model: <model> }` rule plus `class="hard"` into a direct `model="<alias>"` attribute on each classed node (per §4 WG-002 and [execution-model.md §4.3 EM-012b] tier 0), and drop `llm_provider` (handler binding is fixed per [handler-contract.md §4.1 HC-003]). Promoting `model_stylesheet` to a normative selector mechanism (e.g. for more than two model tiers, or selector indirection) is a clean future amendment; the direct `model`/`effort` attributes remain the floor.
>
> Tags: informative

### A.4 WG-031 — Reserved-attribute set (amended)

The reserved set at v1.0 in §10 WG-031 gains `model` and `effort`. The amended reserved-set sentence reads:

> The reserved set at v1.0 is: `type`, `agent_type`, `handler_ref`, `gate_ref`, `sub_workflow_ref`, `workflow_version`, `input_mapping`, `idempotency_class`, `axis_tags`, `policy_ref` (reserved-and-rejected name; see [control-points.md §4.12 CP-056]), `hook_ref`, `guard_ref`, `budget_ref`, `skills_ref`, `freedom_profile_ref`, `model`, `effort`, `schema_version`, `version`, `condition`, `preferred_label`, `weight`, `ordering_key`, `start_node`, `terminal_node_ids`, `context_keys` (graph-level per [handler-contract.md §5.6 HC-062]; see WG-031a).

`class` and `model_stylesheet` are NOT added to the reserved set; they remain permissive/informative (warned and retained in `UnknownAttrs` per WG-031/WG-032 and WG-002a).

The strict-position consequence follows from the existing WG-031 rule: a `model` or `effort` attribute used outside its declared position (i.e. anywhere other than as an attribute on an `agentic` node) is an ingest error and the run MUST NOT start.

### A.5 §13 OQ-WG / §16.1 bookkeeping (informative)

- §16.1 vocabulary-diff table gains a row: "§4 WG-002 per-node `model`/`effort` attrs | New normative content per component D (`attractor-parity`); OQ-2 resolved (direct attrs normative, `model_stylesheet`/`class` informative per WG-002a)."
- OQ-2 (stylesheet normative vs informative) is marked RESOLVED: per-node `model`/`effort` are normative; `class` + `model_stylesheet` are informative at v1.0.

---

## B. `specs/execution-model.md` amendments

### B.1 EM-012b — Model/effort resolution precedence (amended)

The existing EM-012b tier walk (tiers 1–4) and its value-opacity paragraph are unchanged. The "resolve once / never re-evaluate" paragraph (the prose that currently begins "`model` and `effort` are resolved independently … MUST NOT be re-evaluated for the lifetime of the run") is **reworded** to frame the run-level pair as the per-node *default* and to admit the per-node attribute as a static-graph layer applied at dispatch — without contradicting the resolve-once invariant. The reworded paragraph reads:

> `model` and `effort` are resolved independently: each walks the tier list separately, and the first non-empty value wins for that field. The tier walk MUST run exactly once per run at claim time. The resolved `(model, effort)` pair MUST be sealed into the Run record as a `ModelPreference` descriptor (per §6.1) before any node in the run is dispatched, and the **tier walk** MUST NOT be re-evaluated for the lifetime of the run. The sealed pair is the **run-level default**: every node in the run uses it unless the node carries its own per-node override (EM-012b-NODE). The resolved pair MUST be surfaced on the `run_started` event payload per [event-model.md §8.1] for downstream consumers.

A new sub-clause is added immediately after EM-012b's value-opacity paragraph:

> **EM-012b-NODE — Per-node override (tier 0).** Under `workflow_mode = dot`, an `agentic` node MAY carry a `model` and/or `effort` attribute per [workflow-graph.md §4 WG-002]. When present, the node's attribute value takes precedence over the run-level `ModelPreference` default (the tiers-1..4 result sealed per EM-012b) **for that node's dispatch only**. The per-node value is **static graph data** read from the already-loaded, already-validated workflow graph at the moment that node is dispatched; it is NOT a second resolution walk and MUST NOT re-evaluate bead labels (tier 1), project config (tier 2), compiled defaults (tier 3), or the fallback (tier 4). The run-level `ModelPreference` sealed into the Run record (§6.1) at claim time is unchanged by per-node overrides; the override is applied when the per-node launch specification is built, by substituting the node's value into that single node's launch `(model, effort)`. `model` and `effort` override independently: a node carrying only `model` inherits the run-level `effort`, and a node carrying only `effort` inherits the run-level `model`. Because the resolution inputs (the claim-time run-level seal plus the static graph attributes fixed at load time) are both immutable for the run's lifetime, replay determinism is preserved: a replay re-derives the same per-node `(model, effort)` for every node. The per-node value is opaque below the descriptor layer on the same terms as the run-level pair (shape-validated `model`, closed-enum `effort`; handler-side launch failure is authoritative).
>
> Tags: mechanism
> Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

This rewording is a **clarification, not a relaxation**: the "resolve exactly once" guarantee now binds the tier-1..4 walk (the run-level seal); per-node attributes are not a runtime resolution pass but static data layered at dispatch.

### B.2 §6.1 Node RECORD (amended)

The `Node` RECORD in §6.1 gains two optional fields:

> ```
> RECORD Node:
>     …                                         -- (existing fields unchanged)
>     model               : String | None   -- optional per-node model override; shape-validated per EM-012b-NODE; valid only when type = agentic; overrides the run-level ModelPreference.model for this node's dispatch
>     effort              : EffortLevel | None  -- optional per-node effort override; closed enum (§6.1 EffortLevel); valid only when type = agentic; overrides the run-level ModelPreference.effort for this node's dispatch
> ```

No new RECORD or ENUM is introduced (`ModelPreference` and `EffortLevel` are reused). No existing field is renamed or removed.

### B.3 §6.4 Schema evolution (additive note)

The §6.4 schema-evolution log gains an additive entry recording that `Node.model` and `Node.effort` are new optional fields (additive, non-breaking, N-1 readable per §6.4 and [workflow-graph.md §11 WG-034]); a reader at the prior schema treats them as unknown-and-absent and every node uses the run-level `ModelPreference`.

---

## C. Model-resolution precedence (informative summary)

Precedence, highest first, after this change:

```
tier 0  per-node attr (model="…" / effort="…" on the agentic node)   [NEW, EM-012b-NODE; static graph data, per node]
tier 1  per-task bead label (model:<alias> / effort:<level>)         (EM-012b, run-level)
tier 2  per-project .harmonik/config.yaml                            (EM-012b, run-level)
tier 3  per-agent-type compiled default                              (EM-012b, run-level)
tier 4  built-in fallback (empty)                                    (EM-012b, run-level)
```

Tiers 1–4 produce the run-level pair once at claim time (unchanged). Tier 0 is read per-node at dispatch and substitutes the run-level value into that node's launch spec, independently for `model` and `effort`.

---

## D. Backwards-compatibility

- Additive only. No node-type, edge-field, or enum-member change; `schema_version` stays `1`; the change is a minor, N-1-readable bump per [workflow-graph.md §11 WG-034] / [execution-model.md §6.4].
- A graph carrying no per-node `model`/`effort` behaves exactly as before: every node inherits the run-level `ModelPreference`.
- A graph carrying `model_stylesheet`/`class` (previously accepted as unknown permissive attrs) continues to parse, now formally INFORMATIVE per WG-002a — no behavior change.
