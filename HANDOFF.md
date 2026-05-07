<!-- PP-TRIAL:v4 2026-05-07 main -->

<!-- ORCHESTRATION DIRECTIVES — DO NOT EDIT. Loaded every /session-resume. -->
Act as the orchestrator. Delegate substantively; keep main thread small.

Claim from `br ready` (type=task; scope:bootstrap first). Spawn implementers
(model: sonnet, effort: high) with `isolation: worktree`, run_in_background.

Run as many concurrent agents as the work can absorb — 6-8 is a midpoint
reference, not a ceiling. Scale up when the shape is familiar (records,
typed aliases, enums); only throttle down when there's a specific reason
(e.g., a real merge-conflict risk on the same file). When one finishes,
immediately spawn the next ready bead — don't drain the batch before
refilling. That's the bottleneck.

Before claiming a parallel batch: audit dep edges. Sibling-artifact beads with
content-only `blocks` deps (i.e., one references the other at runtime, not
at authoring time) must have those edges converted: `br dep remove <a> <b>`
then `br dep add <a> <b> --type related`. Without this, `br update --status
in_progress` refuses the claim. NOTE: this also fires when `blocks` deps point
at already-closed type beads (e.g., a sensor that references a shipped enum).
Convert and retry — don't escalate. Two recurrences this session.

Same-file sibling conflict: when 3+ beads in one ready slice all mutate the
SAME file (e.g., three typed-alias hoists each updating one field of run.go),
do NOT dispatch them in parallel. Combine into ONE implementer with a brief
that lists all the bead IDs and instructs three sequential commits (one per
bead, each carrying its own `Refs:`). The pre-rebase + cumulative-rebase chain
during the merge dance handles them as a unit. (Done this session for
hk-b3f.90/.91/.92 — landed clean as 3 separate commits.)

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

Reviewer brief SHOULD also include: verify `git show <sha> --format='%s'`
returns ONLY the subject (not bullets-collapsed-into-subject). The Phase-0
implementer briefs that did NOT use a HEREDOC produced 5 commits with the
defect (78c8ebf..f5c131a). The HEREDOC-mandating brief in Phase-1
produced 7 clean commits. Always require the HEREDOC pattern for new
implementer briefs (template below).

Path-discrepancy resolution: when a bead body and a referenced doc disagree
on a path or identifier, the bead body wins. Implementer patches the
inconsistent file in the same commit and surfaces the still-stale doc to a
follow-up bead. EXCEPTION — for *spec content* (e.g., enum values, regex
shapes, RECORD field-types), the spec wins per CLAUDE.md ("specs are
normative"); bead body gets the follow-up note. **Special pattern: ID
records.** A spec §6.1 RECORD that says `value: String` is the type contract;
wire-fill conventions stated elsewhere (e.g., "handler fills with UUIDv7" in
§8.3) are NOT type constraints. SessionID is `type SessionID string`, not
`type SessionID uuid.UUID`. Don't assume UUID-backed for opaque-handler IDs.

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

Pipelined merges. When 3+ APPROVE-CLEAN reviews come back close together,
chain the merges in invocation 2 instead of one Bash call per bead. CRITICAL
correction to the prior pattern: after the first FF merge, local `main` has
moved past `origin/main`, so subsequent branches won't FF unless they're
ALSO rebased on the new local `main`. Pre-rebasing every branch on
`origin/main` is a precondition; the chain itself MUST insert a
`rebase main` between each pair:

    cd /Users/gb/github/harmonik
    git fetch origin main
    git merge --ff-only <B1>
    git -C <wt2> rebase main && git merge --ff-only <B2>
    git -C <wt3> rebase main && git merge --ff-only <B3>
    git -C <wt4> rebase main && git merge --ff-only <B4>
    git -C <wt5> rebase main && git merge --ff-only <B5>
    git push origin main         # one push for the whole chain
    # then cleanup each in series:
    for B in <B1> <B2> <B3> <B4> <B5>; do
        git worktree unlock ".claude/worktrees/${B#worktree-}" 2>/dev/null
        git worktree remove --force ".claude/worktrees/${B#worktree-}"
        git branch -d "$B"
    done
    # then close each bead with its closure note.

Two clean 5-chain merges this session under this pattern.

Typed-alias deferral (recurring decision). When a record references a type
not yet in `core/` (e.g., WorkspaceRef, PolicyExpression, EventType):
default to `*string`/`string` placeholder + godoc citing the spec section +
`br create` follow-up bead for the typed wrapper. Only insert a prerequisite
bead when the dependent type is unambiguously small (<10 values) AND a
single existing parent bead can carry the whole enum in one shot. Don't
escalate to the user — the future hoist from `string` to a typed alias is
non-breaking. **Hoist follow-up beads should mutate the consumer record's
field type and Valid() guard in the same commit; godoc deferral notes get
removed at hoist time.** Done this session for hk-b3f.90/.91/.92 (Run
WorkflowID/WorkflowVersion/Input) and hk-hqwn.64 (Event EventID).

Implementer commit-format template (HEREDOC; required in every brief):

    git commit -m "$(cat <<'EOF'
    feat(<scope>): <subject ≤72 chars>

    - <file>: <one-line bullet>
    - <file>: <one-line bullet>

    Refs: <bead-id>
    EOF
    )"

The HEREDOC's `'EOF'` (quoted) prevents shell expansion. Verify with
`git show HEAD --format='%s'` — output should be ONLY the subject. Without
the HEREDOC, implementers passed the multi-line markdown brief as a single
`-m` arg and produced subject-line collapses (5 commits in batch 1).

On resume: continue working unless the handoff body flags a real blocker.
Context budget: keep dispatching below ~200k tokens. When you cross ~200k,
finish the in-flight batch cleanly (don't refill mid-batch), then write a
fresh HANDOFF and stop. Don't write a HANDOFF earlier than that on a "session
feels long" hunch — the goal is high throughput per session, not pristine
context.
<!-- END DIRECTIVES -->

# Session Handoff

## State
Clean. Main at `f488d0c` and pushed. **Twelve beads landed this session
across two clean 5-chain pipelined merges:**

- Batch 1 (records / enums / sentinels):
  - `hk-b3f.75` Run record (9 fields)
  - `hk-hqwn.53` Event envelope (10 fields)
  - `hk-8i31.24` HC-020 sentinels (new package `internal/handler/`)
  - `hk-b3f.85` Trailer registry (8 entries)
  - `hk-b3f.86` Failure-class taxonomy (6-value closed enum)

- Batch 2 (typed-alias hoists + records + sensor):
  - `hk-hqwn.64` EventID typed alias (Event.EventID hoisted)
  - `hk-b3f.90` WorkflowID typed alias (Run.WorkflowID hoisted)
  - `hk-b3f.91` WorkflowVersion typed alias (Run.WorkflowVersion hoisted)
  - `hk-b3f.92` WorkspaceRef typed alias (Run.Input hoisted)
  - `hk-8i31.75` SessionID record (string-backed)
  - `hk-8mwo.60` LeaseLockFile record (4 fields)
  - `hk-872.7` HarmonikWriteStatus/CoarseStatus subset adapter (sensor)

All commits in batch 2 follow the HEREDOC commit-format pattern; subject
lines are clean. Batch 1 commits (78c8ebf..f5c131a) carry the
collapsed-subject defect — cosmetic, force-push to fix is forbidden.

## Notes from this session

- **Pipelined-chain merge pattern is now correctly documented in the
  directives block.** Two 5-chain runs, no conflicts.
- **HEREDOC commit-format pattern works; mandate it in every brief.**
  Pre-batch-2 implementers collapsed bullets into the subject. Post-fix:
  7 of 7 commits in batch 2 came out clean.
- **Stuck `blocks`-on-closed-deps fired again on hk-872.7.** Conversion
  to `related` still works. Pattern likely recurs for sensor/invariant
  beads referencing closed type beads.
- **Same-file sibling conflict (3 beads on run.go) handled by combining
  into one implementer.** Three sequential commits, each its own
  `Refs:`. Worked clean. Pattern documented in directives.
- **§6.1 RECORD type contract overrides §8.x wire-fill conventions.**
  SessionID landed as `type SessionID string` because §6.1 RECORD says
  `value: String`. The §8.3 "handler fills with UUIDv7" wording is a
  wire-fill convention, not a type constraint.
- **Orchestrator inline-amend used twice.** Run record (3 godoc nits:
  bead-ID citations + EM-012 cross-ref correction) and Trailer registry
  (1 godoc disambiguation). Single-line edits + `git commit --amend
  --no-edit` + rebase + merge.
- **Stale CLAUDE.md text — non-blocking, user-touch.** Both root and
  worktree CLAUDE.md still say *"Don't write code yet. Phase 0
  (plan refinement → spec drafting) is active."* Implementation is
  clearly active (12 beads landed in this session alone). Update is a
  user-touch follow-up: orchestrator should NOT silent-fix CLAUDE.md
  without surfacing — the prohibition reads like a load-bearing
  authorization check.

## Ready candidates — claim a batch, don't over-plan

`br ready -l scope:bootstrap` returned 20 entries at session end. New
since last session: `hk-8mwo.61` (WorkspaceState enum), `hk-8mwo.62`
(InterruptState enum), `hk-8mwo.64` (WM error taxonomy 12-class).
`hk-8i31.76` (HC error sentinel detection-and-emission table) is now
unblocked since `hk-b3f.86` closed.

**Records / enums (clean record-shape; closest match to last batches):**

- `hk-8mwo.61` WorkspaceState enum (§6.1) — string enum; mirror
  `coarsestatus.go`. Quick.
- `hk-8mwo.62` InterruptState enum (§6.1) — string enum; mirror
  `coarsestatus.go`. Quick.
- `hk-8i31.74` LaunchSpec record (§6.1) — read shape via `br show`.

**Substantive (claim solo or small batch — bigger briefs):**

- `hk-8mwo.64` WM error taxonomy — 12-class typed sentinel set. Decide
  package layout: extend `internal/handler/errors.go` or create
  `internal/workspace/errors.go`. Read shape first.
- `hk-8i31.76` HC error sentinel detection-and-emission table — the 5
  primary + 2 sub sentinels exist (hk-8i31.24); this adds the §8.1-§8.7
  detection rules per class. Likely a data-table mapping
  observable-condition → sentinel, OR actual classifier code. Inspect
  before briefing.
- `hk-872.27` br-CLI adapter — substantive subsystem implementation.
- `hk-b3f.87` Crash-recovery scenario harness — test infrastructure.
- `hk-b3f.88` Workflow-validator unit-test fixture (canonical malformed-
  DOT corpus).
- `hk-hqwn.41` Tagged-union event registry — needs `EventType` enum
  (deferred follow-up filed at `hk-hqwn.59.82` for the ~80 EventType
  rows). Effectively blocked until that lands.

**Interface-shape (different shape — brief carefully, NOT records):**

- `hk-8i31.71` Handler interface (§6.1)
- `hk-8i31.72` Session interface (§6.1)
- `hk-8i31.73` Adapter interface (§6.1)

**Sensor / invariant (different shape — read first; brief differently
from records; expect stuck-blocks-deps conversions):**

- `hk-872.8` BeadIDs opaque — discipline rule for adapter. Doc
  codification or compile-time test asserting BeadID has no parsing
  helpers.
- `hk-872.18` Run metadata records bead_id — sensor confirming
  Run.BeadID usage. Test fixture exercising bead-tied vs.
  non-bead-tied runs.
- `hk-b3f.14` many-runs-per-bead — invariant; test fixture for once
  lifecycle code lands.
- `hk-b3f.2` Edge directed transition — sensor for Edge type.
- `hk-b3f.24` Transition record discoverable by git-show — sensor for
  EM-019.
- `hk-hqwn.2` UUIDv7 invariant — sensor.
- `hk-hqwn.26` Idempotent emission — sensor.
- `hk-8i31.49` Repo-relative subprocess launch path — sensor.

**Discipline rules (low priority; doc/test-only):**

- `hk-872.1` Adopt Beads SQLite fork; reject Dolt — doc / test.
- `hk-872.3` Forbid `br serve` — discipline; likely a doc note.
- `hk-872.6` Beads owns typed dependency edges — discipline.
- `hk-872.9` Atomic-claim semantics — sensor against br adapter.

## Suggested first move

Spawn 6-8 implementers from the top of the ready queue, biased toward
clean records / enums:

1. `hk-8mwo.61` WorkspaceState enum (mirror coarsestatus.go)
2. `hk-8mwo.62` InterruptState enum (mirror coarsestatus.go)
3. `hk-8i31.74` LaunchSpec record (read shape via `br show`)
4. `hk-872.8` BeadIDs opaque sensor (small; doc-codification or
   compile-time test)
5. `hk-872.18` Run metadata records bead_id sensor
6. `hk-b3f.14` Many-runs-per-bead sensor

Audit dep edges before claiming. Expect at least one stuck-blocks-on-
closed-deps conversion (recurring this session). For larger items
(`hk-8mwo.64`, `hk-8i31.76`, `hk-872.27`), claim solo with deliberate
briefs — the record-pattern template won't fit.

## Files to open first

1. `git log --oneline -15` — 7 batch-2 commits + 5 batch-1 record commits
2. `internal/core/eventid.go`, `workflowid.go` — UUID-backed typed-alias
   pattern (mirror `runid.go`)
3. `internal/core/sessionid.go` — string-backed typed-alias pattern
   (mirror `beadid.go`); §6.1 RECORD says String even when wire-fill is
   UUIDv7
4. `internal/core/leaselockfile.go` — multi-field record with
   int + time.Time + RunID composing
5. `internal/core/harmonikwritestatus.go` — read the subset-adapter
   pattern for future write/read-split sensors; subset-invariant test
   shape catches enum drift
6. `internal/handler/errors.go` — handler-package sentinel pattern; DO
   NOT redefine; `hk-8i31.76`'s detection table likely extends this
7. `ls internal/core/` — full inventory of typed wrappers, enums,
   records already shipped
8. Bead body via `br show <id>` — only consult docs the bead cites

## Blocking question for user
None. Standing rules + orchestrator authority resolve everything.

User-touch follow-up (non-blocking): update root CLAUDE.md
*"Don't write code yet. Phase 0..."* text. Implementation is active per
the orchestrator directives in this HANDOFF; the prohibition reads like
a load-bearing authorization check, so an orchestrator silent-fix would
be the wrong move.
