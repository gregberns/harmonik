<!-- PP-TRIAL:v2 2026-05-14 main — v40. Three epics closed in one session: branching (hk-8m91i), scenario-testing (hk-wuu5h), model-selection (hk-cfhj2). 14 commits on main pushed to origin (HEAD `952f7eb`). 1 commit on side branch `ci-workflows-hk-4tttc` AWAITING USER PUSH (OAuth token lacks workflow scope). Phase 2 first-real-demo is the next obvious step. -->

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

PUSH AUTONOMY (v40 2026-05-14). User lifted the "ask before push" constraint. Orchestrator pushes `origin main` after merge dance + tests-green without confirmation. Destructive-op rules (force-push, reset --hard, branch -D, --no-verify) STILL require confirmation; only the routine push step is lifted.

CI-WORKFLOW PUSH CAVEAT (v40 2026-05-14). The orchestrator's OAuth token lacks GitHub `workflow` scope — pushes that modify `.github/workflows/*` files are REJECTED at remote. Process: keep workflow-file changes on a side branch (`ci-workflows-<bead>`), surface to user, user pushes manually. Do NOT include workflow-file changes in main pushes.

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

# Where we are (v40, 2026-05-14)

**Main at `952f7eb`, pushed to origin. Working tree dirty only with HANDOFF + beads.jsonl session-end churn. One side branch `ci-workflows-hk-4tttc` awaits user push (OAuth scope).**

Phase 1 (operational harmonik) shipped in v38. v40 closed three Phase-2-prerequisite epics in parallel:

- **Branching (`hk-8m91i`)** — closed. 5 children landed: spec amendment to WM-005/005b/006/008/019 + new BI-009b parse contract; new `internal/branching` package for `.harmonik/branching.yaml` defaults; daemon factory now cuts worktrees from the bead's `start_from` ref instead of raw HEAD; landing-strategy selector adds cherry-pick alongside squash; full WM-005b resolution chain wired at claim time.
- **Scenario-testing (`hk-wuu5h`)** — closed. 5 children + 1 spec-doc follow-up: twin parity audit doc (5 fixes twin-feasible today, 4 extension-needed, 2 real-claude-only); twin extended with `--worktree-path`, settings.json reader, Stop hook caller, `commit_on_cue`, `startup_delay_ms`; scenario harness at `test/scenario/` boots daemon+twin and asserts event sequences for 5 fixes; CI workflows for PR-tier smoke and nightly full suite (side-branched pending push).
- **Model selection (`hk-cfhj2`)** — closed. 3 children landed: spec amendment EM-012b (4-tier resolution chain) + HC-055a (ModelPreference invariant, value-opacity); `claudelaunchspec.go` accepts `--model`/`--effort` with shape validation; `.harmonik/config.yaml` loader + tier-3 compiled defaults + claim-time resolution wired into `claudeRunCtx`.

## CI push pending (operator action required)

The branch `ci-workflows-hk-4tttc` contains commit `4022bca` which adds `.github/workflows/ci.yml` (PR-tier scenario smoke) and `scenario-nightly.yml` (nightly full suite). The orchestrator's OAuth token cannot push workflow files (no `workflow` scope). **Run `git push origin ci-workflows-hk-4tttc` from your own shell, then merge that branch into main locally and push** (or open a PR from it). Until then, scenario tests still run locally but CI does not enforce them.

## v40 process notes

- The "ask before push" constraint is lifted (HANDOFF directive PUSH AUTONOMY). CI-workflow caveat is the only remaining gate.
- Recurring friction: CWD drifts into agent worktrees during long bash chains — `cd /Users/gb/github/harmonik && pwd` recovery pattern used repeatedly. Worth a directive note for the next agent.
- Beads parent-child-as-blocker gridlock (L-011) hit twice in v40 — once for branching children, once for model children. Sqlite-flip `blocks → related` for parent edges remains the standard recovery.

# Next session — START HERE

**Phase 2 first-real-demo.** Phase 1 proved harmonik can drive a real claude end-to-end on one bead (operational green). Phase 2 swaps the orchestrator's dispatch substrate: instead of the Agent tool spawning sub-agents, the orchestrator files real beads and runs the harmonik daemon against them. The branching/model/scenario infrastructure that just landed is the prerequisite stack — branching gives team-friendly base/target refs, model selection lets each bead pick its harness model, scenario tests give regression coverage.

The pragmatic first demo: pick a small concrete bead, dispatch it through harmonik daemon with `start_from: main`, `lands_on: main`, `model: sonnet`, `effort: high`, watch the resulting JSONL stream and merge commit on main. If that round-trips end-to-end, file a parallel pair of beads and run `harmonik --max-concurrent 2`. That's the proof point for Phase 2 entry.

Open follow-ups (not blocking):
- Phase 2 demo bead — pick one and run it (no bead filed yet — file at session start).
- DOT-defined node graphs (Phase 3).
- Twin paste-receipt + reject-input-before-ready (audit items 4+5) — small twin extensions, low priority since current scenarios don't exercise them yet.

# Files to open first

1. `HANDOFF.md` (this).
2. `docs/dogfood-smoke-run-2026-05-14-operational-green.md` — Phase 1 milestone.
3. `docs/twin-parity-audit-2026-05-14.md` — twin coverage map; informs which scenarios the daemon-driven demo can rely on.
4. `specs/workspace-model.md` §WM-005b — bead-level branching contract (new).
5. `specs/execution-model.md` §EM-012b — model-selection resolution chain (new).
6. `internal/branching/branching.go` — project-defaults loader.
7. `internal/daemon/branching.go` — bead-body parser + resolution-chain wiring.
8. `internal/daemon/projectconfig.go` + `modelpreference.go` — model resolution.
9. `test/scenario/scenarios_test.go` — 5 baseline scenarios that should keep passing as the daemon evolves.
