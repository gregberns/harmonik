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

The orchestrator is a Claude Code session. When it dispatches `harmonik run` as a bash background task, several friction points emerge that require wrapper discipline. Patterns below were discovered during the 2026-05-21 20-bead dogfood session.

### 1. Multi-bead runs are one opaque bash task — use `--notify-stream` + active Monitor

**Problem:** `harmonik run --beads a,b,c` exits only when the *whole* batch finishes. The Bash-tool completion notification fires once at batch-exit, so the orchestrator is blind to per-bead progress and to mid-batch hangs (e.g. the pasteinject quit-on-commit hang that landed earlier today).

**Fix:**
- Always pass `--notify-stream`. Each bead emits `[hk-XXX] success|failed` to stdout when it finishes, so a Monitor grep can surface per-bead notifications mid-batch (landed ce9d0e4, bead hk-ibilr).
- Run a Monitor (the Claude Code Monitor tool) that polls every 30s for (i) new commits on `main`, (ii) hung worktrees matching the daemon-log `pasteinject quit-on-commit timeout` signal — and auto-kills the claude PID to unblock the daemon's `sess.Wait` (pattern from bead hk-trjef).

### 2. CWD discipline — never `cd` into a worktree

**Problem:** `harmonik` writes per-run worktrees at `.harmonik/worktrees/<run_id>/`. The daemon may `git worktree remove` one out from under you on bead completion. If your shell's CWD is inside that worktree, subsequent commands fail in confusing ways.

**Fix:** Always operate from the repo root via absolute paths. Use `git -C /Users/gb/github/harmonik <cmd>` rather than `cd`. The orchestrator's CWD must remain `/Users/gb/github/harmonik` for the whole session.

```bash
git -C /Users/gb/github/harmonik log --oneline -5 main
git -C /Users/gb/github/harmonik/.harmonik/worktrees/<run_id> rev-parse HEAD
```

### 3. Pre-screen beads for already-landed work

**Problem:** Beads can be stale-open — implementation already landed on `main` but the bead never got closed. Dispatching one wastes a slot (it hits the noChange path).

**Fix:** Before dispatching, grep git history for the bead ID. Today's session caught 10+ pre-merged beads this way.

```bash
for id in hk-aaa hk-bbb hk-ccc; do
  hits=$(git -C /Users/gb/github/harmonik log --all --grep "Refs: $id" --oneline | wc -l)
  echo "$id $hits"
done
# any id with hits>0 → close as already-landed, don't dispatch
```

### 4. Queue semantics — `wave` vs `stream`

**Problem:** `harmonik run --beads` creates a `kind=wave` queue, which does NOT accept appends. To append mid-run you need `kind=stream` (gap filed as hk-b0cyc). Separately, the daemon doesn't wake on `queue submit` if it's idle (gap filed as hk-24xn1).

**Fix:** For appendable runs, submit a stream-kind queue JSON first, then append:

```bash
harmonik queue submit /path/to/stream-queue.json    # kind=stream
harmonik queue append --queue-id <uuid> grp1 hk-xxx hk-yyy
# workaround for hk-24xn1: keep an active `harmonik run` so the workloop stays hot
```

### Copy-paste Monitor block

The exact bash that worked this session. Wrap your `harmonik run` background task with this loop in the Monitor tool — it streams new-commit lines, auto-kills hung claude PIDs, and surfaces the batch-exit signal.

```bash
REPO=/Users/gb/github/harmonik
LAST_MAIN=$(git -C "$REPO" rev-parse main)
RUN_LOG="$REPO/.harmonik/daemon.log"
until ! pgrep -f "harmonik run --beads" >/dev/null; do
  # (1) new commit on main?
  CUR_MAIN=$(git -C "$REPO" rev-parse main)
  if [ "$CUR_MAIN" != "$LAST_MAIN" ]; then
    git -C "$REPO" log --oneline "$LAST_MAIN..$CUR_MAIN"
    LAST_MAIN=$CUR_MAIN
  fi
  # (2) hung worktree? — daemon log shows "pasteinject quit-on-commit timeout"
  for wt in "$REPO"/.harmonik/worktrees/*/; do
    [ -d "$wt" ] || continue
    rid=$(basename "$wt")
    if grep -q "run=$rid.*pasteinject quit-on-commit timeout" "$RUN_LOG" 2>/dev/null; then
      pid=$(pgrep -f "claude.*$rid" | head -1)
      [ -n "$pid" ] && { echo "auto-kill hung claude pid=$pid run=$rid"; kill "$pid"; }
    fi
  done
  sleep 30
done
echo "BATCH-EXIT: harmonik run finished"
```

### Pre-flight checklist

1. Rebuild harmonik first (`go build -o ~/bin/harmonik ./cmd/harmonik`) — stale binary is the #1 cause of "but I fixed that".
2. Pre-screen the batch: `git log --all --grep "Refs: <id>"` for each bead; drop already-landed ones.
3. Choose `--max-concurrent` — keep at `1` until hk-wx8z8 (parallel-run stability) is verified.
4. Wrap the run in the Monitor block above.
5. Dispatch in background (`run_in_background=true`) and pass `--notify-stream`.

### When dispatch hangs

1. Identify the stuck `run_id` from `.harmonik/queue.json` or the worktree listing.
2. Compare `git -C .harmonik/worktrees/<run_id> rev-parse HEAD` against `main` — if it differs, work was done but not merged.
3. Check the daemon log for `run=<run_id>` and look for `pasteinject quit-on-commit timeout`.
4. `kill $(pgrep -f "claude.*<run_id>")` — the daemon's `sess.Wait` unblocks and resumes the merge path.

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
