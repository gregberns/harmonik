# Claude Hook Bridge

```yaml
---
title: Claude Hook Bridge
spec-id: claude-hook-bridge
requirement-prefix: CHB
status: draft
spec-category: runtime-subsystem
spec-shape: requirements-first
version: 0.4
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
- The handler-process responsibility for emitting `handler_capabilities`, `session_log_location`, `skills_provisioned`, `agent_ready` BEFORE exec'ing Claude (since Claude has no native ready-state distinct from `SessionStart`).
- The handler-process responsibility for emitting timer-driven `agent_heartbeat` while Claude is alive.
- Failure-mode classification: relay-can't-dial-socket, daemon-not-ready-retry-exhausted, malformed hook payload, missing `.harmonik/review.json` at Stop in reviewer phase.
- Twin-parity rules: `harmonik-twin-claude` emits the same wire-format NDJSON sequence the bridge would synthesize from Claude, WITHOUT going through the relay subcommand.

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
- [workspace-model.md §4.7 WM-026, WM-027a] — sidecar atomic-write discipline; reviewer verdict artifact path.
- [workspace-model.md §4.7a WM-040a] — claude-code settings.json materialization (new in this kerf).
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
| 2026-05-13 | 0.5 | agent (hk-gql20.4) | Decision record: no CHB-028 clause filed. The bridge-integration initiative (`hk-gql20`) evaluated whether the tmux substrate required an amendment to CHB. Conclusion: no amendment is needed. Twin parity remains at the wire-format level (CHB-021 and CHB-022 are unchanged); the substrate — tmux panes as the execution environment for Claude subprocesses — is orthogonal to the hook-relay and progress-stream contracts defined here. Tmux substrate obligations are captured in [workspace-model.md WM-002a] and [handler-contract.md HC-026a]. Source: `.kerf/projects/gregberns-harmonik/bridge-integration/05-specs/claude-hook-bridge-amendments.md`. |
