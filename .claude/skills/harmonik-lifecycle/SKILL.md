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
  - .claude/skills/keeper/SKILL.md
  - AGENTS.md §"Work-project deployment"
---

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

Three of them resolve `--project` the same way (explicit flag → `$HARMONIK_PROJECT`
→ cwd); always pass `--project $HARMONIK_PROJECT` explicitly from an
orchestrator to avoid CWD drift (a known hazard with worktree sub-agents).

---

## § `harmonik supervise` — the supervisor (auto-revive)

`supervise` manages a long-lived **supervisor** process that runs inside a
detached tmux session named `harmonik-<project_hash>-flywheel`, with
`remain-on-exit on`. The supervisor's load-bearing job (under `--watch-restart`)
is **auto-revival of the harmonik daemon**: a `DaemonWatchdog` probes the
daemon's Unix socket on a fixed interval and respawns the daemon (detached via
`setsid`) when it is found dead.

It is a **verb dispatcher** (`supervise_cmd.go:24-67`); the top-level usage is
`supervise_cmd.go:69`. Each verb lives in its own file under
`cmd/harmonik/supervise/`.

### What the watchdog actually does (auto-revive)

`internal/supervise/daemon_watchdog.go`. Only active when the supervisor was
started with `--watch-restart` (`shim.go:143-203`, `runWithSupervisor`). Real
defaults (`daemon_watchdog.go:61-78`):

| param | default | meaning |
|---|---|---|
| `CheckInterval` | **30s** | how often the daemon socket is probed for liveness |
| `DialTimeout` | **3s** | per-probe connection cap |
| `MaxRevives` | **3** | consecutive failed revivals before giving up (resets to 0 after a confirmed-alive revival; `-1` = unlimited) |
| `ReviveBackoff` | **10s** | poll interval while waiting for a just-revived daemon to bind its socket |
| `ReviveWindow` | **15m** | max wait for socket-bind after a revive (must cover the daemon's `restartBackoffCap` of 10m) |

The revive command is built from the current binary + project
(`shim.go:223-233`, `buildDaemonCmd`): it re-launches the daemon with
**`--no-auto-pull`** (queue-only safe default) and `--max-concurrent N` when
configured. The supervisor (under restart-shim) also restarts the *supervisee*
(the Pi/cognition process) on crash, with backoff base **1000ms** / cap
**60000ms** / max **5** restarts (`shim.go:151-179`, from `config.json`).

> **Restart-backoff delays socket-bind — this is expected.** After a rebuild +
> daemon restart the socket can take **≈30s–1m** to appear, and during that
> window `supervise status` / probes report `(no socket)`. The watchdog
> tolerates this because `ReviveWindow` (15m) is sized to cover the daemon's
> boot-backoff (`daemon_watchdog.go:39-43,186-201`). Do **not** declare the
> daemon dead from a single snapshot in that window. See the
> "Daemon supervisor auto-revives" operational note.

### Verbs

`start | stop | status | attach | restart | logs | pause | resume`
(`supervise_cmd.go:41-66`). `_shim` is internal (runs inside the pane; not for
operator use).

#### `harmonik supervise start [--project DIR] [--watch-restart] [--require-api-key] [--command CMD ...] | -- CMD ...`
(`start.go:41-232`, usage `start.go:308`)
Probes the daemon socket first, acquires `supervisor.lock` (flock), refuses if a
flywheel session already exists, writes a `config.json` snapshot, and creates the
tmux session running the shim. **`--watch-restart`** interposes the restart-shim
(crash-restart of the supervisee + the daemon watchdog). **`--require-api-key`**
fails closed (exit 1) if no `ANTHROPIC_API_KEY` source resolves (operator env →
gitignored `.env`); without it an empty key is allowed so the holder may auth via
OAuth. The supervisee argv comes from `--command` or after a `--` separator.

#### `harmonik supervise stop [--project DIR]`
(`stop.go:25-105`) SIGTERM → 10s wait → SIGKILL the supervisor PID, then
**reap the flywheel tmux session child-tree** via `tmux kill-session` (verified
by `supervise_reap_hkizs8s_test.go:24` — stop reaps the child tree, not just the
PID), and remove `supervisor.pid` + sentinel. **Note:** stopping the *supervisor*
does not kill an already-revived *daemon* — the daemon is spawned detached
(`setsid`) precisely so a SIGTERM to the pane does not cascade to it.

#### `harmonik supervise status [--project DIR] [--json]`
(`status.go:43-151`) **File-surface only — does NOT connect to the daemon
socket.** Reads `supervisor.pid` and probes liveness via `kill(pid,0)`; reads
`config.json` for restart-policy metadata; surfaces the cognition loop state
(`loop_status` / `pause_reason`, incl. `budget-paused` / `circuit-tripped`).
`--json` emits a schema-versioned `StatusResult`.

#### `harmonik supervise restart [--project DIR] [--watch-restart]`
(`restart.go:21-68`) `stop` → re-read `config.json` (validate it parses) →
`start`. **Re-reads config (does not hot-reload):** parameter changes take effect
only on restart. This is the standard "deploy a new binary" step after
`go install`.

#### `harmonik supervise attach [--project DIR]`
(`attach.go:24-64`) **execve-replaces** the current process with `tmux
attach-session -t harmonik-<project_hash>-flywheel` (so you get a real terminal,
not a subprocess). Returns 1 only on tmux-not-found / exec failure.

#### `harmonik supervise logs [--project DIR] [--lines N]`
(`logs.go:22-79`) Runs `tmux capture-pane -p -S -<N>` on the flywheel session
(default `N=200`). The session must exist.

#### `harmonik supervise pause [--project DIR]` / `resume [--project DIR]`
(`pause.go:33-70`, `resume.go:30-67`) These talk to the **daemon over its Unix
socket** (`{"op":"operator-pause"}` / `operator-resume`), not the supervisor.
`pause` blocks new dispatch immediately and lets in-flight runs finish (drain);
`resume` re-enables dispatch. Both exit **17** when the daemon is not running.

### Exit codes (`supervise_cmd.go:84-89` + per-verb)

| code | meaning | verbs |
|---|---|---|
| `0` | success | all |
| `1` | argument / I/O / operational error | all |
| `2` | unrecognised verb | dispatcher |
| `17` | daemon not running (socket absent / refused) | start, restart, pause, resume |
| `24` | flywheel tmux session already exists (lock free, pane left by a prior shim crash) | start (`ExitCodeFlywheelSessionExists`, `start.go:29`) |
| `25` | supervisor already running (`supervisor.lock` held) | start (`ExitCodeSupervisorRunning`, `start.go:24`) |

> The top-level usage string lists 0/1/2/17/25 but **not 24**; 24 is real and
> emitted by `RunStart` when a prior shim left a `remain-on-exit` pane around
> (`start.go:139-152,203-214`). Recover with `harmonik supervise stop` first.

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
(`runPromotePush`, `promote_cmd.go:241-377`)
Cherry-picks the given reviewed SHA(s) onto the target branch in a **temp
worktree** rooted at the fetched `origin/<target>` tip, runs a **build gate**
(`go build ./... && go vet ./...`, only when `go.mod` is present), and pushes
**race-safely** with up to **3** non-fast-forward rebase retries
(`maxPromotePushAttempts = 3`, `promote_cmd.go:82`). The cherry-pick uses `-x`
(records provenance). This formalises the captain bypass-SOP for landing banked,
reviewed-not-pushed commits.

### Mode 2 — PR-mode: `harmonik promote --pr`
(`runPromotePR`, `promote_cmd.go:379-410`)
Opens a PR from `--from` (default **`integration`**) onto the target via
`gh pr create --base <target> --head <from>` — **never pushes directly.** Requires
the `gh` GitHub CLI on PATH (else exit 1). `--title` / `--body` pass through. This
is the mode for the human integration→main review step.

`--pr` and positional SHA args are **mutually exclusive**; push-mode requires ≥1
SHA (`parsePromoteFlags`, `promote_cmd.go:222-229`).

### Flags (`promote_cmd.go:52-59`, parsed `174-237`)

| flag | mode | meaning |
|---|---|---|
| `--project DIR` | both | project root (default cwd / `$HARMONIK_PROJECT`) |
| `--target BRANCH` | both | target branch (default: `branching.yaml` `lands_on`, else `main`) |
| `--pr` | PR | PR-mode; mutually exclusive with SHA args |
| `--from BRANCH` | PR | head branch for the PR (default `integration`) |
| `--title TEXT` / `--body TEXT` | PR | passthrough to `gh pr create` |
| `--protect-branch BRANCH` | both | operator override of the protect-branch deny-list (repeatable) |
| `--dry-run` | both | print planned actions; mutate nothing |

### Protection gate (fail-closed)

If the resolved target is in the project's `protect_branches`
(`.harmonik/branching.yaml` or `--protect-branch`), **push-mode is refused**
fail-closed (exit 5) with a message directing you to `--pr`
(`promote_cmd.go:140-150`). This is what enforces the AGENTS.md "work-project
deployment" rule that the daemon must never push a protected `main`: a protected
target can only be promoted via a PR.

### Exit codes (`promote_cmd.go:23-31`, usage `65-71`)

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

---

## § `harmonik reconcile` — close beads whose work already merged (Cat 3c)

`reconcile.go`. **Operator-facing, on-demand reconciler** for the
"inverse premature-close" drift (Cat 3c, `specs/reconciliation/spec.md §8.6`): a
bead is still `in_progress` even though its implementation already merged to the
target branch.

### What it does (`reconcile.go:171-236`)
1. Lists all beads in coarse status `in_progress` (`adapter.ListInFlightBeads`).
2. For each, scans `git log` on the target branch for a commit bearing the
   trailer `Harmonik-Bead-ID: <bead_id>` (`lifecycle.GitMergeCommitScanner`).
3. If a merge commit is found → **closes the bead** via `br close` (Cat 3c
   auto-resolve). This is a **mutating** command (it writes bead closes).
4. Reports `closed=N skipped=N failed=N` to stderr.

It overlaps the daemon's own orphan sweep (`RunOrphanSweep`); the race is benign
because `br close` is idempotent. Requires `br` on PATH (else exit 1). Has a 5-min
internal timeout (`reconcile.go:168`).

### Flags (`reconcile.go:73-79`)

| flag | meaning |
|---|---|
| `--project DIR` | project directory (default cwd) |
| `--target-branch BRANCH` | branch to scan for merge commits (default `main`) |
| `--run RUN_ID` | scope the scan to the single in-flight bead tied to that run_id (via the `main` queue ledger; fail-OPEN to a full scan if the queue can't load) |

### Exit codes (`reconcile.go:80-83`)

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

`init_cmd.go`. First-time scaffold of a NEW repo for harmonik (`runInit`,
`init_cmd.go:63-203`).

### What it scaffolds (in order, `init_cmd.go:140-202`)
1. Precondition / doctor checks: git repo present, `br` and `harmonik` on PATH.
2. **FAIL-CLOSED guard:** `--target-branch` MUST equal `main` until hk-m8vy2
   (merge-retarget) lands — any other value exits 1 (`init_cmd.go:129-138`). So
   `init` cannot yet stand up an integration-branch deployment directly; use the
   integration-branch flags / `branching.yaml` on the daemon for that (AGENTS.md
   §"Work-project deployment"), and promote via `harmonik promote --pr`.
3. `.harmonik/` subdirs: `events/`, `worktrees/`, `beads-intents/`.
4. `br init --prefix <prefix>` (default prefix `hk`) — skipped if `.beads/`
   exists unless `--force`.
5. `.harmonik/config.yaml` — daemon defaults (`target_branch`,
   `max_concurrent: 4`, `workflow_mode: review-loop`).
6. `.harmonik/branching.yaml` — `start_from` / `lands_on` (= target branch),
   `landing_strategy: squash`.
7. `.harmonik/.gitignore` — excludes runtime files (`daemon.pid`, `daemon.sock`,
   `events/`, `worktrees/`, `cognition/`, `beads-intents/`, `queue.json`,
   `comms/`).
8. Renders `docs/templates/AGENTS.template.md` → `AGENTS.md` (substitutes
   `$PROJECT_DIR`, `$TARGET_BRANCH`).
9. Symlinks `CLAUDE.md` → `AGENTS.md`.
10. Unless `--no-supervise`: runs `harmonik supervise start --watch-restart`
    (non-fatal — if the daemon isn't up yet it exits 17 and init just warns).
11. `--smoke`: post-init sanity checks (`.harmonik/`, the two YAMLs, `AGENTS.md`,
    `br list` exits 0).

### Idempotency
**Each step is skipped when its output artifact already exists**
(`init_cmd.go:30-33`); the `CLAUDE.md` symlink and `br init` are also
skip-if-present. **`--force`** overwrites config files and re-runs `br init
--force`. Safe to re-run on a partially-initialized project.

### Flags (`init_cmd.go:534-542`)

| flag | meaning |
|---|---|
| `--project DIR` | project directory (default cwd) |
| `--target-branch BRANCH` | merge target (default `main`; **only `main` allowed** until hk-m8vy2) |
| `--prefix PREFIX` | bead-ID prefix for `br init` (default `hk`) |
| `--doctor` | run precondition checks only; mutate nothing |
| `--force` | overwrite existing config + reinit the br database |
| `--smoke` | run post-init sanity checks |
| `--no-supervise` | skip the auto `supervise start` |

### Exit codes (`init_cmd.go:53-57`, `560-562`)

| code | meaning |
|---|---|
| `0` | success (or `--doctor` all-checks-passed) |
| `1` | argument, precondition (no git repo / missing `br`/`harmonik`), or I/O error; **also `--target-branch != main`** |

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

- `cmd/harmonik/supervise_cmd.go` — verb dispatcher, top usage, exit-code table.
- `cmd/harmonik/supervise/{start,stop,status,restart,pause,resume,attach,logs,shim}.go`
  — per-verb behaviour, flags, exit codes 17/24/25, the restart-shim, the
  `buildDaemonCmd` revive argv.
- `internal/supervise/daemon_watchdog.go` — the auto-revive watchdog: 30s probe,
  3s dial, 3-revive cap, 10s revive-backoff, 15m revive-window (covers the
  daemon's 10m boot-backoff = the "(no socket)" window).
- `cmd/harmonik/supervise_reap_hkizs8s_test.go` — stop reaps the flywheel
  child-tree; start exits 24 on an existing flywheel session.
- `cmd/harmonik/promote_cmd.go` — push-mode (`runPromotePush`) + PR-mode
  (`runPromotePR`), the protection gate (exit 5), exit codes 0–5 (hk-pk3p1).
- `cmd/harmonik/reconcile.go` — Cat 3c reconciler, `Harmonik-Bead-ID` trailer
  scan, exit codes 0/1/2, `--run` scoping.
- `cmd/harmonik/init_cmd.go` — bootstrap steps, idempotency, `--force`,
  fail-closed `--target-branch == main` guard (hk-m8vy2).
- `.claude/skills/keeper/SKILL.md` — the per-session context-fill watcher
  (distinct from the supervisor; reset a Claude session, not the daemon).
- `.claude/skills/harmonik-dispatch/SKILL.md` — the per-task dispatch loop these
  lifecycle commands sit alongside.
- AGENTS.md §"Work-project deployment" — integration-branch flags,
  `branching.yaml`, and "integration → main is a human step" (which `promote
  --pr` performs).
