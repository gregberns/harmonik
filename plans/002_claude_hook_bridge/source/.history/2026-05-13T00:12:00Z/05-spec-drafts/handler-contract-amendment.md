# Amendment to specs/handler-contract.md (v0.3.3 → v0.3.4)

## Frontmatter

- `version: 0.3.3` → `version: 0.3.4`
- `last-updated: 2026-04-25` → `last-updated: 2026-05-12`

## New requirements

The existing HC-053 in §6.2 ("Cross-subsystem surface MUST remain stable across shape evolution") is occupied; the three new requirements introduced by this kerf are placed as gap-filler IDs after HC-045 in §4.10, matching the gap-filler pattern previously used for HC-016a / HC-026b in v0.3.3. No prior IDs are renumbered.

### Add to §4.10 (after HC-045):

#### HC-045a — Pointer to claude-hook-bridge.md for claude-code agent type

For `agent_type = "claude-code"`, the launch mechanism, `.claude/settings.json` materialization, hook-event-to-progress-message translation, env-var schema, and failure-mode classification are normatively defined by [claude-hook-bridge.md]. This spec (handler-contract) defines the cross-handler invariants; the bridge spec defines the claude-code-specific realization.

Tags: mechanism

### Add to §4.10 (after HC-045a):

#### HC-045b — Hook-bridge connection regime

Some handler subsystems (notably the claude-code bridge per [claude-hook-bridge.md]) cause additional short-lived subprocesses to be spawned by the agent subprocess. These short-lived subprocesses MAY open one-shot NDJSON connections to the daemon socket (per HC-007's transport, per HC-007a's framing) carrying a single progress-stream message and then closing. Such connections MUST carry both `run_id` and `claude_session_id` in the message envelope at the top level (so the daemon's connection acceptor can route the message to the correct session-bound watcher). Such connections are NOT subject to HC-007's "sole bidirectional channel" phrasing (which scopes to the handler subprocess itself, not to incidental short-lived subprocesses spawned by the agent). HC-INV-007 (watcher is sole authoritative publisher) is preserved because the watcher publishes all messages from all connection regimes to the bus.

Per-connection lifetime requirements: dial timeout ≤ 5 s, single message ≤ 1 MiB per HC-007a, optional ack-line read with 5 s deadline, then close. Failure modes are classified per HC-020 and routed through `agent_failed` per HC-024.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent

### Add to §4.10 (after HC-045b):

#### HC-045c — Handler-side claude_session_id minting and resume

For `agent_type = "claude-code"`, the handler subprocess MUST observe the following session-id lifecycle:

(a) For `phase ∈ {single, implementer-initial, reviewer}`: the handler MUST mint a fresh UUIDv7 as `claude_session_id`, pass it to Claude via `--session-id <claude_session_id>`, AND include `claude_session_id` in the payload of the `handler_capabilities` progress-stream message per §4.2.HC-009.

(b) For `phase = implementer-resume`: the handler MUST reuse `LaunchSpec.claude_session_id` (carried from the prior iteration, populated by the daemon per §4.2.HC-006), pass it to Claude via `--resume <claude_session_id>` (NOT `--session-id`), and include the same value in `handler_capabilities`.

(c) The handler MUST NOT pass `--fork-session`, `--bare`, or `--no-session-persistence` flags to Claude, and MUST NOT set the env var `CLAUDE_CODE_SKIP_PROMPT_HISTORY`; these flags / vars conflict with bridge invariants per [claude-hook-bridge.md §4.2 CHB-007].

(d) Each reviewer phase MUST mint a fresh `claude_session_id`; the handler MUST NOT inherit reviewer claude_session_id across iterations.

(e) Orphan-reconnect lookups (per §4.3 HC-016a) MUST resolve `claude_session_id` from `Run.context.claude_session_id` reconstructed per [execution-model.md §4.7 EM-031] from the git checkpoint trail; JSONL-tail reads MUST NOT be used as the source of truth for `claude_session_id`.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent

## Amendment to §4.2 HC-006 (clarification, not renumbering)

In the existing HC-006 paragraph describing `claude_session_id`, add a sentence at the end:

> The handler-side minting and propagation discipline for `claude_session_id` is normative per §4.10.HC-045c; the LaunchSpec field is populated by the daemon per HC-006 (this requirement) for the resume case only, and the daemon's durability obligation for the persisted value is per [claude-hook-bridge.md §4.6 CHB-023].

## Revision-history entry

| 2026-05-12 | 0.3.4 | foundation-author | Add HC-045a / HC-045b / HC-045c in §4.10 (gap-filler placement after HC-045, matching the HC-016a / HC-026b pattern) covering claude-code agent type's launch mechanism (pointer to claude-hook-bridge.md), hook-bridge one-shot NDJSON connection regime, and handler-side claude_session_id minting/resume discipline including orphan-reconnect git-derived lookup. Clarifying sentence added to HC-006 pointing forward to HC-045c and to CHB-023's durability boundary. No requirement IDs renumbered or retired; HC-053 in §6.2 is unchanged. Status remains `reviewed`. |
