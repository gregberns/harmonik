# C4 — Daemon integration & harness-routing seam (findings)

> codename:codex-app-server · Pass 3 research · component C4
> Question: where a resident `codex app-server` JSON-RPC client lives relative to the daemon; how
> `crew-start` routes to a Codex-app-server orchestrator (the parked `hk-l63b9` seam); what new
> failure modes the daemon must own and how they map to existing supervisor machinery.
> Design research only — no implementation proposed. Uncertainty flagged as OPEN QUESTION.

## TL;DR (verdict)

There are **two structurally different process models** already in the harmonik daemon, and the
Codex-app-server orchestrator fits *neither* cleanly:

1. **Worker-harness path** (`Harness` interface + `routedLaunchSpecBuilder`): a *short-lived,
   fire-and-forget* subprocess spawned per turn into a tmux pane, self-terminating on turn
   completion. This is where `codexharness.go` lives.
2. **Crew-orchestrator path** (`buildCrewLaunchSpec` -> `HandleCrewStart`): a *long-lived
   interactive* `claude --remote-control` session in its own tmux session, seeded by tmux paste,
   watched by a keeper. **Hardcoded to Claude** — this is what `hk-l63b9` un-parks.

A resident `codex app-server` is a **long-lived, stateful, bidirectional JSON-RPC client** — the
first of its kind in the daemon. Neither existing path models it: the worker path is
spawn-per-turn and stdout-JSONL-only (no live re-drive channel); the crew path assumes a tmux pane
you *paste* into, not a socket you *write JSON-RPC frames* to. The minimal integration is a **new
daemon-side subsystem that owns a supervised app-server child process + a persistent JSON-RPC
client**, dispatched from the crew-start seam when the resolved harness is codex.

---

## 1. Where the JSON-RPC app-server CLIENT lives: in-process subsystem vs. supervised sidecar

### How the daemon launches/supervises harness processes today (two models)

**Worker harness (per-turn subprocess).** `routedLaunchSpecBuilder`
(`internal/daemon/harnessregistry.go:134`) resolves a harness via `resolveHarness`
(`harnessresolve.go:53`), looks it up in the `HarnessRegistry` (`ForAgent`,
`harnessregistry.go:145`), and returns a `handler.LaunchSpec`. For codex, `CodexHarness.LaunchSpec`
(`codexharness.go:90`) -> `buildCodexLaunchSpec` (`codexlaunchspec.go:127`) emits
`codex exec --json --sandbox workspace-write --model <m> -C <wt> <seed>`. The process is spawned
into a tmux pane, streams JSONL to stdout, and **self-terminates** on turn completion
(`Completion() == CompletionProcessExit`, `codexharness.go:185`; contract at
`handlercontract/harness.go:29`). Supervision is minimal-by-design: the shared loop just does
`sess.Wait` + an absolute `commitHardCeiling` (~90m). There is **no reconnect, no liveness probe,
no restart** for a worker — a dead worker is simply a failed run.

**Crew orchestrator (long-lived tmux session).** `HandleCrewStart` (`crewstart.go`, dispatch block
at `crewstart.go:240-360`) builds a spec via `buildCrewLaunchSpec` (`crewlaunchspec.go:92`) and
spawns it through the **independent-session path** `SpawnCrewSession` (`crewstart.go:288`). Key
property (`crewstart.go:283-296`): the crew lives in its **own tmux session** with two windows —
`agent` (the claude session) and `keeper` — precisely so *daemon SIGTERM / supervisor-revive does
not kill running crew windows*. The daemon does **not** hold the crew process handle for its
lifetime; after spawn + paste-seed it records a window handle in the crew registry
(`crewstart.go:350`) and walks away. Liveness is delegated to the **keeper** (context watcher) and,
externally, to `harmonik supervise reap`. So today "supervision of a crew" = a detached tmux
window + a sidecar keeper watcher, **not** a daemon-parented child.

### The daemon's OWN supervision machinery (the closest reusable pattern)

The daemon itself is supervised by `DaemonWatchdog` (`internal/supervise/daemon_watchdog.go:19`),
which is the richest supervision primitive in the tree and the natural template:
- Probes a **Unix socket** for liveness on a `CheckInterval` (default 30s, `:29/:76`).
- Respawns a **detached (setsid)** child from `Command` argv when the probe fails (`:23-26`).
- `MaxRevives` cap with reset-on-confirmed-alive (`:33-37`), `ReviveBackoff` poll (`:38-40`),
  `ReviveWindow` bound (`:41-45`, default 15m).
- **Health-window / last-good pin**: after a successful revival it starts a health window equal to
  `CheckInterval`, and on the next alive tick past the window pins the binary as last-good
  (`:117-120`, `internal/release/lastgood.go`). Yanked-binary fallback via `LedgerPath`/`LastGoodPath`.

This is exactly the shape a resident app-server needs (probe -> respawn -> backoff -> health-window),
but it is currently daemon-scoped and **socket-liveness-based**, not JSON-RPC-session-aware.

### Weighing the two placements

| | **In-process daemon subsystem** (client goroutine inside the daemon) | **Supervised sidecar** (separate process, e.g. its own tmux window like the crew path) |
|---|---|---|
| App-server child lifecycle | Daemon parents `codex app-server` directly; dies with daemon | App-server + client run detached; survive daemon SIGTERM/revive (matches today's crew invariant `crewstart.go:283`) |
| Reconnect/backpressure | Owned in-daemon; direct access to eventbus/comms/queue seams | Must re-expose daemon seams over the socket to the sidecar |
| Failure blast radius | An app-server hang can wedge a daemon goroutine (needs isolation) | Isolated; a crashed sidecar is one dead orchestrator, like a dead crew today |
| Reuses `DaemonWatchdog` | Would need an *intra-process* variant (no such thing today) | **Directly reuses** the probe->respawn->health-window pattern |
| Keeper analogy | n/a | The keeper is already a per-crew *sidecar watcher*; an app-server supervisor is its structural sibling |

**Assessment.** The existing invariants point toward a **supervised sidecar**, not an in-process
client. The crew path already went out of its way (`crewstart.go:283-296`) to make orchestrators
*outlive* daemon restarts; putting the app-server client inside the daemon would regress that — a
daemon redeploy (a routine, frequent event per the redeploy runbook) would kill every Codex
orchestrator, whereas Claude crews survive it. A sidecar also lets the proven `DaemonWatchdog`
probe->respawn->health-window machinery be reused almost verbatim (socket-probe -> JSON-RPC-ping).

- **OPEN QUESTION (transport ownership):** a sidecar still needs the daemon's comms + queue seams.
  Does the sidecar reach them (a) by shelling `harmonik comms`/`harmonik queue` CLIs (same surface
  crews use today — substrate-neutral, cited as the C2 seam), or (b) by holding its own daemon
  socket JSON-RPC connection? Option (a) reuses the exact integration seam the problem-space fixes
  as invariant and is strongly preferred; confirm the CLI surface covers every orchestrator verb.
- **OPEN QUESTION (who runs the client loop):** if the orchestrator "brain" is Codex (server-side)
  and the client is dumb frame-plumbing, the client can be a thin Go process. But *something* must
  translate comms/queue events -> app-server turns and app-server tool-calls -> comms/queue actions.
  Where that translation lives (thin Go shim vs. Codex-driven via tool definitions) is a C1xC4
  join and unresolved here.

---

## 2. The harness-routing seam (hk-l63b9): where crew-start would branch

### What "a harness" abstracts today — and what it does NOT

The `Harness` interface (`handlercontract/harness.go:202-256`) abstracts a **worker turn**:
`LaunchSpec` / `Seed` / `Retask` / `Teardown` / `DetectReady` / `SessionIDPolicy` / `Completion` /
`NewSessionIDInterceptor`. Every method is framed around a *single spawn that runs to completion*.
There is **no method for a resident session** — no "connect", "send-turn-and-stream",
"stay-alive", "reconnect". `Retask` is explicitly a no-op for codex (`codexharness.go:142`)
"there is no live REPL to drive." So the current harness abstraction **cannot express a resident
orchestrator**; it is a worker-turn contract.

Critically, the crew path **does not use the `Harness` interface at all**. `buildCrewLaunchSpec`
(`crewlaunchspec.go:92`) is a standalone argv builder hardcoded to `claude` (`:100-102`,
`:110-115`), and `HandleCrewStart` calls it directly (`crewstart.go:251`). The four-tier
`resolveHarness` walk and the `HarnessRegistry` are **worker-path-only**; nothing in the crew path
consults harness selection today.

### Where the branch goes (per hk-l63b9 + hk-lrf30)

`hk-l63b9` is precisely scoped to close this gap. Its target files (from the bead):
`cmd/harmonik/crew.go` (`resolveCrewStartArgs:133`, payload:308), `internal/daemon/crewstart.go`
(`CrewStartRequest:73`, build:245), `crewlaunchspec.go`. Concretely:

1. **Add a harness field to `CrewStartRequest`** (`crewstart.go:73`) — a `Harness string` alongside
   `Name/Queue/MissionPath/Type`, populated from a `--harness` flag or a `harness:<type>`
   mission front-matter field (`cmd/harmonik/crew.go:resolveCrewStartArgs`).
2. **Resolve** it in `HandleCrewStart` before the build step (`crewstart.go:~245`). NOTE: the
   worker-path `resolveHarness` (`harnessresolve.go:53`) reads a `core.BeadRecord`'s labels — a
   crew has **no bead**, so crew-start needs its *own* thin resolver (flag -> mission front-matter ->
   per-crew config -> global default), not a reuse of `resolveHarness` verbatim. The precedence
   *shape* carries over; the *input source* (mission/flag, not bead labels) does not.
3. **Branch the spec builder** (`crewstart.go:251`): `if harness == codex ->
   buildCrewCodexLaunchSpec(...)` (the new builder `hk-lrf30` adds in
   `internal/daemon/crewcodexlaunchspec.go`) `else -> buildCrewLaunchSpec(...)`.
4. **Branch the spawn/seed** (`crewstart.go:265-336`): the Claude path uses `SpawnCrewSession` +
   tmux paste-seed. The Codex-app-server path needs a *different* spawn (start/attach the
   supervised app-server sidecar) and a *different* seed (JSON-RPC "create session + first turn",
   not `pasteCrewMissionToSession` at `crewstart.go:311`). `hk-8efdl` owns this boot-seed path.

### What abstraction exists vs. what must be added

- **Exists & reusable:** the `HarnessRegistry`/`AgentType` key space (`core.AgentTypeCodex`), the
  four-tier *precedence pattern*, the credential-strip + billing-guard + `buildCodexEnv` helpers
  (`codexlaunchspec.go:242`, reused by `hk-lrf30` per its bead).
- **Must be added:** a **resident-orchestrator abstraction** distinct from the worker `Harness`.
  The worker `Harness` interface should probably NOT be extended to carry resident semantics
  (`Retask`/`Completion`/`SessionIDPolicy` are worker-turn concepts); a parallel
  `CrewSubstrate`/`Orchestrator` seam (spawn-resident / send-turn / reconnect / teardown) is the
  cleaner fit. **OPEN QUESTION:** one unified interface with a resident flag, or two parallel
  interfaces? C2's substrate-neutrality analysis should decide this.

---

## 3. How existing `codexharness.go` patterns do / don't carry over

**Carries over (env & auth plumbing):**
- `buildCodexEnv` (`codexlaunchspec.go:242`): credential strip (`OPENAI_API_KEY`/`CODEX_API_KEY`
  -> empty overrides, `:264-267`), `CODEX_HOME` resolution (`:269-270`, shared `resolveCodexHome`
  `:223`), rc-prompt suppression (`:279-282`). An app-server child needs the *identical* env
  treatment — `hk-lrf30` explicitly reuses this.
- The **billing guard** (`runCodexBillingGuard`, `codexlaunchspec.go:200-205`): materializes
  `forced_login_method=chatgpt` into `$CODEX_HOME/config.toml` and fail-closed asserts a ChatGPT
  plan before launch. A resident app-server has the *same* auth surface and must run the same guard
  at spawn (and, unlike a one-shot exec, must also handle **auth expiry mid-session** — see 4).
- The **stale-WAL guard** (`cleanCodexStaleWAL`, `codexharness.go:97`): a killed codex leaves a
  stale `state_*.sqlite-wal`. A supervised, restart-prone app-server makes this *more* relevant,
  not less — every respawn is a potential stale-WAL producer.

**Does NOT carry over (session model):**
- `codex exec` is a **one-shot batch** with the task delivered via **argv** (the seed prompt,
  `codexlaunchspec.go:166`) and completion via **process self-exit** (`codexharness.go:181-187`).
  An app-server is **turn-by-turn over a resident JSON-RPC connection** — the task is a *method
  call*, not argv; completion is a *response/stream-end*, not process exit. `Completion() ==
  CompletionProcessExit` is meaningless for a resident session.
- **thread_id capture.** Today the thread_id is scraped from the first `thread.started` **JSONL
  stdout event** (`codexThreadIDInterceptor`, `codexharness.go:196`; `SessionIDCaptured` policy
  `:177`) and reused via `codex exec resume <thread_id>` argv (`codexlaunchspec.go:168-176`). For
  an app-server the session/thread identity comes back in a **JSON-RPC response**, and "resume" is
  a *reconnect + rebind to a server-held thread*, not a fresh process with a resume argv. The
  *concept* (capture an opaque id, re-present it to continue) carries over; the *mechanism* (stdout
  scrape vs. RPC result; argv resume vs. RPC reattach) is entirely different. `hk-0ysh3` owns this.
- `Seed`/`Retask`/`Teardown` are all no-ops today because there's no live channel
  (`codexharness.go:131-158`). For a resident app-server they become **real operations** (create
  session; send a follow-up turn; close the connection) — the exact methods the worker `Harness`
  interface leaves as no-ops are the ones the orchestrator needs to actually implement.

---

## 4. New failure modes the daemon must own, mapped to existing machinery

| Failure mode | Existing machinery it maps to | Gap / what's new |
|---|---|---|
| **App-server process crash** | `DaemonWatchdog` (`daemon_watchdog.go:19`) probe->respawn->backoff->`MaxRevives`; the crew "own tmux session survives daemon" invariant (`crewstart.go:283`) | Watchdog probes a **socket bind**; an app-server needs a **JSON-RPC health ping** (liveness != port-open). On respawn, the *server-side thread* must be reattached (3), not just the process restarted. **OPEN QUESTION:** does app-server persist thread state across a process restart, or is the thread lost (-> this is the C1 keeper-question crux)? If lost, respawn != recovery. |
| **JSON-RPC reconnect** (connection drops, server alive) | *No analog.* Worker path never reconnects (spawn-per-turn); crew path re-drives by tmux paste, not reconnect. | Genuinely new: a persistent client needs connect/backoff/rebind logic. Closest existing code is the daemon's *own* socket JSON-RPC server (`internal/lifecycle/` — `prereject_pl003b.go`, `readytransition_pl009_test.go`), but that's server-side and request/response, not a resilient long-lived *client*. |
| **Backpressure** (Codex slower than event arrival; comms/queue events pile up) | Queue `submit`/`subscribe` already buffers work; `subscribe_capacity_exceeded` is a known daemon signal (poll via `comms recv` workaround) | New at the *orchestrator turn* level: only one Codex turn can be in flight; wake/event coalescing must happen client-side. No existing turn-level flow-control. |
| **Auth expiry mid-session** | Billing guard runs **once at launch** (`codexlaunchspec.go:200`); `CODEX_HOME` set for token refresh (`codexlaunchspec.go:18-19`) | A one-shot exec never outlives its token; a resident session **can**. Need mid-session re-auth detection + a guard re-run, with no launch boundary to hang it on. Entirely new. |
| **App-server hang (alive but not answering)** | Worker path: `commitHardCeiling` (~90m absolute kill). Keeper watches Claude crew *context*, not liveness. | Need a per-turn deadline + a "hung session" kill/respawn distinct from "process dead." The 90m ceiling is a worker-turn concept; a resident orchestrator needs a *turn* timeout, not a *run* ceiling. |
| **Supervisor revival correlation** | `detectAndEmitSupervisorRevival` (`supervisorrevival_hkrnkuy.go:34`) already emits `supervisor_revival{unexpected_exit}` by scanning for a missing `daemon_shutdown` | Pattern is directly copyable for an app-server: emit an equivalent revival event on unclean app-server exit for postmortem. Low-risk reuse. |

**Health-window / last-good analog.** `DaemonWatchdog`'s post-revival health window + last-good pin
(`daemon_watchdog.go:117-120`, `release/lastgood.go`) is *binary-version* oriented. It does **not**
obviously map to an app-server orchestrator (there's no "last-good Codex binary" the daemon
manages; codex is an external tool). The reusable half is the **probe cadence + revival-cap +
backoff**; the last-good/yanked-binary half is daemon-redeploy-specific and likely **not**
applicable. **OPEN QUESTION:** is there a "last-good *thread state*" notion worth persisting so a
crashed orchestrator resumes from a known checkpoint rather than cold?

---

## 5. Minimal integration surface (smallest set of new daemon-side pieces)

Sketch only — no implementation. Smallest coherent set:

1. **Crew-start harness resolution + branch (`hk-l63b9`).** Add `Harness` to `CrewStartRequest`
   (`crewstart.go:73`) + a thin crew-scoped resolver (flag -> mission front-matter -> per-crew config
   -> global default; *not* the bead-label `resolveHarness`) + a `if codex` branch at
   `crewstart.go:251`. This is the un-parking seam and the load-bearing change.

2. **A resident-orchestrator launch-spec builder (`hk-lrf30`).** New
   `internal/daemon/crewcodexlaunchspec.go`: argv/env for `codex app-server`, reusing
   `buildCodexEnv` + credential-strip + billing-guard from `codexlaunchspec.go`. Sibling of
   `buildCrewLaunchSpec`, *not* a modification of it.

3. **A supervised app-server sidecar + JSON-RPC client subsystem** (new, no bead cited — the
   substrate itself). Owns: spawn the app-server as a **detached child in its own tmux session**
   (preserving the `crewstart.go:283` survive-daemon-restart invariant); a persistent JSON-RPC
   client (connect / create-session / send-turn / stream / reconnect); a `DaemonWatchdog`-shaped
   liveness supervisor adapted to JSON-RPC ping + turn-deadline. **This is the genuinely new
   subsystem** and the bulk of the cost the problem-space says must be justified.

4. **Boot-seed / commission path (`hk-8efdl`).** Replace tmux paste-seed
   (`pasteCrewMissionToSession`, `crewstart.go:311`) with a JSON-RPC "create session + deliver
   mission as first turn" for the codex branch.

5. **Session-id / resume as reconnect-rebind (`hk-0ysh3`).** Capture the app-server thread id from
   the RPC result (not stdout scrape) and rebind on reconnect (not `exec resume` argv).

6. **Keeper compatibility decision (`hk-6z72r`).** *Possibly a deletion, not an addition.* If C1/C3
   confirm app-server manages context server-side, the per-crew keeper window
   (`crewstart.go:288` two-window session) is **not spawned** for a codex crew — the central bet of
   this kerf work. C4's job is only to note that the keeper is a *sidecar window the crew-start path
   spawns*, so "retire keeper for codex crews" is a clean branch at spawn time, not a deep rewrite.

**Reuse ledger (existing pieces that do NOT need reinventing):** `buildCodexEnv` / credential-strip
/ billing-guard / stale-WAL guard (`codexlaunchspec.go`, `codexharness.go`); the
`DaemonWatchdog` probe->respawn->backoff->health-window skeleton (`daemon_watchdog.go`); the
`supervisor_revival` emit pattern (`supervisorrevival_hkrnkuy.go`); the comms + queue CLI seam
(the substrate-neutral integration point the problem-space fixes as invariant); the crew registry +
independent-tmux-session survive-daemon-restart invariant (`crewstart.go:283-296`).

**Net-new (the real cost):** the persistent JSON-RPC *client* transport with reconnect/backpressure
— nothing in the worker path or crew path is a client of a long-lived external RPC server; the only
JSON-RPC code today is the daemon's own *server* side (`internal/lifecycle/`), which is request/
response, not a resilient long-lived client.

---

## Key file/line citations
- `internal/daemon/crewlaunchspec.go:92` — `buildCrewLaunchSpec`, hardcoded `claude` (`:100-115`).
- `internal/daemon/crewstart.go:73` (`CrewStartRequest`), `:240-360` (build+spawn+seed dispatch),
  `:283-296` (independent-tmux-session survive-daemon-restart invariant), `:311` (paste-seed).
- `internal/daemon/codexharness.go:90` (`LaunchSpec`), `:131-158` (Seed/Retask/Teardown no-ops),
  `:177` (`SessionIDCaptured`), `:185` (`CompletionProcessExit`), `:196` (thread_id interceptor).
- `internal/daemon/codexlaunchspec.go:127` (`buildCodexLaunchSpec`), `:200` (billing guard),
  `:242` (`buildCodexEnv` credential-strip/CODEX_HOME).
- `internal/daemon/harnessresolve.go:53` (`resolveHarness` four-tier, **bead-label** input).
- `internal/daemon/harnessregistry.go:47` (`newHarnessRegistry`), `:134`
  (`routedLaunchSpecBuilder`), `:200` (`buildCodexRoutedLaunchSpec`).
- `internal/handlercontract/harness.go:202-256` (`Harness` interface — worker-turn-shaped).
- `internal/supervise/daemon_watchdog.go:19` (`DaemonWatchdogSpec`: probe/respawn/backoff/
  health-window/last-good).
- `internal/daemon/supervisorrevival_hkrnkuy.go:34` (revival-event emit pattern).
- `internal/release/lastgood.go` (last-good pin — binary-version-scoped, likely N/A to app-server).
- `internal/lifecycle/prereject_pl003b.go`, `readytransition_pl009_test.go` — the daemon's OWN
  JSON-RPC *server* side (the only JSON-RPC in-tree; server, not resilient client).

## Beads referenced
- `hk-l63b9` (route crew-start through harness selection — the seam) · `hk-lrf30` (Codex crew
  launch-spec builder) · `hk-8efdl` (boot-seed/commission for non-TUI crew) · `hk-6z72r` (keeper
  compat) · `hk-0ysh3` (harness-aware crew session-id/resume) · `hk-fijwi` (spike, CLOSED;
  superseded — Option B killed) · parent epic `hk-q3ovr`.
