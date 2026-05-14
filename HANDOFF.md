<!-- PP-TRIAL:v2 2026-05-14 main — v41. Three implementer waves (8 + 5 + 4), 14 new beads closed (12 commits, 2 SUBSUMED), stale CI branch dropped, root MD archived. HEAD `7c54c76`, pushed. Zero-blocker code backlog drained — remaining ready work is spec-authoring (needs user check-in) or parent-child gridlock (needs L-011 sqlite-flip). Phase 2 first-demo bead `hk-09tne` filed and awaiting daemon-driven run. -->

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

CONTEXT BUDGET (orchestrator). ~700 k effective. v41 used ~50% across 3 waves.

<!-- END DIRECTIVES -->

# Where we are (v41, 2026-05-14)

**Main at `7c54c76`, pushed to origin. Working tree clean.**

v41 ran three implementer waves and closed 14 beads (12 new commits, 2 SUBSUMED). All waves drained the zero-blocker leaf-code backlog. The next session should pivot from sub-agent dispatch to either: (a) running harmonik daemon on `hk-09tne` (the Phase 2 first-demo), or (b) breaking parent-child gridlock to surface a wave-4 batch.

## v41 waves

**Wave 1** (8 dispatched, 7 commits + 1 SUBSUMED):
- `hk-hqwn.44` source_subsystem registry → `2e9ce19`
- `hk-hqwn.49` sync-consumer cardinality + acyclicity sensors → `f311f12`
- `hk-hqwn.50` UUIDv7 monotonic event_id → SUBSUMED by `hk-hqwn.62` / `5ca45d9`
- `hk-hqwn.52` EV-INV-006 redaction + structural check → `4e694b5`
- `hk-hqwn.59.27` session_log_location payload → `5aead46`
- `hk-hqwn.59.78` redaction_failed payload → `c7f2fc3`
- `hk-8i31.27` mechanism-tagged error classification (HC-023) → `04aeb01`
- `hk-8i31.41` HC-034 no-secret-in-JSONL test → `0ddc097`

**Wave 2** (5 dispatched, 5 commits):
- `hk-mmvcm` JSONLWriter.Close idempotent → `1ae9782`
- `hk-6x7dw` rename StaleVerdictPayload.SnapshotToken → `1ff0c86`
- `hk-xlach` CHB-INV-003 mechanism-no-cognition → `514c0f6`
- `hk-gerqr` CHB-INV-001 two-contributor session → `79e7f19`
- `hk-qo96c` CHB-INV-002 single terminal event → `8956ebc`

**Wave 3** (4 dispatched, 3 commits + 1 SUBSUMED):
- `hk-qo08q.22` CHB-022 daemon-is-twin-blind → `7c54c76`
- `hk-qo08q.23` CHB-023 session_id durable checkpoint → SUBSUMED by `hk-w5vra.6` / `1b88110`
- `hk-qo08q.24` CHB-024 settings-precedence → `be91ba6`
- `hk-u5c5i` sub_reason enum → `b939afe`

## Cleanup

- Stale `ci-workflows-hk-4tttc` branch dropped (CI explicitly unwanted).
- Orphan `worktree-agent-ae97d05df4ee78078` removed (its `hk-gql20.11` work already in main via different commit).
- Root MD archived to `docs/historical/`: `OVERNIGHT_RUN_2026-04-19.md`, `MVH_ROADMAP.md`, `QUESTIONS.md`, `EXPLORATORY_TESTING_PLAN.md`.
- Root MD deleted: `NEXT_AGENT.md`, `SESSION_HANDOFF.md` (superseded by this file).
- Stale closeable beads: `hk-zrj83`, `hk-gql20.23`, `hk-w5vra.7` (closed in cleanup commit).

## Follow-up beads filed in v41

- `hk-09tne` — Phase 2 first-demo: append milestone line to README via harmonik daemon end-to-end. P2 docs.
- `hk-6x7dw` — EV-036 violation (CLOSED in wave 2).
- `hk-mmvcm` — JSONLWriter close-of-closed-channel panic (CLOSED in wave 2).

# Next session — START HERE

**Two viable next moves.** Pick one based on what you want to learn.

**Option A — Phase 2 first-demo run (RECOMMENDED).** Drive `hk-09tne` end-to-end through the harmonik daemon (NOT via sub-agent dispatch). This is the v40-handoff's stated Phase-2 entry test. Steps:

    cd /Users/gb/github/harmonik
    go build -o /tmp/harmonik ./cmd/harmonik
    # In a tmux session:
    /tmp/harmonik tmux-start --session-name harmonik-phase2 --project /Users/gb/github/harmonik
    # Inside the resulting tmux session:
    /tmp/harmonik --project /Users/gb/github/harmonik --max-concurrent 1

The daemon should poll `br ready`, claim `hk-09tne`, spawn a real claude in a tmux window, watch the work happen, and merge to main. Success signal: a merge commit on main authored by the implementer + `outcome_emitted{kind=approved}` in `.harmonik/events/events.jsonl`. If round-trip fails, file a bug bead with the failing-step name and the JSONL excerpt. Pre-flight blockers: `hk-zrj83` (paste-inject), `hk-fdyip` (auto-trust) — paste-inject is closed; auto-trust still has an open design question, but the operational-green smoke ran without it being formally resolved so the demo should work.

**Option B — wave 4 via gridlock-flip.** Open beads under epic parents (hk-hqwn, hk-8i31, hk-872, hk-b3f, hk-a8bg, hk-8mwo) all show "0 open children" via the simple prefix filter — meaning either every child IS closed, or the dep edge is `blocks` and L-011-grid-lock applies. Run `bv --robot-triage --format toon` to see if children are still actionable; if `br stats` Open count exceeds the sum of openable children, flip parent-`blocks` edges to `related` via the sqlite-flip in the directives above, then dispatch a new wave.

Open follow-ups (not blocking):
- DOT testbed (Phase 3) — needs `kerf new dot-testbed --jig spec` work — defer until Phase 2 demo proves out.
- Daemon command-queue: `br create` IS the queue; SIGUSR1 "poll now" handler in `workloop.go` is the smallest possible LP-extension (≤20 LOC) if poll-latency bites.
- Pre-existing test failures (not introduced this session, file fix beads if needed): `TestAR013EnvelopeDeclaration/claude-hook-bridge.md` (specaudit), `TestEventEV002b_HandlerPackageDoesNotImportCore` (core/handler import cycle hint), `PL-021*` axis-invalid spec-audit failures in process-lifecycle.md.

# Files to open first

1. `HANDOFF.md` (this).
2. `docs/dogfood-smoke-run-2026-05-14-operational-green.md` — Phase 1 milestone + the 11 umbrella fixes that converged.
3. `docs/twin-parity-audit-2026-05-14.md` — twin coverage map.
4. `specs/workspace-model.md` §WM-005b + `specs/execution-model.md` §EM-012b — bead-level branching + model selection (Phase 2 prerequisites).
5. `cmd/harmonik/main.go` — daemon entrypoint, tmux-start subcommand.
6. `internal/daemon/workloop.go` — the run-one loop.
