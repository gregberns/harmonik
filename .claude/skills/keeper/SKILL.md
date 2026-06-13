---
name: keeper
description: >
  Agent-facing operating contract for the harmonik session-keeper — the
  per-orchestrator / per-crew context-fill watcher that gauges a long-lived
  Claude session's context usage and, when it fills, drives an
  intent-preserving handoff → /clear → /session-resume cycle BEFORE the pane
  overflows and stops accepting keystrokes. Load this when you run (or manage)
  any keeper-watched session — captain, crew, flywheel, orchestrator. Covers:
  the two thresholds (warn vs act) and their REAL default values, the command
  surface (enable / doctor / set-dispatching / clear-dispatching / the watcher
  itself), the hard rule "do NOT self-/quit on a keeper warn" (only the keeper's
  ACT path performs the reset-cycle), crew-restart re-hydration, and the KNOWN
  drift that the gauge is not wired for crews on the live deployment (confirm with
  `keeper doctor`). Load-bearing: must not rot. Composes with crew-launch
  (§ Self-restart via the keeper) and captain (§10 restart continuity).

sources:
  - cmd/harmonik/keeper_cmd.go
  - cmd/harmonik/keeper_enable_doctor_cmd.go
  - cmd/harmonik/keeper_dispatching_cmd_hkrc51s_test.go
  - cmd/harmonik/keeper_enable_doctor_cmd_test.go
  - internal/keeper/cycle.go
  - internal/keeper/watcher.go
  - docs/known-workarounds.md
  - docs/captain-restart.md
  - .claude/skills/crew-launch/SKILL.md
  - .claude/skills/captain/SKILL.md
---

# Keeper operating context

The **keeper** is harmonik's per-session **context-fill watcher**. One keeper
process supervises one long-lived Claude session (a captain, a crew, the
flywheel, an orchestrator). It reads how full that session's context window is
each turn and, before the window overflows (a full pane stops accepting
keystrokes), it performs an **intent-preserving reset cycle**: write a handoff,
`/clear`, then `/session-resume` the SAME session so the agent wakes up fresh
with its work intact.

You interact with the keeper in three roles:

1. **As a managed session** — you keep working; you read its warn injections but
   you do NOT act on them yourself (the keeper owns the reset). See § Don't
   self-quit on a warn.
2. **As an operator/captain wiring it up** — `keeper enable` / `keeper doctor`.
3. **As a dispatcher** — `keeper set-dispatching` / `clear-dispatching` to defer
   the reset while a queue batch is in flight.

---

## § What the keeper is / when it fires

Each turn, the keeper's **statusLine hook** (`keeper-statusline.sh`) writes
`.harmonik/keeper/<agent>.ctx` with the session's `pct`, absolute `tokens`, and
`session_id`. A **watcher** loop polls that gauge every ~5s
(`keeper_cmd.go:285`) and crosses one of two thresholds:

- **WARN** — the session is getting full. The keeper injects a wrap-up-warning
  prompt into the tmux pane (if `--tmux` is set) and emits `session_keeper_warn`.
  **This is informational. The agent keeps working.**
- **ACT** — the session is near-full AND idle (CrispIdle) AND not holding a
  dispatch. The keeper runs the **reset cycle** (`Cycler.MaybeRun`): truncate the
  handoff → inject `/session-handoff` → **poll for the handoff nonce** → ONLY
  THEN `/clear` → `/session-resume <agent>`. The invariant — *never `/clear`
  without a confirmed handoff nonce* — is what makes the cycle safe
  (`docs/captain-restart.md`).

The whole thing is **`.managed`-gated**: if `.harmonik/keeper/<agent>.managed`
is absent, the keeper logs a no-op and exits 0 (passive mode — no reset cycle
ever fires). Creating `.managed` requires explicit destructive consent (see
§ keeper enable).

---

## § The two thresholds — REAL values from code

The keeper evaluates **both** an absolute-token threshold and a
percent-of-window threshold and uses **whichever is smaller** — i.e. the
effective threshold is `min(absTokens, pctCeil * windowSize)`
(`internal/keeper/cycle.go:39-43`). This is deliberate so the same defaults
work on a 200k window (the pct-ceil wins, ~170k) and a 1M window (the abs cap
wins, 300k) — preventing a `90%` gate from firing only at ~900k tokens
(Refs: hk-cl74g).

| gate | abs-token default | pct default | source |
|---|---|---|---|
| **WARN** | `WarnAbsTokens = 240000` | `--warn-pct 80` (pct-ceil 0.70) | `cycle.go:147-151`, `keeper_cmd.go:61` |
| **ACT** | `ActAbsTokens = 300000` | `--act-pct 90` (pct-ceil 0.85) | `cycle.go:141-146`, `keeper_cmd.go:62` |
| **FORCE-ACT** | `ForceActAbsTokens = 380000` | pct 95 (pct-ceil 0.95) | `cycle.go:56-57,142` |
| window fallback | `FallbackWindowSize = 200000` | — | `watcher.go:249`, `--window-size` |

- The **pct gates are only used as a fallback** when the gauge does not emit
  absolute token counts (`CtxFile.Tokens == 0` or `WindowSize == 0`) — i.e.
  older Claude Code versions (`cycle.go:59-63`). When absolute tokens ARE
  present, the abs/pct-ceil `min` formula above governs.
- **FORCE-ACT** is the hard ceiling: above it the cycle fires **unconditionally,
  bypassing the CrispIdle gate**, so a perpetually-busy session that never goes
  idle still gets cleared before exhaustion (`cycle.go:50-57`, Refs: hk-0uu).
- **All of these are configurable flags** on the watcher: `--warn-pct`,
  `--act-pct`, `--warn-abs-tokens`, `--act-abs-tokens`, `--window-size`
  (`keeper_cmd.go:59-65`). On a 1M-token window the bare `80/90` pct defaults
  defeat the intent — captains relaunch with tighter pct (`--warn-pct 25
  --act-pct 30`) so the gauge fires at a sane absolute fill. The abs-token caps
  (240k/300k/380k) already bound it regardless.

---

## § Command surface

All keeper verbs are under `harmonik keeper`. Top-level usage:
`keeper_cmd.go:243` (`keeperTopUsage`).

### `harmonik keeper --agent <name> [flags]` — the watcher (run this to start it)

Starts the watcher loop and blocks until SIGINT/SIGTERM.

Flags (`keeper_cmd.go:59-66`):

| flag | default | meaning |
|---|---|---|
| `--agent <name>` | — (**required**) | identifies the lockfile + `.managed` marker |
| `--tmux <target>` | auto-derived | pane to inject warn/handoff into; auto-resolved from `harmonik-<hash12>-<agent>` if omitted (`keeper_cmd.go:111-116`) |
| `--warn-pct N` | `80` | pct fallback warn gate |
| `--act-pct N` | `90` | pct fallback act gate (`.managed`-gated) |
| `--warn-abs-tokens N` | `240000` | absolute warn gate |
| `--act-abs-tokens N` | `300000` | absolute act gate |
| `--window-size N` | `200000` | assumed window when gauge reports `WindowSize==0` |
| `--respawn-cmd <cmd>` | — | supervised respawn: after the gauge goes stale 20s and the pane is at a shell prompt, run `sh -c <cmd>` to relaunch the agent (requires `--tmux`; 90s cooldown). Refs hk-3w2. |

**Behaviour** (`keeper_cmd.go:27-35,281-291`): acquire the single-keeper lock →
boot-doctor (loud, non-fatal) → check `.managed` (absent ⇒ no-op exit 0) →
resolve tmux target → crash-recovery (resume any interrupted prior cycle) →
poll the gauge every 5s. Emits `session_keeper_warn` on the first upward warn
crossing, runs the reset cycle on the act crossing (CrispIdle + no in-flight
dispatch), and emits `session_keeper_no_gauge` at boot and every 120s when the
gauge file is absent/stale (so a missing `statusLine.command` is visible, not
silent).

**Exit codes** (`keeper_cmd.go:37-41,301-304`): `0` clean (no-op or signal
shutdown); `1` argument or I/O error; `2` lock already held by another live
keeper (only ONE keeper per agent).

### `harmonik keeper enable <agent> [flags]` — wire the hooks

IDEMPOTENT wiring of the three keeper stanzas into the GLOBAL
`~/.claude/settings.json`: `statusLine` + `Stop` hook + `PreCompact` hook. Backs
up settings.json first, normalizes env-var names, seeds `HANDOFF-<agent>.md`,
validates the `--tmux` pane, and prints the exact run command
(`keeper_enable_doctor_cmd.go:139-287`).

Flags (`keeper_enable_doctor_cmd.go:889-922`): `--project DIR`,
`--scripts-dir DIR` (auto-detected relative to the binary if omitted),
`--tmux TARGET`, `--yes-destructive`.

**Safety / gates:**
- It edits the **GLOBAL** settings.json — a machine-wide change that affects
  EVERY Claude session on the box (`docs/captain-restart.md` Enablement step 1).
  Do it deliberately, ideally when no crew is mid-task.
- `.managed` (the marker that makes the reset cycle LIVE) is **never created
  without `--yes-destructive`** (`keeper_enable_doctor_cmd.go:256-280`).
- Known live agents (`flywheel`, `named-queues`, `controlpoints`) are **refused
  without `--yes-destructive`** — a misconfigured `.managed` could `/clear` an
  active session (`keeper_enable_doctor_cmd.go:29-33,151-160`).
- The `statusLine` stanza is normalized to include `"type":"command"`; without
  it Claude Code rejects the whole settings.json and disables ALL hooks (hk-hs1,
  `keeper_enable_doctor_cmd.go:610-617`).

**Exit codes** (`keeper_enable_doctor_cmd.go:919-922`): `0` success; `1`
argument, validation, or I/O error.

### `harmonik keeper doctor <agent> [--project DIR]` — read-only drift validator

READ-ONLY; mutates nothing. Also runs automatically at keeper **boot** as a loud
diagnostic (`keeper_enable_doctor_cmd.go:539-552`). **Run this to find out the
ACTUAL deployed keeper state.** Checks (`keeper_enable_doctor_cmd.go:366-536`,
`924-948`):

| check | passes when |
|---|---|
| `binary` | `harmonik` on PATH and `<30` days old |
| `statusLine` | `keeper-statusline.sh` wired (+ `HARMONIK_PROJECT=`, `"type":"command"`, no literal `HARMONIK_AGENT=` pollution) |
| `Stop hook` | `keeper-stop-hook.sh` wired in `hooks.Stop` |
| `PreCompact hook` | `keeper-precompact-hook.sh` wired in `hooks.PreCompact` |
| `gauge` | `.harmonik/keeper/<agent>.ctx` exists and is `<5` min old |
| `idle marker` | `.harmonik/keeper/<agent>.idle` written (Stop hook has fired) |
| `managed` | `.harmonik/keeper/<agent>.managed` present (reset cycle LIVE) |
| `api-key-risk` | `ANTHROPIC_API_KEY` NOT set (else keeper-launched claude bills the API pool, not the subscription) |

**Exit codes** (`keeper_enable_doctor_cmd.go:945-948`): `0` all checks passed;
`1` one or more failed (details on stdout).

### `harmonik keeper set-dispatching <agent> [--project DIR]` — hold the reset

Writes `.harmonik/keeper/<agent>.dispatching` so `HoldingDispatch → true`
(`keeper_cmd.go:162-200`). The reset cycle **defers** while this marker is
present. **Call it BEFORE submitting a batch to the daemon queue** so the keeper
does not `/clear` you mid-dispatch (`keeperTopUsage` VERBS). Exit codes
(`keeper_cmd.go:166-171`): `0` written; `1` argument / path-traversal / I/O
error. Verified by `keeper_dispatching_cmd_hkrc51s_test.go:15-34`.

### `harmonik keeper clear-dispatching <agent> [--project DIR]` — release the hold

Removes the `.dispatching` marker so `HoldingDispatch → false`
(`keeper_cmd.go:202-241`). **Idempotent** — an already-absent marker is not an
error (`keeper_dispatching_cmd_hkrc51s_test.go:86-96`). Call it once all
in-flight queue work has completed. Exit codes: `0` removed (or already absent);
`1` argument / path-traversal / I/O error.

---

## § Warn vs act — what to do at each

| crossing | keeper does | YOU do |
|---|---|---|
| **WARN** (≥240k tokens / `--warn-pct`) | injects a wrap-up prompt, emits `session_keeper_warn` | **Keep working.** Optionally refresh your `HANDOFF-<agent>.md` so the eventual reset carries good state. Do NOT `/quit`, do NOT `/clear`, do NOT stop. |
| **ACT** (≥300k / `--act-pct`, CrispIdle, no dispatch hold) | runs handoff → nonce-poll → `/clear` → `/session-resume` | **Nothing.** The keeper owns the cycle. If you are mid-dispatch, hold it off with `keeper set-dispatching` first so ACT waits. |
| **FORCE-ACT** (≥380k / `--act-pct` 95) | runs the cycle **unconditionally** (bypasses CrispIdle) | **Nothing** — this is the safety net for a never-idle session. |
| **operator attached** | act-path goes **warn-only**: the destructive injection is suppressed so the keeper never races a human's keystrokes; warn/gauge emissions continue; the cycle resumes once the operator detaches | nothing (`cycle.go:128-137`, hk-6qf) |

---

## § Don't self-quit on a keeper warn (HARD RULE)

**A warn is informational. The agent MUST NOT `/quit`, `/clear`, or stop in
response to it.** Only the keeper's **ACT path** performs the
handoff → `/clear` → `/session-resume` cycle, and it rebinds your minted
`--session-id` so you wake up as the SAME agent with context cleared.

If you obey a warn injection's "wrap up / quit" wording and actually exit, and
you were launched **without a supervised respawn wrapper** (`--respawn-cmd`),
you **stay dead** — defeating the entire cycle (`captain` skill §10; this is a
documented live failure mode). So on a keeper context-warning:

1. Refresh your `HANDOFF-<agent>.md` (so the eventual reset carries good state).
2. **Keep working.** Let the keeper cycle you when it crosses ACT.

The captain and every crew are themselves keeper-managed; this rule applies to
all of them.

---

## § Crew-restart re-hydration

When the keeper cycles a session, it `/clear`s and **`/session-resume`s the SAME
`session_id`** — so the agent re-runs its full boot sequence from scratch, with
context cleared but identity and durable state intact.

**In-flight queue work is NOT lost.** A crew's named queue keeps draining on the
**daemon** independent of the crew's session, and `{queue, epic_id}` are durable
in beads (`assignee == crew_name`). On resume the crew re-reads its handoff
frontmatter and the `br show <epic_id> --assignee` mirror, re-`join`s comms with
a fresh dedupe `seen` set, and re-processes its inbox idempotently. See
**crew-launch § Self-restart via the keeper** for the exact re-hydration steps,
and **captain §10 Restart continuity** — *a keeper restart is a NON-EVENT for
the captain*: do not treat a transient presence drop as a crew failure and do not
re-`crew start`; the crew returns under the same name.

---

## § KNOWN DRIFT — the keeper is NOT wired for crews on the live deployment

There is a documented inconsistency in the docs, and the source ships the gauge
**OFF by default**:

- The watcher is **`.managed`-gated and no-op without the markers**: absent
  `.managed`, `keeper --agent ...` logs and exits 0; absent `statusLine.command`
  in `~/.claude/settings.json`, **no `.ctx` gauge file is ever written** and the
  keeper emits `session_keeper_no_gauge`. The wiring is NOT automatic — it
  requires an explicit, destructive `keeper enable ... --yes-destructive`
  (`keeper_enable_doctor_cmd.go`).

- **`docs/captain-restart.md §Current deployment state (2026-06-09)` confirms
  the live fleet ships WITHOUT the gauge:** no `statusLine.command` wired, no
  Stop/PreCompact stanzas, no watcher running, no `.ctx` files for any crew.

- **`docs/known-workarounds.md` (line 57, "SESSION-KEEPER NOT DEPLOYED FOR
  CREWS")** documents the operational consequence: when a crew's context fills
  (~200k tokens) the pane stops accepting keystrokes and the auto-clear/reseed
  cycle does NOT fire because the statusLine hook is not wired. **Current
  workaround: manual `harmonik crew stop <name>` then `crew start <name>` with a
  fresh mission file.** (Refs hk-ekap1, hk-njetn; enablement deferred to an
  operator-supervised session.)

- Meanwhile the **captain** skill (§A lane snapshot) instructs relaunching the
  watcher with `--warn-pct 25 --act-pct 30` — an **armed** posture that assumes
  the gauge IS wired. These two coexist: the captain note describes how to arm
  it; the deployment-state docs say it is not currently armed for crews.

**KNOWN DRIFT:** whether the gauge is armed for a given agent on YOUR box is not
something to assume from the docs — they disagree. **Confirm the ACTUAL state
with `harmonik keeper doctor <agent>`** before relying on the keeper to clear
that session. If `doctor` reports the `statusLine` / `gauge` / `managed` checks
failing, the keeper is passive and you must fall back to manual
stop/start. Source-verified facts: the gauge is OFF unless `keeper enable
--yes-destructive` has wired it; everything else about whether a specific live
session is armed is a deployment fact to check, not infer.

---

## § Quick reference

```bash
# Is the keeper actually armed for this agent? (run this first — settles the drift)
harmonik keeper doctor <agent> --project $HARMONIK_PROJECT

# Wire the hooks (GLOBAL settings.json edit; --yes-destructive arms the reset cycle)
harmonik keeper enable <agent> --tmux <pane> --yes-destructive

# Start the watcher (tighten pct on a 1M window so the gauge fires sanely)
harmonik keeper --agent <agent> --tmux <pane> --warn-pct 25 --act-pct 30

# Defer the reset while a queue batch is in flight, then release
harmonik keeper set-dispatching <agent>
harmonik keeper clear-dispatching <agent>
```

If the keeper is NOT armed (per `doctor`) and a crew wedges at ~200k tokens:
`harmonik crew stop <name>` then `harmonik crew start <name>` with a fresh
mission (known-workarounds.md §Crew context management).

---

## References

- `cmd/harmonik/keeper_cmd.go` — the watcher, `set-dispatching` /
  `clear-dispatching`, flags, exit codes, `keeperTopUsage`.
- `cmd/harmonik/keeper_enable_doctor_cmd.go` — `enable` / `doctor`, the
  settings.json wiring, the doctor check table, usage strings.
- `internal/keeper/cycle.go` — the threshold defaults (240k/300k/380k), the
  `min(abs, pct*window)` formula, CrispIdle / force-act / operator-attached
  gating.
- `internal/keeper/watcher.go` — the poll loop, `FallbackWindowSize`, warn
  emission.
- `cmd/harmonik/keeper_dispatching_cmd_hkrc51s_test.go` — the
  set/clear-dispatching contract (markers, idempotency, exit codes).
- `docs/captain-restart.md` — the captain reset cycle and the §Current
  deployment state drift note.
- `docs/known-workarounds.md` §Crew context management — the
  not-deployed-for-crews workaround.
- `.claude/skills/crew-launch/SKILL.md` § Self-restart via the keeper — crew
  re-hydration.
- `.claude/skills/captain/SKILL.md` §10 — captain restart continuity + the
  do-not-self-`/quit` rule.
