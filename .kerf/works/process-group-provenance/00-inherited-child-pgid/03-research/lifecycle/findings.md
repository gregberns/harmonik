# Research — lifecycle (spawn attrs + orphan-sweep provenance)

## C0 (LOAD-BEARING) — Is the env-marker provenance path sufficient once the daemon-PGID stops being a project-constant?

### What the orphan sweep actually reads

`SweepOrphanHandlers` (`internal/lifecycle/orphansweep.go:303`) delegates candidate
enumeration to `OSHandlerProcessLister.ListOrphanHandlerPIDs`
(`orphansweep.go:225-277`). The match rule there is:

1. `ps -eo pid,ppid` -> keep processes with **PPID == 1** (`:245`).
2. For each, `ReadProcessEnviron(pid)` (`:257`) then `MatchesProvenanceMarker(env,
   projectHash)` (`:262`) -- i.e. match the **`HARMONIK_PROJECT_HASH` env var**
   (`provenance.go:177-185`, key `provenance.go:19`).
3. Exclude relay grandchildren by argv (`:270-273`).

**The PGID is never consulted by the implementation.** `ReadProcessEnviron`
(`provenance.go:195-210`) reads `/proc/<pid>/environ`, which exists only on Linux; on
darwin it returns `os.ErrNotExist`, the loop hits `continue` (`:259`), and **every**
candidate is skipped -> the darwin sweep is a structural **no-op** (confirms hk-o7x4w).

### Consequence for this work

- **Linux:** the env var is the **sole implemented** provenance signal. It is set on the
  child via `cmd.Env = spec.Env` (`handler.go:308`) with `ProvenanceEnvVar` injected by the
  caller (`handler.go:55-56`; usage pattern `spawnwait_pl014.go:22`). The env var is
  **inherited by grandchildren** across fork/exec, so re-parented grandchildren also carry
  `HARMONIK_PROJECT_HASH` and remain sweepable by env. Giving the child its own PGID
  **changes nothing** the Linux sweep reads. SUFFICIENT on Linux.
- **darwin:** env is unreadable (no `/proc`), so the sweep is *already* inert regardless of
  PGID. The fix does **not regress** darwin.

### The nuance that must be honored in the spec (not a code blocker)

The **spec** -- not the code -- treats PGID as provenance. PL-006a(ii)
(`process-lifecycle.md:370`) requires the marker be "BOTH ... (i) env var ... (ii) ... PGID ...";
OQ-PL-008 (`:1358-1363`) sets the *default* that "**PGID is the primary marker on darwin**;
environment variable is set for consistency but not read by the sweep." Under the OLD
design every child shares the daemon PGID, so a darwin PGID-match against the daemon's PGID
*would* (if wired) identify orphaned children -- that resolution path exists on paper.

Once each child leads its **own** group (`Pgid == child_pid`), there is no per-project
constant PGID to match against, so **OQ-PL-008's "PGID-primary-on-darwin" default is
foreclosed.** In practice nothing is lost (that path was never implemented; darwin sweep is
a no-op), but the spec must be honest: darwin post-crash provenance must move to OQ-PL-008's
alternative (a per-pid marker file at `/tmp/harmonik-<hash>-<pid>.marker`) **or** be
explicitly deferred as a known darwin gap. **This is the genuine design decision this work
surfaces** -- and it is a scoped follow-up, not a blocker for the kill-path fix.

### Bottom line for C0

Env-marker provenance is sufficient **today on Linux** (it is the only signal the sweep
reads) and the change does **not regress darwin** (already inert). The only real cost is
closing the theoretical "PGID-as-darwin-provenance" door, which OQ-PL-008 must re-answer.

## C1 — spawn-attribute variants (darwin vs linux)

- `spawndaemonchild_linux.go:20-26`: `{Setpgid:true, Pgid:pgid.Int(), Pdeathsig:SIGTERM}`.
- `spawndaemonchild_darwin.go:20-25`: `{Setpgid:true, Pgid:pgid.Int()}` -- **no `Pdeathsig`**
  (the field does not exist on darwin's `SysProcAttr`; the split file exists precisely for
  this).
- Change for both: `Pgid: 0`. With `Setpgid:true, Pgid:0` the kernel makes the child a new
  group leader whose PGID equals its PID (standard `setpgid(0,0)` semantics). Linux keeps
  `Pdeathsig:SIGTERM` (kernel-delivered SIGTERM on daemon-thread death -- orthogonal, keep).
- `EACCES` retry (child already `execve`'d before parent `setpgid`): Go's `os/exec` retries
  transparently when `SysProcAttr.Setpgid` is set (noted in `provenance.go:52-54`); `Pgid:0`
  does not change that.
- The **daemon's own** PGID (pidfile line 2, PL-002b `process-lifecycle.md:180`; liveness
  sensor `:1103`) is unaffected -- it records `getpgrp()` of the daemon, not the child. Only
  the *linkage* "recorded_pgid is passed into child spawn" (PL-006a(ii) `:372`) is severed.

## Parallel helper note

`SpawnSysProcAttr` (`provenance.go:59`, the PL-006a helper used for **br** subprocesses) is
distinct from `SpawnChildSysProcAttr`. br children are short-lived, fork no grandchildren
needing group-kill, and stay in the daemon group -- **out of scope**; leaving them there is
consistent with env-based provenance.
