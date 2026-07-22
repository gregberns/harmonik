# 03 — Remote Execution Architecture

> Repo state: branch `phase1-session-restart-substrate` @ `37651569`. Daemon + comms DOWN;
> all claims below grounded in source / git / beads read statically. Flags marked
> **[UNVERIFIED]** where I could not confirm at runtime.

---

## Bottom line

The operator's suspicion is **substantially correct** for the remote-*worker* substrate that
actually exists today. A harmonik remote run does **ship the whole project onto the worker** — the
worker holds a full, standing git clone at `repo_path` (`/Users/gb/harmonik-worker/repo`,
`.harmonik/workers.yaml:11`), and each run `git worktree add`s a fresh worktree *on the worker*,
runs the agent there over ssh, and the daemon "connects back" two ways: a per-run `ssh -N -R`
reverse tunnel for the agent's hooks, plus a direct `git fetch ssh://worker/...` to pull the
completed `run/<id>` branch home for merge. The unit of remote dispatch is a **whole run** (one
bead's entire DOT graph — implement → review → close — pinned to ONE worker), **not** per-node
streaming to a resident process. The "boot one codex app-server on a remote box and stream a ton of
work at it" model the operator describes is **not what remote workers do**; the only place that
model appears in the codebase is a *separate, orchestrator-focused* kerf work
(`codex-app-server`) that is still at spec/research stage with no implementation, and even there it
targets running a *captain/crew-lead orchestrator* on a resident app-server, not streaming worker
tasks to one.

---

## How remote work is placed + executed

**What lives on the worker: the whole repo (a standing clone), plus per-run worktrees.**

- `workers.yaml` gives each worker a `repo_path` — a pre-provisioned full git clone the operator
  must set up out-of-band: `repo_path: /Users/gb/harmonik-worker/repo` (`.harmonik/workers.yaml:11`).
  It is not a binary drop and not created per-run; the daemon assumes it exists.
- The daemon binary must ALSO be installed on the worker (`harmonik_path` /
  `workers.DefaultHarmonikPath`, `reversetunnel.go:59-64`) because the agent's hook subprocess runs
  on the worker and shells out to it.

**Unit of remote work = a WHOLE RUN, not per-node.** `SelectWorker()` picks a worker once, at the
top of `beadRunOne`, for the entire bead run. The DOT case (`workloop.go:4130`) hands the whole
validated graph to `driveDotWorkflow` with a single `dotRunner = rbc.sshRunner`
(`workloop.go:4186-4203`) that every node — implement, gate, review, close — is dispatched through.
So one worker executes the complete `start → … → terminal` cascade for that bead
(`workloop.go:4205-4209`); there is no notion of node-A-here, node-B-there.

**Per-run code-sync sequence (DD1 / remote-substrate B8), `codesync_rs_b8.go:1-40`:**

1. **fetch-base on worker** — `ssh worker -- git -C <repoPath> fetch origin <baseSHA>`
   (`ensureBaseOnWorker`, `workloop.go:3839`; `codesync_rs_b8.go:60+`). Ensures the base commit is
   in the worker's ODB before the worktree is cut. `fetchBaseOnWorker`+`worktree add` are run as ONE
   serialized critical section under `worktreeCreateMu` to avoid the empty-HEAD race
   (`workloop.go:3813-3839`, hk-5qp7z / hk-2hfyt).
2. **worktree add on worker** — `workspace.CreateWorktree(ctx, workerRepoPath, runID, headSHA, cfg)`
   with the SSHRunner threaded in (`workloop.go:3783-3792`). The worktree lands at
   `<repoPath>/.harmonik/worktrees/<run_id>/` on the worker
   (`worktreepath.go:12,148`; worktree root is repo-relative, so it is under the worker's clone).
3. **materialize launch artifacts ON the worker** — settings.json (hook bridge), agent-task.md,
   `~/.claude.json` trust, isolated CLAUDE_CONFIG_DIR — all routed through the SSHRunner via
   base64-over-ssh / `python3 -` stdin writes (`remotematerialize.go`, whole file; `workloop.go:4354-4364`).
   A box-A-local write here would land on the wrong machine (that was the hk-z8ek symptom).
4. **agent runs on the worker**, committing to a `run/<runID>` branch created by
   `worktree add -b run/<id>`.
5. **box-A fetch (connect back for results)** — `git -C <projectDir> fetch
   ssh://<host><repoPath> run/<runID>:refs/heads/run/<runID>` (`codesync_rs_b8.go:25,186-214`).
   Box A pulls the run branch **directly from the worker's clone over ssh** — the old
   worker→GitHub→box-A round-trip was removed because the worker's GitHub push credential is invalid
   (hk-7bwx). The final merge is the UNCHANGED local `mergeRunBranchToMain`, and only box A's
   credentials push `main` to origin.
6. **worktree GC** — on completion the daemon `ssh worker -- git worktree remove --force` + `prune`
   (`workloop.go:3793-3800`).

So: **whole repo clone (standing) + per-run worktree + per-run ssh dispatch + git-branch results
pulled back over ssh.** No binary-only, no streaming.

---

## The connect-back / reverse tunnel (tcp:59963 and friends)

**What it is:** a per-run, long-lived `ssh -N -R 127.0.0.1:<port>:<box-A daemon.sock>` process
(`reversetunnel.go:3-35, 188-247`). `59963` is just one instance of `<port>` — an ephemeral port
allocated per run by `allocateReverseTunnelPort()` (`reversetunnel.go:153`; called at
`workloop.go:3647`). It reverse-forwards a loopback TCP listener **on the worker** back to box A's
Unix daemon socket.

**Why a remote agent must call back at all:** the agent (claude or codex) running on the worker has
to reach the *dispatching daemon* on box A to drive its lifecycle — this is the hook relay:
`agent_ready` signaling, progress heartbeats, and the hook bridge
(`hookrelay.go:494-655`, which dials the endpoint and emits `bridge_dial_failed` on any dial
error). The env var `HARMONIK_DAEMON_SOCKET` is set to `tcp://127.0.0.1:<port>` for a remote run and
the plain unix `daemon.sock` for a local run (`resolveAgentDaemonSocket`, `reversetunnel.go:260-278`;
wired at `workloop.go:4311-4325`). The `tcp://` prefix is the switch the hookrelay dialer keys off
(`reversetunnel.go:89-106`).

**Why it's a separate long-lived ssh (not `-R` on the spawn ssh):** the agent is spawned via a
DETACHED `ssh worker -- tmux new-window -d …` that returns immediately, so a `-R` riding it would be
torn down before the first hook. The tunnel must be its own `ssh -N -R` kept warm for the run
(`reversetunnel.go:5-11`, `workloop.go:3590-3604`).

**Why TCP loopback, not a unix socket (hk-ege6):** on macOS sshd runs as root, so a `-R` StreamLocal
bind creates a root-owned `0600` socket the unprivileged worker user's hook cannot connect to →
`connect: permission denied` → silent relay death → `agent_ready_timeout`
(`reversetunnel.go:20-30`). A TCP listener has no filesystem perm bits.

**Where it breaks (the hk-qxvc2 wedge):** the agent can fire `agent_ready` BEFORE the forward is
actually live; the hook relay does NOT retry on a dial failure, so it silently
`bridge_dial_failed`s (`reversetunnel.go:298-313`). There is a readiness gate —
`waitWorkerSocketLive` runs `nc -z 127.0.0.1 <port>` AS THE WORKER USER and refuses to Launch until
the listener answers (`reversetunnel.go:329-361`, gate invoked `workloop.go:3712-3735`). The known
failure in hk-qxvc2 gate #4 is that for the codex-substrate remote review node the tunnel was "not
stood up on the worker (silent bridge_dial_failed)" — i.e. the review node never got a live
reverse-tunnel/relay, so even after substrate-routing was fixed the reviewer never emitted
`agent_ready`. Under concurrency the tunnel also collapsed onto ssh's shared ControlMaster until it
was forced onto a dedicated non-multiplexed connection (`ControlMaster=no`/`ControlPath=none`,
`reversetunnel.go:207-218`, hk-cnp17).

---

## The review-node entanglement (why review transport couples to the codex substrate)

**Root cause: the substrate is chosen ONCE, GLOBALLY, at daemon boot.** `selectSubstrate`
(`substrate_select.go:74-81`) reads `HARMONIK_SUBSTRATE` at boot and wires a SINGLE
`daemon.Config.Substrate`. When `HARMONIK_SUBSTRATE=codexdriver`, that global substrate is the Codex
app-server driver, which speaks JSON-RPC (`initialize`, thread/start…). Every node in a DOT run
defaults to `deps.substrate` (`dot_cascade.go:1450`). So a **claude** review node handed the global
codex substrate gets driven with codex's app-server JSON-RPC `initialize` frame — claude replies in
prose and never emits `agent_ready` (hk-qxvc2 comment 22:17). The transports are "entangled" only
because there is one global substrate variable and the review node inherited it.

**The mitigation (a second substrate handle):** `selectSubstrate` returns a distinct
`reviewerSubstrate` that is ALWAYS the tmux/claude substrate, even on the codex path
(`substrate_select.go:72-80` — literally `return codexdriver.NewCodexSubstrate(opts), …, tmuxSub`).
The DOT cascade then, for a claude (SessionIDMinted) reviewer, swaps the base substrate to it:

```
dot_cascade.go:1444-1453
  reviewerHarnessIsClaude := isReviewer && harness.SessionIDPolicy()==SessionIDMinted
  baseSubstrate := deps.substrate
  if reviewerHarnessIsClaude && deps.reviewerSubstrate != nil { baseSubstrate = deps.reviewerSubstrate }
```

(mirrored in `reviewloop.go:1284-1285` and `dot_gate.go:324-325`). This is the "route the review node
through the CLAUDE substrate/protocol" fix. Gate-4 evidence (hk-qxvc2, 23:29) confirms the routing
WORKED — a tmux session was created on the worker at `reviewer_launched`, i.e. the reviewer was on
tmux not codex JSON-RPC — but it was **necessary-not-sufficient**: the reviewer still timed out
because the reverse-tunnel/relay and remote-onboarding pieces above were not all standing on the
worker. **Why review is fatal:** review is the sole inbound edge to `close`, so a stuck review node
blocks EVERY DOT bead from closing on the codex substrate (hk-qxvc2 body).

Secondary coupling that made this worse: codex's own commit path (hk-daegv) and env-forwarding
(hk-okqyx/hk-qxvc2) are ssh-transport concerns bolted onto the same remote codex dispatch — the
composition root now injects writable git-common-dir roots and an `exec env KEY=VAL … codex` argv
prefix because ssh drops client env (`substrate_select.go:227-263`; hk-okqyx comment; commits
`f907b702`, `44831898`).

---

## Operator's model vs current model

| Dimension | Operator's model ("point at a remote app-server, stream work") | Current model (remote-worker substrate) |
|---|---|---|
| Remote process | ONE resident, long-lived server you connect to | NEW ssh spawn per run (`tmux new-window -d`), plus one standing git clone |
| What's shipped | just work/threads over a protocol | whole repo clone (standing) + per-run worktree + per-run artifact writes over ssh |
| Unit of work | a stream of threads/turns | a whole bead run (entire DOT graph) pinned to one worker |
| Result return | streamed back over the same connection | daemon `git fetch ssh://worker` pulls the `run/<id>` branch, merges locally |
| Connect-back | inherent in the persistent connection | per-run `ssh -N -R` reverse tunnel for hooks + separate git-fetch for code |
| Codex worker today | app-server, resident | `codex exec --json` ONE-SHOT per turn, resume via `codex exec resume <thread_id>` (`codexharness.go:9-19,182`) — spawn-per-turn, NOT resident |

**Is "point at a remote app-server and stream" blocked, planned, or possible?**

- **Not the current worker design.** Remote workers are architecturally spawn-per-run + git-branch
  merge-back. Even the *codex driver* variant (`codexdriver`, the app-server JSON-RPC path) is still
  constructed once at box A and routed to the worker with a NEW `ssh … codex app-server` per run via
  `codexWorkerRoutingRunner.Command` (`substrate_select.go:154-176`); it is not a persistent remote
  server you stream at. So the app-server is used as a *local-protocol* to a *per-run-spawned* codex,
  which is the awkward middle the wedges (hk-czb11 remote-cwd, hk-okqyx env, hk-daegv sandbox) all
  live in.
- **Planned — but for a DIFFERENT purpose.** The `codex-app-server` kerf work
  (`.kerf/works/codex-app-server/01-problem-space.md`) IS about a resident app-server session, but
  its target is running a crew **orchestrator** (captain/crew-lead) on a resident Codex session to
  retire the keeper/compaction cycle — explicitly "the orchestrator path is additive, not a rewrite
  of the worker path" and "no implementation" this phase (lines 43, 59-61). It does not propose
  streaming worker tasks to a remote app-server.
- **Possible, with real work.** Nothing is *architecturally* blocked, but adopting the operator's
  model means: a persistent per-worker app-server client subsystem (reconnect/backpressure/auth as
  new daemon-owned failure modes, per C4 of that work), replacing "per-run ssh spawn + git-branch
  merge-back" with "stream threads + stream diffs", and re-answering how box A gets the code back
  (today it is `git fetch` of a branch the worktree produced). The current worktree+ssh+reverse-
  tunnel plumbing would be largely retired, not extended.

---

## ASCII diagram — current remote flow

```
  BOX A (daemon host)                                   WORKER (gb-mbp, ssh/Tailscale)
  ┌────────────────────────────┐                        ┌──────────────────────────────────┐
  │ SelectWorker() picks worker│                        │ standing clone: repo_path         │
  │ ONCE for the whole run     │                        │   /Users/gb/harmonik-worker/repo  │
  │                            │                        │                                   │
  │ (a) fetch-base ────────────┼──ssh git fetch────────▶│ base SHA into worker ODB          │
  │ (b) CreateWorktree ────────┼──ssh git worktree add─▶│ .harmonik/worktrees/<run_id>/     │
  │ (c) materialize artifacts ─┼──ssh base64/python3 ──▶│ settings.json, agent-task.md,     │
  │     (hook cfg, trust, cfg) │                        │ ~/.claude.json, CLAUDE_CONFIG_DIR │
  │                            │                        │                                   │
  │ daemon.sock (unix) ◀═══════╪══ ssh -N -R ═══════════╡ tcp://127.0.0.1:<port>  (per run) │
  │        ▲  hooks:           │   reverse tunnel        │   ▲ agent hook relay dials this   │
  │        │  agent_ready,     │   (long-lived)          │   │ (bridge_dial_failed if dead)  │
  │        │  progress         │                        │   │                               │
  │        └────────────────── HOOK RELAY ──────────────┼───┘                               │
  │                            │                        │ AGENT spawned: ssh -- tmux -d     │
  │ drive whole DOT graph  ────┼──ssh (dotRunner) ──────▶│  implement → gate → review → close│
  │  (all nodes, one worker)   │                        │  commits to run/<run_id> branch   │
  │                            │                        │                                   │
  │ (d) box-A fetch ◀──────────┼──git fetch ssh://worker│ run/<run_id> (from worker clone)  │
  │ mergeRunBranchToMain (local)│                       │ (e) worktree remove --force (GC)  │
  │ push main → GitHub          │                       └──────────────────────────────────┘
  └────────────────────────────┘
```

Connect-back happens in TWO places: the **reverse tunnel** (worker→box-A, live, for hooks) and the
**box-A git fetch** (box-A←worker, at completion, for code).

---

## Classification table

| Architectural choice | Where | Tag | Why |
|---|---|---|---|
| Whole standing repo clone lives on the worker (`repo_path`) | `workers.yaml:11`; `codesync_rs_b8.go` | **SELF-IMPOSED-CONSTRAINT** | Chosen so `git worktree add` + local commits work on the worker; a streaming design would not need it. |
| Per-run `git worktree add` on the worker | `workloop.go:3783-3792`; `worktreepath.go:148` | **SELF-IMPOSED-CONSTRAINT** | Reuses the local worktree isolation model verbatim on the remote; not forced by ssh. |
| Whole bead run (entire DOT graph) pinned to ONE worker | `workloop.go:4186-4209` | **SELF-IMPOSED-CONSTRAINT** | Follows from run-scoped `SelectWorker`; per-node routing is possible but not built. |
| Results returned as a `run/<id>` git branch fetched over ssh | `codesync_rs_b8.go:25,186-214` | **SELF-IMPOSED-CONSTRAINT** | Deliberate rework off the GitHub round-trip (hk-7bwx); a design decision, not a platform limit. |
| Per-run `ssh -N -R` reverse tunnel for the hook relay | `reversetunnel.go`; `workloop.go:3590-3735` | **ACTUAL-LIMITATION** (given the spawn-per-run + hook model) | A remote agent genuinely cannot reach box A's unix socket; SOME callback is required. Its per-run churn is downstream of the self-imposed spawn-per-run choice. |
| TCP loopback (not unix socket) for the `-R` bind | `reversetunnel.go:20-30` | **ACTUAL-LIMITATION** | macOS root sshd makes a `-R` StreamLocal socket unconnectable by the worker user (hk-ege6). Real OS constraint. |
| Dedicated non-multiplexed ssh per tunnel | `reversetunnel.go:207-218` | **ACTUAL-LIMITATION** | ssh ControlMaster sharing genuinely collapses concurrent `-R` forwards (hk-cnp17). |
| Single GLOBAL substrate chosen at boot (`HARMONIK_SUBSTRATE`) | `substrate_select.go:74-81` | **SELF-IMPOSED-CONSTRAINT** | The entanglement source: one substrate var → review node inherits codex JSON-RPC. Mitigated by a second `reviewerSubstrate` handle, not by decoupling per-node. |
| `reviewerSubstrate` always tmux even on codex path | `substrate_select.go:72-80`; `dot_cascade.go:1444-1453` | **ASSUMPTION** (that a claude reviewer + a codex implementer can coexist per run) | A patch over the global-substrate choice; assumes review must be claude and needs a parallel transport. |
| Codex WORKER is spawn-per-turn (`codex exec --json`), not resident | `codexharness.go:9-19,182` | **SELF-IMPOSED-CONSTRAINT** | Directly contradicts the operator's "resident, stream at it" model; a resident worker is not implemented. |
| Remote codex needs writable-git-root + env-argv-prefix hacks | `substrate_select.go:227-263`; hk-daegv/hk-okqyx | **ACTUAL-LIMITATION** (of driving codex over ssh) surfaced BY the self-imposed per-run-ssh choice | ssh drops env (no SendEnv) and codex's seatbelt denies the worktree's git-common-dir; both are real, both exist only because codex is spawned fresh over ssh per run. |
| No enabled worker today (fleet is LOCAL-only) | `workers.yaml:13` `enabled: false` | **ACTUAL-LIMITATION** (operational) | gb-mbp durably disabled after recurring empty-HEAD worktree-create failures; remote path is not currently exercised. **[UNVERIFIED at runtime — daemon down]** |

---

## Loose ends / UNVERIFIED

- gb-mbp is `enabled: false` (`workers.yaml:13`) after the 2026-07-12 empty-HEAD incident; I could
  not confirm live routing state (daemon down). The remote path's current health is asserted from
  bead history, not observed.
- hk-qxvc2 / hk-daegv are OPEN as of `37651569`; the review-node stall and codex-commit-sandbox
  wedges were NOT closed on this branch (fixes `fff3d937`/`f907b702`/`44831898` landed but the live
  re-gate never went green — "daemon owns close", no terminal transition). Treat the codex remote
  substrate as unproven end-to-end.
- The `codex-app-server` resident-orchestrator work is spec/research only (no code); its
  applicability to *worker* streaming is my inference from its problem-space scope, not a stated goal.
