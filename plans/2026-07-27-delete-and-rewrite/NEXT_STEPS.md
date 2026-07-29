# Next steps — after the cleanup

**Date:** 2026-07-27
**Sibling:** [`_plan.md`](_plan.md) — deletion and rewrite sequencing. [`CARRY-FORWARD.md`](CARRY-FORWARD.md) — the 83 external facts.
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

**1,180 unique requirement IDs in `specs/`; 291 (25%) appear in zero Go file.** They are not evenly
spread — the distribution is bimodal:

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
only enforcement AR-013, AR-052 and HC-026b ever had.

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
| **ON-018** | Promises N-1 readability of `.harmonik/queue.json`'s `schema_version` (`operator-nfr.md:435`, §4.5). `UnmarshalQueue` returns `ErrSchemaVersion` on anything but 1. (ON-015 is a *different* N-1 promise covering the Beads schema and harmonik's overlay — amending it leaves this contradiction standing.) |

**A live bug fell out of this** — filed as `hk-rr1dy`: the daemon writes `handler-state.json` at
`schema_version: 2` while the CLI hard-rejects anything above 1, so `harmonik handler status` and
`handler resume` fail against state the current daemon wrote, telling the operator to upgrade a
binary that is already current. The daemon's own comment claims the two constants match. HP-016 is
the stale contract behind it.

### What to do

1. **Build the traceability report** — generated, run in CI, **never a test**. It is now the only
   spec-drift detector that could exist.
2. **Apply the polarity check**, not the noun check. The 7-step decision procedure is scriptable
   through step 3; step 4 (polarity) needs a reader.
3. **Delete `cognition-loop.md` and the phantom half of `operator-nfr.md` before the rewrite reads
   them.** Category C is the only one that actively costs you.

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

### 2.2 The instruction that told agents to write tests they never ran

`.claude/implementer-protocol.md` §F19 tells implementers never to run daemon-booting or
scenario-tagged suites, and that if a scenario test is the natural gate for the bead, to skip it and
note the deferral in the commit. Combined with `docs/foundation/project-level/build-practices.md`
("bug fixes require a reproducing scenario test") the net instruction is: **write a scenario-shaped
test, at the wrong layer, and do not run it.** That is the 885-file pattern's other half.

**Action:** make the layer rule explicit — *the test goes at the lowest layer that still fails before
the fix* — and treat "the natural gate is a suite I cannot run in budget" as a signal the change is
too big for one dispatch, not as a licence to defer.

### 2.3 Volume incentives

`docs/methodology/TESTING.md` sets **"80% line coverage per package"** (§1) and **"CI fails the merge
if thresholds regress"** (§Coverage enforcement) — plus "one scenario per workflow-library entry."
These reward volume, and we got volume: **2.25:1 test-to-production** (487,997 test lines against
216,695 production, post-deletion).

⚠ **Do not delete these blindly — a stricter gate is live and unreconciled.** `scripts/coverage-gate.sh`
enforces a 90% floor / 95% core / 0.3pp regression cap against a checked-in 27-entry
`coverage.baseline`, and `scripts/cmd-coverage-gate.sh` ratchets `cmd/**`; both run from `make check`
(Makefile:701, 708). What is false is the *CI* claim — CI runs only `check-short`, which invokes
neither. Reconcile the doc and the scripts in one change, or you strip the prose and leave a harsher
undocumented gate behind.

**Action: DELETE the targets from the doc and reconcile against the scripts.** Replace with one line: coverage is a diagnostic, never a target — the
question is *which real behavior is unprotected*, not *what percent*.

### 2.4 The origin of the markdown-grepping tests

`docs/foundation/spec-template.md` requires citing "the normative test obligations … the test layer
and the requirement IDs each test proves." That instruction, applied literally, produces a test whose
job is to assert a document contains a string. It produced `internal/specaudit`.

**Action: DELETE it.** Requirement-ID→test traceability is a *generated report* (§1), never a test.

### 2.5 The complexity ratchet that cannot fire

`.golangci.yml` sets `funlen: lines: 100`, grandfathered via `--new-from-rev`. Two problems, both verified:

- `funlen` reports at the function's **declaration line**, and `check-fast` uses
  `--new-from-rev=HEAD~1`, so a grandfathered function never re-reports. `beadRunOne` was **born over
  the ceiling at 119 lines** (commit #16 of 370) and is **2,289 today** (peak 2,394) — it has never
  once produced a finding.
- ⚠ **And it carries an explicit `//nolint:funlen,gocognit,cyclop`** at `internal/daemon/workloop.go:3119`,
  justified in-line as "pre-existing … the RT ports stream exists to decompose [it]". **An explicit
  grandfather list alone does not surface it** — the suppression has to be removed too, or the
  ratchet still cannot see the largest function in the repo.
- The exclusion `- path: (_test\.go$|^internal/scenario/|^internal/specaudit/)` disables `funlen`,
  `cyclop` and `gocognit` for **all test code**. 525k lines with no complexity ceiling at all.

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

### 2.7 Reviewer checks that would have caught all of it

`.claude/agents/agent-reviewer.md` §3 asks only whether tests are at "the appropriate tier" and are
"meaningful." Add three mechanical checks, which need no judgment:

- a new `_test.go` filename containing a bead ID → `REQUEST_CHANGES`
- a new or widened `export_*_test.go` entry → `REQUEST_CHANGES`
- **exporting a pointer to a production global** → `BLOCK` (this exists today and lets tests mutate
  production state)

⚠ Note the two agent-reviewer definitions have diverged: `.claude/agents/agent-reviewer.md` (200
lines) is what the Agent tool loads, while `.claude/skills/agent-reviewer/SKILL.md` (542 lines) is in
the live skill registry, is **newer** (Jul 23 vs Jul 12), and is a **superset** — five sections exist
nowhere else. Reconcile them; do not delete the larger one, which would lose content.

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

**Deployment note:** all 10 shipped skills are currently byte-identical between `.claude/skills/` and
`cmd/harmonik/assets/skills/`. Any edit to a shipped skill must land in both in the same commit.

---

## 3. Stale phase markers

"MVH" (Minimum Viable Harmonik) was an early phase concept agents latched onto and never let go:
**443 lines in `specs/`, ~913 lines in `docs/`, ~516 in Go.** (Counts vary by unit — occurrences run
higher; the figures here are lines.) The damage is that "at MVH" makes a live
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

**⚠ The one thing that must not move mechanically:** `"mvh-required"` is an **on-disk policy-document
YAML enum**, normative in `specs/control-points.md` **§6.2 (line 806) and §6.3 (line 854)** — note
`policydocument.go:95` mis-cites it as §4.6, which is probably how this got lost — compared against parsed YAML in
`internal/core/role.go` and `policydocument.go`. A blind `MVH → ""` pass corrupts it and **no compiler
error catches it** — both sides are string literals, so the policy parser would begin rejecting every
valid role document at runtime. Changing it needs a spec amendment and a parser accepting both forms.

Two secondary traps: `mVHTierOrder` (lowercase leading m) is missed by a case-sensitive pattern and
mangled by a case-insensitive one; and stripping "at MVH" from `not wired at MVH` converts a TODO
into a statement of permanent design.

**Three of the "deferrals" are actually live defects, not future work** — `RequestHandler not wired at
MVH` (`internal/daemon/socket.go:210,214`) and `dispatcher_backlog_held: (unavailable at MVH)`
(`cmd/harmonik/handler.go:487`) are latent stubs in shipped paths. File as bugs.

### `v0.1` is the same disease, and must be cleaned in the same pass

**110 lines in `specs/`, 533 in `docs/`, 31 in Go** — "in v0.1 the daemon…", "v0.2 may add…", "v0.1
ships no timeout". It is a *parallel* vocabulary for the same idea, so the specs now carry two
competing era markers. Fixing only MVH fixes half the problem.

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
  28 has one), and both WG-036's "the engine's example-loader looks there" and the README's cited
  `internal/workflow/examples_test.go` **do not exist**.

**The duplicate is worse than drift — they are different workflows.** `specs/examples/review-loop.dot`
and `internal/workflow/dot/testdata/review-loop.dot` have different `start_node` (`start` vs
`implement`), different node sets, different handler refs, and different condition dialects. The
testdata header still claims it is authoritative "until C5 lands" — C5 landed. Meanwhile
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
  moves, and it closes `approved`.
- **`br` exit 3 never stderr-refined** — a permanent "not found" is classified as retryable
  infrastructure failure, routing to Cat-0 and potentially daemon exit 8.

**Both were caught by tests that already existed and that block nothing.** `.github/workflows/scenario.yml`
*does* run `make test-scenario` on every push and PR plus a nightly cron — but it carries
`continue-on-error: true` and its own header says "Non-merge-blocking: no required status check
configured in branch protection." Neither `check-fast` nor `check-short` invokes the tier either. So
the suite runs, goes red, and nobody is stopped.

**Action, in priority order:**
1. **Make the scenario tier merge-blocking.** Cheaper than it sounds: delete one
   `continue-on-error: true` from `.github/workflows/scenario.yml` and add the required status check in
   branch protection. It goes red until the branch-guard bug is fixed — that is the point.
2. **Fix the correct bug, and verify the call chain before believing any claim about which code is
   live.** The `br` defect is instructive in a way that caught this document out. A first pass asserted
   that `BrErrorFromExitCode`'s inverted table was dead code and the real defect lay elsewhere. It is
   not dead: `BrErrorFromExit` (`internal/brcli/brerror.go:171`) calls it for the base value and only
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
block. Fix is to resolve alias nodes before the mapping check.

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
- **Nothing routinely runs the tagged tier**, so its state is unknown between deliberate looks. Measured
  2026-07-29: `go test -tags scenario ./internal/daemon/` yields **eight** failures where the untagged
  run yields one. This is the same invisibility that made "does EM015e fail or never run?" unanswerable
  from the tree for days. §5.1's gate is the fix; this section is why it matters.
  **Root cause established 2026-07-29: no prior assessment ever ran `go test -tags scenario` on this
  package at all.** The P2 recipes only `go vet`ed it, and `.github/workflows/scenario.yml` carries
  `continue-on-error: true`. The tier was not neglected — it was never looked at.

- **Three of these tests print an unconditional `OK` / `PASS`-shaped `t.Logf` while failing** —
  `TestBranchGuard_FailClosed_MergeGuardBackstop`, `TestScenario_RestartRecovery_QM002bDeadlock`, and
  `TestScenario_MultiBead_SerializedNCompletion`. Skim-reading their output tells you the opposite of
  the truth. Deleting those log lines is a five-minute change with outsized payoff, and it belongs with
  this work.

**Dispositions, measured 2026-07-29 — all seven are pre-existing; the tier blocks nothing in Phase 3:**

| Test | Verdict |
|---|---|
| `BranchGuard_FailClosed_MergeGuardBackstop` | **False red.** Guard did not fail open — the ref that moved was the *unprotected* `integration` branch the bead asks for. Premise went stale 2026-07-06 (`hk-lgykq`) when merge-target resolution moved to per-bead `lands_on` and got **stricter**. Open P0 `hk-zobns` is a mis-diagnosis. Real cost is three weeks with no coverage of that backstop. |
| `RemoteSubstrate_Localhost_DOT_E2E` | **False red, environmental.** `t.TempDir()` + a 45-char test name pushes `daemon.sock` to 131 bytes and `ValidateSocketPathLength` correctly refuses. `TMPDIR=/tmp/h` → **PASS in 6.95s**. **The DOT path is healthy** — full remote lifecycle over SSH lands on main and reaches origin. One-line `TMPDIR` pin. |
| `RestartRecovery_QM002bDeadlock` | **False red.** Behaviour changed correctly under `hk-qkahq`; the wedge is still prevented via `paused-by-failure` + QM-027. |
| `EM015e_NoProgress_ReviewerNotLaunched` | **True red.** Dead emitter. Dies with `reviewloop.go`; property survives on DOT. |
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

| # | Step | Blocks | Cost |
|---|---|---|---|
| 1 | ~~Reconcile `origin/integration/phase-reviewloop-20260725`~~ — **RESOLVED 2026-07-28: abandon it.** See §Deferred item C | nothing | done |
| 2 | **Agent instruction changes (§2)** — §2.6 landed 2026-07-28 (`734f283a7`); §2.5 ratchet and §2.8 structure-block remain | dispatching any new agent work | mostly deletions |
| 3 | Wire the scenario tier to a merge-blocking gate (§5.1), then harden the load-sensitive family (§5.2) | trusting any green build | small / then real |
| 4 | Deletion steps 2–4 of the predecessor plan | the rewrite | mechanical |
| 5 | **Subsystem partition — config-driven enable/disable of each part of the system** (§below) | the rewrite's shape and its priority order | design + planning |
| 6 | Decompose the run machine — **`workloop.go` + `reviewloop.go` + `dot_cascade_core.go` as one unit** | — | the point of all this |
| 7 | `.dot` fixture relocation + WG-036 amendment (§4) | nothing — do it opportunistically | small |

**Everything not on that list is deferred.** The failure mode of this project has never been running
out of things to do; it has been doing the interesting adjacent thing instead of the load-bearing one.
Steps 4–5 are undone by the next agent that names a test file after a bead, so §2 stays ahead of them.

---

## Two traps in the sequence above — read before running step 4

Both found 2026-07-28 by re-verifying the superseded `plans/2026-07-24-code-health-audit/` against the
tree as it stands. Both are silent: nothing fails, work just disappears.

**Trap 1 — the zero-caller sweep will delete the queue transaction substrate.**
`internal/queue/transaction.go` (1,005 LOC) is reached only via `queuewiring.QueueStore.Transact`, and
`Transact` has exactly one caller: a test. Three of its five exported functions have no caller at all.
It is therefore indistinguishable from dead code to the mechanical selector — but it is **unfinished,
not dead**, and this plan lists it under KEEP. **It needs an explicit carve-out in the step-4 selector,
alongside `internal/workflow/scenario/` and the `//go:build scenario` daemon files.**

**Trap 2 — a `ready` kerf work will overwrite `specs/`, including the correction at this branch's tip.**
`.kerf/works/reviewloop-decoupling/` holds 11 unlanded spec drafts, among them a `run-state-machine.md`
numbered **0.2.1 — the same version as the input-ack correction landed on 2026-07-27**. `kerf finalize`
copies drafts into `specs/` wholesale, so running it overwrites that correction plus ten other specs
with pre-correction text. This plan names `specs/` the rewrite oracle. **Do not finalize that work.
Resolve or abandon it before any spec triage begins.**

---

## 6. Subsystem partition — turn parts of the system on and off by configuration

**Operator direction, 2026-07-28.** Before rebuilding or fixing anything, build a simple partition that
decides — at startup, from configuration — which parts of the system are running: queue, comms, keeper,
crew, and the rest. Each subsystem becomes something you can opt into or out of.

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
   path. `internal/queue` is 7,627 production lines with the durable transaction substrate still
   unfinished (see Trap 1) — that substrate is core work, not a deferred nicety.
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
   instead of being a switchable stage, a large part of why `beadRunOne` is 2,289 lines.
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
delete the positive loop, goal-keeper, and `internal/cognition/`.

**Operator confirmation, 2026-07-28** — the measurement matched the operator's independent recollection:
*"I'm almost positive that should be pulled, and we probably need to consider removing. I think it was
added early on and basically always has been a problem."* Sequence is unchanged: gate it out of the core
(`SubsystemMovementGovernor` — the name exists, the call site does not yet), then delete it during the
decomposition. Note the governor's observe block still runs on **every** production daemon today — a
`br ready` shell-out plus an `events.jsonl` scan every two minutes, emitting `governor_signal`, which has
no **Go** consumer (`scripts/ops-monitor-check.sh` does list it in `ACTIONABLE_EVENT_TYPES`, so the shell
side is not quite nothing).

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
governor fails.

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
built. **Seven** requirements would regress working code if a rewrite obeyed them — `AR-017` is the
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
**359 local and all 129 remote branches remain**, untouched — the worktrees are cleaned up, the
branches essentially are not, so branch cleanup proper is still open.

**Five worktrees were deliberately kept, and this is the part that has to survive:**

| Kept | Why |
|---|---|
| `harmonik-wt/cq-01` | 8 staged files incl. an unfinished `internal/queue/transaction_store.go` — the queue-transaction work Trap 1 protects |
| `harmonik-wt/lift-l8-reviewloop` | 2 staged renames moving `reviewloop.go` into `internal/runloop` — relevant to the run-machine decomposition (step 6) |
| `harmonik-wt/arch-01-contract` | untracked `.kerf/` artifacts |
| `harmonik-wt/cq-mig-01` | untracked evidence under `plans/` |
| `/private/tmp/harmonik-main-integration-20260725` | staged for the item-C hand-harvest |

Also kept, and **not** a worktree despite living under `harmonik-wt/`:
`harmonik-wt/kilo-preserved` is a plain directory holding two deliberately-named evidence patches
(`hk-o7x4w-BLOCKED-do-not-merge.patch`, `hk-pvrfx-UNCOMMITTED-under-review.patch`). It has no `.git`
and `git worktree list` cannot see it, so a worktree-driven sweep will neither remove it nor warn you
it exists.

One thing needs a human look: **`/tmp/hk-rqhz3.2ebLUi/clone` (271 MB)**, an unregistered self-contained
clone left by a `harmonik init` smoke test on 2026-07-24. Its three HEAD commits are all present in the
main repo and its 8 dirty files are init's own scaffolding output, so it is almost certainly disposable
— but it is dirty, so it was left.

**E. Delete `plans/2026-07-24-code-health-audit/` — 129 files, 17,813 lines, all tracked.**
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
over."* `internal/scenario` is **4,952 lines of production code** behind the `harmonik harness`
subcommand, and `cmd/harmonik/harness.go` calls **45** exported symbols from it. Its reach is far wider
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
  paused-by-drain back to active". It resumes the *other* pause state.
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
Carry this into the partition work (§6) — `crew-idle-reap` is one of the eleven subsystems currently
behind the single socket-listener condition, and it should default to **off** when it becomes switchable.

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

**Disposition:** gate the coupling out of the core (`SubsystemDashboardGate` — the name exists, the call
site does not yet). Note this sheds the *import*, not the daemon's knowledge of the file:
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
load that does not include it. So §6 — *"beware test theater: a suite that mostly asserts constants is
not coverage"* — never reaches the role that writes tests. **Not a contradiction, a coverage hole.**
Fixing it is a dual-path edit (`cmd/harmonik/assets/skills/crew-launch/` plus the byte-identical
`.claude/skills/` mirror), which is why it was not smuggled into a config-review commit. Captains need
no equivalent change — captains do not write code.

**K. Role validators are not wired into `RegisterFromDocument`.** `internal/core/s02registrar_hka8bg45.go`
calls only `ValidateSections` and `ValidateSchemaVersion`; `ValidateRoles`, `ValidateDeferredRoleShells`
and `ValidateRequiredRoleDefaultSkills` have **no non-test caller**, so CP-028/CP-030/CP-031 are enforced
only under `go test`. Found 2026-07-28 during the role-status rename; wiring them changes registrar
behavior and needs its own test surface, so it was correctly kept out of a rename. Related to item J,
which is the lint for this class of bug.

**L. `MVH` survives as a milestone noun in 18 live documents.** The 2026-07-28 sweep cleared `specs/`,
all Go code, and `docs/foundation/spec-template.md` — the generator — but "MVH-baseline", "post-MVH",
"at MVH" and "MVH ordering" remain in `docs/decompose-to-tasks/` (the `bootstrap-subset/` set and the
`mnem-maps/*.csv` files), plus `docs/review-claude-hook-bridge-spec.md`. A further 26 files are dated
records or pilot captures and are deliberately left as history. **The 18 live ones are the regrowth
path** — agents read working documents as current context and imitate the vocabulary, which is how the
term survived two previous removals.

**J. Lint for a bare string literal where a typed constant exists.** Operator, 2026-07-28, prompted by
`internal/core/policydocument.go` comparing `r.Status` against the literal `"mvh-required"` while
`internal/core/role.go` declares `RoleStatusMVHRequired` with that exact value. Renaming the constant
would have left the literal stale **with no compiler error**, and that particular comparison `continue`s
on mismatch, so role validation would have silently stopped enforcing CP-031. The same latent bug sat
next to it on `declared-but-deferred`. This is a `PRINCIPLES.md` §7 lever and worth having *if* an
existing linter can express it — check `golangci-lint`'s `goconst`, `usestdlibvars`, and whether a
`forbidigo`/`ruleguard` pattern can catch "string literal equal to the value of a declared constant of
a named type." **Prefer configuring a linter already in `.golangci.yml` over writing a new gate.**

**P. Any unrecognized `harmonik` subcommand starts the daemon.** `harmonik status`, `harmonik daemon
status` — anything not in the dispatch table falls through to the daemon-start path, which then exits on
the `$TMUX` guard. Known issue, recorded not chased: the consequence for this program is that there is
**no read-only status surface outside tmux**, so "is the core running?" cannot be answered without
booting something. Worth an unknown-subcommand error before the partition work needs to inspect a
running core.

**I. Fresh worktrees have no `.tools/`.** Every agent dispatched into a new worktree hits
`make check-fast` failing immediately at `fmt-check` with `gofumpt: No such file or directory`. Either
worktree setup runs `make tools`, or `check-fast` bootstraps `.tools/` when absent.

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
  `nonce_provenance.go` went. It is exported, so nothing breaks and no linter fires — which is exactly
  why it will sit there. Candidate for the next sweep, along with the rest of the keeper's T6 render leg.
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
  4,952 of its 17,126 lines are production code behind the shipped `harmonik harness` subcommand
  (`cmd/harmonik/harness.go`, dispatched from `main.go`), which calls 45 exported symbols from the
  package. `ParseScenarioFile` → `ScenarioFile.Valid()` reaches agentoverride, fixturesetup, gitseedop,
  fileseed, eventexpectation, workspacepredicate, outcomeexpectation and cadencetag; `EvaluateAssertions`
  evaluates three of those at runtime. Of the 31 test files, 22 cover that production surface and 9 drive
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
  `NewFixtureRoot` in `fixtureroot.go` — `harness.go` inlines `os.MkdirTemp` instead of calling it.
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

It is 400 lines, so the "keep it short" instruction at the top deserves an honest accounting: **length
was never the disease.** The predecessor plan (`plans/2026-07-24-code-health-audit/`) had a task
index, a state model, a disposition lattice, a coordinator protocol, and 92 task cards — and reached
**13 of 92 complete**, with 77 never leaving triage. The bottleneck was never task
decomposition, and adding more of it never helped.

Everything here is a **finding or a decision**, each with the measurement behind it. There is no task
index, no state machine, no per-task card, no staffing model, and nothing to keep in sync. Every
section names a file to change and a number that says why.

The disease returns if this acquires a YAML index, a `phase:`/`disposition:` lattice, or per-task
cards — not if it gets longer. If a section here cannot name the file it changes, cut that section.
