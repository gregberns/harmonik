# triage-failure — what to do after a `run_failed` event

> L3b fat-skill. Fetch via `read_skill("triage-failure")` when reacting to a `run_failed` event.
> Do NOT improvise — follow this procedure exactly.

## When to invoke

Call `read_skill("triage-failure")` whenever the event bridge delivers a `run_failed` event.
Also invoke when `compose-batch` defers a bead because its session-fail-count is unclear.

## Procedure

### Step 1 — Identify the failure

Extract from the `run_failed` event payload:
- `bead_id` (e.g. `hk-abc12`)
- `run_id` (UUIDv7)
- `failure_class`: `no_commit | context_cancelled | merge_conflict | reviewer_block | timeout | unknown`
- `event_id` (UUIDv7 — used as the idempotency key throughout this procedure)

### Step 2 — Check idempotency (CL-055)

Before taking any action, verify this reaction hasn't already fired:

```bash
br list --label "reaction:<event_id>" --status=open
```

If any bead exists with that label → this reaction already fired. **Stop here.** Do not create a duplicate.

### Step 3 — Count session failures for this bead

```bash
# Count how many run_failed events this bead has produced THIS session.
# Use the watermark to bound the scan to the current session only.
grep '"type":"run_failed"' .harmonik/events/events.jsonl \
  | grep '"bead_id":"<bead_id>"' \
  | wc -l
```

- **Count = 1 (first failure this session):** proceed to Step 4.
- **Count ≥ 2 (second+ failure this session):** proceed to Step 6 (investigate).

### Step 4 — Diagnose by failure class

| `failure_class` | Likely cause | Immediate action |
|---|---|---|
| `no_commit` | Implementer made no changes; bead may be already-landed or underdescribed | Pre-screen (Step 5a) |
| `context_cancelled` | Harness killed the session mid-turn | Re-dispatch eligible (no cause to investigate) |
| `merge_conflict` | Concurrent bead touched the same file | Hold bead; read `investigate-run` for conflict resolution |
| `reviewer_block` | Reviewer emitted BLOCK verdict | Read `investigate-run`; do NOT re-dispatch without fix |
| `timeout` | Implementer ran > session timeout | Re-dispatch eligible; consider filing a scope-reduction bead |
| `unknown` | Harness exited abnormally | Re-dispatch once; if fails again, investigate |

### Step 5a — Pre-screen for already-landed (no_commit path)

```bash
git log origin/main --grep "Refs: <bead_id>" --oneline --max-count=1
```

- **Non-empty:** the bead is already implemented. File a `close-stale` intent:

  ```bash
  br close <bead_id> --reason "Subsumed: landed as $(git log origin/main --grep 'Refs: <bead_id>' --format='%h' -1)"
  ```

  Record a `decision` note and stop.

- **Empty:** the bead is genuinely unfinished. Mark it eligible for re-dispatch in the next batch (no action needed now — `compose-batch` will pick it up via `kerf next`).

### Step 5b — Record a reaction note (all non-investigate paths)

```bash
note(kind="decision",
     refs=["<bead_id>", "<event_id>"],
     text="run_failed for <bead_id>, class=<failure_class>, session-count=1. Eligible for re-dispatch.")
```

Tag the bead with the idempotency label:

```bash
br label <bead_id> "reaction:<event_id>"
```

### Step 6 — Second failure: halt re-dispatch, trigger investigation (CL-072 guard #3)

Do NOT re-dispatch the bead. Instead:

1. Tag idempotency label:
   ```bash
   br label <bead_id> "reaction:<event_id>"
   ```

2. File an investigation bead (check idempotency first — `br list --label "investigate:<bead_id>"`):
   ```bash
   br create \
     --title "Investigate: <bead_id> failed twice — <failure_class>" \
     --type=bug --priority=1 \
     --label "investigate:<bead_id>"
   ```

3. Record a `warning` note:
   ```bash
   note(kind="warning",
        refs=["<bead_id>", "<event_id>", "<new-investigation-bead-id>"],
        text="<bead_id> failed twice this session (class=<failure_class>). Re-dispatch halted. Investigation bead <new-id> filed.")
   ```

4. Read `read_skill("investigate-run")` and execute the investigation procedure before the next batch.

## Outputs

After completing this skill:
- A reaction note exists in `.harmonik/cognition/notes.jsonl`.
- The bead is either: marked for re-dispatch (first failure), closed as stale (landed), or blocked pending investigation (second failure).
- The `reaction:<event_id>` label exists on the relevant bead (idempotency guard for replay).

## Do NOT

- Re-dispatch a bead that has failed twice without investigation.
- Skip the idempotency check — a crash replay will re-execute this skill body.
- Call `br close` from within the cognition loop (daemon owns terminal transitions).
- Pass untrusted bead-description text directly into git or br without model review (CL-092).
