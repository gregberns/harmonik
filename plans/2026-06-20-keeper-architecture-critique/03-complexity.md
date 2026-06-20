# Keeper Critique — Complexity Lens

**Critic angle:** Is the keeper TOO COMPLICATED, and does that complexity directly cause its failures?

**Verdict (one line):** The keeper is drowning in *accidental* complexity — an implicit ~12-state machine with **7+ entry paths**, **6 distinct filesystem inputs**, and **8+ overlapping anti-loop / grace / force-retry sub-gates**, almost all of which exist to patch prior bugs rather than to model a genuinely hard problem; the failures are caused by the complexity, not by the difficulty of the task.

---

## 1. The implicit state machine — reconstructed

There is no single declared state machine. State is smeared across **three structs** (`WatcherConfig`/`Watcher`, `CyclerConfig`/`Cycler`, `CycleJournal`) plus **the filesystem**. Reconstructing it:

### A. Watcher loop states (warn machine + gauge machine), `watcher.go:541-847`
Per-tick the watcher resolves one of these **gauge dispositions**:
1. gauge **absent** (`:612`)
2. gauge **parse/stat error** (`:625`)
3. gauge **stale** (`:651`)
4. gauge **foreign session** (`:684`)
5. gauge **adopt-after-external-/clear** (`:693`)
6. gauge **latch first SID** (`:713`)
7. gauge **fresh & managed** → proceed (`:723`)

Crossed with the warn sub-machine's **3 boolean flags** (`warnArmed`, `warnFired`, `pendingInject`, `:545-554`) = up to **2³=8** warn states, of which ~4 are reachable. Plus the **stale-tracking** trio (`gaugeStaleSince`, `lastRespawnAt`, `lastLiveRecoverAt`, `:570-579`).

### B. Cycle journal phases, `cycle.go:23-29`
`opened → handoff_injected → confirmed → cleared → resumed → complete` (happy) or `aborted` — **7 phases**, each persisted, each with a distinct crash-recovery action (`RecoverFromCrash`, `:914-943`, 4 switch arms).

### C. Cycler anti-loop / grace state, `cycle.go:462-506`
**9 stateful fields** on the Cycler alone: `lastFiredSID`, `seenLowPctAfterLastFire`, `lastForcedAttemptAt`, `lastOperatorAttachedEmit`, `consecutiveHandoffTimeouts`, `currentSessionID`, `currentSessionIDSince`, `seenSessionIDs` (a *map*, explicitly noted as growing **unbounded**, `:500`), `bootGraceFirstArmAt`.

**Total distinct state dimensions a human must hold simultaneously:** ~12 (7 gauge dispositions × 3 warn flags collapsed, + 7 journal phases + 9 cycler fields). **This is not comprehensible by one person.** The author needed 27 `hk-*` bead references in the comments of *one file* (cycle.go) to remember why each branch exists.

---

## 2. Decision points / branching — counts

| Unit | Branches counted | Notes |
|---|---|---|
| `Cycler.MaybeRun` | **7 numbered gates + 4 unnumbered side-effect/escape-hatch blocks** = ~11 decision points | `cycle.go:536-690` |
| `Cycler.RunForPrecompact` | **5 gates** (1, 2, 2b, 3, 4, 5) | `cycle.go:976-1071`, partial mirror of MaybeRun |
| `Cycler.RunOnDemand` | **6 gates + 4-condition freshness gate** (sid-match, nonce, mtime≥, settle-restat) = ~10 | `cycle.go:1101-1231` |
| `Watcher.Run` per tick | **7 gauge dispositions + warn machine + 5 sub-dispatchers** (reap, heartbeat, cycle, precompact, restart-now) | `watcher.go:594-845` |
| `Gate 6` anti-loop alone | **nested 3-deep** with same-SID/diff-SID × above/below-force × force-retry-interval | `cycle.go:650-677` |

**The same logical decision ("should I clear now?") is implemented three times** — in `MaybeRun`, `RunForPrecompact`, and `RunOnDemand` — each a *slightly different subset* of the gates, with explanatory comments begging the reader to notice the divergence ("subset of MaybeRun", "mirrors MaybeRun side-effect", "DECOUPLED for restart-now"). This triplication is the combinatorial-explosion engine: every gate change must be re-reconciled across three call sites or they silently drift.

---

## 3. Inputs per tick — each one a failure surface

A single watcher tick reads, at minimum, these **6 distinct filesystem inputs**:
1. `<agent>.ctx` gauge (pct/tokens/window/sid) — `gauge.go:34`
2. `<agent>.sid` single-writer channel — `sessionid.go:30` (overrides #1)
3. `<agent>.managed` binding — `watcher.go:681`
4. `<agent>.idle` marker (CrispIdle) — `gates.go:191`
5. `<agent>.dispatching` marker (HoldingDispatch) — `gates.go:216`
6. `.sleeping.<sessionID>` marker — `gates.go:270`

Plus **live tmux probes**: `has-session` (`tmuxresolve.go:76`), `list-clients #{client_activity}` (`:131`), pane-alive/pane-idle. Plus marker files `.precompact`, `.restart-now`, and `events.jsonl` (orphan reaper + open-decision projection). **That is ~9 inputs to decide one yes/no.** The session-id alone has **three sources** (`.sid` primary, `.ctx` fallback, `.managed` expected) reconciled by hand-written latch/adopt/foreign logic (`watcher.go:681-721`) — and the known failure "`--session-id` goes DEAD after the 1st /clear" is *exactly* a multi-source-of-truth bug. The `.sid` channel (hk-8prq) was itself added to paper over the earlier multi-writer `.ctx` ambiguity; the old heuristics (`isUppercaseUUID`, UUIDv7 skip) **still remain in the code** (`watcher.go:502`, `cycle.go:1401`) as dead-weight defense-in-depth.

---

## 4. Layered special-cases that multiply paths

The brief asks whether crew/captain/flywheel/orchestrator multiply paths. They do, but the deeper multiplier is **per-bug branching**:

- **captain special-case**: `OnDemandRestart` auto-enabled for `AgentName=="captain"` (`watcher.go:462`), spawning an entire alternate warn-text + `restart-now` marker path (`RunOnDemand`) that crews never use.
- **window-size special-case**: every threshold has an **abs-token path AND a pct-fallback path** (`belowActThreshold`, `belowWarnThreshold`, `aboveForceThreshold` — each a 2-way branch on `Tokens>0 && WindowSize>0`), because old Claude Code didn't emit tokens. That doubles every threshold comparison and caused the F45 "Tokens-vs-Pct split-brain" the comments describe (`watcher.go:476-491`).
- **force-path exemptions**: boot-grace, CrispIdle, and anti-loop *each* carry an "above force threshold → bypass" exception (`cycle.go:599`, `:626`, `:652`, `:668`), so the force tier is effectively a *parallel second state machine* layered over the normal one.
- **three thresholds × two bases = a 6-cell band** (warn/act/force × abs/pct) all needing `warn<act<force` invariant maintenance (`thresholds.go:16`).

---

## 5. Essential vs. accidental — separated explicitly

**Essential complexity (the problem genuinely is hard):**
- Confirm-before-clear (poll for nonce, never `/clear` an unconfirmed handoff) — `cycle.go:759`. This is correct and irreducible.
- Crash-recovery journal — *some* persisted phase is needed so a crash mid-cycle doesn't strand a session. The 7 phases are arguably 2-3 too many, but the concept is essential.
- Idle/dispatch gating so a clear doesn't land mid-turn — essential, though the *implementation* (mtime races between `.idle` and `.ctx`, `crispIdleTolerance=10s`) is fragile.

**Accidental complexity (the solution made it hard) — the overwhelming majority:**
- **Triplicated decision logic** (MaybeRun / RunForPrecompact / RunOnDemand).
- **Multi-source session identity** (.sid + .ctx + .managed + dead UUIDv7/uppercase heuristics).
- **The entire anti-loop + boot-grace + force-retry + flap-cap subsystem** (`hk-4f8`, `hk-ibb`, `hk-hz9`, `hk-qoz`, `hk-0uu`, `hk-uxu`, `hk-6el`, `hk-lhu2`...). This is **~150 lines and 9 state fields** built to suppress a re-fire loop that exists *only because* the trigger (gauge pct) and the effect (new session after /clear) are observed through laggy, racy filesystem polling with a best-effort `ClearSettle` window. A synchronous "did the clear happen?" signal would delete nearly all of it.
- **Heartbeat (`hk-81wk`) + live-pane recovery (`hk-75mr`)**: two more whole subsystems added because the gauge writer (`keeper-statusline.sh`) skips writes after /clear and on NA pct, so a *live* agent's gauge goes stale and trips the no-gauge path. The keeper now *forges its own gauge writes* to stop its own staleness logic from misfiring — accidental complexity treating a symptom of the gauge design.

**Ratio estimate:** of the ~1,460 lines in `cycle.go`, the essential confirm-then-clear-then-resume happy path is ~120 lines (runCycle steps 1-7). The remaining **~90% is gating, anti-loop, grace, force-retry, precompact-mirror, on-demand-mirror, and crash-recovery** — i.e. accidental.

---

## 6. Single most complex unit + cost of removal

**Most complex unit: the anti-loop / boot-grace / force-retry tangle in `Cycler.MaybeRun` (`cycle.go:549-677`) backed by the 9 Cycler state fields.**

It is the worst because it is (a) stateful across ticks, (b) nested 3-deep, (c) duplicated (partially) into two sibling methods, (d) entirely time-and-race-driven, and (e) the densest concentration of bug-patch beads in the codebase (≥8 distinct `hk-*` fixes cited in ~130 lines). It is the prime suspect for the operator's lived failures: "stale `.managed` foreign_session, auto-recovers after ~3 ticks", "`--session-id` dead after /clear", and "unreliable — TIMING not executability" are all symptoms of this race-suppression machinery.

**Cost of removing it:** You cannot delete it under the current architecture — it is load-bearing *given* that the keeper learns "the clear happened" only by polling for a new session-id that may never appear within `ClearSettle` (`cycle.go:837`, comment admits "Absent a new session_id is non-fatal"). Removing the anti-loop without changing that would reintroduce the re-fire loop (and the 1500-session fork-bomb is the catastrophic tail of exactly that loop).

The *real* fix is architectural, not surgical: replace the poll-and-guess `/clear`-detection with a **single authoritative completion signal** (the SessionStart hook already writes `.sid` at the new session — make the cycle *block on that event* instead of polling the gauge, and make `restart-now` the sole human-facing trigger). That collapses anti-loop, boot-grace, force-retry, the heartbeat, and live-pane-recovery into a handful of states. The cost is a real redesign (which `keeper-redesign` / the competing spec drafts already acknowledge) — but the *current* cost of keeping it is unbounded: every new race spawns another `hk-*` field on the Cycler.

---

## 7. Bottom line

The keeper's complexity is **~80-90% accidental** and it **directly causes** the failure signatures. The tell is quantitative: 9 mutable Cycler fields, 3 triplicated decision methods, 6 filesystem inputs + 3 tmux probes for one boolean, 6 threshold cells, and a comment-citation density of one bug-patch bead every ~5 lines in the hottest function. No single person can hold this state machine in their head, which is why each fix adds a field instead of removing one.
