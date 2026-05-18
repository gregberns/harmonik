# Execution Model — Change Design (C2)

Scope: introduce `workflow_mode` as a first-class field on the Run record; define the four-tier resolution-precedence rule; describe `review-loop` as a hardcoded two-node sub-case of the general workflow-graph model. No graph-walker implementation arrives here — `dot` mode remains out of scope.

## 1. Current state

- `execution-model.md §4.1 EM-001..EM-005a` (lines ~80–126) defines the core types (Workflow, Edge, State, Transition, Outcome). No mode discriminator on the Run or Workflow.
- `§4.2 EM-006..EM-011` (lines ~129–166) defines node attributes (type, handler, timeout, axes). No phase discriminator.
- `§4.3 EM-012..EM-015c` (lines ~169–211) defines the Run model. `EM-012` (line 169) enumerates Run fields: `run_id`, `workflow_id`, `workflow_version`, `input`, `state`, `context`, `start_time`, `end_time`. `EM-013` (line 175) pins the `run_id` as the cross-store join key. `EM-014` (line 181) governs many-runs-per-bead; `EM-015` (line 187) forbids intra-run loops from minting new runs — "the run's `run_id` is stable across loop traversals."
- `§6.1` (Go type def for Run) declares the canonical struct.

## 2. Target state

### (a) Field on the Run record

Amend `EM-012` so the Run record carries a new field `workflow_mode ∈ {single, review-loop, dot}` with default `single`. Resolved at claim time, immutable for the run's lifetime, persisted into run metadata, surfaced in `run_started` and other run-scoped event payloads via the `bead_id`-style optional-field convention (see `event-model-design.md`). Update the `§6.1` Go type def to add the field.

### (b) Precedence rule

Add a new requirement, provisionally `EM-012a — Workflow-mode resolution precedence`. The daemon's claim path resolves `workflow_mode` by walking, in order:

1. Per-task: bead's `workflow:<mode>` label per `beads-integration.md §4.3 BI-009a`.
2. Per-project: project-level config (reserved tier; not populated at MVH, but the resolution function MUST tolerate its absence and pass through).
3. Per-daemon: daemon config default per `process-lifecycle.md §4.1 PL-004a`.
4. Built-in fallback: `single`.

The first non-empty tier wins. Resolution runs once per run at claim time; the result is sealed into the Run record. A `bead_label_conflict` on tier 1 (per BI design) MUST cause tier 1 to be treated as absent.

### (c) `review-loop` as a hardcoded two-node sub-case

Add a new requirement, provisionally `EM-015d — review-loop mode lifecycle`. Describe review-loop mode as a hardcoded two-node graph: `implementer → reviewer → {APPROVE: close, REQUEST_CHANGES: implementer, BLOCK: close-needs-attention, iteration-cap: close-needs-attention, no-progress: close-needs-attention}`. The spec NAMES this graph and its routing semantics, but does NOT obligate a graph-walker implementation; the v1 driver is mode-specific code in the daemon's claim/dispatch path. When `dot` mode lands later, this two-node graph is one specific instance of the general walker. EM-015's "intra-run loops are not new runs" applies unmodified: every implementer-launch and reviewer-launch within one review-loop cycle is part of the same Run; the `run_id` is stable across all iterations.

### (d) Per-iteration state in `context`

Amend `EM-012`'s `context` field description: in `review-loop` mode, the Run's `context` map MUST carry, at minimum, `iteration_count` (integer, 1..3), `last_verdict` (enum from the agent-reviewer JSON schema or `null` before iteration 1's review), `last_diff_hash` (used by the no-progress detector — diff hash of iteration N's worktree state vs the parent commit, compared against N-1's hash), and `implementer_session_id` (the captured Claude session ID for the same-session resume mechanism, per session-resume research). These keys are reserved; their format is normative for `review-loop`.

### (e) Iteration cap and early-exit rules

Add a new requirement, provisionally `EM-015e — review-loop iteration cap and early-exit`. The hardcoded cap is **3** iterations at MVH; not operator-tunable. Early-exit conditions:

- **APPROVE early-exit.** On reviewer `APPROVE`, the run terminates as `completed` regardless of iteration count.
- **No-progress early-exit.** If iteration N's `last_diff_hash` equals iteration N-1's (or the diff-set Jaccard exceeds a threshold owned by spec-draft; recommended ≥ 0.9), the run terminates with `needs-attention` close path.
- **Cap-hit.** If iteration 3's reviewer returns `REQUEST_CHANGES`, the run terminates with `needs-attention` close path.
- **BLOCK.** Any `BLOCK` verdict terminates immediately with `needs-attention` close path.

The `needs-attention` close path is operator-drained only (no auto-retry), specified in `operator-nfr-design.md`. The Beads write at termination follows the existing `BI-010a` table — review-loop terminations route through the same `close` / `reopen` ops; `needs-attention` is a label/marker addition described in operator-nfr.

### (f) Session-id semantics under stable `run_id`

Cross-reference `EM-013`: review-loop preserves one `run_id` for the whole cycle. Multiple `session_id`s exist under it (each implementer launch is a fresh OS subprocess; each reviewer launch is its own session). The implementer subprocess sessions form one logical Claude session via `--resume <session_id>` (per locked decision: same implementer Claude session resumed across iterations). The Run record's `context.implementer_session_id` is the Claude session ID, distinct from harmonik's `session_id` event field — clarify the naming in §3 Glossary.

### (g) Glossary disambiguation: `claude_session_id`

Glossary (§3 of execution-model.md) gains a `claude_session_id` entry distinguishing it from harmonik's internal `session_id` — `claude_session_id` is the value captured from `claude -p ... --output-format json | jq -r .session_id` and used with `claude --resume <id>`.

## 3. Rationale

- Satisfies problem-space success criteria §2, §3 (lifecycle definition), §8 (mode-selection mechanism factored so `dot` is a new branch, not a refactor).
- Locked decision: same implementer session resumed across iterations (NOT fresh-per-iteration). The `context.implementer_session_id` field shape reflects that decision.
- Locked decision: cap=3, hardcoded, with APPROVE early-exit and no-progress diff-hash early-exit. `EM-015e` enumerates each.
- The two-node graph framing is the bridge that lets `dot` mode adopt this case without re-spec — research finding `ralph-prior-art/findings.md` is informational; the spec language stays neutral on Huntley/Reflexion provenance.

## 4. Requirements traceability

| Req (02-components.md C2) | Target-state element |
|---|---|
| Run carries `workflow_mode`, resolved at claim, immutable | §2 (a) |
| Resolution precedence rule (task → project → daemon → built-in) | §2 (b) |
| Review-loop is a hardcoded two-node sub-case of the workflow-graph model | §2 (c) |
| `context` carries `iteration_count`, `last_verdict`, `implementer_session_id` | §2 (d) |
| One `run_id` across all iterations; multiple `session_id`s under it | §2 (f) |
| Cap=3, APPROVE early-exit, no-progress early-exit | §2 (e) |
| `EM-015` intra-run loop semantics preserved | §2 (c) restatement |
| `dot` is a new branch in resolution + walker, not a refactor | §2 (b) and (c) framing |

## 5. Open decisions remaining for spec-draft pass

- **Project-level tier (tier 2).** Whether to populate the tier at MVH or leave it as a reserved no-op slot. Recommend reserved no-op; spec-draft confirms.
- **No-progress detector threshold.** Exact algorithm (raw hash equality vs Jaccard on changed-file set vs Jaccard on hunk-set). Recommend simple SHA-256 hash of `git diff <parent>..<head>` output for v1; spec-draft picks.
