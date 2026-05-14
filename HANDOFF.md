<!-- PP-TRIAL:v2 2026-05-14 main — v38. PHASE 1 ACHIEVED: harmonik operational. Smoke from main HEAD `7247d5c` reaches OPERATIONAL GREEN in 24.1s (claude runs in tmux substrate, reads agent-task.md, edits + commits, daemon /quit-injects on commit detection, Stop hook fires, bead_closed via run_completed). 11 umbrella fixes landed this session. Main is **62 commits ahead of origin/main**, NOT pushed. -->

<!-- ORCHESTRATION DIRECTIVES — DO NOT EDIT EXCEPT BY EXPLICIT USER REQUEST. Loaded every /session-resume. -->

ROLE. You are the orchestrator. Delegate substantively. Keep the main thread minimal.

LEARNING LOG (READ ON EVERY RESUME). `docs/orchestration-learnings.md` — friction-and-fix log. Read on `/session-resume`. Append new entries when you observe friction. Promote durable rules to this directives block or `.claude/implementer-protocol.md`.

STREAM-NOT-WAVES (HARD RULE). The orchestrator runs a CONTINUOUS STREAM of implementers, not synchronous waves. On every implementer-completion notification, do exactly two things, in order: (1) Merge the returning implementer; (2) inspect dispatchable depth and either spawn one replacement OR note "queue draining" and stop spawning.

Per-return acknowledgment is ≤2 lines. Full session summary lives at `/session-handoff` time.

PHASE 2 IS UNBLOCKED (NEW v38). With harmonik operational you CAN now dispatch beads via the daemon instead of via the Agent tool — file a bead with `br create`, start harmonik against the project, watch it execute. Trade-off: harmonik overhead is ~30s+ per bead vs sub-agent's seconds; use it when (a) durability matters, (b) the work spans sessions, (c) tmux inspectability matters, or (d) parallel `--max-concurrent N` amortizes the overhead. For trivial inline work, sub-agent dispatch still wins.

IMPLEMENTER COMMIT DISCIPLINE (REINFORCED v38). Most implementers this session ran self-review APPROVE BUT NEVER COMMITTED in their worktree. The orchestrator had to commit-on-behalf. Briefs MUST end with "COMMIT EXPLICITLY (`git add` + `git commit`) before exiting" and the orchestrator MUST verify the commit landed before merging. If diff is uncommitted, the orchestrator stages + commits on behalf using `Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>`.

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

CONTEXT BUDGET (orchestrator). ~700 k effective. v38 used ~30% — heavy session with 11 umbrella fixes + 8 smoke runs.

<!-- END DIRECTIVES -->

# Where we are (v38, 2026-05-14)

**Main at `7247d5c`. Nothing in flight. 62 commits ahead of origin, NOT pushed. Working tree clean.**

## Phase 1 — operational harmonik — ACHIEVED

`docs/dogfood-smoke-run-2026-05-14-operational-green.md` — independent confirmation, smoke from main HEAD reaches GREEN in 24.1 seconds. Claude runs in the daemon's tmux substrate, reads agent-task.md, edits the file, commits the work, and the bead closes — zero human input.

The mechanism (see [[harmonik-operational-milestone]] memory):
1. Daemon spawns claude in a tmux pane inside a per-bead worktree.
2. `.harmonik/agent-task.md` written with bead body + a `## Session Completion` section instructing claude to `/quit` after committing.
3. Settings.json materialised with `permissions.allow` (no per-tool dialogs).
4. Trust pre-seeded in `~/.claude.json` under the canonical (EvalSymlinks) worktree path.
5. After welcome splash dismisses (`SendEnterToLastPane` + 750ms `splashDismissDelay`), the daemon paste-injects the kick-off via stored pane-ID target.
6. Claude reads the task, does the work, commits.
7. `pasteInjectQuitOnCommit` goroutine polls worktree HEAD every 500ms; on commit-change it sends `/quit Enter` to the pane via `tmux send-keys`.
8. Stop hook fires, daemon receives outcome via hook-relay socket, `sess.Wait` unblocks, bead closes.

## 11 umbrella fixes landed this session

| Bead | Commit | What |
|---|---|---|
| hk-lj1p9.3 | f6cd256 | EnsureWorktreeTrust env override + sidecar flock |
| hk-tjl40 | 2104168 | Wire RunSocketListener into daemon.Start |
| hk-smuku | e430807 | Wait uses stored per-pane PID + syscall.Kill |
| hk-lj1p9.4 | f79b2a8 | SetAgentReadyCallback + pasteInjectOnLaunch wired |
| hk-yngq2 | ff79246 | Use stable pane ID (%NNNN) for tmux pane target |
| hk-o5eww | e8fd1df | EvalSymlinks worktreePath before trust key |
| hk-zchbu | ff2bc6f | Paste-inject moves to AFTER waitAgentReady |
| hk-rf4ux | cec27e6 | SendEnterToLastPane + 750ms splashDismissDelay |
| hk-53y35 | cec27e6 | permissions.allow in materialized settings.json |
| hk-cmybm | cbc4725 | Daemon-side /quit injection on commit detection |

Smoke iteration map: v3 RED (no task) → v4 RED (socket) → v5 RED (Wait deadlock) → v6 RED (callback) → v7 RED (pane target) → v8 RED (trust path) → v9 RED (paste ordering) → v10 RED (splash race + permissions) → v11 functional-GREEN, operational-RED (Stop hook) → v13 implementer-self-GREEN → operational-green confirmation 2026-05-14 GREEN.

# TOP PRIORITY NEXT SESSION

1. **Push to origin** if the user signs off. Main is 62 commits ahead. `git push origin main`.

2. **Phase 2 demonstration.** File a real, scoped, dispatchable bead in `harmonik`'s own ledger or a scratch project. Run `harmonik` against it. Observe the operational queue executing work without sub-agent fan-out. Candidate beads (already ready in `br ready`):
   - `hk-872` Beads Integration spec implementation (epic — pick a child)
   - `hk-b3f` Execution Model spec implementation (epic — pick a child)
   - File a NEW small task (e.g. "Add a copy of marker.txt under docs/ for QA" or similar bounded scope).

3. **Phase 3 (DOT-defined bead processes)** — multi-session feature. Start with a kerf spec-first pass:
   - `kerf new dot-workflow --jig spec`
   - Frame: replace hardcoded `reviewloop.go` cycle (implementer→reviewer→loop-or-exit) with a DOT-graph driver. Each node = a phase; each edge labelled with the predicate that fires it (`APPROVE`, `REQUEST_CHANGES`, `BLOCK`, `iteration < cap`).
   - Touch on: DOT grammar choice (custom subset vs full graphviz), node-type registry, transition predicate evaluator, claude-side per-node task-file generation.
   - Cross-ref [[harmonik-workflow-composition]] (Gas Town formula/molecule/convoy vocabulary as candidate).

4. **Close the operational-caveats list** documented in `docs/dogfood-smoke-run-2026-05-14-operational-green.md`:
   - `noopRequestHandler` (hk-tjl40 stub) — implement real `EmitOutcome`/`ClaimNext` so the agent can post outcomes via the socket without relying on Stop hook.
   - Reviewloop's implementer-phase paste-inject (line ~272 of reviewloop.go) — still uses the old ordering. Re-test with a review-loop bead and apply the hk-zchbu reorder if it surfaces.
   - Multi-bead concurrent re-validation (`--max-concurrent 4`) hasn't been smoked end-to-end since the umbrella landed.

5. **Reviewer-loop end-to-end code** (carried over from v37 — but now lower priority than Phase 2/3). Specs exist (EM-015d-RFD + EM-015d-RIA + review-target.md write). The impl phase is built; the reviewer-phase code (write review-target.md, paste-inject for reviewer, read reviewer-feedback on resume) is the gap.

# Files to open first

1. **HANDOFF.md** (this).
2. `docs/dogfood-smoke-run-2026-05-14-operational-green.md` — the milestone evidence.
3. `internal/daemon/pasteinject.go` — where `pasteInjectQuitOnCommit` lives.
4. `internal/workspace/agenttask_chb028.go` — where the `## Session Completion` instruction is appended.
5. `specs/claude-hook-bridge.md §4.11 CHB-028` — the spec amendment for the quit-on-commit mechanism.

# Pre-existing flakes — still known, still not blocking

- `TestT4_ReopenThenRedispatch` — git-I/O contention; passes in isolation.
- `TestT5_RedactionHC031ByFieldName` in `internal/t5probe`.
- `internal/specaudit` failures.

# Caveats from the milestone doc

- The terminal event is `run_completed`, not a dedicated `bead_closed` event type. CHB and event-model specs talk about `bead_closed`; the actual implementation emits `run_completed` with `success=true` and the workloop interprets that as the bead-close signal. Spec terminology can be reconciled in a future pass (low priority).
- The `pasteInjectQuitOnCommit` polling interval is 500ms with a 10-minute timeout — fine for the smoke but worth observing on long-running real beads. May surface as a hk-cmybm follow-up if beads take > 10 minutes.
- Implementer commit discipline: most implementers in this session needed orchestrator commit-on-behalf. The protocol amendment (commit explicitly) is in this HANDOFF's directives; the implementer-protocol.md file should be amended next session to make this enforceable.
