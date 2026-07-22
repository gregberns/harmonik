# Typed Parameters & Dataflow in Workflow Engines — Research Findings

Research for the harmonik `.dot`-driven agent-workflow engine. Goal: learn how established
orchestration engines model **typed parameters**, **dataflow between nodes**, and
**per-node resource/model config** — to inform a strict, FP-flavored typed-param design.

Axes examined per system:
- **Scope:** global (workflow-scoped) vs edge-scoped (task->task on connections). Both?
- **Type system:** is there one on inputs (string/number/bool/enum/object, required/optional)? How strict?
- **Routing:** how outputs of one node reach inputs of the next (explicit map vs implicit namespace).
- **Per-node compute:** any per-node resource/model selection.
- **Validation:** behavior on a missing/mistyped param.

---

## 1. Argo Workflows

Argo is the closest structural analog to a `.dot` DAG engine (it literally runs YAML DAGs).

**Two data channels — the key distinction.** Argo separates:
- **`parameters`** — small string values (config, flags, loop items, up to ~256 kb via `result`).
- **`artifacts`** — larger binary/file blobs handed off through an artifact store (S3, etc.).

This parameters-vs-artifacts split is the single most transferable idea: *small typed control
values travel one way; bulk payloads travel another.* An agent engine's analog: a short typed
value (a verdict, a status enum, a file path) vs. the bulk artifact (the diff, the transcript).

**Scope: BOTH global and edge-scoped, explicitly distinguished by prefix.**
- Workflow-scoped (global): declared in `spec.arguments.parameters`, read anywhere as
  `{{workflow.parameters.NAME}}`.
- Template-scoped (local inputs): declared in a template's `inputs.parameters`, read as
  `{{inputs.parameters.NAME}}`.
- Edge-scoped (task->task): a downstream task references an upstream task's output explicitly:
  - DAG: `{{tasks.generate-parameter.outputs.parameters.hello-param}}`
  - Steps: `{{steps.generate-parameter.outputs.parameters.hello-param}}`
- Global *outputs*: an output param can be promoted to workflow scope with `globalName`, then
  read as `{{workflow.outputs.parameters.NAME}}`.

**Output declaration uses `valueFrom` — outputs are pulled from a source, not just returned:**
```yaml
outputs:
  parameters:
  - name: hello-param
    valueFrom:
      path: /tmp/hello_world.txt      # file contents become the value
```
Scripts/containers also expose stdout as `outputs.result` (capped ~256 kb).

**Routing: explicit templated references.** There is NO implicit "previous node" namespace for
DAG edges — the consumer names the producer task and the specific output parameter. Dependencies
(`dependencies: [A]` / `depends: "A && B"`) control ordering; parameter references control
dataflow. The two are separate concerns (an edge can carry ordering without data, or data).

**Type system: weak.** Parameters are fundamentally strings interpolated into command args.
There is no first-class string/number/bool/enum typing on `inputs.parameters` in core Argo; type
is by convention. (Argo does support an `enum` list for UI-suggested values on
`spec.arguments.parameters`, and `valueFrom.default`.) Required-ness is implicit: an unresolved
`{{...}}` reference or a missing required parameter fails the workflow at submission/resolution.

**Per-node compute: yes, per-template.** Each template can set its own container `image`,
`resources` (CPU/memory requests/limits), `nodeSelector`, and `activeDeadlineSeconds`. This is
the direct analog of "choose a model/resource per node" — resource selection is a per-node
(per-template) property, not global.

**Validation:** missing required param or an unresolvable `{{...}}` -> workflow fails
(resolution error). Type mismatches are not caught (everything is a string).

Sources:
- https://argo-workflows.readthedocs.io/en/latest/walk-through/parameters/
- https://argo-workflows.readthedocs.io/en/latest/walk-through/output-parameters/
- https://argo-workflows.readthedocs.io/en/latest/walk-through/dag/

---

## 2. Temporal

Temporal is code-first (no declarative graph), so its lesson is about *typing discipline*, not
graph wiring.

**Scope: edge-scoped by function call.** Data flows as ordinary function arguments and return
values:
- Workflow gets typed input args; returns a typed result.
- A workflow invokes activities and passes typed args; each activity returns a typed value that
  the workflow code receives directly.
- There is no global parameter namespace — everything is lexical/return-value dataflow, exactly
  like an FP program. "Global" state only appears as workflow-instance state the code holds.

**Type system: strong, borrowed from the host language.** Types are enforced by the SDK language
(Go structs, TypeScript types, Java classes). At the wire boundary, values are serialized through
a **Data Converter** into **Payloads** (JSON by default; pluggable/custom/encrypted). So typing is
compile-time in-process, and structural (serializable) at the boundary. Signals, queries, and
updates also carry typed payloads.

**Routing: implicit via language binding** — you literally pass the return value of one call into
the next. No string interpolation, no named-output lookup. The compiler is the validator.

**Per-node compute:** activities are pinned to **task queues**; workers polling a queue provide
the compute. Choosing which worker/queue runs an activity ~= choosing the execution resource per
node. Retry policies, timeouts are per-activity.

**Validation:** a serialization/deserialization mismatch (Payload can't decode into the expected
type) fails at the data-converter boundary; in-process type errors are caught by the compiler.

Sources:
- https://docs.temporal.io/encyclopedia/workflow-message-passing
- https://docs.temporal.io/develop/typescript/core-application
- https://docs.temporal.io/develop/go/core-application

---

## 3. GitHub Actions (reusable workflows)

The most declarative, most explicitly *typed* input contract of the systems surveyed — and the
closest model to what a `.dot` node-input schema could look like.

**Scope: BOTH, explicitly separated.**
- Workflow-level typed inputs declared under `on.workflow_call.inputs` (the reusable-workflow
  contract).
- Edge-scoped passing via `with:` on the calling job (the call site maps caller values -> callee
  inputs).
- Outputs are declared at workflow level and consumed downstream via `needs.<job>.outputs.<name>`.

**Type system: yes — explicit, one of `string | boolean | number`, plus `required` + `default`:**
```yaml
on:
  workflow_call:
    inputs:
      config-path:
        required: true
        type: string
    outputs:
      firstword:
        value: ${{ jobs.example_job.outputs.output1 }}
    secrets:
      token:
        required: true
```
Caller:
```yaml
jobs:
  call:
    uses: octo-org/repo/.github/workflows/reusable.yml@main
    with:
      config-path: .github/labeler.yml
    secrets: inherit          # or explicit map
```
"The data type of the input value must match the type specified in the called workflow (boolean,
number, or string)." **Secrets are a separate, explicitly-typed channel** — you cannot smuggle a
secret through a normal input; it must be passed as a secret (or `inherit`ed). This
secrets-as-a-distinct-channel idea mirrors Argo's params-vs-artifacts split.

**Routing: explicit mapping on the edge.** `with:` is a name->value map at the call site; outputs
climb step->job->workflow via explicit `${{ steps.x.outputs.y }}` / `${{ jobs.j.outputs.o }}`
re-exports. Nothing is implicit; each hop is written.

**Per-node compute: yes.** `runs-on:` selects the runner (ubuntu-latest, a labeled self-hosted
runner, GPU pool, etc.) per job — a direct analog to per-node model/resource selection.

**Validation:** required input missing or type mismatch -> the run fails fast at start
(validation before execution). This is the "validate the contract before running anything" model.

Sources:
- https://docs.github.com/en/actions/how-tos/reuse-automations/reuse-workflows

---

## 4. Airflow / Dagster / Prefect

Three points on a spectrum from untyped-global-bus (Airflow) to strictly-typed-edges (Dagster).

### Airflow — XCom (untyped, task-addressable "bus")
- **Scope:** a shared key/value store keyed by `(dag_id, task_id, key)`. Effectively global
  storage, but consumers pull **edge-style** by naming the source: `xcom_pull(task_ids='src')`.
  Default key is `return_value`; operators auto-push results there. (Airflow 3 tightened this:
  `xcom_pull()` without `task_ids` pulls only the current task — less implicit than Airflow 2.)
- **Type system:** none enforced. XCom accepts "any serializable value." TaskFlow (`@task`)
  passes typed Python return values between decorated functions, but Airflow does not check them.
- **Routing:** explicit pull by `task_ids` (+ optional `key`), OR implicit in TaskFlow where
  `b(a())` wires `a`'s return into `b`.
- **Warning transferable to agents:** XComs are for *small* values — "do not pass around large
  values like dataframes." Same params-vs-artifacts lesson: keep the typed channel small; put
  bulk in object storage.
- **Validation:** missing XCom -> returns `None` (silent) unless the task asserts; no type check.

### Dagster — typed ops (the FP-typing exemplar)
- **Scope: edge-scoped.** Ops declare `ins`/`outs`; the `@job`/`@graph` wires an upstream op's
  output into a downstream op's input by passing it as an argument. Execution is
  dependency-driven: "an op only starts to execute once all of its inputs have been resolved."
- **Type system: strong and pluggable.** Inputs/outputs carry `DagsterType` with an arbitrary
  runtime `type_check_fn`; Python type annotations are honored directly.
```python
@dg.op(ins={"abc": dg.In(dagster_type=MyType)})
def my_op(abc): ...

@dg.op(out={"first": dg.Out(), "second": dg.Out()})
def multi_output_op():
    return 5, 6

MyType = dg.DagsterType(type_check_fn=lambda _, v: v % 2 == 0, name="MyType")
```
- **Routing: explicit multi-output.** Named `outs` let one op emit several typed values, each
  routed to distinct downstream inputs — a clean model for "implement node emits {diff, summary,
  status}" fanning to different consumers.
- **IO managers:** storage of the value *between* ops is a separable, swappable concern
  (an `IOManager`) — decoupling "what the value is" (typed) from "where it lives" (storage).
  This is exactly the params-vs-artifacts idea generalized: the type stays in the graph, the
  bytes go wherever the IO manager puts them.
- **Validation:** the `type_check_fn` runs at runtime as data crosses the edge; a failing check
  raises and the op fails.

### Prefect — typed flow params via Pydantic
- **Scope:** function args/returns; downstream tasks consume upstream **futures** that resolve to
  data. Edge-scoped, like Temporal.
- **Type system:** flow/task parameters are validated & **coerced** via Pydantic from Python type
  hints. "Prefect automatically performs type conversion of inputs using any provided type hints."
- **Validation (notable):** a deployment run with invalid parameters "moves from Pending to
  Failed without entering Running" — **validate-before-execute**, same fail-fast posture as
  GitHub Actions.

Sources:
- https://airflow.apache.org/docs/apache-airflow/stable/core-concepts/xcoms.html
- https://docs.dagster.io/guides/build/ops
- https://docs.prefect.io/v3/concepts/flows

---

## 5. n8n / Windmill

Visual node graphs — closest UX analog to a `.dot` canvas.

### n8n — items flow on connections (untyped, positional)
- **Scope: edge-scoped.** The unit of dataflow is an **array of items**, each item an object with
  a `json` key (and optional `binary` key). This array travels along the connection from one node
  to the next; a node processes each item and emits a new array.
- **Cross-node reads:** beyond the immediate incoming edge, expressions can reach *any* prior
  node's output: `{{ $json.name }}` (current item's json), `$binary`, `$node["NodeName"].json`,
  `$items(...)`. So the immediate connection is edge-scoped, but the expression layer grants
  global read access to upstream nodes by name.
- **Type system: none.** Items are untyped JSON; n8n even auto-wraps/repairs the `{json: ...}`
  shape. Structure is by convention (the array-of-`{json,binary}` envelope), not a schema.
- **Routing:** implicit-positional (the connection carries the array) plus explicit expression
  references by node name. Binary is a separate key on each item (again the small-vs-bulk split).
- **Validation:** none at graph-load; a bad expression resolves at runtime (error on that item).

### Windmill — typed steps from script signatures (strong, auto-derived)
- **Scope: edge-scoped with any-ancestor reach.** "The input of any step can be the output of any
  previous step, hence every Flow is a DAG." Input transforms reference:
  - `flow_input` — the flow's initial (global) parameters,
  - `results.{id}` — the output of any previous step (not just the immediate predecessor),
  - `resource(path)` / `variable(path)` — external config.
- **Type system: yes, auto-derived JSON Schema.** A step's inputs are typed by the **JSON schema
  generated from the script's function signature** — you write a typed function, Windmill infers
  the input schema and validates against it. Strong typing without hand-writing a schema.
- **Routing:** per-input **input transforms** — small JS expressions (`results.a.foo`,
  `flow_input.bar`) mapping upstream results / flow input into this step's named parameters.
  Explicit, per-parameter mapping (like GitHub `with:`), but expression-powered.
- **Per-node compute:** each step is an independent script (its own language/runtime), and can
  carry its own concurrency/retry/timeout config — per-node resource selection.
- **Validation:** inputs are schema-validated; a value not matching the derived JSON schema is
  rejected.

Sources:
- https://docs.n8n.io/data/data-structure/
- https://docs.n8n.io/data/expression-reference/
- https://www.windmill.dev/docs/flows/architecture

---

## Synthesis — an FP-principled typed-param model for a small agent-graph engine

### A. Global vs edge-scoped — use BOTH, with a sharp rule for each
Every mature system that has both keeps them **lexically distinguished** (Argo's
`workflow.parameters.*` vs `tasks.X.outputs.*`; GitHub's workflow `inputs` vs `needs.*.outputs`;
Windmill's `flow_input` vs `results.id`). Recommendation:

- **Global (graph-scoped) inputs** for values that are *ambient to the whole run* and read by many
  nodes: the target repo/branch, the task/goal text, run-wide policy (max review rounds, model
  budget), operator config. These are the graph's function signature. Read via an explicit
  namespace, e.g. `graph.inputs.<name>`.
- **Edge-scoped (node->node) values** for everything a node *produces for a specific successor*:
  the implement node's diff/summary/status flowing to the review node; the review verdict flowing
  back to the feedback/implement node. Read via `node.<id>.outputs.<name>`.

Rule of thumb: **if removing a producer node would make the value undefined, it belongs on an
edge; if the value exists before any node runs, it's global.** Avoid a promiscuous global bus
(Airflow XCom / n8n `$node[...]`): global read-anywhere access defeats static analysis and makes
the graph's real dataflow invisible. Prefer edges as the default; reserve global for genuine
ambient config. Windmill's `results.{any prior id}` is a reasonable middle ground *only because*
each reference is still explicit and statically resolvable.

### B. A minimal but strict type system
Best-of-breed = **GitHub's explicit primitive types + Dagster's pluggable type checks +
Windmill's schema-from-signature**, kept small:

- **Scalars:** `string`, `number`, `bool`, `enum(<values>)`. Enum is high-value for agent graphs —
  a review verdict is `enum(approve, request_changes, block)`, and enum-typing an edge lets the
  `.dot` conditional edges (which branch on that value) be validated against the producer.
- **Compound:** `object` (JSON Schema) and `artifact` (an opaque handle/ref, NOT inlined).
- **Modifiers:** `required` (default) / `optional` + `default`. Optional-with-default is what lets
  a node be reused across graphs.
- **The params-vs-artifacts split is the load-bearing decision.** Adopt Argo's separation
  wholesale: **typed scalar/enum/small-object values travel as parameters on edges; bulk payloads
  (diffs, full transcripts, file trees) travel as `artifact` references** whose bytes live in a
  store, with only the handle typed into the graph (Dagster IO-manager style). This keeps the
  typed dataflow layer small, inspectable, and cheap to validate — every system that skipped this
  (Airflow, n8n) had to bolt on a "don't put big things here" warning.

Keep the type system *structural and declarative* (JSON-Schema-shaped), not a full language type
system — you want it checkable at graph-load without executing anything.

### C. Explicit output->input mapping (no implicit "previous node")
Every robust system makes the consumer **name the producer and the specific output**
(`{{tasks.X.outputs.parameters.Y}}`, `needs.J.outputs.O`, `results.id.field`,
`In(dagster_type=...)` wiring). The implicit-namespace designs (n8n positional array, Airflow
default `return_value`) are the ones that produce silent-wrong-data bugs. For the `.dot` engine:

- A node declares a **named, typed output set** (Dagster's multi-`out` — e.g. implement emits
  `{diff: artifact, summary: string, status: enum}`).
- Each edge carries an **explicit mapping** `producer.output -> consumer.input` (GitHub `with:` /
  Windmill input-transform style). The `.dot` edge is the natural home for this map: an edge label
  `A.diff -> review.patch` both draws the arrow and binds the data.
- Ordering-only edges (dependency without data) are allowed and distinct from data edges (Argo's
  `dependencies` vs parameter refs) — keep them separable so a node can wait-for without
  consuming.

### D. Validation at graph-load (fail before any agent runs)
The strongest posture observed is **GitHub Actions / Prefect: validate the whole parameter
contract before entering Running** ("Pending -> Failed without Running"). For an agent engine this
matters even more — a run costs model tokens and wall-clock, so a mistyped edge should never
launch an agent. At `.dot` load time, statically check:

1. Every consumer input is bound (an edge maps something into it) OR has a default OR is optional
   — else **unbound-input error**.
2. Every edge reference names a real producer node and a declared output — else **dangling-ref
   error**.
3. Producer output type is assignable to consumer input type (incl. enum-value compatibility for
   conditional edges) — else **type-mismatch error**.
4. Every global (`graph.inputs.*`) reference resolves to a declared graph input.
5. (FP bonus) the graph is a DAG / no cycles except explicitly-marked feedback edges; feedback
   edges must carry the loop-bounding param (max rounds).

Runtime validation still needed for values only knowable at execution (Dagster's `type_check_fn`
on the actual payload, Temporal's data-converter decode) — a two-tier check: **static contract at
load, structural check at each edge crossing.**

### E. Per-node model/resource override
Universal pattern: resource selection is a **per-node property with a graph-level default**
(Argo per-template `image`/`resources`; GitHub per-job `runs-on`; Temporal task-queues; Windmill
per-step runtime). Recommendation:

- A **graph-global default model/budget** (a global input), **overridable per node**
  (`node.model = "opus" | "sonnet" | "haiku"`, plus per-node `max_tokens`/timeout/retry).
- Treat the model choice as *typed config*, not dataflow — it's a node attribute in the `.dot`,
  not something that flows on an edge. This mirrors how every engine keeps `runs-on`/`resources`
  as node metadata separate from the parameter graph.
- Keep it degradation-friendly (per the project's no-external-version-binding rule): model names
  are advisory labels with a fallback, not hard pins.

### Tradeoffs / tensions to flag
- **Strictness vs reuse.** Dagster/Windmill-strength typing catches wiring bugs at load but makes
  nodes less drop-in; mitigate with `optional`+`default` and structural (not nominal) types.
- **Edge-explicit vs ergonomics.** Explicit per-edge mapping is verbose (GitHub `with:` fatigue).
  A convention like "same-named output auto-binds to same-named input unless overridden" recovers
  ergonomics without losing static checkability — but make the auto-bind *resolved and recorded*
  at load, never a runtime lookup.
- **Global config vs hidden coupling.** Every global input a node reads is invisible in the graph
  topology; keep the global set tiny (run identity + policy) so the `.dot` remains the truth of
  dataflow.
- **Static vs runtime validation.** You cannot statically type an agent's free-text output; the
  realistic contract is *strict types on the small control values (status/verdict/counters) that
  drive branching*, and *artifact handles for the free-form bulk* — validate the former hard, pass
  the latter by reference.
