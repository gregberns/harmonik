# Sub-Workflow Dispatch

```yaml
---
title: Sub-Workflow Dispatch
spec-id: sub-workflow-dispatch
requirement-prefix: SW
status: draft
spec-shape: requirements-first
spec-category: foundation-cross-cutting
version: 0.1.0
spec-template-version: 1.1
owner: standard-bead-dot-author
last-updated: 2026-06-11
depends-on:
  - execution-model
  - handler-contract
  - workflow-graph
---
```


## 1. Purpose
This spec consolidates the **sub-workflow node dispatch** contract for `workflow_mode = dot` workflows into one normative source. The DOT cascade engine and the sub-workflow data types are already landed; what remains is the dispatch behavior that wires a `sub-workflow`-type node into the parent run's execution — the action the DOT cascade currently stubs as out of scope (`internal/daemon/dot_cascade.go`, the `core.NodeTypeSubWorkflow` case). This spec makes that dispatch behavior normative so the implementation bead can replace the stub.

The contract is **mechanism**, not cognition ([execution-model.md §4.2 EM-006]): expansion, namespacing, acyclicity rejection, event emission, and outcome escape are all deterministic; no model judgment is consulted to decide whether or how a sub-workflow expands.

This spec does not re-own the requirements it binds. The expansion semantics, namespacing rule, acyclicity rule, lifecycle events, and terminal-outcome rule are owned normatively by [execution-model.md §4.8] and [workflow-graph.md §4]; this spec restates the **dispatch obligations** that consume them, anchored to the runner boundary in `internal/handler/runtime.go` (`SubWorkflowRunner`, `SubWorkflowRunSpec`). Where this spec and its `depends-on` specs conflict, the owning spec wins.

## 2. Scope
### 2.1 In scope
- The **in-place expansion** dispatch action for a `sub-workflow` node within a parent DOT run (SW-001).
- The **node-ID namespacing** the dispatch produces and threads into state/transition records (SW-002).
- The **acyclicity check** the dispatch performs at expansion time, before any expanded node executes (SW-003).
- The three-tier **graph-resolution** order the dispatch follows to locate the target sub-workflow (SW-004).
- The **lifecycle events** the dispatch emits on entry and exit, carrying the parent `run_id` (SW-005).
- The **terminal-outcome escape** the dispatch propagates verbatim to the parent cascade (SW-006).
- The **`SubWorkflowRunner` handler-boundary** contract that the daemon's DOT cascade invokes instead of `Handler.Launch` (SW-007).
- The **context-update discipline** observed during expanded-child execution (SW-008).
- The **graph-driven-mode** constraint on `sub-workflow` nodes — valid only under `dot` (and the `single` carve-out), never `review-loop` (SW-009).
- The **no review-loop sub-workflow** constraint (SW-010).

### 2.2 Out of scope
- The on-disk schema of a `sub-workflow` node's attributes (`sub_workflow_ref`, `workflow_version`, `input_mapping`) — owned by [workflow-graph.md §4 WG-006].
- The durable expansion-pin record shape and its restart-reconstruction rule — owned by [execution-model.md §4.8 EM-034c]; this spec references the pin but does not redefine it.
- The `sub_workflow_entered` / `sub_workflow_exited` event names and payload field lists — owned by [event-model.md §8.1.9 / §8.1.10] (the typed payloads land in `internal/core/subworkflowenteredpayload.go` and `subworkflowexitedpayload.go`); this spec requires emission, not the wire format.
- The `Outcome` schema — owned by [execution-model.md §4.1 EM-005].
- The pre-run validator's static resolvability and acyclicity checks — owned by [execution-model.md §4.9 EM-038] and [workflow-graph.md §10 WG-029]; this spec owns the **runtime** acyclicity re-check at expansion (SW-003), which is a distinct dispatch obligation.
- Workflow-mode resolution — owned by [execution-model.md §4.3 EM-012a].

## 3. Glossary
- **sub-workflow node** — a workflow node whose `type = sub-workflow` per [workflow-graph.md §4 WG-001]. It carries no `handler_ref` per [execution-model.md §4.2 EM-007] and dispatches no handler subprocess.
- **expansion** — the runtime operation that replaces a sub-workflow node's position in the parent graph with the sub-workflow's own nodes and edges, in place, within the parent run. (see SW-001)
- **parent run** — the run whose `run_id` is the sole run identifier for the entire nested execution; no child `RunID` is allocated. (see SW-001)
- **parent node ID** — the `node_id` of the sub-workflow node in the parent graph; the namespace prefix for the expanded children. (see SW-002)
- **namespaced node ID** — an expanded child node's `node_id`, rewritten to `<parentNodeID>/<subNodeID>`. (see SW-002)
- **expanded sub-graph** — the set of namespaced nodes and edges produced by expanding one sub-workflow node (the `core.SubWorkflowExpansion` record). (see SW-001)
- **terminal outcome** — the `Outcome` produced by the last expanded node executed before the expanded sub-graph reached one of its terminal nodes; the value that escapes to the parent cascade. (see SW-006)
- **the runner** — the `SubWorkflowRunner` interface (`internal/handler/runtime.go`) whose `Run` method the DOT cascade calls to execute a sub-workflow node. (see SW-007)

## 4. Normative requirements

### 4.1 Expansion and identity

#### SW-001 — Sub-workflow node expands in place within the parent run
When the DOT cascade ([execution-model.md §7.5]) encounters a node of type `sub-workflow`, the daemon MUST expand the referenced sub-workflow's nodes and edges in place within the parent run's execution graph. The dispatch MUST NOT allocate a new `RunID` and MUST NOT spawn a child run; the parent `run_id` is the sole run identifier for the entire nested execution. The expanded sub-graph is materialized as the `core.SubWorkflowExpansion` record (`internal/core/subworkflowexpansion.go`), which MUST satisfy `Valid() == true` before any expanded node is dispatched. Expansion is keyed on the sub-workflow version resolved at workflow-load time per [execution-model.md §4.8 EM-034c]; a sub-workflow registry update between load and runtime expansion MUST NOT change the expanded shape. This binds [execution-model.md §4.8 EM-034].

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

#### SW-002 — Expanded node IDs are namespaced `<parentNodeID>/<subNodeID>`
On expansion, every expanded child node's `node_id` MUST be rewritten to the form `<parentNodeID>/<subNodeID>`, where `parentNodeID` is the sub-workflow node's `node_id` in the parent graph and `subNodeID` is the child's `node_id` in the sub-workflow source. The parent node's own `node_id` MUST remain unchanged. For nested expansions the rule composes left-to-right (a grandparent `A` containing sub-workflow node `B` containing sub-workflow node `C` yields `A/B/C`). Every node reference in the expanded edges, the expansion's `StartNodeID`, and every entry in `TerminalNodeIDs` MUST carry the already-namespaced form. State records and transition records produced during expanded execution MUST carry the namespaced `node_id`. This binds [execution-model.md §4.8 EM-034a] (and [workflow-graph.md §4 WG-006]).

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

#### SW-003 — Resolved sub-workflow graph MUST be acyclic; reject cycles at expansion
The dispatch MUST verify, at expansion time and before any expanded node executes, that the directed graph whose vertices are workflows and whose edges are sub-workflow references is acyclic. A direct or indirect sub-workflow self-reference MUST cause the dispatch to fail closed: the daemon MUST NOT execute any expanded node, and MUST route the run to the `needs-attention` close path with failure class `structural` per [execution-model.md §4.10 EM-046a]. This runtime check is distinct from — and MUST hold even if the pre-run validator was bypassed — the static acyclicity obligation of [execution-model.md §4.9 EM-038] / [workflow-graph.md §10 WG-029]. This binds [execution-model.md §4.8 EM-034b].

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

### 4.2 Resolution

#### SW-004 — Sub-workflow graph resolution is three-tier
The dispatch MUST resolve the target sub-workflow graph by walking the following tiers in order and selecting the first tier that produces a registered, parseable graph:

1. **Explicit reference.** The sub-workflow node's `sub_workflow_ref` attribute (a target workflow `name` per [workflow-graph.md §4 WG-006]), resolved against the daemon's registered `.dot` artifact set at the pinned `workflow_version`.
2. **Project-default graph.** When `sub_workflow_ref` does not resolve to a registered artifact AND a project-default workflow is present, the file `<projectDir>/workflow.dot` is the resolution target.
3. **Error.** When neither tier produces a registered, parseable graph, the dispatch MUST fail closed: it MUST NOT execute any node, and MUST route the run to the `needs-attention` close path with failure class `structural` per [execution-model.md §4.10 EM-046a].

The resolved graph version is the value pinned at workflow-load time per [execution-model.md §4.8 EM-034c]; resolution MUST NOT re-consult the registry for a newer version. This aligns [workflow-graph.md §4 WG-006].

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

### 4.3 Events and outcome

#### SW-005 — Emit `sub_workflow_entered` on expansion and `sub_workflow_exited` on completion, carrying the parent `run_id`
On entering a sub-workflow expansion (after acyclicity passes per SW-003 and the graph resolves per SW-004), the dispatch MUST emit the `sub_workflow_entered` lifecycle event per [event-model.md §8.1.9], and the entry checkpoint MUST carry the expansion pin per [execution-model.md §4.8 EM-034c]. On the expansion completing (the expanded sub-graph reaching a terminal node), the dispatch MUST emit the `sub_workflow_exited` lifecycle event per [event-model.md §8.1.10]. Both events MUST carry the **parent** `run_id` and the parent namespaced `node_id` (the sub-workflow node's `parent_node_id`) per SW-002; they MUST NOT carry a synthesized child run identifier (none exists per SW-001). The `sub_workflow_exited` payload's `terminal_outcome_status` field MUST carry the terminal outcome's status per SW-006. This binds [execution-model.md §4.8 EM-036].

Tags: mechanism
Axes: llm-freedom=none; io-determinism=best-effort; replay-safety=safe; idempotency=non-idempotent

#### SW-006 — Terminal outcome of the expanded sub-graph propagates verbatim to the parent cascade
The `Outcome` that escapes a sub-workflow node — consumed by the parent's edge-selection cascade ([execution-model.md §4.10 EM-041]) on the edges leaving the sub-workflow node — MUST be the `Outcome` produced by the last expanded node executed before the sub-graph reached one of its terminal nodes. The dispatch MUST propagate this `Outcome` verbatim: its `status`, `kind`, `payload`, `failure_class`, and `context_updates` MUST NOT be rewritten, synthesized, or aggregated at the sub-workflow boundary. The `context_updates` applied by that last-expanded-node `Outcome` MUST have already been applied to the run's shared context per [execution-model.md §4.10 EM-041a] before escape, so the parent cascade observes post-update context state. An expanded sub-graph with multiple terminal nodes reaches exactly one at runtime; the `Outcome` that produced that terminal-reaching transition is the terminal outcome. The parent sub-workflow node MUST NOT declare its own `Outcome` shape. This binds [execution-model.md §4.8 EM-036a] and [handler-contract.md §5 HC-061].

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

### 4.4 Handler boundary

#### SW-007 — `SubWorkflowRunner` is the dispatch boundary; the DOT cascade MUST call it, not `Handler.Launch`
A `sub-workflow` node carries no `handler_ref` per [execution-model.md §4.2 EM-007], so the DOT cascade MUST NOT call `Handler.Launch` for it. Instead, when the cascade's dispatch loop encounters a node of type `core.NodeTypeSubWorkflow`, it MUST call `SubWorkflowRunner.Run` (`internal/handler/runtime.go`) with a `SubWorkflowRunSpec` whose `Run` is the parent run, `ParentNodeID` is the sub-workflow node's `node_id`, `SubWorkflowRef` is the resolved reference, and `SubWorkflowVersion` is the load-time pin; the spec MUST satisfy `Valid() == true`. The runner is responsible for: (1) resolving the target sub-workflow graph per SW-004; (2) checking acyclicity per SW-003; (3) building the namespaced `SubWorkflowExpansion` per SW-001/SW-002; (4) emitting the entry and exit events per SW-005; (5) running the cascade over the expanded nodes; and (6) returning the terminal `Outcome` per SW-006. A non-nil `error` returned from `Run` denotes an unrecoverable infrastructure failure (event-emission, graph-load); the daemon MUST map it to `run_failed`. Structural failures within the expanded sub-graph (cascade failure, missing edge) MUST be surfaced as an `Outcome` with `status = FAIL` and an appropriate `failure_class`, not as a returned `error`. The daemon composition root wires a concrete `SubWorkflowRunner` at startup.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

### 4.5 Context discipline

#### SW-008 — Expanded-child context updates obey the registered-key discipline
Handlers dispatched for the expanded children of a sub-workflow execute against the **parent run's** shared context (there is no separate child context). Their `Outcome.context_updates` MUST obey the registered-key discipline of [handler-contract.md §5 HC-062]: only keys in the active (parent) workflow's `context_keys` registered-key list are routable as edge-LHS terms; an unregistered key MUST trigger the daemon's `context_update_unregistered_key` warn-and-drop per HC-062 and MUST NOT enter the run's shared context. The dispatch MUST NOT introduce a sub-workflow-scoped registered-key list — the parent workflow's `context_keys` attribute governs the entire nested execution. Per [handler-contract.md §5 HC-061], no handler may emit a synthetic `Outcome` at the sub-workflow boundary itself; the boundary is a graph-level construct, not a handler dispatch site.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

### 4.6 Mode constraints

#### SW-009 — Sub-workflow nodes are valid only under DOT-mode (and `single`) runs
A `sub-workflow` node MUST be dispatched only within a run whose resolved `workflow_mode` ([execution-model.md §4.3 EM-012a]) admits graph-driven composition: `dot` (the general workflow-graph walker of [execution-model.md §7.5]) or `single` (per [execution-model.md §4.3 EM-015d]'s carve-out that sub-workflow nodes MAY appear inside a `single`-mode node-level workflow). The `review-loop` cycle is mode-driven, not graph-driven, and the EM-034 expansion rule does not apply to it. This spec's dispatch obligations (SW-001..SW-008) are written for the `dot` cascade dispatch site; a `single`-mode run reuses the same expansion mechanism per [execution-model.md §4.8].

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

#### SW-010 — No review-loop sub-workflows
A `dot` (or `single`) workflow MUST NOT reference a sub-workflow whose `workflow_mode` is `review-loop`. The review-loop cycle is the hardcoded two-node implementer→reviewer machine of [execution-model.md §4.3 EM-015d]; it is mode-driven, not graph-driven, and is NOT a sub-workflow per [execution-model.md §4.8]. The DOT validator ([execution-model.md §7.5.3]) MUST reject such a reference; if a non-conforming reference reaches dispatch, the dispatch MUST fail closed and route to `needs-attention` with failure class `structural` per [execution-model.md §4.10 EM-046a]. This binds the [execution-model.md §4.3 EM-015d] carve-out and [execution-model.md §7.5.5] (`A dot workflow MUST NOT reference a review-loop sub-workflow`).

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

## 5. Invariants
- **SW-INV-001 — Single run identity.** Across an arbitrary depth of nested sub-workflow expansion, exactly one `RunID` exists: the parent `run_id`. No dispatch path allocates a child `RunID`. (binds SW-001)
- **SW-INV-002 — Verbatim outcome.** The `Outcome` observed by the parent cascade on a sub-workflow node's outgoing edges is byte-equal to the last-expanded-node `Outcome`; the boundary performs no rewrite, synthesis, or aggregation. (binds SW-006)

## 6. Conformance
**Core v0.1.** Conformant when SW-001..SW-010 and both invariants hold. The following are implementation-test obligations the impl bead MUST satisfy (mirrors [execution-model.md §8 EM-034..EM-037 sub-workflow scenario coverage]):
1. **Acyclicity rejection.** A `dot` workflow whose sub-workflow reference graph contains a direct or indirect cycle MUST cause the dispatch to fail closed at expansion: no expanded node executes, the run routes to `needs-attention` with `failure_class = structural`, and no `sub_workflow_entered` event is emitted (SW-003).
2. **Namespacing format.** For a parent node `A` containing sub-workflow node `B` containing sub-workflow node `C`, the state and transition records produced during expanded execution carry node IDs in the exact `A/B/C` left-to-right composed form (SW-002).
3. **Outcome escape.** The `Outcome` the parent cascade routes on (edges leaving the sub-workflow node) is byte-equal to the last-expanded-node `Outcome` — `status`, `kind`, `payload`, `failure_class`, and `context_updates` all match — and the `sub_workflow_exited` payload's `terminal_outcome_status` equals that `Outcome`'s status (SW-006, SW-INV-002).
4. **Parent run_id on events.** Both `sub_workflow_entered` and `sub_workflow_exited` carry the parent `run_id` and the parent namespaced `node_id`, and no child run identifier appears anywhere in the nested execution's records (SW-005, SW-INV-001).
5. **Resolution order.** With both an explicit `sub_workflow_ref` and a `<projectDir>/workflow.dot` present, the explicit reference resolves (tier 1 wins); with neither present, the dispatch fails closed with `failure_class = structural` (SW-004).
6. **No review-loop sub-workflow.** A `dot` workflow referencing a `review-loop`-mode sub-workflow is rejected (validator) / fails closed at dispatch (SW-010).

## 7. Open questions
None at draft. The dispatch contract reuses landed types (`SubWorkflowExpansion`, `SubWorkflowRunSpec`, `SubWorkflowRunner`, the entered/exited payloads) and binds already-`reviewed` requirements in `execution-model`, `workflow-graph`, and `handler-contract`.

## 8. Cross-spec coordination
This spec is the dispatch-behavior consolidation point for the requirements owned elsewhere. The owning specs carry the normative definitions; this spec restates the dispatch obligations and MUST be kept consistent with them:
- [execution-model.md §4.8 EM-034 / EM-034a / EM-034b / EM-034c / EM-035 / EM-036 / EM-036a] — expansion, namespacing, acyclicity, pin durability, checkpoint coverage, events, terminal outcome.
- [execution-model.md §4.2 EM-007], §4.3 EM-012a, §4.3 EM-015d, §7.5 — handler-ref discipline, mode resolution, review-loop carve-out, DOT-mode binding.
- [workflow-graph.md §4 WG-001 / WG-006], §10 WG-029 — node-type enum, sub-workflow attribute set, static acyclicity.
- [event-model.md §8.1.9 / §8.1.10] — the two lifecycle event names and payload field lists.
- [handler-contract.md §5 HC-058 / HC-061 / HC-062] — per-node-type Outcome obligations, sub-workflow boundary no-emit rule, registered-key discipline.

## 9. Revision history
| Date | Version | Author | Summary |
|---|---|---|---|
| 2026-06-11 | 0.1.0 | agent (kerf `standard-bead-dot` work, epic hk-o7j) | Initial draft. Consolidates the sub-workflow dispatch contract for `workflow_mode = dot` into SW-001..SW-010 plus two invariants, to replace the out-of-scope stub at the `core.NodeTypeSubWorkflow` case in `internal/daemon/dot_cascade.go`. Binds expansion/namespacing/acyclicity/events/terminal-outcome to [execution-model.md §4.8 EM-034 family / EM-036 / EM-036a], the `SubWorkflowRunner` boundary to `internal/handler/runtime.go`, resolution to [workflow-graph.md §4 WG-006], and context discipline to [handler-contract.md §5 HC-058 / HC-061 / HC-062]. |
