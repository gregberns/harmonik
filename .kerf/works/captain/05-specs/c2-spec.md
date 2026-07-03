# C2 — Persistent crew-start path — Change Spec

> Component **C2** of the *Captain & Crew* kerf plan. Implementation-level change
> spec; an implementing agent follows it verbatim.
>
> Scope: the captain-invokable `harmonik crew start/stop` command + the durable
> crew registry + the keeper-attach inputs at spawn. **Out of scope** (do NOT
> spec or build): the keeper itself, remote-control's internal drive mechanism,
> the keeper token-threshold (90%-of-1M) bug, ranking/failure judgment, a Go
> captain-supervisor.
>
> All file:line refs verified against the tree on 2026-06-08 (`main` @ 2272d9f1).

---

## Risks

The make-or-break runtime unknown is whether the interactive `--remote-control`
pane (a) fires the statusLine hook that writes `.harmonik/keeper/<name>.ctx` and
(b) accepts bracketed-paste like a normal `claude` TUI.

**De-risked — flag existence is confirmed.** The flags themselves are CONFIRMED
present in the installed binary (claude v2.1.168, `claude --help`):
- `--remote-control [name]` — "Start an interactive session with Remote Control".
- `--session-id <uuid>` — caller-supplied ("Use a specific session ID").
- `--resume [value]` — resume by session id.
- `--fork-session` — fork on resume.

So flag existence/composition is no longer the unknown — only the runtime pane
behaviour (statusLine hook + bracketed-paste), gated by the mandatory §6 manual
smoke. §6 adds a **smoke step 0**: a throwaway
`claude --remote-control "preflight" --session-id <uuid>` to confirm the pane comes
up + the statusLine hook fires, BEFORE writing handler code.

---

## 1. Requirements (carried forward from 03-components.md C2)

A captain-invokable way to start a **long-lived, programmatically-drivable crew
session**, persisted durably so the captain and keeper can find and re-address it.

R-C2.1 **New verb** `harmonik crew start <name> --queue <q> --mission <handoff-path>`
that launches a long-lived **interactive** `claude --remote-control "<name>" --session-id <uuid>`
session (a local, cloud-watchable tmux pane) with a **caller-minted** `session_id`,
seeds the mission handoff via bracketed-paste, and ensures the named queue `<q>`
exists (if `queue.Load(projectDir, q)` returns nil/absent, persist a minimal empty
`Queue{Name: q, Workers: 1}` via `queue.Persist`; idempotent — if it already exists,
leave it untouched). The crew→queue binding lives ONLY in the crew registry
(`Record.Queue`), not in the queue model. This **replaces** the fresh-per-bead spawn approach
as the crew lifecycle. (This is the *interactive* remote-control form — a real pane
we own and paste into — NOT server-mode `claude remote-control`, which is an empty
human-driven server with no local pane.)

R-C2.2 **Addressing.** Crew are addressed by **comms name** (for tasking) and by
**`session_id`** (for re-task / restart via `--resume <uuid>`) — NOT by tmux pane-id.
Because C2 mints the `session_id` and resumes it with `--resume <uuid>` (which
continues the **same** id; `--fork-session` would fork — we do not), the id is
**stable** across a keeper restart. This aligns with the keeper, which already keys
identity on `session_id` (`scripts/keeper-statusline.sh` writes `{pct, session_id, ts}`).

R-C2.3 **Crew registry** (new durable state): one record per crew member at
`.harmonik/crew/<name>.json` = `{name, session_id, queue, epic, handle,
started_at}`. `name`+`queue`+`session_id` are all stable keys: C2 mints the
`session_id` at first launch and persists it immediately; restarts `--resume` the
same id. The registry is the durable record the captain/keeper re-address from.

R-C2.4 **Keeper-attach inputs at spawn** (R3): stand up the per-crew gauge
(`.harmonik/keeper/<name>.ctx`), create the `.managed` marker, and register the
crew's pane/handle — otherwise the keeper no-ops (`keeper.IsManaged`,
`internal/keeper/keeper.go:106`).

R-C2.5 **Stop path** (minimal for this slice): `harmonik crew stop <name>`
deregisters the crew, stops the session, and optionally pauses its queue.

**Verifies success-criteria #1 and #6:**
- **#1** — a captain brings up ≥2 named crew sessions; `harmonik comms who` shows
  them online, each on its own named queue. (`crew start` is the mechanism the
  captain calls per crew.)
- **#6** — the captain spawns a NEW crew member non-interactively: writes a
  mission handoff and starts the session; the crew boots, auto-subscribes to its
  comms inbox, and claims its named queue with no human steps. (`crew start`
  delivers the mission seed + ensures the queue; the auto-subscribe/claim
  behaviour is the crew's boot loop, specified in C3 and merely *triggered* here.)

---

## 2. Research summary (R2 + R3)

**R2 — programmatic long-lived crew + `session_id` (corrected against the live docs):**
- **Two remote-control forms.** Server mode `claude remote-control` is an *empty*
  server with **no local pane** — only a human drives it from claude.ai/code or
  mobile; there is no parent-process seed path. **Interactive mode**
  `claude --remote-control "<title>"` runs as a **local tmux pane that is also
  cloud-watchable** — pasteable and autonomous, exactly harmonik's existing
  implementer-spawn shape. C2 uses the **interactive** form.
- **`session_id` is caller-minted, not captured.** `claude --session-id <uuid>`
  takes a caller-supplied UUID (harmonik already does this for ephemeral sessions,
  `claudelaunchspec.go:359`). C2 mints the UUID before launch and writes it to the
  registry immediately — there is **no** stdout-parse / gauge-back-fill step.
- **Seed = bracketed-paste** of a kick-off line into the interactive pane (the
  proven `pasteInjectImplementerInitial` mechanism). **Re-task / restart** =
  `claude -p --resume <uuid> "<msg>"` (appends a turn) or relaunch interactive
  `--resume <uuid>` (same id, continue; `--fork-session` would fork).
- **Rejected alternative — the Managed Agents / Agent SDK sessions API**
  (`POST api.anthropic.com/v1/sessions` + `events.send`). It is the *only* docs path
  with a server-minted id + programmatic message send, BUT it **bills the API
  credit pool**, not the Max subscription — exactly the credit-burn this project
  fought (the local `claude` CLI is what bills the subscription). It also
  contradicts the operator's confirmed local-CLI approach. Considered and rejected.

**R3 — keeper integration (keeper itself OUT of scope; C2 provides its inputs):**
- The keeper keys anti-loop identity on `session_id` via its gauge file and
  injects into a `--tmux <target>` it is *handed* (`cmd/harmonik/keeper_cmd.go:111`
  `TmuxTarget: tmuxFlag`; `internal/keeper/injector.go:25-26`). At spawn, C2 MUST:
  (a) stand up the per-crew gauge `.harmonik/keeper/<name>.ctx`,
  (b) create the `.managed` marker `.harmonik/keeper/<name>.managed`,
  (c) register the crew's pane/handle so the keeper can attach,
  else the keeper no-ops (`keeper.IsManaged`, `keeper.go:106`).
- **Dependency (out of this work):** correct wind-down on a 1M-context crew needs
  the keeper token-threshold fix (flagged to flywheel).

**Existing surfaces this composes on (current-state map, all verified):**
- CLI-verb convention: `cmd/harmonik/main.go:447` routes `harmonik comms` →
  `runCommsSubcommand`; `:431` routes `harmonik subscribe`. New verbs are added as
  an `os.Args[1] == "<verb>"` branch dispatching to a `runXSubcommand(os.Args[2:])`.
- Daemon RPC convention: a CLI subcommand marshals a `{op, payload}`
  `SocketRequest`, dials `<project>/.harmonik/daemon.sock`, and reads a
  `SocketResponse {ok, result|error}`. The acceptor dispatches on `req.Op` at
  `internal/daemon/socket.go:383` (e.g. `case "comms-send":` at `:470`). Exit code
  **17** = daemon not running (socket absent / ECONNREFUSED).
- Ephemeral spawn (NOT reused wholesale): `tmuxSubstrate.SpawnWindow`
  (`internal/daemon/tmuxsubstrate.go:309`) → `adapter.NewWindowIn`
  (`tmuxsubstrate.go:381`) → argv from `buildClaudeLaunchSpec`
  (`internal/daemon/claudelaunchspec.go:220`), `--session-id`/`--resume` argv at
  `claudelaunchspec.go:359/361`, initial prompt pasted via
  `pasteInjectImplementerInitial` (`internal/daemon/pasteinject.go:878`). Every
  ephemeral run ends `sess.Kill` + `removeWorktree`; there is **no persistent
  self-submitting spawn path** today — that is the net-new lifecycle.
- Named queues: `queue.Queue.Name` (`internal/queue/types.go:260`, charset
  `[a-z0-9-]`, 1–64, default `main`), persisted to `.harmonik/queues/<name>.json`
  via `queue.Persist` (`internal/queue/persistence.go:89`); loaded via
  `queue.Load` (`persistence.go:171`). Daemon already exposes `queue-submit`
  (`socket.go:408`, `queue.HandleQueueSubmit`).
- Keeper: `keeper.AcquireLock` (`internal/keeper/keeper.go:59`) writes
  `.harmonik/keeper/<agent>.lock`; `keeper.IsManaged` (`:106`) gates on
  `.harmonik/keeper/<agent>.managed`.

---

## 3. Approach (locked decisions + rationale)

### 3.1 CLI surface and daemon vs local

**Decision: `harmonik crew start/stop` is a DAEMON RPC**, mirroring `comms`/`queue`
(NOT a fully-local command like `comms log`/`comms who`).

CLI surface:
```
harmonik crew start <name> --queue <q> --mission <handoff-path> [--project DIR] [--socket PATH]
harmonik crew stop  <name> [--pause-queue] [--project DIR] [--socket PATH]
harmonik crew list  [--json] [--project DIR]          # read-only, local (reads .harmonik/crew/)
```

**Rationale (against the subscribe.go / comms.go pattern):**
- `crew start` must coordinate **three pieces of daemon-owned state in one atomic
  operation**: (1) ensure the named queue exists — if `queue.Load(projectDir, q)`
  returns nil/absent, persist a minimal empty `Queue{Name: q, Workers: 1}` via
  `queue.Persist` (idempotent; if it already exists, leave it untouched). The queue
  lifecycle is daemon-owned — the daemon dispatches it; an out-of-band file write
  would race the workloop. (2) launch the long-lived session, (3) write the registry
  record + keeper-attach inputs. Doing this client-side would race the daemon's queue
  load/persist and produce a registry record the daemon never observes. So `crew
  start` is a daemon op: client marshals `{op:"crew-start", payload:{name, queue,
  mission_path}}`, daemon performs all three steps and returns the minted
  `session_id`. This matches `comms-send` (`socket.go:470`) exactly: client → op →
  daemon handler → `SocketResponse`.
- **The session launch itself runs daemon-side** for the same reason ephemeral
  spawns do: the daemon owns the tmux substrate, the pane/handle bookkeeping, and
  the keeper-attach registration. A client-launched session would leave the daemon
  unable to associate the pane/handle with the registry.
- `crew list` is **read-only and local** (reads `.harmonik/crew/*.json` directly),
  mirroring `comms who`/`comms log` — no daemon round-trip, works even when the
  daemon is down, so an operator can always inspect the roster.
- Exit code **17** when the daemon is down for `start`/`stop` (mirrors comms).

### 3.2 Launch — interactive `claude --remote-control` + minted session_id + paste seed

**Decision: the daemon launches a NEW persistent variant of the spawn path —
interactive `claude --remote-control "<name>" --session-id <uuid>` — it does NOT
reuse the ephemeral worktree+kill `beadRunOne` path, and it does NOT use server-mode
`claude remote-control` (no local pane, human-only drive).**

Launch construction (a sibling of `buildClaudeLaunchSpec`, NOT a modification of
it — the ephemeral path is untouched):
- **Mint the session id first.** C2 generates a UUID and writes it to the registry
  *before* launch, so the id is known a priori and never needs capturing.
- argv = `[<claudeBinary> --remote-control "<name>" --session-id <uuid>]`.
  `<claudeBinary>` resolves the same way the daemon resolves its `HandlerBinary`
  (`internal/daemon/daemon.go:99`), defaulting to `claude`. The positional `"<name>"`
  is the remote-control title; `--session-id` is the global flag harmonik already
  uses (`claudelaunchspec.go:359`).
- **No worktree.** Crew run from the project root (or an operator-chosen cwd),
  persist across many epics, and dispatch their own beads to their named queue
  (which the *daemon* picks up in isolated worktrees as usual). The crew session
  is an orchestrator, not an implementer — so it gets no `--worktree`, no
  `--dangerously-skip-permissions` worktree gate.
- The session is spawned into a tmux window the daemon owns (reusing
  `NewWindowIn`-style window creation, `tmuxsubstrate.go:381`) so it is
  inspectable and the keeper can target its pane — BUT it is **never** `sess.Kill`'d
  on bead completion. It lives until `crew stop`.

**Mission-seed delivery — DECISION: bracketed-paste via the pasteinject mechanism,
delivering a single kick-off line that points the crew at the mission file**
(mirroring `pasteInjectImplementerInitial`, `pasteinject.go:878`, which pastes
`"Please read .harmonik/agent-task.md and begin."`).

- Concretely: `crew start` writes/echoes the mission handoff to a stable
  per-crew path the crew can read (the handoff schema is C3's contract:
  `{crew_name, queue, epic_id, goal, captain_name}` — C2 only delivers the path
  the captain passed via `--mission`), then after the session is alive, pastes a
  kick-off line: `"Please read <handoff-path> and run /session-resume on it, then
  begin your operating loop."`.
- **Rationale for bracketed-paste over `--append-system-prompt` / an initial-prompt
  arg:** (1) it is the *already-proven* mechanism in this codebase for seeding a
  freshly-launched `claude` session, including the splash-dismiss + post-paste
  Enter handling that the TUI requires (`pasteinject.go:886-909`); (2)
  `--append-system-prompt` mutates the system prompt rather than issuing a first
  user turn, which is the wrong altitude for "go resume this handoff and start
  working"; (3) the crew boot loop is a *user-turn* behaviour (subscribe to comms,
  claim queue, dispatch epic), exactly what a pasted first message triggers. The
  `--print`/initial-prompt path is for one-shot headless runs, not a long-lived
  session. **Resolved (was an open caveat):** the *interactive* `--remote-control "<name>"`
  form runs as a real local TUI pane (docs: "runs locally in your terminal, also
  accessible remotely"), so it IS pasteable exactly like a normal `claude` session.
  The mandatory manual smoke (§6) confirms `--session-id` composes with
  `--remote-control` on the live binary before declaring done.

**session_id — minted, not captured.** C2 generates a UUID (the same generator the
ephemeral path uses for `--session-id`) and writes the registry record with that id
**before** launching; pass it as `--session-id <uuid>`. There is no capture, no
stdout parse, no gauge back-fill, and no bounded-wait — the id is known a priori and
the registry's `session_id` is never empty.

**Re-task / restart.** To inject a later instruction (a new epic, a reprioritise),
the captain runs `claude -p --resume <uuid> "<message>"` (appends a turn to the
existing session and runs it) or the keeper relaunches the interactive session with
`--resume <uuid>` after a context-full cycle. `--resume` continues the **same**
session id by default; `--fork-session` (which we do NOT pass) would mint a new id.

### 3.3 Crew registry (new durable state)

**Decision: a new `internal/crew/` subsystem package** owning the registry
read/write/update API.

Record (`internal/crew/registry.go`, schema-versioned per project convention):
```
type Record struct {
    SchemaVersion int       `json:"schema_version"` // == 1
    Name          string    `json:"name"`           // stable key; charset [a-z0-9-]
    SessionID     string    `json:"session_id"`     // caller-minted UUID; set before launch; stable across --resume restart
    Queue         string    `json:"queue"`          // stable key; the crew's named queue
    Epic          string    `json:"epic"`           // assigned epic id; may be "" at start
    Handle        string    `json:"handle"`         // tmux pane/window handle for keeper-attach
    StartedAt     time.Time `json:"started_at"`     // UTC
}
```

API surface (atomic write per the WM-026 temp-write+rename pattern that
`queue.Persist` uses, `persistence.go:89-...`):
- `Write(projectDir string, r Record) error` — atomic create/overwrite of
  `.harmonik/crew/<name>.json`.
- `Load(projectDir, name string) (Record, error)` — read one record.
- `List(projectDir string) ([]Record, error)` — scan `.harmonik/crew/*.json`
  (powers `crew list`).
- `UpdateSessionID(projectDir, name, sessionID string) error` — read-modify-write
  the `session_id` field only (called on a re-mint after `--fork-session` — there is
  no gauge back-fill under the minted-id model).
- `Remove(projectDir, name string) error` — delete the record (called by `crew
  stop`).
- Name validation reimplements locally the same rule (reject `/` and `..`; require
  charset `[a-z0-9-]`, length 1–64) — `keeper.validateAgent` is unexported and
  `internal/crew` must not depend on keeper internals. This keeps `name` safe as
  both a filename and a comms identity.

**Rationale for a new package over daemon-inline:** the analysis (02-analysis.md
§Conventions) and the `go-subsystem-add` skill establish that durable
project-state with its own read/write/update lifecycle is a subsystem, not an
inline helper — the registry is read by the daemon (`crew start/stop`), the CLI
(`crew list`), and conceptually by the captain/keeper. A focused package keeps the
storage format + atomic-write discipline in one tested place, mirroring how
`internal/queue/persistence.go` and `internal/keeper/` own their own durable
files. **A new subsystem package REQUIRES a depguard component-matrix entry in
`.golangci.yml` per the `go-subsystem-add` skill** — the implementing agent MUST
run that skill's scaffold (package layout per `subsystem-organization.md`,
depguard matrix entry, test-helper hookup). The package depends only on stdlib +
`internal/core` (for any shared types); it MUST NOT import `internal/daemon`
(daemon imports crew, not vice-versa).

### 3.4 Keeper-attach at spawn (R3)

After the session is alive, the daemon (inside the `crew-start` handler) performs,
in order:
1. **Gauge bring-up.** Ensure `.harmonik/keeper/` exists; the per-crew gauge file
   `.harmonik/keeper/<name>.ctx` is written by the statusLine hook
   (`scripts/keeper-statusline.sh`) once the session runs with
   `HARMONIK_AGENT=<name>` + `HARMONIK_PROJECT=<projectDir>` in its environment.
   **C2's obligation:** set those two env vars on the launched session (via the
   tmux window env, the same channel `NewWindowIn` uses for `Env`) so the hook
   namespaces the gauge to `<name>`. (The hook itself is already wired in
   `~/.claude/settings.json`; C2 does not install it.) Optionally seed an initial
   `.ctx` so the keeper watcher's first poll finds a file.
2. **`.managed` marker.** Create `.harmonik/keeper/<name>.managed` (empty file) so
   `keeper.IsManaged(projectDir, name)` returns true (`keeper.go:106`) — without
   it the keeper no-ops (`keeper_cmd.go:94`). Reuse the same atomic create the
   keeper-enable path uses.
3. **Register the pane/handle.** Persist the crew's tmux pane/window handle into
   the registry `Handle` field so the keeper can be handed a `--tmux <target>`
   (`keeper_cmd.go:111`) that points at the crew's pane. (C2 does not start the
   keeper process; it makes the handle discoverable.) C2 only PERSISTS `Handle` in
   the registry; actually starting the keeper against that `--tmux` target is the
   operator's / keeper-Phase-2's responsibility and is out of scope here.

These three steps are the entirety of C2's keeper involvement. The keeper watcher,
cycler, injector, and the token-threshold calibration are all out of scope.

### 3.5 Stop path (minimal)

`harmonik crew stop <name>`:
1. Read the registry record (`crew.Load`); error if absent.
2. Stop the live session — `/quit`-then-kill the crew's tmux pane via its
   `Handle` (reuse the daemon's existing quit→grace→kill teardown helper used for
   ephemeral sessions; the *trigger* is `crew stop`, not bead completion).
3. Remove the `.managed` marker (`.harmonik/keeper/<name>.managed`) so the keeper
   stops acting on the now-dead pane. Leave the `.ctx` gauge file (harmless; it
   ages out).
4. `crew.Remove` the registry record.
5. **`--pause-queue` (optional flag):** if set, halt dispatch on the crew's named
   queue via the existing `queue-set-concurrency <q> 0` op (sets worker concurrency
   to 0 without killing the queue; resume later with `queue-set-concurrency <q> N`)
   so no further dispatch happens. Default: leave the queue as-is (the captain may
   reassign it; queue lifecycle is otherwise untouched).

**Stop explicitly does NOT:** reassign the epic, close beads, re-home work to
another crew, or delete the named queue. Epic reassignment is captain *judgment*,
which is out of scope (03-components.md "Explicitly out").

---

## 4. Files & changes

| Path | Create/Modify | Why |
|---|---|---|
| `cmd/harmonik/crew.go` | **Create** | `runCrewSubcommand(subArgs)` + `start`/`stop`/`list` verb routing, flag parsing, `{op,payload}` marshal + dial, `SocketResponse` decode. Mirrors `cmd/harmonik/comms.go`. Includes usage text. |
| `cmd/harmonik/main.go` | **Modify** | Add the `os.Args[1] == "crew"` dispatch branch → `runCrewSubcommand(os.Args[2:])`, beside the `comms` branch (`main.go:447`). |
| `internal/crew/registry.go` | **Create** | New subsystem package: `Record` struct + `Write`/`Load`/`List`/`UpdateSessionID`/`Remove` with atomic temp-write+rename; name validation. |
| `internal/crew/registry_test.go` | **Create** | Table-driven unit tests for the registry round-trip + update + validation. |
| `internal/daemon/crewstart.go` | **Create** | Daemon-side `crew-start` / `crew-stop` handler: mint the `session_id` UUID + write the registry record, ensure the named queue exists (persist a minimal empty `Queue{Name:q, Workers:1}` via `queue.Persist` if absent; idempotent), launch the interactive `claude --remote-control` session (new sibling launch-spec builder), paste the mission seed, do the keeper-attach steps (§3.4), teardown on stop (§3.5). |
| `internal/daemon/crewlaunchspec.go` | **Create** | `buildCrewLaunchSpec(...)` — the persistent-session argv builder (`claude --remote-control "<name>" --session-id <uuid>` + env: `HARMONIK_AGENT`, `HARMONIK_PROJECT`). Sibling of `buildClaudeLaunchSpec`; does NOT touch it. |
| `internal/daemon/socket.go` | **Modify** | Add `case "crew-start":` and `case "crew-stop":` to the op switch (`socket.go:383`), dispatching to a new `CrewHandler` interface. The `CrewHandler` interface type is DECLARED in `internal/daemon/crewstart.go` (alongside the handler impl) and registered in `daemon.go` like `CommsSendHandler`. |
| `internal/daemon/daemon.go` (wiring) | **Modify** | Construct + register the `CrewHandler` impl, threading `HandlerBinary`, project dir, tmux substrate, and queue access. |
| `.golangci.yml` | **Modify** | Add the `internal/crew` depguard component-matrix entry (per `go-subsystem-add`): allow stdlib + `internal/core`; deny `internal/daemon` import from `internal/crew`. |
| `internal/crew/` test-helper hookup | **(per skill)** | Per `go-subsystem-add`, register any package test helpers in `internal/testhelpers/` if used. |

No change to: the ephemeral `beadRunOne` spawn path, `buildClaudeLaunchSpec`,
`tmuxSubstrate.SpawnWindow`, the queue model, the keeper package, or any spec under
`specs/`. (If C2's new event/handle conventions warrant a normative line, that is a
separate spec-edit decision, not a code change here.)

---

## 5. Acceptance criteria (concrete / testable)

AC-1 (**#6 + #1**): `harmonik crew start alpha --queue alpha-q --mission
/tmp/alpha-handoff.md` against a running daemon:
  - exits 0 and prints the minted `session_id`;
  - produces a live interactive `claude --remote-control "alpha" --session-id <uuid>`
    session visible as a tmux pane the daemon owns and does NOT kill;
  - creates `.harmonik/crew/alpha.json` containing `{name:"alpha",
    queue:"alpha-q", session_id:<the-minted-uuid>, handle:<pane>,
    started_at:<utc>}` (schema_version 1), readable by `crew.Load`, with a
    non-empty `session_id`;
  - the named queue `alpha-q` exists at `.harmonik/queues/alpha-q.json` (minimal
    empty `Queue{Name:"alpha-q", Workers:1}` if it was not already present), and the
    crew→queue binding is recorded only in `.harmonik/crew/alpha.json` (`queue:"alpha-q"`)
    — there is no owner field on the queue itself;
  - the keeper inputs are present: `.harmonik/keeper/alpha.managed` exists and the
    env vars (`HARMONIK_AGENT`/`HARMONIK_PROJECT`) are set on the launched session.
    IF the §6 smoke confirms the statusLine hook fires under `--remote-control`,
    `.harmonik/keeper/alpha.ctx` is written within a few ticks reporting the same
    `session_id`; otherwise the asserted keeper inputs are just the env-vars-set +
    `.managed` marker present.

AC-2 (**#1**): two `crew start` invocations (`alpha`, `beta`) each produce a
distinct registry record + distinct named queue; `harmonik crew list` shows both;
once each crew has run its boot loop, `harmonik comms who` shows both online.
(The comms-online behaviour is the crew boot loop from C3, *triggered* by C2's
mission seed — C2's AC is that the seed was delivered and the queue+marker exist.)

AC-3 (**stop**): `harmonik crew stop alpha` exits 0; the `alpha` tmux pane is gone;
`.harmonik/crew/alpha.json` is removed; `.harmonik/keeper/alpha.managed` is
removed; with `--pause-queue`, `alpha-q` is paused. `crew list` no longer shows
`alpha`.

AC-4 (**registry unit**): `internal/crew` round-trips a record through
`Write`→`Load`, `UpdateSessionID` mutates only `session_id`, `List` returns all
records sorted by name, and invalid names (`/`, `..`, uppercase, >64 chars) are
rejected.

AC-5 (**launch-spec unit**): `buildCrewLaunchSpec` produces argv
`[<claude> --remote-control "<name>" --session-id <uuid>]` with the caller-supplied
UUID, `HARMONIK_AGENT`/`HARMONIK_PROJECT` in the env, and NO worktree / NO
`--dangerously-skip-permissions`.

---

## 6. Verification

- **Unit (CI gate, fast):**
  - `go test ./internal/crew/...` — registry read/write/update/list/validation (AC-4).
  - `go test ./internal/daemon/ -run CrewLaunchSpec` — argv + env construction (AC-5).
  - `go test ./internal/daemon/ -run CrewStart` — handler logic with a fake
    substrate + fake queue access (queue-ensured, registry-written, keeper marker
    created), asserting the minted UUID is written to the registry before launch.
- **Daemon-level scenario (NOT in the daemon gate — run yourself):** a
  `//go:build scenario` test that boots a real daemon and drives `crew start`
  against the **twin** `harmonik-twin-claude` binary standing in for `claude
  remote-control`, asserting the registry file, queue file, and `.managed` marker
  appear, and `crew stop` tears them down. (The daemon gate SKIPS scenario tests —
  per `reference_scenario_test_authoring`, author/run this via a worktree agent or
  manually.)
- **Manual smoke (the real-`claude` path — REQUIRED before declaring done):**
  - **Step 0 (preflight, BEFORE writing handler code):** run a throwaway
    `claude --remote-control "preflight" --session-id <uuid>` and confirm the pane
    comes up and the statusLine hook fires (writes a `.ctx` gauge). This validates
    the two runtime unknowns from §Risks before any handler code is written; if the
    hook does not fire under `--remote-control`, the keeper-attach AC weakens (see
    AC-1) and that must be settled first.
  - Then, with a live daemon under the supervisor session, run
  `harmonik crew start <name> --queue <q> --mission <handoff>` against the **real**
  `claude`, and confirm: (1) `claude --remote-control "<name>" --session-id <uuid>`
  composes — the session comes up on the minted id as a pasteable, cloud-watchable
  pane; (2) the bracketed-paste mission seed lands and the crew begins its loop;
  (3) `claude -p --resume <uuid> "<msg>"` re-tasks the same session; (4)
  `harmonik comms who` eventually shows the crew. This validates the two things
  `go test` cannot: the `--remote-control`+`--session-id` composition and the
  paste-seed delivery. Per the daemon-code lesson
  (`reference_harmonik_daemon_session_nesting`): reviewer-reasoning + `go test` is
  NOT sufficient for tmux/session/spawn code — always live-smoke under the
  supervisor.

---

## 7. Error handling & edge cases

- **Name collision (crew already exists):** if `.harmonik/crew/<name>.json`
  exists with a live session, `crew start` fails with a clear "crew <name> already
  running" message (non-zero exit, NOT 17). If the record exists but the session
  is dead (stale), treat as a re-launch: re-use `name`+`queue` **and the recorded
  `session_id`**, relaunching interactive `--resume <uuid>` to continue the same
  conversation (no new id minted).
- **Queue already bound to another crew:** the `internal/queue` model has NO
  owner/assignee field and we do NOT add one; the crew→queue binding lives only in
  the crew registry (`Record.Queue`). Conflict detection scans `crew.List` for an
  existing LIVE record with the same `Queue` under a *different* `name`; if found,
  fail with "queue <q> already bound to crew <other>". If the conflicting record is
  this same crew (re-launch) or no record binds `<q>`, proceed.
- **session_id is minted, so it never "fails to capture."** The id is written to
  the registry before launch. If the launch itself fails, see the launch-failure
  edge case below (the record is rolled back; no partial state).
- **Keeper gauge already present:** seeding `.ctx` is idempotent (overwrite is
  fine); the `.managed` marker create is idempotent (already-exists is success).
- **Daemon restart while a crew is live:** the registry is durable
  (`.harmonik/crew/`), so the daemon re-discovers crew on boot via `crew.List`, and
  the `session_id` is the durable minted id (not stale — it's the same one
  `--resume` uses). (No auto-resume of the *queue* across a daemon restart —
  `specs/queue-model.md:283`; the operator/captain resumes it.)
- **launch fails** (binary missing, `NewWindowIn` error): the registry record is
  written first (it holds the minted id), so on launch failure C2 rolls it back
  (`crew.Remove`), creates no `.managed` marker, leaves the named queue as-found,
  and returns a structural error (non-zero). Ordering: check-collision → mint id +
  write registry → ensure queue → launch → on success, keeper-attach inputs; on
  failure, remove the record. (If the queue had to be created and the launch then
  fails, the empty queue is harmless and reused on retry.)
- **Daemon not running:** `crew start`/`crew stop` exit **17** (socket absent /
  ECONNREFUSED), mirroring comms. `crew list` works without the daemon (local
  read).

---

## 8. Migration / back-compat

Purely **additive**:
- New verb `harmonik crew` — no existing verb changes.
- New gitignored state dir `.harmonik/crew/` — already covered by the
  `/.harmonik/` gitignore rule (verified: `git check-ignore .harmonik/crew/x.json`
  matches `.gitignore:28`). No `git status` noise.
- New `internal/crew/` package + one `.golangci.yml` depguard entry.
- The ephemeral bead-dispatch spawn path, the queue model, and the keeper package
  are untouched. No schema migration; no behavioural change to any existing
  command. A daemon without the C2 handler simply rejects `crew-start`/`crew-stop`
  ops (forward-incompatible old binary), which is the same posture as any other
  new op.
