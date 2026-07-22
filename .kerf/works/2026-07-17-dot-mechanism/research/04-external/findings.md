# DOT Mechanism — External Research: Attractor Model & Ecosystem

Research date: 2026-07-17. Purpose: understand the canonical Attractor model (strongDM)
and its ecosystem implementations, focused on three themes for harmonik's DOT redesign:
**(a) per-node model selection, (b) typed parameters / variable passing, (c) reviewer→implementer feedback loops.**

Sources fetched:
- Canonical spec: https://raw.githubusercontent.com/strongdm/attractor/refs/heads/main/attractor-spec.md
- Products/ecosystem: https://factory.strongdm.ai/products/attractor
- Kilroy (Go impl): https://github.com/danshapiro/kilroy
- Arc (TS impl, convergence loops): https://github.com/point-labs-dev/arc
- attractor-pi-dev (TS impl, cookbook): https://github.com/jhugman/attractor-pi-dev/blob/main/docs/user/cookbook.md
- Web searches (variables/templating; model_stylesheet)

---

## 1. Canonical Attractor model (strongdm/attractor-spec.md)

Attractor is a **non-interactive coding agent** driven by a pipeline expressed as a
**Graphviz DOT directed graph**. Nodes are work stages; directed edges (`->`) are control flow.
The canonical model is deliberately **schema-free** at the parameter level, trading static
safety for handler decoupling and rapid iteration.

### 1.1 Node types (shape -> handler mapping, spec section 2.8)

| Handler type | Shape | Purpose |
|---|---|---|
| `start` | `Mdiamond` | Pipeline entry (no-op) |
| `exit` | `Msquare` | Pipeline exit (no-op) |
| `codergen` | `box` | LLM task (**the default**) |
| `wait.human` | `hexagon` | Human-in-the-loop gate |
| `conditional` | `diamond` | Routing point |
| `parallel` | `component` | Concurrent fan-out |
| `parallel.fan_in` | `tripleoctagon` | Consolidate branches |
| `tool` | `parallelogram` | External execution |
| `stack.manager_loop` | `house` | Supervisor loop |

A `type="..."` attribute can override the shape-derived handler (takes precedence over shape).

### 1.2 Node attributes (spec section 2.6)

```
[label="...", prompt="...", shape=box, type="...",
 max_retries=N, goal_gate=true|false,
 llm_model="...", llm_provider="...", reasoning_effort="low|medium|high",
 fidelity="full|truncate|compact|summary:low|summary:medium|summary:high",
 thread_id="...", class="...", timeout="duration"]
```

Semantics:
- `prompt` — the stage instruction; falls back to `label` if empty. Only `$goal` is expanded.
- `llm_model` / `llm_provider` / `reasoning_effort` — configure the LLM call for this node.
- `max_retries` (int) — additional attempts beyond the first.
- `goal_gate` (bool) — node outcome must reach `SUCCESS`/`PARTIAL_SUCCESS` before the pipeline may exit.
- `class` (comma-separated) — targets model-stylesheet rules (see 1.4).
- `fidelity` — how much prior context is carried into this node's LLM session (see 1.5).

### 1.3 The Outcome contract (spec section 5.2) — the "return value" of every node

```
Outcome:
    status: StageStatus       # SUCCESS | FAIL | PARTIAL_SUCCESS | RETRY | SKIPPED
    preferred_label: String
    suggested_next_ids: List<String>
    context_updates: Map<String, Any>
    notes: String
    failure_reason: String
```

Handler interface: `execute(node, context, graph, logs_root) -> Outcome`.
The `codergen` handler writes a `status.json` in the stage dir; third-party agents/tools can
write their own `status.json` to report an Outcome back — but **the JSON is unvalidated** (no schema).

### 1.4 Model assignment — **three-tier precedence** (spec section 8)

Canonical mechanism. Highest wins:
1. **Explicit node attribute** — `llm_model="gpt-5.2"` on the node.
2. **Model stylesheet** — a graph-level `model_stylesheet` with **CSS-like selectors**.
3. **Graph-level / system default.**

Stylesheet example (spec section 8.6):
```
model_stylesheet="
    * { llm_model: claude-sonnet-4-5; }
    .code { llm_model: claude-opus-4-6; }
    #critical_review { llm_model: gpt-5.2; reasoning_effort: high; }
"
```
Selectors: `*` (universal) < ShapeName < `.class` < `#node_id` (CSS-style specificity).
Nodes opt into a rule with `class="code"` etc. The stylesheet **centralizes** model choice so
individual nodes need not repeat `llm_model`/`llm_provider`/`reasoning_effort`.

### 1.5 Parameter / variable flow (canonical) — minimal

Two distinct channels:

**(a) Prompt templating — `$goal` ONLY.** Direct quote:
> "The only built-in template variable is `$goal`, which resolves to the graph-level `goal`
> attribute. Variable expansion is simple string replacement, not a templating engine."

So `graph[goal="Run tests"]` + `prompt="Run tests for: $goal"`. **No user-declared variables,
no ${var}, no typing, no scoping** in the canonical spec. This is an explicit constraint.

**(b) Runtime context — a shared, schemaless key-value store (spec section 5.1).**
A thread-safe dict shared across all stages. Handlers read `context` and return `context_updates`;
the engine merges them post-execution: `FOR EACH (key,value) IN outcome.context_updates: context.set(key,value)`.
Engine sets built-in keys: `outcome`, `preferred_label`, `current_node`, `last_response`
("truncated text of the last LLM response"), etc. **No schema enforcement** on context values.

`fidelity` controls how much context enters each node's LLM session:
`full` = reuse the LLM session on the same `thread_id` (full history); `compact` = fresh session,
goal + run id only; `summary:high` = fresh session + ~3000-token summary. Thread resolution order:
edge `thread_id` > node `thread_id` > subgraph class > previous node id.

### 1.6 Edge selection — **literal expressions, deterministic** (spec section 10, 3.3)

NOTE — the products page loosely calls edges "expressed in natural language and evaluated by the
LLM," but the actual spec is **literal boolean conditions evaluated by the engine**, not the LLM.
Five-step priority algorithm:
1. **`condition` edges** — evaluate each edge's `condition` expression against context+outcome;
   true edges are eligible. `outcome` = the node's status (`success|retry|fail|partial_success`).
   Combine clauses with `&&`, e.g. `condition="outcome=success && context.tests_passed=true"`.
   Missing context keys resolve to empty string (never match non-empty).
2. `preferred_label` match (from the Outcome).
3. `suggested_next_ids` (from the Outcome).
4. Higher `weight` wins.
5. Lexical order of target node ids.

### 1.7 Review -> implement feedback loop (canonical)

Expressed with a **conditional back-edge** plus context carry-back — there is no dedicated
"feedback" primitive:
```
implement [shape=box, goal_gate=true, prompt="Implement the plan"]
review    [shape=box, prompt="Review the code"]
implement -> review    [condition="outcome=success"]
review    -> implement  [condition="outcome=fail", label="Fix"]
```
How feedback actually reaches the implementer on retry:
- The review node returns `context_updates` (and `notes` / `failure_reason`); these merge into
  shared context. The built-in `last_response` (truncated review text) is injected into the next
  node's prompt. So reviewer feedback informs re-implementation **via the shared context dict +
  last_response injection**, not via a typed message.
- Alternatively `fidelity="full"` + a shared `thread_id` keeps reviewer and implementer in one
  LLM conversation for iterative refinement.

### 1.8 stack.manager_loop — supervisor pattern (spec, "manager" section)

A parent node that drives a child pipeline in **observe -> guard -> steer** cycles:
- **Observe**: ingest child telemetry (active stage, outcomes, retry counts, artifacts).
- **Guard**: score progress; route to continue / intervene / escalate.
- **Steer**: write intervention instructions into the child's active stage directory.
Polls every `manager.poll_interval` (default 45s) up to `manager.max_cycles`; stops on child
`completed`/`failed` or when `manager.stop_condition` is true. Tracks `context.stack.child.status`,
`context.stack.child.outcome`. This is the canonical "supervisor steers an implementer loop" shape.

### 1.9 parallel.fan_in — consolidation

Waits for all branches, then **selects the best candidate**. If the fan-in node has a `prompt`,
an LLM ranks candidates; else heuristic ranking (SUCCESS > FAIL, then by score). Candidates live
under `context.parallel.results`; winner recorded as `parallel.fan_in.best_id`.

### 1.10 Typed / validated I/O — **none, by design**

No formal input/output typing anywhere. The only fixed contract is the `StageStatus` enum in the
Outcome. `context_updates` and `status.json` are arbitrary maps with no schema enforcement.

---

## 2. Ecosystem (factory.strongdm.ai/products/attractor)

Canonical model as pitched: a "graph-structured pipeline" — nodes = work phases governed by core
prompts; properties: determinism, observability at transitions, resumability from checkpoints,
composability. ~20 community implementations across languages. The ones relevant to our themes:

| Impl | Lang | Emphasis (theme relevance) |
|---|---|---|
| **Fabro** (fabro-sh/fabro) | Rust | Branching, loops, human gates; **CSS-like model routing** (models) |
| **Kilroy** (danshapiro/kilroy) | Go | English->pipeline; isolated git worktrees; runtime `--force-model` (models) |
| **attractor-pi-dev** (jhugman) | TS | Backend-agnostic `CodergenBackend`; **extends vars with defaults + `--set`** (typed params) |
| **Arc** (point-labs-dev/arc) | TS | **Convergence loops, fresh context per attempt, persistent failure learnings** (feedback) |
| **F#kYeah** (TheFellow/fkyeah) | F# | CSS-like stylesheets for routing; automatic retry loops; checkpoint/resume |
| **amolstrongdm** | Python | Probabilistic satisfaction scoring; multi-agent roles (Coding/Validator/Debugger/Planner) |
| **attractor-rb** (aliciapaz) | Ruby | 13 built-in **linting rules for pipeline validation** (a validation angle) |
| Forge, samueljklee, coreydaley, SoulCaster, brynary, etc. | many | multi-provider clients, SSE, REST/dashboards, conformance testing |

(Forge's listed URL smartcomputer-ai/forge resolved to an unrelated "Lightspeed" repo — see Failures.)

---

## 3. Kilroy (danshapiro/kilroy) — Go, local-first CLI

- **Graph model:** DAG in DOT; each completed node = a **git checkpoint commit on a run branch**;
  per-node artifacts `{logs_root}/{node_id}/{prompt.md,response.md,status.json}`.
- **Models — three mechanisms, highest first:**
  1. **Runtime override flag** `--force-model openai=gpt-5.4 --force-model google=gemini-3-pro-preview`
  2. Node attribute `[... llm_provider=anthropic, llm_model=claude-opus]`
  3. Global `model_stylesheet` (`* { llm_provider: openai; llm_model: gpt-5.4; }`).
  -> Adds a **per-run CLI override layer** above the canonical two tiers. Useful for A/B / cost control.
- **Params:** README does **not** document inter-node typed params/templating beyond canonical;
  outputs persisted to disk artifacts / context. Extra knobs: `max_tokens`, `max_agent_turns=300`,
  `reasoning_effort`, global `runtime_policy.max_llm_retries: 6`.
- **Feedback:** human gates + conditional edges + retry; live intervention via
  `kilroy attractor status/stop` and an experimental HTTP server
  (`POST /pipelines/{id}/questions/{qid}/answer`) to answer pending human-gate questions.
- Deeper docs to mine if needed: `docs/strongdm/attractor/{attractor-spec.md,coding-agent-loop-spec.md,kilroy-metaspec.md}`, `skills/create-dotfile/SKILL.md`.

---

## 4. attractor-pi-dev (jhugman) — the typed-params extension worth stealing

This implementation **extends** the canonical `$goal`-only model into a real (lightweight)
variable system. Direct from its cookbook:

**Declare vars with defaults in the graph:**
```
graph [
    goal="Deploy $service to $env",
    vars="service, env=staging, region=us-east-1"
]
```
- `vars` lists variables; `name=default` gives a default, bare `name` is required.
- Referenced anywhere in prompts/labels with `$name`.

**Override at runtime (repeatable flag):**
```
attractor run deploy.dot --set service=api --set env=production --set region=eu-west-1
```
`--set` overrides defaults from `graph[vars]`. This is the "typed params / variable passing"
capability the canonical spec deliberately omits — added at the **graph/run boundary** (not
edge-scoped, not per-node). Still string-typed (no schema), but with required-vs-default distinction.

Model config mirrors canonical `model_stylesheet` + per-node `llm_model`. Feedback examples:
implement<->validate back-edge `gate -> implement [condition="outcome!=success", label="Retry"]`,
and `fidelity="full"` + shared `thread_id` for conversational refinement. Human gates render
interactive menus (`[A] Approve`, `[F] Fix`) routing to different downstream paths.

---

## 5. Arc (point-labs-dev/arc) — the feedback-loop extension worth stealing

- Pipeline is a DOT graph `pipelines/convergence.dot`; the engine **walks it automatically**.
- **Convergence loop with fresh context per attempt:** each coding attempt runs in a **fresh
  context window** to prevent cross-iteration context pollution — the opposite of `fidelity="full"`.
- **Persistent failure learnings:** failed attempts write learnings to a `progress/` directory;
  later attempts reference accumulated insights **without an explicit in-band feedback message**.
  Failures are treated as durable data points, not just retry signals.
- **Holdout scenarios** the agent can't see (`scenarios/`) prevent overfitting/"cheating" to the check.
- Models assigned at the **backend layer** (`backends/`, e.g. Pi RPC as `CodergenBackend`);
  per-node typed params not documented (defers to canonical spec).

Contrast for our design: Attractor-canonical carries feedback **in-band** (context dict +
`last_response` + full-fidelity thread); Arc carries it **out-of-band + durable** (a `progress/`
learnings store re-read on each fresh attempt). Two different, combinable strategies.

---

## 6. Synthesis — what Attractor/ecosystem does that harmonik could adopt

Mapped to our three themes. "Canonical" = strongdm spec; "ext" = an implementation's addition.

### Theme A — per-node model selection
- **Three-tier precedence is the strong idea** (canonical section 8): per-node `llm_model` attr >
  `model_stylesheet` CSS-like rules (by `*`/shape/`.class`/`#id`) > graph default. This gives
  "set once, override precisely" ergonomics — a graph can say "everything Sonnet, `.code` nodes
  Opus/high-reasoning, `#critical_review` a different provider" in ~3 lines.
- **Add Kilroy's per-run `--force-model provider=model` override** on top for A/B, cost, and
  incident response without editing the graph.
- Model config is a **node/graph concern, not a typed node input** — keeps the graph declarative.
- **Adopt for harmonik:** replace any hardcoded-per-node model with (1) a stylesheet keyed by node
  class/role (reviewer vs implementer vs planner), (2) per-node override attr, (3) a run-level
  `--force-model`. `reasoning_effort` as a first-class per-node/stylesheet knob is cheap and high-value.

### Theme B — typed parameters / variable passing
- Canonical is **intentionally minimal**: only `$goal` string-substitution + a schemaless shared
  context dict. Powerful because simple, but no typing/validation and no declared inputs.
- **The adoptable upgrade is attractor-pi-dev's `graph[vars]` + `--set`**: declare named variables
  with defaults (required vs defaulted), reference as `$name` in prompts/labels, override per-run
  with repeatable `--set k=v`. This is the smallest step from "one magic $goal" to "a parameterized,
  reusable pipeline template" — and it is exactly what harmonik wants for typed params.
- **Two flow layers to keep distinct** (both from canonical): (1) **pipeline variables** =
  inputs/config, resolved once at the graph/run boundary; (2) **runtime context** = node-to-node
  data (`context_updates` merged into a shared dict, `last_response` auto-injected). Don't conflate.
- **Adopt for harmonik:** implement a `vars` declaration (with required/default + optional a *type*
  or *enum* annotation — going one step past pi-dev's string-only to get real validation), keep the
  shared-context dict for inter-node data, but consider **naming/scoping context keys** (canonical's
  schemaless dict is the weakest part — collisions and no validation). A declared output-key contract
  per node (even if optional) would give harmonik the "typed/validated" edge the ecosystem lacks.

### Theme C — reviewer -> implementer feedback loops
- Canonical expresses it as a **conditional back-edge** `review -> implement [condition="outcome=fail"]`;
  feedback rides back **in the shared context** (`context_updates`, `notes`, `failure_reason`) and via
  auto-injected `last_response`; `fidelity="full"` + shared `thread_id` keeps it a single conversation.
- **`goal_gate=true`** makes a node a hard quality gate the pipeline can't exit past — the clean
  primitive for "don't ship until review passes."
- **`stack.manager_loop`** (observe->guard->steer, writing steering instructions into the child's
  stage dir) is the canonical **supervisor** shape — a reviewer/manager that actively steers an
  implementer loop rather than just pass/fail routing.
- **Arc's durable `progress/` learnings** = an out-of-band, persistent feedback store re-read on each
  fresh attempt; pairs well with fresh-context-per-attempt to avoid context rot. Plus **holdout
  scenarios** to stop the implementer overfitting the reviewer's check.
- **amolstrongdm's explicit role split** (Coding / Validator / Debugger / Planner) + probabilistic
  satisfaction score generalizes the review loop into a multi-agent critique.
- **Adopt for harmonik:** model the feedback loop as (1) a conditional back-edge + `goal_gate` for
  the pass/fail contract, (2) a **structured feedback payload** carried in a *named* context key
  (not just truncated `last_response`) so the implementer gets the reviewer's actual findings, and
  (3) optionally Arc-style **durable per-attempt learnings** + fresh context to prevent the loop from
  degrading over iterations. Consider the `manager_loop` supervisor shape for "steer, don't just retry."

### Cross-cutting "powerful + easily controllable" ideas
- **DOT graph = the whole spec** — declarative, observable at every transition, resumable from
  per-node checkpoints (canonical) and git-checkpoint-per-node (Kilroy). Easy to read, diff, review.
- **CSS-like stylesheets** generalize beyond models — a single specificity mechanism (`*`/shape/
  `.class`/`#id`) to attach any cross-cutting config to node sets. Powerful controllability lever.
- **Deterministic literal edge conditions** (not LLM-judged) keep routing reproducible/testable —
  worth preserving over the "natural-language edges" framing the marketing page implies.
- The ecosystem's gap — **no typed/validated I/O** — is precisely where harmonik can differentiate:
  optional declared var-types + named per-node output contracts would make the graph both more
  powerful (composable, checkable) and more controllable than any current Attractor implementation.

---

## Failures / caveats
- `smartcomputer-ai/forge` (listed on the products page) fetched as an unrelated "Lightspeed"
  repo — likely renamed/moved; Forge details not verified. Did not block the research.
- The products page's "edges expressed in natural language, evaluated by the LLM" is **contradicted
  by the canonical spec**, which uses literal, engine-evaluated `condition` expressions. Trust the spec.
- Canonical spec has **no `vars`/`--set`**; that is an attractor-pi-dev (and similar) extension —
  labeled as "ext" above so we don't attribute it to the canonical model.
