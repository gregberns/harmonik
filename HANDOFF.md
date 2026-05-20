<!-- PP-TRIAL:v2 2026-05-19 main — v50. Phase-2 dogfood OPERATIONAL: 6/6 harmonik run --beads dogfoods landed real spec/Go changes end-to-end. ALL hit bead-close timeout (root cause still unknown after 3 hypotheses). MVH terminology scrubbed (hk-wn2pl). 5 scenario-test gap beads filed. ~73 commits past v49. Outstanding: hk-5dewt REOPEN — auto-close stays elusive. -->

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

# Where we are (v51, 2026-05-19/20)

**Main at `60b4abd`. ~15 commits past v50.** Working tree has `M .beads/issues.jsonl` (hk-2nril auto-close from dogfood — to be committed) + stray untracked `harmonik-twin-claude` directory + two new investigator reports under `docs/kerf-feedback/`.

## 🟢 Phase 2 unattended is VALIDATED end-to-end

**hk-2nril dogfood (commit `60b4abd`):** `harmonik run --beads hk-2nril` ran to completion, exit code 0, in 101s. Bead auto-closed (close_reason=done, closed_at 2026-05-20T03:34:03Z). Daemon edited AGENTS.md, committed, merged to main, AND pushed to origin — orchestrator was bypassed entirely. **The previous 6-run timeout pattern is closed.** Log at `/tmp/dogfood-v51.log`.

This is the proof that was missing in v50. The retry-cap fix (2s→15s) works in production, not just in the lab.

## Headline outcomes (v50 → v51)

1. **hk-5dewt ROOT-CAUSED + FIXED** (commits `2298cab` + `32259dd`). The 19-second "bead-close timeout" was never about `br close` itself — `br close` is 0.35s. It was harmonik's own retry loop fighting itself: `UnavailableRetryMax=10` with `cap=2s` meant up to 11 br instances queued up, each holding `.write.lock` for ~15s under SIGTERM grace before being killed. Fix: raise `UnavailableRetryCap` 2s → 15s in `internal/brcli/dblockretry.go:53`. Three prior fixes (retry-widen, WAL pre-flight, .br_history rotation) kept as defense-in-depth, not load-bearing. Reproduction recipe in `docs/kerf-feedback/2026-05-19.md` §hk-5dewt.
2. **4 scenario tests landed (P0 backlog from gap audit)** — `hk-6f1uj` budget_exhausted (`1270265`), `hk-t5j2w` review-loop tmux substrate (`728cc6a`), `hk-qxtbq` handler-fatal dispatcher gate (`918bdf8`), `hk-nfhqd` reviewer agent_ready timeout (`b92dc69`). Each exercises composition-root wiring through `ExportedRunReviewLoop` / `ExportedRunWorkLoop` / `daemon.Start`. Twin gained `budget-exhausted` and `handler-fatal` scenarios.
3. **Plan 009 CLI help redesign COMPLETE** (6 beads: `hk-judtf` umbrella + `hk-oj65f`/`vudz0`/`ct3t9`/`y4e96` impls + `hk-u0oo2` tests). `harmonik --help` lists all 6 subcommands; every subcommand responds to `--help`/`-h`; `(default 1) (default 1)` bug fixed. New `cmd/harmonik/usage.go` and `cmd/harmonik/help_test.go` (7 substring-assertion tests). Plan at `plans/009_cli_help_redesign/_plan.md`.
4. **Live dogfood validated everything end-to-end.** hk-2nril shipped real AGENTS.md edits via harmonik with zero orchestrator intervention.

## 🟡 Three caveats before declaring "all work through harmonik"

The readiness auditor and retry-cap reviewer both flagged areas the 6-prior dogfoods did NOT exercise. Validators returned GREEN on the single-bead, P2-docs, single-concurrency case — that's the proven envelope.

### Untested workload classes (HIGH severity)
1. **Code-touching beads** never dogfooded. Agent has never compiled, hit a build error, or run `go test` inside a harmonik-dispatched session. Failure path: compile loop → no `/quit` → daemon's quit-on-commit fires on broken HEAD. **Suggested probe:** dispatch ONE trivial Go-touching bead (e.g. add a const or rename a var).
2. **`--max-concurrent N > 1`** only twin-tested. Real parallelism (claude × N) is unproven. `.br_history` rotation + brAdapter shared state under contention are race candidates. **Suggested probe:** 2-bead `--max-concurrent 2` dogfood.
3. **Priority-claim bug (hk-rp48p OPEN)** — daemon claimed P1 IN_PROGRESS stale bead over P0 ready. Auto-dispatch may silently misroute. Exclude priority-sensitive workloads until fixed.

Full report: `docs/kerf-feedback/2026-05-19-phase2-readiness-audit.md`.

### Retry-cap fix follow-ups (MEDIUM severity, REQUEST_CHANGES from reviewer)
- Worst-case wait if all 10 retries fail with cap=15s: ~206 seconds before surfacing failure. Single-instance is fine (cap prevents self-cascading). Concurrent instances or external contention can still trigger this. Track as follow-up.
- Test gap: no behavioral test verifies the non-cascading invariant. The constant-check test just asserts the value, not runtime behavior. File a small bead.

### Exit-path investigator returned CLEAN
- **NO hang risks identified** in the post-success path. CancelOnQueueExit fires correctly; all watchdogs bounded; `bus.Drain` is never called (events may drop, but no hang); per-connection socket handlers not awaited (cosmetic).
- Worth one small follow-up bead: add `bus.Drain()` + `daemon_stopped` emit at shutdown for observability completeness.
- Full report: `docs/kerf-feedback/2026-05-19-phase2-exit-path.md`.

## Next session — priorities

1. **Commit the pending state.** `.beads/issues.jsonl` has hk-2nril auto-close from dogfood — needs to be committed. The two new investigator reports in `docs/kerf-feedback/` also need adding.
2. **Run the two outstanding probes** (in parallel):
   - One Go-touching bead through `harmonik run` (validates compile/test loop)
   - One 2-bead `--max-concurrent 2` dogfood (validates parallelism)
3. **If both probes pass, the orchestrator stops being the dispatcher.** Route the next 5+ P2 docs beads through harmonik in batches.
4. **File follow-ups** flagged by reviewers: (a) 206s worst-case retry exhaustion, (b) test for non-cascading invariant, (c) `bus.Drain` + `daemon_stopped` at shutdown.
5. **Cleanup:** investigate `harmonik-twin-claude` stray directory before deleting; archive `.beads/.br_history.226mb-archived/`; revisit `.gitignore` regression (br doctor strips `.beads/*`).

## Candidate P2 docs beads ready for batch-routing through harmonik (post-probes)

- `hk-ynn8u` — `.gitignore`: add `.harmonik/agent-task*` explicit pattern
- `hk-xlq2e` — Handler-pause: submitter-agent query docs + AGENTS.md note
- `hk-vh1bw` — Spec drift: queue-model.md §8.7 input-event vocabulary
- `hk-m0uop` — Follow-up: queue-model.md spec drift on -32018 + missing test cases
- `hk-h7eke` (if open) — workflow-modes spec

## Files to open first

1. `HANDOFF.md` (this).
2. `docs/kerf-feedback/2026-05-19.md` §hk-5dewt — root-cause investigation that landed the fix.
3. `docs/kerf-feedback/2026-05-19-phase2-exit-path.md` — exit-path trace, clean verdict.
4. `docs/kerf-feedback/2026-05-19-phase2-readiness-audit.md` — workloads still untested.
5. `internal/brcli/dblockretry.go:53` — the fix.
6. `plans/009_cli_help_redesign/_plan.md` — Plan 009 complete.

## Plain-English glossary

- **hk-5dewt / hk-5ce5n** — CLOSED. Bead-close timeout root cause. Fix: `UnavailableRetryCap` 2s → 15s.
- **hk-2nril** — CLOSED. The bead used as the live dogfood-validation target (kerf bench-location clarification in AGENTS.md). Auto-closed via `harmonik run` — first fully-unattended Phase-2 run.
- **Plan 009 / `codename:cli-help`** — CLOSED. Six-bead redesign of `harmonik --help` so agents can self-discover the CLI surface.
- **hk-6f1uj / hk-t5j2w / hk-qxtbq / hk-nfhqd** — CLOSED. The 4 scenario tests; each catches a half-built-systems pattern.
- **hk-judtf** — Plan 009 umbrella epic.
- **hk-rp48p** — OPEN. Priority-claim bug: daemon claimed P1 in_progress over P0 ready. Blocks priority-sensitive auto-dispatch.
- **Half-built systems pattern** — feature is unit-tested + reviewer-APPROVED but breaks in production because composition-root wiring or runtime-environment was untested. Recurring; dogfood is the canonical test.
- **Phase 1 operational milestone (2026-05-14)** — daemon runs jobs end-to-end with zero human input. (Previously "MVH" — retired.)
- **Phase 2 unattended = VALIDATED for the single-bead, P2-docs, --max-concurrent=1 envelope** as of v51. Code-touching and parallelism still need probes.
- **`harmonik run --beads <id>`** — Phase-2 dispatch CLI. Now responds to `--help` per Plan 009.
- **`.write.lock`** — `.beads/.write.lock` — the fcntl LOCK_EX file every `br` write contends on. Held ~350ms uncontested; up to 15s under SIGTERM grace.

## Loose ends / blockers

- `M .beads/issues.jsonl` (hk-2nril close from dogfood) + untracked `docs/kerf-feedback/2026-05-19-phase2-{exit-path,readiness-audit}.md` need committing.
- `harmonik-twin-claude/` stray untracked directory at repo root — looks like a leak from a worktree dispatch. Inspect before deleting.
- `.beads/.br_history.226mb-archived/` (226MB) — safe to delete now that root cause is known.
- `.gitignore` strip-on-doctor regression — file a kerf-feedback note + restore-on-detect protocol.
- Pre-existing `TestBI010c_SpecContainsWorkflowLabelDiscipline` test failure — fixture reads wrong paragraph. Several reviewers have flagged. File a small bead.

## No hard blockers requiring user input.
