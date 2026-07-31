# Next steps — after the cleanup

**Date:** 2026-07-27. **Reconciled against the tree 2026-07-30** — the corrections are marked ⚠ in place.
**Read first:** [`CHARTER.md`](CHARTER.md) — the stable statement of what the program is and what "done"
means. It outranks this document on intent.
**Siblings:** [`_plan.md`](_plan.md) — deletion sequencing. [`DECOMPOSITION-MAP.md`](DECOMPOSITION-MAP.md)
— the ordered steps, and where the live order now lives. [`OPEN-DEFECTS.md`](OPEN-DEFECTS.md) — defects
recorded, not chased. [`SPEC-TRIAGE.md`](SPEC-TRIAGE.md) — the traceability report §1 commissioned.
[`CARRY-FORWARD.md`](CARRY-FORWARD.md) — the 89 external facts. (This line said 83.
`grep -cE '^[0-9]+\. ' CARRY-FORWARD.md` returns 89, and both that file's own header and `CHARTER.md`
say 89.)
**This document:** what has to be true *before* the rewrite is worth starting, and what has to change so the same defects do not regrow.

The deletion plan removes bad code. This one removes the things that **produced** it. If only the
first happens, the codebase regrows the same shape in another three months — the causes below are
still live in the instruction surface today.

Keep this short. Over-planning is the documented failure mode of the last attempt — planning-doc
churn outweighed shipped production code by roughly an order of magnitude over its last 50 commits.
Five workstreams, each with a measured trigger.

---

## 1. Spec triage — telling good requirements from bad

### The finding

**1,180 unique requirement IDs in `specs/`; 325 (28%) appear in zero Go file.** They are not evenly
spread — the distribution is bimodal:

> **Recount 2026-07-30. The total is unchanged at 1,180 and the orphans are about 325 (27.5%).**
> The orphan count was written as 291 (25%). Commands:
> `grep -rhoE '\b[A-Z]{2,6}-[0-9]{3}[a-z]?\b' specs/ --include='*.md' | sort -u | wc -l` for the total,
> and the same pattern over `internal cmd --include='*.go'` piped through `comm -23` for the orphans.
> Two passes ran that regex on 2026-07-30 and got 325 and 324 — a one-ID difference that rounds to the
> same rate. Do not spend time on the gap; re-run the command.
>
> **Why it got worse is the finding, not the number.** The cleanup is the cause. Deleting
> `internal/specaudit`, retiring `reviewloop.go` and removing 681 signature-pinning test files took the
> citing Go files with them. That inverts this section's framing: the deletions are **manufacturing**
> orphans, so an orphan count taken after a deletion step measures the deletion, not the spec. Deleting
> dead test mass raises the orphan rate. That is expected and is not new spec rot.
>
> **⚠ Three numbers for one thing, and the two passes disagree on which to trust.** The report this
> section asks for at "What to do" now exists — [`SPEC-TRIAGE.md`](SPEC-TRIAGE.md), with
> `traceability.csv` beside it. It reports 293 uncited of 1,164 (§B below quotes the same pair). It uses
> a different attribution model — registry prefix ownership, not per-file occurrence — which is why the
> totals differ at all. One pass concluded "trust `SPEC-TRIAGE.md`, treat the table below as the shape,
> not the count". The later staleness sweep concluded the opposite, that §B's pair is the stale one,
> because the command above reproduces this section's total exactly. **Tie-break: prefer the number you
> can re-run.** `CHARTER.md` §"Specs come after" records the reason — `SPEC-TRIAGE.md` cites
> `traceability.csv`, `trace.json` and a `trace.py` generator that are not in the repo, so its figure
> cannot be reproduced. Treat the table below as the shape, not the count, either way.

| Healthy (written with the code) | orphan % | | Suspect (written ahead of code) | orphan % |
|---|---|---|---|---|
| `run-state-machine` | 4% | | `harness-contract` | **85%** |
| `execution-model` | 5% | | `architecture` | **61%** |
| `control-points` | 5% | | `cognition-loop` | **49%** |
| `claude-hook-bridge` | 5% | | `handler-pause` | **50%** |
| `workspace-model` | 5% | | `digest-command` | **41%** |
| `event-model` | 6% | | `claude-launchspec` | **36%** |
| `scenario-harness` | 6% | | `operator-nfr` | **35%** |

The band between is thin but not empty (`system-state` 23%, `pi-harness` 24%, `flywheel-motion` 25%,
`session-keeper` 27%). **Orphan rate reliably tells you which specs to distrust.** It does not tell you what is wrong with them — see the taxonomy.

### The taxonomy — this is not a cheap tagging pass

70 orphans hand-classified with grep and git evidence across the 7 suspect specs:

| Cat | Meaning | % of sample | Est. of 291 |
|---|---|---:|---:|
| **A** | Implemented, untagged — cheap citation fix | **37%** | ~108 |
| **C** | **Stale** — described a real design since replaced or deleted | **23%** | ~67 |
| **E** | Duplicate, contradictory, or broken cross-reference | 14% | ~41 |
| **B** | Aspirational, never built | 13% | ~38 |
| **D** | Unfalsifiable | 9% | ~25 |
| **F** | Not a requirement (ID-extraction artifact) | 4% | ~12 |

**Only 37% is a tagging fix. 36% (B+C) is actively wrong — and stale dominates aspirational.** The
specs are not wishful; they are **rotted**. Two deletions caused most of it: commit `353fc3c1e`
(2026-07-02) removed the entire cognition-loop implementation (9,038 lines of
`.pi/extensions/flywheel/`), and `e99a52fff` — **this session's specaudit deletion** — removed the
only enforcement AR-013, AR-052 and HC-026b ever had. Both commits are on HEAD
(`git merge-base --is-ancestor <sha> HEAD` returns 0 for each).

> **Correction 2026-07-30 — `internal/specaudit` was not deleted, it was reduced.** The package is
> still in the tree. `ls internal/specaudit` returns three test files and a `doc.go`. What went was
> the 129 prose-only sensors. What stayed are three tests that import the package they constrain: the
> agent-type regex (AR-025), the event-bus interface (HQWN-57), and declarative scenario loadability
> (SH-INV-005). Do not read "the specaudit deletion" as "the directory is gone".

> That second one is a real consequence of today's work. Those sensors were the wrong mechanism
> (they asserted markdown contained strings), and deleting them was right — but it means the
> traceability report below is now the *only* thing that can catch spec drift, and it does not exist
> yet. Build it before the drift compounds.

### Two mechanical signals that DO NOT work — measured, not assumed

- **Weasel-word grep fails.** RFC-2119 modal distribution is statistically identical between orphaned
  and tagged IDs (MAY: 1.5% vs 1.8%). You cannot find category D by grepping for SHOULD/MAY.
- **"Does the named symbol exist in Go?" fails, and fails dangerously** — it scores 84% as category A,
  misclassifying **every** stale requirement, because the symbol exists and the code does the
  *opposite* of what the spec says.

**The load-bearing step is a polarity check**, and it is the one a naive script skips: does the spec
say MUST NOT where the code does? Does it say "no-op" where the code kills? Does it pin
`schema_version: 1` where the code writes `2`? That step is what separates A from C, and it is the
whole value of the exercise.

### Your example, and why it is the *easy* case

`config-inventory.md` §2.10 cites `ON-004c`, which appears in zero Go files — but the feature **is**
implemented (`FLYWHEEL_BUDGET_USD_PER_DAY`, `envFlywheelBudgetUSDPerDay`). That is category A: a missing link,
cheap to fix. The dangerous ones look identical from the spec side and are the opposite.

### Per-spec verdicts

| Spec | Orphan | Verdict |
|---|---:|---|
| `session-keeper` | 27% | **TRIAGE** — best oracle in the repo; SK-002/3/4/6/7 map 1:1 to `internal/keeper/ports.go`. Pure missing citations. |
| `system-state` | 23% | **TRIAGE** — `stategather.go` `RollUpLabel` is a literal transcription of the spec's pseudocode. |
| `digest-command` | 42% | **TRIAGE (light)** — orphanhood is 100% missing `// Spec:` comments. |
| `handler-pause` | 50% | **TRIAGE + rewrite** — the subsystem is *richer* than the spec; it ships per-account pause and auto-resume that the spec lists as out-of-scope. |
| `harness-contract` | 85% | **TRIAGE + rewrite 5 IDs** — real subsystem, but **Go** cites `PI-*` across 49 files / 26 unique IDs vs `HN-*` in 5 files / 1 ID. `pi-harness.md` superseded it in practice. |
| `architecture` | 62% | **SPLIT** — triage §4.1–4.5 (AR-010/011/016 are real, depguard already machine-enforces layering); **delete** §4.0/4.6/4.10, which constrain documents rather than code. |
| `cognition-loop` | 49% | **DELETE** — a faithful map of a subsystem deleted 2026-07-02. `specs/flywheel-motion.md` §0.2 explicitly forbids rebuilding it. Salvage CL-030..033, CL-051, CL-083, CL-090/090a. |
| `operator-nfr` | 35% | **REWRITE, mostly delete** — 1 of 8 sampled is real. Specifies an entire upgrade/rollback subsystem with zero product code; `internal/operatornfr/` was already deleted as unwired theater (`c14ad11d2`). |

### The requirements that would actively break a rewrite

These are worse than orphans — a rewrite that *obeys* them regresses the working system:

| ID | The trap |
|---|---|
| **AR-038** ⚠ | "Agent-to-agent coordination via files … is forbidden," with a reviewer sensor rejecting any proposal that introduces file-based handoff. The shipped crew flow *does* pass work through `.harmonik/crew/missions/<name>.md` — but **that file is written by the crew agent per the `crew-launch` skill, not by Go**; every occurrence in `cmd/harmonik/crew.go` is a comment, several saying the path is never auto-consulted. **The contradiction is with the shipped workflow, not with any Go line — re-establish the evidence before acting on this row.** |
| **HN-003** | "`LaunchSpec` MUST NOT write `agent-task.md`." `internal/harness/claude/launchspec.go` calls `workspace.WriteAgentTaskVia`. Enforcing it breaks every claude run. |
| **HN-012** | Says the harness resolver fails closed on unknown selectors. `resolveHarness` has **no error return** and ends `return core.AgentTypeClaudeCode`. Implementing it changes production behavior on every malformed `harness:` label. |
| **CP-059** | Reads as a shipped egress sandbox. Zero hits for `egress_whitelist` in any `.go`. |
| **ON-020g / ON-021** | A fully-specified upgrade + `--rollback` + fd-passing subsystem, 7 sub-rules, exit codes. No `upgrade` subcommand exists. A rewrite would treat a phantom as regression scope. |
| **AR-017** | Closed list of out-of-process actors omits the supervisor, the keeper watcher, and tmux. A rewrite would design a process model that cannot host the running system. |
| **CL-100** | Names a deleted directory as the *sole legal* wiring point for budget and credentials. Real wiring is `internal/daemon/spendmeter_hkk3f8g.go`. |
| **ON-018** | Promises N-1 readability of `.harmonik/queue.json`'s `schema_version` (`specs/operator-nfr.md` §4.5). `internal/queue/types.go` `UnmarshalQueue` returns `ErrSchemaVersion` on anything but 1, and the package's `schemaVersion` constant is 1. (ON-015 is a *different* N-1 promise covering the Beads schema and harmonik's overlay — amending it leaves this contradiction standing.) |

**Re-verified 2026-07-30.** Every code claim in the table above still holds. `WriteAgentTaskVia` is
still called from `internal/harness/claude/launchspec.go`. `internal/daemon/harnessresolve.go`
`resolveHarness` still has no error return and still ends `return core.AgentTypeClaudeCode`.
`grep -rn 'egress_whitelist' --include='*.go' .` returns zero lines. No `upgrade` subcommand exists in
`cmd/harmonik`. `internal/daemon/spendmeter_hkk3f8g.go` is still the real budget wiring. AR-017 and
AR-038 were not re-checked in this pass and stay as written.

**A live bug fell out of this** — filed as `hk-rr1dy`: the daemon writes `handler-state.json` at
`schema_version: 2` while the CLI hard-rejects anything above 1, so `harmonik handler status` and
`handler resume` fail against state the current daemon wrote, telling the operator to upgrade a
binary that is already current. The daemon's own comment claims the two constants match. HP-016 is
the stale contract behind it. **Still open and still true on 2026-07-30**:
`br show hk-rr1dy` reports OPEN, `internal/daemon/handlerpause_persist_m0k0a.go`
`handlerStateSchemaVersionDaemon` is 2, and `cmd/harmonik/handler.go` `handlerStateSchemaVersion` is 1.

### What to do

1. **Build the traceability report** — generated, run in CI, **never a test**. It is now the only
   spec-drift detector that could exist.
2. **Apply the polarity check**, not the noun check. The 7-step decision procedure is scriptable
   through step 3; step 4 (polarity) needs a reader.
3. **Delete `cognition-loop.md` and the phantom half of `operator-nfr.md` before the rewrite reads
   them.** Category C is the only one that actively costs you. **Not done — both files are still in
   `specs/` on 2026-07-30.** The Go package this item cites as already deleted,
   `internal/operatornfr/`, really is gone.

**Do not treat `specs/` as uniformly normative.** The 4–7% specs are usable as a rewrite oracle today.
The 35–85% specs must be triaged first, and `AGENT_INDEX.md`'s blanket "all of `specs/` is normative"
heading is what currently prevents anyone from noticing the difference.

---

## 2. Agent instructions — removing what caused this

Every defect below traces to a specific instruction that is **live in the tree today**. The fix is
mostly deletion, per the standing preference that the remedy is usually to remove an instruction
rather than add one.

### 2.1 The direct cause of 885 bead-named test files

`.claude/implementer-protocol.md` §Helper-prefix discipline:

> "package-level test **helpers** MUST use a per-bead camelCase prefix (e.g. `leaseFixtureWriteLockAtomic`) … derive one from the bead's concept … Never collide with sibling-bead helpers."

This is the only instruction in the tree tying a test artifact's *name* to a *ticket*. It was written
about helper functions; agents generalized it to **files** — 885 of them, 255,664 lines.

The underlying goal (parallel worktrees not colliding) is better served by the opposite instruction:
add tests to the existing `_test.go` file for the code you changed. If two beads collide there, the
merge conflict is correct and cheap — a per-bead file *hides* that the same behavior is now tested
twice.

**Action: DELETE the section. Replace with the colocation rule.**

> **DONE, and the premise is now dead.** The rule was removed in `92d81fd60` (on HEAD). The section in
> `.claude/implementer-protocol.md` now records what it used to say and points at colocation instead.
> The file count came down with it: `find . -name '*_test.go' | grep -cE '(^|/|_)hk[a-z0-9]{3,}'`
> returns **44** today, against the 885 that started this. Keep the finding as history. Do not act on
> it again.

### 2.2 The instruction that told agents to write tests they never ran

`.claude/implementer-protocol.md` §F19 tells implementers never to run daemon-booting or
scenario-tagged suites, and that if a scenario test is the natural gate for the bead, to skip it and
note the deferral in the commit. Combined with `docs/foundation/project-level/build-practices.md`
("bug fixes require a reproducing scenario test") the net instruction is: **write a scenario-shaped
test, at the wrong layer, and do not run it.** That is the 885-file pattern's other half.

**Action:** make the layer rule explicit — *the test goes at the lowest layer that still fails before
the fix* — and treat "the natural gate is a suite I cannot run in budget" as a signal the change is
too big for one dispatch, not as a licence to defer.

> **DONE.** `.claude/implementer-protocol.md` now carries "Put the test at the lowest layer that still
> fails before your fix", directly under F19, and frames F19 as an instruction to escalate rather than
> to fake. F19 itself stays, which is correct — it is a budget rule, not a quality rule.

### 2.3 Volume incentives

`docs/methodology/TESTING.md` set **"80% line coverage per package"** (§1) and **"CI fails the merge
if thresholds regress"** (§Coverage enforcement) — plus "one scenario per workflow-library entry."
These reward volume, and we got volume: at the time of writing, **2.25:1 test-to-production**
(487,997 test lines against 216,695 production, post-deletion). **⚠ Re-measured 2026-07-30: the ratio
is now 1.44:1** — 306,825 test lines against 212,562 production. `UNWIRED-INVENTORY.md`'s table gives
307,741 against 214,117 from a different command on the same day; the ratio is 1.44 either way, so use
whichever command you can re-run and do not treat the absolute lines as exact. The deletions moved the
ratio; nothing about the incentives that produced it has changed, so the number is a result, not a fix.

⚠ **Do not delete these blindly — a stricter gate is live and unreconciled.** `scripts/coverage-gate.sh`
enforces a 90% floor / 95% core / 0.3pp regression cap against the checked-in `coverage.baseline`, and
`scripts/cmd-coverage-gate.sh` ratchets `cmd/**`; both run from the `check` target in the `Makefile`,
not from `check-fast` or `check-short`. What is false is the *CI* claim — CI runs only `check-short`,
which invokes neither. Reconcile the doc and the scripts in one change, or you strip the prose and
leave a harsher undocumented gate behind.

**Action: DELETE the targets from the doc and reconcile against the scripts.** Replace with one line: coverage is a diagnostic, never a target — the
question is *which real behavior is unprotected*, not *what percent*.

> **DONE for the doc half, and three numbers above have moved.** `docs/methodology/TESTING.md`
> §Coverage enforcement now names `scripts/coverage-gate.sh` as the authority, states that CI runs
> neither gate, and records that the old bullet was false in both halves. Re-measured 2026-07-30:
> the ratio is **1.44:1** — 307,741 test lines against 214,117 production, by
> `find . -name '*_test.go' -exec cat {} + | wc -l` and its `-not -name '*_test.go'` twin. And
> `coverage.baseline` holds **51** entries, not 27. The 27 was wrong on the day it was written:
> `git show fa97cf791:coverage.baseline | wc -l`, at the commit that created this document, returns
> 52. The scripts themselves are unchanged, so the doc-versus-script reconciliation the paragraph asks
> for is only half done.

### 2.4 The origin of the markdown-grepping tests

`docs/foundation/spec-template.md` requires citing "the normative test obligations … the test layer
and the requirement IDs each test proves." That instruction, applied literally, produces a test whose
job is to assert a document contains a string. It produced `internal/specaudit`.

**Action: DELETE it.** Requirement-ID→test traceability is a *generated report* (§1), never a test.

### 2.5 The complexity ratchet that cannot fire

`.golangci.yml` sets `funlen: lines: 100`, grandfathered via `--new-from-rev`. Two problems, both verified:

- `funlen` reports at the function's **declaration line**, and `check-fast` uses
  `--new-from-rev=HEAD~1`, so a grandfathered function never re-reports. `beadRunOne` was **born over
  the ceiling at 119 lines** (commit #16 of 370), peaked at 2,394, and is **1,764 today** — it has
  never once produced a finding. (Re-measured 2026-07-30. The figure in this bullet said 2,289. The
  drop is real work, not a measurement change: the dispatch scheduler, the cadenced maintenance and
  the pure admission gates were all lifted out of `workloop.go` over the last week. It is still by far
  the largest function in the repo and still 17× the ceiling.)
- ⚠ **And it carries an explicit `//nolint:funlen,gocognit,cyclop`** on the line above
  `internal/daemon/workloop.go` `beadRunOne`, justified in-line as "pre-existing … the RT ports stream
  exists to decompose [it]". **An explicit grandfather list alone does not surface it** — the
  suppression has to be removed too, or the ratchet still cannot see the largest function in the repo.
  (This bullet used to cite `workloop.go:3119`. That line number rotted within days of being written,
  which is why the convention is to name the symbol.)
- The exclusion `- path: (_test\.go$|^internal/scenario/|^internal/specaudit/)` disables `funlen`,
  `cyclop` and `gocognit` for **all test code** — 308k lines with no complexity ceiling at all
  (re-measured 2026-07-30; the bullet said 525k, and the deletions are why it fell).

**Action:** replace the implicit grandfather with an explicit, checked-in, **shrinking** list — a
commit touching a listed function must not grow it. Remove the blanket `_test\.go$` exclusion.

### 2.6 The surface every agent actually reads says nothing about quality

`buildAgentTaskContent` (`internal/workspace/agenttask_chb028.go`) generates the `agent-task.md` that
**every one of ~4,700 commits was driven from**. Verified: `Coverage Check` and `Spec Field-Name Check` are not in this function at all — they live in
`buildReviewTargetContent` (same file), which writes `review-target.md`. Only `Reviewer Constraint` is
a reviewer-phase branch of the task file. The implementer receives the phase header, task description, bead, and extra context —
**nothing about test placement, test quality, or structure.**

Every instruction above lives in a file the implementer may never open. This one it always opens.

**Action:** add two short sections to the implementer phase — test through the entry point the daemon
or a user actually calls, name the file after the behavior rather than the bead, and treat "I need a
new `export_*_test.go` seam" as a report that the seam is in the wrong place.

**Landed, and one thing more.** The `## Tests` section now also carries the operator's 2026-07-28
default: a bad test is worse than bad production code, so an implementer that meets one in its path
deletes it as part of the work rather than filing it. This matters most for `internal/daemon` and
`internal/core`, the two packages holding the largest remaining concentration and the next to be
worked.

> **Verified 2026-07-30.** `buildAgentTaskContent` writes both a `## Tests` and a `## Structure`
> section for every non-reviewer phase. The Sequence table below used to cite this as landing at
> `734f283a7`. That object is not on this branch — `git merge-base --is-ancestor 734f283a7 HEAD`
> returns 1. The commit that is on HEAD is `ab12d65ba`, same title. Use that one.

### 2.7 Reviewer checks that would have caught all of it

`.claude/agents/agent-reviewer.md` §3 asks only whether tests are at "the appropriate tier" and are
"meaningful." Add three mechanical checks, which need no judgment:

- a new `_test.go` filename containing a bead ID → `REQUEST_CHANGES`
- a new or widened `export_*_test.go` entry → `REQUEST_CHANGES`
- **exporting a pointer to a production global** → `BLOCK` (this exists today and lets tests mutate
  production state)

⚠ Note the two agent-reviewer definitions have diverged: `.claude/agents/agent-reviewer.md` (216
lines) is what the Agent tool loads, while `.claude/skills/agent-reviewer/SKILL.md` (558 lines) is in
the live skill registry and is a **superset** — five sections exist nowhere else. Both are still
present and still divergent on 2026-07-30 (`wc -l` on each). Reconcile them; do not delete the larger
one, which would lose content. The line counts here said 200 and 542. The "newer — Jul 23 vs Jul 12"
claim is **unverified**: file timestamps in a fresh worktree are checkout times, so mtime cannot
settle it. Use `git log -1 --format=%ci` on each path if the ordering matters.

### 2.8 The architecture-warning mechanism

The operator's requirement: *agents raise warnings when they encounter bad architecture and get it
fixed before new code.*

The design constraint is that an implementer is a single-shot, ~10-minute agent that cannot wait for
adjudication. **Nothing can be gated on a live judgment call**, or every task deadlocks on taste.

**Trigger — countable, two conditions only:**
1. Landing the bead requires adding lines to a function or file already over the complexity ceiling.
2. The change cannot be tested through a real entry point without a new or widened `export_*_test.go` seam.

Anything else an agent dislikes is an ordinary follow-up bead, not a structure block.

**Where it goes:** a bead, `--label structure-block --priority 1`, parented to the epic, titled with
the *file and symbol*. Plus a `structure-deferred` flag in the commit's `Review-Verdict` trailer. Not
comms — the implementer has no inbox and no time to wait on one.

**What the implementer does meanwhile — it does not stop.** It makes the smallest change that does
not deepen the defect and commits, naming the block bead. If no such change exists, it commits the
*failing test* plus the block bead. An implementer that halts produces a false `no_commit` and gets
re-dispatched into the same wall.

**The rule that actually stops the bleeding — second sighting.** A second `structure-block` against
the same file or function means **no further bead touching it dispatches until the decomposition
bead lands.** This is what converts "agents raise warnings" into "it gets fixed before new code," and
it is cheap: the orchestrator greps open `structure-block` beads by file before submitting a batch.

> `workloop.go` would have hit this at roughly commit 30 of 370.

Reuse the existing precedent rather than inventing machinery — `orchestrator-rules` §Priority already
says friction beads jump ahead of feature work. Add `structure-block` to that same sentence.

**Deployment note:** all 10 shipped skills are byte-identical between `.claude/skills/` and
`cmd/harmonik/assets/skills/`. Re-checked 2026-07-30 with
`diff -rq cmd/harmonik/assets/skills/ .claude/skills/` — the only output is the seven project-authored
skill directories that have no embedded source. Any edit to a shipped skill must land in both in the
same commit.

---

## 3. Stale phase markers

> ## ⚠ THIS SECTION'S PREMISE IS DEAD FOR `specs/` AND FOR Go. Measured 2026-07-30.
>
> **`grep -rn 'MVH' specs/ --include='*.md' | wc -l` returns 0.**
> **`grep -rn 'MVH' --include='*.go' internal cmd | wc -l` returns 0.**
>
> The 2026-07-28 sweep cleared both surfaces completely. Everything below about spec lines, Go lines,
> the three renames, the `"mvh-required"` enum, and the two "secondary traps" describes a state that no
> longer exists. It is kept as the record of what was done and why, not as work to do. The one part
> still live is `docs/`, and item L in §Deferred — housekeeping owns it.
>
> Item by item, against the tree today:
> - **The three renames are done.** `DefaultMVHRoles` no longer exists under any spelling.
>   `ValidateMVHRoleDefaultSkills` is now `ValidateRequiredRoleDefaultSkills` in
>   `internal/core/policydocument.go`. `mVHTierOrder` is now `modelTierOrder` in
>   `internal/core/freedomprofiletightest_hka8bg33.go`.
> - **The `"mvh-required"` wire value is retired, not preserved.** `internal/core/role.go` now declares
>   exactly two `RoleStatus` values, `required` and `declared-but-deferred`, and
>   `internal/core/rolevalidation_test.go` asserts that the old spelling is *rejected*. The warning
>   below says a blind rename would silently corrupt the policy parser. That was true. It was handled
>   by a spec amendment plus a rejecting parser, which is what the warning asked for.
> - **The two latent stubs are still latent, and their MVH wording is gone.**
>   `internal/daemon/socket.go` `noopRequestHandler` now returns "RequestHandler not wired yet", and
>   `cmd/harmonik/handler.go` prints "(unavailable — HandlerPauseController not yet wired)". The
>   qualifier was replaced with "yet", so the feared TODO-to-permanent-design conversion did not
>   happen. Both are still unwired. Filing them as bugs is still open work.

"MVH" (Minimum Viable Harmonik) was an early phase concept agents latched onto and never let go:
**443 lines in `specs/`, ~913 lines in `docs/`, ~516 in Go**, as measured on 2026-07-27. (Counts vary
by unit — occurrences run higher; the figures here are lines.) The damage is that "at MVH" makes a live
requirement read as provisional — `specs/claude-hook-bridge.md` alone carries "the hook timeout is
fixed at 30 seconds at MVH", "no new bus event is emitted at MVH", "cleanup is acceptable but not
required at MVH". A reader cannot tell current behavior from abandoned intention.

**The good news: the compatibility surface is one string.**

| Category | Volume | Disposal |
|---|---|---|
| Phase marker, now meaningless | 253 spec lines, ~370 Go comment lines | Delete the qualifier; the requirement stands. Per-file with review — specs are normative. |
| Deferral still live | 190 spec lines → **29 headline capabilities** | Bead them **before** deleting the prose, or the work is lost. |
| Load-bearing identifier | **4 non-test Go symbols; 1 wire value** | See below. |
| Historical narrative | `.kerf/` 11,749 · `plans/` 4,485 · `docs/historical/` 56 | **Freeze. Append-only archives, never touch.** |

**Safe renames (zero non-test callers):** `DefaultMVHRoles()` → `DefaultRoles`,
`ValidateMVHRoleDefaultSkills()` → `ValidateRequiredRoleDefaultSkills`, and `mVHTierOrder` →
`modelTierOrder` (misnamed — it ranks model tiers haiku/sonnet/opus, nothing phase-related; it does
have 2 in-file callers, so the rename is compiler-checked rather than callerless).

**⚠ The one thing that must not move mechanically — RESOLVED, see the banner at the top of this
section.** `"mvh-required"` was an **on-disk policy-document YAML enum**, normative in
`specs/control-points.md` §6.2 and §6.3, compared against parsed YAML in `internal/core/role.go` and
`internal/core/policydocument.go`. A blind `MVH → ""` pass would have corrupted it with **no compiler
error** — both sides were string literals, so the policy parser would have begun rejecting every valid
role document at runtime. It was changed the safe way: the enum is now `required`, and the retired
spelling is rejected with a named error rather than silently ignored.

Two secondary traps, both now historical: `mVHTierOrder` (lowercase leading m) was missed by a
case-sensitive pattern and mangled by a case-insensitive one; and stripping "at MVH" from
`not wired at MVH` would have converted a TODO into a statement of permanent design.

**Three of the "deferrals" are actually live defects, not future work** — the two `noopRequestHandler`
methods in `internal/daemon/socket.go` and the `dispatcher_backlog_held` line in
`cmd/harmonik/handler.go` are latent stubs in shipped paths. Still unwired on 2026-07-30. File as bugs.

### `v0.1` is the same disease, and must be cleaned in the same pass

**130 lines in `specs/`, 534 in `docs/`, 24 in Go** — "in v0.1 the daemon…", "v0.2 may add…", "v0.1
ships no timeout". (Re-counted 2026-07-30 with `grep -rn 'v0\.1'` over each tree. The figures here
said 110 / 533 / 31. Unlike MVH, this vocabulary was never swept, and the spec count went **up**.) It
is a *parallel* vocabulary for the same idea, so the specs now carry two competing era markers. MVH is
gone, so `v0.1` is what is left of the problem, not half of it.

**Checked and NOT stale — leave alone:** `parity` (327 in Go) is the live `twinparity` subsystem;
`D1`/`D2` in specs are normatively-cited design-decision IDs, not phases; `P1`/`P2` are bead
priorities. `Phase 1`/`Phase 2` in Go is **mostly** test-step labels (85 of 127 hits) — but
`internal/core/eventtype.go` ~1230–1243 uses "Phase 1, WR1" / "Phase 2, PB1" as a genuine era marker,
so it needs a look rather than a blanket pass.

---

## 4. Workflow fixtures in the wrong place

`specs/examples/` holds 28 `.dot` files presented as normative spec artifacts. They are mostly not.

- **Nothing in `specs/examples/` is embedded or loaded at runtime.** `standardgraph.go` embeds
  `internal/daemon/standard-bead.dot` — its own local copy. The runtime resolver reads the *project*
  dir (`.harmonik/workflows/`, `workflow.dot`), never `specs/examples/`.
- **24 of 28 are cited by no `specs/*.md` at all.** `specs/examples/README.md` states the governing
  test itself — normative iff a spec section names the file. Only 4 qualify.
- Two documented invariants are already unmet: WG-037 requires a sibling `<name>.md` per `.dot` (1 of
  28 has one — `per-node-model-effort.md`), and both WG-036's "the engine's example-loader looks there"
  and the README's cited `internal/workflow/examples_test.go` **do not exist**. All three re-checked
  2026-07-30: 28 `.dot` files, one sidecar, and no `examples_test.go`. Note WG-036 §13 also names
  `specs/examples/standard-bead.md` as a required sidecar, and that file is absent too.

**The duplicate is worse than drift — they are different workflows.** `specs/examples/review-loop.dot`
and `internal/workflow/dot/testdata/review-loop.dot` have different `start_node` (`start` vs
`implement`), different node sets, different handler refs, and different condition dialects. The
testdata header still claims it is authoritative "until C5 lands" — C5 landed. **Both halves still
true 2026-07-30**: the two files still disagree on `start_node`, and the stale header sentence is still
in the testdata file. Meanwhile
`standard-bead.dot` **is** byte-identical to its daemon copy, because `standardgraph_sync_test.go`
enforces it. Guarded copies stay in sync; unguarded ones fork silently.

| Disposition | Count | Detail |
|---|---|---|
| **Stay in `specs/`** | 4 | `standard-bead`, `review-loop`, `sub-workflow-example`, `sub-workflow-commit-gate` — the only ones with real spec citations |
| **Move to `testdata/`** | 15 | Beside their consuming test. Each needs **one line** edited — every test declares its own private path helper; there is no shared loader |
| **Delete as redundant** | 9 | Strict subsets of a kept fixture — plus their 9 test files, ~45 tests with **zero unique assertions** |

**The tests are good and stay.** `plan_to_shipped_faithful_test.go` runs 9 scenarios through the real
engine. What is wrong is calling a test input a normative spec artifact.

**⚠ Three hazards for the move:**
1. `specs/workflow-graph.md` WG-036 makes the *path itself* normative ("MUST live at `specs/examples/`").
   Moving fixtures out requires amending WG-036 first, or the move is spec-violating on paper.
2. `standard-bead.dot` **must not move** — `standardgraph_sync_test.go` hard-codes
   `../../specs/examples/standard-bead.dot` and fails the build if it cannot read it.
3. `AGENT_INDEX.md` labels all of `specs/` normative with no carve-out for `examples/`. **That blanket
   heading is the origin of the overclaim** and should be fixed regardless of whether the move happens.

**Real coverage gaps, worth building fixtures for** (the operator's instinct that more pathway
coverage is wanted is correct — these are the pathways with none): `sub_workflow_ref` expansion,
`ordering_key`/`weight` tiebreak, no-progress detection, and `non_committing` as an asserted
behavior. Note `sub-workflow-example.dot` and `sub-workflow-commit-gate.dot` are spec-pinned as
SW-EX-001 yet **have no test at all** — a gap to fill, not files to delete.

---

## 5. Bug discipline

Two live defects were filed today, and the lesson is not "be more careful":

- **Branch-protection deep guard fails open** — a bead merges to a protected target, the ref actually
  moves, and it closes `approved`. **⚠ This one was a mis-diagnosis, and it is now closed.**
  `br show hk-zobns` reports CLOSED as of 2026-07-30, invalid. The guard never failed open. The ref
  that moved was the *unprotected* `integration` branch that the fixture's own bead asks to land on.
  Coverage was instead restored at the guard's own seam. Read the §5.2 row for the detail.
- **`br` exit 3 never stderr-refined** — a permanent "not found" is classified as retryable
  infrastructure failure, routing to Cat-0 and potentially daemon exit 8. **Still live on 2026-07-30**:
  `internal/brcli/brerror.go` `BrErrorFromExit` still refines only `if code == 1`, and
  `BrErrorFromExitCode` still has `case 3: return BrDbLocked`.

**Both were caught by tests that already existed and that blocked nothing.** `.github/workflows/scenario.yml`
runs `make test-scenario` on every push and PR plus a nightly cron. It used to carry
`continue-on-error: true`, and its header still says "Non-merge-blocking: no required status check
configured in branch protection." Neither `check-fast` nor `check-short` invokes the tier.

> **The `continue-on-error` half is FIXED, 2026-07-29.**
> `grep -n 'continue-on-error' .github/workflows/scenario.yml` returns nothing but two warnings not to
> put it back. The tier now reports its real result, and the ops-monitor nightly probe reads a true
> conclusion again. The suite still blocks nothing, because the required status check was deliberately
> **not** added — the workflow header now says do not add it until the deterministic failures close,
> because doing so would wedge every merge.

**Action, in priority order:**
1. ~~**Make the scenario tier merge-blocking.** Cheaper than it sounds: delete one
   `continue-on-error: true` from `.github/workflows/scenario.yml` and add the required status check in
   branch protection. It goes red until the branch-guard bug is fixed — that is the point.~~
   **⚠ HALF DONE, and do not repeat the first half.** The `continue-on-error: true` deletion landed on
   2026-07-29. What remains is the required status check in branch protection, and that is deliberately
   gated on the deterministic failures closing first. A second blocker surfaced with it: about half the
   tier **skips** in CI, because nothing installs `br` and nothing declares a twin build, and a skip
   reads as a pass. Tracked as `hk-ynohn`, open. So a green run on that workflow today proves much less
   than it looks like. Re-stated 2026-07-30 — see §5.1a below, which is now the live version of this
   item.
2. **Fix the correct bug, and verify the call chain before believing any claim about which code is
   live.** The `br` defect is instructive in a way that caught this document out. A first pass asserted
   that `BrErrorFromExitCode`'s inverted table was dead code and the real defect lay elsewhere. It is
   not dead: `internal/brcli/brerror.go` `BrErrorFromExit` calls it for the base value and only
   applies stderr refinement `if code == 1`, so exit 3 falls straight through to
   `case 3: return BrDbLocked`. **That arm is the bug.** The lesson is that "this looks like dead
   code" is a claim requiring a call-chain check, not a reading. Require a fix to name the failing
   observation it removes, not the code it touches.
3. **Fix it at the layer that failed.** See §2.2 — a bug reproduced at the wrong layer produces
   another bead-named unit test and leaves the real path uncovered.

**Carried forward from the subsystem-partition work:** `unknownYAMLKey` in `internal/projectconfig`
early-returns "no unknown keys" for any node that is not a mapping, so a YAML alias defeats the strict
unknown-key rejection — `keeper: *anchor` (or a `subsystems:` entry) hides a typo'd key behind the
alias and it is silently accepted. Pre-existing on the keeper block; inherited by the new `subsystems:`
block. Fix is to resolve alias nodes before the mapping check. **Still live on 2026-07-30** —
`internal/projectconfig/projectconfig.go` `unknownYAMLKey` still returns `"", true` for any node whose
`Kind` is not `yaml.MappingNode`, and its own comment now names `alias` in that list.

### 5.1a The scenario-tier gate — on the list, and here is where it fits

**Added 2026-07-30. Operator call, and it is load-bearing on the ordering:** *"it does not really
matter right now because we are not close to merging, so put it on the list and work out where it
fits."* **Treat it as placed work, not as a blocker.**

**What already landed.** The `continue-on-error: true` flag is gone from
`.github/workflows/scenario.yml` (`hk-plw4z`, `hk-21v7c`). The tier now reports the truth on every REST
surface, and the nightly ops-monitor probe works again with no change to the probe. That was the safe
half, and it is done.

**What did not land, deliberately.** The tier is still **not** a required status check, and the
workflow header now says so in as many words: do not add it until the deterministic failures close,
because that would wedge every merge. So the item is not stalled. It was consciously deferred.

**Three measured reasons it is correctly deferred, not one:**

1. **The gate above it is already red.** `main`'s branch protection requires exactly one check —
   `check (Tier 2)` from `ci.yml` — and that check has failed on every run since 2026-07-17. A second
   required check behind a red first one gates nothing that is not already gated.
2. **About half the tier skips in CI, and a skip reads as a pass** (`hk-ynohn`). **Reproduced
   2026-07-30 by re-running the tier with `br` removed from `PATH`:** skips go from **9 to 39**, and
   `internal/daemon` drops from **354 s to 54 s** while `test/scenario` drops from **54 s to 2.6 s** —
   the tier loses 85% and 95% of its execution time and still reports. **Making a half-skipping tier
   required makes `main` green on half a tier**, which is worse than leaving it advisory.

   ⚠ **Pin the denominator or this number is wrong in both directions.** 39 of the 673 tests that
   compile under the tag is 6%. 30 of the ~60 that call `skipRealDaemonE2EInShort` — the real-daemon
   population the tier exists for — is **exactly half**. "Half the tier" is true of the second and
   false of the first. Quote the population, not the fraction.
3. **Eight tests fail deterministically.** Per-test dispositions are in §5.2 below. **Re-measured
   2026-07-30 and the list has moved even though the count has not** — see the correction there before
   quoting either.

**Where it fits, in order:**

1. **Now, and it is the only part worth doing now: stop the tier skipping.** This is not the gate. It
   is what makes every later measurement of the gate honest, and it is the one item here whose value
   does not depend on merging. **Three separate causes, measured 2026-07-30 — "install `br`" is only
   the first:**

   - **`br` absent — 24 tests.** Three of them say *"CI sets br on PATH"* in their own skip message.
     Nothing does.
   - **The twin binary is never built where the tier looks, and these 7 skip LOCALLY too.** Six tests
     in `t2_scenarios_test.go` want `<root>/twin-fail` and `<root>/twin-hang`, from
     `test/twins/fail-immediately` and `test/twins/hang`. **No Makefile target, script or workflow
     builds either, anywhere.** A seventh wants `<root>/harmonik-twin-claude`, which is in
     `.gitignore` and so can never exist in a fresh checkout. `make test-scenario`'s own comment claims
     `build-all` compiles it — but `build-twin-claude` is an alias to `build-twin-generic`, which
     writes `twins/generic-twin` under a different name in a different directory. The Makefile states
     a precondition it does not satisfy, which is why this reads as solved.
   - **Two twin-resolution helpers with different search paths in one tier.**
     `test/scenario/harness_test.go`'s `TestMain` builds its own twins into a temp dir and **swallows
     the build error**, so that half degrades quietly instead of failing. `scenariotest.TwinBinaryPath`
     and `workloop_handlerpause_qxtbq_test.go` look on disk instead and cannot succeed in CI.
     Consolidate on the one that builds what it needs.

   **And seven scenario-tagged tests are orphaned from the tier that names them.** `make test-scenario`
   runs `./test/scenario/...` and `./internal/daemon/...` only, but `//go:build scenario` files also
   live in `cmd/harmonik` (4 tests), `internal/sentinel` (2), `internal/keeper` (1) and
   `internal/runloop` (1). **Two of them fail** when run directly. They are invisible everywhere.
2. **Then get `check (Tier 2)` green.** It is the required check. Nothing about tier 3 matters while
   tier 2 is red.
3. **Then close the deterministic failures** (§5.2, `hk-97gcz`), re-counting them first.
4. **Then add the required status check** — when merging to `main` resumes. In the decomposition order
   that is after `DECOMPOSITION-MAP.md` §3 step 15, not before it.

**Do not read this as "the tier does not matter."** `DECOMPOSITION-MAP.md` §3 step 9 turns a live
scratch-daemon pass into the per-step acceptance check for the decomposition, precisely because this
tier cannot gate anything yet. The two are complements: the scenario tier is in-process coverage of the
composition root with a fake agent, and the live pass is the half it cannot reach.

### 5.2 The load-sensitive test family — make these robust instead of re-diagnosing them

**Operator, 2026-07-29: "at some point we need to come back to that and figure out where the issues are
so those tests can be robust."** This exists because the same diagnosis keeps being re-derived from
scratch. A session runs the daemon suite, finds a spread of failures, and spends an hour concluding
"load flake" — a conclusion four earlier campaigns already reached and wrote down in four different
dated directories that nothing points at.

**Where the prior work actually lives** (none of it is reachable from a boot read — that is half the
problem):

- `plans/2026-07-17-assessor-daemon-campaign/runs/baseline-599f80ab/REGRESSION-TREE.md` — names the
  family: **srt, SocketBinds, ShutdownDrains, Throughput, ClaimSemaphore, SubscribeStream**. Contains
  the deepest single diagnosis in the set (below).
- `plans/2026-07-13-code-revamp/RT12-acceptance-evidence.md` — a wider **"Bucket A — pinned
  known-flakes"** list: SSHLocalhost, StopHookE2E, TenBeadsAtMaxFour, Hk6ynv4_SubscribeStream, VN4,
  ConcurrentMultiQueue, RestartRecovery_QM002bDeadlock, EM015e, QueueSubmit_*,
  ConcurrentRemoteAgentReady, OperatorNFR_Pause, CaptainCrewE2E, ReviewLoop_ResumeSubmitReliable,
  AutoStatus*.
- `plans/2026-07-21-p2-extraction/RT16-emitterport-conversion.md` — the procedural traps.
- `plans/2026-07-21-p2-extraction/E1c-pi.md` — states the oracle plainly: `internal/daemon` **is already
  red at HEAD, so the oracle is differential, not zero.** Capture a before-set with the identical
  command, then `comm -13`.

**The one finding worth not losing.** `TestThroughput_TenBeadsAtMaxFour` was chased to ground: every
extra `run_started` envelope carries a **distinct run_id** with `envelope.run_id == payload.run_id` — no
double-emit. The extra events are legitimate re-dispatches of beads whose fixture handlers timed out,
~26 of 50 beads re-running exactly once across five iterations. **The emission invariant is intact; the
test's `exactly 10` assertion encodes a first-try-success assumption that only holds with spare CPU
headroom.** That is the shape of the whole family: the assertions encode scheduling luck.

> **⚠ RE-OPEN THE CAUSE, 2026-07-29.** The *invariant* half above is confirmed. The **CPU-starvation
> mechanism is not.** Re-measured on an idle box (load 2.8 across 10 cores), the test still produces
> **exactly the same `got 16`** — so "sustained box load" cannot be the explanation, even though the
> emission invariant genuinely holds. This run also settles two conflicting prior signatures in favour
> of `got 16` (the `ratio` line passes at 0.44 and is a `t.Logf`, not the failure). Something
> deterministic is causing ~6 re-dispatches regardless of load. Treat the prior ENV/LOAD-FLAKE label as
> **right about the invariant, unproven about the cause.**
>
> **STILL UNEXPLAINED after the low-disk correction. Re-measured 2026-07-30 by this sweep.**
> `go test ./internal/daemon/ -run '^TestThroughput_TenBeadsAtMaxFour$' -count=1` on a box with 59 GiB
> free — six times the 10 GiB watermark — fails with **`got 16`** again, and names all sixteen run IDs.
> This matters because `OPEN-DEFECTS.md` established on the same day that a low disk reading holds the
> dispatch tick before queue selection and turns 23 daemon tests red, and that
> `internal/daemon/t11_throughput_test.go` is one of the fixtures that does **not** stub the disk
> reading. So low disk was the obvious candidate cause, and it is not the cause here. The signature
> survives an idle box, a healthy disk, and isolation. Do not close this as either a load flake or a
> disk artifact.

**Two traps that invert the answer if you get them wrong:**

1. **The confirmation procedure is not uniform.** The load-sensitive ones are confirmed by re-running
   **in isolation** (isolated pass ⇒ load artifact). But `TestMergeToMain_RealConflictWithBeadsLedger_Escalates`
   is **isolation-sensitive** and must be confirmed **in the full suite**. Applying the wrong procedure
   to either gives you a confident wrong answer.
2. **`FAIL scenariopkg.test/scenariopkg [build failed]` is not a failure.** It is intentional
   scaffolding from the scenario-gate efficacy test, and it also appears as a *timeout-cascade artifact*
   when the package blows Go's 10-minute default. Neither is breakage.

**The part that is actually broken, and the reason this is a work item rather than a filing exercise:**

- **The allowlist has absorbed at least one genuine defect.** `EM015e` sits in RT12's pinned-known-flakes
  bucket. Re-measured 2026-07-29 it is a **deterministic true red**, not a flake: it asserts a
  `no_progress_detected` event and `completion_reason="no_progress"`, but `emitNoProgressDetected` has
  **zero call sites** and the live path emits `review_fixup_stalled`. A flake allowlist that swallows a
  real bug is worse than no allowlist, because it converts a red into permanent silence. Assume EM015e
  is not the only one — every entry needs re-confirming against today's tree, not inherited.
  **The EM015e test is gone as of 2026-07-30.** `grep -rn 'EM015e' --include='*.go' .` finds only
  unrelated `internal/workspace` diff-hash tests. The review-loop retirement (`3cec5afd7`) deleted
  `internal/daemon/reviewloop.go`, `emitNoProgressDetected` and the scenario test together. The lesson
  about the allowlist stands. The instance does not — do not go looking for that test.
- **Nothing routinely runs the tagged tier**, so its state is unknown between deliberate looks. Measured
  2026-07-29: `go test -tags scenario ./internal/daemon/` yielded **eight** failures where the untagged
  run yielded one. This is the same invisibility that made "does EM015e fail or never run?" unanswerable
  from the tree for days. §5.1's gate is the fix; this section is why it matters.
  **Root cause established 2026-07-29: no prior assessment ever ran `go test -tags scenario` on this
  package at all.** The P2 recipes only `go vet`ed it, and `.github/workflows/scenario.yml` carried
  `continue-on-error: true`. The tier was not neglected — it was never looked at.

  > **Both numbers in that bullet are now wrong, in opposite directions. 2026-07-30.**
  > **The tagged count is at most seven**, because one of the eight — EM015e — no longer exists.
  > **The untagged count was never one.** `OPEN-DEFECTS.md` establishes that `go test -short
  > ./internal/daemon/` yields 23 failures when the disk reading is low and **zero** when it is
  > healthy, on the same box and the same commit. The "exactly one pre-existing failure" that this
  > whole family was reasoned from was an artifact of a machine below the disk watermark. The `-tags
  > scenario` figure was measured on the same low-disk box, so it also needs re-measuring before it is
  > quoted again. Do not re-quote either number without re-running it.

- **Three of these tests print an unconditional `OK` / `PASS`-shaped `t.Logf` while failing** —
  `TestBranchGuard_FailClosed_MergeGuardBackstop`, `TestScenario_RestartRecovery_QM002bDeadlock`, and
  `TestScenario_MultiBead_SerializedNCompletion`. Skim-reading their output tells you the opposite of
  the truth. Deleting those log lines is a five-minute change with outsized payoff, and it belongs with
  this work.

**Dispositions, measured 2026-07-29 — all seven are pre-existing; the tier blocks nothing in Phase 3.**
**Re-checked 2026-07-30: six of the seven test functions are still in the tree** (`t6_scale_shape_test.go`,
`scenario_restart_recovery_ivzsl_test.go`, `scenario_remote_substrate_localhost_dot_test.go`,
`scenario_concurrent_multiqueue_hkumemp_test.go`, `scenario_multibead_mergeconflict_serial_hktijaj_test.go`,
`t11_throughput_test.go`, `branchguard_test.go`). **EM015e is deleted.** `hk-co8g8` and `hk-t2d7n` are
still OPEN. `hk-zobns` is CLOSED.

> **⚠ RE-MEASURED 2026-07-30. The count survived and the list did not — which is the more useful
> finding.** `go test -tags=scenario -count=1 ./internal/daemon/...` gives **656 pass / 9 skip / 7
> fail** in 354 s.
>
> **Two rows below are closed.** `EM015e_NoProgress_ReviewerNotLaunched` is **gone** —
> `emitNoProgressDetected` has zero hits anywhere and `reviewloop.go` was deleted, exactly as its row
> predicted. The branch-guard P0 is **closed**, and its "false red" verdict was confirmed rather than
> overturned.
>
> **Two failures are not on the list at all:** `MultiBead_ConflictSkipsButOthersProceed` (same file and
> same lost-commit race as `hk-co8g8`, so it is one defect presenting twice, not two) and
> `Bl2k6_SubstrateKill_LeavesNoOrphanDescendant` (tmux-contended, did not reproduce on a second run).
>
> **One row needs its verdict inverted.** `RemoteSubstrate_Localhost_DOT_E2E` is listed as fixed by a
> one-line `TMPDIR` pin. The pin exists in `check-short` and **not** in `make test-scenario`, so the
> tier still fails it: macOS `TMPDIR` puts the socket at 130 bytes against the 104-byte `sun_path`
> limit and the reverse tunnel never comes up. That is a one-line Makefile change nobody made.
>
> **`MultiBead_SerializedNCompletion` also prints `serialized N-completion OK: 5 beads closed, 5 files
> on main` *after* failing** — the third instance of the unconditional-`PASS`-shaped-`t.Logf` problem
> this section already names.
>
> **The lesson, which is why this block is longer than a count.** The list was treated as stable and
> the count as the thing to re-derive. It is the other way round: the count has been 7–8 throughout
> while the membership turned over by half. **Re-run the tier; do not re-quote the table.**

| Test | Verdict |
|---|---|
| `BranchGuard_FailClosed_MergeGuardBackstop` | **False red.** Guard did not fail open — the ref that moved was the *unprotected* `integration` branch the bead asks for. Premise went stale 2026-07-06 (`hk-lgykq`) when merge-target resolution moved to per-bead `lands_on` and got **stricter**. `hk-zobns` was a mis-diagnosis and is now **CLOSED invalid, 2026-07-30** — this row said "Open P0". Coverage was restored at the guard's own seam by a direct call to the merge entry point with `target=main` and `protect=[main]`, verified by mutation. The recorded one-line fix could not have worked: the early landing gate and the deep merge guard read the same resolved value against the same list, so protecting `integration` makes the early gate refuse and the deep guard is still never reached. Caveat: `internal/daemon/branchguard_test.go` sits behind the scenario build tag, so the new assertion does not run in the default short gate. |
| `RemoteSubstrate_Localhost_DOT_E2E` | **False red, environmental.** `t.TempDir()` + a 45-char test name pushes `daemon.sock` to 131 bytes and `ValidateSocketPathLength` correctly refuses. `TMPDIR=/tmp/h` → **PASS in 6.95s**. **The DOT path is healthy** — full remote lifecycle over SSH lands on main and reaches origin. One-line `TMPDIR` pin. |
| `RestartRecovery_QM002bDeadlock` | **False red.** Behaviour changed correctly under `hk-qkahq`; the wedge is still prevented via `paused-by-failure` + QM-027. |
| `EM015e_NoProgress_ReviewerNotLaunched` | **True red — and now GONE, 2026-07-30.** Dead emitter. It did die with `reviewloop.go`, in `3cec5afd7`. The property survives on DOT. Nothing to do. |
| `MultiBead_SerializedNCompletion` | **True red — genuine lost-commit race.** 5/5 including isolated on a quiet box; *which* beads lose varies. Files are non-colliding by construction, so a merge race is the only explanation. → `hk-co8g8`. |
| `ConcurrentMultiQueue_N2_HappyPath` | **True red.** `structural / protocol_mismatch` on the 2nd and 3rd dispatch — a deterministic ordinal, not load. → `hk-t2d7n`. |
| `T6_10BeadSequentialDrain` | **Confirmed load flake.** Passes 3/3 on a quiet box at both commits. No action. |
- **The assertions should stop encoding scheduling luck.** The durable fix is not to re-diagnose these
  every quarter but to make them assert the *invariant* rather than the *count* — for Throughput, that
  distinct-run_id-per-dispatch with no double-emit is exactly the property that survived, and exactly
  what the test should have been checking instead of `exactly 10`.

**Sequenced after §5.1's gate**, not before: making the tier merge-blocking is what stops the set
growing, and there is no point hardening tests nothing runs. Tracked as `hk-97gcz`.

---

## Sequence

**⚠ Re-measured 2026-07-30. Six of these seven rows are done, overtaken, or name a file that no
longer exists.** The live order now lives in [`DECOMPOSITION-MAP.md`](DECOMPOSITION-MAP.md) §3, where
the steps are written and numbered. This table is a record of what was planned, not a plan. The
"blocks" column is kept because it says why each row was ordered where it was.

| # | Step | Status 2026-07-30 | Blocks |
|---|---|---|---|
| 1 | Reconcile `origin/integration/phase-reviewloop-20260725` | **DONE.** The remote branch is gone — `git branch -r \| grep integration` returns nothing. Its tip survives at `origin/salvage/reviewloop-kernels-20260729`. See §Deferred item C | nothing |
| 2 | Agent instruction changes (§2) | **MOSTLY DONE.** §2.1, §2.2, §2.3-doc, §2.6 and §2.7 all landed. §2.5's ratchet and explicit shrinking grandfather list and §2.8's structure-block have no implementation. ⚠ The SHA this row used to cite for §2.6 (`734f283a7`) is not an ancestor of this branch — the commit is `ab12d65ba`. | dispatching any new agent work |
| 3 | Scenario tier merge-blocking (§5.1), then harden the load-sensitive family (§5.2) | **SPLIT, and the second half was reversed on purpose.** The reporting half landed 2026-07-29. The gating half is deliberately deferred — see §5.1. `hk-ynohn` (half the tier skips in CI) is a new prerequisite. | trusting any green build |
| 4 | Deletion steps 2–4 of the predecessor plan | **DONE.** | the rewrite |
| 5 | Subsystem partition — config-driven enable/disable of each part of the system | **LANDED — eight switches ship today.** See §6. | the rewrite's shape and its priority order |
| 6 | Decompose the run machine — **`workloop.go` + `dot_cascade_core.go`** | **IN PROGRESS, and it is a two-file job now, not three.** `reviewloop.go` was in this row and is deleted (`3cec5afd7`). `DECOMPOSITION-MAP.md` §3 steps 0–4 are done and step 5 is being written. | — it is the point of all this |
| 7 | `.dot` fixture relocation + WG-036 amendment (§4) | **NOT STARTED.** Still true exactly as written. | nothing — do it opportunistically |

**Everything not on that list is deferred.** The failure mode of this project has never been running
out of things to do; it has been doing the interesting adjacent thing instead of the load-bearing one.
Steps 4–5 are undone by the next agent that names a test file after a bead, so §2 stays ahead of them.

---

## Two traps in the sequence above — read before running step 4

Both found 2026-07-28 by re-verifying the superseded `plans/2026-07-24-code-health-audit/` against the
tree as it stands. Both are silent: nothing fails, work just disappears.

**Trap 1 — the zero-caller sweep will delete the queue transaction substrate.**
`internal/queue/transaction.go` (1,005 LOC, unchanged) is reached via `queuewiring.QueueStore.Transact`.
It is **unfinished, not dead**, and this plan lists it under KEEP. **It needs an explicit carve-out in
the step-4 selector, alongside `internal/workflow/scenario/` and the `//go:build scenario` daemon
files.**

> **The "one caller, and it is a test" half is FALSE as of 2026-07-30.** `Transact` now has a
> production caller: `internal/daemon/scheduler_reservation.go` `reserveQueueItem`, at two call sites,
> landed in `b029f9ce1` ("the dispatch stamp is one durable write"). Command:
> `grep -rn '\.Transact(' --include='*.go' .` — the non-test hits are in `scheduler_reservation.go`.
> The trap itself is unchanged and still worth the carve-out, because most of the substrate is still
> unreached. Only the sentence about the caller count was wrong.

**Trap 2 — a `ready` kerf work will overwrite `specs/`, including the correction at this branch's tip.**
`.kerf/works/reviewloop-decoupling/` holds 11 unlanded spec drafts, among them a `run-state-machine.md`
numbered **0.2.1 — the same version as the input-ack correction landed on 2026-07-27**. `kerf finalize`
copies drafts into `specs/` wholesale, so running it overwrites that correction plus ten other specs
with pre-correction text. This plan names `specs/` the rewrite oracle. **Do not finalize that work.
Resolve or abandon it before any spec triage begins.**

> **Still live, re-checked 2026-07-30.** `.kerf/works/reviewloop-decoupling/05-spec-drafts/` still holds
> 11 drafts, and its `run-state-machine.md` still declares `version: 0.2.1` — the same version as
> `specs/run-state-machine.md`. Nothing has been resolved or abandoned.

---

## 6. Subsystem partition — turn parts of the system on and off by configuration

> **⚠ STATUS 2026-07-30 — the mechanism LANDED. This section is now planning for work already done,
> and it is kept only for the operator quote and the core-set statement below.**
>
> `internal/projectconfig/subsystems.go` ships **eight** switches — `reconciliation_scheduler`,
> `socket_listener`, `dashboard_gate`, `movement_governor`, `crew_idle_reap`, `bandwidth_tuner`,
> `branch_reaper`, `worker_report_loop` — and all eight are gated at their construction seams, not made
> inert. So RA-3, RA-4 and RA-6 below are partly executed: the "the name exists, the call site does not
> yet" wording in RA-6 and in item A is **stale**. The `$TMUX` fail-fast is also out.
>
> **What is left is coverage, not design, and it moved to `DECOMPOSITION-MAP.md` §3 step 12.** Nine bus
> consumers are still constructed with no switch at all, and six of the subsystems the charter puts
> outside the core — comms, crew, captain, keeper, live-state, subscribe — have no switch of their own.
> They ride `socket_listener`, which is one coarse switch over eleven things.
>
> `CHARTER.md` §2 and §3 now own the stable statement of the partition and the core set. Read those, not
> this.

**Operator direction, 2026-07-28.** Before rebuilding or fixing anything, build a simple partition that
decides — at startup, from configuration — which parts of the system are running: queue, comms, keeper,
crew, and the rest. Each subsystem becomes something you can opt into or out of.

> ## ⚠ THE MECHANISM HAS SHIPPED. Verified 2026-07-30.
>
> This section is written as "this needs planning before implementation". The planning happened and so
> did the implementation. `internal/projectconfig/subsystems.go` declares a closed set of **eight**
> switchable subsystems, and each has a real gate at its construction seam:
>
> `reconciliation_scheduler`, `socket_listener`, `dashboard_gate`, `movement_governor`,
> `crew_idle_reap`, `bandwidth_tuner`, `branch_reaper`, `worker_report_loop`.
>
> An unknown name under `subsystems:` is a hard start-up error, not a silent ignore.
> `SubsystemsConfig.Enabled` returns **true for anything not explicitly switched off**, so every
> subsystem is on by default and the config is opt-*out*, not opt-in. Landed across
> `51143d5b1`, `f6408b861` and `e4abbd67b`.
>
> Two statements elsewhere in this document say "the name exists, the call site does not yet" — one for
> `SubsystemMovementGovernor` in §Deferred item A, one for `SubsystemDashboardGate` in RA-6. **Both are
> false.** `grep -rn 'SubsystemMovementGovernor\|SubsystemDashboardGate' --include='*.go' .` finds real
> gates in `internal/daemon/bootworkloop.go`, `internal/daemon/movementgovernor.go` and
> `internal/daemon/dashboardgate.go`. Each has been corrected in place.
>
> What is NOT done: the queue itself is not yet a standalone subsystem, the default-on posture is the
> opposite of what RA-3 asks for `crew_idle_reap`, and no subsystem has been *deleted* on the strength
> of being switchable. The prose below still describes the goal correctly. It no longer describes the
> starting point.

**Why it comes before the rewrite, not after.** It lets the rebuild start with a small subset of the
product and make *that* genuinely robust before anything else is switched on. Every previous attempt has
had to hold the whole system live in order to run any of it, which is why a defect anywhere blocked
progress everywhere and why "fix the bug" always meant "fix the bug in a rotten system."

**Its second job is prioritisation, and that may be the more valuable one.** Deciding what may be
switched off forces the question of what is actually core. The list of subsystems that must be on for
the product to do anything at all *is* the rewrite's priority order — derived from a real constraint
rather than argued about. Expect the exercise to demote things everyone assumed were central.

This needs planning before implementation: what the partition boundaries are, what a disabled subsystem
does at its call sites (absent, or present-and-inert), what the minimum viable "on" set is, and how the
partition is expressed in config. Do that planning after the deletions land and before the run-machine
decomposition starts — the decomposition should be shaped by the partition, not retrofitted to it.

### The target, stated by the operator 2026-07-28

> *"I like the idea of a clear concise architecture that segments out the subsystems and then stitches
> them back together. It should also focus on the most critical parts of the system (queue) and make
> things composable — meaning not all the parts of the system need to exist for the system to run.
> I'm COMPLETELY ok if we get done with part of the re-write and the only thing that 'works' or is
> hooked up is the queue + beads processing. Comms, crews, keeper, all can come later."*

**This is the acceptance criterion for the rewrite, and it is a permission as much as a target.** A
rewrite that ends with only the queue and bead processing working is a SUCCESS, not a partial one.
Nothing else has to be reconnected to declare the core done.

Three consequences that should settle arguments later:

1. **Segment, then stitch.** Subsystems are separated first and composed back together explicitly at a
   composition root — not left implicitly entangled and documented as if they were separate.
2. **The queue is the centre.** Where effort is contested, it goes to the queue and the bead-processing
   path. `internal/queue` is 7,718 production lines with the durable transaction substrate still
   unfinished (see Trap 1) — that substrate is core work, not a deferred nicety. (Re-counted
   2026-07-30 with `find internal/queue -name '*.go' -not -name '*_test.go' | xargs wc -l`. This said
   7,627 in two places.)
3. **Composability is the test of the design.** If the system cannot run without a given subsystem
   present, that subsystem is entangled with the core and the entanglement is the defect.

**THE CORE SET — measured 2026-07-28, confirmed by the operator the same day. This is decided, not proposed:**

> **config → event bus → queue → bead-ledger adapter → worktrees → harness registry + one substrate → work loop → merge**

   Operator: *"That seems like a fantastic set to start with. Comprehensive but tight."* Treat it as the
   boundary of the rewrite's first target. Adding to it requires a reason; everything absent from it is
   deferred by default rather than by argument.
   **Comms, crew, captain, keeper, dashboard, live-state, subscribe and the sentinel are all outside
   it** — as is the socket listener itself (`harmonik run <bead-id>` already runs work without it);
   operator decision 2026-07-28, recorded in `CHARTER.md` §3, extracted when it blocks the
   decomposition and PL-003 amended rather than obeyed.
   **The reviewer is wrongly fused INTO the core and must come out** — welded into the work loop
   instead of being a switchable stage, a large part of why `beadRunOne` is 1,764 lines (re-measured
   2026-07-30; this said 2,289, see §2.5).
   The hard `$TMUX` fail-fast is **cleared to come out** — operator reversal 2026-07-28, recorded in
   `CHARTER.md` §3, which reopens locked decision #4 and is the single source for that decision. Scope
   it from there: the daemon must be able to *run* without tmux; tmux is not removed and stays the
   default. Any contradicted requirement is a named spec amendment, not a silent violation.

---

## Two ways the mechanical selector lies — both hit during step 2, both silent

The compiler is a good oracle for "does it still build", and a useless one for these. Neither was
caught by `go build`, `go vet`, or any of six tag-qualified vet passes.

**The cascade must come from the compiler, never from filenames.** A name-based expansion pass swept in
`internal/daemon/daemon_test.go`, which holds the package-wide `TestMain` that points
`HARMONIK_CLAUDE_CONFIG_PATH` at a temp file and isolates the whole daemon test package from the real
`~/.claude.json`. Deleting it **compiled clean** and silently broke a scenario test with a
`RemoveAll: directory not empty` race. Compiler-minimal set: **685** files. Name-based set: **715**.

**The bead-ID pattern has false positives, and they cluster on the boundary subsystems.**
`internal/harness/pi/live_hktwin_test.go` matched the `hk`-prefixed regex — but `hktwin` is
*"harmonik twin"*, not a ticket; its header reads `Bead: M6 WS3-pi`. It is the sole definer of
`TestPiA_LiveSingleTurn`, and the Makefile's `test-pi-live` runs `-run TestPiA_`, which **exits 0 when
it matches nothing**. Deleting it leaves the only real-hardware Pi oracle green while running zero
tests. `scripts/harnesspi-freeze-gate.sh` check (6) exists for precisely this hazard and would still
have passed, because it only asserts the Makefile no longer points at `./internal/daemon`.

Generalise before step 3: **a `-run` filter that matches nothing is a passing target.** Any deletion
that removes the last definer of a name referenced by a live `-run` pattern converts a gate into a
no-op. Sweep the Makefile, `*.sh`, and `*.yml` for `-run` patterns after every deletion step.

---

## Deferred — real work, deliberately not now

Each of these has evidence already gathered and a reason it is not the current job. **Do not start any
of them before step 5 above is underway.** Operator direction, 2026-07-28: *"We are not building or
fixing bugs. We spent weeks fixing bugs in a rotten system and building more and more tech debt."*

**A. Which part of flywheel we actually want — ANSWERED 2026-07-28, and part of it is a deletion, not a deferral.**

The name covers **two** systems and only one was deleted. The TypeScript agent that *replaced* the
human orchestrator (`.pi/extensions/flywheel/`, 9,038 lines) was deleted in `353fc3c1e` on 2 July
after it fork-bombed the machine to load average 249 — it auto-loaded into every session and polled
with no backoff. The second system is Go, lives in `internal/sentinel/`, was never deleted, and **is
running now**. `specs/flywheel-motion.md` §0.2 is the *second* system's spec forbidding a rebuild of
the first — not a self-prohibition.

**The movement governor is dead by measurement.** It has run in observe-only mode since 21 June and
emitted **34,125 `governor_signal` events — 12.4% of the entire 102 MB event log**. Replaying them
against what acting mode would have done: 61% nothing, **26% block all dispatch and spawn an adversary
session, 13% kill the daemon** — **4,520 daemon self-kills in five weeks**. It scores only commits,
merges and bead-closes, so design work, planning, and a quiet weekend are indistinguishable from
stalled. It also carries a documented 25–50% daemon-CPU hazard from scanning an event log it is itself
filling. The reason no disposition record exists: the flip-to-acting decision was raised 22 June, never
answered, and a backlog reset on 12 July bulk-closed every flywheel bead including the one whose only
job was to write that record.

**Delete during the decomposition, not after** — ~190 lines of the governor are near-duplicated *inside*
`workloop.go`. Carrying dead-by-measurement code through a rewrite is how it becomes permanent. Also
delete the positive loop and goal-keeper. **`internal/cognition/` is already gone** — `ls
internal/cognition` reports no such directory as of 2026-07-30.

**Operator confirmation, 2026-07-28** — the measurement matched the operator's independent recollection:
*"I'm almost positive that should be pulled, and we probably need to consider removing. I think it was
added early on and basically always has been a problem."* Sequence is unchanged: gate it out of the core
first, then delete it during the decomposition.

> **The gate now exists. Corrected 2026-07-30.** This paragraph said "`SubsystemMovementGovernor` — the
> name exists, the call site does not yet". The call sites are real:
> `internal/daemon/bootworkloop.go` and `internal/daemon/movementgovernor.go` both guard on
> `cfg.Subsystems.Enabled(projectconfig.SubsystemMovementGovernor)`. So the first half of the sequence
> is done and only the deletion is left. **But the observe block still runs on every default
> deployment**, because `SubsystemsConfig.Enabled` returns true unless the operator explicitly writes
> `enabled: false`. Switching it off is now one config line rather than a code change.

The governor's observe block is a `br ready` shell-out plus an `events.jsonl` scan every two minutes,
emitting `governor_signal`, which still has no **Go** consumer as of 2026-07-30
(`grep -rn 'governor_signal' --include='*.go' .` finds only the emitter, the event-type declaration and
the config comment). `scripts/ops-monitor-check.sh` does list it in `ACTIONABLE_EVENT_TYPES`, so the
shell side is not quite nothing. Treat the 34,125-event and 12.4% figures as a point-in-time reading —
`internal/projectconfig/subsystems.go` says the same thing in its own comment. Re-derive before
quoting.

**And a wider direction that falls out of it — audit every bead query outside the queue path.**
Operator, same day: *"A lot of the bead calling to search beads (outside the normal queue stuff) should
be closely looked at and questioned as far as its value."* The governor is one instance: a background
loop shelling out to `br` on a timer to infer fleet health from a coarse count. The question to ask at
each site is what decision the query feeds and whether that decision should be automatic at all — the
§5 pattern predicts most of them score "no" on the second half. This is an audit to run during the
decomposition, not a sweep to start now.

**Keep ~630 lines that are not flywheel at all:** `ComputeSnapshot` + `DetectLayerA`
(`internal/sentinel/signals.go`, `layera_hkl087e.go`) came from different July work that borrowed the
package name. They detect per-run stalls — heartbeat gap, review-loop wedge, run age — are tested, have
zero production callers, and their judgment is evidence-local, which is exactly where the fleet-wide
governor fails. Still true 2026-07-30: `grep -rn 'ComputeSnapshot\|DetectLayerA' --include='*.go' .`
outside `_test.go` files finds only their own definitions and two comments.

**Three landmines when cutting:** `DecisionBlocker` (`decision_block_ev043a.go`) is the general
human-in-the-loop mechanism with several callers — remove only its `AddQueueBlock("sentinel")` site;
`eagerfill_em063.go` holds both the live eager refill and a never-fired staged-bead generator, so split
it rather than delete it; and `.flywheel/skills/sentinel-adversary.md` is a **runtime dependency** of
`sentinel.SpawnAdversary`, not documentation.

**Dispositions:** gut `specs/flywheel-motion.md` to a stub preserving §0.2 and §0.3; delete
`specs/cognition-loop.md`, `HANDOFF-flywheel.md`, `.flywheel/`, and the `sentinel:` config block; keep
`harmonik supervise`'s `-flywheel` tmux name for now (load-bearing). Correct or delete
`.kerf/works/flywheel-motion/06-completion-plan.md` — it still claims `sentinel.Evaluate()` has zero
callers, which stopped being true on 21 June, making it the most misleading document in the tree.

Worth keeping from §0.3 before that spec is gutted: *"Drift and over-deference are beaten by
independence or determinism — never by more prompt text in the same context."*

**B. Spec triage — good vs crufty, and what to build.** Operator: *"before we start building we will
need to go through the specs and identify what is good and useful, and what is old and crufty"*, and
separately: walk a good-size sample to see whether each feature exists and whether we still want it —
delete some, mark others not-built and build them later. Measured 2026-07-28: **1,164 requirement IDs,
293 uncited (25%)**; of a 119-ID sample, 48% are implemented-but-untagged, **28% stale**, 13% never
built. **⚠ That pair contradicts §1 above, which says 1,180 and 291 for the same corpus. Re-measured
2026-07-30 the total is 1,180 and the orphan count is 325 (28%) — see the recount box in §1. Use those.
The 1,164 / 293 pair here reproduces from no command tried in this sweep.** **Seven** requirements would regress working code if a rewrite obeyed them — `AR-017` is the
worst: a closed list of out-of-process actors that omits tmux, the supervisor, the keeper, `git`, and
`gh`, so a rewrite obeying it would design a process model that cannot host the running system. Also:
eight spec files declare **zero** requirement IDs and are unfalsifiable by construction, and
`specs/cognition-loop.md` maps the flywheel subsystem deleted on 2 July.

**C. Harvest the abandoned integration branch, then delete it.** Decided 2026-07-28: `origin/integration/phase-reviewloop-20260725`
is **abandoned, not merged**. 25 of its 44 commits are probe garbage, `workloop.go` is byte-identical
to ours, its two new packages have zero importers anywhere, and its ssh and queue-harness fixes are
already on this branch verbatim. Merging would *regress* the input-ack correction landed six days ago
(the branch still carries the `Degraded` outcome this branch removed) and demote `specs/execution-model.md`
from `reviewed` to `draft`. One commit — `88f36c15d` — carries operationally-earned constants worth
hand-copying into our specs: the reviewer time budget (10 min base, +10 min/kLOC, 60 min ceiling,
75 s one-shot reseed), a session-identity checkpoint that must be written *before* `version_selected`,
close-plus-`needs-attention` as one crash-convergent transaction, and a no-progress detector comparing
HEAD SHAs instead of diff hashes. Harvest those clauses by hand, then delete the remote branch.

**Revised 2026-07-28 after a second pass:** ~37,292 lines of that 44-commit diff is `internal/specaudit`
— the test mass already deleted here. The real payload is ~3,130 lines in `internal/runloop`:
`continuity` (457 LOC) and `reviewcycle` (606 LOC), two reviewed and tested **pure decision kernels
extracted from `runReviewLoop`** and never wired. Since `reviewloop.go` is now in the rewrite scope,
that extraction is prior art for the rewrite rather than junk. **Cherry-pick those two packages; still
do not merge the branch.**

**STATUS 2026-07-29 — the spec half of this item is DONE; the branch is now safe to delete.**

The `88f36c15d` spec clauses were harvested by hand, verified against `internal/daemon/reviewloop.go`
rather than taken on trust, and landed in `specs/execution-model.md` at v0.9.6 as a named amendment.
Both drifts were confirmed in code: `emitNoProgressDetected` has zero call sites and the live path emits
`review_fixup_stalled` instead, and a flagless `REQUEST_CHANGES` is treated as an implicit approval on a
branch sitting *ahead* of the iteration-cap check. The lane deliberately did **not** take the
`reviewed`→`draft` front-matter demotion, EM-015d-KERNEL/-CONT/-RVA, the WM-027a reviewer projection, or
the EM-015f revert. It also recorded a dated implementation gap rather than asserting conformance: four
of the five reserved context keys have no durable carrier at all.

**The branch tip is preserved at `origin/salvage/reviewloop-kernels-20260729` (`30d4e08`)**, which this
manifest's own convention marks as protected. So `origin/integration/phase-reviewloop-20260725` can now
be deleted with nothing at risk, and the cherry-pick decision below is no longer on a deadline.

> **DONE, 2026-07-30. The integration branch is deleted.** `git branch -r | grep -i integration`
> returns nothing. The salvage branch is still there —
> `git branch -r --list 'origin/salvage/*'` lists `reviewloop-kernels-20260729` alongside
> `reviewloop-decoupling-20260729` and `hk-hzj-984ec58c`. Nothing further to do on this item.

**The cherry-pick recommendation is REVISED by the Phase 3 reframe, and split.** The original rationale
was "`reviewloop.go` is in the rewrite scope, so this extraction is prior art for the rewrite." Phase 3
is now a *deletion*, not a rewrite, which cuts the two packages apart:

- **`reviewcycle` (606 LOC) — do NOT harvest.** Its entire subject is the review-loop decision cycle,
  and that is being deleted rather than rebuilt. Pulling it in would re-add unwired code of exactly the
  kind Phase 1 spent ~225,000 lines removing.
- **`continuity` (457 LOC) — harvest WHEN the gap it answers is worked, not before.** Its subject is not
  review-loop-specific: it is the implementer continuity checkpoint, and it models the identity handshake
  as a policy split between `IdentityMinted` (identity known before first work) and `IdentityCaptured`
  (harness reveals its native identity only after launch). That is precisely the codex/pi asymmetry
  behind the 1-of-N findings, and it is a designed answer to `hk-5sebh` — crash-recovery resume being
  review-loop-only, and therefore about to leave the product with `reviewloop.go`. It is a pure kernel
  with a `purity_test.go` and effects behind narrow ports, which is the right shape. But it is still
  unwired code, so it should arrive attached to the work that wires it, not ahead of it.

> **⚠ CORRECTION, 2026-07-29 — the two bullets above were written on a false premise and are now moot.**
> **Both packages were already in-tree**, not waiting on the integration branch: `internal/runloop/continuity`
> and `internal/runloop/reviewcycle` were present at HEAD, and `continuity` arrived via a commit literally
> titled *"feat(runloop): harvest continuity checkpoint kernel"*. The cherry-pick this item asks for had
> **already been done** — the item was stale, and so was my reasoning about whether to do it.
>
> Measured state at the time of correction: **both had zero importers anywhere in the tree.** They were
> unwired dead code, of exactly the kind Phase 1 removed ~225,000 lines of. The review-loop retirement
> deleted both (~2,600 lines including their tests), which is the right call — `reviewcycle` is the
> review-loop decision cycle by definition, and `continuity`'s own package doc frames it as *"the
> review-loop implementer continuity checkpoint"*.
>
> **The design insight is still worth keeping even though the code is gone**: the minted-vs-captured
> identity split is a real distinction that the live code does not model, and it maps directly onto the
> codex/pi asymmetry in `ONE-OF-N-DRIFT.md` N3. Whoever works `hk-5sebh` should read `continuity.go` from
> `origin/salvage/reviewloop-kernels-20260729` for the design, not to re-import the package.
>
> **Do not confuse these packages with the actually-wired resume path.** `hk-5sebh` is about
> `persistClaudeSessionID` in `internal/daemon/sessioncontext_chb023.go`, which is live and separate.
> Deleting these two kernels does not touch it.

Recorded as a decision rather than an omission: if wiring `continuity` later proves wrong, the salvage
branch above is the recovery path.

**D. Worktree and branch cleanup — DONE 2026-07-28.** Operator: *"after the delete, we need to do a
worktree cleanup and branch cleanup. This house is a mess."* Two counts here were wrong and are
corrected: the registered worktree count was **37**, not 29 (31 removed), and **five** worktrees had
**forked bead ledgers**, not two — `arch-01-contract`, `extinguish-mvh`, `input-ack-contract`,
`subsystem-partition-01`, and a `reviewloop-finalize` tmp worktree. All five forks were verified empty
before removal, so nothing was lost, but the hazard behind them is unchanged and still worth the
warning: running bare `br` inside a worktree silently creates a fresh `.beads/beads.db` and issues IDs
from a new namespace, with no warning and exit 0. 40 provably-merged local branches were deleted;
at the time, **359 local and all 129 remote branches remained**, untouched.

> **The branch counts have moved a long way. Re-counted 2026-07-30** with `git branch | wc -l` and
> `git branch -r | wc -l`: **383 local** and **32 remote**. Local went *up*, because agent worktrees
> keep creating branches. Remote came down from 129 to 32, so remote branch cleanup did happen even
> though this item was never worked. Local branch cleanup is still open.

**Five worktrees were deliberately kept, and this is the part that has to survive.**
**⚠ Two of the five are already gone, checked 2026-07-30 with `git worktree list`.**

| Kept | Why |
|---|---|
| `harmonik-wt/cq-01` | 8 staged files incl. an unfinished `internal/queue/transaction_store.go` — the queue-transaction work Trap 1 protects. **Still present.** |
| `harmonik-wt/lift-l8-reviewloop` | 2 staged renames moving `reviewloop.go` into `internal/runloop`. **GONE, and it no longer matters**: `reviewloop.go` was deleted outright in `3cec5afd7`, so there is nothing left to move. |
| `harmonik-wt/arch-01-contract` | untracked `.kerf/` artifacts. **Still present.** |
| `harmonik-wt/cq-mig-01` | untracked evidence under `plans/`. **Still present.** |
| `/private/tmp/harmonik-main-integration-20260725` | staged for the item-C hand-harvest. **GONE**, and item C is complete, so nothing is at risk. |

Also kept, and **not** a worktree despite living under `harmonik-wt/`:
`harmonik-wt/kilo-preserved` is a plain directory holding two deliberately-named evidence patches
(`hk-o7x4w-BLOCKED-do-not-merge.patch`, `hk-pvrfx-UNCOMMITTED-under-review.patch`). It has no `.git`
and `git worktree list` cannot see it, so a worktree-driven sweep will neither remove it nor warn you
it exists.

~~One thing needs a human look: **`/tmp/hk-rqhz3.2ebLUi/clone` (271 MB)**, an unregistered
self-contained clone left by a `harmonik init` smoke test on 2026-07-24.~~ **Resolved by attrition.**
That path no longer exists as of 2026-07-30 — `ls -d /tmp/hk-rqhz3.2ebLUi/clone` reports no such file.
It was almost certainly swept with the 14 GiB of `/private/tmp` scratchpads reclaimed the same day. No
human look needed.

**E. Delete `plans/2026-07-24-code-health-audit/` — 129 files, 17,831 lines, all tracked. Still present
and still undeleted on 2026-07-30** (`find plans/2026-07-24-code-health-audit -type f | wc -l`, then
`xargs wc -l`; the line figure here said 17,813).
It is ~10% live evidence and ~90% dead process; 13 of 92 tasks ever completed and every file with real
content belongs to one of those 13. Its still-true findings are already folded into this document
(the two traps above, the corrected measurements, the branch revision). Before `git rm -r`, keep
pointers to the two kerf works that survive *outside* it: `queue-transaction-contract/07-tasks.md`
(a 21-slice plan, 1 slice built — this is what Trap 1 protects) and
`run-architecture-contract/04-design/c6-migration-gates-design.md` (a baseline/target complexity table
per decomposition step — prior art for the run-machine seam map). Do **not** keep `TASK-INDEX.yaml` or
the task cards under any framing; they are verbatim the disease this plan names.
It also holds ~30 recorded defects that never reached the ledger, five confirmed live today — worst is
`reconcileOrphanedRunsOnResume` scanning `run_started`-minus-terminal without excluding live run IDs,
so it can reset a bead **under a live agent**. We are not fixing bugs now; carry the list forward, do
not rediscover it.

**F. Verify the sandbox-gate consolidation on a real run — the code landed 2026-07-29, the check did not.**
Operator direction the same day: *make the fix now, do the verification later when we test the whole
system.* This is that later.

The three launch sites each carried their own sandbox scope. They now all ask one gate,
`sandboxSpawnForRun`, and `sandbox.harnesses` in `.harmonik/config.yaml` is the only switch. Three
source-level tests in `internal/daemon/agentlaunch_scope_test.go` guard the shape. No call site
re-scopes, the gate call stays unconditional, and the gate's answer is never overwritten afterwards.
All three were mutation-checked. **Do not read the third as surplus** — it is the one that closes the
real hole. A second gate written as `sandboxSpawn = nil` after the call keys off an existing field, so
it adds no new name and leaves the call unconditional, which means the first two guards read it as
clean.

**What is NOT verified, and why it was deferred:** the shape tests cannot prove the gate does anything.
Proving that means reaching the srt engagement probe, which shells out to the real `srt` binary — a
whole-system check, not a package test. Two things to confirm when the system is next exercised end to
end:

- **A pi run on the graph path is still sandboxed and still passes.** Pi is the only harness listed
  today, and it already took this path before the change, so this is a no-regression check.
- **The build-cache redirect now firing on graph runs does no harm.** This is the one genuine behaviour
  change in the consolidation. It was previously scoped to single-mode launches. A graph run that builds
  inside the sandbox hit the same denied write with no redirect, so the change should fix a failure
  rather than cause one — but it has never run. The redirect sits inside the captured-session-id branch,
  so the real delta is exactly "pi graph nodes now get `GOCACHE`/`GOPATH`".

**One consequence to know before editing `sandbox.harnesses`.** Consolidating did not remove the case the
old write-up was afraid of. It made it reachable by one config line, which is the point of having a single
switch. Adding `claude` to that list now does three things at once: it sandboxes every graph claude node
for the first time, it sandboxes the cognition gate for the first time, and it arms `verifySandboxEngaged`
in front of both. That check is fail-closed, so a harness the sandbox cannot engage for stops launching
rather than launching unprotected. That is the behaviour we want, and it is still a bigger step than the
one-line diff looks like. `.harmonik/config.yaml` is machine-local and gitignored, so this note is the only
place the warning can live.

**Related, and larger than a verification:** the redirect handles the **Go** toolchain only, because Go
is what this repo builds. Every language with a writable cache under `$HOME` has the same problem inside
the sandbox — Rust's `CARGO_HOME` is the one we expect to need next, and node, Python and Java all
qualify. Operator, 2026-07-29: *we'll want to support similar things for other languages.* When the
second toolchain arrives, the hardcoded `GOCACHE`/`GOPATH` block in `agentlaunch.go` should stop being
Go-specific and become a per-language cache redirect the sandbox config names, so adding a language is
config rather than code. Do not build that for Go alone — one instance is not yet a pattern, and the
consolidation rule below is about removing duplication that exists, not pre-empting it.

**Standing caveat:** the operator has paused sandbox support as a program and wants remote and
containerized execution, which may replace srt outright. If that lands first, this whole item is
deleted rather than done.

---

### The rewrite is built to `PRINCIPLES.md`

[`PRINCIPLES.md`](../../PRINCIPLES.md) — repo root, 84 lines, added 2026-07-15 — states the eight
principles this codebase is supposed to be built on. **Until 2026-07-28 it had ZERO inbound references anywhere in the tree** —
`git grep -l "PRINCIPLES.md" HEAD` returned nothing. The link direction was one-way: PRINCIPLES.md
cites `plans/2026-07-13-code-revamp/`, not the reverse. So no agent ever loaded it. It is now cited from
`AGENTS.md` and `AGENT_INDEX.md`.

The cost of that omission is measurable. Its §6 reads: *"Beware test theater: a suite that mostly
asserts constants is not coverage. 'Green' must mean the product code actually ran."* Thirteen days
later this project deleted ~225,000 lines of exactly that. The warning was already in the tree.

Four of the eight map directly onto work already planned, which is a good sign the principles are real
rather than aspirational:

- **§8 prove one vertical, then generalize** — this is the operator's own method for the rewrite
  (queue + bead processing first, everything else later), independently re-derived.
- **§2 consumer-owned ports** — the decomposition seams for `workloop.go` / `reviewloop.go` /
  `dot_cascade_core.go`.
- **§4 time is a port** — directly addresses the mutable package-level timing `var`s identified as the
  single biggest blocker to rewriting the run machine.
- **§5 explicit state machines, single writer** — the diagnosis of `beadRunOne`, which open-codes the
  same transitions in four places.

**One tension to hold consciously.** §7 says "enforce the principles with CI levers, not vibes", while
the operator's direction is *"I dont want to build more guards and crap to maintain."* These reconcile:
most of §7's named levers already exist (`.golangci.yml` complexity ceilings, depguard boundary rules,
the `--new-from-rev` ratchet, the `scripts/*-gate.sh` set). The rule is **use the levers that exist;
do not build new ones without a reason that survives being questioned.**

### A worked example of why this section exists: AR-009

An agent reported that `specs/architecture.md` **AR-009 forbids configuring subsystems away**, and that
the partition work would need a spec amendment. **It does not, and it was never checked before being
repeated.** AR-009 reads: *"Every harmonik deployment MUST include a representation of search, a
representation of verification, and a representation of traces. Removing any one of the three from a
deployment is not a valid configuration."* Those are abstract foundation mechanisms — backtracking
transition kinds, a verifier role-function, the Transition record. **None of them is comms, crew, the
keeper, or the socket.** The requirement says nothing about the partition work.

Two lessons, and they are the reason for this whole section:

1. **A requirement that sounds like a blocker will be repeated as one.** Nobody opened AR-009 until the
   operator asked what it actually meant. Cost: a spec amendment nearly proposed against a requirement
   that did not apply.
2. **Its enforcement was a "corpus presence test"** — per the spec's own §10.2, a check that certain
   terms appear in the markdown corpus. It constrains documents, not code, and the sensors enforcing it
   were among the 129 prose-grepping files deleted on 2026-07-28. It is now unenforced and
   unenforceable. `architecture.md` scores 61% of its requirement IDs appearing in no Go file; the
   standing triage verdict is to keep §4.1–4.5 and delete §4.0/4.6/4.10, which constrain documents
   rather than code. AR-009 is in that territory.

**G. Centralize durable writes — one owner per file, and make a second writer impossible.**
Operator, 2026-07-30: *"persistence across multiple threads/processes on THE SAME FILES should not be
allowed. That seems like bad news."*

It is already the spec. **QM-060:** *"All queue mutations MUST execute through the single QueueStore
transaction owner."* The owner exists — `internal/queue/transaction.go`, 1,005 lines, reached through
`queuewiring.QueueStore.Transact`. This is row 5 of `UNWIRED-INVENTORY.md` and it is marked keep, not
delete.

> **⚠ TWO FACTS IN THIS ITEM WERE ALREADY FALSE. Corrected 2026-07-30.**
>
> **1. "It is wired to nothing. All ten `.Transact(` call sites are in
> `queuewiring/store_transaction_test.go`."** Neither half holds.
> `grep -rn '\.Transact(' --include='*.go' .` shows the test call sites spread over three files
> (`store_transaction_test.go`, `store_precondition_test.go`, `store_quarantine_test.go`) and **two
> production call sites** in `internal/daemon/scheduler_reservation.go` `reserveQueueItem`, landed in
> `b029f9ce1`. The dispatch reservation is a real durable write through the owner. `Transact` also
> gained a `Precondition` hook and quarantine-on-any-I/O-failure with that work.
>
> **2. "bare `queue.Persist`, at about 20 sites in `workloop.go` alone."** There are **zero**
> `queue.Persist` calls in `workloop.go` — `grep -n 'queue\.Persist(' internal/daemon/workloop.go`
> returns nothing. The dispatch code moved into `internal/daemon/scheduler.go`, which is where those
> calls now live. Anyone following this sentence opens the wrong file. The "eleven files overall"
> figure does hold: `operatorevents.go`, `eagerfill_em063.go`, `perqueuespendmeter_tigaf11.go`,
> `runports.go`, `crewstart.go`, `startup_pl005_qm002.go`, `scheduler.go`, `persistence.go`,
> `rpc.go`, `cmd/harmonik/run.go`, plus a scenario helper.
>
> So the shape of the problem is unchanged — most writes still bypass the owner — but the item is one
> caller better off than it says, and it sends you to the wrong file. `UNWIRED-INVENTORY.md` row 5
> already carries the corrected version.

> **⚠ "Wired to nothing" is FALSE as of 2026-07-30, and the file pointer is wrong too.** There are
> **two production `Transact` call sites**, both in `internal/daemon/scheduler_reservation.go`
> (`reserveQueueItem`), landed by the step-4 reservation work. The `Persist` sites also moved out of
> `workloop.go` into `scheduler.go` when Seam A split. `UNWIRED-INVENTORY.md` row 5 already carries
> this correction, so **read that row, not this paragraph**, and re-run
> `grep -rn '\.Transact(' --include='*.go' internal/ cmd/` before quoting any count.
>
> **Item 1 below is still the live work, and it is now measured.** Queue status is set by direct field
> assignment at **27 production sites, in 9 files, across 5 packages**, and `Persist` is called from 9
> production files in 5 packages. That is `DECOMPOSITION-MAP.md` §3 step 11. Also note
> `ClassifyReplaceIntent` in `internal/queue/transaction.go` has **zero production callers** while
> `WriteReplacement` is now wired — intents are written and never classified on the recovery path.
> Recorded, not chased.

`Persist` itself is sound — temp file, atomic rename, fsync of the parent directory, a 1 MiB bound. Per
file it is crash-atomic. What it has no way to provide is what a transaction owner provides: a
generation guard, a replace intent, archive handoff binding, and quarantine. Those are the things that
stop a second writer, and they are exactly what `transaction.go` implements.

Two separable questions, and the second is the architectural one:

1. **Route every queue write through the existing owner.** Mechanical, already specified, already
   built. The 21-slice plan in `queue-transaction-contract/07-tasks.md` covers it. The "one slice is
   built" count predates `b029f9ce1`, which routed the dispatch reservation through `Transact` — treat
   the slice count as **unverified** and re-read the task file before quoting it. This is not new
   design work; it is finishing.
2. **Decide the general rule for the system, not just for queues.** Multiple processes will need to
   persist state — that is fine and expected. Two writers on one file is not. The rule to test every
   new persistent file against: *one owner writes it, everyone else asks that owner.* Worth stating
   once, in the architecture, rather than re-deciding per file. Defer until the queue case is finished,
   because that case will show what the general seam has to look like.

Related and already recorded: `hk-f1wb0` (nothing prunes the bead history while the daemon is off) is
the same shape one layer down — a file with no owner, so no policy reaches it.

---

## Re-assess — parts of the system whose existence is in question

**These are not bugs to fix. They are things to decide whether to keep.** Each one is a mechanism that
was built, works as designed, and may simply not deserve to exist in the rebuilt system. The rewrite is
the moment to ask; carrying them forward unexamined is how the last system accumulated.

A pattern runs through most of them, and it is worth naming because it will keep producing new
candidates: **a mechanical rule infers intent from a coarse signal, then acts automatically on that
inference.** The movement governor scored "no commits" as "stalled" and would have killed the daemon
4,520 times in five weeks (§A). Crew-idle-reap scores "quiet" as "dead". The bandwidth tuner scores a
token rate as a concurrency ceiling. In every case the honest answer is either agent judgment — which
can look and see — or an operator-set number. Not an inference.

Specs belong in this section too: the spec triage in §B is the same exercise applied to requirements
rather than mechanisms. 25% of requirement IDs appear in no Go file, and seven would actively regress
working code if a rewrite obeyed them.

**RA-1. Reassess `harmonik harness` — it is a testing system with tentacles into everything.**
Operator, 2026-07-28: *"I'm not even sure we've actually used it and it seems to have tentacles all
over."* `internal/scenario` is **4,660 lines of production code** behind the `harmonik harness`
subcommand, and `cmd/harmonik/harness.go` calls **45** exported symbols from it. (Re-measured
2026-07-30: the symbol count is exact — `grep -o 'scenario\.[A-Z][A-Za-z0-9_]*' cmd/harmonik/harness.go
| sort -u | wc -l` returns 45. The production-line figure said 4,952 and has drifted down.) Its reach
is far wider
than the call list suggests: scenario-file parsing fans out into agent overrides, fixture setup, git
seeding, file seeds, event expectations, workspace predicates, outcome expectations and cadence tags;
bootstrap reaches project-root synthesis; matrix expansion is called straight from the CLI. That reach
is why step 4 shrank from a planned ~18,000-line deletion to 860 lines — almost everything in there is
load-bearing *for the harness itself*.

Three questions to answer: **is it used at all** (check whether any run, CI job, or human invocation
has exercised it); **is it separable** from the core — it is a test system and should have no claim on
the product's centre; and **what should it be** if kept. It also has a latent defect found 2026-07-28:
`ScenarioFile.Valid()` validates every `git_seed` and `files` fixture entry, while `BootstrapFixture`
takes no `FixtureSetup` argument at all — **a scenario declaring fixture seeds parses clean and seeds
nothing.**

**RA-2. `paused-by-failure` has no exit — plus four overlapping stop mechanisms agents cannot tell apart.**
Operator, 2026-07-28: *"we need to revise how the queues can get into a bad state — I forget what it's
called, but it's really dumb. Some problem occurs and the queue is hung/stopped/whatever, then the
agents think they can't do anything. That needs to change, but we need to talk about it later."*

**NAMED BY THE OPERATOR 2026-07-28 — it is `paused-by-failure`** (`specs/queue-model.md` §2.2). *"Queues
get stuck in that state all the time and then the agents just leave them there and talk about them over
and over. It may just be the instructions that need to be updated or something. Thats there for a reason
and agents should by default 'unstick' things, and then get things moving again — not just create a new
queue and leave the other one dead until I have to tell it to clean it up."*

**It is not the instructions, and the agents are not being lazy — there is no command that clears it.**
Verified in source:

- A queue enters `paused-by-failure` **automatically** whenever any group reaches `complete-with-failures`
  (§8.3, `workloop.go`). All dispatch on that queue stops and the state survives daemon restart.
- **`harmonik queue resume <name>` does not clear it.** `internal/queuewiring/operatorevents.go` skips any
  queue whose status is not `paused-by-drain`; its own doc comment says it transitions "from
  paused-by-drain back to active". It resumes the *other* pause state. **Still true 2026-07-30** —
  every status reference in that file is `paused-by-drain`.
- `specs/queue-model.md` §8.4 states the recovery outright: *"v0.1 recovery is daemon restart followed by a
  fresh `queue-submit` after the operator addresses the failed beads; v0.2 will add `queue-resume`."* And
  QM-027 explicitly permits a fresh submit to overwrite a `paused-by-failure` queue.

So **the documented recovery procedure IS "abandon it and submit a new queue"** — exactly the behaviour the
operator is objecting to. The agents are following the spec. The spec is also now stale in a way that hides
this: `queue-resume` did ship, so a reader concludes the v0.2 gap closed, when what shipped resumes a
different state.

**The fix is a capability, not a prompt:** something that transitions `paused-by-failure → active` after
the failed items are dealt with, and an agent default of unsticking rather than abandoning. Note the
asymmetry that makes this bite — entering the state is automatic and requires no judgment, while leaving
it requires an operator. Any mechanism that is easy to enter and impossible to leave will accumulate.
**Not a priority; recorded for the RA-5 queue walkthrough.**

Beyond that specific state, there are **four separate mechanisms that stop work**, each with its own
vocabulary, its own recovery path, and no common surface telling an agent which one it is in:

1. **Queue paused** — `QueuePaused` / the `queue_paused` event. ~140 references.
2. **Quiesce / drain** — `QuiesceArbiter`, `DrainState`, `Quiesced`, plus a `QuiesceOverrideHandler`. ~210 references. Constructed unconditionally at boot.
3. **Handler paused** — `ReasonHandlerPaused` (QM-052a), a queue-*submit* gate with its own JSON-RPC error code `-32018`.
4. **Decision block** — `DecisionBlocker.AddQueueBlock`, the human-in-the-loop hold, awaiting an ack token. One of its callers is the sentinel governor that §A deletes.

An agent hitting any of these sees "cannot proceed" and stops. The design question for later is not
"fix the bug" — it is whether four stop mechanisms should be one, what an agent is supposed to DO when
it meets one, and which of them should exist at all after the core is rebuilt. **Discuss before
building.** Relevant to §6: the queue is the centre of the rewrite, so this is core work, not polish.

**RA-3. `crew-idle-reap` MUST NOT be enabled by default.**
Operator, 2026-07-28. The replacement should be **non-deterministic and agent-controlled** — a judgment
about whether a crew is actually idle, made by an agent that can look, not a timer that reaps on a fixed
rule. Deterministic reaping of a live-but-quiet session is the same failure shape as the movement
governor in §A: a mechanical rule scoring "no visible output" as "not working", and acting on it.

> **It IS switchable now, and it defaults to ON. Corrected 2026-07-30.** This paragraph said
> `crew-idle-reap` was "one of the eleven subsystems currently behind the single socket-listener
> condition" and asked that it default to off "when it becomes switchable". It became switchable in
> `51143d5b1`: `internal/projectconfig/subsystems.go` declares `SubsystemCrewIdleReap`, gated at
> `daemon.buildCommsAndCrewHandlers`. But `SubsystemsConfig.Enabled` returns true for anything not
> explicitly switched off, so the operator's requirement is **unmet** — it is on unless somebody
> writes `enabled: false`. Two things to decide, and they are separable: flip the default, and decide
> whether the body should be deleted at all. Its scan has been an operator-directed no-op since
> 2026-07-18, and it is still constructed on every boot.

**RA-4. The bandwidth tuner — a rolling-5h token-rate auto-tuner for `--max-concurrent`.**
Operator, 2026-07-28: *"I assume that may be something like token use — also dumb."* Confirmed:
`internal/daemon/bandwidthtuner.go` samples a 5-hour rolling token rate every 60 seconds and moves the
concurrency ceiling so the system "doesn't overshoot the operator's subscription bandwidth." It is
entangled well beyond its own file — `workloop.go` reads its runtime value for the per-queue worker
ceiling, it shares `PollGate` with the StaleWatcher, it needs a `bandwidthTunerBackstop` to bridge a
pre-Seal bus subscription, and Pi's rate-limit signal had to be explicitly *isolated* from it (PI-073)
because feeding one harness's limits into a global tuner was wrong.

Same shape as the other three: a mechanical rule inferring intent from a coarse signal, then acting on
it automatically. Reassess whether automatic concurrency tuning should exist at all, or whether the
ceiling should simply be a number the operator sets. If it survives, it belongs OUTSIDE the core set
and defaults off.

**RA-5. Separate the queue, and fix how much attention it demands from a crew — OPERATOR ITEM, 2026-07-28.**
*"Once we get everything in the core working, we should make sure the queue mechanism separated out and
I want to go through how that works. I dont particularly like how closely the crew has to pay attention
to the queue most of the time — we need to see if there are changes needed to it."*

**Timing is explicit and load-bearing: after the core runs, not before.** This is a walkthrough with the
operator followed by a design change, not a refactor to start unprompted.

Two halves, and the second is the interesting one:

1. **Separation.** The queue is the charter's centre (§4, "where effort is contested, it goes to the
   queue"), so it is the one subsystem that must come out of the partition genuinely standalone rather
   than merely switchable. `internal/queue` is 7,627 production lines with the durable transaction
   substrate still unfinished — see Trap 1, and note the `harmonik-wt/cq-01` worktree preserved in item D
   holds 8 staged files of exactly that work.
2. **The attention cost.** A crew currently has to watch the queue closely and continuously — the
   `crew-launch` skill's operating loop is built around polling its own named queue, and the progress
   feed is on a ≤10-minute timer while dispatching. The operator's objection is that this is the crew's
   *dominant* activity rather than an occasional one. **Related, and probably the same problem seen from
   the other side: RA-2** — `paused-by-failure` is a state a queue enters automatically and that nothing
   in the product can leave, so the documented recovery is to abandon the queue and submit a new one.
   Watching a queue closely is a rational response to a queue that can silently become permanently dead.
   Fix that first and some of the attention cost may simply go away.

⚠ **Get the mechanism right before designing against it — the obvious framing is half wrong in each
direction.** Per `.claude/skills/crew-launch/SKILL.md`:

- **There is no queue *poll loop*.** The loop is event-driven — it arms `harmonik subscribe --types
  run_completed,run_failed,run_stale,heartbeat` and advances on delivered events, with `comms recv
  --follow --json` as the primary wake. So "make the queue push instead of pull" is *already true*, and
  proposing it would waste the walkthrough.
- **But the crew does read queue state, repeatedly.** `scripts/crew-boot-digest.sh` runs `harmonik queue
  status` and `queue list --json` at boot, and the loop's own rule forbids reading an empty `br ready` as
  "drained" without *also* checking in-progress beads, epic-blocked beads, and **paused/failed queues**.
  The operator's sense that the queue demands constant attention is not a misreading of a push system.
- **There are two sweeps, and they are constrained in opposite directions.** `br ready` against the bead
  ledger is rate-*limited* — a floor on the interval ("no more frequently than every 10 minutes"). The
  one-shot inbox backstop sweep (`comms recv --agent … --json`, own cursor) is *mandated* on a **ceiling**
  — it must happen at least every 15 minutes, and it rides the idle progress-feed tick rather than
  standing alone. Separately the ≤10-minute progress-*reporting* cadence writes to two surfaces — comms
  status *and* `br` comments.

**So the attention cost is real but it is not the queue's delivery model.** The candidates worth testing
against the operator's lived experience: the dual-surface reporting cadence, two simultaneously-armed
watchers plus the two sweeps above, the drain check that must consult four sources to answer one
question, and **RA-2's four indistinguishable stop states** — an agent that cannot tell which of four
ways it is stuck will re-check constantly no matter how work is delivered. Establish which before
designing anything.

**RA-6. The dashboard forcing gate — DECIDED 2026-07-28: the coupling comes out, the idea goes on a list.**
Operator, on being told what it was: *"What is the 'dashboard' its talking about? At some point we talked
about building an operator dashboard, but I didnt think that made it anywhere. Regardless — it looks like
that coupling should be removed, and if its useful, put on a list for integration later."*

**It is not the operator dashboard, which was never built.** It is `.harmonik/context/dashboard.json`
(`internal/dashboard`), a **captain-curated planning file** — ranked current priorities, on-deck items,
expected throughput per lane, a notes field. The captain or admiral writes it; the daemon reads it.

The coupling is the **forcing gate**: `runWorkLoop`'s step 2b calls `evaluateDashboardGate` and feeds its
blocked-queue set into `selectNextQueue`, so a stale freshness stamp withholds new dispatch. That is the
§5 pattern — a mechanical rule reading a coarse signal (a timestamp) and acting on it — and
`dashboardgate.go` is the only route by which `internal/dashboard` enters package `daemon`.

**Do not overstate it, which an earlier draft of this entry did.** It is **opt-in and inert here today**:
`LoadDashboardGateConfig` returns an off gate when there is no `dashboard:` block, this repo's config has
none, and `lanes.json` is empty. It is **not silent** — the daemon emits `dashboard_stale` on the blocking
edge and `dashboard_refreshed` on recovery, and `internal/keeper/dashboardnag.go` nags the captain's pane
at 80% of the window. It does **not halt work** — it withholds only NEW dispatch on queues named in
`lanes.json`; in-flight runs, mailbox and reconcile are untouched, and a gated queue is skipped like a
paused one without blocking siblings. It is a work-loop tick, not a boot dependency. The core already runs
without it by default.

**The residue that IS sharp:** `dashboard_stale` has no Go consumer — the system emits a
dispatch-withholding signal that nothing in the product reads.

**Disposition:** gate the coupling out of the core. **The gate is now BUILT — corrected 2026-07-30.**
This line said "`SubsystemDashboardGate` — the name exists, the call site does not yet". The call site
is `internal/daemon/dashboardgate.go`, which returns early unless
`pc.Subsystems.Enabled(projectconfig.SubsystemDashboardGate)`. As with every other subsystem, the
default is on, so switching it off is a config line rather than a code change. Note this sheds the
*import*, not the daemon's knowledge of the file:
`internal/daemon/dashboardgather.go` reads the same `dashboard.json` directly through its own type, so
"remove the blockers" means both the forcing gate and that second read path.

**Keep the package; redesign it later as a plugin — operator, 2026-07-28.** On hearing that the captain
writes the file and the daemon reads it: *"Oh… interesting. Yea worse. Definitely remove the coupling —
that seems like a really shitty way to manage these things — a total hack. We should redesign that as
like a 'plugin' to core. I assume we can defer that til later. Lets make sure we put that on the list to
redesign/revisit later and think through a proper design."* And on what it should become: *"later we
should be able to derive that from core…. or something."*

Three constraints for that later design, all stated or implied above:
1. **Derive from core, do not hand-author.** The present shape has an agent hand-writing a JSON file that
   the daemon then treats as authoritative. Most of what it holds — what is running, on which lane, at
   what rate — the core already knows. Only genuine human intent (expected throughput, notes) cannot be
   derived, and that is a much smaller surface.
2. **Plugin, not a gate.** It consumes core state; core must not consult it to decide whether to work.
3. **Nothing it does may withhold dispatch**, by staleness or otherwise.

This sits behind the dataplane question in `CHARTER.md` §3 — "how everything else communicates with the
core" is the same problem, and a plugin surface is one answer to it. Do not design either in isolation.

---

## Deferred — housekeeping

**N. ~20 milestone-scoped normative claims were silently broadened by the terminology sweep — OPERATOR CALL.**
Commit `72215ba69` removed the scope qualifier from sentences whose scope it was *bounding*, converting
milestone-pinned declarations into unbounded ones across **nine reviewed specs**. Each is internally
coherent and most have an amendment escape hatch, so none is provably false — but the temporal scope
changed without an amendment, which is not a prose repair to make unilaterally.

Strongest sites: `event-model.md` (a `queue_paused.reason` "exhaustive enum" and the "complete
cross-subsystem emission surface", both now unqualified); `control-points.md` ("complete family";
"structured returns not supported"); `handler-contract.md` ("complete class × sub-reason taxonomy";
"no other transport permitted"; "flags MUST NOT be passed"); `architecture.md` ("only out-of-process
actors admitted"; "all cross-subsystem registries daemon-owned and in-process"); `beads-integration.md`
(states harmonik "does NOT write"); `handler-pause.md` (**`schema_version: 1` is the only supported
version — while the next clause presupposes a v2**); `execution-model.md` (`workflow_class` "the only
accepted value"); `workspace-model.md` (lock-file closed map, "additional keys forbidden");
`scenario-harness.md` (discovery "MUST NOT load from any other location").

**O. Two specs' §10.1 Core range contradicts their own Deferred-extensions list — OPERATOR CALL.**
**Pre-existing; predates the sweep.** `handler-contract.md` Core requires "every invariant HC-INV-001
through HC-INV-008" while the structured-agent-input bullet lists HC-INV-008 as an additive *deferred*
extension — the carve-out is scoped to the HC-001..HC-053 range and silent on invariants, and the v0.7.0
revision row suggests the Core bump was deliberate, so it likely resolves in Core's favour.
`reconciliation/spec.md` Core requires "every requirement RC-001 through RC-031" while Deferred says
RC-027 is required "only if operators have opted in" — RC-027 is titled a spec-draft obligation with its
grammar unfinalized, so it likely resolves the other way. **They resolve in opposite directions; each
needs its own call.** Both are `status: reviewed`, so adjudicating changes conformance scope.
Landed 2026-07-28: the false blanket "No requirement is deferred." in both was replaced with a closure
sentence pointing at the Deferred-extensions paragraph *without* adjudicating. Also: the sweep left
reconciliation's deferred paragraph reading "MAY ship as a follow-on within one release" — an unanchored
deadline with no release named.

**M. Crews never load `PRINCIPLES.md`, and crews are who write the tests.** Found 2026-07-28 by
`agent-config-reviewer` while wiring the document in. The new `PRINCIPLES → AGENT_INDEX → STATUS →
HANDOFF` reading order lives in `AGENTS.md` §Start here, but the per-role load map says each role
skill's boot runbook is authoritative, and `crew-launch/SKILL.md` enumerates a deliberately minimal
load that does not include it. **Still true 2026-07-30** — `grep -n 'PRINCIPLES'` finds nothing in
either `.claude/skills/crew-launch/SKILL.md` or its embedded source under
`cmd/harmonik/assets/skills/`. So §6 — *"beware test theater: a suite that mostly asserts constants is
not coverage"* — never reaches the role that writes tests. **Not a contradiction, a coverage hole.**
Fixing it is a dual-path edit (`cmd/harmonik/assets/skills/crew-launch/` plus the byte-identical
`.claude/skills/` mirror), which is why it was not smuggled into a config-review commit. Captains need
no equivalent change — captains do not write code.

**K. Role validators are not wired into `RegisterFromDocument`. Still true 2026-07-30.**
`internal/core/s02registrar_hka8bg45.go` calls only `ValidateSections` and `ValidateSchemaVersion`;
`ValidateRoles`, `ValidateDeferredRoleShells` and `ValidateRequiredRoleDefaultSkills` still have **no
non-test caller** — the only non-test hits are their own definitions in `policydocument.go` — so
CP-028/CP-030/CP-031 are enforced only under `go test`. Found 2026-07-28 during the role-status rename; wiring them changes registrar
behavior and needs its own test surface, so it was correctly kept out of a rename. Related to item J,
which is the lint for this class of bug.

**L. `MVH` survives as a milestone noun in `docs/`, and only there.** The 2026-07-28 sweep cleared
`specs/`, all Go code, and `docs/foundation/spec-template.md` — the generator. Both of those are
confirmed empty on 2026-07-30 (`grep -rn 'MVH' specs/ --include='*.md' | wc -l` and the same over
`internal cmd --include='*.go'` each return 0). "MVH-baseline", "post-MVH", "at MVH" and "MVH ordering"
remain in `docs/decompose-to-tasks/` (the `bootstrap-subset/` set and the `mnem-maps/*.csv` files), plus
`docs/review-claude-hook-bridge-spec.md`. **The live ones are the regrowth path** — agents read working
documents as current context and imitate the vocabulary, which is how the term survived two previous
removals.

> **The split is unverified.** This item said "18 live documents" against "a further 26 dated records
> or pilot captures", which totals 44. `grep -rl 'MVH' docs/ | wc -l` returns **144** files on
> 2026-07-30. The difference is probably a narrower original scope rather than growth, but the working
> versus historical split was not reproducible from this document, so treat both figures as unproven
> and re-derive the live set before sweeping.

**J. Lint for a bare string literal where a typed constant exists.** Operator, 2026-07-28, prompted by
`internal/core/policydocument.go` comparing `r.Status` against the literal `"mvh-required"` while
`internal/core/role.go` declared `RoleStatusMVHRequired` with that exact value. Renaming the constant
would have left the literal stale **with no compiler error**, and that particular comparison `continue`s
on mismatch, so role validation would have silently stopped enforcing CP-031. The same latent bug sat
next to it on `declared-but-deferred`.

> **The prompting instance is gone. Verified 2026-07-30.** `RoleStatusMVHRequired` no longer exists.
> `internal/core/role.go` declares exactly two statuses, and `policydocument.go` now compares against
> the typed constants `RoleStatusRequired` and `RoleStatusDeclaredButDeferred`, with a `Valid()` check
> as the single enforcement site. The general ask survives — this is a class of bug, not one site — but
> nobody should go looking for the `"mvh-required"` literal.

This is a `PRINCIPLES.md` §7 lever and worth having *if* an
existing linter can express it — check `golangci-lint`'s `goconst`, `usestdlibvars`, and whether a
`forbidigo`/`ruleguard` pattern can catch "string literal equal to the value of a declared constant of
a named type." **Prefer configuring a linter already in `.golangci.yml` over writing a new gate.**

**P. Any unrecognized `harmonik` subcommand starts the daemon. UNVERIFIED in this sweep** — proving it
means running an unknown subcommand, which is the behaviour under complaint, and the top-level fall-
through in `cmd/harmonik/main.go` was not traced. Note the nested verb tables (`queue`, `worker`,
`keeper`) do reject an unknown verb with exit 2, so if the claim holds it is specific to the top level.
`harmonik status`, `harmonik daemon status` — anything not in the dispatch table falls through to the
daemon-start path, which then exits on the `$TMUX` guard. Known issue, recorded not chased: the consequence for this program is that there is
**no read-only status surface outside tmux**, so "is the core running?" cannot be answered without
booting something. Worth an unknown-subcommand error before the partition work needs to inspect a
running core.

**I. Fresh worktrees have no `.tools/`. Confirmed live 2026-07-30, from inside a fresh worktree.**
`ls -d .tools` reports no such file, and the `check-fast` target still calls `scripts/go-format.sh check`
and `$(TOOLS_DIR)/golangci-lint` with no `tools` prerequisite. Every agent dispatched into a new
worktree hits `make check-fast` failing immediately at `fmt-check` with `gofumpt: No such file or
directory`. Either worktree setup runs `make tools`, or `check-fast` bootstraps `.tools/` when absent.

---

## 6. Carried forward out of the step-3 dead-code deletion

Three things the deletion surfaced and deliberately did not act on. Recorded here rather than in the
bead ledger, which is machine-local and does not travel.

- **CP-021 now has no live implementation anywhere.** Deleting
  `internal/core/cp021_guard_invocation_s01.go` was right — it bolted onto `core.Registry.LookupByAttachPoint`
  and `core.DispatchEdge`, both themselves callerless, so reviving it would have been re-architecture
  rather than wiring. But the guard-invocation clause is now unimplemented, and the shipped cascade
  reaching `core.SelectNextEdge` directly is not the same contract. Decide during the run-machine
  decomposition whether CP-021 is a real requirement or spec rot.
- **`CycleIDFromNonceMarker` (`internal/keeper/cycle.go`) is callerless** after
  `nonce_provenance.go` went. Still callerless on 2026-07-30 — a repo-wide grep finds only its own
  declaration and doc comment, not even a test. It is exported, so nothing breaks and no linter fires
  — which is exactly why it will sit there. Candidate for the next sweep, along with the rest of the keeper's T6 render leg.
- **`coverage.baseline` is now approximate for four packages** — `internal/core`, `internal/daemon`,
  `internal/handlercontract`, `internal/keeper` each lost covered production files. Do **not** hand-edit
  the numbers; re-derive them. Nothing is gated on this today: `scripts/coverage-gate.sh` runs only from
  `make check`, which exits non-zero by design and never runs in CI. §2.3 already flags that
  doc-versus-script contradiction — fix both in one change or not at all.

---

## 7. Carried forward out of the step-4 `internal/scenario` test deletion

Same framing as §6: recorded here rather than only in the bead ledger, which is machine-local and does
not travel.

- **The step-4 premise was wrong, and the corrected scope is one file pair, not ~18,000 LOC.**
  `_plan.md` §3 row 4 says delete the `internal/scenario` harness engine. It cannot be deleted:
  4,660 of its 16,269 lines are production code behind the shipped `harmonik harness` subcommand
  (`cmd/harmonik/harness.go`, dispatched from `main.go`), which calls 45 exported symbols from the
  package. (Re-counted 2026-07-30; this said 4,952 of 17,126 across 31 test files, and the tree now
  holds 30.) `ParseScenarioFile` → `ScenarioFile.Valid()` reaches agentoverride, fixturesetup, gitseedop,
  fileseed, eventexpectation, workspacepredicate, outcomeexpectation and cadencetag; `EvaluateAssertions`
  evaluates three of those at runtime. Of the test files, 22 cover that production surface and 9 drive
  `internal/queue` / `queuewiring` / `lifecycle` directly. Exactly one — `crashrecovery_test.go` — had a
  subject the CLI could not reach, and it went with `crashrecovery.go`. **Update the row rather than
  re-attempting it.**
- **The scenario harness has no crash-recovery tier, and now no Go marker for one.** Tracked as bead
  `hk-p5sqp`, repeated here because beads do not travel. `specs/execution-model.md` EM-016..EM-022
  (kill the daemon between `git write-tree`/`commit-tree` and `update-ref`, and between `update-ref` and
  event emission), EM-024a branch-tip monotonicity, EM-025a ENOSPC transient classification, and
  `specs/process-lifecycle.md` PL-024..PL-026 chaos tests are all still normative and still unenforced.
  Three prerequisites, unchanged since the 2026-07-18 captain COORD entry deferred them: a daemon-side
  crash-injection hook at the checkpoint boundary; a `crash_recovery` field on the `ScenarioFile` schema
  (needs a `specs/scenario-harness.md` §6.1 amendment first — the record is not declared there); a
  kill/restart driver. Deleted declarations recoverable at
  `git show afdfccbd0:internal/scenario/crashrecovery.go`.
- **Two more zero-caller residues the step-3 sweep missed**, both in `internal/scenario`:
  `sh_inv_004_rerun_diff.go` (264 lines; all 7 exported symbols callerless, no test file) and
  `NewFixtureRoot` in `fixtureroot.go`. Both are still in the tree on 2026-07-30, and `NewFixtureRoot`
  still has no non-test caller. **But the reason given for the second one no longer resolves:** this
  bullet said "`harness.go` inlines `os.MkdirTemp` instead of calling it", and
  `internal/scenario/harness.go` does not exist. Re-find the inlining site before acting on it.
  `ScenarioProjectRoot` in the same file **is** live via `SynthesizeProjectRoot` ← `BootstrapFixture`,
  so the file stays. Caveat for the sweeper: `NewFixtureRoot` has test coverage, so deleting it takes
  tests with it.
- **`FixtureSetup`'s `git_seed` and `files` are validated but never applied.** `ScenarioFile.Valid()`
  checks every `GitSeedOp` and `FileSeed`, and `BootstrapFixture` takes no `FixtureSetup` argument at
  all — so a scenario declaring fixture seeds parses clean and silently seeds nothing. A polarity
  mismatch of exactly the kind §1 describes, in code rather than in a spec.
- **Relocating the 9 queue tests out of `internal/scenario` is not a file move — leave them.** They are
  self-contained (zero references to any of the 74 helpers declared in the 22 harness test files, in
  either direction), but `.golangci.yml` depguard splits them three ways: the `queue` rule allows only
  `$gostd` + `uuid` + `internal/core` + `internal/queue`, so `named_queues_workers_test.go` (imports
  `queuewiring`) and `queue_daemon_wiring_test.go` (imports `lifecycle`) are denied there,
  `single_active_per_name_test.go` needs `testify` added or removed, and `queue_lifecycle_test.go` calls
  `scenario.BootstrapFixture` — the scaffolding drag. Cost is two depguard amendments, one testify
  rewrite, and one fixture decision, to buy a directory rename with zero behavior change. §6 of this
  document (the subsystem partition) should decide their home; do not pre-empt it.

---

## What this document is not

It is over 1,300 lines — it said 400 when that number was roughly right, and nobody updated it as the
document tripled. The "keep it short" instruction at the top deserves an honest accounting: **length
was never the disease.** The predecessor plan (`plans/2026-07-24-code-health-audit/`) had a task
index, a state model, a disposition lattice, a coordinator protocol, and 92 task cards — and reached
**13 of 92 complete**, with 77 never leaving triage. The bottleneck was never task
decomposition, and adding more of it never helped.

> **⚠ Counted 2026-07-30: it is 1,400 lines, not 400.** The defence above is sound and the count under
> it was wrong by 3.5x on the day it was written. Two things followed from the drift, and both are the
> thing this section says it is guarding against:
>
> - **The document grew two lettered backlogs** — "Deferred — real work" (A–G) and "Deferred —
>   housekeeping" (I–P, out of alphabetical order). A lettered list of items with owners and blockers
>   is a task index whatever it is called. The live ones belong in `OPEN-DEFECTS.md`, which exists for
>   exactly that and says why.
> - **It has two `## 6` headings and a `## 7` that is unrelated to either.** The numbering has become
>   the lattice.
>
> The ordered work has moved to `DECOMPOSITION-MAP.md` §3. **What earns its place here is the findings
> nothing else records** — §2.4, §2.5's grandfather list, §2.7's reviewer divergence, §2.8's
> structure-block, §4 in full, §5's `br` exit-3 arm, the `unknownYAMLKey` alias hole, and Traps 1 and 2.
> Anything in this file that a sibling doc now owns should be cut on the next edit rather than
> re-reconciled.

Everything here is a **finding or a decision**, each with the measurement behind it. There is no task
index, no state machine, no per-task card, no staffing model, and nothing to keep in sync. Every
section names a file to change and a number that says why.

The disease returns if this acquires a YAML index, a `phase:`/`disposition:` lattice, or per-task
cards — not if it gets longer. If a section here cannot name the file it changes, cut that section.
