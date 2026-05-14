<!-- PP-TRIAL:v2 2026-05-14 main — v39. Operational-green pushed to origin (HEAD `3a5d36a`). 9 design beads filed across branching, model selection, scenario testing. User aligned on plan; **branching is the next active work item**. Working tree clean. -->

<!-- ORCHESTRATION DIRECTIVES — DO NOT EDIT EXCEPT BY EXPLICIT USER REQUEST. Loaded every /session-resume. -->

ROLE. You are the orchestrator. Delegate substantively. Keep the main thread minimal.

LEARNING LOG (READ ON EVERY RESUME). `docs/orchestration-learnings.md` — friction-and-fix log. Read on `/session-resume`. Append new entries when you observe friction. Promote durable rules to this directives block or `.claude/implementer-protocol.md`.

STREAM-NOT-WAVES (HARD RULE). The orchestrator runs a CONTINUOUS STREAM of implementers, not synchronous waves. On every implementer-completion notification, do exactly two things, in order: (1) Merge the returning implementer; (2) inspect dispatchable depth and either spawn one replacement OR note "queue draining" and stop spawning.

Per-return acknowledgment is ≤2 lines. Full session summary lives at `/session-handoff` time.

PHASE 2 IS UNBLOCKED (NEW v38). With harmonik operational you CAN now dispatch beads via the daemon instead of via the Agent tool — file a bead with `br create`, start harmonik against the project, watch it execute. Trade-off: harmonik overhead is ~30s+ per bead vs sub-agent's seconds; use it when (a) durability matters, (b) the work spans sessions, (c) tmux inspectability matters, or (d) parallel `--max-concurrent N` amortizes the overhead. For trivial inline work, sub-agent dispatch still wins.

IMPLEMENTER COMMIT DISCIPLINE (REINFORCED v38). Most implementers in the v38 session ran self-review APPROVE BUT NEVER COMMITTED in their worktree. The orchestrator had to commit-on-behalf. Briefs MUST end with "COMMIT EXPLICITLY (`git add` + `git commit`) before exiting" and the orchestrator MUST verify the commit landed before merging. If diff is uncommitted, the orchestrator stages + commits on behalf using `Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>`.

WORKTREE TASK-INJECTION LEAK (v36, ONGOING). Implementer edits leak into main's working tree as uncommitted changes. Workaround: `git stash push -m "v36-leak ..." && git merge --ff-only <branch> && git stash drop`. Never commit the leaked main-tree edits as a separate commit — the proper changes arrive via the worktree branch merge.

ISOLATED-WORKTREE STALE-BASE BUG (v35, ONGOING). Every implementer dispatched with `isolation: "worktree"` MUST be told in its brief to:

    cd <your worktree path>
    git fetch origin
    git rebase main

BEFORE reading any spec or code. Verify base via `git log --oneline -5`.

TRUST `br ready` BUT VERIFY (HARD RULE — L-011, L-017).
`br ready` is not authoritative for "the corpus is drained":
  1. Stale `blocked_issues_cache` (L-011): cross-check `br stats` Open vs Ready. Recovery: `br doctor --repair`.
  2. Parent-child gridlock (L-011): convert via sqlite3:
       sqlite3 .beads/beads.db "UPDATE dependencies SET type='related' WHERE issue_id='<id>' AND depends_on_id='<parent>';"
       br doctor --repair
       git checkout -- .gitignore  # br doctor --repair strips .beads/* ignore line
  3. Stale `defer_until` (L-017): clear via `br update <id> --defer ""`.

DON'T ASK — EXECUTE.
On `/session-resume` with no hard blocker, EXECUTE — don't close the say-back with an A/B question. Sub-agents inherit via `.claude/implementer-protocol.md`. EXCEPTION: spec-text authoring is user-shaping; check in before dispatching agents that will write normative spec sections.

IMPLEMENTER LIFECYCLE — ENFORCED IN PROTOCOL.
`.claude/implementer-protocol.md` is authoritative. (a) Implementer CLOSES OWN BEADS via `br close` after each commit. (b) Implementer DOES THE BEADS NAMED IN ITS BRIEF AND EXITS — no free-claiming. (c) Implementer DOES NOT ASK questions back. (d) **Implementer COMMITS EXPLICITLY** (v38 reinforcement).

DISPATCH SHAPE.
- Implementers: `model=sonnet`, `effort=high`, `isolation=worktree`, `run_in_background=true`. **REBASE FIRST per the hard rule.**
- Reviewers: `model=sonnet`, `effort=high`, no isolation.
- Briefs ≤15 lines: see brief-template appendix in `.claude/implementer-protocol.md`. **Do NOT paraphrase the bead body.** Implementer fetches via `br show`.

PRE-FLIGHT (orchestrator, ≤3 reads per dispatch).
- Bead body via `br show <id> --format json`.
- The cited spec section or roadmap row.
- ONE canonical sibling for pattern conventions.

CWD DISCIPLINE.
Use `git -C /Users/gb/github/harmonik` for ALL git ops AND read absolute paths to avoid bash-cwd drift inside worktrees. Verify `pwd` returns `/Users/gb/github/harmonik` before any build/test command.

MERGE DANCE — RUN FROM `/Users/gb/github/harmonik`.

    cd /Users/gb/github/harmonik
    for id in <agent-id-1> <agent-id-2>; do
      WTPATH="/Users/gb/github/harmonik/.claude/worktrees/agent-$id"
      BRANCH="worktree-agent-$id"
      git -C "$WTPATH" rebase main
      git -C /Users/gb/github/harmonik merge --ff-only "$BRANCH"
      git -C /Users/gb/github/harmonik worktree remove --force --force "$WTPATH"
      git -C /Users/gb/github/harmonik branch -d "$BRANCH"
    done

CONTEXT BUDGET (orchestrator). ~700 k effective. v38 used ~30% — heavy session.

<!-- END DIRECTIVES -->

# Where we are (v39, 2026-05-14)

**Main at `3a5d36a`, pushed to origin. Nothing in flight. Working tree clean.**

Phase 1 (operational harmonik) landed and is pushed. Phase 2 (orchestrator dispatches via harmonik instead of via the Agent tool) is unblocked but not demonstrated. Phase 3 (DOT-defined bead processes) not started. See `docs/dogfood-smoke-run-2026-05-14-operational-green.md` for the milestone evidence and the 11-fix table.

## What just happened in v39 (this short session)

The user reviewed the Phase 1 milestone, signed off on pushing 63 commits to origin, and raised three forward threads. Three Opus research agents went out and filed **9 new design beads** capturing the design space (no implementation):

- **Branching (`hk-8m91i` epic + `hk-oe6zt`, `hk-icgp1`, `hk-zy9s3` children).** Found a real bug: spec WM-005 says the worktree branches from "the integration branch" but code branches from raw HEAD. User clarified the actual flow: each bead carries a `start_from` (cut here) and a `lands_on` (merge here) — usually the same, but they can differ, and multiple integration branches can be in flight simultaneously. Carrier: `.harmonik/branching.yaml` for project defaults + structured `## Branching` section in bead body for per-bead override.
- **Model selection (`hk-cfhj2` epic + `hk-xo03m`, `hk-bfvk7` children).** 4-tier resolution chain (bead → per-project → per-agent-type default → daemon default), mirroring EM-012a. User added: harmonik passes a *structured model preference descriptor* to the handler; the handler validates against what its underlying tool accepts. Harmonik does NOT keep a closed enum of model names — future harnesses (Codex, pi) can take arbitrary model strings.
- **Scenario testing (`hk-wuu5h` epic + `hk-cno1z`, `hk-8ys88`, `hk-mg1ya`, `hk-4tttc` children).** Twin already exists (`cmd/harmonik-twin-claude`, YAML-script driven, NDJSON over UDS). Covers 7 of 11 umbrella fixes today; 4 need twin extensions. User clarified the twin's job is **behavioral parity, not visual parity** — no TUI rendering, but it MUST read the worktree's `.claude/settings.json` and call the hook commands the same shape real claude would.

# Next session — START HERE

**Active work item: branching (`hk-8m91i` epic).** Sequence within the epic:

1. Spec amendment to `specs/workspace-model.md §WM-005`. Make the `start_from` / `lands_on` asymmetry explicit. Spec authoring is user-shaping per the directives block — DRAFT for the user, do not silently land normative text. The plain-language description in v38's bead body (search `hk-8m91i`) is the source. A few questions the user has already answered — re-read v38 chat history or the bead notes if available.

2. Then `hk-oe6zt` — daemon worktree factory reads `start_from`. Code-only impl after spec lands.

3. Then `hk-zy9s3` — `.harmonik/branching.yaml` schema + loader.

4. Then `hk-icgp1` — landing-strategy selector (merge vs cherry-pick).

After branching closes: scenario testing (`hk-wuu5h`), then first real Phase 2 demo, then model selection, then DOT. The user said model can slip later if needed.

# Files to open first

1. `HANDOFF.md` (this).
2. `docs/dogfood-smoke-run-2026-05-14-operational-green.md` — the milestone artifact.
3. `specs/workspace-model.md` §WM-005, WM-019 — the branching spec being amended.
4. `internal/daemon/workloop.go` lines ~904-942 (`productionWorktreeFactory`, `resolveHEAD`) — where the bug actually lives.
5. `br show hk-8m91i --format json` — branching epic with the design analysis embedded.
