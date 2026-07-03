# C5 — Single-flywheel-per-project lock — Research findings

Component: assert at most one flywheel owner (daemon ⊕ supervised-Pi) per project; a second start
is refused with a clear exit code + diagnostic, preventing the dual-orchestrator collision.
Bead hk-li14r, analysis gap #6, goal G6. Anchors verified at `2e49a8df`.

## RQ1 — What locks exist today and what does each prevent?

**Finding:** Two INDEPENDENT fd-lifetime advisory locks exist; neither alone prevents the incident's daemon⊕Pi collision.

- **Daemon pidfile lock (PL-002/PL-002a)** — `internal/lifecycle/pidfilelock.go`. `ProbePidfileLock(projectDir)` does `Flock(LOCK_EX|LOCK_NB)` on the pidfile (`:90`), then the PL-002a two-step disambiguation: on EAGAIN → `PidfileLockStatusHeld` → exit 5 (`pidfile-locked`); flock OK + `kill(pid,0)` ESRCH → `Stale` → remove pidfile + proceed per PL-024 (`:108-114`); flock OK + PID alive → `Ambiguous` → refuse (`:118-149`). Prevents a SECOND DAEMON.
- **Supervisor lock (PL-019c)** — `cmd/harmonik/supervise/start.go:98-126`. `Flock(LOCK_EX|LOCK_NB)` on `supervisor.lock`; on failure exit 25 (`ExitCodeSupervisorRunning`, `start.go:21-23`). The shim re-acquires it blocking. Prevents a SECOND SUPERVISED-PI. Also writes the PL-006d sentinel (`start.go:130`).

**Gap (the incident):** killing the auto-pull DAEMON tripped a separate live `harmonik-pi` SUPERVISOR doing the same work. The two locks are orthogonal: a daemon boot checks the pidfile lock; a `supervise start` checks `supervisor.lock`. Neither checks the other. So "one daemon + one independent Pi" is a permitted state — both burning credit on the same beads. There is no single lock asserting "this project has exactly ONE flywheel owner (daemon ⊕ Pi)."

## RQ2 — How to define the canonical single-flywheel relationship (C5-R1)?

**Tradeoff (three designs):**
- **Option A — unify on ONE flywheel lock file** `.harmonik/flywheel.lock` that BOTH `daemon.Start` (flywheel topology) AND `supervise start` acquire (flock LOCK_EX|LOCK_NB) before proceeding; whoever holds it is THE flywheel owner; the second (whichever layer) is refused. Pro: one canonical ownership assertion, exactly C5-R1. Con: a new lock added to two start paths; must not break the non-flywheel daemon (a plain `harmonik --project` daemon used for non-flywheel work should still boot — so the flywheel lock is conditional on flywheel topology, not unconditional).
- **Option B — make the supervisor lock canonical; daemon (flywheel topology) checks supervisor.lock before booting, and supervise checks the pidfile.** Pro: reuses existing locks. Con: cross-checking two different lock files is racy (TOCTOU between probe and acquire) and asymmetric.
- **Option C — make the pidfile lock canonical for both.** Con: a supervised Pi is not a daemon; overloading the pidfile semantics is confusing.
- **Recommendation:** Option A — a dedicated `.harmonik/flywheel.lock`, acquired by both the flywheel-daemon boot and `supervise start`, with the existing pidfile + supervisor locks retained for their narrower duties. The flywheel lock is the single ownership assertion; it is the clean expression of C5-R1 and avoids cross-file TOCTOU. The change spec must scope it to the flywheel topology so a plain daemon is unaffected (non-goal: blocking legitimate non-flywheel daemons).

## RQ3 — fd-lifetime + stale-reclaim discipline (C5-R3, R5)

**Finding:** The exact pattern C5 needs is already implemented in `pidfilelock.go` and reusable. Requirements:
- **C5-R3 fd-lifetime flock** — `Flock(LOCK_EX|LOCK_NB)`, kernel-auto-released on crash (clean or unclean). POSIX `F_SETLK` is FORBIDDEN (PL-002a discipline; memory note + analysis §D). The supervisor.lock and pidfile lock both already do this; the flywheel lock copies the idiom.
- **C5-R5 stale reclaim** — disambiguate via flock-acquire + `kill(pid,0)` probe, mirroring PL-002a/PL-024: flock OK + recorded-PID dead → stale marker, reclaim without operator intervention; flock OK + PID alive → ambiguous → refuse. `ProbePidfileLock` (`pidfilelock.go:77-149`) is the template; C5 generalizes it to the flywheel-lock marker.

## RQ4 — Exit code + diagnostic (C5-R2)

**Finding — NO free legacy exit code; C5 must ALLOCATE a new one.** The registry `ExitCodes` (`internal/operatornfr/exitcode.go:36`) is densely allocated 0–23; the next free code is **24**. Relevant existing entries: 5 `pidfile-locked`, 16 `operator-control-invalid-state`, 17 `multi-daemon-target-missing`, 25-via-local-const `supervisor.lock held` (the `supervise start` local `ExitCodeSupervisorRunning=25` at `start.go:21` is NOT in the central registry — a pre-existing inconsistency worth flagging). C5 should add a §8 taxonomy entry (e.g. code 24 `flywheel-already-running` or reuse/extend the `pidfile-locked`/supervisor-running family) and a `CommandExitCodeSet` entry (`internal/operatornfr/commandcodes.go:80-90`) for the daemon + supervise commands. `VerifyCommandExitCodeSets` (`:198-207`) asserts every declared code resolves to a §8 entry — the new code must be registered or that test fails. **Diagnostic (C5-R2):** name the live owner — PID + lock/pidfile path — read from the flywheel-lock marker file (the existing locks already record PID).

**Naming inconsistency to flag for the change spec:** code 17 in the central registry is `multi-daemon-target-missing`, but `supervise/start.go:17-19` defines a LOCAL `ExitCodeDaemonDown=17`. These collide semantically; the change spec touching exit codes should reconcile (out-of-strict-scope but adjacent — note, do not silently fix).

## RQ5 — First-owner-undisturbed + test surface (C5-R4, R6)

**Finding:** C5-R4 (refused second start must not mutate the first's state) is naturally satisfied by `LOCK_NB`: the second acquirer gets EAGAIN and exits BEFORE writing sentinel/config (the supervisor path already orders flock BEFORE `WriteSentinel`, `start.go:120-131`). The change spec must ensure the flywheel-lock acquire precedes ANY state write on both paths. Tests: `multidaemon_sx9r83_test.go`, `pidfilelock` tests, and `supervisor_test.go` model the lock-held / stale-reclaim / first-owner-unaffected cases; C5-R6 adds the cross-layer case (daemon up → supervise start refused, and vice versa).

## Risks / unknowns
- **R1 (non-flywheel daemon false-positive):** the flywheel lock must NOT block a legitimate plain `harmonik --project` daemon doing non-flywheel work — scope the lock to flywheel topology (a flag/config), else it regresses normal daemon use. Change-spec decision.
- **R2 (exit-code allocation + registry test):** adding code 24 requires a §8 entry + `CommandExitCodeSet` registration or `VerifyCommandExitCodeSets` fails — must be done together.
- **R3 (TOCTOU if Option B/C chosen):** cross-file lock checking is racy; Option A (single dedicated lock) avoids it — recommendation stands.

## No-blocker assertion
No blocker prevents a C5 change spec. The fd-lifetime + stale-reclaim machinery is fully precedented
(`pidfilelock.go`). Two change-spec deliverables: (a) the canonical flywheel-lock design (recommend a
dedicated `.harmonik/flywheel.lock`, Option A, scoped to flywheel topology), (b) a new §8 exit code
(24) + `CommandExitCodeSet` registration. One adjacent inconsistency flagged (code-17 dual meaning) —
note for the integration pass, do not silently fix.
