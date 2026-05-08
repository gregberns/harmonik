<!-- PP-TRIAL:v10 2026-05-08 main (evening refresh after 18-bead session) -->

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

**Sibling-overlap pre-check (v9).** Before dispatching, scan in-flight
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
(Hit on hk-b3f.87; recovered with a post-merge follow-up commit.)
Re-review may be skipped after a metadata-only or pure-deletion fix.

**Inline-amend ceiling (v9.5 — observed this session).** Inline-amend
worked clean for ≤3 mechanical edits across 1 file (hk-872.4 single-line
delete; hk-872.42 three nolint-directive additions; hk-zs0.13 single-line
delete; hk-hqwn.27 dead-field + dead-return removal). Above that bar (e.g.
hk-872.30: 4 lint fixes including signature change at multiple call sites),
spawn a fix-agent — the verification cost climbs faster than the dispatch
cost.

Path-discrepancy resolution: bead body wins over docs. EXCEPTION — for spec
content (enum values, regex shapes, RECORD field-types, **enumerated
message-type sets**), the spec wins per CLAUDE.md ("specs are normative");
bead body gets the follow-up note. (Reinforced this session: hk-ahvq.48.2
fix-agent correctly OMITTED `agent_log` because HC-007 doesn't enumerate it,
even though the bead body listed it. Reviewer cleared.)

Orchestrator authority: if reviewer flags MEDIUM but implementation clearly
meets bead acceptance, you may merge with a closure note explaining the
tier-override and (optionally) filing a follow-up bead.

Merge dance — RUN FROM THE MAIN REPO DIR, NOT THE WORKTREE. **Do NOT pipe
git commands through `head`/`tail`** in chains — `| head -1` swallows the
exit code and `&&` short-circuits don't catch upstream failures. Use raw
`git` calls in chains, or `bash -c 'set -o pipefail; ...'`.

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

**Dep direction quirk (v9.5).** `br dep remove A B` removes only the A→B
edge; if children block the parent (B→A blocks), the remove call emits
`Dependency not found` — that's normal, just continue. The subsequent
`dep add A B --type related` + `close A` chain still resolves the close.
Hit this when closing hk-jhob (4 children all closed, parent epic blocked
by them).

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

`br create` flag syntax (verified this session): use `-p N` for priority NOT
`-P N`; use `--labels "a,b,c"` (comma-separated single arg) NOT repeated
`-l a -l b`; use `--parent <id>` not `-P`. Run `br create --help` if unsure.

`go test -C <dir>` quirk (v9.5). The `-C` flag must be the FIRST flag —
`go test -run X -C dir` fails. Use `cd <wt> && go test -run X` instead.

When ambiguity arises, spend real effort resolving without escalation. Bead
acceptance criteria is authoritative.

On resume: continue working unless the handoff body flags a real blocker.
Context budget on this 1M-context model is generous (~700k effective). When
you cross ~500k, finish in-flight batch cleanly, then write a fresh HANDOFF
and stop.
<!-- END DIRECTIVES -->

# Session Handoff

## State
Clean. Main at `05d7249`, pushed to origin. **18 beads landed this session**
(16 implementations + 2 closed-as-subsumed) across 4 packages
(brcli, core, lifecycle, specaudit, testhelpers). All my worktrees cleaned.
**13 follow-up beads filed**: hk-b3f.94 (SnapshotToken), .95 (Evidence),
.96 (VerifierMetrics), .97-.103 (7 deferred typed aliases — AxisTags,
ModeTag, PolicyRef, GateRef, FreedomProfileRef, BudgetRef, SubWorkflowRef),
hk-hqwn.70 (BusFlusher / EventBus.Flush), hk-872.57 (divergence_inconclusive
event-bus wiring).

## What landed this session

RECORD family (5): VerdictEvent (hk-b3f.93), Transition 15-field
(hk-b3f.77), Node 13-field (hk-b3f.73, with round-2 Timeout *int fix),
Workflow 11-field (hk-b3f.72), TransitionRecord marshal + schema_version
cross-check (hk-b3f.22).
Typed enums (2): ConsumerClass (hk-hqwn.65), OnPanic (hk-hqwn.66) — both
substituted string placeholders in subscription.go.
Specaudit family (5): AR-002 baseline-axis (hk-zs0.4, 5 violations pinned
to existing hk-zs0.59/.60), HC-014 channel closure (hk-8i31.17), EV-007
monotonic time (hk-hqwn.10), HC-029 agent_started no-env (hk-8i31.36),
BI-028 Beads-CLI skill launch-context (hk-872.35).
Lifecycle / process (2): EV-019a panic-flush bus extension (hk-hqwn.28
adds BusFlusher arg to RecoverWithLogFlush), HC-044 SpawnChildSysProcAttr
cross-platform Linux/darwin (hk-8i31.51).
brcli (1): BI-025a br exit-code → BrError classification on Result.BrErr
(hk-872.28).
testhelpers (1): BI-029..BI-032 crash-injection harness (hk-872.54,
8 tests, atomic write + startup scan + mock-br factory).
Closed-as-subsumed (2): hk-hqwn.40 (axes+durability — fully covered by
hk-hqwn.63 §8 taxonomy lint); hk-b3f.58 (backtracking — fully covered by
hk-b3f.77 Transition.Valid() EM-044 + 9 EM-044 tests).

## Process scars to internalize (NEW this session)

1. **Subsumption pre-check is high-leverage.** Two beads correctly
   identified as SUBSUMED this session (hk-hqwn.40, hk-b3f.58) by
   pre-flighting the implementer with explicit "if subsumed: print
   'SUBSUMED — cite which closed bead covers it' and do NOT commit"
   instructions. The "(coalesce)" tag in bead title is a strong subsumption
   hint; "all blocker deps closed and the work has structurally landed
   elsewhere" is the trigger. Worth the 60s implementer round-trip.

2. **Wire-format pattern: separate wire structs over annotating in-memory
   shape.** hk-b3f.22 followed `eventPatternJSON` precedent: defined
   `transitionWire` / `transitionWireState` / `transitionWireCommitRange`
   structs with snake_case tags rather than annotating `Transition`
   directly. Keeps in-memory shape clean and the wire-format constraints
   localized to the marshaler. Use this pattern for any future RECORD that
   needs JSON wire shape.

3. **Spec "Integer | None — positive seconds" → `*int`, NOT
   `*time.Duration`.** hk-b3f.73 round-1 review caught Node.Timeout
   emitting nanoseconds. Round-2 fix matched `Edge.TraversalCap *int`
   precedent. Default for any spec-typed numeric field with positive-
   integer wire shape: `*int` with godoc citing spec section, not Duration.

4. **Brief errors must surface, not be papered over.** hk-b3f.73 brief
   incorrectly claimed `AxisTags` and `ModeTag` already existed in core —
   they didn't. Implementer correctly deferred via follow-up beads
   (hk-b3f.97, .98) and reported the discrepancy in their summary. This
   is the right pattern: implementer flags brief vs reality, doesn't
   silently fabricate.

5. **Inline-amend ceiling held cleanly twice.** hk-872.28 (single-line
   godoc bead-id citation: "tracked by a follow-up bead" → "tracked by
   hk-872.57") and hk-b3f.22 (single test-key addition: `confidence` to
   requiredKeys). Both ≤3 mechanical edits in 1 file. Re-review skipped
   on the godoc fix; re-review skipped on the test-key fix because the
   `go test -run` confirmation was cheap.

6. **`.claire/` untracked dir is not mine.** Pre-existing directory at
   repo root with a `worktrees` subdir — likely typo'd `.claude/` from
   another session/tool. Don't clean it without asking.

## Suggested first move

1. **Verify state**: `git status` clean (only `.claire/` untracked);
   `git log --oneline -3` shows `05d7249 fix(core): add confidence to
   snake_case sensor requiredKeys (hk-b3f.22)` on top.

2. **Three leftover branches need investigation before delete**:
   `worktree-agent-a81fc0155348f648a`, `ab0d5be116be801d9`,
   `ae62858e28981ec72`. They have no worktree paths attached (orphaned
   from a prior session). Run `git log <branch> --oneline -5` on each
   to see if they have unmerged commits before `git branch -D`.

3. **Open with parallel-safe RECORD/Ref work** — there are still 7
   deferred typed aliases from this session waiting (hk-b3f.97-.103
   AxisTags, ModeTag, PolicyRef, GateRef, FreedomProfileRef, BudgetRef,
   SubWorkflowRef). All bundle naturally — bundle 2-3 per implementer in
   the same worktree (sequential commits per bead). Each is a string-
   alias enum or typed wrapper following the `outcomekind.go` /
   `actiondescriptor.go` precedent.

4. **Other open follow-ups** (more substantive):
   - hk-b3f.94 SnapshotToken — typed alias citing reconciliation/schemas.md
   - hk-b3f.95 Evidence — typed wrapper for transition.Evidence (currently
     `map[string]any`)
   - hk-b3f.96 VerifierMetrics — typed wrapper, same pattern
   - hk-hqwn.70 BusFlusher / EventBus.Flush — depends on EventBus actually
     existing; defer until event-bus subsystem exists
   - hk-872.57 divergence_inconclusive event emission — same as above,
     defer until event bus exists

5. **Cognition-tagged spec-fix beads remain undispatched** (handoff rule
   from prior sessions): hk-zs0.58 (RC-015 split), hk-zs0.59 (BI-025a/b/c
   idempotency=safe), hk-zs0.60 (RC-015 invalid axis vocabulary),
   hk-zs0.62 (investigator role), hk-hqwn.67 (§8 table per-row Axes:
   slot — substantive spec format change), hk-hqwn.68 (6 events lacking
   sibling citation). **Don't dispatch as implementer briefs.** Need user
   pair-on or kerf jig.

## Files to open first

1. `git log --oneline -22` — this session's commits.
2. `.claude/implementer-protocol.md` — standing rules; no structural
   change this session.
3. `internal/core/{verdict.go,verdictevent.go,consumerclass.go,onpanic.go,
   transition.go,transitionrecord.go,node.go,workflow.go,
   monotsmono_hqwn10_test.go}` — this session's 9 core landings.
4. `internal/specaudit/{zs04_baseline_axis_test.go,
   hc014_channel_closure_test.go,hc029_agent_started_no_env_test.go,
   bi028_skill_launch_context_test.go}` — 4 new audit-style binding tests
   (5 with last session's = audit family is now ~10 tests; the
   `expectedViolations` skip-list pattern is the canonical reuse target).
5. `internal/lifecycle/{panicrecovery.go,spawndaemonchild_*}` — EV-019a
   panic-flush extension and HC-044 SpawnChildSysProcAttr.
6. `internal/brcli/{adapter.go,classifyexitcode_test.go,timeout.go}` —
   Result.BrErr wire-up via BrErrorFromExitCode.
7. `internal/testhelpers/crashharness.go` — BI-029..BI-032 shared test
   infrastructure (8 B87254-prefixed exports).

## Blocking question for user

None. The 6 cognition-tagged spec-fix beads still need pair-on; everything
else (typed-alias follow-ups, RECORD/binding-test work) is freely
dispatchable.
