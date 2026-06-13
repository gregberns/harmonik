# Harmonik — Agent Instructions

> Cross-project working-style rules (keep moving, delegate, plain English, compact, review gate) live in `~/.claude/CLAUDE.md`. This file adds harmonik-specific bits on top of those.

## Start here

Read [AGENT_INDEX.md](AGENT_INDEX.md) first. It is the master map of the knowledge base. Then read [STATUS.md](STATUS.md) for current project state and [TASKS.md](TASKS.md) for the active work list.

**Orchestrator permanent directives:** stable rules (dispatch discipline, priority, bead lifecycle, autonomy, dispatch shape, monitor pattern) are encoded in this document. Load alongside `HANDOFF.md` on every `/session-resume`.

## Orchestrator discipline (HARD RULE)

The orchestrator MUST NOT do inline code reading, investigation, or debugging on the main thread. Every session the main thread exists to dispatch — not to be an implementer or investigator.

When a batch fails or a bug surfaces:
1. File a bead if one doesn't exist (`br create --title="..." --type=bug --priority=1`).
2. Dispatch a sub-agent to investigate — anchor it to **durable artifacts** (file paths, line numbers, events.jsonl entries), NOT ephemeral state (tmux pane contents, live process output).
3. Keep the main thread dispatching other work while the investigator runs.

**Investigation dispatch template:** "Start with `<file>:<line>`, read the code and comments there, then check `<specific durable artifact>`. Report root cause in under 200 words."

The main-thread context window is precious — protect it. Inline investigation is the #1 cause of context exhaustion.

## On batch failure

When a submitted batch returns failures (a group reaches complete-with-failures, or `harmonik subscribe` reports `run_failed`):
1. Read the failure class from `.harmonik/events/events.jsonl` (`no_commit`, `context_cancelled`, etc.).
2. If the **same bead failed twice** this session → dispatch an investigator sub-agent; do NOT re-dispatch the bead.
3. If a **new failure class** → file a bead, dispatch an investigator.
4. Never re-dispatch a bead more than twice without investigation.
5. Reopen any beads incorrectly closed by implementers (`br update <id> --status=open`).

## Daily loop (canonical)

**One persistent daemon per project is the dispatcher; agents dispatch by submitting beads to its queue.** harmonik enforces a single daemon per project via the pidfile lock. Multiple agents/orchestrators can run concurrently, but they MUST share that one daemon: it dispatches up to N beads concurrently in isolated worktrees, merges to `$TARGET_BRANCH` **one-at-a-time** (so there are no merge races), and **auto-skips** any bead whose merge conflicts. That shared-queue handoff IS the multi-agent coordination mechanism — there is no manual agent-wrangling to do.

### Start the daemon once

Start a single background daemon in a detached tmux session, queue-only:

```bash
tmux new-session -d -s harmonik-daemon \
  'harmonik --project $PROJECT_DIR --no-auto-pull --max-concurrent N'
```

- `--no-auto-pull` = **queue-only**: the daemon dispatches only work that arrives via the queue surface; it will NOT auto-drain `br ready`.
- `--max-concurrent N` sets the concurrent-dispatch ceiling for the whole daemon. Use the disk/CPU knee (~4–5 wide on a 10-core box) — wider oversubscribes cores and can exhaust disk.
- If a daemon is already up, `harmonik queue status` returns the live queue; do NOT start a second one (it will collide on the pidfile lock and exit code 5).

### The loop

1. `kerf next` — surface the prioritized work (ranked feed of beads with work-context).
2. Pick a batch of beads from the top of the feed.
3. **Submit them to the running daemon's queue** (see "Submitting work" below). The daemon spawns claude per bead, watches for completion, commits, merges to `$TARGET_BRANCH` one-at-a-time, pushes, and closes each bead. Review-loop is **on by default**.
4. While the daemon works: queue the next batch (append to the same queue), drain `kerf triage` untriaged items, file follow-ups from prior runs, review recently-merged commits.
5. As groups complete: review outcomes, submit/append the next batch.

**Sub-agent dispatch (via the Agent tool) is the EXCEPTION**, justified only when (a) you're fixing the orchestration stack itself in a code path that breaks dispatch, or (b) the change is ≤2 lines of typo/cross-reference cleanup.

Target: ≥75% of substantive commits per session land through the daemon queue. If sub-agent dispatch is creeping above 25%, stop and audit why — that's a signal harmonik has new friction or you've drifted into batch-mind.

### `harmonik run` is the legacy / solo-bootstrap path

`harmonik run --beads ...` is NOT the canonical dispatcher. If a daemon is already up, `harmonik run` submits its beads to that daemon's queue as a stream group. If no daemon is running, `harmonik run` becomes the inline daemon for the duration of its beads. For all ongoing multi-agent work, run the persistent daemon above and submit to its queue.

### Submitting work

**Primary path — submit bead IDs directly with `--beads` (no hand-authored JSON):**

```bash
harmonik queue dry-run --beads hk-a,hk-b   # validate without persisting
harmonik queue submit  --beads hk-a,hk-b   # accept; prints the daemon-minted queue_id
harmonik queue status                      # inspect the live queue
```

**For multi-group or wave submits, hand-author a `QueueSubmitRequest` JSON:**

```json
{
  "schema_version": 1,
  "groups": [
    {
      "group_index": 0,
      "kind": "stream",
      "status": "pending",
      "created_at": "2026-01-01T00:00:00Z",
      "items": [
        { "bead_id": "hk-aaa", "status": "pending" },
        { "bead_id": "hk-bbb", "status": "pending" }
      ]
    }
  ]
}
```

```bash
harmonik queue dry-run /tmp/batch.json
harmonik queue submit  /tmp/batch.json
```

- `kind: "stream"` groups accept mid-flight appends; `kind: "wave"` groups are immutable after submit. Use `stream` for the daily loop.
- **Mid-flight adds:** `harmonik queue append [--queue-id <uuid>] <group-index> <bead-id ...>`. Streams accept appends while pending or active.
- Exit code 17 from any `queue` verb means the daemon is not running — start it first.

## Monitoring the daemon

Each agent is a Claude Code session that submits to the daemon and then needs per-bead progress, hangs, and daemon failures surfaced back to it. Submitting a batch returns only the `queue_id` — it does NOT block until the work finishes. So after submitting, run a **Claude Code Monitor** alongside.

### Canonical pattern

Use `harmonik subscribe` — one process, NDJSON to stdout, server-side heartbeat so the agent wakes periodically even if the daemon goes quiet:

```bash
# In a Monitor tool call:
harmonik subscribe --types run_completed,run_failed,run_stale,heartbeat --heartbeat 60s --json
```

`subscribe` attaches to the running daemon, so one Monitor sees every bead the daemon dispatches regardless of which agent submitted it. Re-arm if it hits the Monitor timeout.

**Events-tail fallback** (use only if `harmonik subscribe` is unavailable):

```bash
# In a Monitor tool call (timeout_ms = 3600000, persistent = false):
tail -F $PROJECT_DIR/.harmonik/events/events.jsonl 2>/dev/null \
  | grep --line-buffered -E "run_completed|run_failed|run_stale|merge_conflict|reviewer_verdict"
```

`.harmonik/events/events.jsonl` is where the daemon writes typed events. Each matched line becomes a notification.

### CWD discipline — never `cd` into a worktree

The daemon may `git worktree remove` a worktree out from under you on bead completion. Always operate from the repo root via absolute paths: `git -C $PROJECT_DIR <cmd>` rather than `cd`. The orchestrator's CWD must remain `$PROJECT_DIR` for the whole session.

### Pre-screen beads for already-landed work

Beads can be stale-open — implementation landed on `$TARGET_BRANCH` but the bead never got closed. Dispatching one wastes a slot. Before dispatching, grep history:

```bash
for id in hk-aaa hk-bbb hk-ccc; do
  hits=$(git -C $PROJECT_DIR log --all --grep "Refs: $id" --oneline | wc -l)
  echo "$id $hits"
done
# any id with hits>0 → br close <id> --reason "Subsumed: landed as <sha>"
```

### Queue semantics — `wave` vs `stream`

Each group in a submitted queue has a `kind`. **Use `kind: "stream"` for the daily loop** — stream groups accept mid-flight appends via `harmonik queue append`. `kind: "wave"` groups are immutable after submit (no appends), but dispatch their whole set concurrently up to the daemon's `--max-concurrent`. Stream-mode dispatches items in order; reach for a `wave` group when you need true concurrent dispatch of a fixed set with `--max-concurrent > 1`.

### Appending to a running queue

Mid-flight appends to a **stream** group: `harmonik queue append [--queue-id <uuid>] <group-index> <bead-id ...>`. Get the `queue_id` from the `submit` response or `harmonik queue status`.

**Wave groups do NOT accept appends.** Wait for the wave to drain, then submit a new group.

### Pre-flight checklist

1. Rebuild harmonik first (`go install ./cmd/harmonik`) — stale binary is the #1 cause of "but I fixed that". Restart the daemon after rebuilding.
2. Confirm the daemon is up: `harmonik queue status` (exit 17 = not running → start it per "Start the daemon once"). Never start a second daemon.
3. Pre-screen the batch; drop already-landed beads.
4. Build the `QueueSubmitRequest` JSON or use `--beads`; `harmonik queue dry-run` to validate, then `harmonik queue submit`.
5. Arm a Monitor running `harmonik subscribe`.

### Post-flight: failure triage

After each batch completes, review outcomes before queuing the next batch:

- **Failed once:** eligible for re-dispatch in the next batch.
- **Failed twice in the same session:** STOP. Dispatch an investigator sub-agent before any further re-dispatch.
- **Never dispatch the same bead more than twice without investigation.**

### When dispatch hangs

For hangs:

1. Identify the stuck `run_id` from `.harmonik/queue.json` or the worktree listing.
2. `git -C $PROJECT_DIR/.harmonik/worktrees/<run_id> log --oneline -3` — if a `Refs:` commit exists, work was done; daemon is stuck on the next step (merge, reviewer, push).
3. Tail `.harmonik/events/events.jsonl` filtered by `run_id` — which event types fired? Which expected ones did not?
4. If implementer claude has already exited but the daemon is hung: kill the harmonik daemon PID, ff-merge the worktree branch by hand, push, close bead. Re-start the daemon.

## Multi-agent comms

When multiple orchestrator sessions run concurrently, use **`harmonik comms`** (the bus) to coordinate.

### Send a message to another agent

```bash
harmonik comms send --to <agent-name> -- <body>
harmonik comms send --broadcast -- <body>        # to all online agents
```

### Receive messages

```bash
harmonik comms recv --follow                     # drain backlog then stream live
harmonik comms recv --follow --json              # NDJSON output (safe to parse)
```

**Dedupe on `event_id`** — delivery is at-least-once. See `$PROJECT_DIR/.claude/skills/agent-comms/SKILL.md` (PROJECT-LOCAL skill, NOT `~/.claude/skills/agent-comms/`) for the full CLI surface and the dedup requirement.

### See who is online

```bash
harmonik comms who        # agents online within the last ~120s
harmonik comms who --json
```

### Pre-coordination (shared resources)

Before touching shared resources (daemon restart, `git reset --hard`, binary rebuild), announce via `comms send` and check `comms who`.

### Operator log view (no daemon needed)

```bash
harmonik comms log --since 30m               # all agent_message events last 30m
harmonik comms log --from orchestrator --json
```

## Planning with kerf

This project uses kerf for structured planning. The project is **spec-first**: the spec describes how the system operates, and code is updated to match the spec.

Before implementing non-trivial changes (new subsystems, refactors that cross subsystem boundaries, changes to cross-cutting contracts), create a kerf work:

    kerf new <codename>

### Key commands

    kerf new <codename>              Create a new work
    kerf show <codename>             See current state + jig instructions for next steps
    kerf status <codename>           Check current status
    kerf status <codename> <status>  Advance to next pass
    kerf shelve <codename>           Save progress when ending a session
    kerf resume <codename>           Pick up where you left off
    kerf square <codename>           Verify the work is complete
    kerf finalize <codename> --branch <name>  Package for implementation

### Queue + work-attachment surface

    kerf next                        Ranked feed of bead IDs ready to dispatch
    kerf triage                      Drift report (suggested bead reattachments, stale links)
    kerf triage --ack                Advance kerf's baseline after acting on the report
    kerf pin <bead> <work>           Attach a bead to a kerf work
    kerf work edit <codename>        Edit a work's bead-attachment config (bead_filter etc.)
    kerf map                         Works grouped by area
    kerf areas                       Manage areas (list/add/edit)

### Agent loop pattern (informal)

`kerf next` returns ranked bead IDs → orchestrator dispatches them via harmonik → on completion, `br close <id>` is invoked → `kerf triage --ack` advances kerf's baseline.

### When to use kerf

- New subsystems, cross-cutting spec changes → `kerf new --jig spec`
- Non-trivial feature plans → `kerf new --jig plan`
- Bug investigations → `kerf new --jig bug`
- Trivial changes (typos, one-line fixes) → skip kerf

### Workflow

1. `kerf new <codename>` — read the output, it tells you exactly what to do
2. Follow each pass: write the artifacts, advance status
3. `kerf show <codename>` — if you lose context, this shows where you are
4. `kerf shelve` / `kerf resume` — for multi-session work
5. `kerf square` — verify everything is complete
6. `kerf finalize` — package into a git branch for implementation

Don't skip the planning process. Measure twice, cut once.

## Key conventions

- **Specs live in `specs/`** at the repo root. These are normative: the spec is always right, and code is expected to match it.
- **Kerf process artifacts** (problem space, research, design, drafts, tasks, reviews) live on the **global bench** at `~/.kerf/projects/{id}/{codename}/`, NOT under the repo's `.kerf/`.
- **Knowledge base docs** (`docs/`) capture problems, goals, concepts, components, subsystems, ideas, and the collaboration log.
- **Bead label convention for kerf work codenames:** use the `codename:<name>` prefix (e.g. `codename:my-feature`). Functional/topical labels remain bare.

## Don't

- Don't reopen locked-in decisions without explicit user request.
- Don't add abstraction layers the user hasn't asked for.
- Don't skip the AGENT_INDEX → STATUS → TASKS reading order when picking up the project.

---

## Beads Workflow Integration

This project uses [beads_rust](https://github.com/Dicklesworthstone/beads_rust) (`br`) for issue tracking and kerf for prioritization and triage. Issues are stored in `.beads/` and tracked in git.

### Prioritization: use kerf, not bv

**`kerf next` is the single entry point for "what to work on."** It returns a ranked feed of beads with work-context. `kerf triage` handles drift detection.

```bash
kerf next                        # Ranked feed: top item is what to do next
kerf next --format=json          # Machine-readable output
kerf next --only=bead            # Only bead items (skip cleanup/warnings)
kerf triage                      # Drift report: untriaged, multi-matched, external drift
kerf triage --ack                # Advance baseline after acting on the report
kerf map                         # Works grouped by area
```

`bv` (beads_viewer) is only useful for graph-metric analysis (`--robot-insights` for PageRank/betweenness) or dependency graph export (`--robot-graph`). **CRITICAL: Use ONLY --robot-* flags with bv. Bare bv launches an interactive TUI that blocks your session.**

### br Commands for Issue Management

```bash
br ready              # Show issues ready to work (no blockers)
br list --status=open # All open issues
br show <id>          # Full issue details with dependencies
br create --title="..." --type=task --priority=2
br update <id> --status=in_progress
br close <id> --reason="Completed"
br close <id1> <id2>  # Close multiple issues at once
br sync --flush-only  # Export DB to JSONL
```

### Workflow Pattern

1. **Triage**: Run `kerf next` to find the highest-impact actionable work
2. **Claim**: Use `br update <id> --status=in_progress`
3. **Work**: Implement the task
4. **Complete**: Use `br close <id>`
5. **Sync**: Always run `br sync --flush-only` at session end

### Key Concepts

- **Dependencies**: Issues can block other issues. `br ready` shows only unblocked work.
- **Priority**: P0=critical, P1=high, P2=medium, P3=low, P4=backlog (use numbers 0-4, not words)
- **Types**: task, bug, feature, epic, chore, docs, question
- **Blocking**: `br dep add <issue> <depends-on>` to add dependencies

### Session Protocol

```bash
git status              # Check what changed
git add <files>         # Stage code changes
br sync --flush-only    # Export beads changes to JSONL
git commit -m "..."     # Commit everything
git push                # Push to remote
```
