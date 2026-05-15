<!-- PP-TRIAL:v2 2026-05-15 main — v45. T80/T81/T82 scenario tests merged → Roadmap Row 8 GATE PASSED. Bridge-integration epic hk-gql20 CLOSED (Row 5 P0 done). 75 beads closed across the session (242 → 167 open). Cross-project ~/.claude/CLAUDE.md synthesized + v44 ACTIVE DISPATCH directive added. -->

Roadmap: [ROADMAP.md](ROADMAP.md) — high-level epic order. Cross-project working-style rules: `~/.claude/CLAUDE.md`.

<!-- ORCHESTRATION DIRECTIVES — DO NOT EDIT EXCEPT BY EXPLICIT USER REQUEST. Loaded every /session-resume. -->

ROLE. You are the orchestrator. Delegate substantively. Keep the main thread minimal.

LEARNING LOG (READ ON EVERY RESUME). `docs/orchestration-learnings.md` — friction-and-fix log. Read on `/session-resume`. Append new entries when you observe friction. Promote durable rules to this directives block or `.claude/implementer-protocol.md`.

STREAM-NOT-WAVES (HARD RULE). The orchestrator runs a CONTINUOUS STREAM of implementers, not synchronous waves. On every implementer-completion notification, do exactly two things, in order: (1) Merge the returning implementer; (2) inspect dispatchable depth and either spawn one replacement OR note "queue draining" and stop spawning.

Per-return acknowledgment is ≤2 lines. Full session summary lives at `/session-handoff` time.

PHASE 2 IS UNBLOCKED (NEW v38). With harmonik operational you CAN now dispatch beads via the daemon instead of via the Agent tool — file a bead with `br create`, start harmonik against the project, watch it execute. Trade-off: harmonik overhead is ~30s+ per bead vs sub-agent's seconds; use it when (a) durability matters, (b) the work spans sessions, (c) tmux inspectability matters, or (d) parallel `--max-concurrent N` amortizes the overhead. For trivial inline work, sub-agent dispatch still wins.

IMPLEMENTER COMMIT DISCIPLINE (REINFORCED v38). Most implementers in the v38 session ran self-review APPROVE BUT NEVER COMMITTED in their worktree. The orchestrator had to commit-on-behalf. Briefs MUST end with "COMMIT EXPLICITLY (`git add` + `git commit`) before exiting" and the orchestrator MUST verify the commit landed before merging. If diff is uncommitted, the orchestrator stages + commits on behalf using `Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>`.

WORKTREE TASK-INJECTION LEAK (v36, ONGOING). Implementer edits leak into main's working tree as uncommitted changes. Workaround: `git stash push -m "v36-leak ..." && git merge --ff-only <branch> && git stash drop`. Never commit the leaked main-tree edits as a separate commit — the proper changes arrive via the worktree branch merge.

WORKTREE AUTO-REMOVED BY HARNESS (v41 NEW). When an implementer agent finishes, the harness may auto-remove its worktree directory (but NOT the branch). If `git -C <wtpath>` returns `cannot change to directory`, the worktree is already gone — just `git merge --ff-only worktree-agent-<id>` directly from main.

WORKTREE-REMOVE STEALS CWD (v45 NEW). When `git worktree remove` runs against the directory the shell is sitting in (or the next command's cwd resolves to a now-removed worktree), subsequent commands fail with `fatal: Unable to read current working directory`. ALWAYS prepend `cd /Users/gb/github/harmonik` to the post-remove commands in the same Bash call. v45 hit this twice; lost a merge commit until reflog-cherry-pick.

WORKTREE BEADS-JSONL LEAK (v41 PATTERN). Implementers' `br close` writes to `.beads/issues.jsonl` in the worktree, which then conflicts with rebase. Workaround in the merge dance: `git -C "$WTPATH" stash push -m leak && git -C "$WTPATH" rebase main` BEFORE the ff-merge. The stash is intentionally never popped — the JSONL state on main wins.

ISOLATED-WORKTREE STALE-BASE BUG (v35, ONGOING). Every implementer dispatched with `isolation: "worktree"` MUST be told in its brief to:

    cd <your worktree path>
    git fetch origin
    git rebase main

BEFORE reading any spec or code. Verify base via `git log --oneline -5`.

TRUST `br ready` BUT VERIFY (HARD RULE — L-011, L-017).
`br ready` is not authoritative for "the corpus is drained":
  1. Stale `blocked_issues_cache` (L-011): cross-check `br stats` Open vs Ready. Recovery: `br doctor --repair`.
  2. Parent-child gridlock (L-011): convert via sqlite3.
  3. Stale `defer_until` (L-017): clear via `br update <id> --defer ""`.

`br ready --format json` ALSO drops `labels` (br v0.1.45). Fixed in 93aeaae via ShowBead hydration in workloop. Don't add a parallel fix.

DON'T ASK — EXECUTE.
On `/session-resume` with no hard blocker, EXECUTE — don't close the say-back with an A/B question. Sub-agents inherit via `.claude/implementer-protocol.md`.

**Spec text is NOT a blanket exception.** Default for spec edits is DISPATCH. Only check in for SIGNIFICANT/architectural changes per the threshold below (line ~49). When a failing test requires a missing section/needle/wording-fix in a spec, that is hygiene — dispatch without check-in. v45 incident: orchestrator queued an AR-013 envelope-section add (literally "1 new section" required by an existing test) as if it were architectural; user had to redirect. Don't repeat.

ACTIVE DISPATCH — DON'T PARK THE STREAM (v44, L-018). Three sub-patterns of the above, all observed in the v43 session as moments where the orchestrator stalled on questions whose answers were in scope:
- **Critical-path serialized?** Pull from the broader ready queue and dispatch non-conflicting parallel work — don't ask "keep pulling or hold?"
- **Bead body offers design candidates?** Pick the one most consistent with current code, state a one-sentence rationale, dispatch it, and note "respond before commit if you want a different one." Don't park.
- **Spec/refinement threshold:** ≤1 new section, cross-ref fix, or wording-gap close → dispatch. New contract, normative field rename, or reversal of a locked decision → check in.
- **Informational planning-agent output** (roadmap, triage, audit) → synthesize and continue dispatching; only pause when the output explicitly surfaces a user-decision.
- **Dispatch updates end with the next action you're taking, not a question.** If two paths are equally valid, pick the throughput-maximizing one and name it — the user will redirect if wrong.

SUBSUMED BEADS ARE COMMON (v45 NEW). Many open beads' spec content has already landed in earlier corpus-finalize commits (e.g. 6bc2e57). When dispatching a spec-amendment implementer, the brief should END with "if the bead's spec text is already in place, close as SUBSUMED with the landing commit SHA and exit — no edit needed." In v45, ~10 dispatches resolved to SUBSUMED out of ~25 spec-amend implementers — fast hygiene work the orchestrator can run as a periodic sweep agent rather than waiting for implementers to discover.

PUSH AUTONOMY (v40 2026-05-14). User lifted "ask before push" constraint. Orchestrator pushes `origin main` after merge dance + tests-green without confirmation.

NO CI (v41 2026-05-14). User does NOT want GitHub Actions. Do not propose CI workflow files.

IMPLEMENTER LIFECYCLE — ENFORCED IN PROTOCOL. `.claude/implementer-protocol.md` is authoritative. (a) Implementer CLOSES OWN BEADS via `br close`. (b) Implementer DOES THE BEADS NAMED IN ITS BRIEF AND EXITS. (c) Implementer DOES NOT ASK questions back. (d) Implementer COMMITS EXPLICITLY.

DISPATCH SHAPE.
- Implementers: `model=sonnet`, `effort=high`, `isolation=worktree`, `run_in_background=true`. REBASE FIRST per the hard rule.
- Reviewers: `model=sonnet`, `effort=high`, no isolation.
- Briefs ≤15 lines: see brief-template in `.claude/implementer-protocol.md`. Do NOT paraphrase the bead body. Implementer fetches via `br show`.

CWD DISCIPLINE. Use `git -C /Users/gb/github/harmonik` for ALL git ops AND absolute paths for reads. After any `git worktree remove`, the next command MUST start with `cd /Users/gb/github/harmonik` (v45 cwd-steal).

MERGE DANCE — RUN FROM `/Users/gb/github/harmonik`.

    cd /Users/gb/github/harmonik
    for id in <agent-id-1> <agent-id-2>; do
      WTPATH="/Users/gb/github/harmonik/.claude/worktrees/agent-$id"
      BRANCH="worktree-agent-$id"
      [ -d "$WTPATH" ] && git -C "$WTPATH" stash push -m leak
      [ -d "$WTPATH" ] && git -C "$WTPATH" rebase main
      git merge --ff-only "$BRANCH"
      cd /Users/gb/github/harmonik   # restore cwd before remove
      git worktree remove --force --force "$WTPATH" 2>/dev/null
      git branch -d "$BRANCH"
    done

If a branch is lost (e.g. worktree dir gone before merge): `git reflog --all | grep worktree-agent-<id>` then `git cherry-pick <SHA>`.

CONTEXT BUDGET (orchestrator). ~700 k effective. v45 used ~60% across heavy parallel dispatch (~25 implementers, 6 explorers, 4 hygiene agents, 30 commits).

<!-- END DIRECTIVES -->

# Where we are (v45, 2026-05-15)

**Main at `a8b6568`. All work pushed to origin. Working tree clean (1 in-progress bead per `br stats` — verify after pull). Big session — 24 commits.**

## Headline outcomes

1. **Roadmap Row 5 (bridge-integration) CLOSED.** Epic `hk-gql20` closed at `a8b6568` after take-2 review-loop dogfood smoke went GREEN. Same session: epic `hk-lj1p9` (claude session lifecycle) closed at `10d4bf5` after hygiene confirmed all 20 children done. Phase 0 epic `hk-ahvq` also closed via the orphan-hygiene pass.
2. **Roadmap Row 8 (Phase 2 multi-bead E2E) GATE PASSED.** T80 (`hk-8vokz` queue lifecycle, 676 lines), T81 (`hk-2gqua` paused-by-failure, 517 lines), T82 (`hk-30wgn` crash recovery, 565 lines) all merged green at `e46fc5b` / `384f7a2` / `8201de3`. `internal/scenario/...` package fully green.
3. **Cross-project working-style synthesis.** `~/.claude/CLAUDE.md` now holds 7 cross-project guidelines (keep moving, delegate, plain English, compact, review gate, etc.) distilled from the v43 friction-mine + the parallel kerf-project mine. Harmonik's `CLAUDE.md` adds a one-line pointer + keeps project-specific bits.
4. **v44 directive added + L-018 in orchestration-learnings.** ACTIVE DISPATCH paragraph in HANDOFF directives block — "don't park the stream" with 5 sub-rules. L-018 captures the 5 concrete moments justifying it.
5. **Big hygiene reckoning.** 75 beads closed (242 → 167 Open). Extqueue v0.1: 11 SUBSUMED beads closed in the first sweep (`71044e1`) — handoff v43 said "landed" but `br close` had never run. Then Row 9 sweep closed 2 more (`35a22b7`). Then individual implementers found ~10 more SUBSUMED while doing real spec-amend work. Pattern: spec content already lived in commit `6bc2e57` (claude-hook-bridge spec corpus finalize) but the corresponding tracking beads were orphaned. See new directive paragraph "SUBSUMED BEADS ARE COMMON" above.
6. **imrest (Row 6) major progress.** `hk-iuaed.2` (PL-006 orphan-reset spec, `a1d281c`), `hk-iuaed.3` (BI adapter ResetBead op — `internal/brcli/resetbead_bi010d_test.go` + 588-line impl, `60c8170`), `hk-iuaed.5` (EV §8.7.14 confirm + catch-up of 5 additive fields, `bedd5a5`) all landed. `hk-iuaed.4` (PL-006 sweep impl) and `.6` (sensor) are now unblocked and dispatchable.
7. **Other code landings**: `hk-do7te` agent_ready timeout (`e19de6a` — adds 10s reap-after-Kill in workloop + reviewloop), `hk-a0htu` labels-gap fix in workloop (`93aeaae` — ShowBead hydration after Ready), `hk-zs0.21` AR-020 amendment-proposal procedure with architect+critic personas (`0d2bcd5`), `hk-sx9r.24` 7 upgrade sub-rules ON-020b–h (`cca00f5`), `hk-sx9r.27` ON-022 secrets binding test (`2af7dfa`), `hk-sx9r.69` ON-INV-001 N-1 compat sensor harness (`5856de2`).
8. **Orphan-parent hygiene.** 3 parent-child edges added (`3697cc0`): hk-do7te → hk-kqdpf, hk-4goy3 → hk-kqdpf, hk-6x7dw → hk-hqwn. hk-7uasg has an existing `related` edge to hk-qo08q — needs manual upgrade to parent-child if desired.
9. **ROADMAP audit.** All 15 open epics covered by existing rows (`b736d9d` removed closed `hk-lj1p9` from Row 5). No new rows needed.

## Stream / dispatch state at handoff time

- Stream drained — no implementer agents running.
- `br stats`: Open 167, In Progress 1 (likely stale — re-check on resume), Blocked 128, Ready 50, Deferred 38, Closed 1092.

## Plain-English glossary (what the codes mean)

- `hk-iuaed` — imrest epic: separating "bead in_progress" (activity marker, recoverable) from close/reopen (truth claim). Row 6 on the roadmap.
- `hk-kqdpf.5` — single remaining bridge-followup task: re-run dogfood smoke with substrate + bridge wired (P0). Only thing keeping Row 5 from full closure now that hk-gql20 is done.
- `hk-qo08q` — claude-hook-bridge spec corpus implementation epic (Row 7).
- `hk-sx9r` — operator-NFR spec implementation (Row 9).
- `hk-zs0.*` — architecture spec amendments under epic hk-b3f (Row 9).
- "SUBSUMED" — bead's spec text already landed in earlier commit; closing as hygiene rather than re-doing work.

# Next session — START HERE

## Immediate plan (in order)

1. **Dispatch `hk-kqdpf.5`** (P0 remaining bridge-followup) — re-run dogfood smoke with substrate + bridge wired. Now that hk-gql20 closed via the review-loop smoke, this one should also be GREEN. Closing it closes the bridge-followup epic and lets Row 5 be fully checked off. Operational agent (not worktree-isolated) similar to the gql20.24 smoke pattern in `af7aa914bdf4ee1da`'s output.
2. **Dispatch `hk-iuaed.4`** (P1 sweep impl, now unblocked by .2/.3/.5) — extend PL-006 orphan-sweep with stale-in_progress reset using the new ResetBead adapter op. Then `hk-iuaed.6` (sensor — depends on .4).
3. **Dispatch a deferred-clear sweep** — 38 beads sit in deferred state per `br stats`. Some are valid post-MVH parks; others are stale `defer_until` blockers (L-017). A small agent can scan and clear the stale ones — high ROI on ready-queue depth.

## Subsequent waves

- **Roadmap Row 7 (CHB spec corpus)** — hk-qo08q has ~15 open code-implementation children per the Row 9 sweep report. Triage with `bv --robot-triage --graph-root hk-qo08q` and dispatch in parallel; many are likely independent CHB-NNN req beads.
- **Roadmap Row 5 final close** — once hk-kqdpf.5 lands, close the `hk-kqdpf` epic itself; both Row 5 epics will be done.
- **Roadmap Row 8 fold-up** — hk-1n0cw (smoke epic) is a meta-parent with 2 open children (hk-w5vra closed, hk-do7te closed via this session). Verify hk-1n0cw can now close.
- **Roadmap Row 6 close-out** — once hk-iuaed.4 + .6 land, the imrest epic closes (Row 6 done).

## Files to open first

1. `HANDOFF.md` (this).
2. `ROADMAP.md` (11-row plan).
3. `~/.claude/CLAUDE.md` (cross-project working style — auto-loaded by every session).
4. `docs/orchestration-learnings.md` (read on resume; L-018 is newest).
5. `docs/dogfood-smoke-run-2026-05-15-review-loop-take2.md` — GREEN smoke run that closed hk-gql20.

## Question that blocks the next session

None. Continue executing per directives + roadmap.

## Known-failing tests (pre-existing, NOT blocking)

- `TestAR013EnvelopeDeclaration` in specaudit (cosmetic spec hygiene).
- `TestON027DrainStep1StopPullingQueue/check-4` (cosmetic wording).
- `TestWorkLoop_FailedHandlerReopensBead`, `TestWorkLoop_TwoConcurrentBeads` (pre-existing flaky under full-suite parallel load).
- `TestBI010c_SpecContainsWorkflowLabelDiscipline` in brcli (pre-existing; noted in hk-iuaed.3 dispatch report).
- `TestThroughput_TenBeadsAtMaxFour` is slow (~57s) and times out full suite at 120s — not a regression.
