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
Clean. Main at `b0c999e` and pushed. Bootstrap build/test scaffolding
cluster complete: `hk-pvcs.{1..7}` all closed (Makefile, .golangci.yml,
lefthook.yml + commit-msg validator, coverage-gate.sh + baseline,
internal/testhelpers + test/{scenario,integration,crash} build-tagged
stubs, tools/forbid-import). `hk-pvcs.8` (BUILDING.md) remains open and
is the natural close-out for the `hk-pvcs` epic. `hk-pvcs.9` is open at
P3 — consolidated doc-cleanup follow-ups (Trivial: true bypass keyword
in build-practices.md, agent-review timeout strategy, .lefthook.yml
typo, tools/forbid-import path stale in quality-checks.md) — defer
unless touched.

20 ready in `br ready -l scope:bootstrap`; substantially more once the
parent-epic quirk filters lift after `hk-pvcs` closes (file pvcs.8 to
land that).

## Next step — bulk work
Process is now nailed. Two-batch shakeout produced 9 durable refinements
encoded in the directives above (precedence clause, tier discipline,
dep-edge audit, merge-cd discipline, inline-amend scope, etc.). Switch
to ~8-concurrent and start eating the ready corpus.

Suggested ordering:
1. `hk-pvcs.8` (BUILDING.md) on its own — short, closes the parent epic
   and unlocks downstream filtering. Can run alongside the next batch.
2. Then dispatch from `br ready -l scope:bootstrap` in priority order.
   The next clusters are typed-aliases / enums under `hk-b3f.*` and
   `hk-872.*` (~mechanical: small files defining typed identifiers and
   enums per spec §6.1); those are good candidates for a high-concurrency
   sweep with light review.

Briefs: reuse the implementer + reviewer templates from this session
(see git history for `worktree-agent-*` commits and the closure notes
on `hk-pvcs.{2..7}`). Encode the two reviewer clauses verbatim every
time.

## If something changes
- BLOCKER persisting through 4 review rounds → tag `needs-clarification`,
  move on. Do NOT block on the user.
- `br ready -l scope:bootstrap` returns 0 → claim from open epic
  children directly via `br show <id>` (parent-child filter quirk;
  not a real blocker).
- Context past ~80% → write fresh HANDOFF, `/clear`, `/session-resume`.

## Files to open first
1. `git log --oneline -10` (recent state)
2. `br ready -l scope:bootstrap` (claimable corpus)
3. Bead body for the next target via `br show <id>` — only consult the
   docs the bead cites; do not pre-read the foundation tree.

## Blocking question for user
None.
