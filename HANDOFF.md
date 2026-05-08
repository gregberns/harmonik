<!-- PP-TRIAL:v9 2026-05-08 main (afternoon refresh after 18-bead session) -->

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
Clean. Main at `7fcf8b0`, pushed to origin. **18 beads landed + 1
closed-as-subsumed = 19 closures this session** across 5 packages
(brcli, core, lifecycle, specaudit, testhelpers). All worktrees cleaned.
**10 follow-up beads filed**: hk-zs0.59, .60, .61, .62, hk-b3f.93,
hk-hqwn.65, .66, .67, .68, .69. No tasks left open from this session.

The session also caught **87 real spec defects** via the audit-style
binding tests (5 from AR-001 axes, 2 from AR-032 roles, 80 from §8
taxonomy lint). All pinned via expected-violations skip-list with
follow-up beads filed for each fix family.

## What landed this session

Wave-1 (hk-872.51, hk-872.56, hk-zs0.3): IntentLogEntry RECORD +
BrError-sentinel migration to enum + AR-001 four-axis Axes: line audit
(caught 5 real spec defects). Wave-2 (hk-zs0.55/.57/.56 bundled):
`ActorRole` (9-value closed enum) + `PolicyVersion` + `ActionDescriptor`
typed aliases — all 4 placeholders in trace.go substituted; ActionDescriptor
chose typed-string-alias because OQ-EM-005 + handler-contract.md §4.1
genuinely silent on shape (validated by reviewer). Wave-3 (hk-872.31,
hk-b3f.79, hk-872.17): bounded br-stderr capture (5 BI-025d scenarios) +
Outcome RECORD with discriminated-union enforcement + orphan br subprocess
sweep (BI-014a). Wave-4 (hk-zs0.33, hk-b3f.65, hk-hqwn.55, hk-872.53):
AR-032 role-vocabulary audit (caught `investigator` non-canonical use) +
EM-INV-005 git-wins sensor + Subscription RECORD + BrError→reconciliation
routing table. Wave-5 (hk-hqwn.6, hk-hqwn.60, hk-a8bg.58, hk-hqwn.63,
hk-zs0.40): EV-003 process-scope sensor + canonical JSONL fixture in
testhelpers + Kind enum (Gate/Hook/Guard/Budget) + §8 taxonomy lint
(caught 80 defects: 81 events lack Axes: annotation slot in §8 table
format, 6 lack consumer citation) + AR-041 repo-as-SOT audit (clean).

hk-872.34 (Beads-CLI skill) closed-as-subsumed — fully shipped at
`.claude/skills/beads-cli/SKILL.md`.

## Process scars to internalize (NEW this session)

1. **Audit-style binding tests are extraordinarily productive.** Five
   audits this session (AR-001, AR-032, §8 taxonomy, AR-041 + last
   session's AR-005) caught 95 real spec defects in aggregate. The
   `expectedViolations` skip-list pattern (Path B from prior session)
   absorbs known violations cleanly; stale-entry guard catches drift.
   The ratio of "audit lines shipped" to "spec defects caught" is
   excellent. **Treat new normative-discipline beads as candidate
   binding-tests by default.**

2. **Audit scope errors come from file-collector narrowness.**
   hk-hqwn.63's round-1 review caught a false-positive pin because
   `hqwn63FixtureSiblingSpecFiles` only collected `*/spec.md`, missing
   `schemas.md`. Generalize: when implementing audit-style binding tests,
   the sibling-file collector should default to `**/*.md` one level deep,
   not a single hard-coded filename. Add this default to future briefs.

3. **Closed-set follow-up beads after typed-alias-deferral.** Pattern
   reinforced: hk-hqwn.55 deferred ConsumerClass and OnPanic as string
   placeholders, filing hk-hqwn.65/.66 explicitly. The follow-up bead's
   `--description` should name the eventual file (`consumerclass.go`),
   the canonical-sibling pattern (`outcomekind.go`), and the substitution
   work (replace placeholder in subscription.go). The bead body IS the
   work spec — invest in description quality at filing time.

4. **`br create` flag syntax: title is positional, not `-t`.** v9.5
   directives noted `-p N` for priority and `--labels "a,b,c"` comma-sep.
   This session added: **`-t` is `--type`, NOT title.** Use the positional
   title argument (`br create "title goes here" -p 2 --labels "..."`).
   I made this mistake once filing hk-zs0.61.

5. **Race-condition risk on package-var test knobs.** hk-872.17's round-1
   fix added test-time mutation of `orphanSweepGracePeriod` /
   `orphanSweepPollInterval` package vars; the round-2 review's race
   detector caught a real data race because parallel tests read these vars
   while the writer test mutated them. **Fix pattern: drop `t.Parallel()`
   from the writer test only.** Non-parallel tests run before parallel
   tests in Go's test scheduler, so `t.Cleanup` restoration completes
   before any parallel reader fires. Future briefs adding test-tunable
   package vars should warn about this race surface.

6. **Spec-defect-as-design-evidence.** When an audit catches a structural
   defect (e.g., §8 table format has no Axes: annotation slot — 81
   violations), the right move is to PIN ALL the violations and file ONE
   spec-fix bead (hk-hqwn.67), not to weaken the audit. The audit
   correctly captures "the spec is wrong here"; the fix bead captures
   "fix the spec format".

7. **Inline-amend ceiling held.** Multiple successful inline-amends this
   session: hk-872.31 NIT skipped (orchestrator authority), hk-zs0.40
   comment-only fix inline-amended cleanly, hk-hqwn.6 MEDIUM godoc fix
   spawned fix-agent (correctly above ceiling because 2 files needed
   coordinated edit). The ≤3-mechanical-edits-in-1-file ceiling is the
   right discriminator.

## Suggested first move

1. **Verify state**: `git status` clean; `git log --oneline -3` shows
   `7fcf8b0 test(specaudit): clarify ar041 prohibited-channels comment`
   on top of `dc400ab test(specaudit): add AR-041 repo-as-single-source-of-truth binding test`.

2. **Open with 4 typed-enum follow-ups** — small, parallel-safe, all
   filed by this session's bundled work:
   - **hk-hqwn.65** (just filed): `ConsumerClass` typed string alias
     (3 values: synchronous, asynchronous, observer per event-model §6.1).
     Substitutes string placeholder in subscription.go. Sibling pattern:
     `internal/core/outcomekind.go`. ~80-line bead.
   - **hk-hqwn.66** (just filed): `OnPanic` typed string alias (3 values:
     recover_and_log, quarantine_consumer, fail_daemon). Same pattern as
     hk-hqwn.65. Substitutes placeholder in subscription.go. **Bundle
     hk-hqwn.65 + .66 into ONE implementer per v9 directive (both touch
     subscription.go).**
   - **hk-b3f.93** (just filed): VerdictEvent 8-field RECORD per
     reconciliation/schemas.md §6.1. Substitutes `*string` Payload
     placeholder in outcome.go. Sibling pattern: intentlogentry.go.
     Reviewer's hk-b3f.79 review noted this is well-scoped already.
   - **hk-zs0.61** (just filed): ActionDescriptor record promotion —
     **deferred** until handler-contract.md §4.1 defines the structured
     shape. Currently not actionable; leave for spec-author work.

3. **Spec-fix beads** (cognition-tagged, defer to user pair-on per
   session scar from prior session): hk-zs0.58 (RC-015 split), hk-zs0.59
   (BI-025a/b/c idempotency=safe), hk-zs0.60 (RC-015 invalid axis
   vocabulary), hk-zs0.62 (investigator role), hk-hqwn.67 (§8 table needs
   per-row Axes: slot — substantive spec format change), hk-hqwn.68
   (6 events lacking sibling citation — possibly trivially fixable but
   each event needs its own owning-spec citation drafted). **Don't
   dispatch these as implementer briefs.**

4. **Subsystem-add candidates**: `internal/reconciliation/` doesn't exist
   yet but `ReconciliationCategory` (hk-872.53) is a string-alias
   placeholder; when reconciliation/spec.md drives a Go package, the
   typed wrapper migrates there. Watch the `go-subsystem-add` skill.

5. **Subsumed-bead pre-check** (recurring discipline): before dispatching
   anything from the 200+ ready queue, grep for the bead's named symbols.
   This session caught hk-872.34 (Beads-CLI skill) as subsumed by the
   already-shipped `.claude/skills/beads-cli/SKILL.md`. Look for similar
   patterns: anything in the docs/skills/test-fixture corpus may be
   already-done.

## Files to open first

1. `git log --oneline -25` — this session's commits (18 features +
   3 fix-iteration commits + 1 inline-amend + handoff doc).
2. `.claude/implementer-protocol.md` — standing rules; no structural
   change this session.
3. `internal/specaudit/` — the audit family is now 5 binding tests:
   `ar005_tags_test.go`, `ar001_axes_test.go`, `ar032_roles_test.go`,
   `hqwn63_eventmodel_taxonomy_test.go`, `ar041_repo_sot_test.go`. Pattern
   reuse for future audit-style beads.
4. `internal/core/{outcome.go,subscription.go,kind.go,actorrole.go,
   policyversion.go,actiondescriptor.go,intentlogentry.go,
   gitwins_b3f65_test.go,monotsmono_hqwn6_test.go}` — this session's
   8 core landings. Subscription.go is the substitution target for
   hk-hqwn.65/.66; outcome.go is target for hk-b3f.93.
5. `internal/brcli/{stderrcap.go,classifyreconciliation.go}` — two
   brcli landings. classifyreconciliation.go is the BrError→Cat routing.
6. `internal/lifecycle/orphansweepbr.go` — BI-014a sweep helper, no
   daemon-startup wiring yet (deferred per scope rule scar).
7. `internal/testhelpers/jsonlfixture.go` — 8 canonical JSONL fixtures
   for envelope/durability/read-recovery tests. Wire-form precedent set
   via package-private `jsonlEventWire` struct.

## Blocking question for user
None. The 6 cognition-tagged spec-fix beads (hk-zs0.58, .59, .60, .62,
hk-hqwn.67, .68) require user pair-on or kerf jig — they're not
auto-dispatchable. The next session can pick from the typed-enum
follow-ups (hk-hqwn.65/.66 bundled, hk-b3f.93) freely.
