# Round-2 Critique — Lens: Crews Have No Keeper Watchers

**Date:** 2026-06-20. Reviewer lens: the live fleet runs captain + 5 crews; only the captain
has a keeper. Verified against the running deployment and the code, not memory.

## TL;DR

The gap is real but its root cause is **NOT** what the docs/memory say. The docs claim
"gauge not wired for crews / no `.ctx` files for crews." **Refuted by live state:** every
crew is producing a fresh gauge. The actual gap is narrow and almost free to close: **no
`harmonik keeper --agent <crew>` watcher process is started for crews.** Crew-start arms
every input the keeper needs (`.managed`, `.sid`, gauge env, stable session-id, independent
tmux session) — it just never spawns the watcher, and there is no crew analog of
`captain-launch.sh` step 4 / no respawn script.

## Live state (ground truth, 2026-06-20 ~09:46)

Running keeper processes — ONLY the captain (`ps aux | grep "harmonik keeper"`):
```
harmonik keeper --agent captain --tmux captain --warn-abs-tokens 200000 \
  --act-abs-tokens 215000 --respawn-cmd .../captain-respawn.sh
```
No `harmonik keeper --agent {chani,irulan,jamis,logmine,paul}` process exists.

But the crews ARE fully gauged. `.harmonik/keeper/<crew>.ctx` (current timestamps):
```
chani:   pct 19, tokens 186050, window 1000000  ts 16:41Z  (fresh)
jamis:   pct 13, tokens 134175, window 1000000  ts 16:44Z  (fresh)
paul:    pct 28, tokens 275702, window 1000000  ts 16:46Z  (fresh, ALREADY PAST 215k act band)
logmine: pct 13, tokens 127114                  ts 14:55Z
irulan:  pct  6, tokens  61219                  ts 14:49Z
```
Each crew also has `.managed` (`.harmonik/keeper/<crew>.managed`), `.sid`
(`chani/irulan/jamis/logmine/paul.sid`), a stable `session_id` in
`.harmonik/crew/<crew>.json`, and an independent tmux session
(`harmonik-a3dc45482890-crew-<crew>`).

**Note paul is at 275k tokens — well past the captain's 215k act ceiling — with no watcher
to clear it.** This is exactly the overflow the lens warns about, happening right now.

## Why crews lack keepers — traced

1. **The gauge IS wired for crews.** `~/.claude/settings.json` `statusLine.command` =
   `/Users/gb/github/harmonik/scripts/keeper-statusline.sh` (no hardcoded `HARMONIK_AGENT`).
   `buildCrewLaunchSpec` (`internal/daemon/crewlaunchspec.go:78-81`) sets
   `HARMONIK_AGENT=<name>` and `HARMONIK_PROJECT=<dir>` in the crew's process env.
   `keeper-statusline.sh:47-48` keys the `.ctx` file off inherited `HARMONIK_AGENT`. Result:
   every crew session writes its own `<crew>.ctx` on each repaint. The five fresh files above
   prove it end-to-end.

2. **Crew-start arms every keeper INPUT but never the watcher.** `crewstart.go HandleCrewStart`
   step 6a (`crewstart.go:241-246`, `createCrewManagedMarker:425-438`) writes `.managed`.
   Steps 1/2 mint+persist a stable `session_id` (`resolveSessionID:273-290`). Step 4 sets the
   gauge env. What it does NOT do, and what `captain-launch.sh` step 4 (lines 116-117) DOES:
   `tmux new-session -d -s hk-keeper-<name> "harmonik keeper --agent <name> --tmux <session>
   --warn-abs-tokens ... --act-abs-tokens ... --respawn-cmd <script>"`. There is no crew
   equivalent of that spawn, and no crew analog of `captain-respawn.sh`
   (`captain-launch.sh:95-106`).

3. **The doc/memory "gauge not wired / no .ctx for crews" is STALE.** `.claude/skills/keeper/
   SKILL.md:337-339` ("no `.ctx` files for any crew") and `docs/known-workarounds.md:71-72`
   ("SESSION-KEEPER NOT DEPLOYED FOR CREWS … statusLine hook is not wired") are both refuted
   by the five live `.ctx` files. These were true when crew env did not inject
   `HARMONIK_AGENT`; the env injection (crewlaunchspec.go) plus the agent-less global
   statusLine command (hk-nm32w) fixed the gauge. **The remaining gap is purely the missing
   watcher process — a much smaller fix than "wire the gauge."**

## What it takes to give each crew a keeper (machinery audit)

The watcher needs four inputs; crews already have all four:

| Input | Captain source | Crew status |
|---|---|---|
| `.ctx` gauge file | statusLine + `HARMONIK_AGENT` | ✅ present & fresh |
| `.managed` marker (watcher `.managed`-gate) | captain-launch | ✅ written by `createCrewManagedMarker` |
| stable session-id to `--resume` after `/clear` | minted in captain-launch | ✅ `resolveSessionID` mints+reuses |
| tmux session name for `--tmux` | captain-launch arg | ✅ `harmonik-<hash>-crew-<name>` (registry `handle`) |

Missing pieces, all small:
- **A watcher spawn per crew.** Add a step 6b to `HandleCrewStart` (after `.managed`): start
  `harmonik keeper --agent <name> --tmux <crew-session> --warn-abs-tokens N --act-abs-tokens N
  --respawn-cmd <crew-respawn.sh>` in a `hk-keeper-<name>` tmux session. Idempotency: skip if
  a watcher for `<name>` already runs (mirror "no dup keeper").
- **A crew respawn script** (optional but matches captain). Generate `crew-<name>-respawn.sh`
  that relaunches the crew pane with `--resume <session_id>` (the registry already holds it).
  Without it the keeper still warns/clears in-process; it just can't self-heal a dead pane.
- **Teardown.** `HandleCrewStop` already removes `.managed` (`crewstart.go:487-491`); it must
  also kill the `hk-keeper-<name>` watcher session and the respawn script, else a stopped
  crew leaves an orphan watcher (which then respawns the crew — a leak/fork risk).

## Is it safe to wire crew keepers NOW?

**Mostly yes, with three caveats — none blocking.**

1. **Gauge: not a blocker (refuted).** It is already wired and producing data. The single
   biggest stated risk is false.
2. **Identity for crews: solid.** Crews have lowercase `.sid` and a stable registry
   `session_id`; the watcher's `isPrimarySID` gate and `--resume` re-bind work the same as for
   the captain. The captain's own restart-continuity comment (`captain-launch.sh:7-16`)
   explicitly says it MIRRORS the crew model — so the crew side of identity is the reference
   implementation, not the risky one.
3. **The automatic-cycle reliability hole applies to crews too (round-2 finding #1).** The
   `/clear`→`/session-resume` cycle is still fire-and-forget (no `await-ack` integration,
   hk-uldg). Arming five more open-loop cyclers multiplies exposure to a botched clear. This
   argues for **warn-only or restart-now-only for crews first**, deferring the automatic ACT
   cycle until `await-ack` is wired — NOT for leaving crews unprotected.
4. **Real safety risk = orphan-watcher / fork-bomb.** If a crew keeper has a respawn-cmd and
   `HandleCrewStop` does not kill it, the watcher resurrects a crew you tried to stop. The
   keeper-smoke fork-bomb history (1500+ `*-flywheel` sessions) shows respawn loops are the
   sharp edge. **Mitigation: ship the watcher WITHOUT `--respawn-cmd` first** (warn + advisory
   inject only), add teardown in `HandleCrewStop`, then add respawn later.

## Worth doing now? Verdict

**Yes — high value, and the cheapest it will ever be, because the gauge already works.** This
is NOT a larger initiative; it is one daemon step + one teardown step. The lens's premise
("crews can overflow with no protection") is live-true right now (paul at 275k). Severity:
**high** — an unwatched crew that overflows wedges its pane and silently stops draining its
queue until a human notices.

But scope it conservatively against the round-2 automatic-cycle hole: **Phase 1 = warn-only
crew watcher, no respawn-cmd, with `HandleCrewStop` teardown.** That removes the silent-
overflow blind spot (the crew/captain get a warn inject and can `restart-now` themselves)
without amplifying the open-loop-clear risk or the respawn fork-bomb risk.

### Concrete first step

In `internal/daemon/crewstart.go HandleCrewStart`, add a step 6b after the `.managed` marker
(line 246): spawn `harmonik keeper --agent <req.Name> --tmux <crew tmux session from
windowHandle> --warn-abs-tokens 200000 --act-abs-tokens 215000` in a `hk-keeper-<name>` tmux
session, guarded by a "watcher already running for this name?" check; and in `HandleCrewStop`
(after line 491) kill that `hk-keeper-<name>` session. Omit `--respawn-cmd` in this first cut.
File it as a bead (`codename:keeper`, type=feature, p1). Before coding, **also fix the two
stale docs** (`SKILL.md:325-361`, `known-workarounds.md:69-73`) so future agents stop
believing the gauge is unwired.
