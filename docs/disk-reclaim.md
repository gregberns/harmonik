# Disk reclaim runbook (where harmonik's disk actually goes)

What to check when the box runs low on disk, ordered by how much each source
actually grows. Every entry here is harmonik-generated or harmonik-adjacent —
this is not a general macOS cleanup guide. System caches (Spotify, Chrome,
Homebrew) are large but *stable*; they are not what fills the disk.

Why this matters beyond disk: below the daemon's `diskLowWatermarkDefault`
(10 GiB, `internal/daemon/workloop.go`) dispatch is **skipped silently**. A full
disk therefore manufactures fake test failures and fake hangs fleet-wide, and
the event log makes them look like real regressions. Check `df` before blaming
a timing-out daemon or scenario test.

First measurement, always:

```bash
df -h /System/Volumes/Data
du -sh "$TMPDIR"                                           # often #1 — see §2
du -sh /private/tmp/claude-502/-Users-gb-github-harmonik   # §1
du -sh /Users/gb/github/harmonik/.beads                    # §3
```

## Did the command actually do anything?

**Every defect this runbook has ever shipped was a command that no-oped while
looking successful.** Deleting zero files and deleting a thousand files both
print nothing. So:

```bash
before=$(df -k /System/Volumes/Data | awk 'NR==2{print $4}')
# ... run one reclaim step ...
after=$(df -k /System/Volumes/Data | awk 'NR==2{print $4}')
echo "reclaimed $(( (after-before)/1024 )) MiB"
```

A step that reports 0 MiB did not work — do not move on, and do not report it
as done. Corollaries that have each bitten someone here:

- Pipe a destructive command through `| head`/`| wc` and you lose its exit
  status to `PIPESTATUS`. Check the error text, not just `$?`.
- `rm -rf` prints `Permission denied` per file and still exits 0 on some
  shells' pipelines. Re-`ls` the directory.
- `du -sh` after the delete is the cheapest per-directory confirmation.

## Portability: this box is darwin, the runbook is not GNU

Commands here run on macOS (BSD userland). Several GNU idioms that appear in
blog posts and in agent muscle memory **fail or silently do nothing** here.
Verified on this machine:

| GNU idiom | On darwin | Use instead |
|---|---|---|
| `head -n -5` (negative count) | `head: illegal line count` — prunes **zero** | `sed -n "1,${drop}p"` after computing `drop` |
| `find … -newermt '-2H'` (relative) | `Can't parse date/time: -2H` | absolute: `-newermt '2026-07-23 01:07'` — this **does** work |
| `find … -printf` | `unknown primary or operator` | `-exec` or a `while read` loop |
| `stat -c %s` | `illegal option -- c` | `stat -f %z` |
| `date -d '-2 hours'` | `illegal option -- d` | `date -v-2H` |
| `sed -i` | consumes the next arg as the suffix | `sed -i ''` |
| `xargs -r` | flag accepted, but BSD `xargs` already skips empty input | plain `xargs` |
| `tac` | not installed | `tail -r`, or sort so you don't need reversal |
| `n=$(… | wc -l); [ "$n" != 0 ]` | BSD `wc` pads (`"       0"`), so the test is **always true** | `wc -l \| tr -d ' '`, then `[ "$n" -gt 0 ]` |

`-newermt` deserves the correction spelled out, because it is easy to conclude
from a failed relative form that the whole primary is unusable on darwin. It is
not. Only the *relative offset* form is rejected; an absolute timestamp works
and is the recommended liveness guard throughout this document. Note also that
in an agent shell `find` may be a shell-snapshot function resolving to `bfs`
rather than `/usr/bin/find`; both reject the relative form, but their error text
differs, so match on behavior, not on the message.

A note on `find` and sizes: sorting by *apparent* size hides the real offenders
(sparse VM images look like 8 TB, occupy 13 GB). Rank by `du` output, not
`ls -l`. And a log that has been deleted while a process still holds it open
consumes disk but is invisible to both `find` and `du` — `lsof -nP +L1` is the
only way to see it.

---

## 1. Agent session scratchpads

`/private/tmp/claude-502/-Users-gb-github-harmonik/<session-uuid>/scratchpad/`

One directory per Claude Code session, **never swept by anything**. Observed at
16 GB across 526 session dirs (2026-07-22).

The bulk is not source — it is **orphaned Go build caches**. Agents point
`GOCACHE` at their own scratchpad, so every session builds a private full Go
cache and leaves it behind. 37 such dirs held 12.1 GB; a single session held
2.4 GB across four of them.

### Detect by content, not by name (hk-9oizl)

The old detector, `find … -type d -name '*gocache*'`, **matches nothing** as of
2026-07-22 and reports a clean machine over several GiB of caches. Naming has
drifted to `gc-shared`, `gc-private`, `drain-gocache`, `lintcache-XXXXXX`,
`review-gocache`, `tmp.XXXXXXXX`. Go and golangci-lint both drop a `README`
into their cache root; match on that instead, and the check survives the next
rename:

```bash
ROOT=/private/tmp/claude-502/-Users-gb-github-harmonik
find "$ROOT" -maxdepth 4 -name README -type f 2>/dev/null | while read -r r; do
  grep -qs 'from the Go build system\|from golangci-lint' "$r" && dirname "$r"
done | sort -u | while read -r d; do du -sm "$d"; done | sort -rn
```

**Safe to delete**: Go caches are regenerable build artifacts — zero risk.

### Deciding a session is dead (hk-pans1)

The old advice — exclude "any session with a live process
(`lsof -a -p <pid> -d cwd -Fn`)" — **excludes nothing**. A live agent's cwd is
the *repo*, not its scratchpad: every live `claude`-spawned shell on this box
reports `n/Users/gb/github/harmonik`. The check can never match a scratchpad
path, so it silently protects nothing.

Neither signal alone is sufficient. Use all three:

```bash
ROOT=/private/tmp/claude-502/-Users-gb-github-harmonik

# (a) sessions holding OPEN FILE HANDLES — sub-agent task outputs live here,
#     so a genuinely live session can have a 16h-old directory mtime.
lsof -nP 2>/dev/null | grep -o "$ROOT/[0-9a-f-]\{36\}" | sort -u

# (b) ... but an open fd is NOT proof of life. Reject holders reparented to
#     launchd. Observed: `head` (83776) and `tailscale version` (59246) stuck
#     since Jul 2, both PPID 1. Trusting (a) alone pins those dirs forever.
lsof -nP 2>/dev/null | grep "$ROOT" | awk 'NR>1{print $2}' | sort -un |
while read -r p; do
  ppid=$(ps -o ppid= -p "$p" 2>/dev/null | tr -d ' ')
  [ -n "$ppid" ] && [ "$ppid" != 1 ] && echo "live-ish pid=$p ppid=$ppid"
done

# (c) recent writes anywhere under the session (absolute timestamp — see the
#     portability table; a relative '-2H' is REJECTED on darwin).
find "$ROOT/<session-uuid>" -newermt '2026-07-23 01:07' -type f | head
```

Also exclude **your own** session's scratchpad — the path in your system prompt.

### Uncommitted work hides deeper than `git worktree list`

Registered worktrees are not the whole set. Agents `git init` or `git clone`
standalone repos inside scratchpads (`jcrzn-b-check`, `e2e-proj` on this box,
both dirty, neither in `git worktree list`). Scan recursively before deleting
anything:

```bash
find "$ROOT" -maxdepth 5 -name .git 2>/dev/null | while read -r g; do
  d=$(dirname "$g")
  n=$(git -C "$d" status --porcelain 2>/dev/null | wc -l | tr -d ' ')
  [ "${n:-0}" -gt 0 ] && echo "$n  $d"
done
```

That scan has surfaced an 837-file dirty worktree nested in a scratchpad.

### `rm -rf` alone does not delete a Go module cache

`GOPATH/pkg/mod` is written **read-only** (`dr-xr-xr-x` / `-r--r--r--`). A naive
`rm -rf` emits hundreds of `Permission denied` lines, exits without a useful
status through a pipe, and leaves multi-GB directories in place. This bit the
2026-07-23 reclaim mid-run. Always:

```bash
chmod -R u+w "$victim" && rm -rf "$victim"
```

Then `du -sh` the parent to confirm, per §"Did the command actually do
anything?".

## 2. `$TMPDIR` — orphaned Go caches, the single largest source on 2026-07-23

**Missing from this runbook until 2026-07-23, and it was #1 that day:
7.5 GiB across 69 orphaned Go cache directories** in
`/private/var/folders/s9/…/T/`. That is more than every dead agent scratchpad
combined. `du -sh /private/tmp/…` will never see it.

```bash
echo "$TMPDIR"
du -sh "$TMPDIR"
du -sm "$TMPDIR"* 2>/dev/null | sort -rn | head -20
```

**Producer: inline `GOCACHE=$(mktemp -d)` / `GOPATH=$(mktemp -d)` per command.**
This was adopted fleet-wide as the mitigation for hk-gjbpp (the daemon's
proactive `go clean -cache` corrupting out-of-band builds). Bare `mktemp -d`
lands in `$TMPDIR` as `tmp.XXXXXXXX`, each build leaves 100–190 MB, and
**nothing owns or reaps them** — the shell that created the variable is long
gone. An earlier round of the same pattern produced 243 orphans / 23 GB.
The mitigation for one P1 manufactured a second.

`scripts/with-isolated-gocache.sh` is **not** the producer: it uses a
`harmonik-gocache.XXXXXX` prefix and removes its directory in an `EXIT` trap
(plus `HUP`/`INT`/`TERM` forwarding), and zero `harmonik-gocache.*` orphans
exist. Its residual exposure is `SIGKILL` and killed panes, which no trap
survives. The fix shape is one cache per *session* — export once, reuse,
delete at session end — not one per command.

Also in `$TMPDIR`: Go's own `go-build*` scratch dirs (8 present, up to 188 MB
each) and `scratch-*` / `scenario-twin-*` test leftovers.

### There are at least six places a Go cache lives in this project

A reclaim that checks one convention finds almost nothing:

| Path | Written by |
|---|---|
| `$TMPDIR/tmp.XXXXXXXX` | bare inline `GOCACHE=$(mktemp -d)` — **unowned, the big one** |
| `$TMPDIR/harmonik-gocache.XXXXXX` | `scripts/with-isolated-gocache.sh` (self-cleaning except on SIGKILL) |
| `$TMPDIR/go-build*` | the Go toolchain's own temp dirs |
| `~/Library/Caches/go-build` | Go's default `GOCACHE` — **macOS-purgeable**, see hk-pgtbr |
| `~/.cache/h-*-gocache`, `/tmp/h-*/gocache` | long-lived named caches (assessor campaigns, isolated lanes) |
| `<worktree>/.harmonik/go-cache` | the daemon's merge gate (`internal/daemon/workloop.go`) |
| scratchpad `gc-*`, `*-gocache`, `lintcache-*` | per-agent-session caches (§1) |

## 3. `.beads/.br_history-archive` — the historical 25 GiB root cause

`br` writes a **full copy of `issues.jsonl` on every write** (~6.0 MB each,
about one per minute during active work). Rotation *archives* these into
`.br_history-archive/` rather than deleting them.

This is the confirmed 256 GB disk-fill incident: **25 GiB across 15,072
snapshots**. Capped by hk-8vnwg (`internal/daemon/brhistoryrotate.go`):

| Directory | Policy | Constant |
|---|---|---|
| `.br_history/` | keep 5 | `brHistoryCloseTrimKeep` |
| `.br_history-archive/` | keep 300 **or** older than 7d | `brHistoryArchiveKeep`, `brHistoryArchiveMaxAge` |

**Two failure modes remain:**

1. **Every prune path is daemon-driven** — `daemon.Start` and on-bead-close in
   `workloop.go`. With the daemon off while `br` writes continue, nothing
   prunes. Observed at 100 snapshots (642 MB) against a policy of 5.
2. **The cap is count-based, not size-based.** Measured 2026-07-23: **300 pairs
   — exactly at policy, every one inside the 7-day window — occupying
   1.7 GiB.** Full policy compliance still costs 1.7 GiB, and the cost grows
   with `issues.jsonl`. The 7-day age rule never fires under active work. This
   is a real design gap, not an operational failure; the constants are marked
   in-source as knobs for the review gate to tune.

### Counting

Each snapshot is a **pair**: a `.jsonl` and a `.jsonl.meta.json` sidecar. So a
bare entry count reads 2× the number of snapshots and looks like a violation
when the directory is exactly at policy:

```bash
ls .beads/.br_history/*.jsonl | wc -l                        # pairs; policy 5
ls .beads/.br_history-archive/ | grep -c '\.jsonl\.archived-'  # pairs; policy 300
ls .beads/.br_history-archive/ | wc -l                       # 2x — NOT the count
```

Anchor the archive pattern as `'\.jsonl\.archived-'`. An unanchored
`grep -c jsonl.archived` happens to return the right number today only because
the sidecar is named `…jsonl.meta.json.archived-…`; do not rely on the `.`
wildcard staying lucky through a naming change.

### Pruning by hand

Keep the N newest **pairs** — orphaning a sidecar confuses rotation.
`head -n -N` is a GNU extension and **prunes nothing on darwin**; compute the
drop count and use `sed` instead. Filenames embed a zero-padded timestamp, so
plain lexical sort is oldest-first:

```bash
A=.beads/.br_history-archive
keep=100
total=$(ls "$A" | grep -c '\.jsonl\.archived-')
drop=$((total-keep))
[ "$drop" -gt 0 ] && ls "$A" | grep '\.jsonl\.archived-' | sort |
  sed -n "1,${drop}p" | while read -r f; do
    base=${f%%.jsonl.archived-*}; suffix=${f#*.jsonl.archived-}
    rm -f -- "$A/$f" "$A/${base}.jsonl.meta.json.archived-${suffix}"
  done
```

Run it once with `rm -f` replaced by `echo` first, then confirm with `df`.

Also here: `.beads/.br_recovery/` (pre-migration `beads.db` backups, 52 MB) and
any `.beads.bak.<epoch>/` left by a migration. Both are one-shot, not growing —
check dates and drop what predates a completed migration.

## 4. Git repos and worktrees — five locations, none self-cleaning

```bash
git worktree list                 # registered
git worktree prune --dry-run -v   # registrations whose dir is gone
```

`prune` only clears registrations for *already-deleted* directories. It will
report nothing while stale worktrees still exist on disk — absence of prune
output is not evidence of cleanliness. And it never sees an unregistered nested
clone at all (§1). Check all five:

| Location | Created by |
|---|---|
| `/private/tmp/claude-502/…/scratchpad/*` | agent sessions (`isolation: worktree`), plus unregistered `git init`/`clone` |
| `~/github/harmonik-wt/*` | crew lanes |
| `.claude/worktrees/agent-*` | Agent-tool worktree isolation |
| `.harmonik/worktrees/<run-id>` | daemon run workspaces |
| `$TMPDIR/tmp.*`, `/tmp/hk-*` | `mktemp -d` scratch projects from `test/exploratory/*.sh`, smoke scripts, and inline Go caches (§2) |

**Deleting a worktree directory never loses a commit.** Branches and objects
live in the shared store under `.git`; a branch sitting "30 commits ahead of
origin/main" is fully intact after its worktree is gone. Only *uncommitted*
changes are at risk.

```bash
# Find the ones that actually hold unsaved work.
# NOTE: `tr -d ' '` is load-bearing — BSD `wc -l` pads its output, so the
# untrimmed comparison is true for every worktree and reports them all dirty.
git worktree list --porcelain | grep '^worktree ' | sed 's/^worktree //' |
while read -r w; do
  [ -d "$w" ] || continue
  n=$(git -C "$w" status --porcelain | wc -l | tr -d ' ')
  [ "${n:-0}" -gt 0 ] && echo "$n  $w"
done
```

Then run the **recursive** scan in §1 for the repos `git worktree list` cannot
see.

Remove with `git worktree remove <path>` — **without `--force`**, so anything
dirty refuses on its own. Then `git worktree prune`. Never `cd` into a worktree
to do this (see orchestrator-rules §CWD discipline); operate from repo root.

## 5. `.harmonik/events/events.jsonl` — append-only, unrotated

99 MB and growing; plus frozen `baseline-*/` snapshots (94 MB).

**Do not truncate or rewrite it.** EV-020 makes this log append-only, and the
event bus detects tail truncation as corruption. Rotation needs a real
segmenting design — the defect is acknowledged in-source (`sessioncapture`
warns new subsystems not to "inherit the unrotated large-events.jsonl defect")
but unfixed for `events.jsonl` itself.

Frozen `baseline-*` snapshots would compress roughly 10:1, but **`gzip -r`
alone fails on them and reclaims nothing.** They are deliberately frozen:
the directory is `dr-xr-xr-x` and its files `-r--r--r--`, so gzip cannot write
the `.gz` next to the original — it prints one `Permission denied` per file and
leaves the tree untouched. Verified 2026-07-23. If you compress one, restore
the freeze afterwards:

```bash
B=.harmonik/events/baseline-2026-07-13
chmod -R u+w "$B" && gzip -r "$B" && chmod -R a-w "$B"
du -sh "$B"      # confirm it actually shrank
```

Smaller siblings worth a glance: `.harmonik/watch/`, `.harmonik/keeper/`,
`.harmonik/daemon-run.log`.

## 6. launchd stdout logs (adjacent projects on this box)

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

## What we deliberately did NOT delete (2026-07-23)

Recorded so the next sweeper does not re-litigate these.

- **`.beads/.br_history-archive`, 1.7 GiB — left in place.** It is at *exactly*
  its documented policy: 300 pairs, all inside the 7-day window. Nothing is
  broken; the policy itself costs 1.7 GiB. See §3 failure mode 2. Fix the
  policy, don't hand-prune a compliant directory.
- **`.harmonik/events/events.jsonl` (99 MB) and `baseline-2026-07-13` (94 MB) —
  left in place.** Append-only per EV-020; truncation reads as corruption to
  the bus. The baseline is compressible (§5) but was not touched.
- **macOS APFS local snapshots — operator action, do not attempt.** Three
  `com.apple.os.update-*` snapshots exist (`tmutil listlocalsnapshots /`) and
  likely account for much of the gap between measured file usage and the
  volume's reported usage — ~58 GiB unaccounted on 2026-07-23. Removing them
  needs `sudo tmutil deletelocalsnapshot <name>`. **This is an operator
  decision; an agent must surface it, not run it.**
- **VM / container images — owner call, not sweeper territory.** `~/.lima` is
  3.3 GiB (`~/.lima/test`); OrbStack keeps its VM data outside `~/.orbstack`
  (which is only config). Deleting either destroys container/VM state that
  nothing else backs up. Ask the owner.

## Recurrence

§1, §2 and §3 regenerate continuously; the rest are one-shot. Re-check them
whenever free space drops below ~20 GB, and after any stretch with the daemon
stopped — that is when `.br_history` grows unbounded. §2 grows fastest of the
three while the hk-gjbpp inline-`mktemp` workaround is in force.

Whatever you run, bracket it with `df` (§"Did the command actually do
anything?"). Every defect corrected in the 2026-07-23 rewrite of this file
would have been caught by a single before/after reading.
