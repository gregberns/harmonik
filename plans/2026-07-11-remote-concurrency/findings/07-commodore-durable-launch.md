# Commodore durable-launch recipe (survives daemon boot / redeploy)

Date: 2026-07-12 · Live daemon binary: b25e9919 (project hash `a3dc45482890`) · supervisor pid 46203

## TL;DR — the task premise is partially wrong; commodore CAN be protected TODAY

You do **NOT** need hk-zeo5y to land. On the CURRENT (b25e9919, pre-hk-zeo5y) daemon,
commodore survives the next boot/redeploy **if and only if it is launched via the
crew-registry RPC path** (`harmonik crew start commodore`). That path writes a crew
registry record AND a `crew-`-prefixed tmux session, which is exactly orphan-sweep
**exemption (ii)**. It is the same protection every live crew (admiral, stilgar, yueh, …)
already has. hk-zeo5y is about making protection *non-optional for ANY launch path*; the
crew-start path is already protected.

Why commodore kept dying every ~7 min: it was launched via a NON-registry path (bare
`claude --remote-control` / `harmonik agent brief`, which write no registry record), so
it had no exemption and was reaped at the next supervisor-revive / redeploy. The
untracked watchdog papered over this until the watchdog itself died overnight.

Confirmed: there is currently **no `commodore.json`** in `.harmonik/crew/` and no
`crew-commodore` tmux session → commodore is down and unregistered right now.

## 1. The reaper exemptions (verified against internal/daemon/orphansweep.go @ b25e9919)

`RunOrphanSweep` (orphansweep.go:731, called from daemon.go ~1394) runs ONCE at daemon
boot with zero in-memory tracking, so **every** `harmonik-a3dc45482890-*` tmux session is
an orphan-candidate unless it matches an exemption. The exemptions built into the sweep:

- **(i) Captain** — `probeCaptainSentinel` (:550): `captain.sentinel` present AND
  (live captain tmux session with live pane PID, OR recorded `captain.pid` live).
  Protects `harmonik-<hash>-captain`.
- **(ii) Crew registry** — `probeCrewRegistrySessions` (:616): a `crew.Record` on disk
  (`.harmonik/crew/<name>.json`) AND a `harmonik-<hash>-crew-<name>` session whose
  first-pane PID is live. PLUS a live-session fallback (hk-aoapq, :690): ANY
  `crew-`-prefixed session in the snapshot with a live pane PID is exempt even if the
  registry is cold/errored. **This is the exemption commodore uses.**
- **(iii) Coordinator / flywheel** — `probeCoordinatorSentinel` (:357):
  `supervisor.sentinel` present AND supervisor PID live. Protects
  `harmonik-<hash>-flywheel`.
- Plus `DaemonSpawnSession` (hk-9vp51): the daemon never sweeps its own spawn-target
  (`-default`) session.

There is **no name-allowlist for "commodore"/"admiral"/"planner"** — grep shows zero
daemon references. Protection is purely structural (registry record + live crew session),
not name-based. So a fresh commodore session **can be made exempt right now** by giving
it a crew registry record + crew-prefixed session, i.e. launching it through the daemon's
crew-start RPC.

## 2. (Moot — exemption IS available) 

No interim hack needed. The registry exemption above is the durable mechanism. Do NOT
launch commodore outside the crew path (bare `claude --remote-control` or `harmonik agent
brief`) — those leave no registry record and it will be reaped at the next boot.

## 3. Subscriber-pool leak (hk-6629b)

The daemon caps concurrent `subscribe` connections at **32** per process
(`subscribeMaxConnectionsDefault`, subscribe.go). Each long-lived `--follow` subscriber
holds one slot until its process exits; a crash/respawn cycle that doesn't tear down the
old `--follow` leaks the slot until that stale process actually dies. Over a night of
whack-a-mole respawns this bleeds the pool → new subscribers get `subscribe_capacity_exceeded`.

Guidance for commodore: it is an idle planner, not a dispatcher — it must **not** hold a
persistent `--follow`/`subscribe --follow`. Use **poll-on-wake** instead:
`harmonik comms recv --agent commodore` each time it wakes (re-`join` if presence aged).
This matches the established env note that background watchers are torn down at every
turn boundary in these sessions anyway. If a follow is ever used, keep exactly one and
ensure it is torn down before any respawn.

## 4. The watchdog is the wrong durable approach

`scripts/agent-session-watchdog.sh` is **untracked**, a **detached `nohup` shell** (not
supervised), and pinned to Homebrew bash 5 — when relaunched without Homebrew in PATH it
resolved to macOS bash 3.2, `declare -A` failed under `set -u`, and it crashed silently
(died overnight, last start 23:57Z). It is explicitly labeled "interim mitigation only."

Correct durable supervision on this codebase:
- **Redeploy/boot survival:** the crew-registry exemption (above) — this is what actually
  makes commodore survive a supervisor-revive / `daemon-redeploy`. No external watchdog
  needed for that class of death.
- **Context-fill continuity:** arm a **keeper** (`harmonik keeper --agent commodore`) so
  the session hands off → `/clear` → `/session-resume` before the pane overflows. On a
  keeper RESTART the on-disk mission (`.harmonik/crew/missions/commodore.md`) IS re-read.
- **Crash revival (genuine claude crash, not sweep):** currently only the untracked
  watchdog covers this; folding it into the supervisor is tracked in hk-zeo5y (option B)
  and hk-dtpoo (keeper auto-revive). Until then, crash-respawn is the only residual gap —
  but crashes are rare vs. the redeploy-reaps that were the actual nightly problem, and
  the registry path closes the redeploy hole completely.

Do NOT rely on / restart `agent-session-watchdog.sh` as the primary mechanism.

## 5. How commodore is normally launched

Via the daemon crew-start RPC, with its planner mission passed explicitly (FRESH-START
rule D3: `crew start` never auto-reads the on-disk mission — you MUST pass `--mission`):

```
harmonik crew start commodore --mission .harmonik/crew/missions/commodore.md
```

The daemon then writes `.harmonik/crew/commodore.json`, ensures queue `commodore-q`,
launches `claude --remote-control` in session `harmonik-a3dc45482890-crew-commodore`, and
pastes the mission seed. The mission file already tells commodore to IGNORE the crew
dispatch loop and sit idle-armed as a planner.

---

# FINAL RECIPE — durable commodore on the live b25e9919 daemon

Run from repo root `/Users/gb/github/harmonik` (never cd into a worktree):

```bash
# 1. Launch via the registry-protecting crew path (writes commodore.json + crew session).
harmonik crew start commodore --mission /Users/gb/github/harmonik/.harmonik/crew/missions/commodore.md

# 2. Arm a keeper for context-fill continuity (handoff -> clear -> resume before overflow).
harmonik keeper --agent commodore &        # or: harmonik keeper enable commodore
```

Commodore's own boot sequence (in its mission): join comms as `commodore`, post the
online status line, then **poll-on-wake** with `harmonik comms recv --agent commodore` —
do NOT hold a persistent `subscribe --follow` (avoids the 32-slot leak).

## Verify it survived

```bash
# Registry record + live crew session exist:
ls -l /Users/gb/github/harmonik/.harmonik/crew/commodore.json
tmux has-session -t harmonik-a3dc45482890-crew-commodore && echo "SESSION UP"

# Force the exact failure condition (a redeploy/supervisor-revive) then re-check:
#   redeploy per docs/daemon-redeploy.md, or SIGTERM the daemon to trigger a supervisor revive,
#   then confirm the boot sweep SKIPPED commodore:
harmonik subscribe --json | jq 'select(.event_type=="daemon_orphan_sweep_completed") | .payload.crew_sessions_skipped'   # expect >= 1
tmux has-session -t harmonik-a3dc45482890-crew-commodore && echo "SURVIVED REDEPLOY"

# Keeper is actually running (not just a fresh gauge file):
harmonik keeper doctor commodore    # look for the live-watcher check = OK
```

If `crew_sessions_skipped` includes commodore and the session is still up after a
redeploy, protection is working. The only residual gap is a genuine `claude` process
crash (not a sweep) — tracked in hk-zeo5y (fold watchdog into supervisor) / hk-dtpoo
(keeper auto-revive); those are follow-ups, not blockers for durable redeploy survival.
