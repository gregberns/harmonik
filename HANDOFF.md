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
Convert and retry — don't escalate.

Same-file sibling conflict: when 3+ beads in one ready slice all mutate the
SAME file, do NOT dispatch them in parallel. Combine into ONE implementer
with a brief that lists all the bead IDs and instructs sequential commits
(one per bead, each carrying its own `Refs:`).

Each implementation gets reviewed (model: sonnet, effort: high). Iterate up
to 4 rounds; stop when no BLOCKER/MAJOR/MEDIUM findings remain. If still
open at round 4, tag `needs-clarification` and move on.

Reviewer brief MUST encode two clauses every time, verbatim:
1. PRECEDENCE — orchestrator directives override `build-practices.md` for
   implementer commits. Expected commit format: subject + 1-line file
   bullets + `Refs:`. Do NOT flag absence of `## Why / ## What / ## Spec
   alignment / ## Test plan / ## Risk` sections, or `Reviewed-By:` /
   `Review-Verdict:` trailers.
2. TIER DISCIPLINE — MEDIUM = defect against THIS bead's acceptance
   criteria. Cross-cutting / future-bead / spec-doc concerns = MINOR or
   follow-up note, not MEDIUM.

Reviewer brief SHOULD also include: verify `git show <sha> --format='%s'`
returns ONLY the subject (not bullets-collapsed-into-subject). Always
require the HEREDOC pattern for new implementer briefs.

Path-discrepancy resolution: when a bead body and a referenced doc disagree
on a path or identifier, the bead body wins. Implementer patches the
inconsistent file in the same commit and surfaces the still-stale doc to a
follow-up bead. EXCEPTION — for *spec content* (e.g., enum values, regex
shapes, RECORD field-types), the spec wins per CLAUDE.md ("specs are
normative"); bead body gets the follow-up note.

Orchestrator authority: if reviewer flags MEDIUM but implementation clearly
meets bead acceptance, you may merge with a closure note explaining the
tier-override and (optionally) filing a follow-up bead. Don't mechanically
iterate on cross-cutting noise.

Inline-amend by orchestrator (no fix-agent) for: trivial single-line text
fixes; literal one-line code fixes (e.g., flipping a comparison operator,
fixing a single off-by-one); **mechanical multi-line refactors verifiable
by reading** (e.g., search-and-replace renames, drop-an-unused-import,
substitute strings.Contains for hand-rolled helper). This session: 5 of 6
fix iterations were inline amends (`.51 .53 .59` plus `.66/.67/.68/.70`
sibling fix-agents that did the same thing internally). Amends are
faster than fix-agents when the fix doesn't need judgment. Re-review may
also be skipped after a metadata-only fix.

When ambiguity arises, spend real effort resolving without escalation.
Bead acceptance criteria is authoritative.

Merge dance — RUN FROM THE MAIN REPO DIR, NOT THE WORKTREE. Split into TWO
Bash invocations so the cwd doesn't leak across the boundary:

    # invocation 1 (cwd = worktree):
    cd <worktree-path> && git fetch origin main && git rebase origin/main

    # invocation 2 (cwd = main repo, freshly cd'd):
    cd /Users/gb/github/harmonik && git merge --ff-only <branch>
    git push origin main
    git worktree unlock <worktree-path>
    git worktree remove --force <worktree-path>   # --force needed: symlinks
    git branch -d <branch>
    br close <bead-id> -r "<closure note>"

Use `br close <id> -r "..."` for the closure note. `br update <id> -c "..."`
does NOT exist (no `-c` flag).

After each bead merges, leave no stale worktrees under `.claude/worktrees/`.

Pipelined merges. When 3+ APPROVE-CLEAN reviews come back close together,
chain the merges in invocation 2:

    cd /Users/gb/github/harmonik
    git fetch origin main
    git merge --ff-only <B1>
    git -C <wt2> rebase main && git merge --ff-only <B2>
    git -C <wt3> rebase main && git merge --ff-only <B3>
    git push origin main         # one push for the whole chain
    # then cleanup each in series + close beads.

Inline fix-iteration on existing worktrees (no new isolation): when a review
returns REQUEST-CHANGES with mechanical findings, spawn a fix-agent WITHOUT
`isolation: worktree` — point it at the existing worktree path from the
implementer run. The agent cd's into the existing branch, applies fixes,
creates a NEW commit (per directive: never amend pre-merge commits unless
user asks). Two-commit branches FF-merge cleanly.

Typed-alias deferral (recurring decision). When a record references a type
not yet in `core/`: default to `*string`/`string` placeholder + godoc citing
the spec section + `br create` follow-up bead for the typed wrapper. Only
insert a prerequisite bead when the dependent type is unambiguously small
(<10 values) AND a single existing parent bead can carry it. Hoist
follow-up beads should mutate the consumer's field type and Valid() guard
in the same commit; godoc deferral notes get removed at hoist time.

Implementer commit-format template (HEREDOC; required in every brief):

    git commit -m "$(cat <<'EOF'
    feat(<scope>): <subject ≤72 chars>

    - <file>: <one-line bullet>
    - <file>: <one-line bullet>

    Refs: <bead-id>
    EOF
    )"

The HEREDOC's `'EOF'` (quoted) prevents shell expansion. Verify with
`git show HEAD --format='%s'` — output should be ONLY the subject.

LINT COMPLIANCE — bake into every implementer brief from the start.
The project's `.golangci.yml` enforces (and PL-50/WM-batch fix iterations
this session established the patterns):

1. **camelCase, no underscores** (revive var-naming). Helper prefix is
   `<beadOrConcept>Fixture<FuncName>` — e.g., `leaseFixtureWriteLockAtomic`,
   NOT `leaseFixture_writeLockAtomic`. The underscore form was the
   convention from a prior session — IT IS WRONG, do not propagate it.
2. **`exec.Command` is forbidden** (noctx): always
   `exec.CommandContext(t.Context(), ...)`.
3. **`net.Listen` / `net.Dial`** use context-aware forms:
   `(&net.ListenConfig{}).Listen(t.Context(), "unix", path)` and
   `(&net.Dialer{}).DialContext(t.Context(), "unix", path)`.
4. **`panic` is forbidden in helpers** (forbidigo): helpers must take
   `*testing.T` and call `t.Fatalf`.
5. **`os.ReadFile` / `os.Open` on constructed paths**: add
   `//nolint:gosec // G304: path is constructed from t.TempDir() + known
   relative segments, not user input` immediately above.
6. **`os.MkdirAll` 0o755**: add
   `//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions`.
7. **Cleanup discards** like `defer ln.Close()` should use
   `defer func() { _ = ln.Close() }()` (errcheck-clean).
8. **`err != io.EOF`** must be `errors.Is(err, io.EOF)` (errorlint).
9. **NO `panic(...)` outside `internal/testhelpers/`**.

Pre-baking these into the PL-batch implementer briefs (`.51/.52/.53/.54/.59`)
delivered 5/5 with 0 lint issues from start — vs the WM-batch which all
needed round-2 fix iteration. Pre-baking pays. Don't skip.

Same-package shared-symbol discipline (PARALLEL DISPATCH RISK).
When N beads add tests to the same Go package, the test files must NOT
collide on package-level helpers. EVERY implementer brief in a parallel
batch must specify a per-bead helper prefix (camelCase, e.g.,
`branchNameFixture`, `leaseFixture`, `mergeBackFixture`,
`sessionLogFixture`, `plFixture`, `startupSweepFixture`, `readyFixture`,
`shutdownFixture`, `supervisionFixture`, `cliFixture`). Reviewers must
grep for unprefixed package-level symbols and flag them as MEDIUM.

On resume: continue working unless the handoff body flags a real blocker.
Context budget on this 1M-context model is generous (~700k effective);
the prior "200k" reference was for a smaller window. When you cross
~500k, finish in-flight batch cleanly, then write a fresh HANDOFF and
stop. Don't write a HANDOFF earlier on a "session feels long" hunch.
<!-- END DIRECTIVES -->

# Session Handoff

## State
Clean. Main at `905b04e` and pushed. **18 commits this session: 11 beads
landed (1 substantive Go + 4 WM fixtures + 1 PL foundation + 5 PL
fixtures), 6 fix-iteration commits, plus a metadata-only HANDOFF refresh
(this).** All HEREDOC subjects clean.

## What landed (in commit order, oldest first)

Implementations:

1. `cfed79a` `hk-hqwn.41` EV-032 event payload-constructor registry
   (`internal/core/eventregistry.go` + tests; `EventPayload` marker,
   `RegisterEventType`, `DecodePayload`, `Err{Unknown,Duplicate}EventType`)
2. `df338ce → cf72cd0` `hk-8mwo.66` WM-005..009 + WM-005a/006a branch-naming
3. `4ec5f82 → 0495f25` `hk-8mwo.68` WM-018..021 + WM-018a/019a merge-back
4. `c8110c4 → e97a03f` `hk-8mwo.67` WM-010..013 + WM-013a..e lease-lifecycle
5. `498fcf4 → c6e957d` `hk-8mup.50` PL-001..003b twin-driven foundation —
   established `internal/lifecycle/` package with `plFixture*` helpers
6. `040c99b → c3a3873` `hk-8mwo.70` WM-025..030 session-log + atomic sidecar
7. `5eec581 → b77c3ea` `hk-8mup.51` PL-005..008a startup + orphan-sweep
8. `e010809` `hk-8mup.54` PL-014..017 + PL-014a agent supervision
9. `c6250bd → e482e38` `hk-8mup.52` PL-009..010 ready-state + degraded
10. `49bb814 → 1b57c4a` `hk-8mup.59` PL-028 CLI command surface
11. `f69df9a → 905b04e` `hk-8mup.53` PL-011..013 shutdown + drain

## Notes from this session — LEARNINGS

- **Underscore prefix convention from prior session was wrong.** `<bead>Fixture_<func>`
  trips revive var-naming. Pivoted mid-session to `<bead>FixtureFunc` (camelCase, no underscore).
  Updated DIRECTIVES above to make this explicit so future briefs don't re-introduce the bug.
- **Pre-baking lint patterns into briefs eliminates round-2 fix iteration.**
  WM-batch: 4/4 needed round-2 fixes (mostly systemic). PL-batch (with
  patterns pre-baked): 5/5 reported "0 lint issues" from implementer.
  Reviewers still found 2-3 small bead-specific issues per bead (unprefixed
  package-level helper, misleading comment, atomic+mutex hybrid, dead
  assertion, runtime side-effect anchor) — all inline-amendable.
- **Inline-amend is the right pattern for mechanical multi-line refactors.**
  Renames, dropping unused imports, substituting `strings.Contains` for
  hand-rolled helper, mutex-only counters: orchestrator does these directly
  rather than spawning fix-agents. Faster and equally safe (verify by
  reading + run tests).
- **PL-50 `truncate-rewrite-keep-fd` vs `temp+rename`.** Implementer flagged
  this — the spec mandates the former because rename changes the inode and
  breaks the flock association. Important for pidfile semantics. The brief
  text said "temp+rename" (wrong); implementer correctly read the spec.
  Future PL implementer briefs should NOT mention "temp+rename" for pidfile
  contexts.
- **macOS `sun_path` 104-byte limit on `t.TempDir()`.** PL-50's
  `plFixtureTempProjectDir` falls back to `os.MkdirTemp("/tmp", ...)`
  when needed for unix-socket tests. Pattern is reusable; sibling beads
  picked it up.
- **Package growth pattern.** `internal/lifecycle/` went from 0 → 1 (foundation)
  → 6 (full PL fixture corpus) in one session. Same shape `internal/workspace/`
  followed in prior session. Future test-infra batches should follow:
  solo foundation first, then parallelize peers.
- **Fix-agent operating on existing worktree (not isolation: worktree).**
  Pattern: the implementer's worktree is still on disk + locked after their
  run. Fix-agent gets the path + branch name in its brief and operates there
  directly. Creates a NEW commit (per directive). Two-commit branches
  FF-merge cleanly.

## Ready candidates — claim a batch

`br ready -l scope:bootstrap` returns 20 (11 closed this session, several
new appeared). Top candidates:

**Substantive Go (no test-infra):**
- `hk-872.27` Single br-CLI adapter — likely the next big subsystem-impl bead
- `hk-hqwn.42` Type dispatch is deterministic on type field (EV-033) —
  builds on `hk-hqwn.41` (already closed); small focused commit
- `hk-8i31.76` HC error sentinel set + 5-class detection-and-emission table
- `hk-8mwo.24` Workspace state machine — will add non-test code under
  `internal/workspace/`
- `hk-8mwo.64` WM error taxonomy (12-class typed sentinel set)
- `hk-8mup.5` Atomic pidfile write via truncate-rewrite-keep-fd —
  consumer of PL-50's `plFixtureAcquirePidfile`; small focused
- `hk-8mup.12` Project hash + provenance marker (env var + PGID)
- `hk-sx9r.74` Exit-code taxonomy + obligations fixture (ON-001..ON-004)

**Test-infra fixtures (test-only, parallelizable):**
- `hk-b3f.87` Crash-recovery scenario harness for EM checkpoint contract
- `hk-b3f.88` Workflow-validator unit-test fixture (canonical malformed-DOT corpus)
- `hk-i0tw.14/.17/.31/.43..51` — scenario-harness records (8 candidates,
  parallelizable, mostly small RECORD definitions)

## Suggested first move

1. **Tackle `hk-hqwn.42`** (type-dispatch determinism) as a quick warm-up —
   builds directly on the just-shipped `hk-hqwn.41` registry; should be a
   single small commit that adds `ErrUnknownEventType` surfacing + tests
   for deterministic-lookup invariant. Verify it doesn't already overlap
   with `hk-hqwn.41`'s scope.

2. **Then a parallel batch of `hk-i0tw.43..51`** (RECORD definitions for
   scenario-harness) — 7-8 small RECORD beads in one parallel dispatch.
   These mirror the typed-alias-deferral pattern from prior sessions (see
   `internal/core/event.go` for the shape). Each is small enough that
   round-1 should APPROVE-CLEAN.

3. **Then `hk-872.27` br-CLI adapter** SOLO — this is likely the largest
   non-test bead in the queue and may need its own design pass. Inspect
   the bead body before dispatch.

## Files to open first

1. `git log --oneline -20` — this session's commits
2. `internal/lifecycle/testfixture_test.go` — PL foundation harness with
   `plFixture*` helpers (consumer pattern for future PL beads)
3. `internal/lifecycle/socket_pl003_test.go` — socket + ListenConfig pattern
4. `internal/core/eventregistry.go` + `eventregistry_test.go` — registry
   pattern (consumer pattern for `hk-hqwn.42`)
5. `internal/workspace/leasefixture_helpers_test.go` — file-extracted helpers
   pattern (when bead has many helpers reused across files)
6. `.golangci.yml` — full lint config (the LINT COMPLIANCE block in the
   directives above is a summary of the rules that bite tests)

## Blocking question for user
None.
