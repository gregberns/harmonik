<!-- PP-TRIAL:v2 2026-05-15 main — v46. Row 5 fully closed (hk-kqdpf.5 GREEN); Row 6 closed (hk-iuaed.4 orphan-sweep landed, 1421 lines, 15 tests green). 15 commits past v45. Bead-graph cleanup: 11 CHB blocks→removed (3 impl beads unblocked), 5 orphan-parents linked, 37 SUBSUMED closed. Phase 2/3 NORTH STAR still not exercised — see "What's actually missing" below. -->

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

CONTEXT BUDGET (orchestrator). ~700 k effective. v45 used ~60% across heavy parallel dispatch (~25 implementers, 6 explorers, 4 hygiene agents, 30 commits). v46 used ~24% on a lighter dispatch load (15 commits, 12 sub-agents, no worktrees).

<!-- END DIRECTIVES -->

# Where we are (v46, 2026-05-15)

**Main at `9779f72`. All work pushed. Working tree clean. 15 commits past v45 (a48e3ef).**

## Headline outcomes

1. **Roadmap Row 5 FULLY CLOSED.** `hk-kqdpf.5` smoke GREEN at `f24ff5f` — substrate active end-to-end. Epic `hk-kqdpf` closed; meta-epic `hk-1n0cw` closed.
2. **Roadmap Row 6 (imrest) CLOSED.** `hk-iuaed.4` orphan-sweep impl landed at `9779f72` — 1421 lines, 4-branch exclusion logic, 13 unit + 2 integration tests, all green. Follow-up `hk-11xkn` filed for the audit-log `actor=project_hash` provenance gap (MVH unreachability tracked, not silently shipped). `hk-iuaed.6` sensor remains open — needs corpus scan + harness, prep already done.
3. **Bead-graph structural cleanup.** 11 CHB spec-text `blocks` edges removed (3 impl beads `hk-crf9a`/`hk-lj848`/`hk-pcvw8` unblocked). 5 orphan-parents linked. blocked_count 125 → 106. 37 deferred SUBSUMED. 30 hygiene labels.
4. **AGENTS.md is now canonical.** CLAUDE.md symlinks → AGENTS.md (per agent-configuration spec). 154 lines still over the 120 cap — trim is deferred.
5. **AR-013 §4.a envelopes added** to `specs/queue-model.md` and `specs/claude-hook-bridge.md`. `TestAR013EnvelopeDeclaration` green. `hk-wywsm`/`hk-g3iyl` closed.
6. **Process tightenings**: L-019 (dispatch-priority ordering), L-020 (queue-with-context discipline) added. L-003/L-013 retired into L-015. HANDOFF.md spec-text-check-in rule clarified (hygiene ≠ architectural — explicitly excludes test-driven section adds).
7. **TestThroughput_TenBeadsAtMaxFour 57s→16s** — daemon test budget back inside the 120s suite limit.

## What's actually missing (NORTH STAR audit — user-raised at handoff)

Per memory `project_harmonik_north_star.md`, the three phases are:
1. **Phase 1 (operational smoke GREEN):** ✅ achieved 2026-05-14 (v40 milestone). Harmonik can run claude end-to-end on a bead with zero human input.
2. **Phase 2 (orchestrator dispatches VIA harmonik, not sub-agents):** ⚠️ technically unblocked since v38 (see directive paragraph "PHASE 2 IS UNBLOCKED"). **Has not actually been exercised in v45 or v46.** Every commit this session was dispatched via the Claude Code Agent tool, not via `br create` + `harmonik run`. The mechanics exist; the habit hasn't shifted.
3. **Phase 3 (DOT-defined bead processes):** ❌ Not started. No DOT files exist that describe agent-flow processes. No harmonik feature consumes a DOT. This is the strategic gap and the one closest to the product thesis ("composable agentic orchestration").

The recent session work has all been operational hardening (smoke runs, orphan-sweep, scenario tests, bead hygiene). It's necessary but it's all *foundation* — the visible part of the product still doesn't exist.

# Next session — START HERE

## Sub-goal A (this is the real one)

Begin Phase 2/3 transition. Concrete first steps:

1. **Dogfood a single bead through harmonik instead of sub-agent dispatch.** Pick a small, fully-spec'd ready bead (e.g. `hk-iuaed.6` sensor — already prepped, no architectural risk). File the brief as a bead, run `harmonik` against it locally, observe end-to-end. The friction encountered is the most-valuable signal in the project right now.
2. **Identify the DOT shape.** No DOT format exists yet for bead processes. Drafting the first sketch — even informally — unblocks Phase 3 thinking. What does a DOT describing "implementer → review → merge" look like? Where do branch points (REQUEST_CHANGES → fix-and-resubmit) live? What's the equivalent of an `if` node? Spawn a planning agent to propose 2–3 candidate DOT schemas.
3. **Frame the next user check-in.** Before scaling Phase 2, the user should see one harmonik-dispatched bead complete and weigh in on whether the experience is product-shaping (this is one of the few user-decision moments).

## Sub-goal B (operational, continue if A blocked)

Same as v45's tail: triage Row 7 CHB corpus, work through `hk-a8bg.*` ControlPoint spec beads (highest PageRank in current triage), continue sensor implementations as their target impls land.

## Files to open first

1. `HANDOFF.md` (this).
2. `ROADMAP.md` (11-row plan — Rows 5+6+8 now done; Rows 7/9/10/11 remain).
3. `~/.claude/CLAUDE.md` + project `AGENTS.md` (working-style + project-specific).
4. `docs/orchestration-learnings.md` (L-019, L-020 are newest).
5. `~/.claude/projects/-Users-gb-github-harmonik/memory/MEMORY.md` (project memory; auto-loaded).

## Plain-English glossary

- **Phase 1/2/3** — the project's north-star sequence: operational smoke → orchestrator-via-harmonik → DOT-defined processes. Phase 1 done; 2 unexercised; 3 unstarted.
- **DOT bead processes** — the unstated product thesis: workflow graphs (like Graphviz DOT) that describe how a bead should be processed by agents (which nodes, which decision points). Not yet implemented or even sketched.
- `hk-iuaed.6` — sensor bead, prep done (read-only corpus scan returned zero violations), ready to implement.
- `hk-iuaed.4` — PL-006 orphan-sweep impl, landed at 9779f72.
- `hk-kqdpf.5` — final bridge-followup, smoke GREEN, landed at f24ff5f.
- `hk-11xkn` — follow-up tracking the audit-log provenance gap discovered while implementing hk-iuaed.4.
- "L-019/L-020" — newest entries in orchestration-learnings.md (dispatch-priority; queue-with-context discipline).
- "SUBSUMED" — bead's spec text already landed in earlier commit; close as hygiene.

## Question that blocks the next session

None operational. **Strategic:** does the user want to lean into Phase 2 dogfooding immediately, or finish more of the Row 7 / Row 9 foundation first? Default per directives is execute — start Phase 2 dogfooding (sub-goal A) without asking.

## Known-failing tests (pre-existing, NOT blocking)

- `TestWorkLoop_FailedHandlerReopensBead`, `TestWorkLoop_TwoConcurrentBeads` (pre-existing flaky under full-suite parallel load).
- `TestBI010c_SpecContainsWorkflowLabelDiscipline` in brcli (pre-existing).
- `TestPidfileRelease_AllowsReacquire` (passes alone, flakes under parallel load — observed in hk-zixbp fix).
