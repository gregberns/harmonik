# 03 — Research: option B2 (daemon-group PGID + start-time conjunct)

**Work:** process-group-provenance · **Pass:** 3 (research) · **Assignment:** OD-1(b) / B2, and the
honest accounting of what it costs.
**Method:** repo read at `/Users/gb/github/harmonik` (read-only, HEAD), the BLOCKED patch at
`/Users/gb/github/harmonik-wt/kilo-preserved/hk-o7x4w-BLOCKED-do-not-merge.patch` (read, not applied),
and **live measurement on the operator's darwin box** (macOS 26.3.2, arm64, Go 1.26.1). Probes were
built under a scratchpad with `GOCACHE=/private/tmp/claude-502/-Users-gb-github-harmonik/kilo-gocache`.
No process was killed; nothing was written to the repo.

---

## Bottom line first

Two findings dominate, and they point in opposite directions.

1. **The either/or dissolves.** A POSIX **session** (daemon `setsid`s) + **per-child process group**
   (`SysProcAttr{Setpgid:true, Pgid:0}`) satisfies *both* provenance and grandchild kill-reach
   simultaneously. Measured working on darwin, including `getsid(2)` readability for arbitrary pids.
   So "B2 forecloses hk-n93gq" is **false** — that cost is an artifact of using the *group* as the
   namespace when the *session* is the correct coarser namespace. §4.
2. **The start-time conjunct does not save B2.** The conjunct authenticates an identity by
   interrogating the live process that holds it. At sweep time the prior daemon — the group/session
   leader whose pid *is* the recorded marker — is **dead by construction**, so there is nothing to
   interrogate. A start time recorded in the pidfile has no counterpart to compare against, and the
   collision that was actually observed (dead leader, live unrelated members) passes every check the
   conjunct can perform. §1.

Net: the *cost* the pass-2 dependency map assigned to B2 is refutable; the *fatal flaw* is not. B2 as
specified stays blocked. There is a repair (§1.5, a death-bound / heartbeat), and it converts B2 into
a design that is honest about needing a durable liveness record — at which point most of B1's
objection ("new on-disk state") applies to it too, at much lower write volume.

---

## 1. Does the start-time conjunct close pid recycling? — **NO, not for B2's shape**

### 1.1 How a start time is obtained, and at what resolution

**darwin** — `ps -o lstart=` is the portable source and resolves to **one second**:

```
$ ps -o pid,lstart= -p <two procs spawned 60 ms apart>
91768 Wed Jul 22 03:34:57 2026
91770 Wed Jul 22 03:34:57 2026     <- same second, indistinguishable
```
`etime` is likewise second-granular. Darwin `ps` has **no `sid` keyword** (`ps: sid: keyword not
found`) and `ps -o sess` prints `0` for a normal user — so `ps` gives pid/ppid/pgid/lstart and nothing
else useful here. A sub-second start time exists in `kinfo_proc.kp_proc.p_starttime` (a `timeval`) via
`sysctl KERN_PROC_PID`, but reaching it needs `golang.org/x/sys/unix` (**not** a current dependency —
`go.mod` has only `uuid`, `expr`, `yaml.v3`, `rapid`) or hand-rolled `sysctl` marshalling. It is not
needed; see 1.2.

**Linux** — `ps -o lstart=` is also one-second. `/proc/<pid>/stat` field 22 (`starttime`) is in clock
ticks since boot (`USER_HZ`, conventionally 100 Hz => **10 ms**), and field 6 is the session id. So
Linux is strictly better-provisioned. *(Not machine-verified — no Linux host in this environment.
Stated from documented `proc(5)` semantics; flag for a Linux spot-check before it becomes normative.)*

### 1.2 One-second resolution is sufficient — measured, with a corrected wrap figure

```
pid 90359 -> 91767 over 20.0 s  =  70.3 pids/sec
wrap over darwin's 99 999-pid space = 23.7 minutes
```
The problem space's "~37 pids/sec, ~45-minute wrap" (`01-problem-space.md:105`) was measured at a
quieter moment; under current fleet load the box is **twice as fast, wrap ~= 24 min**. The direction of
the correction matters (recycling arrives sooner), but the margin is unaffected: a given pid cannot
recur inside the same one-second bucket unless the wrap drops below ~1 s, i.e. ~100 000 pids/sec —
three orders of magnitude away. **`02-components.md:704-708`'s "granularity is sufficient" claim is
verified**, with the wrap number to be restated as load-dependent (24–45 min observed range) rather
than as a constant.

### 1.3 …but sufficiency of *resolution* is not the question. The question is *what gets compared.*

A start-time conjunct is sound in exactly one shape:

> record `(pid, start_time)` for process X; at match time read X's start time from the live process
> table; equal => same X, not a recycled pid.

That shape requires **X to be alive when the match runs**. It is precisely B1's shape (per-child
registry records, matched against live candidates) and it works.

B2 records `(daemon_pid, daemon_pgid = daemon_pid, daemon_start_time)` and matches
`row.PGID == recorded_pgid` over live candidates. The recorded start time belongs to the **daemon**,
and the daemon is dead — that is the precondition for the sweep running at all
(patch `orphansweep.go` keeps only `row.PPID == 1`, patch:397; `PriorDaemonOrphanPGID` at patch:863
*requires* `IsDeadPID(priorPID)`). There is no way to read a dead process's start time on either
platform without BSD process accounting, which is off by default. **The conjunct has no left-hand
side.**

Applying it to the *members* instead does not help:
- **Lower bound** (`member.start >= daemon.start`): members of a *recycled* group are strictly
  *younger* than the daemon, so they pass. Closes nothing.
- **Upper bound** (`member.start <= daemon.death`) is the discriminator that would work — a recycled
  group cannot form until pid G is freed, i.e. after death — but **death time is exactly what a
  crashed daemon does not record**. Clean shutdown removes the pidfile (PL-011 step 8), so a pidfile
  present at boot *means* crash, which *means* no death record.

### 1.4 The residual collision, in concrete numbers — measured live, not hypothetical

Probe over the live process table (642 processes, 504 distinct pgids):

```
DEAD-LEADER groups (leader pid absent from the table, members still live):
  35 groups · 95 live members · 76 of those members have PPID == 1
DEAD-LEADER sessions (same analysis via getsid):
  35 sessions · 93 live members · 76 with PPID == 1
```

Every one of those 35 groups **passes `PriorDaemonOrphanPGID` unchanged** — the leader is dead, the
recorded pid would equal the recorded pgid — and 76 live processes sit inside them with `PPID == 1`,
which is the sweep's candidate filter. Samples (fleet processes among them, corroborating the
problem-space capture at `01-problem-space.md:119-122`):

```
pgid=8605  sid=8605  members=[8610(ppid=1,harmonik)]           <- `comms recv --agent mike --follow`
pgid=40832 sid=40832 members=[40843(ppid=1,harmonik)]          <- `keeper --agent captain`
pgid=17473 sid=17473 members=[15 live `sleep` processes, all ppid=1]
pgid=10705 sid=10705 members=[bash, go, daemon.test, lifecycle.test, 3x sleep …]
```

Residual probability, stated plainly: **given >= 1 pid wrap (~24 min) of downtime, the recorded PGID is
effectively a uniform draw over the 99 999-pid space, so P(the sweep selects live unrelated
processes) ~= 35/99 999 ~= 3.5e-4 per boot**, with a mean blast radius of ~2.7 live processes and an
observed maximum of 15. Under ~24 min downtime the probability is near zero (the counter has not
returned to G). Two things make 3.5e-4 an *under*-estimate in practice:

- **Non-uniformity.** Pid allocation is a cyclic counter. A process created at time T and a process
  created at T + wrap receive *nearby* pids. The fleet creates group leaders continuously (keepers,
  crews, tmux panes, `comms --follow` watchers), so the reallocation of pid G is disproportionately
  likely to land on another fleet-spawned group leader — which is exactly what the group-91086
  incident was.
- **Repetition.** The daemon restarts often; each restart is another draw.

`internal/lifecycle/orphansweep.go:366`'s re-enumeration before SIGKILL is a pid-reuse guard *within*
a single sweep (between SIGTERM and SIGKILL). It does not touch this.

### 1.5 The only repair I can find that actually works — a death bound

To make B2 sound you must bound the prior daemon's **death**, so members with `start <= D_bound` are
provably pre-death (genuine) and members of a recycled group (which cannot exist before
`D + wrap`) are excluded. With heartbeat period h << wrap (h = 30 s vs wrap ~= 24 min => 48x margin),
`D_bound = last_heartbeat + h` is safe, and everything after it fails closed (missed orphans, never
wrong kills). Candidate sources already on disk, cheapest first:

- the last event timestamp in `.harmonik/events/events.jsonl` (a *content* timestamp, not mtime — it
  survives the operator's mtime objection). Caveat: an idle daemon's last event can be old, which
  loosens the bound and (safely) shrinks coverage.
- an explicit periodic line-5 rewrite of the pidfile.

This is a real repair, and it should be on the table in pass 4 as **B2'**. Note what it costs: a
periodic durable write — the same category of objection levelled at B1's registry, at far lower
volume (one file, one write per h, versus one write per spawn), and still without per-child identity
or grandchild coverage.

---

## 2. The step-ordering blocker (A7) — **verified; the amendment is small and safe**

**Verified in spec.** `specs/process-lifecycle.md:315` = PL-005 step 1 "Acquire the pidfile lock";
`:317` = step 3 "Execute the orphan sweep per §PL-006". PL-002b step 3 (`:179`) mandates
`ftruncate(fd, 0)` immediately after lock acquisition.

**Verified in code.** `internal/lifecycle/pidfile.go:110` (`fd.Truncate(0)`) inside `AcquirePidfile`;
called from `internal/daemon/daemon.go:809` inside `acquirePidfile`, which `startWithHooks` calls
first at `internal/daemon/daemon.go:935`. The sweep runs much later via
`internal/daemon/bootreconcile.go:207` (`RunOrphanSweep`). So the prior generation's line-2 PGID
(and any future line 4) is destroyed **two steps before** the only consumer that wants it.
`02-components.md:869-872` (A7) is correct.

**The amendment is ~6 lines and already written.** The BLOCKED patch does it at
`hk-o7x4w-BLOCKED-do-not-merge.patch:64-70`: read `lifecycle.ReadPidfile` *before* calling
`AcquirePidfile`, inside the same helper, and thread the value through `bootState.priorOrphanPGID`.

**Does it disturb crash-recovery ordering? No.** Three checks:
- **Concurrency.** A torn/partial pidfile can only be observed while a live writer holds the flock —
  and in that case our own `AcquirePidfile` fails `ErrPidfileLocked` and we exit 5, discarding the
  read. A writer that crashed between truncate and write leaves an empty file, the lock is
  kernel-released, `ReadPidfile` errors, and the patch's `if readErr == nil` guard yields 0 = "no
  marker" (fail closed). Both paths are safe.
- **No competing deleter.** `RemoveStalePidfile` (`internal/lifecycle/pidfile.go:247`) and
  `ProbePidfileLock` (`internal/lifecycle/pidfilelock.go:77`) have **zero production callers** on the
  daemon boot path (the only `ProbePidfileLock` caller is `cmd/harmonik/run.go:605`, a different
  process). Nothing removes the file before step 1, so the read is uncontested.
- **Out-of-package readers unaffected.** `cmd/harmonik/supervise/start.go:101` reads line 3 only, in
  the supervisor process, before launch.

**One real cost, not in the pass-2 note:** `ReadPidfile`'s signature
(`internal/lifecycle/pidfile.go:191`, `(pid, pgid, instanceID string, err)`) is already at four
returns; adding a line-4 start time makes it five. `02-components.md:722-725` already recommends a
`PidfileRecord` struct — concur, and note this is now forced, not optional, if A8 lands.

---

## 3. The setsid dependency — **confirmed, and worse than stated**

**`SetsidDaemon()` has zero production callers.** `internal/lifecycle/provenance.go:79`; a repo-wide
grep for `Setsid` outside tests returns only its own definition/doc lines plus three spawn sites.
Confirms hk-g1qby and `01-problem-space.md:67`.

**Session leadership today is an accident of the launch path — and there are *two* different
accidents, not one.**

| Launch path | `SysProcAttr` | Daemon ends up as |
|---|---|---|
| `internal/supervise/daemon_watchdog.go:397` (watchdog revive) | `{Setsid: true}` | session **and** group leader |
| `internal/supervise/supervisor.go:355` (Supervisor child) | `{Setpgid: true}` (Pgid 0) | group leader, **not** session leader |
| bare shell | job control | group leader, **not** session leader |

`internal/supervise/supervisor_watchdog.go:199` also sets `Setsid: true`, but it spawns the
*supervisor*, not the daemon.

**Live confirmation on this box** — the running daemon got its leadership from the watchdog path:
```
pidfile .harmonik/daemon.pid -> 65959 / 65959 / 019f8911-…
pid=65959 ppid=60295 pgid=65959   getsid(65959)=65959   <- session AND group leader
pid=60295 = `harmonik supervise _shim …`                 <- the watchdog's host
```

**EPERM is not just "must be tolerated" — under one live launch path it is unrecoverable.** Measured:

```
CHILD spawned with SysProcAttr{Setpgid:true}   (the supervisor.go:355 shape)
  pid=95616 pgrp=95616 sid=95476   groupLeader=true  sessionLeader=false
  setsid() -> -1 EPERM
  after:  pgrp=95616 sid=95476     <- still in the SUPERVISOR's session, permanently
```
`setsid(2)` fails EPERM when the caller is already a **process-group** leader, which the supervisor
path guarantees *without* granting session leadership. The daemon cannot self-remedy: there is no
fork-then-setsid escape from Go without re-exec. Consequences:

- **The correct precheck is `getsid(0) == getpid()` (am I a session leader?), never
  `getpgrp() == getpid()`.** The obvious guard is wrong in exactly the case that matters: it reads
  "already a leader, skip" for a daemon that is a group leader in someone else's session.
- **Goal 5 of the problem space ("group leadership is a property of the daemon, not of its launcher")
  is not achievable by an in-process `setsid()` alone.** Either the supervisor spawn at
  `supervisor.go:355` changes `Setpgid` -> `Setsid`, or the daemon re-execs itself. This should be an
  explicit decision in pass 4; OD-3 as written does not surface it.
- Tolerated-EPERM behaviour after the correct precheck: if `getsid(0) == getpid()`, no-op (the
  watchdog path — already correct). If not, `setsid()` and on EPERM emit a structured warning and
  **degrade the marker to unusable** (fail closed), never proceed as if the namespace were owned.

---

## 4. What B2 forecloses — **REFUTED. A session/group split gives both properties.** *(headline)*

### 4.1 The semantics, stated precisely

- `setsid(2)` — caller must **not** already be a process-group leader; creates a new **session**,
  makes the caller session leader *and* group leader of a new group; `SID = PGID = PID`; drops the
  controlling terminal.
- `setpgid(0, 0)` — makes the caller a **process-group** leader of a new group (`PGID = PID`)
  **within the same session**. It cannot move a process across sessions.
- Sessions strictly contain process groups. A child inherits **both** its parent's session and its
  parent's group unless it changes one.
- Therefore: daemon `setsid`s (session **S** = daemon pid) -> each child spawned with
  `SysProcAttr{Setpgid: true, Pgid: 0}` leads its **own group C** but stays in **session S** ->
  grandchildren inherit **C**. Provenance = `getsid(pid) == S`. Kill-reach = `kill(-C, sig)`.

### 4.2 Measured on darwin, end to end

```
LEADER   pid=69364 setsid -> sid=69364  err=<nil>  pgrp=69364
LEADER   second setsid -> EPERM                                   (leader-detection works)
CHILD    pid=69365 pgid=69365 sid=69364      <- own group, daemon's session  OK
GRANDCHILD pid=69370 pgid=69365 sid=69364    <- child's group, daemon's session  OK
kill(-69365, SIGKILL) -> grandchild alive=false  OK
```
Both properties hold at once. **hk-n93gq and daemon-scoped provenance are not mutually exclusive.**

### 4.3 Can Go express it, on both platforms?

Yes. `syscall.SysProcAttr` carries `Setsid bool`, `Setpgid bool`, `Pgid int` on both
(`$GOROOT/src/syscall/exec_bsd.go:19,22,36` and `exec_linux.go:74,77,91`), and `forkAndExecInChild`
applies `setsid` first, then `setpgid(0, Pgid)` (`exec_bsd.go:112,120`; `exec_linux.go:385,393`).
`Pgid: 0` => own group, documented in the struct comment. The existing
`lifecycle.SpawnChildSysProcAttr` (`internal/lifecycle/spawndaemonchild_darwin.go:20`,
`spawndaemonchild_linux.go:20`) needs only `Pgid: 0` instead of the recorded pgid; the Linux variant
keeps `Pdeathsig`, so the platform split survives untouched.

### 4.4 Reading the session id back — **the one implementation gap**

- **darwin:** `syscall.Getsid` **exists** (`$GOROOT/src/syscall/zsyscall_darwin_arm64.go:880`) and
  works for **arbitrary** pids. Probe over the whole table: **616 of 626 pids returned a SID, 0
  EPERM**, 10 ESRCH (exited between the `ps` snapshot and the call). Session is genuinely coarser than
  group here: **279 distinct sids vs 504 distinct pgids**.
- **Linux:** `syscall.Getsid` is **NOT** exported in the stdlib (`SYS_GETSID = 124` exists in
  `$GOROOT/src/syscall/zsysnum_linux_amd64.go:131`, no wrapper). Options, none large: raw
  `syscall.Syscall(syscall.SYS_GETSID, …)` behind a `_linux.go` build tag; read `/proc/<pid>/stat`
  field 6 (free, already the platform's idiom); or add `golang.org/x/sys/unix` (a new dependency).
  Linux `ps -o sid=` also works (procps). Caveat to record: in a foreign pid namespace,
  `getsid` returns 0 when the session leader is not visible — that must fail closed.

### 4.5 What the split does *not* fix — be honest

- **Recycling is identical.** The SID is the daemon's pid, and the leader is dead at sweep time. My
  probe shows **35 dead-leader sessions with 76 re-parented live members** — the same 35, because on
  this box every dead-leader group's leader was also a session leader. Moving from group to session
  **changes nothing about §1**.
- **`setsid`-ing descendants escape it**, exactly as they escape the PGID marker. PL-006a already
  concedes this for groups (`specs/process-lifecycle.md:376` region, OQ-PL-011). The tmux substrate is
  the standing example: tmux gives each pane its own session, so substrate-hosted runs
  (`internal/handler/handler.go:272-273` returns to `launchViaSubstrate` **before** the `:309` spawn
  attrs) are outside *both* namespaces. Coverage is not widened by the split.
- **Coverage is thin today, measured.** At this instant the live daemon's session and group each
  contain exactly **two** processes — itself and one `br` child:
  ```
  pid=4973  ppid=65959 pgid=65959 sess=true grp=true  /Users/gb/.local/bin/br dep list hk-joacj …
  pid=65959 ppid=60295 pgid=65959 sess=true grp=true  harmonik --project … --no-auto-pull
  ```
  Zero agent processes. Corroborates D6: the leak lives on the tmux-hosted path, which neither marker
  reaches.

**Verdict on item 4: the either/or dissolves.** Pass 2's dependency map should drop "B2 forecloses
hk-n93gq" (`02-components.md:777-782`) — with the session as the namespace, A3 and daemon-scoped
provenance are *independent*, and A3 need not wait on OD-1 for that reason. The ordering gate
survives only in its weaker form: OQ-PL-008 must still be resolved before HC-044 is edited, because
`specs/process-lifecycle.md:376`'s "match on the PGID on darwin" MUST becomes literally
machine-wide-unsafe once children lead their own groups (`02-components.md:760-766` — that part is
correct and unaffected).

---

## 5. The free win (hk-c6dt2) — **half verified: real as a scoping signal, inert as a fix**

**The spawn site is bare, as claimed.** `internal/brcli/adapter.go:146`
(`exec.CommandContext(ctx, a.brPath, args...)`) sets only `cmd.Dir` (`:155`). No `SysProcAttr`, no
`cmd.Env`.

**The scoping is already free — no spawn-site change needed at all.** With `SysProcAttr` nil, Go does
no `setpgid` (`$GOROOT/src/syscall/exec_bsd.go:120`), so the child inherits the daemon's group *and*
session. Measured above: live `br` pid 4973 carries `pgid = 65959` and `sid = 65959`. So a `br` child
does have a project-scoped group today — B2 does not create it, it merely proposes to *read* it.

**But the sweep that needs it is cross-generational, so the win does not survive §1.**
`SweepOrphanBr` is called from exactly one place, `internal/daemon/orphansweep.go:903`, inside the
boot-time `RunOrphanSweep`. Its filter is `comm == "br" && PPID == 1`
(`internal/lifecycle/orphansweepbr.go:64-79`) — a `br` child only reaches `PPID == 1` because its
daemon died. Adding `pgid == recorded_prior_pgid` therefore inherits the identical dead-leader
recycling hole.

What it *does* buy, stated exactly: **fail-closed containment.** Today the sweep kills other projects'
and other developers' `br` processes unconditionally. With any project scope — even an unusable one —
the sweep either matches its own or matches nothing. hk-c6dt2 stops being *destructive*; whether it
becomes *correct* or merely *inert* depends on §1.

**A cheaper, sounder alternative that this pass should record.** The daemon does **not** put
`HARMONIK_PROJECT_HASH` in its own environment — it is only appended to `handlerEnv`
(`internal/daemon/workloop.go:1093-1103`). One `os.Setenv(lifecycle.ProvenanceEnvKey, hash)` at
PL-005 step 0 gives *every* nil-`Env` child — `br` included — the marker by inheritance, with no
spawn-site edits, and it is a per-process fact that survives the daemon's death and is readable on
Linux. It does not help darwin. Worth naming in OD-1/A5 as a complement, not a substitute.

---

## 6. The fail-open landmine — **verified; the patch closes it, but only incidentally**

**At HEAD, the PL-017a(b) relay-grandchild exclusion fails OPEN on darwin.**
`internal/lifecycle/orphansweep.go:270-274`:
```go
args, cmdErr := ReadProcessCmdlineArgs(pid)
if cmdErr == nil && IsRelayGrandchild(args) {
    continue
}
matched = append(matched, pid)      // <- reached when cmdErr != nil
```
`ReadProcessCmdlineArgs` reads `/proc/<pid>/cmdline` (`internal/lifecycle/provenance.go:151-154`),
which never exists on darwin. So `cmdErr != nil` always, the guard never fires, and the candidate is
**included**. The "exclude" is written as `err == nil && excluded`, i.e. *unreadable => include =>
kill*. Masked today only because the environ read at `orphansweep.go:257-261` `continue`s first —
exactly as pass 2 (D7) states.

**The BLOCKED patch does disarm it**, by sourcing argv from the same portable `ps` row
(`patch:294`, `-eo pid,ppid,pgid,args`) and calling `IsRelayGrandchild(row.Args)` with no error guard
(`patch:416`); rows with fewer than 4 fields are skipped entirely (`patch:315`). So in *that* patch
the landmine is closed. **The hazard is that the two changes are separable**: any B2 implementation
that adds a darwin-readable matcher while leaving the `/proc`-based argv read in place arms the
landmine immediately. That is a one-line regression away at all times.

**What B2 must state normatively to keep the fail-closed constraint:**
1. Every *exclusion* predicate MUST fail toward **exclude** (do not reap) when its input is
   unreadable. Concretely: `if argvUnreadable || IsRelayGrandchild(args) { continue }`. The current
   idiom inverts this.
2. The identification predicate and every exclusion predicate MUST draw from the **same** enumeration
   snapshot, so an exclusion can never be unavailable on a platform where identification succeeds.
   The single-`ps` design in the patch satisfies this; two independent readers do not.
3. This is a **D7 drift fix that is independent of OD-1** (`02-components.md:815-819` already places it
   there) — it should land regardless of which branch wins, because it is currently one darwin-capable
   matcher away from killing live hook-relay processes.

---

## 7. What I verified / refuted, at a glance

| Claim | Source | Verdict | Evidence |
|---|---|---|---|
| A7: pidfile truncated 2 steps before the sweep | `02-components.md:869` | **verified** | spec `:315`/`:317`; `lifecycle/pidfile.go:110`; `daemon.go:935` vs `bootreconcile.go:207` |
| A7 amendment is small and safe | new | **verified** | patch:64-70; `RemoveStalePidfile`/`ProbePidfileLock` have no boot callers |
| `SetsidDaemon()` has zero production callers | `01-problem-space.md:67` | **verified** | `provenance.go:79`; repo-wide grep |
| `daemon_watchdog.go:397` is the only source of daemon session leadership | `01-problem-space.md:64` | **verified, but incomplete** | `supervisor.go:355` gives group-only leadership, which *blocks* setsid |
| setsid EPERM must be tolerated | assignment | **verified + sharpened** | EPERM is unrecoverable under `supervisor.go:355`; precheck must use `getsid`, not `getpgrp` |
| `lstart` resolution beats the pid wrap | `02-components.md:704` | **verified** | 1 s vs 23.7 min measured |
| "~37 pids/sec, ~45-min wrap" | `01-problem-space.md:105` | **corrected** | 70.3 pids/sec, 23.7 min under current load |
| Start-time conjunct closes B2's recycling | operator direction, `01-problem-space.md:109` | **REFUTED** | the authenticated identity is dead at sweep time; 35 dead-leader groups / 76 live PPID==1 members pass every check |
| B2 forecloses hk-n93gq | `02-components.md:777` | **REFUTED** | session+per-child-group measured working on darwin |
| `getsid` usable on darwin for arbitrary pids | new | **verified** | 616/626 OK, 0 EPERM |
| `br` adapter sets neither `SysProcAttr` nor `Env` | `01-problem-space.md:59` | **verified** | `brcli/adapter.go:146,155` |
| B2 fixes hk-c6dt2 "for free" | `02-components.md:773` | **partially refuted** | scoping is free and already present; the *sweep* is cross-generational, so it inherits §1. Buys containment, not correctness |
| Relay exclusion fails open on darwin | `02-components.md:862` (D7) | **verified** | `orphansweep.go:270-274` + `provenance.go:151-154` |

---

## 8. Open risks

- **R1 — Linux side unverified.** `/proc/<pid>/stat` field 6 (session) and field 22 (starttime), and
  the absence of stdlib `syscall.Getsid` on Linux, are stated from documentation and from
  `$GOROOT` inspection. A Linux spot-check is cheap and should precede any normative sentence.
- **R2 — `getsid` in a pid namespace** can return 0 for a leader outside the caller's namespace.
  Must fail closed. Only bites containerised deployments.
- **R3 — session membership is not a *project* marker.** SID = the daemon's pid, so it identifies a
  *daemon instance*, not a project. Two daemons for two projects have different SIDs, so it is
  project-scoping *in effect* — but if the spec says "project-scoped," that is imprecise, and it
  breaks the moment two daemons ever share a session.
- **R4 — the `Pgid: 0` change touches a live invariant.** `internal/handler/session.go:404-419`
  documents at length that `Kill` targets the positive pid *because* children join the daemon's group
  (hk-4c7kw). Flipping to `Pgid: 0` makes `kill(-childpid)` correct and makes the current comment
  false; the "exactly one `cmd.Wait()`" discipline must be re-checked against group-directed
  signalling.
- **R5 — `internal/handler/handler.go:272-273`.** Any spawn-attr change covers the direct-exec branch
  only. C4 stands.
- **R6 — B2' (the heartbeat repair) has not been costed.** It needs a write cadence, a fail-closed
  default when the bound is stale, and a decision on whether `events.jsonl`'s last timestamp is an
  acceptable source or a layering violation.

---

## 9. Feasibility judgment

**B2 as written: NOT feasible. Do not adopt.** Not because it is hard to implement — the BLOCKED
patch shows it is ~60 lines — but because its central safety claim cannot be made true by the
mechanism proposed for it. The conjunct that was supposed to close the hole cannot be evaluated at
the moment it is needed.

**B2' (B2 + a durable death bound) is feasible**, and is the version pass 4 should actually weigh
against B1. It keeps B2's genuine advantages (no per-spawn write, works with the marker children
already carry by inheritance, containment for `br`), and it pays an honest price: one periodic durable
write, and an explicit statement that coverage after the last heartbeat is forfeited.

**Independently of which branch wins, three things this pass established should land:**
1. Use the **session**, not the group, as the daemon-scoped namespace, and give each child its own
   group. It is strictly more capable, measured working on darwin, and it removes the
   B2-versus-hk-n93gq trade entirely.
2. Fix the setsid precheck story — `getsid`, not `getpgrp`, and settle `supervisor.go:355`.
3. Land D7 (fail-closed exclusions) now; it is one darwin-capable matcher away from being live.

### The strongest argument AGAINST B2, stated as fairly as I can make it

*Even granting everything favourable — that the session/group split dissolves the hk-n93gq cost, that
the A7 reordering is six safe lines already written, that `br` children already carry the marker for
free, that the conjunct's one-second resolution has a 1400x margin — B2 still asks the sweep to trust
a number whose meaning expired when the process that gave it meaning died. Every other provenance
scheme in this repo authenticates something that is still there to be asked: the tmux name prefix is
carried by the live session, the env marker is carried by the live process, B1's registry row is
matched against the live candidate. B2 alone authenticates a ghost. The 35 dead-leader groups holding
76 live re-parented processes on this box right now are not a tail risk to be priced — they are the
same shape as the group-91086 incident, standing in the process table at the moment of writing, and
each one would be accepted by the eligibility predicate the patch already implements. A design whose
safety argument is "the number probably has not been handed out again yet" is not a provenance
mechanism; it is a timer.*

The honest rebuttal — and it is a good one — is that B1 has an unpriced hole of its own (OD-2:
per-pid records do not cover grandchildren, and the grandchild leak is the actual leak), and that B3
may be closer to today's real behaviour than it looks. This pass does not adjudicate that; it removes
B2 from the running in its current form and hands pass 4 a repaired variant to weigh instead.
