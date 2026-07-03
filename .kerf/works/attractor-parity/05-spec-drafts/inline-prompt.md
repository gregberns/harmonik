# Spec Draft — B. Inline per-node prompt (component `inline-prompt`, hk-sdnzj)

> Pass 5 (`spec-draft`) of `attractor-parity`. Grounded in `04-design/inline-prompt-design.md`.
> Component-scoped draft (see the note in `tool-node.md`): the integration pass (06) merges this component's WG-002 / WG-031 / handler-contract edits with components A and C into the full updated target files.
>
> All changes are ADDITIVE: minor `schema_version` bump (stays `1`-readable per WG-034), no new node type / enum / edge field, Outcome envelope untouched (`prompt` is input-only), v69 review-loop path unaffected (reviewer-class `prompt` is inert at v1).

---

## Amendment B1 — `workflow-graph.md` §4 WG-002: amend the `agentic` row

In `specs/workflow-graph.md` §4 WG-002 (the node-type catalog table), the `agentic` row's **optional attrs** column gains `prompt`. The amended row reads (this component's contribution; component C adds `non_committing` to the same row — integration merges):

    | `agentic` | LLM-driven | `agent_type`, `handler_ref` | `prompt`, `idempotency_class`, `axis_tags`, `skills_ref`, `freedom_profile_ref`, `budget_ref`, `hook_ref`, `guard_ref` | SUCCESS, FAIL, RETRY, PARTIAL_SUCCESS | `handler_outcome` | [handler-contract.md §4.5] |

No other column on this row changes.

## Amendment B2 — `workflow-graph.md` §4: new requirement WG-040 (`prompt` attr semantics)

Insert a new normative requirement in §4 of `specs/workflow-graph.md`. (ID `WG-040` is **provisional and COLLIDES** with component E's `WG-040` (goal) and component A's provisional `WG-039`; integration assigns the final non-colliding sequential ID across A/B/C/D/E and fixes all references including HC-006a's WG-040 citation.)

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
> **Reviewer-class scope (v1).** A `prompt` on a **reviewer-class** `agentic` node (resolved by the node's reviewer-class binding, e.g. `agent_type="reviewer"` / `handler_ref="claude-reviewer"`) is accepted-but-inert at v1: the reviewer's brief is sourced from the review-target artifact per [execution-model.md §4.3 EM-015d (sub-clause EM-015d-RIA)] and is NOT overridden by `prompt`. The loader retains the `prompt` attribute in the AST and emits no error; the value is ignored for reviewer-class dispatch. Reviewer-class `prompt` override is reserved for a future schema version.
>
> A `prompt` on a `non-agentic` or `gate` node is a validation warning at v1 per §10 WG-031 (those node types dispatch no agent that reads a brief); the value is retained in the AST and ignored.
>
> Tags: mechanism, normative

## Amendment B3 — `workflow-graph.md` §10 WG-031: add `prompt` to the reserved set

In `specs/workflow-graph.md` §10 WG-031, the reserved-set list in the "Strict positions" bullet gains `prompt`. The merged reserved-set sentence (with components A and C contributions) is produced by integration; this component contributes the addition of `prompt` alongside the other node-attr names:

> ... `idempotency_class`, `axis_tags`, `prompt`, ... `freedom_profile_ref`, ...

Effect: a loader accepts `prompt` on a node and rejects `prompt` appearing on an edge (or any non-node position) as a reserved-attribute-out-of-position strict error per WG-031.

## Amendment B4 — `handler-contract.md` §4.2 HC-006a: amend the `agent-task.md` content row

In `specs/handler-contract.md` §4.2 HC-006a (per-phase LaunchSpec field requirements table), the `agent-task.md` path and content row's `implementer-initial` cell currently reads "contains bead body, no prior-verdict section". Append a clause to that cell (or add a note below the table):

> The `agent-task.md` Body for a `dot`-mode implementer-class `agentic` node MAY be sourced from the node's `prompt` attribute per [workflow-graph.md §4 WG-040], overriding the bead-derived body; the bead `Title` and bead ID remain in the header. This override is `dot`-mode-only and does not apply to `review-loop` phases. No LaunchSpec required field (HC-006), no wire-protocol obligation, and no Outcome obligation changes — `prompt` affects the rendered task-artifact Body only.

The HC-006a table's `working_dir`, `argv`, `env`, `claude_session_id`, and `phase`/`iteration_count` rows are unchanged.

---

## Cross-component reconciliation flagged for integration (06)

- **I-B-shared-table (with A, C):** WG-002 `agentic` row optional-attrs column is amended by BOTH this component (`prompt`) and component C (`non_committing`). Integration produces ONE merged `agentic` row whose optional-attrs column is `prompt`, `non_committing`, `idempotency_class`, `axis_tags`, `skills_ref`, `freedom_profile_ref`, `budget_ref`, `hook_ref`, `guard_ref`.
- **I-B-newids:** WG-040 (provisional) must not collide with component A's WG-039 / component C's new WG requirement; integration assigns final sequential IDs and fixes §14 cross-references.
- **I-B-shared-fn (with C):** the implementation threading for B (pre-launch task-brief build) and C (post-launch outcome derivation) touch the same `dispatchDotAgenticNode` function but non-overlapping regions; this is an implementation-sequencing note, not a spec-text conflict.
- **I-B-substitution (I-B1, R-B3):** if a future graph-level `goal` + `__PARAM__` substitution component lands, the substitution pass MUST run over the per-node `prompt` too (kilroy prompts embed `__SENTRY_SHORT_ID__`-style sentinels). That component owns the substitution surface; integration wires it to cover both `goal` and `prompt`. Out of scope for this draft.
- **I-B-inert-doc (I-B2, R-B1):** reviewer-node `prompt` accepted-but-inert at v1 — integration should document this in the canonical `specs/examples/` sidecar and authoring notes.
