# Spec Draft — Graph goal + template-param substitution (component E)

> Pass 5 (`spec-draft`) of `attractor-parity`, component **E**. Normative requirement text for a graph-level `goal` attribute and `__UPPER_SNAKE__` template-param substitution over `.dot` source text. Source design: `04-design/goal-template-params-design.md`. Resolves OQ-1 (substitution point: LOAD vs LAUNCH → LAUNCH, over raw source, before parse).
>
> **Target spec files:** `specs/workflow-graph.md` (two new graph-level requirements, §10 WG-031 reserved set, §16.1 vocabulary diff) and `specs/execution-model.md` (§6.1 Workflow RECORD, §6.1 Run RECORD, §6.4 schema evolution, §7.5 `dot` dispatcher launch surface). This draft states the requirement text to be merged into those files at integration (pass 6); it is NOT a full-file replacement, per the component-scoped split agreed for this work.

---

## A. `specs/workflow-graph.md` amendments

### A.1 WG-039 (new requirement) — Graph-level `goal` attribute

> #### WG-039 — Graph-level `goal` attribute
>
> A `workflow_mode = dot` graph MAY carry a graph-level `goal` DOT attribute: a free-form string stating the run's objective. A loader MUST parse `goal` into the typed `Graph.Goal` field ([execution-model.md §6.1] Workflow RECORD); it is a reserved graph-level attribute name per §10 WG-031 (a `goal` attribute on a node or edge is a reserved-attribute-out-of-position strict error). When `goal` is present, the daemon MUST surface it to `agentic`-node briefs (per [handler-contract.md §4 / CHB-028]) as the run-level objective, threaded through the run-level ExtraContext channel ([execution-model.md §7.5]); it composes with — and does NOT replace — any per-node `prompt` attribute ([workflow-graph.md §4 WG-002, component B]) and the bead-derived body. `goal` MAY contain template-param placeholders (WG-040), which are substituted at launch before parse (WG-040). A graph with no `goal` leaves the brief bead/prompt-driven, unchanged from prior behavior.
>
> `goal` joins `workflow_class` and `context_keys` as a typed, dispatcher-surfaced graph-level attribute.
>
> Tags: mechanism, normative

### A.2 WG-040 (new requirement) — Template-param substitution

> #### WG-040 — Template-param substitution over `.dot` source text
>
> A `.dot` source MAY contain template-param placeholders matching the grammar `__[A-Z][A-Z0-9_]*__` (double-underscore-delimited, an uppercase leading letter, then uppercase letters, digits, and underscores). At run launch, the daemon MUST apply a **single substitution pass over the raw `.dot` source text — before parsing the graph (before the §9 / [execution-model.md §7.5] parse step)** — replacing each placeholder with the corresponding value from the run's launch-time **param map**. The param-map key for a placeholder is the placeholder name with the delimiting double-underscores removed (the map key `ISSUE_NUMBER` substitutes the token `__ISSUE_NUMBER__`).
>
> Substitution scope is the **entire source text**, not the parsed AST: placeholders MAY appear inside any attribute value — `goal` (WG-039), node `tool_command` (component A), node `prompt` (component B), or any other attribute — and a single source-text pass substitutes all of them uniformly. A loader MUST NOT substitute by walking the parsed AST (an AST walk would miss tokens in attributes the parser retains in `UnknownAttrs`).
>
> Substitution MUST occur exactly once, at launch, before parse and validation, and MUST NOT be re-applied during the run (parallels the resolve-once discipline of [execution-model.md §4.3 EM-012b]). The param map is sealed into the Run record ([execution-model.md §6.1]) so a replay re-substitutes identically.
>
> After the substitution pass, **any residual token matching the grammar `__[A-Z][A-Z0-9_]*__` is a launch-time error**: the daemon MUST refuse to start the run, MUST NOT dispatch a literal `__TOKEN__` into any node attribute or shell command, and MUST report the offending token(s). A `.dot` source containing no placeholders is unaffected (the pass is a no-op).
>
> **Trust boundary (normative).** Param values are operator-supplied and TRUSTED. Substitution occurs over raw source text before parse, so a param value MAY contain DOT syntax, shell metacharacters, or further `__…__` tokens; the daemon does NOT sanitize, escape, or quote param values, and a param value that injects malformed DOT surfaces as a normal parse/validation error against the substituted text. Operators MUST treat `--param` values with the same trust they treat the `.dot` artifact itself. (A param value that itself contains a `__UPPER_SNAKE__` token is substituted only by the single pass — the pass is not recursive — so such a token survives into the substituted text and trips the residual-token launch error of the preceding paragraph.)
>
> Tags: mechanism, normative

### A.3 WG-041 (new requirement) — Substitution ordering invariant

> #### WG-041 — Substitution ordering invariant
>
> The load-to-dispatch ordering for a `workflow_mode = dot` run is: **read source → substitute params (WG-040) → parse → validate (§9) → dispatch.** Because substitution precedes parse, every downstream consumer — node `tool_command` (component A), node `prompt` (component B), `goal` (WG-039), and any validation rule of §9 — operates on the concrete (substituted) graph. A loader MUST NOT reorder these steps; in particular it MUST NOT validate or dispatch a graph carrying unsubstituted placeholders, and MUST NOT substitute after parse.
>
> Tags: mechanism, normative

### A.4 WG-031 — Reserved-attribute set (amended)

The reserved set at v1.0 in §10 WG-031 gains `goal`. The amended reserved-set sentence reads:

> The reserved set at v1.0 is: `type`, `agent_type`, `handler_ref`, `gate_ref`, `sub_workflow_ref`, `workflow_version`, `input_mapping`, `idempotency_class`, `axis_tags`, `policy_ref` (reserved-and-rejected name; see [control-points.md §4.12 CP-056]), `hook_ref`, `guard_ref`, `budget_ref`, `skills_ref`, `freedom_profile_ref`, `goal`, `schema_version`, `version`, `condition`, `preferred_label`, `weight`, `ordering_key`, `start_node`, `terminal_node_ids`, `context_keys` (graph-level per [handler-contract.md §5.6 HC-062]; see WG-031a).

`goal` is a graph-level reserved name (dispatcher-consumed): a `goal` attribute on a node or edge is a reserved-attribute-out-of-position strict error. The template-param surface (WG-040) is a load-time text transform, NOT an attribute, so it adds no reserved name; its token grammar is defined normatively in WG-040.

### A.5 §16.1 vocabulary-diff table (informative)

§16.1 gains two rows:

- "§ WG-039 graph-level `goal` attr | New normative content per component E (`attractor-parity`); `goal` was previously accepted as an unknown permissive graph attr, now promoted to a typed reserved field."
- "§ WG-040/WG-041 template-param substitution | New normative content per component E (`attractor-parity`); pre-parse source-text substitution + residual-token launch error + ordering invariant."

---

## B. `specs/execution-model.md` amendments

### B.1 §6.1 Workflow RECORD (amended)

The `Workflow` RECORD in §6.1 gains an optional `goal` field:

> ```
> RECORD Workflow:
>     …                                       -- (existing fields unchanged)
>     goal               : String | None      -- optional graph-level objective per [workflow-graph.md §WG-039]; threaded into agentic-node briefs via the run-level ExtraContext channel (§7.5); MAY contain template-param placeholders substituted at launch (§7.5, [workflow-graph.md §WG-040])
> ```

No existing field is renamed or removed.

### B.2 §6.1 Run RECORD (amended)

The `Run` RECORD in §6.1 gains an optional `template_params` field, sealed at claim time alongside `model_preference` and `workflow_id`:

> ```
> RECORD Run:
>     …                                          -- (existing fields unchanged)
>     template_params    : Map<String, String> | None  -- per-run template-param map supplied at launch (--param KEY=VALUE per §7.5); sealed at claim time, applied exactly once to the raw .dot source before parse per [workflow-graph.md §WG-040]; None when no params supplied; sealing makes a replay re-substitute identically
> ```

No existing field is renamed or removed.

### B.3 §6.4 Schema evolution (additive note)

The §6.4 schema-evolution log gains an additive entry recording that `Workflow.goal` and `Run.template_params` are new optional fields (additive, non-breaking, N-1 readable per §6.4 and [workflow-graph.md §11 WG-034]); a reader at the prior schema treats them as unknown-and-absent (a `goal=""` graph runs bead-driven; a run with no `template_params` performs a no-op substitution pass).

### B.4 §7.5 `dot` dispatcher — launch surface (amended)

§7.5's `dot`-mode launch contract is amended to:

1. Accept a per-run param map at launch. The `harmonik run` CLI (and the queue-item launch context) accepts repeated `--param KEY=VALUE` flags; the resulting `map[string]string` is the run's `template_params` (§6.1), sealed at claim time alongside `workflow_ref` and threaded through the bead-run launch path (the same per-run channel as ExtraContext).
2. Before parsing the `.dot` artifact, apply the WG-040 substitution pass to the raw source text using the sealed `template_params`, then run the WG-040 residual-token check, then parse and validate per §9 / §7.5.3 (the WG-041 ordering invariant).
3. Surface the (substituted) `Graph.Goal`, when present, into every `agentic` node's brief via the run-level ExtraContext channel, composing with the bead-derived body and any per-node `prompt` (B↔E composition — see §C).

### B.5 §6.5 Co-owned events (informative note)

The `run_started` payload's existing fields are unchanged. (Whether `goal` / `template_params` are echoed on `run_started` is an [event-model.md §8] decision deferred to integration; this component does not require a new event field.)

---

## C. B↔E brief-composition contract (for integration pass 6)

Both component E (graph-level `goal`, run-level) and component B (node-level `prompt`) write the `agentic`-node brief through the [claude-hook-bridge.md CHB-028] `agent-task.md` path. They MUST **compose, not double-inject**:

- **`goal` (E, run-level)** is threaded via the **run-level ExtraContext channel** as the run's stated objective. It is constant across every node in the run.
- **`prompt` (B, node-level)** replaces the node-level task body (the bead-derived body) for the node that carries it.
- **bead-derived body** remains the node brief when no per-node `prompt` is present.

The resulting brief is: run-level objective (`goal`, via ExtraContext) + node body (`prompt` if present, else bead-derived body). E's `goal` and B's `prompt` occupy **distinct channels** (ExtraContext vs taskBody) and therefore do not collide; integration (pass 6) MUST confirm the two channels remain distinct in the merged `agent-task.md` assembly so a node carrying both `goal` (run-level) and `prompt` (node-level) receives each exactly once.

---

## D. Out of scope (flagged)

`default_max_retry` (a graph-level kilroy attribute co-located with `goal`) is OUT of scope for component E; it remains a permissive unknown attribute (retained in `UnknownAttrs`) and is covered, if at all, by `traversal_cap` / a separate graph-default attribute. No action here.

---

## E. Backwards-compatibility

- Additive only. `goal` is a graph-level attribute (like `workflow_class`); the param surface is a pre-parse text transform. No node-type, edge-field, or enum change; `schema_version` stays `1`; minor, N-1-readable bump per [workflow-graph.md §11 WG-034] / [execution-model.md §6.4].
- A graph with no `goal` and no `__PARAM__` tokens is byte-for-byte unchanged: the substitution pass over a token-free source is a no-op, and absent `goal` leaves the brief bead-driven.
- A graph that previously carried `goal="…"` as an unknown permissive attr now parses it into the typed `Graph.Goal` field — previously accepted (warned + retained), now interpreted; no breakage.
