<!-- DRAFT — proposed replacement for /cmd/harmonik/assets/templates/AGENTS.template.md
     (startup-doc revamp companion, per 02-cutover-and-open-questions.md §2.2[B] + Step 0.3).
     Lands WITH the amended AGENTS.md at Step 4.1 — this is the `harmonik init` scaffold that
     seeds a NEW project's AGENTS.md, so it must carry the SAME router model the amended live
     AGENTS.md lands with, or every project bootstrapped between now and a future re-sync would
     scaffold the retired per-role load-map ritual right back in.

     What changed and why (mirrors 00-SYNTHESIS.md §6 "Entry docs" verdict for AGENTS.md):
     - CUT the "## Per-role load map" section (the STARTUP.md Step 0/0a/0b/1/2 walk, the
       Captain/Crew/Implementer-orchestrator boot-route table) and the "Start here" reading-order
       ritual — replaced with ONE "## Booting" section: run `harmonik agent brief`, its output IS
       the complete boot context; beads boot from `agent-task.md` and ignore this file's routing
       entirely; solo `/session-resume` reads its own target directly.
     - Trimmed the "Don't" list's "Don't skip the AGENT_INDEX → STATUS → captain-lanes → HANDOFF
       reading order" bullet — that ritual is exactly what's being cut; keeping it as a "Don't"
       would re-instate the same contradiction the live router just fixed.
     - Everything else — Precedence, launch verbs (adjusted for the generic $PROJECT_DIR/
       $TARGET_BRANCH template form this file already used), Standing-rules pointers, Key
       conventions, remaining Don't bullets, the Beads Workflow Integration section — KEPT
       verbatim per 00-SYNTHESIS.md §6 ("AGENTS.md Precedence, launch verbs + D2 rule, Key
       conventions, Don't 1–2, skill/doc pointers, beads/kerf sections | KEEP").
     - Per 03-operator-decisions.md's governing principle (PRINCIPLES, NOT RULES): the Booting
       section states WHAT to run and WHY its output suffices, not a checklist of steps to walk.
     This DRAFT comment is removed on deploy. -->

# Agent Instructions

> `CLAUDE.md` is a symlink to this file (`AGENTS.md`). They are the same content — edits to `AGENTS.md` cover both.

> Cross-project working-style rules (keep moving, delegate, plain English, compact, review gate) live in `~/.claude/CLAUDE.md`. This file adds project-specific bits on top of those.

<!-- BEGIN harmonik:managed agents-router -->

## Precedence

Standing behavioral rules: the **`orchestrator-rules` skill** (`.claude/skills/orchestrator-rules/SKILL.md`) is canonical. On conflict: **orchestrator-rules skill > AGENTS.md prose > per-domain skills own their detail**. Operational state lives in `.harmonik/context/` (captain tiers) and `HANDOFF.md` (this session) — **never in this file**. AGENTS.md is a ROUTER: it points you at the right contract; it does not restate one.

## Booting

**Booting as a manifest agent** (captain, crew, admiral, watch)? Run `harmonik agent brief --wake <reason>`. Its output IS your complete boot context — tier state, standing-rules pointers, and the embedded handoff, already assembled and precedence-ordered. There is no separate reading-order to walk on top of it.

**Booting to work a bead?** The daemon-injected `agent-task.md` IS your boot context — ignore this file's routing; it doesn't apply to bead workers.

**Solo `/session-resume` (no manifest, no bead)?** Read `HANDOFF.md` + the `orchestrator-rules` skill + the `harmonik-dispatch` skill directly — no chain beyond that.

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
