<!-- PP-TRIAL:v9 2026-05-08 main -->

<!-- ORCHESTRATION DIRECTIVES — DO NOT EDIT. Loaded every /session-resume. -->
Act as the orchestrator. Delegate substantively; keep main thread small.

**Implementer + reviewer agents read `.claude/implementer-protocol.md` for
standing conventions** (commit format, lint rules, helper-prefix discipline,
typed-alias-deferral, reporting format, gofmt-clean discipline, run-before-
commit checks, pre-flight reading order, **and worktree discipline — added
v8.5 after 4-of-5 wave-1 implementers committed directly to main**). Do NOT
duplicate that content into briefs. Briefs orchestrate; the protocol doc
instructs.

THROUGHPUT FLOOR. The bead body IS the work spec. Implementer briefs cap at
~30 lines: bead-id, worktree pointer, helper prefix, canonical-sibling
pointer (if useful), and any follow-up `br create` command for typed-alias-
deferral. **Never paraphrase the bead body into the brief.** The implementer
runs `br show <id> --format json` and the cited spec sections itself.

Active-work floor: 4 concurrent agents. Target: 6–8 across non-overlapping
packages. **Refill on review-dispatch, not review-return** — when you spawn
a reviewer, immediately spawn the next implementer if the ready queue has
non-conflicting work. Reviews and implementers run in parallel at the agent
level; the orchestrator should layer them.

Pre-flight investigation cap: orchestrator reads ≤3 files per dispatch (bead
body via `br show`, the cited spec section, ONE canonical sibling).

Spawn implementers (model: sonnet, effort: high) with `isolation: worktree`,
run_in_background. Reviewers same model/effort, no isolation.

**Same-package-different-file is parallel-safe.** Same-file conflict (3+
beads mutating one file) → combine into ONE implementer with sequential
commits.

**Sibling-overlap pre-check (NEW v9).** Before dispatching, scan in-flight
worktrees for *types* that the new bead may also define. The hk-i0tw.52 vs
hk-i0tw.31 collision (both defined CadenceFilter) cost a fix-agent round.
For wide-impact siblings (RECORDs, enums) consider serializing or naming
the canonical-source bead in the later brief.

Each implementation gets reviewed (model: sonnet, effort: high). Iterate up
to 4 rounds; stop when no BLOCKER/MAJOR/MEDIUM findings remain.

Reviewer brief MUST encode TIER DISCIPLINE: MEDIUM = defect against THIS
bead's acceptance criteria. Cross-cutting / future-bead / spec-doc concerns
= MINOR or follow-up note, not MEDIUM. Reviewer brief MUST also tell the
reviewer NOT to flag absence of `## Why / ## What / ## Spec alignment / ##
Test plan / ## Risk` sections or `Reviewed-By:` / `Review-Verdict:`
trailers — those are not used in this project.

Inline-amend by orchestrator (no fix-agent) for: trivial single-line text
fixes; literal one-line code fixes; **mechanical multi-line refactors
verifiable by reading**. **Read the worktree file BEFORE Edit** — Edit
requires Read in the same conversation, and bash `git commit` returns 0 on
"nothing to commit" so a missed Edit can sneak through a chained merge.
(Hit this on hk-b3f.87 fix; recovered with a post-merge follow-up commit.)
Re-review may be skipped after a metadata-only or pure-deletion fix.

Path-discrepancy resolution: bead body wins over docs. EXCEPTION — for spec
content (enum values, regex shapes, RECORD field-types), the spec wins per
CLAUDE.md ("specs are normative"); bead body gets the follow-up note.

Orchestrator authority: if reviewer flags MEDIUM but implementation clearly
meets bead acceptance, you may merge with a closure note explaining the
tier-override and (optionally) filing a follow-up bead.

Merge dance — RUN FROM THE MAIN REPO DIR, NOT THE WORKTREE. **Do NOT pipe
git commands through `head`/`tail`** in chains — `| head -1` swallows the
exit code and `&&` short-circuits don't catch upstream failures. Use raw
`git` calls in chains, or `bash -c 'set -o pipefail; ...'`. (Hit this on
hk-8mwo.64; recovered with checkout+rebase+ff-merge from main repo dir.)

For the actual merge dance use a for-loop pattern that worked well this
session:

    cd /Users/gb/github/harmonik
    for id in <agent-id-1> <agent-id-2> <agent-id-3>; do
      WTPATH=".claude/worktrees/agent-$id"
      BRANCH="worktree-agent-$id"
      git -C "$WTPATH" rebase main
      git merge --ff-only "$BRANCH"
      git worktree unlock "$WTPATH"
      git worktree remove --force "$WTPATH"
      git branch -d "$BRANCH"
    done
    git push origin main

Use `set -e` at the top of the bash script (NOT `| head` / `| tail`).

**Post-merge close failures from `blocks` deps are common.** When
`br close <id>` fails with "blocked by: <other-id>", convert the edge:

    br dep remove <id> <other-id> && br dep add <id> <other-id> --type related && br close <id> -r "..."

Use `br close <id> -r "..."` for the closure note. `br update <id> -c "..."`
does NOT exist.

Pipelined merges. When 3+ APPROVE reviews come back close together, chain
the merges in one bash invocation and push once for the whole chain.

Inline fix-iteration on existing worktrees (no new isolation): when a review
returns REQUEST-CHANGES with mechanical findings, spawn a fix-agent WITHOUT
`isolation: worktree` — point it at the existing worktree path. The agent
cd's into the existing branch, applies fixes, creates a NEW commit (never
amend pre-merge commits). Two-commit branches FF-merge cleanly.

Typed-alias deferral (recurring decision). When a record references a type
not yet in `core/`: default to `*string`/`string` placeholder + godoc citing
the spec section + `br create` follow-up bead for the typed wrapper. The
brief names the `br create` command; the implementer creates the bead,
captures its ID, and substitutes it into godoc before committing.

When ambiguity arises, spend real effort resolving without escalation. Bead
acceptance criteria is authoritative.

On resume: continue working unless the handoff body flags a real blocker.
Context budget on this 1M-context model is generous (~700k effective). When
you cross ~500k, finish in-flight batch cleanly, then write a fresh HANDOFF
and stop.
<!-- END DIRECTIVES -->

# Session Handoff

## State
Clean. Main at `869e43f`, pushed to origin. **17 beads landed and closed
this session** across 5 packages. New auto-registered skills: `agent-reviewer`,
`beads-cli`, `agent-config-reviewer`, `go-subsystem-add`. Protocol patched
at `3201f63` with mandatory worktree-discipline checks.

## What landed this session

Wave 1 (5 closed; 4 of 5 escaped worktree → protocol patched mid-session):
hk-872.26, hk-i0tw.14, hk-jhob.1, hk-8mwo.24, hk-sx9r.74.

Wave 2 (8 closed; all worktree-isolated correctly post-patch):
hk-jhob.2, hk-i0tw.17, hk-8mwo.64, hk-jhob.4, hk-872.33, hk-i0tw.31,
hk-jhob.3, hk-b3f.88.

Wave 3 (4 closed): hk-8mup.12, hk-8mup.4, hk-i0tw.52, hk-i0tw.18, hk-b3f.87.

Follow-up beads filed (typed-alias-deferral): hk-8mwo.72 (HandlerRef),
hk-i0tw.54 (SuiteID), hk-8mup.60 (ProjectHash + PGID).

## Process scars to internalize

1. **Worktree escape (4-of-5 wave 1).** Implementer agents committed
   directly to main from /Users/gb/github/harmonik because their
   prompts/CLAUDE.md mentioned that path and they cd'd into it. Patched at
   3201f63: protocol now mandates `pwd` + `git branch --show-current` +
   `git rev-parse --show-toplevel` before every commit, with branch MUST
   start `worktree-agent-`. Wave 2+ all behaved correctly. Keep the patch.

2. **Pipefail + `&&`-chains.** Bash chains with `git X | head -1` swallow
   the upstream exit code; `&&` doesn't short-circuit. Hit on hk-8mwo.64
   merge dance. Recovered. Avoid piping git commands in chained merge
   logic — emit raw output.

3. **Edit-then-commit fragility.** Inline-amend in a chained bash that
   does `git add && git commit` will silently no-op if the file wasn't
   actually edited (Edit tool requires prior Read in same conversation).
   `git commit` returns 0 on "nothing to commit" so set -e doesn't catch
   it. Hit on hk-b3f.87. Mitigate: read worktree file BEFORE Edit, or
   verify modification with `git status` between Edit and merge.

4. **`blocks` deps on bead close.** `hk-872.26→hk-872.25`,
   `hk-872.33→hk-872.25`, `hk-i0tw.18→hk-8mup.10/.2` all blocked close
   on closed-or-not-yet-implemented siblings. Convert pattern works. The
   directive block now describes it.

5. **Sibling type-symbol collision.** hk-i0tw.52 (SuiteResult) defined
   `CadenceFilter` while hk-i0tw.31 (cadence) defined the same type.
   When .31 merged first, .52 hit duplicate-symbol on rebase. The .52
   reviewer caught it with a rebase-preview before approving. Fixed by
   removing the .52 definition. Pre-dispatch sibling-overlap scan added
   to the directive block.

## Ready candidates (17 with no blockers, scope:bootstrap)

`br ready -l scope:bootstrap` snapshot:

- **brcli foundation:** hk-872.2 (Route all Beads I/O through br),
  hk-872.4 (Daemon to br direct, agents via skill).
- **Workspace pre-startup:** hk-8mwo.2 (Min git version pin ≥ 2.34),
  hk-8mwo.11 (Ref-safe bead-ID substitution via `git check-ref-format`),
  hk-8mwo.25 (Workspace lifecycle event emission obligations — WHEN).
- **Operator NFR:** hk-sx9r.73 (ON exit-code taxonomy §8 — 23-code
  authoritative table; sibling to closed hk-sx9r.74 fixture).
- **Twin/handler:** hk-ahvq.48.1 (cmd/harmonik-twin-claude/main.go
  scaffold + module entry-point), hk-8i31.26 (ErrSkillProvisioningFailed
  sub-sentinel), hk-8i31.50 (commit-hash check for in-repo binaries).
- **MVH composition root:** hk-b3f.89 (no-op PolicyEngine wired in
  composition root, resolves §A5).
- **Typed-alias follow-ups (this session's children):** hk-i0tw.54
  (SuiteID), hk-8mup.60 (ProjectHash + PGID), hk-8mwo.72 was created
  earlier this session for HandlerRef.
- **Hoists/cleanups:** hk-szv5 (EventExpectation.Type → core.EventType),
  hk-ido0 (FailureClass enum + hoist), hk-872.55 (core.EdgeKind extend
  for Beads dep-type surface), hk-pvcs.9 (P3, doc cleanup).

## Suggested first move

1. **Verify state**: `git status` clean; `git log --oneline -5` shows
   `869e43f fix(scenario): correct stale "five" count to "six"`.

2. **Open with a 5-bead wave**, all parallel-safe across packages:
   - `hk-8mwo.2` (git version pin) — internal/lifecycle/ or
     internal/workspace/, sensor-style
   - `hk-8mwo.11` (ref-safe bead-ID) — internal/workspace/, sibling to
     errors.go (just landed)
   - `hk-sx9r.73` (exit-code taxonomy 23-code table) — internal/operatornfr/
     or new package; sibling pattern from .74 fixture
   - `hk-i0tw.54` (SuiteID typed alias) — internal/core/, very small,
     unblocks future scenario RECORDs
   - `hk-jhob` (operational-skills meta-epic) — investigate if there's a
     concrete subtask, otherwise pick another typed-alias follow-up

3. **Watch for sibling-symbol collisions** in the wave (per Scar #5).

## Files to open first

1. `git log --oneline -25` — this session's commits
2. `.claude/implementer-protocol.md` — current standing rules; read
   §"Worktree discipline" carefully
3. `internal/scenario/` — many siblings landed here this session; the
   package now has ScenarioResult, AssertionResult, EventExpectation,
   FileSeed, GitSeedOp, WorkspacePredicate, EventLogPath/EventLogDir,
   FixtureRoot, CadenceTag/CadenceFilter, SuiteResult, SyntheticProjectRoot,
   CrashRecoveryFixture
4. `internal/lifecycle/` — Pidfile, ProbePidfileLock (PL-002a/PL-024 +
   linux/darwin variants), provenance (PL-006a)
5. `internal/workspace/` — workspace.go state machine + errors.go 12-class
   sentinel taxonomy

## Blocking question for user
None.
