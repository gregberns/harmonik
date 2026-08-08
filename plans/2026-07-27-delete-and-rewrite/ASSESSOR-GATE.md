# The two lists — what blocks an assessor sign-off, and what does not

**Written 2026-08-06 on the operator's instruction. Read `CHARTER.md` first for what the program is.**

## The objective this file serves, in the operator's own framing

Right now we want **one build that works, handed to an agent acting as the assessor.** The assessor
audits a candidate commit in a scratch daemon. It is not production. We are not running real beads
through this until the work is merged to main, and merging to main happens *after* the assessor signs
off — not before.

So the sorting question for every open defect is exactly one thing:

> **Would this stop the assessor from reaching an honest verdict on a candidate commit?**

"Honest" is doing the work in that sentence. A defect that makes the assessor's run *fail* is a
blocker. So is a defect that makes it *pass when it should not* — a false green is worse than a red,
because the sign-off is the whole point of the exercise. A defect that is real, serious, and simply
not exercised by a local scratch-daemon audit is not a blocker.

**List A** must be fixed before the candidate goes to the assessor.
**List B** can be fixed while the assessor works, or by an agent before major daemon work resumes.

---

## How much to trust this file

**This is a reasoned first cut, not a completed triage.** There are 3 open P0 and 80 open P1 issues.
Every item below was sorted from its own record and, where the claim was cheap to check, against the
code. Items were **not** individually reproduced. Two consequences follow, and neither is hedging:

- An item in List A might turn out not to fire in a local scratch run. Check before you spend a day
  on it.
- **List A is a floor, not a ceiling.** Roughly forty P1 issues were not examined closely enough to
  place. Any of them may belong in List A. Treat an empty result from this file as "not yet looked
  at", never as "cleared".

---

## 2026-08-07 — the unplaced items were sorted, and five were already fixed

**The gap this file declared is now closed for the open set.** Every open P0 and P1 that this file
had never named was read and placed. That was 27 issues: 5 P0 and 22 P1. They are now in List A or
List B below, and each carries the reason it went there.

**Five of the 27 were already fixed in the tree and still open in the ledger.** Re-derived against
`e4c91dfec`, not taken from the bead text. Cite the commit, never the bead id — ids do not survive a
fresh clone and these did not survive a single day.

| Issue | Fixed by | What was checked |
| --- | --- | --- |
| `hk-assessor-contract-cannot-execute-61a4g` (P0) | `b5c628fed` | `operating.md` now names `make core` and `make full`, and says in place that `make check-short` does not exist and must not come back. |
| `hk-scratch-daemon-audits-wrong-tree-zljvm` (P0) | `14cd25722` | `scratch-daemon.sh` requires `--rev`, forces the tree to it, reads HEAD back and compares. |
| `hk-core-red-run-terminal-writer-urbeg` (P0) | `c7ea4a3e2` | The FIFO write end takes `O_CLOEXEC`, and a checker test now fails if any raw descriptor call omits it. |
| `hk-sunpath-guard-unresolved-y2gwy` (P1) | `c7ea4a3e2` | The fixture resolves symlinks and then measures, through `lifecycle.ValidateSocketPathLength`. |
| `hk-ziouf` (P1) | `54c3c38ad`, `684cee2b5` | The root cause was a fixture that built two repositories whose commits collided on SHA. The gate assertion was naming the wrong subject. |

`hk-1a7yb`, already in List B, is also fixed: `a4981d686` deletes
`shared.MainHistoryHasRefsTrailer` and leaves a tombstone. **Its spec half is not fixed** — that is
`hk-0o5ya`, placed in A5 below.

**Four of the five are closed. `hk-core-red-run-terminal-writer-urbeg` is deliberately still open.**
Its repair is in the tree and was read there, but that bead's own claim is that a TEST is red, and no
confirming run has been taken. The fix being present and the test passing are two different facts,
and this file has been wrong before by treating the first as the second.

**This says nothing about the roughly forty CLOSED-but-unverified issues** the earlier note worried
about, and nothing about anything filed after this date. The floor rule stands.

---

## List A — fix before the candidate goes to the assessor

### A1. The assessor cannot run at all

- **`hk-g0xgo`** — a stale sentinel trip file wedges all dispatch on an observe-mode daemon.

> **`hk-joacj` was listed here and is withdrawn.** It said a fresh project's first bead dies at the
> commit gate because the shipped graph hardcodes `scripts/scenario-gate.sh`, which `harmonik init`
> never writes. That script is deleted and the gate now runs `make full`. The defect survives in a
> new form — `init` writes no Makefile either, so the shipped default still names a command only
> this repo has — but **the assessor is not exposed to it**: `scripts/scratch-daemon.sh` builds its
> scratch project as a clone of this checkout, so the Makefile is there. Moved to List B, and the
> bead is corrected.
>
> Worth reading as a warning about this file: that item was placed from the bead's own text without
> re-deriving it, and the bead was two weeks stale. **Re-check before you spend a day on anything
> here.**

> **Sandboxing is OFF for the assessor — operator, 2026-08-06.** `hk-quoka` (a sandboxed run gets
> zero network egress) and `hk-bzydx` (a sandboxed run cannot reach its harness state directory) are
> therefore **moved to List B**. They are still real and still serious; they are simply not on this
> path. Do not promote them back without turning sandboxing on first.

### A2. The assessor reaches a verdict, but the verdict is not honest

This group is the reason the list exists. Each one makes something pass that should not.

> **How to treat these — operator rule, 2026-08-06. Disable, do not delete, and do not fix now.**
> A test that cannot fail is not helping, so it should not cost a run. It also must not vanish, or
> the next person writes it again. `t.Skip` it with a reason naming the issue, and `tools/testreport`
> names it in the **NOT RUN** section of every run, green ones included. Getting these back to a
> real green is later work, done deliberately. The full rule is in `PRINCIPLES.md` §7. First two
> applied 2026-08-06 in `internal/keeper`.

- **`hk-hs4b0`** — `make full` runs a crash tier containing zero tests and always exits 0. `make full`
  is the merge decision.
- **`hk-97gcz`** — a whole tier of daemon failures is invisible: 8 failures under the scenario build
  tag, not the 1 believed.
- **`hk-ynohn`** — half the scenario tier silently skips in CI, and every skip reads as a pass.
- **`hk-q2r9q`** — 46 of 50 daemon work-loop fixtures omit the disk stub; 45 go loudly red and **8 go
  silently vacuous**.
- **`hk-oxbv9`** — the scenario scorer cannot tell an incomplete event log from a complete one. This
  is the assessor's own instrument.
- **`hk-mzwex`** — a typo in the diff ref makes **both halves of the review gate pass**.
- **`hk-2h2fa`** — the review gate approved a 1,412-line deletion that orphaned two subsystems and
  called it a clean diff.
> **`hk-7bfqe` is FIXED and was withdrawn from this list on 2026-08-07.** It said
> `specs/assessor-handoff-schema.md` contradicted itself — §9 saying version 3, §4 and §8 requiring
> 2, and an appended amendment adding a `readiness` gate kind valid only at 3. `fa6fe25f9` retired
> that amendment and put the frontmatter and §9 back to 2. Verified against the tree: the field
> table, §8, §9 and both examples all say 2, and no `gate: readiness` text survives. The bead is
> closed.
>
> **It was found stale by a reviewer, not by this file, and it had sat on List A for two days.**
> That is the third time this week an item here has been read from a bead rather than re-derived.
> Re-check before you spend a day on anything in this list — the instruction at the top of the file
> is not decoration.

**Placed 2026-08-07.** Each of these makes a gate report something other than what it tested.

- **`hk-core-gate-nondeterministic-m9vlf`** (P0) — `make core` is the HARD gate, and it names a
  different set of failures on every run. Neither its pass nor its red is evidence about the code.
  The triage procedure at the end of this file is the current answer, not a fix.
- **`hk-core-target-not-scoped-qgk9a`** — `make core` does not run what its name promises: a
  whole-tree static stage runs first, and one package is silently excluded. The hard gate does not
  test the set the sign-off is defined against.
- **`hk-gate-static-new-from-rev-eejbl`** — the static gate lints against `HEAD~1`, so what it
  checks depends on where the assessor stands in history rather than on the candidate commit. A
  clean lint result means much less than it reads.
- **`hk-scenario-tier-nondeterministic-xt1wa`** — the same defect as the core gate's, in the tier
  the core gate does not cover. Both sampled runs also sat under the disk-low dispatch pause, so
  the disjoint failure sets are unexplained until somebody re-runs above 10 GiB free.
- **`hk-fabricated-review-trailer-zbkqf`** (P0) — a commit carried a review verdict its reviewer
  never issued. The cold-diff-review leg reads trailers as evidence, so a fabricated one buys an
  approval that never happened. **This is also blocker 1 for the operator: bind a verdict to the
  reviewer that issued it, or stop treating trailers as evidence.**
- **`hk-oge5e`** — production reaches the edge cascade through a caller that skips the only function
  taking a guard or a gate. No gate can deny or escalate a transition, so a run that should be
  stopped proceeds and reports success.
- **`hk-codex-harness-real-home-rquc5`** — FIXED the same day it was found, recorded here because it
  is the reason `make full` was red and nobody could say so. Four tests built the codex harness
  against `$HOME/.codex` and then ran the fail-closed billing guard, so `make full` asked a question
  about the machine and, worse, rewrote the operator's real `~/.codex/config.toml` on the way. It
  died before `lint-allow` or `test-scenario` ever ran, which means every earlier report of "`make
  full` is red for the scenario tier" was naming a tier the target never reached.
- **`hk-codex-billing-guard-race-xychw`** — **the production half of the item above, and it is the
  more serious one.** The billing guard rewrites `config.toml` with a truncating write, re-reads it
  to verify, and takes no lock of any kind. Measured by an independent review: **8,047 of 12,000
  concurrent reads saw a truncated file with no key.** The daemon registers ONE codex harness aimed
  at the operator's real `~/.codex` and dispatches beads concurrently, so a launch can be refused by
  the guard's own write — and a live codex child reading inside that window sees no billing pin,
  which is the API-pool fallback the guard exists to prevent. The guard can defeat itself. The same
  package already documented this exact hazard for the stale-WAL guard and gave that one a lock
  check; the billing guard got nothing. **On List A because an assessor audit that drives concurrent
  dispatch can be refused a launch for a reason that is not about the candidate commit.**

### A3. The run does not survive its own lifecycle

- **`hk-hsp9e`** (P0) — both halves of hang handling are dead: nothing kills a hung agent, and nothing
  shows one as hung.
- **`hk-8uznx`** (P0) — a frozen agent is never killed; the freeze-kill events have no producer.
- **`hk-97jco`** — a graceful daemon shutdown does not let a run survive; only a SIGKILL does. The
  assessor will stop and start its scratch daemon.
- **`hk-b4xf2`** — the lifecycle terminal transition runs only in single mode, so **the default path
  is inert.** Graph is the only path, which makes this the live one.
- **`hk-rr1dy`** — handler-state schema split: the daemon writes version 2, the CLI accepts only
  version 1. The operator cannot read the daemon's own state.

**Placed 2026-08-07.**

- **`hk-e7y44`** — `queue submit` publishes the queue, wakes the work loop, releases the lock, and
  only then walks the same object to build its payload. The queue is the one thing this tool has to
  do, and the live-verify leg drives it hard.
- **`hk-agent-worktree-stale-base-kh2rv`** — two of three agents dispatched on 2026-08-07 got a
  worktree cut from a stale base, one of them 847 commits behind, in a tree where the package it was
  sent to fix did not exist. The live-verify leg drives real dispatch, so this grades the wrong code.
- **`hk-1xp9h`** — the socket server registers 28 routes and checks readiness on none of them. The
  assessor stops and starts its scratch daemon, which is exactly when a pre-ready request lands.

### A4. The test bed corrupts itself or the machine

These matter more than usual because lanes run beside the assessor on one box.

- **`hk-ttri2`** — a daemon test run on a low-disk box runs `go clean -cache` and can corrupt every
  other build on the machine.
- **`hk-ts5wp`** — daemon tests leak production daemons that outlive the run, keep firing the cache
  reaper, and wipe the shared build cache during later runs. Contaminates every flake measurement.
- **`hk-c6dt2`** — the orphan sweep kills **other projects'** `br` processes: it matches on the
  process name with no project scoping.
- **`hk-codex-home-zero-value-caah1`** (added 2026-08-07) — `codex.NewHarness` accepts an empty
  `codexHome` and resolves the ZERO VALUE to the operator's real `$HOME/.codex`. One of the things
  that value reaches is a sweep that **`os.Remove`s** `state_*.sqlite-wal` files. Before
  `hk-codex-harness-real-home-rquc5` that pointed a file-deleting glob at the operator's real
  `~/.codex`, and it no-opped only by accident of the test process's working directory. The config
  rewrite was the visible half of that bead; this is the half that could have destroyed data. The
  repair is at the composition root: resolve the home in `newHarnessRegistry` and have the
  constructor refuse an empty value.
- **`hk-59flr`, `hk-hqttl`, `hk-fr7ht`** — the daemon suite goes red under load with a different test
  each time, and each passes alone.

  > **Do not open another investigation into this — operator, 2026-08-06.** It has been looked at
  > four or five times and re-raising it is not progress. **The ruling narrows the question instead:
  > the only thing that has to work is the core queue.** Tests outside that do not have to run, and
  > can be ignored or disabled. Scope the suite to the core queue and judge on that. A flake in a
  > package the queue does not depend on stops being a blocker by definition rather than by
  > investigation.

**Placed 2026-08-07.** Both of these corrupt the measurement rather than the code.

- **`hk-7ssd1`** — a hand-rolled load generator typed inline into an agent's shell leaked 12 spin
  loops for 22 hours at load 27. Second occurrence. **This matters more here than its priority
  suggests: this file's own triage procedure says load decides the result, so a leaked spinner
  makes every red and every green on the box meaningless.** There is no script to patch; the
  pattern is typed inline, so the fix is a rule and `scripts/leakcheck.sh`.
- **`hk-unlcp`** — the git stash stack belongs to the repository, not the worktree, so one agent's
  `git stash pop` takes another's work. Observed 2026-08-06 with real work briefly lost. Lanes run
  beside the assessor on one box.

### A5. Cheap safety, do them while you are in there

- **`hk-ifj6p`** — an unknown first token falls through and **boots a daemon against the current
  directory** instead of being refused.
- **`hk-brl9q`** — a missing branching config silently empties the protected-branch list *and*
  disables the guard that would report it. Pairs with `hk-uwhrb`.
- **`hk-6c85b`** — six daemon functions survive deletion, including the gate that stops an agent
  launching unsandboxed.
- **`hk-7ue65`, `hk-zusgg`** — two freeze gates pin files that the extraction removed.

**Placed 2026-08-07.**

- **`hk-lint-allow-red-on-base-hdln0`** — 14 file-and-linter pairs are off the allow list on the
  integration base, and `lint-allow` takes the whole of `make full` with it. The same run reports
  five allow-list entries that are now clean, so the list has drifted in both directions. Cheap,
  and it is the difference between a merge decision that can pass and one that structurally cannot.
  **Not yet observed directly**: the runs on 2026-08-07 died in the test step before reaching it.
- **`hk-0o5ya`** — the daemon stopped deciding completion from a commit-message mention
  (`a4981d686`), and two specs still require it. Confirmed still present on 2026-08-07. The specs
  are normative here, so the false green can be re-implemented from them and pass review.
  **Two clauses, and they are not equally bad — a reviewer flagged the distinction and it is worth
  keeping.** `specs/execution-model.md` EM-063 Phase 2 and `specs/cognition-loop.md` §4.8 tier 2
  both use the bare grep ALONE as an already-landed test, and those are the defect.
  `specs/cognition-loop.md`'s "two-phase done" definition is a THIRD clause and it is weaker: it
  requires the trailer AND a `run_completed{success}` event, so it is not the same false green. Fix
  the two skip clauses; do not sweep the definition in with them.

---

## List B — fix while the assessor works, or before major daemon work resumes

Real, mostly serious, and not on the path between a local scratch run and an honest verdict.

**Sandbox — moved here 2026-08-06 because sandboxing is off for the assessor:**
`hk-quoka` (a sandboxed run gets zero egress: the allowed-domain list is operator-supplied and
production sets none, so the harness cannot reach its own model endpoint) · `hk-bzydx` (a sandboxed
run cannot reach `~/.claude` or `$CODEX_HOME`, so it fails at init) · `hk-mp37h`, `hk-scaj0` (the
per-bead sandbox switch and the uniform-sandbox epic). **These become blocking the moment sandboxing
is turned on.**

**Not exercised by a local, single-concurrency, local-only audit:**
`hk-uaka2` and `hk-t8myp` (the wrapped worktree probe and the remote shutdown drain — remote path
only, though `hk-uaka2` remains the named first blocker for distributed execution and should not
drift) · `hk-sbd4l` (a gate passing on a flapping ssh link) · `hk-6n0z7`, `hk-lprn7` (remote graph
runs) · `hk-71joj`, `hk-j63y6`, `hk-zjwke` (concurrency; the assessor runs at concurrency one) ·
`hk-o7x4w` (the orphan-handler sweep is a no-op on macOS).

**Correctness that shows up after the audit window:**
`hk-1a7yb` (a bead closes because some commit named it, and the check asks a branch no configuration
can change) · `hk-uwhrb`, `hk-q6fwh` (branch defaults falling back to the literal `main`) ·
`hk-zeox3` (every event subscription is live-only, so a restarting captain misses what completed) ·
`hk-1dwk2` (four of six subscribe clients read a daemon refusal as success) · the queue durability
set `hk-7t615`, `hk-c8mvo`, `hk-xhmf1`, `hk-jvrsv`, `hk-mk4cl`, `hk-e91d8` — **bravo's lane; some may
promote to List A if the assessor's audit drives the queue hard.**

**Test honesty, no runtime effect:**
`hk-9kkpp`, `hk-61urw` (assertions on events the package cannot emit) · `hk-1kv4l` (the Guarding
state is unreached, already recorded in the spec) · `hk-qjxvq` (per-function zero-coverage
reporting).

**Housekeeping and process:**
`hk-6lt60` (P0 by label, but it is a kerf-process defect, not a runtime one) · `hk-f1wb0`,
`hk-99szy`, `hk-gxy67`, `hk-7yd1u` (disk and cache) · `hk-7zzk7` (a bare `br` in a worktree forks the
ledger — real, and the working rule is already written down) · `hk-4pulw` (comms sender is
unauthenticated) · `hk-rlvhi`, `hk-xbrc2`, `hk-j7yo0` (stale binary and wrong-verb messages) ·
`hk-qbsha` (the review-loop vocabulary; the code pass landed, specs remain).

**Placed 2026-08-07 — real, and not between a local scratch run and an honest verdict:**

- `hk-ykbo3` — the run record and the launch read different facts, but the two only disagree when
  the remote-bead context is set. The assessor runs local.
- `hk-graph-worktreerootpath-projectroot-zz0it` — wrong on a cross-repo run only, and harmless
  today because a loose substring match fires whatever it is given. Local audit does not reach it.
- `hk-yvlak` — the keeper scrub that protects a crew handoff has no test, proven by reintroducing
  the data loss. Keeper is outside the core set by `CHARTER.md` §3.
- `hk-wn8wp` — the reachability baseline ratified guards, gates, locks and reapers production never
  calls. It is the parent of `hk-oge5e` and `hk-1xp9h`, both of which went to List A on their own
  runtime effect. What is left here is the baseline's own honesty.
- `hk-5cn51` — the shipped defaults are written for this repo, so a fresh project cannot boot a
  captain or crew. Same reasoning that moved `hk-joacj`: `scripts/scratch-daemon.sh` builds its
  scratch project as a clone of this checkout, so the assessor is not exposed. **It becomes List A
  the moment the audit stops cloning this repo.**
- `hk-tunnel-port-early-failure-path-9adih` — two tunnel tests may measure an early-failure path
  rather than the ending they are named for. **Unverified by its own author.** Test honesty, no
  runtime effect, but do not read the bead as established.
- `hk-85pqo` — worktree trust writes into the operator's real `~/.claude.json` and nothing prunes
  it: 11 of 5,358 entries point at a path that still exists. Housekeeping. **Pruning the operator's
  real config needs their yes — this is blocker 2 for the operator.**
- `hk-qdr-lost-authorized-changes-e14eo` — four authorized spec changes were never written and
  never landed. Spec debt.
- `hk-specs-crossrepo-stale-i1bcg` — two specs still say cross-repo dispatch is refused. It has
  been supported for some time. Spec staleness, and the specs are normative, so it will mislead a
  reader before it breaks a run.

**Close rather than fix:** `hk-main-required-check-red-i14hq`. The operator ruled on 2026-08-06 that
`main` needs nothing until the merge at the end. Nothing should judge this branch's work against it.

---

## What has to happen before this file can be trusted as a plan

1. ~~**Define the core-queue test scope.**~~ **DONE 2026-08-06: `make core`.** It runs `CORE_PKGS` in
   the `Makefile`, which is `CHARTER.md` §3's decided pipeline — config, event bus, queue, bead-ledger
   adapter, worktrees, harness registry and one substrate, work loop, merge — resolved to packages in
   the charter's own order. Nothing was added to the set; §3 says anything absent is deferred by
   default rather than by argument. Keeper, crew, captain, dashboard, sentinel, subscribe, the socket
   listener and the second and third substrates are all outside it, by that section.

   **`make core` is what "the build works" means for this sign-off.** `make full` stays the merge
   decision and stays whole-tree. A green `core` beside a red `full` is a real answer, not a
   contradiction: the tool does its job and something outside the core does not.

   **Amended 2026-08-07 (`hk-od9d4`): `make core` now runs without `-short`, and that is what makes
   it worth running.** As first written it passed `-short`, which skipped 45 tests in the core set —
   35 in `internal/daemon`, 10 in `internal/runloop`. The daemon 35 included
   `TestScenario_HappyPath_N1` and `TestSmokeLoop`, the two end-to-end tests that put a bead in one
   end of the queue and assert it comes out closed at the other. The gate was green without ever
   running the proof it existed to give. The test step goes from 144 seconds to 246. `make core` now
   also depends on `twins`, because seven of the newly-enabled tests skip silently when the twin
   binaries are not built, and a silent skip reads exactly like a pass.

   **`make core` IS NOT RELIABLY GREEN, AND THE ASSESSOR MUST KNOW THAT BEFORE RUNNING IT.**
   Six full runs on 2026-08-07 gave **three green and three red**, with a different test failing each
   time. Every red was in `internal/daemon`. The four distinct tests seen failing were:

   | Test | Skipped by `-short` before? | Status |
   | --- | --- | --- |
   | `TestT6_10BeadSequentialDrain` | yes — this change enabled it | **fixed** (`hk-oipc9`), then 12 of 12 green |
   | `TestMultiBead_TwoBeadsCompleteBothClose` | no — already in the gate | open, `hk-4f1bs`, and 12 of 12 green alone |
   | `TestWorkLoop_TwoConcurrentBeads` | no — already in the gate | open, same family |
   | `TestParallelSmoke_TwoBeadsConcurrent` | yes — this change enabled it | open, same family, and took 61s in the failing run against about 6s normally |

   Read the middle column carefully, because it carries the honest verdict on this change. **Two of
   the four were already in the gate**, unguarded, running in every `make core` before this change.
   The other two are tests this change admitted, one of which had a real defect that is now fixed.

   Be careful about what that does and does not prove. It is tempting to write "so the gate was
   always flaky", and the evidence does not support it. The one recorded pre-change run was green:
   5,238 pass, 45 skipped, exit 0. What is measured is that those two tests were **present and
   unguarded**, not that they were failing. The paragraph below argues this change adds the
   contention that trips them, which cuts the other way. Both things are true and neither is the
   whole story.

   **This family is already on List A, at A4: `hk-59flr`, `hk-hqttl`, `hk-fr7ht` — "the daemon suite
   goes red under load with a different test each time, and each passes alone."** That entry carries
   an operator ruling from 2026-08-06: do not open another investigation, scope the suite to the core
   queue and judge on that, because "a flake in a package the queue does not depend on stops being a
   blocker by definition rather than by investigation." **The scoping was done and it does not reach
   these four.** All four live in `internal/daemon`, which is inside the core set, so there is no
   package boundary to put between them and the gate. The ruling is not being reopened
   here. It is being reported that its remedy does not cover this case, which is a fact the operator
   needs and did not have when the ruling was made.

   `TestWorkLoop_TwoConcurrentBeads` also has recorded history worth reading before anyone re-derives
   it: `hk-5pwv5` (closed 2026-06-11) names that exact test, puts it in a "macOS socket-path and
   contention class", and records it reproducing in isolation on clean `main` as environmental rather
   than a code regression. It was closed as likely subsumed by a fix that moved daemon tests off the
   shared `~/.claude.json`. It is back.

   These are all one failure family: the daemon suite goes red under load
   with a different test each time. The Makefile's own `CORE_PKGS` comment says it "has been
   investigated four or five times without resolution", and scoping the package set was the answer
   that was available at the time. Scoping did not remove it, because the family lives inside the
   core set. Every one of these tests passes alone with large margins — `hk-4f1bs` finishes in 5 to 8
   seconds against a 25-second budget — so what fails is a wall-clock bet under contention, not the
   product path. **And this change adds contention:** dropping `-short` puts 35 real-daemon
   end-to-end tests into the same `internal/daemon` binary, so every wall-clock budget in that
   package now competes with more work.

   **HOW LOADED THE BOX IS DECIDES THE RESULT, so run the gate on a quiet one.** The first five runs
   were taken on a shared development machine with other agents working on it. Load average moved
   between about 4 and 7 on ten cores across the session, and the reds cluster in the busy part of
   it. One deliberate contention probe made this unmistakable: the same core set run with
   `go test -p 4` while three other agent sessions were active produced **six** failures, and
   `TestT6_10BeadSequentialDrain` — which passes in about 45 seconds and had just gone 12 for 12 —
   took 121 seconds. Several other failures landed within a second of 60, which is a budget expiring,
   not a product path breaking. `hk-4f1bs` records the same operating condition independently.

   **The quietest run available supports that.** A sixth run was queued behind a wait-for-quiet loop
   and started at load 3.90 with 37 GiB free: **green, exit 0, 465 seconds, 71,456 tests across 29
   packages, 3 skipped** — and those 3 are unrelated `internal/queue` subtests, not the real-daemon
   tier. Running tally is therefore three green and three red in six, and the reds cluster in the busy
   part of the session.

   So the numbers above are a floor on reliability, not a fair estimate of it. An assessor running
   alone should see fewer reds than three in six. That is a reason to re-run rather than a reason to
   trust one green.

   **WHAT TO DO WITH A RED, in order.** A single red is not a verdict.

   1. **Run `make leakcheck` FIRST, then check the load.** `uptime` tells you the number and not
      whose it is, and the scenario tier has been measured leaking a test binary that span at 100%
      CPU for 76 minutes after its own run finished (`hk-gate-leaks-spinning-test-binary-x4qgy`). A
      box that looks busy because of the LAST run is not "other agents were working", and reading it
      that way retires a red for the wrong reason. If other work was on the box, the run does not
      count. Re-run on a quiet one.
   2. Re-run the failing test alone. Every member of this family passes alone with a large margin.
      A test that fails alone is NOT this family and IS a real signal — treat it as one.
   3. If it fails alone, or fails repeatedly on a quiet box, that is a finding. Stop and read it.
   4. If it only fails inside a loaded full run, it is this family. Record it against `hk-4f1bs`
      with the load figure and move on.

   The criterion is the FAMILY — a wall-clock budget in `internal/daemon` losing under contention —
   not the four names in the table. The table is what has been seen so far, not the full membership.

   **Does a red in this family block the sign-off?** That is an operator call and it has not been
   made. This file records what the gate does. It does not have the authority to waive a red.

   This is a worse-looking gate and a better one. It can now fail on the tests that prove the core
   works, and before those tests never ran.

   **`make test-scenario` is a separate matter and it is still red.** The same change widened that
   tier from two packages to the six that actually carry `//go:build scenario` files, which is how
   the 11 never-run tests were found. Measured back to back on 2026-08-07: the old package list gave
   18 failures in 464 seconds, the new one 17 in 471. The tier was already red before the change —
   that is `hk-97gcz` and `hk-ynohn`, both still on List A — and widening it cost seven seconds and
   added one pre-existing keeper flake
   (`hk-keeper-warn-cooldown-clock-bet-c5umc`, which fails standalone under plain `-short` on an
   untouched checkout). `make full` runs this tier, so **`make full` is red today for reasons that
   predate this work.** `make core` is green.
2. ~~**Triage the remaining ~40 P1 issues against the criterion at the top.**~~ **DONE 2026-08-07
   for the OPEN set** — see the dated section near the top of this file. List A is still a floor: the
   closed-but-unverified issues were not examined, and nothing filed after that date is covered.

---

## What `make full` actually does, measured 2026-08-07 on `f56c770c`

**`make full` is red, and for the first time this week it is red at the stage everyone thought it was
red at.** Until today it died two stages earlier, so every report of "red for the scenario tier" was
naming a tier the target never reached. Three runs on a quiet box, 46 GiB free:

| Stage | Result |
| --- | --- |
| `gate-static` | pass |
| `gate-test-compile` | pass |
| whole-tree `go test -short ./...` | **pass** — 74,817 tests, 108 of 108 packages, 53 skipped. Green on two consecutive runs. |
| `lint-allow` | **pass** — was 14 file-and-linter pairs off the list on the base (`hk-lint-allow-red-on-base-hdln0`), now fixed, and the 5 stale entries deleted. |
| `test-scenario` | **FAIL** — 19 failures across `internal/daemon` under `-tags scenario` and `test/scenario`. |
| `module-hygiene` | not reached |

**What was in the way, and it was not what the record said.** `internal/harness/codex` built four
harnesses against `$HOME/.codex` and then ran the fail-closed billing guard, so the merge decision
asked a question about the machine — and, because the guard writes `config.toml` before it asserts,
**rewrote the operator's real Codex config on every run.** Four parallel tests truncating one shared
file is why it presented as a flake. `hk-codex-harness-real-home-rquc5`.

**The scenario tier is the remaining red and it is not new.** The 19 failures are almost entirely
wall-clock timeouts waiting for a bead state change, inside a `-tags scenario` daemon run that took
507 seconds. That is `hk-97gcz` and `hk-ynohn`, plus the A4 load family. **Only ONE of the 19 appears
in the eight names `hk-97gcz` recorded** (`TestScenario_MultiBead_SerializedNCompletion`), which is
direct support for `hk-scenario-tier-nondeterministic-xt1wa`: the tier names a different set every
run, so neither its pass nor its failure is evidence about the code.

**None of the 19 is attributable to the work that got the gate this far, and that was measured rather
than argued.** Across both commits exactly one non-test, non-doc file changed —
`internal/daemon/workloop.go`, one line, a De Morgan rewrite of `daemonStopping`. All six reachable
input combinations were enumerated and the old and new expressions agree on every one, the nil handle
included, with no dereference in either form. Everything else is test files, this document and the
lint allow list.

**So the honest statement of where the gate stands: four of its five stages pass, and the fifth is a
tier whose own bead says its result is not evidence.** Whether that blocks the sign-off is an
operator call, and this file still does not have the authority to waive a red.

### The 19 were then isolated, and they are three things, not one

Each was run alone at `-race -count=3`, at load 3.2–7.2 with 44–45 GiB free.

- **ONE REAL DEFECT, and it is in the core queue.**
  `TestScenario_TerminatedButLocked_BootReconcileReleasesDispatchLock` **fails alone**, with a
  detector-confirmed data race. Every functional assertion in it passes; the only failure is the
  race. It is `hk-e7y44`, already on List A — the scheduler writes a group's status under the
  queue-store lock while the submit path walks the same object after releasing it. A detected race
  is never a false positive. **This is the only one of the nineteen that is evidence about the
  product.**
- **ONE DETERMINISTICALLY BROKEN TEST.** `TestScenario_Codex_EmptyModel_FullLifecycle` fails 3 of 3
  alone on a quiet box, in 25.8s every time — not load-sensitive at all. It asks for a workflow mode
  that has been RETIRED, so the run walks the standard graph, whose commit gate tries to run
  `make full` inside a three-file temp directory that is not a Go module. Every codex assertion in it
  passes. It fails because its sensor scans the shared event log without filtering to its own run,
  and its message names a cause it never established. Its sibling in the same file calls a fixture
  helper for exactly this reason; this one omits the call.
- **FIFTEEN STRUCTURAL UNDER-BUDGETS.** `hk-scenario-budgets-structural-2z9dx`.

**THIS FILE'S "large margins" CLAIM IS WRONG FOR MOST OF THE FAMILY, and the correction matters
because a policy hangs off it.** The text above generalises from `hk-4f1bs` — "5 to 8 seconds against
a 25-second budget" — and concludes that widening a timeout masks a flake. Measured:
`TestT2_ExitZeroNoSignal` is 4.7s against a 6s poll inside an 8s context, **1.3x**. The T2 family is
1.3–1.6x. `TestT4_CloseBeadError` allows 7s for two sequential dispatches that each incur a 3-second
stop-hook grace, so its **structural floor is 6s**. Only two of the fifteen fit "large margin".
These budgets were set against the happy path and never against the grace the fixtures actually pay,
so widening them is a real fix here rather than the masking the policy warns about. **Five of the
fifteen need no number at all** — they already wait on the right milestone and then impose a second,
smaller cap on top of it; two of those logged their named property as PASSED and then failed on a
teardown stopwatch.

**Four of the nineteen cannot fail on the property they are named for** and two are pure duplicates
that should be deleted — `hk-scenario-tests-cannot-fail-x9wij`. The sharpest:
`TestWorkLoop_ClaimSemaphore_BoundsClaimConcurrency` asserts peak concurrency is **at most** 4 with
no lower bound, and the loaded run recorded a peak of 1 — so it would pass unchanged if concurrency
were completely broken.

**And step 1 of the triage procedure below is unreliable by construction.**
`hk-gate-leaks-spinning-test-binary-x4qgy`: the `make full` run that produced these numbers leaked a
`daemon.test` at 100% CPU for 76 minutes, 66 minutes past its own 10-minute timeout. Every "re-run on
a quiet box" after it — including the ones in this file — was taken on a box the previous run had
poisoned by a full core. "Check the load" cannot tell you WHOSE load it is. Run `make leakcheck`
before any timing measurement, not `uptime`.

Sandboxing is settled: it is off, and the two sandbox items moved to List B.
