# Evidence

Everything here was measured on `4ebd8a334` (`work/bravo-reachability`, rebased onto alpha's
`eac00a6c2`), clean tree, on 2026-08-11 between 14:34 and 15:10 local. Every exit code was read
from inside a log file, never from a pipe and never from the task runner.

## 0. The box, before anything ran

| Reading | Value | Threshold |
|---|---|---|
| Free disk | 17–18 GiB | 10 GiB floor |
| Load average at 14:33 | 15.94 | — |
| Load average at 14:34 | 8.25 | — |
| Leaked `.test` binaries | **none** | — |
| Holders of `~/.claude.json.lock` | **none** | — |

The last two rows matter more than they look. **The box was verifiably clean before the gate ran.**
That is what makes the leak in §3 self-inflicted rather than inherited.

The handoff opened with "THE BOX IS BELOW THE DISK FLOOR — 9.6 GiB". That had cleared. Re-checking
it cost one `df` and changed the plan from "reclaim disk first" to "run the gate now".

## 1. The rebase

`git rebase eac00a6c2` replayed all 9 bravo commits with no conflict. Pre-rebase tip preserved at
`backup/bravo-pre-rebase3-20260811` (`0d44509ac`). Verified after: `git merge-base --is-ancestor
eac00a6c2 HEAD` returns 0, and `git rev-list --count eac00a6c2..HEAD` returns 9. Nine ahead, zero
behind.

## 2. The merge decision

    make full  ->  LOGGED EXIT rc=2
    started 14:34, ended 14:54:05, 20 minutes
    218 packages ok, 15 failures, all in internal/daemon

**The task runner reported "completed (exit code 0)".** Twelfth recorded occurrence of that
disagreement. The log's own line says `rc=2`.

Load sampled every 15s for the whole run. During the scenario tier, 14:47 to 14:54, where every
failure occurred:

    14:47  2.52     14:51  4.42
    14:48  3.32     14:52  3.04
    14:49  5.39     14:53  2.85
    14:50  5.57     14:54  2.51

Disk held at 16 GiB throughout. **The box was quiet while the tier failed.** This is the reading
that breaks the previous assessment's conclusion, and it was only available because load was
sampled alongside the run rather than at the start.

### The failure durations

    8 x (50.18s)      1 x (63.16s)
    1 x (50.19s)      1 x (70.16s)
    1 x (50.23s)      1 x (100.20s)
    1 x (50.42s)      1 x (10.12s)

Ten of the fifteen also share one message, character for character:

    reached no terminal transition; events=[run_started node_dispatch_requested
    node_dispatch_decided node_dispatch_requested]

A healthy run in the same log continues past that point to `handler_capabilities
session_log_location skills_provisioned launch_initiated`. The wedged runs all stop at the same
edge.

Buried in the same 16,000-line log, five times:

    runmerge: prune worktree trust ... failed: workspace: EnsureWorktreeTrust:
    handlercontract: structural: write-lock acquire timed out (contended ~/.claude.json)

## 3. The holder, caught alive

At 14:57, three minutes after `make full` exited:

    PID 89524, ELAPSED 08:04, %CPU 98.6, STAT R
    /var/folders/.../go-build117863048/b252/daemon.test -test.timeout=10m0s

    lsof: daemon.te 89524 gb 9u REG /Users/gb/.claude.json.lock

An exclusive flock on the operator's **real** Claude Code config lock, held by a test binary from
the run that had just finished, past its own 10-minute timeout, burning a full core. Its other open
files name `TestSmokeLoop` and `TestThroughput_TenBeadsAtMaxFour`.

## 4. Positive control — remove the lock

Killed 89524. Confirmed `lsof ~/.claude.json.lock` empty. Load 2.54. Same commit, same tests, no
code change:

    PASS TestDotReviewer_ApproveThenWatcherErrorFailsTheNode   0.99s
    PASS TestDotReviewer_ApproveThenNonZeroExitFailsTheNode    0.99s
    PASS TestDotNode_ReadableBaselineFailsANodeThatDidNoWork   1.06s
    PASS TestDotNode_SessionMachineReachesTheRunHandle         1.20s
    PASS TestDotNode_HealthyRunWithARunnerStillCloses          1.47s
    ok internal/daemon 2.131s          LOGGED EXIT rc=0

One to one and a half seconds against a 50-second deadline. **A 35-fold margin.** These tests are
not marginal on wall clock, so no threshold change would be an honest repair.

## 5. Negative control — put the lock back

Held the flock from a python process. Same commit, same quiet box:

    PASS TestDotNode_SessionMachineReachesTheRunHandle         0.89s
    FAIL TestDotNode_HealthyRunWithARunnerStillCloses         50.19s
    FAIL TestDotNode_ReadableBaselineFailsANodeThatDidNoWork  50.19s
    LOGGED EXIT rc=1

Same tests, same 50.19s, same event set as the gate produced. The failure reproduces on demand and
disappears on demand. **That is causation measured in both directions, not a correlation.**

One test passed under the held lock that had failed in the gate. That is not noise — it is the
mechanism for the nondeterminism: which tests wedge depends on which happen to need the config
write while the lock is held.

## 6. The repair already exists, unmerged

    d119d5149  2026-08-09  work/alpha-trust-isolation
    "fix(workspace): the worker wrote the operator's real home config and waited on its lock forever"

Its own commit message opens: *"NOT MERGED TO THE INTEGRATION BRANCH ON PURPOSE. The review verdict
below is REQUEST_CHANGES and its secondary findings are NOT yet addressed."*

Verified: `git merge-base --is-ancestor d119d5149 eac00a6c2` returns non-zero. It is not in alpha's
tip and not in bravo's. It has sat for two days.

It describes the same mechanism found here — an unbounded `fcntl.flock(LOCK_EX)` on a hardcoded
`expanduser("~")` in the python worker programs — and reports **three** affected tests. This run
found **ten**.

## 7. Alpha's two claimed fixes, verified by running

| Claim | Where | Result |
|---|---|---|
| `hk-5ji8t`, the stale codex test | gate log | `PASS TestScenario_Codex_EmptyModel_FullLifecycle (5.59s)`; package `ok test/scenario 56.844s`. Was 4 of 4 failing. **Holds.** |
| `hk-set-concurrency-unbounded-ad79i` (LP-005) | live scratch daemon | **Holds** — see below |
| keeper watcher fix (`hk-oduuc`) | gate log | `ok internal/keeper 13.94s, 431 passed, 0 failed` |
| spawn-cap bounds | gate log | `ok internal/queue 10.93s, 557 passed, 0 failed` |

LP-005 run live against a scratch daemon at `4ebd8a334` on a 10-core host:

    999999 -> rc=2  error: spawn_cap_exceeded (code -32099)
     99999 -> rc=2  error: spawn_cap_exceeded (code -32099)
         0 -> rc=2  n must be an integer >= 1, got "0"
         2 -> rc=0  max_concurrent: 1 → 2
         4 -> rc=0  max_concurrent: 2 → 4
         8 -> rc=0  max_concurrent: 4 → 8
        16 -> rc=0  max_concurrent: 8 → 16
        32 -> rc=2  error: spawn_cap_exceeded (code -32099)

Previously `99999` exited 0 and applied `max_concurrent: 1 → 99999`. Both the refusal and the
acceptance were checked: a ceiling that refused everything would have passed a test that only tried
the big number.

## 8. A false trail, recorded because it nearly became three findings

The first scratch daemon was built at the session scratchpad path. It started, wired all 21
singletons, completed its orphan sweep and reconciliation, and then sat unreachable with no socket.
Three findings looked available: a daemon that boots and never listens; `scratch-daemon.sh status`
reporting `RUNNING` for it; `harmonik queue status` exiting 2 with empty stdout **and** empty
stderr.

All three were the choice of directory. Near the top of its own log:

    daemon.Start: socket path "...scratchpad/sd/.harmonik/daemon.sock" is 131 bytes, at or beyond
    the platform sun_path limit of 104 bytes ... move the project to a shorter path

Re-run at `/tmp/h/sd`: `daemon ready (2s)`. The corpus README prescribes `/tmp/h/<name>` and that
short path is load-bearing. **The product was right and the operator was wrong**, which is the same
shape as `hk-5ji8t` and the fourth time this pattern has been caught. It cost twenty minutes and it
is in the corpus now as LP-019's closing trap.

## What was NOT measured

- **`make full` was run once, not twice.** The both-directions controls in §4 and §5 carry the
  causal claim, so a second full run would add little; but the *set* of 15 failures is a single
  sample.
- **The five non-wedge failures** (`63.16s`, `70.16s`, `100.20s`, `10.12s`, and
  `TestCHBINV002_SessionContainsExactlyOneTerminalEvent`) were not isolated. They are not accounted
  for by the lock and may be the load class LP-018 describes, or something else. Presumption, not
  measurement.
- **No bead was driven end to end through a live dispatched run.** A scratch daemon was stood up and
  probed, which is more than the previous session managed, but the standing gap is still open.
