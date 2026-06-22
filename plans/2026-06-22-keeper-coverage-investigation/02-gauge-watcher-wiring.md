# Keeper coverage investigation — the gauge/watcher split

**Angle:** Who WRITES the gauge vs who READS it to act? Why a fresh gauge does NOT
imply a live keeper, and whether `keeper doctor` can detect a missing watcher.

**Verdict:** A fresh `<agent>.ctx` proves only that the Claude *session* is alive
and rendering its statusline — NOT that a `harmonik keeper` watcher is running.
The writer (statusline hook, in-session) and the reader/actor (watcher process,
out-of-band) are completely decoupled. `keeper doctor` checks gauge *freshness*
but has **no check** for watcher liveness, even though a ready-made read-only
liveness probe (`LiveKeeperPresent`) already exists and is used elsewhere.

---

## 1. The writer: statusline hook → `<agent>.ctx` (in-session, no watcher needed)

`scripts/keeper-statusline.sh` is wired as the `statusLine.command` in
`~/.claude/settings.json` (provisioned by `harmonik keeper enable`,
`keeper_enable_doctor_cmd.go:273,295`). Claude Code invokes it on **every
statusline render** of a live session — it is part of the session's own render
loop, NOT spawned by any keeper.

It reads the statusline JSON from stdin and atomically writes the gauge:

- Derives the agent from `HARMONIK_AGENT` → tmux session name → `default`
  (`keeper-statusline.sh:47-53`).
- Extracts `pct`, `tokens`, `window_size`, `session_id`
  (`keeper-statusline.sh:66,73,83,86`).
- Atomic write via temp + rename to
  `$HARMONIK_PROJECT/.harmonik/keeper/$AGENT.ctx` (`keeper-statusline.sh:115-122`).

The Go side documents the contract: `CtxFile` "is the JSON content written by
scripts/keeper-statusline.sh to .harmonik/keeper/<agent>.ctx on every statusLine
update" (`internal/keeper/gauge.go:11-12`).

**Key property:** the only precondition for a fresh gauge is *a live Claude
session whose statusline keeps rendering*. No keeper process participates in the
write. So any live crew pane keeps its own gauge fresh forever, watcher or not.

## 2. The reader/actor: the watcher process → reads `<agent>.ctx`, drives warn/act/restart

The warn/act/restart logic lives in the **separate** `harmonik keeper --agent
<crew>` process. Its tick loop READS the gauge the hook wrote:

- `internal/keeper/watcher.go:954` — `ReadCtxFile(w.cfg.ProjectDir, w.cfg.AgentName)`
  on every tick; this is the value it gates warn/act on.
- `internal/keeper/watcher.go:1356` — re-reads `ReadCtxFile` for the staleness /
  live-recover path.
- `ReadCtxFile` itself is `internal/keeper/gauge.go:34-63`.

The watcher acquires an exclusive flock at startup and writes its own PID into
the lockfile: `AcquireLock` →
`.harmonik/keeper/<agent>.lock`, `fmt.Fprintf(fd, "%d\n", os.Getpid())`
(`internal/keeper/keeper.go:59-99`; the run command acquires it at
`cmd/harmonik/keeper_cmd.go:373`).

**This is the split:** the hook (in-session) writes; the watcher (out-of-band
process holding the flock) reads and acts. Kill the watcher and the writes keep
flowing — but nothing reads them to act.

## 3. The trap, confirmed on the LIVE box: fresh gauge ≠ live watcher

Live evidence from `/Users/gb/github/harmonik/.harmonik/keeper/`:

| agent   | `.ctx` mtime  | `.lock` | lock PID | keeper proc actually running? |
|---------|---------------|---------|----------|-------------------------------|
| captain | Jun 21 21:08  | yes     | 59748    | **YES** — `harmonik keeper --agent captain --tmux` (PID 59748) |
| paul    | Jun 21 20:58  | yes     | 96405    | **NO** — PID 96405 is dead; no keeper process for paul |
| leto    | Jun 21 21:00  | none    | —        | **NO** |
| admiral | Jun 21 21:02  | none    | —        | **NO** |
| stilgar | Jun 21 20:58  | none    | —        | **NO** |

`ps aux | grep "harmonik keeper"` on the live box returns exactly **one** line:
PID 59748 (captain). Every crew gauge is fresh (statusline still rendering in a
live crew pane), yet only the captain has a watcher.

Two distinct failure shapes are both visible:
- **No lockfile at all** (leto/admiral/stilgar): a watcher was never started for
  this crew — gauge fresh purely from the live pane.
- **Stale lockfile** (paul): a watcher ran once (PID 96405, lock dated Jun 12),
  died, and left the lockfile behind. The lockfile's mere *existence* is not
  liveness — only an `flock` probe distinguishes a live holder from a corpse.

## 4. Does `keeper doctor` detect the missing watcher? NO.

`runKeeperDoctor` (`cmd/harmonik/keeper_enable_doctor_cmd.go:497-717`) runs
exactly these checks: `binary`, `statusLine`, `Stop hook`, `PreCompact hook`,
`SessionStart hook`, `gauge`, `sid channel`, `idle marker`, `managed`,
`api-key-risk` (enumerated in usage text at lines 1156-1166). **None probes for a
running watcher.**

The `gauge` check is the one most likely to be mistaken for a liveness check, but
it only stats the `.ctx` file's mtime:

> `cmd/harmonik/keeper_enable_doctor_cmd.go:606-620`
> ```go
> // 5. Gauge freshness: has <agent>.ctx been written?
> ctxPath := filepath.Join(cfg.projectDir, ".harmonik", "keeper", cfg.agentName+".ctx")
> info, statErr := os.Stat(ctxPath)
> ...
> age := time.Since(info.ModTime())
> if age > 5*time.Minute {
>     check("gauge", false, ".ctx file is %s old — gauge may be stale ...")
> } else {
>     check("gauge", true, ".ctx fresh (%s old)")
> }
> ```

For every live crew this returns **`✓ gauge .ctx fresh`** — a green check whose
true meaning is "the crew's pane is alive," which doctor presents as keeper
health. There is no corresponding `watcher`/`live keeper` check. The `managed`
check (lines 660-678) can flag a *dead-session binding* (managed SID ≠ live SID),
but says nothing about whether a watcher *process* is running; an unmanaged crew
(no `.managed`, like leto/admiral/stilgar) skips even that.

**Gap quote (doctor usage, lines 1156-1166):** the entire CHECKS list omits any
"is the watcher running" line. So `harmonik keeper doctor --agent paul` on the
live box would print `✓ gauge .ctx fresh` and never reveal that PID 96405 is
dead and nothing is watching paul.

## 5. A watcher-liveness detector ALREADY EXISTS — it is just not wired into doctor

`internal/keeper/keeper.go:102-137` — `LiveKeeperPresent(projectDir, agent) bool`:
a **read-only** probe that opens `<agent>.lock` and attempts a non-blocking
SHARED flock. If it fails with `EAGAIN`/`EWOULDBLOCK` an exclusive holder (a live
watcher) is present → `true`; if it succeeds it releases immediately and returns
`false`; a missing lockfile returns `false`. This is exactly the "is the watcher
process actually running" check doctor lacks — and it correctly distinguishes a
live holder (captain) from a stale lockfile corpse (paul).

It is already used as an advisory liveness gate in **two** places in
`set-dispatching`/`clear-dispatching` (`cmd/harmonik/keeper_cmd.go:650,706`),
which emit a non-fatal WARNING when a `.dispatching` marker is set for an agent
with no live watcher ("I set the marker but nobody's watching"). The same probe
is simply never called by `runKeeperDoctor`.

**The fix shape (not implemented — read-only investigation):** add a `watcher`
check to `runKeeperDoctor` that calls `keeper.LiveKeeperPresent(cfg.projectDir,
cfg.agentName)` and fails when it returns false. That converts the silent
"fresh-gauge false-green" into an explicit `✗ watcher: no live keeper holds
<agent>.lock — gauge is fresh but unwatched`.

---

## Other liveness signals that exist on the box

- **Lockfile + PID** (`<agent>.lock`): authoritative only via `flock` probe, not
  by existence (paul proves existence is a false positive).
- **Presence heartbeat** (`watcher.go` §4, `HeartbeatEnabled` / `HeartbeatThreshold`,
  e.g. lines 383-409, 1356, 1486): a keeper-SIDE heartbeat the *watcher itself*
  emits onto the subscribe stream / refreshes the gauge — present only when a
  watcher is alive. The K6 reap exemption keys off "presence Online (a fresh §4
  heartbeat)" (lines 1633-1684). This is a real liveness signal, but it lives on
  the comms/event bus, not in `keeper doctor`.
- **`.idle` marker**: written by the Stop hook (in-session), so like the gauge it
  reflects session activity, NOT watcher liveness.

---

## 3-line summary

1. The statusline hook (in-session) WRITES `<agent>.ctx` on every render
   (`keeper-statusline.sh:115-122`); the `harmonik keeper` watcher process READS
   it (`watcher.go:954`) and drives warn/act/restart — fully decoupled, so a live
   crew pane keeps its gauge fresh with NO watcher (live box: leto/admiral/stilgar
   fresh + no lock, paul fresh + stale dead-PID lock, only captain has a running
   keeper).
2. `keeper doctor` checks gauge *freshness* via `os.Stat` mtime
   (`keeper_enable_doctor_cmd.go:606-620`) and has NO watcher-liveness check at
   all (CHECKS list, lines 1156-1166) — so an unwatched-but-live crew shows
   `✓ gauge .ctx fresh`, a false-green.
3. A ready-made read-only liveness probe already exists —
   `LiveKeeperPresent` flock-probes `<agent>.lock` (`keeper.go:102-137`) and is
   already used by `set-dispatching` (`keeper_cmd.go:650,706`); it is simply
   never called by doctor. Wiring it in is the gap-closer.
