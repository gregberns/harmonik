# Tmux session organization — design exploration

**Date:** 2026-06-20 · **Trigger:** operator — "captain and its keeper start in different sessions not tied together; same for crew; flywheel processes spread out and clutter the tree view. Find a systematic way of grouping things together."

**Status:** DESIGN / proposal for operator reaction. Not yet beaded.

---

## 1. Current state (mapped from code, 2026-06-20)

Every role spawns its own **top-level tmux session**, across **three inconsistent prefix families**:

| Role | Session name today | Created by | Prefix family |
|------|-------------------|-----------|---------------|
| Daemon spawn-target | `harmonik-<hash>-default` | daemon `EnsureSession` | `harmonik-<hash>-` |
| ↳ implementer/reviewer | **windows** `<beadID>/i<n>`, `<beadID>/r<n>` inside `-default` | daemon `SpawnWindow` | (already nested ✅) |
| Captain | `harmonik-<hash>-captain` | `captain-launch.sh` (`new-session`) | `harmonik-<hash>-` |
| Crew | `harmonik-<hash>-crew-<name>` (legacy `hk-crew-<name>`) | daemon `SpawnCrewSession` | mixed |
| Worker (remote) | `harmonik-<hash>-worker-<name>` | daemon | `harmonik-<hash>-` |
| Flywheel | `harmonik-<hash>-flywheel` | `supervise start` | `harmonik-<hash>-` |
| Supervisor | `hk-<hash>-daemon-supervise` | `supervise` | `hk-<hash>-` |
| **Captain keeper** | `hk-keeper-<agent>` (**no hash!**) | `captain-launch.sh` (separate `new-session`) | `hk-keeper-` |
| Crew keeper | standalone process targeting the crew pane (no own session) | — | — |

Naming helpers are centralized in `internal/lifecycle/tmux/subcommand.go`
(`DefaultSessionName`, `SupervisorSessionName`, `FlywheelSessionName`,
`ResolveDaemonSpawnSession`) and `internal/daemon/tmuxsubstrate.go`
(`crewSessionName`, `workerSpawnSessionName`). Window names: `tmux/windowname.go`.
Keeper re-implements the formula locally in `internal/keeper/tmuxresolve.go`
(`HarmonikSessionName`) because keeper may only import stdlib/core/eventbus.

## 2. Root causes of the clutter

1. **Inconsistent prefixes.** `harmonik-<hash>-*`, `hk-<hash>-*`, `hk-keeper-*`.
   They don't sort together in `tmux list-sessions` / the tree view.
2. **Keeper sessions carry no project hash** → on a multi-project box you can't
   attribute a keeper to a project, and two projects' `hk-keeper-captain`
   collide outright.
3. **Everything is a flat top-level sibling.** A captain and the keeper that
   keeps it alive are unrelated entries; each crew and its keeper likewise.
   Nothing in tmux says "these belong together."
4. **No orphan reaper for dead sessions.** Flywheel panes use `remain-on-exit on`
   for crash recovery, but there is no auto-reap (documented P1 gap,
   `docs/retro/2026-06-10/A3-embed-inventory.md`). Crashes + test/smoke runs
   leave `-flywheel` panes accumulating forever.

The one thing that **is** organized — implementer/reviewer agents as *windows
inside one `-default` session* — is the model to extend to the rest of the fleet.

## 3. Invariants any redesign MUST preserve

- **I1** Keeper survives the captain's `/clear` AND the captain's restart cycle.
- **I2** Keepers + crews survive daemon `SIGTERM`/restart (so they cannot live
  inside the daemon's `-default` session).
- **I3** The orphan reaper must never kill a live agent.

Reaper reality check: `reapDeadCoordinatorSession` (`internal/daemon/orphansweep.go:362-366`)
is proven-by-construction to target **only `-flywheel` sessions** — never
`-default`, never captain/crew/keeper. So the keeper's `hk-keeper-` prefix is
*defensive over-caution*: moving it under `harmonik-<hash>-*` carries no reaper
risk today. (If we ever broaden the reaper, exclude non-`-flywheel`/non-dead
sessions explicitly.)

## 4. Proposal — three tiers

### Tier 1 — Unify the namespace (cheap, high-value, low-risk). RECOMMENDED NOW.

One prefix for everything: **`harmonik-<hash>-<role>[-<name>]`**.

| Role | Today | Proposed |
|------|-------|----------|
| Daemon | `harmonik-<hash>-default` | `harmonik-<hash>-default` (unchanged) |
| Captain | `harmonik-<hash>-captain` | `harmonik-<hash>-captain` (unchanged) |
| Crew | `hk-crew-<name>` / mixed | `harmonik-<hash>-crew-<name>` (kill legacy) |
| Flywheel | `harmonik-<hash>-flywheel` | `harmonik-<hash>-flywheel` (unchanged) |
| Supervisor | `hk-<hash>-daemon-supervise` | `harmonik-<hash>-supervise` |
| Captain keeper | `hk-keeper-captain` | `harmonik-<hash>-keeper-captain` |
| Crew keeper | (none) | `harmonik-<hash>-keeper-<crew>` if it gets a session |

Effect: ONE `harmonik-<hash>-*` block per project, sorted; a keeper sorts
adjacent to its agent (`...-captain` next to `...-keeper-captain`); multi-project
boxes group cleanly and never collide. **No durability change** — still separate
sessions, all invariants hold. Cost: rename in ~3 name helpers + `captain-launch.sh`
+ keeper's local formula; update the reaper/sweep matchers and any session-name
greps; one migration note (old sessions drain naturally on next restart).

### Tier 2 — Structurally pair each agent with its keeper (nicer tree, higher effort).

Make each agent + its keeper **two windows in one session**:
- `harmonik-<hash>-captain` → window `captain` (the claude pane) + window `keeper`.
- `harmonik-<hash>-crew-<name>` → window `crew` + window `keeper`.

The operator expands one session and sees the agent and its watchdog together.
Requires the restart path to be **window-granular**: a captain respawn must
kill+recreate only the `captain` window, never the session (else it kills the
keeper, violating I1). Today `captain-launch.sh` does `new-session`; this becomes
`new-window`/`respawn-window`. Worth doing but it's a real change to the restart
mechanism — sequence it after Tier 1 and coordinate with the keeper-config
redesign (durable tmux↔session-id identity binding is in flight there).

### Tier 3 — Close the orphan-reap gap (hygiene, independent of 1 & 2).

Add `harmonik supervise reap` (the already-specified P1 verb): enumerate
`harmonik-<hash>-flywheel` sessions with `pane_dead=1` predating the live
daemon's start, kill survivors, emit `tmux_orphan_reaped`. Run it on supervisor
boot + on demand. Stops `remain-on-exit` panes from accumulating. This is the
direct fix for "flywheel processes spread out and never maintained."

## 5. Recommendation

Do **Tier 1 + Tier 3 now** — together they fix the operator's stated pain
(navigable, sortable, project-attributable tree + no orphan pile) at low risk and
no durability change. Hold **Tier 2** as a fast-follow once Tier 1 lands and the
keeper-config identity-binding work settles, since it touches the restart path.

Open question for the operator: is true **window-level nesting** (Tier 2) wanted,
or is **consistent adjacent naming** (Tier 1) enough for the tree-navigation goal?
