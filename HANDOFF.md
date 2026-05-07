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
follow-up bead.

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
`br close` then fail with "cwd no longer exists". Correct chain:

    cd <worktree-path> && git fetch origin main && git rebase origin/main
    cd /Users/gb/github/harmonik && git merge --ff-only <branch>
    git push origin main
    git worktree unlock <worktree-path>
    git worktree remove <worktree-path>
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
Clean. Main at `c00253e` and pushed. **8-bead batch landed** plus three
orchestrator-tier infrastructure commits.

Beads closed this session:
- `hk-pvcs.8` BUILDING.md (closes the `hk-pvcs` build-scaffolding epic)
- `hk-b3f.66` RunID, `.67` StateID, `.68` TransitionID — UUID-backed
  named types in `internal/core/`, with `String/MarshalText/UnmarshalText`
- `hk-b3f.69` NodeID, `.70` BeadID — String-backed named types
- `hk-b3f.71` CommitRange — named struct (FirstCommitSHA, LastCommitSHA)
- `hk-b3f.80` NodeType — typed-string enum (5 spec-exact values, Valid()
  predicate, MarshalText/UnmarshalText reject unknowns)

Orchestrator-tier infrastructure commits (see `git log` between
`6fcdfaf..c00253e`):
- `d4b47c0` build: repair check gauntlet — drop `gosimple` (subsumed
  into `staticcheck` in golangci-lint v2), exempt `tools/` from
  forbidigo+noctx (CLI tools print to stdout / call exec.Command),
  gofumpt-align goListPackage struct, bash-4 re-exec preamble for
  coverage-gate.sh (macOS ships bash 3.2; lacks `declare -A`), exempt
  `internal/testhelpers` from FLOOR coverage gate (test infra).
- `e5e1616` build: add `allow: [$gostd, github.com/google/uuid]` to the
  depguard `core` rule. v2 deny-only rules implicitly default to
  stdlib-only and blocked uuid until this landed.
- `0b9b453` build: migrate `.golangci.yml` to v2 schema (`linters-settings:`
  → `linters.settings:`; `disable-all: true` → `default: none`; forbidigo
  `p:` → `pattern:`; add `internal/testhelpers/` to forbidigo exclusions);
  cover StateID/TransitionID with package-internal tests (95% CORE gate
  was firing once methods landed). Verified with `golangci-lint config
  verify`.

**Why all this gauntlet repair was needed.** Despite the prior session
closing `hk-pvcs.{1..7}` as "CLEAN", `make check` was actually red on
clean `main` for five independent reasons listed above. Most likely the
prior session ran `check-fast` or stopped at the first red and didn't
notice downstream gates. Implementer #1 surfaced this by running `make
check` honestly and reporting the failures rather than "passing". The
RunID implementer (Sonnet) tried to fix all of them in-bead — overscoped
but with a correct read of the situation. Their fixes were re-derived
orchestrator-side and landed as separate infrastructure commits; their
overlapping diffs were dropped during rebase via `git checkout --ours`.

20 ready in `br ready -l scope:bootstrap` (next cluster: hk-872 enums,
hk-b3f.81/.82, hk-zs0.54).

## Lessons for the next batch — encode in implementer briefs

1. **internal/core test files use `package core`, not `package core_test`.**
   The depguard `core` rule denies `github.com/gregberns/harmonik/internal/`
   imports from `internal/core/**` files. External-test convention
   (`package core_test`) requires importing `internal/core` to reach the
   exported API, which trips the rule. Internal-test convention (the
   stateid_test.go / transitionid_test.go pattern) avoids the import
   entirely.
2. **Each new public exported method needs a test or the 95% CORE gate
   fires.** NodeType's `MarshalText` slipped through the implementer's
   table-driven `UnmarshalText` test; orchestrator added a one-function
   test inline-amend. Brief should say: "tests must cover every exported
   method body, not just the one named in the bead."
3. **The first `internal/core` file in any batch carries the package doc
   comment** (`// Package core ...` above `package core`). Subsequent
   files in the same package don't need their own. revive's
   `package-comments` rule fires on whichever file is alphabetically
   first in the package without one — and on a clean main, packages are
   often empty before the first bead lands. Briefs: "if your file might
   be the first in `internal/core/`, include the package doc comment."

## Next step — bulk work
Process is now nailed AND the gauntlet is solid. Switch to ~8-concurrent
and continue eating the ready corpus.

Suggested next batch (mechanical enums + a typed alias):
1. `hk-872.46` CoarseStatus, `.47` HarmonikWriteStatus, `.49` EdgeKind,
   `.52` TerminalOp — 4 enums (typed-string + Valid() + Marshal/Unmarshal)
2. `hk-b3f.81` IdempotencyClass, `.82` TransitionKind — 2 more enums
3. `hk-zs0.54` agent_type identifier regex — typed string with regex

That's ~7 beads, all matching the NodeType pattern. The next "bigger"
batch would be the §6.1 record types (Workflow, Node, Edge, Run, State,
Transition, Checkpoint, Outcome) which compose the typed aliases just
landed; those are larger and warrant tighter review batches of 3.

Briefs: reuse the implementer + reviewer templates from this session
(see git log on `worktree-agent-*` commits and closure notes on the
8 closed beads). Always encode the two reviewer clauses verbatim. Add
the three "Lessons for the next batch" items above to implementer briefs
for all internal/core work going forward.

## If something changes
- BLOCKER persisting through 4 review rounds → tag `needs-clarification`,
  move on. Do NOT block on the user.
- `br ready -l scope:bootstrap` returns 0 → claim from open epic
  children directly via `br show <id>` (parent-child filter quirk;
  not a real blocker).
- `make check` fails on a clean main → trust the failure; the gauntlet
  is now honest. Triage which gate, fix the root cause, commit as an
  orchestrator-tier infrastructure commit before resuming bead merges.
- Context past ~80% → write fresh HANDOFF, `/clear`, `/session-resume`.

## Files to open first
1. `git log --oneline -12` (recent infrastructure + bead commits)
2. `br ready -l scope:bootstrap` (claimable corpus; should still show ~20)
3. Bead body for the next target via `br show <id>` — only consult the
   docs the bead cites.

## Blocking question for user
None.
