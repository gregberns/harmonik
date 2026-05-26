<!-- PP-TRIAL:v2 2026-05-26 main — v64 (commit 90f2426). Clean. 5 daemon bugs fixed, 14 commits, 9 beads closed. Harmonik full pipeline (implement→review→merge→close→push) working end-to-end for the first time. -->

Roadmap: [ROADMAP.md](ROADMAP.md). Cross-project working-style rules: `~/.claude/CLAUDE.md`. Plans index: [plans/README.md](plans/README.md).

**Orchestrator rules (permanent directives): [docs/orchestrator-rules.md](docs/orchestrator-rules.md). Known workarounds: [docs/known-workarounds.md](docs/known-workarounds.md).**

ROLE. You are the orchestrator. Delegate substantively. Keep the main thread minimal.

LEARNING LOG (READ ON EVERY RESUME). `docs/orchestration-learnings.md` — friction-and-fix log. Read on `/session-resume`. Append new entries when you observe friction. Promote durable rules to `docs/orchestrator-rules.md` or `.claude/implementer-protocol.md`.

# Where we are (v64, 2026-05-26)

**Main at `90f2426`** (origin parity, working tree clean). 14 commits landed this session.

## What v64 landed

**5 daemon bugs fixed — harmonik full pipeline now works end-to-end:**
1. **Reviewer quit-on-verdict (312b872, hk-zimkh):** New `pasteInjectQuitOnReviewFile` polls for `.harmonik/review.json` and sends `/quit`. Reviewers no longer hang.
2. **Review-loop merge-to-main (312b872):** The approval path was skipping `mergeRunBranchToMain`. Now mirrors single-mode.
3. **Reopen on failure (5adcdcf):** Review-loop failure path was calling `CloseBead` instead of `ReopenBead`. Beads no longer vanish on no_commit.
4. **launchHeartbeatTimeout 60→180s (e3183dc):** Sessions run 5-30 min now instead of dying at 60s. Liveness checker provides secondary defense.
5. **Bead description hydration (806f4e7):** `ShowBead` Title+Description weren't being copied into `BeadRecord` — implementers saw just the bead ID as their entire task. ~70% no_commit rate root cause.

**DOT chain: 4/6 impl beads landed:**
- hk-mwqxg (edge evaluator, 4ae0552), hk-waj4b (loader+wiring, 5c0a34b), hk-rhj3t (cascade context, 73cdc7e), hk-jtyzz (policy_ref rejection, 7c6dc8a).
- Still open: hk-bf85t (cascade engine), hk-qo9pq (CLI dot mode).

**Spec-corpus: 3 CP beads landed:** hk-a8bg.3 (boundary-classification), hk-a8bg.1 (typed primitive), hk-a8bg.2 (name uniqueness).

**Tests: 3 beads landed:** hk-trfif (reopen-on-failure regression), hk-jimbc (quit-on-review-file tests), plus beads sync.

## Observed no_commit rate

With all 5 fixes, commit rate is ~40% (9 landed / ~22 dispatched). Remaining failures are genuine — implementers run 5-10 min with full bead descriptions but exit without committing. Likely causes: bead complexity exceeds single-session capacity, or implementer protocol needs "commit early" reinforcement. The description fix was the biggest lever; further improvements are incremental.

## Known issues

1. **Push race on concurrent merges:** When two beads in the same wave both succeed, the second push fails (non-FF). Daemon reopens the bead but it's already closed. Workaround: cherry-pick from the run branch. Filed as known pattern.
2. **REQUEST_CHANGES → implementer-resume fragile:** hk-a8bg.2 iteration 2 hit `agent_ready_timeout` after 31s. The resume path may have tmux pane issues.
3. **Batch 6 may still be running** when next session starts — check `ps aux | grep harmonik`.

## Test health (audited this session)

4,676 tests, 748 test files, 7/8 packages pass (specaudit fails on doc schema). **P1 gap: hk-fgy9o (lifecycle subsystem: 5.7K LOC, 6 tests, crash recovery untested).** 3 new test beads filed this session for v64 daemon fixes (hk-jimbc closed, hk-2suwl + hk-trfif open).

## Next session priorities

1. **Keep dispatching via harmonik** — the pipeline works. Target 6-bead waves, `--wave --max-concurrent 3`.
2. **DOT chain:** hk-bf85t (cascade engine) and hk-qo9pq (CLI dot mode) need landing — both failed as harmonik beads (complexity). Consider sub-agent dispatch.
3. **hk-fgy9o (P1 test uplift):** 6 days old, zero progress. Lifecycle crash recovery is untested.
4. **Repeatedly-failing beads:** hk-hqwn.38 (N-1 compat), hk-hqwn.51 (event tagging), hk-lhv8i (pre-screen at submit), hk-buy0j (watchdog follow-ups) all failed 2+ times. May need richer descriptions or sub-agent implementation.

## Files to open first

1. `HANDOFF.md` (this)
2. `docs/orchestrator-rules.md` — permanent directives
3. `internal/daemon/pasteinject.go:706` — new `pasteInjectQuitOnReviewFile`
4. `internal/daemon/workloop.go:1073` — review-loop merge-to-main fix site
5. `internal/workspace/agenttask_chb028.go:271` — `buildAgentTaskContent` (where bead descriptions render)

## Plain-English glossary

- **hk-zimkh** — reviewer hang bug (FIXED in v64)
- **hk-eknhz** — review-loop merge-to-main bug (FIXED in v64)
- **hk-waj4b** — DOT daemon wiring (T-IMPL-004, CLOSED)
- **hk-mwqxg** — edge-condition evaluator (T-IMPL-007, CLOSED)
- **hk-rhj3t** — cascade context plumbing (T-IMPL-009, CLOSED)
- **hk-jtyzz** — policy_ref rejection (T-IMPL-012, CLOSED)
- **hk-bf85t** — cascade engine (T-IMPL-008, still OPEN)
- **hk-qo9pq** — CLI dot mode (T-IMPL-013, still OPEN)
- **hk-fgy9o** — P1 test uplift epic (lifecycle crash recovery)
- **DOT** — workflow-graph-defined bead processes, replaces `--review-loop`
- **`pasteInjectQuitOnReviewFile`** — new function that watches for review.json and sends `/quit` to the reviewer
- **`launchHeartbeatTimeout`** — window for first heartbeat; was 60s (too tight), now 180s

## No hard blockers requiring user input.
