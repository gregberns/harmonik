# Disk reclaim runbook (where harmonik's disk actually goes)

What to check when the box runs low on disk, ordered by how much each source
actually grows. Every entry here is harmonik-generated or harmonik-adjacent —
this is not a general macOS cleanup guide. System caches (Spotify, Chrome,
Homebrew) are large but *stable*; they are not what fills the disk.

First measurement, always:

```bash
df -h /System/Volumes/Data
du -sh /private/tmp/claude-502/-Users-gb-github-harmonik   # usually #1
du -sh /Users/gb/github/harmonik/.beads                    # usually #2
```

A note on `find`: sorting by *apparent* size hides the real offenders (sparse VM
images look like 8 TB, occupy 13 GB). Rank by `du` output, not `ls -l`. And a
log that has been deleted while a process still holds it open consumes disk but
is invisible to both `find` and `du` — `lsof -nP +L1` is the only way to see it.

---

## 1. Agent session scratchpads — the biggest source by far

`/private/tmp/claude-502/-Users-gb-github-harmonik/<session-uuid>/scratchpad/`

One directory per Claude Code session, **never swept by anything**. Observed at
16 GB across 526 session dirs (2026-07-22).

The bulk is not source — it is **orphaned Go build caches**. Agents point
`GOCACHE` at their own scratchpad (`gocache-*`, `review-gocache`, …), so every
session builds a private full Go cache and leaves it behind. 37 such dirs held
12.1 GB; a single session held 2.4 GB across four of them.

```bash
# What's there
du -sh /private/tmp/claude-502/-Users-gb-github-harmonik
find /private/tmp/claude-502/-Users-gb-github-harmonik -maxdepth 3 -type d -name '*gocache*' \
  | while read -r d; do du -sm "$d"; done | sort -rn | head
```

**Safe to delete**: Go caches are regenerable build artifacts — zero risk.
Session dirs older than a day are dead unless they host a registered worktree.

**Before deleting, exclude:**
- your own session's scratchpad (the path in your system prompt),
- any session with a live process (`lsof -a -p <pid> -d cwd -Fn`),
- any session hosting a worktree with uncommitted changes (see §3).

## 2. `.beads/.br_history-archive` — the historical 25 GiB root cause

`br` writes a **full copy of `issues.jsonl` on every write** (~6.4 MB each, about
one per minute during active work). Rotation *archives* these into
`.br_history-archive/` rather than deleting them.

This is the confirmed 256 GB disk-fill incident: **25 GiB across 15,072
snapshots**. Capped by hk-8vnwg (`internal/daemon/brhistoryrotate.go`):

| Directory | Policy | Constant |
|---|---|---|
| `.br_history/` | keep 5 | `brHistoryCloseTrimKeep` |
| `.br_history-archive/` | keep 300 **or** older than 7d | `brHistoryArchiveKeep`, `brHistoryArchiveMaxAge` |

**Two failure modes remain:**

1. **Every prune path is daemon-driven** — `daemon.Start` and on-bead-close in
   `workloop.go`. With the daemon off while `br` writes continue, nothing prunes.
   Observed at 100 snapshots (642 MB) against a policy of 5.
2. **The cap is count-based, not size-based.** 300 snapshots at today's 6.4 MB
   is ~1.9 GB *at policy*, and grows as `issues.jsonl` grows. The 7-day age rule
   never fires under active work. The constants are explicitly marked in-source
   as knobs for the review gate to tune.

```bash
ls .beads/.br_history/*.jsonl | wc -l          # policy: 5
ls .beads/.br_history-archive/ | grep -c jsonl.archived   # policy: 300
```

To prune by hand, keep the N newest **pairs** — each snapshot is a `.jsonl` plus
a `.jsonl.meta.json` sidecar, and orphaning a sidecar confuses rotation.

Also here: `.beads/.br_recovery/` (pre-migration `beads.db` backups) and any
`.beads.bak.<epoch>/` left by a migration. Both are one-shot, not growing —
check dates and drop what predates a completed migration.

## 3. Git worktrees — four locations, none self-cleaning

```bash
git worktree list                 # registered
git worktree prune --dry-run -v   # registrations whose dir is gone
```

`prune` only clears registrations for *already-deleted* directories. It will
report nothing while stale worktrees still exist on disk — absence of prune
output is not evidence of cleanliness. Check all four:

| Location | Created by |
|---|---|
| `/private/tmp/claude-502/…/scratchpad/*` | agent sessions (`isolation: worktree`) |
| `~/github/harmonik-wt/*` | crew lanes |
| `.claude/worktrees/agent-*` | Agent-tool worktree isolation |
| `.harmonik/worktrees/<run-id>` | daemon run workspaces |

**Deleting a worktree directory never loses a commit.** Branches and objects
live in the shared store under `.git`; a branch sitting "30 commits ahead of
origin/main" is fully intact after its worktree is gone. Only *uncommitted*
changes are at risk.

```bash
# Find the ones that actually hold unsaved work
git worktree list --porcelain | grep '^worktree ' | sed 's/^worktree //' | while read -r w; do
  [ -d "$w" ] && n=$(git -C "$w" status --porcelain | wc -l) && [ "$n" != 0 ] && echo "$n  $w"
done
```

Remove with `git worktree remove <path>` — **without `--force`**, so anything
dirty refuses on its own. Then `git worktree prune`. Never `cd` into a worktree
to do this (see orchestrator-rules §CWD discipline); operate from repo root.

## 4. `.harmonik/events/events.jsonl` — append-only, unrotated

~100 MB and growing; plus frozen `baseline-*/` snapshots (~85 MB each).

**Do not truncate or rewrite it.** EV-020 makes this log append-only, and the
event bus detects tail truncation as corruption. Rotation needs a real
segmenting design — the defect is acknowledged in-source (`sessioncapture`
warns new subsystems not to "inherit the unrotated large-events.jsonl defect")
but unfixed for `events.jsonl` itself. Frozen `baseline-*` snapshots compress
~10:1 and are safe to gzip.

Smaller siblings worth a glance: `.harmonik/watch/`, `.harmonik/keeper/`,
`.harmonik/daemon-run.log`.

## 5. launchd stdout logs (adjacent projects on this box)

A launchd `StandardOutPath`/`StandardErrorPath` target has **no rotation** — the
app cannot rotate a file it did not open. Observed: a 2.4 GB `nanoclaw.log`,
306 MB of traefik logs.

Rotation must be **copy-truncate** (`: > file`). launchd holds the fd open, so
`rm` or `mv` leaves the daemon writing into an unlinked inode and reclaims
nothing.

`~/bin/rotate-app-logs.sh` handles this, run daily by
`~/Library/LaunchAgents/com.gb.rotate-app-logs.plist`. Add new paths to the
`LOGS` array there rather than writing a second rotator.

---

## Recurrence

Sources 1 and 2 regenerate continuously; the rest are one-shot. Re-check §1 and
§2 whenever free space drops below ~20 GB, and after any stretch with the daemon
stopped — that is when `.br_history` grows unbounded.
