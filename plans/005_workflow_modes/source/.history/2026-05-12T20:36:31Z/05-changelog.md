# Workflow Modes — Changelog

One entry per spec file drafted in `05-spec-drafts/`. Each names: target filename, modification status, summary of changes, motivating change-design doc.

---

## `specs/process-lifecycle.md` — MODIFIED

**Motivated by:** `04-design/process-lifecycle-design.md` (component C4).

**Changes:**
- New requirement **PL-004a** (Default workflow mode) in §4.1. Declares `workflow_mode_default ∈ {single, review-loop, dot}` as a daemon-startup config value (default `single`), read once at PL-005, immutable for the daemon's lifetime, occupying the lowest tier of the resolution precedence.
- **PL-ENV-001(e)** state-owned list extended to include `workflow_mode_default`.
- **PL-005 step 0** amended to load `workflow_mode_default` from project config.
- **PL-018** clarified: caching the resolved mode is configuration plumbing, not LLM-bearing logic.
- Revision-history row pending finalize.

---

## `specs/beads-integration.md` — MODIFIED

**Motivated by:** `04-design/beads-integration-design.md` (component C5).

**Changes:**
- New requirement **BI-009a** (Workflow-mode label encoding). Defines `workflow:<mode>` label as the per-bead override; values `workflow:single`, `workflow:review-loop`, `workflow:dot`. Multi-`workflow:`-label conflict routed to `bead_label_conflict` event (per event-model §8.8.6) with structured-log fallback; daemon falls back to next-precedence tier on conflict.
- New requirement **BI-010c** (Write-discipline prohibition). Agents MUST NOT mutate `workflow:<mode>` labels via `br update`. Only operators or daemon-as-orchestrator (where workflow design so dictates) write the field.
- **BI-013** amended: ready-work query returns labels in the response payload.
- New requirement **BI-013a** (needs-attention exclusion). The Beads adapter's ready-work query MUST exclude beads carrying a `needs-attention` label, even when `status = open`. Anchors the operator-NFR drain-policy.
- **BI-020** amended: sidecar metadata MAY carry the resolved `workflow_mode`.
- Revision-history row pending finalize.

---

## `specs/execution-model.md` — MODIFIED

**Motivated by:** `04-design/execution-model-design.md` (component C2).

**Changes:**
- **EM-012** (Run record) amended: adds `workflow_mode ∈ {single, review-loop, dot}` field (resolved at claim time, immutable for run lifetime) and reserves four `context` keys: `iteration_count`, `last_verdict`, `claude_session_id`, `last_diff_hash`.
- New requirement **EM-012a** (Mode resolution precedence). Resolution order: per-bead `workflow:<mode>` label → project config (reserved no-op at MVH) → daemon default (PL-004a) → built-in `single`.
- New requirement **EM-015d** (Review-loop lifecycle). Hardcoded two-node sub-case: `implementer → reviewer → {APPROVE: close, REQUEST_CHANGES: implementer, BLOCK: close-needs-attention, iteration-cap: close-needs-attention, no-progress: close-needs-attention}`. Single `run_id` covers entire cycle; multiple `session_id`s under it.
- New requirement **EM-015e** (Iteration cap + early-exit). Cap = 3 (hardcoded for v1). Early-exit on `APPROVE` (do not run remaining iterations). No-progress detector: if SHA-256(`git diff <parent>..<head>`) equals iteration N-1's hash, exit to `needs-attention`.
- **§6.1 Run RECORD** extended with `workflow_mode` field; new ENUM `WorkflowMode`.
- **Glossary** entries added: `workflow_mode`, `claude_session_id` (distinct from harmonik's internal `session_id`), `iteration_count`, `needs-attention`.
- Revision row: v0.4.0.

---

## `specs/handler-contract.md` — MODIFIED

**Motivated by:** `04-design/handler-contract-design.md` (component C1).

**Changes:**
- New requirement **HC-003a** (Workflow-mode dispatch-level, not handler-selection-level). Mode does not alter which handler type is bound to a run; it alters the dispatch shape within the bound handler.
- **HC-004** (idempotency key) rewritten to support a conditional 4-tuple key. For `single` and `dot`-without-loops: `(run_id, node_id)`. For `review-loop` and any multi-phase node: `(run_id, node_id, phase, iteration_count)`. Field presence (rather than mode value) determines key shape.
- **HC-006** (LaunchSpec) extended with four optional fields: `workflow_mode`, `phase ∈ {implementer-initial, implementer-resume, reviewer}`, `iteration_count`, `claude_session_id` (present only on resume).
- **§6.1 LaunchSpec record** updated to declare the new optional fields.
- Cross-reference added: the reviewer phase's `outcome_emitted` corresponds to writing `.harmonik/review.json` per workspace-model.md §6.2.
- Revision-history row pending finalize.

---

## `specs/event-model.md` — MODIFIED

**Motivated by:** `04-design/event-model-design.md` (component C3).

**Changes:**
- New §**8.1a** (Review-loop cycle) with six event types:
  - `implementer_resumed` (class O). Payload: `run_id`, `session_id`, `iteration_count`, `claude_session_id`, `prior_verdict_summary` (front-truncated to 256 UTF-8 bytes).
  - `reviewer_launched` (class O). Payload: `run_id`, `session_id`, `iteration_count`, `claude_session_id`.
  - `reviewer_verdict` (class F — gates terminal routing). Payload conforms to `agent-reviewer` JSON schema v1: `{schema_version, verdict ∈ {APPROVE, REQUEST_CHANGES, BLOCK}, flags[], notes}` plus `run_id`, `session_id`, `iteration_count`.
  - `iteration_cap_hit` (class O). Payload: `run_id`, `iteration_count`, `cap_value`, `final_verdict`.
  - `no_progress_detected` (class O). Payload: `run_id`, `iteration_count`, `diff_hash_current`, `diff_hash_prior` (both SHA-256 of `git diff <parent>..<head>`).
  - `review_loop_cycle_complete` (class F). Payload: `run_id`, `final_iteration_count`, `completion_reason ∈ {approved, cap_hit, blocked, no_progress, error}`.
- New §**8.8.6** `bead_label_conflict` (class O). Payload: `bead_id`, `conflicting_labels[]`, `fallback_action`.
- §8.1 payload-field rule extended: all run-lifecycle events (`run_started`, `run_completed`, `run_failed`, plus the six review-loop events) carry an optional `workflow_mode ∈ {single, review-loop, dot}` field.
- §6.3 schemas added for all seven new events.
- Emission ordering rule: terminal-routing weight rests on class-F events (`reviewer_verdict` and `review_loop_cycle_complete`); `iteration_cap_hit` is observational.
- Reviewer-verdict schema-reuse rule normatively pinned (agent-reviewer JSON schema v1, no parallel schema).
- Revision row: v0.4.0.

---

## `specs/workspace-model.md` — MODIFIED

**Motivated by:** `04-design/workspace-model-design.md` (component C6).

**Changes:**
- **WM-011** gains an informative note citing review-loop as a concrete instance of sequential same-worktree multi-role occupation. No new state; the existing rule already permits the pattern.
- **WM-013a** amended: one lease per run lifetime, spanning all review-loop iterations and all implementer/reviewer subprocess pairs within a run.
- **WM-013e** `.gitignore` set extended to include `.harmonik/review.json` and `.harmonik/review.iter-*.json`.
- New normative paragraph under **WM-014** stating that review-loop introduces no new workspace lifecycle state; the run-context pointer (per execution-model EM-012) carries iteration state instead.
- New requirement **WM-027a** (Reviewer-verdict artifact). `.harmonik/review.json` is the canonical reviewer-emitted artifact. Daemon archives the prior iteration's verdict to `.harmonik/review.iter-<N>.json` before launching the next iteration's reviewer. Daemon owns the archive write; reviewer subprocess writes only the current `review.json`. Malformed reviewer output is handled per BI / event-model fallback rules.
- **WM-030** session-log accumulation note clarified for review-loop multi-session runs.
- §6.2 path table gains rows for `.harmonik/review.json` and `.harmonik/review.iter-<N>.json`.
- Revision-history row pending finalize.

---

## `specs/operator-nfr.md` — MODIFIED

**Motivated by:** `04-design/operator-nfr-design.md` (component C7).

**Changes:**
- **ON-002** amended: review-loop terminations (cap-hit, no-progress, BLOCK) do NOT consume new exit-code categories; they fold into the existing failure-category taxonomy.
- **ON-004** enumeration extended to reference ON-004a.
- New requirement **ON-004a** (Workflow-mode config inventory). Operator config inventory MUST list `workflow_mode` with its four-tier precedence (per-bead label → project config → daemon default → `single`), default value, and iteration-cap value (3, hardcoded).
- **ON-008** amended: between-task invariant preserved for `single` and `dot`; review-loop additionally admits intra-run iteration boundaries (between `reviewer_verdict` emission and next `implementer_resumed`) as legitimate pause checkpoints.
- New requirement **ON-009a** (needs-attention queue drain discipline). The `needs-attention` queue is operator-drained only — no auto-retry; phantom auto-retry is a structural violation.
- New requirement **ON-013d** (workflow_mode is observable but not runtime-tunable). Mode is sealed at claim time. Iteration cap is not operator-tunable at runtime.
- New requirement **ON-035a** (Review-loop observability). Observability of review-loop runs is via the JSONL event log only; no `harmonik review-status` subcommand. `harmonik status` inline-renders review-loop iteration state when the run is in review-loop mode.
- Revision-history row pending finalize.

---

## Cross-spec notes

- **§9.1 anchor reconciliation** across operator-nfr, beads-integration, execution-model, event-model, and process-lifecycle is deferred to the **integration pass** (next).
- **§10.x test-surface obligations** are deferred to the integration pass.
- **Revision-history row population** across all seven specs is deferred to finalize.
- All spec drafts are **additive** — no existing content was deleted; existing requirement IDs were preserved.
