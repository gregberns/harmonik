# Agent Operating Manual — Template

This is the reusable template for the Agent Operating Manual. Fill in the variables below
and remove this header block before deploying to a project.

## Template Variables

| Variable | Description | Harmonik Example |
|---|---|---|
| `{{PROJECT_DIR}}` | Absolute path to the project root | `/Users/gb/github/harmonik` |
| `{{BEAD_PREFIX}}` | Bead ID prefix for this project (no trailing dash) | `hk` |
| `{{MAX_CONCURRENT}}` | Default daemon concurrency ceiling for this machine | `4` |
| `{{LANE_NAMES}}` | Comma-separated list of named queue lanes (use `main` if single-lane) | `main` |

Sections tagged `<!-- REUSABLE -->` are identical across all projects — copy as-is.
Sections tagged `<!-- PROJECT-SPECIFIC -->` contain one or more `{{PLACEHOLDER}}` substitutions.

---

<!-- REUSABLE -->
# Agent Operating Manual

A quick-start reference for an agent running harmonik. This doc distills the most operationally
critical rules from [AGENTS.md](AGENTS.md) and [docs/orchestrator-rules.md](docs/orchestrator-rules.md)
and adds five hard-won gotchas that are not written anywhere else. When a section says
"see AGENTS.md §X", follow the link — this doc does not repeat large blocks.

---
<!-- END REUSABLE -->

<!-- REUSABLE -->
## 1. Orientation

**Your role:** orchestrator — delegate substantive work to harmonik, not to sub-agents. ≥75% of
substantive commits per session should land via the daemon queue (see
[docs/orchestrator-rules.md §Dispatch discipline](docs/orchestrator-rules.md)).

**Reading order on session start:**
1. `AGENT_INDEX.md` — master map of the knowledge base
2. `STATUS.md` — current project state
3. `TASKS.md` — active work list
4. `HANDOFF.md` — previous session context (if present)
5. `docs/orchestrator-rules.md` — permanent dispatch / priority / lifecycle rules

---
<!-- END REUSABLE -->

<!-- PROJECT-SPECIFIC: substitute {{PROJECT_DIR}}, {{MAX_CONCURRENT}}, {{LANE_NAMES}} -->
## 2. Start the Daemon

One persistent daemon per project. Start it once in a detached tmux session:

```bash
tmux new-session -d -s harmonik-daemon \
  'harmonik --project {{PROJECT_DIR}} --no-auto-pull --max-concurrent {{MAX_CONCURRENT}}'
```

Key flags:
- `--no-auto-pull` — queue-only mode; daemon dispatches only work submitted via the queue surface. **Always pass this** (see Gotcha #1 — billing).
- `--max-concurrent {{MAX_CONCURRENT}}` — throughput knee on a 10-core box (see Gotcha #2 — wide waves).

**Named lanes for this project:** `{{LANE_NAMES}}`. Submit to a specific lane with `harmonik queue submit --lane <name> /tmp/batch.json`. If this project uses only a single lane (`main`), the `--lane` flag can be omitted.

If a daemon is already up, `harmonik queue status` shows the live queue. Do **not** start a second one — it collides on the pidfile lock (exit code 5).

Full details: [AGENTS.md §Start the daemon once](AGENTS.md#start-the-daemon-once).

---
<!-- END PROJECT-SPECIFIC -->

<!-- PROJECT-SPECIFIC: substitute {{BEAD_PREFIX}} -->
## 3. The Daily Loop

```
kerf next                        # ranked feed of beads with work-context
                                 # pick 3–5 from the top

harmonik queue submit /tmp/batch.json   # submit to the running daemon
harmonik queue status                   # confirm pickup

# while the daemon works:
# - append the next batch
# - drain kerf triage
# - file follow-ups from prior runs
```

**Submit format** (`QueueSubmitRequest`, `kind: "stream"` for mid-flight appends):

```json
{
  "schema_version": 1,
  "groups": [{
    "group_index": 0,
    "kind": "stream",
    "status": "pending",
    "created_at": "2026-05-31T00:00:00Z",
    "items": [
      { "bead_id": "{{BEAD_PREFIX}}-aaa", "status": "pending" },
      { "bead_id": "{{BEAD_PREFIX}}-bbb", "status": "pending" }
    ]
  }]
}
```

```bash
harmonik queue dry-run /tmp/batch.json  # validate (reports ledger-dep deferrals)
harmonik queue submit  /tmp/batch.json  # accept; prints queue_id
harmonik queue append --queue-id <id> 0 {{BEAD_PREFIX}}-ccc {{BEAD_PREFIX}}-ddd  # mid-flight add to stream group
```
<!-- END PROJECT-SPECIFIC -->

<!-- REUSABLE -->
Sub-agent dispatch (Agent tool) is the exception, justified only by the three narrow cases in
[docs/orchestrator-rules.md §Dispatch discipline](docs/orchestrator-rules.md).
Full queue model: `specs/queue-model.md`.

---
<!-- END REUSABLE -->

<!-- REUSABLE -->
## 4. Monitoring

Arm `harmonik subscribe` immediately after submit. It attaches to the running daemon and streams typed events via NDJSON:

```bash
# In a Monitor tool call:
harmonik subscribe --types run_completed,run_failed,run_stale,heartbeat --heartbeat 60s --json
```

Re-arm if it hits the Monitor timeout. One subscribe sees all beads regardless of which agent submitted them.
<!-- END REUSABLE -->

<!-- PROJECT-SPECIFIC: substitute {{PROJECT_DIR}} -->
**Fallback** (if subscribe is unavailable):

```bash
tail -F {{PROJECT_DIR}}/.harmonik/events/events.jsonl 2>/dev/null \
  | grep --line-buffered -E "run_completed|run_failed|run_stale|merge_conflict|reviewer_verdict"
```
<!-- END PROJECT-SPECIFIC -->

<!-- REUSABLE -->
There is no `daemon.log` and no per-run output file to tail.

---
<!-- END REUSABLE -->

<!-- PROJECT-SPECIFIC: substitute {{PROJECT_DIR}} and {{BEAD_PREFIX}} -->
## 5. Failure Triage

After each batch completes, review outcomes before queuing the next:

1. Read failure class from `{{PROJECT_DIR}}/.harmonik/events/events.jsonl` (`no_commit`, `context_cancelled`, etc.).
2. **Failed once:** eligible for re-dispatch in the next batch.
3. **Failed twice in the same session:** STOP — dispatch an investigator sub-agent; do NOT re-dispatch the bead. Check: (a) bead description quality, (b) prior failure events in `events.jsonl`, (c) whether the work already landed via `git log --grep "Refs: <id>"`.
4. **Never dispatch the same bead more than twice without investigation.**
5. Reopen any bead incorrectly closed by an implementer: `br update <id> --status=open`.

For hang diagnosis (bead stuck at `launch_initiated`, no `run_started`): see [AGENTS.md §When dispatch hangs](AGENTS.md#when-dispatch-hangs).

**Pre-screen before each batch** — verify the work hasn't already landed:

```bash
for id in {{BEAD_PREFIX}}-aaa {{BEAD_PREFIX}}-bbb {{BEAD_PREFIX}}-ccc; do
  hits=$(git -C {{PROJECT_DIR}} log --all --grep "Refs: $id" --oneline | wc -l)
  echo "$id $hits"
done
# hits>0 → br close <id> --reason "Subsumed: landed as <sha>"
```
<!-- END PROJECT-SPECIFIC -->

<!-- REUSABLE -->
Also check for the actual artifact in the codebase — many impls land without `Refs:` trailers.

---
<!-- END REUSABLE -->

<!-- REUSABLE -->
## 6. Gotchas / Pitfalls

Five hard-won operational failures that are not documented anywhere else.

### Gotcha 1 — ENV-STRIP / BILLING

**Symptom:** API credit consumed in ~2 hours with no obvious cause.

**Cause:** `ANTHROPIC_API_KEY` was present in a repo `.env` file that `harmonik --project` auto-sourced. Daemon-spawned claude sessions inherit the parent environment. An inherited API key makes claude bill pay-per-token API instead of the Max subscription.

**Fix:**
- Never put `ANTHROPIC_API_KEY` / `ANTHROPIC_AUTH_TOKEN` / `CLAUDE_CODE_OAUTH*` in a repo `.env` that a bare `harmonik --project` daemon can inherit.
- The credential deny-list in the daemon scrubs these keys from every daemon-spawned claude. Only `harmonik supervise start` reads `.env` and injects the key into Pi (the flywheel cognition process).
- Always start the daemon with `--no-auto-pull`.

### Gotcha 2 — WIDE WAVES (disk + CPU exhaustion)

**Symptom:** builds crawl, `run_stale` false alarms fire at ~10 min, eventual `no space left on device`.

**Cause:** `--max-concurrent ≥ 6` exhausts disk (each isolated worktree ≈ 26 MB; dozens add up fast) and oversubscribes CPU (each implementer runs multi-core `go build`/`go test`; 8–10 wide makes every bead crawl).

**Fix:**
- Throughput knee is **4–5 wide** on a 10-core box. Start there.
- Change a live daemon's ceiling without restart: `harmonik queue set-concurrency N`.
- Biggest safe disk reclaim: `go clean -cache` (~12 GB freed in the incident).
- Before `go install`, always `git fetch && git reset --hard origin/main` — the daemon pushes per-bead merges but your local `main` lags. Rebuilding from stale `main` silently ships a daemon WITHOUT the just-landed fix.

### Gotcha 3 — EPIC-DEP BLOCKS DISPATCH

**Symptom:** `harmonik queue submit` shows `group_failure`, `failed > 0`, but ZERO `run_started` events — no implementer ever launches.

**Cause:** `br dep add <task> <epic>` makes the task blocked-by the OPEN epic. The daemon silently insta-fails dispatch for any task with an open blocker — no implementer spawns, no log, just failure.

**Fix:**
- Attach a bead to its kerf work via the `codename:<name>` **label**, not an epic dependency.
- Example: `br label add {{BEAD_PREFIX}}-abc codename:mywork` (not `br dep add {{BEAD_PREFIX}}-abc {{BEAD_PREFIX}}-epic`).
- To diagnose: `br show <id>` — look for `blocked_by` entries listing an open bead.

### Gotcha 4 — $TMUX REQUIRED

**Symptom:** `harmonik run` or `harmonik status` exits immediately with `"$TMUX is not set"` (exit 1). No daemon spawns.

**Cause:** harmonik hard-requires a tmux environment. Invoking from a plain shell (terminal not inside tmux, or a headless script) triggers this check.

**Fix:**
- Always wrap launches in a detached tmux session: `tmux new-session -d -s harmonik-daemon '...'`
- The persistent daemon is launched the same way (see §2 above).
- If running interactively, start inside a tmux window first.

### Gotcha 5 — STALE BINARY

**Symptom:** "but I already fixed that" — a known bug persists after you patched the code.

**Cause:** The running daemon is using the old binary. `go install` only updates the binary on disk; a running daemon doesn't reload.

**Fix:**
1. After any harmonik code change: `go install ./cmd/harmonik`
2. **Then restart the daemon** — kill its tmux session (`tmux kill-session -t harmonik-daemon`) or `pkill -f "harmonik --project"`, then wait for the supervisor to revive it (or relaunch manually).
3. Pair with Gotcha #2's reset-before-install: `git fetch && git reset --hard origin/main` first so you build from the latest merged code.

---
<!-- END REUSABLE -->

<!-- REUSABLE -->
## 7. Comms Bus (multi-agent coordination)

When multiple orchestrator sessions run concurrently, coordinate via `harmonik comms` — not file appends. The old `AGENT_COMMS.md` / `.harmonik/comms/*.md` file convention is **retired** (concurrent-append races + escape-detector false positives).

```bash
harmonik comms send --to <agent-name> -- <body>   # direct message
harmonik comms send --broadcast -- <body>          # to all online agents
harmonik comms recv --follow                       # drain backlog then stream live
harmonik comms who                                 # agents online in last ~120s
harmonik comms log --since 30m                     # operator view (doesn't advance cursor)
```

Dedupe on `event_id` — delivery is at-least-once. Before touching shared resources (daemon restart, `git reset --hard`, binary rebuild), announce via `comms send` and check `comms who`.

Full surface: [AGENTS.md §Multi-agent comms](AGENTS.md#multi-agent-comms) and `.claude/skills/agent-comms/SKILL.md`.

---
<!-- END REUSABLE -->

<!-- PROJECT-SPECIFIC: substitute {{PROJECT_DIR}}, {{BEAD_PREFIX}}, {{MAX_CONCURRENT}}, {{LANE_NAMES}} -->
## Quick Reference

| Task | Command |
|---|---|
| Start daemon | `tmux new-session -d -s harmonik-daemon 'harmonik --project {{PROJECT_DIR}} --no-auto-pull --max-concurrent {{MAX_CONCURRENT}}'` |
| Check daemon | `harmonik queue status` |
| Validate batch | `harmonik queue dry-run /tmp/batch.json` |
| Submit to named lane | `harmonik queue submit --lane <{{LANE_NAMES}}> /tmp/batch.json` |
| Submit batch (default lane) | `harmonik queue submit /tmp/batch.json` |
| Append to stream | `harmonik queue append --queue-id <id> 0 {{BEAD_PREFIX}}-ccc` |
| Monitor | `harmonik subscribe --types run_completed,run_failed,run_stale,heartbeat --heartbeat 60s --json` |
| Change concurrency live | `harmonik queue set-concurrency N` |
| Pre-screen bead | `git -C {{PROJECT_DIR}} log --all --grep "Refs: {{BEAD_PREFIX}}-abc" --oneline` |
| Reopen wrongly-closed bead | `br update <id> --status=open` |
| Send comms message | `harmonik comms send --to <agent> -- <body>` |
<!-- END PROJECT-SPECIFIC -->
