# Event Model

```yaml
---
title: Event Model
spec-id: event-model
requirement-prefix: EV
status: reviewed
spec-category: foundation-cross-cutting
spec-shape: taxonomy-first
version: 0.7.1
spec-template-version: 1.1
owner: foundation-author
last-updated: 2026-07-14
depends-on:
  - architecture
  - execution-model
  - handler-contract
  - workspace-model
---
```

## 1. Purpose

This spec defines harmonik's event substrate: the typed event taxonomy, the common envelope, the on-disk JSONL format, the in-process pub/sub bus, the consumer taxonomy (synchronous / asynchronous / observer), the durability-class-driven fsync semantics, schema versioning, partial-ordering rules, the observational-vs-state-reconstruction replay split, and the non-blocking back-pressure policy. It is normative for every subsystem that emits or consumes events.

Events are the **observational stream**. They are NOT the state-reconstruction source: git plus Beads is (see [execution-model.md §4.7] and §4.5 here). Events are **lifecycle-boundary signals**, not agent internals; agent-internal detail lives in session logs (see [workspace-model.md §4.7]). Logs are a separate diagnostic channel covered by `quality-checks.md` once that spec lands.

## 2. Scope

### 2.1 In scope

- Common event envelope and required fields (UUIDv7 `event_id`, `schema_version`, `type`, timestamps, scoping, subsystem, trace context, payload).
- The typed event taxonomy for foundation MVH (complete cross-subsystem emission surface).
- On-disk JSONL format, single-file-per-project layout, and dead-letter layout.
- In-process pub/sub bus and the three-class consumer taxonomy.
- Durability classes, fsync cadence derived from class, event-loss-between-fsyncs-is-OK invariant, and producer idempotency obligation.
- Non-blocking async back-pressure policy (bounded per-consumer queue, shed rule, `bus_overflow` event).
- Clock / time-source rules: `timestamp_wall` vs. `timestamp_mono_nsec` vs. `event_id` ordering; monotonic UUIDv7 within a process.
- Schema versioning and the N-1 compatibility window, with an explicit breaking-change table (§6.4).
- Observational replay, divergence-evidence read (with post-crash-window guardrail), and the prohibition against state-reconstruction via JSONL.
- The Go tagged-union envelope + registry representation for event payloads.
- Redaction obligations for payload fields matching the secret-prefix rule.

### 2.2 Out of scope

- **State reconstruction.** Owned by [execution-model.md §4.7]. JSONL is observational per §4.5 here; state reconstruction walks git plus Beads.
- **Per-subsystem event-authorship rules** (which subsystem owns the WHEN of a given emission). Each subsystem spec declares its emissions; this spec is normative for payload shape, envelope, and taxonomy membership only. See §6.5 for the co-ownership rule.
- **Structured log format** (`log/slog` JSON records, diagnostic channel). Owned by `quality-checks.md` once that spec lands. Events are not logs per [docs/foundation/core-scope.md §2]; cross-reference tracked at OQ-EV-002.
- **Reconciliation category classification, investigator-agent contract, verdict vocabulary.** Owned by [reconciliation/spec.md §8, §4.4, §4.5]. This spec declares the shapes of reconciliation-related events only.
- **JSONL rotation policy.** Unbounded append works for MVH per [docs/foundation/core-scope.md §2]; rotation is deferred, tracked at OQ-EV-001.

## 3. Glossary

- **event** — a typed record emitted to the in-process pub/sub bus and durably appended to a JSONL file; one of the types declared in §8. (see §4.1)
- **envelope** — the set of common fields every event carries: `event_id`, `schema_version`, `type`, timestamps, `run_id` / `state_id` scoping, `source_subsystem`, `trace_context`, `payload`. (see §4.1, §6.1)
- **payload** — the type-specific body of an event; the shape is keyed by `Event.type`. (see §6.1)
- **event bus** — the in-process pub/sub mechanism that routes emitted events to registered consumers. (see §4.3)
- **consumer class** — one of `synchronous`, `asynchronous`, `observer`; the class determines failure-handling rules. (see §4.3)
- **durability class** — one of `fsync-boundary`, `ordinary`, `lossy-tail-ok`; declared on every §8 row and drives fsync cadence per EV-016. (see §4.4)
- **fsync boundary** — a point at which the JSONL writer calls `fsync(2)` to force durability; triggered by `fsync-boundary`-class events per §4.4.
- **event-loss window** — the span between two fsync boundaries during which a hard crash may lose emitted events. (see §4.4)
- **observational replay** — walking the JSONL tail to answer questions about what was seen between T1 and T2. Distinct from state reconstruction. (see §4.5)
- **divergence-evidence read** — a permitted JSONL read whose purpose is to detect that the three stores (git / Beads / JSONL) disagree. Its output is a typed divergence event, never reconstructed state. (see §4.5)
- **lifecycle-boundary signal** — an event marking a boundary of an entity's lifecycle (run started, agent ready, workspace merged). Distinct from intra-lifecycle detail (tool calls, thinking, per-token output), which belongs in session logs. (see §4.2)
- **dead-letter queue** — a persistent JSONL file holding events whose delivery to an asynchronous consumer failed after retry exhaustion. (see §4.3, §6.2)
- **TransitionKind** — the enum declared in [execution-model.md §4.10 EM-044] and [execution-model.md §6.1]. Values: `forward | local-patchback | architectural-rollback | policy-rollback | context-restore`. Event payloads use the enum by cross-reference; no redeclaration.
- **FailureClass** — the enum declared in [execution-model.md §8]. Values: `transient | structural | deterministic | canceled | budget_exhausted | compilation_loop`.
- **ErrorCategory** — the sentinel-error set declared in [handler-contract.md §4.5]. Values: `ErrTransient | ErrStructural | ErrDeterministic | ErrCanceled | ErrBudget | ErrSkillProvisioningFailed | ErrProtocolMismatch`. See §6.3 `run_failed` notes for how the two coexist.
- **WorkflowMode** — the enum declared in [execution-model.md §6.1]. Values: `single | review-loop | dot`. Surfaced on run-lifecycle event payloads (`run_started`, `run_completed`, `run_failed`) and on every §8.1a review-loop event payload via the optional `workflow_mode` field per §8.1 / §8.1a.
- **claude_session_id** — the Claude Code session identifier per [execution-model.md §3]. Distinct from this spec's `session_id` envelope/payload field. `session_id` (used pervasively on §8.3 agent-lifecycle event payloads) is a UUIDv7 minted by the handler per [handler-contract.md §4.1] and is opaque to non-handler consumers; `claude_session_id` is the Claude-Code-minted opaque string consumed by `claude --resume <id>`. The two MUST NOT be conflated; the review-loop event payloads at §8.1a carry `claude_session_id` explicitly to distinguish it.

## 8. Event taxonomy

Every event type declared below is part of the **complete cross-subsystem emission surface** for MVH. Subsystem-internal events are permitted and are not listed here; the normative rules governing internal-vs-cross-bus and amendment are EV-026 and EV-027. Payload field names in this section are declared here; precise Go struct field types live in §6.3. Every row carries a **durability class** (`F` = fsync-boundary, `O` = ordinary, `L` = lossy-tail-ok) driving §4.4's fsync cadence.

### 8.1 Run lifecycle

| # | Type | Dur | Emitter | Typical consumers | Payload fields |
|---|---|---|---|---|---|
| 8.1.1 | `run_started` | F | orchestrator-core | reconciliation, audit, observability, beads-integration | `run_id`, `workflow_id`, `workflow_version`, `bead_id?`, `workspace_path`, `input_ref` |
| 8.1.2 | `run_completed` | F | orchestrator-core | audit, beads-integration, observability, improvement-loop | `run_id`, `terminal_state_id`, `ended_at`, `summary?` |
| 8.1.3 | `run_failed` | F | orchestrator-core | reconciliation, audit, beads-integration, observability, improvement-loop | `run_id`, `terminal_state_id?`, `failure_class` (see §6.3), `error_category?`, `ended_at`, `reason` |
| 8.1.4 | `state_entered` | O | orchestrator-core | observability, improvement-loop | `run_id`, `state_id`, `node_id`, `entered_at` |
| 8.1.5 | `state_exited` | O | orchestrator-core | observability, improvement-loop | `run_id`, `state_id`, `node_id`, `exited_at`, `transition_id?` |
| 8.1.6 | `transition_event` | F | orchestrator-core | audit, observability, improvement-loop, reconciliation | `run_id`, `transition_id`, `from_state_id`, `to_state_id`, `commit_hash`, `transition_kind` (TransitionKind per §3) |
| 8.1.7 | `checkpoint_written` | F | orchestrator-core | reconciliation, audit, observability | `run_id`, `state_id`, `transition_id`, `commit_hash`, `bead_id?` |
| 8.1.8 | `outcome_emitted` | O | handler (via daemon watcher) | orchestrator-core, audit, improvement-loop | `run_id`, `session_id`, `node_id`, `outcome_status` (per [execution-model.md §4.1 EM-005]), `preferred_label?`, `suggested_next_ids?` |
| 8.1.9 | `sub_workflow_entered` | O | orchestrator-core | audit, observability | `run_id`, `parent_node_id`, `sub_workflow_name`, `sub_workflow_version` |
| 8.1.10 | `sub_workflow_exited` | O | orchestrator-core | audit, observability | `run_id`, `parent_node_id`, `sub_workflow_name`, `sub_workflow_version`, `terminal_outcome_status` |
| 8.1.11 | `node_dispatch_requested` | O | daemon-core | observability, audit | `run_id`, `node_id`, `requested_at`, `origin` (`workflow` / `reconciliation` / `operator`) |

> Section Axes (§8.1 Run lifecycle): All §8.1 event emissions are mechanism-tagged. Class F entries (`run_started`, `run_completed`, `run_failed`, `transition_event`, `checkpoint_written`) are fsync-backed; class O entries are best-effort. Default per-entry Axes — class F: `llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent`. Class O: `llm-freedom=none; io-determinism=best-effort; replay-safety=safe; idempotency=non-idempotent`. Replay-safety is `safe` for all: the JSONL append is idempotent at the content level and consumers tolerate duplicate delivery per EV-014b.

> **`workflow_mode` payload-field rule (§8.1 and §8.1a).** Every run-lifecycle event payload listed in §8.1 (`run_started`, `run_completed`, `run_failed`) and every review-loop event payload listed in §8.1a MUST carry an optional `workflow_mode ∈ {single, review-loop, dot}` field (per WorkflowMode in §3 glossary, cross-referenced to [execution-model.md §6.1]). The field surfaces the resolved `workflow_mode` per [execution-model.md §4.3 EM-012a]. The field is OPTIONAL on `run_started` / `run_completed` / `run_failed` for backward compatibility with v0.3.x consumers (a v0.3.x reader observing the new field treats it as additive per §6.4); it is REQUIRED on every §8.1a event payload. For most §8.1a events the value is always `review-loop`; the exception is `no_progress_detected` (§8.1a.5), which may carry `workflow_mode = "dot"` when emitted from the DOT cascade driver — see the §8.1a.5 normative note.

Axes: llm-freedom=none; io-determinism=best-effort; replay-safety=safe; idempotency=non-idempotent

### 8.1a Review-loop cycle

Five of the seven event types in this subsection (`implementer_resumed`, `reviewer_launched`, `reviewer_verdict`, `iteration_cap_hit`, `review_loop_cycle_complete`) are emitted only by runs whose `workflow_mode = review-loop` per [execution-model.md §4.3 EM-012, EM-015d]. The sixth — `no_progress_detected` (§8.1a.5) — is also emitted by DOT-mode runs (`workflow_mode = dot`) when the DOT cascade detects that the implementer made zero meaningful changes across iterations where no prior REQUEST_CHANGES verdict exists (EM-015e enforcement for DOT mode); in that case `workflow_mode = "dot"` on the payload. The seventh — `review_fixup_stalled` (§8.1a.7) — is emitted by both review-loop and DOT-mode runs when the implementer advances HEAD by zero commits after a REQUEST_CHANGES verdict (EM-015e, hk-m1wqp); it carries the reviewer flags from the prior verdict to enable targeted triage. Every event in this subsection MUST carry the standard envelope per §4.1 EV-001 (including `run_id`), AND MUST carry the appropriate `workflow_mode` on its payload per the §8.1 `workflow_mode` payload-field rule.

A single `run_id` covers the entire review-loop cycle per [execution-model.md §4.3 EM-015d]. Multiple `session_id` values (per §8.3) exist under the umbrella `run_id`, one per implementer-launch and one per reviewer-launch within the cycle. The `claude_session_id` payload field (per §3 glossary) is distinct from `session_id` and carries the Claude Code session identifier used for `claude --resume` continuity across implementer iterations.

| # | Type | Dur | Emitter | Typical consumers | Payload fields |
|---|---|---|---|---|---|
| 8.1a.1 | `implementer_resumed` | O | orchestrator-core | audit, observability, improvement-loop | `run_id`, `workflow_mode`, `session_id`, `claude_session_id`, `iteration_count`, `prior_verdict_summary` (String, ≤ 256 bytes; derived from prior reviewer `notes` per §6.3) |
| 8.1a.2 | `reviewer_launched` | O | orchestrator-core | audit, observability | `run_id`, `workflow_mode`, `session_id`, `claude_session_id`, `iteration_count` |
| 8.1a.3 | `reviewer_verdict` | F | orchestrator-core (after `.harmonik/review.json` read) | audit, observability, improvement-loop, beads-integration | `run_id`, `workflow_mode`, `session_id`, `claude_session_id`, `iteration_count`, plus agent-reviewer JSON schema v1 fields verbatim: `schema_version` (Integer), `verdict ∈ {APPROVE, REQUEST_CHANGES, BLOCK}`, `flags` (String[]), `notes` (String) |
| 8.1a.4 | `iteration_cap_hit` | O | orchestrator-core | audit, observability, operator-observability, beads-integration | `run_id`, `workflow_mode`, `iteration_count`, `cap_value` (Integer; = 3 at MVH), `final_verdict ∈ {REQUEST_CHANGES, BLOCK}` |
| 8.1a.5 | `no_progress_detected` | O | orchestrator-core | audit, observability, improvement-loop | `run_id`, `workflow_mode` (`"review-loop"` or `"dot"`; see §8.1a.5 normative note), `iteration_count`, `diff_hash_current` (String; SHA-256 hex), `diff_hash_prior` (String; SHA-256 hex) |
| 8.1a.6 | `review_loop_cycle_complete` | F | orchestrator-core | audit, observability, beads-integration, improvement-loop | `run_id`, `workflow_mode`, `final_iteration_count` (Integer), `completion_reason ∈ {approved, cap_hit, blocked, no_progress, fixup_stalled, error}` |
| 8.1a.7 | `review_fixup_stalled` | O | orchestrator-core | audit, observability, improvement-loop, beads-integration | `run_id`, `workflow_mode` (`"review-loop"` or `"dot"`), `iteration_count`, `reviewer_flags` (String[]; flags from the prior REQUEST_CHANGES verdict), `diff_hash_current` (String; SHA-256 hex), `diff_hash_prior` (String; SHA-256 hex) |

> Each of `implementer_resumed`, `reviewer_launched`, and `reviewer_verdict` carries both `session_id` (harmonik's internal UUIDv7, correlates with `agent_started`/`agent_completed`) and `claude_session_id` (the Claude Code subprocess session ID used for `--resume`).

> Note: `iteration_cap_hit` is class O, deliberately downgraded from the change-design's class-F recommendation; terminal routing weight rests on `review_loop_cycle_complete` (class F) per the §8.1a emission-ordering rule.

> **§8.1a.5 normative — `no_progress_detected` from DOT mode.** This event is emitted by DOT-mode runs (`workflow_mode = "dot"`) when the DOT cascade detects zero meaningful changes across iterations AND no prior REQUEST_CHANGES verdict exists. The DOT cascade driver (dot_cascade.go) computes `git diff <parent>..<head>` before each reviewer-class node dispatch; when `iteration_count ≥ 2` and the hash is unchanged, it emits either `no_progress_detected{workflow_mode="dot"}` (no prior REQUEST_CHANGES) or `review_fixup_stalled{workflow_mode="dot"}` (prior verdict was REQUEST_CHANGES — see §8.1a.7 normative note). DOT mode DOES NOT emit `review_loop_cycle_complete` after either event — the DOT cascade terminates directly (there is no iteration-level pairing event). Consumers that assert `review_loop_cycle_complete` MUST follow `no_progress_detected` or `review_fixup_stalled` MUST guard this assertion on `workflow_mode = "review-loop"`. **APPROVED-AND-DONE exemption (hk-8ps7q):** When `iteration_count ≥ 2`, HEAD is unchanged, the prior reviewer verdict was `APPROVE`, AND HEAD has advanced past the run baseline `parentSHA` (committed work exists from a prior iteration), the DOT cascade MUST complete the run as success WITHOUT emitting `no_progress_detected` or `review_fixup_stalled` — the approved commit is final and MUST NOT be stranded. The run terminates directly via `run_completed{outcome.status=SUCCESS}`. This exemption is gated on `!prevAgenticNodeWasReviewer` (to preserve multi-reviewer fan-out correctness per hk-2vpj): when the immediately preceding agentic node was itself a reviewer, the exemption does NOT fire and the next reviewer runs as required.

> Section Axes (§8.1a Review-loop cycle): All §8.1a event emissions are mechanism-tagged. Class F entries (`reviewer_verdict`, `review_loop_cycle_complete`) are fsync-backed because their loss orphans the cycle's terminal-routing decision; loss of either would force reconciliation into work greater than the cost of a disk sync per EV-016. Class O entries are best-effort. Default per-entry Axes — class F: `llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent`. Class O: `llm-freedom=none; io-determinism=best-effort; replay-safety=safe; idempotency=non-idempotent`.

> Emission ordering (§8.1a). For a single **review-loop** iteration, the emission order MUST be: (a) `implementer_resumed` (on every implementer launch after iteration 1; iteration 1's implementer is dispatched via `run_started` per §8.1 with no `implementer_resumed`); (b) `reviewer_launched`; (c) on no-progress detection BEFORE step (b): if a prior REQUEST_CHANGES verdict exists, emit `review_fixup_stalled` followed directly by `review_loop_cycle_complete{completion_reason=fixup_stalled}`, skipping (b); otherwise emit `no_progress_detected` followed directly by `review_loop_cycle_complete{completion_reason=no_progress}`, skipping (b); (d) `reviewer_verdict` (after the verdict file is read and validated); (e) on `REQUEST_CHANGES` with iterations remaining, loop back to (a) with `iteration_count + 1`; on cap-hit, emit `iteration_cap_hit` then `review_loop_cycle_complete{completion_reason=cap_hit}`; on `BLOCK`, emit `review_loop_cycle_complete{completion_reason=blocked}`; on `APPROVE`, emit `review_loop_cycle_complete{completion_reason=approved}`. The terminal `run_completed` / `run_failed` per §8.1 MUST follow `review_loop_cycle_complete`, never precede it. For **DOT-mode** no-progress termination: three paths — (1) prior verdict was `REQUEST_CHANGES`: emit `review_fixup_stalled{workflow_mode="dot"}` and terminate via needs-attention; (2) prior verdict was neither `REQUEST_CHANGES` nor `APPROVE` (or no verdict yet): emit `no_progress_detected{workflow_mode="dot"}` and terminate via needs-attention; (3) prior verdict was `APPROVE` AND HEAD advanced past the run baseline `parentSHA` (APPROVED-AND-DONE exemption per hk-8ps7q): emit NO no-progress event and COMPLETE the run as success. In all DOT cases `review_loop_cycle_complete` is NOT emitted, and `run_completed` / `run_failed` follows directly.

> Reviewer-verdict schema reuse (§8.1a.3). The `reviewer_verdict` payload's `schema_version`, `verdict`, `flags`, and `notes` fields conform verbatim to the `agent-reviewer` skill's JSON verdict schema v1 (referenced from the project skill registry). The daemon MUST read `.harmonik/review.json` from the run's worktree (path owned by [workspace-model.md §4.7]), MUST validate the file against schema v1 (the `schema_version` field MUST equal 1; `verdict` MUST be in `{APPROVE, REQUEST_CHANGES, BLOCK}`; `flags` MUST be a String array; `notes` MUST be a String), AND MUST emit `reviewer_verdict` only after successful validation. A malformed verdict file MUST cause the daemon to emit `review_loop_cycle_complete{completion_reason=error}` and route the run to the `needs-attention` close path per [execution-model.md §4.3 EM-015e]; the daemon MUST NOT emit `reviewer_verdict` with a malformed payload. The reviewer-hardening rule that `notes` MUST include file:line citations for any `REQUEST_CHANGES` verdict is enforced upstream of event emission (reviewer prompt + adapter validation per the agent-reviewer skill); event-model records what was emitted but does not re-validate citation form.

Axes: llm-freedom=none; io-determinism=best-effort; replay-safety=safe; idempotency=non-idempotent

### 8.2 Control-point lifecycle

| # | Type | Dur | Emitter | Typical consumers | Payload fields |
|---|---|---|---|---|---|
| 8.2.1 | `hook_fired` | O | hook-system | audit, observability | `run_id?`, `hook_name`, `triggering_event_id`, `side_effect_descriptor` |
| 8.2.2 | `hook_failed` | O | hook-system | audit, observability, reconciliation | `run_id?`, `hook_name`, `triggering_event_id`, `error_category`, `reason` |
| 8.2.3 | `hook_verdict_persisted` | O | hook-system | audit, observability | `run_id`, `hook_invocation_id`, `hook_name`, `verdict_path`, `commit_hash` |
| 8.2.4 | `gate_allowed` | O | orchestrator-core | audit, observability | `run_id`, `gate_name`, `reason?` |
| 8.2.5 | `gate_denied` | O | orchestrator-core | audit, observability, reconciliation | `run_id`, `gate_name`, `reason` |
| 8.2.6 | `gate_escalated` | O | orchestrator-core | audit, operator-observability | `run_id`, `gate_name`, `reason?` |
| 8.2.7 | `guard_reordered` | O | orchestrator-core | audit, observability | `run_id`, `guard_name`, `edge_set_before`, `edge_set_after` |
| 8.2.8 | `guard_failed` | O | orchestrator-core | audit, observability, reconciliation | `run_id`, `guard_name`, `error_category`, `reason` |
| 8.2.9 | `control_points_registered` | O | control-points (S02) | audit, observability | `count`, `started_at` |
| 8.2.10 | `control_points_registration_started` | O | control-points (S02) | audit, observability | `batch_id`, `started_at` |
| 8.2.11 | `verdict_envelope_mismatch` | O | control-points (S02) | reconciliation, audit, observability | `run_id`, `control_point_name`, `transition_id?`, `event_id_ref?`, `stored_envelope_hash`, `current_envelope_hash`, `detected_at` |
| 8.2.12 | `policy_expression_exceeded_cost` | F | control-points (S02) | reconciliation, audit, observability | `run_id?`, `control_point_name`, `bound_fired` (enum: `ast_steps` / `wall_clock`), `io_determinism` (enum: `deterministic` / `best-effort`), `aborted_at` |
| 8.2.13 | `gate_definition_drift` | F | orchestrator-core | reconciliation, audit, observability | `run_id`, `gate_name`, `prior_envelope_hash`, `current_envelope_hash`, `changed_inputs` |
| 8.2.14 | `gate_redefined_under_cat_6` | F | orchestrator-core | reconciliation, audit, observability | `run_id`, `gate_name`, `prior_decision`, `new_decision`, `cat_6_verdict_id` |

> Section Axes (§8.2 Control-point lifecycle): All §8.2 event emissions are mechanism-tagged. Class O entries use best-effort io-determinism; §8.2.12 (`policy_expression_exceeded_cost`), §8.2.13 (`gate_definition_drift`), and §8.2.14 (`gate_redefined_under_cat_6`) are class F (fsync-backed, deterministic). Default per-entry: `llm-freedom=none; io-determinism=best-effort; replay-safety=safe; idempotency=non-idempotent`. Exceptions — §8.2.12, §8.2.13, §8.2.14: `llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent`.

Axes: llm-freedom=none; io-determinism=best-effort; replay-safety=safe; idempotency=non-idempotent

### 8.3 Agent / handler lifecycle

| # | Type | Dur | Emitter | Typical consumers | Payload fields |
|---|---|---|---|---|---|
| 8.3.1 | `agent_ready` | O | handler (via daemon watcher) | orchestrator-core, observability | `run_id`, `session_id`, `capabilities[]` |
| 8.3.2 | `agent_started` | O | handler (via daemon watcher) | audit, observability | `run_id`, `session_id`, `node_id`, `agent_type`, `started_at` |
| 8.3.3 | `agent_output_chunk` | L | handler (via daemon watcher) | improvement-loop, observability | `run_id`, `session_id`, `chunk_index`, `bytes_emitted`, `chunk_digest?` |
| 8.3.4 | `agent_completed` | O | handler (via daemon watcher) | orchestrator-core, audit, observability | `run_id`, `session_id`, `ended_at`, `exit_code` (observational), `outcome_ref` |
| 8.3.5 | `agent_failed` | O | handler (via daemon watcher) | orchestrator-core, reconciliation, audit | `run_id`, `session_id`, `ended_at`, `error_category`, `reason` |
| 8.3.6 | `agent_rate_limit_status` | O | handler (via daemon watcher) | orchestrator-core, observability | `run_id`, `session_id`, `status` (`active` / `cleared`), `rate_limit_source?`, `retry_after_seconds?`, `changed_at` |
| 8.3.7 | `session_log_location` | O | handler (via daemon watcher) | audit | `run_id`, `session_id`, `node_id`, `agent_type`, `log_path`, `log_format`, `bead_id?` |
| 8.3.8 | `skills_provisioned` | O | handler (via daemon watcher) | audit, observability | `run_id`, `session_id`, `skills[]` (each: `name`, `source_path`, `version?`), `rejected_skills[]?` (each: `name`, `reject_reason`) |
| 8.3.9 | `handler_capabilities` | O | handler (via daemon watcher) | orchestrator-core | `run_id`, `session_id`, `protocol_versions_supported[]` |

> §8.3.8 (`skills_provisioned`). The `skills[]` list names ONLY the skills successfully installed before `agent_ready`; it is the authoritative record of what actually ran. The optional `rejected_skills[]` list names skills that failed pre-provisioning policy checks per [handler-contract.md §4.11.HC-048b] (egress-policy or workspace-escape violation), each entry carrying a `name` and a `reject_reason` string (e.g., `"egress_domain_not_whitelisted"`, `"path_escapes_workspace"`). `rejected_skills[]` is absent when no skills were rejected; an empty `skills[]` with a non-empty `rejected_skills[]` is valid when all required skills failed. Additive extension to §8.3: `rejected_skills[]?` is an optional field; N-1 readers that do not know the field tolerate its presence per the schema-evolution rule of §6.4.
| 8.3.10 | `agent_warning_silent_hang` | O | handler (via daemon watcher) | orchestrator-core, observability | `run_id`, `session_id`, `threshold_seconds`, `last_progress_event_at`, `fsm_state` |
| 8.3.11 | `agent_resumed_after_warning` | O | handler (via daemon watcher) | orchestrator-core, observability | `run_id`, `session_id`, `resumed_at`, `warning_duration_seconds` |
| 8.3.12 | `agent_soft_terminating` | O | handler (via daemon watcher) | orchestrator-core, audit | `run_id`, `session_id`, `threshold_seconds`, `started_at` |
| 8.3.13 | `agent_hard_terminating` | O | handler (via daemon watcher) | orchestrator-core, audit | `run_id`, `session_id`, `threshold_seconds`, `started_at` |
| 8.3.14 | `lifecycle_transition` | O | handler (via daemon watcher) | orchestrator-core, audit, cognition-loop | `run_id`, `session_id`, `from_state`, `to_state`, `reason`, `transitioned_at` |

> §8.3.14 (`lifecycle_transition`). Emitted by the watcher goroutine on every `LifecycleState` machine transition per [handler-contract.md §4.13 HC-067]. Payload fields: `from_state` and `to_state` are values from the `LifecycleState` enum (HC-064); `reason` is a `TransitionReason` string from the transition-history ring; `transitioned_at` is wall-clock at transition. Class O (reconstructible from the per-session transition-history ring in the daemon's in-memory state; the ring is the authoritative in-process surface per HC-067). Additive extension to §8.3; schema-version bump per §6.4 row 1 (add optional field); N-1 readers tolerate unknown fields. Cross-spec: [handler-contract.md §4.13 HC-065] owns the valid-transition table; this spec owns the WHEN and SHAPE. Cognition-loop consumers use `lifecycle_transition` to detect `Ready→Failed(silent_hang)` transitions as Tier-2 judgment wakes, correlating with [event-model.md §8.3.10 `agent_warning_silent_hang`].

> Section Axes (§8.3 Agent / handler lifecycle): All §8.3 event emissions are mechanism-tagged. All entries are class O or L (best-effort). §8.3.3 (`agent_output_chunk`) is class L (lossy; observed only). §8.3.14 (`lifecycle_transition`) is class O (best-effort; in-process ring is authoritative per HC-067). Default per-entry: `llm-freedom=none; io-determinism=best-effort; replay-safety=safe; idempotency=non-idempotent`.

Axes: llm-freedom=none; io-determinism=best-effort; replay-safety=safe; idempotency=non-idempotent

### 8.4 Budget lifecycle

| # | Type | Dur | Emitter | Typical consumers | Payload fields |
|---|---|---|---|---|---|
| 8.4.1 | `budget_warning` | O | agent-runner (S04) | orchestrator-core, observability | `run_id`, `session_id?`, `budget_ref`, `threshold_fraction`, `remaining`, `role`, `node_id`, `category` |
| 8.4.2 | `budget_accrual` | L | handler (via daemon watcher) | improvement-loop, observability | `run_id`, `session_id`, `chunk_index?`, `cost_units`, `cost_basis`, `role`, `node_id`, `category`, `amount`, `delegation_path?` |
| 8.4.3 | `budget_exhausted` | O | agent-runner (S04); cognition-loop (flywheel) | orchestrator-core, audit | `run_id?`, `session_id?`, `budget_ref`, `budget_scope?`, `attempted_dispatch_cost?`, `spent_usd?`, `cap_usd?`, `role?`, `node_id?`, `category?` |

> Section Axes (§8.4 Budget lifecycle): All §8.4 event emissions are mechanism-tagged. §8.4.2 (`budget_accrual`) is class L (lossy); others are class O. Default per-entry: `llm-freedom=none; io-determinism=best-effort; replay-safety=safe; idempotency=non-idempotent`.

> NORMATIVE (§8.4 attribution fields): The fields `role`, `node_id`, `category`, and `amount` on `budget_accrual` (§8.4.2) are the on-wire realization of the ON-049 attribution 5-tuple `(run_id, role, node_id, category, amount)` per [operator-nfr.md §4.11 ON-049]. `role` and `node_id` on `budget_warning` (§8.4.1) and `budget_exhausted` (§8.4.3) carry the same semantics; they are OPTIONAL (`?`) on `budget_exhausted` because the account-scoped variant (per the INFORMATIVE note below) is run-agnostic and may lack `run_id`, `role`, and `node_id`. `delegation_path?` on `budget_accrual` is REQUIRED for steps where the ControlPoint's evaluator is cognition-tagged per [control-points.md §4.8 CP-039]; absent otherwise. These are additive fields; N-1 readers tolerate unknown fields per §6.4.

> INFORMATIVE (§8.4.3 producer set): the cognition loop ([cognition-loop.md §4.11 CL-090]) is a registered producer of `budget_exhausted` for the **account-scoped** variant — `budget_scope = handler_account` (the Budget primitive `scope` value per [control-points.md §4.5 CP-022]). The account-scoped exhaustion is run-agnostic: `run_id` and `session_id` MAY be absent, and `spent_usd` / `cap_usd` carry the per-day meter state. This variant is the trip that the budget-exhaustion handler-pause policy consumes per [handler-pause.md §4 HP-012]. The per-run variant emitted by agent-runner (S04) at dispatch (per [control-points.md §4.5 CP-023]) carries `run_id` and `attempted_dispatch_cost` and is per-bead, not handler-fatal. The event class (O, ordinary) and mechanism tag are unchanged for both variants.

Axes: llm-freedom=none; io-determinism=best-effort; replay-safety=safe; idempotency=non-idempotent

### 8.5 Workspace lifecycle

| # | Type | Dur | Emitter | Typical consumers | Payload fields |
|---|---|---|---|---|---|
| 8.5.1 | `workspace_created` | O | workspace-manager (S06) | audit, observability | `workspace_id`, `path`, `branch_name`, `parent_commit` |
| 8.5.2 | `workspace_leased` | O | workspace-manager (S06) | orchestrator-core, audit | `workspace_id`, `run_id`, `leased_at` |
| 8.5.3 | `workspace_merge_status` | F | workspace-manager (S06) | audit, observability, beads-integration | `workspace_id`, `run_id`, `status` (`pending` / `merged`), `source_branch`, `target_branch`, `merge_commit_hash?`, `changed_at` |
| 8.5.4 | `workspace_discarded` | O | workspace-manager (S06) | audit, observability | `workspace_id`, `run_id`, `reason` |
| 8.5.5 | `workspace_interrupted` | O | reconciliation detector (per [reconciliation/spec.md §8]) | reconciliation, audit, operator-observability | `workspace_id`, `run_id`, `detected_at`, `category` (Cat 6) |
| 8.5.6 | `merge_conflict_escalation` | O | workspace-manager (S06) | operator-observability, audit | `workspace_id`, `run_id`, `conflict_paths[]`, `escalated_at` |

> Section Axes (§8.5 Workspace lifecycle): All §8.5 event emissions are mechanism-tagged. §8.5.3 (`workspace_merge_status`) is class F (fsync-backed, deterministic); others are class O. Default per-entry — class O: `llm-freedom=none; io-determinism=best-effort; replay-safety=safe; idempotency=non-idempotent`. Exception — §8.5.3: `llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent`.

Axes: llm-freedom=none; io-determinism=best-effort; replay-safety=safe; idempotency=non-idempotent

### 8.6 Reconciliation lifecycle

| # | Type | Dur | Emitter | Typical consumers | Payload fields |
|---|---|---|---|---|---|
| 8.6.1 | `reconciliation_started` | O | daemon-core | reconciliation-monitoring, audit | `reconciliation_run_id`, `trigger` (`startup` / `on-demand` / `divergence-detected`) |
| 8.6.2 | `reconciliation_category_assigned` | O | daemon-core | reconciliation-monitoring, audit, improvement-loop | `reconciliation_run_id`, `target_run_id?`, `category` (Cat 0..6), `evidence_ref`, `post_crash_window?` |
| 8.6.3 | `reconciliation_verdict_emitted` | O | daemon-core | reconciliation-monitoring, audit | `investigator_run_id`, `target_run_id`, `verdict` (per [reconciliation/spec.md §4.5]), `rationale?` |
| 8.6.4 | `reconciliation_verdict_executed` | O | daemon-core | reconciliation-monitoring, audit, improvement-loop | `investigator_run_id`, `target_run_id`, `verdict`, `executed_at_timestamp`, `action_summary` |
| 8.6.5 | `reconciliation_verdict_malformed` | O | daemon-core | reconciliation-monitoring, audit | `investigator_run_id`, `target_run_id`, `malformation_reason`, `raw_verdict_excerpt` |
| 8.6.6 | `reconciliation_budget_exhausted` | O | daemon-core | reconciliation-monitoring, audit, improvement-loop | `run_id`, `workflow_id`, `budget_seconds`, `elapsed_seconds` |
| 8.6.7 | `reconciliation_verdict_stale` | O | daemon-core | reconciliation-monitoring, audit | `investigator_run_id`, `target_run_id`, `snapshot_token`, `current_state`, `divergence_reason` |
| 8.6.8 | `store_divergence_detected` | O | daemon-core | reconciliation, audit | `run_id?`, `bead_id?`, `divergence_kind`, `evidence_ref`, `post_crash_window`, `corroboration` (`git-corroborated` / `beads-corroborated`) |
| 8.6.9 | `operator_escalation_required` | O | daemon-core | operator-observability, audit | `target_run_id?`, `reason` (enum, widened per §6.3), `reference_commits[]?` |
| 8.6.10 | `divergence_inconclusive` | O | daemon-core | reconciliation, audit | `run_id?`, `bead_id?`, `evidence_ref`, `post_crash_window`, `reason` (enum: `no_authority_reference` / `authority_unavailable`) |
| 8.6.11 | `reconciliation_dispatch_deduplicated` | O | daemon-core | reconciliation-monitoring, audit | `target_run_id`, `existing_investigator_run_id?`, `dedup_at` |
| 8.6.12 | `reconciliation_detector_panic` | O | daemon-core | reconciliation-monitoring, audit, operator-observability | `detector_class`, `error_class`, `panicked_at` |
| 8.6.13 | `reconciliation_verdict_execution_retry` | O | daemon-core | reconciliation-monitoring, audit | `target_run_id`, `attempt`, `retried_at` |
| 8.6.14 | `bead_terminal_transition_recovered` **(post-MVH)** | O | beads-adapter | observability, audit | `bead_id`, `op` (enum: `claim` / `close` / `reopen`), `idempotency_key`, `recovered_at` |

> **(post-MVH)** §8.6.14 `bead_terminal_transition_recovered` is reserved for a future revision per OQ-BI-008. The MVH adapter emits a structured-log record per [operator-nfr.md §4.9 ON-035] for adapter-recovery observability rather than this event; the entry is reserved here so the type identifier is burned for future use by the BI adapter and not reused for any other purpose. No MVH conformance obligation attaches to §8.6.14.

> Section Axes (§8.6 Reconciliation lifecycle): All §8.6 event emissions are mechanism-tagged. All entries are class O (best-effort). Default per-entry: `llm-freedom=none; io-determinism=best-effort; replay-safety=safe; idempotency=non-idempotent`.

Axes: llm-freedom=none; io-determinism=best-effort; replay-safety=safe; idempotency=non-idempotent

### 8.7 Operator-control and daemon lifecycle

| # | Type | Dur | Emitter | Typical consumers | Payload fields |
|---|---|---|---|---|---|
| 8.7.1 | `daemon_started` | F | daemon-core | observability, audit | `started_at`, `pid`, `binary_commit_hash` |
| 8.7.2 | `daemon_ready` | F | daemon-core | observability, audit, operator-nfr | `ready_at`, `ready_at_ns_since_boot`, `investigator_run_ids[]` |
| 8.7.3 | `daemon_shutdown` | F | daemon-core | observability, audit | `shutdown_at`, `shutdown_at_ns_since_boot`, `mode` (`graceful` / `immediate`) |
| 8.7.4 | `daemon_startup_failed` | F | daemon-core | operator-observability, audit | `failed_at`, `exit_code`, `failure_mode` (per [operator-nfr.md §8]), `required_migration_release?` (REQUIRED when `failure_mode = "queue-format-unsupported"` per ON-016, omitted otherwise) |
| 8.7.5 | `daemon_degraded` | O | daemon-core | operator-observability, audit | `detected_at`, `reason` (enum per §6.3, exhaustive: `rto_breach` / `reconstruction_notify` / `clock_regression` / `cat0_post_ready` / `infrastructure_unavailable` / `silent_hang_aggregate`) |
| 8.7.6 | `operator_pause_status` | O | daemon-core | observability, audit | `status` (`pausing` / `paused`), `changed_at`, `operator_id?` |
| 8.7.7 | `operator_resuming` | O | daemon-core | observability, audit | `resumed_at` |
| 8.7.8 | `operator_stopped` | O | daemon-core | observability, audit | `stopped_at`, `mode` (`graceful` / `immediate`) |
| 8.7.9 | `operator_upgrading` | O | daemon-core | observability, audit | `upgrade_version`, `started_at` |
| 8.7.10 | `operator_upgrade_completed` | F | daemon-core | observability, audit | `upgrade_version`, `completed_at`, `binary_commit_hash` |
| 8.7.11 | `operator_upgrade_rejected` | O | daemon-core | operator-observability, audit | `upgrade_version?`, `rejected_at`, `reason` (`hash_mismatch` / `schema_incompatible` / `not_paused`) |
| 8.7.12 | `operator_command_rejected` | O | daemon-core | operator-observability, audit | `command`, `current_state`, `rejected_at` |
| 8.7.13 | `dispatch_deferred` | O | daemon-core | observability, audit | `run_id?`, `node_id?`, `reason` (`machine_ceiling_exhausted` / other), `deferred_at` |
| 8.7.14 | `daemon_orphan_sweep_completed` | O | daemon-core | observability, audit | `tmux_sessions_killed`, `locks_cleared`, `subprocesses_killed`, `swept_at`; additive fields (§6.4 row 1, non-breaking): `tmux_windows_killed` (PL-021c), `tmux_kill_window_survivors []int` (PL-021c §6), `br_subprocesses_killed`, `reconciliation_locks_removed`, `stale_intents_observed` (PL-006), `bead_in_progress_reset` (PL-006/hk-iuaed.2), `coordinator_sessions_skipped` (PL-006d), `crew_sessions_skipped` (PL-006d fleet-portability), `captain_sessions_skipped` (PL-006d fleet-portability); consumers MUST tolerate unknown integer fields per EV-029 |
| 8.7.15 | `infrastructure_unavailable` | O | daemon-core | operator-observability, audit | `failed_prerequisite` (enum per §6.3), `detail_string`, `retry_count` |
| 8.7.16 | `operator_command_failed` | O | daemon-core | operator-observability, audit | `command` (enum: `pause` / `stop` / `upgrade` / `attach` / `enqueue`), `failure_class` (enum per §6.3), `run_id?`, `failed_at` |
| 8.7.17 | `operator_escalation_cleared` | O | daemon-core | operator-observability, audit | `target_run_id?`, `cleared_at`, `clearance_reason` (enum: `verdict_executed` / `manual_clear` / `superseded`) |

> Section Axes (§8.7 Operator-control and daemon lifecycle): All §8.7 event emissions are mechanism-tagged. §8.7.1 (`daemon_started`), §8.7.2 (`daemon_ready`), §8.7.3 (`daemon_shutdown`), §8.7.4 (`daemon_startup_failed`), and §8.7.10 (`operator_upgrade_completed`) are class F (fsync-backed, deterministic); all others are class O. Default per-entry — class F: `llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent`. Class O: `llm-freedom=none; io-determinism=best-effort; replay-safety=safe; idempotency=non-idempotent`.

Axes: llm-freedom=none; io-determinism=best-effort; replay-safety=safe; idempotency=non-idempotent

### 8.8 Observability and bus-internal

| # | Type | Dur | Emitter | Typical consumers | Payload fields |
|---|---|---|---|---|---|
| 8.8.1 | `metric` | L | any subsystem | observability | `metric_name`, `value`, `unit?`, `labels?` |
| 8.8.2 | `consumer_failed` | O | bus-internal | observability, audit | `consumer_name`, `event_type`, `event_id`, `error_category` (incl. `overflow`), `failed_at` |
| 8.8.3 | `dead_letter_enqueued` | O | bus-internal | observability, audit | `consumer_name`, `event_type`, `original_event_id`, `retries_attempted`, `enqueued_at` |
| 8.8.4 | `bus_overflow` | O | bus-internal | observability, audit, operator-observability | `consumer_name`, `event_type`, `event_id`, `queue_depth`, `shed_at`, `shed_policy` (`fsync-spilled` / `ordinary-dropped` / `lossy-dropped`) |
| 8.8.5 | `redaction_failed` | O | bus-internal | operator-observability, audit | `event_type`, `run_id?`, `error_class`, `failed_at` |
| 8.8.6 | `bead_label_conflict` | O | daemon-core (claim path) | reconciliation, audit, observability | `bead_id`, `conflicting_labels[]` (String[]), `fallback_action` (String), `detected_at` |

> §8.8.6 emission rule. `bead_label_conflict` is emitted by the daemon's claim path during workflow-mode resolution per [execution-model.md §4.3 EM-012a] when (a) a bead carries more than one `workflow:<mode>` label or (b) a bead carries a `workflow:<mode>` label whose `<mode>` value is not in `{single, review-loop, dot}`. In either case the daemon MUST treat the tier-1 input as absent AND MUST emit `bead_label_conflict` before continuing the precedence walk. The event is class O because the resolution path falls through to a defined tier-2/3/4 result; the conflict is observational evidence rather than a routing-gating decision.

> Section Axes (§8.8 Observability and bus-internal): All §8.8 event emissions are mechanism-tagged. §8.8.1 (`metric`) is class L (lossy); others are class O. All entries use best-effort io-determinism. Default per-entry: `llm-freedom=none; io-determinism=best-effort; replay-safety=safe; idempotency=non-idempotent`.

Axes: llm-freedom=none; io-determinism=best-effort; replay-safety=safe; idempotency=non-idempotent

### 8.9 Acceptance criteria for candidate event types

A candidate event type is accepted into §8 if and only if **all** of:

- (a) At least one cross-subsystem consumer exists (cross-subsystem boundary criterion).
- (b) It is a lifecycle-boundary signal rather than an intra-lifecycle detail (boundary criterion).
- (c) At least one cross-subsystem consumer requires per-chunk or per-boundary access rather than a single summary event (granularity criterion).
- (d) Its payload schema is defined with typed Go fields (see §6.3).
- (e) It carries the four-axis tags plus mechanism/cognition tag (see §5 EV-INV-005) AND a durability class (§4.4).
- (f) It specifies its replay side-effect classification per §4.5.
- (g) At least one sibling spec's emission section cites the event by name. The `metric` entry (§8.8.1) is the single escape-hatch exception; its use is free but payload-shape-bounded.
- (h) Paired-phase lifecycles (pending/resolved, active/cleared) MUST NOT split into two event types; use a single type with a `status` field. Existing merges: `agent_rate_limit_status`, `workspace_merge_status`, `operator_pause_status`. Emitters of status-carrying events MUST emit only on `status` transitions (entry into a new phase); successive emissions with identical `status` for the same scoped entity are forbidden — this rule prevents keepalive-style re-emission that would force consumers back into the correlation-deduplication logic the merge was designed to eliminate. The `changed_at` field MUST carry millisecond resolution to distinguish rapid transitions. §8.9(h) applies to paired-phase *lifecycles* only; gate verdicts (`gate_allowed` / `gate_denied` / `gate_escalated`) are terminal-distinct outcomes, not sequential phases of the same lifecycle, and remain split per control-points §6.5.

The `agent_output_chunk` and `budget_accrual` types remain fine-grained in MVH because the improvement-loop subsystem requires per-chunk cost attribution and mid-run signals; collapsing them to summary events would lose information. A log-level / filter mechanism for suppressing chunk events at consumer boundaries is a future refinement slot.

> **Per-tool-call lifecycle events are excluded at v1.** The `tool_command_completed` event — a candidate that arose from the attractor-parity T7 tool-node feature ([workflow-graph.md §4 WG-039]) — was considered and deferred. At v1, tool nodes (non-agentic nodes carrying `tool_command`) reuse existing node-lifecycle observability: `node_dispatch_requested` (§8.1.11) covers dispatch, `outcome_emitted` (§8.1.8) carries the terminal result, and `run_completed` / `run_failed` (§8.1) carry the run boundary. A bespoke `tool_command_completed` event was found to fail §8.9(b) (it is an intra-lifecycle detail rather than a lifecycle-boundary signal) and §8.9(a) (no cross-subsystem consumer exists at v1). If operator demand for per-command observability surfaces, `tool_command_completed` MAY be added via the §4.6 amendment protocol, provided it satisfies all §8.9 criteria at the time of addition. The type name `tool_command_completed` is reserved and MUST NOT be reused for any other purpose.

### 8.10 Queue lifecycle

| # | Type | Dur | Emitter | Typical consumers | Payload fields |
|---|---|---|---|---|---|
| 8.10.1 | `queue_submitted` | F | queue | audit, observability, orchestrator-core | `queue_id`, `submitted_at`, `group_count`, `total_bead_count`, `schema_version` (queue.json) |
| 8.10.2 | `queue_group_started` | O | queue | audit, observability, orchestrator-core | `queue_id`, `group_index`, `group_kind` (enum: `wave` / `stream`), `item_count`, `started_at` |
| 8.10.3 | `queue_group_completed` | F | queue | audit, observability, orchestrator-core | `queue_id`, `group_index`, `final_status` (enum: `complete-success` / `complete-with-failures`), `success_count`, `fail_count`, `completed_at` |
| 8.10.4 | `queue_paused` | F | queue | audit, observability, orchestrator-core, operator-observability | `queue_id`, `group_index`, `fail_count`, `paused_at`, `reason` (enum: `group_failure` / `operator_drain`) |
| 8.10.5 | `queue_appended` | O | queue | audit, observability, orchestrator-core | `queue_id`, `group_index`, `appended_bead_ids` (String[]), `appended_at` |
| 8.10.6 | `queue_item_deferred_for_ledger_dep` | O | queue | audit, observability, orchestrator-core | `queue_id`, `group_index`, `bead_id`, `blocker_bead_id`, `detected_at` |
| 8.10.7 | `queue_item_reconciled` | F | queue | audit, observability, orchestrator-core | `queue_id`, `group_index`, `bead_id`, `reason` (enum: `claim_write_lost`), `reconciled_at` |

> Section Axes (§8.10 Queue lifecycle): All §8.10 event emissions are mechanism-tagged. Class F entries (`queue_submitted`, `queue_group_completed`, `queue_paused`, `queue_item_reconciled`) are fsync-backed because loss orphans the queue's execution plan, the group-boundary advance decision, the hard pause landmark, or the startup reconciliation correction respectively (per EV-016). Class O entries (`queue_group_started`, `queue_appended`, `queue_item_deferred_for_ledger_dep`) are best-effort: each is reconstructible from a sibling class-F landmark (group_started from the predecessor's `queue_group_completed` plus queue.json; appended from queue.json mutation history; deferred from ledger state plus queue.json). Default per-entry Axes — class F: `llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent`. Class O: `llm-freedom=none; io-determinism=best-effort; replay-safety=safe; idempotency=non-idempotent`.

> Emission ordering (§8.10). For a single queue's lifecycle the emission order MUST be: (a) `queue_submitted` (one); (b) immediately followed by `queue_group_started{group_index:0}` plus any `queue_item_deferred_for_ledger_dep` events arising from submit-time validation per [queue-model.md §6 QM-025]; (c) per dispatched bead, the §8.1 chain `run_started{queue_id, queue_group_index} → … → run_completed{queue_id, queue_group_index}` OR `run_failed{queue_id, queue_group_index}` carries per-item lifecycle (no separate `queue_item_*` emission exists); (d) when every item in the active group reaches a terminal state, `queue_group_completed`; (e) if any item in that group failed, `queue_paused{reason:group_failure}` MUST follow `queue_group_completed` in that emission order (group_completed first, paused second); (f) otherwise, `queue_group_started{group_index:N+1}` follows; (g) `queue_appended` MAY interleave at any time on a stream group whose status is `pending` or `active` per [queue-model.md §7]. The §8.1 terminal `run_completed` / `run_failed` for the last item of a group MUST precede `queue_group_completed` for that group, never follow it.

Axes: llm-freedom=none; io-determinism=best-effort; replay-safety=safe; idempotency=non-idempotent

### 8.11 Handler-pause lifecycle

Three new event types introduced by the handler-pause Phase-1 implementation (normative spec: [specs/handler-pause.md](handler-pause.md)). `handler_paused` and `handler_resumed` are Class F because loss would orphan the pause-state landmark; the reconciliation investigator depends on these events to respect pauses across restarts. `queue_item_held_for_handler_pause` is Class O: the held state is reconstructible from `handler-state.json` plus queue.json at startup.

| # | Type | Dur | Emitter | Typical consumers | Payload fields |
|---|---|---|---|---|---|
| 8.11.1 | `handler_paused` | F | daemon-core (HandlerPauseController) | orchestrator-core, operator-observability, audit, reconciliation | `agent_type`, `cause` (`failure_class`, `sub_reason`, `source_run_id`, `source_bead_id`, `tripped_at`), `in_flight_count`, `paused_epoch` |
| 8.11.2 | `handler_resumed` | F | daemon-core (HandlerPauseController) | orchestrator-core, operator-observability, audit | `agent_type`, `by` (enum: `operator`), `prior_cause` (`failure_class`, `sub_reason`, `source_run_id`, `source_bead_id`, `tripped_at`), `paused_epoch` |
| 8.11.3 | `queue_item_held_for_handler_pause` | O | daemon-core (dispatcher) | operator-observability, audit, orchestrator-core | `bead_id`, `agent_type`, `paused_epoch` |

> **Dedup contract (§8.11.3).** `queue_item_held_for_handler_pause` MUST be emitted at-most-once per `(bead_id, paused_epoch)` pair. The dispatcher MUST NOT re-emit if the same bead is skipped again within the same pause epoch. The `paused_epoch` is the monotonic counter from `.harmonik/handler-state.json` incremented on every pause→resume cycle, which the dispatcher reads from `HandlerPauseController.CurrentEpoch(agent_type)` at each skip point.

> **Paired-phase note (§8.11.1–2).** `handler_paused` and `handler_resumed` are NOT a paired-phase lifecycle (§8.9(h)) because Pause and Resume are not sequential phases of the same entity — they are distinct terminal-distinct outcomes with independent payload shapes. The `status` merge rule of §8.9(h) does not apply. Each event is its own type.

> **Emission ordering (§8.11).** For a single pause epoch the emission order MUST be: (a) `handler_paused` (once, on pause-trip, MUST precede any `queue_item_held_for_handler_pause` for that epoch); (b) zero or more `queue_item_held_for_handler_pause` events (one per skipped item, dedup'd per `(bead_id, paused_epoch)`); (c) `handler_resumed` (once, on operator resume, terminates the epoch). `handler_paused` is fsync-backed before control returns from `HandlerPauseController.Pause()`; `handler_resumed` is fsync-backed before control returns from `HandlerPauseController.Resume()`.

> Section Axes (§8.11 Handler-pause lifecycle): All §8.11 event emissions are mechanism-tagged. §8.11.1 (`handler_paused`) and §8.11.2 (`handler_resumed`) are class F (fsync-backed, deterministic) because their loss would leave the pause-state landmark unrecoverable across restart — reconciliation depends on the JSONL to detect that a handler was paused when the daemon last exited. §8.11.3 (`queue_item_held_for_handler_pause`) is class O (ordinary, reconstructible from `handler-state.json` plus queue.json). Default per-entry Axes — class F: `llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent`. Class O: `llm-freedom=none; io-determinism=best-effort; replay-safety=safe; idempotency=non-idempotent`.

Axes: llm-freedom=none; io-determinism=best-effort; replay-safety=safe; idempotency=non-idempotent

### 8.12 Decision-required lifecycle

| # | Type | Dur | Emitter | Typical consumers | Payload fields |
|---|---|---|---|---|---|
| 8.12.1 | `decision_required` | F | daemon-core | operator-observability, cognition-loop, audit | `subject`{`kind`,`id`}, `reason`, `suggested_action`, `ack_required`, `ack_token`, `triggering_event_id` |
| 8.12.2 | `decision_acknowledged` | F | daemon-core | operator-observability, cognition-loop, audit | `ack_token`, `subject`{`kind`,`id`}, `ack_method`{`operator`\|`note`}, `acked_at` |

> **Emission conditions (§8.12.1).** Daemon MUST emit `decision_required` on: (a) bead fails twice in a daemon session without an intervening success (`run_failed`×2 on same `bead_id`); (b) `iteration_cap_hit` fires with `final_verdict ∈ {REQUEST_CHANGES, BLOCK}`; (c) `merge_conflict_escalation` is emitted (§8.5.6); (d) `queue_paused{reason: group_failure}` is emitted (§8.10.4). At-most-one per triggering event (idempotency-keyed on `triggering_event_id`). Re-processing the same trigger after restart MUST NOT re-emit for an already-emitted pending `ack_token`. Enforced via `.harmonik/decision_acks/<ack_token>` file presence + `status=pending` check.

> **Acknowledgement surface.** Either (a) `harmonik decision ack <ack_token>` CLI (operator), OR (b) `note(kind=warning|defer, refs=[subject.id], ...)` from the cognition loop (Tier-2 judgment implicitly ACKs). Daemon updates the `ack_token` record → `{status:acknowledged, acked_at, ack_method:operator|note}` and emits `decision_acknowledged` (§8.12.2).

> **TTL.** `ack_token` valid 24h (configurable, default 86400s). After TTL without ACK, daemon MUST re-emit `decision_required` with a fresh `ack_token` (same subject/reason; new token) AND `daemon_degraded{reason: decision_ack_timeout}` (requires EV-027 amendment for new `daemon_degraded` variant).

> **Dispatch-blocking rule (EV-043).** While any `decision_required` for `subject kind=bead,id=X` is unacknowledged, daemon MUST NOT dispatch a new run for bead X. While any `decision_required` for `subject kind=queue,id=Q` is unacknowledged, daemon MUST NOT advance queue Q past the paused group. Applies across daemon restarts (per §4.12 EV-043a).

> §8.12 is NOT a paired-phase per §8.9(h) — payloads differ in shape; `decision_required` carries the problem; `decision_acknowledged` carries the resolution reference. §8.9(h) status-merge does not apply. §8.9 compliance evidence: (a) cross-subsystem consumers: operator-observability, cognition-loop, audit; (b) lifecycle-boundary signal: dispatch-eligibility transition; (c) single summary event per condition (no per-heartbeat re-emit); (d) schema in §6.3; (e) class F (loss silently unblocks dispatch); (f) idempotent on `ack_token`; (g) cited by cognition-loop spec as Tier-2 wake.

> Section Axes (§8.12 Decision-required lifecycle): Both entries are mechanism-tagged, class F (fsync-backed): `decision_required` loss silently leaves a double-failed bead eligible for re-dispatch; `decision_acknowledged` loss leaves the ack-state file authoritative but JSONL observability broken. Axes: `llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent`.

Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

### 8.13 Epic-completion lifecycle

| # | Type | Dur | Emitter | Typical consumers | Payload fields |
|---|---|---|---|---|---|
| 8.13.1 | `epic_completed` | O | daemon-core | observability, audit, cognition-loop | `epic_id`, `last_child_bead_id`, `closed_at` |

> **Emission rule (§8.13.1).** Daemon MUST emit `epic_completed` at most once per parent epic per daemon session (at-most-once guard keyed on `epic_id`; in-process `emittedEpics` map). Emission is triggered after a successful `CloseBead` call: if the closed bead has a parent epic AND every sibling child shows `status=closed` in the Beads ledger, the guard is claimed (under `emittedEpicsMu`) and the event is emitted. Zero-emit conditions: (a) closed bead has no parent (AC-4); (b) at least one sibling child is not yet closed (AC-3); (c) `emittedEpics` already contains the parent ID. Cross-process sibling-race (AC-2) and boot-survival (AC-5) are out-of-scope for C1 and exercised by the T4 scenario bead.

> **EV-029 compatibility.** This row is additive per EV-029 (N-1 readable): existing consumers that do not recognise `epic_completed` MUST ignore it per §6.4 row 1.

> Section Axes (§8.13 Epic-completion lifecycle): mechanism-tagged, class O (ordinary — observational; loss does not affect bead routing or completion). Axes: `llm-freedom=none; io-determinism=best-effort; replay-safety=safe; idempotency=non-idempotent`.

Axes: llm-freedom=none; io-determinism=best-effort; replay-safety=safe; idempotency=non-idempotent

### 8.14 HITL-decisions lifecycle

Three new event types introduced by the `hitl-decisions` work (codename:hitl-decisions, hk-33p; normative spec: [specs/hitl-decisions.md](hitl-decisions.md)). They are the **agent→human dual** of agent-comms (§8.10's `agent_message` family): an agent that hits a decision point needing a human emits `decision_needed` and blocks cleanly on a subscribe stream; a human answers with `decision_resolved`, routed back to unblock the originating agent; an orphaned or self-obsoleted decision is closed with `decision_withdrawn`. All three ride the standard EV-001 envelope and are **Class F** (fsync-boundary, hitl-decisions SPEC §6 N1 / Risk R1, load-bearing): a lost `decision_resolved` would leave the blocked agent waiting forever. Go payload structs: `core.DecisionNeededPayload`, `core.DecisionResolvedPayload`, `core.DecisionWithdrawnPayload` (`internal/core/decisionpayloads_33p.go`); fsync-boundary registration in `internal/eventbus/busimpl.go`.

> **DISTINCT from §8.12.** This family is NOT the pre-existing §8.12 `decision_required` / `decision_acknowledged` daemon-escalation pair — different names, different emitter, different purpose. §8.12 is daemon-internal escalation (a double-failed bead, an iteration-cap hit; daemon-core emitted; ACK gates dispatch). §8.14 is an agent's request for a *human product/authorization decision* (agent-emitted via `harmonik decisions raise`; operator-answered via `harmonik decisions answer`). The two families do not share payload fields, `decision_id` keying, or emitter.

| # | Type | Dur | Emitter | Typical consumers | Payload fields |
|---|---|---|---|---|---|
| 8.14.1 | `decision_needed` | F | agent (via `harmonik decisions raise` → daemon socket) | operator-observability, cognition-loop, audit, keeper | `question`, `options []string` (≥1), `context_link?`, `blocked_agent?`, `value_requested?` (bool) |
| 8.14.2 | `decision_resolved` | F | operator (via `harmonik decisions answer` → daemon socket) | the blocked agent (via subscribe stream), operator-observability, audit | `decision_id`, `chosen_option`, `value?`, `resolver?` |
| 8.14.3 | `decision_withdrawn` | F | agent (`reason=self_obsoleted`) OR keeper (`reason=orphaned`, sole orphaned emitter) | the blocked agent, operator-observability, audit | `decision_id`, `reason` (`self_obsoleted` \| `orphaned`), `by?` |

> **EV-045 — `decision_id` keying & first-writer-wins (NORMATIVE).** The `decision_id` for a decision is the `decision_needed` event's own bus-minted `event_id` (UUIDv7); it is returned to the agent by `raise` and is DISTINCT from the two terminals' own `event_id`s. The open-decision projection (hitl-decisions SPEC §3) is a pure fold over `events.jsonl`: **add** on `decision_needed` keyed by `event_id`; **remove** on `decision_resolved` / `decision_withdrawn` keyed by `payload.decision_id`; **dedupe** on `event_id` (per EV-014b / N2). Open = `decision_needed − (decision_resolved ∪ decision_withdrawn)`. Resolution is **first-writer-wins** on `decision_id` (hitl-decisions SPEC §9 / N3): the first `decision_resolved` for a given `decision_id` is authoritative; any later `decision_resolved` (a second human, a replay) is a no-op — single apply, single wake. `answer`/`withdraw` on an unknown or already-terminal `decision_id` is likewise a no-op.

> **Required vs optional fields (§8.14).** `decision_needed`: `question` and `options` (≥1 element) are REQUIRED; `context_link`, `blocked_agent`, `value_requested` are OPTIONAL (omitted via `omitempty`). `decision_resolved`: `decision_id` and `chosen_option` are REQUIRED (the `Valid()` method rejects empty either); `value` (v1.1 hook, empty in v1) and `resolver` are OPTIONAL. `decision_withdrawn`: `decision_id` and `reason` are REQUIRED (`reason` MUST be `self_obsoleted` or `orphaned` — the only two `DecisionWithdrawnReason` constants); `by` is OPTIONAL (agent name for `self_obsoleted`, `"keeper"` for `orphaned`). In v1 a `decision_resolved.chosen_option` MUST be one of the originating `decision_needed.options` (hitl-decisions SPEC N7); that cross-event check is enforced at the answer site (component K4), not at the payload `Valid()` level.

> **Blocked-wait & orphan-reap (§8.14).** A blocked agent MUST wait by holding an open `harmonik subscribe --types decision_resolved,decision_withdrawn` stream — both to wake on the matching terminal AND to keep its keeper gauge fresh via the stream heartbeat (hitl-decisions SPEC §4 / N5). The **keeper tick is the SOLE emitter** of `decision_withdrawn(reason=orphaned, by="keeper")`, and only when the `blocked_agent` has explicitly `leave`d or is Offline (past the ~10-min presence cutoff, never merely Stale); `decisions list` only *flags* an Offline-blocked decision as orphaned-pending without a read-side emit (hitl-decisions SPEC §5 / N9). `decision_withdrawn(reason=self_obsoleted)` is agent-emitted.

> §8.14 is NOT a paired-phase per §8.9(h): the three types differ in shape and are distinct-terminal outcomes (a decision resolves OR is withdrawn, not a single status-carrying lifecycle). The §8.9(h) status-merge rule does not apply. §8.9 compliance evidence: (a) cross-subsystem consumers: operator-observability, cognition-loop, keeper, audit, plus the blocked agent as an out-of-process subscribe consumer; (b) lifecycle-boundary signal: the agent's blocked↔unblocked transition; (c) single summary event per lifecycle edge (no per-heartbeat re-emit); (d) schema in `internal/core/decisionpayloads_33p.go` with `Valid()` methods; (e) class F (loss leaves the blocked agent waiting forever — Risk R1); (f) idempotent on `decision_id` (first-writer-wins, EV-045); (g) cited by the hitl-decisions spec and the agent skill/handler-contract.

> Section Axes (§8.14 HITL-decisions lifecycle): all three entries are mechanism-tagged, class F (fsync-backed) because loss of any terminal would orphan the blocked agent's wake (SPEC §6 N1, load-bearing). Axes: `llm-freedom=none; io-determinism=best-effort; replay-safety=safe; idempotency=idempotent`.

Axes: llm-freedom=none; io-determinism=best-effort; replay-safety=safe; idempotency=idempotent

### 8.15 Bead-ledger merge lifecycle

Two event types emitted during the bead-ledger union-merge path (normative spec: [specs/beads-integration.md §4.8b BL-MRG](beads-integration.md)). `bead_sync_failed` is class F because its loss leaves the Cat-BL2 routing decision unrecorded — the daemon would continue silently past a failed SQLite refresh, violating BL-MRG-004's explicit route-to-Cat-BL2 obligation. `bead_ledger_conflict_audit` is class O: `.beads/merge-conflicts.log` is the authoritative source; the event is observational evidence surfaced by the reconciliation investigator at Cat-BL3 audit time.

| # | Type | Dur | Emitter | Typical consumers | Payload fields |
|---|---|---|---|---|---|
| 8.15.1 | `bead_sync_failed` | F | daemon-core (beads-adapter, post-merge) | reconciliation (Cat-BL2), audit, operator-observability | `run_id`, `error`, `timestamp` |
| 8.15.2 | `bead_ledger_conflict_audit` | O | reconciliation-investigator | reconciliation (Cat-BL3), audit, operator-observability | `run_id`, `bead_ids` (String[]), `conflicts` (Object[]), `timestamp` |

> **§8.15.1 emission rule (`bead_sync_failed`).** The daemon MUST emit this event if `br sync --import-only` fails following any rebase or merge that touches `.beads/issues.jsonl` per [beads-integration.md §4.8b BL-MRG-004]. The event MUST be emitted and fsynced before the daemon routes to Cat-BL2 per [reconciliation/spec.md §8.BL2]. `timestamp` is the wall-clock time at the failure site (distinct from the envelope `timestamp_wall` only when the daemon synthesizes the event from a deferred write; in the common path they are equal). `error` is a free-form error string from the `br sync` subprocess stderr or exit code.

> **§8.15.2 emission rule (`bead_ledger_conflict_audit`).** The reconciliation investigator MUST emit this event for each `.beads/merge-conflicts.log` batch read during a Cat-BL3 audit per [beads-integration.md §4.8b BL-MRG-003]. `bead_ids` lists the bead IDs involved in the logged semantic conflicts; `conflicts` is a structured representation of the logged conflict lines — one Object per line: `{bead_id, field, a_value, b_value, resolution}`. `timestamp` is the wall-clock time of the log read. The conflict log is the authoritative record; this event is observational. Class O because the log survives daemon restart and the investigator can re-emit on recovery.

> **NOT a paired-phase (§8.9(h)).** `bead_sync_failed` and `bead_ledger_conflict_audit` are not sequential phases of the same lifecycle: `bead_sync_failed` occurs on SQLite refresh failure (BL-MRG-004); `bead_ledger_conflict_audit` occurs during reconciliation-investigator Cat-BL3 audit (BL-MRG-003). The §8.9(h) status-merge rule does not apply.

> Section Axes (§8.15 Bead-ledger merge lifecycle): §8.15.1 (`bead_sync_failed`) is class F (fsync-backed, deterministic) — loss silences the Cat-BL2 routing obligation per BL-MRG-004. §8.15.2 (`bead_ledger_conflict_audit`) is class O (ordinary, best-effort) — the conflict log is the authoritative source. Default per-entry Axes — §8.15.1: `llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent`. §8.15.2: `llm-freedom=none; io-determinism=best-effort; replay-safety=safe; idempotency=non-idempotent`.

Axes: llm-freedom=none; io-determinism=best-effort; replay-safety=safe; idempotency=non-idempotent

### 8.16 Session-keeper watcher & cycle lifecycle

The session-keeper watcher/lifecycle cohort (codename:session-keeper, hk-ekap1). These 18 types are the coarse-grained watcher and cycle-boundary signals the keeper has shipped since v0.6.x; they are documented here to close the code↔spec §-numbering drift — the code doc-comments cited them as "§8.13", colliding with the spec's §8.13 Epic-completion family (§8.13/§8.14/§8.15 are unmoved; only the code-comment citations are corrected to §8.16 in the same change). This cohort is registered by `registerKeeperEvents()` and is the coarse counterpart to the fine-grained interior milestones in §8.20. Emitter is the standalone `internal/keeper` process (the `session_keeper_watcher_dead` liveness probe is daemon-emitted). All 18 are durability class O.

| # | Type | Dur | Emitter | Typical consumers | Payload fields |
|---|---|---|---|---|---|
| 8.16.1 | `session_keeper_warn` | O | internal/keeper | operator-observability, audit | `agent_name`, `pct`, `warn_pct`, `session_id?` |
| 8.16.2 | `session_keeper_no_gauge` | O | internal/keeper | operator-observability, audit | `agent_name`, `reason` |
| 8.16.3 | `session_keeper_handoff_started` | O | internal/keeper | operator-observability, audit | `agent_name`, `cycle_id`, `session_id?` |
| 8.16.4 | `session_keeper_cycle_complete` | O | internal/keeper | operator-observability, audit | `agent_name`, `cycle_id`, `prev_session_id?`, `new_session_id?` |
| 8.16.5 | `session_keeper_cycle_aborted` | O | internal/keeper | operator-observability, audit | `agent_name`, `cycle_id`, `session_id?`, `reason` |
| 8.16.6 | `session_keeper_clear_unconfirmed` | O | internal/keeper | operator-observability, audit | `agent_name`, `cycle_id`, `session_id?` |
| 8.16.7 | `session_keeper_cycle_recovered` | O | internal/keeper | operator-observability, audit | `agent_name`, `cycle_id`, `phase_at_crash` |
| 8.16.8 | `session_keeper_precompact_blocked` | O | internal/keeper | operator-observability, audit | `agent_name`, `session_id?`, `action` |
| 8.16.9 | `session_keeper_respawn_attempted` | O | internal/keeper | operator-observability, audit | `agent_name`, `outcome`, `error?` |
| 8.16.10 | `session_keeper_operator_attached` | O | internal/keeper | operator-observability, audit | `agent_name`, `session_id?`, `phase` |
| 8.16.11 | `session_keeper_restart_now_blocked` | O | internal/keeper (RunOnDemand) | operator-observability, audit | `agent_name`, `session_id?`, `reason` |
| 8.16.12 | `session_keeper_blind` | O | internal/keeper | operator-observability, audit | `agent_name`, `managed_sid?`, `live_sid?`, `blind_seconds` |
| 8.16.13 | `session_keeper_hard_ceiling` | O | internal/keeper | operator-observability, audit | `agent_name`, `tokens`, `hard_ceiling` |
| 8.16.14 | `session_keeper_idle_crew` | O | internal/keeper | captain, operator-observability, audit | `agent`, `tokens`, `reason` |
| 8.16.15 | `session_keeper_config_rejected` | O | internal/keeper | operator-observability, audit | `agent_name`, `field`, `reason` |
| 8.16.16 | `session_keeper_watcher_dead` | O | daemon-core | captain, operator-observability, audit | `agent_name`, `grace_period_seconds`, `reason` |
| 8.16.17 | `session_keeper_live_pane_recover` | O | internal/keeper | operator-observability, audit | `agent_name`, `session_id?`, `stale_seconds`, `outcome`, `error?` |
| 8.16.18 | `session_keeper_ack_timeout` | O | internal/keeper (await-ack) | operator-observability, audit | `agent_name`, `nonce`, `kind`, `timeout_seconds`, `tmux_target?`, `reason` |

> **Cohort-guard carve-out (§8.16).** Per EV-050, the `session_keeper_*` family is registered (each has a `mustRegister` constructor and a mandatory `pertypecompat` row) but is EXCLUDED from the cross-bus taxonomy cohort guard (`allEventTypeCohort`) and from the EV-027 amendment count guard. This documents the existing 18-type precedent (they were already absent from the cohort) and applies equally to the §8.20 interior cohort.

> Section Axes (§8.16 Session-keeper watcher & cycle lifecycle): all 18 entries are mechanism-tagged, class O (ordinary — observability; a lost signal is recoverable from the keeper journal / gauge / a fresh probe). Default per-entry Axes: `llm-freedom=none; io-determinism=best-effort; replay-safety=safe; idempotency=non-idempotent`.

Axes: llm-freedom=none; io-determinism=best-effort; replay-safety=safe; idempotency=non-idempotent

### 8.17 Alarm / self-check

The alarm / self-check cohort (hk-tnmjy). Documented here to close the code↔spec drift (the code doc-comment cited "§8.14", colliding with the spec's §8.14 HITL-decisions family). One type today.

| # | Type | Dur | Emitter | Typical consumers | Payload fields |
|---|---|---|---|---|---|
| 8.17.1 | `review_gate_anomaly` | O | daemon-core (ReviewGateAnomalyWatcher) | operator-observability, audit | `consecutive_count`, `threshold`, `bead_ids`, `detected_at` |

> Section Axes (§8.17 Alarm / self-check): mechanism-tagged, class O (ordinary — observability alarm; the causal `bead_closed` + `reviewer_verdict` sequence is reconstructible from the JSONL log). Axes: `llm-freedom=none; io-determinism=best-effort; replay-safety=safe; idempotency=non-idempotent`.

Axes: llm-freedom=none; io-determinism=best-effort; replay-safety=safe; idempotency=non-idempotent

### 8.18 Remote-substrate workers

The remote-substrate worker cohort (remote-substrate B6/B11, worker-report WR1/PB1). Documented here to close the code↔spec drift (the code doc-comments cited "§8.16", now corrected to §8.18). Emitter is the daemon-side `workers` package. All class O.

| # | Type | Dur | Emitter | Typical consumers | Payload fields |
|---|---|---|---|---|---|
| 8.18.1 | `worker_unhealthy` | O | daemon-core (workers) | operator-observability, audit | `worker_name`, `failing_probe`, `detail`, `detected_at` |
| 8.18.2 | `worker_offline` | O | daemon-core (workers) | operator-observability, audit | `worker_name`, `worker_host`, `phase`, `detail`, `detected_at` |
| 8.18.3 | `worker_tunnel_failed` | O | daemon-core (workers) | operator-observability, audit | `run_id`, `bead_id`, `worker_name`, `worker_host`, `socket_path`, `detail`, `detected_at` |
| 8.18.4 | `worker_report` | O | daemon-core (workers) | operator-observability, audit, autoscale | `worker_name`, `sampled_at`, `load1`, `load5`, `ncpu`, `mem_total_mb`, `mem_free_mb`, `swap_used_mb`, `disk_free_mb`, `claude_procs`, `problems` |
| 8.18.5 | `resource_breach` | O | daemon-core (workers) | operator-observability, audit, autoscale | `worker_name`, `kind`, `signal`, `value`, `threshold`, `breached_for_seconds`, `in_flight`, `started_at`, `fired_at` |

> Section Axes (§8.18 Remote-substrate workers): all five entries are mechanism-tagged, class O (ordinary — operator observability; breach/health state is reconstructible from the `worker_report` stream). Default per-entry Axes: `llm-freedom=none; io-determinism=best-effort; replay-safety=safe; idempotency=non-idempotent`.

Axes: llm-freedom=none; io-determinism=best-effort; replay-safety=safe; idempotency=non-idempotent

### 8.19 Flywheel governor / liveness-halt / stall-sentinel

The flywheel-governor and stall-sentinel cohort (flywheel FW2/FW3 hk-z1lr/hk-4toh; stall-sentinel Layer A hk-l087e). Documented here to close the code↔spec drift (the code doc-comments cited "§8.17/§8.18/§8.19"; the spec homes all three under §8.19). Mixed durability — the ACT-mode halt page is class F.

| # | Type | Dur | Emitter | Typical consumers | Payload fields |
|---|---|---|---|---|---|
| 8.19.1 | `governor_signal` | O | daemon-core (sentinel) | operator-observability, audit | JSON-serialised `sentinel.GovernorSignal` (observe-mode audit record for one `Evaluate` cycle) |
| 8.19.2 | `liveness_halt` | F | daemon-core | operator-observability, audit | `consecutive_zero_cycles`, `liveness_no_progress_n` |
| 8.19.3 | `stall_detected` | O | daemon-core (Layer A stall detector) | watch-tier, ops-monitor, operator-observability, audit | `run_id`, `bead_id`, `signature`, `elapsed_ms` |

> Section Axes (§8.19 Flywheel governor / liveness-halt / stall-sentinel): all mechanism-tagged. §8.19.1 (`governor_signal`) class O — reconstructible by re-running `Evaluate` over the events window. §8.19.2 (`liveness_halt`) class F — flight-critical: the daemon halts on emission and MUST NOT lose the page. §8.19.3 (`stall_detected`) class O — reconstructible from a fresh Snapshot. Default per-entry Axes: `llm-freedom=none; io-determinism=best-effort; replay-safety=safe; idempotency=non-idempotent`.

Axes: llm-freedom=none; io-determinism=best-effort; replay-safety=safe; idempotency=non-idempotent

### 8.20 Session-keeper interior cycle events

The four fine-grained interior milestones of the keeper restart state machine (codename:session-restart-substrate). Where §8.16 carries the coarse watcher/lifecycle signals, §8.20 carries the per-cycle transition points a machine-checkable replay-invariant harness needs. All four are durability class O (observational), emitter `internal/keeper` (the existing `Emitter`/`FileEmitter` path), consumed by the `internal/replay` invariant harness (EV-048). Names follow the `session_keeper_*` catalog convention shared with §8.16 — not the `keeper_*` prose shorthand. The payload structs are in §6.3; the emission timing and ordering are owned by [session-keeper.md §4.4] (SK-R4).

| # | Type | Dur | Emitter | Typical consumers | Payload fields |
|---|---|---|---|---|---|
| 8.20.1 | `session_keeper_handoff_written` | O | internal/keeper | replay-invariant-harness, audit, observability | `agent_name`, `cycle_id` (REQUIRED), `session_id?`, `nonce?`, `recovered?`, `handoff_mtime?` |
| 8.20.2 | `session_keeper_model_done` | O | internal/keeper | replay-invariant-harness, audit, observability | `agent_name`, `cycle_id` (REQUIRED), `session_id?`, `source` (REQUIRED: `idle_marker`\|`transcript_turn`\|`timeout`), `degraded?` |
| 8.20.3 | `session_keeper_clear_sent` | O | internal/keeper | replay-invariant-harness, audit, observability | `agent_name`, `cycle_id` (REQUIRED), `session_id?`, `attempt` (1-based) |
| 8.20.4 | `session_keeper_new_session_up` | O | internal/keeper | replay-invariant-harness, audit, observability | `agent_name`, `cycle_id` (REQUIRED), `prev_session_id` (REQUIRED), `new_session_id` (REQUIRED, ≠ `prev_session_id`) |

> **Emission ordering (§8.20).** Within one restart cycle (keyed by the composite `(agent_name, cycle_id)` — see EV-046) the emission order MUST be: `session_keeper_handoff_written` → `session_keeper_model_done` → `session_keeper_clear_sent` → `session_keeper_new_session_up`. `session_keeper_clear_sent` MUST NOT be emitted before both `session_keeper_handoff_written` (SR3) and `session_keeper_model_done` (SR4) for the same cycle. The full ordering/liveness semantics (SR3/SR4/SR6/SR7/SR9) are owned by [session-keeper.md §5]; this spec owns the type registration, payload shape, and durability class.

> **Durability (§8.20 — D9, O-class explicit not implicit).** These four are class O for this phase, NOT class F. F-class would require routing keeper emits through the daemon or adding `Sync()` to `FileEmitter` (which opens/appends/closes with no fsync) — larger blast radius, out of scope (no daemon in execution). F-class is deferred; revisit if a later phase routes keeper through the daemon. **Required hardening:** unlike the existing keeper `_ = emit(...)` sites, the four §8.20 emit helpers MUST NOT silently swallow a marshal or emit error — they MUST log on failure, because "a durable event that silently fails to write" is a spec lie. This hardening is scoped to the four new helpers only; the existing `_ =` keeper emits are out of scope.

> **§8.9(b) cycle-interior exception (§8.20, RECORDED compliance evidence).** §8.9 admits a candidate type iff all of (a)–(h). These four are cycle-interior, which reads against §8.9(b) ("a lifecycle-boundary signal rather than an intra-lifecycle detail"). The exception is argued and recorded inline (as §8.12/§8.14 do): **(a) SATISFIED** — a real cross-subsystem consumer exists, the `internal/replay` invariant harness (EV-048), and the events cross a genuine process boundary (keeper process → `events.jsonl` → the replay package), which is also why they are NOT EV-026-internal. **(c) SATISFIED (granularity)** — the harness requires per-boundary access: SR3/SR4/SR6 are orderings *between* these interior milestones; a single summary event cannot express "`clear_sent` after `model_done`." **(b) REFRAMED and satisfied** — these four ARE the lifecycle boundaries of the restart state machine's sub-lifecycle (handoff-write / model-done / clear / new-session are its transition points), not incidental chatter; unlike the deferred `tool_command_completed` candidate, the consumer is named and SR3/SR4/SR6/SR7/SR9 are load-bearing. **(d)–(f) SATISFIED** — typed Go payloads in §6.3; four-axis + mechanism tags and durability class O per the Section Axes note; replay side-effect classification `safe`. **(g) SATISFIED** — [session-keeper.md] SK-R4 cites all four by their registered names as its ordering-invariant subjects. **(h) DOES NOT force a merge** — the four are distinct single-emission milestones, not a paired pending/resolved lifecycle. **EV-026 is not the escape hatch:** EV-026-internal events "MUST NOT be passed to `RegisterEventType`," which would kill the EV-048 typed-decode adoption; because the four cross the process boundary they are cross-bus and MUST be registered per EV-027.

> **Cohort-guard carve-out (§8.20).** Per EV-050, these four are registered + compat-tabled but excluded from `allEventTypeCohort` and the EV-027 count guard, following the §8.16 keeper precedent.

> Section Axes (§8.20 Session-keeper interior cycle events): all four entries are mechanism-tagged, class O (observational; loss does not gate the restart cycle, whose crash-recovery need is met by the retained keeper journal `RecoverFromCrash`). Default per-entry Axes: `llm-freedom=none; io-determinism=best-effort; replay-safety=safe; idempotency=non-idempotent`.

Axes: llm-freedom=none; io-determinism=best-effort; replay-safety=safe; idempotency=non-idempotent

### 8.21 Agent-input acceptance events

The two cross-bus signals of the M2 input driver's submission sub-lifecycle (codename:agent-input-substrate). When an input submission's acceptance is positively observed, the driver emits `agent_input_acked` at the input-acceptance boundary — the event's existence IS the positive ack (there is no acceptance "class"); if the InputAckTimeout window elapses with no positive acceptance, the driver emits `agent_input_stale` instead. This is uniform across the tmux/Claude path (where the positive signal is the Claude-hook-bridge event — `outcome_emitted` on `Stop`, `agent_ready` on `SessionStart` start/resume per [claude-hook-bridge.md §4.5 CHB-013] — not a `capture-pane` scrape) and the structured Codex app-server driver (the wire input-ack) — peers, not tiers. Both are durability class O (observational), emitter `daemon-core` (the structured input driver in the daemon input path; the emitter label follows the §8.3 agent-lifecycle convention where the daemon owns the emission point), consumed by the M3 runexec **run-reactor**, the `internal/replay` invariant harness (EV-048), audit, and observability. The payload structs are in §6.3; the emission timing and ordering are owned by [agent-input.md §4 AIS-004], and the driver→run seam is owned by [handler-contract.md §4 HC-070].

| # | Type | Dur | Emitter | Typical consumers | Payload fields |
|---|---|---|---|---|---|
| 8.21.1 | `agent_input_acked` | O | daemon-core (input driver) | run-reactor, replay-invariant-harness, audit, observability | `run_id` (REQUIRED), `input_seq` (REQUIRED, monotonic), `acceptance_token?`, `session_id?`, `acked_at` |
| 8.21.2 | `agent_input_stale` | O | daemon-core (input driver) | run-reactor, replay-invariant-harness, audit, observability | `run_id` (REQUIRED), `input_seq` (REQUIRED), `session_id?`, `timed_out_at` (REQUIRED), `window` |

> **§8.9(b) input-acceptance-boundary exception (§8.21, RECORDED compliance evidence).** §8.9 admits a candidate type iff all of (a)–(h). These two register the acceptance boundary of a submission's sub-lifecycle, which reads against §8.9(b) ("a lifecycle-boundary signal rather than an intra-lifecycle detail"). The exception is argued and recorded inline (as §8.12/§8.14/§8.20 do): **(a) SATISFIED** — real cross-subsystem consumers exist, the M3 runexec run-reactor and the `internal/replay` invariant harness (EV-048), and the events cross a genuine process boundary (the input driver → `events.jsonl` → the reactor/replay consumer), which is also why they are NOT EV-026-internal. **(c) SATISFIED (granularity)** — the reactor and harness require per-boundary access: an acceptance verdict per submission (keyed by `input_seq`) cannot be recovered from a single summary event; `agent_input_stale` is the distinct timeout boundary the reactor reacts to. **(b) REFRAMED and satisfied** — the ack IS the input-acceptance boundary of the submission sub-lifecycle (its existence is the positive ack — there is no acceptance class; `agent_input_stale` is its timeout terminal), not incidental chatter; the consumer is named (the M3 reactor + replay harness) and AIS-004's ordering is load-bearing. **(d)–(f) SATISFIED** — typed Go payloads in §6.3; four-axis + mechanism tags and durability class O per the Section Axes note; replay side-effect classification `safe`. **(g) SATISFIED** — [agent-input.md] AIS-004 cites both by their registered names as its emission-timing subjects, and [handler-contract.md] HC-070 names the seam. **(h) DOES NOT force a merge** — `agent_input_acked` and `agent_input_stale` are not a paired pending/resolved lifecycle: they are mutually exclusive terminal verdicts of the same acceptance attempt (accepted-verdict vs. timed-out), analogous to the split gate verdicts, not two sequential phases of one lifecycle. **EV-026 is not the escape hatch:** EV-026-internal events "MUST NOT be passed to `RegisterEventType`," which would kill the EV-048 typed-decode adoption; because the two cross the process boundary they are cross-bus and MUST be registered per EV-027.

> **Cohort-guard carve-out (§8.21).** Per EV-050, these two are registered (each has a `mustRegister` constructor and a mandatory `pertypecompat` / `PayloadCompatEntry` row) + compat-tabled, but excluded from `allEventTypeCohort` and the EV-027 count guard, following the §8.16/§8.20 keeper precedent (the async-observer cohort is documented, not counted).

> Section Axes (§8.21 Agent-input acceptance events): both entries are mechanism-tagged, class O (observational; a lost ack does not gate the run, whose acceptance state is re-derivable by the driver on the next submission / from the retained input journal). Default per-entry Axes: `llm-freedom=none; io-determinism=best-effort; replay-safety=safe; idempotency` — `agent_input_acked` is idempotent on `input_seq` (a re-emitted ack for the same `(run_id, input_seq)` is a no-op for the reactor); `agent_input_stale` is non-idempotent.

Axes: llm-freedom=none; io-determinism=best-effort; replay-safety=safe; idempotency=non-idempotent

## 4. Normative requirements

### 4.1 Envelope

#### EV-001 — Every event MUST carry the common envelope fields

Every event emitted to the bus or appended to JSONL MUST carry these envelope fields: `event_id` (UUIDv7), `schema_version` (integer), `type` (one of the types in §8), `timestamp_wall` (RFC 3339 wall-clock time at the emitter), `timestamp_mono_nsec` (optional monotonic nanoseconds from the emitter's process), `source_subsystem` (an opaque Go-package-identifier string per [architecture.md §4.5]), `trace_context` (for cross-subsystem correlation), `run_id` (when scoped to a run), `state_id` (when scoped to a state), and `payload` (type-specific body, see §6.3). The emitter MUST perform exactly one wall-clock read per emission and reuse that reading for both `timestamp_wall` and UUIDv7 generation so the envelope is self-consistent.

Tags: mechanism

#### EV-002 — `event_id` MUST be a UUIDv7

Every `event_id` MUST be a UUIDv7 (time-ordered UUID). UUIDv7 carries a 48-bit Unix-millisecond timestamp in its high bits, which supplies best-effort total ordering across processes without coordinated clocks. UUIDv4 and UUIDv1 MUST NOT be used for `event_id`.

Tags: mechanism

#### EV-002a — UUIDv7 generation MUST be monotonic within a process

The daemon's UUIDv7 generator MUST produce strictly monotonic values within a single process (RFC 9562 §6.2 method 1 or 3: monotonic sub-millisecond counter or monotonic random). Generation occurs in-process within the daemon; all cross-subsystem event emissions flow through the daemon's single emitter, so cross-process monotonicity within a project is a consequence of the daemon-as-sole-emitter property. On same-millisecond emissions from distinct emitter goroutines, the monotonic counter MUST break ties, strengthening the partial-order contract of EV-008.

Tags: mechanism

#### EV-002b — All `event_id` generation MUST route through the daemon's monotonic generator

All `event_id` generation for events emitted from handler subprocesses MUST route through the daemon's monotonic generator. Handler subprocesses MUST NOT generate `event_id` values independently. The emitter-column phrasing "handler (via daemon watcher)" in §8.3 is normatively enforced by this requirement: the handler writes an event envelope with no `event_id` (or a placeholder the daemon discards); the daemon watcher stamps the `event_id`, envelope timestamps, and `source_subsystem` at the moment it enqueues the event for emission. This preserves EV-002a's intra-daemon-process monotonicity as the sole monotonicity contract across all cross-bus events.

Tags: mechanism

#### EV-002c — UUIDv7 monotonicity across daemon restart via high-water-mark file

The daemon MUST persist a UUIDv7 high-water-mark (HWM) to `.harmonik/event_id_hwm`. The HWM MUST be updated on every `fsync-boundary` write (piggyback on the JSONL fsync domain so no additional fsync cost is incurred; the HWM update MUST be ordered before or within the same fsync as the boundary event). On daemon startup the generator MUST read the HWM and ensure every new `event_id` is strictly greater than it, even if the wall clock has regressed since the last run. If the wall clock is behind the HWM by more than 1 second the daemon MUST emit `daemon_degraded{reason=clock_regression}` and synthesize UUIDv7 timestamp components ahead of the wall clock until the clock catches up. If the HWM file is missing or unreadable at startup (first-run case, or `.harmonik/` wiped), the daemon MUST log a structured warning and seed from the wall clock; cross-restart ordering is NOT guaranteed in that case and consumers MUST NOT assume global total ordering finer than per-process contiguous runs.

Tags: mechanism

#### EV-003 — `timestamp_mono_nsec` is process-scoped and NOT cross-process-comparable

When present, `timestamp_mono_nsec` MUST be a monotonic nanosecond reading from the emitter's process. It MUST NOT be compared across daemon restarts or across processes; it is meaningful ONLY for intra-process ordering within the emitter's lifetime.

Tags: mechanism

#### EV-004 — `source_subsystem` is layout-open

The `source_subsystem` field MUST be a Go-package-identifier string. The envelope schema MUST NOT enumerate a fixed set, keeping the layout open for post-MVH reorganization. Subsystem identifiers are declared in each subsystem's envelope per [architecture.md §4.4].

Tags: mechanism

#### EV-005 — Events are lifecycle-boundary signals, not agent internals

Events emitted to the bus and JSONL MUST be lifecycle-boundary signals. Agent-internal detail — tool calls, thinking traces, per-token output — MUST NOT be emitted as events; it lives in the agent's session log per [workspace-model.md §4.7]. The per-chunk types `agent_output_chunk` and `budget_accrual` are lifecycle-boundary signals routed to the bus per §8.9; they are NOT the mechanism by which the orchestrator reconstructs agent-internal state.

Tags: mechanism

### 4.2 Clock and ordering

#### EV-006 — Wall clock is advisory for cross-process ordering

`timestamp_wall` MUST NOT be used for ordering decisions across processes; NTP skew, clock adjustments, and container-host time sync make it unreliable. Consumers that need cross-process ordering MUST use `event_id` (UUIDv7) per EV-002. `timestamp_wall` is for audit, human-readable display, and external correlation.

Tags: mechanism

#### EV-007 — Monotonic time orders intra-process events

Within a single emitter process, `timestamp_mono_nsec` (when present) MUST be non-decreasing across emissions in emission order.

Tags: mechanism

#### EV-008 — Partial-order contract

The event stream MUST satisfy a partial-ordering contract: UUIDv7 supplies ms-resolution total ordering across processes; EV-002a extends that to strict intra-process monotonicity at sub-millisecond resolution; `timestamp_mono_nsec` refines within a process; there is no total-ordering guarantee across *distinct* processes finer than UUIDv7's millisecond precision. Tooling that requires stricter cross-process ordering MUST insert explicit causal references via `trace_context.parent_event_id` (§6.1) or a payload-specific field such as `triggering_event_id` on `hook_fired`.

Tags: mechanism

### 4.3 Bus and consumer taxonomy

#### EV-009 — Subscription is declared at registration, not inferred

A consumer MUST register its subscription with the bus at daemon startup, declaring (a) the event types it consumes, (b) its class, (c) its consumer identifier. Dynamic mid-run subscription is forbidden; the bus is sealed after `daemon.Start()` returns, and post-seal `Subscribe` calls MUST return a typed sealed-bus error.

Tags: mechanism

#### EV-010 — Synchronous consumer class

A `synchronous` consumer runs on the producer's critical path. A synchronous-consumer failure MUST halt the producer's progress on the specific run: the producer receives a typed error, does NOT retry synchronously, emits a `consumer_failed` event (§8.8.2), and the run enters a quarantine state that requires operator escalation. At most ONE synchronous consumer per event type is permitted; enforced at subscription-registration time. A synchronous consumer MUST NOT emit events that would re-dispatch to itself (directly or transitively); the registration path MUST verify acyclicity across declared emission surfaces at startup and fail-closed on cycles.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent

#### EV-011 — Asynchronous consumer class

An `asynchronous` consumer runs off the critical path. Consumer failure MUST NOT block the producer. Failed deliveries are retried per a bounded policy (default: 3 attempts with exponential backoff starting at 1 second); exhausted retries enqueue the event to the dead-letter queue (§6.2) and emit `dead_letter_enqueued` (§8.8.3). Per-consumer dispatch queue depth MUST be bounded (default 1024; operator-configurable per [operator-nfr.md]). See EV-011a for overflow handling. Asynchronous consumers MAY be added at subscription time up to a per-type default cap of 8.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=best-effort; replay-safety=safe; idempotency=idempotent

#### EV-011a — Non-blocking producer back-pressure

The bus MUST NOT block the producer on async-consumer delivery. When a per-consumer queue is full, the bus MUST shed the event for that consumer according to the event's durability class: `lossy-tail-ok` sheds first, then `ordinary`; `fsync-boundary` events MUST NOT be shed from any queue and MUST spill over into a secondary spill file at `.harmonik/events/spill-<consumer>.jsonl` for out-of-band replay. Every shed event MUST cause emission of a `bus_overflow` event (§8.8.4). The bus MUST reserve a single dedicated slot (capacity-1 reservation) in the observer queue that consumes `bus_overflow`; the reservation guarantees the bus can enqueue at least one overflow signal per actual shed without a recursive fill check. If the `bus_overflow` reservation is exhausted (queue full AND reservation consumed), the bus MUST fall back to direct JSONL append with `fsync-boundary` semantics for that single `bus_overflow` write (promoted from `O` to `F` at write-time; the promotion MUST be recorded in the structured-log channel). The direct-append fallback blocks the producer for one write+fsync; this is accepted as the floor-price of telling the operator the bus ran out of queue space. Per-consumer spill files MUST be pre-created at subscription-registration time (EV-009) with `O_CREAT|O_APPEND|O_DSYNC` semantics; failure to create the spill file MUST fail daemon startup with a typed error.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=best-effort; replay-safety=safe; idempotency=idempotent

#### EV-012 — Observer consumer class

An `observer` is a passive consumer whose failures MUST NOT produce bus events or side effects beyond its own local logging. Observers MUST NOT mutate persistent state. Enforcement is by coding discipline plus a `depguard` rule: observer packages MUST NOT import state-mutating packages (git, Beads, workspace-manager). If an observer needs to mutate state, it MUST re-register as asynchronous.

Tags: mechanism

#### EV-013 — Consumer class is opt-in at subscription

An in-process subscriber's default class MUST be `observer`. `synchronous` and `asynchronous` classes are opt-in.

Tags: mechanism

#### EV-014 — Subscription-time class conflict is a startup error

If two `synchronous` consumers for the same event type register, the daemon MUST fail startup with a typed configuration error.

Tags: mechanism

#### EV-014a — Dispatch semantics

`Emit` returns after (a) redaction (EV-035), (b) JSONL append and any mandated fsync per EV-016, (c) synchronous-consumer dispatch (blocking until the at-most-one synchronous consumer returns or errors). Asynchronous and observer dispatches occur off the critical path via the bus worker pool (default 4 workers, operator-configurable) and MUST NOT extend `Emit` latency.

Tags: mechanism

#### EV-014b — Consumers MUST be idempotent on recovery and dead-letter replay

Every consumer MUST be coded against a tail-truncated event stream on recovery AND against repeated delivery via `DeadLetterReplay` and `ReplayFrom`. This obligation pairs with the producer idempotency of EV-018 to make lossy-tail-plus-replay safe end-to-end.

Tags: mechanism

#### EV-014c — Observer dispatch uses per-observer goroutine plus bounded per-observer queue

`FANOUT_OBSERVERS` dispatches each event to each registered observer via a per-observer dedicated goroutine draining a per-observer bounded queue (default depth 1024, operator-configurable; same default as async per EV-011). Observer queues are class `lossy-tail-ok` for shed semantics per EV-011a: when a per-observer queue is full, the event is dropped and a `bus_overflow{shed_policy=lossy-dropped}` signal is emitted. A slow observer MUST NOT back-pressure the producer OR starve peer observers; each observer has its own queue and its own dispatcher goroutine. `fsync-boundary`-class events that cannot queue to an observer follow the EV-011a spill-file path identically to async consumers.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=best-effort; replay-safety=safe; idempotency=idempotent

#### EV-014d — Consumer-recovery replay contract (closes EV-INV-002 consumer side)

On daemon startup, for every subscription whose `since` field is non-nil OR whose `offset_checkpoint_event_id` is non-nil, the bus MUST perform a JSONL-tail replay to the consumer before live-stream delivery resumes. Replay scans `events.jsonl` for lines whose `event_id` is strictly greater than the consumer's effective checkpoint and dispatches them to the consumer's handler in `event_id` order. Dead-letter log and spill files are NOT automatically replayed; those require `DeadLetterReplay` per EV-011. Replay and live-stream are serialized per consumer: live events are buffered into the consumer's queue during replay and delivered only after replay reaches the current JSONL tail; `on_tail_truncation` fires (if registered) after replay completes when the tail lost data. Consumers with `since: None` and no `offset_checkpoint_event_id` start from the live stream (accept observability gap per EV-INV-002; the two-sided covenant is satisfied producer-side by fsync on F-class and consumer-side by tolerate-gap). Synchronous consumers do NOT participate in replay — a synchronous consumer's critical-path contract ended when the producer returned from `Emit`; re-invoking a synchronous handler on restart would risk double side effects.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

### 4.4 Durability classes and fsync semantics

#### EV-015 — JSONL is the durable on-disk form

The bus MUST persist every emitted event (before any shed) to a JSONL file on the local filesystem at `.harmonik/events/events.jsonl`. Line format per §6.2.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent

#### EV-016 — Every §8 row MUST declare a durability class; fsync derives from class

Every row in §8 MUST carry a `durability_class` attribute in `{fsync-boundary, ordinary, lossy-tail-ok}`. The JSONL writer MUST call `fsync(2)` on the event-log file after appending any event whose class is `fsync-boundary`. `Append` MUST return to the caller only after the fsync completes for boundary-class events; an `ordinary`-class or `lossy-tail-ok`-class event returns without fsync. An operator-configurable timer (default 1 second) MAY additionally flush on `ordinary`-class events; `lossy-tail-ok` MAY be flushed only opportunistically. Classes in §8 are assigned per the rule: events whose loss forces reconciliation into work greater than the cost of a disk sync are `fsync-boundary`; high-cardinality granular events (chunk, accrual, metric) are `lossy-tail-ok`; everything else is `ordinary`.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent

#### EV-016a — Per-event fsync; no multi-event atomicity guarantee

`Emit` performs per-event fsync for `fsync-boundary`-class events (EV-016). The bus does NOT guarantee atomicity across multiple boundary events: a producer that emits two boundary events sequentially MAY observe a post-crash state in which the first event is durable but the second is not. Producers requiring two events to be durably persisted together MUST emit a single event carrying both payloads OR MUST resolve the pair via the authoritative stores (git plus Beads) rather than via the event stream. Similarly, `event_id` generation is not atomic with `write(2) + fsync(2)`: a generated-but-not-persisted `event_id` has no durable trace and is acceptable (consumers MUST tolerate sparse `event_id` sequences). Batching multiple boundary events into a single fsync is best-effort only and reserved for post-MVH amendment; MVH accepts the O(N-fsync) cost in exchange for a trivially-reasoned per-event durability contract.

Tags: mechanism

#### EV-017 — Event loss between fsyncs is acceptable because git is authoritative

Producers MUST assume that `ordinary` and `lossy-tail-ok` events emitted between two fsync boundaries MAY be lost on a hard crash. This is acceptable because git plus Beads is authoritative for state per [execution-model.md §4.7] and producers emit events idempotently per EV-018. Consumers MUST be coded against tail truncation (EV-014b). (Rationale in §A.3.)

Tags: mechanism

#### EV-018 — Producers MUST emit events idempotently

Every producer MUST emit each event in an idempotent form: re-emitting the same event (same `event_id`, same payload) during recovery MUST be safe for downstream observational consumers to process. Producers MUST NOT encode one-shot side-effect semantics into event payloads; side effects belong in the run's checkpoint trail (git) or in Beads.

Tags: mechanism

#### EV-019 — Panic handler MUST flush structured logs

On a Go `panic`, the daemon's top-level recovery handler MUST flush the structured-log channel before exit. OS-level crashes (SIGKILL, kernel panic) bypass this handler; diagnostics rely on the aggressive-fsync log channel plus post-restart reconciliation.

Tags: mechanism

#### EV-019a — Panic handler SHOULD best-effort flush the bus

On a Go `panic`, the daemon's top-level recovery handler SHOULD additionally make a best-effort flush of the event bus after log flush completes. This is a best-effort obligation; completeness is not guaranteed.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=best-effort; replay-safety=safe; idempotency=idempotent

#### EV-020 — JSONL writes MUST be append-only

The JSONL writer MUST NOT rewrite, truncate, or reorder existing lines. Corruption (partial-line write on crash) is detected by readers per §6.2.

Tags: mechanism

### 4.5 Replay semantics

#### EV-021 — Observational replay MUST NOT reconstruct state

Any tool that walks JSONL for debugging, pattern analysis, or dashboard purposes is performing **observational replay**. Output is advisory only. Authoritative state-reconstruction source is git plus Beads per [execution-model.md §4.7].

Tags: mechanism

#### EV-047 — `ScanAfter` is the declared normative offline-replay read surface

`eventbus.ScanAfter(path string, sinceID core.EventID) iter.Seq[core.Event]` is the **declared read surface** for offline observational replay over `events.jsonl`. It is promoted from its incidental EV-038 mention to a first-class primitive: it is exported, already has production callers, and yields every envelope whose `event_id` is strictly greater than `sinceID` in **file order**. An offline replay / invariant-checking consumer (EV-048) MUST read the log through `ScanAfter` — passing `core.EventID{}` (zero) to scan from the beginning, or a persisted watermark `EventID` for incremental re-runs — and MUST NOT reconstruct authoritative state from the result (subject to EV-021). Because the keeper `FileEmitter`s and the daemon `JSONLWriter` append to the **same** `events.jsonl` with **per-process** `EventIDGenerator`s, `ScanAfter`'s file order is only an approximation of global `EventID` order across writers; a consumer whose invariants depend on inter-event ordering MUST re-sort the collected events by `EventID` after the scan drains before evaluating ordering invariants. `eventbus.Filter(path, runID)` selects by envelope `run_id` and remains undeclared/unused (its deadness corroborates EV-046: cycle events carry no envelope `run_id`).

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

#### EV-048 — The typed-decode registry is the sanctioned decode/assert layer for an observational replay-invariant consumer

The typed-decode registry path — `Event.DecodePayload`, `core.ValidateEnvelopeSchemaVersion`, the `DispatchObservational` / `DispatchSynchronous` dispatchers, and the `pertypecompat` N-1 compat table (`LookupPayloadCompatEntry`) — is declared ADOPTED (not dead, not deleted) as the decode-and-assert layer of a sanctioned **observational replay-invariant-checking consumer** (the `internal/replay` harness). This consumer is the EV-033 "observational consumer" the API was specced for and becomes the registry's first production reader. It MUST decode each scanned envelope through the registry, and MUST treat a schema-version mismatch as a recorded **finding** (writer/reader drift), consulting `LookupPayloadCompatEntry(ev.Type).CompatWindowHolds` to decide whether N-1 replay is declared safe — never as a fatal error. In its default (tolerant) mode it MUST apply the EV-033 unknown-type policy: an unknown `type` is **counted and skipped**, never fatal. A type registered but never observed in the corpus MUST be reported informationally, never as a violation. This consumer is observational per EV-021: its output is advisory and MUST NOT reconstruct authoritative state.

Tags: mechanism

#### EV-022 — State reconstruction MUST NOT walk JSONL

The daemon's startup state-reconstruction path MUST walk git plus query Beads; it MUST NOT read JSONL to reconstruct state.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

#### EV-023 — Divergence-evidence read with post-crash-window guardrail

Reconciliation detectors and investigator agents MAY read the JSONL tail for the express purpose of detecting **inconsistency** between the three stores. A divergence-evidence read MUST produce a typed `store_divergence_detected` event (§8.6.8) as its output. To prevent false positives from lossy-tail event loss, the detector MUST:

- Determine whether the read covers the **post-crash window** (any JSONL position after the last durable fsync boundary preceding the most recent daemon startup). This determination uses the most recent `daemon_started` event's `event_id` as a landmark.
- Set `post_crash_window: true` on any divergence event whose evidence falls inside that window.
- Corroborate divergence against git and Beads before flagging: an event that references a `commit_hash` MUST be tested against git; a bead transition MUST be tested against Beads. Only if the on-disk authoritative stores also disagree is the divergence real.

Cross-reference: [reconciliation/spec.md §4.3].

Tags: mechanism

#### EV-023a — Evidence-inconclusive classification for non-corroborable events

A divergence detector MUST classify the evidence supporting every candidate divergence event into one of: `git-corroborated`, `beads-corroborated`, or `inconclusive`. An event is `git-corroborated` when its payload carries a `commit_hash` (or other git reference) that the detector can test against the git DAG; it is `beads-corroborated` when its payload carries a `bead_id` the detector can query against Beads; otherwise it is `inconclusive`. The detector MUST emit `store_divergence_detected` ONLY for corroborated evidence. For `inconclusive` evidence the detector MUST emit `divergence_inconclusive` (§8.6.10) carrying the same `evidence_ref` plus the inconclusive classification; this event signals that reconciliation cannot decide whether divergence occurred from observational data alone. Boundary events with no authority-reference field (e.g., `daemon_started`, `daemon_ready`, a pending `workspace_merge_status`) always fall to `inconclusive` when inside the post-crash window; they MAY contribute to a peer corroborated divergence event but MUST NOT be flagged individually.

Tags: mechanism

#### EV-024 — Replay cannot re-establish agent state or re-invoke LLMs

Neither observational replay nor state reconstruction MAY re-establish live agent-process state or re-invoke LLMs. Tools that appear to do so are debugging aids; their output is non-authoritative.

Tags: mechanism

### 4.6 Producer / consumer contract

#### EV-025 — Each event type has exactly one owning spec for payload shape

Each event type in §8 MUST have its payload schema declared in §6.3 of this spec. The subsystem spec that emits the event is normative for the **WHEN** (timing and preconditions); this spec is normative for the **SHAPE**. Co-ownership is declared in §6.5 of each emitting spec.

Tags: mechanism

#### EV-046 — Cycle-scoped keeper events MUST join on a required payload `cycle_id`, never a zero envelope `run_id`

The four §8.20 session-keeper interior event types MUST carry their cycle correlation identity in a **REQUIRED payload field `cycle_id`** (JSON `cycle_id`, no `omitempty`), NOT in the envelope `run_id`. A §8.20 event MUST NOT be emitted with an empty `cycle_id`; the payload `Valid()` method MUST reject an empty `cycle_id`. The envelope `run_id` for these events MUST be absent (`core.RunID{}` / `None`); a zero-valued (Nil-UUID) envelope `run_id` MUST NOT be written for a cycle-scoped keeper event — this reuses the §6.1 "`run_id` present when scoped to a run" rule (these events are cycle-scoped, not workflow-run-scoped) and keeps them out of the EM-013 workflow-run keyspace that live daemon folds (`eventbus.Filter`) walk. Because `cycle_id` alone is not globally unique (`newCycleIDGen` resets its sequence per process), the normative join key is the **composite `(agent_name, cycle_id)`**; every §8.20 payload MUST carry both `agent_name` and `cycle_id` so the composite is always available. Precedent: the §8.6 `reconciliation_run_id` payload-level run identity (`ReconciliationStartedPayload`, `Valid()`-checked). Consumers MUST group cycle-scoped keeper events by the composite, and MUST NOT attempt to correlate them via envelope `run_id`.

Tags: mechanism

> **Optional additive backfill (non-blocking).** `SessionKeeperAckTimeoutPayload` MAY additively gain `CycleID string` (json `cycle_id,omitempty` — a backfill onto an existing type; old records legitimately lack it). No schema-version bump (additive; covered by its `AdditiveOnly:true` compat row). Deferrable as a follow-up.

#### EV-026 — Subsystems MAY emit internal events not on this list

Internal events (within a subsystem's own Go package, never dispatched to the bus) MUST NOT cross the bus and do not require §8 registration.

Tags: mechanism

#### EV-027 — Adding or removing a cross-bus event type requires a foundation amendment

A subsystem that wants to emit a new cross-bus event type MUST add the type to §8 via the foundation amendment protocol ([architecture.md §4.6]). The addition amendment MUST provide: type name, emitter, typical consumers, payload fields, four-axis tags, durability class, and evidence satisfying every criterion in §8.9. The addition amendment MUST include (a) the §8 row, (b) the emitter-spec edit adding the emission requirement, (c) at least one consumer cited in another spec.

Symmetrically, a sibling spec that removes an emission requirement MUST also amend §8 via the same protocol — either by removing the event type (deletion amendment) OR by documenting the orphan status with evidence that the event remains load-bearing via a different emitter. A deletion amendment MUST provide: the retiring event-type name, the emitter-spec edit removing the emission, migration guidance for current consumers, and a statement that the retired `EventType` enum value is retired and MUST NOT be reused for any future event. Event-type identifiers, once retired, are burned; consumers pinned to N-1 schema versions must continue to accept the retired type as a known value that will never be emitted again.

Tags: mechanism

#### EV-050 — The `session_keeper_*` cohorts are carved out of the cross-bus cohort/count guards

The `session_keeper_*` event families (the §8.16 watcher/lifecycle cohort AND the §8.20 interior cohort) are **registered and compat-tabled** (each has a `mustRegister` constructor and a `PayloadCompatEntry` row) but are **excluded from the cross-bus taxonomy cohort guard** (`allEventTypeCohort` in `eventtype_coverage_gjyks_test.go`) and from the EV-027 amendment count guard (`wantCount`). This carve-out follows the existing 18-type keeper precedent (today those 18 are already absent from the cohort, silently) and is now stated normatively so future keeper additions know the rule: a new `session_keeper_*` type MUST get a `mustRegister` constructor and a mandatory `pertypecompat` row, but MUST NOT be added to `allEventTypeCohort` or to the EV-027 `wantCount`. The `eventtype_coverage_gjyks_test.go` doc-comment — which currently states "every `EventType` constant MUST also be appended to `allEventTypeCohort`", a contract 18 keeper types already violate — MUST be amended to name this carve-out explicitly (a doc-comment edit, not a test-logic change), so the stated contract stops being false.

Tags: mechanism

### 4.7 Schema versioning

#### EV-028 — Each event type and the envelope carry `schema_version`

`schema_version` is an integer on the envelope and MUST match the schema version of the payload for that `type`. The envelope-level `schema_version` field is normative for the envelope shape; per-type schema versions MAY increment independently as declared in the amendment protocol.

Tags: mechanism

#### EV-049 — Additive `DecodePayloadStrict` variant surfaces additive writer drift

The registry MUST provide an additive `DecodePayloadStrict` variant that decodes a payload exactly as `DecodePayload` does but with `DisallowUnknownFields` set, so that an **additive field a newer writer introduced** surfaces as a decode error instead of being silently ignored (`DecodePayload` uses `json.Unmarshal` with no `DisallowUnknownFields` and therefore cannot see additive drift). The replay-invariant consumer's **strict mode** MUST route through `DecodePayloadStrict` and treat an unknown payload field, or an unknown event `type`, as a hard finding — strict mode is for replaying the harness's OWN freshly-recorded corpus, where such a surprise means a `mustRegister` was forgotten or a writer drifted. The addition is purely additive: it introduces no new obligation on existing writers and does not change `DecodePayload`'s tolerant semantics, which remain the default for historical replay.

Tags: mechanism

#### EV-029 — N-1 readable compatibility window (per-type AND envelope)

Readers of events MUST accept the immediately prior schema version (N-1) for every event type AND for the envelope. Per-type independence means harmonik maintains up to 71+ independent compatibility contracts (the §8 taxonomy today is 71 event types; per-type versions evolve independently). Breaking changes (per §6.4 table) require a migration release scheduled at an operator pause per [operator-nfr.md §4.3].

Tags: mechanism

#### EV-030 — Breaking-change classification is declared in §6.4

A schema change is breaking per the classification table in §6.4. Additive changes (new optional field, type widening, new enum variant without required-semantics shift) are non-breaking.

Tags: mechanism

### 4.8 Tagging obligations

#### EV-031 — Every event type declared in §8 MUST carry the four-axis tags AND a durability class

Every §8 entry MUST have the four-axis tags (`llm-freedom`, `io-determinism`, `replay-safety`, `idempotency`), the `mechanism` / `cognition` tag per SC-10, AND a `durability_class`. Event emission itself is mechanism-tagged in every case; cognition-tagged events are forbidden. A cognition-tagged operation that produces an event does so by calling the mechanism-tagged emission primitive. The payload-registry (§6.3) is the authoritative carrier; §8 tables carry only the durability class for display compactness, with full tags registered per-type in the Go registry.

Tags: mechanism

### 4.9 Go representation

#### EV-032 — Events are a tagged-union envelope plus a payload-constructor registry

Event types in Go MUST be represented as a tagged-union: a top-level `Event` envelope struct carrying common fields plus `Payload json.RawMessage`; a per-type constructor registry `map[EventType]func() EventPayload` decodes `Payload` keyed by `Event.type`.

Tags: mechanism

#### EV-033 — Type dispatch is deterministic on the `type` field

Given a valid envelope, payload-type resolution MUST be a deterministic map lookup. If the registry has no entry, the reader MUST surface a typed `ErrUnknownEventType` and skip the event (observational consumers) or fail with a structured error (synchronous consumers).

Tags: mechanism

#### EV-034 — Registry registration is startup-time

Payload-type registration MUST happen at daemon init (via `init()` functions or `RegisterEventType` calls during startup). Runtime registration after the first event is emitted MUST be a startup-time error. The registry is sealed at the same time the bus is sealed (EV-009).

Tags: mechanism

#### EV-034a — `source_subsystem` identifiers are registered at startup

Each subsystem MUST register its `source_subsystem` identifier at daemon init; duplicates MUST fail startup with a typed error. This catches typos and prevents two subsystems from sharing an identifier.

Tags: mechanism

### 4.10 Redaction

#### EV-035 — Redaction registry applied before emission

The bus MUST apply the redaction registry ([handler-contract.md §4.7]) to every event payload before appending to JSONL AND before dispatch to consumers. Fields whose names match `(?i)(secret|token|password|api[_-]?key|auth)` MUST be replaced with `"<redacted>"`. Per-handler additional redaction patterns MUST be applied by the same path. This is a best-effort defense; the compile-time check (EV-036) is the structural guardrail.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

#### EV-036 — Compile-time check: no payload field name matches the secret-prefix rule

At daemon startup the payload-type registry MUST be scanned; any registered payload type whose struct field names match the secret-prefix rule MUST cause startup to fail with a typed configuration error.

Tags: mechanism

### 4.11 `harmonik subscribe` consumer contract

`harmonik subscribe` is the primary push transport for external consumers (e.g. the flywheel cognition loop). These obligations are ADDITIONAL to the in-process consumer rules (§4.3 EV-009–EV-014d); they apply specifically to the out-of-process, NDJSON-over-stdout subscribe interface.

#### EV-037 — External consumers MUST persist a watermark of `last_processed_event_id`

The consumer MUST persist the UUIDv7 of the last fully-processed event to a stable location (e.g. `.harmonik/cognition/watermark.json`). On reconnect or cold-start it MUST supply this as `--since-event-id` to `harmonik subscribe`, triggering server-side replay before live-stream resumes. The consumer MUST NOT advance its watermark BEFORE recording any reaction to the processed event; required ordering: effect → ledger-entry → watermark-advance. Crash between effect and ledger-entry is recovered via effect idempotency (keyed on `event_id`); crash between ledger-entry and watermark-advance causes a re-read that finds the ledger entry and skips. Watermark keys MUST be UUIDv7 `event_id`, NEVER a byte offset (byte offsets are rotation-unsafe and undefined after log compaction).
Tags: mechanism

#### EV-037a — Watermark MUST NOT regress

Safe advance rule: `watermark = max(persisted_watermark, incoming_event_id)`. On heartbeat whose `last_event_id > current watermark`, advance even when no actionable event was processed — this is how quiet periods advance the watermark without LLM work. No-regression invariant prevents a context-reset crash from re-processing already-reacted events.
Tags: mechanism

#### EV-038 — Consumers MUST treat `subscription_gap` as a forced re-sync trigger

On `subscription_gap{dropped:N}` (drop-oldest overflow per `internal/daemon/subscribe.go`), the consumer MUST NOT continue as if no events were lost. It MUST: (a) `ScanAfter(watermark)` on `events.jsonl` to replay the gap; (b) re-sense `queue.json` and the git completion log (for any run_id whose terminal event may have been dropped). Only after re-sync may live-stream processing resume. Required because a dropped event might be a Tier-2 judgment event (e.g. `run_failed`, `merge_conflict_escalation`, `iteration_cap_hit`) whose loss would silently leave the consumer in an incorrect state.
Tags: mechanism

#### EV-039 — Heartbeat carries `last_event_id` + `active_runs`; consumers MUST use both

Heartbeat (default 60s) payload: `last_event_id` (UUIDv7 string), `active_runs[]` array of `{bead_id: String, age_seconds: Integer}`. Consumers MUST advance their watermark to `last_event_id` on every heartbeat (per EV-037a). Consumers SHOULD inspect `active_runs[].age_seconds`; if any run exceeds a configured stall threshold, treat as a synthetic Tier-2 wake to investigate. **The `active_runs` array carries `bead_id` + `age_seconds` ONLY; it does NOT carry `run_id`.** Consumers MUST NOT assume `run_id` is present; run-level correlation requires reading `queue.json`.
Tags: mechanism

#### EV-040 — Missing heartbeats = daemon liveness failure; reconnect with backoff

No heartbeat for `K × heartbeat_interval` (recommended K=2 → 120s at 60s) → treat as daemon liveness failure. Reconnect with exponential backoff (suggested 5s/10s/30s). If `harmonik subscribe` exits 17 (daemon-not-running sentinel), emit a synthetic `daemon_down` signal to the consumer's reaction layer. Reconnection MUST supply `--since-event-id=<watermark>` (per EV-037); MUST NOT start from live-stream head on reconnect — terminal events emitted during the outage would be missed.
Tags: mechanism

#### EV-041 — Git-done-but-no-terminal-event after K heartbeats SHOULD trigger a wake

If after K consecutive heartbeats (suggested K=2) a `bead_id` has disappeared from `active_runs` yet no terminal event has been processed for that run since the watermark, the consumer SHOULD git-check the missing `run_id` (`git log --all --grep "Harmonik-Run-ID: <run_id>"`). A merged commit without a terminal event = daemon crashed mid-terminal-emission; treat the git completion as authoritative (per EV-INV-001) and synthesize a Tier-1 reaction (advance kerf baseline, close stale bead) without waiting for an event that will never arrive. This SHOULD does not override EV-022 (state reconstruction MUST walk git+Beads); it's an observational heuristic.
Tags: mechanism

### 4.12 `decision_required` dispatch-blocking rule

#### EV-042 — `decision_required` MUST be emitted on the four canonical conditions

Daemon MUST emit on each condition enumerated in §8.12.1. Emission MUST be fsync-backed (class F) and MUST precede any state mutation the condition would otherwise trigger (e.g. workloop MUST NOT attempt a third dispatch for a double-failed bead before `decision_required` is durable). Emission idempotency-keyed on `triggering_event_id`; re-processing after restart MUST NOT produce a second event for an already-pending `ack_token`.
Tags: mechanism

#### EV-043 — Unacknowledged `decision_required` blocks dispatch for its subject

While a `decision_required` for a given `subject` is unacknowledged (no matching `decision_acknowledged` in `events.jsonl` AND `.harmonik/decision_acks/<ack_token>` absent or `status=pending`), daemon MUST NOT dispatch a new run for that subject. Blocking check MUST run at daemon startup (EV-043a) AND at every workloop dispatch attempt for the subject. ACK via `harmonik decision ack <token>` or cognition-loop `note()` unblocks atomically; `decision_acknowledged` MUST be emitted+fsynced BEFORE the workloop is permitted to dispatch.
Tags: mechanism

#### EV-043a — Startup MUST restore `decision_required` blocking state

On startup, daemon MUST scan `.harmonik/decision_acks/` for records `status=pending` and restore the corresponding dispatch-blocking state BEFORE the workloop begins dispatching. Loss of a `decision_required` event from JSONL (ordinary tail-truncation) is survived via `.harmonik/decision_acks/`, which is fsynced independently as the authoritative ack-state store. The ack-state file is the durability anchor; the JSONL event is the observational record.
Tags: mechanism

#### EV-044 — Unacknowledged `decision_required` is a digest exception

Any cognition-loop or external monitoring consumer producing a periodic digest MUST surface every unacknowledged `decision_required` in that digest, regardless of whether a Tier-2 action has been taken. MUST NOT silently suppress on the grounds that the consumer has already "seen" the event; suppression is only valid after `decision_acknowledged` for the matching `ack_token` is observed.
Tags: mechanism

## 5. Invariants

#### EV-INV-001 — Events are observational, not authoritative

The JSONL event log MUST NEVER be treated as authoritative state. Git plus Beads is authoritative per [execution-model.md §4.7 EM-INV-001].

Tags: mechanism

#### EV-INV-002 — Event-loss between fsyncs is acceptable; consumers MUST handle it

A hard crash between fsync boundaries MAY lose events emitted in that window. Producers satisfy this invariant via EV-018 (idempotent emission). Consumers MUST be coded to handle a tail-truncated event stream on recovery per EV-014b. This invariant is a two-sided operational covenant, not a producer-only claim.

Tags: mechanism

#### EV-INV-003 — At most one synchronous consumer per event type, no reentrant emission

For every event type in §8, `synchronous` consumers MUST have cardinality at most one AND MUST NOT emit events that re-dispatch to themselves (EV-010 acyclicity clause).

Tags: mechanism

#### EV-INV-004 — Every event carries a valid monotonic UUIDv7 `event_id`

Every event MUST have `event_id` be a valid UUIDv7 AND MUST be strictly greater than the immediately preceding `event_id` emitted by the same process (EV-002a).

Tags: mechanism

#### EV-INV-005 — Every event type in §8 carries full tagging

Every §8 entry MUST carry the four-axis tags, `mechanism` tag, and `durability_class`. No cognition-tagged event type exists.

Tags: mechanism

#### EV-INV-006 — Best-effort redaction plus compile-time structural check

No event payload SHOULD contain a field value that was a secret at emission time; the redaction registry (EV-035) is best-effort. The compile-time check (EV-036) structurally rules out secret-named fields on registered payloads, discharging the invariant at the structural level.

Tags: mechanism

## 6. Schemas and data shapes

### 6.1 Envelope RECORD

```
RECORD Event:
    event_id             : UUID                       -- UUIDv7; monotonic intra-process per EV-002a
    schema_version       : Integer                    -- envelope version; bump on envelope-level change
    type                 : EventType                  -- one of the types in §8
    timestamp_wall       : Timestamp                  -- RFC 3339 wall-clock at emitter
    timestamp_mono_nsec  : Integer | None             -- monotonic ns from emitter process; process-scoped per EV-003
    run_id               : UUID | None                -- present when scoped to a run
    state_id             : UUID | None                -- present when scoped to a state
    source_subsystem     : String                     -- Go package identifier per EV-004, registered per EV-034a
    trace_context        : TraceContext | None        -- for cross-subsystem correlation
    payload              : Bytes                      -- json.RawMessage; decoded per §6.3 registry
```

```
RECORD TraceContext:
    trace_id             : String | None              -- optional external correlation
    parent_event_id      : UUID | None                -- event that causally preceded this one; SHOULD be populated when causal linkage is known
    root_event_id        : UUID | None                -- event originating the causal chain
```

`EventType` is the enum of every §8 type. The list is generated from §8 rows; expansion requires amendment per EV-027.

```
RECORD Subscription:
    consumer_id                 : String                         -- opaque identifier, unique per bus; enforced at registration
    consumer_class              : Enum (synchronous | asynchronous | observer)  -- EV-010/011/012
    event_pattern               : EventPattern                   -- wildcard ("*") or explicit list of EventType; see below
    since                       : UUID | None                    -- optional replay offset; bus replays JSONL from this event_id before live delivery per EV-014d
    offset_checkpoint_event_id  : UUID | None                    -- consumer's last durably processed event_id; consumer SHOULD persist this in its own store and supply it as `since` on restart
    on_panic                    : Enum (recover_and_log | quarantine_consumer | fail_daemon)  -- policy for consumer-goroutine panics per OQ-EV-007 default `recover_and_log`
    handler                     : Function(ctx, Event) -> error  -- consumer-supplied
```

```
RECORD EventPattern:
    wildcard  : Boolean           -- when true, matches every current and future EventType
    types     : Set<EventType>    -- explicit type list; empty when wildcard=true
```

```
INTERFACE EventBus:
    Emit(ctx, type, payload) -> error                             -- redacts (EV-035); appends JSONL (EV-015) with fsync per class (EV-016); then dispatches sync/async/observer per EV-014a
    Subscribe(sub Subscription) -> (Subscription, error)          -- startup-only per EV-009; post-seal fails; if sub.since is non-nil, bus will replay_from(since) before live delivery
    Seal() -> error                                               -- called by daemon.Start once subscription registration is complete
    ReplayFrom(consumer_id, since event_id) -> error              -- re-issues JSONL events whose event_id is strictly greater than `since` to the named consumer; consumers MUST be idempotent per EV-014b
    DeadLetterReplay(consumer_name, filter?) -> error             -- operator-initiated replay from dead-letter log; consumers MUST be idempotent per EV-014b
    Drain(ctx) -> error                                           -- quiescence primitive; returns after all in-flight dispatches for all consumers complete
```

The bus MUST invoke an optional consumer-supplied `on_tail_truncation(ctx, last_durable_event_id)` callback immediately after restart replay completes when the JSONL tail was truncated by the read-recovery rule (§6.2). Consumers that do not supply the callback receive no truncation signal and operate under EV-INV-002's "tolerate loss" obligation alone.

### 6.2 On-disk JSONL format

- **Primary log:** `.harmonik/events/events.jsonl` — single-file-per-project, append-only.
- **Dead-letter log:** `.harmonik/events/dead-letters.jsonl` — same line format with additional top-level `dead_letter: {consumer_name, retries_attempted, enqueued_at}`.
- **Spill files:** `.harmonik/events/spill-<consumer>.jsonl` — per-consumer spill for `fsync-boundary` events that could not be queued per EV-011a.

```json
{
  "event_id": "<UUIDv7>",
  "schema_version": 1,
  "type": "checkpoint_written",
  "timestamp_wall": "2026-04-24T14:22:11.482Z",
  "timestamp_mono_nsec": 918273645,
  "run_id": "<UUID>",
  "state_id": "<UUID>",
  "source_subsystem": "github.com/harmonik/internal/orchestrator",
  "trace_context": { "trace_id": "<String>", "parent_event_id": "<UUID>", "root_event_id": "<UUID>" },
  "payload": { "run_id": "<UUID>", "state_id": "<UUID>", "transition_id": "<UUID>", "commit_hash": "<git SHA>", "bead_id": "<String|null>" }
}
```

Read-recovery rules (extended):

- **Torn tail.** A final line that lacks a terminating newline OR fails JSON parse OR parses as JSON but fails envelope schema validation (unknown `type`, missing required envelope field) is a torn tail. In post-crash startup-recovery context the reader MUST discard the torn tail silently (the expected lossy-tail shape). In all other read contexts (investigator walk, observational replay on a live daemon) the reader MUST emit `store_divergence_detected{divergence_kind=schema_mismatch, post_crash_window=false}` before discarding the line.
- **Mid-file corruption.** A non-final line that fails JSON parse indicates corruption (block reordering, media error, or torn write followed by an appended line). The reader MUST emit `store_divergence_detected{divergence_kind=parse_failure}` carrying a `byte_offset` and halt the reader; the condition escalates to reconciliation Cat 6 per [reconciliation/spec.md §8]. The reader MUST NOT skip the corrupt line and continue.
- **Empty log.** An empty or absent `events.jsonl` at daemon startup is a valid fresh-project state when git and Beads also carry no prior daemon cycle. When git or Beads DO carry prior-cycle evidence, the empty log MUST emit `store_divergence_detected{divergence_kind=log_missing, post_crash_window=true}` and enter reconciliation's degraded-start path.
- **Concurrent tailing.** A reader tailing `events.jsonl` while the writer is actively appending MUST treat the currently-growing file's final line as non-authoritative until a terminating newline is observed. POSIX `O_APPEND` atomicity is bounded to `PIPE_BUF` (4096 bytes); JSONL lines may exceed this, so readers MUST NOT assume any single in-flight line is atomic. Concurrent readers MUST NOT take exclusive file locks; the writer's append-only invariant (EV-020) plus the newline-sentinel is the reader's sole synchronization primitive.
- **Post-fsync tail.** Events past the last durable fsync on a post-crash log MAY be absent; readers operate per EV-017.

### 6.3 Per-type payload schemas (selected)

Every type listed in §8 has a payload whose fields are named in §8's "Payload fields" column; the canonical Go shape lives in the registry per EV-032. Concrete YAML for the most-consumed types:

#### `run_started`

```yaml
run_id: <UUID>
workflow_id: <UUID>
workflow_version: <String>
bead_id: <String> | null
workspace_path: <String>
input_ref: <String>
queue_id: <String> | null              # UUIDv7 as string; populated when the run was dispatched from a queued submission per §8.10 / [queue-model.md §4]
queue_group_index: <Integer> | null    # populated alongside queue_id; identifies the group within the queue
```

The `queue_id` and `queue_group_index` fields are OPTIONAL additive fields per §6.4 row 1; older readers ignore them. The fields are populated only when the run was dispatched from a queued submission; foreground / single-bead invocations leave both null. The same optional pair appears on `run_completed` and `run_failed` below.

#### `run_completed`

```yaml
run_id: <UUID>
terminal_state_id: <UUID>
ended_at: <Timestamp>
summary: <String> | null
queue_id: <String> | null              # UUIDv7 as string; populated when the run was dispatched from a queued submission per §8.10
queue_group_index: <Integer> | null    # populated alongside queue_id
```

The `queue_id` / `queue_group_index` pair is OPTIONAL per §6.4 row 1 and carries the same semantics as on `run_started`.

#### `run_failed`

```yaml
run_id: <UUID>
terminal_state_id: <UUID> | null
failure_class: <FailureClass>   # coarse bucket per execution-model.md §8
error_category: <ErrorCategory> | null  # narrow sentinel per handler-contract.md §4.5 when the failure originated from a handler; absent for orchestrator-originated failures
ended_at: <Timestamp>
reason: <String>
queue_id: <String> | null              # UUIDv7 as string; populated when the run was dispatched from a queued submission per §8.10
queue_group_index: <Integer> | null    # populated alongside queue_id
```

The `queue_id` / `queue_group_index` pair is OPTIONAL per §6.4 row 1 and carries the same semantics as on `run_started`.

`failure_class` is the coarse bucket; `error_category` is the narrow sentinel when present. Consumers SHOULD key on `failure_class` for bucket-level decisions and on `error_category` for handler-origin detail. `error_category` is absent for orchestrator-originated failures (e.g., `compilation_loop`) that have no handler-origin sentinel.

#### `checkpoint_written`

```yaml
run_id: <UUID>
state_id: <UUID>
transition_id: <UUID>
commit_hash: <String>
bead_id: <String> | null
```

#### `transition_event`

```yaml
run_id: <UUID>
transition_id: <UUID>
from_state_id: <UUID>
to_state_id: <UUID>
commit_hash: <String>
transition_kind: <TransitionKind>  # per §3 glossary + execution-model.md §6.1
```

#### `agent_output_chunk`

```yaml
run_id: <UUID>
session_id: <UUID>  # UUIDv7 per handler-contract.md §4.1; opaque to non-handler consumers
chunk_index: <Integer>
bytes_emitted: <Integer>
chunk_digest: <String> | null
```

All `session_id` fields across §8.3 agent/handler events share this typing: UUIDv7 generated by the handler and treated as opaque by non-handler consumers. Cross-ref: [handler-contract.md §4.1].

#### `agent_rate_limit_status`

```yaml
run_id: <UUID>
session_id: <UUID>
status: <enum: active | cleared>
rate_limit_source: <String> | null
retry_after_seconds: <Integer> | null
changed_at: <Timestamp>
```

#### `workspace_merge_status`

```yaml
workspace_id: <UUID>
run_id: <UUID>
status: <enum: pending | merged>
source_branch: <String>
target_branch: <String>
merge_commit_hash: <String> | null   # null when status=pending
changed_at: <Timestamp>
```

#### `operator_pause_status`

```yaml
status: <enum: pausing | paused>
changed_at: <Timestamp>
operator_id: <String> | null
```

#### `store_divergence_detected`

```yaml
run_id: <UUID> | null
bead_id: <String> | null
divergence_kind: <enum: checkpoint_missing | beads_closed_no_commit | jsonl_references_missing_commit | parse_failure | schema_mismatch | log_missing>
evidence_ref: <String>
post_crash_window: <Boolean>         # true when evidence falls in the post-crash lossy-tail window per EV-023
corroboration: <enum: git-corroborated | beads-corroborated>   # per EV-023a; inconclusive cases emit divergence_inconclusive instead
```

> **(post-MVH)** The `divergence_kind` enum is closed at the MVH set above. Adapter-specific values (e.g., for the `br` Beads adapter per [beads-integration.md §4.10] and OQ-BI-008) MAY be added in a future revision via the §4.6 amendment protocol; until then, adapters emit `divergence_inconclusive` (§8.6.10) per EV-023a's single-authority semantics. No adapter-specific values are added in this revision.

#### `divergence_inconclusive`

```yaml
run_id: <UUID> | null
bead_id: <String> | null
evidence_ref: <String>
post_crash_window: <Boolean>
reason: <enum: no_authority_reference | authority_unavailable>   # per EV-023a
```

#### `operator_escalation_required`

```yaml
target_run_id: <UUID> | null
reason: <enum: cat_6a_investigator_escalated | cat_6b_auto_escalated | cat_3_stale_write | budget_exhausted | merge_conflict | gate_escalated | other_verdict_driven>
reference_commits: <String[]> | null
```

#### `infrastructure_unavailable`

```yaml
failed_prerequisite: <enum: br_missing | br_timeout | br_version_incompatible | beads_sqlite_locked | git_index_locked | harmonik_dir_unwritable | filesystem_full>
detail_string: <String>
retry_count: <Integer>
```

#### `bus_overflow`

```yaml
consumer_name: <String>
event_type: <EventType>
event_id: <UUID>
queue_depth: <Integer>
shed_at: <Timestamp>
shed_policy: <enum: fsync-spilled | ordinary-dropped | lossy-dropped>   # fsync-spilled means the event was redirected to spill-<consumer>.jsonl per EV-011a; *-dropped means the event was shed
```

`shed_policy` lets a consumer of `bus_overflow` attribute the shed without cross-referencing §8 for the event's durability class. Overflow handlers seeing `fsync-spilled` should check the spill file for reconciliation; `ordinary-dropped` and `lossy-dropped` are acceptable losses under EV-017 / EV-INV-002.

#### `policy_expression_exceeded_cost`

```yaml
run_id: <UUID> | null
control_point_name: <String>
bound_fired: <enum: ast_steps | wall_clock>     # which CP-034b bound triggered the abort
io_determinism: <enum: deterministic | best-effort>   # per-abort tag per CP-034b NOTE; deterministic only when bound_fired=ast_steps
aborted_at: <Timestamp>
```

`bound_fired` and `io_determinism` are load-bearing per [control-points.md §4.7 CP-034b]: operators diagnosing cost-ceiling crossings depend on the discriminator, and the io-determinism tag MUST track the bound that fired (`ast_steps` ⇒ `deterministic`; `wall_clock` ⇒ `best-effort`). The event reaches JSONL durability before the evaluator wrapper returns control to its caller per the CP-034b durability-pair rule; durability class is `F` per §8.2.12. Re-adding either field post-MVH would be a breaking event-payload change.

#### `daemon_ready`

```yaml
ready_at: <Timestamp>                           # RFC 3339 wall-clock at the daemon's ready transition
ready_at_ns_since_boot: <Integer>               # uint64 monotonic clock reading at ready, in ns since the host's boot; complements ready_at for RTO measurement under wall-clock skew per [operator-nfr.md §4.8 ON-033]
investigator_run_ids: <UUID[]>
```

`ready_at_ns_since_boot` is REQUIRED. ON-033 mandates that the RTO measurement boundary use a monotonic-corrected source so SIGTERM-receipt and `ready` emission timestamps are comparable across NTP adjustments and VM pause/resume. On boot-transition cycles (where the monotonic clock resets), the operator-nfr-side RTO computation marks the cycle `rto_undefined` per ON-033; `ready_at_ns_since_boot` is still emitted (it is well-defined within a single boot epoch).

#### `daemon_shutdown`

```yaml
shutdown_at: <Timestamp>                        # RFC 3339 wall-clock at the daemon's shutdown emission
shutdown_at_ns_since_boot: <Integer>            # uint64 monotonic clock reading at shutdown, in ns since the host's boot; complements shutdown_at for RTO measurement per ON-033
mode: <enum: graceful | immediate>
```

Durability class is `F` per §8.7.3 (resolves OQ-PL-012 — `daemon_shutdown` is fsync-boundary so RTO reconstruction can identify the SIGTERM-receipt landmark on the prior daemon cycle). `shutdown_at_ns_since_boot` is REQUIRED for graceful shutdowns. SIGKILL terminations have no `daemon_shutdown` emission at all (no defer-recover gets to run); ON-033 marks those RTO cycles `rto_undefined`.

#### `daemon_degraded`

```yaml
detected_at: <Timestamp>
reason: <enum: rto_breach | reconstruction_notify | clock_regression | cat0_post_ready | infrastructure_unavailable | silent_hang_aggregate>
```

`reason` is exhaustive. New variants require an EV-027 amendment. The `cat0_post_ready` variant is RC-012a's carve-out: a Cat 0 prerequisite failure observed AFTER the daemon has reached `ready` MUST emit `daemon_degraded{reason=cat0_post_ready}` but MUST NOT transition the §6.1 daemon-status enum from `ready` to `degraded` (per [reconciliation/spec.md §4.2 RC-012a]). The `clock_regression` variant is EV-002c's HWM regression carve-out. The `silent_hang_aggregate` variant is the [operator-nfr.md §4.9 ON-040] silent-hang-fan-out aggregator.

#### `implementer_resumed`

```yaml
run_id: <UUID>
workflow_mode: <enum: single | review-loop | dot>   # always "review-loop" for this event
session_id: <UUID>                                   # harmonik's internal handler-minted UUIDv7 per [handler-contract.md §4.1]; distinct from claude_session_id; correlates with agent_started/agent_completed
claude_session_id: <String>                          # Claude Code session identifier per §3 glossary
iteration_count: <Integer>                           # 1-based; per [execution-model.md §4.3 EM-015d]
prior_verdict_summary: <String>                      # ≤ 256 bytes; front-truncation of prior reviewer notes per §6.3 derivation rule below
```

> Derivation rule for `prior_verdict_summary` (§8.1a.1). The daemon MUST derive this field at MVH by taking the first 256 UTF-8 bytes of the prior iteration's `reviewer_verdict.notes` field (front-truncate), discarding any incomplete trailing UTF-8 sequence. If the prior verdict had `verdict ∈ {APPROVE, BLOCK}` (no implementer resume occurs), this event MUST NOT fire. Iteration 1's implementer launch does NOT emit `implementer_resumed` (it is the initial dispatch); the field is therefore only populated from iteration 2 onward.

#### `reviewer_launched`

```yaml
run_id: <UUID>
workflow_mode: <enum: single | review-loop | dot>   # always "review-loop" for this event
session_id: <UUID>                                   # harmonik's internal handler-minted UUIDv7 per [handler-contract.md §4.1]; distinct from claude_session_id; correlates with agent_started/agent_completed
claude_session_id: <String>                          # Claude Code session identifier; the reviewer launches a fresh session (NOT resumed) per [execution-model.md §4.3 EM-015d], but the reviewer's own session-id is captured here for trace continuity
iteration_count: <Integer>
```

#### `reviewer_verdict`

```yaml
run_id: <UUID>
workflow_mode: <enum: single | review-loop | dot>   # always "review-loop" for this event
session_id: <UUID>                                   # harmonik's internal handler-minted UUIDv7 per [handler-contract.md §4.1]; distinct from claude_session_id; correlates with agent_started/agent_completed
claude_session_id: <String>
iteration_count: <Integer>
schema_version: <Integer>                            # MUST equal 1 (agent-reviewer JSON schema v1)
verdict: <enum: APPROVE | REQUEST_CHANGES | BLOCK>   # from .harmonik/review.json verbatim
flags: <String[]>                                    # issue tags from agent-reviewer schema v1; MAY be empty
notes: <String>                                      # free text from agent-reviewer schema v1; 1–3 sentences per skill contract
```

The `schema_version`, `verdict`, `flags`, and `notes` fields are passed through from `.harmonik/review.json` after validation per §8.1a's reviewer-verdict schema-reuse rule. The verdict file MUST be archived to `.harmonik/review.iter-<iteration_count>.json` by the daemon per [execution-model.md §4.3 EM-015d] before the next iteration's reviewer launch (so the file slot is free for the next reviewer to write into).

#### `iteration_cap_hit`

```yaml
run_id: <UUID>
workflow_mode: <enum: single | review-loop | dot>   # always "review-loop"
iteration_count: <Integer>                           # = cap_value at MVH
cap_value: <Integer>                                 # = 3 at MVH per [execution-model.md §4.3 EM-015e]
final_verdict: <enum: REQUEST_CHANGES | BLOCK>       # the verdict at the cap-hit boundary
```

#### `no_progress_detected`

```yaml
run_id: <UUID>
workflow_mode: <enum: single | review-loop | dot>   # "review-loop" for builtin review-loop path; "dot" for DOT cascade path (see §8.1a.5 normative note)
iteration_count: <Integer>                           # the iteration AT which no-progress was detected (iteration ≥ 2)
diff_hash_current: <String>                          # SHA-256 of `git diff <parent>..<head>` at the current iteration's worktree state; hex-encoded
diff_hash_prior: <String>                            # SHA-256 of the prior iteration's diff; hex-encoded; equal to diff_hash_current at emission time
```

#### `review_fixup_stalled`

Emitted when a REQUEST_CHANGES fix-up run advances HEAD by zero commits — the implementer was given reviewer feedback but produced no new commit. Carries the reviewer flags from the prior REQUEST_CHANGES verdict so triage can see the specific flag the implementer failed to address. Replaces `no_progress_detected` on the post-REQUEST_CHANGES no-commit path in both review-loop and DOT modes. `iteration_count` MUST be ≥ 2 (structural proof that at least one REQUEST_CHANGES verdict was issued). `reviewer_flags` MUST be a non-nil array (MAY be empty if the verdict carried no flags). Bead ref: hk-m1wqp.

```yaml
run_id: <UUID>
workflow_mode: <enum: single | review-loop | dot>   # "review-loop" for builtin review-loop path; "dot" for DOT cascade path
iteration_count: <Integer>                           # the iteration AT which the stall was detected (≥ 2)
reviewer_flags: <String[]>                           # flags from the prior REQUEST_CHANGES verdict (non-nil; MAY be empty)
diff_hash_current: <String>                          # SHA-256 of `git diff <parent>..<head>` at the current iteration's worktree state; hex-encoded
diff_hash_prior: <String>                            # SHA-256 of the prior iteration's diff; hex-encoded; equal to diff_hash_current at emission time
```

#### `review_loop_cycle_complete`

```yaml
run_id: <UUID>
workflow_mode: <enum: single | review-loop | dot>   # always "review-loop"
final_iteration_count: <Integer>                     # 1..3 at MVH; = iteration_count at termination
completion_reason: <enum: approved | cap_hit | blocked | no_progress | fixup_stalled | error>   # see [execution-model.md §4.3 EM-015e]; fixup_stalled added hk-m1wqp
```

#### `bead_label_conflict`

```yaml
bead_id: <String>                                    # opaque bead identifier per [beads-integration.md §4.3 BI-008]
conflicting_labels: <String[]>                       # the offending `workflow:<mode>` labels observed on the bead (length ≥ 1)
fallback_action: <String>                            # describes the daemon's fallback behavior (e.g., "tier-1 input treated as absent; precedence walk continues to tier 2")
detected_at: <Timestamp>
```

Emitted by the daemon's claim path per [execution-model.md §4.3 EM-012a] when tier-1 mode resolution encounters either multiple `workflow:<mode>` labels on a single bead or a single label naming an unknown mode value. The event does not gate the run's dispatch; the precedence walk falls through to tier 2 / 3 / 4.

#### `queue_submitted`

```yaml
queue_id: <String>                       # UUIDv7 as string per [queue-model.md §4 QM-010..012]
submitted_at: <Timestamp>
group_count: <Integer>
total_bead_count: <Integer>
schema_version: <Integer>                # version of the queue.json document per [queue-model.md §2]; distinct from the event envelope schema_version per EV-028
```

Per §6.4 conventions every new payload starts at envelope `schema_version: 1`; the `schema_version` field above is the queue.json document version surfaced for consumers (the envelope-level `schema_version` lives on the §6.1 envelope, not in this payload).

#### `queue_group_started`

```yaml
queue_id: <String>                       # UUIDv7 as string per [queue-model.md §4 QM-010..012]
group_index: <Integer>
group_kind: <enum: wave | stream>        # per [queue-model.md §2]
item_count: <Integer>
started_at: <Timestamp>
```

#### `queue_group_completed`

```yaml
queue_id: <String>                       # UUIDv7 as string per [queue-model.md §4 QM-010..012]
group_index: <Integer>
final_status: <enum: complete-success | complete-with-failures>   # per [queue-model.md §5]
success_count: <Integer>
fail_count: <Integer>
completed_at: <Timestamp>
```

#### `queue_paused`

```yaml
queue_id: <String>                       # UUIDv7 as string per [queue-model.md §4 QM-010..012]
group_index: <Integer>
fail_count: <Integer>
paused_at: <Timestamp>
reason: <String>                         # enum: group_failure | operator_drain — per [queue-model.md §5, §8]
```

`reason` is an exhaustive enum at MVH (`group_failure` is the v0.1 pause-by-failure path; `operator_drain` is the operator-initiated drain path). New variants require an EV-027 amendment. v0.1 ships no `queue-resume` operation; `queue_resumed` is reserved for v0.2 per the design rationale (see §A.3).

#### `queue_appended`

```yaml
queue_id: <String>                       # UUIDv7 as string per [queue-model.md §4 QM-010..012]
group_index: <Integer>
appended_bead_ids: <String[]>
appended_at: <Timestamp>
```

Emitted on stream-group mutation per [queue-model.md §7]; ignored on wave groups (a wave group's contents are immutable post-submit).

#### `queue_item_deferred_for_ledger_dep`

```yaml
queue_id: <String>                       # UUIDv7 as string per [queue-model.md §4 QM-010..012]
group_index: <Integer>
bead_id: <String>
blocker_bead_id: <String>
detected_at: <Timestamp>
```

Informational observability of submit-time and dispatch-time ledger-dependency deferrals per [queue-model.md §6 QM-020..026]. The ledger remains the authority for `blocks` edges; loss is tolerable because the dispatcher re-evaluates eligibility from ledger state on each dispatch tick.

#### `queue_item_reconciled`

```yaml
queue_id: <String>                       # UUIDv7 as string per [queue-model.md §4 QM-010..012]
group_index: <Integer>
bead_id: <String>
reason: <String>                         # enum: "claim_write_lost"
reconciled_at: <Timestamp>
```

Startup reconciliation correction per [queue-model.md §3.2a QM-002a]: emitted when an item recorded as `dispatched` in queue.json is found to be `open` in the Beads ledger at daemon startup, indicating a prior claim-write succeeded for the queue but the corresponding Beads write was lost. The item is reverted to `pending` before this event is emitted. Class F: loss could silently re-dispatch an already-reverted item, so the correction MUST be durable before proceeding.

#### `decision_required`

```yaml
subject:
  kind: <enum: bead | queue>
  id: <String>
reason: <enum: bead_double_failure | iteration_cap_hit | merge_conflict_escalation | queue_group_failure>
suggested_action: <String>     # free text; SHOULD be ≤ 256 bytes
ack_required: <Boolean>        # always true at v1; reserved for future advisory-only signals
ack_token: <String>            # opaque UUIDv4; unique per emission; key for .harmonik/decision_acks/
triggering_event_id: <UUID>    # event_id of the condition event that caused this emission (dedup key)
```

`reason` is exhaustive at v1; new variants require an EV-027 amendment. `triggering_event_id` MUST reference a specific event in `events.jsonl`. `ack_token` is the durability anchor; `.harmonik/decision_acks/<ack_token>` is fsynced independently per EV-043a. Class F: loss silently leaves a double-failed bead eligible for re-dispatch.

#### `decision_acknowledged`

```yaml
ack_token: <String>            # MUST match the ack_token of the matching decision_required
subject:
  kind: <enum: bead | queue>
  id: <String>
ack_method: <enum: operator | note>
acked_at: <Timestamp>
```

Class F: loss breaks JSONL observability for the acknowledgement (ack-state file remains authoritative per EV-043a). `ack_method=operator` indicates `harmonik decision ack <token>` CLI; `ack_method=note` indicates cognition-loop `note()` implicit ACK.

#### `gate_definition_drift`

```yaml
run_id: <UUID>
gate_name: <String>                        # name of the Gate-kind ControlPoint (CP-002)
prior_envelope_hash: <String>              # SHA-256 hex of the envelope at original evaluation per CP-038a
current_envelope_hash: <String>            # SHA-256 hex of the envelope recomputed at replay time
changed_inputs: <String[]>                 # subset of {expression_text, context_subset, policy_meta} that changed
```

Emitted during replay when a mechanism-tagged Gate's envelope inputs (per [control-points.md §4.7 CP-038a]) differ between the original evaluation and the replay attempt. The run MUST NOT silently re-evaluate using the new definition; Cat 6 reconciliation is required per CP-038a. Durability class is `F` (the replay is blocked on this event reaching JSONL durability before the Cat 6 escalation fires).

#### `gate_redefined_under_cat_6`

```yaml
run_id: <UUID>
gate_name: <String>                        # name of the Gate-kind ControlPoint (CP-002)
prior_decision: <enum: allow | deny | escalate-to-human>    # the decision from the original evaluation
new_decision: <enum: allow | deny | escalate-to-human>      # the decision from the Cat 6 re-evaluation
cat_6_verdict_id: <String>                 # identifier of the Cat 6 reconciliation verdict that authorized re-evaluation
```

Emitted when a Cat 6 reconciliation verdict authorizes mechanism-tagged Gate re-evaluation under a drifted definition per [control-points.md §4.7 CP-038a]. The `prior_decision` is read from the JSONL `gate_allowed` / `gate_denied` event for the original transition. Durability class is `F` (the re-evaluation outcome is a lifecycle boundary).

#### `session_keeper_handoff_written` (§8.20.1)

```yaml
agent_name: <String>
cycle_id: <String>                 # REQUIRED (no omitempty); Valid() rejects empty
session_id: <String>               # optional
nonce: <String>                    # optional — confirmed nonce marker (audit)
recovered: <Bool>                  # optional — true iff accepted via freshness recovery (not nonce)
handoff_mtime: <String>            # optional — RFC3339; carried on the recovery edge
```

Go struct `core.SessionKeeperHandoffWrittenPayload` (`internal/core/keeperevents.go`). `Valid()` requires non-empty `agent_name` and `cycle_id`. Emitted per [session-keeper.md §4.4 SK-012]; schema v1, `PayloadCompatEntry{CurrentVersion:1, PreviousVersion:0, CompatWindowHolds:true, AdditiveOnly:true}`.

#### `session_keeper_model_done` (§8.20.2)

```yaml
agent_name: <String>
cycle_id: <String>                 # REQUIRED (no omitempty); Valid() rejects empty
session_id: <String>               # optional
source: <enum: idle_marker | transcript_turn | timeout>   # REQUIRED — which signal fired
degraded: <Bool>                   # optional — true iff reached via model_done_timeout
```

Go struct `core.SessionKeeperModelDonePayload`. `Valid()` requires non-empty `agent_name` and `cycle_id`. Emitted per [session-keeper.md §4.5 SK-014]; schema v1, compat as above.

#### `session_keeper_clear_sent` (§8.20.3)

```yaml
agent_name: <String>
cycle_id: <String>                 # REQUIRED (no omitempty); Valid() rejects empty
session_id: <String>               # optional
attempt: <Integer>                 # 1-based; increments on defensive re-injects
```

Go struct `core.SessionKeeperClearSentPayload`. `Valid()` requires non-empty `agent_name`/`cycle_id` and `attempt >= 1`. Emitted per [session-keeper.md §4.4 SK-012]; schema v1, compat as above.

#### `session_keeper_new_session_up` (§8.20.4)

```yaml
agent_name: <String>
cycle_id: <String>                 # REQUIRED (no omitempty); Valid() rejects empty
prev_session_id: <String>          # REQUIRED — needed for the != check
new_session_id: <String>           # REQUIRED — Valid(): non-empty AND != prev_session_id
```

Go struct `core.SessionKeeperNewSessionUpPayload`. `Valid()` requires non-empty `agent_name`/`cycle_id`/`new_session_id` and `new_session_id != prev_session_id`. Emitted per [session-keeper.md §4.4 SK-012]; schema v1, compat as above. The `^cyc-` prefix on `cycle_id` is a soft check kept OUT of `Valid()` (a future `CycleIDGen` change MUST NOT retro-invalidate historical corpora); a non-conforming id is a low-severity harness finding, not a dropped event.

#### `agent_input_acked` (§8.21.1)

```yaml
run_id: <String>                   # REQUIRED (no omitempty); Valid() rejects empty
input_seq: <Integer>               # REQUIRED — monotonic submission sequence; the idempotency key
acceptance_token: <String>         # optional — the protocol turn id the driver acknowledged
session_id: <String>               # optional
acked_at: <String>                 # RFC3339 — wall-clock at the acceptance boundary
```

The event carries no acceptance "class": its existence IS the positive ack (COORD c019 — no capability hierarchy). A protocol-level refusal is the synchronous `Ack{Rejected}` return of `SubmitInput` (structured driver only, per [agent-input.md §6.2]), not an `agent_input_acked` variant; the never-confirmed case is the distinct `agent_input_stale` timeout terminal. Go struct `core.AgentInputAckedPayload` (`internal/core/agentinputevents.go`). `Valid()` requires non-empty `run_id` and `input_seq >= 0`. Emitted per [agent-input.md §4 AIS-004]; the driver→run seam is [handler-contract.md §4 HC-070]. Schema v1, `PayloadCompatEntry{CurrentVersion:1, PreviousVersion:0, CompatWindowHolds:true, AdditiveOnly:true}` (N-1 readable per [operator-nfr.md §4.5]). Idempotent on `(run_id, input_seq)`: a re-emitted ack for the same submission is a reactor no-op per §8.9(h)-adjacent granularity.

#### `agent_input_stale` (§8.21.2)

```yaml
run_id: <String>                   # REQUIRED (no omitempty); Valid() rejects empty
input_seq: <Integer>               # REQUIRED — the submission whose acceptance window elapsed
session_id: <String>               # optional
timed_out_at: <String>             # REQUIRED — RFC3339; wall-clock at window expiry
window: <String>                   # the InputAckTimeout + overhead bound that elapsed (e.g. "30s")
```

Go struct `core.AgentInputStalePayload`. `Valid()` requires non-empty `run_id`, `input_seq >= 0`, and non-empty `timed_out_at`. Emitted per [agent-input.md §4 AIS-004] when the InputAckTimeout window elapses with no `agent_input_acked` for the same `(run_id, input_seq)`; the driver→run seam is [handler-contract.md §4 HC-070]. Schema v1, `PayloadCompatEntry{CurrentVersion:1, PreviousVersion:0, CompatWindowHolds:true, AdditiveOnly:true}` (N-1 readable per [operator-nfr.md §4.5]). Non-idempotent (a distinct timeout boundary; not keyed for dedupe).

Remaining per-type payloads follow the same pattern: field names listed in §8 columns, Go types resolved against the registry per EV-032. Outstanding: full YAML for the remaining ~41 types lands within one revision cycle per OQ-EV-005.

### 6.4 Schema evolution — breaking-change table

The envelope carries a `schema_version` integer; each payload type carries a per-type version maintained by the registry. Per-type versions evolve independently, which means harmonik maintains N (currently 71) independent compatibility contracts. "N-1 readable" applies per-type.

| Change kind | Breaking? | Reader obligation |
|---|---|---|
| Add optional field | No | Accept; ignore unknown fields on older readers. |
| Add required field | Yes | Older readers fail closed with typed error on missing field. |
| Rename field | Yes | Migration release; no on-the-fly rewrite. |
| Remove field | Yes | Migration release. |
| Widen type (int32 → int64) | No | Accept widened values. |
| Narrow type (string → enum) | Yes | Migration release. |
| Add enum variant (non-required semantics) | No | Older readers see the new variant as `unknown`; handlers MUST treat unknown variants as non-fatal. |
| Remove enum variant | Yes | Migration release. |
| Tighten validation (e.g., required length bound) | Yes | Migration release. |

Migration releases are scheduled at operator pauses per [operator-nfr.md §4.3]. Between-run pause semantics means migration may require drain-to-quiescent; operators are advised to schedule migrations during low-activity windows. Imports [operator-nfr.md §4.5].

### 6.5 Co-owned event payloads

This spec owns the payload SHAPE for every type in §8. The WHEN of each emission is owned by the emitting subsystem:

- Run-lifecycle events (§8.1): emission rules in [execution-model.md §6.5]. The optional `workflow_mode` payload field on `run_started` / `run_completed` / `run_failed` is co-owned: this spec normatively declares its placement and enum values per §3 glossary `WorkflowMode`; [execution-model.md §4.3 EM-012a] declares how the resolved value is computed.
- Review-loop cycle events (§8.1a): emission rules in [execution-model.md §4.3 EM-015d, EM-015e]. All seven entries (`implementer_resumed`, `reviewer_launched`, `reviewer_verdict`, `iteration_cap_hit`, `no_progress_detected`, `review_fixup_stalled`, `review_loop_cycle_complete`) are orchestrator-core-emission-owned; this spec is normative for their payload shape, ordering rule, and durability class. `review_fixup_stalled` (§8.1a.7) replaces `no_progress_detected` on the post-REQUEST_CHANGES no-commit path in both review-loop and DOT modes (hk-m1wqp).
- Control-point events (§8.2): emission rules in [control-points.md §6.5]. The entries §8.2.10 `control_points_registration_started` (CP §7.1; companion to §8.2.9 `control_points_registered` — the pair brackets the registration batch per CP §7.1's crashed-mid-registration rule), §8.2.11 `verdict_envelope_mismatch` (CP §4.8.CP-041; envelope-hash mismatch on persisted-verdict replay), and §8.2.12 `policy_expression_exceeded_cost` (CP §4.7.CP-034b; cost-ceiling abort, durability pair) are CP-emission-owned. §8.2.13 `gate_definition_drift` (CP §4.7 CP-038a; mechanism-tagged Gate envelope drift detected at replay) and §8.2.14 `gate_redefined_under_cat_6` (CP §4.7 CP-038a; Cat 6 authorized re-evaluation under drifted definition) are orchestrator-core-emission-owned; emission WHEN is governed by [control-points.md §6.5 CP-038a]; this spec is normative for their payload shape and durability class (both class F).
- Agent / handler events (§8.3), including silent-hang FSM: emission rules in [handler-contract.md §4.1, §4.9, §4.11, §7.1].
- Budget events (§8.4): emission rules in [control-points.md §4.5].
- Workspace events (§8.5): emission rules in [workspace-model.md §4.4, §4.5].
- Reconciliation events (§8.6): emission rules in [reconciliation/spec.md §4.1, §4.3, §4.5]. The new entries §8.6.11 `reconciliation_dispatch_deduplicated` (RC-002a), §8.6.12 `reconciliation_detector_panic` (RC-020b), and §8.6.13 `reconciliation_verdict_execution_retry` (RC-026a) are RC-emission-owned. §8.6.14 `bead_terminal_transition_recovered` is **(post-MVH)** per OQ-BI-008 and reserved for future BI-adapter emission; no MVH emitter exists.
- Operator / daemon events (§8.7): emission rules in [operator-nfr.md §6.5, §7.3] and [process-lifecycle.md §6.2, §8.6]. The new entries §8.7.16 `operator_command_failed` (ON-013a) and §8.7.17 `operator_escalation_cleared` (ON; companion to RC-emitted `operator_escalation_required`) are ON-emission-owned.
- Observability events (§8.8): bus-internal (`consumer_failed`, `dead_letter_enqueued`, `bus_overflow`, `redaction_failed`) and free-call (`metric`). The new entry §8.8.5 `redaction_failed` (ON-022 fail-closed redactor) is bus-internal; the redactor MUST emit it before aborting the redaction-violating emission. The new entry §8.8.6 `bead_label_conflict` (per [execution-model.md §4.3 EM-012a]) is emitted by the daemon's claim path on tier-1 mode-resolution conflicts.
- Queue-lifecycle events (§8.10): emission rules in [queue-model.md §4 QM-010..012 (identity), §6 QM-020..026 (validation), §7 (append semantics), §8 (lifecycle), §3.2a QM-002a (startup reconciliation)]. All seven entries (`queue_submitted`, `queue_group_started`, `queue_group_completed`, `queue_paused`, `queue_appended`, `queue_item_deferred_for_ledger_dep`, `queue_item_reconciled`) are queue-emission-owned; this spec is normative for their payload shape, ordering rule, and durability class. The optional `queue_id` / `queue_group_index` payload fields on `run_started` / `run_completed` / `run_failed` are co-owned: this spec normatively declares their placement and typing per §6.3; [queue-model.md §8] declares when and how the dispatcher populates them.

## 7. Protocols and state machines

### 7.1 Emit-and-flush sequence

```
FUNCTION Emit(ctx, type, payload):
    wall = WALL_CLOCK_NOW()                     -- single read per EV-001
    event.event_id = NEW_MONOTONIC_UUIDV7(wall) -- EV-002, EV-002a
    event.timestamp_wall = wall
    event.timestamp_mono_nsec = MONO_CLOCK_NS()
    event.source_subsystem = REGISTERED_ID()    -- EV-034a
    INFER_SCOPE(ctx, event)                     -- fills run_id, state_id, trace_context from ctx
    redacted = REDACT(payload)                  -- EV-035
    event.payload = JSON_MARSHAL(redacted)
    APPEND_LINE(events.jsonl, event)            -- EV-015, EV-020
    IF DURABILITY_CLASS(type) == fsync-boundary:
        FSYNC(events.jsonl)                     -- EV-016
    -- synchronous dispatch on caller path (at-most-one per type)
    IF sync := bus.syncFor[type]:
        err = sync.Handle(ctx, event)
        IF err != nil:
            EMIT(consumer_failed, ...)
            QUARANTINE(run)                      -- EV-010
            RETURN err
    -- async and observer dispatch off the critical path
    ENQUEUE_ASYNC_NON_BLOCKING(event)            -- EV-011, EV-011a
    FANOUT_OBSERVERS(event)                      -- EV-012 (goroutine-per-observer)
    RETURN nil
```

### 7.2 Async dispatch, overflow, and dead-letter

```
FUNCTION EnqueueAsync(consumer, event):
    IF consumer.queue.full():
        IF DURABILITY_CLASS(event.type) == fsync-boundary:
            SPILL_TO_FILE(event, consumer.name)    -- EV-011a
        ELSE:
            DROP(event)
        EMIT(bus_overflow, {consumer.name, event.type, event.event_id, consumer.queue.depth, NOW()})
        RETURN
    consumer.queue.push(event)

FUNCTION DispatchAsync(consumer, event):
    FOR attempt IN 1..MAX_RETRIES:
        IF consumer.Handle(event) == nil: RETURN
        BACKOFF(attempt)  -- exponential, default 1s base
    ENQUEUE_DEAD_LETTER(events/dead-letters.jsonl, event, consumer.name, MAX_RETRIES)
    EMIT(dead_letter_enqueued, {consumer.name, event.type, event.event_id, MAX_RETRIES, NOW()})
```

### 7.3 Synchronous-consumer halt

```
FUNCTION DispatchSync(consumer, event, run):
    result = consumer.Handle(event)
    IF result != nil:
        EMIT(consumer_failed, ...)
        QUARANTINE(run)                          -- EV-010
        RETURN result
    RETURN nil
```

Branch points above correspond to normative requirements: fsync cadence (EV-016), redaction (EV-035), synchronous-halt (EV-010), async retry and dead-letter (EV-011), overflow and back-pressure (EV-011a), emission idempotency (EV-018).

## 9. Cross-references

### 9.1 Depends on

- **[architecture.md §4.1]** — four-axis classification; every event type's tags use the axes defined there.
- **[architecture.md §4.4, §4.5]** — subsystem envelope; `source_subsystem` is a registered Go package identifier.
- **[architecture.md §4.6]** — amendment protocol (EV-027).
- **[execution-model.md §4.1]** — `Run`, `State`, `Transition` types referenced by event scoping fields.
- **[execution-model.md §4.4]** — checkpoint contract; `checkpoint_written` references commit hash and trailers.
- **[execution-model.md §4.7]** — state-reconstruction source; §4.5 defers to it.
- **[execution-model.md §4.10 EM-044]** / **§6.1** — `TransitionKind` enum referenced by `transition_event` payload; see §3 glossary.
- **[execution-model.md §8]** — `FailureClass` enum referenced by `run_failed` payload.
- **[handler-contract.md §4.5]** — `ErrorCategory` sentinel-error set.
- **[handler-contract.md §4.7]** — redaction registry (EV-035 depends normatively).
- **[handler-contract.md §4.1, §4.9, §4.11, §7.1]** — handler-event emission rules (agent lifecycle + silent-hang FSM + skill injection).
- **[workspace-model.md §4.7]** — session-log pipeline; `session_log_location` emission rule.

### 9.2 Reverse dependencies

> INFORMATIVE: Reverse dependencies are computed on demand from the foundation corpus. Populated at finalize.

### 9.3 Co-references (read-only consumption)

- **[control-points.md §6.5]** — control-point event emission rules (gates, hooks, guards, budgets).
- **[reconciliation/spec.md §8 Category taxonomy]**; **[§4.3 Post-crash-window guardrail]**; **[§4.5 Verdict vocabulary]**.
- **[operator-nfr.md §6.5, §7.3, §7.5]** — operator-control emission timing + N-1 compatibility contract + migration-release pause semantics.
- **[operator-nfr.md §4.7 ON-022]** — fail-closed redactor; emitter of `redaction_failed` (§8.8.5).
- **[operator-nfr.md §4.4 ON-013a]** — operator-command panic supervision; emitter of `operator_command_failed` (§8.7.16).
- **[operator-nfr.md §4.8 ON-033]** — RTO measurement boundary; consumer of the `_at_ns_since_boot` companion fields on `daemon_ready` / `daemon_shutdown`.
- **[reconciliation/spec.md §4.1 RC-002a, §4.3 RC-020b, §4.5 RC-026a]** — emission rules for `reconciliation_dispatch_deduplicated` (§8.6.11), `reconciliation_detector_panic` (§8.6.12), `reconciliation_verdict_execution_retry` (§8.6.13).
- **[reconciliation/spec.md §4.2 RC-012a]** — emitter of `daemon_degraded{reason=cat0_post_ready}` (§8.7.5).
- **[beads-integration.md §4.6]** — optional `bead_id` field propagation across run-lifecycle events. `bead_claimed` / `bead_closed` / `bead_reopened` are NOT declared as dedicated events; bead terminal transitions ride on `run_started` / `run_completed` / `run_failed` with `bead_id` per BI-010/BI-011.
- **[beads-integration.md §4.8b BL-MRG-003]** — semantic conflict logging; reconciliation-investigator reads `.beads/merge-conflicts.log` to surface Cat-BL3 audit items; owns the WHEN of `bead_ledger_conflict_audit` (§8.15.2) emission.
- **[beads-integration.md §4.8b BL-MRG-004]** — post-merge SQLite refresh; failure routes to Cat-BL2 and owns the WHEN of `bead_sync_failed` (§8.15.1) emission.
- **[beads-integration.md §4.10, OQ-BI-008]** — post-MVH reservation slot for `bead_terminal_transition_recovered` (§8.6.14) and post-MVH `divergence_kind` adapter-specific values.
- **[process-lifecycle.md §6.2, §8.2]** — `daemon_ready` / `daemon_started` emission timing; RTO measurement endpoint.
- **[execution-model.md §4.3 EM-012a, EM-015d, EM-015e]** — workflow-mode resolution precedence and review-loop cycle lifecycle that drive the §8.1a review-loop events and the `workflow_mode` payload field on §8.1 run-lifecycle events.
- **[execution-model.md §6.1 WorkflowMode]** — enum referenced by the `workflow_mode` payload field per §3 glossary.

## 10. Conformance

### 10.1 Conformance profiles

**Core MVH.** An implementation conforming to Core MVH MUST:

- Implement every requirement in EV-001 through EV-036 (including EV-002a, EV-011a, EV-014a, EV-014b, EV-019a, EV-034a).
- Implement every invariant EV-INV-001 through EV-INV-006.
- Declare and register every event type in §8 with payload fields named in the §8 tables and durability classes per EV-016. This includes the six §8.1a review-loop event types (`implementer_resumed`, `reviewer_launched`, `reviewer_verdict`, `iteration_cap_hit`, `no_progress_detected`, `review_loop_cycle_complete`) and the §8.8.6 `bead_label_conflict` event type added by the workflow-modes integration; together these seven new cross-bus event types are the EV-027 amendment scope for the workflow-modes kerf.

**Post-MVH extensions.** JSONL rotation policy (OQ-EV-001), quality-checks.md cross-reference (OQ-EV-002), testing.md migration (OQ-EV-003), and per-payload version field (OQ-EV-004) are deferred.

### 10.2 Test-surface obligations

During bootstrap (before `testing.md` exists) test obligations are named in prose:

- **EV-001 — EV-008 (envelope and ordering).** Schema-conformance tests; UUIDv7 shape; monotonic non-decrease within a process; monotonic-counter tiebreaker under same-ms emission load.
- **EV-009 — EV-014b (bus and consumer taxonomy).** Registration tests: two synchronous consumers for the same type fail startup; observer default class; synchronous-halt via fault injection; reentrant synchronous subscription fails startup (EV-010 acyclicity); post-seal `Subscribe` fails; overflow under bounded queue exhibits shed + `bus_overflow` per EV-011a.
- **EV-015 — EV-020 (durability).** Fsync tests per durability class; kill between boundary events and between an ordinary event and its post-flush; append-only crash-replay.
- **EV-021 — EV-024 (replay semantics).** Restart rebuilds state from git plus Beads with JSONL unavailable; divergence-evidence read flags `post_crash_window: true` correctly in the lossy-tail window and corroborates against git before flagging.
- **EV-025 — EV-027 (producer/consumer contract).** Every emitter subsystem's emissions are a subset of §8; internal events do not cross the bus; amendment requires the three artifacts of EV-027.
- **EV-028 — EV-030 (schema versioning).** N-1 reader test per §6.4 rows; breaking-change detection surfaces typed errors.
- **EV-031 (tagging).** Static check: every entry in §8 carries durability class and the registry-side four-axis tags.
- **EV-032 — EV-034a (Go representation).** Registry tests: post-init registration fails; unknown `Event.type` surfaces `ErrUnknownEventType`; duplicate `source_subsystem` registrations fail startup.
- **EV-035 — EV-036 (redaction).** Secret-redaction tests.

**§8 lint obligation:** a lint rule MUST fail if any §8 row has no cited emission in a sibling spec (closes the orphan drift path). The `metric` row (§8.8.1) is exempt per §8.9(g).

Migration to `[testing.md §<layer>]` cross-references tracked at OQ-EV-003.

### 10.3 Excluded conformance claims

This spec does NOT grant conformance over: structured-log format (owned by `quality-checks.md`), JSONL rotation (OQ-EV-001), per-subsystem event-authorship rules, reconciliation category classifier, checkpoint trailer format, handler wire protocol. Bus latency / throughput bounds are operator-observable in [operator-nfr.md §4.8].

#### CLI help text is canonical alongside this spec; stale text MUST be corrected on spec adoption

`harmonik subscribe --since-event-id` is IMPLEMENTED as of 2026-05-27 (hk-a5sil, sha 994c6d2); the CLI help at `cmd/harmonik/subscribe.go:200` previously read "NOT YET IMPLEMENTED — daemon rejects; hk-a5sil", which was STALE. This spec is normative for the behavior contract; the CLI help is the operator-facing surface for the same contract. The two MUST agree. As part of adopting this revision, `cmd/harmonik/subscribe.go:14` and `:200` MUST be updated to remove the "NOT YET IMPLEMENTED" annotation and describe the replay semantics (e.g. "Resume cursor: replay events strictly after this event_id before delivering live stream"). This note closes the obligation; the corrections were applied in the same commit as this spec revision.

## 11. Open questions

#### OQ-EV-001 — JSONL rotation policy

Question: Should `events.jsonl` rotate by size, age, or remain unbounded for MVH?
Owner: foundation-author
Default-if-unresolved: Unbounded append for MVH; revisit before production deployment.

#### OQ-EV-002 — Structured-log / events boundary cross-reference

Question: §2.2 excludes structured-log format and names `quality-checks.md` as owner; that spec does not yet exist.
Owner: foundation-author
Default-if-unresolved: Prose reference; migrate after `quality-checks.md` finalizes.

#### OQ-EV-003 — Migrate test-obligation prose to testing.md references

Question: §10.2 names obligations in prose.
Owner: foundation-author
Default-if-unresolved: Keep prose obligations; migrate within one revision cycle after `testing.md`.

#### OQ-EV-004 — Explicit per-type `schema_version` field on payloads

Question: EV-028 says envelope-level `schema_version` MAY diverge from per-type versions maintained in the registry. Should payloads carry their own field?
Owner: foundation-author
Default-if-unresolved: No per-payload version field for MVH. **Note:** downgrade is lossy — a v2 producer's JSONL consumed by a v1 binary cannot distinguish payload versions from the wire alone. Acceptable for MVH (single-daemon, operator-gated upgrade); revisit when external tooling needs Go-registry-free consumption.

#### OQ-EV-005 — Complete §6.3 payload-schema coverage

Question: §6.3 declares concrete YAML for ~14 of 71 event types; remaining types land per §8 field lists plus the Go registry.
Owner: foundation-author
Default-if-unresolved: Declare full §6.3 YAML for every §8 type within one revision cycle; until then, §8 tables + registry are normative.

#### OQ-EV-006 — Operator-state event consolidation

Question: §8.7 carries 15 events, several of which (`operator_pause_status`, `operator_resuming`, `operator_stopped`, `operator_upgrading`, `operator_upgrade_completed`, `operator_upgrade_rejected`, `operator_command_rejected`, `dispatch_deferred`, `daemon_orphan_sweep_completed`) are close to "the daemon is in some state now" signals. A unified `operator_state_changed{from_state, to_state, detail}` could fold 4–5 of these into one if the consumer-branching cost of the merge is acceptable. Deferred to post-MVH so current subscribers can be audited for actual branch-shape needs before collapsing.
Owner: foundation-author
Default-if-unresolved: §8.7 shape remains granular for MVH; revisit after first implementer pass once subscriber branch-shape is measurable.

#### OQ-EV-007 — Consumer-goroutine panic policy — machine-checkable enforcement

Question: Subscription.on_panic (§6.1) offers three options (`recover_and_log` / `quarantine_consumer` / `fail_daemon`) but the bus-internal enforcement is unwritten. Default is `recover_and_log`: the bus recovers the panic, emits `consumer_failed` (§8.8.2) with `error_category=panic`, and continues dispatching to other consumers. `quarantine_consumer` additionally suspends dispatch to the panicking consumer for the rest of the daemon cycle. `fail_daemon` escalates to `daemon_startup_failed` (inappropriate for MVH default). Deferred: testing.md enforcement and the `quarantine_consumer`/`fail_daemon` semantics.
Owner: foundation-author
Default-if-unresolved: Implement `recover_and_log` for MVH; `quarantine_consumer` and `fail_daemon` are declared in the Subscription record but implemented post-testing.md.

## 12. Revision history

| Date | Version | Author | Summary |
|---|---|---|---|
| 2026-07-14 | 0.7.1 | agent (codename:agent-input-substrate) | **§8.21 Agent-input acceptance events — two O-class cross-bus signals for the M2 structured input driver.** Adds a new §8.21 taxonomy cohort registering the M2 structured input driver's submission-sub-lifecycle signals that async observers consume (the M3 runexec run-reactor, the `internal/replay` invariant harness (EV-048), audit, observability). **Taxonomy additions (2 new §8.21 rows):** §8.21.1 `agent_input_acked` (O; emitter `daemon-core` input driver; payload `run_id` REQUIRED, `input_seq` REQUIRED monotonic, `acceptance_token?`, `session_id?`, `acked_at`; no acceptance "class" per COORD c019 — its existence IS the positive ack; idempotent on `(run_id, input_seq)`), §8.21.2 `agent_input_stale` (O; same emitter; payload `run_id` REQUIRED, `input_seq` REQUIRED, `session_id?`, `timed_out_at` REQUIRED, `window`; emitted when the InputAckTimeout window elapses with no ack). Both class O — observational; loss does not gate the run, whose acceptance state is re-derivable from the retained input journal. **Recorded §8.9(b) input-acceptance-boundary exception** argued inline (the ack IS the acceptance boundary of the submission sub-lifecycle; consumers named — the M3 reactor + replay harness; cross-process so NOT EV-026-internal; EV-027 registration mandatory). **Cohort-guard carve-out (§8.21):** per EV-050 both are registered (each a `mustRegister` constructor + a mandatory `pertypecompat` / `PayloadCompatEntry` row) and compat-tabled but excluded from `allEventTypeCohort` and the EV-027 count guard, following the §8.16/§8.20 precedent. **§6.3** gains the two payload schemas (`core.AgentInputAckedPayload`, `core.AgentInputStalePayload`; schema v1, N-1 readable per [operator-nfr.md §4.5]). **§8.21 Section Axes note** added (mechanism-tagged, class O, `llm-freedom=none; io-determinism=best-effort; replay-safety=safe`; `agent_input_acked` idempotent on `input_seq`). Emission-timing/ordering owned by [agent-input.md §4 AIS-004]; driver→run seam owned by [handler-contract.md §4 HC-070]. No EV requirement IDs added/renumbered; no prior §8 rows renumbered or retired. Source: kerf agent-input-substrate change design (M2). |
| 2026-07-13 | 0.7.0 | agent (codename:session-restart-substrate) | **Session-keeper interior events + §8 drift reconciliation.** §8-numbering reconciliation (EV-U5): added real spec sections **§8.16** (the 18 existing `session_keeper_*` watcher/lifecycle types, renumbered out of the §8.13 code-comment collision), **§8.17** (alarm / self-check — `review_gate_anomaly`), **§8.18** (remote-substrate workers — `worker_unhealthy`/`worker_offline`/`worker_tunnel_failed`/`worker_report`/`resource_breach`), **§8.19** (flywheel governor / liveness-halt / stall-sentinel — `governor_signal`/`liveness_halt`/`stall_detected`); §8.13/§8.14/§8.15 unmoved (only the code-comment citations are corrected). New cohort **§8.20 Session-keeper interior cycle events** — four O-class types (`session_keeper_handoff_written`, `session_keeper_model_done`, `session_keeper_clear_sent`, `session_keeper_new_session_up`), each joinable by a REQUIRED payload `cycle_id`, with the recorded §8.9(b) cycle-interior exception and the D9 O-class + emit-failure-logging durability note; **§6.3** gains the four payload schemas. Five new requirements **EV-046** (cycle-scoped keeper events join on required payload `cycle_id`, composite `(agent_name, cycle_id)`, never a zero envelope `run_id`), **EV-047** (`ScanAfter` declared the normative offline-replay read surface + cross-writer EventID-sort caveat), **EV-048** (typed-decode registry adopted as the decode/assert layer for the `internal/replay` observational consumer), **EV-049** (additive `DecodePayloadStrict` variant surfacing additive writer drift), **EV-050** (the `session_keeper_*` §8.16+§8.20 cohorts carved out of `allEventTypeCohort` / the EV-027 count guard; gjyks doc-comment contract amended). Highest prior ID EV-045; next-free EV-046. No prior IDs renumbered or retired; no prior §8 row renumbered. Source: kerf session-restart-substrate change design (D6/D7/D8/D9; 00b R1+R2). |
| 2026-06-01 | 0.5.5 | agent (hk-sx9r.68) | **ON-049 coordination: attribution 5-tuple fields added to §8.4 budget events.** Added the ON-049 attribution fields to §8.4 budget lifecycle events as additive optional fields per §6.4: (1) `budget_accrual` (§8.4.2) gains `role`, `node_id`, `category`, `amount`, and `delegation_path?` — these are the on-wire realization of the ON-049 5-tuple `(run_id, role, node_id, category, amount)` and the cognition-tagged `delegation_path` supplement per [operator-nfr.md §4.11 ON-049]; (2) `budget_warning` (§8.4.1) gains `role`, `node_id`, `category` to support per-role and per-node threshold observability; (3) `budget_exhausted` (§8.4.3) gains `role?`, `node_id?`, `category?` as optional — absent for the account-scoped (run-agnostic) variant per the existing INFORMATIVE note. Added NORMATIVE note to §8.4 section header explaining the attribution-field semantics and the additive-field rationale. `delegation_path?` is REQUIRED on `budget_accrual` events for cognition-tagged evaluator steps (ControlPoint with cognition tag per [control-points.md §4.8 CP-039]) and absent otherwise. No EV requirement IDs added or renumbered; all additions are governed by the §6.4 additive-field rule and the §4.6 amendment protocol. Source: [operator-nfr.md §4.11 ON-049] (hk-sx9r.68). |
| 2026-05-31 | 0.5.4 | agent (hk-sx9r.30) | **ON-025 egress/workspace-escape audit: `rejected_skills[]?` added to §8.3.8 `skills_provisioned`.** Updated §8.3.8 payload to add optional `rejected_skills[]` (each: `name`, `reject_reason`). Added INFORMATIVE note: `skills[]` lists only successfully installed skills; `rejected_skills[]` lists skills rejected by pre-provisioning policy checks (egress-policy or workspace-escape per [handler-contract.md §4.11.HC-048b]); absent when no rejections occurred; `reject_reason` strings are `"egress_domain_not_whitelisted"` and `"path_escapes_workspace"`. Additive optional-field extension; N-1 readers tolerate unknown fields per §6.4. No EV requirement IDs added or renumbered. |
| 2026-05-31 | 0.x | agent (kerf `credfence` work) | Additive producer-set amendment for `budget_exhausted` (§8.4.3). Added `cognition-loop (flywheel)` to the producer set so the unified per-day spend meter ([cognition-loop.md CL-090]) emits the account-scoped `budget_exhausted{budget_scope=handler_account}` that the budget-exhaustion handler-pause policy ([handler-pause.md HP-012]) consumes; added the optional `budget_scope`, `spent_usd`, `cap_usd` payload fields and made `run_id`/`session_id`/`attempted_dispatch_cost` optional for the account-scoped (run-agnostic) variant; added an INFORMATIVE producer-set note distinguishing the per-run (S04) variant from the account-scoped (cognition-loop) variant. Event class O and mechanism tag unchanged. Governed by the §6.4 additive-field rule and the §4.6 amendment protocol. Source: kerf `credfence` change design. |
| 2026-04-23 | 0.1.0 | foundation-author | Initial draft. 54 events; fsync on 4 hardcoded types; paired-phase events split; `gate_evaluated` single-event; envelope-plus-registry Go shape. |
| 2026-04-24 | 0.2.0 | foundation-author | Round-1 reviewer integration. Event count 54 → 70 (net +16): added 22 missing cross-subsystem emissions (daemon lifecycle, upgrade contract, split gate verdicts, silent-hang FSM family, hook/guard events, `node_dispatch_requested`, `control_points_registered`, `bus_overflow`); merged 3 paired-phase pairs into status-carrying singles; retired 3 orphans (`gate_evaluated` replaced by split, `guard_denied` replaced by `guard_reordered`+`guard_failed`, `policy_violation` and `health_check` removed). Added EV-002a (monotonic UUIDv7), EV-011a (non-blocking back-pressure), EV-014a (dispatch semantics), EV-014b (consumer idempotency), EV-019a (panic bus flush as SHOULD), EV-034a (`source_subsystem` registration). Refactored EV-016 to derive fsync from durability class declared in §8. Added §6.4 breaking-change classification table. Added EV-023 post-crash-window guardrail. Added `TransitionKind`, `FailureClass`, `ErrorCategory`, `session_id` typing to §3. Added §8.9(g) orphan-lint criterion and §8.9(h) paired-event prohibition. Added EV-010 acyclicity clause and `depguard` rule for observers (EV-012). MUST/SHOULD split on EV-019/EV-019a; EV-017 rationale moved to §A.3; EV-011 quota bounded. Status remains `draft`. |
| 2026-04-24 | 0.3.0 | foundation-author | Round-2 reviewer integration. Blocking fixes: taxonomy count reconciled (54 → 70, not 69); added EV-002b (handler subprocesses route event_id generation through daemon); added EV-002c (UUIDv7 high-water-mark file for restart monotonicity); added `Subscription` RECORD to §6.1 with `consumer_id`, `consumer_class`, `event_pattern`, `since`, `on_panic`, `offset_checkpoint_event_id`; added `replay_from(since)` and `on_tail_truncation` consumer-recovery hooks to bus interface (closes EV-INV-002 consumer side); clarified FANOUT_OBSERVERS concurrency (per-observer goroutine + bounded queue); added `shed_policy` field to `bus_overflow` payload. Crash findings: added EV-023a (evidence-inconclusive clause for non-corroborable events); new `divergence_inconclusive` event (§8.6.10); extended §6.2 read-recovery with torn-tail / mid-file / empty-log / concurrent-tail rules; added `bus_overflow` reserved-slot requirement to EV-011a; added EV-016a multi-event atomicity disclaimer. Should-apply: §8.9(h) amended with emit-on-transition-only clause; EV-027 amended with add/remove symmetry. Deferred: OQ-EV-006 operator-state consolidation; OQ-EV-007 consumer panic policy. Status: draft → reviewed. |
| 2026-04-24 | 0.3.1 | foundation-author | Corpus-wide cleanup pass (no semantic changes). Migrated legacy architecture.md citation anchors to the §4.N map per the v0.2 NOTE: §1.1→§4.1 (×2 in §9 cross-refs and §3.2), §1.4→§4.4 (×1 in §3.8), §1.4a→§4.5 (×2 in §3.2 envelope and §9 cross-refs), §1.5→§4.6 (×2 in §6.5 amendment clause and §9 cross-refs). Completed AR-MIG-001 `handler_type` → `agent_type` rename at §8.3.2 (`agent_started` payload) and §8.3.7 (`session_log_location` payload). No requirement IDs, invariants, or schemas were touched. |
| 2026-04-24 | 0.3.2 | foundation-author | Corpus citation-drift cleanup pass 2: migrated legacy §N.N cross-spec anchors to current template §N.N form per the central remap table; 12 citations fixed. WM: `§5.3→§4.7` (session-log pipeline) at §3 scope, §4.1 EV-005, §9.3 cross-refs; `§5.2→§4.4`, `§5.4→§4.5` at §9.3 emission-rule references. Reconciliation path fix: `[reconciliation.md §9.N]` → `[reconciliation/spec.md §N]` (multi-file spec) at §2.2 scope, §8.5.5 `workspace_interrupted` emitter reference, §8.6.3 verdict payload reference, §4.5 EV-023a cross-ref, §6.2 mid-file corruption Cat 6, §9.3 reconciliation emission rules. ON: `§7.3→§4.3` (operator pause), `§7.5→§4.5` (N-1 compat window), `§7.8→§4.8` (bus latency) at §4.7, §6.4, §10.3. BI: `§10.6→§4.6` (bead_id propagation) at §9.3, §A.3. No requirement IDs, invariants, or schemas touched. |
| 2026-05-30 | 0.6.0 | agent (flywheel spec-bundle hk-j7o3i) | **Flywheel additions: §8.3.14 `lifecycle_transition`; §8.12 `decision_required`/`decision_acknowledged` (EV-042..044); §4.11 `harmonik subscribe` consumer contract (EV-037..EV-041); §6.3 payload schemas; §10.3 CLI-help correction.** §8.3.14: `lifecycle_transition` (class O; watcher-emitted per HC-064..067 FSM; payload: `from_state`, `to_state`, `reason`, `transitioned_at`; §8.3 Section Axes note updated). §8.12: two new decision-lifecycle event types — `decision_required` (class F; 4 canonical emission conditions; idempotency-keyed on `triggering_event_id`; dispatch-blocking per EV-043; ACK surface operator-CLI or cognition-loop note) and `decision_acknowledged` (class F; carries `ack_method ∈ {operator, note}`); NOT a paired-phase per §8.9(h); §8.12 Section Axes note added. §4.11 (EV-037..EV-041): out-of-process subscribe consumer contract — watermark persistence (EV-037/EV-037a), `subscription_gap` forced-resync (EV-038), heartbeat `last_event_id`+`active_runs` usage (EV-039), missing-heartbeat reconnect-with-backoff (EV-040), git-done-but-no-event heuristic (EV-041). §4.12 (EV-042..EV-044): `decision_required` dispatch-blocking requirements — emission obligations (EV-042), dispatch-blocking check at startup+workloop (EV-043/EV-043a), digest exception (EV-044). §6.3: `decision_required` and `decision_acknowledged` payload schemas added after `queue_item_reconciled`. §10.3: CLI-help-is-canonical note — `harmonik subscribe --since-event-id` stale annotation removed from `cmd/harmonik/subscribe.go:14` and `:200`. `daemon_degraded` reason enum: `decision_ack_timeout` variant flagged as requiring separate EV-027 amendment. New IDs: EV-037, EV-037a, EV-038, EV-039, EV-040, EV-041, EV-042, EV-043, EV-043a, EV-044. New event types: `decision_required` (§8.12.1, class F), `decision_acknowledged` (§8.12.2, class F), `lifecycle_transition` (§8.3.14, class O). No prior IDs renumbered or retired. |
| 2026-06-10 | 0.6.1 | agent (hk-m1wqp) | **§8.1a.7 `review_fixup_stalled` — distinct failure class for REQUEST_CHANGES fix-up stalls.** Adds a new event type emitted when a REQUEST_CHANGES fix-up run advances HEAD by zero commits, replacing the generic `no_progress_detected` on that specific path in both review-loop and DOT modes. **Taxonomy addition (1 new §8.1a row):** §8.1a.7 `review_fixup_stalled` (O; carries `reviewer_flags []string` from the prior REQUEST_CHANGES verdict; emitted by orchestrator-core in both `workflow_mode = "review-loop"` and `workflow_mode = "dot"` when `priorVerdict == REQUEST_CHANGES` and HEAD is unchanged at iter ≥ 2). **§8.1a table updated:** §8.1a.7 row added; §8.1a.6 `completion_reason` enum extended with `fixup_stalled` variant. **§8.1a intro paragraph updated:** "Five of the six" → "Five of the seven"; seventh event described. **§8.1a.5 normative note updated:** DOT mode now emits `review_fixup_stalled` (not `no_progress_detected`) when prior verdict was REQUEST_CHANGES; `no_progress_detected` retained for non-REQUEST_CHANGES path. **§8.1a emission ordering rule updated:** step (c) now branches: REQUEST_CHANGES prior → `review_fixup_stalled + review_loop_cycle_complete{fixup_stalled}`; otherwise → `no_progress_detected + review_loop_cycle_complete{no_progress}`; DOT-mode note updated accordingly. **§6.3 payload schema added** for `review_fixup_stalled`. **`review_loop_cycle_complete` payload schema updated:** `completion_reason` enum now includes `fixup_stalled`. **§6.5 co-ownership map updated:** "All six entries" → "All seven entries"; `review_fixup_stalled` enumerated. Bead ref: hk-m1wqp. No prior event-type identifiers renumbered or retired; no EV requirement IDs added/renamed/retired. Status remains `reviewed`. |
| 2026-05-06 | 0.3.4 | foundation-author | Coordination patch landing 3 CP-emitted events that CP §6.5 / §7.1 / §4.7.CP-034b / §4.8.CP-041 declare but EV §8 was missing. **Taxonomy additions (3 new §8.2 control-point-lifecycle rows, no renumbering of pre-existing entries):** §8.2.10 `control_points_registration_started` (companion to existing §8.2.9 `control_points_registered`; the pair brackets the CP §7.1 registration batch — absence of the trailing event paired with a prior registration_started of the same `batch_id` signals a crashed-mid-registration batch); §8.2.11 `verdict_envelope_mismatch` (CP §4.8.CP-041 envelope-hash mismatch on persisted-verdict replay; reconciliation Cat 6 input); §8.2.12 `policy_expression_exceeded_cost` (CP §4.7.CP-034b cost-ceiling abort; durability class `F` because the abort and the event are a durability pair — the event MUST reach JSONL durability before the evaluator wrapper returns control). **§6.3 payload schema added** for `policy_expression_exceeded_cost` declaring `bound_fired ∈ {ast_steps, wall_clock}` discriminator and per-abort `io_determinism ∈ {deterministic, best-effort}` tag (load-bearing per CP-034b; re-adding post-MVH would be breaking). **§6.5 co-ownership map** updated to enumerate the 3 new CP-emission-owned entries. Mirrors the CP v0.2.0 changelog's "added to §6.5" note that never landed in EV. No EV requirement IDs added/renamed/retired; no pre-existing §8 entries renumbered; status remains `reviewed`. |
| 2026-04-25 | 0.3.3 | foundation-author | Coordination patch wave landing R2 cross-spec items filed against EV by ON, RC, BI overnight 2026-04-24. **Taxonomy additions (7 new event-type IDs in gaps; no renumbering of pre-existing entries):** §8.6.11 `reconciliation_dispatch_deduplicated` (RC-002a `flock(LOCK_EX|LOCK_NB)` second-dispatch dedup); §8.6.12 `reconciliation_detector_panic` (RC-020b per-detector `recover()` barrier); §8.6.13 `reconciliation_verdict_execution_retry` (RC-026a Cat 3b retry cap N=5); §8.6.14 `bead_terminal_transition_recovered` **(post-MVH)** reserved per OQ-BI-008 with explicit "no MVH emitter; structured-log via ON-035 at MVH" annotation block; §8.7.16 `operator_command_failed` (ON-013a panic-barrier emission carrying `command` + `failure_class` + optional `run_id`); §8.7.17 `operator_escalation_cleared` (ON companion to RC-emitted `operator_escalation_required`, carrying `clearance_reason` enum); §8.8.5 `redaction_failed` (ON-022 fail-closed redactor, bus-internal). **Daemon-shutdown durability confirmed F (resolves OQ-PL-012 — recorded here; OQ lives in PL).** §8.7.3 `daemon_shutdown` row already carried `F`; the durability-class statement is now load-bearing as the prior-cycle SIGTERM-receipt landmark for ON-033 RTO reconstruction. **Monotonic companion fields on §8.7.2/§8.7.3:** added `ready_at_ns_since_boot` (uint64) and `shutdown_at_ns_since_boot` (uint64) per ON-033, with concrete §6.3 payload schemas declaring both fields REQUIRED and explicitly noting boot-transition / SIGKILL `rto_undefined` carve-outs. **`daemon_degraded` reason enum promoted from informative (`/ other`) to exhaustive** with 6 values: `rto_breach`, `reconstruction_notify`, `clock_regression` (EV-002c), `cat0_post_ready` (RC-012a carve-out), `infrastructure_unavailable`, `silent_hang_aggregate` (ON-040 aggregator); concrete §6.3 payload added; §8.7.5 row updated; future variants require an EV-027 amendment. **`divergence_kind` post-MVH extension note** added under the §6.3 `store_divergence_detected` block: the MVH enum stays closed; adapter-specific values are reserved for a future revision per OQ-BI-008; no concrete adapter-specific values added in this revision. **§6.5 co-ownership map** updated to enumerate the 6 new MVH-active emitters (RC: §8.6.11–13; ON: §8.7.16–17 + bus-internal §8.8.5) and to mark §8.6.14 explicitly post-MVH. **§9.3 cross-references** added: ON-022 (`redaction_failed`), ON-013a (`operator_command_failed`), ON-033 (RTO consumer of monotonic fields), RC-002a / RC-020b / RC-026a / RC-012a, BI §4.10 + OQ-BI-008. No EV requirement IDs added/renamed/retired; no §8 entries renumbered; status remains `reviewed`. |

| 2026-05-15 | 0.5.2 | agent (imrest/hk-iuaed.5) | **§8.7.14 `daemon_orphan_sweep_completed` payload field catchup (EV additive-tolerance confirmation).** The §8.7.14 taxonomy row was stale: it listed only the original four fields (`tmux_sessions_killed`, `locks_cleared`, `subprocesses_killed`, `swept_at`) and was missing all additive fields declared by subsequent PL amendments. EV determination: all additions are **non-breaking** per §6.4 row 1 ("Add optional field → No → Accept; ignore unknown fields on older readers"); no per-type schema version bump is required; EV-029 N-1 compatibility applies. Updated §8.7.14 to enumerate the full additive field set with source annotations: `tmux_windows_killed` and `tmux_kill_window_survivors []int` (PL-021c); `br_subprocesses_killed`, `reconciliation_locks_removed`, `stale_intents_observed` (PL-006 BI-v0.4.1/R2); `bead_in_progress_reset` (PL-006 sixth bullet / hk-iuaed.2). The hk-iuaed.5 contingency bead (filed as a companion to hk-iuaed.2 in case a schema bump was required) is now closed: additive-tolerated, no bump needed. No EV requirement IDs added, renamed, or retired; no §8 entries renumbered. Status remains `reviewed`. |
| 2026-05-15 | 0.5.1 | foundation-author | Gap-closure pass for extqueue v0.1 (hk-089gr). **Taxonomy addition (1 new §8.10 row):** §8.10.7 `queue_item_reconciled` (F; startup cross-check correction per [queue-model.md §3.2a QM-002a]; emitted when a `dispatched` item is found `open` in Beads at startup and reverted to `pending`; reason enum: `claim_write_lost`). **§6.3 payload schema** added for `queue_item_reconciled` (fields: `queue_id`, `group_index`, `bead_id`, `reason`, `reconciled_at`). **§8.10 Section Axes updated** to include `queue_item_reconciled` in the Class F list. **§6.5 co-ownership map updated** to enumerate 7 queue-emission-owned entries (was 6). No other EV requirement IDs added, renamed, or retired; no §8 entries renumbered. Status remains `reviewed`. |
| 2026-05-14 | 0.5.0 | foundation-author | External-queue kerf integration (`extqueue`). **Taxonomy additions (6 new cross-bus event types in new §8.10 Queue-lifecycle cohort):** §8.10.1 `queue_submitted` (F; loss orphans the execution plan), §8.10.2 `queue_group_started` (O; reconstructible from the predecessor group's `queue_group_completed` plus queue.json), §8.10.3 `queue_group_completed` (F; group-boundary advance landmark), §8.10.4 `queue_paused` (F; hard execution stop, `reason ∈ {group_failure, operator_drain}`), §8.10.5 `queue_appended` (O; stream-group mutation observability), §8.10.6 `queue_item_deferred_for_ledger_dep` (O; informational submit/dispatch-time deferrals). All six additions are queue-emission-owned (`source_subsystem = github.com/harmonik/internal/queue`, registered per EV-034a). The six additions are the EV-027 foundation-amendment scope for the external-queue kerf. **Dropped candidates** (per EV-016a tandem-emission rule and §8.9(c) granularity criterion): `queue_item_dispatched` (reconstructible from `run_started{queue_id, queue_group_index}`); `queue_item_completed` / `queue_item_failed` (tandem with `run_completed` / `run_failed` per EV-016a; the F-class run event is the durable landmark). `queue_resumed` is reserved for v0.2 alongside the `queue-resume` operation. **§6.3 payload-schema additions on existing types:** OPTIONAL additive fields `queue_id: <String> | null` (UUIDv7 as string) and `queue_group_index: <Integer> | null` added to `run_started`, `run_completed`, `run_failed` payloads per §6.4 row 1 (non-breaking; older readers ignore unknown fields per §6.4 reader obligation). Pattern mirrors the v0.4 `workflow_mode` precedent. **§6.3 payload schemas:** concrete YAML added for all six §8.10 entries; `queue_paused.reason` is an exhaustive enum at MVH. **§8.10 ordering rule** normatively pins the lifecycle emission order: `queue_submitted → queue_group_started{0} → per-item run_* chain → queue_group_completed → [queue_paused{group_failure}] OR queue_group_started{N+1}`; the terminal `run_completed`/`run_failed` for the last item of a group MUST precede `queue_group_completed` for that group. **§6.5 co-ownership map** updated to enumerate the six new queue-emission-owned entries plus the co-owned `queue_id` / `queue_group_index` fields on §8.1 run-lifecycle events. No prior event-type identifiers renumbered or retired; no EV requirement IDs added, renamed, or retired (the rule additions are §8.10 ordering and payload-field rules, not new top-level EV-NNN requirements). Status remains `reviewed`. |
| 2026-05-28 | 0.5.4 | attractor-parity/T7v2 | **§8.9 deferred-candidate note: `tool_command_completed` (hk-tksed).** No taxonomy addition. Documents the Part IV decision that per-tool-call lifecycle events are excluded from §8 at v1. Tool nodes ([workflow-graph.md §4 WG-039]) reuse `node_dispatch_requested` (§8.1.11) + `outcome_emitted` (§8.1.8) + `run_completed`/`run_failed` (§8.1) for observability; a bespoke `tool_command_completed` event fails §8.9(a) (no cross-subsystem consumer) and §8.9(b) (intra-lifecycle detail). The type name is reserved for future use pending §8.9 compliance; future addition requires the §4.6 amendment protocol. No EV requirement IDs added/renamed/retired; no §8 entries renumbered; status remains `reviewed`. |
| 2026-05-23 | 0.5.3 | phase-3-dot/C4 | Coordination patch landing 2 new CP-emitted events from C4 (control-points.md v0.4.0 CP-038a). **Taxonomy additions (2 new §8.2 control-point-lifecycle rows, no renumbering of pre-existing entries):** §8.2.13 `gate_definition_drift` (class F; mechanism-tagged Gate envelope drift at replay time per CP-038a; orchestrator-core-emission-owned); §8.2.14 `gate_redefined_under_cat_6` (class F; Cat 6 authorized re-evaluation under drifted Gate definition per CP-038a; orchestrator-core-emission-owned). **§6.3 payload schemas added** for both new types. **§8.2 Section Axes note updated** to enumerate §8.2.13 and §8.2.14 as class F exceptions. **§6.5 co-ownership map updated** to enumerate the 2 new orchestrator-core-emission-owned entries and their CP-038a emission-WHEN authority. No EV requirement IDs added/renamed/retired; no pre-existing §8 entries renumbered; status remains `reviewed`. |
| 2026-05-12 | 0.4.0 | foundation-author | Workflow-modes kerf integration (C3). **Taxonomy additions (7 new cross-bus event types):** new §8.1a Review-loop cycle subsection adds six event types — §8.1a.1 `implementer_resumed` (O), §8.1a.2 `reviewer_launched` (O), §8.1a.3 `reviewer_verdict` (F; promoted from class O because the verdict gates terminal routing of the run — loss would orphan a closed-or-needs-attention task), §8.1a.4 `iteration_cap_hit` (O), §8.1a.5 `no_progress_detected` (O), §8.1a.6 `review_loop_cycle_complete` (F). §8.8.6 adds `bead_label_conflict` (O), emitted by the daemon's claim path on tier-1 mode-resolution conflicts per [execution-model.md §4.3 EM-012a]. All seven additions are the EV-027 foundation-amendment scope for the workflow-modes kerf. **§8.1 `workflow_mode` payload-field rule:** new normative note declaring that `run_started` / `run_completed` / `run_failed` payloads carry an optional `workflow_mode ∈ {single, review-loop, dot}` field (additive, backward-compatible) and that every §8.1a review-loop event payload carries the field as REQUIRED. **§3 Glossary additions:** `WorkflowMode` (enum cross-referenced to [execution-model.md §6.1]) and `claude_session_id` (the Claude Code session identifier; explicitly disambiguated from harmonik's `session_id` event field which is the handler-minted UUIDv7). **§6.3 payload schemas:** concrete YAML added for all six §8.1a entries plus §8.8.6 `bead_label_conflict`. The §8.1a.3 `reviewer_verdict` payload reuses the `agent-reviewer` JSON schema v1 fields verbatim (`schema_version`, `verdict`, `flags`, `notes`); no parallel schema is introduced per the locked decision. The `prior_verdict_summary` field on `implementer_resumed` is derived by front-truncating the prior reviewer's `notes` at 256 UTF-8 bytes. **§8.1a ordering rule** normatively pins the emit order within an iteration (`implementer_resumed → reviewer_launched → reviewer_verdict`; or `no_progress_detected → review_loop_cycle_complete` early-exit; or terminal `review_loop_cycle_complete` before §8.1 `run_completed`/`run_failed`). **§6.5 co-ownership map** updated to enumerate the new orchestrator-core-emission-owned entries plus the bus-internal `bead_label_conflict`. **Conformance (§10.1)** expanded to include the seven new event types in Core MVH. No prior event-type identifiers renumbered or retired; no EV requirement IDs added, renamed, or retired (the rule additions are payload-field rules and ordering rules on §8.1 / §8.1a, not new top-level EV-NNN requirements). Status remains `reviewed`. |
| 2026-06-21 | 0.6.4 | agent (hk-bfrli) | **§8.15 Bead-ledger merge lifecycle — `bead_sync_failed` (F) + `bead_ledger_conflict_audit` (O).** Adds new §8.15 taxonomy cohort for the bead-ledger union-merge observability path (normative refs: [beads-integration.md §4.8b BL-MRG-003/BL-MRG-004]). **Taxonomy additions (2 new §8.15 rows):** §8.15.1 `bead_sync_failed` (F; daemon-core beads-adapter post-merge; payload `run_id`, `error`, `timestamp`; emitted when `br sync --import-only` fails per BL-MRG-004; must precede Cat-BL2 routing), §8.15.2 `bead_ledger_conflict_audit` (O; reconciliation-investigator; payload `run_id`, `bead_ids []string`, `conflicts []Object`, `timestamp`; emitted per Cat-BL3 audit read of `.beads/merge-conflicts.log` per BL-MRG-003). `bead_sync_failed` is class F because its loss silences the daemon's Cat-BL2 route-to-reconciliation obligation. `bead_ledger_conflict_audit` is class O because `.beads/merge-conflicts.log` is the authoritative source and the investigator can re-emit on recovery. **§9.3 co-references updated** to enumerate BL-MRG-003 and BL-MRG-004 as the WHEN-authority sources for the two new event types. NOT a paired-phase per §8.9(h). No EV requirement IDs added/renamed/retired; no pre-existing §8 entries renumbered. Bead ref: hk-bfrli. |
| 2026-06-21 | 0.6.3 | agent (hk-8ps7q) | **§8.1a.5 + §8.1a emission-ordering: APPROVED-AND-DONE exemption for DOT-mode no-progress detector.** The hk-8ps7q fix (code landed a0987bb6 + 856c500a) exempts DOT-mode runs from `no_progress_detected` when the prior reviewer verdict was APPROVE AND HEAD has advanced past the run baseline parentSHA (committed work exists). Without the spec update, §8.1a.5 implied no_progress always fires on HEAD-unchanged at iter≥2; the emission-ordering note's DOT-mode clause listed only two termination paths (REQUEST_CHANGES → review_fixup_stalled; other → no_progress_detected). **§8.1a.5 normative note updated:** APPROVED-AND-DONE exemption documented — gated on `!prevAgenticNodeWasReviewer` to preserve multi-reviewer fan-out (hk-2vpj); run COMPLETES as success without emitting either no-progress event. **§8.1a emission ordering note updated:** DOT-mode clause now documents three paths: (1) REQUEST_CHANGES → review_fixup_stalled + needs-attention; (2) no REQUEST_CHANGES/no APPROVE → no_progress_detected + needs-attention; (3) APPROVE + committed work → COMPLETE (no no-progress event). No EV requirement IDs added/renamed/retired; no §8 entries renumbered. Bead ref: hk-8ps7q. |
| 2026-06-13 | 0.6.2 | agent (hk-01i) | **§8.14 HITL-decisions lifecycle — 3 F-class `decision_*` events (codename:hitl-decisions, hk-33p).** Adds a new §8.14 taxonomy cohort for the agent→human decision dual of agent-comms, matching the landed `internal/core/decisionpayloads_33p.go` schema and `internal/eventbus/busimpl.go` fsync-boundary registration. **Taxonomy additions (3 new §8.14 rows):** §8.14.1 `decision_needed` (F; agent-emitted via `harmonik decisions raise`; payload `question`, `options []string` ≥1, `context_link?`, `blocked_agent?`, `value_requested?`), §8.14.2 `decision_resolved` (F; operator-emitted via `harmonik decisions answer`; payload `decision_id`, `chosen_option`, `value?`, `resolver?`), §8.14.3 `decision_withdrawn` (F; agent `self_obsoleted` OR keeper `orphaned`; payload `decision_id`, `reason ∈ {self_obsoleted, orphaned}`, `by?`). All three Class F per hitl-decisions SPEC §6 N1 (a lost terminal orphans the blocked agent's wake — Risk R1, load-bearing). **New requirement ID EV-045** (`decision_id` keying & first-writer-wins): `decision_id` = the `decision_needed` event's own `event_id`; the open-set projection folds add-on-needed-by-`event_id` / remove-on-terminal-by-`payload.decision_id` / dedupe-on-`event_id`; resolution is first-writer-wins on `decision_id` (SPEC §9 / N3); no-op on unknown/terminal. **Distinct-family note** added clarifying §8.14 is NOT the pre-existing §8.12 `decision_required` / `decision_acknowledged` daemon-escalation pair. NOT a paired-phase per §8.9(h). Required/optional field rules and blocked-wait/orphan-reap (keeper-sole-emitter) notes added; §8.14 Section Axes note added. New IDs: EV-045. New event types: `decision_needed` (§8.14.1, class F), `decision_resolved` (§8.14.2, class F), `decision_withdrawn` (§8.14.3, class F). No prior event-type identifiers renumbered or retired. Bead ref: hk-01i. Status remains `reviewed`. |

## A. Appendices

### A.3 Rationale

**Why taxonomy-first.** The spec's substance IS the typed event taxonomy: every normative rule is of the form "each entry in §8 obeys this rule." The reading order puts the taxonomy before the requirements so rules land as "rules about the table."

**Why the envelope-plus-registry Go shape.** A single all-fields struct breaks on every new event type; generics-based discriminated unions do not compose cleanly in current Go; one Go type per event with no common envelope prevents heterogeneous stream processing. Envelope-plus-registry is the standard `encoding/json` pattern and decouples the type list from the envelope.

**Why the three-class consumer taxonomy with default-observer.** Without class distinction, consumers drift into either "fail-the-producer" or "swallow-failures" behaviors. Default-observer prevents accidental coupling. `synchronous` is retained for future bus-level halt-on-invariant-violation surfaces; the acyclicity clause (EV-010) prevents reentrant deadlock. No MVH consumer registers synchronous; removing and re-adding the class later costs more than keeping it. Per EV-014c, observer dispatch uses per-observer bounded queues on the same shed semantics as async.

**Why durability-class-driven fsync (EV-016) over hardcoded names.** The first draft hardcoded four types, which rots on every addition. The class framing ties fsync to a semantic property (loss cost > sync cost) declared per §8 row. `workspace_merge_status` is F because losing a merge forces git-DAG reconstruction; `transition_event` is F because it carries divergence-evidence for Cat 3/4 reconciliation; `agent_output_chunk` is L because chunks are statistical aggregates. Per EV-016a, atomicity across multiple boundary events is NOT guaranteed; producers requiring paired durability must emit a single event or resolve via git. Group-commit batching is deferred post-MVH.

**Why event-loss between fsyncs is acceptable.** Git is authoritative for state. Events are observational. Treating events as authoritative would gate the critical path on disk I/O for every emission. The lossy-tail design makes non-blocking emission viable; idempotency contracts (EV-018 on producers, EV-014b on consumers) ensure recovery safety. EV-023/EV-023a carve out divergence-evidence reads with the post-crash-window guardrail and the inconclusive-evidence rule for non-corroborable events.

**Why paired-phase events were merged.** Rate-limit, merge-pending, and pause-state pairs carried identical information content except a boolean phase. Consumers wrote correlation logic to reunite them. A status-field single event eliminates the correlation burden. §8.9(h) now enforces emit-on-transition-only discipline so keepalive re-emission cannot re-introduce the correlation problem. Gate verdicts (`gate_allowed` / `gate_denied` / `gate_escalated`) are distinct terminal outcomes, not sequential phases, and remain split.

**Why per-chunk `agent_output_chunk` and `budget_accrual` are retained.** The future improvement-loop subsystem needs per-chunk cost attribution; collapsing loses information. Their `lossy-tail-ok` class matches their statistical-aggregate consumption.

**Why no `bead_claimed` / `bead_closed` / `bead_reopened`.** Beads terminal transitions ride on `run_started` / `run_completed` / `run_failed` with `bead_id` per [beads-integration.md §4.6 BI-010/BI-011]. A separate bead-lifecycle family would duplicate the signal.

**Why `metric` has `any subsystem` as emitter.** Metric observability is open-ended by design; constraining it would force an amendment per metric. §8.9(g)'s single escape-hatch exception is justified on that ground.

**Why the consumer-recovery replay contract (EV-014d) closes EV-INV-002's consumer side.** The Round-2 consumer-implementer review flagged that EV-INV-002 named a two-sided covenant but the spec only delivered the producer side (idempotency, fsync on F). The consumer side — "I tolerate gaps if you give me hooks to close them" — needed an offset checkpoint, a replay primitive, and a tail-truncation signal. Subscription.since + `ReplayFrom` + `on_tail_truncation` deliver these three hooks. Synchronous consumers do not participate in replay because re-invoking a synchronous handler on restart could double-commit side effects; their critical-path contract ended when `Emit` returned.

**Why UUIDv7 ordering needs the high-water-mark (EV-002c).** UUIDv7's time bits are wall-clock. NTP adjustment, VM pause/resume, and operator clock fixes can regress the wall clock across a daemon restart. Without HWM persistence, a post-restart event could sort before a pre-restart event, violating EV-008's partial order at the restart boundary. Piggybacking the HWM write on the F-class fsync domain keeps the cost flat — there is no second fsync — and aligns HWM durability with the events the HWM needs to sort after.

**Hidden assumptions explicitly acknowledged.** (1) The bus is in-process; cross-process consumers (investigator agents in separate Claude Code sessions) read JSONL subject to EV-021/EV-022. (2) One JSONL file per project; cross-project correlation is out of scope. (3) Redaction-before-observe destroys evidence that a payload contained a secret; this is a deliberate safety-over-forensics tradeoff. (4) `trace_context.parent_event_id` is populated SHOULD, not MUST; payload-specific causal fields (`triggering_event_id`) coexist deliberately for cases where the causal link is type-specific. (5) 71-event taxonomy has no hard budget; post-MVH subsystems expand via EV-027; a soft target of ≤120 is advisory, not normative. (6) `fsync(2)` durability is contingent on the filesystem honoring write barriers and the storage device flushing its write cache; consumer-grade SSDs without power-loss-protection may silently weaken EV-016 to "best-effort durability at the kernel boundary." Operators on such hardware accept this floor.
