<!-- PP-TRIAL:v11 2026-05-08 main (afternoon refresh after 13-bead typed-alias completion session) -->

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

**Pre-dispatch TODO grep (v11 — high leverage).** Before dispatching a
typed-alias / RECORD bead, grep `internal/core/*.go` for `TODO(<bead-id>)`
markers. If multiple in-flight beads each have a marker on the same file
(e.g. node.go), serialize the substitutions OR scope each implementer to
type-definition-files-only and file ONE serial follow-up bead for the
substitution pass. This session's typed-alias wave avoided rebase friction
by accident (Bundles A, C went type-defs-only; Bundle B substituted 3
fields without conflict). Make it intentional next time.

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
even though the bead body listed it. Reviewer cleared. Reinforced again
v11: hk-b3f.99 PolicyRef bead-title cited control-points.md §6.3 but
execution-model.md §6.1 cites §6.4 — implementer correctly chose §6.4 and
surfaced the discrepancy. Reviewer cleared.)

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
Clean. Main at `3fe300f`, pushed to origin. **13 beads landed this session**
across 3 pipelined merge waves with **0 rebase conflicts and 0 fix-iterations**.
All worktrees cleaned. 3 orphan branches from a prior session deleted
(`worktree-agent-{a81fc0155348f648a, ab0d5be116be801d9, ae62858e28981ec72}` —
all were fully merged into main).

## What landed this session

**Wave 1 (5 beads):** hk-b3f.94 SnapshotToken (extracted from verdictevent.go
to its own file with Valid()), hk-b3f.97 AxisTags (struct with 4 sub-enums:
LLMFreedom/IODeterminism/ReplaySafety/AxisIdempotency + BaselineAxisTags),
hk-b3f.101 FreedomProfileRef, hk-b3f.102 BudgetRef, hk-b3f.103 SubWorkflowRef.

**Wave 2 (5 beads):** hk-b3f.95 Evidence (typed map[string]any wrapper with
EvidenceKeySubWorkflowPin/SynthesizedOutcome reserved-key constants),
hk-b3f.96 VerifierMetrics (typed wrapper, permissive Valid), hk-b3f.98
ModeTag (closed enum {mechanism, cognition}), hk-b3f.99 PolicyRef, hk-b3f.100
GateRef. Bundle B also substituted ModeTag/PolicyRef/GateRef into Node;
Bundle D substituted Evidence/VerifierMetrics into Transition.

**Wave 3 (3 beads, all follow-ups I filed mid-session):** hk-b3f.104
substituted the remaining 4 typed aliases into Node fields (Axes string→
AxisTags, +3 *Ref fields), hk-b3f.105 substituted PolicyRef into Workflow.
Policies, hk-b3f.106 wired SnapshotToken.Valid() into VerdictEvent.Valid()
delegation (closes a pre-existing latent bug surfaced by the Wave-1
reviewer).

Net effect: `Node`, `Workflow`, `Transition`, and `VerdictEvent` records
are now fully migrated to typed aliases. No `TODO(hk-b3f.NN)` markers
remain in core for these field-substitution beads.

## Process scars to internalize (NEW this session)

1. **Pre-dispatch TODO grep (codified in directives v11).** I dispatched
   3 implementers in parallel before realizing all three beads had
   `TODO(hk-b3f.NN)` markers in `node.go` from a prior session. The wave
   landed clean only because Bundles A and C chose conservatively (type-
   def files only, no substitution). Future agent: grep `TODO(<bead-id>)`
   pre-dispatch and explicitly scope substitution as separate beads if
   3+ markers share a file.

2. **Empty bead bodies are normal for follow-up beads.** All 13 beads in
   this session had empty `description` fields — title + spec citation
   were the entire spec. Implementers handled this fine when the brief
   provided spec section pointers + a canonical sibling pointer
   (outcomekind.go for closed enums, actiondescriptor.go for non-empty
   string aliases). Don't re-investigate body-emptiness.

3. **Pipelined merges scale to 3 waves cleanly.** 5+5+3 beads merged
   over 3 waves with zero rebase conflicts. The trick: each implementer's
   diff was non-overlapping by line distance within shared files (e.g.
   Bundle F's Axes lines 19/26/150/313 vs Bundle G's Policies lines
   37/261 in workflow_test.go).

4. **Implementer scope-extension is the right pattern.** Bundle F's brief
   only mentioned node.go and node_test.go; the implementer correctly
   extended to workflow_test.go (which embeds Node literals and broke
   compilation when Axes type changed) and surfaced the deviation in
   their report. Trust this pattern; don't over-specify mechanical scope.

5. **`.claire/` untracked dir is still here.** Pre-existing typo'd
   directory at repo root with a `worktrees` subdir — not from this
   session. Leave alone unless asked.

## Suggested first move

1. **Verify state**: `git status` shows only `.claire/` untracked;
   `git log --oneline -3` top is `3fe300f feat(core): substitute 4
   typed aliases into Node fields (hk-b3f.104)`.

2. **The next obvious wave is the EM-NNN contract beads** in the ready
   queue (all `tag:mechanism`, all dispatchable):
   - hk-b3f.1 (EM-001 Workflow is named/versioned/directed-graph)
   - hk-b3f.5 (EM-005/EM-005a Outcome record + kind discriminator —
     **(coalesce) tag — pre-flight subsumption check**: OutcomeKind enum
     and Outcome.kind/payload fields may already be present from earlier
     work)
   - hk-b3f.19 (EM-016 Checkpoint = git commit + sibling-file pattern)
   - hk-b3f.23 (EM-018a Transition ID generation contract — daemon-local
     UUIDv7, may already be in transitionid.go)
   - hk-b3f.25 (EM-020 Transition records are immutable)
   - hk-b3f.36 (EM-028 Transition record canonical durable form vs event)
   - hk-b3f.57 (EM-043/EM-043a Cycle traversal cap — **(coalesce) tag,
     check subsumption against edge.go TraversalCap**)
   - hk-b3f.60 (EM-046 Context restore is agent-scoped)

   These are bigger than typed-alias work — each likely 50-150 LOC
   implementation + tests. Bundle 1-2 per implementer; pre-grep for any
   pre-existing partial implementation before dispatching.

3. **Event-model wave** (hk-hqwn.9 EV-006, .34 EV-025, .35 EV-026) is
   smaller and parallel-safe; could go alongside.

4. **Cognition-tagged spec-fix beads STILL need user pair-on**: hk-zs0.58,
   .59, .60, .62, hk-hqwn.67, .68. Don't dispatch as implementer briefs.

5. **Deferred until subsystems exist**: hk-hqwn.70 (BusFlusher / EventBus.
   Flush — needs event-bus subsystem), hk-872.57 (divergence_inconclusive
   event emission — same).

## Files to open first

1. `git log --oneline -13` — this session's commits.
2. `.claude/implementer-protocol.md` — standing rules; no structural
   change this session.
3. `internal/core/{node.go,workflow.go,transition.go,verdictevent.go}` —
   four records now fully typed. Verify zero `TODO(hk-b3f.NN)` markers
   for the closed beads (`grep -n 'TODO(hk-b3f' internal/core/*.go`
   should return nothing relevant to closed beads).
4. New typed alias files: `internal/core/{axistags.go, modetag.go,
   policyref.go, gateref.go, freedomprofileref.go, budgetref.go,
   subworkflowref.go, evidence.go, verifiermetrics.go, snapshottoken.go}`.

## Blocking question for user

None. The 6 cognition-tagged spec-fix beads still need pair-on; everything
else (EM-NNN contract beads, EV-NNN event-model beads) is freely
dispatchable.
