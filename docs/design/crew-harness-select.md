# crew-harness-select — Codex crew-orchestrator session model (spike)

> codename:crew-harness-select · hk-fijwi · parent hk-q3ovr
> DESIGN spike — decision, not yet built. Feeds a future kerf work if adopted.

## The question

The Codex *worker* harness (`internal/daemon/codexharness.go`, kerf work
`codex-harness`) is done: a worker turn is spawn-per-turn, `codex exec --json`,
self-terminating, resumed via `codex exec resume <thread_id>`. That shape is a
clean fit for a bounded implement/review turn.

A crew **orchestrator** (captain or crew lead) is a different animal:
`buildCrewLaunchSpec` (`crewlaunchspec.go`) launches Claude as a **long-lived
loop** — one continuous `claude --remote-control <label> --session-id <uuid>`
process that stays resident for a whole session, joins comms, submits/monitors
a named queue, closes beads, and gets re-tasked *while alive* via
bracketed-paste into the live pane. Nothing about the worker harness answers
whether/how Codex could fill that role.

## Why the worker model doesn't transfer

| Orchestrator need | Claude (today) | Codex `exec` |
|---|---|---|
| Stay resident across many turns | one process, whole session | `exec` exits per turn (confirmed, `04-research/codex-cli/findings.md` §5) |
| External push mid-session (comms message, watch escalation, operator directive) | bracketed-paste into the live REPL via `--remote-control` | **none** — `exec` has no remote-control flag, no paste target (`04-research/current-harness/findings.md` L18) |
| Caller-minted, stable identity for --resume / keeper mapping | `--session-id <uuid>` minted up front | thread_id only **captured** post-launch; pre-minting closed NOT_PLANNED 2026-05-29 (`04-research/codex-cli/findings.md` §3) |
| Live interactive TUI | `--remote-control` session picker | plain `codex` TUI exists but has **no caller-minted session-id and no remote-control channel** — confirmed by web research below; not headless-drivable |

So none of the three literal options in the task ("persistent Codex TUI",
"bounded `codex exec resume` re-invoke loop", "app-server/proto driver") is a
drop-in replacement for `--remote-control`. Each trades a different piece of
orchestrator behavior away.

## Option survey

### A. Persistent interactive `codex` TUI
Run the bare `codex` REPL resident in a tmux pane, like Claude today.
- **Rejected.** No caller-minted session id, no scriptable input channel
  analogous to `--remote-control`'s paste path. Codex's own "Codex Remote"
  (GA 2026-06-25) is a **human-to-phone** remote control for the interactive
  TUI, not a machine-drivable API a harmonik process could push structured
  turns into. Driving it would mean the same bracketed-paste hacks the
  Claude harness already carries, for a *TUI harmonik didn't build the paste
  contract for* — pure risk, no gain over Claude's existing REPL.

### B. Bounded `codex exec resume` re-invoke loop — **recommended**
Do not try to make Codex *be* the long-lived loop. Invert it: the harmonik
daemon holds the loop (comms membership, queue subscription, event wake),
and each time there is new work to react to it invokes one decision turn:

```
codex exec resume <thread_id> "<digest of new comms/queue/watch events>"
```

The turn runs with normal shell access (same as a worker turn), so it can
call `harmonik queue submit`, `harmonik comms send`, `br update`, etc.
directly — an orchestrator turn is just a worker turn whose prompt is "here
is what changed since you last looked; act." `thread_id` (captured on first
launch, `codexRunArtifacts`) threads continuity across turns exactly like a
worker's iteration ≥2 resume already does — **this reuses
`buildCodexLaunchSpec`'s resume path almost unchanged** (`04-research/codex-cli
/findings.md` §3, `seam-design/findings.md` "KEY DE-RISK" section).

- **Comms-join** downgrades from *live* membership to *daemon-held mailbox*:
  the daemon (not Codex) holds the comms join/subscribe state for the crew
  and drains buffered messages into the next turn's prompt. Consistent
  semantics (nothing is lost), different latency (next wake, not instant
  push) — acceptable for a crew loop that already runs on a poll/wake
  cadence (see `docs/design/agent-wake-mechanism.md`).
- **Keeper-restart mapping** simplifies rather than needing new machinery:
  each turn is already a fresh process reconstructed from the server-side
  thread transcript + the injected digest, so there is no client-side
  context-window to gauge and no clear→resume cycle to drive. The
  daemon's wake loop *is* the keeper for a Codex orchestrator — one
  substrate covers both roles.
- **How captain drives it**: identical mechanism to how it drives Codex
  workers today (queue submit → daemon spawns → resume-chains on next
  wake), just pointed at a crew's inbox+queue instead of one bead. No new
  captain-side concept.
- **Cost**: smallest engineering delta of the three options — no new
  protocol client, no new transport. Reuses `codexRunArtifacts` /
  `buildCodexLaunchSpec` verbatim; the new part is purely daemon-side
  ("what counts as a wake for a crew orchestrator" — an extension of the
  existing per-crew queue-append trigger, not a new subsystem).

### C. app-server / proto driver
Codex ships a real persistent surface for this: `codex app-server` is a
long-lived bidirectional JSON-RPC 2.0 process (durable, resumable threads;
survives restarts) that is *not* MCP — it's the same protocol the VS Code
extension and desktop app use to drive Codex as a client
([App Server — Codex | OpenAI Developers](https://developers.openai.com/codex/app-server),
[openai/codex app-server/README.md](https://github.com/openai/codex/blob/main/codex-rs/app-server/README.md)).
This is the *architecturally* closest thing to `--remote-control`: a resident
process a harmonik driver could push new-turn events into without spawning.
- **Not recommended now.** It requires harmonik to embed a JSON-RPC client
  (new subsystem, new transport, new failure modes — restart/reconnect,
  auth, backpressure) to get a benefit Option B already captures for the
  orchestrator's actual need (Codex has its own shell tool; it doesn't need
  the app-server to *act*, only to *stay reachable*, and Option B's
  daemon-side mailbard gets "reachable enough" for free). Revisit if B's
  next-wake latency proves too coarse in practice, or if harmonik grows a
  second app-server client need (e.g. driving the desktop/VS Code surface)
  that would amortize the integration cost.

## Decision

**Adopt Option B** — bounded `codex exec resume` re-invoke loop, orchestrator
state (comms membership, queue subscription) held daemon-side, decision turns
spawned on wake. No persistent Codex process, no new protocol client.

Concretely, when this is built:
1. Extend the per-crew wake trigger (queue-append / comms-deliver) that
   already exists for Claude crews to also cover Codex-harnessed crews —
   same trigger, different `LaunchSpec` (Codex resume vs Claude paste).
2. The daemon holds comms `join`/`recv` on behalf of a Codex crew and folds
   drained messages into the next turn's seed prompt (new: a small digest
   builder, not a new comms primitive).
3. `thread_id` persists in the crew registry the same way `--session-id`
   does for Claude today, just captured instead of minted (`SessionIDCaptured`
   is already modeled in `handlercontract`).
4. No TUI, no `--remote-control`, no app-server client — out of scope unless
   B's wake latency proves insufficient in practice.

## Open risk carried forward

- Wake latency: a Codex crew reacts only at the next spawned turn, not
  instantly. Fine for queue/comms-driven work; a genuinely latency-sensitive
  escalation path (e.g. an IMMEDIATE operator interrupt) may need an
  explicit fast-wake trigger rather than waiting for the normal cadence —
  flag for whoever builds this, not solved here.
- MCP servers silently disable `--json`/`--output-schema` under Codex
  (`04-research/codex-cli/findings.md` §2) — an orchestrator turn must not
  configure MCP servers, same constraint the worker adapter already carries.

## Sources (external, cited)

- [App Server — Codex | OpenAI Developers](https://developers.openai.com/codex/app-server)
- [codex/codex-rs/app-server/README.md · openai/codex](https://github.com/openai/codex/blob/main/codex-rs/app-server/README.md)
- [Model Context Protocol | ChatGPT Learn](https://developers.openai.com/codex/mcp) — app-server vs MCP distinction
- [Remote connections | ChatGPT Learn](https://developers.openai.com/codex/remote-connections)
- Internal: `.kerf/works/codex-harness/04-research/codex-cli/findings.md`,
  `.kerf/works/codex-harness/04-research/seam-design/findings.md`,
  `.kerf/works/codex-harness/04-research/current-harness/findings.md`,
  `internal/daemon/crewlaunchspec.go`, `internal/daemon/codexharness.go`
