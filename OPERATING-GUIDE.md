# Operating Guide

A day-to-day runbook for the person running harmonik. Each task below tells you **when** you'd
do it, **why**, and the **exact commands**. For the full flag reference see
[CLI-REFERENCE.md](CLI-REFERENCE.md); for the integration-branch / branch-protection setup see
[CONFIGURATION.md](CONFIGURATION.md). New here? Start with [QUICKSTART.md](QUICKSTART.md).

> **The one rule that saves you the most grief:** harmonik runs as a long-lived background
> process. When you change harmonik's code, the running process keeps using the *old* build until
> you restart it. A stale build is the single most common reason a fix "didn't take." See
> [Deploy a new build safely](#1-deploy-a-new-build-safely).

> **Before you point harmonik at any repo:** by default the daemon **pushes to `main` on every
> successful task**. If `main` must be protected, configure an integration branch *first* — see
> [CONFIGURATION.md](CONFIGURATION.md). Personal/throwaway repos are fine without it.

---

## A note on "the daemon"

Harmonik runs as one long-lived background process per project, called the **daemon**. It lives
inside a detached **tmux** session so it keeps running after you close your terminal. The daemon
picks up work you submit, hands each task to a Claude session in its own isolated copy of the
repo, reviews the result, merges it, and pushes — then moves to the next.

**Only one daemon per project, ever.** The daemon takes out a lock file on startup. If you try to
start a second one, it refuses and exits with code **5**. If you run a command that talks to the
daemon and there isn't one running, you get exit code **17**. Keep those two numbers in mind —
they tell you almost everything about "is a daemon up or not."

---

## 1. Deploy a new build safely

**When:** any time you change harmonik's own code, or pull updates that include code changes.

**Why:** `go install` only writes a fresh binary to disk. The *running* daemon keeps using the
build it started with. You must restart it to pick up the new build. Skipping this is the #1
cause of "but I already fixed that."

```bash
# 1. Get your local main up to date FIRST.
#    The daemon pushes per-task merges, so your local main usually lags behind.
#    Building from a stale main silently ships a daemon WITHOUT the latest fix.
git -C /Users/gb/github/harmonik fetch
git -C /Users/gb/github/harmonik reset --hard origin/main      # see CAUTION below

# 2. Build and install the new binary.
cd /Users/gb/github/harmonik && go install ./cmd/harmonik

# 3. Restart the daemon so it runs the new binary (see section 2).
```

> **CAUTION — `git reset --hard origin/main` throws away uncommitted local changes.**
> Run it only when your working tree is clean (or you're certain you want to discard whatever's
> there). If you have local edits you care about, commit or stash them first. Also note: if
> *other people or agents* share this machine, announce a rebuild before you do it — a restart
> interrupts whatever the daemon is currently running.

To check which build is on disk:

```bash
harmonik version          # prints the binary now on disk (semver + commit)
```

There's no separate "running daemon version" readout, so the reliable habit is simply:
**rebuild, then restart.** If you restarted after `go install`, the daemon is on the new build.

---

## 2. Start, stop, and restart the daemon

### Start it

**When:** no daemon is running for this project (e.g. after a reboot, or the first time today).

```bash
tmux new-session -d -s harmonik-daemon \
  'harmonik --project /Users/gb/github/harmonik --no-auto-pull --max-concurrent 4'
```

- `--no-auto-pull` — **queue-only mode: the daemon only runs work you explicitly submit.** This is
  now the *default* behavior, so the flag is a back-compat no-op alias — it changes nothing, but
  it's worth keeping in your launch line as documentation of intent. (The opposite, `--auto-pull`,
  lets the daemon drain the whole ledger on its own; that once burned a pile of API credit in a
  couple of hours, which is why queue-only is the default. Don't pass `--auto-pull` unless you mean
  it.)
- `--max-concurrent 4` — how many tasks run at once. **4–5 is the sweet spot on a 10-core
  machine.** Going wider (6+) oversubscribes the CPU and can fill the disk (each task gets its
  own copy of the repo).

Confirm it came up:

```bash
harmonik queue status     # exit 17 = no daemon; anything else = it's up
```

> If a daemon is **already** running and you start another, the second one exits with code **5**
> (the lock is held). That's harmless — just don't expect two.

### A self-healing alternative

If your daemon keeps dying on you, there's a keep-alive wrapper that relaunches it automatically:

```bash
cd /Users/gb/github/harmonik
./scripts/hk-keeper.sh /Users/gb/github/harmonik 4      # [project-dir] [max-concurrent]
```

It runs *outside* tmux, launches the daemon in its own session, strips any inherited API key
(so work bills your subscription, not pay-per-token), and revives the daemon if it disappears.

### Stop it

**When:** you're about to rebuild, or you want everything to go quiet.

```bash
tmux kill-session -t harmonik-daemon
# or, if you're not sure which tmux session it's in:
pkill -f "harmonik --project /Users/gb/github/harmonik"
```

> **Don't delete the daemon's socket file by hand** (`.harmonik/daemon.sock`). The daemon (or the
> keep-alive script) manages it. Removing it under a live daemon causes confusing failures.

### Restart it

**When:** after a rebuild (section 1), or to clear a wedged daemon.

```bash
# Stop, then start.
tmux kill-session -t harmonik-daemon 2>/dev/null
tmux new-session -d -s harmonik-daemon \
  'harmonik --project /Users/gb/github/harmonik --no-auto-pull --max-concurrent 4'
```

If you use the keep-alive script, just `pkill` the daemon and the script revives it. **Give it a
moment** — there's a restart back-off, so a brief "no socket" window right after a restart is
normal, not a failure.

---

## 3. Watch what the daemon is doing

**When:** any time work is in flight and you want live progress, completions, and failures.

### The live event stream

```bash
harmonik subscribe \
  --types run_completed,run_failed,run_stale,heartbeat \
  --heartbeat 60s
```

This attaches to the running daemon and prints one line per event (JSON). A completed task prints
`run_completed`; a failure prints `run_failed` with a `failure_class` (e.g. `no_commit`). The
`--heartbeat 60s` means you'll see a tick every minute even when nothing else is happening, so you
know the stream is still alive. One subscribe sees every task, no matter who submitted it.

### A point-in-time snapshot

```bash
harmonik queue status     # what's queued / running right now
harmonik queue list       # all named queues with worker counts
```

### The raw log (fallback)

There is **no** `daemon.log` file. The daemon writes typed events here:

```bash
tail -F /Users/gb/github/harmonik/.harmonik/events/events.jsonl
```

Use this only if `harmonik subscribe` isn't available for some reason.

---

## 4. Drain or cancel a stuck queue

**When:** a queue is wedged — work submitted but nothing's moving, or a daemon was killed and left
a stale queue behind.

First, figure out whether a daemon is even alive:

```bash
harmonik queue status     # exit 17 = no daemon running
```

### If the daemon is alive but a task is stuck

A task can sit at "launched but never started," or it committed but the daemon is stuck on the next
step. Quick triage:

```bash
# What's in flight, by run id:
harmonik queue status

# Did the work actually get committed? Look in the task's worktree:
git -C /Users/gb/github/harmonik/.harmonik/worktrees/<run_id> log --oneline -3
```

If a commit with a `Refs:` line exists, the work is done and the daemon is stuck downstream
(merge/review/push). Most stuck-after-commit cases now recover on their own within ~30–60 minutes.
If it never recovers, restart the daemon (section 2) and the work will be reconciled on the next
boot.

### If the daemon is dead and left a stale queue

This is what `cancel` is for — it works **without** a live daemon:

```bash
harmonik queue cancel             # archive the leftover queue
harmonik queue cancel --force     # if cancel balks
```

Then start a fresh daemon (section 2) and resubmit whatever still needs doing.

> **Re-dispatching a failed task:** don't just re-add the same task to the *same* queue — it'll be
> treated as already-seen and won't run again. To retry, submit it in a **fresh** queue.

---

## 5. Change concurrency on a running daemon

**When:** you want more throughput, or you're seeing slow builds / disk pressure and need to back
off — without stopping work.

```bash
harmonik queue set-concurrency 5      # raise or lower the live ceiling; no restart needed
```

**Picking a number:** 4–5 is the knee on a 10-core box. Symptoms you've gone too wide: builds
crawl, false "stale" alarms fire, or you hit `no space left on device`. If disk is tight, the
biggest quick reclaim is `go clean -cache` (can free many GB).

---

## 6. Start and stop crews (multi-agent)

**When:** you want several persistent Claude "crew" sessions, each working its own queue in
parallel, instead of one shared queue. The daemon must be running.

```bash
# Start a crew member bound to its own queue, with a mission/handoff file:
harmonik crew start alpha --queue alpha-q --mission /tmp/alpha-handoff.md
#   prints the crew's session id

# See who's registered:
harmonik crew list

# Stop one (and optionally pause its queue so nothing new dispatches):
harmonik crew stop alpha
harmonik crew stop alpha --pause-queue
```

> **Heads-up: crews don't yet auto-recover when their context fills.** When a crew's Claude
> session gets full (around 200k tokens) its pane stops accepting input. The context-watcher
> (section 7) does **not** currently rescue crews. The manual fix is to restart the crew with a
> fresh mission file: `harmonik crew stop <name>` then `harmonik crew start <name> ...`. That
> stop-then-start *is* the way to clear a crew's context.

---

## 7. Enable the keeper (context watcher)

**When:** you want a single managed agent session (e.g. your orchestrator) to be watched so it
doesn't silently run out of context. The keeper tracks how full the session is and warns — or, if
you opt in, hands off and restarts it cleanly.

**Phase 1 (warn-only) is the safe default.** It nags when the session gets full but never touches
it. The handoff-and-restart behavior is gated behind an explicit opt-in flag.

```bash
# Wire the keeper's hooks into your Claude settings (idempotent; backs up settings first):
harmonik keeper enable orchestrator --project /Users/gb/github/harmonik --tmux <pane-target>

# It prints the exact 'harmonik keeper ...' command to run. Run that to start watching.
# Default thresholds: warn at 80% full, act at 90%.

# Check it's healthy / not drifted:
harmonik keeper doctor orchestrator --project /Users/gb/github/harmonik
```

To enable the **live handoff-and-restart** cycle (not just warnings), re-run `enable` with
`--yes-destructive`. Only do this once you've watched the warn-only behavior and trust it.

> If you're about to submit a batch to the daemon, tell the keeper to hold off on any handoff
> while that work is in flight:
> `harmonik keeper set-dispatching orchestrator` before submitting, and
> `harmonik keeper clear-dispatching orchestrator` once it's all done.

---

## 8. Pause work (operator pause)

**When:** you want the daemon to stop *starting new* tasks — to deploy a new build, investigate
something, or just take a breather — without killing in-flight work.

There are two levels of pause:

### Pause the whole daemon

In-flight tasks finish; no new ones start.

```bash
harmonik supervise pause      # stop dispatching new work
harmonik supervise resume     # carry on
```

### Pause a single named queue

Leaves other queues running. Useful with crews (section 6).

```bash
harmonik queue pause alpha-q
harmonik queue resume alpha-q
```

> `supervise pause` is the gentle "everybody hold" button — far better than killing the daemon if
> you just need a quiet moment. Killing the daemon mid-task can leave a half-finished worktree and
> a task marked in-progress.

---

## Troubleshooting

### Exit-code cheat sheet

These are the codes harmonik commands return. The two that matter most day-to-day are **5** and
**17** — they tell you whether a daemon is running.

| Code | Meaning | What to do |
|------|---------|------------|
| **0** | Success | Nothing — it worked. |
| **1** | Validation / argument error | You passed something wrong (bad flag, malformed queue file, bad value). Read the error text and fix the input. |
| **2** | Transport/protocol error, or an unrecognised subcommand | Check the verb you typed; if the verb is right, the daemon connection may be unhealthy — check `queue status`. |
| **5** | Pidfile locked — **a daemon is already running** | Don't start a second one. Use the existing daemon (`harmonik queue status`). |
| **17** | **Daemon not running** (no socket) | Start one (section 2), then retry. |
| 25 | Supervisor already running (`supervise start` only) | The cognition/supervisor process is already up; nothing to do. |

> Codes 0/1/2/17/25 are confirmed from the command help. Code **5** ("pidfile locked — a daemon
> is already running") is confirmed in the source (the pidfile-lock path) and fires when you try
> to launch a second `harmonik --project ...` daemon. One nuance: `harmonik run` no longer collides
> — if a daemon is already up it submits your beads to it instead of exiting 5. Code **25**
> ("supervisor already running") is confirmed in the source and appears only on `harmonik supervise
> start`. See [CLI-REFERENCE.md](CLI-REFERENCE.md) for any per-command specifics.

### Symptom → likely cause → fix

| Symptom | Likely cause | Fix |
|---------|--------------|-----|
| A bug you fixed is still happening | Daemon is running the **old build** | `go install ./cmd/harmonik`, then restart the daemon (section 1 & 2). Rebuild from up-to-date `main`, not stale local code. |
| Any `harmonik queue ...` command returns **exit 17** | No daemon running | Start the daemon (section 2). |
| `harmonik` won't start, says `$TMUX is not set` | You launched it from a plain shell, not inside tmux | Wrap it: `tmux new-session -d -s harmonik-daemon 'harmonik ...'`. The daemon always runs inside tmux. |
| Started a daemon, got **exit 5** | A daemon is already running (lock held) | That's fine — there should only be one. Use it. |
| API credit draining fast for no clear reason | An API key in the repo's `.env` got inherited by spawned sessions, billing pay-per-token | Remove `ANTHROPIC_API_KEY` / `ANTHROPIC_AUTH_TOKEN` / `CLAUDE_CODE_OAUTH*` from any repo `.env` the daemon can read. Don't pass `--auto-pull` (queue-only is the default). The keep-alive script strips these keys for you. |
| Builds crawling, false "stale" alarms, `no space left on device` | Concurrency too high — too many parallel repo copies and `go` builds | Drop concurrency live: `harmonik queue set-concurrency 4`. Reclaim disk: `go clean -cache`. |
| Submitted work but **nothing starts** — failure reported with no task actually launching | The task is blocked by an open dependency (often an open epic it was linked to) | Don't link tasks to an open epic as a dependency. Attach via a label instead. Check with `br show <id>` for `blocked_by` entries pointing at an open item. |
| A task is stuck "launched" but never really ran | Spawn slot didn't free up (intermittent under high concurrency) | Run a touch narrower (`--max-concurrent 4`). Restart the daemon to clear it; the work reconciles on reboot. |
| Daemon dies repeatedly / queue stalls (exit 17 keeps coming back) | Daemon crash-looping | Use the keep-alive script `./scripts/hk-keeper.sh` (section 2) — it auto-revives the daemon. |
| Merge conflict on `.beads/issues.jsonl` when a task lands | The bead ledger forked at task-start | `git checkout --theirs .beads/issues.jsonl`. (Mostly handled automatically now.) |
| A crew session stops responding to input | Its context filled up (~200k tokens) | Restart it: `harmonik crew stop <name>` then `harmonik crew start <name> --mission <fresh-file>`. The keeper does not rescue crews yet. |
| Your shell's working directory suddenly seems "invalid" or `br`/`harmonik` can't find the project | The daemon removed a worktree you'd `cd`'d into | Always operate from the repo root. Use `git -C /Users/gb/github/harmonik ...` instead of `cd`-ing into worktrees. |
| Crew/agent never starts; its pane just sits there | The login shell is blocked on an interactive prompt (e.g. a shell-update `[Y/n]`) | Disable the interactive prompt in your shell config, or launch the session with a no-rc shell. Check the pane contents, not the event log, to spot this. |

### When you really do need to kill a wedged daemon

If a daemon is hung and won't recover:

```bash
# 1. Confirm what's stuck (by run id), and whether work was committed:
harmonik queue status
git -C /Users/gb/github/harmonik/.harmonik/worktrees/<run_id> log --oneline -3

# 2. Kill the daemon:
pkill -f "harmonik --project /Users/gb/github/harmonik"

# 3. Clear any leftover queue (works with no daemon):
harmonik queue cancel

# 4. Start a fresh daemon (section 2). It reconciles in-flight work on boot.
```

If a task had already committed (you saw a `Refs:` line in step 1), that work is recoverable —
the daemon's boot-time reconciliation, or `harmonik reconcile`, will pick it up.

---

## See also

- [QUICKSTART.md](QUICKSTART.md) — first-run, end to end.
- [CLI-REFERENCE.md](CLI-REFERENCE.md) — every command and flag in detail.
- [CONFIGURATION.md](CONFIGURATION.md) — branch protection / integration-branch setup, config keys.
- [CONCEPTS.md](CONCEPTS.md) — beads, runs, queues, worktrees, the review loop.
- [OVERVIEW.md](OVERVIEW.md) — what harmonik is and the design behind it.
