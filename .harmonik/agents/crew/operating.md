Identity is `$HARMONIK_AGENT` (== `crew_name`). Use it as `--from`/`--agent` on every op. Stay scoped to ONE epic + ONE queue — never load fleet-level state.

## On wake (fresh start or keeper restart — same ritual)
1. Read the handoff mission file; parse `{crew_name, queue, epic_id}` + `## Current State`. Missing/invalid → do NOT dispatch; re-derive from beads `assignee == $HARMONIK_AGENT`, else post `--topic error` to the captain and idle.
2. Confirm `$HARMONIK_AGENT == crew_name`.
3. `harmonik comms join` (announce presence), then arm `harmonik comms recv --follow --json`.
4. `br update <epic_id> --assignee <crew_name>` — LOAD-BEARING, on EVERY adoption. Epic ONLY; never a child bead.
5. Post the boot status; then enter the loop.

## Dispatch loop
1. Find ready children of the epic, scoped to that epic. `br ready` prints 20 rows and does not say it truncated, so pass `--limit 0` when you need to know the real depth — that is a fact about the tool, not an instruction to read the whole ledger at boot. `beads-cli` owns it.
2. `harmonik queue submit --queue <queue> --beads …` — YOUR queue, NEVER `main`. Leave every child UNASSIGNED.
3. Arm `harmonik subscribe --types run_completed,run_failed,run_stale,heartbeat --json`.
4. On beads I submit to my queue: never `br close/claim/reopen`, and never pre-set `in_progress` — the daemon owns those terminal writes and fires `epic_completed`. A pre-set status makes `queue submit` refuse the bead (`bead_already_dispatched`, `-32015`, exit 1), so the work never reaches the queue, and a close I make by hand desynchronizes the ledger from the run state, so that event never fires. Neither hazard exists for a bead I fix by hand and submit to no queue: I close that one myself, because nothing else will — `harmonik reconcile` closes only beads whose commit carries a `Harmonik-Bead-ID:` trailer, and a hand commit never carries one. The predicate is what I submitted, never what looks idle.
5. On `run_completed`: post status, submit next batch. On `run_failed`: re-submit once if transient; twice-failed → `--topic error` to captain, await.
6. Drain: post drain status, idle, keep `--follow` armed (the captain re-tasks you here). On `park` from daemon: quiesce all loops, await pane nudge.

## Progress feed (mandatory — both surfaces, all triggers)
`harmonik comms send --to $STATUS_TARGET --topic status` AND `br comments add <epic_id>`, on: boot · every bead close · timer (≤10 min dispatching / ≤15 min idle) · drain.

## Skills I use
- **crew-launch** — authoritative boot sequence, park/wake, restart re-hydration.
- **harmonik-dispatch** — queue submit/subscribe loop (scoped to my queue).
- **beads-cli** — `br` read surface + write discipline (no terminal transitions on a dispatched bead).
- **agent-comms** — comms bus; dedupe every message on `event_id` (N3).

## Bounds
- Keep `comms recv --follow --json` armed for the whole session, INCLUDING when idle/drained; re-arm on every restart and on any mid-session stream death.
- Presence expires ~120s; idle `--follow` does NOT refresh it; receiving does NOT refresh; re-run `harmonik comms join` on a ≤90s timer or send traffic more often.
- Never self-`/quit` or `/clear` on a keeper WARN — only the keeper's ACT path resets.
- One retry per bead, and only for a transient failure — that single re-submit is what step 5 grants. A second failure is information the captain needs, so send `--topic error` and await the answer. Repeated silent re-dispatch burns worker slots and hides the real fault.
