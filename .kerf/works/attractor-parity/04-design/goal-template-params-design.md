# Change Design — Graph goal + template-param substitution (E, E1)

> Pass 4 (`change-design`) of `attractor-parity`, component **E**. Normative design for a graph-level `goal` string and `__PLACEHOLDER__` template-param substitution. Grounded in `03-research/goal-template-params/findings.md`. Resolves **OQ-1** (substitution point: LOAD vs LAUNCH).

## 1. Design summary

Two additions: (1) a graph-level `goal` attr — the run's stated objective, promoted from a permissive unknown attr to a typed reserved field, threaded into the agent brief; (2) a **template-param substitution surface** — `__UPPER_SNAKE__` tokens in the `.dot` source substituted from a per-run param map supplied at launch. Substitution happens at **launch (run-start), over the raw source text, BEFORE parse** — making the on-disk `.dot` a reusable template that produces a different concrete graph per run/param-map. Backwards-compatible: a graph with no `goal` and no tokens behaves exactly as today.

## 2. OQ-1 resolution (normative)

**Substitute at LAUNCH (run-start), over the RAW SOURCE TEXT, before `dot.Parse`.** (Full rationale: research F-E3.) Summary:
- **Launch-time** (not a distinct load stage) keeps the on-disk graph reusable — the same template runs with different params per run (the `01-problem-space.md` OQ-1 lean).
- **Over source text, not the AST** — because placeholders pervade node `tool_command` and `prompt` attrs, not just `goal` (research F-E1). A single regexp/string pass over the source before parsing is both single-pass and total; an AST walk would have to visit every string field and would miss tokens in attrs the parser drops into `UnknownAttrs`. This is the decisive, non-obvious choice (flag §7).
- **Before parse/validate** — so validation runs on the concrete (substituted) graph; an unsubstituted residual token surfaces as a normal error, not a silent pass.

## 3. Normative definitions

### 3.1 Graph-level `goal` attr (workflow-graph.md, new requirement e.g. WG-0Y in §10/§11 graph-attr area)

> **`goal` (graph-level, optional).** A `workflow_mode=dot` graph MAY carry a graph-level `goal` DOT attribute: a free-form string stating the run's objective. A loader MUST parse it into the typed `Graph.Goal` field (it is a reserved graph-level name per §10 WG-031 — see §3.3). When present, the daemon MUST surface `goal` to agentic-node briefs (per [handler-contract.md §4 / CHB-028]) as the run-level objective, composing with (not replacing) any per-node `prompt` ([WG-002, component B]) and the bead-derived body. `goal` MAY contain template-param placeholders (§3.2), substituted at launch before parse. A graph with no `goal` leaves the brief bead/prompt-driven (unchanged from today).

`goal` joins `workflow_class` as a typed, informative-to-tooling-but-dispatcher-surfaced graph attr.

### 3.2 Template-param substitution surface (workflow-graph.md, new requirement e.g. WG-0Z)

> **Template-param substitution.** A `.dot` source MAY contain template-param placeholders matching the grammar `__[A-Z][A-Z0-9_]*__` (double-underscore-delimited, uppercase-leading, uppercase + digits + underscore). At run launch, the daemon MUST apply a **single substitution pass over the raw `.dot` source text** (before parsing per [WG-002]) replacing each placeholder with the corresponding value from the run's launch-time **param map**. The param map is a string→string mapping supplied at `harmonik run` launch (see §4) and sealed into the Run record (§6.1) for replay. Substitution MUST occur exactly once, at launch, before parse/validate; it MUST NOT be re-applied during the run (parallels the resolve-once discipline of [execution-model.md §4.3 EM-012b]). After substitution, **any residual placeholder matching the grammar is a launch-time error**: the daemon MUST refuse to start the run and MUST report the offending token(s), rather than dispatch a literal `__TOKEN__` into a node attr or shell command. A `.dot` source containing no placeholders is unaffected (the pass is a no-op). The param map keys are the placeholder names without the delimiting underscores (e.g. map key `ISSUE_NUMBER` substitutes `__ISSUE_NUMBER__`).

Substitution scope: the **entire source text**, so placeholders inside `goal`, `tool_command` (component A), `prompt` (component B), and any other attr are all substituted uniformly.

### 3.3 WG-031 reserved set (workflow-graph.md §10)

Add `goal` to the reserved graph-attr set (dispatcher-consumed → must be strict-position; a `goal` attr on a node or edge is a reserved-out-of-position strict error). The template-param surface is a load-time text transform, NOT an attr, so it needs no reserved name — but the spec defines its token grammar normatively (§3.2). Code: `reservedGraphAttrs` map in `internal/workflow/dot/parser.go:509-518` gains `"goal": true`; `Graph` struct in `internal/workflow/dot/ast.go:34` gains `Goal string`; the graph-attr switch (`parser.go:533-557`) gains a `case "goal":` arm.

### 3.4 Run record + launch flag

- **Run record (execution-model.md §6.1 / workflow-graph layer):** the Run gains an optional `template_params : map[string]string` (or equivalent) sealed at claim time, alongside the existing `workflow_ref`. This is what makes a replay re-substitute identically.
- **Launch surface (CLI / queue item):** `harmonik run` accepts repeated `--param KEY=VALUE` flags (mirroring `--workflow-ref` hk-qo9pq). The param map threads through `beadRunOne(...)` (the same channel as `extraContext`, hk-boiwe, `workloop.go:1123`) into the load step.

## 4. Substitution mechanism + point (code, informative — confirms clean-add)

- New loader entry `LoadDotWorkflowWithParams(dotPath string, params map[string]string)` (sibling of `LoadDotWorkflowWithPolicy`, `internal/workflow/loader.go:110`), or a `params` arg added to `LoadDotWorkflow` (`loader.go:56`). It: (a) reads the source; (b) applies substitution (`strings.ReplaceAll` per `__KEY__`→value, or one `regexp` pass over `__[A-Z][A-Z0-9_]*__`); (c) scans the substituted text for any residual token → launch-time error; (d) calls `dot.Parse(substituted, filename)` as today.
- `internal/daemon/workloop.go:1277` — the `WorkflowModeDot` case calls the params-aware loader, passing the Run's sealed `template_params`.
- `goal`-into-brief: `internal/daemon/dot_cascade.go` agentic dispatch / `claudelaunchspec.go:258-273` — inject the (already-substituted) `graph.Goal` into the `agent-task.md` payload, reusing the existing `ExtraContext`/brief channel (`claudelaunchspec.go:271`). Lean: prepend `goal` as the run-level objective; per-node `prompt` and bead body remain the node brief.

## 5. Substitution ordering invariant (normative)

`read source -> substitute params -> parse -> validate -> dispatch`. Because substitution precedes parse, all downstream consumers (component A's `tool_command`, component B's `prompt`, `goal`) see concrete values. The spec MUST state this ordering so an implementer does not substitute the AST instead (which would miss in-attr tokens — research R-E1).

## 6. Backwards-compatibility

- **Additive only.** `goal` is a graph-level attr (like `workflow_class`); the param surface is a pre-parse text transform. No node-type / edge-field / enum change. Schema stays minor, N-1 readable (WG-034); `schema_version` stays `1`.
- A graph with no `goal` and no `__PARAM__` tokens is byte-for-byte unchanged (no-op pass; bead-driven brief).
- Existing graphs that carried `goal="..."` as an unknown permissive attr now parse it into the typed field — previously accepted (warned + retained), now interpreted; no breakage.
- The review-loop (v69 live workload) carries no `goal` and no tokens → untouched.

## 7. Items NOT clean-additive (flag for integration)

- **Substitution-over-source-text, pre-parse (research R-E1, F-E3/F-E6).** This is a deliberate layering choice (single-pass + total coverage of in-attr placeholders). Pass-6/spec-draft must state it normatively so an implementer doesn't AST-walk and miss `tool_command`/`prompt` tokens. The **unsubstituted-token-is-a-launch-error** rule (§3.2) is the one genuinely NEW normative behavior (not pure cleanup) — fail-fast over shipping a literal token.
- **`goal`-into-brief composition with component B (inline-prompt).** Both E (`goal`, run-level) and B (`prompt`, node-level) write the agent brief via the CHB-028 / `agent-task.md` path. They must COMPOSE (goal = run-level objective; prompt = node brief; bead body = fallback), not double-inject. **B↔E reconciliation required at pass-6 integration** — coordinate with the foundation agent owning B. This is the one cross-component coupling for E.
- **`default_max_retry` (kilroy E3) is OUT of scope** for component E — it co-locates in the `graph[...]` block with `goal` but is covered by `traversal_cap` / a separate graph-default attr. It stays permissive (UnknownAttrs) here; no action.
