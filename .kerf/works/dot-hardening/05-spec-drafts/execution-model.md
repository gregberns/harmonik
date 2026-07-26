<!--
Spec-draft — Component B (execution-model.md), kerf work `dot-hardening`.
CLAUSE-SCOPED for finalization splicing. Each block below is the NORMATIVE
text to insert / replace at the anchored location in specs/execution-model.md
(1909 lines at HEAD). It is written to be lifted verbatim into the spec; the
"Anchor" line names where it lands, "Action" names NEW / AMEND / REPLACE /
REWRITE. Voice, clause-id style (EM-###), and MUST/SHOULD/MAY register match
the surrounding spec. The "## Changelog fragment (EM)" at the end is the §12
revision-history row for this change.
-->

# execution-model.md — dot-hardening spec-draft (Component B)

---

## §6.1 record amendments (grammar)

### AMEND — §6.1 `RECORD Workflow.goal` note (L1158)

**Action:** AMEND in place. The `goal` field stops being an unconditional ExtraContext broadcast and becomes a default-visible declared input the single renderer reads (tracks D-A3).

Replace the `goal` row's trailing note:

```
    goal               : String | None     -- optional graph-level objective per [workflow-graph.md §4 WG-044]; a DEFAULT-VISIBLE declared input surfaced into an agentic node's brief by the single node-brief renderer (§7.5.1 EM-069) only when that node's declared/default input set includes it (a node MAY exclude it per WG-044 as amended); constant across every node in the run; MAY contain template-param placeholders substituted at launch (§7.5, [workflow-graph.md §4 WG-045])
```

### AMEND — §6.1 `RECORD Node`: new `role` field (insert after `non_committing`, L1181)

**Action:** NEW field on the Node record — a typed promotion of the `node.Role` value threaded today only via ExtraContext (`dot_cascade.go:1305–1316`), absent from the L1162–1182 record.

```
    role                : Role | None     -- optional framing selector for the single node-brief renderer (§7.5.1 EM-069); promoted from the ExtraContext-threaded node.Role; when absent the renderer infers framing from agent_type exactly as today; see [workflow-graph.md §4 WG-002]
```

Add the accompanying enum after `ENUM NodeType` (L1190):

```
ENUM Role:
    implementer                 -- worktree-discipline framing; declares {task} (+{feedback} on resume)
    reviewer                    -- read-only + verdict-emission framing; declares {task_context, rubric}
```

> NOTE: `Role` is an **open set** (parallels `agent_type`, L1166): a value outside the enumerated members MUST NOT fail load. The renderer MUST fall back to `agent_type`-inferred framing for an unknown `role`, matching the [workflow-graph.md §4 WG-003] open-set posture. The two MVH members are the only framings the renderer specifies concretely (EM-069, EM-069-REV).

### AMEND — §6.1 `RECORD Run`: new `node_model_seal` field (insert after `template_params`, L1229)

**Action:** NEW nullable Run field backing EM-070. Additive; `None` for every existing run.

```
    node_model_seal    : Map<(node_id, iteration_count), {tool, model, effort}> | None
                          -- per-(node, iteration) concrete-model seal per §7.5.1.EM-070; each entry
                             is written at the FIRST dispatch of its (node_id, iteration_count) key,
                             after alias→concrete resolution for that node's effective tool; None until
                             the run's first agentic dispatch; each key is frozen once written and is the
                             authoritative source for any re-dispatch or replay of that key per EM-071
```

---

## §7.5.1 — new clauses (single renderer family + seal + replay)

> **ID allocation.** Dot-mode clauses run EM-055..EM-059 + EM-068; new dot-mode ids continue from **EM-069**. Allocated: **EM-069** (single renderer) with lettered sub-clauses **EM-069-REV**, **EM-069-SRC**, **EM-069-MAN**, **EM-069-FB**, **EM-069-ITER**; **EM-070** (per-(node,iteration) seal); **EM-071** (replay reads the seal). The ladder flip is an in-place rewrite of the existing **EM-012b-NODE** (no new id).

### NEW — EM-069 — Single node-brief renderer

**Anchor:** §7.5.1, inserted immediately after EM-055 (before §7.5.2). **Action:** NEW clause that **REPLACES** the *B↔E brief-composition contract* prose (L1584–1590) and the `buildAgentTaskContent`-naming sentence (L1590) in full — that block is struck and folded here.

> **REPLACES (before-text struck in full):** the paragraph beginning *"**B↔E brief-composition contract (normative).** Both the graph-level `goal` (component E, run-level) and the node-level `prompt` (component B) write the `agentic`-node brief…"* through the sentence *"…confirmed against `internal/workspace/agenttask_chb028.go` (`buildAgentTaskContent`: `Body` precedes `ExtraContext`)."* (L1584–1590). Its behavior-preserving content (goal and prompt compose, do not double-inject) is subsumed below.

#### EM-069 — Single node-brief renderer

Under `workflow_mode = dot`, ONE renderer MUST assemble the task brief for **every** agentic node, for **every** handler tool. For a node `N`, the brief is composed from exactly:

```
brief(N) = {N's declared inputs, resolved to their bound values}
         + {N.prompt, when present}
         + {framing selected by N.role}
         + {run goal, iff N's declared/default input set includes `goal`}
```

The renderer is the **sole producer** of the brief; it subsumes both `buildAgentTaskContent` and `buildReviewTargetContent`. `N.role` selects the **framing** (worktree discipline for the implementer; read-only plus verdict-emission instruction for the reviewer — EM-069-REV), NOT which builder runs. The set of declared inputs and the role-default input set are owned by [workflow-graph.md §4 WG-002/WG-057] (D-A2); the renderer **consumes** the resolved input list and does not define the graph syntax.

Because the brief carries **only** the node's declared inputs, a node that does not declare a given input cannot render it. In particular a node that does not declare the review rubric cannot emit it into its brief: the cross-role leak is **structurally unexpressible**, not merely discouraged (see EM-069-SRC for the value-source invariant that makes this load-bearing).

`goal` and `prompt` occupy distinct positions in the brief and MUST each be rendered at most once; a node carrying both `goal` (run-level, default-visible) and `prompt` (node-level) receives each exactly once. When `N.role` is absent the renderer MUST infer framing from `agent_type` exactly as today, and un-migrated `.dot` graphs — whose default input set (WG-002) reproduces the current `Body` + `ExtraContext` shape — MUST render byte-identically to the pre-change output.

**AMENDS EM-055 step 6 (L1581):** step 6's "surface `goal` into every agentic node's brief via the run-level ExtraContext channel" is superseded by the renderer: `goal` is surfaced by EM-069 **only when the node's declared/default input set includes it**, not unconditionally. The ExtraContext channel remains the transport for `goal` when it is included; the change is the visibility gate, not the transport.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

### NEW — EM-069-REV — Reviewer framing preserves task context

**Anchor:** sub-clause of EM-069, §7.5.1. **Action:** NEW.

#### EM-069-REV — Reviewer framing preserves task context

When `role = reviewer`, the renderer's default input set MUST include the bead **id, title, and body verbatim** — no truncation, reproducing the current `buildReviewTargetContent` behavior (`agenttask_chb028.go:610–613`) — plus the diff **base SHA and head SHA** (`:618–620`), alongside the rubric (EM-069-SRC). The verbatim body (not a bare reference) is normative: the reviewer needs the task the diff is meant to satisfy in order to judge the diff, and a bare reference regresses into under-contexting. The reviewer MUST NOT be reduced to rubric-only.

The git diff (base/head SHAs → the run's worktree) is the **code channel** and is NOT carried as an edge/variable payload. The reviewer's task-context input names the SHAs; the diff content reaches the reviewer through the worktree.

Tags: mechanism

### NEW — EM-069-SRC — Value-source of `task` and `rubric` (leak-unexpressibility)

**Anchor:** sub-clause of EM-069, §7.5.1. **Action:** NEW. **LOAD-BEARING (D-FIX-1):** the "leak is unexpressible" guarantee requires `task` and `rubric` to come from **distinct sources**, not two renders of one bead body.

#### EM-069-SRC — Value-source of `task` and `rubric`

The renderer subsumes and deletes `buildReviewTargetContent`; this clause specifies where each role's separately-sourced instructions originate so a cross-role leak has no source to bind to.

- **Reviewer `rubric` input source.** The `rubric` bound to a reviewer node is the concatenation of (1) the **reviewer node's `prompt`** — this is where today's hardcoded generic reviewer rubric (the coverage-check and spec-field-check strings in `buildReviewTargetContent`, `agenttask_chb028.go:580–608`) MOVES: on deletion of the builder it becomes authored graph text the renderer emits on the reviewer path, so it is not orphaned; PLUS (2) an OPTIONAL per-bead `rubric` field carried in the structured bead config. The rubric MUST NOT be sourced from the bead task/body.

- **Implementer `task` input source.** The `task` bound to an implementer node is the bead's task text — the bead body, or a structured `task:` field when the bead uses the structured config block. This source EXCLUDES the reviewer node's `prompt` AND the per-bead `rubric` field.

- **Invariant.** An implementer node declares `task` (and `feedback` on resume, EM-069-ITER) and MUST NOT declare `rubric`; the rubric sources bind ONLY to a reviewer node. The cross-role leak (a reviewer's rubric reaching an implementer's brief) is therefore **structurally unexpressible**: there is no declared input on the implementer that names it, and the two value-sources are disjoint. (This invariant is co-stated in [workflow-graph.md §4 WG-057].)

- **Boundary (stated honestly).** This guarantee does NOT prevent an author from embedding review criteria in the **task prose itself** — that is telling the implementer directly (putting the answer in the question), not a cross-role leak, and is out of scope. The precise guarantee is: **one role's separately-sourced instructions cannot reach another role.**

- **Back-compat.** Today's hardcoded reviewer rubric strings become the standard-bead reviewer node's `prompt`, so the reviewer brief is **preserved, not lost**. The reviewer default input set is `{task_context (bead id/title/body verbatim + diff base/head SHAs per EM-069-REV), rubric (= reviewer node prompt + optional bead rubric field)}`.

> NOTE (open, ISSUES #1): the structured bead config carrying the optional per-bead `rubric` field and the `task:` field is a fenced `harmonik` block in the bead body; its parse home and exact shape are owned by [workflow-graph.md] / [beads-integration.md] and await operator confirmation. EM consumes the resolved `rubric` and `task` values; it does not define the block.

Tags: mechanism

### NEW — EM-069-MAN — Typed input manifest per node dispatch

**Anchor:** sub-clause of EM-069, §7.5.1. **Action:** NEW (D-FIX-2). This is the leak oracle's surface — a production byproduct of the real routing path, NOT test-only.

#### EM-069-MAN — Typed input manifest per node dispatch

As a byproduct of assembling a brief, the daemon MUST emit a typed **input manifest** for every node dispatch:

```
InputManifest {
    node_id         : NodeID
    role            : Role | None
    iteration_count : Integer
    source_keys     : List<String>   -- the top-level declared-input names the brief was assembled from
}
```

`source_keys` MUST name each declared input the brief was assembled from, using **top-level declared-input names only**: the admitted names at MVH are `task`, `feedback`, `rubric`, `task_context`, and `goal`. `task_context` is **ONE key** — it MUST NOT be decomposed into sub-keys (bead id / title / body / SHAs are constituents of the single `task_context` input, not separate manifest keys).

The manifest is a real production emission of the renderer, not a test fixture: it MUST be emitted on the live dispatch path so it cannot rot silently. The [handler-contract.md] C3 leak oracle asserts on this manifest — specifically that `rubric` ∉ the `source_keys` of any dispatch whose `role = implementer` (EM-069-SRC's invariant, made checkable).

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

### NEW — EM-069-FB — Reviewer feedback as a produced value on the back-edge

**Anchor:** sub-clause of EM-069, §7.5.1. **Action:** NEW (the value channel proper; pairs with the EM-056 clause-4 replacement below).

#### EM-069-FB — Reviewer feedback as a produced value on the back-edge

In a `dot` run, reviewer→implementer feedback is a **produced value carried on the back-edge**, not a review-loop artifact. On a `REQUEST_CHANGES` back-edge the reviewer node's produced outputs — `verdict:enum(APPROVE,REQUEST_CHANGES,BLOCK)` and `notes:string` (typed per [workflow-graph.md §4 WG-014] / D-A4) — MUST bind the implementer node's declared `feedback` input, and the single renderer (EM-069) MUST render them into the implementer's resume brief. The rendered value is identical across the claude, pi, and codex handlers.

The **value is canonical**: the WHAT lives in the Run record and the produced outputs, read by the renderer. The on-disk `${workspace_path}/.harmonik/reviewer-feedback.iter-<N>.md` file, when written, is **retained ONLY as claude's transport detail** — claude receives its resume brief by paste-inject referencing a worktree file (not argv), while pi and codex carry the same rendered value in the positional seed argv (`agentseedprompt.go:44`). The file is NOT the feedback channel; it is one tool's delivery envelope for the channel, per [handler-contract.md] C1.

The EM-056 clause-4 prohibition and this channel MUST NOT both be live: a `dot` run producing the feedback value on the back-edge is exactly what EM-056 clause 4 (as replaced below) now sanctions, and re-instating the prohibition would re-create the code/spec contradiction of hazard H3.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent

### NEW — EM-069-ITER — Iteration-1 vs resume binding of `feedback`

**Anchor:** sub-clause of EM-069 / EM-069-FB, §7.5.1. **Action:** NEW. Defines the shared iteration counter **once**.

#### EM-069-ITER — Iteration-1 vs resume binding of `feedback`

"Iteration" throughout §7.5.1 is the review-loop `iteration_count` (§6.1 `Run.context`, L1225) — the SAME counter carried in the [handler-contract.md §4 HC-004] launch-idempotency tuple `(run_id, node_id, phase, iteration_count)` and the SAME counter the seal keys on (EM-070). It is defined here once and referenced elsewhere.

At `iteration_count = 1` the implementer's declared `feedback` input is **unbound**, and its section in the brief MUST be **omitted**. On a bounce-back (`iteration_count ≥ 2`) the `feedback` input is **bound** (per EM-069-FB) and MUST be **rendered**. This mirrors today's phase selection (`dot_cascade.go:1288–1303`: `iterationCount ≤ 1` → `ImplementerInitial`, else `ImplementerResume`) and makes an implicit code behavior normative. Once the cross-role leak is removed (EM-069-SRC), this binding is what makes a deterministic `REQUEST_CHANGES → resume → APPROVE` round-trip forceable.

Tags: mechanism

### NEW — EM-070 — Per-(node, iteration) concrete-selection seal

**Anchor:** §7.5.1, inserted after EM-069. **Action:** NEW (backs the §6.1 `Run.node_model_seal` field above).

#### EM-070 — Per-(node, iteration) concrete-selection seal

At the **first dispatch** of each `(node_id, iteration_count)`, the daemon MUST write the node's resolved concrete `{tool, model, effort}` into `Run.node_model_seal` (§6.1), keyed by the **composite** `(node_id, iteration_count)`.

The seal is written **after** two dispatch-time facts are known:

1. The node's **effective tool** is resolved by the existing handler-selection path ([handler-contract.md §4 HC-003]) at dispatch, BEFORE the seal write. The effective tool is a dispatch-time fact — `strong.claude` need not equal `strong.pi` in a mixed-harness run — so a claim-time seal of an opaque alias string (`modelpreference.go:220–223`) has **zero determinism value**; the seal MUST record the per-tool concrete, not the alias.
2. The alias→concrete **precedence-order** resolution (EM-012b / EM-012b-NODE) has completed for that effective tool. The resolved concrete for that tool is what the seal records.

The composite key is **load-bearing (hazard H1):** a `REQUEST_CHANGES` back-edge re-dispatches the SAME implementer node at `iteration_count = 2, 3, …` (`dot_cascade.go:1288–1303`). An iteration-blind `node_id`-only key would overwrite iteration 1's sealed concrete, and a subsequent replay of iteration 1 would then bind iteration 2's model. Each `(node_id, iteration_count)` key MUST be written exactly **once and frozen**; a re-dispatch of an already-sealed key MUST **read** the seal (EM-071), never re-write it.

The run-level `model_preference` (§6.1, L1219) is unchanged and remains the run-level default; `node_model_seal` is the per-dispatch record layered on top. The general deterministic-replay contract EM-032 (§4.7) is the umbrella; EM-070 is the model-concrete specialization it never covered.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

### NEW — EM-071 — Replay reads the seal

**Anchor:** §7.5.1, inserted after EM-070, cross-referencing EM-070, EM-055 resume semantics, and EM-012b-NODE. **Action:** NEW; pairs with the EM-055 resume-semantics amendment below.

#### EM-071 — Replay reads the seal

On any replay or resume of a `dot` run, the workflow graph reparses as today (EM-055 resume semantics, unchanged), BUT at the dispatch of any `(node_id, iteration_count)` whose key is present in `Run.node_model_seal`, the daemon MUST read the sealed concrete `{tool, model, effort}` and MUST NOT recompute model/effort from the live alias catalog, bead labels, or project config. The reparsed node attributes feed **first** resolution only; for an already-sealed key the seal wins.

Without this branch the determinism guarantee is prose only: the hot-reloadable alias catalog ([handler-contract.md] C2) opens a window the old immutable-graph-text model never had — a daemon restart mid-run plus a catalog edit would otherwise produce one logical run mixing two catalog versions across its nodes (hazard H2). The catalog is an input to **first** resolution only; after that the run is frozen per key. This is the operator's replay/rerun confirmation ("did it use what I configured?"). Keep-last-good on catalog parse failure is a [handler-contract.md] C2 detail; the seal-read is what isolates an already-sealed run from any reload.

A run with `node_model_seal = None` (every pre-change run, and any run before its first agentic dispatch) falls through to the existing re-resolution path unchanged; only sealed keys take the short-circuit.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

### AMEND — EM-055 "Resume semantics" (L1592)

**Action:** AMEND — add the seal-read branch; the mandatory reparse is preserved intact.

Append to the "Resume semantics" paragraph:

> **Seal-read branch (EM-071).** The graph still reparses as above, and the `workflow_id`/`workflow_version` identity check is unchanged. In addition, at dispatch of any `(node_id, iteration_count)` present in `Run.node_model_seal` (§6.1), the daemon MUST read the sealed concrete per §7.5.1.EM-071 and MUST NOT recompute model/effort from the live catalog, labels, or config. The reparsed node attributes are the input to *first* resolution only; for an already-sealed key the seal is authoritative.

---

## §7.5.2 — REPLACE EM-056 clause 4 (L1610)

**Action:** REPLACE clause 4 in full — the prohibition is struck, not augmented (hazard H3; the code already writes both files, `dot_cascade.go:875` `WriteReviewerFeedback` (`hk-wixms`) and `:1274` `WriteReviewTargetVia`).

> **REPLACES (before-text struck in full):**
> *"4. **Review-loop artifacts are not applicable.** The `${workspace_path}/.harmonik/reviewer-feedback.iter-<N>.md` artifact per §4.3.EM-015d (sub-clause EM-015d-RFD) and the `${workspace_path}/.harmonik/review-target.md` artifact per §4.3.EM-015d (sub-clause EM-015d-RIA) are `review-loop`-mode-only. A `dot` run MUST NOT produce them; their absence on a `dot` run is not an authoring error and MUST NOT be flagged by the §7.5.3 validator. (The EM-015d cross-ref placeholder is resolved by §7.5.)"*

Replacement text:

> 4. **Reviewer feedback is a produced value, not a review-loop artifact.** Reviewer→implementer feedback in a `dot` run is a **produced value on the back-edge** per §7.5.1.EM-069-FB: the reviewer node's `verdict` + `notes` outputs bind the implementer's `feedback` input and are rendered by the single renderer (EM-069) into the resume brief. The on-disk `${workspace_path}/.harmonik/reviewer-feedback.iter-<N>.md` file, when written, is **claude's transport detail** per §7.5.1.EM-069-FB and [handler-contract.md] C1 — not a review-loop artifact — and its presence on a `dot` run is therefore expected, not an authoring error. Likewise the reviewer's task context (bead id/title/body + diff SHAs) is delivered as the reviewer node's declared `task_context` input per EM-069-REV; any on-disk `review-target.md` written for a `dot` run is transport for that input, not the review-loop `EM-015d-RIA` artifact. **The prohibition and the value channel MUST NOT both be live:** this clause is a REPLACEMENT of the former prohibition, not an addition beside it. The §7.5.3 validator MUST NOT flag the presence OR absence of these files on a `dot` run.

---

## §4.3 EM-015d — AMEND the on-disk-artifact scoping (RFD L364–383, RIA L386–413)

**Action:** AMEND — add ONE scoping sentence to each sub-clause so the "review-loop-mode-only" framing does not forbid the `dot` value channel. No mechanism inside RFD/RIA changes.

### AMEND — EM-015d-RFD (append to the sub-clause, after L381)

> **Scope (dot carve-out).** The review-loop-mode framing of the `reviewer-feedback.iter-<N-1>.md` file governs the **review-loop driver** only. It does NOT forbid a `workflow_mode = dot` run from writing a `reviewer-feedback.iter-<N>.md` file **as claude transport** for the §7.5.1.EM-069-FB value channel; in `dot` mode the canonical feedback is the produced `verdict`+`notes` value, and the file (when present) is one handler's delivery envelope, not this sub-clause's review-loop artifact.

### AMEND — EM-015d-RIA (append to the sub-clause, after L409)

> **Scope (dot carve-out).** The review-loop-mode framing of `review-target.md` governs the **review-loop driver** only. It does NOT forbid a `workflow_mode = dot` run from writing a `review-target.md` file **as claude transport** for the reviewer node's §7.5.1.EM-069-REV `task_context` input; in `dot` mode the reviewer's context is a declared input rendered by the single renderer (EM-069), and the file (when present) is one handler's delivery envelope, not this sub-clause's review-loop artifact.

---

## §4.3 EM-012b-NODE — REWRITE the per-node tier + precedence block (L295, L297–306)

**Action:** REWRITE (ladder flip, D-B5; the one honestly-flagged non-additive change with B3). Uses **"precedence order"** as the term throughout (P1) — never the bare word "resolution".

> **REWRITES (before-text struck):** the EM-012b-NODE paragraph (L295) whose current text reads *"When present, the node's attribute value takes precedence over the run-level `ModelPreference` default … **for that node's dispatch only**"* and the informative precedence-summary block (L297–306) that lists `tier 0 per-node attr` as highest.

Replacement paragraph:

> **EM-012b-NODE — Per-node model/effort default (soft, overridable).** Under `workflow_mode = dot`, an `agentic` node MAY carry a `model` and/or `effort` attribute per [workflow-graph.md §4 WG-042]. The per-node attribute is a **soft default**: in the **precedence order** below it sits BELOW a per-run force (tier 0) and a per-bead escalation (tier 1) and ABOVE the project config, env var, compiled default, and built-in fallback (tiers 3, 3.5, 4, 5). A per-bead escalation therefore beats the workflow file's stage-typical `model=` default — task-specificity beats file-position — reversing the pre-`dot-hardening` ordering in which the node attribute won. A node MAY carry the `model_locked` marker per [workflow-graph.md §4 WG-042] (the author asserting "cheap even for hard beads"): a `model_locked` node's attribute is NOT overridden by a tier-1 per-bead escalation. `model` and `effort` are each resolved independently through the precedence order; the per-node value is static graph data read from the already-loaded, already-validated graph at dispatch and is NOT a second walk of tiers 1/3/3.5/4/5. The per-node value is opaque below the descriptor layer on the same terms as the run-level pair (shape-validated `model`, closed-enum `effort`; handler-side launch failure is authoritative). The concrete selected by this precedence order for the node's effective tool is what is sealed per §7.5.1.EM-070 and read back on replay per EM-071.

Replacement precedence-summary block:

```
Informative precedence order, highest first (dot mode):

tier 0    per-run force            --force-model / --force-effort (operator, one-shot)         [ISSUES #2 — force-vs-lock]
tier 1    per-bead escalation      model:<alias> / effort:<level> label (run-wide; @node scoping per D-B6)
tier 2    per-node attr            model="…" / effort="…" on the agentic node                  (soft default; UNLESS model_locked — see below)
tier 3    per-project config       .harmonik/config.yaml
tier 3.5  operator env var         HARMONIK_CLAUDE_MODEL / HARMONIK_CLAUDE_EFFORT
tier 4    per-agent-type compiled default
tier 5    built-in fallback (empty)

model_locked: a node carrying model_locked is NOT overridden by a tier-1 per-bead escalation
              (its attribute holds against the escalation band). Its interaction with a tier-0
              per-run force is deferred — see NOTE below.
```

> NOTE (open, ISSUES #2): whether a tier-0 per-run `--force-model` / `--force-effort` overrides a `model_locked` node is an operator decision. Recommendation on record: **force overrides** the lock (an explicit operator one-shot), while a tier-1 per-bead escalation **respects** the lock. Until confirmed, tier 0's interaction with `model_locked` is left as this pointer, not a decided clause.

> NOTE (open, ISSUES #1): the per-bead escalation surface and the `model_locked` marker are serialized in the structured bead config (recommended: a fenced `harmonik` YAML block in the bead body carrying per-node `{tool, model, effort, locked}`); its shape is owned by [workflow-graph.md] / [beads-integration.md] and awaits operator confirmation. EM owns only the tier order above.

> NOTE (open, ISSUES #3): the flip is **NOT behavior-preserving** for a deployed bead whose node `model=` was authored to win over a per-bead `model:` label. Blast radius is mitigated — a bare `model:<alias>` label STAYS run-wide (tier 1, per D-B6), so most deployed labels are unaffected, and beads are OFF this phase (blast radius likely nil). Confirm no deployed bead relies on a node `model=` beating a label. An explicit escalation that cannot resolve MUST fail loud, not silently degrade — that fail-loud lives handler-side ([handler-contract.md] C2); EM sets only the precedence order.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

---

## Changelog fragment (EM)

**Target file:** `specs/execution-model.md` — **status: modified.**

| Date | Version | Author | Summary |
|---|---|---|---|
| 2026-07-17 | 0.10.0 | agent (kerf work `dot-hardening` / Component B) | **Single node-brief renderer, leak-unexpressibility, reviewer-feedback value channel, and per-(node,iteration) model seal for `dot` mode.** New **EM-069** (§7.5.1) — one renderer assembles every agentic node's brief as `{declared inputs} + {prompt} + {role framing} + {goal, when declared}`, subsuming `buildAgentTaskContent` and `buildReviewTargetContent`; **REPLACES** the B↔E brief-composition contract (L1584–1590) and its `buildAgentTaskContent`-naming sentence; AMENDS EM-055 step 6 so `goal` is surfaced only when the node's input set includes it (no longer an unconditional ExtraContext broadcast). New sub-clauses: **EM-069-REV** (reviewer framing keeps bead id/title/body verbatim + diff base/head SHAs + rubric; not rubric-only), **EM-069-SRC** (load-bearing: reviewer `rubric` sources = reviewer node `prompt` [where the deleted builder's hardcoded rubric moves] + optional per-bead `rubric` field; implementer `task` source = bead body / structured `task:` field, excluding both; implementer never declares `rubric`, so the cross-role leak is structurally unexpressible; boundary stated: does not stop an author embedding criteria in task prose), **EM-069-MAN** (daemon MUST emit a typed input manifest `{node_id, role, iteration_count, source_keys[]}` per dispatch — top-level keys only, `task_context` is one key — as the C3 leak-oracle surface, a production byproduct not test-only), **EM-069-FB** (reviewer feedback is a produced `verdict`+`notes` value on the back-edge rendered into the resume brief; on-disk `reviewer-feedback.iter-<N>.md` retained ONLY as claude transport; the prohibition and the channel MUST NOT both be live), **EM-069-ITER** (defines the shared `iteration_count` once; `feedback` unbound → omitted at iteration 1, bound → rendered on bounce-back). New **EM-070** (§7.5.1 + §6.1 `Run.node_model_seal: Map<(node_id, iteration_count), {tool, model, effort}>`) — per-(node,iteration) concrete seal written at first dispatch after HC-003 effective-tool selection and alias→concrete resolution; composite key is load-bearing (back-edge re-dispatch would otherwise overwrite iteration 1). New **EM-071** (§7.5.1) + AMENDED **EM-055 "Resume semantics"** (L1592) — replay reparses the graph but MUST read the sealed concrete for an already-sealed `(node_id, iteration_count)` and MUST NOT recompute from the live catalog. **REPLACES EM-056 §7.5.2 clause 4** (L1610) — the "a `dot` run MUST NOT produce `reviewer-feedback`/`review-target`" prohibition is struck in full and replaced by the EM-069-FB value channel + the "prohibition and channel MUST NOT both be live" note (reconciles the shipped-code contradiction, `hk-wixms`). AMENDED **EM-015d-RFD** and **EM-015d-RIA** (§4.3, L364–413) — one scoping sentence each: their review-loop framing governs the review-loop driver only and does not forbid a `dot` run from writing those files as claude transport. **REWRITES EM-012b-NODE** (§4.3, L295) and the precedence-summary block (L297–306) — the per-node `model=`/`effort=` becomes a soft default (tier 2) BELOW per-run force (tier 0) and per-bead escalation (tier 1), with a `model_locked` opt-out that holds against the escalation band; uses "precedence order" as the pinned term; the not-behavior-preserving flip is flagged. §6.1: new `Node.role : Role | None` field + `ENUM Role {implementer, reviewer}` (open-set, unknown → agent_type-inferred framing), new `Run.node_model_seal` field. Operator-deferred items carried as inline `> NOTE (open, ISSUES #N)`: #1 (structured bead config shape for `rubric`/`task`/escalation/`model_locked`), #2 (force-vs-lock), #3 (ladder-flip migration). New requirement IDs: EM-069, EM-069-REV, EM-069-SRC, EM-069-MAN, EM-069-FB, EM-069-ITER, EM-070, EM-071. EM-056 clause 4 replaced; EM-012b-NODE + precedence block rewritten; EM-055 step 6 + resume semantics, §6.1 `Workflow.goal`, EM-015d-RFD, EM-015d-RIA amended. No prior requirement IDs renumbered or retired. Motivating design: `.kerf/works/dot-hardening/04-design/execution-model-design.md` (reqs B1–B7); decisions `DECISIONS.md` (D-B1..D-B9, D-FIX-1/2, P1–P4). |
