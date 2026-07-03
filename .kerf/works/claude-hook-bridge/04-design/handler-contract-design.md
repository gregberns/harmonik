# Change Design — handler-contract.md amendment

## Current state

`handler-contract.md` v0.3.3 covers the wire protocol, concurrency model, error taxonomy, secrets, twin parity, ready-state detection, agent-to-orchestrator trust (§4.10), and skill injection. The `claude_session_id` LaunchSpec field is declared in §4.2 HC-006 with PROSE describing the populate-on-resume rule (informative). The handler-side obligation to mint claude_session_id is implied but not normative.

`docs/subsystems/agent-runner.md` line 28 hand-waves at "Configure Claude Code hooks (or equivalent)".

## Target state

Two normative additions under §4.10 Agent-to-orchestrator trust (MVH):

### HC-NEW-1 — Claude-code handler obligations for session_id and resume

For agent_type = "claude-code", the handler MUST observe the following session-id lifecycle:

1. **Phase ∈ {single, implementer-initial, reviewer}**: handler MUST mint a fresh UUIDv7, pass it to Claude via the `--session-id <uuid>` flag, AND populate the same UUID in the env-var `HARMONIK_CLAUDE_SESSION_ID` for inheritance through to the relay subcommand. The minted UUID MUST be reported to the daemon via a payload field on `handler_capabilities` (new field `claude_session_id`, optional, present only for claude-code).
2. **Phase = implementer-resume**: handler MUST reuse `LaunchSpec.claude_session_id` (carried from the prior iteration), pass it via `--resume <claude_session_id>` (NOT `--session-id`), AND set the same in HARMONIK_CLAUDE_SESSION_ID.
3. The handler MUST NOT pass `--fork-session`, `--bare`, or `--no-session-persistence` to Claude. These flags conflict with the bridge's session-stability and hook-discovery requirements.
4. The reviewer phase ALWAYS mints fresh; handler MUST NOT inherit claude_session_id from a prior reviewer launch.

Tagging: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent

### HC-NEW-2 — Pointer to claude-hook-bridge.md

For agent_type = "claude-code", the launch mechanism, settings.json materialization, hook-event-to-progress-message translation, env-var schema, and failure-mode classification are normatively defined by `specs/claude-hook-bridge.md`. This spec (handler-contract) defines the cross-handler invariants; the bridge spec defines the claude-code-specific realization.

Tagging: mechanism

### HC-NEW-3 — One-shot NDJSON connection regime

Amend §4.2 (HC-007a) prose (or add HC-007c) to acknowledge that a session's progress stream MAY include short-lived per-message NDJSON connections in addition to the long-lived bidirectional stream of HC-007. The watcher's connection acceptor MUST treat both connection regimes identically:

- The session is identified by (run_id, claude_session_id) carried in the message envelope.
- One-shot connections write exactly one NDJSON line and close.
- Long-lived connections write many lines and close at session end.
- The HC-007a 1 MiB line cap, partial-message rule (HC-007b), and watcher-emission ordering (HC-INV-007) apply uniformly.

This is required because the claude-hook-bridge's relay invocations are short-lived: each Claude hook fires a fresh `harmonik hook-relay` process which opens a new socket connection.

NOTE: this is the largest cross-cutting change. The reviewer-pass MUST verify it does not contradict HC-007 ("the sole bidirectional channel") or HC-024a (single-socket-lifetime). Re-read of HC-007: it says "the sole bidirectional channel between the daemon and the handler subprocess" — the relay is a SEPARATE subprocess (child of Claude), not the handler subprocess. So the handler's bidirectional stream remains the sole bidirectional channel for the handler. Relay one-shots are a DIFFERENT process's connection, not a second handler connection. **This resolves the apparent contradiction without amending HC-007.** HC-024a's single-socket-lifetime applies to the handler's bidirectional stream, not to per-process incidental connections — also clean.

CONCLUSION: HC-NEW-3 can be a NEW requirement (HC-054 or similar) rather than an amendment to HC-007a. It NAMES the relay-connection regime explicitly so the bridge's design is sanctioned. Move from "amendment" to "new requirement."

Reformulation: **HC-054 — Hook-bridge connection regime.** Some handler subsystems (notably the claude-code bridge per `claude-hook-bridge.md`) cause additional, short-lived subprocesses (the hook-relay subprocess) that open one-shot NDJSON connections to the daemon socket. Such connections MUST carry the session-routing envelope fields (run_id, claude_session_id) in every message; the daemon's watcher acceptor MUST route them to the same per-session bus event flow as the handler's long-lived stream. The relay-subprocess connections are NOT subject to HC-007's "sole bidirectional channel" rule (which scopes to the handler subprocess itself); HC-INV-007's "watcher is sole authoritative publisher" remains satisfied because the watcher publishes ALL connections' messages to the bus.

## Requirements traceability

- HC-NEW-1: bridge D9 (--session-id / --resume / no --fork-session).
- HC-NEW-2: bridge spec as the realization of S05 for claude-code.
- HC-054: bridge D4 (one-shot NDJSON connection regime).

## Rationale

The pre-generated session_id is the load-bearing multi-agent disambiguation mechanism. Promoting it to a normative HC requirement makes the bridge's behavior contract-checked rather than handler-implementation-detail. HC-054 is the only structurally non-trivial change; everything else is glue.
