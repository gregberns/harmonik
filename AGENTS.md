# Harmonik — Agent Instructions

> `CLAUDE.md` is a symlink to this file (`AGENTS.md`). They are the same content — edits to `AGENTS.md` cover both.

> Cross-project working-style rules (keep moving, delegate, plain English, compact, review gate) live in `~/.claude/CLAUDE.md`. This file adds harmonik-specific bits on top of those.

## Start here

Read [AGENT_INDEX.md](AGENT_INDEX.md) first. It is the master map of the knowledge base and every document is reachable from there within two hops. Then read [STATUS.md](STATUS.md) for current project state and [TASKS.md](TASKS.md) for the active work list.

**Orchestrator permanent directives:** [`docs/orchestrator-rules.md`](docs/orchestrator-rules.md) — all stable rules (dispatch discipline, priority, bead lifecycle, autonomy, dispatch shape, monitor pattern). Load alongside HANDOFF.md on every `/session-resume`. **Known workarounds** (worktree bugs, harness quirks): [`docs/known-workarounds.md`](docs/known-workarounds.md).

## Orchestrator discipline (HARD RULE)

The orchestrator MUST NOT do inline code reading, investigation, or debugging on the main thread. Every session the main thread exists to dispatch — not to be an implementer or investigator.

When a batch fails or a bug surfaces:
1. File a bead if one doesn't exist (`br create --title="..." --type=bug --priority=1`).
2. Dispatch a sub-agent to investigate — anchor it to **durable artifacts** (file paths, line numbers, events.jsonl entries), NOT ephemeral state (tmux pane contents, live process output).
3. Keep the main thread dispatching other work while the investigator runs.

**Investigation dispatch template:** "Start with `<file>:<line>`, read the code and comments there, then check `<specific durable artifact>`. Report root cause in under 200 words."

The main-thread context window is precious — protect it. Inline investigation is the #1 cause of context exhaustion (v60: ~30% of context wasted on inline reads).

## On batch failure

When a submitted batch returns failures (a group reaches complete-with-failures, or `harmonik subscribe` reports `run_failed`):
1. Read the failure class from `.harmonik/events/events.jsonl` (`no_commit`, `context_cancelled`, etc.).
2. If the **same bead failed twice** this session → dispatch an investigator sub-agent; do NOT re-dispatch the bead.
3. If a **new failure class** → file a bead, dispatch an investigator.
4. Never re-dispatch a bead more than twice without investigation.
5. Reopen any beads incorrectly closed by implementers (`br update <id> --status=open`).

## Daily loop (canonical)

**One persistent daemon per project is the dispatcher; agents dispatch by submitting beads to its queue.** harmonik now enforces a single daemon per project via the pidfile lock (hk-li14r — single-flywheel-per-project supervise lock, landed 2026-05-31). Multiple agents/orchestrators can run concurrently, but they MUST share that one daemon: it dispatches up to N beads concurrently in isolated worktrees, merges to main **one-at-a-time** (so there are no merge races), and **auto-skips** any bead whose merge conflicts. That shared-queue handoff IS the multi-agent coordination mechanism — there is no manual agent-wrangling to do.

### Start the daemon once

Start a single background daemon in a detached tmux session, queue-only:

```bash
tmux new-session -d -s harmonik-daemon \
  'harmonik --project /Users/gb/github/harmonik --no-auto-pull --max-concurrent N'
```

- `--no-auto-pull` = **queue-only**: the daemon dispatches only work that arrives via the queue surface; it will NOT auto-drain `br ready`. This is the safe default after the 2026-05-30 credit-burn incident (auto-pull billed the API pool, not the subscription). Today it must be passed explicitly; **hk-8vy18** will flip `--no-auto-pull` to the default (with `--auto-pull` as the opt-in).
- `--max-concurrent N` sets the concurrent-dispatch ceiling for the whole daemon. Use the disk/CPU knee (~4–5 wide on a 10-core box) — wider oversubscribes cores and can exhaust disk.
- If a daemon is already up, `harmonik queue status` returns the live queue; do NOT start a second one (it will collide on the pidfile lock and exit code 5).

### Work-project deployment

For repos where harmonik must **never auto-push `main`** (e.g. a product repo with branch-protection rules), use the integration-branch flags:

- `--target-branch <branch>` — daemon merges and pushes here instead of `main`.
- `--protect-branch <branch>` — deny-list; daemon fail-closes any run that would push this branch.
- `--forbid-default-main` — refuse to start if the resolved target branch is `main`.

All three are enforced fail-closed at boot, at each dispatch, and during merge.

#### config/branching.yaml template

Create `config/branching.yaml` at the repo root so agents pick up the config without flags:

```yaml
# config/branching.yaml — work-project deployment
protect_branches:
  - main
target_branch: integration
```

The daemon reads this file on startup; CLI flags override it.

#### Migration steps

1. **Create the integration branch** off `main`:
   ```bash
   git checkout -b integration main
   git push -u origin integration
   ```
2. **Configure branching** — add `config/branching.yaml` (above), or pass the flags directly:
   ```bash
   harmonik --project /path/to/repo \
     --target-branch integration \
     --protect-branch main \
     --forbid-default-main \
     --no-auto-pull --max-concurrent N
   ```
3. **Restart the daemon** so it picks up the new config/binary:
   ```bash
   pkill -f "harmonik --project /path/to/repo" || true
   # re-launch via hk-keeper.sh (set HK_TARGET_BRANCH / HK_PROTECT_BRANCH) or the tmux command above
   ```

#### integration → main is a human step

The daemon **never** auto-merges `integration` into `main`. Once a sprint of beads lands on `integration`, open a PR from `integration` → `main` for human review and merge.

> **Coming:** `harmonik promote` (hk-gax8v) will automate opening that PR.

### The loop

1. `kerf next` — surface the prioritized work (ranked feed of beads with work-context).
2. Pick a batch of beads from the top of the feed (skip the untested-workload classes documented in `HANDOFF.md` §"Three caveats" until the probes land).
3. **Submit them to the running daemon's queue** (see "Submitting work" below). The daemon spawns claude per bead, watches for completion, commits, merges to main one-at-a-time, pushes, and closes each bead. Review-loop is **on by default** (hk-g0ckv).
4. While the daemon works: queue the next batch (append to the same queue), drain `kerf triage` untriaged items, file follow-ups from prior runs, review recently-merged commits.
5. As groups complete: review outcomes, submit/append the next batch.

**Sub-agent dispatch (via the Agent tool) is the EXCEPTION**, justified only when (a) you're fixing harmonik itself in a code path that breaks dispatch, (b) the change is ≤2 lines of typo/cross-reference cleanup, or (c) the work touches an untested workload class (see `docs/kerf-feedback/2026-05-19-phase2-readiness-audit.md`).

Target: ≥75% of substantive commits per session land through the daemon queue. If sub-agent dispatch is creeping above 25%, stop and audit why — that's a signal harmonik has new friction or you've drifted into batch-mind.

Full design: `docs/orchestration-protocol-v2.md`. The kerf workflow below remains the planning surface for non-trivial NEW work (spec drafts, plan epics); kerf hands off beads, the daemon executes them.

### `harmonik run` is the legacy / solo-bootstrap path

`harmonik run --beads ...` does **not** submit to an existing daemon — it *becomes* the daemon for the duration of its beads, then exits when they finish. If a daemon is already up it collides on the pidfile lock and **exits code 5** (`pidfile locked`). So:

- Use `harmonik run --beads ...` ONLY to bootstrap a one-shot solo batch when no daemon is running and you don't want a persistent one.
- For all ongoing multi-agent work, run the persistent daemon above and submit to its queue.
- In-flight gap: **hk-b3wqd** will make `harmonik run` submit-to-an-existing-daemon (instead of exit-5) so the two paths interoperate. Until it lands, treat `harmonik run` and "persistent daemon + submit" as mutually exclusive within a project.

### Submitting work

`harmonik queue submit <queue-file>` takes a JSON file shaped as a `QueueSubmitRequest` (struct: `internal/queue/types.go` — `QueueSubmitRequest` / `Group` / `Item`):

```json
{
  "schema_version": 1,
  "groups": [
    {
      "group_index": 0,
      "kind": "stream",
      "status": "pending",
      "created_at": "2026-05-31T00:00:00Z",
      "items": [
        { "bead_id": "hk-aaa", "status": "pending" },
        { "bead_id": "hk-bbb", "status": "pending" }
      ]
    }
  ]
}
```

```bash
harmonik queue dry-run /tmp/batch.json     # validate without persisting (reports ledger-dep deferrals)
harmonik queue submit  /tmp/batch.json     # accept; prints the daemon-minted queue_id
harmonik queue status                      # inspect the live queue (returns the queue envelope)
```

- `kind: "stream"` groups accept mid-flight appends; `kind: "wave"` groups are immutable after submit. Use `stream` for the daily loop.
- **Mid-flight adds:** `harmonik queue append [--queue-id <uuid>] <group-index> <bead-id ...>` — e.g. `harmonik queue append --queue-id <uuid> 0 hk-ccc hk-ddd`. Streams accept appends while pending or active.
- Exit code 17 from any `queue` verb means the daemon is not running — start it first.
- In-flight gaps: **hk-m9a7g** will let `submit` / `dry-run` accept `--beads hk-a,hk-b` directly so you no longer hand-author the JSON file; **hk-24xn1** — the daemon doesn't wake on submit/append when idle, so newly-submitted beads sit `pending` until the next workloop tick (it advances on its own; just don't expect instant pickup).

## Monitoring the daemon

Each agent is a Claude Code session that submits to the daemon and then needs per-bead progress, hangs, and daemon failures surfaced back to it. Submitting a batch returns only the `queue_id` — it does NOT block until the work finishes. So after submitting, run a **Claude Code Monitor** alongside; without one you are blind from submit to group-completion.

### Canonical pattern

Use `harmonik subscribe` (hk-6ynv4, landed) — one process, NDJSON to stdout, server-side heartbeat so the agent wakes periodically even if the daemon goes quiet:

```bash
# In a Monitor tool call:
harmonik subscribe --types run_completed,run_failed,run_stale,heartbeat --heartbeat 60s --json
```

`subscribe` attaches to the running daemon, so one Monitor sees every bead the daemon dispatches regardless of which agent submitted it. Re-arm if it hits the Monitor timeout.

**DEPRECATED — Events-tail fallback** (use only if `harmonik subscribe` is unavailable): tail the daemon's typed event log directly. There is no `daemon.log` file and no per-run output file to tail — `--notify-stream` belonged to the foreground `harmonik run` path, which the persistent daemon does not use.

```bash
# In a Monitor tool call (timeout_ms = 3600000, persistent = false):
tail -F /Users/gb/github/harmonik/.harmonik/events/events.jsonl 2>/dev/null \
  | grep --line-buffered -E "run_completed|run_failed|run_stale|merge_conflict|reviewer_verdict"
```

`.harmonik/events/events.jsonl` is where the daemon writes typed events (`run_started`, `run_completed`, `run_failed`, `reviewer_verdict`, etc.). Each matched line becomes a notification. The Monitor exits when the tail does (e.g., daemon dies). Re-arm if it hits the 1-hour timeout.

### CWD discipline — never `cd` into a worktree

The daemon may `git worktree remove` a worktree out from under you on bead completion. Always operate from the repo root via absolute paths: `git -C /Users/gb/github/harmonik <cmd>` rather than `cd`. The orchestrator's CWD must remain `/Users/gb/github/harmonik` for the whole session.

### Pre-screen beads for already-landed work

Beads can be stale-open — implementation landed on `main` but the bead never got closed. Dispatching one wastes a slot (hits the noChange path). Before dispatching, grep history:

```bash
for id in hk-aaa hk-bbb hk-ccc; do
  hits=$(git -C /Users/gb/github/harmonik log --all --grep "Refs: $id" --oneline | wc -l)
  echo "$id $hits"
done
# any id with hits>0 → br close <id> --reason "Subsumed: landed as <sha>"
```

(Gap filed as hk-lhv8i to do this at submit-time inside the daemon.)

### Queue semantics — `wave` vs `stream`

Each group in a submitted queue has a `kind`. **Use `kind: "stream"` for the daily loop** — stream groups accept mid-flight appends via `harmonik queue append [--queue-id <uuid>] <group-index> <bead-id ...>`. `kind: "wave"` groups are immutable after submit (no appends), but dispatch their whole set concurrently up to the daemon's `--max-concurrent`. Stream-mode enforces head-of-line blocking via `streamEligible()`, so a stream group dispatches its items in order; reach for a `wave` group when you need true concurrent dispatch of a fixed set with `--max-concurrent > 1`. Remaining gap: hk-24xn1 — the daemon doesn't wake on submit/append when idle, so newly-added beads sit `pending` until the next workloop tick.

### Appending to a running queue

Mid-flight appends to a **stream** group: `harmonik queue append [--queue-id <uuid>] <group-index> <bead-id ...>` (e.g. `harmonik queue append --queue-id <uuid> 0 hk-ccc hk-ddd`). Streams accept appends while pending or active. Get the `queue_id` from the `submit` response or `harmonik queue status`. Remaining gap: hk-24xn1 — appended beads sit `pending` until the next workloop tick.

**Wave groups do NOT accept appends.** Wait for the wave to drain, then `harmonik queue submit` a new group (or submit a fresh stream group up front).

The `extqueue` kerf work (status=ready) covers the full spec for this surface. See `specs/queue-model.md` for the normative wave/stream/append contract.

### Pre-flight checklist

1. Rebuild harmonik first (`go install ./cmd/harmonik`) — stale binary is the #1 cause of "but I fixed that". Restart the daemon after rebuilding (kill the tmux session, re-launch) so it runs the new binary.
2. Confirm the daemon is up: `harmonik queue status` (exit 17 = not running → start it per "Start the daemon once"). Never start a second daemon — it collides on the pidfile lock (exit 5).
3. Pre-screen the batch (see above); drop already-landed beads.
4. Build the `QueueSubmitRequest` JSON (stream group); `harmonik queue dry-run <file>` to validate, then `harmonik queue submit <file>`.
5. Arm a Monitor running `harmonik subscribe` (or the events-tail fallback if unavailable).

### Post-flight: failure triage

After each batch completes, review outcomes before queuing the next batch:

- **Failed once:** eligible for re-dispatch in the next batch.
- **Failed twice in the same session:** STOP. Dispatch an investigator sub-agent before any further re-dispatch. The investigator should check: (a) bead description quality (is there enough context?), (b) prior failure events in `.harmonik/events/events.jsonl`, (c) whether the work is already landed via `git log --grep "Refs: <id>"`.
- **Never dispatch the same bead more than twice without investigation.** Repeating a failing dispatch without diagnosis wastes slots and obscures the root cause.

### When dispatch hangs

The pasteinject quit-on-commit hang is now **auto-recovered in the daemon** (hk-trjef, `f2c395e`, `internal/daemon/pasteinject.go:146-208`) — quit → 30s grace → kill → noChange-subsumed check. You should rarely need manual intervention for that class.

The post-commit /quit watchdog (hk-5s7tg, `internal/daemon/pasteinject.go` post-commit branch) similarly auto-recovers the case where the implementer claude committed but `sess.Wait` is stuck (stale tmux pane handle from a prior daemon's killed run, or surrounding shell pid surviving claude exit). After commit-detection + /quit, a 60s grace fires before the session is force-killed so the workloop's `sess.Wait` unblocks and the reviewer launches.

For other hangs:

1. Identify the stuck `run_id` from `.harmonik/queue.json` or the worktree listing.
2. `git -C .harmonik/worktrees/<run_id> log --oneline -3` — if a `Refs:` commit exists, work was done; daemon is stuck on the next step (merge, reviewer, push).
3. Tail `.harmonik/events/events.jsonl` filtered by `run_id` — which event types fired? Which expected ones did not?
4. If implementer claude has already exited but the daemon is hung: kill the harmonik daemon PID (`pkill -f "harmonik --project"`, or `harmonik run` if you bootstrapped via the legacy path), ff-merge the worktree branch by hand, push, close bead. File a friction bead with the missing-event signature. After killing, re-start the daemon per "Start the daemon once".

## Multi-agent comms

When multiple orchestrator sessions run concurrently, use **`harmonik comms`** (the bus) to coordinate — NOT file appends. The file-outbox convention (`AGENT_COMMS.md` at the repo root, `.harmonik/comms/<me>.md`) is **RETIRED** as of hk-8sm4f (2026-06-01).

### Send a message to another agent

```bash
harmonik comms send --to <agent-name> -- <body>
harmonik comms send --broadcast -- <body>        # to all online agents
```

`<body>` may be a status announcement, a resource-lock warning (daemon restart, `git reset --hard`, binary rebuild), or a handoff note. Use `--topic` to categorize.

### Receive messages

```bash
harmonik comms recv --follow                     # drain backlog then stream live
harmonik comms recv --follow --from orchestrator # filter by sender
harmonik comms recv --follow --json              # NDJSON output (safe to parse)
```

`recv --follow` replaces tailing `.harmonik/comms/<me>.md`. **Dedupe on `event_id`** — delivery is at-least-once (N3). See `.claude/skills/agent-comms/SKILL.md` for the full CLI surface and the dedup requirement.

### See who is online

```bash
harmonik comms who        # agents online within the last ~120s
harmonik comms who --json
```

### Pre-coordination (shared resources)

Before touching shared resources (daemon restart, `git reset --hard`, binary rebuild, others' beads/worktrees), announce via `comms send` and check `comms who`. Read `comms recv` tail to ensure no peer has claimed the resource first.

### Operator log view (no daemon needed)

```bash
harmonik comms log --since 30m               # all agent_message events last 30m
harmonik comms log --from orchestrator --json
```

`comms log` does NOT advance any agent cursor — it is the human/operator "read the conversation" view.

### Why the file outbox is retired

The old `AGENT_COMMS.md` / `.harmonik/comms/<me>.md` approach had concurrent-append races (garbled lines) and tripped the daemon's escape-detector on in-flight beads (false `implementer_escape` signals). Bus-routed `comms send` writes events under gitignored `.harmonik/`, so `git status` is always clean (success-criterion 3 from the agent-comms spec §6).

**Do NOT**: append to `AGENT_COMMS.md` or any `.harmonik/comms/*.md` file from an orchestrator session. The physical files may still exist on disk during the live transition — do not delete them; just stop writing to them.

## Planning with kerf

This project uses [kerf](docs/components/internal/kerf.md) for structured planning. The project is **spec-first**: the spec describes how the system operates, and code is updated to match the spec. The `spec` jig is the default.

Before implementing non-trivial changes (new subsystems, refactors that cross subsystem boundaries, changes to cross-cutting contracts), create a kerf work:

    kerf new <codename>

This creates a work on the bench and shows the process to follow. The jig guides you through structured passes — problem space, decomposition, research, design, spec draft, integration, and tasks — with review sub-agents at each major pass.

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

`kerf next` returns ranked bead IDs → orchestrator dispatches them via harmonik → on completion, `br close <id>` is invoked → `kerf triage --ack` advances kerf's baseline. kerf manages the queue and work-attachment; harmonik executes.

### Beta-test caveat

kerf is currently in **beta-test** in this project — harmonik is the first beta-tester of the new `kerf next` / `triage` / `pin` / `work edit` / `init` surface. Expect friction. Known issues at time of writing: `kerf next` may report empty for works lacking `bead_filter` clauses; `kerf init` emits stale + duplicated agent-instruction blocks; `kerf triage` mixes good and phantom suggestions. Log issues you encounter to [`docs/kerf-beta-feedback.md`](docs/kerf-beta-feedback.md) — see [`KERF-FEEDBACK.md`](KERF-FEEDBACK.md) for the convention.

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

- **Specs live in `specs/`** at the repo root. These are normative: the spec is always right, and code is expected to match it. Spec drafts produced by kerf are copied here on `kerf finalize`.
- **Kerf process artifacts** (problem space, research, design, drafts, tasks, reviews) live on the **global bench** at `~/.kerf/projects/{id}/{codename}/`, NOT under the repo's `.kerf/`. The repo-local `.kerf/` directory is a partial mirror and not the authoritative working directory. Agents must write pass artifacts to the bench path printed by `kerf new` / `kerf show`, or they will silently produce orphan files. Run `kerf localize` to reconcile any files written to the wrong location.
- **Knowledge base docs** (`docs/`) capture problems, goals, concepts, components, subsystems, ideas, and the collaboration log. These are inputs to kerf works; they are not themselves normative specs.
- **Ten architectural decisions** are locked in as of 2026-04-19. See [STATUS.md](STATUS.md#decisions-locked-in-2026-04-19). Reopening one requires strong new evidence.
- **Bead label convention for kerf work codenames:** use the `codename:<name>` prefix (e.g. `codename:handler-pause`, `codename:claude-hook-bridge`). Kerf work `bead_filter` clauses must match the same form. Functional/topical labels (e.g. `queue`, `spec-drift`) remain bare — only labels whose sole purpose is to identify a kerf work codename get the prefix.

## Don't

- Don't reopen locked-in decisions without explicit user request.
- Don't add abstraction layers the user hasn't asked for.
- Don't skip the AGENT_INDEX → STATUS → TASKS reading order when picking up the project.

## Terminology — avoid MVH

MVH (Minimum Viable Harmonik) was a project-level milestone achieved 2026-05-14. It is NOT a per-feature scope label. New plans/beads/specs MUST NOT use MVH framing — it has historically licensed half-built features. Use "Done means..." criteria per plans/README.md instead.

<!-- bv-agent-instructions-v2 -->

---

## Beads Workflow Integration

This project uses [beads_rust](https://github.com/Dicklesworthstone/beads_rust) (`br`) for issue tracking and [kerf](docs/components/internal/kerf.md) for prioritization and triage. Issues are stored in `.beads/` and tracked in git.

### Prioritization: use kerf, not bv

**`kerf next` is the single entry point for "what to work on."** It returns a ranked feed of beads with work-context (which kerf work owns each bead), cleanup tasks, and warnings. `kerf triage` handles drift detection (untriaged beads, external closes/reopens, multi-matched beads).

```bash
kerf next                        # Ranked feed: top item is what to do next
kerf next --format=json          # Machine-readable output
kerf next --only=bead            # Only bead items (skip cleanup/warnings)
kerf triage                      # Drift report: untriaged, multi-matched, external drift
kerf triage --ack                # Advance baseline after acting on the report
kerf map                         # Works grouped by area
```

`bv` (beads_viewer) is installed but **not used for prioritization** — kerf owns that. `bv` is only useful for graph-metric analysis (`--robot-insights` for PageRank/betweenness) or dependency graph export (`--robot-graph`), which kerf does not cover. **CRITICAL: Use ONLY --robot-* flags with bv. Bare bv launches an interactive TUI that blocks your session.**

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

<!-- end-bv-agent-instructions -->
