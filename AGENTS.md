# Harmonik — Agent Instructions

> `CLAUDE.md` is a symlink to this file (`AGENTS.md`). They are the same content — edits to `AGENTS.md` cover both.

> Cross-project working-style rules (keep moving, delegate, plain English, compact, review gate) live in `~/.claude/CLAUDE.md`. This file adds harmonik-specific bits on top of those.

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
  - Does **NOT** boot-read: `AGENT_INDEX.md`, `STATUS.md`, product/`docs/` knowledge base, full skill bodies. `ROADMAP.md` only on cold boot / milestone.
- **Captain — keeper-restart resume (LEAN):** re-drain comms → re-read tier-3/tier-2 + ONE boot digest → trust cached tier state as input → re-arm watchers. No heavy re-derive.
- **Crew — minimal load** (see `.claude/skills/crew-launch/SKILL.md`): its mission file (`.harmonik/crew/missions/<crew>.md`) + `crew-launch/SKILL.md` + `agent-comms` + `beads-cli` + `harmonik-dispatch`. Does **NOT** load fleet-level state (ROADMAP, captain-lanes, project.yaml, orchestrator standing-rules, STATUS, HANDOFF, knowledge base) — scoped to ONE epic + ONE queue.
- **Implementer-orchestrator (main `/session-resume`, non-captain):** `AGENT_INDEX → STATUS → HANDOFF` reading order + the **`orchestrator-rules` skill** (standing rules) + `harmonik-dispatch`.

## Start here

Read [AGENT_INDEX.md](AGENT_INDEX.md) first — the master map of the knowledge base (every doc reachable within two hops). Then [STATUS.md](STATUS.md) for phase + locked decisions, `.harmonik/context/captain-lanes.md` for the medium-term lane/epic tracker, and [HANDOFF.md](HANDOFF.md) for this-session state.

**Launching a captain or crew?** Use the native umbrella verb — `harmonik start captain` or `harmonik start crew <name>` (keeper auto-armed; NO env var, NO script path — `--project` defaults to cwd). The old `~/.claude/captain-tools/captain-launch.sh` + `HK_PROJECT` env var are RETIRED in favor of `harmonik start captain`. Positional-XOR-flags rule (D2): the simple form is a bare name only (`start crew paul`); the moment any `--flag` appears the name must move to `--name` (mixing a bare name with flags is a hard error). `harmonik captain` / `harmonik crew start <name>` remain as back-compat aliases.

**Launching an oversight session (commodore, admiral)?** ALWAYS use `harmonik start commodore` / `harmonik start admiral` — never a bare `claude --remote-control` or `harmonik agent brief` in a hand-rolled tmux session. Those launch paths write neither the crew registry record nor the `crew-<name>`-prefixed tmux session that the daemon's boot-time orphan sweep (`RunOrphanSweep`) checks for, so the session is reaped as an orphan at the next daemon boot / supervisor revive (hk-zeo5y). `harmonik start commodore` / `admiral` always route through the same registry-protecting RPC as `harmonik crew start <name>` — the identity is the role name (no positional), so protection is automatic, not a step to remember.

**Booting as a captain or crew?** These skills are **project-local under the repo** — read them at `/Users/gb/github/harmonik/.claude/skills/…`, NOT the global `~/.claude/skills/` (no captain/crew skill exists there). Captain: read `.claude/skills/captain/STARTUP.md` FIRST, then `SKILL.md` in that dir. Crew: read `.claude/skills/crew-launch/SKILL.md`. See also `.claude/skills/keeper` (per-session context-watcher) and `.claude/skills/harmonik-lifecycle` (supervise / promote / reconcile / init).

## Standing rules → the `orchestrator-rules` skill

Dispatch discipline (the daily loop, the HARD-RULE exceptions), priority (kerf-first), bead lifecycle (daemon owns terminal transitions; never pre-set in_progress), the review gate, autonomy/flow boundaries, and the major-issue fan-out trigger: all canonical in the **`orchestrator-rules` skill** (`.claude/skills/orchestrator-rules/SKILL.md`). It points to the detail-owner skills; it does not duplicate them.

- **Daily loop / daemon / `queue submit` / `append` / `subscribe`:** the **harmonik-dispatch** skill. Full design: `docs/orchestration-protocol-v2.md`.
- **Monitoring the daemon** (the canonical Monitor pattern, stream-vs-wave, failure triage): the **harmonik-dispatch** skill. Manual hang-recovery: `docs/known-workarounds.md`.
- **CWD discipline** (never `cd` into a worktree; operate from repo root via `git -C` absolute paths): the **orchestrator-rules** skill.
- **Multi-agent comms** (`harmonik comms` bus; dedupe on `event_id`): the **agent-comms** skill. The `.harmonik/comms/*.md` file-outbox is RETIRED — do NOT write to those files.
- **Lifecycle** (init / supervise / reconcile / promote; work-project deployment, `branching.yaml`): the **harmonik-lifecycle** skill. integration→main is always a human PR step.
- **Redeploy the live daemon binary** (in-place swap on the running box; supervisor revival, SIGTERM-the-daemon, health-window/last-good, `daemon-YYYYMMDD-NN` tag): the runbook at [`docs/daemon-redeploy.md`](docs/daemon-redeploy.md).
- **Keeper** (per-session context-fill watcher; now incl. the `hold`/`release` co-working override that suspends the ACT/restart cutoff while WARN still fires): the **keeper** skill.

<!-- END harmonik:managed -->

## Planning with kerf

Non-trivial changes are planned with **kerf** (spec-first; create a kerf work before new subsystems / cross-subsystem refactors / cross-cutting contracts; trivial changes skip it). The full command surface, jigs, workflow, and beta caveats are planning-agent detail — see [`docs/components/internal/kerf.md`](docs/components/internal/kerf.md) §"Commands & Workflow". The `codename:` bead-label + artifact-path rules stay in "Key conventions" below.

## Key conventions

- **Specs live in `specs/`** at the repo root. These are normative: the spec is always right, and code is expected to match it. Spec drafts produced by kerf are copied here on `kerf finalize`.
- **Kerf process artifacts** (problem space, research, design, drafts, tasks, reviews) live **in the repo** at `.kerf/works/{codename}/`. This project runs kerf in **local mode** (`.kerf/config.yaml` has `storage: local`), so `.kerf/works/` IS the authoritative working directory — not a mirror. The global bench path `~/.kerf/projects/gregberns-harmonik` is a **symlink** to it, so a bench path printed by `kerf new` / `kerf show` resolves to the same files; either path is safe to write to. Because these artifacts live in the repo, they are tracked files and must be **committed** like any other change — an uncommitted work exists only in one working tree. **Do NOT run `kerf localize`** — the migration to local mode already happened; re-running it is not how you fix a misplaced file.
- **Knowledge base docs** (`docs/`) capture problems, goals, concepts, components, subsystems, ideas, and the collaboration log. These are inputs to kerf works; they are not themselves normative specs.
- **Ten architectural decisions** are locked in as of 2026-04-19. See [STATUS.md](STATUS.md#decisions-locked-in-2026-04-19). Reopening one requires strong new evidence.
- **Bead label convention for kerf work codenames:** use the `codename:<name>` prefix (e.g. `codename:handler-pause`, `codename:claude-hook-bridge`). Kerf work `bead_filter` clauses must match the same form. Functional/topical labels (e.g. `queue`, `spec-drift`) remain bare — only labels whose sole purpose is to identify a kerf work codename get the prefix.

## Don't

- Don't reopen locked-in decisions without explicit user request.
- Don't add abstraction layers the user hasn't asked for.
- Don't skip the AGENT_INDEX → STATUS → captain-lanes → HANDOFF reading order when picking up the project.

<!-- bv-agent-instructions-v2 -->

---

## Beads Workflow Integration

This project uses [beads_rust](https://github.com/Dicklesworthstone/beads_rust) (`br`) for issue tracking and [kerf](docs/components/internal/kerf.md) for prioritization and triage. Issues are stored in `.beads/` and tracked in git.

### Prioritization: use kerf, not bv

**Work the operator's and admiral's named initiatives first; `kerf next` ranks everything below that line.** It returns a ranked feed of beads with work-context, cleanup tasks, and warnings — the priority source for the *unclaimed backlog*, never an override of a named initiative. `kerf triage` handles drift detection.

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

````markdown
## UBS Quick Reference for AI Agents

UBS stands for "Ultimate Bug Scanner": **The AI Coding Agent's Secret Weapon: Flagging Likely Bugs for Fixing Early On**

**Install:** `curl -sSL https://raw.githubusercontent.com/Dicklesworthstone/ultimate_bug_scanner/main/install.sh | bash`

**Golden Rule:** `ubs <changed-files>` before every commit. Exit 0 = safe. Exit >0 = fix & re-run.

**Commands:**
```bash
ubs file.ts file2.py                    # Specific files (< 1s) — USE THIS
ubs $(git diff --name-only --cached)    # Staged files — before commit
ubs --only=js,python src/               # Language filter (3-5x faster)
ubs --ci --fail-on-warning .            # CI mode — before PR
ubs --help                              # Full command reference
ubs sessions --entries 1                # Tail the latest install session log
ubs .                                   # Whole project (ignores things like .venv and node_modules automatically)
```

**Output Format:**
```
⚠️  Category (N errors)
    file.ts:42:5 – Issue description
    💡 Suggested fix
Exit code: 1
```
Parse: `file:line:col` → location | 💡 → how to fix | Exit 0/1 → pass/fail

**Fix Workflow:**
1. Read finding → category + fix suggestion
2. Navigate `file:line:col` → view context
3. Verify real issue (not false positive)
4. Fix root cause (not symptom)
5. Re-run `ubs <file>` → exit 0
6. Commit

**Speed Critical:** Scope to changed files. `ubs src/file.ts` (< 1s) vs `ubs .` (30s). Never full scan for small edits.

**Bug Severity:**
- **Critical** (always fix): Null safety, XSS/injection, async/await, memory leaks
- **Important** (production): Type narrowing, division-by-zero, resource leaks
- **Contextual** (judgment): TODO/FIXME, console logs

**Anti-Patterns:**
- ❌ Ignore findings → ✅ Investigate each
- ❌ Full scan per edit → ✅ Scope to file
- ❌ Fix symptom (`if (x) { x.y }`) → ✅ Root cause (`x?.y`)
````
