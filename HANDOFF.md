<!-- PP-TRIAL:v2 2026-05-26 main — v65 (commit 9fdccb5). Clean. 20 commits, ~32 beads closed, critical workloop infinite-loop bug found and designed. -->

Roadmap: [ROADMAP.md](ROADMAP.md). Cross-project working-style rules: `~/.claude/CLAUDE.md`. Plans index: [plans/README.md](plans/README.md).

**Orchestrator rules (permanent directives): [docs/orchestrator-rules.md](docs/orchestrator-rules.md). Known workarounds: [docs/known-workarounds.md](docs/known-workarounds.md).**

ROLE. You are the orchestrator. Delegate substantively. Keep the main thread minimal.

LEARNING LOG (READ ON EVERY RESUME). `docs/orchestration-learnings.md` — friction-and-fix log. Read on `/session-resume`. Append new entries when you observe friction. Promote durable rules to `docs/orchestrator-rules.md` or `.claude/implementer-protocol.md`.

# Where we are (v65, 2026-05-26)

**Main at `9fdccb5`** (origin parity, working tree clean). 20 commits landed this session.

## Priority 1: Workloop bounded-retry (hk-mb8x4)

**The daemon's workloop enters infinite retry loops when ClaimBead fails for unexpected reasons.** Three investigation agents + two reviewers converged on adding an `Attempts` counter to `queue.Item` with `maxItemAttempts=3`. Design doc: `docs/design/workloop-bounded-retry.md`. This is the top priority — harmonik cannot safely dispatch beads until this is fixed.

**12 beads filed — ALL must be handled this session:**
- **hk-6pspu** (P0) — core fix: add Attempts counter + enforce bound
- **hk-kupeo** (P0) — ShowBead pre-claim retry also unbounded
- **hk-8ai2u** (P1) — test: permanent ClaimBead failure terminates after N attempts
- **hk-tmhak** (P1) — test: all-unclaimable wave reaches terminal state
- **hk-fvpz5** (P1) — test: ShowBead error doesn't cause infinite retry
- **hk-cun4l** (P2) — test: unexpected br exit codes
- **hk-xorlb** (P2) — test: ClaimBead timeout
- **hk-2hygc** (P2) — test: bead blocked during run
- **hk-5t0s6** (P2) — test: autoCloseStaleBlockers integration
- **hk-oylis** (P3) — test: 50-item wave with stuck head (property test)
- **hk-r6opz** (P3) — test: semaphore shutdown race
- **hk-f0xb6** (P1) — stop hook `HARMONIK_RUN_ID` env var absent

**Implementation order:** hk-6pspu first (the structural fix), then P1 tests in parallel, then P2/P3.

## What v65 landed

- **DOT complete on main:** All 6 core DOT impl beads merged (cascade engine, CLI dot mode, loader, edge evaluator, context updates, policy_ref rejection). Cherry-picked from orphaned run branches.
- **~32 beads closed:** 20 stale-open pre-screening + 12 cherry-picks from orphaned run branches.
- **3 daemon bug fixes:** wake-on-submit (hk-24xn1), blocked-bead detection (hk-n91y0), stale-blocker auto-close (hk-rnsjs).
- **Notify-stream auto-enable (hk-ze3op):** `--notify-stream` defaults on for multi-bead runs.
- **Temporary n91y0 follow-up (450a4c9):** Added error-message-based detection for blocked-by-deps. This is a STOPGAP — the bounded-retry counter (hk-6pspu) is the real fix.

## Observed harmonik dispatch issues

1. **10 concurrent sessions = 100% failure rate** — API rate limit kills all sessions in ~2 min. Use `--max-concurrent 3` with `--wave`.
2. **Abstract spec beads fail ~90%** — bead descriptions like "N-1 compat window per EV-029" don't tell implementers WHERE to write code. Use `--context @file` with concrete file paths.
3. **Orphaned run branches accumulate** — the daemon creates worktrees but doesn't always merge. Check `git branch --list "run/*"` for salvageable commits.
4. **Blocked beads in dispatch cause infinite loop** — the design fix (hk-mb8x4) must land before further dispatch.

## Files to open first

1. `docs/design/workloop-bounded-retry.md` — the design doc for the P0 fix
2. `internal/daemon/workloop.go:920` — current stopgap fix site
3. `internal/queue/types.go` — where `Attempts` field goes
4. `internal/daemon/workloop_hkn91y0_test.go` — existing test to extend

## Plain-English glossary

- **hk-mb8x4** — workloop bounded-retry epic (eliminate infinite loops)
- **hk-6pspu** — core structural fix (Attempts counter on queue items)
- **hk-kupeo** — ShowBead retry also unbounded (same bug class)
- **hk-f0xb6** — stop hook env var missing (HARMONIK_RUN_ID)
- **hk-n91y0** — prior blocked-bead fix (string-matching, now a fast-path)
- **DOT** — workflow-graph-defined bead processes (Phase 3 endgame)
- **maxItemAttempts** — proposed constant (3) bounding dispatch retries per item

## No hard blockers requiring user input.
