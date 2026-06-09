# Research: Cross-Handler Task Transfer When One Handler Is Paused

**Bead:** hk-bm9qm  
**Ref:** docs/components/internal/handler-pause-and-resume.md §9.2  
**Status:** research-only; no implementation in this bead.

---

## 1. Question framing

Handler-pause-and-resume §9.2 identifies an open question: when a handler type
(e.g. `claude-code`) is paused, could pending queue items be re-bound to a
different handler (e.g. `codex`) automatically — if the workflow declared
`agent_type` as a fallback list rather than a singleton?

This memo answers four questions:

1. Is fallback declaration a workflow-graph attribute (node-level) or a policy
   attribute (control-points)?
2. What is the semantic cost of fallback (different agents produce different
   outputs; equivalence is not guaranteed)?
3. Do we need a `handler-equivalence-class` concept, or is fallback always
   operator-specified?
4. Concrete proposal for `specs/execution-model.md §4.2` node-attribute changes
   if fallback is recommended.

---

## 2. Finding: fallback declaration belongs at the node level — with a policy-level seam reserved

### 2.1 The current model

Per `specs/workflow-graph.md §4 WG-002`, an `agentic` node carries exactly one
`agent_type` string (required). Per `WG-003`, the field is an open-set string;
the closed set of known agent types is informational only. Handler resolution
runs at dispatch time via `handler-contract.md §4.3`.

The node attribute set is **static graph data** — validated at load time, not
at dispatch time (except for unknown-type resolution which surfaces as a
`structural` failure). This is the same treatment given to `model`, `effort`,
and `non_committing`.

### 2.2 Node-level vs. control-points (policy-level)

**Node-level (add `agent_type_fallbacks`):**
- The workflow author explicitly declares: "if `claude-code` is unavailable,
  try `codex`, then `pi`."
- Consistent with the existing `model`/`effort` per-node-override pattern
  (EM-012b-NODE): the node encodes dispatch preferences, the daemon enforces
  them.
- Static graph data → replay-determinism is preserved (same inputs → same
  dispatch choice for a given handler-availability state).
- Validation is straightforward: the fallback list items are subject to the same
  open-set posture as `agent_type` (WG-003); unknown values surface as
  `structural` failure at dispatch time, not ingest time.

**Policy-level (new ControlPoint kind or handler-group config):**
- A central policy declares `handler-equivalence-groups` (e.g. `coding-llm =
  [claude-code, codex, pi]`); nodes bind to a group name.
- Decouples handler topology from graph artifacts: adding `gemini` to the group
  does not require modifying every `.dot` file.
- More consistent with control-points architecture (CP §4.1, CP-002) when the
  equivalence relationship is an **operational concern** (which handlers are
  available in this deployment) rather than a **workflow concern** (what the
  node is doing).

**Recommendation: node-level as the primary mechanism, policy-level seam
reserved.**

The node-level approach fits the current model with minimal new concepts and
preserves the "static graph is authoritative" invariant. The policy-level
approach has merit when the deployment topology changes independently of graph
authors (multi-tenant or managed deployments). Reserve a `handler_group_ref`
node attribute (analogous to `gate_ref`) for the policy-level path in a future
pass; it does not need to be declared at MVH.

---

## 3. Semantic cost

This is the decisive constraint. The fallback mechanism is only safe when the
operator explicitly accepts the semantic equivalence assumption.

### 3.1 Outputs differ between handler types

`claude-code` and `codex` are not drop-in substitutes:
- Code style, docstring conventions, diff shape, and error messages are
  handler-specific.
- Review-loop semantics break if the implementer uses claude-code on iteration 1
  and codex on iteration 2: the reviewer's brief (including `last_diff_hash`,
  `claude_session_id`, and `context.iteration_count` per EM-012) is built
  around the prior handler's output. The reviewer cannot meaningfully continue
  the same review thread across a handler switch.
- Skill provisioning (`required_skills`, EM-008) is handler-specific.
  A skill installed for claude-code may not exist for codex, causing a
  `structural` failure on the fallback attempt.
- The `axis_tags` on the node (llm-freedom, io-determinism, replay-safety) are
  authored for the primary handler; a different handler may not satisfy the same
  axis constraints, violating the four-axis invariant of EM-011.

### 3.2 Safe cases

Fallback is semantically safe only in a narrow class of nodes:
- **Stateless, non-committing analysis nodes** (e.g. `non_committing="true"`,
  `idempotency_class=idempotent`) where the output is a structured artifact
  (a JSON report, a lint score) and the downstream cascade routes on the
  *structure* of the outcome, not on its prose or diff content.
- **Gate-evaluator nodes** where the gate policy encodes the equivalence
  (the gate decision is deterministic given the inputs, independent of which
  handler runs the evaluation).
- **First-iteration implementer nodes** where no prior session state exists and
  the fallback produces a fresh commit whose content is independently reviewable.

In all other cases — especially `review-loop` re-entry, partial-success
recovery, and nodes carrying `required_skills` — the semantic cost is
significant enough that automatic fallback is operationally dangerous.

### 3.3 Consequence for the dispatch mechanism

Fallback MUST NOT be automatic. The dispatch loop MUST treat fallback as
opt-in per node, and the node's fallback list is a hint, not an obligation.
The operator retains the `harmonik handler resume <type>` path as the primary
resolution. Fallback is a "skip to next available handler" action at the
operator's explicit consent (either declared statically in the graph, or
triggered by a future `harmonik handler transfer` command).

---

## 4. Handler-equivalence-class concept

Three design options:

| Option | Description | Verdict |
|---|---|---|
| **A: Operator-specified per-node** | `agent_type_fallbacks` list on each node; no shared concept. | Recommended for first iteration. Simple, explicit, static. |
| **B: Named equivalence class** | A new policy artifact declares `handler_group: coding-llm = [claude-code, codex]`; nodes bind via `handler_group_ref`. | Reserve for post-MVH. Useful at operator scale (many nodes, many deployments). |
| **C: Axis-tag capability matching** | The daemon infers equivalences from matching four-axis tags across handler implementations. | Do not recommend. Axis tags are node attributes, not handler capability declarations; the daemon has no authoritative handler-capability registry. |

**Conclusion:** fallback is always operator-specified (Option A per node,
or Option B as a shared policy). The handler-equivalence-class concept (Option B)
is worth reserving as a future seam but not needed at the stage where this
feature would ship. A named group avoids per-node repetition in large graphs
and keeps the equivalence decision outside the workflow artifact (closer to the
control-points discipline), but it is additive: Option A can ship first and
Option B can layer on top without breaking changes.

---

## 5. Concrete proposal: `specs/execution-model.md §4.2` changes

If the feature is recommended (no implementation in this bead), the following
spec amendments would be required.

### 5.1 `agentic` node attribute: `agent_type_fallbacks` (optional, list of string)

Add to the optional-attribute set of `agentic` nodes (currently declared in
`specs/execution-model.md §4.2 EM-007` and mirrored in
`specs/workflow-graph.md §4 WG-002`):

> **`agent_type_fallbacks`** (list of string, optional). When present, a
> non-empty ordered list of alternative `agent_type` values the daemon MAY use
> when the primary `agent_type` is paused at dispatch time. Each list element is
> subject to the same open-set posture as `agent_type` per WG-003; an
> unresolved element at dispatch time is skipped (emits a
> `fallback_handler_unresolved` event) and the next element is tried. If all
> primary and fallback agent types are paused or unresolvable, the item is held
> per the existing `queue_item_held_for_handler_pause` path. A list element MUST
> NOT repeat the primary `agent_type` (validation error at ingest).
>
> The daemon MUST NOT auto-select a fallback unless the operator has explicitly
> set `--allow-handler-fallback` in daemon configuration (process-lifecycle.md
> §4.1 PL-004a reserved slot). When the flag is absent, `agent_type_fallbacks`
> is retained in the AST and inert; the item is held per the normal handler-pause
> path. This preserves opt-out safety: a graph authored with `agent_type_fallbacks`
> does not silently activate cross-handler routing on deployments that have not
> opted in.

### 5.2 Dispatch-loop change (informative, not in this bead)

The `dispatch_eligible` check in §7.4 that currently evaluates
`HandlerPauseController.IsPaused(node.agent_type)` would extend to:

```
IF IsPaused(node.agent_type) AND flag_allow_handler_fallback THEN
    FOR each f IN node.agent_type_fallbacks DO
        IF NOT IsPaused(f) THEN selected_agent_type = f; BREAK
    END
    IF selected_agent_type still == node.agent_type THEN hold item
ELSE IF IsPaused(node.agent_type) THEN hold item
```

### 5.3 `specs/workflow-graph.md §4 WG-002` change (informative)

Add `agent_type_fallbacks` to the optional-attribute column for `agentic` nodes,
alongside the existing `prompt`, `model`, `effort`, etc.

### 5.4 New event: `handler_fallback_activated` (informative, for event-model.md)

When the daemon selects a fallback handler at dispatch time, emit:
`handler_fallback_activated { run_id, bead_id, primary_agent_type, selected_agent_type, node_id }`.
Class O (observability). This event is essential for operator auditability —
without it, operators cannot tell which handler actually ran a bead.

---

## 6. Recommendation summary

| Question | Answer |
|---|---|
| Node-level or policy-level? | **Node-level first** (`agent_type_fallbacks` optional list attribute on `agentic` nodes). Reserve `handler_group_ref` policy seam for Option B post-MVH. |
| Semantic cost? | **High for stateful/review-loop/skill-bound nodes; acceptable for stateless idempotent analysis nodes.** Fallback must be opt-in per node AND per daemon (flag gate). |
| Equivalence-class concept needed? | **Not needed at first iteration.** Operator specifies the fallback list explicitly. Named groups (Option B) are additive and can follow. |
| `execution-model.md §4.2` change? | Add optional `agent_type_fallbacks: [String]` to `agentic` node attribute set. Gate activation on a new `--allow-handler-fallback` daemon flag. See §5 above for full proposed wording. |

**Net position:** The feature is architecturally feasible and fits the current
model with a small, well-scoped extension. The semantic risk is real and should
block automatic fallback behind an explicit daemon opt-in flag. Cross-handler
task transfer is **not safe as a default** and **not safe for review-loop
re-entry**. Shipping the attribute as inert-unless-flagged (with the dispatch
logic gated on the new flag) allows graph authors to annotate their intent today
without activating behavior until an operator consciously enables it.
