# A6 — Session-Keeper Enablement Brief for Live Crew Sessions
Date: 2026-06-10

## Context

Session-keeper (hk-ekap1 — the managed-context watcher: warn at 80%, handoff→clear→resume at 90%) is
**fully implemented**. All 3 follow-up beads are **closed**. Nothing is code-blocked. The `.managed`
markers already exist for all 4 crews:

```
.harmonik/keeper/chani.managed
.harmonik/keeper/duncan.managed
.harmonik/keeper/liet.managed
.harmonik/keeper/stilgar.managed
```

However, **zero keeper hook stanzas are wired** in either `~/.claude/settings.json` (global) or
`/Users/gb/github/harmonik/.claude/settings.json` (project). The `statusLine.command` is absent in
both. The Stop and PreCompact hooks are absent. This means:

- No gauge `.ctx` files are being written (keeper is completely blind — it has nothing to poll).
- The Stop hook is not writing `.idle` markers (CrispIdle never fires → the cycle will never trigger
  even after `.managed` is present).
- The PreCompact backstop is absent (auto-compaction will proceed unblocked if a crew reaches the
  compaction threshold).

---

## 1. Exact Enable Command (per crew)

`harmonik keeper enable` wires the 3 stanzas idempotently into `~/.claude/settings.json`. The `.managed`
files already exist for all 4 crews, so `--yes-destructive` is NOT needed for the `.managed` step — but
because `chani`, `duncan`, `liet`, `stilgar` are live agent names (not on the internal blocklist of
`flywheel/named-queues/controlpoints`), the enable command should succeed without `--yes-destructive`.

**Per crew (repeat for chani / liet / stilgar):**

```bash
# 1. Wire stanzas into ~/.claude/settings.json + confirm tmux pane
harmonik keeper enable duncan \
  --project /Users/gb/github/harmonik \
  --scripts-dir /Users/gb/github/harmonik/scripts \
  --tmux "harmonik-a3dc45482890-default:hk-crew-duncan"

# 2. Run doctor to confirm all checks green before starting keeper
harmonik keeper doctor duncan --project /Users/gb/github/harmonik

# 3. Start the keeper watcher (in a detached pane or background)
# -- enable prints the exact command; generic form:
harmonik keeper --agent duncan \
  --tmux "harmonik-a3dc45482890-default:hk-crew-duncan" \
  --warn-pct 80 --act-pct 90
```

Crew tmux handles (from `harmonik crew list`):
| Crew    | tmux handle                                          |
|---------|------------------------------------------------------|
| chani   | harmonik-a3dc45482890-default:hk-crew-chani          |
| duncan  | harmonik-a3dc45482890-default:hk-crew-duncan         |
| liet    | harmonik-a3dc45482890-default:hk-crew-liet           |
| stilgar | harmonik-a3dc45482890-default:hk-crew-stilgar        |

**Note on where stanzas land:** `keeper enable` writes the `statusLine` stanza and the Stop/PreCompact
hook stanzas into `~/.claude/settings.json` (global user settings). The hooks will then apply to ALL
Claude Code sessions, not only crew panes. This is expected — the stanzas are namespaced by
`HARMONIK_AGENT=<name>`, so each crew's gauge writes to its own `.ctx` file. Running `enable` for each
crew will merge 4 stanzas but they are additive and idempotent.

**Dispatching marker discipline (mandatory for Phase-2):**  
Before a crew submits a batch to the daemon queue, it MUST call:
```bash
harmonik keeper set-dispatching <crew-name> --project /Users/gb/github/harmonik
```
And after all in-flight work completes:
```bash
harmonik keeper clear-dispatching <crew-name> --project /Users/gb/github/harmonik
```
Without this, `HoldingDispatch` is always-false and CrispIdle alone is the only mid-dispatch guard.

---

## 2. Are the 3 Follow-Up Beads Blocking?

All 3 are **CLOSED**. None block enablement.

| Bead    | Title (short)                                  | Status | Blocks enable? |
|---------|------------------------------------------------|--------|----------------|
| hk-kzqml | keeper enable + doctor command                | closed | No — IS the enable command |
| hk-rc51s | set-dispatching / clear-dispatching CLI verbs | closed | No — but REQUIRED for safe Phase-2 use (already landed) |
| hk-dopv3 | tmux target from harmonik-<hash>-<agent> convention | closed | No — ergonomics only |

Summary: the mechanism is complete. The enable command (`hk-kzqml`) and the dispatching-safety CLI
(`hk-rc51s`) are both landed. **Zero code work is needed.**

---

## 3. Risks of Enabling on a Live Crew Mid-Work

### Risk 1 — Mid-dispatch /clear (HIGH — mitigated by .dispatching)
**The cycle must not run while a crew has in-flight queue work.** The design gates on
`HoldingDispatch` (the `.dispatching` marker), BUT since no crew currently calls
`set-dispatching` / `clear-dispatching`, `HoldingDispatch` is always false. The only protection
today is `CrispIdle` (the Stop hook fires = no response in progress). A crew that has submitted
a batch but is currently idle (waiting on the daemon) would appear CrispIdle yet BE mid-dispatch.

**Mitigation:** Wire the `set-dispatching` / `clear-dispatching` calls into each crew's mission
script BEFORE enabling Phase-2 (`.managed`). OR: start with Phase-1 (warn-only, no `.managed`)
to validate the gauge and idle signals, then enable Phase-2.

### Risk 2 — No gauge → silent inertness
If `statusLine.command` is not wired before starting the keeper watcher, the keeper emits
`session_keeper_no_gauge` every 120s and never triggers. This is non-destructive but invisible.
`doctor` catches it.

### Risk 3 — Gauge staleness after /clear
Immediately after a `/clear`, `used_percentage` returns NA from the statusLine. The keeper script
skips the write. The gauge file becomes temporarily stale. The keeper's no-gauge self-check fires.
This is expected and recovers on the next assistant response.

### Risk 4 — stop+start resets session_id → breaks anti-loop guard
The anti-loop guard keys on `session_id` dipping below `warn-pct` after a cycle. If a crew is
restarted (stop+start = the current manual context-clear), the new session will have a different
`session_id` and the guard will re-arm correctly. No special handling needed.

### Risk 5 — keeper watcher process lifecycle
Each keeper watcher is a separate process (takes a flock on `<agent>.lock`). If it exits (crash,
signal), the flock releases and a new keeper can be started. The crash-recovery path (`RecoverFromCrash`
on boot) handles any interrupted mid-cycle state. Low risk if started in a supervised pane.

### Risk 6 — The .managed files already exist
All 4 crew `.managed` markers are already present. This means Phase-2 (destructive cycle) is
**already opted in** for all 4 crews the moment a keeper watcher is started. Starting the watcher
without first wiring the Stop hook would leave `CrispIdle` always-false (Stop hook never fires),
so the cycle would never trigger even with `.managed`. But once the Stop hook IS wired, all 4
crews are immediately live on Phase-2. Confirm this is intended before starting any watcher.

---

## 4. Recommended Rollout Sequence

### Phase-1 validation first (1 crew, warn-only)

The `.managed` markers exist for all crews, which means Phase-2 is on as soon as any keeper watcher
starts (assuming hooks are wired). Given this, the recommended sequence is:

**Step 1 — Wire stanzas for ONE crew (e.g. chani), then validate.**
```bash
harmonik keeper enable chani \
  --project /Users/gb/github/harmonik \
  --scripts-dir /Users/gb/github/harmonik/scripts \
  --tmux "harmonik-a3dc45482890-default:hk-crew-chani"
harmonik keeper doctor chani --project /Users/gb/github/harmonik
```
Doctor must pass all checks (binary, statusLine, Stop hook, PreCompact) before proceeding.

**Step 2 — Wait for chani to emit a response (Stop hook fires → .idle written).** Doctor should
then show `idle marker: present`.

**Step 3 — Check gauge is writing:**
```bash
cat /Users/gb/github/harmonik/.harmonik/keeper/chani.ctx
```
Should show `{"pct":..., "session_id":..., "ts":...}` with a recent timestamp.

**Step 4 — Start the keeper watcher for chani** (in a tmux pane or background):
```bash
harmonik keeper --agent chani \
  --tmux "harmonik-a3dc45482890-default:hk-crew-chani" \
  --warn-pct 80 --act-pct 90
```
Because `.managed` exists, this is immediately Phase-2-live. Confirm chani's crew mission includes
`set-dispatching` / `clear-dispatching` calls around queue submits BEFORE this step, or remove
(or rename) `chani.managed` for a warn-only trial.

**Step 5 — Wire remaining crews (duncan, liet, stilgar)** using the same enable + doctor + start
sequence once chani validates.

### Warn-only option (if .managed is too aggressive for now)

Rename `chani.managed` → `chani.managed.disabled` before starting the keeper to run in warn-only
mode (80% warning, no cycle). Validate that the warn injection arrives in the tmux pane. Then
restore `.managed` when ready for full Phase-2.

### Must-land-first

**Nothing needs to land.** All code is present. The only pending operator actions are:
1. Wire the stanzas via `keeper enable` for each crew.
2. Add `set-dispatching` / `clear-dispatching` to crew mission scripts (or accept CrispIdle-only guard).
3. Start a keeper watcher process per crew.

---

## Summary Table

| Item                        | Status                                |
|-----------------------------|---------------------------------------|
| hk-kzqml (enable + doctor)  | CLOSED, landed                        |
| hk-rc51s (dispatching CLI)  | CLOSED, landed                        |
| hk-dopv3 (tmux convention)  | CLOSED, landed                        |
| .managed markers            | ALL 4 crews already present           |
| Hook stanzas wired          | NOT YET (zero stanzas in either settings.json) |
| Gauge .ctx files            | NOT writing (statusLine absent)       |
| .idle markers               | NOT writing (Stop hook absent)        |
| Blocker to enable           | NONE — operator action only           |
| Recommended first step      | `keeper enable chani` + `keeper doctor chani` |
