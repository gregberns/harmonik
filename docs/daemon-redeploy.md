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

## GATE 0 (MANDATORY, operator-mandated 2026-07-05): end-to-end tests on the new code

**Do NOT proceed to the runbook until this passes.** A green unit suite is NOT this gate, and cycling the live daemon to watch a canary is NOT this gate (never test on the primary daemon — see orchestrator-rules §"PRE-DEPLOY END-TO-END TEST GATE").

1. For the behavior this deploy changes, ADD at least one **end-to-end test that reproduces the daemon's real launch path in ISOLATION** — ephemeral worktree / stub HTTP server / throwaway repo, exercising the actual argv+env+sandbox-wrap+models.json+commit path, not a mock of the thing under test.
2. The test must prove the fix WORKS end-to-end (not just that a gate returns the right value) AND that neighbouring paths don't regress.
3. Start simple + focused on the exact bug; breadth accretes every deploy. Every deploy leaves behind ≥1 more real e2e test than it found.
4. Run new + existing e2e tests GREEN, isolated from the live daemon, BEFORE `make install-harmonik`. No e2e coverage of the changed behavior → no ship.

If exercising the change needs the live daemon, the missing artifact is the harness — build it (`codename:daemon-testbed`, epic `hk-zk0v2`), don't test in prod.

## Runbook

```bash
cd /Users/gb/github/harmonik

# 0. Tree must be clean of TRACKED-file edits that overlap the target commit
#    (daemon runs `git checkout --` on main and reverts uncommitted tracked edits).
git fetch origin
git diff --name-only HEAD                                   # what's dirty
comm -12<(git diff --name-only HEAD|sort)<(git diff --name-only HEAD origin/main|sort)  # overlap MUST be empty
git merge --ff-only origin/main                             # advance main to the target commit
DEPLOY_SHA="$(git rev-parse HEAD)"; echo "deploy SHA: $DEPLOY_SHA"

# 1. Quiet the daemon so the 30s health-window can't false-revert under load
harmonik supervise pause --project "$PWD"

# 2. Keep a guaranteed old-binary rollback point, then build the stamped binary
#    FROM A CLEAN THROWAWAY CLONE — not from this working tree.
#    Step 0 deliberately tolerates dirty tracked files that don't overlap the
#    target commit. `go build` stamps vcs.modified=true if ANY tracked file is
#    dirty, so a working-tree build makes the step-2b gate report contains-dirty
#    (exit 3) on the normal path — the runbook would contradict itself. The clone
#    is what makes exit 0 reachable. (Measured 2026-07-22: the deployed binary
#    stamps vcs.modified=false at 392ba0833 while the working tree it was
#    deployed from was dirty — a working-tree build would have stamped true.)
cp -p /Users/gb/go/bin/harmonik /Users/gb/go/bin/harmonik.pre-$(date +%Y%m%d)
BUILD_DIR="$(mktemp -d)/harmonik"
git clone --quiet "$PWD" "$BUILD_DIR"                       # committed state only
git -C "$BUILD_DIR" checkout --quiet "$DEPLOY_SHA"
git -C "$BUILD_DIR" status --porcelain                      # MUST print nothing
make -C "$BUILD_DIR" install-harmonik                       # go install -ldflags "-X main.commitHash=$(git rev-parse HEAD)"

# 2b. SWAP GATE (authoritative — see "Which fix is in this binary?" below).
#     FAILS CLOSED. It requires BOTH exit 0 AND a literal `status:   contains`
#     line: a harmonik older than 392ba0833 ignores these flags, prints only its
#     version line and exits 0, so the exit code alone verifies NOTHING. That
#     case is live after a rollback, a failed step 2, or a stale PATH entry.
#     Step 3 is chained onto the gate, so a failure stops the paste there.
#     Steps 4-5 are NOT chained: they carry a literal <OLD_DAEMON_PID>
#     placeholder and cannot be blind-pasted anyway.
swap_gate() {
  local out rc
  out="$(/Users/gb/go/bin/harmonik version --binary /Users/gb/go/bin/harmonik --contains "$DEPLOY_SHA" 2>&1)"; rc=$?
  printf '%s\n' "$out"
  [ "$rc" -eq 0 ] || { echo "GATE FAIL (exit $rc) — do NOT swap"; return 1; }
  printf '%s\n' "$out" | grep -qx 'status:   contains' || {
    echo "GATE FAIL: exit 0 with no 'status:   contains' line — this harmonik predates the check and ignored the flags"; return 1; }
  echo "GATE PASS: installed binary carries $DEPLOY_SHA, built clean"
}
swap_gate || echo "STOP — do NOT run steps 3-5"

# 3. Restart the SUPERVISOR so its os.Executable() re-resolves to the new file
#    (guarded: the gate is read-only and idempotent, so re-running it here makes
#     step 3 unable to execute without a pass)
swap_gate && harmonik supervise restart --project "$PWD" --watch-restart

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

## Which fix is in this binary? (the ONE authoritative check)

Go stamps `vcs.revision` / `vcs.modified` into every binary it builds. That stamp —
not symbols, not strings — is the answer to "does this binary contain fix X":

```bash
harmonik version --binary /Users/gb/go/bin/harmonik                    # revision + dirty flag
harmonik version --binary /Users/gb/go/bin/harmonik --contains <SHA>   # ancestry gate
harmonik version --binary … --contains <SHA> --json                    # machine-readable
```

| status | exit | means |
|---|---|---|
| `contains` | 0 | SHA is an ancestor **and** the tree was clean — **the only ship-safe result** |
| `revision` | 0 | no `--contains` asked; revision reported |
| `missing` | 1 | SHA is not an ancestor — the binary predates the fix |
| `usage-error` | 2 | bad flags / unreadable binary / not a git repo / a `--contains` SHA this repo does not have |
| `contains-dirty` | 3 | ancestor, but `vcs.modified=true` — necessary, **not sufficient** |
| `no-build-info` | 4 | not a Go binary, or build info stripped |
| `no-vcs-stamp` | 4 | built with `-buildvcs=false` / outside a worktree |
| `unknown-revision` | 4 | the revision is not in this repo (shallow clone / rebased away) |

**Exit 0 alone is not the answer.** `--help` and status `revision` also exit 0, and any
harmonik built before `392ba0833` ignores these flags entirely: it prints its version line
and exits 0 without checking anything. Always require the `status:   contains` line as well —
that is what the step-2b gate does. `usage-error` is a JSON-only token; in human mode an
exit-2 failure is prose on stderr.

**Why not `strings <binary> | grep <token>` or `go tool nm | grep <symbol>`.** Both probe an
*incidental artefact* of one particular fix. A literal or symbol can be renamed, inlined or
dropped while the fix is present; it can survive a revert while the fix is gone; and a fix
that adds no new literal or symbol at all leaves nothing to probe. So the trick must be
reinvented per fix, and for some fixes it cannot be invented. The `vcs.revision` stamp
answers the question actually being asked — *which source revision is this binary* — the same
way for every fix.

Note it is not that `strings` "always lies". For `hk-9hvr0` it happened to be a correct
discriminator, because that fix *deleted* a string constant. Measured 2026-07-22:
`strings -a … | grep -c harmonik-input` → **1** on `/Users/gb/go/bin/harmonik.pre-hk9hvr0-20260722`
(revision `eb2b4f1a`, which really does declare `const inputBufferName = "harmonik-input"`)
and **0** on the post-fix binary. Being right about one fix is exactly what does not
generalise.

**Ancestry has two blind spots** — it answers "was this commit ever merged into the binary's
history", not "is this code in the binary". Both were reproduced:

- **Revert → false `contains`.** In a throwaway repo, reverting commit F leaves F an ancestor
  of HEAD (`git merge-base --is-ancestor` exits 0) while F's file is gone from the tree — a
  revert appends an inverse commit, it does not remove history.
- **Cherry-pick → false `missing`.** In this repo, `762cc10d` is *not* an ancestor of
  `eb2b4f1a`, yet `git grep -c refreshMergedPaths eb2b4f1a -- internal/` finds the code
  (3 hits in `internal/daemon/workloop.go`) — the pick landed under a different SHA.
  Cherry-pick is how many fixes reach a deploy branch here, so this direction is live in
  normal operation.

When either applies, confirm by content: `git grep '<symbol the fix introduces>' <revision>`.
The step-2b deploy gate is unaffected, because there the `--contains` target *is* the revision
the binary was built from.

## Verify (authoritative)

```bash
# 0. The deployed binary carries the deploy SHA — re-run the step-2b gate
#    (exit 0 AND a `status:   contains` line; the exit code alone is not enough):
swap_gate

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
