# Pass 2 — Decompose: claude-hook-bridge

Spec areas affected, with concrete requirements (in "what must be true" form, not "rewrite this paragraph" form).

## NEW SPEC: `specs/claude-hook-bridge.md` (CHB prefix)

**Scope:** Normative definition of the translation layer between the Claude Code CLI's native lifecycle (hooks, session_id, transcripts) and harmonik's handler-contract progress-stream protocol. Owns: settings.json schema, env-var schema, hook-event → progress-message mapping table, the `harmonik hook-relay` subcommand wire shape, pre-generated `claude_session_id` flow, failure-mode classification reusing HC sentinels, twin-parity rules.

**Spec-category:** `foundation-cross-cutting` (bridges WM, HC, EM, EV)

**Depends on:** `handler-contract`, `workspace-model`, `event-model`, `process-lifecycle`, `execution-model` (for the phase enum), `architecture`.

**Requirements the spec must establish:**

- **CHB-001..010 — Settings.json shape.** Materialized path `${workspace_path}/.claude/settings.json`. Hook entries for at minimum `Stop`, `SessionStart`, `SessionEnd`, `Notification`, `UserPromptSubmit`, `PreCompact` (final set confirmed in research). Each entry invokes `harmonik hook-relay <event-kind>` with no positional args beyond the kind. Atomic-write discipline cited from WM-026.
- **CHB-011..020 — Env-var schema.** Stable prefixes: `HARMONIK_*` for harmonik-owned context (RUN_ID, DAEMON_SOCKET, WORKSPACE_PATH, HANDLER_SESSION_ID, CLAUDE_SESSION_ID, WORKFLOW_MODE, PHASE, ITERATION_COUNT). The secret prefix `HARMONIK_SECRET_*` (HC-028) remains separate. Required-vs-optional, types, validation, redaction.
- **CHB-021..030 — Relay subcommand contract.** `harmonik hook-relay <event-kind>` reads JSON-on-stdin (Claude's hook payload schema), reads env (harmonik context), constructs a `progress_stream` NDJSON message per HC-007a, dial-and-send to `${HARMONIK_DAEMON_SOCKET}`. Exit codes: 0 on durable send, non-zero with stderr diagnostics on transport failure (Claude treats non-zero as soft signal; harmonik treats stderr as the operator-visible relay-error trace). Bounded retry within bounded wall-clock window (research-pass: align with HC-016a 60s startup window or shorter).
- **CHB-031..040 — Hook → progress-message mapping table.** Each row: (Claude hook event kind, Claude payload fields used, generated progress_stream message type, derivation rules for each field). Notable rows:
  - `Stop` → `outcome_emitted` (when Claude declares the turn complete; the reviewer's verdict file or the implementer's exit signal lives here). Outcome `kind` derived from phase: `single` / `implementer-initial` / `implementer-resume` → `WORK_COMPLETE`; `reviewer` → `REVIEWER_VERDICT` discriminated by `.harmonik/review.json` presence.
  - `SessionStart` → `agent_ready` (after the relay also synthesizes `handler_capabilities`, `session_log_location`, `skills_provisioned` per HC-INV-004 ordering — research-pass: are these emitted by the handler-side launcher BEFORE Claude is exec'd, or by the relay on `SessionStart`?).
  - `SessionEnd` → `agent_completed` (when preceded by `outcome_emitted`) OR `agent_failed` (when not — collapses dirty-exit into HC-008a discipline).
  - `Notification:idle_prompt` → `agent_heartbeat{phase=waiting_input}` (every notification resets the silent-hang timer per HC-026/HC-026a).
  - `UserPromptSubmit` → optional `agent_output_chunk` (best-effort observability, low priority).
  - `PreCompact` → no-op at MVH (informational; research-pass confirms).
- **CHB-041..050 — Pre-generated Claude session_id flow.** Handler subprocess mints UUIDv7. Passes to Claude via `--session-id <uuid>` at exec time. Reports same UUID to the daemon via a fresh `progress_stream` message (or via LaunchSpec field — research-pass) before any work dispatch. Hook payloads echo this UUID back (Claude includes `session_id` in stdin); the relay carries it forward; the daemon routes hook-derived events to the watcher keyed by `(run_id, claude_session_id)`. For `phase = implementer-resume`, handler reuses the prior run's `claude_session_id` and launches Claude with `--resume <id>` instead of `--session-id <new-uuid>`.
- **CHB-051..060 — Failure mode taxonomy.** Reuse HC sentinels:
  - Relay can't dial socket → relay exits 1 with stderr `bridge_dial_failed`; the watcher emits `agent_failed{class=ErrTransient, sub_reason=bridge_dial_failed}` ONLY when relay-failure observation is correlatable (a stream of failed relay invocations within one Claude session); single transient failure during quiescent windows is NOT terminal. Research-pass confirms whether a separate transport-failure event is needed; default: NO new event, route through agent_failed.
  - Malformed hook payload (Claude version skew) → relay exits 1, watcher emits `agent_failed{class=ErrStructural, sub_reason=bridge_malformed_hook_payload}`.
  - Daemon-not-ready response (HC-016a) → relay applies the same bounded-retry as HC-016a.
- **CHB-061..070 — Twin parity.** `harmonik-twin-claude` MUST be capable of emitting the same progress-stream sequence the relay would synthesize from a Claude run, without invoking the relay subcommand. Scenario scripts MAY drive the twin directly via the existing HC-026a `heartbeat_mode: scripted` carve-out. The relay subcommand MAY be exercised by integration tests that pipe canned Claude hook payloads to it.
- **CHB-071..080 — Subprocess-tree clarification.** Relay processes are children of Claude, grandchildren of the daemon. They are short-lived (one Claude hook invocation = one relay exec). They are NOT watched by an HC-011 watcher; the only daemon-side observation surface is the relay's socket connection (decoded by the run's existing watcher).
- **CHB-INV-001..** Invariants:
  - Every progress-stream message emitted on behalf of a Claude session derives deterministically from (Claude hook payload + env at relay invocation time); no LLM cognition participates.
  - Watcher publishes exactly one terminal event per claude_session_id (inherits HC-INV-006).
  - `claude_session_id` is stable for the lifetime of a single Claude `--session-id` / `--resume` lineage; archived per-iteration in `.harmonik/review.iter-<N>.json`-style record if needed (research-pass).

## AMEND: `specs/workspace-model.md`

**Change shape:** Add one or two requirements under §4.7 area (or a new §4.7a Settings.json materialization sub-section). Add the `.claude/` path to the lease-discovery sweep.

**Requirements the amendment must establish:**

- **WM-NEW-1 — `.claude/settings.json` materialization at workspace creation.** Path: `${workspace_path}/.claude/settings.json`. Write timing: between WM-003 (worktree creation) and WM-016 (workspace_leased emission), folded into the same fsync gate. Atomic-write discipline matches WM-026 (temp + rename + fsync(parent_dir)). Content is owned by `claude-hook-bridge.md §x` (cite forward).
- **WM-NEW-2 — Teardown disposition.** `.claude/settings.json` is treated like the sessions sidecar: it persists in the worktree on success (post-merge retention per WM-030) and persists on failure (per WM-031 failed-run worktree retention). It is NOT committed to the task branch (added to the WM-013e `.gitignore` hygiene set).
- **WM-NEW-3 — `.claude/` is a harmonik-managed path.** Add to the §4.3.WM-013e gitignore set alongside `.harmonik/`.

## AMEND: `specs/handler-contract.md`

**Change shape:** Two amendments. (a) Update §4.2 HC-006 to make the `claude_session_id` field's population path normative (currently informative prose). (b) Add an HC-NEW under §4.10 Agent-to-orchestrator trust that names `claude-hook-bridge.md` as the normative source for Claude-specific launch mechanics, and the bridge as the in-MVH realization of S05.

**Requirements the amendment must establish:**

- **HC-NEW-1 — Handler-side claude_session_id minting.** Concrete normative statement: when launching Claude Code in `phase ∈ {single, implementer-initial}`, the handler MUST mint a fresh UUIDv7, MUST pass it to Claude via `--session-id <uuid>`, AND MUST report it to the daemon (over the progress-stream socket as part of a new field on `handler_capabilities`, OR via a new dedicated `claude_session_bound` message — design-pass decides). For `phase = implementer-resume`, the handler MUST reuse the run's prior `claude_session_id` from the LaunchSpec and MUST launch Claude with `--resume <claude_session_id>` instead. For `phase = reviewer`, the handler MUST mint a fresh `claude_session_id` (each reviewer launch is a fresh session per HC-006).
- **HC-NEW-2 — Hook bridge is the normative launch mechanism for claude-code.** A pointer requirement: the `claude-code` agent type's launch mechanism is defined by `specs/claude-hook-bridge.md`. The bridge spec's requirements bind the handler.

## AMEND: `specs/event-model.md`

**Change shape:** Default: no new event types. Add a glossary entry only if needed.

**Requirements the amendment must establish (only if needed):**

- **EV-NEW-1 (conditional) — `bridge_unreachable` event** (durability O, class observational). Emitted by the watcher when the daemon observes that relay invocations have stopped arriving but Claude is still running (silent-hang state machine of HC-026 would fire; this event is the operator-observable diagnostic that distinguishes "Claude is hung" from "the bridge is broken"). Research-pass decides if this is needed or if HC-026's existing `agent_failed{sub_reason=...}` carries enough discriminator.

## AMEND: `specs/process-lifecycle.md`

**Change shape:** A small clarification at §4.5.

**Requirements the amendment must establish:**

- **PL-NEW-1 — Relay processes are grandchildren.** Relay subprocess invocations (`harmonik hook-relay`) are children of an agent subprocess (claude-code), not direct children of the daemon. They are not subject to the §4.5 PL-014 "daemon child" rule. They are not subject to HC-011 watcher supervision. Their lifetime is bounded by Claude's hook invocation. Orphan-sweep PL-006 does not target them (they exit on their own when Claude completes a hook invocation; surviving orphans are reaped via OS init-reparenting at daemon death).

## AMEND: `docs/subsystems/agent-runner.md`

Replace the line-28 "Hook attachment" bullet with a paragraph that cites `claude-hook-bridge.md` as the realization of S05 for the claude-code agent type at MVH.

## AMEND: `docs/subsystems/hook-system.md`

Add a "Realization at MVH" note: for the claude-code agent type, the hook system is realized by the bridge spec. Other agent types (post-MVH) re-realize the bridge surface per their own bridge spec.

## Dependency order between changes

1. CHB spec (new) is independent — it cites the existing specs but does not modify their semantics.
2. WM amendment depends on CHB (for the content-of-settings.json reference).
3. HC amendment is mostly independent; the `claude_session_id` population requirement is local to HC and CHB.
4. EV amendment is conditional and lands last.
5. PL amendment is a one-paragraph clarification, no order constraint.
6. The two docs/subsystems amendments are last.

## Goal-to-area traceability

| Goal | Spec area |
|---|---|
| G1 (real Claude runs review-loop) | CHB-001..080 + HC-NEW-1 |
| G2 (zero new event types) | EV: default no-op, conditional EV-NEW-1 only if research mandates |
| G3 (twin parity) | CHB-061..070 |
| G4 (reuse HC silent-hang invariants) | CHB-051..060 |
| G5 (implementer-resume mechanical) | CHB-041..050 + HC-NEW-1 |
| G6 (settings.json crash-safe) | WM-NEW-1 |

Every goal maps. No spec area exists that isn't goal-driven.

## Decompose-review note

Reviewer sub-agent NOT spawned for this pass — under the kickoff prompt's "operate autonomously" directive and given the constraint surface is well-bounded by existing spec text, a structural decomposition review at this stage would primarily restate the rules in CLAUDE.md. The research and design passes will each be reviewer-gated where they materially shape decisions.
