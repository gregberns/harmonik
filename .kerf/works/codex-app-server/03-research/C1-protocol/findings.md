# C1 — codex app-server: protocol & session surface (research findings)

> Codename: codex-app-server · Component C1
> Sources fetched 2026-07-10: official docs (developers.openai.com/codex/app-server →
> 308-redirects to learn.chatgpt.com/docs/app-server), the openai/codex GitHub
> app-server README, and the harmonik-local codex-harness research cache.

## Summary

`codex app-server` is a long-lived, bidirectional **JSON-RPC 2.0** process (stdio /
WebSocket / Unix-socket transports) that OpenAI's own Codex VS Code extension and
desktop app use to drive Codex as a *client* — explicitly **not MCP**, though it
borrows MCP's bidirectional JSON-RPC style. It models work as **Thread → Turn →
Item**: a client `initialize`s the connection, calls `thread/start` (or
`thread/resume`) to get a server-minted thread id, then `turn/start` to send a user
prompt into that thread and reads a stream of `item/*` and `turn/*` notifications
back (agent-message deltas, tool/command events, token usage) until `turn/completed`.
**Threads are durable and resumable across process restarts** — the server persists
each thread as rollout JSONL log files plus SQLite metadata and reloads history on
`thread/resume`, so **the server holds the conversation/context window, not the
client**. Context management is server-side (explicit `thread/compact/start` +
`thread/tokenUsage/updated` streaming). One app-server hosts **many concurrent
threads**. The load-bearing consequence for harmonik: a resident Codex thread over
app-server would carry no client-side growing context buffer, which is exactly the
thing keeper/handoff machinery exists to manage.

---

## 1. What is `codex app-server`? — CONFIRMED

Confirmed as a long-lived, bidirectional JSON-RPC 2.0 process used by the Codex VS
Code extension / desktop app to drive Codex as a client, and explicitly distinguished
from MCP.

- Official docs: *"Like MCP, codex app-server supports bidirectional communication
  using JSON-RPC 2.0 messages"* but it is **not MCP** — it is a first-party control
  surface with Thread/Turn/Item primitives. (https://learn.chatgpt.com/docs/app-server
  ; canonical URL https://developers.openai.com/codex/app-server)
- Transports: stdio (newline-delimited JSON), WebSocket, and Unix sockets.
  (https://learn.chatgpt.com/docs/app-server)
- Repo: `codex-rs/app-server/` in openai/codex is the Rust implementation; it is the
  same protocol the VS Code extension and desktop app speak.
  (https://github.com/openai/codex/blob/main/codex-rs/app-server/README.md)
- harmonik's own prior spike already characterized it this way ("a long-lived
  bidirectional JSON-RPC 2.0 process (durable, resumable threads; survives restarts)
  that is *not* MCP"): `/Users/gb/github/harmonik/docs/design/crew-harness-select.md`
  §"C. app-server / proto driver".

Primitives (three nested concepts):
- **Thread** — a conversation between user and agent; contains turns.
- **Turn** — one user request + the agent work that follows; contains items and
  streams incremental updates.
- **Item** — a unit of input/output (user message, agent message, command run, file
  change, tool call, reasoning, …).
  (https://learn.chatgpt.com/docs/app-server)

## 2. JSON-RPC method / notification set — CONFIRMED

Handshake (required before anything else):
- `initialize` request with `clientInfo` (name/title/version); then the client emits
  an `initialized` notification. Any request before the handshake is rejected
  ("Not initialized"). `capabilities.optOutNotificationMethods` suppresses named
  notifications per connection; `capabilities.experimentalApi: true` gates
  experimental methods. (https://learn.chatgpt.com/docs/app-server)

Create / resume / branch a thread:
- `thread/start` — create a new thread (accepts model, cwd, approvalPolicy, sandbox
  restrictions); returns a thread object with a server-generated `id`; a
  `thread/started` notification follows.
- `thread/resume` — reopen an existing thread by stored `threadId`; reconstructs
  history.
- `thread/fork` — branch history into a new thread id (optional `ephemeral: true`
  in-memory thread; optional copy up to `lastTurnId`).
- Query without loading: `thread/read` (set `includeTurns: true` for full history),
  `thread/list` (cursor pagination; filters `archived`, `cwd`, `modelProviders`,
  `searchTerm`), `thread/loaded/list` (in-memory thread ids).
  (https://learn.chatgpt.com/docs/app-server ;
  https://github.com/openai/codex/blob/main/codex-rs/app-server/README.md)

Send a user turn (prompt) into an existing thread:
- `turn/start` with target `threadId` + user input; returns a turn object immediately
  and emits `turn/started`. Optional `clientUserMessageId` correlation id lets the
  client match responses.
- `turn/steer` — append user input to an already-running turn (no new turn).
  (https://learn.chatgpt.com/docs/app-server)

Receive streamed model output + tool-call events (server→client notifications after
`turn/start`):
- `item/started`, `item/completed`, `item/agentMessage/delta` (agent text chunks),
  plus reasoning/plan/command-output and tool-progress notifications; terminates with
  `turn/completed` carrying final state + token usage.
  (https://learn.chatgpt.com/docs/app-server)
- Tool calls run through configured MCP servers; e.g. `mcpServer/tool/call`, and
  `tool/requestUserInput` prompts the user during tool execution; `approvalPolicy`
  controls confirmation. (https://learn.chatgpt.com/docs/app-server)

End / interrupt / close:
- `turn/interrupt` (thread + turn id) → turn finishes with `status: "interrupted"`.
- `thread/unsubscribe` removes a connection's subscription; when no subscribers remain
  the server waits **30 minutes** of inactivity, then emits `thread/closed` and
  unloads the thread from memory.
- Lifecycle/storage: `thread/archive`, `thread/unarchive`, `thread/delete`,
  `thread/rollback` (deprecated).
  (https://learn.chatgpt.com/docs/app-server)

## 3. Session / thread lifecycle & identity — CONFIRMED (durable & resumable);
   caller CANNOT mint the id

- **Durable & resumable across process restarts:** threads persist as rollout JSONL
  log files + optional SQLite metadata; `thread/resume` reloads JSONL history so the
  agent continues where it left off — including after a server restart.
  (https://learn.chatgpt.com/docs/app-server)
- **Identity:** a thread is identified by a server-generated `id` (docs show
  `"thread": { "id": "thr_123" }`). Threads also carry `thread.sessionId`.
  (https://learn.chatgpt.com/docs/app-server)
- **Minting:** the **server** generates the thread id; the caller does **not** mint it
  up front — it can only be **captured** from the `thread/start` response /
  `thread/started` notification. This matches the CLI-side finding that codex's
  pre-mint-the-session-id request was **closed NOT_PLANNED (2026-05-29, #25111)**:
  `/Users/gb/github/harmonik/.kerf/works/codex-harness/04-research/codex-cli/findings.md`
  §3. (also https://learn.chatgpt.com/docs/app-server)

## 4. CRITICAL — context / compaction handled SERVER-SIDE — CONFIRMED

The server owns the conversation/context window. The client sends turns and reads
event streams; it does **not** carry a growing context buffer.

- History lives server-side (rollout JSONL + SQLite) and is reconstructed by the
  server on `thread/resume`; the client re-attaches to a thread rather than replaying
  a buffer it holds. (https://learn.chatgpt.com/docs/app-server)
- Explicit server-side compaction method: `thread/compact/start` — *"triggers
  conversation history compaction for a thread; returns {} immediately while progress
  streams via turn/* and item/* notifications."*
  (https://learn.chatgpt.com/docs/app-server ;
  https://github.com/openai/codex/blob/main/codex-rs/app-server/README.md)
- Token usage is reported by the server via `thread/tokenUsage/updated`; on resume the
  server emits restored usage immediately so the client can render it before the next
  turn. This confirms the *server* is the accountant of the window, not the client.
  (https://learn.chatgpt.com/docs/app-server)
- Backpressure is server-managed: bounded queues, JSON-RPC error `-32001` "Server
  overloaded; retry later." (https://learn.chatgpt.com/docs/app-server)

OPEN QUESTION — **automatic vs caller-triggered compaction.** The presence of an
explicit `thread/compact/start` method plus the `experimentalApi` gating means at
least *manual* compaction is caller-invokable. Whether app-server also performs
**automatic** compaction when a thread nears the window (the CLI `codex exec` path is
known to auto-compact) was **not confirmed from these sources**. Either way the
history/window state is held server-side; the open point is only *who pulls the
compaction trigger*, not *where the buffer lives*.

## 5. Restart / reconnect & auth — CONFIRMED (state recoverable); auth partial

- **Restart recovery:** because threads are persisted (JSONL rollout + SQLite),
  thread state is recoverable after an app-server crash/restart — a client reconnects,
  `initialize`s, and `thread/resume`s the stored `threadId` to reload history.
  (https://learn.chatgpt.com/docs/app-server)
- **In-memory unload (not a crash):** an idle loaded thread unloads after 30 min of
  inactivity with no subscribers (`thread/closed`); this is a memory-eviction, and the
  persisted thread is re-loadable via `thread/resume`.
  (https://learn.chatgpt.com/docs/app-server)
- **Auth:** WebSocket transport supports bearer-token auth (`--ws-auth` flags).
  Experimental `mcpServer/oauth/login` initiates OAuth flows for configured MCP
  servers (returns an `authorization_url`, then `mcpServer/oauthLogin/completed`).
  (https://learn.chatgpt.com/docs/app-server)
- OPEN QUESTION — **how the app-server authenticates to the OpenAI/Codex backend
  itself** (i.e. the ChatGPT login / API-key the underlying model calls use) was not
  spelled out in the fetched pages; the app-server presumably inherits the local
  `codex` CLI's auth (`~/.codex/`), but that is **not confirmed** from C1 sources.

## 6. Concurrency — CONFIRMED (many threads per server)

- Multiple threads coexist in a single app-server session; each `thread/start` /
  `thread/resume` / `thread/fork` subscribes the initiating connection to that
  thread's turn/item events.
- `thread/loaded/list` reports currently in-memory thread ids; `thread/list` pages
  through stored threads; `thread/status/changed` notifies on loaded-thread status
  transitions. Clients can drive multiple active threads simultaneously via separate
  turn requests. (https://learn.chatgpt.com/docs/app-server ;
  https://github.com/openai/codex/blob/main/codex-rs/app-server/README.md)
- OPEN QUESTION — whether concurrent *turns run in true parallel* vs are serialized
  per underlying model/quota was not stated; only that many threads can be loaded and
  addressed concurrently.

---

## Keeper-relevance verdict (factual half)

**Yes — app-server manages the conversation context window server-side.** The
conversation history is persisted by the server (rollout JSONL + SQLite), reconstructed
by the server on `thread/resume`, and its size is tracked server-side
(`thread/tokenUsage/updated`) with a server-side compaction primitive
(`thread/compact/start`). The client's role is to send `turn/start` inputs and consume
event streams; it does **not** hold or replay a growing context buffer.

**Therefore, factually, a resident Codex session driven over app-server could avoid a
client-side context window entirely.** A harmonik driver would keep only a small,
bounded connection state (the JSON-RPC socket + the captured `thread_id` + last-seen
turn/item cursor), not an ever-growing transcript. That removes the specific condition
keeper/handoff machinery exists to manage on the Claude harness: a client-side context
buffer that fills and forces a handoff → /clear → resume cycle before the pane
overflows. This corroborates the harmonik spike's Option-B note that a Codex loop has
"no client-side context-window to gauge and no clear→resume cycle to drive"
(`/Users/gb/github/harmonik/docs/design/crew-harness-select.md` §B) — and shows the
same server-side-context property holds for the *app-server* path (Option C), not only
the `codex exec resume` path.

Caveat (not a contradiction): retiring keeper client-side does **not** eliminate
*window exhaustion* as a concern — the model still has a finite window server-side.
It relocates the management: instead of a client keeper driving handoffs, the server
handles compaction/token-accounting, with the open point (see §4) being whether that
compaction is automatic or must be caller-triggered via `thread/compact/start`.

## Open questions (consolidated)

1. §4 — Is compaction **automatic** at window-pressure, or only caller-triggered via
   `thread/compact/start`? (experimental flag suggests at least manual; auto not
   confirmed.)
2. §5 — How does app-server authenticate to the **model backend** (ChatGPT login vs
   API key; does it inherit `~/.codex/` auth)? Not confirmed from C1 sources.
3. §6 — Do concurrent threads execute turns **in parallel**, or are model calls
   serialized per quota? Not stated.
4. Source-confidence: the two web fetches (developers.openai.com redirect →
   learn.chatgpt.com, and the GitHub README) were summarized by the fetch model; exact
   method names above should be re-verified against the raw
   `codex-rs/app-server/README.md` and any `*.proto`/schema files in that directory
   before they are treated as a normative wire contract for implementation.
