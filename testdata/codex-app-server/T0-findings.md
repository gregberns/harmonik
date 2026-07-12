# T0 Spike — codex app-server (Phase-1)

Run: 2026-07-11, codex-cli 0.142.5, /opt/homebrew/bin/codex, host gb-mac-mini.local.

## Auth
CONFIRMED. Auth carried silently from `~/.codex/auth.json` — no interactive login prompt.
`initialize` returned `codexHome=/Users/gb/.codex`, the live model replied `ok`, and a
real `account/rateLimits/updated` frame arrived (limitId `codex`). No `account/login/*`
needed.

## Working invocation
- Transport: **stdio is the default** — just `codex app-server` (equiv `--listen stdio://` / `--stdio`).
  Speaks newline-delimited JSON-RPC 2.0 on stdin/stdout.
- No proxy/daemon needed for a one-shot capture. (`codex app-server daemon start` + `codex app-server proxy --sock <path>` is the alternate path for talking to a shared long-lived daemon.)
- Handshake that worked:
  1. `initialize` → params `{clientInfo:{name,title,version}, capabilities:null}`; result gives `userAgent, codexHome, platformFamily, platformOs`.
  2. `initialized` **notification** (no id, method only — the sole client notification).
  3. `thread/start` → params `{cwd}` (all fields optional); result `thread.id` = server-minted thread id.
  4. `turn/start` → params `{threadId, input:[{type:"text", text, text_elements:[]}]}`. NOTE: `text_elements:[]` is REQUIRED in the `text` UserInput variant.
  5. Stream `item/*` + `thread/*` + `turn/*` notifications until `turn/completed`.

## Schema (authoritative)
`generate-json-schema --out <DIR>` / `generate-ts --out <DIR>` (both need `--out`, dump a
per-type file tree, not a single blob). Canonical schema saved as `protocol-schema.json`
(= `codex_app_server_protocol.schemas.json`, 551 KB). Full generated tree kept under `gen/`.
Method enum authoritative source: `gen/ClientRequest.ts`, `gen/ClientNotification.ts`,
`gen/ServerNotification.ts`.

## Real vs design-assumed method names — resolves OQ-7
All the design's assumed names are REAL and correct:
- `thread/start` ✓  `thread/resume` ✓  `turn/start` ✓  `turn/steer` ✓
- `turn/completed` ✓  `thread/tokenUsage/updated` ✓  `thread/compact/start` ✓
- `item/*` family ✓ (`item/started`, `item/completed`, `item/agentMessage/delta`, `item/reasoning/*`, `item/commandExecution/*`, `item/fileChange/*`, `item/plan/delta`, `item/mcpToolCall/progress`, `item/autoApprovalReview/*`).

Deltas / additions worth flagging for T2:
- Cancel is **`turn/interrupt`** (not `turn/cancel`). `turn/steer` exists as assumed.
- `initialized` is a client→server NOTIFICATION, not a request.
- Turn lifecycle notifications: `turn/started`, `turn/completed`, plus `turn/plan/updated`,
  `turn/diff/updated`, `turn/moderationMetadata`.
- Extra thread-lifecycle signals seen live: `thread/started`, `thread/status/changed`
  (`{type:"active"|"idle"}`), `thread/compacted` (past-tense completion vs the request
  `thread/compact/start`).
- v2 surface is large (fs/*, plugin/*, marketplace/*, command/exec/*, account/*, config/*,
  skills/*, model/list, review/start) — out of Phase-1 scope but present.

## Corpus
- Path: `/Users/gb/github/harmonik/testdata/codex-app-server/corpus/raw-session-01.jsonl`
  (copied to bench `~/.kerf/projects/gregberns-harmonik/codex-app-server/corpus/`).
- 23 frames, verbatim both directions. Covers the full happy path:
  initialize → initialized → thread/start → thread/started → turn/start → turn/started →
  item/started(userMessage) → item/completed(userMessage) → item/started(agentMessage) →
  item/agentMessage/delta → item/completed(agentMessage "ok") → thread/tokenUsage/updated →
  account/rateLimits/updated → thread/status/changed(idle) → turn/completed.
- Also captured unsolicited server notifications at startup: `configWarning`
  (project-untrusted: local config/hooks/exec-policies disabled until trusted, skills still load),
  `remoteControl/status/changed`, `mcpServer/startupStatus/updated` (codex_apps MCP: starting→ready).

## Thread/turn id format
Both are **UUIDv7** (time-ordered): thread_id `019f5489-8dde-7ed2-81c3-5848fe26f1ac`,
turn_id `019f5489-8e9f-7d62-b86c-6020273ed855`. `thread.id == thread.sessionId` on a fresh
thread. Item ids are mixed: userMessage = plain UUIDv4; agentMessage = `msg_<hex>`.

## Surprises / notes for T2
- **Token usage shape** (`thread/tokenUsage/updated`): `{tokenUsage:{total,last:{totalTokens,
  inputTokens,cachedInputTokens,outputTokens,reasoningOutputTokens}, modelContextWindow}}`.
  Trivial turn cost 15,825 total tokens (15,820 in / 5 out, 2,432 cached) — the system prompt
  dominates; context window 258,400.
- **Compaction**: request is `thread/compact/start`; completion notification is
  `thread/compacted`. Auto-compaction is governed by `AutoCompactTokenLimitScope` in the schema
  (config-driven), so both auto and manual paths exist. Not exercised this run.
- **Parallel turns**: not exercised. `turn/start` returns a `turn` object immediately with
  `status:"inProgress"`; steering/interrupt are per-turn (`turn/steer`, `turn/interrupt`).
- `configWarning` on untrusted project means project-local hooks/exec-policy won't load unless
  the cwd is trusted — relevant if T2 needs harmonik's local config/hooks active.

## OQs resolved
- OQ-7 (method names web-summarized): RESOLVED — authoritative schema confirms all assumed
  names; only correction is cancel = `turn/interrupt`.
