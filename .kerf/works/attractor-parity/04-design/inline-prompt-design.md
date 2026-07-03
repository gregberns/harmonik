# Change Design — B. Inline per-node prompt (hk-sdnzj)

> Pass 4 (`change-design`) of `attractor-parity`. Normative design for the `prompt` node attr on agentic nodes. Grounded in `03-research/inline-prompt/findings.md`.

## 1. Design decision (summary)

Add a `prompt` (string) optional attr to **agentic** nodes. When present on an implementer-class agentic node, `prompt` becomes the node's brief: it REPLACES the bead-derived `Body` of the per-launch `agent-task.md` artifact. The bead `Title` + bead ID remain in the agent-task header for traceability. The Outcome contract is untouched.

## 2. Attr name + WG-002 row

Add to `specs/workflow-graph.md` §4 WG-002, the `agentic` row's **optional attrs** column:

| attr | type | meaning |
|---|---|---|
| `prompt` | string | the node's brief. When present, it is the agent-task Body for this node, overriding the bead-derived body. |

Amended `agentic` row (optional-attr column gains `prompt`):

    | `agentic` | LLM-driven | `agent_type`, `handler_ref` | `prompt`, `idempotency_class`, `axis_tags`, `skills_ref`, `freedom_profile_ref`, `budget_ref`, `hook_ref`, `guard_ref` | SUCCESS, FAIL, RETRY, PARTIAL_SUCCESS | `handler_outcome` | [handler-contract.md §4.5] |

## 3. WG-031 reserved-set addition

Add `prompt` to the WG-031 reserved set (`workflow-graph.md:388`). Parser gains a `case "prompt":` in `buildNode` (`parser.go:599-668`) setting a typed `Node.Prompt string` (add to `ast.go`), and rejecting `prompt` on edges/non-node positions as reserved-out-of-position. (Note: `prompt` on a non-agentic node — design leaves as a validation warning at v1; a tool node has no agent to read it. Lean: warn, not error, to keep the reserved-set rule simple — the value is just ignored.)

## 4. Override semantics (resolves the override-vs-coexist question)

`prompt`, when present, **REPLACES** the bead-derived `Body`. Rationale (F-B3): kilroy `box` nodes carry a complete self-contained prompt that IS the brief; silently prepending a possibly-large bead body would dilute the node author's instruction. The bead `Title` + ID stay in the agent-task header (the template renders `Title`/`BeadID` separately from `Body` — `AgentTaskPayload` fields), preserving the bead-tie for events and close.

Scope: **implementer-class agentic nodes only** at v1. A `prompt` on a reviewer-class node (`agent_type="reviewer"` / `handler_ref="claude-reviewer"`, per `nodeIsReviewer` at `dot_cascade.go:598-603`) does NOT override the review brief (which comes from `review-target.md` via `WriteReviewTarget`, `dot_cascade.go:364-381`). Document this as accepted-but-inert at v1; the live kilroy pipelines have no harmonik-reviewer-class nodes so this edge is unexercised (F-B3).

## 5. Code threading point

`internal/daemon/`:
- Add `nodePrompt string` to `claudeRunCtx` (`claudelaunchspec.go:48`, next to `beadTitle`/`beadDescription`).
- In `dispatchDotAgenticNode` (`dot_cascade.go:400-419`), set `rc.nodePrompt = node.Prompt` when building `rc` (implementer phase only; leave empty for reviewer phase).
- In `buildClaudeLaunchSpec` (`claudelaunchspec.go:246-249`), branch the `taskBody` derivation:

      taskBody := rc.beadDescription
      if rc.nodePrompt != "" { taskBody = rc.nodePrompt }   // NEW: prompt overrides bead body
      if taskBody == "" { taskBody = rc.beadTitle }
      if taskBody == "" { taskBody = rc.beadID }

Nothing below the `WriteAgentTask` call changes — claude reads `agent-task.md` exactly as today.

## 6. handler-contract anchor

`handler-contract.md` §4.2 / the HC-006a agent-task-content row (`handler-contract.md:156`) gains a clarifying note: the `agent-task.md` Body MAY be sourced from the node's `prompt` attr (overriding the bead body) for `dot`-mode agentic nodes. No change to LaunchSpec required fields (HC-006), the wire protocol, or the handler's read of `agent-task.md`. The Outcome contract (EM-005) is untouched — `prompt` affects input only.

## 7. Backwards compatibility

- **Additive.** New optional attr `prompt`; no existing attr changes.
- **Minor schema bump; N-1 readable (WG-034).** No new type/enum/edge-field. An agentic node without `prompt` behaves exactly as today (bead-derived brief).
- **Outcome envelope untouched.** `prompt` is input-only.
- **review-loop (v69) unaffected.** The threading is in the `dot`-mode dispatch path; review-loop's own brief construction is separate and untouched. Reviewer-class nodes ignore `prompt`.

## 8. Open items flagged for the integration pass

- I-B1 (R-B3): if component E (graph `goal` + `__PARAM__` substitution) lands, the substitution pass MUST run over the per-node `prompt` too (kilroy prompts embed `__SENTRY_SHORT_ID__` etc.). E's design owns the substitution surface; integration wires it to cover both `goal` and `prompt`.
- I-B2 (R-B1): reviewer-node `prompt` is accepted-but-inert at v1 — document in the canonical example + the shell/agentic authoring notes.
- I-B3 (R-B2): co-design or strictly sequence with component C (both touch `dispatchDotAgenticNode`; non-overlapping regions but one function).
