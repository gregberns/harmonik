# Change Design — claude-hook-bridge (new spec)

## Current state

There is no spec governing how Claude Code's lifecycle translates into harmonik's handler-contract progress-stream protocol. `internal/handler/adapter_claudecode.go` provides only Adapter callbacks (DetectReady, DetectRateLimit, CleanExitSequence, RotateAccount). The launch mechanics, settings.json materialization, hook-event mapping, and pre-generated session_id flow are unspecified. `docs/subsystems/agent-runner.md` line 28 says "Configure Claude Code hooks (or equivalent)" and stops.

## Target state

A new normative spec `specs/claude-hook-bridge.md` (prefix CHB; version 0.1; status `draft` at first writing, advancing to `reviewed` after spec-corpus review). Spec category: `runtime-subsystem`. Depends on: handler-contract, workspace-model, event-model, process-lifecycle, execution-model, architecture.

The spec defines, in seven sections under `## 4. Normative requirements`, the deterministic translation layer between Claude Code and the daemon's progress-stream wire protocol.

## DESIGN DECISIONS (resolved here)

### D1. Subcommand vs separate binary

**DECIDED: subcommand of the main `harmonik` binary** (`harmonik hook-relay <event-kind>`).

Rationale:
- One shipping artifact: the operator installs `harmonik` and gets the relay automatically.
- Settings.json `command:` slot references `harmonik` resolved via PATH; works for both system-installed and dev-built binaries.
- Twin-parity unchanged — twins do not invoke the relay.
- Future re-implementations of the bridge against a different agent type re-implement under their own subcommand without proliferating binaries.

### D2. Env-var schema (final)

Each name, type, requirement-level:

| Env var | Type | Required? | Source | Notes |
|---|---|---|---|---|
| `HARMONIK_RUN_ID` | UUIDv7 string | required | LaunchSpec.run_id | filesystem-safe |
| `HARMONIK_DAEMON_SOCKET` | absolute path | required | daemon config | default `<repo>/.harmonik/daemon.sock` |
| `HARMONIK_WORKSPACE_PATH` | absolute path | required | LaunchSpec.workspace_path |  |
| `HARMONIK_HANDLER_SESSION_ID` | UUID string | required | minted by handler at Launch | distinct from claude_session_id |
| `HARMONIK_CLAUDE_SESSION_ID` | UUID string | required | minted by handler (or carried from prior iteration for resume) | matches Claude's --session-id / --resume value |
| `HARMONIK_WORKFLOW_MODE` | enum `{single, review-loop, dot}` | optional | LaunchSpec.workflow_mode | only set when non-default |
| `HARMONIK_PHASE` | enum `{single, implementer-initial, implementer-resume, reviewer}` | optional | LaunchSpec.phase | only set in multi-phase modes |
| `HARMONIK_ITERATION_COUNT` | integer 1..3 | optional | LaunchSpec.iteration_count |  |
| `HARMONIK_BEAD_ID` | string | optional | LaunchSpec.bead_id |  |
| `HARMONIK_NODE_ID` | string | required | LaunchSpec.node_id |  |
| `HARMONIK_WORKFLOW_ID` | UUID string | required | LaunchSpec.workflow_id |  |
| `HARMONIK_AGENT_TYPE` | string | required | LaunchSpec.agent_type | = "claude-code" |
| `HARMONIK_SECRET_*` | string | per HC-028 | secrets registry | unchanged |

Spec MUST forbid any HARMONIK_* env-var not on this list. Future fields require a foundation amendment.

### D3. Stdin payload schema (per Claude hook event)

The relay reads JSON on stdin (Claude's hook payload schema), matches `hook_event_name`, and constructs the corresponding progress-stream message.

Mapping table (final):

| Claude hook event | Translates to progress-stream message | Fields derived |
|---|---|---|
| `SessionStart {source: startup}` | `agent_ready` (preceded by handler-emitted `handler_capabilities`, `session_log_location`, `skills_provisioned`; see D5) | `session_id` = HARMONIK_HANDLER_SESSION_ID, `capabilities[]` = handler's declared set |
| `SessionStart {source: resume}` | `agent_ready` (with `is_resume=true` payload hint) | same |
| `Stop` | `outcome_emitted` | payload = `Outcome{kind, payload}`; kind = WORK_COMPLETE if phase ∈ {single, implementer-initial, implementer-resume}, REVIEWER_VERDICT if phase = reviewer; payload pulled from `.harmonik/review.json` for reviewer or synthesized for implementer |
| `SessionEnd` | (no-op; the handler's Wait-return emits `agent_completed` via HC-008a) | — |
| `StopFailure {error_type: rate_limit}` | `agent_rate_limited` | `retry_after_seconds` synthesized if absent |
| `StopFailure {other error types}` | `agent_failed` | `class` mapped per error_type → sentinel: authentication_failed/oauth_org_not_allowed/billing_error → ErrStructural; invalid_request → ErrStructural; server_error → ErrTransient; max_output_tokens → ErrTransient; unknown → ErrStructural |
| `Notification {type: idle_prompt}` | `agent_heartbeat {phase: waiting_input}` | resets silent-hang timer per HC-026/HC-026a |
| `Notification {type: permission_prompt}` | `agent_heartbeat {phase: waiting_input}` | |
| `Notification {other}` | `agent_heartbeat {phase: reasoning}` | |
| `UserPromptSubmit` | (no-op at MVH) | reserved for post-MVH observability |
| `PreCompact` / `PostCompact` | (no-op at MVH) | reserved |

All other Claude hook events (28 of 30) are explicitly out of scope at MVH. The relay handles them with a no-op exit-0.

### D4. Daemon-socket protocol for relay → daemon

**DECIDED: relay reuses the bare NDJSON path on `.harmonik/daemon.sock`** (NOT a new JSON-RPC method).

Each relay invocation:
1. Reads stdin (Claude's hook payload JSON).
2. Reads env (HARMONIK_*).
3. Maps to one NDJSON progress_stream message per D3.
4. Dials `${HARMONIK_DAEMON_SOCKET}` (Unix domain socket, with timeout 5s).
5. Writes ONE NDJSON line (the message), each line ≤ 1 MiB per HC-007a.
6. Reads back a one-byte ACK or a typed-error JSON line (`daemon_not_ready{reason=...}`).
7. On typed daemon_not_ready response: retry per HC-016a bounded backoff, capped at 30s total wall-clock (subset of HC-016a's 60s to fit inside the settings.json timeout).
8. Closes the connection. Exits 0 on success, 1 with stderr diagnostic on permanent failure.

The watcher-side connection acceptor:
- Each new connection on `.harmonik/daemon.sock` reads the first line (NDJSON).
- The line's envelope MUST carry `run_id` and `claude_session_id` (new envelope field) so the watcher can route the message to the correct session.
- The acceptor publishes the message to the existing session-bound bus event flow (same as if the message had arrived on the long-lived progress-stream connection).
- The acceptor closes the connection after publishing.

This re-uses HC-007a's NDJSON framing without inventing a new protocol. The "one connection per progress message" mode is a NEW connection-lifetime regime within HC-007a — design pass calls this out so the HC amendment names it.

### D5. handler_capabilities / skills_provisioned / agent_ready / session_log_location ordering

These four are required by HC-INV-004 to fire in order BEFORE work dispatch. None of them map cleanly to a Claude hook event (Claude doesn't have a "ready" signal Internal to its own lifecycle that's distinct from SessionStart).

**DECIDED:** the **handler process** (long-lived, parent of Claude — not the short-lived relay) emits these four directly to the daemon socket BEFORE invoking `claude --session-id <uuid>`. Specifically:
1. Handler binds its progress-stream socket connection to the daemon.
2. Handler writes `handler_capabilities` (declaring wire-protocol version 1).
3. Handler writes `session_log_location` (pointing at `~/.claude/projects/<slug>/<claude_session_id>.jsonl` — the Claude transcript path).
4. Handler writes `skills_provisioned` (after provisioning per HC-046).
5. Handler writes `agent_ready` (declaring the session is ready to accept work — note this fires BEFORE Claude exec, not after).
6. Handler exec's Claude.
7. From here, Claude's lifecycle drives the relay-side events.
8. On Claude exit (Wait returns), the handler emits `agent_completed` (if outcome_emitted was published per HC-008a) or `agent_failed` (otherwise).

This satisfies HC-INV-004 deterministically without depending on Claude emitting any specific signal.

### D6. agent_heartbeat synthesis (long-running)

Notification hooks fire only when Claude shows UI notifications, which can be sparse. Handler-side timer-based synthesis is required for silent-hang invariant compliance (HC-026a).

**DECIDED:** the handler process maintains a `time.Ticker` at interval `T_heartbeat = T_silent_hang/2 = 300s`. On each tick, if Claude is still alive (`cmd.ProcessState == nil`), emit `agent_heartbeat{phase: reasoning}`. Notification-hook-driven heartbeats (from the relay) carry a different `phase` field (`waiting_input`) and refresh the silent-hang timer just the same.

### D7. Reviewer phase verdict file read-out

For phase=reviewer, the Stop hook fires when Claude finishes writing `.harmonik/review.json` and emits its final assistant message. The relay needs to read the file to construct the `outcome_emitted` payload.

**DECIDED:** the relay reads `${HARMONIK_WORKSPACE_PATH}/.harmonik/review.json` on Stop in phase=reviewer. Reads schema_version (MUST=1), verdict ∈ {APPROVE, REQUEST_CHANGES, BLOCK}, flags[], notes. Validation per event-model §8.1a.3 (the `reviewer_verdict` schema-reuse rule). The relay publishes `outcome_emitted` with the verdict payload; the daemon's review-loop dispatcher (`internal/daemon/reviewloop.go`) reads `.harmonik/review.json` itself in parallel for the `reviewer_verdict` event emission per §8.1a (already specced).

**Edge case (file missing):** if `.harmonik/review.json` is absent at Stop time for phase=reviewer, the relay publishes `outcome_emitted{kind=REVIEWER_VERDICT, payload={error: "missing_review_file"}}` — the daemon's dispatcher routes this to `review_loop_cycle_complete{completion_reason=error}` per the existing event-model spec.

### D8. Pre-existing user settings.json collision

**DECIDED:** harmonik's settings.json materialization MERGES with any pre-existing `${workspace_path}/.claude/settings.json`. Merge algorithm: harmonik's hook entries are APPENDED to the matching event-type arrays. User hooks fire alongside harmonik hooks.

If merge produces a conflict (e.g., same matcher with side effects), it is the user's responsibility to organize their hooks; harmonik does not warn at MVH. Post-MVH: a `workspace_warning` event MAY be emitted on detected conflicts.

If parsing the pre-existing file fails, harmonik's materialization OVERWRITES it (and emits a `workspace_warning` event noting the displacement). The bridge spec MUST name this behavior in its WM amendment.

### D9. --session-id and --resume usage

**DECIDED:**
- phase = single OR implementer-initial OR reviewer: handler mints fresh UUIDv7, sets HARMONIK_CLAUDE_SESSION_ID, exec's `claude --session-id <uuid> --output-format text --print "..."` (or appropriate flags per Claude CLI invocation discipline; the bridge spec MUST NOT use `--fork-session`, `--bare`, `--no-session-persistence`).
- phase = implementer-resume: handler reuses LaunchSpec.claude_session_id (carried from prior iteration), sets HARMONIK_CLAUDE_SESSION_ID = LaunchSpec.claude_session_id, exec's `claude --resume <claude_session_id> ...`.
- The reviewer phase ALWAYS uses fresh --session-id (per HC-006).
- After implementer's outcome_emitted in iteration N, the daemon stores claude_session_id in run state. When dispatching iteration N+1's implementer (phase=implementer-resume), the daemon populates LaunchSpec.claude_session_id from this stored value. Already specced in HC-006 prose; the bridge formalizes the handler-side obligation.

### D10. Twin parity

The twin binary `harmonik-twin-claude` MUST be capable of emitting the same progress-stream sequence directly (no relay layer, no settings.json materialization). Scenario scripts drive the twin per HC-026a's scripted carve-out. The relay subcommand is invoked ONLY by Claude Code via settings.json hooks — never by the twin.

The bridge spec's conformance section (§10) enumerates the relay-subcommand-specific tests separately from the twin-conformance tests. The twin satisfies the wire-level invariants; the relay's NDJSON output is byte-identical to what the twin would emit for the same scenario.

### D11. Failure-mode classification (final)

| Failure | Class | Sub-reason | Event emitted by |
|---|---|---|---|
| Relay can't dial daemon socket | ErrTransient (transient) | `bridge_dial_failed` | handler on Wait-return (via agent_failed) IF the relay's failure pattern indicates the relay can't reach daemon; otherwise the missing event is masked by the handler's other emissions |
| Relay daemon_not_ready retry exhausted | ErrTransient | `bridge_daemon_startup_window_exceeded` | handler on Wait-return |
| Malformed hook payload (Claude version skew) | ErrStructural | `bridge_malformed_hook_payload` | handler on Wait-return |
| Hook command timeout (Claude killed relay) | (no separate event) | (Claude continues; harmonik may miss the event; handler-side terminal event covers it) | — |
| Stop hook fired but .harmonik/review.json missing in phase=reviewer | (no separate event) | outcome_emitted with kind=REVIEWER_VERDICT and error payload | relay |

### D12. Event-model amendment: zero new event types

**DECIDED:** event-model.md amendment is empty. The relay-failure modes route through existing `agent_failed{class, sub_reason}` envelopes. No new event types are introduced.

Trade-off considered (counter-evidence): introducing a `bridge_unreachable` event for operator observability. Rejected: HC-026's silent-hang plus agent_failed's sub_reason discriminator already carry the diagnostic. Adding an event would expand the event vocabulary without unlocking a routing decision.

### D13. Handler vs relay vs daemon responsibilities (matrix)

| Step | Who | When |
|---|---|---|
| Mint claude_session_id | Handler | At Launch (for single/initial/reviewer); reused from LaunchSpec for resume |
| Write .claude/settings.json | Workspace manager (S06) | At workspace creation (WM-016 ordering) |
| Emit handler_capabilities | Handler | Before Claude exec |
| Emit session_log_location | Handler | Before Claude exec |
| Emit skills_provisioned | Handler | After provisioning, before Claude exec |
| Emit agent_ready | Handler | Before Claude exec |
| Exec Claude with --session-id / --resume | Handler | After agent_ready |
| Spawn relay invocations | Claude (via settings.json) | On each hook event during session |
| Emit outcome_emitted | Relay | On Stop hook |
| Emit agent_heartbeat (Notification-derived) | Relay | On Notification hook |
| Emit agent_heartbeat (timer-derived) | Handler | Every 300s while Claude alive |
| Emit agent_rate_limited / agent_failed (StopFailure) | Relay | On StopFailure hook |
| Emit agent_completed | Handler | On clean Wait-return after outcome_emitted (per HC-008a) |
| Emit agent_failed (Claude crash, no outcome) | Handler | On dirty Wait-return without outcome_emitted |
| Validate review.json | Relay (raw) + daemon dispatcher (canonical) | Stop hook (relay reads for outcome payload); reviewer_verdict event emission (daemon canonical) |

## Requirements traceability (CHB-NNN sketches)

- CHB-001..010 (Settings.json shape): D2, D8, D11.
- CHB-011..020 (Env-var schema): D2.
- CHB-021..030 (Relay subcommand contract): D1, D4, D11.
- CHB-031..040 (Hook → progress-message mapping): D3.
- CHB-041..050 (Pre-generated session_id flow): D9.
- CHB-051..060 (Failure-mode classification): D11, D12.
- CHB-061..070 (Twin parity): D10.
- CHB-071..080 (Subprocess tree clarification): D1, D6.
- CHB-INV-001..00x: derived from D3, D5, D6.

## Rationale summary

The design holds the user-fixed constraints (settings.json + relay, pre-generated session_id, twin parity, reuse HC silent-hang) verbatim. Counter-evidence (stream-json + --include-hook-events) is documented but not adopted; the relay path is the chosen architecture. The handler-side emission of pre-Claude-exec events (D5) is the most novel decision — it sidesteps Claude's lack of a clean "I'm ready" signal that's distinct from SessionStart. The two-contributor model (handler + relay both writing to the daemon socket) is the load-bearing architectural insight.
