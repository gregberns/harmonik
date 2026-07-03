# Captain & Crew — Problem Space

## Summary
A **Captain** is a long-lived orchestrator that brings up and coordinates a pool of
long-lived **crew** agents, each owning a major initiative (an epic) and its own work
queue. This work defines the **mechanical wiring** to (1) start a captain and its crew
as persistent sessions, (2) deliver work and status between them over the comms bus, and
(3) notify the captain structurally when an initiative (epic) completes. It deliberately
excludes the captain's *judgment* layer.

## Goals (in scope)
1. **Start a captain and crew (captain spawns — option A).** The captain **programmatically
   starts** long-lived crew sessions: it writes a mission/handoff file, then launches a
   persistent local-tmux `claude` with `/session-resume <handoff>`. Each crew member
   registers on the comms bus by a stable name, owns a named queue, and runs under keeper
   context-management. Crew persist across many epics (NOT one-bead-then-kill). Reuses the
   daemon's tmux-spawn substrate + the keeper's resume-injection; the *persistent* lifecycle
   is the net-new part.
2. **Comms in between.** The captain sends the crew member its work — an initial message on
   spawn and ongoing epics via `comms send --to <crew>`; crew report progress/status back
   over comms (`--topic status`). Directed, durable, dedupe-on-`event_id`.
3. **Events.** When an epic completes (its last child bead closes), the system emits a
   structural `epic_completed` event the captain subscribes to — so the captain learns of
   completion without depending on a crew self-report.
4. **Crew launch instructions.** A standard launch context every crew member boots with:
   its identity (name), its named queue, how to **subscribe to its messaging**
   (`comms recv --follow --to <name>`), and its operating loop (work the assigned epic;
   self-restart via keeper). The handoff file the captain writes carries the per-crew
   mission; the launch context carries the invariant boot/subscribe steps.

## Non-goals (explicitly out of scope)
- **Portfolio model** — how the captain *ranks / prioritizes* initiatives across crew.
- **Stuck / failed-initiative handling** — what the captain does when a crew member's
  initiative stalls or fails.
- **The keeper restart mechanism** — built elsewhere (session-keeper Phase-2); this work
  *depends on* it but does not build it.
- **(removed — was wrong.)** `--remote-control` / programmatic session drive is now the
  IN-scope spawn+address mechanism (see Resolved decisions). What remains out: running crew
  on a *different host* via ssh, and a hosted/multi-tenant control plane.
- **Programmatic spawn-on-demand sophistication / a Go "captain supervisor"** — Phase 2.
- **A joined `harmonik roster` view** — optional convenience; roster data already lives in
  beads + comms.

## Constraints
- **Programmatic drive IS supported (corrected 2026-06-08).** A process can start a
  persistent Claude Code session and re-task/restart it headlessly by `session_id` —
  `claude -p "<task>" --resume <id> --output-format json` and the Agent SDK (`resume=<id>`);
  `claude remote-control --name <n>` runs a long-lived, cloud-watchable,
  programmatically-drivable server (docs: `headless.md`, `remote-control.md`, agent-sdk
  overview). So crew are **long-lived `claude remote-control` sessions, addressed by
  `session_id`, tasked over comms** — autonomous AND watchable from any device. (Supersedes
  the earlier "local tmux only / not drivable" assumption, which was a misread of the docs.)
- **Reuse existing durable surfaces, don't reinvent:** roster = beads `--assignee/--owner`;
  per-work journal = `br comments`; per-agent status feed = `comms --topic status`
  (events.jsonl-backed); per-crew work = named queues; restart = keeper.
- **Crew lifecycle ≠ bead lifecycle.** Spawning/retiring crew is rare and coarse — must NOT
  reuse the daemon's one-bead-then-kill spawn path.
- **`epic_completed` has no close-cascade today** — `bead_closed` carries only RunID+BeadID;
  emitting epic-completion requires querying the bead's parent + remaining open children at
  the close site (after `emitBeadClosed`, `internal/daemon/workloop.go`).
- **Depends on the keeper** for crew context-restart (in-flight: session-keeper Phase-2 +
  the token-threshold calibration flagged to flywheel).

## Success criteria (concrete, verifiable)
1. A captain session brings up ≥2 named crew sessions; `harmonik comms who` shows all of
   them online, each on its own named queue.
2. The captain assigns an epic to a crew member via comms; the crew member receives it
   (deduped on `event_id`) and begins dispatching that epic's beads to its own queue.
3. A crew member writes progress to a durable, captain-readable feed as it works
   (`comms --topic status` and/or `br comments`).
4. When the last child bead of an assigned epic closes, an `epic_completed` event fires
   carrying the epic id, and a captain subscribed to it receives the notification.
5. On keeper-driven restart (context-fill), a crew member re-hydrates its assignment from
   durable state (its queue + assigned epic) and continues — captain coordination is
   unaffected by the restart.
6. The captain spawns a NEW crew member non-interactively: it writes a mission handoff and
   starts the session with `/session-resume`; the crew member boots, **auto-subscribes to
   its comms inbox**, and claims its named queue with no human steps.

## Resolved boundary decisions
- **Spawn model = (A) captain spawns long-lived crew.** The captain programmatically starts
  each crew member as a **long-lived `claude remote-control --name <name>` session** seeded
  with a mission handoff, captures its `session_id`, and tasks it over comms. Crew run their
  own operating loop (watch comms, dispatch their epic) and live across dozens of epics.
- **Crew liveness = (a) long-lived autonomous** (confirmed 2026-06-08), NOT (b)
  headless-per-step.
- **Addressing + restart use `session_id` / `--resume`** (a first-class primitive), NOT
  tmux-pane injection. This resolves the keeper↔pane-identity gap and aligns with the keeper,
  which already keys identity on `session_id`.
- **`--remote-control` is IN scope** as the spawn / address / watch mechanism; the *judgment*
  layer remains out.
