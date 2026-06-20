# Tmux session organization — COMPLETE (2026-06-20)

Operator ask: the tmux tree was unnavigable — captain and its keeper in unrelated
sessions, crews likewise, and stray flywheel sessions cluttering the view. Goal:
a systematic way to group things, with **true window-level nesting**.

**Outcome: shipped to `main`. Epic hk-0v9e CLOSED.**

## What changed (the new topology)

```
harmonik-<hash>-captain      session → windows: agent (claude) + keeper
harmonik-<hash>-crew-<name>  session → windows: agent (claude) + keeper
harmonik-<hash>-default      session → implementer/reviewer windows (unchanged)
harmonik-<hash>-flywheel     session → flywheel (now auto-reaped when dead)
hk-<hash>-supervise          session → supervisor (kept hk- for sweep-immunity)
```

Each agent now lives in ONE session with its keeper as a sibling `keeper` window
targeting the `agent` window via `--tmux <session>:agent`. A restart respawns only
the `agent` window, so the keeper survives. One `harmonik-<hash>-*` block per
project; keepers sort with their agent.

## Slices (all landed, each worktree-implemented + independently reviewed)

| Slice | Bead | Commit | What |
|-------|------|--------|------|
| N naming | hk-g0e3 | 607054bf | `hk-<hash>-supervise` rename + `tmux.WindowAgent`/`WindowKeeper` |
| K keeper | hk-8j0j | 91ceefbb | keeper `--tmux session:window` inject/gauge target |
| D reap | hk-kzd7 | 7215c80c | `harmonik supervise reap` + boot auto-reap (flywheel-only) |
| B captain | hk-z036 | 84de742d | captain keeper-as-window + window-granular restart |
| C crew | hk-rmy1 | 93e87bf5 | crew agent+keeper windows + drop legacy `hk-crew` name |
| C2 cleanup | hk-34sa | fd46e111 | remove redundant CLI crew-keeper launcher (was 2 keepers/crew) |
| asset re-sync | — | b2711dec, ec740894 | mirror edited skill/script sources to embedded copies |

## Decisions / hazards caught during the work

1. **Supervisor stayed `hk-`.** N originally moved it into `harmonik-<hash>-*`, but
   the daemon boot orphan-sweep prefix-kills that namespace; it would reap its own
   supervisor. Kept `hk-` (sweep-immune, still sorts adjacent). No exclusion coupling.
2. **Agent/keeper windows are sweep-safe.** The window sweep only kills `hk-<hash6>-`
   windows; literal `agent`/`keeper` names don't match. No exclusion needed.
3. **Two-keepers-per-crew** (found in C review): the daemon now spawns the crew
   keeper in-window, but the CLI still spawned a legacy `hk-keeper-<name>` one →
   C2 removed the CLI launcher.
4. **Embedded-asset sync:** edits to `.claude/skills/captain/STARTUP.md` and
   `scripts/captain-tools/*.sh` must be mirrored to `cmd/harmonik/assets/skills/...`
   and `cmd/harmonik/captain-tools/...` or the embed-sync tests go red. Bit us twice.

## Verification

- All touched packages: build + vet + targeted tests green.
- Live tmux smoke (8/8): two-window creation, `respawn-window -k -t :agent` leaves
  the keeper window alive, and tmux honors `session:agent` targets for the keeper's
  ops (capture-pane / set-environment / display-message / list-clients / send-keys).
- D reap kill-safety independently reviewed (anchored regex re-checked per candidate,
  exact-match kill, protect/live-pane/predate triple-skip).

## Unrelated issue surfaced

`hk-7m39` — 3 `TestDaemonWatchdog_*` tests fail on main from the concurrent
`ReviveWindow` work; confirmed pre-existing (fails on the pre-D commit too).
