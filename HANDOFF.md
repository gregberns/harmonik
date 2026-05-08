<!-- PP-TRIAL:v8 2026-05-07 main -->

<!-- ORCHESTRATION DIRECTIVES — DO NOT EDIT. Loaded every /session-resume. -->
Act as the orchestrator. Delegate substantively; keep main thread small.

**Implementer + reviewer agents read `.claude/implementer-protocol.md` for
standing conventions** (commit format, lint rules, helper-prefix discipline,
typed-alias-deferral, reporting format, gofmt-clean discipline, run-before-
commit checks, pre-flight reading order). Do NOT duplicate that content into
briefs. Briefs orchestrate; the protocol doc instructs.

THROUGHPUT FLOOR. The bead body IS the work spec. Implementer briefs cap at
~30 lines: bead-id, worktree pointer, helper prefix, canonical-sibling
pointer (if useful), and any follow-up `br create` command for typed-alias-
deferral. **Never paraphrase the bead body into the brief.** The implementer
runs `br show <id> --format json` and the cited spec sections itself.

Active-work floor: 4 concurrent agents. Target: 6–8 across non-overlapping
packages. **Refill on review-dispatch, not review-return** — when you spawn
a reviewer, immediately spawn the next implementer if the ready queue has
non-conflicting work. Reviews and implementers run in parallel at the agent
level; the orchestrator should layer them. The "wait for foundation" rule
applies ONLY when the next bead literally cannot be briefed without the
foundation file's content; if the brief can name the canonical pattern
abstractly, dispatch in parallel.

Pre-flight investigation cap: orchestrator reads ≤3 files per dispatch (bead
body via `br show`, the cited spec section, ONE canonical sibling). Do NOT
sample real CLI output unless an exact JSON/output shape is load-bearing AND
not in the bead body. The implementer is competent; let them read.

Spawn implementers (model: sonnet, effort: high) with `isolation: worktree`,
run_in_background. Reviewers same model/effort, no isolation.

Before claiming a parallel batch: audit dep edges. Sibling-artifact beads
with content-only `blocks` deps (i.e., one references the other at runtime,
not at authoring time) must have those edges converted: `br dep remove <a>
<b>` then `br dep add <a> <b> --type related`. Without this, `br update
--status in_progress` refuses the claim. Also fires when `blocks` deps point
at already-closed type beads. Convert and retry — don't escalate.

Same-file sibling conflict: when 3+ beads in one ready slice all mutate the
SAME file, do NOT dispatch them in parallel. Combine into ONE implementer
with a brief that lists all the bead IDs and instructs sequential commits
(one per bead, each carrying its own `Refs:`).

Each implementation gets reviewed (model: sonnet, effort: high). Iterate up
to 4 rounds; stop when no BLOCKER/MAJOR/MEDIUM findings remain. If still
open at round 4, tag `needs-clarification` and move on.

Reviewer brief MUST encode TIER DISCIPLINE: MEDIUM = defect against THIS
bead's acceptance criteria. Cross-cutting / future-bead / spec-doc concerns
= MINOR or follow-up note, not MEDIUM. Reviewer brief MUST also tell the
reviewer NOT to flag absence of `## Why / ## What / ## Spec alignment / ##
Test plan / ## Risk` sections or `Reviewed-By:` / `Review-Verdict:`
trailers — those are not used in this project.

Inline-amend by orchestrator (no fix-agent) for: trivial single-line text
fixes; literal one-line code fixes; **mechanical multi-line refactors
verifiable by reading** (gofmt reflows, search-and-replace renames, drop-an-
unused-import, substitute strings.Contains for hand-rolled helper). Amends
are faster than fix-agents when the fix doesn't need judgment. Re-review
may be skipped after a metadata-only fix.

Path-discrepancy resolution: bead body wins over docs. EXCEPTION — for spec
content (enum values, regex shapes, RECORD field-types), the spec wins per
CLAUDE.md ("specs are normative"); bead body gets the follow-up note.

Orchestrator authority: if reviewer flags MEDIUM but implementation clearly
meets bead acceptance, you may merge with a closure note explaining the
tier-override and (optionally) filing a follow-up bead.

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

Pipelined merges. When 3+ APPROVE-CLEAN reviews come back close together,
chain the merges in invocation 2 and push once for the whole chain.

Inline fix-iteration on existing worktrees (no new isolation): when a review
returns REQUEST-CHANGES with mechanical findings, spawn a fix-agent WITHOUT
`isolation: worktree` — point it at the existing worktree path. The agent
cd's into the existing branch, applies fixes, creates a NEW commit (never
amend pre-merge commits unless user asks). Two-commit branches FF-merge
cleanly.

Typed-alias deferral (recurring decision). When a record references a type
not yet in `core/`: default to `*string`/`string` placeholder + godoc citing
the spec section + `br create` follow-up bead for the typed wrapper. Only
insert a prerequisite bead when the dependent type is unambiguously small
(<10 values) AND a single existing parent bead can carry it. The brief
names the `br create` command; the implementer creates the bead, captures
its ID, and substitutes it into godoc before committing.

When ambiguity arises, spend real effort resolving without escalation. Bead
acceptance criteria is authoritative.

On resume: continue working unless the handoff body flags a real blocker.
Context budget on this 1M-context model is generous (~700k effective). When
you cross ~500k, finish in-flight batch cleanly, then write a fresh HANDOFF
and stop. Don't write a HANDOFF earlier on a "session feels long" hunch.
<!-- END DIRECTIVES -->

# Session Handoff

## State
Clean. Main at `a103cf5` and pushed. **5 beads landed this session: hk-872.15
ShowBead, hk-i0tw.50 ScenarioResult, hk-872.14 ListDependencies, hk-8mup.5
AcquirePidfile, hk-872.16 AuditLog + ListInFlightBeads.** 2 inline-amend
review fixes (gofumpt struct alignment on `.15`, trailing-newline on `.5`).
2 follow-up beads filed: `hk-872.55` (EdgeKind extension for Beads's broader
dep-type surface incl. `related`), `hk-ido0` (scenario-harness FailureClass
enum hoist).

`hk-8i31.76` (HC error sentinels) shipped from worktree
`agent-ac5423283dd4787c7` at commit `25aec8a`; reviewer dispatched, **NOT
yet merged.** Pick up that thread first.

## What landed this session (in commit order)

1. `cdd6596` `hk-872.15` ShowBead query (BI-015)
2. `6ce2398` `hk-872.15` review-fix — gofumpt brShowItem alignment (orchestrator inline-amend)
3. `0a8190a` `hk-i0tw.50` ScenarioResult RECORD + ScenarioVerdict enum (last scenario RECORD)
4. `bea460f` `hk-872.14` ListDependencies query (BI-014)
5. `85a8abb` `hk-8mup.5` AcquirePidfile production API (PL-002b)
6. `367995d` `hk-8mup.5` review-fix — trailing-newline gofmt (orchestrator inline-amend)
7. `157072b` `hk-872.16` AuditLog + ListInFlightBeads (BI-016)
8. (uncommitted-on-main) `25aec8a` in worktree `agent-ac5423283dd4787c7` — `hk-8i31.76` HC sentinels (review in flight)

## NEW this session — `.claude/implementer-protocol.md`

Single canonical conventions doc. Cited by every brief; not duplicated into
them. Currently UNTRACKED (`.claude/` is not gitignored — git just hasn't
been told about it). **First action next session: `git add
.claude/implementer-protocol.md` and commit it on main.** Implementer + reviewer
briefs already reference this path; if it's missing, the agents will fail
the read.

## Throughput lessons from this session

5 beads merged but peak concurrency was only 2-3 agents. Root causes
identified and codified into the directives above:

1. **Briefs were 200-400 lines** because I paraphrased bead content. The
   bead body IS the work spec; the brief should orchestrate, not duplicate.
   New cap: ~30 lines.
2. **Sequential dispatch.** I waited for `.15` to land before dispatching
   `.14`/`.16` even though their briefs could have referenced the canonical
   pattern abstractly. New rule: dispatch on review-dispatch, not review-
   return.
3. **Orchestrator did implementer's reading.** I sampled real CLI output,
   read sibling files in full, extracted spec sub-sections — all to bake
   detail into briefs the implementer could have read itself. Pre-flight
   cap: ≤3 files per dispatch.
4. **No batch start.** I dispatched 1-2 agents and waited. New floor: 4
   concurrent during active work, target 6-8.

The thin-brief pattern was demoed at the end with `.76`'s reviewer (~25
lines vs prior ~150). Apply to ALL dispatches next session.

## Ready candidates — claim a wave on entry

`br ready -l scope:bootstrap` after this session's closures. Group by package:

**`internal/brcli/` (post-`.16`; 5 methods now exist):**
- `hk-872.26` `br --version` handshake (BI-024a) — likely small, new file `version.go`
- `hk-872.28` exit-code taxonomy → `BrError` enum (BI-025a) — new file `brerror.go`; foundational for all other policy beads
- `hk-872.29` mandatory `--format json` argv prepend (BI-025b) — modifies `Run` in `adapter.go`
- `hk-872.30` subprocess timeout discipline (BI-025c, 5s/10s) — modifies `Run`
- `hk-872.31` stderr 1 MiB cap + 5 scenarios (BI-025d) — modifies `Run` and `Result`
- `hk-872.32` concurrent-invocation discipline (BI-025e) — mostly contract test
- `hk-872.33` Beads-breakage absorption (BI-026)

**Same-file conflict warning:** `.29/.30/.31` ALL touch `Run` in `adapter.go`.
Per directives, combine them into ONE implementer brief listing all three
bead IDs with sequential commits. Don't parallel-dispatch them. `.26` and
`.28` are independent (new files) and parallel-safe.

**`internal/handlercontract/` (post-`.76` if it merges):**
- HC follow-up beads (TBD — bead inventory exists; check `br ready
  -l spec:handler-contract`). The package is now seeded for sibling beads.

**`internal/lifecycle/` (post-`.5`; production code exists):**
- `hk-8mup.12` Project hash + provenance marker (env var + PGID)
- `hk-8mup.4` Pidfile lock is fd-lifetime advisory (sensor)
- `hk-8mup.3` Pidfile at `.harmonik/daemon.pid` (sensor)
- `hk-8mup.10` Startup order is deterministic (steps 0-9 + 3a + 8a)
- `hk-8mup.11` Orphan sweep precedes reconciliation
- `hk-8mup.44` Sensor: one daemon per project (pidfile lock + content match)
- `hk-8mup.9` Daemon-owned per-project file surface

**`internal/workspace/` (substantial Go work):**
- `hk-8mwo.24` Workspace state machine — production code
- `hk-8mwo.64` WM error taxonomy (12-class typed sentinel set)

**`internal/scenario/` (operational, not RECORDs — last RECORD shipped):**
- `hk-i0tw.14` Each scenario uses isolated event-log directory
- `hk-i0tw.17` Per-suite ephemeral fixture root
- `hk-i0tw.31` Every scenario declares a cadence tag

**Other / cross-cutting:**
- `hk-8i31.76` HC sentinels — IN-FLIGHT; review the result first
- `hk-sx9r.74` Exit-code taxonomy + obligations fixture (ON-001..004)
- `hk-b3f.87` Crash-recovery scenario harness for EM checkpoint contract
- `hk-b3f.88` Workflow-validator unit-test fixture

**Skill beads (`.claude/skills/` markdown content, NOT Go):**
- `hk-jhob.1/2/3/4` — agent-reviewer / beads-cli / agent-config-reviewer / go-subsystem-add skills

## Suggested first move

1. **Add `.claude/implementer-protocol.md` to git** and commit it on main
   before dispatching any agents. They depend on the path.

2. **Resolve `.76` thread**: the reviewer (agentId `ad27e999d5f65b1ca`) was
   dispatched with the new thin-brief pattern. If APPROVE, merge dance
   per directives + close `hk-8i31.76`. Note this is the first dispatch
   under the protocol-doc-driven pattern — verify the reviewer found the
   doc and the dispatch worked end-to-end.

3. **Open with a 5-bead wave**, all dispatched in one Agent-tool message
   batch (parallel):
   - `hk-872.26` (br --version, internal/brcli/version.go — new file)
   - `hk-872.28` (BrError enum, internal/brcli/brerror.go — new file)
   - `hk-8mwo.24` or `.64` (workspace package — different package, no
     collision with brcli)
   - `hk-i0tw.14` (scenario operational — different package)
   - `hk-sx9r.74` (operator-nfr exit codes — likely new package)

   Each brief: ~25 lines, points at the protocol doc, names the per-bead
   helper prefix, optionally points at one canonical sibling. Bead body
   has the work spec. Do NOT paraphrase.

4. After dispatching the wave, immediately convert closed-blocks deps for
   the NEXT wave (`.29/.30/.31` combined-implementer brief; substantive
   workspace beads; etc.) so dispatches are queued for refill the moment
   reviewers go out.

## Files to open first

1. `git log --oneline -10` — this session's commits
2. `.claude/implementer-protocol.md` — the new conventions doc; read it before dispatching anything
3. `internal/brcli/adapter.go` + the 5 method files (`show.go`, `listdependencies.go`, `audit.go`, `listinflight.go`) — canonical brcli-consumer pattern
4. `internal/handlercontract/sentinels.go` (if `.76` merges) — HC sentinel pattern for sibling beads
5. `internal/lifecycle/pidfile.go` — production-code-from-fixture pattern reference

## Blocking question for user
None.
