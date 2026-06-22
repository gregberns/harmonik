# Hard-ceiling failsafe — does anything protect crew paul (669k, no watcher)?

READ-ONLY investigation, current `main` (HEAD `2fd42bf6`). Angle: the hard-ceiling
backstop. Bottom line: the hard-ceiling is now WIRED (the stale memory entry is
wrong), but it lives inside the keeper WATCHER, and paul has no watcher — so it
cannot fire for paul. There is NO watcher-independent backstop. That is the gap.

---

## 1. Hard-ceiling wired-or-nil status: WIRED (memory entry is STALE)

The memory note "keeper 280K hard-ceiling UNWIRED in prod / `HardCeilingRestartFn`
nil" describes a state that NO LONGER EXISTS on main. Both tracking beads are
CLOSED:

- `hk-746u` (the UNWIRED bug) — CLOSED 2026-06-20, "RESOLVED by hk-z8d0".
- `hk-z8d0` (wire `HardCeilingRestartFn` + mode gating) — CLOSED 2026-06-20,
  merged with review APPROVE.

On current main the fn is set, not nil:

- `cmd/harmonik/keeper_cmd.go:110-111` — `WatcherConfig.HardCeilingRestartFn` is
  assigned from `keeperHardCeilingRestartFn(resolved.HardCeilingMode, p.ResolvedTmux,
  p.ProjectDir, p.RespawnCmd)` inside `buildKeeperConfigs`. Also wired:
  `HardCeilingTokens` (:108), `HardCeilingMode` (:109), `HardCeilingCooldown` (:105).
- `cmd/harmonik/keeper_cmd.go:530-535` — `keeperHardCeilingRestartFn` is FAIL-CLOSED:
  it returns a real restart closure (`keeper.NewLiveRecoverViaRespawn`) ONLY when
  `mode == HardCeilingModeRestart` AND `respawnCmd != ""` AND `tmuxTarget != ""`.
  Otherwise it returns nil — but nil now degrades to alarm-only, it is not a silent
  no-op (see below).

The bug hk-746u captured was subtler than "fn is nil": the emit USED to live inside
the `HardCeilingRestartFn != nil` guard, so in alarm mode (or with a nil fn) NOTHING
was emitted. That is fixed. The gate now consults `HardCeilingMode` explicitly:

- `internal/keeper/watcher.go:1114-1144` (Backstop 2, SID-independent):
  - `off` → no-op.
  - `alarm` (the zero-value default) → `emitHardCeiling` ONLY, cooldown-gated —
    fires even when `HardCeilingRestartFn` is nil (:1132-1143).
  - `restart` → emit + call `HardCeilingRestartFn`, cooldown-gated; degrades to
    alarm-only if the fn is nil (:1116-1131).
- Default mode is alarm: `internal/keeper/thresholds.go:136-153` (`HardCeilingMode`
  zero value = `HardCeilingModeAlarm`); ceiling value `DefaultHardCeilingTokens =
  280_000` (`thresholds.go:95`), filled by `applyDefaults` at `watcher.go:664-667`.

So: wired, mode-gated, alarm-by-default. In ALARM mode (the default, and what crews
get — see §2) it EMITS `session_keeper_hard_ceiling` but does NOT restart. A restart
only happens if the operator explicitly selects `--hard-ceiling-mode restart` AND
supplies `--respawn-cmd` AND a tmux target resolves.

## 2. The fatal caveat: the failsafe lives INSIDE the watcher — moot for an unwatched crew

The hard-ceiling check is one branch of the watcher poll loop
(`watcher.go:1114`, reached only inside `Watcher.Run`'s tick). It reads the crew's
`.ctx` gauge file and compares `ctxFile.Tokens` to the ceiling. It is NOT a
standalone process, NOT a daemon hook, NOT a supervisor sweep. If no
`harmonik keeper` watcher process is running for an agent, this code never executes
for that agent — full stop. The hard-ceiling is not independent of the watcher; it
IS the watcher.

The comment at `watcher.go:1091-1096` even frames it as "the ONE place the keeper
can act on context overflow while blind" — i.e. its whole purpose is to cover a
keeper whose SID binding has gone stale. It assumes a watcher EXISTS but is
mis-bound. It does nothing for the case where there is no watcher at all, which is
paul's case.

Additionally, even if paul HAD a watcher, crews are launched warn-only and never
get restart mode:

- `internal/agentlaunch/keeperargv.go:100-125` — `KeeperWindowArgv` is the single
  source of truth for the keeper-window argv (daemon crew-spawn + CLI captain both
  route through it). When `WarnOnly` is true it appends `--warn-only` (:107-108) and
  NEVER passes `--hard-ceiling-mode`. So a crew keeper, if it ran, would default to
  alarm mode: it would EMIT `session_keeper_hard_ceiling` at 280k but would NOT
  auto-restart. The crew keeper window is built by the daemon at crew start
  (`internal/daemon/crewstart.go:224-229`, `internal/agentlaunch/keeperwindow.go`
  `SpawnKeeperWindow`).

For paul today the distinction is academic: with no watcher, neither the alarm nor
the restart branch runs.

## 3. Is there ANY watcher-independent backstop? NO.

I searched the daemon and supervisor for any process that reads a pane's context /
token gauge independently of the keeper watcher. There is none.

- Daemon: no reader of the `.ctx` token gauge or any per-pane context-overflow check
  exists outside `internal/keeper`. (grep for `.ctx` / `ctxFile` / `gauge` /
  `HardCeiling` / context-overflow across `internal/daemon`, `internal/supervisor`,
  `cmd/harmonik/supervise` returns only the keeper package and unrelated hits.) The
  daemon's job is bead dispatch + merge; it does not monitor live agent panes for
  context fill.
- Supervisor: `cmd/harmonik/supervise/*` only revives the DAEMON PROCESS when it
  dies and tracks a budget `TokenCap` (`supervise/config.go:69`, a SPEND/budget
  number, not a per-pane context-fill gauge). It has no per-pane context watch and
  no hard-ceiling logic. `supervise reap` / `orphansweep` reap dead tmux
  windows/sessions; they do not read context tokens.
- No external monitor (cron, separate daemon) reads the gauge.

The gauge file itself is written by the per-session `statusLine` hook, and the only
consumer of that gauge is the keeper watcher. No watcher → the gauge (if even
written) is read by nobody.

So the safety gap is explicit: **a session with no keeper watcher has ZERO
context-overflow protection of any kind.** Nothing stops paul at 669k from climbing
to 1M and locking its pane. (Compounding this: per the keeper skill,
`docs/captain-restart.md` and `docs/known-workarounds.md` line 57, the live fleet
historically ships WITHOUT the gauge statusLine wired for crews at all — so even a
crew that DID have a keeper window may have no `.ctx` to read. Confirm paul's actual
state with `harmonik keeper doctor paul`.)

## 4. Failure mode at ~1M tokens for a Claude pane

From the keeper skill (`.claude/skills/keeper/SKILL.md:8, 35-36, 467` and
`docs/known-workarounds.md` §"SESSION-KEEPER NOT DEPLOYED FOR CREWS"): when a
session's context window fills, **the pane stops accepting keystrokes** — it
overflows and wedges. The whole point of the keeper's handoff→/clear→/session-resume
cycle is to reset BEFORE that point. Once a pane has wedged on overflow, the
auto-clear/reseed cannot fire (it needs to inject keystrokes into a pane that no
longer accepts them).

**Only recovery once wedged (manual, operator-driven):**
`harmonik crew stop paul` then `harmonik crew start paul` with a fresh mission file
(`.claude/skills/keeper/SKILL.md:525-540`, known-workarounds.md §Crew context
management). The stop+start IS the context-clear. There is no automatic recovery
path for an unwatched, already-wedged pane.

Note the skill quotes ~200k as the historical wedge point (the old 200k-window
models). On a `[1m]` 1M-window model the wedge point is correspondingly ~1M — paul
at 669k is past the keeper's would-be 280k hard-ceiling alarm but not yet at the
pane-lock point, so there is still a window to act by manually stop+start-ing paul.

---

## Summary

1. **Hard-ceiling is WIRED on main** (`keeper_cmd.go:110-111`, mode-gated at
   `watcher.go:1114-1144`, default alarm) — the "UNWIRED/nil" memory note is STALE;
   beads hk-746u and hk-z8d0 are both CLOSED 2026-06-20.
2. **It is moot for paul**: the hard-ceiling is a branch of the keeper WATCHER loop;
   paul has no watcher, so it cannot fire — and crew keepers are warn-only / alarm
   mode anyway (`keeperargv.go:107-117`), never auto-restart.
3. **No watcher-independent backstop exists** — not in the daemon, not in the
   supervisor (`TokenCap` is budget, not context), not external. An unwatched
   session has zero overflow protection; at ~1M the pane stops accepting keystrokes
   and only a manual `crew stop`+`start` recovers it.
