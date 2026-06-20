# Keeper Critique — Code Structure & Coupling

**Lens:** coupling, cohesion, god-files/functions, dependency direction, change-safety.
**Verdict:** The keeper is **structurally over-coupled around two god-functions and an 8-marker shared-filesystem ABI**. The package boundaries are *clean on paper* (small focused files) but the **decision logic is not** — the same gate sequence is hand-copied across four entry points, and the state machine lives in two giant functions that nobody can change in isolation. This is a primary driver of the "fix one bug, regress another" pattern.

---

## 1. God-functions (the real complexity center)

Line counts of the longest units (measured, not estimated):

| Function | File | Lines |
|---|---|---|
| `Watcher.Run` | watcher.go | **306** |
| `runKeeperDoctor` | keeper_enable_doctor_cmd.go | **239** |
| `Cycler.runCycle` | cycle.go | **174** |
| `runKeeperEnable` | keeper_enable_doctor_cmd.go | **166** |
| `Cycler.MaybeRun` | cycle.go | **155** |
| `Cycler.RunOnDemand` | cycle.go | **130** |
| `CyclerConfig.applyDefaults` | cycle.go | **113** |
| `Cycler.RunForPrecompact` | cycle.go | **95** |

`Watcher.Run` (watcher.go:541–847) is a 306-line `for`/`select` body that interleaves: orphan-decision reaping, gauge read, absent/stale/parse branches, heartbeat, session-id binding+latch+adopt+foreign-reject, idle-gate, **three** separate cycler dispatch sites (MaybeRun / RunForPrecompact / RunOnDemand), and the warn state machine — all sharing ~10 mutable locals (`warnArmed`, `warnFired`, `pendingInject`, `gaugeStaleSince`, `lastRespawnAt`, `lastLiveRecoverAt`, …). There is no seam between "read gauge / resolve identity" and "decide / act". Every one of the known failure signatures (foreign-session reject, latch-after-/clear, stale-binding) is a branch *inside this one loop*, so any fix touches the same 306 lines that everything else depends on.

`runCycle` (cycle.go:706–880) is a 7-step imperative script (journal→truncate→inject→poll→identity→/clear→settle→resume→journal) with anti-loop bookkeeping woven through. `runOnDemandCycleTail` (cycle.go:1240) is a **near-verbatim copy of steps 3b–7** — a textbook duplication: a change to the post-/clear tail must be made in two places or they drift.

## 2. Cohesion: gate logic is copied, not shared (the #1 drift risk)

The same gate sequence — `.managed` opt-in, empty-session-id, anti-loop suppression, HoldingDispatch fail-closed, operator-attached, boot-grace, force-threshold exception — is **re-implemented four times**:

- `MaybeRun` (cycle.go:536)
- `RunForPrecompact` (cycle.go:976) — "mirrors MaybeRun side-effect" comments at :1015, :1020
- `RunOnDemand` (cycle.go:1101)
- partially in `Watcher.Run` itself (the session-id binding gates).

`grep` counts **59** references to the gate predicates (`IsManagedFn`/`HoldingDispatchFn`/`operatorAttached`/`aboveForceThreshold`/`seenLowPctAfterLastFire`/`lastFiredSID`) inside cycle.go alone. The comments openly admit the coupling: RunForPrecompact's boot-grace block (cycle.go:997–999) says *"The grace state is kept current by MaybeRun, which the watcher calls immediately before RunForPrecompact (watcher.go:568 before :580) — that ordering is the load-bearing invariant."* **A load-bearing invariant that lives in call-site ordering between two methods, enforced by a comment, is exactly the kind of implicit coupling that produces regressions.** Reorder those two dispatch sites and the boot-grace gate silently breaks with no compile error and no failing unit test (the unit test for RunForPrecompact populates grace state manually).

The anti-loop state machine (`lastFiredSID` / `seenLowPctAfterLastFire` / `lastForcedAttemptAt` / `consecutiveHandoffTimeouts` / `bootGraceFirstArmAt` / `seenSessionIDs` / `currentSessionIDSince`) is **7 interdependent fields on `Cycler`** mutated from MaybeRun, runCycle, RunForPrecompact, runOnDemandCycleTail. The DEFECT-1..4 / hk-4f8 / hk-ibb / hk-hz9 / hk-qoz / hk-uxu citation density (each a past bug, each a special-case branch grafted onto these fields) is the fossil record of repeatedly patching this state machine without ever refactoring it. cycle.go:500 even carries a `TODO(hk-hz9 fix 5): seenSessionIDs grows unbounded` — a known leak left in.

## 3. The 8-marker filesystem ABI is the true coupling substrate

Identity/decision state is not in memory or one store — it is **8 sibling dotfiles** under `.harmonik/keeper/<agent>.*`: `.ctx`, `.sid`, `.managed`, `.idle`, `.dispatching`, `.precompact`, `.restart-now`, `.cycle`, `.lock`. The cross-process contract (statusline-hook writes `.ctx`; sessionstart-hook writes `.sid`; stop-hook writes `.idle`; CLI writes `.restart-now`/`.dispatching`; precompact-hook writes `.precompact`; watcher+cycler+CLI all write `.managed`) is an **untyped, implicit, multi-writer protocol**. `WriteManagedSessionID` (keeper.go:178) needs temp+fsync+rename specifically because "watcher, cycler, and rebind CLI (separate processes)" race on it (hk-b5e2). The session-id authority alone is resolved across `.ctx` value → `.sid` override (gauge.go:59) → `.managed` binding → UUIDv4/v7/uppercase discriminators (sessionid.go, watcher.go:502) → latch/adopt/foreign branches (watcher.go:681–721). **That is five files cooperating to answer one question ("who is this session?"), which is precisely the question the operator's bug list keeps centering on** (`--session-id` dies after /clear; stale `.managed`; gauge-not-wired-for-crews). The structure spreads one decision across the maximum number of files.

## 4. Go ↔ shell logic duplication (real, bounded)

Two decisions are implemented in *both* Go and shell:

1. **Agent-name resolution** — `HARMONIK_AGENT → HARMONIK_KEEPER_AGENT → $1 → tmux #S → "default"` plus the `*/* | *..*` path-traversal reject is duplicated across **all four** keeper-*.sh hooks (confirmed: precompact, sessionstart, stop, statusline) AND mirrored in Go's `validateAgent` (keeper.go:46) and the three CLI arg parsers. Five+ copies of the same rule.
2. **`.managed` gate** — the precompact hook re-checks `[ -f .managed ]` (keeper-precompact-hook.sh:95) and `RunForPrecompact` re-checks `IsManagedFn` (cycle.go:983). Documented as "defensive" but it is two implementations of one policy.
3. **session-id lowercasing** — done in keeper-statusline.sh:74 *and* in Go `ReadSessionIDFile` (sessionid.go:44) and `isUppercaseUUID` (watcher.go:502).

This is genuine drift risk, though smaller than the in-Go gate duplication. The pct/token threshold logic is **not** duplicated — thresholds.go is correctly the single source and the shell only extracts raw fields. Good.

## 5. Dependency direction & global state — mostly clean

- `internal/keeper` depends only on `internal/core`, `internal/presence`, stdlib — no cmd-layer import. Correct direction.
- Package-level **mutable** state is minimal: only `injector.go:41 var submitSettle` (a tunable) and `respawn.go:54 var shellCmds` (effectively const). The rest are sentinel errors. No problematic globals. Good.
- Heavy use of injectable `…Fn` fields (CyclerConfig has **18**) makes units testable in isolation — but the 18-field wiring surface is itself a coupling tax: the production wiring in keeper_cmd.go (`NewCycler` at :198, `NewWatcher` at :258) and the boot-grace/heartbeat/live-recover opt-ins are spread across that one cmd function, and a default applied in the wrong place changes behavior package-wide (thresholds.go:63–74 explicitly warns BootGracePeriod is opt-in-per-site *because* applying it as a default would break every other caller).

## 6. The cmd-layer file *is* a god-file (mixed concerns)

`keeper_enable_doctor_cmd.go` (1163 lines) mixes: flag parsing (`parseKeeperEnableArgs`/`parseKeeperDoctorArgs`), business policy (known-live-agent refusal, .managed gating), **settings.json structural surgery** (`mergeHookStanza`/`findHookForScript`/`updateHookCommand`/`appendHookGroup` — ~200 lines of nested `map[string]interface{}` traversal), tmux ops, filesystem I/O, and 11 doctor checks inline in one 239-line function. The settings.json-JSON-mutation cluster is a distinct subsystem that should not live in the same file as the doctor's 11 ad-hoc `check(...)` calls. `runKeeperDoctor` having 11 inline checks (binary currency, statusLine type, agent-pollution canary, captain-tools embedded-copy diff, …) means each new drift class grows this one function. It is a god-file, but a *lower-risk* one than cycle.go/watcher.go because it is read-mostly tooling, not the live restart path.

## 7. `goalkeeper_cmd.go` — NOT a competing keeper

`goal-keeper` (goalkeeper_cmd.go) is unrelated to the session-keeper. It is a flywheel utility that folds operator comms into `goal-state.json` (`internal/goalstate`). Name collision only; no shared code, no overlapping concern. Acquit it — it is not part of this problem.

## 8. Change-safety rating (the headline)

**To safely modify the restart cycle you must touch and simultaneously reason about:**
- `runCycle` **and** its duplicate tail `runOnDemandCycleTail` (cycle.go) — or they drift;
- all four gate copies (`MaybeRun`, `RunForPrecompact`, `RunOnDemand`, plus the watcher's own session-id gates) if the change is gate-shaped;
- the 7-field anti-loop state machine, whose invariants are enforced only by the call-site ordering inside `Watcher.Run` (the 306-line loop);
- the `.managed`/`.sid`/`.ctx` multi-writer ABI and the matching shell hooks;
- `applyDefaults` (cycle.go) where every threshold/grace/timeout default is wired, with documented "do not make this a default" foot-guns.

Realistically that is **~5 files and ~6 functions held in your head at once** for any non-trivial cycle change, with at least two correctness invariants (call-site ordering; tail duplication) that the compiler and the unit tests do **not** catch. That is a high change-cost and a direct mechanistic explanation for the regression pattern: the test suite is large (~28 test files) but it tests the *current* branch shape, not the invariant that the four gate copies stay equal or that the two dispatch sites stay ordered.

## Top structural fixes (in impact order)

1. **Extract one `evaluateGates(ctx, cf, mode) → decision` function** that MaybeRun/RunForPrecompact/RunOnDemand all call with a mode enum. Kills the 4-way gate duplication and the "load-bearing call ordering" comment.
2. **Extract `runCycleTail`** and have both `runCycle` and `runOnDemandCycleTail` call it. Kills the verbatim copy.
3. **Decompose `Watcher.Run`**: pull session-id resolution (`resolveIdentity`), the warn state machine, and the recovery dispatch into named methods so the loop body is a dispatcher, not the implementation.
4. **Make identity one typed accessor** (`ResolveSessionID`) that encapsulates the .ctx/.sid/.managed/UUID-version precedence, so the operator's recurring identity bugs have one place to fix.
5. **Lift the settings.json mutation cluster** out of keeper_enable_doctor_cmd.go into its own file/helper; split the 11-check doctor into a table.
