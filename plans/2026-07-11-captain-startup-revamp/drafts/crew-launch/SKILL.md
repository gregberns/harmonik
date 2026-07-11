<!--
DRAFT — proposed replacement for .claude/skills/crew-launch/SKILL.md (2026-07-11
startup-doc revamp, Stage 2 principles pass). Do NOT deploy without the Stage-3 gate checks.
Landing notes:
  1. Strip this HTML comment at landing so the frontmatter is the first bytes.
  2. PREREQ (cutover Step 1.1 before Step 3.1): drafts/agents/crew/operating.md lands FIRST —
     it now carries every §2.6 re-homed guardrail (false-drain guard, 10-min poll floor, no
     self-unblocking, no Agent-tool sub-agents for epic work, $STATUS_TARGET resolution) plus
     the two restored landmines (`br comments add` TEXT is positional / NO --body flag;
     subscribe `--heartbeat 60s`). If this file lands before that one, those guardrails
     vanish from every crew's injected context.
  3. Same landing as this file: crew/manifest.yaml flips the crew-launch ref
     `presence: injected → retrieved`, and operating.md's wording (no more "authoritative
     boot sequence" pointing here) ships — flip the story everywhere in ONE commit so no
     crew reads mixed eras.
  4. .harmonik/agents/_skills/crew-launch/SKILL.md (today a 1-line stub) becomes a generated
     mirror of THIS file, per _skills/SYNC.md.
  5. The ≤90s presence-refresh rule is already backfilled into agent-comms (Stage 0 LANDED);
     the `## Current State` field contract re-homes to specs/crew-handoff-schema.md
     (companion edit, cutover Step 0.3).
  6. §2.6 [A, verify-deliberate] confirmed: the `crew:<name>` label fallback and
     "beads win on disagreement" are NOT dropped — both are still stated below (§ Where
     truth lives, § Invalid handoff). The old topic-routing table (assign/reprioritize/
     park/unknown) IS dropped from this file on purpose — it re-homed to operating.md
     as one principle line ("unknown/unexpected message topics: log and no-op — never
     crash the loop") per the PRINCIPLES-NOT-RULES pass; a closed-category table is
     exactly the smell that pass exists to remove. Nothing vanished: operating.md is
     injected (always loaded), so the guarantee is stronger there than a pull-on-demand
     table here would have been.
-->
---
name: crew-launch
description: >
  Pull-on-demand reference for a crew orchestrator: park/wake discrimination,
  invalid-handoff recovery, restart verification and keeper liveness, and failure
  classification. Boot and the operating loop are injected by the crew manifest
  (`harmonik agent brief`) — this file must not restate them.
---

# Crew Reference (retrieved, not injected)

Boot = `harmonik agent brief` (`--wake fresh | keeper-restart | trigger:queue` —
`queue` is the crew manifest's one and only trigger). The brief's output IS your
complete boot context; the injected `.harmonik/agents/crew/operating.md` owns your
principles, wake ritual, dispatch loop, progress feed, and bounds. Open this file
only when you hit one of the situations below.

**Nothing here overrides the principles you already carry** — one epic one queue ·
keep moving · the daemon executes, you conduct · durable state over memory · fail
fast and loud · invisible work doesn't exist. Each section below is one of those
principles applied to an edge case. When the case in front of you doesn't match an
example exactly, reason from the principle, not the example.

## Where truth lives (durable state over memory)

Restart-survivable state outranks anything you remember or were handed:

- **The brief's embedded handoff** — tier-1 "what now". A CLAIM, not ground truth;
  live daemon/queue state overrides it.
- **Your mission file** `.harmonik/crew/missions/<crew_name>.md` — tier-2 "what
  epic / why": frontmatter `{schema_version, crew_name, queue, epic_id, goal,
  captain_name[, model]}` plus exactly ONE `## Current State` block (field contract:
  `specs/crew-handoff-schema.md`). Overwrite-only: goal is rewritten on re-task,
  superseded blocks are deleted (git is the archive), never stacked.
- **Beads** — the most durable mirror. If the mission file and beads disagree about
  your epic, **beads win** (`assignee == $HARMONIK_AGENT`; or the `crew:<crew_name>`
  label if the deployment uses the label fallback).

## Park / wake (owner: `specs/park-resume-protocol.md` §3.2 / §4.2)

The point of park is token silence: the daemon has judged the whole fleet drained,
and every loop you re-arm while parked burns a Claude turn against that judgment.
Everything below follows from that.

**Park signal:** `comms recv --follow` delivers `"topic":"park","from":"daemon"`,
then exits 0.

**Park-exit vs disconnect-exit (load-bearing discrimination):** both exit 0. Check
the last output line for `"topic":"park"`:
- **Park line present** → PARK: stop the `harmonik subscribe` monitor, leave
  `--follow` stopped, pause the progress-feed timer, await the pane nudge. Re-arm
  NOTHING in between — a re-armed heartbeat while parked defeats the whole point.
- **No park line** → normal disconnect: re-arm `comms recv --follow --json`
  immediately (a deaf idle crew sleeps through its own re-tasking).

**WAKE:** on the pane nudge, run `harmonik agent brief --wake trigger:queue` — its
output is your complete re-boot context. Re-derive live state; never trust the
pre-sleep snapshot.

If the captain instead fully stopped your pane (`harmonik crew stop`), none of this
applies — the captain re-starts you on wake.

## Invalid handoff (fail fast and loud — never guess an epic)

Dispatching against a guessed epic is worse than idling loudly: it can double-staff
a lane or burn work on the wrong goal, and nobody knows until it merges. So if the
mission file is missing, unreadable, any required field is absent, or
`schema_version != 1`:

1. Do NOT dispatch any beads.
2. Re-derive `{crew_name, queue, epic_id}` from `$HARMONIK_AGENT` + `br list` for an
   epic with `assignee == $HARMONIK_AGENT` (or the `crew:<crew_name>` label if the
   deployment mirrors ownership via the label fallback).
3. Still indeterminate → post `--topic error` to the captain
   ("invalid/missing handoff at <path>; awaiting re-seed") and idle on the inbox
   with `--follow` armed.
4. Never guess, never improvise a mission.

## Restart verification & keeper liveness

You cannot verify your OWN restart-now — the `/clear` wipes your context before the
keeper's ACK could be read. The **captain** confirms it externally via
`harmonik keeper await-ack --agent <you> --kind restart` and re-arms your keeper on
timeout; that is their job, not yours.

**Self-service liveness ping** (this you CAN do, while live — fresh nonce each time):

```bash
n=ping-$(date +%s%3N)
harmonik keeper ping --agent <you> --nonce "$n"
harmonik keeper await-ack --agent <you> --nonce "$n" --kind ping --timeout 15s
```

Exit 0 = keeper alive and watching your pane. Exit 3 = ack timeout — fail loud:
report it to the captain over comms; do NOT silently continue assuming the keeper
will save you. Thresholds, `keeper doctor`, and mechanism: the **keeper** skill.

**Idempotency on restart** (why re-running the whole wake ritual is always safe):
- `br update <epic_id> --assignee <crew_name>` on an already-assigned epic → no-op.
- Re-submitting a bead already in your queue → the daemon deduplicates.
- Re-processing a replayed `assign` → dedupe hit on `event_id` → no-op.

## Failure classification (escalation is judgment, not a reflex)

You decide and verify your own failures; you raise to the captain only what they
would genuinely want a say in — judged by stakes and reversibility, each time. A
one-off flake is yours to absorb; a bead that keeps failing is a real defect
someone is about to build on top of. On `run_failed`, classify before acting:

- **Transient** (infra flake, merge race, timeout, daemon restart): re-submit ONCE.
  Don't raise it — this is the operational trivia escalation exists to filter out.
- **Genuine bug** (deterministic test/build failure, spec mismatch): do not
  re-submit — report it with what you observed.
- **Same bead fails twice** (either class): do NOT re-dispatch; post `--topic error`
  to the captain and await instructions. Quietly re-spinning a twice-failed bead
  hides a real defect behind retry noise.

## Clean shutdown

On `crew stop`: emit a final status on both surfaces, then `harmonik comms leave`
(best-effort `agent_presence offline`; presence ages out ~120s if you crash without
it).

## Owners of the detail this file does not restate

- `.harmonik/agents/crew/operating.md` — injected principles, wake ritual, dispatch
  loop (incl. the `--heartbeat 60s` monitor, the false-drain guard, and
  unknown/unexpected message-topic handling — log and no-op, never crash), progress
  feed ($STATUS_TARGET resolution, both-surfaces × four-triggers, the
  `br comments add` positional-TEXT landmine), and bounds. Canonical; never
  duplicated here.
- `specs/park-resume-protocol.md` — full park/resume protocol.
- `specs/crew-handoff-schema.md` — mission-file field contract, incl. the
  `## Current State` block fields.
- **keeper** skill — thresholds, doctor, restart mechanism.
- **agent-comms** skill — comms CLI, N3 at-least-once + `event_id` dedupe, `--wake`
  pane-nudge, ≤90s presence refresh.
- **beads-cli** skill — `br` read surface + write discipline.
- **harmonik-dispatch** skill — queue submit/subscribe pattern.
