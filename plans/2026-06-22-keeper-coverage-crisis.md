# Keeper-coverage crisis — consolidated investigation + remediation

Date: 2026-06-22
Scope: why the live crews ran with no context-overflow protection, and what to do about it.
Sources: the five findings docs in `plans/2026-06-22-keeper-coverage-investigation/` (01–05). This file consolidates them and pins live raw data at the bottom.

---

## 1. Executive summary

Two stacked failures left every live crew running with **zero automated context-overflow protection**, and one (paul) climbed to ~669k tokens unbounded.

1. **Crews never got keeper watchers.** The crew-launch path *is* supposed to arm a per-crew keeper window (daemon `HandleCrewStart → SpawnCrewSession → spawnCrewKeeperWindow`). But the daemon binary that actually spawned paul/leto/admiral/stilgar/logmine on Jun-21 morning predated the Jun-20 keeper-window wiring — the wired binary was only installed today at 19:41, *hours after* every crew launched. So no crew ever got a keeper window or watcher process. This is "never armed," not "armed-then-died": no keeper window exists, no keeper process exists, and the daemon log has zero keeper-window spawn lines (success or failure).
2. **The operator's Sonnet ctx-watchdog — the de-facto crew 300k governor — died ~36h ago and nothing relaunched it.** That standalone watchdog was the layer that actually covered crews (it force-restarted any crew ≥300k). It last ticked 2026-06-20T17:14Z and silently died; no skill, Go code, cron, or launchd job relaunches it.

With both layers absent, the crews had no context cap of any kind. The captain was fine throughout — it has its own manually-armed keeper (and the watchdog deliberately skips the captain). The captain's perceived "early fire at ~170–190k" was a separate, benign red herring: a hard-coded literal in a hint string, not a real threshold bug.

---

## 2. Root cause (with file:line citations)

### Failure A — crews never armed (deploy-lag)

- The wiring is correct and present in source: `internal/daemon/tmuxsubstrate.go:1346` (`spawnCrewKeeperWindow` call inside `SpawnCrewSession`) and `:1430-1460` (best-effort window build via `crewKeeperWindowArgv` → `internal/agentlaunch/keeperargv.go:100` `KeeperWindowArgv`). The CLI delegates and does not self-spawn (`cmd/harmonik/crew.go:267-271`).
- The keeper-window wiring landed on main **Jun 20** (hk-yfcc, hk-rmy1, hk-34sa).
- The crews launched **Jun 21 07:14–12:34**, spawned by the morning daemon — built from a pre-wiring binary. The installed `/Users/gb/go/bin/harmonik` has mtime **Jun 21 19:41**; the daemon only restarted onto the wired binary at 19:43:56 — long after every crew was up.
- Proof the rest of `HandleCrewStart` ran but the keeper-window step did not: `.managed` markers exist for every crew, yet `grep -c "keeper window"` and `grep -c "launch keeper window for crew"` over the entire 8000-line daemon log both return **0** (neither the success line nor the non-fatal-failure line ever fired).
- Crews are independent tmux sessions (hk-mmlqt) so they survived every later daemon restart; the now-wired binary cannot retroactively add keeper windows to already-spawned sessions.

### Failure B — the ctx-watchdog died and nothing relaunched it

- The watchdog is a plain `claude --remote-control --model sonnet` tmux session named `ctx-watchdog`, launched by `scripts/ctx-watchdog-launch.sh`, ticking a `/loop 30m` prompt (`.harmonik/cognition/ctx-watchdog-prompt.txt`) that force-restarts any session ≥300,000 tokens. It deliberately **skips the captain** (captain has its own keeper).
- It ran (runtime artifacts: `.harmonik/cognition/ctx-watchdog.sid`, the generated `ctx-watchdog-respawn.sh`, and a live gauge). Last tick: gauge `ts=2026-06-20T17:14:20Z` — ~36h stale at investigation time.
- Nothing relaunches it. `grep ctx-watchdog .claude/skills/` → nothing. The launcher header claims "the captain health-tick re-runs this script if the pane dies" (`scripts/ctx-watchdog-launch.sh:20-21`) but **that wiring exists in no live skill** — it was aspiration, never implemented. The operator directive that mandated it (`.harmonik/context/captain-lanes.md:102`) carries `set:2026-06-19 expires:~2026-06-22`, a window that has now closed.

### Why nothing else caught it

- The hard-ceiling failsafe is now WIRED on main (`cmd/harmonik/keeper_cmd.go:110-111`, mode-gated at `internal/keeper/watcher.go:1114-1144`, default alarm) — but it is a branch of the keeper **watcher** loop. No watcher → it never executes. There is **no watcher-independent backstop** anywhere: not in the daemon, not in the supervisor (`supervise/config.go:69` `TokenCap` is a spend budget, not a context gauge), not external.

---

## 3. The five findings

### F01 — Launch path: crews never armed (deploy-lag)

The crew-launch path *is* supposed to arm a keeper window via the daemon (`tmuxsubstrate.go:1346,1430`), sharing the captain's argv builder (`agentlaunch.KeeperWindowArgv`). At runtime no crew has a keeper window or `harmonik keeper --agent <crew>` process; only the captain does (manually armed). `.managed` markers prove `HandleCrewStart` ran, but zero keeper-window log lines prove the keeper-window step never executed — consistent with a pre-wiring daemon binary at spawn time. The keeper/crew-launch skills already flag this as KNOWN live drift. Verdict: **never armed**, deploy-lag, not a code defect.

### F02 — Gauge/watcher split: fresh gauge ≠ live watcher

The statusline hook (`scripts/keeper-statusline.sh:115-122`) writes `<agent>.ctx` on every render — in-session, needing no watcher. The separate `harmonik keeper` watcher process READS it (`internal/keeper/watcher.go:954`) and drives warn/act/restart. So a live crew pane keeps its gauge fresh forever with no watcher. `keeper doctor` only stats gauge mtime (`cmd/harmonik/keeper_enable_doctor_cmd.go:606-620`) and has **no watcher-liveness check** — an unwatched live crew shows a false-green `✓ gauge .ctx fresh`. A ready-made read-only probe already exists, `LiveKeeperPresent` (`internal/keeper/keeper.go:102-137`, flock-probes `<agent>.lock`), already used by `set-dispatching` (`keeper_cmd.go:650,706`) but **never called by doctor**. Live: paul has a stale dead-PID lockfile; leto/admiral/stilgar have no lock at all; only captain has a running watcher.

### F03 — Threshold "misfire" is a benign text artifact (NOT a bug)

The captain warn fires at exactly **200,000 tokens** on the 1M window — `minAbsOrPctCeil(200000, 0.70, 1_000_000) = min(200000, 700000) = 200000` (`internal/keeper/thresholds.go:181-189`; gate at `watcher.go:706-712`). The operator's "~170–190k early fire" is a **reporting artifact**: the soft `[KEEPER HINT]` rides the same 200k crossing as the hard `[KEEPER WARN]` (same block, `watcher.go:1251-1276`), and the hint body hard-codes a literal **"~190K"** (`watcher.go:1390`) instead of interpolating the live count. The hint is advisory (one-time nudge, no forced clear); the forced cycle is the separate 215k act gate. Gauge is accurate (live ~211k legitimately crosses 200k). No early-fire bug.

### F04 — Hard-ceiling failsafe is wired but moot for an unwatched crew

The "UNWIRED/nil HardCeilingRestartFn" memory note is STALE — beads hk-746u and hk-z8d0 both CLOSED 2026-06-20. On main the fn is wired and mode-gated, default **alarm** (`keeper_cmd.go:110-111`; `watcher.go:1114-1144`; `DefaultHardCeilingTokens=280_000` at `thresholds.go:95`). But the check is a branch of the watcher poll loop — paul has no watcher, so it cannot fire. Crews are warn-only/alarm anyway (`keeperargv.go:100-125`, never `--hard-ceiling-mode restart`). And there is **no watcher-independent backstop**: the daemon doesn't read context gauges, and the supervisor's `TokenCap` is a spend budget, not a per-pane gauge. An unwatched session has zero overflow protection; at ~1M the pane stops accepting keystrokes and only a manual `crew stop`+`start` recovers it.

### F05 — Watchdog history: the real crew governor that silently died

The watchdog the operator remembers is real: `scripts/ctx-watchdog-launch.sh`, a standalone Sonnet `--remote-control` tmux session running a `/loop 30m` that force-restarts any session ≥300k. Runtime artifacts prove it ran (sid, generated respawn script, live gauge `ts=2026-06-20T17:14Z`). It deliberately ran OUTSIDE the orphan-sweep namespace and skipped the captain, keepers, and `*-default`. It was **explicitly the compensation layer for keeper unreliability, and specifically the layer that covered CREWS** (the keeper gauge is known-not-wired-for-crews on this deployment). It died ~36h ago and nothing relaunches it — the self-heal "captain health-tick" it cites was never implemented. Its death + the unarmed crew keepers = the full coverage gap.

---

## 4. Remediation

### DONE

- **ctx-watchdog relaunched** 2026-06-22 ~04:20Z. Live as tmux session `ctx-watchdog`, session_id `a356d678-8ca2-4b93-82e9-2513afed6700`, gauge ticking (`ts=2026-06-22T04:21:30Z`). The crew 300k governor is back online.

### PENDING (each a candidate bead under `codename:keeper-redesign`)

1. **Wire watchdog auto-relaunch into a supervised layer.** Make the captain health-tick (or, better, the daemon supervisor) detect a dead `ctx-watchdog` session and re-run `scripts/ctx-watchdog-launch.sh`. The self-heal the launcher already advertises (`:20-21`) must actually exist somewhere live so a silent death self-corrects.
2. **Wire `LiveKeeperPresent` into `keeper doctor`.** Add a `watcher` check to `runKeeperDoctor` calling `keeper.LiveKeeperPresent(projectDir, agent)` so an unwatched-but-live crew prints `✗ watcher: no live keeper holds <agent>.lock` instead of a false-green `✓ gauge .ctx fresh`. The probe already exists (`keeper.go:102-137`); it is one call-site away.
3. **Make the HINT print the live token count.** Replace the hard-coded `"~190K"` literal in `keeperHintText` (`watcher.go:1390`) with interpolated live `cf.Tokens` + the configured warn band, as `ActionableWarnText` already does (`injector.go:39`). Optionally fold the hint into the warn so one crossing emits one message.
4. **Decide crew-keeper auto-restart policy vs the watchdog layer.** Crews are currently warn-only/alarm (no auto-restart) and were relying on the watchdog for force-cut. Decide whether the per-crew keeper should own force-cut (flip crew band to restart mode) or whether the watchdog remains the canonical crew governor — and remove the ambiguity so crews aren't covered by *neither*.
5. **Backfill keepers on the currently-live crews.** The now-wired binary cannot retro-add keeper windows to already-spawned sessions. Restarting each crew on the current binary (`harmonik crew stop <name>` + `start`) arms its keeper window. (Operator decision — not done here; this is a documentation task.)

---

## 5. RAW DATA appendix

All commands run from `/Users/gb/github/harmonik` on 2026-06-22, output pasted verbatim.

### Keeper watcher process census — `pgrep -fl "harmonik keeper"`

```
59748 harmonik keeper --agent captain --tmux harmonik-a3dc45482890-captain:agent --warn-abs-tokens 200000 --act-abs-tokens 215000
```

Exactly one watcher — the captain. No `--agent <crew>` keeper anywhere.

### Live sessions — `tmux ls | grep -iE "crew|captain|watchdog"`

```
ctx-watchdog: 1 windows (created Sun Jun 21 21:20:30 2026)
harmonik-a3dc45482890-captain: 2 windows (created Sun Jun 21 07:04:14 2026) (attached)
harmonik-a3dc45482890-crew-admiral: 1 windows (created Sun Jun 21 09:39:35 2026)
harmonik-a3dc45482890-crew-leto: 1 windows (created Sun Jun 21 09:06:47 2026)
harmonik-a3dc45482890-crew-logmine: 1 windows (created Sun Jun 21 12:34:55 2026)
harmonik-a3dc45482890-crew-paul: 1 windows (created Sun Jun 21 07:14:45 2026) (attached)
harmonik-a3dc45482890-crew-stilgar: 1 windows (created Sun Jun 21 07:17:57 2026)
```

Every crew has **1 window** (`agent` only — no `keeper` window). Captain has **2** (agent + keeper). The `ctx-watchdog` session is present (relaunched — see §4 DONE; this `created` timestamp is the tmux session, re-minted on relaunch).

### Agent gauges — `cat .harmonik/keeper/<a>.ctx`

```
paul:         {"pct":68,"tokens":681571,"window_size":1000000,"session_id":"d3dd06fb-1b31-412b-8132-7b514e1df5c9","ts":"2026-06-22T04:21:23Z"}
leto:         {"pct":38,"tokens":383519,"window_size":1000000,"session_id":"55b5ef6d-3810-4c46-b96b-87f4692bee3c","ts":"2026-06-22T04:00:20Z"}
admiral:      {"pct":29,"tokens":292818,"window_size":1000000,"session_id":"cf35c324-5110-46d6-8239-552312260689","ts":"2026-06-22T04:17:56Z"}
stilgar:      {"pct":37,"tokens":370069,"window_size":1000000,"session_id":"91ca317e-00e8-48c0-8d2a-6d86fd19f6ff","ts":"2026-06-22T03:58:35Z"}
logmine:      {"pct":13,"tokens":134917,"window_size":1000000,"session_id":"7acee4e7-7b9a-4388-b8f4-d25770550ee6","ts":"2026-06-22T04:21:16Z"}
captain:      {"pct":25,"tokens":252616,"window_size":1000000,"session_id":"78c49260-036d-4071-a92f-6ce89c1c7742","ts":"2026-06-22T04:21:25Z"}
ctx-watchdog: {"pct":22,"tokens":44093,"window_size":200000,"session_id":"a356d678-8ca2-4b93-82e9-2513afed6700","ts":"2026-06-22T04:21:30Z"}
```

paul at **681,571 tokens** (past the would-be 280k hard-ceiling alarm, climbing toward the ~1M pane-lock) with no watcher. ctx-watchdog freshly relaunched (session `a356d678…`, 44k tokens, Sonnet 200k window).

### Lockfiles — `ls -la .harmonik/keeper/*.lock`

```
-rw-------@ 1 gb  staff  6 Jun 21 08:06 .harmonik/keeper/captain.lock
-rw-------@ 1 gb  staff  6 Jun 12 10:18 .harmonik/keeper/gurney.lock
-rw-------@ 1 gb  staff  6 Jun 12 10:18 .harmonik/keeper/liet.lock
-rw-------@ 1 gb  staff  6 Jun 12 10:18 .harmonik/keeper/paul.lock
-rw-------@ 1 gb  staff  6 Jun 18 09:21 .harmonik/keeper/rebind.lock
-rw-------@ 1 gb  staff  6 Jun  3 12:25 .harmonik/keeper/smoketest-noexist.lock
```

Only `captain.lock` (Jun 21 08:06) corresponds to a live watcher. **paul.lock is a stale corpse dated Jun 12 10:18** — a long-dead watcher's lockfile left behind; its mere existence is a false positive that only an `flock` probe (i.e. `LiveKeeperPresent`) can distinguish. gurney/liet/smoketest-noexist are dead-watcher corpses from earlier sessions. leto/admiral/stilgar/logmine have **no lockfile at all** (never had a watcher).

### Recent main — `git -C /Users/gb/github/harmonik log --oneline -3 origin/main`

```
7c3160db fix(test): skip ProactiveReaper_RunsWhenIdle while hk-y3frr stopgap is active
3d1a6869 test(pasteinject): regression test for re-entrant implementer post-agent_ready silent-death path (hk-emuic)
6d575d6f harmonik: strip run-context from merge (hk-4je)
```

### Binary install time vs crew session creation times

`ls -la /Users/gb/go/bin/harmonik`:

```
-rwxr-xr-x@ 1 gb  staff  14147010 Jun 21 19:41 /Users/gb/go/bin/harmonik
```

Installed-binary mtime **Jun 21 19:41**. Crew session creation times (from `tmux ls`):

```
crew-paul:    created Sun Jun 21 07:14:45 2026
crew-stilgar: created Sun Jun 21 07:17:57 2026
crew-leto:    created Sun Jun 21 09:06:47 2026
crew-admiral: created Sun Jun 21 09:39:35 2026
crew-logmine: created Sun Jun 21 12:34:55 2026
```

Every crew was spawned **between 07:14 and 12:34** — all **before** the wired binary was installed at **19:41**. This is the deploy-lag that pins Failure A: the morning daemon ran a pre-keeper-window binary, so the keeper-window spawn step never executed for any crew.

### Full session census — `tmux ls`

```
ctx-watchdog: 1 windows (created Sun Jun 21 21:20:30 2026)
harmonik-a3dc45482890-captain: 2 windows (created Sun Jun 21 07:04:14 2026) (attached)
harmonik-a3dc45482890-crew-admiral: 1 windows (created Sun Jun 21 09:39:35 2026)
harmonik-a3dc45482890-crew-leto: 1 windows (created Sun Jun 21 09:06:47 2026)
harmonik-a3dc45482890-crew-logmine: 1 windows (created Sun Jun 21 12:34:55 2026)
harmonik-a3dc45482890-crew-paul: 1 windows (created Sun Jun 21 07:14:45 2026) (attached)
harmonik-a3dc45482890-crew-stilgar: 1 windows (created Sun Jun 21 07:17:57 2026)
hk-a3dc45482890-keeper: 3 windows (created Sun Jun 21 19:43:56 2026)
hk-daemon-supervise: 1 windows (created Sun Jun 21 14:26:01 2026)
```
