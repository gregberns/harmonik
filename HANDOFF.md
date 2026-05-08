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

`br create` flag syntax (hit this 2026-05-08): use `-p N` for priority NOT
`-P N`; use `--labels "a,b,c"` (comma-separated single arg) NOT repeated
`-l a -l b`; use `--parent <id>` not `-P`. Run `br create --help` if unsure.

When ambiguity arises, spend real effort resolving without escalation. Bead
acceptance criteria is authoritative.

On resume: continue working unless the handoff body flags a real blocker.
Context budget on this 1M-context model is generous (~700k effective). When
you cross ~500k, finish in-flight batch cleanly, then write a fresh HANDOFF
and stop.
<!-- END DIRECTIVES -->

# Session Handoff

## State
Clean. Main at `435c0d4`, pushed to origin. **15 beads landed and closed
this session** across 7 packages: 12 normal closes via worktree-merge, 2
subsumed-closes (hk-8i31.26 + hk-sx9r.3 — work already shipped under prior
closed siblings), 1 fix-iteration close (hk-8i31.50 — sentinel-taxonomy fix).
All worktrees cleaned. New follow-up beads filed: `hk-uwie`, `hk-pvcs.10`,
`hk-ahvq.48.10`.

## What landed this session
hk-i0tw.54 (SuiteID), hk-8mwo.2 (git ≥2.34 detector), hk-sx9r.73 (ON 23-code
exit taxonomy), hk-8mwo.11 (ref-safe bead-ID), hk-8mup.60 (ProjectHash+PGID),
hk-ahvq.48.1 (twin-claude scaffold), hk-872.2 (BI-002 depguard), hk-pvcs.9
(P3 doc cleanup), hk-ido0 (scenario FailureClass), hk-8i31.50 (commithash
verifier), hk-ahvq.48.5 (Makefile twin target), hk-8mwo.10 (integration
branch naming), hk-ahvq.48.4 (twin --version + ldflags var). Subsumed:
hk-8i31.26, hk-sx9r.3.

## Process scars to internalize (NEW)

1. **Sentinel-taxonomy expansion is a spec violation.** hk-8i31.50
   implementer added `ErrCommitHashMismatch` as a third structural sub-
   sentinel; HC-020 enumerates exactly "5 primary + 2 sub-sentinels".
   Reviewer caught it; fix-iteration removed it. Reviewer briefs for any
   bead in HC sentinel space MUST flag the enumerated set as a hard limit.

2. **Build-artifact directories need gitignore.** hk-ahvq.48.5 created
   `twins/` but didn't `.gitignore` the build output; reviewer caught the
   3.4MB binary sitting untracked. When a bead introduces a new artifact
   directory, the gitignore entry is part of the deliverable — say so in
   the brief.

3. **Tests must exercise the function under test.** hk-8mwo.10 implementer
   wrote an "IntegrationBranchName propagates Err…" sub-test that never
   called IntegrationBranchName — it called a helper that called
   BeadIDToRefSafe + a `t.Logf`. A `t.Logf` is not a test. Reviewer caught
   it; orchestrator inline-deleted the bogus test.

4. **Pre-grep the codebase before dispatching for already-shipped work.**
   hk-8i31.26 (ErrSkillProvisioningFailed sub-sentinel) and hk-sx9r.3 (ON
   exit-code obligation) both turned out to be already implemented under
   closed sibling beads. Saved two waves by closing as subsumed instead of
   dispatching. Orchestrator pre-flight: `grep` the bead's named symbols
   before writing the brief; if the work has shipped, close-as-subsumed.

5. **Stale bead-body wording vs. spec — spec wins.** hk-ahvq.48.1 bead body
   referenced `LaunchSpec.SocketPath`; spec §6.1 has no such field; HC-044
   fixes the path at `.harmonik/daemon.sock`. Implementer correctly took
   the `--socket-path` CLI-flag route. Orchestrator inline-amended the
   perpetuating comment in main.go and noted the bead-body staleness in
   the close reason.

## Suggested first move

1. **Verify state**: `git status` clean; `git log --oneline -3` shows
   `435c0d4 feat(cmd/twin-claude): add commitHash ldflags stamp + --version`.

2. **Open with a 4-bead wave**, all parallel-safe:
   - `hk-ahvq.48.10` (just filed): wire `-ldflags "-X main.commitHash=..."`
     into Makefile `build-twin-claude` target. Small, mechanical, unblocks
     hk-uwie integration test.
   - `hk-ahvq.48.2` (twin wire-protocol parity loop): the substantive twin
     work. Larger; pre-grep for any in-tree NDJSON helpers first.
   - `hk-872.4` (Daemon to br direct, agents via skill): brcli + handler.
     Disjoint from .48.x.
   - `hk-jhob` (operational-skills meta-epic): investigate for a concrete
     subtask; otherwise skip.

3. **Subsumed-bead pre-check** (per Scar #4): before dispatching any of
   the remaining `br ready` queue, grep for the bead's named symbols. The
   handoff list contains beads from a corpus where some may already have
   shipped under sibling work.

## Files to open first

1. `git log --oneline -25` — this session's 16 commits (15 features + 1
   fix-iteration deletion).
2. `.claude/implementer-protocol.md` — standing rules; nothing changed
   structurally this session.
3. `internal/handler/` — commithash.go (HC-043 verifier) and launchpath.go
   (SH-009 search-path resolver) just landed; sibling pattern for any new
   handler work.
4. `cmd/harmonik-twin-claude/` — main.go scaffold + version.go ldflags var.
5. `internal/scenario/` and `internal/core/` — both have a `FailureClass`
   enum now (different 8-value vs 6-value sets); don't conflate.

## Blocking question for user
None.
