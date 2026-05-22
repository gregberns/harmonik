# Harmonik — Agent Instructions

> Cross-project working-style rules (keep moving, delegate, plain English, compact, review gate) live in `~/.claude/CLAUDE.md`. This file adds harmonik-specific bits on top of those.

## Start here

Read [AGENT_INDEX.md](AGENT_INDEX.md) first. It is the master map of the knowledge base and every document is reachable from there within two hops. Then read [STATUS.md](STATUS.md) for current project state and [TASKS.md](TASKS.md) for the active work list.

## Daily loop (canonical)

**`harmonik run` is the default dispatcher for this project's own development.** The intended loop:

1. `bv --robot-triage` and `kerf next` — surface the prioritized work.
2. Pick a batch of 3–5 beads from the top of the feed (skip the untested-workload classes documented in `HANDOFF.md` §"Three caveats" until the probes land).
3. `harmonik run --beads id1,id2,... --max-concurrent N` — run in background; the daemon spawns claude, watches for completion, commits, merges to main, pushes, and closes each bead. Review-loop is **on by default** (hk-g0ckv); pass `--no-review-loop` to opt out.
4. While harmonik runs: queue the next batch, drain `kerf triage` untriaged items, file follow-ups from prior runs, review recently-merged commits.
5. On exit: review outcomes, dispatch next batch.

**Sub-agent dispatch (via the Agent tool) is the EXCEPTION**, justified only when (a) you're fixing harmonik itself in a code path that breaks dispatch, (b) the change is ≤2 lines of typo/cross-reference cleanup, or (c) the work touches an untested workload class (see `docs/kerf-feedback/2026-05-19-phase2-readiness-audit.md`).

Target: ≥75% of substantive commits per session land via `harmonik run`. If sub-agent dispatch is creeping above 25%, stop and audit why — that's a signal harmonik has new friction or you've drifted into batch-mind.

Full design: `docs/orchestration-protocol-v2.md`. The kerf workflow below remains the planning surface for non-trivial NEW work (spec drafts, plan epics); kerf hands off beads, harmonik executes them.

## Orchestrator wrappers for `harmonik run`

The orchestrator is a Claude Code session. When it dispatches `harmonik run` as a bash background task, the Bash tool only delivers ONE completion notification when the whole batch exits. For multi-bead runs (or any run >a few minutes) the orchestrator must run a **Claude Code Monitor** alongside to surface per-bead progress, hangs, and daemon failures. Without a Monitor, the orchestrator is blind from dispatch to batch-exit.

### Canonical pattern (until `harmonik subscribe` lands — hk-6ynv4)

Three pieces, always together:

1. **Dispatch in background:** `Bash(run_in_background=true)` with `harmonik run --beads id1,id2,... --notify-stream` (review-loop is on by default per hk-g0ckv; pass `--no-review-loop` only to opt out).
2. **Monitor the bash task's stdout** (the file printed by the Bash-tool result) — that's where `--notify-stream` writes per-bead `[hk-XXX] success|failed` lines.
3. **Monitor `.harmonik/events/events.jsonl`** — that's where the daemon writes typed events (`run_started`, `run_completed`, `run_failed`, `reviewer_verdict`, etc.). NOTE: there is no `daemon.log` file; ignore older guidance that says to grep one.

```bash
# In a Monitor tool call (timeout_ms = 3600000, persistent = false):
( tail -F /private/tmp/claude-XXX/.../tasks/<bash-task-id>.output 2>/dev/null \
    | grep --line-buffered -E "\[hk-[a-z0-9]+\] (success|failed)|ERROR|panic|fatal|FATAL" ) &
( tail -F /Users/gb/github/harmonik/.harmonik/events/events.jsonl 2>/dev/null \
    | grep --line-buffered -E "run_completed|run_failed|merge_conflict|reviewer_verdict" ) &
wait
```

Each matched line becomes a notification to the orchestrator. The Monitor exits when both tail processes do (e.g., daemon dies). Re-arm if it hits the 1-hour timeout.

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

`harmonik run --beads` creates a `kind=wave` queue that does NOT accept appends. Mid-flight extension requires `kind=stream` via `harmonik queue submit <file>` + `harmonik queue append --queue-id <uuid> <group> <bead-ids...>`. Daemon doesn't wake on submit if idle — keep an active `harmonik run` so the workloop stays hot (gaps: hk-b0cyc, hk-24xn1, hk-7nbey for stream-default).

### Pre-flight checklist

1. Rebuild harmonik first (`go install ./cmd/harmonik`) — stale binary is the #1 cause of "but I fixed that".
2. Pre-screen the batch (see above); drop already-landed beads.
3. Choose `--max-concurrent` — keep at `1` until hk-wx8z8 (parallel-run stability) verifies it.
4. Dispatch in background with `--notify-stream` (review-loop is default on per hk-g0ckv).
5. Arm a Monitor tailing the bash stdout file AND `.harmonik/events/events.jsonl`.

### When dispatch hangs

The pasteinject quit-on-commit hang is now **auto-recovered in the daemon** (hk-trjef, `f2c395e`, `internal/daemon/pasteinject.go:146-208`) — quit → 30s grace → kill → noChange-subsumed check. You should rarely need manual intervention for that class.

For other hangs (e.g. hk-5s7tg reviewer-spawn hang):

1. Identify the stuck `run_id` from `.harmonik/queue.json` or the worktree listing.
2. `git -C .harmonik/worktrees/<run_id> log --oneline -3` — if a `Refs:` commit exists, work was done; daemon is stuck on the next step (merge, reviewer, push).
3. Tail `.harmonik/events/events.jsonl` filtered by `run_id` — which event types fired? Which expected ones did not?
4. If implementer claude has already exited but daemon is hung: kill the harmonik PID (`pkill -f "harmonik run"`), ff-merge the worktree branch by hand, push, close bead. File a friction bead with the missing-event signature.

### Future: `harmonik subscribe` (hk-6ynv4)

The current "tail two files" pattern is a stop-gap. Once `harmonik subscribe` lands, the canonical pattern becomes:

```bash
# In a Monitor tool call:
harmonik subscribe --types run_completed,run_failed,run_stale,heartbeat --heartbeat 60s --json
```

— one process, NDJSON to stdout, server-side heartbeat (default 60s) so the orchestrator wakes periodically even if the daemon goes quiet. Until that ships, use the tail-pair pattern above.

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

This project uses [beads_rust](https://github.com/Dicklesworthstone/beads_rust) (`br`) for issue tracking and [beads_viewer](https://github.com/Dicklesworthstone/beads_viewer) (`bv`) for graph-aware triage. Issues are stored in `.beads/` and tracked in git.

### Using bv as an AI sidecar

bv is a graph-aware triage engine for Beads projects (.beads/beads.jsonl). Instead of parsing JSONL or hallucinating graph traversal, use robot flags for deterministic, dependency-aware outputs with precomputed metrics (PageRank, betweenness, critical path, cycles, HITS, eigenvector, k-core).

**Scope boundary:** bv handles *what to work on* (triage, priority, planning). `br` handles creating, modifying, and closing beads.

**CRITICAL: Use ONLY --robot-* flags. Bare bv launches an interactive TUI that blocks your session.**

#### The Workflow: Start With Triage

**`bv --robot-triage` is your single entry point.** It returns everything you need in one call:
- `quick_ref`: at-a-glance counts + top 3 picks
- `recommendations`: ranked actionable items with scores, reasons, unblock info
- `quick_wins`: low-effort high-impact items
- `blockers_to_clear`: items that unblock the most downstream work
- `project_health`: status/type/priority distributions, graph metrics
- `commands`: copy-paste shell commands for next steps

```bash
bv --robot-triage        # THE MEGA-COMMAND: start here
bv --robot-next          # Minimal: just the single top pick + claim command

# Token-optimized output (TOON) for lower LLM context usage:
bv --robot-triage --format toon
```

#### Other bv Commands

| Command | Returns |
|---------|---------|
| `--robot-plan` | Parallel execution tracks with unblocks lists |
| `--robot-priority` | Priority misalignment detection with confidence |
| `--robot-insights` | Full metrics: PageRank, betweenness, HITS, eigenvector, critical path, cycles, k-core |
| `--robot-alerts` | Stale issues, blocking cascades, priority mismatches |
| `--robot-suggest` | Hygiene: duplicates, missing deps, label suggestions, cycle breaks |
| `--robot-diff --diff-since <ref>` | Changes since ref: new/closed/modified issues |
| `--robot-graph [--graph-format=json\|dot\|mermaid]` | Dependency graph export |

#### Scoping & Filtering

```bash
bv --robot-plan --label backend              # Scope to label's subgraph
bv --robot-insights --as-of HEAD~30          # Historical point-in-time
bv --recipe actionable --robot-plan          # Pre-filter: ready to work (no blockers)
bv --recipe high-impact --robot-triage       # Pre-filter: top PageRank scores
```

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

1. **Triage**: Run `bv --robot-triage` to find the highest-impact actionable work
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
