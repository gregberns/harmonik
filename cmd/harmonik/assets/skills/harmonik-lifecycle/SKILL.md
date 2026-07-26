---
name: harmonik-lifecycle
description: >
  Agent/operator reference for harmonik's four project- and daemon-LIFECYCLE
  commands — `harmonik init` (one-time project bootstrap), `harmonik supervise`
  (the supervisor process that auto-revives the daemon), `harmonik reconcile`
  (close in_progress beads whose work already merged), and `harmonik promote`
  (move reviewed work toward the target branch in push- or PR-mode). Load this
  when you are STANDING UP, RESTARTING, RECONCILING, or PROMOTING a harmonik
  deployment — i.e. operating on the project/daemon itself, as opposed to the
  per-task dispatch loop (that is harmonik-dispatch). Covers each command's
  real purpose, real flags, real exit codes, and when an agent or operator
  reaches for it — all derived from the cmd/harmonik source, not the CLI --help
  alone. Notes that `promote` is LANDED with BOTH push-mode and PR-mode
  (hk-pk3p1, superseding the older "coming" framing). Load-bearing: must not rot.

sources:
  - cmd/harmonik/supervise_cmd.go
  - cmd/harmonik/supervise/start.go
  - cmd/harmonik/supervise/stop.go
  - cmd/harmonik/supervise/status.go
  - cmd/harmonik/supervise/restart.go
  - cmd/harmonik/supervise/pause.go
  - cmd/harmonik/supervise/resume.go
  - cmd/harmonik/supervise/attach.go
  - cmd/harmonik/supervise/logs.go
  - cmd/harmonik/supervise/shim.go
  - cmd/harmonik/supervise_reap_hkizs8s_test.go
  - internal/supervise/daemon_watchdog.go
  - cmd/harmonik/promote_cmd.go
  - cmd/harmonik/reconcile.go
  - cmd/harmonik/init_cmd.go
  - cmd/harmonik/assets/skills/keeper/SKILL.md
  - AGENTS.md §"Work-project deployment"
---

<!-- SOURCE OF TRUTH: cmd/harmonik/assets/skills/harmonik-lifecycle/SKILL.md (Go //go:embed).
     The copy at .claude/skills/harmonik-lifecycle/SKILL.md is GENERATED OUTPUT — `harmonik sync-assets`
     overwrites it from the embed and there is NO reverse sync, so an edit made
     only there silently drifts and is eventually reverted. To change this skill:
     edit the cmd/harmonik/assets/ copy, then mirror it byte-for-byte into
     .claude/skills/ in the SAME commit. The two paths must stay byte-identical. -->

# harmonik lifecycle / operator surface

These four commands operate on the **harmonik deployment itself** — the
project, the daemon process, the bead ledger's drift, and the integration→main
boundary. They are distinct from the **per-task dispatch loop** (`harmonik queue
submit` / `append` / `subscribe`), which is covered by the **harmonik-dispatch**
skill, and from the **context-fill watcher** (`harmonik keeper`), which is the
**keeper** skill.

| command | what it manages | when you reach for it |
|---|---|---|
| `harmonik init` | first-time project bootstrap | standing up harmonik on a NEW repo |
| `harmonik supervise` | the supervisor process (auto-revives the dead daemon) | start/stop/inspect the supervisor; pause/resume dispatch |
| `harmonik reconcile` | bead-ledger ⇄ git drift (Cat 3c) | a bead is stuck `in_progress` though its work merged |
| `harmonik promote` | integration → target-branch promotion | landing a reviewed SHA, or opening the integration→main PR |

**`--project` resolution differs — do not assume the env var is read.** Only
`promote` falls back to `$HARMONIK_PROJECT` (`runPromoteSubcommand`,
`cmd/harmonik/promote_cmd.go`). `init`, `reconcile`, and every `supervise` verb
resolve **explicit flag → cwd** and never consult the env var. So always pass
`--project $HARMONIK_PROJECT` explicitly from an orchestrator — it is the only
thing that protects you from CWD drift (a known hazard with worktree sub-agents).

---

## § `harmonik supervise` — the supervisor (auto-revive)

`supervise` manages a long-lived **supervisor** process that runs inside a
detached tmux session named `harmonik-<project_hash>-flywheel`, with
`remain-on-exit on`. The supervisor's load-bearing job (under `--watch-restart`)
is **auto-revival of the harmonik daemon**: a `DaemonWatchdog` probes the
daemon's Unix socket on a fixed interval and respawns the daemon (detached via
`setsid`) when it is found dead.

It is a **verb dispatcher** (`runSuperviseSubcommand`, `cmd/harmonik/supervise_cmd.go`);
the top-level usage is the `superviseTopUsage` const in the same file. Each verb
lives in its own file under `cmd/harmonik/supervise/`.

### What the watchdog actually does (auto-revive)

`internal/supervise/daemon_watchdog.go`. Two paths in the shim (`RunShim`,
`cmd/harmonik/supervise/shim.go`) start it:

- `--watch-restart` → `runWithSupervisor` runs the watchdog alongside the
  supervisee.
- **no supervisee at all** — `config.json` has an empty `Command` →
  `runWatchdogOnly` runs the watchdog on its own, *regardless of
  `--watch-restart`*. This is the expected state when the operator has dropped
  the flywheel command.

Without `--watch-restart` and *with* a supervisee configured, the shim
exec-replaces itself with the supervisee (`runDirect`) and there is **no
watchdog**. Real defaults (`DaemonWatchdogSpec.applyDefaults`):

| param | default | meaning |
|---|---|---|
| `CheckInterval` | **30s** | how often the daemon socket is probed for liveness |
| `DialTimeout` | **3s** | per-probe connection cap |
| `MaxRevives` | **3** | consecutive failed revivals before giving up (resets to 0 after a confirmed-alive revival; `-1` = unlimited) |
| `ReviveBackoff` | **10s** | poll interval while waiting for a just-revived daemon to bind its socket |
| `ReviveWindow` | **15m** | max wait for socket-bind after a revive (must cover the daemon's `restartBackoffCap` of 10m) |

The revive command is built from the current binary + project (`buildDaemonCmd`,
`cmd/harmonik/supervise/shim.go`): it re-launches the daemon with
**`--no-auto-pull`** (queue-only safe default), `--max-concurrent N` when
configured, and `--default-harness <v>` when `HARMONIK_DEFAULT_HARNESS` is set in
the supervisor's env. The supervisor (under restart-shim) also restarts the
*supervisee* (the Pi/cognition process) on crash, with backoff base **1000ms** /
cap **60000ms** / max **5** restarts (`runWithSupervisor`, defaults applied when
the `config.json` fields are zero).

> **Restart-backoff delays socket-bind — this is expected.** After a rebuild +
> daemon restart the socket can take **≈30s–1m** to appear, and during that
> window `supervise status` / probes report `(no socket)`. The watchdog
> tolerates this because `ReviveWindow` (15m) is sized to cover the daemon's
> boot-backoff — see the `DaemonWatchdogSpec.ReviveWindow` doc comment and the
> `pollUntilAlive` call in `DaemonWatchdog.Run`. Do **not** declare the
> daemon dead from a single snapshot in that window. See the
> "Daemon supervisor auto-revives" operational note.

### Verbs

`start | stop | status | ps | attach | restart | logs | pause | resume | reap`
(the `switch verb` in `runSuperviseSubcommand`). `ps` prints canonical supervisor
process signatures + tmux sessions; `reap` clears dead flywheel orphan sessions
(`start` also auto-reaps at boot). `_shim` is internal (runs inside the pane; not
for operator use).

#### `harmonik supervise start [--project DIR] [--watch-restart] [--require-api-key] [--command CMD ...] | -- CMD ...`
(`RunStart`, usage `startUsage` — `cmd/harmonik/supervise/start.go`)
Probes the daemon socket first, acquires `supervisor.lock` (flock), refuses if a
flywheel session already exists, writes a `config.json` snapshot, and creates the
tmux session running the shim. **`--watch-restart`** interposes the restart-shim
(crash-restart of the supervisee + the daemon watchdog). **`--require-api-key`**
fails closed (exit 1) if no `ANTHROPIC_API_KEY` source resolves (operator env →
gitignored `.env`); without it an empty key is allowed so the holder may auth via
OAuth. The supervisee argv comes from `--command` or after a `--` separator.

#### `harmonik supervise stop [--project DIR]`
(`RunStop`, `cmd/harmonik/supervise/stop.go`) SIGTERM → 10s wait → SIGKILL the
supervisor PID, then **reap the flywheel tmux session child-tree** via
`tmux kill-session` (covered by `TestSupervise_StopReapsFlywheelSession` in
`cmd/harmonik/supervise_reap_hkizs8s_test.go` — stop reaps the child tree, not
just the PID), and remove `supervisor.pid` + sentinel. **Idempotent:** a missing
pidfile exits **0** ("supervisor not running"), not 1. **Note:** stopping the *supervisor*
does not kill an already-revived *daemon* — the daemon is spawned detached
(`setsid`) precisely so a SIGTERM to the pane does not cascade to it.

#### `harmonik supervise status [--project DIR] [--json]`
(`RunStatus` / `buildStatusWithProbe`, `cmd/harmonik/supervise/status.go`)
**File-surface only — does NOT connect to the daemon socket.** Reads
`supervisor.pid` and probes liveness via `kill(pid,0)`; reads `config.json` for
restart-policy metadata; surfaces the cognition loop state (`loop_status` /
`pause_reason`, incl. `budget-paused` / `circuit-tripped`). When the pidfile is
absent or stale it falls back to a **process/tmux signature probe** for a
shell-based revive loop (`hk-keeper.sh` / `hk-supervise.sh`) and reports
`presence_source: "keeper-loop"` rather than "stopped". `--json` emits a
schema-versioned `StatusResult`.

#### `harmonik supervise restart [--project DIR] [--watch-restart]`
(`RunRestart`, `cmd/harmonik/supervise/restart.go`) `stop` → validate
`config.json` parses → `start`. **Re-reads config (does not hot-reload):**
parameter changes take effect only on restart. A *missing* `config.json` is not
fatal — restart cold-starts and `RunStart` writes a fresh one; only a config that
exists and fails to parse aborts (exit 1). This is the standard "deploy a new
binary" step after `go install`.

#### `harmonik supervise attach [--project DIR]`
(`RunAttach`, `cmd/harmonik/supervise/attach.go`) **execve-replaces** the current
process with `tmux attach-session -t harmonik-<project_hash>-flywheel` (so you
get a real terminal, not a subprocess). Returns 1 on tmux-not-found / exec
failure / unresolvable project dir.

#### `harmonik supervise logs [--project DIR] [--lines N]`
(`RunLogs`, `cmd/harmonik/supervise/logs.go`) Runs `tmux capture-pane -p -S -<N>`
on the flywheel session (default `N=200`). The session must exist.

#### `harmonik supervise pause [--project DIR]` / `resume [--project DIR]`
(`RunPause` / `RunResume` → `sendOperatorOp`, `cmd/harmonik/supervise/pause.go`)
These talk to the **daemon over its Unix socket** (`{"op":"operator-pause"}` /
`operator-resume`), not the supervisor. `pause` blocks new dispatch immediately
and lets in-flight runs finish (drain); `resume` re-enables dispatch. Both exit
**17** when the daemon socket is absent or refuses the connection.

### Exit codes (the `EXIT CODES` block of `superviseTopUsage` + per-verb)

| code | meaning | verbs |
|---|---|---|
| `0` | success | all |
| `1` | argument / I/O / operational error | all |
| `2` | unrecognised verb | dispatcher |
| `17` | daemon not running (socket absent / refused) | start, restart, pause, resume |
| `24` | flywheel tmux session already exists (lock free, pane left by a prior shim crash) | start (`ExitCodeFlywheelSessionExists`, `cmd/harmonik/supervise/start.go`) |
| `25` | supervisor already running (`supervisor.lock` held) | start (`ExitCodeSupervisorRunning`, `cmd/harmonik/supervise/start.go`) |

> `RunStart` returns 24 from **two** places: the pre-flight `tmux has-session`
> check (before it writes sentinel or config), and the narrow race where
> `tmux new-session` itself reports "duplicate session". Its own doc comment
> lists only 0/1/17/25 and omits 24 — the top-level `superviseTopUsage` table is
> the complete one. Recover with `harmonik supervise stop` first.

**Cross-ref:** the **keeper** skill is the per-session *context-fill* watcher; it
is a different process from the supervisor. The supervisor revives the *daemon*;
the keeper resets a *Claude session* when its context window fills. Do not
conflate them.

---

## § `harmonik promote` — integration → target-branch promotion (LANDED)

`promote_cmd.go`. **This command is LANDED with TWO modes** (hk-pk3p1, which
reconciles the older hk-gax8v "coming" plan referenced in AGENTS.md). It is the
tool that crosses the integration→main boundary the daemon **never** auto-merges.

### Mode 1 — push-mode: `harmonik promote <sha>...`
(`runPromotePush`, `cmd/harmonik/promote_cmd.go`)
Cherry-picks the given reviewed SHA(s) onto the target branch in a **temp
worktree** rooted at the fetched `origin/<target>` tip, runs a **build gate**
(`go build ./... && go vet ./...`, only when `go.mod` is present in the
worktree), and pushes **race-safely** with up to **3** non-fast-forward rebase
retries (`maxPromotePushAttempts`). The cherry-pick uses `-x` (records
provenance). Each cherry-pick is then amended with a **`Harmonik-Bead-ID:`
trailer** — from `--bead`, else auto-detected from a `(hk-xxx)` parenthetical in
the source commit's subject — which is what lets `harmonik reconcile` auto-close
the bead later; a failed stamp is a warning, not a failure. This formalises the
captain bypass-SOP for landing banked, reviewed-not-pushed commits.

### Mode 2 — PR-mode: `harmonik promote --pr`
(`runPromotePR`, `cmd/harmonik/promote_cmd.go`)
Opens a PR from `--from` (default **`integration`**) onto the target via
`gh pr create --base <target> --head <from>` — **never pushes directly.** Requires
the `gh` GitHub CLI on PATH (else exit 1). `--title` / `--body` pass through. This
is the mode for the human integration→main review step.

`--pr` and positional SHA args are **mutually exclusive**; push-mode requires ≥1
SHA (both checks in `parsePromoteFlags`, `cmd/harmonik/promote_cmd.go`).

### Flags (`promoteUsage` for the help text, `parsePromoteFlags` for what is actually accepted)

| flag | mode | meaning |
|---|---|---|
| `--project DIR` | both | project root (default `$HARMONIK_PROJECT`, else cwd) |
| `--target BRANCH` | both | target branch (default: `branching.yaml` `lands_on`, else `main`) |
| `--bead ID` | push | bead ID stamped as the `Harmonik-Bead-ID` trailer; auto-detected from the commit subject when omitted |
| `--pr` | PR | PR-mode; mutually exclusive with SHA args |
| `--from BRANCH` | PR | head branch for the PR (default `integration`) |
| `--title TEXT` / `--body TEXT` | PR | passthrough to `gh pr create` |
| `--protect-branch BRANCH` | both | **replaces** (does not extend) the `branching.yaml` protect-branch deny-list; repeatable |
| `--dry-run` | both | print planned actions; mutate nothing |

Any other `-`-prefixed argument is an error (exit 1); bare arguments are treated
as SHAs. Note `--protect-branch` is accepted by the parser but **absent from the
`--help` text**.

### Protection gate (fail-closed)

If the resolved target is in the project's `protect_branches`
(`.harmonik/branching.yaml` or `--protect-branch`), **push-mode is refused**
fail-closed (exit 5) with a message directing you to `--pr` (the protection loop
in `runPromoteSubcommand`). This is what enforces the AGENTS.md "work-project
deployment" rule that the daemon must never push a protected `main`: a protected
target can only be promoted via a PR.

### Exit codes (`promote_cmd.go` file header + the `EXIT CODES` block of `promoteUsage`)

| code | meaning |
|---|---|
| `0` | success |
| `1` | argument / flag / config error (incl. `gh` not on PATH in PR-mode) |
| `2` | cherry-pick conflict (push-mode) |
| `3` | build gate failed (push-mode) |
| `4` | push failed, all retries exhausted (push-mode) |
| `5` | push-mode refused: target branch is protected |

**When you use it:** to land a reviewed, banked SHA onto `integration` (push-mode,
in a true daemon lull to avoid racing merges), or to open the
`integration → main` pull request for human review (PR-mode). The daemon never
auto-merges integration→main — `promote` is the tool that does/opens it.
Cross-ref AGENTS.md §"Work-project deployment" → "integration → main is a human
step".

### Salvage pattern: `context_cancelled` run with a committed SHA

When a run fails with `failure_class: context_cancelled` but the implementer
already committed (a `Refs: <bead_id>` commit exists on `run/<run_id>`),
**do not re-dispatch** — the work is done and the SHA is immutable.

```bash
# 1. Find the committed SHA:
git log --oneline run/<run_id> | head -5

# 2. Verify (build + short tests must pass):
git checkout --detach <sha>
go build ./...
go test -short ./...
git checkout -

# 3. Promote race-safely (cherry-pick -x, 3 non-ff retries, build gate):
harmonik promote --project $HARMONIK_PROJECT <sha>

# 4. Close the bead (or let harmonik reconcile do it automatically):
br close <bead_id> --reason "Salvaged: context_cancelled; SHA promoted"
```

**Safety:** the SHA is immutable and carries the `Refs:` trailer; the build gate
runs as normal; this is not a gate-bypass. Byte-identity across two independent
runs (same diff content produced by separate agents) is proof-of-determinism and
makes a single review sufficient for salvage. When the target branch is
protected, use `harmonik promote --pr` instead of push-mode (exit 5 is the
fail-closed signal). See `docs/known-workarounds.md §Salvage-promote` for the
full procedure.

---

## § `harmonik reconcile` — close beads whose work already merged (Cat 3c)

`reconcile.go`. **Operator-facing, on-demand reconciler** for the
"inverse premature-close" drift (Cat 3c, `specs/reconciliation/spec.md §8.6`): a
bead is still `in_progress` even though its implementation already merged to the
target branch.

### What it does (`runReconcileSubcommandIO`, `cmd/harmonik/reconcile.go`)
1. Lists all beads in coarse status `in_progress` (`adapter.ListInFlightBeads`).
   Zero in-flight beads → exits **0** immediately.
2. For each, scans `git log` on the target branch for a commit bearing the
   trailer `Harmonik-Bead-ID: <bead_id>` (`lifecycle.GitMergeCommitScanner`,
   `HasMergeCommitForBead`).
3. If a merge commit is found → **closes the bead** via `adapter.SweepCloseBead`
   (`br close`, Cat 3c auto-resolve). This is a **mutating** command.
4. Reports `closed=N skipped=N failed=N` to stderr (all of reconcile's progress
   output goes to stderr, not stdout).

It overlaps the daemon's own orphan sweep (`RunOrphanSweep`); the race is benign
because `br close` is idempotent. Requires `br` on PATH (else exit 1). The whole
run is under a **5-minute** `context.WithTimeout`.

### Flags (`reconcileUsage` + the parse loop in `runReconcileSubcommandIO`)

| flag | meaning |
|---|---|
| `--project DIR` | project directory (default cwd) |
| `--target-branch BRANCH` | branch to scan for merge commits (default `main`) |
| `--run RUN_ID` | scope the scan to the single in-flight bead tied to that run_id (via the `main` queue ledger; fail-OPEN to a full scan if the queue can't load) |

### Exit codes (`reconcile.go` file header + `reconcileUsage`)

| code | meaning |
|---|---|
| `0` | success — all subsumed beads closed (**zero matches is also success**) |
| `1` | argument / adapter error (e.g. `br` not on PATH, project dir missing) |
| `2` | at least one `br close` failed (partial reconciliation) |

**When you use it:** on demand, when a bead is wedged `in_progress` though its
code clearly landed (blocking a dependent bead from becoming ready). The same
sweep also runs inside the daemon, so manual `reconcile` is mostly for the
operator-triggered case and one-off cleanups.

---

## § `harmonik init` — one-time project bootstrap

`cmd/harmonik/init_cmd.go`. First-time scaffold of a NEW repo for harmonik
(`runInit`).

> **There is no `--target-branch == main` guard in `init` any more.** Earlier
> versions of this skill said `--target-branch` had to equal `main` until
> hk-m8vy2 landed. That guard is gone: `init` passes `--target-branch` straight
> through to `config.yaml` and `branching.yaml`, and the real fail-closed
> enforcement is the **daemon's** boot guard (flag > file > default per WM-005b,
> plus protect-branch / forbid-default-main). `harmonik init --target-branch
> integration` is a supported invocation and is in the command's own examples.

### What it scaffolds (in order, `runInit`)
1. Flag parse — **fail-closed**: an unknown argument prints usage and exits 1, so
   a typo like `--target-branc` cannot silently bootstrap against defaults.
2. Doctor checks (`runDoctorChecks`): project dir exists, it is a git repo, `br`
   on PATH, `harmonik` on PATH. `--doctor` stops here and exits 0.
3. `.harmonik/` subdirs (`mkdirAll`): `events/`, `worktrees/`, `beads-intents/`,
   `comms/`, `crew/`, `keeper/`, `queues/`, `intent/`.
4. `br init --prefix <prefix>` (`runBrInit`) — skipped if `.beads/` exists unless
   `--force`. The default prefix is **derived from the project directory name**
   (`deriveBeadPrefix`), falling back to `hk` only when the name has no usable
   alphanumerics.
5. `.harmonik/config.yaml` (`writeConfigYAML`) — daemon defaults plus a complete,
   uncommented `keeper:` block (harmonik ships no built-in keeper defaults, so
   the generated block is what makes the keeper startable). `remote_control_prefix`
   defaults to the same value passed to `br init --prefix`.
6. `.harmonik/branching.yaml` (`writeBranchingYAML`) — `start_from` / `lands_on`
   (both = target branch), `landing_strategy: squash`.
7. `.harmonik/.gitignore` (`writeHarmonikGitignore`) — excludes runtime files
   (`daemon.pid`, `daemon.sock`, `events/`, `worktrees/`, `cognition/`,
   `beads-intents/`, `queue.json`, `comms/`, `crew/`, `keeper/`, `queues/`,
   `schedules.json.lock`, `review.json`, `review.iter-*.json`).
8. **Fleet skills** (`provisionSkills`) — every skill directory in the binary's
   embedded bundle → `.claude/skills/<skill>/`. Never deletes or overwrites
   sibling skill directories.
9. Scaffolds (`provisionScaffolds`): `AGENT_INDEX.md`, `STATUS.md`. `TASKS.md` is
   retired.
10. Context tiers (`provisionContextTiers`): `.harmonik/context/project.yaml`,
    `captain-lanes.md`, `roadmap.md`, plus `HANDOFF.md` **at the repo root**.
11. Renders the *embedded* `assets/templates/AGENTS.template.md` → `AGENTS.md`
    (`renderAgentsMD`, substitutes `$PROJECT_DIR` / `$TARGET_BRANCH`). Note it is
    read from the embed, not from a `docs/templates/` path on disk.
12. Symlinks `CLAUDE.md` → `AGENTS.md` (`ensureClaudeMDSymlink`).
13. Seeds the goal-keeper job in `.harmonik/schedules.json`
    (`seedGoalKeeperSchedule`) — every-1h backstop.
14. Unless `--no-supervise`: runs `harmonik supervise start --watch-restart`
    (`maybeStartSupervise`) — **non-fatal**; if the daemon isn't up it exits 17
    and init only warns.
15. `--smoke`: post-init sanity checks (`runSmokeTest` — `.harmonik/`, the two
    YAMLs, `AGENTS.md`, and `br list` exits 0).
16. Prints the stale-assets hint when existing managed files are behind the
    running binary — this is how a re-`init` tells you to run `sync-assets`.

### Idempotency
**Each step is skipped when its output artifact already exists**; the `CLAUDE.md`
symlink and `br init` are also skip-if-present. **`--force`** overwrites files
and re-runs `br init --force`. Safe to re-run on a partially-initialized project.

### Flags (`initUsage` + the parse loop in `runInit`)

| flag | meaning |
|---|---|
| `--project DIR` | project directory (default cwd; `$HARMONIK_PROJECT` is **not** consulted) |
| `--target-branch BRANCH` | merge target (default `main`; any branch is accepted) |
| `--prefix PREFIX` | bead-ID prefix for `br init` (default: derived from the project directory name) |
| `--doctor` | run precondition checks only; mutate nothing |
| `--force` | overwrite existing files + reinit the br database |
| `--smoke` | run post-init sanity checks |
| `--no-supervise` | skip the auto `supervise start` |

### Exit codes (`runInitSubcommand` doc comment + `initUsage`)

| code | meaning |
|---|---|
| `0` | success (or `--doctor` all-checks-passed) |
| `1` | unknown argument, precondition failure (no git repo / missing `br` or `harmonik`), or I/O error |

**When you use it:** exactly once, when standing up harmonik on a brand-new repo.
Run `harmonik init --doctor` first to confirm preconditions without mutating, then
`harmonik init` (optionally `--smoke`).

---

## § Quick reference

```bash
# Stand up a NEW project (preconditions first, then bootstrap)
harmonik init --doctor --project /path/to/repo
harmonik init --smoke  --project /path/to/repo

# Deploy a rebuilt binary: rebuild, then restart the supervisor (re-reads config)
go install ./cmd/harmonik
harmonik supervise restart --project $HARMONIK_PROJECT --watch-restart
# expect "(no socket)" for ~30s–1m during restart-backoff — that is normal

# Inspect / pause / resume
harmonik supervise status --project $HARMONIK_PROJECT --json
harmonik supervise logs   --project $HARMONIK_PROJECT --lines 500
harmonik supervise pause  --project $HARMONIK_PROJECT   # drain, block new dispatch
harmonik supervise resume --project $HARMONIK_PROJECT

# Close a bead whose work merged but it's stuck in_progress
harmonik reconcile --project $HARMONIK_PROJECT --target-branch main

# Promote reviewed work
harmonik promote --dry-run abc1234                 # preview push-mode
harmonik promote --target integration abc1234      # land a banked SHA (push-mode)
harmonik promote --pr --from integration --title "Sprint 23"   # open integration→main PR
```

---

## References

- `cmd/harmonik/supervise_cmd.go` — `runSuperviseSubcommand` verb dispatcher,
  `superviseTopUsage`, exit-code table.
- `cmd/harmonik/supervise/{start,stop,status,restart,pause,resume,attach,logs,shim}.go`
  — per-verb behaviour, flags, exit codes 17/24/25, the restart-shim, the
  `buildDaemonCmd` revive argv.
- `internal/supervise/daemon_watchdog.go` — the auto-revive watchdog: 30s probe,
  3s dial, 3-revive cap, 10s revive-backoff, 15m revive-window (covers the
  daemon's 10m boot-backoff = the "(no socket)" window).
- `cmd/harmonik/supervise_reap_hkizs8s_test.go` — `TestSupervise_StopReapsFlywheelSession`
  (stop reaps the flywheel child-tree) and `TestSupervise_StartRefuses_FlywheelSessionExists`
  (start exits 24 on an existing flywheel session).
- `cmd/harmonik/promote_cmd.go` — push-mode (`runPromotePush`) + PR-mode
  (`runPromotePR`), the protection gate (exit 5), exit codes 0–5 (hk-pk3p1).
- `cmd/harmonik/reconcile.go` — Cat 3c reconciler, `Harmonik-Bead-ID` trailer
  scan, exit codes 0/1/2, `--run` scoping.
- `cmd/harmonik/init_cmd.go` — bootstrap steps, idempotency, `--force`,
  `deriveBeadPrefix`, and the file-header note that target-branch enforcement is
  the daemon's boot guard, **not** init's.
- the **keeper** skill (`cmd/harmonik/assets/skills/keeper/SKILL.md`) — the
  per-session context-fill watcher (distinct from the supervisor; it resets a
  Claude session, not the daemon).
- the **harmonik-dispatch** skill — the per-task dispatch loop these lifecycle
  commands sit alongside.
- AGENTS.md §"Work-project deployment" — integration-branch flags,
  `branching.yaml`, and "integration → main is a human step" (which `promote
  --pr` performs).
