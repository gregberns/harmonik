# 03 — Research: Option B1 — durable per-process provenance registry

**Work:** process-group-provenance · **Pass:** 3 · **Crew:** kilo · **Date:** 2026-07-22
Repo read READ-ONLY at `/Users/gb/github/harmonik` @ `650f359b`. Measurements taken live on this box (Darwin 25.3.0, arm64, `kern.maxproc=4000`).

## Executive summary

| Question | Answer |
|---|---|
| Does the PL-006d precedent exist in code? | **Yes** — stronger than pass 2 credited in one way (a full WM-026 atomic writer ships) and **materially weaker in another**: it answers `name → is this session ours?`, never `pid → is this process ours?` |
| Reusable atomic-write primitive? | **No.** WM-026 is a requirement satisfied by **four independent unexported copies**. B1 adds a fifth or extracts one. |
| Works on darwin where env marker doesn't? | **Yes, decisively** — moves the marker off the process onto the filesystem. `ps` + `kill(pid,0)`, no `/proc`. |
| Does `start_time` close pid recycling? | **Yes, ~500× margin — but the margin is 5× smaller than the work's own figure.** Measured today: **~195 pids/s, wrap ≈ 511 s (~8.5 min)**, not "~37/s, ~45 min." The crux is not resolution — it's **source portability**, with a Linux-specific trap. |
| Grandchild coverage? | **Not by itself, and the conjunct that would cover them fails in exactly the observed leak scenario.** |
| GC? | **Already solved in-repo by a better primitive than B1 proposes**: `flock` + recorded creator-pid. |
| Cost? | **~7.6 ms/spawn**, ~3.9 ms/reap; **+17.7 ms if start-time is read back by forking `ps`**. Negligible. |
| OD-7 — subsumes HC-044a's `.lock`? | **Yes, better than assumed: HC-044a's `.lock` was never implemented.** Retire it outright. Scheme count goes **down**. |

**Bottom line: B1 is feasible and is the only option that is darwin-native rather than darwin-tolerated. But it fixes the wrong half of the problem** — see §9.3.

---

## 1. The precedent — verified, and it is not the precedent pass 2 thinks it is

### 1.1 It exists and is fully implemented

Spec text is at `specs/process-lifecycle.md:439-465`; `:449` is the "Owner-proof mechanisms" clause. All three mechanisms ship:

| Mechanism | Spec | Implementation |
|---|---|---|
| (i) supervisor sentinel + `kill(pid,0)` | `specs/process-lifecycle.md:451` | `internal/daemon/orphansweep.go:357` `probeCoordinatorSentinel` |
| (ii) captain sentinel + live-pane-PID, falling back to `captain.pid` | `specs/process-lifecycle.md:453` | `internal/daemon/orphansweep.go:550` `probeCaptainSentinel` |
| (iii) crew durable registry record + live-pane-PID | `specs/process-lifecycle.md:455` | `internal/daemon/orphansweep.go:616` `probeCrewRegistrySessions`, reading `internal/crew/registry.go:177` `List` |

Writers: `cmd/harmonik/supervise/config.go:192`, `cmd/harmonik/captain.go:594`, `internal/crew/registry.go:88`.
Stale-record GC: `internal/daemon/orphansweep.go:500` `removeStaleSentinel` (unlink + parent-dir `fsync`).
Event surface already extended: `internal/core/daemonevents_hqwn59.go:814,832,841`.

So there **is** a live, shipped, spec-anchored "durable file says this thing is ours" pattern with writer, reader, liveness probe, GC and event payload. That much of pass 2's claim is correct.

### 1.2 Where the precedent is weaker than claimed — the lookup runs the wrong way

**The PL-006d registry is keyed by *name* and answers `name → protect?`. B1 needs `pid → ours?`. Not the same query; the existing artifact cannot answer the second.**

`internal/crew/registry.go:38-49`:

```go
type Record struct {
	SchemaVersion int       `json:"schema_version"`
	Name          string    `json:"name"`
	Type          string    `json:"type,omitempty"`
	SessionID     string    `json:"session_id"`
	Queue         string    `json:"queue"`
	Epic          string    `json:"epic"`
	Handle        string    `json:"handle"`
	StartedAt     time.Time `json:"started_at"`
}
```

**No `pid` field, no kernel start-time field.** `StartedAt` is a `time.Now()` observation written by the launcher — the exact value `02-components.md §4` correctly rules out. The spec concedes the gap in the sentence pass 2 cites as the precedent, `specs/process-lifecycle.md:455`:

> "Because the crew `Record` carries NO stored PID field, liveness MUST be a LIVE pane-PID probe (no schema change)."

The pid is recovered *at sweep time* by asking tmux (`orphansweep.go:646`). The registry never held process identity; tmux did, and the registry held only a *name* to look tmux up by.

**Consequence:** B1 cannot "extend the existing registry." It introduces a second registry with a different key, lifetime and consumer. Pass 2's framing — "extend rather than introduce a fifth provenance scheme" — is **not available**. What B1 *can* reuse: the file layout convention, `crew.Write`'s WM-026 sequence verbatim, `pidIsLive` (`orphansweep.go:599`), and `removeStaleSentinel`'s unlink+fsync GC. Real reuse of *mechanism*, not of *the record*.

### 1.3 Two things that should temper enthusiasm

**(a) Only one of the three owner-proofs is atomic.** `crew.Write` (`internal/crew/registry.go:88-149`) is a textbook WM-026 sequence. The sentinel writers are plain `os.WriteFile`, no fsync, no rename — `cmd/harmonik/supervise/config.go:198` and `:218`.

**(b) The precedent fails OPEN toward killing, repeatedly, and each failure has needed a patch.** In `probeCrewRegistrySessions` every error path is `continue` — not added to `excludeSessions` — and the unconditional sweeper kills everything not excluded (`internal/lifecycle/orphansweep.go:137-142`). A `WindowPanePID` error means "kill the live crew" (`orphansweep.go:650-655`). Two fix-forward beads are visible in the code for exactly this class: hk-aoapq (registry cold/corrupt → live-session fallback loop, `orphansweep.go:686-700`; `crew.List` returning a stub Record for corrupt files, `internal/crew/registry.go:202-206`) and hk-wuxg (write-once `captain.pid` goes stale across keeper `/clear` cycles → live captain reaped, `orphansweep.go:536-545`).

Fair warning, not a disqualifier: **the hard part of a durable-marker scheme is enumerating every way the marker can be absent-but-the-process-live and defaulting those to "do not reap."** B1's marker (per-pid, written per spawn) has *more* ways to go missing than a per-name file does.

### 1.4 Incidental drift found while verifying

`specs/process-lifecycle.md:689` (PL-019(g)) says the daemon MUST NOT touch `.harmonik/cognition/` "except (i) **reading** `supervisor.sentinel` and `supervisor.pid`." The shipped sweep also reads `captain.sentinel`/`captain.pid` (`orphansweep.go:558`, `:583`) and **writes** (unlinks) both sentinel families (`orphansweep.go:500` via `:576`, `:594`) — the latter mandated by PL-006d(i) at `:451`. PL-019(g)'s carve-out is descriptively false and narrower than PL-006d requires. Belongs with the D1–D8 drift batch.

---

## 2. WM-026 — the requirement is real; the reusable primitive is not

**Requirement:** `specs/workspace-model.md:558-570`; atomicity clause at `:566` (temp → `fsync(temp_fd)` → `rename(2)` → `fsync(parent_directory_fd)`, step (iv) REQUIRED). Cited across `specs/process-lifecycle.md:314, 685, 784, 878, 1243`, `specs/queue-model.md:275`, `specs/handler-pause.md:106`, `specs/workspace-model.md:583, 636, 669`.

**No exported shared helper. Four independent unexported implementations:**

- `internal/workspace/claudesettings_wm040a.go:336` `atomicWriteWithParentFsync` — the only *generic* one (`path`, `content`), unexported in `internal/workspace`.
- `internal/crew/registry.go:88` `Write` — full sequence, inlined, typed to `Record`.
- `internal/daemon/handlerpause_persist_m0k0a.go:186` `atomicWriteHandlerStateDaemon`.
- `cmd/harmonik/handler.go:428` `atomicWriteHandlerState`.

Near-misses that are *not* WM-026: `internal/core/eventidghwm.go:56` `WriteEventIDHWMAtomicNoSync` (no fsync, says so), `cmd/harmonik/supervise/config.go:198` (plain `os.WriteFile`).

**Verdict:** B1 adds a fifth copy or extracts a shared one. Extraction is right and is a small independently-valuable cleanup — but it is **work B1 creates, not work B1 inherits.**

---

## 3. Darwin readability — the walk, concretely

**Why the env marker fails.** `internal/lifecycle/provenance.go:195-199` — `ReadProcessEnviron` is `os.ReadFile("/proc/<pid>/environ")`. On darwin the path does not exist → always `ErrNotExist` → `OSHandlerProcessLister.ListOrphanHandlerPIDs` `continue`s at `internal/lifecycle/orphansweep.go:257-261` for every candidate. Empty set, unconditionally. Confirmed.

**Why the registry does not fail.** B1 relocates the marker from inside the process's address space (readable only via `/proc`) to the filesystem. The darwin sweep then needs only `ps` and `kill(pid, 0)`.

**The concrete walk (post-crash sweep, darwin):**

1. `os.ReadDir(".harmonik/procs/")` → N records: `{schema_version, pid, start_time, project_hash, role, run_id, pgid}`.
2. Drop records whose `project_hash != lifecycle.ComputeProjectHash(realpath(root))` (`provenance.go:33`). This is the project scoping hk-c6dt2 needs and that `SweepOrphanBr` (`internal/lifecycle/orphansweepbr.go:64-79`) lacks entirely.
3. One batched `ps -eo pid,ppid,pgid,lstart` — **43 ms measured** for the whole table (`lstart` MUST be the last column; it contains spaces: `"Tue Jun 16 10:21:05 2026"`, verified). Build `pid → (ppid, pgid, start_time)`.
4. Per record: pid present **AND** `start_time` matches exactly → **ours, reap**. Pid absent → gone; delete record. Pid present, `start_time` differs → **recycled, NOT ours, do not touch**; delete record. Record unparseable / `start_time` missing → **do not reap** (fail closed).
5. `kill(pid, SIGTERM)` → grace → `SIGKILL`, reusing `SweepOrphanHandlers` (`internal/lifecycle/orphansweep.go:295`).

**Three structural improvements this gets for free:**

- **It deletes the `PPID==1` filter.** Today enumeration is gated on `ppid != 1 → continue` (`internal/lifecycle/orphansweep.go:244-246`) — the whole of finding D6. The registry walk starts from *records*, not the process table, so parentage never enters the predicate. D6 dissolves rather than being patched.
- **It deletes the need for the relay-grandchild exclusion — finding D7's live hazard.** D7's danger: `IsRelayGrandchild` (`provenance.go:137`) is gated on `ReadProcessCmdlineArgs`, which is `/proc`-only (`provenance.go:151-153`), so on darwin `cmdErr != nil` always and the candidate is added to `matched` — a fail-OPEN exclusion armed the moment a darwin marker lands. Under B1 there is no exclusion to fail open: a `harmonik hook-relay` grandchild has no record, so it is never a candidate. **B1 converts a fail-open exclusion into a fail-closed inclusion.** Strongest technical argument in B1's favour; pass 2 does not make it.
- **It gives `SweepOrphanBr` a project scope.** `br` children spawn at `internal/brcli/adapter.go:146` with neither `SysProcAttr` nor `cmd.Env`; the sweep matches `comm=="br" && PPID==1` machine-wide. A registry record written by the `brcli` adapter is the only mechanism examined here that fixes hk-c6dt2 without a `setsid`'d daemon.

**Cost:** one `ps` (43 ms) + N `os.ReadFile`s — cheaper than today's Linux path (one `/proc/<pid>/environ` read per `PPID==1` candidate).

---

## 4. The pid-recycling defence — the crux

### 4.1 The work's own recycling numbers are stale by ~5×

Both `01-problem-space.md` and `02-components.md §4` assert "~37 pids/sec, ~45-minute wrap." **Re-measured today, twice:**

```
p1=$( (exec /usr/bin/true & echo $!) ); sleep 30; p2=$(…)   →  5086 pids / 30 s  ≈ 169/s
p1=$( (exec /usr/bin/true & echo $!) ); sleep 60; p2=$(…)   → 11712 pids / 60 s  ≈ 195/s
```

darwin allocates sequentially with `PID_MAX = 99999` → full-space wrap **≈ 511–590 s (~8.5–10 min)**, not 45. The box is running the agent fleet, i.e. normal operating condition; the 37/s figure was presumably a quiet box.

**Does this break the argument? No — but it changes its shape.** `02-components.md §4` argues sufficiency categorically: *"the same pid cannot recur within the same second… a complete discriminator for pid reuse here, not an approximation."* That is a property of a measurement that just moved 5×, not of the design. The correct normative statement is a **ratio with a re-check trigger**:

> The start-time conjunct is sound while `pid_space / pid_allocation_rate ≫ start_time_resolution`. Measured 2026-07-22: 99999 / ~195 s⁻¹ ≈ 511 s versus 1 s resolution — ~500× margin. A deployment whose sustained fork rate exceeds ~10⁵/s, or a platform whose start-time source is coarser than seconds, invalidates the conjunct and MUST fail closed.

State the ratio and the measurement date. Do not state "45 minutes."

### 4.2 Reading a running process's start time — darwin

**`ps -o lstart= -p <pid>`. Resolution: 1 second. Stable across reads. Verified:**

```
$ ps -o lstart= -p 1        (×5, same second)
Tue Jun 16 10:21:05 2026      ← identical all five times
```

Format is fixed-width and age-independent (`lstart`, unlike `start`, does not switch between `3:28AM` / weekday / date forms — confirmed against a 35-day-old pid 1 and a 0-second-old pid). Exit status 1 for a nonexistent pid — a clean "gone" signal.

Underlying kernel field: `kinfo_proc.kp_proc.p_starttime`, a `struct timeval` (µs) captured by `microtime()` at fork and never adjusted. Two consequences:

- Sub-second precision *is* available on darwin, but not through `ps` — it needs `sysctl {CTL_KERN, KERN_PROC, KERN_PROC_PID, pid}`.
- **The Go stdlib cannot do that sysctl.** Verified by compiling against Go 1.26.1: `syscall.CTL_KERN`, `syscall.KERN_PROC`, `syscall.KERN_PROC_PID`, `syscall.Kinfo_proc` are all **undefined** on `darwin/arm64` (only `Sysctl(name string)`/`SysctlUint32` survive, `$GOROOT/src/syscall/syscall_bsd.go:434,463`), and the CLI can't resolve the OID either (`sysctl kern.proc.pid.1` → `unknown oid`). The function that does it is `golang.org/x/sys/unix.SysctlKinfoProc` (`x/sys@v0.46.0/unix/syscall_darwin.go:502`).

  **`golang.org/x/sys` is NOT in `go.mod`** (direct deps: `google/uuid`, `expr-lang/expr`, `yaml.v3`, `rapid`), and the `lifecycle` package has an *explicit written policy against it* — `internal/lifecycle/monotonic_linux.go:12-14`:

  > "this project has a no-external-dependency policy for the lifecycle package and x/sys is not in go.mod."

  That policy has already cost one correctness deviation (`MonotonicNsSinceBoot` returns wall-clock instead of CLOCK_MONOTONIC, `monotonic_linux.go:19-31`, open as OQ-PL-009b).

  **So on darwin, B1's start-time source is `ps` — a fork+exec — unless this work also decides OQ-PL-009b.** A real, unbudgeted dependency; pass 2 does not name it.

### 4.3 Reading a running process's start time — Linux, and the trap

**Do not use `ps -o lstart=` on Linux.** It is not the same kind of value:

- darwin `lstart` = a frozen wall-clock stamp taken at fork. Stable by construction.
- Linux `lstart` = *computed*: `btime` (from `/proc/stat`, itself derived at read time as wall-clock minus uptime) **plus** `starttime_ticks / HZ` (`/proc/<pid>/stat` field 22). Because `btime` is re-derived per read and rounds to whole seconds, the *same* process can report `lstart` values differing by ±1 s across two `ps` runs, and a clock step (NTP) moves it.

  A 1-second field that jitters ±1 s has effective resolution ~2 s and, worse, is **not reflexive**: `read(P) == read(P)` can be false. Under B1 a genuinely-ours process then fails its own provenance check → fail closed → **not reaped → the leak persists silently.** Safe direction, but it turns the sweep into a coin flip.

  *Caveat: not executable here (darwin only). Mechanism stated from the procps computation; **must be measured on the Linux target before the spec commits to a source.** Pass-4 verification item, not established fact.*

- **The correct Linux source is `/proc/<pid>/stat` field 22 (`starttime`)** — clock ticks since boot, `sysconf(_SC_CLK_TCK)` (100 Hz typical → **10 ms**), never recomputed, immune to clock steps. Cheap (`os.ReadFile`, no fork); the repo already reads sibling `/proc` files the same way (`provenance.go:198`, `:154`). Parse hazard: field 2 (`comm`) may contain spaces and parens — split after the **last** `')'`.

  It is **boot-relative**, so a recorded value is comparable only within one boot. Acceptable (no harmonik process survives a reboot) **provided the record carries a boot identity** so a pre-reboot record is rejected rather than matched by coincidence. Add `boot_id`: `/proc/sys/kernel/random/boot_id` on Linux, `sysctl kern.boottime` on darwin. **The proposed schema has no such field and needs one.**

### 4.4 Start time of a process **we just spawned** — the overlooked half

`02-components.md §4` prescribes the expensive answer: "record the value read back from the kernel… via the same command the sweep will use." Per-child that is a **`ps -o lstart= -p <childpid>` fork+exec on every spawn — measured 17.7 ms** — plus a race: if the child dies inside that window, `ps` returns nothing, the record has no start time, and it must fail closed → **a record that can never be reaped**, i.e. B1's own leak.

**Cheaper and strictly more correct alternative, not considered by pass 2: record a bracket, not a point.** Capture `t_before = time.Now()` immediately before `cmd.Start()` and `t_after` immediately after. The child's kernel start time is *provably* in `[t_before, t_after]`. Store the pair; at sweep time a candidate matches iff its start time lies in the interval widened by one resolution unit.

- No fork on the spawn path (removes the 17.7 ms *and* the race).
- Not a "fuzz window" in the sense §4 rightly rejects — a fuzz window is a *tolerance* papering over two incomparable clocks; this is a *containment proof* between two reads of the same clock. Width = fork latency (<1 ms) + read resolution (1 s darwin / 10 ms Linux) against a ~511 s wrap. Margin unchanged.
- Requires the recorded clock and the source to share an epoch — true on darwin (both `CLOCK_REALTIME`); on Linux the bracket must be taken in the source's units (read `/proc/self/stat` ticks before and after, not `time.Now()`).

**Verdict on the crux:** `start_time` closes pid recycling with ~500× margin, and 1-second darwin resolution is fine. The crux is not resolution — **it is that "start_time" is three different values across the two platforms** (frozen wall-clock µs on darwin; ticks-since-boot on Linux; a jittering derived wall-clock in Linux `ps lstart`), the fine-grained sources are unreachable from Go stdlib on darwin, and the spec must therefore name a **per-platform source, a resolution, an epoch/boot identity, and a fail-closed rule for "source unavailable."** A spec that says only "record start_time" will be implemented three incompatible ways.

---

## 5. Grandchild coverage (OD-2) — the honest answer

### 5.1 Mechanics

A record is written by the daemon for a pid it spawned. `fork(2)` copies the environment; it does not copy a filesystem record. A grandchild has **no record and no way to acquire one** — it does not know the registry exists.

The env marker does not have this problem: `HARMONIK_PROJECT_HASH` is inherited by the whole subtree, arbitrarily deep, forever. **B1 trades away the one property the env marker has that matters — inheritance — for the one it lacks — darwin readability.** That trade should be the headline of the OD-1 decision record; it is currently a bullet in an *Against* list.

### 5.2 The four candidate conjuncts, against live evidence

From `ps -eo pid,ppid,pgid,etime,comm` on the running box:

```
91090     1  91086    03:06:47  harmonik
91164     1  91086    03:06:47  harmonik
91194     1  91086    03:06:46  harmonik
91231     1  91086    03:06:46  harmonik
91307     1  91086    03:06:45  harmonik     ← five live keeper watchers, dead leader 91086
23440 32783  23440    08:30:31  claude       ← substrate agent; parent 32783 is the TMUX SERVER
32783     1  32783    —         tmux new-session -d -s harmonik-a3dc45482890-captain … claude …
```

**(a) Group-membership conjunct.** Requires A3 (child leads its own group) — otherwise every child shares the daemon's PGID and the conjunct matches the daemon and every sibling. With A3 it *does* work while the leader is alive: verify pid==pgid is live and its start_time matches, then sweep the group. But **the leak scenario is precisely leader-dead.** Group 91086 above has a dead leader and five live members; once the leader is gone the group is a bare integer with no kernel-visible identity and no start time of its own, over a pid space that wraps every ~8.5 min. A daemon down 20 minutes cannot distinguish "group 91086, ours" from "group 91086, reissued." You can bound it heuristically (member started after the recorded child start_time and before the daemon's last heartbeat), but with an ~8.5-min wrap and unbounded downtime that window routinely contains a full wrap. **Rating: works while the leader lives; degrades to a heuristic exactly when needed. Not admissible as sole basis for a kill.**

**(b) Walking the ppid chain.** Dead on arrival. Every leaked process above has `PPID == 1`: reparenting to init *erases* the chain, and it is the definition of the swept population (`internal/lifecycle/orphansweep.go:244-246`). **Rating: vacuous.**

**(c) Env inheritance, Linux-only.** Works; is the status quo. Adds nothing on darwin, which is the point of this work. Produces an asymmetric spec — at least honest and testable, and probably the right thing to *write down*, but not a fix.

**(d) No coverage.** The default if nothing is chosen.

### 5.3 What "no coverage" costs — smaller than it sounds, for two reasons

**Reason 1: on the substrate path there is no grandchild to cover, and provenance already works.** `claude` (23440) is a child of the **tmux server** (32783), itself `PPID==1`. The daemon is not `claude`'s ancestor at all — the process is not in harmonik's tree. Its handle is the tmux session name `harmonik-a3dc45482890-captain`: portable, darwin-native, forgery-resistant, **already working** (`lifecycle.TmuxSessionPrefix`, `provenance.go:106`; consumed at `orphansweep.go:135`). `tmux kill-session` reaps the whole pane subtree in one verb. And `internal/handler/handler.go:273` returns early into `launchViaSubstrate` **before** the `SysProcAttr` line at `:309`, so the substrate path never had a PGID marker anyway. **The grandchild gap is scoped to the direct-exec branch only.** OD-5 should record this as "two explicitly-named regimes," not a carve-out.

**Reason 2: for direct-exec, the live kill path is the primary leak source, and A3 fixes it without any provenance.** `internal/handler/session.go:421` `Kill` signals the positive child pid only; its own comment (`:411-419`) explains why — the child joined the *daemon's* group, so `kill(-childpid, …)` addresses a nonexistent group (ESRCH) and `kill(-daemonpgid, …)` would signal the daemon and every sibling; "any grandchildren it forked are bounded by the caller's post-kill wait plus the daemon's orphan sweep." Give the child its own group (A3 — already landed three times per hk-me8ru) and `kill(-childpid, …)` reaches the whole subtree while the daemon lives. **That closes the leak at source for every case except "the daemon died mid-run."**

### 5.4 Answer to OD-2

> **B1 cannot identify a grandchild.** Records are per-pid and not inherited; the pid→record direction is unavailable for any process the daemon did not itself spawn. Of the four conjuncts, ppid-walking is vacuous (the leak population is `PPID==1`), env-inheritance is Linux-only and is the status quo, and group-membership works only while the group leader is alive — which is not the leak scenario.
>
> **What it costs:** the residual uncovered population is *grandchildren of direct-exec handler children, orphaned by a daemon that died before it could kill them*. Substrate-hosted work is not in that set (covered portably by tmux session-name provenance + `kill-session`); live-daemon kills are not in that set once A3 lands. The spec must say this in those words, and PL-INV-005's sensor must be scoped to match, or PL-006 will again claim a completeness it does not have (D6, one level down).

---

## 6. GC / staleness — solved, by a better primitive than B1 proposes

**Failure mode:** the daemon `SIGKILL`s (or power loss) between spawn and reap. Records survive with no owner; `.harmonik/procs/` grows unbounded and every stale record is a standing licence to kill a recycled pid.

**Three in-repo patterns, ascending fitness:**

1. **Stale-marker removal on probe** — `internal/daemon/orphansweep.go:500` `removeStaleSentinel`: `os.Remove` + `fsync(parent_dir)`, invoked whenever the recorded pid is dead (`:576`, `:594`); mandated at `specs/process-lifecycle.md:451`. Correct for B1: the sweep deletes every record it resolves (matched-and-killed, gone, or recycled). Steady-state growth is then bounded by *concurrent live spawns*, not history.

2. **Count-capped archive sweep** — `internal/lifecycle/queuearchivesweep_hkpycay.go:60` `SweepQueueArchives` (keep newest N per category, default 5, env-overridable). Belt-and-braces; not primary.

3. **`flock` + recorded creator-pid — the one B1 should copy.** `internal/lifecycle/orphansweep.go:854` `reconLockProbeStale`:

   ```go
   f, _ := os.OpenFile(lockPath, os.O_RDWR, 0o600)
   flockErr := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
   if flockErr != nil { return nil, false, nil }        // EWOULDBLOCK → actively held → NOT stale
   pid, parseErr := reconLockReadCreatorPID(f)
   if parseErr != nil { return nil, false, err }         // cannot prove dead → do NOT remove
   if orphanSweepIsPidLive(pid) { return nil, false, nil }
   return f, true, nil                                   // stale; caller unlinks, THEN closes
   ```

   Spec anchor: PL-006 "Stale reconciliation locks…", quoted at `orphansweep.go:753-763`.

   **Why better.** The kernel releases an advisory lock when the holding *file description* closes — on process death, including `SIGKILL` and panic. So "is this record's owner alive?" is answered by the kernel against the actual process, **not against a pid number**, and is immune to pid recycling by construction. It also gives B1 its serialization point free: the design note at `orphansweep.go:882` (spec at `:762`) is exactly the "do not unlink a record another daemon is acquiring" race B1 would otherwise have to invent a rule for. And its **fail-closed default is already right**: unparseable → do not remove — the polarity `probeCrewRegistrySessions` gets wrong.

   Cost: one open fd per live child, held for the child's lifetime. Bounded by `max_concurrent`; trivial.

   Caveat: the flock proves *the daemon that wrote the record* is alive, not that *the child* is. It is a record-staleness oracle, not a child-liveness oracle. B1 needs both — flock for "is this record trustworthy," `start_time` for "is this pid still the pid I recorded." Complementary, not alternatives.

**Recommended GC:** flock-held record + sweep-pass deletion (3 + 1), with the count cap (2) as a diagnostic backstop. No timer, no TTL, no mtime heuristic (mtime independently ruled out by operator direction, correctly).

---

## 7. Cost, and the fail-closed rule

### 7.1 Measured on this box (APFS)

| Operation | Measured | Note |
|---|---|---|
| WM-026 atomic write (open+write+fsync+rename+fsync(dir)), 200× | **7.62 ms** | dominated by the two fsyncs |
| unlink + fsync(parent), 200× | **3.85 ms** | reap-path delete |
| `ps -o lstart= -p <pid>`, 20× | **17.7 ms** | *only* if start time is read back by forking `ps` |
| `ps -eo pid,ppid,pgid,lstart` (whole table) | **43 ms** | once per sweep, not per spawn |

**Hot-path add per spawn: ~7.6 ms** with the §4.4 bracket, or **~25 ms** with the read-back-via-`ps` approach `02-components.md §4` prescribes. Against handler launches measured in seconds (worktree creation, settings materialization, `claude` startup, HC-056 `agent_ready` budgets), both are noise. **Cost is not an argument against B1.** It *is* an argument for the bracket, which removes a fork from the spawn path for free. Reap-path add: ~3.9 ms.

### 7.2 Fail-closed, as two distinct obligations that pull opposite ways

**(a) Write failure at spawn — fail the LAUNCH, do not launch unmarked.** If the record cannot be written durably, the daemon has spawned a process it can never prove is its own — a permanent leak silently created on the happy path. This is the polarity WM-040b already establishes for a comparable pre-launch durable write (`specs/workspace-model.md:665`: *"**Failure.** If the write fails … the daemon MUST NOT exec Claude … `ErrStructural` with `sub_reason: trust_seed_failed`"*). Mirror it: write the record **before** `cmd.Start()` keyed by run_id/session_id; on write failure return `ErrStructural`, do not spawn. Patch in pid + start_time immediately after `cmd.Start()` — a second WM-026 write, ~7.6 ms.

Ordering is not optional: a record written *after* `Start()` leaves a window in which a crash produces an unmarked live process. `specs/process-lifecycle.md:455` already establishes the "written BEFORE spawn — the 'this name is ours' marker" convention; reuse the phrasing.

**(b) Unresolvable marker at sweep — do not kill.** Missing/short/corrupt record, missing `start_time`, missing `boot_id`, `ps` unavailable, source unreadable: **skip.** Doubly true here because B1 is a *positive* matcher (a record is an authorization to kill) whereas the env marker is a *filter*. A missing record can never authorize a kill — the good news; what must be forbidden is any "no record but it looks like ours" fallback. Given that the shipped `probeCrewRegistrySessions` and `probeCaptainSentinel` both grew exactly such fallbacks under operational pressure (hk-aoapq, hk-wuxg — `orphansweep.go:686-700`, `:536-545`), this prohibition needs to be normative **and sensored**, not prose.

### 7.3 Two structural costs pass 2 does not list

- **PL-004's file surface is a closed set with a MUST NOT.** `specs/process-lifecycle.md:214` enumerates the daemon's per-project files and ends: *"The daemon MUST NOT read or write harmonik-owned state outside this surface."* `.harmonik/procs/` is not in it. **B1 requires a PL-004 amendment or it is born non-conformant.** Cheap, but absent from `02-components.md §5`'s ordering diagram.
- **Package placement is constrained.** The sweep lives in `internal/lifecycle`, which has a declared no-external-dependency policy (`monotonic_linux.go:12-14`) and a depguard component matrix (`.golangci.yml`). Writers would be `internal/brcli`, `internal/handler`, `cmd/harmonik`. A registry package all four import must be leaf-level — the constraint `internal/crew` satisfies ("depends only on stdlib and internal/core", `internal/crew/registry.go:1-2`) and that `queuearchivesweep_hkpycay.go:32-34` documents having worked around by duplicating a constant. Not hard; needs deciding rather than discovering.

---

## 8. OD-7 — does B1 subsume HC-044a's `.lock`? Yes, more cleanly than expected

**HC-044a** (`specs/handler-contract.md:812`) requires a pidfile at `.harmonik/worktrees/<run_id>/.lock`, "written atomically at subprocess spawn and removed on clean session termination," probed with `kill(pid, 0)`, used to fail `Launch` fast with `workspace_held_by_orphan`. **That is B1's record in different words, for one consumer** — same lifetime, liveness probe, atomicity obligation, purpose.

**Three findings that make OD-7 easier than assumed:**

1. **HC-044a's `.lock` has never been implemented.** No production code writes or reads `.harmonik/worktrees/<run_id>/.lock`. What ships is WM-013a's `lease.lock` at `<workspace_path>/.harmonik/lease.lock`, and the test suite **asserts the canonical path is NOT the HC-044a path** — `internal/workspace/leaselock_wm013a_test.go:61-65`: *"The canonical path must NOT be the HC-044a path … WM's path is authoritative per OQ-WM-005."* The conflict is acknowledged at `internal/workspace/leaselock.go:27` and `internal/workspace/orphansweep.go:62`. So OD-7 is not "fold in an existing scheme" — it is **"retire a spec-only scheme that never shipped and whose path a test already contradicts."** Strictly easier.

2. **The shipped `lease.lock` is a *different* record and is NOT subsumed.** `internal/core/leaselockfile.go:15-30`: `{RunID, PID, CreatedAt, TTLSec}` — `PID` is the **daemon's** (`:19-20`) and `CreatedAt` is a `time.Now()` observation. It answers "which daemon generation holds this workspace," not "which child is alive." Keep it.

3. **HC-044a contains the exact prohibited construct this work is writing a rule against.** `specs/handler-contract.md:812`: *"Stale pidfiles (PID not live, **or PID recycled to a non-handler process identifiable by argv check**) MAY be reclaimed."* That is argv-as-provenance, which `specs/process-lifecycle.md:475` (PL-007) forbids and which A6 is about to strengthen. Under B1 the argv check is unnecessary — `start_time` distinguishes "recycled" from "still ours" without argv. Retiring HC-044a removes a live A6 violation from the spec set, alongside BI-014a (`02-components.md §1.3`).

**Answer: yes.** Before: env var, PGID, tmux name-prefix, `br` comm+PPID, PL-006d sentinels/crew-registry, HC-044a `.lock` (spec-only), WM-013a `lease.lock`. After B1: registry, tmux name-prefix, PL-006d sentinels, WM-013a `lease.lock` — env var demoted to a Linux-only grandchild aid, PGID demoted from provenance to kill-handle (A3), `br` comm+PPID deleted, HC-044a retired. **Net −2, and the two removed are the two PL-007 forbids.** Pass 2's "four become five unless HC-044a is folded in" is pessimistic; the true accounting is a reduction, and that is B1's second-strongest argument.

---

## 9. Refuted, open risks, bottom line

### 9.1 Refuted or materially corrected

| Claim | Source | Finding |
|---|---|---|
| B1 can extend PL-006d's registry "rather than introduce a fifth provenance scheme" | `02-components.md §8 OD-1(a)` | **Refuted.** The crew `Record` has no pid and no kernel start-time field (`internal/crew/registry.go:38-49`); the spec says so (`specs/process-lifecycle.md:455`). It answers `name → protect?`; B1 needs `pid → ours?`. B1 is a new registry reusing the *mechanism*. |
| "atomic per WM-026" is inherited | `02-components.md §8 OD-1(a)`; task brief | **Corrected.** No reusable primitive — four unexported copies (`claudesettings_wm040a.go:336`, `crew/registry.go:88`, `handlerpause_persist_m0k0a.go:186`, `cmd/harmonik/handler.go:428`); the two sentinel writers are plain `os.WriteFile` (`supervise/config.go:198,218`). |
| "~37 pids/sec, ~45-minute wrap" | `01-problem-space.md`; `02-components.md §4` | **Refuted by measurement.** ~169–195 pids/s → wrap ≈ **511–590 s (~8.5–10 min)**. Conjunct still holds (~500× margin) but sufficiency must be a stated ratio with a re-check trigger, not a categorical "cannot recur within the same second." |
| `lstart` treated as one portable 1-second source | `02-components.md §4` | **Corrected.** Frozen wall-clock on darwin (stable, verified ×5); *derived, jitter-prone* on Linux (`btime` + ticks, re-derived per read). Correct Linux source is `/proc/<pid>/stat` field 22 (ticks since boot, ~10 ms, clock-step-immune) — boot-relative, therefore needs a `boot_id` field the proposed schema lacks. |
| Reading the kernel start time is free | implied by `02-components.md §4` | **Corrected.** Sub-second darwin start time needs `sysctl KERN_PROC_PID`, which **Go stdlib cannot reach** (`syscall.Kinfo_proc`/`CTL_KERN` undefined on darwin/arm64, verified by compile) and needs `golang.org/x/sys` — **not in `go.mod`, explicitly refused by the `lifecycle` package** (`monotonic_linux.go:12-14`, OQ-PL-009b). Otherwise darwin's source is a **17.7 ms `ps` fork per spawn**. |
| HC-044a's `.lock` is an existing scheme to fold in | `02-components.md §8 OD-7` | **Corrected, favourably.** Never implemented; shipped path is WM-013a's `lease.lock` and a test asserts the HC-044a path is wrong (`leaselock_wm013a_test.go:61-65`). B1 lets it be **retired**, also removing an argv-as-provenance clause PL-007 `:475` forbids. |
| Grandchild coverage is a bullet in an "Against" list | `02-components.md §8 OD-1(a)` | **Escalated.** It is the *defining* trade — inheritance swapped for darwin readability. Should be the headline of the OD-1 decision record. |

### 9.2 Open risks

1. **Linux `lstart` jitter is stated from mechanism, not measured.** Pass 4 must measure `ps -o lstart=` reflexivity on the Linux target before the spec names a source. If it jitters, Linux must use `/proc/<pid>/stat` field 22 and the spec carries two sources, two resolutions, two epochs.
2. **`golang.org/x/sys` is an unbudgeted dependency decision.** Either accept a `ps` fork per spawn on darwin, or this work also resolves OQ-PL-009b. Not a detail.
3. **Fail-closed erosion under operational pressure is the demonstrated failure mode of this exact pattern.** hk-aoapq and hk-wuxg both added "no record but it looks live → protect anyway" fallbacks to PL-006d within months of landing. B1's polarity is inverted (a record *authorizes* a kill), so the analogous erosion is "no record but it looks like ours → kill anyway" — destructive rather than merely leaky. The prohibition must be normative **and sensored**.
4. **Two writes per spawn is a new ordering obligation.** Record-before-`Start()` (fail launch on write failure), then patch pid+start_time. A crash between them leaves a record with no pid — classify as unresolvable (skip) and GC, not "kill run_id's processes."
5. **PL-004's file-surface MUST NOT** (`specs/process-lifecycle.md:214`) makes B1 non-conformant until amended. Absent from `02-components.md §5`'s ordering diagram.
6. **`SweepOrphanBr` coverage is real but conditional.** B1 fixes hk-c6dt2 only if `internal/brcli/adapter.go:146` writes a record. `br` calls are frequent and short-lived; ~11.5 ms (write + delete) per `br` invocation is a different cost profile from a handler launch and should be measured, not assumed.

### 9.3 The strongest argument AGAINST B1, stated as fairly as I can

> **B1 builds an excellent mechanism for identifying the processes that were never the problem.**
>
> The leak is grandchildren. B1's record is per-pid and not inherited, so it cannot see a grandchild — and the one conjunct that could (group membership) fails precisely in the observed failure mode: a dead group leader over a pid space that wraps every eight and a half minutes. The env marker B1 replaces has exactly the property B1 lacks: inherited by the entire subtree, forever, free, at zero write cost. B1's whole value proposition is "the env marker is unreadable on darwin" — but the correct comparison is not "unreadable marker vs. readable marker," it is "unreadable marker that *would* cover the leak vs. readable marker that *provably does not*."
>
> Meanwhile the two things B1 is genuinely good at are obtainable more cheaply:
> - **The live-daemon kill path** — the primary leak source — is fixed by A3 alone (child leads its own group, `kill(-childpid, …)`), already landed three times in this repo (hk-me8ru), needing no on-disk schema, no per-spawn fsync, no start-time source, no platform split, no GC, no PL-004 amendment, no `x/sys` decision.
> - **Substrate-hosted work** — where the agents actually run (`internal/handler/handler.go:273` returns to `launchViaSubstrate` before the spawn site anyone proposes changing) — is already covered portably by tmux session-name provenance plus `kill-session`, and is invisible to the `PPID==1` handler sweep on both platforms anyway (D6).
>
> That leaves B1's exclusive territory as: *direct-exec handler children, and their grandchildren, orphaned by a daemon that died before it could kill them, on darwin.* B1 covers the children. It does not cover the grandchildren. So the honest scope of B1's benefit is one generation deep in the narrower of the two spawn branches, in the crash case only — bought with a new on-disk schema, a per-platform start-time source, a dependency-policy decision, a two-phase spawn-path write with a new failure mode, a GC subsystem, and a spec surface that has already demonstrated it erodes toward fail-open under operational pressure.
>
> **B3 ("declare the darwin post-crash handler sweep out of scope") is not obviously wrong**, and pass 2's own note that "D6 shows the tmux path is uncovered either way, so this may be closer to the status quo than it looks" is the beginning of that argument rather than a concession.

### 9.4 Feasibility judgment

**B1 is feasible, buildable in roughly the shape pass 2 describes, and is the only option examined that is darwin-native rather than darwin-tolerated. Recommend it — but as a *second* change, decided on a narrower and more honest claim than pass 2 makes for it.**

Holds up:
- **Darwin readability: unqualified yes** (§3). Filesystem marker + `ps` + `kill(pid,0)`. No `/proc`. This is what B1 is for and it works.
- **Pid recycling: closed, ~500× margin** (§4), once the spec names a per-platform source, a resolution, a boot identity, and a fail-closed rule for "source unavailable."
- **Cost: negligible** (§7.1). ~7.6 ms/spawn with the bracket. Reject the read-back-via-`ps` prescription; use `[t_before, t_after]` containment (§4.4).
- **GC: already solved in-repo by a better primitive** (§6). Copy `reconLockProbeStale`'s flock discipline (`orphansweep.go:854`), not a TTL.
- **Scheme count goes DOWN, not up** (§8). Net −2, and the two removed are the two PL-007 `:475` forbids. Better argument for B1 than any pass 2 makes.
- **It disarms finding D7** (§3), replacing a fail-open exclusion with a fail-closed inclusion. Also better than any argument pass 2 makes.

Does not hold up:
- **The precedent does not transfer as a record; only as a mechanism** (§1.2).
- **Grandchildren are not covered, and cannot be** (§5). Must be written into the spec as an explicit non-goal with a named residual population, or PL-006 will again claim a completeness it does not have.

**Recommended sequencing if B1 is chosen:** land A3 (child leads own group; three in-repo precedents) and A4 (substrate kill parity) **first** — they close the live-daemon leak on both branches with no new schema. Then land B1 scoped explicitly to *post-crash, direct-exec, one-generation* coverage, with `br`-adapter marking (hk-c6dt2) as its main additional payoff. Write the grandchild gap down as a non-goal in the same commit that writes the marker. Take the `x/sys` / OQ-PL-009b decision deliberately rather than discovering it in implementation.
