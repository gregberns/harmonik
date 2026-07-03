# Attractor + kilroy Parity — Problem Space

> Pass 1 (`problem-space`) of the `attractor-parity` spec work. Grounded in `/tmp/sdlc-corpus/_parity-research.md` (the parity research pass, 2026-05-28) and the harmonik DOT specs (`workflow-graph.md`, `execution-model.md`, `handler-contract.md`). This is a planning artifact; it does not modify `specs/`.

## 1. What is changing and why

harmonik's `workflow_mode=dot` engine has a proven sequential cascade (the review-loop runs end-to-end, v69), but it cannot yet execute an **Attractor/kilroy-class pipeline** — the natural-language-spec DOT-runner family that harmonik adopted its Outcome+context model from (`D-attractor-adoption`). The two live kilroy pipelines (`sentry-triage`, `sentry-bugfix`) make the gaps concrete:

- **~Half of all nodes are shell `tool_command` nodes** (`parallelogram` shape: pytest, `make ci`, `gh`, `git`, sentry, ast-grep, file-based loop counters). harmonik's only non-agentic handler is `noop` — it synthesizes SUCCESS and runs *nothing*. There is no way to execute a shell command and map its exit code to an Outcome.
- **Each LLM (`box`) node carries its own `prompt="..."`**. harmonik agentic nodes derive their brief from the *bead* (`beadTitle`/`beadDescription`); a multi-node pipeline where each box has a distinct prompt has no home.
- **Analysis nodes legitimately produce no commit** (investigate / dedup / review). harmonik hard-fails any agentic node whose HEAD doesn't advance ("implementer didn't advance HEAD").
- **Per-node model/effort is unaddressed** — kilroy picks Opus for `class="hard"` nodes, Sonnet otherwise; harmonik resolves ONE model per run, applied identically to every node.
- **No graph-level `goal` nor template-param substitution** — both pipelines carry `goal="..."` with `__ISSUE_NUMBER__`-style placeholders; harmonik has no parameterized-instance launch surface.

The research pass's architecture verdict is **clean-add**: the Outcome envelope (EM-005), the five-step cascade (EM-041), and the handler-contract dispatch surface already accommodate everything these five capabilities need. The gaps are missing *node attrs* and missing *dispatch branches*, **not** missing *contracts*. All additions land additively under the mixed unknown-attribute policy (WG-031) as a minor schema bump (N-1 readable per WG-034). The only capability that would touch a load-bearing abstraction — parallel fan-out / join — is out of scope (below).

**Why now.** DOT execution is proven live end-to-end (HANDOFF v69). The next unlock is *workload* parity: making harmonik run the real SDLC pipelines it was modeled on. This is the bridge from "harmonik runs its own review-loop" to "harmonik runs arbitrary kilroy-class processes."

## 2. Goals

What should be true about the **system (specs)** after this work:

- G1. A workflow author can declare a node that runs a shell command, with a timeout, and have its exit code mapped deterministically to `outcome.status` + `failure_class`. (KEYSTONE — hk-l8rpd)
- G2. An agentic node can carry an inline `prompt` that becomes its brief, independent of any bead. (hk-sdnzj)
- G3. An agentic node can be declared non-committing — it returns SUCCESS by producing a work product (working files / outcome), without requiring HEAD to advance. (hk-69asi)
- G4. A node can select its own model/effort, overriding the run-level default. (hk-q8nqr)
- G5. A graph can carry a `goal` string and `__PLACEHOLDER__` template params that are substituted at launch from a supplied param map. (E1)
- G6. The above are expressible such that both live kilroy pipelines (or harmonik-native ports) are *representable* in harmonik's DOT vocabulary, and the specs say how each is dispatched.

These are spec-level goals. The implementation epics (the `dot_cascade.go` exec branch, launch-spec threading, the invariant relaxation, the substitution pass) are derived in pass-7; the success criteria below are about what the **specs describe**, per the kerf jig.

## 3. Non-goals

- **Parallel fan-out / join.** A real Attractor-spec primitive (`parallel` / `parallel.fan_in`) that harmonik deliberately defers per EM-059, architecture.md §4.6, and the closed-set enum WG-001. NOT used by either live kilroy pipeline (both are strictly sequential — diamonds pick ONE successor). It is the *only* capability requiring load-bearing rework (`dot_cascade.go`'s single-`currentNodeID` walk; `SelectNextEdge`'s pick-ONE-edge contract; the one-agent-per-worktree invariant, workspace-model §4.5/§4.7). Lifting these is a separate, lower-priority, post-parity capability — explicitly out.
- **Gate evaluator (hk-karlz).** Orthogonal. kilroy uses `diamond` *conditional checks* (edge conditions on the existing cascade), not policy Gates. The gate-evaluator seam is its own work.
- **Generating `.dot` from natural-language goals.** No spec; reserved for future tooling (workflow-graph §2.2).
- **A new node type.** The tool/shell capability rides the **existing** `non-agentic` node type (per OQ-WG-007 / D17), NOT a new enum member — so no schema *major* bump.
- **A second routing channel.** No `retry_target` or model-stylesheet-as-routing; failure_class-as-LHS (WG-020) already gives the expressivity. Model selection is a launch-time resolution, not a routing input.

## 4. Constraints

- **Backwards-compatible with the proven review-loop (v69).** The review-loop is the load-bearing live workload; nothing here may regress it. All five capabilities are opt-in via new optional attrs; a graph that uses none of them behaves exactly as today.
- **Outcome envelope + cascade UNCHANGED — additive only.** EM-005 (the five base Outcome fields) and EM-041 (the five-step cascade) are adopted-verbatim-from-Attractor and must not be restructured. The tool node *emits* a normal `default`-kind Outcome; it does not need a new Outcome shape. failure_class is already a top-level field (WG-018).
- **Mixed unknown-attribute policy (WG-031) governs the new attrs.** New node attrs (`tool_command`, `timeout`, `prompt`, `non_committing`/`auto_status`, `model`/`class`) and the graph-level `goal` must land as *additive* attrs. Reserved-position strictness (WG-031 strict set) must be respected — adding a name to the reserved set is part of the spec change for each attr that the dispatcher consumes.
- **Schema stays N-1 readable (WG-034).** All additions are a minor bump; `schema_version` stays `1`-readable. No major bump (no new node type, no new edge field, no new enum member).
- **Reuse existing model-resolution plumbing.** Per-run model selection already exists: `LaunchSpec.model_preference` (HC-006), the `--model`/`--effort` argv rule (HC-055/HC-055a), and the resolution chain (EM-012b). Per-node selection should *extend* this chain (add a per-node layer), not invent a parallel mechanism.
- **Exit-code → failure_class mapping must be mechanism-tagged.** Per EM §8 / handler-contract §4.4, classification must be determinable without semantic judgment. The tool node's exit-code mapping table is a new mechanism-tagged classifier and must align with the six-class enum (transient / structural / deterministic / canceled / budget_exhausted / compilation_loop).

## 5. Success criteria

Concrete statements about what the specs MUST describe when this work is done:

- SC1. `workflow-graph.md` §4 WG-002 defines a tool/shell capability on the `non-agentic` node type with `tool_command` and `timeout` optional attrs, and the spec defines an **exit-code → `outcome.status` + `failure_class`** mapping (e.g. `0 → SUCCESS`; non-zero → FAIL with a `deterministic`/`transient` classification; timeout → `transient` or `canceled`).
- SC2. `handler-contract.md` adds a normative anchor for the **shell/tool handler** (resolving OQ-WG-007 / D17): how it is invoked, what it emits, its boundary-classification tags. The §7.5.4 dispatch table (EM-058) row for `non-agentic` either covers it or gains a sub-note.
- SC3. `workflow-graph.md` WG-002 adds a `prompt` optional attr to `agentic` nodes; `handler-contract.md` says how `prompt` threads into the LaunchSpec brief (overriding / coexisting with the bead-derived brief).
- SC4. The specs define a **non-committing agentic mode** (a `non_committing` / `auto_status` optional attr): an agentic node so marked returns SUCCESS without requiring HEAD advance; the over-strict HEAD-advance invariant is relaxed to a per-node mode. The Outcome contract is unchanged (SUCCESS-without-commit is already legal).
- SC5. The specs define **per-node model/effort selection** (a `model` / `effort` / `class` optional attr) as an additional layer in the EM-012b resolution chain, ahead of the run-level default. A `class`-based stylesheet is INFORMATIVE unless a concrete need pins it normative (research pass to decide).
- SC6. `workflow-graph.md` defines a graph-level `goal` attr and a **template-param substitution** surface (`__PARAM__` placeholders substituted from a launch-time param map). The substitution point (loader vs. launch) and the param-map source are specified; `goal`/params land under the permissive policy (WG-031).
- SC7. The `failure_class` vocabulary divergence (kilroy `transient_infra` vs harmonik `transient`; `outcome=success` shorthand vs `outcome.status == 'SUCCESS'`) is resolved by either a documented authoring-convention note or a dialect-compat shim — the specs say which.
- SC8. A worked **canonical example** under `specs/examples/` (per WG-036/WG-037) demonstrates a multi-node pipeline exercising tool nodes + inline prompts + a non-committing node, with its sidecar `.md`.

## 6. Affected spec areas (preliminary)

| Spec | Sections likely touched | Nature of change |
|---|---|---|
| `workflow-graph.md` | §4 WG-002 (node-type catalog table — add `tool_command`, `timeout`, `prompt`, `non_committing`/`auto_status`, `model`/`class` to optional attrs); §10 WG-031 (add dispatcher-consumed attr names to the reserved set); new graph-level `goal` + template-param attr (§10/§11 area); §13 OQ-WG-007 (resolve — tool node contract) | Additive normative; minor schema bump |
| `handler-contract.md` | §4.1/§4.2 (new shell/tool handler anchor; `prompt` LaunchSpec threading); §4.2a HC-058 (non-committing Outcome obligation, if needed); §4.5/§8 (exit-code → sentinel mapping for the shell handler) | New anchor + cross-check; additive |
| `execution-model.md` | §7.5.4 EM-058 (dispatch-table row/sub-note for the tool node); §4.3 EM-012b (per-node model-resolution layer); §8 (exit-code → failure_class classifier alignment) | Extension; additive |
| `specs/examples/` | New canonical `.dot` + sidecar (WG-036/WG-037) | New artifact |

## 7. Open questions for later passes

- OQ-1 (research/design): Where does template-param substitution happen — at loader parse-time (graph becomes concrete before validation) or at launch-time per node? Loader-time is simpler but couples the param map to ingest; launch-time keeps the graph reusable. Lean: launch-time, param map supplied alongside the run.
- OQ-2 (design): Is `class`-based per-node model selection (a stylesheet) normative or informative? kilroy uses a CSS-like `model_stylesheet`. Lean: `model`/`effort` attrs normative (direct); `class` + stylesheet INFORMATIVE at v1 unless a real workload needs the indirection.
- OQ-3 (design): Does the non-committing mode reuse `auto_status` (kilroy's spelling) or introduce `non_committing`? They overlap but `auto_status` also implies outcome-derivation-from-work-product. Lean: one attr; pick the spelling in design.
- OQ-4 (design): Exit-code → failure_class mapping granularity. A bare `0/non-zero → SUCCESS/FAIL(deterministic)` is the floor; kilroy routes on `transient_infra` vs `deterministic`. Does the tool node expose a way for the author to declare which exit codes are transient (a `transient_exit_codes` attr), or is everything non-zero `deterministic` at v1? Lean: `deterministic` floor at v1; author-declared transient codes is a follow-up.
