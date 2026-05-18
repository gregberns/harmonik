# Pass 1 — Problem Space: claude-hook-bridge

## Summary

The daemon-side review-loop dispatcher (`internal/daemon/reviewloop.go`) emits the full §8.1a event surface against twin handlers, but the **handler-subprocess side** of the loop — how a real Claude Code agent invocation translates Claude's native lifecycle into the daemon's `outcome_emitted` signal (and the rest of the §4.2 progress-stream message set) — is structurally specced but mechanically undefined.

`docs/subsystems/agent-runner.md` line 28 currently says *"Configure Claude Code hooks (or equivalent) for the agent's session"* and stops. `internal/handler/adapter_claudecode.go` exists as an MVP value type satisfying the `Adapter` callbacks (`DetectReady`, `DetectRateLimit`, `CleanExitSequence`, `RotateAccount`) but there is **no specification of how the Claude Code subprocess actually delivers `agent_ready`, `outcome_emitted`, `agent_completed`, `agent_failed`, `agent_heartbeat`, `skills_provisioned`, or `session_log_location` over the Unix domain socket at `.harmonik/daemon.sock`**.

This blocks running review-loop against a real Claude Code agent: the twin path works because the twin is a harmonik-authored binary that speaks the wire protocol directly; the Claude path does not, because Claude Code does not natively speak harmonik's wire protocol.

## What changes after this work

A normative spec — **`specs/claude-hook-bridge.md`** (new spec; prefix `CHB`) — plus targeted amendments to four existing specs and one subsystem doc, defines a deterministic translation layer that lets the Claude Code CLI satisfy `handler-contract.md` without modifying Claude. The translation has three load-bearing parts:

1. **Settings.json + env-var schema**, materialized into the workspace's `.claude/` directory at workspace creation, carrying static per-launch context (run_id, workspace_path, daemon_socket_path, workflow_mode, phase, iteration_count, claude_session_id, harmonik_session_id).
2. **A relay subcommand of the main `harmonik` binary** (`harmonik hook-relay`) invoked from `.claude/settings.json` `command` slots; it reads Claude's per-event JSON-on-stdin, combines it with the static env-var context, and posts a structured `progress_stream` message to `.harmonik/daemon.sock`. The relay is the one and only translator between Claude's hook payload schema and harmonik's NDJSON progress-stream schema.
3. **A pre-generated `claude_session_id` flow** so that the handler subprocess (not Claude) chooses the session UUID, passes it to Claude via `--session-id <uuid>`, AND reports it to the daemon at watcher-bind time. Hook payloads echo this UUID back, so the daemon routes hook-derived events to the correct watcher even when N agents share a workspace (post-MVH multi-agent) or when the same workspace runs sequential implementer/reviewer launches in the review-loop cycle.

## Goals

- **G1.** Real Claude Code agent runs review-loop end-to-end against `internal/daemon/reviewloop.go` with no test branches in the daemon. Verified by an integration test that the twin currently passes; the only diff is the binary launched.
- **G2.** Zero new event types in `event-model.md` (the bridge maps Claude hook events INTO existing `outcome_emitted` / `agent_completed` / `agent_failed` / `agent_heartbeat` / `skills_provisioned`). One new ERROR event MAY be allowed if relay→socket transport itself fails in a way the existing taxonomy can't carry; design pass decides.
- **G3.** Twin-binary parity preserved (HC-INV-002). The relay surface MUST be the same shape the twin can fake deterministically; the daemon code carries zero `if isClaude` branches.
- **G4.** Reuse silent-hang (HC-026, 600s default) and exactly-one-terminal-event-per-session (HC-INV-006) invariants for relay failure modes — no parallel taxonomy.
- **G5.** `phase = implementer-resume` is mechanical: the handler stores the prior `claude_session_id` for the run and re-launches Claude with `--resume <id>`.
- **G6.** Settings.json + relay invocation are themselves crash-safe: the workspace materialization step that writes them is folded into the WM-016 atomic-write discipline (temp + rename + fsync(parent)).

## Non-goals

- **NG1.** Cross-agent-type generalization. This spec covers Claude Code specifically. A future "pi-code-bridge" or other agent-type bridge re-implements the same shape; we do NOT introduce a generic "hook bridge" abstraction layer (cf. CLAUDE.md "don't add abstraction layers the user hasn't asked for").
- **NG2.** Replacing the existing `Adapter` interface. `ClaudeCodeAdapter` stays; new mechanism slots in via the watcher's per-session knowledge of the relay socket protocol.
- **NG3.** Authenticating relay→daemon. MVH inherits HC-044's filesystem-permission discipline on the daemon socket; no separate auth for the relay.
- **NG4.** Multi-agent-per-workspace support is a future fit. The pre-generated session_id design must NOT preclude it, but the spec scopes its conformance to 1 agent at a time per workspace (consistent with WM-011).
- **NG5.** Post-outcome shutdown semantics (HC-008a, T_shutdown=10s) — already specced. The bridge inherits, does not modify.

## Constraints

- **C1.** `handler-contract.md` is `reviewed` v0.3.3 with **frozen requirement IDs**. The bridge MUST NOT renumber any HC-NNN ID. New requirements live under HC-054+ (or in the new `claude-hook-bridge.md` spec under `CHB-NNN` prefix; design pass decides).
- **C2.** `workspace-model.md` is `reviewed` v0.4.2. New WM requirement(s) MUST be minted in the WM-Free gap (post-WM-037 or under a new sub-section) without renumbering.
- **C3.** `event-model.md` is `reviewed` v0.3.3. Adding a new event type is allowed but expensive; the design pass MUST justify any addition or close it out.
- **C4.** No daemon-side `if isTwin` / `if agent_type == "claude-code"` branches per HC-INV-002.
- **C5.** Relay subcommand is the smallest reasonable shipping surface. NOT a separate binary unless research surfaces a binary-size or dependency-graph reason.
- **C6.** Claude Code is a system handler (`system_handler=true`, `$PATH`-resolved per HC-042). The bridge MUST cope with the absence of a commit-hash check; the launch trust rests on the operator's `claude --version` log (HC-043).
- **C7.** The relay process is a **child of Claude**, not of the daemon. Per HC-044 the daemon spawns Claude as its child; Claude in turn spawns the relay (via the `command:` slot in settings.json) as ITS child. This is structurally distinct from watcher-goroutine supervision (HC-011) — the relay's lifetime is bounded by Claude's hook invocation, not by the watcher.

## Constraints already agreed in the kickoff prompt

The following are user-fixed decisions to be carried forward, not re-litigated in design:

- **A1.** Settings.json is materialized at workspace creation.
- **A2.** Hook callback shape: shell command from settings.json, JSON on stdin, env from process inheritance. Default relay = subcommand of the main harmonik binary (`harmonik hook-relay <event-kind>`).
- **A3.** Multi-agent disambiguation via pre-generated Claude `--session-id`.
- **A4.** Reuse existing event-model machinery wherever possible.
- **A5.** Twin parity preserved.

These are inputs to the research pass (which verifies they hold against external evidence) and to the design pass (which records them as decisions with rationale).

## Success criteria

After this work, the spec corpus will contain:

- **SC1.** A new normative spec `specs/claude-hook-bridge.md` (CHB prefix) that defines:
  - the `.claude/settings.json` schema harmonik emits
  - the env-var schema agents-spawned-from-settings inherit (HARMONIK_RUN_ID, HARMONIK_DAEMON_SOCKET, HARMONIK_WORKSPACE_PATH, HARMONIK_PHASE, HARMONIK_ITERATION_COUNT, HARMONIK_CLAUDE_SESSION_ID, HARMONIK_HANDLER_SESSION_ID, plus the freedom-profile and secrets fields already specced by HC-028)
  - the relay-stdin-payload schema for each supported Claude hook event (Stop, SessionEnd, Notification, SessionStart, UserPromptSubmit, PreCompact at minimum — research pass confirms the set)
  - the relay→daemon wire format (NDJSON `progress_stream` messages on `.harmonik/daemon.sock`, using the existing HC-007a framing)
  - the mapping table: each Claude hook event → the existing progress-stream message it translates into (with field-by-field derivation rules)
  - the pre-generated session_id flow including how the handler captures it, how it survives the implementer-resume phase, and how it's archived between iterations
  - failure-mode classification reusing HC's sentinel taxonomy (relay-can't-reach-socket → ErrTransient; malformed hook payload → ErrStructural sub-reason `bridge_malformed_hook_payload`; etc.)
  - twin-parity rules: the twin emits the same progress-stream sequence the relay synthesizes from Claude hooks
- **SC2.** Amendments to four existing specs:
  - `workspace-model.md`: new requirement that `.claude/settings.json` is materialized during workspace creation, on the WM-016/WM-026 atomic-write path; teardown disposition (purged with the workspace or preserved with logs, decided in design).
  - `handler-contract.md`: §4.10 extension that names the bridge spec as the normative source for Claude-specific launch mechanics; the pre-generated `claude_session_id` flow becomes a normative HC-side requirement (or §4.2 LaunchSpec extension for the implementer-resume path — already noted in HC-006 prose but not normative).
  - `event-model.md`: no new event types if research confirms the mapping is clean, else exactly one error event for `bridge_unreachable`.
  - `process-lifecycle.md`: small note in §4.5 that hook-relay subprocesses are grandchildren of the daemon and out-of-band of HC-011 watcher supervision; orphan-sweep policy unchanged.
- **SC3.** A new subsection or rewrite of `docs/subsystems/agent-runner.md` line 28 ("Hook attachment") that cites the new spec rather than hand-waving.
- **SC4.** Bead decomposition under `hk-` prefix following `docs/decompose-to-tasks/discipline.md` v0.4; rough sizing 25-40 beads (CHB spec + 4 cross-spec amendments + impl tasks for relay subcommand, settings.json materialization, end-to-end real-Claude integration test).

## Spec areas affected (preliminary)

1. **NEW**: `specs/claude-hook-bridge.md` — central new spec
2. **AMEND**: `specs/workspace-model.md` — §4.7 area (settings.json materialization) and possibly §4.1 (worktree creation includes settings write)
3. **AMEND**: `specs/handler-contract.md` — §4.10 (launch trust) and §4.2 (LaunchSpec.claude_session_id from-handler population path)
4. **AMEND**: `specs/event-model.md` — possibly §8.3 (one error event) or no change
5. **AMEND**: `specs/process-lifecycle.md` — §4.5 (subprocess tree clarification)
6. **AMEND**: `docs/subsystems/agent-runner.md` — replace §28 hand-wave with cite-forward shape
7. **AMEND**: `docs/subsystems/hook-system.md` — note that S05's MVH realization for Claude Code is the bridge spec

The research pass MAY surface additional affected specs (notably reconciliation, if relay-state crash semantics demand it) or MAY collapse some of the above to no-op (notably event-model).

## Confirmation

This problem space is set unilaterally by the agent per the kickoff prompt's "Operate autonomously" directive. No user confirmation step is being inserted; downstream passes record any deviation as discovered.
