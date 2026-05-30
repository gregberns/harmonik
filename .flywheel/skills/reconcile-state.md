# reconcile-state — what to do at startup before the first batch

> L3b fat-skill. Fetch via `read_skill("reconcile-state")` at loop startup, after the harness
> acquires the singleton lock and before the first dispatch. Also invoke after a daemon restart
> or after the loop resumes from a `budget-paused` or `circuit-tripped` state.

## When to invoke

- The harness delivers the session-seed digest and the turn prompt includes "reconcile state before dispatching."
- After a loop restart (process cold-start or recycle after `harmonik supervise restart`).
- After the watermark is found missing or corrupt (CL-054 cold-start path).

## Purpose

Before dispatching any new work, the loop must establish a consistent picture of the world:
which beads are truly done, which are in-flight, which failed, and which the daemon thinks it owns.
This prevents double-dispatch and ensures the first batch is coherent.

## Inputs

```bash
# 1. Watermark — last processed event position
cat .harmonik/cognition/state.json 2>/dev/null | jq '{last_processed_event_id, updated_at}'

# 2. Current queue state
cat .harmonik/queue.json | jq '[.items[] | {bead_id, status, run_id}]'

# 3. Events since watermark (or full tail if cold-start)
grep -c '"type"' .harmonik/events/events.jsonl   # total event count for sizing

# 4. Open beads from beads tracker
br list --status=open | head -30
br list --status=in_progress

# 5. Recent git commits on origin/main (two-phase done verification)
git log origin/main --oneline --since="48 hours ago" | head -30

# 6. Open cognition notes (pending decisions/hypotheses)
tail -50 .harmonik/cognition/notes.jsonl 2>/dev/null | jq 'select(.resolved_at == null)'
```

## Procedure

### Step 1 — Validate the watermark

Read `.harmonik/cognition/state.json`:
- **Missing or unparseable:** this is a cold-start (CL-054). Record a `warning` note:
  ```bash
  note(kind="warning", refs=[], text="Cold-start: watermark missing or corrupt. Replaying events from JSONL floor with empty reacted-ledger.")
  ```
  Set effective watermark to the floor (`ScanAfter` from the beginning of events.jsonl).
- **Valid:** note the `last_processed_event_id`; events after this ID are the gap to process.

### Step 2 — Triage the queue

For each item in `.harmonik/queue.json`:

| Queue status | Action |
|---|---|
| `completed` | Verify two-phase done (Step 3). If done: no action. If not: flag for follow-up. |
| `dispatched` | Bead is in-flight. Do NOT re-dispatch. Watch events for its `run_completed`/`run_failed`. |
| `pending` | Bead is queued but not yet dispatched. No action; daemon will pick it up. |
| `failed` | Bead failed in the prior session. Apply `triage-failure` logic to determine re-dispatch eligibility. |

### Step 3 — Verify two-phase done for "completed" beads (CL-051)

For each `completed` queue item:
```bash
bead_id="<id>"
# Phase 1: terminal event
grep '"type":"run_completed"' .harmonik/events/events.jsonl \
  | grep '"success":true' | grep "\"bead_id\":\"$bead_id\"" | wc -l

# Phase 2: trailer on origin/main
git log origin/main --grep "Refs: $bead_id" --max-count=1 --oneline
```

- **Both conditions met:** bead is done. No action.
- **Event only (no trailer):** daemon's push may have failed. Record a `warning` note referencing the bead-id; route to Tier-2 reconciliation (do not act directly per CL-051).
- **Trailer only (no event):** phantom done. Record `loop_observed_phantom_done` via `note(kind="warning", ...)`. Route to Tier-2 reconciliation.

### Step 4 — Replay gap events

Scan events.jsonl from `last_processed_event_id` forward:

```bash
# Approximate: grep events after the watermark's approximate timestamp
# (exact replay uses the watermark's UUIDv7 monotonic sort)
```

For each event in the gap, apply the wake-filter (CL-061):
- `run_completed{success}`: verify two-phase done (Step 3).
- `run_failed`: apply triage logic. Count session-failures per bead.
- `reviewer_verdict{BLOCK}`: flag for investigation.
- `merge_conflict`: flag for investigation.
- Anything else: log and advance watermark.

Record a reaction ledger entry for each processed event (idempotency — CL-055).

### Step 5 — Check open notes

Review unresolved notes from `notes.jsonl`:
- `decision` notes referencing still-open beads: re-surface in the working session digest (no action).
- `warning` notes: re-read each; if the condition is resolved (bead closed, conflict gone), append a `decision` note marking it resolved.
- `defer` notes: check if the deferred condition is now met (bead two-phase-done → auto-resolve per CL-041).
- `hypothesis` notes: do not auto-resolve — require explicit observation.

### Step 6 — Build the startup inventory

After Steps 1–5, record a `decision` note summarizing the starting state:

```bash
note(kind="decision",
     refs=[],
     text="Startup reconciliation complete. In-flight: <N> beads [<ids>]. Queue-pending: <M>. Gap events processed: <K>. Phantom-done flags: <count>. Cold-start: <yes/no>.")
```

### Step 7 — Check for pause signals

```bash
ls .flywheel/PAUSE 2>/dev/null && echo "PAUSED" || echo "ok"
```

If `.flywheel/PAUSE` exists: do NOT dispatch. Record a `defer` note. Wait for operator to remove the file.

### Step 8 — Ready to dispatch

If no HIGH/CRITICAL escalations are pending and no PAUSE file exists:
- Re-arm the event bridge (harness does this automatically).
- Proceed to `compose-batch` for the first batch.

## Reconciliation categories

The daemon's reconciliation taxonomy has 6 categories (RC §4.4). The loop's role here:
- **Cat 1–4** (routine merge/close/push outcomes): loop observes via events + two-phase-done check (Steps 3–4); no direct action.
- **Cat 5** (phantom done): loop emits `loop_observed_phantom_done` warning note; routes to Tier-2 (CL-051). MUST NOT act directly.
- **Cat 6** (human escalation): loop records warning; reads `escalate` skill; waits for operator.

The loop MUST NOT acquire reconciliation locks (RC-002a) and MUST NOT modify queue.json or beads-intents/ (CL-050).

## Do NOT

- Dispatch any new beads before completing Steps 1–7.
- Close or update beads directly — daemon owns terminal transitions.
- Attempt to resolve Cat-5 phantom-done states directly — route to Tier-2 reconciliation.
- Skip the gap-event replay — without it, the session-fail-count for re-dispatch eligibility is wrong.
- Trust the queue's `completed` status alone as proof of done — always verify two-phase done (CL-051).
