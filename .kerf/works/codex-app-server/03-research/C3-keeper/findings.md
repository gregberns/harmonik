# C3 — Keeper / Compaction Machinery: substrate-coupling analysis

Codename: codex-app-server. Question for C3: how much of the keeper exists ONLY
because a Claude CLI client holds a bounded, growing context window that must be
handed-off and cleared before the pane overflows — vs. what serves a
substrate-neutral purpose (liveness, presence, daemon coordination).

Sources read (with cites below): `.claude/skills/keeper/SKILL.md`,
`internal/keeper/thresholds.go`, `docs/design/agent-wake-mechanism.md`,
`.claude/skills/crew-launch/SKILL.md` §Self-restart, `.claude/skills/captain/SKILL.md` §10,
`harmonik keeper --help`.

---

## 1. What the keeper IS, mechanically

The **keeper** is harmonik's per-session **context-fill watcher**. One keeper
process supervises exactly one long-lived Claude session (captain, crew,
flywheel, orchestrator). It reads how full that session's context window is each
turn and, before the window overflows (a full pane stops accepting keystrokes),
performs an **intent-preserving reset cycle** (SKILL.md:32-38).

### The gauge + watcher (decoupled)
- **Gauge writer:** the Claude Code **statusLine hook** (`keeper-statusline.sh`)
  writes `.harmonik/keeper/<agent>.ctx` on *every* Claude Code render with
  `{pct, tokens, session_id}` (SKILL.md:51-53, 451-453). It runs regardless of
  whether any watcher exists — a fresh gauge file does NOT imply a live keeper.
- **Watcher loop:** `harmonik keeper --agent <name>` polls that gauge every ~5s
  (`DefaultPollInterval = 5s`, thresholds.go:127; SKILL.md:281). It is
  `.managed`-gated: absent `.harmonik/keeper/<agent>.managed` → logs a no-op and
  exits 0 (passive mode, no cycle ever fires) (SKILL.md:66-69).

### The two thresholds — REAL default values (thresholds.go:23-52, 95)
The effective gate is `min(absTokens, pctCeil × windowSize)` — the single formula
in `minAbsOrPctCeil` (thresholds.go:214-222). On a 1M ([1m]) window the abs
values win; on a 200k window the pct-ceil caps fire first. Pct flags are a
FALLBACK only used when the gauge emits no absolute token count (SKILL.md:106-116).

| gate | abs-token default | pct-ceil | const |
|---|---|---|---|
| **WARN** | **200,000** | 0.70 | `defaultWarnAbsTokens` (:35) |
| **ACT** | **215,000** | 0.85 | `defaultActAbsTokens` (:36) |
| **FORCE-ACT** | **240,000** (act+25k) | 0.95 | `defaultForceActAbsOffset` (:39) |
| **HARD-CEILING** | **280,000** (SID-independent) | — | `DefaultHardCeilingTokens` (:95) |
| window fallback | 200,000 | — | `defaultFallbackWindowSize` (:51) |

Pct fallbacks: `defaultWarnPct=80`, `defaultActPct=90` (:26-27).
NOTE the SKILL header stresses these are **operator-required** at the CLI/config
layer — `ResolveKeeperConfig` imposes NO runtime fallback and the keeper REFUSES
TO START on any unset key; the numbers above are the library `applyDefaults`
values that `keeper config --example` ships as suggestions (SKILL.md:75-96).

- **WARN**: session filling. Injects a wrap-up-warning prompt into the tmux pane,
  emits `session_keeper_warn`. Informational — the agent keeps working
  (SKILL.md:56-58).
- **ACT**: near-full AND idle (CrispIdle) AND not holding a dispatch → runs the
  reset cycle (SKILL.md:59-64).
- **FORCE-ACT** (240k): fires the cycle **unconditionally, bypassing CrispIdle**,
  so a never-idle session still gets cleared (SKILL.md:117-119).
- **HARD-CEILING** (280k): a SEPARATE, SID-independent trip-wire — forces
  handoff+restart even if the session_id binding is wrong, so a mis-bound keeper
  cannot silently let a pane overflow (thresholds.go:79-98; SKILL.md:120-124).

### The reset cycle: handoff → /clear → /session-resume
`Cycler.MaybeRun`: truncate handoff → inject `/session-handoff` → **poll for the
handoff nonce** → ONLY THEN `/clear` → `/session-resume <agent>` (same
session_id). Safety invariant: **never `/clear` without a confirmed handoff
nonce** (SKILL.md:59-64, 433-434). Timings: `DefaultHandoffTimeout=300s`,
`DefaultClearSettle=10s`, `DefaultClearConfirmBackstop=150s` (thresholds.go:157-174).

### Restart re-hydration
`/clear` + `/session-resume` re-mints the session_id and re-runs the agent's full
boot sequence with context cleared but identity + durable state intact
(SKILL.md:433-445). In-flight queue work is NOT lost: the named queue keeps
draining on the **daemon** independent of the session; `{queue, epic_id}` are
durable in beads. On resume the crew re-reads its handoff frontmatter + the
`br show <epic_id> --assignee` mirror, re-joins comms with a fresh dedupe set, and
re-processes its inbox idempotently (SKILL.md:436-445; crew-launch:467-501).
For the captain a keeper restart is a "NON-EVENT" — a transient presence drop is
not a crew failure (captain:677-726).

### The hold/release co-working override
`keeper hold` / `release`: suspends ONLY the ACT/restart cutoff while a human is
co-working, so the keeper doesn't `/clear` out from under a live collaboration.
**WARN still fires** (SKILL.md:357-384). Marker `.hold.<sessionID>` is keyed by
the live session-id (dies on any `/clear` — cannot leak past its window), with a
45m timer backstop (`DefaultHoldTTL`, thresholds.go:193) and a carve-out: the
**hard-ceiling restart overrides a hold** (overflow protection wins).

### Dispatch-hold (distinct from co-working hold)
`set-dispatching` / `clear-dispatching` write/remove `.dispatching` so the reset
cycle defers while a queue batch is in flight (SKILL.md:340-355).

### Verification handshake (restart-now / ping / await-ack)
`restart-now` injects `[KEEPER ACK <nonce>]` before the gated `/clear`;
`await-ack` polls the pane for that exact bracket token (exit 3 +
`session_keeper_ack_timeout` on timeout). SELF restart is synchronous/self-verifying
in-process; a CREW restart is verified by the external captain (SKILL.md:198-259).

### Related but separate: the wake-watcher (`hk-wake.sh`)
NOT part of the keeper, but the same substrate coupling. A parked interactive
Claude pane at the `❯` prompt does not poll comms, so an out-of-process bash
poller drains comms and `tmux send-keys`-injects messages into the pane only when
it is idle-at-prompt (agent-wake-mechanism.md:42-77). It exists because the
client is an interactive terminal pane with no server-side inbox delivery.

---

## 2. Component-by-component: WHY each piece exists

Legend — **CLIENT-WINDOW** = exists ONLY because the Claude CLI holds a bounded,
growing, client-side context window that must be handed-off + cleared before a
tmux pane overflows. **SUBSTRATE-NEUTRAL** = serves liveness/presence/coordination
that any substrate needs. **MIXED** = both.

| Component | Cite | Why it exists | Class |
|---|---|---|---|
| statusLine gauge writer (`.ctx` pct/tokens) | SKILL.md:51-53 | Measure how full the *client's* growing window is | **CLIENT-WINDOW** |
| WARN threshold (200k) | thresholds.go:35 | Warn before the window fills | **CLIENT-WINDOW** |
| ACT threshold (215k) + reset cycle | thresholds.go:36; SKILL.md:59-64 | Clear before overflow | **CLIENT-WINDOW** |
| FORCE-ACT (240k, bypass idle) | thresholds.go:39 | Clear a never-idle full window | **CLIENT-WINDOW** |
| HARD-CEILING (280k, SID-independent) | thresholds.go:95 | Backstop against pane overflow even when keeper mis-bound | **CLIENT-WINDOW** |
| `min(abs, pctCeil×window)` formula | thresholds.go:214 | Make one band work across 200k vs 1M *client windows* | **CLIENT-WINDOW** |
| handoff → nonce-poll → /clear → /session-resume | SKILL.md:59-64 | The `/clear` is a client-context wipe; the handoff preserves intent across it | **CLIENT-WINDOW** |
| handoff-nonce safety invariant | SKILL.md:433-434 | Never wipe client context without a saved intent | **CLIENT-WINDOW** |
| `.managed` gate | SKILL.md:66-69 | Guards the destructive `/clear` | **CLIENT-WINDOW** |
| CrispIdle gate | SKILL.md:59; cycle.go | Don't `/clear` mid-turn / race keystrokes | **CLIENT-WINDOW** |
| operator-attached → warn-only | SKILL.md:397 | Don't race a human typing into the pane | **CLIENT-WINDOW** (tmux-pane specific) |
| boot-grace (5m young-session guard) | thresholds.go:111 | Don't re-clear a just-resumed session | **CLIENT-WINDOW** |
| hold / release co-working override | SKILL.md:357-384 | Suspend the `/clear` cutoff during live co-work | **CLIENT-WINDOW** |
| set/clear-dispatching | SKILL.md:340-355 | Defer the `/clear` while a queue batch is in flight | **MIXED** (coordinates daemon work vs a client-context wipe) |
| restart re-hydration (re-boot on resume) | SKILL.md:436-445 | Re-establish identity/comms/assignee AFTER a context wipe | **MIXED** (re-hydration is forced by the wipe; join/subscribe/assignee-mirror are substrate-neutral) |
| restart-now / ping / await-ack ACK handshake | SKILL.md:198-259 | Prove the *keeper* delivered a restart into a pane; prove a watcher is alive | **MIXED** (restart-verify is client-window; liveness-ping is substrate-neutral) |
| `session_keeper_no_gauge` / blind-keeper alarm | SKILL.md:283-284; thresholds.go:149 | Detect a missing statusLine hook / dead gauge | **MIXED** (leans client-window: the gauge only exists to measure the window) |
| `keeper doctor` (`live-watcher`, hooks, gauge, api-key) | SKILL.md:319-338 | Validate the watcher + hook wiring is armed | **CLIENT-WINDOW** (validates client-window machinery) |
| single-keeper lockfile (`LiveKeeperPresent`) | SKILL.md:456-458 | One watcher per agent | **SUBSTRATE-NEUTRAL** (process-liveness) |
| `--respawn-cmd` supervised respawn | SKILL.md:276 | Relaunch a dead agent pane | **SUBSTRATE-NEUTRAL** (agent liveness) |
| wake-watcher `hk-wake.sh` (comms→pane inject) | agent-wake-mechanism.md:42-77 | Deliver comms to a parked interactive pane that doesn't poll | **MIXED** (comms delivery is neutral; the *tmux-inject* mechanism is client/terminal-specific) |

---

## 3. KEY analysis — if a substrate managed conversation context server-side

Premise: the substrate (e.g. a Codex/OpenAI *app-server* style host) manages
conversation context **server-side** — automatic compaction / summarization /
unbounded effective window — so there is **no client-side growing window to
overflow** and **no `/clear` to drive**.

### BECOMES UNNECESSARY (delete)
- **The entire threshold band** — WARN/ACT/FORCE-ACT/HARD-CEILING (thresholds.go:35-98).
  With no client window to overflow, there is no "near-full" to gate on. The
  `min(abs, pctCeil×window)` formula, the pct fallbacks, boot-grace, force-act
  bypass — all gone.
- **The statusLine gauge writer** (`.ctx` pct/tokens). Nothing to measure.
- **The reset cycle** handoff→nonce-poll→`/clear`→`/session-resume` and its
  **handoff-nonce safety invariant**. There is no context wipe, so no intent to
  preserve across one. This is the single largest deletion — most of `cycle.go`.
- **`.managed` gate, CrispIdle gate, operator-attached warn-only** — all guard the
  destructive `/clear`. No `/clear` → no guard.
- **hold / release co-working override** (SKILL.md:357-384) — its whole reason is
  "don't `/clear` out from under a live human." No cutoff to suspend.
- **restart-now / restart-verify half of the ACK handshake** — restart-now exists
  to drive a `/clear`→`/session-resume` into a pane and prove it landed. No restart.
- **`session_keeper_no_gauge` / blind-keeper alarm** — the gauge it watches for
  is gone.
- **`keeper doctor`'s statusLine / PreCompact / gauge / idle-marker / managed
  checks** (SKILL.md:329-334) — they validate hook wiring that no longer exists.

### RESHAPES (survives in altered form)
- **Restart re-hydration → re-connection.** The re-boot is currently *forced* by
  the context wipe. Under server-side context there is no forced wipe, but a
  session can still drop/reconnect (crash, redeploy, network). What survives is
  the **re-hydration payload**: re-join comms with a fresh dedupe set, re-mirror
  `br show <epic_id> --assignee`, re-subscribe the queue (crew-launch:477-501).
  That logic RESHAPES from "run on every keeper `/clear`" to "run on
  reconnect." The `session_id` re-mint semantics change (server may keep a stable
  conversation id).
- **set/clear-dispatching → pure daemon-coordination marker.** Today it defers a
  `/clear`. With no `/clear`, its "don't interrupt me mid-dispatch" intent could
  still be a useful signal if ANY session-lifecycle action (redeploy, migration)
  needs to defer around in-flight queue work — but it loses its keeper coupling
  and likely folds into general lifecycle logic.
- **The wake-watcher (`hk-wake.sh`).** The *need* — deliver an inbound comms
  message to an idle agent — is substrate-neutral and REMAINS. The *mechanism*
  (poll comms + `tmux send-keys` into an idle-at-prompt pane) is entirely
  client/terminal-specific and RESHAPES: a server-hosted app-server could own
  "deliver to an idle agent" natively via its own turn/event API (the doc's own
  "Future work: a comms-native idle/deliver hook", agent-wake-mechanism.md:246-260),
  deleting the pending/seen bookkeeping and the idle-gate heuristics.
- **ACK handshake → liveness ping only.** The `ping`/`await-ack` liveness half
  (prove a watcher/session is alive) survives; the restart-verify half dies.

### REMAINS (substrate-neutral, unchanged in purpose)
- **Process liveness / single-keeper lockfile** (`LiveKeeperPresent`,
  SKILL.md:456-458) — "is exactly one supervisor alive for this agent" is needed
  regardless of context management. May relocate (into the daemon), but the
  concern remains.
- **`--respawn-cmd` supervised respawn** (SKILL.md:276) — relaunch a dead agent.
  Agents still crash under any substrate.
- **Comms presence / `comms who` TTL** (referenced captain:611) — presence
  freshness is independent of context.
- **The `br show --assignee` durable mirror + idempotent inbox re-processing** —
  durable-state coordination that survives any reconnect, not tied to `/clear`.

**Net:** roughly the entire `internal/keeper` band + cycle + gauge + hold + doctor
hook-checks (call it ~70-80% of the keeper's mass) exists ONLY because of the
bounded client-side window. What remains after that deletion is a much smaller
**agent-liveness + reconnect-rehydration + comms-delivery** supervisor — and even
those pieces would likely migrate server-side under an app-server substrate.

---

## 4. What liveness/presence still needs to happen regardless of context

Even with fully server-side context, these must exist somewhere:

1. **Session liveness** — detect that an agent process/session died and respawn
   it (`--respawn-cmd`, single-keeper lock). Substrate-neutral.
2. **Presence freshness** — an agent must keep asserting it is alive so peers
   (captain, watch) don't treat it as failed; `comms who` ~120s TTL
   (captain:611). Independent of context.
3. **Reconnect re-hydration** — on ANY session drop/reconnect, re-join comms with
   a fresh dedupe set, re-subscribe the named queue, re-mirror the epic assignee
   (crew-launch:477-501). Forced-wipe is gone, but reconnect is not.
4. **Idle-inbound delivery** — a message addressed to an idle agent must reach it
   (today: wake-watcher; ideally: substrate-native delivery). The *concern* is
   neutral even though today's *mechanism* is tmux-specific.
5. **Liveness verification handshake** — `ping`/`await-ack` (the liveness half,
   not the restart half) to prove a watcher/session actually responds.

OPEN QUESTIONS:
- Does the codex app-server actually compact/manage context server-side with an
  effectively unbounded window, or does it still surface a token budget the
  client must respect? If a budget is still exposed, some form of the WARN/ACT
  band survives (RESHAPED to read the server's budget instead of a local gauge)
  rather than being deleted. Not answerable from harmonik source — needs the
  codex-app-server component spec.
- Whether idle-inbound delivery (#4) can be moved server-side depends on whether
  the app-server exposes a "push a turn to a session" API. Unknown here.
- I read `thresholds.go`, the SKILL, and the wake-mechanism doc directly but did
  NOT read `cycle.go` / `watcher.go` line-by-line — component→class assignments
  for CrispIdle and operator-attached are from SKILL.md cites (:59, :397), not
  the Go bodies. Low risk; the SKILL is kept in sync per its docdrift test.
