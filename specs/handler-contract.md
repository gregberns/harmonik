# Handler Contract

```yaml
---
title: Handler Contract
spec-id: handler-contract
requirement-prefix: HC
status: reviewed
spec-category: foundation-cross-cutting
spec-shape: requirements-first
version: 0.4.0
spec-template-version: 1.1
owner: foundation-author
last-updated: 2026-05-18
depends-on:
  - architecture
  - execution-model
  - event-model
  - process-lifecycle
---
```

## 1. Purpose

This spec defines the Go `Handler` and `Session` interfaces, the handler-subprocess wire protocol, the `LaunchSpec` shape the daemon delivers to every handler, the goroutine ownership split between the daemon and the Agent Runner (S04), the typed error taxonomy that bridges handler failures to routing decisions, the secrets-redaction pipeline, the twin-parity invariant, the ready-state signal, the session-log emission obligation, and the skill-injection obligation. It is also the architectural modularity boundary: the handler contract is the stable seam between the deterministic daemon and the execution shape (ntm + tmux + subprocess today, anything else tomorrow).

It is normative for every subsystem that launches, monitors, or interprets the output of an agent subprocess, and for any future handler implementation (Claude Code, Pi, twins, cloud execution shapes).

## 2. Scope

### 2.1 In scope

- Go `Handler` and `Session` interfaces.
- Wire protocol for the daemon-to-handler-subprocess boundary (LaunchSpec delivery on the daemon Unix socket, NDJSON-framed progress-event stream, outcome delivery, capability negotiation, `ErrProtocolMismatch`).
- `LaunchSpec` record shape, including `snapshot_token` for reconciliation investigator handlers and `provisioning_timeout` for skill installation.
- Goroutine ownership split: daemon-owned watcher + S04-owned adapter. The watcher is authoritative for every bus-emitted handler-lifecycle event; handler-subprocess progress-stream messages are translated into events by the watcher (see §4.2.HC-007, §4.2.HC-010).
- Session lifecycle event emission obligations (`agent_started`, `agent_ready`, `agent_output_chunk`, `agent_completed`, `agent_failed`, `agent_rate_limited`, `agent_rate_limit_cleared`, `agent_heartbeat`, `skills_provisioned`, `session_log_location`).
- `context.Context` propagation rules (cancellation, deadlines, value scoping).
- Typed error taxonomy: five primary sentinel classes (`ErrTransient`, `ErrStructural`, `ErrDeterministic`, `ErrCanceled`, `ErrBudget`) plus two structural sub-sentinels (`ErrProtocolMismatch`, `ErrSkillProvisioningFailed`).
- Silent-hang detection state machine, keyed off absent `agent_heartbeat` events.
- Post-outcome shutdown window (T_shutdown) after `outcome_emitted`.
- Secrets injection via environment variable prefix; redaction registry (common prefix rule, per-handler patterns, compile-time payload-schema check).
- Ready-state detection.
- Twin-parity invariant (same interface, same wire protocol, same event schema).
- Skill injection obligation (resolve, provision, emit `skills_provisioned`, fail-launch with `ErrSkillProvisioningFailed` on resolution failure).
- Handler launched from a repo-relative path with commit-hash gate for in-repo binaries.
- Handler contract as modularity boundary (execution-shape evolution re-implements the adapter, not the watcher).

### 2.2 Out of scope

- Event payload schemas for handler-lifecycle events and `skills_provisioned` — owned by [event-model.md §6.3]. This spec declares WHEN each event fires and what fields it MUST carry at a name level; event-model is normative for the on-the-wire payload.
- Workspace path construction, session-log directory creation, post-merge session-log archival — owned by [workspace-model.md §4.7]. This spec declares S04's emission obligation; workspace-model owns the three-subsystem pipeline (S04 → S06 → S08).
- Per-handler implementations (Claude Code, Pi, `claude-twin`, `pi-twin`) — each is its own per-handler spec, post-MVH.
- Skill storage location, skill-package registry, per-handler skill-installation shape — owned by [control-points.md §4.11] (declaration surface) and future `agent-configuration` spec (storage layout).
- Scenario harness and twin-conformance drift detection — owned by the `scenario-harness` (S07) spec, post-MVH.
- Binary signing (cosign, full supply-chain verification) — deferred post-MVH per locked decision; commit-hash check is the MVH gate.
- Secret rotation mid-session — out of scope for MVH; a new launch is required. Note: this is **provider-secret rotation** (new API key value for the same provider); **account rotation** (different pool member) is in scope and is governed by §4.3.HC-013 + §4.6.HC-013a at clean turn boundaries.
- Non-Unix transport targets — Windows and remote/cloud execution shapes cannot use the local filesystem socket pinned in §4.2.HC-007. They are deferred post-MVH. Adding them is a breaking change to §4.2 (a transport-substitution mechanism, preserving NDJSON framing, bidirectional flow, and authenticated-connection semantics) requiring a foundation amendment per §6.3; tracked as OQ-HC-010.

## 3. Glossary

- **handler** — a Go type implementing the `Handler` interface that is responsible for launching, monitoring, and cleaning up an agent subprocess of a specific `agent_type`. (see §4.1)
- **session** — a single instantiation of an agent subprocess produced by a `Handler.Launch` call; represented by the `Session` interface. (see §4.1)
- **adapter** — a per-agent-type callback object owned by S04, invoked synchronously by the daemon's watcher goroutine on specific lifecycle events. Not a goroutine; not per-session state. (see §4.3)
- **watcher** — the single goroutine per active session owned by S01 that reads the handler's progress stream and publishes events. (see §4.3)
- **LaunchSpec** — the record the daemon hands to `Handler.Launch`, carrying everything the handler needs to start an agent subprocess. (see §4.2, §6.1)
- **wire protocol** — the process-boundary contract: how the daemon and the handler subprocess exchange LaunchSpec, progress events, and outcomes. (see §4.2, §7.2)
- **ready-state** — the moment a handler subprocess has signalled it is able to accept work, indicated by the `agent_ready` event. (see §4.9)
- **twin** — a handler whose `Launch` spawns a subprocess that emits scripted output instead of invoking an LLM; same interface, same wire protocol, same event schema as the real handler. (see §4.8)
- **skill** — a capability bundle (a file-drop directory, a CLI binary, an MCP registration, a reference-doc bundle) provisioned into an agent subprocess before it begins work. (see §4.11)
- **redaction registry** — the mechanism-tagged component that scans event payloads and log lines for secret-shaped fields and strips them before emission. (see §4.7)
- **snapshot token** — an opaque token carried in LaunchSpec for investigator-agent handlers, binding the agent's reads to a captured `(git_head_hash, beads_audit_entry_id)` per [reconciliation/spec.md §4.4].

## 4. Normative requirements

### 4.1 Handler and Session interfaces

#### HC-001 — Handler is the Go interface defined in §6.1

Every handler MUST implement the `Handler` interface defined in §6.1. The interface is the single contract for launching an agent subprocess. Real handlers, twin handlers, and any future execution-shape handlers (cloud-execution, remote-container) MUST implement the same interface. Adding an agent type MUST NOT alter the `Handler` interface; it adds an adapter per §4.3.

Tags: mechanism

#### HC-002 — Session is the Go interface defined in §6.1

Every `Handler.Launch` success MUST return a `Session` satisfying the interface defined in §6.1. The session object's lifetime begins with `Launch` return and ends when `Wait` returns. All session methods MUST be safe to call from any goroutine.

Tags: mechanism

#### HC-003 — Handler selection is config-level

The choice of which handler binary to launch for a given node MUST be derived from DOT node attributes (`handler_ref`, `agent_type`) and YAML policy, resolved at workflow-load time per [execution-model.md §4.9]. The daemon MUST NOT carry test-mode branches that select between real and twin handlers at runtime; the selection is config-level only.

Tags: mechanism

#### HC-003a — Workflow-mode is dispatch-level, not handler-selector

`LaunchSpec.workflow_mode` (per §4.2.HC-006) MUST NOT be used to pick among registered handlers. Handler selection remains the config-level binding from `agent_type` to a registered handler per §4.1.HC-003. The resolved workflow mode determines (a) which phase the daemon launches next within a multi-phase mode and (b) the LaunchSpec's `phase`, `iteration_count`, and `claude_session_id` fields per §4.2.HC-006. The same registered handler MUST be used across every phase of a multi-phase mode (e.g., both `implementer-initial` and `reviewer` phases of `review-loop` resolve to the same `agent_type` binding); the phases are distinguished by LaunchSpec content (prompt, `required_skills[]`, `freedom_profile_ref`), NOT by handler binding. The adapter surface (§4.3.HC-013) MUST NOT expand to accommodate workflow-mode dispatch; watcher behavior (§4.3.HC-011) MUST remain mode-agnostic.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

#### HC-004 — Launch is idempotent on (run_id, node_id, phase, iteration_count)

`Handler.Launch` MUST be idempotent within one daemon generation. The idempotency key composition depends on the LaunchSpec's `phase` and `iteration_count` fields per §4.2.HC-006: when both fields are present (multi-phase modes such as `review-loop`), the key is the 4-tuple `(spec.run_id, spec.node_id, spec.phase, spec.iteration_count)`; when both fields are absent (`workflow_mode = single` or omitted), the key is the 2-tuple `(spec.run_id, spec.node_id)`. A second `Launch` call with the same key MUST return the existing `Session` (or an `ErrTransient` if the prior session is terminating) rather than spawn a duplicate subprocess. If a second `Launch` call arrives while the first is still executing its handshake (concurrent-launch), the second call MUST block on the handshake outcome and return the same `(Session, error)` the first call returns; it MUST NOT spawn a second subprocess. Within a single `review-loop` cycle, the daemon MAY legitimately issue Launch calls with distinct `(phase, iteration_count)` tuples (e.g., `(phase=implementer-resume, iteration_count=2)` distinct from `(phase=implementer-resume, iteration_count=1)`) without this requirement's "return existing Session" branch firing. Callers receiving `ErrTransient` (prior session terminating) SHOULD retry after a backoff bounded by the node's retry policy in [control-points.md §6.7]; a concrete default per-handler retry delay is the per-handler spec's call. Reconciliation-driven re-launches after a daemon restart are a new daemon generation and therefore a new launch; idempotency is scoped per daemon generation. Review-loop launches that survive a daemon restart re-launch under their `(run_id, node_id, phase, iteration_count)` key in the new generation, distinguishing them from prior launches in the same logical cycle.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=unsafe; idempotency=idempotent

### 4.2 Wire protocol

#### HC-005 — LaunchSpec delivery is JSON-on-stdin by default; file-path when over 1 MiB

The daemon MUST deliver the LaunchSpec to the handler subprocess via ONE of two mechanisms: (a) JSON on stdin (default, for LaunchSpec payloads ≤ 1 MiB), or (b) a file-path argument `--launch-spec <path>` pointing at a JSON file (for LaunchSpec payloads > 1 MiB). The handler MUST accept both forms. Selection MUST be driven by payload size at call time, NOT by handler type.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

#### HC-006 — LaunchSpec is the record defined in §6.1

The delivered LaunchSpec MUST conform to the record shape in §6.1. Required fields are: `run_id`, `workflow_id`, `node_id`, `agent_type`, `workspace_path`, `required_skills[]`, `skill_search_paths[]`, `timeout`, `provisioning_timeout`, `budget`, `freedom_profile_ref`. Optional fields: `bead_id` (present when bead-tied per [execution-model.md §4.3]), `snapshot_token` (present for reconciliation-investigator handlers per [docs/foundation/reconciliation.md §9.4b] (bootstrap; migrates to `specs/reconciliation.md §9.4b` when finalized)), `workflow_mode`, `phase`, `iteration_count`, `claude_session_id`, `model_preference` (present when the resolution chain per [execution-model.md §4.3.EM-012b] produced at least one non-empty field; absent when both fields are empty).

**`workflow_mode`** (enum `{single, review-loop, dot}`, optional). Present iff the daemon resolved a non-default mode for the dispatched run; otherwise omitted. The handler MUST accept the field. The handler MUST NOT branch implementation behavior on this field — it is observational, supplied for handler-side logging and skill-loading hints only. Handler selection MUST NOT depend on this field per §4.1.HC-003a.

**`phase`** (enum, optional). Present iff the dispatched run is in a multi-phase mode. For `workflow_mode = review-loop`, the domain is `{implementer-initial, implementer-resume, reviewer}`. For `workflow_mode = single`, the field MUST be omitted. The handler MUST NOT interpret routing semantics from `phase`; routing is the daemon's responsibility per §4.1.HC-003a.

**`iteration_count`** (integer, optional, 1..3). Present iff the dispatched run is in a multi-phase mode that iterates. For `review-loop`, the value is bounded by the hardcoded iteration cap of 3 declared in [operator-nfr.md §4.1 ON-004]. For `workflow_mode = single`, the field MUST be omitted.

**`claude_session_id`** (string, optional). Present iff `phase = implementer-resume` for a `review-loop` dispatch; carries the Claude Code session identifier used to drive `claude --resume <id>` and is distinct from harmonik's own `session_id` per §6.1. The `reviewer` phase MUST omit `claude_session_id`; each reviewer launch is a fresh Claude session. The `implementer-initial` phase MUST omit `claude_session_id` (no prior session exists to resume). Handlers that do not implement Claude Code's session-resume capability MAY ignore the field; handlers that do MUST honor it when present. The handler-side minting and propagation discipline for `claude_session_id` is normative per §4.10.HC-045c; the LaunchSpec field is populated by the daemon per HC-006 (this requirement) for the resume case only, and the daemon's durability obligation for the persisted value is per [claude-hook-bridge.md §4.6 CHB-023].

> INFORMATIVE: The `phase = reviewer` launch typically carries an `agent-reviewer` skill in `required_skills[]` per the [CLAUDE.md] skill registry; `phase = implementer-*` launches carry the implementer skill set. Selection of `required_skills[]` is the daemon's claim-path responsibility per §4.11.HC-050, not the handler's. The reviewer phase's `outcome_emitted` corresponds to the reviewer writing a verdict file at `.harmonik/review.json` (archived to `.harmonik/review.iter-<N>.json` between iterations) per [workspace-model.md §4.7].

Tags: mechanism

#### HC-006a — Per-phase LaunchSpec field requirements (normative table)

This table is the single authoritative reference for which `LaunchSpec` fields MUST differ across the three `review-loop` phases vs. which fields MAY be shared. Each row names the field (or field group), gives the required value shape per phase, and cites the spec clause that is the primary normative source for that requirement.

The implementation in `internal/daemon/claudelaunchspec.go` (`buildClaudeLaunchSpec`) reflects current behavior and is cited as evidence below. The spec is authoritative; the code follows.

**Scope:** `workflow_mode = review-loop` only. For `workflow_mode = single`, `phase` is absent and all per-phase distinctions collapse to the `implementer-initial` column (fresh session, bead-scoped workspace).

| Field / group | `implementer-initial` | `implementer-resume` | `reviewer` | Primary spec ref |
|---|---|---|---|---|
| **`argv[0]`** (`--session-id` vs `--resume`) | `--session-id <fresh-UUIDv7>` — handler mints a new UUID | `--resume <claude_session_id>` — reuses the UUID from `LaunchSpec.claude_session_id` (carried from prior iteration) | `--session-id <fresh-UUIDv7>` — handler mints a new UUID; MUST NOT reuse a prior reviewer ID across iterations | §4.10.HC-045c; [claude-hook-bridge.md §4.3 CHB-008] |
| **`argv` — `--model` / `--effort`** | Present iff `LaunchSpec.model_preference.{model,effort}` is non-empty; omitted otherwise | Same rule as `implementer-initial` (shared `model_preference` descriptor) | Same rule as `implementer-initial`; reviewer MAY carry a different profile if the operator resolves it differently | §4.10.HC-055a; §4.10.HC-055 |
| **`argv` — `--dangerously-skip-permissions`** | Present iff `workspace_path` canonicalizes under the harmonik worktrees root | Same rule | Same rule | §4.10.HC-055b |
| **`env` — `HARMONIK_PHASE`** | `"implementer-initial"` | `"implementer-resume"` | `"reviewer"` | [claude-hook-bridge.md §4.2 CHB-006] |
| **`env` — `HARMONIK_ITERATION_COUNT`** | `"1"` (first cycle iteration) | `"2"` or `"3"` (subsequent iterations, capped at 3 per ON-004) | Same value as the co-iterating implementer phase; set by daemon on construction | §4.2.HC-006 (`iteration_count` field); [operator-nfr.md §4.1 ON-004] |
| **`env` — `HARMONIK_CLAUDE_SESSION_ID`** | Fresh UUIDv7 minted by handler | Reused from prior implementer-initial launch (same value as `LaunchSpec.claude_session_id`) | Fresh UUIDv7 minted by handler; distinct from any implementer session | [claude-hook-bridge.md §4.2 CHB-006]; §4.10.HC-045c |
| **`env` — `HARMONIK_WORKFLOW_MODE`** | `"review-loop"` | `"review-loop"` | `"review-loop"` | [claude-hook-bridge.md §4.2 CHB-006] (shared across all phases) |
| **`env` — `HARMONIK_RUN_ID` / `HARMONIK_WORKSPACE_PATH` / `HARMONIK_DAEMON_SOCKET`** | Set from `claudeRunCtx.runID` / `workspacePath` / `daemonSocket` | Same values as `implementer-initial` (same run, same worktree, same socket) | Same `runID` and `daemonSocket`; `workspacePath` MAY be the same worktree or a review-staging path (see `working_dir` row below) | [claude-hook-bridge.md §4.2 CHB-006] |
| **`working_dir` (`LaunchSpec.WorkDir`)** | Bead-assigned worktree path (e.g. `.harmonik/worktrees/<run_id>/`) | Same worktree as `implementer-initial` (resume writes to the same tree) | Same worktree as the implementer phases at MVH; a separate review-staging path is a post-MVH option | §4.2.HC-006 (`workspace_path` field); [workspace-model.md §4.1] |
| **`LaunchSpec.claude_session_id` (wire-protocol field)** | **ABSENT** — no prior session exists | **PRESENT** — carries the Claude session ID minted by the `implementer-initial` launch; durability obligation per [claude-hook-bridge.md §4.6 CHB-023] | **ABSENT** — each reviewer launch is a fresh session; MUST NOT inherit any prior reviewer or implementer `claude_session_id` | §4.2.HC-006; §4.10.HC-045c |
| **`session_id` source (harmonik-side `handlerSessionID`)** | Fresh UUIDv7 minted by `buildClaudeLaunchSpec` at call time | Fresh UUIDv7 minted at each `buildClaudeLaunchSpec` call (distinct from the Claude session ID being reused) | Fresh UUIDv7 minted at each `buildClaudeLaunchSpec` call | §4.2.HC-006; [event-model.md §4.1] |
| **`agent-task.md` path and content** | `<workspace_path>/agent-task.md` — contains bead body, no prior-verdict section | Same path; `AgentTaskPayload.PriorVerdictFile` + `PriorVerdictSummary` are set (prior-iteration context section rendered) | Same path; `AgentTaskPayload.ReviewBaseSHA` + `ReviewHeadSHA` are set (diff under review section rendered) | [claude-hook-bridge.md §4.x CHB-028]; `internal/workspace/agenttask_chb028.go` |
| **`LaunchSpec.phase` wire-protocol field** | `"implementer-initial"` | `"implementer-resume"` | `"reviewer"` | §4.2.HC-006 (`phase` field definition) |
| **`LaunchSpec.iteration_count` wire-protocol field** | `1` | `2` or `3` (bounded by ON-004 cap) | Same value as co-iterating implementer (daemon sets from loop counter) | §4.2.HC-006 (`iteration_count` field definition) |

**Testability note:** Each row above is independently testable. The idempotency key composition `(run_id, node_id, phase, iteration_count)` per HC-004 provides the natural test oracle: a conforming implementation MUST produce distinct `(phase, iteration_count)` tuples for distinct rows, and identical tuples MUST return the same `Session` rather than spawn a second subprocess.

**Test-hookpoint sensor:** The `internal/operatornfr/` package does not currently contain a LaunchSpec phase-coverage test that exercises all three phases of `buildClaudeLaunchSpec` end-to-end. The `reviewloopstatus_on035a_test.go` file verifies the `phase` field in the operator-visible status event surface (`TestON035a_PhaseSetIsComplete`, line 234) and serves as a partial sensor. A dedicated test asserting that `buildClaudeLaunchSpec` produces the required per-phase `argv` and `claude_session_id` distinctions would close this gap; track as a follow-up bead against `internal/daemon/` (target: `TestHC006a_PerPhaseLaunchSpecInvariants`).

Tags: mechanism

#### HC-007 — Handler subprocess emits progress-stream messages over a Unix domain socket

The handler subprocess MUST connect back to the daemon on the local Unix domain socket at `.harmonik/daemon.sock` (per [process-lifecycle.md §4.1] and §4.10.HC-044) and emit a stream of typed progress-stream messages over that connection. No other transport (named pipe, generic TCP, file tail) is permitted at MVH. The progress stream is the sole bidirectional channel between the daemon and the handler subprocess; the daemon-side watcher (§4.3.HC-011) consumes these messages and is the authoritative publisher of handler-lifecycle events to the in-process event bus.

Progress-stream messages MUST include (at minimum) the message types: `handler_capabilities`, `agent_ready`, `agent_started`, `agent_output_chunk`, `agent_completed`, `agent_failed`, `agent_rate_limited`, `agent_rate_limit_cleared`, `agent_heartbeat`, `session_log_location`, `skills_provisioned`, `outcome_emitted`. Each message corresponds to a bus event of the same name per §6.4; the watcher translates on-stream messages into bus events, applying the envelope of [event-model.md §4.1] on publication.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=best-effort; replay-safety=safe; idempotency=idempotent

#### HC-007a — Progress-stream framing is newline-delimited JSON (NDJSON)

The progress stream MUST be framed as newline-delimited JSON (NDJSON): each message is a single JSON object terminated by a single `\n` (0x0A) byte; no whitespace other than the terminating newline appears between messages; no embedded unescaped newlines appear inside a JSON object. Both directions of the bidirectional stream (daemon-to-handler control messages and handler-to-daemon progress messages) MUST use the same NDJSON framing. Handlers that cannot guarantee newline-free JSON encoding MUST NOT claim conformance. The watcher MUST enforce a **max line length of 1 MiB**; a line (byte sequence up to and including the terminating `\n`) exceeding the cap MUST abort the session with `ErrProtocolMismatch` and emit `agent_failed` with `sub_reason = "ndjson_line_too_long"`. The cap applies to the decoded framing layer and is a DoS guard, not a payload-size limit (payloads > 1 MiB are the LaunchSpec file-path path per §4.2.HC-005; outbound handler messages larger than the cap are a protocol defect).

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

#### HC-007b — Message-boundary durability on stream close

A progress-stream message is considered **observed** iff the watcher has successfully JSON-decoded the full line AND published the corresponding bus event. Partial messages at stream close — socket EOF with bytes buffered before the terminating `\n`, a decoder error on a malformed JSON object, or a line exceeding the HC-007a cap — MUST be discarded: the watcher MUST NOT synthesize a truncated or best-effort `agent_output_chunk` from partial bytes. On stream close with partial bytes pending, the watcher MUST emit `agent_failed` with class `ErrStructural`, sub-reason `partial-message`. On a syntactically invalid JSON object received on a live socket, the watcher MUST emit `agent_failed` with class `ErrStructural`, sub-reason `malformed_progress_message` and close the session (no reconnect — see §4.10.HC-044 and §4.6.HC-024a). Consequence: `agent_output_chunk` is a best-effort stream and replay on retry is NOT guaranteed to reproduce identical chunks; exactly-once chunk durability is the session-log file on disk, not the bus.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent

#### HC-008 — Outcome delivery is event-based, not exit-code-based

The handler subprocess MUST deliver the run's `Outcome` (per [execution-model.md §4.1]) as the payload of a final `outcome_emitted` progress-stream message; the daemon-side watcher translates that message into the `outcome_emitted` bus event per §6.4. Subprocess exit status MUST be treated as a liveness signal only: exit 0 = clean shutdown after `outcome_emitted`, non-zero = crash. A non-zero exit WITHOUT a preceding `outcome_emitted` progress-stream message MUST be classified as an agent failure per §4.6; the watcher emits `agent_failed` to the bus.

Tags: mechanism

#### HC-008a — Post-outcome shutdown window

After emitting the `outcome_emitted` progress-stream message, the handler subprocess MUST exit cleanly within `T_shutdown` (default: 10 seconds; pinned at MVH). The daemon MUST NOT apply silent-hang detection (§4.6.HC-026, §7.1) during the shutdown window; the regime is distinct. On expiry of `T_shutdown` without subprocess exit, the watcher MUST send SIGKILL and emit `agent_failed` with class `ErrStructural` and sub-reason `post_outcome_shutdown_timeout`. The shutdown window begins when the watcher has acknowledged `outcome_emitted` to the subscribers (bus publication completed); it ends on observed subprocess exit.

**Dirty-exit inside the shutdown window.** If the subprocess exits non-zero during the shutdown window AFTER `outcome_emitted` has been published to the bus, the outcome is durable: the watcher MUST emit `agent_completed` (NOT `agent_failed`) with an additional payload field `shutdown_exit_code` carrying the non-zero exit status for operator observability. Exactly one terminal event per session is invariant (§5 HC-INV-006). The watcher MUST complete bus-publication of any already-received terminal message before observing subprocess exit status; if `Wait()` returns while a terminal message is still pending publication, the watcher MUST publish it before emitting the exit-derived terminal event. This collapses the shutdown-window / crash race into a total order.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent

#### HC-009 — Version negotiation via handler_capabilities message

The handler subprocess MUST emit a `handler_capabilities` progress-stream message as the FIRST message on the stream, carrying its supported wire-protocol versions. The daemon MUST select the highest mutually supported version. If no version is mutually supported, the daemon MUST terminate the session and return `ErrProtocolMismatch` from `Launch`.

Tags: mechanism

#### HC-010 — Session log path emission

The handler subprocess MUST emit a `session_log_location` progress-stream message early in the session (after `handler_capabilities` and before `skills_provisioned` / `agent_ready`) carrying `{session_id, run_id, node_id, agent_type, log_path, log_format, bead_id?}`. The daemon-side watcher (S04's watcher callback per §4.3) translates that message into the bus event `session_log_location` per [workspace-model.md §4.7]; the on-the-wire payload schema is owned by [event-model.md §6.3]. By the time this message is emitted, the session-log directory and sidecar already exist per [workspace-model.md §4.7]; this message announces the path, it does not create the directory.

Tags: mechanism

### 4.3 Concurrency model

#### HC-011 — Daemon owns exactly one watcher goroutine per active session

The daemon (S01 Orchestrator Core) MUST spawn exactly ONE watcher goroutine per active handler session. The watcher owns (a) the read-loop on the handler's progress stream, (b) publication of handler-emitted events to the in-process event bus per [event-model.md §4.3], and (c) cleanup at session end. N active sessions produce N watcher goroutines. Watchers MUST NOT share state across sessions.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

#### HC-011a — Watcher liveness and panic recovery

The watcher goroutine body MUST install a `recover()` barrier: a panic within the watcher MUST be converted to `agent_failed` with class `ErrStructural`, sub-reason `watcher_panic`, and MUST NOT bring down the daemon. The same `recover()` discipline applies to subscriber goroutines: a subscriber panic MUST be isolated per-subscriber (routed through the dead-letter of §4.6.HC-027) and MUST NOT wedge the watcher's publish path. The daemon MUST maintain a per-watcher `last_read_event_at` timestamp updated on every successful socket read return (distinct from the `last_progress_event_at` of §7.1, which updates on successful message decode). A daemon-level supervisor MUST check, at cadence ≤ `T/4`, that every active watcher has advanced `last_read_event_at` within `T/2`; a watcher that has NOT (despite the subprocess being required to heartbeat at ≤ T/2 per §4.6.HC-026a) MUST be classified as a **daemon defect**, the session terminated, and `agent_failed` emitted with class `ErrStructural`, sub-reason `watcher_wedged`. This distinguishes watcher failure from agent silent-hang in the event record and prevents a wedged subscriber from being misattributed to the agent. The watcher-to-event-bus publish channel MUST have a small bounded buffer (implementation SHOULD default to 8 events); on buffer-full, the watcher MUST route to the dead-letter per §4.6.HC-027 rather than block indefinitely.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent

#### HC-012 — S04 owns the per-agent-type adapter; no per-session goroutines

The Agent Runner (S04) MUST expose one `Adapter` per registered `agent_type` and MUST NOT hold per-session state or spawn per-session goroutines. Per-session state MUST live entirely inside the watcher's stack or closure (§4.3.HC-011). The adapter's methods are synchronous callbacks invoked by the watcher on specific lifecycle events.

Tags: mechanism

#### HC-013 — Adapter surface is fixed

Every `Adapter` MUST provide: `DetectReady(event) -> bool` (ready-state recognition per §4.9), `DetectRateLimit(event) -> (limited bool, retry_after time.Duration)` (rate-limit recognition per §4.6), `CleanExitSequence(ctx, session) -> error` (orderly termination), and `RotateAccount(ctx) -> error` (for handlers supporting account rotation; MAY return `ErrDeterministic` if not supported). Adding a callback to the adapter surface requires a foundation amendment.

Tags: mechanism

#### HC-013a — RotateAccount fires only at turn boundaries

`Adapter.RotateAccount(ctx)` MUST NOT be invoked while the subprocess has an in-flight LLM turn (a turn is the interval between a work-dispatch that reaches the subprocess and the subsequent `agent_completed` of that turn, or the interval between two consecutive turn-dispatches on the same session). The watcher MUST schedule a requested rotation at the next clean turn boundary: after `agent_completed` of one turn and before the next `LaunchSpec`-delivered work dispatch in the same session. In-session rotation mid-turn is forbidden — it would interrupt an open provider HTTPS call with ambiguous recovery, leave worktree side-effects mid-transaction, and (per §4.7.HC-028) cannot mutate the spawn-time `HARMONIK_SECRET_*` environment anyway. If a rotation is requested but no quiescent turn boundary is observed within a configurable window (default: the enclosing `LaunchSpec.timeout`), `RotateAccount` MUST return `ErrTransient` and the orchestrator MAY retry after the current turn completes. Provider-secret rotation (new API-key value for the same provider) remains out of scope per §2.2; account rotation (different pool member) is in scope and travels only through this callback at turn boundaries.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

#### HC-014 — Channel closure rule

An emitter of a Go channel used across subsystem boundaries MUST close the channel on end-of-stream. Consumers MUST treat a closed channel as end-of-stream, NOT as error. This rule applies to the watcher-to-event-bus publication channel and to any future cross-subsystem channel.

Tags: mechanism

#### HC-015 — Mutex discipline

State transitions MUST acquire a per-run write lock. Event publication MUST NOT block the state lock. Event consumers MUST read from a per-subscriber channel. No mutex MAY be held across a call into S04's adapter.

Tags: mechanism

#### HC-016 — Work queue per agent role

The orchestrator MUST maintain one work queue per agent role (per [architecture.md §4.8]). Workers MUST drain their own queue; cross-queue handoff MUST go through explicit transitions per [execution-model.md §4.6], never shared memory.

Tags: mechanism

#### HC-016a — Orphan-reconnect window retry against `daemon_not_ready{reason="unknown_run_id"}`

A handler subprocess (orphan or otherwise) whose `emit-outcome` / `claim-next` / other agent-originated JSON-RPC request lands on the daemon's listener between the daemon's socket bind and the daemon's completion of its in-memory model build is rejected with the typed error `daemon_not_ready{reason="unknown_run_id"}` per [process-lifecycle.md §4.2 PL-003b]. Clients (the handler subprocess and any in-process Go-side caller wrapping the JSON-RPC) that observe this typed error MUST treat it as a bounded-retry condition and MUST retry per the ready-protocol exponential backoff of [process-lifecycle.md §4.2 PL-009b]: initial 100 ms, doubling per attempt, max 2 s per attempt, capped at `T_ready_wait = 60 s` total wall-clock (default per OQ-PL-002). The watcher MUST NOT classify a `daemon_not_ready{reason="unknown_run_id"}` response as a session-failure event during the retry window; in particular it MUST NOT emit `agent_failed` and MUST NOT escalate to silent-hang detection (§4.6.HC-026) on the basis of a still-pending startup-window retry. After cap exhaustion, the request MUST fail with the appropriate handler-side error class — `ErrTransient` if the typed error continued to fire across the entire window (the daemon never reached `ready` for our run_id, indicating either a true orphan whose run_id will never re-appear in the new daemon generation OR a daemon-side classification gap), with watcher-emitted `agent_failed` carrying class `ErrTransient` and sub-reason `daemon_startup_window_exceeded`. This rule is the handler-side companion to PL-003b and closes the orphan-agent-reconnect-during-startup-window race named there: the daemon is responsible for typed rejection, the handler is responsible for bounded retry rather than misclassifying a transient startup-window window as a permanent failure.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

### 4.4 Context propagation

#### HC-017 — Every public method takes ctx as first parameter

Every public method on `Handler` and `Session` MUST take `ctx context.Context` as its first parameter. Implementations MUST propagate `ctx` to the subprocess via the wire protocol (timeout and deadline fields in LaunchSpec; cancellation via explicit `Kill` call).

Tags: mechanism

#### HC-018 — Cancellation terminates in-flight operations within bounded intervals

A `ctx` cancellation MUST cause in-flight Go-side operations to return with `context.Canceled` (wrapped as `ErrCanceled` per §4.5) within 500ms, and MUST cause subprocess cleanup to complete within 5s. Exceeding the subprocess cleanup bound triggers escalation to hard termination per §4.6.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=best-effort; replay-safety=safe; idempotency=idempotent

#### HC-019 — Context values are restricted to observability metadata

Context values MUST NOT carry business data (run fields, outcomes, bead IDs). Context values MAY carry observability metadata only: trace IDs, correlation IDs, operator-identity tokens. Business data MUST flow via explicit parameters, LaunchSpec fields, or event payloads. This restriction applies to every cross-subsystem call.

Tags: mechanism

### 4.5 Error taxonomy

#### HC-020 — Five primary sentinel classes plus two structural sub-sentinels

The handler contract defines **five primary sentinel error classes** and **two structural sub-sentinels** that wrap `ErrStructural`. The full set, declared in §6.1 and detailed in §8, is:

- Primary: `ErrTransient`, `ErrStructural`, `ErrDeterministic`, `ErrCanceled`, `ErrBudget`.
- Sub-sentinels (each wraps `ErrStructural`): `ErrProtocolMismatch`, `ErrSkillProvisioningFailed`.

Every error returned across a subsystem boundary by a handler, session, or adapter MUST wrap (via `fmt.Errorf("...: %w", ...)`) exactly one primary class (possibly via one of the sub-sentinels). Consumers MUST use `errors.Is` or `errors.As` for class detection; string matching on error messages is forbidden. Consumers MUST dispatch on the narrowest class first (sub-sentinel before primary) when handler-specific behavior is desired; otherwise `errors.Is(err, ErrStructural)` matches both sub-sentinels and the base class uniformly.

Tags: mechanism

#### HC-021 — ErrProtocolMismatch is a structural sub-sentinel

`ErrProtocolMismatch` (emitted on version-negotiation failure per §4.2.HC-009, on `handler_capabilities` absence within 5s, or on an NDJSON line exceeding the HC-007a cap) MUST wrap `ErrStructural`. Consumers detecting `ErrProtocolMismatch` via `errors.Is` MUST also see it as `ErrStructural` via `errors.Is`. The Structural routing regime is appropriate because the re-plan that resolves `ErrProtocolMismatch` is concrete and plan-visible (not an operator-only intervention): swap `handler_ref` to a different handler, change the pinned commit hash via configuration, or upgrade the daemon. A `re-plan` that names one of these actions is not degenerate; the orchestrator's retry policy (per [execution-model.md §8.2]) MUST distinguish protocol-mismatch re-plans from transient re-plans to avoid retry-spin against the same pinned binary — see routing notes in §8.7.

Tags: mechanism

#### HC-022 — ErrSkillProvisioningFailed is a structural sub-sentinel

`ErrSkillProvisioningFailed` (emitted on skill-injection structural failure per §4.11.HC-048) MUST wrap `ErrStructural`: the plan cannot proceed as specified, but a different plan (e.g., a node with different `required_skills`) might. The outer `ErrSkillProvisioningFailed` sentinel is what consumers dispatch on when they care about skill-specific failure reporting; `ErrStructural` is the routing fallback. Transient provisioning failures (§4.11.HC-048a) do NOT wrap this sub-sentinel — they wrap `ErrTransient` directly.

Tags: mechanism

#### HC-023 — Error classification is mechanism-tagged

Mapping a subprocess exit state or adapter-detected condition to a sentinel class MUST be deterministic from structured fields (exit code, event payload flags, typed adapter return). No cognition MAY participate in classification. Semantic interpretation (is this transient? is this the same bug twice?) belongs in reconciliation-investigator nodes, not in the error classifier.

Tags: mechanism

### 4.6 Error propagation across async boundaries

#### HC-024 — Subprocess crash emits agent_failed with typed class

When a handler subprocess crashes (exits non-zero without a preceding `outcome_emitted` message per §4.2.HC-008), the watcher MUST emit an `agent_failed` bus event whose payload includes the mapped sentinel class per §4.5. Routing of that event to retry / re-plan / terminal paths is owned by [execution-model.md §8]; this spec is normative for the detection and emission rule only.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent

#### HC-024a — Socket-level I/O error is distinct from subprocess crash

A socket-level I/O error from the progress-stream read-loop — `EPIPE`, `ECONNRESET`, the socket file being unlinked or the filesystem unmounting under foot, or a decoder error per §4.2.HC-007b — MUST be distinguished from subprocess-level termination. On the FIRST occurrence of such an error without a prior `agent_completed` or `agent_failed` for the session, the watcher MUST: (a) emit `agent_failed` with class `ErrTransient` and sub-reason `socket_io_error`; (b) attempt ONE reconnect against the same socket path within a bounded window (default: 500ms). If reconnect succeeds and the subprocess is still alive, the watcher MAY resume the read-loop; if reconnect fails OR the subsequent stream emits another socket-level error before a clean terminal event, the watcher MUST reclassify to `ErrStructural` with sub-reason `progress_stream_broken`, send SIGKILL to the subprocess (the subprocess has no other channel to the daemon and any continued work is unobservable), and mark the session terminated. Sessions are single-socket-lifetime at MVH: there is no operator-visible reconnect UI. This requirement distinguishes "socket broken, subprocess alive" from silent-hang (§4.6.HC-026) and prevents the session from waiting the full silent-hang window when the cause is transport failure.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent

#### HC-025 — Rate-limit events are distinct from failure events

When the adapter's `DetectRateLimit` returns `limited=true`, the watcher MUST emit `agent_rate_limited` (NOT `agent_failed`) carrying `retry_after`. On the adapter's detection of rate-limit clearance (the session resumes producing output), the watcher MUST emit `agent_rate_limit_cleared`. Rate-limited sessions are NOT failures; the daemon's policy for rate-limit handling is exponential backoff within wall-clock budget.

> INFORMATIVE: Per [event-model.md §8.9(h)], `agent_rate_limited` and `agent_rate_limit_cleared` are a paired-phase lifecycle and are published to the bus as the merged `agent_rate_limit_status` event (§8.3.6) with a `status` field discriminating `active` / `cleared`. Consumers subscribe to `agent_rate_limit_status`, not to the progress-stream message names.

Tags: mechanism

#### HC-026 — Silent-hang detection state machine

The watcher MUST detect a silent-hang condition: the subprocess is alive (socket connected, `Wait` has not returned) but has not emitted any progress-stream message — INCLUDING heartbeats per §4.6.HC-026a — for a threshold interval `T`. Silent-hang is triggered by the absence of heartbeats, NOT by absence of `agent_output_chunk` messages; well-behaved agents under extended reasoning continue to emit heartbeats and are therefore not subject to silent-hang termination. Detection and escalation MUST follow the state machine in §7.1. The terminating error class MUST be `ErrStructural`.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent

#### HC-026a — Handler heartbeat obligation

Every handler subprocess MUST emit an `agent_heartbeat` progress-stream message at least every `T/2` seconds for as long as the subprocess is alive and has not emitted `outcome_emitted`. The heartbeat payload MUST include `{session_id, phase}` where `phase` is drawn from the extensible enum `{starting, reasoning, tool_call, waiting_input, rotating, shutting_down}`. Handlers MAY declare additional phase values in their subsystem envelope; the enum is additive-only and not a pinned contract. Handlers wrapping LLMs that have no output channel during extended reasoning MUST synthesize heartbeats on an internal timer. Silent-hang detection (§4.6.HC-026, §7.1) is keyed off the absence of ANY progress-stream message, including heartbeats; emitting a heartbeat resets the silent-hang timer per §7.1. During rate-limited windows (§4.6.HC-025), handlers MUST continue to emit heartbeats (natural phase: `waiting_input`); rate-limit and silent-hang are independent regimes. During the post-outcome shutdown window (§4.2.HC-008a), heartbeat emission is not required (silent-hang is suspended in that regime).

**Scenario-mode (scripted) carve-out.** A twin subprocess per §4.8 running against a scenario script MAY emit heartbeats at explicit scripted relative timestamps bypassing the `T/2` wall-clock timer, so that scenario tests produce byte-reproducible event streams. The carve-out is limited to the canonical twin binary and MUST be declared on the script (e.g., `heartbeat_mode: scripted`). Scenario-mode heartbeat is non-realtime by construction: scenario-harness false-positive resilience tests (per §10.2 HC-026 obligations) MUST use the wall-clock timer mode, not the scripted mode, to exercise the watcher's timer-tick guards. Real (non-twin) handlers MUST use the wall-clock timer mode only.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

#### HC-026b — Drain-forced silent-hang synthesis is ON-classified, not HC-classified

When a handler subprocess is silent-hanging during an operator-initiated drain — specifically, a drain-timeout escalation per [operator-nfr.md §4.7 ON-029] produces a SIGKILL to a still-running agent subprocess — the synthesis of the resulting `agent_warning_silent_hang{reason=drain_forced, run_id, node_id}` event is governed by [operator-nfr.md §4.9 ON-040], NOT by HC's own silent-hang taxonomy of §4.6.HC-026 / §7.1. This spec accepts ON-040's classification: the synthesized event is the operator-control subsystem's normalization of an unclean agent exit during drain step 4's wait window per ON-027, even when no prior silent-hang detection (the §7.1 state machine) had fired. The watcher MUST cooperate by NOT also emitting an HC-classified silent-hang event for the same run/node when the operator-control subsystem has signalled a drain-forced synthesis path; the synthesis is single-emitter (ON-side) by construction so as to keep HC-INV-004 ("watcher publishes exactly ONE terminal event per session") satisfied — the subsequent SIGKILL-induced subprocess exit lands as the session's `agent_failed` with the synthesis carrying the `reason=drain_forced` discriminator. This clause is an acceptance / cross-spec deferral, not an HC obligation: the enforcement mechanism lives in operator-nfr.

Tags: mechanism

#### HC-027 — Dead-letter behavior for undeliverable events

Events that cannot be delivered by the watcher to the in-process bus (bus full, subscriber panic) MUST be routed to the dead-letter destination declared by [event-model.md §4.3]. The watcher MUST NOT drop events silently.

Tags: mechanism

### 4.7 Secrets

#### HC-028 — Secrets are injected via environment variable

Secrets (API keys, tokens) MUST be delivered to the handler subprocess as environment variables with the stable prefix `HARMONIK_SECRET_*`. LaunchSpec MUST NOT carry secret values in any field; the fact that a given secret is required MAY be encoded in LaunchSpec indirectly (via `freedom_profile_ref` resolution), but the value itself travels only via process environment.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=unsafe; idempotency=idempotent

#### HC-029 — agent_started event MUST NOT include environment variables

The `agent_started` event payload MUST NOT include the handler subprocess's environment. Launch provenance fields (binary path, commit hash) are permitted; environment variable maps are not.

Tags: mechanism

#### HC-030 — Redaction registry middleware

The daemon MUST install a redaction-middleware function in the event-bus producer path that applies the rules of §4.7.HC-031 and §4.7.HC-032 to every event payload and every structured log line before emission. The middleware is mechanism-tagged; no cognition participates.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

#### HC-031 — Common prefix redaction rule

Any field whose NAME matches the case-insensitive regex `(secret|token|password|api[_-]?key|auth)` MUST be replaced with the literal string `"<redacted>"` before emission. The match is on field name, not value; intentional serialization of a field whose name matches the regex is a bug at the producer, not a redaction bypass.

Tags: mechanism

#### HC-032 — Per-handler redaction patterns

Each handler spec MAY contribute additional value-shaped regex patterns for handler-specific secret formats (e.g., Anthropic API keys matching `sk-ant-*`). Patterns MUST be declared in the handler's subsystem envelope as redaction entries and registered at daemon init. Values matching ANY registered pattern MUST be replaced with `"<redacted>"` before emission.

A handler whose secrets have a known value-shape but omits the corresponding pattern is a spec defect caught by review: HC-INV-003 (no secret in event log) depends on both the common-prefix rule of HC-031 AND on per-handler value patterns of HC-032 being kept in sync with the handler's provider. Review MUST reject a handler spec that ships a secret provider without a declared pattern when one is shape-detectable.

Tags: mechanism

#### HC-033 — Compile-time event-schema payload check

The event-schema registry MUST verify at startup that no registered event type's payload schema declares a field whose name matches the regex of §4.7.HC-031. Any such field MUST be a startup-time error, not a runtime warning. This prevents schema drift that would silently ship unredacted secrets.

Tags: mechanism

#### HC-034 — Secrets never appear in event log or unredacted session log

No secret value MAY appear in any persisted event record, audit record, or session log as stored to disk. Operator debugging of secret-related failures MUST use redacted forms; full-secret access requires filesystem-level privileges outside harmonik's control surface.

Tags: mechanism

### 4.8 Twin parity

#### HC-035 — Twins (as real-handler substitutes) implement the same Handler interface

A **twin handler** — the canonical real-handler substitute used by the scenario-test harness and in CI — MUST implement the `Handler` interface of §6.1 with the SAME method signatures and the SAME error-class discipline as a real handler, AND MUST be launched as a separate subprocess (per §4.10.HC-045). Selection between a real handler and its twin is config-level per §4.1.HC-003.

**Carve-out for unit-test fakes.** Hand-written in-process fakes of the `Handler` and `Session` interfaces — used exclusively in Go unit tests for per-adapter, per-watcher, or per-error-classifier targeted testing — are NOT twins in the sense of this section. They are not required to be subprocesses, not required to honor the wire protocol end-to-end, and not subject to §4.8 parity constraints. The twin-parity requirement applies ONLY to the canonical twin binary used as the real-handler substitute in scenario-harness tests and CI; it does NOT forbid in-process `Handler` implementations written for targeted unit tests.

Tags: mechanism

#### HC-036 — Twin subprocesses honor the same wire protocol

A twin handler subprocess (per HC-035) MUST emit the same progress-stream message types (per §4.2.HC-007 and §4.2.HC-007a) and MUST obey the same outcome-delivery rule (per §4.2.HC-008) as its corresponding real handler. The only permitted differences are: (a) the subprocess script drives output instead of an LLM; (b) model budget is not charged against the operator's provider account; (c) the binary name is `<real>-twin` (e.g., `claude-twin`) or a declared alias in configuration. This requirement applies to the twin subprocess; unit-test in-process fakes per HC-035's carve-out are unaffected.

Tags: mechanism

#### HC-036a — Twin script-file format

Tags: mechanism

A twin subprocess (per HC-035) that operates in scenario mode MUST read its output from a YAML script file. The script-file format is normatively defined here; implementors MUST NOT deviate from this schema.

**File location.** The script file MUST reside at:

```
<fixture-root>/<scenario>/twin-scripts/<role>.yaml
```

where `<fixture-root>` is the suite-level ephemeral root per [scenario-harness.md §4.4 SH-016a], `<scenario>` is the scenario name per [scenario-harness.md §4.1 SH-005], and `<role>` is the agent role string declared in the scenario's `agent_overrides` map.

**Top-level YAML fields.** The script file MUST be a YAML mapping with the following top-level fields:

| Field | Type | Required | Default | Description |
|---|---|---|---|---|
| `heartbeat_mode` | string enum | No | `wall_clock` | Controls how heartbeats are timed. MUST be one of `wall_clock` or `scripted`. Absence or empty string is treated as `wall_clock`. Any other value MUST be rejected with a load error. |
| `messages` | list of ScriptMessage | No | `[]` | Ordered list of progress-stream messages to emit. Absence or null is treated as an empty list; the driver exits immediately without emitting. |

**ScriptMessage record.** Each entry in `messages` MUST be a YAML mapping with the following fields:

| Field | Type | Required | Constraint |
|---|---|---|---|
| `type` | string | Yes | Non-empty. The driver emits this value verbatim as the `type` field per §4.2.HC-007 NDJSON framing. Callers MUST use a type declared in §4.2 (e.g., `agent_heartbeat`, `agent_output_chunk`, `outcome_emitted`). |
| `payload` | map (string → any) | No | Optional key-value pairs merged into the emitted JSON object alongside `type`. Callers MUST include all fields required by the wire schema for the declared `type` (§4.2.HC-007, §6.4, [event-model.md §8.3.*]); the driver does NOT synthesize missing fields. The `type` key in `payload` is silently overwritten by the top-level `type` field; scripts MUST NOT rely on overriding `type` via `payload`. |
| `relative_timestamp_ms` | int | No | Milliseconds to wait before emitting this message, measured from the previous message (or script start for the first message). MUST be `>= 0`; negative values MUST be treated as `0` (emit immediately). Honoured only when `heartbeat_mode` is `scripted`; ignored when `heartbeat_mode` is `wall_clock`. |

**Heartbeat-mode semantics.** When `heartbeat_mode` is `wall_clock`, the driver MUST emit all messages in declaration order without any inter-message delay derived from `relative_timestamp_ms`; the real-time `T/2` wall-clock timer (per §4.6.HC-026a) governs heartbeat timing. When `heartbeat_mode` is `scripted`, the driver MUST wait `relative_timestamp_ms` milliseconds before emitting each message (or zero milliseconds when the field is absent or zero), implementing the HC-026a scripted-mode carve-out for byte-reproducible scenario test streams. The scripted carve-out is limited to the canonical twin binary; real handlers MUST NOT use scripted mode.

**Validation.** The twin binary MUST reject the script file and fail with a non-zero exit at load time if: (a) the YAML is syntactically malformed; (b) `heartbeat_mode` carries a value other than `wall_clock` or `scripted`; (c) any `messages` entry has an empty or absent `type`. A load-time rejection MUST produce an operator-readable error message identifying the file path and the violation.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

#### HC-037 — Twins carry identical boundary-classification tags

A twin handler's mechanism/cognition tagging MUST match its real counterpart's. A twin does NOT make decisions its real counterpart would delegate; it scripts them. Any deviation in tagging is a twin defect.

Tags: mechanism

#### HC-038 — Twin conformance drift detection is scoped to S07

The ongoing obligation to keep twins honest against real-agent drift is scoped to the scenario-harness (S07) spec, post-MVH. This spec establishes the parity contract; S07 owns the drift-detection workflow.

Tags: mechanism

### 4.9 Ready-state detection

#### HC-039 — Ready-state is signaled by agent_ready event

Every handler subprocess MUST emit a single `agent_ready` event on process startup to signal it can accept work. The event payload MUST include `session_id` and `capabilities[]` at minimum; the full payload schema is declared in [event-model.md §6.3]. The daemon MUST NOT dispatch work to a session before observing `agent_ready` for that session.

Tags: mechanism

#### HC-040 — Twins MUST emit agent_ready identically

Twin handlers MUST emit `agent_ready` with the same shape and timing as their real counterparts. Skipping the signal in twins is a twin-parity violation per §4.8.

Tags: mechanism

#### HC-041 — Adapter-level ready detection

The adapter's `DetectReady` callback (per §4.3.HC-013) MUST return `true` exactly when it has observed an `agent_ready` event for the session in question. Adapters MUST NOT synthesize ready-state from other signals (e.g., first output chunk).

Tags: mechanism

#### HC-056 — `agent_ready` timeout

The daemon MUST observe an `agent_ready` event from each launched session within `agent_ready_timeout`. The default value is **30 seconds**; operators MAY tune via `Config.AgentReadyTimeout`.

**Timeout window start point — substrate-dependent:**

- **Interactive (tmux) substrate** (`agent_type = "claude-code"` under [process-lifecycle.md §4.7 PL-021b]): the timeout window starts at `SubstrateSpawn` return (i.e., the moment `tmux new-window` has succeeded and the pane is alive). The daemon then waits for the relay-synthesized `agent_ready` event (per [claude-hook-bridge.md §4.5 CHB-013], `provenance: "claude_session_start"`). This is the first claude-originated lifecycle signal and is the correct measurement point for "is claude alive and ready to accept work."
- **Conventional subprocess substrate**: the timeout window starts at `cmd.Start()` return as before.

Under the tmux substrate, `launch_initiated` (the handler's pre-exec emission per [claude-hook-bridge.md §4.7 CHB-018] step 4) MUST NOT reset or satisfy this timeout. The 30 s window is measured from `SubstrateSpawn` return to relay-synthesized `agent_ready` receipt, encompassing: tmux pane initialization, claude process startup, `.claude/` filesystem warm-up, hook system registration, and first `SessionStart` hook relay round-trip to the daemon socket.

On timeout, the daemon MUST:

1. Cancel the session's context (which triggers `Session.Kill`).
2. Reap the subprocess via `Session.Wait`.
3. Emit `agent_failed{class=structural, sub_reason=agent_ready_timeout, exit_code=<observed>}`.
4. Reopen the bead with reason `agent_ready_timeout`.

The 30 s default is informed by claude's observed cold-start latency (≤ 5 s typical, 10–15 s under cold disk caches), plus hook-relay round-trip overhead (≤ 2 s including socket dial, NDJSON write, and ACK). The margin accommodates skill provisioning, one-time `.claude/` filesystem warm-up, and the relay's `daemon_not_ready` retry budget (≤ 25 s per [claude-hook-bridge.md §4.6 CHB-016]). Tighten in a follow-up bead once telemetry from real-claude smokes lands.

The timeout MUST fire from the same goroutine that owns the session's lifecycle to ensure ordered Kill/Wait. Concurrent `agent_ready` arrival and timeout-expiry race is resolved in favour of `agent_ready` (last-second arrival wins).

Cross-refs: HC-039 (emitter identity), HC-041 (DetectReady), [claude-hook-bridge.md §4.5 CHB-013] (SessionStart → agent_ready mapping), [claude-hook-bridge.md §4.7 CHB-018] (launch_initiated precursor, agent_ready gating), [claude-hook-bridge.md §4.7 CHB-020] (terminal-event mapping). Closes follow-up bead `hk-do7te`.

Tags: mechanism
Axes: llm-freedom=mechanical; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

#### HC-057 — Heartbeat-emission ownership for `claude-code` at MVH

For `agent_type == "claude-code"`, the daemon MAY emit `agent_heartbeat{phase:"reasoning"}` events on the handler-process's behalf at the [claude-hook-bridge.md §4.7 CHB-019] cadence (300 s). This is a permissive carve-out from CHB-019's "handler-process emits" language, justified by the absence of a distinct claude-handler wrapper binary at MVH. Subscribers MUST treat daemon-emitted heartbeats as semantically equivalent to handler-emitted heartbeats; no payload distinction is required.

Post-MVH, when a `harmonik claude-handler` shim binary lands, heartbeat emission MUST migrate to the shim and this clause is retired.

Cross-ref: [claude-hook-bridge.md §4.7 CHB-019].

Tags: mechanism
Axes: llm-freedom=mechanical; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

### 4.10 Agent-to-orchestrator trust (MVH)

#### HC-042 — Handler subprocess launched from repo-relative path

Every handler subprocess MUST be launched via an absolute path resolved from a repo-relative prefix configured at daemon startup. `$PATH` lookup is forbidden for in-repo handler binaries. A handler whose `agent_type` declaration carries `system_handler=true` in configuration (e.g., a Claude Code CLI installed by the operator) MAY resolve via `$PATH`; all other handlers MUST NOT use `$PATH` and MUST fail launch if the configured absolute path is absent.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

#### HC-043 — Commit-hash check for in-repo binaries

Before launch, the orchestrator MUST verify an in-repo handler binary's embedded commit hash matches the expected hash configured for the agent type. Mismatch MUST fail launch with `ErrStructural` and emit an `agent_failed` event carrying the mismatch details. System handlers MAY log the `--version` output at startup in lieu of a hash check; no signature verification is performed at MVH.

Tags: mechanism

#### HC-044 — Subprocess is a child of the daemon

The daemon MUST spawn every handler subprocess as a direct child process (per [process-lifecycle.md §4.5]). The handler subprocess MUST communicate back to the daemon on the Unix domain socket at `.harmonik/daemon.sock` (per [process-lifecycle.md §4.1]); this is the same socket that carries the progress stream per §4.2.HC-007 and §4.2.HC-007a. There is one bidirectional socket-backed channel per session; there is no separate "control channel" at MVH. Socket authenticity is filesystem-permission-based for MVH (daemon socket MUST be mode `0600` owned by the daemon user); per-connection challenges are deferred post-MVH. On Linux, handler subprocesses SHOULD install `PR_SET_PDEATHSIG(SIGTERM)` at spawn time; macOS has no equivalent and subprocess survival across daemon death is a platform reality addressed by §4.10.HC-044a.

Tags: mechanism

#### HC-044a — Launch MUST fail-fast on orphan-held workspace

Before `Launch` returns a `Session`, the daemon MUST verify that the target `workspace_path` is NOT held by a prior-generation handler subprocess. Detection mechanism: the daemon MUST maintain a pidfile at `.harmonik/worktrees/<run_id>/.lock` written atomically at subprocess spawn and removed on clean session termination. On `Launch`, if the pidfile exists AND the recorded PID is live (liveness probe via `kill(pid, 0)` or platform equivalent) AND the live process is NOT owned by the current daemon generation, `Launch` MUST return `ErrStructural` with sub-reason `workspace_held_by_orphan` and emit `agent_failed` carrying the offending PID for operator attention. The daemon MUST NOT silently reclaim the workspace: two concurrent subprocesses writing to the same worktree is the one scenario in this spec that can silently corrupt committed artifacts, so fail-fast is mandatory. Stale pidfiles (PID not live, or PID recycled to a non-handler process identifiable by argv check) MAY be reclaimed by the new generation. This requirement is a minimum-surface stub that the reconciliation subsystem's startup sweep (per [reconciliation/spec.md §4]) will subsume post-MVH; until then, OQ-HC-006's cross-generation GC default is "reconciliation owns it, handler-contract owns fail-fast." The socket file at `.harmonik/daemon.sock` from a prior generation MUST be unlinked before `bind` by the new daemon generation per [process-lifecycle.md §4.1].

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

#### HC-045 — Twin binaries obey the same launch rules

Twin binaries MUST also be launched from a known repo-relative path with an expected-commit-hash check. The twin's expected commit hash MUST be pinned at workflow/policy configuration time.

Tags: mechanism

#### HC-045a — Pointer to claude-hook-bridge.md for claude-code agent type

For `agent_type = "claude-code"`, the launch mechanism, `.claude/settings.json` materialization, hook-event-to-progress-message translation, env-var schema, and failure-mode classification are normatively defined by [claude-hook-bridge.md]. This spec (handler-contract) defines the cross-handler invariants; the bridge spec defines the claude-code-specific realization.

Tags: mechanism

#### HC-045b — Hook-bridge connection regime

Some handler subsystems (notably the claude-code bridge per [claude-hook-bridge.md]) cause additional short-lived subprocesses to be spawned by the agent subprocess. These short-lived subprocesses MAY open one-shot NDJSON connections to the daemon socket (per HC-007's transport, per HC-007a's framing) carrying a single progress-stream message and then closing. Such connections MUST carry both `run_id` and `claude_session_id` in the message envelope at the top level (so the daemon's connection acceptor can route the message to the correct session-bound watcher). Such connections are NOT subject to HC-007's "sole bidirectional channel" phrasing (which scopes to the handler subprocess itself, not to incidental short-lived subprocesses spawned by the agent). HC-INV-007 (watcher is sole authoritative publisher) is preserved because the watcher publishes all messages from all connection regimes to the bus.

Per-connection lifetime requirements: dial timeout ≤ 5 s, single message ≤ 1 MiB per HC-007a, optional ack-line read with 5 s deadline, then close. Failure modes are classified per HC-020 and routed through `agent_failed` per HC-024.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent

#### HC-045c — Handler-side claude_session_id minting and resume

For `agent_type = "claude-code"`, the handler subprocess MUST observe the following session-id lifecycle:

(a) For `phase ∈ {single, implementer-initial, reviewer}`: the handler MUST mint a fresh UUIDv7 as `claude_session_id`, pass it to Claude via `--session-id <claude_session_id>`, AND include `claude_session_id` in the payload of the `handler_capabilities` progress-stream message per §4.2.HC-009.

(b) For `phase = implementer-resume`: the handler MUST reuse `LaunchSpec.claude_session_id` (carried from the prior iteration, populated by the daemon per §4.2.HC-006), pass it to Claude via `--resume <claude_session_id>` (NOT `--session-id`), and include the same value in `handler_capabilities`.

(c) The handler MUST NOT pass `--fork-session`, `--bare`, or `--no-session-persistence` flags to Claude, and MUST NOT set the env var `CLAUDE_CODE_SKIP_PROMPT_HISTORY`; these flags / vars conflict with bridge invariants per [claude-hook-bridge.md §4.2 CHB-007].

(d) Each reviewer phase MUST mint a fresh `claude_session_id`; the handler MUST NOT inherit reviewer claude_session_id across iterations.

(e) Orphan-reconnect lookups (per §4.3 HC-016a) MUST resolve `claude_session_id` from `Run.context.claude_session_id` reconstructed per [execution-model.md §4.7 EM-031] from the git checkpoint trail; JSONL-tail reads MUST NOT be used as the source of truth for `claude_session_id`.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent

#### HC-055 — Allowed `claude` CLI flags at MVH

The daemon's claude-launch path MUST construct `argv` from exactly the following allow-list:

- `--session-id <uuid>` — passed when phase is `single`, `implementer-initial`, or `reviewer` ([claude-hook-bridge.md §4.3 CHB-008]).
- `--resume <uuid>` — passed when phase is `implementer-resume` (CHB-008).
- `--model <value>` — passed when `LaunchSpec.model_preference.model` is non-empty (per §4.10.HC-055a). Optional; omitted when the resolution chain produces empty.
- `--effort <value>` — passed when `LaunchSpec.model_preference.effort` is non-empty (per §4.10.HC-055a). Optional; omitted when the resolution chain produces empty.

Both `--model` and `--effort` are optional and are omitted entirely when the corresponding `ModelPreference` field is empty (i.e., the built-in fallback tier was reached for that field).

Operator-supplied additional arguments (`Config.HandlerArgs`) are appended after the allow-listed flags and forwarded verbatim. `Config.HandlerArgs` MUST be validated against the [claude-hook-bridge.md §4.2 CHB-007] deny-list before exec.

The daemon also MUST add `--dangerously-skip-permissions` to argv when the launch CWD canonicalizes to a path under the harmonik-owned worktree root, per §4.10.HC-055b.

The following flags MUST NOT be passed at MVH (in addition to the CHB-007 deny-list):

- `--print` / `-p` — incompatible with interactive tmux substrate.
- `--add-dir` — workspace boundary is `cmd.Dir`; additional dirs defer to a follow-up bead.
- `--allowed-tools`, `--disallowed-tools` — tool policy lives in worktree-materialized `.claude/settings.json` ([claude-hook-bridge.md §4.1 CHB-001..005]); CLI overrides would silently shadow policy.
- `--mcp-server`, `--mcp-config` — out of scope. Follow-up bead.
- `--permission-mode` — same shadowing concern.

A daemon that detects any of these flags in `Config.HandlerArgs` MUST refuse to launch with a structural `forbidden_claude_flag` error.

Cross-refs: [claude-hook-bridge.md §4.2 CHB-006] (env), [claude-hook-bridge.md §4.2 CHB-007] (forbidden flags), [claude-hook-bridge.md §4.1 CHB-001..005] (settings.json materialization), §4.10.HC-055b (worktree path-check for `--dangerously-skip-permissions`).

Tags: mechanism, security-relevant
Axes: llm-freedom=mechanical; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

#### HC-055b — Worktree path-check for `--dangerously-skip-permissions`

**Rationale:** Claude Code's interactive trust prompt is triggered when the CWD is not in the user's trusted-projects list. In an operator-managed daemon launch, the CWD is always a harmonik-owned worktree that has already been materialized and trust-seeded (CHB-029 / WM-040b). Passing `--dangerously-skip-permissions` in this context does not widen the security perimeter beyond what the operator has already sanctioned; it merely bypasses the interactive confirmation that would otherwise hang the unattended session. Operator-launched (human-at-keyboard) sessions MUST NOT receive this flag.

**Path-check rule:** The daemon MUST perform the following check before adding `--dangerously-skip-permissions` to `argv`:

1. Resolve `workspacePath` via `os.EvalSymlinks` to obtain `canonicalWorkspace`.
2. Resolve the harmonik worktrees root (`<projectDir>/.harmonik/worktrees`) via `os.EvalSymlinks` to obtain `canonicalWorktreeRoot`.
3. Add `--dangerously-skip-permissions` to `argv` if and only if `canonicalWorkspace` has `canonicalWorktreeRoot+string(os.PathSeparator)` as a prefix (positive-allowlist match).

**Implementation constraint:** The check MUST be a positive-allowlist match against the harmonik worktrees prefix, NOT a negative-allowlist against a set of "other" paths. If `EvalSymlinks` fails for either path (e.g. the directory does not yet exist), the flag MUST be omitted and no error is returned.

**HC-055b-1 (settings.json workaround obsolescence):** This rule OBVIATES any prior `dangerouslyAllowedPermissions` field in the worktree-materialized `.claude/settings.json`. The workaround code MUST be removed in the same commit that adds this path-check; the CLI flag replaces it.

Tags: mechanism, security-relevant
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

#### HC-055a — ModelPreference descriptor invariants

`LaunchSpec.model_preference` carries the `ModelPreference` descriptor resolved at claim time per [execution-model.md §4.3.EM-012b]. The following invariants apply at the harmonik layer:

**Shape invariants (harmonik-enforced at claim time):**
- `model`: when non-empty, MUST match the regex `^[A-Za-z0-9._:/-]+$` (allows alias forms such as `sonnet`, version-pinned forms such as `claude-sonnet-4-6`, and provider-prefixed forms such as `anthropic/claude-sonnet`; rejects shell metacharacters). MUST NOT exceed 128 characters (defensive bound against argv overflow and log-line explosion; the constraint is generous enough to accommodate all known model name forms). An empty `model` field indicates the built-in fallback tier was reached; the underlying tool applies its own default.
- `effort`: when non-empty, MUST be one of `{low, medium, high, xhigh, max}`. The enum is closed at the harmonik layer because the effort flag shape is shared across all current claude-family handlers; future handler families with a different effort vocabulary are expected to declare their own mapping at the handler-spec level.

**Value opacity invariant (normative):** Harmonik validates the **shape** of `model`, not its **value**. A non-empty `model` string that passes the regex and length checks is forwarded to the handler without semantic interpretation. Future handler types (codex, pi, and others) may accept arbitrary model strings; a closed enum of model values at the harmonik layer would prevent forward-compatibility. Handler-side launch failure (the subprocess exits non-zero or the adapter returns a typed error before `agent_ready`) is the authoritative compatibility check for whether the resolved model is accepted by the underlying tool.

**LaunchSpec field:** `model_preference` is an optional LaunchSpec field (per §6.1 and [execution-model.md §6.1]). When both `model` and `effort` are empty, the field MAY be omitted from the serialised LaunchSpec; handlers MUST treat an absent field as `ModelPreference{model: "", effort: ""}`.

**Translation to argv:** The handler receives `ModelPreference` from LaunchSpec and translates it to handler-tool-specific argv. For `agent_type = claude-code`: `--model <model>` and `--effort <effort>` per HC-055. If the tool rejects the value at exec (non-zero exit, tool-side error message), the handler MUST surface the failure as `ErrStructural` with sub-reason `model_rejected_by_tool`.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

### 4.11 Skill injection

#### HC-046 — Handler MUST provision required skills before agent begins work

A handler MUST ensure the agent subprocess has every skill named in `LaunchSpec.required_skills[]` available in an agent-type-specific shape (one or more of: file drops into an agent-visible directory, CLI binaries on `$PATH`, MCP-server registrations, reference-doc bundles; additional shapes are permissible as declared by future per-handler specs) before the subprocess **begins work**, where "begins work" is the emission of `agent_ready` per §4.9. Provisioning MUST complete before that message fires. Until the `agent-configuration` spec lands, handlers MUST resolve skill packages against the directory layout declared in `[docs/foundation/components.md §10]` (bootstrap; migrates to `specs/agent-configuration.md` when finalized).

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

#### HC-047 — Skill resolution is mechanism-tagged

The mapping from a declared skill name to an available skill package MUST be deterministic: resolve the name against `LaunchSpec.skill_search_paths[]` in order, take the first match. No cognition participates. This is a split from HC-046: HC-046 declares the obligation (provisioning); HC-047 declares the resolution mechanism.

Tags: mechanism

#### HC-048 — Fail-launch on unresolvable required skill (structural)

If any name in `required_skills[]` cannot be resolved against `skill_search_paths[]` at launch time, the handler MUST fail-launch with `ErrSkillProvisioningFailed` (wrapping `ErrStructural` per §4.5.HC-022). The handler MUST emit (via the progress stream) an `agent_failed` message carrying the unresolved skill name; the watcher publishes the corresponding bus event. The run MUST NOT proceed: a different plan is required to resolve the structural failure.

Tags: mechanism

#### HC-048a — Retry with backoff on transient provisioning failure

If a skill name **resolves** against `skill_search_paths[]` but **provisioning of the resolved package into the agent-process shape fails** at runtime — network fetch timeout, package registry 5xx, disk-full during copy, MCP registration handshake network error — the handler MUST classify per the adapter's per-agent-type heuristic: transient conditions (network errors, 5xx, timeout) wrap `ErrTransient`; structural conditions (package-integrity check failure, unsupported manifest shape, permission denied) wrap `ErrSkillProvisioningFailed`. Transient classifications MUST be retried in-handler with exponential backoff (base 1s, cap 16s, max 4 attempts) bounded by `LaunchSpec.provisioning_timeout` (default 60s). On timeout or attempt-cap exhaustion, reclassify to `ErrStructural` per §8.2 and fail-launch.

`provisioning_timeout` is distinct from `LaunchSpec.timeout`: it bounds provisioning only and does NOT consume the wall-clock work budget.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=best-effort; replay-safety=safe; idempotency=idempotent

#### HC-049 — Emit skills_provisioned before agent_ready

After successful provisioning and before `agent_ready`, the handler MUST emit a `skills_provisioned` event (payload schema in [event-model.md §6.3]) carrying the set of installed skills, the source path resolved for each skill, and the skill package version where available.

Tags: mechanism

#### HC-049a — Twin-parity for skill provisioning is wire-only

The twin-parity invariant (§4.8) applies to the **wire signal** of skill provisioning — the `skills_provisioned` event — not to the filesystem side effects. A twin handler MAY skip the filesystem and process-state side effects of real provisioning (file drops, CLI installations, MCP registrations) BUT MUST emit the same `skills_provisioned` event with the same claimed skill set as if provisioning had occurred. Downstream nodes that inspect worktree artifacts to verify skill installation are NOT part of the handler-contract parity surface; such inspection is a test harness concern and SHOULD use the event payload as the authoritative record, not the filesystem. A twin that fails to emit `skills_provisioned` with the declared set violates HC-INV-002 even if the filesystem is untouched.

Tags: mechanism

#### HC-050 — Skill declarations are read from LaunchSpec

The handler MUST consult `LaunchSpec.required_skills[]` and `LaunchSpec.skill_search_paths[]` only; it MUST NOT read DOT node attributes or YAML policy documents directly. The workflow-load-time resolution per [execution-model.md §4.9] and [control-points.md §4.11] is what populates LaunchSpec; the handler consumes the resolved set.

Tags: mechanism

### 4.12 Handler as modularity boundary

#### HC-051 — Handler contract is the deterministic-daemon / execution-shape seam

The handler contract (this spec, in its entirety) is the architectural seam between the deterministic daemon (workflow state, routing, dispatch per [process-lifecycle.md §4.6]) and the execution shape (ntm + tmux + subprocess today). A proposal that would couple the daemon to a specific execution shape (e.g., importing ntm-specific types into the daemon's routing logic) MUST fail this boundary check.

Tags: mechanism

#### HC-052 — Execution-shape evolution re-implements the adapter, not the watcher

A future replacement of the execution shape (a custom tmux + agent-profile library, a cloud-execution shape, a remote-container shape) MUST re-implement the per-agent-type adapter of §4.3.HC-012 without altering the watcher of §4.3.HC-011. The concurrency pin is load-bearing: new shapes provide new adapters; they do not move the concurrency boundary.

Tags: mechanism

#### HC-053 — Cross-subsystem surface MUST remain stable across shape evolution

The cross-subsystem surface defined by this spec — the `Handler` interface (§6.1), the `LaunchSpec` record (§6.1), the error taxonomy (§8), the emitted event set (§4.2.HC-007) — MUST remain stable across execution-shape evolution. An execution-shape change that alters any of these surfaces is a breaking change requiring a foundation amendment.

Tags: mechanism

## 5. Invariants

#### HC-INV-001 — Exactly one watcher goroutine per active session

For every active handler session tracked by the daemon, there MUST exist exactly one live watcher goroutine owned by S01. Zero watchers with an active session is a daemon defect; more than one watcher per session is a daemon defect. This invariant is observable via the daemon's health-check surface per [operator-nfr.md §4.9].

Tags: mechanism

#### HC-INV-002 — Twin handlers are indistinguishable from real handlers to the daemon

From the daemon's perspective (interface surface, wire protocol, event schema, error-class discipline, tagging), a twin handler MUST be indistinguishable from its real counterpart. The daemon MUST carry zero conditional logic that varies on handler-is-twin. Verification: reviewing the daemon codebase yields zero `if isTwin` / `if agent_type == "*-twin"` branches.

Tags: mechanism

#### HC-INV-003 — No secret value crosses the event-bus or log-emission boundary

No secret value MAY be observable in any event payload delivered to any consumer, in any persisted event record, or in any structured log line written to disk. The redaction registry (§4.7) enforces this at producer time; the compile-time schema check (§4.7.HC-033) closes the schema-drift path.

Tags: mechanism

#### HC-INV-004 — agent_ready precedes work dispatch

For every session, the sequence `handler_capabilities` → `session_log_location` → `skills_provisioned` → `agent_ready` → `Launch returns` → (first work dispatch) MUST hold. The watcher MUST NOT publish `agent_ready` to subscribers before it has delivered `handler_capabilities`, `session_log_location`, and `skills_provisioned` to subscribers in that order; the daemon MUST NOT dispatch work to a session before that session's `agent_ready` bus event has been published.

Tags: mechanism

#### HC-INV-005 — No handler subprocess is launched without a verified binary path

For every successful `Launch`, the binary that was exec'd MUST have passed the launch-path and commit-hash rules of §4.10.HC-042 and §4.10.HC-043 (or, for system handlers declared via `system_handler=true`, the `$PATH`-resolved absolute path and `--version` log). A `Launch` return value of `(Session, nil)` where the verification did not occur is a daemon defect. This invariant is observable via launch-path audit logging per [operator-nfr.md §4.9].

Tags: mechanism

#### HC-INV-006 — Exactly one terminal event per session

For every session that crosses the `agent_ready` threshold (i.e., was successfully launched), the watcher MUST publish exactly ONE terminal event to the bus, chosen from `{agent_completed, agent_failed}`. `agent_completed` fires on clean exit after `outcome_emitted` OR on dirty exit inside the post-outcome shutdown window per §4.2.HC-008a (outcome is durable, non-zero exit is recorded via `shutdown_exit_code`). `agent_failed` fires on any other terminal condition (crash without outcome, silent-hang, socket break, watcher wedge, protocol mismatch, skill-provisioning structural failure). Double terminal emission is a daemon defect. `budget_exhausted` (§8.5) is a pre-launch denial and does NOT count as a terminal event for this invariant; sessions denied at budget check never launched. On any dirty exit (exit code non-zero, no prior `agent_completed` or `agent_failed` published, no `outcome_emitted` received) the watcher MUST emit `agent_failed` — silent termination without a terminal event is forbidden.

Tags: mechanism

#### HC-INV-007 — Watcher is the sole authoritative publisher of handler-lifecycle events

For every handler-lifecycle event type enumerated in §6.4 and §4.2.HC-007, the session watcher (§4.3.HC-011) MUST be the SOLE publisher to the in-process event bus. No other component — including in-process `Handler` fakes per the §4.8.HC-035 carve-out — MAY publish a handler-lifecycle event directly. In-process fakes per HC-035 MUST route their emissions through the same watcher and redaction middleware (§4.7) that a real-subprocess-backed session uses; a fake that bypasses the watcher bypasses HC-INV-003 (redaction) and HC-INV-004 (ordering) by construction. This invariant makes the watcher's position as the redaction and ordering enforcement point normative rather than descriptive.

Tags: mechanism

## 6. Schemas and data shapes

### 6.1 Interface and record schemas

```
INTERFACE Handler:
    Launch(ctx, spec) -> (Session, error)     -- starts an agent subprocess; idempotent on (spec.run_id, spec.node_id); returns ErrProtocolMismatch, ErrSkillProvisioningFailed, or other sentinel on failure
    AgentType() -> String                      -- returns the lowercase-hyphenated agent_type identifier per [architecture.md §6.1]
```

```
INTERFACE Session:
    ID() -> SessionID                              -- stable identifier for this session; assigned at Launch return
    SendInput(ctx, input) -> error                 -- delivers input to the running agent; typed sentinel on failure
    Attach(ctx) -> (io.Reader, error)              -- returns a reader over the session's tmux or log tail for operator-facing observability
    Kill(ctx) -> error                             -- signals the subprocess to exit; safe to call multiple times
    Wait(ctx) -> (Outcome, error)                  -- blocks until the subprocess terminates; safe to call multiple times; returns the Outcome from the final outcome_emitted event, or a typed sentinel on crash
    LogLocation() -> String                        -- returns the absolute session-log path emitted in session_log_location
```

```
INTERFACE Adapter:
    DetectReady(event) -> Bool                                    -- true iff event is this session's agent_ready
    DetectRateLimit(event) -> (Bool, Duration)                    -- (limited, retry_after); non-zero retry_after implies limited=true
    CleanExitSequence(ctx, session) -> error                      -- orderly termination on normal cancellation
    RotateAccount(ctx) -> error                                   -- rotate the provider account; ErrDeterministic if unsupported for this agent type
```

```
RECORD LaunchSpec:
    run_id                : UUID                    -- [execution-model.md §4.3 Run]
    workflow_id           : UUID                    -- [execution-model.md §4.1 Workflow]
    node_id               : String                  -- node_id within workflow
    agent_type            : String                  -- [architecture.md §6.1 Agent type identifier]
    workspace_path        : String                  -- absolute path to the run's worktree per [workspace-model.md §4.1]
    required_skills       : List<String>            -- resolved skill names per [control-points.md §4.11]
    skill_search_paths    : List<String>            -- ordered list of absolute paths to search for skill packages
    timeout               : Integer                 -- wall-clock seconds for the run's work; positive; zero forbidden
    provisioning_timeout  : Integer                 -- seconds for skill provisioning only; distinct from timeout; default 60
    budget                : BudgetRef               -- [control-points.md §4.5]
    freedom_profile_ref   : String                  -- [control-points.md §6.7]
    bead_id               : String | None           -- present when bead-tied per [execution-model.md §4.3]
    snapshot_token        : String | None           -- present for reconciliation-investigator handlers per [docs/foundation/reconciliation.md §9.4b] (bootstrap)
    workflow_mode         : Enum | None             -- {single, review-loop, dot}; present iff non-default mode resolved per §4.2.HC-006; observational only per §4.1.HC-003a
    phase                 : Enum | None             -- multi-phase modes only; for review-loop: {implementer-initial, implementer-resume, reviewer}; omitted for single
    iteration_count       : Integer | None          -- present iff phase present and mode iterates; 1..3 for review-loop per [operator-nfr.md §4.1 ON-004]
    claude_session_id     : String | None           -- present iff phase=implementer-resume; Claude Code session ID for `claude --resume <id>`; distinct from SessionID
    model_preference      : ModelPreference | None  -- resolved at claim per [execution-model.md §4.3.EM-012b]; shape-validated per §4.10.HC-055a; absent or {model:"",effort:""} ⟹ tool default
    schema_version        : Integer                 -- N-1 readable per [operator-nfr.md §4.5]
```

```
RECORD ModelPreference:
    model  : String   -- opaque to harmonik; shape: ^[A-Za-z0-9._:/-]+$, max 128 chars; empty ⟹ tool default
    effort : String   -- one of {low, medium, high, xhigh, max} or empty ⟹ tool default
```

```
RECORD SessionID:
    value                : String                  -- daemon-assigned; unique within daemon generation
```

```
RECORD Outcome:
    -- see [execution-model.md §6.1 Outcome]
```

**Sentinel errors (Go var declarations).** Five primary classes plus two structural sub-sentinels. See §8 for detection rules; routing is owned by [execution-model.md §8].

```
-- Primary classes
VAR ErrTransient                    error   -- detection per §8.1; routed per [execution-model.md §8.1]
VAR ErrStructural                   error   -- detection per §8.2; routed per [execution-model.md §8.2]
VAR ErrDeterministic                error   -- detection per §8.3; routed per [execution-model.md §8.3]
VAR ErrCanceled                     error   -- detection per §8.4; routed per [execution-model.md §8.4]
VAR ErrBudget                       error   -- detection per §8.5; routed per [execution-model.md §8.5]

-- Sub-sentinels (each wraps ErrStructural)
VAR ErrSkillProvisioningFailed      error   -- wraps ErrStructural; detection per §8.6
VAR ErrProtocolMismatch             error   -- wraps ErrStructural; detection per §8.7
```

#### HC-054 — `Session.Attach()` for `agent_type=claude-code` returns a live tty stream

For sessions where `agent_type == "claude-code"` running under the tmux-pane substrate ([process-lifecycle.md §4.7 PL-021b]), `Session.Attach()` MUST return an `io.Reader` that streams the live contents of the pane's pty — not a tail of a log file. The reader MUST remain open for the lifetime of the session; reads MAY block when no bytes are available. The reader MUST NOT buffer beyond a single line ahead, so an attached operator observes claude's TUI in real time.

Closing the reader MUST NOT terminate the session; the session terminates only via cancellation of the enclosing context or invocation of `Session.Kill`.

Multiple concurrent `Attach()` calls are permitted; each call returns an independent reader fed from the same underlying pty. Implementations MAY coalesce readers into a single tee.

Cross-refs: [process-lifecycle.md §4.7 PL-021b] (pane substrate), [claude-hook-bridge.md §4.7 CHB-018] (pre-exec emission ordering — unaffected).

Tags: mechanism
Axes: llm-freedom=mechanical; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

### 6.2 Wire-protocol message envelope

Progress-stream events use the envelope in [event-model.md §4.1]; this spec adds no additional envelope fields. Event payloads referenced by this spec (`handler_capabilities`, `agent_ready`, `agent_started`, `agent_output_chunk`, `agent_completed`, `agent_failed`, `agent_rate_limited`, `agent_rate_limit_cleared`, `session_log_location`, `skills_provisioned`, `outcome_emitted`) have their payload schemas declared in [event-model.md §6.3]; see §6.4 below for the index.

### 6.3 Schema evolution

`LaunchSpec.schema_version` is an integer incremented on every normative schema change. The compatibility contract is N-1 readable per [operator-nfr.md §4.5]. Adding an optional field is non-breaking (version bump, N-1 readers accept). Removing or renaming a field is breaking (version bump + migration release at operator pause).

The error-sentinel taxonomy is versioned at the spec level (this spec's `version` in front matter), not as a `schema_version` field on individual errors. A breaking change to the sentinel set (renaming a sentinel, adding a mandatory new class) is a foundation amendment.

### 6.4 Co-owned event payloads

Each item below is a progress-stream message emitted by the handler subprocess; the daemon-side watcher translates it into a bus event of the same name per §4.2.HC-007. This spec is normative for WHEN each fires and at a name level what fields it MUST carry; [event-model.md §6.3] is normative for the on-the-wire bus-event payload.

- `handler_capabilities` — emitted first on the progress stream per §4.2.HC-009.
- `agent_ready` — emitted when the handler subprocess is ready to begin work per §4.9.
- `agent_started` — emitted after subprocess spawn and before `agent_ready`; payload MUST NOT include environment variables per §4.7.HC-029.
- `agent_output_chunk` — emitted for every output chunk from the agent.
- `agent_heartbeat` — emitted at ≤ `T/2` cadence while the subprocess is alive per §4.6.HC-026a.
- `agent_completed` — emitted on clean subprocess exit after `outcome_emitted`.
- `agent_failed` — emitted on crash or fatal typed error per §4.6; payload carries the mapped sentinel class.
- `agent_rate_limited` — emitted when the adapter detects rate-limit per §4.6.HC-025.
- `agent_rate_limit_cleared` — emitted when the adapter detects rate-limit clearance per §4.6.HC-025.
- `session_log_location` — emitted early in the session per §4.2.HC-010 and [workspace-model.md §4.7].
- `skills_provisioned` — emitted after skill provisioning and before `agent_ready` per §4.11.HC-049.
- `outcome_emitted` — emitted as the final message carrying the session's `Outcome` per §4.2.HC-008.

**Watcher-synthesized events (not handler-emitted).** The following are emitted by the watcher in response to state-machine transitions rather than handler messages; their payload schemas also live in [event-model.md §6.3]:

- `agent_warning_silent_hang`, `agent_resumed_after_warning`, `agent_soft_terminating`, `agent_hard_terminating` — from the §7.1 state machine.

**Daemon-to-handler control messages (MVH catalog).** The daemon uses the same NDJSON-framed socket (per §4.2.HC-007a) to send a small closed set of control messages to the handler subprocess. The MVH catalog is:

- `version_selected` — sent after `handler_capabilities` during the launch handshake per §7.2; payload `{selected_version: Integer}`.
- `cancel` — requests orderly subprocess cancellation corresponding to `ctx` cancellation per §4.4.HC-018; handler responds by triggering its `CleanExitSequence`.
- `shutdown` — daemon-initiated shutdown request (e.g., operator stop); handler responds via normal `CleanExitSequence` path.
- `rotate_account` — triggers the handler's in-subprocess rotation at the next turn boundary per §4.3.HC-013a.

Any control message the handler does not recognize MUST be ignored (forward-compatibility), NOT treated as a protocol error. Adding a new control message post-MVH is a foundation amendment (tracked as OQ-HC-009); adding fields to an existing control message is schema-additive. Twin binaries MUST support the MVH catalog.

## 7. Protocols and state machines

### 7.1 Silent-hang detection state machine

Per-agent-type threshold `T` is declared in the handler's subsystem envelope (**MVH default: T = 600 seconds**; raised from 120s because extended-thinking LLMs routinely exceed 120s of no-output; heartbeats per §4.6.HC-026a close the false-positive gap). Escalation multipliers `M_soft = 2 * T`, `M_hard = 4 * T`. The watcher maintains a per-session `last_progress_event_at` timestamp updated on every progress-stream message received, including `agent_heartbeat`. The watcher SHOULD tick at ≤ `T/10`. Absolute-from-last semantic: soft-terminate fires at `2*T` from the last message, hard-terminate at `4*T` from the last message (not `2*T` after soft-terminate entry).

Silent-hang detection is SUSPENDED during the post-outcome shutdown window of §4.2.HC-008a (distinct regime) and during explicit `ctx` cancellation (HC-018's cleanup bound applies instead — `ctx` cancellation supersedes silent-hang escalation and the resulting error is `ErrCanceled`, not `ErrStructural`).

| From | Event | Guard | To | Emits |
|---|---|---|---|---|
| `active` | progress-stream message received (incl. heartbeat) | — | `active` | — (timestamp updated) |
| `active` | timer tick | `now - last_progress_event_at >= T` | `warning` | `agent_warning_silent_hang` |
| `warning` | progress-stream message received (incl. heartbeat) | — | `active` | `agent_resumed_after_warning` |
| `warning` | timer tick | `now - last_progress_event_at >= M_soft` | `soft-terminating` | `agent_soft_terminating` + send graceful kill to subprocess |
| `soft-terminating` | subprocess exit | — | `terminated` | `agent_failed` (class `ErrStructural`, sub-reason `silent_hang`) |
| `soft-terminating` | timer tick | `now - last_progress_event_at >= M_hard` | `hard-terminating` | `agent_hard_terminating` + send SIGKILL to subprocess |
| `hard-terminating` | subprocess exit | — | `terminated` | `agent_failed` (class `ErrStructural`, sub-reason `silent_hang_hard_kill`) |

> INFORMATIVE: The warning-only event lets downstream workflows attempt a lightweight nudge (log a prompt, emit a correlation hint) before the watcher's automated escalation kicks in. Post-MVH may introduce a `nudge` callback on the adapter.

### 7.2 Launch handshake (protocol pseudocode)

```
FUNCTION launch_handshake(ctx, spec):
    -- daemon side
    IF size_of(spec) <= 1 MiB:
        subprocess = spawn(handler_path, stdin=json_encode(spec))
    ELSE:
        path = write_temp_spec_file(spec)
        subprocess = spawn(handler_path, args=["--launch-spec", path])
    watcher = spawn_watcher_goroutine(subprocess, spec)
    -- all await_message calls read NDJSON progress-stream messages on .harmonik/daemon.sock
    cap_msg = watcher.await_message("handler_capabilities", timeout=5s)
    IF cap_msg IS None:
        watcher.kill_subprocess()
        RETURN (None, ErrProtocolMismatch.wrap("no handler_capabilities received"))
    selected_version = negotiate_version(daemon.supported_versions, cap_msg.supported_versions)
    IF selected_version IS None:
        watcher.kill_subprocess()
        RETURN (None, ErrProtocolMismatch.wrap("no common version"))
    watcher.send_control("version_selected", selected_version)
    session_log_msg = watcher.await_message("session_log_location", timeout=10s)
    IF session_log_msg IS None:
        watcher.kill_subprocess()
        RETURN (None, ErrStructural.wrap("handler did not emit session_log_location"))
    -- provisioning has its own timeout, distinct from spec.timeout
    skills_msg = watcher.await_message("skills_provisioned", timeout=spec.provisioning_timeout)
    IF skills_msg IS None:
        watcher.kill_subprocess()
        RETURN (None, ErrSkillProvisioningFailed.wrap("skills not provisioned before agent_ready"))
    ready_msg = watcher.await_message("agent_ready", timeout=spec.timeout)
    IF ready_msg IS None:
        watcher.kill_subprocess()
        RETURN (None, ErrStructural.wrap("handler did not become ready"))
    session = new_session(subprocess, watcher, session_log_msg.log_path)
    RETURN (session, nil)
```

Every branch point above corresponds to a normative requirement: size-based delivery (§4.2.HC-005), handler_capabilities (§4.2.HC-009 and §4.5.HC-021), session_log_location (§4.2.HC-010), skills_provisioned (§4.11.HC-049 and §4.5.HC-022), agent_ready (§4.9.HC-039 and §5 HC-INV-004).

## 8. Error and failure taxonomy

Handler-contract sentinel classes. This section is normative for **detection** (what in-handler condition maps to which sentinel) and for the **emission rule** (which progress-stream message the handler produces, and which bus event the watcher publishes as a result). **Routing** of bus events — retry policy, reclassification to `compilation_loop`, terminal `run_failed` emission — is owned by [execution-model.md §8]; this spec is not normative for what the orchestrator does next.

Classification is mechanism-tagged per §4.5.HC-023. Every error returned across a subsystem boundary MUST wrap exactly one primary class (possibly via one of the two sub-sentinels); consumers dispatch via `errors.Is` narrowest-first.

### 8.1 ErrTransient

- **Detection rule.** Handler or adapter determines the failure is network-transient (DNS failure, connection reset, 5xx on a provisioning fetch), rate-limit-transient (where the transport itself failed rather than emitted `agent_rate_limited`), a progressless-timeout below the silent-hang thresholds of §7.1, or a first-occurrence socket-level I/O error per §4.6.HC-024a.
- **Emission rule.** Watcher publishes `agent_failed` with payload field `class = "transient"` and the wrapped error value. No other event fires at detection; any retry event is the orchestrator's emission per [execution-model.md §8.1]. Sub-reasons include `socket_io_error` (§4.6.HC-024a) and `partial-message` only when triggered from recoverable framing causes (most `partial-message` cases are Structural per §4.2.HC-007b).

### 8.2 ErrStructural

- **Detection rule.** The plan is wrong (wrong tool selected, missing precondition recoverable only by a different plan), or silent-hang triggered per §7.1, post-outcome shutdown exceeded `T_shutdown` per §4.2.HC-008a, partial/malformed progress-stream message per §4.2.HC-007b, watcher wedge or panic per §4.3.HC-011a, sustained socket-level break after reconnect attempt per §4.6.HC-024a, or workspace held by prior-generation orphan per §4.10.HC-044a. Structural failures that carry sub-sentinel identity use the sub-sentinel (see §8.6, §8.7).
- **Emission rule.** Watcher publishes `agent_failed` with payload field `class = "structural"` and any applicable `sub_reason`. Defined `sub_reason` values (non-exhaustive): `silent_hang`, `silent_hang_hard_kill`, `post_outcome_shutdown_timeout`, `partial-message`, `malformed_progress_message`, `progress_stream_broken`, `watcher_panic`, `watcher_wedged`, `workspace_held_by_orphan`, `skill_provisioning_failed` (§8.6), `protocol_mismatch` (§8.7), `ndjson_line_too_long`.

### 8.3 ErrDeterministic

- **Detection rule.** Confirmed-bug or impossible-condition determined from structured fields: specific exit code the handler classifies as deterministic, typed error payload from the subprocess, or adapter-returned deterministic condition.
- **Emission rule.** Watcher publishes `agent_failed` with payload field `class = "deterministic"` and the wrapped error value.

### 8.4 ErrCanceled

- **Detection rule.** Operator-initiated cancellation or policy-initiated cancellation observed on `ctx`. `ctx` cancellation supersedes silent-hang escalation; a cancellation during `warning`/`soft-terminating` state produces `ErrCanceled`, not `ErrStructural`.
- **Emission rule.** Watcher publishes `agent_failed` with payload field `class = "canceled"`. If the cancellation was operator-initiated, the orchestrator additionally publishes `operator_stopped` per [execution-model.md §8.4]; that emission is not this spec's concern.

### 8.5 ErrBudget

- **Detection rule.** Budget counter at dispatch would exceed the remaining budget per [control-points.md §4.5]. Detection fires before `Launch` is called; no subprocess is spawned.
- **Emission rule.** Watcher publishes `budget_exhausted` with the budget identity; `agent_failed` is NOT emitted because no agent ran. Terminal-run emission (`run_failed`) is orchestrator-level per [execution-model.md §8.5].

### 8.6 ErrSkillProvisioningFailed (sub-sentinel, wraps ErrStructural)

- **Detection rule.** A name in `LaunchSpec.required_skills[]` does not resolve against `LaunchSpec.skill_search_paths[]` at launch time (§4.11.HC-048), OR a resolved skill fails to provision in the agent-process shape with a structural cause (package-integrity check failure, unsupported manifest, permission denied) per §4.11.HC-048a. Transient provisioning failures wrap `ErrTransient` per §8.1, NOT this sub-sentinel.
- **Emission rule.** Watcher publishes `agent_failed` with `class = "structural"`, `sub_reason = "skill_provisioning_failed"`, and the unresolved (or failing) skill name in the payload.

### 8.7 ErrProtocolMismatch (sub-sentinel, wraps ErrStructural)

- **Detection rule.** No mutually supported wire-protocol version between the daemon and the handler subprocess at handshake (§4.2.HC-009), or `handler_capabilities` absent within 5s of subprocess spawn (§7.2), or an NDJSON line exceeding the cap of §4.2.HC-007a.
- **Emission rule.** Watcher publishes `agent_failed` with `class = "structural"`, `sub_reason = "protocol_mismatch"` (or `ndjson_line_too_long` for the line-cap subcase), and the daemon-supported and handler-supported version lists in the payload.
- **Routing note.** Although `ErrProtocolMismatch` wraps `ErrStructural`, the orchestrator's retry policy per [execution-model.md §8.2] MUST NOT retry re-plans against the SAME pinned handler binary — the negotiation cannot succeed without a binary change. The re-plan space that resolves ProtocolMismatch is: swap `handler_ref`, change pinned commit via configuration, or upgrade the daemon. Execution-model's router is the normative authority for distinguishing these routing shapes; this spec is normative for the detection.

## 9. Cross-references

### 9.1 Depends on

- **[architecture.md §4.1]** — four-axis classification; every requirement in this spec uses the axes defined there.
- **[architecture.md §4.2]** — ZFC test; error classification (§4.5.HC-023) and skill resolution (§4.11.HC-047) are mechanism-tagged, and the spec prohibits cognition-tagged error classification.
- **[architecture.md §4.4]** — subsystem envelope; handlers declare redaction patterns (§4.7.HC-032) and silent-hang thresholds (§7.1) in their subsystem envelope.
- **[architecture.md §6.1]** — `agent_type` identifier shape; LaunchSpec carries the identifier.
- **[architecture.md §4.9]** — centralized-controller principle; the watcher/adapter split (§4.3) and cross-queue handoff rule (§4.3.HC-016) pin this principle at the concurrency layer.
- **[execution-model.md §4.1]** — `Outcome` type used as the payload of `outcome_emitted` (§4.2.HC-008).
- **[execution-model.md §4.3]** — `Run` and `bead_id` fields; LaunchSpec carries `run_id` and optional `bead_id`.
- **[execution-model.md §4.9]** — workflow validator resolves `handler_ref` and `required_skills` before launch.
- **[execution-model.md §8]** — failure taxonomy and routing; the five primary sentinel classes of §4.5 map to the failure classes there, and routing of `agent_failed` / `budget_exhausted` / `run_failed` to retry / re-plan / terminal paths is owned there.
- **[control-points.md §6 Registry]** — CP registry interface that declares `required_skills`, `default_skills`, `freedom_profile`, `budget`; LaunchSpec fields `required_skills[]`, `skill_search_paths[]`, `freedom_profile_ref`, `budget` are populated from that registry at workflow-load time.
- **[event-model.md §4.1]** — event envelope; handler-emitted events use this envelope.
- **[event-model.md §8]** — event taxonomy; this spec's co-owned events have their payload schemas there.
- **[event-model.md §4.3]** — consumer taxonomy and dead-letter destination referenced by §4.6.HC-027.
- **[process-lifecycle.md §4.1]** — daemon socket path for subprocess-to-daemon communication (§4.10.HC-044).
- **[process-lifecycle.md §4.5]** — agent subprocess as a child of the daemon (§4.10.HC-044).
- **[process-lifecycle.md §4.6]** — deterministic daemon vs. orchestrator-agent distinction; the seam in §4.12 pins this boundary.

### 9.2 Reverse dependencies

> INFORMATIVE: Reverse dependencies are computed on demand from the foundation corpus; not stored here per template §0.

### 9.3 Co-references (read-only consumption)

- **[control-points.md §4.11 Skill-declaration surface]** — this spec consumes the skill-declaration surface defined there; does not depend on control-points' internal types. The directional resolution rule in [docs/foundation/components.md §Co-dependency resolution rules] applies.
- **[control-points.md §6.7 Freedom profile]** — LaunchSpec's `freedom_profile_ref` points at a profile declared there; the field shape is the only coupling.
- **[control-points.md §4.5 Budget enforcement point]** — LaunchSpec's `budget` field points at a budget declared there; enforcement semantics are in control-points.
- **[workspace-model.md §4.7 Session-log pipeline]** — S04's `session_log_location` emission (§4.2.HC-010) is one of three stages in the workspace-model pipeline; workspace-model owns the end-to-end ownership split.
- **[reconciliation/spec.md §4.4 Snapshot-token binding]** — LaunchSpec's `snapshot_token` is read by investigator-agent handlers; the token semantics live in reconciliation.
- **[operator-nfr.md §4.9 Observability protocol]** — the redaction registry (§4.7) is one of the enforcement points for operator-nfr's "no secrets in event log / audit record" invariant.
- **[operator-nfr.md §4.7 Security posture]** — skill-injection policy enforcement (network egress, sandbox) consumes this spec's provisioning hook.

## 10. Conformance

### 10.1 Conformance profiles

**Core MVH.** An implementation conforming to Core MVH MUST pass every requirement `HC-001` through `HC-053` (including `HC-007a`, `HC-007b`, `HC-008a`, `HC-011a`, `HC-013a`, `HC-024a`, `HC-026a`, `HC-036a`, `HC-044a`, `HC-048a`, `HC-049a`) and every invariant `HC-INV-001` through `HC-INV-007`. No requirement is deferred at MVH.

**Post-MVH extensions.** The following are additive extensions to Core MVH; neither is required to claim Core MVH conformance:

- Binary signing / cosign verification (per §4.10.HC-043 — commit-hash check is MVH, signing is post-MVH).
- Per-connection socket authentication (per §4.10.HC-044 — filesystem-permission authenticity is MVH).
- Twin-conformance drift detection (per §4.8.HC-038 — scoped to S07 scenario-harness post-MVH).
- Account rotation support for a given handler type (per §4.3.HC-013 `RotateAccount` — returning `ErrDeterministic` is conformant for agent types without rotation).
- Secret rotation mid-session (explicitly out of scope; a new launch is required for MVH).

### 10.2 Test-surface obligations

During bootstrap (before `testing.md` exists) test obligations are named in prose. Each requirement's test obligation:

- **HC-001 — HC-004 (interfaces).** Interface-conformance unit tests (every registered handler implements `Handler`; every `Launch` return value implements `Session`). Idempotency test: double-launch on the same `(run_id, node_id)` returns the existing session.
- **HC-005 — HC-010 (wire protocol).** Wire-protocol integration tests with the twin handler: LaunchSpec round-trip under both delivery modes (stdin and file-path), handshake sequence verification, NDJSON-framing conformance test (`HC-007a`) with embedded-newline rejection and 1-MiB-line-cap rejection, message-boundary durability test (`HC-007b`) forcing socket EOF mid-JSON-object, version-negotiation negative test (`ErrProtocolMismatch`), post-outcome shutdown timeout scenario (`HC-008a`), dirty-exit-inside-shutdown-window scenario asserting one-terminal-event invariant.
- **HC-011 — HC-016 (concurrency).** Race-detector scenario tests covering N concurrent sessions; invariant test asserting exactly-one watcher per session via daemon introspection; adapter-no-goroutine-spawn test via goroutine-count assertions; watcher-panic-recovery test (`HC-011a`) asserting panic is converted to `agent_failed`; watcher-wedge test (`HC-011a`) forcing a blocked subscriber and asserting `watcher_wedged` sub-reason fires at `T/2` without misattribution to silent-hang.
- **HC-017 — HC-019 (context).** Cancellation test suite: `ctx` cancel at every phase of launch and session lifetime; deadline-propagation test asserting subprocess receives the deadline; context-value lint asserting no business data is carried via ctx values.
- **HC-020 — HC-023 (error taxonomy).** Error-wrapping tests: every boundary-return error satisfies `errors.Is` for exactly one of the five primary sentinels; `ErrProtocolMismatch` and `ErrSkillProvisioningFailed` satisfy both their own sentinel and `ErrStructural`; narrowest-first dispatch order test.
- **HC-024 — HC-027 (async error propagation).** Subprocess-crash scenario test with twin forcing each failure class; rate-limit scenario test covering `agent_rate_limited` → `agent_rate_limit_cleared`; dead-letter test for undeliverable events.
- **HC-026 + HC-026a + §7.1 (silent-hang + heartbeat).** State-machine unit tests covering every transition in the §7.1 table; timing-based scenario test with twin-forced silence confirming warning → soft-terminate → hard-terminate sequence; false-positive resilience test: twin emitting heartbeats during long reasoning MUST NOT trigger silent-hang; false-negative detection test: twin emitting no messages (no heartbeats) MUST trigger silent-hang within `T` + tick-jitter.
- **HC-028 — HC-034 (secrets).** Redaction-middleware unit tests for the common-prefix regex; per-handler-pattern registration tests; compile-time schema-check test verifying a registered event type with a secret-shaped field name is rejected at startup; end-to-end test asserting no secret appears in event log or session log.
- **HC-035 — HC-038 (twin parity, including HC-036a).** Interface-equivalence test suite: every twin handler (the canonical subprocess) and its real counterpart pass the same interface-conformance tests; daemon-codebase lint asserting zero `if isTwin` branches. Separately: in-process-fake carve-out test confirming unit-test fakes of `Handler`/`Session` are NOT required to honor the wire protocol. HC-036a script-file format tests: load-time rejection of unknown `heartbeat_mode` values; rejection of missing or empty `type` in any ScriptMessage; `wall_clock` default applied when `heartbeat_mode` is absent; `relative_timestamp_ms` ignored in `wall_clock` mode and honoured in `scripted` mode; `type` key in `payload` silently overwritten.
- **HC-039 — HC-041 (ready-state).** Ready-state scenario test: work dispatch before `agent_ready` is rejected; twin emits `agent_ready` identically to real handler.
- **HC-042 — HC-045 (trust).** Launch-path negative test: missing binary, mismatched commit hash. System-handler path (via `system_handler=true` declaration) exercised via Claude Code fixture.
- **HC-046 — HC-050 (skill injection).** Skill-injection scenario tests: (a) `required_skills` not resolvable triggers `ErrSkillProvisioningFailed` at launch per `HC-048`; (b) resolved-but-provisioning-fails-transiently path retries per `HC-048a` and eventually succeeds within `provisioning_timeout`; (c) resolved-but-provisioning-fails-after-backoff path reclassifies to `ErrStructural` on attempt-cap exhaustion; `skills_provisioned` event carries the installed set; end-to-end Beads-CLI skill provisioning test.
- **HC-051 — HC-053 (modularity).** Boundary-enforcement static-analysis rule: daemon packages MUST NOT import ntm-specific types. Changeable-adapter test: swapping the claude-code adapter for a mock adapter does not alter daemon behavior.

Migration to `[testing.md §<layer>]` cross-references occurs within one revision cycle once `testing.md` lands; tracked in `OQ-HC-003`.

### 10.3 Excluded conformance claims

- This spec does NOT grant conformance over: per-handler wire-format specifics (each per-handler spec owns); session-log format (handler-specific; no unified schema across agent types per core-scope §4); skill storage layout (owned by `agent-configuration`); scenario-harness twin-drift detection (owned by S07, post-MVH).
- This spec does NOT guarantee performance bounds on launch latency or session throughput; those are operator-observable in [operator-nfr.md §4.8] (restart RTO) and are not requirements of this spec.

## 11. Open questions

#### OQ-HC-001 — Per-agent-type silent-hang threshold T defaults

Question: The silent-hang state machine in §7.1 uses a per-agent-type threshold `T`. The MVH default was raised to 600s in v0.2 to accommodate extended-thinking LLMs; heartbeat obligation (HC-026a) at ≤T/2 makes false-positive kills rare. Do any agent types warrant lower values because they genuinely tick faster and heartbeat-less-often would still be detectable?
Owner: foundation-author
Blocks: HC-026 finalization at `reviewed` status
Default-if-unresolved: T = 600s for all agent types; each handler spec may override in its subsystem envelope, but only downward with justification tied to heartbeat cadence.

#### OQ-HC-002 — Account-rotation surface adequacy for multi-account handlers

Question: The `Adapter.RotateAccount` callback is single-return-value (`error`). If a handler wants to report "rotated to account X of N remaining," does the surface need extending?
Owner: foundation-author
Blocks: none (MVH: single-return is adequate; Claude Code uses a script-driven rotation with its own account pool)
Default-if-unresolved: Keep single-return-value; extend the surface if a post-MVH handler demonstrates a concrete need.

#### OQ-HC-003 — Migrate test-obligation prose to testing.md references

Question: §10.2 currently names test obligations in prose; template §10.2 expects cross-references to `[testing.md §<layer>]` once `testing.md` lands.
Owner: foundation-author
Blocks: none
Default-if-unresolved: Keep prose obligations; migrate within one revision cycle after `testing.md` is finalized.

#### OQ-HC-004 — Wire-protocol version-negotiation granularity

Question: `handler_capabilities` advertises supported wire-protocol versions. Is the version a monotonic integer, or should it be a semver to allow add-new-field-only "minor" compatibility without version bump?
Owner: foundation-author
Blocks: none (MVH: monotonic integer is sufficient; N-1 compatibility per §6.3 is declared at the LaunchSpec schema level, not at the wire-protocol level)
Default-if-unresolved: Monotonic integer. Revisit if post-MVH demonstrates a concrete need for semver-style negotiation.

#### OQ-HC-005 — Nudge callback on the adapter before automated silent-hang escalation

Question: §7.1 informative note suggests a post-MVH `Nudge` callback on the adapter that would fire during `warning` state before automated escalation. Should it be part of the adapter surface now to avoid a breaking change later?
Owner: foundation-author
Blocks: none (MVH: not part of the adapter surface)
Default-if-unresolved: Not included in MVH. Adding it post-MVH is additive (new adapter method with a default implementation returning immediately); not a breaking change.

#### OQ-HC-006 — Cross-generation re-launch garbage-collection ownership

Question: HC-004 pins idempotency "within one daemon generation." A reconciliation-driven re-launch after daemon restart is a new launch; prior-generation artifacts (residual subprocess, socket file, session-log handles) need a GC owner. Reconciliation's investigator flow addresses the semantic reconciliation but this spec does not name the concrete GC actor for prior-generation handler artifacts.
Owner: foundation-author
Blocks: none (MVH: the reconciliation investigator is the de facto owner; this OQ just pins the contract)
Default-if-unresolved: Reconciliation's startup sweep owns prior-generation artifact GC; this spec cross-references that responsibility when `specs/reconciliation.md` lands.

#### OQ-HC-007 — Skill-package on-disk shape

Question: HC-047 declares deterministic resolution against `skill_search_paths[]` but does not pin what constitutes a "match" on disk (directory with a manifest, bare directory, file, symlink, archive). Bootstrap citation in HC-046 points at `[docs/foundation/components.md §10]`.
Owner: foundation-author
Blocks: none (MVH: handler implementers follow the bootstrap citation)
Default-if-unresolved: When `specs/agent-configuration.md` lands, migrate HC-046's bootstrap citation to the normative reference and resolve this OQ.

#### OQ-HC-008 — Rate-limit twin emission locus

Question: HC-025 and §6.4 name `agent_rate_limited` / `agent_rate_limit_cleared` as progress-stream message types. Twin-author review r2 flagged two consistent interpretations: (A) twin emits the event directly and the adapter's `DetectRateLimit` is a trivial pass-through, vs. (B) twin emits a provider-shaped signal (e.g., a fake 429 response) embedded in `agent_output_chunk` and the adapter parses it. The two have different per-handler-spec implications.
Owner: foundation-author
Blocks: none (MVH: scenario-harness twins use interpretation A — direct event emission)
Default-if-unresolved: Interpretation A for the canonical twin. Real-handler adapters MAY additionally parse provider-shaped content and synthesize an `agent_rate_limited` message before the watcher publishes; this is a per-handler-spec concern, not a handler-contract constraint. Resolve when first per-handler spec lands.

#### OQ-HC-009 — Post-MVH daemon→handler control-message evolution

Question: §6.4 fixes the MVH control-message catalog at four messages (`version_selected`, `cancel`, `shutdown`, `rotate_account`). What is the evolution rule for adding a control message post-MVH (e.g., `pause`, `resume` for suspend/resume workflows)?
Owner: foundation-author
Blocks: none (MVH: the catalog is closed; handlers ignore unknown messages)
Default-if-unresolved: Post-MVH additions are foundation amendments; capability-negotiated via an additive field on `handler_capabilities` (handlers declare the control-message names they support). Resolve when the first post-MVH control-message proposal arrives.

#### OQ-HC-010 — Non-Unix transport substitution mechanism

Question: §2.2 defers Windows and remote/cloud transports. HC-053 pins the cross-subsystem surface as stable across shape evolution, but a remote shape by definition replaces the transport, not just the adapter. What is the substitution mechanism?
Owner: foundation-author
Blocks: none (MVH: single Unix-socket transport)
Default-if-unresolved: Post-MVH, introduce a `Transport` interface behind the Handler/LaunchSpec surface that preserves NDJSON framing, bidirectional flow, authenticated-connection semantics, and the HC-007b durability rule. `.harmonik/daemon.sock` becomes the MVH implementation of that interface. Resolve when the first non-Unix transport proposal (Windows or cloud) is drafted.

#### OQ-HC-011 — TOCTOU on verified binary path

Question: HC-043 commit-hash check and HC-INV-005 path verification both occur BEFORE `exec()`. Between the check and the exec, a racer could replace the binary at the verified path. Platform-specific mitigations exist (`O_CLOEXEC` + `fexecve`-equivalent) but are not uniform; post-MVH binary signing (per §10.1) resolves this structurally.
Owner: foundation-author
Blocks: none (MVH: accepted risk for single-operator local execution)
Default-if-unresolved: Accept TOCTOU risk at MVH; revisit with binary-signing post-MVH.

## 12. Revision history

| Date | Version | Author | Summary |
|---|---|---|---|
| 2026-04-23 | 0.1.0 | foundation-author | Initial draft. Handler + Session + Adapter interfaces; wire protocol with stdin/file-path LaunchSpec delivery and `handler_capabilities` version negotiation; six sentinel classes plus `ErrProtocolMismatch`; silent-hang state machine with T = 120s default; secrets redaction registry with compile-time payload check; twin parity invariant; skill-injection obligation; handler-as-modularity-boundary. |
| 2026-04-24 | 0.2.0 | foundation-author | Round-1 review integration. Pinned Unix-domain-socket transport and NDJSON framing (HC-007, HC-007a, HC-044 consolidated). Added post-outcome shutdown window HC-008a (T_shutdown=10s). Reworded session_log_location / outcome_emitted emitter identity: handler emits on progress stream; watcher translates to bus event (HC-007, HC-008, HC-010, §6.4). Restructured error taxonomy as 5 primary + 2 structural sub-sentinels (HC-020, HC-021, HC-022); §8 rewritten as detection-only, routing owned by execution-model §8. Split skill-injection fail-launch into structural (HC-048 unresolvable) and transient (HC-048a resolved-but-provisioning-failed, retry with backoff); added `provisioning_timeout` LaunchSpec field. Added handler heartbeat obligation HC-026a (≤T/2 cadence); raised silent-hang default T to 600s; silent-hang triggers on absent heartbeats. Carved out in-process unit-test fakes from HC-035/HC-036 twin-binary rule. Added HC-INV-005 (no launch without verified binary). Tightened HC-INV-004 watcher-emission ordering. Fixed HC-042 `system_handler=true` declaration subject. Added control-points Registry cross-ref to §9.1. Added OQ-HC-006 (cross-generation GC), OQ-HC-007 (skill-on-disk shape). |
| 2026-04-24 | 0.3.1 | foundation-author | Corpus-wide cleanup pass (no semantic changes). Migrated legacy architecture.md citation anchors to the §4.N / §6.1 map per the v0.2 NOTE: §1.1→§4.1 (×1 in §9 cross-refs), §1.2→§4.2 (×1 in §9 cross-refs), §1.4→§4.4 (×1 in §9 cross-refs), §1.6→§4.8 (×1 in §4.3.HC-016 work-queue clause), §1.6a→§6.1 (×3 in §6.1 Handler.AgentType() comment, §6.1 LaunchSpec.agent_type comment, and §9 cross-refs — chose §6.1 since each site cites the identifier shape, not the normative requirement), §1.8→§4.9 (×2 in §9 cross-refs and §A.3 rationale footer). Completed AR-MIG-001 `handler_type` → `agent_type` rename at §4.2.HC-010 (session_log_location progress-stream payload; note: task doc listed HC-008, but the bare identifier actually lives in HC-010 which owns `session_log_location` emission per the v0.2 HC-010 split). No requirement IDs, invariants, or schemas were touched. |
| 2026-04-24 | 0.3.2 | foundation-author | Corpus citation-drift cleanup pass 2: migrated legacy §N.N cross-spec anchors to current template §N.N form per the central remap table; ~25 citations fixed. EV: `§3.1→§4.1` (envelope) ×3, `§3.2→§6.3` (payload schemas) ×5, `§3.7→§4.3` (bus consumer/dead-letter) ×2, `§3.2→§8` (event taxonomy) ×1 at §9.1 cross-refs. WM: `§5.3a→§4.7` (session-log pipeline) ×3, `§5.1→§4.1` (workspace path) ×1 at §6.1 LaunchSpec comment. ON: `§7.1→§4.9` (observability) ×2, `§7.2→§4.7` (secrets/sandbox) ×1, `§7.5→§4.5` (N-1 compat) ×2, `§7.8→§4.8` (restart RTO) ×1. PL: `§8.1→§4.1` (socket path) ×3, `§8.5→§4.5` (agent subprocess) ×1, `§8.6→§4.6` (daemon vs orchestrator-agent) ×1. Reconciliation path fix: `[reconciliation.md §9.4b]→[reconciliation/spec.md §4.4]` (snapshot-token binding) ×2 at §3 glossary and §9.3 cross-refs; `[reconciliation.md §9]→[reconciliation/spec.md §4]` at §4.10 startup-sweep reference. CP: `§6.11→§4.11` (skill declaration) ×4, `§6.9→§4.5` (budget) ×3. No requirement IDs, invariants, or schemas touched. |
| 2026-04-25 | 0.3.3 | foundation-author | Two cross-spec coordination patches landing as gap-filler IDs (no renumbering; HC ID FREEZE preserved). **Edit 1 (§4.3 concurrency model):** new HC-016a — orphan-reconnect-window retry rule, the handler-side companion to [process-lifecycle.md §4.2 PL-003b]. Clients receiving the typed `daemon_not_ready{reason="unknown_run_id"}` rejection (issued by the daemon between socket bind at PL-005 step 3a and in-memory model build at PL-005 step 7) MUST retry per the [process-lifecycle.md §4.2 PL-009b] exponential backoff schedule (initial 100 ms, doubling, max 2 s per attempt, capped at `T_ready_wait = 60 s` per OQ-PL-002); the watcher MUST NOT classify the typed-error response as a session failure during the retry window (no `agent_failed`, no silent-hang escalation per §4.6.HC-026); after cap exhaustion the request fails with `ErrTransient` and the watcher emits `agent_failed` carrying sub-reason `daemon_startup_window_exceeded`. Closes the orphan-agent-reconnect-during-startup-window race named in PL-003b's R2 amendment. **Edit 2 (§4.6 error propagation):** new HC-026b — acceptance clause for [operator-nfr.md §4.9 ON-040]'s drain-forced silent-hang synthesis. When operator-initiated drain step 4 SIGKILLs a still-running agent subprocess per [operator-nfr.md §4.7 ON-029], ON-040 synthesizes `agent_warning_silent_hang{reason=drain_forced}` even when no §4.6.HC-026 / §7.1 silent-hang detection had fired; HC accepts ON-040's classification and obligates the watcher NOT to also emit an HC-classified silent-hang event for the same run/node, preserving HC-INV-004 (single terminal event per session) by routing the synthesis as ON-side-only. Acceptance clause; enforcement remains in operator-nfr. **New IDs (net):** HC-016a, HC-026b (2 new). No invariants, no schema changes, no §6 / §8 / §10 touches. Status remains `reviewed`. |
| 2026-05-09 | 0.3.4 | foundation-author | Normative spec section for twin script-file format (hk-ahvq.48.11). **New §4.8.HC-036a — Twin script-file format:** promotes de-facto schema from `cmd/harmonik-twin-claude/scriptdriver.go` package godoc (hk-ahvq.48.3) to normative HC text. Defines: file path rule (`<fixture-root>/<scenario>/twin-scripts/<role>.yaml`); top-level YAML fields (`heartbeat_mode` enum `wall_clock`|`scripted`, default `wall_clock`; `messages` list); ScriptMessage record fields (`type` required string, `payload` optional map, `relative_timestamp_ms` optional int); heartbeat-mode semantics; load-time validation requirements. No existing requirement IDs renumbered; no invariants, no §6/§8/§10 touches. Status: reviewed → reviewed (spec-edit only). |
| 2026-05-12 | 0.3.5 | foundation-author | Add HC-045a / HC-045b / HC-045c in §4.10 (gap-filler placement after HC-045, matching the HC-016a / HC-026b pattern) covering claude-code agent type's launch mechanism (pointer to claude-hook-bridge.md), hook-bridge one-shot NDJSON connection regime, and handler-side claude_session_id minting/resume discipline including orphan-reconnect git-derived lookup. Clarifying sentence added to HC-006 pointing forward to HC-045c and to CHB-023's durability boundary. No requirement IDs renumbered or retired; HC-053 in §6.2 is unchanged. Status remains `reviewed`. |
| 2026-05-13 | 0.3.6 | bridge-integration | **HC-054/055/056/057 added (hk-gql20.3).** Additive amendments for the bridge-integration initiative. **HC-054** (§6.1) — `Session.Attach()` for `agent_type=claude-code` under the PL-021b tmux substrate returns a live pty `io.Reader` (not a log tail); single-line buffering for real-time observation; close-reader does not terminate the session; multiple concurrent attaches permitted. **HC-055** (§4.10) — claude CLI flag allow-list at MVH: only `--session-id` / `--resume` constructed by the daemon; explicit deny on `--print`, `--add-dir`, `--allowed-tools`/`--disallowed-tools`, `--mcp-server`/`--mcp-config`, `--permission-mode` (policy lives in worktree-materialized `.claude/settings.json`); operator `Config.HandlerArgs` validated against CHB-007 deny-list. **HC-056** (§4.9) — `agent_ready` timeout default 30s via `Config.AgentReadyTimeout`; on timeout: kill, reap, emit `agent_failed{sub_reason=agent_ready_timeout}`, reopen bead. Closes `hk-do7te`. **HC-057** (§4.9) — heartbeat-emission ownership carve-out: for `agent_type=claude-code` at MVH, the daemon MAY emit `agent_heartbeat` on the handler-process's behalf (CHB-019 cadence); retired when post-MVH shim binary lands. No existing HC IDs renumbered. Status remains `reviewed`. |
| 2026-05-13 | 0.3.7 | agent (hk-p63bz) | **agent_ready semantics reframed for the interactive substrate (audit §6 B3).** HC-039 amended: `agent_ready` emitter identity is now substrate-dependent; under the tmux substrate the relay synthesizes it on `SessionStart` receipt (not the handler pre-exec). HC-041 amended: `DetectReady` must accept relay-synthesized `agent_ready` with `provenance: "claude_session_start"` and MUST NOT accept `launch_initiated`. HC-056 amended: timeout window now starts at `SubstrateSpawn` return (not `cmd.Start()`) under the tmux substrate, explicitly excluding `launch_initiated` as a satisfying signal, and cross-refs updated to point at CHB-013 (SessionStart mapping) and CHB-018 (launch_initiated precursor). No existing HC IDs renumbered. Coexists with HC-054/055/056/057 (hk-gql20.3). Refs: hk-p63bz. |
| 2026-05-18 | 0.4.0 | agent (hk-zudz0) | **Phase-LaunchSpec contract: HC-006a normative table added (§4.2).** **HC-006a added** — per-phase LaunchSpec field requirements table for `review-loop`, covering: `argv` (`--session-id` vs `--resume` per phase), `--model`/`--effort`/`--dangerously-skip-permissions` flags, `HARMONIK_PHASE` / `HARMONIK_ITERATION_COUNT` / `HARMONIK_CLAUDE_SESSION_ID` env vars, `HARMONIK_WORKFLOW_MODE` (shared across phases), `working_dir` (worktree path shared across all three phases at MVH), `LaunchSpec.claude_session_id` wire field (ABSENT for initial and reviewer, PRESENT for resume), harmonik-side `handlerSessionID` source (fresh UUIDv7 every call), `agent-task.md` content variants by phase, and wire-protocol `phase` / `iteration_count` fields. Cites `internal/daemon/claudelaunchspec.go` (`buildClaudeLaunchSpec`) as implementation evidence; spec remains authoritative. Includes test-hookpoint sensor note pointing at `internal/operatornfr/reviewloopstatus_on035a_test.go` (partial coverage) and naming `TestHC006a_PerPhaseLaunchSpecInvariants` as a follow-up gap. No existing HC IDs renumbered. Version bump to 0.4.0 (new normative section). |
| 2026-05-15 | 0.3.9 | agent (hk-fdyip) | **Worktree auto-trust: `--dangerously-skip-permissions` carveout (HC-055 + HC-055b).** **HC-055 amended** to add `--dangerously-skip-permissions` to the flag allow-list, permitted only when the daemon launch CWD canonicalizes to a path under the harmonik-owned worktree root. **HC-055b added** (§4.10) — specifies the path-check rule: canonicalize both `workspacePath` and `<projectDir>/.harmonik/worktrees` via `os.EvalSymlinks`; emit the flag iff `canonicalWorkspace` has `canonicalWorktreeRoot+separator` as a prefix; positive-allowlist match, NOT negative-allowlist. **HC-055b-1** — `dangerouslyAllowedPermissions` settings.json workaround is obsoleted by this CLI flag and MUST be removed from the materialization path. Rationale: in an operator-daemon context the worktree is already operator-sanctioned; the flag removes the interactive confirmation that would otherwise block the unattended session. Operator-supplied `Config.HandlerArgs` MUST NOT carry this flag (CHB-007 guard applies). Refs: hk-fdyip. |
| 2026-05-14 | 0.3.8 | agent (hk-7zvh4) | **Model-selection spec amendment: ModelPreference descriptor + HC-055 flag allow-list extension.** New **HC-055a** (§4.10) — `ModelPreference` descriptor invariants: shape constraints (`^[A-Za-z0-9._:/-]+$`, max 128 chars, rationale: allows aliases, version pins, provider-prefixed forms; rejects shell metacharacters); value-opacity invariant (harmonik validates shape not value; handler-side launch failure is the authoritative compatibility check; supports future handler types with arbitrary model strings); `effort` closed enum `{low,medium,high,xhigh,max}`; argv translation rule for `agent_type=claude-code`; `model_rejected_by_tool` structural sub-reason. **HC-055 amended** to extend the allow-list with `--model <value>` and `--effort <value>` as optional flags, both omitted when the resolution chain produces empty. **LaunchSpec RECORD** (§6.1) gains `model_preference : ModelPreference | None`; **HC-006** optional-fields list updated to include `model_preference`. **ModelPreference RECORD** added to §6.1. No existing HC IDs renumbered. Refs: hk-7zvh4, hk-cfhj2. |

## A. Appendices

### A.3 Rationale

**Why the 5-primary + 2-sub-sentinel taxonomy instead of a flat enum.** The five primary classes map one-to-one to routing regimes in [execution-model.md §8]: Transient → retry, Structural → re-plan, Deterministic → fail-run, Canceled → cleanup, Budget → deny. The two sub-sentinels (`ErrProtocolMismatch`, `ErrSkillProvisioningFailed`) are handler-specific labels on `ErrStructural`: they share the Structural routing regime but carry enough specific information that downstream consumers want to `errors.Is` on the narrower class. The wrapping rule (sub-sentinel wraps `ErrStructural`) plus the narrowest-first-dispatch obligation on consumers means `errors.Is` on either layer behaves predictably. Calling the set "six orthogonal classes" (as v0.1 did) conflated orthogonality with labeling; v0.2 names the structure honestly.

**Why skill provisioning is split between structural and transient failure.** The v0.1 spec conflated two failure modes under `ErrSkillProvisioningFailed`: (a) name does not resolve against search paths — genuinely structural, no plan that names the same skill will succeed; (b) name resolves but provisioning fails at runtime (network fetch timeout, 5xx, disk-full) — often transient, the same provisioning would succeed on retry. v0.2 splits these: HC-048 (structural, fail-launch immediately) vs. HC-048a (adapter-classified; transient cases retry with bounded backoff before reclassifying to structural). The split also introduces `provisioning_timeout` distinct from `timeout` so installs don't consume the work budget.

**Why adapter-not-goroutine.** Putting per-session state in the watcher goroutine's closure means the daemon (S01) owns the concurrency boundary, and S04's adapter is a pure callback object. Adding a new agent type — the common case for extending harmonik — is then just writing an adapter; the concurrency boundary never moves. This is the concrete realization of the centralized-controller principle ([architecture.md §4.9]) at the concurrency layer.

**Why twin parity is an architectural invariant, not a test discipline.** If twins were "almost like real handlers but different in minor ways," every test using a twin would need a caveat. The invariant (same interface, same wire protocol, same event schema, same tagging) means the daemon has zero test-mode branches — the real-vs-twin choice is purely config-level. Twin conformance drift detection (keeping twins honest against real-agent evolution) is a separate concern, owned by S07 post-MVH.

**Why skill injection is fail-launch, not fail-soft.** An agent that starts work without its declared skills available silently produces bad work that fails at some later, harder-to-diagnose point (tool call returns "unknown command," a reference is missing, the agent hallucinates capabilities). Fail-launch is expensive in operator attention but cheap in wrong work; fail-soft is the opposite. The skill-injection obligation choosing fail-launch is the same trade the workflow-attribute validator makes at ingest-time.

**Why handler-as-modularity-boundary.** The execution shape (ntm + tmux + subprocess) is the most-likely-to-change surface in harmonik. Cloud execution, remote containers, a custom tmux + agent-profile library — all plausible. If the daemon couples to the execution shape, every change cascades. The handler contract (this spec) is the stable seam: the deterministic daemon sits on one side; the execution shape sits on the other. Evolution replaces adapters, not watchers; interface surfaces, LaunchSpec, event set, error taxonomy all remain stable. Proposals that would breach this seam are structural violations, not soft preferences.

**Why heartbeat + T = 600s rather than output-detection + T = 120s.** Silent-hang was v0.1's most-likely false-positive source: extended-thinking LLMs emit no output chunks for minutes during reasoning, and the v0.1 120s threshold would have killed healthy agents. Rather than tune T upward blindly, v0.2 adds a handler obligation (HC-026a): emit a heartbeat progress-stream message at ≤T/2 cadence, regardless of user-visible output. Silent-hang then means the process is actually stuck (no heartbeat), not that reasoning is taking a while. T is raised to 600s as a conservative upper bound that should never trigger on a healthy agent; the practical detection window is driven by heartbeat cadence, which handlers tune per-agent-type.

**Why twin-as-subprocess applies only to the canonical twin binary, not to every in-process Handler implementation.** The locked user decision was "twin binaries not in-process mocks" in the context of scenario tests: there, subprocess-fidelity matters. But unit tests of the adapter, the error classifier, or the watcher state machine are better served by hand-written in-process fakes that skip the wire-protocol handshake. v0.2 carves this out explicitly: HC-035/HC-036 apply to the canonical twin; unit-test fakes are not twins. The daemon still has zero `if isTwin` branches — that property is about the daemon's code, not about whether every `Handler` implementation is a subprocess.
