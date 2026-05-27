<!-- PP-TRIAL:v2 2026-05-27 main — v66 (commit 6245a34). Clean. 23 commits, 24 beads resolved, bounded-retry shipped + harmonik dispatch exercised. -->

Roadmap: [ROADMAP.md](ROADMAP.md). Cross-project working-style rules: `~/.claude/CLAUDE.md`. Plans index: [plans/README.md](plans/README.md).

**Orchestrator rules (permanent directives): [docs/orchestrator-rules.md](docs/orchestrator-rules.md). Known workarounds: [docs/known-workarounds.md](docs/known-workarounds.md).**

ROLE. You are the orchestrator. Delegate substantively. Keep the main thread minimal.

LEARNING LOG (READ ON EVERY RESUME). `docs/orchestration-learnings.md` — friction-and-fix log. Read on `/session-resume`. Append new entries when you observe friction. Promote durable rules to `docs/orchestrator-rules.md` or `.claude/implementer-protocol.md`.

# Where we are (v66, 2026-05-27)

**Main at `6245a34`** (origin parity, working tree clean). 23 commits landed this session.

## What v66 landed

- **Bounded-retry shipped (hk-mb8x4 epic closed):** `Attempts` counter on `queue.Item`, `MaxItemAttempts=3` enforcement in workloop dispatch, defense-in-depth in `waveEligible`/`streamEligible`, br-ready path bounded via `readyPathAttempts` map. 8 test beads covering all retry scenarios.
- **Hook-relay fix (hk-f0xb6):** exit 0 outside harmonik sessions — no more `bridge_malformed_hook_payload` errors in user Claude Code sessions.
- **Auto-archive queue (hk-ly4w5):** `harmonik run` auto-archives paused-by-failure/cancelled queues so re-dispatch is one command.
- **Subscribe connection cap (hk-bra0j):** MaxConnections=32 with CAS-protected counter and `subscribe_capacity_exceeded` rejection.
- **Depguard handler-contract rule (hk-t5h2p):** activated the previously-deferred lint rule.
- **Chores:** heldEventDedup cleanup on epoch change (hk-o48pb), handler disk struct dedup (hk-n8yyk), rate-limit docs (hk-kumjl).
- **11 stale-open beads closed** via pre-screening (already-implemented CHB/handler/workspace features).

## Harmonik dispatch learnings (CRITICAL for next session)

1. **Don't commit to local main while harmonik is running.** The daemon rebases worktree branches onto local main — any divergence causes rebase conflicts. Queue commits until the wave completes.
2. **Cherry-pick from `run/*` branches when reviewer approves but merge fails.** Check `git log run/<run-id> --oneline -3`.
3. **Concrete beads succeed, abstract beads fail ~90%.** Beads with specific file paths and line numbers in descriptions land; beads saying "implement X per spec" produce no_commit.
4. **Use `--context @file` for enriched dispatches.** The context lands in agent-task.md's "Extra Context" section.
5. **Stale tmux windows accumulate.** Daemon doesn't clean up tmux windows on wave completion. Filed hk-j6npz.
6. **4 pre-existing test failures** in `workloop_test.go`: `TestWorkLoop_DispatchClosesBead`, `TestWorkLoop_TwoConcurrentBeads`, `TestWorkLoop_LabelsHydratedFromShowBead`, `TestWorkLoop_ClaimSemaphore_BoundsClaimConcurrency`. All timeout waiting for bead close. Pre-date this session.

## Unsalvaged work worth reviewing

- **hk-a5sil** (subscribe since_event_id replay): commit `981ea82` on deleted branch `feat/hk-a5sil-since-event-id-replay` — 398 lines, 5 files. Was NOT reviewed. Retrievable via `git reflog` or re-dispatch.

## Next priorities

1. **Fix the 4 pre-existing test failures** — likely a workloop regression from a prior session. File beads and investigate.
2. **hk-j6npz** (tmux window cleanup on daemon exit) — needs sub-agent, too architectural for harmonik.
3. **hk-a5sil** (subscribe replay) — re-dispatch with context or review the orphaned commit.
4. **hk-6232r** (subscribe test improvements) — split into smaller beads, each with one test.
5. Continue closing stale-open beads — `br list --status=open` still has ~50.

## Files to open first

1. `internal/daemon/workloop.go` — bounded-retry code at lines 60-65 (maxItemAttempts), 525-530 (readyPathAttempts), 740-765 (Phase 3 enforcement)
2. `internal/queue/types.go:15-21` — `MaxItemAttempts` constant, `Attempts`/`LastFailureReason` fields
3. `internal/hookrelay/hookrelay.go:148-159` — exit 0 fix for non-harmonik sessions
4. `cmd/harmonik/run.go:422-450` — auto-archive logic

## Plain-English glossary

- **hk-mb8x4** — workloop bounded-retry epic (all children landed, closed)
- **hk-6pspu** — Attempts counter on queue items (core structural fix)
- **hk-kupeo** — ShowBead pre-claim retry bound
- **hk-f0xb6** — hook-relay env var error fix
- **hk-ly4w5** — auto-archive queue.json on re-run
- **hk-bra0j** — subscribe connection cap (MaxConnections=32)
- **hk-t5h2p** — depguard lint rule for handlercontract
- **hk-j6npz** — tmux window cleanup bug (filed, not yet fixed)
- **hk-a5sil** — subscribe since_event_id replay (orphaned commit, needs review)
- **maxItemAttempts** — constant (3) bounding dispatch retries per queue item

## No hard blockers requiring user input.
