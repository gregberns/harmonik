<!-- PP-TRIAL:v3 2026-05-06 main -->

<!-- ORCHESTRATION DIRECTIVES — DO NOT EDIT. Loaded every /session-resume. -->
Act as the orchestrator. Delegate substantively; keep main thread small.

Claim from `br ready` (type=task; scope:bootstrap first). Spawn implementers
(model: sonnet, effort: high) with `isolation: worktree`, run_in_background.

Maintain ~8 concurrent agents during bulk work. When one finishes, immediately
spawn the next ready bead. Don't drain the batch before refilling — that's the
bottleneck. (For early-process-refinement passes, smaller batches of 3 are OK
to tighten review loops.)

Before claiming a parallel batch: audit dep edges. Sibling-artifact beads with
content-only `blocks` deps (i.e., one references the other at runtime, not
at authoring time) must have those edges converted: `br dep remove <a> <b>`
then `br dep add <a> <b> --type related`. Without this, `br update --status
in_progress` refuses the claim.

Each implementation gets reviewed (model: sonnet, effort: high). Iterate up
to 4 rounds; stop when no BLOCKER/MAJOR/MEDIUM findings remain. If still
open at round 4, tag `needs-clarification` and move on.

Reviewer brief MUST encode two clauses every time, verbatim:
1. PRECEDENCE — orchestrator directives override `build-practices.md` for
   implementer commits. Expected commit format: subject + 1-line file
   bullets + `Refs:`. Do NOT flag absence of `## Why / ## What / ## Spec
   alignment / ## Test plan / ## Risk` sections, or `Reviewed-By:` /
   `Review-Verdict:` trailers. Implementer self-stamping bit-rots during
   fix iterations; the orchestrator stamps post-merge if anything stamps.
2. TIER DISCIPLINE — MEDIUM = defect against THIS bead's acceptance
   criteria. Cross-cutting / future-bead / spec-doc concerns = MINOR or
   follow-up note, not MEDIUM. Improvements to other files are not
   defects against this bead.

Path-discrepancy resolution: when a bead body and a referenced doc disagree
on a path or identifier, the bead body wins. Implementer patches the
inconsistent file in the same commit and surfaces the still-stale doc to a
follow-up bead. EXCEPTION — for *spec content* (e.g., enum values, regex
shapes), the spec wins per CLAUDE.md ("specs are normative"); bead body
gets the follow-up note.

Orchestrator authority: if reviewer flags MEDIUM but implementation clearly
meets bead acceptance, you may merge with a closure note explaining the
tier-override and (optionally) filing a follow-up bead for the underlying
spec/doc concern. Don't mechanically iterate on cross-cutting noise.

Inline-amend by orchestrator (no fix-agent) for: trivial single-line text
fixes; literal one-line code fixes (e.g., flipping a comparison operator,
fixing a single off-by-one). Re-review for these is theatrical — verify by
reading the result and merge. Spawn fix-agents only for multi-line logic
or when the fix might introduce new issues.

Re-review may also be skipped after a metadata-only fix (commit-message
text only, no file change). Orchestrator may amend and merge directly.

When ambiguity arises, spend real effort resolving without escalation.
Paths to consider: sibling specs, the discipline doc, parent bead body,
git log of related work, a second sub-agent for an independent read. Bead
acceptance criteria is authoritative.

Merge dance — RUN FROM THE MAIN REPO DIR, NOT THE WORKTREE. The recurring
failure mode: `cd worktree && rebase && merge` runs the merge inside the
worktree, making it a no-op (merging branch into itself); push reports
"Everything up-to-date"; worktree-remove succeeds; `branch -d` and
`br close` then fail with "cwd no longer exists". Correct chain — split
into TWO Bash invocations so the cwd doesn't leak across the boundary:

    # invocation 1 (cwd = worktree):
    cd <worktree-path> && git fetch origin main && git rebase origin/main

    # invocation 2 (cwd = main repo, freshly cd'd):
    cd /Users/gb/github/harmonik && git merge --ff-only <branch>
    git push origin main
    git worktree unlock <worktree-path>
    git worktree remove --force <worktree-path>   # --force needed: implementers
                                                  # symlink .tools/ into worktrees,
                                                  # which leaves untracked entries
    git branch -d <branch>
    br close <bead-id> -r "<closure note>"

Use `br close <id> -r "..."` for the closure note. `br update <id> -c "..."`
does NOT exist (no `-c` flag).

After each bead merges, leave no stale worktrees under `.claude/worktrees/`.

On resume: continue working unless the handoff body flags a real blocker.
If context fills or the session feels long: write a fresh HANDOFF, then
judge whether to continue or hand off cleanly.
<!-- END DIRECTIVES -->

# Session Handoff

## State
Clean. Main at `99ccdf6` and pushed. **8-bead enum batch landed in one cycle**,
no fix iterations needed — process is humming.

Beads closed this session:
- `hk-872.46` CoarseStatus, `.47` HarmonikWriteStatus, `.49` EdgeKind, `.52` TerminalOp
- `hk-b3f.81` IdempotencyClass, `.82` TransitionKind, `.83` OutcomeStatus
- `hk-zs0.54` AgentType (regex-validated typed string + 4 reserved MVH IDs)

All eight follow the NodeType pattern (typed string + `Valid()` + `Marshal/UnmarshalText`),
package `core`, 100% coverage on `internal/core`, full `make check` green on integrated main.

## Notes from this batch
- **CoarseStatus bead-body vs spec discrepancy.** Bead body listed
  `{parked, cancelled}`; the spec at `specs/beads-integration.md` §6.1
  has `{draft, pinned}`. Implementer correctly took the spec (CLAUDE.md:
  specs are normative). Closure note records the discrepancy. The
  PRECEDENCE-resolution clause in the directives now codifies this:
  spec wins for spec content; bead body wins for paths/identifiers.
- **Worktree `.tools/` symlinks.** Every implementer this batch had to
  symlink `/Users/gb/github/harmonik/.tools/` into their worktree to make
  `make check` work — fresh worktrees don't inherit the pinned tool dir.
  That leaves an untracked entry, so `git worktree remove` needs `--force`.
  Directive updated to use `--force` by default and to split the merge
  dance into two Bash invocations (orchestrator hit the cwd-leak trap once
  and recovered cleanly; the two-invocation pattern prevents it).

## Next step — §6.1 record-type batch (smaller, batches of 3)
The enums just landed compose into the §6.1 record types. Suggested first
batch (all parents are now unblocked):
1. `hk-872.48` DependencyEdge (composes EdgeKind)
2. `hk-b3f.74` Edge record (composes EdgeKind)
3. `hk-b3f.76` State record

Records are denser than enums (multiple fields, cross-references to other
records, validators). The prior HANDOFF flagged them for "tighter review
batches of 3" — keep that. Use the same implementer/reviewer template:
NodeType for the typed-string shape, but for the record shape itself
follow `internal/core/commitrange.go` (the only existing struct in core).

After that initial 3, the next records would be `hk-b3f.78` Checkpoint,
`hk-hqwn.54` TraceContext, then Run/Transition/Outcome/Workflow/Node.

`br ready -l scope:bootstrap` shows 20 ready (closing 8 unblocked 8 more).

## Files to open first
1. `git log --oneline -12` — the 8 enum commits + lineage
2. `internal/core/commitrange.go` — the only existing struct shape
3. `internal/core/nodetype.go` — pattern for typed identifiers (still relevant)
4. `br ready -l scope:bootstrap` — claimable corpus
5. Bead body for the chosen target via `br show <id>` — only consult docs the bead cites

## Blocking question for user
None.
