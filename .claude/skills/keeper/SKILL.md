---
name: keeper
description: >
  The per-session context-fill watcher: what it does to a session when the
  context window fills, and the `harmonik keeper` command surface.
  Load-bearing: must not rot.
---

<!-- Generated from cmd/harmonik/assets/skills/keeper/SKILL.md
     Edit there and mirror in the same commit; scripts/skill-mirror-check.sh fails on drift. -->

# Keeper operating context

The **keeper** watches one long-lived Claude session — a captain, a crew, an
orchestrator — and resets it before its context window overflows. A full pane
stops accepting keystrokes, so the reset has to happen early. The cycle is:
write a handoff, `/clear`, then `/session-resume` the SAME session, so the agent
wakes fresh with its work intact.

**The safety invariant is that the keeper never `/clear`s without a confirmed
handoff nonce.** Every gate below exists to protect it.

Nothing fires unless `.harmonik/keeper/<agent>.managed` exists. Without that
marker the keeper logs a no-op and exits 0. Creating it needs
`--yes-destructive`.

## § How the bands work

A statusLine hook writes the session's token count to
`.harmonik/keeper/<agent>.ctx` each turn. A watcher polls it and crosses three
bands: a **notice**, a **warn**, and an **act** gate that runs the reset cycle.
Above all of them sits a hard ceiling that forces a restart even when the
keeper's session-id binding is wrong, so a mis-bound keeper cannot let a pane
overflow.

Two things about the numbers:

- **Every value is operator-set.** harmonik applies no runtime default. An unset
  required key makes the keeper refuse to start with one aggregated error naming
  every missing key. Generate a complete block with `harmonik keeper config
  --example >> .harmonik/config.yaml`, then tune it. Precedence is CLI flag, then
  `config.yaml`, then refuse to start. Threshold changes need a keeper restart.
- **Read the numbers from `internal/keeper/thresholds.go`, never from prose.**
  The band has been retuned more than once, and every copy of it in a doc has
  gone stale.

The effective gate is `min(absTokens, pctCeil × windowSize)`
(`minAbsOrPctCeil`). That is deliberate: one band then works on a 200k window and
on a 1M window without a re-tune, and on a 1M-window model the absolute cap is
what fires.

**`--warn-pct` / `--act-pct` are tighten-only.** They feed in as the pct-ceil, so
they can only move a gate earlier. A value looser than the resolved ceil is
rejected and the keeper refuses to start. Older runbooks suggest `--warn-pct 80`
/ `--act-pct 90`; both are looser than the defaults and both hard-fail. To move
the band, use `--warn-abs-tokens` / `--act-abs-tokens`.

## § What you do at each band

| crossing | the keeper does | you do |
|---|---|---|
| **notice** | sends early continuity guidance | keep working; shape state so a fresh session could resume it |
| **warn** | asks for a durable checkpoint | bring the current unit to a checkpoint; captains run `restart-now` when ready |
| **act** | starts the handoff request once the safety gates pass | finish the handoff |
| **hard ceiling** | forces handoff and restart regardless of session-id binding | nothing — it is the last-resort backstop |

The act path holds off while an operator is attached to the pane, and a real
user turn during the handoff wait parks the cycle before the `/clear`.

**On a warn, refresh your `HANDOFF-<agent>.md` and keep working.** Read that file
before you Write it — it already exists, and the Write tool refuses a file the
current session has not read, which after a `/clear` is every file.

**Never exit or `/quit` your own session on a warn.** The keeper owns the
clear-and-resume cycle. Self-terminating ends the session for good; there is no
supervised respawn path.

A **captain** additionally drives its own restart, at a clean idle point with no
dispatch in flight: finish the current unit, write `HANDOFF-captain.md` with a
fresh `<!-- KEEPER:<nonce> -->` comment, run `harmonik keeper restart-now --agent
captain`, then keep the turn open and stop typing. The keeper fires on its next
tick. The handoff carries INTENT only — the boot runbook re-grounds on live
state, so do not snapshot queue or daemon state into it.

## § Command surface

**Every keeper verb is flag-only.** Pass `--agent <name>`. A positional agent is
rejected with exit 2, because it used to be silently accepted as an agent named
after the flag — the recurring restart-now failure.

| command | what it does |
|---|---|
| `harmonik keeper --agent <name> [--tmux T]` | start the watcher; blocks until signal. Exit 2 means another live keeper already holds the lock — there is only ever one per agent. |
| `harmonik keeper doctor --agent <name>` | read-only drift check. Run this to find the ACTUAL state. |
| `harmonik keeper enable --agent <name> --tmux T [--yes-destructive]` | idempotent wiring of the statusLine and Stop / PreCompact hooks. |
| `harmonik keeper config --example` | print a complete `keeper:` config block. |
| `harmonik keeper restart-now --agent <name>` | run the cycle now. Prints `nonce=rn-<millis>`. |
| `harmonik keeper ping --agent <name> --nonce N` | inject an ACK line for a liveness check. |
| `harmonik keeper await-ack --agent <name> --nonce N [--kind restart\|ping]` | confirm the ACK landed. Exit 0 observed; exit 3 timed out and a `session_keeper_ack_timeout` event was written. |
| `harmonik keeper set-dispatching` / `clear-dispatching --agent <name>` | defer the reset while a queue batch is in flight, then release. Both idempotent. |
| `harmonik keeper hold` / `release --agent <name>` | suspend the act cutoff while co-working with an operator, then release. |

`--warn-abs-tokens` / `--act-abs-tokens` set the band on the watcher.
`--respawn-cmd` relaunches the agent through tmux after the gauge goes stale at a
shell prompt.

**`enable` edits the GLOBAL `~/.claude/settings.json`**, which affects every
Claude session on the box. Do it deliberately, ideally when no crew is mid-task.
It refuses to arm a known-live agent without `--yes-destructive`, because a
misconfigured `.managed` marker can `/clear` a working session.

### restart-now and await-ack

`restart-now` validates the current session and its non-empty handoff. It then
starts a detached driver and returns. The return lets the active tool call end.
The driver sends one `/clear`, waits until the SessionStart hook reports a new
session ID, removes input left at the new prompt, and then sends the resume
brief. It does not retry `/clear`.

Read the command exit code and the printed nonce. A zero exit means the driver
was accepted. It does not mean that session turnover finished. The driver writes
its result to `.harmonik/keeper/<agent>.restart-now.log`. A missing turnover is a
visible failure. Investigate it as a pane, hook, or session-lifecycle fault.

### set-dispatching, and hold

Call `set-dispatching` **before** submitting a batch to the daemon queue so the
keeper does not `/clear` you mid-dispatch, and `clear-dispatching` once the
in-flight work drains.

`hold` is the different case: a dispatch-hold defers the cycle while a queue
batch is in flight, a `hold` defers it while an **operator is in the loop**. Warn
still fires under a hold; only the act-and-restart action is suspended. Two
things keep a hold from leaking:

- The marker is keyed by the live session id, which is re-minted on every
  `/clear`, so a hold can never survive a restart.
- A hold older than `cadence.hold_ttl` is ignored regardless, so a forgotten one
  self-clears after an operator walks away.

The hard-ceiling restart overrides a hold. Overflow protection wins.

**A hold is only honoured by a keeper new enough to know about it.** An older
watcher ignores the marker and restarts anyway. Check binary age with `keeper
doctor` before you rely on a hold to protect a live session.

## § Confirming the keeper is actually armed

The gauge writer and the watcher are decoupled: the statusLine hook writes the
`.ctx` file on every render whether or not any watcher is running. **A fresh
gauge file does not mean a keeper is active.** Run `harmonik keeper doctor
--agent <agent>` and read its `live-watcher` check, which probes the lock and so
distinguishes a running keeper from a stale corpse lockfile.

`doctor` also checks the binary age, the statusLine and hook wiring, the gauge,
the idle marker, whether `ANTHROPIC_API_KEY` is set (which would bill the API
pool instead of the subscription), and `.managed`. It reports the last cycle
phase; a `phase=parked reason=operator_turn_recent` is a transient deferral that
the watcher retries, not a hold.

**`.managed` present with no watcher is a deadlock, not a degraded mode.** A live
captain once sat at a typed-but-unsent `/clear` waiting for a cycle that could
never fire, while config, hooks, gauge, marker and pane all read green. Nothing
on the box supervises the watcher. If `doctor` shows a missing watcher, start one
by hand — `harmonik keeper --agent <agent>` — before you rely on the cycle.

Crew keepers are armed by the daemon at `crew start`, which adds a sibling keeper
window and writes the `.managed` marker. A crew spawned by an older binary is
unwatched, and `doctor` is what tells you.

If the keeper is not armed and a crew wedges near the ceiling: `harmonik crew
stop <name>` then `harmonik crew start <name>` with a fresh mission
(`docs/known-workarounds.md` § Crew context management).

## § What a restart means for the fleet

A keeper restart is a NON-EVENT. In-flight queue work is not lost — a crew's
named queue keeps draining on the daemon independent of the session, and
`{queue, epic_id}` are durable in beads. On resume the agent re-runs its own boot
sequence with a fresh dedupe set. Do not read a transient presence drop as a crew
failure and do not `crew start` a replacement; the crew comes back under the same
name. The crew-side steps are in the **crew-launch** skill, § Restart via the
keeper.
