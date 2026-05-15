<!-- PP-TRIAL:v2 2026-05-15 main — v47. Phase 2 EXERCISED (twice — hk-iuaed.6 + hk-cd92e). 2 of 3 Phase-2 blockers fixed (hk-yjsk8 br-close retry; hk-jvzc2 .gitignore leak). kerf phase-3-dot work opened, passes 1-3 done, pass-4 D3 design landed (Framing A: control-point dropped from node-type taxonomy, 5→4 types). 15 kerf-upstream beads filed from beta-test. AGENTS.md kerf section refreshed. 18 commits past v46. -->

Roadmap: [ROADMAP.md](ROADMAP.md) — high-level epic order. Cross-project working-style rules: `~/.claude/CLAUDE.md`.

<!-- ORCHESTRATION DIRECTIVES — DO NOT EDIT EXCEPT BY EXPLICIT USER REQUEST. Loaded every /session-resume. -->

ROLE. You are the orchestrator. Delegate substantively. Keep the main thread minimal.

LEARNING LOG (READ ON EVERY RESUME). `docs/orchestration-learnings.md` — friction-and-fix log. Read on `/session-resume`. Append new entries when you observe friction. Promote durable rules to this directives block or `.claude/implementer-protocol.md`.

STREAM-NOT-WAVES (HARD RULE). The orchestrator runs a CONTINUOUS STREAM of implementers, not synchronous waves. On every implementer-completion notification, do exactly two things, in order: (1) Merge the returning implementer; (2) inspect dispatchable depth and either spawn one replacement OR note "queue draining" and stop spawning.

Per-return acknowledgment is ≤2 lines. Full session summary lives at `/session-handoff` time.

PHASE 2 IS UNBLOCKED (NEW v38). With harmonik operational you CAN now dispatch beads via the daemon instead of via the Agent tool — file a bead with `br create`, start harmonik against the project, watch it execute. Trade-off: harmonik overhead is ~30s+ per bead vs sub-agent's seconds; use it when (a) durability matters, (b) the work spans sessions, (c) tmux inspectability matters, or (d) parallel `--max-concurrent N` amortizes the overhead. For trivial inline work, sub-agent dispatch still wins.

IMPLEMENTER COMMIT DISCIPLINE (REINFORCED v38). Most implementers in the v38 session ran self-review APPROVE BUT NEVER COMMITTED in their worktree. The orchestrator had to commit-on-behalf. Briefs MUST end with "COMMIT EXPLICITLY (`git add` + `git commit`) before exiting" and the orchestrator MUST verify the commit landed before merging. If diff is uncommitted, the orchestrator stages + commits on behalf using `Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>`.

AGENTS IN BACKGROUND (v46 NEW). When dispatching ≥2 parallel sub-agents, pass `run_in_background: true` on every Agent call. Do NOT wait for them inline — the orchestrator's value is dispatching breadth, blocking on foreground returns drops parallelism well below the 5–7 target. Completion notifications fire automatically; no polling.

QUEUE WITH CONTEXT (v46 NEW, L-020). Two rules: (1) Don't queue minor/hygiene work to the user — test-driven fixes, internal renames, corrections, hygiene closures are dispatch-without-asking. The threshold for queueing is "does this change product direction or affect users/agents irreversibly?" (2) When queuing IS warranted, the surface MUST carry plain-English what + why-queued + concrete options-with-consequences. A label like "X drafts (A/B/C)" without context is not a decidable surface — it wastes a user turn.

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

CONTEXT BUDGET (orchestrator). ~700 k effective. v45 used ~60% across heavy parallel dispatch (~25 implementers, 6 explorers, 4 hygiene agents, 30 commits). v46 used ~24% on a lighter dispatch load (15 commits, 12 sub-agents, no worktrees). v47 used ~38% across 12 background sub-agents (2 harmonik dogfoods, 5 phase-3-dot research agents, 4 worktree implementers, 1 pass-3 finalizer, 1 pass-4 design) — 18 commits.

HARNESS BLOCKS `.md` WRITES FOR SUB-AGENTS (v47 NEW). 2 of 5 phase-3-dot research sub-agents (R3, R4) hit a system-prompt rule blocking `.md` writes for "findings/analysis/summary" files — they returned content inline. Orchestrator (main thread) persisted via `Write` tool. Same friction hit pass-3 finalizer's SUMMARY.md (Bash heredoc fallback). When dispatching kerf-pass sub-agents that must write `.md` artifacts, expect ~40% to hit this and plan for orchestrator persistence. Tracked as kerf-upstream feedback item.

KERF IS IN BETA (v47 NEW). New kerf surface (`kerf next`, `kerf triage`, `kerf pin`, `kerf work edit`, `kerf map`, `kerf areas`) landed and was exercised. 15 kerf-upstream bugs filed in `br` (`label:kerf-upstream`). Feedback log: `docs/kerf-beta-feedback.md`. Convention pointer: root `KERF-FEEDBACK.md`. **Use kerf next as the queue feed once `bead_filter` clauses cover the corpus; ad-hoc workflows still need `br ready` for unattached beads.** Current state: only `claude-hook-bridge` work has a filter; 137 beads still untriaged.

<!-- END DIRECTIVES -->

# Where we are (v47, 2026-05-15)

**Main at `1c5f525`. All work pushed. Working tree clean. 18 commits past v46 (9779f72).**

## Headline outcomes

1. **Phase 2 EXERCISED (north-star milestone).** Two harmonik dogfood runs:
   - **Dogfood #1** — `hk-iuaed.6` (sensor bead) dispatched via harmonik. Real claude session wrote 449 lines of Go (`bi010d_*_test.go` ×2, 4 tests green), committed, daemon auto-merged to main at `dcd7f7e`. **First bead in project history dispatched VIA harmonik, not via sub-agents.**
   - **Dogfood #2** — `hk-cd92e` (P3 .gitignore-path bug) targeted. Daemon claimed wrong bead (`hk-a0htu`) despite priority bump — exposed claim-path priority bug. Bead body's fix was already present in code (closed as already-fixed). Run produced rich friction signal.
2. **2 of 3 Phase-2 blockers fixed.**
   - **`hk-yjsk8` fixed** at `fb809b0` — br-close timeout now retries on `BrUnavailable` (3 attempts, 10/20/40s backoff). 2 regression tests green. Prevents the bead-stuck-IN_PROGRESS pattern.
   - **`hk-jvzc2` fixed** at `1c5f525` — daemon no longer mutates parent repo's `.gitignore` + `.claude/settings.json` per-run. 4 per-launch writes removed; non-mutation contract pinned by byte-equality tests.
   - **`hk-icecw` still open** — `harmonik run <bead-id>` subcommand (P1 feature). Biggest remaining Phase-2 blocker. Without it, dogfooding requires priority-bump-and-pray which dogfood #2 proved unreliable.
3. **kerf phase-3-dot work — passes 1, 2, 3 done; pass-4 in progress (D3 landed).**
   - **Pass 1** problem-space: 6 audit gaps + 5 drift items absorbed. Reviewer APPROVE.
   - **Pass 2** decomposition: 5 spec components identified (C1 workflow-graph.md new, C2 execution-model dot mode, C3 handler-contract Outcome, C4 control-points binding, C5 examples/*.dot). Reviewer APPROVE.
   - **Pass 3** research: 5 component findings + cross-cutting SUMMARY.md with 20-row decision matrix. Pass-3 also identified 3 already-resolved items not needing pass-4 (EM-046a, §8 6-class taxonomy, EM-005a kind extension).
   - **Pass 4 D3 landed at `0f7deb5`** — control-point dropped from node-type taxonomy. 5 types → 4 types (`agentic | non-agentic | gate | sub-workflow`). Framing A chosen because CP-005's per-Kind trigger table is structurally disjoint and only Gate maps to a node-shaped slot.
4. **Audit drift fixes landed.** 5 docs refreshed at `5492cdb`: orchestrator-core.md (Attractor mischaracterization), concepts/kilroy.md (counts), QUESTIONS.md (resolution notes), components/external/kilroy.md (Kilroy+Attractor + harmonik divergence), foundation/OVERVIEW.md (DOT-in-specs clarification).
5. **kerf bootstrap + beta-test surface fully exercised.** `kerf init` ran; `bead_filter` configured for `claude-hook-bridge` work (137 beads still untriaged). `kerf next` now returns ranked beads. 15 kerf-upstream beads filed (`label:kerf-upstream`).
6. **AGENTS.md kerf section refreshed** at `6bd03bc`. New surface documented: kerf next/triage/pin/work edit/map/areas + agent-loop pattern + beta-test caveat. Root `KERF-FEEDBACK.md` pointer added.
7. **9 new friction beads from dogfooding** (5 from #1, 4 from #2). Top P1 unfixed: `hk-icecw` (harmonik run subcommand), `hk-rp48p` (daemon claims wrong bead vs priority), `hk-sc3o4` (orphan-sweep observes=4 reset=0 — PL-006 gap), `hk-lgtq2` (Cat 3a auto-reconciler).

## NORTH STAR status (refreshed)

1. **Phase 1 (operational smoke GREEN):** ✅ achieved v40 (2026-05-14).
2. **Phase 2 (orchestrator dispatches VIA harmonik):** ⚠️ EXERCISED but **not yet ready to scale**. Dogfood #2 verdict: 3 blockers (1 of 3 fixed, 1 of 3 fixed, 1 of 3 — `hk-icecw` harmonik-run subcommand — still open). The mechanics work; the developer-experience needs `hk-icecw` to land before this becomes default.
3. **Phase 3 (DOT-defined bead processes):** ⏳ **STARTED.** kerf work `phase-3-dot` consolidates the audit gaps into a structured 5-component plan; passes 1-3 done; pass-4 (design) has 1 decision landed (D3), ~6-10 remaining (D2 failure_class placement, D1/D4/D5 cluster, D6/D8/D12 cluster, D9/D10/D11 schema-versioning cluster).

# Next session — START HERE

## Sub-goal A (highest-leverage): land `hk-icecw` (harmonik run subcommand)

The Phase-2 scale blocker. Until `harmonik run <bead-id>` exists, dogfooding requires priority-bump tricks that dogfood #2 proved unreliable. Estimate: 1-2 implementer beads. This unlocks all future dogfooding.

`br show hk-icecw` for the brief. Probably also wants a `--one-shot` semantic so the daemon exits after the target bead reaches terminal (covers `hk-ajchp` simultaneously).

## Sub-goal B: continue phase-3-dot pass-4 design

Pass-4 has 1 decision down (D3), ~7 remaining. Suggested order from `03-research/SUMMARY.md`:

1. **D2** — `failure_class` placement on Outcome (lean: top-level field on FAIL). Unblocks D1/D4/D8.
2. **D1 + D4 + D5 cluster** — edge-condition LHS whitelist, failure_class as condition-LHS, condition dialect. Closes G2 end-to-end.
3. **D6 + D8 + D12 cluster** — verdict surfacing, `context_updates` typing, terminal-node differentiation. Required before review-loop.dot can be written.
4. **D9 + D10 + D11 cluster** — schema versioning + repo convention. Parallel-lane; closes G6.

Bench: `~/.kerf/projects/gregberns-harmonik/phase-3-dot/04-design/`. Pass-5 (spec drafts) is after all design decisions land.

## Sub-goal C: continue Phase-2 dogfooding once `hk-icecw` lands

Use `kerf next` to pick a real ready bead, file the brief, `harmonik run <id>`. Capture friction. Address any new blockers as found.

## Sub-goal D (operational, parallel): keep landing P1 Phase-2 blockers

Still open: `hk-rp48p` (priority claim bug — likely subsumed by hk-icecw); `hk-sc3o4` (orphan-sweep PL-006 gap); `hk-lgtq2` (Cat 3a auto-reconciler); `hk-44w19` (SIGTERM propagation P2). Lower urgency than A, but real correctness work.

## Files to open first

1. `HANDOFF.md` (this).
2. `KERF-FEEDBACK.md` (root pointer) → `docs/kerf-beta-feedback.md` (the log).
3. `ROADMAP.md` (Phase 3 is now in motion via phase-3-dot kerf work).
4. `~/.claude/CLAUDE.md` + project `AGENTS.md` (refreshed kerf section).
5. `docs/orchestration-learnings.md`.
6. `~/.kerf/projects/gregberns-harmonik/phase-3-dot/03-research/SUMMARY.md` (the cross-cutting design-decision matrix).

## Plain-English glossary

- **Phase 1/2/3** — north-star sequence: operational smoke → orchestrator-via-harmonik → DOT-defined processes. Phase 1 done; 2 exercised + ~30% blockers fixed; 3 started.
- **DOT bead processes** — workflow graphs (Graphviz DOT) describing how a bead is processed by agents. `phase-3-dot` kerf work is the structured planning for this. Pass-4 design in progress.
- **Framing A (D3)** — control-point is NOT a node-type; it's a policy primitive bound via `*_ref` attributes. 5→4 type taxonomy: `agentic | non-agentic | gate | sub-workflow`.
- **kerf next** — new kerf command returning ranked actionable beads; the agent-between-tools loop reads this and dispatches via harmonik. Currently only `claude-hook-bridge` work has a `bead_filter`; other works show 0 attached beads.
- `hk-icecw` — P1 Phase-2 blocker, `harmonik run <bead-id>` subcommand. Biggest remaining unfix.
- `hk-yjsk8` — br-close retry, fixed at fb809b0.
- `hk-jvzc2` — .gitignore mutation per-run, fixed at 1c5f525.
- `hk-rp48p` — daemon claim-path ignores priority (dogfood #2 finding).
- `hk-sc3o4` — orphan-sweep observed-but-not-reset (PL-006 gap).
- "Pass 1-7" — kerf spec-jig passes: problem-space → decompose → research → change-design → spec-drafts → integration → tasks.

## Question that blocks the next session

None operational. **Strategic ask once `hk-icecw` lands**: should the next dogfood target be a P1 bug or a Phase-3-dot spec-draft bead? Default per directives is execute — pick the next ready bead from `kerf next` and continue.

## Known-failing tests (pre-existing, NOT blocking)

- `TestWorkLoop_FailedHandlerReopensBead`, `TestWorkLoop_TwoConcurrentBeads` (pre-existing flaky under full-suite parallel load).
- `TestBI010c_SpecContainsWorkflowLabelDiscipline` in brcli (pre-existing — confirmed unrelated to v47 work).
- `TestPidfileRelease_AllowsReacquire` (passes alone, flakes under parallel load).
