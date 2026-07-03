# Pass-3 Research Summary — `phase-3-dot`

> **Upstream alignment (added pass-4):** The headline meta-decision for this phase is to **adopt Attractor's outcome+context model verbatim and deviate only where harmonik has a specific reason**. See [`04-design/D-attractor-adoption.md`](../04-design/D-attractor-adoption.md) for the five items adopted verbatim (Outcome envelope shape, 5-step edge-routing cascade, context-as-state-bag, engine-MUST-fall-back-to-unconditional-edge invariant, lowercase canonical status strings) and the four genuine harmonik divergences (`failure_class` parallel field, JSONL events, three-artifact separation, skill-injection-via-handler-contract). Subsequent D-rows in the matrix below resolve to specific adoptions or divergences from that meta-decision.

> Cross-cutting index across the 5 component findings (C1 workflow-graph, C2 execution-model §dot, C3 handler-contract Outcome, C4 control-points binding, C5 examples). **This is not a design.** It synthesizes the open-decision surface so pass-4 (`change-design`) can pick decisions in dependency order without re-reading 1,100+ lines of findings.

## Per-component headline

**C1 — `specs/workflow-graph.md` (new).** Half the audit "gaps" are already partially specified in `execution-model.md` (EM-006 taxonomy, EM-002 edge fields, EM-041 cascade, EM-005 Outcome shape, §8 failure classes, §6.4 schema-version contract). C1's primary job is **consolidation + extension**, not greenfield design. Genuinely open: (a) the concrete `agent_type` catalog within `agentic`, (b) the gate-vs-control-point boundary (Q1), (c) the LHS whitelist for edge `condition` expressions, (d) whether `failure_class` is a legal LHS, (e) in-repo `.dot` path convention, (f) unknown-attribute policy.

**C2 — `specs/execution-model.md` §dot (extension).** The walker does NOT need a new state machine — §7.4 `execute_workflow` + §7.3 cascade already are the dispatcher. The amendment is largely a **binding document** ("for `workflow_mode=dot`, load workflow from validated parse tree; §7.4 applies unchanged"). Closed-already items: 6-class failure enum, EM-046a no-match fallback, EM-043 cycle bounding, EM-034 sub-workflow expansion, EM-046b RETRY protocol, single-threaded-per-run worktree model. Genuinely open: edge-condition evaluation site (lean: daemon), status-primary vs. node-level `retry_target` (lean α: status-primary), whether to introduce a normative "node-type dispatch table," and whether to reserve language for post-MVH parallel fan-out.

**C3 — `specs/handler-contract.md` §Outcome (extension).** Outcome is normatively owned by EM-005, not by handler-contract. Closed: shape adequate for agentic + non-agentic + sub-workflow (EM-036a verbatim propagation). Provably-insufficient gaps: (a) gate-node rationale capture — `notes` is unparseable, `preferred_label` conflates routing-with-evidence, EM-046c correlation has no field; (b) FAIL-outcome failure-class disambiguation — current Outcome carries no class, so the edge cascade cannot route on it. Top decisions: `failure_class` placement (lean A: top-level field on FAIL), gate-node payload extension (lean: new `kind=gate_decision`), `context_updates` typing discipline (lean: per-workflow registered-key list).

**C4 — `specs/control-points.md` §node-type binding (extension).** CP contract already locks the by-name binding (CP-002/CP-036), required YAML sections (CP-035/§6.3), daemon-scoped flat registry (CP-043/045), source precedence and N-1 schema-version readability (CP-037/038). Three framings for Q1: (A) drop control-point from node-type catalog, gate-node = "node whose handler evaluates a Gate-kind CP" — 5→4 taxonomy; (B) reject; (C) single `control-point` node-type with `kind` discriminant. Real ambiguity: `policy_ref` is overloaded across gates + freedom-profiles + skill-sets — wants a typed `skills_ref` symmetric with the other `*_ref` family. Schema-version drift handling for mechanism-tagged Gates is unaddressed (cognition path is closed via CP-040a hash + CP-INV-003 escalation).

**C5 — `specs/examples/` (new directory).** `review-loop.dot` is well-scoped: EM-015d already pins terminal-node set, routing inputs, reserved `Run.context` keys, and iteration cap. The example surfaces a real constraint on C1: are `verdict`, `completion_reason`, `iteration_count` legal LHS keys, or only `outcome.status`/`failure_class`? `bead-process.dot` is **more speculative** — depends on tool-handler contract, merge node, and reconciliation primitives that aren't specced. **Recommendation: ship review-loop.dot alone in C5; defer bead-process.dot to a post-Phase-3 follow-up bead.** Directory shape lean: flat for v1; mandatory README mapping each `.dot` to its spec anchor; two-layer testing (static round-trip + scenario harness with golden trace).

---

## Cross-cutting decision matrix

Top decisions across the 5 components; "Lean" = research-expressed lean (NOT a pass-4 commit). "Blocks" = components that cannot finalize until this decision lands. "Closes gap" = pass-1 G-id.

| # | Decision | Owning component | Blocks | Closes | Lean |
|---|---|---|---|---|---|
| D1 | Is `failure_class` a permitted LHS on edge `condition` expressions? | C1 | C2, C3, C5 | G2, G5 | yes (sugar over Outcome meta) — **LANDED (D1-edge-condition-lhs-admit-failure-class.md)** |
| D2 | `failure_class` placement on Outcome (top-level field vs. notes vs. new `kind`) | C3 | C1 (LHS whitelist), C2 (cascade input) | G5 | Option A — top-level field on FAIL — **LANDED (D2-failure-class-placement.md)** |
| D3 | Q1 — control-point as node-type vs. policy primitive vs. discriminant | C1 + C4 (symmetric) | C2 (dispatch table), C3 (control-point payload), C5 (bead-process) | G1 | Framing A (drop from catalog) or C (single type w/ discriminant); reject B — **LANDED (control-point-node-type-design.md)** |
| D4 | Edge-condition LHS whitelist enumeration | C1 | C2 (validator), C3 (Outcome surface), C5 (review-loop verdict) | G2, Q6 | Outcome fields + whitelisted context keys — **LANDED (D4-edge-condition-lhs-whitelist.md)** |
| D5 | Edge-condition dialect (CP §6.4 predicate language vs. restricted `key=value` mini-language) | C1 | C2 (evaluator), C4 (guard-vs-edge boundary) | G2 | Restricted equality mini-language — **LANDED (D5-edge-condition-dialect.md)** |
| D6 | Verdict surfacing for review-loop (status-mapping vs. context_updates key vs. first-class field) | C3 + C1 | C5 (review-loop.dot), C2 (cascade) | G1 partial, G4 partial | Adopt Attractor `preferred_label` — **LANDED (D-verdict-surfacing.md)** |
| D-attractor-adoption | Headline meta-decision: adopt Attractor outcome+context model verbatim; deviate only where harmonik has a specific reason | All | (frames every Outcome/cascade/context decision) | (frame) | **LANDED (D-attractor-adoption.md)** |
| D-edge-cascade-invariant | Lock 5-step cascade + engine-MUST-fall-back-to-unconditional-edge (Attractor audit V3.2 fix) as normative day-one invariant | C2 | C1 (edge prose), C5 (fallback scenario) | G2 | **LANDED (D-edge-cascade-invariant.md)** |
| D7 | Gate-node Outcome surface — `notes`+`preferred_label` vs. `kind=gate_decision` payload | C3 | C4 (escalated-gate path), C5 (gate examples) | G1 partial | `kind=gate_decision` payload |
| D8 | `context_updates` typing discipline (free-form vs. per-workflow registered keys vs. typed schema) | C3 | C1 (LHS whitelist), C2 (cascade) | Q6, G2 partial | Per-workflow registered-key list |
| D9 | Unknown-attribute policy (strict / permissive / mixed) | C1 | C2 (validator), C5 (forward-compat tests) | G6, Q3 | Mixed (Option C: strict for enums + reserved, permissive elsewhere) |
| D10 | `schema_version` placement — graph-level only vs. graph + per-node | C1 | C2 (parser) | G6, Q4 | Graph-level only |
| D11 | In-repo path for `.dot` files — `specs/examples/` for canonical; `.harmonik/workflows/` for project-local | C1 + C5 | C5 (dir layout), C2 (loader) | G6 | Declare `specs/examples/` only; project-local out of scope at v1 |
| D12 | Terminal-node differentiation (`close` vs. `close-needs-attention`) — `terminal_kind` attr vs. distinct terminal IDs vs. edge-label inspection | C1 + C2 | C5 (review-loop.dot) | G1 partial | (open) |
| D13 | `bead-process.dot` scope (inline single review / inline review-loop / sub-workflow ref) | C5 | C6 (epic bead-process artifact) | G4 partial | Defer entire example post-Phase-3 |
| D14 | Disambiguate `policy_ref` overload — introduce typed `skills_ref` | C4 | C5 (gate examples), C1 (node attribute table) | G1 partial | Yes — add typed `skills_ref` |
| D15 | Schema-version drift handling for mechanism-tagged Gates (re-validate on bump / require pin / accept) | C4 | C2 (runtime check), C5 | G6 partial | (open) |
| D16 | Status-primary edges plus `retry_target` / `fallback_retry_target` node attrs? | C1 + C2 | C3 (Outcome routing fields), C5 | G5 | α — status-primary only; defer β |
| D17 | Tool-node handler contract — exit-code→status mapping vs. full handler-contract conformance | C1 (node-type table) | C5 (bead-process if kept), C6 (parser) | G1 partial | (open — only material if bead-process kept) |
| D18 | Sub-workflow `kind`/`payload` propagation (verbatim per EM-036a vs. normalize at boundary) | C3 | C2 (parent cascade behavior) | G1 partial | Verbatim (status quo per EM-036a) |
| D19 | Normative "node-type dispatch table" in C2 (yes/no) | C2 | C6 (dispatcher bead acceptance) | G3 | (open) |
| D20 | Reserve language for post-MVH parallel fan-out in C2 | C2 | (none — opt) | (none) | Yes — one sentence, defer per architecture.md §4.6 |

---

## Suggested pass-4 design order

Decisions must land in dependency order. The shortest sufficient sequence:

1. **D3 (Q1 — control-point framing).** Blocks the C1 catalog, C4 contract, C2 dispatch-table shape, C5 bead-process scope. C1+C4 cannot be drafted without this. Single highest-leverage call.
2. **D2 (`failure_class` placement on Outcome).** Once D3 frames the node-type table, D2 pins the Outcome field that the cascade routes on. Drives D1, D4, D8.
3. **D1 (`failure_class` as edge-LHS) + D4 (LHS whitelist) + D5 (condition dialect).** A single cluster — D1 and D4 are jointly the answer to "what can edges route on"; D5 picks the grammar. Once these land, C1 §Edge Conditions is writable end-to-end. Drives D6.
4. **D6 (verdict surfacing) + D8 (context_updates typing) + D12 (terminal-node differentiation).** The C5 review-loop example forces these. Once they land, the example is writable.
5. **D9, D10, D11 (schema versioning + repo convention).** Independent of the above; can run in parallel with steps 2–4. Closes G6 in full.

D14 (`skills_ref`), D16 (`retry_target`), D7 (gate-decision payload), D13 (bead-process scope), D18 (sub-workflow propagation), D20 (parallel-reserve sentence) are tractable in any order after the above and are good parallel-lane work for pass-5 sub-agents.

D15 (mechanism-tag schema drift), D17 (tool-node contract), D19 (dispatch-table normativity) are pass-4-or-defer judgment calls — research did not surface a lean.

---

## Already-resolved items — DO NOT re-litigate in pass-4

Research surfaced multiple items the audit framed as "open" that are in fact closed:

1. **EM-006 5-node taxonomy is locked.** `agentic | non-agentic | gate | control-point | sub-workflow`. Only Q1 (control-point's framing within this) is open; the taxonomy categories themselves are not.
2. **EM-002 edge fields are locked.** `from_node, to_node, condition, preferred_label, weight, ordering_key`. C1 cites; does not redesign.
3. **EM-041 5-step edge-selection cascade is locked.** condition → preferred_label → suggested_next_ids → weight → ordering_key.
4. **EM-005 Outcome shape is locked.** `status, preferred_label, suggested_next_ids, context_updates, notes, kind, payload`. C3 extends only via `kind`/`payload` discriminator (EM-005a) or by adding `failure_class` as a top-level field (D2). The closed status enum `{SUCCESS, FAIL, RETRY, PARTIAL_SUCCESS}` does not change.
5. **EM-005a `kind` extension protocol is closed.** Unknown `kind` → reconciliation Cat 6a, NOT silently degraded to `default`. C3 does not need to redesign discriminator semantics.
6. **§8 6-class failure taxonomy is locked.** `transient, structural, deterministic, canceled, budget_exhausted, compilation_loop`. The set is closed. Audit's "harmonik has not picked which set is canonical" is **stale**.
7. **EM-046a no-matching-edge fallback is closed.** Empty match set → `structural` failure with reason `no_outgoing_edge_matches`. G2's "failed-edge-fallback" sub-question is resolved.
8. **EM-046b RETRY protocol is closed.** Same-node re-dispatch with attempt-cap; reclassifies to `transient` on cap. §8.1 `transient` cap-exhaustion reclassifies to `structural`. Disjointness with `compilation_loop` is explicit.
9. **EM-043 cycle bounding + EM-043a counter storage are closed.** Per-`(run_id, edge)` counter; in-memory authoritative, git-derivable on restart. C2 cites; does not redesign.
10. **EM-034 / EM-034a-c sub-workflow expansion is closed.** Namespacing, acyclicity, durable-pin-on-entry. Parent owns `run_id`, task branch, worktree. EM-015d explicitly excludes review-loop from EM-034 (review-loop is mode-driven, not graph-driven) — so `dot` mode CANNOT reference a `review-loop` sub-workflow.
11. **EM-036a sub-workflow outcome lifting is closed.** Parent sees the **verbatim** terminal-node Outcome. No synthesis at the sub-workflow boundary. (D18 confirms verbatim is the v1 rule; only flagged for future amendment if needed.)
12. **EM-042 / EM-042a gate-deny continuation via `gate-pending` is closed.** Cascade carve-out for guards (pre-cascade reorder) and gates (post-cascade permit/deny) is in place.
13. **Failure-class classification authority is closed in harmonik's favor.** §8 says classification is from handler-returned `ErrX` sentinel, period. `failure_class` is NOT on Outcome today (it's a payload field on `run_failed` event). This is a *better* position than Kilroy's; C3 must not regress it. (D2 adds a derived field to Outcome but does NOT change classification authority.)
14. **§6.4 schema-version contract is closed.** All schemas carry `schema_version` integer; N-1 readable per operator-nfr §4.5; additive bumps; renaming/removing is breaking.
15. **CP-002/CP-036 by-name DOT→YAML binding is closed.** `gate_ref`, `policy_ref`, `freedom_profile_ref`, `budget_ref` carry the YAML `name`. Inline policy bodies in DOT are forbidden.
16. **CP-043/045 daemon-scoped flat registry is closed.** One in-process Go map keyed by `name`, rebuilt at startup. No cross-daemon sharing. AR-INV-007 forbids out-of-daemon stores.
17. **CP-037/038 source precedence + N-1 readability are closed.** Runtime > operator > workflow > defaults; deep-merge; no mid-run reloads; per-doc schema_version integer.
18. **Worktree model — one-per-run, sequential agents (WM-002, §4.5, §4.7) — is locked.** Walker dispatches sequentially within a run; one worktree per run; sub-workflow expansion does NOT create new worktrees. Parallel fan-out is post-Phase-3.
19. **EM-015d review-loop terminal-node set + reserved context keys are locked.** Terminals: `close` (APPROVE), `close-needs-attention` (BLOCK / cap-hit / no-progress). Reserved keys: `iteration_count, last_verdict, claude_session_id, last_diff_hash`. Iteration cap = 3 (hardcoded; not graph-tunable at MVH).
20. **§7.4 main-loop pseudocode + §7.3 cascade — already specified; C2 does NOT redesign.** The amendment is a binding/cross-reference, not new state-machine prose.

Items 1–20 represent the bulk of the pass-1 audit gap surface. Pass-4 should treat them as immutable inputs and direct design effort exclusively at D1–D20 above.

---

**Sources:** `01-problem-space.md`, `02-components.md`, `03-research/{workflow-graph,execution-model-dot,handler-contract-outcome,control-points-binding,examples}/findings.md`, `decompose-review.md`, prior recon (`.kerf/recon/{kilroy,attractor}-findings.md`), live specs (`specs/execution-model.md`, `specs/handler-contract.md`, `specs/control-points.md`).
