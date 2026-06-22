# Keeper-coverage investigation — the launch path (READ-ONLY forensic)

Date: 2026-06-21 (live fleet)
Angle: how is a crew keeper SUPPOSED to be armed, and why isn't it?

---

## ROOT-CAUSE STATEMENT

Crew keepers are **wired in source but were never created at runtime** for the
five live crews. The crew-launch path **is** supposed to arm a keeper: the
daemon's `HandleCrewStart → SpawnCrewSession → spawnCrewKeeperWindow`
(`internal/daemon/tmuxsubstrate.go:1346,1430`) creates a sibling `keeper` window
in every crew's tmux session. But the daemon instance that actually spawned
paul/leto/admiral/stilgar/logmine ran with the keeper-window code path **not
executing** — almost certainly an older installed binary built before the
Jun-20 wiring landed (the daemon binary was only rebuilt to a wired version
today at 19:41, hours *after* every crew launched). The result is "armed in
code, never armed in this deployment": each crew session has exactly one window
(`agent`), no `keeper` window, no `harmonik keeper --agent <crew>` process — a
state the keeper/crew-launch skills already document as KNOWN live drift.

This is **"never armed,"** not "armed-then-died." There is zero evidence any
crew keeper ever started: no keeper window exists, no keeper process exists, and
the daemon log contains **zero** keeper-window spawn lines (success *or* the
best-effort failure string) across its entire 8000-line history.

---

## Evidence

### 1. The crew-launch path DOES arm a keeper (source is correct)

The CLI does NOT spawn the crew keeper itself — it delegates to the daemon:

- `cmd/harmonik/crew.go:267-271` — comment: *"The crew's warn-only keeper is
  launched by the DAEMON as a sibling `keeper` window inside the crew session
  (HandleCrewStart → SpawnCrewSession, hk-rmy1)… The CLI no longer spawns a
  separate hk-keeper-<name> session."* The CLI only seeds `.sid`
  (`crew.go:281`) and prints the session_id.

The daemon wiring:

- `internal/daemon/crewstart.go:220-236` — when the substrate implements
  `crewSessionSpawner` (production OSAdapter path), `HandleCrewStart` calls
  `css.SpawnCrewSession(ctx, req.Name, spawn)`. The doc comment (lines 221-230)
  states the session is created *"with TWO windows — 'agent' … and 'keeper'
  (the per-crew session-keeper)."*
- `internal/daemon/tmuxsubstrate.go:1346` — inside `SpawnCrewSession`, right
  after creating the agent window: `s.spawnCrewKeeperWindow(ctx, crewName,
  sessName, spawn)`.
- `internal/daemon/tmuxsubstrate.go:1430-1460` — `spawnCrewKeeperWindow` builds
  the argv via `crewKeeperWindowArgv` and calls `s.adapter.NewWindowIn` to add
  a `tmux.WindowKeeper` window. **Best-effort**: a failure is logged to stderr
  (`"daemon: SpawnCrewSession: launch keeper window for crew %q … (non-fatal)"`,
  line 1457) and does NOT fail the crew start.
- `internal/daemon/tmuxsubstrate.go:1402-1415` (`crewKeeperWindowArgv`) →
  `internal/agentlaunch/keeperargv.go:100` (`KeeperWindowArgv`) — the SHARED
  argv builder. Crew band is **full warn→act→restart** (`WarnOnly:false`, D4 /
  hk-lcga, "crew force-cut by default"), numbers driven by operator config.

So a crew is supposed to end up with: `harmonik keeper --agent <crew> --tmux
<session>:agent --project <dir>` running in a sibling `keeper` window. This is
the SAME shared helper the captain uses (`cmd/harmonik/captain.go:460
ops.SpawnKeeperWindow`), so captain and crew share one keeper-arming code path.

**Conclusion on the "is it wired?" question:** crew keeper-watching is NOT
deliberately unwired in current source. The memory hint (hk-tt9q "no auto-clear
today; manual stop/start") is stale relative to D4/hk-lcga, which flipped the
crew keeper to full force-cut band. The wiring exists and is correct.

### 2. But at runtime, NO crew has a keeper window or process

Live tmux (`tmux list-windows`):

```
crew-paul     1 windows:  1: agent*          ← no keeper window
crew-leto     1 windows:  1: agent*          ← no keeper window
crew-admiral  1 windows:  1: agent*          ← no keeper window
crew-stilgar  1 windows:  1: agent*          ← no keeper window
crew-logmine  1 windows:  1: agent*          ← no keeper window
captain       2 windows:  1: agent*  2: keeper-   ← HAS a keeper window
```

Running keeper processes (`ps`): exactly ONE —
`harmonik keeper --agent captain --tmux …:agent --warn-abs-tokens 200000
--act-abs-tokens 215000` (pid 59748). No `--agent <crew>` keeper anywhere.

The captain's keeper is **manually armed** (explicit `--warn-abs-tokens
200000 --act-abs-tokens 215000`, which the native launcher does not pass — it
omits band numbers so operator config drives them), i.e. it was started by hand,
not by the spawn path.

### 3. The daemon DID run HandleCrewStart for each crew (markers prove it)…

Every crew has a `.managed` marker written by `createCrewManagedMarker` in
**Step 6a** of `HandleCrewStart` (`crewstart.go` step-6a block), so the handler
ran to completion for each:

```
.harmonik/keeper/paul.managed     2026-06-21 07:14
.harmonik/keeper/stilgar.managed  2026-06-21 07:17
.harmonik/keeper/leto.managed     2026-06-21 09:06
.harmonik/keeper/admiral.managed  2026-06-21 09:39
.harmonik/keeper/logmine.managed  2026-06-21 12:34
```

Daemon log confirms the spawns (mission paste via `daemon_pane_write …
purpose=init`) for each crew session_id at exactly those times (log lines 2087,
2088, 2247, 2301 in the 07:07:58 daemon instance).

### 4. …yet the keeper-window step left NO trace — "never armed"

- `grep -c "keeper window" /tmp/hk-a3dc45482890-daemon.log` → **0** across the
  entire 8000-line log (Jun 16 → Jun 21).
- `grep -c "launch keeper window for crew" …` → **0** (the best-effort FAILURE
  string never fired either).

If the wired `spawnCrewKeeperWindow` had executed, it would EITHER have created
a window (none exists) OR logged the non-fatal failure (count 0). The total
absence of both means **the keeper-window code path did not run** in the daemon
that spawned these crews.

### 5. Timeline pins the cause to a stale daemon binary

- Keeper-window wiring landed on **main Jun 20** (hk-yfcc 11:18, hk-rmy1 14:39,
  hk-34sa 14:42).
- The crews launched **Jun 21 07:14–12:34**, spawned by daemon instances that
  relaunched at 07:07:58 and 12:34:17 (daemon-log relaunch markers).
- The currently-installed `/Users/gb/go/bin/harmonik` has mtime **Jun 21
  19:41** — rebuilt today, but HOURS AFTER every crew launched. The daemon
  itself only restarted onto a fresh process at **19:43:56** (pid 47502).

So although the wiring was on *main* before the crews launched, the *installed
binary the morning daemon was running* was evidently built before the wiring was
deployed (or from a branch without it) — which is exactly why the keeper-window
step produced no window and no log line. The crews are independent tmux sessions
(hk-mmlqt) so they survived every subsequent daemon restart; the newer wired
binary now running cannot retroactively add keeper windows to already-spawned
crew sessions.

### 6. The skills already document this as KNOWN drift

- **keeper skill** (`.claude/skills/keeper/SKILL.md`, description lines 11-13):
  *"the KNOWN drift that the gauge is not wired for crews on the live deployment
  (confirm with `keeper doctor`)."*
- **crew-launch skill** (`.claude/skills/crew-launch/SKILL.md:64`): *"The keeper
  rides along automatically"* — describes the intended (code-correct) behavior,
- **crew-launch skill** (`.claude/skills/crew-launch/SKILL.md:427-428`): walks
  it back — *"the deployment-state caveat (the gauge is not yet wired for crews
  on the live deployment — confirm with `keeper doctor`)."*

So the skills' CLAIM ("keeper rides along automatically") matches the CODE, and
their CAVEAT ("not wired on the live deployment") matches the OBSERVED runtime.
The gap is purely deploy-lag: a wired binary was not the one running when the
crews were spawned.

---

## Crew gauges are fresh but unconsumed (corroborates "watcher absent")

`.harmonik/keeper/{paul,leto,admiral,stilgar,logmine}.ctx` all have mtimes
within the last few minutes (20:58-21:02) — the crews' statusline SessionStart
hooks ARE writing gauges. But with no `harmonik keeper --agent <crew>` watcher
process, nothing polls those gauges, so paul at ~669k tokens has no warn/act/
restart logic firing. The gauge half of the loop works; the watcher half was
never started.

---

## Net answer to the brief's questions

1. **Supposed-to-arm path:** daemon `HandleCrewStart → SpawnCrewSession →
   spawnCrewKeeperWindow` (`tmuxsubstrate.go:1346,1430`); CLI delegates, does
   not self-spawn (`crew.go:267-271`). Shared with captain via
   `agentlaunch.KeeperWindowArgv`.
2. **Does crew-launch arm a keeper at all, or only captain?** Source arms BOTH.
   Crew keeper-watching is NOT deliberately unwired in current code (the memory
   hint is stale post-D4/hk-lcga).
3. **Never-armed vs armed-then-died:** **NEVER ARMED.** No keeper window, no
   keeper process, zero keeper-window log lines (success or failure) ever — while
   `.managed` markers prove the rest of `HandleCrewStart` ran. The keeper-window
   step did not execute, consistent with a pre-wiring daemon binary at spawn time.
4. **Skills vs code:** skills claim "keeper rides along automatically" (matches
   code) AND explicitly flag "not wired for crews on the live deployment"
   (matches runtime). Documented drift, not a silent surprise.
