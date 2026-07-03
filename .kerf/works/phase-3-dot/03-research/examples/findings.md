# C5 — `specs/examples/` + canonical `.dot` files: Research Findings

> Pass-3 research for component **C5** of `phase-3-dot`. Scope: research only — what the example `.dot` files would *contain*, what the directory should look like, what validation must cover, and how examples should be tested. **No design decisions.** **No `.dot` files written.** Sketches are descriptive, not normative.

## 1. Purpose of C5 (recap)

C5 ships a new directory `specs/examples/` with the minimum-two canonical `.dot` workflows that the rest of Phase 3 must validate against. Each example doubles as (a) the anchoring artifact for `specs/workflow-graph.md` (C1), (b) a worked test case for the `dot` dispatch driver (C2), and (c) a contract reference for the `Outcome` surface (C3). Absorbs gap **G4** (no example `.dot` files in repo) in full; absorbs gap **G6** (repo convention) partially by exercising the convention.

## 2. `review-loop.dot` — research

### 2.1 What the existing hardcoded review-loop gives us "for free"

The EM-015d hardcoded review-loop is a complete, normatively-specified two-node sub-graph already implemented in daemon code. From `specs/execution-model.md:258`:

`implementer → reviewer → {APPROVE: close, REQUEST_CHANGES: implementer, BLOCK: close-needs-attention, iteration-cap: close-needs-attention, no-progress: close-needs-attention}`

`review-loop.dot` is the DOT expression of this exact shape. EM-015d explicitly anticipates this: *"This sub-graph shape is representable as an instance of the general workflow-graph model defined for `dot` mode (cross-ref pending dot-mode spec)."* C5's job is to make that cross-reference concrete.

From EM-015d/EM-015e the spec already pins:
- terminal-node set: `close` (APPROVE path), `close-needs-attention` (BLOCK / cap-hit / no-progress);
- routing inputs: `verdict ∈ {APPROVE, REQUEST_CHANGES, BLOCK}`, `iteration_count` (1..3), `last_diff_hash` (no-progress detector);
- reserved `Run.context` keys: `iteration_count`, `last_verdict`, `claude_session_id`, `last_diff_hash`;
- iteration cap = 3 (hardcoded, not graph-tunable at MVH).

### 2.2 Nodes the file would contain

Five-node sketch (start + implementer + reviewer + two terminals):
1. `start` — entry no-op (Kilroy `Mdiamond` / `start` handler).
2. `implementer` — agentic node; `handler_ref` points to the claude-implementer handler; reused via `claude --resume <claude_session_id>` per EM-015d.
3. `reviewer` — agentic node; `handler_ref` points to the claude-reviewer handler; fresh Claude session per iteration per EM-015d.
4. `close` — exit terminal (APPROVE path).
5. `close_needs_attention` — exit terminal (BLOCK / cap-hit / no-progress). Distinct exit ID is the cleanest way to make the `needs-attention` label routing legible at the graph level.

Optionally a sixth conditional node `route_verdict` between reviewer and the terminals — but Kilroy's idiom puts routing on outgoing edges of the agentic node, not on a separate pass-through.

### 2.3 Edges + conditions the file would carry

Outgoing-edge set from `reviewer`:
- `reviewer -> implementer` with `condition="verdict=REQUEST_CHANGES"` (loop edge — the retry cycle);
- `reviewer -> close` with `condition="verdict=APPROVE"`;
- `reviewer -> close_needs_attention` with `condition="verdict=BLOCK"`;
- `reviewer -> close_needs_attention` with `condition="completion_reason=cap_hit"` (or by failure_class);
- `reviewer -> close_needs_attention` with `condition="completion_reason=no_progress"`.

Plus the start-edge (`start -> implementer`) and the inner loop edge (`implementer -> reviewer`, unconditional).

This forces C1 to answer: are `verdict`, `completion_reason`, `iteration_count` legal LHS keys in edge conditions, or only `outcome.status` / `failure_class`? The `review-loop` shape *requires* surfacing `verdict` (or its mapping into the Outcome status enum). This is an example-driven constraint on Gap 2 (edge-condition syntax) and on Q6 (LHS whitelist).

### 2.4 Attributes the file would need

Graph-level: `schema_version="1.0"`, `terminal_node_ids="close,close_needs_attention"`, possibly `workflow_id="review-loop"`, `default_max_retry=3` (matches EM-015e cap; whether `iteration_count` is generic retry or a review-loop-specific counter is a C1 question).

Node-level on `implementer` / `reviewer`: `handler_ref`, `prompt_ref` (or `prompt` inline), `fidelity` (per Kilroy's modes — implementer uses `full` with session resume; reviewer uses `truncate` or `compact` per iteration), `thread_id` (implementer only; reviewer is fresh).

Node-level on terminals: `terminal_kind ∈ {close, close-needs-attention}` so the daemon's terminal-transition write per BI-010 knows which close-path to take.

### 2.5 What fields C1 must add to make this expressible

Surveyed against the existing Outcome surface (`status, preferred_label, suggested_next_ids, context_updates, notes, kind, payload`):

- **Verdict surfacing.** Either (a) require the implementer/reviewer handler to map verdict → status (APPROVE → SUCCESS, REQUEST_CHANGES → RETRY, BLOCK → FAIL); or (b) expose `verdict` as a first-class context_updates key with a whitelisted name; or (c) generalize edge LHS to read any context key. Option (a) is cheapest but loses fidelity; option (c) is most powerful but enlarges Gap 2. **Pass-4 design decision.**
- **`completion_reason`** as a routing LHS — only meaningful inside the review-loop family. Could be a context_updates key; in that case the LHS whitelist must include it.
- **Terminal-kind annotation** — does C1's terminal-node-id mechanism distinguish "close-success" from "close-needs-attention"? Either via per-terminal `terminal_kind` attribute or via the run's terminal-state classifier (§4.3.EM-015c) reading the inbound edge's label. Pass-4.

### 2.6 What this example proves

A clean `review-loop.dot` proves: (i) the C1 node-type catalog covers agentic-with-session-resume, (ii) the C2 dispatcher can drive a cyclic graph with a hardcoded cap, (iii) the C3 Outcome surface carries verdict information without the engine needing to special-case "review-loop", (iv) the migration target for the EM-015d hardcoded Go path exists.

## 3. `bead-process.dot` — research

### 3.1 What the workflow represents

The full "process a bead through the orchestrator" loop the user currently does manually via sub-agents in the Agent tool. Stages:
- `claim` (`br update <id> --status=in_progress`) — non-agentic / tool node;
- `worktree_create` — non-agentic / tool node (git worktree from main);
- `implement` — agentic (claude-implementer);
- `review` — agentic (claude-reviewer) OR sub-workflow expansion to review-loop.dot;
- `merge` — non-agentic / tool (ff-only merge into main per the locked branching-model decision);
- `close_bead` (`br close <id>`) — non-agentic / tool;
- `reconcile` — optional reconciliation node per the reconciliation-as-workflow decision.

### 3.2 The sub-workflow seam

Most interesting design question this example surfaces: **is the review step a single agentic node, or a sub-workflow expansion to review-loop.dot?** If sub-workflow, this example exercises Kilroy's `stack.manager_loop` shape — which Kilroy itself stubbed to FAIL in v1 and pass-1 §5 lists as **out of scope** for Phase 3. So bead-process.dot either:

(a) inlines a single `review` agentic node (no review-loop semantics; closes on first verdict); or
(b) inlines the review-loop.dot shape inline (duplicates 4 nodes); or
(c) references review-loop.dot as a sub-workflow (depends on the deferred sub-workflow primitive).

Option (a) is honest but degrades the example's value. Option (b) is workable but duplicative. Option (c) is the natural shape but blocked by the out-of-scope decision. **This is a serious open question for pass-4** and motivates the recommendation at the end of this doc.

### 3.3 Edges + routing

Mostly linear with a small number of fail-back edges:
- `claim -> worktree_create -> implement -> review`;
- `review -> merge` on APPROVE;
- `review -> close_needs_attention` on BLOCK / cap-hit;
- `merge -> close_bead -> reconcile -> exit`;
- `reconcile -> exit_needs_human` on Cat-6 verdict (per the reconciliation memory).

Routes back to `implement` on REQUEST_CHANGES if option (b) inlined; otherwise that loop lives inside the sub-workflow per (c).

### 3.4 Attributes the file would need

Beyond what review-loop.dot needs:
- node-type `tool` for `br` and `git` invocations (Kilroy `parallelogram`);
- per-tool node: `command_ref` or `tool_handler`, structured args (bead_id, etc.);
- `goal_gate=true` on `merge` (don't reach exit without merge);
- branch-strategy attribute on `merge` (`ff_only=true` per locked decision);
- context-updates flow: `claim` produces `bead_id, worktree_path`; `implement` produces verdict surface; `merge` produces `merge_sha`.

### 3.5 What fields C1 must add

Beyond the review-loop set: **tool-node handler contract**. C1 must specify how a non-agentic `tool` node's Outcome maps to status — is `exit 0` → SUCCESS, non-zero → FAIL? Or must every tool wrapper conform to the full handler-contract? This is partially answered by HC-008 (Outcome-via-progress-stream) but tool wrappers may not be Claude subprocesses. The example forces the question.

### 3.6 Speculative-ness

bead-process.dot is more speculative than review-loop.dot. It includes nodes (`reconcile`, `worktree_create`, tool-node `merge`) whose handler contracts are not yet written. Two of these (reconcile, merge) depend on cross-cutting subsystem work outside Phase 3 scope. Authoring it now risks pinning shapes before their handlers exist. See §8 recommendation.

## 4. `specs/examples/` directory shape

### 4.1 Layout

Three candidates:

- **Flat.** `specs/examples/review-loop.dot`, `specs/examples/bead-process.dot`, `specs/examples/README.md`. Pro: minimal; matches Kilroy's `docs/strongdm/dot specs/` shape. Con: scaling to many examples is messy.
- **Per-workflow subdir.** `specs/examples/review-loop/{workflow.dot, README.md, expected-trace.json}`. Pro: each example can ship a sibling expected-trace artifact for round-trip testing; pro: prose lives next to graph. Con: heavier for two examples.
- **Categorized.** `specs/examples/{cycles, linear, gated}/...`. Premature.

**Lean (research note, not decision):** flat for v1; revisit at 5+ examples. The README.md is mandatory either way — it maps each `.dot` to its spec anchor (which gap it absorbs, which spec sections normalize each attribute it uses).

### 4.2 Schema-version pinning

Every `.dot` file MUST carry graph-level `schema_version="1.0"` per Gap-6 resolution. The directory README MUST state which schema version is the canonical for examples. Mixed-version examples (testing back-compat) belong in a later test fixture set, not in `specs/examples/`.

### 4.3 Comments inside `.dot` files

Open Q from pass-2 §C5: inline DOT comments explaining each node, or terse-DOT-plus-sibling-README? Both valid; Kilroy's `consensus_task.dot` uses inline comments. **Lean: do both** — inline one-line comments per node, prose in README.md.

## 5. Pre-run validator coverage (Gap-3 / C2 angle)

The C2 validator (Kilroy's `ingestor-spec` analog) must catch — and the examples must exercise:

| Validator check | review-loop.dot | bead-process.dot |
|---|---|---|
| `schema_version` present and parseable | yes | yes |
| Start node exists (exactly one entry) | yes | yes |
| Terminal nodes match `terminal_node_ids` declaration | yes (two terminals) | yes (two-or-three terminals) |
| All nodes reachable from start (BFS) | yes | yes |
| ASCII-only identifiers | yes | yes |
| Every node's `handler_ref` resolves to a known handler | yes | yes (incl. tool handlers) |
| Every edge condition's LHS is in the whitelist | stress-test | stress-test |
| Cyclic edge has a `max_retries` or `default_max_retry` bound | yes (implementer-loop) | yes (if option-b) |
| Unknown attributes warn (per the warn-and-continue lean for Gap-6) | — | — |
| Tool-node has `command_ref` or equivalent | — | yes |

review-loop.dot exercises cyclic-with-cap; bead-process.dot exercises tool-node validation and linear-with-fail-edge routing. Between the two, validator coverage is reasonable for a v1 schema.

## 6. Test coverage strategy

### 6.1 Two layers

- **Static round-trip.** Each `.dot` in `specs/examples/` is parsed and validated by the C2 ingestor in a unit test (`go test ./internal/workflow/...`). Failure to round-trip is a CI break. This is cheap, deterministic, and pins the schema-vs-example tightness contract.
- **Scenario test.** Each example workflow is loaded by a scenario harness test (`specs/scenario-harness.md`) that runs a mock bead through it end-to-end. The harness mocks the handler subprocesses; the dispatcher walks the graph; the test asserts (a) the final terminal node matches expectation, (b) the event sequence (per event-model.md §8) matches a golden trace.

### 6.2 Test fixture pairing

Each example `.dot` ships with `expected-trace.golden.jsonl`. The scenario harness compares the run's emitted event stream against the golden. Updates to the golden are spec-level review events.

### 6.3 What this catches that the validator doesn't

The validator catches structural errors. The scenario test catches:
- driver bugs (wrong edge selected for a given Outcome);
- routing-cascade ordering bugs (preferred_label preempts condition, etc.);
- terminal-state classifier bugs (reaches close vs. close-needs-attention);
- event-emission bugs (missing `review_loop_cycle_complete`, etc.).

Both layers are needed; the static round-trip is a precondition for the scenario test running.

## 7. Cross-component dependencies

- **C1 (workflow-graph.md):** C5 cannot author the `.dot` files until C1 pins the node-type catalog (Gap 1), the edge-condition LHS whitelist (Gap 2), the terminal-node mechanism (close vs. close-needs-attention), and the schema-version attribute (Gap 6). C5 is **fully blocked on C1**.
- **C2 (execution-model.md §dot):** C5 needs the dispatcher's contract pinned to know whether `iteration_count`-bounded cycles are encoded as `default_max_retry` or as a cycle-detection mechanism. Also pins the validator's surface. Partial-block.
- **C3 (handler-contract.md §Outcome):** C5 needs to know whether `verdict` surfaces via `status`-mapping, `context_updates` key, or a new Outcome field. Affects what attributes review-loop.dot's reviewer node carries.
- **C4 (control-points.md):** bead-process.dot's `merge` node is a candidate control-point. Whether `control-point` is a node-type or a flag determines how `merge` is annotated. Partial-block on bead-process.dot only.

## 8. Top 3 open design decisions for pass-4

1. **Scope of bead-process.dot.** Inline single-review node (option a), inline review-loop shape (option b), or sub-workflow reference (option c — blocked by pass-1 out-of-scope). **Recommendation: defer bead-process.dot to a Phase-3 follow-up; ship review-loop.dot alone in C5.** Rationale: bead-process.dot depends on handler contracts for tool-nodes, merge, and reconciliation that are not yet specced; authoring it now pins shapes prematurely and the example's value as "what the orchestrator does today" is lost without sub-workflow composition.
2. **Verdict-to-routing surface.** Map verdict → Outcome.status (lossy), expose verdict via `context_updates` with whitelisted LHS, or add a first-class `verdict` field to Outcome. Affects C1 §Edge Conditions + C3 §Outcome.
3. **Terminal-node differentiation.** How do `close` and `close-needs-attention` differ at the graph level? Per-terminal `terminal_kind` attribute, distinct terminal-node-id sets, or edge-label inspection by the daemon's terminal-state classifier (EM-015c). Affects C1 + C2.

## 9. Recommendation: bead-process.dot scope

**Defer to post-Phase-3.** review-loop.dot alone is sufficient to satisfy Gap-4 (canonical example exists), Gap-6 (repo convention validated), and the dogfood-migration path for the EM-015d hardcoded code (the highest-value Phase-3 outcome). bead-process.dot's value depends on primitives (sub-workflows, tool-handler contract, reconcile-as-workflow) that pass-1 §5 explicitly defers. Ship review-loop.dot as the canonical example in C5; mark bead-process.dot as a candidate for a follow-up bead (`phase3-bead-process-example`) gated on the deferred primitives landing.

If pass-4 design wants to keep bead-process.dot in scope, the cheapest shape is option (a): a single-review-node version that is honest about not being the full orchestrator loop. Option (b) duplicates structure; option (c) is blocked.

---

**Sources:** `01-problem-space.md`, `02-components.md`, `.kerf/recon/kilroy-findings.md`, `.kerf/recon/attractor-findings.md`, `specs/execution-model.md` §§4.3.EM-012, EM-015d, EM-015e, §6.1 Outcome record, `specs/handler-contract.md` §HC-008.
