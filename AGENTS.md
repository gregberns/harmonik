# Harmonik — Agent Instructions

> `CLAUDE.md` is a symlink to this file (`AGENTS.md`). They are the same content — edits to `AGENTS.md` cover both.

> Cross-project working-style rules (keep moving, delegate, plain English, compact, review gate) live in `~/.claude/CLAUDE.md`. This file adds harmonik-specific bits on top of those.

## Start here

Read [AGENT_INDEX.md](AGENT_INDEX.md) first. It is the master map of the knowledge base and every document is reachable from there within two hops. Then read [STATUS.md](STATUS.md) for current project state and [TASKS.md](TASKS.md) for the active work list.

**Orchestrator permanent directives:** [`docs/orchestrator-rules.md`](docs/orchestrator-rules.md) — all stable rules (dispatch discipline, priority, bead lifecycle, autonomy, dispatch shape, monitor pattern). Load alongside HANDOFF.md on every `/session-resume`. **Known workarounds** (worktree bugs, harness quirks): [`docs/known-workarounds.md`](docs/known-workarounds.md).

**Booting as a captain or crew?** These skills are **project-local under the repo** — read them at `/Users/gb/github/harmonik/.claude/skills/…`, NOT the global `~/.claude/skills/` (there is no captain/crew skill there; reading the global path fails). Captain session: read `/Users/gb/github/harmonik/.claude/skills/captain/STARTUP.md` FIRST, then `SKILL.md` in that same dir. Crew session: read `/Users/gb/github/harmonik/.claude/skills/crew-launch/SKILL.md`. These hold your operating contract — boot sequence, queue/comms discipline, progress feed. See also `.claude/skills/keeper` (per-session context-watcher) and `.claude/skills/harmonik-lifecycle` (supervise / promote / reconcile / init), same project-local dir.

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

Daemon-start, the daily loop, `harmonik run` legacy behavior, and `queue submit` / `append` / `dry-run`: see the **harmonik-dispatch** skill (loads on session-resume); the ≥75%-of-substantive-commits-through-the-daemon-queue target lives there too. Full design: `docs/orchestration-protocol-v2.md`.

### Work-project deployment

Work-project (integration-branch / protected-main) deployment, `branching.yaml`, and `harmonik promote` (push & PR modes): see the **harmonik-lifecycle** skill. integration→main is always a human PR step.

## Monitoring the daemon

`harmonik subscribe` (the canonical Monitor pattern), stream-vs-wave semantics, queue append, the pre-flight checklist, and post-flight failure triage: see the **harmonik-dispatch** skill. The dispatch-hang class (pasteinject quit-on-commit, post-commit /quit) is **auto-recovered in the daemon** (hk-trjef / hk-5s7tg); manual hang-recovery steps live in `docs/known-workarounds.md`.

### CWD discipline — never `cd` into a worktree

The daemon may `git worktree remove` a worktree out from under you on bead completion. Always operate from the repo root via absolute paths: `git -C /Users/gb/github/harmonik <cmd>` rather than `cd`. The orchestrator's CWD must remain `/Users/gb/github/harmonik` for the whole session.

## Multi-agent comms

The `harmonik comms` bus surface (send / recv / who / log; dedupe on `event_id`, at-least-once delivery N3) is the coordination mechanism for concurrent orchestrator sessions — see the **agent-comms** skill. The `AGENT_COMMS.md` / `.harmonik/comms/*.md` file-outbox is RETIRED (hk-8sm4f, 2026-06-01) — do NOT write to those files.

## Planning with kerf

Non-trivial changes are planned with **kerf** (spec-first; create a kerf work before new subsystems / cross-subsystem refactors / cross-cutting contracts; trivial changes skip it). The full command surface, jigs, workflow, and beta caveats are planning-agent detail — see [`docs/components/internal/kerf.md`](docs/components/internal/kerf.md) §"Commands & Workflow". The `codename:` bead-label + bench-path rules stay in "Key conventions" below.

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
