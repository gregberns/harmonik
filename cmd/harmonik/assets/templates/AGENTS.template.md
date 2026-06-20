# Agent Instructions

> `CLAUDE.md` is a symlink to this file (`AGENTS.md`). They are the same content — edits to `AGENTS.md` cover both.

> Cross-project working-style rules (keep moving, delegate, plain English, compact, review gate) live in `~/.claude/CLAUDE.md`. This file adds project-specific bits on top of those.

<!-- BEGIN harmonik:managed agents-router -->

## Precedence

Standing behavioral rules: the **`orchestrator-rules` skill** (`.claude/skills/orchestrator-rules/SKILL.md`) is canonical. On conflict: **orchestrator-rules skill > AGENTS.md prose > per-domain skills own their detail**. Operational state lives in `.harmonik/context/` (captain tiers) and `HANDOFF.md` (this session) — **never in this file**. AGENTS.md is a ROUTER: it points you at the right contract; it does not restate one.

## Per-role load map

Each role loads only its slice. The boot runbook in each role's skill is authoritative; this is the map.

- **Captain — cold boot** (see `.claude/skills/captain/STARTUP.md`):
  1. Step 0 — identity + CWD guard.
  2. Step 0a — tier-3 `.harmonik/context/project.yaml` (phase, locked decisions, guardrails).
  3. Step 0b — tier-2 `.harmonik/context/captain-lanes.md` (lanes + epics-in-progress + parked + dated operator directives).
  4. Step 1 — `captain/SKILL.md` + the **`orchestrator-rules` skill** (standing rules) + tier-1 `HANDOFF.md` (a claim, not ground truth).
  5. Step 2 — boot digest = ground-truth; overrides every claim above. Steps 3–6 reconcile / plan / staff / arm watchers.
  - Does **NOT** boot-read: `AGENT_INDEX.md`, `STATUS.md`, product/`docs/` knowledge base, full skill bodies. `.harmonik/context/roadmap.md` only on cold boot / milestone.
- **Captain — keeper-restart resume (LEAN):** re-drain comms → re-read tier-3/tier-2 + ONE boot digest → trust cached tier state as input → re-arm watchers. No heavy re-derive.
- **Crew — minimal load** (see `.claude/skills/crew-launch/SKILL.md`): its mission file (`.harmonik/crew/missions/<crew>.md`) + `crew-launch/SKILL.md` + `agent-comms` + `beads-cli` + `harmonik-dispatch`. Does **NOT** load fleet-level state (roadmap, captain-lanes, project.yaml, orchestrator standing-rules, STATUS, HANDOFF, knowledge base) — scoped to ONE epic + ONE queue.
- **Implementer-orchestrator (main `/session-resume`, non-captain):** `AGENT_INDEX → STATUS → HANDOFF` reading order + the **`orchestrator-rules` skill** (standing rules) + `harmonik-dispatch`.

## Start here

Read [AGENT_INDEX.md](AGENT_INDEX.md) first — the master map of the knowledge base (every doc reachable within two hops). Then [STATUS.md](STATUS.md) for phase + locked decisions, `.harmonik/context/captain-lanes.md` for the medium-term lane/epic tracker, and [HANDOFF.md](HANDOFF.md) for this-session state.

**Booting as a captain or crew?** These skills are **project-local under the repo** — read them at `$PROJECT_DIR/.claude/skills/…`, NOT the global `~/.claude/skills/` (no captain/crew skill exists there). Captain: read `.claude/skills/captain/STARTUP.md` FIRST, then `SKILL.md` in that dir. Crew: read `.claude/skills/crew-launch/SKILL.md`. See also `.claude/skills/keeper` (per-session context-watcher) and `.claude/skills/harmonik-lifecycle` (supervise / promote / reconcile / init).

## Standing rules → the `orchestrator-rules` skill

Dispatch discipline (the daily loop, the HARD-RULE exceptions), priority (kerf-first), bead lifecycle (daemon owns terminal transitions; never pre-set in_progress), the review gate, autonomy/flow boundaries, and the major-issue fan-out trigger: all canonical in the **`orchestrator-rules` skill** (`.claude/skills/orchestrator-rules/SKILL.md`). It points to the detail-owner skills; it does not duplicate them.

- **Daily loop / daemon / `queue submit` / `append` / `subscribe`:** the **harmonik-dispatch** skill.
- **Monitoring the daemon** (the canonical Monitor pattern, stream-vs-wave, failure triage): the **harmonik-dispatch** skill.
- **CWD discipline** (never `cd` into a worktree; operate from repo root via `git -C` absolute paths): the **orchestrator-rules** skill.
- **Multi-agent comms** (`harmonik comms` bus; dedupe on `event_id`): the **agent-comms** skill. The `.harmonik/comms/*.md` file-outbox is RETIRED — do NOT write to those files.
- **Lifecycle** (init / supervise / reconcile / promote; work-project deployment, `branching.yaml`): the **harmonik-lifecycle** skill. The daemon merges completed bead branches into `$TARGET_BRANCH`.
- **Keeper** (per-session context-fill watcher): the **keeper** skill.

<!-- END harmonik:managed -->

## Key conventions

- **Operational state lives in tier files, not in this router.** `.harmonik/context/project.yaml` (durable phase + locked decisions), `.harmonik/context/captain-lanes.md` (lane registry + dated directives), `.harmonik/context/roadmap.md` (epic roadmap), `HANDOFF.md` (this-session). Each carries a header declaring who loads it and what must NOT go there.
- **Bead label convention for kerf work codenames:** use the `codename:<name>` prefix (e.g. `codename:my-feature`). Functional/topical labels remain bare — only labels whose sole purpose is to identify a kerf work codename get the prefix.

## Don't

- Don't reopen locked-in decisions without explicit operator request.
- Don't add abstraction layers the operator hasn't asked for.
- Don't skip the AGENT_INDEX → STATUS → captain-lanes → HANDOFF reading order when picking up the project.

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
