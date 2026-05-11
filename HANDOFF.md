<!-- PP-TRIAL:v2 2026-05-11 main — v27, 12 commits pushed (a46d23e..ed5c34b), MVH-root cohort largely drained, specaudit sensor pattern dominant -->

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

Main at `ed5c34b`, pushed to origin/main. **Open=149, Closed=833, Ready=19 (~7 MVH tasks + 11 epics + 1 admin), Deferred=40.** No active worktrees, no in-flight implementers. Working tree clean.

# What this session did — drained the MVH-root cohort

Picked up v26's 5 MVH roots and stream-dispatched implementers; cascade unblocks fed the next layer. **17 beads closed, 12 commits pushed.** Headline finding: **most MVH-root beads (zs0.8, zs0.25, sx9r.22, 8mup.32, i0tw.38) were SUBSUMED** — the spec text + Go runtime + closed-sibling sensors had already landed in prior sessions. The remaining roots needed only the corpus-search sensor that pins spec-text to runtime.

## Commits landed (a46d23e..ed5c34b)

| SHA | Bead | What |
|---|---|---|
| `218955a` | hk-zs0.17 | AR-016 spec-corpus sensor — subsystem is a Go package |
| `fd09379` | hk-i0tw.15 | SH-015 5-step ordered teardown sensor (325 LOC) |
| `20a5cfd` | hk-zs0.26 | AR-025 regex byte-identity sensor + exported `AgentTypeRegexPattern` const |
| `10e821d` | hk-1hoxo | `core.RateLimitSource` typed alias (open-vocab regex pattern; not closed enum) |
| `cb84707` | hk-i0tw.28 | SH-026 timeout-verdict + cancel-chain sensor (318 LOC) |
| `342d6f5` | (merge of cb84707) | |
| `f1f3c45` | hk-sx9r.20 | ON-016 `required_migration_release` payload field + 4 sensor tests |
| `73f5002` | hk-i0tw.33 | SH-031 sequential-scenarios sensor (265 LOC) |
| `dd58966` | hk-zs0.1 | AR-052 spec-category front-matter sensor (276 LOC) + added `spec-category: foundation-cross-cutting` to control-points/event-model/execution-model/handler-contract |
| `0cb73fc` | hk-i0tw.25 | SH-023 assertion-failed verdict sensor (307 LOC) |
| `7ac15f1` | hk-e1kdc | event-model §8.7.4 table — `required_migration_release?` (orchestrator inline) |
| `4893b96` | hk-i0tw.35 | SH-033 signal-handling sensor (346 LOC) |
| `ed5c34b` | hk-zs0.28 | AR-027 four-surface byte-identity sensor + patched missing `agent_type` field in execution-model.md Node RECORD |

## SUBSUMED closures (no commits)

- `hk-zs0.8` (AR-006) — mechanism-tag definition; sensor not lint-implementable per AR-INV-001 reviewer-enforced model; covered by `internal/core/modetag.go`.
- `hk-zs0.25` (AR-024) — agent-type conformance class; `core.AgentType` typed string + sidecar fields already landed (8mwo.63/.38). Four-surface sensor is AR-027's scope (zs0.28).
- `hk-sx9r.22` (ON-018) — N-1 compat window declaration fully embodied in `internal/operatornfr/schemacompatwindow_test.go` (hk-sx9r.78). Sensor bead is hk-sx9r.69 (open, downstream).
- `hk-8mup.32` (PL-020) — composition-root sensor already landed in commit `e7e13d6` (`internal/specaudit/pl020_composition_root_test.go`).
- `hk-i0tw.38` (SH-INV-002) — `shinv002_workspace_reset_test.go` already on main as commit `1d95451`; prior session implemented without closing the bead.

## Audit closure

- `hk-wcstp` — cascade-closure spot-check (15 beads across `hk-872/sx9r/i0tw/8mup` namespaces): **14/15 CONFIRMED**, 1 WEAK (hk-sx9r.6 commit-hash gate — coverage embedded in operatornfr handler, no dedicated test). No reopens. Cohort closures legitimate despite bare `done` rationale.

## Pattern observations (candidate L-018 / product-input)

1. **MVH-root beads were declarations; sensors landed via siblings.** 5 of the 9 root-dispatch attempts returned SUBSUMED. The deliverable is reliably (a) confirm the spec text exists, (b) confirm the runtime expression exists, (c) point at the closed-sibling sensor that already enforces it. If `kerf finalize` had emitted explicit `derives-from` edges instead of `blocks`, half this session's first-wave dispatches could have been skipped at audit time.
2. **Corpus-search sensors are the dominant deliverable shape.** 9 of 12 commits land an `internal/specaudit/{ar,sh,on}###_*_test.go` file with 8–13 sub-checks (heading present, normative tokens present, cross-refs cited, Tags-line present). The pattern is mechanical enough that a generator (`kerf scaffold-sensor <req-id>`) is feasible.
3. **`post-mvh` label filter MUST be checked every refill.** `br ready` surfaces `post-mvh` beads as soon as their blockers close — the label is the only thing keeping them out of dispatch. Four times this session `br ready` listed `hk-8i31.27`, `hk-8mwo.14`, `hk-8mwo.33`, `hk-sx9r.23` — all correctly post-mvh, all silently skipped by manual label check.
4. **Cross-spec spec-text patches surface during sensor work.** zs0.28 found `agent_type` missing from execution-model.md Node RECORD. zs0.1 found 4 specs missing the AR-052 `spec-category` front-matter. These were inline-fixed in the same commit. Without the sensor, both gaps would have stayed silent until reviewer-enforced lint caught them.

# Next session — direction

The MVH-root cohort is **drained.** What remains for MVH is **11 task-beads + 11 epic rollups + `hk-ahvq` Phase-0-completion meta-task.** Of the 11 task-beads, **3 are dispatchable right now**; the other 8 are chained behind them (almost entirely behind `hk-zs0.14`).

## Dispatchable RIGHT NOW (3)

| Bead | Spec | Notes |
|---|---|---|
| `hk-zs0.14` | architecture — AR-013 | Subsystem envelope declaration (8 elements). **Load-bearing root**: closing this unblocks 4 chained beads. Same corpus-search-sensor shape as v27. |
| `hk-hqwn.59.22` | event-model — §8.3.2 | Event row: `agent_started`. Standalone; payload schema work. |
| `hk-8mup.33` | process-lifecycle — PL-020a | Cross-subsystem registries reside in the composition root. Standalone; likely SUBSUMED candidate (sibling `pl020_composition_root_test.go` already covers PL-020/PL-020a per v27 hk-8mup.32 SUBSUMED finding). |

## Chained MVH work (8) — unlocks as the 3 close

| Bead | Blocked by | Title |
|---|---|---|
| `hk-zs0.2` | hk-zs0.14 | Envelope declaration section slot (§4.a) with reserved `<PREFIX>-ENV-NNN` |
| `hk-8mup.1` | hk-zs0.2 | Subsystem envelope declaration (daemon-core / `internal/daemon`) |
| `hk-8mwo.1` | hk-zs0.2 | Subsystem envelope declaration (S06 / workspace-model) |
| `hk-8i31.39` | hk-zs0.14 | Per-handler redaction patterns |
| `hk-8i31.37` | hk-8i31.39 | Redaction registry middleware |
| `hk-hqwn.45` | hk-8i31.37 | Redaction registry applied before event emission |
| `hk-hqwn.19` | hk-hqwn.45 | Dispatch semantics |
| `hk-hqwn.7` | hk-zs0.14 | `source_subsystem` is layout-open |

Dep-graph shape: `zs0.14` is the root of two chains (envelope and redaction); a third chain runs purely inside hqwn. **Recommended dispatch:** spawn the 3 ready beads in parallel; on `zs0.14` close, `zs0.2` + `8i31.39` + `hqwn.7` become ready (3 refills); on `zs0.2` close, `8mup.1` + `8mwo.1` become ready; the redaction chain (`8i31.39 → 8i31.37 → hqwn.45 → hqwn.19`) is sequential. Net: ~3-4 stream cycles drains the MVH task surface, half of which will likely close SUBSUMED (v27 pattern was 5 of 9).

## After MVH tasks close

- **10 spec-epic rollups** (`hk-872`, `hk-b3f`, `hk-hqwn`, `hk-8i31`, `hk-a8bg`, `hk-8mwo`, `hk-8mup`, `hk-sx9r`, `hk-63oh`, `hk-i0tw`) need manual close at MVH-cut time. Beads' parent-child auto-rollup is disabled in this corpus per L-011 conversion. Cleanup, not new work.
- **`hk-ahvq` "Phase 0 completion — load remaining pilots and exit to code phase"** is a substantive meta-task, not a rollup. Belongs in next session's first triage.

## Post-mvh filter is the real ergonomic gap (NOT a `br ready` bug — protocol note)

`br ready` itself is sound; the v27 friction was that `post-mvh` labeling is checked manually per bead. Of 8 non-epic entries in `br ready --limit 0`, 5 are `post-mvh`-labeled and skipped. Candidate small fix: a `br ready --exclude-label post-mvh` flag, or a session helper script. Until then: always run the per-bead label check before dispatching. Beware: my v27 in-session attempts to write a one-liner filter using `br ready --json` failed because `br ready --json` omits the `labels` field — only `br show` or `br list` carry labels. Don't trust scripted filters without re-checking against `br show`.

**Anti-recommendation:** do NOT spend cycles trying to find more MVH-root work in the existing corpus. The roots are drained. After the 11 tasks close, MVH throughput depends on either (a) reviewing the `hk-ahvq` Phase-0 pilot-ingestion path, or (b) starting on subsystem-skeleton work (`internal/daemon/` composition root, redaction registry implementation) that doesn't have a dispatchable bead today and may need a new kerf work.

# Files to open first

- This file (the directives — v27 BEAD PICKING block has updated post-mvh language).
- `docs/orchestration-learnings.md` — no new L-### added this session; **two candidate L-### entries** worth writing if next session reproduces the pattern: (a) "MVH-root beads are largely SUBSUMED-by-prior-sessions; product input = `kerf finalize` derives-from edges"; (b) "`br ready --json` omits labels — scripted post-mvh filters silently no-op unless they cross-check via `br show`."
- `STATUS.md` — high-level state (unchanged this session).
- `.claude/implementer-protocol.md` — implementer rules (unchanged this session, but always read).

# Quick references

- MVH-root cohort (5 beads) DRAINED — 4 SUBSUMED + 1 sensor (`218955a`); plus 8 sequential cascade-unblocked beads dispatched in same session.
- 12 commits on main since session start, pushed clean.
- ~130 `post-mvh` beads remain correctly parked — DO NOT dispatch unless explicitly dragged forward.
- 40 deferred beads — legitimate cognition holds; do not un-defer without explicit audit (L-017 protocol).
- `hk-e1kdc` filed and closed this session — only follow-up bead created.
- No reviewer dispatches this session — sonnet implementer self-judgment + protocol discipline held; no rework needed.
- **MVH remaining surface: 11 task-beads (3 ready, 8 chained) + `hk-ahvq` Phase-0 meta-task + 10 hollow spec-epic rollups for end-of-MVH cleanup.** Half of the 11 likely close SUBSUMED per v27 pattern.
