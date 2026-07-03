# Research — B. Inline per-node prompt (hk-sdnzj)

> Pass 3 (`research`) of `attractor-parity`. Component B per `02-components.md`. Adds a `prompt` optional attr to `agentic` nodes, threaded into the Claude launch brief. Today the brief comes only from the bead (`beadTitle`/`beadDescription`).

## Research questions

- RQ-B1. Where does the brief originate today, and where would a per-node `prompt` thread in?
- RQ-B2. What does WG-002 require/permit on the `agentic` row; what reserved-attr discipline applies?
- RQ-B3. Override vs coexist: does `prompt` replace the bead-derived brief or augment it? What does kilroy expect?
- RQ-B4. Does this touch the Outcome contract or any handler-contract obligation?
- RQ-B5. Coupling with component C (both edit `dispatchDotAgenticNode`).

## Findings

### F-B1 — The brief is bead-derived; the threading point is the agent-task body (RQ-B1)

The brief is delivered to claude via the per-launch `agent-task.md` artifact (CHB-028). The construction is in `internal/daemon/claudelaunchspec.go:246-273`:

    taskBody := rc.beadDescription
    if taskBody == "" { taskBody = rc.beadTitle }
    if taskBody == "" { taskBody = rc.beadID }
    ...
    agentTaskPayload := workspace.AgentTaskPayload{
        ...
        Title: taskTitle,   // rc.beadTitle (or beadID)
        Body:  taskBody,    // rc.beadDescription (the brief)
        ...
    }
    workspace.WriteAgentTask(rc.workspacePath, agentTaskPayload)

`rc` is a `claudeRunCtx` (struct at `claudelaunchspec.go:48`; `beadTitle`/`beadDescription` fields at lines 94-103). `rc` is populated in `dispatchDotAgenticNode` (`dot_cascade.go:400-419`) from the function's `beadTitle`/`beadDescription` params, which flow down from `driveDotWorkflow` (`dot_cascade.go:128-129`) which receives them from `beadRunOne` (`workloop.go:1226/1292`, `beadRecord.Title`/`.Description`).

**The threading point is therefore a single new field on `claudeRunCtx` (e.g. `nodePrompt string`), set in `dispatchDotAgenticNode` from `node.UnknownAttrs["prompt"]` (or a typed `node.Prompt`), and consumed at `claudelaunchspec.go:246` to override/augment `taskBody`.** Nothing below the agent-task write needs to change — claude reads `agent-task.md` exactly as today.

Parser: `prompt` currently lands in `node.UnknownAttrs` permissively (`parser.go:660`), readable today with zero parser change; to make it strict-position it gets a switch case + WG-031 reserved-set entry (same as component A's attrs).

### F-B2 — WG-002 `agentic` row + reserved set (RQ-B2)

`specs/workflow-graph.md:85` — the `agentic` row's optional-attr column today: `idempotency_class`, `axis_tags`, `skills_ref`, `freedom_profile_ref`, `budget_ref`, `hook_ref`, `guard_ref`. Add `prompt`. Add `prompt` to the WG-031 reserved set (`workflow-graph.md:388`) so a `prompt` on a non-agentic/gate node or an edge is an ingest error. Minor schema bump, N-1 readable (WG-034) — confirmed clean (optional attr, no enum/type/edge-field change).

### F-B3 — Override vs coexist; kilroy expectation (RQ-B3)

kilroy `box` nodes carry a *complete, self-contained* `prompt="..."` (see the multi-step `investigate` / `dedup_check` / `gather_reproduce` prompts in both pipelines — each is the full brief for that node, with its own Step 1..N structure and output-file contract). They do NOT reference a bead — kilroy has no bead concept; the graph-level `goal` (component E) supplies the cross-node mission and the per-node `prompt` supplies the step. **So kilroy semantics = `prompt` is the brief; the bead is irrelevant when `prompt` is present.**

But harmonik runs `workflow_mode=dot` *bead-tied* (a `dot` run is claimed against a bead per EM-014; `beadRunOne` always has a `beadRecord`). The two are not mutually exclusive in harmonik: a node may want the bead context (the "why this run exists") AND a node-specific instruction. **Recommended resolution (design to confirm): `prompt`, when present, REPLACES the bead-derived `Body` as the node brief, but the bead `Title` + bead ID remain in the agent-task header for traceability** (the agent-task template already renders `Title` + `BeadID` separately from `Body` — see `AgentTaskPayload` fields). This matches kilroy (the prompt is the brief) while preserving harmonik's bead-tie for events/close. Rationale: a node author who writes a full `prompt` intends it to be the instruction; silently prepending a possibly-large bead body would dilute it. Authors who want bead context inside the prompt can reference it (harmonik could template `__BEAD_TITLE__` etc., but that overlaps component E — out of scope here).

Edge case: review-loop reviewer nodes. The reviewer brief comes from `review-target.md` (written at `dot_cascade.go:364-381` via `WriteReviewTarget`), NOT `agent-task.md`. A `prompt` on a reviewer node is therefore a no-op on the review brief unless the design also threads it into `WriteReviewTarget`. **Lean: `prompt` applies to implementer-class agentic nodes only at v1; a `prompt` on a reviewer node is accepted but documented as not overriding the review-target brief** (or rejected — design decides). The live kilroy pipelines have no reviewer-class nodes (their `code_review` box is a plain implementer-style box writing `.ai/review.md`, not a harmonik reviewer with `review.json`), so this edge is not exercised by the target workloads.

### F-B4 — No Outcome / handler-contract change (RQ-B4)

The Outcome envelope (EM-005) is untouched — `prompt` affects only the *input* brief, not the *output* Outcome. HC §4.2 / CHB-028 owns the agent-task threading; the change is "the daemon MAY source `agent-task.md` Body from the node's `prompt` attr instead of the bead body." This is a daemon-side launch-spec construction detail; the wire protocol, LaunchSpec required fields (HC-006, `handler-contract.md:121`), and the handler's read of `agent-task.md` are all unchanged. The HC change is at most a clarifying note in the §4.2 / HC-006a agent-task row (`handler-contract.md:156`) that the Body MAY be node-prompt-sourced.

### F-B5 — Coupling with component C (RQ-B5)

B and C both edit `dispatchDotAgenticNode` (`dot_cascade.go:343-593`) and its `claudeRunCtx` build. B adds a `nodePrompt` field + a brief-source branch (~line 246 of claudelaunchspec.go and ~line 413 of dot_cascade.go). C gates the HEAD-advance check (`dot_cascade.go:589`). **The edits are in different regions of the function (B: launch-spec build / pre-launch; C: post-launch outcome derivation) and do not textually overlap** — but per the decompose sequencing (B before C, or co-design) they should land in one coordinated change or strictly sequenced to avoid a merge conflict on the function. Recommend co-design (single bead-batch) since both are small.

## Patterns to follow

- Agent-task Body construction: `claudelaunchspec.go:246-273`.
- `claudeRunCtx` field plumbing: `dot_cascade.go:400-419` → `claudelaunchspec.go:48`.
- Permissive→strict attr promotion: `parser.go:644-666`.

## Risks / conflicts

- **R-B1 (low):** reviewer-node `prompt` is a no-op on the review brief (F-B3) — document or reject at v1; not exercised by kilroy.
- **R-B2 (low):** merge coupling with C on `dispatchDotAgenticNode` (F-B5) — co-design / sequence.
- **R-B3 (low, flag for integration):** if component E (graph `goal` + `__PARAM__` substitution) lands, the substitution pass must also run over the per-node `prompt` (kilroy prompts contain `__SENTRY_SHORT_ID__` etc.). Out of scope for B's design but the integration pass must wire E's substitution to cover both `goal` and `prompt`.
