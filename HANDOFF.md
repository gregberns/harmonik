<!-- PP-TRIAL:v3 2026-05-07 main -->

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

Pipelined merges. When 3+ APPROVE-CLEAN reviews come back close together,
chain the merges in invocation 2 instead of one Bash call per bead:

    cd /Users/gb/github/harmonik
    git fetch origin main
    git merge --ff-only <branch-1> && git merge --ff-only <branch-2> && \
      git merge --ff-only <branch-3>
    git push origin main         # one push for the whole chain
    # then cleanup each in series:
    for B in <branch-1> <branch-2> <branch-3>; do
        git worktree unlock "$WORKTREE_FOR_$B" 2>/dev/null
        git worktree remove --force "$WORKTREE_FOR_$B"
        git branch -d "$B"
    done
    # then close each bead:
    br close <bead-1> -r "..."
    br close <bead-2> -r "..."
    br close <bead-3> -r "..."

Saves a round-trip per bead. Each branch must rebase clean against
`origin/main` first (invocation 1 per branch); only chain the FF merges
after all rebases land.

Typed-alias deferral (recurring decision). When a record references a type
not yet in `core/` (e.g., WorkspaceRef, PolicyExpression, EventType):
default to `*string`/`string` placeholder + godoc citing the spec section +
`br create` follow-up bead for the typed wrapper. Only insert a prerequisite
bead when the dependent type is unambiguously small (<10 values) AND a
single existing parent bead can carry the whole enum in one shot. Don't
escalate to the user — the future hoist from `string` to a typed alias is
non-breaking.

On resume: continue working unless the handoff body flags a real blocker.
Context budget: keep dispatching below ~200k tokens. When you cross ~200k,
finish the in-flight batch cleanly (don't refill mid-batch), then write a
fresh HANDOFF and stop. Don't write a HANDOFF earlier than that on a "session
feels long" hunch — the goal is high throughput per session, not pristine
context.
<!-- END DIRECTIVES -->

# Session Handoff

## State
Clean. Main at `2ecdcc0` and pushed. **Two consecutive clean 3-batches**
(DependencyEdge/Edge/State, then Checkpoint/TraceContext/BeadRecord) with
effectively zero fix work — only one trivial inline orchestrator amend
(godoc text on Checkpoint). **Process is stable; scale up batch size.**
Prior sessions ran 3-at-a-time as an early-process carve-out; that's retired.
Default to 6-8+ concurrent agents now per the directives.

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

## Ready candidates — claim a batch, don't over-plan

`br ready -l scope:bootstrap` returned 20 entries at session end. The
typed-alias deferral rule in the directives resolves the "what about
EventType / WorkspaceRef" overhang automatically — apply `*string`
placeholder + godoc + follow-up `br create`, don't escalate.

**Records (closest match for the State/Edge/Checkpoint pattern):**

- `hk-b3f.75` Run — 8 fields. `input` is WorkspaceRef → `*string` deferral
  (workspace-model §4.1). No new file in `core/` for WorkspaceRef yet.
- `hk-hqwn.53` Event envelope — 10 fields. `type` is EventType (no enum
  in `core/` yet — defer to `string` per standing rule; `payload` is
  `json.RawMessage`). EventType is large (~80 event rows under parent
  `hk-hqwn.59`); deferring is the right call.

**Primitive-shape (different shape, pattern-friendly):**

- `hk-8i31.24` Five sentinel error classes (HC-020) — `var ErrTransient
  = errors.New(...)` etc., with two `%w`-wrapped structural sub-sentinels.
  Tests cover `errors.Is` dispatch.
- `hk-b3f.85` Checkpoint-commit trailer registry (§6.2) — 7-key constant
  table; trailer-lint discriminates required/conditional/extension. Likely
  a `var trailerRegistry = map[string]TrailerSpec{...}` or set of typed
  constants.
- `hk-b3f.86` EM failure-class taxonomy (§8) — enum/constant table. Inspect
  before claiming.

**Likely sensor / invariant (read first to confirm shape):**

- `hk-872.6` Beads owns typed dependency edges
- `hk-872.7` CoarseStatus 5-value subset; reads tolerate Beads enum
- `hk-872.8` Bead IDs stable
- `hk-872.9` Atomic-claim semantics
- `hk-872.18` Run metadata records bead_id
- `hk-b3f.2` Edge is a directed transition with deterministic selection inputs
- `hk-b3f.24` Transition record discoverable by git-show
- `hk-hqwn.2` event_id MUST be a UUIDv7
- `hk-hqwn.26` Producers MUST emit events idempotently
- `hk-8i31.49` Handler subprocess launched from repo-relative path

These are likely sensor/invariant beads (not pure record types); their
implementation may be a test fixture or a sensor function rather than a
new core type. `br show <id>` to confirm before briefing — different shape
needs a different brief skeleton.

**Bigger / specialized (claim solo or as their own small batch):**

- `hk-872.27` br-CLI adapter (sole translation layer) — substantive
  subsystem implementation, not a single-file primitive.
- `hk-b3f.87` Crash-recovery scenario harness — test infrastructure.
- `hk-b3f.88` Workflow-validator unit-test fixture — canonical
  malformed-DOT corpus.

**Known blocked (don't claim):**

- `hk-b3f.77` Transition — blocked by `hk-zs0.33` (Seven roles named).
  Will unblock once the role enum lands. Composes State + OutcomeStatus +
  TransitionKind otherwise.

## Suggested first move

Spawn 6-8 implementer agents in parallel from the **Records** + **Primitive-shape**
sections above. The two Records (Run, Event envelope) and the three Primitive-shape
beads (Sentinels, Trailer registry, Failure-class taxonomy) are five clean candidates
with no sibling blocks between them. Pick a 6th-8th from the **Likely sensor**
group after confirming shape via `br show`.

Audit deps before claiming. None of the listed candidates should sibling-block
each other within a batch, but verify with `br show` — if a `blocks` edge
between two batch members is content-only (runtime reference, not authoring),
convert it: `br dep remove a b && br dep add a b --type related`.

## Files to open first
1. `git log --oneline -12` — six record commits + prior enum/typed-alias batches
2. `internal/core/checkpoint.go`, `beadrecord.go`, `tracecontext.go` — latest record-shape patterns
3. `internal/core/edge.go` — **PolicyExpression deferral pattern** (the template for WorkspaceRef on Run, EventType on Event envelope, and any future "type not in core/ yet" record)
4. `internal/core/state.go` + `state_test.go` — the canonical multi-field record + Valid() pattern
5. `ls internal/core/` — full inventory of typed wrappers + enums already shipped (don't redefine)
6. Bead body via `br show <id>` — only consult docs the bead cites

## Blocking question for user
None. Standing rules resolve the typed-alias deferral question; orchestrator
authority covers MEDIUM tier-overrides; context budget governs when to hand off.
