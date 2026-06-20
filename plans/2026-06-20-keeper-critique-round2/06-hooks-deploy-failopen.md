# Keeper Critique R2 — Hooks / Deploy / Fail-Open Lens

**Verdict up front:** Round-1's two CRITICAL findings BOTH PERSIST on current
`main`. (a) The PreCompact-vs-watcher race is structurally unchanged. (b) Silent
fail-open is still pervasive AND — the sharper R2 finding — **the per-agent keeper
watcher has NO supervisor**: it is a bare `tmux new-session` that "quits and stays
dead", with zero continuous liveness detection anywhere in the system. We just
hand-relaunched the captain keeper *because nothing else will*.

---

## 1. PreCompact-vs-watcher race — STILL PRESENT (unchanged)

`internal/keeper/watcher.go`: the precompact check is at **lines 765-769**, and it
sits **inside the fresh-gauge branch, after all three early `continue`s**:

- gauge-absent `continue` — line 623 (and parse-error 636)
- gauge-stale `continue` — line 662
- foreign-session `continue` — line 711

So if the gauge is **stale** (662) or **foreign** (711) — the keeper's two most
common known-broken states — the watcher `continue`s and **never reaches the
`HasPrecompactTrigger` check at 765**. The `.precompact` marker just sits there.
Per the bounded-fallback contract (`keeper-precompact-hook.sh` lines 99-105, Gate
2), the next PreCompact fire then sees the marker still present and `exit 0`s →
**falls through to exactly the lossy native compaction the backstop exists to
prevent.** The degradation is aligned with the keeper's main failure modes, not a
corner case. R1 §5 verbatim still holds.

The heartbeat (`maybeHeartbeat`, line 648) mitigates the *stale* arm somewhat (it
re-writes `.ctx` on a live pane so the gauge ages slower), but it runs only AFTER
the absent/parse `continue`s and does nothing for the **foreign-session**
`continue` (711) — a foreign `.managed` binding still skips the precompact check
entirely. Race not closed; only one of its two triggers is partially papered over.

**Smallest fix:** hoist the `HasPrecompactTrigger` → `RunForPrecompact` block ABOVE
the stale/foreign `continue`s (it is gauge-independent by design — it bypasses the
CrispIdle/act_pct gates anyway), mirroring how `maybeReapOrphanedDecisions` (607)
is deliberately placed before the gauge branches "regardless of THIS agent's own
gauge state." Same rationale, same placement; the precompact backstop wants it too.

---

## 2. Silent fail-open + NO continuous liveness — STILL TRUE, and worse than R1 said

R1 catalogued the per-hook silent `exit 0` paths; all still present verbatim:
- `keeper-statusline.sh`: bad/NA pct or non-numeric → `exit 0` (lines 69-71), no
  log/event. Missing `jq` → every `jq` returns empty → PCT empty → same silent
  `exit 0`. Field path `.context_window.used_percentage` (line 66) still the
  empirically-reverse-engineered schema; a rename silently zeroes the gauge.
- `keeper-sessionstart-hook.sh`: no `jq` → `exit 0` (lines 48-50), `.sid` never
  written, identity silently degrades to the race-prone gauge id.
- `keeper-stop-hook.sh`: pure `touch`; a *missing* Stop hook silently disables the
  `CrispIdle` gate with no runtime "is the Stop hook wired?" check.
- `keeper-precompact-hook.sh`: every gate fail-open `exit 0`.

**What IS new since R1:** the `await-ack` primitive (`internal/keeper/awaitack.go`)
and its `session_keeper_ack_timeout` event. But it is a **point-in-time** liveness
probe used by `harmonik keeper restart-now`/`await-ack` on a *single restart
handshake* — it is NOT continuous keeper-liveness. Confirmed: **nothing consumes
`session_keeper_ack_timeout`** to trigger a watcher restart (grep finds only the
emitter in `awaitack.go` + the core type; no daemon/cmd consumer). So the new event
is a one-shot "did THIS restart land" signal, not "is the keeper still alive."

**There is NO continuous keeper-liveness detector anywhere:**
- `keeper doctor` (`cmd/harmonik/keeper_enable_doctor_cmd.go`) is a manual probe;
  it checks gauge freshness (599), managed-SID-vs-live mismatch (654), hook wiring
  — but explicitly **"the watcher still starts even if checks fail"** (line 723)
  and it has **no check that the watcher PROCESS is alive**. It cannot, by design:
  it runs in a different process from the watcher.
- `scripts/ops-monitor-check.sh` detects *crew agent* staleness via comms
  `last_seen` (lines 287-335, `crew-stale:` signal) — NOT keeper-watcher liveness.
  A dead keeper over a live agent produces no signal here.
- The watcher's own `session_keeper_no_gauge` event fires when the GAUGE is
  blind — but that requires the watcher to be RUNNING to emit it. A **dead
  watcher emits nothing at all.** This is the silent-watchdog anti-pattern R1
  named: the watchdog must be loud when *it* dies, and it is silent.

**Net:** the operator's lived "it silently stopped working" is unaddressed. The
keeper still dies invisibly; the only difference vs R1 is that a *manual* restart
now has an ACK confirmation.

**Smallest LOUD-when-dead change:** add a tiny independent liveness emitter — the
watcher writes a `<agent>.keeperalive` heartbeat file each tick (it already ticks
every 5s), and `ops-monitor-check.sh` adds one check: for every `hk-keeper-<name>`
tmux session that *should* exist (or every `.managed` agent), if
`<agent>.keeperalive` is older than ~30s → emit a `keeper-dead:<agent>` digest
signal. This reuses the existing ops-monitor signal plumbing (the same path that
already emits `crew-stale`) and costs ~15 lines. It is the cheapest way to convert
a silent watchdog death into a loud operator alert.

---

## 3. Deploy reliability — the per-agent keeper has NO supervisor (the gap that bit us)

This is the headline R2 deploy finding. Two "keepers", two completely different
reliability postures:

- **Daemon keeper** = `scripts/hk-keeper.sh`: a real `while true` supervisor (lines
  65-83). Liveness = `pgrep -f "harmonik --project $PROJ"` (69); relaunches on
  death with backoff. Robust.
- **Per-agent session keeper** = launched by `scripts/captain-tools/captain-launch.sh`
  **lines 116-117** as a **bare `tmux new-session -d -s "hk-keeper-$CAP_NAME"`
  running `harmonik keeper …` ONCE.** There is **no while-loop, no pgrep, no
  relaunch.** If that `harmonik keeper` process exits or crashes, the tmux pane
  drops to a shell and **nothing brings it back.** Grep confirms no
  `hk-keeper-<name>` supervisor anywhere except this single one-shot spawn.

This is precisely the **captain-keeper-restart-gap** (memory:
`reference_captain_keeper_restart_gap`) — and it is still live: we just
hand-relaunched the captain keeper because the system has no mechanism to do it.

The asymmetry is the bug: the captain-launch self-heal (lines 85-106,
`captain-respawn.sh` via `--respawn-cmd`) heals the **captain agent pane** if it
dies — but that heal is driven BY the keeper watcher. **There is no one watching
the watcher.** Watcher dies → respawn-cmd never fires → captain pane death also
goes unhealed. Single point of failure with no supervisor.

**Smallest fix:** wrap the watcher launch in the same `while true; do … done`
shape `hk-keeper.sh` already uses, e.g. a `scripts/keeper-watch-supervise.sh`
that loops `tmux has-session -t hk-keeper-$NAME || harmonik keeper …; sleep 15`,
and have `captain-launch.sh` spawn THAT instead of the raw `harmonik keeper`.
This is a near-verbatim copy of the daemon supervisor's proven pattern.

---

## 4. Go↔shell duplicated decision logic that can drift — STILL PRESENT

- **Agent-name derivation** is duplicated across 4 hooks with **inconsistent
  fallback chains** (verified verbatim):
  - statusline (47-53): `env → tmux → "default"` (no positional-arg fallback)
  - sessionstart (55-63): `env → alias → tmux → *exit 0*` (degrades, no default)
  - stop (43-53) & precompact (73-83): `env → alias → arg → tmux → "default"`
  Four slightly-different copies of the same 8-line block; any fix to the rule
  must be applied 4× or they drift.
- **Path-traversal guard** (`*/*|*..*`) present in precompact (85-87) and
  sessionstart (65-67) but **still ABSENT from stop-hook and statusline** — both
  write `${KEEPER_DIR}/${AGENT}.{idle,ctx}` with an unvalidated AGENT.
  Inconsistent security posture across copies of the same pattern.
- **`.managed` opt-in rule** still enforced in bash (precompact Gate 1, line 95)
  AND Go (`cycle.go` MaybeRun / RunForPrecompact gates) — belt-and-braces with
  the known divergence risk R1 described (stale/foreign `.managed`: bash blocks
  compaction while Go refuses to cycle).

The R1 recommendation stands: collapse the hooks to ~5-line shims over a single
tested `harmonik keeper hook <event>` Go subcommand so agent-name derivation,
schema parsing, path-guarding and `.managed` checks live in **one tested place**.
The most failure-prone half of the integration is still the untested bash half.

---

## 5. Severity-ranked summary + what to fix NOW

| # | Finding | Status vs R1 | Severity |
|---|---|---|---|
| 3 | Per-agent keeper has NO supervisor — bare one-shot tmux spawn (captain-launch.sh:116); "quits and stays dead"; no one watches the watcher | NEW emphasis (R1 noted daemon supervisor only) | **CRITICAL** |
| 2 | No continuous keeper-liveness detector; a DEAD watcher emits nothing; `await-ack`/`ack_timeout` is one-shot, unconsumed; doctor/ops-monitor don't probe watcher-process liveness | PERSISTS (await-ack is new but doesn't close it) | **CRITICAL** |
| 1 | PreCompact backstop `continue`'d-past on stale/foreign gauge → degrades to lossy compaction (watcher.go:765 after continues at 662/711) | PERSISTS (heartbeat partially mitigates stale arm only) | **CRITICAL** |
| 4 | Silent fail-open in all hooks (statusline 69, sessionstart 48-50, etc.); welded to undocumented statusLine schema | PERSISTS | HIGH |
| 5 | Agent-name derivation duplicated 4× with inconsistent chains; path-guard missing from stop/statusline; `.managed` rule duplicated bash↔Go | PERSISTS | MEDIUM |

**Fix now, in priority order (all small):**
1. **Wrap the per-agent keeper in a `while true` supervisor** (copy `hk-keeper.sh`'s
   pattern; ~20 lines). Closes the gap that just forced a manual relaunch.
2. **Make the dead watcher LOUD**: watcher writes `<agent>.keeperalive` per tick;
   `ops-monitor-check.sh` emits `keeper-dead:<agent>` when it ages out (~15 lines,
   reuses existing signal plumbing).
3. **Hoist the precompact block above the stale/foreign `continue`s** in
   `watcher.go` (it is already gauge-gate-independent by design).
