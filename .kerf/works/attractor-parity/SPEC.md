# Unified Spec Draft — `attractor-parity`

> Integrated, conflict-free spec draft produced by Pass 6 (Integration) from the five component drafts in `05-spec-drafts/` (A tool-node, B inline-prompt, C non-committing, D per-node-model, E goal-template-params). Reconciliation decisions, the OLD→NEW WG-ID map, the merged-row text, and the observability-event decision are recorded in `06-integration.md`.
>
> **Scope:** parity amendments to the `workflow_mode = dot` surface so harmonik can natively author the per-node / per-graph capabilities of an upstream attractor pipeline. All changes are **ADDITIVE** and **N-1-readable** (`schema_version` stays `1`): no new node type, no new enum member, no new edge field; the Outcome envelope (EM-005/005a/005c) and the edge cascade (EM-041) are unchanged; the v69 `review-loop` path does not regress.
>
> **Target spec files (amendments grouped per file below):** `specs/workflow-graph.md`, `specs/execution-model.md`, `specs/handler-contract.md`. No `event-model.md` amendment is required (see §observability decision below).
>
> **Final WG-ID allocation** (next free sequential block after the live max WG-038):
>
> | FINAL ID | Component | Requirement |
> |---|---|---|
> | **WG-039** | A | Tool-command attributes (`tool_command` / `timeout`) on `non-agentic` nodes |
> | **WG-040** | B | Inline `prompt` attribute on `agentic` nodes |
> | **WG-041** | C | `non_committing` attribute on `agentic` nodes |
> | **WG-042** | D | Per-node `model` / `effort` attributes on `agentic` nodes |
> | **WG-043** | D | `class` / `model_stylesheet` authoring convention (INFORMATIVE) |
> | **WG-044** | E | Graph-level `goal` attribute |
> | **WG-045** | E | Template-param substitution over `.dot` source text |
> | **WG-046** | E | Substitution ordering invariant |
> | **HC-063** | A | Built-in `shell` handler for tool nodes (no WG collision; HC-062 was live max) |

---

# Part I — `specs/workflow-graph.md` amendments

## I.1 §4 WG-002 — Node type catalog table (merged)

Replace the `agentic` and `non-agentic` rows of the §4 WG-002 table with the merged rows below. The `gate` and `sub-workflow` rows are unchanged.

```
| `type` (ID) | category | required attrs | optional attrs | legal outcome statuses | Outcome `kind` surface | handler-contract anchor |
|---|---|---|---|---|---|---|
| `agentic` | LLM-driven | `agent_type`, `handler_ref` | `prompt`, `non_committing`, `model`, `effort`, `idempotency_class`, `axis_tags`, `skills_ref`, `freedom_profile_ref`, `budget_ref`, `hook_ref`, `guard_ref` | SUCCESS, FAIL, RETRY, PARTIAL_SUCCESS | `handler_outcome` | [handler-contract.md §4.5] |
| `non-agentic` | deterministic | `handler_ref` | `tool_command`, `timeout`, `idempotency_class`, `axis_tags`, `budget_ref`, `hook_ref`, `guard_ref` | SUCCESS, FAIL, RETRY, PARTIAL_SUCCESS | `handler_outcome` | [handler-contract.md §4.5] |
```

Append to the §4 WG-002 Notes list:

> - `prompt` (§4 WG-040), `non_committing` (§4 WG-041), `model`, and `effort` (§4 WG-042) are valid ONLY on `agentic` nodes. `model`/`effort` are the highest-precedence (tier-0) input to the model/effort resolution chain of [execution-model.md §4.3 EM-012b]; when present they override the run-level default for the node that carries them. `class` and `model_stylesheet` are INFORMATIVE-only at v1.0 (see §4 WG-043); a loader MUST accept them permissively per §10 WG-031/WG-032 and MUST NOT dispatch on them.
> - `tool_command` and `timeout` (§4 WG-039) are valid ONLY on `non-agentic` nodes; a `non-agentic` node carrying `tool_command` is a **tool node** dispatched by the built-in `shell` handler ([handler-contract.md §4.1 HC-063]).

## I.2 §4 WG-039 — Tool-command attributes on `non-agentic` nodes (NEW; component A)

Insert after WG-008 in §4.

> ### WG-039 — Tool-command attributes on `non-agentic` nodes
>
> A `non-agentic` node MAY carry a `tool_command` (string) optional attribute. When `tool_command` is present, the node is a **tool node**: at dispatch time the run executes `tool_command` as a shell command in the run's worktree per the built-in `shell` handler of [handler-contract.md §4.1 HC-063], and the command's exit state is mapped to an Outcome per [handler-contract.md §4.1 HC-063] / §7 WG-017.
>
> A `non-agentic` node MAY carry a `timeout` (integer, seconds) optional attribute. `timeout` is the wall-clock kill bound for the command; when absent, the loader applies a default of `300` seconds. `timeout` is only meaningful on a node that also carries `tool_command`; a `timeout` attribute on a `non-agentic` node without `tool_command` is retained in the AST and ignored.
>
> A `non-agentic` node WITHOUT `tool_command` is unchanged from prior behavior: it carries no tool semantics and its handler dispatch is governed by the §4 WG-002 `non-agentic` row and [handler-contract.md §4.1].
>
> A tool node MUST carry `handler_ref="shell"` (the built-in shell handler of [handler-contract.md §4.1 HC-063]). The `handler_ref="shell"` requirement satisfies the §4 WG-002 `non-agentic` row's required-`handler_ref` obligation and [execution-model.md §7.5.3 EM-057] item 7. A `tool_command` present on a node whose `handler_ref` is not `shell` is a validation warning at v1 (the loader emits the §10 WG-031 warning event and retains the node); it is reserved to become a strict error at the next schema major bump.
>
> **Trust boundary (normative).** `tool_command` is a literal shell string supplied by the `.dot` author. The `.dot` author is a trusted operator. At v1 the value is passed verbatim to `/bin/sh -c`; there is NO sandboxing, NO argument escaping, and NO allow-list of permitted commands. A workflow that admits an untrusted `.dot` author admits arbitrary command execution in the run's worktree under the daemon's privileges. Operators MUST treat `.dot` artifacts as trusted code, equivalent to a checked-in shell script.
>
> Tags: mechanism, normative

## I.3 §4 WG-040 — Inline prompt attribute on `agentic` nodes (NEW; component B)

Insert in §4 after WG-039.

> ### WG-040 — Inline prompt attribute on `agentic` nodes
>
> An `agentic` node MAY carry a `prompt` (string) optional attribute. `prompt` is the node's brief: the natural-language instruction the agent receives for this node's dispatch.
>
> When `prompt` is present on an **implementer-class** `agentic` node, it REPLACES the bead-derived task body for that node's dispatch: the agent's task brief is `prompt` verbatim, not the bead's body. The bead `Title` and bead ID remain in the per-dispatch task artifact's header for traceability (per [handler-contract.md §4.2 HC-006a, the `agent-task.md` content row]); only the body is overridden.
>
> When `prompt` is absent, the node's brief is the bead-derived body, exactly as prior behavior.
>
> `prompt` is **input-only**: it affects the task brief delivered to the agent and does NOT alter the Outcome contract ([execution-model.md §4.1 EM-005]), the routing cascade ([execution-model.md §4.10 EM-041]), or any handler-emitted field.
>
> `prompt` composes with a graph-level `goal` (§4 WG-044): `goal` is the run-level objective threaded via the run-level ExtraContext channel, while `prompt` is the node-level task body; they occupy distinct channels and do NOT double-inject (see [execution-model.md §7.5] launch-surface and the B↔E composition note).
>
> **Reviewer-class scope (v1).** A `prompt` on a **reviewer-class** `agentic` node (resolved by the node's reviewer-class binding, e.g. `agent_type="reviewer"` / `handler_ref="claude-reviewer"`) is accepted-but-inert at v1: the reviewer's brief is sourced from the review-target artifact per [execution-model.md §4.3 EM-015d (sub-clause EM-015d-RIA)] and is NOT overridden by `prompt`. The loader retains the `prompt` attribute in the AST and emits no error; the value is ignored for reviewer-class dispatch. Reviewer-class `prompt` override is reserved for a future schema version.
>
> A `prompt` on a `non-agentic` or `gate` node is a validation warning at v1 per §10 WG-031 (those node types dispatch no agent that reads a brief); the value is retained in the AST and ignored.
>
> Tags: mechanism, normative

## I.4 §4 WG-041 — Non-committing attribute on `agentic` nodes (NEW; component C)

Insert in §4 after WG-040.

> ### WG-041 — Non-committing attribute on `agentic` nodes
>
> An `agentic` node MAY carry a `non_committing` (boolean) optional attribute. When `non_committing="true"` on an **implementer-class** `agentic` node, the node returns `SUCCESS` on a clean agent exit WITHOUT requiring the worktree HEAD to advance past its pre-launch value; the engine does NOT treat a no-commit clean exit as a failure for that node. When `non_committing` is absent or `"false"` (the default), an implementer-class node that exits cleanly without advancing HEAD is a node failure, as in prior behavior.
>
> A `non_committing` clean exit yields `Outcome{status = SUCCESS}` at v1; the engine does NOT inspect a work product, an embedded `{"status":...}` marker, or any other artifact to derive a non-`SUCCESS` outcome from a `non_committing` node. SUCCESS-without-commit is already a legal Outcome per [execution-model.md §4.1 EM-005]; `non_committing` relaxes an engine-side HEAD-advance check, it does not introduce a new Outcome shape.
>
> **Authoring rule (normative).** A `non_committing` node produces no committed work product the engine validates; the engine cannot distinguish a good no-commit exit from a bad one. A workflow author MUST pair every `non_committing` node with a downstream validating `non-agentic` tool node (per §4 WG-039) that inspects the node's work product and exit-codes the routing decision. The engine does not enforce the pairing; it is an authoring obligation documented in the canonical example sidecar.
>
> **`auto_status` is reserved.** A `non_committing` node controls exactly one axis: commit-or-not. It does NOT derive status from a work product or an embedded marker. The attribute name `auto_status` is NOT accepted as a node attribute at v1 (it would mislead authors into expecting status-derivation that does not exist); `auto_status` is reserved for a future status-derivation feature. Pipelines ported from external `auto_status=true` semantics MUST use `non_committing="true"`.
>
> A `non_committing` attribute on a **reviewer-class** `agentic` node, a `non-agentic` node, or a `gate` node is a validation warning at v1 per §10 WG-031 (those dispatch paths do not reach the implementer HEAD-advance check); the value is retained in the AST and ignored.
>
> Tags: mechanism, normative

## I.5 §4 WG-042 — Per-node `model` / `effort` attributes on `agentic` nodes (NEW; component D)

Insert in §4 after WG-041.

> ### WG-042 — Per-node `model` / `effort` attributes on `agentic` nodes
>
> An `agentic` node MAY carry an optional `model` attribute and/or an optional `effort` attribute. When present, the attribute overrides the run-level `(model, effort)` pair sealed at claim time (per [execution-model.md §4.3 EM-012b]) **for that node's dispatch only** (the override mechanism is normative in [execution-model.md §4.3 EM-012b], sub-clause EM-012b-NODE).
>
> - **`model`** (string, optional). An opaque model alias. A loader MUST validate it for **shape only** — non-empty when present, matching `^[A-Za-z0-9._:/-]+$`, at most 128 characters — per the value-opacity invariant of [execution-model.md §4.3 EM-012b] and [execution-model.md §6.1] `ModelPreference`. harmonik MUST NOT verify that the value names a real model; handler-side launch failure is the authoritative compatibility check.
> - **`effort`** (string, optional). A loader MUST require the value to be a member of the closed enum `{low, medium, high, xhigh, max}` per [execution-model.md §6.1] `EffortLevel`. An out-of-enum `effort` value on a node attribute is an **ingest-time strict error**: the graph is static, so the loader MUST reject it at load and the run MUST NOT start. (This is stricter than tier-1's runtime bead-label path in [execution-model.md §4.3 EM-012b], which treats an unrecognised label as absent and emits `bead_label_conflict`; that runtime relaxation applies only to bead labels, not to static node attributes.)
> - `model` and `effort` are independent: a node MAY carry one without the other. A node carrying only `model` inherits the run-level `effort`, and vice versa.
> - Both attributes are valid ONLY on `agentic` nodes. A `model` or `effort` attribute on a `non-agentic`, `gate`, or `sub-workflow` node, on an edge, or at the graph level, is a reserved-attribute-out-of-position strict error per §10 WG-031.
>
> Tags: mechanism, normative

## I.6 §4 WG-043 — `class` / `model_stylesheet` authoring convention (NEW, INFORMATIVE; component D)

Insert in §4 after WG-042.

> ### WG-043 — `class` / `model_stylesheet` (informative)
>
> Some upstream pipelines select per-node models via a graph-level CSS `model_stylesheet` (a `*`-default selector plus a `.hard`-class override) together with a per-node `class` attribute. harmonik does NOT interpret `class` or `model_stylesheet` at v1.0: a loader accepts them permissively per §10 WG-031/WG-032 (warned, retained in `UnknownAttrs`) and the dispatcher MUST NOT route on them. Neither name is added to the §10 WG-031 reserved set.
>
> To port such a pipeline to harmonik, translate each `.hard { llm_model: <model> }` rule plus `class="hard"` into a direct `model="<alias>"` attribute on each classed node (per §4 WG-042 and [execution-model.md §4.3 EM-012b] tier 0), and drop `llm_provider` (handler binding is fixed per [handler-contract.md §4.1 HC-003]). Promoting `model_stylesheet` to a normative selector mechanism (e.g. for more than two model tiers, or selector indirection) is a clean future amendment; the direct `model`/`effort` attributes remain the floor.
>
> Tags: informative

## I.7 §4 WG-044 — Graph-level `goal` attribute (NEW; component E)

Insert in §4 after WG-043.

> ### WG-044 — Graph-level `goal` attribute
>
> A `workflow_mode = dot` graph MAY carry a graph-level `goal` DOT attribute: a free-form string stating the run's objective. A loader MUST parse `goal` into the typed `Graph.Goal` field ([execution-model.md §6.1] Workflow RECORD); it is a reserved graph-level attribute name per §10 WG-031 (a `goal` attribute on a node or edge is a reserved-attribute-out-of-position strict error). When `goal` is present, the daemon MUST surface it to `agentic`-node briefs (per [claude-hook-bridge.md §4 CHB-028]) as the run-level objective, threaded through the run-level ExtraContext channel ([execution-model.md §7.5]); it composes with — and does NOT replace — any per-node `prompt` attribute (§4 WG-040) and the bead-derived body. `goal` MAY contain template-param placeholders (§4 WG-045), which are substituted at launch before parse (§4 WG-045). A graph with no `goal` leaves the brief bead/prompt-driven, unchanged from prior behavior.
>
> `goal` joins `workflow_class` and `context_keys` as a typed, dispatcher-surfaced graph-level attribute.
>
> Tags: mechanism, normative

## I.8 §4 WG-045 — Template-param substitution over `.dot` source text (NEW; component E)

Insert in §4 after WG-044.

> ### WG-045 — Template-param substitution over `.dot` source text
>
> A `.dot` source MAY contain template-param placeholders matching the grammar `__[A-Z][A-Z0-9_]*__` (double-underscore-delimited, an uppercase leading letter, then uppercase letters, digits, and underscores). At run launch, the daemon MUST apply a **single substitution pass over the raw `.dot` source text — before parsing the graph (before the §9 / [execution-model.md §7.5] parse step)** — replacing each placeholder with the corresponding value from the run's launch-time **param map**. The param-map key for a placeholder is the placeholder name with the delimiting double-underscores removed (the map key `ISSUE_NUMBER` substitutes the token `__ISSUE_NUMBER__`).
>
> Substitution scope is the **entire source text**, not the parsed AST: placeholders MAY appear inside any attribute value — `goal` (§4 WG-044), node `tool_command` (§4 WG-039), node `prompt` (§4 WG-040), or any other attribute — and a single source-text pass substitutes all of them uniformly. A loader MUST NOT substitute by walking the parsed AST (an AST walk would miss tokens in attributes the parser retains in `UnknownAttrs`).
>
> Substitution MUST occur exactly once, at launch, before parse and validation, and MUST NOT be re-applied during the run (parallels the resolve-once discipline of [execution-model.md §4.3 EM-012b]). The param map is sealed into the Run record ([execution-model.md §6.1]) so a replay re-substitutes identically.
>
> After the substitution pass, **any residual token matching the grammar `__[A-Z][A-Z0-9_]*__` is a launch-time error**: the daemon MUST refuse to start the run, MUST NOT dispatch a literal `__TOKEN__` into any node attribute or shell command, and MUST report the offending token(s). A `.dot` source containing no placeholders is unaffected (the pass is a no-op).
>
> **Trust boundary (normative).** Param values are operator-supplied and TRUSTED. Substitution occurs over raw source text before parse, so a param value MAY contain DOT syntax, shell metacharacters, or further `__…__` tokens; the daemon does NOT sanitize, escape, or quote param values, and a param value that injects malformed DOT surfaces as a normal parse/validation error against the substituted text. Operators MUST treat `--param` values with the same trust they treat the `.dot` artifact itself. (A param value that itself contains a `__UPPER_SNAKE__` token is substituted only by the single pass — the pass is not recursive — so such a token survives into the substituted text and trips the residual-token launch error of the preceding paragraph.)
>
> Tags: mechanism, normative

## I.9 §4 WG-046 — Substitution ordering invariant (NEW; component E)

Insert in §4 after WG-045.

> ### WG-046 — Substitution ordering invariant
>
> The load-to-dispatch ordering for a `workflow_mode = dot` run is: **read source → substitute params (§4 WG-045) → parse → validate (§9) → dispatch.** Because substitution precedes parse, every downstream consumer — node `tool_command` (§4 WG-039), node `prompt` (§4 WG-040), `goal` (§4 WG-044), and any validation rule of §9 — operates on the concrete (substituted) graph. A loader MUST NOT reorder these steps; in particular it MUST NOT validate or dispatch a graph carrying unsubstituted placeholders, and MUST NOT substitute after parse.
>
> Tags: mechanism, normative

## I.10 §9 WG-024 — Reserved-attribute strictness (amended; component A)

Append a bullet to the WG-024 checked-at-parse-time list:

> - On a `non-agentic` node carrying `tool_command`, `handler_ref` MUST equal `shell`. A `tool_command` on a node whose `handler_ref` resolves to any other handler is a warning at v1 per §10 WG-031 (not a strict error); the constraint is reserved to become strict at the next schema major bump. `timeout`, when present, MUST be a non-negative integer; a non-integer or negative `timeout` is a strict error.

## I.11 §10 WG-031 — Reserved-attribute set (merged; components A/B/C/D/E)

Replace the reserved-set sentence in the WG-031 "Strict positions" bullet with the merged sentence:

> The reserved set at v1.0 is: `type`, `agent_type`, `handler_ref`, `gate_ref`, `sub_workflow_ref`, `workflow_version`, `input_mapping`, `idempotency_class`, `axis_tags`, `tool_command`, `timeout`, `prompt`, `non_committing`, `model`, `effort`, `policy_ref` (reserved-and-rejected name; see [control-points.md §4.12 CP-056]), `hook_ref`, `guard_ref`, `budget_ref`, `skills_ref`, `freedom_profile_ref`, `schema_version`, `version`, `condition`, `preferred_label`, `weight`, `ordering_key`, `start_node`, `terminal_node_ids`, `context_keys` (graph-level per [handler-contract.md §5.6 HC-062]; see WG-031a), `goal` (graph-level per §4 WG-044).

Position rules (carried by the existing WG-031 strict-position consequence): `tool_command` / `timeout` are node-level (`non-agentic` only); `prompt` / `non_committing` / `model` / `effort` are node-level (`agentic` only); `goal` is graph-level. A name used outside its declared position is the WG-031 strict error and the run MUST NOT start. `class` and `model_stylesheet` are NOT in the reserved set (permissive/informative per WG-043). The WG-045 template-param surface is a load-time text transform, not an attribute, and adds no reserved name.

## I.12 §16.1 Vocabulary-diff table (informative; components A/B/C/D/E)

Append rows to the §16.1 table:

- `§4 WG-039 tool-command attrs | New normative content per component A (attractor-parity); tool node = non-agentic + tool_command + handler_ref="shell"; built-in shell handler HC-063; trust-boundary normative.`
- `§4 WG-040 inline prompt | New normative content per component B (attractor-parity); per-node prompt REPLACES bead body for implementer-class nodes; reviewer-class inert at v1; input-only.`
- `§4 WG-041 non_committing | New normative content per component C (attractor-parity); implementer-class clean exit ⇒ SUCCESS without HEAD-advance; auto_status reserved/not-accepted; pair-with-validating-tool-node authoring rule.`
- `§4 WG-042 per-node model/effort | New normative content per component D (attractor-parity); OQ-2 resolved (direct attrs normative, model_stylesheet/class informative per WG-043).`
- `§4 WG-044 graph-level goal attr | New normative content per component E (attractor-parity); goal was previously an unknown permissive graph attr, now a typed reserved field threaded via ExtraContext.`
- `§4 WG-045/WG-046 template-param substitution | New normative content per component E (attractor-parity); pre-parse source-text substitution + residual-token launch error + ordering invariant.`

OQ-2 (stylesheet normative vs informative) is marked RESOLVED: per-node `model`/`effort` are normative (WG-042); `class` + `model_stylesheet` are informative at v1.0 (WG-043). OQ-1 (substitution point) is marked RESOLVED: LAUNCH, over raw source, before parse (WG-045/WG-046).

---

# Part II — `specs/execution-model.md` amendments

## II.1 §4.3 EM-012b — Model/effort resolution precedence (amended; component D)

Reword the EM-012b "resolve once" paragraph (the one beginning "`model` and `effort` are resolved independently … MUST NOT be re-evaluated for the lifetime of the run") to frame the run-level pair as the per-node *default*:

> `model` and `effort` are resolved independently: each walks the tier list separately, and the first non-empty value wins for that field. The tier walk MUST run exactly once per run at claim time. The resolved `(model, effort)` pair MUST be sealed into the Run record as a `ModelPreference` descriptor (per §6.1) before any node in the run is dispatched, and the **tier walk** MUST NOT be re-evaluated for the lifetime of the run. The sealed pair is the **run-level default**: every node in the run uses it unless the node carries its own per-node override (EM-012b-NODE). The resolved pair MUST be surfaced on the `run_started` event payload per [event-model.md §8.1] for downstream consumers.

Add a new sub-clause immediately after EM-012b's value-opacity paragraph:

> **EM-012b-NODE — Per-node override (tier 0).** Under `workflow_mode = dot`, an `agentic` node MAY carry a `model` and/or `effort` attribute per [workflow-graph.md §4 WG-042]. When present, the node's attribute value takes precedence over the run-level `ModelPreference` default (the tiers-1..4 result sealed per EM-012b) **for that node's dispatch only**. The per-node value is **static graph data** read from the already-loaded, already-validated workflow graph at the moment that node is dispatched; it is NOT a second resolution walk and MUST NOT re-evaluate bead labels (tier 1), project config (tier 2), compiled defaults (tier 3), or the fallback (tier 4). The run-level `ModelPreference` sealed into the Run record (§6.1) at claim time is unchanged by per-node overrides; the override is applied when the per-node launch specification is built, by substituting the node's value into that single node's launch `(model, effort)`. `model` and `effort` override independently: a node carrying only `model` inherits the run-level `effort`, and a node carrying only `effort` inherits the run-level `model`. Because the resolution inputs (the claim-time run-level seal plus the static graph attributes fixed at load time) are both immutable for the run's lifetime, replay determinism is preserved: a replay re-derives the same per-node `(model, effort)` for every node. The per-node value is opaque below the descriptor layer on the same terms as the run-level pair (shape-validated `model`, closed-enum `effort`; handler-side launch failure is authoritative).
>
> Tags: mechanism
> Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

This rewording is a **clarification, not a relaxation**: the "resolve exactly once" guarantee now binds the tier-1..4 walk (the run-level seal); per-node attributes are not a runtime resolution pass but static data layered at dispatch.

Informative precedence summary, highest first, after this change:

```
tier 0  per-node attr (model="…" / effort="…" on the agentic node)   [NEW, EM-012b-NODE; static graph data, per node]
tier 1  per-task bead label (model:<alias> / effort:<level>)         (EM-012b, run-level)
tier 2  per-project .harmonik/config.yaml                            (EM-012b, run-level)
tier 3  per-agent-type compiled default                              (EM-012b, run-level)
tier 4  built-in fallback (empty)                                    (EM-012b, run-level)
```

## II.2 §4.3 EM-015d — Review-loop mode lifecycle (scoping clause; component C)

Add a bullet to EM-015d's "The cycle MUST observe:" list. This is a **scoping clarification**, not a relaxation: the implementer-MUST-commit obligation is `review-loop`-scoped and `dot` mode is carved out. The v69 review-loop path does NOT regress.

> - **Implementer commit obligation is review-loop-scoped.** Under `workflow_mode = review-loop`, the implementer phase MUST advance the worktree HEAD (produce a commit) before the reviewer is launched; a clean implementer exit that does not advance HEAD is a cycle-internal failure routed per [handler-contract.md §4.6]. This commit obligation is specific to `review-loop` mode and does NOT apply to `workflow_mode = dot`. A `dot`-mode `agentic` node MAY relax the HEAD-advance requirement via the per-node `non_committing` attribute per [workflow-graph.md §4 WG-041]; that relaxation is gated on the per-node attribute and never reaches the `review-loop` path.

## II.3 §6.1 Node RECORD (amended; component D)

Add two optional fields to the `Node` RECORD in §6.1:

> ```
> RECORD Node:
>     …                                          -- (existing fields unchanged)
>     model               : String | None       -- optional per-node model override; shape-validated per EM-012b-NODE; valid only when type = agentic; overrides the run-level ModelPreference.model for this node's dispatch; see [workflow-graph.md §4 WG-042]
>     effort              : EffortLevel | None   -- optional per-node effort override; closed enum (§6.1 EffortLevel); valid only when type = agentic; overrides the run-level ModelPreference.effort for this node's dispatch; see [workflow-graph.md §4 WG-042]
>     tool_command        : String | None        -- optional shell command for a non-agentic tool node; valid only when type = non-agentic; dispatched by the built-in shell handler [handler-contract.md §4.1 HC-063]; see [workflow-graph.md §4 WG-039]
>     timeout_command     : Integer | None       -- optional wall-clock kill bound (seconds) for tool_command; default 300; meaningful only with tool_command; see [workflow-graph.md §4 WG-039]
>     prompt              : String | None         -- optional inline brief; replaces bead-derived body for implementer-class agentic nodes; reviewer-class inert at v1; see [workflow-graph.md §4 WG-040]
>     non_committing      : Boolean               -- default false; when true on an implementer-class agentic node, a clean exit yields SUCCESS without requiring HEAD-advance; see [workflow-graph.md §4 WG-041]
> ```

(Implementation note for the tasks pass: the live `Node` RECORD already carries a `timeout : Integer | None` field semantically reserved as "positive seconds"; the tool-node `timeout` per WG-039 reuses that field rather than introducing `timeout_command`. The tasks pass MUST reconcile the field name — reuse the existing `Node.timeout` slot — so this draft's `timeout_command` is illustrative only. No new ENUM or RECORD is introduced; `ModelPreference` and `EffortLevel` are reused.)

## II.4 §6.1 Workflow RECORD (amended; component E)

Add an optional `goal` field to the `Workflow` RECORD in §6.1:

> ```
> RECORD Workflow:
>     …                                       -- (existing fields unchanged)
>     goal               : String | None      -- optional graph-level objective per [workflow-graph.md §4 WG-044]; threaded into agentic-node briefs via the run-level ExtraContext channel (§7.5); MAY contain template-param placeholders substituted at launch (§7.5, [workflow-graph.md §4 WG-045])
> ```

## II.5 §6.1 Run RECORD (amended; component E)

Add an optional `template_params` field to the `Run` RECORD in §6.1, sealed at claim time alongside `model_preference` and `workflow_id`:

> ```
> RECORD Run:
>     …                                          -- (existing fields unchanged)
>     template_params    : Map<String, String> | None  -- per-run template-param map supplied at launch (--param KEY=VALUE per §7.5); sealed at claim time, applied exactly once to the raw .dot source before parse per [workflow-graph.md §4 WG-045]; None when no params supplied; sealing makes a replay re-substitute identically
> ```

## II.6 §6.4 Schema evolution (additive notes; components D + E)

Add additive schema-evolution entries recording that:
- `Node.model`, `Node.effort`, `Node.tool_command`, `Node.timeout` (reused slot), `Node.prompt`, `Node.non_committing` are new optional `Node` fields (additive, non-breaking, N-1 readable per §6.4 and [workflow-graph.md §11 WG-034]); a reader at the prior schema treats them as unknown-and-absent.
- `Workflow.goal` and `Run.template_params` are new optional fields (additive, non-breaking, N-1 readable); a `goal=""` graph runs bead-driven and a run with no `template_params` performs a no-op substitution pass.

## II.7 §7.5.3 EM-057 item 7 — Required attributes by node type (amended; component A)

Amend EM-057 item 7's `non-agentic` sub-bullet:

> - `non-agentic` nodes MUST carry `handler_ref` resolving to a handler registered per [handler-contract.md §4.1]. (This obligation is the §4.2.EM-007 amendment per EM-060.) When the node is a tool node (carries `tool_command` per [workflow-graph.md §4 WG-039]), `handler_ref` MUST be `shell`, resolving to the built-in `shell` handler per [handler-contract.md §4.1 HC-063]; the validator MAY emit a warning rather than fail when `tool_command` is present with a non-`shell` `handler_ref` at v1 (reserved to become a validation failure at the next schema major bump).

The mandatory-`handler_ref` invariant of EM-057 item 7 is preserved: a tool node still carries `handler_ref` (pinned to `shell`).

## II.8 §7.5.4 EM-058 — Normative dispatch table for `dot` node types (keystone reconciliation; components A + C)

Replace the EM-058 `non-agentic` table row with the split-on-`tool_command` row. This reconciles the spec with the live in-process behavior at `internal/daemon/dot_cascade.go:198-203` (which synthesizes `Outcome{Status: SUCCESS}` for a `non-agentic` node):

```
| `non-agentic` | When the node carries `tool_command` and `handler_ref="shell"`: the built-in `shell` handler of [handler-contract.md §4.1 HC-063] executes the command in the run's worktree and applies the exit-state → Outcome mapping of HC-063 / [workflow-graph.md §4 WG-039]. The `shell` handler MAY run in-process (no subprocess, no socket) per the HC-063 exception. When the node carries no `tool_command` (start / terminal / pass-through node), the engine synthesizes a `SUCCESS` Outcome without dispatching a handler (the `internal/daemon/dot_cascade.go` in-process path). Otherwise (a non-agentic node bound to a non-`shell` handler), invoke the handler referenced by the node's `handler_ref` per [handler-contract.md §4.1]; handler-internal determinism is the handler's responsibility per the node's four-axis tags (§4.2.EM-011). | §4.1.EM-005 `Outcome` with `kind = default` per §4.1.EM-005a. |
```

The `agentic`, `gate`, and `sub-workflow` rows are unchanged. Append two sub-notes below the EM-058 table (after the existing "load-bearing for implementer epics" paragraph):

> **Non-agentic dispatch sub-note (component A).** The `non-agentic` row admits three dispatch paths distinguished by node content: (1) a tool node (`tool_command` + `handler_ref="shell"`) runs the built-in `shell` handler per [handler-contract.md §4.1 HC-063], which MAY execute in-process; (2) a start / terminal / pass-through node (no `tool_command`) is synthesized to `SUCCESS` without a handler dispatch; (3) any other `non-agentic` node invokes its bound `handler_ref` via the handler registry exactly as `agentic` does. The "invoke the handler referenced by `handler_ref`" dispatch action of the prior EM-058 row is preserved for path (3); paths (1) and (2) are the spec-layer reconciliation of the in-process behavior at `internal/daemon/dot_cascade.go`.
>
> **Non-committing `agentic` dispatch sub-note (component C).** For a `dot`-mode implementer-class `agentic` node, the engine derives the node Outcome after a clean agent exit as follows: if the node carries `non_committing="true"` per [workflow-graph.md §4 WG-041], a clean exit yields `Outcome{status = SUCCESS}` regardless of whether the worktree HEAD advanced; otherwise a clean exit that did NOT advance HEAD is a node failure. A worktree whose HEAD cannot be resolved at all is a daemon-side error in BOTH modes (a broken worktree is a real failure). This derivation is `dot`-mode-only; `review-loop` mode's implementer commit obligation per §4.3 EM-015d is unchanged.

## II.9 §7.5 `dot` dispatcher — launch surface (amended; component E)

Amend the §7.5 `dot`-mode launch contract to:

1. Accept a per-run param map at launch. The `harmonik run` CLI (and the queue-item launch context) accepts repeated `--param KEY=VALUE` flags; the resulting `map[string]string` is the run's `template_params` (§6.1), sealed at claim time alongside `workflow_ref` and threaded through the bead-run launch path (the same per-run channel as ExtraContext).
2. Before parsing the `.dot` artifact, apply the WG-045 substitution pass to the raw source text using the sealed `template_params`, then run the WG-045 residual-token check, then parse and validate per §9 / §7.5.3 (the WG-046 ordering invariant).
3. Surface the (substituted) `Graph.Goal`, when present, into every `agentic` node's brief via the run-level **ExtraContext channel**, composing with the bead-derived body and any per-node `prompt` (the B↔E composition contract; see §II.10).

## II.10 §7.5 — B↔E brief-composition contract (normative; components B + E)

Both component E (graph-level `goal`, run-level) and component B (node-level `prompt`) write the `agentic`-node brief through the [claude-hook-bridge.md CHB-028] `agent-task.md` path. They MUST **compose, not double-inject**:

- **`goal` (E, run-level)** is threaded via the **run-level ExtraContext channel** (`AgentTaskPayload.ExtraContext`, rendered as the `## Extra Context` section) as the run's stated objective. It is constant across every node in the run.
- **`prompt` (B, node-level)** replaces the node-level task body (`AgentTaskPayload.Body`, rendered as the `## Task Description` section) for the node that carries it.
- **bead-derived body** remains the node brief (the `## Task Description` Body) when no per-node `prompt` is present.

`goal` and `prompt` occupy **distinct channels** (ExtraContext vs taskBody) and therefore do not collide; a node carrying both `goal` (run-level) and `prompt` (node-level) receives each exactly once. **Assembly order:** the `agent-task.md` renders `## Task Description` (the node body — `prompt` if present, else bead body) BEFORE `## Extra Context` (the run-level `goal`) — confirmed against `internal/workspace/agenttask_chb028.go` (`buildAgentTaskContent`: `Body` precedes `ExtraContext`).

---

# Part III — `specs/handler-contract.md` amendments

## III.1 §4.1 HC-063 — Built-in `shell` handler for tool nodes (NEW; component A)

Insert a new normative requirement at the end of §4.1 (after HC-004), or as a clearly-anchored §4.1a. (HC-063 is the next free HC ID; HC-062 is the highest assigned.)

> #### HC-063 — Built-in `shell` handler for tool nodes
>
> The `shell` handler is a built-in deterministic handler bound by the reserved `handler_ref="shell"` per [workflow-graph.md §4 WG-039]. It dispatches a `non-agentic` tool node by executing the node's `tool_command` and mapping the command's exit state to an `Outcome` per [execution-model.md §4.1 EM-005].
>
> **Invocation.** The `shell` handler executes `/bin/sh -c <tool_command>` with the working directory set to the run's `workspace_path` (the worktree per [workspace-model.md §4.1]) and the daemon's handler environment. The command is killed if it exceeds the node's `timeout` (default `300` seconds per [workflow-graph.md §4 WG-039]).
>
> **In-process exception (normative).** The `shell` handler is a built-in deterministic handler and MAY execute IN-PROCESS within the daemon: it has no agent subprocess, no NDJSON progress stream (§4.2 HC-007 / HC-007a), no `agent_ready` signal (§4.9 HC-039), no heartbeat (§4.6 HC-026a), and no silent-hang detection (§4.6 HC-026). The §4.2 wire-protocol obligations (HC-005 through HC-010), the §4.9 ready-state obligations, and the §4.6 silent-hang obligations DO NOT apply to the `shell` handler. The `timeout` kill-bound replaces silent-hang detection as the liveness guard. This is the sole built-in-handler exception at v1; all agent-dispatching handlers remain subprocess-and-socket-bound per §4.2 HC-007.
>
> **Outcome.** The `shell` handler emits an `Outcome` with `kind = default` per [execution-model.md §4.1 EM-005a]; no `payload`. The handler does NOT emit a `failure_class` hint; classification is daemon-side per §4.5 HC-020 and the exit-state mapping below (the daemon back-fills `failure_class` on FAIL per HC-059 / [execution-model.md §4.1 EM-005c]). The exit-state → Outcome mapping is:
>
> | Exit state | `status` | `failure_class` |
> |---|---|---|
> | exit 0 | `SUCCESS` | (absent) |
> | exit non-zero (1..255) | `FAIL` | `deterministic` |
> | timeout-kill (exceeded `timeout`) | `FAIL` | `transient` |
> | signal-kill (context cancel / operator stop / SIGKILL) | `FAIL` | `canceled` |
>
> A non-zero exit is a `FAIL` `Outcome` the cascade routes on per [execution-model.md §4.10 EM-041]; it is NOT a daemon-side error that reopens the run. Author-declared per-command transient exit codes are reserved for a future schema version and are NOT supported at v1 (every non-zero exit is `deterministic`). The `shell` handler never emits `structural`, `budget_exhausted`, `compilation_loop`, or `partial_success`.
>
> **Boundary-classification tags.** The `shell` handler's default four-axis tags per [execution-model.md §4.2 EM-011] are `io-determinism = non-deterministic` (shell commands have side effects) and `replay-safety = unsafe` (re-running a side-effecting command may double-apply). A tool node's author MAY declare tighter `axis_tags` per [workflow-graph.md §4 WG-039] when the specific command is known-idempotent.
>
> **Trust boundary.** Per [workflow-graph.md §4 WG-039], `tool_command` is a literal shell string from the trusted `.dot` author; the `shell` handler performs no sandboxing or escaping at v1.
>
> **Observability.** A tool node reuses the existing node-lifecycle observability surface: `node_dispatch_requested` ([event-model.md §8.1.11]) fires before dispatch, and the command's result flows through the standard `Outcome` surface and run-terminal events. No `tool_command_completed` event is introduced at v1 (per the integration-pass observability decision; per-command lifecycle events are deliberately excluded by [event-model.md §8]'s lifecycle-boundary discipline).
>
> Tags: mechanism, normative

## III.2 §4.2 HC-005 — In-process reconciliation note (amended; component A)

Append a clarifying clause to §4.2 (the wire-protocol section preamble or HC-005):

> The wire-protocol obligations of this section (HC-005 through HC-010) apply to handlers that launch an agent subprocess. Built-in deterministic handlers MAY execute in-process and are exempt as declared by their own requirement; the only such handler at v1 is the `shell` handler of §4.1 HC-063, which has no subprocess, no socket, and no progress stream. No other handler is exempt from §4.2 at v1.

## III.3 §4.2 HC-006a — `agent-task.md` content row (amended; component B)

Append a clause to the HC-006a `agent-task.md` path-and-content row's `implementer-initial` cell (or add a note below the table):

> The `agent-task.md` Body for a `dot`-mode implementer-class `agentic` node MAY be sourced from the node's `prompt` attribute per [workflow-graph.md §4 WG-040], overriding the bead-derived body; the bead `Title` and bead ID remain in the header. This override is `dot`-mode-only and does not apply to `review-loop` phases. No LaunchSpec required field (HC-006), no wire-protocol obligation, and no Outcome obligation changes — `prompt` affects the rendered task-artifact Body only. A graph-level `goal` ([workflow-graph.md §4 WG-044]), when present, is rendered into the SAME `agent-task.md` artifact's `## Extra Context` section via the run-level ExtraContext channel; `goal` and `prompt` occupy distinct sections and do not double-inject (see [execution-model.md §7.5]).

The HC-006a table's `working_dir`, `argv`, `env`, `claude_session_id`, and `phase`/`iteration_count` rows are unchanged.

## III.4 §4.2a HC-058 — `non-agentic` (tool-style) row (amended; component A)

Append a note clause to the §4.2a HC-058 `non-agentic` (tool-style) row's `failure_class` cell:

> For a tool node dispatched by the built-in `shell` handler of §4.1 HC-063, `failure_class` is daemon-classified from the command's exit state per HC-063 (`deterministic` on non-zero exit, `transient` on timeout, `canceled` on signal); the `shell` handler emits no hint. The `shell` handler emits only `SUCCESS` or `FAIL` (never `RETRY` or `PARTIAL_SUCCESS`) at v1.

## III.5 §4.2a (near HC-058) — SUCCESS-without-commit clarifying note (amended; component C)

Add a clarifying note in §4.2a near HC-058 / the agentic row. No new HC requirement is needed — the existing agentic-node Outcome obligations already permit `SUCCESS` without a commit:

> A `dot`-mode `agentic` node MAY emit `status = SUCCESS` without the run having produced a commit when the node is `non_committing` per [workflow-graph.md §4 WG-041]. `SUCCESS`-without-commit is already a legal Outcome per [execution-model.md §4.1 EM-005]; it is not a new Outcome shape and imposes no new handler obligation. The HEAD-advance expectation that gates a non-`non_committing` implementer node is an engine-side derivation per [execution-model.md §7.5], not a handler-contract obligation.

---

# Part IV — Observability decision (no `event-model.md` amendment)

A tool node (HC-063 `shell` handler) **reuses the existing node-lifecycle events**; no `tool_command_completed` event is added at v1. The live `dot` cascade already emits `node_dispatch_requested` ([event-model.md §8.1.11], class O) before every node including non-agentic ones, and the command's Outcome flows through the standard Outcome surface + run-terminal events. event-model.md §8 explicitly excludes per-tool-call lifecycle events ("tool calls … MUST NOT be emitted as events; it lives in the agent's session log"); a bespoke `tool_command_completed` event would violate that discipline and the §8.9 orphan-lint posture. The in-process `shell` handler has no NDJSON stream, so there is no chunk-class observability to add or replace. Decision and rationale recorded in `06-integration.md` §6.

---

# Part V — Backwards-compatibility summary

All amendments are additive and N-1-readable; `schema_version` stays `1`. A graph carrying none of the new attributes behaves exactly as before: every `agentic` node inherits the run-level `ModelPreference` and the bead-derived brief; every `non-agentic` node without `tool_command` is a plain handler/noop node; a graph with no `goal` and no `__PARAM__` tokens is byte-for-byte unchanged (the substitution pass over a token-free source is a no-op). No node type, edge field, or enum member is added; the Outcome envelope (EM-005/005a/005c) and edge cascade (EM-041) are unchanged; the v69 `review-loop` implementer-MUST-commit invariant is preserved (review-loop-scoped; `dot` mode carved out). The one wording touch to a load-bearing invariant (EM-012b "resolve once") is a clarification that binds the tier-1..4 walk and frames the sealed pair as the run-level default.
