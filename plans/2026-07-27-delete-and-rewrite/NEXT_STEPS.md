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

---

## Sequence

| # | Step | Blocks | Cost |
|---|---|---|---|
| 1 | Reconcile `origin/integration/phase-reviewloop-20260725` — **44 commits not in the active branch** (verified; it is not 44 ahead of `main`) | everything below | judgment |
| 2 | **Agent instruction changes (§2)** | dispatching any new agent work | mostly deletions |
| 3 | Wire the scenario tier to a merge-blocking gate (§5.1) | trusting any green build | small |
| 4 | Bead the 29 hidden deferrals (§3) before touching era-marker prose | era-marker cleanup | mechanical |
| 5 | Deletion steps 2–4 of the predecessor plan | the workloop rewrite | mechanical |
| 6 | Spec triage of the 7 suspect specs (§1) | using specs as the rewrite oracle | real work |
| 7 | `.dot` fixture relocation + WG-036 amendment (§4) | nothing — do it opportunistically | small |
| 8 | `workloop.go` rewrite | — | the point of all this |
| 9 | MVH / `v0.1` era-marker sweep (§3) | nothing — cosmetic but clarifying | do last |

**§2 is the one to do first, and it is the cheapest.** Steps 5–8 are undone by the next agent that
reads `.claude/implementer-protocol.md` and names a test file after a bead. Everything else on this
list is recoverable; regrowing 255k lines of the wrong tests is not.

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
