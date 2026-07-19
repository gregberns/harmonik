# Plan: Keeper restart timing — graceful handoff instead of interrupt

## Status

research-phase (problem-framing only — NOT a solution)

## Objective

Rethink *when and how* the keeper triggers a session handoff/restart so it stops
interrupting agents (especially the admiral/captain mid-conversation with the
operator) at the worst possible moment, and instead lands the restart at a natural
pause.

> **Scope guard (operator directive, 2026-07-17):** This is groundwork. Do NOT
> design or build a fix yet. Hone the problem statement, capture how the keeper
> works today, and brainstorm the option space. A follow-on plan turns the chosen
> direction into a kerf work.

---

## The core problem

The keeper works — it does its job. When a session fills, a message is injected,
the agent stops what it's doing, writes a handoff, and that triggers the clear.

But **"the agent stops everything it is doing" is the problem.** By construction
the keeper fires when context is *rising* — i.e. almost always while the agent is
mid-task. Two failure modes follow:

1. **Lost task detail.** The agent abandons an in-flight task to write a handoff.
   The fine-grained working state doesn't always survive the handoff → the resumed
   session picks up a lossy summary, not where it actually was.
2. **Lost operator input.** Most acute with admiral/captain, whom the operator
   talks to constantly. The operator is mid-way through typing a long message; the
   keeper injects its handoff line *after* the operator's partial text; it all goes
   in together and the session restarts. Neither the agent nor the operator
   remembers what was being written. Time is lost, conversation is disrupted.

The restart is *correct* — it has to happen. The **timing and the framing** are
what's wrong.

---

## How the keeper works today (grounded map)

Verified against the code 2026-07-17. Sources in `internal/keeper/` and
`cmd/harmonik/keeper_*`.

### Gauge → watcher (decoupled)
- The `keeper-statusline.sh` hook writes `.harmonik/keeper/<agent>.ctx` (JSON:
  `pct`, absolute `tokens`, `window_size`, `session_id`) on **every** Claude Code
  render. Context is gauged by the **absolute token count Claude Code itself
  reports**, not by the keeper counting.
- A watcher (`internal/keeper/watcher.go`, `Watcher.Run`) polls that gauge every
  **5s** (`DefaultPollInterval`). Gauge and watcher are fully decoupled — a fresh
  gauge file does NOT mean a watcher is alive (`keeper doctor --agent` +
  `live-watcher` is the real liveness check).

### The threshold band (`internal/keeper/thresholds.go`)
Effective gate = `min(absTokens, pctCeil × windowSize)`. Absolute band (TA1
retune, hk-8hr1):

| Gate | Abs default | Behaviour |
|---|---|---|
| **WARN** | 200k | Inject warn text, emit `session_keeper_warn`. **Informational — agent keeps working.** |
| **ACT** | 215k | Run the reset cycle — but only if **CrispIdle** AND not **HoldingDispatch**. |
| **FORCE-ACT** | 240k (act+25k) | Run the cycle **unconditionally** — bypasses CrispIdle (safety net for a never-idle session). |
| **HARD-CEILING** | 280k | SID-independent trip-wire; fires even if session-id binding is wrong; **overrides a hold**. |

So there already **are** the operator's three conceptual tiers — the "soft"
signal (WARN), the "must restart" (ACT/FORCE-ACT), and the "hard clear"
(HARD-CEILING). The intent exists; the operator's read is that the *implementation*
of that layering never worked well in practice. That judgement is the thing to
re-examine, not re-invent from zero.

### What "fires" means — the delivery path
Delivery is a **tmux paste-then-Enter into the agent's pane**
(`internal/keeper/injector.go`, `injectTextClocked`) — NOT a comms message, NOT a
session API, and **NOT** a bracketed paste (verified: no path uses `paste-buffer
-p`). The exact sequence is `load-buffer -b hk-keeper-inject` → `paste-buffer -d`
→ **sleep 750ms** → `send-keys Enter` → **2 retry Enters (400ms apart)**
(injector.go:154-181; `submitSettle`/`submitRetries`/`submitRetryDelay` at :84-95).
It does NOT clear the operator's current input line first (no `C-u`/`C-c`;
`SendEscapeKey` exists but is called only by the *restart cycle* before
`/session-handoff`, not on the warn-inject path). This is why a partially-typed
operator message gets swept in: the paste lands at the cursor in the same input
buffer the operator is typing into, then **three Enter keypresses over ~1.55s**
submit whatever is on the line during that window.

Notably: `harmonik comms` wake (`commsInjectTmuxPane`, `cmd/harmonik/comms.go:451-471`)
and the daemon brief inject (`internal/daemon/pasteinject.go`) use the **same tmux
paste primitive** via three *separate* code paths with distinct buffer names. There
is no single shared injection function.

**Q2 ANSWERED (code diagnosis 2026-07-17):** the three sequences are nearly
identical — **all three send a trailing Enter**, none use bracketed paste, none
clear the line first. So the working hypothesis "comms pastes without submitting"
is **REFUTED by the code**. The ONLY mechanical difference: comms fires **exactly
one Enter immediately** (comms.go:465); the keeper adds the **750ms settle + two
retry Enters**. That multi-Enter window is the plausible disruptiveness driver —
the injected line sits in the operator's prompt for 750ms inviting a keystroke, and
the two later Enters submit whatever gets typed. **Caveat:** the code does NOT
support "comms *never* sweeps in operator input" — both `paste-buffer -d` at the
cursor without clearing, so any real difference likely comes from Claude Code's
TUI paste-queuing while busy, or the operator simply not using `--wake` — **not**
from the tmux sequences. **Load-bearing constraint for any future fix:** the
settle + retry-Enter loop is itself a reliability fix (hk-89g / hk-ip33d /
hk-7rgqs) — a lone immediate Enter was intermittently *dropped*, leaving the
injected command unsubmitted. Dropping it is ~3 lines but reverses that fix.

### The reset cycle (once ACT gate passes)
`Cycler.MaybeRun` → pure reactor (`step.go`) → shell driver (`shell.go`):
1. Inject `/session-handoff <path>` + "include this line verbatim:
   `<!-- KEEPER:<cycleID> -->`". The **cycleID/nonce** is `cyc-<timestamp>-<seq>`,
   minted by the keeper (`newCycleIDGen`). Manual `restart-now`: the captain mints
   the nonce into `HANDOFF-captain.md`.
2. **Poll the handoff file** until it contains that exact nonce (timeout 300s).
3. Await model-done (Stop-hook signal, 60s).
4. Inject `/clear` — **structurally reachable only after a nonce-confirmed
   handoff** (invariant SK-INV-001: never clear without a confirmed handoff).
5. Await a new session-id, then inject `harmonik agent brief --wake keeper-restart`
   to re-boot the same session.

### Existing gates / overrides
- **CrispIdle** (`.idle` marker from the Stop hook) — ACT waits for the agent to be
  idle. This is the closest existing thing to "wait for a good stopping point," but
  it's binary (turn-boundary), not "task-complete."
- **set-dispatching / clear-dispatching** — defers the cycle while a queue batch is
  in flight.
- **hold / release** — operator co-working override; suspends ACT (WARN still
  fires); session-id-keyed so it auto-reverts on restart; 45m TTL backstop.
- **operator-attached** — when the operator is attached to the pane the keeper
  goes **warn-only** (`cycle.go` / `watcher.go`, hk-1ryc/hk-6qf): the destructive
  ACT *reset cycle* and the **actionable self-restart instruction** are suppressed.
  **Q1 ANSWERED (code diagnosis 2026-07-17) — this does NOT address C4, by design.**
  The guard only suppresses ACT and the actionable *text*; the **WARN injection
  itself is never gated by operator-attached** (`watcher.go:1402-1408`: the attached
  sample only feeds `selectWarnText` for text choice, then `InjectText` fires
  unconditionally once quiesced) and uses the **same paste-then-3×-Enter primitive**.
  The code comment states it outright: "The warn is still delivered; only the
  self-restart command is withheld." So a WARN crossing while the operator types
  lands the paste after their partial text and submits it. That is C4's primary
  mechanism. Compounding gaps: (a) attachment is detected via tmux
  `#{client_activity}` with a **5-minute** window (`tmuxresolve.go:185-226`) — a
  local operator composing a long message with pauses reads as *un*attached, and
  remote-control/mobile operators' keystrokes never advance `client_activity` so
  they're treated as unattached by design; (b) the ACT path samples attachment
  **once** at cycle entry (`cycle.go:716`, `ports.go:190`) then runs synchronously
  through a paste+Enter and up to a 300s nonce wait — a TOCTOU window where an
  operator who starts typing after the snapshot is raced.

### Known gaps flagged by the code
- The **automatic** cycle does NOT run `await-ack` verification (only manual
  `restart-now`/`ping` do — hk-uldg OUT OF SCOPE).
- The keeper does **NOT** check comms subscription anywhere. The operator's "check
  if subscribed to comms first" idea is net-new, not a tweak.

---

## Case catalog (the situations a good design must handle)

From the operator, plus derived:

- **C1 — Operator ↔ captain/admiral, mid-conversation.** Restart disrupts the
  thread. But as context grows the agent *should* eventually be nudged to hand off.
  Restarting while the operator is typing is the worst case. (Note: operator-attached
  warn-only path *should* cover this — why doesn't it fully?)
- **C2 — Crew a few turns from finishing a task.** Ideal: let it finish, then hand
  off. A natural pause is imminent; forcing a stop now is pure loss.
- **C3 — Crew just stopped, one token from the restart limit.** No buffer left;
  restart must happen now regardless.
- **C4 — Operator has typed a long unsent message; keeper injects after it.** Both
  the message and the reply intent are lost. High-pain, high-frequency (admiral).
- **C5 — Never-idle session.** Perpetually busy → must be force-cut eventually
  (FORCE-ACT exists for this).
- **C6 — Crew transcript review (retro).** We *notice* the pain most with
  captain/admiral; we should sample crew transcripts post-hoc to measure how
  disruptive keeper restarts were for crews too, before assuming they're fine.

---

## Idea inventory (brainstorm — NOT decisions)

Operator ideas first, then derived. Each tagged with rough feasibility given the
current architecture.

### From the operator
- **I1 — "Range" / early-restart window.** When a crew *stops* (turn boundary) and
  is within a `range` (e.g. 10k) below the cutoff (e.g. stops at 195k with a 200k
  cutoff), the keeper sends the handoff then. Restart at the *last natural pause
  before* the ceiling instead of at the ceiling. **Feasible** — CrispIdle already
  detects the stop; this is a "if idle AND within range of ACT" gate. Directly
  attacks C2/C3.
- **I2 — Softer handoff language.** Change the injected message from "stop and hand
  off now" to "when you're at a good stopping point, hand off." **Risk (operator-
  observed):** agents get confused about what a "good stopping point" *is*. The
  language needs real work — vague guidance produces vague behavior. Needs a crisp,
  testable definition of "good stopping point."
- **I3 — Deferred-restart command for captain/admiral.** Instead of injecting a
  restart, inject: "when you're at a good stopping point, hand off then run
  `<command>`." The command triggers the restart and carries the keeper's cycleID
  (agent passes the nonce in). This is **exactly the existing `restart-now` +
  ActionableWarnText path** the captain already uses — the idea is to make it the
  *default* for conversational agents, and possibly for crews. **Feasible, mostly
  built.**
- **I4 — Check comms-subscription, then push over comms.** If the agent is
  subscribed to comms, deliver the nudge as a comms message rather than a raw pane
  inject — especially for captain/admiral, so it doesn't collide with the operator's
  input box. **Net-new** (keeper doesn't touch comms today). Attractive because
  comms delivery empirically feels less disruptive.
- **I5 — Escalating interval reminders past a threshold.** Once over a threshold,
  re-nudge at shrinking intervals (e.g. +5k, +3k, +1.5k) assuming the agent keeps
  working, rather than one warn then silence-until-cut. **Feasible** — the warn path
  already fires once on the upward crossing; this generalizes it to a schedule.
- **I6 — Deliver nudges "the comms way," not necessarily over comms.** The key
  insight: a comms message seems to get *injected into context* while the agent
  keeps working, and (crucially) does NOT sweep in the operator's half-typed input.
  Understand the mechanical difference between the comms inject and the keeper
  inject and adopt whichever property makes comms non-disruptive. **This may be the
  highest-leverage thread** — it addresses C1/C4 at the delivery layer regardless of
  which timing policy wins.

### Derived / worth adding
- **I7 — Empirically diagnose the operator-input-loss (C4).** The
  operator-attached warn-only path (hk-6qf) is *supposed* to prevent exactly the
  "inject on top of operator keystrokes" case. Before designing new machinery,
  find out why it still happens: is attachment mis-detected? is it a WARN (not
  suppressed) that's landing? is the paste racing the send? **This is the first
  groundwork task** — the answer reshapes the whole design.
- **I8 — Redefine the ACT trigger from "idle + threshold" to "idle + threshold +
  task-boundary."** CrispIdle is a turn boundary, not a task boundary. I1's "range"
  is one way to approximate "last pause before ceiling." Consider whether the agent
  can signal task-complete explicitly (a marker the keeper reads) so the restart
  lands between tasks, not between turns.
- **I9 — Preserve the operator's unsent input across a restart.** Even if the
  keeper never sweeps it in, a restart still drops whatever is in the input box.
  Is there any way to capture/stash it? (Probably out of the keeper's reach — flag
  as an open question, maybe a Claude Code concern.)

---

## Open questions / groundwork before designing

1. ~~**Why does C4 still happen** given the operator-attached warn-only path?~~
   **ANSWERED (2026-07-17, code-verified).** The operator-attached guard only
   suppresses the ACT cycle + the actionable restart *text*; the **WARN injection
   is not gated by attachment** and fires the paste+Enter regardless
   (`watcher.go:1402-1408`). Compounded by a 5-min / remote-control detection gap
   and a single-sample ACT TOCTOU. Full write-up in the grounded map above. Residual
   gap: which mechanism fired in a *given* incident needs the `events.jsonl`
   window (`session_keeper_warn` vs `…_operator_attached` timestamps vs operator
   keystrokes) — code alone can't attribute a specific incident.
2. ~~**What mechanically makes comms delivery feel non-disruptive**~~ **ANSWERED
   (2026-07-17, code-verified).** Nothing in the tmux layer does — all three paths
   send a trailing Enter and paste at the cursor without clearing. The only
   difference is the keeper's 750ms-settle + 2-retry-Enter window vs comms' single
   immediate Enter; the "non-disruptive" feel likely comes from Claude Code TUI
   paste-queuing while busy, not the injector. See grounded map above. The retry
   loop is a load-bearing reliability fix (hk-89g) — reversing it is not free.
3. **What is a crisp, agent-legible definition of "good stopping point"?** Without
   it, I2/I3/I8 all inherit the same ambiguity failure.
4. **How disruptive are crew restarts, really?** Sample crew transcripts (C6) to
   decide whether crews need the same treatment as captain/admiral or a lighter one.
5. **Does the three-tier intent (soft/stern/hard) already exist adequately** in
   WARN/ACT/FORCE/HARD-CEILING, and is the problem purely *delivery + timing* rather
   than *thresholds*? (Working hypothesis: yes — the band is fine, the trigger
   moment and the injection mechanics are what hurt.)

---

## Done means... (for THIS research plan)

1. Problem statement is agreed with the operator (this doc, reviewed).
2. ✅ **I7 answered (2026-07-17, code-verified).** Root cause: the WARN injection is
   structurally exempt from the operator-attached guard and delivered via the same
   paste+3×-Enter primitive; the guard only suppresses ACT + the actionable text.
   Compounded by 5-min/remote-control detection gaps and an ACT-path TOCTOU.
   *Residual:* attributing a specific incident still needs its `events.jsonl` window.
3. ✅ **Q2 answered (2026-07-17, code-verified).** The three inject paths are nearly
   identical (all trailing-Enter, no bracketed paste, no line-clear); the only
   difference is the keeper's settle + 2 retry Enters vs comms' single immediate
   Enter — and that retry loop is a load-bearing reliability fix (hk-89g). The
   "comms feels better" effect is not in the tmux layer.
4. ✅ **C6 ANSWERED (2026-07-18, data + transcript sampling — see `C6-findings.md`).**
   12 crews / 138 restarts in the event log + 4 sampled crews (kynes, leto, yueh,
   chani). **Crews do NOT suffer the C1/C4 pain** that motivates this plan — no
   operator conversation, warn/restart≈0.28 (vs captain 1.67), restarts already land
   at natural idle pauses. Healthy restarts (kynes, leto) are near-zero disruption:
   state survives via **durable substrate** (`HARMONIK_AGENT` env + on-disk mission
   file + config.yaml + beads), NOT the handoff — crews often don't even read their
   own handoff on reboot. The crew-relevant failure is **keeper RELIABILITY, not
   timing**: a 20% abort rate that decomposes into (a) handoff-never-delivered (dead
   keeper watcher / parked-crew unreachable — yueh) and (b) handoff-written-but-
   discarded (recovery boot doesn't read the custom-path handoff; in-flight run
   SIGKILLed → orphan-process contamination — chani). **Implication: crews need a
   lighter, reliability-focused treatment kept separate from the captain/admiral
   timing+delivery redesign.**
5. ⬜ A short direction memo picks 1–2 threads to pursue and hands off to a
   solution/kerf-work plan. NOT built here. (The Q1/Q2 findings above reshape the
   candidate threads — see note below.)

## What the Q1/Q2 findings imply for the option space (framing only — NOT a decision)

The diagnosis relocates the highest-leverage threads without choosing one:
- **I6 (deliver "the comms way") is smaller than feared but not a silver bullet.** The
  tmux sequences are ~identical, so there's no magic injection property to copy; the
  difference is the keeper's extra Enters. If the goal is "don't force-submit the
  operator's line," the lever is the **submit behavior**, not the buffer path — but
  it collides with the hk-89g reliability fix, so it's a real tradeoff, not a freebie.
- **I7 points the WARN path at the real bug.** The cheapest honest improvement space is
  around *the WARN injection itself* (it ignores attachment and force-submits) and the
  *detection freshness* (5-min window; remote/mobile blindness) — both are where C4
  actually originates. Any redesign should start there, not at the ACT band.
- **The three-tier band (Q5 hypothesis) looks confirmed-adequate:** the pain is
  delivery + timing + attachment-detection, not the threshold values — consistent with
  the STATUS.md hard guardrail of ZERO warn/act/force/window changes.
These are inputs to the direction memo (item 5), still to be written with the operator.
