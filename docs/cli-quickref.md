# Harmonik CLI Quick Reference

Lookup-only command card. Relocated from the retired AGENT_OPERATING_MANUAL.
Paths use `/Users/gb/github/harmonik`; substitute your project dir as needed.

## Command table

| Task | Command |
|---|---|
| Start daemon | `tmux new-session -d -s harmonik-daemon 'harmonik --project /Users/gb/github/harmonik --no-auto-pull --max-concurrent 4'` |
| Check daemon | `harmonik queue status` |
| Validate batch | `harmonik queue dry-run /tmp/batch.json` |
| Submit to named queue | `harmonik queue submit --queue main /tmp/batch.json` |
| Submit batch (default queue) | `harmonik queue submit /tmp/batch.json` |
| Append to stream | `harmonik queue append --queue-id <id> 0 hk-ccc` |
| Monitor | `harmonik subscribe --types run_completed,run_failed,run_stale,heartbeat --heartbeat 60s --json` |
| Change concurrency live | `harmonik queue set-concurrency N` |
| Pre-screen bead | `git -C /Users/gb/github/harmonik log --all --grep "Refs: hk-abc" --oneline` |
| Reopen wrongly-closed bead | `br update <id> --status=open` |
| Send comms message | `harmonik comms send --to <agent> -- <body>` |

## Queue submit JSON shape

`QueueSubmitRequest` (`kind: "stream"` for mid-flight appends). Full queue model: `specs/queue-model.md`.

```json
{
  "schema_version": 1,
  "groups": [{
    "group_index": 0,
    "kind": "stream",
    "status": "pending",
    "created_at": "2026-05-31T00:00:00Z",
    "items": [
      { "bead_id": "hk-aaa", "status": "pending" },
      { "bead_id": "hk-bbb", "status": "pending" }
    ]
  }]
}
```

```bash
harmonik queue dry-run /tmp/batch.json  # validate (reports ledger-dep deferrals)
harmonik queue submit  /tmp/batch.json  # accept; prints queue_id
harmonik queue append --queue-id <id> 0 hk-ccc hk-ddd  # mid-flight add to stream group
```

## Monitoring

Arm `harmonik subscribe` immediately after submit. It attaches to the running daemon and streams typed events via NDJSON; one subscribe sees all beads regardless of which agent submitted them. Re-arm if it hits the Monitor timeout.

```bash
harmonik subscribe --types run_completed,run_failed,run_stale,heartbeat --heartbeat 60s --json
```

Fallback (if subscribe is unavailable — there is no `daemon.log` and no per-run output file):

```bash
tail -F /Users/gb/github/harmonik/.harmonik/events/events.jsonl 2>/dev/null \
  | grep --line-buffered -E "run_completed|run_failed|run_stale|merge_conflict|reviewer_verdict"
```

## Comms bus

```bash
harmonik comms send --to <agent-name> -- <body>   # direct message
harmonik comms send --broadcast -- <body>          # to all online agents
harmonik comms recv --follow                       # drain backlog then stream live
harmonik comms who                                 # agents online in last ~120s
harmonik comms log --since 30m                     # operator view (doesn't advance cursor)
```

Dedupe on `event_id` — delivery is at-least-once. Full surface: `.claude/skills/agent-comms/SKILL.md`.
