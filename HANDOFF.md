<!-- PP-TRIAL:v12 2026-05-08 main (afternoon — Wave-4 EM-NNN contract beads completion) -->

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

**Subsumption signal in implementer briefs (v12 — high leverage).** When
orchestrator's pre-flight read shows a bead's record / field / variant
already exists, surface it explicitly in the brief: "PRE-FLIGHT SIGNAL:
<file:line citation>. The bead may be largely or entirely subsumed. Your
first job is to verify, NOT to invent work." Wave-4 (this session) hit this
3-of-8 times: hk-b3f.5 fully subsumed → SUBSUMED verdict in 48 s with no
commit; hk-b3f.57 type-only partial → counter-only delta, no record edits;
hk-b3f.60 variant-only → godoc-only delta. Without the signal, implementers
default to scaffolding parallel structures and waste cycles. With it, they
verify and scope to the gap.

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
Re-review may be skipped after a metadata-only or pure-deletion fix, OR
after a trivial idiom swap with tests passing (v12 — hk-b3f.57 fmt.Errorf
→ errors.New + import add: 1 file, 2 edits, tests re-run, merged without
re-review).

**Inline-amend ceiling (v9.5 — observed this session).** Inline-amend
worked clean for ≤3 mechanical edits across 1 file (hk-872.4 single-line
delete; hk-872.42 three nolint-directive additions; hk-zs0.13 single-line
delete; hk-hqwn.27 dead-field + dead-return removal; **hk-b3f.57 fmt→errors
2-edit swap, v12**). Above that bar (e.g. hk-872.30: 4 lint fixes including
signature change at multiple call sites), spawn a fix-agent — the
verification cost climbs faster than the dispatch cost.

Path-discrepancy resolution: bead body wins over docs. EXCEPTION — for spec
content (enum values, regex shapes, RECORD field-types, **enumerated
message-type sets**), the spec wins per CLAUDE.md ("specs are normative");
bead body gets the follow-up note. (Reinforced this session: hk-ahvq.48.2
fix-agent correctly OMITTED `agent_log` because HC-007 doesn't enumerate it,
even though the bead body listed it. Reviewer cleared. Reinforced again
v11: hk-b3f.99 PolicyRef bead-title cited control-points.md §6.3 but
execution-model.md §6.1 cites §6.4 — implementer correctly chose §6.4 and
surfaced the discrepancy. Reviewer cleared. **Reinforced v12: hk-b3f.25
implementer found stale `EM-036` citation in pre-existing godoc — EM-036
does not exist in spec; EM-028 is the correct anchor. Implementer fixed
in-place as additive correction; reviewer verified and cleared.**)

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
Clean. Main at `a202085`, pushed. **9 beads landed this session** across one
7-implementer parallel wave + 1 refill, two pipelined merge waves. **0
rebase conflicts, 0 fix-iterations, 1 orchestrator inline-amend.** All
worktrees cleaned (verified `git worktree list` shows main only). `.claire/`
untracked dir still here from prior sessions (typo of `.claude/`, leave
alone).

## What landed this session

**Wave 4 — EM-NNN execution-model contract beads (8 dispatches: 7
parallel + 1 refill):**

- **hk-b3f.5 SUBSUMED** (Outcome record + EM-005/EM-005a): existing
  internal/core/outcome.go:34-105 + outcomekind.go fully satisfy the
  contract (Kind/Payload discriminator, Cat 6a routing reference, full
  test coverage). No commit; closed-with-citation.
- **hk-b3f.1** (EM-001 Workflow well-formed-graph invariant) — `fb4797c`:
  Workflow.Valid() now enforces edge endpoints in Nodes; +13 LOC + 3 tests.
- **hk-b3f.19** (EM-016 Checkpoint atomicity contract) — `3f291d8`: godoc
  on write-tree → commit-tree → update-ref sequence; new path-coherence
  invariant in Valid() (TransitionRecordPath == TransitionRecordPath(RunID,
  TransitionID)); side-fix to gitwins_b3f65_test.go fixture path mismatch.
- **hk-b3f.23** (EM-018a TransitionIDGenerator) — `2d595f1`: daemon-local
  UUIDv7 mirroring eventidgen pattern; +IsUUIDv7 method on TransitionID; 6
  unit + 6 stress tests.
- **hk-b3f.25** (EM-020 immutability contract) — `44d01b1`: godoc on
  write-once + audit-tool dep + EM-036 → EM-028 stale-citation fix in
  pre-existing godoc.
- **hk-b3f.36** (EM-028 canonical durable form vs projection) — `8dfcc3c`:
  new TransitionEventPayload type with exactly the 3 EM-028 fields,
  EM-029 no-duplication observed structurally.
- **hk-b3f.57** (EM-043 + EM-043a cycle counter) — `b2568a1` + `a202085`
  (orchestrator idiom fix): new CycleCounter with per-(run_id, edge) map,
  ReconcileFromTransitions for restart-recovery, ErrCompilationLoop
  sentinel; 10 tests including race-detector.
- **hk-b3f.60** (EM-046 context-restore behavioral contract) — `452b1b4`:
  godoc citation in Transition.Valid() (existing needsRollback guard
  already enforced the constraint; pre-existing tests cover both
  directions); 2 follow-up beads filed (`hk-b3f.107`, `hk-b3f.108`) for
  daemon-side rules.
- **hk-hqwn.9** (EV-006 wall-clock advisory) [REFILL] — `73ea7d5`:
  TimestampWall godoc citing EV-006 + new wallclock_hqwn9_test.go sensor
  pattern (264 LOC paralleling monotsmono_hqwn6_test.go).

Net effect: Workflow / Checkpoint / TransitionID / Transition record /
Edge cycle-counter / Event TimestampWall — all carry their EM- / EV- spec
citations and structural invariants in code.

Follow-up beads filed mid-wave: **hk-b3f.107** (daemon-initiated
context-restore initiation-source enforcement) and **hk-b3f.108** (daemon
Outcome synthesis for context-restore). Both blocked on daemon subsystem
scaffold.

## Process scars to internalize (NEW this session)

1. **Subsumption signals in briefs are high-leverage (codified as
   directive v12).** 3 of 8 wave-4 dispatches had pre-existing partial
   implementations that orchestrator pre-flight read caught: hk-b3f.5
   (fully subsumed → 48-second SUBSUMED verdict, no commit), hk-b3f.57
   (Edge.TraversalCap field present; counter store missing → counter-only
   delta, no record edits), hk-b3f.60 (TransitionKindContextRestore
   variant present; godoc citation missing → 7-line godoc-only delta).
   Surfacing these in the brief — "PRE-FLIGHT SIGNAL: <file:line>. The
   bead may be subsumed. Your first job is to verify, NOT to invent
   work." — got tight scope-to-the-gap deltas every time. Without the
   signal, implementers default to scaffolding parallel structures.

2. **Bundle decision: same-record-territory only.** hk-b3f.25 + .36
   bundled into ONE implementer with TWO sequential commits (same prefix,
   both bead IDs in commit messages, instructed not to squash) — clean.
   Bundling worked because both beads concern transition-record semantics
   (transition.go / transitionrecord.go). hk-b3f.60 was kept solo despite
   "transition" in its name because its only edit was transitionkind.go-
   adjacent godoc, which would have collided line-wise with E's
   transitionrecord.go work; the file-territory check held.

3. **Inline-amend bar held; codified as v12.** F's idiom fix (fmt.Errorf
   → errors.New) was 1 file, 2 edits (import line + sentinel line), one
   re-test, one new commit, no fix-agent, no re-review. The "trivial
   idiom swap with tests passing" generalization to the v9.5 bar is now
   in the directives.

4. **`.claire/` untracked dir is STILL here.** Pre-existing typo'd dir
   from a prior session. Has a `worktrees/` subdir. Leave alone unless
   asked.

## Suggested first move

1. **Verify state**: `git status` shows only `.claire/` untracked;
   `git log --oneline -6` top is `a202085 fix(core): use errors.New for
   ErrCompilationLoop sentinel (idiom parity)`.

2. **Next obvious wave: event-model continuation + remaining hk-b3f
   contract beads.** Pre-flight `br ready --label tag:mechanism --limit
   30`. Strong candidates:
   - **hk-hqwn.34** (EV-025 — each event type has exactly one owning
     spec for payload shape) and **hk-hqwn.35** (EV-026 — subsystems MAY
     emit internal events not on this list). Both look like spec-text +
     sensor-test beads similar in shape to hk-hqwn.9 (use the
     wallclock_hqwn9_test.go pattern). **Bundle both into one
     implementer** with 2 sequential commits.
   - Remaining hk-b3f beads from the parent epic — run `br show hk-b3f
     --format json` to see open children. Several may be subsumed
     already; surface subsumption signals per directive v12 when
     dispatching.

3. **Daemon scaffold is the next cross-cutting unlock.** hk-b3f.107,
   hk-b3f.108, the cycle-counter git-history adapter, and the EM-016
   git-plumbing all defer to "daemon subsystem when scaffolded." Worth a
   conversation with the user before dispatching — this is bigger than a
   single-implementer bead and may need a kerf work first.

4. **Cognition-tagged spec-fix beads STILL need user pair-on**:
   hk-zs0.58, .59, .60, .62, hk-hqwn.67, .68. Don't dispatch as
   implementer briefs.

5. **Deferred until subsystems exist**: hk-hqwn.70 (BusFlusher /
   EventBus.Flush — needs event-bus subsystem), hk-872.57
   (divergence_inconclusive event emission — same), hk-b3f.107 / .108
   (daemon-side context-restore rules).

## Files to open first

1. `git log --oneline -10` — this session's commits.
2. `.claude/implementer-protocol.md` — standing rules; no structural
   change this session.
3. Files touched this session (verify EM-/EV- citations are intact):
   `internal/core/{workflow.go, checkpoint.go, transition.go,
   transitionidgen.go, transitioneventpayload.go, cyclecounter.go,
   event.go}`.
4. New files this session:
   `internal/core/{transitionidgen.go, transitionidgen_test.go,
   transitionidgen_stress_test.go, transitioneventpayload.go,
   transitioneventpayload_test.go, cyclecounter.go, cyclecounter_test.go,
   wallclock_hqwn9_test.go}`.

## Blocking question for user

None. The 6 cognition-tagged spec-fix beads still need pair-on; everything
else (remaining hk-b3f / hk-hqwn / hk-872 mechanism beads) is
freely dispatchable. Daemon scaffold is the next strategic question
worth raising before the next big wave.
