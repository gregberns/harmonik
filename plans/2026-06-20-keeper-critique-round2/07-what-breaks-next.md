# 07 — What Breaks Next (Failure-Prediction Critic)

**Lens:** Given everything that shipped (restart-now direct+ACK-line, Bug3 loop/truncation
fix, `await-ack` primitive, band pinned 200k/215k, daemon+captain-keeper redeployed),
predict the NEXT production incident for a long-running captain/crew. Adversarial: find the
incident before it happens.

**Method:** verified against live runtime state on 2026-06-20 ~16:47Z (not memory):
process tree, tmux sessions, `.harmonik/keeper/*`, `events.jsonl`, and the shipped code in
`internal/keeper/{cycle,restartnow,awaitack}.go` + `cmd/harmonik/keeper_cmd.go` +
`scripts/ctx-watchdog-launch.sh`.

---

## Live ground truth (what is actually running right now)

| Fact | Evidence |
|---|---|
| Captain keeper PID 33207, **no supervisor parent** | `ps`: parent is `zsh -c … \| tee`, grandparent is the `tmux new-session` that launched the captain. Nothing re-spawns the keeper if it dies. |
| Two keeper tmux sessions exist (`hk-keeper-captain` 09:27, `hk-a3dc45482890-keeper` 09:25) | `tmux ls` — a possible duplicate/zombie keeper from the redeploy. |
| Captain has already `/clear`-cycled since launch | launch argv minted `--session-id fe5efd0e…`; live `.ctx`/`.sid`/`.managed` all = `1274a140…`. Identity overlay tracked it correctly — **this path WORKS.** |
| **Zero crew keeper processes** | `ps … grep crew` → 0. 5 live crews (chani/irulan/jamis/logmine/paul), none keeper-watched. |
| Crew/flywheel safety net = `ctx-watchdog`, a single **Sonnet LLM `/loop` session** | `ps`: PID 25982; `scripts/ctx-watchdog-launch.sh` + prompt. |
| `session_keeper_ack_timeout` emitted **2×, both from a smoke test** (`agent:keeper-smoke-live`, `reason:no_tmux_target`), **never from a real restart** | `events.jsonl`. |
| **Nothing consumes `ack_timeout`** | grep across `internal/ cmd/ scripts/ .claude/`: only the emitter (`awaitack.go`) + payload/eventtype defs. ctx-watchdog prompt does not mention it. |
| `await-ack` / `AwaitAck` / `AckLine` **not referenced by the auto cycle** (`cycle.go`/`watcher.go`) | grep: 0 hits. The primitive is islanded. |
| restart-now CLI **prints** the nonce but **does not AwaitAck** | `keeper_cmd.go` ~445: `RestartNow(...)` then `fmt.Printf(... nonce ...)`. Open loop even on the manual path. |
| `no_gauge:stale` still the dominant event (165 in the recent window) | `events.jsonl` tail — C2 (gauge-dies-on-live-pane) is **not fixed**, still firing. |
| Leftover `captain.ctx.tmp.*` write-race artifacts present | `.harmonik/keeper/` (2 zero-byte `.tmp` files dated Jun19/Jun16). |

The throughline of round-1 — *infer-and-act-blindly, no liveness alarm* — is **still the live
shape.** The shipped work added a verifiable handshake **as an unused library**; production
still runs open-loop.

---

## Predicted failures, ranked by likelihood × blast-radius

### P1 — Captain keeper dies and stays dead; captain then silently overflows overnight
**Likelihood: HIGH · Blast radius: FLEET-WIDE (the captain is the only keeper-protected agent).**

The captain keeper (PID 33207) is a bare process with **no supervisor loop** — its parent is
a `tee` pipe, not `harmonik supervise` and not a `while`-loop launcher (`captain-launch.sh`
fires it once; `grep -nE 'while|until|loop'` over that script finds none around the keeper
launch). The known-failure catalog already records this class twice:

- **C10** — keeper process went `^Z`-SUSPENDED on 2026-06-20 and silently stopped watching;
  the daemon supervisor does **not** revive keepers by design (hk-34ac OPEN).
- The captain "quit-and-stays-dead if launched without `--session-id`" memory
  (`reference_captain_keeper_restart_gap`).

So the realistic overnight sequence: keeper crashes / is `^Z`-stopped / the host reboots →
**nothing restarts it** → captain keeps running, crosses 200k/215k → the gauge keeps writing
`.ctx` from the statusline hook but no watcher reads it → captain pane overflows and stops
accepting keystrokes → fleet loses its orchestrator with **no alarm**. The `ctx-watchdog`
prompt **explicitly SKIPs the captain** ("its own keeper owns its restart") — so the one
safety net deliberately ignores exactly the agent whose keeper just died. Single point of
silent failure, fleet blast radius.

Worsened by the **duplicate keeper tmux session** (`hk-a3dc45482890-keeper` @09:25 +
`hk-keeper-captain` @09:27): if two keepers ever bind the same pane they race injections —
the redeploy may already have left a latent double-driver.

### P2 — `ack_timeout` is an alarm nobody hears; a missed paste during the auto cycle goes undetected
**Likelihood: HIGH · Blast radius: per-agent → fleet (captain) — and it fails SILENTLY.**

Two compounding gaps:

1. **The auto cycle never uses the handshake.** `runCycle` (`cycle.go:752`) still injects
   `/session-handoff` → polls a handoff-file nonce → `/clear` → `/session-resume`
   **open-loop**: `/clear` and `/session-resume` success is *inferred from a later gauge
   dip*, never *confirmed* (`AwaitAck`/`AckLine` absent from the file). When the captain
   crosses 215k unattended and a paste doesn't land (busy pane, repaint race, the 10.5%-
   covered `InjectText` path), the cycle records `clear_unconfirmed` (already 11× in the live
   log) and re-arms on gauge noise — the exact C4 loop shape Bug-3 narrowed but did not
   close, because closure requires the read-back that lives only in the unused primitive.

2. **Even the manual path's ACK is write-only.** restart-now injects `[KEEPER ACK <nonce>]`
   and the CLI prints the nonce, but **no caller runs `await-ack`** — not the CLI, not the
   captain/crew skills (`grep -rln await-ack .claude/skills scripts` → empty). And when
   `await-ack` *does* time out, it emits `session_keeper_ack_timeout` into a void: **zero
   consumers** (verified). The only two such events ever emitted came from a smoke test. An
   unconsumed alarm is no alarm. So the failure mode is the worst kind: the system *has* the
   evidence that a restart silently failed, and **throws it on the floor.**

Net: the unattended-overnight overflow that the keeper exists to prevent will fail the same
way it always has (inferred-success, silent), and the one new mechanism that could catch it
is not wired into either the automatic OR the manual restart, and its failure event is dead-
lettered.

### P3 — A crew overflows and nobody is watching the watcher
**Likelihood: MEDIUM-HIGH · Blast radius: one crew's lane (recoverable, but invisible until a lane stalls).**

Crews have **no keeper at all** (0 crew keeper processes; C11 "crew gauge not wired"
unchanged). Their only protection is `ctx-watchdog` — a **single Sonnet LLM session** that
(a) depends on the *same* `.ctx` gauge that emits 165 `no_gauge:stale` events and falls back
to `capture-pane` heuristics, (b) has **its own context budget** and self-heals only via a
step-6 self-check inside its own `/loop` (if it overflows mid-tick before the self-check, it
goes dark with no external supervisor — same no-supervisor gap as P1, one level out), and
(c) is itself just another paste-driven session subject to every paste/repaint race. A crew
that overflows between watchdog ticks (30-min cadence) sits dead until the next tick; if the
watchdog itself is wedged, the crew sits dead indefinitely. Discovery today is "a lane
stopped making progress," not an alarm.

### P4 — Gauge-stale (C2) makes the watchdog's own input lie
**Likelihood: MEDIUM · Blast radius: false-negative restarts (oversize agent looks fine).**

165 `no_gauge:stale` in the recent window confirms the gauge still dies on live panes. The
watchdog reads `tokens` from `.ctx`; a stale gauge under-reports (round-1 saw the captain at
313k while the gauge showed 20-27%). The watchdog's `capture-pane` fallback only triggers
when `.ctx` is **>5min stale** — but a gauge that is *fresh-but-wrong* (recently written,
under-reporting) passes the freshness check and the agent is never flagged. So C2 degrades
both the keeper AND the watchdog from the same root.

### P5 (latent) — duplicate keeper / `.tmp` write-race residue
Two keeper tmux sessions + 2 leftover `captain.ctx.tmp.*` files suggest the atomic-write +
single-keeper invariants are not being enforced at deploy time. Low blast radius today but a
known precursor to identity flap (C1) and double-injection.

---

## Top 2 + smallest preventive fix

### Fix for P1 (keeper dies & stays dead) — **a liveness backstop, not a bigger keeper**
Smallest fix: **wrap the keeper launch in a supervised restart loop** and add a blind-keeper
alarm. Two cheap forms, either acceptable:
- **(a)** Change `captain-launch.sh` (and the crew launch) to start the keeper under a
  `while true; do harmonik keeper …; sleep 5; done` shim in its tmux window — so a crash/`^Z`
  exit auto-revives (mirrors `ctx-watchdog-respawn.sh`'s self-heal pattern). ~3 lines.
- **(b)** Add ONE line to the `ctx-watchdog` prompt: *also* check that each agent's keeper
  PID/tmux session is alive (`pgrep -f "keeper --agent <name>"`), and if a managed agent has
  no live keeper, `comms send --to operator` an alarm + relaunch its launch script. This is
  hk-34ac's "blind-keeper alarm" realized in the net we already run, and it removes the
  "watchdog deliberately SKIPs the captain" blind spot.
This is the highest-value fix: it converts the #1 silent-fleet-loss path into a self-healing
or at-least-alarming one for the cost of a loop wrapper.

### Fix for P2 (dead-lettered ack_timeout) — **consume the alarm**
Smallest fix: **add `session_keeper_ack_timeout` to the ctx-watchdog's read set** — one
clause in the prompt: each tick, `grep` the tail of `events.jsonl` for
`session_keeper_ack_timeout` newer than the last tick and, for any hit, `comms send --from
ctx-watchdog --to operator --topic watchdog -- "ACK-TIMEOUT <agent> <reason> — restart may
have silently failed; investigate"`. Zero code change; turns the existing-but-ignored signal
into an actual page. (The deeper fix — wiring `await-ack` into the restart-now skill and the
auto cycle so the signal is actually *generated* on real restarts — is the right follow-on,
but consuming the event is the smallest step that stops the floor-drop.)

---

## One-line verdict
The shipped work built a verifiable handshake and then left it on the shelf: production still
restarts open-loop, the captain keeper has no supervisor, crews have no keeper, and the one
new alarm has no consumer — so the **next incident is an unattended overnight captain
overflow with no alarm**, exactly the failure the keeper exists to prevent. The two cheapest
preventions (supervise/alarm the keeper; consume `ack_timeout` in the watchdog) are both
prompt-or-shim-sized and worth doing now.
