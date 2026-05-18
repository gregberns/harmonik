<!-- PP-TRIAL:v2 2026-05-18 main — v49. Dogfood-pivot session. Audit findings (12 spec-gap beads, 5 follow-up beads, 5 dogfood-CLI beads). Plans/Done-means convention landed. Dogfood CLI multi-bead+context+review-loop landed. Two dogfood-blockers fixed; retry #3 in flight at handoff time. ~35 commits past v48. -->

Roadmap: [ROADMAP.md](ROADMAP.md). Cross-project working-style rules: `~/.claude/CLAUDE.md`. Plans index: [plans/README.md](plans/README.md).

<!-- ORCHESTRATION DIRECTIVES — DO NOT EDIT EXCEPT BY EXPLICIT USER REQUEST. Loaded every /session-resume. -->

ROLE. You are the orchestrator. Delegate substantively. Keep the main thread minimal.

LEARNING LOG (READ ON EVERY RESUME). `docs/orchestration-learnings.md` — friction-and-fix log. Read on `/session-resume`. Append new entries when you observe friction. Promote durable rules to this directives block or `.claude/implementer-protocol.md`.

STREAM-NOT-WAVES (HARD RULE). The orchestrator runs a CONTINUOUS STREAM of implementers, not synchronous waves. On every implementer-completion notification, do exactly two things, in order: (1) Merge the returning implementer; (2) inspect dispatchable depth and either spawn one replacement OR note "queue draining" and stop spawning.

Per-return acknowledgment is ≤2 lines. Full session summary lives at `/session-handoff` time.

PHASE 2 IS UNBLOCKED (NEW v38). With harmonik operational you CAN now dispatch beads via the daemon instead of via the Agent tool — file a bead with `br create`, start harmonik against the project, watch it execute. Trade-off: harmonik overhead is ~30s+ per bead vs sub-agent's seconds; use it when (a) durability matters, (b) the work spans sessions, (c) tmux inspectability matters, or (d) parallel `--max-concurrent N` amortizes the overhead. For trivial inline work, sub-agent dispatch still wins.

`harmonik run <bead-id>` IS LIVE (NEW v48). Single-bead invocation: `harmonik run <id> [--project DIR]` builds a queue-of-one, runs the daemon, exits on completion. Exit code: 0 success / 1 paused-by-failure / 2 unexpected. Refuses overwrite of an active queue.json. Hangs avoided via `CancelOnQueueExit`. THIS IS the canonical Phase-2 dispatch UX — use it instead of priority-bump tricks.

`harmonik run --beads` MULTI-BEAD + --context + --review-loop (v49 NEW). Multi-bead one-shot: `harmonik run --beads id1,id2,... --max-concurrent N [--context "string|@file"] [--review-loop]`. Builds a queue of N items, parallel dispatch up to max-concurrent, single daemon, exits on completion. `--context` adds an Extra Context section to the agent-task.md for the handler. `--review-loop` selects WorkflowModeReviewLoop. Landed at `0da3a71`/`ebd25a4` via hk-w3cp1+hk-boiwe+hk-hiqrl.

IMPLEMENTER COMMIT DISCIPLINE (REINFORCED v38). Most implementers in the v38 session ran self-review APPROVE BUT NEVER COMMITTED in their worktree. The orchestrator had to commit-on-behalf. Briefs MUST end with "COMMIT EXPLICITLY (`git add` + `git commit`) before exiting" and the orchestrator MUST verify the commit landed before merging. If diff is uncommitted, the orchestrator stages + commits on behalf using `Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>`.

IMPLEMENTER MUST PUSH BRANCH (v49 NEW). Three implementer attempts this session were LOST when the harness force-removed the worktree before the branch was pushed (hk-37zy8 first attempt, hk-m0k0a-rebased branch deleted before recovered, hk-2hb2y test file lost in stash dance). EVERY implementer brief MUST end with `git push origin HEAD` AFTER committing. Recovery path: if commit is in object DB but not on a branch, `git cat-file -t <SHA>` then `git cherry-pick <SHA>`.

AGENTS IN BACKGROUND (v46 NEW). When dispatching ≥2 parallel sub-agents, pass `run_in_background: true` on every Agent call. Do NOT wait for them inline — the orchestrator's value is dispatching breadth, blocking on foreground returns drops parallelism well below the 5–7 target. Completion notifications fire automatically; no polling.

QUEUE WITH CONTEXT (v46 NEW, L-020). Two rules: (1) Don't queue minor/hygiene work to the user — test-driven fixes, internal renames, corrections, hygiene closures are dispatch-without-asking. The threshold for queueing is "does this change product direction or affect users/agents irreversibly?" (2) When queuing IS warranted, the surface MUST carry plain-English what + why-queued + concrete options-with-consequences. A label like "X drafts (A/B/C)" without context is not a decidable surface — it wastes a user turn.

REVIEWER GATE ON SIGNIFICANT WORK (v48 NEW). After merging any worktree implementer that touches load-bearing code (CLI surface, daemon composition, workloop, queue subsystem, hook bridge), dispatch a reviewer agent on the commit BEFORE moving on. v48 caught a BLOCK (hang-on-failure + exit-code-0 + silent-overwrite) on the just-merged `harmonik run` keystone; without the reviewer the CLI would have been unusable in scripted contexts. Reviewer briefs should: (a) reference the commit SHA, (b) name 8-10 specific checks, (c) demand a JSON verdict per the agent-reviewer schema, (d) request file:line citations for any issue.

REVIEWERS MISS COMPOSITION-ROOT WIRING (v49 NEW). Per-commit reviewers check the unit but DO NOT ask "is this thing actually triggered in production?" v49 caught hk-37zy8 (HandlerPausePolicyGoroutine never Subscribed in daemon.go), hk-yjduq (revWatcher nil-deref in tmux path), hk-2hb2y (pasteinject before pane spawn) — all unit-tested + reviewer-APPROVED, all broken at runtime. The structural fix is twin-based scenario tests at plan-end (see hk-b6ls5 + hk-85trr + scenario-test audit results). Until those land, reviewers SHOULD include an explicit check: "find the production call site for the new symbol; verify the wire-up exists."

DON'T LET BEADS CLOSE WITHOUT IMPL (v49 NEW). Handler agents in worktrees occasionally run `br close` even when no implementation landed. The closes leak to main's .beads/issues.jsonl. Mitigation landed at `a7bcd49` (agent-task.md now has a "Bead Lifecycle (CRITICAL)" section telling handlers NOT to close beads from inside the worktree; daemon owns transitions). When closing on-behalf after a failed run, REOPEN any beads marked closed-without-commit via `br update <id> --status=open`.

WORKTREE BEADS-JSONL STALE-AT-FORK (v48 PATTERN, OBSERVED REPEATEDLY). When the orchestrator's main creates a bead via `br create` AFTER a worktree has already been spawned, the worktree's `.beads/issues.jsonl` won't include it. The implementer's `br show <id>` fails ("Issue not found"). The implementer typically re-creates the bead under a NEW ID and closes it there. The orchestrator must then: (1) close the ORIGINAL ID on main with the same landing commit; (2) close the duplicate IDs as "worktree-stale-at-fork duplicate"; (3) commit the bead-state reconciliation separately. ALSO occurred when the merge-dance rebase hits `.beads/issues.jsonl` conflict — resolve with `git checkout --theirs .beads/issues.jsonl` to take main's state.

WORKTREE TASK-INJECTION LEAK (v36, ONGOING). Implementer edits leak into main's working tree as uncommitted changes. Workaround: `git stash push -m "v36-leak ..." && git merge --ff-only <branch> && git stash drop`. Never commit the leaked main-tree edits as a separate commit — the proper changes arrive via the worktree branch merge.

WORKTREE AUTO-REMOVED BY HARNESS (v41 NEW). When an implementer agent finishes, the harness may auto-remove its worktree directory (but NOT the branch). If `git -C <wtpath>` returns `cannot change to directory`, the worktree is already gone — just `git merge --ff-only worktree-agent-<id>` directly from main.

WORKTREE-REMOVE STEALS CWD (v45 NEW). When `git worktree remove` runs against the directory the shell is sitting in (or the next command's cwd resolves to a now-removed worktree), subsequent commands fail with `fatal: Unable to read current working directory`. ALWAYS prepend `cd /Users/gb/github/harmonik` to the post-remove commands in the same Bash call.

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

**Spec text is NOT a blanket exception.** Default for spec edits is DISPATCH. Only check in for SIGNIFICANT/architectural changes per the threshold below. When a failing test requires a missing section/needle/wording-fix in a spec, that is hygiene — dispatch without check-in.

ACTIVE DISPATCH — DON'T PARK THE STREAM (v44, L-018). Three sub-patterns:
- **Critical-path serialized?** Pull from the broader ready queue and dispatch non-conflicting parallel work — don't ask "keep pulling or hold?"
- **Bead body offers design candidates?** Pick the one most consistent with current code, state a one-sentence rationale, dispatch it. Don't park.
- **Spec/refinement threshold:** ≤1 new section, cross-ref fix, or wording-gap close → dispatch. New contract, normative field rename, or reversal of a locked decision → check in.
- **Informational planning-agent output** (roadmap, triage, audit) → synthesize and continue dispatching; only pause when the output explicitly surfaces a user-decision.
- **Dispatch updates end with the next action you're taking, not a question.** If two paths are equally valid, pick the throughput-maximizing one and name it.

SUBSUMED BEADS ARE COMMON (v45 NEW, REINFORCED v48). Many open beads' impl already landed; the close-out lagged. v48 closed ~30 subsumed beads (audit-verified, then `br close` with SUBSUMED reason naming the landing commit). When wading into a corpus, dispatch a parallel-audit-then-sweep before assuming the open-count is the real backlog. v48 example: plan 002 had "31 open" before audit, ~2 after.

PUSH AUTONOMY (v40 2026-05-14). User lifted "ask before push" constraint. Orchestrator pushes `origin main` after merge dance + tests-green without confirmation.

NO CI (v41 2026-05-14). User does NOT want GitHub Actions. Do not propose CI workflow files.

IMPLEMENTER LIFECYCLE — ENFORCED IN PROTOCOL. `.claude/implementer-protocol.md` is authoritative. (a) Implementer CLOSES OWN BEADS via `br close`. (b) Implementer DOES THE BEADS NAMED IN ITS BRIEF AND EXITS. (c) Implementer DOES NOT ASK questions back. (d) Implementer COMMITS EXPLICITLY. (e) Implementer PUSHES THE BRANCH (v49).

DISPATCH SHAPE.
- Implementers: `model=sonnet`, `effort=high`, `isolation=worktree`, `run_in_background=true`. REBASE FIRST per the hard rule.
- Reviewers: `model=sonnet`, `effort=high`, no isolation.
- Briefs ≤15 lines: see brief-template in `.claude/implementer-protocol.md`. Do NOT paraphrase the bead body. Implementer fetches via `br show`.

CWD DISCIPLINE. Use `git -C /Users/gb/github/harmonik` for ALL git ops AND absolute paths for reads. After any `git worktree remove`, the next command MUST start with `cd /Users/gb/github/harmonik`.

MERGE DANCE — RUN FROM `/Users/gb/github/harmonik`.

    cd /Users/gb/github/harmonik
    for id in <agent-id-1> <agent-id-2>; do
      WTPATH="/Users/gb/github/harmonik/.claude/worktrees/agent-$id"
      BRANCH="worktree-agent-$id"
      [ -d "$WTPATH" ] && git -C "$WTPATH" stash push -m leak
      [ -d "$WTPATH" ] && git -C "$WTPATH" rebase main
      git merge --ff-only "$BRANCH"
      cd /Users/gb/github/harmonik
      git worktree remove --force --force "$WTPATH" 2>/dev/null
      git branch -d "$BRANCH"
    done

If a branch is lost: `git reflog --all | grep worktree-agent-<id>` then `git cherry-pick <SHA>`. If merge-dance leaks code into main's working tree without committing (v48 observed): discard the leaked working tree edits, cherry-pick the actual commit by SHA found in reflog.

CONTEXT BUDGET (orchestrator). ~700 k effective. v48 used ~heavy across 15+ background sub-agents — kerf/bead/plan hygiene + 4 worktree implementers + 3 reviewers. 16 commits. v49 used ~51% across ~15 audit agents + 4 implementers + dogfooding cycle. 35 commits.

HARNESS BLOCKS `.md` WRITES FOR SUB-AGENTS (v47 NEW). Some sub-agents hit a system-prompt rule blocking `.md` writes for "findings/analysis/summary" files — they return content inline. Orchestrator (main thread) must persist via `Write` tool. When dispatching kerf-pass or audit sub-agents that must write `.md` artifacts, expect this friction and plan for orchestrator persistence.

KERF IS IN BETA + REALIGNED (v48 NEW). `kerf next`, `kerf triage`, `kerf pin`, `kerf work edit`, `kerf map`, `kerf areas` all functional. v48 created 2 new kerf works (`handler-pause`, `phase-2-completion`) so 30+ formerly-orphan beads now surface in `kerf next`. Filter syntax supports OR via repeated `--bead-filter-add` clauses (produces `any=[...]`). 15+ kerf-upstream bugs filed (`label:kerf-upstream`). Feedback log: `docs/kerf-feedback/<date>.md` (per-session dated file, NEW v49 convention). **Use `kerf next` as the primary dispatch surface.** phase-3-dot filter is intent-correct but matches zero beads until spec-amend/task beads are spawned (work is still in change-design pass). Local jig customization: `kerf jig save <name>` → edit → `kerf jig load <name> <path>` (hk-85trr P1 to apply for testing-criteria convention).

PLANS HAVE "DONE MEANS..." (v49 NEW). `plans/README.md` now requires every `_plan.md` to include an explicit "Done means..." section listing observable behavioral acceptance criteria, NOT "the beads shipped." Guards against minimum-viable shipping. Applied to `plans/007_handler_pause_and_resume/_plan.md` as the example. hk-b6ls5 extends to require scenario-test + exploratory-test beads at plan-end; hk-85trr applies the same to kerf jig templates locally.

<!-- END DIRECTIVES -->

# Where we are (v49, 2026-05-18)

**Main at `48a574e`. Working tree clean. ~35 commits past v48 baseline (`581f926`).** Dogfood retry #3 in flight at handoff time (background command `b7lq45kbe`; beads `hk-x0y2k` + `hk-75rij` IN_PROGRESS).

## Headline outcomes

1. **Handler-pause 8 of 9 P1 beads landed**, BUT it is DORMANT in production: `hk-8ykjq` (P1, OPEN) is the wire-up gap — `HandlerPausePolicyGoroutine.Subscribe(bus)` is never called in `daemon.go` composition root. The controller + persistence + dispatcher gate + policy goroutine all exist; nothing actually triggers pauses in production until hk-8ykjq lands. Reviewer (hk-37zy8 review) caught this; first 7 reviewers did not.
2. **Dogfood CLI surface landed.** `harmonik run --beads id1,id2,... --max-concurrent N [--context "string|@file"] [--review-loop]` works at the orchestration level. Two dogfood-blockers found and fixed: `hk-2hb2y` (tmux pane race + bead-close-without-impl, fixed at `a7bcd49`) and `hk-yjduq` (nil watcher in tmux substrate path, fixed at `94d8992`). Three retries; #3 in flight.
3. **Audit wave: 6 subsystem spec-vs-impl audits + source-of-truth inventory.** workspace/brcli is 100% spec-aligned. daemon has 8 categories of code-not-specced (most important: handler-pause-and-resume.md is a DESIGN DOC, never elevated to specs/). core/eventbus has 8 event types declared but never registered → JSONL replay fails. queue has 2 normative requirements not implemented (QM-002a, operator-drain). 12 spec-gap beads filed (6 P1 + 6 P2). `docs/source-of-truth-inventory.md` written.
4. **Terminology cleanup landed.** 4 drifted scope-qualifier lines fixed (hk-ux915). plans/README.md now requires "Done means..." section per plan. Upstream kerf jigs are clean; local jig customization via `kerf jig save/load` is the path for the testing-criteria convention (hk-85trr P1).
5. **Scenario-test audit.** 20 prioritized workflows; 5 P0 with zero coverage. The audit names hk-37zy8 wire-up as exactly the kind of bug twin-based scenarios would catch (and indeed it was; and then hk-yjduq was caught dogfooding-as-scenario-test).
6. **"Half-built systems" pattern formally identified.** 3 instances in this session alone (hk-37zy8 wire-up, hk-2hb2y pane race, hk-yjduq nil watcher) — all unit-tested, all reviewer-APPROVED, all broken in production. New directive: REVIEWERS MISS COMPOSITION-ROOT WIRING.

## Next session — priorities

1. **Check on dogfood retry #3** (`b7lq45kbe` background process). Inspect `/private/tmp/claude-502/.../tasks/b7lq45kbe.output` and bead status. If still failing, expect a new P0 — file, fix, repeat.
2. **Land `hk-8ykjq`** (handler-pause policy goroutine wire-up). This is THE most impactful real-impact bead — handler-pause is non-functional in production until it lands. Small fix: add `policyGoroutine := NewHandlerPausePolicyGoroutine(...); policyGoroutine.Subscribe(deps.bus)` pre-Seal in daemon.go composition root. Dogfood it.
3. **`hk-gjyks`** (8 EventType constants declared but never registered — JSONL replay fails). Real correctness gap.
4. **`hk-m7joe`** (elevate handler-pause-and-resume.md → specs/handler-pause.md normative). This is the structural fix for one major half-built-systems instance.
5. **Once dogfood is stable, route P2 docs beads through harmonik:** `hk-x0y2k`, `hk-75rij`, `hk-rwdvm`, `hk-zudz0`, `hk-n3v1q`, `hk-pxrv6`, `hk-vqoh2`, `hk-o0yft`, `hk-xpnfy`, `hk-h7eke`. All pure docs work.

## Files to open first

1. `HANDOFF.md` (this).
2. `docs/source-of-truth-inventory.md` (new this session).
3. `plans/README.md` — "Done means..." convention now lives here.
4. `cmd/harmonik/run.go` — new multi-bead CLI surface.
5. `internal/daemon/handlerpause_policy_37zy8.go` — the dormant goroutine waiting for hk-8ykjq wire-up.
6. `br list --label spec-gap --status=open` — the 12 spec-gap beads from this session's audits.

## Plain-English glossary

- **hk-8ykjq** — wire-up fix: connect `HandlerPausePolicyGoroutine.Subscribe(bus)` in daemon.go composition root. P1 OPEN. *Most impactful pending work.*
- **hk-2hb2y** — DOGFOOD-BLOCKER #1, now CLOSED. Tmux pane race (`SendEnterToLastPane: no window spawned yet`). Fixed by wiring `Substrate` on implSpec + revSpec.
- **hk-yjduq** — DOGFOOD-BLOCKER #2, now CLOSED. Nil-pointer panic in `Watcher.Done` because tmux substrate path returns nil watcher. Fixed by nil-guards in reviewloop.go.
- **hk-w3cp1 / hk-boiwe / hk-hiqrl** — Dogfood CLI bundle (multi-bead + --context + --review-loop). CLOSED.
- **hk-m7joe** — Elevate handler-pause-and-resume.md from design-doc to specs/handler-pause.md normative.
- **hk-gjyks** — 8 EventType constants declared but never registered → JSONL replay fails.
- **hk-b6ls5 / hk-85trr** — Extend "Done means..." convention to require scenario+exploratory test beads at plan end (both in plans/README.md and in local kerf jig templates).
- **hk-f31xv** — Restore `pasteinject_hk2hb2y_test.go` (lost in merge-dance stash).
- **Half-built systems pattern** — feature is unit-tested, reviewer-APPROVED, but broken in production because composition-root wiring was skipped. 3 instances this session.
- **Phase-1 operational milestone (2026-05-14)** — the point at which the daemon can run jobs end-to-end with zero human input. Previously called "MVH" internally; that label is retired — it licensed half-built features. Use "Done means..." criteria per plans/README.md for future work.
- **`harmonik run --beads`** — new multi-bead dispatch CLI; the dogfood mechanism.

## Loose ends / blockers

- The dogfood retry (`b7lq45kbe`) may still be running OR may have hit a new bug. First action next session: check it.
- When daemon panics, queue.json stays in "active" state — `hk-ly4w5` only archives on paused-by-failure. May want a follow-up bead for "archive on any abnormal exit." Not filed yet.
- Stale leftover stashes (3 worktree-leak stashes) — harmless; `git stash drop` after confirming each is leak-only.

## No hard blockers requiring user input.
