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
Clean. Main at `28259d1` and pushed. **Second 3-bead record batch landed in one
cycle.** Two consecutive 3-batches now (DependencyEdge/Edge/State, then
Checkpoint/TraceContext/BeadRecord) with effectively zero fix work — only one
trivial inline orchestrator amend (godoc text on Checkpoint). Process is stable.

Beads closed this session:
- `hk-hqwn.54` TraceContext (3 optional pointer fields; APPROVE-CLEAN)
- `hk-872.45` BeadRecord (7 fields, composes BeadID + CoarseStatus + []DependencyEdge; APPROVE-CLEAN)
- `hk-b3f.78` Checkpoint (7 fields, composes RunID/StateID/TransitionID + optional *BeadID; APPROVE-WITH-NITS — single godoc text fix amended inline)

All follow the existing core conventions: package `core`, named struct,
`Valid() bool` method, table-driven tests in `package core` (not `core_test`),
100% coverage on `internal/core`, full `make check` green.

## Notes from this batch

- **SchemaVersion = `int`, guard rejects only `== 0`.** The Checkpoint godoc
  initially claimed "non-zero (positive)" — reviewer caught the mismatch (a
  `-1` would pass). Spec says `Integer` without "positive" constraint
  (unlike `timeout` which does). Orchestrator amended godoc to drop "positive"
  (matching code to documentation, not the other way). **Future SchemaVersion
  fields: same call** — keep guard at `!= 0`, do not over-claim positivity in
  godoc unless the spec explicitly requires positive.
- **All-optional records: Valid() permits the zero-value.** TraceContext
  Valid() returns true when all three pointers are nil — that's the central
  invariant of an all-optional record. The non-nil-but-empty rejections are
  what make Valid() useful. (Pattern reusable for any future all-optional
  record.)
- **Inline-amend pattern works cleanly.** For the SchemaVersion godoc tweak:
  edit in worktree → `git commit --amend --no-edit` → rebase → merge dance.
  No fix-agent spawned, no re-review. Total elapsed: ~30s. Use this for any
  one-line text/code fix the reviewer flags as MINOR/MEDIUM.
- **Two-Bash-invocation merge dance held throughout.** No cwd-leak failures.
  Rebase in worktree (invocation 1), merge from main repo dir (invocation 2,
  freshly cd'd). Six successful merges this session under this pattern.

## Next-batch decision overhang

**`hk-hqwn.53` Event envelope** is now ready (TraceContext closed it). Body
references `type` field as `EventType` enum. **No EventType file in `core/`
yet.** The EventType enum is large — `br list -l spec:event-model` shows
~80 leaf beads under parent `hk-hqwn.59` (one per event-row in §8.6/§8.7/§8.8).
That parent appears to be the EventType-enum aggregator.

Two options when claiming Event envelope:

  (a) **Defer EventType to `string` placeholder** with godoc citing
  `event-model.md §8` and a follow-up bead for the typed enum. Mirrors the
  PolicyExpression deferral on Edge. **Risk:** EventType drives a typed
  payload registry per §6.3 — string is fragile for future
  payload-deserialization work.

  (b) **Insert prerequisite bead first.** Check whether `hk-hqwn.59`
  (or sibling) is the EventType-enum parent and whether it's claimable as
  a single bead. If yes, claim and land that first; Event envelope follows
  cleanly with typed `EventType`.

  Recommendation: (b) if `hk-hqwn.59` is small and self-contained as the
  enum-only definition (i.e., the .59.XX leaf beads are the per-row event
  taxonomy work, not enum-definition work). Otherwise (a). Inspect with
  `br show hk-hqwn.59` before claiming.

**`hk-b3f.75` Run** is also ready. Body: `input` field is `WorkspaceRef`
(workspace-model §4.1). No `workspaceref.go` in `core/`. **Pre-decided last
session: apply `*string` deferral with godoc citing workspace-model §4.1.**
Same posture as PolicyExpression on Edge. Future hoist to typed alias is
non-breaking.

**`hk-b3f.77` Transition is NOT ready** — blocked by `hk-zs0.33` (Seven roles
named — canonical role vocabulary). The 13-field record references
`actor_role` which depends on the role enum. Skip Transition until roles land.

## Suggested next batch — 3 beads, mixed shape

1. `hk-b3f.75` Run — apply WorkspaceRef → `*string` deferral
2. `hk-hqwn.53` Event envelope — see option (a)/(b) above; resolve before brief
3. `hk-8i31.24` Five primary sentinel classes (HC-020) — different shape
   (`var ErrTransient = errors.New(...)` etc., with `%w`-wrapping for the
   two structural sub-sentinels). Good shape-diversity test for the process.

Audit deps before claiming. None of these three should sibling-block each
other (Run + Event envelope are different specs; Sentinels is HC). `br show`
each one anyway.

## Files to open first
1. `git log --oneline -10` — six record commits + prior batches
2. `internal/core/checkpoint.go`, `beadrecord.go`, `tracecontext.go` — latest record-shape patterns
3. `internal/core/edge.go` — **PolicyExpression deferral pattern** (relevant for WorkspaceRef on Run)
4. `br ready -l scope:bootstrap` — claimable corpus (20 entries)
5. `br show hk-hqwn.59` — check whether this is the EventType enum parent (resolves (a)/(b) above)
6. Bead body via `br show <id>` — only consult docs the bead cites

## Blocking question for user
None. Continue per directives. Resolve Event envelope's EventType deferral
question (a)/(b) by inspecting `br show hk-hqwn.59`; orchestrator authority
covers either path.
