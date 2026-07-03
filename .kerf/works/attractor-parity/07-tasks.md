# Implementation Tasks — `attractor-parity`

> Pass 7 (Tasks). Maps the unified `SPEC.md` (Pass 6 integration-APPROVED) to sequenced, implementation-agnostic tasks. Every changelog entry has ≥1 implementing task; every task traces to a SPEC section. Bead mapping (existing-to-refine vs new-to-file) is in §"Bead Mapping". The dependency graph and parallelization plan are at the bottom.
>
> **Code anchors verified live (2026-05-28):**
> - `internal/daemon/dot_cascade.go:198-203` — non-agentic SUCCESS synth (tool-node split point, T2).
> - `internal/daemon/dot_cascade.go:343-593` — `dispatchDotAgenticNode` (THE co-edited function: B prompt/role pre-launch, C non-committing post-launch, D model/effort). HEAD-advance check at `:585-591`.
> - `internal/daemon/claudelaunchspec.go:48` `claudeRunCtx`, `:213` `buildClaudeLaunchSpec`, `:244-246` `taskBody := rc.beadDescription`.
> - `internal/workspace/agenttask_chb028.go` — `Body` (`## Task Description`) before `ExtraContext` (`## Extra Context`); B↔E channels confirmed distinct.
> - `internal/core/node.go:8` `core.Node` (RECORD); `Timeout *int` already present (`:28-31`) — REUSE for tool-node `timeout` per SPEC §II.3 note.
> - `internal/workflow/dot/ast.go:86` `dot.Node`; `:34` `dot.Graph` (has `UnknownAttrs`; `role=` currently lands there).
> - `internal/workflow/dot/parser.go:509` `reservedGraphAttrs`, `:601-664` node-attr switch, `:534-562` graph-attr switch (add `goal`).
> - `internal/workflow/loader.go:56` `LoadDotWorkflow(dotPath)`; daemon calls it at `internal/daemon/workloop.go:1277`. The `--param` substitution (E) must run on the raw source string BEFORE `dot.Parse` here.

---

## Task List

### T0 — Spec-text landing (apply SPEC.md to real `specs/`)

- **What:** Apply all normative SPEC.md changes to the three live spec files. No code. This is the spec-first contract: code tasks below cite the landed IDs.
  - `specs/workflow-graph.md`: insert WG-039…WG-046 (after WG-038 / in §4 per the SPEC insertion anchors); merge the §4 WG-002 `agentic` row (add `prompt`, `non_committing`, `model`, `effort`) and `non-agentic` row (add `tool_command`, `timeout`) + the two WG-002 Notes bullets; append the WG-024 tool-node validation bullet (§I.10); replace the §10 WG-031 reserved-set sentence with the merged sentence (§I.11) + position rules; append §16.1 vocabulary-diff rows; mark OQ-1/OQ-2 RESOLVED.
  - `specs/execution-model.md`: reword EM-012b "resolve once" paragraph + add EM-012b-NODE sub-clause + tier-0 precedence summary (§II.1); add EM-015d dot-carve-out bullet (§II.2); add Node-RECORD fields `model`/`effort`/`tool_command`/`timeout`(reused slot)/`prompt`/`non_committing` (§II.3 — heed the reuse note: NO `timeout_command`, reuse `Node.timeout`); add Workflow-RECORD `goal` (§II.4) + Run-RECORD `template_params` (§II.5); §6.4 additive schema notes (§II.6); amend EM-057 item 7 (§II.7); replace EM-058 `non-agentic` row + two sub-notes (§II.8 — the keystone); amend §7.5 launch surface (§II.9) + B↔E composition contract (§II.10).
  - `specs/handler-contract.md`: insert HC-063 `shell` handler (§III.1); HC-005 in-process note (§III.2); HC-006a `agent-task.md` Body-from-prompt note (§III.3); HC-058 tool-style `failure_class` note (§III.4); §4.2a SUCCESS-without-commit note (§III.5).
- **Spec sections:** SPEC.md Parts I/II/III/V (every section). Source-of-truth for IDs: `06-integration.md` §1.2.
- **Deliverables:** edited `specs/workflow-graph.md`, `specs/execution-model.md`, `specs/handler-contract.md`. NO `event-model.md` change (Part IV decision).
- **Acceptance:** `grep -c "WG-039\|WG-040\|WG-041\|WG-042\|WG-043\|WG-044\|WG-045\|WG-046" specs/workflow-graph.md` ≥ 8; `grep "HC-063" specs/handler-contract.md` matches; EM-058 non-agentic row contains the `tool_command`/`handler_ref="shell"` split text; the reserved-set sentence in WG-031 lists all of `tool_command, timeout, prompt, non_committing, model, effort, goal` and does NOT list `class`/`model_stylesheet`; spec ID-lint / cross-ref check passes (no dangling WG-/HC-/EM- ref).
- **Depends on:** none. **MUST precede or accompany** every code task (spec-first).

### T1 — Tool node: `dispatchDotToolNode` + built-in `shell` handler (KEYSTONE)

- **What:** Split the `dot_cascade.go:198-203` non-agentic branch on `tool_command` presence.
  - Parse `tool_command` (string) and reuse `timeout` (int seconds, default 300) into `dot.Node` (new fields) + `core.Node` (`ToolCommand *string`; `timeout` already exists, REUSE `Node.Timeout`). Add `tool_command`/`timeout` to the parser node-attr switch (`parser.go:601+`) and the reserved node-attr handling; per WG-024, `timeout` must be non-negative integer (strict error otherwise), and `tool_command` on a node whose `handler_ref != "shell"` is a v1 WARNING (retain + emit WG-031 warning), reserved-strict at next major.
  - Implement the built-in `shell` handler: when `node.Type==non-agentic && tool_command present && handler_ref=="shell"`, execute `/bin/sh -c <tool_command>` with `cwd = wtPath` and the daemon handler env; kill on `timeout` (default 300s). May run IN-PROCESS (no subprocess/socket/NDJSON/agent_ready/heartbeat per HC-063). Map exit state → Outcome: exit 0 → `SUCCESS`; non-zero → `FAIL`+`failure_class=deterministic`; timeout-kill → `FAIL`+`transient`; signal-kill (ctx-cancel/stop) → `FAIL`+`canceled`. `kind=default`, no payload, no RETRY/PARTIAL.
  - When the non-agentic node has NO `tool_command` → preserve today's `Outcome{SUCCESS}` synth (path 2). A non-agentic node bound to a non-`shell` handler → path 3 (invoke handler_ref) — out-of-scope stub OK at v1 (no live non-shell non-agentic handler), but the branch structure must exist.
  - Default axis_tags for shell: `io-determinism=non-deterministic`, `replay-safety=unsafe` (author may tighten via `axis_tags`).
  - Reuse `node_dispatch_requested` (already emitted at `:191-193`); NO new event (Part IV).
- **Spec sections:** WG-039, WG-024 bullet (§I.10), HC-063 (§III.1), HC-005 note (§III.2), HC-058 note (§III.4), EM-057 item 7 (§II.7), EM-058 non-agentic row + sub-note (§II.8).
- **Deliverables:** `dot.Node`+`core.Node` field additions; parser/validator changes; `dispatchDotToolNode` (new) in `dot_cascade.go` + the split in the `NodeTypeNonAgentic` case; `specs/examples/<tool-node>.dot` canonical example; unit tests for the exit-state→Outcome map.
- **Acceptance:** a `.dot` with a `non-agentic` `tool_command="exit 0"` node returns SUCCESS; `tool_command="exit 3"` returns FAIL+`deterministic`; a sleeping command past `timeout` returns FAIL+`transient`; ctx-cancel → FAIL+`canceled`. A non-agentic node with no `tool_command` still synths SUCCESS (no regression — existing dot smoke graphs pass). `tool_command` with `handler_ref != shell` → load warning, run still starts. Negative/non-int `timeout` → ingest strict error.
- **Depends on:** T0 (spec landed). Foundation for T2 (scenario) and the example beads `hk-o52fm.*`.

### T2 — Inline `prompt` (B) + role-surfacing (precursor; B-before-C order)

- **What:** Thread a per-node brief into `dispatchDotAgenticNode` BEFORE launch (pre-launch region of the function, before `claudeRunCtx` is built at `:400`).
  - **Role surfacing (cheap precursor, hk-m5lmo):** `role=` currently lands in `dot.Node.UnknownAttrs["role"]` (NOT a struct field; never read). v0 fix: read the node's `role` and append a short role descriptor to the agent brief (a one-line suffix to taskBody / via a `claudeRunCtx` field). This gives reviewer-node differentiation immediately.
  - **Inline prompt (hk-sdnzj):** add `prompt` (string) to `dot.Node`+`core.Node` and the parser reserved node-attr set (`agentic`-only; on non-agentic/gate → v1 WARNING, retained, ignored). For an **implementer-class** agentic node with `prompt` present, REPLACE the bead-derived `taskBody` (`claudelaunchspec.go:246`) with `prompt` verbatim; bead Title + ID stay in the header. **Reviewer-class** `prompt` is accepted-but-inert at v1 (retain, ignore; reviewer brief stays review-target-sourced). Input-only: Outcome/cascade untouched.
- **Spec sections:** WG-040 (§I.3), HC-006a note (§III.3), B↔E composition §II.10 (prompt → `Body`/`## Task Description` channel).
- **Deliverables:** `dot.Node`/`core.Node` `prompt` field + `Role`/role-read; parser changes; `dispatchDotAgenticNode` pre-launch threading via `claudeRunCtx`; unit test that `prompt` overrides `taskBody` for implementer, is inert for reviewer, and that `role` appends to the brief.
- **Acceptance:** an implementer node with `prompt="X"` launches with taskBody = "X" (not bead body); a reviewer node with `prompt` launches with the review-target brief unchanged; a node with `role="design&idioms"` has that descriptor visible in its rendered `agent-task.md`. No double-injection with goal (T5).
- **Depends on:** T0. **CO-EDITS `dispatchDotAgenticNode` with T3 + T4 — see sequencing.** Touches the pre-launch region (distinct from C's post-launch region).

### T3 — Non-committing agentic node (C)

- **What:** Gate the HEAD-advance failure check at `dot_cascade.go:585-591` on `node.NonCommitting`, `dot` mode only.
  - Add `non_committing` (boolean, default false) to `dot.Node`+`core.Node` and the parser reserved node-attr set (`agentic`-only; on reviewer/non-agentic/gate → v1 WARNING, retained, ignored).
  - In the implementer-class post-launch branch: if `node.NonCommitting` is true, a clean exit yields `Outcome{SUCCESS}` WITHOUT requiring `postHeadSHA != preHeadSHA`. If false (default), today's behavior (no-advance → node failure). **Both modes:** an unresolvable HEAD (`resolveWorktreeHEAD` error at `:585-587`) remains a daemon-side error (broken worktree is a real failure).
  - `dot`-mode-only: the `review-loop` implementer-MUST-commit path (EM-015d) is untouched — v69 must not regress.
  - REJECT `auto_status` as a node attribute (it is NOT in the reserved set; but add an explicit ingest error/warning so an author porting `auto_status=true` gets a clear "use non_committing" message — see T7 deferred item).
- **Spec sections:** WG-041 (§I.4), EM-015d carve-out (§II.2), EM-058 non-committing sub-note (§II.8), HC §4.2a note (§III.5).
- **Deliverables:** `dot.Node`/`core.Node` `non_committing` field; parser changes; the gated check in `dispatchDotAgenticNode`; unit test: non_committing implementer clean-exit-no-commit → SUCCESS; default implementer no-commit → failure; review-loop path unchanged; broken-HEAD → error in both modes.
- **Acceptance:** a `dot` agentic node `non_committing="true"` that exits clean without a commit returns SUCCESS and the cascade continues; the same node without the attr fails; a `review-loop` run still requires the implementer commit (regression guard).
- **Depends on:** T0. **CO-EDITS `dispatchDotAgenticNode` with T2 + T4** (C touches the post-launch HEAD region; B/D touch pre-launch — non-overlapping lines but same function).

### T4 — Per-node `model`/`effort` (D, tier-0 override)

- **What:** Add tier-0 per-node override in `dispatchDotAgenticNode` pre-launch.
  - Add `model` (string) and `effort` (`EffortLevel`) to `dot.Node`+`core.Node` and the parser reserved node-attr set (`agentic`-only; on non-agentic/gate/sub-workflow/edge/graph → reserved-out-of-position STRICT error per WG-031). `model` shape-validated (`^[A-Za-z0-9._:/-]+$`, ≤128 chars, non-empty when present); `effort` must be in closed enum `{low,medium,high,xhigh,max}` — out-of-enum is an **ingest-time STRICT error** (graph is static; stricter than tier-1 bead-label runtime relaxation).
  - In `dispatchDotAgenticNode`, when building `claudeRunCtx` (`:400-419`), if the node carries `model`/`effort`, substitute the node's value into `rc.model`/`rc.effort` for THAT node only (override the run-level `resolvedModel`/`resolvedEffort` params). Independent: only-model inherits run-level effort, vice versa. NOT a second resolution walk — static graph data layered at dispatch. Run-level `ModelPreference` seal in the Run record is unchanged (replay-safe).
- **Spec sections:** WG-042 (§I.5), WG-043 informative (§I.6 — `class`/`model_stylesheet` permissive, NOT reserved, never dispatched on), EM-012b reword + EM-012b-NODE (§II.1), Node-RECORD `model`/`effort` (§II.3).
- **Deliverables:** `dot.Node`/`core.Node` `model`/`effort` fields + ingest validation; parser changes; the per-node override in `dispatchDotAgenticNode`; unit test: node with `model=opus,effort=high` launches with that pair; sibling with neither uses run-level default; out-of-enum effort → ingest error; model on non-agentic → reserved-out-of-position error; `class=`/`model_stylesheet=` parse permissively (warned, retained, never dispatched).
- **Acceptance:** matches scenario bead `hk-mca0b` and exploratory bead `hk-xp9j7`.
- **Depends on:** T0. **CO-EDITS `dispatchDotAgenticNode` pre-launch with T2** (both set `claudeRunCtx` fields).

### T5 — Graph-level `goal` + template-param substitution (E)

- **What:** Two coupled mechanisms (one component, can land together):
  - **`goal` (WG-044):** add `goal` to `reservedGraphAttrs` (`parser.go:509`) + the graph-attr switch (`:534-562`) → typed `dot.Graph.Goal` + `core.Workflow.Goal`. A `goal` on a node/edge → reserved-out-of-position strict error. Thread the (substituted) `Graph.Goal` into every agentic node's brief via the run-level **ExtraContext channel** (`rc.extraContext` / `AgentTaskPayload.ExtraContext` → `## Extra Context`) — composes with bead body and per-node `prompt` (T2); does NOT double-inject (distinct channel, confirmed in `agenttask_chb028.go`).
  - **Template params (WG-045/WG-046):** accept repeated `--param KEY=VALUE` on `harmonik run` → `map[string]string`, sealed into Run-record `template_params` at claim time. Apply ONE substitution pass over the RAW `.dot` source string (`__[A-Z][A-Z0-9_]*__` grammar) BEFORE parse — i.e. before `dot.Parse` inside / ahead of `LoadDotWorkflow` (`loader.go:56`; daemon call site `workloop.go:1277`). After the pass, any residual `__TOKEN__` is a launch-time ERROR (run refuses to start, names the offending token). Non-recursive; param values are trusted/un-sanitized. Token-free source → no-op. Ordering invariant (WG-046): read → substitute → parse → validate → dispatch.
- **Spec sections:** WG-044/045/046 (§I.7-I.9), Workflow-RECORD `goal` (§II.4), Run-RECORD `template_params` (§II.5), §7.5 launch surface (§II.9), B↔E composition (§II.10).
- **Deliverables:** `dot.Graph.Goal`/`core.Workflow.Goal`; parser graph-attr change; `--param` CLI flag on `harmonik run` (+ queue-item launch context); the pre-parse substitution + residual-token check (a new function ahead of `LoadDotWorkflow`, or a `LoadDotWorkflowWithParams` variant); Run-record `template_params` seal; goal→ExtraContext threading in the dot driver; unit tests for substitution/residual/no-op/ordering.
- **Acceptance:** matches scenario bead `hk-4bn9o` and exploratory bead `hk-9ohjf`: a `.dot` with `goal="Fix #__ISSUE_NUMBER__"` + `--param ISSUE_NUMBER=172` substitutes before parse, threads the substituted goal into the brief; a missing param → launch-time residual-token error; token-free `.dot` → byte-identical no-op; replay re-substitutes identically.
- **Depends on:** T0. Independent of T1-T4 except B↔E composition (the ExtraContext-vs-Body channel split must agree with T2 — coordinate the rendered-section contract, no line conflict).

### T6 — Scenario + exploratory tests (gate; required before Ready)

- **T6a — Tool-node scenario:** end-to-end `harmonik run --beads <id>` against `specs/examples/<tool-node>.dot` whose tool node runs a real command; assert exit-code→Outcome→cascade routing + terminal. Twin or real-claude substrate. **NEW bead** (title `scenario: attractor-parity — tool-node exit-code→outcome→cascade`). Depends on T1.
- **T6b — Per-node model/effort scenario:** `hk-mca0b` (EXISTS). Depends on T4.
- **T6c — Per-node model/effort exploratory:** `hk-xp9j7` (EXISTS). Depends on T4.
- **T6d — goal+param scenario:** `hk-4bn9o` (EXISTS). Depends on T5.
- **T6e — goal+param exploratory:** `hk-9ohjf` (EXISTS). Depends on T5.
- **T6f — Substitution × prompt/tool_command/goal ordering conformance test:** a `__PARAM__` token inside EACH of `prompt` (T2), `tool_command` (T1), and `goal` (T5) is substituted by the single pre-parse pass (WG-046). **NEW bead** (title `scenario: attractor-parity — param substitution covers prompt/tool_command/goal`). Depends on T1, T2, T5.
- **T6g — Inline-prompt / non-committing scenario:** end-to-end multi-node `dot` run where an implementer node carries `prompt`, an investigate node carries `non_committing="true"` paired with a downstream validating tool node; assert the brief override, the no-commit SUCCESS, and the validating tool node gates routing. **NEW bead** (title `scenario: attractor-parity — inline-prompt + non-committing + validating tool node`). Depends on T1, T2, T3.
- **Spec sections:** all of the above.
- **Acceptance:** all listed test beads closed; neither this work nor any impl bead closes until the test beads close.
- **Depends on:** the impl tasks each validates (per-test above).

### T7 — Authoring guidance + deferred items (non-normative + follow-ups)

- **What:**
  - **Example sidecar / authoring notes** (`specs/examples/` README or per-example `.md`): the `auto_status → non_committing` porting alias + "harmonik does NOT accept `auto_status` at v1"; the "pair every `non_committing` node with a downstream validating tool node" obligation; reviewer-node `prompt` accepted-but-inert; the `model_stylesheet`/`class` → direct `model=` port note. (NON-normative; integration §12.)
  - **`auto_status` alias rejection** (impl, small): ingest emits a clear error/warning when `auto_status` appears, pointing to `non_committing`. Pairs with T3.
  - **v2 follow-up beads (FILE, do not implement):** (1) author-declared per-tool-node `transient_exit_codes`; (2) real `auto_status` work-product/marker status-derivation feature; (3) normative `model_stylesheet` selector mechanism for >2 model tiers; (4) optional `tool_command_completed` observability event if operator demand surfaces.
- **Spec sections:** SPEC §I.4 (`auto_status` reserved), §I.6 (WG-043 port note), `06-integration.md` §12.
- **Deliverables:** authoring-notes file(s); the `auto_status` ingest rejection (in T3's parser change or a tiny standalone); four new v2 beads filed.
- **Depends on:** T3 (auto_status rejection); T0 (port notes cite landed IDs).

---

## Bead Mapping

| Task | Bead | Action | Refinements to add |
|---|---|---|---|
| T0 | **NEW** `attractor-parity: land SPEC.md into specs/ (workflow-graph, execution-model, handler-contract)` | file P1 | type=task; label `codename:attractor-parity`. Acceptance = the grep/ID-lint checks in T0. First in the DAG. |
| T1 | `hk-l8rpd` (tool node) | REFINE | Add: `dispatchDotToolNode` splits `dot_cascade.go:198-203` on `tool_command`; `handler_ref="shell"`; REUSE `Node.Timeout` (default 300); exit-state→Outcome map (0/non-zero/timeout/signal → SUCCESS/deterministic/transient/canceled); MAY run in-process (HC-063); `specs/examples/` tool-node example. Cite WG-039/HC-063/EM-058. Bump P2→**P1**. |
| T2 | `hk-sdnzj` (inline prompt) + `hk-m5lmo` (role) | REFINE both | `hk-m5lmo`: clarify `role=` lands in `UnknownAttrs["role"]` today (not a struct field); v0 = read it + append to brief. Already P1. `hk-sdnzj`: prompt REPLACES taskBody (`claudelaunchspec.go:246`) for implementer; reviewer inert; cite WG-040/HC-006a. Bump P2→**P1**. **Sequence m5lmo before/with sdnzj** (same region). |
| T3 | `hk-69asi` (non-committing) | REFINE | Gate `dot_cascade.go:585-591` HEAD-check on `node.NonCommitting`, dot-only; broken-HEAD still errors both modes; review-loop untouched (v69 guard); reject `auto_status`. Cite WG-041/EM-015d. Bump P2→**P1**. |
| T4 | `hk-q8nqr` (model/effort) | REFINE | Reframe from "stylesheet" to tier-0 per-node `model`/`effort` direct attrs (WG-042); `class`/`model_stylesheet` informative-only (WG-043, permissive, never dispatched); effort out-of-enum = ingest strict error; cite EM-012b-NODE. Bump P3→**P1** (user wants the cluster). |
| T5 | **NEW** `attractor-parity: graph goal + --param template substitution (WG-044/045/046)` | file P1 | type=feature; label `codename:attractor-parity`, `dot`. No existing bead covers goal/params impl (only the test beads hk-4bn9o/hk-9ohjf). Acceptance = T5 + hk-4bn9o/hk-9ohjf. |
| T6a | **NEW** `scenario: attractor-parity — tool-node exit-code→outcome→cascade` | file P2 | label `codename:attractor-parity`,`scenario-test`. dep T1. |
| T6b | `hk-mca0b` | EXISTS | dep T4. add `codename:` label (has it). |
| T6c | `hk-xp9j7` | EXISTS | dep T4. |
| T6d | `hk-4bn9o` | EXISTS | dep T5. (Refs cite WG-039/040/041 — UPDATE to WG-044/045/046 per integration remap.) |
| T6e | `hk-9ohjf` | EXISTS | dep T5. (Same WG-ref remap to WG-044/045/046.) |
| T6f | **NEW** `scenario: attractor-parity — param substitution covers prompt/tool_command/goal` | file P2 | scenario-test. dep T1,T2,T5. |
| T6g | **NEW** `scenario: attractor-parity — inline-prompt + non-committing + validating tool node` | file P2 | scenario-test. dep T1,T2,T3. |
| T7 | **NEW** `attractor-parity: authoring sidecar + auto_status rejection` (P2) + 4 v2 follow-up beads (P3/backlog) | file | auto_status rejection pairs with T3; v2 beads = transient_exit_codes, real auto_status, model_stylesheet selector, tool_command_completed event. |

**WG-ref correction for `hk-4bn9o` / `hk-9ohjf`:** both currently cite WG-039/040/041 (the pre-integration provisional IDs for component E). The integration pass remapped E to **WG-044/045/046**. The orchestrator should fix these refs when refining the beads.

---

## Dependency Graph (DAG)

```
T0 (spec land, P1) ──┬─→ T1 (tool node, hk-l8rpd) ──┬─→ T6a (tool scenario)
                     │                               ├─→ T6f (param×3 ordering)  ← also T2,T5
                     │                               └─→ T6g (prompt+noncommit)  ← also T2,T3
                     ├─→ T2 (prompt+role, hk-sdnzj/m5lmo) ─┬─→ T6f, T6g
                     ├─→ T3 (non-committing, hk-69asi) ────→ T6g
                     ├─→ T4 (model/effort, hk-q8nqr) ──────┬─→ T6b (hk-mca0b scenario)
                     │                                     └─→ T6c (hk-xp9j7 explore)
                     ├─→ T5 (goal+params, NEW) ────────────┬─→ T6d (hk-4bn9o scenario)
                     │                                     ├─→ T6e (hk-9ohjf explore)
                     │                                     └─→ T6f
                     └─→ T7 (authoring/auto_status) ←also T3
```

Valid DAG, no cycles. Every changelog/SPEC entry has an implementing task; every test bead depends on the impl task(s) it validates.

---

## Parallelization Plan

**T0 first, alone.** Spec-text landing precedes (or accompanies as the first commit of) all code. Cheap; do it as one bead so the landed IDs are stable for the rest.

**After T0, the critical co-edit constraint:** T2 (inline-prompt + role), T3 (non-committing), and T4 (per-node model/effort) ALL edit `internal/daemon/dispatchDotAgenticNode`. T2 and T4 edit the **pre-launch** region (building `claudeRunCtx`, `:400-419` + the `taskBody` source at `claudelaunchspec.go:246`); T3 edits the **post-launch** HEAD-advance region (`:585-591`). They also all add fields to the SAME `dot.Node` + `core.Node` structs and the SAME parser node-attr switch.

> **RECOMMENDATION: SERIALIZE T2 → T4 → T3 in one harmonik group (or one combined bead-chain), NOT concurrent.** Running them as separate concurrent `harmonik run` slots will merge-conflict on `dot.Node`/`core.Node`/`parser.go`/`dot_cascade.go` every time. Two viable shapes:
> - **(A, preferred) one sequential group** `[T2, T4, T3]` — each lands on `main` before the next dispatches (stream-mode head-of-line, default). Lowest conflict risk; matches the B-before-C ordering note (integration §"Implementation sequencing").
> - **(B) one combined "agentic-node attrs" bead** that lands prompt+role+model+effort+non_committing in a single commit. Coarser, but zero intra-cluster conflict. Acceptable since they share the struct/parser surface anyway.

**Concurrent-safe (separate files, no co-edit) — dispatch in parallel:**
- **T1 (tool node)** — touches the `NodeTypeNonAgentic` branch + a NEW `dispatchDotToolNode`, NOT the agentic function. Conflicts with T2/T3/T4 only on the shared `dot.Node`/`core.Node`/`parser.go` field additions — minor, additive, low-conflict (different struct fields/switch cases). Can run concurrently with the agentic-cluster IF struct/parser edits are additive append-only; safest to land T1 first (it's the keystone) then the agentic cluster.
- **T5 (goal+params)** — touches `parser.go` graph-attr switch (`:509`,`:534`), `loader.go`, `harmonik run` CLI, Run-record — NOT `dispatchDotAgenticNode`, NOT the non-agentic branch. The only coupling is the ExtraContext-vs-Body channel contract with T2 (a read-only agreement, no shared lines). **T5 can run fully concurrent with T1 and the agentic cluster.**

**Suggested wave shape:**
1. **Wave 0:** T0 (spec land) — solo, P1.
2. **Wave 1 (concurrent):** T1 (keystone tool node) ‖ T5 (goal+params). Both independent of the agentic cluster.
3. **Wave 2 (serial chain):** T2 → T4 → T3 (the `dispatchDotAgenticNode` co-edit cluster). Start after Wave 1 lands so struct/parser additions don't race T1.
4. **Wave 3 (concurrent):** all T6 test beads, each gated on its impl task; T7 authoring + auto_status (after T3) + file the 4 v2 beads.

**P1-bump set (user wants this cluster implemented):** T0 (new spec-land bead), `hk-l8rpd` (T1), `hk-sdnzj` (T2), `hk-69asi` (T3), `hk-q8nqr` (T4, was P3), the NEW goal+params bead (T5). `hk-m5lmo` is already P1. Test beads stay P2.
