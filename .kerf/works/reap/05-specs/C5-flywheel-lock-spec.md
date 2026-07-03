# C5 — Single-flywheel-per-project lock — Change Spec

**Component:** Assert at most one flywheel owner (daemon ⊕ supervised-Pi) per project; a second start is refused with a clear exit code + diagnostic, preventing the dual-orchestrator collision.
**Bead:** hk-li14r · **Goal:** G6 · **Analysis gap:** #6
**Spec home:** `specs/process-lifecycle.md` §4.1 (new sub-clause **PL-002c** flywheel-lock) + §4.5 PL-019(c) cross-ref; `specs/operator-nfr.md` §8 (absorb existing exit code **25** `supervisor-already-running` + add new exit code **24** `flywheel-already-running` + ON-001 catalog + new `CommandSupervise` `CommandName` + `CommandExitCodeSet` registration).

---

## Requirements (carried forward from 03-components.md)

- **C5-R1** — Define the canonical flywheel-ownership lock relationship between the daemon pidfile lock (PL-002) and the supervisor lock (PL-019c), such that a SECOND flywheel orchestrator for the same project cannot run concurrently with the first regardless of which layer already holds ownership.
- **C5-R2** — A second attempt to bring up the flywheel for a project that already has a live owner MUST be REFUSED with a specific non-zero exit code (allocated from operator-nfr §8) and a diagnostic naming the live owner (PID + pidfile/lockfile path).
- **C5-R3** — The lock MUST be an fd-lifetime advisory `flock` (kernel-auto-released on crash). POSIX `F_SETLK` is FORBIDDEN.
- **C5-R4** — The first (live) owner MUST be undisturbed by the refused second attempt — no state mutation, no signal, no config rewrite.
- **C5-R5** — A STALE lock (owner crashed, lock auto-released, marker file remains) MUST be reclaimable by a subsequent start without operator intervention.
- **C5-R6** — Tests MUST cover: (i) second start refused while first is live; (ii) start succeeds after first owner exits (stale lock reclaimed); (iii) first owner unaffected by refused second start.

## Research summary (from 04-research/C5)

Two INDEPENDENT fd-lifetime advisory locks exist; neither alone prevents the daemon⊕Pi collision. The daemon pidfile lock (PL-002/PL-002a, `internal/lifecycle/pidfilelock.go`) does `Flock(LOCK_EX|LOCK_NB)` on the pidfile + the PL-002a stale-disambiguation (`kill(pid,0)` probe → reclaim or refuse, exit 5) — it prevents a SECOND DAEMON. The supervisor lock (PL-019c, `cmd/harmonik/supervise/start.go:98-126`) does `Flock(LOCK_EX|LOCK_NB)` on `supervisor.lock`, exit 25 if held — it prevents a SECOND SUPERVISED-PI. The two are orthogonal: a daemon boot checks the pidfile lock; a `supervise start` checks `supervisor.lock`; neither checks the other. In the incident, killing the auto-pull DAEMON tripped a separate live `harmonik-pi` SUPERVISOR doing the same work — "one daemon + one independent Pi" is a permitted state, both burning credit on the same beads. **Recommended design (research RQ2, Option A):** a dedicated `.harmonik/flywheel.lock` that BOTH the flywheel-topology daemon boot AND `supervise start` acquire (flock `LOCK_EX|LOCK_NB`) before any state write — one canonical ownership assertion, avoiding the cross-file TOCTOU of Option B/C. The lock MUST be scoped to the flywheel topology so a plain non-flywheel `harmonik --project` daemon is unaffected (research R1). The fd-lifetime + stale-reclaim pattern is fully precedented in `pidfilelock.go` (`ProbePidfileLock`, the PL-002a/PL-024 two-step). **Exit code (research RQ4):** the central §8 registry is allocated 0–23; the next free code is **24** — C5 must ALLOCATE it (`flywheel-already-running`), add a §8 catalog row, and register it in a `CommandExitCodeSet` or `VerifyCommandExitCodeSets` fails. Adjacent inconsistency FLAGGED for the integration pass (do NOT silently fix): code 25 (`supervisor-already-running`) is used as a LOCAL const in `start.go:23` and referenced by PL-019(c) as "PL-INTERIM pending ON absorption" but is NOT in the central registry; and `start.go:19` defines a LOCAL `ExitCodeDaemonDown=17` that collides semantically with the registry's code-17 `multi-daemon-target-missing`.

## Approach

A dedicated flywheel lock, scoped to the flywheel topology. Decisions recorded:

- **Decision 1 (design, research Option A):** a single `.harmonik/flywheel.lock` acquired by BOTH start paths. The existing pidfile lock (PL-002) and supervisor lock (PL-019c) are RETAINED for their narrower duties (second-daemon / second-supervisor); the flywheel lock is the additional, canonical single-flywheel-owner assertion. This avoids the cross-file TOCTOU of checking one lock from the other path.
- **Decision 2 (scope, research R1):** the flywheel lock is acquired ONLY when the boot is part of the flywheel topology — gated on a flag/config (e.g. `--flywheel` on the daemon boot, and unconditionally on `supervise start` since `supervise` IS the flywheel). A plain `harmonik --project` daemon doing non-flywheel work does NOT acquire it and is unaffected (no regression of normal daemon use).
- **Decision 3 (marker content):** the lock file records the owner's `{pid, layer, started_at, project_hash}` (layer ∈ {daemon, supervisor}) so the refused-second diagnostic can name the live owner (C5-R2).
- **Decision 4 (stale reclaim, research RQ3):** generalize `ProbePidfileLock`'s two-step into a `ProbeFlywheelLock`: `Flock(LOCK_EX|LOCK_NB)` → on `EWOULDBLOCK` → held → refuse with exit 24 + diagnostic; flock OK + recorded-PID dead (`kill(pid,0)` ESRCH) → stale → remove marker + proceed (PL-024 discipline); flock OK + PID alive → ambiguous → refuse.
- **Decision 5 (acquire-before-write, C5-R4):** the flywheel-lock acquire MUST precede ANY state write on BOTH paths (the supervisor path already orders flock before `WriteSentinel`; the daemon path acquires it right after the pidfile lock, before queue-load/auto-pull). `LOCK_NB` means the second acquirer gets `EWOULDBLOCK` and exits BEFORE touching state — the first owner is undisturbed.
- **Decision 6 (exit code, research RQ4):** allocate **code 24 `flywheel-already-running`** in operator-nfr §8 + ON-001 catalog + `CommandExitCodeSets` for the daemon (`--flywheel`) and `supervise start` commands.

### Spec text to add — PL-002c (process-lifecycle.md §4.1, after PL-002b)

> **PL-002c — Single-flywheel-per-project lock**
>
> The daemon pidfile lock (§PL-002) prevents a second DAEMON; the supervisor lock (§PL-019(c)) prevents a second SUPERVISOR. These two fd-lifetime advisory locks are orthogonal — a daemon boot checks only the pidfile lock, a `harmonik supervise start` checks only `supervisor.lock` — so the state "one daemon + one independent supervisor, both driving the same project's beads" is permitted by neither lock and is the dual-orchestrator collision of the 2026-05-30 incident. To assert that a project has AT MOST ONE flywheel owner, the daemon (when booting as part of the flywheel topology) AND `harmonik supervise start` MUST both acquire a single canonical flywheel lock before proceeding.
>
> **Lock surface.** The flywheel lock is a fd-lifetime advisory lock at `.harmonik/flywheel.lock`, acquired via `flock(LOCK_EX|LOCK_NB)` (the §PL-002a primitive). POSIX process-lifetime `fcntl(F_SETLK)` is FORBIDDEN (any fd-close releases it). The kernel releases the lock automatically on the owner's termination, clean or crash. The lock file records the owner's `{pid, layer, started_at, project_hash}` where `layer ∈ {daemon, supervisor}`, written after acquire following §PL-002b atomicity, so a refused second attempt can name the live owner.
>
> **Scope.** The flywheel lock is acquired ONLY for flywheel-topology boots: the daemon acquires it when booting under the flywheel configuration (a `--flywheel` flag / config), and `harmonik supervise start` acquires it unconditionally (supervise IS the flywheel layer). A plain `harmonik --project` daemon doing non-flywheel work MUST NOT acquire the flywheel lock and MUST be unaffected by it.
>
> **Acquire ordering (first-owner-undisturbed).** The flywheel-lock acquire MUST precede ANY state write on both paths. On the daemon path it is acquired at §PL-005 **step 1.5** — immediately after the §PL-002 pidfile lock (step 1) and BEFORE the §PL-005c boot-backoff gate (step 1.6, C6), the orphan sweep (step 3), queue-load, auto-pull, and dispatch. On the supervise path it is acquired BEFORE the sentinel / config / tmux-session writes of §PL-019. Because the lock is `LOCK_NB`, a second acquirer gets `EWOULDBLOCK` and exits before mutating any state, signalling the first owner, or rewriting config — the first (live) owner is undisturbed (C5-R4). (The flywheel-lock gate at step 1.5 and the boot-backoff gate at step 1.6 are DISTINCT startup steps with distinct labels; neither shares "1.5".)
>
> **Refusal + diagnostic.** When the flywheel lock is held by a LIVE owner, the second attempt MUST refuse with [operator-nfr.md §8] code 24 (`flywheel-already-running`) and MUST print a diagnostic to stderr naming the live owner: its PID, its layer (`daemon` / `supervisor`), and the lock-file path. The daemon's pidfile-lock refusal (exit 5) and the supervisor-lock refusal (exit 25) are unchanged for their respective narrower duties; the flywheel-lock refusal (exit 24) is the cross-layer single-owner assertion.
>
> **Stale reclaim.** A stale flywheel lock (owner crashed, kernel released the advisory lock, the marker file remains) MUST be reclaimable without operator intervention. Disambiguation follows §PL-002a / §PL-024: attempt `flock(LOCK_EX|LOCK_NB)`; on success, read the recorded PID and probe `kill(pid, 0)` — if the PID does NOT respond (ESRCH), the marker is stale → remove it and proceed; if the PID responds, the state is ambiguous (recycled PID) → refuse with code 24. The probe-then-reclaim is the serialization point; the sweep MUST NOT racily remove a marker currently being acquired by another process (the `EWOULDBLOCK` observation is authoritative — the lock is in active use).
>
> Tags: mechanism
> Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent

### Spec text to add — operator-nfr.md §8 + ON-001 + commandcodes

Performed in the ordered sequence of the "Ordered integration obligation" above (absorb 25 → add `CommandSupervise` → add 24):

- **§8 taxonomy, code 25 (absorb-first)** — add row: `25 | supervisor-already-running | `harmonik supervise start` found `.harmonik/cognition/supervisor.lock` held by a live supervisor per [process-lifecycle.md §4.5 PL-019(c)]. | — (refusal before event-bus init) | Identify the live supervisor from the lockfile; stop it before starting another.` (the value already used at `cmd/harmonik/supervise/start.go:23`).
- **§8 taxonomy, code 24** — add row: `24 | flywheel-already-running | A flywheel-topology daemon boot or `harmonik supervise start` found `.harmonik/flywheel.lock` held by a live owner per [process-lifecycle.md §4.1 PL-002c]. | — (refusal before event-bus init on the supervise path; `daemon_startup_failed{failure_mode="flywheel-already-running"}` on the daemon path after PL-005 step 0) | Identify the live flywheel owner from the lock-file diagnostic (PID + layer); stop it before starting another.`
- **ON-001 / §4.1 ON-003 catalog** — register codes 25 and 24 with detection rule, symptom, event, remediation.
- **`internal/operatornfr/commandcodes.go`** — add a `CommandSupervise` (`supervise` / `supervise start`) `CommandName` + `CommandExitCodeSet` declaring codes 24, 25 (and 0/17 already in use on the supervise path); add code 24 to the `CommandDaemon` set (for the `--flywheel` daemon boot). Then `VerifyCommandExitCodeSets` resolves 24 and 25.

### Adjacent inconsistencies — ORDERED INTEGRATION OBLIGATION (must precede the code-24 add)

Code 24 and (C6's) code 26 are allocated while code **25** is a LOCAL const `ExitCodeSupervisorRunning` in `cmd/harmonik/supervise/start.go:23` (verified) that is NOT in the central operator-nfr §8 registry (verified: `internal/operatornfr/exitcode.go` declares "all 24 exit codes (0–23)"; the §8 taxonomy table ends at row 23). Allocating 24 and 26 while leaving a HOLE at 25 produces a non-contiguous registry that `VerifyCommandExitCodeSets` cannot resolve for 25, and "supervise" is NOT a registered `CommandName` in `internal/operatornfr/commandcodes.go` (only daemon/attach/enqueue/status/pause/stop/upgrade/list/runner exist — verified). Because AC6/AC7 of C5 and AC7 of C6 require `VerifyCommandExitCodeSets()` to pass with codes 24/26 registered, the integration pass MUST perform these steps IN ORDER (this is a normative integration obligation, NOT a silent fix — recorded here so finalize chases it deliberately):

1. **Absorb code 25 into §8** as the `supervisor-already-running` entry (the value already used at `start.go:23`; PL-019(c)'s "PL-INTERIM pending ON absorption" note resolves here). Add the §8 taxonomy row + ON-001/ON-003 catalog entry. This closes the registry hole BEFORE 24 is added so the registry stays contiguous.
2. **Add a `CommandSupervise` (or `supervise-start`) `CommandName`** in `internal/operatornfr/commandcodes.go` + a corresponding `CommandExitCodeSet` (so `supervise`/`supervise start` exit codes resolve). Without this, no `CommandExitCodeSet` can declare codes 24/25/26 for the supervise surface and `VerifyCommandExitCodeSets` has nowhere to register them.
3. **THEN add code 24** (`flywheel-already-running`, this spec) and **code 26** (`daemon-boot-throttled`, C6) to §8 and to the relevant `CommandExitCodeSet`s (`CommandSupervise` for 24/25; `CommandDaemon` for 24-when-flywheel and 26).

Step 2 also resolves the §8 home for the supervisor-lock refusal exit 25 cited throughout PL-019. The code-17 dual-meaning note below is a separate, lower-priority reconciliation (does not block 24/26 registration).

**Separate (non-blocking) note:** `start.go:19` defines a LOCAL `ExitCodeDaemonDown=17` that semantically collides with the registry's code-17 `multi-daemon-target-missing`. The integration pass SHOULD reconcile the dual meaning of code 17 (do NOT silently rewrite), but this does not gate the 24/25/26 registration above.

### Spec text to amend — PL-019(c)

Add a cross-reference: "the supervisor singleton lock of this clause is narrower than the single-flywheel-per-project lock of §PL-002c; `harmonik supervise start` MUST acquire BOTH (the flywheel lock first, then the supervisor lock), refusing with code 24 on flywheel-lock contention and code 25 on supervisor-lock contention."

## Files & changes

| File | Change | Why |
|---|---|---|
| `specs/process-lifecycle.md` | Add PL-002c; amend PL-019(c) cross-ref; PL-004 file-surface inventory adds `.harmonik/flywheel.lock`; changelog row. | Normative single-flywheel lock. |
| `specs/operator-nfr.md` | Ordered: (1) absorb §8 code 25 `supervisor-already-running` + catalog; (2) add §8 code 24 `flywheel-already-running` + ON-001/ON-003 catalog; changelog row; note code-17 dual-meaning for later reconciliation. | Exit-code registration (25-before-24 keeps the registry contiguous). |
| `internal/lifecycle/flywheellock.go` (new, modeled on `pidfilelock.go`) | `ProbeFlywheelLock` + acquire/reclaim/diagnostic; marker `{pid, layer, started_at, project_hash}`. | Lock mechanics. |
| `internal/daemon/daemon.go` | Acquire the flywheel lock after the pidfile lock (PL-005 step 1.5) when `--flywheel`; refuse with exit 24 + diagnostic. | Daemon-path acquire. |
| `cmd/harmonik/supervise/start.go` | Acquire the flywheel lock BEFORE the supervisor lock + before any state write; refuse with exit 24. | Supervise-path acquire. |
| `internal/operatornfr/exitcode.go` + `commandcodes.go` | Add code 25 then code 24 to the §8 `ExitCodes` registry; add a `CommandSupervise` `CommandName` + `CommandExitCodeSet` (codes 24/25/17/0); add code 24 to `CommandDaemon`. | `VerifyCommandExitCodeSets` passes with 24 and 25 resolvable. |

## Acceptance criteria

- **AC1 (C5-R6 i, second-start-refused, daemon→supervise):** Daemon up (flywheel topology, holding `flywheel.lock`); `harmonik supervise start` refuses with exit 24 and a diagnostic naming the daemon PID + `layer=daemon` + lock path.
- **AC1b (supervise→daemon):** `supervise start` up (holding `flywheel.lock`); a flywheel-topology daemon boot refuses with exit 24 naming the supervisor PID + `layer=supervisor`.
- **AC2 (C5-R6 ii, stale reclaim):** First owner crashes (lock auto-released, marker remains, recorded PID dead); a subsequent start acquires the lock, removes the stale marker, and proceeds — no operator intervention.
- **AC3 (C5-R6 iii, first-owner-undisturbed):** A refused second start performs NO state mutation; the first owner's pidfile/sentinel/config/queue are byte-identical before and after the refused attempt.
- **AC4 (C5-R3):** The lock is `flock(LOCK_EX|LOCK_NB)`; killing the owner (SIGKILL) auto-releases it (next start reclaims). No `F_SETLK` anywhere in the new path.
- **AC5 (scope / R1):** A plain non-flywheel `harmonik --project` daemon boots WITHOUT acquiring `flywheel.lock` and is unaffected by a held flywheel lock.
- **AC6 (registry):** `VerifyCommandExitCodeSets()` returns no errors after the ordered registration (code 25 absorbed, `CommandSupervise` added, code 24 added); BOTH code 24 and code 25 resolve to §8 entries; the registry is contiguous (no hole at 25).

## Verification

```bash
go test ./internal/lifecycle/...  -run 'FlywheelLock|PidfileLock|Probe|StaleReclaim' -count=1
go test ./internal/daemon/...     -run 'FlywheelLock|Start|MultiDaemon'              -count=1
go test ./cmd/harmonik/supervise/... -run 'Start|FlywheelLock|Lock'                 -count=1
go test ./internal/operatornfr/... -run 'VerifyCommandExitCodeSets|ExitCode'        -count=1
```

Manual: start a flywheel daemon; run `harmonik supervise start`; confirm exit 24 + diagnostic. Kill the daemon (`kill -9`); run `harmonik supervise start`; confirm it reclaims the stale lock and starts.

## Error handling & edge cases

- **Recycled PID** — flock OK + recorded PID alive but is a DIFFERENT process (PID reuse) → ambiguous → refuse with code 24 (the safe direction; OQ-PL-007 PID-reuse-on-reboot handling applies). MAY corroborate via `proc_pidpath` (darwin) / `/proc/<pid>/cmdline` (Linux) per PL-002a.
- **`flock` ENOLCK/ENOTSUP** (non-supporting fs) — exit 9 (`filesystem-unwritable`) per the PL-002b precedent, not code 24.
- **Torn marker file** — an unparseable marker is treated as stale per PL-024 (the flock probe is authoritative for liveness).
- **Both locks contended** — `supervise start` acquires the flywheel lock first; on its contention it exits 24 before even probing `supervisor.lock` (the flywheel lock is the outer gate).

## Migration / backwards compatibility

Additive: the flywheel lock is a NEW file acquired only on flywheel-topology boots. Existing non-flywheel daemons are unaffected. Code 24 is a new §8 entry within the N-1 window (existing 0–23 mappings unchanged per ON-001 code-stability). The pidfile-lock (exit 5) and supervisor-lock (exit 25) behaviors are unchanged.

## Test beads

- **Scenario:** AC1 + AC2 (cross-layer refusal + stale reclaim) IS the scenario test. CLI under test: `harmonik supervise start` (and the flywheel daemon boot). Lifecycle state: flywheel lock held by a live owner / held by a dead owner. Observable terminal condition: exit code 24 + stderr diagnostic on contention; successful start + removed stale marker on reclaim.
- **Exploratory:** AC1 diagnostic IS the exploratory test — `harmonik supervise start` while a flywheel daemon runs prints a stderr line naming the live owner (PID + layer + lock path) and exits 24.
- See the shared test-bead block in `C6-boot-backoff-spec.md` §Test beads for the filed bead IDs.
