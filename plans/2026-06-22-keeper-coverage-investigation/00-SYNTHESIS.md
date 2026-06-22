# Keeper-coverage crisis — root cause + remediation (SYNTHESIS)

Date: 2026-06-22
Status: crisis MITIGATED (ctx-watchdog relaunched, crews context-cleared). This
document synthesizes the 5-agent fan-out (findings 01–05) and the consolidated
crisis write-up into a single root-cause + remediation record.

Inputs: `01-launch-path.md`, `02-gauge-watcher-wiring.md`, `03-threshold-misfire.md`,
`04-hardceiling-failsafe.md`, `05-watchdog-history.md`, `../2026-06-22-keeper-coverage-crisis.md`.

---

## 1. Headline root cause

Every live crew ran with **zero automated context-overflow protection** because
two layers were absent at once. The operator's standalone Sonnet "ctx-watchdog"
— the de-facto crew 300k-token governor — silently died ~36h ago
(last tick `2026-06-20T17:14Z`) and nothing relaunches it. And the per-crew
keeper auto-arming, though correctly wired in source, never ran: the daemon that
spawned the live crews on Jun-21 morning was a **pre-wiring binary**, so no crew
ever got a keeper window or watcher process. The captain was fine throughout —
it has its own manually-armed keeper, which the watchdog deliberately skips.

---

## 2. Contributing-factor chain (with evidence)

### (1) Crew keepers never armed — deploy-lag, not a code defect

The crew-launch path *is* supposed to arm a keeper window: daemon
`HandleCrewStart → SpawnCrewSession → spawnCrewKeeperWindow`
(`internal/daemon/tmuxsubstrate.go:1346`, builder at `:1430-1460` via
`crewKeeperWindowArgv` → `internal/agentlaunch/keeperargv.go:100`
`KeeperWindowArgv`, the SAME argv builder the captain uses). The CLI delegates
and does not self-spawn (`cmd/harmonik/crew.go:267-271`).

But at runtime every crew has exactly **1 window (`agent`)**, no `keeper`
window, and there is exactly one `harmonik keeper` process anywhere (the
captain's). Proof the rest of `HandleCrewStart` ran but the keeper-window step
did NOT: `.managed` markers exist for all five crews, yet `grep -c "keeper
window"` and `grep -c "launch keeper window for crew"` over the entire
8000-line daemon log both return **0** — neither the success line nor the
non-fatal-failure line ever fired.

Timeline pins it: keeper-window wiring landed on main **Jun 20** (hk-yfcc,
hk-rmy1, hk-34sa); crews spawned **Jun 21 07:14–12:34**; the installed
`/Users/gb/go/bin/harmonik` has mtime **Jun 21 19:41** and the daemon only
restarted onto the wired binary at 19:43:56 — *hours after* every crew launched.
Crews are independent tmux sessions (hk-mmlqt) so they survived later daemon
restarts; the now-wired binary cannot retro-add keeper windows to already-spawned
sessions.

### (2) Fresh-gauge ≠ live-watcher trap; doctor can't detect a missing watcher

The gauge writer and the watcher are fully decoupled. The statusline hook
(`scripts/keeper-statusline.sh:115-122`) writes `<agent>.ctx` on **every render**
— in-session, needing no watcher. The separate `harmonik keeper` watcher process
READS that gauge (`internal/keeper/watcher.go:954`) and drives warn/act/restart.
So a live crew pane keeps its own gauge fresh forever with no watcher present.

`keeper doctor` only stats gauge mtime
(`cmd/harmonik/keeper_enable_doctor_cmd.go:606-620`) and has **no
watcher-liveness check** — an unwatched-but-live crew shows a false-green
`✓ gauge .ctx fresh`. A ready-made read-only probe already exists,
`LiveKeeperPresent` (`internal/keeper/keeper.go:102-137`, which flock-probes
`<agent>.lock` to distinguish a live holder from a stale corpse). It is already
called by `set-dispatching`/`clear-dispatching` (`cmd/harmonik/keeper_cmd.go:650,706`)
but is **never called by `runKeeperDoctor`** — one call-site away from closing
the false-green gap.

### (3) The captain "threshold misfire" was BENIGN — a text artifact, not a bug

The captain warn fires at exactly **200,000 tokens** on the 1M window:
`minAbsOrPctCeil(200000, 0.70, 1_000_000) = min(200000, 700000) = 200000`
(`internal/keeper/thresholds.go:181-189`; gate at `watcher.go:706-712`). The
operator's perceived "~170–190k early fire" is a **reporting artifact**: the
soft `[KEEPER HINT]` rides the *same* 200k crossing as the hard `[KEEPER WARN]`
(same block, `watcher.go:1251-1276`), and the hint body hard-codes a literal
**"~190K"** (`watcher.go:1390`) instead of interpolating the live count. The
hint is advisory (one-time nudge, no forced clear); the forced cycle is the
separate 215k act gate. The gauge is accurate (live ~211k legitimately crosses
200k). No early-fire bug — this is the one red herring in the investigation.

### (4) Hard-ceiling failsafe is wired now, but moot with no watcher

The "UNWIRED/nil HardCeilingRestartFn" memory note is **STALE** — beads hk-746u
and hk-z8d0 both CLOSED 2026-06-20. On main the fn is wired and mode-gated,
default **alarm** (`cmd/harmonik/keeper_cmd.go:110-111`; gate at
`internal/keeper/watcher.go:1114-1144`; `DefaultHardCeilingTokens=280_000` at
`thresholds.go:95`). But the check is a **branch of the watcher poll loop** — no
watcher means it never executes. And even if a crew HAD a watcher, crews launch
**warn-only / alarm mode** and never pass `--hard-ceiling-mode restart`
(`internal/agentlaunch/keeperargv.go:100-125`), so they would emit
`session_keeper_hard_ceiling` at 280k but never auto-restart. Critically, there
is **no watcher-independent backstop** anywhere: the daemon reads no context
gauges, and the supervisor's `TokenCap` (`cmd/harmonik/supervise/config.go:69`)
is a *spend* budget, not a per-pane context gauge. An unwatched session has zero
overflow protection; at ~1M the pane stops accepting keystrokes and only a manual
`crew stop`+`start` recovers it.

### (5) The ctx-watchdog — built, ran, died, never auto-relaunched

The watchdog is a plain `claude --remote-control --model sonnet` tmux session
named `ctx-watchdog`, launched by `scripts/ctx-watchdog-launch.sh`, ticking a
`/loop 30m` prompt (`.harmonik/cognition/ctx-watchdog-prompt.txt`) that
force-restarts any session ≥300,000 tokens. It deliberately runs OUTSIDE the
orphan-sweep namespace and **skips the captain** (captain has its own keeper),
keepers, and `*-default`. It was **explicitly the compensation layer for keeper
unreliability, and specifically the layer that covered CREWS**. Built/launched
by the operator 2026-06-19; runtime artifacts prove it ran (minted sid, the
generated `ctx-watchdog-respawn.sh`, a live gauge). Last tick:
`ts=2026-06-20T17:14:20Z` — ~36h stale at investigation time. Nothing relaunches
it: `grep ctx-watchdog .claude/skills/` → nothing; the launcher header's claim
that "the captain health-tick re-runs this script if the pane dies"
(`scripts/ctx-watchdog-launch.sh:20-21`) is **aspiration, never implemented**.
The operator directive that mandated it (`.harmonik/context/captain-lanes.md:102`)
carries `set:2026-06-19 expires:~2026-06-22`, a window now closed.

---

## 3. The one disagreement, reconciled (never-armed vs armed-then-died)

Finding 01 (launch path) concluded the crews were **never armed**: no keeper
window, no keeper process, and zero keeper-window log lines ever. Finding 02
(gauge/watcher) observed a **stale dead-PID lockfile for paul** (`paul.lock`,
dated Jun 12, PID 96405 dead) — the signature of a watcher that ran once and
died, i.e. armed-then-died.

Both are correct; the situation is a **MIX**:
- **paul** — armed-then-died: the `paul.lock` corpse is from a *much earlier*
  watcher (Jun 12), long predating the current Jun-21 crew session. So paul has
  a stale lockfile artifact, but for *this* crew incarnation it was never armed.
- **leto / admiral / stilgar / logmine** — never armed: no lockfile at all, no
  keeper window, no process.

Both findings agree on the load-bearing conclusion: **every crew is currently
unwatched.** The lockfile-vs-no-lockfile difference is a leftover-corpse detail,
not a contradiction — `paul.lock` existing is exactly the false positive that
finding 02 warns only an `flock` probe (`LiveKeeperPresent`) can resolve.

---

## 4. Remediation plan (candidate beads — NOT filed)

| # | Pri | Item | Evidence / location |
|---|-----|------|---------------------|
| a | P1 | Wire **ctx-watchdog auto-relaunch** into the captain health-tick or daemon supervisor so a silent death self-corrects (the self-heal the launcher already advertises must actually exist live). | `scripts/ctx-watchdog-launch.sh:20-21` claims it; no live skill implements it (finding 05). |
| b | P1 | ctx-watchdog tmux-restart **bypasses the daemon's `spawnCrewKeeperWindow`** — a watchdog-restarted crew comes back keeper-LESS. Integrate watchdog-restart with the daemon keeper path so a restarted crew is re-armed. | `tmuxsubstrate.go:1346` (only the daemon spawn path adds the keeper window); watchdog uses `crew stop`+`start`/`tmux kill-session` (finding 05). |
| c | P2 | Wire **`LiveKeeperPresent` into `keeper doctor`** — add a `watcher` check so an unwatched-but-live crew prints `✗ watcher: no live keeper holds <agent>.lock` instead of false-green `✓ gauge fresh`. | probe at `keeper.go:102-137`, already used at `keeper_cmd.go:650,706`; doctor at `keeper_enable_doctor_cmd.go:606-620` (finding 02). |
| d | P3 | Keeper **HINT prints a hard-coded "~190K"**, not the live count — interpolate `cf.Tokens` + the configured warn band (as `ActionableWarnText` already does), optionally fold hint into warn. | `watcher.go:1390` literal; `injector.go:39` interpolating reference (finding 03). |
| e | P2 | **Captain gauge undercounts** (gauge ~211k vs `/context` ~274k) AND bands fire at ~27% of the 1M window — revisit the gauge token-basis and band tuning together. | gauge basis `keeper-statusline.sh:83`, `heartbeat.go:115`; bands `thresholds.go:35-43` (findings 03 + crisis §2). |
| f | — | **DESIGN DECISION:** crew per-keeper auto-restart (flip crew band to restart mode) vs. the standalone watchdog layer as the canonical crew governor — pick the durable answer so crews aren't covered by *neither*. | crews warn-only/alarm at `keeperargv.go:100-125`; watchdog was the de-facto force-cut (findings 04 + 05). |

Plus the operational follow-up: **backfill keepers on the currently-live crews**
— the now-wired binary cannot retro-add keeper windows, so each crew must be
`crew stop`+`start`-ed on the current binary to arm its keeper window (operator
decision).

---

## 5. Current mitigation status

The crisis is mitigated but **FRAGILE**. The ctx-watchdog was relaunched
2026-06-22 ~04:20Z (live tmux session `ctx-watchdog`, session_id
`a356d678…`, gauge ticking) — the crew 300k governor is back online. Crews
paul/leto/stilgar were context-cleared and now read low. Crew keeper processes
are running again on the restarted crews. BUT none of this is durable: the
watchdog is still a manually-launched standalone script with **no supervisor**
(same silent-death exposure as before), and paul still reports "no tmux target"
after a subsequent restart — so its keeper binding is not clean. Until item (a)
(watchdog auto-relaunch) and item (b) (watchdog-restart ↔ daemon keeper-path
integration) land, the fleet is one silent watchdog death away from the same gap.
