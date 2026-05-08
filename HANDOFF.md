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
Clean. Main at `5ca45d9`, pushed to origin. **13 beads landed and closed
this session** across 7 packages: 12 normal closes via worktree-merge plus
1 subsumed-close (hk-jhob meta-epic — all 4 children already shipped). All
worktrees cleaned. **5 follow-up beads filed: hk-872.56, hk-zs0.55,
hk-zs0.56, hk-zs0.57, hk-zs0.58.** No tasks left open from this session.

## What landed this session

Wave-1 (hk-ahvq.48.10, hk-872.25, hk-872.4, hk-ahvq.48.2): twin-binary
ldflags wire + Beads-version pin + BI-004 handler-brcli depguard + twin
NDJSON wire-protocol with 12 HC-007 emitters. Wave-2 (hk-872.30, hk-b3f.84,
hk-hqwn.56, hk-872.42, hk-hqwn.27): brcli timeout discipline (5s/10s
SIGTERM-grace-SIGKILL per HC-018) + OutcomeKind + EventPattern + BI-INV-002
sensor + EV-019 panic-recovery helper. Wave-3 (hk-zs0.13, hk-872.50,
hk-zs0.7, hk-hqwn.62): 11-field Trace RECORD + BrError closed-set enum +
AR-005 spec audit + UUIDv7 monotonicity stress test. hk-jhob meta-epic
closed-as-subsumed.

## Process scars to internalize (NEW v9.5)

1. **Inline-amend ceiling = ~3 mechanical edits in 1 file.** Above that,
   spawn a fix-agent. Hit by hk-872.30 (4 lint fixes including signature
   change at 2 call sites) where the verification cost outweighed dispatch.
   Easy heuristic: if you'd need to Read the file twice, dispatch.

2. **Spec-binding tests need an expected-violations skip-list.** When an
   audit (e.g., AR-005 mechanism|cognition tagging) catches a real spec
   defect, the audit can't ship in a perpetually-failing state. The right
   pattern (used by hk-zs0.7) is: `expectedViolations` map keyed by
   `(file:line:requirement-id)` with bead pin; pinned-and-present →
   `t.Logf`, pinned-and-absent → `t.Errorf` (stale-entry guard), unexpected
   violation → `t.Errorf`. Generalizable to any audit-style binding test.

3. **Migration follow-up beads belong in the brief, not the implementer's
   judgment.** hk-872.30's `ErrBrTimeout` got a `TODO(hk-872.50)` because
   the brief told it to defer. hk-872.50's brief told the implementer to
   file the migration follow-up bead (hk-872.56) explicitly. The pattern:
   when a bead defines an enum/type that other code wants to migrate to,
   file the migration as a sibling bead, not as part of the defining bead.

4. **Empty `cmd/` for daemon binary defers caller-wiring beads.** hk-872.25
   shipped `internal/release.BeadsVersion` but the daemon-startup
   `CheckBrVersion(release.BeadsVersion, ...)` wiring is deferred — no
   daemon binary in `cmd/` yet. When a bead's primary use-case is "wired
   into the daemon," scope the deliverable to "the artifact + its self
   tests"; the wiring lands with the daemon-binary bead. Currently affects:
   `internal/release.BeadsVersion`, `internal/lifecycle.RecoverWithLogFlush`.

5. **`OutcomeKind` vs `OutcomeStatus`** — both live in `internal/core/`,
   distinct concepts. `OutcomeKind` (hk-b3f.84, just landed) is the §6.1
   discriminator for `Outcome.payload` envelope (2 values). `OutcomeStatus`
   (pre-existing) is the 4-value result enum. hk-zs0.13's reviewer caught
   the potential confusion; the implementer correctly used `OutcomeStatus`
   for `Trace.Outcome`. Future briefs naming "outcome" should disambiguate.

6. **HC-020 sentinel-set discipline persists.** No new HC-020 sub-sentinels
   added this session (vs prior session's hk-8i31.50 fix-iteration).
   `ErrBrTimeout` (hk-872.30) lives in `internal/brcli/`, NOT in
   `internal/handlercontract/sentinels.go` — that distinction matters.

## Suggested first move

1. **Verify state**: `git status` clean; `git log --oneline -3` shows
   `5ca45d9 test(core): add UUIDv7 monotonicity stress tests (hk-hqwn.62)`.

2. **Open with a 4-bead wave** — concrete, all parallel-safe:
   - **hk-872.56** (just filed): mechanical migration — replace
     `ErrBrTimeout` (timeout.go) and `ErrBrVersionIncompatible` (version.go)
     sentinels with `BrError` enum values; remove the `TODO(hk-872.50)` /
     `TODO(hk-872.28)` lines. Small, ~30-line bead, unblocks the brcli
     error-classification cleanup.
   - **hk-zs0.55** (just filed): `ActorRole` typed string alias (7 AR-032
     roles + daemon/reconciliation synthesized) with Valid/MarshalText/
     UnmarshalText. Sibling pattern: outcomekind.go, failureclass.go.
     Substitutes the placeholder string in trace.go.
   - **hk-zs0.56** (just filed): `ActionDescriptor` schema for
     `candidate_actions`/`chosen_action` fields in trace.go. Slightly more
     substantive — read AR-012 to confirm the shape before dispatching.
   - **hk-zs0.57** (just filed): `PolicyVersion` typed string alias for
     trace.go's `policy_version` field. Smallest of the three.

3. **Subsumed-bead pre-check** (per Scar #4 prior session): before
   dispatching anything from the 200+ ready queue, grep for the bead's
   named symbols. Several beads in the corpus may already have shipped
   under sibling work — `internal/specaudit/` is a fresh package and may
   absorb other audit-style beads (e.g., AR-001 four-axis classification,
   hk-hqwn.40 four-axis tags on event types).

4. **hk-zs0.58 (RC-015 split) is NOT a typical implementer brief.** It's
   a substantive spec-author edit splitting a normative requirement into
   two halves (mechanism + cognition). Either dispatch with a kerf jig or
   defer until the user pairs on it.

## Files to open first

1. `git log --oneline -16` — this session's 16 commits (13 features +
   3 fix-iterations).
2. `.claude/implementer-protocol.md` — standing rules; nothing changed
   structurally this session.
3. `internal/release/manifest.go` — new leaf package shipped this session
   (BeadsVersion=0.1.45). Will be the import target for the daemon-binary
   bead's CheckBrVersion call.
4. `internal/brcli/{timeout.go,brerror.go,sensorbiinv002_test.go}` — three
   landings this session. hk-872.56 will touch timeout.go to migrate the
   sentinel.
5. `cmd/harmonik-twin-claude/wire.go` — 12 HC-007 emitters now in place;
   integration test target for OPEN bead `hk-uwie` (the real-binary
   round-trip test deferred since hk-ahvq.48.4).
6. `internal/specaudit/ar005_tags_test.go` — audit-style binding-test
   pattern; sibling beads in zs0/sx9r/hqwn families may follow this shape.
7. `internal/core/{trace.go,outcomekind.go,eventpattern.go}` — three
   RECORD/enum landings this session. Trace's typed-alias placeholders
   (`ActorRole`, `CandidateActions`, `ChosenAction`, `PolicyVersion`) are
   the targets of hk-zs0.55/.56/.57.

## Blocking question for user
None.
