# compose-batch — how to compose the next dispatch batch from `kerf next`

> L3b fat-skill. Fetch via `read_skill("compose-batch")` at the empty-queue boundary (CL-073)
> or when the harness delivers a "queue empty; compose next batch" turn prompt.

## When to invoke

- The harness delivers: "queue empty; compose next batch" in a turn prompt.
- You are in a proactive compositing cycle between batches.
- `kerf next` returns empty and ≥1 dispatch slot is free.

Do NOT invoke this skill in the eager-refill path — that path is harness-side pure code (CL-071).
This skill governs the **LLM-judgment** composition path (CL-073).

## Inputs

Before composing, gather:

```bash
# 1. Ranked candidate beads
kerf next --format=json --only=bead

# 2. In-flight runs (count toward max-concurrent ceiling)
cat .harmonik/queue.json | jq '[.items[] | select(.status == "dispatched")] | length'

# 3. Active goal focus (operator-defined)
cat .flywheel/goals.md

# 4. Open dispatch-log entries (overrides / skips already recorded)
tail -20 .harmonik/cognition/dispatch-log.jsonl 2>/dev/null
```

## Procedure

### Step 1 — Determine available slots

```
free_slots = max_concurrent - in_flight_count
```

If `free_slots ≤ 0`: no dispatch needed. Wait for a `run_completed` or `run_failed` event before re-invoking this skill.

### Step 2 — Apply pre-screen guards (CL-072, in order)

For each candidate bead from `kerf next`, apply these checks. Skip on first hit.

| Guard | Check | Action on hit |
|---|---|---|
| **Already in queue** | `cat .harmonik/queue.json \| jq '.items[] \| select(.bead_id == "<id>") \| .status'` shows `pending/dispatched/completed` | Skip; append skip to dispatch-log |
| **Already landed** | `git log origin/main --grep "Refs: <id>" --max-count=1` non-empty | Skip; file deferred close-stale intent via `note` |
| **Failed twice this session** | session-fail-count ≥ 2 (tracked in working memory or dispatch-log) | HALT batch composition; read `investigate-run` before continuing |
| **Conflict with in-flight** | Both this bead and an in-flight bead touch the same contractual file (best-effort; v0.1) | Skip; note the conflict; retry after in-flight completes |

For each skipped bead, append to the dispatch-log:
```bash
# Append one JSON line to .harmonik/cognition/dispatch-log.jsonl
echo '{"ts":<unix-ms>,"candidate_bead":"<id>","skipped_reason":"<guard-name>","picked_instead":null}' \
  >> .harmonik/cognition/dispatch-log.jsonl
```

### Step 3 — Check goal focus from `goals.md`

Read the **Active epics** section. If specific kerf work codenames are listed:
- Prefer beads whose kerf work matches the active epic.
- Deprioritize (but do not skip) beads from explicitly deferred works.

If no active epic is listed: use `kerf next` ranking as-is.

### Step 4 — Select the batch

After pre-screening, select up to `free_slots` beads from the top of the filtered `kerf next` output.

Do NOT exceed `max_concurrent` (daemon ceiling; CL-070). If `kerf next` returns fewer than `free_slots` candidates after guards: dispatch what's available; do not pad with speculative beads.

### Step 5 — Dispatch

```bash
harmonik run --beads <id1>,<id2>,... --notify-stream
```

Or append to a running stream queue:
```bash
harmonik queue append --queue-id <uuid> 0 <id1> <id2> ...
```

Record the dispatch:
```bash
for id in <id1> <id2> ...; do
  echo "{\"ts\":<unix-ms>,\"dispatched_bead\":\"$id\",\"skipped_reason\":null}" \
    >> .harmonik/cognition/dispatch-log.jsonl
done
```

### Step 6 — Record a decision note

```bash
note(kind="decision",
     refs=["<id1>", "<id2>", ...],
     text="Composed batch: [<id1>, <id2>] from kerf next. <id3> skipped (<reason>). free_slots=<N>.")
```

## Edge cases

### `kerf next` is empty

If `kerf next --format=json --only=bead` returns an empty list AND there are no in-flight runs:

Options (judge which applies):
1. **Wait:** beads may be in triage; re-check in 5 minutes. Record a `defer` note.
2. **Escalate:** no ready work and operator intent is unclear. Read `escalate.md`.
3. **File new beads:** if you can identify a clear next step from `goals.md` and the digest. Use `br create` and dispatch immediately.

### `kerf next` returns only failed-twice beads

HALT. Read `investigate-run.md` for each failed-twice bead before composing any new batch.

### Active epic's beads are all blocked

Check dependencies:
```bash
br show <bead_id>  # look for "blocks:" section
```

If a blocking bead is not in the ready set, compose a batch from a secondary epic or unblocked standalone beads. Record reasoning in a `decision` note.

## Do NOT

- Dispatch a bead that failed twice without investigation.
- Speculate about bead content to pad a batch — only dispatch what `kerf next` surfaces.
- Omit the dispatch-log append — it is the audit trail for repeat-dispatch detection.
- Wake the model for eager refill (slot freed by a completed run) — that is harness-side work (CL-071).
