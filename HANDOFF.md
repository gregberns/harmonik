<!-- PP-TRIAL:v2 2026-05-06 main -->

<!-- ORCHESTRATION DIRECTIVES — DO NOT EDIT. Loaded every /session-resume. -->
Act as the orchestrator. Delegate substantively; keep main thread small.

Claim from `br ready` (type=task; scope:bootstrap first). Spawn implementers
(model: sonnet, effort: high) with `isolation: worktree`, run_in_background.

Maintain ~8 concurrent agents. When one finishes, immediately spawn the next
ready bead. Don't drain the batch before refilling — that's the bottleneck.

Each implementation gets reviewed (model: sonnet, effort: high). Iterate up
to 4 rounds; stop when no BLOCKER/MAJOR/MEDIUM findings remain. If still
open at round 4, tag bead `needs-clarification` and move on.

Consider scaling reviews by bead criticality: more reviewers (and Opus) for
cross-cutting / high-fanout / architecture-touching work; fewer for
mechanical beads. Use judgment.

When ambiguity arises, spend real effort resolving without escalation.
Paths to consider: sibling specs, the discipline doc, parent bead body,
git log of related work, a second sub-agent for an independent read. Bead
acceptance criteria is authoritative.

After each bead merges: rebase the worktree branch onto main, ff-merge,
push, then `git worktree unlock && git worktree remove <path>` and
`git branch -d <branch>` to clean up. Close the bead via `br update
--status closed` with a closure note. No stale worktrees under
`.claude/worktrees/`.

On resume: continue working unless the handoff body flags a real blocker.
If context fills or the session feels long: write a fresh HANDOFF, then
judge whether to continue or hand off cleanly.
<!-- END DIRECTIVES -->

# Session Handoff

## State
Clean. Phase 0 closed and pushed (final commit `84a666a`). Worktree
pipeline piloted end-to-end on `hk-pvcs.1` (Go module init, merged at
`bc0688a` on `origin/main`). 145 beads ready corpus-wide; 20 ready in
`scope:bootstrap` per `br ready -l scope:bootstrap`.

## Next step — refine briefs, then two batches of 3 concurrent

The pilot worked but surfaced six small refinements. Encode 1–4 into
the implementer/reviewer brief templates you write inline when spawning
sub-agents (no separate charter doc — keep it close to the call site).

1. Implementer must NOT self-stamp `Reviewed-By` / `Review-Verdict`
   trailers in commit bodies. Real review happens after; orchestrator
   stamps if anything stamps.
2. Implementer commit messages: subject + 1-line bullets of files
   changed + `Refs: <bead-id>`. Avoid `## What / ## Risk` narrative
   sections — they bit-rot when fix-iterations change file content,
   producing stale-text MEDIUMs in the next review.
3. Re-review after a metadata-only fix (commit-message text only, no
   file change) is theatrical. Orchestrator may skip and merge.
4. Trivial single-line text fixes do not need a fix-agent. Orchestrator
   inline amendment is faster than spawning.
5. `br ready` parent-child quirk: epic children look "blocked" because
   the parent epic counts as a dep. The directive's `type=task` filter
   handles this; confirm in dispatcher when picking work.
6. Worktree cleanup discipline is now in the directives block above
   (rebase → ff-merge → push → unlock + remove → branch -d → close).
   Apply consistently after every bead.

Then run **two batches of 3 concurrent**, not 8. Targets:
`hk-pvcs.{2,3,4}` first batch (Makefile, golangci, lefthook), then
`hk-pvcs.{5,6,7}` second (coverage-gate, test-helper scaffold,
forbid-import). `hk-pvcs.8` (BUILDING.md) last — it documents what the
others built. This gets the build-scaffolding epic complete and unblocks
everything downstream.

## If something changes
- BLOCKER persisting through 4 review rounds → tag `needs-clarification`,
  move on. Do NOT block on the user.
- `br ready -l scope:bootstrap` returns 0 → claim from `hk-pvcs`
  children directly via `br show hk-pvcs.<n>` (parent-child filter
  quirk; not a real blocker).
- Context past ~80% → write fresh HANDOFF, `/clear`, `/session-resume`.

## Files to open first
1. `git log --oneline -5` (recent state at a glance)
2. `br ready -l scope:bootstrap` (claimable work; 20 currently)
3. `docs/foundation/project-level/{quality-checks,subsystem-organization,
   build-practices}.md` — only consult when a bead cites them; do not
   pre-read

## Blocking question for user
None.
