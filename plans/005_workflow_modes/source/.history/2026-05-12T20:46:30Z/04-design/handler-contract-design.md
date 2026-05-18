# Handler Contract — Change Design (C1)

Scope: LaunchSpec gains optional `workflow_mode` and `phase` fields; idempotency key extends to (run_id, node_id, phase, iteration); restate that mode dispatch is **dispatch-level**, not handler-selection-level. Adapter surface (HC-013) does NOT expand. Watcher and adapter remain mode-agnostic.

## 1. Current state

- `handler-contract.md §4.1 HC-001..HC-004` (lines ~78–101) defines Handler/Session interfaces. **`HC-003`** (line ~91) — "Handler selection is config-level." **`HC-004`** (line ~96) — "Launch is idempotent on `(spec.run_id, spec.node_id)` within one daemon generation." Quote (line 98): "a second `Launch` call with the same key MUST return the existing `Session` ... rather than spawn a duplicate subprocess."
- `§4.2 HC-005..HC-010` (lines ~105–166): wire protocol. **`HC-006`** (line ~112) names the LaunchSpec required and optional fields (`run_id`, `workflow_id`, `node_id`, `agent_type`, `workspace_path`, `required_skills[]`, `skill_search_paths[]`, `timeout`, `provisioning_timeout`, `budget`, `freedom_profile_ref`; optional `bead_id`, `snapshot_token`). No workflow-mode field today.
- `§4.3 HC-011..HC-016a` (lines ~170–227): concurrency model. **`HC-013`** (line ~190) — "Adapter surface is fixed" — names the four callbacks (DetectReady, DetectRateLimit, CleanExitSequence, RotateAccount); adding to this surface requires a foundation amendment.

## 2. Target state

### (a) LaunchSpec field additions

Amend `HC-006` (and the corresponding `§6.1` LaunchSpec type def). Add two optional fields:

- **`workflow_mode`** (enum `{single, review-loop, dot}`, optional). Present iff the daemon resolved a non-default mode for the dispatched run; otherwise omitted. The handler MUST accept the field and MAY ignore it for `single` mode. Handlers MUST NOT branch implementation behavior on this field — it is observational, supplied for handler-side logging and skill-loading hints only.

- **`phase`** (enum, optional). Present iff the dispatched run is in a multi-phase mode. For `review-loop` mode the domain is `{implementer-initial, implementer-resume, reviewer}`. Other modes reserve other values; `single` mode omits the field. The handler does not interpret routing semantics from `phase`; routing is the daemon's responsibility per §2 (c) below.

Also add to `HC-006` for `review-loop` dispatches: optional fields **`iteration`** (integer, 1..3) and **`claude_session_id`** (string, present only on `phase=implementer-resume` to drive `claude --resume <id>` per the session-resume mechanism). The reviewer phase MUST omit `claude_session_id`; each reviewer launch is a fresh Claude session.

### (b) Idempotency key extension

Amend `HC-004` so the idempotency key is widened for multi-phase modes. The current key `(run_id, node_id)` becomes `(run_id, node_id, phase, iteration)` when both `phase` and `iteration` are present on the LaunchSpec; otherwise the existing two-tuple is preserved. Concrete consequence: within one review-loop cycle, the daemon may legitimately issue distinct Launch calls for `(run_id, node_id=implementer, phase=implementer-resume, iteration=2)` and the prior `(..., iteration=1)` without HC-004's "return existing Session" branch firing.

Crash-replay semantics extend straightforwardly: idempotency is scoped per daemon generation (HC-004's existing rule); review-loop launches that survived a daemon restart re-launch under their `(run_id, node_id, phase, iteration)` key, which makes them distinguishable from prior launches in the same logical cycle.

### (c) Mode dispatch is dispatch-level, not handler-selection-level

Amend `HC-003` with a clarifying sentence (or add `HC-003a — Workflow-mode is dispatch-level, not handler-selector`). State explicitly: `workflow_mode` MUST NOT be used to pick among registered handlers. Handler selection remains a config-level binding from `agent_type` to a registered handler. Mode determines (a) which phase the daemon launches next and (b) the LaunchSpec's `phase` / `iteration` / `claude_session_id` fields. The same Claude-Code handler runs the implementer and the reviewer phases; they are distinguished by LaunchSpec content (prompt, required-skills, freedom profile), not by handler binding.

This framing preserves the locked decision: the adapter surface (`HC-013`) does NOT expand. Watcher behavior is unchanged. The same agent-runner adapter wraps both phases.

### (d) Skill-loading hint

Add a non-normative note (or `HC-006` clarification): the `phase=reviewer` launch will typically carry an `agent-reviewer` skill in `required_skills[]` (per CLAUDE.md skill registry); the `phase=implementer-*` launches carry the implementer skill set. Selecting which skills appear in `required_skills[]` is the daemon's claim-path responsibility, not the handler's. The reviewer phase's `outcome_emitted` corresponds to writing `.harmonik/review.json` per `workspace-model-design.md §2(c)`.

## 3. Rationale

- Locked decision: mode name is `review-loop` (not `ralph`); two non-`single` modes exist with planned `dot`. Adding `workflow_mode` to LaunchSpec lets handler-side logging and skill-loading branch on the resolved mode without requiring a new handler type per mode.
- Locked decision: same implementer Claude session resumed across iterations. The `claude_session_id` field on the LaunchSpec is the wire-level vehicle for `claude --resume <id>` — required by `session-resume/findings.md`.
- Idempotency-key widening (b) is required so re-launches across iterations are not deduplicated as repeat launches of the same phase. Without this widening, HC-004 would silently coalesce iteration N's implementer launch into iteration N-1's Session.
- The "dispatch-level, not handler-selector" clarification (c) is locked-decision material and pre-empts a misreading of `HC-003`. The user committed: "three modes total: `single`, `review-loop`, `dot`." Adding two more `agent_type`s would have been the wrong shape.

## 4. Requirements traceability

| Req (02-components.md C1) | Target-state element |
|---|---|
| LaunchSpec gains `workflow_mode` field | §2 (a) |
| LaunchSpec gains `phase` (and `iteration`, `claude_session_id`) field | §2 (a) |
| HC-004 idempotency widens to `(run_id, node_id, phase, iteration)` | §2 (b) |
| `HC-003` handler-selection rule preserved; mode is dispatch-level | §2 (c) |
| Adapter surface (HC-013) does NOT expand | §2 (c) (explicit), no HC-013 amendment |
| Watcher remains mode-agnostic | §2 (c) (explicit) |

## 5. Open decisions remaining for spec-draft pass

- **Enum value `phase=implementer-fresh-next-iteration`.** Research finding ralph-prior-art enumerated this for the fresh-per-iteration alternative. Per locked decision (same-session resume), it is NOT needed. Spec-draft confirms only `implementer-initial`, `implementer-resume`, `reviewer` are declared for `review-loop`.
- **Iteration field placement.** Whether `iteration` is a top-level LaunchSpec field or nested inside a `phase_context` sub-object. Recommend top-level for clarity at the wire level; spec-draft picks.
