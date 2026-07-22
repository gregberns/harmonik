# 04 — Design: Codex app-server crew-orchestrator session model

> codename:codex-app-server · Pass 4 change-design · parent epic hk-q3ovr
> Synthesizes C1 (protocol), C2 (orchestrator loop), C3 (keeper), C4 (integration), C5 (commissioning).
> Design only — no implementation this phase. This is the artifact for the design-review gate.

## Decision in one line

Run a harmonik crew orchestrator as a **supervised sidecar that drives a resident `codex
app-server` thread over a persistent JSON-RPC client**, with orchestrator conversational state held
**server-side** — which lets a Codex crew **retire the per-session keeper/handoff machinery** that
Claude crews require. The orchestrator loop itself is unchanged and substrate-neutral; only the
substrate under it changes.

## Why this is feasible (evidence synthesis)

1. **Server-side context (C1, CONFIRMED).** app-server is a long-lived JSON-RPC 2.0 process modeling
   work as Thread→Turn→Item. History persists server-side (rollout JSONL + SQLite), is reconstructed
   on `thread/resume`, token-accounted via `thread/tokenUsage/updated`, and compacted via
   `thread/compact/start`. The client holds only a socket + captured `thread_id` + a last-seen
   turn/item cursor — **no growing context buffer.**
2. **The orchestrator loop is already substrate-neutral (C2).** Boot → comms-join/presence →
   assignee-mirror → `recv --follow` inbox → named-queue submit/`subscribe` → event-wake react →
   dual-surface progress feed → drain/park is **all daemon-socket CLI** (`comms`/`queue`/`subscribe`/
   `br`). Only **four** things bind to Claude's `--remote-control` live-paste, and all four exist
   only to drive an interactive REPL that has no event loop of its own: the tmux boot-seed paste,
   the `--wake` mid-session nudge, the splash-dismiss Enter, and the keeper `/clear`+resume cycle.
   `--remote-control` itself is cosmetic (a picker label, not identity).
3. **A real event-loop process replaces the whole paste/wake apparatus (C2).** The one capability a
   resident substrate must add — an actual event loop that reacts to delivered messages without a
   keystroke — is exactly what dissolves the four Claude-specific items rather than reimplementing
   them.

## Target session model

**One resident `codex app-server` process, fronted by a per-crew supervised sidecar, one thread per
crew orchestrator.** (app-server hosts many concurrent *loaded threads* — C1 §6 — so a single
app-server could in principle front the whole crew fleet. **Caveat:** whether concurrent turns
execute in true parallel or serialize per model/quota is unconfirmed — C1 §6, OQ-3 — so the
"front the whole fleet" throughput claim is provisional; a shared-server-vs-per-crew-server choice
is left to the implementation kerf, flagged below.)

Lifecycle:
- **Commission.** Captain commissions exactly as today (substrate-neutral, C5): write the C3 mission
  handoff file, spawn via `harmonik crew start` (now harness-aware), mail the epic over comms,
  subscribe `epic_completed`. The daemon's crew-start branch (see integration) starts/attaches the
  app-server sidecar and delivers the mission as the **first `turn/start`** on a new thread instead
  of a tmux paste-seed.
- **Identity.** `thread_id` is **server-minted, capture-only** (C1 §3, matches the CLI NOT_PLANNED
  finding) — captured from the `thread/start` response and persisted in the crew registry (new
  field, C5 — the registry has none today).
- **Drive a turn / react to a wake.** The sidecar holds the comms + queue + subscribe streams
  (daemon-fronted). On a wake (comms message, queue event, timer), it folds the delta into a
  `turn/start` on the resident thread; the turn runs with normal shell access and calls
  `harmonik queue submit` / `comms send` / `br` directly — an orchestrator turn is a worker turn
  whose prompt is "here is what changed; act." `turn/steer` (C1 §2) appends to an in-flight turn for
  fast follow-ups.
- **Mid-session re-task (C5).** Captain intent unchanged (a `--topic assign` comms send); delivery
  moves from pane-`--wake` to the sidecar converting the message into a `turn/start`/`turn/steer`.
  The "idle crew is deaf without a keystroke" failure mode is a pane artifact and disappears.
- **Restart continuity COLLAPSES (C5).** No handoff→`/clear`→`/session-resume`. On sidecar/app-server
  restart: `initialize` → `thread/resume <thread_id>` (server reloads history) → replay any comms/
  queue events missed since the last-seen cursor. The daemon persists the thin state (thread_id,
  queue, epic_id, comms identity, subscription cursor); the server holds all conversational state.
- **Presence is daemon-proxied (C5, OPEN).** comms presence TTL is ~120s, refreshed today by the
  client's `recv --follow` heartbeat. With no interactive session, the **daemon/sidecar emits the
  crew's `agent_presence` beats.** This removes the fragile client-stream dependency, but it is
  **not** an unconditional win: a naive "sidecar is up → beat online" would mask a hung or
  idle-unloaded thread as healthy. **Gate the beat on liveness that reflects real orchestrator
  capacity** — thread reachable (JSON-RPC ping answered / `thread/resume`-able) AND the crew's queue
  is being serviced — not merely sidecar process-up. Exact liveness predicate is an implementation
  detail (flagged OQ-8).
- **Idle unload (C1 §5).** A thread with no subscribers unloads from memory after 30 min
  (`thread/closed`) but stays persisted and re-`resume`-able. The sidecar either keeps the
  subscription alive or treats re-resume as normal — an idle crew is a cheap reconnect, not a loss.

## Integration & the `hk-l63b9` un-park path (C4)

Placement verdict: **supervised sidecar, not an in-process daemon client.** Primary reasons, in
order: (1) **failure isolation** — an app-server hang or a wedged JSON-RPC client is one dead
orchestrator, not a wedged daemon goroutine; (2) **watchdog reuse** — a sidecar reuses the proven
`DaemonWatchdog` probe→respawn→backoff skeleton (socket-probe → JSON-RPC ping) almost verbatim,
where an in-process client would need an intra-process variant that does not exist today. The
survive-daemon-redeploy invariant (`crewstart.go:283-296`) is a **secondary** reason and a weaker
one here: because conversational state is server-side, a killed in-process client is a cheap
`thread/resume` reconnect, not a lost session — so "survives redeploy" matters less for Codex crews
than it does for Claude crews. Isolation + watchdog-reuse carry the decision.

The concrete change set (each mapped to an existing child bead — this is what un-parks them):

| Bead | Change | Files (C4 citations) |
|---|---|---|
| **hk-l63b9** (the seam) | Add `Harness` to `CrewStartRequest`; add a **crew-scoped** resolver (flag → mission front-matter → per-crew config → global default — NOT the bead-label `resolveHarness`, a crew has no bead); branch the spec builder + spawn/seed at the codex case. | `crewstart.go:73,245,251`; `cmd/harmonik/crew.go:resolveCrewStartArgs` |
| **hk-lrf30** | New `crewcodexlaunchspec.go` — argv/env for `codex app-server`, a *sibling* of `buildCrewLaunchSpec`, reusing `buildCodexEnv` + credential-strip. Billing-guard reuse is **contingent on OQ-2** (backend auth): the guard materializes `forced_login_method=chatgpt`; whether app-server authenticates the same way is unconfirmed, so treat guard-reuse as *provisional*, not a clean carry-over, and re-verify before relying on it. | `codexlaunchspec.go:200,242` |
| **hk-8efdl** | Boot-seed via JSON-RPC "create session + mission as first turn" instead of `pasteCrewMissionToSession`. | `crewstart.go:311` |
| **hk-0ysh3** | Capture `thread_id` from the RPC result (not stdout scrape) + rebind on reconnect (not `exec resume` argv); persist in crew registry (new field). | `codexharness.go:196`; crew registry |
| **hk-6z72r** | Keeper compatibility = **primarily a deletion/branch** (see keeper-verdict doc): don't spawn the keeper window for a codex crew. The only *additive* piece is a thin token-pressure compaction trigger, and only if OQ-1 resolves to manual compaction. | `crewstart.go:288` |
| **hk-nzzos** *(net-new, filed)* | The **persistent JSON-RPC client + supervised app-server sidecar** subsystem: connect / create-session / send-turn / stream / reconnect / backpressure + a `DaemonWatchdog`-shaped liveness supervisor (JSON-RPC ping + per-turn deadline). **This is the real cost** — no worker- or crew-path analog; the only in-tree JSON-RPC is the daemon's own *server* side. Filed as its own bead so the hard part is tracked when hk-l63b9 un-parks. | new subsystem |

Abstraction note (C4): the worker `Harness` interface is worker-turn-shaped (`LaunchSpec`/`Seed`/
`Retask`/`Completion`/`SessionIDPolicy`) and **cannot express a resident session** — `Retask` is a
codex no-op today "no live REPL to drive." Do **not** overload it; add a **parallel
`CrewSubstrate`/`Orchestrator` seam** (spawn-resident / send-turn / reconnect / teardown). Unified-
with-a-flag vs. two-interfaces is an implementation-phase call (flagged).

Reuse ledger (do not reinvent): `buildCodexEnv` / credential-strip / billing-guard / stale-WAL
guard; the `DaemonWatchdog` probe→respawn→backoff skeleton; the `supervisor_revival` emit pattern;
the comms+queue CLI seam (substrate-neutral integration point); the crew registry +
independent-tmux-session survive-daemon-restart invariant.

## New failure modes the daemon must own (C4 §4)

- **app-server crash** → watchdog respawn, **then `thread/resume`** to reattach the server-held
  thread (respawn alone ≠ recovery). Thread state survives process restart (C1 §5), so recovery is
  reconnect+resume.
- **JSON-RPC reconnect** (connection drops, server alive) → genuinely new; needs connect/backoff/
  rebind. No in-tree analog.
- **Backpressure** → only one turn in flight per thread; wake/event coalescing is client-side.
  app-server also signals overload with JSON-RPC `-32001`.
- **Auth expiry mid-session** → a resident session outlives its token; need mid-session re-auth
  detection + billing-guard re-run (no launch boundary to hang it on). New.
- **App-server hang (alive, not answering)** → per-**turn** deadline + hung-session kill/respawn,
  distinct from the worker `commitHardCeiling` run-ceiling.

The `DaemonWatchdog` last-good/yanked-binary half is binary-version-scoped and **N/A** (codex is an
external tool); only the probe-cadence + revive-cap + backoff half is reusable.

## Open questions carried to the design-review gate / implementation kerf

1. **Auto vs. caller-triggered compaction (C1 §4).** `thread/compact/start` proves *manual*
   compaction exists; whether app-server auto-compacts at window pressure is unconfirmed. This
   decides whether the retired keeper WARN/ACT band **fully deletes** (auto) or **reshapes** into a
   thin "call `thread/compact/start` at token pressure" trigger driven off `thread/tokenUsage/updated`
   (manual). Either way the client-side `/clear` cycle is gone.
2. **Backend auth (C1 §5).** How app-server authenticates to the model backend (ChatGPT login vs.
   API key; does it inherit `~/.codex/`?) — gates the mid-session re-auth design.
3. **Parallel turns (C1 §6).** Do concurrent threads execute turns in true parallel or serialize per
   quota? Affects shared-app-server-vs-per-crew and fleet throughput.
4. **Where the event↔turn translation "brain" lives (C4).** Thin Go shim vs. Codex-driven via tool
   definitions. C1×C4 join, unresolved.
5. **Sidecar↔daemon seam (C4).** Sidecar reaches comms/queue by shelling `harmonik` CLIs
   (substrate-neutral, preferred) vs. its own daemon-socket connection. Confirm the CLI surface
   covers every orchestrator verb.
6. **Shared app-server vs. per-crew app-server.** One process many threads (cheaper, blast-radius
   shared) vs. isolation per crew.
7. **Wire-contract re-verification (C1 §"source-confidence").** Method names were summarized from
   web fetch; re-verify against raw `codex-rs/app-server/README.md` + schema before treating as
   normative for implementation. **The method names in this design are illustrative, not a
   normative wire contract.**
8. **Presence liveness predicate.** What exactly gates the daemon-proxied `agent_presence` beat
   (thread-reachable ping + queue-serviced) so "online" reflects real orchestrator capacity, not
   just sidecar process-up.

## Pi generalization (flag only, per non-goals)

The harness-routing seam (hk-l63b9) and the resident-orchestrator abstraction are harness-agnostic;
Pi-as-orchestrator would reuse both if Pi exposes an equivalent resident/server-side-context
surface. Whether it does is out of scope here — flag for a separate assessment, do not design.
