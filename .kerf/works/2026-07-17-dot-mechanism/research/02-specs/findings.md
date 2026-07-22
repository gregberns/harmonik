# DOT workflow mechanism — normative spec findings (02-specs)

Scope: what the NORMATIVE specs say TODAY about the `workflow_mode=dot` model — node/edge/param/model surface, per-node input, verdict routing — and, critically, what is UNSPECIFIED or explicitly DEFERRED for the three redesign axes: (a) per-node/per-bead MODEL selection, (b) TYPED params through the graph, (c) reviewer→implementer feedback routing.

Specs read: `workflow-graph.md` (WG), `execution-model.md` (EM), `handler-contract.md` (HC), `sub-workflow-dispatch.md` (SW), `control-points.md` (CP). All are `status: draft`. The DOT vocabulary spec `workflow-graph.md` is at v0.3.1.

---

## 1. The DOT node/edge model as it stands (workflow-graph.md)

**Node types — closed enum of four** (WG-001, WG-002): `{agentic, non-agentic, gate, sub-workflow}`. A `type` outside this set is an ingest-time reject; the run MUST NOT start. `control-point` is NOT a node type (collapsed; CP Kinds bind as `*_ref` attributes, only `gate` is node-shaped).

Per-type attribute contract (WG-002 table, normative):

| `type` | category | required attrs | key optional attrs | legal outcome statuses | Outcome `kind` |
|---|---|---|---|---|---|
| `agentic` | LLM-driven | `agent_type`, `handler_ref` | `prompt`, `non_committing`, `auto_status`, `model`, `effort`, `idempotency_class`, `axis_tags`, `skills_ref`, `freedom_profile_ref`, `budget_ref`, `hook_ref`, `guard_ref` | SUCCESS, FAIL, RETRY, PARTIAL_SUCCESS | `handler_outcome` |
| `non-agentic` | deterministic | `handler_ref` | `tool_command`, `timeout`, `idempotency_class`, `axis_tags`, … | SUCCESS, FAIL, RETRY, PARTIAL_SUCCESS | `handler_outcome` |
| `gate` | policy-decision | `gate_ref`, `handler_ref` (EM-007 amendment, WG-005) | `axis_tags`, `hook_ref`, `skills_ref` | SUCCESS, FAIL | `gate_decision` |
| `sub-workflow` | composition | `sub_workflow_ref`, `workflow_version` | `input_mapping`, `axis_tags`, `hook_ref`, `guard_ref` | (inherited from inner terminal node) | (inherited) |

- `agent_type` is an **open set** (WG-003); unknown values are NOT rejected at parse — they surface as `structural` failure at handler-resolution time (HC §4.3). The validation weight a closed enum would carry is instead borne by four-axis `axis_tags` consistency (EM-011, WG-030 SHOULD-check).
- `handler_ref` is REQUIRED on `agentic`, `non-agentic`, AND `gate` (post-EM-007-amendment, WG-005). `sub-workflow` carries NO `handler_ref` and dispatches no handler (SW-007 → `SubWorkflowRunner.Run`, not `Handler.Launch`).
- **Tool node**: a `non-agentic` node with `tool_command` + `handler_ref="shell"` runs `/bin/sh -c <cmd>` in the worktree; every non-zero exit → `deterministic` FAIL (WG-039, HC-063). Default `timeout=300`s.

**Edges** (WG-009): exactly EM-002's locked field set — `from_node`, `to_node`, optional `condition`, optional `preferred_label`, `weight`, `ordering_key`. No spec introduces new edge fields. **No `model`, `effort`, or param attribute is legal on an edge** (WG-042: those on an edge = reserved-attribute-out-of-position strict error).

**Terminal nodes** (WG-021/022): outcome is communicated by DISTINCT terminal node IDs, not a `terminal_kind` attribute. Two reserved IDs: `close` (normal) and `close-needs-attention` (operator attention). No outgoing edges allowed from a terminal node (WG-023).

**Schema/version** (WG-033/035): graph-level `schema_version` (currently `1`, graph-level ONLY) is distinct from workflow `version` (author intent). N-1 readability contract (WG-034).

**Canonical default graph** — `standard-bead.dot` (WG-047–WG-052, §17, normative): six nodes `start(noop) → implement(implementer) → commit_gate(shell tool) → review(reviewer) → {close | close-needs-attention}`, ten edges, with the **review-floor invariant WG-050**: `close` has EXACTLY ONE inbound edge — `review → close` on `outcome.preferred_label == 'APPROVE'`. Everything else routes to `close-needs-attention`. This is the tier-4 built-in default a `dot`-mode run resolves to (EM-012a; embedded at `internal/daemon/standard-bead.dot`).

---

## 2. Per-node INPUT rules (prompt / goal / params / bead body)

Four distinct input channels exist today, designed to occupy **distinct, non-double-injecting channels** (WG-040 composition note):

1. **`prompt`** (WG-040) — node-level `agentic`-only string. On an **implementer-class** node it REPLACES the bead-derived task body verbatim (bead Title + ID retained in the artifact header for traceability). On a **reviewer-class** node it is *accepted-but-inert at v1* — "the reviewer's brief is sourced from the review-target artifact per EM-015d-RIA and is NOT overridden by `prompt`. …Reviewer-class `prompt` override is reserved for a future schema version." `prompt` is input-only; it does not alter Outcome/cascade. On `non-agentic`/`gate` = validation warning, retained-and-ignored.

2. **`goal`** (WG-044) — **graph-level** free-form string, parsed into typed `Graph.Goal`. Surfaced to `agentic`-node briefs as the run-level objective via the run-level **ExtraContext** channel (CHB-028, EM §7.5). Composes with — does NOT replace — per-node `prompt` and bead body. MAY contain template-param placeholders.

3. **Template params** (WG-045/046) — `__[A-Z][A-Z0-9_]*__` placeholders substituted over the parsed graph at launch, exactly once, per-attribute, AFTER parse: `tool_command` values POSIX single-quote shell-quoted, **all other attributes verbatim**. Param map is sealed into the Run record for replay determinism. **Param values are UNTRUSTED** (arrive over queue-submit RPC from any local agent); validated at ingest (no NUL/newline/control chars, ≤8192 bytes, key `^[A-Z][A-Z0-9_]*$`). Residual unsubstituted token after the pass = launch-time error.

4. **`context_keys` / shared context** (WG-031a, HC-062) — graph-level comma-list declaring the context keys that MAY appear as `context.<key>` on edge-condition LHS. Handler `Outcome.context_updates` may write these keys (EM-041a, applied BEFORE the cascade). Unregistered-key writes are warn-and-dropped (HC-062).

Bead body is the default implementer brief when `prompt` is absent (WG-040).

### Input-primitives table — what each can and cannot carry

| primitive | scope | typed? | trust | carries | CANNOT carry / limits |
|---|---|---|---|---|---|
| `prompt` (WG-040) | node, `agentic` only | untyped string | trusted (author) | verbatim implementer task body (replaces bead body) | inert on reviewer-class at v1; ignored on non-agentic/gate; no structured fields |
| `goal` (WG-044) | graph-level | typed `Graph.Goal` (string) | trusted (author) | one run-wide objective string, threaded via ExtraContext to agentic briefs | not per-node; one per graph; string only |
| template params (WG-045) | `.dot` text → any attr | untyped string map | UNTRUSTED | launch-time values spliced into goal/prompt/tool_command/edge/UnknownAttrs | no structured/typed values; not edge-scoped (splice is textual, global param map); recursion not supported |
| `context_keys` + context (WG-031a, EM-041a, HC-062) | graph-level decl; per-run mutable map | **declared but NOT type-pinned (OQ-WG-002 OPEN)** | daemon-mediated | key→value pairs written by `Outcome.context_updates`; readable as `context.<key>` edge LHS | LHS refs NOT validated against declared list at v1; no per-key types; only `==`/`!=` string/int equality in conditions |
| `input_mapping` (WG-006) | `sub-workflow` node | claimed "typed key→key mapping" | — | *intended* to project parent context into inner run | **mechanism UNSPECIFIED — see gap G2b below** |
| bead body | run input | free text | — | default implementer brief when no `prompt` | overridden entirely by `prompt` |

---

## 3. MODEL / effort selection today (WG-042, EM-012b)

Well-specified as a **five/six-tier precedence chain**, resolved ONCE at claim time and sealed as `ModelPreference{model, effort}` into the Run record (EM-012b):

- tier 0 — **per-node** `model` / `effort` attribute on the `agentic` node (EM-012b-NODE, WG-042). Static graph data, applied at that node's launch only; NOT a second resolution walk; overrides the run-level default for that node's dispatch.
- tier 1 — per-bead label `model:<alias>` / `effort:<level>` (unknown label → treated absent + `bead_label_conflict`).
- tier 2 — `.harmonik/config.yaml` per-agent-type `{model, effort}`.
- tier 2.5 — env `HARMONIK_CLAUDE_MODEL` / `HARMONIK_CLAUDE_EFFORT` (hot-reload at claim).
- tier 3 — per-agent-type compiled default.
- tier 4 — built-in fallback (empty; handler applies its own).

Value rules: `model` is **opaque/shape-validated only** (`^[A-Za-z0-9._:/-]+$`, ≤128 chars, non-empty) — harmonik never verifies it names a real model; handler launch failure is the authoritative compatibility check. `effort` is a **closed enum** `{low, medium, high, xhigh, max}`; an out-of-enum node attribute is an ingest-time strict error (stricter than the tier-1 label path). `model` and `effort` resolve independently. Per-node override valid ONLY on `agentic` nodes; on any other position (non-agentic/gate/sub-workflow/edge/graph) it is a strict error.

`class` / `model_stylesheet` (CSS-style per-node model selection from upstream pipelines) are **INFORMATIVE-only, not interpreted** at v1 (WG-043); a loader accepts them permissively and the dispatcher MUST NOT route on them. Porting guidance: translate `.hard { llm_model: … }` + `class="hard"` into a direct `model="…"` attribute. Promoting `model_stylesheet` to a normative selector is called out as "a clean future amendment" — DEFERRED to hk-1xzg3.

---

## 4. Verdict routing — how edges are chosen (WG-010, WG-019, EM-041)

Edge selection = **five-step deterministic cascade** (WG-010 / EM-041): (1) evaluate `condition` on every outgoing edge; (2) restrict to edges whose `preferred_label` matches `outcome.preferred_label`; (3) highest `weight`; (4) lexicographically smallest `ordering_key`; (5) unconditional-edge fallback. The unconditional-fallback invariant (WG-011) is non-negotiable: a declared default route MUST be honored before `no_outgoing_edge_matches` (a `structural` failure, WG-012/EM-046a).

**Edge-condition dialect** (WG-013): restricted equality mini-language — `==`/`!=`, `&&` only. No `||`, no `!`, no parens, no `<`/`>`, no arithmetic. LHS whitelist (WG-014): `outcome.status`, `outcome.preferred_label`, `outcome.failure_class`, `outcome.kind`, `context.<key>`. RHS: single-quoted string, non-negative int, or closed-enum member. Deliberately narrower than CP §4.7 guard-predicate language (WG-016 — the two dialects are NOT interchangeable).

**Verdicts ride `outcome.preferred_label`** (WG-019): no first-class `verdict` field. Authors route on `outcome.preferred_label == 'APPROVE' | 'REQUEST_CHANGES' | 'BLOCK'`. Reviewer agent is the SOLE authority for APPROVE/BLOCK (EM-015d, EM-068).

**Failure-class routing** (WG-017/018): six-class closed enum `{transient, structural, deterministic, canceled, budget_exhausted, compilation_loop}`. `failure_class` is a top-level Outcome field; handler-side OPTIONAL on FAIL, daemon-side back-filled and MUST be present when `status==FAIL`. **No `retry_target` side-channel** (WG-020): retry/fallback are expressed purely as edges (`condition="outcome.status=='FAIL' && outcome.failure_class=='transient'"` → retry node). Per-edge traversal caps bound cycles (WG-028, EM-043).

**`auto_status`** (WG-053, EM-068): implementer-class-only **deny-side** deterministic daemon gate. It may derive `FAIL`+`failure_class` (from a build/vet probe C1, or a validated `.harmonik/auto_status.json` marker C2) with zero LLM calls; it NEVER derives APPROVE/BLOCK/verdict/RETRY/PARTIAL_SUCCESS and never auto-confirms SUCCESS. Derived FAIL routes through the same failure-class edges; it does NOT re-enter any review loop. Orthogonal to `non_committing` (WG-041, HEAD-advance relaxation).

---

## 5. Sub-workflow composition (sub-workflow-dispatch.md, EM-034 family)

In-place expansion within the parent run — no child RunID (SW-001, SW-INV-001). Node IDs namespaced `<parentNodeID>/<subNodeID>` left-to-right (SW-002, EM-034a). Terminal Outcome of the inner graph escapes VERBATIM to the parent cascade (SW-006, EM-036a, SW-INV-002 — byte-equal, no rewrite/aggregation). Three-tier resolution (SW-004): explicit `sub_workflow_ref` → `<projectDir>/workflow.dot` → fail-closed structural. Expanded children write into the PARENT's shared context under the parent's `context_keys` discipline (SW-008 — no sub-scoped key list). Acyclicity enforced statically (WG-029/EM-034b) and re-checked at expansion (SW-003). Sub-workflow nodes valid only under `dot`/`single`, never `review-loop`; a `dot` graph MUST NOT reference a `review-loop` sub-workflow (SW-009/SW-010).

---

## 6. Reviewer → implementer feedback routing — the sharp finding

There are **two entirely separate machines**, and the rich feedback delivery exists in only ONE of them:

- **`review-loop` MODE** (EM-015d, hardcoded two-node `implementer→reviewer` cycle, NOT a DOT graph). Here the daemon delivers reviewer feedback CONTENT to the implementer on re-dispatch:
  - **EM-015d-RFD** — before each `implementer-resume` (iteration N≥2) the daemon WRITES `${workspace}/.harmonik/reviewer-feedback.iter-<N-1>.md` (prior verdict + flags + notes) and delivers a "read this file" instruction to the live implementer session via the AIS structured input port.
  - **EM-015d-RIA** — before each reviewer launch the daemon writes `${workspace}/.harmonik/review-target.md` (the reviewer's SOLE structured context source). Reviewer writes `.harmonik/review.json` (agent-reviewer schema v1), archived to `review.iter-<N>.json`; `Run.context.last_verdict` updated.
  - Cap-3 iterations (EM-015e), no-progress hash early-exit, `review_loop_cycle_complete` with `completion_reason`.

- **`dot` MODE.** The `standard-bead.dot` review-floor routes `review → implement` on `outcome.preferred_label == 'REQUEST_CHANGES'` (edge 8, cap 3). BUT: **EM §7.5 (line ~1610) states the `reviewer-feedback.iter-<N>.md` and `review-target.md` artifacts are "`review-loop`-mode-ONLY. A `dot` run MUST NOT produce them."** So in DOT mode, when a REQUEST_CHANGES edge loops back to the implementer node, **NO spec defines how the reviewer's feedback content reaches the re-dispatched implementer**. The cascade routes the edge; the implementer node is (re)dispatched with its bead body or static `prompt` and the graph-level `goal` — none of which carries the just-produced reviewer verdict/flags/notes. The reviewer's `preferred_label` drives ROUTING only; its CONTENT is not threaded forward in DOT mode. This is the single largest normative gap for the redesign.

---

## 7. Spec gaps / deferrals for the redesign

**(a) Per-node / per-bead MODEL selection**
- Model selection itself is WELL-specified for `agentic` nodes (WG-042 / EM-012b-NODE, tier-0 static attr over a 6-tier chain). Not a gap in mechanism.
- GAP: model is **opaque, never validated** against a real catalog (by design) — the redesign cannot rely on the spec to catch a bad alias; only handler launch failure does.
- GAP: `class` / `model_stylesheet` multi-tier / selector-based model selection is explicitly **NOT interpreted at v1** (WG-043, deferred hk-1xzg3). If the redesign wants >2 model tiers or selector indirection, that is greenfield.
- No spec ties model choice to node ROLE beyond the per-node attribute (e.g. "all reviewer nodes use model X" must be written per-node; no role-default model surface in the DOT layer — freedom-profile `model_tier` exists in CP but is a *ceiling*/tightest-wins intersection, CP §4.6/311, not a selector).

**(b) TYPED params flowing through the graph (global vs edge-scoped)**
- Params today are **untyped strings** only. `goal` = one graph-level string; template params = untyped launch-time text splice; context = key→value with **type-pinning explicitly OPEN (OQ-WG-002 / D8)** — a loader MUST NOT validate `context.<key>` LHS against the declared `context_keys`.
- GAP (G2a): there is **no typed-parameter surface**. No per-key types, no schema for context values, no typed `goal`/`prompt` structure.
- GAP (G2b): **`input_mapping` on sub-workflow nodes is declared but its mechanism is UNSPECIFIED.** WG-006 calls it "a typed key→key mapping that projects parent-run context into the inner run's context per EM-034a" — but EM-034a is solely node-ID namespacing; it says NOTHING about context projection/mapping. SW-008 states expanded children just share the parent context under the parent `context_keys` list. So `input_mapping` is a named-but-hollow attribute: reserved in the WG-031 set, accepted, with no normative projection semantics anywhere.
- GAP: **params are global, never edge-scoped.** No spec surface lets an edge carry or transform a param; `model`/`effort`/params on an edge = strict error (WG-042, WG-009). All parameter flow is graph-level (goal, param map) or run-shared (context). Edge-scoped typed data flow is greenfield.

**(c) Reviewer → implementer feedback routing**
- GAP (the big one, §6 above): **DOT mode has no normative feedback-delivery mechanism.** The `reviewer-feedback.iter-N.md` / `review-target.md` machinery is review-loop-mode-only and DOT runs MUST NOT produce it (EM §7.5 ~L1610). A `review → implement` REQUEST_CHANGES loop in a DOT graph re-dispatches the implementer with no thread of the reviewer's verdict/flags/notes.
- Related: reviewer-class `prompt` is inert at v1 (WG-040) — you cannot even statically inject a reviewer brief through the node; the reviewer brief comes only from the (review-loop-only) `review-target.md`. In DOT mode there is no spec'd reviewer-input artifact at all.
- Related deferral (EM-068 D4): `auto_status` REQUEST_CHANGES-via-policy + reviewer-loop re-entry is explicitly DEFERRED at v1.

**Cross-cutting open questions relevant to the redesign** (WG §13): OQ-WG-002 (context-key type-pinning — OPEN), OQ-WG-003 (`gate_decision` payload shape — pending), OQ-WG-006 (Gate policy schema-drift handling), OQ-WG-008 (parallel fan-out primitives — deferred, absent from closed enum), and the forward-referenced-but-partly-hollow `input_mapping` contract.

---

### Citations index
WG-001/002/003/005/006 (node model), WG-009/010/011/012 (edges+cascade), WG-013/014/015/016 (dialect), WG-017/018/019/020 (failure-class + verdict routing), WG-039/040/041/042/043/044/045/046/053/054 (input & model attrs), WG-047–WG-052 (standard-bead default + review-floor), WG-031/031a/032 (unknown-attr + context_keys), OQ-WG-002/003/006/008. EM-005/005a (Outcome), EM-012a/012b/012b-NODE (mode + model resolution), EM-015d(+RFD/RIA)/015e (review-loop feedback machinery), EM-034/034a/034c/036a (sub-workflow), EM-041/041a/046a/046b (cascade/context/retry), EM-068 (auto_status deny-gate), EM §7.5 ~L1610 (dot MUST NOT produce review-loop artifacts), §8 (failure taxonomy). HC-003/006/063 (handler selection, shell handler), HC-062 (context_keys discipline). SW-001..SW-010 (sub-workflow dispatch). CP-036/055/057 (`*_ref` typed family, skills_ref), CP §4.6 freedom-profile `model_tier` (ceiling, not selector).
