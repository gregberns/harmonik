# Daemon redeploy runbook (in-place binary swap on the running box)

Replace the **live daemon binary** with one built from a target commit, on the same
machine, without losing in-flight work. This is distinct from cutting a semver
release (`docs/known-workarounds.md` §Release process) — that publishes a GitHub
release; this swaps the binary the supervisor revives.

Use it for: shipping a fix to the running fleet, picking up `origin/main`, recovering
from a stale-binary daemon.

## How the chain actually works (read once)

- The supervisor (`harmonik supervise _shim …`) revives the daemon from **its own
  `os.Executable()`** (`cmd/harmonik/supervise/shim.go`). So the revive target is
  whatever file path the supervisor process itself runs from — here
  `/Users/gb/go/bin/harmonik`. To deploy a new binary you must replace that file
  **and** get the daemon to actually cycle.
- `harmonik supervise restart` restarts the **supervisor** (re-reads `config.json`,
  re-resolves `os.Executable()`). It does **not**, by itself, kill a daemon that has
  orphaned to `PPID 1` — the new supervisor just adopts the still-running old daemon
  via the pidfile. **You must terminate the daemon** so the watchdog respawns it from
  the new binary. (`go install` + `pkill` of the daemon alone is *not* enough if the
  supervisor still runs the old on-disk binary.)
- After respawn the watchdog waits up to **15 min** for the daemon to bind the
  **socket** (it checks the socket, not the pid — expect `(no socket)` for ~30–60s;
  do not declare it dead). Then a **30s health window**: survive it and the watchdog
  **pins the new binary as `…/harmonik.last-good`**. Under heavy load this window can
  false-revert, so deploy from a *paused/quiet* daemon.

### On-disk chain
- Active binary: `/Users/gb/go/bin/harmonik`
- Last-good snapshot: `/Users/gb/go/bin/harmonik.last-good`
- Pointer file: `.harmonik/state/last-good-binary`
- Supervisor session: tmux `harmonik-<projecthash>-flywheel`; `.harmonik/supervisor.{pid,lock}`
- Daemon runtime: `.harmonik/daemon.sock`, `.harmonik/daemon.pid` — **never `rm` the live socket**.

## Runbook

```bash
cd /Users/gb/github/harmonik

# 0. Tree must be clean of TRACKED-file edits that overlap the target commit
#    (daemon runs `git checkout --` on main and reverts uncommitted tracked edits).
git fetch origin
git diff --name-only HEAD                                   # what's dirty
comm -12<(git diff --name-only HEAD|sort)<(git diff --name-only HEAD origin/main|sort)  # overlap MUST be empty
git merge --ff-only origin/main                             # advance main to the target commit
git rev-parse HEAD                                          # note the deploy SHA

# 1. Quiet the daemon so the 30s health-window can't false-revert under load
harmonik supervise pause --project "$PWD"

# 2. Keep a guaranteed old-binary rollback point, then build the stamped binary
cp -p /Users/gb/go/bin/harmonik /Users/gb/go/bin/harmonik.pre-$(date +%Y%m%d)
make install-harmonik                                       # go install -ldflags "-X main.commitHash=$(git rev-parse HEAD)"
/Users/gb/go/bin/harmonik --version                        # confirm commit == deploy SHA

# 3. Restart the SUPERVISOR so its os.Executable() re-resolves to the new file
harmonik supervise restart --project "$PWD" --watch-restart

# 4. Cycle the daemon: it orphaned to PPID 1, so SIGTERM it and let the watchdog
#    revive it FROM THE NEW BINARY. Find the live daemon pid first.
pgrep -fl 'harmonik --project '"$PWD"' --no-auto-pull'      # -> <OLD_DAEMON_PID>
kill -TERM <OLD_DAEMON_PID>

# 5. Wait for revival WITHOUT polling `harmonik status` (bare `harmonik status` tries
#    to START a daemon and will hang / contend with the reviving one). Watch files:
#    - socket reappears:      ls .harmonik/daemon.sock
#    - new daemon pid:        pgrep -f 'harmonik --project '"$PWD"' --no-auto-pull'
#    - watchdog progress:     harmonik supervise logs --project "$PWD" --lines 20
```

## Verify (authoritative)

```bash
# a. New daemon_started carries the deploy SHA:
grep '"daemon_started"' .harmonik/events/events.jsonl | tail -1   # binary_commit_hash == deploy SHA

# b. It survived the health window and was pinned (in supervise logs):
#    "daemon-watchdog: pinned last-good binary"

# c. Daemon actually SERVES over the socket (needs a live daemon):
harmonik queue list

# d. Resume dispatch (pause from step 1 persists across the revive):
harmonik supervise resume --project "$PWD"
```

## Mark the deploy on the commit

The established convention is a lightweight `daemon-YYYYMMDD-NN` git tag on the
deployed commit (the audit record is also the `daemon_started.binary_commit_hash`
event). A semver release is *not* required for an unscheduled redeploy.

```bash
git tag daemon-$(date +%Y%m%d)-01 <deploy-SHA>
git push origin daemon-$(date +%Y%m%d)-01
```

## After deploy

- **Asset skew:** a new binary often logs `asset-skew: project assets behind running
  binary` and notifies the captain to run **sync-assets** — embedded skills/scripts
  are stamped into the binary and the on-disk copies lag. Run the sync so crews load
  the matching assets.

## Rollback

```bash
harmonik release rollback --project "$PWD"                  # restores from .last-good
harmonik supervise restart --project "$PWD" --watch-restart
```

Caveat: once the new binary survives 30s it becomes `.last-good`, so `release
rollback` then targets the *new* binary. For a guaranteed old-binary rollback use the
`harmonik.pre-<date>` copy from step 2 (`mv` it back over `/Users/gb/go/bin/harmonik`
+ `supervise restart`).

## Pitfalls

1. `supervise restart` alone does **not** redeploy — the orphaned daemon keeps running
   the old binary until you SIGTERM it (step 4).
2. **Don't poll `harmonik status`** during revival — it spawns a transient daemon and
   contends with the reviving one (it hung a poll loop and likely killed an early
   revive attempt during the 2026-06-30 deploy). Use file/event checks instead.
3. Clean tree is mandatory before `ff` — the daemon reverts uncommitted tracked edits.
4. `supervise restart` re-reads `config.json` (no hot-reload) — concurrency/flags only
   change on restart.
5. Expect `(no socket)` for ~30–60s after the daemon cycle; that's the bind window,
   not death.

_Source: 2026-06-30 redeploy (`f1dbdfa8`/`448c039d` → `7a9bf2e5`, tag
`daemon-20260630-01`). Code: `cmd/harmonik/supervise/shim.go`,
`internal/release/lastgood.go`, `cmd/harmonik/release_cmd.go`,
`specs/release-pipeline.md §7`, `Makefile` (`install-harmonik`)._
