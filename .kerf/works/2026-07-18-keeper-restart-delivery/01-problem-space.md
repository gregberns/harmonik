# Keeper restart timing & delivery — Problem Space

Codename: `2026-07-18-keeper-restart-delivery` · jig: spec · pass 1
Upstream research (authoritative inputs, do not re-derive):
`plans/2026-07-17-keeper-restart-timing/_plan.md`, `.../C6-findings.md`, `.../DIRECTION.md`.

## Summary

The keeper — the background process that restarts a Claude session before its context
window fills — restarts *correctly* but at the *wrong moment* and *through the wrong
channel* for leader sessions (the captain and admiral, the sessions the operator talks
to). Two mechanisms cause the pain, both code-verified in the upstream research:

1. **Delivery collision.** The keeper's warn/restart nudge is typed into the agent's
   terminal as a paste-then-Enter (`internal/keeper/injector.go`). When the operator is
   mid-way through typing, the paste lands on the operator's input line and the trailing
   Enter(s) submit it. The operator's half-written message and the agent's reply intent are
   both lost. The operator-attached guard suppresses the *actionable restart text* but **not
   the warn injection itself** (`watcher.go`, hk-vs4u/hk-1ryc block: "the lighter advisory
   ALWAYS injects once gaugeQuiesced ... even when NOT CrispIdle"), so the collision still
   happens.

2. **Timeout gap.** Once the keeper asks for a handoff it watches the handoff file for a
   **fixed 300 s** (`internal/keeper/cycle.go`, `DefaultHandoffTimeout`, hk-4xni9). If the
   agent takes longer — because it (correctly) finishes a task or a conversation first — the
   keeper gives up. The agent then writes a handoff no one is watching and stalls on a
   bloated context, never restarted. Below the hard force-ceiling there is no fallback.

This work redesigns the leader-session path so the nudge is **delivered without stepping on
the operator**, **framed to defer until a real pause**, and **backed by an agent-run restart
command** that does not depend on the keeper still watching. It also sharpens the keeper's
read of "is the operator here?" and "is the agent reachable?", and — the operator's explicit
call — ships with **integration / end-to-end / twin-scenario testing**, not just the unit
tests that already exist.

The crew case is a *different* problem (reliability, not timing — proven in C6) and is
handled as a **separate bug track**, already filed as beads
(`keeper-reliability`: hk-220lv, hk-4tjyj, hk-bl2k6, hk-pgtt6) and routed to the captain.
This spec does **not** own those. It does address one crew-touching design question: whether
the same "finish, then self-restart" message should extend to crews once reliable.

## Goals

- **G1 — Deliver the nudge over comms, not a terminal paste, for leader sessions.** The
  keeper's nudge to the captain/admiral is delivered as an `agent_message` on the comms bus
  (the channel agents already use to talk to each other), which lands in the agent's reading
  queue at its next turn and cannot touch the operator's input line.
- **G2 — The keeper verifies the agent is actually reachable on comms before delivering over
  comms.** (Operator-directed 2026-07-18.) Comms delivery only works if the target has its
  inbox armed. The keeper must confirm this and fall back deterministically (defined below)
  when it cannot — no silent no-op.
- **G3 — Frame the message to defer to a real pause.** The message instructs the agent to
  hold if it is (a) mid-conversation with the operator or (b) mid-task, and to hand off at
  the next good stopping point — with "good stopping point" made concrete via the Q3 self-
  test (see Constraints).
- **G4 — Every nudge carries the agent's own restart command as the guarantee.** The message
  includes the command the agent runs to trigger its own handoff-confirm → clear → reboot,
  carrying the keeper's cycle nonce. This makes the restart independent of whether the
  keeper is still inside its 300 s watch window.
- **G5 — Sharpen the keeper's situational read.** Improve operator-present detection
  (currently a 5-minute activity window that misreads a slow-typing or remote operator as
  absent) and add a reachability/liveness pre-check before a cycle fires.
- **G6 — Ship with real end-to-end testing.** Integration, end-to-end, and twin-scenario
  coverage for each named failure, each failing before the change and passing after.
- **G7 (conditional) — Decide the crew extension.** Determine whether the finish-then-self-
  restart message should extend to crews, gated on the `keeper-reliability` bugs being fixed;
  produce a written disposition (adopt / defer / reject-with-reason). Operator direction
  (2026-07-18): the crew message itself is **deferred** — but must be delivered through the
  configurable mechanism (G8) so it can be turned on and tuned on the fly without a code
  change, rather than hard-coded and shipped later.
- **G8 — Nudge message text is configurable, editable on the fly.** (Operator-directed
  2026-07-18.) The wording of every keeper nudge (leader defer-message, crew message, the
  good-stopping-point framing) lives in external configuration the operator can edit without
  a rebuild/redeploy, so the messages can be iterated toward "agents don't get confused"
  empirically. The message *structure* (the four required elements — defer conditions,
  stopping-point test, self-restart command+nonce) is normative; the *prose* is tunable.

## Non-goals

- **NG1 — No threshold changes.** ZERO changes to the warn / act / force / hard-ceiling /
  window values (STATUS.md hard guardrail; the research confirmed the band is adequate and
  the pain is delivery + timing, not the numbers).
- **NG2 — The four crew reliability bugs are out of scope here.** hk-220lv / hk-4tjyj /
  hk-bl2k6 / hk-pgtt6 are a separate bug track delegated to the captain. This spec may
  *reference* them where the crew-extension decision (G7) depends on them, but does not
  specify their fixes.
- **NG3 — Not removing the terminal retry-Enter loop.** The 750 ms-settle + 2-retry-Enter
  sequence is a load-bearing reliability fix (hk-89g / hk-ip33d / hk-7rgqs): a lone immediate
  Enter was intermittently dropped. Any change to the terminal path must preserve delivery
  reliability, not naively delete the retries.
- **NG4 — Not preserving the operator's unsent input across a restart** (idea I9). Likely
  outside the keeper's reach (a Claude Code / TUI concern). Flag as an open question, do not
  scope.
- **NG5 — Not touching the reset cycle's core invariant** (SK-INV-001: never clear without a
  nonce-confirmed handoff). The agent-run command must uphold it, not bypass it.

## Constraints

- **The "good stopping point" test is agent-legible, not keeper-legible** (Q3, answered
  upstream). The keeper cannot read the agent's context, so it can only detect a turn
  boundary (CrispIdle), never a task boundary. Therefore the *agent* owns the self-
  assessment; the keeper *nudges and bounds*. The concrete test to wire into G3's message:
  *a good stopping point is one where everything needed to continue is already saved to disk
  / the task ledger / a short handoff — nothing important lives only in the agent's context;
  specifically (i) between discrete units, not mid-edit/plan/tool-sequence; (ii) in-flight
  work committed or trivially re-derivable; (iii) no unanswered operator question held;
  (iv) next session resumes from handoff + substrate with no redo and no lost decision.*
- **Deadline-bounded deferral.** Because an agent may never idle on its own (never-idle
  session, C5), the deferral in G3 must sit under the existing FORCE-ACT ceiling, which cuts
  unconditionally. Soft target, hard backstop — do not weaken the backstop.
- **Comms is net-new to the keeper.** The keeper touches no comms code today; G1/G2 add that
  surface. Delivery-as-comms depends on the target having `comms recv --follow` armed via the
  Monitor tool (hk-b51bg) for the message to land as a turn.
- **Existing self-restart primitive.** `internal/keeper/restartnow.go` (hk-5da7) already
  implements a synchronous agent-run restart carrying a nonce; the captain/admiral already
  use a form of it. G4 is largely wiring it in as the default + composing the message, not
  inventing a mechanism.
- **The twin is the scenario-test vehicle.** `cmd/harmonik-twin-claude` is a scripted stand-
  in for a real Claude session; existing parity audit at `docs/twin-parity-audit-2026-05-14.md`
  is required reading before writing scenario tests, so coverage targets paths the twin
  actually reaches.

## Success criteria (decidable)

- **SC-1.** The spec defines a leader-session nudge delivered as an `agent_message` over the
  comms bus, with the delivery-channel decision (comms vs terminal-fallback) specified as a
  deterministic function of a named reachability check — no path where the keeper injects
  into a leader's terminal while the operator may be typing. Decidable by reading the spec's
  delivery state table.
- **SC-2.** The spec defines the reachability check (G2): what the keeper inspects to decide
  the agent is on comms, and the exact fallback when it is not. No branch results in a silent
  no-op. Decidable by grep for the fallback clause + absence of an unhandled branch.
- **SC-3.** The nudge message content is specified verbatim (or as a normative template) and
  contains all four elements: the two defer conditions (operator-conversation, in-flight
  work), the concrete good-stopping-point test, and the agent-run restart command with its
  nonce. Decidable by checklist against the message template.
- **SC-4.** The spec states the restart completes via the agent-run command **independent of
  the keeper's 300 s watch window** — i.e. a handoff written at T+301 s still results in a
  clean restart. Decidable by a scenario test asserting exactly this (see SC-7).
- **SC-5.** The spec specifies the sharpened operator-present detection (G5): what replaces
  or augments the 5-minute `client_activity` window, and how remote/mobile operators are
  handled, with the failure mode (misreading present-as-absent) named and closed. Decidable
  by comparison against the named current behavior in `tmuxresolve.go`.
- **SC-6.** The spec includes a written disposition for the crew extension (G7): adopt /
  defer / reject, each with a reason and a stated dependency on the `keeper-reliability`
  beads. Decidable by presence of the disposition + reason.
- **SC-6b.** The spec defines message text as external configuration (G8): where it lives,
  the edit-without-rebuild path, and which elements are normative-structure vs tunable-prose.
  A message-wording change requires no code change. Decidable by reading the config surface +
  the structure-vs-prose split.
- **SC-7.** The spec's test plan names, for each of these failures, a test at the stated
  level that fails before and passes after: (a) operator-typing collision → integration/twin;
  (b) late-handoff-after-300 s → twin scenario; (c) comms-unreachable fallback → integration;
  (d) operator-present misread → unit/integration; (e) FORCE-ACT still cuts a never-idle
  session → existing-level. Decidable by checklist mapping each failure to a named test.
- **SC-8.** No aspirational language in normative spec text (banned: "appropriate",
  "adequate", "gracefully", "reliable", "as needed"). Decidable by grep.
- **SC-9.** No normative statement changes a threshold value (NG1). Decidable by grep against
  the warn/act/force/window constants in `thresholds.go`.

## Open questions to carry into later passes

- **OQ-1 (blocks G1/G2).** Does the captain/admiral session reliably keep `comms recv
  --follow` armed across its own keeper restarts? If not always, G2's fallback carries more
  weight. (Confirm against crew-launch / captain restart behavior.)
- **OQ-2 (G4 mechanics).** Can `restartnow.go` be invoked by the agent from *inside* a normal
  turn with the keeper's minted nonce, or does the nonce provenance need a new path? Research
  pass to confirm.
- **OQ-3 (G5 scope).** Is a sharper operator-present signal available without a Claude Code
  change (e.g. a keystroke-recency signal the keeper can read), or does closing the remote/
  mobile blindness require cooperation from the TUI? Determines whether G5 is fully in-scope.
- **OQ-4 (G7).** Which of the four `keeper-reliability` bugs must land before a crew self-
  restart message is safe? (Likely hk-220lv dead-watcher + hk-4tjyj discarded-handoff.)
