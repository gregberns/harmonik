# Research — Graph goal + template-param substitution (E, E1)

> Pass 3 (`research`) of `attractor-parity`, component **E**. Grounds the design for a graph-level `goal` string and `__PLACEHOLDER__` template-param substitution. Evidence is code-touchpoint + spec-section + kilroy-source. Resolves OQ-1 (substitution point: LOAD vs LAUNCH).

## Research questions

- RQ-E1. What does kilroy actually use `goal` and `__PARAM__` for, and where do the params appear (graph-level only, or inside node attrs)?
- RQ-E2. Does harmonik have ANY graph-level `goal` concept, or any param-substitution surface today?
- RQ-E3. How is a `dot` graph loaded and a run launched — and what is the precise seam where substitution would happen? (OQ-1)
- RQ-E4. Where would a param map come from at launch time, and how does it reach the loader/launch path?
- RQ-E5. How does `goal` relate to the run / the agent brief — is it dispatcher-consumed (reserved) or purely informational?
- RQ-E6. Is this clean-additive, and what must be flagged for integration?

## Findings

### F-E1 — kilroy `goal` is a graph-level string; `__PARAM__` placeholders pervade graph AND node attrs

Both pipelines open with a graph-level `goal` carrying placeholders:

- `sentry-triage/pipeline.dot:3`: `goal="Investigate Sentry issue __SENTRY_SHORT_ID__ and create a GitHub issue if confidence is HIGH or MEDIUM. Repository: __GITHUB_REPO__."`
- `sentry-bugfix/pipeline.dot:3`: `goal="Fix GitHub issue #__ISSUE_NUMBER__. Repository: gigpro/sidegig-mcp. Read .ai/issue.md for full issue details."`

Critically, **placeholders are NOT confined to `goal`** — they appear inside node `tool_command` and `prompt` attrs too:

- `sentry-triage/pipeline.dot:20` (`init.tool_command`): `... sentry issue view __SENTRY_SHORT_ID__ --json ... sentry api 'issues/__SENTRY_NUMERIC_ID__/events/?limit=5' ...`
- `sentry-triage/pipeline.dot:28,42,52` (node `prompt`s): `__SENTRY_SHORT_ID__`, `__GITHUB_REPO__` embedded in the prompt bodies.
- `sentry-bugfix/pipeline.dot:17` (`init.tool_command`): `gh issue view __ISSUE_NUMBER__ ...`

So the param set seen across the two live pipelines is: `__SENTRY_SHORT_ID__`, `__SENTRY_NUMERIC_ID__`, `__GITHUB_REPO__`, `__ISSUE_NUMBER__`. The token convention is `__UPPER_SNAKE__` (double-underscore delimited, uppercase + digits). **This means substitution must operate over the WHOLE artifact text (or over every attr value after parse), not just the `goal` attr** — a `goal`-only substitution would leave `__ISSUE_NUMBER__` unsubstituted inside the `init` node's shell command, breaking the pipeline. This is the single most important finding for the design (see F-E3 / F-E6).

### F-E2 — harmonik has NO graph-level goal and NO param-substitution surface

`grep -rni "goal|__|substitut|placeholder|template" internal/workflow/dot/*.go` → no matches (excluding tests). The `Graph` struct (`internal/workflow/dot/ast.go:34-79`) has `Name, SchemaVersion, Version, StartNodeID, TerminalNodeIDs, ContextKeys, WorkflowClass, Nodes, Edges, Warnings, UnknownAttrs` — no `Goal` field. The reserved graph-attr set (`internal/workflow/dot/parser.go:509-518`) is `{schema_version, version, start_node, start_node_id, terminal_node_ids, context_keys, workflow_id, workflow_class}` — `goal` is absent, so today a `goal="..."` attr is accepted as an **unknown permissive graph attr** (WG-031/WG-032): warned + retained in `g.UnknownAttrs["goal"]`, not interpreted. There is no template/placeholder pass anywhere in the load or launch path.

The agent brief today comes from the BEAD (`beadTitle`/`beadDescription` → `agent-task.md` via CHB-028, `claudelaunchspec.go:246-273`), not from any graph-level goal. So `goal` is the graph's analogue of the bead description for a non-bead-driven (or bead-augmented) pipeline run.

### F-E3 — Load/launch seam (OQ-1 RESOLVED: substitute at LAUNCH, before parse)

The load path: `internal/daemon/workloop.go:1277` — `graph, loadErr := workflow.LoadDotWorkflow(dotPath)`. `LoadDotWorkflow(dotPath string)` (`internal/workflow/loader.go:56`) reads the file and calls `dot.Parse(src, filename)` (`internal/workflow/dot/parser.go:50`). The validated `*dot.Graph` is then handed to `driveDotWorkflow(..., graph, ...)` (`workloop.go:1291`), which walks it. There is exactly one load per run, inside `beadRunOne`, at claim/dispatch time — there is no separate "graph compile" stage decoupled from the run.

**OQ-1 resolution: substitute at LAUNCH (run-start), not at a distinct LOAD stage — and substitute over the raw `.dot` SOURCE TEXT before `dot.Parse`.** Reasons:

1. **The lean from `01-problem-space.md` OQ-1 is launch-time** ("so the same loaded graph can run with different params"). The current architecture already supports the spirit of this: the on-disk `.dot` is the reusable template; each run re-reads + re-parses it. A param map supplied per run means the same template file produces different concrete graphs per run — exactly the parameterized-instance launch the gap calls for.
2. **Placeholders live inside node attrs, not just `goal` (F-E1).** Substituting the parsed AST would require walking every string field of every node/edge (`tool_command`, `prompt`, `goal`, and any future attr). Substituting the raw source text once, *before* `dot.Parse`, is simpler, total (catches every occurrence including ones in attrs the parser drops into `UnknownAttrs`), and keeps the parser/validator unchanged. This is the decisive reason: source-text substitution is the only point that is both single-pass and complete.
3. **Keeps the loaded/validated graph free of placeholder residue.** Substituting pre-parse means validation runs on the concrete graph — a missing-param token like `__ISSUE_NUMBER__` left in a `start_node` would surface as a normal validation error against the substituted text, not a silent pass.

Concretely: insert a substitution step in `LoadDotWorkflow` (or a new `LoadDotWorkflowWithParams(dotPath, params)` sibling, mirroring the existing `LoadDotWorkflowWithPolicy` at `loader.go:110`) that reads the source, applies `strings.ReplaceAll` for each `__KEY__ -> value` in the param map (or a single regexp pass over `__[A-Z0-9_]+__` tokens), THEN calls `dot.Parse`. The daemon passes the param map from the run-launch context.

### F-E4 — Param-map source: the run/queue-item launch context (`extraContext` is the precedent)

There is an existing per-run free-form injection precedent: `extraContext` (hk-boiwe) — `beadRunOne(..., extraContext string, ...)` (`workloop.go:1123`) threads an operator-supplied string through `driveDotWorkflow` (`workloop.go:1292`, `dot_cascade.go:135`) into `claudeRunCtx.extraContext` → `agent-task.md`. The param map should arrive by the **same channel**: a per-run / per-queue-item `params map[string]string` (or `[]string` of `KEY=VALUE`) supplied at `harmonik run` launch (CLI flag, e.g. `--param ISSUE_NUMBER=172`, mirroring how `--workflow-ref` (hk-qo9pq) selects the `.dot`). It is sealed into the Run record alongside `workflow_ref` so a replay re-substitutes identically (replay-safety). The map is run-scoped, supplied once, applied once at load — no runtime re-evaluation (parallels the EM-012b "resolve once" discipline).

A natural v1 default source: when the run is bead-tied, the bead's structured fields could seed defaults (e.g. an `issue_number` label), but to keep this clean-additive and avoid coupling param-substitution to bead schema, **v1 sources the map from the explicit launch param flag only**; bead-derived defaults are a clean follow-up.

### F-E5 — `goal` semantics: dispatcher-consumed (reserved) and threaded into the brief

For `goal` to be more than a comment, the dispatcher must consume it. Two roles: (a) it is the graph's stated objective, surfaced on `run_started`-class observability and available to tooling; (b) it SHOULD thread into the agent brief — i.e. `goal` becomes (part of) the `agent-task.md` body for agentic nodes when the run is not driven by a bead description, or augments it when it is. The cleanest v1: `goal` is added to the WG-031 **reserved graph-attr set** (so it parses into a typed `Graph.Goal` field, not `UnknownAttrs`), and the launch path injects the (substituted) `goal` into the brief via the existing `extraContext`/`agent-task.md` channel (`claudelaunchspec.go:271`). This avoids a new brief-assembly mechanism — `goal` rides the same payload field that already exists. Whether `goal` overrides or augments the bead description is a small design call (lean: augment — prepend `goal` as the run-level objective, keep per-node `prompt`/bead body as the node brief).

### F-E6 — Clean-additive; substitution-over-source-text is the one non-obvious choice to flag

Additive: new typed `Graph.Goal` field + `goal` in the reserved set (WG-031), a new graph-level template-param surface in `workflow-graph.md`, a substitution step in the loader, a `params` field on the Run record + a `--param` launch flag. Schema stays minor / N-1 readable (WG-034) — `goal` is a graph-level attr (like `workflow_class`), no node-type/edge-field/enum change. A graph with no `goal` and no `__PARAM__` tokens behaves exactly as today (the substitution pass over a token-free source is a no-op; `goal` absent leaves the brief bead-driven).

**Flag for integration (the non-obvious bits):**
1. **Substitution is over raw source text, pre-parse — NOT over the AST** (F-E3). This is a deliberate layering choice (single-pass + complete coverage of in-attr placeholders) and pass-6/spec-draft must state it normatively so an implementer doesn't "helpfully" walk the AST instead and miss `tool_command`/`prompt` tokens. It also means the spec must define the token grammar (`__[A-Z0-9_]+__`) and the behavior on an **unsubstituted token** (lean: a residual `__TOKEN__` after substitution is a launch-time error — fail fast rather than ship a literal `__ISSUE_NUMBER__` into a shell command). This unsubstituted-token-is-an-error rule is the one genuinely new normative behavior, not pure cleanup.
2. **`goal`-into-brief threading** reuses `extraContext`/`agent-task.md` but is a new content source for the brief — pass-6 must reconcile it with the bead-derived brief (CHB-028) so the two don't double-inject. Coordinate with component B (inline-prompt, foundation agent) which also edits the brief-assembly path — `goal` (run-level), `prompt` (node-level), and bead body (node-level fallback) must compose, not collide. **Cross-component dependency: flag for B↔E reconciliation at integration.**
3. The `__PARAM__` substitution and component A's `tool_command` / component B's `prompt` are **the consumers of substituted tokens** — substitution must run before those attrs are read. Since substitution is pre-parse (F-E3), this is automatically satisfied, but the spec should note the ordering invariant (substitute -> parse -> validate -> dispatch).

## Patterns to follow

- **Reuse the existing launch-context channel** for the param map (mirror `extraContext` hk-boiwe and `--workflow-ref` hk-qo9pq), not a new config file.
- **Reserved-set discipline** (WG-031): `goal` must enter the reserved set (dispatcher-consumed). The template-param surface is a load-time text transform, not an attr — it does not need a reserved name, but the spec must define the token grammar.
- **Permissive-then-promote**: `goal` is already accepted permissively today; promoting it to a typed reserved field is backwards-compatible (existing `goal="..."` graphs keep working, now interpreted).
- **Replay-safety / resolve-once** (parallels EM-012b): seal the param map into the Run record; substitute once at load; never re-substitute at runtime.

## Risks / conflicts

- **R-E1 (MEDIUM — design-critical).** A `goal`-only substitution would be wrong: placeholders pervade `tool_command`/`prompt` (F-E1). The design MUST substitute over the full source text. Captured in F-E3/F-E6; not a blocker, but the single most likely implementation mistake.
- **R-E2 (LOW).** Brief double-injection if `goal` and component B's `prompt` both write the brief. Mitigation: B↔E reconciliation at integration (F-E6 #2).
- **R-E3 (LOW).** Unsubstituted-token policy is a new normative behavior (fail-fast vs pass-through). Lean: fail-fast at launch. Design must state it.
- **R-E4 (INFORMATIONAL).** kilroy's `default_max_retry=3` graph attr (E3 in `_parity-research.md`) appears alongside `goal` in the same `graph[...]` block but is OUT of this component's scope (covered by `traversal_cap` / a separate graph-default attr). It stays permissive (UnknownAttrs) here; no action in component E.
