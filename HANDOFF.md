<!-- PP-TRIAL:v2 2026-05-26 main — v63 (commit cf5f5b1). Clean. P0 heartbeat-emitter-bypass FIXED (3d57033). 4 commits landed, hk-muvk9 closed (was misdiagnosed rate limit). New issue: reviewer-phase hang (hk-zimkh). -->

Roadmap: [ROADMAP.md](ROADMAP.md). Cross-project working-style rules: `~/.claude/CLAUDE.md`. Plans index: [plans/README.md](plans/README.md).

**Orchestrator rules (permanent directives): [docs/orchestrator-rules.md](docs/orchestrator-rules.md). Known workarounds: [docs/known-workarounds.md](docs/known-workarounds.md).**

ROLE. You are the orchestrator. Delegate substantively. Keep the main thread minimal.

LEARNING LOG (READ ON EVERY RESUME). `docs/orchestration-learnings.md` — friction-and-fix log. Read on `/session-resume`. Append new entries when you observe friction. Promote durable rules to `docs/orchestrator-rules.md` or `.claude/implementer-protocol.md`.

# Where we are (v63, 2026-05-26)

**Main at `cf5f5b1`** (origin parity, working tree clean). 4 commits landed in this session.

## What v63 landed

- **P0 FIX (3d57033): Heartbeat emitter routed through per-run event tap + immediate first heartbeat.** Root cause of ALL v62 "rate limit" failures: `newDaemonHeartbeatEmitter` used `deps.bus` instead of `tap`, so heartbeats never reached `pasteInjectQuitOnCommit`'s event channel. `launchHeartbeatTimeout` (60s) always fired, killing every session. Additionally, `HeartbeatInterval` (300s) >> `launchHeartbeatTimeout` (60s), so even with the routing fix, the first heartbeat wouldn't arrive in time — now emitted immediately on loop start. **This closes hk-muvk9** (the "rate limit" was misdiagnosed).
- EV-030 breaking-change classification (6bebde2, hk-hqwn.39, 384 lines)
- ControlPoint test guards for AxisTags + ModeTag (cf5f5b1 merge, hk-a8bg.57)
- HookVerdictRecord test (cf5f5b1 merge, hk-a8bg.72)

## NEW ISSUE: Reviewer-phase hang (hk-zimkh, P1)

Reviewers write `review.json` with APPROVE verdict but don't exit — they get stuck at a prompt. The daemon has no timeout/watchdog for the reviewer phase (line 800: "the reviewer does not call `pasteInjectQuitOnCommit`"). Workaround: manually `kill <reviewer-pid>` after checking `review.json` exists.

## ISSUE: Daemon merge-to-main not working

All 4 commits that landed were merged to main MANUALLY by the orchestrator. The daemon reports `run_completed` but the run branch is not merged to main. Filed as known bug — likely the `mergeRunBranchToMain` step is failing silently (possibly because the orchestrator's working tree is dirty or HEAD advanced mid-wave). Investigation needed.

## No_commit failures at ~55-60s (NEW PATTERN)

After the heartbeat fix, ~50% of implementers still fail with `no_commit_during_implementer` after 55-60 seconds. The implementer claude processes start, read the bead, explore the codebase, then EXIT ON THEIR OWN without committing. The heartbeat fix prevents the daemon from killing them, but the implementers are exiting voluntarily.

Possible causes:
- API rate limit when 3+ concurrent sessions run (sessions exit after a single thinking phase)
- Beads too complex for the 55-second thinking window (implementer reads code but can't act before session budget expires)
- Something in the implementer protocol or CLAUDE.md causing early exit

Successful beads (hqwn.39, a8bg.57, a8bg.72) all ran for 3-6 minutes with productive output. Failed beads exit at ~55s. This is NOT the heartbeat kill — the implementer sessions genuinely exit.

## PRIORITY 1: DOT implementation chain

- hk-7okmx (T-IMPL-003 loader) — **CLOSED** (landed in v62)
- hk-waj4b (T-IMPL-004 daemon wiring) — OPEN, failed twice this session (no_commit). Needs investigation or manual implementation.
- hk-qo9pq (T-IMPL-013 CLI dot mode) — blocked by hk-waj4b
- hk-mwqxg (T-IMPL-007 edge evaluator) — OPEN, failed once (no_commit)
- hk-rhj3t (T-IMPL-009 cascade context) — OPEN, failed once (no_commit)
- hk-jtyzz (T-IMPL-012 policy_ref rejection) — dispatched in current wave

## PRIORITY 2: Spec-corpus + quality work

- hk-hqwn.39 — **CLOSED** (landed this session)
- hk-a8bg.57 — **CLOSED** (landed this session)
- hk-a8bg.72 — **CLOSED** (landed this session)
- Still open: hk-hqwn.51 (event tagging sensor), hk-a8bg.3 (evaluator boundary), hk-lhv8i (pre-screen at submit)

## Three caveats

1. **Reviewer hang** — every successful run requires manual `kill <reviewer-pid>` to unblock the daemon.
2. **Daemon merge failure** — run branches are not being merged to main automatically. Orchestrator must merge manually.
3. **~50% no_commit rate** — many implementers exit in ~55s without producing commits. Beads that succeed take 3-6 minutes.

## Files to open first

1. `HANDOFF.md` (this)
2. `docs/orchestrator-rules.md` — all permanent directives
3. `internal/daemon/workloop.go:1250` — heartbeat fix site
4. `internal/daemon/reviewloop.go:800` — reviewer has no pasteInjectQuitOnCommit (source of reviewer hang)

## Plain-English glossary

- **hk-s5szr** — P0 heartbeat-emitter bypass bug. **FIXED** in v63 (3d57033).
- **hk-muvk9** — "Concurrent-session rate limit." **CLOSED** — was misdiagnosed; root cause was heartbeat bug.
- **hk-zimkh** — NEW: reviewer phase hangs after writing verdict (no timeout/watchdog).
- **DOT** — workflow-graph-defined bead processes, replaces --review-loop.
- **perRunEventTap** — wrapper that forwards events to both the global bus AND a per-run channel for the commit/heartbeat watchdog.
- **`launchHeartbeatTimeout`** — 60s window; if no heartbeat arrives, session is killed. Fixed by emitting first heartbeat immediately.
- **L-022** — NEW learning: never rm worktree directories while daemon is running.

## No hard blockers requiring user input.
