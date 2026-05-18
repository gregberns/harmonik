# Event Model — Change Design (C3)

Scope: add six new event types covering the review-loop cycle. Reviewer verdict payload reuses the existing `agent-reviewer` JSON schema v1 verbatim. All new events carry the standard `run_id` join key per `EV-001`.

## 1. Current state

- `event-model.md §8.1 Run lifecycle` (lines ~75–93) declares eleven event types — `run_started`, `run_completed`, `run_failed`, `state_entered`, `state_exited`, `transition_event`, `checkpoint_written`, `outcome_emitted`, `sub_workflow_entered`, `sub_workflow_exited`, `node_dispatch_requested`. Durability classes mix F (fsync) and O (ordinary). Section Axes default: class O is `llm-freedom=none; io-determinism=best-effort; replay-safety=safe; idempotency=non-idempotent`.
- `§8.3 Agent / handler lifecycle` (lines ~116–136) declares thirteen agent-scoped events including `agent_started`, `agent_completed`, `outcome_emitted`-via-watcher, `session_log_location`, `skills_provisioned`. All class O or L.
- `§4.1 EV-001..EV-005` (lines ~249–297) names the envelope obligations: every event MUST carry `event_id`, `run_id` (when scoped to a run), `type`, `source_subsystem`, `timestamp_*`, `schema_version`. Cross-subsystem additions require a foundation amendment per `EV-027`.
- `§6.3` owns payload-field Go types; `§8.9` owns acceptance criteria for candidate event types.

## 2. Target state

### (a) Six new event types in `§8.1` (or a new `§8.1a — Review-loop cycle` subsection)

| # | Type | Dur | Emitter | Typical consumers | Payload fields |
|---|---|---|---|---|---|
| 8.1.a1 | `implementer_resumed` | O | orchestrator-core | audit, observability, improvement-loop | `run_id`, `claude_session_id`, `iteration_count`, `prior_verdict_summary` (string, ≤ 256 bytes, derived from prior reviewer `notes`) |
| 8.1.a2 | `reviewer_launched` | O | orchestrator-core | audit, observability | `run_id`, `claude_session_id`, `iteration_count` |
| 8.1.a3 | `reviewer_verdict` | F | orchestrator-core (after `.harmonik/review.json` read) | audit, observability, improvement-loop | `run_id`, `claude_session_id`, `iteration_count`, **reviewer-JSON schema v1 fields verbatim**: `schema_version`, `verdict ∈ {APPROVE, REQUEST_CHANGES, BLOCK}`, `flags[]`, `notes` |
| 8.1.a4 | `iteration_cap_hit` | F | orchestrator-core | audit, observability, operator-observability | `run_id`, `iteration_count`, `cap_value` (=3 at MVH), `final_verdict` |
| 8.1.a5 | `no_progress_detected` | O | orchestrator-core | audit, observability | `run_id`, `iteration_count`, `diff_hash_current`, `diff_hash_prior` |
| 8.1.a6 | `review_loop_cycle_complete` | F | orchestrator-core | audit, beads-integration, observability | `run_id`, `final_iteration_count`, `completion_reason ∈ {approved, cap_hit, blocked, no_progress, error}` |

Section Axes for the new entries follow `§8.1`'s class-F and class-O defaults verbatim.

### (b) Envelope obligations

All six events MUST carry the standard envelope (`EV-001`) and `run_id`. Per locked decision: one `run_id` covers the entire review-loop cycle; multiple `claude_session_id`s exist under it. The `claude_session_id` field is distinct from `EV-001`'s join keys — it is a payload-level field, not a top-level envelope field. Document in `§3 Glossary` so future readers do not confuse it with harmonik's `session_id` event field (which still appears on `§8.3` agent events for the implementer and reviewer launches).

### (c) Reviewer-verdict schema reuse

Add a normative note under `8.1.a3 reviewer_verdict`: the payload's `schema_version`, `verdict`, `flags`, `notes` fields conform to the existing `agent-reviewer` skill's JSON verdict schema v1 (referenced from `CLAUDE.md` skill registry). The daemon reads `.harmonik/review.json` from the worktree (path owned by `workspace-model.md`; see `workspace-model-design.md`), validates against schema v1, and emits the event. NO parallel schema is introduced. The reviewer-hardening rule — `notes` MUST include file:line citations for any `REQUEST_CHANGES` — is enforced upstream of event emission (reviewer prompt + adapter validation); event-model only records what was emitted.

### (d) Alternative-considered note (legibility vs schema bloat)

Add an informational note (in `§8.1` preamble or `§8.9` acceptance criteria appendix): an alternative considered was folding `implementer_resumed` / `reviewer_launched` into the existing `outcome_emitted` / `agent_started` with a `role ∈ {implementer, reviewer}` discriminator. v1 prefers distinct event types for legibility and to give consumers a cheap predicate filter. The role-field consolidation is captured as a future refactor candidate; promoting it requires `EV-027` foundation amendment.

### (e) Coordination event for beads-label conflict

Per `beads-integration-design.md` §2 (b): add a single class-O observability event in `§8.8` (Observability and bus-internal), provisionally `bead_label_conflict`, payload `{bead_id, conflicting_labels[]}`. This is not strictly a review-loop event but is required by the precedence-resolution path EV emits when tier-1 input is malformed. (Alternative: place in `§8.6 Reconciliation lifecycle`; spec-draft picks.)

### (f) `workflow_mode` on run-lifecycle event payloads

All run-lifecycle events (`run_started`, `run_completed`, `run_failed`, and the new review-loop events) carry an optional `workflow_mode ∈ {single, review-loop, dot}` field in their payload, surfacing the resolved mode for per-run filtering and observability.

### (g) `EV-027` amendment trigger

The six new event types and the `bead_label_conflict` event are cross-bus additions per `EV-027` — adding or removing a cross-bus event type requires a foundation amendment. The amendment scope for the workflow-modes kerf covers these seven additions.

## 3. Rationale

- Satisfies problem-space success criteria §4 (event types for review-loop cycle) and §5 (reviewer JSON contract reuses agent-reviewer schema).
- `reviewer_verdict` promoted from class O (as scoped in 02-components.md §C3) to class F because the verdict drives terminal routing of the run; loss would orphan a closed-or-needs-attention task.
- Distinct event types (over a role-field on existing types) make the cycle visible at a glance in `jq` consumption — operator-NFR observability path (`harmonik logs`, `jq`) becomes legible without payload-field filtering.
- `no_progress_detected` is required by the locked decision "no-progress diff-hash early-exit." Without an event, the path becomes silently observable only via verdict + non-emission.
- Locked decision: one `run_id` umbrella. The events spec must reflect this (no multi-`run_id`-per-task semantics). The `claude_session_id` payload field accommodates the multiple-implementer-launches-as-one-logical-session shape required by `--resume`.

## 4. Requirements traceability

| Req (02-components.md C3) | Target-state element |
|---|---|
| `implementer_resumed` event | §2 (a) #8.1.a1 |
| `reviewer_launched` event | §2 (a) #8.1.a2 |
| `reviewer_verdict` event using agent-reviewer JSON schema v1 | §2 (a) #8.1.a3, §2 (c) |
| `iteration_cap_hit` event | §2 (a) #8.1.a4 |
| `review_loop_cycle_complete` event (class F) | §2 (a) #8.1.a6 |
| All carry `run_id` per envelope contract | §2 (b) |
| Reviewer JSON schema reused verbatim — no parallel schema | §2 (c) |
| Alternative considered (role-field on existing events) recorded | §2 (d) |

## 5. Open decisions remaining for spec-draft pass

- **`bead_label_conflict` placement.** §8.6 vs §8.8. Recommend §8.8 (observability); spec-draft chooses.
- **`prior_verdict_summary` derivation.** How the daemon derives the ≤ 256-byte summary from the prior reviewer `notes` (truncate? structured-extract-flag-list?). Recommend simple front-truncate at MVH; spec-draft picks.
- **Class of `no_progress_detected`.** Class O is the recommendation (observability-only); the run's termination is signaled by `review_loop_cycle_complete` class-F. Spec-draft confirms.
