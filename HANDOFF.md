<!-- PP-TRIAL:v2 2026-05-20 main — v52. Orchestration v2 LIVE: harmonik is the default dispatcher (HARD-RULE in directives + AGENTS.md Daily loop + harmonik-dispatch skill). Three Phase-2 dogfood-blocker bugs FIXED: hk-rp48p (priority-sort), hk-wx8z8 (parallel pane allocator), hk-ppt32 (queue-on-cancel cleanup). Go-touching probe revealed daemon was misclassifying success as crash — Stop-hook not delivering outcome_emitted (hk-cj0gm P1). Open P1: hk-8jh26 only. -->

Roadmap: [ROADMAP.md](ROADMAP.md). Cross-project working-style rules: `~/.claude/CLAUDE.md`. Plans index: [plans/README.md](plans/README.md).

<!-- ORCHESTRATION DIRECTIVES — DO NOT EDIT EXCEPT BY EXPLICIT USER REQUEST. Loaded every /session-resume. -->

ROLE. You are the orchestrator. Delegate substantively. Keep the main thread minimal.

LEARNING LOG (READ ON EVERY RESUME). `docs/orchestration-learnings.md` — friction-and-fix log. Read on `/session-resume`. Append new entries when you observe friction. Promote durable rules to this directives block or `.claude/implementer-protocol.md`.

STREAM-NOT-WAVES (HARD RULE). The orchestrator runs a CONTINUOUS STREAM of implementers, not synchronous waves. On every implementer-completion notification, do exactly two things, in order: (1) Merge the returning implementer; (2) inspect dispatchable depth and either spawn one replacement OR note "queue draining" and stop spawning.

Per-return acknowledgment is ≤2 lines. Full session summary lives at `/session-handoff` time.

**HARMONIK IS THE DEFAULT DISPATCHER (HARD RULE, v51).** Substantive work routes through `harmonik run --beads <ids>` unless an exception applies. The intended daily loop: `bv --robot-triage` → `kerf next` → pick batch of 3–5 → `harmonik run --beads id1,id2,... --max-concurrent N` → while it runs, queue next batch / drain triage / file follow-ups → on exit, review + dispatch next batch. Target: ≥75% of substantive commits per session land via `harmonik run` (committer identity / `Refs:` trailer in `git log`). The three exceptions: (a) the bead is a bug-fix to harmonik itself in code that breaks dispatch; (b) ≤2-line typo/cross-reference fix where ~30s daemon overhead isn't worth it; (c) untested workload class per the readiness-audit caveats (priority-sensitive routing — until hk-rp48p's regression test lands; `--max-concurrent > 1` — until hk-wx8z8 lands; code-touching — until the Go-touching probe passes). Sub-agent dispatch is otherwise the WRONG move. If you find yourself reaching for the Agent tool on a 4th task in a row, STOP — batch them and run `harmonik run --beads`. Full design: `docs/orchestration-protocol-v2.md`.

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

# Where we are (v52, 2026-05-20)

**Main at `49b6fd1`.** ~12 commits past v51. Working tree clean apart from the long-running untracked `harmonik-twin-claude/` stray directory.

## What landed this session

The session was about turning "Phase 2 occasionally works" into "Phase 2 is the default." Five pieces:

1. **Orchestration v2 protocol LIVE** (`5e295dd`). Three changes wired together: (a) HANDOFF.md ORCHESTRATION DIRECTIVES gained a HARD-RULE block "HARMONIK IS THE DEFAULT DISPATCHER" with the ≥75% criterion and 3 documented exceptions, (b) AGENTS.md gained a "Daily loop (canonical)" section placed BEFORE the kerf-planning section, (c) new project-local skill `.claude/skills/harmonik-dispatch/SKILL.md` loads on session-resume and gates dispatch decisions. Full design in `docs/orchestration-protocol-v2.md`.
2. **hk-rp48p priority-claim bug FIXED** (`2e48555`). Root cause: `brcli.Ready` invoked `br ready --format json` with NO `--sort` flag; br's default is `hybrid` (age-weighted), not `priority`. One-line fix: pin `--sort priority`. Reviewer APPROVE with 2 follow-ups filed (`hk-tul2a` scenario test, `hk-uhvjo` spec amendment).
3. **hk-wx8z8 parallel pane allocator FIXED** (`5e8f868`). Root cause: `tmuxSubstrate` held a substrate-wide `lastPaneID` mutated by every `SpawnWindow`; concurrent calls raced. Fix: per-session `paneID` captured atomically from `tmux new-window -P -F "#{pane_id}"`; new `WritePane`/`SendEnter`/`SendQuit` methods. Two new race-tests pass. PL-021d spec amended. Substrate-level `lastPaneID` kept vestigial for hk-aievp/hk-zrj83 test back-compat (small follow-up cleanup possible).
4. **hk-ppt32 queue-on-cancel FIXED** (`5e651c2`). SIGINT/timeout now drains the queue to `cancelled` and renames `queue.json` to `queue.json.cancelled-<ts>` so the next run loads clean. Exit code 1 on cancel (was 2 + stuck queue). Two new tests pass.
5. **Plain-English re-framing of "Go-touching dogfood RED":** the Go-touching probe (hk-6x7dw) DID NOT fail — claude completed work, committed `d36c4d2`, exited via `/exit`. Daemon misclassified because no `outcome_emitted` reached the socket (Stop-hook wiring gap). Recovered: cherry-picked the work, closed hk-6x7dw, demoted hk-ajhqw to P2-umbrella. **Real fix lives in `hk-cj0gm` (P1) — Stop-hook delivery audit.**

## The single remaining blocker for "code-touching through harmonik"

`hk-cj0gm` (P1): in worktree-provisioned `.claude/settings.json` the Stop hook may not be wired (or `/exit` bypasses Stop hooks in claude-code 2.1.145 vs `/quit`). Until this lands, harmonik will keep flagging successful code-touching runs as `claude_crashed`. Diagnosis (with file:line cites) was written to `docs/kerf-feedback/2026-05-20-hk-ajhqw-crash.md` by the investigator but is currently lost in the working-tree dance — re-derive from hk-cj0gm description and `~/.claude/projects/-Users-gb-github-harmonik--harmonik-worktrees-019e4396-bc65-7860-8dde-d5e76dbbfb90/019e4396-bfde-72b7-af19-fa91246ea3c4.jsonl` (the claude session transcript proving success).

## Priority cleanup applied

- `hk-judtf` (Plan 009 umbrella) — CLOSED (all 6 children landed)
- `hk-ux915`, `hk-5mjrs`, `hk-51ivc`, `hk-kx498` — demoted P1 → P2
- `hk-ajhqw` — demoted P1 → P2 (umbrella; children own the real fix)

**Open P1 list is now ONE bead**: `hk-8jh26` (harmonik run: hang on bead failure + always exits 0 + silent queue overwrite). Reviewer-flagged from v48; not yet fixed.

## Next session — priorities

1. **Dispatch `hk-cj0gm` through a sub-agent** (this IS the "harmonik bug" exception under the new HARD-RULE). Once Stop-hook delivery is reliable, code-touching dispatch through `harmonik run` becomes routine.
2. **Re-probe code-touching dispatch** after hk-cj0gm lands. Pick a tiny `internal/core/` const-rename bead. Confirm daemon reports success (not crash) end-to-end.
3. **Re-probe `--max-concurrent 2`** dispatch — hk-wx8z8 should make this work now. Pick two non-conflicting docs beads from the candidate list below.
4. **If both probes GREEN, start batching.** Route 3-5 P2 beads at a time through `harmonik run --beads`. Target ≥75% of substantive commits via harmonik this session.
5. **Tackle `hk-8jh26`** (the last P1) once code-touching dispatch is reliable.

## Candidate beads ready for batch-routing through harmonik

- `hk-ynn8u` — `.gitignore`: add `.harmonik/agent-task*` pattern (small, docs-ish)
- `hk-xlq2e` — Handler-pause: submitter-agent query docs + AGENTS.md note
- `hk-vh1bw` — Spec drift: queue-model.md §8.7 input-event vocabulary
- `hk-m0uop` — queue-model.md spec drift on -32018 + missing test cases
- `hk-tul2a` — scenario test for daemon br-ready priority claim path (hk-rp48p follow-up)
- `hk-uhvjo` — BI-013 spec amendment documenting --sort priority

## Files to open first

1. `HANDOFF.md` (this).
2. `AGENTS.md` §"Daily loop (canonical)" + `.claude/skills/harmonik-dispatch/SKILL.md` — the new default protocol.
3. `docs/orchestration-protocol-v2.md` — full design + rationale.
4. `docs/kerf-feedback/2026-05-19-phase2-{exit-path,readiness-audit}.md` — what's still untested.
5. `internal/daemon/tmuxsubstrate.go` + `internal/daemon/pasteinject.go` — wx8z8 fix landing zone (per-session paneID).
6. `internal/queue/persistence.go` `CancelQueueOnShutdown` — ppt32 fix.

## Plain-English glossary

- **`harmonik run --beads <id>`** — single-shot dispatch CLI. Spawns claude, watches, commits, merges to main, pushes, closes the bead. Validated for docs in v51; parallel + code-touching now fixed and need re-probe.
- **harmonik-dispatch skill** — new project-local skill, loads on session-resume; gates dispatch decisions to default to `harmonik run`.
- **Daily loop / Orchestration v2** — `bv --robot-triage` → `kerf next` → pick batch → `harmonik run --beads ... --max-concurrent N` → review + repeat. Sub-agent dispatch is the exception, not the default.
- **75% rule** — target ≥75% of substantive commits per session land via `harmonik run` (committer identity / `Refs:` trailer).
- **hk-rp48p** — CLOSED. br ready was sorting by `hybrid` (age) not `priority`. Fix pins `--sort priority`.
- **hk-wx8z8** — CLOSED. Parallel sessions shared a single `pane_target` via substrate-wide state. Fix: per-session paneID.
- **hk-ppt32** — CLOSED. SIGINT/timeout now drains queue to `cancelled` and renames `queue.json` so the next run starts clean.
- **hk-cj0gm** — OPEN P1. Stop-hook `outcome_emitted` not reaching daemon socket. The thing blocking reliable code-touching dispatch.
- **hk-17eci** — OPEN P3. tmuxSubstrate runWait reports `ExitCode=-1` on ctx-cancel; should reconcile with `processDead(pid)` first.
- **hk-ajhqw** — OPEN P2 umbrella. NOT a crash; daemon misclassified hk-6x7dw success. Children hk-cj0gm + hk-17eci own real fixes.
- **hk-8jh26** — OPEN P1, pre-existing. harmonik run: hang on bead failure + always exits 0 + silent queue overwrite.
- **hk-tul2a / hk-uhvjo** — OPEN follow-ups from hk-rp48p reviewer (scenario test, spec amendment).
- **Half-built systems pattern** — feature unit-tested + reviewer-APPROVED but broken at runtime. Dogfood is the canonical test.

## Loose ends / blockers

- **`harmonik-twin-claude/` stray untracked directory** at repo root — persists across multiple sessions now. Inspect contents before deleting; likely a worktree-dispatch leak.
- **`.beads/.br_history.226mb-archived/`** (226MB) — safe to delete.
- **`docs/kerf-feedback/2026-05-20-hk-ajhqw-crash.md`** — investigator wrote it, got lost in working-tree dance. Re-create from hk-cj0gm description + the claude session transcript if needed.
- **`.gitignore` strip-on-`br doctor`** — `br doctor --repair` removes `.beads/*` from the ignore file; revert manually if it happens again. File feedback at `docs/kerf-feedback/2026-05-20.md`.
- **kerf was UPDATED AGAIN** mid-session per user note — friction in `docs/kerf-feedback/2026-05-20.md` may already be fixed in the newest version. Re-probe `kerf next` early next session and log any new findings to a fresh dated file (NOT amending 2026-05-20.md).
- **Pre-existing `TestBI010c_SpecContainsWorkflowLabelDiscipline` failure** — fixture reads wrong paragraph. File a small bead.

## No hard blockers requiring user input.
