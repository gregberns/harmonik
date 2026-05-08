<!-- PP-TRIAL:v7 2026-05-07 main -->

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
Clean. Main at `ec6c535` and pushed. **9 commits this session: 8 beads
landed (1 dispatch policy + 6 scenario RECORDs + 1 brcli scaffold), 1
review-fix commit (inline-amend on `.48`).** All HEREDOC subjects clean.
One follow-up bead created (`hk-szv5`).

## What landed (in commit order, oldest first)

1. `af799c4` `hk-hqwn.42` EV-033 dispatch policy
   (`internal/core/eventdispatch.go` — `DispatchObservational` returns
   `ErrSkipUnknown`; `DispatchSynchronous` returns `*DispatchUnknownEventError`
   wrapping `ErrUnknownEventType`; deterministic-lookup test over 20 iters)
2. `5a40fed` `hk-i0tw.43` AgentOverride RECORD — also seeded the new
   `internal/scenario/` package (doc.go + foundation)
3. `ec80efa` `hk-i0tw.45` GitSeedOp RECORD + `GitSeedOpKind` enum +
   per-op required-keys table from §6.3
4. `7d36c09` `hk-i0tw.46` FileSeed RECORD + `FileSeedEncoding` enum
   (utf8/base64; mode validates octal in `[0, 0o777]`)
5. `df16692` `hk-i0tw.47` EventExpectation RECORD + `EventExpectationKind`
   enum. `Type` is `string` placeholder per typed-alias-deferral; follow-up
   hoist bead **`hk-szv5`** created
6. `9548a5e` `hk-i0tw.51` AssertionResult RECORD + `AssertionResultKind` enum;
   `ActualValue`/`ExpectedValue` typed as `any` (JSONValue)
7. `13685da` `hk-i0tw.48` WorkspacePredicate RECORD + 5-value enum + per-kind
   interpretation table (§6.3) + SH-022 path safety
8. `dfa5e42` `hk-i0tw.48` review-fix — added `UnmarshalText` to
   `WorkspacePredicateKind` (orchestrator inline-amend, ~12-line additive
   method + 39-line test table)
9. `ec6c535` `hk-872.27` br-CLI adapter scaffold (BI-025) — new
   `internal/brcli/` package: `New(brPath) (*Adapter, error)` +
   `Run(ctx, args) (Result, error)` low-level primitive. TODO stubs cite
   sibling beads for BI-024a/025a/025b/025c/025d/025e behavior (`.26/.28-.32`)

## Notes from this session — LEARNINGS

- **Harness anomaly: `isolation: worktree` silently failed on `.27`.**
  Implementer reported committing on `main` directly, not in
  `.claude/worktrees/agent-*`. The harness did not produce the
  `<worktree>` block in its completion notification (compare to `.42/.43/.45/.46/.47/.48/.51` which all DID). Mitigation: post-hoc
  reviewer pass before push; commit was on local `main` but unpushed,
  so review still gated origin. **Future protocol:** when an
  implementer's notification lacks the `<worktree>` block AND its
  report says `branch: main`, treat as a no-isolation run — review
  before `git push`, fix-up commits go on `main` directly (NOT amend),
  one push at the end. Don't block on this — verify and proceed.
- **Reviewer inconsistency: UnmarshalText on typed enums.** `.47`
  reviewer (EventExpectationKind) accepted absence (relied on Go's
  implicit string conversion); `.48` reviewer (WorkspacePredicateKind)
  flagged absence as MEDIUM. The `.48` reviewer is right: without
  `UnmarshalText`, JSON/YAML deserialisation silently accepts unknown
  enum values and only `Valid()` catches them at use time — the
  boundary is the right place to reject. **PRE-BAKE INTO BRIEFS:**
  when a bead adds a typed enum that participates in JSON/YAML, REQUIRE
  the brief to specify BOTH `MarshalText` AND `UnmarshalText` with
  explicit rejection of unknown values + a corresponding test table.
  `.45/.46/.51` pre-baked it correctly; `.47` did not. Consider a
  follow-up bead to add `UnmarshalText` to `EventExpectationKind` for
  consistency, or just patch on next touch.
- **Typed-alias-deferral pattern works cleanly for cross-spec types.**
  `EventExpectation.Type` references `EventType` (event-model.md §8) which
  doesn't yet have a Go enum. Used `string` placeholder + godoc TODO
  citing `hk-szv5` (the auto-created hoist bead). Same shape as the
  prior `hk-hqwn.59.82` TODO in `eventregistry.go`. **Implementer
  briefs MUST: (a) supply the bead-creation `br create` command in the
  brief, (b) require the implementer to substitute the returned bead
  ID into the godoc.** `.47` did this correctly.
- **Same-package parallel dispatch with per-bead helper prefix is
  proven at scale.** 5 sibling RECORD beads (`.45/.46/.47/.48/.51`)
  landed in `internal/scenario/` simultaneously. Zero collisions.
  Per-bead prefixes: `gitSeedOpFixture`, `fileSeedFixture`,
  `eventExpectationFixture`, `workspacePredicateFixture`,
  `assertionResultFixture`. Reviewers caught ZERO unprefixed
  package-level helpers. The discipline is enforceable as a brief
  contract.
- **Foundation-then-parallelize cadence still pays.** `.43` shipped
  solo to seed `internal/scenario/` (doc.go + AgentOverride). Then
  `.45/.46/.47/.48/.51` parallel-dispatched safely on top. Same shape
  as prior `internal/lifecycle/` and `internal/workspace/` package
  bootstrap. **Use this pattern for any green-field package.**
- **Inline-amend continues to outperform fix-agent for mechanical
  changes.** `.48`'s missing `UnmarshalText` was a 12-line method +
  one new test function. Orchestrator amended in main thread (~30 sec
  of edits + 1 verification test run + 1 commit) — no fix-agent spawn.
  Two-commit branches FF-merge cleanly. **Heuristic:** if the
  reviewer's MEDIUM finding describes a fix in a single sentence and
  the patch is &lt;30 lines, inline-amend; spawn a fix-agent only when
  the fix needs judgment or careful integration with surrounding code.

## Ready candidates — claim a batch

`br ready -l scope:bootstrap` returns 20 after this session's closures.
`.27` unblocked five br-CLI consumer beads. Top candidates:

**brcli consumer batch (NEW — `.27` just shipped; `internal/brcli/` exists):**
- `hk-872.15` Implement bead-detail query (`br show <id> --format json`)
  — likely smallest, single typed return, good foundation bead
- `hk-872.14` Implement dependency-graph query (`br dep`)
- `hk-872.16` Implement reconciliation queries (audit log + status)
- `hk-872.13` (already-closed in prior session? verify) — `br ready` query
- `hk-872.2` Route all Beads I/O through the br CLI (umbrella sensor;
  mostly a discipline test that ensures no scattered `os/exec br` calls)
- `hk-872.4` Daemon to br direct; agents to br via Beads-CLI skill

These all add methods to `internal/brcli/Adapter` (consume `Run`). Each
likely 1 file (`query_<name>.go`). Same-package collision risk if 3+
parallelize without distinct files; brief MUST specify per-bead filename
+ `<bead>Fixture<...>` helper prefix.

**brcli adapter-policy siblings (UNBLOCKED by `.27`):**
- `hk-872.26` `br --version` handshake (BI-024a)
- `hk-872.28` exit-code taxonomy (BI-025a) → `BrError` enum
- `hk-872.29` `--format json` mandatory (BI-025b)
- `hk-872.30` subprocess timeout discipline (BI-025c, 5s read / 10s write)
- `hk-872.31` stderr 1 MiB cap + 5 scenarios (BI-025d)
- `hk-872.32` concurrent-invocation discipline (BI-025e — no adapter
  mutex; mostly contract test)
- `hk-872.33` Beads-breakage absorption (BI-026)

(Verify which are in `br ready` — not all may be unblocked yet.)

**Substantive Go (independent of brcli):**
- `hk-8mup.5` Atomic pidfile write — consumer of PL-50's
  `plFixtureAcquirePidfile`; small focused
- `hk-8mup.12` Project hash + provenance marker
- `hk-8mwo.24` Workspace state machine — production code in
  `internal/workspace/`
- `hk-8mwo.64` WM error taxonomy (12-class typed sentinel set)
- `hk-8i31.76` HC error sentinel set + 5-class detection table
- `hk-sx9r.74` Exit-code taxonomy + obligations fixture (ON-001..004)

**Last RECORD remaining:**
- `hk-i0tw.50` ScenarioResult RECORD (11 fields). References
  `FailureClass` (scenario-harness §8 — DIFFERENT from
  `internal/core/FailureClass` for execution-model). Needs
  string-placeholder + follow-up hoist bead. References `AssertionResult`
  (already shipped this session).

**Scenario-harness operational beads (not RECORDs):**
- `hk-i0tw.14` Each scenario uses an isolated event-log directory
- `hk-i0tw.17` Per-suite ephemeral fixture root
- `hk-i0tw.31` Every scenario declares a cadence tag

**Skill beads (`.claude/skills/` markdown content, NOT Go):**
- `hk-jhob.1/2/3/4` agent-reviewer / beads-cli / agent-config-reviewer /
  go-subsystem-add skills

## Suggested first move

1. **Solo `hk-872.15` (bead-detail query)** as the brcli-consumer-pattern
   foundation. `br show <id> --format json` returns a single typed
   `BeadRecord` (already exists in `internal/core/beadrecord.go`). The
   implementer:
   - adds `Adapter.ShowBead(ctx, beadID string) (core.BeadRecord, error)`
   - parses JSON output via `Result.Stdout`
   - foundation for typed-error classification (defer to `.28`)
   - establishes the consumer-method file pattern (`query_show.go` or
     `show.go`)

2. **Then parallel batch `.14/.16` (dep-graph + reconciliation queries)**
   once `.15` lands — each its own file, each `<bead>Fixture<...>` helper
   prefix. Two beads, same package, distinct files: safe to parallelize.

3. **OR alternative: `.26/.28/.29` policy enhancements** if you'd rather
   build the adapter "correct" before adding queries on top. `.26` is
   tiny (parse `br --version` regex); `.28` is the `BrError` enum +
   classification table; `.29` is mandatory `--format json` argv
   prepending. Each refines `Run` or wraps it.

Choose by appetite: queries deliver visible end-user value faster; policy
beads close gaps in the adapter contract before more callers depend on it.

## Files to open first

1. `git log --oneline -15` — this session's commits
2. `internal/brcli/adapter.go` — `Adapter`, `Result`, `Run`; the consumer
   pattern foundation for `.13/.14/.15/.16/.2/.4`
3. `internal/brcli/adapter_test.go` — `brcliFixtureMockBinary` shell-script
   pattern for testing query implementations against a mock `br`
4. `internal/scenario/agentoverride.go` — sibling-style reference (still
   the canonical record convention)
5. `internal/core/eventdispatch.go` + `eventregistry.go` — the .42-shipped
   dispatch primitive; consumers across the codebase
6. `internal/core/beadrecord.go` — existing `BeadRecord` shape that
   `.15` will return from `ShowBead`
7. `specs/beads-integration.md` lines 306-376 — full BI-025 family for
   the brcli policy beads (`.26/.28-.33`)

## Blocking question for user
None.
