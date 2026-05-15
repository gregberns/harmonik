<!-- PP-TRIAL:v2 2026-05-15 main — v43. Phase 2 first-demo GREEN; extqueue v0.1 implementation ~80% done (19 of 24 task beads landed including T50 workloop rewrite); ROADMAP.md added at repo root. T80/T81/T82 scenario tests are the immediate next dispatches — they gate Roadmap Row 8 (Phase 2 multi-bead E2E). -->

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
On `/session-resume` with no hard blocker, EXECUTE — don't close the say-back with an A/B question. Sub-agents inherit via `.claude/implementer-protocol.md`. EXCEPTION: spec-text authoring is user-shaping; check in before dispatching agents that will write normative spec sections. (v43 refinement: SMALL spec amendments may dispatch without check-in; only check in for SIGNIFICANT/architectural changes.)

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

CONTEXT BUDGET (orchestrator). ~700 k effective. v41 used ~50%. v42 used ~53%. v43 used ~51% across heavy implementer-stream + 3 review rounds + multi-wave dispatch.

<!-- END DIRECTIVES -->

# Where we are (v43, 2026-05-15)

**Main at HEAD (will be updated to post-T50-merge SHA before this handoff is used). All work pushed to origin. Working tree clean. Big session — ~25 commits landed.**

## Headline outcomes

1. **Phase 2 first-demo GREEN** — bead `hk-09tne` ran end-to-end through the daemon (claude → commit on run-branch → daemon merge-to-main → outcome_emitted + bead_closed). Commit `d50393b`. Unblocker was `hk-ftyvo` (added EM-052/053: merge-to-main + non-FF reopen) plus `hk-4goy3` (added EM-054: working-tree refresh after update-ref).
2. **Extqueue v0.1 spec landed and gap-filled.** Original spec `e228bc3`; v0.1.1 gap-fix `cfb55a0` closed 6 wire-contract + recovery gaps surfaced by a 3-reviewer pass (completeness / failure-modes / feasibility). New section §2.10 has the JSON-RPC request/response RECORDs; §6.11a QM-029b has the error-code mapping; §3.2a QM-002a has the startup Beads cross-check.
3. **Extqueue v0.1 implementation ~80% done.** Epic `hk-lj0pb` + 24 task beads filed and reviewed (DAG + scope APPROVE after 6 bead-body sharpenings). Landed: T03, T10, T11, T12, T20, T21, T30, T31, T32, T40, T41, T42, T50, T60, T61, T62, T70, T83, T84. **T50 (workloop rewrite, P0) shipped in `3b53a8e`** — `br ready` poll replaced by `EligibleItems()` pull from active queue group; EM-015f group-advance gate; `complete-with-failures` → `paused-by-failure` + `queue_paused`; QueueID/QueueGroupIndex stamped on run_* payloads; backward-compat fallback to `br ready` when no queue is loaded. T80/T81/T82 (scenario tests, the Phase 2 multi-bead E2E gate) are now dispatchable.
4. **HC-055b worktree auto-trust** — picked Candidate 3 from the bead body. Daemon now injects `--dangerously-skip-permissions` only when launching into a path that canonicalizes to under the harmonik worktrees prefix. Replaces the prior `dangerouslyAllowedPermissions` settings.json hack.
5. **ROADMAP.md added at repo root** — 11 ordered rows from Phase 1 GREEN to Phase 3 (DOT-defined bead processes). Referenced from this HANDOFF.
6. **EV-002b sensor narrowed** — `internal/handler/launchspecdelivery_hc005_test.go` is a test file, not a handler subprocess, so importing core was a false positive. Sensor now skips `_test.go` files. Bead `hk-59ob6`.
7. **Other code landings**: `hk-nvrvp` (HARMONIK_PROJECT_HASH env injection), `hk-mz0x4` (ldflags binary_commit_hash + Makefile), `hk-a6nob` (envelope run_id on emitRunStarted/emitRunCompleted), `hk-8mwo.33` (sidecar-walk WM-022). **Sensors**: HC-INV-003 (`hk-8i31.66`), ON-INV-006 (`hk-sx9r.72`), ON-019 (`hk-sx9r.23`), WM-INV-003 (`hk-8mwo.57`), WM-009 (`hk-8mwo.14`). **Spec amendments**: BI-010d activity-marker/truth-claim split (`hk-iuaed.1`), handler-contract front-matter fix (`hk-4woeq`).

## Stream / dispatch state at handoff time

- Stream **drained** — no implementer agents running. Working tree clean. Main pushed.
- Queue draining note: T50 was the last in-flight agent; it landed in `3b53a8e` + `1873195`.

# Next session — START HERE

## Immediate plan (Roadmap Row 4 → Row 8)

1. **Dispatch T80 (`hk-8vokz`, P0)** — Phase 2 multi-bead E2E scenario tests. This is the gate test for the entire extqueue v0.1 milestone. Brief should cite scenario-harness.md for testing convention + `internal/queue/state.go` + `internal/daemon/workloop.go` (post-T50) for assertions. Acceptance: run a 3–5 bead queue via `hk queue submit`, observe each run_started + run_completed has non-nil QueueID/QueueGroupIndex, observe group-advance gate held until all-terminal, observe queue_paused on synthetic failure.
2. **Then T81 (`hk-2gqua`) and T82 (`hk-30wgn`)** — additional scenario tests; can dispatch in parallel after T80 lands (or in parallel WITH T80 since they touch sibling test files).
3. **Optional T71 (`hk-dji5z`)** — small cleanup bead in workloop.go (replaces nothing-ready-sleep with socket-block idle). P2; can fold into T50 review or do as separate cleanup.
4. **Optional T51 (`hk-w85to`)** — annotation-only; consider closing as SUBSUMED by T50 if the implementer already added the EM-049/050/051 godoc.

## After Row 8 — Roadmap Row 5 (Bridge cluster)

- **`hk-gql20`** (bridge-integration epic, P0), **`hk-kqdpf`** (bridge-followup epic, P0), **`hk-lj1p9`** (claude session lifecycle parent). These have many open child beads — re-check `bv --robot-triage --label hk-gql20` (or equivalent) for current dispatchable children.
- The daemon audit (commit `e17cd39`) noted: `workLoopDeps.substrate = nil` in composition root (claude is still spawned via `exec.CommandContext`, not tmux panes); review-loop lacks `waitAgentReady`; orphan session sweep is window-only.

## Known-failing tests (NOT blocking — confirmed pre-existing on main)

- `TestAR013EnvelopeDeclaration` in specaudit — `queue-model.md` and `claude-hook-bridge.md` lack the §4.a Subsystem envelope section. Cosmetic spec hygiene.
- `TestON027DrainStep1StopPullingQueue/check-4` — `operator-nfr.md` drain step wording mismatches the sensor's expected phrase. Cosmetic.
- `TestWorkLoop_FailedHandlerReopensBead` and `TestWorkLoop_TwoConcurrentBeads` — pre-existing flaky workloop tests; pass in isolation but time out under full-suite parallel load. Triage as part of Row 5 work, not blocking.

## Files to open first

1. `HANDOFF.md` (this).
2. `ROADMAP.md` (the 11-row plan).
3. `specs/queue-model.md` (v0.1.1 with §2.10 + §6.11a + §3.2a).
4. `internal/queue/state.go` + `internal/daemon/workloop.go` (post-T50) — your next implementers will read these.
5. `.kerf/extqueue/07-tasks.md` — for context on T80/T81/T82 scope.

## Question that blocks the next session

None. Continue executing per directives + roadmap.
