# Disk reclaim runbook (where harmonik's disk actually goes)

What to check when the box runs low on disk, ordered by how much each source
actually grows. Every entry here is harmonik-generated or harmonik-adjacent —
this is not a general macOS cleanup guide. Third-party caches (Spotify, Chrome,
Homebrew) are large but *stable*; they are not what fills the disk. The shared
Go and lint caches in `~/Library/Caches` look like that category and are not —
they grow with the builds this project runs, which is why they lead the list.

Why this matters beyond disk: below the daemon's `diskLowWatermarkDefault`
(10 GiB, `internal/daemon/workloop.go`) dispatch is **skipped silently**. A full
disk therefore manufactures fake test failures and fake hangs fleet-wide, and
the event log makes them look like real regressions. Check `df` before blaming
a timing-out daemon or scenario test.

## FIRST ACTION — reap the Go build cache (measured #1, 2026-08-11)

This is the first ACTION in this runbook. It is not the first MEASUREMENT: when
`df` and `du` disagree, the subtraction in the next section still comes first.
Start with the dry run. It measures, it deletes nothing, and it tells you which
of the two cases below you are in. The reap takes about a minute, and it is the
largest single return this runbook has measured.

```bash
DRY_RUN=1 scripts/go-cache-reap.sh   # report only, delete nothing
scripts/go-cache-reap.sh 15          # reap the shared cache to 15 GiB
```

Run it by hand. Nothing schedules it, and nothing should. The last step of the
shared-cache section below gives the reason: an operator can pause the fleet
first, and the daemon could not.

The script also landed under a REQUEST_CHANGES verdict
(`hk-reap-age-floor-r96xm`), so read it before you trust it. Its header still
says a reap is safe during a build. That header is wrong, and the same bead asks
for it to be corrected.

Measured 2026-08-11, before the reap. The cache held 32.4 GiB by `du` and
31.4 GiB by the sum of the file sizes. Both numbers are right. `du` counts
allocated disk blocks, and the 165,000 one-line `-a` metadata files each take a
whole block, which adds about 0.6 GiB. The reap removed 24,418 objects and
returned **17.9 GiB in 48 seconds**. Free space went from 12 GiB to 29 GiB.

**Prefer a quiet box. The claim that a reap is always safe beside a running
build is WRONG, and this runbook records the symptom further down.** Most of a
delete is harmless. Cache entries are content addressed, so a deleted entry is a
cache miss and the build makes it again. A build that already has the file open
keeps its handle. But Go stats a cache object and hands the PATH back to its
caller, which opens it later, sometimes at link time. A delete inside that
window is a build failure, not a cache miss.

**Tell the two cases apart before you delete. The dry run prints the object
count.** Fewer than 1,000 objects is a small reap. It is very unlikely to reach
a file a live build is about to open, so run it whenever you want. Treat 1,000
objects or more as a large reap. The headline recipe above is that case, because
it took 24,418 objects. Before a large reap, make the box quiet: stop the lanes,
let the running builds finish, then reap.

A large reap can reach live entries, because this cache is churn and not stale
accumulation. Measured on 2026-08-11, 13 to 17 percent of the output objects had
been used within the last hour, and nothing at all had gone 5 days unused. The
idle material is thin: on that day only 18 GiB of 31 GiB had gone more than a
day without use, and the reap needed 17 GiB. The margin between taking the idle
bytes and taking bytes a build still wants is one reap wide, and no age floor
holds it open.

Until the script grows an age floor (see `hk-reap-age-floor-r96xm`), the operator
is the age floor. Quiet the box first.

### Why the cache grows without limit

Go bounds the build cache by TIME, not by SIZE. `trimLimit` in
`cmd/go/internal/cache/cache.go` removes entries that are unused for 5 days.
There is no size limit at all.

**You cannot change the 5 days.** `trimLimit` is a compile-time constant, and no
environment variable moves it. The cache package reads `GOCACHE`. That key only
names the directory. It also reads `GOCACHEPROG`, which replaces the disk cache
with an external program. It also reads three `GODEBUG` keys — `gocacheverify`,
`gocachehash` and `gocachetest`. Those three are debug switches. `go env` has
three cache keys: `GOCACHE`, `GOCACHEPROG` and `GOMODCACHE`. Not one of them
sets a size bound.
The 5 days comes from one month of telemetry from Go developers, where almost
all reuse happened within 5 days of the last reuse (golang/go#22990). It fits
one developer on one machine. It does not fit a fleet of lanes that cross-build
five configurations.

There is no steady state on this box, because Go removes nothing on this box. In
the 24 hours to 2026-08-11 21:15 it made 13.1 GiB of new cache objects, measured
on creation time (`stat -f %B`). Read that as a floor, because the reap earlier
that day removed some of them. No entry reaches 5 days unused, so `trimLimit`
never fires. Go ran its own trim at 16:15 that day and freed nothing. The cache
therefore grows until a person removes it, and free space reaches the daemon's
10 GiB dispatch floor long before Go acts.

Age profile of the 31 GiB measured on 2026-08-11, before the reap. **These rows
are last-USED times, not made-at times** — see "Use mtime, not creation time"
below. They say how much of the cache is idle:

| last used | size |
|---|---|
| less than 1 day | 12.8 GiB |
| 1 to 2 days | 17.0 GiB |
| 2 to 3 days | 1.2 GiB |
| more than 3 days | 0.0 GiB |

Do not read a daily creation rate off this table. It is a different measurement.
The table cannot hold five days of creation, because an entry that builds keep
using never ages into the old rows.

### What the files are

Each of the 256 shard directories holds two kinds of file.

- `<hash>-a` is action metadata. There were 164,964 of them. Each one is a
  single line of text, so they hold under 0.1 GiB of data. They take about
  0.6 GiB of disk, because each one uses a whole block.
- `<hash>-d` is the output. There were 41,835 of them, and they held 31.4 GiB.
  `file` reports them as `ar` archives. They are compiled Go packages.

Almost all the space is the `-d` objects. None of it is garbage. Every object
is compiler output that Go made and kept on purpose. The problem is that no
entry ever replaces another. One changed line makes a NEW entry for that
package and for every package that depends on it. The old entry stays until it
is 5 days stale.

The count multiplies across three axes at the same time:

- Build configurations. Counted 2026-08-11, the makefile and the scripts use
  `-race` 34 times and `-tags=scenario` 14 times, and they ask for coverage in
  three forms — `-coverprofile` 18 times, `-coverpkg` 15 times, `-covermode`
  8 times — plus the `subprocess` and `integration` tags. These counts drift
  every time the makefile or a script changes, so re-count them before you quote
  them. Each configuration makes a separate set of entries for the same code.
  Nothing here measures which axis multiplies hardest, so do not rank them. They
  multiply together. Count the flags, not the word. A grep for `cover` returns
  hundreds of hits, and almost all of them are the word "coverage" in script
  names and identifiers.
- Lane branches, each with its own source state. This runbook has said "about
  15" since it was written. That figure is inherited and unverified. On
  2026-08-11 only 5 lane cache directories existed.
- Every commit.

### Use mtime, not creation time

Go touches an entry's mtime each time the entry is USED, at most once per hour.
So mtime means "last used" and not "made at". Oldest mtime first is therefore
least recently used, and that is what the script deletes. Creation time
(`stat -f %B`) gives first in, first out. It would delete the packages that
every build depends on, and it would cause constant rebuilds.

### The per-lane caches need a separate run

`scripts/go-cache-reap.sh` reaps the directory that `go env GOCACHE` reports.
That is the shared cache. Each lane has its own cache under
`~/Library/Caches/harmonik-lane-gocache/`, and the script does not see those.
Point it at one with the same variable:

```bash
GOCACHE=~/Library/Caches/harmonik-lane-gocache/<lane>-<8hex> scripts/go-cache-reap.sh 2
```

A lane directory name ends in an 8-hex suffix, as in `bravo-6f0b4425`. Take the
real name from `ls ~/Library/Caches/harmonik-lane-gocache/`.

The per-lane caches have their own defect. Nothing removes a lane cache after
its worktree goes away, and the caches for throwaway merge-check worktrees are
never reused. See bead hk-qqg5c.

### When the reap is not enough

Read the next section and reconcile `df` against `du`. Space that the reap does
not return is in a place that no file list shows.

## READ FIRST — reconcile `df` against `du` before you look at any file list

**More than a dozen agents worked this runbook and none of them found the space.** The reason is
structural: on 2026-07-30 the single biggest consumer was **invisible to `du` by permission**, and
about 42 GiB more sat on APFS volumes this runbook never mentions. Every command below the fold
reported honest numbers about the wrong 4% of the disk.

So the first measurement is not a file list. It is a subtraction:

```bash
df -k /System/Volumes/Data | awk 'NR==2{print "df used MiB:", $3/1024}'
sudo du -x -sk /System/Volumes/Data | awk '{print "du  sees MiB:", $1/1024}'
```

**Do not add `2>/dev/null` to the `du` line.** An earlier version of this runbook did, and that hides
the one error you must see. Without **Full Disk Access** for your terminal application, macOS TCC
denies `du` on almost every path **even under `sudo`**, and it prints `Operation not permitted` per
file. Silenced, the command still exits 0 and still prints a number — a very small one — so the gap
looks enormous and you chase a phantom. Grant Full Disk Access in **System Settings → Privacy &
Security → Full Disk Access**, then re-run. This is the runbook's own headline command failing the
"did it actually do anything?" test below.

**Full Disk Access does not silence every denial, and that is correct behaviour.** About a dozen paths
stay denied to root even with it — `.Spotlight-V100` (owned by `_mds_stores`), `.DocumentRevisions-V100`,
`.fseventsd`, `private/var/db/{Spotlight,sysdiagnose,DumpPanic,SoC,appinstalld}`,
`System/Library/AssetsV2/com_apple_MobileAsset_*`. **Read the list, do not silence it.** A dozen lines
naming system databases means the number below is trustworthy to within a few GiB. Pages of denials
under `/Users` means Full Disk Access did not take. Those three hidden directories are the only ones
big enough to matter, and only root can size them:

```bash
sudo du -xsh /System/Volumes/Data/.Spotlight-V100 \
             /System/Volumes/Data/.DocumentRevisions-V100 \
             /System/Volumes/Data/.fseventsd
```

`.DocumentRevisions-V100` is the macOS Versions database. It reaches tens of GiB on a machine that
writes files constantly, nothing in this project reaps it, and it is invisible to every other command
in this runbook.

**If those disagree by more than a few GiB, the gap IS the answer and no file list will show it.**
Chase the gap in this order, and stop when it closes:

1. **Directories `du` cannot read.** Measured 2026-07-30: `/System/Volumes/Data/macOS Install Data/`
   held a **16.7 GB** completed-in-March macOS installer that was never removed. Its `Locked Files`
   subdirectory is mode `d-w-r-xr--`, so `du` reports **`0B`** and `ls` returns permission denied.
   The manifest beside it (`index.sproduct`) is world-readable and names the payload and its exact
   byte count. Check it: `grep -a InstallAssistant "/System/Volumes/Data/macOS Install Data/index.sproduct"`.
2. **Other APFS volumes in the same container.** They share the disk and are invisible to any `du` of
   `/Users` or `/System/Volumes/Data`. Measured: System 16.6, Preboot 16.5, VM 6–8, Recovery 2.4 —
   about 42 GiB. A Preboot far above 1–2 GiB means a staged-but-not-installed macOS update is holding
   `cryptex1/proposed` beside `current`. Check `softwareupdate --list` for a pending `Action: restart`.
   **Do not hand-delete anything under Preboot — that can leave the box unbootable.** Install the
   update and restart.
3. **Swap, which is monotonic within a boot.** macOS grows swapfiles under pressure and never removes
   them until restart. `sysctl vm.swapusage`. Measured 8 GiB after 5 days of heavy agent load. This is
   why a reboot "frees a ton" and why the growth feels like a leak — it is one, and only a restart
   returns it.

   **This is fast enough to invalidate a test run while the run is happening.** Measured 2026-08-05,
   also 5 days up: swap had reached 21.5 GiB, and **one `go test ./internal/daemon/` alongside another
   lane's coverage build minted five 1 GiB swapfiles in three minutes** — `ls -lt /System/Volumes/VM/`
   dates them. Free space had just been reclaimed to 13 GiB and was back under the daemon's 10 GiB
   dispatch floor before the suite finished, so the suite was measuring a starved box and its verdict
   was worthless. When `softwareupdate --list` reports nothing pending, item 2 is not the answer and
   this one is: **a box that has been up for days cannot give a trustworthy `make full`. Restart it
   first.**
4. **Local APFS snapshots.** `tmutil listlocalsnapshots /` and
   `diskutil apfs listSnapshots /dev/<data-volume>`. A snapshot named `MSUPrepareUpdate` is item 2
   again.
5. **Deleted-but-still-open files** held by a long-lived process — `lsof +L1`, and sum the SIZE
   column. This is the classic signature when `df` and `du` disagree and everything above is clean.
   Measured only 1.6 GiB on 2026-07-30, so it was not the answer that day, but it is cheap to rule out.

Only when the gap is closed does the file list below become the right tool.

**Outcome of the 2026-07-30 case, so nobody re-derives it.** Free space went 5.2 GiB → 31 GiB by hand,
then **31 GiB → 64 GiB from one action**: install the pending macOS update and restart. That single
step did what no `rm` could. It let macOS remove the SIP-protected installer under
`macOS Install Data/Locked Files/` (`rm` refuses there even as root, so do not try), retired the
staged Preboot copy, and zeroed 8 GiB of swap. After the restart: swap 0.00M, Preboot back to 8.5 GiB,
`macOS Install Data/` an empty stub. **When items 1, 2 and 3 all point at a pending update, stop
deleting and install it.**

**What a closed reconciliation looks like**, measured the same day after the restart: `df` used
146,110 MiB, privileged `du` saw 139,917 MiB, **gap 6.05 GiB**. No local snapshots, nothing held by
deleted-but-open files. A gap that size is the denied system databases plus APFS accounting, and it
means the subtraction is finished — go read the file list. Compare against the same machine before
the restart, where the gap was above 40 GiB.

**Do not compute the gap by summing your own list of top-level directories.** That was tried here and
it manufactured a 25 GiB hole that did not exist, because the list left out `Data/System`, the other
home directories, and the hidden databases. The `du` total is the measurement. A hand-built sum of
parts is a guess wearing a measurement's clothes.

**Two traps in this runbook's own history.** The shared `~/Library/Caches/go-build` is listed below as
the measured number-one source. It read **7 MiB** on 2026-07-30 — because the daemon's own low-disk
reap ran `go clean -cache` and had already emptied it. **That reap is gone as of 2026-08-03.** The
daemon now reports a low disk and deletes nothing it does not own, because the cache it was clearing
is shared with builds it cannot see (§0). So a small `go-build` reading no longer has a standing
explanation: somebody cleared it by hand, or the box is genuinely cold. Either way the space is in
§2 and §4, which nothing reaps at all. And `du -x` does **not** confine itself to one volume
here: firmlinks give `/`, `/Users` and `/System/Volumes/Data` the same device id, so `du -x -s -g /`
returns more than the disk holds.

---

First measurement of the file-list phase (only after the gap above is closed):

```bash
df -h /System/Volumes/Data
du -sh ~/Library/Caches/go-build ~/Library/Caches/golangci-lint  # see the trap note above
du -sh "$TMPDIR"                                           # often #1 — see §2
du -sh /private/tmp/claude-502/-Users-gb-github-harmonik   # §1
du -sh /Users/gb/github/harmonik/.beads                    # §3
```

Add the per-checkout caches. Nothing reaps either one, and they are the first thing to clear
rather than the last — see §0:

```bash
du -sh /Users/gb/github/harmonik-wt-cache          # 8.3 GiB on 2026-07-30, ~3.7 GiB/day while lanes run
du -sh ~/Library/Caches/harmonik-lane-gocache      # one dir per checkout; outlives the checkout — see §2
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
- A zsh glob that matches nothing **aborts the whole command**. `rm -rf "$T"/go-build*
  "$T"/scratch-*` prints `no matches found` and deletes *nothing* — including the
  glob that did match. This is the purest form of the failure this section exists
  to catch: it looks like a completed step and reclaims 0 MiB. See the portability
  table for the `find … -print0 | xargs -0` form that does not have this property.

## zsh: three ways a cleanup command does nothing and reports success

The default interactive shell here is zsh. Agents write bash idioms from muscle memory. **All three of
the following ran during one session on 2026-07-30, and every one silently did nothing while looking
like it worked.** This is the same failure class §"Did the command actually do anything?" exists for,
so check the reclaimed bytes, not the exit code.

| Idiom | What zsh does | Cost when it bit us | Use instead |
|---|---|---|---|
| `for d in $LIST; do rm -rf "$dir/$d"; done` | **No word splitting** on unquoted expansion. `$LIST` is ONE word, so the path never exists and `rm -rf` succeeds against nothing. | Reclaimed 0 MiB, reported success, every directory still present. | `LIST=(a b c)` and `"${LIST[@]}"`, or run the loop under `bash -c`. |
| `BUSY=$(jobs -p); kill $BUSY` | **Clears the job table inside command substitution.** `$BUSY` is empty, `kill` gets no arguments and exits 1. | Twice. 30 orphaned spin loops survived 13 h at ~551% CPU, then 12 more survived 22 h 39 m at load average 27. Both times the `2>/dev/null` ate the error and the next line printed success. | For a load generator, do not hand-roll one at all — `scripts/loadgen.sh` is the tested version, and its spinners also self-terminate on a deadline so a killed shell cannot leak them. Elsewhere: collect `$!` per spawn and kill by PID. Note `trap 'kill $BUSY'` fails identically — the variable is empty either way. **And do not reach for `trap 'kill -- -$$' EXIT INT TERM`**: this table used to recommend it, and it belongs in the left column rather than the right. Job control is off in a non-interactive script, so the shell does not lead its own process group and `$$` is not the pgid — measured on this box, a script with `$$=62617` was in pgid `62613`, and the kill fails with "No such process". It is the same shape as every other row: a cleanup line that looks correct and does nothing. |
| `rm -rf "$T"/go-build* "$T"/scratch-*` | A glob matching nothing **aborts the whole command**, including the glob that did match. | Documented below; reclaims 0 MiB and looks done. | `find … -print0 \| xargs -0`, or `setopt nullglob`. |

The shared lesson: **on this box a cleanup step is not verified by its exit code.** Re-`ls` the target
or measure the bytes.

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
| `rm -rf "$T"/go-build* "$T"/scratch-*` (multi-glob) | zsh `nomatch`: one unmatched glob aborts the **entire** command — `no matches found`, zero deletions | `find "$T" -maxdepth 1 \( -name 'go-build*' -o -name 'scratch-*' \) -print0 \| xargs -0 rm -rf` |

`-newermt` deserves the correction spelled out, because it is easy to conclude
from a failed relative form that the whole primary is unusable on darwin. It is
not. Only the *relative offset* form is rejected; an absolute timestamp works
and is the recommended liveness guard throughout this document. Note also that
in an agent shell `find` may be a shell-snapshot function resolving to `bfs`
rather than `/usr/bin/find`; both reject the relative form, but their error text
differs, so match on behavior, not on the message.

The zsh `nomatch` row is the one most likely to void a whole reclaim step
without anyone noticing (2026-07-28). Unlike bash, zsh treats an unmatched glob
as a hard error and **never runs the command at all**, so a two-glob `rm -rf`
where one pattern happens to be already-clean deletes nothing and reports
nothing but `no matches found`. Prefer `find -maxdepth 1 … -print0 | xargs -0`
for any multi-pattern delete: `find` returning no rows is an empty pipe, not an
aborted command. `setopt null_glob` would also work, but it is per-shell state
an agent cannot rely on inheriting.

A note on `find` and sizes: sorting by *apparent* size hides the real offenders
(sparse VM images look like 8 TB, occupy 13 GB). Rank by `du` output, not
`ls -l`. And a log that has been deleted while a process still holds it open
consumes disk but is invisible to both `find` and `du` — `lsof -nP +L1` is the
only way to see it.

---

## 0. `~/Library/Caches/*` — the shared Go caches, measured #1 on 2026-07-28

> **The "FIRST ACTION" section at the top of this file replaces this section as
> an action.** This section stays because it records how the shared cache was
> found and how fast it grew. Use `scripts/go-cache-reap.sh` instead of the `go clean -cache` this
> section recommends. `go clean -cache` empties the whole cache, so the next
> build of every lane starts cold. The reap takes the cache down to a size limit
> and keeps the entries that builds still use. Read the top section for why the
> cache refills, because no reap changes that.
>
> **Two rules in this section are NOT superseded.** "Measure these first. Do not
> clear them first" still holds, and `DRY_RUN=1` is how the reap obeys it. And no
> automation owns this sweep: run it by hand, because an operator can pause the
> fleet first and the daemon could not.

Until 2026-07-28 `~/Library/Caches/go-build` appeared in this runbook only as
one row of §2's Go-cache table, annotated "macOS-purgeable, see hk-pgtbr". That
annotation reads as *the OS handles this one*, and it is why prior sweeps walked
straight past it while chasing 100 MB scratchpad caches. It held **9.4 GiB —
more than everything else this sweep found combined** — and `go clean -cache`
returned all of it in seconds. `~/Library/Caches/golangci-lint`, which this
runbook did not mention at all, held another **1.1 GiB**.

```bash
du -sh ~/Library/Caches/go-build ~/Library/Caches/golangci-lint
```

**Re-measured 2026-08-10: `go-build` held 20 GiB and `harmonik-lane-gocache` held
19 GiB — 39 GiB of Go cache on a box with 9.8 GiB free.** The shared cache had
doubled from the 9.4 GiB above in 13 days, and the per-checkout caches below
had grown from 13 GiB in six days. Read every figure in this section as a floor,
not a size. They describe how fast these directories grow, and the growth rate is
the part that stays true.

Both caches grow with the number of lanes running, so the interval between
sweeps matters more than the sweep. A week of two or three active lanes is
enough to put the box back under the dispatch floor from a clean start.

**Measure these first. Do not clear them first.** This section used to call
`go clean -cache` "regenerable, zero risk" and put it at the head of the sweep.
That was wrong, and the daemon acted on the same belief until 2026-08-03.
`go-build` is the DEFAULT `GOCACHE`, so it is not one person's cache: both
lanes, every agent worktree, the daemon's merge builds and any terminal the
operator is using all read and write it at the same time. Clearing it mid-build
gave concurrent suites "could not import os/context/testing/... no such file or
directory" — and, worse, builds that reported success without rebuilding
anything. The cost is not one slow build. It is a wrong answer that looks like a
right one, on a box where several agents are deciding whether to merge.

Reclaim in this order instead. It runs from what one checkout reads to what
everything reads:

1. **Per-checkout Go caches — nothing reaps these, and they are the ones that
   grow.** `/Users/gb/github/harmonik-wt-cache` held 8.3 GiB on 2026-07-30 and
   grows about 3.7 GiB/day while lanes run.
   `~/Library/Caches/harmonik-lane-gocache/` holds one directory per checkout and
   **outlives the checkout that made it** — agent worktrees are made and dropped
   constantly here, so many of those directories belong to a checkout that is
   already gone. `go clean -cache` reaches neither path. See §2 for the full
   table of the seven places a Go cache lives.

   **On 2026-08-05 this directory held 13 GiB in 21 directories, fourteen of them
   owned by a checkout that no longer existed; deleting only those returned
   8.6 GiB.** They are about 500 MiB each now, not the 157 MiB this step used to
   quote, and the stale figure is part of why sweeps kept walking past them.

   The name is `<checkout-basename>-<8 hex>`, so a live worktree list sorts them:

   ```bash
   du -sh /Users/gb/github/harmonik-wt-cache ~/Library/Caches/harmonik-lane-gocache
   git -C /Users/gb/github/harmonik worktree list --porcelain |
     sed -n 's|^worktree ||p' | xargs -n1 basename | sort -u > /tmp/live-checkouts
   for d in ~/Library/Caches/harmonik-lane-gocache/*/; do
     base=$(basename "$d" | sed -E 's/-[0-9a-f]{8}$//')
     grep -qx "$base" /tmp/live-checkouts || echo "GONE $(basename "$d")"
   done
   ls -lt ~/Library/Caches/harmonik-lane-gocache   # newest first: the busy lanes sit at the top
   rm -rf ~/Library/Caches/harmonik-lane-gocache/<one-directory>
   ```

   A basename can match a live worktree and still be stale, because two
   checkouts at different paths take the same basename and a different hash.
   Read `ls -lt` too: a directory nothing has written to for days is stale
   whatever its name says.

   A directory whose checkout is gone, or that no build has written to for
   hours, is free to delete, and it costs that checkout one cold build. A
   directory a lane is compiling against right now is **not** free — that is the
   same mid-build hazard as the shared-cache step below, at one lane's scale
   instead of the whole box. This is why the step lists directories before it
   deletes them.

2. **Stale worktrees — §4.** Deleting a worktree directory never loses a commit;
   only uncommitted changes are at risk, and §4 shows how to find those first.

3. **The shared `go-build` and `golangci-lint` caches — last, and only when the
   box is quiet.** Check that nothing is compiling, and prefer to tell the lanes
   first:

   ```bash
   pgrep -fl 'go build|go test|golangci-lint|compile' | head
   go clean -cache             # the 9.4 GiB
   golangci-lint cache clean   # the 1.1 GiB (or rm -rf the directory)
   ```

   `pgrep` is a sample, not a guarantee — a build can start one second later.
   That is the reason this step is third and the reason no automation owns it.
   An operator can pause the fleet before running it. The daemon could not, so
   it no longer tries.

The lesson worth carrying past this one cache: *purgeable* is not *purged*.
macOS reclaims a purgeable cache under its own pressure signals, not because
your data volume is at 94%, so treat "the OS handles it" as a claim to measure
rather than a reason to skip a directory. Anything annotated that way in this
document deserves a `du` before it is believed.

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

**The thing it was hiding from is gone.** On 2026-08-03 the daemon stopped
deleting the Go build cache on any path (§0), so no automated process wipes
`GOCACHE` under a running build any more. A new command does not need a private
throwaway cache. Use the default `GOCACHE`, or `scripts/with-lane-gocache.sh`
for a per-checkout one. Retiring the inline `mktemp -d` habit is what stops this
section from refilling; the orphans already in `$TMPDIR` still have to be swept
by hand.

`scripts/with-isolated-gocache.sh` is **not** the producer: it uses a
`harmonik-gocache.XXXXXX` prefix and removes its directory in an `EXIT` trap
(plus `HUP`/`INT`/`TERM` forwarding), and zero `harmonik-gocache.*` orphans
exist. Its residual exposure is `SIGKILL` and killed panes, which no trap
survives. The fix shape is one cache per *session* — export once, reuse,
delete at session end — not one per command.

Also in `$TMPDIR`: Go's own `go-build*` scratch dirs (8 present, up to 188 MB
each) and `scratch-*` / `scenario-twin-*` test leftovers.

### There are at least seven places a Go cache lives in this project

A reclaim that checks one convention finds almost nothing:

| Path | Written by |
|---|---|
| `~/Library/Caches/go-build` | Go's default `GOCACHE` — **9.4 GiB, the biggest single source on the box; see §0** |
| `~/Library/Caches/golangci-lint` | the linter's default cache — **1.1 GiB**, missing from this runbook until 2026-07-28 |
| `$TMPDIR/tmp.XXXXXXXX` | bare inline `GOCACHE=$(mktemp -d)` — **unowned, the big one in `$TMPDIR`** |
| `$TMPDIR/harmonik-gocache.XXXXXX` | `scripts/with-isolated-gocache.sh` (self-cleaning except on SIGKILL) |
| `~/Library/Caches/harmonik-lane-gocache/<name>-<hash>` | `scripts/with-lane-gocache.sh`, used by the Go steps in `make fast`, `make core` and `make full`. **Persistent by design, never self-cleans, and OUTLIVES the worktree that made it** — persistence is what keeps a lane warm, but agent worktrees are created and discarded constantly here and nothing reaps what they leave. One directory per checkout: 157 MiB for `go build ./...` alone, larger once `-race` test objects land. This is the "one cache per session" shape recommended above. **`go clean -cache` does NOT reach these** — it clears whatever `GOCACHE` resolves to, which by default is `go-build`. Sweep with `rm -rf ~/Library/Caches/harmonik-lane-gocache`; deleting any one directory is safe and costs that checkout one cold build. Override the root with `HARMONIK_LANE_GOCACHE_ROOT`. |
| `$TMPDIR/go-build*` | the Go toolchain's own temp dirs |
| `~/.cache/h-*-gocache`, `/tmp/h-*/gocache` | long-lived named caches (assessor campaigns, isolated lanes) |
| `<worktree>/.harmonik/go-cache` | the daemon's merge gate (`internal/daemon/workloop.go`) |
| scratchpad `gc-*`, `*-gocache`, `lintcache-*` | per-agent-session caches (§1) |

## 3. `.beads/` history tiers — the historical 25 GiB root cause

`br` writes a **full copy of `issues.jsonl` on every write** (~6.0 MB each,
about one per minute during active work). Rotation *archives* these into
`.br_history-archive/` rather than deleting them.

This is the confirmed 256 GB disk-fill incident: **25 GiB across 15,072
snapshots**. Capped by hk-8vnwg (`internal/daemon/brhistoryrotate.go`):

| Directory | Policy | Constant |
|---|---|---|
| `.br_history/` | keep 20 on rotation, trimmed to 5 on bead-close | `brHistoryRotationDefaultKeep`, `brHistoryCloseTrimKeep` |
| `.br_history-archive/` | keep 300 **or** older than 7d | `brHistoryArchiveKeep`, `brHistoryArchiveMaxAge` |

The live tier has **two** thresholds, not one — the tighter close-trim is what
holds in-session growth down, and it only fires when a bead closes. Earlier
versions of this table listed only the 5, which makes a directory sitting at 20
look like a violation when it is exactly at rotation policy.

**Two failure modes remain, and both tiers suffer the first:**

1. **Every prune path is daemon-driven** — `daemon.Start` and on-bead-close in
   `workloop.go`. With the daemon off while `br` writes continue, nothing
   prunes. Observed at 100 snapshots (642 MB) against a close-trim of 5, and
   again on 2026-07-28 at **92 pairs / 618 MB against a rotation keep of 20**.
   Both tiers grow by this mechanism and at the same ~6 MB per snapshot; the
   25 GiB incident happened in the archive, which is why earlier versions of
   this section warned only about the archive. Measure both, especially after
   any stretch with the daemon stopped.
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
ls .beads/.br_history/*.jsonl | wc -l                        # pairs; rotation 20 / close-trim 5
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

**That loop cannot remove an orphan sidecar, and one existed on 2026-07-28.**
It derives each sidecar's name *from its payload*, so a `.jsonl.meta.json`
whose `.jsonl` is already gone is never named by any iteration and survives
every future run — the loop is structurally blind to exactly the files a
half-completed earlier prune leaves behind. Sweep the other direction
afterwards:

```bash
A=.beads/.br_history-archive
ls "$A" | grep '\.jsonl\.meta\.json\.archived-' | while read -r m; do
  base=${m%%.jsonl.meta.json.archived-*}; suffix=${m#*.jsonl.meta.json.archived-}
  [ -f "$A/${base}.jsonl.archived-${suffix}" ] || echo "orphan sidecar: $m"
done
```

Same shape applies to `.br_history/`. Neither direction alone is a complete
prune.

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
| `.harmonik/worktrees/<run-id>` | daemon run workspaces — the **only** thing the daemon reclaims for itself, and only when free space is below the watermark. It removes a directory here just when the run ID is not in its registry, so an in-flight run is never touched. |
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
to do this (see orchestrator-rules §CWD and commit discipline); operate from repo root.

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

### `.harmonik/gate-logs/` — kept on purpose, pruned by hand (hk-0kdr6)

One directory per run, holding the full output of every FAILED commit-gate
attempt. Nothing removes it, and that is deliberate: the gate log used to live in
the run worktree and die with it, which made a red run impossible to diagnose
after it reported. A gate is `make full`, so one failed attempt is megabytes and
one four-attempt run is tens of megabytes.

It is safe to delete outright, and safe to delete per-run. Delete the runs you
have already read:

```bash
du -sh .harmonik/gate-logs
find .harmonik/gate-logs -mindepth 1 -maxdepth 1 -type d -mtime +7 -exec rm -rf {} +
```

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

## What we deliberately did NOT delete (2026-07-23, revisited 2026-07-28)

Recorded so the next sweeper does not re-litigate these.

- **`.beads/.br_history-archive`, 1.7 GiB — left in place 2026-07-23, then
  pruned anyway on 2026-07-28.** The 2026-07-23 reasoning still stands on its
  own terms: 300 pairs, all inside the 7-day window, *exactly* at documented
  policy, so the cost is the policy's and the fix belongs in the constants (§3
  failure mode 2), not in a sweeper's hands.

  The 2026-07-28 sweep pruned it to 20 pairs anyway — **1.6 GiB** — against a
  directory that was again nominally compliant (300 pairs, oldest 6.98 days
  against a 7-day cutoff). Recorded as precedent with its reasoning, not as a
  new rule: *nominal* compliance was about to become non-compliance regardless
  — every one of those snapshots crossed the daemon's own hard-delete line
  within hours — and the contents were week-old copies of a machine-local
  ledger that has been rewritten thousands of times since. Deleting them
  removed nothing the daemon was not itself about to remove.

  The general shape, offered as judgment rather than instruction: "at policy"
  is a reason not to hand-prune when the policy is the thing keeping the files
  useful. When the policy is about to discard them anyway and the files are
  superseded copies, being at policy is a technicality. Check *where in the
  window* a compliant directory is sitting before deciding — 6.98 of 7 days is
  a different fact from 1 of 7.
- **`.harmonik/events/events.jsonl` (99 MB) and `baseline-2026-07-13` (94 MB) —
  left in place.** Append-only per EV-020; truncation reads as corruption to
  the bus. The baseline is compressible (§5) but was not touched.
- **macOS APFS local snapshots — operator action, do not attempt.** Three
  `com.apple.os.update-*` snapshots exist (`tmutil listlocalsnapshots /`) and
  likely account for much of the gap between measured file usage and the
  volume's reported usage — ~58 GiB unaccounted on 2026-07-23. Removing them
  needs `sudo tmutil deletelocalsnapshot <name>`. **This is an operator
  decision; an agent must surface it, not run it.** Still three, still present
  on 2026-07-28. Reach for this whenever the arithmetic does not close: it is
  the standing explanation for "I deleted 10 GiB and free space barely moved,"
  because a snapshot pins the blocks of files you just unlinked. If a step's
  `df` delta is much smaller than its `du` delta, suspect a snapshot before
  suspecting the delete failed — and note that this is the one case where the
  0-MiB rule above has an innocent explanation.
- **VM / container images — owner call, not sweeper territory.** `~/.lima` is
  3.3 GiB (`~/.lima/test`); OrbStack keeps its VM data outside `~/.orbstack`
  (which is only config). Deleting either destroys container/VM state that
  nothing else backs up. Ask the owner.

## Recurrence

§0, §1, §2 and §3 regenerate continuously; the rest are one-shot. Re-check them
whenever free space drops below ~20 GB, and after any stretch with the daemon
stopped — that is when **both** `.br_history` tiers grow unbounded. §2 grows
fastest of the four while the inline-`mktemp` habit lasts. §0 is the largest at
rest, so measure it first — but clear it in the order §0 gives, per-checkout
caches before the shared one.

Whatever you run, bracket it with `df` (§"Did the command actually do
anything?"). Every defect corrected in the 2026-07-23 rewrite of this file
would have been caught by a single before/after reading.
