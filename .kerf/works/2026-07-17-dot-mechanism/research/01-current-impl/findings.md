# DOT Mechanism — Current Implementation Map (as of 2026-07-17)

Primary reference for how the GraphViz `.dot` workflow mechanism (implement→review→feedback loop) works TODAY. All citations are `file:line` against `/Users/gb/github/harmonik`.

---

## 0. Top-level shape

A DOT workflow is a `digraph` describing a state machine. The daemon:
1. resolves a `.dot` path (queue ref, `<projectDir>/workflow.dot`, or embedded `standard-bead.dot`),
2. reads → parses (with `__TOKEN__` placeholders intact) → substitutes template params per-attribute → validates → hands the typed graph to the cascade driver,
3. walks nodes from `start_node`, dispatching each node by type (non-agentic/tool, agentic implementer, agentic reviewer, gate, sub-workflow),
4. after each agentic node produces an `Outcome`, runs the EM-041 edge cascade to pick the next node,
5. terminates when a node in `terminal_node_ids` is reached (or on failure/cap-hit).

Entry point in the daemon workloop: `internal/daemon/workloop.go:4031` (`case core.WorkflowModeDot:`) → `driveDotWorkflow` (`internal/daemon/dot_cascade.go`).

---

## 1. Graph parsing & execution

### Packages
- **`internal/workflow/dot/`** — the DOT parser + AST + validator (the language layer).
  - `ast.go` — typed AST (`Graph`, `Node`, `Edge`, `Condition`).
  - `parser.go` (36 KB) — `dot.Parse(src, path)`.
  - `validator.go` — `dot.Validate(graph) []Diagnostic`.
  - `edges.go` — `dot.EvalCondition`.
- **`internal/workflow/`** — the loader + param substitution + cascade bridge.
  - `loader.go` — `LoadDotWorkflow`, `LoadDotWorkflowWithParams`, `LoadDotWorkflowFromBytes`, `LoadDotWorkflowWithPolicy`.
  - `params.go` / `params_graph.go` — template-param substitution (WG-045/046).
  - `dispatcher.go` — `DecideNextNode` (edge cascade bridge to `core.SelectNextEdge`).
- **`internal/daemon/dot_cascade.go`** — `driveDotWorkflow`: the runtime driver that actually dispatches nodes into the substrate.

### Load pipeline
`LoadDotWorkflowWithParams(dotPath, params)` — `internal/workflow/loader.go:135`:
```
read source → dot.Parse(template, tokens intact) → substituteGraphParams(graph, params) → dot.Validate → return graph
```
The ordering is a security invariant (WG-046): parse happens BEFORE substitution so a param value can never alter graph shape (`loader.go:144-176`). Validation errors of `SeverityError` become `*ErrWorkflowLoad` (`loader.go:178-190`), which the daemon maps to `failure_class=workflow_load` and reopens the bead (`workloop.go:4059-4067`).

### Node attributes (the closed catalog) — `internal/workflow/dot/ast.go:104-255`
The `Node` struct is the authoritative attribute list. Key fields:

| DOT attribute | Go field | Notes |
|---|---|---|
| `type` | `Type` (`core.NodeType`) | closed enum: `agentic`, `non-agentic`, `gate`, `sub-workflow` (WG-001) |
| `agent_type` | `AgentType` | open set; `implementer` / `reviewer` etc. (WG-003) |
| `handler_ref` | `HandlerRef` | e.g. `claude-implementer`, `claude-reviewer`, `noop`, `shell` |
| `gate_ref` | `GateRef` | required on gate nodes |
| `sub_workflow_ref` / `workflow_version` / `input_mapping` | `SubWorkflowRef` … | sub-workflow nodes |
| `idempotency_class` | `IdempotencyClass` | required on agentic/non-agentic |
| `role` | `Role` | free-text persona, surfaced into brief (WG-040) |
| `prompt` | `Prompt` | inline LLM prompt; REPLACES bead body for implementers (WG-040 §I.3) |
| `model` | `Model` | per-node model override (WG-042); regex `^[A-Za-z0-9._:/-]+$`, ≤128 |
| `effort` | `Effort` | per-node effort override; enum {low,medium,high,xhigh,max} |
| `non_committing` | `NonCommitting` | clean exit = SUCCESS w/o HEAD advance (WG-041) |
| `auto_status` | `AutoStatus` | post-exit deterministic work-product inspection (WG-053) |
| `tool_command` | `ToolCommand` | shell command for `handler_ref="shell"` non-agentic nodes (WG-039) |
| `timeout` | `Timeout` | integer seconds, default 300 |
| `harness` / `agent_runtime` | `Harness` / `AgentRuntime` | per-node harness override (claude/pi/codex) |
| `reviewer_harness` | `ReviewerHarness` | per-node harness for the reviewer spawned in review-loop |
| `skills_ref` / `freedom_profile_ref` / `hook_ref` / `guard_ref` / `budget_ref` / `axis_tags` | typed *_ref fields | optional |
| anything else | `UnknownAttrs` map | retained but "dispatcher MUST NOT read" (WG-032) |

Graph-level attributes (`ast.go:34-97`): `schema_version`, `version`, `start_node`, `terminal_node_ids` (comma/space list), `context_keys`, `workflow_class`, `no_progress_guard` (`""`/`strict`/`capped:N`/`off`), and **`goal`** (WG-044 — threaded into every agentic node's brief).

### Node-type dispatch switch — `internal/daemon/dot_cascade.go:430`
```
switch node.Type {
  case NonAgentic:  → noop OR dispatchDotToolNode (if tool_command/shell)   (line 431)
  case Agentic:     → dispatchDotAgenticNode (implementer OR reviewer)       (line 494)
  case Gate:        → dot_gate.go path (cognition gate)                      (line 1009)
  case SubWorkflow: → sub_workflow_runner.go                                 (line 1028)
  default:          → unknown-type failure                                   (line 1070)
}
```
`nodeIsReviewer(node)` (`dot_cascade.go:2353`) returns true when `agent_type == "reviewer"` OR `handler_ref == "claude-reviewer"` — this is what forks implementer vs reviewer behavior.

---

## 2. Node dispatch — what each role sees on disk

Both roles get a brief written to a file inside their **own worktree's** `.harmonik/` dir, then the harness delivers a pointer/kick-off. Content builders live in `internal/workspace/agenttask_chb028.go`.

### Implementer → `.harmonik/agent-task.md`
Written by `WriteAgentTask` (`agenttask_chb028.go:205`), content built by `buildAgentTaskContent` (`:271`). Atomic write (temp+rename+fsync). Sections in order:
1. Header key/values: `bead_id`, `title`, `phase`, `iteration`, `run_id`, `workspace_path`, optional `base_branch` (`:274-283`).
2. **Worktree Discipline** block — "all paths must stay in your worktree" (`:293-305`).
3. **Bead Lifecycle** prohibition — never run `br close` (`:312-316`).
4. **Task Description** — `p.Body` verbatim (`:318-322`). Body is the bead body OR, on a DOT node with a `prompt=` attribute, the node's prompt REPLACES the body (`claudelaunchspec.go:308-311`).
5. Optional **Extra Context** — carries the workflow `goal` and node `role` (`:327-333`).
6. Phase-specific:
   - `implementer-resume`: **Prior-Iteration Context** naming the `reviewer-feedback` file path + optional `prior-verdict-summary` (`:337-353`).
   - `reviewer`: reviewer read-only constraint + `review_base_sha`/`review_head_sha` (`:355-363`).
7. **Session Completion** — "you MUST run `/quit`" (`:378-382`); this is what fires the Stop hook that unblocks the daemon workloop.

### Reviewer → `.harmonik/review-target.md`
Written by `WriteReviewTarget` (`agenttask_chb028.go:558`), content by `buildReviewTargetContent` (`:577`). Sections:
1. Header `# Review target — bead <id>, iteration <n>`.
2. **Reviewer Constraint** (read-only; `renderReviewerConstraint` at `:397`) — MUST NOT mutate git state; produce verdict via `harmonik write-review-verdict --verdict=… --notes=… --flags=…`; DO NOT hand-write `.harmonik/review.json`.
3. **Coverage Check** ("all-X" completeness grep instruction; `:588-600`).
4. **Spec Field-Name Check** (`:604-608`).
5. **Bead** id/title/body (`:610-616`).
6. **Diff range** `base:`/`head:` SHAs (`:618-620`).
7. **Prior verdicts** (omitted at iteration 1; else per-iter verdict file path + summary; `:623-637`).
8. **Hints** (operator reviewer-tier hints, if any; `:640-646`).

The reviewer's brief is written *before* launch inside `dispatchDotAgenticNode` (`dot_cascade.go:1257-1286`): it first removes any stale `review.json`, resolves HEAD, and writes `review-target.md` via the runner (local or remote/SSH).

### Reviewer verdict output → `.harmonik/review.json`
The reviewer writes it via the `harmonik write-review-verdict` CLI (atomic). The daemon reads it back with `readDotReviewVerdictRetry` (`dot_cascade.go:1202`), retrying on malformed JSON. Verdict → `outcome.preferred_label` (APPROVE / REQUEST_CHANGES / BLOCK).

---

## 3. Edge conditions & verdict routing

### Cascade bridge — `internal/workflow/dispatcher.go:87` `DecideNextNode`
After a node produces an `Outcome`, the driver calls `DecideNextNode(graph, fromNodeID, outcome, run, cycles)`:
1. If `fromNodeID ∈ graph.TerminalNodeIDs` → `IsTerminal` (`dispatcher.go:95-106`).
2. Collect outgoing edges from `fromNodeID` (`:109-114`).
3. Build a condition evaluator bridging `dot.EvalCondition` into `core.ConditionEvaluator`, keyed by the edge's raw condition string (`:123-144`).
4. Convert `dot.Edge`→`core.Edge` (`dotEdgeToCoreEdge`, `:199`) and call `core.SelectNextEdge(run, candidates, outcome, eval, cycles)` (`:154`) — the shared EM-041 five-step cascade.
5. cap-hit → `FailureClassCompilationLoop`, `completion_reason="cap_hit"` (`:156-176`).

### Edge condition dialect — restricted equality (WG-013)
`Condition` = conjunction (`&&`) of `Equality{LHS, Op ("==" / "!="), RHS}` (`ast.go:295-311`). LHS restricted to a whitelist (WG-014); canonical verdict-routing LHS is `outcome.preferred_label`. NO `<`/`>`. Example from `specs/examples/review-loop.dot`:
```
reviewer -> close                  [condition="outcome.preferred_label == 'APPROVE'"];
reviewer -> implementer            [condition="outcome.preferred_label == 'REQUEST_CHANGES'", traversal_cap="3"];
reviewer -> "close-needs-attention"[condition="outcome.preferred_label == 'BLOCK'"];
reviewer -> "close-needs-attention";   // unconditional fallback (REQUIRED, must be last)
```

### Verdict → next edge
The reviewer verdict lands in `outcome.PreferredLabel` (`dot_cascade.go:954,990`). The cascade evaluates conditional edges in declaration order; first satisfied wins; an unconditional fallback edge catches everything else (WG-011). APPROVE routes to terminal `close`; REQUEST_CHANGES routes the **back-edge** to the implementer.

### The back-edge (re-implement)
The REQUEST_CHANGES edge points back to the implementer node and carries `traversal_cap="3"`. `dotEdgeToCoreEdge` reads `traversal_cap` from `UnknownAttrs` (`dispatcher.go:230-234`) into `core.Edge.TraversalCap`, so `core.SelectNextEdge` enforces the EM-043 iteration cap. On cap-hit the cascade returns `Failed` (not the fallback) and the run reopens as needs-attention. When the driver re-enters the implementer node at iteration ≥ 2, it selects `ReviewLoopPhaseImplementerResume` and resumes the prior session (`dot_cascade.go:1288-1303`).

---

## 4. Feedback delivery (back-edge → implementer)

Three harness paths, historically divergent (a known fragility area — c073/WS4-4 product-defect finding):

### The feedback file (shared)
`WriteReviewerFeedback` (`agenttask_chb028.go:443`) writes `.harmonik/reviewer-feedback.iter-<N-1>.md` (`buildReviewerFeedbackContent`, `:458`): verdict, flags, notes, diff summary. Daemon MUST write it before launching the resume turn.

### claude/tmux path
`pasteInjectImplementerResume` in `internal/daemon/pasteinject.go` delivers a paste-inject kick-off into the TUI pane pointing at the feedback file; the resume-turn brief is the rewritten `agent-task.md` with `phase=implementer-resume` and the **Prior-Iteration Context** section naming the `reviewer-feedback` path (`agenttask_chb028.go:337-353`).

### pi / codex path
No TUI/paste path — task delivered as a positional seed-prompt argv. `implementerResumeSeedPrompt(beadID, priorIteration)` (`internal/daemon/agentseedprompt.go:44`) builds a resume seed (`implementerResumeSeedTemplate`, `:33`) that: tells the agent it's resuming after a review; instructs it to FIRST read `.harmonik/reviewer-feedback.iter-<N-1>.md` and address every point; REQUIRES a new commit carrying the `Refs: <beadID>` trailer (else no-progress fail → loop); degrades gracefully when the file is absent. It exists because the INITIAL pi/codex seed never referenced the feedback file, so a resumed implementer got the identical prompt and produced no commit (`agentseedprompt.go:1-22`). It is the generic peer of `pasteInjectImplementerResume` so the two harness families stay aligned.

---

## 5. Model selection — the load-bearing detail

### Run-level model: the EM-012b 4-tier walk — `internal/daemon/modelpreference.go:190` `ResolveModelPreference`
Called once per run at claim time (`workloop.go:3274`). `model` and `effort` resolve **independently**, each walking:
- **Tier 1** — per-bead labels `model:<alias>` / `effort:<level>` (`modelpreference.go:212-232`). **Exactly one** label wins; multiple → conflict, treated as absent + `bead_label_conflict` event. → **The only way an individual bead overrides the model today.**
- **Tier 2** — per-project `.harmonik/config.yaml` (`projectCfg.LookupAgent(agentType)`, `:236`). Loaded once at daemon start; needs restart to reload.
- **Tier 2.5** — env vars `HARMONIK_CLAUDE_MODEL` / `HARMONIK_CLAUDE_EFFORT`, read at call time (hot-reload), invalid silently skipped (`:241-247`).
- **Tier 3** — compiled per-agent-type defaults `defaultModelEntries` (`:156-159`): `claude-code → {sonnet, medium}`; `claude-twin → {"",""}`. **Hardcoded in Go.**
- **Tier 4** — empty → harness applies its own tool default.

Agent-type resolved first via `resolveHarnessAgentTypeQuiet` (`workloop.go:3268`) so a pi/codex run isn't sealed with a claude default (hk-pkugu/pi-model-leak).

### Per-node model/effort override — `internal/daemon/dot_cascade.go:1318-1355`
Inside `dispatchDotAgenticNode`, run-level `resolvedModel`/`resolvedEffort` are the start, then the DOT node's `model=`/`effort=` are layered on (WG-042 / EM-012b-NODE). **Static graph data layered at dispatch, NOT a second resolution walk**:
- `effort=`: applied unconditionally if non-empty (`:1352-1355`) — harness-agnostic.
- `model=`: applied ONLY when the node's **effective harness is claude-code family** (`nodeModelForHarness`, `:1182`). For a pi/codex node the `model=` pin is IGNORED and run-level model is used (a claude model name is meaningless to the pi/codex provider; hk-lfrub, `:1174-1187`).
- Effective-harness precedence for scoping: `reviewer_harness override > node harness= pin > run-level resolved harness` (`:1339-1350`).

### Where the flag gets emitted
`buildClaudeLaunchSpec` (`internal/daemon/claudelaunchspec.go`) appends `--model <model>` and `--effort <effort>` to the claude argv when non-empty (`:138-145`), shape-validated at "Step 6". Chain: `.dot` node attr → `resolvedModel/nodeModel` → `claudeRunCtx.model` → claude argv.

### "Can a bead override the model?"
**Yes — via a `model:<alias>` bead label (Tier-1).** A `.dot` node `model=` overrides per-node (claude-family only). NO per-item queue-submit model field. Precedence: node attr (claude only) → bead label → project config → env → compiled default (`sonnet`).

---

## 6. Template params (WG-045)

### Storage
Params live on the **queue item**, not the bead or Run: `queue.Item.TemplateParams map[string]string` (`internal/queue/types.go:181-185`). Arrive over the `queue submit` RPC, validated for ingestion hygiene at ingest (`internal/queue/rpc.go:232-281`, `core.ValidateTemplateParams`). Workloop snapshots them (`workloop.go:1441,1518,2273,2669`), threads them as `itemTemplateParams` into `beadRunOne` (`workloop.go:3121`), then into `LoadDotWorkflowWithParams(dotPath, itemTemplateParams)` (`workloop.go:4058`).

### Token grammar & substitution
- Token regex: `__[A-Z][A-Z0-9_]*__` — `internal/workflow/params.go:34`.
- **Ordering (WG-046, security-critical):** substitution happens AFTER parse, per-attribute, on the typed graph — `substituteGraphParams` (`internal/workflow/params_graph.go:168`). NOT on raw source (the old raw-source splice `SubstituteTemplateParams` in `params.go:56` is retained but MUST NOT be on a load path feeding a shell sink).
- **Per-attribute handling** (`params_graph.go:190-254`): `tool_command` values are POSIX shell-quoted via `ShellQuote` (command-injection close); **every other attribute** (goal, prompt, role, label, model, ids, conditions' RHS) substituted **verbatim**. A `tool_command` token inside the author's own quotes is a fail-loud load error (`ErrQuotedToolCommandToken`, `:55-66,181-188`).
- **Residual check:** any surviving `__TOKEN__` is a launch error ("pass --param KEY=VALUE for each") — `params.go:38-46`, `params_graph.go:256-271`.
- No-op fast path: token-free source byte-identical (`params.go:57-59`).

Flow: `queue submit --param KEY=VALUE` → `Item.TemplateParams` → validated at RPC ingest AND in `substituteGraphParams` → applied per-attribute post-parse → residual tokens fail the load.

---

## 7. Graph-source resolution (3 tiers) & the goal channel

`workloop.go:4042-4068`:
- **Tier 3 (embedded):** `preloadedDotGraph` — embedded `internal/daemon/standard-bead.dot`, loaded via `loadStandardGraph(itemTemplateParams)` (`workloop.go:3877`) / `LoadDotWorkflowFromBytes`.
- **Tier 1 (explicit ref):** `itemWorkflowRef` from the queue item (absolute or `<projectDir>/`-relative).
- **Tier 2 (default):** `<projectDir>/workflow.dot`.

Graph-level `goal=` (WG-044) prepended into every agentic node's `ExtraContext` as `Workflow goal: <goal>` (`workloop.go:4070-4081`); node `role=` prepended per-node (`dot_cascade.go:1305-1316`).

---

## 8. Gaps / limitations / hardcoded points (for the redesign)

1. **Tier-3 model defaults hardcoded in Go** — `defaultModelEntries`: `claude-code → {sonnet, medium}` (`modelpreference.go:156-159`). Changing the default coding model needs a code change (or label/config/env override).
2. **No per-queue-item model field** — only override channels are the `model:<alias>` bead label and the `.dot` node `model=`. `queue submit` has `TemplateParams` but no model field (comments ref hk-4x3rg "queue default not landed", `dot_cascade.go:1346`, `workloop.go:3270`).
3. **Per-node `model=` is claude-family-only** — pi/codex node `model=` pin silently ignored (`nodeModelForHarness`, `dot_cascade.go:1182-1187`); "future pi/codex node model= pin out of scope" (`:1333`).
4. **Params live on the queue item only, not persisted in the Run record** — snapshotted through the workloop, applied at load time (`capturedItemTemplateParams`, `workloop.go:2089,2669`). No Run-record `params` field found.
5. **`prompt=` is implementer-only at v1** — inert on reviewer nodes (`claudelaunchspec.go:308-311`); WARNING on non-agentic/gate (`ast.go:159-162`). Reviewer briefs can't be customized via `prompt=`.
6. **Feedback-delivery drift across harnesses** — claude uses paste-inject + rewritten `agent-task.md`; pi/codex use a separate resume seed (`agentseedprompt.go`). Two independent paths kept manually aligned; caused the no-commit-on-resume loop.
7. **Edge dialect equality-only** — `==`/`!=` + `&&`, whitelisted LHS, no `<`/`>`, no `||` (`ast.go:295-311`). No numeric-threshold / disjunction routing.
8. **Config Tier-2 needs daemon restart** — `.harmonik/config.yaml` model/effort loaded once at start, no mtime invalidation (`modelpreference.go:16-19`). Only env-var Tier-2.5 hot-reloads.
9. **`UnknownAttrs` retention is informational only** — dispatcher "MUST NOT read" (`ast.go:251-254`), except the deliberate `traversal_cap` bridge (`dispatcher.go:230-234`). New edge/node semantics must be promoted to typed fields.
10. **`gate` / `sub-workflow` dispatch** exist (`dot_gate.go`, `sub_workflow_runner.go`) but are more thinly exercised than the review-loop core.

---

## Key file index

| Concern | File |
|---|---|
| Node/Edge/Graph AST + attribute catalog | `internal/workflow/dot/ast.go` |
| DOT parser | `internal/workflow/dot/parser.go` |
| DOT validator | `internal/workflow/dot/validator.go` |
| Edge condition eval | `internal/workflow/dot/edges.go` (`EvalCondition`) |
| Loader (read→parse→subst→validate) | `internal/workflow/loader.go` |
| Template-param substitution | `internal/workflow/params.go`, `params_graph.go` |
| Edge cascade bridge (`DecideNextNode`) | `internal/workflow/dispatcher.go` |
| Runtime driver (`driveDotWorkflow`) + per-node model | `internal/daemon/dot_cascade.go` |
| DOT-mode entry + graph-source resolution + goal | `internal/daemon/workloop.go:4031` |
| Model/effort 4-tier resolution | `internal/daemon/modelpreference.go` |
| claude launch-spec (--model/--effort argv, prompt→body) | `internal/daemon/claudelaunchspec.go` |
| pi/codex resume seed prompt | `internal/daemon/agentseedprompt.go` |
| Implementer/reviewer/feedback brief writers | `internal/workspace/agenttask_chb028.go` |
| Review-loop launch-spec builders | `internal/daemon/launchspecbuild.go` |
| Queue item TemplateParams | `internal/queue/types.go`, `internal/queue/rpc.go` |
| Example workflows | `specs/examples/*.dot` |
| Embedded fallback workflow | `internal/daemon/standard-bead.dot` |
