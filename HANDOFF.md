<!-- PP-TRIAL:v2 2026-05-23 main — v55. DOT (kerf phase-3-dot) advanced from change-design (5-day stall) → integration (pass-6) in one session: 5 component spec drafts written (1414 lines, WG/EM/HC/CP IDs assigned), pass-4 + pass-5 reviewers APPROVED, 10 test beads filed and pinned. 9 harmonik commits landed earlier (hk-mtm0w/xegej/ndysh/59lg8/0xmwq/8uy6m/mmh8f/6zylj/yozgd-protocol). 2 daemon bug-fixes via sub-agent (hk-0xmwq reviewer-brief, hk-6zylj worktree-escape). Heartbeat-staleness HARD-RULE added (v55 directive — d339fca) + kerf work `daemon-liveness` opened. 10-min wall-kill in pasteinject.go:104 is NOT fixed (user pushback; survival-layer-only). Salvage patches parked: /tmp/escape-recovery.{patch,untracked.tgz}. -->

Roadmap: [ROADMAP.md](ROADMAP.md). Cross-project working-style rules: `~/.claude/CLAUDE.md`. Plans index: [plans/README.md](plans/README.md).

<!-- ORCHESTRATION DIRECTIVES — DO NOT EDIT EXCEPT BY EXPLICIT USER REQUEST. Loaded every /session-resume. -->

ROLE. You are the orchestrator. Delegate substantively. Keep the main thread minimal.

LEARNING LOG (READ ON EVERY RESUME). `docs/orchestration-learnings.md` — friction-and-fix log. Read on `/session-resume`. Append new entries when you observe friction. Promote durable rules to this directives block or `.claude/implementer-protocol.md`.

STREAM-NOT-WAVES (HARD RULE). The orchestrator runs a CONTINUOUS STREAM of implementers, not synchronous waves. On every implementer-completion notification, do exactly two things, in order: (1) Merge the returning implementer; (2) inspect dispatchable depth and either spawn one replacement OR note "queue draining" and stop spawning.

Per-return acknowledgment is ≤2 lines. Full session summary lives at `/session-handoff` time.

**HARMONIK IS THE DEFAULT DISPATCHER (HARD RULE, v51).** Substantive work routes through `harmonik run --beads <ids>` unless an exception applies. The intended daily loop: `bv --robot-triage` → `kerf next` → pick batch of 3–5 → `harmonik run --beads id1,id2,... --max-concurrent N` → while it runs, queue next batch / drain triage / file follow-ups → on exit, review + dispatch next batch. Target: ≥75% of substantive commits per session land via `harmonik run` (committer identity / `Refs:` trailer in `git log`). The three exceptions: (a) the bead is a bug-fix to harmonik itself in code that breaks dispatch; (b) ≤2-line typo/cross-reference fix where ~30s daemon overhead isn't worth it; (c) untested workload class per the readiness-audit caveats (priority-sensitive routing — until hk-rp48p's regression test lands; `--max-concurrent > 1` — until hk-wx8z8 lands; code-touching — until the Go-touching probe passes). Sub-agent dispatch is otherwise the WRONG move. If you find yourself reaching for the Agent tool on a 4th task in a row, STOP — batch them and run `harmonik run --beads`. Full design: `docs/orchestration-protocol-v2.md`.

**EVERY BEAD GETS A REVIEW PHASE (HARD RULE, v53 NEW — USER-ORDERED 2026-05-21).** `harmonik run` MUST be invoked with `--review-loop` on every batch. No exceptions. The point of harmonik's per-bead workflow IS implement → review → fix — skipping review defeats it. Round-2 session ran 12 commits without `--review-loop` and the user flagged it; do not repeat. P0 bead **hk-g0ckv** flips the default in `cmd/harmonik/run.go` (move from opt-in `--review-loop` to opt-out `--no-review-loop`) — until that lands, the orchestrator MUST pass `--review-loop` explicitly. Verification: each landed commit should carry a `Reviewed-By: agent-reviewer` + `Review-Verdict:` trailer; if absent on a `Refs: <bead-id>` commit, the review was skipped and the bead should be re-opened.

**HARMONIK DOES (BASICALLY) ALL THE WORK (HARD RULE, v53 REINFORCEMENT).** The Agent tool is for the THREE narrow exceptions in the harmonik-default-dispatcher rule above. Any Agent-tool dispatch must justify itself against those exceptions in the same message that issues the call. Anything that looks like "I'll just have a sub-agent do this" without an exception applied is the WRONG choice — file it as a bead and route via `harmonik run --beads ... --review-loop`.

**FRICTION GETS PRIORITY (HARD RULE, v53 NEW — USER-ORDERED 2026-05-21).** Any bead labeled `phase2-dogfood-friction`, `kerf-upstream`, `review-gate`, or otherwise tagged as breaking the orchestrator's loop MUST be filed at P1 minimum (P0 if it's hit the operator twice in the same session). When choosing the next batch, friction beads jump ahead of substantive feature work. Rationale: friction compounds — every unfixed daemon hang is a tax on every future dispatch.

**KERF IS THE PRIORITY SOURCE OF TRUTH (HARD RULE, v53 NEW — USER-ORDERED 2026-05-21).** Use `kerf next` as the dispatch feed. If you disagree with kerf's ranking, do NOT silently pick a different bead — investigate the disagreement. Likely causes: (a) the kerf work's `bead_filter` is missing a `codename:` label on the bead, (b) the kerf work itself has wrong area/priority weights, (c) the bead is mis-labeled (file `label:kerf-upstream` if it's a kerf bug). Document the resolution as a kerf-feedback entry under `docs/kerf-feedback/<date>.md`. Goal: kerf's recommendation = the right answer; agent-overrides are evidence of a fixable upstream defect.

**PHASE-3 DOT IS THE NEAR-TERM ENDGAME (v53 NEW — USER-ORDERED 2026-05-21).** The DOT-defined bead-process workflow (`~/.kerf/projects/gregberns-harmonik/phase-3-dot/`) is the planned replacement for the current `--review-loop` pattern. The work is still in change-design pass — no beads exist yet. Next-session priorities for advancing DOT: (1) finish the design pass, (2) draft the spec, (3) spawn implement/review/test beads, (4) ship enough of the DOT runtime that we can dispatch a single bead through it end-to-end. Until DOT ships, `--review-loop` remains the gate. Once DOT is operational, the implement/review/fix loop becomes structural rather than per-bead-CLI-flag.

PHASE 2 IS UNBLOCKED (NEW v38). With harmonik operational you CAN now dispatch beads via the daemon instead of via the Agent tool — file a bead with `br create`, start harmonik against the project, watch it execute. Trade-off: harmonik overhead is ~30s+ per bead vs sub-agent's seconds; use it when (a) durability matters, (b) the work spans sessions, (c) tmux inspectability matters, or (d) parallel `--max-concurrent N` amortizes the overhead. For trivial inline work, sub-agent dispatch still wins.

`harmonik run <bead-id>` IS LIVE (NEW v48). Single-bead invocation: `harmonik run <id> [--project DIR]` builds a queue-of-one, runs the daemon, exits on completion. Exit code: 0 success / 1 paused-by-failure / 2 unexpected. Refuses overwrite of an active queue.json. Hangs avoided via `CancelOnQueueExit`. THIS IS the canonical Phase-2 dispatch UX — use it instead of priority-bump tricks.

`harmonik run --beads` MULTI-BEAD + --context + --review-loop (v49 NEW). Multi-bead one-shot: `harmonik run --beads id1,id2,... --max-concurrent N [--context "string|@file"] [--review-loop]`. Builds a queue of N items, parallel dispatch up to max-concurrent, single daemon, exits on completion. `--context` adds an Extra Context section to the agent-task.md for the handler. `--review-loop` selects WorkflowModeReviewLoop. Landed at `0da3a71`/`ebd25a4` via hk-w3cp1+hk-boiwe+hk-hiqrl.

`harmonik run --notify-stream` (v53 LIVE). Per-bead completion lines `[hk-XXX] success|failed` emitted to stdout; combine with a Monitor wrapper to surface mid-batch progress. Landed at `ce9d0e4` via hk-ibilr.

PASTEINJECT AUTO-RECOVERY IS IN THE DAEMON NOW (v53, hk-trjef commit f2c395e). The Monitor-based auto-hang-kill pattern from earlier sessions is REDUNDANT for the rebuilt binary — `pasteinject.go:146-208` does quit → 30s grace → kill → noChange-subsumed check natively. **Always rebuild harmonik before dispatching** (`go install ./cmd/harmonik`); stale binary is the #1 cause of "the daemon hung again". The AGENTS.md "Orchestrator wrappers" Monitor block is now FALLBACK ONLY (hk-yejfj filed P1 to revise).

QUEUE SEMANTICS (v53 FINDINGS). `harmonik run --beads` creates `kind=wave` queues that do NOT accept appends. Mid-flight extension requires `kind=stream` via `harmonik queue submit <file>` + `harmonik queue append --queue-id <uuid> <group> <bead-ids...>`. Daemon doesn't wake on submit if idle (workaround: keep an active `harmonik run` so the workloop stays hot). Quick-win beads filed: **hk-7nbey** (default `--beads` to `kind=stream`), **hk-24xn1** (daemon wake-on-submit), **hk-b0cyc** (UX gap). **hk-ze3op** (default `--notify-stream` on for multi-bead). **hk-lhv8i** (pre-screen subsumed at submit-time — eliminates the noChange slot-waste that hit ~10 beads in this session).

PRE-SCREEN STALE-OPEN BEADS BEFORE DISPATCH. Until hk-lhv8i lands, manually screen each bead in the batch: `git log --all --grep "Refs: <id>" --oneline`. If it returns a hit, the implementation already landed — `br close <id> --reason "Subsumed: landed as <sha>"` instead of dispatching. Today's session caught 10+ pre-merged beads this way; each saved a wasted ~5-min dispatch.

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

CONTEXT BUDGET (orchestrator). ~700 k effective. v48 used ~heavy across 15+ background sub-agents — kerf/bead/plan hygiene + 4 worktree implementers + 3 reviewers. 16 commits. v49 used ~51% across ~15 audit agents + 4 implementers + dogfooding cycle. 35 commits. v53 used ~25% across 20-bead dogfood (2 rounds via harmonik) + 3 follow-up audit agents. 18 commits.

HARNESS BLOCKS `.md` WRITES FOR SUB-AGENTS (v47 NEW). Some sub-agents hit a system-prompt rule blocking `.md` writes for "findings/analysis/summary" files — they return content inline. Orchestrator (main thread) must persist via `Write` tool. When dispatching kerf-pass or audit sub-agents that must write `.md` artifacts, expect this friction and plan for orchestrator persistence.

KERF IS IN BETA + REALIGNED (v48 NEW). `kerf next`, `kerf triage`, `kerf pin`, `kerf work edit`, `kerf map`, `kerf areas` all functional. v48 created 2 new kerf works (`handler-pause`, `phase-2-completion`) so 30+ formerly-orphan beads now surface in `kerf next`. Filter syntax supports OR via repeated `--bead-filter-add` clauses (produces `any=[...]`). 15+ kerf-upstream bugs filed (`label:kerf-upstream`). Feedback log: `docs/kerf-feedback/<date>.md` (per-session dated file, NEW v49 convention). **Use `kerf next` as the primary dispatch surface.** phase-3-dot filter is intent-correct but matches zero beads until spec-amend/task beads are spawned (work is still in change-design pass). Local jig customization: `kerf jig save <name>` → edit → `kerf jig load <name> <path>` (hk-85trr P1 to apply for testing-criteria convention).

PLANS HAVE "DONE MEANS..." (v49 NEW). `plans/README.md` now requires every `_plan.md` to include an explicit "Done means..." section listing observable behavioral acceptance criteria, NOT "the beads shipped." Guards against minimum-viable shipping. Applied to `plans/007_handler_pause_and_resume/_plan.md` as the example. hk-b6ls5 extends to require scenario-test + exploratory-test beads at plan-end; hk-85trr applies the same to kerf jig templates locally.

**HEARTBEAT-STALENESS WATCH (HARD RULE, v55 NEW 2026-05-23 — survival layer until kerf `daemon-liveness` redesign lands).** Every `harmonik run` dispatch MUST arm a heartbeat-staleness watcher in addition to the existing bash-task + events.jsonl monitors. Daemon emits `agent_heartbeat` events at ~5 min intervals; staleness >6 min on any active run means the implementer has gone silent BEFORE the 10-min `commitPollTimeout` wall-kill (`pasteinject.go:104`) fires. Background: the wall-kill destroys productive work — even trivial 1-line beads failed at the 10-min mark on the 2026-05-22 post-eb43a6b batch. Watcher pattern (Bash background, 60s poll):

```bash
while true; do
  for rid in $(python3 -c "import json; q=json.load(open('.harmonik/queue.json')); [print(i['run_id']) for g in q['groups'] for i in g['items'] if i.get('status')=='dispatched' and i.get('run_id')]"); do
    last_hb=$(grep "\"run_id\":\"$rid\"" .harmonik/events/events.jsonl | grep agent_heartbeat | tail -1 | python3 -c "import sys,json,datetime; print(int(datetime.datetime.fromisoformat(json.loads(sys.stdin.readline())['timestamp_wall']).timestamp()))" 2>/dev/null)
    [ -z "$last_hb" ] && continue
    age=$(( $(date +%s) - last_hb ))
    [ $age -gt 360 ] && echo "HEARTBEAT-STALE: run $rid age=${age}s (>6min) — implementer silent, decide before 10min wall-kill"
  done
  sleep 60
done
```

Proper redesign tracked in kerf work **`daemon-liveness`** (`~/.kerf/projects/gregberns-harmonik/daemon-liveness/`). Eventual DOT (kerf `phase-3-dot`) replaces this entire brittle layer.

<!-- END DIRECTIVES -->

# Where we are (v55, 2026-05-23)

**Main at `d339fca`** (origin parity, working tree clean). Status: **clean** — no in-flight dispatch, no orphan worktrees, no uncommitted code in main.

## What happened this session (the short version)

Two threads: (1) a brittle harmonik dispatch cycle that exposed three real daemon bugs and got them fixed; (2) a big DOT (kerf phase-3-dot) push that moved a 5-day-stalled change-design through to integration. Net: 9 substantive code commits via harmonik+sub-agents (hk-mtm0w / hk-xegej / hk-ndysh / hk-59lg8 / hk-0xmwq / hk-8uy6m / hk-mmh8f / hk-6zylj / hk-yozgd-protocol) + 5 full DOT spec drafts (1414 lines).

**Daemon bugs fixed this session:**
- `61f084a` hk-0xmwq P0 — reviewer-brief file `review-target.md` was never written; reviewer panes idled forever
- `f7f2cff` hk-mmh8f P0 — no-review-loop path falsely reported success when implementer never committed
- `eb43a6b` hk-6zylj P0 — implementer was escaping its worktree by anchoring absolute MAIN-repo paths after `find /Users/gb/github/harmonik/...` discovery; fix injects worktree-discipline preamble into agent-task.md + daemon-side post-implementer dirty-tree check

**Still NOT fixed (deliberately deferred — survival-layer-only until DOT lands):**
- `internal/daemon/pasteinject.go:104` `commitPollTimeout = 10 * time.Minute` kills implementers wall-clock-regardless-of-progress. Even a 1-line test-arity fix (hk-ortkx) failed at the 10-min mark when the daemon ran 3 parallel agents. **User pushed back on "design a proper liveness check"**; opened kerf work **`daemon-liveness`** to track the redesign. Survival layer is the v55 HARD-RULE: orchestrator MUST arm a heartbeat-staleness watcher on every dispatch (Bash poll on `agent_heartbeat` events; alert at >6min staleness).

## DOT progress (kerf phase-3-dot)

| Pass | This session | Output |
|------|--------------|--------|
| 4 — Change Design | APPROVE round-2 | C1/C2/C3/C5 design docs (C4 pre-existing) + 7 D-decisions all locked |
| 5 — Spec Draft | APPROVE round-1 (3 cross-component contradictions caught & patched inline pre-reviewer) | 5 drafts in `~/.kerf/projects/gregberns-harmonik/phase-3-dot/05-spec-drafts/` (1414 lines) + `05-changelog.md` + `spec-draft-review.md` |
| 6 — Integration | **CURRENT — not started** | next session writes `06-integration.md` |

**10 test beads filed + pinned** (5 scenario, 5 exploratory): hk-fiq55, hk-lphyf, hk-aoz34, hk-yfm05, hk-isp3y, hk-w3eip, hk-4fvid, hk-6zvki, hk-zqr6f, hk-geype. They surface in `kerf next` already.

## Top priorities for next session

1. **DOT pass-6 integration.** Write `~/.kerf/projects/gregberns-harmonik/phase-3-dot/06-integration.md` — cross-spec coordination plan + dependency order + ship-readiness check + the C6 implementation epic (the spec→code bead decomposition). Then pass-6 reviewer → advance to pass-7 (tasks) → spawn implementation beads. **This is the DOT critical path.**
2. **Salvage parked work.** `/tmp/escape-recovery.patch` (277 lines) + `/tmp/escape-recovery-untracked.tgz` (6.9 KB) contain the hk-wkzlc + hk-jon6r implementations that escaped to main during the buggy dispatch cycle. Probably correct work (codename in filename, follows the bead spec); needs a sub-agent to extract, verify, commit with proper `Refs:` trailers. Cheap to skip if DOT integration is more valuable.
3. **Friction backlog still real:** hk-930o3 (pasteinject `/exit`-before-brief race, P1) is the one substantive daemon bug filed-but-not-fixed; will reappear under heavy dispatch.
4. **Heartbeat-watcher discipline.** If you dispatch anything via `harmonik run` next session, the v55 HARD-RULE requires the heartbeat-staleness watcher in addition to the bash-task + events.jsonl monitors. The watcher pattern is in the directives block above.

## Files to open first

1. `HANDOFF.md` (this).
2. `~/.kerf/projects/gregberns-harmonik/phase-3-dot/05-spec-drafts/05-changelog.md` — DOT pass-5 traceability + the 3 cross-component contradictions resolved at pass-5 close (read this BEFORE the design docs).
3. `~/.kerf/projects/gregberns-harmonik/phase-3-dot/spec-draft-review.md` — pass-5 APPROVE verdict + 3 NIT/MINOR findings carried into integration.
4. `~/.kerf/projects/gregberns-harmonik/daemon-liveness/01-problem-space.md` — the survival-layer-vs-DOT framing for the timeout discussion.

## Plain-English glossary

- **harmonik** — project-local daemon dispatching beads to claude sub-sessions; commits, merges, pushes, closes.
- **DOT (kerf `phase-3-dot`)** — DAG-defined bead-process runtime; planned replacement for `--review-loop`. Now in pass-6 integration.
- **kerf `daemon-liveness`** — survival-layer work tracking the proper replacement for the 10-min wall-kill (`commitPollTimeout`). Will be subsumed by DOT but useful in the gap.
- **commitPollTimeout** — the 10-min hardcoded wall-kill at `pasteinject.go:104`. Kills implementers regardless of progress. NOT fixed this session; survival layer is the heartbeat-staleness watcher.
- **worktree-escape (hk-6zylj, FIXED `eb43a6b`)** — implementer running `find /Users/gb/github/harmonik/...` (absolute MAIN path) got back MAIN paths and Wrote to them; work landed in main's working tree, not the worktree. Fix: agent-task.md preamble + daemon post-implementer dirty-tree check.
- **review-target.md (hk-0xmwq, FIXED `61f084a`)** — reviewer brief file the daemon was never writing; reviewers idled.
- **no-review-loop false-positive (hk-mmh8f, FIXED `f7f2cff`)** — `--no-review-loop` path reported success when implementer never committed.
- **heartbeat-staleness watcher (v55 HARD-RULE, `d339fca`)** — orchestrator-side Bash poll alerting at >6min staleness on `agent_heartbeat` events. Stop-gap.
- **C1–C5** — DOT spec-draft components: C1=`specs/workflow-graph.md` (NEW), C2=`execution-model.md §7.5`, C3=`handler-contract.md §Outcome ext`, C4=`control-points.md §node-type binding`, C5=`specs/examples/review-loop.dot`.
- **/tmp/escape-recovery.{patch,untracked.tgz}** — uncommitted hk-wkzlc + hk-jon6r work that escaped main during buggy dispatch; salvage candidate.

## No hard blockers requiring user input.
- **Subsumed bead** — bead whose implementation already landed on main but the ledger never closed it. Pre-screen with `git log --all --grep "Refs: <id>"` before dispatching.

## Loose ends

- **`harmonik-twin-claude/` stray untracked directory** at repo root — still present from earlier sessions. Inspect before deleting.
- **`.beads/.br_history.226mb-archived/`** (226MB) — safe to delete.
- **`docs/kerf-feedback/2026-05-21.md`** — was not written this session; the friction findings live in the new bead descriptions (hk-b0cyc, hk-24xn1, hk-yejfj, hk-ze3op, hk-7nbey, hk-lhv8i, hk-g0ckv, hk-3jmke). File a consolidated kerf-feedback entry next session.

## No hard blockers requiring user input.
