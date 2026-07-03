# Spec Draft — C. Non-committing agentic mode (component `non-committing`, hk-69asi)

> Pass 5 (`spec-draft`) of `attractor-parity`. Grounded in `04-design/non-committing-design.md`. Resolves OQ-3 (`non_committing` vs `auto_status`).
> Component-scoped draft (see the note in `tool-node.md`): the integration pass (06) merges this component's WG-002 / WG-031 / handler-contract / execution-model edits with components A and B into the full updated target files.
>
> All changes are ADDITIVE: minor `schema_version` bump (stays `1`-readable per WG-034), no new node type / enum / edge field, Outcome envelope untouched (SUCCESS-without-commit is already legal per EM-005). **CRITICAL:** the relaxation is gated on the per-node `non_committing` attribute AND scoped to `dot` mode only; the v69 review-loop implementer-MUST-commit invariant (EM-015d) is untouched.

---

## Amendment C1 — `workflow-graph.md` §4 WG-002: amend the `agentic` row

In `specs/workflow-graph.md` §4 WG-002 (the node-type catalog table), the `agentic` row's **optional attrs** column gains `non_committing`. This component's contribution; component B adds `prompt` to the same row — integration merges into ONE row whose optional-attrs column reads:

    | `agentic` | LLM-driven | `agent_type`, `handler_ref` | `prompt`, `non_committing`, `idempotency_class`, `axis_tags`, `skills_ref`, `freedom_profile_ref`, `budget_ref`, `hook_ref`, `guard_ref` | SUCCESS, FAIL, RETRY, PARTIAL_SUCCESS | `handler_outcome` | [handler-contract.md §4.5] |

No other column on this row changes.

## Amendment C2 — `workflow-graph.md` §4: new requirement WG-041 (`non_committing` attr semantics)

Insert a new normative requirement in §4 of `specs/workflow-graph.md`. (ID `WG-041` is **provisional and COLLIDES** with component E's `WG-041` (substitution/ordering) and the foundation components' other provisional IDs; integration assigns the final non-colliding sequential ID across A/B/C/D/E and fixes all references including the EM-015d / EM-058 citations.)

> ### WG-041 — Non-committing attribute on `agentic` nodes
>
> An `agentic` node MAY carry a `non_committing` (boolean) optional attribute. When `non_committing="true"` on an **implementer-class** `agentic` node, the node returns `SUCCESS` on a clean agent exit WITHOUT requiring the worktree HEAD to advance past its pre-launch value; the engine does NOT treat a no-commit clean exit as a failure for that node. When `non_committing` is absent or `"false"` (the default), an implementer-class node that exits cleanly without advancing HEAD is a node failure, as in prior behavior.
>
> A `non_committing` clean exit yields `Outcome{status = SUCCESS}` at v1; the engine does NOT inspect a work product, an embedded `{"status":...}` marker, or any other artifact to derive a non-`SUCCESS` outcome from a `non_committing` node. SUCCESS-without-commit is already a legal Outcome per [execution-model.md §4.1 EM-005]; `non_committing` relaxes an engine-side HEAD-advance check, it does not introduce a new Outcome shape.
>
> **Authoring rule (normative).** A `non_committing` node produces no committed work product the engine validates; the engine cannot distinguish a good no-commit exit from a bad one. A workflow author MUST pair every `non_committing` node with a downstream validating `non-agentic` tool node (per [workflow-graph.md §4 WG-039]) that inspects the node's work product and exit-codes the routing decision. The engine does not enforce the pairing; it is an authoring obligation documented in the canonical example sidecar.
>
> **`auto_status` is reserved.** A `non_committing` node controls exactly one axis: commit-or-not. It does NOT derive status from a work product or an embedded marker. The attribute name `auto_status` is NOT accepted as a node attribute at v1 (it would mislead authors into expecting status-derivation that does not exist); `auto_status` is reserved for a future status-derivation feature. Native authoring of pipelines ported from external `auto_status=true` semantics MUST use `non_committing="true"`.
>
> A `non_committing` attribute on a **reviewer-class** `agentic` node, a `non-agentic` node, or a `gate` node is a validation warning at v1 per §10 WG-031 (those dispatch paths do not reach the implementer HEAD-advance check); the value is retained in the AST and ignored.
>
> Tags: mechanism, normative

## Amendment C3 — `workflow-graph.md` §10 WG-031: add `non_committing` to the reserved set

In `specs/workflow-graph.md` §10 WG-031, the reserved-set list in the "Strict positions" bullet gains `non_committing`. This component contributes the addition alongside the other node-attr names (integration merges with A and B):

> ... `idempotency_class`, `axis_tags`, `non_committing`, ... `freedom_profile_ref`, ...

Effect: a loader accepts `non_committing` on a node and rejects it on an edge (or any non-node position) as a reserved-attribute-out-of-position strict error per WG-031. The loader parses `"true"`/`"false"`; any other value is a strict error.

## Amendment C4 — `execution-model.md` §4.3 EM-015d: scope the implementer-MUST-commit invariant to review-loop

In `specs/execution-model.md` §4.3 EM-015d (review-loop mode lifecycle), the implementer-must-produce-a-commit expectation is review-loop-scoped. Add a scoping clause to EM-015d (as a new bullet in the "The cycle MUST observe:" list, or a sentence in the opening paragraph) making the scope explicit so the `dot`-mode `non_committing` relaxation does not appear to contradict it:

> - **Implementer commit obligation is review-loop-scoped.** Under `workflow_mode = review-loop`, the implementer phase MUST advance the worktree HEAD (produce a commit) before the reviewer is launched; a clean implementer exit that does not advance HEAD is a cycle-internal failure routed per [handler-contract.md §4.6]. This commit obligation is specific to `review-loop` mode and does NOT apply to `workflow_mode = dot`. A `dot`-mode `agentic` node MAY relax the HEAD-advance requirement via the per-node `non_committing` attribute per [workflow-graph.md §4 WG-041]; that relaxation is gated on the per-node attribute and never reaches the `review-loop` path.

If EM-015d already states the commit obligation only implicitly, integration adds the explicit scoping clause; the substantive change is the carve-out sentence ("does NOT apply to `workflow_mode = dot`"). No other EM-015d sub-clause changes; the v69 review-loop lifecycle is otherwise untouched.

## Amendment C5 — `execution-model.md` §7.5.4 EM-058 (or §7.5.2): `dot`-mode `non_committing` outcome-derivation note

In `specs/execution-model.md` §7.5 (`dot` mode binding), add a sub-note (anchored under EM-058's table notes, alongside the component-A non-agentic sub-note) describing the `dot`-mode implementer-class outcome-derivation under `non_committing`:

> **Non-committing `agentic` dispatch sub-note.** For a `dot`-mode implementer-class `agentic` node, the engine derives the node Outcome after a clean agent exit as follows: if the node carries `non_committing="true"` per [workflow-graph.md §4 WG-041], a clean exit yields `Outcome{status = SUCCESS}` regardless of whether the worktree HEAD advanced; otherwise a clean exit that did NOT advance HEAD is a node failure. A worktree whose HEAD cannot be resolved at all is a daemon-side error in BOTH modes (a broken worktree is a real failure). This derivation is `dot`-mode-only; `review-loop` mode's implementer commit obligation per §4.3 EM-015d is unchanged.

## Amendment C6 — `handler-contract.md` §4.2a (near HC-058): clarifying note on SUCCESS-without-commit

In `specs/handler-contract.md` §4.2a (the Outcome surface section, near HC-058 / the agentic row), add a clarifying note. No new HC requirement is needed — the existing agentic-node Outcome obligations already permit `SUCCESS` without a commit. The note:

> A `dot`-mode `agentic` node MAY emit `status = SUCCESS` without the run having produced a commit when the node is `non_committing` per [workflow-graph.md §4 WG-041]. `SUCCESS`-without-commit is already a legal Outcome per [execution-model.md §4.1 EM-005]; it is not a new Outcome shape and imposes no new handler obligation. The HEAD-advance expectation that gates a non-`non_committing` implementer node is an engine-side derivation per [execution-model.md §7.5], not a handler-contract obligation.

---

## Cross-component reconciliation flagged for integration (06)

- **I-C-shared-table (with A, B):** WG-002 `agentic` row optional-attrs column is amended by BOTH component B (`prompt`) and this component (`non_committing`). Integration produces ONE merged `agentic` row (see C1 for the merged column). WG-031 reserved set gains `prompt` (B), `non_committing` (C), `tool_command`, `timeout` (A) — ONE merged sentence.
- **I-C-newids:** WG-041 (provisional) must not collide with WG-039 (A) / WG-040 (B); integration assigns final sequential IDs and fixes §14 cross-references.
- **I-C-em015d-scope (I-C4, R-C4) — VERIFY:** the EM-015d edit (Amendment C4) MUST be a scoping clarification that leaves the review-loop lifecycle behavior unchanged. Integration MUST verify no review-loop test regresses and that the carve-out reads as "the commit obligation is review-loop-scoped" rather than "the commit obligation is removed". The v69 review-loop is the proven path and must not regress.
- **I-C-shared-fn (with B):** B (pre-launch task-brief build) and C (post-launch outcome derivation) touch non-overlapping regions of `dispatchDotAgenticNode`; implementation-sequencing note only.
- **I-C-porting (I-C1, R-C1):** document the `auto_status` → `non_committing` porting alias and the "harmonik does NOT accept `auto_status` at v1" rule in the canonical example sidecar and authoring notes.
- **I-C-pairing (I-C2, R-C2):** the "pair `non_committing` with a validating tool node" authoring rule (stated in WG-041) should also appear in the canonical example sidecar and authoring notes.
- **I-C-deferred (I-C5):** the proper `auto_status` status-derivation feature (work-product / embedded marker → FAIL) is a separate future capability — file a follow-up bead; out of scope for parity v1.
