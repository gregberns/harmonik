<!-- PP-TRIAL:v6 2026-05-07 main -->

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

Three clean pipelined chains last session (5 + 6 + 6 = 17 beads, no conflicts).

Typed-alias deferral (recurring decision). When a record references a type
not yet in `core/` (e.g., WorkspaceRef, PolicyExpression, EventType):
default to `*string`/`string` placeholder + godoc citing the spec section +
`br create` follow-up bead for the typed wrapper. Only insert a prerequisite
bead when the dependent type is unambiguously small (<10 values) AND a
single existing parent bead can carry the whole enum in one shot. Don't
escalate to the user — the future hoist from `string` to a typed alias is
non-breaking. **Hoist follow-up beads should mutate the consumer record's
field type and Valid() guard in the same commit; godoc deferral notes get
removed at hoist time.** Done in prior session for hk-b3f.90/.91/.92 (Run
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
Clean. Main at `37b93da` and pushed. **Eight commits this session: 6-pack
chain (sensors + enum), one inline-amend fixup, foundation WM fixture solo.**
All HEREDOC subjects clean.

## What landed (in commit order)

1. `9505a39` BI-003 `br serve` forbidden sensor (`hk-872.3`)
2. `c399766` PL DaemonStatus enum (`hk-8mup.49`)
3. `b47d8e6` BI-001 Beads-fork-adoption sensor (`hk-872.1`)
4. `301495a` BI-009 atomic-claim invariant sensor (`hk-872.9`)
5. `7f6193f` BI-022 git-authoritative-completion sensor (`hk-872.23`)
6. `8ba189a` EV-002b handler-event-id-daemon-route sensor (`hk-hqwn.4`)
7. `1a5cd98` post-merge fixup (repoRoot dedup + gosec nolint + gofumpt)
8. `37b93da` WM-001..004 worktree-primitive foundation fixture (`hk-8mwo.65`)

## Notes from this session

- **Same-package shared-symbol collision in parallel batches.** Two implementers
  in batch 1 each defined `func repoRoot(t *testing.T) string` in
  `internal/core/` package — invisible to each reviewer (each saw only their
  own worktree); collision surfaces only post-merge as a `redeclared in this
  block` vet error. Caught by the next bead's `make check`. Cost: one inline
  fixup commit.
  - **Mitigation for future parallel batches:** when 2+ beads in one slice add
    new helpers to the same Go package, brief one of them to define the helper
    and the others to consume it (cite the originating file in their brief),
    OR explicitly require the helper live in a shared `*_test.go` file from
    the start. The reviewer for `hk-872.1` did flag the DRY observation as a
    MINOR — promote it to MAJOR/blocker when a sibling parallel bead is also
    queued in the same package.
- **Implementer-bypass-of-worktree.** `hk-872.3` implementer wrote
  `internal/core/brserve_bi003_test.go` directly into the main repo dir
  (committed at `9505a39` on main, not in its assigned worktree). Result was
  correct, but it desynchronized the merge-dance and the reviewer had to
  review post-hoc on main HEAD. **Brief mitigation:** add to implementer
  briefs an explicit instruction "ALL file edits must occur inside your
  worktree path; do NOT cd to or write into `/Users/gb/github/harmonik/`
  directly." The Agent-tool `isolation: worktree` parameter gives the agent
  a worktree, but doesn't prevent it from writing elsewhere if it tries.
- **Three-chain rhythm holds.** Pre-rebase + per-pair `rebase main` + single
  push at end + cleanup loop + batched `br close`. No merge conflicts on the
  5-branch chain.
- **Stuck-blocks-on-closed-deps still reflexive.** Three deps converted at
  audit time on the 6-pack: `hk-872.9→.45`, `hk-872.23→.19`, `hk-hqwn.4→.3`.
- **Foundation WM fixture established.** `internal/workspace/` package now
  exists with shared helpers in `testfixture_test.go` (`tempRepo`,
  `runIDValid`, `runIDRegex`, `canonicalWorktreePath`,
  `classifyCrashEvidence` placeholder). Downstream fixtures
  `.66/.67/.68/.70` reuse these helpers in-package — no import needed.
- **gosec G304 on os.ReadFile.** Spec/file-content sensors using
  `os.ReadFile(constructedPath)` need `//nolint:gosec // G304: path is
  constructed from runtime.Caller + known relative segments, not user input`.
  Pattern already present in `gitauthoritative_bi022_test.go:55` and
  `atomicclaim_bi009_test.go:59`. Add it to NEW spec-content sensor briefs.

## Ready candidates — claim a batch

`br ready -l scope:bootstrap` returns 19 (6 closed this session, 5 newly
unblocked appeared earlier). Most remaining work is "substantive."

**Top parallel batch (workspace-model fixtures, mirror `hk-8mwo.65`):**
- `hk-8mwo.66` Branch-naming + ref-safe substitution (WM-005..009 + WM-005a/.6a)
- `hk-8mwo.67` Lease-lifecycle + crash-recovery (WM-010..013e)
- `hk-8mwo.68` Merge-back + scratch-worktree (WM-018..021 + WM-018a/.19a)
- `hk-8mwo.70` Session-log + atomic sidecar (WM-025..030)

These four MUST add new tests to the same `internal/workspace/` package
where `.65` already lives. **Risk:** any test-helper any of them adds will
collide with siblings (see "Same-package shared-symbol collision" note
above). **Brief mitigation: instruct each implementer to consume the helpers
from `testfixture_test.go` (already present) and to NOT add new package-level
test helpers; if a new helper is unavoidable, use a distinctive prefix
(`leaseSetup_`, `branchNameFixture_`, etc.) and surface it in the commit
bullet.** Dispatch in parallel only with that brief mitigation.

**Process-lifecycle fixtures (also parallelizable, separate package
`internal/lifecycle/` or wherever PL chooses to land):**
- `hk-8mup.50` Pidfile + socket twin (PL-001..003b + PL-INV-001/004)
- `hk-8mup.51` Startup + orphan-sweep harness (PL-005, .006, .006a, .007, .008a, INV-003/005)
- `hk-8mup.52` Ready-state + degraded scenario (PL-009..010)
- `hk-8mup.53` Shutdown + drain scenario (PL-011..013)
- `hk-8mup.54` Agent supervision + spawn discipline (PL-014..017)
- `hk-8mup.59` CLI command surface (PL-028)

PL package doesn't yet exist — first PL fixture will establish the
foundation harness (mirror the `hk-8mwo.65` pattern). Don't dispatch the
first PL fixture in parallel with the others.

**Substantive (solo briefs):**
- `hk-872.27` br-CLI adapter (subsystem implementation — likely big)
- `hk-hqwn.41` Events tagged-union envelope + payload-constructor registry
  (will touch `internal/core/event*.go`; coordinate with WM/PL fixture work
  to avoid conflicts)
- `hk-8i31.76` HC error sentinel set + 5-class detection-and-emission table
- `hk-8mwo.24` Workspace state machine (will likely add non-test code under
  `internal/workspace/`; coordinate with WM-fixture batch)
- `hk-8mwo.64` WM error taxonomy (12-class)
- `hk-b3f.87` Crash-recovery scenario harness, `hk-b3f.88` workflow-validator
  fixture corpus

## Suggested first move

1. Audit deps on `hk-8mwo.66/.67/.68/.70` (expect ≥1 stuck-blocks-on-closed
   conversion).
2. Dispatch all four in parallel with the **same-package mitigation** in each
   brief: "Use the helpers in `internal/workspace/testfixture_test.go`; do
   NOT add new package-level test helpers without a prefix; cite
   `hk-8mwo.65` as the foundation."
3. Then dispatch `hk-8mup.50` SOLO as the PL foundation fixture (mirror the
   solo-then-parallelize pattern that worked for WM).
4. After PL-50 lands and establishes its harness, parallelize
   `hk-8mup.51/.52/.53/.54/.59`.

## Files to open first

1. `git log --oneline -10` — this session's 8 commits
2. `internal/workspace/testfixture_test.go` — WM foundation harness (helpers
   to reuse in `.66/.67/.68/.70`)
3. `internal/workspace/worktreeprimitive_wm001_test.go` — WM filesystem-scenario
   test pattern
4. `internal/core/failureclass.go` + `internal/core/daemonstatus.go` —
   strict-closed enum mirror (for any new enum bead)
5. `internal/core/atomicclaim_bi009_test.go` + `gitauthoritative_bi022_test.go`
   — Shape-B (spec-content) sensor pattern with `//nolint:gosec` boilerplate
6. `internal/core/handlereventidroute_ev002b_test.go` — multi-shape sensor
   (A: import boundary walk, B: AST godoc check, C: spec-content)

## Blocking question for user
None.
