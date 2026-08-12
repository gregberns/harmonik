# Agent Instructions

> `CLAUDE.md` is a symlink to this file (`AGENTS.md`). They are the same content — edits to `AGENTS.md` cover both.

> Cross-project working-style rules (keep moving, delegate, plain English, compact, review gate) live in `~/.claude/CLAUDE.md`. This file adds project-specific bits on top of those.

<!-- BEGIN harmonik:managed agents-router -->

## Precedence

Standing behavioral rules: the **`orchestrator-rules` skill** (`.claude/skills/orchestrator-rules/SKILL.md`) is canonical. On conflict: **orchestrator-rules skill > AGENTS.md prose > per-domain skills own their detail**. Operational state lives in `.harmonik/context/` (captain tiers) and `HANDOFF.md` (this session) — **never in this file**. AGENTS.md is a ROUTER: it points you at the right contract; it does not restate one.

## Per-role load map

Each role loads only its slice. **This map is the reading order.** Where any other file states one, it defers here. Each role's skill stays authoritative for that role's own steps; this is the map across roles. The slices differ on purpose: a captain does not boot-read `AGENT_INDEX.md` or `STATUS.md`, and an implementer-orchestrator has three steps where the captain has four.

- **Captain — cold boot** (see `.claude/skills/captain/STARTUP.md`):
  1. Step 0 — identity + CWD guard.
  2. Step 0a — tier-3 `.harmonik/context/project.yaml` (phase, locked decisions, guardrails).
  3. Step 0b — tier-2 `.harmonik/context/captain-lanes.md` (lanes + epics-in-progress + parked + dated operator directives).
  4. Step 1 — `captain/SKILL.md` + the **`orchestrator-rules` skill** (standing rules) + tier-1 `HANDOFF.md` (a claim, not ground truth).
  5. Step 2 — boot digest = ground-truth; overrides every claim above. Steps 3–6 reconcile / plan / staff / arm watchers.
  - Does **NOT** boot-read: `AGENT_INDEX.md`, `STATUS.md`, product/`docs/` knowledge base, full skill bodies. `.harmonik/context/roadmap.md` only on cold boot / milestone.
- **Captain — keeper-restart resume (LEAN):** re-drain comms → re-read tier-3/tier-2 + ONE boot digest → trust cached tier state as input → re-arm watchers. No heavy re-derive.
- **Crew — minimal load** (see `.claude/skills/crew-launch/SKILL.md`): its mission file (`.harmonik/crew/missions/<crew>.md`) + `crew-launch/SKILL.md` + `agent-comms` + `beads-cli` + `harmonik-dispatch`. Does **NOT** load fleet-level state (roadmap, captain-lanes, project.yaml, orchestrator standing-rules, STATUS, HANDOFF, knowledge base) — scoped to ONE epic + ONE queue.
- **Implementer-orchestrator (main `/session-resume`, non-captain):** `AGENT_INDEX → STATUS → HANDOFF` + the **`orchestrator-rules` skill** (standing rules) + `harmonik-dispatch`. **Three steps, not the captain's four:** `.harmonik/context/captain-lanes.md` is captain-tier, and its own tier header says so.
- **Any session with no role:** `AGENT_INDEX.md` → `STATUS.md` → `HANDOFF.md`.

## Start here

Read [AGENT_INDEX.md](AGENT_INDEX.md) first — the master map of the knowledge base. Then [STATUS.md](STATUS.md) for phase + locked decisions, and [HANDOFF.md](HANDOFF.md) for this-session state. `.harmonik/context/captain-lanes.md` is captain-tier: a captain reads it at boot, and crews and implementer-orchestrators skip it. The load map above is the full statement of who reads what.

**Booting as a captain or crew?** These skills are **project-local under the repo** — read them at `$PROJECT_DIR/.claude/skills/…`, NOT the global `~/.claude/skills/` (no captain/crew skill exists there). Captain: read `.claude/skills/captain/STARTUP.md` FIRST, then `SKILL.md` in that dir. Crew: read `.claude/skills/crew-launch/SKILL.md`. See also `.claude/skills/keeper` (per-session context-watcher) and `.claude/skills/harmonik-lifecycle` (supervise / promote / reconcile / init).

## Standing rules → the `orchestrator-rules` skill

Dispatch discipline (the daily loop, the HARD-RULE exceptions), priority (stated intent first, then the ledger), bead lifecycle (daemon owns terminal transitions; never pre-set in_progress), the review gate, autonomy/flow boundaries, and the major-issue fan-out trigger: all canonical in the **`orchestrator-rules` skill** (`.claude/skills/orchestrator-rules/SKILL.md`). It points to the detail-owner skills; it does not duplicate them.

- **Daily loop / daemon / `queue submit` / `append` / `subscribe`:** the **harmonik-dispatch** skill.
- **Monitoring the daemon** (the canonical Monitor pattern, stream-vs-wave, failure triage): the **harmonik-dispatch** skill.
- **CWD discipline** (never `cd` into a worktree; operate from repo root via `git -C` absolute paths): the **orchestrator-rules** skill.
- **Multi-agent comms** (`harmonik comms` bus; dedupe on `event_id`): the **agent-comms** skill. The `.harmonik/comms/*.md` file-outbox is RETIRED — do NOT write to those files.
- **Lifecycle** (init / supervise / reconcile / promote; work-project deployment, `branching.yaml`): the **harmonik-lifecycle** skill. The daemon merges completed bead branches into `$TARGET_BRANCH`.
- **Keeper** (per-session context-fill watcher): the **keeper** skill.

<!-- END harmonik:managed -->

## Key conventions

- **Operational state lives in tier files, not in this router.** `.harmonik/context/project.yaml` (durable phase + locked decisions), `.harmonik/context/captain-lanes.md` (lane registry + dated directives), `.harmonik/context/roadmap.md` (epic roadmap), `HANDOFF.md` (this-session). Each carries a header declaring who loads it and what must NOT go there.
- **Write guidance as principles, not laws.** An agent treats a rule as a law: it obeys the letter and does strange things to satisfy it. A principle gives a direction to travel and leaves the judgment. When you write into this file, a skill, or a review criterion, prefer "lean toward X because Y" and "treat Z as a smell worth investigating" over "never X". Keep the absolutes for what is genuinely irreversible, for self-dealing, and for a protocol invariant with a silent mechanical consequence — and when you keep one, write the consequence next to it, so a reader can tell a law from a piece of documentation.
- **Cite symbols, not line numbers.** A `file.go:72` reference rots within days and ships as stale guidance. Write `internal/keeper/thresholds.go` `HardCeilingAbsTokens` and let the reader grep.
- **Write prose in plain, simple English.** Short common words, active voice, one instruction per sentence, one name per thing. This covers prose that is not code: docs, specs, plans, skills, commit bodies, PR text, and bead descriptions. It also covers what you say to the operator — give what a thing is, not a bead ID or a codename as its handle.
- **Bead label convention for kerf work codenames:** use the `codename:<name>` prefix (e.g. `codename:my-feature`). Functional/topical labels remain bare — only labels whose sole purpose is to identify a kerf work codename get the prefix.

## Judgment calls

- **A locked decision reopens on the operator's call, and evidence is what earns the conversation.** Bring evidence that the decision is now wrong, say what changed, and put it to the operator. That the current task would be easier the other way is not evidence, and finding good evidence is not the same as having the authority to act on it.
- **An abstraction has to name what it buys.** Name the thing and the abstraction is welcome: a second caller that exists today, a test seam the code has no other way to reach, a port a linter requires, or a boundary a spec draws. The smell is a layer that names nothing — one caller, no test that needed it, no rule that demanded it. Say what it buys in the commit body, and a reviewer can agree or disagree with a stated claim instead of guessing at intent.
- **Read your role's slice.** The per-role load map above is the reading order, and the slices differ on purpose. Read less than yours and you boot on a stale claim. Read more than yours and you spend the context the role needs for its actual job.

<!-- bv-agent-instructions-v2 -->
<!-- end-bv-agent-instructions -->

> **Maintainer note — the marker block above is machine-regenerable, and it is empty on purpose.**
> `br agents --update` rewrites everything between the `bv-agent-instructions-v2` markers from br's
> generic upstream template. This project's issue-tracking guidance therefore lives BELOW the markers,
> where that command cannot reach it. After any `br agents --update`, read the regenerated block and
> delete each claim upstream makes that is false here: (1) the bead ledger is gitignored and
> machine-local, not "stored in `.beads/` and tracked in git"; (2) `git commit -m "..."` alone omits
> the required review trailers; (3) commit-message validation is agent-enforced — git hooks
> (lefthook) are retired, not "wired via `lefthook.yml`"; (4) an agent never claims a bead with
> `br update --status=in_progress` and never closes one with `br close` — the daemon owns terminal
> transitions, and a bead pre-set to `in_progress` stops being dispatchable with nothing reporting an
> error; (5) `kerf next` is not the entry point for what to work on. Diff the block before you accept
> the result.

---

## Issue tracking with beads (`br`)

This project uses [beads_rust](https://github.com/Dicklesworthstone/beads_rust) (`br`) as the task ledger. Each unit of work is a bead.

**The ledger is machine-local.** `.beads/` is gitignored. Beads do not travel between clones and they do not reach CI, and `br sync --flush-only` stages nothing. Do not "fix" this by tracking `.beads/`: `br sync` rewrites the JSONL continuously, a tracked copy leaves the working tree perpetually dirty, and that trips the daemon's `implementer_escaped_worktree` detector and false-fails dispatched beads. Anything that must survive goes in a tracked doc or in the commit message.

### What to work on

Priority comes from stated intent first, then from the ledger. Work the named initiatives of the operator first — the active plan's order and the dated directives in `.harmonik/context/captain-lanes.md`. Below that line, order the unclaimed backlog:

```bash
br ready --sort priority --limit 0                      # whole ready set, highest priority first
br ready --sort priority --parent <epic_id> --limit 0   # scoped to one lane
br ready --sort oldest --limit 0                        # what is starving
```

Pass `--limit 0`. `br ready` returns 20 rows by default and sorts by `hybrid`, so a short default listing is not evidence of a short backlog.

`kerf` plans work; it does not rank work. `kerf map` shows which planned work owns a bead and what context it carries. Do not take an order from `kerf next` — its score comes from graph structure and never reads the `br` priority field, so a P0 bead and a P3 bead come back the same, and it reports empty for a work with no `bead_filter`. Ranking what matters is judgment, and a graph metric cannot do it for you.

**Never run bare `bv`** — it opens an interactive TUI that holds the terminal until a human quits it, so an agent session stops there and nothing reports an error. Use `--robot-*` flags only, and only for graph metrics (`--robot-insights` for PageRank and betweenness, `--robot-graph` for dependency export).

### Reading and creating beads

```bash
br ready              # issues ready to work (no blockers)
br list --status=open # all open issues
br show <id>          # full issue details with dependencies
br create --title="..." --type=task --priority=2
br dep add <issue> <depends-on>
br comment <id> "..."
```

- **Priority**: P0=critical, P1=high, P2=medium, P3=low, P4=backlog. Use the numbers 0-4, not words.
- **Types**: task, bug, feature, epic, chore, docs, question.
- **Dependencies**: a bead can block another. `br ready` shows only unblocked work.

### The daemon owns the terminal transitions

**Do not set a bead to `in_progress`, and do not close one yourself.** The daemon claims a bead when it dispatches it and closes it when the work merges. A bead an agent pre-set to `in_progress` is no longer dispatchable, and the daemon cannot tell that state from a live run, so the work stalls and nothing reports an error. Creating beads, commenting on them, and adding dependencies are yours.

### Committing

Commit with `git commit -F <file>`, not `-m`. Non-trivial commits carry `Reviewed-By:` and `Review-Verdict:` trailers, and `-m` cannot write them. Validation is agent-enforced — there are no git hooks, and `--no-verify` is forbidden. A `BLOCK` verdict is never committed. Quote the reviewer's verdict verbatim and never author your own approval. If no reviewer can be reached, record that fact in the trailer with no verdict and commit anyway: work stranded in a worktree is one `checkout` from gone, and a commit labelled "not reviewed" is a state the next person can act on.

At session end, run `br sync --flush-only` to keep the local database and the JSONL consistent. Expect it to stage nothing.
