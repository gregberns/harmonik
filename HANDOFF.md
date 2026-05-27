<!-- PP-TRIAL:v2 2026-05-27 main — v67 (commit f47e344). Clean. 22 commits, 28 beads resolved, 7 CHB beads landed via harmonik, workloop tests fixed. -->

Roadmap: [ROADMAP.md](ROADMAP.md). Cross-project working-style rules: `~/.claude/CLAUDE.md`. Plans index: [plans/README.md](plans/README.md).

**Orchestrator rules (permanent directives): [docs/orchestrator-rules.md](docs/orchestrator-rules.md). Known workarounds: [docs/known-workarounds.md](docs/known-workarounds.md).**

ROLE. You are the orchestrator. Delegate substantively. Keep the main thread minimal.

LEARNING LOG (READ ON EVERY RESUME). `docs/orchestration-learnings.md` — friction-and-fix log. Read on `/session-resume`. Append new entries when you observe friction. Promote durable rules to `docs/orchestrator-rules.md` or `.claude/implementer-protocol.md`.

# Where we are (v67, 2026-05-27)

**Main at `f47e344`** (origin parity, working tree clean). 22 commits landed this session.

## What v67 landed

- **7 CHB beads via harmonik dispatch:** CHB-006 (env-var schema), CHB-007 (forbidden flags), CHB-009 (fresh-mint enforcement), CHB-011 (no-op exit), CHB-012 (stdin validation), CHB-013 (hook mapping table), CHB-014 (reviewer verdict read). All reviewer-approved except CHB-007 (impl verified manually).
- **4 workloop test failures fixed (hk-95xm9):** Root cause was no-commit guard (hk-mmh8f) reopening beads when test handlers used `exit 0` without committing. Fix: `workloopFixturePreCommitWorktreeFactory` creates dummy commits in worktrees; `workloopFixtureGitRepo` adds bare origin remote.
- **3 orphaned commits salvaged:** hk-6232r (subscribe test improvements), hk-j6npz (tmux window cleanup), hk-a5sil (subscribe since_event_id replay). All cherry-picked from deleted worktree branches.
- **17 stale/probe beads closed:** 10 with implementations already on main, 3 probe artifacts, 4 test artifacts.

## Harmonik dispatch learnings (CRITICAL — extends v66 list)

1–5. (Unchanged from v66 — concrete beads succeed, use `--context @file`, etc.)
6. **`.beads/issues.jsonl` blocks merges.** Every `br close` dirties this file; harmonik's rebase detects unstaged changes and fails. Commit beads changes BEFORE dispatching a batch, and don't run `br close` while a batch is in reviewer/merge phase.
7. **Non-isolated sub-agents dirty main, blocking merges.** The workloop test investigator wrote to `workloop_test.go` in the main repo while harmonik was merging — caused 4 merge failures. NEVER dispatch non-isolated (`isolation != worktree`) sub-agents while harmonik is running.
8. **Spec context improves commit rate.** Batch 3 used `--context @specs/claude-hook-bridge.md` — CHB-013 committed (previously failed). Still no silver bullet for beads that need nonexistent integration points.
9. **4 CHB beads persistently no_commit:** hk-qo08q.8 (session-id), .16 (retry backoff), .17 (exit-code discipline), .18 (pre-exec ordering). Each failed 2× across batches. These reference code integration points that don't exist yet — they need prerequisite scaffolding or richer bead descriptions with exact file:line targets.

## Next priorities

1. **Remaining 4 CHB beads** (.8, .16, .17, .18) — enrich descriptions with file paths or create prerequisite scaffold beads. Consider dispatching as a single focused sub-agent with full codebase context (exception (a): fixing harmonik's own hook-relay).
2. **hk-cw56j** (CHB-023, implementer --resume correctness) — complex, cross-cutting. Needs kerf work or focused investigation.
3. **Phase-3 DOT beads** — `kerf next` shows DOT exploration/scenario beads ready. These are the near-term endgame per orchestrator rules.
4. **Continue stale-bead closure** — ~144 open, many likely subsumed.
5. **Pre-existing test failure:** `TestSession_Outcome_StderrTail` in `internal/handler/session_test.go` — observed during CHB-009 test run. Not blocking.
6. **38 stale stashes** accumulated from prior sessions — safe to drop (`git stash drop` the worktree-agent-* and leak entries).

## Files to open first

1. `internal/hookrelay/hookrelay.go` — the hook-relay dispatch hub; CHB-013 mapping table tests at `hookrelay_chb013_qo08q_test.go`
2. `internal/handler/claudehandler_chb006_024.go` — CHB-006/007/008/009 live here
3. `internal/daemon/workloop_precommit_factory_test.go` — the new test factory (hk-95xm9 fix)
4. `specs/claude-hook-bridge.md` — normative spec for remaining CHB beads

## Plain-English glossary

- **hk-qo08q** — CHB (claude-hook-bridge) spec implementation epic
- **hk-qo08q.8/.16/.17/.18** — four CHB beads that persistently fail no_commit (session-id mint, retry backoff, exit-code discipline, pre-exec ordering)
- **hk-95xm9** — workloop test failure bug (FIXED this session)
- **hk-j6npz** — tmux window cleanup on daemon exit (LANDED via cherry-pick)
- **hk-a5sil** — subscribe since_event_id replay (LANDED via cherry-pick)
- **hk-6232r** — subscribe test improvements (LANDED via cherry-pick)
- **hk-cw56j** — implementer --resume correctness across daemon restart (CHB-023, still open)
- **no_commit** — harmonik failure class: implementer exited without advancing HEAD

## No hard blockers requiring user input.
