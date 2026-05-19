<!-- PP-TRIAL:v2 2026-05-19 main — v50. Phase-2 dogfood OPERATIONAL: 6/6 harmonik run --beads dogfoods landed real spec/Go changes end-to-end. ALL hit bead-close timeout (root cause still unknown after 3 hypotheses). MVH terminology scrubbed (hk-wn2pl). 5 scenario-test gap beads filed. ~73 commits past v49. Outstanding: hk-5dewt REOPEN — auto-close stays elusive. -->

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

# Where we are (v50, 2026-05-19)

**Main at `2622783`. Working tree clean (apart from in-flight worktree). ~70 commits past v49 baseline (`30a89bd`).** One implementer in flight at handoff time: hk-5dewt daemon `.br_history` rotation pre-flight (the root-cause fix for bead-close timeout).

## Headline outcomes

1. **Phase-2 dogfood OPERATIONAL — 5/5 dogfood runs landed real work via `harmonik run --beads`.** Each shipped a substantive spec edit committed by the daemon, merged + pushed to main without any sub-agent dispatch. Commits: `0b233a0` (hk-x0y2k CHB-013), `02e31e1` (hk-75rij QM-052a), `54fa28a` (hk-pxrv6 HC-057), `2748c51` (hk-vqoh2 EM-012b), `c3a6731` (hk-o0yft PL-006b/c). Work-completion path is solid.
2. **2 dogfood-blockers fixed + 1 still elusive (REOPEN).** **hk-aievp** (`fd5d268`): tmux `WindowPaneID(session:name-with-slash)` misparsed worktree-path window names → returned stale prior-session pane; fixed by atomic `-P -F "#{pane_id}"` capture. **hk-ry3be** (`4daaa3d`): `runWait` only checked `processDead(pid)` which returns false when macOS reparents an orphan to launchd → daemon heartbeated for 15h after silent pane death; fixed by secondary `WindowPanePID` check. **hk-5dewt** (REOPEN, P0): tracked through 3 hypotheses — retry-widen (`d99693f`, insufficient), WAL pre-flight (`abf36c4`, insufficient), `.br_history` rotation (`f87704f`, prevents bloat but `br close` STILL takes 19.25s with only 22 history entries). Defense-in-depth fixes kept; real root cause unknown. **6/6 dogfoods land work end-to-end but all hit bead-close timeout** → orchestrator manually closes after each run. Phase-2 work-completion is operational; auto-close is not.
3. **MVH terminology scrubbed** (`hk-wn2pl`, P0). 5 top-level docs rewritten, `POST_MVH_PARALLELISM_ROADMAP.md` renamed, 6 open beads retitled to drop `(post-MVH)`/`MVH-required` qualifiers, CLAUDE.md/AGENTS.md gained a normative "Terminology — avoid MVH" guardrail. Root user concern: "MVH framing licensed half-built features." Reinforced by hk-b6ls5 (plans/README) + hk-85trr (local kerf jigs require Validation/Acceptance Tests section).
4. **Substantive work: 70 commits.** Beads closed include hk-8ykjq (HandlerPausePolicy wire-up), hk-gjyks (8 EventType registrations), hk-m7joe (handler-pause.md elevated to specs/ normative), hk-9als7 (queue operator-drain consumer), hk-87u3q (HP-035 RWMutex), hk-107gz (handler-fatal taxonomy + HC-020a), hk-c8k4c + hk-th378 (BI-031b classifier wire-up at all 3 brAdapterErr sites), hk-rwdvm (EM-015e enum), hk-zudz0 (HC-006a per-phase LaunchSpec table), hk-5mjrs (label convention unified to `codename:*`), hk-3aqtb (nil-watcher scenario test), hk-f31xv (restored pasteinject test), hk-s95t2 (source-of-truth inventory verified).
5. **Scenario-test gap audit** (`docs/scenario-test-gap-audit-2026-05-18.md`) filed 5 P0 beads: hk-6f1uj, hk-t5j2w, hk-3aqtb (✓ landed), hk-qxtbq, hk-nfhqd. Each names the half-built-systems pattern it would have caught at PR time.
6. **Reviewer-gate paid off twice.** hk-m7joe reviewer caught HP-035 spec/code drift (filed as hk-87u3q, fixed at `df3ac9e`). hk-c8k4c reviewer caught half-built-systems (filed hk-th378, fixed at `75fdacd`). The directive "REVIEWERS MISS COMPOSITION-ROOT WIRING" continues to hold.
7. **Diagnostic improvement landed** (`f518d8b`): br retry escalation now surfaces per-attempt brErr class + exit + stderr. The "BrUnavailable persisted after N retries" message had been masking the actual error class — exactly how we discovered (11/11 BrUnavailable, 0/11 BrDbLocked) was a timeout issue, not a lock-conflict issue.

## Next session — priorities

1. **Land hk-5dewt** (`.br_history` rotation pre-flight) when implementer returns. Verify by running another dogfood end-to-end without manual close.
2. **Investigate stalled `harmonik run` post-success.** Even with the WAL fix and the silent-termination detection, the run process still hangs waiting after close-fail (queue stays "active"). Once hk-5dewt fixes close, run a full dogfood + confirm clean exit code 0.
3. **Route remaining P2 docs beads through harmonik** (validates unattended Phase 2): hk-n3v1q, hk-xpnfy, hk-h7eke (workflow-modes spec), and `hk-2hb2y`/`hk-yjduq` follow-ups if any remain. The list of "all pure docs work" from v49 has 5 closed via dogfood already.
4. **Stale-bead sweep** — orchestrator hit worktree-stale-at-fork ~5 times this session. The pattern is well-documented; not blocking, just verbose. Optional: tighten the implementer protocol so `br` ops happen in main's DB, not the worktree's.
5. **Scenario-test backlog**: hk-6f1uj, hk-t5j2w, hk-qxtbq, hk-nfhqd. Each needs a small twin variant. Good 4-way parallel dispatch.

## Files to open first

1. `HANDOFF.md` (this).
2. `docs/scenario-test-gap-audit-2026-05-18.md` (5 P0 scenario beads, source).
3. `docs/kerf-feedback/2026-05-19.md` (.br_history bloat + WAL-fix-was-insufficient findings, MAJOR-tagged).
4. `internal/daemon/walcheckpoint.go` + `internal/daemon/daemon.go` (PL-005 step 0 pre-flight hooks — where .br_history rotation will land).
5. `specs/handler-pause.md` (newly elevated normative spec at 77ae7ee).
6. `br list --status=open --priority 1` — current P1 backlog (most are now spec-gap docs work).

## Plain-English glossary

- **hk-5dewt** — daemon-side `.br_history` rotation pre-flight (keep latest 20). Root cause for the bead-close timeout that every dogfood hits. *In flight at handoff.*
- **hk-aievp** — CLOSED. Tmux misparse of window names with `/` returned stale prior pane. Atomic `-P -F` fix.
- **hk-ry3be** — CLOSED. `processDead(pid)` false-negative when macOS reparents claude orphan to launchd. Secondary `WindowPanePID` check fix.
- **hk-wn2pl** — CLOSED. MVH terminology scrubbed from project; CLAUDE.md guardrail added.
- **hk-u9kn5 / hk-ekz5v** — Both CLOSED. The path-of-investigation beads for the bead-close timeout (each layer was insufficient: retry-widen → WAL-pre-flight → finally .br_history rotation).
- **`harmonik run --beads <id>`** — Phase-2 dispatch CLI. 5/5 dogfoods landed work this session. Bead-close step still requires manual completion until hk-5dewt lands.
- **Phase-1 operational milestone (2026-05-14)** — daemon runs jobs end-to-end with zero human input. (Previously "MVH" — retired.)
- **Half-built systems pattern** — feature is unit-tested + reviewer-APPROVED but breaks in production because composition-root wiring or runtime-environment was untested. v50 found 3 more (hk-aievp, hk-ry3be, hk-c8k4c→hk-th378). Dogfood is the canonical test.
- **.br_history bloat** — every `br` write appends a full ~2.3MB jsonl snapshot to `.beads/.br_history/`. 200 entries = 226MB. Each `br close` then scans the history → 19.5s instead of 0.15s. Upstream kerf/beads_rust issue logged in `docs/kerf-feedback/2026-05-19.md`.

## Loose ends / blockers

- **hk-5dewt** fix in flight (agent id known to current session — see TaskList). Cherry-pick + close + re-test on next session start.
- `.beads/.br_history.226mb-archived/` is a 226MB directory I manually archived during diagnosis. Safe to delete after confirming hk-5dewt rotation works.
- Pre-existing `TestBI010c_SpecContainsWorkflowLabelDiscipline` test failure noted by two reviewers — fixture reads wrong paragraph due to forward-reference vs `#### BI-010c` heading order. Not in scope for any current bead.
- Tasks #7 (route P2 docs through harmonik) is the natural next move once hk-5dewt lands.

## No hard blockers requiring user input.
