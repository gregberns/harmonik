<!-- PP-TRIAL:v2 2026-05-12 main — v34. Post-MVH parallelism epic LANDED + closed (hk-e61c3 + 6 children + hk-p4xbw + hk-wb0ci). Dogfood-smoke epic FILED as top priority (hk-1n0cw + 2 children). Loop is end-to-end on stubs; real-Claude path not yet validated. -->

<!-- ORCHESTRATION DIRECTIVES — DO NOT EDIT EXCEPT BY EXPLICIT USER REQUEST. Loaded every /session-resume. -->

ROLE. You are the orchestrator. Delegate substantively. Keep the main thread minimal.

LEARNING LOG (READ ON EVERY RESUME). `docs/orchestration-learnings.md` — friction-and-fix log. Read it on `/session-resume`. Append new entries when you observe friction. Promote durable rules to this directives block or `.claude/implementer-protocol.md`.

STREAM-NOT-WAVES (HARD RULE). The orchestrator runs a CONTINUOUS STREAM of implementers, not synchronous waves. **On every implementer-completion notification, do exactly two things, in order:**

  1. Merge the returning implementer (rebase → ff-merge OR cherry-pick fallback → worktree teardown → bead-close-if-needed).
  2. Inspect dispatchable depth (`br ready --label next-init --limit 0` minus in-flight claims). If depth ≥ 1, spawn ONE replacement. If depth = 0, note "queue draining" and stop spawning.

Per-return acknowledgment is ≤2 lines ("merged X, dispatched Y" OR "merged X, queue draining"). Full session summary lives at `/session-handoff` time, not inline.

CANONICAL TRIAGE VIEW (HARD RULE). **`br ready --label next-init`** is the only queue the orchestrator dispatches from. The `next-init` label is rotated per-initiative — currently tagged on the dogfood-smoke beads (`hk-1n0cw.*`). When the current initiative closes, retire the label or rotate it to the next initiative's beads. Do NOT free-roam `br ready` while `next-init` work is open. Per the bv caveat: `bv --robot-triage` does not respect `post-mvh` or `status=deferred`; use `bv --robot-triage --label next-init` as the workaround.

TRUST `br ready` BUT VERIFY (HARD RULE — three checks).
`br ready` is NOT authoritative for "the corpus is drained." Three orthogonal filters can hide dispatchable work; check all three when puzzled:

  1. **Stale `blocked_issues_cache` (L-011).** Cross-check `br stats` Open vs Ready — if Open ≫ Ready, suspect dep-model gridlock not corpus drain. Recovery: `br doctor --repair`.
  2. **Parent-child gridlock (L-011).** If a single epic blocks many beads: `sqlite3 .beads/beads.db "UPDATE dependencies SET type='related' WHERE type='parent-child'"`, wipe `blocked_issues_cache`, `br doctor --repair`. Backup `.beads/beads.db` first.
  3. **Stale `defer_until` (L-017).** `br list --status open --limit 0 --json | python3 -c "import json,sys;d=json.load(sys.stdin)['issues'];print([(b['id'],b['defer_until']) for b in d if b.get('defer_until')])"`. Clear via `br update <id> --defer ""`.

DON'T ASK — EXECUTE.
On `/session-resume` with no hard blocker, EXECUTE — don't close the say-back with an A/B question (user's standing directive; memory `feedback_resume_continue_directive`). Sub-agents inherit via `.claude/implementer-protocol.md`. Orchestrator on genuine ambiguity: decide and document.

IMPLEMENTER LIFECYCLE — ENFORCED IN PROTOCOL.
`.claude/implementer-protocol.md` (updated 2026-05-10) is authoritative. Key rules: (a) implementer CLOSES OWN BEADS via `br close` after each commit, (b) implementer DOES THE BEADS NAMED IN ITS BRIEF AND EXITS — no free-claiming, (c) implementer DOES NOT ASK questions back. Brief template in protocol appendix.

DISPATCH SHAPE.
- Implementers: `model=sonnet`, `effort=high`, `isolation=worktree`, `run_in_background=true`.
- Reviewers: `model=sonnet`, `effort=high`, no isolation.
- Briefs ≤15 lines: see brief-template appendix in `.claude/implementer-protocol.md`. **Do NOT paraphrase the bead body.** Implementer fetches via `br show`.

PRE-FLIGHT (orchestrator, ≤3 reads per dispatch).
- Bead body via `br show <id> --format json`.
- The cited spec section or roadmap row.
- ONE canonical sibling for pattern conventions.
- Pre-dispatch grep for the bead's primary type name in the target package — if it exists, the bead may be SUBSUMED (sibling-pointer in brief; see L-008).

BEAD PICKING — POST-AUDIT SCOPE.
- **Canonical triage view: `br ready --label next-init`.** See HARD RULE above.
- Same-package-different-file = parallel-safe.
- Same-file conflict (2+ beads on one file) → serialize via per-bead dispatch.
- **SUBSUMED beads are common in late waves.** Pre-grep the target package; if the primary deliverable already exists, brief the implementer to close as SUBSUMED without a commit.

STANDING CONVENTIONS (full version: `.claude/implementer-protocol.md`).
- Bead body wins over docs; spec wins over bead body for normative content.
- Typed-alias deferral: real follow-up bead via `br create`, ID substituted into godoc BEFORE commit.
- gofmt-clean, lint clean, tests pass before commit.
- Worktree discipline: implementer commits in their worktree, never main.

REVIEWER TIER DISCIPLINE.
- MEDIUM = defect against THIS bead's acceptance criteria.
- Cross-cutting / future-bead / spec-doc concerns = MINOR or follow-up.

INLINE-AMEND CEILING.
Trivial single-line text fix, literal one-line code fix, mechanical multi-line refactor → orchestrator inline-amends, no fix-agent. Above ~3 mechanical edits in 1 file → spawn fix-agent on existing worktree.

MERGE DANCE — RUN FROM `/Users/gb/github/harmonik`.
Use `git -C /Users/gb/github/harmonik` for ALL git ops to avoid bash-cwd drift inside worktrees.

    cd /Users/gb/github/harmonik
    for id in <agent-id-1> <agent-id-2>; do
      WTPATH="/Users/gb/github/harmonik/.claude/worktrees/agent-$id"
      BRANCH="worktree-agent-$id"
      git -C "$WTPATH" rebase main
      git -C /Users/gb/github/harmonik merge --ff-only "$BRANCH"
      git -C /Users/gb/github/harmonik worktree remove --force --force "$WTPATH"
      git -C /Users/gb/github/harmonik branch -d "$BRANCH"
    done
    git -C /Users/gb/github/harmonik push origin main

FALLBACK — cherry-pick when worktree dir is gone. To merge a leftover branch whose worktree was already removed, use `git -C /Users/gb/github/harmonik cherry-pick <sha>` against the branch tip.

REBASE-SKIP for duplicate-bead commits. When a long-running OLD-protocol implementer's branch carries a commit for a bead ALREADY closed by a newer-protocol dispatch in the same session, `git rebase main` will hit add/add or content conflicts. Use `git rebase --skip`. Cross-package signature mismatches DO NOT surface as text conflicts; always run `go vet ./...` after the last merge of a session.

`br close` failures from `blocks` deps → flip to `related`:
    br dep remove <id> <other> ; br dep add <id> <other> --type related ; br close <id> -r "..."

`br update -d` does NOT exist — use `--description` or `--body`. `--notes` adds without overwriting. `br update --defer ""` clears `defer_until` (see L-017). `br create` flags: `-p` priority, `--labels "a,b,c"`, `--parent <id>`.

REBASE-CONFLICT ON `go.mod` — DO NOT USE `git reset --soft main`. Use `git rebase -i main` to drop the offending hunk, or `--strategy-option theirs` for go.mod/go.sum specifically.

`bv` USAGE. `bv --robot-triage` is a graph-aware triage sidecar. Reads only — `br` writes. Bare `bv` launches an interactive TUI that blocks the session; use only `--robot-*` flags. **Caveat:** bv does NOT respect `post-mvh` or `status=deferred`; always scope with `--label next-init` (or another initiative label).

CONTEXT BUDGET (orchestrator). ~700 k effective. At ~500 k, finish in-flight stream cleanly, write fresh HANDOFF, stop. v33→v34 spanned this session at ~22% used at handoff — plenty of headroom.

<!-- END DIRECTIVES -->

# Where we are (v34, 2026-05-12)

Main at `4a7a58f` (pushed). **Nothing in flight.** Stream is fully drained.

## What MVH means as of today

MVH ("minimum viable harmonik") is **shipped and runnable as a foreground binary**. The loop runs end-to-end against fixture-stub and twin handlers under `go test -race`:

- `cmd/harmonik/main.go` → `daemon.Start` → poll `br ready` → claim a bead via `br update --claim` → resolve `workflow:<mode>` label (4-tier precedence in `moderesolve.go`) → create worktree → dispatch handler subprocess → read verdict from `review.iter-N.json` (if review-loop mode) → close-or-reopen bead → loop.
- **Concurrent runs work.** `cfg.MaxConcurrent` defaults to 1 (MVH-preserving) but can be set higher via `--max-concurrent` flag. Goroutine-per-active-bead, RunRegistry-tracked. N=10 throughput test passed at MaxConcurrent=4 with ~5–8× speedup over sequential baseline.

## Two big initiatives landed this session

**1. Workflow-modes corpus (v32→v33; epic hk-7om2q + 32 children, CLOSED).** The three dispatch modes — `single`, `review-loop`, `dot` — are defined in `specs/...` (per commit `caf4b57`). `single` and `review-loop` are implemented; `dot` is the spec slot for future workflow-composition. The `review-loop` mode IS the "ralph loop" — a per-bead implementer↔reviewer iteration with 5 termination paths (APPROVE, REQUEST_CHANGES looped, BLOCK, cap-hit at iter=3, no-progress via diff-hash). Until DOT lands, `review-loop` is the stand-in.

**2. Post-MVH parallelism epic (v34; epic `hk-e61c3` + 6 row beads + `hk-p4xbw` + `hk-wb0ci`, all CLOSED).** Lifted MaxConcurrent from 1 to N. Six commits on main (all under `go test -race`):

- `1bb7c34` row 6 — `MaxConcurrent` Config field + CLI flag
- `92ee855` row 10 — `eventbus.Filter(path, runID)` helper
- `7afb9eb` row 5 — goroutine-per-bead work loop (the gate row)
- `c8e49c9` `hk-wb0ci` — HC-004 test needle updated (other 4 in the hygiene sweep were already passing)
- `b628e97` row 7 — N=2 smoke test
- `c2ce491` `hk-p4xbw` — pre-claim `ShowBead` guard; `FINDING_DOUBLE_DISPATCH` sentinel REMOVED
- `8f3b9c7` row 9 — claim semaphore bounded at `MaxConcurrent`
- `4a7a58f` row 11 — 10-bead throughput test (passed at MaxConcurrent=4)

Roadmap: `POST_MVH_PARALLELISM_ROADMAP.md` at repo root (125 lines, audited against `60b6024`). All 11 rows of the roadmap are now landed.

# Where we want to go (high level)

The user wants to **start using hk to do real work** (dogfood). The user's stated long-term sequence: **ralph loop → parallel runs → DOT workflow format → ...**. Ralph loop ✓ shipped. Parallel runs ✓ shipped. DOT is next on the headline list, BUT —

## TOP PRIORITY THIS COMING SESSION: dogfood smoke test

**Epic `hk-1n0cw` — labelled `next-init`.** The reason: the loop runs end-to-end against stubs and the twin handler. It has NOT been verified against a real `claude` CLI subprocess on a real bead. Likely failure surfaces (per the loop-investigation report run this session):

1. **Prompt/instruction injection.** What hk feeds to the Claude subprocess. Is the bead body passed? System/role prompt? The verdict-format requirement?
2. **Authentication / session resumption.** `ClaudeCodeAdapter.RotateAccount` is a stub. `ParseClaudeSessionID` exists (T-WM-018) but real session reuse untested.
3. **Worktree CWD / env.** Does the subprocess inherit the right CWD?
4. **Adapter output parsing.** `DetectReady` / `DetectRateLimit` / `CleanExitSequence` rely on text patterns the real Claude CLI may not emit.

Before any further investment in DOT, multi-bead dogfooding, or anything else, smoke the real-Claude path. The user explicitly chose this over diving into DOT.

### Smoke epic children — DISPATCH ORDER

| Bead | What | Sub-agent shape |
|---|---|---|
| `hk-1n0cw.1` (P1, ready) | **TRACE.** Read-only: document what hk feeds to the Claude subprocess (argv, env, CWD, stdin, expected verdict file). Output: `docs/dogfood-smoke-trace.md`. | Explore subagent (read-only). |
| `hk-1n0cw.2` (P1, blocked on .1) | **RUN.** Set up an isolated `.beads/` in a `mktemp -d` scratch dir, one disposable bead (adds a marker line to a file), build `/tmp/hk`, run it, capture observations to `docs/dogfood-smoke-run-<date>.md`. File ALL gaps as follow-up beads under `hk-1n0cw`. | Implementer subagent (worktree isolation, model=sonnet, effort=high). |

The user's standing instruction for the smoke: **instruct the next session agent to dispatch sub-agents for the actual smoke work — don't do it in the orchestrator's main context.** Dispatch shape per the directives block above.

### After smoke is green: DOT workflow format

DOT is the third dispatch mode in `specs/` (defined in commit `caf4b57` as one of three) but the **`dot` mode itself is empty** — no implementation, no roadmap, no decomposed beads. Unlike parallelism (which had a 125-line roadmap ready to go), DOT will need a planning pass.

User's preference asked this session: either
- **A. Kerf spec pass** — `kerf new dot-workflow-mode --jig spec` (full structured process: problem-space → decomposition → research → design → spec draft → integration → tasks). The project rule is spec-first; this is the right move for something this size.
- **B. Roadmap write-up first** (like POST_MVH_PARALLELISM_ROADMAP.md), bead-decompose from it.

**No decision made yet.** Ask the user once smoke is green. Don't pre-commit to A or B.

### Beyond DOT (locked decisions, not next-up)

- **Daemonization.** Detached process, pidfile, socket, JSON-RPC operator-control. Deferred per locked decision 2026-05-08. Don't open until DOT lands.
- **Multi-project.** One daemon per project is locked; multi-project is post-daemonization.
- **Reconciliation.** Post-MVH per RC spec; not blocking anything currently.

# Open follow-up beads (not next-init, but on the radar)

- **`hk-a6nob` (P3, parallelism follow-up).** `emitRunStarted` should use `EmitWithRunID` so the envelope `run_id` is populated; would let `eventbus.Filter` match `run_started` events directly. Currently the row-11 throughput test extracts run_ids from the payload as a workaround.

# Pre-existing flakes — known, not blocking

- `TestT4_ReopenThenRedispatch` — git-I/O contention; passes in isolation, intermittent under high parallel test load.
- `TestWorkLoop_FailedHandlerReopensBead` — same root cause.
- `TestT5_RedactionHC031ByFieldName` in `internal/t5probe` — surfaced by `hk-wb0ci`, out of that bead's scope.

Possible follow-up: a small bead for git-I/O test-isolation hardening (use `t.Setenv` for `TMPDIR`? or serialize git-touching tests via a build tag?). NOT urgent.

# About the foreground-binary question

The user asked: "if the process isn't running as a daemon is that a problem?" Answer: **no, not for dogfooding.** Foreground binary works fine — keep the terminal alive (use `tmux` / `screen` so SSH drops don't kill it), operator control is via signals (SIGINT/SIGTERM stop, SIGSTOP/SIGCONT pause). Daemonization unlocks "detach + RPC + cross-invocation persistence" but none of those block real-world use today.

# Files to open first (next session)

1. **This file (HANDOFF.md)** — you're reading it.
2. `docs/orchestration-learnings.md` — friction log.
3. `POST_MVH_PARALLELISM_ROADMAP.md` (repo root) — for context on what just shipped.
4. `br show hk-1n0cw` and `br show hk-1n0cw.1` — the top-priority bead bodies.
5. `internal/daemon/workloop.go` and `internal/daemon/reviewloop.go` — the loop the smoke will exercise.
6. `internal/handler/adapter_claudecode.go` — the Claude CLI adapter the smoke will hit.

# Blocking question for the user

None. The smoke epic is filed; dispatch `hk-1n0cw.1` first. If the smoke surfaces gaps, file follow-ups under `hk-1n0cw` and triage them before opening DOT.
