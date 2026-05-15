<!-- PP-TRIAL:v2 2026-05-14 extqueue-v0.1-specs — v42. Whole 8-pass kerf cycle for extqueue v0.1 landed on a feature branch. 6 spec files (1 new + 5 edits) authored via 22 sub-agent invocations across research/design/draft/review rounds. Branch `extqueue-v0.1-specs` pushed; main is unchanged. PR not opened. -->

Roadmap: [ROADMAP.md](ROADMAP.md) — high-level epic order from current state to fully operational.

<!-- ORCHESTRATION DIRECTIVES — DO NOT EDIT EXCEPT BY EXPLICIT USER REQUEST. Loaded every /session-resume. -->

ROLE. You are the orchestrator. Delegate substantively. Keep the main thread minimal.

LEARNING LOG (READ ON EVERY RESUME). `docs/orchestration-learnings.md` — friction-and-fix log. Read on `/session-resume`. Append new entries when you observe friction. Promote durable rules to this directives block or `.claude/implementer-protocol.md`.

STREAM-NOT-WAVES (HARD RULE). The orchestrator runs a CONTINUOUS STREAM of implementers, not synchronous waves. On every implementer-completion notification, do exactly two things, in order: (1) Merge the returning implementer; (2) inspect dispatchable depth and either spawn one replacement OR note "queue draining" and stop spawning.

Per-return acknowledgment is ≤2 lines. Full session summary lives at `/session-handoff` time.

PHASE 2 IS UNBLOCKED (NEW v38). With harmonik operational you CAN now dispatch beads via the daemon instead of via the Agent tool — file a bead with `br create`, start harmonik against the project, watch it execute. Trade-off: harmonik overhead is ~30s+ per bead vs sub-agent's seconds; use it when (a) durability matters, (b) the work spans sessions, (c) tmux inspectability matters, or (d) parallel `--max-concurrent N` amortizes the overhead. For trivial inline work, sub-agent dispatch still wins.

IMPLEMENTER COMMIT DISCIPLINE (REINFORCED v38). Most implementers in the v38 session ran self-review APPROVE BUT NEVER COMMITTED in their worktree. The orchestrator had to commit-on-behalf. Briefs MUST end with "COMMIT EXPLICITLY (`git add` + `git commit`) before exiting" and the orchestrator MUST verify the commit landed before merging. If diff is uncommitted, the orchestrator stages + commits on behalf using `Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>`.

WORKTREE TASK-INJECTION LEAK (v36, ONGOING). Implementer edits leak into main's working tree as uncommitted changes. Workaround: `git stash push -m "v36-leak ..." && git merge --ff-only <branch> && git stash drop`. Never commit the leaked main-tree edits as a separate commit — the proper changes arrive via the worktree branch merge.

WORKTREE AUTO-REMOVED BY HARNESS (v41 NEW). When an implementer agent finishes, the harness may auto-remove its worktree directory (but NOT the branch). If `git -C <wtpath>` returns `cannot change to directory`, the worktree is already gone — just `git merge --ff-only worktree-agent-<id>` directly from main. The branch can be rebased without a checked-out worktree by creating a temporary worktree at `/tmp/wt-<short>`, rebasing there, merging into main, then removing the temp worktree. Pattern used 3x in v41.

ISOLATED-WORKTREE STALE-BASE BUG (v35, ONGOING). Every implementer dispatched with `isolation: "worktree"` MUST be told in its brief to:

    cd <your worktree path>
    git fetch origin
    git rebase main

BEFORE reading any spec or code. Verify base via `git log --oneline -5`.

WORKTREE BEADS-JSONL LEAK (v41 PATTERN). Implementers' `br close` writes to `.beads/issues.jsonl` in the worktree, which then conflicts with rebase. Workaround in the merge dance: `git -C "$WTPATH" stash push -m leak && git -C "$WTPATH" rebase main` BEFORE the ff-merge. The stash is intentionally never popped — the JSONL state on main wins.

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

PUSH AUTONOMY (v40 2026-05-14). User lifted the "ask before push" constraint. Orchestrator pushes `origin main` after merge dance + tests-green without confirmation. Destructive-op rules (force-push, reset --hard, branch -D, --no-verify) STILL require confirmation; only the routine push step is lifted.

NO CI (v41 2026-05-14). User explicitly does NOT want GitHub Actions. The `ci-workflows-hk-4tttc` side branch was dropped in v41 and `.github/workflows/` does NOT exist in main. Do not propose CI workflow files in future work. Scenario tests run locally only.

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
Use `git -C /Users/gb/github/harmonik` for ALL git ops AND read absolute paths to avoid bash-cwd drift inside worktrees. Verify `pwd` returns `/Users/gb/github/harmonik` before any build/test command. v41 hit CWD-disappeared errors 3x when removing a worktree the shell was sitting inside — always `cd /Users/gb/github/harmonik &&` before any worktree-remove.

MERGE DANCE — RUN FROM `/Users/gb/github/harmonik`.

    cd /Users/gb/github/harmonik
    for id in <agent-id-1> <agent-id-2>; do
      WTPATH="/Users/gb/github/harmonik/.claude/worktrees/agent-$id"
      BRANCH="worktree-agent-$id"
      # v41 pattern: stash leak then rebase BEFORE ff-merge
      [ -d "$WTPATH" ] && git -C "$WTPATH" stash push -m leak
      [ -d "$WTPATH" ] && git -C "$WTPATH" rebase main
      git -C /Users/gb/github/harmonik merge --ff-only "$BRANCH"
      git -C /Users/gb/github/harmonik worktree remove --force --force "$WTPATH" 2>/dev/null
      git -C /Users/gb/github/harmonik branch -d "$BRANCH"
    done

CONTEXT BUDGET (orchestrator). ~700 k effective. v41 used ~50% across 3 waves. v42 used ~53% across one 8-pass kerf cycle (no implementer waves).

<!-- END DIRECTIVES -->

# Where we are (v42, 2026-05-14)

**Branch `extqueue-v0.1-specs` at commit `20d362d`, pushed to origin. Main is untouched.**

Whole 8-pass kerf spec cycle (codename `extqueue`) ran in one session. Output: a feature branch carrying the v0.1 spec package for an external-orchestrator queue control surface — the daemon no longer picks beads itself; an external agent submits an ordered queue of waves and streams via CLI; daemon executes it. v0.1 surface = `queue-submit / queue-append / queue-status / queue-dry-run`. v0.2 deferrals (remove/pause/resume/clear) explicitly scoped out.

Files on the branch (945 insertions, 96 deletions vs main):
- `specs/queue-model.md` (NEW, 604 lines)
- `specs/execution-model.md` — §7.4 dispatch loop rewritten; new EM-015f / §4.11 concurrency primitives
- `specs/beads-integration.md` — BI-013 demoted; BI-013b/c submit-time read surface
- `specs/process-lifecycle.md` — PL-003a method-set extended; `enqueue` retired; PL-013 retired-with-stub
- `specs/event-model.md` — 6 new `queue_*` events; optional `queue_id` on `run_*`
- `specs/operator-nfr.md` — ON-015 reframed; 9 surgical amendments

Kerf artifacts (problem space → tasks, plus 4 reviewer rounds) at `.kerf/extqueue/` — gitignored per project convention.

# Next session — START HERE

**The branch is awaiting your call.** Three options, plain English:

1. **Open a PR and review the spec text yourself.** No agent action needed. The 6 spec files are the product; they need a human read before merge. The PR-ready URL printed by `git push` was `https://github.com/gregberns/harmonik/pull/new/extqueue-v0.1-specs`.

2. **File the 22 implementation tasks as beads and start churning.** Implementation decomposition lives at `.kerf/extqueue/07-tasks.md` — 22 tasks across 8 tiers with a full DAG and parallelization plan. Critical path ~7 implementer cycles; Wave 3-4 fans out to 4-5 parallel implementers. If the user says "file the beads," the agent runs through 07-tasks.md and creates one bead per Txx with the bead body matching the task's scope + acceptance criteria.

3. **Pause the extqueue work and come back to Phase 2 first-demo.** The previous session (v41) filed `hk-ftyvo` — a Phase-2 bug: the daemon's auto-close path doesn't merge run branches back to main. That bead is still open and is the actual Phase-2 round-trip blocker. extqueue is the longer-term scheduling rework; `hk-ftyvo` is the shorter-term "make the daemon actually finish a round-trip" fix.

If the user opens the next session with **"resume"** and no further direction, default to option 2 (file beads, prepare for implementation) — that's the path that converts the spec work into runnable code.

# Files to open first

1. `HANDOFF.md` (this).
2. `specs/queue-model.md` — the foundation spec (new, 604 lines).
3. `.kerf/extqueue/01-problem-space.md` — the locked decisions D1-D6.
4. `.kerf/extqueue/05-changelog.md` — the package-level changelog with all amendment summaries + flagged residuals.
5. `.kerf/extqueue/07-tasks.md` — the 22-task implementation plan.
6. `.kerf/extqueue/SESSION.md` — narrative of the v42 session (what each pass produced).

# Notes for the next agent

- `hk-ftyvo` (Phase 2 bug: daemon doesn't merge run branches to main) is independent of extqueue and still open. It blocks Phase 2 round-trip regardless of which path above is chosen.
- `hk-09tne` (Phase 2 first-demo bead) is reopened and blocked on `hk-ftyvo` — that's intentional.
- The previously deferred `hk-gql20`, `hk-kqdpf`, `hk-fdyip`, `hk-1n0cw`, `hk-kqdpf.5` beads had their `defer_until` cleared during the v42 demo cleanup; their priorities are also restored.
- One pre-existing miscitation noted by the integration audit: `beads-integration.md:205` cites WM-007 in §4.5 but it's actually in §4.2. Out of scope for extqueue; flag for a future housekeeping pass.
