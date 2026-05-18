# C2 Research — `specs/execution-model.md` §`dot` mode

> Pass-3 research for component C2 of `phase-3-dot`. Scope: the daemon walker + edge-cascade evaluator that turns `workflow_mode = dot` from a reserved placeholder into a live dispatch path. **No design picks here** — candidates only.

## 0. Existing surface (what is already specced)

The walker does not need to invent a frame; most of it is already pinned in `specs/execution-model.md`:

- **`workflow_mode` enum (§3 glossary, EM-012, EM-012a).** `{single, review-loop, dot}`. `dot` is the "general workflow-graph walker (post-MVH)". The `workflow:dot` label is accepted at MVH (label-resolution must not crash) but no MVH dispatcher is obligated to drive it (§10.1 conformance).
- **§7.4 main-loop pseudocode.** `orchestrator_main_loop` + `execute_workflow` are already published. `execute_workflow` already encodes: dispatch → outcome → RETRY check → durability check → checkpoint → `select_next_edge` (cascade §7.3) → advance / STAY / ESCALATE / FAIL.
- **§7.3 deterministic edge-selection cascade.** Already specced as condition → preferred_label → suggested_next_ids → weight → ordering_key. This is the post-MVH `dot`-mode router; for `review-loop` and `single` it currently runs but is fed by hardcoded paths.
- **EM-005 Outcome shape.** `{status, preferred_label, suggested_next_ids, context_updates, notes, kind, payload}`. The cascade's input vocabulary is the same Outcome the review-loop already produces.
- **EM-034 sub-workflow expansion** + EM-034a/b/c (namespacing, acyclicity, durable pin on entry checkpoint).
- **EM-043 per-edge traversal caps** + EM-043a (counter storage = memory authoritatively re-derivable from git scan; one counter per `(run_id, edge)`).
- **EM-046a no-matching-edge** = failure class `structural`.
- **EM-042 / EM-042a gate-deny continuation** via `gate-pending` sub-state.

**Implication.** The C2 amendment is largely a *binding* document: it names the dispatch driver, declares how `workflow_mode = dot` selects it, and ties together pieces already specced for the other modes. Many "open questions" in pass-1/pass-2 are already answered by EM-012a/EM-041/EM-043/EM-046a/EM-034c — the design-pass risk is *redundant re-specification*, not under-specification.

## 1. Daemon walker — state-machine shape candidates

Three candidate shapes:

### 1A. "Drive the existing `execute_workflow`" (minimal)

The §7.4 `execute_workflow` pseudocode is *already* the walker; `single` mode is `execute_workflow` against a workflow whose graph has one agentic node + one terminal. `dot` mode is `execute_workflow` against a *user-authored* graph. The C2 amendment becomes a short clause: "for `workflow_mode = dot`, the workflow object is loaded from the validated parse tree of a `.dot` file at claim time; §7.4 applies unchanged."

- **Pros.** Reuses every existing requirement; no new state shape; no parity-with-Kilroy gap (§7.3 cascade *is* Kilroy's 5-step cascade). Implementation = pure data binding.
- **Cons.** Hides the dispatch-driver as a "load step"; readers expecting a distinct §4.13 driver may miss it. Doesn't surface the .dot-specific concerns (parse, validate, version-check) that don't apply to hardcoded `single`/`review-loop`.

### 1B. Distinct `dot`-mode walker sub-section, parallels EM-015d

Author EM-015g or §4.13 "DOT-mode walker" naming: ingestion (parse + validate), graph loading into `Run.workflow`, then "dispatch per §7.4". A small section, not a new state machine.

- **Pros.** Mirrors how `review-loop` is documented (EM-015d/e); makes the "you can write a .dot" affordance discoverable. Gives a place to put DOT-specific concerns (schema_version, unknown-attribute policy, library choice).
- **Cons.** Slight prose duplication with C1 (`workflow-graph.md`) and §7.4.

### 1C. "Walker subsystem" framing

A larger section that re-declares dispatch state. **Reject preemptively** — collides with §7.4's authority and Kilroy-parity (Attractor + Kilroy are single-threaded top-level, recon-Kilroy §Graph structure / recon-Attractor §Concurrency). §7.4 matches.

Recon parity: **single-threaded top-level traversal**. §7.4 `spawn_async execute_workflow(run)` is at the *run* level (§4.11 concurrency); inside one run, nodes are sequential. No tension.

**Lean (research-only):** 1A or 1B. 1B is more discoverable; 1A is most spec-economical. Design pass picks.

## 2. Edge-condition evaluation site — who runs the cascade

Three loci, all consistent with current spec text:

- **(a) Daemon.** §7.3 `select_next_edge` runs inside `execute_workflow` (daemon process). Conditions are key=value literals over `Outcome.status` + `failure_class` + whitelisted context keys (per pass-1 G2 lean). Simple, deterministic, audit-friendly.
- **(b) Policy-engine subsystem.** Push condition evaluation behind the Policy Engine (S02). Permits richer DSL later. *But* §4.10.EM-042 already reserves the policy-engine surface for **guards** (pre-cascade reorder) and **gates** (post-cascade permit/deny). Doubling it up duplicates surface.
- **(c) Handler.** Handler returns Outcome whose `preferred_label` is already the routing hint. Condition syntax becomes superfluous. *But* this collapses cascade steps 2+3 over step 1 and breaks Kilroy/Attractor parity (both spec the 5-step cascade with condition first).

Cited recon: edge selection is daemon-resident in Attractor (§3.3) and in Kilroy. Kilroy audit V3.2 found a real bug where every-condition-miss leaked into success-only edges — fallback-to-any-edge was added. Harmonik should bake the fallback into spec, not implementation. EM-046a already does this (no-match → `structural` failure).

**Lean:** (a). Already implicit in §7.4. Design pass should make the site explicit (one sentence).

## 3. Failure-class → edge-routing cascade

### 3.1 Failure-class enum

Attractor's six: `transient_infra`, `budget_exhausted`, `compilation_loop`, `deterministic`, `canceled`, `structural`. Harmonik's §8 already adopts the same six (execution-model.md §8 references `transient | structural | deterministic | canceled | budget_exhausted | compilation_loop`). **Set is closed.** No design choice for v1.

### 3.2 Class-as-routing-input

Attractor retry routing (recon-Attractor §Failure handling): `fail-edge → retry_target → fallback_retry_target → terminate`. Kilroy spec-vs-code tension: spec treats status as primary, code treats class as primary (recon-Kilroy + audit V3.6/V4.3/V4.9).

Harmonik posture is **status-primary** (§7.3 cascade keys on `outcome.status`; EM-046b on `RETRY`). Two candidate extensions:

- **(α) Status-primary, failure-class as condition-LHS sugar.** Edge condition can name `failure_class=transient_infra`; daemon resolves from Outcome meta/payload. No retry_target/fallback_retry_target on the node — routing is purely graph-expressed.
- **(β) Status-primary, plus node-level `retry_target` / `fallback_retry_target` attributes.** Adds Attractor's per-node attributes. Two routing axes coexist: declarative edges + per-node fallback. Mirrors Attractor §3.7.

(α) is strictly simpler and keeps the graph as the sole routing authority — consistent with harmonik's centralized-controller principle. (β) lets graph authors omit fail-edges and lean on node attributes, but introduces "where does routing live?" reader confusion.

**Lean:** (α). Surface for design pass — **OPEN-1**.

## 4. Cycle bounding (EM-043 per-edge traversal caps)

**Already specced.** Confirm:

- Every cycle MUST have ≥1 edge carrying a `traversal_cap` (positive int).
- Counter is per-`(run_id, edge)` tuple; in-memory authoritative for live runs, git-derivable on restart.
- Hit-cap → fail-class `compilation_loop`, disjoint from `structural`.
- Multi-cycle edge: one shared counter (§11 OQ-EM-004 already records this).

**Open issue surfaced:** §11's pathological-multi-cycle audit is still TODO; not a C2 blocker. The amendment should cite EM-043/§7.4 advance step and not re-spec.

**Daemon-restart caveat.** EM-043a says counter is git-derivable. For `dot` mode, daemon scans run's task branch and counts traversals of each capped edge. Cost `O(commits × capped_edges)`. Acceptable at MVH scale; surface as informative note.

## 5. Sub-workflow expansion (EM-034)

**Already specced**; §4.3.EM-015d explicitly excludes `review-loop` from EM-034 expansion. For `dot` mode:

- Sub-workflow nodes inside a `.dot` graph expand per EM-034a (namespaced node_id), EM-034b (acyclic reference graph, validator-enforced), EM-034c (pin durable on entry checkpoint).
- Parent run's `run_id` is the only run identifier; checkpoints all on parent task branch (EM-035).
- Worktree: parent owns it; sub-workflow nodes share.

**Open issues for design pass (not blockers):**
- Is sub-workflow registry harmonik-internal or does a `.dot` author reference by path? Pass-2 §C1 OQ; not C2's call.
- Can a `dot`-mode workflow reference a `review-loop` sub-workflow? §4.3.EM-015d says "review-loop is mode-driven, not graph-driven" — implies **no**. Confirm in design pass.

## 6. Walker × worktree isolation

The walker spawns: **one worktree per run, not per node, not per agentic-node cluster.** Anchors:

- workspace-model.md §4.1 WM-002: per-run canonical path `<repo>/.harmonik/worktrees/<run_id>/`.
- workspace-model.md §4.5: "Sub-workflow expansion ... does NOT create additional task branches or workspaces."
- workspace-model.md §4.7 line 294: "AT MOST ONE agent process MAY be actively writing to the worktree. Agents run sequentially as the run traverses its workflow graph."

This **diverges from Kilroy's parallel-branch-per-worktree** model (recon-Kilroy §Worktree isolation). Divergence is already locked: harmonik defers `parallel` / `parallel.fan_in` post-Phase-3 (pass-1 §5). Walker dispatches sequentially within a run, one node at a time, all into the same worktree. No new C2 amendment text needed beyond a cross-reference.

**If parallel-fan-out is re-opened post-Phase-3,** the worktree model has to change (Kilroy-parity branch-per-fan-out-branch). Out of scope; record in OPEN-3.

## 7. Cross-component dependencies

- **C1 (`workflow-graph.md`) → C2.** C2 consumes C1's node-type catalog, edge-condition LHS whitelist, schema-version policy. C2 cannot land until C1 freezes the validated parse-tree shape.
- **C3 (`handler-contract.md` Outcome) → C2.** Cascade reads Outcome; if C3 amends Outcome to require `failure_class` on FAIL, C2's condition-LHS whitelist gains a new key. EM-005 already references C3's obligation.
- **C4 (`control-points.md` node-type binding) → C2.** Only if control-point is its own node type with a guard/gate evaluation point distinct from EM-042 — currently EM-042 already names guards/gates as separate from the cascade.
- **C5 (`specs/examples/*.dot`) → C2.** Examples must round-trip §7.4 + §7.3 against the validated parse tree. C2 fixes the dispatch contract the examples test.

## 8. Surprises from reading the daemon code (`internal/daemon/reviewloop.go`)

1. **`runReviewLoop` is not parameterized by a graph object.** It is a hand-rolled state machine over `reviewLoopState` (iterationCount, claudeSessionID, lastVerdictNotes, lastDiffHash). For `dot` mode the equivalent driver consumes a parsed `Workflow` record (§6.1) and dispatches by `current_node`'s type — a much smaller per-iteration state struct. **Implication:** C2's amendment doesn't have to model "iteration state" at all; `Run.state` + traversal-counter map per EM-043a are sufficient.
2. **No node-type registry exists yet.** `runReviewLoop` hardcodes the two phases (`implementer-initial`, `implementer-resume`, `reviewer`) directly via `ReviewLoopPhase`. A `dot`-mode dispatcher would need a `(node_type → dispatch_fn)` table that doesn't exist. **Implication:** the amendment should name "node-type dispatch table" as a daemon-internal concept; it currently has no spec presence because `single` / `review-loop` don't need it. Surface in OPEN-2.
3. **Reviewer-feedback / review-target artifacts are mode-specific.** `.harmonik/reviewer-feedback.iter-N.md` and `.harmonik/review-target.md` are normative under EM-015d-RFD / EM-015d-RIA but have no analog for `dot` mode. C2 should explicitly state these artifacts are review-loop-only.
4. **`workflowMode` is passed all the way down into `claudeRunCtx`** (reviewloop.go:173). The handler-launch path already accepts `workflow_mode`, so adding `dot` requires no plumbing change at the handler boundary — the dispatch fork happens above the launch. The integration surface for C2 is small.
5. **Hook-store registration** (`deps.hookStore.RegisterHookSession(...)`) is generic — scopes hook outputs by `(run_id, claude_session_id)`. A `dot`-mode dispatcher reuses verbatim per-node. No spec text required.

## 9. Top design questions surfaced for pass-4

- **OPEN-1.** Status-primary edges with `failure_class` as condition-LHS sugar (α) vs. add Attractor-style `retry_target` / `fallback_retry_target` node attributes (β). Resolves §3.2 above. **Lean (α)** for v1; defer (β) to post-MVH amendment. Design pass picks.
- **OPEN-2.** Should C2 introduce a "node-type dispatch table" as a normative concept (declared per-node-type in C1 with the dispatch contract in C2), or leave dispatch-by-type as an implementation detail and only require "the daemon dispatches per node type per the handler-contract per the node-type catalog"? Affects how prescriptive C2's prose is.
- **OPEN-3.** Does C2 reserve language for parallel-node fan-out so a post-Phase-3 amendment can land without rewriting §7.4? Candidate: "MVH `dot` mode dispatches a run's nodes sequentially; parallel-node-type semantics are deferred to a post-MVH amendment per [architecture.md §4.6]." — one sentence sufficient.

## 10. Sources

- `~/.kerf/projects/gregberns-harmonik/phase-3-dot/01-problem-space.md` §3 G3, §3 G5, §3 G6, §4 D5, §9 Q3.
- `~/.kerf/projects/gregberns-harmonik/phase-3-dot/02-components.md` §C2 (path, kind, gaps absorbed, open questions Q3/Q5/Q7).
- `.kerf/recon/kilroy-findings.md` §Node model, §Edge/transition model, §Failure taxonomy, §Worktree isolation, §Open questions (#4).
- `.kerf/recon/attractor-findings.md` §Workflow model, §Replay semantics (cascade), §Failure handling (`retry_target` / `fallback_retry_target`), §Concurrency (single-threaded top-level), §Fit for harmonik §Adopt #3, #6.
- `specs/execution-model.md` §3 (workflow_mode glossary), §4.1.EM-005 (Outcome), §4.3.EM-012a (resolution precedence), §4.3.EM-015d (review-loop EM-034 carve-out), §4.5.EM-023a (durability), §4.8.EM-034..EM-037 (sub-workflow), §4.10.EM-041 + §7.3 (cascade), §4.10.EM-042/EM-042a (guards/gates), §4.10.EM-043/EM-043a (traversal cap + counter), §4.10.EM-046a/EM-046b (no-match + RETRY), §7.4 (main-loop pseudocode), §10.1 (conformance — `dot` reserved).
- `specs/workspace-model.md` §4.1.WM-002 (canonical per-run path), §4.5 (sub-workflow does not create new worktrees), §4.7 line 294 (one-agent-per-worktree).
- `internal/daemon/reviewloop.go` lines 1-250 (hand-rolled state machine; no graph parameterization; hook-store and `workflowMode` plumbing).
