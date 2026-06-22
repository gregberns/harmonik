# Watchdog history — what it was, did it run, why it's gone

READ-ONLY investigation. 2026-06-22. Sources cited inline.

## TL;DR

The "watchdog" the operator remembers is **real, was built, and did run** — it is the
**Sonnet context-watchdog** (`scripts/ctx-watchdog-launch.sh`), an independent 300k-token
governor launched by the operator on 2026-06-19 as a belt-and-suspenders layer *on top of*
the per-session keeper. It is **(b): a manually-launched standalone tmux session that died and
was never relaunched** — NOT a never-built idea, and NOT the keeper subsystem itself. Last live
~2026-06-20T17:14Z (≈36 h before this investigation). Nothing in the codebase or any skill
re-launches it; it relied entirely on the operator running the script + a captain health-tick
self-heal that is not wired in any live skill.

## What it was — concretely

A **plain `claude --remote-control --model sonnet` interactive session**, NOT a crew, NOT a
daemon, NOT a Go process, NOT a cron/launchd job. Specifically:

- **Launcher:** `scripts/ctx-watchdog-launch.sh` (tracked on main). Mints a `--session-id`,
  starts a tmux session literally named `ctx-watchdog`, seeds it via bracketed-paste with a
  `/loop 30m` prompt, and writes a self-heal `ctx-watchdog-respawn.sh`.
- **The loop prompt:** `.harmonik/cognition/ctx-watchdog-prompt.txt` — a `/loop 30m` directive.
  Each tick: list crews (`harmonik crew list --json`) + tmux sessions, read each agent's
  `.harmonik/keeper/<agent>.ctx` gauge, and **force-restart any session at/over 300,000 tokens**
  (`crew stop`+`crew start`, escalating to `tmux kill-session`). It explicitly **SKIPS the
  captain** (only alerts the operator if captain >280k — "keeper should cycle it"), SKIPS keeper
  sessions, SKIPS `*-default`, and SKIPS itself.
- **Why standalone (not a crew):** the launcher header (`ctx-watchdog-launch.sh:12-18`) states a
  crew "binds to a queue and is keeper-managed — that would entangle the watchdog in the very
  mechanism it polices." So it deliberately runs OUTSIDE the `harmonik-<hash>-*` orphan-sweep
  namespace, with no keeper armed on it.

So: the "300k cap via Sonnet watchdog" in the 3-day scale-out memory == this exact script.

## Did it ever run? YES — runtime artifacts prove it

On-disk evidence (all dated Jun 19–20, the scale-out window):

- `.harmonik/cognition/ctx-watchdog.sid` — minted session-id `ba039814-…` (Jun 19 21:57).
- `.harmonik/cognition/ctx-watchdog-respawn.sh` — the GENERATED self-heal script (Jun 19 21:57),
  which the launcher only writes *at launch time*.
- `.harmonik/keeper/ctx-watchdog.ctx` — a **live context gauge** stamped by the running session:
  `{"pct":57,"tokens":113892,"session_id":"4a3faa32-…","ts":"2026-06-20T17:14:20Z"}`. A gauge is
  only written by a live, gauged session — its presence is direct proof the session was alive and
  ticking. (Note the gauge sid `4a3faa32-…` differs from the cognition sid `ba039814-…` → it was
  relaunched at least once, consistent with self-heal / a fresh launch on Jun 20.)
- `.harmonik/keeper/ctx-watchdog.idle` (Jun 20 10:14) and `.harmonik/keeper/ctx-watchdog.sid`
  (Jun 20 07:09) — more live-session bookkeeping.

**Last-alive proxy:** the gauge `ts` = `2026-06-20T17:14:20Z`. As of this investigation
(`2026-06-22T04:10Z`) that is ~36 hours stale.

## Why is it gone now? — it died and nothing relaunches it

- **No live process / session:** `ps`, `tmux list-sessions`, `/tmp/*watchdog*`, `crontab -l`,
  `~/Library/LaunchAgents` — all confirmed ABSENT. The only "watchdog" process on the box is
  macOS `watchdogd` (irrelevant). Current tmux sessions are the captain + 5 crews + 1 keeper +
  daemon-supervise — no `ctx-watchdog`.
- **No automated relauncher:** `grep ctx-watchdog .claude/skills/` → **nothing**. No live
  captain/crew/keeper skill, and no Go code, re-runs `ctx-watchdog-launch.sh`. The launcher's own
  header claims "the captain health-tick re-runs this script if the pane dies"
  (`ctx-watchdog-launch.sh:20-21`), but **that wiring does not exist in any live skill** — it was
  an aspiration, not implemented. So once the operator's manual launch died (context fill,
  `/clear`, host restart, or the scale-out window simply ending), nothing brought it back.
- **The directive expired:** the operator directive lives in `.harmonik/context/captain-lanes.md`
  line 102, tagged **`set:2026-06-19 expires:~2026-06-22`** — "EVERY session … MUST stay under
  300k … Run an INDEPENDENT Sonnet context-watchdog." That window has now closed, which is
  consistent with no one restarting it.
- **Not deleted by a commit:** the launcher + prompt are still tracked on main (last touched by
  `565e03fd` hk-jxcx sleep-guard and `5edab6af` rc-prefix). It vanished operationally (the
  session), not in source.

## Relation to the missing crew-keeper coverage

This is the load-bearing point for the keeper-coverage investigation:

1. **The watchdog was explicitly the COMPENSATION for keeper unreliability.** The launcher header
   (`:5-10`) and the operator directive both say it exists *because* "the built-in session-keeper
   is unreliable (and its own bands are 215k/240k)". It was a **separate enforcement layer on top
   of keepers**, exactly as the investigation hypothesized.
2. **It was specifically the layer that covered CREWS.** The keeper gauge is known-not-wired-for-
   crews on the live deployment (per the `keeper` skill's own drift note). The ctx-watchdog's loop
   prompt iterates **crews + flywheel** and force-restarts them — it was the de-facto crew context
   governor. The keeper-critique plan corroborates: "Per-agent keeper has NO supervisor → dies &
   stays dead … The `ctx-watchdog` net *explicitly skips the captain*"
   (`plans/2026-06-20-keeper-critique-round2/EXEC_SUMMARY.md` W1). I.e. the captain has its own
   keeper; **crews had the watchdog and nothing else**.
3. **Therefore its death = a real coverage gap.** With the watchdog dead AND the crew keeper gauge
   unwired, there is currently **no automated 300k/context governor on the live crews** — they
   rely on whatever keeper coverage exists (unreliable for crews) and the captain's manual
   attention. The crews running right now (admiral, leto, logmine, paul, stilgar) have no
   independent context cap enforcer.
4. **A redesign already anticipated folding it in.** The fleet-sleep-wake plans
   (`plans/2026-06-20-fleet-sleep-wake-status-and-next/`) proposed making the watchdog *read*
   "context-into-state" system signals rather than eyeball gauges, and Phase-0 already taught it
   to skip `.sleeping.*` sessions (hk-jxcx). That work treats the ctx-watchdog as a keeper-
   adjacent permanent fixture — but it remains a **manually-launched script with no supervisor**,
   so it stays prone to exactly this silent death.

## Key file paths

- `/Users/gb/github/harmonik/scripts/ctx-watchdog-launch.sh` — the launcher (Sonnet, tmux `ctx-watchdog`, 300k cap).
- `/Users/gb/github/harmonik/.harmonik/cognition/ctx-watchdog-prompt.txt` — the `/loop 30m` tick prompt.
- `/Users/gb/github/harmonik/.harmonik/cognition/ctx-watchdog-respawn.sh` — generated self-heal (proof it launched, Jun 19).
- `/Users/gb/github/harmonik/.harmonik/cognition/ctx-watchdog.sid` — minted session-id.
- `/Users/gb/github/harmonik/.harmonik/keeper/ctx-watchdog.ctx` — live gauge; `ts=2026-06-20T17:14Z` = last-alive.
- `/Users/gb/github/harmonik/.harmonik/context/captain-lanes.md:102` — operator directive (`set:2026-06-19 expires:~2026-06-22`).
- `/Users/gb/github/harmonik/plans/2026-06-20-fleet-sleep-wake-status-and-next/` — redesign treating it as a permanent fixture.
- `/Users/gb/github/harmonik/plans/2026-06-20-keeper-critique-round2/EXEC_SUMMARY.md` — "W1: per-agent keeper has no supervisor; ctx-watchdog skips the captain".

## Absences explicitly confirmed (no evidence of these)

- No cron job, no launchd plist, no `/tmp/*watchdog*` log.
- No live `ctx-watchdog` tmux session or process.
- No reference to `ctx-watchdog` in any **live** `.claude/skills/` file (only inside detached worktrees + plans).
- No `watchdog`/`300k` entry in `.harmonik/context/project.yaml` (the launcher header cites it, but the live directive is in captain-lanes.md, not project.yaml).
