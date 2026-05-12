<!-- PP-TRIAL:v2 2026-05-12 main — v28, 8 commits pushed (e0b5240..60b6024). MVH-roadmap critical path (redaction chain) DRAINED but a runtime-vs-bookkeeping gap surfaced: composition root exists, daemon does nothing. Read MVH_REALITY_CHECK.md FIRST. -->

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

# State

Main at `8277522`, pushed to origin/main. **Open=139, Closed=847, Ready=10 (1 MVH-relevant: hk-8mup.63), Deferred=40.** No active worktrees. **Read `MVH_ROADMAP.md` first — it was rewritten in this session as the empirical source of truth and supersedes the bead corpus for "what needs to happen for MVH."** Detailed appendix at `MVH_REALITY_CHECK.md`. Working tree clean.

# What this session did — drained the MVH redaction-chain critical path AND surfaced a viability question

Picked up v27's 3 ready MVH beads + the 8-bead chain behind them. Stream-dispatched implementers and SUBSUMED-audits in parallel. **14 beads closed, 7 commits pushed, 3 new beads filed-and-closed in-session, 1 new follow-up bead filed (hk-8mup.63 — JSONL persistence).** The session's headline finding is not a closure list — it's that **closing the redaction chain did not produce a runnable tool.** The MVH composition root now exists and compiles, but the daemon's `Start()` returns nil after wiring the bus; nothing schedules, dispatches, or processes tasks. Exploratory testing was dispatched to characterize the actual runtime gap.

## Commits landed (e0b5240..60b6024)

| SHA | Bead | What |
|---|---|---|
| `e0b5240` | hk-zs0.14 | AR-013 eight-element envelope corpus-search sensor |
| `b621a63` | hk-hqwn.59.22 | §8.3.2 `agent_started` payload struct + RegisterPayload |
| `6ccc97f` | hk-8mup.61 (NEW) | Scaffold `internal/daemon/` composition-root package — `Start(cfg Config) error` entry point |
| `64e87d9` | hk-8mup.62 (NEW) | Concrete EventBus `busimpl` — Emit applies HC-031 redaction before JSONL stub + consumer dispatch |
| `5974c10` | hk-8i31.83 (NEW) | `RedactionRegistry` + `RedactionMiddleware` (HC-031+HC-032) + wired at `daemon.Start` |
| `ae19275` | hk-8i31.37 | HC-030 runtime sensor — registry middleware in Emit producer path |
| `924747c` | hk-hqwn.19 | EV-014a dispatch-order contract: sync blocks Emit, async/observer go off-path via `wg`, Drain awaits |
| `60b6024` | (docs) | `MVH_ROADMAP.md` committed |

## SUBSUMED closures (no commits)

- `hk-8mup.33` (PL-020a) — sibling `pl020_composition_root_test.go` already coalesced PL-020 + PL-020a coverage.
- `hk-zs0.2` (AR-053) — §4.0 of `specs/architecture.md` already declares the §4.a envelope slot + reserved `<PREFIX>-ENV-NNN` ID range.
- `hk-8mup.1` (PL-ENV-001) — `specs/process-lifecycle.md` §4.a already has the 8-element envelope.
- `hk-8mwo.1` (WM-ENV-001) — `specs/workspace-model.md` §4.a already has the 8-element envelope.
- `hk-8i31.39` (HC-032) — `internal/handlercontract/redaction_hc028_test.go` already has the per-handler pattern fixtures + compile-check tests (landed under hk-8i31.81). Runtime registration was deferred to hk-8i31.83 (filed + closed same session).
- `hk-hqwn.7` (EV-004) — `specs/event-model.md` §EV-004 already declares `source_subsystem` open-vocab; `internal/core/event.go:85` types it as plain `string`; `internal/core/event_ev001_test.go:59` asserts the open-vocab shape.
- `hk-hqwn.45` (EV-035) — `internal/eventbus/busimpl.go:86` already applies `registry.RedactionMiddleware` before consumer dispatch (just landed in `5974c10`).

## New beads filed this session

- `hk-8mup.61` (CLOSED `6ccc97f`) — daemon-package scaffold.
- `hk-8mup.62` (CLOSED `64e87d9`) — concrete EventBus.
- `hk-8i31.83` (CLOSED `5974c10`) — RedactionRegistry + middleware + wire.
- `hk-8mup.63` (OPEN) — Thread JSONL log path through `daemon.Config` + `busimpl.Emit` fsync per durability class. **This is the only known explicit MVH gap on the bus side**; the wider daemon-behavior gap is being characterized by the exploratory agent.

## Files added at repo root

- `MVH_ROADMAP.md` (committed `60b6024`) — corpus-bookkeeping view of remaining work, filtered by `not post-mvh`. **Caveat in header: this is NOT a demoability view.**
- `MVH_REALITY_CHECK.md` (pending — exploratory agent in flight) — empirical runtime-gap analysis: tried to `go run` the thing, traced one event end-to-end, audited scheduler/dispatch/workspace/handler surfaces. Will list concrete gaps with file:line and "absent" verdicts. Read this BEFORE deciding next-session direction.

# Next session — direction

**Do not assume the bead corpus is the source of truth for what's needed.** This session's exploratory dispatch was the explicit response to user pushback: "Are we seriously saying we have a viable working tool?" The answer was no, and the corpus's `post-mvh` filter likely hides work that is actually required for demoability (scheduler, handler dispatch, workspace creation, task ingestion, `cmd/harmonik` main, etc.). The MVH_REALITY_CHECK.md report from agent `a98c1daee8571f46c` should be the primary input for picking next moves.

## If MVH_REALITY_CHECK.md is present

1. Read it cover-to-cover. The "Verdict" paragraph is the headline; the "Concrete runtime gaps" table is the action list.
2. For each gap, decide: existing bead (likely mislabeled `post-mvh` — un-defer / relabel via `br update`), or new bead (file via `br create`).
3. Dispatch implementers in the same stream-not-waves shape as this session.

## If MVH_REALITY_CHECK.md is missing or partial

The exploratory agent may have failed or timed out. In that case:
- Check the agent's output file under `/private/tmp/claude-502/-Users-gb-github-harmonik/*/tasks/a98c1daee8571f46c.output` — but do NOT read the JSONL via the shell tool (per the dispatch-time warning). Use `tail` only if absolutely necessary, sparingly.
- Re-dispatch a fresh exploratory agent with the same prompt shape (see commit history for this session's Agent call).

## Bead-corpus ready surface (dispatch only if explicitly chosen)

`br ready --limit 0 | grep -v '[epic]'` shows ~10 ready beads as of session-end; all but `hk-8mup.63` (JSONL) are `post-mvh`-labeled. The hollow spec-epic rollups (`hk-872`, `hk-b3f`, `hk-hqwn`, `hk-8i31`, `hk-a8bg`, `hk-8mwo`, `hk-8mup`, `hk-sx9r`, `hk-63oh`, `hk-i0tw`) plus `hk-ahvq` are still parked — defer until cleanup pass.

## Pattern observations (candidate L-018 / L-019)

1. **Bead-corpus `post-mvh` labels are not a demoability filter.** The roadmap doc generated from the corpus claimed 4 beads remained on the MVH path. The exploratory dispatch was launched precisely to test that claim. Likely L-### entry: "Distinguish corpus-bookkeeping MVH from runtime-demoability MVH; the corpus is a slow-moving artifact of planner judgment and lags reality."
2. **Filing-then-closing-in-session worked smoothly.** 3 new beads (hk-8mup.61/.62, hk-8i31.83) were filed mid-session in response to a scoping report and all closed within ~30 minutes of file-time. The `br create --deps "blocks:<id>"` syntax composes a clean parent/child wiring that `br ready` honors. No friction.
3. **Two-bead-one-file collision was correctly serialized.** hk-8mup.62 (busimpl.go) and hk-8i31.83 (registry + busimpl.go wire) were dispatched as ONE sequential-commits agent rather than two parallel ones. Per directives. Held cleanly — no rebase conflicts, two clean commits in one worktree.
4. **Audit-only briefs continue to work.** 6 of 7 SUBSUMED closures this session were from explicit audit-only dispatches that resolved in <60s each. Pattern proven across two sessions now.

# Files to open first

- **`MVH_REALITY_CHECK.md`** (if landed) — primary input for next-session direction.
- This file (directives unchanged from v27).
- `MVH_ROADMAP.md` (committed `60b6024`) — secondary; the corpus-bookkeeping view.
- `docs/orchestration-learnings.md` — no new L-### added; two candidate entries documented above.
- `STATUS.md` — high-level state (unchanged this session).
- `.claude/implementer-protocol.md` — unchanged.

# Quick references

- MVH redaction chain DRAINED (hk-zs0.14 sensor + hk-8mup.61/.62/.61 daemon scaffolding + hk-8i31.83/.37 + hk-hqwn.19/.45).
- 7 commits on main since session start, plus 1 docs commit, pushed clean.
- 14 bead closures total: 7 commits + 7 SUBSUMED.
- Exploratory agent `a98c1daee8571f46c` may be in flight; check for MVH_REALITY_CHECK.md before assuming corpus-driven dispatch.
- The one explicit known runtime gap on the bus side is hk-8mup.63 (JSONL persistence).
- **Open question for product/user: is the bead corpus's `post-mvh` filter trustworthy as a runtime-MVH filter? Exploratory testing this session strongly suggests no.**
