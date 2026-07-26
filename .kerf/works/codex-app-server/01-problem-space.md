# 01 — Problem Space: Resident `codex app-server` as a harmonik crew orchestrator

> codename:codex-app-server · parent epic hk-q3ovr (MR1 CREW harness selection) · jig:spec
> Supersedes the retired spike hk-fijwi. **Option B (`codex exec resume` per-wake re-invoke) is
> permanently killed by operator decision 2026-07-11 — not reasoned from anywhere below.**

## What is changing, and why

Harmonik runs its orchestrators (captain, crew leads) as **long-lived Claude sessions**: one
resident `claude --remote-control <label> --session-id <uuid>` process per role that stays up for
a whole session — joins comms, submits/monitors a named queue, closes beads, and is re-tasked
*while alive* by bracketed-paste into the live pane. Because that session accumulates context, it
needs the **keeper** — a per-session context-fill watcher that drives an intent-preserving
handoff → `/clear` → `/session-resume` cycle before the pane overflows.

The program goal (MR1, operator/admiral 2026-07-05) is to run a crew orchestrator on a
**non-Claude harness** to cut orchestration-tier Claude token burn. The operator has now fixed the
target: investigate the **resident `codex app-server`** path — Codex's long-lived, bidirectional
JSON-RPC surface (the same protocol the VS Code extension and desktop app drive Codex through) —
as the substrate for a resident, long-context Codex orchestrator.

The distinctive bet the operator wants tested: a **resident long-context model session may retire
the keeper/compaction machinery entirely.** Claude orchestrators need keeper because the client
holds a bounded, growing context window that must be handed-off and cleared. If Codex's
app-server holds durable, server-side, resumable thread state with its own context management, the
whole handoff→clear→resume apparatus (and the per-session keeper watcher) may become unnecessary
rather than merely ported. That is the central research question, not an aside.

## Goals (what should be true after this work)

- A **design spec** exists describing how a harmonik crew orchestrator would run on a resident
  `codex app-server` session: the process/session lifecycle, how the daemon/captain commissions
  and drives it, how it joins comms + owns a named queue, and how it is re-tasked mid-session.
- The spec **answers the keeper question** with evidence: does a resident long-context Codex
  session retire the keeper/compaction cycle, reduce it, or still require an equivalent — and why.
- Findings are **grounded in the real app-server surface** (proto/driver, session lifecycle,
  context handling, restart/reconnect semantics), not in any prior Option-B framing.
- The design **names the concrete integration path** so the parked `hk-l63b9` (route crew-start
  through harness selection) can un-park with a real target.

## Non-goals (this phase)

- **No implementation.** Problem-space → decompose → research → change-design → spec-draft only.
- No resurrection of Option B (`codex exec resume` per-wake) or Option A (persistent bare `codex`
  TUI). Both are closed; do not re-survey them.
- Not deciding Pi-as-orchestrator here — but note where the app-server design does/doesn't
  generalize to a second non-Claude harness (flag, don't design).
- Not building the harness-selection routing (`hk-l63b9`) — that stays parked until the design
  names the path.

## Constraints & things that must not change

- **Orchestrator behavioral contract is fixed** (orchestrator-rules skill): dispatch discipline,
  kerf-first priority, daemon-owns-terminal-transitions, review gate, comms presence/dedupe (N3),
  named-queue-only (never `main`). The Codex orchestrator must satisfy the same contract; only the
  *substrate* changes.
- **Comms semantics** (at-least-once, dedupe on `event_id`) and the **queue submit/subscribe**
  surface are the integration seam — the design plugs into them, does not alter them.
- Existing **Codex worker harness** (`internal/daemon/codexharness.go`, spawn-per-turn
  `codex exec --json`, resume via `codex exec resume <thread_id>`) is done and stays as-is; the
  orchestrator path is additive, not a rewrite of the worker path.
- Adding a **JSON-RPC app-server client** is a new subsystem/transport with new failure modes
  (restart/reconnect, auth, backpressure). The design must treat that cost honestly and show the
  benefit (resident reachability + possible keeper retirement) justifies it.

## Success criteria (concrete, spec-level)

1. The design spec **describes the app-server session model** a harmonik orchestrator would use:
   how a session is created, kept resident, driven turn-by-turn, and recovered after a
   daemon/app-server restart — cited to the real app-server protocol.
2. The spec **states a keeper verdict**: retire / reduce / retain-equivalent, with the mechanism
   (server-side context handling) that justifies it.
3. The spec **names the harmonik integration point** (where the app-server client lives relative
   to the daemon, how captain commissions a Codex crew, how comms + queue attach) precisely
   enough that `hk-l63b9` can un-park against it.
4. Open risks/unknowns are enumerated with what evidence would close each.

## Preliminary areas likely affected (feeds Pass 2 decompose)

- **codex-app-server-protocol** — the external surface: JSON-RPC methods, session/thread
  lifecycle, context/compaction handling, restart & auth.
- **orchestrator-session-model** — how a resident Codex session maps to the orchestrator loop
  (comms-join, queue own, re-task mid-session).
- **daemon-integration / harness-routing** — where the app-server client sits, how
  `buildCrewLaunchSpec` / crew-start routes to it (the `hk-l63b9` seam).
- **keeper-compaction** — the keeper/handoff/clear machinery and whether a resident long-context
  Codex session retires or reshapes it.
- **captain-commissioning & keeper-restart mapping** — how captain drives/commissions the Codex
  crew and what "restart continuity" means when state is server-side.
