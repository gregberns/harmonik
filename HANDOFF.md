<!-- PP-TRIAL:v2 2026-05-12 main — v30. MVH ships end-to-end (production `./harmonik` drives ready beads to closed). Next-session focus per user: parallelism so multiple tasks can run concurrently. Read POST_MVH_PARALLELISM_ROADMAP.md first. -->

<!-- ORCHESTRATION DIRECTIVES — DO NOT EDIT EXCEPT BY EXPLICIT USER REQUEST. Loaded every /session-resume. -->

ROLE. You are the orchestrator. Delegate substantively. Keep the main thread minimal.

LEARNING LOG (READ ON EVERY RESUME). `docs/orchestration-learnings.md` — friction-and-fix log. Read it on `/session-resume`. Append new entries when you observe friction. Promote durable rules to this directives block or `.claude/implementer-protocol.md`.

STREAM-NOT-WAVES (HARD RULE). The orchestrator runs a CONTINUOUS STREAM of implementers, not synchronous waves. **On every implementer-completion notification, do exactly two things, in order:**

  1. Merge the returning implementer (rebase → ff-merge OR cherry-pick fallback → worktree teardown → bead-close-if-needed).
  2. Inspect dispatchable depth (`br ready --limit 0` minus in-flight claims minus excluded labels). If depth ≥ 1 and floor not met (target ≥10 active), spawn ONE replacement implementer. If depth = 0, note "queue draining" and stop spawning.

Per-return acknowledgment is ≤2 lines ("merged X, dispatched Y" OR "merged X, queue draining"). Full session summary lives at `/session-handoff` time, not inline.

TRUST `br ready` BUT VERIFY (HARD RULE — three checks, **L-017 added a third**).
`br ready` is NOT authoritative for "the corpus is drained." Three orthogonal filters can hide dispatchable work; check all three:

  1. **Stale `blocked_issues_cache` (L-011).** Cross-check `br stats` Open vs Ready — if Open ≫ Ready, suspect dep-model gridlock not corpus drain. Inspect blocker distribution: `br blocked --limit 0 --json | python3 -c "import json,sys;from collections import Counter;d=json.load(sys.stdin);d=d.get('issues',d) if isinstance(d,dict) else d;c=Counter();[c.update(b.get('blocked_by',[])) for b in d];print(c.most_common(20))"`. Recovery: `br doctor --repair` rebuilds the cache.
  2. **Parent-child gridlock (L-011).** If a single epic appears as the blocker for many beads: `sqlite3 .beads/beads.db "UPDATE dependencies SET type='related' WHERE type='parent-child'"`, wipe `blocked_issues_cache`, `br doctor --repair`. Backup `.beads/beads.db` first. **Trade-off**: `br epic status` reports 0/0 children after the conversion — accept for MVH.
  3. **Stale `defer_until` (L-017 — NEW).** A bead with `status=open` can still carry `defer_until: <future-date>` from a prior `br update --defer` and silently filter out of `br ready`. Detect via JSON: `br list --status open --limit 0 --json | python3 -c "import json,sys;d=json.load(sys.stdin)['issues'];print([(b['id'],b['defer_until']) for b in d if b.get('defer_until')])"`. Clear via `br update <id> --defer ""`.

DON'T ASK — EXECUTE.
On `/session-resume` with no hard blocker, EXECUTE — don't close the say-back with an A/B question (user's standing directive; memory `feedback_resume_continue_directive`). Sub-agents inherit via `.claude/implementer-protocol.md` — they make judgment calls and document reasoning in commit body. Orchestrator on genuine ambiguity: decide and document; explanation goes in next handoff or ≤2-line ack.

IMPLEMENTER LIFECYCLE — ENFORCED IN PROTOCOL.
`.claude/implementer-protocol.md` (updated 2026-05-10) is authoritative. Key rules: (a) implementer CLOSES OWN BEADS via `br close` after each commit, (b) implementer DOES THE BEADS NAMED IN ITS BRIEF AND EXITS — no free-claiming from `br ready`; orchestrator owns refill, (c) implementer DOES NOT ASK questions back. Brief template: appendix of `.claude/implementer-protocol.md`. Briefs MUST NOT include "after close, continue claiming X" lines.

DISPATCH SHAPE.
- Implementers: `model=sonnet`, `effort=high`, `isolation=worktree`, `run_in_background=true`.
- Reviewers: `model=sonnet`, `effort=high`, no isolation.
- Briefs ≤15 lines: see brief-template appendix in `.claude/implementer-protocol.md`. **Do NOT paraphrase the bead body.** Implementer fetches via `br show`.

PRE-FLIGHT (orchestrator, ≤3 reads per dispatch).
- Bead body via `br show <id> --format json`.
- The cited spec section.
- ONE canonical sibling for pattern conventions.
- Pre-dispatch grep for the bead's primary type name in the target package — if it exists, the bead may be SUBSUMED (sibling-pointer in brief; see L-008).

BEAD PICKING — POST-AUDIT SCOPE (v27).
- Dispatchable depth: **`br ready --limit 0`** (19 entries as of session-end 2026-05-11 v27: ~7 MVH tasks + 11 epics + 1 admin). Filter rule: `br ready --limit 0 | grep -v "\[epic\]"`.
- **`post-mvh` label exclusion is HARD.** Always check labels before dispatching: `br show <id> --format json | python3 -c "..."`. The audit (v26 → v27) explicitly dragged 4 beads forward as MVH (sx9r.22, 8mup.32, hqwn.45, 8i31.37, 8i31.39 + the redaction registry set). Anything ELSE labeled `post-mvh` stays parked; ~130 beads are correctly post-mvh by design.
- Same-package-different-file = parallel-safe. Confirmed v27: 5 concurrent `internal/specaudit/sh###_*` and `ar###_*` sensors landed without collision.
- Same-file conflict (3+ beads on one file) → ONE implementer with sequential commits.

STANDING CONVENTIONS (full version: `.claude/implementer-protocol.md`).
- Bead body wins over docs; spec wins over bead body for normative content. Surface discrepancies via L-009 channel.
- Typed-alias deferral: real follow-up bead via `br create`, ID substituted into godoc BEFORE commit.
- gofmt-clean, lint clean, tests pass before commit.
- Worktree discipline: implementer commits in their worktree, never main.
- Specaudit watchdog: every new normative requirement in `specs/*.md` MUST carry `Tags: mechanism` or `Tags: cognition` within 30 lines of its heading. Failures surface in `internal/specaudit/ar005_tags_test.go`.

REVIEWER TIER DISCIPLINE.
- MEDIUM = defect against THIS bead's acceptance criteria.
- Cross-cutting / future-bead / spec-doc concerns = MINOR or follow-up.

INLINE-AMEND CEILING.
Trivial single-line text fix, literal one-line code fix, mechanical multi-line refactor → orchestrator inline-amends, no fix-agent. Above ~3 mechanical edits in 1 file → spawn fix-agent on existing worktree. Validated v27: 1-row event-model table addition for hk-e1kdc landed inline as commit `7ac15f1`.

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

FALLBACK — cherry-pick when ff-merge fails. When `git merge --ff-only` reports "Already up to date" after rebase but the worktree clearly has a new commit, fall back to `git -C /Users/gb/github/harmonik cherry-pick <sha>`. Do NOT use `git reset --soft main` from a worktree.

REBASE-SKIP for duplicate-bead commits. When a long-running OLD-protocol implementer's branch carries a commit for a bead ALREADY closed by a newer-protocol dispatch in the same session, `git rebase main` will hit add/add or content conflicts. Use `git rebase --skip`. Cross-package signature mismatches DO NOT surface as text conflicts; always run `go build ./...` after the last merge of a session and inline-fix.

**WORKTREE TEARDOWN DOES NOT KILL THE AGENT (L-016).** `git worktree remove --force --force` does NOT terminate an active sub-agent. The agent can recreate the worktree and continue making bash calls; `br close` writes hit SQLite directly. Mitigated by L-015 (implementers do scope and exit, no free-claim). At session end, before writing HANDOFF, re-check `br stats` Open count and `git worktree list` ONCE MORE.

`br close` failures from `blocks` deps → flip to `related`:
    br dep remove <id> <other> ; br dep add <id> <other> --type related ; br close <id> -r "..."

`br update -d` does NOT exist — use `--description` or `--body`. `--notes` adds without overwriting. `br update --defer ""` clears `defer_until` (see L-017). `br create` flags: `-p` priority, `--labels "a,b,c"`, `--parent <id>`.

REBASE-CONFLICT ON `go.mod` — DO NOT USE `git reset --soft main`. Use `git rebase -i main` to drop the offending hunk, or `--strategy-option theirs` for go.mod/go.sum specifically.

CONTEXT BUDGET (orchestrator). ~700 k effective. At ~500 k, finish in-flight stream cleanly, write fresh HANDOFF, stop.

<!-- END DIRECTIVES -->

# State (v30, 2026-05-12)

Main at `8b53c27`, pushed. Open ~150, Closed ~890, Ready ~32. No active worktrees. Working tree clean.

**Running clean.** Production binary `./harmonik --project <git-init'd-dir>` drives ready beads end-to-end: claim → worktree → handler → close → JSONL with full event chain. Verified by manual sanity run (`/tmp/sanity-check2.sh`). Full Go test suite green (one rare flake in `TestRunSocketListener_BindsAndSetsMode` under heavy parallel load; mitigation already in the test).

# Where to go next (user's direction)

> "Start figuring out how to start using what we have to execute more tasks. Probably should focus on how we can have multiple tasks running first so we can push more work through."

User wants to **discuss** before implementing — don't just dispatch. Read `POST_MVH_PARALLELISM_ROADMAP.md` (in repo root, committed `8b00ae8`). It's an 11-row plan focused on goroutine-per-bead inside the one daemon process, with five concrete shared-state blockers cited at `file:line`. **Row #1 (run_id in envelope) already landed** (`87cd69a`, hk-n9f51). Open the doc and the next session can talk through: which rows first, whether the model still holds now that MVH has shipped, and what "more tasks running" means concretely to the user (parallel goroutines? multiple foreground binaries? something else?).

Don't start dispatching parallelism implementers without that conversation — the user wants the design call, not a fait accompli.

# This session in one paragraph

Drained MVH_ROADMAP rows 10 (work loop) + 11 (smoke test). Landed test infra (P1–P4 — fixture seed, fail/hang twins, reset script). Ran the six-tester exploratory wave per `EXPLORATORY_TESTING_PLAN.md`; testers filed beads directly per user override; about 10 fix-implementers cleared the findings in batches. The keystone bug was `brcli.Adapter.RunWithTimeout` not setting `cmd.Dir` — production binary appeared to run but silently failed every Ready() poll. Fixed in `35b49c2`. Stderr logging added at every silent-`continue` site so future debugging is much faster. Two flaky tests fixed in passing. `POST_MVH_PARALLELISM_ROADMAP.md` authored. PARA-1 (run_id envelope) landed.

# Files to open first

- `POST_MVH_PARALLELISM_ROADMAP.md` — the roadmap to discuss.
- `MVH_ROADMAP.md` — historical context; rows 1–11 done.
- `EXPLORATORY_TESTING_PLAN.md` — reference for future testing waves.
- `internal/daemon/workloop.go` — current work loop (single-bead, polling, `runWorkLoop`).
- `internal/daemon/daemon.go` — composition root, where work loop launches.
- `test/exploratory/findings-T*.md` — what the first wave found.
- `.claude/implementer-protocol.md` — unchanged.

# Deferred follow-ups (not blocking)

- `hk-5wbzj` (cosmetic) — ClaimBead exclusion test needs production-SQLite-atomic coverage.
- `hk-4oyc2`, `hk-nmiww`, `hk-33tcf` (INFO from T6) — `br` body-size ceiling, `--format json` field name (`description` not `body`), work loop doesn't read bead body. Mostly observations, not bugs.
- `hk-5dade` (P4 by-spec) — work loop doesn't write lease-lock; reconciliation territory.
- Smoke test (`TestMVHSmoke`) showed one new flake right after PARA-1 landed (context-cancel race), then went green; watch on next CI run.

# Quick verification commands for the next session

    # Confirm MVH still works end-to-end:
    go build -o /tmp/h ./cmd/harmonik
    D=$(mktemp -d); cd "$D" && git init --initial-branch=main -q && git -c user.email=x -c user.name=x commit -q --allow-empty -m init && br init --prefix sx && br create "probe" -p 2
    cd / && /tmp/h --project "$D" &
    sleep 6 && cat "$D/.harmonik/events/events.jsonl" | jq -r .type
    # Expected: daemon_started, daemon_orphan_sweep_completed, run_started, run_completed (or run_failed if claude isn't on PATH)

# Blocking question for the user

When you say "multiple tasks running" — do you mean concurrent goroutines inside one `harmonik` process (the `POST_MVH_PARALLELISM_ROADMAP.md` model), or multiple OS processes (one per project, or N per machine)? The locked-in thesis is goroutines-inside-one-daemon-per-project, but worth confirming before we commit to row #2+ work.

