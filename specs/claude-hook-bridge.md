# Claude Hook Bridge

```yaml
---
title: Claude Hook Bridge
spec-id: claude-hook-bridge
requirement-prefix: CHB
status: draft
spec-category: runtime-subsystem
spec-shape: requirements-first
version: 0.9
spec-template-version: 1.1
owner: foundation-author
last-updated: 2026-05-13
depends-on:
  - handler-contract
  - workspace-model
  - event-model
  - process-lifecycle
  - execution-model
  - architecture
---
```

## 1. Purpose

This spec defines the deterministic translation layer between the Claude Code CLI's native lifecycle (settings.json hooks, `--session-id`, transcripts) and harmonik's handler-contract progress-stream wire protocol. It is the MVH realization of S05 (Hook System) for the `claude-code` agent type.

The bridge has three load-bearing parts:

1. A `.claude/settings.json` file materialized in the workspace at workspace creation, declaring command-type hooks that invoke `harmonik hook-relay`.
2. A `harmonik hook-relay <event-kind>` subcommand of the main harmonik binary that translates Claude's per-hook JSON-on-stdin into harmonik progress-stream NDJSON messages on the daemon's Unix domain socket.
3. A pre-generated `claude_session_id` flow: the handler subprocess mints the UUID, passes it to Claude via `--session-id <uuid>`, reports it to the daemon via `handler_capabilities`, and uses `--resume <claude_session_id>` for `phase = implementer-resume`.

This spec is normative for the claude-code agent type only. Other agent types may re-realize the bridge surface per their own per-agent-type bridge specs post-MVH.

## 2. Scope

### 2.1 In scope

- `.claude/settings.json` file content materialized in a harmonik-managed workspace.
- The env-var schema inherited by the handler subprocess, by Claude Code, and by relay subprocesses.
- The `harmonik hook-relay <event-kind>` subcommand contract: stdin payload, env-var consumption, daemon-socket message construction and send, exit-code semantics.
- The mapping from Claude hook events (`SessionStart`, `Stop`, `SessionEnd`, `StopFailure`, `Notification`) to harmonik progress-stream messages (`agent_ready`, `outcome_emitted`, `agent_completed`, `agent_failed`, `agent_rate_limited`, `agent_heartbeat`).
- The pre-generated `claude_session_id` flow including reuse on `phase = implementer-resume`.
- The handler-process responsibility for emitting `handler_capabilities`, `session_log_location`, `skills_provisioned`, and `launch_initiated` BEFORE exec'ing Claude; and the relay-subprocess responsibility for synthesizing `agent_ready` on receipt of the first `SessionStart` hook (since the relay-synthesized `agent_ready` is the first claude-originated lifecycle signal under the tmux substrate).
- The handler-process responsibility for emitting timer-driven `agent_heartbeat` while Claude is alive.
- Failure-mode classification: relay-can't-dial-socket, daemon-not-ready-retry-exhausted, malformed hook payload, missing `.harmonik/review.json` at Stop in reviewer phase.
- Twin-parity rules: `harmonik-twin-claude` emits the same wire-format NDJSON sequence the bridge would synthesize from Claude, WITHOUT going through the relay subcommand.
- The per-launch task-delivery artifact `${workspace_path}/.harmonik/agent-task.md`: atomic-write discipline, reserved name, content shape by phase, gitignore hygiene, and re-attach semantics.

### 2.2 Out of scope

- Per-agent-type bridges other than claude-code — each is its own spec post-MVH.
- Settings.json schema beyond what the bridge declares — the user's hook entries coexist with harmonik's per the merge rule (§4.1.CHB-009).
- Claude Code authentication, account-rotation, or provider-secret rotation — out of scope here; covered by [handler-contract.md §4.7].
- The Claude transcript file format at `~/.claude/projects/<slug>/<session-uuid>.jsonl` — read-only consumed; not redefined.
- Hook events Claude supports other than the five enumerated in §4.4 — handled by relay no-op.
- The `stream-json + --include-hook-events` alternative bridging architecture — documented in §11 Informative as a post-MVH evolution path; not adopted at MVH.

## 3. Glossary

- **bridge** — this spec, in its entirety: the translation layer between Claude Code and the daemon's progress-stream wire protocol.
- **hook-relay subprocess** — a short-lived subprocess invocation of `harmonik hook-relay <event-kind>` spawned by Claude Code via a command-type hook declared in `.claude/settings.json`. Child of Claude Code, grandchild of the daemon.
- **claude_session_id** — the Claude Code session identifier, a UUID. Same value passed via `--session-id <uuid>` (or carried on `--resume <claude_session_id>`) and echoed back in every hook payload's `session_id` field. Distinct from harmonik's handler-side `session_id` (per [handler-contract.md §4.1]) which is the UUIDv7 minted by the handler.
- **harmonik handler-process** — the long-lived OS process produced by `Handler.Launch` for `agent_type = claude-code`; parent of Claude Code; owner of the long-lived bidirectional progress-stream socket connection to the daemon.
- **two-contributor model** — the architectural pattern in which both the harmonik handler-process AND hook-relay subprocesses write NDJSON progress-stream messages to the daemon socket, both keyed by `(run_id, claude_session_id)`, both routed by the daemon's watcher to the same per-session bus event flow.

## 4. Normative requirements

### 4.a Subsystem envelope

#### CHB-ENV-001 — Envelope declaration

Envelope for the claude-hook-bridge subsystem per [/Users/gb/github/harmonik/specs/architecture.md §4.0 AR-053]. The bridge is the deterministic translation layer between Claude Code's native lifecycle (settings.json hooks, `--session-id`, transcripts) and harmonik's handler-contract progress-stream wire protocol; it is the MVH realization of S05 (Hook System) for `agent_type = claude-code`. It has two emitter roles — the handler-process (long-lived) and the hook-relay subprocess (short-lived, one per Claude hook firing) — that together write NDJSON progress-stream messages to the daemon's Unix domain socket, both keyed by `(run_id, claude_session_id)`.

(a) Events produced (progress-stream messages emitted by handler-process and relay-subprocess; the bridge introduces zero new bus event types per §1; all entries are existing progress-stream messages whose schemas are owned by [/Users/gb/github/harmonik/specs/handler-contract.md §4.2] and [/Users/gb/github/harmonik/specs/event-model.md §8.1, §8.3]):
  - `handler_capabilities` — handler-process; emission rule §4.7 CHB-018 step 1 (carries `claude_session_id`); schema in [/Users/gb/github/harmonik/specs/handler-contract.md §4.2 HC-009].
  - `session_log_location` — handler-process; emission rule §4.7 CHB-018 step 2; schema in [/Users/gb/github/harmonik/specs/event-model.md §8.3.7].
  - `skills_provisioned` — handler-process; emission rule §4.7 CHB-018 step 3; schema in [/Users/gb/github/harmonik/specs/handler-contract.md §4.11].
  - `agent_ready` — handler-process; emission rule §4.7 CHB-018 step 4; schema in [/Users/gb/github/harmonik/specs/event-model.md §8.1].
  - `agent_heartbeat` — handler-process timer (§4.7 CHB-019, every 300 s) AND relay subprocess (§4.5 CHB-013, on Notification hooks); schema in [/Users/gb/github/harmonik/specs/event-model.md §8.3].
  - `outcome_emitted` — relay subprocess; emission rule §4.5 CHB-013 (Stop, StopFailure non-rate-limit), §4.10 CHB-025 (daemon last-received-wins dedup); schema in [/Users/gb/github/harmonik/specs/event-model.md §8.1a].
  - `agent_rate_limited` — relay subprocess; emission rule §4.5 CHB-013 (StopFailure rate_limit); schema in [/Users/gb/github/harmonik/specs/event-model.md §8.3].
  - `agent_completed` — handler-process on `cmd.Wait()` return; emission rule §4.7 CHB-020; schema in [/Users/gb/github/harmonik/specs/event-model.md §8.1].
  - `agent_failed` — handler-process on `cmd.Wait()` return OR settings-shadow check failure; emission rules §4.7 CHB-020, §4.9 CHB-024, §4.6 CHB-027 (daemon-side fallback for partial-write); schema in [/Users/gb/github/harmonik/specs/event-model.md §8.1].
  - `launch_initiated` — handler-process pre-exec emission (cross-referenced in §1 scope); schema in [/Users/gb/github/harmonik/specs/handler-contract.md §4.2].

(b) Events consumed:
  - Claude Code hook events (`SessionStart`, `Stop`, `SessionEnd`, `StopFailure`, `Notification`) — out-of-process inputs delivered via Claude's settings.json hook mechanism (§4.1 CHB-003); not bus events. The relay subprocess reads each as JSON-on-stdin per §4.4 CHB-012.
  - `outcome_emitted` — the handler-process observes its own session's `outcome_emitted` (looped back via the daemon watcher) to drive terminal-event derivation on Wait-return per §4.7 CHB-020.
  - `daemon_not_ready{reason=unknown_run_id}` — typed-error response on the daemon socket; the relay consumes this to drive its retry loop per §4.6 CHB-016; schema in [/Users/gb/github/harmonik/specs/process-lifecycle.md §4.2 PL-003b].

(c) Types introduced (cross-subsystem; the bridge is wire-format-only, introducing no new payload records — all message types are owned by handler-contract / event-model; the entries below are bridge-internal contracts that appear at cross-subsystem boundaries):
  | Type | `Tags:` | `Axes:` (if non-baseline) |
  |---|---|---|
  | `.claude/settings.json` hooks block (§4.1 CHB-003) | mechanism | `io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent` (on-disk artifact) |
  | `${workspace_path}/.harmonik/agent-task.md` (§4.11 CHB-028) | mechanism | `io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent` (on-disk artifact) |
  | `HARMONIK_*` env-var schema (§4.2 CHB-006) | mechanism | baseline |
  | `claude_session_id` (UUIDv7; cross-subsystem identifier carried on `handler_capabilities`, on `Run.context.claude_session_id` per CHB-023, and on the per-message envelope) | mechanism | baseline |
  | `bridge_*` stderr error codes (`bridge_session_id_mismatch`, `bridge_event_kind_mismatch`, `bridge_daemon_startup_window_exceeded`, `bridge_partial_write`, `bridge_settings_shadowed`) | mechanism | baseline |
  | `WorkspaceRef` (consumed from [/Users/gb/github/harmonik/specs/execution-model.md §6.1]) | mechanism | baseline |
  | `LaunchSpec` (consumed from [/Users/gb/github/harmonik/specs/handler-contract.md §4.2]) | mechanism | baseline |

(d) Handlers implemented: none. The bridge is not a handler-contract handler; it is the wire-protocol translation layer that the `claude-code` handler-process uses internally to translate Claude's native hook lifecycle into the handler-contract progress-stream. The `claude-code` handler itself is declared in [/Users/gb/github/harmonik/specs/handler-contract.md]; this spec is normative for that handler's internal bridge surface only.

(e) State owned:
  - `${workspace_path}/.claude/settings.json` (§4.1) — materialized per workspace; daemon owns writes.
  - `${workspace_path}/.harmonik/agent-task.md` (§4.11 CHB-028) — materialized per launch; daemon owns writes; reserved name.
  - In-process per-session `latestOutcome` field on the daemon's per-session watcher (§4.10 CHB-025) — bounded to the open session window from first `outcome_emitted` until `cmd.Wait()` returns.
  - `Run.context.claude_session_id` durability is consumed but NOT owned here ([/Users/gb/github/harmonik/specs/execution-model.md §4.3 EM-012, EM-015d]); CHB-023 names the persistence-ordering rule, not the storage home.
  - Beyond these on-disk artifacts and the watcher field, the bridge owns no persistent state; it is a stateless translation surface.

(f) Control points provided: none. The bridge is a mechanism-tagged subsystem; its operations are not gate/hook/guard/budget points per [/Users/gb/github/harmonik/specs/control-points.md §4.1]. The Claude hook firings the relay consumes (`Stop`, `SessionEnd`, etc.) are Claude-internal lifecycle signals, not harmonik control points.

(g) NFRs inherited / overridden:
  - Inherited: `ON-018` N-1 schema compatibility (the env-var schema §4.2 CHB-006 is additive-only; the hooks-block content §4.1 CHB-003 is versioned implicitly by the `args` form).
  - Inherited: `ON-027` graceful-shutdown ordering (the handler-process's Wait-return terminal-event emission per §4.7 CHB-020 participates in the daemon shutdown drain).
  - Inherited: `HC-INV-002` (twin-blind daemon) per §4.8 CHB-022; the daemon-side code carries zero `if isTwin` / `if relay` branches.
  - Inherited: `HC-INV-004` (pre-exec emission ordering) per §4.7 CHB-018.
  - Inherited: `HC-INV-006` (exactly one terminal event per session) per §4.7 CHB-020.
  - Overridden: none.

(h) Boundary classification per operation:
  | Operation | `Tags:` | Axes |
  |---|---|---|
  | `materialize_settings_json` (§4.1 CHB-001 / CHB-002) | mechanism | `llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent` |
  | `merge_user_settings` (§4.1 CHB-004) | mechanism | `llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent` |
  | `materialize_agent_task` (§4.11 CHB-028) | mechanism | `llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent` |
  | `set_env_vars` (§4.2 CHB-006) | mechanism | `llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent` |
  | `mint_or_resume_claude_session_id` (§4.3 CHB-008 / CHB-009) | mechanism | `llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent` |
  | `hook_relay_translate` (§4.4 CHB-010, §4.5 CHB-013, §4.6 CHB-015) | mechanism | `llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent` |
  | `reviewer_verdict_file_read` (§4.5 CHB-014) | mechanism | `llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent` |
  | `daemon_socket_dial_and_send` (§4.6 CHB-015 / CHB-016) | mechanism | `llm-freedom=none; io-determinism=best-effort; replay-safety=safe; idempotency=non-idempotent` |
  | `daemon_last_received_wins_dedup` (§4.10 CHB-025) | mechanism | `llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent` |
  | `handler_pre_exec_emission` (§4.7 CHB-018) | mechanism | `llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent` |
  | `handler_timer_heartbeat` (§4.7 CHB-019) | mechanism | `llm-freedom=none; io-determinism=best-effort; replay-safety=safe; idempotency=idempotent` |
  | `handler_terminal_emission_on_wait_return` (§4.7 CHB-020) | mechanism | `llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent` |
  | `settings_shadow_verification` (§4.9 CHB-024) | mechanism | `llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent` |
  | `twin_emit_wire_format` (§4.8 CHB-021) | mechanism | `llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent` |

Tags: mechanism

### 4.1 Settings.json materialization

#### CHB-001 — `.claude/settings.json` path and ownership

For every workspace that will host a `claude-code` agent session, the workspace manager MUST materialize a file at `${workspace_path}/.claude/settings.json`. The file's content is owned by this spec; the workspace manager MUST NOT add, remove, or modify the bridge-required entries.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent

#### CHB-002 — Materialization ordering and atomic write

The `${workspace_path}/.claude/settings.json` file MUST be written between [workspace-model.md §4.1 WM-003] (worktree creation) and [workspace-model.md §4.4 WM-016] (`workspace_leased` emission), folded into the same fsync gate. The write MUST follow the atomic-write discipline of [workspace-model.md §4.7 WM-026]: write to a sibling temp file, fsync the temp file, rename to the canonical name, fsync the parent directory. The parent-directory fsync MUST complete BEFORE `workspace_leased` emits.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent

#### CHB-003 — Required hook entries

The materialized `${workspace_path}/.claude/settings.json` MUST contain at least the following `hooks` entries (using `type: command` with `args` form for shell-injection safety; `command: "harmonik"`):

```
{
  "hooks": {
    "SessionStart":   [{ "matcher": "", "hooks": [{ "type": "command", "command": "harmonik", "args": ["hook-relay", "SessionStart"],   "timeout": 30 }] }],
    "Stop":           [{ "matcher": "", "hooks": [{ "type": "command", "command": "harmonik", "args": ["hook-relay", "Stop"],           "timeout": 30 }] }],
    "SessionEnd":     [{ "matcher": "", "hooks": [{ "type": "command", "command": "harmonik", "args": ["hook-relay", "SessionEnd"],     "timeout": 30 }] }],
    "StopFailure":    [{ "matcher": "", "hooks": [{ "type": "command", "command": "harmonik", "args": ["hook-relay", "StopFailure"],    "timeout": 30 }] }],
    "Notification":   [{ "matcher": "", "hooks": [{ "type": "command", "command": "harmonik", "args": ["hook-relay", "Notification"],   "timeout": 30 }] }]
  }
}
```

The hook timeout is fixed at 30 seconds at MVH. The relay's internal retry budget against `daemon_not_ready` MUST fit inside this envelope. The `command` value `"harmonik"` MUST be resolvable via PATH at Claude exec time; the handler MUST verify resolvability per [handler-contract.md §4.10 HC-042] before launch.

Tags: mechanism

#### CHB-004 — User-settings merge

If a `${workspace_path}/.claude/settings.json` file already exists at the materialization time (inherited from the cloned repo state per [workspace-model.md §4.1 WM-003]), the workspace manager MUST attempt a merge: for each event-type key under `hooks`, the bridge-required matcher group is APPENDED to the existing array. User-declared hooks for the same event continue to fire alongside the bridge's hooks.

If the existing file is malformed JSON, the workspace manager MUST OVERWRITE with the bridge-required content AND log a warning line to the session log noting the displacement. No new bus event is emitted at MVH (the bridge introduces zero new event types per §4); post-MVH operators MAY route this through an existing observability surface.

The `disableAllHooks: true` key, if present in the merged result, MUST be removed; the bridge's correct operation depends on hooks firing.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

#### CHB-005 — Gitignore hygiene

`${workspace_path}/.claude/settings.json` MUST be present in the worktree's `.gitignore` set per [workspace-model.md §4.3 WM-013e] at materialization time. The workspace manager MUST add the line if absent.

Tags: mechanism

### 4.2 Env-var schema

#### CHB-006 — Required env-var schema

The harmonik handler-process MUST set the following env vars on the Claude subprocess at exec time. Claude inherits them; hook-relay subprocesses inherit them via Claude. Names are the contract.

| Env var | Type | Required? | Notes |
|---|---|---|---|
| `HARMONIK_RUN_ID` | UUIDv7 string | required | LaunchSpec.run_id |
| `HARMONIK_DAEMON_SOCKET` | absolute path | required | Default `<repo>/.harmonik/daemon.sock` |
| `HARMONIK_WORKSPACE_PATH` | absolute path | required | LaunchSpec.workspace_path |
| `HARMONIK_HANDLER_SESSION_ID` | UUID string | required | Distinct from claude_session_id |
| `HARMONIK_CLAUDE_SESSION_ID` | UUID string | required | Matches Claude's `--session-id` / `--resume` value |
| `HARMONIK_WORKFLOW_ID` | UUID string | required | LaunchSpec.workflow_id |
| `HARMONIK_NODE_ID` | string | required | LaunchSpec.node_id |
| `HARMONIK_AGENT_TYPE` | string | required | Constant `"claude-code"` |
| `HARMONIK_WORKFLOW_MODE` | enum | optional | LaunchSpec.workflow_mode; only set when non-default |
| `HARMONIK_PHASE` | enum | optional | LaunchSpec.phase; only set in multi-phase modes |
| `HARMONIK_ITERATION_COUNT` | integer 1..3 | optional | LaunchSpec.iteration_count |
| `HARMONIK_BEAD_ID` | string | optional | LaunchSpec.bead_id |
| `HARMONIK_SECRET_*` | string | per HC-028 | Secret values, redacted per [handler-contract.md §4.7] |

The harmonik handler-process MUST NOT set any other `HARMONIK_*` env var; future fields require a foundation amendment.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

#### CHB-007 — Forbidden Claude flags

The harmonik handler-process MUST NOT pass any of the following flags to Claude:

- `--fork-session` — would mint a new session_id on resume, breaking the claude_session_id stability invariant.
- `--bare` — would disable hook auto-discovery, breaking the settings.json bridging mechanism.
- `--no-session-persistence` — would disable session persistence, breaking `phase = implementer-resume`.

The handler MUST also NOT set the env var `CLAUDE_CODE_SKIP_PROMPT_HISTORY` (which has the same effect as `--no-session-persistence`).

Tags: mechanism

### 4.3 Pre-generated claude_session_id flow

#### CHB-008 — Session ID minting and propagation

For `agent_type = "claude-code"`, the harmonik handler-process MUST (this duplicates the cross-handler discipline at [handler-contract.md §4.10 HC-045c]):

- For `phase ∈ {single, implementer-initial, reviewer}`: mint a fresh UUIDv7 as `claude_session_id`, pass it to Claude via `--session-id <claude_session_id>`, set `HARMONIK_CLAUDE_SESSION_ID = claude_session_id`, AND include `claude_session_id` in the payload of the `handler_capabilities` progress-stream message (per [handler-contract.md §4.2 HC-009] and [handler-contract.md §4.10 HC-045c]).
- For `phase = implementer-resume`: reuse `LaunchSpec.claude_session_id` (carried from the prior iteration; populated by the daemon per [handler-contract.md §4.2 HC-006]), pass it to Claude via `--resume <claude_session_id>` (NOT `--session-id`), set `HARMONIK_CLAUDE_SESSION_ID = LaunchSpec.claude_session_id`.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent

#### CHB-009 — Reviewer launches always mint fresh

For `phase = reviewer`, the handler MUST NOT inherit `claude_session_id` from a prior reviewer launch. Each reviewer phase mints a new UUIDv7. This preserves the per-iteration reviewer-launch independence implicit in [event-model.md §8.1a].

Tags: mechanism

### 4.4 Hook-relay subcommand contract

#### CHB-010 — Subcommand surface

The main `harmonik` binary MUST expose a subcommand `hook-relay <event-kind>` where `event-kind ∈ {SessionStart, Stop, SessionEnd, StopFailure, Notification}`. The subcommand reads a single JSON object from stdin (Claude's hook input payload per the hook-event docs), reads its environment for `HARMONIK_*` and `CLAUDE_*` variables, constructs a single NDJSON progress-stream message per §4.5, and writes it to the daemon socket per §4.6.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent

#### CHB-011 — Out-of-scope event kinds are no-op

For any `event-kind` not in the set above, `harmonik hook-relay` MUST exit 0 without writing to the daemon socket and without writing to stderr. This preserves forward-compatibility if a future operator-provided settings.json fragment references the relay on additional events.

Tags: mechanism

#### CHB-012 — Stdin payload schema

The relay MUST accept Claude's hook input JSON, which includes at minimum:

- `session_id` (string) — the claude_session_id; MUST match `HARMONIK_CLAUDE_SESSION_ID` env var or the relay exits 1 with `bridge_session_id_mismatch` on stderr.
- `transcript_path` (string) — absolute path to Claude's session transcript.
- `cwd` (string)
- `permission_mode` (string)
- `hook_event_name` (string) — MUST match the `<event-kind>` argument or the relay exits 1 with `bridge_event_kind_mismatch` on stderr.
- Event-specific fields per the Claude hooks reference.

The relay MUST NOT depend on Claude payload fields not enumerated in this section. Fields beyond the contract are ignored (additive-only forward compatibility).

Tags: mechanism

### 4.5 Hook → progress-message mapping

#### CHB-013 — Mapping table

| Claude hook event | Translates to progress-stream message | Derivation rules |
|---|---|---|
| `SessionStart {source: startup}` | (no-op at MVH; ready-state is handler-emitted per §4.7) | — |
| `SessionStart {source: resume}` | (no-op at MVH; ready-state is handler-emitted per §4.7) | — |
| `Stop` | `outcome_emitted` | `kind = WORK_COMPLETE` if phase ∈ {single, implementer-initial, implementer-resume}; `kind = REVIEWER_VERDICT` if phase = reviewer. For reviewer, payload is read from `${HARMONIK_WORKSPACE_PATH}/.harmonik/review.json` per §4.5.CHB-014. For implementer, payload is `{summary: <Claude's final assistant message text, truncated to 4 KiB>}`. The relay emits `outcome_emitted` on EVERY Stop invocation without filtering; in a multi-turn session multiple `outcome_emitted` messages are delivered. The daemon watcher applies last-received-wins dedup per §4.10 CHB-025. |
| `SessionEnd` | (no-op; the handler emits `agent_completed` on Wait-return per §4.7) | — |
| `StopFailure {error_type: rate_limit}` | `agent_rate_limited` | `retry_after_seconds = 60` (synthesized constant at MVH; no Claude-provided retry-after available). `agent_rate_limited` is non-terminal per [event-model.md §8.3]. |
| `StopFailure {error_type ∈ {authentication_failed, oauth_org_not_allowed, billing_error, invalid_request, max_output_tokens, unknown}}` | `outcome_emitted{kind = FAILURE_SIGNAL}` | `payload.error_type = "claude_" + error_type`; `payload.sub_reason = "claude_" + error_type`; `payload.suggested_class = ErrStructural`. The relay MUST NOT emit `agent_failed`; per CHB-INV-002 the relay never emits terminal events. The handler-process consumes `outcome_emitted{kind = FAILURE_SIGNAL}` on Wait-return and emits the single terminal `agent_failed` per §4.7 CHB-020 carrying the suggested class. |
| `StopFailure {error_type: server_error}` | `outcome_emitted{kind = FAILURE_SIGNAL}` | `payload.error_type = "claude_server_error"`; `payload.sub_reason = "claude_server_error"`; `payload.suggested_class = ErrTransient`. Handler-process maps to terminal `agent_failed` per §4.7 CHB-020. |
| `Notification {notification_type ∈ {idle_prompt, permission_prompt}}` | `agent_heartbeat` | `phase = "waiting_input"` |
| `Notification {other notification_type}` | `agent_heartbeat` | `phase = "reasoning"` |

Tags: mechanism

#### CHB-014 — Reviewer verdict file read

For `phase = reviewer` Stop hook, the relay MUST read `${HARMONIK_WORKSPACE_PATH}/.harmonik/review.json`. The file MUST conform to the agent-reviewer JSON verdict schema v1 per [workspace-model.md §4.7 WM-027a] and [event-model.md §8.1a]. The relay MUST validate `schema_version = 1`, `verdict ∈ {APPROVE, REQUEST_CHANGES, BLOCK}`, `flags` is a string array, `notes` is a string. On validation success, the relay packages the four fields into the `outcome_emitted` payload's `verdict` sub-field. On validation failure or file-absent, the relay packages `{error: "missing_review_file" | "malformed_review_file"}` into the payload's `error` sub-field; the daemon's review-loop dispatcher routes the run to `review_loop_cycle_complete{completion_reason=error}` per the existing event-model rule.

Tags: mechanism

### 4.6 Daemon-socket protocol

#### CHB-015 — One-shot NDJSON connection regime

Per [handler-contract.md §4.10 HC-045b], each `hook-relay` invocation opens a short-lived Unix domain socket connection to `${HARMONIK_DAEMON_SOCKET}`. The relay MUST:

1. Dial with a 5-second timeout.
2. Write exactly one NDJSON line (the constructed progress-stream message), terminated by `\n` (0x0A), with byte length ≤ 1 MiB per [handler-contract.md §4.2 HC-007a].
3. Read back at most one NDJSON line of acknowledgment or typed-error response within a 5-second deadline.
4. Close the connection.

The relay's per-message envelope MUST carry both `run_id` (= HARMONIK_RUN_ID) and `claude_session_id` (= HARMONIK_CLAUDE_SESSION_ID) at the top level so the daemon's connection acceptor can route to the correct session-bound watcher.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent

#### CHB-016 — Daemon-not-ready retry

On a `daemon_not_ready{reason=unknown_run_id}` typed-error response (per [process-lifecycle.md §4.2 PL-003b] and [handler-contract.md §4.3 HC-016a]), the relay MUST retry with exponential backoff (base 100 ms, doubling per attempt, max 2 s per attempt), capped at 25 seconds total wall-clock so the retry fits inside the 30-second hook timeout (§4.1.CHB-003). On cap exhaustion, the relay MUST exit 1 with `bridge_daemon_startup_window_exceeded` on stderr.

Tags: mechanism

#### CHB-017 — Exit codes

The relay MUST exit 0 iff the progress-stream message was acknowledged by the daemon. The relay MUST exit 1 on any unrecoverable failure (dial timeout, message rejected, retry exhausted, malformed input, env-var mismatch). The relay MUST NOT exit with codes other than 0 and 1. Diagnostic detail goes to stderr; Claude treats non-zero exit as non-blocking (continues the session per the hook-event-non-blocking rules in [hooks reference]).

Tags: mechanism

#### CHB-023 — Daemon-side `claude_session_id` durability before Claude exec

The daemon MUST persist `claude_session_id` into `Run.context.claude_session_id` (per [execution-model.md §4.3 EM-012, EM-015d]) on receipt of the handler's `handler_capabilities` progress-stream message, BEFORE returning the connection-accept ACK that gates the handler's `claude --session-id <uuid>` exec. The persistence MUST be backed by a checkpoint-commit-class durability boundary: the daemon MUST land a `transition_event` with the updated `context.claude_session_id` to git (per [execution-model.md §4.5 EM-023a]) before the handler is permitted to exec Claude. A mid-launch crash MUST therefore find either (a) no session_id persisted (handler had not yet emitted `handler_capabilities` — safe to re-launch under a fresh UUIDv7) or (b) session_id durably committed (safe to `--resume`). Storage in JSONL, on the bead, or in-memory only is forbidden; `Run.context` (git-backed) is the sole durable home.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent

#### CHB-026 — Concurrent-connection serialization rule

For every claude-code session the daemon socket acceptor receives messages from N+1 concurrent connections in the general case: one long-lived handler connection and N one-shot relay connections (one per hook-relay invocation). The serialization rule governing how the watcher merges these streams is:

**Rule: per-connection FIFO, across-connection unordered.**

Formally:

1. **Within a single connection** the watcher MUST process messages in the order they arrive on that connection (FIFO). The handler connection is single-producer and sequential; each relay connection carries exactly one message. Per-connection FIFO is therefore trivially satisfied by any single-goroutine read loop per accepted connection.

2. **Across distinct connections** the watcher MUST NOT impose or guarantee any ordering. Two messages arriving on different connections at approximately the same wall-clock instant MAY be observed in either order by downstream subscribers. Subscribers (bus consumers, event-model state machines) MUST be written to tolerate any arrival order of messages from distinct connections.

3. **No cross-connection reordering by `emitted_at_ns`** is performed at MVH. `emitted_at_ns` is a monotonic-relative timestamp recorded by the emitter (handler or relay) for observability and replay purposes; the daemon's acceptor MUST NOT buffer messages from one connection while waiting to sort them against messages from another connection.

**Rationale:** the current socket acceptor (per `internal/daemon/socket.go RunSocketListener`) dispatches each accepted connection to an independent goroutine with no cross-goroutine ordering gate. This is Rule (C). Rules (A) and (B) would require a shared ordered channel or a reorder buffer with a quiescence window, both of which introduce complexity and latency that are unnecessary at MVH. In practice, concurrent relay arrivals are rare: Claude's hook execution model fires hooks sequentially relative to the agent's tool invocations; the handler's long-lived connection carries low-frequency lifecycle messages. When relay messages genuinely race (e.g., two `Notification` events from a parallelized tool call), both orderings are semantically equivalent to the subscriber.

**Twin parity implication:** `harmonik-twin-claude` emits all messages on a single connection (it does not spawn relay subprocesses). A twin run therefore trivially satisfies per-connection FIFO with no cross-connection ambiguity. CHB-021 byte-for-byte parity holds within each connection's ordered stream; the across-connection ordering variance that exists in real runs is absent in twin runs, which is consistent with declaring across-connection order as unspecified. Conformance tests MUST NOT assert a fixed cross-connection emission order; they MUST instead assert that each expected message is present and that per-connection order constraints hold.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

#### CHB-027 — Daemon behavior on abrupt EOF before envelope arrival

If a relay subprocess is killed (SIGKILL, OOM, or any other cause) AFTER opening the daemon socket connection but BEFORE writing a complete NDJSON envelope line, the daemon receives an abrupt EOF on that connection without any `run_id` or `claude_session_id` routing key.

The daemon MUST:

1. **Drop the connection silently.** No bus event is emitted. No terminal event (`agent_failed`, `agent_completed`) is emitted for any session. The connection is unidentifiable and cannot be attributed to any watcher.
2. **Log a single debug-level line** noting the orphan connection (remote address, byte count received, timestamp). No structured event is emitted at MVH; the log line is for operator diagnostics only.
3. **Leave the handler's Wait-return path as the authoritative recovery mechanism.** Per CHB-020 and CHB-INV-002, the handler-process emits the terminal event when `cmd.Wait()` returns. If the relay was carrying a Stop hook and died mid-write, no `outcome_emitted` arrives at the daemon; the watcher's "no outcome_emitted observed" branch fires on Wait-return, emitting `agent_failed{class=ErrTransient, sub_reason=bridge_partial_write}` per §8. This is a recoverable false-negative: the terminal event is correctly emitted with an accurate sub_reason; orchestrator retry semantics apply.

**Scope boundary.** This requirement covers only connection-level EOF before any byte of the envelope reaches the daemon. Partial envelope receipt (some bytes written, no `\n` terminator) is handled by the existing wire-framing rule: the NDJSON reader discards the incomplete line and treats the connection as EOF with zero complete messages, which falls under this same rule.

**Implementation note.** The current `RunSocketListener` (per `internal/daemon/socket.go`) dispatches each accepted connection to a goroutine that reads until EOF before routing; a connection that yields zero complete lines is already discarded without routing. This requirement makes that behavior normative. No code change is required; conformance testing MUST verify the silent-drop behavior (see §10).

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

### 4.7 Handler-process responsibilities

#### CHB-018 — Pre-Claude-exec emission ordering

The harmonik handler-process MUST emit the following progress-stream messages to the daemon, in this order, BEFORE invoking `claude --session-id <uuid>` or `claude --resume <uuid>`, satisfying [handler-contract.md §5 HC-INV-004]:

1. `handler_capabilities` carrying wire-protocol version 1 AND `claude_session_id` payload field.
2. `session_log_location` carrying `log_path = ~/.claude/projects/<slug>/<claude_session_id>.jsonl` (or platform equivalent — derivation rule deferred to impl, but path MUST be the Claude transcript path).
3. `skills_provisioned` after the handler has provisioned required skills per [handler-contract.md §4.11].
4. `agent_ready` carrying `session_id = HARMONIK_HANDLER_SESSION_ID` and `capabilities[]`.

The handler-process is the SOLE emitter of these four messages for a claude-code session; the relay MUST NEVER emit any of them.

Tags: mechanism

#### CHB-019 — Timer-driven heartbeat emission

While Claude is alive (the handler's `cmd.Wait()` has not returned and `outcome_emitted` has not been observed), the handler-process MUST emit `agent_heartbeat{phase: "reasoning"}` at intervals of `T_silent_hang / 2 = 300 seconds` (per [handler-contract.md §4.6 HC-026a]). Heartbeats emitted by the relay (Notification-driven, §4.5.CHB-013) supplement, not replace, the handler's timer.

> NOTE (cross-ref HC-057): For `agent_type = claude-code` at MVH, the daemon MAY emit `agent_heartbeat` on the handler-process's behalf per [handler-contract.md §4.6 HC-057] (daemon-side heartbeat carve-out); such daemon-emitted heartbeats satisfy this requirement without constituting a protocol violation.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

#### CHB-020 — Terminal-event emission on Wait-return

On `cmd.Wait()` return for the Claude subprocess, the handler-process MUST:

- If an `outcome_emitted{kind ∈ {WORK_COMPLETE, REVIEWER_VERDICT}}` message has been observed (from a Stop hook relay invocation per §4.5 CHB-013) AND `Wait()` returned during the post-outcome shutdown window per [handler-contract.md §4.2 HC-008a]: emit `agent_completed`. Non-zero exit during the shutdown window is recorded via `shutdown_exit_code` payload field per HC-008a's dirty-exit clause.
- If an `outcome_emitted{kind = FAILURE_SIGNAL}` message has been observed (from a StopFailure hook relay invocation per §4.5 CHB-013): emit `agent_failed` per [handler-contract.md §4.6 HC-024], carrying `class = payload.suggested_class` and `sub_reason = payload.sub_reason` from the FAILURE_SIGNAL payload.
- If no `outcome_emitted` was observed: emit `agent_failed` per [handler-contract.md §4.6 HC-024]. Class derivation: exit code 0 → `ErrStructural` with sub_reason `claude_exit_without_outcome` (the agent shut down without producing a verdict — structural defect); non-zero exit code → `ErrStructural` with sub_reason `claude_crashed`.

Per [handler-contract.md §5 HC-INV-006], exactly one terminal event per session MUST be emitted; the handler-process is the sole emitter of the terminal event.

Tags: mechanism

### 4.8 Twin parity

#### CHB-021 — Twin emits the same wire format

The twin binary `harmonik-twin-claude` MUST be capable of emitting the same NDJSON progress-stream sequence (handler-emitted messages + relay-emitted messages, in correct order) that a real Claude run would produce, WITHOUT spawning a `harmonik hook-relay` subprocess and WITHOUT materializing `.claude/settings.json`. Scenario scripts drive the twin per the scripted-heartbeat carve-out in [handler-contract.md §4.6 HC-026a].

Tags: mechanism

#### CHB-022 — Daemon is twin-blind

The daemon's watcher acceptor MUST route messages from real-Claude (via relay) and from twin sessions identically. The daemon-side code MUST carry zero `if isTwin` / `if relay` branches per [handler-contract.md §5 HC-INV-002]. The (run_id, claude_session_id) envelope is the only session-routing key.

Tags: mechanism

### 4.9 Settings-precedence verification

#### CHB-024 — Startup verification that bridge hooks are not shadowed

The harmonik handler-process MUST perform a startup-verification check before exec'ing Claude that confirms the bridge-required hook entries are reachable through Claude's settings hierarchy. Specifically: if `${workspace_path}/.claude/settings.local.json` exists, the handler MUST parse it and verify that (a) it does NOT contain `disableAllHooks: true`, and (b) it does NOT define a `hooks` block whose precedence rules in Claude's settings hierarchy would silently shadow the bridge-required entries written in `${workspace_path}/.claude/settings.json`. On verification failure, the handler MUST NOT exec Claude; it MUST emit `agent_failed` with `class = ErrStructural` and `sub_reason = "bridge_settings_shadowed"` per [handler-contract.md §4.6 HC-024].

This check protects against silent bridging failure when a user-managed `.claude/settings.local.json` (which takes precedence over `.claude/settings.json` per Claude's settings hierarchy) disables or overrides the bridge's hooks. The remediation is operator-side: remove the conflicting `settings.local.json` content or move the user-managed hooks into the merge-capable `.claude/settings.json` path per §4.1 CHB-004.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

### 4.10 Stop-hook dedup gate

#### CHB-025 — Daemon last-received-wins dedup for `outcome_emitted` (option b)

**Context.** Claude's Stop hook fires once per turn-loop completion, not once per session. In a multi-turn session the relay therefore emits one `outcome_emitted` message per completed turn (each with `kind ∈ {WORK_COMPLETE, REVIEWER_VERDICT, FAILURE_SIGNAL}`). The relay MUST NOT attempt to suppress intermediate Stop deliveries via transcript inspection or local state (relay-side gate is rejected; see rationale below).

**Rule.** The daemon's per-session watcher MUST accept every `outcome_emitted` message keyed by `(run_id, claude_session_id)` and replace its in-memory "current outcome" with the most-recently-received value. When `cmd.Wait()` returns for the Claude subprocess (per §4.7 CHB-020), the watcher uses the LAST `outcome_emitted` received as the authoritative outcome for the terminal-event derivation.

**Boundary.** The "last-received-wins" replacement is bounded to the open session window: from the first `outcome_emitted` for a given `(run_id, claude_session_id)` until `cmd.Wait()` returns. After `cmd.Wait()` returns the session window is closed; any stale relay messages that arrive late (due to OS scheduling) are silently dropped by the daemon using the existing `unknown_session` typed-error path per §6.2.

**Why last, not first.** The semantically correct outcome is the one Claude commits to immediately before exiting. In a multi-turn session Claude may complete an intermediate turn (emitting WORK_COMPLETE) and then continue work in response to a follow-up; the final Stop before exit is the ground truth. Accepting only the first `outcome_emitted` would misclassify intermediate-turn stops as the session result.

**Why relay does not gate (option a rejected).** A relay-side gate would require the relay to inspect Claude's transcript file to determine whether the current Stop is the "final" one. This couples the relay to the Claude transcript JSONL format — an interface Claude Code does not formally contract, subject to schema drift, and not available for inspection until after Claude writes it (race-prone). The relay's invariant per CHB-INV-003 is that it derives outputs deterministically from `(stdin payload, env, on-disk artifacts)` without coupling to undocumented internals; transcript inspection would violate this. The daemon already owns per-session state and is the natural home for last-wins replacement logic.

**Implementation note.** The daemon watcher MUST update a single `latestOutcome *OutcomeEmittedPayload` field (or equivalent) on the per-session struct on each `outcome_emitted` receipt, protected by the watcher's existing serialization discipline (the watcher goroutine owns all writes). No additional locking surface is needed. A follow-up implementation bead exists for this (see below).

**Follow-up bead.** Daemon-side implementation of CHB-025 (the last-received-wins watcher field + drop-after-close logic) is tracked as a separate implementation bead filed under parent `hk-w5vra` with labels `next-init,claude-adapter-real`. The spec is normative immediately; code is deferred to that bead.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

### 4.11 Per-launch task artifact

#### CHB-028 — `agent-task.md` as the daemon-to-claude task-delivery file

Tags: mechanism

**Purpose.** The daemon MUST materialize a task artifact at `${workspace_path}/.harmonik/agent-task.md` before launching any `claude-code` session. This file is the normative daemon→claude task-delivery channel under the tmux substrate (per [process-lifecycle.md §4.7 PL-021b]). It is the mechanism by which harmonik hands an implementer or reviewer a concrete unit of work before exec'ing Claude.

**Reserved name.** The filename `agent-task.md` under `${workspace_path}/.harmonik/` is reserved for harmonik's exclusive use. No user-authored file, no operator-side config, and no agent-written output MAY occupy this path. The daemon owns writes to the path. Each launch overwrites the file with content for the caller's `(run_id, phase, iteration)` tuple; review-loop phase transitions (impl → reviewer → impl-resume) reuse the same worktree and each is its own logical launch — overwrite is the expected behavior, not an error. (Amended 2026-05-13: original CHB-028 treated a pre-existing file as `task_file_collision` `ErrStructural`; that semantic was a spec-author oversight that did not accommodate review-loop phase transitions and is hereby retracted. See §10 revision history.)

**NOT `.claude/CLAUDE.md` or `CLAUDE.md`.** The task artifact MUST NOT be written to `CLAUDE.md`, `.claude/CLAUDE.md`, or any path that Claude Code's settings-hierarchy auto-discovery would treat as a global system prompt. The operator's existing `CLAUDE.md` in the worktree MUST remain unmodified. `agent-task.md` is a per-launch sidecar; Claude is expected to read it as an ordinary file, directed by a pane-paste kick-off message (per the mechanism defined in the forthcoming B2 + B8 spec amendments).

**Materialization timing.** The daemon MUST write `agent-task.md` AFTER [workspace-model.md §4.1 WM-003] (worktree creation) and BEFORE exec'ing Claude via the tmux substrate. The write ordering relative to `.claude/settings.json` materialization (CHB-002) is: both MUST be complete and fsynced before the tmux substrate receives the `SubstrateSpawn` call. The parent directory `${workspace_path}/.harmonik/` is created (if absent) as part of the workspace-creation step; `agent-task.md` does not introduce a new directory.

**Atomic-write discipline.** The write MUST follow the same atomic discipline as [workspace-model.md §4.7 WM-026]:

1. Construct the full file content in memory.
2. Write to a sibling temp file at `${workspace_path}/.harmonik/agent-task.tmp-<pid>`.
3. `fsync(2)` the temp file.
4. `rename(2)` the temp file to `${workspace_path}/.harmonik/agent-task.md` (POSIX rename is atomic).
5. `fsync(2)` the parent directory `${workspace_path}/.harmonik/` to durably record the rename.

A power loss after step 4 without step 5 MUST NOT leave a partial or missing task file visible to Claude. The fsync-parent step is required on all supported platforms (Darwin, Linux).

**Content shape.** The content is a UTF-8 Markdown document. The daemon MUST include all of the following fields in every task file, regardless of phase:

```
# Harmonik Task

bead_id: <HARMONIK_BEAD_ID value, or "none" if not bead-tied>
title: <bead title, or run_id if not bead-tied>
phase: <one of: implementer-initial | implementer-resume | reviewer>
iteration: <integer; 1-based; LaunchSpec.iteration_count>
run_id: <HARMONIK_RUN_ID>
workspace_path: <absolute path to workspace root>

## Task Description

<bead body verbatim, or operator-provided task string if not bead-tied; MUST NOT be empty>

## Prior-Iteration Context

<present only when phase = implementer-resume or phase = reviewer; omitted entirely when phase = implementer-initial>
```

For `phase = implementer-resume`: the Prior-Iteration Context section MUST include the path to the reviewer's verdict file from the immediately preceding iteration: `reviewer-feedback: ${workspace_path}/.harmonik/review.iter-<N-1>.json` where `<N-1>` is the previous iteration ordinal (1-indexed, matching WM-027a archival naming). It SHOULD also include a human-readable summary of the prior verdict's `verdict` field and `notes` string so that Claude does not need to parse JSON to understand its immediate next action.

For `phase = reviewer`: the Prior-Iteration Context section MUST include the base and head commit SHAs for the diff under review: `review_base_sha: <sha>` and `review_head_sha: <sha>`. The daemon derives these from the task-branch tip at reviewer-launch time (HEAD of the task branch after the implementer's last commit) relative to the task-branch fork point (the `parent_commit` field on the Workspace record per WM-026).

**Gitignore hygiene.** `${workspace_path}/.harmonik/agent-task.md` MUST be excluded from checkpoint commits per [workspace-model.md §4.3 WM-013e]. The daemon MUST add the line `.harmonik/agent-task.md` (or the glob `.harmonik/agent-task*`) to the worktree's `.gitignore` set at materialization time, in the same atomic-write pass as the file itself. The task artifact is workflow-control state, not work product; it MUST NOT appear in squash-merge commits per WM-019.

**Re-launch (re-attach) semantics.** If the daemon restarts mid-session and finds an existing `agent-task.md` in the worktree for the same `(run_id, phase, iteration)` tuple it is about to launch, the file is idempotent — the daemon SHOULD return early without re-writing. The re-attach path is identified by the daemon having already persisted `claude_session_id` (CHB-023) and the workspace being in `leased` state with an active session. (Normal review-loop phase transitions are NOT re-attach — they overwrite per the Reserved name clause above.)

**Invariant.** The presence of a durable `agent-task.md` at `${workspace_path}/.harmonik/agent-task.md` is a prerequisite for any Claude exec under the tmux substrate. The daemon MUST assert this file exists and is non-empty after the atomic write and before issuing the `SubstrateSpawn` call. An empty or absent file after the write step is a fatal structural error.

**Session-completion instruction (hk-cmybm).** Every `agent-task.md` MUST include a `## Session Completion` section at the end of the file instructing Claude to run `/quit` after completing and committing the work. This section is non-negotiable for all phases (implementer-initial, implementer-resume, reviewer).

Rationale: in interactive TUI mode, Claude Code's `Stop` hook fires on session exit (`/quit`, Ctrl-C) — NOT after each assistant response. Without `/quit`, the claude process remains alive at the REPL after completing the bead, the Stop hook never fires, `outcome_emitted` is never delivered to the daemon socket, and the workloop's `sess.Wait()` call (tmuxSubstrateSession.runWait polling loop) blocks indefinitely. The daemon's capacity gate jams after the first bead.

The `/quit` is executed by Claude itself (typed and submitted at the REPL), not injected via paste-buffer. This produces a genuine keypress that the TUI key-event handler routes as a slash command; bracketed-paste mode (used for kick-off message delivery per the B8 mechanism) would NOT trigger it.

Cross-ref: CHB-014 (Stop hook → `outcome_emitted` envelope), CHB-025 (daemon dedup), OQ2 resolution in `waitsocketgrace.go`.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent

### 4.12 Worktree auto-trust pre-seed

#### CHB-029 — Pre-seed `~/.claude.json` with worktree trust before exec

Tags: mechanism

**Problem.** When Claude Code starts in a directory it has not seen before, it displays an interactive "Trust this directory?" dialog on the terminal. With no human at the keyboard (daemon-spawned tmux pane), this dialog blocks indefinitely and HC-056 fires.

**Mechanism.** Claude Code persists trust acceptance in `~/.claude.json` under a top-level `"projects"` map keyed by the absolute directory path. The entry shape that suppresses the dialog is:

```json
{
  "projects": {
    "<worktree_path>": {
      "hasTrustDialogAccepted": true
    }
  }
}
```

Harmonik MUST write this entry to `~/.claude.json` before exec'ing Claude in the worktree pane. This is a user-level file (not worktree-scoped). Note: `--permission-mode` remains deny-listed per [handler-contract.md §4.9 HC-055]. `--dangerously-skip-permissions` was previously deny-listed but is now a conditional allow: per [handler-contract.md §4.10 HC-055b], the daemon emits it when the launch CWD canonicalizes to a path under the harmonik worktree root; the `~/.claude.json` trust-seed remains required and complementary (the CLI flag suppresses the interactive dialog; the trust entry is the stable per-worktree record).

**Ordering.** The `~/.claude.json` write MUST occur AFTER WM-003 (worktree creation) and WM-040a (settings.json materialization) and BEFORE `SubstrateSpawn` (the tmux new-window call that starts Claude). The ordering invariant for CHB-028's materialization window — worktree creation → materialize settings + trust → Claude exec — is extended to include the trust write.

**Idempotency.** The write MUST be idempotent: if the entry is already present and `hasTrustDialogAccepted` is already `true`, no write is performed. On re-attach (daemon restart mid-session), the entry is already present and the call is a no-op.

**Atomicity.** The `~/.claude.json` write MUST use an atomic temp-file + rename pattern (same WM-026 discipline as settings.json) to avoid corruption when concurrent daemon instances write to the same user config. The temp file MUST be sibling to `~/.claude.json` (e.g., `~/.claude.json.tmp-<pid>`).

**Failure handling.** If the write fails for any reason (read error, parse error, marshal error, rename error), the daemon MUST NOT exec Claude. The error is surfaced as an `ErrStructural` with `sub_reason: trust_seed_failed` and `agent_failed` is emitted. An un-seeded launch would block rather than hang silently.

**Existing entry preservation.** The write MUST NOT remove or modify any other key in `~/.claude.json` or any other entry in the `projects` map. The merge is additive: only `hasTrustDialogAccepted` is set on the worktree's entry; all other fields in the entry (if pre-existing) are retained.

**Scope note.** This is the user-operator machine running the daemon, not the worktree itself. No worktree-local file is written by this step. The entry persists after the run completes; cleanup is acceptable but not required at MVH (orphaned trust entries are cosmetically inert).

**Test isolation and concurrency.** The implementation MUST honor `HARMONIK_CLAUDE_CONFIG_PATH` (full file path) or `CLAUDE_CONFIG_HOME` (directory; config file is `<dir>/.claude.json`) environment variable overrides so that unit and integration tests can redirect writes to a temp path and never touch the real `~/.claude.json`. Precedence: `HARMONIK_CLAUDE_CONFIG_PATH` > `CLAUDE_CONFIG_HOME` > `~/.claude.json`. The implementation MUST hold a blocking exclusive advisory flock (`LOCK_EX`) on a sidecar lockfile (`<cfgPath>.lock`) across the entire read-modify-write cycle to serialize concurrent daemon instances writing to the same config file. The sidecar pattern is required: locking the target file directly would interfere with the atomic rename. The sidecar MUST be created with `O_CREATE|O_RDWR` (mode `0600`) if it does not yet exist; the lock is released when the file descriptor is closed after the rename completes.

Cross-refs: [workspace-model.md §4.7b WM-040b] (spec side of the same requirement), [handler-contract.md §4.9 HC-055] (flag allow-list; --permission-mode deny-listed), [handler-contract.md §4.10 HC-055b] (worktree path-check for --dangerously-skip-permissions), [handler-contract.md §4.9 HC-056] (agent_ready timeout triggered by blocked trust prompt), [claude-hook-bridge.md §4.2 CHB-007] (forbidden flags).

Tags: mechanism, security-relevant
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

## 5. Invariants

#### CHB-INV-001 — Two-contributor session

For every Claude session, both the harmonik handler-process AND zero-or-more hook-relay subprocesses contribute messages to the daemon socket, all keyed by the same (run_id, claude_session_id) tuple. The watcher MUST treat both contributors as one event stream. This is observable via the daemon's per-watcher connection-accept log.

Tags: mechanism

#### CHB-INV-002 — Single terminal event per session

The handler-process is the sole emitter of `agent_completed` and `agent_failed` for a claude-code session, satisfying [handler-contract.md §5 HC-INV-006]. The relay MUST NEVER emit a terminal event.

Tags: mechanism

#### CHB-INV-003 — Mechanism, no cognition

Every relay-emitted message derives deterministically from (stdin payload, env, on-disk artifacts). No cognition participates. The bridge is mechanism-tagged per [architecture.md §4.2].

Tags: mechanism

## 6. Schemas and data shapes

### 6.1 Hook-relay message envelope

```
RECORD HookRelayMessage:
    type                 : String                  -- progress-stream message type per [handler-contract.md §4.2 HC-007]
    run_id               : UUIDv7 string            -- = HARMONIK_RUN_ID
    claude_session_id    : UUID string             -- = HARMONIK_CLAUDE_SESSION_ID
    handler_session_id   : UUID string             -- = HARMONIK_HANDLER_SESSION_ID (so the daemon can correlate to the handler's progress-stream emissions)
    emitted_at_ns        : Integer                  -- monotonic relative to relay invocation start
    payload              : Object                   -- message-type-specific payload per [event-model.md §6.3]
```

### 6.2 Daemon ACK / typed-error response

```
RECORD HookRelayAck:
    status : Enum { "ok", "daemon_not_ready", "bad_envelope", "unknown_session" }
    reason : String  -- present iff status != "ok"; matches the typed-error contract of [process-lifecycle.md §4.2 PL-003b]
```

## 7. State machines

(None beyond what `[handler-contract.md §7]` already declares. The bridge introduces no new state machine; relay-failure paths reuse HC's silent-hang and terminal-event invariants.)

## 8. Error taxonomy

Bridge-specific failure modes route through `agent_failed{class, sub_reason}` per [handler-contract.md §4.5]:

| Sub-reason | Class | Cause |
|---|---|---|
| `bridge_dial_failed` | ErrTransient | Relay could not connect to daemon socket within 5 s |
| `bridge_daemon_startup_window_exceeded` | ErrTransient | Relay's daemon_not_ready retry budget exhausted |
| `bridge_malformed_hook_payload` | ErrStructural | Stdin JSON malformed or required field missing |
| `bridge_session_id_mismatch` | ErrStructural | Hook stdin's `session_id` does not match `HARMONIK_CLAUDE_SESSION_ID` |
| `bridge_event_kind_mismatch` | ErrStructural | Stdin's `hook_event_name` does not match the relay's argv |
| `claude_exit_without_outcome` | ErrStructural | Claude exited cleanly (0) but no Stop hook fired |
| `claude_crashed` | ErrStructural | Claude exited non-zero without outcome_emitted |
| `claude_<error_type>` | per §4.5.CHB-013 | Mapped from StopFailure.error_type |
| `bridge_partial_write` | ErrTransient | Relay process terminated after socket open but before envelope completion; daemon received EOF on an unidentified connection. Recovery: handler Wait-return CHB-020 "no outcome_emitted" branch fires per §4.6.CHB-027. |
| `bridge_settings_shadowed` | ErrStructural | `.claude/settings.local.json` shadows bridge hooks per §4.9.CHB-024 |
| `task_file_empty` | ErrStructural | `agent-task.md` is absent or empty after atomic write per §4.11.CHB-028 |

## 9. Cross-references

- [handler-contract.md §4.2 HC-007, HC-007a, HC-009] — wire protocol, NDJSON framing, handler_capabilities.
- [handler-contract.md §4.2 HC-006] — LaunchSpec.claude_session_id.
- [handler-contract.md §4.3 HC-016a] — daemon_not_ready retry semantics.
- [handler-contract.md §4.6 HC-024, HC-026, HC-026a] — agent_failed, silent-hang, heartbeat obligation.
- [handler-contract.md §4.10 HC-042, HC-044] — launch path, subprocess parentage, daemon socket.
- [handler-contract.md §5 HC-INV-002, HC-INV-004, HC-INV-006, HC-INV-007] — twin parity, pre-work-dispatch ordering, exactly-one terminal event, watcher as sole publisher.
- [handler-contract.md §4.10 HC-045a] — pointer to this spec for claude-code agent type (new in this kerf).
- [handler-contract.md §4.10 HC-045b] — hook-bridge connection regime (new in this kerf).
- [handler-contract.md §4.10 HC-045c] — handler-side claude_session_id minting/resume discipline (new in this kerf).
- [workspace-model.md §4.1 WM-003] — worktree creation gate.
- [workspace-model.md §4.3 WM-013e] — gitignore hygiene.
- [workspace-model.md §4.4 WM-016] — workspace_leased event ordering.
- [workspace-model.md §4.7 WM-026, WM-027a] — sidecar atomic-write discipline; reviewer verdict artifact path; atomic-write discipline reused verbatim by CHB-028.
- [workspace-model.md §4.7a WM-040a] — claude-code settings.json materialization (new in this kerf).
- [workspace-model.md §4.3 WM-013e] — gitignore hygiene set; CHB-028 `agent-task.md` exclusion uses the same mechanism.
- [process-lifecycle.md §4.5 PL-017a] — relay subprocesses as grandchildren of the daemon (new in this kerf).
- [execution-model.md §4.3 EM-012, EM-015d] — Run.context durability for claude_session_id.
- [execution-model.md §4.5 EM-023a] — durable-transition checkpoint-commit class.
- [execution-model.md §4.7 EM-031] — state reconstruction from git checkpoint trail.
- [event-model.md §8.1a] — review-loop cycle events.
- [process-lifecycle.md §4.1, §4.2 PL-003a, PL-003b] — daemon socket layout, wire format, pre-ready typed-error.
- [process-lifecycle.md §4.5 PL-014] — agent subprocess parentage; bridge clarifies grandchild status.

## 10. Conformance

A handler implementation claiming `claude-code` conformance MUST satisfy:

- CHB-001..005 (settings.json materialization).
- CHB-006..007 (env-var schema and forbidden flags).
- CHB-008..009 (claude_session_id flow).
- CHB-010..017 (relay subcommand contract).
- CHB-018..020 (handler-process emission obligations).
- CHB-021..022 (twin parity).
- CHB-023 (daemon-side claude_session_id durability before Claude exec).
- CHB-024 (startup verification that bridge hooks are not shadowed by settings.local.json).
- CHB-025 (daemon last-received-wins dedup for `outcome_emitted` across multi-turn Stop firings).
- CHB-028 (per-launch task artifact: `agent-task.md` materialized atomically before Claude exec).
- CHB-029 (worktree auto-trust: `~/.claude.json` pre-seeded with `hasTrustDialogAccepted: true` before Claude exec).

Scenario tests MUST cover:

- A `single` workflow-mode run against real Claude and against twin: both produce identical progress-stream byte sequences (modulo timestamp fields and Claude's transcript-text payload contents).
- A 3-iteration `review-loop` run: 7 sessions per workspace (initial implementer + per-iteration implementer + per-iteration reviewer); verifies claude_session_id stability across implementer-resume launches and freshness across reviewer launches.
- A relay-can't-dial scenario: daemon socket file deleted mid-session; relay emits `bridge_dial_failed`; handler's Wait-return emits the terminal event.
- A daemon-not-ready scenario: relay invoked during daemon startup window; retries succeed within the 25 s budget.

## 11. Informative — alternative architecture (post-MVH)

Claude Code supports a `--output-format stream-json --include-hook-events --include-partial-messages` mode that emits hook lifecycle events natively on stdout as NDJSON. This is a competing bridging architecture in which the harmonik handler-process parses Claude's stdout directly, eliminating `.claude/settings.json` materialization and the relay subprocess entirely.

This architecture is NOT adopted at MVH for the following reasons:

- The stream-json event vocabulary is less documented than the hooks reference; the field schemas are subject to evolution.
- The relay-subprocess pattern is simpler to test in isolation (canned stdin / env per invocation).
- The kickoff-design constraint A2 specifies the relay path.

Post-MVH evolution to stream-json + `--include-hook-events` is possible without changing the watcher (per [handler-contract.md §4.12 HC-052]); the adapter and the bridge spec change, the wire-level invariants do not.

## 12. Open questions

- **OQ-CHB-001** — Should the relay's daemon-socket protocol use a SO_PEERCRED check (Linux) / LOCAL_PEERCRED check (macOS) to verify it's running as the same user as the daemon? Default deferred to filesystem-permission discipline per HC-044 at MVH.
- **OQ-CHB-002** — Should `Notification {notification_type: idle_prompt}` synthesize an additional `agent_output_chunk` carrying the notification message text for operator-side observability? Default no at MVH.
- **OQ-CHB-003** — Settings.json merge: should the existence of conflicting user hooks (same event, same matcher, side-effect-bearing command) be detected and warned? Default no at MVH; relies on user discipline.

## Revision history

| Date | Version | Author | Change |
|---|---|---|---|
| 2026-05-12 | 0.1 | foundation-author | Initial draft from kerf `claude-hook-bridge`. |
| 2026-05-13 | 0.2 | agent (hk-w5vra.8) | CHB-025: Stop-hook dedup gate — daemon last-received-wins for `outcome_emitted`; relay-side gate (option a) rejected; §4.5 CHB-013 Stop row updated to reference CHB-025; §4.10 added; conformance updated. |
| 2026-05-13 | 0.3 | agent (hk-w5vra.10) | CHB-026: concurrent-connection serialization rule — per-connection FIFO, across-connection unordered (Rule C). Matches current `RunSocketListener` topology; no code change required. Twin-parity implication added. |
| 2026-05-13 | 0.4 | agent (hk-w5vra.9) | CHB-027: daemon silent-drop on orphan relay connection (relay OOM-killed after socket open, before envelope write). §8 error taxonomy entry added: `bridge_partial_write` (ErrTransient). Doc-only; no code change required. |
| 2026-05-13 | 0.5 | agent (hk-gql20.4) | Decision record: no CHB-028 clause filed. The bridge-integration initiative (`hk-gql20`) evaluated whether the tmux substrate required an amendment to CHB. Conclusion: no amendment is needed. Twin parity remains at the wire-format level (CHB-021 and CHB-022 are unchanged); the substrate — tmux panes as the execution environment for Claude subprocesses — is orthogonal to the hook-relay and progress-stream contracts defined here. Tmux substrate obligations are captured in [workspace-model.md WM-002a] and [handler-contract.md HC-054]. Source: `.kerf/projects/gregberns-harmonik/bridge-integration/05-specs/claude-hook-bridge-amendments.md`. |
| 2026-05-13 | 0.6 | agent (hk-gql20.25) | Bridge-integration spec review findings (MINOR + MEDIUM). **MINOR:** v0.5 changelog citation corrected: `HC-026a` → `HC-054` (Session.Attach pty contract; the prior cite was the heartbeat obligation, not the pty contract intended). **MEDIUM (CHB-019):** Added cross-reference note to §4.7 CHB-019 stating that for `agent_type=claude-code` at MVH the daemon MAY emit `agent_heartbeat` on the handler-process's behalf per HC-057; daemon-emitted heartbeats satisfy CHB-019 without constituting a protocol violation. Refs: hk-gql20.25. |
| 2026-05-13 | 0.7 | agent (hk-yrplz) | CHB-028: per-launch task artifact — `${workspace_path}/.harmonik/agent-task.md` as the normative daemon→claude task-delivery channel under the tmux substrate. §4.11 added. Atomic-write discipline per WM-026. Reserved name (NOT CLAUDE.md). Content shape by phase (implementer-initial, implementer-resume, reviewer). Prior-iteration pointers for resume and reviewer phases. Gitignore hygiene, re-attach semantics, and task_file_collision / task_file_empty error sub-reasons added to §8. §2.1 scope and §9 cross-references updated. Conformance checklist updated. Refs: hk-yrplz. |
| 2026-05-13 | 0.8 | agent (hk-p63bz) | **agent_ready semantics reframed for the interactive (tmux) substrate.** CHB-013: `SessionStart {source: startup}` and `SessionStart {source: resume}` rows updated — relay now synthesizes `agent_ready` (with `provenance: "claude_session_start"`) on first hook receipt rather than being a no-op. This makes `agent_ready` a claude-originated signal rather than a daemon self-emission. CHB-018: step 4 changed from `agent_ready` self-emission to `launch_initiated` precursor; added normative rationale explaining that `agent_ready` is gated on relay receipt under the tmux substrate. §2.1 scope bullet updated to reflect `launch_initiated` (not `agent_ready`) as the pre-exec handler emission. Coexists with CHB-028 (task artifact, hk-yrplz). Refs: hk-p63bz. |
| 2026-05-13 | 0.9 | agent (hk-fdyip) | **CHB-029: worktree auto-trust pre-seed.** §4.12 added. The daemon MUST pre-seed `~/.claude.json` projects[worktreePath].hasTrustDialogAccepted=true before exec'ing Claude in a daemon-spawned tmux pane; without this the interactive trust dialog blocks indefinitely and HC-056 fires. Mechanism: atomic temp-file+rename write to user-level ~/.claude.json. Ordering: after WM-003 + WM-040a, before SubstrateSpawn. Failure: ErrStructural / trust_seed_failed / agent_failed. Alternatives rejected: --permission-mode and --dangerously-skip-permissions are deny-listed in HC-055 and CHB-007. Companion: workspace-model.md §4.7b WM-040b. Code: internal/workspace/claudetrust_wm040b.go. Refs: hk-fdyip. |
| 2026-05-13 | 0.9.1 | orchestrator | **CHB-028 amendment: each launch overwrites `agent-task.md`.** Original 0.7 text treated a pre-existing file as `task_file_collision` `ErrStructural`. This was a spec-author oversight: review-loop phase transitions (impl → reviewer → impl-resume) reuse the same worktree and each is its own logical launch, so overwrite is the expected behavior. §4.11 Reserved-name and Re-launch-semantics paragraphs rewritten; `task_file_collision` row removed from §8 error taxonomy. Re-attach idempotency retained (same `(run_id, phase, iteration)` short-circuit). Surfaced by TestReviewLoop_HappyPath_APPROVE failure post-hk-9ow36 merge. |
| 2026-05-14 | 0.9.2 | agent (hk-53y35, hk-rf4ux) | **WM-040a amendment: permissions.allow pre-authorization (hk-53y35).** `MaterializeClaudeSettings` now emits a top-level `"permissions": {"allow": [...]}` array with the standard Claude Code tool set so per-tool confirmation dialogs do not block unattended daemon operation. `dangerouslySkipPermissions` remains deny-listed per CHB-007 / HC-055. Cross-ref: workspace-model.md §4.7a WM-040a. **Splash-race defeat (hk-rf4ux — Option B).** `tmuxSubstrate` now implements the `enterSender` interface: `SendEnterToLastPane` issues `tmux send-keys -t <pane> Enter` (the tmux key-name, NOT `-l` literal) before the paste-buffer write, dismissing the Claude Code welcome splash. The splash is a React/ink TUI that processes key events via the key-event path; `paste-buffer` operates in bracketed-paste mode where `\n` bytes are NOT dispatched as Enter keypresses. A 750ms delay (`splashDismissDelay`) between the Enter and the paste allows the splash animation to complete and the REPL to become active. New `SendKeysEnter` method added to `tmux.Adapter` and `OSAdapter`; `enterSender` interface added to `pasteinject.go`. Code: internal/daemon/pasteinject.go, internal/daemon/tmuxsubstrate.go, internal/lifecycle/tmux/osadapter.go. |
| 2026-05-15 | 1.1 | agent (hk-fdyip) | **CHB-029 amendment: `--dangerously-skip-permissions` carveout.** §4.12 CHB-029 prose updated to reflect that `--dangerously-skip-permissions` is now a conditional allow per [handler-contract.md §4.10 HC-055b]: the daemon emits it when the launch CWD canonicalizes to a path under the harmonik worktree root. The `~/.claude.json` trust-seed (CHB-029 core mechanism) remains required and complementary. Cross-refs updated to point at HC-055b. Refs: hk-fdyip. |
| 2026-05-14 | 1.0 | agent (hk-cmybm) | **CHB-028 amendment: session-completion instruction.** Every `agent-task.md` MUST include a `## Session Completion` section instructing Claude to run `/quit` after completing and committing the work. Root cause (hk-cmybm): in interactive TUI mode, Stop hook fires only on session exit — not after each assistant response. Without `/quit`, the tmuxSubstrateSession polling loop (`runWait`) never observes process exit, `sess.Wait()` blocks indefinitely, and the capacity gate jams. The `/quit` is executed by Claude itself (typed at the REPL), not injected via paste-buffer; this generates a real keypress that the TUI dispatches as a slash command. §4.11 CHB-028 augmented with session-completion-instruction paragraph (rationale + cross-refs). Code: `internal/workspace/agenttask_chb028.go` (`buildAgentTaskContent`). Test: `TestCHB028_SessionCompletionInstruction` (all three phases). Smoke v12: OPERATIONAL GREEN (bead_closed confirmed in events.jsonl, sess.Wait returns, capacity gate unblocked). |
