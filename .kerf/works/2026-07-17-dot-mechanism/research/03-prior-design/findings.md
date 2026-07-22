# Prior-Design Mining — `phase-3-dot` + `standard-bead-dot`

> Research pass for `2026-07-17-dot-mechanism`. Mines the two prior kerf works that
> designed the DOT workflow mechanism so the current hardening/redesign does not
> re-invent or blindly re-reject settled ground.
>
> **Two source works:**
> - **`phase-3-dot`** (2026-05) — the *foundational* design pass: node-type taxonomy,
>   edge/verdict routing, Attractor adoption, control-point framing, schema versioning.
>   D-decisions are the design record. Source: `.kerf/works/phase-3-dot/`.
> - **`standard-bead-dot`** (2026-06) — the *follow-on* pass: DOT engine had LANDED in code
>   (hk-30vlb); this work made default-flip + sub-workflow dispatch normative AND added the
>   "attractor-parity" node attributes (per-node model/effort, inline prompt, non_committing,
>   tool_command, goal, template-params). Its workflow-graph.md + execution-model.md DRAFTS
>   are the most advanced artifacts and touch all three operator themes. Source:
>   `.kerf/works/standard-bead-dot/`.
>
> **Operator directive:** constraints are OFF. Prior *rejections* are informative, not binding.

---

## 1. LOCKED DECISIONS (and rationale)

### Node types / taxonomy
- **5-node taxonomy collapsed to 4: `agentic | non-agentic | gate | sub-workflow`.** `control-point`
  is NOT a node type. Rationale: the four ControlPoint Kinds (Gate/Hook/Guard/Budget) have structurally
  disjoint triggers; only Gate's ("evaluate on transition attempt") maps to a node-arrival slot.
  Hooks/Guards/Budgets bind as node *attributes* (`hook_ref`/`guard_ref`/`budget_ref`). `gate` is "a node
  whose handler evaluates a Gate-kind ControlPoint." (Source: phase-3-dot D3; standard-bead-dot workflow-graph WG-001/WG-002; execution-model EM-006.)
- **`agent_type` is an OPEN set** (not a closed enum); unknown types surface as `structural` failure at
  dispatch. Validation weight carried by four-axis tags (EM-011). Same open-set posture for non-agentic
  subtypes. Rationale: handlers added frequently; closed enum = amendment per handler. (Source: WG-003/WG-004; phase-3-dot C1.)
- **`gate` AND `non-agentic` nodes REQUIRE `handler_ref`** — SUPERSEDES older EM-007 "agentic-only" prose.
  (Source: standard-bead-dot execution-model EM-007; workflow-graph WG-005; was phase-3-dot D3 open-follow-up #2.)
- **Node type enum is CLOSED; `parallel`/`parallel.fan_in` ABSENT (rejected, not stubbed).** New node type
  = major schema bump. (Source: WG-001.)

### Edge / verdict routing (the Attractor lineage)
- **Adopt Attractor's outcome+context model VERBATIM; justify only divergences.** Headline meta-decision.
  Five items verbatim: Outcome envelope `{status, preferred_label, suggested_next_ids, context_updates,
  notes}`; 5-step edge cascade; context as a separate state bag mutated only via `context_updates`;
  engine-MUST-fall-back-to-unconditional-edge invariant (Attractor audit-V3.2 fix); lowercase status
  strings. (Source: phase-3-dot D-attractor-adoption.)
- **5-step edge cascade:** condition → preferred_label → suggested_next_ids → weight → ordering_key(lexical).
  Deterministic, first-match. (Source: EM-041; WG-010.)
- **Unconditional-edge fallback invariant** (day-one normative). (Source: phase-3-dot D-edge-cascade-invariant; WG-011/012.)
- **Edge-condition dialect = restricted equality mini-language:** `lhs (== | !=) literal` joined by `&&`
  only. NO `||`/`!`/parens/`<`/`>`/functions/arithmetic. Disjunction = multiple edges. REJECTED CEL
  (gold-plating) and reusing control-points §6.4 guard language (premature unification — two dialects kept).
  (Source: phase-3-dot D5; WG-013/016.)
- **Edge-condition LHS whitelist (closed):** `outcome.status`, `outcome.preferred_label`,
  `outcome.failure_class`, `outcome.kind`, `context.<registered-key>`. Tighter than Attractor's open
  context surface by design. `outcome.payload.*` NOT a legal LHS. (Source: phase-3-dot D4; WG-014.)
- **Reviewer verdict rides `outcome.preferred_label`** (`{APPROVE, REQUEST_CHANGES, BLOCK}`); no
  first-class `verdict` field. (Source: phase-3-dot D-verdict-surfacing; WG-019.)
- **`failure_class` = top-level Outcome field, FAIL-only,** 6-class closed enum. Two-sided: handler HINT,
  daemon back-fills from `ErrX` sentinels and is AUTHORITATIVE; `compilation_loop` daemon-only. (Source:
  phase-3-dot D1/D2; execution-model EM-005/005c; WG-018.)
- **Status-primary routing ONLY; NO `retry_target`/`fallback_retry_target`** (rejected Attractor's second
  channel). Cross-node retry = an edge condition on `failure_class == 'transient'`. Future-tractable via
  additive amendment. (Source: phase-3-dot D16-α; WG-020.)
- **Terminal state = distinct terminal node IDs** (`close`, `close-needs-attention` reserved). NOT a
  `terminal_kind` attr, NOT edge-label inspection. (Source: phase-3-dot D12; WG-021/022.)

### Gate / control points
- **CP bind by-name via typed `*_ref` attributes** (`gate_ref`/`hook_ref`/`guard_ref`/`budget_ref`/
  `skills_ref`/`freedom_profile_ref`). Inline policy bodies forbidden. `policy_ref` DEPRECATED-AND-REJECTED
  (was overloaded); typed `skills_ref` + `freedom_profile_ref` are successors. (Source: phase-3-dot D3/D14; CP-036/055/056; WG-002.)
- **Gate Outcome = `kind=gate_decision` payload** (`{policy_id, decision, decision_actor,
  decision_evidence_ref, resolution_signal_id}`). Evaluated gate is always `status=SUCCESS`
  (allow/deny/escalate); un-evaluable gate is `status=FAIL`+`failure_class`, no payload. Mutually
  exclusive carriers. (Source: phase-3-dot D7/C3; execution-model EM-005a/005b.)

### Execution model / dispatch
- **`dot` mode is a BINDING document, not a new state machine** — §7.4/§7.3 already ARE the mode-agnostic
  dispatcher. (Source: phase-3-dot C2.)
- **Ingestion is a separate pre-run pass; re-parse on restart** (no serialized parse-tree trust),
  `run_id`-keyed. DOT lib lean: `gonum.org/v1/gonum/graph/encoding/dot`. (Source: C2 §7.5.1.)
- **Unknown-attribute policy = MIXED (D9):** strict for `type` enum + reserved names + closed-enum RHS;
  permissive (warn, retain in AST) elsewhere. (Source: phase-3-dot D9; WG-031.)
- **Schema version graph-level only** (`schema_version="1"`), N-1 readable, distinct from workflow
  `version`. (Source: D10; WG-033/035.)
- **Canonical examples in `specs/examples/`** with `<name>.md` sidecar. (Source: D11; WG-036/037.)
- **Sub-workflow expansion = in-place, single-run** (parent run_id only), namespaced `<parent>/<sub>`,
  acyclic, verbatim terminal-Outcome escape; review-loop MAY NOT be a sub-workflow. (Source: EM-034 family;
  standard-bead-dot sub-workflow-dispatch SW-001..010.)
- **Default mode flipped `single` → `dot`** at tier-4, hard `review-loop` FLOOR (never `single`) on
  embedded-graph load failure. LANDED (hk-30vlb) then normative. Canonical default graph `standard-bead.dot`
  (start → implement → commit_gate → review → close/close-needs-attention) with SOLE-inbound-APPROVE-edge-to-
  `close` review-floor invariant (WG-050). (Source: standard-bead-dot problem-space; execution-model
  EM-012a+FLOOR; workflow-graph §17 WG-047..052.)

---

## 2. DEFERRED / FUTURE WORK

### Directly touching the three operator themes
- **`model_stylesheet` / `class` (CSS-style per-node model selection)** — INFORMATIVE-only at v1; dispatcher
  MUST NOT route on them. Port `.hard{llm_model:X}`+`class="hard"` → direct `model="X"`. A real stylesheet
  selector (>2 tiers, indirection) is "a clean future amendment." **Deferred to hk-1xzg3.** (WG-043; per-node model theme.)
- **`transient_exit_codes` on tool nodes** — reserved-and-warning at v1; would let a tool-node author mark
  specific exit codes `transient` vs v1's blanket "every non-zero = deterministic." **Deferred until demand
  (hk-9j49t).** (WG-039; routing theme.)
- **`auto_status` (status derived from work product/marker)** — reserved-and-NOT-accepted at v1. Reserved
  for a future status-derivation feature. (WG-041.)
- **Reviewer-class `prompt` override** — accepted-but-INERT at v1 (reviewer brief is `review-target.md`);
  reserved for a future schema version. (WG-040; per-role input-routing theme.)
- **`context_keys` type-pinning** — v1 declares the list but does NOT validate LHS refs against it or pin
  types. "A future amendment will pin type-pinning." **D8 / OQ-WG-002. Most direct open door for typed params.** (WG-031a.)
- **Per-node model/effort resolution is LANDED, not deferred** — see §5 (only the stylesheet sugar is deferred).

### Other deferred
- NL → DOT generation (own project). Parallel branches `parallel`/`parallel.fan_in` (worktree is
  one-per-run sequential; reserved with one C2 sentence; OQ-WG-008). Kilroy `stack.manager_loop`
  sub-pipelines. Dynamic mid-run graph mutation. Project-local `.harmonik/workflows/` path (OQ-WG-004).
  `bead-process.dot` 2nd example (superseded by standard-bead.dot). Mechanism-tagged Gate schema-drift
  (D15/OQ-WG-006). 3-way merge fan-in. (Sources: phase-3-dot 01-problem-space §5; SUMMARY; WG §13.)

---

## 3. REJECTED IDEAS (informative — constraints OFF, so revisitable)

- **`retry_target`/`fallback_retry_target` node attrs** (Attractor 2nd routing channel) — REJECTED for
  status-primary edge routing (avoid dual authority). *Revisit if richer node-local failure recovery is
  wanted; docs call it "future-tractable via additive amendment."* (phase-3-dot D16-α; WG-020.)
- **Dedicated first-class `verdict` Outcome field** — REJECTED; rides `preferred_label`. *Revisit if
  reviewer→implementer feedback needs structured machine-readable verdict data — a `kind=verdict` payload
  was named as the additive path.* (D-verdict-surfacing.)
- **CEL / general expression language for edge conditions** — REJECTED (gold-plating). (D5.)
- **Unifying edge conditions with the control-points guard-predicate language** — REJECTED (premature
  unification; two dialects kept). *Revisit if the redesign wants one condition language everywhere.* (D5; WG §16.3.)
- **`control-point` as a 5th node type** (single type + `kind` discriminant) — REJECTED. (D3.)
- **`terminal_kind` attr and edge-label inspection** for terminal differentiation — REJECTED. (D12.)
- **Per-node `schema_version`** — REJECTED (graph-level only). (D10.)
- **Fully-strict OR fully-permissive unknown-attribute policy** — both REJECTED (mixed chosen). (D9.)
- **Re-deriving Outcome shape from first principles** — REJECTED (adopt Attractor verbatim). (D-attractor-adoption.)

---

## 4. OPEN QUESTIONS left unanswered

- **OQ-WG-002 / D8 — `context_keys` type-pinning** (types + LHS validation). *(Typed-params theme.)*
- **OQ-WG-004 — project-local workflow path** normativity.
- **OQ-WG-006 / D15 — mechanism-tagged Gate schema-drift** loader behavior.
- **OQ-WG-007 / D17 — tool-node handler contract** if a dedicated `tool` type is added.
- **OQ-WG-009 — `agent_type` catalog anchor** location.
- **OQ-C3-2 — unregistered `context_updates` key: warn-and-drop vs reject** (lean/adopted warn-and-drop; SW-008).
- **OQ-C3-4 — handler vs daemon `failure_class` disagreement: log vs escalate** (lean log-only).
- **OQ-C3-5 — may a gate PERMIT also carry gate_decision payload for audit-on-permit.**
- **standard-bead integration O-items:** 4 stale "single is default" spec assertions
  (PL-004a/ON-004a/BI-009a/HC-006) needed the flip; O-4 = the one remaining CODE gap, the
  `NodeTypeSubWorkflow` out-of-scope stub at `internal/daemon/dot_cascade.go:523`. (Source: standard-bead-dot/06-integration.md.)

---

## 5. IDEAS MOST RELEVANT TO THE THREE OPERATOR THEMES

### Theme A — Per-bead / per-node MODEL override  (ALREADY DESIGNED + largely LANDED)
- **EM-012b — model/effort resolution precedence** (standard-bead-dot execution-model). Run-level
  `(model, effort)` `ModelPreference` resolved once at claim time, per-field independent:
  tier1 per-task bead label `model:<alias>`+`effort:<level>` (effort closed enum
  `{low,medium,high,xhigh,max}`; conflict/unknown → tier absent + `bead_label_conflict`);
  tier2 per-project `.harmonik/config.yaml` (`agents.<agentType>.{model,effort}`, schema_version:1,
  impl `internal/daemon/projectconfig.go`); tier2.5 env vars `HARMONIK_CLAUDE_MODEL`/`_EFFORT` read at
  claim time (hot-reload; `internal/daemon/modelpreference.go`, hk-c5oxy); tier3 compiled default; tier4 empty.
- **EM-012b-NODE (tier 0, highest)** — an `agentic` node MAY carry `model=`/`effort=` (WG-042), overriding
  the run-level default for THAT node only; static graph data, not a 2nd walk, so replay-deterministic.
  `model` shape-validated only (`^[A-Za-z0-9._:/-]+$`, ≤128 chars — never verifies a real model; matches the
  operator "no external-version binding" memory); `effort` closed-enum, out-of-enum on a static node attr =
  ingest STRICT error.
- **Deferred sugar:** `model_stylesheet`/`class` (WG-043, hk-1xzg3).
- **Takeaway:** the precedence ladder (node > bead-label > project-config > env > compiled > fallback),
  per-field independence, shape-only opacity, and replay-determinism argument are all worked out. Open
  ergonomic gap = the stylesheet layer.

### Theme B — Typed parameters / variable passing
- **`context` bag + `context_updates`** — workflow-wide KV store, mutated only via `Outcome.context_updates`
  (Attractor §3.4). Edges route on `context.<key>`. Closest existing "variable passing" primitive.
- **`context_keys` graph attr + type-pinning DEFERRED (D8/OQ-WG-002)** — v1 declares keys but doesn't
  type-pin or validate LHS. **Single most direct open door for "typed parameters."**
- **`input_mapping` on sub-workflow nodes (WG-006/EM-034a)** — "typed key→key mapping projecting parent
  context into inner run's context." Existing model for scoped/typed input across a workflow boundary.
- **WG-045/046 template-param substitution** — `__UPPER_SNAKE__` placeholders substituted over raw `.dot`
  source ONCE at launch (before parse) from a launch param map sealed in the Run record (replay-identical);
  residual token = launch error; params TRUSTED/unescaped; can appear in any attribute. Existing
  launch-time value-injection mechanism (text-level, not typed).
- **WG-044 graph-level `goal`** — typed dispatcher-surfaced run objective via ExtraContext; composes with
  per-node `prompt` + bead body; may contain template-params.
- **`outcome.payload.*` deliberately NOT a routing LHS (D4)** — if typed structured outcome data must
  influence routing, promote to `context.<key>` or `preferred_label`.
- **Takeaway:** "typed params on edges" is NEW ground, but the container (context bag + context_keys),
  cross-boundary projection (input_mapping), and launch injection (template-params + goal) already exist.
  Redesign job ≈ close the D8 type-pinning deferral + decide whether params attach to EDGES specifically vs
  the existing node/context/launch channels.

### Theme C — Reviewer → implementer FEEDBACK routing
- **EM-015d-RFD — reviewer-feedback delivery.** Before each implementer iteration N≥2 the daemon writes
  `.harmonik/reviewer-feedback.iter-<N-1>.md` (verdict + flags + full notes + diff_summary), atomically,
  then paste-injects a "read this file, address every REQUEST_CHANGES flag" instruction into the resumed
  pane AFTER it is live. Invariant: file exists → pane live → paste-inject. File is `.gitignore`d.
- **EM-015d-RIA — reviewer input artifact.** Before each reviewer launch the daemon writes
  `.harmonik/review-target.md` (bead context, base/head diff SHAs, prior-verdict summaries, operator
  `LaunchSpec.reviewer_hints`) — the reviewer's SOLE structured context.
- **Same Claude session RESUMED across implementer iterations** (`claude --resume`); reviewer FRESH each
  iteration. Verdict from `.harmonik/review.json` → archived `review.iter-<N>.json` → mirrored to
  `context.last_verdict`.
- `outcome.preferred_label` = route on THIS review; `context.last_verdict` = trailing verdict.
- **Reviewer-class `prompt` INERT at v1** (WG-040) — live gap for per-role input routing.
- **Gaps for redesign:**
  1. The whole feedback pipeline (feedback file + paste-inject + review-target) is `review-loop`-MODE
     daemon code; NOT available to a general `dot` graph whose implement/review nodes are ordinary agentic
     nodes. Generalizing feedback to arbitrary implementer↔reviewer node pairs in a `dot` graph is unbuilt.
  2. Feedback is delivered via a well-known file + paste-inject, not a typed data channel between nodes. For
     structured typed feedback, the rejected `kind=verdict` payload is the named extension path.
  3. `reviewer_hints` (operator→reviewer) is one-directional + review-loop-scoped; no symmetric per-role
     input-routing surface for arbitrary node roles exists.

---

## Source index
- phase-3-dot: 01-problem-space.md; 03-research/SUMMARY.md (D1–D20 + 20 resolved items);
  04-design/{D-attractor-adoption, D-verdict-surfacing, D-edge-cascade-invariant, D5-edge-condition-dialect,
  control-point-node-type-design, C1-workflow-graph-design, C2-execution-model-dot-design,
  C3-handler-contract-outcome-design}.md.
- standard-bead-dot: {01-problem-space, 02-components, 06-integration}.md;
  05-spec-drafts/{workflow-graph, execution-model, sub-workflow-dispatch}.md — most advanced drafts, carrying
  EM-012b (model/effort), EM-015d-RFD/RIA (feedback), WG-042/043/044/045 (per-node model, stylesheet, goal,
  template-params), WG-006 (input_mapping), SW-001..010 (sub-workflow dispatch).
