# Direction — operator-confirmed decisions + the centralization seam

**Date:** 2026-07-03. Records operator steer on the POSSIBILITY-SPACE, and opens the "centralized
changing-state" design. Status: planning.

## Confirmed decisions

### A — Config shape (RESOLVED)
A **folder per agent type**. Inside: several *static* content files (SOUL, operating-instructions,
etc.) + a **tie-together manifest** (a yaml of paths + presence). The system parses the yaml and
pulls in only what's needed per launch. No mega-file of all missions. This IS the capabilities
manifest: yaml declares *what + where + injected/retrieved/embodied*; files hold content.

### B — Model today's reality first, don't refactor roles away (RESOLVED)
Getting rid of captain is NOT a goal. Model what exists as config:
1. **`crew` type** first — cardinality 1:n, no triggers, small mission to execute one epic. The proving ground.
2. **`admiral` type** — model it into the same system.
3. **`captain`** — configure it, and tie its config to the existing `harmonik start captain` command (captain stays a first-class verb; its *content* comes from the registry).
Anti-drift is handled by the **provenance rule** (identity re-read from the type folder, never the
outgoing handoff), not by live monitoring — we can't monitor drift well, so design it out.

### C — Type vocabulary (RESOLVED)
**Open-ended.** A new type = a new folder. No hardcoded enum, no code change to add one.

### D — Embodied guardrails (DEPRIORITIZED)
Not rules-as-code — principles, and the mission defines them. EXCEPTION worth building: an
**alignment watcher as its own configured agent type**, whose mission evolves over time. The system
defines its own overseer through the same config mechanism as everything else.

## Specific mechanics settled

- **Cardinality vs captain self-restart:** `max:1` forbids a *second* instance. A restart is
  *replace*, not *add* — a lifecycle transition (tear down → bring up one). Captain gets a restart
  command it practices on another agent type first, then applies to itself.
- **Markers WITHOUT transcript scanning:** agents already emit structured actions to the comms bus +
  `events.jsonl`. The watch matches *that stream* against "role X never emits command Y" (e.g. a
  `crew_start` event attributed to admiral → flag). Cheap filter on an existing firehose; NOT a
  transcript crawl. Un-emitted actions are simply unchecked. Same stream a dashboard reads.

## The centralization seam — "publish, don't narrate"

### Diagnosis (why `admiral-initiatives.md` goes stale)
Updating it is a **discretionary prose edit with no schema and no cadence** → it rots, and the live
truth migrates into the private handoff (wrong place: handoff is narration for the agent's own next
session, not publication to the system).

### The distinction we've been conflating
- **Narration** (EPISODE / handoff) — "where I was mid-thought," private, prose, for future-me.
- **Publication** (FLEET state) — "priorities are X, I'm focused on Y, crew Z owns epic W" —
  structured, for the system + operator + peers.

### The mechanism (reuse the beads pattern)
Agents publish structured state via a **command** (mirror `br`), to a structured store that
**flushes to a git-tracked JSON/JSONL** (exactly `br sync --flush-only`). One pattern, four wins:

| Want | Delivered by |
|---|---|
| Agents *share* with the system | a command (`harmonik <state> set …`), not a prose edit |
| Queryable / dashboard-able | structured store = single source of truth |
| Never stale | publishing is a **loop step** (like crew posting on bead-close), not discretionary |
| Git history over time | store flushes to a tracked file — diffs, blame, survives machine loss |

**Consequence:** priorities / focus / assignments are **ledger data, not prose.** `admiral-
initiatives.md` becomes a *generated view* of the store, not a hand-edited file. This is the concrete
implementation of the LEDGER/FLEET memory horizons — the LEDGER stops being a markdown file the agent
rewrites and becomes a surface it publishes to. It's also the data layer the
`plans/2026-07-03-operator-dashboard` work needs.

### Open sub-questions for the centralization seam
- **Store choice:** new lightweight store (JSON flushed) vs SQLite-like-beads vs *extend beads
  itself* (are priorities just ranked epics? probably not — orchestration intent ≠ work items).
- **Record vocabulary:** what does the fleet-state schema hold? Candidates: priority list (ordered),
  focus/lane per orchestrator, assignment (agent ↔ epic ↔ status), initiative status
  (ACTIVE/ON-DECK/PARKED). Reuse the admiral-initiatives status vocab as a start.
- **Cadence enforcement:** what makes an agent publish? (loop-step convention vs a keeper/watch nudge
  vs a deterministic "you haven't published in N" check — mirror the crew progress-feed cadence).
- **View generation:** is the human-readable file (git-tracked) generated on flush, or is the flushed
  JSONL itself the git artifact + the dashboard renders live?

## Suggested build order (matches operator's "model what we have" steer)
1. Type-folder + manifest structure; model `crew` end-to-end (B1).
2. Provenance rule for identity re-pin (the Xerox fix) — highest-value, claude-first.
3. Model `admiral` + wire `captain` config to its command.
4. The publish seam (fleet-state store + flush) — unblocks dashboard + fixes stale priorities.
5. Alignment-watcher as a configured type; marker checks on the event stream.
