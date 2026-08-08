# Evidence

> Append-only, written as you go. Every result that will be cited in the verdict gets a row here
> first. Do not reconstruct this at the end — a record assembled from memory agrees with the verdict
> because the same mind produced both.

## The tree this was run from

| | |
|---|---|
| Checkout | `<path>` |
| Safety check | `grep -c -- '--rev' scripts/scratch-daemon.sh` → `<n>` (22 = safe, 0 = refuse) |
| Scratch clone | `<path>` |
| Pinned revision | `<bare hash>` |
| `scratch-daemon.sh status` | `<what it printed>` |

**A revision is a bare commit hash and nothing else.** `NOT PINNED`, `DRIFTED`, `MODIFIED`, or a
`+local-edits` suffix means no result from that tree is an audit of that commit. If any appears,
record it here, rebuild, and do not fold the result into a verdict.

## Runs

One row per command whose result is cited anywhere. Copy the exit code from inside the log, not from
a pipeline — a pipeline returns the last command's status, and `cmd > log 2>&1; echo exit=$?` always
reports the echo's zero.

| # | When | Command | Revision | Exit | Log | Notes |
|---|---|---|---|---|---|---|
| 1 | HH:MM | | | | | |

## Conditions

Things that invalidate a timing result if they were true while it ran. Check before and after.

| | Before | After |
|---|---|---|
| `make leakcheck` | | |
| `df -g /` | | |
| Sub-agents active in this tree | | |

`make leakcheck`, not `uptime` — `uptime` cannot tell you whose load it is, and a leaked test binary
at 100% CPU has already invalidated a whole session's "quiet box" claims. The daemon pauses dispatch
below 10 GiB free and it reads as a flaky test. A sub-agent mutating a file in the same tree has
been compiled into a gate run and read as a real failure.

## NOT RUN

What did not run, and why. A disabled test is an unproven claim, not a pass. Read the NOT RUN
section of every test report and copy it here — this is the section that turns a silent gap into a
recorded one.

## Reproduce it

The shortest command sequence that gets someone else from a clean checkout to the same result.
