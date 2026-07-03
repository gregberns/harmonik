# Integration Review — `attractor-parity` (Pass 6)

> Reconciles the five component spec-drafts in `05-spec-drafts/` (A tool-node, B inline-prompt, C non-committing, D per-node-model, E goal-template-params) into a single conflict-free unified draft (`SPEC.md`). Verified against the live system specs `workflow-graph.md`, `execution-model.md`, `handler-contract.md`, `event-model.md`.
>
> **Live-spec baselines confirmed (2026-05-28):**
> - `workflow-graph.md` max requirement ID = **WG-038** → next free sequential block is **WG-039 … WG-046**.
> - `handler-contract.md` max = **HC-062** → **HC-063** is free (no collision; foundation set claimed it correctly).
> - `execution-model.md` keystone code path confirmed at `internal/daemon/dot_cascade.go:198-203` (synthesizes `Outcome{Status: SUCCESS}` for a `non-agentic` node, treating it as a noop start/terminal).
> - `agent-task.md` assembly confirmed in `internal/workspace/agenttask_chb028.go:271-331`: `## Task Description` (`Body`) then `## Extra Context` (`ExtraContext`) — two distinct sections / two distinct payload fields.

---

## 1. WG requirement-ID collision — RESOLVED (blocker §1)

### 1.1 The collision

Both agent-pairs independently allocated `WG-039 / WG-040 / WG-041`:

| Provisional ID | Foundation set (A/B/C) | Independent set (D/E) |
|---|---|---|
| `WG-039` | A — tool-command attr semantics | D — per-node `model`/`effort`; **and** E — graph-level `goal` |
| `WG-040` | B — inline `prompt` attr | E — template-param substitution |
| `WG-041` | C — `non_committing` attr | E — substitution ordering invariant |

(D additionally introduced a second requirement it labelled "WG-002 (per-node model/effort)" — an informal sub-ID — plus an INFORMATIVE "WG-002a". E introduced three WG requirements + reused the `WG-031` amendment slot.)

Eight **distinct new normative/informative requirements** are needed. Allocating the next free sequential block **WG-039…WG-046** after the live max (WG-038), grouped by component for readability (foundation A/B/C, then D, then E):

### 1.2 Final OLD→NEW reassignment map (authoritative)

| Component | Draft's provisional label | **FINAL ID** | Requirement title |
|---|---|---|---|
| A tool-node | `WG-039` | **WG-039** | Tool-command attributes (`tool_command` / `timeout`) on `non-agentic` nodes |
| B inline-prompt | `WG-040` | **WG-040** | Inline `prompt` attribute on `agentic` nodes |
| C non-committing | `WG-041` | **WG-041** | `non_committing` attribute on `agentic` nodes |
| D per-node-model | `WG-039` / "WG-002 (per-node model/effort)" | **WG-042** | Per-node `model` / `effort` attributes on `agentic` nodes |
| D per-node-model | `WG-002a` (informative) | **WG-043** | `class` / `model_stylesheet` authoring convention (INFORMATIVE) |
| E goal-template | `WG-039` | **WG-044** | Graph-level `goal` attribute |
| E goal-template | `WG-040` | **WG-045** | Template-param substitution over `.dot` source text |
| E goal-template | `WG-041` | **WG-046** | Substitution ordering invariant |

No `HC-063` collision: HC-062 was the live max; HC-063 (A's built-in `shell` handler) stands as drafted.

### 1.3 Cross-references that change as a consequence

Every `WG-039/040/041` reference in the drafts and changelog is rewritten per the map above. The full set fixed in `SPEC.md`:

- **A tool-node draft:** `WG-039` → **WG-039** (unchanged value, but now unambiguous): in A2 (the requirement itself), A3 (WG-031 reserved-set note), A4 (WG-024 validation), A5/HC-063 body (two refs), A8/EM-058 row + sub-note, A9/EM-057 item 7.
- **C non-committing draft:** the "validating `non-agentic` tool node (per WG-039)" authoring-rule reference in C2's authoring rule → **WG-039** (the tool-node requirement keeps WG-039, so C's reference is already correct after the map).
- **B inline-prompt draft:** B2 self-ref `WG-040` → **WG-040** (unchanged value); the HC-006a citation of B's requirement → **WG-040**.
- **C non-committing draft:** C2 self-ref `WG-041` → **WG-041** (unchanged value); C4/EM-015d and C5/EM-058 sub-note citations of C's requirement → **WG-041**.
- **D per-node-model draft:** the WG-002-table note, the requirement body (renamed from informal "WG-002 (per-node model/effort)") → **WG-042**; the informative requirement (was `WG-002a`) → **WG-043**; EM-012b-NODE's `[workflow-graph.md §4 WG-002]` cite → **§4 WG-042**; §6.1 Node-RECORD field comments → **WG-042**; D's WG-043 self-reference inside the WG-042 table note → **WG-043**.
- **E goal-template draft:** A.1 `WG-039` (goal) → **WG-044**; A.2 `WG-040` (substitution) → **WG-045**; A.3 `WG-041` (ordering) → **WG-046**; every internal cross-ref inside E (`goal` body cites WG-045 for substitution, WG-046 references WG-044/045, the §6.1 Workflow/Run RECORD comments, the §7.5 launch-surface steps) rewritten to **WG-044/045/046**; E's reference to "node `prompt` (component B)" → **WG-040**; E's reference to "node `tool_command` (component A)" → **WG-039**.

**Note — accidentally-correct values:** Because the foundation set (A/B/C) keeps WG-039/040/041 and only the independent set (D/E) shifts up to WG-042…WG-046, every A/B/C self-reference retains its literal value; only the *meaning* is now pinned. All D/E references shift. This was the lowest-churn assignment consistent with "next free sequential after WG-038."

---

## 2. Merged shared rows — final text (blocker §2)

### 2.1 WG-002 `agentic` row (merge of B `prompt` + C `non_committing` + D `model`/`effort`)

The live row's optional-attrs column is `idempotency_class, axis_tags, skills_ref, freedom_profile_ref, budget_ref, hook_ref, guard_ref`. Merged additions: `prompt` (B), `non_committing` (C), `model`, `effort` (D). Final row (ordering: new attrs first in B,C,D draft order, then the existing attrs unchanged):

```
| `agentic` | LLM-driven | `agent_type`, `handler_ref` | `prompt`, `non_committing`, `model`, `effort`, `idempotency_class`, `axis_tags`, `skills_ref`, `freedom_profile_ref`, `budget_ref`, `hook_ref`, `guard_ref` | SUCCESS, FAIL, RETRY, PARTIAL_SUCCESS | `handler_outcome` | [handler-contract.md §4.5] |
```

A WG-002 Notes-list bullet is added (D's note, ID-corrected):

> - `prompt` (§4 WG-040), `non_committing` (§4 WG-041), `model`, and `effort` (§4 WG-042) are valid ONLY on `agentic` nodes. `model`/`effort` are the highest-precedence (tier-0) input to the model/effort resolution chain of [execution-model.md §4.3 EM-012b]; when present they override the run-level default for the node that carries them. `class` and `model_stylesheet` are INFORMATIVE-only at v1.0 (see §4 WG-043); a loader MUST accept them permissively per §10 WG-031/WG-032 and MUST NOT dispatch on them.

### 2.2 WG-002 `non-agentic` row (A `tool_command`/`timeout`)

Live optional-attrs column is `idempotency_class, axis_tags, budget_ref, hook_ref, guard_ref`. A adds `tool_command`, `timeout`. Final row:

```
| `non-agentic` | deterministic | `handler_ref` | `tool_command`, `timeout`, `idempotency_class`, `axis_tags`, `budget_ref`, `hook_ref`, `guard_ref` | SUCCESS, FAIL, RETRY, PARTIAL_SUCCESS | `handler_outcome` | [handler-contract.md §4.5] |
```

`required attrs` stays `handler_ref` (a tool node carries `handler_ref="shell"` per WG-039 — see §3 below). `gate` and `sub-workflow` rows unchanged.

### 2.3 WG-031 reserved-set sentence (union of A `tool_command`,`timeout` + B `prompt` + C `non_committing` + D `model`,`effort` + E `goal`)

ONE merged sentence. New names inserted at the natural node-attr / graph-level positions; `class` and `model_stylesheet` are NOT added (D's WG-043 keeps them permissive/informative). Final:

> The reserved set at v1.0 is: `type`, `agent_type`, `handler_ref`, `gate_ref`, `sub_workflow_ref`, `workflow_version`, `input_mapping`, `idempotency_class`, `axis_tags`, `tool_command`, `timeout`, `prompt`, `non_committing`, `model`, `effort`, `policy_ref` (reserved-and-rejected name; see [control-points.md §4.12 CP-056]), `hook_ref`, `guard_ref`, `budget_ref`, `skills_ref`, `freedom_profile_ref`, `schema_version`, `version`, `condition`, `preferred_label`, `weight`, `ordering_key`, `start_node`, `terminal_node_ids`, `context_keys` (graph-level per [handler-contract.md §5.6 HC-062]; see WG-031a), `goal` (graph-level per §4 WG-044).

`goal` is annotated graph-level (like `context_keys`): a `goal` on a node/edge is reserved-out-of-position. `tool_command`/`timeout` are node-level (`non-agentic`); `prompt`/`non_committing`/`model`/`effort` are node-level (`agentic`); a name used outside its declared position is the WG-031 strict error. The WG-045 template-param surface is a load-time text transform, NOT an attribute, so it adds no reserved name (its token grammar `__[A-Z][A-Z0-9_]*__` is defined normatively inside WG-045).

---

## 3. EM-058 `non-agentic` dispatch row + EM-057 (item §3)

The live EM-058 `non-agentic` row says "Invoke the handler referenced by `handler_ref` … Same dispatch path as `agentic`." The live code (`dot_cascade.go:198-203`) instead synthesizes SUCCESS in-process. A's amendment reconciles the two by splitting the row on `tool_command` presence — verified coherent. Final `non-agentic` row text (ID-corrected to WG-039 / HC-063):

```
| `non-agentic` | When the node carries `tool_command` and `handler_ref="shell"`: the built-in `shell` handler of [handler-contract.md §4.1 HC-063] executes the command in the run's worktree and applies the exit-state → Outcome mapping of HC-063 / [workflow-graph.md §4 WG-039]. The `shell` handler MAY run in-process (no subprocess, no socket) per the HC-063 exception. When the node carries no `tool_command` (start / terminal / pass-through node), the engine synthesizes a `SUCCESS` Outcome without dispatching a handler (the `internal/daemon/dot_cascade.go` in-process path). Otherwise (a non-agentic node bound to a non-`shell` handler), invoke the handler referenced by the node's `handler_ref` per [handler-contract.md §4.1]; handler-internal determinism is the handler's responsibility per the node's four-axis tags (§4.2.EM-011). | §4.1.EM-005 `Outcome` with `kind = default` per §4.1.EM-005a. |
```

Plus A's EM-058 "Non-agentic dispatch sub-note" (three paths) appended below the table, and A's C5/non_committing sub-note (see §4). EM-057 item 7's `non-agentic` sub-bullet gains the `handler_ref="shell"` clause (mandatory-`handler_ref` invariant preserved — a tool node still carries `handler_ref`, pinned to `shell`).

**Reconciliation verdict:** coherent. The split row is a strict superset of current behavior: path (2) is exactly today's code; paths (1)/(3) are new. No existing `dot` test regresses (the noop start/terminal nodes used by the live smoke graphs all take path (2)).

---

## 4. EM-015d reads as SCOPING, not relaxation (§4 — VERIFY, the v69 guard)

C4's edit adds a bullet to EM-015d's "The cycle MUST observe:" list. Verified: the clause states the implementer-MUST-advance-HEAD obligation is **`review-loop`-scoped** and "does NOT apply to `workflow_mode = dot`," with the `dot`-mode relaxation gated on the per-node `non_committing` attribute (WG-041). This is a scoping clarification of where the obligation already applied (EM-015d is the `review-loop` lifecycle section), NOT a blanket relaxation:

- The carve-out names `dot` mode only; `review-loop` behavior is bit-for-bit unchanged.
- The relaxation is per-node-attribute-gated (`non_committing="true"`), default-off.
- C's own draft and changelog flag this VERIFY explicitly; the final text in `SPEC.md` keeps the "review-loop-scoped" framing and the "never reaches the `review-loop` path" sentence.

**No v69 review-loop regression implied.** The implementer-MUST-commit invariant that v69 proved live is review-loop-only and stays mandatory there. (ID-correction: C4/C5 cite WG-041 for `non_committing`.)

C5's EM-058 "Non-committing `agentic` dispatch sub-note" is retained alongside A's non-agentic sub-note, and adds the important cross-mode guard: **an unresolvable worktree HEAD is a daemon-side error in BOTH modes** — `non_committing` relaxes only the *clean-exit-without-advance* check, not broken-worktree detection.

---

## 5. B↔E brief composition — distinct channels confirmed (§5)

Verified against `internal/workspace/agenttask_chb028.go:271-331`:

- `goal` (E, run-level) threads through the **run-level ExtraContext channel** → rendered as the `## Extra Context` section (`AgentTaskPayload.ExtraContext`). Constant across every node in the run.
- `prompt` (B, node-level) **replaces** `AgentTaskPayload.Body` → rendered as the `## Task Description` section. When absent, `Body` is the bead-derived body.

These are two distinct struct fields rendered into two distinct Markdown sections. **No double-inject.** A node carrying both `goal` (run-level) and `prompt` (node-level) receives each exactly once.

**Assembly order (normative, as stated in `SPEC.md`):** header (`bead_id`, `title`, …) → Worktree Discipline → Bead Lifecycle → **`## Task Description` (`prompt` if present, else bead body)** → **`## Extra Context` (`goal`, if present)** → phase-specific sections. The `goal` (objective) follows the node body (concrete task) — objective-after-task is the existing ExtraContext placement; the drafts do not require reordering.

E's WG-044 text is ID-corrected to cite `prompt` as **§4 WG-040** and `tool_command` as **§4 WG-039**.

---

## 6. Tool-node observability — DECISION (§6)

**Decision: REUSE existing node-lifecycle events; do NOT add a `tool_command_completed` event at v1.**

Rationale:
- A tool node is a `non-agentic` node dispatched through the established `dot` cascade. The live code already emits **`node_dispatch_requested`** (event-model §8.1.11, class O) before every node — including non-agentic — at `dot_cascade.go:191-193`. A tool node inherits this for free.
- The command's Outcome (SUCCESS/FAIL + back-filled `failure_class`) flows through the existing `Outcome` surface and the run-terminal events (`run_completed`/`run_failed`) and the `reviewer_verdict`/cascade-routing events already specified. No information about the command's result is lost.
- event-model.md §8 §367 is explicit: **"Agent-internal detail — tool calls … MUST NOT be emitted as events; it lives in the agent's session log."** A `tool_command_completed` event would be a per-command lifecycle event of exactly the kind the model deliberately excludes, and would set a precedent for per-handler-type bespoke events that the §8.9 orphan-lint discourages.
- The HC-063 in-process `shell` handler has **no NDJSON progress stream** (no `agent_output_chunk`), by design — so there is no chunk-class observability to reuse or replace; the node-boundary events are the correct granularity.

**Consequence for `SPEC.md`:** the A-draft's flagged item I-A-events is resolved as "reuse `node_dispatch_requested` + the Outcome/terminal-event surface; no new event type." No amendment to `event-model.md` is required by this work. A future, separately-justified observability bead MAY add a tool-command event if operator demand surfaces — recorded as a tasks-pass follow-up, not a v1 obligation.

---

## 7. Cross-reference checks performed

- **WG IDs:** confirmed live max WG-038; verified WG-039…WG-046 are all unused in `workflow-graph.md` before assignment. ✔
- **HC-063:** confirmed HC-062 is live max; HC-063 free. ✔
- **EM IDs:** EM-012b, EM-015d, EM-057, EM-058, §6.1 records all located and quoted; the drafts' EM-012b-NODE / EM-058 sub-notes are additive sub-clauses (no EM ID renumber). ✔
- **`[handler-contract.md §4.1]` / `§4.2` / `§4.2a HC-058` / `§4.2 HC-006a` / `§4.5`** — all resolve in live `handler-contract.md`. ✔
- **`[workspace-model.md §4.1]`** (worktree / `workspace_path`) — cited by HC-063; resolves. ✔
- **`[execution-model.md §4.1 EM-005 / EM-005a / EM-005c]`** (Outcome, kind, failure_class additive) — resolve. ✔
- **`[execution-model.md §4.10 EM-041]`** (cascade) — resolve. ✔
- **`[control-points.md §4.12 CP-056]`** (policy_ref reserved-rejected) — carried verbatim from live WG-031. ✔
- **Code anchors:** `internal/daemon/dot_cascade.go:198-203` (non-agentic synth), `internal/workspace/agenttask_chb028.go` (Body + ExtraContext) — both verified to exist and match the drafts' claims. ✔

## 8. Contradictions found & resolved

1. **WG-039/040/041 triple collision** — resolved per §1 (the blocker).
2. **EM-058 row vs live code** — resolved per §3 (the keystone split-on-`tool_command`).
3. **EM-015d apparent contradiction with `non_committing`** — resolved per §4 (scoping, not relaxation; confirmed no v69 regression).
4. **D's informal "WG-002 (per-node model/effort)" / "WG-002a" sub-IDs** — these are not real sequential WG IDs; mapped to **WG-042** / **WG-043** to fit the sequential scheme. The WG-002 *table* (the catalog) is amended in place per §2; D's new *requirement* is WG-042.
5. **E's reuse of the `WG-031` amendment slot** — not a collision (WG-031 is the reserved-set requirement, amended additively by A/B/C/D/E in one merged sentence per §2.3). ✔

## 9. Consistency / terminology

- "tool node" = a `non-agentic` node carrying `tool_command` + `handler_ref="shell"`. Used uniformly across A, C (pairing rule), E. ✔
- "implementer-class" / "reviewer-class" `agentic` node — used consistently by B (prompt scope) and C (non_committing scope). ✔
- "run-level ExtraContext channel" (E) vs "node-level taskBody" (B) — distinct, consistent. ✔
- `effort` closed enum `{low, medium, high, xhigh, max}` — D matches the live EffortLevel ENUM and EM-012b tier-1 enum. ✔
- Trust-boundary language ("trusted `.dot` author", "operator-supplied/trusted params, un-sanitized") — consistent between A (tool_command) and E (params). ✔

## 10. Changelog verification

`05-changelog.md` is updated (see the file) to the reconciled IDs: the D/E section's WG-039/040/041 → WG-042…WG-046, with the WG-002a → WG-043 rename; the foundation section's collision-BLOCKER note is updated to a RESOLVED note pointing at the §1.2 map; the observability line is updated to the §6 decision. The four/seven target-file rows otherwise match the drafted edits.

## 11. Final assessment

The five components are **mutually coherent after ID reassignment**. All changes are additive and N-1-readable (`schema_version` stays `1`): two new node-attr families on `agentic` (`prompt`, `non_committing`, `model`, `effort`), one new node-attr family on `non-agentic` (`tool_command`, `timeout`), one new graph-level attr (`goal`), one pre-parse text transform (template params), one new built-in handler (HC-063 `shell`), and the keystone EM-058 reconciliation that aligns the spec with the already-shipped `dot_cascade.go` in-process path. No node type, edge field, or enum member is added; the Outcome envelope and edge cascade are untouched; the v69 review-loop path does not regress. The unified `SPEC.md` is ready for the tasks pass.

## 12. Left for the tasks pass

- **Authoring-notes / example sidecar** (`specs/examples/`): document the `auto_status → non_committing` porting alias and the "harmonik does NOT accept `auto_status` at v1" rule (C); the "pair every `non_committing` node with a downstream validating tool node" authoring obligation (C); reviewer-node `prompt` accepted-but-inert (B); the `model_stylesheet`/`class` → direct `model=` port note (D). These are NOT new normative requirements — they are authoring guidance.
- **Implementation sequencing (non-spec):** B (pre-launch task-brief build) and C (post-launch outcome derivation) touch non-overlapping regions of `dispatchDotAgenticNode`; sequence B before C or land together.
- **v2 follow-up beads (file, do not implement here):** author-declared `transient_exit_codes` per tool node (A's I-A-v2); the proper `auto_status` work-product/marker status-derivation feature (C's I-C-deferred); a normative `model_stylesheet` selector mechanism for >2 model tiers (D); optional `tool_command_completed` observability event if operator demand surfaces (§6).
- **Substitution × per-node `prompt`/`tool_command` ordering test (E/B/A):** the WG-046 ordering invariant means WG-045 substitution runs over raw source before parse, so `__PARAM__` tokens inside `prompt`/`tool_command`/`goal` are all covered by the single pass — the tasks pass should add a conformance test exercising a `__PARAM__` token inside each of the three attribute kinds.
