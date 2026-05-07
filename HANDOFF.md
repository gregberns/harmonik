<!-- PP-TRIAL:v3 2026-05-07 main -->

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
Clean. Main at `069b3f5` and pushed. **3-bead record batch landed in one cycle**,
all APPROVE-CLEAN, no fix iterations. Process is still humming.

Beads closed this session:
- `hk-872.48` DependencyEdge (composes EdgeKind + typed BeadID)
- `hk-b3f.74` Edge (8-field record, optionals via pointer)
- `hk-b3f.76` State (5-field record, uuid.Nil + !IsZero validation)

All three follow the existing core conventions: package `core`, named struct,
`Valid() bool` method, table-driven tests in `package core` (not `core_test`),
100% coverage on `internal/core`, full `make check` green.

## Notes from this batch
- **PolicyExpression deferral (Edge).** Spec line 664 declares
  `condition : PolicyExpression | None`, but no typed-alias bead for
  `PolicyExpression` exists yet (`br list | grep -i policy` shows only
  unrelated event-row beads). Orchestrator pre-decided to render it as
  `*string` with godoc citing `control-points.md §6.4` for the grammar.
  Future hoist to a typed alias is non-breaking. If a Workflow / Edge
  consumer bead needs the typed shape, file a fresh bead for
  `PolicyExpression` first rather than re-opening Edge.
- **WorkspaceRef will block hk-b3f.75 (Run).** Run record's `input` field
  is `WorkspaceRef` (workspace-model §4.1). Same posture as PolicyExpression:
  if no typed-alias bead exists when Run is claimed, decide *before* the
  implementer brief whether to defer to `*string` placeholder or insert a
  prerequisite WorkspaceRef bead. Current `internal/core/` has no
  workspaceref.go.
- **BeadID-vs-string convention reconfirmed.** Spec text says "String" for
  bead IDs; implementer used typed `BeadID` (which exists at
  `internal/core/beadid.go` as `type BeadID string`). Reviewer accepted.
  Pattern: when a typed wrapper already exists in `core`, prefer it over
  raw `string` even if the spec text uses the abstract type name.
- **Cwd-leak trap, mitigated.** The Bash tool's cwd persists across
  invocations. The merge-dance directive (split into two Bash invocations)
  prevents the worktree-cwd from leaking into the merge step. Followed
  cleanly all three times this batch.

## Next step — second record batch (still 3-at-a-time)
`br ready -l scope:bootstrap` shows 20 ready. Suggested next batch (all
deps closed, all should follow the State / Edge pattern):
1. `hk-b3f.78` Checkpoint (§6.1) — composes RunID, StateID, commit-trailer fields
2. `hk-hqwn.54` TraceContext (§6.1, event-model) — likely a small struct
3. `hk-872.45` BeadRecord (§6.1, beads-integration) — composes DependencyEdge,
   CoarseStatus, BeadID

Audit deps before claiming the batch. None of the three should sibling-block
each other, but `br show` each one and check.

After that batch: `hk-b3f.75` Run (needs WorkspaceRef decision — see Notes
above), `hk-b3f.77` Transition (composes State, OutcomeStatus, TransitionKind),
then Outcome / Workflow / Node — most of §6.1 will be done.

## Files to open first
1. `git log --oneline -8` — the three record commits + prior enum batch
2. `internal/core/state.go`, `edge.go`, `dependencyedge.go` — record-shape patterns
3. `internal/core/commitrange.go` — minimal struct godoc form
4. `internal/core/nodetype.go` — typed-identifier pattern
5. `br ready -l scope:bootstrap` — claimable corpus
6. Bead body via `br show <id>` — only consult docs the bead cites

## Blocking question for user
None. Continue per directives unless WorkspaceRef decision needs an explicit
escalation when Run is claimed (it shouldn't — orchestrator authority covers
the same `*string`-with-godoc-citation deferral pattern used for
PolicyExpression).
