# Idea 5 — Hermes harness study + the "our own harness" question

**Research note + design prompt.** Date 2026-06-30. Studies Nous Research's **Hermes** agent
harness as prior art for how we run crew, and frames the meta-question the operator raised: build
our own programmatic harness (that harmonik could then sandbox), or support existing programmatic
ones.

> Source: `hermes-agent.nousresearch.com/docs` + `github.com/NousResearch/hermes-agent`.
> **MIT-licensed, Python, synchronous.** Full raw report retained below the summary.

---

## What Hermes actually is (one paragraph)

A **single-machine, per-user Python agent** with three parts: a **gateway process** (long-lived
host — but primarily a *chat I/O front door* to ~18 messaging platforms, that also embeds the
scheduler), a **profile** model where every agent identity is a **forked home directory**
(`HERMES_HOME=~/.hermes/profiles/<name>/` with its own SQLite state, memory, skills, keys, persona),
and a **SQLite kanban board** whose dispatcher loop atomically claims cards and **spawns the assigned
profile as a fresh OS process in a clean workspace**. It is much closer to *"one powerful local agent
you can fork N times"* than to our Go-daemon + multi-machine fleet. Crucially it has **two distinct
orchestration primitives**: `delegate_task` (in-process, blocking, depth-capped subagent fan-out)
and **Kanban** (durable, cross-process, crash-resilient work queue) — and the docs explicitly keep
them separate.

---

## The mapping to how *we* run crew

| Hermes | Nearest harmonik equivalent | The difference that matters |
|---|---|---|
| **Gateway process** | The Go daemon | Hermes' gateway is **messaging-first** and *embeds* the scheduler; our daemon is **scheduler-first** with a dedicated comms bus. Hermes has no comms-bus-as-first-class — coordination is the board + chat platforms. |
| **Profile (forked home dir)** | A crew identity (mission + named queue) | **This is the sharpest contrast.** A Hermes profile is a *fully forked identity* — its own SQLite state, memory, skills, **keys**, persona — isolated by an env var (`HERMES_HOME`). Our crews **share** the repo/beads DB and differ by mission + queue. Neither sandboxes the filesystem. |
| **Kanban card** | A **bead** | Very close: durable rows, parent→child deps (≈ blocked-by), append-only `task_events` (≈ our JSONL), per-attempt `task_runs`. **But:** Hermes bakes `assignee=profile` onto the card and the card *is* the dispatch trigger, and **the worker self-completes** (`kanban_complete`) — we deliberately keep **terminal transitions daemon-owned**. |
| **Dispatcher loop** | The daemon's queue/claim loop | Same atomic-claim (`BEGIN IMMEDIATE`) + crash-reclaim (dead-PID / TTL) ideas we have. Hermes runs it embedded on a 60s tick, single-host only. |
| **Worker in clean workspace** | Our `isolation:worktree` run | Nearly identical (git worktree per task). Hermes adds **workspace *kinds* as a first-class per-card property** — `scratch` (ephemeral, auto-deleted) / `worktree` / `dir:<abs-path>` — with confused-deputy path defenses. We only have worktree. |
| **`delegate_task` subagent** | Our `Agent`-tool fan-out | Hermes formalizes it: `leaf` vs `orchestrator` roles, `max_spawn_depth` cap, per-child toolset + **model/provider override**, typed result objects the orchestrator verifies. Synchronous + blocking (ThreadPoolExecutor, 3 concurrent). |

## Five things Hermes does that we probably don't — and should consider

1. **Profile = fully-forked identity (own memory/skills/keys)** as the unit of specialization. We
   specialize crews by *mission + queue* over a shared store. Hermes' model gives true credential +
   memory isolation between agent identities for near-free — directly relevant to **idea 2
   (sandbox)** and **idea 3 (Pi crew with its own OpenRouter key)**. The `credential-isolation.md`
   spec already wants this; Hermes shows a concrete filesystem-fork implementation.
2. **Two explicitly-separated orchestration primitives.** `delegate_task` (blocking, in-process,
   context-economy fan-out) vs. **Kanban** (durable, crash-resilient, cross-process). We conflate
   "spawn sub-agents" (Agent tool) and "queue work" (daemon) less cleanly. Worth naming the split as
   sharply as they do.
3. **Workspace *kinds* as a per-card property** (`scratch` / `worktree` / `dir:`) with path-safety
   defenses. Our worktree isolation is one fixed mode; a per-bead workspace-kind knob is a clean
   generalization — and `scratch` (ephemeral auto-deleted) maps onto **idea 2's** container story.
4. **Typed `block` reasons** (dependency / needs_input / capability / transient) that route a card
   back through states. Our stranded-`in_progress` / no-commit pain is exactly what a typed block
   vocabulary addresses — a blocked bead could say *why* structurally instead of dying opaquely.
5. **Per-subagent model/provider override as a routine config knob**, with a fallback chain. This is
   **idea 3 (Pi crew)** already solved at the config layer — mix Claude planners + cheap workers +
   local models per-agent. Strong prior art for our provider abstraction.

## What *we* do that Hermes doesn't

A real **multi-machine story** (our remote-worker substrate — Hermes is single-host, its
Modal/Daytona/SSH backends only push *tool execution* off-box, not orchestration); a dedicated
**comms bus** for peer agent messaging (Hermes coordinates via the board + chat platforms);
**daemon-owned terminal transitions** (Hermes lets workers self-complete); and **persistent
long-lived orchestrator sessions** (Hermes agents are per-message/per-card *ephemeral*).

---

## The meta-question: our own harness, or support programmatic ones?

The operator's framing: *"Now that we're branching into other harnesses, it might be interesting to
investigate either our own harness (which harmonik could then run in a sandbox like Daytona) — or
see if there are programmatic ones that make sense to support."*

Hermes sharpens this into a concrete fork:

- **Option A — support Hermes (or a Hermes-like) as a driven harness.** It's **MIT + Python** with an
  **ACP (Agent Client Protocol) stdio/JSON-RPC** entry point and a batch/API-server entry. So
  harmonik *could* drive Hermes **process-level / over ACP** the same way it drives `claude` /
  `codex` / (planned) `pi` — feeding it a bead, reading back a result. It is **not** packaged as a
  clean embeddable library, so integration is realistically subprocess/ACP, which **fits our
  daemon-spawns-processes model well.** This is a natural extension of the idea-3 harness-abstraction
  work (codex is the template; Pi is next; Hermes/ACP could be a third).
- **Option B — build our own programmatic (headless, no-TTY) harness.** Today our harnesses are
  interactive TTY sessions (`claude --remote-control`, tmux-hosted). A *programmatic* harness — one
  that takes a bead + context on stdin and emits structured results, no pane, no bracketed-paste
  seed — would be **far easier to sandbox** (idea 2: drop it in a Daytona/Singularity/container with
  no tmux) and to run on a **remote node** (idea 1: no interactive session to attach). The current
  interactive model is the single biggest source of the launch fragility in our memory notes
  (pane-wake, unsubmitted-prompt wedge, `/clear` session-id flips). A programmatic harness sidesteps
  all of it.

**The synthesis these two suggest:** the real prize isn't Hermes specifically — it's making
**"harness" a clean pluggable seam that accepts a bead + context and returns a structured result**,
with the transport (interactive-tmux vs. programmatic-stdio/ACP) as an implementation detail behind
it. Once that seam exists: Claude-interactive, codex, Pi, Hermes-over-ACP, and a future
harmonik-native programmatic harness all plug into the *same* dispatch + sandbox + node machinery.
That seam is what unlocks ideas 1/2/3 at once. **Recommendation:** treat "define the pluggable
harness contract (bead+context in, structured result out; transport-agnostic)" as a first-class
design item — and evaluate Hermes-over-ACP as the *first non-Claude/codex proof* of it, since it's
MIT and already speaks a structured protocol.

---

## Open questions (for the full sketch)

- Does the harness seam already exist implicitly (codex path, `AgentType`) or does it need a real
  contract doc? What's the minimal "bead + context → structured result" interface?
- Programmatic vs. interactive: is our tmux-interactive model *load-bearing* (inspectability is a
  locked design preference — Gas-Town tmux) or an artifact we could make optional per-harness?
- Would we adopt any Hermes concepts directly — **workspace-kinds**, **typed block reasons**,
  **profile-as-forked-identity** for credential isolation — independent of adopting Hermes itself?
- ACP: is it worth a spike to drive one Hermes agent over ACP from the daemon as a proof of the
  pluggable-harness seam?

**Status: research complete; design sketch pending operator direction on the A-vs-B-vs-synthesis
fork above.**

---

## Appendix — raw research findings

<details><summary>Full Hermes technical report (gateway, profiles, kanban, delegation, backends,
deployment, embeddability)</summary>

**Sources:** primary docs at `hermes-agent.nousresearch.com/docs` + MIT repo
`github.com/NousResearch/hermes-agent`. Python, synchronous, MIT.

**1. Gateway process** — long-running Python process (`gateway/run.py`, `GatewayRunner`) connecting
~20 messaging platforms via a unified adapter layer. Owns inbound/outbound messaging, session
persistence, authorization, slash-commands, hook lifecycle, cron, background maintenance. PID at
`~/.hermes/gateway.pid`, **profile-scoped**. Pipeline: adapter → `MessageEvent` →
`_handle_message()` → authorize → session key → **spawn fresh `AIAgent` with prior history** →
`run_conversation()` → deliver. Embeds the kanban dispatcher by default
(`kanban.dispatch_in_gateway: true`). Identity = chat I/O front door first, scheduler second.
(gateway-internals page omits the dispatcher coupling — doc seam, not a contradiction.)

**2. Named agents / what an agent runs as** — a "profile" is a **separate Hermes home directory**
(`HERMES_HOME=~/.hermes/profiles/<name>/`; default = `~/.hermes`). Wrapper alias at
`~/.local/bin/<name>` sets the env var before launch; ~119 files resolve paths off `HERMES_HOME`. An
agent runs as a **synchronous `AIAgent` Python object inside a host OS process** — not a persistent
per-agent daemon, thread, or container. Flavors: CLI (one interactive agent/session); Gateway (fresh
`AIAgent` per inbound message + session history); Cron/batch (fresh, no history); ACP (stdio
JSON-RPC). Docker/SSH/Modal/Daytona/Singularity/Vercel exist only as **tool-execution (terminal)
backends**, not agent hosts.

**3. Profile contents / isolation** — `config.yaml` (model/provider/toolsets), `.env` (keys + bot
tokens), `SOUL.md` (persona), SQLite state DB, memories (Honcho per profile), sessions/history,
bundled+installed skills, cron jobs, gateway PIDs, checkpoints, logs, backups. **State fully
isolated via `HERMES_HOME`; filesystem NOT sandboxed** ("same filesystem access as your user
account"). Subprocesses keep real `HOME` by default so git/ssh/npm find creds; opt into isolation via
`terminal.home_mode: profile`. Gateway token collision across profiles detected + second blocked.
Profiles are **directories**, exportable to `.tar.gz`. CLI: `hermes profile
create/list/show/rename/export/import/delete`, `--clone/--clone-all/--clone-from`, `hermes -p <name>`.

**4. Which agent to spin up / templates** — **no automatic solver.** Human or orchestrator sets a
card's `assignee = profile name`; dispatcher spawns that profile. "Agent template" ≈ **the profile**;
`--clone-from coder` is the templating mechanism. Selection is **declarative on the card**.

**5. Kanban** — durable SQLite at `~/.hermes/kanban.db` (WAL mode). Tables: `tasks`, `task_links`
(deps), `task_comments`, `task_runs` (per attempt), `task_events` (append-only). States: triage →
todo → ready → running → blocked → done. Dispatcher (embedded in gateway, 60s tick): reclaim stale/
crashed → promote todo→ready when deps done → atomically claim one ready via `BEGIN IMMEDIATE` →
spawn assigned profile as separate OS process in clean workspace. **Workspace kinds:** `scratch`
(ephemeral, deleted on completion), `dir:<abs-path>` (persistent; relative paths rejected),
`worktree` (git worktree, preserved). **Crash resilience:** dead-PID detection + stale-TTL reclaim
(default 4h, needs `kanban_heartbeat()`); gateway stop → ready tasks stay queued. Worker gets
`HERMES_KANBAN_TASK` + `HERMES_KANBAN_BOARD` env → activates focused `kanban_*` toolset (show/
complete/block/heartbeat/comment; create/link/unblock for orchestrators). `complete` carries
structured handoff; `block` is typed (dependency/needs_input/capability/transient).

**6. `delegate_task`** — separate primitive from kanban. Spawns child `AIAgent` in-process, isolated
context + restricted toolset + own terminal; children blind to parent conversation; only summaries
return. Roles: `leaf` (default, can't delegate) vs `orchestrator`. Depth `delegation.max_spawn_depth`
(default 1). Synchronous, **blocks parent** (ThreadPoolExecutor, `max_concurrent_children` default
3); interrupt cascades. Per-task `toolsets`, `max_iterations` (50), model/provider override. Docs
warn: **treat subagent summaries as claims until verified.**

**7. Backends** — OpenRouter, Nous Portal, OpenAI/Codex, Anthropic native, Google/Gemini, DeepSeek,
Ollama, LM Studio, Bedrock, Azure, NVIDIA NIM, xAI, custom (vLLM-style). Cascade: runtime → config →
env → defaults. Different profiles/subagents use different backends concurrently; credential scoping
prevents key leakage; `fallback_model`/`fallback_providers` for failover.

**8. Deployment** — **single machine, per-user.** All state under `~/.hermes`. No native
multi-machine fleet / cluster / cross-host board. "Distributed" = multi-profile + multi-worker
processes on one host sharing one SQLite board. Sandboxing opt-in at tool layer only; agent + fs not
sandboxed by default.

**9. Open source / embeddable** — MIT, Python, `github.com/NousResearch/hermes-agent`. `AIAgent` is
the reusable core; **ACP stdio/JSON-RPC** + batch/API-server entries exist. Not a clean embeddable
library with stable programmatic API — natural integration is **process/ACP-level** (spawn CLI/ACP,
feed the kanban SQLite board), which fits a daemon-spawns-processes model.

Sources: docs home, /profiles, /architecture, /gateway-internals, /kanban, /delegation,
/provider-runtime, GitHub repo.

</details>
