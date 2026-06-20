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
  itself), crew-restart re-hydration, and the KNOWN drift that the gauge is not
  wired for crews on the live deployment (confirm with `keeper doctor`).
  Load-bearing: must not rot. Composes with crew-launch
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

1. **As a managed session** — you keep working; the keeper owns the reset cycle.
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
| **WARN** | `WarnAbsTokens = 270000` | `--warn-pct 80` (pct-ceil 0.70) | `cycle.go:applyDefaults`, `watcher.go:applyDefaults` |
| **ACT** | `ActAbsTokens = 300000` | `--act-pct 90` (pct-ceil 0.85) | `cycle.go:applyDefaults` |
| **FORCE-ACT** | `ForceActAbsTokens = 340000` (act+40k) | pct 95 (pct-ceil 0.95) | `cycle.go:applyDefaults` |
| window fallback | `FallbackWindowSize = 200000` | — | `watcher.go:applyDefaults`, `--window-size` |

- The **pct gates (`--warn-pct`/`--act-pct`) are only used as a fallback** when
  the gauge does not emit absolute token counts (`CtxFile.Tokens == 0` or
  `WindowSize == 0`) — i.e. older Claude Code versions (`cycle.go:belowActThreshold`,
  `watcher.go:belowWarnThreshold`). When absolute tokens ARE present (all current
  Claude Code versions with [1m] or 200k windows), the abs/pct-ceil `min` formula
  above governs.
- **On [1m]-window models (1M token context) the abs thresholds are
  authoritative**: `min(270k, 0.70×1M)=270k` for warn, `min(300k, 0.85×1M)=300k`
  for act. `--warn-pct`/`--act-pct` have no effect and the keeper will emit a
  warning if they are passed explicitly. Use `--warn-abs-tokens`/`--act-abs-tokens`
  to override thresholds. (Refs: hk-odhh.)
- **FORCE-ACT** is the hard ceiling: above it the cycle fires **unconditionally,
  bypassing the CrispIdle gate**, so a perpetually-busy session that never goes
  idle still gets cleared before exhaustion (`cycle.go:50-57`, Refs: hk-0uu).
- **Abs thresholds are configurable** via CLI flags OR `.harmonik/config.yaml`
  `keeper:` block (see § Project config below). CLI flags win over config.yaml;
  config.yaml wins over compiled defaults. The pct flags (`--warn-pct`, `--act-pct`)
  are a legacy fallback — do NOT pass them on modern deployments (they are inert
  when Claude Code emits absolute token counts). Refs: hk-odhh, hk-lhu2.

### § Project config — .harmonik/config.yaml `keeper:` block

Operators can customise thresholds and warn texts per-project by adding a
`keeper:` section to `.harmonik/config.yaml` (`schema_version: 1`). All fields
are optional; absent or `0`/`""` values defer to the CLI flag or compiled default
(precedence: CLI flag > config.yaml > compiled default).

```yaml
schema_version: 1
keeper:
  context_thresholds:
    warn_abs_tokens: 270000      # absolute warn gate; ≤0 = not configured
    act_abs_tokens: 300000       # absolute act gate; ≤0 = not configured
    force_act_abs_tokens: 340000 # hard unconditional ceiling; ≤0 = not configured (derived as act+40k)
    act_pct_ceil: 0.85           # pct-of-window cap for act gate; ≤0 = not configured
    warn_pct_ceil: 0.70          # pct-of-window cap for warn gate; ≤0 = not configured
  warn_messages:
    default_warn_text: ""        # warn injection for non-captain agents; empty = compiled default
    on_demand_warn_text: ""      # warn injection for captain (restart-now); empty = compiled default
```

The config is loaded once at keeper startup. Restart the keeper to reload.
Refs: `internal/daemon/projectconfig.go`, hk-lhu2.

---

## § Command surface

All keeper verbs are under `harmonik keeper`. Top-level usage:
`keeper_cmd.go:243` (`keeperTopUsage`).

### `harmonik keeper restart-now <agent> [--project DIR]` — captain-initiated on-demand restart

Writes the `.restart-now` marker (`{nonce, requested_at, session_id}`) read from
the captain's current `HANDOFF-captain.md`. On the next watcher tick, the keeper
calls `RunOnDemand`, which bypasses the act-pct idle gate and runs the
handoff → nonce-poll → `/clear` → `/session-resume` cycle immediately.

**The keeper band is UNCHANGED.** The warn and act thresholds are not widened.
`restart-now` bypasses ONLY the act-pct idle-gate (CrispIdle check); all other
safety gates (nonce-confirmed handoff, `.managed`, HoldingDispatch check) remain
intact. The operator HARD-NO on widening the band stands.

**The captain mints the nonce.** On the request path the captain writes
`HANDOFF-captain.md` (including the `<!-- KEEPER:<nonce> -->` comment), then calls
`harmonik keeper restart-now --agent captain`. The keeper reads the nonce from the
handoff; if no nonce is present or the nonce mismatches the one in `.restart-now`,
the cycle is aborted (safety invariant: never `/clear` without a confirmed nonce).

```bash
# Captain procedure (at a clean idle point — no in-flight dispatch):
# 1. Write HANDOFF-captain.md with current state (include the KEEPER nonce comment).
# 2. Trigger the restart-now cycle:
harmonik keeper restart-now --agent captain [--project DIR]
# The keeper's next tick (≤5 s) fires RunOnDemand → /clear → /session-resume.
```

**A restart-now is now VERIFIABLE — don't assume it landed (hk-uldg).** Before
this, the firing agent fired the command, trusted the exit code, and moved on; if
the keeper was dead / watching the wrong pane / couldn't verify the session id, the
restart silently never happened. The keeper now injects a `[KEEPER ACK <nonce>]
received restart` line into the pane **before** the gated `/clear`, and
`restart-now` prints `nonce=rn-<millis>` to stdout. An **external** observer reads
that nonce and runs `harmonik keeper await-ack` (below) to PROVE the keeper
delivered the ACK. It must be external because on a SELF restart the `/clear`
wipes the firing agent's context before it could ever read its own ACK — see
§ Verifying a restart with await-ack.

### `harmonik keeper await-ack --agent <name> --nonce <N> [--kind restart|ping] [--timeout 15s] [--poll 1s] [--project DIR]` — confirm the ACK landed

The AGENT-SIDE half of the handshake. Polls the agent's OWN pane scrollback for
the exact bracket token `[KEEPER ACK <nonce>]` (not the bare nonce — no cross-cycle
false match). **Exit 0** when observed (keeper proven alive); on timeout it
emits a durable `session_keeper_ack_timeout` event to `events.jsonl` and **exits
3** (distinct from the flag-misuse exit 2). The pane-capture is an injectable seam
so the timer/poll/match logic is unit-tested Go, not skill prose.

The binary does **NOT** send comms — the CALLER owns escalation (comms identity
`--from <lane>` is the caller's; baking a hardcoded `--from` in would risk the
"uncommissioned --from captain freezes the fleet" footgun). On exit 3 the caller
must comms-alert the operator and run the investigation steps:

```bash
harmonik comms send --to operator --topic keeper-alert --from <lane> \
  "keeper ACK timeout for <agent> nonce <N> — keeper may be dead/wrong-pane/unverifiable sid; investigating"
```

Default timeouts: 15s (ping) — pass `--timeout 30s` for restart-now (the keeper
does freshness checks + three injects around the ACK). **Exit codes:** `0` ack
observed; `1` argument error; `2` flag misuse (flag-only); `3` ack-timeout
(event emitted).

### § Verifying a restart with await-ack — who runs it (design decision 1)

**Captain watches crews; a restart wrapper watches for self.** The asymmetry:

- **ping** (self-service liveness) — the SAME live agent fires `ping --nonce N`
  then runs `await-ack --kind ping --nonce N`; the ACK lands in its own pane and
  it reads `await-ack`'s exit code. Use a FRESH unique nonce per ping.
- **restart-now (SELF)** — the firing agent is `/clear`-wiped before its ACK
  lands, so it CANNOT wait for its own ACK. An **external** process must run
  `await-ack`. Use the wrapper **`scripts/captain-tools/keeper-restart-verified.sh
  <agent>`**: it fires `restart-now`, parses the printed `nonce=rn-…`, then runs
  `await-ack --kind restart` for the SAME agent and exits non-zero (logging) if the
  ACK never lands. Wire keeper/captain SELF restarts through this wrapper instead
  of bare `restart-now`.
- **restart-now (CREW, captain watching)** — the captain tells the crew to
  restart, fires `restart-now --agent <crew>`, captures the nonce, then runs
  `await-ack --agent <crew> --kind restart` directly. The captain's process is
  external to the crew, so it survives the crew's `/clear`. See the captain skill
  §10 Restart continuity.

> **OUT OF SCOPE (hk-uldg):** the AUTOMATIC keeper cycle (`MaybeRun`/`runCycle` in
> `cycle.go`/`watcher.go`) does NOT yet run `await-ack` on its own restarts — it
> still relies on its internal handoff-nonce poll. Adding ACK verification to the
> automatic cycle is a separate bead (companion hk-vpnp owns that area). The
> verification wired here covers the MANUAL `restart-now` / `ping` paths only.

### `harmonik keeper --agent <name> [flags]` — the watcher (run this to start it)

Starts the watcher loop and blocks until SIGINT/SIGTERM.

Flags (`keeper_cmd.go:59-66`):

| flag | default | meaning |
|---|---|---|
| `--agent <name>` | — (**required**) | identifies the lockfile + `.managed` marker |
| `--tmux <target>` | auto-derived | pane to inject warn/handoff into; auto-resolved from `harmonik-<hash12>-<agent>` if omitted (`keeper_cmd.go:111-116`) |
| `--warn-pct N` | `80` | pct fallback warn gate — **inert on [1m] models**; emits a warning if passed explicitly |
| `--act-pct N` | `90` | pct fallback act gate (`.managed`-gated) — **inert on [1m] models**; emits a warning if passed explicitly |
| `--warn-abs-tokens N` | `270000` | absolute warn gate (authoritative on [1m] models) |
| `--act-abs-tokens N` | `300000` | absolute act gate (authoritative on [1m] models) |
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

| crossing | keeper does | YOU do (crew / default) | YOU do (captain / OnDemandRestart) |
|---|---|---|---|
| **WARN** (≥270k tokens abs / `--warn-pct` fallback) | injects warn text, emits `session_keeper_warn` | **Keep working.** Optionally refresh `HANDOFF-<agent>.md`. | **Keep working.** At the next clean idle point: write `HANDOFF-captain.md` (include the KEEPER nonce), run `harmonik keeper restart-now --agent captain`, keep the turn OPEN, and stop typing. |
| **ACT** (≥300k / `--act-pct`, CrispIdle, no dispatch hold) | runs handoff → nonce-poll → `/clear` → `/session-resume` | **Nothing.** Hold with `keeper set-dispatching` if mid-dispatch. | **Nothing** — same cycle fires if the captain has not already triggered restart-now. |
| **FORCE-ACT** (≥340k / `--act-pct` 95) | runs the cycle **unconditionally** (bypasses CrispIdle) | **Nothing** — the safety net for a never-idle session. | **Nothing** — same safety net; always fires regardless of restart-now status. |
| **captain restart-now** | `RunOnDemand`: bypasses CrispIdle gate, runs cycle immediately on next tick | — | Captain writes handoff + nonce, then calls `harmonik keeper restart-now --agent captain`. |
| **operator attached** | act-path goes **warn-only**: destructive injection suppressed so keeper never races human keystrokes; warn/gauge continue; cycle resumes once operator detaches | nothing (`cycle.go:128-137`, hk-6qf) | nothing |

**The keeper band is UNCHANGED.** `restart-now` bypasses only the act-pct idle gate;
it does NOT widen warn or act thresholds. All other safety gates (nonce-confirmed
handoff, `.managed`, `HoldingDispatch`) remain intact.

---

## § On a keeper warn — crew vs captain

### Crews (default advisory warn text)

On a keeper context-warning:

1. Refresh your `HANDOFF-<agent>.md` (so the eventual reset carries good state).
2. **Keep working.** Let the keeper cycle you when it crosses ACT.

### Captain (OnDemandRestart warn text)

The captain's warn injection says: *"[KEEPER WARNING — automated] Proactive context checkpoint — you have ample buffer remaining. Keep working. At a clean checkpoint only: write HANDOFF-captain.md (include the KEEPER nonce), then run: harmonik keeper restart-now --agent captain, keep the turn open, and stop typing. The keeper drives the clear→resume cycle."*

At a **clean idle point** (no `.dispatching` in flight, not mid crew-spawn/merge/submit):
1. Finish the current logical unit of work.
2. Write `HANDOFF-captain.md` with a fresh KEEPER nonce.
3. Run `harmonik keeper restart-now --agent captain`.
4. Keep the turn OPEN, stop typing — the keeper fires the cycle on its next tick (≤5 s).
5. **NEVER exit or terminate your own session on a warn.** The keeper owns the clear→resume cycle; self-terminating
   exits the captain permanently (no supervised respawn path today).

Handoff carries INTENT only — `STARTUP.md` re-drains comms and re-grounds via live
state on resume. Do not snapshot live queue/daemon state in the handoff body.

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

# Captain-initiated restart (write HANDOFF-captain.md first, include KEEPER nonce)
harmonik keeper restart-now --agent captain [--project DIR]

# Confirm a restart actually landed (external watcher; survives the agent's /clear).
# For a SELF restart use the wrapper — it fires restart-now, parses nonce, awaits ACK:
scripts/captain-tools/keeper-restart-verified.sh captain [--project DIR]
# For a CREW restart the captain runs await-ack directly after restart-now:
harmonik keeper await-ack --agent <crew> --nonce rn-<millis> --kind restart --timeout 30s

# Self-service liveness check (live agent — fresh nonce each time):
harmonik keeper ping --agent <self> --nonce ping-$(date +%s%3N)
harmonik keeper await-ack --agent <self> --nonce ping-<same> --kind ping --timeout 15s
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
- `internal/keeper/thresholds.go` — the single source of truth for the threshold
  defaults (270k warn/300k act/340k force-act) and the `min(abs, pct*window)`
  formula, shared by both watcher and cycler.
- `internal/keeper/cycle.go` — CrispIdle / force-act / operator-attached gating
  and the reset-cycle state machine.
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
  do-not-self-terminate rule.
