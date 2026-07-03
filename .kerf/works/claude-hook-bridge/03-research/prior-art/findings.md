# Research — Prior art for agent-bridging in the codebase

## In-repo prior art

- `internal/handler/adapter_claudecode.go` (MVP) — declares ClaudeCodeAdapter with DetectReady, DetectRateLimit, CleanExitSequence, RotateAccount. No launch logic. Reads NDJSON event envelope from a yet-to-be-wired progress stream.
- `internal/handlercontract/launchspec_hc006.go` — LaunchSpec record with claude_session_id field already declared.
- `internal/handlercontract/claudesession_wm018.go` — referenced; likely declares the claude_session_id type alias.
- `internal/daemon/launchspecbuild.go` — daemon-side LaunchSpec construction. Will need extension to populate claude_session_id for implementer-resume phase.
- `internal/daemon/reviewloop.go` — review-loop dispatcher. Calls into the watcher/adapter surface; will receive hook-derived events through the existing watcher event channel.
- `twins/harmonik-twin-claude/` — the twin binary. Already wired against the same wire-protocol surface the bridge will produce. This is the conformance reference for the bridge.

## Pattern: ntm vs direct exec

`docs/components/external/ntm.md` and `docs/subsystems/agent-runner.md` describe ntm as the process-management substrate, but MVH does not require tmux. The handler-contract HC-052 says shape evolution re-implements the adapter, not the watcher. For the bridge:
- Decision: direct `os/exec` for MVH. Subprocess is a child of the daemon per HC-044. Tmux/ntm is a post-MVH shape.

## Pattern: Twin parity (HC-INV-002)

`internal/handler/twinlaunch.go` and tests. The twin binary is a real subprocess that speaks the wire protocol over the daemon socket. It's launched the same way as a real handler subprocess. The watcher cannot distinguish twin from real.

For the bridge:
- The twin already produces the wire protocol natively (no relay layer).
- The real-Claude path produces the wire protocol via the relay layer.
- The watcher sees identical NDJSON either way.
- HC-INV-002 satisfied.

## External Go SDK reference

`github.com/partio-io/claude-agent-sdk-go` (third-party, MIT, Sept 2026): spawns `claude --print --input-format stream-json --output-format stream-json` and parses NDJSON. Hooks are registered via the CONTROL PROTOCOL embedded in stream-json (NOT settings.json).

This is significant prior art for the counter-evidence pass. See counter-evidence/findings.md.

## Pattern: progress-stream NDJSON over Unix domain socket (HC-007/HC-007a)

The daemon-side watcher already has the read-loop for NDJSON on the per-session socket connection. Each handler-side message is one JSON line; the watcher decodes and publishes to the in-process bus.

For the relay subcommand: when invoked by Claude, it opens a one-shot socket connection to `.harmonik/daemon.sock`, writes ONE NDJSON message (the translated progress-stream message), waits for ACK (or fire-and-forget?), closes. This means N relay invocations within a Claude session result in N short-lived socket connections. The watcher MUST accept this — the wire protocol per HC-007a was specced as a long-lived bidirectional stream, but read literally it only requires NDJSON framing. ONE-shot connections work if the watcher's connection handling accepts each new connection as part of the same session-keyed event flow.

DESIGN-PASS DECISION POINT: persistent connection vs per-invocation reconnect.
- Persistent: relay invocations would need to share state, but they're separate processes spawned by Claude per-hook. State-sharing across short-lived subprocesses requires either a long-running side process (the "relay daemon"?) or accepting per-invocation reconnect.
- Per-invocation: each relay invocation dials, writes, closes. The watcher binds by (run_id, claude_session_id) keyed off env-vars in the NDJSON message envelope.
- Decision: PER-INVOCATION (simpler, no relay daemon). The watcher's session-bind happens at handler launch (handler-side, not relay-side); relay invocations are routed into the existing session by (run_id, claude_session_id) carried in each message.

## Pattern: existing daemon socket protocol

`internal/daemon/launchspecbuild.go` and `process-lifecycle.md §4.2 PL-003a` declare the wire format for CLI / agent requests. JSON-RPC 2.0. Per-connection.

For relay → daemon: extending JSON-RPC 2.0 over the existing socket. Add a `progress_stream.emit` method (or similar) that accepts a single NDJSON message and returns success/typed-error. Aligns with HC-016a's typed-error retry path against `daemon_not_ready`.

DESIGN-PASS DECISION POINT: is "progress_stream.emit" a JSON-RPC method, or does the relay reuse the bare NDJSON connection per HC-007a?
- Trade-off: HC-007a says the progress stream IS the bare NDJSON stream over the socket. PL-003a says the CLI control surface is JSON-RPC 2.0 over the same socket. The two protocols coexist by the daemon detecting which one a new connection wants.
- Decision: relay reuses the bare NDJSON path. The first line on the connection is the progress message; the daemon's connection handler dispatches to the watcher keyed by (run_id, claude_session_id). This avoids inventing a new JSON-RPC method.

## Pattern: silent-hang invariant reuse (HC-026 / HC-026a)

The handler MUST emit `agent_heartbeat` at least every T/2 seconds for T_silent_hang = 600s. For Claude, this is achievable via the `Notification` hook (heartbeat synthesis) supplemented by a periodic relay-side timer if Notification doesn't fire often enough.

INSIGHT: A simpler synthesis is to attach a Notification hook with matcher `"*"` (always fires) and translate every Notification (especially `idle_prompt`) into agent_heartbeat. But Notification cadence is not guaranteed under 300s. So we additionally synthesize heartbeats at the HANDLER side (the long-running parent process of Claude, distinct from the short-lived relay): the handler maintains its own T/2 timer and writes agent_heartbeat to the daemon socket while Claude is running. This is consistent with HC-026a's "Handlers wrapping LLMs that have no output channel during extended reasoning MUST synthesize heartbeats on an internal timer." The handler IS that wrapping handler.

ARCHITECTURAL CONSEQUENCE: there are TWO distinct contributors to the progress stream for a Claude session:
1. The HANDLER process (long-lived, parent of Claude) emits handler_capabilities, session_log_location, skills_provisioned, agent_ready, agent_heartbeat (internal timer), agent_completed/agent_failed (on Wait-return), shutdown handling.
2. The RELAY process(es) (short-lived, child of Claude) emits outcome_emitted (from Stop hook), rate-limit signals (from StopFailure hook), and any other event derived from hooks.

Both contributors write the SAME wire-format NDJSON to the daemon socket, keyed by (run_id, claude_session_id). The watcher sees a single coherent event flow.

This is the load-bearing insight for the design pass.
